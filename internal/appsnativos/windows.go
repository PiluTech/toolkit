package appsnativos

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/PiluTech/toolkit/internal/exec"
)

// Executáveis do Windows, SEMPRE por caminho absoluto (nunca pelo PATH). O arquivo compila em qualquer SO: só os comandos
// montados aqui dependem do Windows, e os testes usam um Runner falso.
const (
	powershellExe = `C:\Windows\System32\WindowsPowerShell\v1.0\powershell.exe`
	whoamiExe     = `C:\Windows\System32\whoami.exe`

	listTimeout   = 2 * time.Minute
	removeTimeout = 5 * time.Minute
	maxPackages   = 20000

	// pkgEnv é a variável de ambiente que leva o nome do pacote aos scripts de remoção: o texto do script é CONSTANTE e o
	// nome (validado por reFullName) nunca é concatenado nele.
	pkgEnv = "TOOLKIT_PKG"
)

// ErrEntradaInvalida indica nome de pacote fora do formato seguro. É devolvido ANTES de executar qualquer comando.
var ErrEntradaInvalida = errors.New("entrada inválida")

// Scripts do PowerShell: só ASCII. A saída é JSON compacto com todo caractere não ASCII escapado (\uXXXX), para não depender
// da página de código do console.
const (
	psPreamble = `$ErrorActionPreference='Stop'; `
	psEscape   = `[regex]::Replace($j,'[^\x00-\x7F]',{param($m) '\u{0:x4}' -f [int][char]$m.Value})`

	scriptListProvisioned = psPreamble + `$r=@(Get-AppxProvisionedPackage -Online | ForEach-Object { [pscustomobject]@{DisplayName=[string]$_.DisplayName;PackageName=[string]$_.PackageName;Version=[string]$_.Version} }); ` +
		`$j=ConvertTo-Json -Compress -InputObject $r; ` + psEscape

	scriptRemoveUser        = psPreamble + `Remove-AppxPackage -Package $env:` + pkgEnv
	scriptRemoveAll         = psPreamble + `Remove-AppxPackage -Package $env:` + pkgEnv + ` -AllUsers`
	scriptRemoveProvisioned = psPreamble + `Remove-AppxProvisionedPackage -Online -PackageName $env:` + pkgEnv + ` | Out-Null`
)

func scriptListInstalled(allUsers bool) string {
	cmd := "Get-AppxPackage"
	if allUsers {
		cmd += " -AllUsers"
	}
	return psPreamble + `$r=@(` + cmd + ` | ForEach-Object { [pscustomobject]@{Name=[string]$_.Name;PackageFullName=[string]$_.PackageFullName;PackageFamilyName=[string]$_.PackageFamilyName;` +
		`Version=[string]$_.Version;Publisher=[string]$_.Publisher;IsFramework=[bool]$_.IsFramework;SignatureKind=[string]$_.SignatureKind;NonRemovable=[bool]$_.NonRemovable} }); ` +
		`$j=ConvertTo-Json -Compress -InputObject $r; ` + psEscape
}

func psArgs(script string) []string {
	return []string{"-NoProfile", "-NonInteractive", "-Command", script}
}

// windowsSystem implementa System sobre o powershell.exe.
//
// Leituras vão pelo exec.ReadOnlyRunner (ReadOnly=true, nunca Elevate); remoções pelo exec.MutatingRunner (ReadOnly=false;
// Elevate=true para todos os usuários e para o provisionamento; remoção só do usuário atual sem Elevate). Nenhum shell
// implícito: os argumentos vão como lista e o nome do pacote por variável de ambiente, depois de validado.
type windowsSystem struct {
	read     exec.Runner
	mutate   exec.Runner
	elevated func() bool
}

// NewWindowsSystem cria o System real do Windows. elevated diz se o processo está elevado (nil = não).
func NewWindowsSystem(rn exec.Runner, elevated func() bool) System {
	return &windowsSystem{read: exec.ReadOnlyRunner(rn), mutate: exec.MutatingRunner(rn), elevated: elevated}
}

// IsAdmin devolve o callback injetado (nil = false).
func (w *windowsSystem) IsAdmin(context.Context) bool { return w.elevated != nil && w.elevated() }

// snippet resume a saída de erro de um comando: sem controles, uma linha, até 200 caracteres.
func snippet(r exec.Result) string {
	s := strings.TrimSpace(string(r.Stderr))
	if s == "" {
		s = strings.TrimSpace(string(r.Stdout))
	}
	s = strings.Map(func(c rune) rune {
		if unicode.IsControl(c) || unicode.Is(unicode.Cf, c) {
			return ' '
		}
		return c
	}, s)
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > 200 {
		s = string(r[:200]) + "..."
	}
	return s
}

func cmdFailed(name string, r exec.Result) error {
	exe := name[strings.LastIndex(name, `\`)+1:]
	return fmt.Errorf("%s terminou com código %d: %s", exe, r.ExitCode, snippet(r))
}

func (w *windowsSystem) readPS(ctx context.Context, script string) ([]byte, error) {
	c := exec.Cmd{Name: powershellExe, Args: psArgs(script), ReadOnly: true, Timeout: listTimeout}
	r, err := w.read.Run(ctx, c)
	if err != nil {
		return nil, fmt.Errorf("powershell: %w", err)
	}
	if r.ExitCode != 0 {
		return nil, cmdFailed(c.Name, r)
	}
	if r.Truncated {
		return nil, errors.New("powershell: a saída passou do limite e foi cortada")
	}
	out := bytes.TrimSpace(bytes.TrimPrefix(bytes.TrimSpace(r.Stdout), []byte("\xef\xbb\xbf")))
	if len(out) == 0 {
		return nil, errors.New("powershell: saída vazia")
	}
	return out, nil
}

// decodeList aceita um array JSON ou um objeto único (o PowerShell 5.1 desembrulha listas de um item).
func decodeList(out []byte, v any) error {
	switch out[0] {
	case '[', '{':
		return json.Unmarshal(out, v)
	}
	return errors.New("saída inesperada (não é JSON de lista)")
}

type jsonPackage struct {
	Name              string
	PackageFullName   string
	PackageFamilyName string
	Version           string
	Publisher         string
	IsFramework       bool
	SignatureKind     string
	NonRemovable      bool
}

// ListInstalled roda Get-AppxPackage (com -AllUsers só se pedido, e então só como administrador).
func (w *windowsSystem) ListInstalled(ctx context.Context, allUsers bool) ([]Package, error) {
	out, err := w.readPS(ctx, scriptListInstalled(allUsers))
	if err != nil {
		return nil, err
	}
	var raw []jsonPackage
	if out[0] == '{' {
		var one jsonPackage
		if err := json.Unmarshal(out, &one); err != nil {
			return nil, fmt.Errorf("saída do Get-AppxPackage ilegível: %w", err)
		}
		raw = []jsonPackage{one}
	} else if err := decodeList(out, &raw); err != nil {
		return nil, fmt.Errorf("saída do Get-AppxPackage ilegível: %w", err)
	}
	if len(raw) > maxPackages {
		return nil, errors.New("pacotes demais na saída do Get-AppxPackage")
	}
	pk := make([]Package, 0, len(raw))
	for _, r := range raw {
		pk = append(pk, Package(r))
	}
	return pk, nil
}

type jsonProvisioned struct {
	DisplayName string
	PackageName string
	Version     string
}

// ListProvisioned roda Get-AppxProvisionedPackage -Online (administrador).
func (w *windowsSystem) ListProvisioned(ctx context.Context) ([]Provisioned, error) {
	out, err := w.readPS(ctx, scriptListProvisioned)
	if err != nil {
		return nil, err
	}
	var raw []jsonProvisioned
	if out[0] == '{' {
		var one jsonProvisioned
		if err := json.Unmarshal(out, &one); err != nil {
			return nil, fmt.Errorf("saída do Get-AppxProvisionedPackage ilegível: %w", err)
		}
		raw = []jsonProvisioned{one}
	} else if err := decodeList(out, &raw); err != nil {
		return nil, fmt.Errorf("saída do Get-AppxProvisionedPackage ilegível: %w", err)
	}
	if len(raw) > maxPackages {
		return nil, errors.New("pacotes demais na saída do Get-AppxProvisionedPackage")
	}
	pv := make([]Provisioned, 0, len(raw))
	for _, r := range raw {
		pv = append(pv, Provisioned(r))
	}
	return pv, nil
}

func (w *windowsSystem) removeCmd(ctx context.Context, script string, elevate bool, name string) error {
	if !reFullName.MatchString(name) {
		return fmt.Errorf("%w: nome de pacote %q fora do formato (só letras, números, . _ ~ e -, sem começar com '-')", ErrEntradaInvalida, printableShort(name))
	}
	c := exec.Cmd{Name: powershellExe, Args: psArgs(script), Env: []string{pkgEnv + "=" + name}, Elevate: elevate, Timeout: removeTimeout}
	r, err := w.mutate.Run(ctx, c)
	if err != nil {
		if errors.Is(err, exec.ErrNotElevated) {
			return fmt.Errorf("%w: %w", ErrNeedsAdmin, err)
		}
		return fmt.Errorf("powershell: %w", err)
	}
	if r.ExitCode != 0 {
		return cmdFailed(c.Name, r)
	}
	return nil
}

// RemoveForUser: Remove-AppxPackage -Package (só o usuário atual; sem elevação).
func (w *windowsSystem) RemoveForUser(ctx context.Context, full string) error {
	return w.removeCmd(ctx, scriptRemoveUser, false, full)
}

// RemoveAllUsers: Remove-AppxPackage -Package -AllUsers (administrador).
func (w *windowsSystem) RemoveAllUsers(ctx context.Context, full string) error {
	return w.removeCmd(ctx, scriptRemoveAll, true, full)
}

// RemoveProvisioned: Remove-AppxProvisionedPackage -Online -PackageName (administrador).
func (w *windowsSystem) RemoveProvisioned(ctx context.Context, name string) error {
	return w.removeCmd(ctx, scriptRemoveProvisioned, true, name)
}

// UserSID lê o SID do usuário do processo: `whoami /user /fo csv /nh` devolve uma linha "DOMINIO\usuario","S-1-5-21-...".
// Só leitura. O SID é do token do processo (o mesmo usuário com ou sem elevação por UAC).
func (w *windowsSystem) UserSID(ctx context.Context) (string, error) {
	c := exec.Cmd{Name: whoamiExe, Args: []string{"/user", "/fo", "csv", "/nh"}, ReadOnly: true, Timeout: time.Minute}
	r, err := w.read.Run(ctx, c)
	if err != nil {
		return "", fmt.Errorf("whoami: %w", err)
	}
	if r.ExitCode != 0 {
		return "", cmdFailed(c.Name, r)
	}
	rec, err := csv.NewReader(strings.NewReader(strings.TrimSpace(string(r.Stdout)))).Read()
	if err != nil || len(rec) != 2 || !validSID(rec[1]) {
		return "", errors.New("whoami: saída inesperada, não deu para ler o SID do usuário")
	}
	return rec[1], nil
}

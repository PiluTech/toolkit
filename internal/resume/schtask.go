package resume

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"unicode/utf16"

	"github.com/PiluTech/toolkit/internal/buildinfo"
	"github.com/PiluTech/toolkit/internal/exec"
)

const windowsPowerShell = `C:\Windows\System32\WindowsPowerShell\v1.0\powershell.exe`

// TaskName é o nome da tarefa agendada (e do label do launchd): dev.pilutech.<name>.
func TaskName() string { return buildinfo.Identifier() }

// SchTask registra a retomada como Tarefa Agendada do Windows, pelo cmdlet Register-ScheduledTask
// (não `schtasks /create`: o /tr tem limite de 262 caracteres e `/rl HIGHEST` com `/sc onlogon` dispara
// para qualquer usuário). O gatilho é o logon do usuário que iniciou a receita, com principal Interactive
// (não SYSTEM): wsl --install e a distro são por usuário e a janela precisa aparecer para ele.
type SchTask struct {
	Runner exec.Runner
	FS     FS
	BinDir string // ex.: C:\ProgramData\toolkit\bin: o binário é copiado para cá antes de registrar
}

var _ Registrar = SchTask{}

func (s SchTask) exe() string { return s.BinDir + `\` + buildinfo.Name + ".exe" }

// psQuote devolve s como literal PowerShell entre aspas simples (' vira ”). Recusa controle.
func psQuote(s string) (string, error) {
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			return "", fmt.Errorf("texto com caractere de controle: %q", s)
		}
	}
	return "'" + strings.ReplaceAll(s, "'", "''") + "'", nil
}

func encodePS(script string) string {
	u := utf16.Encode([]rune(script))
	b := make([]byte, 0, len(u)*2)
	for _, c := range u {
		b = append(b, byte(c), byte(c>>8))
	}
	return base64.StdEncoding.EncodeToString(b)
}

func psCmd(script string, readOnly bool) exec.Cmd {
	return exec.Cmd{
		Name:     windowsPowerShell,
		Args:     []string{"-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-EncodedCommand", encodePS(script)},
		ReadOnly: readOnly,
	}
}

// RegisterScript monta o script do Register-ScheduledTask (público para o teste conferir o texto).
func (s SchTask) RegisterScript(sp Spec) (string, error) {
	if err := ValidArgs(sp.Args); err != nil {
		return "", err
	}
	if sp.User == "" {
		return "", errors.New("retomada no Windows exige o usuário que iniciou a receita")
	}
	if err := validPath(s.BinDir, false); err != nil {
		return "", err
	}
	exe, err := psQuote(s.exe())
	if err != nil {
		return "", err
	}
	user, err := psQuote(sp.User)
	if err != nil {
		return "", err
	}
	name, _ := psQuote(TaskName())
	level := "Limited"
	if sp.Elevated {
		level = "Highest"
	}
	return strings.Join([]string{
		"$ErrorActionPreference = 'Stop'",
		"$action = New-ScheduledTaskAction -Execute " + exe + " -Argument '" + strings.Join(sp.Args, " ") + "'",
		"$trigger = New-ScheduledTaskTrigger -AtLogOn -User " + user,
		"$principal = New-ScheduledTaskPrincipal -UserId " + user + " -LogonType Interactive -RunLevel " + level,
		"Register-ScheduledTask -TaskName " + name + " -Action $action -Trigger $trigger -Principal $principal -Force | Out-Null",
	}, "\n"), nil
}

// Register copia o binário para BinDir (as pastas Downloads e temp podem sumir entre os boots) e
// registra a tarefa.
func (s SchTask) Register(ctx context.Context, sp Spec) error {
	script, err := s.RegisterScript(sp)
	if err != nil {
		return err
	}
	if sp.Executable == "" {
		return errors.New("falta o caminho do binário")
	}
	if err := s.FS.MkdirAll(s.BinDir, 0o755); err != nil {
		return fmt.Errorf("criar %s: %w", s.BinDir, err)
	}
	if !sameWinPath(sp.Executable, s.exe()) {
		if err := s.FS.CopyFile(sp.Executable, s.exe(), 0o755); err != nil {
			return fmt.Errorf("copiar o binário para %s: %w", s.exe(), err)
		}
	}
	return runOK(ctx, s.Runner, psCmd(script, false), "registrar a tarefa agendada")
}

func sameWinPath(a, b string) bool {
	return strings.EqualFold(strings.ReplaceAll(a, `\`, "/"), strings.ReplaceAll(b, `\`, "/"))
}

// Unregister remove a tarefa; não registrada = sucesso.
func (s SchTask) Unregister(ctx context.Context) error {
	name, _ := psQuote(TaskName())
	script := "$ErrorActionPreference = 'Stop'\n" +
		"if (Get-ScheduledTask -TaskName " + name + " -ErrorAction SilentlyContinue) { Unregister-ScheduledTask -TaskName " + name + " -Confirm:$false }"
	return runOK(ctx, s.Runner, psCmd(script, false), "remover a tarefa agendada")
}

// IsRegistered consulta a tarefa (exit 0 = existe, 1 = não existe).
func (s SchTask) IsRegistered(ctx context.Context) (bool, error) {
	name, _ := psQuote(TaskName())
	script := "if (Get-ScheduledTask -TaskName " + name + " -ErrorAction SilentlyContinue) { exit 0 } else { exit 1 }"
	res, err := s.Runner.Run(ctx, psCmd(script, true))
	if err != nil {
		return false, err
	}
	switch res.ExitCode {
	case 0:
		return true, nil
	case 1:
		return false, nil
	}
	return false, fmt.Errorf("consultar a tarefa agendada: código %d", res.ExitCode)
}

// runOK roda c e transforma exit code != 0 em erro com um trecho limpo da saída.
func runOK(ctx context.Context, r exec.Runner, c exec.Cmd, what string) error {
	res, err := r.Run(ctx, c)
	if err != nil {
		return fmt.Errorf("%s: %w", what, err)
	}
	if res.ExitCode != 0 {
		return fmt.Errorf("%s: código %d: %s", what, res.ExitCode, snippet(res))
	}
	return nil
}

func snippet(res exec.Result) string {
	s := strings.TrimSpace(string(res.Stderr))
	if s == "" {
		s = strings.TrimSpace(string(res.Stdout))
	}
	if len(s) > 300 {
		s = s[:300] + "..."
	}
	return strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' {
			return ' '
		}
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, s)
}

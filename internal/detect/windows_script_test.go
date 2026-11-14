package detect

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/PiluTech/toolkit/internal/catalog"
	tkexec "github.com/PiluTech/toolkit/internal/exec"
)

// legacyScript é o script ANTIGO (só o trecho dos caminhos), com o defeito que a Lupa achou: no PowerShell a
// vírgula tem precedência maior que o +, então @('HKLM','HKLM:'+$cv+'Uninstall') vira 4 elementos e $r[1] é
// só 'HKLM:'. Fica aqui para provar que a verificação estática reprova esse padrão.
const legacyScript = `$cv='\SOFTWARE\Microsoft\Windows\CurrentVersion\';` +
	`$u=@(@('HKLM','HKLM:'+$cv+'Uninstall'),@('HKLM-WOW64','HKLM:\SOFTWARE\WOW6432Node\Microsoft\Windows\CurrentVersion\Uninstall'),@('HKCU','HKCU:'+$cv+'Uninstall'));` +
	`foreach($r in @(@('HKLM','HKLM:'+$cv+'App Paths'),@('HKCU','HKCU:'+$cv+'App Paths'))){}`

var wantUninstallPaths = []string{
	`HKLM:\SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall`,
	`HKLM:\SOFTWARE\WOW6432Node\Microsoft\Windows\CurrentVersion\Uninstall`,
	`HKCU:\SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall`,
}

var wantAppPaths = []string{
	`HKLM:\SOFTWARE\Microsoft\Windows\CurrentVersion\App Paths`,
	`HKCU:\SOFTWARE\Microsoft\Windows\CurrentVersion\App Paths`,
}

var (
	pathConcatRe = regexp.MustCompile(`'HK(LM|CU):[^']*'\s*\+|\+\s*'?HK(LM|CU):|\$cv\b`)
	pathLitRe    = regexp.MustCompile(`'(HK(?:LM|CU):[^']*)'`)
)

// scriptPathProblems devolve o que há de errado nos caminhos de registro do script: concatenação com + (a
// vírgula do PowerShell pega antes) e a diferença para os 5 caminhos literais completos esperados.
func scriptPathProblems(script string) []string {
	var out []string
	if pathConcatRe.MatchString(script) {
		out = append(out, "caminho de registro montado com + (ou variável $cv): use literais completos")
	}
	got := map[string]int{}
	for _, m := range pathLitRe.FindAllStringSubmatch(script, -1) {
		got[m[1]]++
	}
	for _, w := range append(append([]string{}, wantUninstallPaths...), wantAppPaths...) {
		if got[w] != 1 {
			out = append(out, "caminho literal esperado exatamente 1 vez: "+w)
		}
		delete(got, w)
	}
	for extra := range got {
		out = append(out, "caminho inesperado: "+extra)
	}
	sort.Strings(out)
	return out
}

// Roda em qualquer SO (inclusive o Mac de desenvolvimento, sem PowerShell): os caminhos têm de ser literais.
func TestWindowsScriptCaminhosSaoLiteraisCompletos(t *testing.T) {
	if p := scriptPathProblems(uninstallScript); len(p) != 0 {
		t.Fatalf("script com caminhos de registro errados: %v", p)
	}
}

func TestWindowsScriptAntigoEhReprovadoPelaVerificacaoEstatica(t *testing.T) {
	p := scriptPathProblems(legacyScript)
	if len(p) == 0 || !strings.Contains(strings.Join(p, "\n"), "montado com +") {
		t.Fatalf("o script antigo deveria ser reprovado: %v", p)
	}
}

// --- parse e regras do App Paths (valor padrão, executável local existente) ---

func chromeApp(t *testing.T) catalog.App {
	t.Helper()
	return winApp(t, "display_name: Google Chrome, app_path: chrome.exe")
}

func TestWindowsAppPathsExigeExecutavelLocalExistente(t *testing.T) {
	cases := []struct {
		name  string
		line  string
		found bool
	}{
		{"app válido", ap("HKLM", "chrome.exe", chromePath, "1"), true},
		{"chave vazia (sem valor padrão)", ap("HKLM", "chrome.exe", "", "0"), false},
		{"só o nome da subchave, sem o valor padrão", ap("HKLM", "chrome.exe", "chrome.exe", "0"), false},
		{"caminho ausente (arquivo não existe)", ap("HKLM", "chrome.exe", chromePath, "0"), false},
		{"pasta, não arquivo", ap("HKLM", "chrome.exe", `C:\Program Files\Google\Chrome\Application\chrome.exe`, "0"), false},
		{"caminho de rede UNC", ap("HKLM", "chrome.exe", `\\servidor\share\chrome.exe`, "1"), false},
		{"caminho relativo", ap("HKLM", "chrome.exe", `Application\chrome.exe`, "1"), false},
		{"relativo à unidade", ap("HKLM", "chrome.exe", `C:chrome.exe`, "1"), false},
		{"extensão diferente de .exe", ap("HKLM", "chrome.exe", `C:\Apps\chrome.bat`, "1"), false},
		{"unidade sem arquivo", ap("HKLM", "chrome.exe", `C:\`, "1"), false},
		{"valor com aspas externas já tratadas pelo script", ap("HKCU", "chrome.exe", chromePath, "1"), true},
	}
	for _, tc := range cases {
		f, found := find(t, New(winStub(tc.line), Options{GOOS: "windows"}), chromeApp(t))
		if found != tc.found {
			t.Errorf("%s: found=%v (%+v)", tc.name, found, f)
		}
	}
}

func TestWindowsAppPathsNaoContornaPublisherDoCatalogo(t *testing.T) {
	a := winApp(t, `display_name: Google Chrome, publisher: "Google LLC", app_path: chrome.exe`)
	good := ap("HKLM", "chrome.exe", chromePath, "1")
	// App Paths sozinho não detecta app que exige publisher.
	if _, found := find(t, New(winStub(good), Options{GOOS: "windows"}), a); found {
		t.Fatal("App Paths sozinho contornou o publisher exigido")
	}
	// Publisher divergente no Uninstall + App Paths válido: também não.
	if _, found := find(t, New(winStub(u("HKLM", "Outra coisa", "1", "Outra Empresa"), good), Options{GOOS: "windows"}), a); found {
		t.Fatal("publisher divergente + App Paths não pode detectar")
	}
	// Outro programa do MESMO publisher (Google Drive, Google LLC) não prova que o chrome.exe é o app: sem
	// vínculo entre o publisher e o executável, o fallback do App Paths é recusado.
	for _, other := range []string{"Google Drive", "Google Chrome Beta Renomeado"} {
		if _, found := find(t, New(winStub(u("HKLM", other, "1", "Google LLC"), good), Options{GOOS: "windows"}), a); found {
			t.Fatalf("%q do mesmo publisher autorizou o App Paths do chrome.exe", other)
		}
	}
	// O Uninstall do próprio app, com nome e publisher certos, continua detectando.
	if _, found := find(t, New(winStub(u("HKLM", "Google Chrome", "1", "Google LLC")), Options{GOOS: "windows"}), a); !found {
		t.Fatal("Uninstall com nome e publisher certos deveria detectar")
	}
}

// A unidade tem de ser LOCAL (DriveType Fixed) antes de qualquer acesso ao arquivo: Test-Path num destino de rede
// mapeado (Z:) já toca a rede. A verificação estática vale em qualquer SO; o teste executável fica abaixo.
func TestWindowsScriptValidaUnidadeLocalAntesDoTestPath(t *testing.T) {
	d := strings.Index(uninstallScript, "[IO.DriveInfo]")
	tp := strings.Index(uninstallScript, "Test-Path")
	if d < 0 || tp < 0 || d > tp {
		t.Fatalf("DriveInfo (%d) deve vir antes do Test-Path (%d)", d, tp)
	}
	if !strings.Contains(uninstallScript, "'Fixed'") {
		t.Fatal("só unidade Fixed é local")
	}
}

// --- estado por raiz ---

func TestWindowsStatusPorRaiz(t *testing.T) {
	a := winApp(t, "display_name: Discord")
	all := func(over map[rootKey]string) []string {
		var out []string
		for _, r := range append(append([]rootKey{}, uninstallRoots...), appPathsRoots...) {
			state := "ok"
			if o, ok := over[r]; ok {
				state = o
			}
			out = append(out, st(r.kind, r.origin, state))
		}
		return out
	}
	hkcuFail := map[rootKey]string{{"U", "HKCU"}: "falhou"}
	// Raiz ausente (normal) e tudo lido: não achou = não instalado (sem Incomplete).
	f, found := find(t, New(winRaw(all(map[rootKey]string{{"U", "HKCU"}: "ausente"})...), Options{GOOS: "windows"}), a)
	if found || f.Incomplete {
		t.Fatalf("ausente é normal: %+v %v", f, found)
	}
	// Uma raiz falhou e o app não foi achado: leitura incompleta.
	f, found = find(t, New(winRaw(all(hkcuFail)...), Options{GOOS: "windows"}), a)
	if found || !f.Incomplete {
		t.Fatalf("raiz falhou: esperava Incomplete: %+v %v", f, found)
	}
	// Uma raiz falhou, mas o app foi achado em outra: as leituras válidas valem.
	f, found = find(t, New(winRaw(append(all(hkcuFail), u("HKLM", "Discord", "1.0", "P"))...), Options{GOOS: "windows"}), a)
	if !found || f.Incomplete || f.Version != "1.0" {
		t.Fatalf("achado deveria valer: %+v %v", f, found)
	}
	// Raiz que não reportou estado (saída truncada) também é incompleta.
	if f, found = find(t, New(winRaw(u("HKCU", "Outro", "1", "P")), Options{GOOS: "windows"}), a); found || !f.Incomplete {
		t.Fatalf("sem estado: %+v %v", f, found)
	}
	// App Paths que falhou só pesa para app com app_path; Uninstall que falhou não pesa para app_path-only.
	ap1 := winApp(t, "app_path: chrome.exe")
	if f, found = find(t, New(winRaw(all(hkcuFail)...), Options{GOOS: "windows"}), ap1); found || f.Incomplete {
		t.Fatalf("Uninstall falho não importa para app só com app_path: %+v %v", f, found)
	}
	if f, found = find(t, New(winRaw(all(map[rootKey]string{{"A", "HKLM"}: "falhou"})...), Options{GOOS: "windows"}), ap1); found || !f.Incomplete {
		t.Fatalf("App Paths falho: %+v %v", f, found)
	}
	// PowerShell que falha por inteiro: incompleto, nunca "instalado".
	bad := &stub{handle: func(tkexec.Cmd) (tkexec.Result, error) { return tkexec.Result{ExitCode: 1}, nil }}
	if f, found = find(t, New(bad, Options{GOOS: "windows"}), a); found || !f.Incomplete {
		t.Fatalf("exit 1: %+v %v", f, found)
	}
}

// --- execução do script REAL num PowerShell de verdade, com cmdlets FALSOS ---

// fakeCmdlets sombreia Get-ChildItem e Test-Path (funções ganham dos cmdlets) e registra no stderr cada -Path
// recebido ("CAP|cmdlet|caminho"). Nada toca o registro real: as chaves são objetos sintéticos.
const fakeCmdlets = `$ErrorActionPreference='Stop';
function Key($name,$vals){$o=New-Object PSObject;$o|Add-Member NoteProperty PSChildName $name;
 $o|Add-Member ScriptMethod GetValue {param($n,$d,$opt) $this.V[$n]};$o|Add-Member NoteProperty V $vals;$o};
function Get-ChildItem{param([string]$Path)
 [Console]::Error.WriteLine('CAP|GCI|'+$Path);
 switch($Path){
 'HKLM:\SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall'{
  Key 'g1' @{DisplayName='Git';DisplayVersion='2.45.1';Publisher='The Git Development Community';SystemComponent=0}
  Key 'g2' @{DisplayName='Git';SystemComponent=1}
  Key 'g3' @{DisplayName='Git Patch';ParentKeyName='x'}
  Key 'g4' @{DisplayVersion='9'}}
 'HKLM:\SOFTWARE\WOW6432Node\Microsoft\Windows\CurrentVersion\Uninstall'{throw (New-Object System.Management.Automation.ItemNotFoundException 'ausente')}
 'HKCU:\SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall'{throw 'acesso negado'}
 'HKLM:\SOFTWARE\Microsoft\Windows\CurrentVersion\App Paths'{
  Key 'chrome.exe' @{''='"%TKFAKE%\chrome.exe"'}
  Key 'vazio.exe' @{}
  Key 'rede.exe' @{''='\\servidor\s\rede.exe'}
  Key 'rel.exe' @{''='rel.exe'}
  Key 'pasta.exe' @{''='C:\Pasta\pasta.exe'}
  Key 'mapeado.exe' @{''='Z:\Rede\mapeado.exe'}}
 'HKCU:\SOFTWARE\Microsoft\Windows\CurrentVersion\App Paths'{throw (New-Object System.Management.Automation.ItemNotFoundException 'ausente')}
 default{throw ('caminho inesperado: '+$Path)}}};
function Test-Path{param([string]$LiteralPath,[string]$PathType)
 [Console]::Error.WriteLine('CAP|TP|'+$LiteralPath+'|'+$PathType);
 $LiteralPath -eq 'C:\Fake\chrome.exe'};
`

func shell(t *testing.T) string {
	t.Helper()
	if runtime.GOOS != "windows" {
		t.Skip("o script usa [IO.DriveInfo] do Windows: o teste executável roda só no runner Windows do CI")
	}
	for _, n := range []string{"pwsh", "powershell"} {
		if p, err := exec.LookPath(n); err == nil {
			return p
		}
	}
	if runtime.GOOS == "windows" {
		t.Fatal("PowerShell ausente no Windows: o teste executável do script não pode ser pulado aqui")
	}
	t.Skip("o script usa [IO.DriveInfo] do Windows: o teste executável roda só no runner Windows do CI")
	return ""
}

func TestWindowsScriptRealExecutadoComCmdletsFalsos(t *testing.T) {
	sh := shell(t)
	var stderr bytes.Buffer
	s := &stub{handle: func(c tkexec.Cmd) (tkexec.Result, error) {
		script := c.Args[len(c.Args)-1]
		cmd := exec.CommandContext(context.Background(), sh, "-NoProfile", "-NonInteractive", "-Command", fakeCmdlets+script) //nolint:gosec // só o PowerShell de TESTE, com cmdlets falsos
		cmd.Env = append(os.Environ(), `TKFAKE=C:\Fake`)
		var out bytes.Buffer
		cmd.Stdout, cmd.Stderr = &out, &stderr
		err := cmd.Run()
		return tkexec.Result{Stdout: out.Bytes()}, err
	}}
	a := winApp(t, "display_name: Git, app_path: chrome.exe")
	f, found := find(t, New(s, Options{GOOS: "windows"}), a)
	if !found || f.Version != "2.45.1" {
		t.Fatalf("Git deveria ser achado pelo Uninstall HKLM: %+v %v\nstderr: %s", f, found, stderr.String())
	}
	var gci, tp []string
	for _, l := range strings.Split(stderr.String(), "\n") {
		l = strings.TrimSpace(l)
		switch {
		case strings.HasPrefix(l, "CAP|GCI|"):
			gci = append(gci, strings.TrimPrefix(l, "CAP|GCI|"))
		case strings.HasPrefix(l, "CAP|TP|"):
			tp = append(tp, strings.TrimPrefix(l, "CAP|TP|"))
		case l != "":
			t.Errorf("stderr inesperado do script: %q", l)
		}
	}
	want := append(append([]string{}, wantUninstallPaths...), wantAppPaths...)
	sort.Strings(want)
	sort.Strings(gci)
	if strings.Join(gci, "\n") != strings.Join(want, "\n") {
		t.Fatalf("Get-ChildItem -Path recebeu\n%s\nesperado\n%s", strings.Join(gci, "\n"), strings.Join(want, "\n"))
	}
	// Só o chrome.exe (local absoluto .exe, com a variável expandida) vai ao Test-Path, como arquivo (Leaf); o
	// destino em Z: (unidade que não é Fixed) é recusado ANTES, sem chegar ao acesso ao arquivo.
	if strings.Join(tp, "\n") != `C:\Fake\chrome.exe|Leaf`+"\n"+`C:\Pasta\pasta.exe|Leaf` {
		t.Fatalf("Test-Path recebeu: %q", tp)
	}
	// O resultado completo: chrome.exe existe; o das pastas/UNC/relativo/sem valor não conta.
	for exe, want := range map[string]bool{"chrome.exe": true, "vazio.exe": false, "rede.exe": false, "rel.exe": false, "pasta.exe": false, "mapeado.exe": false} {
		_, got := find(t, New(s, Options{GOOS: "windows"}), winApp(t, "app_path: "+exe))
		if got != want {
			t.Errorf("%s: found=%v, esperado %v", exe, got, want)
		}
	}
	// HKCU\Uninstall falhou (acesso negado): app não achado vira leitura incompleta.
	f, found = find(t, New(s, Options{GOOS: "windows"}), winApp(t, "display_name: Nao Existe"))
	if found || !f.Incomplete {
		t.Fatalf("esperava leitura incompleta: %+v %v", f, found)
	}
}

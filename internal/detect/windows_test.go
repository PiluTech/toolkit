package detect

import (
	"errors"
	"strings"
	"testing"

	"github.com/PiluTech/toolkit/internal/catalog"
	"github.com/PiluTech/toolkit/internal/exec"
)

// Formato da saída do script (ver windows.go): uma linha por item, separada por TAB.
//   U origem nome versão publisher SystemComponent ParentKeyName   (chave de desinstalação)
//   A origem exe                                                   (App Paths)
// As fixtures são SINTÉTICAS: nomes no formato conhecido de cada instalador, não capturas de uma máquina real.

func winStub(lines ...string) *stub {
	out := strings.Join(lines, "\r\n") + "\r\n"
	return &stub{handle: func(exec.Cmd) (exec.Result, error) { return ok(out) }}
}

// u monta uma linha de chave de desinstalação.
func u(origin, name, version, publisher string) string {
	return strings.Join([]string{"U", origin, name, version, publisher, "0", "0"}, "\t")
}

func winApp(t *testing.T, fields string) catalog.App {
	t.Helper()
	return app(t, "      windows: { "+fields+" }\n")
}

// Cada app do catálogo embutido, com o DisplayName típico e a origem esperada.
func TestWindowsCadaAppDoCatalogoEhDetectadoComAOrigem(t *testing.T) {
	cases := []struct {
		id, display, version, origin string
	}{
		{"git", "Git", "2.45.1", "HKLM"},
		{"vscode", "Microsoft Visual Studio Code", "1.95.0", "HKLM"},
		{"vscode", "Microsoft Visual Studio Code (User)", "1.95.0", "HKCU"},
		{"discord", "Discord", "1.0.9170", "HKCU"},
		{"firefox", "Mozilla Firefox (x64 pt-BR)", "132.0", "HKLM"},
		{"vlc", "VLC media player", "3.0.21", "HKLM"},
		{"7zip", "7-Zip 24.08 (x64 edition)", "24.08", "HKLM"},
		{"7zip", "7-Zip", "24.08", "HKLM"},
		{"brave", "Brave", "130.1.71.118", "HKLM"},
		{"chrome", "Google Chrome", "130.0.6723.92", "HKLM-WOW64"},
		{"libreoffice", "LibreOffice 25.8.2.2", "25.8.2.2", "HKLM"},
		{"peazip", "PeaZip", "9.9.0", "HKLM"},
	}
	c, err := catalog.Parse("catalog.yaml", embeddedCatalog(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range cases {
		a, ok := c.Get(tc.id)
		if !ok {
			t.Fatalf("app %s sem entrada no catálogo", tc.id)
		}
		s := winStub(u(tc.origin, tc.display, tc.version, "Editora"))
		f, found := find(t, New(s, Options{GOOS: "windows"}), a)
		if !found {
			t.Errorf("%s (%q): não detectado", tc.id, tc.display)
			continue
		}
		if f.Version != tc.version {
			t.Errorf("%s: versão %q", tc.id, f.Version)
		}
		if !strings.Contains(f.Where, tc.origin) || !strings.Contains(f.Where, tc.display) {
			t.Errorf("%s: origem não aparece em %q", tc.id, f.Where)
		}
	}
}

// Controles de falso positivo: entradas parecidas que NÃO são o app.
func TestWindowsFalsosPositivosNaoCasam(t *testing.T) {
	c, err := catalog.Parse("catalog.yaml", embeddedCatalog(t))
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct{ id, display string }{
		{"libreoffice", "LibreOffice 25.8 Help Pack (Portuguese)"},
		{"libreoffice", "LibreOffice 25.8.2.2 Help Pack (Portuguese)"},
		{"libreoffice", "LibreOffice Help Pack"},
		{"git", "Git Extensions"},
		{"git", "Git LFS"},
		{"vscode", "Microsoft Visual Studio Code Insiders"},
		{"brave", "Brave Update Helper"},
		{"brave", "BraveSoftware Update"},
		{"chrome", "Google Chrome Remote Desktop Host"},
		{"vlc", "VLC Skin"},
		{"7zip", "7-Zip Shell Extension"},
		{"firefox", "Mozilla Firefox ESR Helper"},
	}
	for _, tc := range cases {
		a, _ := c.Get(tc.id)
		if _, found := find(t, New(winStub(u("HKLM", tc.display, "1.0", "X")), Options{GOOS: "windows"}), a); found {
			t.Errorf("%s: %q não deveria casar", tc.id, tc.display)
		}
	}
}

func TestWindowsSystemComponentEParentKeyNameSaoIgnorados(t *testing.T) {
	a := winApp(t, "display_name: Git")
	sys := strings.Join([]string{"U", "HKLM", "Git", "2.0", "X", "1", "0"}, "\t")
	upd := strings.Join([]string{"U", "HKLM", "Git", "2.0", "X", "0", "1"}, "\t")
	if _, found := find(t, New(winStub(sys, upd), Options{GOOS: "windows"}), a); found {
		t.Fatal("SystemComponent=1 e atualização (ParentKeyName) não contam")
	}
	// Uma entrada boa depois das ignoradas ainda conta.
	if _, found := find(t, New(winStub(sys, upd, u("HKLM", "Git", "2.1", "X")), Options{GOOS: "windows"}), a); !found {
		t.Fatal("a entrada válida deveria casar")
	}
}

func TestWindowsDisplayNameVazioEhIgnorado(t *testing.T) {
	a := winApp(t, "display_name: Git")
	if _, found := find(t, New(winStub(u("HKLM", "", "1", "X"), u("HKLM", "   ", "1", "X")), Options{GOOS: "windows"}), a); found {
		t.Fatal("nome vazio não casa")
	}
}

func TestWindowsPublisherDeclaradoExigeIgualdade(t *testing.T) {
	a := winApp(t, `display_name: Chrome, publisher: "Google LLC"`)
	if _, found := find(t, New(winStub(u("HKLM", "Chrome", "1", "Outra Empresa")), Options{GOOS: "windows"}), a); found {
		t.Fatal("publisher diferente não casa")
	}
	if _, found := find(t, New(winStub(u("HKLM", "Chrome", "1", "")), Options{GOOS: "windows"}), a); found {
		t.Fatal("publisher ausente não casa quando o catálogo exige")
	}
	if _, found := find(t, New(winStub(u("HKLM", "Chrome", "1", "google llc")), Options{GOOS: "windows"}), a); !found {
		t.Fatal("publisher igual (sem maiúsculas) deveria casar")
	}
}

func TestWindowsAppPathsComoEvidenciaComplementar(t *testing.T) {
	a := winApp(t, "display_name: Google Chrome, app_path: chrome.exe")
	ap := strings.Join([]string{"A", "HKLM", "chrome.exe"}, "\t")
	f, found := find(t, New(winStub(ap), Options{GOOS: "windows"}), a)
	if !found || !strings.Contains(f.Where, "App Paths") || !strings.Contains(f.Where, "chrome.exe") || f.Version != "" {
		t.Fatalf("%v %+v", found, f)
	}
	// O Uninstall tem prioridade (traz a versão).
	f, found = find(t, New(winStub(ap, u("HKLM", "Google Chrome", "130", "G")), Options{GOOS: "windows"}), a)
	if !found || f.Version != "130" || strings.Contains(f.Where, "App Paths") {
		t.Fatalf("%v %+v", found, f)
	}
	// Outro exe não casa; sem app_path no catálogo o App Paths é ignorado.
	other := strings.Join([]string{"A", "HKCU", "firefox.exe"}, "\t")
	if _, found = find(t, New(winStub(other), Options{GOOS: "windows"}), a); found {
		t.Fatal("outro exe")
	}
	if _, found = find(t, New(winStub(ap), Options{GOOS: "windows"}), winApp(t, "display_name: Google Chrome")); found {
		t.Fatal("sem app_path no catálogo")
	}
	// App com só app_path (sem display_name) também funciona.
	if _, found = find(t, New(winStub(ap), Options{GOOS: "windows"}), winApp(t, "app_path: chrome.exe")); !found {
		t.Fatal("só app_path")
	}
}

func TestWindowsTextoDeForaEhSanitizado(t *testing.T) {
	a := winApp(t, "display_name: Discord")
	f, found := find(t, New(winStub(u("HKCU", "Discord\x1b[31m", "1.0\x1b[0m\u202e", "P")), Options{GOOS: "windows"}), a)
	// ESC no nome faz o nome diferir: não casa, e nada cru vaza.
	if found {
		t.Fatalf("nome com ESC não deveria casar: %+v", f)
	}
	f, found = find(t, New(winStub(u("HKCU", "Discord", "1.0\x1b[0m\u202e\x07", "P")), Options{GOOS: "windows"}), a)
	if !found || strings.ContainsAny(f.Version+f.Where, "\x1b\x07\u202e") {
		t.Fatalf("saída crua: %q %q", f.Where, f.Version)
	}
}

func TestWindowsFalhaNaoEhDeteccao(t *testing.T) {
	a := winApp(t, "display_name: Discord")
	for name, h := range map[string]func(exec.Cmd) (exec.Result, error){
		"erro": func(exec.Cmd) (exec.Result, error) { return exec.Result{}, errors.New("boom") },
		"exit": func(exec.Cmd) (exec.Result, error) {
			return exec.Result{ExitCode: 1, Stderr: []byte("negado"), Stdout: []byte(u("HKCU", "Discord", "1", "P"))}, nil
		},
		"vazio":       func(exec.Cmd) (exec.Result, error) { return ok("") },
		"sem nome":    func(exec.Cmd) (exec.Result, error) { return ok(u("HKCU", "", "1", "P") + "\r\n") },
		"lixo":        func(exec.Cmd) (exec.Result, error) { return ok("Discord\t1.0\r\nU\r\nU\tHKCU\r\n") },
		"origem ruim": func(exec.Cmd) (exec.Result, error) { return ok(u("HKXX", "Discord", "1", "P") + "\r\n") },
	} {
		if _, found := find(t, New(&stub{handle: h}, Options{GOOS: "windows"}), a); found {
			t.Errorf("%s: não deveria achar", name)
		}
	}
}

// Uma raiz que falha (acesso negado) só some da saída: as outras continuam valendo.
func TestWindowsRaizFaltandoNaoDerrubaAsOutras(t *testing.T) {
	a := winApp(t, "display_name: Discord")
	f, found := find(t, New(winStub(u("HKCU", "Discord", "1.0", "P")), Options{GOOS: "windows"}), a)
	if !found || !strings.Contains(f.Where, "HKCU") {
		t.Fatalf("%v %+v", found, f)
	}
}

// Contrato do comando: PowerShell fixo por caminho absoluto, só leitura, sem elevar, script ASCII, nada do
// catálogo no script, raízes cobertas e saída com exit 0 (uma raiz ausente, como o WOW6432Node do HKCU, não
// pode virar exit code diferente de zero e derrubar a detecção inteira).
func TestWindowsContratoDoComando(t *testing.T) {
	s := winStub(u("HKCU", "Discord", "1", "P"))
	d := New(s, Options{GOOS: "windows"})
	find(t, d, winApp(t, "display_name: Discord, publisher: Zzz, app_path: zzz.exe"))
	if len(s.calls) != 1 {
		t.Fatalf("chamadas: %v", s.calls)
	}
	c := s.calls[0]
	if c.Name != `C:\Windows\System32\WindowsPowerShell\v1.0\powershell.exe` || !c.ReadOnly || c.Elevate {
		t.Fatalf("comando: %+v", c)
	}
	script := c.Args[len(c.Args)-1]
	for _, r := range script {
		if r > 0x7e || (r < 0x20 && r != '\n') {
			t.Fatalf("script não ASCII imprimível: %q", r)
		}
	}
	for _, k := range []string{"HKLM:", "HKCU:", `WOW6432Node\Microsoft\Windows\CurrentVersion\Uninstall`, `App Paths`, "DisplayName", "Publisher", "DisplayVersion", "SystemComponent", "ParentKeyName", "exit 0"} {
		if !strings.Contains(script, k) {
			t.Errorf("o script não cobre %q", k)
		}
	}
	for _, k := range []string{"Discord", "Zzz", "zzz"} {
		if strings.Contains(script, k) {
			t.Errorf("%q do catálogo entrou no script", k)
		}
	}
	for _, bad := range []string{"Set-ItemProperty", "Remove-", "New-Item", "Start-Process", "Invoke-", "reg add", "reg delete"} {
		if strings.Contains(script, bad) {
			t.Errorf("script com comando de escrita %q", bad)
		}
	}
}

// Python não tem detect no Windows de propósito: as versões menores convivem e o DisplayName não diz qual o
// usuário pediu (detectar "Python 3.13" como "Python" faria o install de 3.12 dizer "já instalado").
func TestWindowsPythonSemDeteccaoDeProposito(t *testing.T) {
	c, err := catalog.Parse("catalog.yaml", embeddedCatalog(t))
	if err != nil {
		t.Fatal(err)
	}
	a, _ := c.Get("python")
	if _, ok := a.DetectFor("windows"); ok {
		t.Fatal("python não deve ter detect.windows")
	}
}

// Os apps com app_path no catálogo embutido são detectados só por ele (instalação sem entrada no Uninstall).
func TestWindowsAppPathsDoCatalogoEmbutido(t *testing.T) {
	c, err := catalog.Parse("catalog.yaml", embeddedCatalog(t))
	if err != nil {
		t.Fatal(err)
	}
	for id, exe := range map[string]string{"chrome": "chrome.exe", "firefox": "firefox.exe", "brave": "brave.exe"} {
		a, _ := c.Get(id)
		f, found := find(t, New(winStub(strings.Join([]string{"A", "HKCU", exe}, "\t")), Options{GOOS: "windows"}), a)
		if !found || !strings.Contains(f.Where, exe) {
			t.Errorf("%s: %v %+v", id, found, f)
		}
	}
}

package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/PiluVitu/toolkit/internal/exec"
	"github.com/PiluVitu/toolkit/internal/exec/exectest"
)

const statusGit = `dpkg-query -W "-f=${db:Status-Status}\t${Version}\n" git`

func aptFake(t *testing.T) *exectest.FakeRunner {
	t.Helper()
	return exectest.New(t).WithPath("apt-get", "/usr/bin/apt-get").WithPath("apt-cache", "/usr/bin/apt-cache").WithPath("dpkg-query", "/usr/bin/dpkg-query")
}

func notInstalled() exectest.Reply { return exectest.Reply{Result: exec.Result{ExitCode: 1}} }

func runInstallCmd(t *testing.T, f *exectest.FakeRunner, goos string, args ...string) (out, errOut string, err error) {
	t.Helper()
	var o, e bytes.Buffer
	deps := Deps{GOOS: goos, EmbeddedYML: []byte(embedded), NewRunner: func() exec.Runner { return f }, CheckElevation: func(exec.Cmd) error { return nil }}
	cmd := NewRootCmdWith(&o, &e, deps)
	cmd.SetArgs(append([]string{"install"}, args...))
	err = cmd.ExecuteContext(context.Background())
	return o.String(), e.String(), err
}

func onlyReads(t *testing.T, f *exectest.FakeRunner) {
	t.Helper()
	for _, c := range f.Calls() {
		if !c.ReadOnly || c.Elevate {
			t.Errorf("o dry-run deixou passar uma mutação: %s", c.String())
		}
	}
}

func TestInstallDryRunMostraOPlanoESoLe(t *testing.T) {
	f := aptFake(t).On(statusGit, notInstalled())
	out, _, err := runInstallCmd(t, f, "linux", "git", "--dry-run")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"==> git (Git)", "seria instalado com apt (simulado)", "Plano (dry-run", "sudo -n apt-get update", "sudo -n apt-get install -y --no-remove git"} {
		if !strings.Contains(out, want) {
			t.Errorf("saída sem %q:\n%s", want, out)
		}
	}
	onlyReads(t, f)
	if len(f.Calls()) == 0 {
		t.Error("o dry-run executa a leitura real do estado")
	}
}

func TestInstallJaInstaladoNaoPlanejaNada(t *testing.T) {
	f := aptFake(t).On(statusGit, exectest.Exit(0, "installed\t1:2.43.0-1\n"))
	out, _, err := runInstallCmd(t, f, "linux", "git", "--dry-run")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "já instalado (apt, versão 1:2.43.0-1)") || !strings.Contains(out, "Nada a fazer.") {
		t.Errorf("saída:\n%s", out)
	}
	onlyReads(t, f)
}

func TestInstallDeVerdadeExecutaPeloRunner(t *testing.T) {
	f := aptFake(t).On(statusGit, notInstalled()).
		OnElevated("apt-get update", exectest.Exit(0, "")).
		OnElevated("apt-get install -y --no-remove git", exectest.Exit(0, ""))
	out, _, err := runInstallCmd(t, f, "linux", "git")
	if err != nil || !strings.Contains(out, "instalado com apt") || strings.Contains(out, "Plano") {
		t.Fatalf("err=%v out:\n%s", err, out)
	}
	var elevated int
	for _, c := range f.Calls() {
		if c.Elevate {
			elevated++
		}
	}
	if elevated != 2 {
		t.Errorf("update + install elevados: %d", elevated)
	}
}

// Crivo #10/#11: "toolkit install python@3.12" — a versão já está no id e não vai de novo ao provider.
func TestInstallPythonVersaoNoTemplate(t *testing.T) {
	f := aptFake(t).On(`dpkg-query -W "-f=${db:Status-Status}\t${Version}\n" python3.12`, notInstalled())
	out, _, err := runInstallCmd(t, f, "linux", "python@3.12", "--dry-run")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "apt-get install -y --no-remove python3.12") || strings.Contains(out, "python3.12=") {
		t.Errorf("saída:\n%s", out)
	}
	onlyReads(t, f)
	// padrão = a primeira da lista
	f = aptFake(t).On(`dpkg-query -W "-f=${db:Status-Status}\t${Version}\n" python3.13`, notInstalled())
	out, _, err = runInstallCmd(t, f, "linux", "python", "--dry-run")
	if err != nil || !strings.Contains(out, "python3.13") {
		t.Errorf("err=%v out:\n%s", err, out)
	}
}

func TestInstallVersaoForaDaListaEmAppSemVersoes(t *testing.T) {
	f := aptFake(t)
	for _, tc := range []struct{ arg, want string }{
		{"python@3.9", "fora da lista"},
		{"firefox@1", "não aceita escolha de versão"},
		{"nada", "não está no catálogo"},
		{"git@", "inválido"},
		{"@1", "inválido"},
	} {
		_, _, err := runInstallCmd(t, f, "linux", tc.arg, "--dry-run")
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err = %v, queria %q", tc.arg, err, tc.want)
		}
	}
	if len(f.Calls()) != 0 {
		t.Errorf("nada executado em erro de argumento: %d chamadas", len(f.Calls()))
	}
}

func TestInstallVersaoDoGerenciadorComEpoca(t *testing.T) {
	f := aptFake(t).On(statusGit, notInstalled())
	out, _, err := runInstallCmd(t, f, "linux", "git@1:2.43.0-1ubuntu7", "--dry-run")
	if err != nil || !strings.Contains(out, "apt-get install -y --no-remove git=1:2.43.0-1ubuntu7") {
		t.Fatalf("err=%v out:\n%s", err, out)
	}
	onlyReads(t, f)
	f = aptFake(t)
	if _, _, err := runInstallCmd(t, f, "linux", "git@--allow-downgrades", "--dry-run"); err == nil {
		t.Error("versão hostil devia falhar")
	}
	if len(f.Calls()) != 0 {
		t.Errorf("versão hostil não pode chegar ao provider: %v", f.Calls())
	}
}

func TestInstallSemGerenciadorDisponivel(t *testing.T) {
	f := exectest.New(t) // nenhum gerenciador no PATH
	_, _, err := runInstallCmd(t, f, "linux", "git", "--dry-run")
	if err == nil || !strings.Contains(err.Error(), "falhou: git") {
		t.Errorf("err = %v", err)
	}
}

func TestInstallVariosAppsContinuaDepoisDeUmaFalha(t *testing.T) {
	f := aptFake(t).On(statusGit, exectest.Exit(0, "installed\t1.0\n")) // firefox: sem flatpak
	out, errOut, err := runInstallCmd(t, f, "linux", "firefox", "git", "--dry-run")
	if err == nil || !strings.Contains(err.Error(), "falhou: firefox") {
		t.Errorf("err = %v", err)
	}
	if !strings.Contains(out, "já instalado (apt") || !strings.Contains(errOut, "firefox") {
		t.Errorf("out:\n%s\nerr:\n%s", out, errOut)
	}
}

func TestInstallCatalogoLocalHostilNaoExecutaNada(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "evil.yaml")
	src := "apps:\n  evil:\n    name: Evil\n    category: dev\n    linux:\n      - { provider: apt, package: \"-o APT::Update::Pre-Invoke::=sh\" }\n      - { provider: dnf, package: \"--nogpgcheck\" }\n      - { provider: pacman, package: \"-U\" }\n"
	if err := os.WriteFile(path, []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	f := aptFake(t).WithPath("dnf", "/x").WithPath("rpm", "/x").WithPath("pacman", "/x")
	_, _, err := runInstallCmd(t, f, "linux", "evil", "--catalog", path)
	if err == nil {
		t.Fatal("catálogo hostil devia falhar")
	}
	if len(f.Calls()) != 0 {
		t.Errorf("nenhum comando pode rodar com id hostil: %v", f.Calls())
	}
}

func TestInstallFlatpakAvisaDeRuntimes(t *testing.T) {
	f := exectest.New(t).WithPath("flatpak", "/usr/bin/flatpak").
		On("flatpak list --app --columns=application,version", exectest.Exit(0, ""))
	out, _, err := runInstallCmd(t, f, "linux", "firefox", "--dry-run")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "podem ser GB") || !strings.Contains(out, "flatpak install --system -y --noninteractive flathub org.mozilla.firefox") {
		t.Errorf("saída:\n%s", out)
	}
	onlyReads(t, f)
}

func TestInstallSudoSemCredencialDaDica(t *testing.T) {
	f := aptFake(t).On(statusGit, notInstalled()).
		OnElevated("apt-get update", exectest.Reply{Err: exec.ErrNotElevated})
	_, errOut, err := runInstallCmd(t, f, "linux", "git")
	if err == nil || !strings.Contains(errOut, "sudo -v") {
		t.Errorf("err=%v errOut:\n%s", err, errOut)
	}
}

func TestInstallDryRunNoWindowsMarcaPassoQueExigeAdmin(t *testing.T) {
	f := exectest.New(t).WithPath("winget", `C:\winget.exe`).
		On("winget list --id Git.Git -e --disable-interactivity", exectest.Exit(int(uint32(0x8A150014)), ""))
	var o, e bytes.Buffer
	deps := Deps{GOOS: "windows", EmbeddedYML: []byte(embedded), NewRunner: func() exec.Runner { return f }, CheckElevation: func(exec.Cmd) error { return nil }}
	cmd := NewRootCmdWith(&o, &e, deps)
	cmd.SetArgs([]string{"install", "git", "--dry-run"})
	if err := cmd.ExecuteContext(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(o.String(), "winget install --id Git.Git -e --source winget --silent") {
		t.Errorf("saída:\n%s", o.String())
	}
	onlyReads(t, f)
}

// Advisor: app com lista e um instalador SEM template; sem "@", a versão padrão não pode virar pkg=3.13.
func TestInstallListaSemVersaoPedidaNaoFixaInstaladorSemTemplate(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "c.yaml")
	src := "apps:\n  tool:\n    name: Tool\n    category: dev\n    versions: [\"2\", \"1\"]\n    linux:\n      - { provider: flatpak, id: org.example.Tool }\n      - { provider: apt, package: tool }\n"
	if err := os.WriteFile(path, []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	f := aptFake(t).On(`dpkg-query -W "-f=${db:Status-Status}\t${Version}\n" tool`, notInstalled()).WithPath("flatpak", "/x").
		On("flatpak list --app --columns=application,version", exectest.Exit(0, ""))
	out, _, err := runInstallCmd(t, f, "linux", "tool", "--dry-run", "--catalog", path)
	if err != nil {
		t.Fatalf("err = %v\nout:\n%s", err, out)
	}
	if strings.Contains(out, "tool=") || strings.Contains(out, "ErrVersionUnsupported") || strings.Contains(out, "só instala a versão atual") {
		t.Errorf("a versão padrão foi fixada sem pedido:\n%s", out)
	}
	if !strings.Contains(out, "flatpak install --system") {
		t.Errorf("o primeiro instalador (flatpak) devia servir sem versão:\n%s", out)
	}
	onlyReads(t, f)
	// com versão pedida, o flatpak recusa e o apt (sem template) recebe tool=1
	f = aptFake(t).On(`dpkg-query -W "-f=${db:Status-Status}\t${Version}\n" tool`, notInstalled()).WithPath("flatpak", "/x").
		On("flatpak list --app --columns=application,version", exectest.Exit(0, ""))
	out, _, err = runInstallCmd(t, f, "linux", "tool@1", "--dry-run", "--catalog", path)
	if err != nil || !strings.Contains(out, "apt-get install -y --no-remove tool=1") {
		t.Errorf("err=%v out:\n%s", err, out)
	}
}

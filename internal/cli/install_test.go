package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/PiluTech/toolkit/internal/exec"
	"github.com/PiluTech/toolkit/internal/exec/exectest"
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
	deps := Deps{GOOS: goos, EmbeddedYML: []byte(embedded), NewRunner: func() exec.Runner { return f }, CheckElevation: func(exec.Cmd) error { return nil }, Interactive: func() bool { return false }}
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
	for _, want := range []string{"==> git (Git)", "seria instalado com apt (simulado)", "Plano (dry-run", "sudo -n -- env DEBIAN_FRONTEND=noninteractive LC_ALL=C apt-get update", "sudo -n -- env DEBIAN_FRONTEND=noninteractive LC_ALL=C apt-get install -y --no-remove git"} {
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
	out, _, err := runInstallCmd(t, f, "linux", "git", "--yes")
	if err != nil || !strings.Contains(out, "instalado com apt") || strings.Contains(out, "dry-run") {
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
	_, errOut, err := runInstallCmd(t, f, "linux", "git", "--yes")
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

// --- confirmação antes do install real (Crivo #15) ---

func mutations(f *exectest.FakeRunner) int {
	n := 0
	for _, c := range f.Calls() {
		if !c.ReadOnly {
			n++
		}
	}
	return n
}

func runInteractive(t *testing.T, f *exectest.FakeRunner, answer string, args ...string) (string, error) {
	t.Helper()
	var o, e bytes.Buffer
	deps := Deps{GOOS: "linux", EmbeddedYML: []byte(embedded), NewRunner: func() exec.Runner { return f }, CheckElevation: func(exec.Cmd) error { return nil },
		Interactive: func() bool { return true }, Stdin: strings.NewReader(answer)}
	cmd := NewRootCmdWith(&o, &e, deps)
	cmd.SetArgs(append([]string{"install"}, args...))
	err := cmd.ExecuteContext(context.Background())
	return o.String(), err
}

func gitNotInstalled(t *testing.T) *exectest.FakeRunner {
	return aptFake(t).On(statusGit, notInstalled()).
		OnElevated("apt-get update", exectest.Exit(0, "")).
		OnElevated("apt-get install -y --no-remove git", exectest.Exit(0, ""))
}

func TestInstallRealSemTerminalExigeYes(t *testing.T) {
	f := gitNotInstalled(t)
	out, _, err := runInstallCmd(t, f, "linux", "git")
	if err == nil || !strings.Contains(err.Error(), "--yes") {
		t.Fatalf("err = %v", err)
	}
	if !strings.Contains(out, "Plano") {
		t.Errorf("o plano aparece antes do erro:\n%s", out)
	}
	if mutations(f) != 0 {
		t.Error("nada pode ser executado sem confirmação")
	}
	// com --yes executa
	f = gitNotInstalled(t)
	if _, _, err := runInstallCmd(t, f, "linux", "git", "-y"); err != nil || mutations(f) != 2 {
		t.Errorf("err=%v mutações=%d", err, mutations(f))
	}
}

func TestInstallInterativoConfirmaOuCancela(t *testing.T) {
	for _, yes := range []string{"s\n", "sim\n", "Y\n"} {
		f := gitNotInstalled(t)
		out, err := runInteractive(t, f, yes, "git")
		if err != nil || mutations(f) != 2 || !strings.Contains(out, "Executar este plano? [s/N]") || !strings.Contains(out, "instalado com apt") {
			t.Errorf("%q: err=%v mutações=%d\n%s", yes, err, mutations(f), out)
		}
	}
	for _, no := range []string{"\n", "n\n", "nao\n", ""} {
		f := gitNotInstalled(t)
		out, err := runInteractive(t, f, no, "git")
		if err != nil || mutations(f) != 0 || !strings.Contains(out, "cancelado") {
			t.Errorf("%q: err=%v mutações=%d\n%s", no, err, mutations(f), out)
		}
	}
}

func TestInstallDryRunNuncaPergunta(t *testing.T) {
	f := gitNotInstalled(t)
	out, err := runInteractive(t, f, "s\n", "git", "--dry-run")
	if err != nil || strings.Contains(out, "Executar este plano") || mutations(f) != 0 {
		t.Errorf("err=%v mutações=%d\n%s", err, mutations(f), out)
	}
}

func TestInstallNadaAFazerNaoPerguntaNemExigeYes(t *testing.T) {
	f := aptFake(t).On(statusGit, exectest.Exit(0, "installed\t1.0\n"))
	out, _, err := runInstallCmd(t, f, "linux", "git") // sem terminal e sem --yes
	if err != nil || !strings.Contains(out, "já instalado") || !strings.Contains(out, "Nada a fazer.") {
		t.Errorf("err=%v\n%s", err, out)
	}
}

func TestInstallPlanoFalhoNaoExecutaNenhumApp(t *testing.T) {
	f := aptFake(t).On(statusGit, notInstalled()) // firefox: sem flatpak => falha no plano
	_, _, err := runInstallCmd(t, f, "linux", "git", "firefox", "--yes")
	if err == nil || !strings.Contains(err.Error(), "firefox") {
		t.Errorf("err = %v", err)
	}
	if mutations(f) != 0 {
		t.Error("com um app inviável no plano, nada é instalado (valida tudo antes)")
	}
}

// --- o plano mostra como será executado (Crivo #15) ---

func TestPlanoMostraSudoEnvEDoisHifens(t *testing.T) {
	f := aptFake(t).On(statusGit, notInstalled())
	out, _, err := runInstallCmd(t, f, "linux", "git", "--dry-run")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "sudo -n -- env DEBIAN_FRONTEND=noninteractive LC_ALL=C apt-get install") {
		t.Errorf("o plano devia mostrar o '--' e o Env:\n%s", out)
	}
	if !strings.Contains(out, "aviso: este plano usa sudo -n") || !strings.Contains(out, "sudo -v") {
		t.Errorf("dry-run no Unix devia avisar do sudo -n:\n%s", out)
	}
}

func TestPlanoSemSudoNaoAvisaEMostraEnvDoBrew(t *testing.T) {
	f := exectest.New(t).WithPath("brew", "/opt/homebrew/bin/brew").
		On("brew list --versions --formula git", exectest.Reply{Result: exec.Result{ExitCode: 1}})
	out, _, err := runInstallCmd(t, f, "darwin", "git", "--dry-run")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "usa sudo -n") {
		t.Errorf("brew não usa sudo:\n%s", out)
	}
	if !strings.Contains(out, "HOMEBREW_NO_AUTO_UPDATE=1 HOMEBREW_NO_ENV_HINTS=1 LC_ALL=C brew install --formula git") {
		t.Errorf("o plano devia mostrar o Env do brew:\n%s", out)
	}
}

// --- pendências do printPlan: o plano usa o mesmo GOOS/euid do runner real, e o install real não repete cabeçalhos ---

func runInstallAs(t *testing.T, euid int, args ...string) (out string, f *exectest.FakeRunner) {
	t.Helper()
	f = aptFake(t).On(statusGit, notInstalled())
	var o, e bytes.Buffer
	deps := Deps{GOOS: "linux", Euid: func() int { return euid }, EmbeddedYML: []byte(embedded),
		NewRunner: func() exec.Runner { return f }, CheckElevation: func(exec.Cmd) error { return nil }, Interactive: func() bool { return false }}
	cmd := NewRootCmdWith(&o, &e, deps)
	cmd.SetArgs(append([]string{"install"}, args...))
	if err := cmd.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("%v\n%s\n%s", err, o.String(), e.String())
	}
	return o.String(), f
}

// Como root o runner real NÃO usa sudo (OSRunner.argv); o plano não pode mostrar `sudo -n --` nem avisar dele.
func TestInstallPlanAsRootShowsNoSudo(t *testing.T) {
	out, _ := runInstallAs(t, 0, "git", "--dry-run")
	if strings.Contains(out, "sudo") {
		t.Fatalf("como root o plano não mostra sudo:\n%s", out)
	}
	if !strings.Contains(out, "DEBIAN_FRONTEND=noninteractive LC_ALL=C apt-get install -y --no-remove git") {
		t.Fatalf("o comando continua no plano (VAR=... cmd, sem sudo):\n%s", out)
	}
}

// Fora de root o plano continua mostrando sudo -n e o aviso.
func TestInstallPlanAsUserShowsSudo(t *testing.T) {
	out, _ := runInstallAs(t, 1000, "git", "--dry-run")
	if !strings.Contains(out, "sudo -n -- env DEBIAN_FRONTEND=noninteractive") || !strings.Contains(out, "sudo -v") {
		t.Fatalf("como usuário o plano mostra sudo -n e o aviso:\n%s", out)
	}
}

// O install real imprime o cabeçalho de cada app UMA vez (o passe do plano não repete o que o passe real mostra).
func TestInstallRealDoesNotRepeatAppHeaders(t *testing.T) {
	f := aptFake(t).On(statusGit, notInstalled())
	f.OnElevated("apt-get update", exectest.Exit(0, "")).OnElevated("apt-get install -y --no-remove git", exectest.Exit(0, ""))
	var o, e bytes.Buffer
	deps := Deps{GOOS: "linux", Euid: func() int { return 1000 }, EmbeddedYML: []byte(embedded),
		NewRunner: func() exec.Runner { return f }, CheckElevation: func(exec.Cmd) error { return nil }, Interactive: func() bool { return false }}
	cmd := NewRootCmdWith(&o, &e, deps)
	cmd.SetArgs([]string{"install", "git", "--yes"})
	if err := cmd.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("%v\n%s", err, o.String())
	}
	if n := strings.Count(o.String(), "==> git (Git)"); n != 1 {
		t.Fatalf("o cabeçalho do app saiu %d vezes:\n%s", n, o.String())
	}
}

// Passo elevado com Secret: o Display não mostra sudo (nem os Args), mas o aviso de sudo -n acende.
func TestPrintPlanWarnsAboutSudoForSecretSteps(t *testing.T) {
	f := exectest.New(t)
	dry := exec.NewDryRunRunner(f)
	dry.CheckElevation = func(exec.Cmd) error { return nil }
	if _, err := dry.Run(context.Background(), exec.Cmd{Name: "apt-get", Args: []string{"install", "x"}, Elevate: true, Secret: true}); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	printPlan(&out, Deps{GOOS: "linux", Euid: func() int { return 1000 }}, dry, true)
	if !strings.Contains(out.String(), "sudo -v") {
		t.Fatalf("o aviso de sudo -n deve acender para passo elevado com Secret:\n%s", out.String())
	}
	var asRoot bytes.Buffer
	printPlan(&asRoot, Deps{GOOS: "linux", Euid: func() int { return 0 }}, dry, true)
	if strings.Contains(asRoot.String(), "sudo") {
		t.Fatalf("como root não há aviso de sudo:\n%s", asRoot.String())
	}
}

// Com falha no plano do install real, o que o passe do plano escreveu (cabeçalho e "já instalado" dos outros
// apps) NÃO é descartado: o usuário vê o contexto do erro.
func TestInstallRealFailureKeepsPlanPassOutput(t *testing.T) {
	// git já instalado, python a instalar (plano NÃO vazio) e firefox sem flatpak na máquina: falha no plano
	f := aptFake(t).On(statusGit, exectest.Exit(0, "installed\t1:2.43.0-1\n")).
		On(`dpkg-query -W "-f=${db:Status-Status}\t${Version}\n" python3.13`, notInstalled())
	var o, e bytes.Buffer
	deps := Deps{GOOS: "linux", Euid: func() int { return 1000 }, EmbeddedYML: []byte(embedded),
		NewRunner: func() exec.Runner { return f }, CheckElevation: func(exec.Cmd) error { return nil }, Interactive: func() bool { return false }}
	cmd := NewRootCmdWith(&o, &e, deps)
	cmd.SetArgs([]string{"install", "git", "python", "firefox", "--yes"})
	if err := cmd.ExecuteContext(context.Background()); err == nil {
		t.Fatal("firefox sem flatpak deve falhar")
	}
	out := o.String()
	if !strings.Contains(out, "==> git (Git)") || !strings.Contains(out, "já instalado") || !strings.Contains(out, "==> python (Python)") || !strings.Contains(out, "==> firefox (Firefox)") {
		t.Fatalf("o contexto do plano (cabeçalhos e já-instalado) sumiu na falha:\n%s\n--- stderr:\n%s", out, e.String())
	}
}

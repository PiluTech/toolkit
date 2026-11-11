package winget

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/PiluVitu/toolkit/internal/exec"
	"github.com/PiluVitu/toolkit/internal/exec/exectest"
	"github.com/PiluVitu/toolkit/internal/providers"
)

func fixture(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func ref(id string) providers.PackageRef {
	return providers.PackageRef{Provider: "winget", ID: id}
}

func newP(f *exectest.FakeRunner) *Provider {
	p := New(f)
	p.Sleep = func(context.Context, time.Duration) error { return nil }
	return p
}

const (
	listLine    = "winget list --id Git.Git -e --accept-source-agreements --disable-interactivity"
	showLine    = "winget show --id Git.Git -e --versions --source winget --accept-source-agreements --disable-interactivity"
	installBase = "winget install --id Git.Git -e --source winget --silent --accept-package-agreements --accept-source-agreements --disable-interactivity"
)

// exitReply cria a resposta com o HRESULT como o Windows o entrega (positivo em int de 64 bits) ou negativo (int32).
func exitReply(code uint32, negative bool) exectest.Reply {
	c := int(code)
	if negative {
		c = int(int32(code))
	}
	return exectest.Exit(c, "")
}

func TestAvailable(t *testing.T) {
	if newP(exectest.New(t)).Available(context.Background()) {
		t.Error("sem winget no PATH")
	}
	if !newP(exectest.New(t).WithPath("winget", `C:\w\winget.exe`)).Available(context.Background()) {
		t.Error("com winget no PATH")
	}
}

func TestValidate(t *testing.T) {
	p := newP(exectest.New(t))
	ok := []providers.PackageRef{{Provider: "winget", ID: "Git.Git"}, {Provider: "winget", ID: "9NBLGGH4NNS1", Source: "msstore"}}
	bad := []providers.PackageRef{
		{Provider: "winget"},
		{Provider: "winget", ID: "-e"},
		{Provider: "winget", ID: "Git.Git --force"},
		{Provider: "winget", ID: "Git.Git", Source: "--source"},
		{Provider: "winget", ID: "Git.Git", Package: "git"},
		{Provider: "winget", ID: "Git.Git", Args: []string{"/q"}},
	}
	for _, r := range ok {
		if err := p.Validate(r); err != nil {
			t.Errorf("%+v: %v", r, err)
		}
	}
	for _, r := range bad {
		if err := p.Validate(r); !errors.Is(err, providers.ErrInvalidRef) {
			t.Errorf("%+v: err = %v", r, err)
		}
	}
}

func TestIsInstalledLeTabelaEmDoisIdiomas(t *testing.T) {
	for _, name := range []string{"list_en.txt", "list_ptbr.txt"} {
		f := exectest.New(t).On(listLine, exectest.Exit(0, fixture(t, name)))
		ok, ver, err := newP(f).IsInstalled(context.Background(), ref("Git.Git"))
		if err != nil || !ok || ver != "2.47.1" {
			t.Errorf("%s: ok=%v ver=%q err=%v", name, ok, ver, err)
		}
		c := f.Calls()[0]
		if !c.ReadOnly || c.Elevate {
			t.Errorf("%s: list deve ser ReadOnly e sem Elevate: %+v", name, c)
		}
	}
}

func TestIsInstalledVersaoTruncadaEIdExato(t *testing.T) {
	f := exectest.New(t).On(listLine, exectest.Exit(0, fixture(t, "list_multi.txt")))
	ok, ver, err := newP(f).IsInstalled(context.Background(), ref("Git.Git"))
	if err != nil || !ok || ver != "2.40.0" {
		t.Errorf("ok=%v ver=%q err=%v", ok, ver, err)
	}
	// "GitHub.GitLFS" começa igual mas não é o id pedido
	f2 := exectest.New(t).On(strings.Replace(listLine, "Git.Git", "Git", 1), exectest.Exit(0, fixture(t, "list_multi.txt")))
	if ok, _, _ := newP(f2).IsInstalled(context.Background(), ref("Git")); ok {
		t.Error("id parcial não pode casar")
	}
}

func TestIsInstalledNaoEncontrado(t *testing.T) {
	for _, neg := range []bool{false, true} {
		f := exectest.New(t).On(listLine, exitReply(codeNoApplicationsFound, neg))
		ok, _, err := newP(f).IsInstalled(context.Background(), ref("Git.Git"))
		if ok || err != nil {
			t.Errorf("neg=%v ok=%v err=%v", neg, ok, err)
		}
	}
}

func TestIsInstalledErroDesconhecido(t *testing.T) {
	f := exectest.New(t).On(listLine, exectest.Reply{Result: exec.Result{ExitCode: 1, Stderr: []byte("\x1b[2Jfalhou feio\n")}})
	_, _, err := newP(f).IsInstalled(context.Background(), ref("Git.Git"))
	if err == nil || strings.ContainsRune(err.Error(), 0x1b) || !strings.Contains(err.Error(), "falhou feio") {
		t.Errorf("err = %v", err)
	}
}

func TestListVersions(t *testing.T) {
	for _, name := range []string{"show_versions_en.txt", "show_versions_ptbr.txt"} {
		f := exectest.New(t).On(showLine, exectest.Exit(0, fixture(t, name)))
		vs, err := newP(f).ListVersions(context.Background(), ref("Git.Git"))
		if err != nil || len(vs) < 2 || vs[0] != "2.47.1" || vs[1] != "2.47.0" {
			t.Errorf("%s: vs=%v err=%v", name, vs, err)
		}
		if c := f.Calls()[0]; !c.ReadOnly || c.Elevate {
			t.Errorf("show deve ser ReadOnly: %+v", c)
		}
	}
	f := exectest.New(t).On(showLine, exitReply(codeNoApplicationsFound, false))
	if _, err := newP(f).ListVersions(context.Background(), ref("Git.Git")); !errors.Is(err, providers.ErrNotFound) {
		t.Errorf("err = %v", err)
	}
}

func TestInstallComandoEFlags(t *testing.T) {
	f := exectest.New(t).On(installBase, exectest.Exit(0, ""))
	res, err := newP(f).Install(context.Background(), ref("Git.Git"), "")
	if err != nil || res.AlreadyInstalled || res.RebootRequired {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	c := f.Calls()[0]
	if c.ReadOnly || c.Elevate || c.Name != "winget" {
		t.Errorf("install não é ReadOnly nem Elevate: %+v", c)
	}
	// versão fixada
	f = exectest.New(t).On(installBase+" --version 2.47.0", exectest.Exit(0, ""))
	res, err = newP(f).Install(context.Background(), ref("Git.Git"), "2.47.0")
	if err != nil || res.Version != "2.47.0" {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	// "latest" não leva --version
	f = exectest.New(t).On(installBase, exectest.Exit(0, ""))
	if _, err := newP(f).Install(context.Background(), ref("Git.Git"), "latest"); err != nil {
		t.Fatal(err)
	}
}

func TestInstallFonteDoRef(t *testing.T) {
	r := providers.PackageRef{Provider: "winget", ID: "9NBLGGH4NNS1", Source: "msstore"}
	want := "winget install --id 9NBLGGH4NNS1 -e --source msstore --silent --accept-package-agreements --accept-source-agreements --disable-interactivity"
	f := exectest.New(t).On(want, exectest.Exit(0, ""))
	if _, err := newP(f).Install(context.Background(), r, ""); err != nil {
		t.Fatal(err)
	}
}

func TestInstallRecusaVersaoEIdHostis(t *testing.T) {
	f := exectest.New(t) // qualquer comando falha o teste
	p := newP(f)
	if _, err := p.Install(context.Background(), ref("Git.Git"), "1.0 --override x"); !errors.Is(err, providers.ErrInvalidRef) {
		t.Errorf("versão hostil: %v", err)
	}
	if _, err := p.Install(context.Background(), ref("-e"), ""); !errors.Is(err, providers.ErrInvalidRef) {
		t.Errorf("id hostil: %v", err)
	}
	if len(f.Calls()) != 0 {
		t.Error("nada deve ser executado")
	}
}

func TestInstallTabelaDeExitCodes(t *testing.T) {
	cases := []struct {
		code    uint32
		version string
		want    providers.InstallResult
		wantErr error
	}{
		{codeUpdateNotApplicable, "", providers.InstallResult{AlreadyInstalled: true}, nil},
		{codePackageAlreadyInst, "", providers.InstallResult{AlreadyInstalled: true}, nil},
		{codeInstallAlreadyInst, "", providers.InstallResult{AlreadyInstalled: true}, nil},
		{codeRebootRequiredFinish, "", providers.InstallResult{RebootRequired: true}, nil},
		{codeRebootInitiated, "", providers.InstallResult{RebootRequired: true}, nil},
		{codeNoApplicationsFound, "", providers.InstallResult{}, providers.ErrNotFound},
		{codeNoApplicableInstaller, "", providers.InstallResult{}, providers.ErrNoApplicableInstaller},
		{codeHashMismatch, "", providers.InstallResult{}, providers.ErrHashMismatch},
		{codePackageInUse, "", providers.InstallResult{}, providers.ErrRetryable},
		{codeFileInUse, "", providers.InstallResult{}, providers.ErrRetryable},
		{codeRebootRequiredInstall, "", providers.InstallResult{}, providers.ErrRetryable},
	}
	for _, c := range cases {
		for _, neg := range []bool{false, true} {
			f := exectest.New(t).On(installBase, exitReply(c.code, neg))
			got, err := newP(f).Install(context.Background(), ref("Git.Git"), c.version)
			if c.wantErr != nil {
				if !errors.Is(err, c.wantErr) {
					t.Errorf("0x%08X neg=%v: err=%v, queria %v", c.code, neg, err, c.wantErr)
				}
				continue
			}
			if err != nil || !reflect.DeepEqual(got, c.want) {
				t.Errorf("0x%08X neg=%v: got=%+v err=%v", c.code, neg, got, err)
			}
		}
	}
}

func TestInstallErrosClarosSemTipo(t *testing.T) {
	for _, code := range []uint32{codeInstallDowngrade, codeBlockedByPolicy, codeMissingDependency, codeDiskFull, codeNoNetwork, 0x8A150999} {
		f := exectest.New(t).On(installBase, exitReply(code, false))
		_, err := newP(f).Install(context.Background(), ref("Git.Git"), "")
		if err == nil || !strings.Contains(err.Error(), "0x8A15") {
			t.Errorf("0x%08X: err = %v", code, err)
		}
	}
}

func TestInstallVersaoDiferenteJaInstalada(t *testing.T) {
	f := exectest.New(t).On(installBase+" --version 2.0.0", exitReply(codeInstallAlreadyInst, false))
	_, err := newP(f).Install(context.Background(), ref("Git.Git"), "2.0.0")
	if err == nil || !strings.Contains(err.Error(), "2.0.0") {
		t.Errorf("err = %v", err)
	}
}

func TestInstallEmAndamentoTentaTresVezes(t *testing.T) {
	f := exectest.New(t).On(installBase, exitReply(codeInstallInProgress, false), exitReply(codeInstallInProgress, false), exectest.Exit(0, ""))
	res, err := newP(f).Install(context.Background(), ref("Git.Git"), "")
	if err != nil || res.AlreadyInstalled {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	if n := len(f.Calls()); n != 3 {
		t.Errorf("tentativas = %d", n)
	}
	f = exectest.New(t).On(installBase, exitReply(codeInstallInProgress, false))
	_, err = newP(f).Install(context.Background(), ref("Git.Git"), "")
	if !errors.Is(err, providers.ErrRetryable) || len(f.Calls()) != 3 {
		t.Errorf("err=%v tentativas=%d", err, len(f.Calls()))
	}
}

func TestInstallHashMismatchNuncaRetenta(t *testing.T) {
	f := exectest.New(t).On(installBase, exitReply(codeHashMismatch, false))
	_, err := newP(f).Install(context.Background(), ref("Git.Git"), "")
	if !errors.Is(err, providers.ErrHashMismatch) || len(f.Calls()) != 1 {
		t.Errorf("err=%v chamadas=%d", err, len(f.Calls()))
	}
	for _, a := range f.Calls()[0].Args {
		if strings.Contains(a, "ignore-security-hash") {
			t.Error("nunca usar --ignore-security-hash")
		}
	}
}

func TestInstallErroDoRunnerEEsperaCancelada(t *testing.T) {
	boom := errors.New("boom")
	f := exectest.New(t).On(installBase, exectest.Reply{Err: boom})
	if _, err := newP(f).Install(context.Background(), ref("Git.Git"), ""); !errors.Is(err, boom) {
		t.Errorf("err = %v", err)
	}
	f = exectest.New(t).On(installBase, exitReply(codeInstallInProgress, false))
	p := newP(f)
	p.Sleep = func(context.Context, time.Duration) error { return context.Canceled }
	if _, err := p.Install(context.Background(), ref("Git.Git"), ""); !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v", err)
	}
}

func TestParseTableSemTabela(t *testing.T) {
	if ParseTable([]byte("Nenhum pacote instalado corresponde.\n")) != nil {
		t.Error("sem linha de traços não há tabela")
	}
	if ParseTable(nil) != nil {
		t.Error("vazio")
	}
}

var _ providers.Provider = (*Provider)(nil)

func TestSupportsVersionSempreAceita(t *testing.T) {
	if err := newP(exectest.New(t)).SupportsVersion(ref("Git.Git"), "2.47.0"); err != nil {
		t.Error(err)
	}
}

// Leitura e mutação passam por Runners separados: o Cmd de cada operação tem a marca certa.
func TestCmdsTemAMarcaCertaParaOsRunners(t *testing.T) {
	r := ref("Git.Git")
	for name, c := range map[string]exec.Cmd{"list": listCmd(r), "show": showVersionsCmd(r)} {
		if !c.ReadOnly || c.Elevate {
			t.Errorf("%s deve ser ReadOnly sem Elevate", name)
		}
	}
	if c := installCmd(r, ""); c.ReadOnly || c.Elevate {
		t.Error("install não é ReadOnly nem Elevate")
	}
}

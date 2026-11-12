package providers

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func finder(found bool, err error, calls *int) ExternalFinder {
	return func(context.Context) (External, bool, error) {
		*calls++
		return External{Where: "/Applications/Discord.app", Version: "0.0.350"}, found, err
	}
}

func TestInstallAppExternoPulaSemPrepararNemInstalar(t *testing.T) {
	p := &fakeProvider{name: "brew"}
	n := 0
	out, err := InstallAppWith(context.Background(), []Candidate{cand(p)}, "", Options{External: finder(true, nil, &n)})
	if err != nil {
		t.Fatal(err)
	}
	if out.External == nil || out.External.Manager != "Homebrew" || out.External.Where != "/Applications/Discord.app" || out.External.Version != "0.0.350" {
		t.Fatalf("external=%+v", out.External)
	}
	if out.Result.AlreadyInstalled || out.Provider != "brew" {
		t.Fatalf("out=%+v", out)
	}
	if strings.Join(p.calls, ",") != "is" || p.prepared != 0 {
		t.Fatalf("não pode preparar nem instalar: calls=%v", p.calls)
	}
}

func TestInstallAppInstaladoPeloGerenciadorNaoConsultaExterno(t *testing.T) {
	p := &fakeProvider{name: "brew", installed: true, have: "1.0"}
	n := 0
	out, err := InstallAppWith(context.Background(), []Candidate{cand(p)}, "", Options{External: finder(true, nil, &n)})
	if err != nil || !out.Result.AlreadyInstalled || out.External != nil || n != 0 {
		t.Fatalf("out=%+v err=%v consultas=%d", out, err, n)
	}
}

func TestInstallAppSemExternoInstalaNormalmenteEConsultaUmaVez(t *testing.T) {
	a := &fakeProvider{name: "winget", installEr: ErrNoApplicableInstaller}
	b := &fakeProvider{name: "brew"}
	n := 0
	out, err := InstallAppWith(context.Background(), []Candidate{cand(a), cand(b)}, "", Options{External: finder(false, nil, &n)})
	if err != nil || out.External != nil || out.Provider != "brew" || n != 1 {
		t.Fatalf("out=%+v err=%v consultas=%d", out, err, n)
	}
}

func TestInstallAppErroDoDetectorEncerra(t *testing.T) {
	p := &fakeProvider{name: "brew"}
	n := 0
	boom := errors.New("cancelado")
	_, err := InstallAppWith(context.Background(), []Candidate{cand(p)}, "", Options{External: finder(false, boom, &n)})
	if !errors.Is(err, boom) {
		t.Fatalf("err=%v", err)
	}
	if strings.Contains(strings.Join(p.calls, ","), "install") {
		t.Fatalf("não pode instalar depois do erro: %v", p.calls)
	}
}

func TestInstallAppSemOptionsEhOComportamentoAntigo(t *testing.T) {
	p := &fakeProvider{name: "brew"}
	out, err := InstallApp(context.Background(), []Candidate{cand(p)}, "")
	if err != nil || out.External != nil || out.Provider != "brew" {
		t.Fatalf("out=%+v err=%v", out, err)
	}
}

// O gerenciador tem o app em outra versão que a pedida: o .app que existe é DELE; nunca vira "fora".
func TestInstallAppInstaladoPeloGerenciadorEmOutraVersaoNaoConsultaExterno(t *testing.T) {
	p := &fakeProvider{name: "brew", installed: true, have: "1.0"}
	n := 0
	out, err := InstallAppWith(context.Background(), []Candidate{cand(p)}, "2.0", Options{External: finder(true, nil, &n)})
	if err != nil {
		t.Fatal(err)
	}
	if out.External != nil || n != 0 {
		t.Fatalf("external=%+v consultas=%d: app do gerenciador não pode virar fora do gerenciador", out.External, n)
	}
}

// Versão pedida diferente (ou desconhecida) da que está fora do gerenciador: não dizer "nada a fazer".
func TestInstallAppExternoEmOutraVersaoQueAPedidaEhErro(t *testing.T) {
	for _, ver := range []string{"0.0.351", "1.2.3"} {
		p := &fakeProvider{name: "brew"}
		n := 0
		out, err := InstallAppWith(context.Background(), []Candidate{cand(p)}, ver, Options{External: finder(true, nil, &n)})
		if !errors.Is(err, ErrExternalVersion) || out.External != nil {
			t.Fatalf("pedida %s: out=%+v err=%v", ver, out, err)
		}
		if strings.Contains(strings.Join(p.calls, ","), "install") {
			t.Fatalf("não pode instalar por cima: %v", p.calls)
		}
	}
	for _, ver := range []string{"0.0.350", "latest"} {
		p := &fakeProvider{name: "brew"}
		n := 0
		out, err := InstallAppWith(context.Background(), []Candidate{cand(p)}, ver, Options{External: finder(true, nil, &n)})
		if err != nil || out.External == nil {
			t.Fatalf("pedida %s: out=%+v err=%v", ver, out, err)
		}
	}
	p := &fakeProvider{name: "brew"}
	noVer := func(context.Context) (External, bool, error) {
		return External{Where: "/Applications/Discord.app"}, true, nil
	}
	if _, err := InstallAppWith(context.Background(), []Candidate{cand(p)}, "1.0", Options{External: noVer}); !errors.Is(err, ErrExternalVersion) {
		t.Fatalf("err=%v", err)
	}
}

func TestDisplayName(t *testing.T) {
	for in, want := range map[string]string{"brew": "Homebrew", "winget": "winget", "flatpak": "Flatpak", "url": "toolkit", "github-release": "toolkit", "xyz": "xyz"} {
		if got := DisplayName(in); got != want {
			t.Errorf("%s: %q", in, got)
		}
	}
}

// --force: achou o app fora do gerenciador? Instala mesmo assim e leva o que achou para o aviso.
func TestInstallAppForceExternalInstallsAnywayAndReportsWhatWasFound(t *testing.T) {
	p := &fakeProvider{name: "brew"}
	n := 0
	out, err := InstallAppWith(context.Background(), []Candidate{cand(p)}, "", Options{External: finder(true, nil, &n), ForceExternal: true})
	if err != nil {
		t.Fatal(err)
	}
	if out.External != nil {
		t.Fatalf("com --force não é 'já instalado': %+v", out.External)
	}
	if out.ExternalIgnored == nil || out.ExternalIgnored.Manager != "Homebrew" || out.ExternalIgnored.Where != "/Applications/Discord.app" {
		t.Fatalf("ExternalIgnored = %+v", out.ExternalIgnored)
	}
	if !strings.Contains(strings.Join(p.calls, ","), "install") || n != 1 {
		t.Fatalf("instalou e consultou o externo uma vez: calls=%v n=%d", p.calls, n)
	}
	// nada achado + --force: instala normal, sem aviso
	p2 := &fakeProvider{name: "brew"}
	n = 0
	out, err = InstallAppWith(context.Background(), []Candidate{cand(p2)}, "", Options{External: finder(false, nil, &n), ForceExternal: true})
	if err != nil || out.ExternalIgnored != nil || out.External != nil {
		t.Fatalf("%v %+v", err, out)
	}
	// versão pedida + externo em outra versão + --force: segue para instalar (a recusa ErrExternalVersion só vale sem --force)
	p3 := &fakeProvider{name: "brew"}
	n = 0
	if _, err := InstallAppWith(context.Background(), []Candidate{cand(p3)}, "9.9", Options{External: finder(true, nil, &n), ForceExternal: true}); errors.Is(err, ErrExternalVersion) {
		t.Fatalf("--force não recusa por versão: %v", err)
	}
}

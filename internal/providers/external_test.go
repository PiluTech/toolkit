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

func TestDisplayName(t *testing.T) {
	for in, want := range map[string]string{"brew": "Homebrew", "winget": "winget", "flatpak": "Flatpak", "url": "toolkit", "github-release": "toolkit", "xyz": "xyz"} {
		if got := DisplayName(in); got != want {
			t.Errorf("%s: %q", in, got)
		}
	}
}

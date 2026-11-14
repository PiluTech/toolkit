package cli

import (
	"context"
	"strings"
	"testing"

	toolkit "github.com/PiluTech/toolkit"
	"github.com/PiluTech/toolkit/internal/exec"
	"github.com/PiluTech/toolkit/internal/tui"
)

func rustdeskDeps(goos string, f *leitorFalso, interactive bool, in string) (Deps, *strings.Builder, *strings.Builder) {
	var o, e strings.Builder
	d := Deps{GOOS: goos, EmbeddedYML: toolkit.CatalogYAML, NewRunner: func() exec.Runner { return f }, CheckElevation: func(exec.Cmd) error { return nil },
		Interactive: func() bool { return interactive }, Stdin: strings.NewReader(in)}
	return d, &o, &e
}

// PR13: o plano do install mostra o aviso de segurança do RustDesk (versão mínima, senha, servidor próprio) e
// nunca executa mutação; no Windows não há instalador (o id do winget não foi verificado).
func TestInstallDryRunRustDesk(t *testing.T) {
	casos := []struct{ goos, quer string }{
		{"darwin", "brew install --cask rustdesk"},
		{"linux", "flatpak install --system -y --noninteractive flathub com.rustdesk.RustDesk"},
	}
	for _, c := range casos {
		t.Run(c.goos, func(t *testing.T) {
			f := &leitorFalso{}
			d, o, e := rustdeskDeps(c.goos, f, false, "")
			cmd := NewRootCmdWith(o, e, d)
			cmd.SetArgs([]string{"install", "rustdesk", "--dry-run"})
			if err := cmd.ExecuteContext(context.Background()); err != nil {
				t.Fatalf("%v\n%s%s", err, o.String(), e.String())
			}
			out := o.String()
			if !strings.Contains(out, c.quer) {
				t.Errorf("plano sem %q:\n%s", c.quer, out)
			}
			for _, quer := range []string{"aviso (rustdesk):", "senha forte", "servidor próprio", "1.5.0", "2026-11-14"} {
				if !strings.Contains(out, quer) {
					t.Errorf("sem %q no plano:\n%s", quer, out)
				}
			}
			for _, call := range f.calls {
				if !call.ReadOnly || call.Elevate {
					t.Errorf("o dry-run deixou passar uma mutação: %s", call.String())
				}
			}
		})
	}
}

func TestInstallRustDeskSemInstaladorNoWindows(t *testing.T) {
	f := &leitorFalso{}
	d, o, e := rustdeskDeps("windows", f, false, "")
	cmd := NewRootCmdWith(o, e, d)
	cmd.SetArgs([]string{"install", "rustdesk", "--dry-run"})
	if err := cmd.ExecuteContext(context.Background()); err == nil {
		t.Fatalf("rustdesk no Windows não tem instalador verificado, esperava erro:\n%s", o.String())
	}
	if strings.Contains(o.String(), "winget install") {
		t.Errorf("não inventa id de winget:\n%s", o.String())
	}
}

// O aviso aparece ANTES da confirmação no install real (o passe do plano é descartado, mas o aviso não).
func TestInstallRealMostraAvisoAntesDeConfirmar(t *testing.T) {
	f := &leitorFalso{}
	d, o, e := rustdeskDeps("darwin", f, true, "n\n")
	cmd := NewRootCmdWith(o, e, d)
	cmd.SetArgs([]string{"install", "rustdesk"})
	if err := cmd.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("%v\n%s%s", err, o.String(), e.String())
	}
	out := o.String()
	iAviso, iPergunta := strings.Index(out, "aviso (rustdesk):"), strings.Index(out, "Executar este plano?")
	if iAviso < 0 || iPergunta < 0 || iAviso > iPergunta {
		t.Fatalf("o aviso deve vir antes da pergunta:\n%s", out)
	}
	if strings.Count(out, "aviso (rustdesk):") != 1 {
		t.Errorf("o aviso deve aparecer uma vez:\n%s", out)
	}
	for _, call := range f.calls {
		if !call.ReadOnly {
			t.Errorf("cancelado: nada pode ser executado: %s", call.String())
		}
	}
}

// O aviso vem do catálogo: sai limpo (ESC e bidi nunca chegam ao terminal) mesmo num catálogo local mais permissivo no futuro.
func TestPlannerAppComNoteVaiParaAvisos(t *testing.T) {
	e := newTUIEnv(t)
	e.run.On(statusGit, notInstalled())
	d := e.deps()
	d.EmbeddedYML = []byte(strings.Replace(embedded, "  git:\n    name: Git\n", "  git:\n    name: Git\n    note: \"Cuidado com este app.\"\n", 1))
	plan, err := (&tuiPlanner{deps: d, rd: e.rd()}).Plan(context.Background(), tuiSelGit())
	if err != nil {
		t.Fatal(err)
	}
	if !contains(plan.Warnings, "aviso (git): Cuidado com este app.") {
		t.Fatalf("avisos: %v", plan.Warnings)
	}
	if containsSub(plan.Changes, "Cuidado com este app.") {
		t.Fatalf("o aviso não é 'o que muda': %v", plan.Changes)
	}
}

func tuiSelGit() tui.Selection { return tui.Selection{Apps: []tui.Mark{gitMark()}} }

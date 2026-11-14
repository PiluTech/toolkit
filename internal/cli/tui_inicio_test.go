package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/PiluTech/toolkit/internal/exec"
	"github.com/PiluTech/toolkit/internal/tui"
)

// inicioDe abre a TUI com um espião e devolve o Início montado e as áreas.
func inicioDe(t *testing.T, goos string, admin bool) (*tui.InicioTab, []tui.AreaDef) {
	t.Helper()
	var areas []tui.AreaDef
	var o, e bytes.Buffer
	deps := Deps{
		GOOS: goos, EmbeddedYML: []byte(embedded), Interactive: func() bool { return true },
		Diag:        DiagDeps{IsAdmin: func(context.Context, string, exec.Runner) bool { return admin }},
		UpdateCheck: UpdateCheckDeps{Env: func(string) string { return "" }},
		TUI: TUIDeps{
			StdoutIsTerminal: func() bool { return true },
			Term:             func() string { return "xterm" },
			Run: func(_ context.Context, _ tui.Options, ts ...tui.AreaDef) error {
				areas = ts
				return nil
			},
		},
	}
	cmd := NewRootCmdWith(&o, &e, deps)
	cmd.SetArgs([]string{"tui"})
	if err := cmd.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("%v\n%s", err, e.String())
	}
	ini, ok := areas[0].Tabs[0].(*tui.InicioTab)
	if !ok {
		t.Fatalf("a área 0 deve ter o InicioTab, tem %T", areas[0].Tabs[0])
	}
	return ini, areas
}

func TestInicioRealApontaParaAsSecoesCertas(t *testing.T) {
	ini, areas := inicioDe(t, "linux", false)
	items := ini.Items()
	want := []string{"Apps", "Receitas", "Diagnóstico", "Apps nativos", "Privacidade", "Análise", "Execuções"}
	if len(items) != len(want) {
		t.Fatalf("%d objetivos, queria %d", len(items), len(want))
	}
	for i, it := range items {
		if it.Area < 1 || it.Area >= len(areas) || it.Section >= len(areas[it.Area].Tabs) {
			t.Fatalf("%q: destino fora das áreas (%d/%d)", it.Title, it.Area, it.Section)
		}
		if got := areas[it.Area].Tabs[it.Section].Title(); got != want[i] {
			t.Errorf("%q abre %q, queria %q", it.Title, got, want[i])
		}
		if !strings.Contains(it.Dest, want[i]) || it.Title == "" || it.Desc == "" {
			t.Errorf("item %d incompleto: %+v", i, it)
		}
	}
	// a ordem do pedido: Apps e Receitas na área 1, Diagnóstico e Apps nativos na 2, Privacidade 3, Análise 4, Execuções 5
	for i, a := range []int{1, 1, 2, 2, 3, 4, 5} {
		if items[i].Area != a {
			t.Errorf("item %d: área %d, queria %d", i, items[i].Area, a)
		}
	}
	// só "Analisar a rede" ainda não existe (campo explícito, não a etiqueta)
	for i, it := range items {
		if it.Unavailable != (i == 5) {
			t.Errorf("item %d (%q): Unavailable=%v", i, it.Title, it.Unavailable)
		}
	}
	if items[0].Title != "Instalar programas" || items[1].Title != "Preparar a máquina com uma receita" || items[2].Title != "Ver a saúde da máquina" {
		t.Errorf("títulos: %q | %q | %q", items[0].Title, items[1].Title, items[2].Title)
	}
}

func TestInicioRealEtiquetasPorSOEAdmin(t *testing.T) {
	tags := func(goos string, admin bool) []string {
		ini, _ := inicioDe(t, goos, admin)
		var out []string
		for _, it := range ini.Items() {
			out = append(out, it.Tag)
		}
		return out
	}
	join := func(s []string) string { return strings.Join(s, "|") }
	if got, want := join(tags("linux", true)), "|||! só Windows|! só Windows|○ em breve|"; got != want {
		t.Errorf("linux: %q", got)
	}
	if got, want := join(tags("darwin", false)), "|||! só Windows|! só Windows|○ em breve|"; got != want {
		t.Errorf("macOS: %q", got)
	}
	if got, want := join(tags("windows", false)), "||||! pede administrador para alguns itens|○ em breve|"; got != want {
		t.Errorf("Windows sem admin: %q", got)
	}
	if got, want := join(tags("windows", true)), "|||||○ em breve|"; got != want {
		t.Errorf("Windows admin: %q", got)
	}
}

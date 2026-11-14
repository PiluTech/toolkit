package tui

import (
	"fmt"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

var monoTheme = Theme{Mono: true, NoMotion: true}

func sampleConfirm() *Confirm {
	c := NewConfirm("Aplicar", "")
	c.Title = "Aplicar o perfil recomendado?"
	c.Summary = "Muda 3 ajustes de privacidade do Windows."
	c.Backup = "o valor original de cada item é guardado em backup."
	c.Undo = "a tecla d restaura o backup."
	return c
}

func TestConfirmFocoInicialEmCancelar(t *testing.T) {
	c := sampleConfirm()
	if c.Focus != 0 || !c.isCancel(c.Focus) {
		t.Fatalf("foco inicial %d", c.Focus)
	}
	if r := c.Update("enter"); r != ConfirmCancel {
		t.Fatalf("Enter no foco inicial = %v, quero cancelar", r)
	}
}

func TestConfirmTeclasDeFoco(t *testing.T) {
	for _, k := range []string{"tab", "right", "l"} {
		c := sampleConfirm()
		if r := c.Update(k); r != ConfirmNone || c.Focus != 1 {
			t.Errorf("%s: r=%v focus=%d", k, r, c.Focus)
		}
		if r := c.Update("enter"); r != ConfirmAccept {
			t.Errorf("%s+Enter = %v", k, r)
		}
	}
	for _, k := range []string{"shift+tab", "left", "h"} {
		c := sampleConfirm()
		c.Update("tab")
		c.Update(k)
		if c.Focus != 0 {
			t.Errorf("%s: focus=%d", k, c.Focus)
		}
	}
	c := sampleConfirm() // dá a volta
	c.Update("tab")
	c.Update("tab")
	if c.Focus != 0 {
		t.Errorf("volta ao início: %d", c.Focus)
	}
}

func TestConfirmSYNEscape(t *testing.T) {
	for k, want := range map[string]ConfirmResult{
		"s": ConfirmAccept, "S": ConfirmAccept, "y": ConfirmAccept, "Y": ConfirmAccept,
		"n": ConfirmCancel, "N": ConfirmCancel, "esc": ConfirmCancel, "escape": ConfirmCancel,
	} {
		if got := sampleConfirm().Update(k); got != want {
			t.Errorf("%q = %v, quero %v", k, got, want)
		}
	}
}

func TestConfirmEscNuncaConfirmaComFocoNoAplicar(t *testing.T) {
	c := sampleConfirm()
	c.Update("tab")
	if r := c.Update("esc"); r != ConfirmCancel {
		t.Fatalf("Esc com foco em Aplicar = %v", r)
	}
}

func TestConfirmTeclaAleatoriaNaoConfirma(t *testing.T) {
	for _, k := range []string{"x", "q", "1", "space", " ", "a", "r", "p", "ctrl+x", "f5", "backspace", "home", ""} {
		c := sampleConfirm()
		if r := c.Update(k); r != ConfirmNone {
			t.Errorf("%q = %v", k, r)
		}
		c.Update("tab") // mesmo com o foco em Aplicar
		if r := c.Update(k); r == ConfirmAccept {
			t.Errorf("%q confirmou com foco em Aplicar", k)
		}
	}
}

func TestConfirmBotaoDesabilitadoNaoAtiva(t *testing.T) {
	c := NewConfirm("Aplicar", "sem administrador")
	for _, k := range []string{"s", "y", "S", "Y"} {
		if r := c.Update(k); r != ConfirmNone {
			t.Errorf("%q = %v com botão indisponível", k, r)
		}
	}
	c.Update("tab")
	c.Update("right")
	if c.Focus != 0 {
		t.Fatalf("o foco foi a um botão indisponível: %d", c.Focus)
	}
	if r := c.Update("enter"); r != ConfirmCancel {
		t.Fatalf("Enter = %v", r)
	}
	// Mesmo com o foco forçado no botão indisponível, Enter nunca aceita.
	c.Focus = 1
	if r := c.Update("enter"); r == ConfirmAccept {
		t.Fatal("Enter aceitou botão indisponível")
	}
	if h := c.Hint(); strings.Contains(h, "pode confirmar") || strings.Contains(h, "s confirma") {
		t.Errorf("a dica oferece s: %q", h)
	}
	if v := strings.Join(c.View(monoTheme, 80, 20), "\n"); !strings.Contains(v, "( Aplicar: sem administrador )") || !strings.Contains(v, ">[ Cancelar ]<") {
		t.Errorf("botões:\n%s", v)
	}
}

func longPlan(n int) *Confirm {
	c := sampleConfirm()
	for i := 1; i <= n; i++ {
		c.Plan = append(c.Plan, PlanLine{Label: fmt.Sprintf("Ajuste %02d", i), From: "1", To: "0", Admin: i%2 == 0})
	}
	return c
}

func TestConfirmRolagemDoPlano(t *testing.T) {
	c := longPlan(30)
	v := strings.Join(c.View(monoTheme, 80, 24), "\n")
	if !strings.Contains(v, "Ajuste 01") || strings.Contains(v, "Ajuste 30") || !strings.Contains(v, SymMore+" mais ") {
		t.Fatalf("início:\n%s", v)
	}
	if !strings.Contains(v, "[Cancelar]") && !strings.Contains(v, ">[ Cancelar ]<") {
		t.Fatalf("botões sumiram:\n%s", v)
	}
	for range 150 {
		c.Update("down")
		c.View(monoTheme, 80, 24)
	}
	v = strings.Join(c.View(monoTheme, 80, 24), "\n")
	if !strings.Contains(v, "Ajuste 30") || strings.Contains(v, "Ajuste 01") || !strings.Contains(v, ">[ Cancelar ]<") {
		t.Fatalf("fim:\n%s", v)
	}
	c.Update("pgup")
	c.View(monoTheme, 80, 24)
	if v = strings.Join(c.View(monoTheme, 80, 24), "\n"); !strings.Contains(v, SymMore+" mais") {
		t.Errorf("PgUp deveria voltar algumas linhas:\n%s", v)
	}
	c.Update("pgdown")
	c.Update("up")
	if c.scroll < 0 {
		t.Error("scroll negativo")
	}
	// Rolar nunca confirma nem cancela.
	for _, k := range []string{"up", "down", "pgup", "pgdown"} {
		if r := c.Update(k); r != ConfirmNone {
			t.Errorf("%s = %v", k, r)
		}
	}
}

func TestConfirmPlanLineAdminEmNegrito(t *testing.T) {
	c := sampleConfirm()
	c.Plan = []PlanLine{{Label: "Telemetria", From: "3", To: "0", Admin: true, Detail: "HKLM"}, {Label: "Dicas", From: "1", To: "0"}}
	mono := strings.Join(c.View(monoTheme, 80, 24), "\n")
	if !strings.Contains(mono, "Telemetria [admin]") || !strings.Contains(mono, "3 -> 0") || strings.Contains(mono, "Dicas [admin]") || strings.Contains(mono, "\x1b") {
		t.Fatalf("mono:\n%s", mono)
	}
	color := strings.Join(c.View(Theme{Dark: true}, 80, 24), "\n")
	if !strings.Contains(color, "[admin]\x1b[m") && !strings.Contains(color, "\x1b[1m[admin]") || !strings.Contains(ansi.Strip(color), "Telemetria [admin]") {
		t.Errorf("com cor o texto deve ser o mesmo do mono; [admin] em negrito:\n%q", color)
	}
}

func TestConfirmLarguraEAlturaFixas(t *testing.T) {
	for _, sz := range [][2]int{{60, 17}, {60, 12}, {80, 24}, {120, 40}, {40, 10}} {
		for _, th := range []Theme{monoTheme, {Dark: true}} {
			c := longPlan(12)
			c.Text = "um caminho de registro muito comprido HKLM\\SOFTWARE\\Policies\\Microsoft\\Windows\\DataCollection\\AllowTelemetry: 3 -> 0\n" + strings.Repeat("palavra ", 30)
			c.Warn = "Este plano tem itens que exigem administrador e este terminal não é: nada será alterado. Abra o terminal como administrador."
			rows := c.View(th, sz[0], sz[1])
			if len(rows) != sz[1] {
				t.Errorf("%v: %d linhas", sz, len(rows))
			}
			for i, r := range rows {
				if w := lipgloss.Width(r); w > sz[0] {
					t.Errorf("%v linha %d: %d colunas: %q", sz, i, w, ansi.Strip(r))
				}
				if strings.Contains(r, "…") {
					t.Errorf("%v linha %d cortada com reticências: %q", sz, i, r)
				}
			}
			if v := ansi.Strip(strings.Join(rows, "\n")); !strings.Contains(v, ">[ Cancelar ]<") {
				t.Errorf("%v: botão sumiu:\n%s", sz, v)
			}
		}
	}
}

func TestConfirmOrdemDasSecoes(t *testing.T) {
	c := longPlan(3)
	c.Warn = "irreversível"
	v := strings.Join(c.View(monoTheme, 100, 30), "\n")
	last := -1
	for _, s := range []string{"APLICAR O PERFIL", "Muda 3 ajustes", "aviso:", "Backup:", "Como desfazer:", "Ajuste 01", "Cancelar"} {
		i := strings.Index(v, s)
		if i < 0 || i < last {
			t.Fatalf("%q fora de ordem (%d <= %d):\n%s", s, i, last, v)
		}
		last = i
	}
}

func TestConfirmTextoExternoSanitizado(t *testing.T) {
	c := NewConfirm("Apli\x1b[31mcar", "")
	c.Title = "t\x1b]0;x\x07ítulo"
	c.Summary = "r\x1b[2Jesumo"
	c.Text = "linha \x1b[31mvermelha\x1b[0m\r\nsegunda\u202eoculta"
	c.Plan = []PlanLine{{Label: "a\x1bb", From: "x\u200b", To: "y\x07"}}
	out := strings.Join(c.View(monoTheme, 80, 24), "\n")
	for _, bad := range []string{"\x1b", "\x07", "\r", "\u202e", "\u200b"} {
		if strings.Contains(out, bad) {
			t.Errorf("sem sanitizar: %q em\n%q", bad, out)
		}
	}
}

func TestConfirmAlturaMinimaMantemBotoes(t *testing.T) {
	c := longPlan(10)
	for h := 1; h <= 8; h++ {
		rows := c.View(monoTheme, 60, h)
		if len(rows) != h {
			t.Fatalf("h=%d: %d linhas", h, len(rows))
		}
	}
	if v := strings.Join(c.View(monoTheme, 60, 6), "\n"); !strings.Contains(v, "Cancelar") {
		t.Errorf("botões sumiram em 6 linhas:\n%s", v)
	}
}

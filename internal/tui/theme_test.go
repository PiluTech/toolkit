package tui

import (
	"regexp"
	"strings"
	"testing"
)

// A1/A5 da auditoria (docs/auditoria-a11y-l10n.md): a cor só vai no SÍMBOLO; a palavra do estado sai na cor padrão
// do terminal, que o usuário já ajustou para ser legível (as 16 cores ANSI não chegam a 4,5:1 em vários temas).
var colorSGR = regexp.MustCompile(`(\x1b\[|;)(3[0-8]|9[0-7])(;|m)`)

func hasColor(s string) bool { return colorSGR.MatchString(s) }

func themes() []Theme {
	return []Theme{{Dark: true}, {Dark: false}}
}

func TestTextTokensNeverCarryAColor(t *testing.T) {
	for _, th := range themes() {
		for name, p := range map[string]Paint{"Muted": th.Muted(), "Accent": th.Accent(), "OK": th.OK(), "Warn": th.Warn(), "Err": th.Err()} {
			for _, text := range []string{"palavra", "erro:", "Avisos (2)", "ok", "não instalado"} {
				if out := p.Render(text); hasColor(out) {
					t.Errorf("dark=%v %s.Render(%q) tem cor: %q", th.Dark, name, text, out)
				}
			}
		}
	}
}

func TestOnlyTheLeadingSymbolIsColored(t *testing.T) {
	for _, th := range themes() {
		for name, p := range map[string]Paint{"Accent": th.Accent(), "OK": th.OK(), "Warn": th.Warn(), "Err": th.Err()} {
			for _, in := range []string{SymOK + " instalado", SymFail + " falhou", SymWarn + " aguardando reinício", "[x]", "/ busca"} {
				out := p.Render(in)
				if !hasColor(out) {
					t.Errorf("dark=%v %s.Render(%q): o símbolo deveria ter cor: %q", th.Dark, name, in, out)
					continue
				}
				// depois do primeiro reset (fim do símbolo) não pode haver cor, e a palavra tem de estar inteira
				head, tail, _ := strings.Cut(out, "\x1b[m")
				if hasColor(tail) {
					t.Errorf("dark=%v %s.Render(%q): cor depois do símbolo: %q", th.Dark, name, in, out)
				}
				if plain(out) != in {
					t.Errorf("dark=%v %s.Render(%q): o texto mudou: %q", th.Dark, name, in, plain(out))
				}
				_ = head
			}
		}
	}
}

// Sem cor (NO_COLOR/--no-color) nenhum token emite sequência nenhuma.
func TestMonoEmitsNoSequence(t *testing.T) {
	th := Theme{Mono: true}
	for name, p := range map[string]Paint{"Muted": th.Muted(), "Accent": th.Accent(), "OK": th.OK(), "Warn": th.Warn(), "Err": th.Err()} {
		for _, in := range []string{SymOK + " ok", "erro:", "[x]"} {
			if out := p.Render(in); out != in {
				t.Errorf("%s.Render(%q) em Mono = %q", name, in, out)
			}
		}
	}
}

// Todo estado tem símbolo E palavra (nada depende só da cor).
func TestEveryStateHasSymbolAndWord(t *testing.T) {
	th := Theme{Mono: true}
	lt := NewLogTab(th, "")
	for _, s := range []ItemStatus{ItemQueued, ItemRunning, ItemOK, ItemFailed, ItemSkipped, ItemReboot, ItemCanceled, ItemNotRun} {
		sym, word, _ := lt.itemState(logItem{status: s})
		if sym == "" || strings.TrimSpace(word) == "" {
			t.Errorf("log: estado %v sem símbolo ou palavra: %q %q", s, sym, word)
		}
	}
	for _, st := range []string{"done", "skipped", "failed", "awaiting-reboot", "running", "pending", ""} {
		txt, _ := stepState(th, st)
		sym, word, ok := strings.Cut(txt, " ")
		if !ok || sym == "" || strings.TrimSpace(word) == "" {
			t.Errorf("etapa %q: sem símbolo + palavra: %q", st, txt)
		}
	}
	tab := loadedRecipes(t, recipeSample(), false)
	for _, r := range tab.items {
		if txt, _ := tab.recipeState(r); txt != "" {
			if sym, word, ok := strings.Cut(txt, " "); !ok || sym == "" || strings.TrimSpace(word) == "" {
				t.Errorf("receita %s: sem símbolo + palavra: %q", r.ID, txt)
			}
		}
	}
}

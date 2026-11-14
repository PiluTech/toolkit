package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/PiluTech/toolkit/internal/textsafe"
)

// GoToMsg pede à raiz que abra uma área (índice da barra, 0 a 5) e uma seção dela (índice dentro da área).
// Quem decide é a raiz: ela ignora o pedido durante a Revisão, a execução, um modal, uma pergunta [s/N] ou uma
// seção que captura teclas (uma confirmação aberta nunca é abandonada por um pedido de navegação).
type GoToMsg struct{ Area, Section int }

// KeyClaimer é implementada pela seção que usa Enter e os números para si (o Início): com a raiz em navegação
// normal (sem Revisão, execução, modal nem pergunta) essas teclas vão direto à seção, antes de abrir a Revisão
// ou de trocar de área. Em qualquer outro modo valem as regras da raiz.
type KeyClaimer interface{ Claims(key string) bool }

// InicioItem é um objetivo da tela Início: Title diz o que a pessoa quer fazer, Desc é uma frase didática (quebra,
// nunca é cortada), Tag é a etiqueta de estado ou requisito ("! só Windows", "○ em breve"; começa por um
// símbolo e diz o estado também por palavra), Area e Section são o destino e Dest o nome dele na linha de estado.
type InicioItem struct {
	Title, Desc, Tag string
	Area, Section    int
	Dest             string
	// Unavailable marca o objetivo que ainda não existe ("em breve"): Enter e a tecla do item NÃO navegam (nenhum GoTo) e a
	// linha de estado diz que ainda não existe. É um campo explícito, nunca deduzido do texto da etiqueta.
	Unavailable bool
}

// InicioTab é a seção Início: "O que você quer fazer?", uma lista de objetivos. Enter (ou o número) pede à raiz que
// abra a área e a seção do item. Não lê nada do sistema: as etiquetas vêm prontas do chamador.
type InicioTab struct {
	theme                 Theme
	title, short, heading string
	items                 []InicioItem
	cur, top, more        int
}

// NewInicioTab monta o Início. title e short são os nomes da seção; heading é a pergunta do topo.
func NewInicioTab(th Theme, title, short, heading string, items []InicioItem) *InicioTab {
	return &InicioTab{theme: th, title: title, short: short, heading: heading, items: items}
}

// Init não carrega nada.
func (t *InicioTab) Init() tea.Cmd { return nil }

// Capturing é sempre falso: Início não recebe texto.
func (t *InicioTab) Capturing() bool { return false }

// Title devolve o nome inteiro da seção.
func (t *InicioTab) Title() string { return t.title }

// Short devolve o nome curto da seção.
func (t *InicioTab) Short() string { return t.short }

// Items devolve os objetivos (uma cópia; para os testes de quem monta o Início).
func (t *InicioTab) Items() []InicioItem { return append([]InicioItem(nil), t.items...) }

// Cursor devolve o índice do item sob o cursor (para os testes).
func (t *InicioTab) Cursor() int { return t.cur }

// digitLimit é o maior número de atalho: 1 a 9 (e nunca 0, que é a área Início).
func (t *InicioTab) digitLimit() int { return min(len(t.items), 9) }

// Claims diz se a tecla é da seção: Enter e os números 1 até o último item.
func (t *InicioTab) Claims(k string) bool {
	if len(t.items) == 0 {
		return false
	}
	if k == "enter" {
		return true
	}
	return len(k) == 1 && k[0] >= '1' && int(k[0]-'0') <= t.digitLimit()
}

func (t *InicioTab) goTo(i int) tea.Cmd {
	if i < 0 || i >= len(t.items) {
		return nil
	}
	it := t.items[i]
	if it.Unavailable {
		return nil
	}
	return func() tea.Msg { return GoToMsg{Area: it.Area, Section: it.Section} }
}

// Update trata ↑↓/jk, g/G, Enter e os números.
func (t *InicioTab) Update(msg tea.Msg) (Tab, tea.Cmd) {
	k, ok := msg.(tea.KeyPressMsg)
	if !ok || len(t.items) == 0 {
		return t, nil
	}
	switch s := k.String(); s {
	case "up", "k":
		t.cur = max(t.cur-1, 0)
	case "down", "j":
		t.cur = min(t.cur+1, len(t.items)-1)
	case "g", "home":
		t.cur = 0
	case "G", "end":
		t.cur = len(t.items) - 1
	case "enter":
		return t, t.goTo(t.cur)
	default:
		if t.Claims(s) {
			t.cur = int(s[0] - '1')
			return t, t.goTo(t.cur)
		}
	}
	return t, nil
}

// Keys são os atalhos da barra de teclas.
func (t *InicioTab) Keys() []KeyHint {
	return []KeyHint{{"↑↓", "mover"}, {"Enter", "abrir"}, {"1-" + itoa(t.digitLimit()), "ir direto"}}
}

// HelpKeys implementa SectionHelper. Aqui os números escolhem o objetivo (não a área): o Início os usa para si.
func (t *InicioTab) HelpKeys() []KeyDef {
	return []KeyDef{
		navDef("↑↓", "mover", "escolhe o objetivo."),
		navDef("Enter", "abrir", "abre a área e a seção do objetivo."),
		navDef("1-"+itoa(t.digitLimit()), "ir direto", "abre o objetivo do número (aqui os números escolhem o objetivo, não a área)."),
	}
}

// Status diz para onde o Enter leva.
func (t *InicioTab) Status() string {
	if len(t.items) == 0 {
		return ""
	}
	if it := t.items[t.cur]; it.Unavailable {
		return textsafe.Line(it.Title) + ": ainda não existe nesta versão."
	}
	return "Enter abre " + textsafe.Line(t.items[t.cur].Dest)
}

// More é quantas linhas ficaram abaixo da dobra na última vez que a seção foi desenhada.
func (t *InicioTab) More() int { return t.more }

// Selected não marca nada.
func (t *InicioTab) Selected() int { return 0 }

// build monta as linhas do corpo na largura w; start/end dão a primeira e a última linha de cada item.
func (t *InicioTab) build(w int, blanks bool) (lines []string, start, end []int) {
	lines = []string{" " + t.theme.Title().Render(titleCase(t.theme, textsafe.Line(t.heading))), ""}
	const indent = 5 // " > 1 ": a frase fica sob o título
	pad := strings.Repeat(" ", indent)
	for i, it := range t.items {
		if blanks && i > 0 {
			lines = append(lines, "")
		}
		start = append(start, len(lines))
		marker := "  "
		title := textsafe.Line(it.Title)
		if i == t.cur {
			marker = "> "
			title = t.theme.Bold().Render(title)
		}
		head := " " + marker + itoa(i+1) + " " + title
		tag := textsafe.Line(it.Tag)
		tagOwnLine := ""
		if tag != "" {
			painted := t.paintTag(tag)
			if gap := w - lipgloss.Width(head) - lipgloss.Width(tag) - 1; gap >= 2 {
				head += strings.Repeat(" ", gap) + painted
			} else {
				tagOwnLine = pad + painted
			}
		}
		lines = append(lines, head)
		for _, l := range wrap(textsafe.Line(it.Desc), max(w-indent-1, 10), 0) {
			lines = append(lines, pad+l)
		}
		if tagOwnLine != "" {
			lines = append(lines, tagOwnLine)
		}
		end = append(end, len(lines)-1)
	}
	return lines, start, end
}

func (t *InicioTab) paintTag(tag string) string {
	if strings.HasPrefix(tag, SymWarn+" ") {
		return t.theme.Warn().Render(tag)
	}
	return t.theme.Muted().Render(tag)
}

// View desenha o corpo com h linhas: a lista tem espaço entre os itens quando cabe; senão compacta e rola
// (More conta o que ficou abaixo) mantendo o item sob o cursor inteiro na tela.
func (t *InicioTab) View(w, h int) string {
	lines, start, end := t.build(w, true)
	if len(lines) > h {
		lines, start, end = t.build(w, false)
	}
	if len(t.items) > 0 {
		s, e := start[t.cur], end[t.cur]
		switch {
		case t.cur == 0:
			t.top = 0
		case s < t.top:
			t.top = s
		case e >= t.top+h:
			t.top = max(e-h+1, 0)
			if s < t.top { // item mais alto que a tela: o começo dele manda
				t.top = s
			}
		}
	}
	t.top = max(min(t.top, max(len(lines)-h, 0)), 0)
	bottom := min(t.top+h, len(lines))
	t.more = len(lines) - bottom
	return strings.Join(lines[t.top:bottom], "\n")
}

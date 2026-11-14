package tui

import (
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/PiluTech/toolkit/internal/textsafe"
)

// Painel de detalhe (fase 7 do redesign, docs/tui.md): a partir de DetailMinWidth colunas a seção ativa que implementa
// Detailer ganha, à direita da lista, um painel SÓ DE LEITURA de DetailWidth colunas com o item sob o cursor. A lista
// recebe o resto (w - DetailWidth - 1), separada do painel por uma coluna "│". Abaixo de DetailMinWidth nada muda.
const (
	DetailMinWidth = 110
	DetailWidth    = 44
	detailSep      = "│"
)

// detailBody monta o corpo da seção ativa na visão normal: a lista sozinha ou lista, separador e painel.
// Esconde o painel (e devolve a lista na largura toda) com a janela estreita, fora da navegação normal (Revisão,
// execução, pergunta, modal), ou quando a seção diz que não há o que detalhar (Detail vazio: carregando, erro, vazio,
// não suportado, plano ou confirmação na tela).
func (m *Model) detailBody(h int) string {
	tab := m.tabs[m.active]
	dt, ok := tab.(Detailer)
	if !ok || m.w < DetailMinWidth || m.modal != modalNone || m.mode != modeNav || m.asking != askNone || m.question != "" {
		return tab.View(m.w, h)
	}
	det := dt.Detail(DetailWidth, h)
	if strings.TrimSpace(det) == "" {
		return tab.View(m.w, h)
	}
	listW := m.w - DetailWidth - 1
	left := fit(strings.Split(strings.TrimRight(tab.View(listW, h), "\n"), "\n"), listW, h)
	right := fit(strings.Split(strings.TrimRight(det, "\n"), "\n"), DetailWidth, h)
	rows := make([]string, h)
	for i := range rows {
		rows[i] = padRight(left[i], listW) + detailSep + right[i]
	}
	return strings.Join(rows, "\n")
}

// detailDoc monta o texto do painel: tudo quebra na largura (nunca reticências), tudo passa por textsafe e cada linha
// começa com uma coluna de margem, como o resto da TUI.
type detailDoc struct {
	th    Theme
	w     int
	lines []string
}

func newDetail(th Theme, w int) *detailDoc { return &detailDoc{th: th, w: w} }

// push acrescenta uma linha sem espaços no fim.
func (d *detailDoc) push(l string) { d.lines = append(d.lines, strings.TrimRight(l, " ")) }

func (d *detailDoc) room() int { return max(d.w-1, 10) }

// name é o título do item (negrito; em Mono vale a posição de primeira linha).
func (d *detailDoc) name(s string) {
	for _, l := range wrap(textsafe.Line(s), d.room(), 0) {
		d.push(" " + d.th.Bold().Render(l))
	}
}

// section abre um bloco: linha em branco e o rótulo (em Mono, MAIÚSCULAS).
func (d *detailDoc) section(label string) {
	if len(d.lines) > 0 {
		d.push("")
	}
	d.push(" " + d.th.Title().Render(titleCase(d.th, textsafe.Line(label))))
}

// para é um parágrafo quebrado na largura.
func (d *detailDoc) para(s string) {
	for _, l := range wrap(textsafe.Line(s), d.room(), 0) {
		d.push(" " + l)
	}
}

// item é uma linha de lista com recuo: "- texto", e a continuação alinhada sob o texto.
func (d *detailDoc) item(s string) { d.marked("- ", s) }

// marked é um item com o marcador dado (o recuo da continuação é a largura dele).
func (d *detailDoc) marked(marker, s string) {
	pad := strings.Repeat(" ", lipgloss.Width(marker))
	for i, l := range wrap(textsafe.Line(s), max(d.room()-lipgloss.Width(marker), 10), 0) {
		if i == 0 {
			d.push(" " + marker + l)
		} else {
			d.push(" " + pad + l)
		}
	}
}

// status escreve um estado (símbolo e palavra) com a pintura dada; o texto vem pronto, e quebra se não couber.
func (d *detailDoc) status(p Paint, s string) { d.statusAt("", p, s) }

// statusAt é status com um recuo antes do símbolo (a cor fica só no símbolo).
func (d *detailDoc) statusAt(pad string, p Paint, s string) {
	for i, l := range wrap(textsafe.Line(s), max(d.room()-len(pad), 10), 2) {
		if i == 0 {
			d.push(" " + pad + p.Render(l))
		} else {
			d.push(" " + pad + l)
		}
	}
}

// render devolve até h linhas. Se não couber, o fim vira " ↓ mais N linhas" (nada é cortado com reticências).
func (d *detailDoc) render(h int) string {
	lines := d.lines
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	if h < 1 {
		return ""
	}
	if len(lines) > h {
		keep := h - 1
		more := " " + d.th.Muted().Render(SymMore+" mais "+plural(len(lines)-keep, "linha", "linhas"))
		lines = append(append([]string{}, lines[:keep]...), more)
	}
	return strings.Join(lines, "\n")
}

// managerNote diz, em uma frase, o que o gerenciador exige (só fatos do próprio toolkit: sudo -n nunca pede senha).
func managerNote(m string) string {
	switch m {
	case "apt", "dnf", "pacman":
		return "usa sudo -n (rode sudo -v antes; o toolkit nunca pede senha)"
	case "flatpak":
		return "instalação de sistema, usa sudo -n (rode sudo -v antes)"
	case "brew":
		return "sem sudo; não rode como root"
	case "winget":
		return "o Windows pede a permissão (UAC) se precisar"
	case "github-release", "url":
		return "baixa o arquivo e confere o SHA-256 antes de usar"
	}
	return ""
}

package tui

import (
	"context"
	"errors"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/PiluTech/toolkit/internal/diagfmt"
	"github.com/PiluTech/toolkit/internal/diagnostico"
	"github.com/PiluTech/toolkit/internal/textsafe"
)

// MachineBackend lê o estado da máquina (somente leitura): o mesmo relatório do `toolkit diagnostico`.
// A implementação real fica em internal/cli (tui_maquina.go); os testes usam um fake.
type MachineBackend interface {
	Load(ctx context.Context) (*diagnostico.Report, error)
}

type machineLoadedMsg struct {
	gen int
	rep *diagnostico.Report
	err error
}

// DiagTab é a aba Diagnóstico: as receitas de diagnóstico (RecipesTab com diagnostic=true, sem mudança) mais a
// visão "Máquina" (tecla m), com o resumo "o que está ruim e o que fazer" e, rolando, os dados lidos como o
// sistema os informou (nome, série, UUID, MAC, série dos discos) e o que o administrador leria a mais.
type DiagTab struct {
	rec    *RecipesTab
	be     MachineBackend
	ctx    context.Context
	theme  Theme
	lang   diagnostico.Lang
	cancel context.CancelFunc

	machine bool // visão Máquina ativa
	loading bool
	loaded  bool
	gen     int
	err     error
	lines   []string
	wrap    []string // lines quebradas em wrapW colunas (cache)
	wrapW   int
	wrapN   int
	top     int
	frame   int
	lastW   int
	lastH   int
}

// NewDiagTab cria a aba Diagnóstico: rb alimenta a lista de receitas, mb a visão Máquina; lang é o idioma do relatório.
func NewDiagTab(ctx context.Context, rb RecipesBackend, mb MachineBackend, th Theme, lang diagnostico.Lang) *DiagTab {
	return &DiagTab{rec: NewRecipesTab(ctx, rb, th, true), be: mb, ctx: ctx, theme: th, lang: lang}
}

// SetContext troca o contexto das chamadas aos backends (o Run cancela o dele ao sair).
func (t *DiagTab) SetContext(ctx context.Context) { t.ctx = ctx; t.rec.SetContext(ctx) }

// Marked devolve as receitas de diagnóstico marcadas (a Revisão as lê).
func (t *DiagTab) Marked() []RecipeMark { return t.rec.Marked() }

// Reload relê as receitas e o andamento (depois de uma execução); a visão Máquina só recarrega quando o usuário pede.
func (t *DiagTab) Reload() tea.Cmd { return t.rec.Reload() }

// Init implementa Tab: só as receitas carregam; a máquina é lida quando o usuário abre a visão.
func (t *DiagTab) Init() tea.Cmd { return t.rec.Init() }

// Capturing implementa Tab: só a busca das receitas recebe todas as teclas.
func (t *DiagTab) Capturing() bool { return !t.machine && t.rec.Capturing() }

// Title implementa Tab.
func (t *DiagTab) Title() string { return t.rec.Title() }

// Short implementa Tab.
func (t *DiagTab) Short() string { return t.rec.Short() }

// Selected implementa Tab: na visão Máquina nada está marcado para a Revisão (Enter recarrega).
func (t *DiagTab) Selected() int {
	if t.machine {
		return 0
	}
	return t.rec.Selected()
}

// load começa uma leitura nova (cancela a anterior, se houver).
func (t *DiagTab) load() tea.Cmd {
	if t.cancel != nil {
		t.cancel()
	}
	ctx, cancel := context.WithCancel(t.ctx)
	t.cancel = cancel
	t.gen++
	t.loading, t.err = true, nil
	gen, be := t.gen, t.be
	return tea.Batch(func() tea.Msg {
		rep, err := be.Load(ctx)
		return machineLoadedMsg{gen: gen, rep: rep, err: err}
	}, t.theme.Tick())
}

func (t *DiagTab) stop() {
	if t.cancel != nil {
		t.cancel()
		t.cancel = nil
	}
	t.gen++ // a resposta que ainda chegar é descartada
	t.loading = false
}

// Update implementa Tab.
func (t *DiagTab) Update(msg tea.Msg) (Tab, tea.Cmd) {
	switch m := msg.(type) {
	case machineLoadedMsg:
		if m.gen != t.gen {
			return t, nil
		}
		t.loading = false
		if m.err != nil || m.rep == nil {
			t.err = m.err
			if t.err == nil {
				t.err = errors.New("relatório vazio")
			}
			return t, nil
		}
		t.loaded = true
		t.lines = t.render(m.rep)
		t.wrapN = -1
		t.top = 0
		return t, nil
	case tickMsg:
		t.frame++
		var cmds []tea.Cmd
		if t.loading {
			cmds = append(cmds, t.theme.Tick())
		}
		// o spinner das receitas também precisa do tick
		if _, c := t.rec.Update(msg); c != nil && t.rec.loading {
			cmds = append(cmds, c)
		}
		return t, tea.Batch(cmds...)
	case tea.KeyPressMsg:
		if t.machine {
			return t, t.machineKey(m)
		}
		if !t.rec.Capturing() && m.String() == "m" {
			t.machine = true
			if !t.loaded && !t.loading {
				return t, t.load()
			}
			return t, nil
		}
	}
	_, cmd := t.rec.Update(msg)
	return t, cmd
}

func (t *DiagTab) machineKey(msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "m", "esc":
		t.stop() // sair cancela uma leitura em andamento
		t.machine = false
	case "c", "enter":
		return t.load()
	case "up", "k":
		t.scroll(-1)
	case "down", "j":
		t.scroll(1)
	case "pgup":
		t.scroll(-max(t.lastH-1, 1))
	case "pgdown":
		t.scroll(max(t.lastH-1, 1))
	case "g", "home":
		t.top = 0
	case "G", "end":
		t.top = t.maxTop()
	}
	return nil
}

// wrapped quebra as linhas do relatório na largura atual (nada de dado some atrás de "…"); a linha de continuação leva recuo.
func (t *DiagTab) wrapped() []string {
	if t.lastW < 8 {
		return t.lines
	}
	if t.wrapW == t.lastW && t.wrapN == len(t.lines) {
		return t.wrap
	}
	var out []string
	for _, l := range t.lines {
		for i, p := range strings.Split(ansi.Wrap(l, t.lastW-2, ""), "\n") {
			if i > 0 {
				p = "  " + p
			}
			out = append(out, p)
		}
	}
	t.wrap, t.wrapW, t.wrapN = out, t.lastW, len(t.lines)
	return out
}

func (t *DiagTab) maxTop() int { return max(len(t.wrapped())-max(t.lastH, 1), 0) }

func (t *DiagTab) scroll(n int) { t.top = max(min(t.top+n, t.maxTop()), 0) }

// render monta o texto da visão: o resumo e, depois, os dados completos (o relatório do `toolkit diagnostico --completo`).
// Os textos vêm do sistema (nome, série, mensagens de log): passam por textsafe, que preserva LF e Tab.
func (t *DiagTab) render(rep *diagnostico.Report) []string {
	findings := diagnostico.Analyze(rep, t.lang)
	text := t.identity(rep) + diagnostico.Summary(rep, findings, t.lang) + "\n" + diagnostico.Full(rep, t.lang)
	text = strings.ReplaceAll(textsafe.Multiline(text), "\t", "    ")
	return strings.Split(strings.TrimRight(text, "\n"), "\n")
}

// identity é o bloco de identificação (nome, série, UUID, MACs) no topo da visão, antes dos achados.
func (t *DiagTab) identity(rep *diagnostico.Report) string {
	l := t.lang
	tr := func(pt, en, es string) string { return diagfmt.T(l, pt, en, es) }
	var b strings.Builder
	add := func(label, v string) {
		if v != "" {
			b.WriteString(label + ": " + v + "\n")
		}
	}
	add(tr("Nome da máquina", "Machine name", "Nombre del equipo"), rep.Hardware.Hostname)
	add(tr("Número de série", "Serial number", "Número de serie"), rep.Hardware.Serial)
	add("UUID", rep.Hardware.UUID)
	for _, n := range rep.NICs {
		add("MAC "+n.Name, n.MAC)
	}
	if b.Len() == 0 {
		return ""
	}
	return b.String() + "\n"
}

// View implementa Tab.
func (t *DiagTab) View(w, h int) string {
	if !t.machine {
		return t.rec.View(w, h)
	}
	t.lastW, t.lastH = w, h
	switch {
	case t.loading:
		return strings.Join(StateLoading(t.theme, w, h, "a máquina", t.frame, "Esc cancela."), "\n")
	case t.err != nil:
		return strings.Join(StateError(t.theme, w, h, "a máquina", t.err, "Tente de novo (c)."), "\n")
	case !t.loaded:
		return strings.Join(StateEmpty(t.theme, w, h, "Esta máquina ainda não foi lida.", "Tecle c para ler a máquina."), "\n")
	}
	t.top = max(min(t.top, t.maxTop()), 0)
	ls := t.wrapped()
	end := min(t.top+max(h, 1), len(ls))
	out := make([]string, 0, end-t.top)
	for _, l := range ls[t.top:end] {
		out = append(out, clip(" "+l, w))
	}
	return strings.Join(out, "\n")
}

// Keys implementa Tab.
func (t *DiagTab) Keys() []KeyHint {
	if t.machine {
		return []KeyHint{{"↑↓", "rolar"}, {"c", "recarregar"}, {"m", "receitas"}, {"Esc", "voltar"}}
	}
	keys := t.rec.Keys()
	if t.rec.Capturing() || t.rec.focus {
		return keys
	}
	// em 80 colunas só cabem cinco atalhos: "m máquina" (a função nova) entra antes de "→ etapas", que cede o lugar
	var out []KeyHint
	var etapas []KeyHint
	for _, k := range keys {
		if k.Key == "→" {
			etapas = append(etapas, k)
			continue
		}
		out = append(out, k)
	}
	out = append(out, KeyHint{"m", "máquina"})
	return append(out, etapas...)
}

// Status implementa Tab.
func (t *DiagTab) Status() string {
	if !t.machine {
		return t.rec.Status()
	}
	switch {
	case t.loading:
		return "Lendo a máquina…"
	case t.err != nil:
		return "Falhou. c tenta de novo."
	case t.loaded:
		return "Máquina — dados como o sistema informou; c recarrega"
	}
	return ""
}

// More implementa Tab.
func (t *DiagTab) More() int {
	if !t.machine {
		return t.rec.More()
	}
	if t.loading || t.err != nil || !t.loaded {
		return 0
	}
	return max(len(t.wrapped())-(t.top+max(t.lastH, 1)), 0)
}

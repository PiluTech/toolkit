package tui

import (
	"context"
	"errors"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/PiluTech/toolkit/internal/rede"
	"github.com/PiluTech/toolkit/internal/textsafe"
)

// NetBackend lê a rede da máquina (somente leitura): o mesmo relatório do `toolkit rede`. A implementação real fica em
// internal/cli (tui_rede.go); os testes usam um fake.
type NetBackend interface {
	Load(ctx context.Context) (*rede.Report, error)
}

type netLoadedMsg struct {
	gen int
	rep *rede.Report
	err error
}

// RedeTab é a seção Rede › Análise: o resumo "o que está exposto e o que fazer" no topo e, rolando, os dados lidos como o
// sistema os informou (IPs, MACs, nome da rede, portas e processos). Só leitura: nenhum comando muda nada, nenhum faz
// varredura nem acessa a internet. A leitura é pedida pela pessoa (c; o Enter é da raiz e não chega aqui), porque lê a rede e roda comandos do SO;
// Esc a cancela. Todo texto do sistema (SSID, nome de processo) passa por textsafe.
type RedeTab struct {
	be     NetBackend
	ctx    context.Context
	theme  Theme
	lang   rede.Lang
	title  string
	short  string
	cancel context.CancelFunc

	loading   bool
	loaded    bool
	cancelled bool
	gen       int
	err       error
	lines     []string
	wrap      []string // lines quebradas em wrapW colunas (cache)
	wrapW     int
	wrapN     int
	top       int
	frame     int
	lastW     int
	lastH     int
}

// NewRedeTab cria a seção; title e short são o nome da seção ("Análise", "Anál"); lang é o idioma do relatório.
func NewRedeTab(ctx context.Context, be NetBackend, th Theme, lang rede.Lang, title, short string) *RedeTab {
	return &RedeTab{be: be, ctx: ctx, theme: th, lang: lang, title: title, short: short}
}

// SetContext troca o contexto das chamadas ao backend (o Run cancela o dele ao sair).
func (t *RedeTab) SetContext(ctx context.Context) { t.ctx = ctx }

// Init implementa Tab: nada roda ao abrir o toolkit (a leitura é pedida pela pessoa).
func (t *RedeTab) Init() tea.Cmd { return nil }

// Capturing implementa Tab: a seção não recebe texto.
func (t *RedeTab) Capturing() bool { return false }

// Title implementa Tab.
func (t *RedeTab) Title() string { return t.title }

// Short implementa Tab.
func (t *RedeTab) Short() string { return t.short }

// Selected implementa Tab: nada se marca (a seção só lê).
func (t *RedeTab) Selected() int { return 0 }

// load começa uma leitura nova (cancela a anterior, se houver).
func (t *RedeTab) load() tea.Cmd {
	if t.cancel != nil {
		t.cancel()
	}
	ctx, cancel := context.WithCancel(t.ctx)
	t.cancel = cancel
	t.gen++
	t.loading, t.cancelled, t.err = true, false, nil
	gen, be := t.gen, t.be
	return tea.Batch(func() tea.Msg {
		rep, err := be.Load(ctx)
		return netLoadedMsg{gen: gen, rep: rep, err: err}
	}, t.theme.Tick())
}

// stop cancela a leitura em andamento; a resposta que ainda chegar é descartada.
func (t *RedeTab) stop() {
	if t.cancel != nil {
		t.cancel()
		t.cancel = nil
	}
	t.gen++
	t.loading = false
}

// Update implementa Tab.
func (t *RedeTab) Update(msg tea.Msg) (Tab, tea.Cmd) {
	switch m := msg.(type) {
	case netLoadedMsg:
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
		if t.loading {
			return t, t.theme.Tick()
		}
	case tea.KeyPressMsg:
		return t, t.key(m)
	}
	return t, nil
}

func (t *RedeTab) key(msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "esc":
		if t.loading {
			t.stop() // Esc cancela a leitura em andamento
			t.cancelled = true
		}
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
func (t *RedeTab) wrapped() []string {
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

func (t *RedeTab) maxTop() int { return max(len(t.wrapped())-max(t.lastH, 1), 0) }

func (t *RedeTab) scroll(n int) { t.top = max(min(t.top+n, t.maxTop()), 0) }

// render monta o texto da seção: o resumo e, depois, os dados completos (o relatório do `toolkit rede --completo`). Os
// textos vêm do sistema (SSID, processos): passam por textsafe, que preserva LF e Tab.
func (t *RedeTab) render(rep *rede.Report) []string {
	findings := rede.Analyze(rep, t.lang)
	text := rede.Summary(rep, findings, t.lang) + "\n" + rede.Full(rep, t.lang)
	text = strings.ReplaceAll(textsafe.Multiline(text), "\t", "    ")
	return strings.Split(strings.TrimRight(text, "\n"), "\n")
}

// View implementa Tab.
func (t *RedeTab) View(w, h int) string {
	t.lastW, t.lastH = w, h
	switch {
	case t.loading:
		return strings.Join(StateLoading(t.theme, w, h, "a rede", t.frame, "Esc cancela."), "\n")
	case t.err != nil:
		return strings.Join(StateError(t.theme, w, h, "a rede", t.err, "Tente de novo (c)."), "\n")
	case t.cancelled:
		return strings.Join(StateEmpty(t.theme, w, h, "A leitura da rede foi cancelada.", "Tecle c para ler de novo."), "\n")
	case !t.loaded:
		return strings.Join(StateEmpty(t.theme, w, h, "A rede ainda não foi lida.", "Tecle c para ler a rede (só leitura: sem varredura e sem internet)."), "\n")
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
func (t *RedeTab) Keys() []KeyHint {
	if t.loading {
		return []KeyHint{{"Esc", "cancelar"}}
	}
	if !t.loaded {
		return []KeyHint{act("c").Hint()}
	}
	return []KeyHint{{"↑↓", "rolar"}, act("c").Hint()}
}

// HelpKeys implementa SectionHelper: as teclas da seção, uma frase por tecla (legenda única em keys.go).
func (t *RedeTab) HelpKeys() []KeyDef {
	return []KeyDef{
		navDef("↑↓", "rolar", "rola o resumo e os dados da rede."),
		act("c").says("lê a rede (de novo); só leitura, sem varredura e sem acessar a internet."),
		navDef("Esc", "cancelar", "cancela a leitura em andamento."),
	}
}

// Status implementa Tab.
func (t *RedeTab) Status() string {
	switch {
	case t.loading:
		return "Lendo a rede…"
	case t.err != nil:
		return "Falhou. c lê de novo."
	case t.cancelled:
		return "Leitura cancelada. c lê de novo."
	case t.loaded:
		return "Rede — dados como o sistema informou; c lê de novo"
	}
	return "Nada lido ainda"
}

// More implementa Tab.
func (t *RedeTab) More() int {
	if t.loading || t.err != nil || !t.loaded {
		return 0
	}
	return max(len(t.wrapped())-(t.top+max(t.lastH, 1)), 0)
}

package tui

import (
	"context"
	"errors"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/PiluTech/toolkit/internal/otimizacao"
	"github.com/PiluTech/toolkit/internal/textsafe"
)

// OptimizeBackend lê os gargalos do Windows (somente leitura): o mesmo relatório do `toolkit otimizacao`. A implementação
// real fica em internal/cli (tui_otimizacao.go); os testes usam um fake.
type OptimizeBackend interface {
	Load(ctx context.Context) (*otimizacao.Report, error)
}

type otimLoadedMsg struct {
	gen int
	rep *otimizacao.Report
	err error
}

// OptimizeTab é a seção Otimização da área Máquina: a leitura dos gargalos do Windows (CPU, memória, disco, inicialização,
// serviços e boot) com o resumo "o que pesa e o que fazer" e, rolando, os dados lidos. SÓ LEITURA: nada é alterado e o
// "o que fazer" é texto. Fora do Windows a seção só avisa "só existe no Windows". A leitura leva alguns segundos (amostra de
// CPU), então NÃO começa sozinha: a pessoa pede com c, e Esc cancela. (As animações, parte B do plano, virão depois.)
type OptimizeTab struct {
	be    OptimizeBackend
	ctx   context.Context
	theme Theme
	lang  otimizacao.Lang
	goos  string

	cancel    context.CancelFunc
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

// NewOptimizeTab cria a seção. goos decide, sem chamar o backend, se ela existe neste sistema ("windows").
func NewOptimizeTab(ctx context.Context, be OptimizeBackend, th Theme, lang otimizacao.Lang, goos string) *OptimizeTab {
	return &OptimizeTab{be: be, ctx: ctx, theme: th, lang: lang, goos: goos}
}

// SetContext troca o contexto das chamadas ao backend (o Run cancela o dele ao sair).
func (t *OptimizeTab) SetContext(ctx context.Context) { t.ctx = ctx }

// Title implementa Tab.
func (t *OptimizeTab) Title() string { return "Otimização" }

// Short implementa Tab.
func (t *OptimizeTab) Short() string { return "Otim" }

// Init implementa Tab: nada é lido ao abrir a TUI (a amostra de CPU concorreria com as outras leituras da abertura).
func (t *OptimizeTab) Init() tea.Cmd { return nil }

// Capturing implementa Tab: a seção nunca captura texto.
func (t *OptimizeTab) Capturing() bool { return false }

// Selected implementa Tab: nada se marca aqui.
func (t *OptimizeTab) Selected() int { return 0 }

func (t *OptimizeTab) supported() bool { return t.goos == "windows" }

func (t *OptimizeTab) load() tea.Cmd {
	if !t.supported() {
		return nil
	}
	if t.cancel != nil {
		t.cancel()
	}
	ctx, cancel := context.WithCancel(t.ctx)
	t.cancel = cancel
	t.gen++
	t.loading, t.err, t.cancelled = true, nil, false
	gen, be := t.gen, t.be
	return tea.Batch(func() tea.Msg {
		rep, err := be.Load(ctx)
		return otimLoadedMsg{gen: gen, rep: rep, err: err}
	}, t.theme.Tick())
}

// stop cancela a leitura em andamento: a resposta que ainda chegar é descartada.
func (t *OptimizeTab) stop() {
	if t.cancel != nil {
		t.cancel()
		t.cancel = nil
	}
	t.gen++
	t.loading = false
}

// Update implementa Tab.
func (t *OptimizeTab) Update(msg tea.Msg) (Tab, tea.Cmd) {
	switch m := msg.(type) {
	case otimLoadedMsg:
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
		return t, nil
	case tea.KeyPressMsg:
		if !t.supported() {
			return t, nil
		}
		switch m.String() {
		case "esc":
			if t.loading {
				t.stop()
				t.cancelled = true
			}
		case "c", "enter":
			return t, t.load()
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
	}
	return t, nil
}

// wrapped quebra as linhas do relatório na largura atual (nada de dado some atrás de "…"); a linha de continuação leva recuo.
func (t *OptimizeTab) wrapped() []string {
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

func (t *OptimizeTab) maxTop() int { return max(len(t.wrapped())-max(t.lastH, 1), 0) }

func (t *OptimizeTab) scroll(n int) { t.top = max(min(t.top+n, t.maxTop()), 0) }

// render monta o texto: o resumo (achados e "o que fazer") e, depois, os dados lidos. Os textos vêm do sistema (nome de
// processo, de serviço, de item de inicialização): passam por textsafe, que preserva LF e Tab.
func (t *OptimizeTab) render(rep *otimizacao.Report) []string {
	text := otimizacao.Summary(rep, otimizacao.Analyze(rep, t.lang), t.lang) + "\n" + otimizacao.Full(rep, t.lang)
	text = strings.ReplaceAll(textsafe.Multiline(text), "\t", "    ")
	return strings.Split(strings.TrimRight(text, "\n"), "\n")
}

// View implementa Tab.
func (t *OptimizeTab) View(w, h int) string {
	t.lastW, t.lastH = w, h
	switch {
	case !t.supported():
		return strings.Join(StateUnsupported(t.theme, w, h, "a leitura de gargalos", "Os gargalos lidos aqui (serviços, itens de inicialização e o log de boot) são do Windows.", osName(t.goos)), "\n")
	case t.loading:
		return strings.Join(StateLoading(t.theme, w, h, "os gargalos do Windows", t.frame, "Esc cancela."), "\n")
	case t.err != nil:
		return strings.Join(StateError(t.theme, w, h, "os gargalos do Windows", t.err, "Tente de novo (c)."), "\n")
	case !t.loaded && t.cancelled:
		return strings.Join(StateEmpty(t.theme, w, h, "Leitura cancelada. Nada foi alterado.", "Tecle c para ler de novo."), "\n")
	case !t.loaded:
		return strings.Join(StateEmpty(t.theme, w, h, "Os gargalos desta máquina ainda não foram lidos.", "Tecle c para ler (só leitura; leva alguns segundos)."), "\n")
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
func (t *OptimizeTab) Keys() []KeyHint {
	switch {
	case !t.supported():
		return nil
	case t.loading:
		return []KeyHint{{"Esc", "cancelar"}}
	case t.loaded:
		return []KeyHint{{"↑↓", "rolar"}, act("c").Hint()}
	}
	return []KeyHint{act("c").Hint()}
}

// HelpKeys implementa SectionHelper: as teclas da seção, uma frase por tecla (legenda única em keys.go).
func (t *OptimizeTab) HelpKeys() []KeyDef {
	switch {
	case !t.supported():
		return noActionsHelp(false)
	case t.loading:
		return []KeyDef{navDef("Esc", "cancelar", "cancela a leitura em andamento; nada foi alterado.")}
	case t.loaded:
		return []KeyDef{
			navDef("↑↓", "rolar", "rola o resumo e os dados lidos."),
			act("c").says("lê os gargalos outra vez (só leitura)."),
		}
	}
	return []KeyDef{act("c").says("lê os gargalos do Windows (só leitura; leva alguns segundos).")}
}

// Status implementa Tab.
func (t *OptimizeTab) Status() string {
	switch {
	case !t.supported():
		return "Otimização — só existe no Windows"
	case t.loading:
		return "Lendo os gargalos do Windows…"
	case t.err != nil:
		return "Falhou. c lê de novo."
	case t.loaded:
		return "Otimização — só leitura, nada é alterado; c lê de novo"
	case t.cancelled:
		return "Leitura cancelada."
	}
	return "Gargalos ainda não lidos."
}

// More implementa Tab.
func (t *OptimizeTab) More() int {
	if !t.supported() || t.loading || t.err != nil || !t.loaded {
		return 0
	}
	return max(len(t.wrapped())-(t.top+max(t.lastH, 1)), 0)
}

// Detail implementa Detailer: com a leitura na tela, o painel mostra os LIMIARES usados e a lista do que o toolkit nunca faz
// aqui (só o que a seção já tem; nada é repetido nem executado). Carregando, com erro, vazio ou fora do Windows: nada.
func (t *OptimizeTab) Detail(w, h int) string {
	if !t.supported() || t.loading || t.err != nil || !t.loaded {
		return ""
	}
	d := newDetail(t.theme, w)
	d.name("Como ler")
	d.para("Só números de um instante e limiares explícitos; sem pontuação.")
	for _, l := range strings.Split(otimizacao.Legend(t.lang), "\n") {
		switch {
		case strings.HasPrefix(l, "- "):
			d.item(strings.TrimPrefix(l, "- "))
		case strings.TrimSpace(l) != "":
			d.section(l)
		}
	}
	return d.render(h)
}

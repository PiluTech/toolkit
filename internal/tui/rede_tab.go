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

// NetQueryResult é o resultado de uma consulta confirmada (só um dos dois vem preenchido).
type NetQueryResult struct {
	IP     *rede.PublicIPResult
	Portal *rede.PortalResult
}

// NetQueryBackend é a parte OPCIONAL do backend que fala com a internet, SÓ SOB PEDIDO: IP público e portal cativo. PlanQuery
// resolve e valida o serviço que a pessoa configurou (rede.ErrNoService sem URL, rede.ErrInvalidService se recusada) SEM
// acessar a rede; Query envia a requisição do plano JÁ confirmado (a TUI só envia o que foi exibido: usa a URL do plano, não
// relê a configuração). A implementação real fica em internal/cli (tui_rede.go).
type NetQueryBackend interface {
	PlanQuery(kind rede.QueryKind) (rede.Plan, error)
	Query(ctx context.Context, p rede.Plan) NetQueryResult
}

type netLoadedMsg struct {
	gen int
	rep *rede.Report
	err error
}

type netQueryMsg struct {
	gen int
	res NetQueryResult
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

	// consultas sob pedido (IP público e portal cativo)
	qbe      NetQueryBackend // nil = o backend não tem consulta (as teclas o e w não existem)
	rep      *rede.Report    // a última leitura (para refazer o resumo com o resultado da consulta)
	pub      *rede.PublicIPResult
	portal   *rede.PortalResult
	conf     *Confirm   // confirmação única da consulta (só existe com asking)
	plan     *rede.Plan // o plano EXIBIDO na confirmação: uso único (zerado ao confirmar ou cancelar)
	asking   bool
	querying bool
	qcancel  context.CancelFunc
	res      *Result // resultado da última consulta, na faixa da raiz até a pessoa fechar (x)
	notice   string
}

// NewRedeTab cria a seção; title e short são o nome da seção ("Análise", "Anál"); lang é o idioma do relatório.
func NewRedeTab(ctx context.Context, be NetBackend, th Theme, lang rede.Lang, title, short string) *RedeTab {
	t := &RedeTab{be: be, ctx: ctx, theme: th, lang: lang, title: title, short: short}
	t.qbe, _ = be.(NetQueryBackend)
	return t
}

// SetContext troca o contexto das chamadas ao backend (o Run cancela o dele ao sair).
func (t *RedeTab) SetContext(ctx context.Context) { t.ctx = ctx }

// Init implementa Tab: nada roda ao abrir o toolkit (a leitura é pedida pela pessoa).
func (t *RedeTab) Init() tea.Cmd { return nil }

// Capturing implementa Tab.
// Durante a confirmação e a consulta todas as teclas são da seção (q, Tab e os números não saem por engano).
func (t *RedeTab) Capturing() bool { return t.asking || t.querying }

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
		t.rep = m.rep
		t.rep.PublicIP, t.rep.Portal = t.pub, t.portal // o que já foi consultado nesta sessão (só em memória)
		t.lines = t.render(m.rep)
		t.wrapN = -1
		t.top = 0
		return t, nil
	case netQueryMsg:
		if m.gen == t.gen && t.querying {
			t.onQueried(m.res)
		}
		return t, nil
	case tickMsg:
		t.frame++
		if t.loading || t.querying {
			return t, t.theme.Tick()
		}
	case tea.KeyPressMsg:
		return t, t.key(m)
	}
	return t, nil
}

func (t *RedeTab) key(msg tea.KeyPressMsg) tea.Cmd {
	switch {
	case t.asking:
		return t.confirmKey(msg.String())
	case t.querying:
		if k := msg.String(); k == "esc" || k == "escape" {
			t.stopQuery()
			t.setNotice("Consulta cancelada: nada foi guardado.")
		}
		return nil
	}
	t.notice = ""
	switch msg.String() {
	case "o":
		return t.askQuery(rede.KindIP)
	case "w":
		return t.askQuery(rede.KindPortal)
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
	case t.asking && t.conf != nil:
		return strings.Join(t.conf.View(t.theme, w, h), "\n")
	case t.querying:
		return strings.Join(StateLoading(t.theme, w, h, "o serviço consultado", t.frame, "Esc cancela."), "\n")
	case t.loading:
		return strings.Join(StateLoading(t.theme, w, h, "a rede", t.frame, "Esc cancela."), "\n")
	case t.err != nil:
		return strings.Join(StateError(t.theme, w, h, "a rede", t.err, "Tente de novo (c)."), "\n")
	case t.cancelled:
		return strings.Join(StateEmpty(t.theme, w, h, "A leitura da rede foi cancelada.", "Tecle c para ler de novo."), "\n")
	case !t.loaded:
		steps := []string{"Tecle c para ler a rede (só leitura: sem varredura e sem internet)."}
		if t.qbe != nil {
			steps = append(steps, "Tecle o (IP público) ou w (portal cativo) para consultar um serviço que você configurou; só com a sua confirmação.")
		}
		return strings.Join(StateEmpty(t.theme, w, h, "A rede ainda não foi lida.", steps...), "\n")
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
	switch {
	case t.asking && t.conf != nil:
		return t.conf.keyHints()
	case t.loading || t.querying:
		return []KeyHint{{"Esc", "cancelar"}}
	}
	keys := []KeyHint{act("c").Hint()}
	if t.loaded {
		keys = append([]KeyHint{{"↑↓", "rolar"}}, keys...)
	}
	if t.qbe != nil {
		keys = append(keys, act("o").Hint(), act("w").Hint())
	}
	return keys
}

// HelpKeys implementa SectionHelper: as teclas da seção, uma frase por tecla (legenda única em keys.go).
func (t *RedeTab) HelpKeys() []KeyDef {
	keys := []KeyDef{
		navDef("↑↓", "rolar", "rola o resumo e os dados da rede."),
		act("c").says("lê a rede (de novo); só leitura, sem varredura e sem acessar a internet."),
	}
	if t.qbe != nil {
		keys = append(keys,
			act("o").says("consulta o IP público num serviço que VOCÊ configurou (TOOLKIT_IP_URL); a confirmação nomeia o serviço e o que é enviado."),
			act("w").says("sonda o portal cativo num endereço que VOCÊ configurou (TOOLKIT_PORTAL_URL); a confirmação nomeia o serviço; nada é aberto."))
	}
	return append(keys, navDef("Esc", "cancelar", "cancela a leitura, a confirmação ou a consulta em andamento."))
}

// Status implementa Tab.
func (t *RedeTab) Status() string {
	switch {
	case t.asking && t.conf != nil:
		return t.conf.Hint()
	case t.querying:
		return t.theme.Spin(t.frame) + " Consultando o serviço… Esc cancela."
	case t.notice != "":
		return t.notice
	case t.loading:
		return "Lendo a rede…"
	case t.err != nil:
		return "Falhou. c lê de novo."
	case t.cancelled:
		return "Leitura cancelada. c lê de novo."
	case t.loaded:
		if t.qbe != nil {
			return "Rede — dados como o sistema informou; o e w consultam só sob pedido"
		}
		return "Rede — dados como o sistema informou; c lê de novo"
	}
	return "Nada lido ainda"
}

// More implementa Tab.
func (t *RedeTab) More() int {
	if t.loading || t.err != nil || !t.loaded || t.asking || t.querying {
		return 0
	}
	return max(len(t.wrapped())-(t.top+max(t.lastH, 1)), 0)
}

func (t *RedeTab) setNotice(n string) { t.notice = textsafe.Line(n) }

// askQuery abre a confirmação da consulta (nada é enviado aqui). Sem serviço configurado mostra como configurar; com URL
// inválida, o erro. O plano exibido fica guardado em t.plan e é o ÚNICO que a confirmação pode enviar.
func (t *RedeTab) askQuery(kind rede.QueryKind) tea.Cmd {
	if t.qbe == nil || t.loading {
		return nil
	}
	plan, err := t.qbe.PlanQuery(kind)
	switch {
	case errors.Is(err, rede.ErrNoService):
		help := rede.NoServiceHelp(kind, t.lang)
		t.res = &Result{Kind: ResultInfo, Title: help[0], Lines: help[1:]}
		return nil
	case errors.Is(err, rede.ErrInvalidService):
		t.res = &Result{Kind: ResultError, Title: "Serviço inválido: nada foi enviado", Lines: []string{"A URL configurada não é aceita: use https, host público, porta 443, sem usuário, sem # e sem IP literal nem localhost.", "Corrija TOOLKIT_IP_URL ou TOOLKIT_PORTAL_URL e tente de novo."}}
		return nil
	case err != nil:
		t.res = &Result{Kind: ResultError, Title: "Não foi possível preparar a consulta", Lines: []string{textsafe.Line(err.Error())}}
		return nil
	}
	t.plan = &plan
	t.asking, t.res = true, nil
	desc := plan.Describe(t.lang)
	c := NewConfirm("Consultar", "")
	c.Title = "Consultar o IP público?"
	if kind == rede.KindPortal {
		c.Title = "Sondar o portal cativo?"
	}
	c.Summary = desc[0]
	c.Warn = "isto revela o IP público desta conexão ao serviço acima. Cancelar (Esc) não envia nada."
	c.Text = strings.Join(desc[1:], "\n\n")
	t.conf = c
	return nil
}

// confirmKey entrega a tecla à confirmação: só o botão de confirmar ativo envia; Esc, n e Cancelar não enviam nada.
func (t *RedeTab) confirmKey(k string) tea.Cmd {
	switch t.conf.Update(k) {
	case ConfirmAccept:
		return t.sendQuery()
	case ConfirmCancel:
		t.asking, t.conf, t.plan = false, nil, nil
		t.setNotice("Cancelado: nada foi enviado.")
	}
	return nil
}

// sendQuery envia o plano exibido (uso único: o plano é zerado antes do envio).
func (t *RedeTab) sendQuery() tea.Cmd {
	p := t.plan
	t.asking, t.conf, t.plan = false, nil, nil
	if p == nil || t.qbe == nil {
		return nil
	}
	if t.qcancel != nil {
		t.qcancel()
	}
	ctx, cancel := context.WithCancel(t.ctx)
	t.qcancel = cancel
	t.gen++
	t.querying, t.notice = true, ""
	gen, be, plan := t.gen, t.qbe, *p
	return tea.Batch(func() tea.Msg { return netQueryMsg{gen: gen, res: be.Query(ctx, plan)} }, t.theme.Tick())
}

func (t *RedeTab) stopQuery() {
	if t.qcancel != nil {
		t.qcancel()
		t.qcancel = nil
	}
	t.gen++ // a resposta que ainda chegar é descartada
	t.querying = false
}

// onQueried guarda o resultado (só em memória), refaz o resumo se a rede já foi lida e mostra a faixa.
func (t *RedeTab) onQueried(r NetQueryResult) {
	t.querying = false
	var lines []string
	kind := ResultInfo
	switch {
	case r.IP != nil:
		t.pub = r.IP
		lines = r.IP.Lines(t.lang)
		if r.IP.Outcome == rede.OutcomeOK {
			kind = ResultOK
		}
	case r.Portal != nil:
		t.portal = r.Portal
		lines = r.Portal.Lines(t.lang)
		switch r.Portal.Outcome {
		case rede.OutcomeOK:
			kind = ResultOK
		case rede.OutcomePortal:
			kind = ResultPartial
		}
	default:
		t.res = &Result{Kind: ResultError, Title: "A consulta não devolveu resultado"}
		return
	}
	more := "Tecle c para ler a rede e cruzar o IP público com as portas e o firewall."
	if t.loaded && t.rep != nil {
		t.rep.PublicIP, t.rep.Portal = t.pub, t.portal
		t.lines = t.render(t.rep)
		t.wrapN = -1
		more = "O resumo abaixo já cruza o resultado com as portas e o firewall."
	}
	if r.Portal != nil {
		more = ""
	}
	t.res = &Result{Kind: kind, Title: textsafe.Line(lines[0]), Lines: cleanLines(append(lines[1:], more))}
}

func cleanLines(in []string) []string {
	var out []string
	for _, l := range in {
		if strings.TrimSpace(l) != "" {
			out = append(out, textsafe.Line(l))
		}
	}
	return out
}

// Result implementa Resulter: a faixa da última consulta.
func (t *RedeTab) Result() *Result { return t.res }

// Dismiss implementa Resulter.
func (t *RedeTab) Dismiss() { t.res = nil }

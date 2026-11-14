package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/PiluTech/toolkit/internal/rede"
)

// fakeNetQ é o backend com consulta: conta o que o planejamento e o envio fazem (nenhum teste acessa a internet).
type fakeNetQ struct {
	fakeNet
	plan       rede.Plan
	planErr    error
	planCalls  int
	queryCalls int
	got        []rede.Plan
	qctxs      []context.Context
	res        NetQueryResult
	nextURL    string // se não vazio, o PlanQuery seguinte devolve esta URL (a configuração mudou depois da confirmação)
}

func (f *fakeNetQ) PlanQuery(kind rede.QueryKind) (rede.Plan, error) {
	f.planCalls++
	if f.planErr != nil {
		return rede.Plan{}, f.planErr
	}
	p := f.plan
	p.Kind = kind
	if f.nextURL != "" {
		p.URL, p.Host = f.nextURL, "outro.example.org"
	}
	return p, nil
}

func (f *fakeNetQ) Query(ctx context.Context, p rede.Plan) NetQueryResult {
	f.queryCalls++
	f.got = append(f.got, p)
	f.qctxs = append(f.qctxs, ctx)
	return f.res
}

func ipPlan() rede.Plan {
	p, _ := rede.NewPlan(rede.KindIP, "https://ip.example.org/meu-ip")
	return p
}

func newNetQ(f *fakeNetQ) *RedeTab { return newNet(f) }

func okIP() NetQueryResult {
	return NetQueryResult{IP: &rede.PublicIPResult{Service: "ip.example.org", IP: "203.0.113.7", Outcome: rede.OutcomeOK}}
}

func TestRedeConsultaNadaSemConfirmacao(t *testing.T) {
	f := &fakeNetQ{fakeNet: fakeNet{rep: netReport()}, plan: ipPlan(), res: okIP()}
	tb := newNetQ(f)
	netKey(tb, "o")
	if !tb.asking || !tb.Capturing() || f.queryCalls != 0 {
		t.Fatalf("a tecla o só abre a confirmação: asking=%v calls=%d", tb.asking, f.queryCalls)
	}
	if tb.conf.Focus != 0 || tb.conf.Buttons[0].Label != "Cancelar" {
		t.Fatalf("o foco começa em Cancelar: %+v", tb.conf)
	}
	v := tb.View(80, 24)
	for _, w := range []string{"ip.example.org", "https://ip.example.org/meu-ip", "GET", "sem cookies", "IP público desta conexão", "Nada será enviado sem a sua confirmação", "Cancelar", "Consultar"} {
		if !strings.Contains(v, w) {
			t.Errorf("a confirmação não diz %q:\n%s", w, v)
		}
	}
	// Enter no foco (Cancelar), Esc e n cancelam sem enviar
	for _, k := range []string{"enter", "esc", "n"} {
		netKey(tb, "o")
		netKey(tb, k)
		if tb.asking || tb.plan != nil || f.queryCalls != 0 {
			t.Fatalf("%s: asking=%v calls=%d", k, tb.asking, f.queryCalls)
		}
	}
	if got := tb.Status(); !strings.Contains(got, "nada foi enviado") {
		t.Errorf("status: %q", got)
	}
}

func TestRedeConsultaConfirmaEnviaOPlanoExibido(t *testing.T) {
	f := &fakeNetQ{fakeNet: fakeNet{rep: netReport()}, plan: ipPlan(), res: okIP()}
	tb := newNetQ(f)
	netSettle(t, tb, netKey(tb, "c"))
	netKey(tb, "o")
	f.nextURL = "https://trocado.example.org/x" // a configuração muda DEPOIS de a confirmação mostrar o serviço
	cmd := netKey(tb, "s")
	if !tb.querying || tb.asking || tb.plan != nil {
		t.Fatalf("estado: querying=%v asking=%v plan=%v", tb.querying, tb.asking, tb.plan)
	}
	if v := tb.View(80, 24); !strings.Contains(v, "Lendo o serviço consultado") {
		t.Errorf("consultando:\n%s", v)
	}
	// uso único: um segundo "s" não envia de novo
	netKey(tb, "s")
	netSettle(t, tb, cmd)
	if f.queryCalls != 1 || f.got[0].URL != "https://ip.example.org/meu-ip" || f.got[0].Host != "ip.example.org" {
		t.Fatalf("só o plano exibido é enviado: calls=%d %+v", f.queryCalls, f.got)
	}
	if f.planCalls != 1 {
		t.Errorf("o plano é resolvido uma vez (planCalls=%d)", f.planCalls)
	}
	// resultado na faixa e achado no resumo
	r := tb.Result()
	if r == nil || r.Kind != ResultOK || !strings.Contains(r.Title, "203.0.113.7") {
		t.Fatalf("faixa: %+v", r)
	}
	all := strings.Join(tb.lines, "\n")
	for _, w := range []string{"IP público: 203.0.113.7", "Máquina exposta?", "Telnet 23", "o toolkit não testa de fora"} {
		if !strings.Contains(all, w) {
			t.Errorf("falta %q no resumo:\n%s", w, all)
		}
	}
	// ler de novo mantém o que foi consultado (só em memória)
	netSettle(t, tb, netKey(tb, "c"))
	if !strings.Contains(strings.Join(tb.lines, "\n"), "IP público: 203.0.113.7") {
		t.Error("c não pode apagar o IP consultado")
	}
	tb.Dismiss()
	if tb.Result() != nil {
		t.Error("x fecha a faixa")
	}
}

func TestRedeConsultaSemServico(t *testing.T) {
	f := &fakeNetQ{fakeNet: fakeNet{rep: netReport()}, planErr: rede.ErrNoService}
	tb := newNetQ(f)
	for k, want := range map[string]string{"o": "TOOLKIT_IP_URL", "w": "TOOLKIT_PORTAL_URL"} {
		netKey(tb, k)
		r := tb.Result()
		if tb.asking || f.queryCalls != 0 || r == nil || r.Kind != ResultInfo || !strings.Contains(r.Title, "nenhum serviço configurado") || !strings.Contains(strings.Join(r.Lines, " "), want) {
			t.Fatalf("%s: asking=%v res=%+v", k, tb.asking, r)
		}
		tb.Dismiss()
	}
}

func TestRedeConsultaServicoInvalido(t *testing.T) {
	f := &fakeNetQ{fakeNet: fakeNet{rep: netReport()}, planErr: rede.ErrInvalidService}
	tb := newNetQ(f)
	netKey(tb, "o")
	if r := tb.Result(); r == nil || r.Kind != ResultError || !strings.Contains(r.Title, "nada foi enviado") || tb.asking {
		t.Fatalf("%+v", r)
	}
	f.planErr = errors.New("falha\x1b[31m")
	netKey(tb, "o")
	if r := tb.Result(); r == nil || strings.Contains(strings.Join(r.Lines, ""), "\x1b") {
		t.Fatalf("erro cru sem limpar: %+v", r)
	}
}

func TestRedeConsultaEscCancelaAEnvioEmCurso(t *testing.T) {
	f := &fakeNetQ{fakeNet: fakeNet{rep: netReport()}, plan: ipPlan(), res: okIP()}
	tb := newNetQ(f)
	netKey(tb, "o")
	cmd := netKey(tb, "s")
	netKey(tb, "esc")
	if tb.querying {
		t.Fatal("Esc para a consulta")
	}
	msgs := dRunBatch(cmd)
	if f.qctxs[0].Err() == nil {
		t.Fatal("o contexto da consulta deveria estar cancelado")
	}
	for _, m := range msgs {
		tb.Update(m)
	}
	if tb.Result() != nil || tb.pub != nil {
		t.Fatal("a resposta de uma consulta cancelada é descartada")
	}
	if !strings.Contains(tb.Status(), "cancelada") {
		t.Errorf("status: %q", tb.Status())
	}
}

func TestRedeConsultaPortal(t *testing.T) {
	pp, _ := rede.NewPlan(rede.KindPortal, "https://portal.example.org/gen204")
	f := &fakeNetQ{fakeNet: fakeNet{rep: netReport()}, plan: pp, res: NetQueryResult{Portal: &rede.PortalResult{Service: "portal.example.org", Outcome: rede.OutcomePortal, HTTPStatus: 302, Reason: "redirect", RedirectHost: "login.hotel.example"}}}
	tb := newNetQ(f)
	netKey(tb, "w")
	v := tb.View(80, 24)
	for _, w := range []string{"SONDAR O PORTAL CATIVO?", "portal.example.org", "não segue redirecionamento"} {
		if !strings.Contains(v, w) {
			t.Errorf("falta %q:\n%s", w, v)
		}
	}
	if f.queryCalls != 0 {
		t.Fatal("nada enviado antes de confirmar")
	}
	netSettle(t, tb, netKey(tb, "s"))
	r := tb.Result()
	if r == nil || r.Kind != ResultPartial || !strings.Contains(r.Title, "possível portal cativo") || !strings.Contains(strings.Join(r.Lines, "\n"), "login.hotel.example") {
		t.Fatalf("%+v", r)
	}
	if f.got[0].Kind != rede.KindPortal {
		t.Errorf("tipo: %v", f.got[0].Kind)
	}
}

func TestRedeConsultaTextoHostilNaFaixa(t *testing.T) {
	f := &fakeNetQ{fakeNet: fakeNet{rep: netReport()}, plan: ipPlan(),
		res: NetQueryResult{Portal: &rede.PortalResult{Service: "a\x1b[2J\u202eb", Outcome: rede.OutcomePortal, Reason: "redirect", RedirectHost: "x\x1b]0;preso\x07y"}}}
	tb := newNetQ(f)
	netSettle(t, tb, netKey(tb, "c"))
	netKey(tb, "w")
	netSettle(t, tb, netKey(tb, "s"))
	r := tb.Result()
	if r == nil {
		t.Fatal("sem faixa")
	}
	all := r.Title + strings.Join(r.Lines, "")
	if strings.ContainsAny(all, "\x1b\x07\u202e") {
		t.Errorf("controle na faixa: %q", all)
	}
	if out := tb.View(80, 24); strings.ContainsAny(out, "\x1b\x07\u202e") {
		t.Errorf("controle no corpo: %q", out)
	}
}

func TestRedeSemBackendDeConsultaNaoTemAsTeclas(t *testing.T) {
	tb := newNet(&fakeNet{rep: netReport()})
	netKey(tb, "o")
	netKey(tb, "w")
	if tb.asking || tb.Result() != nil {
		t.Fatal("o backend sem consulta ignora o e w")
	}
	for _, h := range tb.Keys() {
		if h.Key == "o" || h.Key == "w" {
			t.Errorf("tecla %q sem backend", h.Key)
		}
	}
}

func TestRedeConsultaTeclasEAjuda(t *testing.T) {
	f := &fakeNetQ{fakeNet: fakeNet{rep: netReport()}, plan: ipPlan()}
	tb := newNetQ(f)
	got := map[string]string{}
	for _, h := range tb.Keys() {
		got[h.Key] = h.Label
	}
	if got["o"] != "IP público…" || got["w"] != "portal cativo…" || got["c"] != "ler de novo" {
		t.Errorf("barra: %v", got)
	}
	help := map[string]bool{}
	for _, h := range tb.HelpKeys() {
		if strings.TrimSpace(h.Help) == "" {
			t.Errorf("%q sem frase", h.Key)
		}
		help[h.Key] = true
	}
	if !help["o"] || !help["w"] {
		t.Errorf("ajuda: %v", help)
	}
	netKey(tb, "o")
	if k := tb.Keys(); len(k) == 0 || !strings.Contains(fmt.Sprint(k), "confirma") {
		t.Errorf("com a confirmação aberta as teclas são as dela: %v", k)
	}
}

func TestRedeConsultaTelaNaoEstoura(t *testing.T) {
	f := &fakeNetQ{fakeNet: fakeNet{rep: netReport()}, plan: ipPlan(), res: okIP()}
	for _, sz := range [][2]int{{60, 16}, {60, 17}, {80, 24}, {120, 40}} {
		m := netQModel(t, f, sz[0], sz[1])
		for _, state := range []string{"confirma", "faixa"} {
			if state == "faixa" {
				m.tabs[0].(*RedeTab).res = &Result{Kind: ResultOK, Title: "IP público: 203.0.113.7", Lines: []string{"a", "b"}}
				m.tabs[0].(*RedeTab).asking, m.tabs[0].(*RedeTab).conf = false, nil
			}
			rows := strings.Split(m.render(), "\n")
			if len(rows) != sz[1] {
				t.Errorf("%v %s: %d linhas", sz, state, len(rows))
			}
			for _, r := range rows {
				if ansi.StringWidth(r) > sz[0] {
					t.Errorf("%v %s: larga demais: %q", sz, state, r)
				}
			}
		}
	}
}

func netQModel(t *testing.T, f *fakeNetQ, w, h int) *Model {
	t.Helper()
	tb := newNetQ(f)
	m := sized(Options{Version: "v0.1.0", Mono: true, NoMotion: true}, w, h, tb)
	netSettle(t, tb, netKey(tb, "c"))
	netKey(tb, "o")
	return m
}

func TestGoldenRedeConsulta(t *testing.T) {
	f := &fakeNetQ{fakeNet: fakeNet{rep: netReport()}, plan: ipPlan(), res: okIP()}
	for _, sz := range [][2]int{{80, 24}, {120, 40}, {60, 16}} {
		golden(t, fmt.Sprintf("rede_consulta_confirma_%dx%d", sz[0], sz[1]), netQModel(t, f, sz[0], sz[1]).render())
	}
	// sem serviço: faixa com os passos
	g := &fakeNetQ{fakeNet: fakeNet{rep: netReport()}, planErr: rede.ErrNoService}
	tb := newNetQ(g)
	m := sized(Options{Version: "v0.1.0", Mono: true, NoMotion: true}, 80, 24, tb)
	netSettle(t, tb, netKey(tb, "c"))
	netKey(tb, "o")
	golden(t, "rede_consulta_sem_servico_80x24", m.render())
	// resultado: IP e achado "máquina exposta?"
	h := &fakeNetQ{fakeNet: fakeNet{rep: netReport()}, plan: ipPlan(), res: okIP()}
	tb = newNetQ(h)
	m = sized(Options{Version: "v0.1.0", Mono: true, NoMotion: true}, 80, 24, tb)
	netSettle(t, tb, netKey(tb, "c"))
	netKey(tb, "o")
	netSettle(t, tb, netKey(tb, "s"))
	golden(t, "rede_consulta_resultado_80x24", m.render())
	golden(t, "rede_consulta_vazio_80x24", func() string {
		tb := newNetQ(&fakeNetQ{fakeNet: fakeNet{rep: netReport()}})
		return sized(Options{Version: "v0.1.0", Mono: true, NoMotion: true}, 80, 24, tb).render()
	}())
}

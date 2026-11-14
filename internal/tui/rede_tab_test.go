package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/PiluTech/toolkit/internal/diagfmt"
	"github.com/PiluTech/toolkit/internal/rede"
)

type fakeNet struct {
	rep   *rede.Report
	err   error
	calls int
	ctxs  []context.Context
}

func (f *fakeNet) Load(ctx context.Context) (*rede.Report, error) {
	f.calls++
	f.ctxs = append(f.ctxs, ctx)
	return f.rep, f.err
}

// netReport é uma leitura SINTÉTICA (documentação: 192.0.2.x, 198.51.100.x, 203.0.113.x, 2001:db8::, REDE-FAKE).
func netReport() *rede.Report {
	return &rede.Report{
		OS: "linux", Generated: time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC),
		Interfaces: []rede.Interface{{Name: "wlan0", Kind: "wifi", MAC: "02:00:00:00:00:01", Up: rede.Yes, IPv4: []rede.Addr{{IP: "192.0.2.10", PrefixLen: 24}}}},
		Gateways:   []string{"192.0.2.1"}, GatewaysRead: true,
		WiFi:      &rede.WiFi{Interface: "wlan0", SSID: "REDE-FAKE", Security: rede.SecOpen, Raw: "--"},
		DNS:       rede.DNS{Servers: []rede.DNSServer{{Addr: "203.0.113.53"}}, Encrypted: rede.No},
		Listeners: []rede.Listener{{Proto: "tcp", Addr: "0.0.0.0", Port: 23, Process: "telnetd", PID: 7}, {Proto: "tcp", Addr: "127.0.0.1", Port: 631}}, ListenersRead: true,
		Firewalls: []rede.Firewall{{Name: "ufw", Enabled: rede.No}},
		Skipped:   []rede.Skipped{{Area: "processos-das-portas", Reason: "sem administrador o ss só mostra o seu processo", NeedsAdmin: true}},
	}
}

func newNet(be NetBackend) *RedeTab {
	return NewRedeTab(context.Background(), be, Theme{Mono: true, NoMotion: true}, diagfmt.PT, "Análise", "Anál")
}

func netKey(tb *RedeTab, keys ...string) tea.Cmd {
	var last tea.Cmd
	for _, k := range keys {
		var m tea.KeyPressMsg
		switch k {
		case "esc":
			m = tea.KeyPressMsg{Code: tea.KeyEscape}
		case "enter":
			m = tea.KeyPressMsg{Code: tea.KeyEnter}
		case "down":
			m = tea.KeyPressMsg{Code: tea.KeyDown}
		default:
			m = tea.KeyPressMsg{Code: rune(k[0]), Text: k}
		}
		_, last = tb.Update(m)
	}
	return last
}

func netSettle(t *testing.T, tb *RedeTab, cmd tea.Cmd) {
	t.Helper()
	if cmd == nil {
		t.Fatal("sem comando")
	}
	for _, m := range dRunBatch(cmd) {
		tb.Update(m)
	}
}

func TestRedeTabNaoLeNadaAoAbrir(t *testing.T) {
	fn := &fakeNet{rep: netReport()}
	tb := newNet(fn)
	if tb.Init() != nil || fn.calls != 0 {
		t.Fatal("abrir o toolkit não pode rodar os comandos de rede")
	}
	v := tb.View(80, 10)
	for _, w := range []string{"i Nada por aqui", "A rede ainda não foi lida.", "1. Tecle c para ler a rede"} {
		if !strings.Contains(v, w) {
			t.Errorf("falta %q:\n%s", w, v)
		}
	}
	if tb.Title() != "Análise" || tb.Short() != "Anál" || tb.Capturing() || tb.Selected() != 0 || tb.More() != 0 {
		t.Fatal("contrato da Tab")
	}
}

func TestRedeTabLeResumoEDados(t *testing.T) {
	fn := &fakeNet{rep: netReport()}
	tb := newNet(fn)
	cmd := netKey(tb, "c")
	if v := tb.View(80, 10); !strings.Contains(v, "Carregando") || !strings.Contains(v, "Lendo a rede") || !strings.Contains(v, "Esc cancela.") {
		t.Fatalf("estado carregando:\n%s", v)
	}
	netSettle(t, tb, cmd)
	if fn.calls != 1 {
		t.Fatalf("calls=%d", fn.calls)
	}
	all := strings.Join(tb.lines, "\n")
	for _, w := range []string{"Análise de rede (Linux)", "alta", "Rede Wi-Fi aberta", "REDE-FAKE", "Telnet", "O que fazer:", "192.0.2.10", "02:00:00:00:00:01", "telnetd", "Firewall desligado", "IP público: não consultado", "Não lidos: 1", "Com administrador a análise também lê"} {
		if !strings.Contains(all, w) {
			t.Errorf("falta %q:\n%s", w, all)
		}
	}
	// resumo e achados no TOPO; dados completos depois
	if strings.Index(all, "Rede Wi-Fi aberta") > strings.Index(all, "== Interfaces ==") {
		t.Error("o resumo vem antes dos dados")
	}
	if got := tb.Status(); !strings.Contains(got, "c lê de novo") {
		t.Errorf("status: %q", got)
	}
	// rola
	tb.View(80, 6)
	if tb.More() == 0 {
		t.Fatal("More deveria contar linhas abaixo da dobra")
	}
	before := tb.top
	netKey(tb, "down", "down")
	if tb.top != before+2 {
		t.Fatalf("down: %d -> %d", before, tb.top)
	}
	netKey(tb, "G")
	if tb.More() != 0 {
		t.Fatalf("G vai ao fim, More=%d", tb.More())
	}
	netKey(tb, "g")
	if tb.top != 0 {
		t.Fatal("g volta ao topo")
	}
	// c lê de novo (e Enter também)
	netSettle(t, tb, netKey(tb, "c"))
	netSettle(t, tb, netKey(tb, "enter"))
	if fn.calls != 3 {
		t.Fatalf("recarregar: calls=%d", fn.calls)
	}
}

func TestRedeTabErroERecarregar(t *testing.T) {
	fn := &fakeNet{err: errors.New("falhou\x1b[31m feio")}
	tb := newNet(fn)
	netSettle(t, tb, netKey(tb, "c"))
	v := tb.View(80, 10)
	if !strings.Contains(v, "✗ Erro") || !strings.Contains(v, "Não deu para ler a rede.") || !strings.Contains(v, "1. Tente de novo (c).") || !strings.Contains(v, "Nada foi alterado.") || strings.Contains(v, "\x1b") {
		t.Fatalf("estado de erro:\n%q", v)
	}
	fn.err, fn.rep = nil, netReport()
	netSettle(t, tb, netKey(tb, "c"))
	if !strings.Contains(strings.Join(tb.lines, "\n"), "REDE-FAKE") {
		t.Fatal("depois do erro, c lê de novo")
	}
}

func TestRedeTabRelatorioVazioEhErro(t *testing.T) {
	tb := newNet(&fakeNet{})
	netSettle(t, tb, netKey(tb, "c"))
	if v := tb.View(80, 10); !strings.Contains(v, "✗ Erro") {
		t.Fatalf("relatório vazio deve ser erro:\n%s", v)
	}
}

func TestRedeTabEscCancelaALeitura(t *testing.T) {
	fn := &fakeNet{rep: netReport()}
	tb := newNet(fn)
	cmd := netKey(tb, "c")
	netKey(tb, "esc")
	if tb.loading {
		t.Fatal("Esc deveria parar o estado carregando")
	}
	if v := tb.View(80, 10); !strings.Contains(v, "cancelada") || !strings.Contains(v, "Tecle c") {
		t.Fatalf("depois de cancelar:\n%s", v)
	}
	msgs := dRunBatch(cmd)
	if fn.ctxs[0].Err() == nil {
		t.Fatal("o contexto da leitura deveria estar cancelado")
	}
	for _, m := range msgs {
		tb.Update(m)
	}
	if tb.loaded {
		t.Fatal("a resposta de uma leitura cancelada é descartada")
	}
	// Esc fora da leitura não faz nada
	netSettle(t, tb, netKey(tb, "c"))
	netKey(tb, "esc")
	if !tb.loaded {
		t.Fatal("Esc com o resultado na tela não apaga nada")
	}
}

func TestRedeTabTeclasDaLegenda(t *testing.T) {
	tb := newNet(&fakeNet{rep: netReport()})
	has := func(keys []KeyHint, k string) bool {
		for _, h := range keys {
			if h.Key == k {
				return true
			}
		}
		return false
	}
	if !has(tb.Keys(), "c") {
		t.Errorf("vazio: %v", tb.Keys())
	}
	cmd := netKey(tb, "c")
	if k := tb.Keys(); len(k) != 1 || k[0].Key != "Esc" {
		t.Errorf("lendo: %v", k)
	}
	for _, m := range dRunBatch(cmd) {
		tb.Update(m)
	}
	if k := tb.Keys(); !has(k, "c") || !has(k, "↑↓") {
		t.Errorf("com resultado: %v", k)
	}
	for _, h := range tb.Keys() {
		if h.Key == "c" && h.Label != "ler de novo" {
			t.Errorf("c = %q, a legenda diz 'ler de novo'", h.Label)
		}
		if h.Key == "d" {
			t.Error("d é desfazer em toda parte: a seção não o usa")
		}
	}
	help := tb.HelpKeys()
	if len(help) != 3 {
		t.Fatalf("ajuda: %v", help)
	}
	for _, h := range help {
		if strings.TrimSpace(h.Help) == "" {
			t.Errorf("%q sem frase", h.Key)
		}
	}
}

// SSID e nome de processo com ESC, OSC, bidi e CR nunca chegam ao terminal como controle (nem quebram o layout).
func TestRedeTabTextoMaliciosoDoSistema(t *testing.T) {
	rep := netReport()
	rep.WiFi.SSID = "A\x1b[2J\x1b]0;preso\x07B\u202eC\rD"
	rep.Listeners[0].Process = "x\x1b[31m\u202e" + strings.Repeat("Longo-", 30)
	tb := newNet(&fakeNet{rep: rep})
	netSettle(t, tb, netKey(tb, "c"))
	for _, sz := range [][2]int{{60, 16}, {80, 24}, {120, 40}} {
		out := tb.View(sz[0], sz[1])
		if strings.ContainsAny(out, "\x1b\x07\r\u202e") {
			t.Fatalf("%v: controle na saída: %q", sz, out)
		}
		rows := strings.Split(out, "\n")
		if len(rows) > sz[1] {
			t.Errorf("%v: %d linhas", sz, len(rows))
		}
		for _, r := range rows {
			if ansi.StringWidth(r) > sz[0] {
				t.Errorf("%v: linha larga demais (%d): %q", sz, ansi.StringWidth(r), r)
			}
		}
	}
	if !strings.Contains(strings.Join(tb.lines, "\n"), `\x1b`) {
		t.Error("o escape deve aparecer de forma visível e inerte (\\x1b)")
	}
}

func netModel(t *testing.T, w, h int, loaded bool) *Model {
	t.Helper()
	tb := newNet(&fakeNet{rep: netReport()})
	m := sized(Options{Version: "v0.1.0", Mono: true, NoMotion: true}, w, h, tb)
	if loaded {
		for _, msg := range dRunBatch(netKey(tb, "c")) {
			tb.Update(msg)
		}
	}
	return m
}

func TestGoldenRedeAnalise(t *testing.T) {
	for _, sz := range [][2]int{{80, 24}, {120, 40}, {60, 16}} {
		golden(t, fmt.Sprintf("rede_analise_%dx%d", sz[0], sz[1]), netModel(t, sz[0], sz[1], true).render())
	}
	golden(t, "rede_analise_vazio_80x24", netModel(t, 80, 24, false).render())
}

func TestRedeTabNaRaizNaoPerdeLinhasNemLargura(t *testing.T) {
	for _, sz := range [][2]int{{60, 16}, {60, 17}, {80, 24}, {120, 40}} {
		out := netModel(t, sz[0], sz[1], true).render()
		rows := strings.Split(out, "\n")
		if len(rows) != sz[1] {
			t.Errorf("%v: %d linhas", sz, len(rows))
		}
		for _, r := range rows {
			if ansi.StringWidth(r) > sz[0] {
				t.Errorf("%v: linha larga demais: %q", sz, r)
			}
		}
	}
}

// loadedMsgForTest dá à seção a resposta de uma leitura (só nos testes de teclas, sem passar pelo comando).
func (t *RedeTab) loadedMsgForTest(rep *rede.Report) tea.Msg {
	t.gen++
	return netLoadedMsg{gen: t.gen, rep: rep}
}

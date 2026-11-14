package rede

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/PiluTech/toolkit/internal/diagfmt"
)

// fakeFetcher é o cliente HTTP falso: conta as chamadas e devolve o roteiro. Nenhum teste deste pacote faz requisição real.
type fakeFetcher struct {
	calls int
	urls  []string
	resp  Response
	err   error
	hold  bool // espera o ctx expirar (prazo)
}

func (f *fakeFetcher) Get(ctx context.Context, u string) (Response, error) {
	f.calls++
	f.urls = append(f.urls, u)
	if f.hold {
		<-ctx.Done()
		return Response{}, fmt.Errorf("get: %w", ctx.Err())
	}
	return f.resp, f.err
}

const svc = "https://ip.example.org/meu-ip"

func TestNewPlanSemURLNaoTemServico(t *testing.T) {
	for _, u := range []string{"", "   "} {
		if _, err := NewPlan(KindIP, u); !errors.Is(err, ErrNoService) {
			t.Errorf("%q: %v, quero ErrNoService", u, err)
		}
	}
}

func TestNewPlanValidaAURL(t *testing.T) {
	bad := []string{
		"http://ip.example.org/x",            // sem TLS
		"ftp://ip.example.org/x",             // outro esquema
		"https://ip.example.org:8443/x",      // porta
		"https://u:p@ip.example.org/x",       // userinfo
		"https://ip.example.org/x#frag",      // fragmento
		"https://203.0.113.7/x",              // IP literal
		"https://[2001:db8::1]/x",            // IPv6 literal
		"https://localhost/x",                // localhost
		"https://intranet/x",                 // sem ponto
		"https://ip.example.org/a b",         // espaço
		"https://ip.example.org/\x1b[2J",     // controle
		"https://ip.example.org/\u202ex",     // bidi
		"https://ip.example.org/{{versao}}",  // template
		"ip.example.org/x",                   // sem esquema
		"https://ip.example.org\\@evil.test", // barra invertida
	}
	for _, u := range bad {
		if _, err := NewPlan(KindIP, u); !errors.Is(err, ErrInvalidService) {
			t.Errorf("%q deveria ser recusada com ErrInvalidService, veio %v", u, err)
		}
	}
	p, err := NewPlan(KindIP, "  "+svc+"  ")
	if err != nil {
		t.Fatal(err)
	}
	if p.Host != "ip.example.org" || p.URL != svc || p.Kind != KindIP {
		t.Errorf("plano: %+v", p)
	}
}

func TestPlanDescreveOQueVaiSerEnviado(t *testing.T) {
	p, _ := NewPlan(KindIP, "https://ip.example.org/meu-ip?token=SEGREDO")
	txt := strings.Join(p.Describe(diagfmt.PT), "\n")
	for _, want := range []string{"ip.example.org", "GET", "User-Agent", "toolkit", "sem cookies", "sem corpo", "IP público", "Nada será enviado sem a sua confirmação"} {
		if !strings.Contains(txt, want) {
			t.Errorf("falta %q:\n%s", want, txt)
		}
	}
	if strings.Contains(txt, "SEGREDO") {
		t.Errorf("a query da URL pode ter segredo: não vai para a tela:\n%s", txt)
	}
	if !strings.Contains(txt, "parâmetros") {
		t.Errorf("avisa que há parâmetros na URL:\n%s", txt)
	}
	pp, _ := NewPlan(KindPortal, "https://portal.example.org/gen204")
	if txt := strings.Join(pp.Describe(diagfmt.PT), "\n"); !strings.Contains(txt, "portal.example.org") || !strings.Contains(txt, "não segue redirecionamento") {
		t.Errorf("portal:\n%s", txt)
	}
	// os 3 idiomas
	for _, l := range []Lang{diagfmt.EN, diagfmt.ES} {
		if strings.Join(p.Describe(l), "") == strings.Join(p.Describe(diagfmt.PT), "") {
			t.Errorf("idioma %v não traduzido", l)
		}
	}
}

func TestSemServicoEnsinaAConfigurar(t *testing.T) {
	ip := strings.Join(NoServiceHelp(KindIP, diagfmt.PT), "\n")
	for _, w := range []string{"nenhum serviço configurado", "TOOLKIT_IP_URL", "--servico-ip", "https://", "painel do roteador"} {
		if !strings.Contains(ip, w) {
			t.Errorf("ip: falta %q:\n%s", w, ip)
		}
	}
	pt := strings.Join(NoServiceHelp(KindPortal, diagfmt.PT), "\n")
	for _, w := range []string{"nenhum serviço configurado", "TOOLKIT_PORTAL_URL", "--servico-portal", "204"} {
		if !strings.Contains(pt, w) {
			t.Errorf("portal: falta %q:\n%s", w, pt)
		}
	}
}

func TestQueryPublicIPValida(t *testing.T) {
	ok := []struct{ body, want string }{
		{"203.0.113.7\n", "203.0.113.7"},
		{"  198.51.100.9  \r\n", "198.51.100.9"},
		{"2001:db8::1234", "2001:db8::1234"},
		{"2001:0DB8:0000:0000:0000:0000:0000:0001", "2001:db8::1"},
		{"::ffff:203.0.113.8", "203.0.113.8"},
	}
	for _, c := range ok {
		f := &fakeFetcher{resp: Response{Status: 200, Body: []byte(c.body)}}
		r := QueryPublicIP(context.Background(), f, svc)
		if r.Outcome != OutcomeOK || r.IP != c.want || r.Service != "ip.example.org" || f.calls != 1 {
			t.Errorf("%q: %+v calls=%d", c.body, r, f.calls)
		}
		if f.urls[0] != svc {
			t.Errorf("url pedida: %q", f.urls[0])
		}
	}
}

func TestQueryPublicIPRespostaInesperada(t *testing.T) {
	cases := []struct {
		name   string
		resp   Response
		reason string
	}{
		{"privado", Response{Status: 200, Body: []byte("192.168.0.5")}, "non-public"},
		{"loopback", Response{Status: 200, Body: []byte("127.0.0.1")}, "non-public"},
		{"cgnat", Response{Status: 200, Body: []byte("100.64.1.2")}, "non-public"},
		{"linklocal", Response{Status: 200, Body: []byte("169.254.1.1")}, "non-public"},
		{"nao especificado", Response{Status: 200, Body: []byte("0.0.0.0")}, "non-public"},
		{"v6 privado", Response{Status: 200, Body: []byte("fd00::1")}, "non-public"},
		{"v6 loopback", Response{Status: 200, Body: []byte("::1")}, "non-public"},
		{"lixo", Response{Status: 200, Body: []byte("<html>oi</html>")}, "not-ip"},
		{"vazio", Response{Status: 200, Body: nil}, "not-ip"},
		{"ip com texto", Response{Status: 200, Body: []byte("seu ip: 203.0.113.7")}, "not-ip"},
		{"dois ips", Response{Status: 200, Body: []byte("203.0.113.7\n198.51.100.1")}, "not-ip"},
		{"com zona", Response{Status: 200, Body: []byte("fe80::1%eth0")}, "non-public"},
		{"controle", Response{Status: 200, Body: []byte("203.0.113.7\x1b[2J")}, "not-ip"},
		{"grande", Response{Status: 200, Body: []byte(strings.Repeat("1", MaxBody+1))}, "too-large"},
		{"truncado", Response{Status: 200, Body: []byte("203.0.113.7"), Truncated: true}, "too-large"},
		{"404", Response{Status: 404, Body: []byte("203.0.113.7")}, "status"},
		{"500", Response{Status: 500}, "status"},
		{"redirecionamento", Response{Status: 302, Location: "https://evil.test/x", Body: []byte("203.0.113.7")}, "redirect"},
	}
	for _, c := range cases {
		f := &fakeFetcher{resp: c.resp}
		r := QueryPublicIP(context.Background(), f, svc)
		if r.Outcome != OutcomeUnexpected || r.Reason != c.reason || r.IP != "" {
			t.Errorf("%s: %+v", c.name, r)
		}
		if f.calls != 1 {
			t.Errorf("%s: calls=%d", c.name, f.calls)
		}
	}
}

func TestQueryPublicIPFalhas(t *testing.T) {
	// URL inválida: nenhuma chamada
	for _, u := range []string{"", "http://ip.example.org/x", "https://203.0.113.7/", "https://ip.example.org:81/"} {
		f := &fakeFetcher{resp: Response{Status: 200, Body: []byte("203.0.113.7")}}
		r := QueryPublicIP(context.Background(), f, u)
		if f.calls != 0 || r.Outcome != OutcomeFailed {
			t.Errorf("%q: calls=%d %+v", u, f.calls, r)
		}
	}
	// sem cliente
	if r := QueryPublicIP(context.Background(), nil, svc); r.Outcome != OutcomeFailed || r.Reason != "no-client" {
		t.Errorf("%+v", r)
	}
	// erro de conexão: o texto do erro (que pode ter a URL) não vai para o resultado
	f := &fakeFetcher{err: errors.New("dial tcp: lookup ip.example.org?token=SEGREDO: no such host")}
	r := QueryPublicIP(context.Background(), f, svc)
	if r.Outcome != OutcomeFailed || r.Reason != "connect" {
		t.Errorf("%+v", r)
	}
	if strings.Contains(fmt.Sprintf("%+v", r), "SEGREDO") {
		t.Errorf("o erro cru não pode vazar: %+v", r)
	}
	// endereço privado recusado pelo cliente
	f = &fakeFetcher{err: fmt.Errorf("dial: %w", ErrNonPublicAddress)}
	if r := QueryPublicIP(context.Background(), f, svc); r.Reason != "private-addr" {
		t.Errorf("%+v", r)
	}
	// prazo
	f = &fakeFetcher{hold: true}
	start := time.Now()
	oldTimeout := queryTimeout
	queryTimeout = 30 * time.Millisecond
	defer func() { queryTimeout = oldTimeout }()
	r = QueryPublicIP(context.Background(), f, svc)
	if r.Outcome != OutcomeFailed || r.Reason != "timeout" || time.Since(start) > 2*time.Second {
		t.Errorf("%+v em %v", r, time.Since(start))
	}
	// cancelado pela pessoa
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	f = &fakeFetcher{hold: true}
	if r := QueryPublicIP(ctx, f, svc); r.Reason != "canceled" {
		t.Errorf("%+v", r)
	}
}

func TestProbePortal(t *testing.T) {
	const purl = "https://portal.example.org/gen204"
	cases := []struct {
		name     string
		resp     Response
		outcome  Outcome
		redirect string
	}{
		{"livre", Response{Status: 204}, OutcomeOK, ""},
		{"redireciona", Response{Status: 302, Location: "http://login.hotel.example/entrar?x=1"}, OutcomePortal, "login.hotel.example"},
		{"redireciona relativo", Response{Status: 307, Location: "/entrar"}, OutcomePortal, ""},
		{"redireciona com bidi e ESC", Response{Status: 302, Location: "https://a\u202eb\x1b[2J.example/x"}, OutcomePortal, ""},
		{"200 em vez de 204", Response{Status: 200, Body: []byte("<html>Faça login</html>")}, OutcomePortal, ""},
		{"511", Response{Status: 511}, OutcomePortal, ""},
		{"404", Response{Status: 404}, OutcomeUnexpected, ""},
		{"503", Response{Status: 503}, OutcomeUnexpected, ""},
	}
	for _, c := range cases {
		f := &fakeFetcher{resp: c.resp}
		r := ProbePortal(context.Background(), f, purl)
		if r.Outcome != c.outcome || f.calls != 1 || r.Service != "portal.example.org" {
			t.Errorf("%s: %+v calls=%d", c.name, r, f.calls)
		}
		if c.redirect != "" && r.RedirectHost != c.redirect {
			t.Errorf("%s: redirect=%q", c.name, r.RedirectHost)
		}
		for _, r2 := range r.RedirectHost {
			if r2 < 0x20 || r2 == 0x7f || r2 == 0x202e {
				t.Errorf("%s: host de redirecionamento com caractere de controle: %q", c.name, r.RedirectHost)
			}
		}
	}
	// URL inválida e falha de conexão
	f := &fakeFetcher{}
	if r := ProbePortal(context.Background(), f, "http://portal.example.org/"); f.calls != 0 || r.Outcome != OutcomeFailed {
		t.Errorf("http: %+v calls=%d", r, f.calls)
	}
	f = &fakeFetcher{err: errors.New("boom")}
	if r := ProbePortal(context.Background(), f, purl); r.Outcome != OutcomeFailed || r.Reason != "connect" {
		t.Errorf("%+v", r)
	}
}

func TestTextoDoResultadoNuncaTrazOCorpo(t *testing.T) {
	f := &fakeFetcher{resp: Response{Status: 200, Body: []byte("\x1b[2J\u202e<script>")}}
	r := QueryPublicIP(context.Background(), f, svc)
	for _, l := range []Lang{diagfmt.PT, diagfmt.EN, diagfmt.ES} {
		txt := strings.Join(r.Lines(l), "\n")
		if strings.ContainsAny(txt, "\x1b\u202e<") {
			t.Errorf("%v: o corpo cru apareceu: %q", l, txt)
		}
		if !strings.Contains(txt, "ip.example.org") {
			t.Errorf("%v: nomeia o serviço: %q", l, txt)
		}
	}
	p := ProbePortal(context.Background(), &fakeFetcher{resp: Response{Status: 302, Location: "https://x.example/"}}, "https://portal.example.org/g")
	if txt := strings.Join(p.Lines(diagfmt.PT), "\n"); !strings.Contains(txt, "possível portal cativo") || !strings.Contains(txt, "não abre") {
		t.Errorf("portal: %q", txt)
	}
}

func baseExposure() *Report {
	return &Report{OS: "linux", GatewaysRead: true, Gateways: []string{"192.0.2.1"},
		Interfaces: []Interface{{Name: "eth0", Kind: "ethernet", Up: Yes, IPv4: []Addr{{IP: "192.0.2.10", PrefixLen: 24}}}},
		Listeners:  []Listener{{Proto: "tcp", Addr: "0.0.0.0", Port: 22, Process: "sshd"}, {Proto: "tcp", Addr: "127.0.0.1", Port: 5432}}, ListenersRead: true,
		Firewalls: []Firewall{{Name: "ufw", Enabled: No}},
		PublicIP:  &PublicIPResult{Service: "ip.example.org", IP: "203.0.113.7", Outcome: OutcomeOK}}
}

func findingByID(fs []Finding, id string) *Finding {
	for i := range fs {
		if fs[i].ID == id {
			return &fs[i]
		}
	}
	return nil
}

func TestAnalyzeSemPedidoNaoConsulta(t *testing.T) {
	r := baseExposure()
	r.PublicIP = nil
	fs := Analyze(r, diagfmt.PT)
	f := findingByID(fs, "ip-publico")
	if f == nil || !strings.Contains(f.Title, "não consultado") || !strings.Contains(f.Action, "toolkit rede ip") {
		t.Fatalf("%+v", f)
	}
	if findingByID(fs, "maquina-exposta") != nil || findingByID(fs, "portal-cativo") != nil {
		t.Error("sem consulta não há achado de exposição nem de portal")
	}
}

func TestMaquinaExpostaCruzaPortasEFirewall(t *testing.T) {
	r := baseExposure()
	fs := Analyze(r, diagfmt.PT)
	ip := findingByID(fs, "ip-publico")
	if ip == nil || !strings.Contains(ip.Title, "203.0.113.7") || !strings.Contains(ip.Title, "ip.example.org") {
		t.Fatalf("ip: %+v", ip)
	}
	ex := findingByID(fs, "maquina-exposta")
	if ex == nil {
		t.Fatal("sem achado de exposição")
	}
	txt := ex.Title + " " + ex.Detail + " " + ex.Action
	for _, w := range []string{"SSH", "22", "firewall está desligado", "o toolkit não testa de fora", "só cruza"} {
		if !strings.Contains(txt, w) {
			t.Errorf("falta %q: %s", w, txt)
		}
	}
	if ex.Severity != SevMedium {
		t.Errorf("porta sensível com firewall desligado: %v", ex.Severity)
	}
	if strings.Contains(txt, "5432") {
		t.Errorf("a porta em 127.0.0.1 não conta: %s", txt)
	}
	// firewall ligado: um nível a menos
	r.Firewalls[0].Enabled = Yes
	if ex := findingByID(Analyze(r, diagfmt.PT), "maquina-exposta"); ex.Severity != SevLow {
		t.Errorf("fw ligado: %v", ex.Severity)
	}
	// firewall desconhecido nunca rebaixa
	r.Firewalls[0].Enabled = Unknown
	if ex := findingByID(Analyze(r, diagfmt.PT), "maquina-exposta"); ex.Severity != SevMedium || !strings.Contains(ex.Detail, "não foi possível saber") {
		t.Errorf("fw desconhecido: %+v", ex)
	}
	// nenhuma porta sensível: informativo
	r.Listeners = []Listener{{Proto: "tcp", Addr: "127.0.0.1", Port: 22}, {Proto: "tcp", Addr: "0.0.0.0", Port: 8080}}
	ex = findingByID(Analyze(r, diagfmt.PT), "maquina-exposta")
	if ex.Severity != SevInfo || !strings.Contains(ex.Title+ex.Detail, "nenhuma porta sensível") {
		t.Errorf("sem porta sensível: %+v", ex)
	}
}

func TestMaquinaExpostaCGNAT(t *testing.T) {
	r := baseExposure()
	r.Interfaces[0].IPv4 = []Addr{{IP: "100.72.3.4", PrefixLen: 10}}
	ex := findingByID(Analyze(r, diagfmt.PT), "maquina-exposta")
	txt := ex.Title + " " + ex.Detail
	if ex.Severity != SevInfo || !strings.Contains(txt, "CGNAT") || !strings.Contains(txt, "provavelmente não é alcançável de fora por IPv4") {
		t.Errorf("cgnat: %+v", ex)
	}
	// interface sem tipo (ex.: VPN em 100.64/10) não é prova de CGNAT
	r.Interfaces[0].Kind = ""
	ex = findingByID(Analyze(r, diagfmt.PT), "maquina-exposta")
	if strings.Contains(ex.Title+ex.Detail, "CGNAT") {
		t.Errorf("interface desconhecida não prova CGNAT: %+v", ex)
	}
	// IPv6 global: ressalva
	r.Interfaces[0].Kind = "ethernet"
	r.Interfaces[0].IPv6 = []Addr{{IP: "2001:db8::10", PrefixLen: 64}}
	ex = findingByID(Analyze(r, diagfmt.PT), "maquina-exposta")
	if !strings.Contains(ex.Detail, "IPv6") {
		t.Errorf("com IPv6 global avisa: %+v", ex)
	}
}

func TestMaquinaExpostaSemNAT(t *testing.T) {
	r := baseExposure()
	r.PublicIP.IP = "192.0.2.10" // o IP público é o de uma interface
	ex := findingByID(Analyze(r, diagfmt.PT), "maquina-exposta")
	if !strings.Contains(ex.Detail, "sem NAT") {
		t.Errorf("sem NAT: %+v", ex)
	}
}

func TestAnalyzeIPFalhouNaoInventaExposicao(t *testing.T) {
	r := baseExposure()
	r.PublicIP = &PublicIPResult{Service: "ip.example.org", Outcome: OutcomeUnexpected, Reason: "not-ip"}
	fs := Analyze(r, diagfmt.PT)
	f := findingByID(fs, "ip-publico")
	if f == nil || !strings.Contains(f.Title, "não foi possível") || findingByID(fs, "maquina-exposta") != nil {
		t.Errorf("%+v", fs)
	}
}

func TestAnalyzePortal(t *testing.T) {
	r := baseExposure()
	r.PublicIP = nil
	r.Portal = &PortalResult{Service: "portal.example.org", Outcome: OutcomePortal, HTTPStatus: 302, RedirectHost: "login.hotel.example"}
	f := findingByID(Analyze(r, diagfmt.PT), "portal-cativo")
	if f == nil || f.Severity != SevLow || !strings.Contains(f.Title, "possível portal cativo") || !strings.Contains(f.Detail, "login.hotel.example") {
		t.Fatalf("%+v", f)
	}
	r.Portal = &PortalResult{Service: "portal.example.org", Outcome: OutcomeOK, HTTPStatus: 204}
	if f := findingByID(Analyze(r, diagfmt.PT), "portal-cativo"); f.Severity != SevOK {
		t.Errorf("livre: %+v", f)
	}
	r.Portal = &PortalResult{Service: "portal.example.org", Outcome: OutcomeFailed, Reason: "connect"}
	if f := findingByID(Analyze(r, diagfmt.PT), "portal-cativo"); f.Severity != SevInfo || !strings.Contains(f.Detail, "não é prova") {
		t.Errorf("falha: %+v", f)
	}
}

func TestQuerySummaryTraz(t *testing.T) {
	r := baseExposure()
	r.Portal = &PortalResult{Service: "portal.example.org", Outcome: OutcomeOK, HTTPStatus: 204}
	s := QuerySummary(r, diagfmt.PT)
	for _, w := range []string{"203.0.113.7", "Máquina exposta?", "portal.example.org", "o toolkit não testa de fora"} {
		if !strings.Contains(s, w) {
			t.Errorf("falta %q:\n%s", w, s)
		}
	}
	if strings.Contains(s, "Wi-Fi") || strings.Contains(s, "DNS") {
		t.Errorf("só os achados da consulta:\n%s", s)
	}
}

func TestMaquinaExpostaSemLeituraDasPortas(t *testing.T) {
	r := baseExposure()
	r.ListenersRead, r.Listeners = false, nil
	ex := findingByID(Analyze(r, diagfmt.PT), "maquina-exposta")
	if ex == nil || ex.Severity != SevInfo || !strings.Contains(ex.Title, "não foi possível avaliar") || strings.Contains(ex.Title+ex.Detail, "nenhuma porta sensível") {
		t.Errorf("sem leitura das portas não se afirma nada: %+v", ex)
	}
}

// A coleta nunca acessa a internet: nenhum arquivo do coletor (deps.go, coleta_*.go) conhece o Fetcher nem as consultas;
// só publico.go, chamado pelo cli/tui depois da confirmação, as tem.
func TestColetaNaoConheceAConsulta(t *testing.T) {
	for _, f := range []string{"deps.go", "coleta_lin.go", "coleta_mac.go", "coleta_win.go"} {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, w := range []string{"Fetcher", "QueryPublicIP", "ProbePortal", "PublicIP =", "Portal ="} {
			if strings.Contains(string(b), w) {
				t.Errorf("%s menciona %s: a coleta não consulta a internet", f, w)
			}
		}
	}
	rep := Collect(context.Background(), "linux", Deps{})
	if rep.PublicIP != nil || rep.Portal != nil {
		t.Errorf("Collect não preenche IP público nem portal: %+v", rep)
	}
}

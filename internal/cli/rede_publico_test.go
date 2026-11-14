package cli

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/PiluTech/toolkit/internal/rede"
	"github.com/PiluTech/toolkit/internal/rede/httpclient"
	"github.com/PiluTech/toolkit/internal/tui"
)

// fetchFake é o cliente HTTP falso das consultas: conta as chamadas (nenhum teste faz requisição real).
type fetchFake struct {
	calls int
	urls  []string
	resp  rede.Response
	err   error
}

func (f *fetchFake) Get(_ context.Context, u string) (rede.Response, error) {
	f.calls++
	f.urls = append(f.urls, u)
	return f.resp, f.err
}

const ipURL = "https://ip.example.org/meu-ip"

func queryDeps(t *testing.T, ff *fetchFake, env map[string]string, tty bool, stdin string) (Deps, *int) {
	t.Helper()
	collects := 0
	d := redeDeps(t, nil, nil)
	inner := d.Rede.Collect
	d.Rede.Collect = func(ctx context.Context, goos string, rd rede.Deps) *rede.Report {
		collects++
		return inner(ctx, goos, rd)
	}
	d.Rede.Fetcher = ff
	d.UpdateCheck = UpdateCheckDeps{Env: func(k string) string { return env[k] }}
	d.Interactive = func() bool { return tty }
	d.Stdin = strings.NewReader(stdin)
	return d, &collects
}

func TestRedeIPSemServicoNaoEnviaNada(t *testing.T) {
	ff := &fetchFake{}
	d, collects := queryDeps(t, ff, nil, true, "s\n")
	out, _, err := runRedeCmd(t, d, "ip", "--yes")
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range []string{"nenhum serviço configurado", "TOOLKIT_IP_URL", "--servico-ip", "painel do roteador"} {
		if !strings.Contains(out, w) {
			t.Errorf("falta %q:\n%s", w, out)
		}
	}
	if ff.calls != 0 || *collects != 0 {
		t.Errorf("nenhuma chamada sem URL: calls=%d collects=%d", ff.calls, *collects)
	}
}

func TestRedeIPURLInvalidaNaoEnvia(t *testing.T) {
	for _, u := range []string{"http://ip.example.org/x", "https://ip.example.org:8443/x", "https://u:p@ip.example.org/", "https://203.0.113.7/", "https://localhost/", "https://ip.example.org/#f"} {
		ff := &fetchFake{}
		d, _ := queryDeps(t, ff, nil, true, "s\n")
		_, _, err := runRedeCmd(t, d, "ip", "--servico-ip", u, "--yes")
		if err == nil || !strings.Contains(err.Error(), "serviço inválido") {
			t.Errorf("%q: err=%v", u, err)
		}
		if ff.calls != 0 {
			t.Errorf("%q: chamou o serviço", u)
		}
	}
}

func TestRedeIPSemTerminalExigeYes(t *testing.T) {
	ff := &fetchFake{resp: rede.Response{Status: 200, Body: []byte("203.0.113.7")}}
	d, collects := queryDeps(t, ff, nil, false, "")
	out, _, err := runRedeCmd(t, d, "ip", "--servico-ip", ipURL)
	if err == nil || !strings.Contains(err.Error(), "sem terminal") {
		t.Fatalf("err=%v", err)
	}
	if ff.calls != 0 || *collects != 0 {
		t.Errorf("nada enviado sem confirmação: calls=%d", ff.calls)
	}
	// a confirmação já nomeou o serviço e disse o que seria enviado
	for _, w := range []string{"ip.example.org", "GET", "IP público", "Nada será enviado sem a sua confirmação"} {
		if !strings.Contains(out, w) {
			t.Errorf("falta %q:\n%s", w, out)
		}
	}
}

func TestRedeIPDryRunMostraENaoEnvia(t *testing.T) {
	ff := &fetchFake{}
	d, collects := queryDeps(t, ff, nil, true, "s\n")
	out, _, err := runRedeCmd(t, d, "ip", "--servico-ip", ipURL, "--dry-run", "--yes")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "ip.example.org") || !strings.Contains(out, "nada foi enviado") {
		t.Errorf("dry-run:\n%s", out)
	}
	if ff.calls != 0 || *collects != 0 {
		t.Errorf("dry-run não envia nem lê: calls=%d", ff.calls)
	}
}

func TestRedeIPInterativo(t *testing.T) {
	for _, tc := range []struct {
		in    string
		calls int
	}{{"s\n", 1}, {"sim\n", 1}, {"y\n", 1}, {"n\n", 0}, {"\n", 0}, {"talvez\n", 0}, {"", 0}} {
		ff := &fetchFake{resp: rede.Response{Status: 200, Body: []byte("203.0.113.7")}}
		d, _ := queryDeps(t, ff, nil, true, tc.in)
		out, _, err := runRedeCmd(t, d, "ip", "--servico-ip", ipURL)
		if err != nil {
			t.Fatal(err)
		}
		if ff.calls != tc.calls {
			t.Errorf("%q: calls=%d", tc.in, ff.calls)
		}
		if tc.calls == 0 && !strings.Contains(out, "Cancelado: nada foi enviado") {
			t.Errorf("%q:\n%s", tc.in, out)
		}
		if !strings.Contains(out, "[s/N]") {
			t.Errorf("pergunta [s/N]:\n%s", out)
		}
	}
}

func TestRedeIPComYesEnviaUmaVezEAnalisa(t *testing.T) {
	ff := &fetchFake{resp: rede.Response{Status: 200, Body: []byte("203.0.113.7\n")}}
	d, collects := queryDeps(t, ff, nil, false, "")
	out, _, err := runRedeCmd(t, d, "ip", "--servico-ip", ipURL, "--yes")
	if err != nil {
		t.Fatal(err)
	}
	if ff.calls != 1 || ff.urls[0] != ipURL || *collects != 1 {
		t.Fatalf("calls=%d urls=%v collects=%d", ff.calls, ff.urls, *collects)
	}
	for _, w := range []string{"IP público: 203.0.113.7", "ip.example.org", "Máquina exposta?", "Telnet 23", "o toolkit não testa de fora"} {
		if !strings.Contains(out, w) {
			t.Errorf("falta %q:\n%s", w, out)
		}
	}
	if strings.Contains(out, "REDE-FAKE") {
		t.Errorf("só os achados da consulta:\n%s", out)
	}
}

func TestRedeIPVariavelDeAmbienteEFlagVale(t *testing.T) {
	ff := &fetchFake{resp: rede.Response{Status: 200, Body: []byte("203.0.113.7")}}
	d, _ := queryDeps(t, ff, map[string]string{"TOOLKIT_IP_URL": "https://env.example.org/ip"}, false, "")
	if _, _, err := runRedeCmd(t, d, "ip", "--yes"); err != nil {
		t.Fatal(err)
	}
	if len(ff.urls) != 1 || ff.urls[0] != "https://env.example.org/ip" {
		t.Errorf("ambiente: %v", ff.urls)
	}
	ff.urls, ff.calls = nil, 0
	if _, _, err := runRedeCmd(t, d, "ip", "--yes", "--servico-ip", "https://flag.example.org/ip"); err != nil {
		t.Fatal(err)
	}
	if len(ff.urls) != 1 || ff.urls[0] != "https://flag.example.org/ip" {
		t.Errorf("a flag vale mais: %v", ff.urls)
	}
}

func TestRedeIPRespostaRuimNaoMostraOCorpo(t *testing.T) {
	ff := &fetchFake{resp: rede.Response{Status: 200, Body: []byte("\x1b[2J\u202e<script>alert(1)</script>")}}
	d, _ := queryDeps(t, ff, nil, false, "")
	out, _, err := runRedeCmd(t, d, "ip", "--servico-ip", ipURL, "--yes")
	if err != nil {
		t.Fatal(err)
	}
	if strings.ContainsAny(out, "\x1b\u202e<") || !strings.Contains(out, "resposta inesperada") {
		t.Errorf("saída:\n%q", out)
	}
}

func TestRedePortal(t *testing.T) {
	const purl = "https://portal.example.org/gen204"
	// sem serviço
	ff := &fetchFake{}
	d, _ := queryDeps(t, ff, nil, true, "s\n")
	out, _, err := runRedeCmd(t, d, "portal", "--yes")
	if err != nil || !strings.Contains(out, "TOOLKIT_PORTAL_URL") || ff.calls != 0 {
		t.Fatalf("sem serviço: %v calls=%d\n%s", err, ff.calls, out)
	}
	// redirecionamento: uma chamada só, não segue, diz possível portal
	ff = &fetchFake{resp: rede.Response{Status: 302, Location: "http://login.hotel.example/entrar"}}
	d, _ = queryDeps(t, ff, map[string]string{"TOOLKIT_PORTAL_URL": purl}, false, "")
	out, _, err = runRedeCmd(t, d, "portal", "--yes")
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range []string{"portal.example.org", "possível portal cativo", "login.hotel.example", "não abre"} {
		if !strings.Contains(out, w) {
			t.Errorf("falta %q:\n%s", w, out)
		}
	}
	if ff.calls != 1 {
		t.Errorf("calls=%d", ff.calls)
	}
	// livre
	ff = &fetchFake{resp: rede.Response{Status: 204}}
	d, _ = queryDeps(t, ff, nil, false, "")
	out, _, _ = runRedeCmd(t, d, "portal", "--servico-portal", purl, "--yes")
	if !strings.Contains(out, "não detectado") {
		t.Errorf("livre:\n%s", out)
	}
	// dry-run e sem terminal
	ff = &fetchFake{}
	d, _ = queryDeps(t, ff, nil, false, "")
	if _, _, err := runRedeCmd(t, d, "portal", "--servico-portal", purl); err == nil || ff.calls != 0 {
		t.Errorf("sem terminal exige --yes: %v calls=%d", err, ff.calls)
	}
	if out, _, err := runRedeCmd(t, d, "portal", "--servico-portal", purl, "--dry-run"); err != nil || ff.calls != 0 || !strings.Contains(out, "nada foi enviado") {
		t.Errorf("dry-run: %v\n%s", err, out)
	}
}

func TestRedeFetcherPadraoERealSoNoDefault(t *testing.T) {
	var d Deps
	if _, ok := d.redeFetcher().(*httpclient.Client); !ok {
		t.Errorf("o padrão é o cliente real (com TLS, sem redirecionamento): %T", d.redeFetcher())
	}
}

func TestRedeSemSubcomandoNaoConsultaNada(t *testing.T) {
	ff := &fetchFake{}
	d, _ := queryDeps(t, ff, map[string]string{"TOOLKIT_IP_URL": ipURL}, true, "s\n")
	if _, _, err := runRedeCmd(t, d); err != nil {
		t.Fatal(err)
	}
	if _, _, err := runRedeCmd(t, d, "--completo"); err != nil {
		t.Fatal(err)
	}
	if ff.calls != 0 {
		t.Fatalf("toolkit rede (e --completo) nunca acessa a internet: calls=%d", ff.calls)
	}
}

func TestRedeBackendDaTUIPlanejaSemRedeEEnviaSoOPlano(t *testing.T) {
	ff := &fetchFake{resp: rede.Response{Status: 200, Body: []byte("203.0.113.7")}}
	env := map[string]string{}
	d, _ := queryDeps(t, ff, env, false, "")
	b := &redeBackend{deps: d}
	var _ tui.NetQueryBackend = b
	if _, err := b.PlanQuery(rede.KindIP); !errors.Is(err, rede.ErrNoService) {
		t.Fatalf("sem URL: %v", err)
	}
	env["TOOLKIT_IP_URL"] = "http://ip.example.org/x"
	if _, err := b.PlanQuery(rede.KindIP); !errors.Is(err, rede.ErrInvalidService) {
		t.Fatalf("http: %v", err)
	}
	env["TOOLKIT_IP_URL"] = ipURL
	p, err := b.PlanQuery(rede.KindIP)
	if err != nil || ff.calls != 0 {
		t.Fatalf("planejar não acessa a rede: err=%v calls=%d", err, ff.calls)
	}
	env["TOOLKIT_IP_URL"] = "https://trocado.example.org/ip" // a configuração muda depois da confirmação
	res := b.Query(context.Background(), p)
	if res.IP == nil || res.IP.IP != "203.0.113.7" || ff.calls != 1 || ff.urls[0] != ipURL {
		t.Fatalf("a TUI só envia o plano exibido: %+v calls=%d urls=%v", res, ff.calls, ff.urls)
	}
	env["TOOLKIT_PORTAL_URL"] = "https://portal.example.org/gen204"
	pp, err := b.PlanQuery(rede.KindPortal)
	if err != nil || pp.Kind != rede.KindPortal {
		t.Fatalf("portal: %+v %v", pp, err)
	}
}

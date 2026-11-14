package rede

import (
	"strings"
	"testing"

	"github.com/PiluTech/toolkit/internal/diagfmt"
)

// healthy é uma leitura sem nenhum problema (todas as áreas com dado): o ponto de partida de cada teste de achado.
func healthy() *Report {
	return &Report{
		OS: "linux", Admin: true,
		Interfaces: []Interface{{Name: "eth0", IPv4: []Addr{{IP: "192.0.2.10", PrefixLen: 24}}}},
		Gateways:   []string{"192.0.2.1"}, GatewaysRead: true,
		WiFi:      &WiFi{Interface: "wlan0", SSID: "REDE-FAKE", Security: SecWPA3},
		DNS:       DNS{Servers: []DNSServer{{Addr: "10.0.0.1"}}, Encrypted: No},
		Listeners: []Listener{{Proto: "tcp", Addr: "127.0.0.1", Port: 631}}, ListenersRead: true,
		Firewalls: []Firewall{{Name: "ufw", Enabled: Yes}},
	}
}

func find(fs []Finding, id string) (Finding, bool) {
	for _, f := range fs {
		if f.ID == id {
			return f, true
		}
	}
	return Finding{}, false
}

func want(t *testing.T, fs []Finding, id string, sev Severity) Finding {
	t.Helper()
	f, ok := find(fs, id)
	if !ok {
		t.Fatalf("falta o achado %q em %v", id, ids(fs))
	}
	if f.Severity != sev {
		t.Fatalf("%s: severidade %v, quer %v", id, f.Severity, sev)
	}
	return f
}

func none(t *testing.T, fs []Finding, id string) {
	t.Helper()
	if f, ok := find(fs, id); ok {
		t.Fatalf("não devia haver %q: %+v", id, f)
	}
}

func ids(fs []Finding) []string {
	var o []string
	for _, f := range fs {
		o = append(o, f.ID+":"+f.Severity.String())
	}
	return o
}

func TestWiFiPorTipoDeSeguranca(t *testing.T) {
	cases := []struct {
		sec  Security
		id   string
		want Severity
	}{
		{SecOpen, "wifi-aberto", SevHigh},
		{SecWEP, "wifi-wep", SevHigh},
		{SecWPA, "wifi-wpa", SevMedium},
		{SecWPA2, "wifi-wpa2", SevInfo},
		{SecWPA3, "wifi-wpa3", SevOK},
		{SecEnterprise, "wifi-corporativo", SevInfo},
		{SecUnknown, "wifi-desconhecido", SevInfo},
	}
	for _, c := range cases {
		r := healthy()
		r.WiFi = &WiFi{Security: c.sec, Raw: "x"}
		f := want(t, Analyze(r, diagfmt.PT), c.id, c.want)
		if f.Title == "" {
			t.Errorf("%s sem título", c.id)
		}
		if c.want >= SevMedium && f.Action == "" {
			t.Errorf("%s sem 'o que fazer'", c.id)
		}
	}
}

// sem dado nunca vira ok: Wi-Fi desconhecido não é "seguro".
func TestWiFiDesconhecidoNaoViraOK(t *testing.T) {
	r := healthy()
	r.WiFi = &WiFi{Security: SecUnknown, Raw: "WPA9_XYZ"}
	f := want(t, Analyze(r, diagfmt.PT), "wifi-desconhecido", SevInfo)
	if !strings.Contains(f.Detail, "WPA9_XYZ") {
		t.Errorf("o texto original deve aparecer: %q", f.Detail)
	}
	r.WiFi = nil
	none(t, Analyze(r, diagfmt.PT), "wifi-wpa3")
}

func TestFirewallWindowsPorPerfil(t *testing.T) {
	r := healthy()
	r.OS = "windows"
	r.Firewalls = []Firewall{{Name: "Public", Enabled: No, Active: true}, {Name: "Private", Enabled: No}, {Name: "Domain", Enabled: Yes}}
	fs := Analyze(r, diagfmt.PT)
	f := want(t, fs, "firewall-desligado-public", SevHigh)
	if !strings.Contains(f.Action, "não liga") && !strings.Contains(strings.ToLower(f.Action), "ligue") {
		t.Errorf("ação: %q", f.Action)
	}
	want(t, fs, "firewall-desligado-private", SevLow) // perfil sem uso
	none(t, fs, "firewall-desligado-domain")
}

func TestFirewallUnixAgregaEntradas(t *testing.T) {
	cases := []struct {
		name string
		fw   []Firewall
		id   string
		want Severity
	}{
		{"algum ligado", []Firewall{{Name: "ufw", Enabled: No}, {Name: "nftables", Enabled: Yes}}, "firewall-ligado", SevOK},
		{"todos desligados", []Firewall{{Name: "ufw", Enabled: No}, {Name: "firewalld", Enabled: No}}, "firewall-desligado", SevHigh},
		{"um desligado e um sem dado", []Firewall{{Name: "ufw", Enabled: No}, {Name: "nftables", Enabled: Unknown}}, "firewall-indeterminado", SevInfo},
		{"só sem dado", []Firewall{{Name: "ufw", Enabled: Unknown}}, "firewall-indeterminado", SevInfo},
		{"nenhum lido", nil, "firewall-indeterminado", SevInfo},
	}
	for _, c := range cases {
		r := healthy()
		r.Firewalls = c.fw
		fs := Analyze(r, diagfmt.PT)
		want(t, fs, c.id, c.want)
		if c.want != SevOK {
			none(t, fs, "firewall-ligado")
		}
	}
}

func TestPortaSensivelSoEmEnderecoNaoLocal(t *testing.T) {
	for _, addr := range []string{"127.0.0.1", "::1", "[::1]", "::ffff:127.0.0.1", "127.0.0.53%lo"} {
		r := healthy()
		r.Listeners = []Listener{{Proto: "tcp", Addr: addr, Port: 23}}
		r.Firewalls = []Firewall{{Name: "ufw", Enabled: No}}
		none(t, Analyze(r, diagfmt.PT), "porta-23")
	}
	for _, addr := range []string{"0.0.0.0", "::", "*", "192.0.2.10", "fe80::1%eth0", "2001:db8::10", ""} {
		r := healthy()
		r.Listeners = []Listener{{Proto: "tcp", Addr: addr, Port: 23, Process: "telnetd"}}
		r.Firewalls = []Firewall{{Name: "ufw", Enabled: No}}
		f := want(t, Analyze(r, diagfmt.PT), "porta-23", SevHigh)
		if !strings.Contains(f.Detail, "telnetd") {
			t.Errorf("%q: o processo deve aparecer: %q", addr, f.Detail)
		}
	}
}

func TestPortaSensivelSeveridadePeloFirewall(t *testing.T) {
	cases := []struct {
		port int
		fw   Tri
		want Severity
	}{
		{23, No, SevHigh}, {23, Unknown, SevHigh}, {23, Yes, SevMedium}, // risco alto
		{22, No, SevMedium}, {22, Unknown, SevMedium}, {22, Yes, SevLow}, // risco médio
		{445, Yes, SevLow}, {3389, No, SevMedium}, {6379, Yes, SevMedium},
	}
	for _, c := range cases {
		r := healthy()
		r.Firewalls = []Firewall{{Name: "ufw", Enabled: c.fw}}
		r.Listeners = []Listener{{Proto: "tcp", Addr: "0.0.0.0", Port: c.port}}
		want(t, Analyze(r, diagfmt.PT), "porta-"+itoa(c.port), c.want)
	}
	// a lista fechada do plano
	for _, p := range []int{21, 22, 23, 135, 139, 445, 3389, 5900, 5901, 5985, 5986, 3306, 5432, 6379, 27017, 9200} {
		r := healthy()
		r.Firewalls = []Firewall{{Name: "ufw", Enabled: No}}
		r.Listeners = []Listener{{Proto: "tcp", Addr: "::", Port: p}}
		if _, ok := find(Analyze(r, diagfmt.PT), "porta-"+itoa(p)); !ok {
			t.Errorf("a porta %d é sensível pelo plano", p)
		}
	}
}

func TestPortaSensivelUDPENaoSensivelNaoGeraAchadoPorPorta(t *testing.T) {
	r := healthy()
	r.Listeners = []Listener{{Proto: "udp", Addr: "0.0.0.0", Port: 23}, {Proto: "tcp", Addr: "0.0.0.0", Port: 8080}, {Proto: "tcp", Addr: "::", Port: 8081}, {Proto: "tcp", Addr: "127.0.0.1", Port: 9000}}
	fs := Analyze(r, diagfmt.PT)
	none(t, fs, "porta-23")
	f := want(t, fs, "portas-expostas", SevInfo)
	if !strings.Contains(f.Detail, "2") {
		t.Errorf("a contagem agregada (2 TCP + 1 UDP) deve aparecer: %q", f.Detail)
	}
	// só local: nada a agregar
	r.Listeners = []Listener{{Proto: "tcp", Addr: "127.0.0.1", Port: 9000}}
	none(t, Analyze(r, diagfmt.PT), "portas-expostas")
}

func TestPortaMesmaPortaEmV4EV6ViraUmAchado(t *testing.T) {
	r := healthy()
	r.Listeners = []Listener{{Proto: "tcp", Addr: "0.0.0.0", Port: 22}, {Proto: "tcp", Addr: "::", Port: 22}}
	n := 0
	for _, f := range Analyze(r, diagfmt.PT) {
		if f.ID == "porta-22" {
			n++
		}
	}
	if n != 1 {
		t.Errorf("achados da porta 22: %d, quer 1", n)
	}
}

func TestDNSPublicoSemCriptografia(t *testing.T) {
	pub := []string{"203.0.113.53", "198.51.100.1", "2001:db8::53"}
	for _, a := range pub {
		r := healthy()
		r.DNS = DNS{Servers: []DNSServer{{Addr: a}}, Encrypted: No}
		want(t, Analyze(r, diagfmt.PT), "dns-publico", SevLow)
	}
	for _, a := range []string{"192.168.0.1", "10.0.0.1", "172.16.0.1", "127.0.0.53", "100.100.100.100", "fe80::1%eth0", "fd00::1", "::1"} {
		r := healthy()
		r.DNS = DNS{Servers: []DNSServer{{Addr: a}}, Encrypted: No}
		none(t, Analyze(r, diagfmt.PT), "dns-publico")
		none(t, Analyze(r, diagfmt.PT), "dns-publico-nao-verificado")
	}
	r := healthy()
	r.DNS = DNS{Servers: []DNSServer{{Addr: "203.0.113.53"}}, Encrypted: Yes}
	none(t, Analyze(r, diagfmt.PT), "dns-publico")
	// sem informação de DoH/DoT não vira "baixa" nem "ok": fica informativo
	r.DNS = DNS{Servers: []DNSServer{{Addr: "203.0.113.53"}}, Encrypted: Unknown}
	fs := Analyze(r, diagfmt.PT)
	none(t, fs, "dns-publico")
	want(t, fs, "dns-publico-nao-verificado", SevInfo)
}

func TestIPv6SemPrivacidadeSoComEnderecoGlobal(t *testing.T) {
	r := healthy()
	r.IPv6Privacy = No
	r.Interfaces[0].IPv6 = []Addr{{IP: "fe80::1", PrefixLen: 64}, {IP: "fd00::5", PrefixLen: 64}}
	none(t, Analyze(r, diagfmt.PT), "ipv6-sem-privacidade")
	r.Interfaces[0].IPv6 = append(r.Interfaces[0].IPv6, Addr{IP: "2001:db8::5", PrefixLen: 64})
	want(t, Analyze(r, diagfmt.PT), "ipv6-sem-privacidade", SevLow)
	r.IPv6Privacy = Yes
	none(t, Analyze(r, diagfmt.PT), "ipv6-sem-privacidade")
	r.IPv6Privacy = Unknown
	none(t, Analyze(r, diagfmt.PT), "ipv6-sem-privacidade") // sem dado: sem achado (e nunca "ok")
}

func TestSemGateway(t *testing.T) {
	r := healthy()
	r.Gateways = nil
	want(t, Analyze(r, diagfmt.PT), "sem-gateway", SevMedium)
	r.GatewaysRead = false // não lido: sem achado
	none(t, Analyze(r, diagfmt.PT), "sem-gateway")
}

func TestPerfilDeRede(t *testing.T) {
	r := healthy()
	r.OS = "windows"
	r.Profiles = []Profile{{Name: "REDE-FAKE", Category: "public"}}
	r.Listeners = []Listener{{Proto: "tcp", Addr: "0.0.0.0", Port: 445}}
	want(t, Analyze(r, diagfmt.PT), "perfil-publico-compartilhando", SevMedium)
	r.Listeners = []Listener{{Proto: "tcp", Addr: "127.0.0.1", Port: 445}}
	none(t, Analyze(r, diagfmt.PT), "perfil-publico-compartilhando")
	r.Profiles = []Profile{{Name: "REDE-FAKE", Category: "private"}}
	r.WiFi = &WiFi{Security: SecOpen}
	want(t, Analyze(r, diagfmt.PT), "perfil-privado-rede-aberta", SevMedium)
	r.WiFi = &WiFi{Security: SecWPA3}
	none(t, Analyze(r, diagfmt.PT), "perfil-privado-rede-aberta")
}

func TestIPPublicoNaoConsultado(t *testing.T) {
	f := want(t, Analyze(healthy(), diagfmt.PT), "ip-publico", SevInfo)
	if !strings.Contains(f.Title, "não consultado") {
		t.Errorf("título: %q", f.Title)
	}
}

func TestRelatorioVazioNaoViraOK(t *testing.T) {
	fs := Analyze(&Report{OS: "linux"}, diagfmt.PT)
	want(t, fs, "nada-lido", SevInfo)
	for _, f := range fs {
		if f.Severity == SevOK {
			t.Errorf("sem dado nunca vira ok: %+v", f)
		}
	}
}

func TestOrdenaDoMaisGraveAoMaisLeve(t *testing.T) {
	r := healthy()
	r.WiFi = &WiFi{Security: SecOpen}
	r.DNS = DNS{Servers: []DNSServer{{Addr: "203.0.113.53"}}, Encrypted: No}
	r.Firewalls = []Firewall{{Name: "ufw", Enabled: Yes}}
	fs := Analyze(r, diagfmt.PT)
	for i := 1; i < len(fs); i++ {
		if fs[i].Severity > fs[i-1].Severity {
			t.Fatalf("fora de ordem: %v", ids(fs))
		}
	}
}

func TestTextosNosTresIdiomas(t *testing.T) {
	r := healthy()
	r.WiFi = &WiFi{Security: SecOpen}
	r.Listeners = []Listener{{Proto: "tcp", Addr: "0.0.0.0", Port: 23}}
	r.Firewalls = []Firewall{{Name: "ufw", Enabled: No}}
	var titles []string
	for _, l := range []Lang{diagfmt.PT, diagfmt.EN, diagfmt.ES} {
		f := want(t, Analyze(r, l), "wifi-aberto", SevHigh)
		if f.Title == "" || f.Action == "" {
			t.Fatalf("%v: texto vazio", l)
		}
		titles = append(titles, f.Title)
		p := want(t, Analyze(r, l), "porta-23", SevHigh)
		if p.Title == "" || p.Action == "" {
			t.Fatalf("%v: porta sem texto", l)
		}
	}
	if titles[0] == titles[1] || titles[1] == titles[2] || titles[0] == titles[2] {
		t.Errorf("os idiomas devem diferir: %v", titles)
	}
}

func TestResumoContaOQueNaoFoiLido(t *testing.T) {
	r := &Report{OS: "linux", Skipped: []Skipped{{Area: "wifi", Reason: "x"}, {Area: "firewall", Reason: "y", NeedsAdmin: true}}}
	s := Summary(r, Analyze(r, diagfmt.PT), diagfmt.PT)
	if strings.Contains(s, "nada de ruim encontrado") {
		t.Errorf("um relatório sem dado não pode dizer que está tudo bem:\n%s", s)
	}
	if !strings.Contains(s, "2") || !strings.Contains(strings.ToLower(s), "não") {
		t.Errorf("o resumo deve contar os itens não lidos:\n%s", s)
	}
	if !strings.Contains(s, "administrador") && !strings.Contains(s, "sudo") {
		t.Errorf("o resumo deve dizer o que o administrador leria a mais:\n%s", s)
	}
	// com dado e sem problema, sem Skipped
	ok := healthy()
	ok.Firewalls = []Firewall{{Name: "ufw", Enabled: Yes}}
	s = Summary(ok, Analyze(ok, diagfmt.PT), diagfmt.PT)
	if !strings.Contains(s, "nada de ruim") {
		t.Errorf("leitura sem problema:\n%s", s)
	}
}

func TestIsLocalEPublico(t *testing.T) {
	for a, local := range map[string]bool{"127.0.0.1": true, "127.5.5.5": true, "::1": true, "[::1]:": false, "0.0.0.0": false, "::": false, "*": false, "192.0.2.1": false, "fe80::1": false} {
		if a == "[::1]:" {
			continue
		}
		if got := isLoopback(a); got != local {
			t.Errorf("isLoopback(%q)=%v", a, got)
		}
	}
}

func TestPortaIgualEmV4EV6NaoRepeteODetalhe(t *testing.T) {
	r := healthy()
	r.Listeners = []Listener{{Proto: "tcp", Addr: "*", Port: 5432, Process: "db"}, {Proto: "tcp", Addr: "*", Port: 5432, Process: "db"}, {Proto: "tcp", Addr: "::", Port: 5432, Process: "db"}}
	f := want(t, Analyze(r, diagfmt.PT), "porta-5432", SevLow)
	if strings.Count(f.Detail, "*:5432") != 1 || strings.Count(f.Detail, "::5432") != 1 {
		t.Errorf("detalhe repetido: %q", f.Detail)
	}
}

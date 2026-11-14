package rede

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/PiluTech/toolkit/internal/diagfmt"
	"github.com/PiluTech/toolkit/internal/exec"
	"github.com/PiluTech/toolkit/internal/exec/exectest"
)

// Fixtures SINTÉTICAS (documentação: 192.0.2.x, 198.51.100.x, 203.0.113.x, 2001:db8::, MACs 02-00-00-00-00-xx, SSID REDE-FAKE).
// winOK e winFail montam o status por subconsulta que os scripts devolvem: {ok:true,data:[...]} ou {ok:false,err:"..."}.
func winOK(data string) string  { return `{"ok":true,"data":` + data + `}` }
func winFail(err string) string { return `{"ok":false,"err":"` + err + `"}` }

var (
	winIPsJSON = `{"ad":` + winOK(`[{"Name":"Ethernet","Mac":"02-00-00-00-00-01","Status":"Up","Media":"802.3"},{"Name":"Wi-Fi","Mac":"02-00-00-00-00-02","Status":"Up","Media":"Native 802.11"},{"Name":"Ethernet 2","Mac":"02-00-00-00-00-03","Status":"Disconnected","Media":"802.3"}]`) + `,` +
		`"ip":` + winOK(`[{"Alias":"Wi-Fi","Addr":"192.0.2.20","Fam":"IPv4","Len":24,"Sfx":"Dhcp"},{"Alias":"Wi-Fi","Addr":"2001:db8::20","Fam":"IPv6","Len":64,"Sfx":"Link"},{"Alias":"Wi-Fi","Addr":"2001:db8::2f","Fam":"IPv6","Len":64,"Sfx":"Random"},{"Alias":"Wi-Fi","Addr":"fe80::20%12","Fam":"IPv6","Len":64,"Sfx":"Link"},`+
		`{"Alias":"Loopback Pseudo-Interface 1","Addr":"127.0.0.1","Fam":"IPv4","Len":8,"Sfx":"WellKnown"},{"Alias":"Loopback Pseudo-Interface 1","Addr":"::1","Fam":"IPv6","Len":128,"Sfx":"WellKnown"}]`) + `,` +
		`"gw":` + winOK(`[{"Alias":"Wi-Fi","Next":"192.0.2.1"},{"Alias":"Wi-Fi","Next":"fe80::ff"}]`) + `}`
	winProfileJSON = `{"p":` + winOK(`[{"Name":"REDE-FAKE","Alias":"Wi-Fi","Cat":"Public"}]`) + `}`
	winDNSJSON     = `{"srv":` + winOK(`[{"Alias":"Wi-Fi","Srv":["192.0.2.1","2001:db8::1"]},{"Alias":"Loopback Pseudo-Interface 1","Srv":["fec0:0:0:ffff::1","fec0:0:0:ffff::2"]},{"Alias":"Ethernet","Srv":[]}]`) + `,"doh":` + winOK(`[{"Addr":"203.0.113.53","Auto":"True","Fb":"False"}]`) + `}`
	winPortsJSON   = `{"t":` + winOK(`[{"A":"0.0.0.0","P":445,"I":4,"N":"System"},{"A":"127.0.0.1","P":5040,"I":8,"N":"svc"},{"A":"::","P":135,"I":7,"N":"svchost"}]`) + `,"u":` + winOK(`[{"A":"0.0.0.0","P":5353,"I":9,"N":"svc2"}]`) + `}`
	winFWJSON      = `{"f":` + winOK(`[{"Name":"Domain","En":"True","In":"NotConfigured"},{"Name":"Private","En":"True","In":"Block"},{"Name":"Public","En":"False","In":"Block"}]`) + `}`
	winIPv6JSON    = `{"t":` + winOK(`["Disabled"]`) + `}`
)

const (
	netshEN = `
There is 2 interfaces on the system:

    Name                   : Wi-Fi
    Description            : Fake Wireless Adapter
    GUID                   : 00000000-0000-0000-0000-000000000000
    Physical address       : 02:00:00:00:00:02
    State                  : connected
    SSID                   : REDE:FAKE
    BSSID                  : 02:00:00:00:00:99
    Network type           : Infrastructure
    Radio type             : 802.11ac
    Authentication         : WPA2-Personal
    Cipher                 : CCMP

    Name                   : Wi-Fi 2
    State                  : disconnected
`
	netshPT = "\r\n    Nome                   : Wi-Fi\r\n    Estado                 : conectado\r\n    SSID                   : REDE-FAKE\r\n    BSSID                  : 02:00:00:00:00:99\r\n" +
		"    Autentica\xe7\xe3o          : WPA2-Pessoal\r\n    Cifra                  : CCMP\r\n"
)

type winFX struct {
	scripts map[string]string
	netsh   string
	netshOK bool
}

func newWinFX() *winFX {
	return &winFX{scripts: map[string]string{
		winScriptIPs: winIPsJSON, winScriptProfile: winProfileJSON, winScriptDNS: winDNSJSON, winScriptPorts: winPortsJSON,
		winScriptFirewall: winFWJSON, winScriptIPv6: winIPv6JSON,
	}, netsh: netshEN, netshOK: true}
}

func netshCmdLine() string {
	return exec.Cmd{Name: winNetsh, Args: []string{"wlan", "show", "interfaces"}}.String()
}

func (x *winFX) run(t *testing.T, admin bool) (*Report, *exectest.FakeRunner) {
	t.Helper()
	f := exectest.New(t)
	for sc, out := range x.scripts {
		f.On(psCmd(sc).String(), exectest.Exit(0, out))
	}
	if x.netshOK {
		f.On(netshCmdLine(), exectest.Exit(0, x.netsh))
	} else {
		f.On(netshCmdLine(), exectest.Exit(1, ""))
	}
	d := Deps{Runner: f, Lang: diagfmt.PT, Admin: admin, Now: func() time.Time { return time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC) },
		ReadFile: func(string) ([]byte, error) { return nil, os.ErrNotExist }}
	return Collect(context.Background(), "windows", d), f
}

func TestWindowsInterfacesERotas(t *testing.T) {
	r, _ := newWinFX().run(t, false)
	names := []string{}
	for _, i := range r.Interfaces {
		names = append(names, i.Name)
	}
	if strings.Join(names, ",") != "Ethernet,Wi-Fi" { // Ethernet 2 desconectada sem IP e o loopback somem
		t.Fatalf("interfaces: %v", names)
	}
	w := r.Interfaces[1]
	if w.Kind != "wifi" || w.MAC != "02-00-00-00-00-02" || w.Up != Yes || len(w.IPv4) != 1 || w.IPv4[0].IP != "192.0.2.20" || w.IPv4[0].PrefixLen != 24 {
		t.Errorf("Wi-Fi: %+v", w)
	}
	if len(w.IPv6) != 3 || !w.IPv6[1].Temporary || w.IPv6[0].Temporary || w.IPv6[2].IP != "fe80::20" {
		t.Errorf("IPv6 (Random = temporário; escopo %%12 sai): %+v", w.IPv6)
	}
	if r.Interfaces[0].Kind != "ethernet" {
		t.Errorf("Ethernet: %+v", r.Interfaces[0])
	}
	if !r.GatewaysRead || strings.Join(r.Gateways, ",") != "192.0.2.1,fe80::ff" {
		t.Errorf("rotas: %v %v", r.GatewaysRead, r.Gateways)
	}
}

func TestWindowsGatewayOnLinkViraDev(t *testing.T) {
	x := newWinFX()
	x.scripts[winScriptIPs] = `{"ad":` + winOK(`[]`) + `,"ip":` + winOK(`[]`) + `,"gw":` + winOK(`{"Alias":"VPN","Next":"0.0.0.0"}`) + `}` // objeto único no lugar de lista
	r, _ := x.run(t, false)
	if strings.Join(r.Gateways, ",") != "dev VPN" {
		t.Errorf("rotas: %v", r.Gateways)
	}
	x.scripts[winScriptIPs] = `{"ad":` + winOK(`[]`) + `,"ip":` + winOK(`[]`) + `,"gw":` + winOK(`[]`) + `}`
	r, _ = x.run(t, false)
	if !r.GatewaysRead || len(r.Gateways) != 0 {
		t.Errorf("sem rota: %v %v", r.GatewaysRead, r.Gateways)
	}
}

func TestWindowsPerfilEFirewallEmUso(t *testing.T) {
	r, _ := newWinFX().run(t, false)
	if len(r.Profiles) != 1 || r.Profiles[0].Category != "public" || r.Profiles[0].Name != "REDE-FAKE" {
		t.Fatalf("perfil: %+v", r.Profiles)
	}
	got := map[string]Firewall{}
	for _, f := range r.Firewalls {
		got[f.Name] = f
	}
	if got["Public"].Enabled != No || !got["Public"].Active || got["Private"].Active || got["Domain"].Enabled != Yes {
		t.Errorf("firewalls: %+v", got)
	}
	fs := Analyze(r, diagfmt.PT)
	want(t, fs, "firewall-desligado-public", SevHigh)
	want(t, fs, "porta-445", SevMedium) // firewall do perfil em uso desligado: sem rebaixar
	want(t, fs, "perfil-publico-compartilhando", SevMedium)
	want(t, fs, "porta-135", SevMedium)
	// perfil objeto único
	x := newWinFX()
	x.scripts[winScriptProfile] = `{"p":` + winOK(`{"Name":"Empresa","Alias":"Ethernet","Cat":"DomainAuthenticated"}`) + `}`
	r, _ = x.run(t, false)
	if len(r.Profiles) != 1 || r.Profiles[0].Category != "domain" {
		t.Errorf("perfil único: %+v", r.Profiles)
	}
}

func TestWindowsFirewallNotConfiguredEhDesconhecido(t *testing.T) {
	x := newWinFX()
	x.scripts[winScriptFirewall] = `{"f":` + winOK(`[{"Name":"Public","En":"NotConfigured","In":"Block"}]`) + `}`
	r, _ := x.run(t, false)
	if r.Firewalls[0].Enabled != Unknown {
		t.Errorf("NotConfigured: %v", r.Firewalls[0].Enabled)
	}
	fs := Analyze(r, diagfmt.PT)
	if _, ok := find(fs, "firewall-ligado"); ok {
		t.Error("sem dado nunca vira ligado")
	}
}

func winDNSScript(doh string) string {
	return `{"srv":` + winOK(`[{"Alias":"Wi-Fi","Srv":["203.0.113.53"]}]`) + `,"doh":` + doh + `}`
}

func TestWindowsDNSComDoH(t *testing.T) {
	r, _ := newWinFX().run(t, false)
	if len(r.DNS.Servers) != 2 { // loopback e lista vazia ficam de fora
		t.Fatalf("DNS: %+v", r.DNS.Servers)
	}
	// 192.0.2.1 sem modelo DoH: não está criptografado
	if r.DNS.Encrypted != No || r.DNS.Mode != DNSModeOff {
		t.Errorf("DoH: %v %v", r.DNS.Encrypted, r.DNS.Mode)
	}
	x := newWinFX()
	// DoH estrito: AutoUpgrade e SEM fallback para UDP: a única combinação que prova "criptografado"
	x.scripts[winScriptDNS] = winDNSScript(winOK(`[{"Addr":"203.0.113.53","Auto":"True","Fb":"False"}]`))
	if r, _ = x.run(t, false); r.DNS.Encrypted != Yes || r.DNS.Mode != DNSModeStrict || r.DNS.AllowFallback != No {
		t.Errorf("DoH estrito: %v %v %v", r.DNS.Encrypted, r.DNS.Mode, r.DNS.AllowFallback)
	}
	// AutoUpgrade com fallback permitido: admite DNS em claro, então a criptografia efetiva é desconhecida
	x.scripts[winScriptDNS] = winDNSScript(winOK(`[{"Addr":"203.0.113.53","Auto":"True","Fb":"True"}]`))
	r, _ = x.run(t, false)
	if r.DNS.Encrypted != Unknown || r.DNS.Mode != DNSModeOpportunistic || r.DNS.AllowFallback != Yes {
		t.Errorf("AutoUpgrade + fallback permitido: %v %v %v", r.DNS.Encrypted, r.DNS.Mode, r.DNS.AllowFallback)
	}
	f := want(t, Analyze(r, diagfmt.PT), "dns-publico-nao-verificado", SevInfo)
	if !strings.Contains(f.Detail, "DNS em claro") {
		t.Errorf("o achado deve dizer que pode cair para DNS em claro: %q", f.Detail)
	}
	// AutoUpgrade sem o fallback lido: nem "sim" nem "não"
	x.scripts[winScriptDNS] = winDNSScript(winOK(`[{"Addr":"203.0.113.53","Auto":"True"}]`))
	r, _ = x.run(t, false)
	if r.DNS.Encrypted != Unknown || r.DNS.Mode != DNSModeOpportunistic || r.DNS.AllowFallback != Unknown {
		t.Errorf("AutoUpgrade, fallback não lido: %v %v %v", r.DNS.Encrypted, r.DNS.Mode, r.DNS.AllowFallback)
	}
	f = want(t, Analyze(r, diagfmt.PT), "dns-publico-nao-verificado", SevInfo)
	if !strings.Contains(f.Detail, "fallback não lido") {
		t.Errorf("o achado deve dizer que o fallback não foi lido: %q", f.Detail)
	}
	x.scripts[winScriptDNS] = winDNSScript(winOK(`[{"Addr":"203.0.113.53","Auto":"False","Fb":"False"}]`))
	r, _ = x.run(t, false)
	if r.DNS.Encrypted != No || r.DNS.Mode != DNSModeOff || len(r.DNS.Servers) != 1 {
		t.Errorf("DoH sem auto-upgrade: %v %v %v", r.DNS.Encrypted, r.DNS.Mode, r.DNS.Servers)
	}
	// Windows sem o cmdlet (na): nem "sim" nem "não", e sem Skipped (não é falha)
	x.scripts[winScriptDNS] = winDNSScript(`{"ok":true,"na":true,"data":[]}`)
	r, _ = x.run(t, false)
	if r.DNS.Encrypted != Unknown || r.DNS.Mode != DNSModeUnknown {
		t.Errorf("sem cmdlet de DoH: %v %v", r.DNS.Encrypted, r.DNS.Mode)
	}
	if _, ok := skippedArea(r, "dns"); ok {
		t.Error("cmdlet ausente (Windows antigo) não é falha de leitura")
	}
	// o cmdlet existe mas FALHOU (exit 0 com erro interno): a criptografia fica desconhecida e a falha é reportada
	x.scripts[winScriptDNS] = winDNSScript(winFail("CimException"))
	r, _ = x.run(t, false)
	if r.DNS.Encrypted != Unknown || len(r.DNS.Servers) != 1 {
		t.Errorf("falha do DoH: %v %v", r.DNS.Encrypted, r.DNS.Servers)
	}
	if s, ok := skippedArea(r, "dns"); !ok || !strings.Contains(s.Reason, "CimException") {
		t.Errorf("falha do DoH deve virar Skipped com o tipo do erro: %+v", s)
	}
	// os servidores falharam: nada de lista vazia "lida"
	x.scripts[winScriptDNS] = `{"srv":` + winFail("CimException") + `,"doh":` + winOK(`[]`) + `}`
	r, _ = x.run(t, false)
	if len(r.DNS.Servers) != 0 {
		t.Errorf("servidores: %+v", r.DNS.Servers)
	}
	if _, ok := skippedArea(r, "dns"); !ok {
		t.Error("falta o Skipped de dns")
	}
}

// O caso real: o PowerShell engole o erro do cmdlet e sai com código 0. Falha NUNCA vira lista vazia "lida".
func TestWindowsFalhaDeSubconsultaComExit0NaoInventaAchado(t *testing.T) {
	x := newWinFX()
	x.scripts[winScriptIPs] = `{"ad":` + winOK(`[{"Name":"Ethernet","Mac":"02-00-00-00-00-01","Status":"Up","Media":"802.3"}]`) + `,"ip":` + winOK(`[{"Alias":"Ethernet","Addr":"192.0.2.20","Fam":"IPv4","Len":24,"Sfx":"Dhcp"}]`) + `,"gw":` + winFail("CimException") + `}`
	x.scripts[winScriptPorts] = `{"t":` + winFail("CimException") + `,"u":` + winOK(`[]`) + `}`
	x.scripts[winScriptProfile] = `{"p":` + winFail("CimException") + `}`
	x.scripts[winScriptFirewall] = `{"f":` + winFail("CimException") + `}`
	x.scripts[winScriptIPv6] = `{"t":` + winFail("CimException") + `}`
	r, _ := x.run(t, false)
	if r.GatewaysRead || len(r.Gateways) != 0 {
		t.Errorf("rota não lida: %v %v", r.GatewaysRead, r.Gateways)
	}
	if r.ListenersRead || len(r.Listeners) != 0 {
		t.Errorf("portas não lidas: %v %v", r.ListenersRead, r.Listeners)
	}
	for _, a := range []string{"rotas", "portas", "perfil", "firewall", "ipv6"} {
		s, ok := skippedArea(r, a)
		if !ok {
			t.Errorf("falta o Skipped de %s", a)
		} else if !strings.Contains(s.Reason, "CimException") {
			t.Errorf("Skipped de %s deve citar o tipo do erro: %q", a, s.Reason)
		}
	}
	fs := Analyze(r, diagfmt.PT)
	none(t, fs, "sem-gateway")
	none(t, fs, "portas-expostas")
	for _, f := range fs {
		if strings.HasPrefix(f.ID, "porta-") || strings.HasPrefix(f.ID, "firewall-ligado") {
			t.Errorf("achado inventado a partir de falha: %s", f.ID)
		}
	}
	if len(r.Interfaces) != 1 { // o que foi lido com sucesso continua valendo
		t.Errorf("interfaces: %+v", r.Interfaces)
	}
	// ip falhou: sem interfaces "lidas" pela metade
	x.scripts[winScriptIPs] = `{"ad":` + winOK(`[{"Name":"Ethernet","Status":"Up"}]`) + `,"ip":` + winFail("CimException") + `,"gw":` + winOK(`[{"Alias":"Ethernet","Next":"192.0.2.1"}]`) + `}`
	r, _ = x.run(t, false)
	if len(r.Interfaces) != 0 || !r.GatewaysRead || len(r.Gateways) != 1 {
		t.Errorf("ip falhou: %+v %v %v", r.Interfaces, r.GatewaysRead, r.Gateways)
	}
	if _, ok := skippedArea(r, "interfaces"); !ok {
		t.Error("falta o Skipped de interfaces")
	}
	// UDP falhou e TCP ok: também não é "lido"
	x.scripts[winScriptPorts] = `{"t":` + winOK(`[]`) + `,"u":` + winFail("CimException") + `}`
	if r, _ = x.run(t, false); r.ListenersRead {
		t.Error("com o UDP falho as portas não estão lidas")
	}
}

// Vazio LEGÍTIMO (ok:true, data vazio) continua sendo uma leitura: sem rota padrão, nenhuma porta.
func TestWindowsVazioLegitimoContinuaLido(t *testing.T) {
	x := newWinFX()
	x.scripts[winScriptIPs] = `{"ad":` + winOK(`[]`) + `,"ip":` + winOK(`[]`) + `,"gw":` + winOK(`[]`) + `}`
	x.scripts[winScriptPorts] = `{"t":` + winOK(`[]`) + `,"u":` + winOK(`[]`) + `}`
	r, _ := x.run(t, false)
	if !r.GatewaysRead || len(r.Gateways) != 0 || !r.ListenersRead || len(r.Listeners) != 0 {
		t.Fatalf("vazio legítimo: %v %v %v %v", r.GatewaysRead, r.Gateways, r.ListenersRead, r.Listeners)
	}
	for _, a := range []string{"rotas", "portas", "interfaces"} {
		if _, ok := skippedArea(r, a); ok {
			t.Errorf("vazio legítimo não é Skipped: %s", a)
		}
	}
	want(t, Analyze(r, diagfmt.PT), "sem-gateway", SevMedium)
}

// Sem o "ok" booleano verdadeiro NÃO é leitura: campo ausente, "ok" como texto, subconsulta ausente, "data" ausente.
func TestWindowsCampoAusenteNaoViraOk(t *testing.T) {
	for name, gw := range map[string]string{
		"sem ok":        `{"data":[]}`,
		"ok como texto": `{"ok":"true","data":[]}`,
		"ok false":      `{"ok":false,"data":[]}`,
		"sem data":      `{"ok":true}`,
		"nulo":          `null`,
		"lista":         `[]`,
	} {
		x := newWinFX()
		x.scripts[winScriptIPs] = `{"ad":` + winOK(`[]`) + `,"ip":` + winOK(`[]`) + `,"gw":` + gw + `}`
		r, _ := x.run(t, false)
		if r.GatewaysRead {
			t.Errorf("%s: não pode virar lido", name)
		}
		if _, ok := skippedArea(r, "rotas"); !ok {
			t.Errorf("%s: falta o Skipped de rotas", name)
		}
	}
	x := newWinFX()
	x.scripts[winScriptIPs] = `{"ad":` + winOK(`[]`) + `,"ip":` + winOK(`[]`) + `}` // subconsulta "gw" ausente
	if r, _ := x.run(t, false); r.GatewaysRead {
		t.Error("subconsulta ausente não vira lida")
	}
}

func TestWindowsPortasComProcesso(t *testing.T) {
	r, _ := newWinFX().run(t, false)
	if !r.ListenersRead || len(r.Listeners) != 4 {
		t.Fatalf("portas: %+v", r.Listeners)
	}
	if l := r.Listeners[0]; l.Proto != "tcp" || l.Addr != "0.0.0.0" || l.Port != 445 || l.Process != "System" || l.PID != 4 {
		t.Errorf("445: %+v", l)
	}
	if l := r.Listeners[3]; l.Proto != "udp" || l.Port != 5353 {
		t.Errorf("udp: %+v", l)
	}
	for _, s := range r.Skipped {
		if s.NeedsAdmin {
			t.Errorf("no Windows o nome do processo não exige administrador: %+v", s)
		}
	}
}

func TestWindowsIPv6(t *testing.T) {
	r, _ := newWinFX().run(t, false)
	if r.IPv6Privacy != No {
		t.Errorf("Disabled: %v", r.IPv6Privacy)
	}
	x := newWinFX()
	x.scripts[winScriptIPv6] = `{"t":` + winOK(`["Enabled"]`) + `}`
	if r, _ = x.run(t, false); r.IPv6Privacy != Yes {
		t.Errorf("Enabled: %v", r.IPv6Privacy)
	}
	x.scripts[winScriptIPv6] = `{"t":` + winOK(`["Default"]`) + `}`
	if r, _ = x.run(t, false); r.IPv6Privacy != Unknown {
		t.Errorf("Default: %v", r.IPv6Privacy)
	}
}

func TestWindowsAreaQueFalhaViraSkippedEASeguinteContinua(t *testing.T) {
	x := newWinFX()
	x.scripts[winScriptFirewall] = "isto não é json"
	x.scripts[winScriptProfile] = ""
	r, _ := x.run(t, false)
	for _, a := range []string{"firewall", "perfil"} {
		if _, ok := skippedArea(r, a); !ok {
			t.Errorf("falta o Skipped de %s", a)
		}
	}
	if len(r.Listeners) == 0 || len(r.Interfaces) == 0 {
		t.Error("as outras áreas devem seguir")
	}
	// sem leitura de firewall, o relatório nunca diz "ligado"
	if _, ok := find(Analyze(r, diagfmt.PT), "firewall-ligado"); ok {
		t.Error("firewall não lido nunca vira ligado")
	}
}

func TestParseNetshInterfaces(t *testing.T) {
	w := parseNetshInterfaces(netshEN)
	if w == nil || w.SSID != "REDE:FAKE" || w.Security != SecWPA2 || w.Interface != "Wi-Fi" || w.Raw != "WPA2-Personal / CCMP" {
		t.Fatalf("en-US: %+v", w)
	}
	w = parseNetshInterfaces(netshPT)
	if w == nil || w.SSID != "REDE-FAKE" || w.Security != SecWPA2 || w.Interface != "Wi-Fi" {
		t.Fatalf("pt-BR (CP850/1252 no nome do campo): %+v", w)
	}
	if parseNetshInterfaces("    Name : Wi-Fi\n    State : disconnected\n") != nil {
		t.Error("sem SSID não há conexão")
	}
	if parseNetshInterfaces("") != nil || parseNetshInterfaces("lixo sem dois pontos") != nil {
		t.Error("texto sem campos")
	}
}

func TestNetshSecurity(t *testing.T) {
	cases := []struct {
		auth, cipher string
		want         Security
	}{
		{"Open", "None", SecOpen}, {"Aberto", "Nenhum", SecOpen}, {"Abierta", "Ninguno", SecOpen},
		{"Open", "WEP", SecWEP}, {"Shared", "WEP", SecWEP}, {"WEP", "", SecWEP},
		{"WPA-Personal", "TKIP", SecWPA}, {"WPA-Pessoal", "CCMP", SecWPA},
		{"WPA2-Personal", "CCMP", SecWPA2}, {"WPA2-Pessoal", "CCMP", SecWPA2}, {"WPA2-Personal", "TKIP", SecWPA},
		{"WPA3-SAE", "CCMP", SecWPA3}, {"WPA3-Personal", "GCMP", SecWPA3},
		{"WPA2-Enterprise", "CCMP", SecEnterprise}, {"WPA2-Empresarial", "CCMP", SecEnterprise},
		{"WPA9", "CCMP", SecUnknown}, {"", "", SecUnknown}, {"Enhanced Open", "GCMP", SecUnknown},
	}
	for _, c := range cases {
		if got := netshSecurity(c.auth, c.cipher); got != c.want {
			t.Errorf("netshSecurity(%q,%q)=%v, quer %v", c.auth, c.cipher, got, c.want)
		}
	}
}

func TestWindowsWiFiSoComPlacaWiFiEFalhaDoNetsh(t *testing.T) {
	r, f := newWinFX().run(t, false)
	if r.WiFi == nil || r.WiFi.SSID != "REDE:FAKE" {
		t.Fatalf("wifi: %+v", r.WiFi)
	}
	called := false
	for _, c := range f.Calls() {
		if c.Name == winNetsh {
			called = true
			if strings.Join(c.Args, " ") != "wlan show interfaces" {
				t.Errorf("netsh só com 'wlan show interfaces': %v", c.Args)
			}
		}
	}
	if !called {
		t.Error("netsh não foi chamado")
	}
	x := newWinFX()
	x.netshOK = false
	r, _ = x.run(t, false)
	if _, ok := skippedArea(r, "wifi"); !ok || r.WiFi != nil {
		t.Error("falha do netsh: Skipped, sem WiFi")
	}
	// sem placa Wi-Fi o netsh nem roda
	x = newWinFX()
	x.scripts[winScriptIPs] = strings.ReplaceAll(winIPsJSON, "Native 802.11", "802.3")
	_, f = x.run(t, false)
	for _, c := range f.Calls() {
		if c.Name == winNetsh {
			t.Error("sem placa Wi-Fi o netsh não deve rodar")
		}
	}
}

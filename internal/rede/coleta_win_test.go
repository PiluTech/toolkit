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
const (
	winIPsJSON = `{"ad":[{"Name":"Ethernet","Mac":"02-00-00-00-00-01","Status":"Up","Media":"802.3"},{"Name":"Wi-Fi","Mac":"02-00-00-00-00-02","Status":"Up","Media":"Native 802.11"},{"Name":"Ethernet 2","Mac":"02-00-00-00-00-03","Status":"Disconnected","Media":"802.3"}],` +
		`"ip":[{"Alias":"Wi-Fi","Addr":"192.0.2.20","Fam":"IPv4","Len":24,"Sfx":"Dhcp"},{"Alias":"Wi-Fi","Addr":"2001:db8::20","Fam":"IPv6","Len":64,"Sfx":"Link"},{"Alias":"Wi-Fi","Addr":"2001:db8::2f","Fam":"IPv6","Len":64,"Sfx":"Random"},{"Alias":"Wi-Fi","Addr":"fe80::20%12","Fam":"IPv6","Len":64,"Sfx":"Link"},` +
		`{"Alias":"Loopback Pseudo-Interface 1","Addr":"127.0.0.1","Fam":"IPv4","Len":8,"Sfx":"WellKnown"},{"Alias":"Loopback Pseudo-Interface 1","Addr":"::1","Fam":"IPv6","Len":128,"Sfx":"WellKnown"}],` +
		`"gw":[{"Alias":"Wi-Fi","Next":"192.0.2.1"},{"Alias":"Wi-Fi","Next":"fe80::ff"}]}`
	winProfileJSON = `[{"Name":"REDE-FAKE","Alias":"Wi-Fi","Cat":"Public"}]`
	winDNSJSON     = `{"srv":[{"Alias":"Wi-Fi","Srv":["192.0.2.1","2001:db8::1"]},{"Alias":"Loopback Pseudo-Interface 1","Srv":["fec0:0:0:ffff::1","fec0:0:0:ffff::2"]},{"Alias":"Ethernet","Srv":[]}],"doh":[{"Addr":"203.0.113.53","Auto":"True"}]}`
	winPortsJSON   = `{"t":[{"A":"0.0.0.0","P":445,"I":4,"N":"System"},{"A":"127.0.0.1","P":5040,"I":8,"N":"svc"},{"A":"::","P":135,"I":7,"N":"svchost"}],"u":[{"A":"0.0.0.0","P":5353,"I":9,"N":"svc2"}]}`
	winFWJSON      = `[{"Name":"Domain","En":"True","In":"NotConfigured"},{"Name":"Private","En":"True","In":"Block"},{"Name":"Public","En":"False","In":"Block"}]`
	winIPv6JSON    = `{"t":"Disabled"}`

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
	x.scripts[winScriptIPs] = `{"ad":[],"ip":[],"gw":{"Alias":"VPN","Next":"0.0.0.0"}}` // objeto único no lugar de lista
	r, _ := x.run(t, false)
	if strings.Join(r.Gateways, ",") != "dev VPN" {
		t.Errorf("rotas: %v", r.Gateways)
	}
	x.scripts[winScriptIPs] = `{"ad":[],"ip":[],"gw":[]}`
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
	x.scripts[winScriptProfile] = `{"Name":"Empresa","Alias":"Ethernet","Cat":"DomainAuthenticated"}`
	r, _ = x.run(t, false)
	if len(r.Profiles) != 1 || r.Profiles[0].Category != "domain" {
		t.Errorf("perfil único: %+v", r.Profiles)
	}
}

func TestWindowsFirewallNotConfiguredEhDesconhecido(t *testing.T) {
	x := newWinFX()
	x.scripts[winScriptFirewall] = `[{"Name":"Public","En":"NotConfigured","In":"Block"}]`
	r, _ := x.run(t, false)
	if r.Firewalls[0].Enabled != Unknown {
		t.Errorf("NotConfigured: %v", r.Firewalls[0].Enabled)
	}
	fs := Analyze(r, diagfmt.PT)
	if _, ok := find(fs, "firewall-ligado"); ok {
		t.Error("sem dado nunca vira ligado")
	}
}

func TestWindowsDNSComDoH(t *testing.T) {
	r, _ := newWinFX().run(t, false)
	if len(r.DNS.Servers) != 2 { // loopback e lista vazia ficam de fora
		t.Fatalf("DNS: %+v", r.DNS.Servers)
	}
	// 192.0.2.1 sem modelo DoH: não está criptografado
	if r.DNS.Encrypted != No {
		t.Errorf("DoH: %v", r.DNS.Encrypted)
	}
	x := newWinFX()
	x.scripts[winScriptDNS] = `{"srv":[{"Alias":"Wi-Fi","Srv":["203.0.113.53"]}],"doh":[{"Addr":"203.0.113.53","Auto":"True"}]}`
	if r, _ = x.run(t, false); r.DNS.Encrypted != Yes {
		t.Errorf("com DoH automático: %v", r.DNS.Encrypted)
	}
	x.scripts[winScriptDNS] = `{"srv":[{"Alias":"Wi-Fi","Srv":"203.0.113.53"}],"doh":[{"Addr":"203.0.113.53","Auto":"False"}]}`
	if r, _ = x.run(t, false); r.DNS.Encrypted != No || len(r.DNS.Servers) != 1 {
		t.Errorf("DoH sem auto-upgrade: %v %v", r.DNS.Encrypted, r.DNS.Servers)
	}
	// Windows sem o cmdlet: nem "sim" nem "não"
	x.scripts[winScriptDNS] = `{"srv":[{"Alias":"Wi-Fi","Srv":["203.0.113.53"]}],"doh":null}`
	if r, _ = x.run(t, false); r.DNS.Encrypted != Unknown {
		t.Errorf("sem cmdlet de DoH: %v", r.DNS.Encrypted)
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
	x.scripts[winScriptIPv6] = `{"t":"Enabled"}`
	if r, _ = x.run(t, false); r.IPv6Privacy != Yes {
		t.Errorf("Enabled: %v", r.IPv6Privacy)
	}
	x.scripts[winScriptIPv6] = `{"t":"Default"}`
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

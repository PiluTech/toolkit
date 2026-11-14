package rede

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/PiluTech/toolkit/internal/diagfmt"
	"github.com/PiluTech/toolkit/internal/exec/exectest"
)

// Fixtures SINTÉTICAS (documentação: 192.0.2.x, 198.51.100.x, 203.0.113.x, 2001:db8::, MACs 02:00:00:00:00:xx).
const (
	macPorts1 = `Hardware Port: Ethernet Adapter (en3)
Device: en3
Ethernet Address: 02:00:00:00:00:03

Hardware Port: Wi-Fi
Device: en0
Ethernet Address: 02:00:00:00:00:04

VLAN Configurations
===================
`
	macIfconfig1 = `lo0: flags=8049<UP,LOOPBACK,RUNNING,MULTICAST> mtu 16384
	inet 127.0.0.1 netmask 0xff000000
	inet6 ::1 prefixlen 128
	inet6 fe80::1%lo0 prefixlen 64 scopeid 0x1
gif0: flags=8010<POINTOPOINT,MULTICAST> mtu 1280
en3: flags=8863<UP,BROADCAST,SMART,RUNNING,SIMPLEX,MULTICAST> mtu 1500
	ether 02:00:00:00:00:03
	status: inactive
en0: flags=8863<UP,BROADCAST,SMART,RUNNING,SIMPLEX,MULTICAST> mtu 1500
	options=6460<TSO4,TSO6>
	ether 02:00:00:00:00:04
	inet6 fe80::1%en0 prefixlen 64 secured scopeid 0xb
	inet 192.0.2.7 netmask 0xffffff00 broadcast 192.0.2.255
	inet6 2001:db8::7 prefixlen 64 autoconf secured
	inet6 2001:db8::77 prefixlen 64 autoconf temporary
	media: autoselect
	status: active
utun0: flags=8051<UP,POINTOPOINT,RUNNING,MULTICAST> mtu 1500
	inet6 fe80::2%utun0 prefixlen 64 scopeid 0xc
utun2: flags=8051<UP,POINTOPOINT,RUNNING,MULTICAST> mtu 1280
	inet 198.51.100.5 --> 198.51.100.5 netmask 0xffffffff
`
	macRoute4 = `Routing tables

Internet:
Destination        Gateway            Flags               Netif Expire
default            192.0.2.1          UGScg                 en0
default            link#18            UCSIg               utun2
192.0.2/24         link#11            UCS                   en0      !
`
	macRoute6 = `Routing tables

Internet6:
Destination                             Gateway                                 Flags               Netif Expire
default                                 fe80::ff%en0                            UGcg                  en0
`
	macSummary = `<dictionary> {
  BSSID : <redacted>
  ConnectionID : 7
  IPv4 : <array> {
    0 : <dictionary> {
      Addresses : <array> {
        0 : 192.0.2.7
      }
      DHCP : <dictionary> {
        Packet : op = BOOTREPLY
htype = 1
          Security : DECOY_NESTED
      }
    }
  }
  SSID : <redacted>
  Security : WPA2_PSK
}
`
	macScutil1 = `DNS configuration

resolver #1
  search domain[0] : example.test
  nameserver[0] : 198.51.100.53
  if_index : 18 (utun2)
  flags    : Supplemental, Request A records
  reach    : 0x00000003 (Reachable,Transient Connection)

resolver #2
  nameserver[0] : fe80::ff%en0
  nameserver[1] : 192.0.2.1
  if_index : 11 (en0)
  flags    : Request A records, Request AAAA records

resolver #3
  domain   : local
  options  : mdns

DNS configuration (for scoped queries)

resolver #1
  nameserver[0] : 203.0.113.9
  if_index : 11 (en0)
`
	macNetstat1 = `Active Internet connections (including servers)
Proto Recv-Q Send-Q  Local Address          Foreign Address        (state)
tcp4       0      0  192.0.2.7.51000        203.0.113.9.443        ESTABLISHED
tcp4       0      0  127.0.0.1.6463         *.*                    LISTEN
tcp46      0      0  *.7000                 *.*                    LISTEN
tcp4       0      0  *.22                   *.*                    LISTEN
tcp6       0      0  ::1.8080               *.*                    LISTEN
`
	macLsof1 = `COMMAND     PID     USER   FD   TYPE             DEVICE SIZE/OFF NODE NAME
rapportd    622  usuario   10u  IPv4 0x1      0t0  TCP *:7000 (LISTEN)
Fake\x20App  900  usuario    4u  IPv4 0x2      0t0  TCP 127.0.0.1:6463 (LISTEN)
`
)

type macFX struct{ out map[string]string }

func newMacFX() *macFX {
	return &macFX{out: map[string]string{
		macNetworksetup + " -listallhardwareports":   macPorts1,
		macIfconfig + " -a":                          macIfconfig1,
		macNetstat + " -rn -f inet":                  macRoute4,
		macNetstat + " -rn -f inet6":                 macRoute6,
		macIpconfig + " getsummary en0":              macSummary,
		macScutil + " --dns":                         macScutil1,
		macNetstat + " -an -p tcp":                   macNetstat1,
		macLsof + " -nP -iTCP -sTCP:LISTEN":          macLsof1,
		macSocketFW + " --getglobalstate":            "Firewall is disabled. (State = 0)\n",
		macSysctl + " -n net.inet6.ip6.use_tempaddr": "1\n",
		macPfctl + " -s info":                        "Status: Disabled\n",
	}}
}

func (x *macFX) run(t *testing.T, admin bool) (*Report, *exectest.FakeRunner) {
	t.Helper()
	f := exectest.New(t)
	for line, o := range x.out {
		f.On(line, exectest.Exit(0, o))
	}
	d := Deps{Runner: f, Lang: diagfmt.PT, Admin: admin, Now: func() time.Time { return time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC) },
		ReadFile: func(string) ([]byte, error) { return nil, os.ErrNotExist }}
	return Collect(context.Background(), "darwin", d), f
}

func TestMacInterfaces(t *testing.T) {
	r, _ := newMacFX().run(t, true)
	names := []string{}
	for _, i := range r.Interfaces {
		names = append(names, i.Name)
	}
	if strings.Join(names, ",") != "en0,utun2" { // lo0, gif0, en3 (inativa) e utun0 (só link-local) somem
		t.Fatalf("interfaces: %v", names)
	}
	e := r.Interfaces[0]
	if e.Kind != "wifi" || e.MAC != "02:00:00:00:00:04" || e.Up != Yes || e.IPv4[0].IP != "192.0.2.7" || e.IPv4[0].PrefixLen != 24 {
		t.Errorf("en0: %+v", e)
	}
	if len(e.IPv6) != 3 || e.IPv6[2].IP != "2001:db8::77" || !e.IPv6[2].Temporary || e.IPv6[1].Temporary {
		t.Errorf("IPv6: %+v", e.IPv6)
	}
	if r.Interfaces[1].IPv4[0].PrefixLen != 32 {
		t.Errorf("ponto a ponto: %+v", r.Interfaces[1])
	}
}

func TestMacRotas(t *testing.T) {
	r, _ := newMacFX().run(t, true)
	if !r.GatewaysRead || strings.Join(r.Gateways, ",") != "192.0.2.1,dev utun2,fe80::ff%en0" {
		t.Errorf("rotas: %v %v", r.GatewaysRead, r.Gateways)
	}
}

func TestMacWiFiSegurancaPassiva(t *testing.T) {
	r, f := newMacFX().run(t, false)
	if r.WiFi == nil || r.WiFi.Security != SecWPA2 || r.WiFi.Raw != "WPA2_PSK" || r.WiFi.Interface != "en0" {
		t.Fatalf("wifi: %+v", r.WiFi)
	}
	if r.WiFi.SSID != "" {
		t.Errorf("SSID <redacted> deve virar vazio, veio %q", r.WiFi.SSID)
	}
	if !strings.Contains(Full(r, diagfmt.PT), "não informou o nome") {
		t.Error("o relatório deve dizer que o macOS ocultou o nome")
	}
	for _, c := range f.Calls() {
		if c.Name == macIpconfig && strings.Join(c.Args, " ") != "getsummary en0" {
			t.Errorf("ipconfig só com getsummary: %v", c.Args)
		}
	}
}

func TestMacWiFiSSIDVisivelEInterfaceSemConexao(t *testing.T) {
	x := newMacFX()
	x.out[macIpconfig+" getsummary en0"] = strings.ReplaceAll(macSummary, "SSID : <redacted>", "SSID : REDE-FAKE")
	r, _ := x.run(t, true)
	if r.WiFi == nil || r.WiFi.SSID != "REDE-FAKE" {
		t.Errorf("SSID: %+v", r.WiFi)
	}
	x = newMacFX()
	x.out[macIfconfig+" -a"] = strings.ReplaceAll(macIfconfig1, "status: active", "status: inactive")
	delete(x.out, macIpconfig+" getsummary en0") // não pode nem chamar
	r, _ = x.run(t, true)
	if r.WiFi != nil {
		t.Error("Wi-Fi inativo não tem conexão a ler")
	}
}

func TestMacWiFiSemTokenReconhecido(t *testing.T) {
	x := newMacFX()
	x.out[macIpconfig+" getsummary en0"] = "<dictionary> {\n  BSSID : <redacted>\n}\n"
	r, _ := x.run(t, true)
	if r.WiFi != nil {
		t.Error("sem Security no resumo, sem WiFi")
	}
	if s, ok := skippedArea(r, "wifi"); !ok || !strings.Contains(s.Reason, "VERIFICAR") {
		t.Errorf("deve explicar (A VERIFICAR): %+v", s)
	}
	x.out[macIpconfig+" getsummary en0"] = "<dictionary> {\n  Security : WPA9_XYZ\n}\n"
	r, _ = x.run(t, true)
	if r.WiFi == nil || r.WiFi.Security != SecUnknown || r.WiFi.Raw != "WPA9_XYZ" {
		t.Errorf("token desconhecido: %+v", r.WiFi)
	}
}

func TestMacSecurity(t *testing.T) {
	for in, want := range map[string]Security{
		"NONE": SecOpen, "OPEN": SecOpen, "": SecOpen, "WEP": SecWEP, "WPA_PSK": SecWPA, "WPA2_PSK": SecWPA2, "WPA3_SAE": SecWPA3,
		"WPA2_ENT": SecEnterprise, "WPA3_ENT": SecEnterprise, "WPA2_PSK_WPA3_SAE": SecWPA2, "OWE": SecUnknown, "XYZ": SecUnknown,
	} {
		if got := macSecurity(in); got != want {
			t.Errorf("macSecurity(%q)=%v, quer %v", in, got, want)
		}
	}
}

func TestMacDNS(t *testing.T) {
	r, _ := newMacFX().run(t, true)
	got := []string{}
	for _, s := range r.DNS.Servers {
		got = append(got, s.Addr+"@"+s.Interface)
	}
	// o resolvedor Supplemental (de um domínio) e a lista "scoped" repetida ficam de fora
	if strings.Join(got, ",") != "fe80::ff%en0@en0,192.0.2.1@en0" {
		t.Errorf("DNS: %v", got)
	}
	if r.DNS.Encrypted != Unknown {
		t.Error("o scutil não informa DoH/DoT: desconhecido")
	}
}

func TestMacPortasNetstatMaisLsof(t *testing.T) {
	r, _ := newMacFX().run(t, false)
	if !r.ListenersRead || len(r.Listeners) != 4 {
		t.Fatalf("portas: %+v", r.Listeners)
	}
	by := map[int]Listener{}
	for _, l := range r.Listeners {
		by[l.Port] = l
	}
	if l := by[7000]; l.Addr != "*" || l.Process != "rapportd" || l.PID != 622 {
		t.Errorf("7000: %+v", l)
	}
	if l := by[6463]; l.Process != "Fake App" {
		t.Errorf("\\x20 do lsof deve virar espaço: %+v", l)
	}
	// a porta 22 NÃO aparece no lsof (outro usuário): continua listada, sem processo, e pede administrador
	if l := by[22]; l.Addr != "*" || l.Process != "" {
		t.Errorf("22: %+v", l)
	}
	if s, ok := skippedArea(r, "processos-das-portas"); !ok || !s.NeedsAdmin {
		t.Errorf("Skipped NeedsAdmin: %+v", s)
	}
	fs := Analyze(r, diagfmt.PT)
	if _, ok := find(fs, "porta-22"); !ok {
		t.Errorf("a porta 22 do netstat não pode sumir por falta de processo: %v", ids(fs))
	}
	// com administrador o lsof vê tudo: a fixture com a 22 mostra que o Skipped some
	x := newMacFX()
	x.out[macLsof+" -nP -iTCP -sTCP:LISTEN"] = macLsof1 + "sshd          1  root    3u  IPv4 0x3      0t0  TCP *:22 (LISTEN)\n"
	r, _ = x.run(t, true)
	if _, ok := skippedArea(r, "processos-das-portas"); ok {
		t.Errorf("com administrador e todas nomeadas, nada a pular: %+v", r.Skipped)
	}
}

func TestMacFirewall(t *testing.T) {
	for state, want := range map[string]Tri{"0": No, "1": Yes, "2": Yes} {
		x := newMacFX()
		x.out[macSocketFW+" --getglobalstate"] = "Firewall is enabled. (State = " + state + ")\n"
		r, _ := x.run(t, false)
		if r.Firewalls[0].Enabled != want || r.Firewalls[0].Name != "aplicativos" {
			t.Errorf("State=%s: %+v", state, r.Firewalls[0])
		}
	}
	r, f := newMacFX().run(t, false)
	if len(r.Firewalls) != 1 {
		t.Errorf("sem administrador o pf não é lido: %+v", r.Firewalls)
	}
	if s, ok := skippedArea(r, "pf"); !ok || !s.NeedsAdmin {
		t.Errorf("pf: NeedsAdmin: %+v", s)
	}
	for _, c := range f.Calls() {
		if c.Name == macPfctl {
			t.Error("pfctl só com administrador")
		}
	}
	r, _ = newMacFX().run(t, true)
	if len(r.Firewalls) != 2 || r.Firewalls[1].Name != "pf" || r.Firewalls[1].Enabled != No {
		t.Errorf("pf com admin: %+v", r.Firewalls)
	}
	x := newMacFX()
	x.out[macSocketFW+" --getglobalstate"] = "saída estranha\n"
	r, _ = x.run(t, false)
	if r.Firewalls[0].Enabled != Unknown {
		t.Error("saída ilegível: desconhecido")
	}
	if _, ok := skippedArea(r, "firewall"); !ok {
		t.Error("saída ilegível: Skipped")
	}
}

func TestMacIPv6(t *testing.T) {
	r, _ := newMacFX().run(t, true)
	if r.IPv6Privacy != Yes {
		t.Errorf("use_tempaddr=1: %v", r.IPv6Privacy)
	}
	x := newMacFX()
	x.out[macSysctl+" -n net.inet6.ip6.use_tempaddr"] = "0\n"
	if r, _ = x.run(t, true); r.IPv6Privacy != No {
		t.Errorf("use_tempaddr=0: %v", r.IPv6Privacy)
	}
}

func TestMacInterfaceDoSistemaNuncaViraOpcao(t *testing.T) {
	x := newMacFX()
	x.out[macNetworksetup+" -listallhardwareports"] = strings.ReplaceAll(macPorts1, "Device: en0", "Device: -setverbose")
	_, f := x.run(t, true)
	for _, c := range f.Calls() {
		if c.Name == macIpconfig {
			t.Errorf("nome de interface suspeito virou argumento: %v", c.Args)
		}
	}
}

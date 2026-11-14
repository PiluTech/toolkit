package rede

import (
	"context"
	"os"
	"path"
	"strings"
	"testing"
	"time"

	"github.com/PiluTech/toolkit/internal/diagfmt"
	"github.com/PiluTech/toolkit/internal/exec"
	"github.com/PiluTech/toolkit/internal/exec/exectest"
)

// Fixtures SINTÉTICAS (documentação: 192.0.2.x, 198.51.100.x, 203.0.113.x, 2001:db8::, MACs 02:00:00:00:00:xx, SSID REDE-FAKE).
const (
	linIPAddr = `[{"ifindex":1,"ifname":"lo","flags":["LOOPBACK","UP"],"operstate":"UNKNOWN","link_type":"loopback","address":"00:00:00:00:00:00","addr_info":[{"family":"inet","local":"127.0.0.1","prefixlen":8,"scope":"host"},{"family":"inet6","local":"::1","prefixlen":128,"scope":"host"}]},` +
		`{"ifindex":2,"ifname":"enp0s3","flags":["UP"],"operstate":"UP","link_type":"ether","address":"02:00:00:00:00:01","addr_info":[{"family":"inet","local":"192.0.2.10","prefixlen":24,"scope":"global"},{"family":"inet6","local":"2001:db8::10","prefixlen":64,"scope":"global","dynamic":true},{"family":"inet6","local":"2001:db8::abcd","prefixlen":64,"scope":"global","temporary":true},{"family":"inet6","local":"fe80::1","prefixlen":64,"scope":"link"}]},` +
		`{"ifindex":3,"ifname":"wlp2s0","flags":[],"operstate":"DOWN","link_type":"ether","address":"02:00:00:00:00:02","addr_info":[]}]`
	linRoute4 = `[{"dst":"default","gateway":"192.0.2.1","dev":"enp0s3","protocol":"dhcp","metric":100}]`
	linRoute6 = `[{"dst":"default","gateway":"fe80::ff","dev":"enp0s3","metric":100}]`
	linNmcli  = "no:REDE-VIZINHA:WPA2\nyes:REDE-FAKE:WPA2 WPA3\nno:OUTRA\\:REDE:--\n"
	linResolv = `Global
       Protocols: -LLMNR -mDNS -DNSOverTLS DNSSEC=no/unsupported
resolv.conf mode: stub

Link 2 (enp0s3)
    Current Scopes: DNS
         Protocols: +DefaultRoute -LLMNR -mDNS -DNSOverTLS DNSSEC=no/unsupported
Current DNS Server: 192.0.2.1
       DNS Servers: 192.0.2.1 2001:db8::1
        DNS Domain: ~.
`
	linSS = `tcp   LISTEN 0      128          0.0.0.0:22         0.0.0.0:*    users:(("sshd",pid=812,fd=3))
tcp   LISTEN 0      4096       127.0.0.1:631        0.0.0.0:*
tcp   LISTEN 0      128             [::]:22            [::]:*    users:(("sshd",pid=812,fd=4))
udp   UNCONN 0      0      127.0.0.53%lo:53         0.0.0.0:*
udp   UNCONN 0      0            0.0.0.0:5353       0.0.0.0:*    users:(("avahi-daemon",pid=700,fd=12))
tcp   LISTEN 0      511                *:8080             *:*    users:(("my \"app\"",pid=900,fd=9))
`
	linUfwOff = "Status: inactive\n"
	linUfwOn  = "Status: active\n\nTo   Action   From\n"
	linNft    = "table inet filter {\n\tchain input {\n\t\ttype filter hook input priority filter; policy drop;\n\t}\n}\n"
)

type linFX struct {
	out      map[string]string // linha de comando -> stdout
	fail     map[string]string // linha de comando -> stderr (exit 1)
	tools    map[string]string
	files    map[string]string
	noWifiHW bool // sem placa Wi-Fi no sysfs
}

func newLinFX() *linFX {
	return &linFX{
		out: map[string]string{
			"/usr/bin/ip -j addr":                                                 linIPAddr,
			"/usr/bin/ip -j route show default":                                   linRoute4,
			"/usr/bin/ip -j -6 route show default":                                linRoute6,
			"/usr/bin/nmcli -t -f ACTIVE,SSID,SECURITY dev wifi list --rescan no": linNmcli,
			"/usr/bin/resolvectl status":                                          linResolv,
			"/usr/bin/ss -H -t -u -l -n -p":                                       linSS,
			"/usr/sbin/ufw status":                                                linUfwOn,
			"/usr/bin/firewall-cmd --state":                                       "running\n",
			"/usr/sbin/nft list ruleset":                                          linNft,
		},
		fail: map[string]string{},
		tools: map[string]string{"ip": "/usr/bin/ip", "nmcli": "/usr/bin/nmcli", "resolvectl": "/usr/bin/resolvectl", "ss": "/usr/bin/ss",
			"ufw": "/usr/sbin/ufw", "firewall-cmd": "/usr/bin/firewall-cmd", "nft": "/usr/sbin/nft"},
		files: map[string]string{"/proc/sys/net/ipv6/conf/enp0s3/use_tempaddr": "2\n", "/etc/resolv.conf": "nameserver 192.0.2.1\n"},
	}
}

func (x *linFX) run(t *testing.T, admin bool) (*Report, *exectest.FakeRunner) {
	t.Helper()
	f := exectest.New(t)
	for n, p := range x.tools {
		f.WithPath(n, p)
	}
	for line, o := range x.out {
		f.On(line, exectest.Exit(0, o))
	}
	for line, e := range x.fail {
		f.On(line, exectest.Reply{Result: exec.Result{ExitCode: 1, Stderr: []byte(e)}})
	}
	d := Deps{Runner: f, Lang: diagfmt.PT, Admin: admin,
		Now: func() time.Time { return time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC) },
		ReadFile: func(p string) ([]byte, error) {
			if v, ok := x.files[p]; ok {
				return []byte(v), nil
			}
			return nil, os.ErrNotExist
		},
		Glob: func(pat string) ([]string, error) {
			var out []string
			for _, p := range []string{"/sys/class/net/wlp2s0/wireless"} {
				if x.noWifiHW {
					break
				}
				if ok, _ := path.Match(pat, p); ok {
					out = append(out, p)
				}
			}
			return out, nil
		}}
	return Collect(context.Background(), "linux", d), f
}

func skippedArea(r *Report, area string) (Skipped, bool) {
	for _, s := range r.Skipped {
		if s.Area == area {
			return s, true
		}
	}
	return Skipped{}, false
}

func TestLinuxInterfacesERotas(t *testing.T) {
	r, _ := newLinFX().run(t, true)
	if len(r.Interfaces) != 2 { // lo some
		t.Fatalf("interfaces: %+v", r.Interfaces)
	}
	e := r.Interfaces[0]
	if e.Name != "enp0s3" || e.MAC != "02:00:00:00:00:01" || e.Kind != "ethernet" || e.Up != Yes || len(e.IPv4) != 1 || e.IPv4[0].IP != "192.0.2.10" || e.IPv4[0].PrefixLen != 24 {
		t.Errorf("enp0s3: %+v", e)
	}
	if len(e.IPv6) != 3 || !e.IPv6[1].Temporary || e.IPv6[0].Temporary {
		t.Errorf("IPv6: %+v", e.IPv6)
	}
	if w := r.Interfaces[1]; w.Name != "wlp2s0" || w.Kind != "wifi" || w.Up != No {
		t.Errorf("wlp2s0: %+v", w)
	}
	if !r.GatewaysRead || strings.Join(r.Gateways, ",") != "192.0.2.1,fe80::ff" {
		t.Errorf("rotas: %v %v", r.GatewaysRead, r.Gateways)
	}
}

func TestLinuxRotaVaziaEFalha(t *testing.T) {
	x := newLinFX()
	x.out["/usr/bin/ip -j route show default"] = ""
	x.out["/usr/bin/ip -j -6 route show default"] = "[]"
	r, _ := x.run(t, true)
	if !r.GatewaysRead || len(r.Gateways) != 0 {
		t.Errorf("sem rota: %v %v", r.GatewaysRead, r.Gateways)
	}
	x = newLinFX()
	delete(x.out, "/usr/bin/ip -j route show default")
	x.fail["/usr/bin/ip -j route show default"] = "boom"
	r, _ = x.run(t, true)
	if r.GatewaysRead {
		t.Error("falha de leitura não pode virar 'sem rota padrão'")
	}
	if _, ok := skippedArea(r, "rotas"); !ok {
		t.Error("falta o Skipped das rotas")
	}
}

func TestLinuxSemIPNaoDerrubaAsOutrasAreas(t *testing.T) {
	x := newLinFX()
	delete(x.tools, "ip")
	r, _ := x.run(t, true)
	if _, ok := skippedArea(r, "interfaces"); !ok {
		t.Error("sem o ip: Skipped de interfaces")
	}
	if len(r.Listeners) == 0 || len(r.Firewalls) == 0 {
		t.Error("as outras áreas devem seguir")
	}
}

func TestLinuxWiFiSoACONECTADAeSemVarredura(t *testing.T) {
	r, f := newLinFX().run(t, true)
	if r.WiFi == nil || r.WiFi.SSID != "REDE-FAKE" || r.WiFi.Security != SecWPA2 || r.WiFi.Interface != "wlp2s0" {
		t.Fatalf("wifi: %+v", r.WiFi)
	}
	for _, c := range f.Calls() {
		if strings.HasSuffix(c.Name, "nmcli") {
			a := strings.Join(c.Args, " ")
			if !strings.Contains(a, "--rescan no") || !strings.Contains(a, "list") || strings.Contains(a, "rescan yes") || strings.Contains(a, "rescan auto") {
				t.Errorf("o nmcli deve levar SEMPRE '--rescan no': %q", a)
			}
		}
	}
	// a rede vizinha do cache nunca entra no relatório
	if strings.Contains(Full(r, diagfmt.PT), "VIZINHA") || strings.Contains(Full(r, diagfmt.PT), "OUTRA") {
		t.Error("redes vizinhas do cache não podem aparecer")
	}
}

func TestLinuxWiFiSSIDComEscapeEControle(t *testing.T) {
	x := newLinFX()
	x.out["/usr/bin/nmcli -t -f ACTIVE,SSID,SECURITY dev wifi list --rescan no"] = "yes:REDE\\:FAKE\x1b[2J\u202e:WPA2\n"
	r, _ := x.run(t, true)
	if r.WiFi == nil || r.WiFi.SSID != "REDE:FAKE\x1b[2J\u202e" {
		t.Fatalf("SSID: %q", r.WiFi)
	}
}

func TestLinuxWiFiSemConexaoOuSemNmcli(t *testing.T) {
	x := newLinFX()
	x.out["/usr/bin/nmcli -t -f ACTIVE,SSID,SECURITY dev wifi list --rescan no"] = "no:A:WPA2\n"
	r, _ := x.run(t, true)
	if r.WiFi != nil {
		t.Error("sem rede ativa, sem WiFi")
	}
	if s, ok := skippedArea(r, "wifi"); !ok || !strings.Contains(s.Reason, "varredura") {
		t.Errorf("deve dizer que não verificou e que não varre: %+v", s)
	}
	x = newLinFX()
	delete(x.tools, "nmcli")
	r, _ = x.run(t, true)
	if _, ok := skippedArea(r, "wifi"); !ok || r.WiFi != nil {
		t.Error("com placa Wi-Fi e sem nmcli: Skipped")
	}
	// sem placa Wi-Fi: nada a ler e nada a pular
	x = newLinFX()
	x.noWifiHW = true
	delete(x.tools, "nmcli")
	r, _ = x.run(t, true)
	if _, ok := skippedArea(r, "wifi"); ok || r.WiFi != nil {
		t.Errorf("sem placa Wi-Fi não há o que pular: %+v", r.Skipped)
	}
}

func TestNmSecurity(t *testing.T) {
	for in, want := range map[string]Security{
		"": SecUnknown, "  ": SecUnknown, "--": SecOpen, "WEP": SecWEP, "WPA1": SecWPA, "WPA1 WPA2": SecWPA, "WPA2": SecWPA2, "WPA3": SecWPA3,
		"WPA2 WPA3": SecWPA2, "WPA3 WPA2": SecWPA2, "WPA2 802.1X": SecEnterprise, "802.1X": SecEnterprise, "OWE": SecUnknown, "XYZ": SecUnknown,
	} {
		if got := nmSecurity(in); got != want {
			t.Errorf("nmSecurity(%q)=%v, quer %v", in, got, want)
		}
	}
}

func TestLinuxDNSResolvectl(t *testing.T) {
	r, _ := newLinFX().run(t, true)
	if len(r.DNS.Servers) != 2 || r.DNS.Servers[0].Addr != "192.0.2.1" || r.DNS.Servers[1].Addr != "2001:db8::1" || r.DNS.Servers[0].Interface != "enp0s3" {
		t.Errorf("servidores: %+v", r.DNS.Servers)
	}
	if r.DNS.Encrypted != No || r.DNS.Mode != DNSModeOff {
		t.Errorf("-DNSOverTLS = não: %v %v", r.DNS.Encrypted, r.DNS.Mode)
	}
	x := newLinFX()
	// DoT estrito: a única forma de provar "criptografado"
	for _, strict := range []string{"+DNSOverTLS", "DNSOverTLS=yes"} {
		x.out["/usr/bin/resolvectl status"] = strings.ReplaceAll(linResolv, "-DNSOverTLS", strict)
		if r, _ = x.run(t, true); r.DNS.Encrypted != Yes || r.DNS.Mode != DNSModeStrict || r.DNS.AllowFallback != No {
			t.Errorf("%s = estrito: %v %v %v", strict, r.DNS.Encrypted, r.DNS.Mode, r.DNS.AllowFallback)
		}
	}
	// DoT oportunista admite DNS em claro: não é "criptografado"
	x.out["/usr/bin/resolvectl status"] = strings.ReplaceAll(linResolv, "-DNSOverTLS", "DNSOverTLS=opportunistic")
	r, _ = x.run(t, true)
	if r.DNS.Encrypted != Unknown || r.DNS.Mode != DNSModeOpportunistic || r.DNS.AllowFallback != Yes {
		t.Errorf("opportunistic: %v %v %v", r.DNS.Encrypted, r.DNS.Mode, r.DNS.AllowFallback)
	}
	if txt := DNSModeText(r.DNS, diagfmt.PT); !strings.Contains(txt, "oportunista") || !strings.Contains(txt, "DNS em claro") {
		t.Errorf("o texto do modo deve dizer 'oportunista' e 'DNS em claro': %q", txt)
	}
	fs := Analyze(func() *Report { r.DNS.Servers = []DNSServer{{Addr: "203.0.113.53"}}; return r }(), diagfmt.PT)
	if f := want(t, fs, "dns-publico-nao-verificado", SevInfo); !strings.Contains(f.Detail, "oportunista") {
		t.Errorf("o achado deve dizer 'oportunista': %q", f.Detail)
	}
	// lista de servidores em várias linhas
	x.out["/usr/bin/resolvectl status"] = "Link 2 (eth0)\n         Protocols: +DefaultRoute\n       DNS Servers: 192.0.2.1\n                    2001:db8::1\n                    198.51.100.1\n"
	r, _ = x.run(t, true)
	if len(r.DNS.Servers) != 3 || r.DNS.Encrypted != Unknown {
		t.Errorf("continuação: %+v %v", r.DNS.Servers, r.DNS.Encrypted)
	}
}

func TestLinuxDNSFallbackResolvConf(t *testing.T) {
	x := newLinFX()
	delete(x.tools, "resolvectl")
	x.files["/etc/resolv.conf"] = "# gerado\nnameserver 198.51.100.7\nnameserver 2001:db8::7\nsearch example.test\n"
	r, _ := x.run(t, true)
	if len(r.DNS.Servers) != 2 || r.DNS.Encrypted != Unknown {
		t.Errorf("resolv.conf: %+v %v", r.DNS.Servers, r.DNS.Encrypted)
	}
	delete(x.files, "/etc/resolv.conf")
	r, _ = x.run(t, true)
	if _, ok := skippedArea(r, "dns"); !ok {
		t.Error("sem nenhuma fonte de DNS: Skipped")
	}
}

func TestLinuxPortas(t *testing.T) {
	r, _ := newLinFX().run(t, true)
	if !r.ListenersRead || len(r.Listeners) != 6 {
		t.Fatalf("portas: %+v", r.Listeners)
	}
	got := map[string]Listener{}
	for _, l := range r.Listeners {
		got[l.Proto+l.Addr+itoa(l.Port)] = l
	}
	if l := got["tcp0.0.0.022"]; l.Process != "sshd" || l.PID != 812 {
		t.Errorf("sshd: %+v", l)
	}
	if _, ok := got["tcp::22"]; !ok {
		t.Error("[::]:22 deve virar :: porta 22")
	}
	if _, ok := got["udp127.0.0.53%lo53"]; !ok {
		t.Errorf("udp do resolvedor local: %v", got)
	}
	if l := got["tcp*8080"]; l.Process != `my "app"` {
		t.Errorf("nome com aspas: %+v", l)
	}
	// sem root, há porta sem processo: pede administrador; com root, o Skipped não existe
	if _, ok := skippedArea(r, "processos-das-portas"); ok {
		t.Error("com root não há o que pular")
	}
	r, _ = newLinFX().run(t, false)
	if s, ok := skippedArea(r, "processos-das-portas"); !ok || !s.NeedsAdmin {
		t.Errorf("sem root: NeedsAdmin: %+v", s)
	}
}

func TestLinuxFirewall(t *testing.T) {
	r, _ := newLinFX().run(t, true)
	names := map[string]Tri{}
	for _, f := range r.Firewalls {
		names[f.Name] = f.Enabled
	}
	if names["ufw"] != Yes || names["firewalld"] != Yes || names["nftables"] != Yes {
		t.Errorf("firewalls: %v", names)
	}
	x := newLinFX()
	x.out["/usr/sbin/ufw status"] = linUfwOff
	delete(x.out, "/usr/bin/firewall-cmd --state")
	x.fail["/usr/bin/firewall-cmd --state"] = "not running"
	x.out["/usr/sbin/nft list ruleset"] = ""
	r, _ = x.run(t, true)
	for _, f := range r.Firewalls {
		if f.Enabled != No {
			t.Errorf("%s deveria estar desligado: %+v", f.Name, f)
		}
	}
	if _, ok := skippedArea(r, "firewall"); ok {
		t.Error("com root tudo foi lido")
	}
}

// ufw e nft sem root NÃO são "desligado": ficam desconhecidos e pedem administrador.
func TestLinuxFirewallSemRootNaoViraLigadoNemDesligado(t *testing.T) {
	x := newLinFX()
	delete(x.out, "/usr/sbin/ufw status")
	x.fail["/usr/sbin/ufw status"] = "ERROR: You need to be root to run this script"
	delete(x.out, "/usr/bin/firewall-cmd --state")
	x.fail["/usr/bin/firewall-cmd --state"] = "Failed to connect: permission denied"
	r, f := x.run(t, false)
	if len(r.Firewalls) != 3 {
		t.Fatalf("firewalls: %+v", r.Firewalls)
	}
	for _, fw := range r.Firewalls {
		if fw.Enabled != Unknown {
			t.Errorf("%s sem root deve ser desconhecido: %v", fw.Name, fw.Enabled)
		}
	}
	if s, ok := skippedArea(r, "firewall"); !ok || !s.NeedsAdmin {
		t.Errorf("Skipped NeedsAdmin: %+v", s)
	}
	for _, c := range f.Calls() {
		if strings.HasSuffix(c.Name, "nft") {
			t.Error("o nft só é lido com root")
		}
	}
	fs := Analyze(r, diagfmt.PT)
	if _, ok := find(fs, "firewall-indeterminado"); !ok {
		t.Errorf("deve ficar indeterminado: %v", ids(fs))
	}
	if _, ok := find(fs, "firewall-ligado"); ok {
		t.Error("indeterminado nunca vira ligado")
	}
}

func TestLinuxFirewallNenhumConhecido(t *testing.T) {
	x := newLinFX()
	for _, n := range []string{"ufw", "firewall-cmd", "nft"} {
		delete(x.tools, n)
	}
	r, _ := x.run(t, true)
	if _, ok := skippedArea(r, "firewall"); !ok || len(r.Firewalls) != 0 {
		t.Errorf("sem firewall conhecido: %+v %v", r.Skipped, r.Firewalls)
	}
}

func TestNftState(t *testing.T) {
	cases := []struct {
		in   string
		want Tri
	}{
		{"", No},
		{linNft, Yes},
		{"table inet filter {\n\tchain input {\n\t\ttype filter hook input priority filter; policy accept;\n\t}\n}\n", Unknown},
		{"table inet filter {\n\tchain input {\n\t\ttype filter hook input priority filter; policy accept;\n\t\ttcp dport 23 drop\n\t}\n}\n", Yes},
		{"table inet filter {\n\tchain forward {\n\t\ttype filter hook forward priority filter; policy drop;\n\t}\n}\n", Unknown},
	}
	for _, c := range cases {
		if got, _ := nftState(c.in); got != c.want {
			t.Errorf("nftState(%q)=%v, quer %v", c.in, got, c.want)
		}
	}
}

func TestLinuxIPv6Privacidade(t *testing.T) {
	r, _ := newLinFX().run(t, true)
	if r.IPv6Privacy != Yes {
		t.Errorf("use_tempaddr=2: %v", r.IPv6Privacy)
	}
	x := newLinFX()
	x.files["/proc/sys/net/ipv6/conf/enp0s3/use_tempaddr"] = "0\n"
	if r, _ = x.run(t, true); r.IPv6Privacy != No {
		t.Errorf("use_tempaddr=0: %v", r.IPv6Privacy)
	}
	delete(x.files, "/proc/sys/net/ipv6/conf/enp0s3/use_tempaddr")
	r, _ = x.run(t, true)
	if r.IPv6Privacy != Unknown {
		t.Error("sem leitura: desconhecido")
	}
	if _, ok := skippedArea(r, "ipv6"); !ok {
		t.Error("sem leitura com IPv6 global: Skipped")
	}
}

func TestLinuxExecucaoCompleta(t *testing.T) {
	r, _ := newLinFX().run(t, true)
	fs := Analyze(r, diagfmt.PT)
	if _, ok := find(fs, "wifi-wpa2"); !ok {
		t.Errorf("achados: %v", ids(fs))
	}
	if _, ok := find(fs, "porta-22"); !ok {
		t.Errorf("sshd em 0.0.0.0: %v", ids(fs))
	}
}

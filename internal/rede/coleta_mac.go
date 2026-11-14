package rede

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/PiluTech/toolkit/internal/diagfmt"
)

// Coletor do macOS: só leitura, SEMPRE por caminho absoluto (nada de PATH). O tipo de segurança do Wi-Fi sai de
// `ipconfig getsummary <interface Wi-Fi>` (leitura passiva do que o configd já sabe; sem varredura, sem sudo), e não do
// system_profiler/wdutil que o plano citava (o system_profiler pode listar e varrer vizinhas; o wdutil exige sudo).
// `ipconfig setverbose` e qualquer subcomando de escrita ficam fora (teste de lista fechada).
const (
	macNetworksetup = "/usr/sbin/networksetup"
	macIfconfig     = "/sbin/ifconfig"
	macNetstat      = "/usr/sbin/netstat"
	macScutil       = "/usr/sbin/scutil"
	macLsof         = "/usr/sbin/lsof"
	macSysctl       = "/usr/sbin/sysctl"
	macIpconfig     = "/usr/sbin/ipconfig"
	macSocketFW     = "/usr/libexec/ApplicationFirewall/socketfilterfw"
	macPfctl        = "/sbin/pfctl"
)

// collectDarwin preenche r com a rede da máquina macOS. Cada área é independente (um pânico vira Skipped).
func collectDarwin(ctx context.Context, d Deps, r *Report) {
	skip := func(area, pt, en, es string, admin bool) {
		r.Skipped = append(r.Skipped, Skipped{Area: area, Reason: diagfmt.T(d.Lang, pt, en, es), NeedsAdmin: admin})
	}
	area := func(name string, fn func()) {
		defer func() {
			if p := recover(); p != nil {
				skip(name, fmt.Sprintf("falha inesperada na leitura desta área (%v)", p), fmt.Sprintf("unexpected failure reading this area (%v)", p), fmt.Sprintf("fallo inesperado al leer esta área (%v)", p), false)
			}
		}()
		fn()
	}
	var ports map[string]macPort
	area("interfaces", func() { ports = macInterfaces(ctx, d, r, skip) })
	area("rotas", func() { macRoutes(ctx, d, r, skip) })
	area("wifi", func() { macWiFi(ctx, d, r, ports, skip) })
	area("dns", func() { macDNS(ctx, d, r, skip) })
	area("portas", func() { macPorts(ctx, d, r, skip) })
	area("firewall", func() { macFirewall(ctx, d, r, skip) })
	area("ipv6", func() { macIPv6(ctx, d, r, skip) })
}

// macPort é uma "Hardware Port" do networksetup.
type macPort struct {
	Name, Device, MAC string
}

// parseHardwarePorts lê `networksetup -listallhardwareports`, indexado pelo nome do dispositivo (en0...).
func parseHardwarePorts(out string) map[string]macPort {
	ports := map[string]macPort{}
	var cur macPort
	flush := func() {
		if cur.Device != "" {
			ports[cur.Device] = cur
		}
		cur = macPort{}
	}
	for _, line := range strings.Split(out, "\n") {
		k, v, ok := strings.Cut(strings.TrimSpace(line), ":")
		if !ok {
			continue
		}
		v = strings.TrimSpace(v)
		switch strings.TrimSpace(k) {
		case "Hardware Port":
			flush()
			cur.Name = v
		case "Device":
			cur.Device = v
		case "Ethernet Address":
			cur.MAC = v
		}
	}
	flush()
	return ports
}

func (p macPort) isWiFi() bool {
	n := strings.ToLower(p.Name)
	return strings.Contains(n, "wi-fi") || strings.Contains(n, "airport")
}

var (
	ifHeadRe  = regexp.MustCompile(`^([A-Za-z0-9._-]+): flags=`)
	ifInetRe  = regexp.MustCompile(`^\s+inet (\S+)(?: --> \S+)? netmask (0x[0-9a-fA-F]+)`)
	ifInet6Re = regexp.MustCompile(`^\s+inet6 (\S+) prefixlen (\d+)(.*)$`)
)

func maskLen(hex string) int {
	v, err := strconv.ParseUint(strings.TrimPrefix(strings.ToLower(hex), "0x"), 16, 32)
	if err != nil {
		return 0
	}
	n := 0
	for ; v&0x80000000 != 0; v <<= 1 {
		n++
		v &= 0xffffffff
	}
	return n
}

// parseIfconfig lê `ifconfig -a`. Mantém a interface que tem IPv4, IPv6 global/ULA ou está ativa (descarta lo0, gif, stf,
// awdl, llw, anpi e afins, que só têm endereço link-local).
func parseIfconfig(out string) []Interface {
	var all []Interface
	var cur *Interface
	active := map[string]bool{}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimRight(line, "\r")
		if m := ifHeadRe.FindStringSubmatch(line); m != nil {
			all = append(all, Interface{Name: m[1]})
			cur = &all[len(all)-1]
			continue
		}
		if cur == nil {
			continue
		}
		t := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(t, "ether "):
			cur.MAC = strings.TrimSpace(strings.TrimPrefix(t, "ether "))
		case strings.HasPrefix(t, "status:"):
			if strings.Contains(t, "active") && !strings.Contains(t, "inactive") {
				active[cur.Name] = true
				cur.Up = Yes
			} else {
				cur.Up = No
			}
		}
		if m := ifInetRe.FindStringSubmatch(line); m != nil {
			cur.IPv4 = append(cur.IPv4, Addr{IP: m[1], PrefixLen: maskLen(m[2])})
		}
		if m := ifInet6Re.FindStringSubmatch(line); m != nil {
			n, _ := strconv.Atoi(m[2])
			cur.IPv6 = append(cur.IPv6, Addr{IP: strings.SplitN(m[1], "%", 2)[0], PrefixLen: n, Temporary: strings.Contains(m[3], "temporary")})
		}
	}
	var out2 []Interface
	for _, i := range all {
		if strings.HasPrefix(i.Name, "lo") {
			continue
		}
		keep := len(i.IPv4) > 0 || active[i.Name]
		for _, a := range i.IPv6 {
			if ip, ok := parseIP(a.IP); ok && !ip.IsLinkLocalUnicast() {
				keep = true
			}
		}
		if keep {
			out2 = append(out2, i)
		}
	}
	return out2
}

func macInterfaces(ctx context.Context, d Deps, r *Report, skip skipFn) map[string]macPort {
	var ports map[string]macPort
	if out, err := d.run(ctx, unixCmd(macNetworksetup, "-listallhardwareports")); err == nil {
		ports = parseHardwarePorts(out)
	}
	out, err := d.run(ctx, unixCmd(macIfconfig, "-a"))
	if err != nil {
		skip("interfaces", "não deu para ler as interfaces e os IPs (ifconfig)", "could not read the interfaces and IPs (ifconfig)", "no se pudieron leer las interfaces y las IPs (ifconfig)", false)
		return ports
	}
	for _, i := range parseIfconfig(out) {
		if p, ok := ports[i.Name]; ok {
			switch {
			case p.isWiFi():
				i.Kind = "wifi"
			case strings.Contains(strings.ToLower(p.Name), "ethernet"):
				i.Kind = "ethernet"
			}
		}
		r.Interfaces = append(r.Interfaces, i)
	}
	return ports
}

// parseNetstatDefaults lê `netstat -rn -f inet|inet6`: as linhas "default" com o gateway (ou "dev <interface>" quando o
// gateway é um "link#N").
func parseNetstatDefaults(out string) []string {
	var gws []string
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		if len(f) < 4 || f[0] != "default" {
			continue
		}
		g := f[1]
		if strings.HasPrefix(g, "link#") {
			g = "dev " + f[3]
		}
		gws = append(gws, g)
	}
	return gws
}

func macRoutes(ctx context.Context, d Deps, r *Report, skip skipFn) {
	v4, err := d.run(ctx, unixCmd(macNetstat, "-rn", "-f", "inet"))
	if err != nil {
		skip("rotas", "não deu para ler a rota padrão (netstat -rn)", "could not read the default route (netstat -rn)", "no se pudo leer la ruta predeterminada (netstat -rn)", false)
		return
	}
	r.Gateways = append(r.Gateways, parseNetstatDefaults(v4)...)
	r.GatewaysRead = true
	if v6, err := d.run(ctx, unixCmd(macNetstat, "-rn", "-f", "inet6")); err == nil {
		r.Gateways = append(r.Gateways, parseNetstatDefaults(v6)...)
	}
}

var ipcfgKeyRe = regexp.MustCompile(`^ {2}(SSID|Security)\s*:\s*(.*?)\s*$`)

// macSecurity classifica o campo Security do `ipconfig getsummary` ("NONE", "WEP", "WPA_PSK", "WPA2_PSK", "WPA3_SAE",
// "WPA2_ENT"...). O formato exato por versão do macOS é A VERIFICAR: token desconhecido = SecUnknown (nunca "seguro").
func macSecurity(s string) Security {
	has := tokenSet(s)
	switch {
	case has["NONE"] || has["OPEN"]: // aberto SÓ com token explícito; vazio/pontuação cai em SecUnknown
		return SecOpen
	case (has["ENT"] || has["EAP"] || has["8021X"]) && (has["WPA"] || has["WPA2"] || has["WPA3"]):
		return SecEnterprise
	case has["WPA3"] && has["WPA2"]: // transição: aceita WPA2, então vale o mais fraco
		return SecWPA2
	case has["WPA3"] || has["SAE"]:
		return SecWPA3
	case has["WPA2"]:
		return SecWPA2
	case has["WPA"]:
		return SecWPA
	case has["WEP"]:
		return SecWEP
	}
	return SecUnknown
}

// parseIpconfigSummary lê só as linhas "SSID :" e "Security :" do nível da interface (recuo de 2 espaços) (o resto, como o pacote DHCP em
// várias linhas, é ignorado). SSID "<redacted>" (o macOS o oculta) vira "".
func parseIpconfigSummary(out string) (ssid, security string, found bool) {
	for _, line := range strings.Split(out, "\n") {
		m := ipcfgKeyRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		switch m[1] {
		case "SSID":
			if ssid == "" && m[2] != "<redacted>" {
				ssid = m[2]
			}
		case "Security":
			if !found {
				security, found = m[2], true
			}
		}
	}
	return ssid, security, found
}

func macWiFi(ctx context.Context, d Deps, r *Report, ports map[string]macPort, skip skipFn) {
	var names []string
	for name := range ports {
		names = append(names, name)
	}
	sort.Strings(names)
	var dev string
	for _, name := range names {
		if ports[name].isWiFi() && safeIface(name) { // o nome vem do sistema e vira ARGUMENTO: só formato de interface
			dev = name
			break
		}
	}
	if dev == "" {
		return // sem placa Wi-Fi
	}
	connected := false
	for _, i := range r.Interfaces {
		if i.Name == dev && i.Up == Yes {
			connected = true
		}
	}
	if !connected {
		return // Wi-Fi sem conexão ativa: nada a ler
	}
	out, err := d.run(ctx, unixCmd(macIpconfig, "getsummary", dev))
	if err != nil {
		skip("wifi", "não deu para ler a conexão Wi-Fi (ipconfig getsummary)", "could not read the Wi-Fi connection (ipconfig getsummary)", "no se pudo leer la conexión Wi-Fi (ipconfig getsummary)", false)
		return
	}
	ssid, sec, found := parseIpconfigSummary(out)
	if !found {
		skip("wifi", "o macOS não informou o tipo de segurança do Wi-Fi conectado (formato A VERIFICAR nesta versão): veja em Ajustes > Wi-Fi > Detalhes", "macOS did not report the security type of the connected Wi-Fi (format TO BE VERIFIED on this version): see System Settings > Wi-Fi > Details", "macOS no informó el tipo de seguridad del Wi-Fi conectado (formato POR VERIFICAR en esta versión): vea Ajustes > Wi-Fi > Detalles", false)
		return
	}
	r.WiFi = &WiFi{Interface: dev, SSID: ssid, Security: macSecurity(sec), Raw: sec}
}

var (
	scutilNSRe = regexp.MustCompile(`^\s*nameserver\[\d+\]\s*:\s*(\S+)`)
	scutilIfRe = regexp.MustCompile(`^\s*if_index\s*:\s*\d+\s*\(([^)]*)\)`)
)

// parseScutilDNS lê `scutil --dns` (só a primeira configuração, não a de consultas com escopo): os servidores dos
// resolvedores que não são só de um domínio (Supplemental), sem repetir.
func parseScutilDNS(out string) []DNSServer {
	type res struct {
		servers []string
		iface   string
		supp    bool
	}
	var rs []*res
	var cur *res
loop:
	for _, line := range strings.Split(out, "\n") {
		t := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(t, "DNS configuration (for scoped queries)"):
			break loop // a segunda lista repete a primeira, com escopo
		case strings.HasPrefix(t, "resolver #"):
			cur = &res{}
			rs = append(rs, cur)
			continue
		}
		if cur == nil {
			continue
		}
		if m := scutilNSRe.FindStringSubmatch(line); m != nil {
			cur.servers = append(cur.servers, m[1])
		} else if m := scutilIfRe.FindStringSubmatch(line); m != nil {
			cur.iface = m[1]
		} else if strings.HasPrefix(t, "flags") && strings.Contains(t, "Supplemental") {
			cur.supp = true
		}
	}
	var servers []DNSServer
	for _, x := range rs {
		if x.supp {
			continue
		}
		for _, s := range x.servers {
			servers = append(servers, DNSServer{Addr: s, Interface: x.iface})
		}
	}
	return dedupeDNS(servers)
}

func macDNS(ctx context.Context, d Deps, r *Report, skip skipFn) {
	out, err := d.run(ctx, unixCmd(macScutil, "--dns"))
	if err != nil {
		skip("dns", "não deu para ler o DNS (scutil --dns)", "could not read the DNS (scutil --dns)", "no se pudo leer el DNS (scutil --dns)", false)
		return
	}
	servers := parseScutilDNS(out)
	if len(servers) == 0 {
		skip("dns", "o scutil não listou nenhum servidor DNS", "scutil listed no DNS server", "scutil no listó ningún servidor DNS", false)
		return
	}
	r.DNS = DNS{Servers: servers, Encrypted: Unknown, Mode: DNSModeUnknown, AllowFallback: Unknown, Note: diagfmt.T(d.Lang, "scutil --dns: DoH/DoT não informado", "scutil --dns: DoH/DoT not reported", "scutil --dns: DoH/DoT no informado")}
}

// parseNetstatListen lê `netstat -an -p tcp`: as linhas LISTEN, com o endereço local no formato "host.porta" do macOS.
func parseNetstatListen(out string) []Listener {
	var ls []Listener
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		if len(f) < 6 || !strings.HasPrefix(f[0], "tcp") || f[len(f)-1] != "LISTEN" {
			continue
		}
		local := f[3]
		i := strings.LastIndexByte(local, '.')
		if i < 0 {
			continue
		}
		port, err := strconv.Atoi(local[i+1:])
		if err != nil || port < 0 || port > 65535 {
			continue
		}
		ls = append(ls, Listener{Proto: "tcp", Addr: local[:i], Port: port})
	}
	return ls
}

type lsofProc struct {
	name string
	pid  int
}

var lsofEscRe = regexp.MustCompile(`\\x([0-9a-fA-F]{2})`)

// parseLsofListen lê `lsof -nP -iTCP -sTCP:LISTEN` e devolve processo e PID por porta (o lsof sem administrador só vê os
// processos do próprio usuário, então o mapa pode ser parcial).
func parseLsofListen(out string) map[int]lsofProc {
	m := map[int]lsofProc{}
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		if len(f) < 9 || f[0] == "COMMAND" {
			continue
		}
		name := f[len(f)-2] // "... TCP *:51387 (LISTEN)": o endereço é o penúltimo campo
		if f[len(f)-1] != "(LISTEN)" {
			name = f[len(f)-1]
		}
		i := strings.LastIndexByte(name, ':')
		if i < 0 {
			continue
		}
		port, err := strconv.Atoi(name[i+1:])
		if err != nil {
			continue
		}
		pid, _ := strconv.Atoi(f[1])
		cmd := lsofEscRe.ReplaceAllStringFunc(f[0], func(s string) string {
			n, _ := strconv.ParseUint(s[2:], 16, 8)
			return string(rune(n))
		})
		if _, dup := m[port]; !dup {
			m[port] = lsofProc{name: cmd, pid: pid}
		}
	}
	return m
}

func macPorts(ctx context.Context, d Deps, r *Report, skip skipFn) {
	// O netstat lista TODOS os sockets em escuta (inclusive de processos de outros usuários); o lsof dá o nome do processo, mas
	// sem administrador só enxerga os do próprio usuário. Por isso o conjunto de portas vem do netstat.
	out, err := d.run(ctx, unixCmd(macNetstat, "-an", "-p", "tcp"))
	if err != nil {
		skip("portas", "não deu para ler as portas em escuta (netstat -an)", "could not read the listening ports (netstat -an)", "no se pudieron leer los puertos en escucha (netstat -an)", false)
		return
	}
	ls := parseNetstatListen(out)
	r.ListenersRead = true
	procs := map[int]lsofProc{}
	if lo, err := d.run(ctx, unixCmd(macLsof, "-nP", "-iTCP", "-sTCP:LISTEN")); err == nil {
		procs = parseLsofListen(lo)
	} else {
		skip("processos-das-portas", "não deu para ler o nome dos processos das portas (lsof)", "could not read the process names of the ports (lsof)", "no se pudieron leer los nombres de los procesos de los puertos (lsof)", false)
	}
	missing := false
	for i := range ls {
		if p, ok := procs[ls[i].Port]; ok {
			ls[i].Process, ls[i].PID = p.name, p.pid
		} else {
			missing = true
		}
	}
	r.Listeners = ls
	if missing && !d.Admin {
		skip("processos-das-portas", "sem administrador o lsof só mostra o processo dos seus próprios programas; o nome do processo das outras portas não aparece", "without administrator rights lsof only shows the process of your own programs; the process name of the other ports is not shown", "sin administrador lsof solo muestra el proceso de sus propios programas; el nombre del proceso de los demás puertos no aparece", true)
	}
}

var (
	alfStateRe = regexp.MustCompile(`State\s*=\s*(\d)`)
	pfStateRe  = regexp.MustCompile(`(?im)^Status:\s*(Enabled|Disabled)`)
)

func macFirewall(ctx context.Context, d Deps, r *Report, skip skipFn) {
	fw := Firewall{Name: "aplicativos"}
	if out, err := d.run(ctx, unixCmd(macSocketFW, "--getglobalstate")); err == nil {
		if m := alfStateRe.FindStringSubmatch(out); m != nil {
			fw.Detail = "State = " + m[1]
			if m[1] == "0" {
				fw.Enabled = No
			} else {
				fw.Enabled = Yes // 1 = ligado; 2 = ligado bloqueando todas as conexões de entrada
			}
		}
	}
	r.Firewalls = append(r.Firewalls, fw)
	if fw.Enabled == Unknown {
		skip("firewall", "não deu para ler o firewall de aplicativos (socketfilterfw)", "could not read the application firewall (socketfilterfw)", "no se pudo leer el firewall de aplicaciones (socketfilterfw)", false)
	}
	if !d.Admin {
		skip("pf", "o firewall de pacotes (pf) só pode ser lido com administrador; ele vem desligado por padrão e o resultado acima é só do firewall de aplicativos", "the packet filter (pf) can only be read with administrator rights; it is off by default and the result above covers the application firewall only", "el filtro de paquetes (pf) solo puede leerse con administrador; viene desactivado por defecto y el resultado de arriba es solo del firewall de aplicaciones", true)
		return
	}
	out, err := d.run(ctx, unixCmd(macPfctl, "-s", "info"))
	pf := Firewall{Name: "pf"}
	if m := pfStateRe.FindStringSubmatch(out); err == nil && m != nil {
		if strings.EqualFold(m[1], "Enabled") {
			pf.Enabled = Yes
		} else {
			pf.Enabled = No
		}
	}
	r.Firewalls = append(r.Firewalls, pf)
}

func macIPv6(ctx context.Context, d Deps, r *Report, skip skipFn) {
	if !hasGlobalV6(r) {
		return
	}
	out, err := d.run(ctx, unixCmd(macSysctl, "-n", "net.inet6.ip6.use_tempaddr"))
	v, perr := strconv.Atoi(strings.TrimSpace(out))
	if err != nil || perr != nil {
		skip("ipv6", "não deu para ler net.inet6.ip6.use_tempaddr", "could not read net.inet6.ip6.use_tempaddr", "no se pudo leer net.inet6.ip6.use_tempaddr", false)
		return
	}
	r.IPv6Privacy = Yes
	if v < 1 {
		r.IPv6Privacy = No
	}
}

package rede

import (
	"context"
	"encoding/json"
	"fmt"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/PiluTech/toolkit/internal/diagfmt"
)

// Coletor do Linux: só leitura (ip, ss, nmcli, resolvectl, ufw, firewall-cmd, nft e arquivos de /proc e /sys), com
// LC_ALL=C e o caminho absoluto achado por LookPath. Nenhum comando varre a rede: o nmcli leva SEMPRE "--rescan no"
// (lista em cache, sem disparar scan de Wi-Fi); "iw dev X scan" e "nmcli dev wifi rescan" não existem aqui.

// collectLinux preenche r com a rede da máquina Linux. Cada área é independente (um pânico vira Skipped).
func collectLinux(ctx context.Context, d Deps, r *Report) {
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
	area("interfaces", func() { linInterfaces(ctx, d, r, skip) })
	area("rotas", func() { linRoutes(ctx, d, r, skip) })
	area("wifi", func() { linWiFi(ctx, d, r, skip) })
	area("dns", func() { linDNS(ctx, d, r, skip) })
	area("portas", func() { linPorts(ctx, d, r, skip) })
	area("firewall", func() { linFirewall(ctx, d, r, skip) })
	area("ipv6", func() { linIPv6(d, r, skip) })
}

type skipFn func(area, pt, en, es string, admin bool)

type ipAddrJSON struct {
	Ifname    string `json:"ifname"`
	Address   string `json:"address"`
	Operstate string `json:"operstate"`
	LinkType  string `json:"link_type"`
	AddrInfo  []struct {
		Family    string `json:"family"`
		Local     string `json:"local"`
		Prefixlen int    `json:"prefixlen"`
		Temporary bool   `json:"temporary"`
	} `json:"addr_info"`
}

func linInterfaces(ctx context.Context, d Deps, r *Report, skip skipFn) {
	ip := d.which("ip")
	if ip == "" {
		skip("interfaces", "o comando ip não foi encontrado: não deu para listar as interfaces e os IPs", "the ip command was not found: could not list the interfaces and IPs", "no se encontró el comando ip: no se pudieron listar las interfaces y las IPs", false)
		return
	}
	out, err := d.run(ctx, unixCmd(ip, "-j", "addr"))
	var list []ipAddrJSON
	if err != nil || json.Unmarshal([]byte(strings.TrimSpace(out)), &list) != nil {
		skip("interfaces", "não deu para ler as interfaces e os IPs (ip -j addr)", "could not read the interfaces and IPs (ip -j addr)", "no se pudieron leer las interfaces y las IPs (ip -j addr)", false)
		return
	}
	for _, e := range list {
		if e.LinkType == "loopback" || e.Ifname == "" {
			continue
		}
		it := Interface{Name: e.Ifname, MAC: e.Address, Kind: "ethernet"}
		if e.LinkType != "ether" {
			it.Kind = ""
		}
		if safeIface(e.Ifname) {
			if w, _ := d.glob(path.Join("/sys/class/net", e.Ifname, "wireless")); len(w) > 0 {
				it.Kind = "wifi"
			}
		}
		switch strings.ToUpper(e.Operstate) {
		case "UP":
			it.Up = Yes
		case "DOWN":
			it.Up = No
		}
		for _, a := range e.AddrInfo {
			ad := Addr{IP: a.Local, PrefixLen: a.Prefixlen, Temporary: a.Temporary}
			switch a.Family {
			case "inet":
				it.IPv4 = append(it.IPv4, ad)
			case "inet6":
				it.IPv6 = append(it.IPv6, ad)
			}
		}
		r.Interfaces = append(r.Interfaces, it)
	}
}

type ipRouteJSON struct {
	Dst     string `json:"dst"`
	Gateway string `json:"gateway"`
	Dev     string `json:"dev"`
}

func linRoutes(ctx context.Context, d Deps, r *Report, skip skipFn) {
	ip := d.which("ip")
	if ip == "" {
		skip("rotas", "o comando ip não foi encontrado: não deu para ler a rota padrão", "the ip command was not found: could not read the default route", "no se encontró el comando ip: no se pudo leer la ruta predeterminada", false)
		return
	}
	read := func(args ...string) ([]string, bool) {
		out, err := d.run(ctx, unixCmd(ip, args...))
		if err != nil {
			return nil, false
		}
		var rs []ipRouteJSON
		if s := strings.TrimSpace(out); s != "" && json.Unmarshal([]byte(s), &rs) != nil {
			return nil, false
		}
		var gws []string
		for _, x := range rs {
			g := x.Gateway
			if g == "" {
				g = "dev " + x.Dev
			}
			gws = append(gws, g)
		}
		return gws, true
	}
	v4, ok := read("-j", "route", "show", "default")
	if !ok {
		skip("rotas", "não deu para ler a rota padrão (ip -j route)", "could not read the default route (ip -j route)", "no se pudo leer la ruta predeterminada (ip -j route)", false)
		return
	}
	r.Gateways = append(r.Gateways, v4...)
	r.GatewaysRead = true
	if v6, ok := read("-j", "-6", "route", "show", "default"); ok {
		r.Gateways = append(r.Gateways, v6...)
	}
}

// nmcliSplit separa os campos do modo terso do nmcli, desfazendo o escape ("\:" e "\\") dentro de cada campo.
func nmcliSplit(line string) []string {
	var fields []string
	var cur strings.Builder
	for i := 0; i < len(line); i++ {
		switch c := line[i]; {
		case c == '\\' && i+1 < len(line):
			i++
			cur.WriteByte(line[i])
		case c == ':':
			fields = append(fields, cur.String())
			cur.Reset()
		default:
			cur.WriteByte(c)
		}
	}
	return append(fields, cur.String())
}

// nmSecurity classifica o campo SECURITY do nmcli ("WPA2", "WPA1 WPA2", "WPA3", "WPA2 WPA3", "WEP", "802.1X", "--").
func nmSecurity(s string) Security {
	s = strings.TrimSpace(s)
	if s == "" || s == "--" {
		return SecOpen
	}
	if strings.Contains(s, "802.1X") {
		return SecEnterprise
	}
	has := map[string]bool{}
	for _, tok := range strings.Fields(s) {
		has[tok] = true
	}
	switch {
	case has["WPA3"] && has["WPA2"]: // modo de transição: aceita WPA2, então vale o mais fraco
		return SecWPA2
	case has["WPA3"]:
		return SecWPA3
	case has["WPA1"]:
		return SecWPA
	case has["WPA2"]:
		return SecWPA2
	case has["WEP"]:
		return SecWEP
	}
	return SecUnknown
}

func linWiFi(ctx context.Context, d Deps, r *Report, skip skipFn) {
	var wlan string
	for _, i := range r.Interfaces {
		if i.Kind == "wifi" {
			wlan = i.Name
			break
		}
	}
	if wlan == "" {
		if ws, _ := d.glob("/sys/class/net/*/wireless"); len(ws) == 0 {
			return // sem placa Wi-Fi: nada a ler (e nada a pular)
		}
	}
	nm := d.which("nmcli")
	if nm == "" {
		skip("wifi", "há placa Wi-Fi, mas o nmcli (NetworkManager) não foi encontrado: o tipo de segurança do Wi-Fi não foi lido", "there is a Wi-Fi adapter, but nmcli (NetworkManager) was not found: the Wi-Fi security type was not read", "hay tarjeta Wi-Fi, pero no se encontró nmcli (NetworkManager): no se leyó el tipo de seguridad del Wi-Fi", false)
		return
	}
	// --rescan no: lista em CACHE; nunca dispara varredura (emite sinal de rádio e parece sondagem).
	out, err := d.run(ctx, unixCmd(nm, "-t", "-f", "ACTIVE,SSID,SECURITY", "dev", "wifi", "list", "--rescan", "no"))
	if err != nil {
		skip("wifi", "não deu para ler o Wi-Fi no cache do NetworkManager (nmcli)", "could not read the Wi-Fi from the NetworkManager cache (nmcli)", "no se pudo leer el Wi-Fi de la caché de NetworkManager (nmcli)", false)
		return
	}
	for _, line := range strings.Split(out, "\n") {
		f := nmcliSplit(strings.TrimRight(line, "\r"))
		if len(f) < 3 || f[0] != "yes" { // só a rede CONECTADA; as vizinhas do cache são descartadas aqui
			continue
		}
		sec := nmSecurity(f[2])
		raw := f[2]
		if raw == "" {
			raw = "--"
		}
		r.WiFi = &WiFi{Interface: wlan, SSID: f[1], Security: sec, Raw: raw}
		return
	}
	skip("wifi", "nenhuma rede Wi-Fi conectada na lista em cache do NetworkManager (a leitura não faz varredura): não verificado", "no connected Wi-Fi network in the NetworkManager cache (the read never scans): not verified", "ninguna red Wi-Fi conectada en la caché de NetworkManager (la lectura nunca escanea): no verificado", false)
}

var (
	resLinkRe = regexp.MustCompile(`^Link\s+\d+\s+\(([^)]*)\)`)
	dotRe     = regexp.MustCompile(`(?:^|\s)([+-])DNSOverTLS(?:\s|$)|DNSOverTLS=(yes|opportunistic|no)`)
)

// parseResolvectl lê "resolvectl status": os servidores por seção (Global e Link N) e o DNS sobre TLS de cada uma.
func parseResolvectl(out string) (servers []DNSServer, enc Tri) {
	type section struct {
		name    string
		servers []string
		dot     Tri
	}
	var secs []*section
	var cur *section
	var global Tri
	inServers := false
	for _, raw := range strings.Split(out, "\n") {
		line := strings.TrimRight(raw, "\r")
		trim := strings.TrimSpace(line)
		switch trim {
		case "":
			inServers = false
			continue
		case "Global":
			cur = &section{name: ""}
			secs = append(secs, cur)
			inServers = false
			continue
		}
		if m := resLinkRe.FindStringSubmatch(trim); m != nil {
			cur = &section{name: m[1]}
			secs = append(secs, cur)
			inServers = false
			continue
		}
		if cur == nil {
			continue
		}
		if k, v, ok := cutKey(trim); ok {
			inServers = false
			switch k {
			case "DNS Servers":
				cur.servers = append(cur.servers, strings.Fields(v)...)
				inServers = true
			case "Protocols":
				if m := dotRe.FindStringSubmatch(v); m != nil {
					t := m[1]
					if t == "" {
						t = map[string]string{"yes": "+", "opportunistic": "+", "no": "-"}[m[2]]
					}
					if t == "+" {
						cur.dot = Yes
					} else {
						cur.dot = No
					}
					if cur.name == "" {
						global = cur.dot
					}
				}
			}
			continue
		}
		if inServers { // continuação da lista de servidores (uma coluna só de endereços)
			cur.servers = append(cur.servers, strings.Fields(trim)...)
		}
	}
	allYes, anyNo, withServers := true, false, 0
	for _, s := range secs {
		if len(s.servers) == 0 {
			continue
		}
		withServers++
		dot := s.dot
		if dot == Unknown {
			dot = global
		}
		for _, a := range s.servers {
			servers = append(servers, DNSServer{Addr: a, Interface: s.name})
		}
		switch dot {
		case No:
			anyNo = true
			allYes = false
		case Unknown:
			allYes = false
		}
	}
	switch {
	case withServers == 0:
		return servers, Unknown
	case anyNo:
		return servers, No
	case allYes:
		return servers, Yes
	}
	return servers, Unknown
}

// cutKey separa "Chave: valor" (dois-pontos seguido de espaço, ou no fim da linha). Um IPv6 nunca tem ": ", então a linha
// de continuação da lista de servidores não é confundida com uma chave.
func cutKey(s string) (k, v string, ok bool) {
	if i := strings.Index(s, ": "); i >= 0 {
		return strings.TrimSpace(s[:i]), strings.TrimSpace(s[i+2:]), true
	}
	if strings.HasSuffix(s, ":") && !strings.Contains(strings.TrimSuffix(s, ":"), ":") {
		return strings.TrimSuffix(s, ":"), "", true
	}
	return "", "", false
}

func linDNS(ctx context.Context, d Deps, r *Report, skip skipFn) {
	if rc := d.which("resolvectl"); rc != "" {
		if out, err := d.run(ctx, unixCmd(rc, "status")); err == nil {
			servers, enc := parseResolvectl(out)
			if len(servers) > 0 {
				r.DNS = DNS{Servers: dedupeDNS(servers), Encrypted: enc, Note: "resolvectl"}
				return
			}
		}
	}
	b, err := d.readFile("/etc/resolv.conf")
	if err != nil {
		skip("dns", "não deu para ler o DNS (resolvectl e /etc/resolv.conf)", "could not read the DNS (resolvectl and /etc/resolv.conf)", "no se pudo leer el DNS (resolvectl y /etc/resolv.conf)", false)
		return
	}
	var servers []DNSServer
	for _, line := range strings.Split(string(b), "\n") {
		f := strings.Fields(line)
		if len(f) >= 2 && f[0] == "nameserver" {
			servers = append(servers, DNSServer{Addr: f[1]})
		}
	}
	if len(servers) == 0 {
		skip("dns", "o /etc/resolv.conf não lista nenhum servidor DNS", "/etc/resolv.conf lists no DNS server", "/etc/resolv.conf no lista ningún servidor DNS", false)
		return
	}
	r.DNS = DNS{Servers: servers, Encrypted: Unknown, Note: diagfmt.T(d.Lang, "/etc/resolv.conf: DoH/DoT não informado", "/etc/resolv.conf: DoH/DoT not reported", "/etc/resolv.conf: DoH/DoT no informado")}
}

func dedupeDNS(in []DNSServer) []DNSServer {
	seen := map[string]bool{}
	var out []DNSServer
	for _, s := range in {
		k := s.Addr + "|" + s.Interface
		if !seen[k] {
			seen[k] = true
			out = append(out, s)
		}
	}
	return out
}

var ssProcRe = regexp.MustCompile(`\(\("((?:[^"\\]|\\.)*)",pid=(\d+)`)

// splitHostPort separa "0.0.0.0:22", "[::]:22", "*:5353" e "127.0.0.53%lo:53" no último ":".
func splitHostPort(s string) (host string, port int, ok bool) {
	i := strings.LastIndexByte(s, ':')
	if i < 0 {
		return "", 0, false
	}
	p, err := strconv.Atoi(s[i+1:])
	if err != nil || p < 0 || p > 65535 {
		return "", 0, false
	}
	return strings.TrimSuffix(strings.TrimPrefix(s[:i], "["), "]"), p, true
}

// parseSS lê "ss -H -tulnp": uma linha por socket em escuta (TCP LISTEN) ou ligado (UDP UNCONN).
func parseSS(out string) []Listener {
	var ls []Listener
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		if len(f) < 5 {
			continue
		}
		proto := f[0]
		if proto != "tcp" && proto != "udp" {
			continue
		}
		host, port, ok := splitHostPort(f[4])
		if !ok {
			continue
		}
		li := Listener{Proto: proto, Addr: host, Port: port}
		if m := ssProcRe.FindStringSubmatch(strings.Join(f[5:], " ")); m != nil {
			li.Process = strings.ReplaceAll(m[1], `\"`, `"`)
			li.PID, _ = strconv.Atoi(m[2])
		}
		ls = append(ls, li)
	}
	return ls
}

func linPorts(ctx context.Context, d Deps, r *Report, skip skipFn) {
	ss := d.which("ss")
	if ss == "" {
		skip("portas", "o comando ss não foi encontrado: as portas em escuta não foram lidas", "the ss command was not found: listening ports were not read", "no se encontró el comando ss: no se leyeron los puertos en escucha", false)
		return
	}
	out, err := d.run(ctx, unixCmd(ss, "-H", "-t", "-u", "-l", "-n", "-p"))
	if err != nil {
		skip("portas", "não deu para ler as portas em escuta (ss)", "could not read the listening ports (ss)", "no se pudieron leer los puertos en escucha (ss)", false)
		return
	}
	r.Listeners, r.ListenersRead = parseSS(out), true
	if !d.Admin {
		for _, li := range r.Listeners {
			if li.Process == "" {
				skip("processos-das-portas", "sem administrador o ss só mostra o processo dos seus próprios programas; o nome do processo das outras portas não aparece", "without administrator rights ss only shows the process of your own programs; the process name of the other ports is not shown", "sin administrador ss solo muestra el proceso de sus propios programas; el nombre del proceso de los demás puertos no aparece", true)
				break
			}
		}
	}
}

func linFirewall(ctx context.Context, d Deps, r *Report, skip skipFn) {
	var needAdmin []string
	if ufw := d.which("ufw"); ufw != "" {
		fw := Firewall{Name: "ufw"}
		out, err := d.run(ctx, unixCmd(ufw, "status"))
		switch st := ufwStatus(out); {
		case err == nil && st == "active":
			fw.Enabled = Yes
		case err == nil && st == "inactive":
			fw.Enabled = No
		default:
			needAdmin = append(needAdmin, "ufw")
		}
		r.Firewalls = append(r.Firewalls, fw)
	}
	if fc := d.which("firewall-cmd"); fc != "" {
		fw := Firewall{Name: "firewalld"}
		out, err := d.run(ctx, unixCmd(fc, "--state"))
		text := strings.ToLower(strings.TrimSpace(out))
		if err != nil {
			text += " " + strings.ToLower(err.Error())
		}
		switch {
		case strings.Contains(text, "not running"):
			fw.Enabled = No
		case err == nil && strings.Contains(text, "running"):
			fw.Enabled = Yes
		default:
			needAdmin = append(needAdmin, "firewall-cmd")
		}
		r.Firewalls = append(r.Firewalls, fw)
	}
	if nft := d.which("nft"); nft != "" {
		fw := Firewall{Name: "nftables"}
		if d.Admin {
			if out, err := d.run(ctx, unixCmd(nft, "list", "ruleset")); err == nil {
				fw.Enabled, fw.Detail = nftState(out)
			}
		} else {
			needAdmin = append(needAdmin, "nft")
		}
		r.Firewalls = append(r.Firewalls, fw)
	}
	if len(r.Firewalls) == 0 {
		skip("firewall", "nenhum firewall conhecido foi encontrado (ufw, firewalld, nftables): não dá para saber se há um ativo", "no known firewall was found (ufw, firewalld, nftables): cannot tell whether one is active", "no se encontró ningún firewall conocido (ufw, firewalld, nftables): no se puede saber si hay uno activo", false)
		return
	}
	if len(needAdmin) > 0 && !d.Admin {
		skip("firewall", "o firewall ("+strings.Join(needAdmin, ", ")+") só pode ser lido com administrador: sem ele o estado fica como não informado", "the firewall ("+strings.Join(needAdmin, ", ")+") can only be read with administrator rights: without it the state stays not reported", "el firewall ("+strings.Join(needAdmin, ", ")+") solo puede leerse con administrador: sin él el estado queda como no informado", true)
	}
}

func ufwStatus(out string) string {
	for _, line := range strings.Split(out, "\n") {
		if k, v, ok := strings.Cut(strings.TrimSpace(line), ":"); ok && strings.EqualFold(k, "Status") {
			return strings.ToLower(strings.TrimSpace(v))
		}
	}
	return ""
}

var (
	nftInputRe = regexp.MustCompile(`hook\s+input`)
	nftDropRe  = regexp.MustCompile(`policy\s+drop|\bdrop\b|\breject\b`)
)

// nftState lê "nft list ruleset" (como root): vazio = sem firewall; uma chain de entrada com política ou regra de bloqueio =
// ligado; o resto = não dá para afirmar (nunca vira "ligado").
func nftState(out string) (Tri, string) {
	if strings.TrimSpace(out) == "" {
		return No, "ruleset vazio"
	}
	inInput := false
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "chain ") && strings.Contains(line, "{") {
			inInput = false
		}
		if nftInputRe.MatchString(line) {
			inInput = true
		}
		if inInput && nftDropRe.MatchString(line) {
			return Yes, "chain de entrada com bloqueio"
		}
	}
	return Unknown, "regras sem bloqueio de entrada identificado"
}

func linIPv6(d Deps, r *Report, skip skipFn) {
	var vals []int
	var names []string
	for _, i := range r.Interfaces {
		if !safeIface(i.Name) {
			continue
		}
		for _, a := range i.IPv6 {
			if isGlobalV6(a.IP) {
				names = append(names, i.Name)
				break
			}
		}
	}
	sort.Strings(names)
	for _, n := range names {
		b, err := d.readFile(path.Join("/proc/sys/net/ipv6/conf", n, "use_tempaddr"))
		if err != nil {
			continue
		}
		v, err := strconv.Atoi(strings.TrimSpace(string(b)))
		if err != nil {
			continue
		}
		vals = append(vals, v)
	}
	if len(vals) == 0 {
		if len(names) > 0 {
			skip("ipv6", "não deu para ler use_tempaddr do IPv6 em /proc", "could not read the IPv6 use_tempaddr from /proc", "no se pudo leer use_tempaddr de IPv6 en /proc", false)
		}
		return
	}
	r.IPv6Privacy = Yes
	for _, v := range vals {
		if v < 1 { // 0 = desligado; 1 = gera temporário mas prefere o estável; 2 = prefere o temporário (1 conta como ligado)
			r.IPv6Privacy = No
		}
	}
}

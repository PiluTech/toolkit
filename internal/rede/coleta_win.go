package rede

import (
	"context"
	"encoding/json"
	"math"
	"strconv"
	"strings"

	"github.com/PiluTech/toolkit/internal/diagfmt"
	"github.com/PiluTech/toolkit/internal/exec"
)

// Coletor do Windows: cada área é um script de PowerShell de LEITURA (só cmdlets Get-*, só ASCII, JSON compacto) rodado
// pelo powershell.exe de caminho absoluto, e uma função que lê o JSON com tolerância (campos nulos, números como texto,
// objeto único no lugar de lista). O único comando que não é PowerShell é `netsh.exe wlan show interfaces` (conexão
// ATUAL; nunca `show networks`, `show profiles` ou `key=clear`). Nada aqui eleva nem escreve.

const (
	winNetsh = `C:\Windows\System32\netsh.exe`
	// winPrefs define Q: roda uma consulta num try/catch com -ErrorAction Stop e devolve o STATUS EXPLÍCITO da subconsulta,
	// {ok:true,data:[...]} ou {ok:false,err:"<tipo do erro>"}. Sem isto o PowerShell engole o erro do cmdlet e sai com
	// código 0, e a falha viraria uma lista vazia "lida" (sem gateway, nenhuma porta). Só o TIPO do erro sai, nunca a mensagem.
	winPrefs = "$ProgressPreference='SilentlyContinue'\n" +
		`function Q($b){try{$ErrorActionPreference='Stop';@{ok=$true;data=@(& $b)}}catch{@{ok=$false;err=$_.Exception.GetType().Name}}}` + "\n" +
		"$r=@{}\n"
	winOut = `$r|ConvertTo-Json -Compress -Depth 6`

	winScriptIPs = winPrefs +
		`$r.ad=Q{Get-NetAdapter -ErrorAction Stop|ForEach-Object{@{Name=[string]$_.Name;Mac=[string]$_.MacAddress;Status=[string]$_.Status;Media=[string]$_.PhysicalMediaType}}}` + "\n" +
		`$r.ip=Q{Get-NetIPAddress -ErrorAction Stop|Where-Object{$_.AddressState -ne 'Tentative'}|ForEach-Object{@{Alias=[string]$_.InterfaceAlias;Addr=[string]$_.IPAddress;Fam=[string]$_.AddressFamily;Len=$_.PrefixLength;Sfx=[string]$_.SuffixOrigin}}}` + "\n" +
		`$r.gw=Q{Get-NetRoute -ErrorAction Stop|Where-Object{$_.DestinationPrefix -eq '0.0.0.0/0' -or $_.DestinationPrefix -eq '::/0'}|ForEach-Object{@{Alias=[string]$_.InterfaceAlias;Next=[string]$_.NextHop}}}` + "\n" +
		winOut

	winScriptProfile = winPrefs +
		`$r.p=Q{Get-NetConnectionProfile -ErrorAction Stop|ForEach-Object{@{Name=[string]$_.Name;Alias=[string]$_.InterfaceAlias;Cat=[string]$_.NetworkCategory}}}` + "\n" +
		winOut

	// doh: o cmdlet só existe no Windows 11/Server 2022+; sem ele {ok:true,na:true} (não é falha). Fb = AllowFallbackToUdp.
	winScriptDNS = winPrefs +
		`$r.srv=Q{Get-DnsClientServerAddress -ErrorAction Stop|ForEach-Object{@{Alias=[string]$_.InterfaceAlias;Srv=@($_.ServerAddresses|ForEach-Object{[string]$_})}}}` + "\n" +
		`if(Get-Command Get-DnsClientDohServerAddress -ErrorAction SilentlyContinue){$r.doh=Q{Get-DnsClientDohServerAddress -ErrorAction Stop|ForEach-Object{@{Addr=[string]$_.ServerAddress;Auto=[string]$_.AutoUpgrade;Fb=[string]$_.AllowFallbackToUdp}}}}else{$r.doh=@{ok=$true;na=$true;data=@()}}` + "\n" +
		winOut

	// o nome do processo é opcional (Process "" = não informado): se o Get-Process falhar, as portas seguem sem nome.
	winScriptPorts = winPrefs +
		`$p=@{};try{Get-Process -ErrorAction Stop|ForEach-Object{$p[[int]$_.Id]=[string]$_.ProcessName}}catch{}` + "\n" +
		`$r.t=Q{Get-NetTCPConnection -ErrorAction Stop|Where-Object{$_.State -eq 'Listen'}|ForEach-Object{@{A=[string]$_.LocalAddress;P=$_.LocalPort;I=$_.OwningProcess;N=[string]$p[[int]$_.OwningProcess]}}}` + "\n" +
		`$r.u=Q{Get-NetUDPEndpoint -ErrorAction Stop|ForEach-Object{@{A=[string]$_.LocalAddress;P=$_.LocalPort;I=$_.OwningProcess;N=[string]$p[[int]$_.OwningProcess]}}}` + "\n" +
		winOut

	winScriptFirewall = winPrefs +
		`$r.f=Q{Get-NetFirewallProfile -ErrorAction Stop|ForEach-Object{@{Name=[string]$_.Name;En=[string]$_.Enabled;In=[string]$_.DefaultInboundAction}}}` + "\n" +
		winOut

	winScriptIPv6 = winPrefs +
		`$r.t=Q{[string](Get-NetIPv6Protocol -ErrorAction Stop).UseTemporaryAddresses}` + "\n" +
		winOut
)

// winSub é o status de UMA subconsulta do script: ok só com "ok":true (booleano) E "data" presente. Qualquer outra forma
// (sem "ok", "ok" como texto, subconsulta ausente, sem "data") é FALHA, nunca leitura.
type winSub struct {
	ok   bool
	data any
	na   bool   // "na":true: o cmdlet não existe nesta versão do Windows (não é falha)
	err  string // tipo curto do erro, só [A-Za-z0-9._]
}

func winSubq(m map[string]any, key string) winSub {
	e := winMap(m[key])
	if e == nil {
		return winSub{err: "sem-status"}
	}
	if ok, isBool := e["ok"].(bool); isBool && ok {
		d, has := e["data"]
		if has {
			na, _ := e["na"].(bool)
			return winSub{ok: true, data: d, na: na}
		}
		return winSub{err: "sem-data"}
	}
	return winSub{err: winErrType(e["err"])}
}

// winErrType deixa só um tipo curto e seguro (letras, números, ponto e sublinhado) para ir ao relatório.
func winErrType(v any) string {
	s, _ := v.(string)
	var b strings.Builder
	for _, c := range s {
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '.' || c == '_' {
			b.WriteRune(c)
		}
		if b.Len() >= 60 {
			break
		}
	}
	if b.Len() == 0 {
		return "desconhecido"
	}
	return b.String()
}

// winFailure é uma subconsulta que falhou: vira um Skipped da área, nunca um achado.
type winFailure struct{ area, cmdlet, err string }

// collectWindows preenche r com a rede da máquina Windows. Cada área é independente (um pânico vira Skipped).
func collectWindows(ctx context.Context, d Deps, r *Report) {
	skip := func(area, pt, en, es string) {
		r.Skipped = append(r.Skipped, Skipped{Area: area, Reason: diagfmt.T(d.Lang, pt, en, es)})
	}
	wifiAdapter := false
	// area roda o script e entrega o objeto de subconsultas a read, que devolve as que falharam (viram Skipped da área).
	// Falha do script inteiro (erro do powershell, saída vazia ou que não é um objeto JSON) chama onFail.
	area := func(script string, read func(m map[string]any) []winFailure, onFail func()) {
		ok := false
		var fails []winFailure
		func() {
			defer func() {
				if recover() != nil {
					ok = false
				}
			}()
			out, err := d.run(ctx, psCmd(script))
			if err != nil || strings.TrimSpace(out) == "" {
				return
			}
			var v any
			if json.Unmarshal([]byte(strings.TrimSpace(strings.TrimPrefix(out, "\ufeff"))), &v) != nil {
				return
			}
			m := winMap(v)
			if m == nil {
				return
			}
			fails = read(m)
			ok = true
		}()
		if !ok {
			onFail()
			return
		}
		for _, f := range fails {
			skip(f.area, "não deu para ler esta parte ("+f.cmdlet+" falhou: "+f.err+"); nada foi concluído a partir dela", "could not read this part ("+f.cmdlet+" failed: "+f.err+"); nothing was concluded from it", "no se pudo leer esta parte ("+f.cmdlet+" falló: "+f.err+"); no se concluyó nada a partir de ella")
		}
	}
	area(winScriptIPs, func(m map[string]any) []winFailure {
		fails := winReadIPs(m, r)
		for _, i := range r.Interfaces {
			wifiAdapter = wifiAdapter || i.Kind == "wifi"
		}
		return fails
	}, func() {
		skip("interfaces", "não deu para ler as interfaces, os IPs e a rota padrão (Get-NetAdapter, Get-NetIPAddress, Get-NetRoute); rode em um PowerShell normal", "could not read the interfaces, IPs and default route (Get-NetAdapter, Get-NetIPAddress, Get-NetRoute); run from a regular PowerShell", "no se pudieron leer las interfaces, las IPs y la ruta predeterminada (Get-NetAdapter, Get-NetIPAddress, Get-NetRoute); ejecute en un PowerShell normal")
	})
	area(winScriptProfile, func(m map[string]any) []winFailure { return winReadProfiles(m, r) }, func() {
		skip("perfil", "não deu para ler o perfil de rede (Get-NetConnectionProfile)", "could not read the network profile (Get-NetConnectionProfile)", "no se pudo leer el perfil de red (Get-NetConnectionProfile)")
	})
	if wifiAdapter {
		winWiFi(ctx, d, r, skip)
	}
	area(winScriptDNS, func(m map[string]any) []winFailure { return winReadDNS(m, r) }, func() {
		skip("dns", "não deu para ler o DNS (Get-DnsClientServerAddress)", "could not read the DNS (Get-DnsClientServerAddress)", "no se pudo leer el DNS (Get-DnsClientServerAddress)")
	})
	area(winScriptPorts, func(m map[string]any) []winFailure { return winReadPorts(m, r) }, func() {
		skip("portas", "não deu para ler as portas em escuta (Get-NetTCPConnection, Get-NetUDPEndpoint)", "could not read the listening ports (Get-NetTCPConnection, Get-NetUDPEndpoint)", "no se pudieron leer los puertos en escucha (Get-NetTCPConnection, Get-NetUDPEndpoint)")
	})
	area(winScriptFirewall, func(m map[string]any) []winFailure { return winReadFirewall(m, r) }, func() {
		skip("firewall", "não deu para ler o firewall (Get-NetFirewallProfile)", "could not read the firewall (Get-NetFirewallProfile)", "no se pudo leer el firewall (Get-NetFirewallProfile)")
	})
	area(winScriptIPv6, func(m map[string]any) []winFailure {
		t := winSubq(m, "t")
		if !t.ok {
			return []winFailure{{"ipv6", "Get-NetIPv6Protocol", t.err}}
		}
		l := winStrings(t.data)
		if len(l) == 0 {
			return nil
		}
		switch strings.ToLower(l[0]) {
		case "enabled":
			r.IPv6Privacy = Yes
		case "disabled":
			r.IPv6Privacy = No
		}
		return nil
	}, func() {
		skip("ipv6", "não deu para ler os endereços temporários do IPv6 (Get-NetIPv6Protocol)", "could not read the IPv6 temporary addresses (Get-NetIPv6Protocol)", "no se pudieron leer las direcciones temporales de IPv6 (Get-NetIPv6Protocol)")
	})
	winMarkActive(r)
}

// winMarkActive marca os perfis de firewall em uso pelas conexões lidas (Public, Private, Domain).
func winMarkActive(r *Report) {
	used := map[string]string{"public": "Public", "private": "Private", "domain": "Domain"}
	act := map[string]bool{}
	for _, p := range r.Profiles {
		if n, ok := used[p.Category]; ok {
			act[n] = true
		}
	}
	for i := range r.Firewalls {
		r.Firewalls[i].Active = act[r.Firewalls[i].Name]
	}
}

// ---- JSON tolerante ----

func winMap(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}

// winList devolve os objetos de uma lista; o PowerShell entrega um objeto único no lugar da lista de um item.
func winList(v any) []map[string]any {
	switch x := v.(type) {
	case []any:
		var out []map[string]any
		for _, e := range x {
			if m, ok := e.(map[string]any); ok {
				out = append(out, m)
			}
		}
		return out
	case map[string]any:
		return []map[string]any{x}
	}
	return nil
}

func winStr(v any) string {
	switch x := v.(type) {
	case string:
		return strings.TrimSpace(x)
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(x)
	}
	return ""
}

func winInt(v any) (int, bool) {
	switch x := v.(type) {
	case float64:
		if math.IsNaN(x) || math.IsInf(x, 0) || x < 0 || x > math.MaxInt32 {
			return 0, false
		}
		return int(x), true
	case string:
		n, err := strconv.Atoi(strings.TrimSpace(x))
		return n, err == nil && n >= 0
	}
	return 0, false
}

// winStrings devolve os textos de uma lista (ou o texto único).
func winStrings(v any) []string {
	var out []string
	switch x := v.(type) {
	case []any:
		for _, e := range x {
			out = append(out, winStr(e))
		}
	case string:
		out = append(out, winStr(x))
	}
	return out
}

func winReadIPs(m map[string]any, r *Report) []winFailure {
	var fails []winFailure
	ad, ip, gw := winSubq(m, "ad"), winSubq(m, "ip"), winSubq(m, "gw")
	if !ad.ok {
		fails = append(fails, winFailure{"interfaces", "Get-NetAdapter", ad.err})
	}
	if !ip.ok {
		fails = append(fails, winFailure{"interfaces", "Get-NetIPAddress", ip.err})
	}
	if ad.ok && ip.ok { // interface "lida pela metade" não entra: sem IP lido ela pareceria sem endereço
		winBuildInterfaces(winList(ad.data), winList(ip.data), r)
	}
	if !gw.ok {
		return append(fails, winFailure{"rotas", "Get-NetRoute", gw.err})
	}
	r.GatewaysRead = true
	for _, g := range winList(gw.data) {
		next := winStr(g["Next"])
		if next == "" || next == "0.0.0.0" || next == "::" {
			next = "dev " + winStr(g["Alias"])
		}
		r.Gateways = append(r.Gateways, next)
	}
	return fails
}

func winBuildInterfaces(adapters, addrs []map[string]any, r *Report) {
	byName := map[string]*Interface{}
	var order []string
	get := func(name string) *Interface {
		if i := byName[name]; i != nil {
			return i
		}
		byName[name] = &Interface{Name: name}
		order = append(order, name)
		return byName[name]
	}
	for _, a := range adapters {
		name := winStr(a["Name"])
		if name == "" {
			continue
		}
		i := get(name)
		i.MAC = winStr(a["Mac"])
		switch winStr(a["Status"]) {
		case "Up":
			i.Up = Yes
		case "Disconnected", "Disabled", "Not Present":
			i.Up = No
		}
		switch media := winStr(a["Media"]); {
		case strings.Contains(media, "802.11"):
			i.Kind = "wifi"
		case strings.Contains(media, "802.3"):
			i.Kind = "ethernet"
		}
	}
	for _, a := range addrs {
		name, ip := winStr(a["Alias"]), winStr(a["Addr"])
		if name == "" || ip == "" || isLoopback(ip) {
			continue
		}
		pl, _ := winInt(a["Len"])
		ad := Addr{IP: strings.SplitN(ip, "%", 2)[0], PrefixLen: pl, Temporary: winStr(a["Sfx"]) == "Random"}
		i := get(name)
		if strings.Contains(winStr(a["Fam"]), "6") {
			i.IPv6 = append(i.IPv6, ad)
		} else {
			i.IPv4 = append(i.IPv4, ad)
		}
	}
	for _, n := range order {
		i := byName[n]
		if len(i.IPv4) == 0 && len(i.IPv6) == 0 && i.Up != Yes {
			continue
		}
		r.Interfaces = append(r.Interfaces, *i)
	}
}

func winReadProfiles(m map[string]any, r *Report) []winFailure {
	p := winSubq(m, "p")
	if !p.ok {
		return []winFailure{{"perfil", "Get-NetConnectionProfile", p.err}}
	}
	for _, e := range winList(p.data) {
		cat := ""
		switch winStr(e["Cat"]) {
		case "Public":
			cat = "public"
		case "Private":
			cat = "private"
		case "DomainAuthenticated":
			cat = "domain"
		}
		r.Profiles = append(r.Profiles, Profile{Name: winStr(e["Name"]), Interface: winStr(e["Alias"]), Category: cat})
	}
	return nil
}

// winTri lê "True"/"False" (ou booleano) como Tri; qualquer outra coisa (inclusive ausente) é Unknown.
func winTri(v any) Tri {
	switch strings.ToLower(winStr(v)) {
	case "true":
		return Yes
	case "false":
		return No
	}
	return Unknown
}

func winReadDNS(m map[string]any, r *Report) []winFailure {
	srv, doh := winSubq(m, "srv"), winSubq(m, "doh")
	if !srv.ok {
		return []winFailure{{"dns", "Get-DnsClientServerAddress", srv.err}}
	}
	var servers []DNSServer
	for _, s := range winList(srv.data) {
		alias := winStr(s["Alias"])
		if strings.HasPrefix(alias, "Loopback") { // o Windows põe um DNS site-local (fec0::) na interface de loopback
			continue
		}
		switch x := s["Srv"].(type) {
		case []any:
			for _, a := range x {
				if ad := winStr(a); ad != "" {
					servers = append(servers, DNSServer{Addr: ad, Interface: alias})
				}
			}
		case string:
			if x != "" {
				servers = append(servers, DNSServer{Addr: x, Interface: alias})
			}
		}
	}
	r.DNS.Servers = dedupeDNS(servers)
	r.DNS.Encrypted, r.DNS.Mode, r.DNS.AllowFallback = Unknown, DNSModeUnknown, Unknown // padrão: o SO não informou
	if !doh.ok {
		return []winFailure{{"dns", "Get-DnsClientDohServerAddress", doh.err}}
	}
	if doh.na { // Windows sem o cmdlet de DoH: nem "sim" nem "não"
		return nil
	}
	type dohEntry struct{ auto, fb Tri }
	entries := map[string]dohEntry{}
	for _, e := range winList(doh.data) {
		entries[winStr(e["Addr"])] = dohEntry{auto: winTri(e["Auto"]), fb: winTri(e["Fb"])}
	}
	r.DNS.Note = "Get-DnsClientDohServerAddress"
	seen, off, fbYes, fbUnknown := false, false, false, false
	for _, s := range r.DNS.Servers {
		if isLoopback(s.Addr) {
			continue
		}
		seen = true
		e := entries[s.Addr]
		switch {
		case e.auto != Yes: // sem modelo DoH, ou sem AutoUpgrade: este servidor é consultado em claro
			off = true
		case e.fb == Yes:
			fbYes = true
		case e.fb == Unknown:
			fbUnknown = true
		}
	}
	switch {
	case !seen:
	case off:
		r.DNS.Encrypted, r.DNS.Mode = No, DNSModeOff
	case fbYes:
		r.DNS.Mode, r.DNS.AllowFallback = DNSModeOpportunistic, Yes
	case fbUnknown:
		r.DNS.Mode, r.DNS.AllowFallback = DNSModeOpportunistic, Unknown
	default: // AutoUpgrade em todos E AllowFallbackToUdp=false em todos: a única combinação que prova DoH
		r.DNS.Encrypted, r.DNS.Mode, r.DNS.AllowFallback = Yes, DNSModeStrict, No
	}
	return nil
}

func winReadPorts(m map[string]any, r *Report) []winFailure {
	tcp, udp := winSubq(m, "t"), winSubq(m, "u")
	var fails []winFailure
	if !tcp.ok {
		fails = append(fails, winFailure{"portas", "Get-NetTCPConnection", tcp.err})
	}
	if !udp.ok {
		fails = append(fails, winFailure{"portas", "Get-NetUDPEndpoint", udp.err})
	}
	if len(fails) > 0 { // lista parcial não é "as portas": nenhuma é anotada
		return fails
	}
	add := func(proto string, list any) {
		for _, e := range winList(list) {
			port, ok := winInt(e["P"])
			if !ok {
				continue
			}
			pid, _ := winInt(e["I"])
			r.Listeners = append(r.Listeners, Listener{Proto: proto, Addr: winStr(e["A"]), Port: port, Process: winStr(e["N"]), PID: pid})
		}
	}
	add("tcp", tcp.data)
	add("udp", udp.data)
	r.ListenersRead = true
	return nil
}

func winReadFirewall(m map[string]any, r *Report) []winFailure {
	f := winSubq(m, "f")
	if !f.ok {
		return []winFailure{{"firewall", "Get-NetFirewallProfile", f.err}}
	}
	for _, p := range winList(f.data) {
		fw := Firewall{Name: winStr(p["Name"]), Detail: "inbound " + winStr(p["In"])}
		switch strings.ToLower(winStr(p["En"])) {
		case "true":
			fw.Enabled = Yes
		case "false":
			fw.Enabled = No
		}
		if fw.Name != "" {
			r.Firewalls = append(r.Firewalls, fw)
		}
	}
	return nil
}

// ---- Wi-Fi: netsh wlan show interfaces (texto localizado; classificado pelos VALORES, não pelos nomes dos campos) ----

func winWiFi(ctx context.Context, d Deps, r *Report, skip func(area, pt, en, es string)) {
	out, err := d.run(ctx, exec.Cmd{Name: winNetsh, Args: []string{"wlan", "show", "interfaces"}, ReadOnly: true})
	if err != nil {
		skip("wifi", "não deu para ler a conexão Wi-Fi (netsh wlan show interfaces): o serviço WLAN pode estar parado", "could not read the Wi-Fi connection (netsh wlan show interfaces): the WLAN service may be stopped", "no se pudo leer la conexión Wi-Fi (netsh wlan show interfaces): el servicio WLAN puede estar detenido")
		return
	}
	if w := parseNetshInterfaces(out); w != nil {
		r.WiFi = w
	}
}

// parseNetshInterfaces lê a saída do netsh e devolve a primeira interface COM SSID (conectada), ou nil. O texto é
// localizado (pt-BR, en-US, es) e vem na página de código do console: por isso só se confia nos valores e no prefixo ASCII
// dos nomes ("Auth*", "Autentic*", "Cipher", "Cifr*"). O que não for reconhecido vira SecUnknown com o texto original.
func parseNetshInterfaces(out string) *WiFi {
	type block struct{ name, ssid, auth, cipher string }
	var blocks []*block
	var cur *block
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimRight(line, "\r")
		k, v, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		key, val := strings.ToLower(strings.TrimSpace(k)), strings.TrimSpace(v)
		if key == "name" || key == "nome" || key == "nombre" {
			cur = &block{name: val}
			blocks = append(blocks, cur)
			continue
		}
		if cur == nil {
			cur = &block{}
			blocks = append(blocks, cur)
		}
		switch {
		case key == "ssid":
			cur.ssid = val
		case strings.HasPrefix(key, "auth") || strings.HasPrefix(key, "autentic"):
			cur.auth = val
		case strings.HasPrefix(key, "cipher") || strings.HasPrefix(key, "cifr") || strings.HasPrefix(key, "criptograf"):
			cur.cipher = val
		}
	}
	for _, b := range blocks {
		if b.ssid == "" {
			continue
		}
		raw := b.auth
		if b.cipher != "" {
			raw += " / " + b.cipher
		}
		return &WiFi{Interface: b.name, SSID: b.ssid, Security: netshSecurity(b.auth, b.cipher), Raw: raw}
	}
	return nil
}

// netshSecurity classifica pelos valores de autenticação e cifra (Open/Aberto/Abierto, WEP, WPA-Personal, WPA2-Personal,
// WPA3-SAE...). Cifra WEP numa autenticação aberta é WEP; WPA2 com TKIP é o WPA fraco.
func netshSecurity(auth, cipher string) Security {
	a, c := strings.ToUpper(auth), tokenSet(cipher)
	at := tokenSet(auth)
	enterprise := strings.Contains(a, "ENTERPRISE") || strings.Contains(a, "EMPRES") || strings.Contains(a, "CORPORAT")
	switch {
	case c["WEP"]:
		return SecWEP
	case (at["WPA"] || at["WPA2"] || at["WPA3"]) && enterprise:
		return SecEnterprise
	case at["WPA3"]:
		return SecWPA3
	case at["WPA2"]:
		if c["TKIP"] {
			return SecWPA
		}
		return SecWPA2
	case at["WPA"]:
		return SecWPA
	case at["WEP"] || at["SHARED"] || strings.Contains(a, "COMPARTILH") || strings.Contains(a, "COMPARTID"):
		return SecWEP
	case (at["OPEN"] && len(at) == 1) || strings.HasPrefix(a, "ABERT") || strings.HasPrefix(a, "ABIERT"):
		return SecOpen
	}
	return SecUnknown
}

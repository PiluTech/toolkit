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
	winPrefs = "$ErrorActionPreference='SilentlyContinue';$ProgressPreference='SilentlyContinue'\n"

	winScriptIPs = winPrefs +
		`$ad=@(Get-NetAdapter|ForEach-Object{@{Name=[string]$_.Name;Mac=[string]$_.MacAddress;Status=[string]$_.Status;Media=[string]$_.PhysicalMediaType}})` + "\n" +
		`$ip=@(Get-NetIPAddress|Where-Object{$_.AddressState -ne 'Tentative'}|ForEach-Object{@{Alias=[string]$_.InterfaceAlias;Addr=[string]$_.IPAddress;Fam=[string]$_.AddressFamily;Len=$_.PrefixLength;Sfx=[string]$_.SuffixOrigin}})` + "\n" +
		`$gw=@(Get-NetRoute -DestinationPrefix '0.0.0.0/0','::/0'|ForEach-Object{@{Alias=[string]$_.InterfaceAlias;Next=[string]$_.NextHop}})` + "\n" +
		`@{ad=$ad;ip=$ip;gw=$gw}|ConvertTo-Json -Compress -Depth 4`

	winScriptProfile = winPrefs +
		`$p=@(Get-NetConnectionProfile|ForEach-Object{@{Name=[string]$_.Name;Alias=[string]$_.InterfaceAlias;Cat=[string]$_.NetworkCategory}})` + "\n" +
		`ConvertTo-Json -Compress -Depth 4 -InputObject $p`

	winScriptDNS = winPrefs +
		`$s=@(Get-DnsClientServerAddress|ForEach-Object{@{Alias=[string]$_.InterfaceAlias;Srv=@($_.ServerAddresses|ForEach-Object{[string]$_})}})` + "\n" +
		`$doh=$null;if(Get-Command Get-DnsClientDohServerAddress){$doh=@(Get-DnsClientDohServerAddress|ForEach-Object{@{Addr=[string]$_.ServerAddress;Auto=[string]$_.AutoUpgrade}})}` + "\n" +
		`@{srv=$s;doh=$doh}|ConvertTo-Json -Compress -Depth 5`

	winScriptPorts = winPrefs +
		`$p=@{};Get-Process|ForEach-Object{$p[[int]$_.Id]=[string]$_.ProcessName}` + "\n" +
		`$t=@(Get-NetTCPConnection -State Listen|ForEach-Object{@{A=[string]$_.LocalAddress;P=$_.LocalPort;I=$_.OwningProcess;N=[string]$p[[int]$_.OwningProcess]}})` + "\n" +
		`$u=@(Get-NetUDPEndpoint|ForEach-Object{@{A=[string]$_.LocalAddress;P=$_.LocalPort;I=$_.OwningProcess;N=[string]$p[[int]$_.OwningProcess]}})` + "\n" +
		`@{t=$t;u=$u}|ConvertTo-Json -Compress -Depth 4`

	winScriptFirewall = winPrefs +
		`$f=@(Get-NetFirewallProfile|ForEach-Object{@{Name=[string]$_.Name;En=[string]$_.Enabled;In=[string]$_.DefaultInboundAction}})` + "\n" +
		`ConvertTo-Json -Compress -Depth 4 -InputObject $f`

	winScriptIPv6 = winPrefs +
		`@{t=[string](Get-NetIPv6Protocol).UseTemporaryAddresses}|ConvertTo-Json -Compress`
)

// collectWindows preenche r com a rede da máquina Windows. Cada área é independente (um pânico vira Skipped).
func collectWindows(ctx context.Context, d Deps, r *Report) {
	skip := func(area, pt, en, es string) {
		r.Skipped = append(r.Skipped, Skipped{Area: area, Reason: diagfmt.T(d.Lang, pt, en, es)})
	}
	wifiAdapter := false
	area := func(name, script string, read func(v any), onFail func()) {
		ok := false
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
			read(v)
			ok = true
		}()
		if !ok {
			onFail()
		}
		_ = name
	}
	area("interfaces", winScriptIPs, func(v any) {
		winReadIPs(v, r)
		for _, i := range r.Interfaces {
			wifiAdapter = wifiAdapter || i.Kind == "wifi"
		}
	}, func() {
		skip("interfaces", "não deu para ler as interfaces, os IPs e a rota padrão (Get-NetAdapter, Get-NetIPAddress, Get-NetRoute); rode em um PowerShell normal", "could not read the interfaces, IPs and default route (Get-NetAdapter, Get-NetIPAddress, Get-NetRoute); run from a regular PowerShell", "no se pudieron leer las interfaces, las IPs y la ruta predeterminada (Get-NetAdapter, Get-NetIPAddress, Get-NetRoute); ejecute en un PowerShell normal")
	})
	area("perfil", winScriptProfile, func(v any) { winReadProfiles(v, r) }, func() {
		skip("perfil", "não deu para ler o perfil de rede (Get-NetConnectionProfile)", "could not read the network profile (Get-NetConnectionProfile)", "no se pudo leer el perfil de red (Get-NetConnectionProfile)")
	})
	if wifiAdapter {
		winWiFi(ctx, d, r, skip)
	}
	area("dns", winScriptDNS, func(v any) { winReadDNS(v, r) }, func() {
		skip("dns", "não deu para ler o DNS (Get-DnsClientServerAddress)", "could not read the DNS (Get-DnsClientServerAddress)", "no se pudo leer el DNS (Get-DnsClientServerAddress)")
	})
	area("portas", winScriptPorts, func(v any) { winReadPorts(v, r) }, func() {
		skip("portas", "não deu para ler as portas em escuta (Get-NetTCPConnection, Get-NetUDPEndpoint)", "could not read the listening ports (Get-NetTCPConnection, Get-NetUDPEndpoint)", "no se pudieron leer los puertos en escucha (Get-NetTCPConnection, Get-NetUDPEndpoint)")
	})
	area("firewall", winScriptFirewall, func(v any) { winReadFirewall(v, r) }, func() {
		skip("firewall", "não deu para ler o firewall (Get-NetFirewallProfile)", "could not read the firewall (Get-NetFirewallProfile)", "no se pudo leer el firewall (Get-NetFirewallProfile)")
	})
	area("ipv6", winScriptIPv6, func(v any) {
		switch strings.ToLower(winStr(winMap(v)["t"])) {
		case "enabled":
			r.IPv6Privacy = Yes
		case "disabled":
			r.IPv6Privacy = No
		}
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

func winReadIPs(v any, r *Report) {
	m := winMap(v)
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
	for _, a := range winList(m["ad"]) {
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
	for _, a := range winList(m["ip"]) {
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
	r.GatewaysRead = true
	for _, g := range winList(m["gw"]) {
		next := winStr(g["Next"])
		if next == "" || next == "0.0.0.0" || next == "::" {
			next = "dev " + winStr(g["Alias"])
		}
		r.Gateways = append(r.Gateways, next)
	}
}

func winReadProfiles(v any, r *Report) {
	for _, p := range winList(v) {
		cat := ""
		switch winStr(p["Cat"]) {
		case "Public":
			cat = "public"
		case "Private":
			cat = "private"
		case "DomainAuthenticated":
			cat = "domain"
		}
		r.Profiles = append(r.Profiles, Profile{Name: winStr(p["Name"]), Interface: winStr(p["Alias"]), Category: cat})
	}
}

func winReadDNS(v any, r *Report) {
	m := winMap(v)
	var servers []DNSServer
	for _, s := range winList(m["srv"]) {
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
	r.DNS.Encrypted = Unknown // sem o cmdlet de DoH (Windows antigo) não dá para saber
	dohRaw, present := m["doh"]
	if !present || dohRaw == nil {
		return
	}
	auto := map[string]bool{}
	for _, e := range winList(dohRaw) {
		if strings.EqualFold(winStr(e["Auto"]), "true") {
			auto[winStr(e["Addr"])] = true
		}
	}
	r.DNS.Note = "Get-DnsClientDohServerAddress"
	enc, seen := Yes, false
	for _, s := range r.DNS.Servers {
		if isLoopback(s.Addr) {
			continue
		}
		seen = true
		if !auto[s.Addr] {
			enc = No
		}
	}
	if !seen {
		r.DNS.Encrypted = Unknown
		return
	}
	r.DNS.Encrypted = enc
}

func winReadPorts(v any, r *Report) {
	m := winMap(v)
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
	add("tcp", m["t"])
	add("udp", m["u"])
	r.ListenersRead = true
}

func winReadFirewall(v any, r *Report) {
	for _, p := range winList(v) {
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

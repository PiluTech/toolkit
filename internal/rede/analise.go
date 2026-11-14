package rede

import (
	"fmt"
	"net/netip"
	"sort"
	"strings"

	"github.com/PiluTech/toolkit/internal/diagfmt"
)

// sensitivePort é uma porta da lista fechada (revisada no PR; docs/rede.md). High marca o risco alto (protocolo sem
// criptografia, banco ou cache sem senha por padrão, acesso remoto de controle total); as demais são risco médio.
type sensitivePort struct {
	Port int
	Name string
	High bool
}

// sensitivePorts é a lista fechada de portas sensíveis. Só TCP. Só vira achado quando escuta em endereço NÃO local.
var sensitivePorts = []sensitivePort{
	{21, "FTP", true}, {22, "SSH", false}, {23, "Telnet", true}, {135, "RPC", false}, {139, "NetBIOS", false},
	{445, "SMB", false}, {1433, "SQL Server", false}, {2375, "Docker sem TLS", true}, {3306, "MySQL", false},
	{3389, "RDP", false}, {5432, "PostgreSQL", false}, {5900, "VNC", true}, {5901, "VNC", true},
	{5985, "WinRM", false}, {5986, "WinRM", false}, {6379, "Redis", true}, {9200, "Elasticsearch", true},
	{11211, "Memcached", true}, {27017, "MongoDB", true},
}

func sensitiveByPort(p int) (sensitivePort, bool) {
	for _, s := range sensitivePorts {
		if s.Port == p {
			return s, true
		}
	}
	return sensitivePort{}, false
}

// parseIP lê um endereço como o sistema o escreve: tira colchetes e o escopo ("%lo"); "::ffff:a.b.c.d" vira IPv4.
func parseIP(s string) (netip.Addr, bool) {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(strings.TrimSuffix(s, "]"), "[")
	if i := strings.IndexByte(s, '%'); i >= 0 {
		s = s[:i]
	}
	a, err := netip.ParseAddr(s)
	if err != nil {
		return netip.Addr{}, false
	}
	return a.Unmap(), true
}

// isLoopback diz que o endereço é DESTA máquina só (127.0.0.0/8 e ::1). Tudo o mais (0.0.0.0, "::", "*", o IP da LAN,
// link-local, vazio ou ilegível) é NÃO local: a dúvida nunca esconde uma porta.
func isLoopback(addr string) bool {
	a, ok := parseIP(addr)
	return ok && a.IsLoopback()
}

var (
	cgnat     = netip.MustParsePrefix("100.64.0.0/10")
	siteLocal = netip.MustParsePrefix("fec0::/10") // obsoleto, mas o Windows o põe como DNS da interface de loopback
)

// isPublicIP diz que o endereço é público (não loopback, privado, link-local, de uso compartilhado CGNAT 100.64.0.0/10,
// que é o da Tailscale, nem não especificado).
func isPublicIP(addr string) bool {
	a, ok := parseIP(addr)
	if !ok {
		return false
	}
	return !a.IsLoopback() && !a.IsPrivate() && !a.IsLinkLocalUnicast() && !a.IsUnspecified() && !a.IsMulticast() && !cgnat.Contains(a) && !siteLocal.Contains(a)
}

// isGlobalV6 diz que é um IPv6 global (2000::/3): estável, e rastreável, se não for temporário.
func isGlobalV6(addr string) bool {
	a, ok := parseIP(addr)
	return ok && a.Is6() && netip.MustParsePrefix("2000::/3").Contains(a)
}

func hasGlobalV6(r *Report) bool {
	for _, i := range r.Interfaces {
		for _, a := range i.IPv6 {
			if isGlobalV6(a.IP) {
				return true
			}
		}
	}
	return false
}

// fwState resume o firewall para decidir a severidade de uma porta: Windows = os perfis EM USO (qualquer um desligado =
// desligado); Unix = qualquer entrada ligada = ligado, todas desligadas = desligado, o resto = desconhecido.
func fwState(r *Report) Tri {
	if len(r.Firewalls) == 0 {
		return Unknown
	}
	if r.OS == "windows" {
		fws := activeFirewalls(r)
		yes := 0
		for _, f := range fws {
			switch f.Enabled {
			case No:
				return No
			case Yes:
				yes++
			}
		}
		if yes == len(fws) {
			return Yes
		}
		return Unknown
	}
	yes, no := 0, 0
	for _, f := range r.Firewalls {
		switch f.Enabled {
		case Yes:
			yes++
		case No:
			no++
		}
	}
	switch {
	case yes > 0:
		return Yes
	case no == len(r.Firewalls):
		return No
	}
	return Unknown
}

// activeFirewalls são os perfis em uso; se nenhum perfil está marcado como em uso (a leitura dos perfis falhou), todos.
func activeFirewalls(r *Report) []Firewall {
	var act []Firewall
	for _, f := range r.Firewalls {
		if f.Active {
			act = append(act, f)
		}
	}
	if len(act) == 0 {
		return r.Firewalls
	}
	return act
}

// Analyze transforma a leitura em achados, do mais grave ao mais leve. Nunca afirma "você foi invadido": só "isto está
// exposto". "Sem dado" nunca vira "ok": o que não foi lido aparece como informativo ou em Report.Skipped.
func Analyze(r *Report, l Lang) []Finding {
	var f []Finding
	t := func(pt, en, es string) string { return diagfmt.T(l, pt, en, es) }
	add := func(id, area string, sev Severity, title, detail, action string) {
		f = append(f, Finding{ID: id, Area: area, Severity: sev, Title: title, Detail: detail, Action: action})
	}

	analyseWiFi(r, l, add)
	analyseFirewall(r, l, add)
	analysePorts(r, l, add)
	analyseProfiles(r, l, add)
	analyseDNS(r, l, add)

	if r.IPv6Privacy == No && hasGlobalV6(r) {
		add("ipv6-sem-privacidade", "ipv6", SevLow,
			t("IPv6 sem endereços temporários (privacy extensions)", "IPv6 without temporary addresses (privacy extensions)", "IPv6 sin direcciones temporales (privacy extensions)"),
			t("o endereço IPv6 global é estável e permite rastrear esta máquina entre redes", "the global IPv6 address is stable and lets this machine be tracked across networks", "la dirección IPv6 global es estable y permite rastrear este equipo entre redes"),
			t("Ative os endereços temporários do IPv6 nas configurações do sistema (instrução manual; o toolkit não muda a rede).", "Turn on IPv6 temporary addresses in the system settings (manual step; the toolkit does not change the network).", "Active las direcciones temporales de IPv6 en la configuración del sistema (paso manual; el toolkit no cambia la red)."))
	}
	if r.GatewaysRead && len(r.Gateways) == 0 {
		add("sem-gateway", "rede", SevMedium,
			t("Sem rota padrão (gateway ausente)", "No default route (gateway missing)", "Sin ruta predeterminada (falta la puerta de enlace)"),
			t("a tabela de rotas não tem saída para outras redes: esta máquina está sem conexão", "the routing table has no way out to other networks: this machine is offline", "la tabla de rutas no tiene salida a otras redes: este equipo está sin conexión"),
			t("Confira o cabo, o Wi-Fi e o roteador.", "Check the cable, the Wi-Fi and the router.", "Revise el cable, el Wi-Fi y el router."))
	}
	add("ip-publico", "ip-publico", SevInfo,
		t("IP público: não consultado (sob pedido, PR seguinte)", "Public IP: not queried (on request, next PR)", "IP público: no consultado (bajo pedido, PR siguiente)"),
		t("descobrir o IP público exige uma requisição a um serviço externo, que fica para um pedido explícito", "finding the public IP needs a request to an external service, which is left for an explicit request", "descubrir la IP pública requiere una petición a un servicio externo, que queda para un pedido explícito"),
		t("Para saber o IP público agora, veja o painel do roteador.", "To learn the public IP now, check the router's admin page.", "Para saber la IP pública ahora, vea el panel del router."))

	if !hasData(r) {
		add("nada-lido", "rede", SevInfo,
			t("Nada da rede pôde ser lido", "Nothing about the network could be read", "No se pudo leer nada de la red"),
			t("nenhuma das leituras devolveu dado (veja os itens não lidos)", "none of the reads returned data (see the unread items)", "ninguna de las lecturas devolvió datos (vea los elementos no leídos)"),
			t("Confira os itens não lidos abaixo e rode de novo.", "Check the unread items below and run again.", "Revise los elementos no leídos y ejecute de nuevo."))
	}
	sort.SliceStable(f, func(i, j int) bool { return f[i].Severity > f[j].Severity })
	return f
}

// hasData diz que pelo menos uma área foi lida.
func hasData(r *Report) bool {
	return len(r.Interfaces) > 0 || r.GatewaysRead || r.WiFi != nil || len(r.DNS.Servers) > 0 || r.ListenersRead || len(r.Firewalls) > 0 || len(r.Profiles) > 0
}

type addFn func(id, area string, sev Severity, title, detail, action string)

func analyseWiFi(r *Report, l Lang, add addFn) {
	w := r.WiFi
	if w == nil {
		return
	}
	t := func(pt, en, es string) string { return diagfmt.T(l, pt, en, es) }
	name := w.SSID
	if name != "" {
		name = " (" + name + ")"
	}
	switch w.Security {
	case SecOpen:
		add("wifi-aberto", "wifi", SevHigh, t("Rede Wi-Fi aberta (sem senha)", "Open Wi-Fi network (no password)", "Red Wi-Fi abierta (sin contraseña)")+name,
			t("qualquer pessoa por perto lê o que não estiver criptografado", "anyone nearby can read what is not encrypted", "cualquiera cerca puede leer lo que no esté cifrado"),
			t("Evite dados sensíveis, use uma VPN ou prefira outra rede.", "Avoid sensitive data, use a VPN or pick another network.", "Evite datos sensibles, use una VPN o elija otra red."))
	case SecWEP:
		add("wifi-wep", "wifi", SevHigh, t("Wi-Fi com WEP", "Wi-Fi using WEP", "Wi-Fi con WEP")+name,
			t("o WEP é quebrado em minutos", "WEP can be broken in minutes", "el WEP se rompe en minutos"),
			t("Troque o roteador para WPA2 ou WPA3.", "Switch the router to WPA2 or WPA3.", "Cambie el router a WPA2 o WPA3."))
	case SecWPA:
		add("wifi-wpa", "wifi", SevMedium, t("Wi-Fi com WPA (TKIP) ou modo misto WPA/WPA2", "Wi-Fi using WPA (TKIP) or mixed WPA/WPA2", "Wi-Fi con WPA (TKIP) o modo mixto WPA/WPA2")+name,
			t("o TKIP é fraco; o modo misto aceita o protocolo mais fraco", "TKIP is weak; mixed mode accepts the weaker protocol", "el TKIP es débil; el modo mixto acepta el protocolo más débil"),
			t("Prefira WPA2-AES ou WPA3 no roteador.", "Prefer WPA2-AES or WPA3 on the router.", "Prefiera WPA2-AES o WPA3 en el router."))
	case SecWPA2:
		add("wifi-wpa2", "wifi", SevInfo, t("Wi-Fi com WPA2-AES", "Wi-Fi using WPA2-AES", "Wi-Fi con WPA2-AES")+name,
			t("adequado", "adequate", "adecuado"),
			t("WPA3 é melhor, se o roteador e as placas aceitarem.", "WPA3 is better, if the router and the adapters support it.", "WPA3 es mejor, si el router y las tarjetas lo admiten."))
	case SecWPA3:
		add("wifi-wpa3", "wifi", SevOK, t("Wi-Fi com WPA3", "Wi-Fi using WPA3", "Wi-Fi con WPA3")+name, "", "")
	case SecEnterprise:
		add("wifi-corporativo", "wifi", SevInfo, t("Wi-Fi corporativo (802.1X)", "Enterprise Wi-Fi (802.1X)", "Wi-Fi corporativo (802.1X)")+name,
			t("a autenticação é por usuário; o toolkit não avalia a configuração do servidor", "authentication is per user; the toolkit does not assess the server setup", "la autenticación es por usuario; el toolkit no evalúa la configuración del servidor"), "")
	default:
		detail := t("o sistema não informou o tipo de segurança de forma reconhecível", "the system did not report the security type in a recognisable way", "el sistema no informó el tipo de seguridad de forma reconocible")
		if w.Raw != "" {
			detail += ": " + w.Raw
		}
		add("wifi-desconhecido", "wifi", SevInfo, t("Segurança do Wi-Fi não verificada", "Wi-Fi security not verified", "Seguridad del Wi-Fi no verificada")+name, detail,
			t("Veja o tipo de segurança nas configurações de Wi-Fi do sistema.", "Check the security type in the system's Wi-Fi settings.", "Vea el tipo de seguridad en la configuración de Wi-Fi del sistema."))
	}
}

func analyseFirewall(r *Report, l Lang, add addFn) {
	t := func(pt, en, es string) string { return diagfmt.T(l, pt, en, es) }
	act := t("Ligue o firewall nas configurações de segurança do sistema (o toolkit não o liga).", "Turn the firewall on in the system's security settings (the toolkit does not turn it on).", "Active el firewall en la configuración de seguridad del sistema (el toolkit no lo activa).")
	unknownTitle := t("Firewall: não foi possível saber se está ligado", "Firewall: could not tell whether it is on", "Firewall: no se pudo saber si está activado")
	if len(r.Firewalls) == 0 {
		add("firewall-indeterminado", "firewall", SevInfo, unknownTitle,
			t("nenhum firewall foi lido", "no firewall was read", "no se leyó ningún firewall"),
			t("Rode como administrador para ler o firewall, ou confira nas configurações do sistema.", "Run as administrator to read the firewall, or check the system settings.", "Ejecute como administrador para leer el firewall, o revise la configuración del sistema."))
		return
	}
	if r.OS == "windows" {
		hadAct := false
		for _, f := range r.Firewalls {
			hadAct = hadAct || f.Active
		}
		yesAct, unknown := 0, false
		for _, f := range r.Firewalls {
			active := f.Active || !hadAct
			switch {
			case f.Enabled == No && active:
				add("firewall-desligado-"+strings.ToLower(f.Name), "firewall", SevHigh,
					t("Firewall desligado no perfil ", "Firewall off on the profile ", "Firewall desactivado en el perfil ")+f.Name,
					t("o perfil está em uso e o Windows não filtra a entrada", "the profile is in use and Windows is not filtering inbound traffic", "el perfil está en uso y Windows no filtra la entrada"), act)
			case f.Enabled == No:
				add("firewall-desligado-"+strings.ToLower(f.Name), "firewall", SevLow,
					t("Firewall desligado no perfil ", "Firewall off on the profile ", "Firewall desactivado en el perfil ")+f.Name,
					t("o perfil não está em uso agora, mas valerá quando a máquina entrar nessa rede", "the profile is not in use now, but will apply when the machine joins that network", "el perfil no está en uso ahora, pero valdrá cuando el equipo entre en esa red"), act)
			case f.Enabled == Unknown && active:
				unknown = true
			case f.Enabled == Yes && active:
				yesAct++
			}
		}
		if unknown {
			add("firewall-indeterminado", "firewall", SevInfo, unknownTitle, "", "")
		}
		if yesAct > 0 && !unknown && fwState(r) == Yes {
			add("firewall-ligado", "firewall", SevOK, t("Firewall ligado nos perfis em uso", "Firewall on for the profiles in use", "Firewall activado en los perfiles en uso"), "", "")
		}
		return
	}
	switch fwState(r) {
	case Yes:
		add("firewall-ligado", "firewall", SevOK, t("Firewall ligado", "Firewall on", "Firewall activado"), "", "")
	case No:
		add("firewall-desligado", "firewall", SevHigh, t("Firewall desligado", "Firewall off", "Firewall desactivado"),
			t("nenhum firewall lido está ativo", "no firewall that was read is active", "ningún firewall leído está activo"), act)
	default:
		add("firewall-indeterminado", "firewall", SevInfo, unknownTitle,
			t("pelo menos um firewall não pôde ser lido (muitos pedem administrador)", "at least one firewall could not be read (many need administrator rights)", "al menos un firewall no pudo leerse (muchos piden administrador)"),
			t("Rode como administrador (sudo) para ler o firewall, ou confira nas configurações do sistema.", "Run as administrator (sudo) to read the firewall, or check the system settings.", "Ejecute como administrador (sudo) para leer el firewall, o revise la configuración del sistema."))
	}
}

func analysePorts(r *Report, l Lang, add addFn) {
	t := func(pt, en, es string) string { return diagfmt.T(l, pt, en, es) }
	fw := fwState(r)
	type group struct {
		sp    sensitivePort
		items []string
	}
	groups := map[int]*group{}
	var order []int
	nonSensTCP, nonSensUDP := 0, 0
	for _, li := range r.Listeners {
		if isLoopback(li.Addr) {
			continue
		}
		sp, sens := sensitiveByPort(li.Port)
		if li.Proto == "tcp" && sens {
			g := groups[li.Port]
			if g == nil {
				g = &group{sp: sp}
				groups[li.Port] = g
				order = append(order, li.Port)
			}
			if txt := listenerText(li, l); !contains(g.items, txt) { // o mesmo socket em IPv4 e IPv6 sai uma vez só
				g.items = append(g.items, txt)
			}
			continue
		}
		if li.Proto == "udp" {
			nonSensUDP++
		} else {
			nonSensTCP++
		}
	}
	sort.Ints(order)
	for _, p := range order {
		g := groups[p]
		sev := SevMedium
		if g.sp.High {
			sev = SevHigh
		}
		if fw == Yes { // com o firewall ligado, um nível a menos; desligado OU sem dado nunca rebaixa
			sev--
		}
		fwText := map[Tri]string{
			Yes:     t("o firewall está ligado", "the firewall is on", "el firewall está activado"),
			No:      t("o firewall está desligado", "the firewall is off", "el firewall está desactivado"),
			Unknown: t("não foi possível saber se o firewall está ligado", "could not tell whether the firewall is on", "no se pudo saber si el firewall está activado"),
		}[fw]
		add("porta-"+itoa(p), "portas", sev,
			fmt.Sprintf("%s %d (%s) %s", t("Porta", "Port", "Puerto"), p, g.sp.Name, t("aceita conexões de fora desta máquina", "accepts connections from outside this machine", "acepta conexiones desde fuera de este equipo")),
			strings.Join(g.items, "; ")+" — "+fwText+"; "+t("se a internet alcança isto depende do roteador, que o toolkit não vê", "whether the internet reaches it depends on the router, which the toolkit cannot see", "si internet lo alcanza depende del router, que el toolkit no ve"),
			t("Feche o serviço ou restrinja-o ao 127.0.0.1 (o toolkit não fecha portas).", "Close the service or restrict it to 127.0.0.1 (the toolkit does not close ports).", "Cierre el servicio o restrínjalo a 127.0.0.1 (el toolkit no cierra puertos)."))
	}
	if nonSensTCP+nonSensUDP > 0 {
		add("portas-expostas", "portas", SevInfo,
			t("Portas em escuta em todos os endereços ou na rede local", "Ports listening on all addresses or on the local network", "Puertos en escucha en todas las direcciones o en la red local"),
			fmt.Sprintf("%d TCP, %d UDP — %s", nonSensTCP, nonSensUDP, t("é o normal de muitos programas (compartilhamento, descoberta, sincronização); a lista completa está no relatório completo", "this is normal for many programs (sharing, discovery, sync); the full list is in the full report", "es lo normal de muchos programas (compartir, descubrimiento, sincronización); la lista completa está en el informe completo")),
			"")
	}
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func listenerText(li Listener, l Lang) string {
	s := li.Addr
	if s == "" {
		s = "?"
	}
	s += ":" + itoa(li.Port)
	if li.Process != "" {
		s += " (" + li.Process + ")"
	} else {
		s += " (" + diagfmt.T(l, "processo não informado", "process not reported", "proceso no informado") + ")"
	}
	return s
}

func analyseProfiles(r *Report, l Lang, add addFn) {
	t := func(pt, en, es string) string { return diagfmt.T(l, pt, en, es) }
	for _, p := range r.Profiles {
		switch p.Category {
		case "public":
			if smb := sharingPorts(r); smb != "" {
				add("perfil-publico-compartilhando", "perfil", SevMedium,
					t("Perfil de rede Público com compartilhamento de arquivos escutando", "Public network profile with file sharing listening", "Perfil de red Público con uso compartido de archivos escuchando"),
					t("a rede ", "the network ", "la red ")+p.Name+": "+smb,
					t("Desligue o compartilhamento que você não usa nas configurações do Windows.", "Turn off the sharing you do not use in the Windows settings.", "Desactive el uso compartido que no use en la configuración de Windows."))
			}
		case "private":
			if r.WiFi != nil && r.WiFi.Security == SecOpen && (p.Interface == "" || r.WiFi.Interface == "" || p.Interface == r.WiFi.Interface) {
				add("perfil-privado-rede-aberta", "perfil", SevMedium,
					t("Perfil de rede Privado numa rede Wi-Fi aberta", "Private network profile on an open Wi-Fi network", "Perfil de red Privado en una red Wi-Fi abierta"),
					t("o perfil Privado confia nos outros aparelhos da rede, e esta rede não tem senha", "the Private profile trusts the other devices on the network, and this network has no password", "el perfil Privado confía en los demás dispositivos de la red, y esta red no tiene contraseña"),
					t("Mude o perfil da rede para Público nas configurações do Windows.", "Change the network profile to Public in the Windows settings.", "Cambie el perfil de la red a Público en la configuración de Windows."))
			}
		}
	}
}

// sharingPorts diz, em texto, quais portas de compartilhamento de arquivos (SMB 445 e NetBIOS 139) escutam em endereço não local.
func sharingPorts(r *Report) string {
	var ps []string
	seen := map[int]bool{}
	for _, li := range r.Listeners {
		if li.Proto == "tcp" && (li.Port == 445 || li.Port == 139) && !isLoopback(li.Addr) && !seen[li.Port] {
			seen[li.Port] = true
			ps = append(ps, itoa(li.Port))
		}
	}
	sort.Strings(ps)
	if len(ps) == 0 {
		return ""
	}
	return "TCP " + strings.Join(ps, ", ")
}

func analyseDNS(r *Report, l Lang, add addFn) {
	t := func(pt, en, es string) string { return diagfmt.T(l, pt, en, es) }
	var pub []string
	for _, s := range r.DNS.Servers {
		if isPublicIP(s.Addr) {
			pub = append(pub, s.Addr)
		}
	}
	if len(pub) == 0 {
		return
	}
	list := strings.Join(pub, ", ")
	switch r.DNS.Encrypted {
	case No:
		add("dns-publico", "dns", SevLow,
			t("DNS em endereço público, sem DoH/DoT", "DNS on a public address, without DoH/DoT", "DNS en una dirección pública, sin DoH/DoT"),
			list+" — "+t("as consultas de nomes viajam sem criptografia", "name lookups travel unencrypted", "las consultas de nombres viajan sin cifrar"),
			t("Sem ação obrigatória; considere um DNS com criptografia (DoH ou DoT).", "No action required; consider an encrypted DNS (DoH or DoT).", "Sin acción obligatoria; considere un DNS cifrado (DoH o DoT)."))
	case Unknown:
		detail := t("o sistema não informou se há criptografia", "the system did not report whether it is encrypted", "el sistema no informó si hay cifrado")
		title := t("DNS em endereço público; DoH/DoT não verificado", "DNS on a public address; DoH/DoT not verified", "DNS en una dirección pública; DoH/DoT no verificado")
		if r.DNS.Mode == DNSModeOpportunistic {
			title = t("DNS em endereço público; DoH/DoT oportunista (pode usar DNS em claro)", "DNS on a public address; opportunistic DoH/DoT (may use plain DNS)", "DNS en una dirección pública; DoH/DoT oportunista (puede usar DNS en claro)")
			detail = DNSModeText(r.DNS, l) + "; " + t("a criptografia efetiva não foi verificada (o toolkit não faz consultas para testar)", "the effective encryption was not verified (the toolkit does not send queries to test it)", "el cifrado efectivo no se verificó (el toolkit no envía consultas para probarlo)")
		}
		add("dns-publico-nao-verificado", "dns", SevInfo, title, list+" — "+detail, "")
	}
}

// DNSModeText descreve o modo configurado do DNS e o que isso prova (ou não) sobre a criptografia efetiva. Vazio quando o
// modo não é conhecido ou não traz nada além do campo Encrypted.
func DNSModeText(d DNS, l Lang) string {
	t := func(pt, en, es string) string { return diagfmt.T(l, pt, en, es) }
	switch d.Mode {
	case DNSModeOpportunistic:
		if d.AllowFallback == Unknown {
			return t("configurado para tentar DoH/DoT; pode cair para DNS em claro (fallback não lido)", "configured to try DoH/DoT; may fall back to plain DNS (fallback not read)", "configurado para intentar DoH/DoT; puede caer a DNS en claro (fallback no leído)")
		}
		return t("configurado para tentar DoH/DoT, mas admite cair para DNS em claro (oportunista)", "configured to try DoH/DoT, but allows falling back to plain DNS (opportunistic)", "configurado para intentar DoH/DoT, pero admite caer a DNS en claro (oportunista)")
	case DNSModeStrict:
		return t("estrito: sem cair para DNS em claro", "strict: no fallback to plain DNS", "estricto: sin caer a DNS en claro")
	}
	return ""
}

// ---- resumo ----

// Símbolos do resumo (sempre com a palavra da gravidade ao lado: nada depende só de cor ou forma).
const (
	symOK   = "✓"
	symInfo = "-"
	symLow  = "-"
	symMed  = "!"
	symHigh = "✗"
)

// SevWord dá o símbolo e a palavra da gravidade no idioma l.
func SevWord(s Severity, l Lang) (sym, word string) {
	switch s {
	case SevHigh:
		return symHigh, diagfmt.T(l, "alta", "high", "alta")
	case SevMedium:
		return symMed, diagfmt.T(l, "média", "medium", "media")
	case SevLow:
		return symLow, diagfmt.T(l, "baixa", "low", "baja")
	case SevInfo:
		return symInfo, diagfmt.T(l, "info", "info", "info")
	}
	return symOK, "ok"
}

// Summary é o resumo "o que está exposto e o que fazer": um achado por bloco, do mais grave ao mais leve. O que NÃO foi
// lido é contado e listado (um relatório sem dado nunca diz "nada de ruim").
func Summary(r *Report, fs []Finding, l Lang) string {
	t := func(pt, en, es string) string { return diagfmt.T(l, pt, en, es) }
	var b strings.Builder
	fmt.Fprintf(&b, "%s (%s)\n\n", t("Análise de rede", "Network analysis", "Análisis de red"), osName(r.OS))
	var high, med, low int
	noData := false
	for _, f := range fs {
		switch f.Severity {
		case SevHigh:
			high++
		case SevMedium:
			med++
		case SevLow:
			low++
		}
		noData = noData || f.ID == "nada-lido"
	}
	res := t("Resultado", "Result", "Resultado")
	switch {
	case noData:
		fmt.Fprintf(&b, "%s: %s.\n\n", res, t("nada pôde ser lido", "nothing could be read", "no se pudo leer nada"))
	case high+med+low > 0:
		fmt.Fprintf(&b, "%s: %d %s, %d %s, %d %s.\n\n", res, high, t("alta(s)", "high", "alta(s)"), med, t("média(s)", "medium", "media(s)"), low, t("baixa(s)", "low", "baja(s)"))
	case len(r.Skipped) > 0:
		fmt.Fprintf(&b, "%s: %s.\n\n", res, t("nada de ruim nos itens lidos (há itens que não puderam ser lidos, veja abaixo)", "nothing bad in the items that were read (some items could not be read, see below)", "nada malo en los elementos leídos (hay elementos que no se pudieron leer, vea abajo)"))
	default:
		fmt.Fprintf(&b, "%s: %s.\n\n", res, t("nada de ruim encontrado", "nothing bad found", "no se encontró nada malo"))
	}
	for _, f := range fs {
		sym, word := SevWord(f.Severity, l)
		fmt.Fprintf(&b, "%s %s: %s", sym, word, f.Title)
		if f.Detail != "" {
			fmt.Fprintf(&b, " — %s", f.Detail)
		}
		b.WriteByte('\n')
		if f.Action != "" {
			fmt.Fprintf(&b, "    %s %s\n", t("O que fazer:", "What to do:", "Qué hacer:"), f.Action)
		}
	}
	if n := len(r.Skipped); n > 0 {
		var areas []string
		for _, s := range r.Skipped {
			areas = append(areas, s.Area)
		}
		fmt.Fprintf(&b, "\n%s: %d (%s) — %s\n", t("Não lidos", "Not read", "No leídos"), n, strings.Join(areas, ", "), t("veja o relatório completo", "see the full report", "vea el informe completo"))
	}
	if gain := adminGain(r, l); gain != "" {
		fmt.Fprintf(&b, "\n%s\n", gain)
	}
	return strings.TrimRight(b.String(), "\n") + "\n"
}

// adminGain diz, quando a coleta NÃO rodou como administrador/root, o que a análise leria a mais com privilégio.
func adminGain(r *Report, l Lang) string {
	if r.Admin {
		return ""
	}
	var areas []string
	for _, s := range r.Skipped {
		if s.NeedsAdmin {
			areas = append(areas, s.Area)
		}
	}
	if len(areas) == 0 {
		return ""
	}
	how := diagfmt.T(l, "abra o terminal como administrador e rode de novo", "open the terminal as administrator and run it again", "abra el terminal como administrador y ejecútelo de nuevo")
	if r.OS != "windows" {
		how = diagfmt.T(l, "rode com sudo (sudo toolkit rede)", "run with sudo (sudo toolkit rede)", "ejecute con sudo (sudo toolkit rede)")
	}
	return diagfmt.T(l, "Com administrador a análise também lê: ", "With administrator rights the analysis also reads: ", "Con administrador el análisis también lee: ") + strings.Join(areas, ", ") + " (" + how + ")."
}

func osName(os string) string {
	switch os {
	case "windows":
		return "Windows"
	case "darwin":
		return "macOS"
	case "linux":
		return "Linux"
	}
	return os
}

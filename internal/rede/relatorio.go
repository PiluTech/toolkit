package rede

import (
	"fmt"
	"strings"

	"github.com/PiluTech/toolkit/internal/diagfmt"
)

// Full é o relatório completo: todos os dados lidos, por área, como o sistema os informou (nada é mascarado). Quem
// grava ou mostra o texto passa por textsafe.Multiline.
func Full(r *Report, l Lang) string {
	t := func(pt, en, es string) string { return diagfmt.T(l, pt, en, es) }
	var b strings.Builder
	h := func(title string) { fmt.Fprintf(&b, "\n== %s ==\n", title) }
	fmt.Fprintf(&b, "%s — %s\n", t("Relatório completo da análise de rede", "Full network analysis report", "Informe completo del análisis de red"), osName(r.OS))
	if !r.Generated.IsZero() {
		fmt.Fprintf(&b, "%s\n", r.Generated.Format("2006-01-02 15:04"))
	}
	fmt.Fprintf(&b, "%s\n", t("Contém dados da rede local (IPs, MACs, nomes de rede): nada foi enviado a ninguém.", "Contains local network data (IPs, MACs, network names): nothing was sent to anyone.", "Contiene datos de la red local (IPs, MACs, nombres de red): no se envió nada a nadie."))

	h(t("Interfaces", "Interfaces", "Interfaces"))
	if len(r.Interfaces) == 0 {
		b.WriteString(t("(não lido)\n", "(not read)\n", "(no leído)\n"))
	}
	for _, i := range r.Interfaces {
		fmt.Fprintf(&b, "%s", i.Name)
		if i.Kind != "" {
			fmt.Fprintf(&b, " [%s]", i.Kind)
		}
		if i.MAC != "" {
			fmt.Fprintf(&b, " MAC %s", i.MAC)
		}
		b.WriteByte('\n')
		for _, a := range i.IPv4 {
			fmt.Fprintf(&b, "  IPv4 %s\n", addrText(a))
		}
		for _, a := range i.IPv6 {
			tmp := ""
			if a.Temporary {
				tmp = " (" + t("temporário", "temporary", "temporal") + ")"
			}
			fmt.Fprintf(&b, "  IPv6 %s%s\n", addrText(a), tmp)
		}
	}
	h(t("Rotas padrão (gateway)", "Default routes (gateway)", "Rutas predeterminadas (gateway)"))
	switch {
	case !r.GatewaysRead:
		b.WriteString(t("(não lido)\n", "(not read)\n", "(no leído)\n"))
	case len(r.Gateways) == 0:
		b.WriteString(t("(nenhuma)\n", "(none)\n", "(ninguna)\n"))
	}
	for _, g := range r.Gateways {
		b.WriteString(g + "\n")
	}
	if len(r.Profiles) > 0 {
		h(t("Perfil de rede", "Network profile", "Perfil de red"))
		for _, p := range r.Profiles {
			fmt.Fprintf(&b, "%s [%s] %s\n", p.Name, p.Category, p.Interface)
		}
	}
	h("Wi-Fi")
	if r.WiFi == nil {
		b.WriteString(t("(sem Wi-Fi conectado ou não lido)\n", "(no connected Wi-Fi, or not read)\n", "(sin Wi-Fi conectado o no leído)\n"))
	} else {
		fmt.Fprintf(&b, "%s: %s\n", t("Interface", "Interface", "Interfaz"), r.WiFi.Interface)
		if r.WiFi.SSID != "" {
			fmt.Fprintf(&b, "SSID: %s\n", r.WiFi.SSID)
		} else {
			fmt.Fprintf(&b, "SSID: %s\n", t("(o sistema não informou o nome)", "(the system did not report the name)", "(el sistema no informó el nombre)"))
		}
		fmt.Fprintf(&b, "%s: %s", t("Segurança", "Security", "Seguridad"), r.WiFi.Security)
		if r.WiFi.Raw != "" {
			fmt.Fprintf(&b, " (%s)", r.WiFi.Raw)
		}
		b.WriteByte('\n')
	}
	h("DNS")
	if len(r.DNS.Servers) == 0 {
		b.WriteString(t("(não lido)\n", "(not read)\n", "(no leído)\n"))
	}
	for _, s := range r.DNS.Servers {
		if s.Interface != "" {
			fmt.Fprintf(&b, "%s (%s)\n", s.Addr, s.Interface)
		} else {
			fmt.Fprintf(&b, "%s\n", s.Addr)
		}
	}
	fmt.Fprintf(&b, "DoH/DoT: %s", triWord(r.DNS.Encrypted, l))
	if r.DNS.Note != "" {
		fmt.Fprintf(&b, " (%s)", r.DNS.Note)
	}
	b.WriteByte('\n')
	h(t("Portas em escuta", "Listening ports", "Puertos en escucha"))
	if !r.ListenersRead {
		b.WriteString(t("(não lido)\n", "(not read)\n", "(no leído)\n"))
	}
	for _, li := range r.Listeners {
		fmt.Fprintf(&b, "%s %s:%d", li.Proto, li.Addr, li.Port)
		if li.Process != "" {
			fmt.Fprintf(&b, "  %s", li.Process)
			if li.PID > 0 {
				fmt.Fprintf(&b, " (pid %d)", li.PID)
			}
		}
		b.WriteByte('\n')
	}
	h("Firewall")
	if len(r.Firewalls) == 0 {
		b.WriteString(t("(não lido)\n", "(not read)\n", "(no leído)\n"))
	}
	for _, f := range r.Firewalls {
		fmt.Fprintf(&b, "%s: %s", f.Name, triWord(f.Enabled, l))
		if f.Active {
			fmt.Fprintf(&b, " [%s]", t("em uso", "in use", "en uso"))
		}
		if f.Detail != "" {
			fmt.Fprintf(&b, " (%s)", f.Detail)
		}
		b.WriteByte('\n')
	}
	h(t("IPv6: endereços temporários (privacidade)", "IPv6: temporary addresses (privacy)", "IPv6: direcciones temporales (privacidad)"))
	fmt.Fprintf(&b, "%s\n", triWord(r.IPv6Privacy, l))
	if len(r.Skipped) > 0 {
		h(t("Não lido", "Not read", "No leído"))
		for _, s := range r.Skipped {
			adm := ""
			if s.NeedsAdmin {
				adm = " [" + t("com administrador", "with administrator rights", "con administrador") + "]"
			}
			fmt.Fprintf(&b, "%s%s: %s\n", s.Area, adm, s.Reason)
		}
	}
	return b.String()
}

func addrText(a Addr) string {
	if a.PrefixLen > 0 {
		return fmt.Sprintf("%s/%d", a.IP, a.PrefixLen)
	}
	return a.IP
}

func triWord(v Tri, l Lang) string {
	switch v {
	case Yes:
		return diagfmt.T(l, "sim", "yes", "sí")
	case No:
		return diagfmt.T(l, "não", "no", "no")
	}
	return diagfmt.T(l, "não informado", "not reported", "no informado")
}

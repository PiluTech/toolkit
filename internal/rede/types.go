// Package rede lê, SÓ PARA LEITURA, o estado de rede da máquina (IPs por interface, perfil de rede, tipo de segurança do
// Wi-Fi conectado, DNS, portas em escuta, firewall e privacidade do IPv6), transforma em achados ("o que está exposto e o
// que fazer") e monta o resumo e o relatório completo. Os coletores de cada SO (coleta_win.go, coleta_lin.go,
// coleta_mac.go) só usam o exec.Runner de leitura: nenhum comando muda o sistema, nenhum faz varredura (scan de Wi-Fi,
// de portas ou de hosts), nenhum eleva e nenhum acessa a internet. O que exigiria administrador vira um item em
// Report.Skipped com NeedsAdmin. IPs, MACs e nomes de rede aparecem como o sistema os informa (decisão do Paulo para uma
// ferramenta de análise); a camada de saída (cli/tui) os limpa com textsafe.
package rede

import (
	"time"

	"github.com/PiluTech/toolkit/internal/diagfmt"
	"github.com/PiluTech/toolkit/internal/diagnostico"
)

// Lang é o idioma da saída (o mesmo do diagfmt).
type Lang = diagfmt.Lang

// Skipped é uma área que não pôde ser lida, com o motivo e o que fazer (o mesmo tipo do diagnóstico).
type Skipped = diagnostico.Skipped

// Tri é um estado que pode ser desconhecido: "sem dado" nunca é "ligado" nem "desligado".
type Tri int

// Valores de Tri.
const (
	Unknown Tri = iota
	No
	Yes
)

// String é o nome estável do estado (usado no JSON).
func (t Tri) String() string {
	switch t {
	case Yes:
		return "yes"
	case No:
		return "no"
	}
	return "unknown"
}

// MarshalText faz o JSON mostrar "yes", "no" ou "unknown".
func (t Tri) MarshalText() ([]byte, error) { return []byte(t.String()), nil }

// Security é o tipo de segurança do Wi-Fi conectado.
type Security int

// Tipos de segurança.
const (
	SecUnknown    Security = iota // não lido ou não reconhecido (nunca vira "seguro")
	SecOpen                       // sem senha
	SecWEP                        // quebrado
	SecWPA                        // WPA (TKIP) ou WPA/WPA2 misto
	SecWPA2                       // WPA2-AES (inclui a transição WPA2/WPA3)
	SecWPA3                       // WPA3
	SecEnterprise                 // 802.1X (WPA-Enterprise): autenticação por usuário
)

// String é o nome estável do tipo (usado no JSON).
func (s Security) String() string {
	switch s {
	case SecOpen:
		return "open"
	case SecWEP:
		return "wep"
	case SecWPA:
		return "wpa"
	case SecWPA2:
		return "wpa2"
	case SecWPA3:
		return "wpa3"
	case SecEnterprise:
		return "enterprise"
	}
	return "unknown"
}

// MarshalText faz o JSON mostrar o nome estável.
func (s Security) MarshalText() ([]byte, error) { return []byte(s.String()), nil }

// Addr é um endereço de uma interface.
type Addr struct {
	IP        string // sem o prefixo ("192.0.2.10", "2001:db8::1")
	PrefixLen int    // 0 = desconhecido
	// Temporary: endereço IPv6 temporário (privacidade); só macOS e Windows informam por endereço.
	Temporary bool
}

// Interface é uma interface de rede com os seus endereços.
type Interface struct {
	Name string
	Kind string // "wifi", "ethernet" ou "" (desconhecido)
	MAC  string
	Up   Tri // a interface está conectada/ativa
	IPv4 []Addr
	IPv6 []Addr
}

// WiFi é a conexão Wi-Fi ATUAL (a lista de redes vizinhas nunca é lida).
type WiFi struct {
	Interface string
	SSID      string // "" = o sistema não informou (macOS oculta o nome)
	Security  Security
	Raw       string // texto do sistema que originou Security (para o relatório e para o que não foi reconhecido)
}

// Profile é o perfil de rede de uma conexão (só Windows).
type Profile struct {
	Name      string
	Interface string
	Category  string // "public", "private" ou "domain"
}

// DNSServer é um resolvedor configurado.
type DNSServer struct {
	Addr      string
	Interface string // "" = global ou desconhecida
}

// DNSMode é o modo de criptografia CONFIGURADO do DNS (não a criptografia efetiva de cada consulta).
type DNSMode string

// Modos do DNS.
const (
	DNSModeUnknown       DNSMode = "unknown"       // o SO não informou
	DNSModeOff           DNSMode = "off"           // sem DoH/DoT configurado
	DNSModeOpportunistic DNSMode = "opportunistic" // tenta DoH/DoT, mas pode cair para DNS em claro (ou o fallback não foi lido)
	DNSModeStrict        DNSMode = "strict"        // DoH/DoT sem cair para DNS em claro
)

// MarshalText faz o JSON mostrar sempre um nome ("" vira "unknown").
func (m DNSMode) MarshalText() ([]byte, error) {
	if m == "" {
		m = DNSModeUnknown
	}
	return []byte(m), nil
}

// DNS é a configuração de resolução de nomes.
type DNS struct {
	Servers []DNSServer
	// Encrypted: a criptografia EFETIVA só é Yes quando provada pela configuração (modo estrito, sem fallback para DNS em
	// claro). Modo oportunista, ou o fallback não lido, é Unknown; sem DoH/DoT configurado é No. Unknown quando o SO não
	// informa. Nunca há sondagem externa para confirmar.
	Encrypted Tri
	// Mode é o modo configurado e AllowFallback diz se o sistema admite voltar ao DNS em claro (Unknown = não lido).
	Mode          DNSMode
	AllowFallback Tri
	Note          string // de onde veio a informação de criptografia
}

// Listener é uma porta em escuta.
type Listener struct {
	Proto   string // "tcp" ou "udp"
	Addr    string // "0.0.0.0", "::", "127.0.0.1", "*"...
	Port    int
	Process string // "" = o sistema não informou (outro usuário sem administrador)
	PID     int
}

// Firewall é o estado de um firewall (um perfil no Windows; o firewall do SO ou de aplicativos nos outros).
type Firewall struct {
	Name    string // "Public", "Private", "Domain" (Windows); "ufw", "firewalld", "nftables", "aplicativos", "pf"
	Enabled Tri
	Detail  string // ação padrão de entrada, estado cru...
	Active  bool   // Windows: o perfil está em uso por uma conexão
}

// Report é tudo o que a coleta leu. Campos vazios = não lido (ver Skipped).
type Report struct {
	OS         string // "windows", "linux" ou "darwin"
	Generated  time.Time
	Admin      bool
	Interfaces []Interface
	// Gateways são as rotas padrão (IPv4 e IPv6). GatewaysRead diz que a tabela de rotas foi lida: lista vazia com
	// GatewaysRead=true é "sem rota padrão"; com false é "não lido".
	Gateways      []string
	GatewaysRead  bool
	Profiles      []Profile
	WiFi          *WiFi // nil = sem Wi-Fi conectado ou não lido
	DNS           DNS
	Listeners     []Listener
	ListenersRead bool
	Firewalls     []Firewall
	// IPv6Privacy: o IPv6 usa endereços temporários (privacy extensions). Unknown = não lido.
	IPv6Privacy Tri
	Skipped     []Skipped
}

// Severity é a gravidade de um achado (as 5 do plano; a faixa ok/atenção/problema é só de exibição).
type Severity int

// Gravidades (em ordem crescente).
const (
	SevOK     Severity = iota // tudo certo
	SevInfo                   // informativa, sem ação
	SevLow                    // baixa
	SevMedium                 // média
	SevHigh                   // alta
)

// String é o nome estável da gravidade (usado no JSON).
func (s Severity) String() string {
	switch s {
	case SevHigh:
		return "high"
	case SevMedium:
		return "medium"
	case SevLow:
		return "low"
	case SevInfo:
		return "info"
	}
	return "ok"
}

// MarshalText faz o JSON mostrar o nome estável.
func (s Severity) MarshalText() ([]byte, error) { return []byte(s.String()), nil }

// Finding é uma linha do resumo "o que está exposto e o que fazer".
type Finding struct {
	ID       string // identificador estável ("wifi-open", "porta-sensivel-445"...)
	Area     string // "wifi", "firewall", "portas", "perfil", "dns", "ipv6", "rede", "ip-publico"
	Severity Severity
	Title    string
	Detail   string
	Action   string // "" quando não há ação
}

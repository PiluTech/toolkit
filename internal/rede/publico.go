package rede

// IP público e portal cativo: as ÚNICAS funções deste pacote que existem para falar com a internet, e só SOB PEDIDO da
// pessoa, com um serviço que ELA informou (TOOLKIT_IP_URL, TOOLKIT_PORTAL_URL ou as flags --servico-ip e
// --servico-portal): o toolkit NÃO tem serviço padrão. O pacote em si não importa net/http (o teste do contrato continua
// valendo): a requisição passa pela interface Fetcher, e a implementação real fica em internal/rede/httpclient. Nada aqui
// roda dentro de Collect: o chamador só chama estas funções depois da CONFIRMAÇÃO da pessoa, que nomeia o serviço
// (Plan.Describe), e nada é guardado em disco.

import (
	"context"
	"errors"
	"net/netip"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/PiluTech/toolkit/internal/catalog"
	"github.com/PiluTech/toolkit/internal/diagfmt"
	"github.com/PiluTech/toolkit/internal/textsafe"
)

// Variáveis de ambiente do serviço (a flag do comando, quando dada, vale mais).
const (
	EnvIPURL     = "TOOLKIT_IP_URL"
	EnvPortalURL = "TOOLKIT_PORTAL_URL"
)

// Limites da requisição: o cliente real os aplica e o avaliador os confere de novo.
const (
	MaxBody   = 1024 // bytes lidos da resposta; mais que isso é "resposta inesperada"
	UserAgent = "toolkit"
)

// queryTimeout é o prazo total de UMA consulta (variável só para o teste encurtar).
var queryTimeout = 5 * time.Second

// QueryTimeout é o prazo de uma consulta (para o cliente real usar o mesmo).
func QueryTimeout() time.Duration { return queryTimeout }

// Erros.
var (
	// ErrNoService: nenhuma URL foi informada. O pedido responde "nenhum serviço configurado" e nada é enviado.
	ErrNoService = errors.New("nenhum serviço configurado")
	// ErrInvalidService: a URL informada não passou na validação (nada é enviado).
	ErrInvalidService = errors.New("serviço inválido")
	// ErrNonPublicAddress: o cliente recusou conectar a um endereço não público (o nome do serviço resolveu para a rede
	// local, loopback...). Devolvido (embrulhado) por Fetcher.Get.
	ErrNonPublicAddress = errors.New("endereço não público")
)

// Response é a resposta do serviço, já limitada: o cliente lê no máximo MaxBody+1 bytes e marca Truncated se passou.
type Response struct {
	Status    int
	Location  string // cabeçalho Location (redirecionamento NÃO seguido)
	Body      []byte
	Truncated bool
}

// Fetcher faz UM GET. A implementação real (internal/rede/httpclient) usa TLS 1.2+, sem proxy implícito, sem cookies, sem
// seguir redirecionamentos, com prazo e limite de corpo; os testes usam um falso que conta as chamadas.
type Fetcher interface {
	Get(ctx context.Context, rawURL string) (Response, error)
}

// QueryKind é o tipo de consulta.
type QueryKind string

// Tipos de consulta.
const (
	KindIP     QueryKind = "ip"
	KindPortal QueryKind = "portal"
)

// Plan é o que será enviado: o serviço (já validado) e o tipo. A confirmação mostra EXATAMENTE este plano e a consulta usa
// a URL dele (nunca relê a configuração).
type Plan struct {
	Kind QueryKind
	URL  string
	Host string
}

// NewPlan valida a URL do serviço. Vazia = ErrNoService. Recusa o que o catálogo recusa (http, porta, userinfo,
// fragmento, IP literal, localhost, host sem ponto, caractere de controle ou de formatação).
func NewPlan(kind QueryKind, rawURL string) (Plan, error) {
	raw := strings.TrimSpace(rawURL)
	if raw == "" {
		return Plan{}, ErrNoService
	}
	if err := catalog.CheckURL(raw, false); err != nil {
		return Plan{}, ErrInvalidService
	}
	for _, r := range raw {
		if unsafeURLRune(r) {
			return Plan{}, ErrInvalidService
		}
	}
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" {
		return Plan{}, ErrInvalidService
	}
	return Plan{Kind: kind, URL: raw, Host: strings.ToLower(u.Hostname())}, nil
}

// unsafeURLRune: caractere fora do ASCII visível. URL com texto não ASCII (IDN, bidi) é recusada: a pessoa usa a forma
// punycode, e nada de formatação invisível chega à tela.
func unsafeURLRune(r rune) bool { return r < 0x21 || r > 0x7e }

// DisplayURL é a URL sem os parâmetros (podem ter token).
func (p Plan) DisplayURL() string {
	u, err := url.Parse(p.URL)
	if err != nil {
		return "https://" + p.Host
	}
	return "https://" + u.Host + u.EscapedPath()
}

func (p Plan) hasQuery() bool {
	u, err := url.Parse(p.URL)
	return err == nil && u.RawQuery != ""
}

func tr(l Lang, pt, en, es string) string { return diagfmt.T(l, pt, en, es) }

// Describe é o que a confirmação diz ANTES de qualquer requisição: o serviço (host), o que é enviado, o que a pessoa
// revela e que nada sai sem a confirmação.
func (p Plan) Describe(l Lang) []string {
	out := []string{
		tr(l, "Serviço que será contatado: ", "Service that will be contacted: ", "Servicio que se contactará: ") + p.Host + " (" + p.DisplayURL() + ")",
	}
	sent := tr(l,
		"O que será enviado: uma requisição GET, sem corpo, sem cookies e sem cabeçalhos de identificação além do User-Agent genérico \""+UserAgent+"\".",
		"What will be sent: one GET request, with no body, no cookies and no identifying headers besides the generic User-Agent \""+UserAgent+"\".",
		"Qué se enviará: una petición GET, sin cuerpo, sin cookies y sin cabeceras de identificación salvo el User-Agent genérico \""+UserAgent+"\".")
	if p.hasQuery() {
		sent += tr(l, " A URL tem parâmetros (não mostrados aqui), que também são enviados.", " The URL has parameters (not shown here), which are also sent.", " La URL tiene parámetros (no se muestran aquí), que también se envían.")
	}
	out = append(out, sent)
	if p.Kind == KindPortal {
		out = append(out,
			tr(l, "O que você revela: o IP público desta conexão ao serviço (e a quem o opera), e que esta máquina testou a conexão.", "What you reveal: this connection's public IP to the service (and whoever runs it), and that this machine tested the connection.", "Qué revela: la IP pública de esta conexión al servicio (y a quien lo opera), y que este equipo probó la conexión."),
			tr(l, "O toolkit não segue redirecionamento: se a rede mandar para outra página, ele só diz \"possível portal cativo\" e não abre nada. Resposta limitada a 1 KiB e 5 s; nada é gravado em disco.", "The toolkit does not follow redirects: if the network sends you to another page, it only says \"possible captive portal\" and opens nothing. Response limited to 1 KiB and 5 s; nothing is written to disk.", "El toolkit no sigue redirecciones: si la red lo manda a otra página, solo dice \"posible portal cautivo\" y no abre nada. Respuesta limitada a 1 KiB y 5 s; no se guarda nada en disco."))
	} else {
		out = append(out,
			tr(l, "O que você revela: o IP público desta conexão ao serviço (e a quem o opera); é inevitável em qualquer requisição de internet.", "What you reveal: this connection's public IP to the service (and whoever runs it); that is unavoidable in any internet request.", "Qué revela: la IP pública de esta conexión al servicio (y a quien lo opera); es inevitable en cualquier petición de internet."),
			tr(l, "O toolkit não segue redirecionamento, limita a resposta a 1 KiB e 5 s e não grava nada em disco.", "The toolkit does not follow redirects, limits the response to 1 KiB and 5 s and writes nothing to disk.", "El toolkit no sigue redirecciones, limita la respuesta a 1 KiB y 5 s y no guarda nada en disco."))
	}
	return append(out, tr(l, "Nada será enviado sem a sua confirmação.", "Nothing will be sent without your confirmation.", "No se enviará nada sin su confirmación."))
}

// NoServiceHelp é a resposta quando não há serviço configurado: nada é enviado e a pessoa aprende a configurar.
func NoServiceHelp(kind QueryKind, l Lang) []string {
	if kind == KindPortal {
		return []string{
			tr(l, "Portal cativo: nenhum serviço configurado.", "Captive portal: no service configured.", "Portal cautivo: ningún servicio configurado."),
			tr(l, "O toolkit não tem serviço padrão e não envia nada sem uma URL informada por você.", "The toolkit has no default service and sends nothing without a URL you provide.", "El toolkit no tiene servicio predeterminado y no envía nada sin una URL que usted indique."),
			tr(l, "Para configurar: defina a variável TOOLKIT_PORTAL_URL (ou use --servico-portal) com uma URL https de um endereço de teste que responda 204 sem conteúdo (https, host público, porta 443).", "To configure: set the TOOLKIT_PORTAL_URL variable (or use --servico-portal) to an https URL of a test address that answers 204 with no content (https, public host, port 443).", "Para configurar: defina la variable TOOLKIT_PORTAL_URL (o use --servico-portal) con una URL https de una dirección de prueba que responda 204 sin contenido (https, host público, puerto 443)."),
			tr(l, "Exemplo: TOOLKIT_PORTAL_URL=https://exemplo.org/gen204 toolkit rede portal", "Example: TOOLKIT_PORTAL_URL=https://example.org/gen204 toolkit rede portal", "Ejemplo: TOOLKIT_PORTAL_URL=https://ejemplo.org/gen204 toolkit rede portal"),
		}
	}
	return []string{
		tr(l, "IP público: nenhum serviço configurado.", "Public IP: no service configured.", "IP pública: ningún servicio configurado."),
		tr(l, "O toolkit não tem serviço padrão e não envia nada sem uma URL informada por você.", "The toolkit has no default service and sends nothing without a URL you provide.", "El toolkit no tiene servicio predeterminado y no envía nada sin una URL que usted indique."),
		tr(l, "Para configurar: defina a variável TOOLKIT_IP_URL (ou use --servico-ip) com uma URL https de um serviço que devolva só o seu IP em texto (https, host público, porta 443).", "To configure: set the TOOLKIT_IP_URL variable (or use --servico-ip) to an https URL of a service that returns just your IP as text (https, public host, port 443).", "Para configurar: defina la variable TOOLKIT_IP_URL (o use --servico-ip) con una URL https de un servicio que devuelva solo su IP como texto (https, host público, puerto 443)."),
		tr(l, "Exemplo: TOOLKIT_IP_URL=https://exemplo.org/ip toolkit rede ip", "Example: TOOLKIT_IP_URL=https://example.org/ip toolkit rede ip", "Ejemplo: TOOLKIT_IP_URL=https://ejemplo.org/ip toolkit rede ip"),
		tr(l, "Sem serviço, você ainda vê o IP público no painel do roteador.", "Without a service you can still see the public IP on the router's admin page.", "Sin servicio, aún puede ver la IP pública en el panel del router."),
	}
}

// Outcome é o desfecho de uma consulta.
type Outcome string

// Desfechos.
const (
	OutcomeOK         Outcome = "ok"              // IP válido / portal: internet livre (204)
	OutcomePortal     Outcome = "possible-portal" // só do portal: possível portal cativo
	OutcomeUnexpected Outcome = "unexpected"      // o serviço respondeu, mas não como o esperado
	OutcomeFailed     Outcome = "failed"          // sem resposta (ou pedido inválido)
)

// PublicIPResult é o resultado da consulta do IP público. Nenhum campo guarda o corpo da resposta nem o erro cru.
type PublicIPResult struct {
	Service    string // host do serviço consultado
	IP         string // só com OutcomeOK (forma canônica)
	Outcome    Outcome
	Reason     string // código do motivo quando não é OK ("not-ip", "timeout"...)
	HTTPStatus int
}

// PortalResult é o resultado da sondagem do portal cativo.
type PortalResult struct {
	Service      string
	Outcome      Outcome
	Reason       string
	HTTPStatus   int
	RedirectHost string // só o host do Location (limpo); "" se relativo ou ilegível
}

// classify traduz o erro do cliente num código (o texto cru do erro, que pode ter a URL, nunca é guardado).
func classify(ctx context.Context, err error) string {
	switch {
	case errors.Is(err, ErrNonPublicAddress):
		return "private-addr"
	case errors.Is(ctx.Err(), context.DeadlineExceeded) || errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	case ctx.Err() != nil || errors.Is(err, context.Canceled):
		return "canceled"
	}
	return "connect"
}

// QueryPublicIP pede o IP público ao serviço. SÓ deve ser chamada depois da confirmação. URL inválida ou cliente nulo:
// nenhuma chamada. A resposta é validada com rigor (IPv4/IPv6 por netip, público; nada além do endereço).
func QueryPublicIP(ctx context.Context, f Fetcher, rawURL string) PublicIPResult {
	p, err := NewPlan(KindIP, rawURL)
	if err != nil {
		return PublicIPResult{Outcome: OutcomeFailed, Reason: "invalid-url"}
	}
	res := PublicIPResult{Service: p.Host}
	if f == nil {
		res.Outcome, res.Reason = OutcomeFailed, "no-client"
		return res
	}
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()
	resp, err := f.Get(ctx, p.URL)
	if err != nil {
		res.Outcome, res.Reason = OutcomeFailed, classify(ctx, err)
		return res
	}
	res.HTTPStatus = resp.Status
	res.Outcome = OutcomeUnexpected
	switch {
	case resp.Truncated || len(resp.Body) > MaxBody:
		res.Reason = "too-large"
	case resp.Status >= 300 && resp.Status < 400:
		res.Reason = "redirect"
	case resp.Status != 200:
		res.Reason = "status"
	default:
		ip, reason := parsePublicIP(string(resp.Body))
		if reason != "" {
			res.Reason = reason
			return res
		}
		res.Outcome, res.IP = OutcomeOK, ip
	}
	return res
}

// parsePublicIP aceita SÓ um endereço (com espaço/quebra de linha nas pontas) e público; devolve a forma canônica.
func parsePublicIP(body string) (ip, reason string) {
	a, err := netip.ParseAddr(strings.TrimSpace(body))
	if err != nil {
		return "", "not-ip"
	}
	a = a.Unmap()
	if !isPublicIP(a.WithZone("").String()) {
		return "", "non-public"
	}
	if a.Zone() != "" {
		return "", "not-ip"
	}
	return a.String(), ""
}

// IsPublicIP diz que o endereço é público (não loopback, privado, link-local, CGNAT nem não especificado). O cliente real
// o usa para recusar conectar a endereço não público.
func IsPublicIP(addr string) bool { return isPublicIP(addr) }

// ProbePortal faz o GET de sondagem e interpreta: 204 = internet livre; redirecionamento (NÃO seguido), 200 com conteúdo ou
// 511 = possível portal cativo; outra resposta = inesperada (sem conclusão); sem resposta = falha (não é prova de portal).
func ProbePortal(ctx context.Context, f Fetcher, rawURL string) PortalResult {
	p, err := NewPlan(KindPortal, rawURL)
	if err != nil {
		return PortalResult{Outcome: OutcomeFailed, Reason: "invalid-url"}
	}
	res := PortalResult{Service: p.Host}
	if f == nil {
		res.Outcome, res.Reason = OutcomeFailed, "no-client"
		return res
	}
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()
	resp, err := f.Get(ctx, p.URL)
	if err != nil {
		res.Outcome, res.Reason = OutcomeFailed, classify(ctx, err)
		return res
	}
	res.HTTPStatus = resp.Status
	switch {
	case resp.Status == 204:
		res.Outcome = OutcomeOK
	case resp.Status >= 300 && resp.Status < 400:
		res.Outcome, res.Reason, res.RedirectHost = OutcomePortal, "redirect", locationHost(resp.Location)
	case resp.Status == 200 || resp.Status == 511:
		res.Outcome, res.Reason = OutcomePortal, "unexpected-content"
	default:
		res.Outcome, res.Reason = OutcomeUnexpected, "status"
	}
	return res
}

// locationHost devolve só o host do Location, e só se for um nome seguro e curto (ASCII visível). O resto vira "".
func locationHost(loc string) string {
	u, err := url.Parse(strings.TrimSpace(loc))
	if err != nil {
		return ""
	}
	h := strings.ToLower(u.Hostname())
	if h == "" || len(h) > 100 {
		return ""
	}
	for _, r := range h {
		if unsafeURLRune(r) {
			return ""
		}
	}
	return h
}

// reasonText é o motivo em texto (nunca o corpo nem o erro cru).
func reasonText(reason string, status int, l Lang) string {
	switch reason {
	case "invalid-url":
		return tr(l, "a URL do serviço é inválida", "the service URL is invalid", "la URL del servicio no es válida")
	case "no-client":
		return tr(l, "sem cliente HTTP", "no HTTP client", "sin cliente HTTP")
	case "timeout":
		return tr(l, "o serviço não respondeu em 5 s", "the service did not answer within 5 s", "el servicio no respondió en 5 s")
	case "canceled":
		return tr(l, "cancelado", "cancelled", "cancelado")
	case "connect":
		return tr(l, "não foi possível conectar (sem internet, DNS ou serviço fora do ar); isso não é prova de nada", "could not connect (no internet, DNS or the service is down); that is not proof of anything", "no se pudo conectar (sin internet, DNS o servicio caído); eso no prueba nada")
	case "private-addr":
		return tr(l, "o nome do serviço aponta para um endereço não público: recusado (pode ser um portal cativo interceptando o DNS)", "the service name points to a non-public address: refused (could be a captive portal intercepting DNS)", "el nombre del servicio apunta a una dirección no pública: rechazado (puede ser un portal cautivo interceptando el DNS)")
	case "redirect":
		return tr(l, "o serviço redirecionou para outro endereço (o toolkit não segue redirecionamentos)", "the service redirected elsewhere (the toolkit does not follow redirects)", "el servicio redirigió a otra dirección (el toolkit no sigue redirecciones)")
	case "status":
		return tr(l, "o serviço respondeu HTTP "+itoa(status), "the service answered HTTP "+itoa(status), "el servicio respondió HTTP "+itoa(status))
	case "not-ip":
		return tr(l, "resposta inesperada: não é um endereço IP", "unexpected response: not an IP address", "respuesta inesperada: no es una dirección IP")
	case "non-public":
		return tr(l, "resposta inesperada: o endereço devolvido não é público", "unexpected response: the returned address is not public", "respuesta inesperada: la dirección devuelta no es pública")
	case "too-large":
		return tr(l, "resposta inesperada: grande demais", "unexpected response: too large", "respuesta inesperada: demasiado grande")
	case "unexpected-content":
		return tr(l, "o serviço respondeu com conteúdo em vez de 204", "the service answered with content instead of 204", "el servicio respondió con contenido en vez de 204")
	}
	return reason
}

// Lines é o resultado em texto para o terminal ou a faixa (nenhum corpo de resposta entra aqui).
func (r PublicIPResult) Lines(l Lang) []string {
	svc := textsafe.Line(r.Service)
	if r.Outcome == OutcomeOK {
		return []string{
			tr(l, "IP público: ", "Public IP: ", "IP pública: ") + r.IP + " (" + tr(l, "consultado em ", "queried at ", "consultado en ") + svc + ")",
			tr(l, "É o endereço que os sites veem. O toolkit não o guardou.", "This is the address sites see. The toolkit did not store it.", "Es la dirección que ven los sitios. El toolkit no la guardó."),
		}
	}
	return []string{tr(l, "IP público: não foi possível obter ("+svc+")", "Public IP: could not be obtained ("+svc+")", "IP pública: no se pudo obtener ("+svc+")"), reasonText(r.Reason, r.HTTPStatus, l)}
}

// Lines é o resultado da sondagem em texto.
func (r PortalResult) Lines(l Lang) []string {
	svc := textsafe.Line(r.Service)
	switch r.Outcome {
	case OutcomeOK:
		return []string{tr(l, "Portal cativo: não detectado ("+svc+" respondeu 204, como esperado)", "Captive portal: not detected ("+svc+" answered 204, as expected)", "Portal cautivo: no detectado ("+svc+" respondió 204, como se esperaba)")}
	case OutcomePortal:
		out := []string{tr(l, "Portal cativo: possível portal cativo ("+svc+")", "Captive portal: possible captive portal ("+svc+")", "Portal cautivo: posible portal cautivo ("+svc+")"), reasonText(r.Reason, r.HTTPStatus, l)}
		if r.RedirectHost != "" {
			out = append(out, tr(l, "Redirecionou para: ", "Redirected to: ", "Redirigió a: ")+textsafe.Line(r.RedirectHost))
		}
		return append(out, tr(l, "O toolkit não abre nenhuma página: se a rede pede login, abra o navegador num site simples (http) para ver a página de entrada.", "The toolkit opens no page: if the network asks you to log in, open the browser on a simple (http) site to see the sign-in page.", "El toolkit no abre ninguna página: si la red pide iniciar sesión, abra el navegador en un sitio simple (http) para ver la página de entrada."))
	}
	return []string{tr(l, "Portal cativo: sem conclusão ("+svc+")", "Captive portal: inconclusive ("+svc+")", "Portal cautivo: sin conclusión ("+svc+")"), reasonText(r.Reason, r.HTTPStatus, l)}
}

// ---- achados ----

func analysePublic(r *Report, l Lang, add addFn) {
	if p := r.PublicIP; p != nil {
		switch p.Outcome {
		case OutcomeOK:
			add("ip-publico", "ip-publico", SevInfo,
				tr(l, "IP público: "+p.IP+" (consultado em "+textsafe.Line(p.Service)+")", "Public IP: "+p.IP+" (queried at "+textsafe.Line(p.Service)+")", "IP pública: "+p.IP+" (consultado en "+textsafe.Line(p.Service)+")"),
				tr(l, "é o endereço que os sites veem; o toolkit o pediu a esse serviço só a seu pedido e não o guardou", "this is the address sites see; the toolkit asked that service only on your request and did not store it", "es la dirección que ven los sitios; el toolkit la pidió a ese servicio solo a su pedido y no la guardó"), "")
			analyseExposure(r, l, add)
		default:
			add("ip-publico", "ip-publico", SevInfo,
				tr(l, "IP público: não foi possível obter ("+textsafe.Line(p.Service)+")", "Public IP: could not be obtained ("+textsafe.Line(p.Service)+")", "IP pública: no se pudo obtener ("+textsafe.Line(p.Service)+")"),
				reasonText(p.Reason, p.HTTPStatus, l),
				tr(l, "Para saber o IP público, veja o painel do roteador.", "To learn the public IP, check the router's admin page.", "Para saber la IP pública, vea el panel del router."))
		}
	} else {
		add("ip-publico", "ip-publico", SevInfo,
			tr(l, "IP público: não consultado (só sob pedido)", "Public IP: not queried (only on request)", "IP pública: no consultado (solo bajo pedido)"),
			tr(l, "descobrir o IP público exige uma requisição a um serviço externo que você escolhe, o que revela o seu IP a ele; o toolkit não tem serviço padrão", "finding the public IP needs a request to an external service you choose, which reveals your IP to it; the toolkit has no default service", "descubrir la IP pública requiere una petición a un servicio externo que usted elige, lo que le revela su IP; el toolkit no tiene servicio predeterminado"),
			tr(l, "Para consultar: toolkit rede ip --servico-ip <URL https> (ou a tecla o na TUI); o portal cativo, com toolkit rede portal. Ou veja o IP público no painel do roteador.", "To query: toolkit rede ip --servico-ip <https URL> (or key o in the TUI); the captive portal, with toolkit rede portal. Or check the public IP on the router's admin page.", "Para consultar: toolkit rede ip --servico-ip <URL https> (o la tecla o en la TUI); el portal cautivo, con toolkit rede portal. O vea la IP pública en el panel del router."))
	}
	if p := r.Portal; p != nil {
		det := strings.Join(p.Lines(l)[1:], " ")
		title := p.Lines(l)[0]
		switch p.Outcome {
		case OutcomeOK:
			add("portal-cativo", "portal", SevOK, title, "", "")
		case OutcomePortal:
			if len(p.Lines(l)) > 1 {
				det = strings.Join(p.Lines(l)[1:len(p.Lines(l))-1], " ")
			}
			add("portal-cativo", "portal", SevLow, title, det,
				tr(l, "Abra o navegador num site simples (http) para ver a página de entrada da rede; o toolkit não abre nada.", "Open the browser on a simple (http) site to see the network's sign-in page; the toolkit opens nothing.", "Abra el navegador en un sitio simple (http) para ver la página de entrada de la red; el toolkit no abre nada."))
		default:
			add("portal-cativo", "portal", SevInfo, title, tr(l, det+" — não é prova de portal nem de internet livre", det+" — this is not proof of a portal nor of free internet", det+" — no es prueba de portal ni de internet libre"), "")
		}
	}
}

func fwText(fw Tri, l Lang) string {
	switch fw {
	case Yes:
		return tr(l, "o firewall está ligado", "the firewall is on", "el firewall está activado")
	case No:
		return tr(l, "o firewall está desligado", "the firewall is off", "el firewall está desactivado")
	}
	return tr(l, "não foi possível saber se o firewall está ligado", "could not tell whether the firewall is on", "no se pudo saber si el firewall está activado")
}

// behindCGNAT: uma interface física (Wi-Fi ou cabo) com IPv4 em 100.64.0.0/10. Interface de tipo desconhecido não vale
// (uma VPN como a Tailscale também usa esta faixa).
func behindCGNAT(r *Report) (string, bool) {
	for _, i := range r.Interfaces {
		if (i.Kind != "wifi" && i.Kind != "ethernet") || i.Up == No {
			continue
		}
		for _, a := range i.IPv4 {
			if ad, ok := parseIP(a.IP); ok && cgnat.Contains(ad) {
				return a.IP, true
			}
		}
	}
	return "", false
}

// directPublic: o IP público consultado é o de uma interface desta máquina (sem NAT no caminho).
func directPublic(r *Report) bool {
	pub, ok := parseIP(r.PublicIP.IP)
	if !ok {
		return false
	}
	for _, i := range r.Interfaces {
		for _, a := range append(append([]Addr{}, i.IPv4...), i.IPv6...) {
			if ad, ok := parseIP(a.IP); ok && ad == pub {
				return true
			}
		}
	}
	return false
}

// analyseExposure cruza o IP público com as portas em escuta e o firewall JÁ lidos. Não varre nada de fora.
func analyseExposure(r *Report, l Lang, add addFn) {
	title := tr(l, "Máquina exposta? ", "Exposed machine? ", "¿Equipo expuesto? ")
	honest := tr(l, "o toolkit não testa de fora; só cruza o que está escutando com o firewall", "the toolkit does not test from outside; it only cross-checks what is listening with the firewall", "el toolkit no prueba desde fuera; solo cruza lo que escucha con el firewall")
	if !r.ListenersRead {
		add("maquina-exposta", "ip-publico", SevInfo, title+tr(l, "não foi possível avaliar", "could not be assessed", "no se pudo evaluar"),
			tr(l, "as portas em escuta não foram lidas, então não há o que cruzar com o IP público; "+honest, "the listening ports were not read, so there is nothing to cross with the public IP; "+honest, "los puertos en escucha no se leyeron, así que no hay qué cruzar con la IP pública; "+honest), "")
		return
	}
	var extra []string
	if hasGlobalV6(r) {
		extra = append(extra, tr(l, "esta máquina tem endereço IPv6 global, que pode ser alcançável mesmo atrás de CGNAT ou NAT; confira o firewall para IPv6", "this machine has a global IPv6 address, which may be reachable even behind CGNAT or NAT; check the IPv6 firewall", "este equipo tiene dirección IPv6 global, que puede ser alcanzable incluso tras CGNAT o NAT; revise el firewall para IPv6"))
	}
	if ip, ok := behindCGNAT(r); ok {
		det := tr(l, "a máquina provavelmente não é alcançável de fora por IPv4: o IP dela ("+ip+") é de CGNAT (100.64.0.0/10), compartilhado pelo provedor; ", "the machine is probably not reachable from outside over IPv4: its IP ("+ip+") is CGNAT (100.64.0.0/10), shared by the provider; ", "el equipo probablemente no es alcanzable desde fuera por IPv4: su IP ("+ip+") es CGNAT (100.64.0.0/10), compartida por el proveedor; ") + honest
		add("maquina-exposta", "ip-publico", SevInfo, title+tr(l, "provavelmente não por IPv4 (CGNAT)", "probably not over IPv4 (CGNAT)", "probablemente no por IPv4 (CGNAT)"), strings.Join(append([]string{det}, extra...), "; "), "")
		return
	}
	var ports []string
	seen := map[int]bool{}
	for _, li := range r.Listeners {
		if sp, ok := sensitiveByPort(li.Port); ok && li.Proto == "tcp" && !isLoopback(li.Addr) && !seen[li.Port] {
			seen[li.Port] = true
			ports = append(ports, sp.Name+" "+itoa(li.Port))
		}
	}
	sort.Strings(ports)
	fw := fwState(r)
	var det []string
	if directPublic(r) {
		det = append(det, tr(l, "o IP público é o de uma interface desta máquina (sem NAT): nada do roteador fica entre a internet e as portas", "the public IP is that of one of this machine's interfaces (no NAT): no router sits between the internet and the ports", "la IP pública es la de una interfaz de este equipo (sin NAT): ningún router queda entre internet y los puertos"))
	}
	if len(ports) == 0 {
		det = append(det, tr(l, "nenhuma porta sensível (da lista fechada do toolkit) escuta fora desta máquina", "no sensitive port (from the toolkit's closed list) listens outside this machine", "ningún puerto sensible (de la lista cerrada del toolkit) escucha fuera de este equipo"), honest)
		add("maquina-exposta", "ip-publico", SevInfo, title+tr(l, "nenhuma porta sensível escutando fora desta máquina", "no sensitive port listening outside this machine", "ningún puerto sensible escuchando fuera de este equipo"), strings.Join(append(det, extra...), "; "), "")
		return
	}
	sev := SevMedium
	if fw == Yes {
		sev = SevLow
	}
	det = append(det, tr(l, "escutam fora desta máquina: ", "listening outside this machine: ", "escuchan fuera de este equipo: ")+strings.Join(ports, ", "), fwText(fw, l), honest+tr(l, "; se a internet alcança depende do roteador, que o toolkit não vê", "; whether the internet reaches it depends on the router, which the toolkit cannot see", "; si internet lo alcanza depende del router, que el toolkit no ve"))
	add("maquina-exposta", "ip-publico", sev, title+tr(l, "há portas sensíveis escutando fora desta máquina", "sensitive ports are listening outside this machine", "hay puertos sensibles escuchando fuera de este equipo"), strings.Join(append(det, extra...), "; "),
		tr(l, "Feche o serviço ou restrinja-o ao 127.0.0.1 e ligue o firewall (o toolkit não muda nada).", "Close the service or restrict it to 127.0.0.1 and turn the firewall on (the toolkit changes nothing).", "Cierre el servicio o restrínjalo a 127.0.0.1 y active el firewall (el toolkit no cambia nada)."))
}

// QuerySummary é o texto do resultado das consultas de IP público e portal: só os achados dessas áreas.
func QuerySummary(r *Report, l Lang) string {
	var b strings.Builder
	for _, f := range Analyze(r, l) {
		if f.Area != "ip-publico" && f.Area != "portal" {
			continue
		}
		formatFinding(&b, f, l)
	}
	return b.String()
}

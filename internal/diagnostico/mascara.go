package diagnostico

import (
	"regexp"
	"strings"

	"github.com/PiluTech/toolkit/internal/diagfmt"
)

var (
	reEmail   = regexp.MustCompile(`[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}`)
	reIPv4    = regexp.MustCompile(`\b(\d{1,3})\.(\d{1,3})\.(\d{1,3})\.(\d{1,3})\b`)
	reIPv6    = regexp.MustCompile(`\b[23][0-9a-fA-F]{3}(?::[0-9a-fA-F]{0,4}){2,7}\b`)
	reWinUser = regexp.MustCompile(`(?i)([A-Z]:\\Users\\)[^\\/\s"'<>|]+`)
	reUnixHom = regexp.MustCompile(`(/(?:Users|home)/)[^/\s"'<>|]+`)
	reWinHost = regexp.MustCompile(`(?i)(\\\\)[A-Za-z0-9._-]+(\\)`)
)

func privateIPv4(a, b int) bool {
	return a == 10 || a == 127 || (a == 172 && b >= 16 && b <= 31) || (a == 192 && b == 168) || (a == 169 && b == 254) || (a == 100 && b >= 64 && b <= 127) || a == 0
}

// MaskText mascara os dados sensíveis de um texto do relatório, nesta ordem: identificadores da máquina (série, UUID,
// MAC...: diagfmt.Redact), e-mails, pastas pessoais (C:\Users\NOME, /Users/NOME, /home/NOME), os nomes pessoais
// informados (usuário e nome da máquina, sem diferenciar maiúsculas), compartilhamentos de rede (\\SERVIDOR\) e IPs
// públicos (IPv4 vira A.x.x.x; IPv6 global vira 2001:x:x; IPs privados ficam).
func MaskText(s string, personal []string) string {
	s = diagfmt.RedactText(s)
	s = reEmail.ReplaceAllString(s, "[e-mail]")
	s = reWinUser.ReplaceAllString(s, "${1}[usuário]")
	s = reUnixHom.ReplaceAllString(s, "${1}[usuário]")
	s = reWinHost.ReplaceAllString(s, "${1}[servidor]${2}")
	for _, n := range personal {
		s = maskName(s, strings.TrimSpace(n))
	}
	s = reIPv4.ReplaceAllStringFunc(s, func(m string) string {
		p := reIPv4.FindStringSubmatch(m)
		a, b := atoi(p[1]), atoi(p[2])
		if a > 255 || b > 255 || atoi(p[3]) > 255 || atoi(p[4]) > 255 { // não é IP (versão, data...)
			return m
		}
		if privateIPv4(a, b) {
			return m
		}
		return p[1] + ".x.x.x"
	})
	return reIPv6.ReplaceAllStringFunc(s, func(m string) string { return m[:4] + ":x:x" })
}

func atoi(s string) int {
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			return 999
		}
		n = n*10 + int(r-'0')
	}
	return n
}

// maskName mascara um nome pessoal. Nome com 3 caracteres ou mais: em qualquer posição, sem diferenciar maiúsculas
// (um nome de usuário aparece dentro de caminhos, domínios e frases). Nome curto (1 a 2 caracteres, ex.: usuário "Li",
// máquina "PC"): por TOKEN, ou seja, só quando é a palavra inteira (delimitada por algo que não é letra, dígito ou _),
// sem diferenciar maiúsculas (o log muda a caixa: "li", "LI", "Pc"), para "PC" não mascarar "PCI" nem "list" virar
// "[pessoal]st". Nome de 1 caractere não
// dá para distinguir de texto comum ("a") e é ignorado (limite documentado em docs/diagnostico.md).
func maskName(s, n string) string {
	switch {
	case n == "" || len([]rune(n)) < 2:
		return s
	case len([]rune(n)) >= 3:
		return regexp.MustCompile(`(?i)`+regexp.QuoteMeta(n)).ReplaceAllString(s, "[pessoal]")
	}
	re := regexp.MustCompile(`(?i)(^|[^\p{L}\p{N}_])` + regexp.QuoteMeta(n) + `($|[^\p{L}\p{N}_])`)
	for i := 0; i < 8; i++ { // o delimitador entre dois tokens vizinhos ("Li Li") é consumido por um só casamento
		out := re.ReplaceAllString(s, "${1}[pessoal]${2}")
		if out == s {
			break
		}
		s = out
	}
	return s
}

// MaskDocument aplica MaskText a todo VALOR de texto de um documento já decodificado (mapas, listas e strings) e
// devolve o documento: as chaves, os números e os tipos ficam exatamente como estão. É o que o --json usa, ANTES de
// serializar (mascarar o JSON já serializado poderia trocar chaves do contrato ou quebrar escapes).
func MaskDocument(v any, personal []string) any {
	switch x := v.(type) {
	case map[string]any:
		for k, val := range x {
			x[k] = MaskDocument(val, personal)
		}
		return x
	case []any:
		for i := range x {
			x[i] = MaskDocument(x[i], personal)
		}
		return x
	case string:
		return MaskText(x, personal)
	}
	return v
}

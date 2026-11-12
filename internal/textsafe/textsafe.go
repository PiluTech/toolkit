// Package textsafe limpa texto vindo de fora (catálogo, saída de gerenciador, notas de release, mensagens
// de erro) antes de ir para o terminal: uma sequência de escape no texto redesenharia a tela ou
// reescreveria o título da janela.
package textsafe

import (
	"fmt"
	"strings"
	"unicode"
)

// Printable devolve s com tudo que não é imprimível (controle, ESC, CR, quebra de linha, U+2028, bidi
// U+202E, zero width...) escapado como \xNN, \uNNNN ou \UNNNNNNNN. Nunca repassa sequência de terminal.
func Printable(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == ' ' || (unicode.IsPrint(r) && !unicode.Is(unicode.Cf, r)):
			b.WriteRune(r)
		case r < 0x100:
			fmt.Fprintf(&b, `\x%02x`, r)
		case r < 0x10000:
			fmt.Fprintf(&b, `\u%04x`, r)
		default:
			fmt.Fprintf(&b, `\U%08x`, r)
		}
	}
	return b.String()
}

// Line é Printable para texto que tem de caber numa linha só: tab vira espaço e quebras viram " ⏎ ".
func Line(s string) string {
	s = strings.NewReplacer("\r\n", " ⏎ ", "\n", " ⏎ ", "\r", " ⏎ ", "\t", " ").Replace(s)
	return Printable(s)
}

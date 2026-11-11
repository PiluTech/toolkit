package winget

import (
	"strings"
	"unicode"
)

// ParseTable lê a tabela do winget. O cabeçalho muda com o idioma do Windows, então as colunas são
// localizadas pela posição: a linha de traços marca o início dos dados, e o início de cada coluna vem das
// palavras do cabeçalho. Descarta spinner e progresso (\r e o que vem antes
// dele na linha). Devolve as linhas de dados já cortadas por coluna e aparadas; sem tabela, nil.
func ParseTable(out []byte) [][]string {
	var lines []string
	for _, l := range strings.Split(string(out), "\n") {
		if i := strings.LastIndex(l, "\r"); i >= 0 {
			l = l[i+1:]
		}
		lines = append(lines, strings.TrimRight(l, " \t"))
	}
	dash := -1
	for i, l := range lines {
		if t := strings.TrimSpace(l); len(t) >= 3 && strings.Trim(t, "-") == "" {
			dash = i
			break
		}
	}
	if dash < 1 {
		return nil
	}
	header := lines[dash-1]
	starts := columnStarts(header)
	if len(starts) == 0 {
		return nil
	}
	var rows [][]string
	for _, l := range lines[dash+1:] {
		if strings.TrimSpace(l) == "" {
			continue
		}
		rows = append(rows, cut([]rune(l), starts))
	}
	return rows
}

// columnStarts devolve o índice (em runes) onde começa cada palavra do cabeçalho. O winget alinha as colunas
// com um espaço de folga, então "2 espaços" não serve de separador; os cabeçalhos de cada idioma são de uma
// palavra só (Name Id Version Available Source / Nome ID Versão Disponível Origem).
func columnStarts(header string) []int {
	var starts []int
	prevSpace := true
	for i, r := range []rune(header) {
		if unicode.IsSpace(r) {
			prevSpace = true
			continue
		}
		if prevSpace {
			starts = append(starts, i)
		}
		prevSpace = false
	}
	return starts
}

func cut(rs []rune, starts []int) []string {
	out := make([]string, len(starts))
	for i, s := range starts {
		end := len(rs)
		if i+1 < len(starts) {
			end = starts[i+1]
		}
		if s >= len(rs) {
			continue
		}
		if end > len(rs) {
			end = len(rs)
		}
		out[i] = strings.TrimSpace(string(rs[s:end]))
	}
	return out
}

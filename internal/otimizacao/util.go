package otimizacao

import (
	"errors"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
)

func itoa(n int) string { return strconv.Itoa(n) }

// ---- JSON tolerante (campos nulos, número como texto, objeto único no lugar de lista) ----

func asMap(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}

// asList devolve os objetos de uma lista; o PowerShell entrega um objeto único no lugar da lista de um item.
func asList(v any) []map[string]any {
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

// numList devolve os números de uma lista (ou o escalar único); nulo e texto que não é número são descartados.
func numList(v any) []float64 {
	var items []any
	switch x := v.(type) {
	case []any:
		items = x
	case nil:
		return nil
	default:
		items = []any{x}
	}
	var out []float64
	for _, e := range items {
		if f, ok := num(e); ok {
			out = append(out, f)
		}
	}
	return out
}

func str(v any) string {
	switch x := v.(type) {
	case string:
		return clean(x)
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	}
	return ""
}

func num(v any) (float64, bool) {
	switch x := v.(type) {
	case float64:
		if math.IsNaN(x) || math.IsInf(x, 0) {
			return 0, false
		}
		return x, true
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(x), 64)
		if err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
			return 0, false
		}
		return f, true
	}
	return 0, false
}

func unum(v any) (uint64, bool) {
	f, ok := num(v)
	if !ok || f < 0 || f >= 1.8e19 {
		return 0, false
	}
	return uint64(f), true
}

func inum(v any) (int, bool) {
	f, ok := num(v)
	if !ok || f < math.MinInt32 || f > math.MaxInt32 {
		return 0, false
	}
	return int(math.Round(f)), true
}

const maxName = 80

// clean deixa um texto do sistema numa linha só, sem controle, ESC, bidi nem separadores (viram espaço), em até maxName
// letras. A saída ainda passa por textsafe na borda (CLI e TUI): esta é a primeira camada.
func clean(s string) string {
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) || r == 0x2028 || r == 0x2029 {
			return ' '
		}
		return r
	}, s)
	s = strings.Join(strings.Fields(s), " ")
	if rs := []rune(s); len(rs) > maxName {
		s = string(rs[:maxName-1]) + "…"
	}
	return s
}

var instanceSuffix = regexp.MustCompile(`#\d+$`)

// procName tira o "#2" das instâncias do contador de desempenho ("chrome#2" vira "chrome").
func procName(s string) string {
	return strings.TrimSpace(instanceSuffix.ReplaceAllString(clean(s), ""))
}

// baseProgram é só o nome do programa de uma linha de comando: sem aspas, sem argumentos e sem caminho.
func baseProgram(cmd string) string {
	s := strings.TrimSpace(cmd)
	switch {
	case strings.HasPrefix(s, `"`):
		if i := strings.Index(s[1:], `"`); i >= 0 {
			s = s[1 : 1+i]
		} else {
			s = s[1:]
		}
	default:
		if i := strings.IndexAny(s, " \t"); i >= 0 {
			// um caminho com espaço sem aspas ("C:\Program Files\x.exe") perde o fim: só o nome base importa aqui
			if j := strings.LastIndex(strings.ToLower(s), ".exe"); j >= 0 {
				s = s[:j+4]
			} else {
				s = s[:i]
			}
		}
	}
	if i := strings.LastIndexAny(s, `\/`); i >= 0 {
		s = s[i+1:]
	}
	return clean(s)
}

func parseTime(v any) time.Time {
	t, err := time.Parse(time.RFC3339Nano, str(v))
	if err != nil {
		return time.Time{}
	}
	return t
}

var errNoData = errors.New("sem dados")

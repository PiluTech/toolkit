package diagfmt

import (
	"bytes"
	"encoding/json"
	"regexp"
	"strings"
)

// Redact remove de um texto cru (JSON do system_profiler, saída do PowerShell, /proc...) os identificadores da
// máquina: número de série, UUID/GUID, UDID, ECID, WWN, endereço MAC, IMEI e afins. Roda no ÚLTIMO ponto antes de a
// saída aparecer em qualquer lugar (resumo, modo detalhes, falha do comando, log do smoke).
//
// Estratégia: JSON válido é lido de verdade e redigido POR CHAVE no objeto (aspas escapadas, unicode e valores de
// qualquer tipo ficam certos); todo o resto (truncado, tabela do Format-Table, tabela de colunas do lsblk, linhas
// "Chave: valor") passa pelo fallback conservador por padrões de texto; e em qualquer caso UUIDs e MACs soltos somem.
// O valor vira "[***]"; a chave continua visível.
const redactedValue = "[***]"

const idKeys = `serial|uuid|guid|udid|ecid|wwn|nguid|eui-?64|imei|meid|mac[ _-]?addr|ethernet[ _-]?addr|hardware[ _-]?id|machine[ _-]?id|platform[ _-]?id|device[ _-]?id|unique[ _-]?id|volume[ _-]?id|provisioning`

var (
	idKeyRe = regexp.MustCompile(`(?i)(?:` + idKeys + `)`)
	// "chave com identificador": valor string (com escapes, inclusive truncada no fim da entrada), número ou booleano
	redactJSONish = regexp.MustCompile(`(?i)("(?:[^"\\\n]|\\.)*(?:` + idKeys + `)(?:[^"\\\n]|\\.)*"\s*:\s*)(?:"(?:[^"\\]|\\.)*(?:"|\\?\z)|[^,}\]\s][^,}\]\n]*)`)
	// linhas "Chave: valor" ou "Chave = valor" (a chave não tem aspas nem chaves: o JSON é da regra de cima)
	redactLine = regexp.MustCompile(`(?im)^([ \t]*[^\n:="{}\[\]]*(?:` + idKeys + `)[^\n:="{}\[\]]*[:=][ \t]*)\S.*$`)
	// "serial: X", "uuid=X" no MEIO de uma frase (mensagem de log): o valor vira [***]
	redactInline = regexp.MustCompile(`(?i)(\b(?:serial(?:[ _-]?(?:number|no\.?))?|uuid|guid|udid|ecid|wwn|imei|meid)\b\s*[:=]\s*)[^\s,;]+`)
	redactUUID   = regexp.MustCompile(`\b[0-9A-Fa-f]{8}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{12}\b`)
	redactMAC    = regexp.MustCompile(`\b(?:[0-9A-Fa-f]{2}[:-]){5}[0-9A-Fa-f]{2}\b`)
	dashLine     = regexp.MustCompile(`^\s*-+(?:\s+-+)*\s*$`)
	dashRun      = regexp.MustCompile(`-+`)
	wordStart    = regexp.MustCompile(`\S+`)
)

// Redact devolve s sem os identificadores da máquina. JSON válido é redigido por chave E por valor ANTES de ser
// serializado de novo (o resultado continua JSON válido, com as mesmas chaves e tipos); o resto passa por RedactText.
func Redact(s string) string {
	if out, ok := redactJSONDocument(s); ok {
		return out
	}
	return RedactText(s)
}

// RedactText é o caminho de TEXTO (sem tentar ler JSON): tabelas, JSON truncado, linhas "Chave: valor", identificadores
// no meio de uma frase e UUIDs e MACs soltos. É o que se usa em texto corrido e em cada valor de string já decodificado.
func RedactText(s string) string {
	s = redactTables(s)
	s = redactJSONish.ReplaceAllString(s, `${1}"`+redactedValue+`"`)
	s = redactLine.ReplaceAllString(s, `${1}`+redactedValue)
	return redactTokens(s)
}

func redactTokens(s string) string {
	s = redactInline.ReplaceAllString(s, `${1}`+redactedValue)
	return redactMAC.ReplaceAllString(redactUUID.ReplaceAllString(s, redactedValue), redactedValue)
}

// redactJSONDocument lê s como UM documento JSON válido e redige por chave. ok=false quando não é JSON válido.
func redactJSONDocument(s string) (string, bool) {
	t := strings.TrimSpace(s)
	if t == "" || (t[0] != '{' && t[0] != '[') {
		return "", false
	}
	dec := json.NewDecoder(strings.NewReader(t))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil || dec.More() {
		return "", false
	}
	if _, err := dec.Token(); err == nil { // lixo depois do documento
		return "", false
	}
	v = redactValue(v)
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if strings.Contains(t, "\n") {
		enc.SetIndent("", "  ")
	}
	if err := enc.Encode(v); err != nil {
		return "", false
	}
	return strings.TrimRight(buf.String(), "\n"), true
}

func redactValue(v any) any {
	switch x := v.(type) {
	case map[string]any:
		for k, val := range x {
			if idKeyRe.MatchString(k) {
				x[k] = redactedValue
				continue
			}
			x[k] = redactValue(val)
		}
		return x
	case string:
		// o valor JÁ DECODIFICADO (sem as barras de escape do JSON): é aqui que "serial: SN\"12\\34" é redigido inteiro
		return redactTokens(x)
	case []any:
		for i := range x {
			x[i] = redactValue(x[i])
		}
		return x
	}
	return v
}

type span struct{ start, end int }

// redactTables mascara colunas de tabela cujo cabeçalho é um identificador: a saída do Format-Table (cabeçalho, linha
// de traços e dados) e as tabelas de colunas alinhadas dos utilitários (lsblk -o NAME,SERIAL).
func redactTables(s string) string {
	lines := strings.Split(s, "\n")
	for i := 0; i+1 < len(lines); i++ {
		header, next := lines[i], lines[i+1]
		if strings.TrimSpace(header) == "" || dashLine.MatchString(header) || strings.ContainsAny(header, "{}[]\"") {
			continue
		}
		var cols []span
		first := i + 2
		if dashLine.MatchString(next) {
			// Format-Table: as colunas começam onde começam os traços
			for _, m := range dashRun.FindAllStringIndex(next, -1) {
				cols = append(cols, span{m[0], m[1]})
			}
		} else {
			// colunas alinhadas sem traços: cada palavra do cabeçalho abre uma coluna (só se alguma é um identificador)
			if strings.ContainsAny(header, ":=") {
				continue
			}
			for _, m := range wordStart.FindAllStringIndex(header, -1) {
				cols = append(cols, span{m[0], m[1]})
			}
			first = i + 1
		}
		var ids []int
		for k, c := range cols {
			end := len(header)
			if k+1 < len(cols) {
				end = cols[k+1].start
			}
			if c.start >= len(header) {
				continue
			}
			if idKeyRe.MatchString(strings.TrimSpace(header[c.start:min(end, len(header))])) {
				ids = append(ids, k)
			}
		}
		if len(ids) == 0 || len(cols) < 2 && !dashLine.MatchString(next) {
			continue
		}
		for j := first; j < len(lines) && strings.TrimSpace(lines[j]) != ""; j++ {
			lines[j] = maskColumns(lines[j], cols, ids)
		}
	}
	return strings.Join(lines, "\n")
}

// maskColumns troca por [***] toda palavra da linha que toca uma coluna de identificador (a coluna vai de onde começa
// até onde a próxima começa; na última, até o fim). Por palavra, e não por posição: se os dados não alinharem
// exatamente com o cabeçalho, nenhum pedaço do identificador sobra. A palavra mascarada é preenchida com espaços até o
// tamanho original para as colunas seguintes não andarem.
func maskColumns(line string, cols []span, ids []int) string {
	rs := []rune(line)
	toks := wordStart.FindAllStringIndex(string(rs), -1)
	// FindAllStringIndex devolve posições em bytes; converte para runas
	pos := make([]span, 0, len(toks))
	for _, m := range toks {
		pos = append(pos, span{len([]rune(line[:m[0]])), len([]rune(line[:m[1]]))})
	}
	var out []rune
	last := 0
	for _, t := range pos {
		hit := false
		for _, k := range ids {
			end := len(rs)
			if k+1 < len(cols) && cols[k+1].start < end {
				end = cols[k+1].start
			}
			if t.start < end && t.end > cols[k].start {
				hit = true
				break
			}
		}
		if !hit {
			continue
		}
		out = append(out, rs[last:t.start]...)
		mask := []rune(redactedValue)
		for len(mask) < t.end-t.start {
			mask = append(mask, ' ')
		}
		out = append(out, mask...)
		last = t.end
	}
	out = append(out, rs[last:]...)
	return string(out)
}

package diagfmt

import "regexp"

// Redact remove de um texto cru (JSON do system_profiler, saída do PowerShell, /proc...) os identificadores da
// máquina: número de série, UUID/GUID, UDID, ECID, WWN, endereço MAC, IMEI e afins. Roda ANTES de a saída original
// aparecer em qualquer lugar (modo detalhes, falha do comando, log do smoke), inclusive com JSON truncado ou inválido:
// por isso trabalha por padrões de texto e não por parse. O valor vira "[***]"; a chave continua visível.
const redactedValue = "[***]"

const idKeys = `serial|uuid|guid|udid|ecid|wwn|nguid|eui-?64|imei|meid|mac[ _-]?addr|ethernet[ _-]?addr|hardware[ _-]?id|machine[ _-]?id|platform[ _-]?id|device[ _-]?id|unique[ _-]?id|volume[ _-]?id|provisioning`

var (
	// "chave com identificador": valor (string, inclusive truncada no fim da entrada, número ou booleano)
	redactJSON = regexp.MustCompile(`(?i)("[^"\n]*(?:` + idKeys + `)[^"\n]*"\s*:\s*)(?:"[^"]*(?:"|\z)|[^,}\]\s][^,}\]\n]*)`)
	// linhas "Chave: valor" ou "Chave = valor" (a chave não tem aspas nem chaves: o JSON é da regra de cima)
	redactLine = regexp.MustCompile(`(?im)^([ \t]*[^\n:="{}\[\]]*(?:` + idKeys + `)[^\n:="{}\[\]]*[:=][ \t]*)\S.*$`)
	redactUUID = regexp.MustCompile(`\b[0-9A-Fa-f]{8}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{12}\b`)
	redactMAC  = regexp.MustCompile(`\b(?:[0-9A-Fa-f]{2}[:-]){5}[0-9A-Fa-f]{2}\b`)
)

// Redact devolve s sem os identificadores da máquina.
func Redact(s string) string {
	s = redactJSON.ReplaceAllString(s, `${1}"`+redactedValue+`"`)
	s = redactLine.ReplaceAllString(s, `${1}`+redactedValue)
	s = redactUUID.ReplaceAllString(s, redactedValue)
	return redactMAC.ReplaceAllString(s, redactedValue)
}

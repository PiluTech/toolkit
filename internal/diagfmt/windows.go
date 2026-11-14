package diagfmt

import (
	"encoding/json"
	"errors"
	"math"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

// Renderizadores do Windows: recebem o JSON que a receita diag-memoria emite (ConvertTo-Json) e devolvem o resumo.
// Nunca entram em pânico: entrada irreconhecível devolve erro e o chamador mostra a saída original.

func init() {
	Register("windows-memory", winMemory)
	Register("windows-disks", winDisks)
}

var (
	errWinEmpty   = errors.New("saída vazia")
	errWinUnknown = errors.New("saída não reconhecida")
)

// winDecode lê o JSON (tolera BOM e espaços). Vazio ou inválido = erro.
func winDecode(raw string) (any, error) {
	s := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(raw), "\ufeff"))
	if s == "" {
		return nil, errWinEmpty
	}
	var v any
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		return nil, err
	}
	return v, nil
}

// winObjects aceita lista ou objeto único (o ConvertTo-Json do PowerShell 5.1 achata lista de 1 item) e devolve só
// os objetos; null, números e textos soltos são ignorados.
func winObjects(v any) []map[string]any {
	switch x := v.(type) {
	case map[string]any:
		return []map[string]any{x}
	case []any:
		var out []map[string]any
		for _, e := range x {
			if m, ok := e.(map[string]any); ok {
				out = append(out, m)
			}
		}
		return out
	}
	return nil
}

// winNum lê número ou texto numérico; nulo, negativo, NaN e infinito viram 0.
func winNum(v any) float64 {
	var f float64
	switch x := v.(type) {
	case float64:
		f = x
	case string:
		f, _ = strconv.ParseFloat(strings.TrimSpace(x), 64)
	}
	if math.IsNaN(f) || math.IsInf(f, 0) || f < 0 {
		return 0
	}
	return f
}

// winUint converte para uint64 sem estourar.
func winUint(v any) uint64 {
	f := winNum(v)
	if f >= 1.8e19 {
		return math.MaxUint64
	}
	return uint64(f)
}

// winStr lê texto (ou número) e apara; qualquer outro tipo vira vazio.
func winStr(v any) string {
	switch x := v.(type) {
	case string:
		return strings.TrimSpace(x)
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	}
	return ""
}

// winStrs lê texto ou lista de textos (OperationalStatus é lista).
func winStrs(v any) []string {
	if l, ok := v.([]any); ok {
		var out []string
		for _, e := range l {
			if s := winStr(e); s != "" {
				out = append(out, s)
			}
		}
		return out
	}
	var out []string
	for _, s := range strings.Split(winStr(v), ",") {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// winNatKey ordena DIMM2 antes de DIMM10 (números com 6 dígitos).
func winNatKey(s string) string {
	var b strings.Builder
	rs := []rune(strings.ToLower(s))
	for i := 0; i < len(rs); {
		if unicode.IsDigit(rs[i]) {
			j := i
			for j < len(rs) && unicode.IsDigit(rs[j]) {
				j++
			}
			d := string(rs[i:j])
			if len(d) < 6 {
				b.WriteString(strings.Repeat("0", 6-len(d)))
			}
			b.WriteString(d)
			i = j
			continue
		}
		b.WriteRune(rs[i])
		i++
	}
	return b.String()
}

// ---- memória ----

// winSMBIOS: tipos da tabela SMBIOS (DSP0134, Type 17, "Memory Device", campo Memory Type), em decimal. 21 a 23 são
// RESERVADOS (sem nome): o tipo SMBIOS 20 é DDR2 FB-DIMM, não DDR. É outra tabela que a do campo legado MemoryType do
// WMI (winWMIType): os números coincidem só em parte e não podem ser misturados.
var winSMBIOS = map[int]string{
	18: "DDR", 19: "DDR2", 20: "DDR2 FB-DIMM", 24: "DDR3", 25: "FB-DIMM", 26: "DDR4", 27: "LPDDR", 28: "LPDDR2",
	29: "LPDDR3", 30: "LPDDR4", 32: "HBM", 33: "HBM2", 34: "DDR5", 35: "LPDDR5", 36: "HBM3",
}

// winWMIType: Win32_PhysicalMemory.MemoryType (campo legado do WMI, usado só quando o SMBIOSMemoryType vem 0):
// 20 = DDR, 21 = DDR2, 22 = DDR2 FB-DIMM, 24 = DDR3, 25 = FB-DIMM. Os demais valores (DRAM, SDRAM...) não dizem a
// geração e ficam sem nome.
var winWMIType = map[int]string{20: "DDR", 21: "DDR2", 22: "DDR2 FB-DIMM", 24: "DDR3", 25: "FB-DIMM"}

// winJEDEC: códigos JEDEC (JEP106) comuns, byte de continuação + id com paridade, em hexadecimal maiúsculo.
var winJEDEC = map[string]string{
	"80AD": "SK hynix",
	"80CE": "Samsung",
	"802C": "Micron",
	"859B": "Crucial",
	"0198": "Kingston",
	"029E": "Corsair",
	"04CB": "A-DATA",
	"04EF": "Team Group",
	"04CD": "G.Skill",
	"830B": "Nanya",
}

func winIsHex(s string) bool {
	if s == "" {
		return false
	}
	digit := false
	for _, r := range s {
		switch {
		case r >= '0' && r <= '9':
			digit = true
		case r >= 'A' && r <= 'F':
		default:
			return false
		}
	}
	return digit
}

// winMaker decodifica o Manufacturer do WMI: código JEDEC ("80AD", "80AD000000000000", "079E000080AD") vira o nome
// da tabela; código fora da tabela aparece como "JEDEC <hex>" (zeros finais removidos); nome legível fica como veio;
// vazio, "Unknown" e zeros omitem (devolve "").
func winMaker(raw string) string {
	s := strings.TrimSpace(raw)
	switch strings.ToLower(s) {
	case "", "unknown", "undefined", "not specified", "to be filled by o.e.m.", "oem":
		return ""
	}
	up := strings.ToUpper(strings.ReplaceAll(s, " ", ""))
	if !winIsHex(up) {
		return s
	}
	if strings.Trim(up, "0") == "" {
		return ""
	}
	// Um código JEDEC termina em zero legitimamente (8010 é um código, não 0801 com padding): o código desconhecido
	// aparece como veio. Só o preenchimento PROVADO sai: o campo de 8 bytes (16 dígitos) com um código de 4 dígitos
	// seguido de 12 zeros ("80AD000000000000").
	code := up
	if len(up) == 16 && up[4:] == strings.Repeat("0", 12) {
		code = up[:4]
	}
	cands := []string{up, code}
	if len(up) > 4 {
		cands = append(cands, up[len(up)-4:]) // "079E000080AD": bytes de continuação + id
		if strings.Trim(up[4:], "0") == "" {
			cands = append(cands, up[:4]) // id seguido só de zeros
		}
	}
	for _, c := range cands {
		if n, ok := winJEDEC[c]; ok {
			return n
		}
	}
	return "JEDEC " + code
}

type winModule struct {
	locator string
	maker   string
	part    string
	size    uint64
	typ     string
	speed   uint64
}

func (m winModule) typeSpeed(l Lang) string {
	var p []string
	if m.typ != "" {
		p = append(p, m.typ)
	}
	if m.speed > 0 {
		p = append(p, strconv.FormatUint(m.speed, 10)+" "+T(l, "MT/s", "MT/s", "MT/s"))
	}
	return strings.Join(p, " ")
}

func winMemory(raw string, l Lang) (string, error) {
	v, err := winDecode(raw)
	if err != nil {
		return "", err
	}
	root, ok := v.(map[string]any)
	if !ok {
		return "", errWinUnknown
	}
	_, hasMod := root["modules"]
	_, hasOS := root["os"]
	_, hasArr := root["arrays"]
	if !hasMod && !hasOS && !hasArr {
		return "", errWinUnknown
	}

	var mods []winModule
	for _, o := range winObjects(root["modules"]) {
		size := winUint(o["Capacity"])
		if size == 0 {
			continue // slot vazio
		}
		// SMBIOSMemoryType (tabela DSP0134) manda; só se vier 0 ("não informado") vale o MemoryType legado do WMI, que
		// é outra tabela. Um SMBIOS reservado ou desconhecido (21 a 23, 99) deixa o tipo sem nome em vez de cair no WMI.
		var typ string
		if smb := int(winNum(o["SMBIOSMemoryType"])); smb != 0 {
			typ = winSMBIOS[smb]
		} else {
			typ = winWMIType[int(winNum(o["MemoryType"]))]
		}
		speed := winUint(o["ConfiguredClockSpeed"])
		if speed == 0 {
			speed = winUint(o["Speed"])
		}
		loc := winStr(o["DeviceLocator"])
		if loc == "" {
			loc = winStr(o["BankLabel"])
		}
		mods = append(mods, winModule{
			locator: loc, maker: winMaker(winStr(o["Manufacturer"])), part: winStr(o["PartNumber"]),
			size: size, typ: typ, speed: speed,
		})
	}
	sort.SliceStable(mods, func(i, j int) bool { return winNatKey(mods[i].locator) < winNatKey(mods[j].locator) })

	var slots uint64
	for _, o := range winObjects(root["arrays"]) {
		slots += winUint(o["MemoryDevices"])
	}

	var inUse string
	if os := winObjects(root["os"]); len(os) > 0 {
		totalKB, freeKB := winNum(os[0]["TotalVisibleMemorySize"]), winNum(os[0]["FreePhysicalMemory"])
		if totalKB > 0 {
			if freeKB > totalKB {
				freeKB = totalKB
			}
			const gb = 1024 * 1024
			inUse = T(l, "Em uso agora: ", "In use now: ", "En uso ahora: ") +
				Number((totalKB-freeKB)/gb, 1, l) + T(l, " de ", " of ", " de ") + Number(totalKB/gb, 1, l) +
				" GB (" + Percent((totalKB-freeKB)/totalKB) + ")"
		}
	}

	if len(mods) == 0 && inUse == "" {
		return "", errWinUnknown
	}

	var total uint64
	same := true
	for i, m := range mods {
		total += m.size
		if i > 0 && (m.typ != mods[0].typ || m.speed != mods[0].speed) {
			same = false
		}
	}

	var head strings.Builder
	if len(mods) == 0 {
		head.WriteString(T(l, "Memória: nenhum módulo identificado", "Memory: no module identified", "Memoria: ningún módulo identificado"))
	} else {
		head.WriteString(T(l, "Memória: ", "Memory: ", "Memoria: ") + Bytes(total, true, l) +
			T(l, " no total", " total", " en total"))
		if slots >= uint64(len(mods)) {
			n := strconv.Itoa(len(mods))
			if slots == 1 {
				head.WriteString(" (" + n + T(l, " de 1 slot usado)", " of 1 slot used)", " de 1 ranura usada)"))
			} else {
				head.WriteString(" (" + n + T(l, " de ", " of ", " de ") + strconv.FormatUint(slots, 10) +
					T(l, " slots usados)", " slots used)", " ranuras usadas)"))
			}
		}
		if same {
			if ts := mods[0].typeSpeed(l); ts != "" {
				head.WriteString(" · " + ts)
			}
		}
	}

	lines := []string{head.String()}
	for i, m := range mods {
		p := []string{T(l, "  Slot ", "  Slot ", "  Ranura ") + strconv.Itoa(i+1) + ": " + Bytes(m.size, true, l)}
		if !same {
			if ts := m.typeSpeed(l); ts != "" {
				p = append(p, ts)
			}
		}
		if m.maker != "" {
			p = append(p, m.maker)
		}
		if m.part != "" {
			p = append(p, m.part)
		}
		lines = append(lines, strings.Join(p, " · "))
	}
	if inUse != "" {
		lines = append(lines, inUse)
	}
	return strings.Join(lines, "\n"), nil
}

// ---- discos ----

var (
	winMediaNum = map[int]string{3: "HDD", 4: "SSD", 5: "SCM"}
	winBusNum   = map[int]string{1: "SCSI", 2: "ATAPI", 3: "ATA", 4: "1394", 5: "SSA", 6: "Fibre Channel", 7: "USB", 8: "RAID",
		9: "iSCSI", 10: "SAS", 11: "SATA", 12: "SD", 13: "MMC", 15: "File-Backed Virtual", 16: "Storage Spaces", 17: "NVMe"}
	winHealthNum = map[int]string{0: "Healthy", 1: "Warning", 2: "Unhealthy"}
)

// winEnum lê um enum do PowerShell que pode vir como nome ou como número.
func winEnum(v any, names map[int]string) string {
	if f, ok := v.(float64); ok {
		return names[int(f)]
	}
	return winStr(v)
}

func winHealth(s string, l Lang) string {
	switch strings.ToLower(s) {
	case "healthy":
		return T(l, "saudável", "healthy", "saludable")
	case "warning":
		return T(l, "atenção", "warning", "atención")
	case "unhealthy":
		return T(l, "com problema", "unhealthy", "con problema")
	}
	return s
}

func winDisks(raw string, l Lang) (string, error) {
	v, err := winDecode(raw)
	if err != nil {
		return "", err
	}
	var objs []map[string]any
	switch x := v.(type) {
	case nil:
	case []any:
		objs = winObjects(x)
		if len(x) > 0 && len(objs) == 0 {
			return "", errWinUnknown
		}
	case map[string]any:
		known := false
		for _, k := range []string{"FriendlyName", "Size", "MediaType", "HealthStatus", "BusType"} {
			if _, ok := x[k]; ok {
				known = true
			}
		}
		if !known {
			return "", errWinUnknown
		}
		objs = []map[string]any{x}
	default:
		return "", errWinUnknown
	}
	if len(objs) == 0 {
		return T(l, "Nenhum disco encontrado", "No disk found", "Ningún disco encontrado"), nil
	}

	label := T(l, "Disco: ", "Disk: ", "Disco: ")
	var lines []string
	for _, o := range objs {
		name := winStr(o["FriendlyName"])
		if name == "" {
			name = T(l, "Disco sem nome", "Unnamed disk", "Disco sin nombre")
		}
		parts := []string{label + name}

		media := winEnum(o["MediaType"], winMediaNum)
		bus := winEnum(o["BusType"], winBusNum)
		switch strings.ToLower(media) {
		case "", "unspecified", "unknown", "0":
			media = ""
		}
		if media == "" && bus != "" && !strings.EqualFold(bus, "unknown") {
			if strings.EqualFold(bus, "nvme") {
				media = "SSD NVMe"
			} else {
				media = bus
			}
		}
		if media != "" {
			parts = append(parts, media)
		}
		if size := winUint(o["Size"]); size > 0 {
			parts = append(parts, Bytes(size, false, l))
		}
		if h := winEnum(o["HealthStatus"], winHealthNum); h != "" {
			parts = append(parts, winHealth(h, l))
		}
		var bad []string
		for _, s := range winStrs(o["OperationalStatus"]) {
			if !strings.EqualFold(s, "ok") {
				bad = append(bad, s)
			}
		}
		if len(bad) > 0 {
			parts = append(parts, T(l, "estado: ", "status: ", "estado: ")+strings.Join(bad, ", "))
		}
		lines = append(lines, strings.Join(parts, " · "))
	}
	return strings.Join(lines, "\n"), nil
}

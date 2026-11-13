package diagfmt

import (
	"encoding/json"
	"errors"
	"regexp"
	"strconv"
	"strings"
)

func init() {
	Register("macos-memory", macMemory)
	Register("macos-disks", macDisks)
}

var (
	errMacMemory = errors.New("saída de memória do macOS não reconhecida")
	errMacDisks  = errors.New("saída de discos do macOS não reconhecida")

	macPageSizeRe = regexp.MustCompile(`page size of (\d+) bytes`)
	macVMLineRe   = regexp.MustCompile(`^\s*([^:]+):\s*(\d+)\.?\s*$`)
)

// macSections separa a saída da receita em seções marcadas por linhas "### nome".
func macSections(raw string) map[string]string {
	out := map[string]string{}
	name := ""
	var b strings.Builder
	flush := func() {
		if name != "" {
			out[name] = b.String()
		}
		b.Reset()
	}
	for _, line := range strings.Split(strings.ReplaceAll(raw, "\r\n", "\n"), "\n") {
		if rest, ok := strings.CutPrefix(line, "### "); ok {
			flush()
			name = strings.TrimSpace(rest)
			continue
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
	flush()
	return out
}

// macUsed devolve a memória em uso do vm_stat. APROXIMAÇÃO: em uso = (páginas ativas + travadas/wired + ocupadas pelo
// compressor) * tamanho da página (o da linha "page size of N bytes": 16384 no Apple Silicon, 4096 no Intel). Não conta
// inativas, especulativas nem arquivos em cache, que o macOS devolve sob pressão; fica perto do "Memória usada" do
// Monitor de Atividade. ok=false quando o vm_stat não traz o tamanho da página ou nenhuma contagem.
func macUsed(vm string) (uint64, bool) {
	m := macPageSizeRe.FindStringSubmatch(vm)
	if m == nil {
		return 0, false
	}
	page, err := strconv.ParseUint(m[1], 10, 64)
	if err != nil || page == 0 {
		return 0, false
	}
	var pages uint64
	found := false
	for _, line := range strings.Split(vm, "\n") {
		lm := macVMLineRe.FindStringSubmatch(line)
		if lm == nil {
			continue
		}
		switch strings.TrimSpace(lm[1]) {
		case "Pages active", "Pages wired down", "Pages occupied by compressor":
			n, err := strconv.ParseUint(lm[2], 10, 64)
			if err != nil {
				continue
			}
			pages += n
			found = true
		}
	}
	if !found {
		return 0, false
	}
	return pages * page, true
}

// macMemType lê o tipo da memória do system_profiler: Apple Silicon traz dimm_type no próprio item (memória unificada,
// sem pentes); Intel traz os pentes em _items (vazios saem). unified=true só no primeiro caso.
func macMemType(profiler string) (typ string, unified bool) {
	var doc map[string]any
	if json.Unmarshal([]byte(profiler), &doc) != nil {
		return "", false
	}
	items, _ := doc["SPMemoryDataType"].([]any)
	var types []string
	seen := map[string]bool{}
	add := func(v any) {
		s, _ := v.(string)
		s = strings.TrimSpace(s)
		if s == "" || strings.EqualFold(s, "empty") || seen[s] {
			return
		}
		seen[s] = true
		types = append(types, s)
	}
	for _, it := range items {
		obj, _ := it.(map[string]any)
		if obj == nil {
			continue
		}
		subs, hasSubs := obj["_items"].([]any)
		if !hasSubs {
			before := len(types)
			add(obj["dimm_type"])
			if len(types) > before {
				unified = true
			}
			continue
		}
		for _, s := range subs {
			if so, _ := s.(map[string]any); so != nil {
				add(so["dimm_type"])
			}
		}
	}
	return strings.Join(types, "/"), unified && len(types) == 1
}

func macMemory(raw string, l Lang) (string, error) {
	sec := macSections(raw)
	total, err := strconv.ParseUint(strings.TrimSpace(sec["memsize"]), 10, 64)
	if err != nil || total == 0 {
		return "", errMacMemory
	}
	head := T(l, "Memória: ", "Memory: ", "Memoria: ") + Bytes(total, true, l) +
		T(l, " no total", " total", " en total")
	if typ, unified := macMemType(sec["profiler"]); typ != "" {
		head += " · " + typ
		if unified {
			head += " " + T(l, "(unificada)", "(unified)", "(unificada)")
		}
	}
	lines := []string{head}
	if used, ok := macUsed(sec["vm_stat"]); ok {
		if used > total {
			used = total
		}
		tb := Bytes(total, true, l)
		ub := Bytes(used, true, l)
		// "9,8 de 16 GB": a unidade só aparece uma vez quando é a mesma
		if ui, ti := strings.LastIndexByte(ub, ' '), strings.LastIndexByte(tb, ' '); ui >= 0 && ti >= 0 && ub[ui:] == tb[ti:] {
			ub = ub[:ui]
		}
		lines = append(lines, T(l, "Em uso agora: ", "In use now: ", "En uso ahora: ")+ub+
			T(l, " de ", " of ", " de ")+tb+" ("+Percent(float64(used)/float64(total))+")")
	}
	return strings.Join(lines, "\n"), nil
}

// macNum aceita número JSON ou texto numérico.
func macNum(v any) (uint64, bool) {
	switch x := v.(type) {
	case float64:
		if x > 0 && x < 1e18 {
			return uint64(x), true
		}
	case string:
		if n, err := strconv.ParseUint(strings.TrimSpace(x), 10, 64); err == nil && n > 0 {
			return n, true
		}
	}
	return 0, false
}

func macStr(v any) string {
	s, _ := v.(string)
	return strings.TrimSpace(s)
}

type macDisk struct {
	name, medium, smart string
	size                uint64
}

func macMedium(s string) string {
	switch strings.ToLower(s) {
	case "ssd", "solid state":
		return "SSD"
	case "hdd", "rotational":
		return "HDD"
	}
	return ""
}

func macSmart(s string, l Lang) string {
	switch strings.ToLower(s) {
	case "verified":
		return T(l, "verificado", "verified", "verificado")
	case "failing":
		return T(l, "com problema", "failing", "con problemas")
	case "not supported":
		return T(l, "não suportado", "not supported", "no compatible")
	}
	return s
}

func macDisks(raw string, l Lang) (string, error) {
	var doc map[string]any
	if json.Unmarshal([]byte(raw), &doc) != nil || doc == nil {
		return "", errMacDisks
	}
	var disks []macDisk
	seen := map[string]bool{}
	add := func(d macDisk) {
		if d.name == "" || seen[d.name] {
			return
		}
		seen[d.name] = true
		disks = append(disks, d)
	}
	// NVMe e SATA: um item por disco físico, dentro de _items de cada controlador
	for _, key := range []string{"SPNVMeDataType", "SPSerialATADataType"} {
		ctrls, _ := doc[key].([]any)
		for _, c := range ctrls {
			co, _ := c.(map[string]any)
			subs, _ := co["_items"].([]any)
			for _, s := range subs {
				so, _ := s.(map[string]any)
				if so == nil {
					continue
				}
				name := macStr(so["device_model"])
				if name == "" {
					name = macStr(so["_name"])
				}
				d := macDisk{name: name, smart: macStr(so["smart_status"])}
				d.size, _ = macNum(so["size_in_bytes"])
				if key == "SPNVMeDataType" {
					d.medium = "SSD"
				} else {
					d.medium = macMedium(macStr(so["spsata_medium_type"]))
				}
				add(d)
			}
		}
	}
	// Storage: volumes com physical_drive; só completa o que NVMe/SATA não trouxeram (sem tamanho do disco)
	vols, _ := doc["SPStorageDataType"].([]any)
	for _, v := range vols {
		vo, _ := v.(map[string]any)
		pd, _ := vo["physical_drive"].(map[string]any)
		if pd == nil || strings.EqualFold(macStr(pd["protocol"]), "Disk Image") {
			continue
		}
		add(macDisk{name: macStr(pd["device_name"]), medium: macMedium(macStr(pd["medium_type"])), smart: macStr(pd["smart_status"])})
	}
	if len(disks) == 0 {
		return T(l, "Nenhum disco encontrado", "No disks found", "No se encontraron discos"), nil
	}
	label := T(l, "Disco: ", "Disk: ", "Disco: ")
	lines := make([]string, 0, len(disks))
	for _, d := range disks {
		parts := []string{d.name}
		if d.medium != "" {
			parts = append(parts, d.medium)
		}
		if d.size > 0 {
			parts = append(parts, Bytes(d.size, false, l))
		}
		if d.smart != "" {
			parts = append(parts, "S.M.A.R.T.: "+macSmart(d.smart, l))
		}
		lines = append(lines, label+strings.Join(parts, " · "))
	}
	return strings.Join(lines, "\n"), nil
}

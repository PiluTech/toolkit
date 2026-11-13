package diagfmt

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

func init() {
	Register("linux-memory", linuxRenderMemory)
	Register("linux-disks", linuxRenderDisks)
}

// linuxMeminfo lê as linhas "Chave: valor kB" de /proc/meminfo e devolve bytes.
func linuxMeminfo(raw string) map[string]uint64 {
	out := map[string]uint64{}
	for _, line := range strings.Split(raw, "\n") {
		k, v, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		f := strings.Fields(v)
		if len(f) == 0 {
			continue
		}
		n, err := strconv.ParseUint(f[0], 10, 64)
		if err != nil || n > 1<<50 {
			continue
		}
		mult := uint64(1)
		if len(f) > 1 && strings.EqualFold(f[1], "kB") {
			mult = 1024
		}
		out[strings.TrimSpace(k)] = n * mult
	}
	return out
}

// linuxUsedOf formata "6,1 de 15,6 GB": tira a unidade do primeiro quando é a mesma do segundo.
func linuxUsedOf(used, total uint64, l Lang) string {
	u, t := Bytes(used, true, l), Bytes(total, true, l)
	uv, uu, _ := strings.Cut(u, " ")
	if _, tu, ok := strings.Cut(t, " "); ok && tu == uu {
		u = uv
	}
	return u + T(l, " de ", " of ", " de ") + t
}

// linuxRenderMemory: MemAvailable ausente (kernel antigo) usa MemFree+Buffers+Cached; sem nenhum dos dois, não mostra uso.
func linuxRenderMemory(raw string, l Lang) (string, error) {
	m := linuxMeminfo(raw)
	total := m["MemTotal"]
	if total == 0 {
		return "", errors.New("MemTotal ausente")
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s: %s %s\n", T(l, "Memória", "Memory", "Memoria"), Bytes(total, true, l), T(l, "no total", "total", "en total"))
	avail, ok := m["MemAvailable"]
	if !ok {
		if free, okF := m["MemFree"]; okF {
			avail, ok = free+m["Buffers"]+m["Cached"], true
		}
	}
	if ok {
		if avail > total {
			avail = total
		}
		used := total - avail
		fmt.Fprintf(&b, "%s: %s (%s)\n", T(l, "Em uso agora", "In use now", "En uso ahora"), linuxUsedOf(used, total, l), Percent(float64(used)/float64(total)))
	}
	if st := m["SwapTotal"]; st > 0 {
		sf := m["SwapFree"]
		if sf > st {
			sf = st
		}
		fmt.Fprintf(&b, "Swap: %s %s\n", linuxUsedOf(st-sf, st, l), T(l, "em uso", "in use", "en uso"))
	} else {
		b.WriteString(T(l, "Sem swap", "No swap", "Sin swap") + "\n")
	}
	b.WriteString(T(l, "detalhes dos pentes exigem administrador; não mostrados",
		"memory module details require administrator; not shown",
		"los detalles de los módulos exigen administrador; no se muestran"))
	return b.String(), nil
}

type linuxLsblkOut struct {
	Devices json.RawMessage `json:"blockdevices"`
}

// linuxStr aceita string, número, bool ou null.
func linuxStr(raw json.RawMessage) string {
	var v any
	if json.Unmarshal(raw, &v) != nil {
		return ""
	}
	switch x := v.(type) {
	case string:
		return strings.TrimSpace(x)
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(x)
	}
	return ""
}

func linuxRenderDisks(raw string, l Lang) (string, error) {
	var top linuxLsblkOut
	if err := json.Unmarshal([]byte(raw), &top); err != nil {
		return "", fmt.Errorf("JSON do lsblk inválido: %w", err)
	}
	var devs []map[string]json.RawMessage
	if err := json.Unmarshal(top.Devices, &devs); err != nil {
		return "", errors.New("blockdevices ausente ou inválido")
	}
	var lines []string
	for _, d := range devs {
		if d == nil || linuxStr(d["type"]) != "disk" {
			continue
		}
		name := linuxStr(d["model"])
		if name == "" {
			name = linuxStr(d["name"])
		}
		if name == "" {
			name = T(l, "sem nome", "unnamed", "sin nombre")
		}
		parts := []string{name}
		switch strings.ToLower(linuxStr(d["rota"])) {
		case "0", "false":
			parts = append(parts, "SSD")
		case "1", "true":
			parts = append(parts, "HDD")
		}
		if strings.EqualFold(linuxStr(d["tran"]), "nvme") {
			parts = append(parts, "NVMe")
		}
		if sz, err := strconv.ParseUint(linuxStr(d["size"]), 10, 64); err == nil && sz > 0 {
			parts = append(parts, Bytes(sz, false, l))
		}
		lines = append(lines, T(l, "Disco", "Disk", "Disco")+": "+strings.Join(parts, " · "))
	}
	if len(lines) == 0 {
		return "", errors.New("nenhum disco encontrado")
	}
	lines = append(lines, T(l, "saúde SMART exige administrador; não verificada",
		"SMART health requires administrator; not checked",
		"la salud SMART exige administrador; no verificada"))
	return strings.Join(lines, "\n"), nil
}

package diagnostico

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/PiluTech/toolkit/internal/diagfmt"
)

// Caminhos absolutos dos utilitários do macOS (nunca resolvidos pelo PATH).
const (
	macSysctl   = "/usr/sbin/sysctl"
	macProfiler = "/usr/sbin/system_profiler"
	macVMStat   = "/usr/bin/vm_stat"
	macLog      = "/usr/bin/log"
	macPmset    = "/usr/bin/pmset"
)

const (
	macLogMaxBytes   = 2 << 20 // teto lido da saída do `log show`
	macMaxEvents     = 30
	macMaxCrashes    = 50
	macCrashDays     = 30
	macMaxMessageLen = 200
)

var macDirs = []string{"/Library/Logs/DiagnosticReports"} // a pasta do usuário entra com Home()

// collectDarwin preenche r com o estado da máquina (macOS). Só leitura, sem root: o que exige root vira Skipped.
func collectDarwin(ctx context.Context, d Deps, r *Report) {
	skip := func(area, pt, en, es string) {
		r.Skipped = append(r.Skipped, Skipped{Area: area, Reason: diagfmt.T(d.Lang, pt, en, es)})
	}
	defer func() { // última barreira: um pânico inesperado nunca derruba o diagnóstico
		if p := recover(); p != nil {
			skip("sistema", "falha inesperada na leitura do macOS", "unexpected failure reading macOS", "fallo inesperado al leer macOS")
		}
	}()
	area := func(f func()) {
		defer func() {
			if p := recover(); p != nil {
				skip("sistema", "falha inesperada na leitura de uma área", "unexpected failure reading an area", "fallo inesperado al leer un área")
			}
		}()
		f()
	}

	area(func() {
		if err := macHardware(ctx, d, r); err != nil {
			skip("hardware", "não foi possível ler o hardware (sysctl/system_profiler)", "could not read the hardware (sysctl/system_profiler)", "no se pudo leer el hardware (sysctl/system_profiler)")
		}
	})
	area(func() {
		if err := macMemory(ctx, d, r); err != nil {
			skip("memoria", "não foi possível ler a memória (sysctl hw.memsize e vm_stat)", "could not read memory (sysctl hw.memsize and vm_stat)", "no se pudo leer la memoria (sysctl hw.memsize y vm_stat)")
		}
	})
	area(func() {
		if err := macDisks(ctx, d, r); err != nil {
			skip("discos", "não foi possível ler os discos (system_profiler SPNVMeDataType SPSerialATADataType SPStorageDataType)", "could not read the disks (system_profiler SPNVMeDataType SPSerialATADataType SPStorageDataType)", "no se pudieron leer los discos (system_profiler SPNVMeDataType SPSerialATADataType SPStorageDataType)")
		}
	})
	area(func() {
		if err := macBattery(ctx, d, r); err != nil {
			skip("bateria", "não foi possível ler a bateria (system_profiler SPPowerDataType)", "could not read the battery (system_profiler SPPowerDataType)", "no se pudo leer la batería (system_profiler SPPowerDataType)")
		}
	})
	skip("temperatura",
		"a temperatura exige administrador; rode 'sudo powermetrics --samplers smc -n 1' para ver",
		"temperature needs administrator rights; run 'sudo powermetrics --samplers smc -n 1' to see it",
		"la temperatura exige administrador; ejecute 'sudo powermetrics --samplers smc -n 1' para verla")
	area(func() {
		if err := macGPU(ctx, d, r); err != nil {
			skip("gpu", "não foi possível ler a GPU (system_profiler SPDisplaysDataType)", "could not read the GPU (system_profiler SPDisplaysDataType)", "no se pudo leer la GPU (system_profiler SPDisplaysDataType)")
		}
	})
	area(func() {
		if err := macEvents(ctx, d, r); err != nil {
			skip("eventos", "não foi possível ler o log do sistema (log show --last 6h); tente de novo ou rode no Terminal", "could not read the system log (log show --last 6h); try again or run it in Terminal", "no se pudo leer el registro del sistema (log show --last 6h); reintente o ejecútelo en Terminal")
		}
	})
	area(func() {
		if err := macCrashes(d, r); err != nil {
			skip("falhas", "não foi possível listar os relatórios de falha (DiagnosticReports)", "could not list the crash reports (DiagnosticReports)", "no se pudieron listar los informes de fallos (DiagnosticReports)")
		}
	})
}

// --- hardware ---------------------------------------------------------------------------------------------------

func macHardware(ctx context.Context, d Deps, r *Report) error {
	ok := false
	if out, err := d.run(ctx, readCmd(macSysctl, "-n", "hw.model")); err == nil {
		if m := firstLine(out); m != "" {
			r.Hardware.Model = macClean(m, 80)
			ok = true
		}
	}
	if out, err := d.run(ctx, readCmd(macSysctl, "-n", "machdep.cpu.brand_string")); err == nil {
		if m := firstLine(out); m != "" {
			r.Hardware.CPU = macClean(m, 80)
			ok = true
		}
	}
	if n, ok2 := macSysctlInt(ctx, d, "hw.physicalcpu"); ok2 {
		r.Hardware.Cores, ok = n, true
	}
	if n, ok2 := macSysctlInt(ctx, d, "hw.logicalcpu"); ok2 {
		r.Hardware.Threads, ok = n, true
	}
	if out, err := d.run(ctx, readCmd(macSysctl, "-n", "kern.boottime")); err == nil {
		if sec, found := macBootSec(out); found {
			if up := d.now().Sub(time.Unix(sec, 0)); up > 0 {
				r.Hardware.Uptime = up
				ok = true
			}
		}
	}
	if out, err := d.run(ctx, readCmd(macProfiler, "SPHardwareDataType", "-json")); err == nil {
		var doc struct {
			Items []struct {
				ChipType    any `json:"chip_type"`
				MachineName any `json:"machine_name"`
				Processors  any `json:"number_processors"`
			} `json:"SPHardwareDataType"`
		}
		if json.Unmarshal([]byte(out), &doc) == nil && len(doc.Items) > 0 {
			it := doc.Items[0]
			if chip := macStr(it.ChipType); chip != "" {
				r.Hardware.CPU = macClean(chip, 80) // Apple Silicon: o brand_string do sysctl não existe
				ok = true
			}
			if name := macStr(it.MachineName); name != "" {
				if r.Hardware.Vendor == "" {
					r.Hardware.Vendor = "Apple"
				}
				if r.Hardware.Model == "" {
					r.Hardware.Model = macClean(name, 80)
				}
				ok = true
			}
			// "proc 10:4:6:0" = total:desempenho:eficiência:...
			if p := macStr(it.Processors); r.Hardware.Threads == 0 && strings.HasPrefix(p, "proc ") {
				if n, err := strconv.Atoi(strings.Split(strings.TrimPrefix(p, "proc "), ":")[0]); err == nil && n > 0 {
					r.Hardware.Threads = n
					ok = true
				}
			}
		}
	}
	if ok && r.Hardware.Vendor == "" {
		r.Hardware.Vendor = "Apple"
	}
	if !ok {
		return fmt.Errorf("sem dados de hardware")
	}
	return nil
}

func macSysctlInt(ctx context.Context, d Deps, key string) (int, bool) {
	out, err := d.run(ctx, readCmd(macSysctl, "-n", key))
	if err != nil {
		return 0, false
	}
	n, err := strconv.Atoi(firstLine(out))
	if err != nil || n <= 0 {
		return 0, false
	}
	return n, true
}

var macBootRe = regexp.MustCompile(`sec\s*=\s*(\d+)`)

func macBootSec(s string) (int64, bool) {
	m := macBootRe.FindStringSubmatch(s)
	if m == nil {
		return 0, false
	}
	n, err := strconv.ParseInt(m[1], 10, 64)
	return n, err == nil && n > 0
}

// --- memória ----------------------------------------------------------------------------------------------------

var (
	macPageSizeRe = regexp.MustCompile(`page size of (\d+) bytes`)
	macVMLineRe   = regexp.MustCompile(`^([A-Za-z" -]+?):\s+(\d+)\.?\s*$`)
	macSwapRe     = regexp.MustCompile(`(total|used)\s*=\s*([0-9.,]+)\s*([KMGT]?)`)
)

func macMemory(ctx context.Context, d Deps, r *Report) error {
	out, err := d.run(ctx, readCmd(macSysctl, "-n", "hw.memsize"))
	if err != nil {
		return err
	}
	total, err := strconv.ParseUint(firstLine(out), 10, 64)
	if err != nil || total == 0 {
		return fmt.Errorf("hw.memsize inválido")
	}
	r.Memory.TotalBytes = total
	if vm, err := d.run(ctx, readCmd(macVMStat)); err == nil {
		r.Memory.UsedBytes = macVMUsed(vm, total)
	}
	if sw, err := d.run(ctx, readCmd(macSysctl, "-n", "vm.swapusage")); err == nil {
		r.Memory.SwapTotalBytes, r.Memory.SwapUsedBytes = macSwap(sw)
	}
	return nil
}

// macVMUsed soma ativa + wired + ocupada pelo compressor (a mesma conta do diag-memoria); 0 = ilegível.
func macVMUsed(vm string, total uint64) uint64 {
	page := uint64(4096)
	if m := macPageSizeRe.FindStringSubmatch(vm); m != nil {
		if n, err := strconv.ParseUint(m[1], 10, 64); err == nil && n > 0 {
			page = n
		}
	}
	vals := map[string]uint64{}
	for _, ln := range strings.Split(vm, "\n") {
		if m := macVMLineRe.FindStringSubmatch(strings.TrimSpace(ln)); m != nil {
			if n, err := strconv.ParseUint(m[2], 10, 64); err == nil {
				vals[m[1]] = n
			}
		}
	}
	active, a := vals["Pages active"]
	wired, w := vals["Pages wired down"]
	comp, c := vals["Pages occupied by compressor"]
	if !a && !w && !c {
		return 0
	}
	used := (active + wired + comp) * page
	if used > total {
		used = total
	}
	return used
}

func macSwap(s string) (total, used uint64) {
	for _, m := range macSwapRe.FindAllStringSubmatch(s, -1) {
		v, err := strconv.ParseFloat(strings.ReplaceAll(m[2], ",", "."), 64)
		if err != nil || v < 0 || math.IsInf(v, 0) || math.IsNaN(v) {
			continue
		}
		mult := float64(1)
		switch m[3] {
		case "K":
			mult = 1 << 10
		case "M":
			mult = 1 << 20
		case "G":
			mult = 1 << 30
		case "T":
			mult = 1 << 40
		}
		b := uint64(v * mult)
		if m[1] == "total" {
			total = b
		} else {
			used = b
		}
	}
	if used > total {
		used = total
	}
	return total, used
}

// --- discos -----------------------------------------------------------------------------------------------------

type macPhys struct {
	name, bus, medium string
	size              uint64
	smart             string
}

func macDisks(ctx context.Context, d Deps, r *Report) error {
	out, err := d.run(ctx, readCmd(macProfiler, "SPNVMeDataType", "SPSerialATADataType", "SPStorageDataType", "-json"))
	if err != nil {
		return err
	}
	var doc map[string]json.RawMessage
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		return err
	}
	var phys []macPhys
	for key, bus := range map[string]string{"SPNVMeDataType": "NVMe", "SPSerialATADataType": "SATA"} {
		var ctrls []struct {
			Items []map[string]any `json:"_items"`
		}
		if raw, ok := doc[key]; ok && json.Unmarshal(raw, &ctrls) == nil {
			for _, c := range ctrls {
				for _, it := range c.Items {
					name := macStr(it["device_model"])
					if name == "" {
						name = macStr(it["_name"])
					}
					phys = append(phys, macPhys{name: macClean(name, 80), bus: bus, size: macUint(it["size_in_bytes"]), smart: macStr(it["smart_status"]), medium: macMedium(it["medium_type"])})
				}
			}
		}
	}
	// Espaço livre por disco físico: os volumes APFS de um contêiner compartilham o espaço, então vale o maior.
	free := map[string]uint64{}
	var vols []map[string]any
	if raw, ok := doc["SPStorageDataType"]; ok {
		_ = json.Unmarshal(raw, &vols)
	}
	for _, v := range vols {
		pd, _ := v["physical_drive"].(map[string]any)
		name := macStr(pd["device_name"])
		if name == "" || strings.EqualFold(name, "Disk Image") {
			continue
		}
		f := macUint(v["free_space_in_bytes"])
		if f > free[name] {
			free[name] = f
		}
		found := false
		for i := range phys {
			if phys[i].name == macClean(name, 80) {
				found = true
				if phys[i].medium == "" {
					phys[i].medium = macMedium(pd["medium_type"])
				}
				if phys[i].smart == "" {
					phys[i].smart = macStr(pd["smart_status"])
				}
			}
		}
		if !found {
			phys = append(phys, macPhys{name: macClean(name, 80), bus: macClean(macStr(pd["protocol"]), 40), medium: macMedium(pd["medium_type"]), smart: macStr(pd["smart_status"]), size: macUint(v["size_in_bytes"])})
		}
	}
	if len(phys) == 0 {
		return fmt.Errorf("nenhum disco encontrado")
	}
	for _, p := range phys {
		dk := Disk{Name: p.name, Bus: p.bus, Medium: p.medium, SizeBytes: p.size, FreeBytes: free[p.name], HealthRaw: p.smart}
		switch strings.ToLower(strings.TrimSpace(p.smart)) {
		case "verified":
			dk.Health = HealthOK
		case "failing":
			dk.Health = HealthBad
		default:
			dk.Health = HealthUnknown
		}
		r.Disks = append(r.Disks, dk)
	}
	sort.SliceStable(r.Disks, func(i, j int) bool { return r.Disks[i].Name < r.Disks[j].Name })
	return nil
}

func macMedium(v any) string {
	switch strings.ToLower(macStr(v)) {
	case "ssd":
		return "SSD"
	case "hdd", "rotational":
		return "HDD"
	}
	return ""
}

// --- bateria ----------------------------------------------------------------------------------------------------

func macBattery(ctx context.Context, d Deps, r *Report) error {
	out, err := d.run(ctx, readCmd(macProfiler, "SPPowerDataType", "-json"))
	if err != nil {
		return err
	}
	var doc struct {
		Items []map[string]any `json:"SPPowerDataType"`
	}
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		return err
	}
	var charge, health map[string]any
	for _, it := range doc.Items {
		if c, ok := it["sppower_battery_charge_info"].(map[string]any); ok {
			charge = c
		}
		if h, ok := it["sppower_battery_health_info"].(map[string]any); ok {
			health = h
		}
	}
	if charge == nil && health == nil {
		return nil // sem bateria (desktop): nil, sem Skipped
	}
	b := &Battery{}
	if n, ok := macInt(charge["sppower_battery_state_of_charge"]); ok && n >= 0 && n <= 100 {
		b.Percent = &n
	}
	switch {
	case macBool(charge["sppower_battery_fully_charged"]):
		b.Status = diagfmt.T(d.Lang, "cheia", "full", "llena")
	case macBool(charge["sppower_battery_is_charging"]):
		b.Status = diagfmt.T(d.Lang, "carregando", "charging", "cargando")
	case charge != nil:
		b.Status = diagfmt.T(d.Lang, "descarregando", "discharging", "descargando")
	}
	if n, ok := macInt(health["sppower_battery_cycle_count"]); ok && n >= 0 {
		b.CycleCount = &n
	}
	if n, ok := macInt(strings.TrimSuffix(macStr(health["sppower_battery_health_maximum_capacity"]), "%")); ok && n >= 0 && n <= 100 {
		b.HealthPct = &n
	}
	b.Condition = macClean(macStr(health["sppower_battery_health"]), 40)
	if b.Percent == nil {
		// Plano B: o percentual do pmset (ex.: "63%; discharging").
		if pm, err := d.run(ctx, readCmd(macPmset, "-g", "batt")); err == nil {
			if m := regexp.MustCompile(`(\d{1,3})%`).FindStringSubmatch(pm); m != nil {
				if n, _ := strconv.Atoi(m[1]); n >= 0 && n <= 100 {
					b.Percent = &n
				}
			}
		}
	}
	r.Battery = b
	return nil
}

// --- GPU --------------------------------------------------------------------------------------------------------

func macGPU(ctx context.Context, d Deps, r *Report) error {
	out, err := d.run(ctx, readCmd(macProfiler, "SPDisplaysDataType", "-json"))
	if err != nil {
		return err
	}
	var doc struct {
		Items []map[string]any `json:"SPDisplaysDataType"`
	}
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		return err
	}
	for _, it := range doc.Items {
		name := macStr(it["sppci_model"])
		if name == "" {
			name = macStr(it["_name"])
		}
		if name == "" {
			continue
		}
		g := GPU{Name: macClean(name, 80)}
		for _, k := range []string{"spdisplays_vram", "spdisplays_vram_shared"} {
			if v := macVRAM(macStr(it[k])); v > 0 {
				g.VRAMBytes = v
				break
			}
		}
		r.GPUs = append(r.GPUs, g)
	}
	if len(r.GPUs) == 0 {
		return fmt.Errorf("nenhuma GPU")
	}
	sort.SliceStable(r.GPUs, func(i, j int) bool { return r.GPUs[i].Name < r.GPUs[j].Name })
	return nil
}

var macVRAMRe = regexp.MustCompile(`^([0-9]+(?:[.,][0-9]+)?)\s*(MB|GB|TB)`)

func macVRAM(s string) uint64 {
	m := macVRAMRe.FindStringSubmatch(strings.TrimSpace(s))
	if m == nil {
		return 0
	}
	v, err := strconv.ParseFloat(strings.ReplaceAll(m[1], ",", "."), 64)
	if err != nil || v <= 0 {
		return 0
	}
	switch m[2] {
	case "TB":
		return uint64(v * (1 << 40))
	case "GB":
		return uint64(v * (1 << 30))
	}
	return uint64(v * (1 << 20))
}

// --- eventos ----------------------------------------------------------------------------------------------------

var (
	macLogRe  = regexp.MustCompile(`^(\d{4}-\d{2}-\d{2}) (\d{2}:\d{2}:\d{2})(?:\.\d+)?(?:[+-]\d{4})?\s+(\S+)\s+(.+)$`)
	macProcRe = regexp.MustCompile(`^(.+?)\[\d+(?::[0-9a-fA-F]+)?\]\s*(.*)$`)
	macHexRe  = regexp.MustCompile(`0x[0-9a-fA-F]+`)
	macNumRe  = regexp.MustCompile(`\d+`)
)

type macEvAgg struct {
	ev Event
}

func macEvents(ctx context.Context, d Deps, r *Report) error {
	c := readCmd(macLog, "show", "--last", "6h", "--style", "compact", "--predicate", "messageType == error")
	c.Timeout = 90 * time.Second
	out, err := d.run(ctx, c)
	if err != nil && out == "" {
		return err
	}
	if len(out) > macLogMaxBytes {
		out = out[:macLogMaxBytes]
	}
	loc := d.now().Location()
	groups := map[string]*macEvAgg{}
	for _, ln := range strings.Split(out, "\n") {
		m := macLogRe.FindStringSubmatch(strings.TrimSpace(ln))
		if m == nil {
			continue
		}
		when, perr := time.ParseInLocation("2006-01-02 15:04:05", m[1]+" "+m[2], loc)
		if perr != nil {
			continue
		}
		proc, msg := m[4], ""
		if pm := macProcRe.FindStringSubmatch(m[4]); pm != nil {
			proc, msg = pm[1], pm[2]
		}
		proc = macClean(proc, 80)
		if proc == "" {
			continue
		}
		msg = macClean(msg, macMaxMessageLen)
		norm := macNumRe.ReplaceAllString(macHexRe.ReplaceAllString(msg, "#"), "#")
		key := proc + "\x00" + norm
		if g, ok := groups[key]; ok {
			g.ev.Count++
			if when.After(g.ev.When) {
				g.ev.When = when
			}
			continue
		}
		groups[key] = &macEvAgg{ev: Event{When: when, Source: proc, Level: diagfmt.T(d.Lang, "erro", "error", "error"), Message: msg, Count: 1}}
	}
	if len(groups) == 0 {
		if err != nil {
			return err
		}
		return nil
	}
	evs := make([]Event, 0, len(groups))
	for _, g := range groups {
		evs = append(evs, g.ev)
	}
	sort.Slice(evs, func(i, j int) bool {
		a, b := evs[i], evs[j]
		if !a.When.Equal(b.When) {
			return a.When.After(b.When)
		}
		if a.Count != b.Count {
			return a.Count > b.Count
		}
		if a.Source != b.Source {
			return a.Source < b.Source
		}
		return a.Message < b.Message
	})
	if len(evs) > macMaxEvents {
		evs = evs[:macMaxEvents]
	}
	r.Events = evs
	return nil
}

// --- falhas -----------------------------------------------------------------------------------------------------

var macStampRe = regexp.MustCompile(`(\d{4}-\d{2}-\d{2})-(\d{2})(\d{2})(\d{2})`)

// macCrashes lista (sem ler o conteúdo) os relatórios de falha dos últimos 30 dias.
func macCrashes(d Deps, r *Report) error {
	dirs := append([]string(nil), macDirs...)
	if h := d.home(); h != "" {
		dirs = append([]string{path.Join(h, "Library/Logs/DiagnosticReports")}, dirs...)
	}
	cutoff := d.now().AddDate(0, 0, -macCrashDays)
	seen := map[string]bool{}
	var out []Crash
	var firstErr error
	okAny := false
	for _, dir := range dirs {
		for _, ext := range []string{"ips", "crash", "panic", "diag"} {
			files, err := d.glob(path.Join(dir, "*."+ext))
			if err != nil {
				if firstErr == nil {
					firstErr = err
				}
				continue
			}
			okAny = true
			for _, f := range files {
				base := path.Base(strings.ReplaceAll(f, "\\", "/"))
				if seen[base] {
					continue
				}
				seen[base] = true
				var when time.Time
				if m := macStampRe.FindStringSubmatch(base); m != nil {
					if t, err := time.ParseInLocation("2006-01-02 150405", m[1]+" "+m[2]+m[3]+m[4], d.now().Location()); err == nil {
						when = t
						if t.Before(cutoff) {
							continue
						}
					}
				}
				out = append(out, macCrash(base, when))
			}
		}
	}
	if !okAny && firstErr != nil {
		return firstErr
	}
	sort.SliceStable(out, func(i, j int) bool {
		if !out[i].When.Equal(out[j].When) {
			return out[i].When.After(out[j].When)
		}
		return out[i].Detail < out[j].Detail
	})
	if len(out) > macMaxCrashes {
		out = out[:macMaxCrashes]
	}
	r.Crashes = out
	return nil
}

func macCrash(base string, when time.Time) Crash {
	lower := strings.ToLower(base)
	if strings.HasSuffix(lower, ".panic") || strings.HasPrefix(lower, "kernel") {
		return Crash{When: when, Kind: CrashKernelPanic, Detail: "kernel"}
	}
	name := strings.TrimSuffix(base, path.Ext(base))
	if i := strings.Index(name, "_"); i >= 0 {
		name = name[:i]
	}
	if loc := macStampRe.FindStringIndex(name); loc != nil {
		name = strings.TrimRight(name[:loc[0]], "-_ ")
	}
	return Crash{When: when, Kind: CrashApp, Detail: macClean(name, 80)}
}

// --- utilidades -------------------------------------------------------------------------------------------------

func macStr(v any) string {
	switch x := v.(type) {
	case string:
		return strings.TrimSpace(x)
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	}
	return ""
}

func macInt(v any) (int, bool) {
	switch x := v.(type) {
	case float64:
		if x != math.Trunc(x) || math.IsInf(x, 0) || math.IsNaN(x) || x > 1e9 {
			return 0, false
		}
		return int(x), true
	case string:
		n, err := strconv.Atoi(strings.TrimSpace(x))
		return n, err == nil
	}
	return 0, false
}

func macUint(v any) uint64 {
	switch x := v.(type) {
	case float64:
		if x > 0 && x < 1<<62 {
			return uint64(x)
		}
	case string:
		if n, err := strconv.ParseUint(strings.TrimSpace(x), 10, 64); err == nil {
			return n
		}
	}
	return 0
}

func macBool(v any) bool {
	switch x := v.(type) {
	case bool:
		return x
	case string:
		s := strings.ToUpper(x)
		return s == "TRUE" || s == "YES"
	}
	return false
}

// macClean tira os caracteres de controle, fica com a primeira linha e trunca (em runas).
func macClean(s string, limit int) string {
	s = firstLine(s)
	s = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, s)
	rs := []rune(s)
	if len(rs) > limit {
		return string(rs[:limit])
	}
	return s
}

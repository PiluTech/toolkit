package diagnostico

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/PiluTech/toolkit/internal/diagfmt"
)

// Limites da coleta Linux.
const (
	linMsgMax     = 200 // tamanho máximo (em runas) da mensagem de um evento
	linMinTempC   = -40.0
	linMaxTempC   = 150.0
	linJournalMax = "300"
)

var (
	errLinNoTool = errors.New("ferramenta ausente")

	linDigitsRe  = regexp.MustCompile(`[0-9]+`)
	linPidRe     = regexp.MustCompile(`\[[0-9]+\]$`)
	linOOMRe     = regexp.MustCompile(`Out of memory: Killed process [0-9]+ \(([^)]+)\)`)
	linSegvRe    = regexp.MustCompile(`^([^\s\[:]+)\[[0-9]+\]: segfault at`)
	linCrashGrep = `Kernel panic|Oops:|segfault at|Out of memory: Killed process`
)

// collectLinux preenche r com o estado da máquina (linux), só por leitura. Cada área é independente.
func collectLinux(ctx context.Context, d Deps, r *Report) {
	linArea(d, r, "hardware", func() { linHardware(d, r) })
	linArea(d, r, "memoria", func() { linMemory(d, r) })
	linArea(d, r, "discos", func() { linDisks(ctx, d, r) })
	linArea(d, r, "bateria", func() { linBattery(d, r) })
	linArea(d, r, "temperatura", func() { linTemps(d, r) })
	linArea(d, r, "gpu", func() { linGPU(ctx, d, r) })
	linArea(d, r, "eventos", func() { linJournal(ctx, d, r) })
}

// linArea roda uma área; um pânico vira Skipped (a coleta nunca derruba o diagnóstico).
func linArea(d Deps, r *Report, area string, fn func()) {
	defer func() {
		if rec := recover(); rec != nil {
			linSkip(d, r, area,
				fmt.Sprintf("não foi possível ler esta área (erro interno: %v)", rec),
				fmt.Sprintf("could not read this area (internal error: %v)", rec),
				fmt.Sprintf("no se pudo leer esta área (error interno: %v)", rec))
		}
	}()
	fn()
}

func linSkip(d Deps, r *Report, area, pt, en, es string) {
	r.Skipped = append(r.Skipped, Skipped{Area: area, Reason: diagfmt.T(d.Lang, pt, en, es)})
}

// linRun acha a ferramenta no PATH (caminho absoluto) e a roda como leitura.
func linRun(ctx context.Context, d Deps, timeout time.Duration, tool string, args ...string) (string, error) {
	if d.Runner == nil {
		return "", errLinNoTool
	}
	p, err := d.Runner.LookPath(tool)
	if err != nil || p == "" {
		return "", errLinNoTool
	}
	c := readCmd(p, args...)
	c.Timeout = timeout
	return d.run(ctx, c)
}

func linRead(d Deps, path string) (string, bool) {
	b, err := d.readFile(path)
	if err != nil {
		return "", false
	}
	return strings.TrimSpace(string(b)), true
}

func linUint(s string) (uint64, bool) {
	v, err := strconv.ParseUint(strings.TrimSpace(s), 10, 64)
	return v, err == nil
}

func linGlob(d Deps, pattern string) []string {
	m, err := d.glob(pattern)
	if err != nil {
		return nil
	}
	sort.Strings(m)
	return m
}

// --- hardware ---

func linHardware(d Deps, r *Report) {
	h := &r.Hardware
	got := false
	if v, ok := linRead(d, "/sys/class/dmi/id/sys_vendor"); ok {
		h.Vendor, got = v, true
	}
	if v, ok := linRead(d, "/sys/class/dmi/id/product_name"); ok {
		h.Model, got = v, true
	}
	if v, ok := linRead(d, "/sys/class/dmi/id/bios_version"); ok {
		h.Firmware, got = v, true
	}
	if b, err := d.readFile("/proc/cpuinfo"); err == nil {
		linCPUInfo(string(b), h)
		got = got || h.CPU != "" || h.Threads > 0
	}
	if v, ok := linRead(d, "/proc/uptime"); ok {
		if f := strings.Fields(v); len(f) > 0 {
			if secs, err := strconv.ParseFloat(f[0], 64); err == nil && secs >= 0 && secs < 1e9 {
				h.Uptime = time.Duration(secs * float64(time.Second))
				got = true
			}
		}
	}
	if !got {
		linSkip(d, r, "hardware",
			"não foi possível ler /sys/class/dmi/id, /proc/cpuinfo nem /proc/uptime; rode em um Linux com /proc e /sys montados",
			"could not read /sys/class/dmi/id, /proc/cpuinfo or /proc/uptime; run on a Linux with /proc and /sys mounted",
			"no se pudo leer /sys/class/dmi/id, /proc/cpuinfo ni /proc/uptime; ejecute en un Linux con /proc y /sys montados")
	}
}

func linCPUInfo(s string, h *Hardware) {
	type key struct{ phys, core string }
	cores := map[key]bool{}
	var phys, core string
	threads, cpuCores, siblings := 0, 0, 0
	flush := func() {
		if core != "" {
			cores[key{phys, core}] = true
		}
		phys, core = "", ""
	}
	sc := bufio.NewScanner(strings.NewReader(s))
	for sc.Scan() {
		k, v, ok := strings.Cut(sc.Text(), ":")
		if !ok {
			if strings.TrimSpace(sc.Text()) == "" {
				flush()
			}
			continue
		}
		k, v = strings.TrimSpace(k), strings.TrimSpace(v)
		switch k {
		case "model name", "Model Name":
			if h.CPU == "" {
				h.CPU = v
			}
		case "processor":
			flush()
			threads++
		case "physical id":
			phys = v
		case "core id":
			core = v
		case "cpu cores":
			if n, err := strconv.Atoi(v); err == nil && cpuCores == 0 {
				cpuCores = n
			}
		case "siblings":
			if n, err := strconv.Atoi(v); err == nil && siblings == 0 {
				siblings = n
			}
		}
	}
	flush()
	h.Threads = threads
	switch {
	case len(cores) > 0:
		h.Cores = len(cores)
	case cpuCores > 0:
		h.Cores = cpuCores
		if siblings > 0 && threads > siblings {
			h.Cores = cpuCores * (threads / siblings)
		}
	}
	if h.Cores > h.Threads && h.Threads > 0 {
		h.Cores = h.Threads
	}
}

// --- memória ---

func linMemory(d Deps, r *Report) {
	b, err := d.readFile("/proc/meminfo")
	if err != nil {
		linSkip(d, r, "memoria", "não foi possível ler /proc/meminfo", "could not read /proc/meminfo", "no se pudo leer /proc/meminfo")
		return
	}
	m := map[string]uint64{}
	have := map[string]bool{}
	sc := bufio.NewScanner(bytes.NewReader(b))
	for sc.Scan() {
		k, v, ok := strings.Cut(sc.Text(), ":")
		if !ok {
			continue
		}
		f := strings.Fields(v)
		if len(f) == 0 {
			continue
		}
		n, ok := linUint(f[0])
		if !ok {
			continue
		}
		if len(f) > 1 && strings.EqualFold(f[1], "kB") {
			n *= 1024
		}
		m[strings.TrimSpace(k)] = n
		have[strings.TrimSpace(k)] = true
	}
	if !have["MemTotal"] || m["MemTotal"] == 0 {
		linSkip(d, r, "memoria", "/proc/meminfo sem MemTotal (saída truncada ou inválida)", "/proc/meminfo has no MemTotal (truncated or invalid output)", "/proc/meminfo sin MemTotal (salida truncada o inválida)")
		return
	}
	total := m["MemTotal"]
	var avail uint64
	if have["MemAvailable"] {
		avail = m["MemAvailable"]
	} else {
		avail = m["MemFree"] + m["Buffers"] + m["Cached"]
	}
	if avail > total {
		avail = total
	}
	r.Memory.TotalBytes = total
	r.Memory.UsedBytes = total - avail
	if have["SwapTotal"] {
		st, sf := m["SwapTotal"], m["SwapFree"]
		if sf > st {
			sf = st
		}
		r.Memory.SwapTotalBytes = st
		r.Memory.SwapUsedBytes = st - sf
	}
}

// --- discos ---

func linNum(v any) (uint64, bool) {
	switch x := v.(type) {
	case json.Number:
		if n, err := strconv.ParseUint(x.String(), 10, 64); err == nil {
			return n, true
		}
		return 0, false
	case string:
		return linUint(x)
	case float64:
		if x >= 0 {
			return uint64(x), true
		}
	}
	return 0, false
}

func linStr(v any) string {
	if s, ok := v.(string); ok {
		return strings.TrimSpace(s)
	}
	return ""
}

// linRota devolve (rotacional, conhecido): aceita bool, número ou texto.
func linRota(v any) (bool, bool) {
	switch x := v.(type) {
	case bool:
		return x, true
	case json.Number:
		return x.String() != "0", true
	case string:
		switch strings.TrimSpace(x) {
		case "1", "true":
			return true, true
		case "0", "false":
			return false, true
		}
	}
	return false, false
}

type linHwmon struct{ dir, name string }

func linHwmons(d Deps) []linHwmon {
	var out []linHwmon
	for _, p := range linGlob(d, "/sys/class/hwmon/hwmon*") {
		n, _ := linRead(d, p+"/name")
		out = append(out, linHwmon{dir: p, name: n})
	}
	return out
}

// linMilli lê um arquivo em milésimos de °C; fora de -40..150 é leitura absurda.
func linMilli(d Deps, path string) (float64, bool) {
	s, ok := linRead(d, path)
	if !ok {
		return 0, false
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, false
	}
	c := float64(n) / 1000
	if c < linMinTempC || c > linMaxTempC {
		return 0, false
	}
	return c, true
}

func linDiskParent(part, disk string) bool {
	if !strings.HasPrefix(part, disk) || part == disk {
		return false
	}
	rest := strings.TrimPrefix(part[len(disk):], "p")
	if rest == "" {
		return false
	}
	for _, c := range rest {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

func linDisks(ctx context.Context, d Deps, r *Report) {
	out, err := linRun(ctx, d, 20*time.Second, "lsblk", "-b", "-d", "-J", "-o", "NAME,SIZE,TYPE,ROTA,MODEL,TRAN")
	if err != nil {
		linSkip(d, r, "discos",
			"não foi possível listar os discos (lsblk); instale o util-linux (por exemplo: sudo apt install util-linux)",
			"could not list the disks (lsblk); install util-linux (for example: sudo apt install util-linux)",
			"no se pudo listar los discos (lsblk); instale util-linux (por ejemplo: sudo apt install util-linux)")
		return
	}
	dec := json.NewDecoder(strings.NewReader(out))
	dec.UseNumber()
	var doc struct {
		Devs []map[string]any `json:"blockdevices"`
	}
	if err := dec.Decode(&doc); err != nil {
		linSkip(d, r, "discos", "a saída do lsblk é inválida ou veio truncada", "the lsblk output is invalid or truncated", "la salida de lsblk es inválida o está truncada")
		return
	}
	type diskDev struct {
		dev  string
		nvme bool
	}
	var devs []diskDev
	var disks []Disk
	for _, m := range doc.Devs {
		if linStr(m["type"]) != "disk" {
			continue
		}
		name := linStr(m["name"])
		if name == "" {
			continue
		}
		size, _ := linNum(m["size"])
		dk := Disk{Name: linStr(m["model"]), SizeBytes: size}
		if dk.Name == "" {
			dk.Name = name
		}
		if rot, ok := linRota(m["rota"]); ok {
			if rot {
				dk.Medium = "HDD"
			} else {
				dk.Medium = "SSD"
			}
		}
		tran := strings.ToLower(linStr(m["tran"]))
		switch tran {
		case "nvme":
			dk.Bus = "NVMe"
		case "sata":
			dk.Bus = "SATA"
		case "usb":
			dk.Bus = "USB"
		case "":
			if strings.HasPrefix(name, "nvme") {
				dk.Bus = "NVMe"
			}
		default:
			dk.Bus = strings.ToUpper(tran)
		}
		devs = append(devs, diskDev{dev: name, nvme: dk.Bus == "NVMe"})
		disks = append(disks, dk)
	}
	sort.SliceStable(disks, func(i, j int) bool { return devs[i].dev < devs[j].dev })
	sort.SliceStable(devs, func(i, j int) bool { return devs[i].dev < devs[j].dev })

	// espaço livre: df agrupado pelo disco pai (0 = desconhecido)
	if dfOut, err := linRun(ctx, d, 20*time.Second, "df", "-B1", "--output=source,avail,size"); err == nil {
		for _, line := range strings.Split(dfOut, "\n") {
			f := strings.Fields(line)
			if len(f) < 3 || !strings.HasPrefix(f[0], "/dev/") {
				continue
			}
			avail, ok := linUint(f[1])
			if !ok {
				continue
			}
			base := strings.TrimPrefix(f[0], "/dev/")
			for i := range devs {
				if linDiskParent(base, devs[i].dev) {
					disks[i].FreeBytes += avail
					break
				}
			}
		}
	}

	// temperatura dos discos por hwmon (nvme/drivetemp), na ordem dos discos do mesmo tipo
	var nvmeT, otherT []float64
	for _, h := range linHwmons(d) {
		if h.name != "nvme" && h.name != "drivetemp" {
			continue
		}
		c, ok := linMilli(d, h.dir+"/temp1_input")
		if !ok {
			continue
		}
		if h.name == "nvme" {
			nvmeT = append(nvmeT, c)
		} else {
			otherT = append(otherT, c)
		}
	}
	ni, oi := 0, 0
	for i := range disks {
		if devs[i].nvme && ni < len(nvmeT) {
			v := nvmeT[ni]
			disks[i].TempC = &v
			ni++
		} else if !devs[i].nvme && oi < len(otherT) {
			v := otherT[oi]
			disks[i].TempC = &v
			oi++
		}
	}
	r.Disks = disks

	if len(disks) > 0 {
		var cmds []string
		for _, dv := range devs {
			if dv.nvme {
				cmds = append(cmds, "sudo nvme smart-log /dev/"+strings.TrimRight(dv.dev, "n0123456789")+"0")
			} else {
				cmds = append(cmds, "sudo smartctl -H -A /dev/"+dv.dev)
			}
		}
		cmds = linUniq(cmds)
		if len(cmds) > 4 {
			cmds = cmds[:4]
		}
		l := strings.Join(cmds, " ; ")
		linSkip(d, r, "smart",
			"o SMART exige administrador e não foi lido; rode: "+l,
			"SMART needs administrator rights and was not read; run: "+l,
			"SMART requiere administrador y no se leyó; ejecute: "+l)
	}
}

func linUniq(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

// --- bateria ---

func linBattery(d Deps, r *Report) {
	dirs := linGlob(d, "/sys/class/power_supply/BAT*")
	if len(dirs) == 0 {
		return
	}
	dir := dirs[0]
	b := &Battery{}
	got := false
	if v, ok := linRead(d, dir+"/capacity"); ok {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 && n <= 100 {
			b.Percent = &n
			got = true
		}
	}
	if v, ok := linRead(d, dir+"/status"); ok {
		switch strings.ToLower(v) {
		case "charging":
			b.Status = diagfmt.T(d.Lang, "carregando", "charging", "cargando")
		case "discharging":
			b.Status = diagfmt.T(d.Lang, "descarregando", "discharging", "descargando")
		case "full":
			b.Status = diagfmt.T(d.Lang, "cheia", "full", "llena")
		}
	}
	if v, ok := linRead(d, dir+"/cycle_count"); ok {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			b.CycleCount = &n
		}
	}
	for _, p := range [][2]string{{"energy_full", "energy_full_design"}, {"charge_full", "charge_full_design"}} {
		fs, ok1 := linRead(d, dir+"/"+p[0])
		ds, ok2 := linRead(d, dir+"/"+p[1])
		if !ok1 || !ok2 {
			continue
		}
		full, ok1 := linUint(fs)
		design, ok2 := linUint(ds)
		if !ok1 || !ok2 || design == 0 {
			continue
		}
		pct := int(min(full*100/design, 100))
		b.HealthPct = &pct
		got = true
		break
	}
	if !got && b.CycleCount == nil && b.Status == "" {
		linSkip(d, r, "bateria",
			"a bateria foi encontrada mas não foi possível ler os valores em /sys/class/power_supply",
			"a battery was found but its values in /sys/class/power_supply could not be read",
			"se encontró una batería pero no se pudieron leer sus valores en /sys/class/power_supply")
		return
	}
	r.Battery = b
}

// --- temperatura ---

func linTemps(d Deps, r *Report) {
	var temps []Temp
	for _, p := range linGlob(d, "/sys/class/thermal/thermal_zone*/temp") {
		c, ok := linMilli(d, p)
		if !ok {
			continue
		}
		name, _ := linRead(d, path.Dir(p)+"/type")
		if name == "" {
			name = path.Base(path.Dir(p))
		}
		temps = append(temps, Temp{Sensor: name, Celsius: c})
	}
	for _, h := range linHwmons(d) {
		for _, p := range linGlob(d, h.dir+"/temp*_input") {
			c, ok := linMilli(d, p)
			if !ok {
				continue
			}
			base := strings.TrimSuffix(path.Base(p), "_input")
			label, _ := linRead(d, h.dir+"/"+base+"_label")
			if label == "" {
				label = base
			}
			name := label
			if h.name != "" {
				name = h.name + " " + label
			}
			temps = append(temps, Temp{Sensor: name, Celsius: c})
		}
	}
	if len(temps) == 0 {
		linSkip(d, r, "temperatura",
			"nenhum sensor de temperatura legível; instale o lm-sensors (sudo apt install lm-sensors) ou o kernel não expõe sensores nesta máquina",
			"no readable temperature sensor; install lm-sensors (sudo apt install lm-sensors) or the kernel does not expose sensors on this machine",
			"ningún sensor de temperatura legible; instale lm-sensors (sudo apt install lm-sensors) o el kernel no expone sensores en esta máquina")
		return
	}
	sort.SliceStable(temps, func(i, j int) bool {
		if temps[i].Sensor != temps[j].Sensor {
			return temps[i].Sensor < temps[j].Sensor
		}
		return temps[i].Celsius < temps[j].Celsius
	})
	r.Temps = temps
}

// --- GPU ---

// linQuoted separa os campos de uma linha do lspci -mm (entre aspas, ou palavras soltas).
func linQuoted(line string) []string {
	var out []string
	i := 0
	for i < len(line) {
		switch line[i] {
		case ' ':
			i++
		case '"':
			j := i + 1
			var sb strings.Builder
			for j < len(line) && line[j] != '"' {
				if line[j] == '\\' && j+1 < len(line) {
					j++
				}
				sb.WriteByte(line[j])
				j++
			}
			out = append(out, sb.String())
			i = j + 1
		default:
			j := i
			for j < len(line) && line[j] != ' ' {
				j++
			}
			out = append(out, line[i:j])
			i = j
		}
	}
	return out
}

func linGPU(ctx context.Context, d Deps, r *Report) {
	out, err := linRun(ctx, d, 20*time.Second, "lspci", "-mm")
	if err == nil {
		var gpus []GPU
		for _, line := range strings.Split(out, "\n") {
			f := linQuoted(line)
			// f[0]=endereço (descartado), f[1]=classe, f[2]=fabricante, f[3]=produto
			if len(f) < 4 {
				continue
			}
			cl := strings.ToLower(f[1])
			if !strings.Contains(cl, "vga") && !strings.Contains(cl, "3d controller") && !strings.Contains(cl, "display controller") {
				continue
			}
			gpus = append(gpus, GPU{Name: strings.TrimSpace(f[2] + " " + f[3])})
		}
		sort.SliceStable(gpus, func(i, j int) bool { return gpus[i].Name < gpus[j].Name })
		r.GPUs = gpus
		return
	}
	// fallback: ids PCI no sysfs
	var gpus []GPU
	for _, p := range linGlob(d, "/sys/class/drm/card*/device/vendor") {
		ven, ok1 := linRead(d, p)
		dev, ok2 := linRead(d, path.Dir(p)+"/device")
		if !ok1 || !ok2 {
			continue
		}
		gpus = append(gpus, GPU{Name: "PCI " + strings.TrimPrefix(ven, "0x") + ":" + strings.TrimPrefix(dev, "0x")})
	}
	if len(gpus) > 0 {
		sort.SliceStable(gpus, func(i, j int) bool { return gpus[i].Name < gpus[j].Name })
		r.GPUs = gpus
		return
	}
	linSkip(d, r, "gpu",
		"não foi possível identificar a GPU; instale o pciutils (sudo apt install pciutils)",
		"could not identify the GPU; install pciutils (sudo apt install pciutils)",
		"no se pudo identificar la GPU; instale pciutils (sudo apt install pciutils)")
}

// --- eventos e falhas ---

type linLine struct {
	when   time.Time
	source string
	msg    string
}

func linClean(s string) string {
	s = firstLine(s)
	rs := []rune(s)
	if len(rs) > linMsgMax {
		s = string(rs[:linMsgMax])
	}
	return s
}

func linParseTime(s string) (time.Time, bool) {
	s = strings.Replace(s, ",", ".", 1)
	for _, l := range []string{"2006-01-02T15:04:05-0700", time.RFC3339Nano, time.RFC3339} {
		if t, err := time.Parse(l, s); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

// linParseJournal lê linhas do journalctl -o short-iso: "<data> <host> <ident[pid]>: <mensagem>" (o host é descartado).
func linParseJournal(out string) []linLine {
	var res []linLine
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimRight(line, "\r")
		if line == "" || strings.HasPrefix(line, "-- ") {
			continue
		}
		f := strings.SplitN(line, " ", 3)
		if len(f) < 3 {
			continue
		}
		t, ok := linParseTime(f[0])
		if !ok {
			continue
		}
		src, msg := "", f[2]
		if i := strings.Index(f[2], ": "); i >= 0 {
			src, msg = f[2][:i], f[2][i+2:]
		}
		src = linPidRe.ReplaceAllString(strings.TrimSpace(src), "")
		res = append(res, linLine{when: t, source: src, msg: msg})
	}
	return res
}

// linParseDmesg lê o dmesg --time-format iso: "<data> <mensagem>".
func linParseDmesg(out string) []linLine {
	var res []linLine
	for _, line := range strings.Split(out, "\n") {
		f := strings.SplitN(strings.TrimRight(line, "\r"), " ", 2)
		if len(f) < 2 {
			continue
		}
		t, ok := linParseTime(f[0])
		if !ok {
			continue
		}
		res = append(res, linLine{when: t, source: "kernel", msg: f[1]})
	}
	return res
}

func linGroup(d Deps, lines []linLine) []Event {
	level := diagfmt.T(d.Lang, "erro", "error", "error")
	idx := map[string]int{}
	var evs []Event
	for _, l := range lines {
		msg := linClean(l.msg)
		if msg == "" {
			continue
		}
		k := l.source + "|" + linDigitsRe.ReplaceAllString(msg, "#")
		if i, ok := idx[k]; ok {
			evs[i].Count++
			if l.when.After(evs[i].When) {
				evs[i].When, evs[i].Message = l.when, msg
			}
			continue
		}
		idx[k] = len(evs)
		evs = append(evs, Event{When: l.when, Source: l.source, Level: level, Message: msg, Count: 1})
	}
	sort.SliceStable(evs, func(i, j int) bool {
		if !evs[i].When.Equal(evs[j].When) {
			return evs[i].When.After(evs[j].When)
		}
		if evs[i].Source != evs[j].Source {
			return evs[i].Source < evs[j].Source
		}
		return evs[i].Message < evs[j].Message
	})
	return evs
}

func linCrashes(lines []linLine) []Crash {
	var cs []Crash
	for _, l := range lines {
		msg := l.msg
		switch {
		case linOOMRe.MatchString(msg):
			cs = append(cs, Crash{When: l.when, Kind: CrashApp, Detail: "OOM: " + linClean(linOOMRe.FindStringSubmatch(msg)[1])})
		case linSegvRe.MatchString(msg):
			cs = append(cs, Crash{When: l.when, Kind: CrashApp, Detail: "segfault: " + linClean(linSegvRe.FindStringSubmatch(msg)[1])})
		case strings.Contains(msg, "Kernel panic"), strings.Contains(msg, "Oops:"):
			cs = append(cs, Crash{When: l.when, Kind: CrashKernelPanic, Detail: linClean(msg)})
		}
	}
	sort.SliceStable(cs, func(i, j int) bool {
		if !cs[i].When.Equal(cs[j].When) {
			return cs[i].When.After(cs[j].When)
		}
		return cs[i].Detail < cs[j].Detail
	})
	return cs
}

func linJournal(ctx context.Context, d Deps, r *Report) {
	out, err := linRun(ctx, d, 30*time.Second, "journalctl", "-p", "err", "-b", "--no-pager", "-q", "-o", "short-iso", "-n", linJournalMax)
	if err == nil {
		r.Events = linGroup(d, linParseJournal(out))
		cout, cerr := linRun(ctx, d, 30*time.Second, "journalctl", "--no-pager", "-q", "-o", "short-iso", "--since", "30 days ago", "-g", linCrashGrep, "-n", linJournalMax)
		if cerr == nil {
			r.Crashes = linCrashes(linParseJournal(cout))
		} else {
			linSkip(d, r, "falhas",
				"não foi possível procurar falhas no journal (journalctl -g); atualize o systemd ou rode com sudo",
				"could not search the journal for crashes (journalctl -g); update systemd or run with sudo",
				"no se pudo buscar fallos en el journal (journalctl -g); actualice systemd o ejecute con sudo")
		}
		return
	}
	dout, derr := linRun(ctx, d, 30*time.Second, "dmesg", "--level=err,crit,alert,emerg", "--time-format", "iso")
	if derr == nil {
		lines := linParseDmesg(dout)
		r.Events = linGroup(d, lines)
		r.Crashes = linCrashes(lines)
		return
	}
	linSkip(d, r, "eventos",
		"sem acesso ao journal nem ao dmesg; adicione o usuário ao grupo systemd-journal (ou adm) ou rode com sudo",
		"no access to the journal or dmesg; add the user to the systemd-journal (or adm) group or run with sudo",
		"sin acceso al journal ni a dmesg; agregue el usuario al grupo systemd-journal (o adm) o ejecute con sudo")
	linSkip(d, r, "falhas",
		"sem acesso ao journal nem ao dmesg; adicione o usuário ao grupo systemd-journal (ou adm) ou rode com sudo",
		"no access to the journal or dmesg; add the user to the systemd-journal (or adm) group or run with sudo",
		"sin acceso al journal ni a dmesg; agregue el usuario al grupo systemd-journal (o adm) o ejecute con sudo")
}

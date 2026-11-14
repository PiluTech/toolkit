package diagnostico

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/PiluTech/toolkit/internal/diagfmt"
)

// Coletor do Windows: cada área é um script de PowerShell de LEITURA (sem administrador, só ASCII, JSON compacto)
// e uma função que lê o JSON com tolerância (campos nulos, números como texto, objeto único no lugar de lista).
// Nada aqui lê número de série, UUID, MAC, usuário, hostname ou IP.

const (
	winPrefs = "$ErrorActionPreference='SilentlyContinue';$ProgressPreference='SilentlyContinue'\n"

	winScriptHW = winPrefs +
		`$cs=Get-CimInstance Win32_ComputerSystem;$os=Get-CimInstance Win32_OperatingSystem;$b=Get-CimInstance Win32_BIOS` + "\n" +
		`$cpu=@(Get-CimInstance Win32_Processor|ForEach-Object{@{Name=[string]$_.Name;Cores=$_.NumberOfCores;Threads=$_.NumberOfLogicalProcessors}})` + "\n" +
		`$up=$null;if($os.LastBootUpTime){$up=[int]((Get-Date)-$os.LastBootUpTime).TotalSeconds}` + "\n" +
		`@{cs=@{Manufacturer=[string]$cs.Manufacturer;Model=[string]$cs.Model;TotalPhysicalMemory=$cs.TotalPhysicalMemory};cpu=$cpu;bios=[string]$b.SMBIOSBIOSVersion;uptimeSec=$up}|ConvertTo-Json -Compress -Depth 4`

	winScriptMem = winPrefs +
		`$os=Get-CimInstance Win32_OperatingSystem` + "\n" +
		`$pf=@(Get-CimInstance Win32_PageFileUsage|ForEach-Object{@{Alloc=$_.AllocatedBaseSize;Used=$_.CurrentUsage}})` + "\n" +
		`@{total=$os.TotalVisibleMemorySize;free=$os.FreePhysicalMemory;swap=$pf}|ConvertTo-Json -Compress -Depth 4`

	winScriptDisks = winPrefs +
		`$d=@(Get-PhysicalDisk|ForEach-Object{@{Id=[string]$_.DeviceId;Name=[string]$_.FriendlyName;Media=[string]$_.MediaType;Bus=[string]$_.BusType;Health=[string]$_.HealthStatus;Size=$_.Size}})` + "\n" +
		`$p=@(Get-Partition|Where-Object{$_.DriveLetter}|ForEach-Object{@{Disk=$_.DiskNumber;Letter=[string]$_.DriveLetter}})` + "\n" +
		`$v=@(Get-Volume|Where-Object{$_.DriveLetter}|ForEach-Object{@{Letter=[string]$_.DriveLetter;Free=$_.SizeRemaining}})` + "\n" +
		`@{disks=$d;parts=$p;vols=$v}|ConvertTo-Json -Compress -Depth 4`

	winScriptSmart = winPrefs +
		`$r=@(Get-PhysicalDisk|ForEach-Object{$c=$_|Get-StorageReliabilityCounter;if($c){@{Id=[string]$_.DeviceId;Temp=$c.Temperature;Wear=$c.Wear;Hours=$c.PowerOnHours}}})` + "\n" +
		`ConvertTo-Json -Compress -Depth 4 -InputObject $r`

	winScriptBattery = winPrefs +
		`$b=@(Get-CimInstance Win32_Battery)|Select-Object -First 1` + "\n" +
		`if(-not $b){'{"present":false}'}else{` +
		`$s=Get-CimInstance -Namespace root\wmi -ClassName BatteryStaticData|Select-Object -First 1;` +
		`$f=Get-CimInstance -Namespace root\wmi -ClassName BatteryFullChargedCapacity|Select-Object -First 1;` +
		`@{present=$true;charge=$b.EstimatedChargeRemaining;status=$b.BatteryStatus;design=$s.DesignedCapacity;full=$f.FullChargedCapacity}|ConvertTo-Json -Compress}`

	winScriptTemp = winPrefs +
		`$t=@(Get-CimInstance -Namespace root\wmi -ClassName MSAcpi_ThermalZoneTemperature|ForEach-Object -Begin{$i=0}{@{Idx=$i;Dk=$_.CurrentTemperature};$i++})` + "\n" +
		`ConvertTo-Json -Compress -Depth 4 -InputObject $t`

	winScriptGPU = winPrefs +
		`$g=@(Get-CimInstance Win32_VideoController|ForEach-Object{@{Name=[string]$_.Name;DriverVersion=[string]$_.DriverVersion;AdapterRAM=$_.AdapterRAM}})` + "\n" +
		`ConvertTo-Json -Compress -Depth 4 -InputObject $g`

	winScriptEvents = winPrefs +
		`$e=@(Get-WinEvent -FilterHashtable @{LogName='System','Application';Level=1,2;StartTime=(Get-Date).AddDays(-7)} -MaxEvents 400|ForEach-Object{@{T=$_.TimeCreated.ToUniversalTime().ToString('o');P=[string]$_.ProviderName;I=$_.Id;L=$_.Level;M=[string]$_.Message}})` + "\n" +
		`ConvertTo-Json -Compress -Depth 4 -InputObject $e`

	winScriptCrashes = winPrefs +
		`$s=(Get-Date).AddDays(-30);$o=@()` + "\n" +
		`$o+=@(Get-WinEvent -FilterHashtable @{LogName='System';ProviderName='Microsoft-Windows-WER-SystemErrorReporting';Id=1001;StartTime=$s} -MaxEvents 50)` + "\n" +
		`$o+=@(Get-WinEvent -FilterHashtable @{LogName='System';ProviderName='Microsoft-Windows-Kernel-Power';Id=41;StartTime=$s} -MaxEvents 50)` + "\n" +
		`$o+=@(Get-WinEvent -FilterHashtable @{LogName='Application';ProviderName='Application Error';Id=1000;StartTime=$s} -MaxEvents 50)` + "\n" +
		`$r=@($o|ForEach-Object{@{T=$_.TimeCreated.ToUniversalTime().ToString('o');P=[string]$_.ProviderName;I=$_.Id;M=[string]$_.Message}})` + "\n" +
		`ConvertTo-Json -Compress -Depth 4 -InputObject $r`

	winScriptStability = winPrefs +
		`$m=Get-CimInstance Win32_ReliabilityStabilityMetrics|Sort-Object TimeGenerated -Descending|Select-Object -First 1` + "\n" +
		`@{idx=$m.SystemStabilityIndex}|ConvertTo-Json -Compress`
)

const (
	winMaxEventGroups = 30
	winMaxMessage     = 200
	winMaxCrashes     = 50
	winWearWarn       = 80
)

var (
	errWinNoData = errors.New("sem dados")

	winBugcheck = regexp.MustCompile(`0x[0-9a-fA-F]{8}`)
	winFaulting = regexp.MustCompile(`(?i)Faulting application name:\s*([^,\r\n]+)`)
	winFaultPth = regexp.MustCompile(`(?i)Faulting application path:\s*([^\r\n]+)`)
)

// collectWindows preenche r com o estado da máquina Windows. Nunca devolve erro: área que falha vira Skipped.
func collectWindows(ctx context.Context, d Deps, r *Report) {
	skip := func(area, pt, en, es string) {
		r.Skipped = append(r.Skipped, Skipped{Area: area, Reason: diagfmt.T(d.Lang, pt, en, es)})
	}
	winArea(ctx, d, r, "hardware", winScriptHW, winHardware, func() {
		skip("hardware", "não foi possível ler o hardware (PowerShell/CIM indisponível); rode o diagnóstico em um PowerShell normal",
			"could not read the hardware (PowerShell/CIM unavailable); run the diagnosis from a regular PowerShell",
			"no se pudo leer el hardware (PowerShell/CIM no disponible); ejecute el diagnóstico en un PowerShell normal")
	})
	winArea(ctx, d, r, "memoria", winScriptMem, winMemory, func() {
		skip("memoria", "não foi possível ler a memória; rode o diagnóstico em um PowerShell normal",
			"could not read the memory; run the diagnosis from a regular PowerShell",
			"no se pudo leer la memoria; ejecute el diagnóstico en un PowerShell normal")
	})
	var diskIDs []string
	winArea(ctx, d, r, "discos", winScriptDisks, winDisks(&diskIDs), func() {
		skip("discos", "não foi possível listar os discos; rode o diagnóstico em um PowerShell normal",
			"could not list the disks; run the diagnosis from a regular PowerShell",
			"no se pudo listar los discos; ejecute el diagnóstico en un PowerShell normal")
	})
	if len(r.Disks) > 0 {
		winArea(ctx, d, r, "smart", winScriptSmart, winSmart(diskIDs), func() {
			skip("smart", "o SMART (temperatura, desgaste, horas ligado) não pôde ser lido sem permissão: abra o PowerShell como administrador e rode o diagnóstico de novo",
				"SMART data (temperature, wear, power-on hours) could not be read without permission: open PowerShell as administrator and run the diagnosis again",
				"no se pudo leer SMART (temperatura, desgaste, horas encendido) sin permiso: abra PowerShell como administrador y ejecute el diagnóstico de nuevo")
		})
	}
	winArea(ctx, d, r, "bateria", winScriptBattery, winBattery, func() {
		skip("bateria", "não foi possível ler a bateria; rode o diagnóstico em um PowerShell normal",
			"could not read the battery; run the diagnosis from a regular PowerShell",
			"no se pudo leer la batería; ejecute el diagnóstico en un PowerShell normal")
	})
	winArea(ctx, d, r, "temperatura", winScriptTemp, winTemps, func() {
		skip("temperatura", "a temperatura não é exposta por esta máquina sem permissão (sensor ACPI ausente ou restrito): abra o PowerShell como administrador ou use a ferramenta do fabricante",
			"temperature is not exposed by this machine without permission (ACPI sensor missing or restricted): open PowerShell as administrator or use the vendor tool",
			"la temperatura no está expuesta por este equipo sin permiso (sensor ACPI ausente o restringido): abra PowerShell como administrador o use la herramienta del fabricante")
	})
	winArea(ctx, d, r, "gpu", winScriptGPU, winGPUs, func() {
		skip("gpu", "não foi possível ler a placa de vídeo; rode o diagnóstico em um PowerShell normal",
			"could not read the graphics card; run the diagnosis from a regular PowerShell",
			"no se pudo leer la tarjeta de video; ejecute el diagnóstico en un PowerShell normal")
	})
	winArea(ctx, d, r, "eventos", winScriptEvents, winEvents, func() {
		skip("eventos", "não foi possível ler o log de eventos do Windows; abra o PowerShell como administrador para ler o log do sistema",
			"could not read the Windows event log; open PowerShell as administrator to read the system log",
			"no se pudo leer el registro de eventos de Windows; abra PowerShell como administrador para leer el registro del sistema")
	})
	winArea(ctx, d, r, "falhas", winScriptCrashes, winCrashes, func() {
		skip("falhas", "não foi possível ler as falhas recentes (tela azul, desligamento inesperado, travamento de programas); abra o PowerShell como administrador",
			"could not read recent failures (blue screen, unexpected shutdown, app crashes); open PowerShell as administrator",
			"no se pudieron leer las fallas recientes (pantalla azul, apagado inesperado, cierres de programas); abra PowerShell como administrador")
	})
	winArea(ctx, d, r, "estabilidade", winScriptStability, winStability, func() {
		skip("estabilidade", "o índice de confiabilidade do Windows não está disponível; rode o diagnóstico em um PowerShell normal",
			"the Windows reliability index is not available; run the diagnosis from a regular PowerShell",
			"el índice de confiabilidad de Windows no está disponible; ejecute el diagnóstico en un PowerShell normal")
	})
}

// winArea roda o script e entrega o JSON ao leitor; erro do comando, saída vazia, JSON inválido ou leitor com erro
// chamam onFail (registrar o Skipped). Um pânico do leitor também vira falha da área.
func winArea(ctx context.Context, d Deps, r *Report, _ string, script string, read func(d Deps, r *Report, v any) error, onFail func()) {
	ok := false
	func() {
		defer func() {
			if recover() != nil {
				ok = false
			}
		}()
		out, err := d.run(ctx, psCmd(script, time.Minute))
		if err != nil || strings.TrimSpace(out) == "" {
			return
		}
		var v any
		if json.Unmarshal([]byte(strings.TrimSpace(strings.TrimPrefix(out, "\ufeff"))), &v) != nil {
			return
		}
		ok = read(d, r, v) == nil
	}()
	if !ok {
		onFail()
	}
}

// ---- helpers de JSON tolerante ----

func winMap(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}

// winList devolve os objetos de uma lista; o PowerShell entrega um objeto único no lugar da lista de um item.
func winList(v any) []map[string]any {
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

func winStr(v any) string {
	switch x := v.(type) {
	case string:
		return strings.TrimSpace(x)
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	}
	return ""
}

func winNum(v any) (float64, bool) {
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

// winUint converte para uint64 (negativo ou não numérico = 0, false).
func winUint(v any) (uint64, bool) {
	f, ok := winNum(v)
	if !ok || f < 0 || f >= 1.8e19 {
		return 0, false
	}
	return uint64(f), true
}

func winInt(v any) (int, bool) {
	f, ok := winNum(v)
	if !ok || f < math.MinInt32 || f > math.MaxInt32 {
		return 0, false
	}
	return int(math.Round(f)), true
}

func winClampPct(n int) int {
	if n < 0 {
		return 0
	}
	if n > 100 {
		return 100
	}
	return n
}

func winTime(v any) (time.Time, bool) {
	t, err := time.Parse(time.RFC3339Nano, winStr(v))
	return t, err == nil
}

// winMessage deixa só a primeira linha não vazia, sem caractere de controle, em até 200 letras.
func winMessage(s string) string {
	line := ""
	for _, l := range strings.FieldsFunc(s, func(r rune) bool { return r == '\n' || r == '\r' }) {
		if strings.TrimSpace(l) != "" {
			line = strings.TrimSpace(l)
			break
		}
	}
	line = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return ' '
		}
		return r
	}, line)
	if rs := []rune(line); len(rs) > winMaxMessage {
		line = string(rs[:winMaxMessage-1]) + "…"
	}
	return line
}

// winBase tira o caminho de um nome de arquivo (barras do Windows e do Unix).
func winBase(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.LastIndexAny(s, `\/`); i >= 0 {
		s = s[i+1:]
	}
	return strings.TrimSpace(s)
}

// ---- leitores de cada área ----

func winHardware(_ Deps, r *Report, v any) error {
	m := winMap(v)
	if m == nil {
		return errWinNoData
	}
	h := &r.Hardware
	if cs := winMap(m["cs"]); cs != nil {
		h.Vendor, h.Model = winStr(cs["Manufacturer"]), winStr(cs["Model"])
	}
	for i, c := range winList(m["cpu"]) {
		if i == 0 {
			h.CPU = winStr(c["Name"])
		}
		if n, ok := winInt(c["Cores"]); ok && n > 0 {
			h.Cores += n
		}
		if n, ok := winInt(c["Threads"]); ok && n > 0 {
			h.Threads += n
		}
	}
	h.Firmware = winStr(m["bios"])
	if s, ok := winNum(m["uptimeSec"]); ok && s > 0 && s < 1e9 {
		h.Uptime = time.Duration(s) * time.Second
	}
	if h.Vendor == "" && h.Model == "" && h.CPU == "" && h.Firmware == "" && h.Uptime == 0 {
		return errWinNoData
	}
	return nil
}

func winMemory(_ Deps, r *Report, v any) error {
	m := winMap(v)
	if m == nil {
		return errWinNoData
	}
	total, ok := winUint(m["total"])
	if !ok || total == 0 {
		return errWinNoData
	}
	r.Memory.TotalBytes = total * 1024
	if free, ok := winUint(m["free"]); ok && free <= total {
		r.Memory.UsedBytes = (total - free) * 1024
	}
	for _, p := range winList(m["swap"]) {
		if a, ok := winUint(p["Alloc"]); ok {
			r.Memory.SwapTotalBytes += a << 20
		}
		if u, ok := winUint(p["Used"]); ok {
			r.Memory.SwapUsedBytes += u << 20
		}
	}
	return nil
}

func winDisks(ids *[]string) func(Deps, *Report, any) error {
	return func(_ Deps, r *Report, v any) error { return winDisksRead(ids, r, v) }
}

func winDisksRead(ids *[]string, r *Report, v any) error {
	m := winMap(v)
	if m == nil {
		return errWinNoData
	}
	free := map[string]uint64{}
	var freeSum uint64
	for _, vol := range winList(m["vols"]) {
		f, ok := winUint(vol["Free"])
		if !ok {
			continue
		}
		free[strings.ToUpper(winStr(vol["Letter"]))] += f
		freeSum += f
	}
	byDisk := map[string]uint64{}
	for _, p := range winList(m["parts"]) {
		if f, ok := free[strings.ToUpper(winStr(p["Letter"]))]; ok {
			byDisk[winStr(p["Disk"])] += f
			delete(free, strings.ToUpper(winStr(p["Letter"])))
		}
	}
	list := winList(m["disks"])
	for _, dm := range list {
		dk := Disk{Name: winStr(dm["Name"]), HealthRaw: winStr(dm["Health"])}
		switch strings.ToUpper(winStr(dm["Media"])) {
		case "SSD":
			dk.Medium = "SSD"
		case "HDD":
			dk.Medium = "HDD"
		}
		if bus := winStr(dm["Bus"]); !strings.EqualFold(bus, "Unknown") && !strings.EqualFold(bus, "Unspecified") {
			dk.Bus = bus
		}
		dk.SizeBytes, _ = winUint(dm["Size"])
		dk.FreeBytes = byDisk[winStr(dm["Id"])]
		switch strings.ToLower(dk.HealthRaw) {
		case "healthy":
			dk.Health = HealthOK
		case "warning":
			dk.Health = HealthWarn
		case "unhealthy":
			dk.Health = HealthBad
		}
		if dk.Name == "" {
			dk.Name = "Disk " + winStr(dm["Id"])
		}
		r.Disks = append(r.Disks, dk)
		*ids = append(*ids, winStr(dm["Id"]))
	}
	if len(r.Disks) == 1 && r.Disks[0].FreeBytes == 0 {
		r.Disks[0].FreeBytes = freeSum
	}
	if len(r.Disks) == 0 {
		return errWinNoData
	}
	return nil
}

// winSmart funde o Get-StorageReliabilityCounter nos discos já lidos, pelo Id (ids[i] é o DeviceId de Disks[i]).
// Lista vazia ou sem nenhum campo aproveitável = sem permissão.
func winSmart(ids []string) func(Deps, *Report, any) error {
	return func(_ Deps, r *Report, v any) error {
		rows := winList(v)
		if len(rows) == 0 {
			return errWinNoData
		}
		merged := false
		for _, row := range rows {
			id := winStr(row["Id"])
			for i := range r.Disks {
				if id == "" || i >= len(ids) || ids[i] != id {
					continue
				}
				dk := &r.Disks[i]
				if t, ok := winNum(row["Temp"]); ok && t > 0 && t < 150 {
					dk.TempC = &t
					merged = true
				}
				if w, ok := winInt(row["Wear"]); ok && w >= 0 && w <= 100 {
					dk.WearPct = &w
					merged = true
					if w >= winWearWarn && dk.Health == HealthOK {
						dk.Health = HealthWarn
					}
				}
				if h, ok := winInt(row["Hours"]); ok && h >= 0 {
					dk.PowerOnHours = &h
					merged = true
				}
			}
		}
		if !merged {
			return errWinNoData
		}
		return nil
	}
}

func winBattery(d Deps, r *Report, v any) error {
	m := winMap(v)
	if m == nil {
		return errWinNoData
	}
	if p, ok := m["present"].(bool); ok && !p {
		return nil
	}
	b := &Battery{}
	if c, ok := winInt(m["charge"]); ok {
		c = winClampPct(c)
		b.Percent = &c
	}
	if s, ok := winInt(m["status"]); ok {
		switch s {
		case 1, 4, 5:
			b.Status = diagfmt.T(d.Lang, "descarregando", "discharging", "descargando")
		case 3:
			b.Status = diagfmt.T(d.Lang, "cheia", "full", "completa")
		case 2, 6, 7, 8, 9, 11:
			b.Status = diagfmt.T(d.Lang, "carregando", "charging", "cargando")
		}
	}
	design, ok1 := winNum(m["design"])
	full, ok2 := winNum(m["full"])
	if ok1 && ok2 && design > 0 && full > 0 {
		h := winClampPct(int(math.Round(full * 100 / design)))
		b.HealthPct = &h
	}
	if b.Percent == nil && b.Status == "" && b.HealthPct == nil {
		return errWinNoData
	}
	r.Battery = b
	return nil
}

func winTemps(_ Deps, r *Report, v any) error {
	for _, row := range winList(v) {
		dk, ok := winNum(row["Dk"])
		if !ok {
			continue
		}
		c := math.Round((dk/10-273.15)*10) / 10
		if c <= 0 || c >= 150 {
			continue
		}
		idx, _ := winInt(row["Idx"])
		r.Temps = append(r.Temps, Temp{Sensor: "ThermalZone " + itoa(idx), Celsius: c})
	}
	if len(r.Temps) == 0 {
		return errWinNoData
	}
	return nil
}

func winGPUs(_ Deps, r *Report, v any) error {
	for _, row := range winList(v) {
		g := GPU{Name: winStr(row["Name"]), Driver: winStr(row["DriverVersion"])}
		if g.Name == "" {
			continue
		}
		g.VRAMBytes, _ = winUint(row["AdapterRAM"])
		r.GPUs = append(r.GPUs, g)
	}
	if len(r.GPUs) == 0 {
		return errWinNoData
	}
	return nil
}

type winEvtGroup struct {
	ev    Event
	level int
}

func winEvents(d Deps, r *Report, v any) error {
	if _, ok := v.([]any); !ok {
		return errWinNoData
	}
	groups := map[string]*winEvtGroup{}
	for _, row := range winList(v) {
		src := winStr(row["P"])
		id := winStr(row["I"])
		if src == "" {
			continue
		}
		when, _ := winTime(row["T"])
		lvl, _ := winInt(row["L"])
		k := src + "\x00" + id
		g := groups[k]
		if g == nil {
			g = &winEvtGroup{ev: Event{Source: src, ID: id, When: when, Message: winMessage(winStr(row["M"]))}, level: lvl}
			groups[k] = g
		} else if when.After(g.ev.When) {
			g.ev.When, g.ev.Message = when, winMessage(winStr(row["M"]))
		}
		if lvl == 1 {
			g.level = 1
		}
		g.ev.Count++
	}
	all := make([]Event, 0, len(groups))
	for _, g := range groups {
		g.ev.Level = diagfmt.T(d.Lang, "erro", "error", "error")
		if g.level == 1 {
			g.ev.Level = diagfmt.T(d.Lang, "crítico", "critical", "crítico")
		}
		all = append(all, g.ev)
	}
	sort.Slice(all, func(i, j int) bool {
		if all[i].Count != all[j].Count {
			return all[i].Count > all[j].Count
		}
		return winEvLess(all[i], all[j])
	})
	if len(all) > winMaxEventGroups {
		all = all[:winMaxEventGroups]
	}
	sort.Slice(all, func(i, j int) bool { return winEvLess(all[i], all[j]) })
	r.Events = all
	return nil
}

// winEvLess ordena o mais recente primeiro, desempatando por origem e id (ordem estável).
func winEvLess(a, b Event) bool {
	if !a.When.Equal(b.When) {
		return a.When.After(b.When)
	}
	if a.Source != b.Source {
		return a.Source < b.Source
	}
	return a.ID < b.ID
}

func winCrashes(_ Deps, r *Report, v any) error {
	if _, ok := v.([]any); !ok {
		return errWinNoData
	}
	var out []Crash
	for _, row := range winList(v) {
		when, ok := winTime(row["T"])
		if !ok {
			continue
		}
		prov, id, msg := winStr(row["P"]), winStr(row["I"]), winStr(row["M"])
		c := Crash{When: when}
		switch {
		case prov == "Microsoft-Windows-WER-SystemErrorReporting" && id == "1001":
			c.Kind = CrashBSOD
			c.Detail = strings.ToLower(winBugcheck.FindString(msg))
		case prov == "Microsoft-Windows-Kernel-Power" && id == "41":
			c.Kind = CrashKernelPower
		case prov == "Application Error" && id == "1000":
			c.Kind = CrashApp
			if m := winFaulting.FindStringSubmatch(msg); m != nil {
				c.Detail = winBase(m[1])
			} else if m := winFaultPth.FindStringSubmatch(msg); m != nil {
				c.Detail = winBase(m[1])
			}
			c.Detail = winMessage(c.Detail)
		default:
			continue
		}
		out = append(out, c)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if !out[i].When.Equal(out[j].When) {
			return out[i].When.After(out[j].When)
		}
		if out[i].Kind != out[j].Kind {
			return out[i].Kind < out[j].Kind
		}
		return out[i].Detail < out[j].Detail
	})
	if len(out) > winMaxCrashes {
		out = out[:winMaxCrashes]
	}
	r.Crashes = out
	return nil
}

func winStability(_ Deps, r *Report, v any) error {
	m := winMap(v)
	if m == nil {
		return errWinNoData
	}
	f, ok := winNum(m["idx"])
	if !ok || f < 0 || f > 10 {
		return errWinNoData
	}
	r.Stability = &f
	return nil
}

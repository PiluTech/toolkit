package otimizacao

import (
	"context"
	"encoding/json"
	"math"
	"sort"
	"strings"
	"time"
)

// Coletor do Windows: cada área é um script de PowerShell de LEITURA (texto fixo, só ASCII, JSON compacto, SilentlyContinue)
// e uma função que lê o JSON com tolerância. Decisões de leitura (docs/otimizacao.md):
//   - nada de Get-Counter: o caminho do contador é traduzido (`\Processor(_Total)\% Processor Time` não existe no Windows em
//     português); as classes CIM Win32_PerfFormattedData_* têm nome fixo em qualquer idioma;
//   - o CPU por processo vem de PerfProc_Process (percentual do momento, somado entre núcleos); o `.CPU` do Get-Process é
//     tempo ACUMULADO e não diz quem está pesando agora;
//   - a latência do disco NÃO vem do contador formatado: Win32_PerfFormattedData_PerfDisk_*.AvgDisksecPerTransfer é um inteiro
//     em SEGUNDOS (30 ms viraria 0). Vem dos contadores BRUTOS (Win32_PerfRawData_PerfDisk_PhysicalDisk): AvgDisksecPerTransfer
//     (ticks acumulados), AvgDisksecPerTransfer_Base (transferências acumuladas) e Frequency_PerfTime (ticks por segundo), em
//     3 amostras. Fórmula do PERF_AVERAGE_TIMER: ((N1 - N0) / F) / (D1 - D0) segundos por transferência, em milissegundos
//     (latencyMs). Amostra única, base sem variação, contador que voltou ou F <= 0 = SEM LEITURA, nunca "0 ms".
//     Fontes: https://learn.microsoft.com/en-us/previous-versions/aa394262(v=vs.85) e a página de cálculo do
//     PERF_AVERAGE_TIMER (docs/otimizacao.md);
//   - o evento 100 de boot é lido com try/catch para separar "sem evento" de "acesso negado" (SilentlyContinue os confunde).
const (
	psPrefs = "$ErrorActionPreference='SilentlyContinue';$ProgressPreference='SilentlyContinue'\n"

	// Amostra de 3 leituras com 1 s entre elas: CPU total e o disco do sistema; depois os processos que mais usam CPU agora.
	scriptPerf = psPrefs +
		`$sd=[string]$env:SystemDrive;$cpu=@();$lat=@()` + "\n" +
		`for($i=0;$i -lt 3;$i++){` +
		`$c=Get-CimInstance Win32_PerfFormattedData_PerfOS_Processor|Where-Object{$_.Name -eq '_Total'};if($c){$cpu+=[double]$c.PercentProcessorTime};` +
		`$w=Get-CimInstance Win32_PerfRawData_PerfDisk_PhysicalDisk|Where-Object{$_.Name -like ('*'+$sd+'*')}|Select-Object -First 1;if($w){$lat+=@{n=[string]$w.AvgDisksecPerTransfer;d=[string]$w.AvgDisksecPerTransfer_Base;f=[string]$w.Frequency_PerfTime;q=[string]$w.AvgDiskQueueLength;t=[string]$w.Timestamp_Sys100NS}};` +
		`if($i -lt 2){Start-Sleep -Seconds 1}}` + "\n" +
		`$lp=[int](Get-CimInstance Win32_ComputerSystem).NumberOfLogicalProcessors` + "\n" +
		`$top=@(Get-CimInstance Win32_PerfFormattedData_PerfProc_Process|Where-Object{$_.Name -ne '_Total' -and $_.Name -ne 'Idle' -and $_.IDProcess -ne $PID}|Sort-Object PercentProcessorTime -Descending|Select-Object -First 5|ForEach-Object{@{Name=[string]$_.Name;Pid=$_.IDProcess;Cpu=$_.PercentProcessorTime}})` + "\n" +
		`@{cpu=$cpu;lat=$lat;lp=$lp;top=$top}|ConvertTo-Json -Compress -Depth 4`

	scriptMem = psPrefs +
		`$cs=Get-CimInstance Win32_ComputerSystem;$m=Get-CimInstance Win32_PerfFormattedData_PerfOS_Memory` + "\n" +
		`$pf=@(Get-CimInstance Win32_PageFileUsage|ForEach-Object{@{Alloc=$_.AllocatedBaseSize;Used=$_.CurrentUsage}})` + "\n" +
		`$top=@(Get-Process|Sort-Object WorkingSet64 -Descending|Select-Object -First 5|ForEach-Object{@{Name=[string]$_.ProcessName;Pid=$_.Id;Mem=$_.WorkingSet64}})` + "\n" +
		`@{total=$cs.TotalPhysicalMemory;avail=$m.AvailableBytes;committed=$m.CommittedBytes;limit=$m.CommitLimit;swap=$pf;top=$top}|ConvertTo-Json -Compress -Depth 4`

	scriptDisk = psPrefs +
		`$sd='C';if($env:SystemDrive){$sd=([string]$env:SystemDrive).Substring(0,1)}` + "\n" +
		`$ld=Get-CimInstance Win32_LogicalDisk -Filter ("DeviceID='"+$sd+":'")` + "\n" +
		`$p=Get-Partition -DriveLetter $sd;$pd=$null;if($p){$pd=Get-PhysicalDisk|Where-Object{[string]$_.DeviceId -eq [string]$p.DiskNumber}|Select-Object -First 1}` + "\n" +
		`@{drive=$sd;size=$ld.Size;free=$ld.FreeSpace;name=[string]$pd.FriendlyName;media=[string]$pd.MediaType;bus=[string]$pd.BusType}|ConvertTo-Json -Compress`

	// Win32_StartupCommand já cobre as chaves Run e as pastas de inicialização; as chaves Run entram também (duplicata sai em Go).
	scriptStartup = psPrefs +
		`$a=@(Get-CimInstance Win32_StartupCommand|ForEach-Object{@{Name=[string]$_.Name;Cmd=[string]$_.Command;Loc=[string]$_.Location}})` + "\n" +
		`$r=@();foreach($k in 'HKCU:\Software\Microsoft\Windows\CurrentVersion\Run','HKLM:\Software\Microsoft\Windows\CurrentVersion\Run'){$p=Get-ItemProperty -Path $k;if($p){foreach($x in $p.PSObject.Properties){if($x.Name -notin 'PSPath','PSParentPath','PSChildName','PSDrive','PSProvider'){$r+=@{Name=[string]$x.Name;Cmd=[string]$x.Value;Loc=$k}}}}}` + "\n" +
		`@{wmi=$a;run=$r}|ConvertTo-Json -Compress -Depth 4`

	scriptServices = psPrefs +
		`$s=@(Get-CimInstance Win32_Service|Where-Object{$_.StartMode -eq 'Auto'}|ForEach-Object{@{N=[string]$_.Name;D=[string]$_.DisplayName;S=[string]$_.State;E=$_.ExitCode;P=[string]$_.PathName}})` + "\n" +
		`ConvertTo-Json -Compress -Depth 4 -InputObject $s`

	// Evento 100 do Diagnostics-Performance (boot). status: ok, none (sem evento), nolog (log ausente), denied, error.
	// Os nomes dos campos do XML (BootTime, BootIsDegradation) vêm de relato da comunidade: A VERIFICAR em máquina real;
	// campo ausente = sem dado.
	scriptBoot = psPrefs +
		`$r=@{status='none'}` + "\n" +
		`try{$e=Get-WinEvent -FilterHashtable @{LogName='Microsoft-Windows-Diagnostics-Performance/Operational';Id=100} -MaxEvents 1 -ErrorAction Stop` + "\n" +
		`if($e){$x=[xml]$e.ToXml();$d=@{};foreach($n in $x.Event.EventData.Data){$d[[string]$n.Name]=[string]$n.'#text'}` + "\n" +
		`$r=@{status='ok';t=$e.TimeCreated.ToUniversalTime().ToString('o');boot=$d['BootTime'];degr=$d['BootIsDegradation']}}}` + "\n" +
		`catch{$i=[string]$_.FullyQualifiedErrorId` + "\n" +
		`if($i -like '*NoMatchingEventsFound*'){$r=@{status='none'}}elseif($i -like '*UnauthorizedAccess*'){$r=@{status='denied'}}elseif($i -like '*NoMatchingLogsFound*'){$r=@{status='nolog'}}else{$r=@{status='error'}}}` + "\n" +
		`$r|ConvertTo-Json -Compress`
)

// allScripts lista todo script que o pacote pode emitir (o teste de contrato os percorre).
var allScripts = []string{scriptPerf, scriptMem, scriptDisk, scriptStartup, scriptServices, scriptBoot}

const (
	perfTimeout = 2 * time.Minute // a amostra leva ~2 s, mas o CIM de desempenho pode demorar na primeira consulta
	readTimeout = time.Minute

	maxList  = 25 // itens de cada lista curta (a contagem é sempre a inteira)
	maxTop   = 5
	maxItems = 4000 // teto de itens lidos de uma lista do sistema (defesa contra saída gigante)
)

// Collect lê os gargalos do SO indicado. Fora do Windows não executa nada: Report.Supported=false. Cada área é
// independente: uma que falha vira Skipped e a coleta segue. Nunca devolve erro.
func Collect(ctx context.Context, goos string, d Deps) *Report {
	r := &Report{OS: goos, Generated: d.now(), Admin: d.Admin}
	if goos != "windows" {
		return r
	}
	r.Supported = true
	collectWindows(ctx, d, r)
	return r
}

func collectWindows(ctx context.Context, d Deps, r *Report) {
	skip := func(area, pt, en, es string) {
		r.Skipped = append(r.Skipped, Skipped{Area: area, Reason: d.t(pt, en, es)})
	}
	// area roda um script e entrega o JSON ao leitor. Contexto cancelado, erro do comando, saída vazia, JSON inválido ou
	// leitor com erro/pânico = área sem leitura.
	area := func(script string, timeout time.Duration, read func(v any) error) (ok bool) {
		if ctx.Err() != nil {
			return false
		}
		defer func() {
			if recover() != nil {
				ok = false
			}
		}()
		out, err := d.run(ctx, psCmd(script, timeout))
		if err != nil || strings.TrimSpace(out) == "" {
			return false
		}
		var v any
		if json.Unmarshal([]byte(strings.TrimSpace(strings.TrimPrefix(out, "\ufeff"))), &v) != nil {
			return false
		}
		return read(v) == nil
	}
	cancelled := func(area string) bool {
		if ctx.Err() == nil {
			return false
		}
		skip(area, "leitura cancelada", "reading cancelled", "lectura cancelada")
		return true
	}

	// 1. amostra de desempenho (primeiro e sozinha: os outros PowerShell não podem inflar a medida de CPU)
	var perf perfSample
	cpuOK := false
	if cancelled("cpu") {
		skip("memoria", "leitura cancelada", "reading cancelled", "lectura cancelada")
		skip("disco", "leitura cancelada", "reading cancelled", "lectura cancelada")
		skip("inicializacao", "leitura cancelada", "reading cancelled", "lectura cancelada")
		skip("servicos", "leitura cancelada", "reading cancelled", "lectura cancelada")
		skip("boot", "leitura cancelada", "reading cancelled", "lectura cancelada")
		return
	}
	if area(scriptPerf, perfTimeout, func(v any) error {
		p, err := readPerf(v)
		perf = p
		return err
	}) {
		r.CPU, cpuOK = perf.cpu()
	}
	if !cpuOK {
		skip("cpu", "não foi possível ler o uso da CPU (PowerShell/CIM indisponível ou amostra sem valores válidos); rode em um PowerShell normal",
			"could not read CPU usage (PowerShell/CIM unavailable or no valid samples); run from a regular PowerShell",
			"no se pudo leer el uso de la CPU (PowerShell/CIM no disponible o muestra sin valores válidos); ejecute en un PowerShell normal")
	}

	if !cancelled("memoria") && !area(scriptMem, readTimeout, func(v any) error { return readMemory(v, r) }) {
		skip("memoria", "não foi possível ler a memória; rode em um PowerShell normal",
			"could not read the memory; run from a regular PowerShell",
			"no se pudo leer la memoria; ejecute en un PowerShell normal")
	} else if r.Memory != nil && r.Memory.AvailableBytes == nil {
		skip("memoria", "a RAM disponível não foi lida (o resto da memória foi); nada foi avaliado sobre ela",
			"the available RAM was not read (the rest of the memory was); nothing was assessed about it",
			"la RAM disponible no se leyó (el resto de la memoria sí); no se evaluó nada sobre ella")
	}
	if !cancelled("disco") && !area(scriptDisk, readTimeout, func(v any) error { return readDisk(v, r, perf) }) {
		skip("disco", "não foi possível ler o disco do sistema; rode em um PowerShell normal",
			"could not read the system disk; run from a regular PowerShell",
			"no se pudo leer el disco del sistema; ejecute en un PowerShell normal")
	}
	if !cancelled("inicializacao") && !area(scriptStartup, readTimeout, func(v any) error { return readStartup(v, r) }) {
		skip("inicializacao", "não foi possível ler os itens de inicialização; rode em um PowerShell normal",
			"could not read the startup items; run from a regular PowerShell",
			"no se pudieron leer los elementos de inicio; ejecute en un PowerShell normal")
	}
	if !cancelled("servicos") && !area(scriptServices, readTimeout, func(v any) error { return readServices(v, r) }) {
		skip("servicos", "não foi possível ler os serviços automáticos; rode em um PowerShell normal",
			"could not read the automatic services; run from a regular PowerShell",
			"no se pudieron leer los servicios automáticos; ejecute en un PowerShell normal")
	}
	if cancelled("boot") {
		return
	}
	status := ""
	if !area(scriptBoot, readTimeout, func(v any) error { return readBoot(v, r, &status) }) || status == "denied" || status == "error" {
		r.Boot = nil
		switch {
		case status == "denied" && !d.Admin:
			r.Skipped = append(r.Skipped, Skipped{Area: "boot", NeedsAdmin: true, Reason: d.t(
				"o log de desempenho do boot (Diagnostics-Performance) não pôde ser lido sem permissão: abra o terminal como administrador e rode de novo",
				"the boot performance log (Diagnostics-Performance) could not be read without permission: open the terminal as administrator and run again",
				"no se pudo leer el registro de rendimiento del arranque (Diagnostics-Performance) sin permiso: abra el terminal como administrador y ejecute de nuevo")})
		case status == "denied":
			skip("boot", "o Windows negou a leitura do log de desempenho do boot mesmo como administrador; o tempo de boot fica sem dado",
				"Windows denied reading the boot performance log even as administrator; the boot time stays without data",
				"Windows negó la lectura del registro de rendimiento del arranque incluso como administrador; el tiempo de arranque queda sin dato")
		default:
			skip("boot", "não foi possível ler o log de desempenho do boot; o tempo de boot fica sem dado",
				"could not read the boot performance log; the boot time stays without data",
				"no se pudo leer el registro de rendimiento del arranque; el tiempo de arranque queda sin dato")
		}
	}
}

// ---- leitores de cada área ----

// perfSample é a amostra do script de desempenho: CPU total, fila e latência do disco do sistema e os processos.
type perfSample struct {
	cpuSamples []float64
	lat        []rawLat
	logical    int
	top        []Process
}

// rawLat é uma amostra dos contadores brutos do disco: n = ticks acumulados (AvgDisksecPerTransfer), d = transferências
// acumuladas (AvgDisksecPerTransfer_Base) e f = ticks por segundo (Frequency_PerfTime).
type rawLat struct {
	n, d, f float64 // latência (PERF_AVERAGE_TIMER)
	// q = AvgDiskQueueLength BRUTO (integral acumulada, PERF_COUNTER_100NS_QUEUELEN_TYPE) e t = Timestamp_Sys100NS (100 ns):
	// a fila média entre duas amostras é (q1-q0)/(t1-t0). O valor formatado do CIM é INTEIRO e trunca (1,9 vira 1).
	q, t float64
	hasQ bool
}

// queueStats calcula a fila média (entre a primeira e a última amostra) e o pico (maior média entre amostras vizinhas) pelos
// contadores brutos. Menos de 2 amostras com q e t, tempo que não avança ou contador que voltou = sem leitura (nil), nunca 0.
func (p perfSample) queueStats() (avg, peak *float64) {
	var qs []rawLat
	for _, e := range p.lat {
		if e.hasQ {
			qs = append(qs, e)
		}
	}
	if len(qs) < 2 {
		return nil, nil
	}
	rate := func(a, b rawLat) (float64, bool) {
		dt, dq := b.t-a.t, b.q-a.q
		if dt <= 0 || dq < 0 {
			return 0, false
		}
		v := dq / dt
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return 0, false
		}
		return v, true
	}
	total, ok := rate(qs[0], qs[len(qs)-1])
	if !ok {
		return nil, nil
	}
	pk := 0.0
	for i := 1; i < len(qs); i++ {
		v, ok := rate(qs[i-1], qs[i])
		if !ok {
			return nil, nil // um intervalo inválido invalida a medida (nada de média parcial)
		}
		pk = max(pk, v)
	}
	return &total, &pk
}

// latencyMs aplica a fórmula do PERF_AVERAGE_TIMER sobre TODA a janela: média ponderada pelas transferências, ou seja
// ((N1-N0)/F)/(D1-D0) entre a primeira e a última amostra, em ms. Cada intervalo consecutivo é validado (contador e base
// monotônicos, F > 0 e constante; base parada só com contador parado, o disco ocioso): UM intervalo inválido invalida a
// medida inteira, porque a primeira e a última amostra sozinhas dariam um valor plausível e enganoso. Menos de 2 amostras
// ou D sem aumento no total = sem leitura (nil), nunca 0 ms.
func (p perfSample) latencyMs() *float64 {
	if len(p.lat) < 2 || p.lat[0].f <= 0 {
		return nil
	}
	for i := 1; i < len(p.lat); i++ {
		a, b := p.lat[i-1], p.lat[i]
		dn, dd := b.n-a.n, b.d-a.d
		if b.f != a.f || dn < 0 || dd < 0 || (dd == 0 && dn != 0) {
			return nil
		}
	}
	a, b := p.lat[0], p.lat[len(p.lat)-1]
	dn, dd := b.n-a.n, b.d-a.d
	if dd <= 0 {
		return nil
	}
	ms := dn / b.f / dd * 1000
	if math.IsNaN(ms) || math.IsInf(ms, 0) {
		return nil
	}
	return &ms
}

func readPerf(v any) (perfSample, error) {
	m := asMap(v)
	if m == nil {
		return perfSample{}, errNoData
	}
	p := perfSample{cpuSamples: numList(m["cpu"])}
	for _, e := range asList(m["lat"]) { // só amostra COMPLETA (n, d e f presentes); número solto (formato antigo) não é leitura
		n, ok1 := num(e["n"])
		d, ok2 := num(e["d"])
		f, ok3 := num(e["f"])
		q, ok4 := num(e["q"])
		tt, ok5 := num(e["t"])
		if ok1 && ok2 && ok3 {
			p.lat = append(p.lat, rawLat{n: n, d: d, f: f, q: q, t: tt, hasQ: ok4 && ok5})
		}
	}
	p.logical, _ = inum(m["lp"])
	if p.logical <= 0 {
		p.logical = 1 // sem a contagem de processadores o percentual por processo não é dividido (não se inventa número)
	}
	for _, e := range asList(m["top"]) {
		name := procName(str(e["Name"]))
		cpu, ok := num(e["Cpu"])
		if name == "" || !ok || cpu < 0 {
			continue
		}
		pid, _ := inum(e["Pid"])
		p.top = append(p.top, Process{Name: name, PID: pid, CPUPct: cpu / float64(p.logical)})
	}
	sort.SliceStable(p.top, func(i, j int) bool { return p.top[i].CPUPct > p.top[j].CPUPct })
	if len(p.top) > maxTop {
		p.top = p.top[:maxTop]
	}
	return p, nil
}

// cpu devolve o uso de CPU; amostras vazias ou TODAS zero são "sem leitura válida" (a primeira leitura formatada do CIM
// pode vir zerada), nunca "CPU ociosa".
func (p perfSample) cpu() (*CPU, bool) {
	var sum, peak float64
	nonZero := false
	for _, s := range p.cpuSamples {
		if s < 0 || s > 100 {
			continue
		}
		sum += s
		peak = max(peak, s)
		if s > 0 {
			nonZero = true
		}
	}
	n := 0
	for _, s := range p.cpuSamples {
		if s >= 0 && s <= 100 {
			n++
		}
	}
	if n == 0 || !nonZero {
		return nil, false
	}
	return &CPU{Samples: n, AvgPct: sum / float64(n), PeakPct: peak, Logical: p.logical, Top: p.top}, true
}

func readMemory(v any, r *Report) error {
	m := asMap(v)
	if m == nil {
		return errNoData
	}
	total, ok := unum(m["total"])
	if !ok || total == 0 {
		return errNoData
	}
	mem := &Memory{TotalBytes: total}
	// PRESENÇA importa: "avail" ausente é dado ausente (nil), nunca "0 B disponíveis"; um zero real é uma medida.
	if av, ok := unum(m["avail"]); ok {
		mem.AvailableBytes = &av
	}
	mem.CommitBytes, _ = unum(m["committed"])
	mem.CommitLimit, _ = unum(m["limit"])
	for _, e := range asList(m["swap"]) {
		a, _ := unum(e["Alloc"]) // em MB
		u, _ := unum(e["Used"])
		mem.PageAllocBytes += a << 20
		mem.PageUsedBytes += u << 20
	}
	for _, e := range asList(m["top"]) {
		name := procName(str(e["Name"]))
		b, ok := unum(e["Mem"])
		if name == "" || !ok {
			continue
		}
		pid, _ := inum(e["Pid"])
		mem.Top = append(mem.Top, Process{Name: name, PID: pid, MemBytes: b})
	}
	sort.SliceStable(mem.Top, func(i, j int) bool { return mem.Top[i].MemBytes > mem.Top[j].MemBytes })
	if len(mem.Top) > maxTop {
		mem.Top = mem.Top[:maxTop]
	}
	if !mem.measured() { // só o total: nada a avaliar, a área fica sem leitura
		return errNoData
	}
	r.Memory = mem
	return nil
}

func readDisk(v any, r *Report, p perfSample) error {
	m := asMap(v)
	drive := ""
	if m != nil {
		drive = strings.ToUpper(str(m["drive"]))
	}
	if drive == "" {
		return errNoData
	}
	d := &Disk{Drive: drive, Name: str(m["name"]), Bus: str(m["bus"])}
	// só "SSD" e "HDD" valem; Unspecified (máquina virtual, RAID) e vazio são desconhecidos, nunca HDD
	switch strings.ToUpper(str(m["media"])) {
	case "SSD":
		d.Medium = "SSD"
	case "HDD":
		d.Medium = "HDD"
	}
	if sz, ok := unum(m["size"]); ok && sz > 0 {
		d.SizeBytes = sz
	}
	if f, ok := unum(m["free"]); ok {
		d.FreeBytes = &f
	}
	d.QueueAvg, d.QueuePeak = p.queueStats()
	d.LatencyMs = p.latencyMs()
	if !d.measured() { // só o "drive" (que o script sempre preenche) não é leitura: a área fica sem leitura
		return errNoData
	}
	r.Disk = d
	return nil
}

func readStartup(v any, r *Report) error {
	m := asMap(v)
	if m == nil {
		return errNoData
	}
	seen := map[string]bool{}
	var items []StartupItem
	for _, src := range []string{"wmi", "run"} {
		for _, e := range asList(m[src]) {
			name := str(e["Name"])
			key := strings.ToLower(name)
			if name == "" || seen[key] || len(seen) >= maxItems {
				continue
			}
			seen[key] = true
			items = append(items, StartupItem{Name: name, Command: baseProgram(str(e["Cmd"])), Where: str(e["Loc"])})
		}
	}
	sort.SliceStable(items, func(i, j int) bool { return strings.ToLower(items[i].Name) < strings.ToLower(items[j].Name) })
	s := &Startup{Total: len(items), Items: items}
	if len(s.Items) > maxList {
		s.Items = s.Items[:maxList]
	}
	r.Startup = s
	return nil
}

// insideWindows diz se o executável do serviço está na pasta do Windows (heurística: o resto é "fora da pasta do Windows").
func insideWindows(path string) bool {
	p := strings.ToLower(strings.Trim(strings.TrimSpace(path), `"`))
	if p == "" {
		return true // sem caminho não se afirma nada: não conta como "de fora"
	}
	return strings.HasPrefix(p, `%systemroot%\`) || strings.HasPrefix(p, `\systemroot\`) || strings.HasPrefix(p, `system32\`) ||
		(len(p) > 10 && p[1] == ':' && strings.HasPrefix(p[2:], `\windows\`))
}

func readServices(v any, r *Report) error {
	list := asList(v)
	if len(list) == 0 {
		return errNoData
	}
	s := &Services{}
	var outside, failed []Service
	for _, e := range list {
		name := str(e["N"])
		if name == "" {
			continue
		}
		s.AutoCount++
		code, _ := inum(e["E"])
		sv := Service{Name: name, Display: str(e["D"]), State: str(e["S"]), ExitCode: code, Program: baseProgram(str(e["P"]))}
		if !insideWindows(str(e["P"])) {
			s.OutsideCount++
			outside = append(outside, sv)
		}
		// parado com erro: ExitCode 0 é parada normal e 1077 é "nunca tentou iniciar" (gatilho ou início atrasado)
		if strings.EqualFold(sv.State, "stopped") && code != 0 && code != 1077 {
			failed = append(failed, sv)
		}
	}
	if s.AutoCount == 0 {
		return errNoData
	}
	byName := func(l []Service) []Service {
		sort.SliceStable(l, func(i, j int) bool { return strings.ToLower(l[i].Name) < strings.ToLower(l[j].Name) })
		if len(l) > maxList {
			l = l[:maxList]
		}
		return l
	}
	s.Outside, s.Failed = byName(outside), byName(failed)
	r.Services = s
	return nil
}

// readBoot lê o status do script do boot. denied/error voltam pelo status (o chamador monta o Skipped); none e nolog são
// "sem dado" (Boot com Found=false), NUNCA "boot rápido".
func readBoot(v any, r *Report, status *string) error {
	m := asMap(v)
	if m == nil {
		return errNoData
	}
	*status = strings.ToLower(str(m["status"]))
	switch *status {
	case "ok":
		b := &Boot{Found: true, When: parseTime(m["t"])}
		b.DurationMs, _ = inum(m["boot"])
		switch strings.ToLower(str(m["degr"])) {
		case "true":
			t := true
			b.Degraded = &t
		case "false":
			f := false
			b.Degraded = &f
		}
		r.Boot = b
	case "none", "nolog":
		r.Boot = &Boot{Found: false}
	case "denied", "error":
		return errNoData
	default:
		return errNoData
	}
	return nil
}

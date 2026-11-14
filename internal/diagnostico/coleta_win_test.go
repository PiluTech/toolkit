package diagnostico

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/PiluTech/toolkit/internal/diagfmt"
	"github.com/PiluTech/toolkit/internal/exec"
	"github.com/PiluTech/toolkit/internal/exec/exectest"
)

var winNow = time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)

const (
	winHWJSON   = `{"cs":{"Manufacturer":"Acme","Model":"Laptop 1","TotalPhysicalMemory":17179869184},"cpu":[{"Name":"Chip X 3.0GHz","Cores":8,"Threads":16}],"bios":"F.20","uptimeSec":93784}`
	winMemJSON  = `{"total":16777216,"free":4194304,"swap":[{"Alloc":2048,"Used":512}]}`
	winDiskJSON = `{"disks":[{"Id":"0","Name":"SSD One","Media":"SSD","Bus":"NVMe","Health":"Healthy","Size":512110190592},{"Id":"1","Name":"Disk Two","Media":"HDD","Bus":"SATA","Health":"Warning","Size":1000204886016}],"parts":[{"Disk":0,"Letter":"C"},{"Disk":1,"Letter":"D"}],"vols":[{"Letter":"C","Free":100000000000},{"Letter":"D","Free":500000000000}]}`
	winSmartOK  = `[{"Id":"0","Temp":41,"Wear":12,"Hours":1234},{"Id":"1","Temp":35,"Wear":null,"Hours":"99"}]`
	winBatJSON  = `{"present":true,"charge":80,"status":6,"design":50000,"full":40000}`
	winTempJSON = `[{"Idx":0,"Dk":3181}]`
	winGPUJSON  = `[{"Name":"GPU Z","DriverVersion":"31.0.1","AdapterRAM":4293918720}]`
	winEvtJSON  = `[` +
		`{"T":"2026-11-12T10:00:00.1234567Z","P":"Svc","I":7000,"L":2,"M":"Falha ao iniciar C:\\Users\\bob\\x.exe\nsegunda linha"},` +
		`{"T":"2026-11-13T09:00:00Z","P":"Svc","I":7000,"L":1,"M":"Mais recente"},` +
		`{"T":"2026-11-11T09:00:00Z","P":"Other","I":1,"L":2,"M":"Outro"}]`
	winCrashJSON = `[` +
		`{"T":"2026-11-10T08:00:00Z","P":"Microsoft-Windows-WER-SystemErrorReporting","I":1001,"L":2,"M":"The computer has rebooted from a bugcheck.  The bugcheck was: 0x0000009f (0x3)."},` +
		`{"T":"2026-11-12T08:00:00Z","P":"Microsoft-Windows-Kernel-Power","I":41,"L":1,"M":"The system has rebooted"},` +
		`{"T":"2026-11-11T08:00:00Z","P":"Application Error","I":1000,"L":2,"M":"Faulting application name: foo.exe, version: 1.0\nFaulting module path: C:\\Users\\bob\\AppData\\foo.dll"},` +
		`{"T":"2026-11-09T08:00:00Z","P":"Application Error","I":1000,"L":2,"M":"Faulting application path: C:\\Users\\bob\\Apps\\bar.exe"}]`
	winStabJSON = `{"idx":"8.5"}`
)

type winScripts map[string]string

func winAll() winScripts {
	return winScripts{
		winScriptHW: winHWJSON, winScriptMem: winMemJSON, winScriptDisks: winDiskJSON, winScriptSmart: winSmartOK,
		winScriptBattery: winBatJSON, winScriptTemp: winTempJSON, winScriptGPU: winGPUJSON,
		winScriptEvents: winEvtJSON, winScriptCrashes: winCrashJSON, winScriptStability: winStabJSON,
	}
}

func winRun(t *testing.T, s winScripts, lang diagfmt.Lang) (*Report, *exectest.FakeRunner) {
	t.Helper()
	f := exectest.New(t)
	for sc, out := range s {
		f.On(psCmd(sc, time.Minute).String(), exectest.Exit(0, out))
	}
	return winRunWith(t, f, lang)
}

func winRunWith(t *testing.T, f *exectest.FakeRunner, lang diagfmt.Lang) (r *Report, _ *exectest.FakeRunner) {
	t.Helper()
	r = &Report{OS: "windows"}
	d := Deps{Runner: f, Lang: lang, Now: func() time.Time { return winNow }}
	collectWindows(context.Background(), d, r)
	for _, c := range f.Calls() {
		if c.Elevate || !c.ReadOnly {
			t.Errorf("comando não é de leitura: %v", c)
		}
	}
	return r, f
}

func winSkipped(r *Report, area string) (Skipped, bool) {
	for _, s := range r.Skipped {
		if s.Area == area {
			return s, true
		}
	}
	return Skipped{}, false
}

func TestWinCaminhoFeliz(t *testing.T) {
	r, f := winRun(t, winAll(), diagfmt.PT)
	if len(r.Skipped) != 0 {
		t.Fatalf("skipped inesperado: %+v", r.Skipped)
	}
	if len(f.Calls()) != 10 {
		t.Errorf("comandos: %d", len(f.Calls()))
	}
	h := r.Hardware
	if h.Vendor != "Acme" || h.Model != "Laptop 1" || h.CPU != "Chip X 3.0GHz" || h.Cores != 8 || h.Threads != 16 || h.Firmware != "F.20" || h.Uptime != 93784*time.Second {
		t.Errorf("hardware: %+v", h)
	}
	m := r.Memory
	if m.TotalBytes != 16777216*1024 || m.UsedBytes != (16777216-4194304)*1024 || m.SwapTotalBytes != 2048<<20 || m.SwapUsedBytes != 512<<20 {
		t.Errorf("memória: %+v", m)
	}
	if len(r.Disks) != 2 {
		t.Fatalf("discos: %+v", r.Disks)
	}
	d0, d1 := r.Disks[0], r.Disks[1]
	if d0.Name != "SSD One" || d0.Medium != "SSD" || d0.Bus != "NVMe" || d0.SizeBytes != 512110190592 || d0.FreeBytes != 100000000000 || d0.Health != HealthOK || d0.HealthRaw != "Healthy" {
		t.Errorf("disco 0: %+v", d0)
	}
	if d0.TempC == nil || *d0.TempC != 41 || d0.WearPct == nil || *d0.WearPct != 12 || d0.PowerOnHours == nil || *d0.PowerOnHours != 1234 {
		t.Errorf("smart 0: %+v", d0)
	}
	if d1.Health != HealthWarn || d1.Medium != "HDD" || d1.FreeBytes != 500000000000 || d1.WearPct != nil || d1.PowerOnHours == nil || *d1.PowerOnHours != 99 {
		t.Errorf("disco 1: %+v", d1)
	}
	b := r.Battery
	if b == nil || b.Percent == nil || *b.Percent != 80 || b.Status != "carregando" || b.HealthPct == nil || *b.HealthPct != 80 {
		t.Fatalf("bateria: %+v", b)
	}
	if len(r.Temps) != 1 || r.Temps[0].Celsius != 45 {
		t.Errorf("temp: %+v", r.Temps)
	}
	if len(r.GPUs) != 1 || r.GPUs[0].Name != "GPU Z" || r.GPUs[0].Driver != "31.0.1" || r.GPUs[0].VRAMBytes != 4293918720 {
		t.Errorf("gpu: %+v", r.GPUs)
	}
	if len(r.Events) != 2 {
		t.Fatalf("eventos: %+v", r.Events)
	}
	e := r.Events[0]
	if e.Source != "Svc" || e.ID != "7000" || e.Count != 2 || e.Level != "crítico" || e.Message != "Mais recente" || !e.When.Equal(time.Date(2026, 11, 13, 9, 0, 0, 0, time.UTC)) {
		t.Errorf("evento 0: %+v", e)
	}
	if r.Events[1].Source != "Other" {
		t.Errorf("ordem: %+v", r.Events)
	}
	if len(r.Crashes) != 4 {
		t.Fatalf("falhas: %+v", r.Crashes)
	}
	want := []struct {
		k CrashKind
		d string
	}{{CrashKernelPower, ""}, {CrashApp, "foo.exe"}, {CrashBSOD, "0x0000009f"}, {CrashApp, "bar.exe"}}
	for i, w := range want {
		if r.Crashes[i].Kind != w.k || r.Crashes[i].Detail != w.d {
			t.Errorf("falha %d: %+v", i, r.Crashes[i])
		}
	}
	if r.Stability == nil || *r.Stability != 8.5 {
		t.Errorf("estabilidade: %v", r.Stability)
	}
}

func TestWinEventoMensagemLimpaETruncada(t *testing.T) {
	long := strings.Repeat("a", 500)
	s := winAll()
	s[winScriptEvents] = `[{"T":"2026-11-12T10:00:00Z","P":"P","I":1,"L":2,"M":"C:\\Users\\bob\\x falhou /home/ana/y ` + long + `\nsegunda"}]`
	r, _ := winRun(t, s, diagfmt.PT)
	m := r.Events[0].Message
	if strings.Contains(m, "bob") || strings.Contains(m, "ana") || strings.Contains(m, "segunda") {
		t.Errorf("vazou: %q", m)
	}
	if n := len([]rune(m)); n > 200 {
		t.Errorf("tamanho %d", n)
	}
}

func TestWinTop30PorContagem(t *testing.T) {
	var sb strings.Builder
	sb.WriteString("[")
	for i := range 40 {
		if i > 0 {
			sb.WriteString(",")
		}
		sb.WriteString(`{"T":"2026-11-12T10:00:00Z","P":"P` + itoa(i) + `","I":1,"L":2,"M":"m"}`)
		if i < 5 { // 5 grupos com contagem 2
			sb.WriteString(`,{"T":"2026-11-12T09:00:00Z","P":"P` + itoa(i) + `","I":1,"L":2,"M":"m"}`)
		}
	}
	sb.WriteString("]")
	s := winAll()
	s[winScriptEvents] = sb.String()
	r, _ := winRun(t, s, diagfmt.PT)
	if len(r.Events) != 30 {
		t.Fatalf("eventos: %d", len(r.Events))
	}
	n2 := 0
	for _, e := range r.Events {
		if e.Count == 2 {
			n2++
		}
	}
	if n2 != 5 {
		t.Errorf("os 5 grupos mais frequentes devem entrar: %d", n2)
	}
}

func TestWinAreaFalhandoIsolada(t *testing.T) {
	areas := map[string]string{
		"hardware": winScriptHW, "memoria": winScriptMem, "discos": winScriptDisks, "bateria": winScriptBattery,
		"gpu": winScriptGPU, "eventos": winScriptEvents, "falhas": winScriptCrashes, "estabilidade": winScriptStability,
	}
	for area, sc := range areas {
		t.Run(area, func(t *testing.T) {
			s := winAll()
			f := exectest.New(t)
			for k, out := range s {
				if k == sc {
					f.On(psCmd(k, time.Minute).String(), exectest.Exit(1, ""))
				} else {
					f.On(psCmd(k, time.Minute).String(), exectest.Exit(0, out))
				}
			}
			r, _ := winRunWith(t, f, diagfmt.PT)
			if len(r.Skipped) != 1 || r.Skipped[0].Area != area || r.Skipped[0].Reason == "" {
				t.Fatalf("skipped: %+v", r.Skipped)
			}
			// as outras áreas seguem
			if area != "hardware" && r.Hardware.Vendor == "" || area != "memoria" && r.Memory.TotalBytes == 0 ||
				area != "gpu" && len(r.GPUs) == 0 || area != "eventos" && len(r.Events) == 0 {
				t.Errorf("área independente quebrou: %+v", r)
			}
		})
	}
}

func TestWinErroDoRunnerEVazioETruncadoViramSkipped(t *testing.T) {
	for name, reply := range map[string]exectest.Reply{
		"erro":      {Err: errors.New("boom")},
		"vazio":     exectest.Exit(0, ""),
		"truncado":  exectest.Exit(0, `{"cs":{"Manu`),
		"tipo":      exectest.Exit(0, `"texto"`),
		"nulo":      exectest.Exit(0, `null`),
		"lixo-json": exectest.Exit(0, `[1,2,"x",null]`),
	} {
		t.Run(name, func(t *testing.T) {
			f := exectest.New(t)
			for sc := range winAll() {
				f.On(psCmd(sc, time.Minute).String(), reply)
			}
			r, _ := winRunWith(t, f, diagfmt.PT)
			if r.Battery != nil {
				t.Errorf("bateria: %+v", r.Battery)
			}
			if len(r.Skipped) == 0 {
				t.Errorf("deveria haver Skipped")
			}
		})
	}
}

func TestWinSmartAusenteVirouSkippedComInstrucao(t *testing.T) {
	for _, out := range []string{"", "[]", "not json"} {
		s := winAll()
		s[winScriptSmart] = out
		r, _ := winRun(t, s, diagfmt.PT)
		sk, ok := winSkipped(r, "smart")
		if !ok || !strings.Contains(sk.Reason, "administrador") {
			t.Errorf("%q: %+v", out, r.Skipped)
		}
		if len(r.Disks) != 2 || r.Disks[0].TempC != nil || r.Disks[0].Health != HealthOK {
			t.Errorf("discos sem smart: %+v", r.Disks)
		}
	}
}

func TestWinDesgasteAltoViraAtencao(t *testing.T) {
	s := winAll()
	s[winScriptSmart] = `[{"Id":"0","Temp":40,"Wear":80,"Hours":1}]`
	r, _ := winRun(t, s, diagfmt.PT)
	if r.Disks[0].Health != HealthWarn || r.Disks[0].HealthRaw != "Healthy" {
		t.Errorf("%+v", r.Disks[0])
	}
	s[winScriptSmart] = `[{"Id":"0","Temp":40,"Wear":79,"Hours":1}]`
	r, _ = winRun(t, s, diagfmt.PT)
	if r.Disks[0].Health != HealthOK {
		t.Errorf("79 não é warn: %+v", r.Disks[0])
	}
}

func TestWinSaudeDoDisco(t *testing.T) {
	for raw, want := range map[string]Health{"Healthy": HealthOK, "Warning": HealthWarn, "Unhealthy": HealthBad, "": HealthUnknown, "Weird": HealthUnknown} {
		s := winAll()
		s[winScriptDisks] = `{"disks":[{"Id":"0","Name":"D","Health":"` + raw + `","Size":1}]}`
		r, _ := winRun(t, s, diagfmt.PT)
		if len(r.Disks) != 1 || r.Disks[0].Health != want {
			t.Errorf("%q: %+v", raw, r.Disks)
		}
	}
}

func TestWinDiscoUnicoSemAssociacaoSomaVolumes(t *testing.T) {
	s := winAll()
	s[winScriptDisks] = `{"disks":{"Id":"0","Name":"D","Health":"Healthy","Size":10},"vols":[{"Letter":"C","Free":3},{"Letter":"D","Free":4}]}`
	r, _ := winRun(t, s, diagfmt.PT)
	if len(r.Disks) != 1 || r.Disks[0].FreeBytes != 7 {
		t.Errorf("%+v", r.Disks)
	}
}

func TestWinSemBateriaNaoEhSkipped(t *testing.T) {
	s := winAll()
	s[winScriptBattery] = `{"present":false}`
	r, _ := winRun(t, s, diagfmt.PT)
	if r.Battery != nil || len(r.Skipped) != 0 {
		t.Errorf("%+v %+v", r.Battery, r.Skipped)
	}
}

func TestWinBateriaLimites(t *testing.T) {
	s := winAll()
	s[winScriptBattery] = `{"present":true,"charge":"150","status":3,"design":1000,"full":2000}`
	r, _ := winRun(t, s, diagfmt.EN)
	b := r.Battery
	if b == nil || *b.Percent != 100 || *b.HealthPct != 100 || b.Status != "full" {
		t.Errorf("%+v", b)
	}
	s[winScriptBattery] = `{"present":true,"charge":50,"status":1,"design":null,"full":null}`
	r, _ = winRun(t, s, diagfmt.PT)
	if r.Battery == nil || r.Battery.HealthPct != nil || r.Battery.Status != "descarregando" {
		t.Errorf("%+v", r.Battery)
	}
}

func TestWinTemperaturaIndisponivel(t *testing.T) {
	for _, out := range []string{"", "[]", `[{"Idx":0,"Dk":0}]`, `[{"Idx":0,"Dk":99999}]`} {
		s := winAll()
		s[winScriptTemp] = out
		r, _ := winRun(t, s, diagfmt.ES)
		if _, ok := winSkipped(r, "temperatura"); !ok || len(r.Temps) != 0 {
			t.Errorf("%q: %+v %+v", out, r.Temps, r.Skipped)
		}
	}
}

func TestWinIdiomaDoSkipped(t *testing.T) {
	s := winAll()
	s[winScriptTemp] = ""
	pt, _ := winRun(t, s, diagfmt.PT)
	en, _ := winRun(t, s, diagfmt.EN)
	if pt.Skipped[0].Reason == en.Skipped[0].Reason {
		t.Error("idiomas iguais")
	}
}

func TestWinSemEventosEhValido(t *testing.T) {
	s := winAll()
	s[winScriptEvents] = `[]`
	s[winScriptCrashes] = `[]`
	r, _ := winRun(t, s, diagfmt.PT)
	if len(r.Events) != 0 || len(r.Crashes) != 0 || len(r.Skipped) != 0 {
		t.Errorf("%+v", r.Skipped)
	}
}

func TestWinHardwareNumerosComoTexto(t *testing.T) {
	s := winAll()
	s[winScriptHW] = `{"cs":{"Manufacturer":"A","Model":"B"},"cpu":{"Name":"C","Cores":"4","Threads":"8"},"uptimeSec":"60"}`
	r, _ := winRun(t, s, diagfmt.PT)
	h := r.Hardware
	if h.Cores != 4 || h.Threads != 8 || h.Uptime != time.Minute {
		t.Errorf("%+v", h)
	}
}

func TestWinCpuComVariosSoquetes(t *testing.T) {
	s := winAll()
	s[winScriptHW] = `{"cpu":[{"Name":"C","Cores":4,"Threads":8},{"Name":"C","Cores":4,"Threads":8}]}`
	r, _ := winRun(t, s, diagfmt.PT)
	if r.Hardware.Cores != 8 || r.Hardware.Threads != 16 {
		t.Errorf("%+v", r.Hardware)
	}
}

func TestWinScriptsSaoASCIIEComSilencio(t *testing.T) {
	for sc := range winAll() {
		for _, c := range sc {
			if c > 127 {
				t.Fatalf("não ASCII: %q", c)
			}
		}
		if !strings.Contains(sc, "$ErrorActionPreference='SilentlyContinue'") || !strings.Contains(sc, "$ProgressPreference='SilentlyContinue'") {
			t.Errorf("sem preferências: %s", sc)
		}
		if strings.Contains(sc, "RunAs") || strings.Contains(sc, "Start-Process") {
			t.Errorf("elevação: %s", sc)
		}
	}
}

func TestWinNaoUsaDadosPessoais(t *testing.T) {
	for sc := range winAll() {
		for _, bad := range []string{"SerialNumber", "UUID", "MACAddress", "UserName", "CSName", "DNSHostName"} {
			if strings.Contains(sc, bad) {
				t.Errorf("script lê %s", bad)
			}
		}
	}
}

var _ = exec.Cmd{}

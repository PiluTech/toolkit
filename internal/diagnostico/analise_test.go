package diagnostico

import (
	"strings"
	"testing"
	"time"

	"github.com/PiluTech/toolkit/internal/diagfmt"
)

func ip(n int) *int         { return &n }
func fp(f float64) *float64 { return &f }
func find(fs []Finding, area string, sev Severity) *Finding {
	for i := range fs {
		if fs[i].Area == area && fs[i].Severity == sev {
			return &fs[i]
		}
	}
	return nil
}

func TestAnalyzeSemProblemaDizQueEstaTudoBem(t *testing.T) {
	r := &Report{OS: "linux", Memory: Memory{TotalBytes: 16 << 30, UsedBytes: 4 << 30},
		Disks:   []Disk{{Name: "SSD", SizeBytes: 500e9, FreeBytes: 300e9, Health: HealthOK, WearPct: ip(10)}},
		Battery: &Battery{HealthPct: ip(90)}, Temps: []Temp{{Sensor: "CPU", Celsius: 50}}, Stability: fp(9.5)}
	fs := Analyze(r, diagfmt.PT)
	if len(fs) != 1 || fs[0].Severity != SevOK || !strings.Contains(fs[0].Title, "Nada de ruim") {
		t.Fatalf("%+v", fs)
	}
}

func TestAnalyzeCadaRegraComAcao(t *testing.T) {
	cases := []struct {
		name string
		r    Report
		area string
		sev  Severity
		want string // trecho do título/ação
	}{
		{"smart ruim", Report{Disks: []Disk{{Name: "D1", Health: HealthBad, HealthRaw: "Failing"}}}, "disco", SevBad, "backup AGORA"},
		{"smart atenção", Report{Disks: []Disk{{Name: "D1", Health: HealthWarn, HealthRaw: "Warning"}}}, "disco", SevWarn, "backup dos dados"},
		{"desgaste 85", Report{Disks: []Disk{{Name: "D1", WearPct: ip(85)}}}, "disco", SevWarn, "desgastado"},
		{"desgaste 95", Report{Disks: []Disk{{Name: "D1", WearPct: ip(95)}}}, "disco", SevBad, "desgastado"},
		{"realocados", Report{Disks: []Disk{{Name: "D1", ReallocatedSectors: ip(3)}}}, "disco", SevWarn, "realocados"},
		{"realocados muitos", Report{Disks: []Disk{{Name: "D1", ReallocatedSectors: ip(500)}}}, "disco", SevBad, "realocados"},
		{"pouco espaço", Report{Disks: []Disk{{Name: "D1", SizeBytes: 1000, FreeBytes: 80}}}, "disco", SevWarn, "Pouco espaço"},
		{"quase sem espaço", Report{Disks: []Disk{{Name: "D1", SizeBytes: 1000, FreeBytes: 30}}}, "disco", SevBad, "Pouco espaço"},
		{"disco quente", Report{Disks: []Disk{{Name: "D1", TempC: fp(70)}}}, "temperatura", SevWarn, "Disco quente"},
		{"bateria 50%", Report{Battery: &Battery{HealthPct: ip(50)}}, "bateria", SevWarn, "Bateria gasta"},
		{"bateria 30%", Report{Battery: &Battery{HealthPct: ip(30)}}, "bateria", SevBad, "Bateria gasta"},
		{"bateria service", Report{Battery: &Battery{Condition: "Service Recommended"}}, "bateria", SevWarn, "serviço"},
		{"bateria replace", Report{Battery: &Battery{Condition: "Replace Now"}}, "bateria", SevBad, "troca"},
		{"cpu quente", Report{Temps: []Temp{{Sensor: "CPU", Celsius: 88}}}, "temperatura", SevWarn, "Temperatura alta"},
		{"cpu fervendo", Report{Temps: []Temp{{Sensor: "CPU", Celsius: 95}}}, "temperatura", SevBad, "Temperatura alta"},
		{"memória cheia", Report{Memory: Memory{TotalBytes: 100, UsedBytes: 95}}, "memoria", SevWarn, "quase toda"},
		{"swap cheio", Report{Memory: Memory{TotalBytes: 100, UsedBytes: 10, SwapTotalBytes: 100, SwapUsedBytes: 90}}, "memoria", SevWarn, "swap"},
		{"erro repetido", Report{Events: []Event{{Source: "disk", ID: "7", Count: 30}}}, "erros", SevWarn, "Erro repetido"},
		{"bsod 1", Report{Crashes: []Crash{{Kind: CrashBSOD, Detail: "0x0000007E"}}}, "falhas", SevWarn, "Tela azul"},
		{"bsod 3", Report{Crashes: []Crash{{Kind: CrashBSOD}, {Kind: CrashBSOD}, {Kind: CrashBSOD}}}, "falhas", SevBad, "Tela azul"},
		{"kernel-power", Report{Crashes: []Crash{{Kind: CrashKernelPower}}}, "falhas", SevWarn, "Desligamento"},
		{"panic", Report{Crashes: []Crash{{Kind: CrashKernelPanic}}}, "falhas", SevBad, "kernel panic"},
		{"app trava 5x", Report{Crashes: []Crash{{Kind: CrashApp, Detail: "x.exe"}, {Kind: CrashApp, Detail: "x.exe"}, {Kind: CrashApp, Detail: "x.exe"}, {Kind: CrashApp, Detail: "x.exe"}, {Kind: CrashApp, Detail: "x.exe"}}}, "falhas", SevWarn, "x.exe"},
		{"confiabilidade baixa", Report{Stability: fp(4.1)}, "sistema", SevWarn, "confiabilidade"},
		{"confiabilidade péssima", Report{Stability: fp(2.0)}, "sistema", SevBad, "confiabilidade"},
	}
	for _, c := range cases {
		r := c.r
		fs := Analyze(&r, diagfmt.PT)
		f := find(fs, c.area, c.sev)
		if f == nil || !strings.Contains(f.Title+" "+f.Action, c.want) || f.Action == "" {
			t.Errorf("%s: achado %s/%v com %q e ação não encontrado: %+v", c.name, c.area, c.sev, c.want, fs)
		}
		if fs[0].Severity < c.sev {
			t.Errorf("%s: o mais grave deve vir primeiro: %+v", c.name, fs)
		}
	}
}

func TestAnalyzeLimitesNaoDisparam(t *testing.T) {
	r := &Report{
		Disks:   []Disk{{Name: "D", SizeBytes: 1000, FreeBytes: 100, WearPct: ip(79), TempC: fp(64), ReallocatedSectors: ip(0)}},
		Battery: &Battery{HealthPct: ip(60), Condition: "Normal"}, Temps: []Temp{{Sensor: "CPU", Celsius: 79}},
		Memory: Memory{TotalBytes: 100, UsedBytes: 89, SwapTotalBytes: 100, SwapUsedBytes: 79},
		Events: []Event{{Source: "x", Count: 19}}, Crashes: []Crash{{Kind: CrashApp, Detail: "a"}, {Kind: CrashApp, Detail: "a"}}, Stability: fp(5.0),
	}
	if fs := Analyze(r, diagfmt.PT); len(fs) != 1 || fs[0].Severity != SevOK {
		t.Fatalf("limites exatos não disparam: %+v", fs)
	}
}

func TestAnalyzeSkippedViraInfoComOMotivoESemTirarOTudoOk(t *testing.T) {
	r := &Report{Skipped: []Skipped{{Area: "smart", Reason: "rode sudo smartctl -H -A /dev/sda"}}}
	fs := Analyze(r, diagfmt.PT)
	if len(fs) != 2 || fs[0].Severity != SevOK || fs[1].Severity != SevInfo || !strings.Contains(fs[1].Detail, "sudo smartctl") {
		t.Fatalf("%+v", fs)
	}
	r.Disks = []Disk{{Name: "D", Health: HealthBad}}
	fs = Analyze(r, diagfmt.PT)
	if fs[0].Severity != SevBad || fs[len(fs)-1].Severity != SevInfo {
		t.Fatalf("ordem por gravidade: %+v", fs)
	}
}

func TestAnalyzeNosTresIdiomas(t *testing.T) {
	r := &Report{Disks: []Disk{{Name: "D1", Health: HealthBad, HealthRaw: "Failing"}}}
	for l, want := range map[diagfmt.Lang]string{diagfmt.PT: "Disco com problema", diagfmt.EN: "Failing disk", diagfmt.ES: "Disco con problemas"} {
		fs := Analyze(r, l)
		if !strings.Contains(fs[0].Title, want) || fs[0].Action == "" {
			t.Errorf("%v: %+v", l, fs[0])
		}
	}
}

var _ = time.Second

func TestAreaNameTraduzIdsEstaveis(t *testing.T) {
	for id, want := range map[string]string{"smart": "saúde SMART dos discos", "temperatura": "temperatura", "eventos": "erros do log do sistema", "xyz": "xyz"} {
		if got := areaName(id, diagfmt.PT); got != want {
			t.Errorf("%s: %q, quer %q", id, got, want)
		}
	}
	if got := areaName("smart", diagfmt.EN); got != "disk SMART health" {
		t.Errorf("en: %q", got)
	}
	r := &Report{Skipped: []Skipped{{Area: "temperatura", Reason: "x"}}}
	if fs := Analyze(r, diagfmt.EN); !strings.Contains(fs[len(fs)-1].Title, "Could not read: temperature") {
		t.Errorf("%+v", fs)
	}
}

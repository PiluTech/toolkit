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

const (
	macTProfHW = `{"SPHardwareDataType":[{"_name":"hardware_overview","chip_type":"Apple M4","machine_name":"MacBook Air","number_processors":"proc 10:4:6:0","physical_memory":"24 GB","platform_UUID":"00000000-0000-4000-8000-000000000001","serial_number":"SN-FAKE-0001"}]}`
	macTDisks  = `{"SPNVMeDataType":[{"_items":[{"_name":"APPLE SSD AP0512Z","device_model":"APPLE SSD AP0512Z","device_serial":"DSN-FAKE-0001","size_in_bytes":500277792768,"smart_status":"Verified"}],"_name":"Apple SSD Controller"}],
"SPStorageDataType":[
{"_name":"Data","free_space_in_bytes":20580208640,"size_in_bytes":494384795648,"physical_drive":{"device_name":"APPLE SSD AP0512Z","medium_type":"ssd","protocol":"Apple Fabric","smart_status":"Verified"}},
{"_name":"Macintosh HD","free_space_in_bytes":20000000000,"physical_drive":{"device_name":"APPLE SSD AP0512Z","medium_type":"ssd"}},
{"_name":"Sim","free_space_in_bytes":457043968,"physical_drive":{"device_name":"Disk Image","medium_type":"ssd"}}]}`
	macTPower = `{"SPPowerDataType":[{"_name":"spbattery_information","sppower_battery_charge_info":{"sppower_battery_fully_charged":"FALSE","sppower_battery_is_charging":"TRUE","sppower_battery_state_of_charge":63},
"sppower_battery_health_info":{"sppower_battery_cycle_count":171,"sppower_battery_health":"Good","sppower_battery_health_maximum_capacity":"97%"}},{"_name":"sppower_information"}]}`
	macTGPU   = `{"SPDisplaysDataType":[{"_name":"Apple M4","sppci_model":"Apple M4"},{"sppci_model":"AMD Radeon Pro 5500M","spdisplays_vram":"8 GB"}]}`
	macTPorts = "Hardware Port: Wi-Fi\nDevice: en0\nEthernet Address: 02:00:00:00:00:01\n\nHardware Port: Thunderbolt Bridge\nDevice: bridge0\nEthernet Address: N/A\n\nHardware Port: Ethernet Adapter (en3)\nDevice: en3\nEthernet Address: 02:00:00:00:00:02\n\nVLAN Configuration\n===============\n"
	macTPM    = "*** SMC sensors ***\n\nCPU die temperature: 45.12 C\nGPU die temperature: 41.00 C\nFan: 1200 rpm\n"
	macTVM    = "Mach Virtual Memory Statistics: (page size of 16384 bytes)\nPages free:   4719.\nPages active:   393500.\nPages wired down:   282668.\nPages occupied by compressor:   483589.\n"
	macTLog   = `Timestamp               Ty Process[PID:TID]
2026-11-13 22:38:26.734 E  Chrome Helper[45034:304a23e] [com.apple.corevideo:] CVDisplayLink error -6670 at /Users/joe/x
2026-11-13 22:39:26.734 E  Chrome Helper[45035:304a23f] [com.apple.corevideo:] CVDisplayLink error -6671 at /Users/ann/x
2026-11-13 22:40:00.000 E  kernel[0:f6d] (IOSurface) task: 0x1 not found
linha solta
`
)

var macNow = time.Date(2026, 11, 13, 23, 0, 0, 0, time.UTC)

type macScript map[string]string // linha -> stdout; "" = falha (exit 1)

func macFake(t *testing.T, s macScript) *exectest.FakeRunner {
	f := exectest.New(t)
	for line, out := range s {
		if out == "!" {
			f.On(line, exectest.Exit(1, ""))
		} else {
			f.On(line, exectest.Exit(0, out))
		}
	}
	return f
}

func macFull() macScript {
	return macScript{
		"/usr/sbin/sysctl -n hw.model":                                                         "Mac16,12\n",
		"/usr/sbin/sysctl -n machdep.cpu.brand_string":                                         "!",
		"/usr/sbin/sysctl -n hw.physicalcpu":                                                   "10\n",
		"/usr/sbin/sysctl -n hw.logicalcpu":                                                    "10\n",
		"/usr/sbin/sysctl -n kern.boottime":                                                    "{ sec = 1794268850, usec = 110507 } Mon\n",
		"/usr/sbin/system_profiler SPHardwareDataType -json":                                   macTProfHW,
		"/usr/sbin/sysctl -n hw.memsize":                                                       "25769803776\n",
		"/usr/sbin/scutil --get ComputerName":                                                  "HOST-FAKE\n",
		"/usr/sbin/networksetup -listallhardwareports":                                         macTPorts,
		"/usr/bin/vm_stat":                                                                     macTVM,
		"/usr/sbin/sysctl -n vm.swapusage":                                                     "total = 11264.00M  used = 10320.00M  free = 943.56M  (encrypted)\n",
		"/usr/sbin/system_profiler SPNVMeDataType SPSerialATADataType SPStorageDataType -json": macTDisks,
		"/usr/sbin/system_profiler SPPowerDataType -json":                                      macTPower,
		"/usr/sbin/system_profiler SPDisplaysDataType -json":                                   macTGPU,
		"/usr/bin/log show --last 6h --style compact --predicate \"messageType == error\"":     macTLog,
	}
}

func macDeps(f exec.Runner) Deps {
	return Deps{
		Runner: f,
		Now:    func() time.Time { return macNow },
		Lang:   diagfmt.PT,
		Home:   "/Users/x",
		Glob: func(p string) ([]string, error) {
			switch p {
			case "/Users/x/Library/Logs/DiagnosticReports/*.ips":
				return []string{
					"/Users/x/Library/Logs/DiagnosticReports/Safari-2026-11-12-101500.ips",
					"/Users/x/Library/Logs/DiagnosticReports/Old_2026-09-01-101500_host.ips",
					"/Users/x/Library/Logs/DiagnosticReports/Kernel_2026-11-10-080000_host.ips",
				}, nil
			case "/Library/Logs/DiagnosticReports/*.panic":
				return []string{"/Library/Logs/DiagnosticReports/panic-full-2026-11-11-090000.0002.panic"}, nil
			case "/Library/Logs/DiagnosticReports/*.crash":
				return []string{"/Library/Logs/DiagnosticReports/Notes_2026-11-13-010000_host.crash", "/Library/Logs/DiagnosticReports/NoDate_host.crash"}, nil
			}
			return nil, nil
		},
	}
}

func macCollect(t *testing.T, s macScript) (*Report, *exectest.FakeRunner) {
	f := macFake(t, s)
	r := &Report{OS: "darwin"}
	collectDarwin(context.Background(), macDeps(f), r)
	return r, f
}

func macSkipped(r *Report, area string) bool {
	for _, s := range r.Skipped {
		if s.Area == area {
			return true
		}
	}
	return false
}

func TestMacCaminhoFeliz(t *testing.T) {
	r, f := macCollect(t, macFull())
	h := r.Hardware
	if h.Vendor != "Apple" || h.Model != "Mac16,12" || h.CPU != "Apple M4" || h.Cores != 10 || h.Threads != 10 {
		t.Fatalf("hardware: %+v", h)
	}
	if h.Uptime <= 0 {
		t.Fatalf("uptime: %v", h.Uptime)
	}
	wantUsed := uint64(393500+282668+483589) * 16384
	if r.Memory.TotalBytes != 25769803776 || r.Memory.UsedBytes != wantUsed {
		t.Fatalf("memória: %+v", r.Memory)
	}
	if r.Memory.SwapTotalBytes != 11264<<20 || r.Memory.SwapUsedBytes != 10320<<20 {
		t.Fatalf("swap: %+v", r.Memory)
	}
	if len(r.Disks) != 1 {
		t.Fatalf("discos: %+v", r.Disks)
	}
	dk := r.Disks[0]
	if dk.Name != "APPLE SSD AP0512Z" || dk.Bus != "NVMe" || dk.Medium != "SSD" || dk.Health != HealthOK || dk.HealthRaw != "Verified" || dk.FreeBytes != 20580208640 || dk.SizeBytes != 500277792768 {
		t.Fatalf("disco: %+v", dk)
	}
	b := r.Battery
	if b == nil || *b.Percent != 63 || *b.CycleCount != 171 || *b.HealthPct != 97 || b.Condition != "Good" || b.Status != "carregando" {
		t.Fatalf("bateria: %+v", b)
	}
	if len(r.GPUs) != 2 || r.GPUs[0].Name != "AMD Radeon Pro 5500M" || r.GPUs[0].VRAMBytes != 8<<30 || r.GPUs[1].VRAMBytes != 0 {
		t.Fatalf("gpu: %+v", r.GPUs)
	}
	// os caminhos aparecem como o sistema os informou (nada é mascarado): /Users/ann e /Users/joe são eventos distintos
	if len(r.Events) != 3 {
		t.Fatalf("eventos: %+v", r.Events)
	}
	if e := r.Events[0]; e.Source != "kernel" || e.Count != 1 {
		t.Fatalf("evento 0: %+v", e)
	}
	e := r.Events[1]
	if e.Source != "Chrome Helper" || e.Count != 1 || !strings.Contains(e.Message, "/Users/ann/x") || !e.When.Equal(time.Date(2026, 11, 13, 22, 39, 26, 0, time.UTC)) {
		t.Fatalf("evento 1: %+v", e)
	}
	if h.Serial != "SN-FAKE-0001" || h.UUID != "00000000-0000-4000-8000-000000000001" || h.Hostname != "HOST-FAKE" {
		t.Fatalf("identificação: %+v", h)
	}
	if dk.Serial != "DSN-FAKE-0001" {
		t.Fatalf("serial do disco: %+v", dk)
	}
	if len(r.NICs) != 2 || r.NICs[0] != (NIC{"Wi-Fi", "02:00:00:00:00:01"}) || r.NICs[1] != (NIC{"Ethernet Adapter (en3)", "02:00:00:00:00:02"}) {
		t.Fatalf("NICs: %+v", r.NICs)
	}
	if !macSkipped(r, "temperatura") || len(r.Skipped) != 1 || !r.Skipped[0].NeedsAdmin || !strings.Contains(r.Skipped[0].Reason, "sudo toolkit diagnostico") {
		t.Fatalf("skipped: %+v", r.Skipped)
	}
	// falhas: Notes (13/11), Safari (12/11), panic (11/11), Kernel .ips (10/11), NoDate (zero); Old fora dos 30 dias.
	want := []struct {
		kind   CrashKind
		detail string
	}{{CrashApp, "Notes"}, {CrashApp, "Safari"}, {CrashKernelPanic, "kernel"}, {CrashKernelPanic, "kernel"}, {CrashApp, "NoDate"}}
	if len(r.Crashes) != len(want) {
		t.Fatalf("falhas: %+v", r.Crashes)
	}
	for i, w := range want {
		if r.Crashes[i].Kind != w.kind || r.Crashes[i].Detail != w.detail {
			t.Fatalf("falha %d: %+v", i, r.Crashes[i])
		}
	}
	for _, c := range f.Calls() {
		if c.Elevate || !c.ReadOnly {
			t.Fatalf("comando não é de leitura: %+v", c)
		}
	}
	for _, c := range f.Calls() {
		if c.Name == macLog && c.Timeout != 90*time.Second {
			t.Fatalf("timeout do log: %v", c.Timeout)
		}
	}
}

func TestMacAreaFalhandoIsolada(t *testing.T) {
	cases := []struct {
		area string
		kill []string
		chk  func(*Report) bool
	}{
		{"hardware", []string{"/usr/sbin/sysctl -n hw.model", "/usr/sbin/sysctl -n hw.physicalcpu", "/usr/sbin/sysctl -n hw.logicalcpu", "/usr/sbin/sysctl -n kern.boottime", "/usr/sbin/system_profiler SPHardwareDataType -json"}, func(r *Report) bool { return r.Memory.TotalBytes > 0 }},
		{"memoria", []string{"/usr/sbin/sysctl -n hw.memsize"}, func(r *Report) bool { return r.Hardware.Model != "" }},
		{"discos", []string{"/usr/sbin/system_profiler SPNVMeDataType SPSerialATADataType SPStorageDataType -json"}, func(r *Report) bool { return r.Battery != nil }},
		{"bateria", []string{"/usr/sbin/system_profiler SPPowerDataType -json"}, func(r *Report) bool { return len(r.Disks) == 1 }},
		{"gpu", []string{"/usr/sbin/system_profiler SPDisplaysDataType -json"}, func(r *Report) bool { return len(r.Disks) == 1 }},
		{"eventos", []string{"/usr/bin/log show --last 6h --style compact --predicate \"messageType == error\""}, func(r *Report) bool { return len(r.Crashes) == 5 }},
	}
	for _, c := range cases {
		t.Run(c.area, func(t *testing.T) {
			s := macFull()
			for _, k := range c.kill {
				s[k] = "!"
			}
			r, _ := macCollect(t, s)
			if !macSkipped(r, c.area) {
				t.Fatalf("faltou Skipped %s: %+v", c.area, r.Skipped)
			}
			if !c.chk(r) {
				t.Fatalf("as outras áreas deviam seguir: %+v", r)
			}
		})
	}
}

func TestMacSaidasRuins(t *testing.T) {
	bad := []string{"", "{", "null", `{"SPPowerDataType":"x","SPDisplaysDataType":[1,2],"SPNVMeDataType":{"a":1},"SPStorageDataType":5}`, `[]`, "lixo \x00 \x1b[31m"}
	for _, b := range bad {
		s := macFull()
		for k := range s {
			if strings.HasPrefix(k, "/usr/sbin/system_profiler") || k == "/usr/bin/vm_stat" || strings.Contains(k, "swapusage") {
				s[k] = b
			}
		}
		s["/usr/sbin/sysctl -n hw.memsize"] = b
		r, _ := macCollect(t, s) // não pode entrar em pânico
		if !macSkipped(r, "temperatura") {
			t.Fatalf("%q: %+v", b, r.Skipped)
		}
	}
}

func TestMacSemBateriaNaoViraSkipped(t *testing.T) {
	s := macFull()
	s["/usr/sbin/system_profiler SPPowerDataType -json"] = `{"SPPowerDataType":[{"_name":"sppower_information"}]}`
	r, _ := macCollect(t, s)
	if r.Battery != nil || macSkipped(r, "bateria") {
		t.Fatalf("%+v %+v", r.Battery, r.Skipped)
	}
}

func TestMacSMART(t *testing.T) {
	for raw, want := range map[string]Health{"Verified": HealthOK, "Failing": HealthBad, "Not Supported": HealthUnknown, "": HealthUnknown} {
		s := macFull()
		s["/usr/sbin/system_profiler SPNVMeDataType SPSerialATADataType SPStorageDataType -json"] = `{"SPNVMeDataType":[{"_items":[{"_name":"X","size_in_bytes":"1000","smart_status":"` + raw + `"}]}]}`
		r, _ := macCollect(t, s)
		if len(r.Disks) != 1 || r.Disks[0].Health != want || r.Disks[0].HealthRaw != raw || r.Disks[0].SizeBytes != 1000 {
			t.Fatalf("%q: %+v", raw, r.Disks)
		}
	}
}

func TestMacLimites(t *testing.T) {
	// bateria com percentual fora da faixa e capacidade como número; vm_stat acima do total; swap em G.
	s := macFull()
	s["/usr/sbin/system_profiler SPPowerDataType -json"] = `{"SPPowerDataType":[{"sppower_battery_charge_info":{"sppower_battery_state_of_charge":"250"},"sppower_battery_health_info":{"sppower_battery_health_maximum_capacity":120}}]}`
	s["/usr/sbin/sysctl -n hw.memsize"] = "1048576\n"
	s["/usr/sbin/sysctl -n vm.swapusage"] = "total = 2.00G  used = 3.00G  free = 0M\n"
	f := macFake(t, s)
	f.On("/usr/bin/pmset -g batt", exectest.Exit(0, "Now drawing from 'Battery Power'\n -InternalBattery-0 (id=1)\t80%; discharging; 3:00 remaining\n"))
	r := &Report{}
	collectDarwin(context.Background(), macDeps(f), r)
	if r.Battery == nil || r.Battery.Percent == nil || *r.Battery.Percent != 80 || r.Battery.HealthPct != nil {
		t.Fatalf("bateria: %+v", r.Battery)
	}
	if r.Memory.UsedBytes != 1048576 || r.Memory.SwapTotalBytes != 2<<30 || r.Memory.SwapUsedBytes != 2<<30 {
		t.Fatalf("memória: %+v", r.Memory)
	}
}

func TestMacEventosTruncadosELimite(t *testing.T) {
	var sb strings.Builder
	for i := 0; i < 60; i++ {
		sb.WriteString("2026-11-13 10:00:00.000 E  proc" + strings.Repeat("a", i) + "[1:2] msg " + strings.Repeat("x", 500) + "\n")
	}
	s := macFull()
	s["/usr/bin/log show --last 6h --style compact --predicate \"messageType == error\""] = sb.String()
	r, _ := macCollect(t, s)
	if len(r.Events) != 30 || len([]rune(r.Events[0].Message)) > 200 {
		t.Fatalf("eventos: %d", len(r.Events))
	}
	// saída acima de 2 MB é cortada sem pânico
	s["/usr/bin/log show --last 6h --style compact --predicate \"messageType == error\""] = strings.Repeat("2026-11-13 10:00:00.000 E  p[1:2] m\n", 200000)
	r, _ = macCollect(t, s)
	if len(r.Events) != 1 || r.Events[0].Count < 50000 {
		t.Fatalf("%+v", r.Events)
	}
}

func TestMacLogComSaidaParcialMasExitErro(t *testing.T) {
	s := macFull()
	f := macFake(t, s)
	r := &Report{}
	collectDarwin(context.Background(), macDeps(f), r)
	_ = f
	s2 := macFull()
	delete(s2, "/usr/bin/log show --last 6h --style compact --predicate \"messageType == error\"")
	f2 := macFake(t, s2)
	f2.On("/usr/bin/log show --last 6h --style compact --predicate \"messageType == error\"", exectest.Reply{Err: errors.New("timeout")})
	r2 := &Report{}
	collectDarwin(context.Background(), macDeps(f2), r2)
	if !macSkipped(r2, "eventos") {
		t.Fatalf("%+v", r2.Skipped)
	}
}

func TestMacGlobComErroViraSkipped(t *testing.T) {
	f := macFake(t, macFull())
	d := macDeps(f)
	d.Glob = func(string) ([]string, error) { return nil, errors.New("boom") }
	r := &Report{}
	collectDarwin(context.Background(), d, r)
	if !macSkipped(r, "falhas") || len(r.Events) == 0 {
		t.Fatalf("%+v", r.Skipped)
	}
}

func TestMacIdiomaEN(t *testing.T) {
	f := macFake(t, macFull())
	d := macDeps(f)
	d.Lang = diagfmt.EN
	r := &Report{}
	collectDarwin(context.Background(), d, r)
	if r.Battery.Status != "charging" || !strings.Contains(r.Skipped[0].Reason, "sudo toolkit diagnostico") {
		t.Fatalf("%+v %+v", r.Battery, r.Skipped)
	}
}

func TestMacIdentificadoresAparecemEValoresVaziosCaem(t *testing.T) {
	s := macFull()
	s["/usr/sbin/system_profiler SPHardwareDataType -json"] = `{"SPHardwareDataType":[{"chip_type":"Apple M4","serial_number":"0000000000","platform_UUID":"00000000-0000-0000-0000-000000000000"}]}`
	s["/usr/sbin/scutil --get ComputerName"] = "!"
	f := macFake(t, s)
	f.On("/bin/hostname", exectest.Exit(0, "host-fake.local\n"))
	r := &Report{}
	collectDarwin(context.Background(), macDeps(f), r)
	if r.Hardware.Serial != "" || r.Hardware.UUID != "" || r.Hardware.Hostname != "host-fake.local" {
		t.Fatalf("%+v", r.Hardware)
	}
}

func TestMacRedeFalhaViraSkipped(t *testing.T) {
	s := macFull()
	s["/usr/sbin/networksetup -listallhardwareports"] = "!"
	r, _ := macCollect(t, s)
	if !macSkipped(r, "rede") || len(r.NICs) != 0 || r.Hardware.Model == "" {
		t.Fatalf("%+v", r.Skipped)
	}
}

func macAdminDeps(f exec.Runner) Deps {
	d := macDeps(f)
	d.Admin = true
	return d
}

func TestMacAdminTemperatura(t *testing.T) {
	s := macFull()
	s["/usr/bin/powermetrics --samplers smc -n 1 -i 1000"] = macTPM
	f := macFake(t, s)
	r := &Report{}
	collectDarwin(context.Background(), macAdminDeps(f), r)
	if len(r.Temps) != 2 || r.Temps[0].Sensor != "CPU die temperature" || r.Temps[0].Celsius != 45.12 || r.Temps[1].Celsius != 41 {
		t.Fatalf("temps: %+v", r.Temps)
	}
	if macSkipped(r, "temperatura") {
		t.Fatalf("%+v", r.Skipped)
	}
	for _, c := range f.Calls() {
		if c.Elevate || !c.ReadOnly {
			t.Fatalf("comando não é de leitura: %+v", c)
		}
	}
}

func TestMacAdminSemSensores(t *testing.T) {
	s := macFull()
	s["/usr/bin/powermetrics --samplers smc -n 1 -i 1000"] = "!"
	s["/usr/bin/powermetrics --samplers thermal -n 1 -i 1000"] = "Current pressure level: Nominal\n"
	f := macFake(t, s)
	r := &Report{}
	collectDarwin(context.Background(), macAdminDeps(f), r)
	if len(r.Temps) != 0 || !macSkipped(r, "temperatura") {
		t.Fatalf("%+v %+v", r.Temps, r.Skipped)
	}
	for _, k := range r.Skipped {
		if k.Area == "temperatura" && (k.NeedsAdmin || !strings.Contains(k.Reason, "sensores") || !strings.Contains(k.Reason, "Nominal")) {
			t.Fatalf("%+v", k)
		}
	}
}

func TestMacAdminSemSensoresEThermalFalha(t *testing.T) {
	s := macFull()
	s["/usr/bin/powermetrics --samplers smc -n 1 -i 1000"] = "sem nada\n"
	s["/usr/bin/powermetrics --samplers thermal -n 1 -i 1000"] = "!"
	r := &Report{}
	collectDarwin(context.Background(), macAdminDeps(macFake(t, s)), r)
	if len(r.Temps) != 0 || !macSkipped(r, "temperatura") {
		t.Fatalf("%+v", r.Skipped)
	}
}

package diagnostico

import (
	"context"
	"errors"
	"os"
	"path"
	"strings"
	"testing"
	"time"

	"github.com/PiluTech/toolkit/internal/diagfmt"
	"github.com/PiluTech/toolkit/internal/exec"
	"github.com/PiluTech/toolkit/internal/exec/exectest"
)

const (
	linLsblk = `{"blockdevices":[
 {"name":"nvme0n1","size":512110190592,"type":"disk","rota":false,"model":"Fake NVMe 512","tran":"nvme"},
 {"name":"sda","size":"1000204886016","type":"disk","rota":"1","model":"Fake HDD 1TB","tran":"sata"},
 {"name":"loop0","size":1000,"type":"loop","rota":false,"model":null,"tran":null}]}`
	linDf = `Filesystem     Avail          1B-blocks
/dev/nvme0n1p2 100000000000 200000000000
/dev/nvme0n1p1 500000000 600000000
/dev/sda1 300000000000 900000000000
tmpfs 1 2
/dev/mapper/x 5 6
`
	linLspci = `00:02.0 "VGA compatible controller" "Intel Corporation" "UHD Graphics 620" -r07 "Dell" "0123"
01:00.0 "3D controller" "NVIDIA Corporation" "GP108M [GeForce MX150]" -ra1 "Dell" "0456"
00:1f.3 "Audio device" "Intel Corporation" "Sunrise Point-LP HD Audio" -r21 "Dell" "0789"
`
	linJournalOut = `2026-11-13T10:00:00+0000 hostx nm-dispatcher[812]: error 12 in /home/alice/foo
second line
2026-11-13T09:00:00+0000 hostx nm-dispatcher[813]: error 99 in /home/bob/foo
2026-11-13T11:00:00+0000 hostx kernel: ata1: link down
-- Boot abc --
lixo sem formato
`
	linCrashOut = `2026-11-12T10:00:00+0000 hostx kernel: Kernel panic - not syncing: Fatal exception
2026-11-12T11:00:00+0000 hostx kernel: Out of memory: Killed process 4321 (firefox) total-vm:123kB
2026-11-12T12:00:00+0000 hostx kernel: chrome[777]: segfault at 0 ip 0000 sp 0000 error 4
`
	linMeminfo = "MemTotal:       16000000 kB\nMemFree:         1000000 kB\nMemAvailable:    8000000 kB\nBuffers: 1 kB\nCached: 2 kB\nSwapTotal:       2000000 kB\nSwapFree:        500000 kB\n"
	linCPU     = "processor\t: 0\nmodel name\t: Fake CPU 3.0GHz\nphysical id\t: 0\ncore id\t: 0\ncpu cores\t: 2\n\nprocessor\t: 1\nmodel name\t: Fake CPU 3.0GHz\nphysical id\t: 0\ncore id\t: 0\n\nprocessor\t: 2\nphysical id\t: 0\ncore id\t: 1\n\nprocessor\t: 3\nphysical id\t: 0\ncore id\t: 1\n"
)

type linFS map[string]string

func (f linFS) deps(r exec.Runner) Deps {
	return Deps{
		Runner: r,
		Lang:   diagfmt.PT,
		Now:    func() time.Time { return time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC) },
		ReadFile: func(p string) ([]byte, error) {
			if v, ok := f[p]; ok {
				return []byte(v), nil
			}
			return nil, os.ErrNotExist
		},
		Glob: func(pat string) ([]string, error) {
			set := map[string]bool{}
			for p := range f {
				// caminhos do sysfs são POSIX em QUALQUER SO (no Windows o Dir do pacote do SO nunca devolve "/" e o laço não
				// terminava: o teste travava 10 minutos); o laço também é limitado
				for q, n := p, 0; q != "/" && q != "." && n < 64; q, n = path.Dir(q), n+1 {
					set[q] = true
				}
			}
			var out []string
			for p := range set {
				if ok, _ := path.Match(pat, p); ok {
					out = append(out, p)
				}
			}
			return out, nil
		},
	}
}

func linFullFS() linFS {
	return linFS{
		"/sys/class/dmi/id/sys_vendor":   "FakeCorp\n",
		"/sys/class/dmi/id/product_name": "Laptop 1\n",
		"/sys/class/dmi/id/bios_version": "1.2.3\n",
		"/proc/cpuinfo":                  linCPU,
		"/proc/uptime":                   "3600.50 100.0\n",
		"/proc/meminfo":                  linMeminfo,

		"/sys/class/power_supply/BAT0/capacity":           "80\n",
		"/sys/class/power_supply/BAT0/status":             "Discharging\n",
		"/sys/class/power_supply/BAT0/cycle_count":        "123\n",
		"/sys/class/power_supply/BAT0/energy_full":        "40000000\n",
		"/sys/class/power_supply/BAT0/energy_full_design": "50000000\n",

		"/sys/class/thermal/thermal_zone0/type": "acpitz\n",
		"/sys/class/thermal/thermal_zone0/temp": "45000\n",
		"/sys/class/thermal/thermal_zone1/type": "bogus\n",
		"/sys/class/thermal/thermal_zone1/temp": "200000\n",
		"/sys/class/hwmon/hwmon0/name":          "nvme\n",
		"/sys/class/hwmon/hwmon0/temp1_input":   "38000\n",
		"/sys/class/hwmon/hwmon0/temp1_label":   "Composite\n",
		"/sys/class/hwmon/hwmon1/name":          "drivetemp\n",
		"/sys/class/hwmon/hwmon1/temp1_input":   "31500\n",
		"/sys/class/hwmon/hwmon2/name":          "coretemp\n",
		"/sys/class/hwmon/hwmon2/temp1_input":   "-50000\n",
	}
}

func linFullRunner(t *testing.T) *exectest.FakeRunner {
	t.Helper()
	f := exectest.New(t)
	for _, n := range []string{"lsblk", "df", "lspci", "journalctl", "dmesg"} {
		f.WithPath(n, "/usr/bin/"+n)
	}
	f.On("/usr/bin/lsblk -b -d -J -o NAME,SIZE,TYPE,ROTA,MODEL,TRAN", exectest.Exit(0, linLsblk))
	f.On("/usr/bin/df -B1 --output=source,avail,size", exectest.Exit(0, linDf))
	f.On("/usr/bin/lspci -mm", exectest.Exit(0, linLspci))
	f.On("/usr/bin/journalctl -p err -b --no-pager -q -o short-iso -n 300", exectest.Exit(0, linJournalOut))
	f.On(`/usr/bin/journalctl --no-pager -q -o short-iso --since "30 days ago" -g "Kernel panic|Oops:|segfault at|Out of memory: Killed process" -n 300`, exectest.Exit(0, linCrashOut))
	return f
}

func linSkipped(r *Report, area string) bool {
	for _, s := range r.Skipped {
		if s.Area == area {
			return true
		}
	}
	return false
}

func linNoMutation(t *testing.T, f *exectest.FakeRunner) {
	t.Helper()
	for _, c := range f.Calls() {
		if c.Elevate || !c.ReadOnly {
			t.Errorf("comando não é de leitura: %s (elevate=%v readonly=%v)", c, c.Elevate, c.ReadOnly)
		}
		if !strings.HasPrefix(c.Name, "/usr/bin/") {
			t.Errorf("esperava caminho absoluto: %s", c.Name)
		}
	}
}

func TestLinCaminhoFeliz(t *testing.T) {
	f := linFullRunner(t)
	r := &Report{}
	collectLinux(context.Background(), linFullFS().deps(f), r)
	if len(r.Skipped) != 1 || r.Skipped[0].Area != "smart" {
		t.Fatalf("só o smart devia ficar de fora: %+v", r.Skipped)
	}
	if !strings.Contains(r.Skipped[0].Reason, "sudo smartctl -H -A /dev/sda") || !strings.Contains(r.Skipped[0].Reason, "sudo nvme smart-log /dev/nvme0") {
		t.Errorf("instrução do smart: %s", r.Skipped[0].Reason)
	}
	h := r.Hardware
	if h.Vendor != "FakeCorp" || h.Model != "Laptop 1" || h.Firmware != "1.2.3" || h.CPU != "Fake CPU 3.0GHz" || h.Cores != 2 || h.Threads != 4 || h.Uptime != 3600500*time.Millisecond {
		t.Errorf("hardware: %+v", h)
	}
	m := r.Memory
	if m.TotalBytes != 16000000*1024 || m.UsedBytes != 8000000*1024 || m.SwapTotalBytes != 2000000*1024 || m.SwapUsedBytes != 1500000*1024 {
		t.Errorf("memória: %+v", m)
	}
	if len(r.Disks) != 2 {
		t.Fatalf("discos: %+v", r.Disks)
	}
	nv, hd := r.Disks[0], r.Disks[1] // ordenados por nome do dispositivo: nvme0n1, sda
	if nv.Name != "Fake NVMe 512" || nv.Medium != "SSD" || nv.Bus != "NVMe" || nv.SizeBytes != 512110190592 || nv.FreeBytes != 100500000000 || nv.TempC == nil || *nv.TempC != 38 {
		t.Errorf("nvme: %+v", nv)
	}
	if hd.Name != "Fake HDD 1TB" || hd.Medium != "HDD" || hd.Bus != "SATA" || hd.SizeBytes != 1000204886016 || hd.FreeBytes != 300000000000 || hd.TempC == nil || *hd.TempC != 31.5 {
		t.Errorf("hdd: %+v", hd)
	}
	if nv.Health != HealthUnknown {
		t.Errorf("sem smart a saúde é desconhecida")
	}
	b := r.Battery
	if b == nil || *b.Percent != 80 || b.Status != "descarregando" || *b.CycleCount != 123 || *b.HealthPct != 80 {
		t.Fatalf("bateria: %+v", b)
	}
	got := map[string]float64{}
	for _, tp := range r.Temps {
		got[tp.Sensor] = tp.Celsius
	}
	if len(r.Temps) != 3 || got["acpitz"] != 45 || got["nvme Composite"] != 38 || got["drivetemp temp1"] != 31.5 {
		t.Errorf("temps: %+v", r.Temps)
	}
	if len(r.GPUs) != 2 || r.GPUs[0].Name != "Intel Corporation UHD Graphics 620" || r.GPUs[1].Name != "NVIDIA Corporation GP108M [GeForce MX150]" {
		t.Errorf("gpus: %+v", r.GPUs)
	}
	// os caminhos aparecem como o sistema os informou (nada é mascarado): /home/alice e /home/bob são eventos distintos
	if len(r.Events) != 3 {
		t.Fatalf("eventos: %+v", r.Events)
	}
	if e := r.Events[0]; e.Source != "kernel" || e.Message != "ata1: link down" || e.Count != 1 {
		t.Errorf("evento 0: %+v", e)
	}
	if e := r.Events[1]; e.Source != "nm-dispatcher" || e.Count != 1 || e.Message != "error 12 in /home/alice/foo" || !e.When.Equal(time.Date(2026, 11, 13, 10, 0, 0, 0, time.UTC)) {
		t.Errorf("evento 1: %+v", e)
	}
	if e := r.Events[2]; e.Message != "error 99 in /home/bob/foo" {
		t.Errorf("evento 2: %+v", e)
	}
	if len(r.Crashes) != 3 || r.Crashes[0].Detail != "segfault: chrome" || r.Crashes[0].Kind != CrashApp ||
		r.Crashes[1].Detail != "OOM: firefox" || r.Crashes[2].Kind != CrashKernelPanic {
		t.Errorf("falhas: %+v", r.Crashes)
	}
	linNoMutation(t, f)
}

func TestLinNaoLeSerialNemUUID(t *testing.T) {
	fs := linFullFS()
	fs["/sys/class/dmi/id/product_serial"] = "SERIAL123"
	fs["/sys/class/dmi/id/product_uuid"] = "UUID-XYZ"
	read := []string{}
	d := fs.deps(linFullRunner(t))
	orig := d.ReadFile
	d.ReadFile = func(p string) ([]byte, error) { read = append(read, p); return orig(p) }
	r := &Report{}
	collectLinux(context.Background(), d, r)
	for _, p := range read {
		if strings.Contains(p, "serial") || strings.Contains(p, "uuid") {
			t.Errorf("leu campo proibido: %s", p)
		}
	}
}

func TestLinSemNada(t *testing.T) {
	f := exectest.New(t) // nenhuma ferramenta no PATH: nenhum comando pode ser chamado
	r := &Report{}
	collectLinux(context.Background(), linFS{}.deps(f), r)
	want := []string{"hardware", "memoria", "discos", "temperatura", "gpu", "eventos", "falhas"}
	for _, a := range want {
		if !linSkipped(r, a) {
			t.Errorf("faltou Skipped %s: %+v", a, r.Skipped)
		}
	}
	if r.Battery != nil {
		t.Errorf("sem BAT* a bateria é nil")
	}
	if linSkipped(r, "bateria") {
		t.Errorf("sem BAT* não há Skipped")
	}
	if len(f.Calls()) != 0 {
		t.Errorf("chamou comando sem ferramenta: %v", f.Calls())
	}
}

func TestLinAreasIndependentes(t *testing.T) {
	// cada área falha sozinha e as outras seguem
	cases := map[string]func(fs linFS, f *exectest.FakeRunner){
		"hardware": func(fs linFS, _ *exectest.FakeRunner) {
			for _, k := range []string{"/sys/class/dmi/id/sys_vendor", "/sys/class/dmi/id/product_name", "/sys/class/dmi/id/bios_version", "/proc/cpuinfo", "/proc/uptime"} {
				delete(fs, k)
			}
		},
		"memoria": func(fs linFS, _ *exectest.FakeRunner) { delete(fs, "/proc/meminfo") },
		"discos": func(_ linFS, f *exectest.FakeRunner) {
			f.On("/usr/bin/lsblk -b -d -J -o NAME,SIZE,TYPE,ROTA,MODEL,TRAN", exectest.Exit(1, ""))
		},
	}
	for area, brk := range cases {
		t.Run(area, func(t *testing.T) {
			fs := linFullFS()
			f := linFullRunner(t)
			brk(fs, f)
			if area == "discos" {
				f = exectest.New(t)
				for _, n := range []string{"lsblk", "df", "lspci", "journalctl"} {
					f.WithPath(n, "/usr/bin/"+n)
				}
				f.On("/usr/bin/lsblk -b -d -J -o NAME,SIZE,TYPE,ROTA,MODEL,TRAN", exectest.Exit(1, ""))
				f.On("/usr/bin/lspci -mm", exectest.Exit(0, linLspci))
				f.On("/usr/bin/journalctl -p err -b --no-pager -q -o short-iso -n 300", exectest.Exit(0, linJournalOut))
				f.On(`/usr/bin/journalctl --no-pager -q -o short-iso --since "30 days ago" -g "Kernel panic|Oops:|segfault at|Out of memory: Killed process" -n 300`, exectest.Exit(0, linCrashOut))
			}
			r := &Report{}
			collectLinux(context.Background(), fs.deps(f), r)
			if !linSkipped(r, area) {
				t.Errorf("faltou Skipped %s: %+v", area, r.Skipped)
			}
			if area != "memoria" && r.Memory.TotalBytes == 0 {
				t.Errorf("memória devia seguir")
			}
			if area != "hardware" && r.Hardware.CPU == "" {
				t.Errorf("hardware devia seguir")
			}
			if len(r.Events) == 0 || len(r.GPUs) == 0 || r.Battery == nil || len(r.Temps) == 0 {
				t.Errorf("as outras áreas devem seguir: %+v", r)
			}
		})
	}
}

func TestLinTemperaturaEBateriaAusentes(t *testing.T) {
	fs := linFullFS()
	for k := range fs {
		if strings.Contains(k, "power_supply") || strings.Contains(k, "thermal") || strings.Contains(k, "hwmon") {
			delete(fs, k)
		}
	}
	r := &Report{}
	collectLinux(context.Background(), fs.deps(linFullRunner(t)), r)
	if r.Battery != nil || linSkipped(r, "bateria") {
		t.Errorf("bateria ausente = nil sem Skipped")
	}
	if !linSkipped(r, "temperatura") || len(r.Disks) != 2 || r.Disks[0].TempC != nil {
		t.Errorf("temp: %+v %+v", r.Skipped, r.Disks)
	}
}

func TestLinFallbackGPUPorSysfs(t *testing.T) {
	fs := linFullFS()
	fs["/sys/class/drm/card0/device/vendor"] = "0x8086\n"
	fs["/sys/class/drm/card0/device/device"] = "0x5917\n"
	f := exectest.New(t)
	for _, n := range []string{"lsblk", "df"} {
		f.WithPath(n, "/usr/bin/"+n)
	}
	f.On("/usr/bin/lsblk -b -d -J -o NAME,SIZE,TYPE,ROTA,MODEL,TRAN", exectest.Exit(0, `{"blockdevices":[]}`))
	f.On("/usr/bin/df -B1 --output=source,avail,size", exectest.Exit(0, linDf))
	r := &Report{}
	collectLinux(context.Background(), fs.deps(f), r)
	if len(r.GPUs) != 1 || r.GPUs[0].Name != "PCI 8086:5917" {
		t.Errorf("gpus: %+v", r.GPUs)
	}
	if linSkipped(r, "smart") {
		t.Errorf("sem discos não há smart")
	}
}

func TestLinEventosViaDmesg(t *testing.T) {
	f := exectest.New(t)
	f.WithPath("journalctl", "/usr/bin/journalctl").WithPath("dmesg", "/usr/bin/dmesg")
	f.On("/usr/bin/journalctl -p err -b --no-pager -q -o short-iso -n 300", exectest.Exit(1, ""))
	f.On("/usr/bin/dmesg --level=err,crit,alert,emerg --time-format iso", exectest.Exit(0,
		"2026-11-13T10:00:00,123456+00:00 usb 1-1: error -71\n2026-11-13T10:05:00,000000+00:00 usb 1-2: error -71\n2026-11-12T10:05:00,000000+00:00 Out of memory: Killed process 9 (java) x\n"))
	r := &Report{}
	collectLinux(context.Background(), linFS{}.deps(f), r)
	if len(r.Events) != 2 || r.Events[0].Source != "kernel" || r.Events[0].Count != 2 {
		t.Errorf("eventos: %+v", r.Events)
	}
	if len(r.Crashes) != 1 || r.Crashes[0].Detail != "OOM: java" {
		t.Errorf("falhas: %+v", r.Crashes)
	}
	if linSkipped(r, "eventos") {
		t.Errorf("dmesg funcionou")
	}
}

func TestLinEventosSemAcesso(t *testing.T) {
	f := exectest.New(t)
	f.WithPath("journalctl", "/usr/bin/journalctl").WithPath("dmesg", "/usr/bin/dmesg")
	f.On("/usr/bin/journalctl -p err -b --no-pager -q -o short-iso -n 300", exectest.Exit(1, ""))
	f.On("/usr/bin/dmesg --level=err,crit,alert,emerg --time-format iso", exectest.Exit(1, ""))
	r := &Report{}
	collectLinux(context.Background(), linFS{}.deps(f), r)
	if !linSkipped(r, "eventos") || !linSkipped(r, "falhas") {
		t.Fatalf("%+v", r.Skipped)
	}
	for _, s := range r.Skipped {
		if s.Area == "eventos" && !strings.Contains(s.Reason, "systemd-journal") {
			t.Errorf("sem instrução: %s", s.Reason)
		}
	}
	linNoMutation(t, f)
}

func TestLinErroDeExecucaoNaoQuebra(t *testing.T) {
	f := exectest.New(t)
	f.WithPath("lsblk", "/usr/bin/lsblk")
	f.On("/usr/bin/lsblk -b -d -J -o NAME,SIZE,TYPE,ROTA,MODEL,TRAN", exectest.Reply{Err: errors.New("boom")})
	r := &Report{}
	collectLinux(context.Background(), linFS{}.deps(f), r)
	if !linSkipped(r, "discos") {
		t.Errorf("%+v", r.Skipped)
	}
}

func TestLinSaidasInvalidas(t *testing.T) {
	bad := []string{"", "{", `{"blockdevices":`, `{"blockdevices":[1,2]}`, `{"blockdevices":[{"name":5,"size":{},"type":"disk","rota":[]}]}`, "null"}
	for _, out := range bad {
		f := exectest.New(t)
		f.WithPath("lsblk", "/usr/bin/lsblk")
		f.On("/usr/bin/lsblk -b -d -J -o NAME,SIZE,TYPE,ROTA,MODEL,TRAN", exectest.Exit(0, out))
		r := &Report{}
		collectLinux(context.Background(), linFS{}.deps(f), r)
		if len(r.Disks) != 0 {
			t.Errorf("%q: %+v", out, r.Disks)
		}
	}
	fs := linFS{
		"/proc/meminfo":                                   "MemTotal: abc kB\nSwapTotal\n",
		"/proc/cpuinfo":                                   "lixo\n:\n\n\n",
		"/proc/uptime":                                    "xx",
		"/sys/class/power_supply/BAT0/status":             "Full",
		"/sys/class/power_supply/BAT0/energy_full":        "0",
		"/sys/class/power_supply/BAT0/energy_full_design": "0",
	}
	r := &Report{}
	collectLinux(context.Background(), fs.deps(exectest.New(t)), r)
	if r.Memory.TotalBytes != 0 || !linSkipped(r, "memoria") || !linSkipped(r, "hardware") {
		t.Errorf("%+v", r)
	}
	if r.Battery == nil || r.Battery.Status != "cheia" || r.Battery.HealthPct != nil {
		t.Errorf("bateria: %+v", r.Battery)
	}
}

func TestLinLimites(t *testing.T) {
	fs := linFS{
		"/proc/meminfo":                                   "MemTotal: 100 kB\nMemFree: 10 kB\nBuffers: 20 kB\nCached: 30 kB\nSwapTotal: 0 kB\nSwapFree: 0 kB\n",
		"/sys/class/power_supply/BAT0/capacity":           "100",
		"/sys/class/power_supply/BAT0/charge_full":        "6000",
		"/sys/class/power_supply/BAT0/charge_full_design": "5000",
		"/sys/class/thermal/thermal_zone0/temp":           "150000",
		"/sys/class/thermal/thermal_zone1/temp":           "-40000",
		"/sys/class/thermal/thermal_zone2/temp":           "150001",
	}
	r := &Report{}
	collectLinux(context.Background(), fs.deps(exectest.New(t)), r)
	if r.Memory.UsedBytes != 40*1024 || r.Memory.SwapTotalBytes != 0 {
		t.Errorf("mem: %+v", r.Memory)
	}
	if *r.Battery.HealthPct != 100 || *r.Battery.Percent != 100 {
		t.Errorf("bat: %+v", r.Battery)
	}
	if len(r.Temps) != 2 || r.Temps[0].Celsius != -40 && r.Temps[1].Celsius != -40 {
		t.Errorf("temps: %+v", r.Temps)
	}
}

func TestLinMensagemTruncadaEMantemOCaminho(t *testing.T) {
	long := strings.Repeat("á", 500)
	out := "2026-11-13T10:00:00+0000 h unit: /home/zed/x /Users/q/y " + long + "\nresto\n"
	f := exectest.New(t)
	f.WithPath("journalctl", "/usr/bin/journalctl")
	f.On("/usr/bin/journalctl -p err -b --no-pager -q -o short-iso -n 300", exectest.Exit(0, out))
	f.On(`/usr/bin/journalctl --no-pager -q -o short-iso --since "30 days ago" -g "Kernel panic|Oops:|segfault at|Out of memory: Killed process" -n 300`, exectest.Exit(1, ""))
	r := &Report{}
	collectLinux(context.Background(), linFS{}.deps(f), r)
	if len(r.Events) != 1 {
		t.Fatalf("%+v", r.Events)
	}
	m := r.Events[0].Message
	if !strings.HasPrefix(m, "/home/zed/x /Users/q/y ") || len([]rune(m)) != 200 || strings.Contains(m, "resto") {
		t.Errorf("mensagem: %q", m)
	}
	if !linSkipped(r, "falhas") {
		t.Errorf("falhas devia ser Skipped")
	}
}

func TestLinPanicNaoPropaga(t *testing.T) {
	d := linFS{}.deps(exectest.New(t))
	d.ReadFile = func(string) ([]byte, error) { panic("x") }
	r := &Report{}
	collectLinux(context.Background(), d, r)
	if !linSkipped(r, "hardware") || !linSkipped(r, "memoria") {
		t.Errorf("%+v", r.Skipped)
	}
}

func TestLinIdiomas(t *testing.T) {
	for _, l := range []diagfmt.Lang{diagfmt.EN, diagfmt.ES} {
		d := linFS{}.deps(exectest.New(t))
		d.Lang = l
		r := &Report{}
		collectLinux(context.Background(), d, r)
		for _, s := range r.Skipped {
			if strings.Contains(s.Reason, "não") {
				t.Errorf("lang %v: %s", l, s.Reason)
			}
		}
	}
}

func TestLinDiskParent(t *testing.T) {
	for _, c := range []struct {
		p, d string
		ok   bool
	}{{"nvme0n1p2", "nvme0n1", true}, {"sda1", "sda", true}, {"sda", "sda", false}, {"sdab", "sda", false}, {"sda1", "sdb", false}, {"sdaX", "sda", false}} {
		if linDiskParent(c.p, c.d) != c.ok {
			t.Errorf("%+v", c)
		}
	}
}

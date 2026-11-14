package diagfmt

import (
	"strings"
	"testing"
)

const linuxFakeMeminfo = `MemTotal:       16384000 kB
MemFree:         2000000 kB
MemAvailable:   10000000 kB
Buffers:          100000 kB
Cached:          3000000 kB
SwapTotal:       4194304 kB
SwapFree:        2936012 kB
`

func linuxMustRender(t *testing.T, name, raw string, l Lang) string {
	t.Helper()
	out, err := Render(name, raw, l)
	if err != nil {
		t.Fatalf("%s: erro inesperado: %v", name, err)
	}
	return out
}

func TestLinuxMemory(t *testing.T) {
	out := linuxMustRender(t, "linux-memory", linuxFakeMeminfo, PT)
	for _, w := range []string{
		"Memória: 15,6 GB no total",
		"Em uso agora: 6,1 de 15,6 GB (39%)",
		"Swap: 1,2 de 4 GB em uso",
		"detalhes dos pentes exigem administrador; não mostrados",
	} {
		if !strings.Contains(out, w) {
			t.Errorf("faltou %q em:\n%s", w, out)
		}
	}
}

func TestLinuxMemoryNoSwap(t *testing.T) {
	raw := strings.NewReplacer("SwapTotal:       4194304 kB", "SwapTotal:             0 kB", "SwapFree:        2936012 kB", "SwapFree:              0 kB").Replace(linuxFakeMeminfo)
	out := linuxMustRender(t, "linux-memory", raw, PT)
	if !strings.Contains(out, "Sem swap") || strings.Contains(out, "Swap:") {
		t.Errorf("sem swap:\n%s", out)
	}
	// linhas de swap ausentes também
	out = linuxMustRender(t, "linux-memory", "MemTotal: 8388608 kB\nMemAvailable: 4194304 kB\n", PT)
	if !strings.Contains(out, "Sem swap") || !strings.Contains(out, "Em uso agora: 4 de 8 GB (50%)") {
		t.Errorf("sem swap/linhas:\n%s", out)
	}
}

func TestLinuxMemoryNoAvailable(t *testing.T) {
	// sem MemAvailable: usa MemFree+Buffers+Cached = 5100000 kB
	raw := "MemTotal: 16384000 kB\nMemFree: 2000000 kB\nBuffers: 100000 kB\nCached: 3000000 kB\n"
	out := linuxMustRender(t, "linux-memory", raw, PT)
	if !strings.Contains(out, "Em uso agora: 10,8 de 15,6 GB (69%)") {
		t.Errorf("fallback:\n%s", out)
	}
	// sem nenhum dos dois: não inventa uso
	out = linuxMustRender(t, "linux-memory", "MemTotal: 16384000 kB\n", PT)
	if strings.Contains(out, "Em uso agora") || !strings.Contains(out, "15,6 GB no total") {
		t.Errorf("sem uso:\n%s", out)
	}
}

func TestLinuxMemoryLangs(t *testing.T) {
	en := linuxMustRender(t, "linux-memory", linuxFakeMeminfo, EN)
	for _, w := range []string{"Memory: 15.6 GB total", "In use now: 6.1 of 15.6 GB (39%)", "Swap: 1.2 of 4 GB in use", "require administrator"} {
		if !strings.Contains(en, w) {
			t.Errorf("en: faltou %q em:\n%s", w, en)
		}
	}
	es := linuxMustRender(t, "linux-memory", linuxFakeMeminfo, ES)
	for _, w := range []string{"Memoria: 15,6 GB en total", "En uso ahora: 6,1 de 15,6 GB (39%)", "Swap: 1,2 de 4 GB en uso", "administrador"} {
		if !strings.Contains(es, w) {
			t.Errorf("es: faltou %q em:\n%s", w, es)
		}
	}
}

func TestLinuxMemoryBad(t *testing.T) {
	for _, raw := range []string{"", "   \n", "lixo qualquer", "MemTotal: abc kB", "MemTotal: 0 kB", "{\"a\":1}", "MemAvailable: 100 kB"} {
		if _, err := Render("linux-memory", raw, PT); err == nil {
			t.Errorf("%q: esperava erro", raw)
		}
	}
}

const linuxLsblk = `{"blockdevices":[
 {"name":"nvme0n1","size":512110190592,"type":"disk","rota":false,"model":"Fake NVMe 512","tran":"nvme"},
 {"name":"sda","size":"1000204886016","type":"disk","rota":"1","model":"Fake HDD 1TB","tran":"sata"},
 {"name":"sdb","size":256060514304,"type":"disk","rota":0,"model":null,"tran":"usb"},
 {"name":"loop0","size":65536,"type":"loop","rota":false,"model":null,"tran":null},
 {"name":"sr0","size":1073741312,"type":"rom","rota":true,"model":"Fake DVD","tran":"sata"}
]}`

func TestLinuxDisks(t *testing.T) {
	out := linuxMustRender(t, "linux-disks", linuxLsblk, PT)
	for _, w := range []string{
		"Disco: Fake NVMe 512 · SSD · NVMe · 512 GB",
		"Disco: Fake HDD 1TB · HDD · 1 TB",
		"Disco: sdb · SSD · 256 GB",
		"saúde SMART exige administrador; não verificada",
	} {
		if !strings.Contains(out, w) {
			t.Errorf("faltou %q em:\n%s", w, out)
		}
	}
	if strings.Contains(out, "loop0") || strings.Contains(out, "DVD") {
		t.Errorf("loop/rom não deveriam aparecer:\n%s", out)
	}
}

func TestLinuxDisksLangs(t *testing.T) {
	en := linuxMustRender(t, "linux-disks", linuxLsblk, EN)
	if !strings.Contains(en, "Disk: Fake NVMe 512 · SSD · NVMe · 512 GB") || !strings.Contains(en, "SMART health requires administrator") {
		t.Errorf("en:\n%s", en)
	}
	es := linuxMustRender(t, "linux-disks", linuxLsblk, ES)
	if !strings.Contains(es, "Disco: Fake NVMe 512 · SSD · NVMe · 512 GB") || !strings.Contains(es, "salud SMART exige administrador") {
		t.Errorf("es:\n%s", es)
	}
}

func TestLinuxDisksTolerant(t *testing.T) {
	// campos nulos, sem rota, nome nulo, tamanho inválido
	raw := `{"blockdevices":[{"name":null,"size":null,"type":"disk","rota":null,"model":" ","tran":null},{"name":"vda","size":"1073741824","type":"disk"}]}`
	out := linuxMustRender(t, "linux-disks", raw, PT)
	if !strings.Contains(out, "Disco: vda · 1,1 GB") {
		t.Errorf("tolerante:\n%s", out)
	}
	// só loop: sem discos, mas JSON válido -> erro (a engine mostra o original)
	if _, err := Render("linux-disks", `{"blockdevices":[{"name":"loop0","type":"loop"}]}`, PT); err == nil {
		t.Error("sem discos: esperava erro")
	}
}

func TestLinuxDisksBad(t *testing.T) {
	for _, raw := range []string{
		"", "  ", "lixo", `{"blockdevices":`, `null`, `[]`, `{"blockdevices":null}`,
		`{"blockdevices":{"name":"x"}}`, `{"blockdevices":[1,"a",null]}`, `{"blockdevices":[]}`,
		"lsblk não encontrado",
	} {
		if _, err := Render("linux-disks", raw, PT); err == nil {
			t.Errorf("%q: esperava erro", raw)
		}
	}
}

package diagfmt

import (
	"strings"
	"testing"
)

const winMemOne = `{"modules":[{"BankLabel":"BANK 0","DeviceLocator":"ChannelA-DIMM0","Manufacturer":"80AD000000000000","PartNumber":"TMA81GS6AFR8N-UHSC  ","Capacity":8589934592,"Speed":2400,"ConfiguredClockSpeed":2400,"SMBIOSMemoryType":26,"MemoryType":0}],"arrays":[{"MemoryDevices":2}],"os":{"TotalVisibleMemorySize":8286208,"FreePhysicalMemory":2900000}}`

func winMust(t *testing.T, name, raw string, l Lang) string {
	t.Helper()
	out, err := Render(name, raw, l)
	if err != nil {
		t.Fatalf("%s: erro inesperado: %v", name, err)
	}
	return out
}

func TestWinMemoryExemplo(t *testing.T) {
	want := "Memória: 8 GB no total (1 de 2 slots usados) · DDR4 2400 MT/s\n" +
		"  Slot 1: 8 GB · SK hynix · TMA81GS6AFR8N-UHSC\n" +
		"Em uso agora: 5,1 de 7,9 GB (65%)"
	got := winMust(t, "windows-memory", winMemOne, PT)
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestWinMemoryIdiomas(t *testing.T) {
	en := winMust(t, "windows-memory", winMemOne, EN)
	for _, s := range []string{"Memory: 8 GB total (1 of 2 slots used) · DDR4 2400 MT/s", "Slot 1: 8 GB · SK hynix", "In use now: 5.1 of 7.9 GB (65%)"} {
		if !strings.Contains(en, s) {
			t.Errorf("en sem %q:\n%s", s, en)
		}
	}
	es := winMust(t, "windows-memory", winMemOne, ES)
	for _, s := range []string{"Memoria: 8 GB en total (1 de 2 ranuras usadas)", "Ranura 1: 8 GB", "En uso ahora: 5,1 de 7,9 GB (65%)"} {
		if !strings.Contains(es, s) {
			t.Errorf("es sem %q:\n%s", s, es)
		}
	}
}

func TestWinMemoryDoisModulos(t *testing.T) {
	raw := `{"modules":[
	 {"DeviceLocator":"DIMM1","Manufacturer":"Samsung","PartNumber":"M2","Capacity":8589934592,"ConfiguredClockSpeed":3200,"SMBIOSMemoryType":26},
	 {"DeviceLocator":"DIMM0","Manufacturer":"0198","PartNumber":"K1","Capacity":8589934592,"ConfiguredClockSpeed":3200,"SMBIOSMemoryType":26}],
	 "arrays":[{"MemoryDevices":2}],"os":{}}`
	got := winMust(t, "windows-memory", raw, PT)
	want := "Memória: 16 GB no total (2 de 2 slots usados) · DDR4 3200 MT/s\n  Slot 1: 8 GB · Kingston · K1\n  Slot 2: 8 GB · Samsung · M2"
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestWinMemoryTiposDiferentesPorSlot(t *testing.T) {
	raw := `{"modules":[
	 {"DeviceLocator":"A","Capacity":4294967296,"ConfiguredClockSpeed":2133,"SMBIOSMemoryType":26},
	 {"DeviceLocator":"B","Capacity":4294967296,"ConfiguredClockSpeed":1600,"SMBIOSMemoryType":24}]}`
	got := winMust(t, "windows-memory", raw, PT)
	if !strings.HasPrefix(got, "Memória: 8 GB no total\n") {
		t.Errorf("cabeçalho não deveria ter tipo/velocidade:\n%s", got)
	}
	if !strings.Contains(got, "Slot 1: 4 GB · DDR4 2133 MT/s") || !strings.Contains(got, "Slot 2: 4 GB · DDR3 1600 MT/s") {
		t.Errorf("sem tipo por slot:\n%s", got)
	}
}

func TestWinMemorySlotVazio(t *testing.T) {
	raw := `{"modules":[{"DeviceLocator":"A","Capacity":8589934592,"SMBIOSMemoryType":34,"Speed":4800},{"DeviceLocator":"B","Capacity":0}],"arrays":{"MemoryDevices":4}}`
	got := winMust(t, "windows-memory", raw, PT)
	if !strings.HasPrefix(got, "Memória: 8 GB no total (1 de 4 slots usados) · DDR5 4800 MT/s\n") {
		t.Errorf("got:\n%s", got)
	}
	if strings.Contains(got, "Slot 2") {
		t.Errorf("slot vazio listado:\n%s", got)
	}
}

func TestWinMemorySlotSingular(t *testing.T) {
	raw := `{"modules":[{"Capacity":4294967296}],"arrays":[{"MemoryDevices":1}]}`
	got := winMust(t, "windows-memory", raw, PT)
	if !strings.Contains(got, "(1 de 1 slot usado)") {
		t.Errorf("got:\n%s", got)
	}
}

func TestWinMemoryFabricante(t *testing.T) {
	cases := map[string]string{
		"80AD":                "SK hynix",
		"80ad000000000000":    "SK hynix",
		"079E000080AD":        "SK hynix",
		"859B":                "Crucial",
		"Samsung":             "Samsung",
		"Kingston Technology": "Kingston Technology",
		"0F1A":                "JEDEC 0F1A",
		"0f1a000000000000":    "JEDEC 0F1A", // preenchimento provado (16 dígitos): sai
		"8010":                "JEDEC 8010", // termina em zero e NÃO é padding (nunca 0801)
		"80100000":            "JEDEC 80100000",
		"0F10":                "JEDEC 0F10",
		"10":                  "JEDEC 10",
		"80AD0000":            "SK hynix", // id conhecido + só zeros
		"Unknown":             "",
		"":                    "",
		"0000":                "",
		"0000000000000000":    "",
	}
	for in, want := range cases {
		if got := winMaker(in); got != want {
			t.Errorf("winMaker(%q) = %q, quer %q", in, got, want)
		}
	}
	raw := `{"modules":[{"Capacity":8589934592,"Manufacturer":"Unknown","PartNumber":"  "}]}`
	got := winMust(t, "windows-memory", raw, PT)
	if !strings.Contains(got, "  Slot 1: 8 GB\n") && !strings.HasSuffix(got, "  Slot 1: 8 GB") {
		t.Errorf("fabricante/part number vazios deveriam sumir:\n%s", got)
	}
}

func TestWinMemoryTipoDesconhecido(t *testing.T) {
	raw := `{"modules":[{"Capacity":8589934592,"SMBIOSMemoryType":0,"MemoryType":0,"Speed":0,"ConfiguredClockSpeed":0}]}`
	got := winMust(t, "windows-memory", raw, PT)
	if strings.Contains(got, "MT/s") || strings.Contains(got, "DDR") || strings.Contains(got, "·") {
		t.Errorf("deveria omitir tipo e velocidade:\n%s", got)
	}
	raw = `{"modules":[{"Capacity":8589934592,"SMBIOSMemoryType":99,"Speed":1333}]}`
	got = winMust(t, "windows-memory", raw, PT)
	if strings.Contains(got, "DDR") || !strings.Contains(got, "1333 MT/s") {
		t.Errorf("tipo 99 omite o tipo, mantém a velocidade:\n%s", got)
	}
}

func TestWinMemoryObjetoUnicoENumerosComoTexto(t *testing.T) {
	raw := `{"modules":{"DeviceLocator":"A","Capacity":"8589934592","SMBIOSMemoryType":"26","ConfiguredClockSpeed":"2666"},"arrays":{"MemoryDevices":"2"},"os":{"TotalVisibleMemorySize":"8286208","FreePhysicalMemory":"4143104"}}`
	got := winMust(t, "windows-memory", raw, PT)
	if !strings.Contains(got, "8 GB no total (1 de 2 slots usados) · DDR4 2666 MT/s") || !strings.Contains(got, "Em uso agora: 4 de 7,9 GB (50%)") {
		t.Errorf("got:\n%s", got)
	}
}

func TestWinMemorySemOS(t *testing.T) {
	got := winMust(t, "windows-memory", `{"modules":[{"Capacity":8589934592}],"os":null}`, PT)
	if strings.Contains(got, "Em uso") {
		t.Errorf("sem os não mostra a linha:\n%s", got)
	}
}

func TestWinMemoryInvalido(t *testing.T) {
	for _, raw := range []string{"", "   ", "lixo", `{"modules":[`, "[]", "[1,2]", "null", `"texto"`, "42", `{}`, `{"foo":1}`} {
		out, err := Render("windows-memory", raw, PT)
		if err == nil {
			t.Errorf("%q: esperava erro, veio %q", raw, out)
		}
	}
	// campos nulos e tipos trocados não derrubam
	for _, raw := range []string{
		`{"modules":null,"arrays":null,"os":null}`,
		`{"modules":[null,1,"x",{"Capacity":null,"Manufacturer":null,"PartNumber":null}],"arrays":[null],"os":[]}`,
		`{"modules":[{"Capacity":-5,"Speed":"abc","SMBIOSMemoryType":[1],"Manufacturer":{"a":1}}],"os":{"TotalVisibleMemorySize":-1,"FreePhysicalMemory":"x"}}`,
		`{"modules":[{"Capacity":1e30}]}`,
	} {
		_, _ = Render("windows-memory", raw, PT)
	}
}

func TestWinDisksExemplo(t *testing.T) {
	raw := `[{"FriendlyName":"LITEON CV8-CE256-HP","MediaType":"SSD","HealthStatus":"Healthy","OperationalStatus":"OK","Size":256060514304,"BusType":"SATA"}]`
	want := "Disco: LITEON CV8-CE256-HP · SSD · 256 GB · saudável"
	if got := winMust(t, "windows-disks", raw, PT); got != want {
		t.Errorf("got %q, quer %q", got, want)
	}
	if got := winMust(t, "windows-disks", raw, EN); got != "Disk: LITEON CV8-CE256-HP · SSD · 256 GB · healthy" {
		t.Errorf("en: %q", got)
	}
	if got := winMust(t, "windows-disks", raw, ES); got != "Disco: LITEON CV8-CE256-HP · SSD · 256 GB · saludable" {
		t.Errorf("es: %q", got)
	}
}

func TestWinDisksSaudeEEstado(t *testing.T) {
	raw := `[
	 {"FriendlyName":"A","MediaType":"HDD","HealthStatus":"Warning","OperationalStatus":"OK","Size":1000204886016},
	 {"FriendlyName":"B","MediaType":"HDD","HealthStatus":"Unhealthy","OperationalStatus":["Lost Communication","Offline"],"Size":500107862016},
	 {"FriendlyName":"C","MediaType":"SSD","HealthStatus":"Stale","OperationalStatus":"OK","Size":0}]`
	got := winMust(t, "windows-disks", raw, PT)
	for _, s := range []string{
		"Disco: A · HDD · 1 TB · atenção\n",
		"Disco: B · HDD · 500 GB · com problema · estado: Lost Communication, Offline\n",
		"Disco: C · SSD · Stale",
	} {
		if !strings.Contains(got, s) {
			t.Errorf("sem %q:\n%s", s, got)
		}
	}
	en := winMust(t, "windows-disks", raw, EN)
	if !strings.Contains(en, "warning") || !strings.Contains(en, "unhealthy") || !strings.Contains(en, "status: Lost") {
		t.Errorf("en:\n%s", en)
	}
	es := winMust(t, "windows-disks", raw, ES)
	if !strings.Contains(es, "atención") || !strings.Contains(es, "con problema") || !strings.Contains(es, "estado: Lost") {
		t.Errorf("es:\n%s", es)
	}
}

func TestWinDisksNVMeUnspecified(t *testing.T) {
	raw := `[{"FriendlyName":"Disco NVMe X","MediaType":"Unspecified","BusType":"NVMe","HealthStatus":"Healthy","OperationalStatus":"OK","Size":512110190592},
	          {"FriendlyName":"Pen","MediaType":"Unspecified","BusType":"USB","HealthStatus":"Healthy","OperationalStatus":"OK","Size":16000000000},
	          {"FriendlyName":"Num","MediaType":0,"BusType":17,"HealthStatus":0,"Size":1000000000000}]`
	got := winMust(t, "windows-disks", raw, PT)
	for _, s := range []string{"Disco: Disco NVMe X · SSD NVMe · 512 GB · saudável", "Disco: Pen · USB · 16 GB", "Disco: Num · SSD NVMe · 1 TB · saudável"} {
		if !strings.Contains(got, s) {
			t.Errorf("sem %q:\n%s", s, got)
		}
	}
}

func TestWinDisksObjetoUnicoVazioENulos(t *testing.T) {
	got := winMust(t, "windows-disks", `{"FriendlyName":"Solo","MediaType":"SSD","HealthStatus":"Healthy","Size":"256060514304"}`, PT)
	if got != "Disco: Solo · SSD · 256 GB · saudável" {
		t.Errorf("got %q", got)
	}
	if got := winMust(t, "windows-disks", `[{"FriendlyName":""}]`, PT); got != "Disco: Disco sem nome" {
		t.Errorf("got %q", got)
	}
	if got := winMust(t, "windows-disks", `[{"FriendlyName":null,"Size":null,"HealthStatus":null}]`, EN); got != "Disk: Unnamed disk" {
		t.Errorf("got %q", got)
	}
	for _, raw := range []string{"[]", "null"} {
		if got := winMust(t, "windows-disks", raw, PT); got != "Nenhum disco encontrado" {
			t.Errorf("%s: got %q", raw, got)
		}
	}
	if got := winMust(t, "windows-disks", "[]", ES); got != "Ningún disco encontrado" {
		t.Errorf("es: %q", got)
	}
}

func TestWinDisksInvalido(t *testing.T) {
	for _, raw := range []string{"", "  ", "lixo", `[{"FriendlyName":`, `"x"`, "42"} {
		if out, err := Render("windows-disks", raw, PT); err == nil {
			t.Errorf("%q: esperava erro, veio %q", raw, out)
		}
	}
	for _, raw := range []string{`[1,null,"x",[]]`, `{"a":{"b":1}}`, `[{"Size":-1,"HealthStatus":[1,2],"OperationalStatus":{"x":1}}]`} {
		_, _ = Render("windows-disks", raw, PT)
	}
}

// Lupa #56: SMBIOSMemoryType (DSP0134) e MemoryType (WMI) são tabelas diferentes: o 20 do SMBIOS é DDR2 FB-DIMM e o 20
// do WMI é DDR; 21 a 23 do SMBIOS são reservados e não caem na tabela do WMI.
func TestWinMemoryTabelasSMBIOSeWMISeparadas(t *testing.T) {
	cases := []struct {
		name, fields, want, notWant string
	}{
		{"SMBIOS 20 = DDR2 FB-DIMM", `"SMBIOSMemoryType":20,"MemoryType":0`, "DDR2 FB-DIMM", ""},
		{"WMI 20 = DDR (SMBIOS 0)", `"SMBIOSMemoryType":0,"MemoryType":20`, "DDR ", "FB-DIMM"},
		{"SMBIOS manda sobre o WMI", `"SMBIOSMemoryType":26,"MemoryType":20`, "DDR4", "DDR "},
		{"SMBIOS 21 reservado", `"SMBIOSMemoryType":21,"MemoryType":0`, "", "DDR"},
		{"SMBIOS 22 reservado, WMI 21 não vale", `"SMBIOSMemoryType":22,"MemoryType":21`, "", "DDR2"},
		{"SMBIOS 23 reservado, WMI 20 não vale", `"SMBIOSMemoryType":23,"MemoryType":20`, "", "DDR"},
		{"WMI 22 = DDR2 FB-DIMM", `"SMBIOSMemoryType":0,"MemoryType":22`, "DDR2 FB-DIMM", ""},
		{"WMI 24 = DDR3", `"SMBIOSMemoryType":0,"MemoryType":24`, "DDR3", ""},
		{"SMBIOS 34 = DDR5", `"SMBIOSMemoryType":34`, "DDR5", ""},
	}
	for _, c := range cases {
		raw := `{"modules":[{"Capacity":8589934592,"Speed":2400,` + c.fields + `}]}`
		got := winMust(t, "windows-memory", raw, PT)
		if c.want != "" && !strings.Contains(got, c.want) {
			t.Errorf("%s: faltou %q:\n%s", c.name, c.want, got)
		}
		if c.notWant != "" && strings.Contains(got, c.notWant) {
			t.Errorf("%s: não deveria ter %q:\n%s", c.name, c.notWant, got)
		}
	}
}

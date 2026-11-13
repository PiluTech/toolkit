package diagfmt

import (
	"strings"
	"testing"
)

const macVMStat16k = `Mach Virtual Memory Statistics: (page size of 16384 bytes)
Pages free:                                     6086.
Pages active:                                 400000.
Pages inactive:                               372066.
Pages wired down:                             100000.
Pages occupied by compressor:                  50000.
`

const macVMStat4k = `Mach Virtual Memory Statistics: (page size of 4096 bytes)
Pages free:                                     6086.
Pages active:                                1600000.
Pages wired down:                             400000.
Pages occupied by compressor:                 200000.
`

const macProfilerAS = `{"SPMemoryDataType":[{"dimm_manufacturer":"Fab","dimm_type":"LPDDR5","SPMemoryDataType":"16 GB"}]}`

const macProfilerIntel = `{"SPMemoryDataType":[{"_name":"Memória","_items":[
 {"_name":"BANK 0/DIMM0","dimm_size":"8 GB","dimm_type":"DDR4","dimm_speed":"2667 MHz","dimm_status":"ok"},
 {"_name":"BANK 1/DIMM0","dimm_size":"8 GB","dimm_type":"DDR4","dimm_speed":"2667 MHz","dimm_status":"ok"},
 {"_name":"BANK 2/DIMM0","dimm_size":"Empty","dimm_type":"empty","dimm_status":"empty"}]}]}`

func macMem(memsize, vm, prof string) string {
	return "### memsize\n" + memsize + "\n### vm_stat\n" + vm + "### profiler\n" + prof + "\n"
}

func TestMacMemory(t *testing.T) {
	// 16 GB; (400000+100000+50000)*16384 = 9011200000 B = 8,39 GB
	cases := []struct {
		name string
		raw  string
		l    Lang
		want []string
		not  []string
	}{
		{"apple silicon pt", macMem("17179869184", macVMStat16k, macProfilerAS), PT,
			[]string{"Memória: 16 GB no total · LPDDR5 (unificada)", "Em uso agora: 8,4 de 16 GB (52%)"}, nil},
		{"apple silicon en", macMem("17179869184", macVMStat16k, macProfilerAS), EN,
			[]string{"Memory: 16 GB total · LPDDR5 (unified)", "In use now: 8.4 of 16 GB (52%)"}, nil},
		{"apple silicon es", macMem("17179869184", macVMStat16k, macProfilerAS), ES,
			[]string{"Memoria: 16 GB en total · LPDDR5 (unificada)", "En uso ahora: 8,4 de 16 GB (52%)"}, nil},
		{"intel dimms", macMem("17179869184", macVMStat4k, macProfilerIntel), PT,
			[]string{"Memória: 16 GB no total · DDR4", "Em uso agora: 8,4 de 16 GB (52%)"}, []string{"unificada", "empty"}},
		{"sem tipo", macMem("17179869184", macVMStat4k, `{"SPMemoryDataType":[{"SPMemoryDataType":"16 GB"}]}`), PT,
			[]string{"Memória: 16 GB no total\n"}, []string{"·"}},
		{"sem profiler", "### memsize\n17179869184\n### vm_stat\n" + macVMStat4k, PT,
			[]string{"Memória: 16 GB no total"}, []string{"·"}},
		{"sem vm_stat", "### memsize\n17179869184\n", PT,
			[]string{"Memória: 16 GB no total"}, []string{"Em uso"}},
		{"vm_stat lixo", macMem("17179869184", "erro\n", "x"), PT,
			[]string{"Memória: 16 GB no total"}, []string{"Em uso"}},
	}
	for _, c := range cases {
		got, err := Render("macos-memory", c.raw, c.l)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		for _, w := range c.want {
			if !strings.Contains(got+"\n", w) {
				t.Errorf("%s: falta %q em %q", c.name, w, got)
			}
		}
		for _, n := range c.not {
			if strings.Contains(got, n) {
				t.Errorf("%s: não devia ter %q em %q", c.name, n, got)
			}
		}
	}
}

func TestMacMemoryUsoLimitado(t *testing.T) {
	vm := "Mach Virtual Memory Statistics: (page size of 16384 bytes)\nPages active: 99999999.\n"
	got, err := Render("macos-memory", macMem("17179869184", vm, ""), PT)
	if err != nil || !strings.Contains(got, "(100%)") {
		t.Errorf("%q %v", got, err)
	}
}

func TestMacErros(t *testing.T) {
	for _, name := range []string{"macos-memory", "macos-disks"} {
		for _, raw := range []string{"", "   \n", "lixo", "{", "### memsize\nabc\n", "### profiler\n[1,2]", "### memsize\n0\n", "###"} {
			func() {
				defer func() {
					if r := recover(); r != nil {
						t.Errorf("%s(%q) entrou em pânico: %v", name, raw, r)
					}
				}()
				if out, err := Render(name, raw, PT); err == nil && strings.TrimSpace(out) == "" {
					t.Errorf("%s(%q): saída vazia sem erro", name, raw)
				}
			}()
		}
	}
	if _, err := Render("macos-memory", "lixo", PT); err == nil {
		t.Error("memória: lixo devia dar erro")
	}
	if _, err := Render("macos-disks", "lixo", PT); err == nil {
		t.Error("discos: lixo devia dar erro")
	}
	if _, err := Render("macos-disks", "", PT); err == nil {
		t.Error("discos: vazio devia dar erro")
	}
}

const macDisksNVMe = `{"SPNVMeDataType":[{"_name":"Apple SSD Controller","_items":[
 {"_name":"APPLE SSD AP0512Z","device_model":"APPLE SSD AP0512Z","size":"500,28 GB","size_in_bytes":500277792768,"smart_status":"Verified","volumes":[{"_name":"X","size_in_bytes":1}]}]}],
 "SPSerialATADataType":[],
 "SPStorageDataType":[{"_name":"Data","physical_drive":{"device_name":"APPLE SSD AP0512Z","medium_type":"ssd","smart_status":"Verified","protocol":"Apple Fabric"}}]}`

func TestMacDisks(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		l    Lang
		want []string
		not  []string
	}{
		{"nvme pt", macDisksNVMe, PT, []string{"Disco: APPLE SSD AP0512Z · SSD · 500 GB · S.M.A.R.T.: verificado"}, []string{"Nenhum"}},
		{"nvme en", macDisksNVMe, EN, []string{"Disk: APPLE SSD AP0512Z · SSD · 500 GB · S.M.A.R.T.: verified"}, nil},
		{"nvme es", macDisksNVMe, ES, []string{"Disco: APPLE SSD AP0512Z · SSD · 500 GB · S.M.A.R.T.: verificado"}, nil},
		{"sata hdd failing",
			`{"SPSerialATADataType":[{"_items":[{"_name":"ST1000","device_model":"ST1000DM010","size_in_bytes":1000204886016,"smart_status":"Failing","spsata_medium_type":"rotational"}]}]}`,
			PT, []string{"Disco: ST1000DM010 · HDD · 1 TB · S.M.A.R.T.: com problema"}, nil},
		{"sata ssd not supported",
			`{"SPSerialATADataType":[{"_items":[{"_name":"Gen SSD","size_in_bytes":256060514304,"smart_status":"Not Supported","spsata_medium_type":"ssd"}]}]}`,
			PT, []string{"Disco: Gen SSD · SSD · 256 GB · S.M.A.R.T.: não suportado"}, nil},
		{"status desconhecido", `{"SPNVMeDataType":[{"_items":[{"_name":"D","size_in_bytes":1000000000,"smart_status":"Weird"}]}]}`,
			PT, []string{"Disco: D · SSD · 1 GB · S.M.A.R.T.: Weird"}, nil},
		{"campos ausentes", `{"SPNVMeDataType":[{"_items":[{"_name":"Só nome"}]}]}`, PT, []string{"Disco: Só nome · SSD"}, []string{"S.M.A.R.T"}},
		{"só storage", `{"SPStorageDataType":[{"physical_drive":{"device_name":"Disk A","medium_type":"hdd","smart_status":"Verified"}},{"physical_drive":{"device_name":"Disk A"}},{"physical_drive":{"device_name":"Img","protocol":"Disk Image"}}]}`,
			PT, []string{"Disco: Disk A · HDD · S.M.A.R.T.: verificado"}, []string{"Img"}},
		{"seções vazias", `{"SPNVMeDataType":[],"SPSerialATADataType":[],"SPStorageDataType":[]}`, PT, []string{"Nenhum disco encontrado"}, nil},
		{"seções nulas e tipos errados", `{"SPNVMeDataType":null,"SPSerialATADataType":{"a":1},"SPStorageDataType":[null,"x",{"physical_drive":5}]}`, PT, []string{"Nenhum disco encontrado"}, nil},
		{"tamanho string", `{"SPNVMeDataType":[{"_items":[{"_name":"D","size_in_bytes":"1000000000"}]}]}`, PT, []string{"Disco: D"}, nil},
		{"vazio en", `{}`, EN, []string{"No disks found"}, nil},
		{"vazio es", `{}`, ES, []string{"No se encontraron discos"}, nil},
	}
	for _, c := range cases {
		got, err := Render("macos-disks", c.raw, c.l)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		for _, w := range c.want {
			if !strings.Contains(got, w) {
				t.Errorf("%s: falta %q em %q", c.name, w, got)
			}
		}
		for _, n := range c.not {
			if strings.Contains(got, n) {
				t.Errorf("%s: não devia ter %q em %q", c.name, n, got)
			}
		}
	}
	// o mesmo disco em NVMe e Storage aparece uma vez só
	got, _ := Render("macos-disks", macDisksNVMe, PT)
	if strings.Count(got, "Disco:") != 1 {
		t.Errorf("duplicado: %q", got)
	}
}

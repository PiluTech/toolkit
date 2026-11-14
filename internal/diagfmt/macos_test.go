package diagfmt

import (
	"fmt"
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

// macAPFSPlist é um "diskutil apfs list -plist" mínimo: contêiner -> disco físico (store), na forma real.
func macAPFSPlist(pairs ...string) string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?><!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd"><plist version="1.0"><dict><key>Containers</key><array>`)
	for i := 0; i+1 < len(pairs); i += 2 {
		b.WriteString(`<dict><key>APFSContainerUUID</key><string>11111111-2222-3333-4444-555555555555</string><key>ContainerReference</key><string>` + pairs[i] + `</string><key>DesignatedPhysicalStore</key><string>` + pairs[i+1] + `</string><key>PhysicalStores</key><array><dict><key>DeviceIdentifier</key><string>` + pairs[i+1] + `</string><key>Size</key><integer>494384795648</integer></dict></array><key>Volumes</key><array><dict><key>DeviceIdentifier</key><string>` + pairs[i] + `s1</string></dict></array></dict>`)
	}
	b.WriteString(`</array></dict></plist>`)
	return b.String()
}

// macSec monta a entrada da receita: seções "### profiler" (JSON) e "### apfs" (plist).
func macSec(profiler, apfs string) string {
	return "### profiler\n" + profiler + "\n### apfs\n" + apfs + "\n"
}

var macDisksNVMe = macSec(`{"SPNVMeDataType":[{"_name":"Apple SSD Controller","_items":[
 {"_name":"APPLE SSD AP0512Z","device_model":"APPLE SSD AP0512Z","bsd_name":"disk0","size":"500,28 GB","size_in_bytes":500277792768,"smart_status":"Verified","volumes":[{"_name":"X","size_in_bytes":1}]}]}],
 "SPSerialATADataType":[],
 "SPStorageDataType":[{"_name":"Data","bsd_name":"disk3s5","physical_drive":{"device_name":"APPLE SSD AP0512Z","medium_type":"ssd","smart_status":"Verified","protocol":"Apple Fabric"}}]}`, macAPFSPlist("disk3", "disk0s2"))

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

// Lupa #56: a identidade do disco é FÍSICA (nome BSD do disco). Dois discos do MESMO modelo continuam dois; o mesmo
// dispositivo visto em duas seções, ou o volume de contêiner APFS do disco (disk3s5 -> contêiner disk3 -> disk0s2 ->
// disk0), vira um só; um externo do mesmo modelo NUNCA é fundido ao interno só por ter o modelo igual.
func TestMacDisksIdentidadeFisicaComprovada(t *testing.T) {
	nvme := func(items ...string) string {
		return `"SPNVMeDataType":[{"_name":"ctrl","_items":[` + strings.Join(items, ",") + `]}]`
	}
	item := func(bsd, smart string) string {
		return `{"_name":"SSD X1","device_model":"SSD X1","bsd_name":"` + bsd + `","size_in_bytes":500277792768,"smart_status":"` + smart + `","device_serial":"SN-` + bsd + `"}`
	}
	vol := func(bsd, smart string) string {
		return `{"_name":"V","bsd_name":"` + bsd + `","physical_drive":{"device_name":"SSD X1","medium_type":"ssd","protocol":"USB","smart_status":"` + smart + `"}}`
	}
	storage := func(vols ...string) string { return `"SPStorageDataType":[` + strings.Join(vols, ",") + `]` }
	count := func(got string) int { return strings.Count(got, "Disco: ") }

	// 1. dois iguais, SMART diferente: duas linhas
	got, _ := macDisksFor(`{` + nvme(item("disk0", "Verified"), item("disk1", "Failing")) + `}`)
	if count(got) != 2 || !strings.Contains(got, "verificado") || !strings.Contains(got, "com problema") || strings.Contains(got, "SN-") {
		t.Errorf("dois discos do mesmo modelo continuam dois, cada um com o seu SMART:\n%s", got)
	}
	// 2. Lupa: primário disk0 (Verified) + externo USB só-Storage disk6s2 do MESMO modelo (Not Supported): DOIS discos,
	//    cada um com o seu SMART (o externo não some e o SMART não vai para o errado)
	got, _ = macDisksFor(`{` + nvme(item("disk0", "Verified")) + `,` + storage(vol("disk6s2", "Not Supported")) + `}`)
	if count(got) != 2 || !strings.Contains(got, "verificado") || !strings.Contains(got, "não suportado") {
		t.Errorf("primário + externo do mesmo modelo são dois discos:\n%s", got)
	}
	for _, line := range strings.Split(got, "\n") {
		if strings.Contains(line, "USB") || strings.Contains(line, "500 GB") && strings.Contains(line, "não suportado") {
			t.Errorf("o SMART do externo não pode ir para o primário: %q", line)
		}
	}
	// o mesmo, com o externo em contêiner APFS (disk8 -> disk6s1): continua distinto do interno
	got, _ = macDisksFor(macSec(`{`+nvme(item("disk0", "Verified"))+`,`+storage(vol("disk8s1", "Not Supported"))+`}`, macAPFSPlist("disk8", "disk6s1", "disk3", "disk0s2")))
	if count(got) != 2 || !strings.Contains(got, "não suportado") {
		t.Errorf("externo APFS (disk8 -> disk6) é outro disco:\n%s", got)
	}
	// 3. CONTROLE: a mesma visão do dispositivo junta. (a) o volume do contêiner APFS do interno (disk3s5 -> disk3 ->
	//    disk0s2 -> disk0) completa o disco0 com o SMART do Storage
	got, _ = macDisksFor(macSec(`{`+nvme(item("disk0", ""))+`,`+storage(vol("disk3s5", "Verified"))+`}`, macAPFSPlist("disk3", "disk0s2")))
	if count(got) != 1 || !strings.Contains(got, "verificado") {
		t.Errorf("volume de contêiner APFS do disk0 é o próprio disk0:\n%s", got)
	}
	//    (b) volume em partição direta do disco (disk0s2 -> disk0)
	got, _ = macDisksFor(`{` + nvme(item("disk0", "")) + `,` + storage(vol("disk0s2", "Verified")) + `}`)
	if count(got) != 1 || !strings.Contains(got, "verificado") {
		t.Errorf("volume em partição do disk0 é o próprio disk0:\n%s", got)
	}
	//    (c) o mesmo bsd_name em NVMe e SATA: um só, com o pior status
	got, _ = macDisksFor(`{` + nvme(item("disk0", "Verified")) + `,"SPSerialATADataType":[{"_name":"c","_items":[{"_name":"SSD X1","device_model":"SSD X1","bsd_name":"disk0","size_in_bytes":500277792768,"smart_status":"Failing","spsata_medium_type":"Solid State"}]}]}`)
	if count(got) != 1 || !strings.Contains(got, "com problema") || strings.Contains(got, "verificado") {
		t.Errorf("o mesmo dispositivo em duas seções vira um e o pior status vence:\n%s", got)
	}
	// 4. sem prova nada se junta: volume de contêiner SEM mapa APFS, e primário sem bsd_name
	got, _ = macDisksFor(`{` + nvme(item("disk0", "Verified")) + `,` + storage(vol("disk3s5", "Verified")) + `}`)
	if count(got) != 2 {
		t.Errorf("sem o mapa APFS não há prova de que disk3s5 é do disk0: não funde:\n%s", got)
	}
	noBSD := `{"_name":"SSD X1","device_model":"SSD X1","size_in_bytes":500277792768,"smart_status":"Verified"}`
	got, _ = macDisksFor(`{` + nvme(noBSD, noBSD) + `,` + storage(vol("disk6s2", "Verified")) + `}`)
	if count(got) != 3 {
		t.Errorf("sem bsd_name no primário nada funde:\n%s", got)
	}
	// 5. mapa APFS ilegível = sem prova (e nunca erro)
	got, err := macDisksFor("### profiler\n{" + nvme(item("disk0", "Verified")) + "," + storage(vol("disk3s5", "Verified")) + "}\n### apfs\n<plist><dict><key>Containers</key><array><dict>")
	if err != nil || count(got) != 2 {
		t.Errorf("plist truncado: sem prova, sem erro (%v):\n%s", err, got)
	}
}

func TestMacAPFSPhysicalMap(t *testing.T) {
	m := macAPFSPhysical(macAPFSPlist("disk3", "disk0s2", "disk8", "disk6s1"))
	if fmt.Sprint(m["disk3"]) != "[disk0]" || fmt.Sprint(m["disk8"]) != "[disk6]" || len(m) != 2 {
		t.Errorf("mapa: %v", m)
	}
	for _, bad := range []string{"", "lixo", "<plist><dict>", "<plist><dict><key>Containers</key><string>x</string></dict></plist>"} {
		if m := macAPFSPhysical(bad); len(m) != 0 {
			t.Errorf("%q deveria dar mapa vazio: %v", bad, m)
		}
	}
}

// Lupa #56 (rodada 3): a identidade de um disco só do Storage é o disco PAI do volume (bsd_name), nunca modelo+protocolo.
func TestMacDisksStorageSoIdentidadePeloDiscoPai(t *testing.T) {
	vol := func(bsd string) string {
		return `{"_name":"V","bsd_name":"` + bsd + `","physical_drive":{"device_name":"Elements 2TB","medium_type":"hdd","protocol":"USB","smart_status":"Not Supported"}}`
	}
	one := func(vols ...string) string {
		got, err := macDisksFor(`{"SPStorageDataType":[` + strings.Join(vols, ",") + `]}`)
		if err != nil {
			t.Fatal(err)
		}
		return got
	}
	// dois externos IGUAIS (disk6 e disk7): dois discos
	if got := one(vol("disk6s2"), vol("disk7s2")); strings.Count(got, "Disco: Elements 2TB") != 2 {
		t.Errorf("dois externos iguais são dois discos:\n%s", got)
	}
	// controle: dois volumes do MESMO disco (disk6s1 e disk6s2, e o próprio disk6): um disco
	if got := one(vol("disk6s1"), vol("disk6s2"), vol("disk6")); strings.Count(got, "Disco: Elements 2TB") != 1 {
		t.Errorf("volumes do mesmo disco viram um:\n%s", got)
	}
	// misturado: disk6 (dois volumes) + disk7: dois
	if got := one(vol("disk6s1"), vol("disk7s1"), vol("disk6s2")); strings.Count(got, "Disco: Elements 2TB") != 2 {
		t.Errorf("disk6 (2 volumes) + disk7 = dois:\n%s", got)
	}
	// sem identidade (bsd_name ausente ou fora do formato) nunca funde por modelo/protocolo
	if got := one(vol(""), vol("")); strings.Count(got, "Disco: Elements 2TB") != 2 {
		t.Errorf("sem bsd_name não há fusão:\n%s", got)
	}
	if got := one(vol("xyz"), vol("xyz")); strings.Count(got, "Disco: Elements 2TB") != 2 {
		t.Errorf("bsd_name fora do formato não funde:\n%s", got)
	}
	for in, want := range map[string]string{"disk6s2": "disk6", "disk3s1s1": "disk3", "disk0": "disk0", "disk10s4": "disk10", "": "", "xyz": "", "disk": "", "disk6x": ""} {
		if got := macParentBSD(in); got != want {
			t.Errorf("macParentBSD(%q) = %q, quer %q", in, got, want)
		}
	}
}

func macDisksFor(raw string) (string, error) { return Render("macos-disks", raw, PT) }

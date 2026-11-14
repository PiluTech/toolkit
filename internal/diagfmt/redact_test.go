package diagfmt

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestRedactRemovesMachineIdentifiersEverywhere(t *testing.T) {
	secrets := []string{"C02XK1ABCDEF", "SN-12345-SECRET", "4C4C4544-0042-5710-8039-B2C04F485932", "aa:bb:cc:dd:ee:ff", "ECID0123456789", "WWNSECRET99", "356938035643809"}
	cases := map[string]string{
		"json válido":                    `{"SPNVMeDataType":[{"_name":"APPLE SSD","device_serial":"C02XK1ABCDEF","bsd_name":"disk0","size_in_bytes":500277792768,"spnvme_smart_status":"Verified","volume_uuid":"4C4C4544-0042-5710-8039-B2C04F485932"}],"x":{"serial_number":"SN-12345-SECRET","mac_address":"aa:bb:cc:dd:ee:ff","ecid":"ECID0123456789","wwn":"WWNSECRET99"}}`,
		"json truncado no meio do valor": `{"a":[{"_name":"APPLE SSD","device_serial":"C02XK1ABCDEF"},{"serial_number":"SN-12345-SEC`,
		"json multilinha":                "{\n  \"disks\" : [\n    {\n      \"device_serial\" : \"C02XK1ABCDEF\",\n      \"_name\" : \"X\",\n      \"imei\" : 356938035643809\n    }\n  ]\n}",
		"texto chave: valor":             "Model: X\nSerial Number: SN-12345-SECRET\nHardware UUID: 4C4C4544-0042-5710-8039-B2C04F485932\nEthernet Address: aa:bb:cc:dd:ee:ff\n",
		"powershell tabela":              "SerialNumber : SN-12345-SECRET\nFriendlyName: Disco\nUniqueId = ECID0123456789\n",
		"uuid solto":                     "ref 4C4C4544-0042-5710-8039-B2C04F485932 e mac AA-BB-CC-DD-EE-FF",
	}
	for name, in := range cases {
		out := Redact(in)
		for _, s := range secrets {
			if strings.Contains(strings.ToLower(out), strings.ToLower(s)) {
				t.Errorf("%s: %q sobrou na saída:\n%s", name, s, out)
			}
		}
	}
}

func TestRedactKeepsEverythingElse(t *testing.T) {
	in := `{"_name":"APPLE SSD AP0512Z","bsd_name":"disk0","size_in_bytes":500277792768,"spnvme_smart_status":"Verified"}`
	if out := Redact(in); out != in {
		t.Errorf("sem identificador nada muda:\n%s", out)
	}
	if out := Redact(`{"device_serial":"ABC","_name":"X"}`); !strings.Contains(out, `"device_serial":"[***]"`) || !strings.Contains(out, `"_name":"X"`) {
		t.Errorf("a chave fica e só o valor some: %s", out)
	}
	if Redact("") != "" {
		t.Error("vazio")
	}
}

// Lupa #56 (rodada 3): a tabela REAL do Format-Table (cabeçalho, traços, dados), tabela de colunas do lsblk, JSON com
// aspas escapadas e valores não alfanuméricos.
func TestRedactRealTablesAndEscapedJSON(t *testing.T) {
	secrets := []string{"6B8A-1234-ABCD", "WD-WXB1A2345678", `SN"12\34`, "SN/é☃-9", "0123 4567"}
	cases := map[string]string{
		"format-table":             "FriendlyName                SerialNumber   MediaType HealthStatus\n------------                ------------   --------- ------------\nVirtual_Disk NVME Premium   6B8A-1234-ABCD SSD       Healthy\nWDC WD10EZEX                WD-WXB1A2345678 HDD      Healthy\n",
		"lsblk colunas":            "NAME SIZE SERIAL         MODEL\nsda  1T   WD-WXB1A2345678 WDC\nnvme0n1 500G 6B8A-1234-ABCD APPLE\n",
		"json com escapes":         `{"disks":[{"_name":"X","device_serial":"SN\"12\\34","note":"ok"},{"serial_number":"SN/é☃-9"},{"wwn":"0123 4567"}]}`,
		"json truncado com escape": `{"disks":[{"device_serial":"SN\"12\\34`,
	}
	for name, in := range cases {
		out := Redact(in)
		for _, s := range secrets {
			if strings.Contains(out, s) {
				t.Errorf("%s: %q sobrou:\n%s", name, s, out)
			}
		}
	}
	// o que não é identificador sobrevive, e as colunas seguintes continuam no lugar
	out := Redact(cases["format-table"])
	for _, want := range []string{"FriendlyName", "SerialNumber", "Virtual_Disk NVME Premium", "WDC WD10EZEX", "SSD", "HDD", "Healthy", "[***]"} {
		if !strings.Contains(out, want) {
			t.Errorf("tabela: faltou %q:\n%s", want, out)
		}
	}
	out = Redact(cases["json com escapes"])
	if !strings.Contains(out, `"_name":"X"`) || !strings.Contains(out, `"note":"ok"`) {
		t.Errorf("json válido: o resto fica:\n%s", out)
	}
	out = Redact(cases["lsblk colunas"])
	if !strings.Contains(out, "sda") || !strings.Contains(out, "WDC") || !strings.Contains(out, "APPLE") {
		t.Errorf("lsblk: nome e modelo ficam:\n%s", out)
	}
}

func TestRedactValidJSONIsRedactedByKeyAtAnyDepth(t *testing.T) {
	in := `{"a":{"b":[{"c":{"hardware_uuid":"X-1","list":["a","b"],"n":42}}]},"serial":12345,"flag":true,"nome":"Disco 4C4C4544-0042-5710-8039-B2C04F485932"}`
	out := Redact(in)
	for _, bad := range []string{"X-1", "12345", "4C4C4544"} {
		if strings.Contains(out, bad) {
			t.Errorf("%q sobrou: %s", bad, out)
		}
	}
	for _, ok := range []string{`"n":42`, `"flag":true`, `"list":["a","b"]`} {
		if !strings.Contains(out, ok) {
			t.Errorf("faltou %s: %s", ok, out)
		}
	}
}

// Mensagem de log com identificador no meio da frase.
func TestRedactInlineIdentifierInsideASentence(t *testing.T) {
	in := "- 2026-11-13 kernel ×2 [erro] falha no disco serial: SN-SECRETO-1, uuid=4F5E-AA e Serial Number: ABC123;"
	out := Redact(in)
	for _, bad := range []string{"SN-SECRETO-1", "4F5E-AA", "ABC123"} {
		if strings.Contains(out, bad) {
			t.Errorf("%q sobrou: %s", bad, out)
		}
	}
	if !strings.Contains(out, "falha no disco serial: [***]") {
		t.Errorf("o resto da frase fica: %s", out)
	}
	if out := Redact("a série de testes terminou: ok"); out != "a série de testes terminou: ok" {
		t.Errorf("texto comum não muda: %s", out)
	}
}

// Lupa #60 (2): JSON válido é redigido por chave e por VALOR decodificado e continua JSON válido, com serial entre aspas
// escapadas e barras (nada do valor sobra, nenhuma barra de escape se perde).
func TestRedactValidJSONWithEscapedQuotesStaysValidAndLeakFree(t *testing.T) {
	in := `{"events":[{"Message":"falha serial: SN\"12\\34 e C:\\Users\\x e uuid=4F5E-AA","Count":3}],"serial_number":"SN\"99\\9"}`
	out := Redact(in)
	var doc map[string]any
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("o resultado precisa ser JSON válido: %v\n%s", err, out)
	}
	for _, bad := range []string{`SN"12`, `12\34`, `SN"99`, `99\9`, "4F5E-AA"} {
		if strings.Contains(out, bad) || strings.Contains(out, strings.ReplaceAll(bad, `"`, `\"`)) {
			t.Errorf("%q sobrou: %s", bad, out)
		}
	}
	ev := doc["events"].([]any)[0].(map[string]any)
	if ev["Count"].(float64) != 3 || !strings.Contains(ev["Message"].(string), `C:\Users\x`) || !strings.Contains(ev["Message"].(string), "serial: [***]") {
		t.Errorf("o resto do valor fica: %v", ev)
	}
}

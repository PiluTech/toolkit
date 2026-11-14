package diagfmt

import (
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

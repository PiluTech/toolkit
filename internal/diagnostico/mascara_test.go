package diagnostico

import (
	"strings"
	"testing"
)

func TestMaskTextRemovesSensitiveData(t *testing.T) {
	in := strings.Join([]string{
		`Falha em C:\Users\paulo\AppData\x.dll e /Users/paulo/Library/y e /home/paulo/.cache/z`,
		`Máquina PAULO-NOTEBOOK usuário Paulo, contato paulo@example.com`,
		`IP 203.0.113.7 e 192.168.1.20 e 10.0.0.5 e versão 1.2.3.4567 e 2001:db8:abcd::5 e fe80::1`,
		`UUID 4C4C4544-0042-5710-8039-B2C04F485932 MAC aa:bb:cc:dd:ee:ff Serial Number: SN-123`,
		`compartilhamento \\SERVIDOR\pasta`,
	}, "\n")
	out := MaskText(in, []string{"PAULO-NOTEBOOK", "paulo", "x"})
	for _, bad := range []string{"paulo", "PAULO", "example.com", "203.0.113", "2001:db8", "4C4C4544", "aa:bb:cc", "SN-123", "SERVIDOR"} {
		if strings.Contains(out, bad) {
			t.Errorf("%q sobrou:\n%s", bad, out)
		}
	}
	for _, keep := range []string{"192.168.1.20", "10.0.0.5", "fe80::1", "203.x.x.x", "2001:x:x", "[usuário]", "[pessoal]", "[e-mail]", "1.2.3.4567"} {
		if !strings.Contains(out, keep) {
			t.Errorf("faltou %q:\n%s", keep, out)
		}
	}
}

func TestMaskTextNomesCurtosNaoMascaramTextoComum(t *testing.T) {
	if out := MaskText("a máquina está ok", []string{"a", "", " "}); out != "a máquina está ok" {
		t.Errorf("%q", out)
	}
}

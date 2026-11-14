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

// Lupa #60 (3): nomes curtos (usuário "Li", máquina "PC") são mascarados por TOKEN, sem dispensá-los.
func TestMaskTextNomesCurtosPorToken(t *testing.T) {
	in := "falha no PC do usuário Li (user=Li, PC-01 e Li/pasta) em PC. PCI e list e Lisa e UPC e li ficam; Li Li"
	out := MaskText(in, []string{"Li", "PC"})
	for _, bad := range []string{"no PC do", "usuário Li", "user=Li", "em PC."} {
		if strings.Contains(out, bad) {
			t.Errorf("%q sobrou: %s", bad, out)
		}
	}
	for _, keep := range []string{"PCI", "list", "Lisa", "UPC", " li "} {
		if !strings.Contains(out, keep) {
			t.Errorf("%q não pode ser alterado: %s", keep, out)
		}
	}
	if strings.Contains(out, "Li Li") || strings.Count(out, "[pessoal]") < 6 {
		t.Errorf("tokens vizinhos também são mascarados: %s", out)
	}
	// 1 caractere não dá para distinguir de texto comum: ignorado
	if got := MaskText("a máquina está ok", []string{"a"}); got != "a máquina está ok" {
		t.Errorf("%q", got)
	}
}

func TestMaskDocumentSoMascaraValores(t *testing.T) {
	doc := map[string]any{"report": "report ok", "n": 3, "list": []any{"paulo", map[string]any{"paulo": "x paulo"}}}
	out := MaskDocument(doc, []string{"report", "paulo"}).(map[string]any)
	if out["report"] != "[pessoal] ok" || out["n"] != 3 {
		t.Errorf("%v", out)
	}
	l := out["list"].([]any)
	m := l[1].(map[string]any)
	if l[0] != "[pessoal]" || m["paulo"] != "x [pessoal]" {
		t.Errorf("chaves intactas, valores mascarados: %v", out)
	}
}

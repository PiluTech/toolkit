package textsafe

import (
	"strings"
	"testing"
)

func TestPrintableEscapesTerminalSequences(t *testing.T) {
	bs := string(rune(92)) // barra invertida, sem depender de escapes no fonte
	cases := []struct{ in, want string }{
		{"\x1b[2J limpa", bs + "x1b[2J limpa"},
		{"\x1b]0;titulo\x07", bs + "x1b]0;titulo" + bs + "x07"},
		{"a\rb", "a" + bs + "x0db"},
		{"bidi " + string(rune(0x202e)) + " fim", "bidi " + bs + "u202e fim"},
		{"zw" + string(rune(0x200b)) + "sp", "zw" + bs + "u200bsp"},
		{string(rune(0x2028)), bs + "u2028"},
		{"c1" + string(rune(0x9b)), "c1" + bs + "x9b"},
		{"normal ação", "normal ação"},
	}
	for _, c := range cases {
		if got := Printable(c.in); got != c.want {
			t.Errorf("Printable(%q) = %q, quero %q", c.in, got, c.want)
		}
	}
}

func TestLineNeverHasBreaks(t *testing.T) {
	got := Line("a\nb\r\nc\td\x1b[31m")
	if strings.ContainsAny(got, "\n\r\t\x1b") {
		t.Fatalf("Line deixou controle: %q", got)
	}
	if !strings.Contains(got, "a ⏎ b ⏎ c d") {
		t.Fatalf("Line = %q", got)
	}
}

func TestMultilinePreservaLFETabEEscapaControles(t *testing.T) {
	in := "linha 1\n\tcom tab, ação e 日本\r\nlinha 3"
	if got := Multiline(in); got != "linha 1\n\tcom tab, ação e 日本\nlinha 3" {
		t.Errorf("dados legíveis, LF e Tab ficam (e CRLF vira LF): %q", got)
	}
	for name, in := range map[string]string{
		"OSC 52 (área de transferência)": "a\x1b]52;c;ZXZpbA==\x07b",
		"CSI limpa tela":                 "a\x1b[2Jb",
		"BEL":                            "a\x07b",
		"NUL":                            "a\x00b",
		"DEL":                            "a\x7fb",
		"C1 CSI (U+009B)":                "a\u009b31mb",
		"C1 OSC (U+009D)":                "a\u009d0;t\u009cb",
		"bidi":                           "a" + string(rune(0x202e)) + "b",
		"CR sozinho":                     "a\rb",
		"U+2028":                         "a b",
	} {
		got := Multiline(in)
		for _, r := range got {
			if r != '\n' && r != '\t' && (r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f) || r == 0x202e || r == 0x2028) {
				t.Errorf("%s: sobrou o controle %U em %q", name, r, got)
			}
		}
		if !strings.Contains(got, "a") || !strings.Contains(got, `\`) {
			t.Errorf("%s: o controle aparece escapado e visível: %q", name, got)
		}
	}
}

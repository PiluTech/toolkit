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

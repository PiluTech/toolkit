package catalog

import (
	"strings"
	"testing"
)

const noteTail = "\n    category: dev\n    any: [{provider: apt, package: g}]"

func TestParseNoteValida(t *testing.T) {
	cat := mustParse(t, "n", "apps:\n  g:\n    name: G\n    note: \"Só instale se souber o que faz. Use senha forte.\""+noteTail+"\n  h:\n    name: H"+noteTail+"\n")
	if got := cat.Apps["g"].Note; got != "Só instale se souber o que faz. Use senha forte." {
		t.Fatalf("note: %q", got)
	}
	if cat.Apps["h"].Note != "" {
		t.Fatalf("app sem note deve ficar vazio: %q", cat.Apps["h"].Note)
	}
}

func TestParseNoteRecusada(t *testing.T) {
	long := strings.Repeat("a", MaxNote+1)
	cases := []struct{ name, yaml, want string }{
		{"vazia", "apps:\n  g:\n    name: G\n    note: \"  \"" + noteTail, "note"},
		{"grande demais", "apps:\n  g:\n    name: G\n    note: " + long + noteTail, "note"},
		{"com template", "apps:\n  g:\n    name: G\n    note: \"use {{version}}\"" + noteTail, "template"},
		{"ESC", "apps:\n  g:\n    name: G\n    note: \"a\\e[2Jb\"" + noteTail, "U+001B"},
		{"bidi U+202E", "apps:\n  g:\n    name: G\n    note: \"a\\u202eb\"" + noteTail, "U+202E"},
		{"quebra de linha", "apps:\n  g:\n    name: G\n    note: \"a\\nb\"" + noteTail, "U+000A"},
		{"lista em vez de texto", "apps:\n  g:\n    name: G\n    note: [a]" + noteTail, "note"},
		{"repetida", "apps:\n  g:\n    name: G\n    note: a\n    note: b" + noteTail, "note"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := Parse("n", []byte(c.yaml))
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("esperava erro com %q, veio %v", c.want, err)
			}
		})
	}
}

func TestParseNoteNoLimiteExato(t *testing.T) {
	mustParse(t, "n", "apps:\n  g:\n    name: G\n    note: "+strings.Repeat("a", MaxNote)+noteTail+"\n")
}

func TestMergeEClonePreservamNote(t *testing.T) {
	base := mustParse(t, "b", "apps:\n  g:\n    name: G\n    note: aviso"+noteTail+"\n")
	m, _ := Merge(base, nil, false)
	if m.Apps["g"].Note != "aviso" {
		t.Fatalf("Merge perdeu a note: %q", m.Apps["g"].Note)
	}
	ext := mustParse(t, "e", "apps:\n  g:\n    name: G2"+noteTail+"\n")
	m2, _ := Merge(base, ext, false)
	if m2.Apps["g"].Note != "" {
		t.Fatalf("o app do ext substitui inteiro, sem herdar a note: %q", m2.Apps["g"].Note)
	}
}

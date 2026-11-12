package recipes

import (
	"fmt"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/PiluTech/toolkit/internal/platform"
)

const tiny = "id: %s\nname: N\nos: %s\nsteps:\n  - id: a\n    run: { sh: t }\n"

func recipe(id, os string) *fstest.MapFile {
	return &fstest.MapFile{Data: []byte(fmt.Sprintf(tiny, id, os))}
}

func TestLoadFS(t *testing.T) {
	fsys := fstest.MapFS{
		"recipes/linux/b.yaml":   recipe("b", "linux"),
		"recipes/linux/a.yaml":   recipe("a", "linux"),
		"recipes/macos/a.yaml":   recipe("a", "macos"),
		"recipes/linux/README":   {Data: []byte("ignorado")},
		"recipes/linux/sub/x.go": {Data: []byte("ignorado")},
	}
	set, err := LoadFS(fsys, "recipes")
	if err != nil {
		t.Fatal(err)
	}
	lin := set.For(platform.Linux)
	if len(lin) != 2 || lin[0].ID != "a" || lin[1].ID != "b" {
		t.Fatalf("linux: %+v", lin)
	}
	if _, ok := set.Get(platform.Darwin, "a"); !ok {
		t.Fatal("macos/a")
	}
	if _, ok := set.Get(platform.Windows, "a"); ok {
		t.Fatal("windows/a não existe")
	}
	if len(set.All()) != 3 {
		t.Fatalf("all=%d", len(set.All()))
	}
}

func TestLoadFSErrors(t *testing.T) {
	cases := map[string]struct {
		fs   fstest.MapFS
		want string
	}{
		"pasta errada":    {fstest.MapFS{"recipes/windows/a.yaml": recipe("a", "linux")}, "a receita é de linux mas está na pasta windows"},
		"so desconhecido": {fstest.MapFS{"recipes/plan9/a.yaml": recipe("a", "linux")}, "pasta de SO desconhecida"},
		"id x arquivo":    {fstest.MapFS{"recipes/linux/b.yaml": recipe("a", "linux")}, "deve ser igual ao nome do arquivo"},
		"yaml ruim":       {fstest.MapFS{"recipes/linux/a.yaml": {Data: []byte("id: [")}}, "YAML inválido"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := LoadFS(c.fs, "recipes")
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("err=%v, quer %q", err, c.want)
			}
		})
	}
	if _, err := LoadFS(fstest.MapFS{}, "recipes"); err == nil {
		t.Fatal("raiz ausente deve falhar")
	}
}

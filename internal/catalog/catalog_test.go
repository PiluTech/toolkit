package catalog

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const valid = `
apps:
  git:
    name: Git
    category: dev
    versions: true
    windows: [{ provider: winget, id: Git.Git }]
    macos:   [{ provider: brew, formula: git }]
    linux:
      - { provider: apt, package: git }
      - { provider: pacman, package: git }
  python:
    name: Python
    category: dev
    versions: ["3.13", "3.12"]
    windows: [{ provider: winget, id: "Python.Python.{{major_minor}}" }]
    any: [{ provider: flatpak, id: org.python.Python }]
  tool:
    name: Tool
    category: utilitarios
    any:
      - provider: github-release
        repo: dono/repo
        asset: "*_{{os}}_{{arch}}.zip"
  msi:
    name: Exemplo MSI
    category: utilitarios
    windows:
      - provider: url
        url: https://exemplo.com/app-1.2.3-x64.msi
        sha256: 3f5a3f5a3f5a3f5a3f5a3f5a3f5a3f5a3f5a3f5a3f5a3f5a3f5a3f5a3f5a3f5a
        args: ["/qn", "/norestart"]
`

func TestParseValido(t *testing.T) {
	c, err := Parse("catalog.yaml", []byte(valid))
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(c.Order, ","); got != "git,python,tool,msi" {
		t.Fatalf("ordem: %s", got)
	}
	git, _ := c.Get("git")
	if !git.Versions.Query || git.Versions.Enabled() == false {
		t.Fatalf("versions: %+v", git.Versions)
	}
	py, _ := c.Get("python")
	if len(py.Versions.List) != 2 || py.Versions.List[0] != "3.13" {
		t.Fatalf("versions: %+v", py.Versions)
	}
	if l := py.InstallersFor("linux"); len(l) != 1 || l[0].Provider != ProviderFlatpak {
		t.Fatalf("fallback any: %+v", l)
	}
	if l := py.InstallersFor("windows"); l[0].ID != "Python.Python.{{major_minor}}" {
		t.Fatalf("template deve ficar intacto na carga: %+v", l)
	}
	if l := git.InstallersFor("darwin"); len(l) != 1 || l[0].Formula != "git" {
		t.Fatalf("darwin -> macos: %+v", l)
	}
	msi, _ := c.Get("msi")
	if in := msi.Installers[KeyWindows][0]; len(in.Args) != 2 || in.Type != "" {
		t.Fatalf("%+v", in)
	}
}

func TestParseErros(t *testing.T) {
	hash := strings.Repeat("a", 64)
	cases := []struct {
		name string
		yaml string
		want string // trecho da mensagem, com linha:coluna quando relevante
	}{
		{"vazio", "", "arquivo vazio"},
		{"yaml quebrado", "apps: [", "YAML inválido"},
		{"sem apps", "x: 1", `chave desconhecida "x" no topo`},
		{"id inválido", "apps:\n  Git:\n    name: G\n    category: dev\n    any: [{provider: apt, package: g}]", `id inválido`},
		{"chave desconhecida do app", "apps:\n  g:\n    name: G\n    category: dev\n    pakage: x\n    any: [{provider: apt, package: g}]", `catalog.yaml:5:5: app "g": chave desconhecida "pakage"`},
		{"categoria", "apps:\n  g:\n    name: G\n    category: xyz\n    any: [{provider: apt, package: g}]", `categoria "xyz" desconhecida`},
		{"sem nome", "apps:\n  g:\n    category: dev\n    any: [{provider: apt, package: g}]", `falta "name"`},
		{"sem instalador", "apps:\n  g:\n    name: G\n    category: dev", "nenhum instalador"},
		{"duplicado", "apps:\n  g:\n    name: G\n    category: dev\n    any: [{provider: apt, package: g}]\n  g:\n    name: G\n    category: dev\n    any: [{provider: apt, package: g}]", `app "g" duplicado`},
		{"provider desconhecido", "apps:\n  g:\n    name: G\n    category: dev\n    any: [{provider: chocolatey, id: g}]", `provider "chocolatey" desconhecido`},
		{"winget sem id", "apps:\n  g:\n    name: G\n    category: dev\n    windows:\n      - provider: winget", `catalog.yaml:6:9: app "g": provider "winget" exige "id"`},
		{"campo de outro provider", "apps:\n  g:\n    name: G\n    category: dev\n    windows: [{provider: winget, id: x, package: y}]", `campo "package" não vale para o provider "winget"`},
		{"chave de instalador desconhecida", "apps:\n  g:\n    name: G\n    category: dev\n    windows: [{provider: winget, id: x, idd: y}]", `chave desconhecida "idd"`},
		{"brew sem nada", "apps:\n  g:\n    name: G\n    category: dev\n    macos: [{provider: brew}]", `exatamente um entre "formula" e "cask"`},
		{"brew com os dois", "apps:\n  g:\n    name: G\n    category: dev\n    macos: [{provider: brew, formula: a, cask: b}]", `exatamente um entre`},
		{"url sem sha256", "apps:\n  g:\n    name: G\n    category: dev\n    windows: [{provider: url, url: \"https://x.com/a.msi\"}]", `provider "url" exige "sha256"`},
		{"url http", "apps:\n  g:\n    name: G\n    category: dev\n    windows: [{provider: url, url: \"http://x.com/a.msi\", sha256: " + hash + "}]", "https://"},
		{"sha256 curto", "apps:\n  g:\n    name: G\n    category: dev\n    windows: [{provider: url, url: \"https://x.com/a.msi\", sha256: abc}]", "64 caracteres"},
		{"type inválido", "apps:\n  g:\n    name: G\n    category: dev\n    windows: [{provider: url, url: \"https://x.com/a\", sha256: " + hash + ", type: bat}]", `type "bat" desconhecido`},
		{"repo inválido", "apps:\n  g:\n    name: G\n    category: dev\n    any: [{provider: github-release, repo: semdono, asset: a}]", "dono/repositório"},
		{"template desconhecido", "apps:\n  g:\n    name: G\n    category: dev\n    any: [{provider: apt, package: \"x{{foo}}\"}]", `variável desconhecida "foo"`},
		{"template de versão sem lista", "apps:\n  g:\n    name: G\n    category: dev\n    any: [{provider: apt, package: \"py{{major_minor}}\"}]", `"versions" precisa ser uma lista`},
		{"template de versão com versions true", "apps:\n  g:\n    name: G\n    category: dev\n    versions: true\n    any: [{provider: apt, package: \"py{{version}}\"}]", `"versions" precisa ser uma lista`},
		{"versions inválido", "apps:\n  g:\n    name: G\n    category: dev\n    versions: sim\n    any: [{provider: apt, package: g}]", "versions deve ser true ou uma lista"},
		{"versions vazio", "apps:\n  g:\n    name: G\n    category: dev\n    versions: []\n    any: [{provider: apt, package: g}]", "não pode ser vazia"},
		{"lista de SO não é lista", "apps:\n  g:\n    name: G\n    category: dev\n    windows: x", "deve ser uma lista de instaladores"},
	}
	for _, c := range cases {
		_, err := Parse("catalog.yaml", []byte(c.yaml))
		if err == nil {
			t.Errorf("%s: esperava erro", c.name)
			continue
		}
		if !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s:\n  erro:  %s\n  falta: %s", c.name, err, c.want)
		}
	}
}

func TestParseRelataTodosOsErros(t *testing.T) {
	src := "apps:\n  a:\n    name: A\n    category: xx\n    any: [{provider: apt}]\n  b:\n    category: dev\n    any: [{provider: apt, package: b}]\n"
	_, err := Parse("c.yaml", []byte(src))
	var errs Errors
	if !errors.As(err, &errs) || len(errs) < 3 {
		t.Fatalf("esperava 3+ erros, veio: %v", err)
	}
	if errs[0].Line == 0 || errs[0].File != "c.yaml" {
		t.Fatalf("posição ausente: %+v", errs[0])
	}
}

func TestMerge(t *testing.T) {
	base, _ := Parse("b", []byte("apps:\n  a:\n    name: A1\n    category: dev\n    any: [{provider: apt, package: a}]\n  b:\n    name: B\n    category: dev\n    any: [{provider: apt, package: b}]\n"))
	ext, _ := Parse("e", []byte("apps:\n  a:\n    name: A2\n    category: dev\n    any: [{provider: apt, package: a}]\n  c:\n    name: C\n    category: dev\n    any: [{provider: apt, package: c}]\n"))

	m := Merge(base, ext, false)
	if strings.Join(m.Order, ",") != "a,b,c" || m.Apps["a"].Name != "A2" {
		t.Fatalf("merge: %v %s", m.Order, m.Apps["a"].Name)
	}
	r := Merge(base, ext, true)
	if strings.Join(r.Order, ",") != "a,c" || len(r.Apps) != 2 {
		t.Fatalf("replace: %v", r.Order)
	}
	if base.Apps["a"].Name != "A1" {
		t.Fatal("merge não pode alterar a base")
	}
	if got := len(m.List()); got != 3 {
		t.Fatalf("List: %d", got)
	}
}

func TestLoadFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "meu.yaml")
	if err := os.WriteFile(p, []byte(valid), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := LoadFile(p)
	if err != nil || len(c.Apps) != 4 {
		t.Fatalf("%v", err)
	}
	if _, err := LoadFile(filepath.Join(dir, "nao-existe.yaml")); err == nil {
		t.Fatal("esperava erro")
	}
	bad := filepath.Join(dir, "ruim.yaml")
	_ = os.WriteFile(bad, []byte("apps: {x: {}}"), 0o600)
	if _, err := LoadFile(bad); err == nil || !strings.Contains(err.Error(), "ruim.yaml:") {
		t.Fatalf("erro deveria citar o arquivo: %v", err)
	}
}

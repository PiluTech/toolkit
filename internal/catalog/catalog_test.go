package catalog

import (
	"errors"
	"fmt"
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
		{"chave repetida no topo", "apps: {}\napps: {}", `topo do arquivo: chave "apps" repetida`},
		{"chave repetida no app", "apps:\n  g:\n    name: G\n    category: dev\n    windows: [{provider: winget, id: a}]\n    windows: [{provider: winget, id: b}]", `app "g": chave "windows" repetida`},
		{"name repetido", "apps:\n  g:\n    name: G\n    category: dev\n    name: H\n    any: [{provider: apt, package: g}]", `chave "name" repetida`},
		{"chave repetida no instalador", "apps:\n  g:\n    name: G\n    category: dev\n    windows: [{provider: winget, id: a, id: b}]", `chave "id" repetida`},
		{"provider repetido", "apps:\n  g:\n    name: G\n    category: dev\n    windows: [{provider: winget, provider: apt, id: a}]", `chave "provider" repetida`},
		{"alias", "x: &a {}\napps:\n  g: *a", "âncoras e aliases"},
		{"merge key", "base: &b {name: G}\napps:\n  g:\n    <<: *b", "não são aceitos"},
		{"url userinfo", "apps:\n  g:\n    name: G\n    category: dev\n    windows: [{provider: url, url: \"https://u:p@x.com/a.msi\", sha256: " + hash + "}]", "userinfo"},
		{"url sem host", "apps:\n  g:\n    name: G\n    category: dev\n    windows: [{provider: url, url: \"https:///a.msi\", sha256: " + hash + "}]", "sem host"},
		{"url localhost", "apps:\n  g:\n    name: G\n    category: dev\n    windows: [{provider: url, url: \"https://localhost/a.msi\", sha256: " + hash + "}]", "localhost"},
		{"url IP literal", "apps:\n  g:\n    name: G\n    category: dev\n    windows: [{provider: url, url: \"https://10.0.0.1/a.msi\", sha256: " + hash + "}]", "IP literal"},
		{"url IPv6 literal", "apps:\n  g:\n    name: G\n    category: dev\n    windows: [{provider: url, url: \"https://[::1]/a.msi\", sha256: " + hash + "}]", "IP literal"},
		{"url porta", "apps:\n  g:\n    name: G\n    category: dev\n    windows: [{provider: url, url: \"https://x.com:8443/a.msi\", sha256: " + hash + "}]", "porta 8443"},
		{"url com espaço", "apps:\n  g:\n    name: G\n    category: dev\n    windows: [{provider: url, url: \"https://x.com/a b.msi\", sha256: " + hash + "}]", "espaço"},
		{"url com controle", "apps:\n  g:\n    name: G\n    category: dev\n    windows: [{provider: url, url: \"https://x.com/a\\u0001.msi\", sha256: " + hash + "}]", "controle"},
		{"url template no host", "apps:\n  g:\n    name: G\n    category: dev\n    versions: [\"1\"]\n    windows: [{provider: url, url: \"https://{{version}}.x.com/a.msi\", sha256: " + hash + "}]", "template no host"},
		{"url fragmento", "apps:\n  g:\n    name: G\n    category: dev\n    windows: [{provider: url, url: \"https://x.com/a.msi#x\", sha256: " + hash + "}]", "fragmento"},
		{"args com número", "apps:\n  g:\n    name: G\n    category: dev\n    windows: [{provider: url, url: \"https://x.com/a.msi\", sha256: " + hash + ", args: [1]}]", "args deve ser um texto"},
		{"args com mapa", "apps:\n  g:\n    name: G\n    category: dev\n    windows: [{provider: url, url: \"https://x.com/a.msi\", sha256: " + hash + ", args: [{a: b}]}]", "args deve ser um texto"},
		{"args com template desconhecido", "apps:\n  g:\n    name: G\n    category: dev\n    windows: [{provider: url, url: \"https://x.com/a.msi\", sha256: " + hash + ", args: [\"{{foo}}\"]}]", `variável desconhecida "foo"`},
		{"args com versão sem lista", "apps:\n  g:\n    name: G\n    category: dev\n    windows: [{provider: url, url: \"https://x.com/a.msi\", sha256: " + hash + ", args: [\"/v{{version}}\"]}]", `"versions" precisa ser uma lista`},
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
	base := mustParse(t, "b", ("apps:\n  a:\n    name: A1\n    category: dev\n    any: [{provider: github-release, repo: o/a, asset: a.zip, args: [\"-y\"]}]\n  b:\n    name: B\n    category: dev\n    any: [{provider: apt, package: b}]\n"))
	ext := mustParse(t, "e", ("apps:\n  a:\n    name: A2\n    category: dev\n    any: [{provider: apt, package: a}]\n  c:\n    name: C\n    category: dev\n    any: [{provider: apt, package: c}]\n"))

	m, replaced := Merge(base, ext, false)
	if strings.Join(m.Order, ",") != "a,b,c" || m.Apps["a"].Name != "A2" {
		t.Fatalf("merge: %v %s", m.Order, m.Apps["a"].Name)
	}
	if strings.Join(replaced, ",") != "a" {
		t.Fatalf("deveria avisar que a foi substituído: %v", replaced)
	}
	r, replaced := Merge(base, ext, true)
	if strings.Join(r.Order, ",") != "a,c" || len(r.Apps) != 2 || len(replaced) != 0 {
		t.Fatalf("replace: %v %v", r.Order, replaced)
	}
	if base.Apps["a"].Name != "A1" {
		t.Fatal("merge não pode alterar a base")
	}
	if got := len(m.List()); got != 3 {
		t.Fatalf("List: %d", got)
	}
}

func TestMergeCopiaProfundaENil(t *testing.T) {
	base := mustParse(t, "b", ("apps:\n  b:\n    name: B\n    category: dev\n    versions: [\"1\"]\n    any: [{provider: github-release, repo: o/b, asset: b.zip, args: [\"-y\"]}]\n"))
	m, _ := Merge(base, nil, false)
	m.Apps["b"].Installers[KeyAny][0].Args[0] = "MUDOU"
	m.Apps["b"].Installers[KeyAny][0].Repo = "MUDOU"
	m.Apps["b"].Versions.List[0] = "MUDOU"
	m.Apps["b"].Installers[KeyAny] = nil
	got := base.Apps["b"]
	if got.Installers[KeyAny][0].Args[0] != "-y" || got.Installers[KeyAny][0].Repo != "o/b" || got.Versions.List[0] != "1" {
		t.Fatalf("a cópia compartilhou estado com a base: %+v", got)
	}
	if m, rep := Merge(nil, base, false); len(m.Apps) != 1 || len(rep) != 0 {
		t.Fatalf("base nil: %+v %v", m, rep)
	}
	if m, _ := Merge(nil, nil, true); len(m.Apps) != 0 {
		t.Fatalf("ambos nil: %+v", m)
	}
}

func TestVersionVars(t *testing.T) {
	c, _ := Parse("c", []byte(valid))
	py, _ := c.Get("python")
	vars, err := py.VersionVars("")
	if err != nil || vars["version"] != "3.13" || vars["major"] != "3" || vars["major_minor"] != "3.13" {
		t.Fatalf("padrão: %v %v", vars, err)
	}
	if vars, err = py.VersionVars("3.12"); err != nil || vars["major_minor"] != "3.12" {
		t.Fatalf("%v %v", vars, err)
	}
	for _, bad := range []string{"3.11", "3.13/../x", "../../etc", "3.13 ", "{{os}}", "3.13;rm"} {
		if _, err := py.VersionVars(bad); err == nil {
			t.Errorf("%q deveria ser recusada (fora da lista)", bad)
		}
	}
	git, _ := c.Get("git") // versions: true — só formato
	if vars, err = git.VersionVars("2.45.1"); err != nil || vars["version"] != "2.45.1" {
		t.Fatalf("%v %v", vars, err)
	}
	if _, err = git.VersionVars("2.45/../x"); err == nil {
		t.Fatal("versão hostil com versions: true deveria falhar")
	}
	tool, _ := c.Get("tool") // sem versions
	if _, err = tool.VersionVars("1.0"); err == nil {
		t.Fatal("app sem versions não aceita versão")
	}
	if vars, err = tool.VersionVars(""); err != nil || len(vars) != 0 {
		t.Fatalf("%v %v", vars, err)
	}
}

func TestLimites(t *testing.T) {
	if _, err := Parse("g.yaml", []byte("apps:\n"+strings.Repeat("#"+strings.Repeat("x", 1000)+"\n", 1100))); err == nil || !strings.Contains(err.Error(), "maior que 1 MiB") {
		t.Fatalf("arquivo grande: %v", err)
	}
	var b strings.Builder
	b.WriteString("apps:\n")
	for i := range MaxApps + 1 {
		fmt.Fprintf(&b, "  a%d: {}\n", i)
	}
	if _, err := Parse("m.yaml", []byte(b.String())); err == nil || !strings.Contains(err.Error(), "apps demais") {
		t.Fatalf("muitos apps: %v", err)
	}
}

func TestCheckURLAceitaCasosLegitimos(t *testing.T) {
	for _, u := range []string{
		"https://exemplo.com/app-1.2.3-x64.msi",
		"https://dl.exemplo.com:443/a/{{version}}/app-{{os}}-{{arch}}.zip",
		"https://xn--exemplo-9ta.com/a?v={{version}}",
	} {
		if err := CheckURL(u, true); err != nil {
			t.Errorf("%q: %v", u, err)
		}
	}
	// depois da expansão, sobra de "{{" é erro
	if err := CheckURL("https://exemplo.com/{{version}}", false); err == nil {
		t.Error("template não expandido deveria falhar")
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
	big := filepath.Join(dir, "grande.yaml")
	_ = os.WriteFile(big, []byte("apps:\n"+strings.Repeat("#"+strings.Repeat("x", 1000)+"\n", 1100)), 0o600)
	if _, err := LoadFile(big); err == nil || !strings.Contains(err.Error(), "maior que 1 MiB") {
		t.Fatalf("teto: %v", err)
	}
	if _, err := LoadFile(dir); err == nil || !strings.Contains(err.Error(), "regular") {
		t.Fatalf("diretório deveria ser recusado: %v", err)
	}
}

func mustParse(t *testing.T, name, src string) *Catalog {
	t.Helper()
	c, err := Parse(name, []byte(src))
	if err != nil {
		t.Fatalf("catálogo de teste inválido: %v", err)
	}
	return c
}

// Caractere de controle em qualquer texto do catálogo é erro: o `list` (e a TUI) imprime name e versões, e
// ESC/OSC/BEL/quebra de linha permitiriam reescrever o terminal do usuário.
func TestParseRecusaCaractereDeControle(t *testing.T) {
	const tail = "\n    category: dev\n    any: [{provider: apt, package: g}]"
	cases := []struct{ name, yaml string }{
		{"ESC no name", "apps:\n  g:\n    name: \"Git\\e[2J\"" + tail},
		{"OSC e BEL no name", "apps:\n  g:\n    name: \"Git\\e]0;pwned\\a\"" + tail},
		{"BEL no name", "apps:\n  g:\n    name: \"Git\\a\"" + tail},
		{"quebra de linha no name", "apps:\n  g:\n    name: \"Git\\nFALSO  x\"" + tail},
		{"CR no name", "apps:\n  g:\n    name: \"Git\\rFALSO\"" + tail},
		{"DEL no name", "apps:\n  g:\n    name: \"Git\\x7f\"" + tail},
		{"C1 (CSI 8 bits) no name", "apps:\n  g:\n    name: \"Git\\u009b2J\"" + tail},
		{"tab no name", "apps:\n  g:\n    name: \"Git\\tx\"" + tail},
		{"ESC em item de versions", "apps:\n  g:\n    name: G\n    versions: [\"1\", \"2\\e[2J\"]" + tail},
		{"quebra de linha em item de versions", "apps:\n  g:\n    name: G\n    versions: [\"1\\n2\"]" + tail},
		{"ESC na categoria", "apps:\n  g:\n    name: G\n    category: \"dev\\e\"\n    any: [{provider: apt, package: g}]"},
		{"ESC no id do instalador", "apps:\n  g:\n    name: G\n    category: dev\n    any: [{provider: apt, package: \"g\\e]0;x\\a\"}]"},
		{"ESC em args", "apps:\n  g:\n    name: G\n    category: dev\n    any: [{provider: url, url: \"https://x.com/a.msi\", sha256: " + strings.Repeat("a", 64) + ", args: [\"/q\\e[2J\"]}]"},
		{"ESC em chave desconhecida", "apps:\n  g:\n    name: G\n    \"x\\e\": 1" + tail},
		{"ESC no id do app", "apps:\n  \"g\\e\":\n    name: G" + tail},
	}
	for _, c := range cases {
		_, err := Parse("catalog.yaml", []byte(c.yaml))
		if err == nil {
			t.Errorf("%s: esperava erro", c.name)
			continue
		}
		if !strings.Contains(err.Error(), "caractere de controle") {
			t.Errorf("%s: erro sem \"caractere de controle\": %v", c.name, err)
		}
		// a mensagem de erro também vai ao terminal: não pode carregar o byte cru.
		for _, r := range err.Error() {
			if r < 0x20 && r != '\n' || r == 0x7f || (r >= 0x80 && r <= 0x9f) {
				t.Errorf("%s: a mensagem de erro contém o controle %U cru", c.name, r)
			}
		}
	}
}

func TestParseVersionsRecusaValorHostil(t *testing.T) {
	for _, v := range []string{"-rf", "--force", "1/../x", "a b"} {
		src := "apps:\n  g:\n    name: G\n    category: dev\n    versions: [\"" + v + "\"]\n    any: [{provider: apt, package: g}]"
		if _, err := Parse("catalog.yaml", []byte(src)); err == nil {
			t.Errorf("versão %q deveria ser recusada na carga", v)
		}
	}
}

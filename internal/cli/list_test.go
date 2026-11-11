package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const embedded = `
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
    linux:   [{ provider: apt, package: "python{{major_minor}}" }]
  firefox:
    name: Firefox
    category: navegadores
    any: [{ provider: flatpak, id: org.mozilla.firefox }]
`

func listCmd(t *testing.T, goos string, args ...string) (string, error) {
	t.Helper()
	var out, errOut bytes.Buffer
	cmd := NewRootCmdWith(&out, &errOut, Deps{GOOS: goos, EmbeddedYML: []byte(embedded)})
	cmd.SetArgs(append([]string{"list"}, args...))
	err := cmd.Execute()
	return out.String(), err
}

func TestListTabelaLinux(t *testing.T) {
	out, err := listCmd(t, "linux")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"ID", "GERENCIADORES", "git", "apt, pacman", "3.13, 3.12", "flatpak"} {
		if !strings.Contains(out, want) {
			t.Errorf("saída sem %q:\n%s", want, out)
		}
	}
	if !strings.Contains(out, "sim") {
		t.Errorf("git tem versions: true (sim):\n%s", out)
	}
}

func TestListSemInstaladorNoSO(t *testing.T) {
	out, _ := listCmd(t, "darwin")
	// python não tem macos nem any
	var line string
	for _, l := range strings.Split(out, "\n") {
		if strings.HasPrefix(l, "python") {
			line = l
		}
	}
	if !strings.Contains(line, "(nenhum neste sistema)") {
		t.Fatalf("python no macOS: %q", line)
	}
}

func TestListFlagOSSobrepoe(t *testing.T) {
	out, err := listCmd(t, "linux", "--os", "windows")
	if err != nil || !strings.Contains(out, "winget") || strings.Contains(out, "apt") {
		t.Fatalf("%v\n%s", err, out)
	}
}

func TestListCategoria(t *testing.T) {
	out, err := listCmd(t, "linux", "--category", "navegadores")
	if err != nil || !strings.Contains(out, "firefox") || strings.Contains(out, "git") {
		t.Fatalf("%v\n%s", err, out)
	}
}

func TestListJSON(t *testing.T) {
	out, err := listCmd(t, "linux", "--json", "--category", "dev")
	if err != nil {
		t.Fatal(err)
	}
	var items []listItem
	if err := json.Unmarshal([]byte(out), &items); err != nil {
		t.Fatalf("JSON inválido: %v\n%s", err, out)
	}
	if len(items) != 2 || items[0].ID != "git" || !items[0].VersionsQuery ||
		strings.Join(items[0].Providers, ",") != "apt,pacman" || len(items[1].Versions) != 2 {
		t.Fatalf("%+v", items)
	}
}

func TestListJSONVazioEArrayVazio(t *testing.T) {
	out, err := listCmd(t, "linux", "--json", "--category", "jogos")
	if err != nil || strings.TrimSpace(out) != "[]" {
		t.Fatalf("%v %q", err, out)
	}
	out, _ = listCmd(t, "linux", "--category", "jogos")
	if !strings.Contains(out, "Nenhum app") {
		t.Fatalf("%q", out)
	}
}

func TestListCatalogoLocalMesclaEReplace(t *testing.T) {
	p := filepath.Join(t.TempDir(), "extra.yaml")
	src := "apps:\n  git:\n    name: Git Custom\n    category: dev\n    any: [{ provider: apt, package: git }]\n  novo:\n    name: Novo\n    category: utilitarios\n    any: [{ provider: apt, package: novo }]\n"
	if err := os.WriteFile(p, []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	out, err := listCmd(t, "linux", "--catalog", p)
	if err != nil || !strings.Contains(out, "Git Custom") || !strings.Contains(out, "novo") || !strings.Contains(out, "firefox") {
		t.Fatalf("merge: %v\n%s", err, out)
	}
	out, err = listCmd(t, "linux", "--catalog", p, "--catalog-replace")
	if err != nil || strings.Contains(out, "firefox") || !strings.Contains(out, "novo") {
		t.Fatalf("replace: %v\n%s", err, out)
	}
}

func TestListCatalogoLocalInvalidoCitaArquivoELinha(t *testing.T) {
	p := filepath.Join(t.TempDir(), "ruim.yaml")
	_ = os.WriteFile(p, []byte("apps:\n  x:\n    name: X\n    category: dev\n    pakage: y\n    any: [{ provider: apt, package: x }]\n"), 0o600)
	_, err := listCmd(t, "linux", "--catalog", p)
	if err == nil || !strings.Contains(err.Error(), "ruim.yaml:5:5") {
		t.Fatalf("%v", err)
	}
}

func TestListErrosDeFlags(t *testing.T) {
	cases := [][]string{
		{"--catalog", "https://exemplo.com/c.yaml"},
		{"--catalog", "https://exemplo.com/c.yaml", "--catalog-sha256", strings.Repeat("a", 64)},
		{"--catalog-sha256", strings.Repeat("a", 64)},
		{"--catalog-replace"},
		{"--os", "beos"},
		{"--category", "xyz"},
		{"extra"},
	}
	for _, args := range cases {
		if _, err := listCmd(t, "linux", args...); err == nil {
			t.Errorf("%v deveria falhar", args)
		}
	}
}

func TestListCatalogoEmbutidoRealFunciona(t *testing.T) {
	var out, errOut bytes.Buffer
	cmd := NewRootCmd(&out, &errOut)
	cmd.SetArgs([]string{"list", "--os", "linux"})
	if err := cmd.Execute(); err != nil || !strings.Contains(out.String(), "git") {
		t.Fatalf("%v\n%s", err, out.String())
	}
}

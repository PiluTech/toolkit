package recipes

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/PiluVitu/toolkit/internal/catalog"
	"github.com/PiluVitu/toolkit/internal/platform"
)

const okRecipe = `id: wsl-dev
name: Ambiente WSL
description: demo
os: windows
requires_admin: true
steps:
  - id: enable-features
    check: { powershell: "(Get-WindowsOptionalFeature -Online -FeatureName VirtualMachinePlatform).State -eq 'Enabled'" }
    run:
      powershell: >-
        dism /online /enable-feature /featurename:VirtualMachinePlatform /all /norestart
      success_codes: [3010]
    reboot: required
    timeout: 30m
  - id: setup-inside
    run: { wsl: { distro: Ubuntu, user: dev, bash: "echo oi" } }
  - id: apps
    install: [vscode, "python@3.12"]
    reboot: if-needed
`

func TestParseOK(t *testing.T) {
	r, err := Parse("wsl-dev.yaml", []byte(okRecipe))
	if err != nil {
		t.Fatal(err)
	}
	if r.ID != "wsl-dev" || r.OS != platform.Windows || r.Kind != KindSetup || !r.RequiresAdmin || len(r.Steps) != 3 {
		t.Fatalf("receita: %+v", r)
	}
	s := r.Steps[0]
	if s.Reboot != RebootRequired || s.Timeout != 30*time.Minute || !s.Run.OKExit(3010) || s.Run.OKExit(1) || !s.Run.OKExit(0) {
		t.Fatalf("etapa 0: %+v", s)
	}
	if w := r.Steps[1].Run.WSL; w == nil || w.Distro != "Ubuntu" || w.User != "dev" || w.Bash != "echo oi" {
		t.Fatalf("wsl: %+v", r.Steps[1].Run)
	}
	if got := r.Steps[2].Install; len(got) != 2 || got[1] != "python@3.12" || r.Steps[2].Reboot != RebootIfNeeded {
		t.Fatalf("install: %+v", r.Steps[2])
	}
	if len(r.Hash) != 64 {
		t.Fatalf("hash: %q", r.Hash)
	}
	r2, _ := Parse("wsl-dev.yaml", []byte(okRecipe+"\n"))
	if r2.Hash == r.Hash {
		t.Fatal("hash deve mudar com o conteúdo")
	}
}

func TestParseDefaults(t *testing.T) {
	r, err := Parse("x.txt", []byte("id: x\nname: X\nos: linux\nsteps:\n  - id: a\n    run: { sh: 'true' }\n"))
	if err != nil {
		t.Fatal(err)
	}
	if r.Kind != KindSetup || r.RequiresAdmin || r.Steps[0].Reboot != RebootNever || r.OS != platform.Linux {
		t.Fatalf("%+v", r)
	}
	mac, err := Parse("x.txt", []byte("id: x\nname: X\nos: macos\nsteps:\n  - id: a\n    run: { bash: 'true' }\n"))
	if err != nil || mac.OS != platform.Darwin {
		t.Fatalf("macos: %v %+v", err, mac)
	}
}

func TestParseMultilineScript(t *testing.T) {
	src := "id: x\nname: X\nos: linux\nsteps:\n  - id: a\n    run:\n      sh: |\n        set -e\n        echo\toi\n"
	r, err := Parse("x.yaml", []byte(src))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(r.Steps[0].Run.Sh, "\n") {
		t.Fatalf("script: %q", r.Steps[0].Run.Sh)
	}
}

func TestParseErrors(t *testing.T) {
	base := "id: x\nname: X\nos: linux\nsteps:\n  - id: a\n    run: { sh: 'true' }\n"
	cases := []struct {
		name, src, file, want string
	}{
		{"vazio", "", "x.yaml", "arquivo vazio"},
		{"topo", "- a\n", "x.yaml", "o topo do arquivo deve ser um mapa"},
		{"yaml quebrado", "id: [", "x.yaml", "YAML inválido"},
		{"chave desconhecida", base + "foo: 1\n", "x.yaml", `chave desconhecida "foo"`},
		{"chave repetida", "id: x\nid: y\nname: X\nos: linux\nsteps:\n  - id: a\n    run: { sh: t }\n", "x.yaml", `chave "id" repetida`},
		{"alias", "id: x\nname: &n X\nos: linux\nsteps:\n  - id: a\n    run: { sh: t }\n", "x.yaml", "âncoras"},
		{"id difere do arquivo", base, "outra.yaml", "deve ser igual ao nome do arquivo"},
		{"id invalido", strings.Replace(base, "id: x", "id: X_", 1), "z.txt", "id \"X_\" inválido"},
		{"sem name", "id: x\nos: linux\nsteps:\n  - id: a\n    run: { sh: t }\n", "x.yaml", `falta "name"`},
		{"sem os", "id: x\nname: X\nsteps:\n  - id: a\n    run: { sh: t }\n", "x.yaml", `falta "os"`},
		{"os ruim", strings.Replace(base, "linux", "plan9", 1), "x.yaml", `os "plan9" desconhecido`},
		{"kind ruim", base + "kind: foo\n", "x.yaml", `kind "foo" desconhecido`},
		{"admin nao bool", base + "requires_admin: sim\n", "x.yaml", "true ou false"},
		{"sem steps", "id: x\nname: X\nos: linux\n", "x.yaml", `falta "steps"`},
		{"steps vazio", "id: x\nname: X\nos: linux\nsteps: []\n", "x.yaml", "ao menos uma etapa"},
		{"etapa nao mapa", "id: x\nname: X\nos: linux\nsteps:\n  - a\n", "x.yaml", "cada etapa deve ser um mapa"},
		{"etapa sem id", "id: x\nname: X\nos: linux\nsteps:\n  - run: { sh: t }\n", "x.yaml", `etapa sem "id"`},
		{"etapa duplicada", base + "  - id: a\n    run: { sh: t }\n", "x.yaml", `etapa "a" duplicada`},
		{"run e install", "id: x\nname: X\nos: linux\nsteps:\n  - id: a\n    run: { sh: t }\n    install: [git]\n", "x.yaml", `exatamente um entre "run" e "install"`},
		{"nem run nem install", "id: x\nname: X\nos: linux\nsteps:\n  - id: a\n    check: { sh: t }\n", "x.yaml", `exatamente um entre "run" e "install"`},
		{"etapa chave desconhecida", "id: x\nname: X\nos: linux\nsteps:\n  - id: a\n    run: { sh: t }\n    foo: 1\n", "x.yaml", `etapa: chave desconhecida "foo"`},
		{"reboot ruim", "id: x\nname: X\nos: linux\nsteps:\n  - id: a\n    run: { sh: t }\n    reboot: sempre\n", "x.yaml", `reboot "sempre" desconhecido`},
		{"timeout ruim", "id: x\nname: X\nos: linux\nsteps:\n  - id: a\n    run: { sh: t }\n    timeout: muito\n", "x.yaml", `timeout "muito" inválido`},
		{"timeout enorme", "id: x\nname: X\nos: linux\nsteps:\n  - id: a\n    run: { sh: t }\n    timeout: 48h\n", "x.yaml", "timeout deve ficar entre"},
		{"duas acoes", "id: x\nname: X\nos: linux\nsteps:\n  - id: a\n    run: { sh: t, bash: t }\n", "x.yaml", "exatamente uma ação"},
		{"zero acoes", "id: x\nname: X\nos: linux\nsteps:\n  - id: a\n    run: { success_codes: [3] }\n", "x.yaml", "exatamente uma ação"},
		{"powershell em linux", "id: x\nname: X\nos: linux\nsteps:\n  - id: a\n    run: { powershell: t }\n", "x.yaml", `"powershell" não vale para receita de linux`},
		{"sh em windows", "id: x\nname: X\nos: windows\nsteps:\n  - id: a\n    run: { sh: t }\n", "x.yaml", `"sh" não vale para receita de windows`},
		{"wsl em macos", "id: x\nname: X\nos: macos\nsteps:\n  - id: a\n    run: { wsl: { distro: U, bash: t } }\n", "x.yaml", `"wsl" não vale para receita de macos`},
		{"acao chave desconhecida", "id: x\nname: X\nos: linux\nsteps:\n  - id: a\n    run: { sh: t, cmd: t }\n", "x.yaml", `run: chave desconhecida "cmd"`},
		{"wsl sem bash", "id: x\nname: X\nos: windows\nsteps:\n  - id: a\n    run: { wsl: { distro: U } }\n", "x.yaml", `wsl exige "distro" e "bash"`},
		{"wsl distro ruim", "id: x\nname: X\nos: windows\nsteps:\n  - id: a\n    run: { wsl: { distro: '-x', bash: t } }\n", "x.yaml", "distro \"-x\" inválida"},
		{"wsl user ruim", "id: x\nname: X\nos: windows\nsteps:\n  - id: a\n    run: { wsl: { distro: U, user: 'a b', bash: t } }\n", "x.yaml", "user \"a b\" inválido"},
		{"codigo ruim", "id: x\nname: X\nos: linux\nsteps:\n  - id: a\n    run: { sh: t, success_codes: [0] }\n", "x.yaml", `exit code "0" inválido`},
		{"codigo texto", "id: x\nname: X\nos: linux\nsteps:\n  - id: a\n    run: { sh: t, success_codes: [a] }\n", "x.yaml", `exit code "a" inválido`},
		{"install ruim", "id: x\nname: X\nos: linux\nsteps:\n  - id: a\n    install: ['-rf']\n", "x.yaml", `app "-rf" inválido`},
		{"install vazio", "id: x\nname: X\nos: linux\nsteps:\n  - id: a\n    install: []\n", "x.yaml", "lista de apps"},
		{"diag com install", "id: x\nname: X\nos: linux\nkind: diagnostic\nsteps:\n  - id: a\n    install: [git]\n", "x.yaml", "diagnóstico não instala"},
		{"diag com reboot", "id: x\nname: X\nos: linux\nkind: diagnostic\nsteps:\n  - id: a\n    run: { sh: t }\n    reboot: required\n", "x.yaml", "diagnóstico não reinicia"},
		{"controle no name", "id: x\nname: \"a\\e[31m\"\nos: linux\nsteps:\n  - id: a\n    run: { sh: t }\n", "x.yaml", "U+001B"},
		{"bidi no script", "id: x\nname: X\nos: linux\nsteps:\n  - id: a\n    run: { sh: \"echo \\u202E\" }\n", "x.yaml", "U+202E"},
		{"CR no script", "id: x\nname: X\nos: linux\nsteps:\n  - id: a\n    run: { sh: \"echo\\rls\" }\n", "x.yaml", "U+000D"},
		{"quebra de linha no name", "id: x\nname: \"a\\nb\"\nos: linux\nsteps:\n  - id: a\n    run: { sh: t }\n", "x.yaml", "U+000A"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := Parse(c.file, []byte(c.src))
			if err == nil {
				t.Fatalf("esperava erro com %q", c.want)
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Fatalf("erro %q não contém %q", err, c.want)
			}
		})
	}
}

func TestParseTooBig(t *testing.T) {
	if _, err := Parse("x.yaml", make([]byte, MaxFileSize+1)); err == nil || !strings.Contains(err.Error(), "maior que") {
		t.Fatalf("err=%v", err)
	}
}

func TestParseTooManySteps(t *testing.T) {
	var b strings.Builder
	b.WriteString("id: x\nname: X\nos: linux\nsteps:\n")
	for i := 0; i <= MaxSteps; i++ {
		b.WriteString("  - id: s" + strings.Repeat("a", i%5) + "\n    run: { sh: t }\n")
	}
	if _, err := Parse("x.yaml", []byte(b.String())); err == nil || !strings.Contains(err.Error(), "etapas demais") {
		t.Fatalf("err=%v", err)
	}
}

func TestParseErrorPosition(t *testing.T) {
	_, err := Parse("r.yaml", []byte("id: r\nname: R\nos: linux\nsteps:\n  - id: a\n    run: { sh: t }\n    reboot: nunca\n"))
	var es catalog.Errors
	if !errors.As(err, &es) || len(es) != 1 {
		t.Fatalf("err=%v", err)
	}
	if es[0].File != "r.yaml" || es[0].Line != 7 || es[0].Col == 0 {
		t.Fatalf("posição: %+v", es[0])
	}
	if !strings.HasPrefix(err.Error(), "r.yaml:7:") {
		t.Fatalf("formato: %q", err)
	}
}

func TestParseReportsAllErrors(t *testing.T) {
	_, err := Parse("x.yaml", []byte("id: x\nname: X\nos: linux\nsteps:\n  - id: a\n    run: { sh: t }\n    reboot: z\n  - id: b\n    run: { sh: t }\n    timeout: z\n"))
	var es catalog.Errors
	if !errors.As(err, &es) || len(es) != 2 {
		t.Fatalf("err=%v", err)
	}
}

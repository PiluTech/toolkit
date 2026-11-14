package rede

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/PiluTech/toolkit/internal/diagfmt"
	"github.com/PiluTech/toolkit/internal/exec"
	"github.com/PiluTech/toolkit/internal/platform"
	"github.com/PiluTech/toolkit/internal/recipes"
)

// Deps são as portas do mundo: tudo que os coletores usam passa por aqui, então os testes injetam saídas falsas.
type Deps struct {
	// Runner executa os comandos de leitura (exec.Cmd com ReadOnly=true). O pacote o embrulha com exec.ReadOnlyRunner:
	// um comando que não seja leitura, ou com Elevate, é recusado.
	Runner   exec.Runner
	ReadFile func(path string) ([]byte, error)      // nil = os.ReadFile
	Glob     func(pattern string) ([]string, error) // nil = filepath.Glob
	Now      func() time.Time                       // nil = time.Now
	Lang     diagfmt.Lang
	// Admin: o processo roda como administrador (Windows) ou root (Unix). As leituras continuam sendo de LEITURA (ReadOnly,
	// sem Elevate): o toolkit nunca eleva sozinho, o usuário abre o terminal elevado.
	Admin bool
}

func (d Deps) readFile(p string) ([]byte, error) {
	if d.ReadFile != nil {
		return d.ReadFile(p)
	}
	return os.ReadFile(p)
}

func (d Deps) glob(p string) ([]string, error) {
	if d.Glob != nil {
		return d.Glob(p)
	}
	return filepath.Glob(p)
}

func (d Deps) now() time.Time {
	if d.Now != nil {
		return d.Now()
	}
	return time.Now()
}

// Prazos: cada leitura tem o seu (um comando pendurado não pode gastar o orçamento das outras).
const (
	cmdTimeout = 20 * time.Second
	psTimeout  = time.Minute
)

// run executa um comando de LEITURA e devolve a saída. Erro de execução ou exit != 0 volta como erro: o chamador registra
// em Skipped e segue. Todo Cmd sai com ReadOnly=true e sem Elevate; o ReadOnlyRunner recusa o contrário.
func (d Deps) run(ctx context.Context, c exec.Cmd) (string, error) {
	c.ReadOnly = true
	c.Elevate = false
	if c.Timeout == 0 {
		c.Timeout = cmdTimeout
	}
	res, err := exec.ReadOnlyRunner(d.Runner).Run(ctx, c)
	if err != nil {
		return "", err
	}
	if res.ExitCode != 0 {
		return string(res.Stdout), &exitError{code: res.ExitCode, stderr: string(res.Stderr)}
	}
	return string(res.Stdout), nil
}

type exitError struct {
	code   int
	stderr string
}

func (e *exitError) Error() string {
	if e.stderr != "" {
		return "o comando saiu com código " + itoa(e.code) + ": " + firstLine(e.stderr)
	}
	return "o comando saiu com código " + itoa(e.code)
}

// psCmd monta um Cmd de leitura que roda um script do PowerShell (caminho absoluto, -EncodedCommand), como as receitas.
func psCmd(script string) exec.Cmd {
	c, _ := recipes.BuildCmd(&recipes.Action{PowerShell: script}, platform.Windows, psTimeout, false, true)
	return c
}

// unixCmd é um Cmd de leitura com a localidade fixa (a leitura do texto não pode depender do idioma).
func unixCmd(name string, args ...string) exec.Cmd {
	return exec.Cmd{Name: name, Args: args, ReadOnly: true, Env: []string{"LC_ALL=C"}}
}

// which acha a ferramenta no PATH (caminho absoluto); "" quando não existe.
func (d Deps) which(tool string) string {
	if d.Runner == nil {
		return ""
	}
	p, err := d.Runner.LookPath(tool)
	if err != nil {
		return ""
	}
	return p
}

// Collect lê a rede do SO indicado. Cada área é independente: uma que falha vira Skipped e a coleta segue.
func Collect(ctx context.Context, goos string, d Deps) *Report {
	r := &Report{OS: goos, Generated: d.now(), Admin: d.Admin}
	switch goos {
	case "windows":
		collectWindows(ctx, d, r)
	case "linux":
		collectLinux(ctx, d, r)
	case "darwin":
		collectDarwin(ctx, d, r)
	default:
		r.Skipped = append(r.Skipped, Skipped{Area: "sistema", Reason: diagfmt.T(d.Lang, "sistema operacional sem suporte: "+goos, "unsupported operating system: "+goos, "sistema operativo no compatible: "+goos)})
	}
	return r
}

// safeIface aceita só nomes de interface bem formados antes de usá-los como ARGUMENTO de comando: o nome vem do sistema
// (texto não confiável) e nunca pode virar opção (começar com "-") nem carregar espaço ou controle.
var ifaceRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:@-]{0,31}$`)

func safeIface(s string) bool { return ifaceRe.MatchString(s) }

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexAny(s, "\r\n"); i >= 0 {
		return strings.TrimSpace(s[:i])
	}
	return s
}

// tokenSet separa um texto de segurança ("WPA2_PSK", "WPA2-Personal") em palavras em maiúsculas. Comparar palavras inteiras
// (e não "contém") impede que um valor desconhecido, como "WPA9_XYZ", seja tomado por "WPA".
func tokenSet(s string) map[string]bool {
	set := map[string]bool{}
	for _, t := range strings.FieldsFunc(strings.ToUpper(s), func(r rune) bool { return (r < 'A' || r > 'Z') && (r < '0' || r > '9') }) {
		set[t] = true
	}
	return set
}

package recipes

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"

	"go.yaml.in/yaml/v3"

	"github.com/PiluTech/toolkit/internal/catalog"
	"github.com/PiluTech/toolkit/internal/diagfmt"
	"github.com/PiluTech/toolkit/internal/platform"
)

// Limites da carga.
const (
	MaxFileSize = 1 << 20
	MaxSteps    = 200
	MaxTimeout  = 24 * time.Hour
)

var (
	idRe      = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)
	appRefRe  = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*(@[A-Za-z0-9][A-Za-z0-9._+~-]*)?$`)
	distroRe  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)
	wslUserRe = regexp.MustCompile(`^[a-z_][a-z0-9_-]{0,31}$`)
)

type parser struct {
	file string
	errs catalog.Errors
}

func (p *parser) errAt(n *yaml.Node, format string, a ...any) {
	p.errs = append(p.errs, catalog.Error{File: p.file, Line: n.Line, Col: n.Column, Msg: fmt.Sprintf(format, a...)})
}

func (p *parser) dup(seen map[string]bool, k *yaml.Node, where string) bool {
	if seen[k.Value] {
		p.errAt(k, "%s: chave %q repetida (a última venceria em silêncio)", where, k.Value)
		return true
	}
	seen[k.Value] = true
	return false
}

func (p *parser) rejectAliases(n *yaml.Node) {
	if n.Kind == yaml.AliasNode || n.Anchor != "" {
		p.errAt(n, "âncoras e aliases YAML (&x, *x) não são aceitos em receitas")
		return
	}
	if n.Tag == "!!merge" {
		p.errAt(n, "a chave de merge (<<) não é aceita em receitas")
	}
	for _, c := range n.Content {
		p.rejectAliases(c)
	}
}

// unsafeRune: controle (Cc), formatação (Cf: bidi, zero width), Zl e Zp. Cf num script é o ataque
// "Trojan Source": o texto exibido difere do executado.
func unsafeRune(r rune) bool {
	return unicode.IsControl(r) || unicode.In(r, unicode.Cf, unicode.Zl, unicode.Zp)
}

// scriptKeys são as chaves cujo valor é um script: só nelas \n e \t são aceitos.
var scriptKeys = []string{"powershell", "sh", "bash"}

func (p *parser) checkText(n *yaml.Node, script bool) {
	for _, r := range n.Value {
		if script && (r == '\n' || r == '\t') {
			continue
		}
		if unsafeRune(r) {
			p.errAt(n, "caractere de controle ou de formatação U+%04X não é aceito (texto sem ESC, CR, tab, bidi, U+2028 etc.; quebra de linha e tab só em scripts)", r)
			return
		}
	}
}

// rejectControl percorre o documento: chaves e valores nunca aceitam controle; o valor das chaves de
// script (powershell, sh, bash, inclusive wsl.bash) aceita \n e \t.
func (p *parser) rejectControl(n *yaml.Node, script bool) {
	switch n.Kind {
	case yaml.ScalarNode:
		p.checkText(n, script)
	case yaml.MappingNode:
		for i := 0; i+1 < len(n.Content); i += 2 {
			k, v := n.Content[i], n.Content[i+1]
			p.checkText(k, false)
			p.rejectControl(v, slices.Contains(scriptKeys, k.Value))
		}
	default:
		for _, c := range n.Content {
			p.rejectControl(c, false)
		}
	}
}

// Parse lê e valida uma receita. file só aparece nas mensagens de erro; se terminar em .yaml, o id da
// receita tem de ser igual ao nome do arquivo.
func Parse(file string, data []byte) (*Recipe, error) {
	if len(data) > MaxFileSize {
		return nil, catalog.Errors{{File: file, Msg: fmt.Sprintf("arquivo maior que %d MiB", MaxFileSize>>20)}}
	}
	var doc yaml.Node
	if err := yaml.NewDecoder(bytes.NewReader(data)).Decode(&doc); err != nil {
		if errors.Is(err, io.EOF) {
			return nil, catalog.Errors{{File: file, Msg: "arquivo vazio"}}
		}
		return nil, catalog.Errors{{File: file, Msg: "YAML inválido: " + err.Error()}}
	}
	p := &parser{file: file}
	p.rejectAliases(&doc)
	p.rejectControl(&doc, false)
	if len(p.errs) > 0 {
		return nil, p.errs
	}
	root := &doc
	if root.Kind == yaml.DocumentNode && len(root.Content) == 1 {
		root = root.Content[0]
	}
	if root.Kind != yaml.MappingNode {
		p.errAt(root, "o topo do arquivo deve ser um mapa")
		return nil, p.errs
	}
	sum := sha256.Sum256(data)
	r := &Recipe{Kind: KindSetup, Hash: hex.EncodeToString(sum[:])}
	var stepsNode *yaml.Node
	seen := map[string]bool{}
	var osSeen bool
	for i := 0; i+1 < len(root.Content); i += 2 {
		k, v := root.Content[i], root.Content[i+1]
		if p.dup(seen, k, "receita") {
			continue
		}
		switch k.Value {
		case "id":
			r.ID = p.text(v, "id")
			if r.ID != "" && !idRe.MatchString(r.ID) {
				p.errAt(v, "id %q inválido (use minúsculas, números e hífen)", r.ID)
			}
		case "name":
			r.Name = strings.TrimSpace(p.text(v, "name"))
		case "description":
			r.Description = strings.TrimSpace(p.text(v, "description"))
		case "os":
			osSeen = true
			switch p.text(v, "os") {
			case "windows":
				r.OS = platform.Windows
			case "linux":
				r.OS = platform.Linux
			case "macos":
				r.OS = platform.Darwin
			default:
				p.errAt(v, "os %q desconhecido (use: windows, linux, macos)", v.Value)
			}
		case "kind":
			switch Kind(p.text(v, "kind")) {
			case KindSetup:
				r.Kind = KindSetup
			case KindDiagnostic:
				r.Kind = KindDiagnostic
			default:
				p.errAt(v, "kind %q desconhecido (use: setup, diagnostic)", v.Value)
			}
		case "requires_admin":
			r.RequiresAdmin = p.boolean(v, "requires_admin")
		case "steps":
			stepsNode = v
		default:
			p.errAt(k, "chave desconhecida %q (esperado: id, name, description, os, kind, requires_admin, steps)", k.Value)
		}
	}
	if r.ID == "" {
		p.errAt(root, "falta \"id\"")
	} else if base := filepath.Base(file); strings.HasSuffix(base, ".yaml") && strings.TrimSuffix(base, ".yaml") != r.ID {
		p.errAt(root, "id %q deve ser igual ao nome do arquivo (%s)", r.ID, base)
	}
	if r.Name == "" {
		p.errAt(root, "falta \"name\"")
	}
	if !osSeen {
		p.errAt(root, "falta \"os\"")
	}
	p.parseSteps(r, stepsNode, root)
	if len(p.errs) > 0 {
		return nil, p.errs
	}
	return r, nil
}

func (p *parser) text(v *yaml.Node, name string) string {
	if v.Kind != yaml.ScalarNode || v.Tag != "!!str" || strings.TrimSpace(v.Value) == "" {
		p.errAt(v, "%q deve ser um texto não vazio", name)
		return ""
	}
	return v.Value
}

func (p *parser) boolean(v *yaml.Node, name string) bool {
	if v.Kind != yaml.ScalarNode || v.Tag != "!!bool" {
		p.errAt(v, "%q deve ser true ou false", name)
		return false
	}
	return v.Value == "true"
}

func (p *parser) parseSteps(r *Recipe, n, root *yaml.Node) {
	if n == nil {
		p.errAt(root, "falta \"steps\"")
		return
	}
	if n.Kind != yaml.SequenceNode || len(n.Content) == 0 {
		p.errAt(n, "\"steps\" deve ser uma lista com ao menos uma etapa")
		return
	}
	if len(n.Content) > MaxSteps {
		p.errAt(n, "etapas demais: %d (o limite é %d)", len(n.Content), MaxSteps)
		return
	}
	ids := map[string]bool{}
	for _, sn := range n.Content {
		st, ok := p.parseStep(r, sn)
		if !ok {
			continue
		}
		if ids[st.ID] {
			p.errAt(sn, "etapa %q duplicada", st.ID)
			continue
		}
		ids[st.ID] = true
		r.Steps = append(r.Steps, st)
	}
}

func (p *parser) parseStep(r *Recipe, n *yaml.Node) (Step, bool) {
	start := len(p.errs)
	st := Step{Reboot: RebootNever, Line: n.Line}
	if n.Kind != yaml.MappingNode {
		p.errAt(n, "cada etapa deve ser um mapa")
		return st, false
	}
	var runNode, installNode, rebootNode *yaml.Node
	seen := map[string]bool{}
	for i := 0; i+1 < len(n.Content); i += 2 {
		k, v := n.Content[i], n.Content[i+1]
		if p.dup(seen, k, "etapa") {
			continue
		}
		switch k.Value {
		case "id":
			st.ID = p.text(v, "id")
			if st.ID != "" && !idRe.MatchString(st.ID) {
				p.errAt(v, "id de etapa %q inválido (use minúsculas, números e hífen)", st.ID)
			}
		case "name":
			st.Name = strings.TrimSpace(p.text(v, "name"))
		case "check":
			st.Check = p.parseAction(r, v, "check")
		case "run":
			runNode = v
			st.Run = p.parseAction(r, v, "run")
		case "install":
			installNode = v
			st.Install = p.parseInstall(v)
		case "reboot":
			rebootNode = v
			switch RebootPolicy(p.text(v, "reboot")) {
			case RebootNever, RebootRequired, RebootIfNeeded:
				st.Reboot = RebootPolicy(v.Value)
			default:
				p.errAt(v, "reboot %q desconhecido (use: never, required, if-needed)", v.Value)
			}
		case "render":
			st.Render = p.text(v, "render")
			if st.Render != "" && !diagfmt.Known(st.Render) {
				p.errAt(v, "render %q desconhecido (use: %s)", st.Render, strings.Join(diagfmt.Names(), ", "))
			}
		case "timeout":
			d, err := time.ParseDuration(p.text(v, "timeout"))
			switch {
			case err != nil:
				p.errAt(v, "timeout %q inválido (exemplo: 30m)", v.Value)
			case d <= 0 || d > MaxTimeout:
				p.errAt(v, "timeout deve ficar entre 1s e %s", MaxTimeout)
			default:
				st.Timeout = d
			}
		default:
			p.errAt(k, "etapa: chave desconhecida %q (esperado: id, name, check, run, install, render, reboot, timeout)", k.Value)
		}
	}
	if st.ID == "" {
		p.errAt(n, "etapa sem \"id\"")
	}
	if (runNode == nil) == (installNode == nil) {
		p.errAt(n, "etapa %q: exige exatamente um entre \"run\" e \"install\"", st.ID)
	}
	if st.Render != "" && (runNode == nil || r.Kind != KindDiagnostic) {
		p.errAt(n, "etapa %q: \"render\" só vale em etapa \"run\" de receita de diagnóstico", st.ID)
	}
	if r.Kind == KindDiagnostic {
		if installNode != nil {
			p.errAt(installNode, "etapa %q: receita de diagnóstico não instala apps", st.ID)
		}
		if rebootNode != nil && st.Reboot != RebootNever {
			p.errAt(rebootNode, "etapa %q: receita de diagnóstico não reinicia (reboot: never)", st.ID)
		}
	}
	return st, len(p.errs) == start
}

func (p *parser) parseInstall(v *yaml.Node) []string {
	if v.Kind != yaml.SequenceNode || len(v.Content) == 0 {
		p.errAt(v, "\"install\" deve ser uma lista de apps do catálogo")
		return nil
	}
	var out []string
	for _, it := range v.Content {
		if it.Kind != yaml.ScalarNode || !appRefRe.MatchString(it.Value) {
			p.errAt(it, "app %q inválido em install (use id do catálogo, ou id@versão)", it.Value)
			continue
		}
		out = append(out, it.Value)
	}
	return out
}

func (p *parser) parseAction(r *Recipe, n *yaml.Node, where string) *Action {
	if n.Kind != yaml.MappingNode {
		p.errAt(n, "%s deve ser um mapa com exatamente uma ação (powershell, sh, bash ou wsl)", where)
		return nil
	}
	a := &Action{}
	var count int
	seen := map[string]bool{}
	for i := 0; i+1 < len(n.Content); i += 2 {
		k, v := n.Content[i], n.Content[i+1]
		if p.dup(seen, k, where) {
			continue
		}
		switch k.Value {
		case "powershell":
			a.PowerShell = p.text(v, "powershell")
			count++
			p.needOS(r, k, "powershell", platform.Windows)
		case "sh":
			a.Sh = p.text(v, "sh")
			count++
			p.needOS(r, k, "sh", platform.Linux, platform.Darwin)
		case "bash":
			a.Bash = p.text(v, "bash")
			count++
			p.needOS(r, k, "bash", platform.Linux, platform.Darwin)
		case "wsl":
			a.WSL = p.parseWSL(v)
			count++
			p.needOS(r, k, "wsl", platform.Windows)
		case "success_codes":
			a.SuccessCodes = p.parseCodes(v)
		default:
			p.errAt(k, "%s: chave desconhecida %q (esperado: powershell, sh, bash, wsl, success_codes)", where, k.Value)
		}
	}
	if count != 1 {
		p.errAt(n, "%s deve ter exatamente uma ação (powershell, sh, bash ou wsl), tem %d", where, count)
	}
	return a
}

func (p *parser) needOS(r *Recipe, k *yaml.Node, action string, allowed ...platform.OS) {
	if r.OS == "" || slices.Contains(allowed, r.OS) {
		return
	}
	p.errAt(k, "a ação %q não vale para receita de %s", action, osName(r.OS))
}

func osName(o platform.OS) string {
	if o == platform.Darwin {
		return "macos"
	}
	return string(o)
}

func (p *parser) parseWSL(v *yaml.Node) *WSLAction {
	if v.Kind != yaml.MappingNode {
		p.errAt(v, "wsl deve ser um mapa com distro, bash e, opcional, user")
		return nil
	}
	w := &WSLAction{}
	seen := map[string]bool{}
	for i := 0; i+1 < len(v.Content); i += 2 {
		k, val := v.Content[i], v.Content[i+1]
		if p.dup(seen, k, "wsl") {
			continue
		}
		switch k.Value {
		case "distro":
			w.Distro = p.text(val, "distro")
			if w.Distro != "" && !distroRe.MatchString(w.Distro) {
				p.errAt(val, "distro %q inválida", w.Distro)
			}
		case "user":
			w.User = p.text(val, "user")
			if w.User != "" && !wslUserRe.MatchString(w.User) {
				p.errAt(val, "user %q inválido (nome de usuário Unix)", w.User)
			}
		case "bash":
			w.Bash = p.text(val, "bash")
		default:
			p.errAt(k, "wsl: chave desconhecida %q (esperado: distro, user, bash)", k.Value)
		}
	}
	if w.Distro == "" || w.Bash == "" {
		p.errAt(v, "wsl exige \"distro\" e \"bash\"")
	}
	return w
}

func (p *parser) parseCodes(v *yaml.Node) []int {
	if v.Kind != yaml.SequenceNode {
		p.errAt(v, "success_codes deve ser uma lista de números")
		return nil
	}
	var out []int
	for _, it := range v.Content {
		c, err := strconv.Atoi(it.Value)
		if it.Kind != yaml.ScalarNode || it.Tag != "!!int" || err != nil || c < 1 || c > 0xFFFF {
			p.errAt(it, "exit code %q inválido (inteiro de 1 a 65535; o 0 já vale)", it.Value)
			continue
		}
		out = append(out, c)
	}
	return out
}

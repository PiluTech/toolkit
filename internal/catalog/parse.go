package catalog

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"regexp"
	"slices"
	"strings"
	"unicode"

	"go.yaml.in/yaml/v3"

	"github.com/PiluVitu/toolkit/internal/tmpl"
)

var (
	idRe     = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)
	sha256Re = regexp.MustCompile(`^[0-9a-fA-F]{64}$`)
	repoRe   = regexp.MustCompile(`^[A-Za-z0-9._-]+/[A-Za-z0-9._-]+$`)
)

var validTypes = []string{"msi", "exe", "zip", "tar.gz", "dmg", "deb", "rpm", "appimage"}

// fields: campos permitidos por provider (obrigatórios em required).
type spec struct {
	required []string
	optional []string
}

var providerSpecs = map[string]spec{
	ProviderWinget:        {required: []string{"id"}, optional: []string{"source"}},
	ProviderBrew:          {optional: []string{"formula", "cask"}}, // exatamente um dos dois (checado à parte)
	ProviderApt:           {required: []string{"package"}},
	ProviderDnf:           {required: []string{"package"}},
	ProviderPacman:        {required: []string{"package"}},
	ProviderFlatpak:       {required: []string{"id"}, optional: []string{"source"}},
	ProviderGitHubRelease: {required: []string{"repo", "asset"}, optional: []string{"sha256", "type", "args"}},
	ProviderURL:           {required: []string{"url", "sha256"}, optional: []string{"type", "args"}},
}

// allFields são todos os campos que algum provider usa (separa "campo de outro provider" de "chave desconhecida").
var allFields = []string{"id", "package", "formula", "cask", "source", "repo", "asset", "url", "sha256", "type", "args"}

var osKeys = []string{KeyWindows, KeyMacOS, KeyLinux, KeyAny}

type parser struct {
	file string
	errs Errors
}

func (p *parser) errAt(n *yaml.Node, format string, a ...any) {
	p.errs = append(p.errs, Error{File: p.file, Line: n.Line, Col: n.Column, Msg: fmt.Sprintf(format, a...)})
}

// Limites de segurança da carga: tamanho do arquivo e número de apps.
const (
	MaxFileSize = 1 << 20
	MaxApps     = 2000
)

// rejectAliases recusa âncoras, aliases e chaves de merge (<<): eles permitem que uma chave "apareça"
// sem estar escrita no arquivo e inflar o documento (billion laughs).
func (p *parser) rejectAliases(n *yaml.Node) {
	if n.Kind == yaml.AliasNode || n.Anchor != "" {
		p.errAt(n, "âncoras e aliases YAML (&x, *x) não são aceitos no catálogo")
		return
	}
	if n.Tag == "!!merge" {
		p.errAt(n, "a chave de merge (<<) não é aceita no catálogo")
	}
	for _, c := range n.Content {
		p.rejectAliases(c)
	}
}

// rejectControl recusa caractere de controle (< 0x20, 0x7f e C1 0x80-0x9f: ESC, OSC, BEL, CR, quebra de
// linha, tab...) em qualquer texto do arquivo, chaves e valores. name, versões e demais textos vão para o
// terminal (list, TUI, logs): sem isso um catálogo local reescreveria a tela do usuário. A mensagem cita o
// ponto de código, nunca o byte cru.
func (p *parser) rejectControl(n *yaml.Node) {
	if n.Kind == yaml.ScalarNode {
		for _, r := range n.Value {
			if unicode.IsControl(r) {
				p.errAt(n, "caractere de controle U+%04X não é aceito no catálogo (texto sem ESC, quebra de linha, tab etc.)", r)
				break
			}
		}
	}
	for _, c := range n.Content {
		p.rejectControl(c)
	}
}

// Parse lê e valida o catálogo. file só aparece nas mensagens de erro.
func Parse(file string, data []byte) (*Catalog, error) {
	if len(data) > MaxFileSize {
		return nil, Errors{{File: file, Msg: fmt.Sprintf("arquivo maior que %d MiB", MaxFileSize>>20)}}
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	var doc yaml.Node
	if err := dec.Decode(&doc); err != nil {
		if errors.Is(err, io.EOF) {
			return nil, Errors{{File: file, Msg: "arquivo vazio"}}
		}
		return nil, Errors{{File: file, Msg: "YAML inválido: " + err.Error()}}
	}
	p := &parser{file: file}
	p.rejectAliases(&doc)
	p.rejectControl(&doc)
	if len(p.errs) > 0 {
		return nil, p.errs
	}
	cat := &Catalog{Apps: map[string]App{}}

	root := &doc
	if root.Kind == yaml.DocumentNode && len(root.Content) == 1 {
		root = root.Content[0]
	}
	if root.Kind != yaml.MappingNode {
		p.errAt(root, "o topo do arquivo deve ser um mapa com a chave \"apps\"")
		return nil, p.errs
	}
	var apps *yaml.Node
	top := map[string]bool{}
	for i := 0; i+1 < len(root.Content); i += 2 {
		k, v := root.Content[i], root.Content[i+1]
		if p.dup(top, k, "topo do arquivo") {
			continue
		}
		if k.Value == "apps" {
			apps = v
		} else {
			p.errAt(k, "chave desconhecida %q no topo (esperado: apps)", k.Value)
		}
	}
	if apps == nil {
		p.errAt(root, "falta a chave \"apps\"")
		return nil, p.errs
	}
	if apps.Kind != yaml.MappingNode {
		p.errAt(apps, "\"apps\" deve ser um mapa de id para app")
		return nil, p.errs
	}
	if n := len(apps.Content) / 2; n > MaxApps {
		p.errAt(apps, "apps demais: %d (o limite é %d)", n, MaxApps)
		return nil, p.errs
	}
	for i := 0; i+1 < len(apps.Content); i += 2 {
		k, v := apps.Content[i], apps.Content[i+1]
		if _, dup := cat.Apps[k.Value]; dup {
			p.errAt(k, "app %q duplicado (o último venceria em silêncio)", k.Value)
			continue
		}
		app, ok := p.parseApp(k, v)
		if !ok {
			continue
		}
		cat.Apps[app.ID] = app
		cat.Order = append(cat.Order, app.ID)
	}
	if len(p.errs) > 0 {
		return nil, p.errs
	}
	return cat, nil
}

// dup marca a chave k como vista em seen; a segunda ocorrência vira erro (senão a última venceria em
// silêncio e, por exemplo, um catálogo local esconderia um instalador).
func (p *parser) dup(seen map[string]bool, k *yaml.Node, where string) bool {
	if seen[k.Value] {
		p.errAt(k, "%s: chave %q repetida (a última venceria em silêncio)", where, k.Value)
		return true
	}
	seen[k.Value] = true
	return false
}

func (p *parser) parseApp(key, node *yaml.Node) (App, bool) {
	start := len(p.errs)
	id := key.Value
	app := App{ID: id, Line: key.Line, Installers: map[string][]Installer{}}
	if !idRe.MatchString(id) {
		p.errAt(key, "app %q: id inválido (use minúsculas, números e hífen)", id)
	}
	if node.Kind != yaml.MappingNode {
		p.errAt(node, "app %q: deve ser um mapa", id)
		return app, false
	}
	var versionsNode *yaml.Node
	seen := map[string]bool{}
	for i := 0; i+1 < len(node.Content); i += 2 {
		k, v := node.Content[i], node.Content[i+1]
		if p.dup(seen, k, fmt.Sprintf("app %q", id)) {
			continue
		}
		switch {
		case k.Value == "name":
			app.Name = strings.TrimSpace(v.Value)
		case k.Value == "category":
			app.Category = v.Value
			if !slices.Contains(Categories, v.Value) {
				p.errAt(v, "app %q: categoria %q desconhecida (use: %s)", id, v.Value, strings.Join(Categories, ", "))
			}
		case k.Value == "versions":
			versionsNode = v
			app.Versions = p.parseVersions(id, v)
		case slices.Contains(osKeys, k.Value):
			app.Installers[k.Value] = p.parseInstallers(id, k.Value, v)
		default:
			p.errAt(k, "app %q: chave desconhecida %q", id, k.Value)
		}
	}
	if app.Name == "" {
		p.errAt(node, "app %q: falta \"name\"", id)
	}
	if app.Category == "" {
		p.errAt(node, "app %q: falta \"category\"", id)
	}
	total := 0
	for _, l := range app.Installers {
		total += len(l)
	}
	if total == 0 {
		p.errAt(node, "app %q: nenhum instalador (windows, macos, linux ou any)", id)
	}
	p.checkVersionTemplates(app, versionsNode)
	return app, len(p.errs) == start
}

func (p *parser) parseVersions(id string, v *yaml.Node) Versions {
	switch v.Kind {
	case yaml.ScalarNode:
		if v.Tag == "!!bool" {
			return Versions{Query: v.Value == "true"}
		}
	case yaml.SequenceNode:
		var list []string
		for _, it := range v.Content {
			if it.Kind != yaml.ScalarNode || it.Value == "" {
				p.errAt(it, "app %q: cada versão deve ser um texto (use aspas: \"3.13\")", id)
				continue
			}
			if !tmpl.ValidValue(it.Value) {
				p.errAt(it, "app %q: versão %q inválida (só letras, números e . _ + ~ -, começando por letra ou número)", id, it.Value)
				continue
			}
			list = append(list, it.Value)
		}
		if len(list) == 0 {
			p.errAt(v, "app %q: a lista de versions não pode ser vazia", id)
		}
		return Versions{List: list}
	}
	p.errAt(v, "app %q: versions deve ser true ou uma lista de versões", id)
	return Versions{}
}

// checkVersionTemplates: variável de versão em campo exige a lista declarada (versions: true não basta,
// porque o id muda com a versão e o gerenciador não sabe quais existem).
func (p *parser) checkVersionTemplates(app App, versionsNode *yaml.Node) {
	for _, list := range app.Installers {
		for _, in := range list {
			for _, f := range in.stringFields() {
				names, err := tmpl.Names(f)
				if err != nil {
					continue // já reportado em parseInstaller
				}
				for _, n := range names {
					if tmpl.VersionVars[n] && len(app.Versions.List) == 0 {
						at := &yaml.Node{Line: in.Line, Column: in.Col}
						if versionsNode != nil {
							at = versionsNode
						}
						p.errAt(at, "app %q: usa {{%s}} em %q, então \"versions\" precisa ser uma lista (a primeira é a padrão)", app.ID, n, f)
						return
					}
				}
			}
		}
	}
}

func (in Installer) stringFields() []string {
	return append([]string{in.ID, in.Package, in.Formula, in.Cask, in.Source, in.Repo, in.Asset, in.URL, in.Type}, in.Args...)
}

func (p *parser) parseInstallers(appID, osKey string, v *yaml.Node) []Installer {
	if v.Kind != yaml.SequenceNode {
		p.errAt(v, "app %q: %q deve ser uma lista de instaladores", appID, osKey)
		return nil
	}
	var out []Installer
	for _, it := range v.Content {
		if in, ok := p.parseInstaller(appID, it); ok {
			out = append(out, in)
		}
	}
	return out
}

func (p *parser) parseInstaller(appID string, n *yaml.Node) (Installer, bool) {
	start := len(p.errs)
	in := Installer{Line: n.Line, Col: n.Column}
	if n.Kind != yaml.MappingNode {
		p.errAt(n, "app %q: cada instalador deve ser um mapa", appID)
		return in, false
	}
	vals := map[string]*yaml.Node{}
	var provNode *yaml.Node
	seen := map[string]bool{}
	for i := 0; i+1 < len(n.Content); i += 2 {
		k, v := n.Content[i], n.Content[i+1]
		if p.dup(seen, k, fmt.Sprintf("app %q (instalador)", appID)) {
			continue
		}
		if k.Value == "provider" {
			provNode = v
			in.Provider = v.Value
			continue
		}
		if !slices.Contains(allFields, k.Value) {
			p.errAt(k, "app %q: chave desconhecida %q", appID, k.Value)
			continue
		}
		vals[k.Value] = v
	}
	if provNode == nil {
		p.errAt(n, "app %q: falta \"provider\"", appID)
		return in, false
	}
	sp, known := providerSpecs[in.Provider]
	if !known {
		providers := make([]string, 0, len(providerSpecs))
		for k := range providerSpecs {
			providers = append(providers, k)
		}
		slices.Sort(providers)
		p.errAt(provNode, "app %q: provider %q desconhecido (use: %s)", appID, in.Provider, strings.Join(providers, ", "))
		return in, false
	}

	allowed := map[string]bool{}
	for _, f := range sp.required {
		allowed[f] = true
	}
	for _, f := range sp.optional {
		allowed[f] = true
	}
	for _, f := range sp.required {
		if vals[f] == nil {
			p.errAt(n, "app %q: provider %q exige %q", appID, in.Provider, f)
		}
	}
	for _, f := range allFields {
		if vals[f] != nil && !allowed[f] {
			p.errAt(vals[f], "app %q: campo %q não vale para o provider %q (aceita: %s)", appID, f, in.Provider,
				strings.Join(sortedKeys(allowed), ", "))
		}
	}
	if in.Provider == ProviderBrew {
		if (vals["formula"] == nil) == (vals["cask"] == nil) {
			p.errAt(n, "app %q: provider \"brew\" exige exatamente um entre \"formula\" e \"cask\"", appID)
		}
	}

	str := func(name string) string {
		v := vals[name]
		if v == nil || !allowed[name] {
			return ""
		}
		if v.Kind != yaml.ScalarNode || strings.TrimSpace(v.Value) == "" {
			p.errAt(v, "app %q: %q deve ser um texto não vazio", appID, name)
			return ""
		}
		if err := tmpl.Validate(v.Value); err != nil {
			p.errAt(v, "app %q: %q: %v", appID, name, err)
		}
		return v.Value
	}
	in.ID, in.Package, in.Formula, in.Cask = str("id"), str("package"), str("formula"), str("cask")
	in.Source, in.Repo, in.Asset, in.URL = str("source"), str("repo"), str("asset"), str("url")
	in.SHA256, in.Type = str("sha256"), str("type")

	if in.SHA256 != "" && !sha256Re.MatchString(in.SHA256) {
		p.errAt(vals["sha256"], "app %q: sha256 deve ter 64 caracteres hexadecimais", appID)
	}
	if in.Type != "" && !slices.Contains(validTypes, in.Type) {
		p.errAt(vals["type"], "app %q: type %q desconhecido (use: %s)", appID, in.Type, strings.Join(validTypes, ", "))
	}
	if in.Repo != "" && !repoRe.MatchString(in.Repo) {
		p.errAt(vals["repo"], "app %q: repo deve ser no formato dono/repositório", appID)
	}
	if in.URL != "" {
		if err := CheckURL(in.URL, true); err != nil {
			p.errAt(vals["url"], "app %q: %v", appID, err)
		}
	}
	if a := vals["args"]; a != nil && allowed["args"] {
		if a.Kind != yaml.SequenceNode {
			p.errAt(a, "app %q: args deve ser uma lista de textos", appID)
		} else {
			for _, it := range a.Content {
				if it.Kind != yaml.ScalarNode || it.Tag != "!!str" {
					p.errAt(it, "app %q: cada item de args deve ser um texto (use aspas em números e booleanos)", appID)
					continue
				}
				if err := tmpl.Validate(it.Value); err != nil {
					p.errAt(it, "app %q: args: %v", appID, err)
					continue
				}
				in.Args = append(in.Args, it.Value)
			}
		}
	}
	return in, len(p.errs) == start
}

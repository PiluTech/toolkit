package appsnativos

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"unicode"

	"github.com/PiluTech/toolkit/internal/diagfmt"
	"github.com/PiluTech/toolkit/internal/textsafe"
)

// Scope é até onde a remoção vale.
type Scope string

// Escopos. O padrão é só o usuário atual (não exige administrador).
const (
	ScopeUser  Scope = scopeUser
	ScopeTodos Scope = scopeTodos
)

// ItemKind diz o que o item remove.
type ItemKind string

// Tipos de item.
const (
	// KindPackage: o pacote instalado (do usuário atual, ou de todos no escopo ScopeTodos).
	KindPackage ItemKind = kindPackage
	// KindProvisioned: o provisionamento na imagem do Windows (só ScopeTodos, administrador).
	KindProvisioned ItemKind = kindProvisioned
)

// Selection é o que o usuário pediu.
type Selection struct {
	// Perfil e Apps se somam; ao menos um dos dois é obrigatório.
	Perfil Perfil
	// Apps são ids do catálogo (--app).
	Apps []string
	// TodosUsuarios (--todos-usuarios) estende a remoção aos outros usuários e ao provisionamento (administrador).
	TodosUsuarios bool
}

// Item é um pacote a remover.
type Item struct {
	App        string // id do catálogo
	Title      string // título no idioma do Build
	Name       string
	FullName   string // PackageFullName (ou PackageName do provisionamento)
	FamilyName string
	Version    string
	Publisher  string
	Kind       ItemKind
	Scope      Scope

	// Marcas da listagem; um item marcado nunca é removido (Apply recusa).
	IsFramework   bool
	NonRemovable  bool
	SignatureKind string
}

// Admin diz se remover este item exige administrador.
func (it Item) Admin() bool { return it.Scope == ScopeTodos || it.Kind == KindProvisioned }

// Motivos de um pacote do catálogo ter ficado fora do plano.
const (
	SkipFramework    = "framework"
	SkipNonRemovable = "nonremovable"
	SkipSystem       = "system"
)

// Skipped é um pacote que casa o catálogo mas NUNCA é removido.
type Skipped struct {
	App      string
	Name     string
	FullName string
	Reason   string
}

// Plan é o que `apps-nativos` faria.
type Plan struct {
	Scope   Scope
	IsAdmin bool
	Items   []Item
	// Absent são os ids do catálogo selecionados que não estão instalados (ficam fora do plano).
	Absent  []string
	Skipped []Skipped
}

// NeedsAdmin diz se o escopo exige administrador (há o que remover para todos os usuários).
func (p *Plan) NeedsAdmin() bool {
	for _, it := range p.Items {
		if it.Admin() {
			return true
		}
	}
	return false
}

// Pending conta os itens a remover.
func (p *Plan) Pending() int { return len(p.Items) }

// ValidateItem confere um item contra o catálogo e a lista de negação: é o que Build e Apply rodam antes de qualquer
// remoção (o plano pode ter sido montado por outro código). Devolve ErrItemInvalido.
func ValidateItem(it Item) error {
	return checkIdentity(identity{app: it.App, name: it.Name, full: it.FullName, family: it.FamilyName, version: it.Version,
		publisher: it.Publisher, kind: string(it.Kind), scope: string(it.Scope), isFramework: it.IsFramework,
		nonRemovable: it.NonRemovable, signatureKind: it.SignatureKind})
}

// cleanText tira controles e caracteres de formatação (bidi, zero width) de texto livre do sistema e o limita.
func cleanText(s string) string {
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return ' '
		}
		return r
	}, s)
	if r := []rune(s); len(r) > maxPublisher {
		s = string(r[:maxPublisher])
	}
	return strings.TrimSpace(s)
}

func skipReason(p Package) string {
	switch {
	case p.IsFramework:
		return SkipFramework
	case p.NonRemovable:
		return SkipNonRemovable
	case strings.EqualFold(p.SignatureKind, "System"):
		return SkipSystem
	}
	return ""
}

// Build lê o Windows (só leituras) e monta o plano: cada app selecionado que está instalado vira um item; o que já está
// ausente fica fora; pacote do catálogo marcado como framework, não removível ou de sistema é ignorado com o motivo.
// No escopo de todos os usuários, só com administrador se lê -AllUsers e o provisionamento; sem administrador o plano sai
// com a visão do usuário atual e NeedsAdmin() true (Apply recusa antes de mudar qualquer coisa).
func Build(ctx context.Context, sys System, sel Selection, l Lang) (*Plan, error) {
	apps, err := Selecionar(sel.Perfil, sel.Apps)
	if err != nil {
		return nil, err
	}
	scope := ScopeUser
	if sel.TodosUsuarios {
		scope = ScopeTodos
	}
	admin := sys.IsAdmin(ctx)
	plan := &Plan{Scope: scope, IsAdmin: admin}

	all := scope == ScopeTodos && admin
	installed, err := sys.ListInstalled(ctx, all)
	if err != nil {
		return nil, fmt.Errorf("não deu para listar os apps instalados: %w", err)
	}
	var prov []Provisioned
	if all {
		if prov, err = sys.ListProvisioned(ctx); err != nil {
			return nil, fmt.Errorf("não deu para listar os apps provisionados: %w", err)
		}
	}
	sort.SliceStable(installed, func(i, j int) bool { return installed[i].PackageFullName < installed[j].PackageFullName })
	sort.SliceStable(prov, func(i, j int) bool { return prov[i].PackageName < prov[j].PackageName })

	seen := map[string]bool{}
	for _, a := range apps {
		found := false
		for _, p := range installed {
			if p.Name != a.Name || seen["package:"+p.PackageFullName] {
				continue
			}
			seen["package:"+p.PackageFullName] = true
			found = true
			if r := skipReason(p); r != "" {
				plan.Skipped = append(plan.Skipped, Skipped{App: a.ID, Name: a.Name, FullName: p.PackageFullName, Reason: r})
				continue
			}
			it := Item{App: a.ID, Title: a.Title(l), Name: p.Name, FullName: p.PackageFullName, FamilyName: p.PackageFamilyName,
				Version: p.Version, Publisher: cleanText(p.Publisher), Kind: KindPackage, Scope: scope,
				IsFramework: p.IsFramework, NonRemovable: p.NonRemovable, SignatureKind: p.SignatureKind}
			if err := ValidateItem(it); err != nil {
				return nil, fmt.Errorf("saída inesperada do Windows para %s: %w", a.Name, err)
			}
			plan.Items = append(plan.Items, it)
		}
		for _, p := range prov {
			if p.DisplayName != a.Name || seen["provisioned:"+p.PackageName] {
				continue
			}
			seen["provisioned:"+p.PackageName] = true
			found = true
			it := Item{App: a.ID, Title: a.Title(l), Name: p.DisplayName, FullName: p.PackageName, Version: p.Version,
				Kind: KindProvisioned, Scope: scope}
			if err := ValidateItem(it); err != nil {
				return nil, fmt.Errorf("saída inesperada do Windows para %s: %w", a.Name, err)
			}
			plan.Items = append(plan.Items, it)
		}
		if !found {
			plan.Absent = append(plan.Absent, a.ID)
		}
	}
	return plan, nil
}

func scopeName(s Scope, l Lang) string {
	if s == ScopeTodos {
		return diagfmt.T(l, "todos os usuários e o provisionamento", "all users and the provisioning", "todos los usuarios y el aprovisionamiento")
	}
	return diagfmt.T(l, "somente o usuário atual", "current user only", "solo el usuario actual")
}

func skipText(r string, l Lang) string {
	switch r {
	case SkipFramework:
		return "framework"
	case SkipNonRemovable:
		return diagfmt.T(l, "não removível", "non-removable", "no se puede quitar")
	}
	return diagfmt.T(l, "componente do sistema", "system component", "componente del sistema")
}

func titleOf(id, fallback string, l Lang) string {
	if a, ok := Find(id); ok {
		return a.Title(l)
	}
	return textsafe.Line(fallback)
}

// Format mostra o plano: escopo, o que será removido (título, pacote, versão), o que já está ausente, o que foi ignorado
// por segurança, o aviso de que não há desfazer automático e, quando falta, o pedido de administrador. Todo texto vindo do
// sistema passa por textsafe.
func (p *Plan) Format(l Lang) string {
	t := func(pt, en, es string) string { return diagfmt.T(l, pt, en, es) }
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n", t("Apps nativos do Windows", "Windows native apps", "Apps nativas de Windows"))
	fmt.Fprintf(&b, "%s: %s\n", t("Escopo", "Scope", "Alcance"), scopeName(p.Scope, l))
	if len(p.Items) > 0 {
		fmt.Fprintf(&b, "\n%s:\n", t("Será removido", "Will be removed", "Se eliminará"))
		for _, it := range p.Items {
			what := ""
			if it.Kind == KindProvisioned {
				what = " (" + t("provisionamento", "provisioning", "aprovisionamiento") + ")"
			}
			admin := ""
			if it.Admin() {
				admin = " [" + t("exige administrador", "needs administrator", "requiere administrador") + "]"
			}
			fmt.Fprintf(&b, "  - %s: %s%s %s%s\n", titleOf(it.App, it.Name, l), textsafe.Line(it.Name), what, textsafe.Line(it.Version), admin)
		}
	}
	if len(p.Absent) > 0 {
		ts := make([]string, 0, len(p.Absent))
		for _, id := range p.Absent {
			ts = append(ts, titleOf(id, id, l))
		}
		fmt.Fprintf(&b, "\n%s: %s\n", t("Já ausentes (ignorados)", "Already absent (ignored)", "Ya ausentes (ignorados)"), strings.Join(ts, "; "))
	}
	if len(p.Skipped) > 0 {
		fmt.Fprintf(&b, "\n%s:\n", t("Ignorados por segurança (nunca removidos)", "Skipped for safety (never removed)", "Omitidos por seguridad (nunca se eliminan)"))
		for _, s := range p.Skipped {
			fmt.Fprintf(&b, "  - %s (%s)\n", textsafe.Line(s.Name), skipText(s.Reason, l))
		}
	}
	fmt.Fprintf(&b, "\n%s\n", t("Atenção: não há desfazer automático (o pacote some do disco). Antes de remover o toolkit grava um backup com a identidade de cada app; `toolkit apps-nativos reinstalar` mostra como reinstalar, sem executar nada.",
		"Warning: there is no automatic undo (the package is deleted from disk). Before removing, the toolkit saves a backup with each app's identity; `toolkit apps-nativos reinstalar` shows how to reinstall, without running anything.",
		"Atención: no hay deshacer automático (el paquete desaparece del disco). Antes de eliminar, el toolkit guarda una copia con la identidad de cada app; `toolkit apps-nativos reinstalar` muestra cómo reinstalar, sin ejecutar nada."))
	if n := p.Pending(); n == 0 {
		fmt.Fprintf(&b, "\n%s.\n", t("Resultado: nada a remover", "Result: nothing to remove", "Resultado: nada que eliminar"))
	} else {
		fmt.Fprintf(&b, "\n%d %s", n, t("item(ns) a remover", "item(s) to remove", "elemento(s) a eliminar"))
		if p.NeedsAdmin() && !p.IsAdmin {
			fmt.Fprintf(&b, "; %s", t("para aplicar abra o terminal como administrador", "to apply, open the terminal as administrator", "para aplicar, abra el terminal como administrador"))
		}
		b.WriteString(".\n")
	}
	return b.String()
}

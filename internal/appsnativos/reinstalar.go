package appsnativos

import (
	"fmt"
	"sort"
	"strings"

	"github.com/PiluTech/toolkit/internal/diagfmt"
	"github.com/PiluTech/toolkit/internal/textsafe"
)

// ReinstallItem é um app removido, com o que o usuário pode fazer para trazê-lo de volta.
type ReinstallItem struct {
	App        string
	Name       string
	FamilyName string
	Version    string
	Scope      string
	Date       string
	StoreID    string
	// CanRegister: o provisionamento NÃO foi removido, então o Windows pode ainda ter o pacote guardado e registrá-lo de novo.
	CanRegister bool
}

// ReinstallPlan é só texto: o toolkit NUNCA executa a reinstalação.
type ReinstallPlan struct {
	Items []ReinstallItem
}

// BuildReinstall lê o backup, valida TODAS as entradas contra o catálogo (ErrBackupInvalido, sem devolver nada) e monta as
// instruções de reinstalação, uma por app. Não executa comando algum.
func BuildReinstall(store Store) (*ReinstallPlan, error) {
	bk, err := store.Load()
	if err != nil {
		return nil, err
	}
	if bk == nil || len(bk.Entries) == 0 {
		return nil, ErrNoBackup
	}
	if err := ValidateBackup(bk); err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(bk.Entries))
	for id := range bk.Entries {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	byApp := map[string]*ReinstallItem{}
	var order []string
	for _, id := range ids {
		e := bk.Entries[id]
		it, ok := byApp[e.App]
		if !ok {
			it = &ReinstallItem{App: e.App, Name: e.Name, Version: e.Version, Scope: e.Scope, Date: e.Date.Format("2006-01-02")}
			if a, ok := Find(e.App); ok {
				it.StoreID = a.StoreID
			}
			byApp[e.App] = it
			order = append(order, e.App)
		}
		if e.Kind == kindPackage {
			it.FamilyName, it.Version, it.Scope = e.PackageFamilyName, e.Version, e.Scope
		}
	}
	// registrar de novo só faz sentido se NENHUMA remoção do app tirou o provisionamento
	for _, e := range bk.Entries {
		if e.Kind == kindProvisioned || e.Scope == scopeTodos {
			if it := byApp[e.App]; it != nil {
				it.Scope = scopeTodos
			}
		}
	}
	rp := &ReinstallPlan{}
	for _, app := range order {
		it := byApp[app]
		it.CanRegister = it.Scope == scopeUser && it.FamilyName != ""
		rp.Items = append(rp.Items, *it)
	}
	return rp, nil
}

// Format imprime as instruções (nos três idiomas). Só texto.
func (rp *ReinstallPlan) Format(l Lang) string {
	t := func(pt, en, es string) string { return diagfmt.T(l, pt, en, es) }
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n", t("Reinstalar apps nativos removidos (o toolkit só mostra os comandos; não executa nada)",
		"Reinstall removed native apps (the toolkit only shows the commands; it does not run anything)",
		"Reinstalar apps nativas eliminadas (el toolkit solo muestra los comandos; no ejecuta nada)"))
	for _, it := range rp.Items {
		title := titleOf(it.App, it.Name, l)
		scope := t("usuário atual", "current user", "usuario actual")
		if it.Scope == scopeTodos {
			scope = t("todos os usuários", "all users", "todos los usuarios")
		}
		fmt.Fprintf(&b, "\n- %s (%s %s; %s %s; %s: %s)\n", title, textsafe.Line(it.Name), textsafe.Line(it.Version),
			t("removido em", "removed on", "eliminado el"), it.Date, t("escopo", "scope", "alcance"), scope)
		n := 1
		if it.CanRegister {
			fmt.Fprintf(&b, "    %d. %s\n       Add-AppxPackage -RegisterByFamilyName -MainPackage %s\n", n,
				t("Se o Windows ainda guarda o pacote (provisionamento mantido), registre de novo no PowerShell:",
					"If Windows still keeps the package (provisioning kept), register it again in PowerShell:",
					"Si Windows aún conserva el paquete (aprovisionamiento mantenido), regístrelo de nuevo en PowerShell:"),
				textsafe.Line(it.FamilyName))
			n++
		}
		if it.StoreID != "" {
			fmt.Fprintf(&b, "    %d. winget install --id %s --source msstore\n", n, textsafe.Line(it.StoreID))
		} else {
			fmt.Fprintf(&b, "    %d. %s \"%s\"\n", n, t("Abra a Microsoft Store e procure por", "Open the Microsoft Store and search for", "Abra Microsoft Store y busque"), title)
		}
	}
	fmt.Fprintf(&b, "\n%s\n", t("Reinstalar pode exigir a Microsoft Store e uma conta Microsoft.",
		"Reinstalling may require the Microsoft Store and a Microsoft account.",
		"Reinstalar puede requerir Microsoft Store y una cuenta de Microsoft."))
	return b.String()
}

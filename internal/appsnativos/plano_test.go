package appsnativos

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func names(items []Item) []string {
	var out []string
	for _, it := range items {
		out = append(out, string(it.Kind)+":"+it.Name)
	}
	return out
}

func TestBuildUsuarioListaSoOQueEstaInstaladoESelecionado(t *testing.T) {
	f := newFake(false)
	f.installed = []Package{pkg("Microsoft.BingWeather"), pkg("Microsoft.BingNews"), pkg("Microsoft.WindowsStore"),
		pkg("Microsoft.MicrosoftEdge.Stable"), pkg("Microsoft.People")}
	plan, err := Build(context.Background(), f, Selection{Perfil: PerfilBasico}, 0)
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(names(plan.Items), ",")
	if got != "package:Microsoft.BingWeather,package:Microsoft.BingNews" {
		t.Errorf("itens = %s", got)
	}
	if plan.Scope != ScopeUser || plan.NeedsAdmin() {
		t.Errorf("escopo usuário não exige administrador: %v %v", plan.Scope, plan.NeedsAdmin())
	}
	if len(plan.Absent) != 6 { // 8 do básico, 2 instalados
		t.Errorf("ausentes = %d (%v)", len(plan.Absent), plan.Absent)
	}
	if strings.Join(f.lists, ",") != "installed:all=false" {
		t.Errorf("leituras = %v (usuário só lê o próprio)", f.lists)
	}
	if len(f.log) != 0 {
		t.Errorf("Build não pode mudar nada: %v", f.log)
	}
	if plan.Items[0].Scope != ScopeUser || plan.Items[0].Title != "MSN Clima" {
		t.Errorf("item = %+v", plan.Items[0])
	}
}

func TestBuildNaoRemoveFrameworkNemNaoRemovivelNemSistema(t *testing.T) {
	f := newFake(false)
	a, b, c, ok := pkg("Microsoft.BingWeather"), pkg("Microsoft.BingNews"), pkg("Microsoft.MicrosoftOfficeHub"), pkg("Microsoft.Getstarted")
	a.IsFramework = true
	b.NonRemovable = true
	c.SignatureKind = "System"
	f.installed = []Package{a, b, c, ok}
	plan, err := Build(context.Background(), f, Selection{Perfil: PerfilBasico}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(names(plan.Items), ","); got != "package:Microsoft.Getstarted" {
		t.Errorf("só o removível entra: %s", got)
	}
	if len(plan.Skipped) != 3 {
		t.Fatalf("ignorados = %+v", plan.Skipped)
	}
	out := plan.Format(0)
	for _, w := range []string{"framework", "não removível", "sistema"} {
		if !strings.Contains(out, w) {
			t.Errorf("o plano deveria explicar %q:\n%s", w, out)
		}
	}
}

func TestBuildTodosSemAdminMostraPlanoMasExigeAdmin(t *testing.T) {
	f := newFake(false)
	f.installed = []Package{pkg("Microsoft.BingWeather")}
	plan, err := Build(context.Background(), f, Selection{Apps: []string{"bing-weather"}, TodosUsuarios: true}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Scope != ScopeTodos || !plan.NeedsAdmin() || plan.IsAdmin {
		t.Errorf("plan = %+v", plan)
	}
	if strings.Join(f.lists, ",") != "installed:all=false" {
		t.Errorf("sem administrador não se lê -AllUsers nem o provisionado: %v", f.lists)
	}
	out := plan.Format(0)
	if !strings.Contains(out, "administrador") {
		t.Errorf("o plano manda abrir como administrador:\n%s", out)
	}
	if items := plan.Items; len(items) != 1 || items[0].Scope != ScopeTodos {
		t.Errorf("itens = %+v", items)
	}
}

func TestBuildTodosComAdminInclueOutrosUsuariosEProvisionado(t *testing.T) {
	f := newFake(true)
	f.installed = []Package{pkg("Microsoft.BingWeather")}
	f.others = []Package{pkg("Microsoft.People")}
	f.provisioned = []Provisioned{prov("Microsoft.BingWeather"), prov("Microsoft.Todos"), prov("Microsoft.WindowsStore")}
	plan, err := Build(context.Background(), f, Selection{Apps: []string{"bing-weather", "people", "todo"}, TodosUsuarios: true}, 0)
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(names(plan.Items), ",")
	want := "package:Microsoft.BingWeather,provisioned:Microsoft.BingWeather,package:Microsoft.People,provisioned:Microsoft.Todos"
	if got != want {
		t.Errorf("itens\n got %s\nwant %s", got, want)
	}
	if strings.Join(f.lists, ",") != "installed:all=true,provisioned" {
		t.Errorf("leituras = %v", f.lists)
	}
	if !plan.NeedsAdmin() {
		t.Error("escopo todos exige administrador")
	}
}

func TestBuildRecusaSaidaDoSistemaForaDoFormato(t *testing.T) {
	f := newFake(false)
	bad := pkg("Microsoft.BingWeather")
	bad.PackageFullName = "x'; Remove-Item C:\\ -Recurse #"
	f.installed = []Package{bad}
	if _, err := Build(context.Background(), f, Selection{Perfil: PerfilBasico}, 0); !errors.Is(err, ErrItemInvalido) {
		t.Errorf("full name com injeção deveria falhar: %v", err)
	}
	f.installed = []Package{pkg("Microsoft.BingWeather")}
	f.installed[0].PackageFamilyName = "a b"
	if _, err := Build(context.Background(), f, Selection{Perfil: PerfilBasico}, 0); !errors.Is(err, ErrItemInvalido) {
		t.Errorf("family name inválido deveria falhar: %v", err)
	}
}

func TestBuildSelecaoInvalida(t *testing.T) {
	f := newFake(false)
	if _, err := Build(context.Background(), f, Selection{}, 0); !errors.Is(err, ErrNadaSelecionado) {
		t.Errorf("%v", err)
	}
	if _, err := Build(context.Background(), f, Selection{Apps: []string{"windows-store"}}, 0); !errors.Is(err, ErrAppDesconhecido) {
		t.Errorf("%v", err)
	}
	if len(f.lists) != 0 {
		t.Errorf("seleção inválida não lê o sistema: %v", f.lists)
	}
}

func TestFormatNosTresIdiomasELimpaTextoDoSistema(t *testing.T) {
	f := newFake(false)
	p := pkg("Microsoft.BingWeather")
	p.Publisher = "CN=Evil\x1b]0;pwn\x07, O=\u202eMS"
	f.installed = []Package{p}
	plan, err := Build(context.Background(), f, Selection{Apps: []string{"bing-weather"}}, 0)
	if err != nil {
		t.Fatal(err)
	}
	for l, want := range map[Lang]string{0: "será removido", 1: "will be removed", 2: "se eliminará"} {
		out := plan.Format(l)
		if !strings.Contains(strings.ToLower(out), want) {
			t.Errorf("idioma %d sem %q:\n%s", l, want, out)
		}
		if strings.ContainsAny(out, "\x1b\x07\u202e") {
			t.Errorf("texto do sistema vazou sem limpar:\n%q", out)
		}
		if !strings.Contains(out, "Microsoft.BingWeather") || !strings.Contains(out, "1.2.3.0") {
			t.Errorf("faltou nome ou versão:\n%s", out)
		}
	}
	if !strings.Contains(plan.Format(0), "reinstalar") {
		t.Error("o plano deve avisar que não há desfazer automático e apontar 'reinstalar'")
	}
}

func TestFormatVazio(t *testing.T) {
	f := newFake(false)
	plan, err := Build(context.Background(), f, Selection{Perfil: PerfilBasico}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Pending() != 0 || !strings.Contains(plan.Format(0), "nada") {
		t.Errorf("plano vazio:\n%s", plan.Format(0))
	}
}

func TestMensagemDePlataforma(t *testing.T) {
	if UnsupportedMessage("windows", 0) != "" {
		t.Error("windows é suportado")
	}
	mac, lin := UnsupportedMessage("darwin", 0), UnsupportedMessage("linux", 0)
	if !strings.Contains(mac, "protegido") || !strings.Contains(lin, "distribuição") {
		t.Errorf("mensagens: %q / %q", mac, lin)
	}
	if UnsupportedMessage("darwin", 1) == mac || UnsupportedMessage("linux", 2) == lin {
		t.Error("outros idiomas devem ter texto próprio")
	}
	if UnsupportedMessage("plan9", 0) == "" {
		t.Error("SO desconhecido também não é suportado")
	}
}

// Lupa #65: Microsoft.GetHelp hospeda os solucionadores de problemas do Windows (Update, rede, áudio, BITS): nunca removível.
func TestGetHelpNuncaEntraNoPlanoNemNoApply(t *testing.T) {
	f := newFake(true)
	f.installed = []Package{pkg("Microsoft.GetHelp"), pkg("Microsoft.BingWeather")}
	if !Protected("Microsoft.GetHelp") {
		t.Error("Microsoft.GetHelp deve estar na lista de negação")
	}
	if _, ok := FindByName("Microsoft.GetHelp"); ok {
		t.Error("Microsoft.GetHelp não pode estar no catálogo removível")
	}
	for _, sel := range []Selection{{Perfil: PerfilBasico}, {Perfil: PerfilCompleto}, {Perfil: PerfilCompleto, TodosUsuarios: true}} {
		plan, err := Build(context.Background(), f, sel, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, it := range plan.Items {
			if strings.Contains(it.Name, "GetHelp") {
				t.Errorf("GetHelp no plano: %+v", it)
			}
		}
		if _, err := Apply(context.Background(), f, &memStore{}, plan, t0, Options{}); err != nil {
			t.Fatal(err)
		}
	}
	for _, l := range f.log {
		if strings.Contains(l, "GetHelp") {
			t.Fatalf("GetHelp removido: %v", f.log)
		}
	}
	if _, err := Build(context.Background(), f, Selection{Apps: []string{"get-help"}}, 0); err == nil {
		t.Error("--app get-help deve falhar (fora do catálogo)")
	}
}

// Lupa #65: Mapas (retirado da Store em jul/2025) e o Visualizador 3D (retirado em 01/07/2026) não podem ser oferecidos:
// com todos os usuários o provisionamento sumiria e a reinstalação prometida não existe.
func TestAppsSemReinstalacaoNaoSaoRemoviveis(t *testing.T) {
	for _, n := range []string{"Microsoft.WindowsMaps", "Microsoft.Microsoft3DViewer"} {
		if _, ok := FindByName(n); ok {
			t.Errorf("%s não pode estar no catálogo removível (sem reinstalação possível na Store)", n)
		}
	}
	for _, id := range []string{"maps", "3d-viewer"} {
		if _, err := Selecionar("", []string{id}); err == nil {
			t.Errorf("--app %s deve falhar", id)
		}
	}
}

package appsnativos

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

var t0 = time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)

func userPlan(t *testing.T, f *fakeSys, ids ...string) *Plan {
	t.Helper()
	plan, err := Build(context.Background(), f, Selection{Apps: ids}, 0)
	if err != nil {
		t.Fatal(err)
	}
	return plan
}

func TestApplyGravaBackupAntesDeRemover(t *testing.T) {
	f := newFake(false)
	f.installed = []Package{pkg("Microsoft.BingWeather"), pkg("Microsoft.BingNews")}
	st := &memStore{}
	checked := 0
	f.onMutate = func() {
		checked++
		if st.b == nil || len(st.b.Entries) != 2 {
			t.Errorf("o backup com TODOS os itens tem de existir antes da primeira remoção: %+v", st.b)
		}
	}
	res, err := Apply(context.Background(), f, st, userPlan(t, f, "bing-weather", "bing-news"), t0, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if checked != 2 || len(res.Removed) != 2 || res.BackupPath != st.Path() {
		t.Errorf("checked=%d res=%+v", checked, res)
	}
	if strings.Join(f.log, ",") != "remove-user:Microsoft.BingWeather_1.2.3.0_x64__8wekyb3d8bbwe,remove-user:Microsoft.BingNews_1.2.3.0_x64__8wekyb3d8bbwe" {
		t.Errorf("log = %v", f.log)
	}
	e := st.b.Entries["package:Microsoft.BingWeather_1.2.3.0_x64__8wekyb3d8bbwe"]
	if e.Name != "Microsoft.BingWeather" || e.App != "bing-weather" || e.Scope != "user" || e.Version != "1.2.3.0" || e.PackageFamilyName == "" || e.Publisher == "" || !e.Date.Equal(t0) {
		t.Errorf("entrada do backup = %+v", e)
	}
}

func TestApplyErroNoMeioParaEBackupCobreOQueMudou(t *testing.T) {
	f := newFake(false)
	f.installed = []Package{pkg("Microsoft.BingWeather"), pkg("Microsoft.BingNews"), pkg("Microsoft.GetHelp")}
	f.failOn = "remove-user:Microsoft.BingNews"
	st := &memStore{}
	res, err := Apply(context.Background(), f, st, userPlan(t, f, "bing-weather", "bing-news", "get-help"), t0, Options{})
	if err == nil || !strings.Contains(err.Error(), "BingNews") {
		t.Fatalf("erro = %v", err)
	}
	if len(res.Removed) != 1 || len(f.log) != 1 {
		t.Errorf("para no primeiro erro: removed=%d log=%v", len(res.Removed), f.log)
	}
	if st.b == nil || st.b.Entries["package:Microsoft.BingWeather_1.2.3.0_x64__8wekyb3d8bbwe"].Name == "" ||
		st.b.Entries["package:Microsoft.BingNews_1.2.3.0_x64__8wekyb3d8bbwe"].Name == "" {
		t.Errorf("o backup tem de cobrir o removido e o que falhou (pode ter mudado pela metade): %+v", st.b)
	}
	if _, ok := st.b.Entries["package:Microsoft.GetHelp_1.2.3.0_x64__8wekyb3d8bbwe"]; ok {
		t.Error("o que nem foi tentado não fica no backup")
	}
}

func TestApplyTodosSemAdminRecusaAntesDeMudarQualquerCoisa(t *testing.T) {
	f := newFake(false)
	f.installed = []Package{pkg("Microsoft.BingWeather")}
	st := &memStore{}
	plan := userPlan(t, f, "bing-weather")
	plan2, err := Build(context.Background(), f, Selection{Apps: []string{"bing-weather"}, TodosUsuarios: true}, 0)
	if err != nil {
		t.Fatal(err)
	}
	_ = plan
	res, err := Apply(context.Background(), f, st, plan2, t0, Options{})
	if !errors.Is(err, ErrNeedsAdmin) {
		t.Fatalf("erro = %v", err)
	}
	if len(f.log) != 0 || st.saves != 0 || st.b != nil || len(res.Removed) != 0 {
		t.Errorf("nada pode mudar nem ser gravado: log=%v saves=%d", f.log, st.saves)
	}
}

func TestApplyTodosComAdminRemovePacoteEDepoisProvisionado(t *testing.T) {
	f := newFake(true)
	f.installed = []Package{pkg("Microsoft.BingWeather")}
	f.provisioned = []Provisioned{prov("Microsoft.BingWeather")}
	st := &memStore{}
	plan, err := Build(context.Background(), f, Selection{Apps: []string{"bing-weather"}, TodosUsuarios: true}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Apply(context.Background(), f, st, plan, t0, Options{}); err != nil {
		t.Fatal(err)
	}
	want := "remove-all:Microsoft.BingWeather_1.2.3.0_x64__8wekyb3d8bbwe,remove-prov:Microsoft.BingWeather_1.2.3.0_neutral_~_8wekyb3d8bbwe"
	if strings.Join(f.log, ",") != want {
		t.Errorf("log\n got %v\nwant %s", f.log, want)
	}
	if len(st.b.Entries) != 2 {
		t.Errorf("backup = %+v", st.b.Entries)
	}
}

func TestApplyDryRunNaoMudaNemGrava(t *testing.T) {
	f := newFake(false) // nem administrador é exigido no dry-run
	f.installed = []Package{pkg("Microsoft.BingWeather")}
	st := &memStore{}
	plan, _ := Build(context.Background(), f, Selection{Apps: []string{"bing-weather"}, TodosUsuarios: true}, 0)
	res, err := Apply(context.Background(), f, st, plan, t0, Options{DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	if !res.DryRun || len(res.Removed) != 0 || len(res.Planned) != 1 || len(f.log) != 0 || st.saves != 0 {
		t.Errorf("res=%+v log=%v saves=%d", res, f.log, st.saves)
	}
}

func TestApplyFalhaAoGravarBackupNaoMudaNada(t *testing.T) {
	f := newFake(false)
	f.installed = []Package{pkg("Microsoft.BingWeather")}
	st := &memStore{failSave: true}
	_, err := Apply(context.Background(), f, st, userPlan(t, f, "bing-weather"), t0, Options{})
	if err == nil || !strings.Contains(err.Error(), "nada foi alterado") {
		t.Errorf("erro = %v", err)
	}
	if len(f.log) != 0 {
		t.Errorf("sem backup não se remove: %v", f.log)
	}
}

func TestApplyRevalidaOPlano(t *testing.T) {
	good := Item{App: "bing-weather", Name: "Microsoft.BingWeather", FullName: "Microsoft.BingWeather_1.2.3.0_x64__8wekyb3d8bbwe",
		FamilyName: "Microsoft.BingWeather_8wekyb3d8bbwe", Version: "1.2.3.0", Publisher: "CN=X", Kind: KindPackage, Scope: ScopeUser}
	mut := func(f func(*Item)) Item { it := good; f(&it); return it }
	cases := map[string]Item{
		"loja": mut(func(i *Item) {
			i.App, i.Name, i.FullName = "windows-store", "Microsoft.WindowsStore", "Microsoft.WindowsStore_1.0.0.0_x64__8wekyb3d8bbwe"
		}),
		"app inexistente": mut(func(i *Item) { i.App = "nao-existe" }),
		"nome de outro app": mut(func(i *Item) {
			i.Name, i.FullName = "Microsoft.BingNews", "Microsoft.BingNews_1.2.3.0_x64__8wekyb3d8bbwe"
		}),
		"webview2": mut(func(i *Item) {
			i.Name, i.FullName = "Microsoft.WebView2Runtime", "Microsoft.WebView2Runtime_1.0.0.0_x64__8wekyb3d8bbwe"
		}),
		"injeção":                        mut(func(i *Item) { i.FullName = "Microsoft.BingWeather_1'; calc #" }),
		"full de outro nome":             mut(func(i *Item) { i.FullName = "Microsoft.WindowsStore_1.0.0.0_x64__8wekyb3d8bbwe" }),
		"framework":                      mut(func(i *Item) { i.IsFramework = true }),
		"não removível":                  mut(func(i *Item) { i.NonRemovable = true }),
		"sistema":                        mut(func(i *Item) { i.SignatureKind = "System" }),
		"kind":                           mut(func(i *Item) { i.Kind = "outro" }),
		"scope":                          mut(func(i *Item) { i.Scope = "" }),
		"provisionado no escopo usuário": mut(func(i *Item) { i.Kind = KindProvisioned }),
		"versão ruim":                    mut(func(i *Item) { i.Version = "1;2" }),
	}
	for name, it := range cases {
		f := newFake(true)
		st := &memStore{}
		plan := &Plan{Scope: it.Scope, IsAdmin: true, Items: []Item{good, it}}
		if it.Scope == "" {
			plan.Scope = ScopeUser
		}
		_, err := Apply(context.Background(), f, st, plan, t0, Options{})
		if !errors.Is(err, ErrItemInvalido) {
			t.Errorf("%s: erro = %v", name, err)
		}
		if len(f.log) != 0 || st.saves != 0 {
			t.Errorf("%s: nada pode mudar nem ser gravado (o item bom também não): %v saves=%d", name, f.log, st.saves)
		}
	}
	if err := ValidateItem(good); err != nil {
		t.Errorf("item bom: %v", err)
	}
}

func TestApplyPrimeiroOriginalVenceENaoRegrava(t *testing.T) {
	f := newFake(false)
	f.installed = []Package{pkg("Microsoft.BingWeather")}
	st := &memStore{}
	if _, err := Apply(context.Background(), f, st, userPlan(t, f, "bing-weather"), t0, Options{}); err != nil {
		t.Fatal(err)
	}
	first := st.b.Entries["package:Microsoft.BingWeather_1.2.3.0_x64__8wekyb3d8bbwe"]
	f.installed = []Package{pkg("Microsoft.BingNews")}
	if _, err := Apply(context.Background(), f, st, userPlan(t, f, "bing-news"), t0.Add(time.Hour), Options{}); err != nil {
		t.Fatal(err)
	}
	if len(st.b.Entries) != 2 || st.b.Entries["package:Microsoft.BingWeather_1.2.3.0_x64__8wekyb3d8bbwe"] != first {
		t.Errorf("backup acumula e preserva o antigo: %+v", st.b.Entries)
	}
}

func TestApplyBackupExistenteAdulteradoNaoMudaNada(t *testing.T) {
	f := newFake(false)
	f.installed = []Package{pkg("Microsoft.BingWeather")}
	st := &memStore{b: &Backup{Version: 1, Entries: map[string]Entry{"package:Microsoft.WindowsStore_1.0.0.0_x64__8wekyb3d8bbwe": {
		ID: "package:Microsoft.WindowsStore_1.0.0.0_x64__8wekyb3d8bbwe", Kind: "package", App: "bing-weather", Name: "Microsoft.WindowsStore",
		PackageFullName: "Microsoft.WindowsStore_1.0.0.0_x64__8wekyb3d8bbwe", Scope: "user"}}}}
	_, err := Apply(context.Background(), f, st, userPlan(t, f, "bing-weather"), t0, Options{})
	if !errors.Is(err, ErrBackupInvalido) || len(f.log) != 0 {
		t.Errorf("err=%v log=%v", err, f.log)
	}
}

func TestApplyBackupDeOutroDonoNaoMudaNada(t *testing.T) {
	dir := t.TempDir()
	other, _ := NewStore(dir, "S-1-5-21-9-9-9-1001")
	if err := other.Save(&Backup{Version: 1, Created: t0, Entries: map[string]Entry{}}); err != nil {
		t.Fatal(err)
	}
	mine, _ := NewStore(dir, "S-1-5-21-1-2-3-1001")
	// copia o arquivo do outro usuário para o nome do meu SID
	data := readFile(t, other.File)
	writeFile(t, mine.File, data)
	f := newFake(false)
	f.installed = []Package{pkg("Microsoft.BingWeather")}
	_, err := Apply(context.Background(), f, mine, userPlan(t, f, "bing-weather"), t0, Options{})
	if !errors.Is(err, ErrBackupOutroDono) || len(f.log) != 0 {
		t.Errorf("err=%v log=%v", err, f.log)
	}
}

func TestApplyPlanoVazioNaoFazNada(t *testing.T) {
	f := newFake(false)
	st := &memStore{}
	res, err := Apply(context.Background(), f, st, userPlan(t, f, "bing-weather"), t0, Options{})
	if err != nil || len(res.Removed) != 0 || st.saves != 0 {
		t.Errorf("res=%+v err=%v saves=%d", res, err, st.saves)
	}
}

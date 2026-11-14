package tui

import (
	"context"
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

type fakeNat struct {
	info      NatInfo
	loadErr   error
	plan      NatPlan
	planErr   error
	applyRes  NatResult
	applyErr  error
	reinst    string
	reinstErr error
	loads     int
	plans     []NatSelection
	applies   []NatSelection
	applyIDs  []string
	reinstN   int
}

func (f *fakeNat) Load(context.Context) (NatInfo, error) { f.loads++; return f.info, f.loadErr }
func (f *fakeNat) Plan(_ context.Context, s NatSelection) (NatPlan, error) {
	f.plans = append(f.plans, s)
	return f.plan, f.planErr
}
func (f *fakeNat) Apply(_ context.Context, s NatSelection, id string) (NatResult, error) {
	f.applies = append(f.applies, s)
	f.applyIDs = append(f.applyIDs, id)
	return f.applyRes, f.applyErr
}
func (f *fakeNat) Reinstall(context.Context) (string, error) {
	f.reinstN++
	return f.reinst, f.reinstErr
}

func natSample(admin bool) NatInfo {
	return NatInfo{Supported: true, OS: "windows", IsAdmin: admin, Apps: []NatApp{
		{ID: "bing-weather", Title: "MSN Clima", Basic: true, Installed: true},
		{ID: "skype", Title: "Skype", Basic: true, Installed: false},
		{ID: "phone-link", Title: "Vínculo com o Celular", Installed: true},
		{ID: "maps", Title: "Mapas", Installed: true},
	}}
}

const natPlanText = "Apps nativos do Windows\nEscopo: somente o usuário atual\n\nSerá removido:\n  - MSN Clima: Microsoft.BingWeather 1.2.3.0\n"

func natRun(tab *NativeAppsTab, cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	switch m := cmd().(type) {
	case nil:
	case tea.BatchMsg:
		for _, c := range m {
			natRun(tab, c)
		}
	default:
		_, next := tab.Update(m)
		natRun(tab, next)
	}
}

func natTab(t *testing.T, be NativeAppsBackend) *NativeAppsTab {
	t.Helper()
	tab := NewNativeAppsTab(context.Background(), be, Theme{Mono: true, NoMotion: true})
	natRun(tab, tab.Init())
	return tab
}

func nkey(tab *NativeAppsTab, keys ...string) {
	for _, k := range keys {
		var msg tea.KeyPressMsg
		switch k {
		case "up":
			msg = tea.KeyPressMsg{Code: tea.KeyUp}
		case "down":
			msg = tea.KeyPressMsg{Code: tea.KeyDown}
		case "esc":
			msg = tea.KeyPressMsg{Code: tea.KeyEscape}
		case "enter":
			msg = tea.KeyPressMsg{Code: tea.KeyEnter}
		case "space":
			msg = tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}
		default:
			msg = tea.KeyPressMsg{Code: rune(k[0]), Text: k}
		}
		_, cmd := tab.Update(msg)
		natRun(tab, cmd)
	}
}

func TestNativeAppsTabIdentidade(t *testing.T) {
	tab := NewNativeAppsTab(context.Background(), &fakeNat{}, Theme{Mono: true})
	if tab.Title() != "Apps nativos" || tab.Short() != "Nativos" {
		t.Fatalf("title=%q short=%q", tab.Title(), tab.Short())
	}
	var _ Tab = tab
	var _ Reloader = tab
}

func TestNativeAppsTabCarregaAssincronoNaoBloqueia(t *testing.T) {
	be := &fakeNat{info: natSample(true)}
	tab := NewNativeAppsTab(context.Background(), be, Theme{Mono: true, NoMotion: true})
	if be.loads != 0 {
		t.Fatal("o construtor não pode ler o sistema")
	}
	if v := tab.View(80, 14); !strings.Contains(v, "Lendo") {
		t.Fatalf("antes da leitura: %q", v)
	}
	cmd := tab.Init()
	if be.loads != 0 {
		t.Fatal("Init só devolve o comando")
	}
	natRun(tab, cmd)
	if be.loads != 1 {
		t.Fatalf("loads=%d", be.loads)
	}
}

func TestNativeAppsTabListaEstadosEPerfis(t *testing.T) {
	tab := natTab(t, &fakeNat{info: natSample(false)})
	v := tab.View(100, 20)
	for _, want := range []string{"MSN Clima", "instalado", "ausente", "basico", "completo", "administrador: não", "não é administrador", "[ ]"} {
		if !strings.Contains(v, want) {
			t.Errorf("faltou %q:\n%s", want, v)
		}
	}
	got := strings.Join(hintLabels(tab), "|")
	for _, want := range []string{"espaço marcar", "p ver plano", "r remover", "b básico", "c completo", "u todos os usuários", "i reinstalar"} {
		if !strings.Contains(got, want) {
			t.Errorf("teclas sem %q: %s", want, got)
		}
	}
}

func TestNativeAppsTabMarcaComEspacoEPerfis(t *testing.T) {
	tab := natTab(t, &fakeNat{info: natSample(true)})
	nkey(tab, "space")
	if got := tab.selection().Apps; len(got) != 1 || got[0] != "bing-weather" {
		t.Fatalf("espaço: %v", got)
	}
	nkey(tab, "down", "down", "space")
	if got := strings.Join(tab.selection().Apps, ","); got != "bing-weather,phone-link" {
		t.Fatalf("seleção individual: %s", got)
	}
	nkey(tab, "space") // desmarca
	if got := strings.Join(tab.selection().Apps, ","); got != "bing-weather" {
		t.Fatalf("desmarcar: %s", got)
	}
	nkey(tab, "b")
	if got := strings.Join(tab.selection().Apps, ","); got != "bing-weather,skype" {
		t.Fatalf("perfil básico: %s", got)
	}
	nkey(tab, "c")
	if len(tab.selection().Apps) != 4 {
		t.Fatalf("perfil completo: %v", tab.selection().Apps)
	}
	if v := tab.View(100, 20); !strings.Contains(v, "[x]") || !strings.Contains(v, "marcados: 4") {
		t.Errorf("marcas não aparecem:\n%s", v)
	}
}

func TestNativeAppsTabTodosUsuariosExigeAdmin(t *testing.T) {
	tab := natTab(t, &fakeNat{info: natSample(false)})
	nkey(tab, "u")
	if tab.allUsers || !strings.Contains(tab.Status(), "exige administrador") || !strings.HasPrefix(tab.Status(), SymWarn) {
		t.Fatalf("sem admin não liga: allUsers=%v status=%q", tab.allUsers, tab.Status())
	}
	adm := natTab(t, &fakeNat{info: natSample(true)})
	nkey(adm, "u")
	if !adm.allUsers || !strings.Contains(strings.Join(hintLabels(adm), "|"), "todos os usuários ✓") || !strings.Contains(adm.View(100, 20), "todos os usuários e o provisionamento") {
		t.Fatalf("com admin liga: %v", hintLabels(adm))
	}
	nkey(adm, "u")
	if adm.allUsers {
		t.Error("segundo u desliga")
	}
}

func TestNativeAppsTabSemMarcaNaoPlaneja(t *testing.T) {
	be := &fakeNat{info: natSample(true)}
	tab := natTab(t, be)
	nkey(tab, "p", "r")
	if len(be.plans) != 0 || tab.Capturing() || !strings.Contains(tab.Status(), "Marque ao menos um app") {
		t.Fatalf("plans=%v status=%q", be.plans, tab.Status())
	}
}

func TestNativeAppsTabVerPlanoNaoMuda(t *testing.T) {
	be := &fakeNat{info: natSample(true), plan: NatPlan{ID: "p1", Text: natPlanText, Pending: 1, IsAdmin: true}}
	tab := natTab(t, be)
	nkey(tab, "space", "p")
	if len(be.plans) != 1 || len(be.applies) != 0 || tab.Capturing() {
		t.Fatalf("plans=%d applies=%d capturing=%v", len(be.plans), len(be.applies), tab.Capturing())
	}
	if v := tab.View(100, 16); !strings.Contains(strings.ToLower(v), "plano de remoção") || !strings.Contains(v, "Microsoft.BingWeather") {
		t.Errorf("plano:\n%s", v)
	}
	if !strings.Contains(tab.Status(), "Só leitura") {
		t.Errorf("status=%q", tab.Status())
	}
	nkey(tab, "esc")
	if tab.view {
		t.Error("Esc volta à lista")
	}
}

func TestNativeAppsTabRemoverPedeConfirmacaoEUsaOIDDoPlano(t *testing.T) {
	be := &fakeNat{info: natSample(true), plan: NatPlan{ID: "plano-7", Text: natPlanText, Pending: 1, IsAdmin: true}, applyRes: NatResult{Removed: 1, BackupPath: `C:\b.json`}}
	tab := natTab(t, be)
	nkey(tab, "space", "r")
	if !tab.Capturing() || !strings.HasSuffix(tab.Status(), "[s/N]") || !strings.Contains(tab.Status(), "1 app(s)") {
		t.Fatalf("confirmação: capturing=%v status=%q", tab.Capturing(), tab.Status())
	}
	if len(be.applies) != 0 {
		t.Fatal("removeu antes da confirmação")
	}
	nkey(tab, "s")
	if len(be.applies) != 1 || be.applyIDs[0] != "plano-7" || strings.Join(be.applies[0].Apps, ",") != "bing-weather" {
		t.Fatalf("applies=%v ids=%v", be.applies, be.applyIDs)
	}
	if tab.Capturing() {
		t.Error("terminou: não captura mais")
	}
	if !strings.Contains(tab.Status(), "Removidos: 1 app(s)") || !strings.Contains(tab.Status(), "tecle i") || !strings.Contains(tab.View(120, 16), `C:\b.json`) {
		t.Errorf("resultado: status=%q view=%s", tab.Status(), tab.View(120, 16))
	}
	if be.loads < 2 {
		t.Error("depois de remover o estado é relido")
	}
	if len(tab.selection().Apps) != 0 {
		t.Error("as marcas saem depois de remover")
	}
}

func TestNativeAppsTabSoSConfirma(t *testing.T) {
	for _, k := range []string{"n", "N", "enter", "esc", "x", "q", "1", "tab", "space"} {
		be := &fakeNat{info: natSample(true), plan: NatPlan{ID: "p", Text: natPlanText, Pending: 1, IsAdmin: true}}
		tab := natTab(t, be)
		nkey(tab, "space", "r", k)
		if len(be.applies) != 0 {
			t.Errorf("tecla %q removeu", k)
		}
		if tab.Capturing() || !strings.Contains(tab.Status(), "Cancelado") {
			t.Errorf("tecla %q: capturing=%v status=%q", k, tab.Capturing(), tab.Status())
		}
	}
	be := &fakeNat{info: natSample(true), plan: NatPlan{ID: "p", Text: natPlanText, Pending: 1, IsAdmin: true}}
	tab := natTab(t, be)
	nkey(tab, "space", "r", "y")
	if len(be.applies) != 1 {
		t.Error("y confirma")
	}
}

func TestNativeAppsTabRolarNaConfirmacaoNaoCancela(t *testing.T) {
	be := &fakeNat{info: natSample(true), plan: NatPlan{ID: "p", Text: natPlanText, Pending: 1, IsAdmin: true}}
	tab := natTab(t, be)
	nkey(tab, "space", "r", "down", "up")
	if !tab.asking {
		t.Fatal("rolar não cancela a confirmação")
	}
}

func TestNativeAppsTabSemAdminRecusaAntesDeConfirmar(t *testing.T) {
	be := &fakeNat{info: natSample(false), plan: NatPlan{ID: "p", Text: natPlanText, Pending: 1, NeedsAdmin: true}}
	tab := natTab(t, be)
	nkey(tab, "space", "r")
	if tab.Capturing() || !strings.Contains(tab.Status(), "exigem administrador") || !strings.HasPrefix(tab.Status(), SymWarn) {
		t.Fatalf("status=%q", tab.Status())
	}
	nkey(tab, "esc", "s")
	if len(be.applies) != 0 {
		t.Fatal("nada pode mudar")
	}
}

func TestNativeAppsTabNadaARemover(t *testing.T) {
	be := &fakeNat{info: natSample(true), plan: NatPlan{ID: "p", Text: natPlanText, Pending: 0, IsAdmin: true}}
	tab := natTab(t, be)
	nkey(tab, "down", "space", "r")
	if tab.Capturing() || !strings.Contains(tab.Status(), "Nada a remover") {
		t.Errorf("capturing=%v status=%q", tab.Capturing(), tab.Status())
	}
}

func TestNativeAppsTabErrosDoPlanoEDaRemocao(t *testing.T) {
	be := &fakeNat{info: natSample(true), planErr: errors.New("powershell falhou")}
	tab := natTab(t, be)
	nkey(tab, "space", "r")
	if !strings.Contains(tab.Status(), "erro: powershell falhou") || tab.Capturing() {
		t.Errorf("status=%q", tab.Status())
	}
	cases := []struct {
		err  error
		res  NatResult
		want string
	}{
		{ErrNatStalePlan, NatResult{}, "mudou desde o plano mostrado"},
		{ErrNatNeedsAdmin, NatResult{}, "Precisa de administrador"},
		{errors.New("boom"), NatResult{Removed: 2, BackupPath: "b.json"}, "erro: boom"},
	}
	for _, c := range cases {
		be := &fakeNat{info: natSample(true), plan: NatPlan{ID: "p", Text: natPlanText, Pending: 1, IsAdmin: true}, applyErr: c.err, applyRes: c.res}
		tab := natTab(t, be)
		nkey(tab, "space", "r", "s")
		if !strings.Contains(tab.Status(), c.want) || !strings.HasPrefix(tab.Status(), SymWarn) {
			t.Errorf("%v: status=%q", c.err, tab.Status())
		}
		if c.res.Removed > 0 && !strings.Contains(tab.View(120, 16), "Removidos antes da falha: 2") {
			t.Errorf("falha parcial não informa o que saiu:\n%s", tab.View(120, 16))
		}
	}
}

func TestNativeAppsTabComoReinstalarSoMostraTexto(t *testing.T) {
	be := &fakeNat{info: natSample(true), reinst: "Reinstalar apps nativos removidos\n- MSN Clima\n    1. winget install --id X --source msstore\n"}
	tab := natTab(t, be)
	nkey(tab, "i") // não exige marcas
	if be.reinstN != 1 || len(be.applies) != 0 || tab.Capturing() {
		t.Fatalf("reinstN=%d applies=%d", be.reinstN, len(be.applies))
	}
	if v := tab.View(100, 12); !strings.Contains(strings.ToLower(v), "como reinstalar") || !strings.Contains(v, "winget install") {
		t.Errorf("texto:\n%s", v)
	}
	if !strings.Contains(tab.Status(), "não reinstala nada") {
		t.Errorf("status=%q", tab.Status())
	}
	nkey(tab, "esc")
	be.reinstErr = ErrNatNoBackup
	nkey(tab, "i")
	if tab.view || !strings.Contains(tab.Status(), "nada a reinstalar") {
		t.Errorf("sem backup: view=%v status=%q", tab.view, tab.Status())
	}
}

func TestNativeAppsTabForaDoWindowsSemTeclasDeMudanca(t *testing.T) {
	be := &fakeNat{info: NatInfo{Supported: false, OS: "darwin", Reason: "No macOS os apps do sistema ficam em um volume protegido."}}
	tab := natTab(t, be)
	v := tab.View(100, 14)
	if !strings.Contains(v, "Os apps nativos só existem no Windows") || !strings.Contains(v, "darwin") || !strings.Contains(v, "volume protegido") {
		t.Fatalf("view: %q", v)
	}
	if len(tab.Keys()) != 0 {
		t.Errorf("teclas fora do Windows: %v", tab.Keys())
	}
	nkey(tab, "space", "b", "c", "u", "p", "r", "i", "s")
	if len(be.plans)+len(be.applies)+be.reinstN != 0 || tab.Capturing() || len(tab.marked) != 0 {
		t.Error("fora do Windows nenhuma tecla faz nada")
	}
}

func TestNativeAppsTabErroDeLeituraEVazio(t *testing.T) {
	tab := natTab(t, &fakeNat{loadErr: errors.New("sem powershell")})
	if v := tab.View(80, 10); !strings.Contains(v, "erro:") || !strings.Contains(v, "sem powershell") {
		t.Errorf("erro: %q", v)
	}
	nkey(tab, "r", "i", "space")
	empty := natTab(t, &fakeNat{info: NatInfo{Supported: true, OS: "windows", IsAdmin: true}})
	if v := empty.View(80, 10); !strings.Contains(v, "Nenhum app no catálogo") {
		t.Errorf("vazio: %q", v)
	}
	nkey(empty, "space", "down", "b", "p")
}

func TestNativeAppsTabEstreitoEAltura(t *testing.T) {
	be := &fakeNat{info: natSample(false), plan: NatPlan{ID: "p", Text: natPlanText, Pending: 1}}
	tab := natTab(t, be)
	for _, w := range []int{20, 40, 60} {
		for _, l := range strings.Split(tab.View(w, 6), "\n") {
			if lipgloss.Width(l) > w {
				t.Errorf("w=%d linha larga: %q", w, l)
			}
		}
	}
	// o cursor sempre fica visível, mesmo com a janela baixa
	nkey(tab, "down", "down", "down")
	if v := tab.View(60, 4); !strings.Contains(v, "> ") {
		t.Errorf("cursor fora da janela:\n%s", v)
	}
	nkey(tab, "space", "p")
	for _, l := range strings.Split(tab.View(24, 8), "\n") {
		if lipgloss.Width(l) > 24 {
			t.Errorf("plano estreito: %q", l)
		}
	}
}

func TestNativeAppsTabTextoExternoSanitizado(t *testing.T) {
	info := natSample(true)
	info.Apps[0].Title = "app\x1b]0;x\x07"
	be := &fakeNat{info: info, plan: NatPlan{ID: "p", Text: "alvo\x1b[2J\n", Pending: 1, IsAdmin: true}, applyRes: NatResult{Removed: 1, BackupPath: "C:\\b\x1b[31m"}, reinst: "x\x1b[2J"}
	tab := natTab(t, be)
	v := tab.View(100, 16)
	nkey(tab, "space", "p")
	v += tab.View(100, 16)
	nkey(tab, "esc", "r", "s")
	v += tab.View(100, 16) + tab.Status()
	nkey(tab, "i")
	v += tab.View(100, 16)
	if strings.ContainsRune(v, 0x1b) || strings.ContainsRune(v, 7) {
		t.Errorf("controle vazou: %q", v)
	}
}

func TestNativeAppsTabPlanoAtrasadoEhIgnorado(t *testing.T) {
	tab := natTab(t, &fakeNat{info: natSample(true)})
	tab.Update(natPlanMsg{seq: 99, kind: natRemove, plan: NatPlan{ID: "x", Text: "x", Pending: 1, IsAdmin: true}})
	if tab.Capturing() || tab.view {
		t.Error("plano de outra consulta não pode abrir a confirmação")
	}
}

// blockNat bloqueia Plan até o teste liberar (resposta tardia).
type blockNat struct {
	fakeNat
	release chan struct{}
	started chan struct{}
}

func (b *blockNat) Plan(ctx context.Context, s NatSelection) (NatPlan, error) {
	b.started <- struct{}{}
	<-b.release
	return b.fakeNat.Plan(ctx, s)
}

func natFirstPlanMsg(cmd tea.Cmd) tea.Msg {
	if cmd == nil {
		return nil
	}
	switch m := cmd().(type) {
	case tea.BatchMsg:
		for _, c := range m {
			if r := c(); r != nil {
				if _, ok := r.(natPlanMsg); ok {
					return r
				}
			}
		}
		return nil
	default:
		return m
	}
}

// Esc durante a preparação do plano cancela; a resposta tardia não abre a confirmação nem muda nada.
func TestNativeAppsEscDuranteOPlanoDescartaARespostaTardia(t *testing.T) {
	for _, key := range []string{"r", "p"} {
		be := &blockNat{fakeNat: fakeNat{info: natSample(true), plan: NatPlan{ID: "tarde", Text: natPlanText, Pending: 1, IsAdmin: true}}, release: make(chan struct{}), started: make(chan struct{}, 1)}
		tab := NewNativeAppsTab(context.Background(), be, Theme{Mono: true, NoMotion: true})
		natRun(tab, tab.Init())
		nkey(tab, "space")
		_, cmd := tab.Update(tea.KeyPressMsg{Code: rune(key[0]), Text: key})
		done := make(chan tea.Msg, 1)
		go func() { done <- natFirstPlanMsg(cmd) }()
		<-be.started
		pctx := tab.cancel
		if pctx == nil {
			t.Fatal("a preparação deve ter contexto cancelável")
		}
		tab.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
		if tab.busy != natBusyNone || !strings.Contains(tab.Status(), "cancelada") {
			t.Fatalf("%s: Esc deve cancelar a preparação: busy=%v status=%q", key, tab.busy, tab.Status())
		}
		close(be.release)
		if m := <-done; m != nil {
			tab.Update(m)
		}
		if tab.asking || tab.view {
			t.Fatalf("%s: a resposta tardia não pode abrir a confirmação (asking=%v view=%v)", key, tab.asking, tab.view)
		}
		if len(be.applies) != 0 {
			t.Fatalf("%s: nada pode mudar", key)
		}
	}
}

func TestNativeAppsTabCapturaTeclasDaRaiz(t *testing.T) {
	be := &fakeNat{info: natSample(true), plan: NatPlan{ID: "p", Text: natPlanText, Pending: 1, IsAdmin: true}}
	tab := natTab(t, be)
	nkey(tab, "space", "r")
	if !tab.Capturing() {
		t.Fatal("a confirmação captura as teclas (q, Tab e números não saem)")
	}
}

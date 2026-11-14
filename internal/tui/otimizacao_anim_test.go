package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/PiluTech/toolkit/internal/diagfmt"
)

type fakeAnim struct {
	info     AnimInfo
	loadErr  error
	plan     AnimPlan
	planErr  error
	undoPlan AnimPlan
	undoErr  error
	applyRes AnimResult
	applyErr error
	undoRes  AnimResult
	undoRunE error
	loads    int
	applies  int
	undos    int
	seq      int
	lastID   string
	applyIDs []string
	hold     chan struct{} // se não nil, Plan espera (ou o contexto cair)
}

func (f *fakeAnim) issue() string {
	f.seq++
	f.lastID = fmt.Sprintf("plano-anim-%d", f.seq)
	return f.lastID
}
func (f *fakeAnim) Load(context.Context) (AnimInfo, error) { f.loads++; return f.info, f.loadErr }
func (f *fakeAnim) Plan(ctx context.Context) (AnimPlan, error) {
	if f.hold != nil {
		select {
		case <-f.hold:
		case <-ctx.Done():
			return AnimPlan{}, ctx.Err()
		}
	}
	pl := f.plan
	pl.ID = f.issue()
	return pl, f.planErr
}
func (f *fakeAnim) Apply(_ context.Context, id string) (AnimResult, error) {
	f.applyIDs = append(f.applyIDs, id)
	if id != f.lastID {
		return AnimResult{}, ErrAnimStalePlan
	}
	f.applies++
	return f.applyRes, f.applyErr
}
func (f *fakeAnim) UndoPlan(context.Context) (AnimPlan, error) {
	pl := f.undoPlan
	pl.ID = f.issue()
	return pl, f.undoErr
}
func (f *fakeAnim) Undo(_ context.Context, id string) (AnimResult, error) {
	f.applyIDs = append(f.applyIDs, id)
	if id != f.lastID {
		return AnimResult{}, ErrAnimStalePlan
	}
	f.undos++
	return f.undoRes, f.undoRunE
}

const animPlanText = "Reduzir animações do Windows (EXPERIMENTAL)\n\nAnimação de janelas ao minimizar e maximizar desligada\n  As janelas minimizam e maximizam sem animação.\n  ! HKCU\\Control Panel\\Desktop\\WindowMetrics\\MinAnimate: \"1\" -> \"0\"\n      desfazer: reg add x\n\nAlgumas animações só valem depois de reabrir a sessão do Windows ou o Explorer.\n\n1 mudança(s) pendente(s).\n"

func animSample() AnimInfo {
	return AnimInfo{Items: []AnimItem{{Title: "Animação de janelas ao minimizar e maximizar desligada", State: AnimPending}}}
}

func newAnimTab(t *testing.T, be *fakeAnim) *OptimizeTab {
	t.Helper()
	tab := NewOptimizeTab(context.Background(), &fakeOtim{rep: otimReport()}, Theme{Mono: true, NoMotion: true}, diagfmt.PT, "windows")
	tab.SetAnimations(be)
	return tab
}

func animRun(tab *OptimizeTab, cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	switch m := cmd().(type) {
	case nil:
	case tea.BatchMsg:
		for _, c := range m {
			animRun(tab, c)
		}
	default:
		_, next := tab.Update(m)
		animRun(tab, next)
	}
}

func akey(tab *OptimizeTab, keys ...string) {
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
		default:
			msg = tea.KeyPressMsg{Code: rune(k[0]), Text: k}
		}
		_, cmd := tab.Update(msg)
		animRun(tab, cmd)
	}
}

// animTab abre a parte Animações (m) com o estado lido.
func animTab(t *testing.T, be *fakeAnim) *OptimizeTab {
	t.Helper()
	tab := newAnimTab(t, be)
	akey(tab, "m")
	return tab
}

func TestAnimSemBackendASecaoSoTemGargalos(t *testing.T) {
	tab := newOtim(&fakeOtim{rep: otimReport()}, "windows")
	for _, h := range tab.Keys() {
		if h.Key == "m" {
			t.Fatal("sem backend de animações não há tecla m")
		}
	}
	okey(tab, "m")
	if tab.inAnim() || tab.Result() != nil {
		t.Fatal("m não faz nada sem backend")
	}
}

func TestAnimMAbreAParteELeSoAoEntrar(t *testing.T) {
	be := &fakeAnim{info: animSample()}
	tab := newAnimTab(t, be)
	if be.loads != 0 {
		t.Fatal("nada é lido antes de abrir a parte")
	}
	akey(tab, "m")
	if be.loads != 1 || !tab.inAnim() {
		t.Fatalf("loads=%d", be.loads)
	}
	v := tab.View(100, 24)
	for _, want := range []string{"Animações do Windows", "experimental", "pendente", "Sem backup", "sensação"} {
		if !strings.Contains(strings.ToLower(v), strings.ToLower(want)) {
			t.Errorf("faltou %q:\n%s", want, v)
		}
	}
	akey(tab, "m")
	if tab.inAnim() {
		t.Fatal("m de novo volta aos gargalos")
	}
}

func TestAnimGargalosContinuaFuncionando(t *testing.T) {
	be := &fakeAnim{info: animSample()}
	tab := newAnimTab(t, be)
	otimSettle(t, tab, okey(tab, "c"))
	if !strings.Contains(strings.Join(tab.lines, "\n"), "== CPU ==") {
		t.Fatal("c lê os gargalos")
	}
	akey(tab, "m", "m")
	if !tab.loaded || !strings.Contains(tab.View(100, 20), "Otimização do Windows") {
		t.Fatal("a leitura de gargalos continua na tela depois de voltar")
	}
}

func TestAnimVerPlanoNaoMudaNada(t *testing.T) {
	be := &fakeAnim{info: animSample(), plan: AnimPlan{Text: animPlanText, Pending: 1}}
	tab := animTab(t, be)
	akey(tab, "p")
	if tab.a.asking || !tab.a.view || be.applies != 0 {
		t.Fatalf("p só mostra: asking=%v view=%v", tab.a.asking, tab.a.view)
	}
	v := tab.View(100, 24)
	if !strings.Contains(v, `"1" -> "0"`) || strings.Contains(v, "desfazer: reg add") {
		t.Fatalf("plano:\n%s", v)
	}
	akey(tab, "esc")
	if tab.a.view {
		t.Fatal("Esc volta")
	}
}

func TestAnimAplicarPedeConfirmacaoComFocoEmCancelar(t *testing.T) {
	be := &fakeAnim{info: animSample(), plan: AnimPlan{Text: animPlanText, Pending: 1}, applyRes: AnimResult{Applied: 1, BackupPath: `C:\x\bk.json`}}
	tab := animTab(t, be)
	akey(tab, "a")
	if !tab.a.asking || be.applies != 0 || !tab.Capturing() {
		t.Fatal("a abre a confirmação e captura as teclas")
	}
	if tab.a.conf.Focus != 0 || tab.a.conf.Buttons[0].Label != "Cancelar" {
		t.Fatal("o foco começa em Cancelar")
	}
	v := tab.View(100, 30)
	for _, want := range []string{"experimental", "Backup:", "Como desfazer:", "reabrir", "HKCU"} {
		if !strings.Contains(v, want) {
			t.Errorf("faltou %q:\n%s", want, v)
		}
	}
	akey(tab, "enter") // Enter em Cancelar cancela
	if be.applies != 0 || tab.a.asking {
		t.Fatal("Enter em Cancelar não aplica")
	}
	akey(tab, "a", "esc")
	if be.applies != 0 {
		t.Fatal("Esc cancela")
	}
	akey(tab, "a", "s")
	if be.applies != 1 {
		t.Fatalf("s confirma: applies=%d", be.applies)
	}
	r := tab.Result()
	if r == nil || r.Kind != ResultOK || !strings.Contains(strings.Join(r.Lines, "\n"), "bk.json") || !strings.Contains(strings.Join(r.Lines, "\n"), "reabrir") {
		t.Fatalf("faixa de resultado: %+v", r)
	}
	tab.Dismiss()
	if tab.Result() != nil {
		t.Fatal("x fecha a faixa")
	}
}

func TestAnimPlanoDeUsoUnicoPorID(t *testing.T) {
	be := &fakeAnim{info: animSample(), plan: AnimPlan{Text: animPlanText, Pending: 1}}
	tab := animTab(t, be)
	akey(tab, "a", "s")
	if len(be.applyIDs) != 1 || be.applyIDs[0] != be.lastID {
		t.Fatalf("a confirmação usa o ID do plano EXIBIDO: %v (último %s)", be.applyIDs, be.lastID)
	}
}

func TestAnimPlanoAtrasadoNaoSubstituiOExibido(t *testing.T) {
	// A bloqueado -> Esc -> B -> A termina tarde: o ID aplicado é o de B
	be := &fakeAnim{info: animSample(), plan: AnimPlan{Text: animPlanText, Pending: 1}, hold: make(chan struct{})}
	tab := animTab(t, be)
	_, cmdA := tab.Update(tea.KeyPressMsg{Code: 'a', Text: "a"})
	akey(tab, "esc")
	be.hold = nil
	akey(tab, "a") // B
	idB := be.lastID
	// A termina tarde (contexto cancelado => erro): resposta descartada pela seq
	be.hold = make(chan struct{})
	close(be.hold)
	animRun(tab, cmdA)
	akey(tab, "s")
	if len(be.applyIDs) != 1 || be.applyIDs[0] != idB {
		t.Fatalf("aplicou %v, queria só %s", be.applyIDs, idB)
	}
}

func TestAnimEscCancelaAPreparacao(t *testing.T) {
	be := &fakeAnim{info: animSample(), plan: AnimPlan{Text: animPlanText, Pending: 1}, hold: make(chan struct{})}
	tab := animTab(t, be)
	_, cmd := tab.Update(tea.KeyPressMsg{Code: 'a', Text: "a"})
	if tab.a.busy != busyPlan {
		t.Fatal("preparando")
	}
	akey(tab, "esc")
	if tab.a.busy != busyNone || tab.a.asking || be.applies != 0 || !strings.Contains(tab.Status(), "cancelada") {
		t.Fatalf("Esc cancela: %q", tab.Status())
	}
	close(be.hold)
	animRun(tab, cmd)
	if tab.a.asking || tab.a.view {
		t.Fatal("o plano atrasado não pode abrir a confirmação")
	}
}

func TestAnimPlanoMudouNaoAlteraENaoPrometeBackup(t *testing.T) {
	be := &fakeAnim{info: animSample(), plan: AnimPlan{Text: animPlanText, Pending: 1}, applyErr: ErrAnimStalePlan}
	tab := animTab(t, be)
	akey(tab, "a", "s")
	r := tab.Result()
	if r == nil || r.Kind != ResultInfo || !strings.Contains(strings.Join(r.Lines, " "), "nada foi alterado") {
		t.Fatalf("%+v", r)
	}
}

func TestAnimErroNoMeioMostraParcialEBackup(t *testing.T) {
	be := &fakeAnim{info: animSample(), plan: AnimPlan{Text: animPlanText, Pending: 1}, applyRes: AnimResult{Applied: 1, BackupPath: `C:\x\bk.json`}, applyErr: errors.New("falhou o segundo")}
	tab := animTab(t, be)
	akey(tab, "a", "s")
	r := tab.Result()
	if r == nil || r.Kind != ResultPartial || !strings.Contains(strings.Join(r.Lines, "\n"), "bk.json") {
		t.Fatalf("%+v", r)
	}
}

func TestAnimErroSemMudancaNaoPrometeBackup(t *testing.T) {
	be := &fakeAnim{info: animSample(), plan: AnimPlan{Text: animPlanText, Pending: 1}, applyErr: errors.New("registro negou")}
	tab := animTab(t, be)
	akey(tab, "a", "s")
	r := tab.Result()
	if r == nil || r.Kind != ResultError || strings.Contains(strings.Join(r.Lines, " "), "Backup em") {
		t.Fatalf("%+v", r)
	}
}

func TestAnimReleituraFalhandoDepoisDeMudarNaoDizNadaFoiAlterado(t *testing.T) {
	be := &fakeAnim{info: animSample(), plan: AnimPlan{Text: animPlanText, Pending: 1}, applyRes: AnimResult{Applied: 1, BackupPath: `C:\x\bk.json`}}
	tab := animTab(t, be)
	be.loadErr = errors.New("registro ilegível")
	akey(tab, "a", "s")
	v := tab.View(100, 24)
	if strings.Contains(strings.ToLower(v), "nada foi alterado") || strings.Contains(strings.ToLower(v), "backup em") {
		t.Fatalf("não pode negar a mudança nem prometer backup:\n%s", v)
	}
	if !strings.Contains(v, "registro ilegível") {
		t.Fatalf("mostra o erro:\n%s", v)
	}
}

func TestAnimDesfazerPedeConfirmacao(t *testing.T) {
	be := &fakeAnim{info: AnimInfo{Items: animSample().Items, HasBackup: true}, undoPlan: AnimPlan{Text: "O que será restaurado:\n  ! HKCU\\x: \"0\" -> \"1\"\n", Pending: 1}, undoRes: AnimResult{Applied: 1}}
	tab := animTab(t, be)
	akey(tab, "d")
	if !tab.a.asking || be.undos != 0 || tab.a.conf.Buttons[1].Label != "Restaurar" {
		t.Fatal("d abre a confirmação")
	}
	akey(tab, "s")
	if be.undos != 1 || tab.Result() == nil || tab.Result().Kind != ResultOK {
		t.Fatalf("undos=%d res=%+v", be.undos, tab.Result())
	}
}

func TestAnimDesfazerSemBackup(t *testing.T) {
	be := &fakeAnim{info: animSample(), undoErr: ErrAnimNoBackup}
	tab := animTab(t, be)
	akey(tab, "d")
	if tab.a.asking || !strings.Contains(tab.Status(), "Não há backup") {
		t.Fatalf("status=%q", tab.Status())
	}
}

func TestAnimBackupPedeRemocaoQuandoTudoJaEstaComoAntes(t *testing.T) {
	be := &fakeAnim{info: animSample(), undoPlan: AnimPlan{Text: "nada\n", Pending: 0}, undoRes: AnimResult{Nothing: true}}
	tab := animTab(t, be)
	akey(tab, "d")
	if tab.a.conf == nil || tab.a.conf.Buttons[1].Label != "Remover backup" {
		t.Fatal("confirma a remoção do backup")
	}
	akey(tab, "s")
	if r := tab.Result(); r == nil || r.Kind != ResultInfo {
		t.Fatalf("%+v", r)
	}
}

func TestAnimTudoAplicadoNaoPedeConfirmacao(t *testing.T) {
	be := &fakeAnim{info: animSample(), plan: AnimPlan{Text: "ok\n", Pending: 0}}
	tab := animTab(t, be)
	akey(tab, "a")
	if tab.a.asking || !strings.Contains(tab.Status(), "Nada a fazer") {
		t.Fatalf("status=%q", tab.Status())
	}
}

func TestAnimErroDeLeituraEC(t *testing.T) {
	be := &fakeAnim{loadErr: errors.New("reg.exe negou")}
	tab := animTab(t, be)
	v := tab.View(80, 20)
	if !strings.Contains(v, "reg.exe negou") || !strings.Contains(v, "Tente de novo") {
		t.Fatalf("\n%s", v)
	}
	be.loadErr, be.info = nil, animSample()
	akey(tab, "c")
	if !strings.Contains(tab.View(80, 20), "pendente") {
		t.Fatal("c lê de novo")
	}
}

func TestAnimForaDoWindowsNaoTemParte(t *testing.T) {
	tab := NewOptimizeTab(context.Background(), &fakeOtim{}, Theme{Mono: true}, diagfmt.PT, "darwin")
	be := &fakeAnim{}
	tab.SetAnimations(be)
	akey(tab, "m", "a")
	if be.loads != 0 || tab.inAnim() || len(tab.Keys()) != 0 {
		t.Fatal("fora do Windows nada é lido nem oferecido")
	}
	if !strings.Contains(tab.View(80, 16), "só existe no Windows") {
		t.Fatal("estado só Windows")
	}
}

func TestAnimTeclasEAjudaUsamOVocabularioUnico(t *testing.T) {
	tab := animTab(t, &fakeAnim{info: animSample()})
	var got []string
	for _, h := range tab.Keys() {
		got = append(got, h.Key+"="+h.Label)
	}
	want := "p=ver plano a=aplicar… d=desfazer… c=ler de novo m=gargalos"
	if strings.Join(got, " ") != want {
		t.Fatalf("barra: %v", got)
	}
}

func TestAnimDetalheFalaDoExperimentalEDoBackup(t *testing.T) {
	tab := animTab(t, &fakeAnim{info: animSample()})
	d := tab.Detail(44, 40)
	for _, want := range []string{"experimental", "MinAnimate", "backup", "Explorer"} {
		if !strings.Contains(strings.ToLower(d), strings.ToLower(want)) {
			t.Errorf("faltou %q:\n%s", want, d)
		}
	}
}

func TestGoldenOtimizacaoAnimacoes(t *testing.T) {
	for _, sz := range [][2]int{{80, 24}, {120, 40}, {60, 16}} {
		be := &fakeAnim{info: animSample(), plan: AnimPlan{Text: animPlanText, Pending: 1}, applyRes: AnimResult{Applied: 1, BackupPath: `C:\Users\ana\AppData\Local\toolkit\otimizacao-animacoes-backup-S-1-5-21-1-2-3-1001.json`}}
		tab := animTab(t, be)
		golden(t, fmt.Sprintf("otimizacao_anim_%dx%d", sz[0], sz[1]), sized(goldenOpts(), sz[0], sz[1], tab).render())
		akey(tab, "p")
		golden(t, fmt.Sprintf("otimizacao_anim_plano_%dx%d", sz[0], sz[1]), sized(goldenOpts(), sz[0], sz[1], tab).render())
		akey(tab, "esc", "a")
		golden(t, fmt.Sprintf("otimizacao_anim_confirma_%dx%d", sz[0], sz[1]), sized(goldenOpts(), sz[0], sz[1], tab).render())
		akey(tab, "s")
		golden(t, fmt.Sprintf("otimizacao_anim_ok_%dx%d", sz[0], sz[1]), sized(goldenOpts(), sz[0], sz[1], tab).render())
	}
}

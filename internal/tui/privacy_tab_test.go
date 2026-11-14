package tui

import (
	"context"
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

type fakePriv struct {
	info      PrivInfo
	loadErr   error
	plan      PrivPlan
	planErr   error
	undoPlan  PrivPlan
	undoErr   error
	applyRes  PrivResult
	applyErr  error
	undoRes   PrivResult
	undoRunE  error
	loads     int
	plans     []string
	applies   []string
	undoCalls int
	seq       int
	lastID    string   // ID do ÚLTIMO plano emitido (o exibido)
	applyIDs  []string // IDs recebidos pelo Apply/Undo (a confirmação deve usar o do plano exibido)
}

func (f *fakePriv) Load(context.Context) (PrivInfo, error) { f.loads++; return f.info, f.loadErr }
func (f *fakePriv) Plan(_ context.Context, p PrivProfile, only bool) (PrivPlan, error) {
	f.plans = append(f.plans, string(p)+map[bool]string{true: "+user", false: ""}[only])
	pl := f.plan
	pl.ID = f.issue()
	return pl, f.planErr
}

func (f *fakePriv) issue() string {
	f.seq++
	f.lastID = "plano-" + string(rune('0'+f.seq%10)) + "-" + string(rune('a'+f.seq%26))
	return f.lastID
}
func (f *fakePriv) Apply(_ context.Context, p PrivProfile, only bool, id string) (PrivResult, error) {
	f.applyIDs = append(f.applyIDs, id)
	if id != f.lastID {
		return PrivResult{}, ErrPrivStalePlan // a confirmação tem de usar o ID do plano EXIBIDO
	}
	f.applies = append(f.applies, string(p)+map[bool]string{true: "+user", false: ""}[only])
	return f.applyRes, f.applyErr
}
func (f *fakePriv) UndoPlan(context.Context) (PrivPlan, error) {
	pl := f.undoPlan
	pl.ID = f.issue()
	return pl, f.undoErr
}
func (f *fakePriv) Undo(_ context.Context, id string) (PrivResult, error) {
	f.applyIDs = append(f.applyIDs, id)
	if id != f.lastID {
		return PrivResult{}, ErrPrivStalePlan
	}
	f.undoCalls++
	return f.undoRes, f.undoRunE
}

func privSample(admin bool) PrivInfo {
	return PrivInfo{Supported: true, OS: "windows", Edition: "pro", IsAdmin: admin, Settings: []PrivSetting{
		{Title: "ID de publicidade desligado", State: PrivApplied},
		{Title: "Sugestões no menu Iniciar desligadas", State: PrivPending},
		{Title: "Dados de diagnóstico no mínimo", Admin: true, State: PrivPending},
		{Title: "Serviço de telemetria", Admin: true, State: PrivPartial},
	}}
}

const planText = "Perfil: basico\n\nID de publicidade desligado\n  ! HKCU\\x\\Enabled: 1 -> 0\n      desfazer: reg add\n"

// run executa o comando (e os de um Batch) e entrega as mensagens à aba, como o Bubble Tea faria.
func privRun(tab *PrivacyTab, cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	switch m := cmd().(type) {
	case nil:
	case tea.BatchMsg:
		for _, c := range m {
			privRun(tab, c)
		}
	default:
		_, next := tab.Update(m)
		privRun(tab, next)
	}
}

func privTab(t *testing.T, be *fakePriv) *PrivacyTab {
	t.Helper()
	tab := NewPrivacyTab(context.Background(), be, Theme{Mono: true, NoMotion: true})
	privRun(tab, tab.Init())
	return tab
}

func pkey(tab *PrivacyTab, keys ...string) {
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
		privRun(tab, cmd)
	}
}

func TestPrivacyTabIdentidade(t *testing.T) {
	tab := NewPrivacyTab(context.Background(), &fakePriv{}, Theme{Mono: true})
	if tab.Title() != "Privacidade" || tab.Short() != "Priv" {
		t.Fatalf("title=%q short=%q", tab.Title(), tab.Short())
	}
	var _ Tab = tab
	var _ Reloader = tab
}

func TestPrivacyTabCarregaAssincronoNaoBloqueia(t *testing.T) {
	be := &fakePriv{info: privSample(true)}
	tab := NewPrivacyTab(context.Background(), be, Theme{Mono: true, NoMotion: true})
	if be.loads != 0 {
		t.Fatal("o construtor não pode ler o sistema")
	}
	if v := tab.View(80, 14); !strings.Contains(v, "Lendo") {
		t.Fatalf("antes da leitura: %q", v)
	}
	cmd := tab.Init()
	if be.loads != 0 {
		t.Fatal("Init só devolve o comando; a leitura roda fora do Update")
	}
	privRun(tab, cmd)
	if be.loads != 1 {
		t.Fatalf("loads=%d", be.loads)
	}
}

func TestPrivacyTabListaEstados(t *testing.T) {
	tab := privTab(t, &fakePriv{info: privSample(false)})
	v := tab.View(100, 20)
	for _, want := range []string{"basico", "recomendado", "aplicado", "pendente", "ID de publicidade", "administrador: não", "m aplica só"} {
		if !strings.Contains(v, want) {
			t.Errorf("faltou %q:\n%s", want, v)
		}
	}
	if strings.Contains(v, "telemetria") {
		t.Error("o perfil básico não lista ajustes de administrador")
	}
	pkey(tab, "down")
	v = tab.View(100, 20)
	for _, want := range []string{"parcial", "[admin]", "Serviço de telemetria"} {
		if !strings.Contains(v, want) {
			t.Errorf("recomendado: faltou %q:\n%s", want, v)
		}
	}
	if !strings.Contains(strings.Join(hintLabels(tab), "|"), "ver plano") {
		t.Errorf("teclas: %v", tab.Keys())
	}
}

func hintLabels(tab Tab) []string {
	var out []string
	for _, k := range tab.Keys() {
		out = append(out, k.Key+" "+k.Label)
	}
	return out
}

func TestPrivacyTabAdminSemAvisoQuandoAdmin(t *testing.T) {
	tab := privTab(t, &fakePriv{info: privSample(true)})
	if v := tab.View(100, 20); strings.Contains(v, "não é administrador") || !strings.Contains(v, "administrador: sim") {
		t.Errorf("admin:\n%s", v)
	}
}

func TestPrivacyTabForaDoWindowsSemTeclasDeMudanca(t *testing.T) {
	be := &fakePriv{info: PrivInfo{Supported: false, OS: "linux"}}
	tab := privTab(t, be)
	if v := tab.View(80, 14); !strings.Contains(v, "A privacidade: só existe no Windows") || !strings.Contains(v, "este sistema é Linux") {
		t.Fatalf("view: %q", v)
	}
	if len(tab.Keys()) != 0 {
		t.Errorf("teclas fora do Windows: %v", tab.Keys())
	}
	pkey(tab, "p", "a", "d", "m", "s")
	if len(be.plans)+len(be.applies)+be.undoCalls != 0 || tab.Capturing() {
		t.Error("fora do Windows nenhuma tecla faz nada")
	}
}

func TestPrivacyTabErroDeLeitura(t *testing.T) {
	tab := privTab(t, &fakePriv{loadErr: errors.New("powershell falhou\x1b[31m")})
	v := tab.View(80, 14)
	if !strings.Contains(v, "✗ Erro") || !strings.Contains(v, "Motivo: powershell falhou") || strings.Contains(v, "\x1b[31m") {
		t.Errorf("view: %q", v)
	}
	pkey(tab, "a")
	if tab.Capturing() {
		t.Error("com erro de leitura nada é capturado")
	}
}

func TestPrivacyTabVerPlanoNaoMuda(t *testing.T) {
	be := &fakePriv{info: privSample(true), plan: PrivPlan{Text: planText, Pending: 1, IsAdmin: true}}
	tab := privTab(t, be)
	pkey(tab, "p")
	if got := strings.Join(be.plans, ","); got != "basico" {
		t.Fatalf("plans=%q", got)
	}
	v := tab.View(100, 16)
	if !strings.Contains(v, "1 -> 0") || strings.Contains(v, "desfazer:") {
		t.Errorf("plano:\n%s", v)
	}
	if tab.Capturing() || len(be.applies) != 0 {
		t.Error("ver o plano não confirma nem aplica")
	}
	pkey(tab, "s") // "s" fora da confirmação não faz nada
	if len(be.applies) != 0 {
		t.Fatal("aplicou sem confirmação")
	}
	pkey(tab, "esc")
	if strings.Contains(tab.View(100, 16), "1 -> 0") {
		t.Error("Esc volta à lista")
	}
}

func TestPrivacyTabAplicarPedeConfirmacao(t *testing.T) {
	be := &fakePriv{info: privSample(true), plan: PrivPlan{Text: planText, Pending: 1, IsAdmin: true}, applyRes: PrivResult{Applied: 1, BackupPath: `C:\b.json`}}
	tab := privTab(t, be)
	pkey(tab, "down", "a")
	if v := tab.View(100, 24); !tab.Capturing() || !strings.Contains(tab.Status(), "s confirma") || !strings.Contains(strings.ToLower(v), "recomendado") || !strings.Contains(v, ">[ Cancelar ]<") {
		t.Fatalf("confirmação: capturing=%v status=%q\n%s", tab.Capturing(), tab.Status(), tab.View(100, 24))
	}
	if len(be.applies) != 0 {
		t.Fatal("aplicou antes da confirmação")
	}
	pkey(tab, "s")
	if got := strings.Join(be.applies, ","); got != "recomendado" {
		t.Fatalf("applies=%q", got)
	}
	if tab.Capturing() {
		t.Error("terminou: não captura mais")
	}
	if !strings.Contains(tab.Status(), "Aplicado: 1 mudança(s)") || tab.Result() == nil || !strings.Contains(strings.Join(tab.Result().Lines, "|"), `C:\b.json`) {
		t.Errorf("resultado: status=%q result=%+v", tab.Status(), tab.Result())
	}
	if be.loads < 2 {
		t.Error("depois de aplicar o estado é relido")
	}
}

func TestPrivacyTabSoSConfirma(t *testing.T) {
	// Cancelam: n, N, esc, e Enter no foco inicial (Cancelar).
	for _, k := range []string{"n", "N", "enter", "esc"} {
		be := &fakePriv{info: privSample(true), plan: PrivPlan{Text: planText, Pending: 1, IsAdmin: true}}
		tab := privTab(t, be)
		pkey(tab, "a", k)
		if len(be.applies) != 0 {
			t.Errorf("tecla %q aplicou", k)
		}
		if tab.Capturing() || !strings.Contains(tab.Status(), "Cancelado") {
			t.Errorf("tecla %q: capturing=%v status=%q", k, tab.Capturing(), tab.Status())
		}
	}
	// Teclas aleatórias não confirmam nem cancelam: a pergunta continua.
	for _, k := range []string{"x", "q", "1", "space", "a", "r"} {
		be := &fakePriv{info: privSample(true), plan: PrivPlan{Text: planText, Pending: 1, IsAdmin: true}}
		tab := privTab(t, be)
		pkey(tab, "a", k)
		if len(be.applies) != 0 || !tab.Capturing() {
			t.Errorf("tecla %q: applies=%d capturing=%v", k, len(be.applies), tab.Capturing())
		}
	}
	// s/y confirmam direto; Tab + Enter confirma pelo botão.
	for _, keys := range [][]string{{"s"}, {"y"}, {"tab", "enter"}} {
		be := &fakePriv{info: privSample(true), plan: PrivPlan{Text: planText, Pending: 1, IsAdmin: true}}
		tab := privTab(t, be)
		pkey(tab, append([]string{"a"}, keys...)...)
		if len(be.applies) != 1 {
			t.Errorf("%v não confirmou", keys)
		}
	}
}

func TestPrivacyTabConfirmacaoCapturaTeclasDaRaiz(t *testing.T) {
	be := &fakePriv{info: privSample(true), plan: PrivPlan{Text: planText, Pending: 1, IsAdmin: true}}
	tab := privTab(t, be)
	m := New(Options{Mono: true, NoMotion: true}, tab)
	pkey(tab, "a")
	if !tab.Capturing() {
		t.Fatal("deveria capturar")
	}
	m.Update(tea.KeyPressMsg{Code: 'q', Text: "q"})
	if m.Quitting() {
		t.Error("q durante a confirmação não pode sair")
	}
}

func TestPrivacyTabRolarNaConfirmacaoNaoCancela(t *testing.T) {
	be := &fakePriv{info: privSample(true), plan: PrivPlan{Text: planText, Pending: 1, IsAdmin: true}}
	tab := privTab(t, be)
	pkey(tab, "a", "down", "up", "pgdown", "pgup")
	if !tab.Capturing() || len(be.applies) != 0 {
		t.Error("rolar mantém a pergunta")
	}
}

func TestPrivacyTabSemAdminRecusaAntesDeMudar(t *testing.T) {
	be := &fakePriv{info: privSample(false), plan: PrivPlan{Text: planText, Pending: 3, NeedsAdmin: true, IsAdmin: false}}
	tab := privTab(t, be)
	pkey(tab, "down", "a")
	if !tab.Capturing() {
		t.Fatal("a confirmação abre, com o botão de aplicar indisponível")
	}
	v := tab.View(100, 24)
	for _, want := range []string{"administrador", "nada será alterado", "( Aplicar: sem administrador )", ">[ Cancelar ]<", "m para aplicar só"} {
		if !strings.Contains(v, want) {
			t.Errorf("faltou %q:\n%s", want, v)
		}
	}
	pkey(tab, "s", "y", "tab", "enter")
	if len(be.applies) != 0 {
		t.Fatal("aplicou sem administrador")
	}
	if tab.Capturing() { // tab não vai ao botão indisponível: Enter ativa Cancelar
		t.Error("Enter em Cancelar deveria fechar a confirmação")
	}
}

func TestPrivacyTabSomenteUsuario(t *testing.T) {
	be := &fakePriv{info: privSample(false), plan: PrivPlan{Text: planText, Pending: 1}, applyRes: PrivResult{Applied: 1, BackupPath: "b"}}
	tab := privTab(t, be)
	pkey(tab, "down", "m")
	if !strings.Contains(strings.Join(hintLabels(tab), "|"), "só usuário ✓") || !strings.Contains(tab.View(100, 20), "--somente-usuario") {
		t.Errorf("opção ligada não aparece: %v", hintLabels(tab))
	}
	pkey(tab, "a")
	if got := be.plans[len(be.plans)-1]; got != "recomendado+user" {
		t.Fatalf("plano pediu %q", got)
	}
	pkey(tab, "s")
	if got := strings.Join(be.applies, ","); got != "recomendado+user" {
		t.Fatalf("applies=%q", got)
	}
}

func TestPrivacyTabNadaAFazer(t *testing.T) {
	be := &fakePriv{info: privSample(true), plan: PrivPlan{Text: planText, Pending: 0, IsAdmin: true}}
	tab := privTab(t, be)
	pkey(tab, "a")
	if tab.Capturing() || !strings.Contains(tab.Status(), "Nada a fazer") {
		t.Errorf("capturing=%v status=%q", tab.Capturing(), tab.Status())
	}
}

func TestPrivacyTabErroDoPlanoEDaAplicacao(t *testing.T) {
	be := &fakePriv{info: privSample(true), planErr: errors.New("registro ilegível")}
	tab := privTab(t, be)
	pkey(tab, "a")
	if !strings.Contains(tab.Status(), "erro: registro ilegível") || tab.Capturing() {
		t.Errorf("status=%q", tab.Status())
	}
	be = &fakePriv{info: privSample(true), plan: PrivPlan{Text: planText, Pending: 2, IsAdmin: true},
		applyRes: PrivResult{Applied: 1}, applyErr: errors.New("HKLM\\x: acesso negado")}
	tab = privTab(t, be)
	pkey(tab, "a", "s")
	if !strings.Contains(tab.Status(), "erro: HKLM") || tab.Result() == nil || !strings.Contains(strings.Join(tab.Result().Lines, "|"), "Antes da falha: 1") {
		t.Errorf("status=%q result=%+v", tab.Status(), tab.Result())
	}
	be.applyErr = ErrPrivNeedsAdmin
	be.applyRes = PrivResult{}
	pkey(tab, "a", "s")
	if !strings.Contains(tab.Status(), "Precisa de administrador") {
		t.Errorf("status=%q", tab.Status())
	}
}

func TestPrivacyTabDesfazer(t *testing.T) {
	be := &fakePriv{info: privSample(true), undoPlan: PrivPlan{Text: "O que será restaurado\n  ! x: 0 -> 1\n", Pending: 1, IsAdmin: true}, undoRes: PrivResult{Applied: 1}}
	tab := privTab(t, be)
	pkey(tab, "d")
	if !tab.Capturing() || !strings.Contains(tab.View(100, 24), "RESTAURAR") || !strings.Contains(tab.View(100, 24), "0 -> 1") {
		t.Fatalf("status=%q view=%s", tab.Status(), tab.View(100, 24))
	}
	if be.undoCalls != 0 {
		t.Fatal("restaurou antes do s")
	}
	pkey(tab, "s")
	if be.undoCalls != 1 || !strings.Contains(tab.Status(), "Restaurado: 1") {
		t.Fatalf("calls=%d status=%q", be.undoCalls, tab.Status())
	}
}

func TestPrivacyTabDesfazerSemBackup(t *testing.T) {
	be := &fakePriv{info: privSample(true), undoErr: ErrPrivNoBackup}
	tab := privTab(t, be)
	pkey(tab, "d")
	if tab.Capturing() || !strings.Contains(tab.Status(), "Não há backup") || be.undoCalls != 0 {
		t.Errorf("status=%q", tab.Status())
	}
}

func TestPrivacyTabDesfazerSemAdmin(t *testing.T) {
	be := &fakePriv{info: privSample(false), undoPlan: PrivPlan{Text: "x", Pending: 1, NeedsAdmin: true}}
	tab := privTab(t, be)
	pkey(tab, "d")
	if v := tab.View(100, 24); !tab.Capturing() || !strings.Contains(v, "administrador") || !strings.Contains(v, "( Restaurar: sem administrador )") {
		t.Errorf("view=%s", v)
	}
	pkey(tab, "s")
	if be.undoCalls != 0 {
		t.Fatal("desfez sem administrador")
	}
}

func TestPrivacyTabDesfazerNadaARestaurar(t *testing.T) {
	be := &fakePriv{info: privSample(true), undoPlan: PrivPlan{Text: "x", Pending: 0, IsAdmin: true}, undoRes: PrivResult{Nothing: true}}
	tab := privTab(t, be)
	pkey(tab, "d")
	if v := tab.View(100, 24); !strings.Contains(v, "REMOVER O BACKUP DE PRIVACIDADE?") {
		t.Fatalf("view=%s", v)
	}
	pkey(tab, "s")
	if !strings.Contains(tab.Status(), "Nada a restaurar") {
		t.Errorf("status=%q", tab.Status())
	}
}

func TestPrivacyTabTextoExternoSanitizado(t *testing.T) {
	info := privSample(true)
	info.Settings[0].Title = "ajuste\x1b]0;x\x07"
	be := &fakePriv{info: info, plan: PrivPlan{Text: "alvo\x1b[2J -> 0\n", Pending: 1, IsAdmin: true}, applyRes: PrivResult{Applied: 1, BackupPath: "C:\\b\x1b[31m"}}
	tab := privTab(t, be)
	pkey(tab, "p")
	v := tab.View(100, 16)
	pkey(tab, "esc", "a", "s")
	v += tab.View(100, 16) + tab.Status()
	if strings.ContainsRune(v, 0x1b) || strings.ContainsRune(v, 7) {
		t.Errorf("controle vazou: %q", v)
	}
}

func TestPrivacyTabTamanhoEstreitoEAltura(t *testing.T) {
	be := &fakePriv{info: privSample(false), plan: PrivPlan{Text: strings.Repeat("linha longa de plano que precisa quebrar ", 6) + "\n", Pending: 1}}
	tab := privTab(t, be)
	for _, w := range []int{60, 40} {
		for _, h := range []int{4, 10} {
			v := tab.View(w, h)
			lines := strings.Split(v, "\n")
			if len(lines) > h {
				t.Errorf("%dx%d: %d linhas", w, h, len(lines))
			}
			for _, l := range lines {
				if lipgloss.Width(l) > w {
					t.Errorf("%dx%d: linha larga (%d): %q", w, h, lipgloss.Width(l), l)
				}
			}
		}
	}
	tab.View(60, 6)
	if tab.More() == 0 {
		t.Error("com pouca altura sobram linhas abaixo da dobra")
	}
	pkey(tab, "pgdown")
	tab.View(60, 6)
	pkey(tab, "p")
	v := tab.View(40, 6)
	for _, l := range strings.Split(v, "\n") {
		if lipgloss.Width(l) > 40 {
			t.Errorf("plano estreito estourou: %q", l)
		}
	}
}

func TestPrivacyTabPlanoAtrasadoEhIgnorado(t *testing.T) {
	be := &fakePriv{info: privSample(true)}
	tab := privTab(t, be)
	tab.Update(privPlanMsg{seq: 99, kind: privApply, plan: PrivPlan{Text: "x", Pending: 1, IsAdmin: true}})
	if tab.Capturing() {
		t.Error("plano de outra consulta não pode abrir a confirmação")
	}
}

func TestPrivacyTabUsaSpinnerSemMovimento(t *testing.T) {
	tab := NewPrivacyTab(context.Background(), &fakePriv{}, Theme{Mono: true, NoMotion: true})
	if !strings.Contains(tab.View(80, 14), SymLoad+" Carregando") {
		t.Error("com movimento reduzido o símbolo é fixo")
	}
	if tab.theme.Tick() != nil {
		t.Error("sem movimento nenhum quadro é agendado")
	}
}

func TestPrivacyTabPalavraLongaNaoPerdeConteudo(t *testing.T) {
	path := `HKCU\Software\Microsoft\Windows\CurrentVersion\ContentDeliveryManager\SubscribedContent-338388Enabled`
	be := &fakePriv{info: privSample(true), plan: PrivPlan{Text: "  ! " + path + ": 1 -> 0\n", Pending: 1, IsAdmin: true}}
	tab := privTab(t, be)
	pkey(tab, "p")
	v := tab.View(40, 12)
	for _, l := range strings.Split(v, "\n") {
		if lipgloss.Width(l) > 40 {
			t.Errorf("linha larga: %q", l)
		}
	}
	joined := strings.NewReplacer("\n", "", " ", "").Replace(v)
	if !strings.Contains(joined, strings.ReplaceAll(path, " ", "")) {
		t.Errorf("o caminho foi cortado com reticências:\n%s", v)
	}
}

// blockPriv bloqueia Plan/UndoPlan até o teste liberar (resposta tardia).
type blockPriv struct {
	fakePriv
	release chan struct{}
	started chan struct{}
}

func (b *blockPriv) Plan(ctx context.Context, p PrivProfile, only bool) (PrivPlan, error) {
	b.started <- struct{}{}
	<-b.release
	return b.fakePriv.Plan(ctx, p, only)
}
func (b *blockPriv) UndoPlan(ctx context.Context) (PrivPlan, error) {
	b.started <- struct{}{}
	<-b.release
	return b.fakePriv.UndoPlan(ctx)
}

// Lupa #66 (2): Esc durante a preparação do plano cancela; a resposta tardia não abre a confirmação nem muda nada.
func TestPrivacyEscDuranteOPlanoDescartaARespostaTardia(t *testing.T) {
	for _, key := range []string{"a", "d"} {
		be := &blockPriv{fakePriv: fakePriv{info: privSample(true), plan: PrivPlan{Text: planText, Pending: 2}, undoPlan: PrivPlan{Text: planText, Pending: 2}}, release: make(chan struct{}), started: make(chan struct{}, 1)}
		tab := NewPrivacyTab(context.Background(), be, Theme{Mono: true, NoMotion: true})
		privRun(tab, tab.Init())
		_, cmd := tab.Update(tea.KeyPressMsg{Code: rune(key[0]), Text: key})
		done := make(chan tea.Msg, 1)
		go func() { done <- privFirstPlanMsg(cmd) }()
		<-be.started
		tab.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
		if tab.busy != busyNone {
			t.Fatalf("%s: Esc deve cancelar a preparação: busy=%v", key, tab.busy)
		}
		close(be.release)
		if m := <-done; m != nil {
			tab.Update(m)
		}
		if tab.asking || tab.view {
			t.Fatalf("%s: a resposta tardia não pode abrir a confirmação (asking=%v view=%v)", key, tab.asking, tab.view)
		}
		if len(be.applies) != 0 || be.undoCalls != 0 {
			t.Fatalf("%s: nada pode mudar", key)
		}
	}
}

// privFirstPlanMsg roda o comando do plano (o que executa o backend) e devolve a mensagem de plano.
func privFirstPlanMsg(cmd tea.Cmd) tea.Msg {
	if cmd == nil {
		return nil
	}
	switch m := cmd().(type) {
	case tea.BatchMsg:
		for _, c := range m {
			if r := c(); r != nil {
				if _, ok := r.(privPlanMsg); ok {
					return r
				}
			}
		}
		return nil
	default:
		return m
	}
}

// Lupa #66: a confirmação usa o ID do plano EXIBIDO (o fake recusa qualquer outro).
func TestPrivacyConfirmUsaOIDDoPlanoExibido(t *testing.T) {
	be := &fakePriv{info: privSample(true), plan: PrivPlan{Text: planText, Pending: 2}, applyRes: PrivResult{Applied: 2}}
	tab := privTab(t, be)
	pkey(tab, "a", "s")
	if len(be.applyIDs) != 1 || be.applyIDs[0] != be.lastID || be.lastID == "" {
		t.Fatalf("ids=%v último=%q", be.applyIDs, be.lastID)
	}
	if len(be.applies) != 1 {
		t.Fatalf("o plano exibido devia ter sido aplicado: %v", be.applies)
	}
}

// Lupa #71: a releitura roda DEPOIS de aplicar/restaurar; se ela falhar, a tela nunca nega as alterações.
func TestPrivacidadeReleituraFalhaDepoisDeAplicarNaoDizNadaFoiAlterado(t *testing.T) {
	for _, partial := range []bool{false, true} {
		be := &fakePriv{info: privSample(true), plan: PrivPlan{Text: planText, Pending: 2}, applyRes: PrivResult{Applied: 2, BackupPath: `C:\b.json`}}
		if partial {
			be.applyErr = errors.New("falhou no meio")
		}
		tab := privTab(t, be)
		be.loadErr = errors.New("reg.exe: acesso negado") // a releitura pós-ação falha
		pkey(tab, "a", "s")
		check := func(when string) {
			out := stripANSI(tab.View(80, 24))
			if strings.Contains(out, "Nada foi alterado") {
				t.Errorf("partial=%v %s: a tela nega as alterações:\n%s", partial, when, out)
			}
			if !strings.Contains(out, "atualizar a leitura") {
				t.Errorf("partial=%v %s: deve dizer que não deu para atualizar a leitura:\n%s", partial, when, out)
			}
		}
		check("com a faixa")
		tab.Dismiss() // fechar a faixa com x não pode deixar só a frase falsa
		check("depois de fechar a faixa")
	}
}

// Sem alteração nenhuma, o erro de leitura continua dizendo (com razão) que nada foi alterado.
func TestPrivacidadeErroDeLeituraSemAcaoContinuaDizendoNadaFoiAlterado(t *testing.T) {
	be := &fakePriv{loadErr: errors.New("boom")}
	tab := privTab(t, be)
	if out := stripANSI(tab.View(80, 24)); !strings.Contains(out, "Nada foi alterado") {
		t.Errorf("sem ação, vale a cauda:\n%s", out)
	}
}

// Lupa #71 (2): o Desfazer bem-sucedido APAGA o backup; se a releitura falhar depois, a tela não promete um backup que
// não existe mais (nem com a faixa, nem depois do x).
func TestPrivacidadeDesfazerOKReleituraFalhaNaoPrometeBackup(t *testing.T) {
	be := &fakePriv{info: privSample(true), undoPlan: PrivPlan{Text: "O que será restaurado\n  x -> y\n", Pending: 1}, undoRes: PrivResult{Applied: 1}}
	tab := privTab(t, be)
	be.loadErr = errors.New("reg.exe: acesso negado")
	pkey(tab, "d", "s")
	for _, when := range []string{"com a faixa", "depois do x"} {
		out := stripANSI(tab.View(80, 24))
		low := strings.ToLower(out)
		if strings.Contains(low, "backup guarda") || strings.Contains(out, "Nada foi alterado") {
			t.Errorf("%s: promete backup inexistente ou nega a alteração:\n%s", when, out)
		}
		if !strings.Contains(out, "atualizar a leitura") {
			t.Errorf("%s: deve dizer que não deu para atualizar a leitura:\n%s", when, out)
		}
		tab.Dismiss()
	}
}

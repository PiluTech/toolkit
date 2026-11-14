package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"unicode"

	tea "charm.land/bubbletea/v2"
)

// fakeUsb é o backend de mentira: nenhum teste da seção toca o Windows.
type fakeUsb struct {
	info     UsbInfo
	loadErr  error
	plan     UsbPlan
	keepPlan UsbPlan
	undoPlan UsbPlan
	planErr  error

	applyRes, keepRes, undoRes UsbResult
	applyErr, keepErr, undoErr error

	loads   int
	plans   [][]string // as exclusões recebidas por cada Plan
	applies []string   // IDs aplicados
	keeps   []string
	undos   []string
	seq     int
	lastID  string
	issued  map[string]bool // todo ID emitido (uma resposta tardia também emite o seu)
}

func (f *fakeUsb) issue() string {
	f.seq++
	f.lastID = fmt.Sprintf("plano-usb-%d", f.seq)
	if f.issued == nil {
		f.issued = map[string]bool{}
	}
	f.issued[f.lastID] = true
	return f.lastID
}
func (f *fakeUsb) Load(context.Context) (UsbInfo, error) { f.loads++; return f.info, f.loadErr }
func (f *fakeUsb) Plan(_ context.Context, ex []string) (UsbPlan, error) {
	f.plans = append(f.plans, append([]string(nil), ex...))
	p := f.plan
	p.ID = f.issue()
	return p, f.planErr
}
func (f *fakeUsb) Apply(_ context.Context, id string) (UsbResult, error) {
	if !f.issued[id] {
		return UsbResult{}, ErrUsbStalePlan // só um plano emitido é aplicável
	}
	f.applies = append(f.applies, id)
	return f.applyRes, f.applyErr
}
func (f *fakeUsb) KeepPlan(context.Context) (UsbPlan, error) {
	p := f.keepPlan
	p.ID = f.issue()
	return p, nil
}
func (f *fakeUsb) Keep(_ context.Context, id string) (UsbResult, error) {
	if !f.issued[id] {
		return UsbResult{}, ErrUsbStalePlan
	}
	f.keeps = append(f.keeps, id)
	return f.keepRes, f.keepErr
}
func (f *fakeUsb) UndoPlan(context.Context) (UsbPlan, error) {
	p := f.undoPlan
	p.ID = f.issue()
	return p, nil
}
func (f *fakeUsb) Undo(_ context.Context, id string) (UsbResult, error) {
	if !f.issued[id] {
		return UsbResult{}, ErrUsbStalePlan
	}
	f.undos = append(f.undos, id)
	return f.undoRes, f.undoErr
}

const (
	usbKbKey  = `USB\VID_FFFF&PID_0001\1`
	usbPenKey = `USB\VID_FFFF&PID_0003\2`
	usbText   = "Modo empresa: permitir só os USB conhecidos (Windows) [EXPERIMENTAL]\n\n" +
		"Dispositivos USB conectados agora:\n  [x] Teclado USB  FFFF:0001  classe USB  conectado agora · essencial -> permitido\n  [ ] Pendrive USB  FFFF:0003  classe USB  conectado agora -> BLOQUEADO ao reconectar (desmarcado)\n\n" +
		"Prova de dispositivos essenciais: OK (nenhum fica bloqueado, nem os pais deles)\n  OK  Teclado PS/2 padrão (Keyboard, PS/2): nenhuma regra o nega\n  OK  Disco NVMe (DiskDrive, SCSI): nenhuma regra o nega\n\n" +
		"Reversão automática em 10 minuto(s): se você não confirmar (toolkit usb confirmar), a tarefa restaura o snapshot sozinha.\n\n" +
		"Se o teclado travar mesmo assim: (1) espere o prazo; (2) use outro teclado; (3) outra conta de administrador restaura o .reg; (5) WinRE (comandos A VERIFICAR em VM).\n"
)

func usbSample(state string, admin bool) UsbInfo {
	return UsbInfo{Supported: true, OS: "windows", Edition: "pro", EditionOK: true, IsAdmin: admin, State: state, Deadline: "12:10", Reason: "o backup não existe, mas há sinais de aplicação",
		Devices: []UsbDevice{
			{Key: usbKbKey, Name: "Teclado USB", VIDPID: "FFFF:0001", Class: "USB", Essential: true},
			{Key: usbPenKey, Name: "Pendrive USB", VIDPID: "FFFF:0003", Class: "USB"},
		}}
}

func newFakeUsb(state string, admin bool) *fakeUsb {
	return &fakeUsb{info: usbSample(state, admin),
		plan:     UsbPlan{Text: usbText, OK: true, Prazo: 10, Allowed: 1, Deselected: 1, Blocked: 1, IsAdmin: admin},
		keepPlan: UsbPlan{Text: "Manter o modo empresa de USB (EXPERIMENTAL)\n\nA tarefa de reversão será removida.\n", OK: true, IsAdmin: admin},
		undoPlan: UsbPlan{Text: "O que será restaurado (snapshot validado):\n  - DenyDeviceClasses: remove (não existia antes)\n", OK: true, IsAdmin: admin},
	}
}

func urun(tab *UsbTab, cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	switch m := cmd().(type) {
	case nil:
	case tea.BatchMsg:
		for _, c := range m {
			urun(tab, c)
		}
	default:
		_, next := tab.Update(m)
		urun(tab, next)
	}
}

func usbTab(t *testing.T, be *fakeUsb) *UsbTab {
	t.Helper()
	tab := NewUsbTab(context.Background(), be, Theme{Mono: true, NoMotion: true})
	if cmd := tab.Init(); cmd != nil || be.loads != 0 {
		t.Fatal("Init não pode ler nada: a leitura do PnP é lenta e só acontece com a tecla c")
	}
	ukey(tab, "c") // a primeira leitura é pedida pela pessoa
	return tab
}

func ukey(tab *UsbTab, keys ...string) {
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
		case "tab":
			msg = tea.KeyPressMsg{Code: tea.KeyTab}
		case "space":
			msg = tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}
		default:
			msg = tea.KeyPressMsg{Code: rune(k[0]), Text: k}
		}
		_, cmd := tab.Update(msg)
		urun(tab, cmd)
	}
}

func uview(tab *UsbTab, w, h int) string { return stripANSI(tab.View(w, h)) }

func TestUsbTabIdentidadeExperimentalNoTitulo(t *testing.T) {
	tab := NewUsbTab(context.Background(), &fakeUsb{}, Theme{Mono: true})
	if tab.Title() != "USB permitidos (experimental)" || tab.Short() != "USB" {
		t.Fatalf("%q %q", tab.Title(), tab.Short())
	}
}

func TestUsbTabEstados(t *testing.T) {
	// ainda não lido (Init não lê) e carregando (depois do c)
	fb := newFakeUsb(UsbClean, true)
	tab := NewUsbTab(context.Background(), fb, Theme{Mono: true, NoMotion: true})
	if out := uview(tab, 80, 24); !strings.Contains(out, "ainda não foram lidos") || !strings.Contains(out, "Tecle c") {
		t.Errorf("antes da primeira leitura:\n%s", out)
	}
	ukeys := tab.Keys()
	if len(ukeys) != 1 || ukeys[0].Key != "c" {
		t.Errorf("antes da primeira leitura só o c: %v", ukeys)
	}
	ukey(tab, "a", "s", "p")
	if len(fb.plans) != 0 || len(fb.applies) != 0 {
		t.Fatal("antes da leitura nada pode ser chamado")
	}
	tab.Update(tea.KeyPressMsg{Code: 'c', Text: "c"}) // comando sem executar: a leitura está em andamento
	if out := uview(tab, 80, 24); !strings.Contains(out, "Carregando") {
		t.Errorf("carregando:\n%s", out)
	}
	// fora do Windows
	be := &fakeUsb{info: UsbInfo{OS: "linux"}}
	tab = usbTab(t, be)
	out := uview(tab, 80, 24)
	if !strings.Contains(out, "Não disponível") || !strings.Contains(out, "Linux") {
		t.Errorf("fora do Windows:\n%s", out)
	}
	if tab.Keys() != nil {
		t.Error("fora do Windows não há teclas")
	}
	ukey(tab, "a", "s", "y", "p", "d", "n")
	if len(be.plans) != 0 || len(be.applies) != 0 || len(be.undos) != 0 || len(be.keeps) != 0 {
		t.Fatal("fora do Windows nada pode ser chamado")
	}
	// Windows Home (edição sem suporte)
	home := newFakeUsb(UsbClean, true)
	home.info.EditionOK, home.info.Edition = false, "home"
	tab = usbTab(t, home)
	out = uview(tab, 80, 24)
	if !strings.Contains(out, "home") || !strings.Contains(out, "não documenta") {
		t.Errorf("Home:\n%s", out)
	}
	ukey(tab, "a", "s", "y")
	if len(home.plans) != 0 || len(home.applies) != 0 {
		t.Fatal("Home: nada pode ser chamado")
	}
	// erro de leitura
	bad := &fakeUsb{loadErr: errors.New("powershell: acesso negado")}
	tab = usbTab(t, bad)
	out = uview(tab, 80, 24)
	if !strings.Contains(out, "Erro") || !strings.Contains(out, "Nada foi alterado") {
		t.Errorf("erro:\n%s", out)
	}
	ukey(tab, "a", "s", "y")
	if len(bad.plans) != 0 || len(bad.applies) != 0 {
		t.Fatal("com erro de leitura nada pode ser chamado")
	}
}

func TestUsbTabTelaPrincipalListaOsDispositivosEDizExperimental(t *testing.T) {
	tab := usbTab(t, newFakeUsb(UsbClean, false))
	out := uview(tab, 100, 30)
	for _, want := range []string{"[experimental]", "Teclado USB", "FFFF:0001", "Pendrive USB", "essencial", "permitido", "reduz o risco, não o elimina", "administrador: não", "nunca eleva sozinho"} {
		if !strings.Contains(out, want) {
			t.Errorf("tela sem %q:\n%s", want, out)
		}
	}
}

func TestUsbTabEspacoMarcaEDesmarcaEPlanoLevaAsExclusoes(t *testing.T) {
	be := newFakeUsb(UsbClean, true)
	tab := usbTab(t, be)
	ukey(tab, "down", "space")
	if out := uview(tab, 100, 30); !strings.Contains(out, "[ ] Pendrive USB") || !strings.Contains(out, "fora da lista") {
		t.Fatalf("desmarcado não aparece:\n%s", out)
	}
	ukey(tab, "p")
	if len(be.plans) != 1 || len(be.plans[0]) != 1 || be.plans[0][0] != usbPenKey {
		t.Fatalf("exclusões enviadas = %v", be.plans)
	}
	ukey(tab, "esc", "space") // marca de novo
	ukey(tab, "p")
	if len(be.plans[1]) != 0 {
		t.Fatalf("depois de marcar de novo não há exclusão: %v", be.plans[1])
	}
}

func TestUsbTabVerPlanoEhSoLeituraESNuncaAplica(t *testing.T) {
	be := newFakeUsb(UsbClean, true)
	tab := usbTab(t, be)
	ukey(tab, "p")
	if !tab.view || tab.asking {
		t.Fatalf("p devia abrir o plano em modo leitura (view=%v asking=%v)", tab.view, tab.asking)
	}
	if out := uview(tab, 100, 30); !strings.Contains(out, "EXPERIMENTAL") || !strings.Contains(tab.Status(), "Só leitura") {
		t.Errorf("plano:\n%s\nstatus=%q", out, tab.Status())
	}
	ukey(tab, "s", "y")
	if len(be.applies) != 0 {
		t.Fatal("s/y no modo leitura não aplicam")
	}
	ukey(tab, "enter") // Enter só volta
	if tab.view || len(be.applies) != 0 {
		t.Fatal("Enter no modo leitura só volta")
	}
}

func TestUsbTabConfirmacaoUnicaComEvidenciaEFocoEmCancelar(t *testing.T) {
	be := newFakeUsb(UsbClean, true)
	tab := usbTab(t, be)
	ukey(tab, "a")
	if !tab.asking || tab.conf == nil {
		t.Fatal("a devia abrir a confirmação")
	}
	if tab.conf.Focus != 0 || tab.conf.Buttons[0].Label != "Cancelar" {
		t.Fatalf("o foco inicial tem de ser Cancelar: %+v", tab.conf)
	}
	if len(be.applies) != 0 {
		t.Fatal("abrir a confirmação não aplica")
	}
	// Enter no foco inicial (Cancelar) cancela
	ukey(tab, "enter")
	if tab.asking || len(be.applies) != 0 {
		t.Fatal("Enter em Cancelar não pode aplicar")
	}
	ukey(tab, "a")
	// evidência: tudo rolável, em qualquer altura, está no corpo da confirmação
	var all strings.Builder
	for _, sz := range [][2]int{{80, 24}, {120, 40}, {60, 16}} {
		all.WriteString(uview(tab, sz[0], sz[1]) + "\n")
	}
	tab.conf.scroll = 0
	conf := tab.conf
	body := strings.Join(append(conf.headLines(tab.theme, 100), conf.planLines(tab.theme, 100)...), "\n")
	body = strings.ToLower(strings.Join(strings.Fields(stripANSI(body)), " "))
	for _, want := range []string{
		"experimental", "teclado usb", "ffff:0001", "pendrive usb", "bloqueado", "permitido",
		"risco", "teclado pode travar", "segundo teclado", "reverte sozinho em 10 minuto(s)", "prova de dispositivos essenciais", "winre",
		"reduz o risco, não o elimina", "snapshot protegido", "antes da primeira chave",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("a confirmação não traz %q:\n%s", want, body)
		}
	}
	if len(be.applies) != 0 {
		t.Fatal("nada foi aplicado ainda")
	}
	ukey(tab, "esc")
	if tab.asking || len(be.applies) != 0 {
		t.Fatal("Esc cancela sem aplicar")
	}
}

func TestUsbTabAplicaOIDExibidoEMostraFaixaPendente(t *testing.T) {
	be := newFakeUsb(UsbClean, true)
	be.applyRes = UsbResult{Entries: 8, Deadline: "12:10", BackupPath: `C:\ProgramData\toolkit`}
	tab := usbTab(t, be)
	ukey(tab, "a", "s")
	if len(be.applies) != 1 || be.applies[0] != be.lastID {
		t.Fatalf("applies=%v último=%q", be.applies, be.lastID)
	}
	r := tab.Result()
	if r == nil || r.Kind != ResultPending || !strings.Contains(r.Title, "aguardando confirmação") {
		t.Fatalf("faixa = %#v", r)
	}
	joined := strings.Join(r.Lines, "\n")
	for _, want := range []string{"REVERTE SOZINHO às 12:10", "tecla n", "Backup protegido", "tecla d"} {
		if !strings.Contains(joined, want) {
			t.Errorf("faixa sem %q: %s", want, joined)
		}
	}
	tab.Dismiss()
	if tab.Result() != nil {
		t.Fatal("x fecha a faixa")
	}
	if be.loads < 2 {
		t.Error("depois de aplicar a seção relê o estado")
	}
}

func TestUsbTabSemAdminBotaoIndisponivelENuncaAplica(t *testing.T) {
	be := newFakeUsb(UsbClean, false)
	be.plan.IsAdmin = false
	tab := usbTab(t, be)
	ukey(tab, "a")
	if !tab.asking || !tab.conf.Buttons[1].Disabled || tab.conf.Buttons[1].Reason != "sem administrador" {
		t.Fatalf("o botão de confirmar deve ficar indisponível sem administrador: %+v", tab.conf.Buttons)
	}
	if !strings.Contains(tab.conf.Warn, "nunca eleva sozinho") {
		t.Errorf("o aviso deve explicar o administrador: %q", tab.conf.Warn)
	}
	ukey(tab, "s", "y", "enter", "tab", "enter")
	if len(be.applies) != 0 {
		t.Fatal("sem administrador nada é aplicado")
	}
}

func TestUsbTabPlanoRecusadoNaoViraConfirmacao(t *testing.T) {
	be := newFakeUsb(UsbClean, true)
	be.plan.OK = false
	be.plan.Refusal = "Teclado USB ficaria BLOQUEADO"
	be.plan.Text = "RECUSADO: Teclado USB ficaria BLOQUEADO\n"
	tab := usbTab(t, be)
	ukey(tab, "a")
	if tab.asking {
		t.Fatal("plano recusado (sem prova) nunca vira confirmação")
	}
	if !tab.view || !strings.Contains(tab.Status(), "Recusado") || !strings.Contains(tab.Status(), "Nada foi alterado") {
		t.Fatalf("status = %q", tab.Status())
	}
	ukey(tab, "s", "y", "enter")
	if len(be.applies) != 0 {
		t.Fatal("recusa: s/y/enter não aplicam")
	}
}

func TestUsbTabTecladoDesmarcadoVaiNoPlanoERecusaPeloBackend(t *testing.T) {
	be := newFakeUsb(UsbClean, true)
	tab := usbTab(t, be)
	ukey(tab, "space", "a") // desmarca o teclado
	if len(be.plans) != 1 || be.plans[0][0] != usbKbKey {
		t.Fatalf("o teclado desmarcado tem de ir ao backend: %v", be.plans)
	}
}

// Plano de uso único por ID: A (lento) -> Esc -> B -> a resposta tardia de A não vira confirmação nem aplica.
type blockUsb struct {
	fakeUsb
	started chan struct{}
	release chan struct{}
	block   bool
}

func (b *blockUsb) Plan(ctx context.Context, ex []string) (UsbPlan, error) {
	if b.block {
		b.block = false
		b.started <- struct{}{}
		<-b.release
	}
	return b.fakeUsb.Plan(ctx, ex)
}

func usbFirstPlanMsg(cmd tea.Cmd) tea.Msg {
	if cmd == nil {
		return nil
	}
	switch m := cmd().(type) {
	case tea.BatchMsg:
		for _, c := range m {
			if r := c(); r != nil {
				if _, ok := r.(usbPlanMsg); ok {
					return r
				}
			}
		}
		return nil
	default:
		return m
	}
}

func TestUsbTabPlanoPorIDEscDescartaARespostaTardia(t *testing.T) {
	be := &blockUsb{fakeUsb: *newFakeUsb(UsbClean, true), started: make(chan struct{}, 1), release: make(chan struct{}), block: true}
	tab := NewUsbTab(context.Background(), be, Theme{Mono: true, NoMotion: true})
	ukey(tab, "c")
	_, cmd := tab.Update(tea.KeyPressMsg{Code: 'a', Text: "a"}) // A, bloqueado
	done := make(chan tea.Msg, 1)
	go func() { done <- usbFirstPlanMsg(cmd) }()
	<-be.started
	ukey(tab, "esc") // cancela a preparação de A
	if tab.busy != busyNone {
		t.Fatalf("Esc devia cancelar a preparação: busy=%v", tab.busy)
	}
	ukey(tab, "a") // B, rápido: abre a confirmação com o ID de B
	if !tab.asking {
		t.Fatal("B devia abrir a confirmação")
	}
	idB := tab.plan.ID
	close(be.release)
	if m := <-done; m != nil {
		tab.Update(m) // A chega TARDE
	}
	if tab.plan.ID != idB {
		t.Fatalf("a resposta tardia de A trocou o plano exibido: %q != %q", tab.plan.ID, idB)
	}
	ukey(tab, "s")
	if len(be.applies) != 1 || be.applies[0] != idB {
		t.Fatalf("só o plano exibido (B=%s) pode ser aplicado: %v", idB, be.applies)
	}
}

func TestUsbTabEscDuranteOPlanoNaoAbreNada(t *testing.T) {
	be := &blockUsb{fakeUsb: *newFakeUsb(UsbClean, true), started: make(chan struct{}, 1), release: make(chan struct{}), block: true}
	tab := NewUsbTab(context.Background(), be, Theme{Mono: true, NoMotion: true})
	ukey(tab, "c")
	_, cmd := tab.Update(tea.KeyPressMsg{Code: 'a', Text: "a"})
	done := make(chan tea.Msg, 1)
	go func() { done <- usbFirstPlanMsg(cmd) }()
	<-be.started
	ukey(tab, "esc")
	close(be.release)
	if m := <-done; m != nil {
		tab.Update(m)
	}
	if tab.asking || tab.view || len(be.applies) != 0 {
		t.Fatalf("a resposta tardia não pode abrir nada (asking=%v view=%v)", tab.asking, tab.view)
	}
}

func TestUsbTabPlanoVelhoDaAvisoENaoMuda(t *testing.T) {
	be := newFakeUsb(UsbClean, true)
	be.applyErr = ErrUsbStalePlan
	tab := usbTab(t, be)
	ukey(tab, "a", "s")
	r := tab.Result()
	if r == nil || r.Kind != ResultInfo || !strings.Contains(r.Title, "plano mudou") {
		t.Fatalf("faixa = %#v", r)
	}
}

func TestUsbTabFalhaNoMeioDizQueRestaurou(t *testing.T) {
	be := newFakeUsb(UsbClean, true)
	be.applyRes = UsbResult{RestoredNow: true}
	be.applyErr = errors.New("falha ao gravar DenyDeviceClasses\\2; o registro foi restaurado na hora")
	tab := usbTab(t, be)
	ukey(tab, "a", "s")
	r := tab.Result()
	if r == nil || r.Kind != ResultError || !strings.Contains(strings.Join(r.Lines, " "), "restaurado na hora") {
		t.Fatalf("faixa = %#v", r)
	}
}

func TestUsbTabReleituraFalhaDepoisDeAcaoNaoDizNadaFoiAlterado(t *testing.T) {
	for _, applyErr := range []error{nil, errors.New("falhou no meio")} {
		be := newFakeUsb(UsbClean, true)
		be.applyRes = UsbResult{Entries: 8, Deadline: "12:10"}
		be.applyErr = applyErr
		tab := usbTab(t, be)
		be.loadErr = errors.New("reg.exe: acesso negado") // a releitura pós-ação falha
		ukey(tab, "a", "s")
		out := uview(tab, 80, 24)
		if strings.Contains(out, "Nada foi alterado") || !strings.Contains(out, "atualizar a leitura") {
			t.Errorf("a tela nega as alterações ou não avisa da releitura (err=%v):\n%s", applyErr, out)
		}
	}
}

func TestUsbTabManterTemConfirmacaoPropriaComFocoEmCancelar(t *testing.T) {
	be := newFakeUsb(UsbArmed, true)
	tab := usbTab(t, be)
	if out := uview(tab, 100, 30); !strings.Contains(out, "REVERTE SOZINHO às 12:10") {
		t.Errorf("o prazo da reversão tem de aparecer (estático, sem contagem viva):\n%s", out)
	}
	ukey(tab, "n")
	if !tab.asking || tab.conf.Focus != 0 || !strings.Contains(tab.conf.Title, "Manter") {
		t.Fatalf("n devia abrir a confirmação de manter com foco em Cancelar: %+v", tab.conf)
	}
	ukey(tab, "enter")
	if len(be.keeps) != 0 {
		t.Fatal("Enter em Cancelar não mantém")
	}
	ukey(tab, "n", "s")
	if len(be.keeps) != 1 || be.keeps[0] != be.lastID {
		t.Fatalf("keeps = %v", be.keeps)
	}
	if r := tab.Result(); r == nil || r.Kind != ResultOK || !strings.Contains(r.Title, "mantido") {
		t.Fatalf("faixa = %#v", r)
	}
}

func TestUsbTabManterForaDoEstadoArmadoNaoAbreNada(t *testing.T) {
	for _, st := range []string{UsbClean, UsbActive, UsbUnknown} {
		be := newFakeUsb(st, true)
		tab := usbTab(t, be)
		ukey(tab, "n")
		if tab.asking || len(be.keeps) != 0 {
			t.Errorf("%s: n não pode abrir confirmação", st)
		}
	}
}

func TestUsbTabDesfazerComConfirmacao(t *testing.T) {
	be := newFakeUsb(UsbActive, true)
	be.undoRes = UsbResult{Restored: 8}
	tab := usbTab(t, be)
	ukey(tab, "d")
	if !tab.asking || tab.conf.Focus != 0 || !strings.Contains(tab.conf.Title, "Desfazer") {
		t.Fatalf("d devia abrir a confirmação com foco em Cancelar: %+v", tab.conf)
	}
	ukey(tab, "esc")
	if len(be.undos) != 0 {
		t.Fatal("Esc não desfaz")
	}
	ukey(tab, "d", "s")
	if len(be.undos) != 1 || be.undos[0] != be.lastID {
		t.Fatalf("undos = %v", be.undos)
	}
	if r := tab.Result(); r == nil || r.Kind != ResultOK || !strings.Contains(r.Title, "desfeito") {
		t.Fatalf("faixa = %#v", r)
	}
}

func TestUsbTabEstadoOriginalDesconhecidoSoMostraARecuperacao(t *testing.T) {
	be := newFakeUsb(UsbUnknown, true)
	be.undoPlan = UsbPlan{Text: "ESTADO ORIGINAL DESCONHECIDO\nRecuperação manual: WinRE\n", OK: false, Refusal: "ESTADO ORIGINAL DESCONHECIDO: nada é restaurado às cegas"}
	tab := usbTab(t, be)
	out := uview(tab, 100, 30)
	if !strings.Contains(out, "ESTADO ORIGINAL DESCONHECIDO") || strings.Contains(out, "Nada a desfazer") {
		t.Errorf("tela:\n%s", out)
	}
	ukey(tab, "d")
	if tab.asking || !strings.Contains(tab.Status(), "Recusado") {
		t.Fatalf("estado desconhecido nunca vira confirmação (asking=%v status=%q)", tab.asking, tab.Status())
	}
	ukey(tab, "s", "y", "enter")
	if len(be.undos) != 0 {
		t.Fatal("estado desconhecido: s/y/enter não restauram")
	}
	ukey(tab, "p")
	if !tab.view || !strings.Contains(tab.plan.Text, "Recuperação manual") {
		t.Fatal("p mostra a recuperação manual")
	}
}

func TestUsbTabAplicarComEstadoJaAplicadoAvisa(t *testing.T) {
	for _, st := range []string{UsbArmed, UsbActive, UsbStale, UsbUnknown} {
		be := newFakeUsb(st, true)
		tab := usbTab(t, be)
		ukey(tab, "a")
		if tab.asking || len(be.plans) != 0 || !strings.Contains(tab.Status(), "Já há modo empresa") {
			t.Errorf("%s: asking=%v plans=%d status=%q", st, tab.asking, len(be.plans), tab.Status())
		}
	}
}

func TestUsbTabNadaADesfazerQuandoNuncaAplicado(t *testing.T) {
	be := newFakeUsb(UsbClean, true)
	tab := usbTab(t, be)
	ukey(tab, "d")
	if tab.asking || !strings.Contains(tab.Status(), "Nada a desfazer") {
		t.Fatalf("status = %q", tab.Status())
	}
}

func TestUsbTabCapturingSoNaConfirmacaoEAplicacao(t *testing.T) {
	tab := usbTab(t, newFakeUsb(UsbClean, true))
	if tab.Capturing() {
		t.Fatal("na tela principal as teclas globais valem")
	}
	ukey(tab, "a")
	if !tab.Capturing() {
		t.Fatal("na confirmação todas as teclas são da seção (q, Tab e números não saem)")
	}
}

func TestUsbTabTextoMaliciosoNeutralizado(t *testing.T) {
	be := newFakeUsb(UsbClean, true)
	be.info.Devices[0].Name = "Teclado\x1b[31m\u202eevil"
	be.plan.Text = "Plano\x1b]0;x\x07 mal\n"
	tab := usbTab(t, be)
	out := tab.View(100, 30)
	if strings.ContainsRune(out, 0x202e) || strings.Contains(out, "\x1b]0;") {
		t.Fatalf("texto do sistema não foi neutralizado: %q", out)
	}
	ukey(tab, "p")
	out = tab.View(100, 30)
	if strings.Contains(out, "\x1b]0;") {
		t.Fatalf("plano com OSC: %q", out)
	}
}

// --- vocabulário único de teclas (keys.go) nos estados da seção ---

func TestUsbTabTeclasNoVocabularioUnico(t *testing.T) {
	check := func(state string, hints []KeyHint) {
		for _, h := range hints {
			r := []rune(h.Key)
			if len(r) != 1 || !unicode.IsLower(r[0]) && h.Key != "espaço" {
				continue
			}
			def := act(h.Key)
			if def.Label == "" {
				t.Errorf("%s: a tecla %q não está na legenda", state, h.Key)
				continue
			}
			ok := baseLabel(h.Label) == def.Label
			for _, a := range def.Alt {
				ok = ok || baseLabel(h.Label) == a
			}
			if !ok {
				t.Errorf("%s: %q com o rótulo %q; a legenda diz %q (variantes %v)", state, h.Key, baseLabel(h.Label), def.Label, def.Alt)
			}
			if strings.HasSuffix(h.Label, "…") != def.Confirms {
				t.Errorf("%s: %q reticências=%v, legenda diz %v", state, h.Key, strings.HasSuffix(h.Label, "…"), def.Confirms)
			}
		}
	}
	for _, st := range []string{UsbClean, UsbArmed, UsbActive, UsbStale, UsbUnknown} {
		tab := usbTab(t, newFakeUsb(st, true))
		check(st, tab.Keys())
		ukey(tab, "p")
		check(st+"/plano", tab.Keys())
		ukey(tab, "esc")
		if st == UsbClean {
			ukey(tab, "a")
			check(st+"/confirma", tab.Keys())
		}
		// a ajuda da seção usa as mesmas letras
		for _, d := range tab.HelpKeys() {
			if len([]rune(d.Key)) == 1 && d.Key != "" && act(d.Key).Label == "" && !unicode.IsUpper([]rune(d.Key)[0]) {
				t.Errorf("%s: a ajuda lista %q fora da legenda", st, d.Key)
			}
		}
	}
	if k := act("n"); k.Label != "manter" || !k.Confirms {
		t.Errorf("n = %+v", k)
	}
	// o título de aplicar tem as reticências (abre confirmação) e o de ver plano não
	hs := usbTab(t, newFakeUsb(UsbClean, true)).Keys()
	var a, p string
	for _, h := range hs {
		switch h.Key {
		case "a":
			a = h.Label
		case "p":
			p = h.Label
		}
	}
	if a != "aplicar…" || p != "ver plano" {
		t.Errorf("a=%q p=%q", a, p)
	}
	hs = usbTab(t, newFakeUsb(UsbArmed, true)).Keys()
	if hs[0].Key != "n" || hs[0].Label != "manter…" {
		t.Errorf("armado: %v", hs)
	}
}

func TestUsbTabNaoEstaNoTextoDaLegendaSemEstarNaTabela(t *testing.T) {
	// "manter" só existe na seção de USB; a tabela documenta o sentido
	if !strings.Contains(act("n").Help, "reversão automática") {
		t.Error("a ajuda de n deve falar da reversão automática")
	}
}

// --- goldens (80x24, 120x40 e 60x16) ---

func usbGolden(t *testing.T, name string, tab *UsbTab) {
	t.Helper()
	for _, sz := range [][2]int{{80, 24}, {120, 40}, {60, 16}} {
		m := sized(Options{Version: "v0.1.0", Mono: true, NoMotion: true}, sz[0], sz[1], tab)
		golden(t, fmt.Sprintf("usb_%s_%dx%d", name, sz[0], sz[1]), m.render())
	}
}

func TestGoldenUsbPrincipal(t *testing.T) {
	tab := usbTab(t, newFakeUsb(UsbClean, true))
	ukey(tab, "down", "space")
	usbGolden(t, "principal", tab)
}

func TestGoldenUsbConfirmacao(t *testing.T) {
	tab := usbTab(t, newFakeUsb(UsbClean, true))
	ukey(tab, "a")
	usbGolden(t, "confirma", tab)
}

func TestGoldenUsbConfirmacaoSemAdmin(t *testing.T) {
	be := newFakeUsb(UsbClean, false)
	be.plan.IsAdmin = false
	tab := usbTab(t, be)
	ukey(tab, "a")
	usbGolden(t, "confirma_semadmin", tab)
}

func TestGoldenUsbPlanoRecusado(t *testing.T) {
	be := newFakeUsb(UsbClean, true)
	be.plan = UsbPlan{Text: "Prova de dispositivos essenciais: RECUSADO, nada será alterado\n  NÃO Teclado USB (USB, USB): a classe USB é negada pela política e o dispositivo não está na lista de IDs\nRECUSADO: Teclado USB ficaria BLOQUEADO\n", OK: false, Refusal: "Teclado USB ficaria BLOQUEADO"}
	tab := usbTab(t, be)
	ukey(tab, "space", "a")
	usbGolden(t, "recusado", tab)
}

func TestGoldenUsbArmado(t *testing.T) {
	tab := usbTab(t, newFakeUsb(UsbArmed, true))
	usbGolden(t, "armado", tab)
}

func TestGoldenUsbManterConfirmacao(t *testing.T) {
	tab := usbTab(t, newFakeUsb(UsbArmed, true))
	ukey(tab, "n")
	usbGolden(t, "manter", tab)
}

func TestGoldenUsbEstadoDesconhecido(t *testing.T) {
	tab := usbTab(t, newFakeUsb(UsbUnknown, true))
	usbGolden(t, "desconhecido", tab)
}

func TestGoldenUsbForaDoWindowsEHome(t *testing.T) {
	linux := usbTab(t, &fakeUsb{info: UsbInfo{OS: "linux"}})
	usbGolden(t, "linux", linux)
	home := newFakeUsb(UsbClean, true)
	home.info.EditionOK, home.info.Edition = false, "home"
	usbGolden(t, "home", usbTab(t, home))
}

func TestGoldenUsbFaixaAplicado(t *testing.T) {
	be := newFakeUsb(UsbClean, true)
	be.applyRes = UsbResult{Entries: 8, Deadline: "12:10", BackupPath: `C:\ProgramData\toolkit`}
	tab := usbTab(t, be)
	be.info.State = UsbArmed // a releitura depois de aplicar mostra o estado armado
	ukey(tab, "a", "s")
	usbGolden(t, "faixa_aplicado", tab)
}

// A releitura que falha COM a confirmação aberta deixa a tela sem estado utilizável: s/y nunca aplicam o plano invisível.
func TestUsbTabSemEstadoUtilizavelSENuncaAplica(t *testing.T) {
	be := newFakeUsb(UsbClean, true)
	tab := usbTab(t, be)
	ukey(tab, "a")
	if !tab.asking {
		t.Fatal("a devia abrir a confirmação")
	}
	tab.Update(usbLoadedMsg{err: errors.New("reg.exe: acesso negado")}) // a leitura falhou com a confirmação na tela
	ukey(tab, "s", "y", "enter")
	if len(be.applies) != 0 {
		t.Fatal("sem estado utilizável s/y/enter nunca aplicam")
	}
	if tab.asking {
		t.Fatal("a confirmação invisível tem de ser fechada")
	}
}

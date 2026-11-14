package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/PiluTech/toolkit/internal/textsafe"
)

// Erros que o backend de USB devolve para a seção mostrar a instrução certa.
var (
	// ErrUsbNeedsAdmin: aplicar, manter e desfazer exigem administrador (nada foi mudado).
	ErrUsbNeedsAdmin = errors.New("usb: exige administrador")
	// ErrUsbStalePlan: a ação não corresponde ao plano mostrado (o sistema, o backup ou a seleção mudou, ou não há plano
	// preparado): nada foi alterado e é preciso ver o plano de novo.
	ErrUsbStalePlan = errors.New("usb: o plano mudou")
)

// Estados do modo empresa lidos do sistema.
const (
	UsbClean   = "clean"   // nunca aplicado
	UsbArmed   = "armed"   // aplicado, aguardando confirmação (a reversão automática está armada)
	UsbActive  = "active"  // aplicado e confirmado
	UsbStale   = "stale"   // sobra de uma aplicação que não chegou ao fim
	UsbUnknown = "unknown" // houve aplicação e o snapshot sumiu: ESTADO ORIGINAL DESCONHECIDO
)

// UsbDevice é um USB conectado agora. Key identifica a instância (a seleção é por ela).
type UsbDevice struct {
	Key       string
	Name      string
	VIDPID    string
	Class     string
	Essential bool // teclado, mouse ou outro HID: desmarcar recusa
}

// UsbInfo é o que a seção carrega. Supported=false fora do Windows. EditionOK=false no Home e nas edições sem suporte.
type UsbInfo struct {
	Supported bool
	OS        string
	Edition   string
	EditionOK bool
	IsAdmin   bool
	State     string
	Reason    string // motivo do estado desconhecido
	Deadline  string // "HH:MM" da reversão automática (estado armed)
	Devices   []UsbDevice
}

// UsbPlan é um plano pronto para mostrar (texto já formatado). OK=false quando recusado (sem prova, edição, política
// existente) ou quando o desfazer não pode restaurar (estado desconhecido, conflito): só leitura, nunca vira confirmação.
type UsbPlan struct {
	ID         string // ID opaco do plano: a confirmação age SÓ sobre o plano com esse ID
	Text       string
	OK         bool
	Refusal    string
	Prazo      int
	Allowed    int // dispositivos marcados (entram na lista)
	Deselected int // dispositivos desmarcados (ficam fora da lista)
	Blocked    int // dos desmarcados, quantos a política bloquearia ao reconectar
	IsAdmin    bool
}

// UsbResult é o que uma ação fez.
type UsbResult struct {
	Entries     int
	Restored    int
	Deadline    string // "HH:MM" da reversão automática (depois de aplicar)
	BackupPath  string
	RestoredNow bool // a falha no meio foi desfeita na hora
	Nothing     bool
}

// UsbBackend liga a seção ao pacote usb. Load e Plan/UndoPlan/KeepPlan só leem; Apply, Keep e Undo mudam o sistema e só são
// chamados depois da confirmação dentro da seção, sempre com o ID do plano mostrado (uso único).
type UsbBackend interface {
	Load(ctx context.Context) (UsbInfo, error)
	Plan(ctx context.Context, excluded []string) (UsbPlan, error)
	Apply(ctx context.Context, planID string) (UsbResult, error)
	KeepPlan(ctx context.Context) (UsbPlan, error)
	Keep(ctx context.Context, planID string) (UsbResult, error)
	UndoPlan(ctx context.Context) (UsbPlan, error)
	Undo(ctx context.Context, planID string) (UsbResult, error)
}

type usbKind int

const (
	usbView usbKind = iota
	usbApply
	usbKeep
	usbUndo
)

type usbLoadedMsg struct {
	info UsbInfo
	err  error
}

type usbPlanMsg struct {
	seq  int
	kind usbKind
	plan UsbPlan
	err  error
}

type usbDoneMsg struct {
	kind usbKind
	res  UsbResult
	err  error
}

// UsbTab é a seção "USB permitidos (experimental)" da área Segurança (só Windows).
type UsbTab struct {
	be    UsbBackend
	ctx   context.Context
	theme Theme

	loading bool
	loaded  bool
	loadErr error
	info    UsbInfo

	cur      int
	excluded map[string]bool // desmarcados (por Key)
	scroll   int
	lastW    int
	lastH    int
	frame    int

	busy    privBusy
	seq     int
	changed bool // alguma ação já mudou o sistema nesta sessão: o erro de releitura não pode dizer "Nada foi alterado"
	cancel  context.CancelFunc
	view    bool
	kind    usbKind
	plan    UsbPlan
	asking  bool
	conf    *Confirm
	notice  string
	warn    bool
	res     *Result
}

// NewUsbTab cria a seção. O backend decide o que é "suportado" (no real, GOOS == windows).
func NewUsbTab(ctx context.Context, be UsbBackend, th Theme) *UsbTab {
	return &UsbTab{be: be, ctx: ctx, theme: th, excluded: map[string]bool{}}
}

// SetContext troca o contexto das chamadas ao backend.
func (t *UsbTab) SetContext(ctx context.Context) { t.ctx = ctx }

// Title implementa Tab. O rótulo de experimental fica SEMPRE no título da seção.
func (t *UsbTab) Title() string { return "USB permitidos (experimental)" }

// Short implementa Tab.
func (t *UsbTab) Short() string { return "USB" }

// Init NÃO lê nada: a leitura do PnP varre todos os dispositivos e levaria vários segundos em TODA abertura da TUI no
// Windows. A primeira leitura é pedida com a tecla c (como na Otimização).
func (t *UsbTab) Init() tea.Cmd { return nil }

// Reload relê o estado depois de uma execução.
func (t *UsbTab) Reload() tea.Cmd { return t.loadCmd() }

func (t *UsbTab) loadCmd() tea.Cmd {
	be, ctx := t.be, t.ctx
	return func() tea.Msg {
		info, err := be.Load(ctx)
		return usbLoadedMsg{info: info, err: err}
	}
}

// Capturing implementa Tab: durante a confirmação e a aplicação todas as teclas são da seção.
func (t *UsbTab) Capturing() bool { return t.asking || t.busy == busyApply }

// Selected implementa Tab.
func (t *UsbTab) Selected() int { return 0 }

func (t *UsbTab) usable() bool {
	return t.loaded && !t.loading && t.loadErr == nil && t.info.Supported && t.info.EditionOK
}

// Result implementa Resulter.
func (t *UsbTab) Result() *Result { return t.res }

// Dismiss implementa Resulter.
func (t *UsbTab) Dismiss() { t.res = nil }

func (t *UsbTab) setNotice(s string, warn bool) { t.notice, t.warn = textsafe.Line(s), warn }

func cleanUsbInfo(i UsbInfo) UsbInfo {
	i.OS, i.Edition, i.Reason, i.Deadline = textsafe.Line(i.OS), textsafe.Line(i.Edition), textsafe.Line(i.Reason), textsafe.Line(i.Deadline)
	ds := make([]UsbDevice, len(i.Devices))
	for k, d := range i.Devices {
		ds[k] = UsbDevice{Key: d.Key, Name: textsafe.Line(d.Name), VIDPID: textsafe.Line(d.VIDPID), Class: textsafe.Line(d.Class), Essential: d.Essential}
	}
	i.Devices = ds
	return i
}

// Update trata dados carregados, planos, resultados, o spinner e as teclas.
func (t *UsbTab) Update(msg tea.Msg) (Tab, tea.Cmd) {
	switch m := msg.(type) {
	case usbLoadedMsg:
		t.loading = false
		if m.err != nil {
			t.loadErr = m.err
			return t, nil
		}
		t.loadErr, t.loaded = nil, true
		t.info = cleanUsbInfo(m.info)
		keep := map[string]bool{}
		for _, d := range t.info.Devices { // a seleção só vale para o que ainda está conectado
			if t.excluded[d.Key] {
				keep[d.Key] = true
			}
		}
		t.excluded = keep
		t.cur = max(min(t.cur, len(t.info.Devices)-1), 0)
	case usbPlanMsg:
		if m.seq != t.seq || t.busy != busyPlan {
			return t, nil // plano atrasado de uma tecla anterior
		}
		t.busy = busyNone
		if t.cancel != nil {
			t.cancel()
			t.cancel = nil
		}
		t.onPlan(m)
	case usbDoneMsg:
		// qualquer ação que terminou, ou que falhou depois de poder ter mexido no sistema, impede a tela de dizer "Nada foi
		// alterado" se a releitura falhar; só a recusa (plano velho, sem administrador) garante que nada mudou.
		if m.err == nil || m.res.Entries > 0 || m.res.Restored > 0 || m.res.RestoredNow ||
			(!errors.Is(m.err, ErrUsbStalePlan) && !errors.Is(m.err, ErrUsbNeedsAdmin)) {
			t.changed = true
		}
		t.onDone(m)
		return t, t.loadCmd()
	case tickMsg:
		t.frame++
		if t.loading || t.busy != busyNone {
			return t, t.theme.Tick()
		}
	case tea.KeyPressMsg:
		return t, t.key(m.String())
	}
	return t, nil
}

func (t *UsbTab) onPlan(m usbPlanMsg) {
	if !t.usable() { // sem tela utilizável o plano não vira confirmação
		t.asking, t.view, t.conf = false, false, nil
		return
	}
	t.scroll, t.asking = 0, false
	if m.err != nil {
		t.view = false
		t.setNotice("erro: "+m.err.Error(), true)
		return
	}
	t.view, t.kind, t.plan = true, m.kind, m.plan
	t.plan.Text = textsafe.Multiline(m.plan.Text)
	if !m.plan.OK { // recusado ou sem como restaurar: só leitura, nunca confirmação
		reason := textsafe.Line(m.plan.Refusal)
		if reason == "" {
			reason = "o plano foi recusado"
		}
		t.setNotice("Recusado: "+reason+". "+NothingChanged+" Esc volta.", true)
		return
	}
	if m.kind == usbView {
		t.setNotice("Só leitura: nada foi alterado. Esc volta.", false)
		return
	}
	t.asking, t.conf, t.notice = true, t.buildConfirm(), ""
}

// buildConfirm monta a confirmação única com a EVIDÊNCIA: a lista de dispositivos (no texto do plano), o que será
// permitido e bloqueado, o risco de trancar, o prazo e o roteiro de recuperação. Sem administrador o botão de confirmar fica
// indisponível com o motivo.
func (t *UsbTab) buildConfirm() *Confirm {
	p := t.plan
	reason := ""
	if !p.IsAdmin {
		reason = "sem administrador"
	}
	var c *Confirm
	switch t.kind {
	case usbKeep:
		c = NewConfirm("Manter", reason)
		c.Title = "Manter o modo empresa de USB? (experimental)"
		c.Summary = "Cancela a reversão automática: a política continua aplicada e só sai quando você desfizer (tecla d)."
		c.Warn = "Só mantenha depois de testar o teclado e o mouse e de ver que os USB de que você precisa funcionam. Isto reduz o risco, não o elimina: um dispositivo que clona o VID/PID de um permitido passa."
		c.Backup = "o snapshot protegido continua em C:\\ProgramData\\toolkit; é ele que o desfazer restaura."
		c.Undo = "tecla d desfaz a qualquer hora."
	case usbUndo:
		c = NewConfirm("Restaurar", reason)
		c.Title = "Desfazer o modo empresa de USB? (experimental)"
		c.Summary = "Restaura EXATAMENTE o snapshot validado (valores de outras origens ficam como estão) e remove a tarefa de reversão e o backup."
		c.Backup = "os valores originais vêm do snapshot protegido; ele é removido depois de restaurar."
		c.Undo = "para voltar ao modo empresa, aplique de novo (tecla a)."
	default:
		c = NewConfirm("Aplicar", reason)
		c.Title = "Permitir só os USB conhecidos? (experimental)"
		c.Summary = fmt.Sprintf("Libera %d dispositivo(s) USB conectado(s) agora; %d desmarcado(s) ficam fora da lista (a política bloquearia %d ao reconectar). Reverte SOZINHO em %d minuto(s) se você não manter (tecla n).", p.Allowed, p.Deselected, p.Blocked, p.Prazo)
		c.Warn = "EXPERIMENTAL, não testado numa máquina real. RISCO: se algo der errado o teclado pode travar; tenha um segundo teclado ou mouse de reserva já permitido. Reduz o risco, não o elimina: um dispositivo que clona o VID/PID de um permitido passa."
		c.Backup = "snapshot protegido em C:\\ProgramData\\toolkit (só SYSTEM e Administradores), relido e conferido; a tarefa de reversão é criada e conferida ANTES da primeira chave. Falha em qualquer passo = nenhuma alteração."
		c.Undo = fmt.Sprintf("n mantém (cancela a reversão); sem ação, volta sozinho em %d min; d desfaz. Recuperação manual (WinRE, outra conta de administrador) abaixo.", p.Prazo)
	}
	if !p.IsAdmin {
		c.Warn = "Sem administrador: nada será alterado. Cancele, abra o terminal como administrador (botão direito, \"Executar como administrador\") e tente de novo. " + NeverElevates + " " + c.Warn
	}
	c.Text = strings.TrimRight(p.Text, "\n")
	return c
}

func (t *UsbTab) onDone(m usbDoneMsg) {
	t.busy, t.asking, t.view, t.scroll, t.conf = busyNone, false, false, 0, nil
	what := map[usbKind]string{usbApply: "aplicar", usbKeep: "manter", usbUndo: "restaurar"}[m.kind]
	bk := ""
	if m.res.BackupPath != "" {
		bk = "Backup protegido em " + textsafe.Line(m.res.BackupPath)
	}
	switch {
	case m.err != nil && errors.Is(m.err, ErrUsbStalePlan):
		t.setNotice("O plano mudou: nada foi alterado.", true)
		t.res = &Result{Kind: ResultInfo, Title: "O plano mudou", Lines: []string{"O sistema, o backup ou a seleção mudou desde o plano mostrado: nada foi alterado.", "Veja o plano de novo (p, a, n ou d) antes de confirmar."}}
	case m.err != nil && errors.Is(m.err, ErrUsbNeedsAdmin):
		t.setNotice("Precisa de administrador: nada foi alterado.", true)
		t.res = &Result{Kind: ResultError, Title: "Precisa de administrador", Lines: adminResultLines()}
	case m.err != nil && m.res.RestoredNow:
		t.setNotice("erro: "+m.err.Error(), true)
		t.res = &Result{Kind: ResultError, Title: "Não foi possível aplicar", Lines: []string{"Erro: " + m.err.Error(), "A falha aconteceu no meio e o registro foi restaurado na hora: nada ficou aplicado."}}
	case m.err != nil:
		t.setNotice("erro: "+m.err.Error(), true)
		lines := []string{"Erro: " + m.err.Error()}
		if m.kind == usbApply {
			lines = append(lines, "Se algo ficou aplicado, a reversão automática restaura sozinha; ou use d (desfazer).")
		} else {
			lines = append(lines, "Veja o estado (tecla c) antes de tentar de novo.")
		}
		t.res = &Result{Kind: ResultError, Title: "Não foi possível " + what, Lines: lines}
	case m.res.Nothing:
		t.setNotice("Nada a fazer.", false)
		t.res = &Result{Kind: ResultInfo, Title: "Nada a fazer", Lines: []string{"Não havia nada aplicado nem pendente."}}
	case m.kind == usbUndo:
		t.setNotice(fmt.Sprintf("Restaurado: %d valor(es).", m.res.Restored), false)
		t.res = &Result{Kind: ResultOK, Title: "Modo empresa desfeito", Lines: []string{fmt.Sprintf("%d valor(es) voltaram ao original", m.res.Restored), "A tarefa de reversão e o backup foram removidos."}}
	case m.kind == usbKeep:
		t.setNotice("Modo empresa mantido.", false)
		t.res = &Result{Kind: ResultOK, Title: "Modo empresa mantido", Lines: []string{"A reversão automática foi cancelada; a política continua aplicada.", "Para desfazer: tecle d nesta seção."}}
	default:
		t.setNotice("Aplicado: aguardando a sua confirmação.", false)
		lines := []string{fmt.Sprintf("%d entrada(s) na política", m.res.Entries)}
		if m.res.Deadline != "" {
			lines = append(lines, "REVERTE SOZINHO às "+textsafe.Line(m.res.Deadline)+" se você não manter (tecla n)")
		}
		if bk != "" {
			lines = append(lines, bk)
		}
		lines = append(lines, "Teste o teclado e o mouse agora. Para desfazer: tecla d")
		t.res = &Result{Kind: ResultPending, Title: "Aplicado, aguardando confirmação", Lines: lines}
	}
	t.res = cleanResult(t.res)
}

// key trata as teclas. Mudar o sistema só acontece depois do plano na tela e do "s" (ou "y") na confirmação.
func (t *UsbTab) key(k string) tea.Cmd {
	if t.busy == busyPlan && (k == "esc" || k == "escape") {
		// Esc cancela a PREPARAÇÃO do plano: invalida a resposta (seq) e o contexto; uma ação já iniciada não é cancelável.
		t.seq++
		if t.cancel != nil {
			t.cancel()
			t.cancel = nil
		}
		t.busy, t.asking, t.view = busyNone, false, false
		t.setNotice("Preparação do plano cancelada: nada foi alterado.", false)
		return nil
	}
	if t.loading || t.busy != busyNone {
		return nil
	}
	if !t.usable() {
		// Carregando, com erro, vazio ou não suportado: nada que arme confirmação ou aja. Só reler (c).
		t.asking, t.view, t.conf = false, false, nil
		if k == "c" && (t.loadErr != nil || !t.loaded) {
			t.loading = true
			return tea.Batch(t.loadCmd(), t.theme.Tick())
		}
		return nil
	}
	if t.asking {
		switch t.conf.Update(k) {
		case ConfirmAccept:
			return t.confirmed()
		case ConfirmCancel:
			t.asking, t.view, t.conf, t.scroll = false, false, nil, 0
			t.setNotice("Cancelado: nada foi alterado.", false)
		}
		return nil
	}
	if t.view {
		switch k {
		case "up", "down", "pgup", "pgdown":
			t.scrollKey(k)
		case "esc", "enter", "left", "backspace":
			t.view, t.scroll, t.notice = false, 0, ""
		}
		return nil
	}
	t.notice = ""
	switch k {
	case "up", "k":
		t.cur = max(t.cur-1, 0)
	case "down", "j":
		t.cur = max(min(t.cur+1, len(t.info.Devices)-1), 0)
	case "pgup", "pgdown":
		t.scrollKey(k)
	case " ", "space":
		if t.info.State == UsbClean && t.cur < len(t.info.Devices) {
			key := t.info.Devices[t.cur].Key
			if t.excluded[key] {
				delete(t.excluded, key)
			} else {
				t.excluded[key] = true
			}
		}
	case "c":
		t.loading = true
		return tea.Batch(t.loadCmd(), t.theme.Tick())
	case "p":
		if t.info.State == UsbClean {
			return t.planCmd(usbView)
		}
		return t.planCmd(usbUndo) // fora do estado limpo, "ver plano" mostra o que o desfazer faria
	case "a":
		if t.info.State != UsbClean {
			t.setNotice("Já há modo empresa aplicado (ou uma sobra dele): veja o estado acima; d desfaz.", true)
			return nil
		}
		return t.planCmd(usbApply)
	case "n":
		if t.info.State != UsbArmed {
			t.setNotice("Não há reversão automática pendente para cancelar.", false)
			return nil
		}
		return t.planCmd(usbKeep)
	case "d":
		if t.info.State == UsbClean {
			t.setNotice("Nada a desfazer: o modo empresa de USB nunca foi aplicado aqui.", false)
			return nil
		}
		return t.planCmd(usbUndo)
	}
	return nil
}

func (t *UsbTab) scrollKey(k string) {
	page := max(t.lastH/2, 1)
	switch k {
	case "up":
		t.scroll = max(t.scroll-1, 0)
	case "down":
		t.scroll++
	case "pgup":
		t.scroll = max(t.scroll-page, 0)
	case "pgdown":
		t.scroll += page
	}
}

func (t *UsbTab) excludedKeys() []string {
	var out []string
	for _, d := range t.info.Devices { // na ordem da lista: determinístico
		if t.excluded[d.Key] {
			out = append(out, d.Key)
		}
	}
	return out
}

func (t *UsbTab) planCmd(kind usbKind) tea.Cmd {
	t.busy = busyPlan
	t.seq++
	pctx, cancel := context.WithCancel(t.ctx)
	t.cancel = cancel
	seq, be, ex := t.seq, t.be, t.excludedKeys()
	return tea.Batch(func() tea.Msg {
		var plan UsbPlan
		var err error
		switch kind {
		case usbUndo:
			plan, err = be.UndoPlan(pctx)
		case usbKeep:
			plan, err = be.KeepPlan(pctx)
		default:
			plan, err = be.Plan(pctx, ex)
		}
		return usbPlanMsg{seq: seq, kind: kind, plan: plan, err: err}
	}, t.theme.Tick())
}

func (t *UsbTab) confirmed() tea.Cmd {
	if !t.usable() || !t.asking || t.conf == nil {
		t.asking, t.view, t.conf = false, false, nil
		return nil
	}
	t.asking, t.conf, t.busy, t.notice, t.res = false, nil, busyApply, "", nil
	be, ctx, kind, id := t.be, t.ctx, t.kind, t.plan.ID
	return tea.Batch(func() tea.Msg {
		var res UsbResult
		var err error
		switch kind {
		case usbUndo:
			res, err = be.Undo(ctx, id)
		case usbKeep:
			res, err = be.Keep(ctx, id)
		default:
			res, err = be.Apply(ctx, id)
		}
		return usbDoneMsg{kind: kind, res: res, err: err}
	}, t.theme.Tick())
}

// Keys implementa Tab (vocabulário único de keys.go).
func (t *UsbTab) Keys() []KeyHint {
	switch {
	case !t.loaded && !t.loading && t.loadErr == nil:
		return []KeyHint{act("c").Hint()}
	case t.loading || t.loadErr != nil || !t.usable():
		return nil
	case t.busy == busyApply:
		return nil
	case t.busy == busyPlan:
		return []KeyHint{{"Esc", "cancelar"}}
	case t.asking:
		return t.conf.keyHints()
	case t.view:
		return []KeyHint{{"↑↓", "rolar"}, {"Esc", "voltar"}}
	}
	switch t.info.State {
	case UsbClean:
		return []KeyHint{{"↑↓", "dispositivo"}, act("espaço").Hint(), act("p").Hint(), act("a").Hint(), act("c").Hint()}
	case UsbArmed:
		return []KeyHint{act("n").Hint(), act("d").Hint(), act("p").Hint(), act("c").Hint()}
	case UsbUnknown:
		return []KeyHint{act("p").Hint(), act("c").Hint()}
	}
	return []KeyHint{act("d").Hint(), act("p").Hint(), act("c").Hint()}
}

// HelpKeys implementa SectionHelper.
func (t *UsbTab) HelpKeys() []KeyDef {
	if !t.loaded && !t.loading && t.loadErr == nil {
		return []KeyDef{act("c").says("lê os dispositivos USB e o estado (a primeira leitura varre o PnP e leva alguns segundos).")}
	}
	if !t.usable() {
		return noActionsHelp(t.loading || t.loadErr != nil)
	}
	switch t.info.State {
	case UsbClean:
		return []KeyDef{
			navDef("↑↓", "dispositivo", "escolhe o dispositivo USB da lista."),
			act("espaço").says("marca ou desmarca o dispositivo: desmarcado fica bloqueado ao reconectar (teclado e mouse desmarcados são recusados)."),
			act("p").says("mostra o plano e a prova dos dispositivos essenciais, sem alterar nada."),
			act("a").says("aplica a política depois da confirmação (administrador); reverte sozinha se você não manter."),
			act("c").says("lê o estado outra vez."),
		}
	case UsbArmed:
		return []KeyDef{
			act("n").says("mantém o modo empresa e cancela a reversão automática, depois da confirmação."),
			act("d").says("desfaz agora, a partir do snapshot protegido, depois da confirmação."),
			act("p").says("mostra o que o desfazer restauraria, sem alterar nada."),
			act("c").says("lê o estado outra vez."),
		}
	case UsbUnknown:
		return []KeyDef{
			act("p").says("mostra a recuperação manual: sem snapshot o toolkit não restaura nada às cegas."),
			act("c").says("lê o estado outra vez."),
		}
	}
	return []KeyDef{
		act("d").says("desfaz o modo empresa, a partir do snapshot protegido, depois da confirmação."),
		act("p").says("mostra o que o desfazer restauraria, sem alterar nada."),
		act("c").says("lê o estado outra vez."),
	}
}

// Status implementa Tab.
func (t *UsbTab) Status() string {
	switch {
	case t.asking && t.conf != nil:
		return t.conf.Hint()
	case t.busy == busyApply:
		return t.theme.Spin(t.frame) + " Aplicando… não feche a janela."
	case t.busy == busyPlan:
		return t.theme.Spin(t.frame) + " Preparando o plano… Esc cancela."
	case t.loading:
		return "Lendo os dispositivos USB…"
	case !t.loaded && t.loadErr == nil:
		return "Ainda não lido. Tecle c para ler."
	case t.loadErr != nil:
		return "Falhou. c lê de novo."
	case t.notice != "" && t.warn:
		return SymWarn + " " + t.notice
	case t.notice != "":
		return t.notice
	}
	return "USB permitidos (experimental) — nada muda antes de confirmar"
}

// More implementa Tab.
func (t *UsbTab) More() int {
	if !t.usable() || t.asking {
		return 0 // a confirmação mostra o próprio "↓ mais N linhas"
	}
	if t.view {
		return max(len(t.planLines(t.lastW))-(t.scroll+t.lastH-1), 0)
	}
	return max(len(t.mainLines(t.lastW))-(t.scroll+t.lastH), 0)
}

// View desenha o corpo.
func (t *UsbTab) View(w, h int) string {
	t.lastW, t.lastH = w, h
	switch {
	case t.loading:
		return strings.Join(StateLoading(t.theme, w, h, "os dispositivos USB", t.frame), "\n")
	case !t.loaded && t.loadErr == nil:
		return strings.Join(StateEmpty(t.theme, w, h, "Os dispositivos USB ainda não foram lidos (a leitura varre todos os dispositivos e leva alguns segundos).", "Tecle c para ler. Nada muda: a leitura é só leitura."), "\n")
	case t.loadErr != nil && t.changed:
		return strings.Join(StateErrorAfterChange(t.theme, w, h, "os USB permitidos", t.loadErr, "Tecle c para reler o estado."), "\n")
	case t.loadErr != nil:
		return strings.Join(StateError(t.theme, w, h, "os USB permitidos", t.loadErr, "Tente de novo (c)."), "\n")
	case !t.info.Supported:
		return strings.Join(StateUnsupported(t.theme, w, h, "o modo empresa de USB", unsupportedWhy(t.info.OS, "a política de restrição de instalação de dispositivos"), osName(t.info.OS)), "\n")
	case !t.info.EditionOK:
		return strings.Join(StateUnsupportedEdition(t.theme, w, h, "o modo empresa de USB", t.info.Edition, "A Microsoft não documenta a política de restrição de instalação de dispositivos para esta edição (só Pro, Enterprise e Education): o toolkit recusa em vez de prometer o que não está documentado."), "\n")
	}
	if t.asking && t.conf != nil {
		return strings.Join(t.conf.View(t.theme, w, h), "\n")
	}
	var lines []string
	if t.view {
		lines = t.fitPlan(w, h)
	} else {
		all := t.mainLines(w)
		t.scroll = max(min(t.scroll, max(len(all)-h, 0)), 0)
		lines = all[min(t.scroll, len(all)):min(t.scroll+h, len(all))]
	}
	for i := range lines {
		lines[i] = clip(lines[i], w)
	}
	return strings.Join(lines, "\n")
}

func (t *UsbTab) fitPlan(w, h int) []string {
	all := t.planLines(w)
	room := max(h-1, 1)
	t.scroll = max(min(t.scroll, max(len(all)-room, 0)), 0)
	title := "Plano do modo empresa (experimental)"
	switch t.kind {
	case usbUndo:
		title = "O que o desfazer faria"
	case usbKeep:
		title = "Manter o modo empresa"
	}
	head := " " + t.theme.Title().Render(titleCase(t.theme, title))
	return append([]string{head}, all[min(t.scroll, len(all)):min(t.scroll+room, len(all))]...)
}

func (t *UsbTab) planLines(w int) []string {
	var out []string
	for _, l := range strings.Split(strings.TrimRight(t.plan.Text, "\n"), "\n") {
		if strings.TrimSpace(l) == "" {
			out = append(out, "")
			continue
		}
		out = append(out, wrapLine(" "+l, w)...)
	}
	return out
}

func (t *UsbTab) mainLines(w int) []string {
	var out []string
	out = append(out, " "+t.theme.Title().Render(titleCase(t.theme, "Modo empresa: USB permitidos"))+" · "+t.theme.Warn().Render("[experimental]")+" · só Windows")
	out = append(out, wrapLine(" "+t.theme.Warn().Render(SymWarn+" experimental:")+" ainda não foi testado numa máquina real (a verificar em VM). Nada se aplica por padrão e nada muda antes de você confirmar. Isto reduz o risco, não o elimina: um dispositivo que clona o VID/PID de um permitido passa.", w)...)
	adm := "administrador: não (aplicar exige; o toolkit nunca eleva sozinho)"
	if t.info.IsAdmin {
		adm = "administrador: sim"
	}
	out = append(out, wrapLine(" edição "+t.info.Edition+" · "+adm, w)...)
	out = append(out, t.stateLines(w)...)
	title := " USB conectados agora "
	out = append(out, SymRule+t.theme.Title().Render(title)+strings.Repeat(SymRule, max(w-lipgloss.Width(title)-1, 1)))
	if len(t.info.Devices) == 0 {
		out = append(out, " Nenhum dispositivo USB externo conectado.")
	}
	for i, d := range t.info.Devices {
		cursor := " "
		if i == t.cur {
			cursor = ">"
		}
		mark, word := "[x]", "entra na lista"
		if t.info.State != UsbClean {
			mark, word = "   ", "conectado"
		} else if t.excluded[d.Key] {
			mark, word = "[ ]", "desmarcado, fora da lista (o plano diz se será bloqueado)"
		}
		ess := ""
		if d.Essential {
			ess = " · essencial (teclado, mouse ou outro HID)"
		}
		prefix := " " + cursor + " " + mark + " "
		out = append(out, hangLines(prefix, lipgloss.Width(prefix), fmt.Sprintf("%s  %s  classe %s%s -> %s", d.Name, d.VIDPID, dash(d.Class), ess, word), w)...)
	}
	out = append(out, "")
	out = append(out, wrapLine(" A prova de que teclado, mouse, disco e controladores continuam permitidos aparece no plano (tecla p ou a). Sem prova, o toolkit recusa e não muda nada.", w)...)
	return out
}

func dash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func (t *UsbTab) stateLines(w int) []string {
	switch t.info.State {
	case UsbArmed:
		when := ""
		if t.info.Deadline != "" {
			when = " REVERTE SOZINHO às " + t.info.Deadline + "."
		}
		return wrapLine(" "+t.theme.Warn().Render(SymWarn+" Aplicado, aguardando confirmação.")+when+" n mantém (cancela a reversão); d desfaz agora.", w)
	case UsbActive:
		return wrapLine(" "+t.theme.OK().Render(SymOK+" Aplicado e confirmado.")+" d desfaz.", w)
	case UsbStale:
		return wrapLine(" "+t.theme.Warn().Render(SymWarn+" Sobra de uma aplicação que não chegou ao fim.")+" d limpa o backup e a tarefa.", w)
	case UsbUnknown:
		out := wrapLine(" "+t.theme.Warn().Render(SymWarn+" ESTADO ORIGINAL DESCONHECIDO.")+" "+t.info.Reason+". O toolkit não restaura nada às cegas; p mostra a recuperação manual.", w)
		return out
	}
	return wrapLine(" Estado: nunca aplicado nesta máquina. espaço marca ou desmarca; p mostra o plano.", w)
}

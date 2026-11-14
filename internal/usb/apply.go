package usb

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/PiluTech/toolkit/internal/textsafe"
)

// ApplyOptions são os parâmetros do Apply. Excluded são as instâncias desmarcadas (as mesmas do plano mostrado).
type ApplyOptions struct {
	Prazo    int
	Now      time.Time
	Excluded []string
	// ExpectedPlan é o texto do plano que foi MOSTRADO e confirmado (Plan.Format com o mesmo Prazo). Vazio = não confere. O
	// Apply recalcula o plano e recusa (ErrPlanChanged) ANTES do backup se ele for diferente: o que é gravado é o que foi mostrado.
	ExpectedPlan string
}

// ApplyResult é o que o Apply fez. Restored=true quando uma falha no meio foi desfeita na hora.
type ApplyResult struct {
	Deadline   time.Time
	Entries    int
	Restored   bool
	BackupPath string
}

func taskExe() string { return ProtectedDir + `\` + RevertBinary }

var systemUsers = []string{"SYSTEM", "S-1-5-18", `NT AUTHORITY\SYSTEM`}

// verifyTask confere a tarefa lida contra o que o toolkit armou: existe, habilitada, o comando EXATO, como SYSTEM e com o
// próximo disparo dentro do prazo (no futuro e até o prazo mais uma folga).
func verifyTask(ti TaskInfo, exe string, now time.Time, prazo int) error {
	bad := func(format string, a ...any) error {
		return fmt.Errorf("%w: %s", ErrTaskUnexpected, fmt.Sprintf(format, a...))
	}
	switch {
	case !ti.Exists:
		return bad("a tarefa não existe")
	case !ti.Enabled:
		return bad("a tarefa está desabilitada")
	case !strings.EqualFold(ti.Exe, exe):
		return bad("o programa da tarefa não é o esperado")
	case ti.Args != RevertArgs:
		return bad("o comando da tarefa não é o esperado")
	}
	sys := false
	for _, u := range systemUsers {
		if strings.EqualFold(ti.User, u) {
			sys = true
		}
	}
	if !sys {
		return bad("a tarefa não roda como SYSTEM")
	}
	if ti.Next.IsZero() || !ti.Next.After(now) {
		return bad("o próximo disparo não é no futuro")
	}
	if ti.Next.After(now.Add(time.Duration(prazo)*time.Minute + 2*time.Minute)) {
		return bad("o próximo disparo passa do prazo")
	}
	return nil
}

// Apply aplica a política, nesta ORDEM obrigatória: prazo -> edição -> administrador -> (sem backup nem tarefa anteriores) ->
// plano com a PROVA dos essenciais -> pasta protegida e backup (relido e conferido) -> cópia do binário -> tarefa de
// reversão criada e CONFERIDA -> só então as chaves. Falha em qualquer passo antes da primeira chave = nenhuma alteração
// (a tarefa e o backup deste Apply são removidos). Falha no meio das chaves tenta restaurar na hora; se não conseguir, a
// tarefa e o backup ficam como reserva. A confirmação do usuário é de quem chama; o Apply nunca pergunta e nunca eleva.
func Apply(ctx context.Context, sys System, st Store, o ApplyOptions) (*ApplyResult, error) {
	res := &ApplyResult{}
	if o.Prazo < PrazoMinimo || o.Prazo > PrazoMaximo {
		return res, fmt.Errorf("%w: use de %d a %d minutos", ErrPrazo, PrazoMinimo, PrazoMaximo)
	}
	ed, err := sys.Edition(ctx)
	if err != nil {
		return res, fmt.Errorf("não deu para descobrir a edição do Windows: %w", err)
	}
	if !EditionSupported(ed) {
		return res, fmt.Errorf("%w (%s)", ErrEdition, textsafe.Line(ed))
	}
	if !sys.IsAdmin(ctx) {
		return res, ErrNeedsAdmin
	}
	if sn, lerr := st.Load(); sn != nil || lerr != nil {
		return res, fmt.Errorf("%w: já existe um backup de uma aplicação anterior; veja `toolkit usb` e desfaça antes de aplicar de novo", ErrExistingPolicy)
	}
	if ti, terr := sys.ReadTask(ctx); terr != nil {
		return res, fmt.Errorf("não deu para ler a tarefa de reversão: %w", terr)
	} else if ti.Exists {
		return res, fmt.Errorf("%w: já existe a tarefa de reversão do toolkit; veja `toolkit usb`", ErrExistingPolicy)
	}
	plan, err := BuildPlan(ctx, sys, o.Excluded)
	if err != nil {
		return res, err
	}
	if !plan.OK() {
		return res, plan.Err()
	}
	plan.Prazo = o.Prazo
	if o.ExpectedPlan != "" && plan.Format() != o.ExpectedPlan {
		return res, ErrPlanChanged
	}
	tr, err := sys.ReadTree(ctx)
	if err != nil {
		return res, fmt.Errorf("não deu para ler a chave Restrictions: %w", err)
	}
	entries := plan.Policy.Entries()
	sn := &Snapshot{Version: 1, Created: o.Now.UTC(), KeyExisted: tr.KeyExists, SubkeysExisted: []string{}, TaskExe: taskExe(), Prazo: o.Prazo}
	for _, e := range entries {
		if err := checkWrite(e.Path, e.Value); err != nil {
			return res, err
		}
		sn.Targets = append(sn.Targets, Target{Path: e.Path, Existed: false, Want: e.Value})
	}

	// (b) backup protegido, relido e conferido
	abort := func(cause error) (*ApplyResult, error) {
		_ = sys.DeleteTask(ctx)
		_ = st.Remove()
		return res, cause
	}
	if err := sys.ProtectDir(ctx, st.Folder()); err != nil {
		return res, fmt.Errorf("não deu para proteger a pasta do backup (nada foi alterado): %w", err)
	}
	if tr.KeyExists {
		if err := sys.ExportReg(ctx, st.RegPath()); err != nil {
			return abort(fmt.Errorf("não deu para exportar o .reg de segurança (nada foi alterado): %w", err))
		}
		h, err := st.FileSHA256(st.RegPath())
		if err != nil {
			return abort(fmt.Errorf("não deu para conferir o .reg exportado (nada foi alterado): %w", err))
		}
		sn.RegFile, sn.RegSHA256 = RegFile, h
	}
	if err := st.Save(sn); err != nil {
		return abort(fmt.Errorf("não deu para gravar o backup (nada foi alterado): %w", err))
	}
	back, err := st.Load()
	if err != nil {
		return abort(fmt.Errorf("o backup relido é inválido (nada foi alterado): %w", err))
	}
	if back == nil || !reflect.DeepEqual(back, normalize(sn)) {
		return abort(errors.New("o backup relido não confere com o gravado (nada foi alterado)"))
	}
	res.BackupPath = st.Folder()

	// (c) cópia do binário e tarefa de reversão, conferida ANTES da primeira chave
	sb, err := sys.StageBinary(ctx, st.Folder())
	if err != nil {
		return abort(fmt.Errorf("não deu para copiar o toolkit para a pasta protegida (nada foi alterado): %w", err))
	}
	if !strings.EqualFold(sb.Path, sn.TaskExe) || sb.SourceSHA == "" || sb.SourceSHA != sb.CopySHA {
		return abort(fmt.Errorf("a cópia do toolkit na pasta protegida não confere com o original (nada foi alterado)"))
	}
	if err := sys.ArmTask(ctx, TaskSpec{Exe: sb.Path, Args: RevertArgs, Minutes: o.Prazo}); err != nil {
		return abort(fmt.Errorf("não deu para criar a tarefa de reversão (nada foi alterado): %w", err))
	}
	ti, err := sys.ReadTask(ctx)
	if err != nil {
		return abort(fmt.Errorf("não deu para conferir a tarefa de reversão (nada foi alterado): %w", err))
	}
	if err := verifyTask(ti, sb.Path, o.Now, o.Prazo); err != nil {
		return abort(fmt.Errorf("tarefa de reversão não conferida (nada foi alterado): %w", err))
	}
	res.Deadline = ti.Next

	// (e) as chaves, na ordem segura
	for _, e := range entries {
		if err := sys.RegWrite(ctx, e.Path, e.Value); err != nil {
			return res, failMidway(ctx, sys, st, res, fmt.Errorf("falha ao gravar %s: %w", e.Path, err))
		}
	}
	got, err := sys.ReadTree(ctx)
	if err != nil {
		return res, failMidway(ctx, sys, st, res, fmt.Errorf("não deu para reler a chave depois de gravar: %w", err))
	}
	for _, e := range entries {
		if v, ok := got.Entries[e.Path]; !ok || v != e.Value {
			return res, failMidway(ctx, sys, st, res, fmt.Errorf("a releitura não encontrou %s como gravado", e.Path))
		}
	}
	res.Entries = len(entries)
	return res, nil
}

// normalize devolve o snapshot como ele volta do JSON (nil vs vazio, tempo em UTC) para a comparação do relido.
func normalize(sn *Snapshot) *Snapshot {
	cp := *sn
	cp.Created = sn.Created.UTC()
	if cp.SubkeysExisted == nil {
		cp.SubkeysExisted = []string{}
	}
	return &cp
}

// failMidway tenta restaurar na hora depois de uma falha no meio das chaves. Se restaurar, a tarefa e o backup saem
// (Undo); se não, ficam como reserva.
func failMidway(ctx context.Context, sys System, st Store, res *ApplyResult, cause error) error {
	if _, err := Undo(ctx, sys, st); err != nil {
		return fmt.Errorf("%w; a restauração imediata também falhou (%w): a tarefa de reversão e o backup ficam como reserva", cause, err)
	}
	res.Restored = true
	return fmt.Errorf("%w; o registro foi restaurado na hora", cause)
}

// --- estado ---

// StatusKind é o estado do modo empresa nesta máquina.
type StatusKind int

// Estados.
const (
	StatusClean   StatusKind = iota // nunca houve aplicação
	StatusArmed                     // aplicado e aguardando confirmação (a tarefa de reversão está armada)
	StatusActive                    // aplicado e confirmado
	StatusStale                     // há backup mas nada aplicado nem tarefa (sobra de uma falha)
	StatusUnknown                   // houve aplicação e o snapshot sumiu ou é inválido: ESTADO ORIGINAL DESCONHECIDO
)

// Status é o estado lido (só leitura).
type Status struct {
	Kind     StatusKind
	Snapshot *Snapshot
	Task     TaskInfo
	Tree     RegTree
	Reason   string
}

// ReadStatus lê o registro, a tarefa e o backup. Falha de leitura é ERRO (nunca vira "limpo").
func ReadStatus(ctx context.Context, sys System, st Store) (*Status, error) {
	tr, err := sys.ReadTree(ctx)
	if err != nil {
		return nil, fmt.Errorf("não deu para ler a chave Restrictions: %w", err)
	}
	ti, err := sys.ReadTask(ctx)
	if err != nil {
		return nil, fmt.Errorf("não deu para ler a tarefa de reversão: %w", err)
	}
	s := &Status{Task: ti, Tree: tr}
	sn, lerr := st.Load()
	switch {
	case lerr != nil && errors.Is(lerr, ErrBackupInvalid):
		s.Kind, s.Reason = StatusUnknown, "o backup está corrompido, adulterado ou inválido: "+textsafe.Line(lerr.Error())
	case lerr != nil:
		return nil, fmt.Errorf("não deu para ler o backup: %w", lerr)
	case sn == nil && (ti.Exists || namespacePresent(tr)):
		s.Kind, s.Reason = StatusUnknown, "o backup não existe, mas há sinais de aplicação (a tarefa de reversão existe ou há chaves da lista do toolkit)"
	case sn == nil:
		s.Kind = StatusClean
	case ti.Exists:
		s.Kind, s.Snapshot = StatusArmed, sn
	case namespacePresent(tr):
		s.Kind, s.Snapshot = StatusActive, sn
	default:
		s.Kind, s.Snapshot = StatusStale, sn
	}
	return s, nil
}

// --- desfazer ---

// UndoKind é o que o desfazer faria.
type UndoKind int

// Tipos de desfazer.
const (
	UndoNothing  UndoKind = iota // nunca houve aplicação
	UndoRestore                  // restaura o snapshot validado
	UndoUnknown                  // ESTADO ORIGINAL DESCONHECIDO: nada é escrito
	UndoConflict                 // um valor gravado pelo toolkit foi alterado por fora: nada é escrito
)

// UndoPlan é o plano do desfazer (só leitura).
type UndoPlan struct {
	Kind      UndoKind
	Reason    string
	Actions   []string // texto: o que será restaurado
	Preserved int      // valores de outras origens que ficam como estão
	Status    *Status
	actions   []undoAction
}

type undoAction struct {
	path   string
	del    bool
	value  RegValue
	target Target
}

// BuildUndo calcula o desfazer sem escrever nada. Snapshot ausente/corrompido depois de uma aplicação = UndoUnknown;
// "nada a desfazer" só quando nunca houve aplicação.
func BuildUndo(ctx context.Context, sys System, st Store) (*UndoPlan, error) {
	s, err := ReadStatus(ctx, sys, st)
	if err != nil {
		return nil, err
	}
	p := &UndoPlan{Status: s}
	switch s.Kind {
	case StatusClean:
		p.Kind = UndoNothing
		return p, nil
	case StatusUnknown:
		p.Kind, p.Reason = UndoUnknown, s.Reason
		return p, nil
	}
	sn := s.Snapshot
	var conflicts []string
	for i := len(sn.Targets) - 1; i >= 0; i-- { // ordem inversa da escrita: a negação sai primeiro
		t := sn.Targets[i]
		cur, exists := s.Tree.Entries[t.Path]
		switch {
		case exists && cur == t.Want:
			if t.Existed {
				p.actions = append(p.actions, undoAction{path: t.Path, value: t.Orig, target: t})
				p.Actions = append(p.Actions, fmt.Sprintf("%s: devolve o valor original", t.Path))
			} else {
				p.actions = append(p.actions, undoAction{path: t.Path, del: true, target: t})
				p.Actions = append(p.Actions, fmt.Sprintf("%s: remove (não existia antes)", t.Path))
			}
		case !exists && t.Existed:
			p.actions = append(p.actions, undoAction{path: t.Path, value: t.Orig, target: t})
			p.Actions = append(p.Actions, fmt.Sprintf("%s: devolve o valor original (foi apagado)", t.Path))
		case !exists, exists && t.Existed && cur == t.Orig:
			// já está como antes
		default:
			conflicts = append(conflicts, t.Path)
		}
	}
	inSnapshot := map[string]bool{}
	for _, t := range sn.Targets {
		inSnapshot[t.Path] = true
	}
	for k := range s.Tree.Entries {
		if !inSnapshot[k] {
			p.Preserved++
		}
	}
	if len(conflicts) > 0 {
		p.Kind = UndoConflict
		p.Reason = fmt.Sprintf("o valor de %s foi alterado por outra origem depois da aplicação (GPO, MDM ou outra ferramenta); o toolkit não sobrescreve", strings.Join(conflicts, ", "))
		return p, nil
	}
	p.Kind = UndoRestore
	return p, nil
}

// Format escreve o plano do desfazer em texto.
func (p *UndoPlan) Format() string {
	var b strings.Builder
	w := func(format string, a ...any) { fmt.Fprintf(&b, format+"\n", a...) }
	switch p.Kind {
	case UndoNothing:
		w("Nada a desfazer: o toolkit nunca aplicou o modo empresa de USB nesta máquina.")
	case UndoUnknown:
		w("ESTADO ORIGINAL DESCONHECIDO")
		w("%s.", textsafe.Line(p.Reason))
		w("Nada será escrito no registro: restaurar às cegas poderia apagar uma política que não é do toolkit. O arquivo de backup (se existir) é mantido para análise.")
		w("")
		w("Recuperação manual (por uma conta de administrador):")
		for _, s := range RecoverySteps {
			w("  %s", s)
		}
		w("  Antes de apagar qualquer chave, confirmar que não havia política anterior com quem administra a máquina (por exemplo, GPO de domínio). Sem o .reg íntegro, o toolkit não tem como saber o estado original.")
	case UndoConflict:
		w("Não dá para restaurar: %s.", textsafe.Line(p.Reason))
		w("Nada será escrito. Confira com quem administra a máquina.")
	case UndoRestore:
		w("O que será restaurado (snapshot validado; só a lista fechada de valores do toolkit):")
		for _, a := range p.Actions {
			w("  - %s", a)
		}
		if len(p.Actions) == 0 {
			w("  (nada a mudar no registro: tudo já está como antes; só o backup e a tarefa de reversão saem)")
		}
		if p.Preserved > 0 {
			w("Preservados (de outras origens, não são do toolkit): %d valor(es).", p.Preserved)
		}
		w("A chave Restrictions só é removida se estiver vazia e o snapshot registrar que ela não existia.")
	}
	return b.String()
}

// UndoResult é o que o Undo fez.
type UndoResult struct {
	Nothing  bool
	Restored int
}

// Undo restaura EXATAMENTE o snapshot validado (o mesmo caminho do `usb desfazer` e da tarefa de reversão). Nunca apaga a
// chave Restrictions às cegas; só remove o que o toolkit criou. Estado original desconhecido ou conflito = erro, sem escrever.
// Exige administrador (conferido antes de qualquer escrita).
func Undo(ctx context.Context, sys System, st Store) (*UndoResult, error) {
	p, err := BuildUndo(ctx, sys, st)
	if err != nil {
		return nil, err
	}
	switch p.Kind {
	case UndoNothing:
		return &UndoResult{Nothing: true}, nil
	case UndoUnknown:
		return nil, fmt.Errorf("%w: %s", ErrOriginalUnknown, textsafe.Line(p.Reason))
	case UndoConflict:
		return nil, fmt.Errorf("%w: %s", ErrConflict, textsafe.Line(p.Reason))
	}
	if !sys.IsAdmin(ctx) {
		return nil, ErrNeedsAdmin
	}
	sn := p.Status.Snapshot
	res := &UndoResult{}
	for _, a := range p.actions {
		var err error
		if a.del {
			err = sys.RegDelete(ctx, a.path)
		} else {
			err = checkRestore(a.path, a.value)
			if err == nil {
				err = sys.RegWrite(ctx, a.path, a.value)
			}
		}
		if err != nil {
			return res, fmt.Errorf("falha ao restaurar %s: %w (o backup e a tarefa de reversão ficam)", a.path, err)
		}
		res.Restored++
	}
	// subchaves e chave que o toolkit criou: só depois de conferir que ficaram vazias
	for _, sub := range []string{nameDeny, nameAllow} {
		tr, err := sys.ReadTree(ctx)
		if err != nil {
			return res, fmt.Errorf("não deu para reler a chave: %w", err)
		}
		if hasString(tr.Subkeys, sub) && !hasString(sn.SubkeysExisted, sub) && !hasEntriesUnder(tr, sub) {
			if err := sys.KeyDelete(ctx, sub); err != nil {
				return res, fmt.Errorf("falha ao remover a subchave %s: %w", sub, err)
			}
		}
	}
	tr, err := sys.ReadTree(ctx)
	if err != nil {
		return res, fmt.Errorf("não deu para reler a chave: %w", err)
	}
	if tr.KeyExists && !sn.KeyExisted && len(tr.Entries) == 0 && len(tr.Subkeys) == 0 {
		if err := sys.KeyDelete(ctx, ""); err != nil {
			return res, fmt.Errorf("falha ao remover a chave Restrictions: %w", err)
		}
	}
	// conferência final: cada alvo voltou ao original
	tr, err = sys.ReadTree(ctx)
	if err != nil {
		return res, fmt.Errorf("não deu para reler a chave: %w", err)
	}
	for _, t := range sn.Targets {
		cur, exists := tr.Entries[t.Path]
		if (t.Existed && (!exists || cur != t.Orig)) || (!t.Existed && exists) {
			return res, fmt.Errorf("depois de restaurar, %s não voltou ao original (o backup e a tarefa ficam)", t.Path)
		}
	}
	if ti, err := sys.ReadTask(ctx); err != nil {
		return res, fmt.Errorf("não deu para ler a tarefa de reversão: %w", err)
	} else if ti.Exists {
		if err := sys.DeleteTask(ctx); err != nil {
			return res, fmt.Errorf("o registro foi restaurado, mas não deu para remover a tarefa de reversão: %w", err)
		}
	}
	if err := st.Remove(); err != nil {
		return res, fmt.Errorf("o registro foi restaurado, mas não deu para remover o backup: %w", err)
	}
	return res, nil
}

func checkRestore(path string, v RegValue) error {
	if err := checkRel(path); err != nil {
		return err
	}
	return checkOrigText(v)
}

func hasString(l []string, s string) bool {
	for _, x := range l {
		if x == s {
			return true
		}
	}
	return false
}

func hasEntriesUnder(tr RegTree, sub string) bool {
	for k := range tr.Entries {
		if strings.HasPrefix(k, sub+`\`) {
			return true
		}
	}
	return false
}

// Confirm cancela a reversão automática (remove a tarefa) e mantém a política e o backup. Só remove a tarefa do toolkit,
// depois de conferir que ela é exatamente a esperada. Sem snapshot válido recusa (cancelar a reversão tornaria o desfazer
// impossível). Exige administrador.
func Confirm(ctx context.Context, sys System, st Store) error {
	s, err := ReadStatus(ctx, sys, st)
	if err != nil {
		return err
	}
	switch s.Kind {
	case StatusUnknown:
		return fmt.Errorf("%w: %s; não dá para confirmar", ErrOriginalUnknown, textsafe.Line(s.Reason))
	case StatusArmed:
	default:
		return ErrNothingToConfirm
	}
	if !strings.EqualFold(s.Task.Exe, s.Snapshot.TaskExe) || s.Task.Args != RevertArgs {
		return fmt.Errorf("%w: o programa ou o comando não conferem; nada foi removido", ErrTaskUnexpected)
	}
	if !sys.IsAdmin(ctx) {
		return ErrNeedsAdmin
	}
	if err := sys.DeleteTask(ctx); err != nil {
		return fmt.Errorf("não deu para remover a tarefa de reversão: %w", err)
	}
	if ti, err := sys.ReadTask(ctx); err != nil || ti.Exists {
		return fmt.Errorf("a tarefa de reversão continua existindo depois de removida")
	}
	return nil
}

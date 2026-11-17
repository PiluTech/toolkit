package usb

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"
)

func snapshotOf(t *testing.T, f *fakeSys) RegTree {
	t.Helper()
	tr, err := f.ReadTree(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return tr
}

func TestDesfazerIdaEVoltaExata(t *testing.T) {
	f := newFake(t)
	st := newSpy(f)
	before := snapshotOf(t, f)
	mustApply(t, f, st)
	if len(f.tree.Entries) == 0 {
		t.Fatal("nada foi aplicado")
	}
	res, err := Undo(context.Background(), f, st)
	if err != nil {
		t.Fatal(err)
	}
	if res.Restored == 0 {
		t.Errorf("res = %#v", res)
	}
	after := snapshotOf(t, f)
	if after.KeyExists != before.KeyExists || len(after.Entries) != 0 || len(after.Subkeys) != 0 {
		t.Fatalf("o registro não voltou ao original: %v %v exists=%v", treeKeys(after), after.Subkeys, after.KeyExists)
	}
	if f.task.Exists {
		t.Error("a tarefa de reversão deveria ter sido removida")
	}
	if sn, _ := st.Load(); sn != nil {
		t.Error("o backup deveria ter sido removido")
	}
}

func TestDesfazerPreservaPoliticaDeOutraOrigem(t *testing.T) {
	f := newFake(t)
	st := newSpy(f)
	mustApply(t, f, st)
	// depois da aplicação, GPO/MDM/outra ferramenta acrescenta uma política na MESMA chave
	f.tree.Entries[`DenyDeviceIDs\1`] = String(`USB\VID_AAAA&PID_0001`)
	f.tree.Entries["DenyDeviceIDs"] = DWORD(1)
	f.addSubkey("DenyDeviceIDs")
	if _, err := Undo(context.Background(), f, st); err != nil {
		t.Fatal(err)
	}
	if !f.tree.KeyExists {
		t.Fatal("a chave Restrictions foi apagada com política de outra origem dentro")
	}
	if len(f.tree.Entries) != 2 || f.tree.Entries[`DenyDeviceIDs\1`].Str == "" || f.tree.Entries["DenyDeviceIDs"].DWORD != 1 {
		t.Fatalf("a política de outra origem não foi preservada: %v", treeKeys(f.tree))
	}
	if f.count("key-delete:") > 0 {
		for _, l := range f.log {
			if l == "key-delete:" {
				t.Fatal("KeyDelete(Restrictions) com conteúdo de outra origem")
			}
		}
	}
	if !contains(f.tree.Subkeys, "DenyDeviceIDs") || contains(f.tree.Subkeys, "AllowDeviceIDs") || contains(f.tree.Subkeys, "DenyDeviceClasses") {
		t.Fatalf("subchaves = %v", f.tree.Subkeys)
	}
}

func contains(l []string, s string) bool {
	for _, x := range l {
		if x == s {
			return true
		}
	}
	return false
}

func TestDesfazerNaoApagaAChaveQueJaExistia(t *testing.T) {
	f := newFake(t)
	f.tree = RegTree{KeyExists: true, Entries: map[string]RegValue{}}
	st := newSpy(f)
	mustApply(t, f, st)
	if _, err := Undo(context.Background(), f, st); err != nil {
		t.Fatal(err)
	}
	if !f.tree.KeyExists {
		t.Fatal("a chave Restrictions já existia (vazia) antes: o desfazer não pode apagá-la")
	}
	for _, l := range f.log {
		if l == "key-delete:" {
			t.Fatal("KeyDelete(Restrictions) apesar de a chave já existir antes")
		}
	}
}

func TestDesfazerValorAlteradoPorOutraOrigemParaSemEscrever(t *testing.T) {
	f := newFake(t)
	st := newSpy(f)
	mustApply(t, f, st)
	f.tree.Entries[`AllowDeviceIDs\1`] = String(`USB\VID_BBBB&PID_0001`) // outra origem trocou o valor que o toolkit gravou
	n := f.writes()
	_, err := Undo(context.Background(), f, st)
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("err = %v, queria ErrConflict", err)
	}
	if f.writes() != n {
		t.Fatalf("o desfazer escreveu apesar do conflito: %v", f.log[n:])
	}
	if sn, _ := st.Load(); sn == nil || !f.task.Exists {
		t.Error("backup e tarefa têm de ficar para a análise")
	}
}

func TestDesfazerSemAdminRecusaAntes(t *testing.T) {
	f := newFake(t)
	st := newSpy(f)
	mustApply(t, f, st)
	f.admin = false
	n := len(f.log)
	if _, err := Undo(context.Background(), f, st); !errors.Is(err, ErrNeedsAdmin) {
		t.Fatalf("err = %v", err)
	}
	if len(f.log) != n {
		t.Fatalf("mexeu em algo sem administrador: %v", f.log[n:])
	}
}

// --- snapshot ausente, corrompido ou adulterado depois de uma aplicação ---

func TestSnapshotAusenteDepoisDeAplicarEEstadoOriginalDesconhecido(t *testing.T) {
	for name, setup := range map[string]func(f *fakeSys, st *spyStore){
		"backup apagado (tarefa e chaves ficaram)": func(_ *fakeSys, st *spyStore) { _ = st.Store.Remove() },
		"backup apagado e tarefa já removida": func(f *fakeSys, st *spyStore) {
			_ = st.Store.Remove()
			f.task = TaskInfo{}
		},
		"backup apagado e só a tarefa sobrou": func(f *fakeSys, st *spyStore) {
			_ = st.Store.Remove()
			f.tree = RegTree{Entries: map[string]RegValue{}}
		},
		"backup corrompido": func(_ *fakeSys, st *spyStore) {
			p := st.Store.(FileStore).BackupPath()
			if err := os.WriteFile(p, []byte(`{"sha256":"x","snapshot":{`), 0o600); err != nil {
				t.Fatal(err)
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			f := newFake(t)
			st := newSpy(f)
			mustApply(t, f, st)
			setup(f, st)
			n := len(f.log)
			plan, err := BuildUndo(context.Background(), f, st)
			if err != nil {
				t.Fatalf("BuildUndo: %v", err)
			}
			if plan.Kind != UndoUnknown {
				t.Fatalf("Kind = %v, queria UndoUnknown", plan.Kind)
			}
			txt := plan.Format()
			if !strings.Contains(txt, "ESTADO ORIGINAL DESCONHECIDO") {
				t.Errorf("sem o aviso:\n%s", txt)
			}
			if strings.Contains(txt, "Nada a desfazer") {
				t.Error("disse 'nada a desfazer' havendo sinais de aplicação")
			}
			for _, want := range []string{"outra conta de administrador", "WinRE", "A VERIFICAR", "confirmar que não havia política anterior"} {
				if !strings.Contains(txt, want) {
					t.Errorf("recuperação manual sem %q:\n%s", want, txt)
				}
			}
			_, err = Undo(context.Background(), f, st)
			if !errors.Is(err, ErrOriginalUnknown) {
				t.Fatalf("Undo err = %v, queria ErrOriginalUnknown", err)
			}
			if len(f.log) != n {
				t.Fatalf("restaurou/escreveu às cegas: %v", f.log[n:])
			}
		})
	}
}

func TestNadaADesfazerSoQuandoNuncaHouveAplicacao(t *testing.T) {
	f := newFake(t)
	st := newSpy(f)
	plan, err := BuildUndo(context.Background(), f, st)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Kind != UndoNothing || !strings.Contains(plan.Format(), "Nada a desfazer") {
		t.Fatalf("plan = %v\n%s", plan.Kind, plan.Format())
	}
	res, err := Undo(context.Background(), f, st)
	if err != nil || !res.Nothing {
		t.Fatalf("res=%#v err=%v", res, err)
	}
	if len(f.log) != 0 {
		t.Fatalf("mexeu em algo: %v", f.log)
	}
}

func TestBackupAdulteradoRecusaRestaurar(t *testing.T) {
	f := newFake(t)
	st := newSpy(f)
	mustApply(t, f, st)
	p := st.Store.(FileStore).BackupPath()
	raw, _ := os.ReadFile(p)
	tampered := strings.Replace(string(raw), `"prazo": 10`, `"prazo": 11`, 1)
	if tampered == string(raw) {
		t.Fatal("o teste não conseguiu adulterar o arquivo")
	}
	if err := os.WriteFile(p, []byte(tampered), 0o600); err != nil {
		t.Fatal(err)
	}
	n := len(f.log)
	plan, err := BuildUndo(context.Background(), f, st)
	if err != nil || plan.Kind != UndoUnknown {
		t.Fatalf("plan=%v err=%v", plan, err)
	}
	if len(f.log) != n {
		t.Fatal("mexeu em algo")
	}
	if _, statErr := os.Stat(p); statErr != nil {
		t.Error("o arquivo adulterado tem de ser mantido para análise")
	}
}

func TestDesfazerValidaEstadoPosRestauracao(t *testing.T) {
	// valor que o toolkit gravou foi apagado por fora: não é conflito, só já está restaurado
	f := newFake(t)
	st := newSpy(f)
	mustApply(t, f, st)
	delete(f.tree.Entries, `AllowDeviceIDs\1`)
	if _, err := Undo(context.Background(), f, st); err != nil {
		t.Fatal(err)
	}
	if len(f.tree.Entries) != 0 {
		t.Fatalf("sobrou %v", treeKeys(f.tree))
	}
}

// --- timer: expirar restaura; confirmar cancela a reversão ---

func TestTimerExpirarRestauraOSnapshot(t *testing.T) {
	f := newFake(t)
	st := newSpy(f)
	mustApply(t, f, st)
	// a tarefa dispara: roda o MESMO caminho do desfazer, como SYSTEM
	if _, err := Undo(context.Background(), f, st); err != nil {
		t.Fatal(err)
	}
	if len(f.tree.Entries) != 0 || f.task.Exists {
		t.Fatal("a reversão automática não restaurou")
	}
}

func TestConfirmarCancelaAReversaoEMantemAPolitica(t *testing.T) {
	f := newFake(t)
	st := newSpy(f)
	mustApply(t, f, st)
	if err := Confirm(context.Background(), f, st); err != nil {
		t.Fatal(err)
	}
	if f.task.Exists {
		t.Fatal("a tarefa deveria ter sido removida")
	}
	if len(f.tree.Entries) == 0 {
		t.Fatal("a política tem de continuar aplicada")
	}
	if sn, _ := st.Load(); sn == nil {
		t.Fatal("o backup tem de ficar para o desfazer")
	}
	// só a tarefa do toolkit foi removida; nenhuma escrita no registro
	if f.count("task-delete") != 1 {
		t.Fatalf("log = %v", f.log)
	}
	// o desfazer continua funcionando depois de confirmar
	if _, err := Undo(context.Background(), f, st); err != nil {
		t.Fatal(err)
	}
	if len(f.tree.Entries) != 0 {
		t.Fatal("desfazer depois de confirmar não restaurou")
	}
}

func TestConfirmarRecusas(t *testing.T) {
	f := newFake(t)
	st := newSpy(f)
	if err := Confirm(context.Background(), f, st); !errors.Is(err, ErrNothingToConfirm) {
		t.Fatalf("sem nada aplicado: %v", err)
	}

	mustApply(t, f, st)
	f.admin = false
	if err := Confirm(context.Background(), f, st); !errors.Is(err, ErrNeedsAdmin) {
		t.Fatalf("sem admin: %v", err)
	}
	f.admin = true

	f.task.Args = "usb desfazer --yes"
	if err := Confirm(context.Background(), f, st); !errors.Is(err, ErrTaskUnexpected) {
		t.Fatalf("tarefa com outro comando: %v", err)
	}
	if f.count("task-delete") != 0 {
		t.Fatal("removeu uma tarefa que não é a do toolkit")
	}
	f.task.Args = RevertArgs

	f.task.Exe = `C:\Users\Public\evil.exe`
	if err := Confirm(context.Background(), f, st); !errors.Is(err, ErrTaskUnexpected) {
		t.Fatalf("tarefa com outro binário: %v", err)
	}
}

func TestConfirmarSemSnapshotRecusa(t *testing.T) {
	f := newFake(t)
	st := newSpy(f)
	mustApply(t, f, st)
	_ = st.Store.Remove()
	if err := Confirm(context.Background(), f, st); !errors.Is(err, ErrOriginalUnknown) {
		t.Fatalf("err = %v", err)
	}
	if f.count("task-delete") != 0 {
		t.Fatal("cancelou a reversão sem snapshot: o desfazer ficaria impossível")
	}
}

func TestStatusCobreOsEstados(t *testing.T) {
	f := newFake(t)
	st := newSpy(f)
	s, err := ReadStatus(context.Background(), f, st)
	if err != nil || s.Kind != StatusClean {
		t.Fatalf("limpo: %v %v", s, err)
	}
	mustApply(t, f, st)
	if s, _ = ReadStatus(context.Background(), f, st); s.Kind != StatusArmed {
		t.Fatalf("armado: %v", s.Kind)
	}
	if !s.Task.Next.Equal(t0.Add(10 * time.Minute)) {
		t.Fatalf("próximo disparo = %v", s.Task.Next)
	}
	if err := Confirm(context.Background(), f, st); err != nil {
		t.Fatal(err)
	}
	if s, _ = ReadStatus(context.Background(), f, st); s.Kind != StatusActive {
		t.Fatalf("ativo: %v", s.Kind)
	}
	_ = st.Store.Remove()
	if s, _ = ReadStatus(context.Background(), f, st); s.Kind != StatusUnknown {
		t.Fatalf("sem snapshot: %v", s.Kind)
	}
}

func TestStatusLeituraDaTarefaFalhaEErro(t *testing.T) {
	f := newFake(t)
	f.taskReadErr = errors.New("acesso negado")
	if _, err := ReadStatus(context.Background(), f, newSpy(f)); err == nil {
		t.Fatal("falha de leitura da tarefa tem de ser erro (nunca virar 'limpo')")
	}
	f = newFake(t)
	f.treeReadErr = errors.New("acesso negado")
	if _, err := ReadStatus(context.Background(), f, newSpy(f)); err == nil {
		t.Fatal("falha de leitura do registro tem de ser erro")
	}
}

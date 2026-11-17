package usb

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// Revisão do Crivo no #91 (reversão e pasta protegida).

// 1) A reversão tinha UMA tentativa: se falhasse, a tarefa ficava concluída e o usuário, travado.
func TestArmTaskTentaMaisDeUmaVez(t *testing.T) {
	s := armTaskScript(10)
	for _, want := range []string{"-RestartCount 3", "-RestartInterval (New-TimeSpan -Minutes 1)"} {
		if !strings.Contains(s, want) {
			t.Errorf("o script da tarefa não tem %q: sem nova tentativa, uma falha deixa a política aplicada para sempre", want)
		}
	}
}

// A tarefa já prova o SYSTEM: o reverter não pergunta IsAdmin (outro powershell.exe que pode falhar sem sessão e
// devolveria ErrNeedsAdmin sem escrever nada). Sem direito, o reg.exe falha sozinho: nada é escrito às cegas.
func TestReverterNaoDependeDeIsAdmin(t *testing.T) {
	f := newFake(t)
	st := f.fileStore()
	mustApply(t, f, st)
	f.admin = false
	if _, err := Undo(context.Background(), f, st); !errors.Is(err, ErrNeedsAdmin) {
		t.Fatalf("o desfazer do usuário continua exigindo administrador: %v", err)
	}
	res, err := Revert(context.Background(), f, st)
	if err != nil || res.Restored == 0 {
		t.Fatalf("o reverter (SYSTEM, sem IsAdmin) deveria restaurar: %+v %v", res, err)
	}
}

// A reversão que falha deixa rastro: `toolkit usb` e a TUI mostram "a reversão rodou e falhou: ...".
func TestReverterGravaORastro(t *testing.T) {
	f := newFake(t)
	st := f.fileStore()
	mustApply(t, f, st)
	// GPO/MDM muda um valor gravado: o desfazer recusa (conflito)
	f.tree.Entries["AllowDenyLayered"] = DWORD(0)
	if _, err := Revert(context.Background(), f, st); !errors.Is(err, ErrConflict) {
		t.Fatalf("esperava conflito: %v", err)
	}
	s, err := ReadStatus(context.Background(), f, st)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(s.RevertNote, "falhou") {
		t.Fatalf("a falha da reversão não deixou rastro: %q", s.RevertNote)
	}
	// o próximo Apply (depois de resolvida a sobra) começa sem o rastro velho: a nota some quando há restauração
	f.tree.Entries["AllowDenyLayered"] = DWORD(1)
	if _, err := Revert(context.Background(), f, st); err != nil {
		t.Fatal(err)
	}
	s, _ = ReadStatus(context.Background(), f, st)
	if !strings.Contains(s.RevertNote, "restaurou") {
		t.Fatalf("o sucesso também deixa rastro: %q", s.RevertNote)
	}
}

// 2) A pasta já pode existir (criada por usuário comum): as ACLs dos FILHOS são refeitas, os que são links são
// recusados e um .reg velho é apagado antes do export. O reset NUNCA é na pasta raiz (desfaria a proteção).
func TestProtectDirRefazAsAclsDosFilhos(t *testing.T) {
	s := protectDirScript
	acl := strings.Index(s, "Set-Acl -LiteralPath $d")
	reset := strings.Index(s, "/reset")
	owner := strings.Index(s, "/setowner")
	if acl < 0 || reset < 0 || owner < 0 || reset < acl || owner < acl {
		t.Fatalf("o reset e o dono dos filhos vêm DEPOIS do Set-Acl da pasta: acl=%d reset=%d setowner=%d", acl, reset, owner)
	}
	if !strings.Contains(s, "Get-ChildItem -LiteralPath $d -Force") {
		t.Error("os filhos têm de ser listados com -Force (ocultos também)")
	}
	if strings.Contains(s, "icacls.exe $d") || strings.Contains(s, "icacls.exe '"+ProtectedDir) {
		t.Error("o icacls NÃO pode resetar a própria pasta raiz (desfaria a proteção)")
	}
	if !strings.Contains(s, "ReparsePoint") || strings.Count(s, "reparse point") < 2 {
		t.Error("filho que é link também é recusado")
	}
	if !strings.Contains(s, "[IO.File]::Delete") || !strings.Contains(s, RegFile) {
		t.Error("um .reg que já estava na pasta é apagado antes do export")
	}
	if err := asciiOnly(s); err != nil {
		t.Errorf("script não ASCII: %v", err)
	}
}

// 3) A árvore é relida logo antes da primeira escrita: o que mudou na janela da preparação aborta (nada escrito).
func TestApplyRelêAArvoreAntesDaPrimeiraEscrita(t *testing.T) {
	f := newFake(t)
	st := f.fileStore()
	f.hook = func(op string) {
		if strings.HasPrefix(op, "task-arm") { // GPO/MDM grava na janela entre o snapshot e a primeira escrita
			f.tree.KeyExists = true
			f.tree.Entries["DenyDeviceIDs"] = DWORD(1)
		}
	}
	_, err := Apply(context.Background(), f, st, applyOpts(f))
	if !errors.Is(err, ErrExistingPolicy) {
		t.Fatalf("a árvore mudou durante a preparação: esperava ErrExistingPolicy, veio %v", err)
	}
	if f.writes() != 0 {
		t.Fatalf("nada podia ser escrito: %d escritas", f.writes())
	}
	if f.task.Exists {
		t.Error("a tarefa deste Apply deveria ter sido removida")
	}
	if sn, _ := st.Load(); sn != nil {
		t.Error("o backup deste Apply deveria ter sido removido")
	}
}

// 4) O Confirm remove a tarefa e confere de novo que a política continua lá: se a reversão já estava rodando, não diz
// "confirmado".
func TestConfirmConfereAPoliticaDepoisDeRemoverATarefa(t *testing.T) {
	f := newFake(t)
	st := f.fileStore()
	mustApply(t, f, st)
	f.hook = func(op string) {
		if op == "task-delete" { // a tarefa já estava executando e termina de restaurar enquanto o Confirm a remove
			f.tree = RegTree{Entries: map[string]RegValue{}}
		}
	}
	if err := Confirm(context.Background(), f, st); err == nil {
		t.Fatal("a política sumiu durante o Confirm: não pode dizer confirmado")
	}
}

// 5) Aviso quando o toolkit roda de uma pasta que usuário comum grava.
func TestAvisoDoExecutavelForaDePastaDeAdministrador(t *testing.T) {
	for _, ok := range []string{`C:\Program Files\toolkit\toolkit.exe`, `c:\program files (x86)\x\t.exe`, `C:\Windows\System32\t.exe`} {
		if w := ExeFolderWarning(ok); w != "" {
			t.Errorf("%s não deveria avisar: %q", ok, w)
		}
	}
	for _, bad := range []string{`C:\Users\ana\Downloads\toolkit.exe`, `D:\tools\toolkit.exe`, `C:\ProgramData\x\toolkit.exe`, `toolkit.exe`} {
		if w := ExeFolderWarning(bad); w == "" {
			t.Errorf("%s deveria avisar", bad)
		}
	}
}

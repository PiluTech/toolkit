package privacidade

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/PiluTech/toolkit/internal/diagfmt"
	"github.com/PiluTech/toolkit/internal/exec"
)

const (
	sidA = "S-1-5-21-1111111111-2222222222-3333333333-1001"
	sidB = "S-1-5-21-1111111111-2222222222-3333333333-1002"
)

// whoamiRunner responde só ao whoami (por caminho absoluto, somente leitura).
type whoamiRunner struct {
	t      *testing.T
	stdout string
	code   int
	calls  []exec.Cmd
}

func (w *whoamiRunner) Run(_ context.Context, c exec.Cmd) (exec.Result, error) {
	w.calls = append(w.calls, c)
	if c.Name != `C:\Windows\System32\whoami.exe` || !c.ReadOnly || c.Elevate {
		w.t.Errorf("comando inesperado: %+v", c)
	}
	return exec.Result{ExitCode: w.code, Stdout: []byte(w.stdout)}, nil
}
func (w *whoamiRunner) LookPath(string) (string, error) { return "", errors.New("sem PATH") }

func TestUserSIDLeDoWhoami(t *testing.T) {
	r := &whoamiRunner{t: t, stdout: "\"PC\\ana\",\"" + sidA + "\"\r\n"}
	sid, err := UserSID(context.Background(), NewWindowsSystem(r, nil))
	if err != nil || sid != sidA {
		t.Fatalf("sid=%q err=%v", sid, err)
	}
	if got := strings.Join(r.calls[0].Args, " "); got != "/user /fo csv /nh" {
		t.Errorf("args = %q", got)
	}
}

func TestUserSIDSaidaRuimFalha(t *testing.T) {
	for _, out := range []string{"", "lixo", `"PC\ana","nao-e-sid"`, `"PC\ana","S-1-5-21-1","extra"`, `"PC\ana","S-1-5-21-1-../x"`} {
		r := &whoamiRunner{t: t, stdout: out}
		if sid, err := UserSID(context.Background(), NewWindowsSystem(r, nil)); err == nil {
			t.Errorf("%q aceito: %q", out, sid)
		}
	}
	r := &whoamiRunner{t: t, stdout: `"PC\ana","` + sidA + `"`, code: 1}
	if _, err := UserSID(context.Background(), NewWindowsSystem(r, nil)); err == nil {
		t.Error("exit 1 aceito")
	}
}

func TestUserSIDSemSuporteFalhaFechado(t *testing.T) {
	type semSID struct{ System }
	if _, err := UserSID(context.Background(), semSID{newFake(EditionPro, true)}); err == nil {
		t.Error("System sem SID deve falhar")
	}
}

func scoped(t *testing.T, dir, sid string) ScopedStore {
	t.Helper()
	s, err := NewScopedStore(dir, sid)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func fakeFor(sid string, adv uint32) *fakeSys {
	f := newFake(EditionPro, true).withAllServicesAndTasks()
	f.sid = sid
	f.regs[rkey(kAdv, nAdv)] = DWORD(adv)
	return f
}

func TestScopedStoreSidInvalidoRecusado(t *testing.T) {
	for _, sid := range []string{"", "x", `S-1-5\..\x`, "S-1-5-21-1/2", "../S-1-5-21-1"} {
		if _, err := NewScopedStore(t.TempDir(), sid); err == nil {
			t.Errorf("SID %q aceito", sid)
		}
	}
}

func TestDoisUsuariosAplicamEDesfazemIndependentes(t *testing.T) {
	dir := t.TempDir()
	fA, fB := fakeFor(sidA, 1), fakeFor(sidB, 7)
	stA, stB := scoped(t, dir, sidA), scoped(t, dir, sidB)
	for _, x := range []struct {
		f  *fakeSys
		st Store
	}{{fA, stA}, {fB, stB}} {
		if _, _, err := buildApply(t, x.f, x.st, PerfilBasico); err != nil {
			t.Fatal(err)
		}
	}
	// cada um tem o SEU original
	bA, _ := stA.Load()
	bB, _ := stB.Load()
	id := regID(kAdv, nAdv)
	if bA.Entries[id].DWORD != 1 || bB.Entries[id].DWORD != 7 {
		t.Fatalf("originais misturados: A=%+v B=%+v", bA.Entries[id], bB.Entries[id])
	}
	if stA.Path() == stB.Path() {
		t.Error("mesmo arquivo para dois usuários")
	}
	// o desfazer do A restaura o A e não mexe no backup do B
	plan, bk, err := BuildUndo(context.Background(), fA, stA, diagfmt.PT)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ApplyUndo(context.Background(), fA, stA, plan, bk); err != nil {
		t.Fatal(err)
	}
	if v, _ := fA.regValue(kAdv, nAdv); v != DWORD(1) {
		t.Errorf("A não voltou: %+v", v)
	}
	if b, _ := stA.Load(); b != nil {
		t.Error("backup do A deveria ter sumido")
	}
	if b, _ := stB.Load(); b == nil || b.Entries[id].DWORD != 7 {
		t.Error("o desfazer do A mexeu no backup do B")
	}
	if v, _ := fB.regValue(kAdv, nAdv); v != DWORD(0) {
		t.Error("o desfazer do A mexeu no registro do B")
	}
}

func TestEscopoMaquinaSeparadoDoUsuario(t *testing.T) {
	dir := t.TempDir()
	fA := fakeFor(sidA, 1)
	stA, stB := scoped(t, dir, sidA), scoped(t, dir, sidB)
	if _, _, err := buildApply(t, fA, stA, PerfilRecomendado); err != nil {
		t.Fatal(err)
	}
	// o B enxerga o que é da MÁQUINA (HKLM, serviço, tarefa) e nada do que é do usuário A (HKCU)
	bB, err := stB.Load()
	if err != nil || bB == nil {
		t.Fatalf("B não viu o escopo máquina: %v %v", bB, err)
	}
	for id, e := range bB.Entries {
		if strings.HasPrefix(e.Key, "HKCU") {
			t.Errorf("B enxerga HKCU do A: %s", id)
		}
	}
	if _, ok := bB.Entries["svc:DiagTrack"]; !ok {
		t.Error("B deveria ver o escopo máquina")
	}
	// e o do A tem os dois escopos
	bA, _ := stA.Load()
	if _, ok := bA.Entries[regID(kAdv, nAdv)]; !ok {
		t.Error("A perdeu o HKCU")
	}
	if len(bA.Entries) <= len(bB.Entries) {
		t.Error("A deveria ter HKCU a mais que B")
	}
}

func TestBackupDeOutroDonoRecusado(t *testing.T) {
	dir := t.TempDir()
	stA, stB := scoped(t, dir, sidA), scoped(t, dir, sidB)
	if _, _, err := buildApply(t, fakeFor(sidA, 1), stA, PerfilBasico); err != nil {
		t.Fatal(err)
	}
	// copia o arquivo do A para o nome do B: o dono gravado dentro do arquivo não confere
	data, err := os.ReadFile(stA.Path())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stB.Path(), data, 0o600); err != nil {
		t.Fatal(err)
	}
	fB := fakeFor(sidB, 7)
	_, _, err = BuildUndo(context.Background(), fB, stB, diagfmt.PT)
	if !errors.Is(err, ErrBackupOutroDono) {
		t.Fatalf("err = %v", err)
	}
	if len(fB.writes()) != 0 {
		t.Errorf("mutações: %v", fB.writes())
	}
	after, _ := os.ReadFile(stB.Path())
	if !bytes.Equal(after, data) {
		t.Error("arquivo do outro dono foi alterado")
	}
	// Apply também recusa (não sobrescreve nem mistura)
	plan, _ := Build(context.Background(), fB, PerfilBasico, diagfmt.PT)
	if _, err := Apply(context.Background(), fB, stB, plan, t0); !errors.Is(err, ErrBackupOutroDono) {
		t.Errorf("Apply: %v", err)
	}
	if len(fB.writes()) != 0 {
		t.Errorf("Apply mutou: %v", fB.writes())
	}
}

func TestScopedStoreSaveRemoveArquivoVazio(t *testing.T) {
	dir := t.TempDir()
	st := scoped(t, dir, sidA)
	if _, _, err := buildApply(t, fakeFor(sidA, 1), st, PerfilRecomendado); err != nil {
		t.Fatal(err)
	}
	b, _ := st.Load()
	for id, e := range b.Entries { // deixa só o escopo usuário
		if !strings.HasPrefix(e.Key, "HKCU") {
			delete(b.Entries, id)
		}
	}
	if err := st.Save(b); err != nil {
		t.Fatal(err)
	}
	files, _ := filepath.Glob(filepath.Join(dir, "*.json"))
	if len(files) != 1 {
		t.Errorf("arquivos: %v", files)
	}
	if err := st.Remove(); err != nil {
		t.Fatal(err)
	}
	if files, _ := filepath.Glob(filepath.Join(dir, "*.json")); len(files) != 0 {
		t.Errorf("sobrou: %v", files)
	}
}

package animacoes

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/PiluTech/toolkit/internal/diagfmt"
	"github.com/PiluTech/toolkit/internal/privacidade"
)

func entryMin(mod func(*privacidade.Entry)) *privacidade.Backup {
	e := privacidade.Entry{ID: regID(keyMin, "MinAnimate"), Kind: "reg", Key: keyMin, Name: "MinAnimate", Existed: true, RegKind: privacidade.RegString, Str: "1"}
	if mod != nil {
		mod(&e)
	}
	return &privacidade.Backup{Version: 1, Created: t0, Entries: map[string]privacidade.Entry{e.ID: e}}
}

// Backup adulterado: aponta para HKLM, outra chave, outro valor, outro tipo de entrada. Zero mutações e zero leituras
// de alvo estranho (a validação roda ANTES de tudo).
func TestBackupAdulteradoERecusadoSemMudarNada(t *testing.T) {
	cases := map[string]func(*privacidade.Entry){
		"hklm":        func(e *privacidade.Entry) { e.Key = `HKLM\SOFTWARE\Policies\Microsoft\Windows\System` },
		"outra chave": func(e *privacidade.Entry) { e.Key = `HKCU\Software\Outra` },
		"outro nome":  func(e *privacidade.Entry) { e.Name = "UserPreferencesMask" },
		"kind":        func(e *privacidade.Entry) { e.Kind = "service" },
		"tipo ruim":   func(e *privacidade.Entry) { e.RegKind = privacidade.RegKind(42) },
	}
	for name, mod := range cases {
		f := newFake()
		bk := entryMin(mod)
		for _, e := range bk.Entries { // o ID do mapa acompanha o alvo adulterado: só o catálogo pode recusar
			if name != "tipo ruim" {
				e.ID = regID(e.Key, e.Name)
			}
			bk.Entries = map[string]privacidade.Entry{e.ID: e}
		}
		st := &memStore{b: bk}
		if _, _, err := BuildUndo(context.Background(), f, st, diagfmt.PT); !errors.Is(err, ErrBackupInvalido) {
			t.Errorf("%s: err=%v", name, err)
		}
		if len(f.log) != 0 {
			t.Errorf("%s: mutou: %v", name, f.log)
		}
	}
	// ID da entrada diferente da chave do mapa
	bk := entryMin(nil)
	e := bk.Entries[regID(keyMin, "MinAnimate")]
	e.ID = "reg:qualquer"
	bk.Entries = map[string]privacidade.Entry{regID(keyMin, "MinAnimate"): e}
	if _, _, err := BuildUndo(context.Background(), newFake(), &memStore{b: bk}, diagfmt.PT); !errors.Is(err, ErrBackupInvalido) {
		t.Errorf("id difere: err=%v", err)
	}
}

// Aplicar com um backup existente adulterado também recusa (o backup é dado não confiável) e não escreve.
func TestApplyComBackupExistenteAdulteradoRecusa(t *testing.T) {
	f := newFake()
	bk := entryMin(func(e *privacidade.Entry) { e.Key = `HKLM\SOFTWARE\X`; e.ID = regID(e.Key, e.Name) })
	st := &memStore{b: bk}
	_, err := Apply(context.Background(), f, st, plan(t, f), t0)
	if !errors.Is(err, ErrBackupInvalido) || len(f.log) != 0 || st.saves != 0 {
		t.Fatalf("err=%v log=%v saves=%d", err, f.log, st.saves)
	}
}

func TestBackupDeOutroSIDERecusado(t *testing.T) {
	dir := t.TempDir()
	a, err := NewStore(dir, sidA)
	if err != nil {
		t.Fatal(err)
	}
	f := newFake()
	f.regs[rkey(keyMin, "MinAnimate")] = str("1")
	if _, err := Apply(context.Background(), f, a, plan(t, f), t0); err != nil {
		t.Fatal(err)
	}
	// o mesmo arquivo aberto como se fosse de outro usuário (arquivo copiado/renomeado)
	b, _ := NewStore(dir, sidB)
	if err := os.Rename(a.Path(), b.Path()); err != nil {
		t.Fatal(err)
	}
	if _, _, err := BuildUndo(context.Background(), f, b, diagfmt.PT); !errors.Is(err, ErrBackupOutroDono) {
		t.Fatalf("err=%v", err)
	}
	f2 := newFake()
	if _, err := Apply(context.Background(), f2, b, plan(t, f2), t0); !errors.Is(err, ErrBackupOutroDono) || len(f2.log) != 0 {
		t.Fatalf("err=%v log=%v", err, f2.log)
	}
}

func TestStoreArquivoPorSIDComPermissao0600(t *testing.T) {
	dir := t.TempDir()
	s, err := NewStore(dir, sidA)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(s.Path()) != "otimizacao-animacoes-backup-"+sidA+".json" {
		t.Fatalf("nome: %s", s.Path())
	}
	for _, bad := range []string{"", "..", `S-1-5-21-1\..\x`, "S-1-5", "a/b"} {
		if _, err := NewStore(dir, bad); err == nil {
			t.Errorf("SID %q aceito", bad)
		}
	}
}

func TestUserSIDFalhaFechado(t *testing.T) {
	if _, err := UserSID(context.Background(), semSID{newFake()}); err == nil {
		t.Fatal("System sem SIDer tem de falhar")
	}
	f := newFake()
	f.sid = "lixo"
	if _, err := UserSID(context.Background(), f); err == nil {
		t.Fatal("SID inválido aceito")
	}
	f.sid = sidA
	if sid, err := UserSID(context.Background(), f); err != nil || sid != sidA {
		t.Fatalf("sid=%q err=%v", sid, err)
	}
}

// semSID esconde o método UserSID do fake.
type semSID struct{ f *fakeSys }

func (s semSID) RegRead(ctx context.Context, k, n string) (RegValue, bool, error) {
	return s.f.RegRead(ctx, k, n)
}
func (s semSID) RegWrite(ctx context.Context, k, n string, v RegValue) error {
	return s.f.RegWrite(ctx, k, n, v)
}
func (s semSID) RegDelete(ctx context.Context, k, n string) error { return s.f.RegDelete(ctx, k, n) }

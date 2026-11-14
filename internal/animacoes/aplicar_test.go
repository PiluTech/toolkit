package animacoes

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/PiluTech/toolkit/internal/diagfmt"
	"github.com/PiluTech/toolkit/internal/privacidade"
)

var t0 = time.Date(2026, 11, 14, 10, 0, 0, 0, time.UTC)

func plan(t *testing.T, f *fakeSys) *Plan {
	t.Helper()
	p, err := Build(context.Background(), f, diagfmt.PT)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestApplyGravaBackupAntesDaPrimeiraEscrita(t *testing.T) {
	f := newFake()
	f.regs[rkey(keyMin, "MinAnimate")] = str("1")
	st := &memStore{}
	f.onWrite = func() {
		if st.saves == 0 || st.b == nil {
			t.Fatal("escreveu no registro antes de gravar o backup")
		}
		if e, ok := st.b.Entries[regID(keyMin, "MinAnimate")]; !ok || e.Str != "1" || !e.Existed {
			t.Fatalf("o backup não tem o original: %+v", st.b)
		}
	}
	res, err := Apply(context.Background(), f, st, plan(t, f), t0)
	if err != nil || len(res.Applied) != 1 {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	if v, _ := f.get(keyMin, "MinAnimate"); v != str("0") {
		t.Fatalf("valor final: %+v", v)
	}
	if !reflect.DeepEqual(f.log, []string{"reg-write:" + keyMin + `\MinAnimate`}) {
		t.Fatalf("mutações: %v", f.log)
	}
}

func TestApplyFalhaNoBackupNaoMudaNada(t *testing.T) {
	f := newFake()
	st := &memStore{failSave: true}
	_, err := Apply(context.Background(), f, st, plan(t, f), t0)
	if err == nil || !strings.Contains(err.Error(), "nada foi alterado") {
		t.Fatalf("err=%v", err)
	}
	if len(f.log) != 0 {
		t.Fatalf("mutou sem backup: %v", f.log)
	}
}

func TestApplyErroNoMeioParaEBackupCobreOQueMudou(t *testing.T) {
	// o catálogo tem 1 item hoje; o teste monta um plano de 2 para provar a regra (parar no erro, backup de tudo)
	f := newFake()
	f.regs[rkey(keyMin, "MinAnimate")] = str("1")
	p := plan(t, f)
	extra := Change{ID: regID(keyMin, "Outro"), Target: "x", Pending: true,
		orig:  privacidade.Entry{ID: regID(keyMin, "Outro"), Kind: "reg", Key: keyMin, Name: "Outro"},
		apply: func(ctx context.Context, s System) error { return s.RegWrite(ctx, keyMin, "Outro", str("0")) }}
	p.Changes = append(p.Changes, extra)
	f.failOn = "reg-write:" + keyMin + `\Outro`
	st := &memStore{}
	res, err := Apply(context.Background(), f, st, p, t0)
	if err == nil {
		t.Fatal("deveria falhar")
	}
	if len(res.Applied) != 1 {
		t.Fatalf("aplicados antes do erro: %d", len(res.Applied))
	}
	if _, ok := st.b.Entries[regID(keyMin, "MinAnimate")]; !ok {
		t.Fatal("o backup cobre o que mudou")
	}
}

func TestReaplicarNaoSobrescreveOOriginal(t *testing.T) {
	f := newFake()
	f.regs[rkey(keyMin, "MinAnimate")] = str("1")
	st := &memStore{}
	if _, err := Apply(context.Background(), f, st, plan(t, f), t0); err != nil {
		t.Fatal(err)
	}
	// alguém volta o valor manualmente para outra coisa e o plano roda de novo
	f.regs[rkey(keyMin, "MinAnimate")] = str("2")
	if _, err := Apply(context.Background(), f, st, plan(t, f), t0.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if e := st.b.Entries[regID(keyMin, "MinAnimate")]; e.Str != "1" {
		t.Fatalf("o original foi sobrescrito: %+v", e)
	}
	if !st.b.Created.Equal(t0) {
		t.Fatalf("data do backup mudou: %v", st.b.Created)
	}
}

func TestApplyNadaPendenteNaoGravaBackup(t *testing.T) {
	f := newFake()
	f.regs[rkey(keyMin, "MinAnimate")] = str("0")
	st := &memStore{}
	res, err := Apply(context.Background(), f, st, plan(t, f), t0)
	if err != nil || res.AlreadyOK != 1 || st.saves != 0 || len(f.log) != 0 {
		t.Fatalf("res=%+v err=%v saves=%d log=%v", res, err, st.saves, f.log)
	}
}

func TestDesfazerRestauraTipoEConteudo(t *testing.T) {
	for name, orig := range map[string]RegValue{"texto": str("1"), "dword": DWORD(1), "expand": {Kind: privacidade.RegExpandString, Str: "%x%"}} {
		f := newFake()
		f.regs[rkey(keyMin, "MinAnimate")] = orig
		st := &memStore{}
		if _, err := Apply(context.Background(), f, st, plan(t, f), t0); err != nil {
			t.Fatal(err)
		}
		up, bk, err := BuildUndo(context.Background(), f, st, diagfmt.PT)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if _, err := ApplyUndo(context.Background(), f, st, up, bk); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if v, ok := f.get(keyMin, "MinAnimate"); !ok || v != orig {
			t.Errorf("%s: restaurou %+v (existe=%v), queria %+v", name, v, ok, orig)
		}
		if st.b != nil {
			t.Errorf("%s: o backup deveria ser removido", name)
		}
	}
}

func TestDesfazerApagaSoOQueNaoExistia(t *testing.T) {
	f := newFake()
	st := &memStore{}
	if _, err := Apply(context.Background(), f, st, plan(t, f), t0); err != nil {
		t.Fatal(err)
	}
	up, bk, err := BuildUndo(context.Background(), f, st, diagfmt.PT)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ApplyUndo(context.Background(), f, st, up, bk); err != nil {
		t.Fatal(err)
	}
	if _, ok := f.get(keyMin, "MinAnimate"); ok {
		t.Fatal("o valor não existia antes: tem de ser apagado")
	}
	if !strings.Contains(f.log[len(f.log)-1], "reg-delete:"+keyMin) {
		t.Fatalf("log: %v", f.log)
	}
}

func TestDesfazerSemBackup(t *testing.T) {
	_, _, err := BuildUndo(context.Background(), newFake(), &memStore{}, diagfmt.PT)
	if !errors.Is(err, ErrNoBackup) {
		t.Fatalf("err=%v", err)
	}
}

func TestDesfazerErroNoMeioMantemOQueSobrou(t *testing.T) {
	f := newFake()
	st := &memStore{b: &privacidade.Backup{Version: 1, Created: t0, Entries: map[string]privacidade.Entry{
		regID(keyMin, "MinAnimate"): {ID: regID(keyMin, "MinAnimate"), Kind: "reg", Key: keyMin, Name: "MinAnimate", Existed: true, RegKind: privacidade.RegString, Str: "1"}}}}
	f.regs[rkey(keyMin, "MinAnimate")] = str("0")
	up, bk, err := BuildUndo(context.Background(), f, st, diagfmt.PT)
	if err != nil {
		t.Fatal(err)
	}
	f.failOn = "reg-write"
	if _, err := ApplyUndo(context.Background(), f, st, up, bk); err == nil {
		t.Fatal("deveria falhar")
	}
	if st.b == nil || len(st.b.Entries) != 1 {
		t.Fatal("o backup do que não foi restaurado tem de ficar")
	}
}

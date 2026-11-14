package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/PiluTech/toolkit/internal/animacoes"
	"github.com/PiluTech/toolkit/internal/exec"
	"github.com/PiluTech/toolkit/internal/exec/exectest"
	"github.com/PiluTech/toolkit/internal/privacidade"
)

const animKey = `HKCU\Control Panel\Desktop\WindowMetrics`

// animSys é o registro do usuário em memória (nenhum teste toca o Windows real).
type animSys struct {
	regs   map[string]animacoes.RegValue
	writes int
	log    []string
	// afterRead roda uma vez, depois da primeira leitura (simula outro processo mexendo no meio).
	afterRead func()
}

func newAnimSys() *animSys { return &animSys{regs: map[string]animacoes.RegValue{}} }

func (s *animSys) RegRead(_ context.Context, k, n string) (animacoes.RegValue, bool, error) {
	v, ok := s.regs[strings.ToUpper(k)+"|"+n]
	if s.afterRead != nil {
		f := s.afterRead
		s.afterRead = nil
		defer f()
	}
	return v, ok, nil
}
func (s *animSys) RegWrite(_ context.Context, k, n string, v animacoes.RegValue) error {
	s.writes++
	s.log = append(s.log, "write:"+k+`\`+n)
	s.regs[strings.ToUpper(k)+"|"+n] = v
	return nil
}
func (s *animSys) RegDelete(_ context.Context, k, n string) error {
	s.writes++
	s.log = append(s.log, "delete:"+k+`\`+n)
	delete(s.regs, strings.ToUpper(k)+"|"+n)
	return nil
}
func (s *animSys) set(v animacoes.RegValue) { s.regs[strings.ToUpper(animKey)+"|MinAnimate"] = v }
func (s *animSys) get() (animacoes.RegValue, bool) {
	v, ok := s.regs[strings.ToUpper(animKey)+"|MinAnimate"]
	return v, ok
}

func animStr(s string) animacoes.RegValue {
	return animacoes.RegValue{Kind: privacidade.RegString, Str: s}
}

func animDeps(t *testing.T, sys *animSys, st *privStore, goos string, tty bool, input string) Deps {
	t.Helper()
	return Deps{GOOS: goos, EmbeddedYML: []byte(embedded), NewRunner: func() exec.Runner { return exectest.New(t) },
		UpdateCheck: UpdateCheckDeps{Env: func(string) string { return "" }},
		Stdin:       strings.NewReader(input), Interactive: func() bool { return tty },
		Otim: OtimDeps{AnimSystem: sys, AnimStore: st, Now: func() time.Time { return time.Date(2026, 11, 14, 10, 0, 0, 0, time.UTC) }}}
}

func runAnim(t *testing.T, deps Deps, args ...string) (string, error) {
	t.Helper()
	var o, e bytes.Buffer
	cmd := NewRootCmdWith(&o, &e, deps)
	cmd.SetArgs(append([]string{"otimizacao", "--no-update-check"}, args...))
	err := cmd.ExecuteContext(context.Background())
	return o.String(), err
}

func TestAnimacoesForaDoWindowsNaoTocaEmNada(t *testing.T) {
	for _, args := range [][]string{{"animacoes", "--yes"}, {"animacoes", "--dry-run"}, {"animacoes", "desfazer", "--yes"}} {
		sys, st := newAnimSys(), &privStore{}
		_, err := runAnim(t, animDeps(t, sys, st, "linux", true, ""), args...)
		if err == nil || !strings.Contains(err.Error(), "só existe no Windows") {
			t.Errorf("%v: erro = %v", args, err)
		}
		if sys.writes != 0 || st.b != nil {
			t.Errorf("%v: nada pode mudar", args)
		}
	}
}

func TestAnimacoesDryRunMostraPlanoENaoMuda(t *testing.T) {
	sys, st := newAnimSys(), &privStore{}
	sys.set(animStr("1"))
	out, err := runAnim(t, animDeps(t, sys, st, "windows", false, ""), "animacoes", "--dry-run")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"1" -> "0"`, "EXPERIMENTAL", "desfazer:", "--dry-run: nada foi alterado", "reabrir"} {
		if !strings.Contains(out, want) {
			t.Errorf("faltou %q:\n%s", want, out)
		}
	}
	if sys.writes != 0 || st.b != nil {
		t.Errorf("--dry-run mudou algo: writes=%d", sys.writes)
	}
}

func TestAnimacoesSemYesESemTerminalErraAntesDeMudar(t *testing.T) {
	sys, st := newAnimSys(), &privStore{}
	out, err := runAnim(t, animDeps(t, sys, st, "windows", false, ""), "animacoes")
	if err == nil || !strings.Contains(err.Error(), "--yes") {
		t.Fatalf("erro = %v", err)
	}
	if !strings.Contains(out, "->") {
		t.Errorf("o plano aparece antes do erro:\n%s", out)
	}
	if sys.writes != 0 || st.b != nil {
		t.Fatal("nada pode mudar")
	}
}

func TestAnimacoesYesAplicaEDesfazRestauraExato(t *testing.T) {
	for name, orig := range map[string]*animacoes.RegValue{"texto": ptr(animStr("1")), "dword": ptr(animacoes.DWORD(1)), "ausente": nil} {
		sys, st := newAnimSys(), &privStore{}
		if orig != nil {
			sys.set(*orig)
		}
		d := animDeps(t, sys, st, "windows", false, "")
		out, err := runAnim(t, d, "animacoes", "--yes")
		if err != nil || !strings.Contains(out, "Aplicado: 1") {
			t.Fatalf("%s: err=%v\n%s", name, err, out)
		}
		if v, _ := sys.get(); v != animStr("0") {
			t.Fatalf("%s: valor %+v", name, v)
		}
		if st.b == nil {
			t.Fatalf("%s: sem backup", name)
		}
		out, err = runAnim(t, animDeps(t, sys, st, "windows", false, ""), "animacoes", "desfazer", "--yes")
		if err != nil || !strings.Contains(out, "Restaurado: 1") {
			t.Fatalf("%s: err=%v\n%s", name, err, out)
		}
		v, ok := sys.get()
		if (orig == nil && ok) || (orig != nil && (!ok || v != *orig)) {
			t.Errorf("%s: restaurou %+v existe=%v", name, v, ok)
		}
		if st.b != nil {
			t.Errorf("%s: backup deveria sumir", name)
		}
	}
}

func ptr[T any](v T) *T { return &v }

func TestAnimacoesPerguntaEPodeCancelar(t *testing.T) {
	sys, st := newAnimSys(), &privStore{}
	out, err := runAnim(t, animDeps(t, sys, st, "windows", true, "n\n"), "animacoes")
	if err != nil || !strings.Contains(out, "Cancelado") || sys.writes != 0 || st.b != nil {
		t.Fatalf("err=%v writes=%d\n%s", err, sys.writes, out)
	}
	sys2, st2 := newAnimSys(), &privStore{}
	if _, err := runAnim(t, animDeps(t, sys2, st2, "windows", true, "s\n"), "animacoes"); err != nil || sys2.writes != 1 {
		t.Fatalf("err=%v writes=%d", err, sys2.writes)
	}
}

func TestAnimacoesTudoAplicadoNaoPergunta(t *testing.T) {
	sys, st := newAnimSys(), &privStore{}
	sys.set(animStr("0"))
	out, err := runAnim(t, animDeps(t, sys, st, "windows", false, ""), "animacoes")
	if err != nil || !strings.Contains(out, "Nada a fazer") || sys.writes != 0 {
		t.Fatalf("err=%v\n%s", err, out)
	}
}

func TestAnimacoesDesfazerSemBackupEDryRun(t *testing.T) {
	sys, st := newAnimSys(), &privStore{}
	if _, err := runAnim(t, animDeps(t, sys, st, "windows", false, ""), "animacoes", "desfazer", "--yes"); err == nil {
		t.Fatal("sem backup é erro")
	}
	sys.set(animStr("1"))
	if _, err := runAnim(t, animDeps(t, sys, st, "windows", false, ""), "animacoes", "--yes"); err != nil {
		t.Fatal(err)
	}
	before := sys.writes
	out, err := runAnim(t, animDeps(t, sys, st, "windows", false, ""), "animacoes", "desfazer", "--dry-run")
	if err != nil || sys.writes != before || !strings.Contains(out, "nada foi alterado") {
		t.Fatalf("err=%v\n%s", err, out)
	}
}

func TestAnimacoesBackupAdulteradoNaoMudaNada(t *testing.T) {
	sys := newAnimSys()
	st := &privStore{b: &privacidade.Backup{Version: 1, Entries: map[string]privacidade.Entry{
		`reg:HKLM\SOFTWARE\X|N`: {ID: `reg:HKLM\SOFTWARE\X|N`, Kind: "reg", Key: `HKLM\SOFTWARE\X`, Name: "N", Existed: true, RegKind: privacidade.RegDWORD}}}}
	_, err := runAnim(t, animDeps(t, sys, st, "windows", false, ""), "animacoes", "desfazer", "--yes")
	if err == nil || !strings.Contains(err.Error(), "fora do catálogo") || sys.writes != 0 {
		t.Fatalf("err=%v writes=%d", err, sys.writes)
	}
}

func TestOtimizacaoSemSubcomandoContinuaSoLeitura(t *testing.T) {
	// o comando sem subcomando segue lendo os gargalos; "animacoes" é um subcomando novo
	cmd := NewRootCmdWith(&bytes.Buffer{}, &bytes.Buffer{}, Deps{GOOS: "windows"})
	oc, _, err := cmd.Find([]string{"otimizacao", "animacoes"})
	if err != nil || oc.Name() != "animacoes" {
		t.Fatalf("subcomando: %v %v", oc, err)
	}
}

// hookReader roda fn na primeira leitura (o instante da pergunta [s/N]) e responde "s".
type hookReader struct {
	fn   func()
	done bool
}

func (h *hookReader) Read(p []byte) (int, error) {
	if h.done {
		return 0, io.EOF
	}
	h.done = true
	h.fn()
	return copy(p, "s\n"), nil
}

const msgStale = "o estado mudou desde o plano mostrado"

// Lupa #82: outro processo grava "1" durante o [s/N]; aplicar gravaria 0 com Existed=false e o desfazer apagaria o 1.
func TestAnimacoesEstadoMudaDuranteAPerguntaNaoAplicaNada(t *testing.T) {
	sys, st := newAnimSys(), &privStore{}
	d := animDeps(t, sys, st, "windows", true, "")
	d.Stdin = &hookReader{fn: func() { sys.set(animStr("1")) }}
	out, err := runAnim(t, d, "animacoes")
	if err == nil || !strings.Contains(err.Error(), msgStale) {
		t.Fatalf("erro = %v\n%s", err, out)
	}
	if sys.writes != 0 || st.b != nil {
		t.Fatalf("zero mutações: writes=%d backup=%v", sys.writes, st.b)
	}
	if v, ok := sys.get(); !ok || v != animStr("1") {
		t.Fatalf("o valor do outro processo tem de ficar: %+v %v", v, ok)
	}
}

func TestAnimacoesCaminhoFelizDepoisDeRevalidarAplica(t *testing.T) {
	sys, st := newAnimSys(), &privStore{}
	sys.set(animStr("1"))
	d := animDeps(t, sys, st, "windows", true, "")
	d.Stdin = &hookReader{fn: func() {}}
	if _, err := runAnim(t, d, "animacoes"); err != nil || sys.writes != 1 {
		t.Fatalf("err=%v writes=%d", err, sys.writes)
	}
}

// Com --yes não há pergunta, mas o plano é recalculado logo antes de aplicar (janela mínima).
func TestAnimacoesYesRecalculaOPlanoAntesDeAplicar(t *testing.T) {
	sys, st := newAnimSys(), &privStore{}
	sys.afterRead = func() { sys.set(animStr("1")) } // depois da 1ª leitura (a do plano mostrado)
	_, err := runAnim(t, animDeps(t, sys, st, "windows", false, ""), "animacoes", "--yes")
	if err == nil || !strings.Contains(err.Error(), msgStale) {
		t.Fatalf("erro = %v", err)
	}
	if sys.writes != 0 || st.b != nil {
		t.Fatalf("zero mutações: writes=%d", sys.writes)
	}
}

func animApplied(t *testing.T) (*animSys, *privStore) {
	t.Helper()
	sys, st := newAnimSys(), &privStore{}
	sys.set(animStr("1"))
	if _, err := runAnim(t, animDeps(t, sys, st, "windows", false, ""), "animacoes", "--yes"); err != nil {
		t.Fatal(err)
	}
	return sys, st
}

func TestAnimacoesDesfazerEstadoMudaDuranteAPerguntaNaoRestaura(t *testing.T) {
	sys, st := animApplied(t)
	before, _ := json.Marshal(st.b)
	writes := sys.writes
	d := animDeps(t, sys, st, "windows", true, "")
	d.Stdin = &hookReader{fn: func() { sys.set(animStr("5")) }}
	_, err := runAnim(t, d, "animacoes", "desfazer")
	if err == nil || !strings.Contains(err.Error(), msgStale) {
		t.Fatalf("erro = %v", err)
	}
	after, _ := json.Marshal(st.b)
	if sys.writes != writes || string(before) != string(after) {
		t.Fatalf("nada pode mudar: writes %d->%d\n%s\n%s", writes, sys.writes, before, after)
	}
}

func TestAnimacoesDesfazerBackupMudaDuranteAPerguntaNaoRestaura(t *testing.T) {
	sys, st := animApplied(t)
	writes := sys.writes
	d := animDeps(t, sys, st, "windows", true, "")
	d.Stdin = &hookReader{fn: func() {
		for id, e := range st.b.Entries {
			e.Existed, e.Str = false, ""
			st.b.Entries[id] = e
		}
	}}
	_, err := runAnim(t, d, "animacoes", "desfazer")
	if err == nil || !strings.Contains(err.Error(), msgStale) || sys.writes != writes || st.b == nil {
		t.Fatalf("err=%v writes=%d->%d backup=%v", err, writes, sys.writes, st.b)
	}
}

func TestAnimacoesDesfazerCaminhoFelizRevalidaERestaura(t *testing.T) {
	sys, st := animApplied(t)
	d := animDeps(t, sys, st, "windows", true, "")
	d.Stdin = &hookReader{fn: func() {}}
	if _, err := runAnim(t, d, "animacoes", "desfazer"); err != nil {
		t.Fatal(err)
	}
	if v, ok := sys.get(); !ok || v != animStr("1") || st.b != nil {
		t.Fatalf("v=%+v ok=%v backup=%v", v, ok, st.b)
	}
}

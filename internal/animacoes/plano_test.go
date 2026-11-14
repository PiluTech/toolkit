package animacoes

import (
	"context"
	"strings"
	"testing"

	"github.com/PiluTech/toolkit/internal/diagfmt"
)

const keyMin = `HKCU\Control Panel\Desktop\WindowMetrics`

func TestPlanoMostraAtualNovoESelo(t *testing.T) {
	f := newFake()
	f.regs[rkey(keyMin, "MinAnimate")] = str("1")
	plan, err := Build(context.Background(), f, diagfmt.PT)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Pending() != 1 {
		t.Fatalf("pendentes=%d", plan.Pending())
	}
	out := plan.Format(diagfmt.PT)
	for _, want := range []string{`"1" -> "0"`, "MinAnimate", "experimental", "EXPERIMENTAL", "reabrir", "sensação", "HKCU"} {
		if !strings.Contains(strings.ToLower(out), strings.ToLower(want)) {
			t.Errorf("faltou %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "HKLM") {
		t.Errorf("animações nunca tocam HKLM:\n%s", out)
	}
	if len(f.log) != 0 {
		t.Fatalf("montar o plano não escreve: %v", f.log)
	}
}

func TestPlanoValorAusenteViraNaoDefinido(t *testing.T) {
	plan, err := Build(context.Background(), newFake(), diagfmt.PT)
	if err != nil || plan.Pending() != 1 {
		t.Fatalf("err=%v plan=%+v", err, plan)
	}
	if !strings.Contains(plan.Format(diagfmt.PT), "não definido -> \"0\"") {
		t.Errorf("\n%s", plan.Format(diagfmt.PT))
	}
}

func TestPlanoJaAplicadoNaoTemPendencia(t *testing.T) {
	f := newFake()
	f.regs[rkey(keyMin, "MinAnimate")] = str("0")
	plan, _ := Build(context.Background(), f, diagfmt.PT)
	if plan.Pending() != 0 || !strings.Contains(plan.Format(diagfmt.PT), "já está assim") {
		t.Errorf("pend=%d\n%s", plan.Pending(), plan.Format(diagfmt.PT))
	}
}

func TestPlanoTipoDiferenteContaComoPendente(t *testing.T) {
	f := newFake()
	f.regs[rkey(keyMin, "MinAnimate")] = DWORD(0) // mesmo número, outro tipo: o catálogo escreve texto
	plan, _ := Build(context.Background(), f, diagfmt.PT)
	if plan.Pending() != 1 {
		t.Fatal("DWORD 0 não é o texto \"0\"")
	}
}

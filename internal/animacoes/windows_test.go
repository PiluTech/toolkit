package animacoes

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/PiluTech/toolkit/internal/exec"
)

// recRunner registra todo comando e responde "valor ausente provado" ou sucesso, sem executar nada.
type recRunner struct{ calls []exec.Cmd }

func (r *recRunner) Run(_ context.Context, c exec.Cmd) (exec.Result, error) {
	r.calls = append(r.calls, c)
	return exec.Result{ExitCode: 0, Stdout: []byte("")}, nil
}
func (r *recRunner) LookPath(string) (string, error) { return "", errors.New("sem PATH") }

// Contrato: o System real recusa qualquer chave fora do HKCU ANTES de executar comando (nenhuma escrita em HKLM).
func TestSystemRealRecusaHKLMSemExecutarNada(t *testing.T) {
	r := &recRunner{}
	s := NewWindowsSystem(r, func() bool { return true })
	ctx := context.Background()
	for _, k := range []string{`HKLM\SOFTWARE\X`, `HKEY_LOCAL_MACHINE\SOFTWARE\X`, `hklm\SOFTWARE\X`, `HKCR\X`, `HKU\S-1-5\X`, `Control Panel\Desktop`} {
		if err := s.RegWrite(ctx, k, "N", str("0")); !errors.Is(err, ErrSoHKCU) {
			t.Errorf("RegWrite %q: %v", k, err)
		}
		if err := s.RegDelete(ctx, k, "N"); !errors.Is(err, ErrSoHKCU) {
			t.Errorf("RegDelete %q: %v", k, err)
		}
		if _, _, err := s.RegRead(ctx, k, "N"); !errors.Is(err, ErrSoHKCU) {
			t.Errorf("RegRead %q: %v", k, err)
		}
	}
	if len(r.calls) != 0 {
		t.Fatalf("executou comando para chave recusada: %+v", r.calls)
	}
}

// Contrato dos comandos: escrita em HKCU é reg.exe add/delete, NUNCA com Elevate; leituras só com ReadOnly.
func TestSystemRealComandosNuncaElevam(t *testing.T) {
	r := &recRunner{}
	s := NewWindowsSystem(r, func() bool { return true })
	ctx := context.Background()
	_ = s.RegWrite(ctx, keyMin, "MinAnimate", str("0"))
	_ = s.RegDelete(ctx, keyMin, "MinAnimate")
	if len(r.calls) == 0 {
		t.Fatal("nenhum comando")
	}
	var wrote bool
	for _, c := range r.calls {
		if c.Elevate {
			t.Errorf("Elevate em %v", c)
		}
		if !strings.EqualFold(c.Name, `C:\Windows\System32\reg.exe`) {
			t.Errorf("executável inesperado: %q", c.Name)
		}
		if len(c.Args) > 0 && c.Args[0] == "add" {
			wrote = true
			if c.ReadOnly || !strings.HasPrefix(c.Args[1], `HKCU\`) {
				t.Errorf("escrita: %+v", c)
			}
		}
		if len(c.Args) > 0 && c.Args[0] == "query" && !c.ReadOnly {
			t.Errorf("leitura sem ReadOnly: %+v", c)
		}
	}
	if !wrote {
		t.Fatal("faltou o reg add")
	}
}

func TestUserSIDDoSystemReal(t *testing.T) {
	r := &recRunner{}
	if _, err := UserSID(context.Background(), NewWindowsSystem(r, nil)); err == nil {
		t.Fatal("saída vazia do whoami não é SID")
	}
}

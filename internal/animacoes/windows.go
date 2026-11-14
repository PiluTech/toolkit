package animacoes

import (
	"context"
	"errors"

	"github.com/PiluTech/toolkit/internal/exec"
	"github.com/PiluTech/toolkit/internal/privacidade"
)

// ErrSoHKCU indica chave fora do HKCU: as animações são por usuário e nada é lido nem gravado em outro lugar. É devolvido
// ANTES de executar qualquer comando.
var ErrSoHKCU = errors.New("as animações só mexem em chaves do HKCU (usuário atual)")

// windowsSystem reaproveita o System do Windows da privacidade (reg.exe por caminho absoluto, leitura por
// exec.ReadOnlyRunner, escrita por exec.MutatingRunner, ausência provada, tipo preservado) mas expõe SÓ o registro e
// recusa tudo que não for HKCU. Em HKCU a escrita nunca leva Elevate.
type windowsSystem struct{ inner privacidade.System }

// NewWindowsSystem monta o System real sobre o Runner. elevated só serve ao System da privacidade por baixo (não é usado
// nenhuma vez aqui: nada é elevado).
func NewWindowsSystem(rn exec.Runner, elevated func() bool) System {
	return windowsSystem{inner: privacidade.NewWindowsSystem(rn, elevated)}
}

func hkcuOnly(key string) error {
	if len(key) < 5 || key[:5] != `HKCU\` { // exatamente "HKCU\": nada de minúsculas, HKLM, HKCR, HKU ou caminho relativo
		return ErrSoHKCU
	}
	return nil
}

func (w windowsSystem) RegRead(ctx context.Context, key, name string) (RegValue, bool, error) {
	if err := hkcuOnly(key); err != nil {
		return RegValue{}, false, err
	}
	return w.inner.RegRead(ctx, key, name)
}

func (w windowsSystem) RegWrite(ctx context.Context, key, name string, v RegValue) error {
	if err := hkcuOnly(key); err != nil {
		return err
	}
	return w.inner.RegWrite(ctx, key, name, v)
}

func (w windowsSystem) RegDelete(ctx context.Context, key, name string) error {
	if err := hkcuOnly(key); err != nil {
		return err
	}
	return w.inner.RegDelete(ctx, key, name)
}

// UserSID encaminha ao System da privacidade (whoami /user, só leitura).
func (w windowsSystem) UserSID(ctx context.Context) (string, error) {
	s, ok := w.inner.(privacidade.SIDer)
	if !ok {
		return "", errors.New("este System não informa o SID do usuário")
	}
	return s.UserSID(ctx)
}

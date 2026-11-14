package cli

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/PiluTech/toolkit/internal/exec"
	"github.com/PiluTech/toolkit/internal/exec/exectest"
	"github.com/PiluTech/toolkit/internal/usb"
	"github.com/PiluTech/toolkit/internal/usb/usbtest"
)

var usbNow = time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)

func usbDeps(t *testing.T, sys *usbtest.Sys, st *usbtest.MemStore, goos string, tty bool, input string) Deps {
	t.Helper()
	return Deps{GOOS: goos, EmbeddedYML: []byte(embedded), NewRunner: func() exec.Runner { return exectest.New(t) },
		UpdateCheck: UpdateCheckDeps{Env: func(string) string { return "" }},
		Stdin:       strings.NewReader(input), Interactive: func() bool { return tty },
		Usb: UsbDeps{System: sys, Store: st, Now: func() time.Time { return usbNow }}}
}

func runUsb(t *testing.T, deps Deps, args ...string) (string, error) {
	t.Helper()
	var o, e bytes.Buffer
	cmd := NewRootCmdWith(&o, &e, deps)
	cmd.SetArgs(append([]string{"usb", "--no-update-check"}, args...))
	err := cmd.ExecuteContext(context.Background())
	return o.String(), err
}

func TestUsbForaDoWindowsNaoTocaEmNada(t *testing.T) {
	for _, args := range [][]string{{}, {"aplicar", "--yes"}, {"aplicar", "--dry-run"}, {"confirmar"}, {"desfazer", "--yes"}, {"reverter"}} {
		sys, st := usbtest.NewSys(), &usbtest.MemStore{}
		// Usb.System nil + runner sem roteiro: qualquer comando real falharia o teste
		d := usbDeps(t, sys, st, "linux", true, "")
		d.Usb.System = nil
		_, err := runUsb(t, d, args...)
		if err == nil || !strings.Contains(err.Error(), "só existe no Windows") {
			t.Errorf("%v: erro = %v", args, err)
		}
		if sys.Writes() != 0 || len(sys.Log) != 0 {
			t.Errorf("%v: nada pode mudar", args)
		}
	}
}

func TestUsbEstadoSoLeEMostraExperimentalELimite(t *testing.T) {
	sys, st := usbtest.NewSys(), &usbtest.MemStore{}
	out, err := runUsb(t, usbDeps(t, sys, st, "windows", false, ""))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"EXPERIMENTAL", "nunca aplicado", "reduz o risco, não o elimina"} {
		if !strings.Contains(out, want) {
			t.Errorf("faltou %q:\n%s", want, out)
		}
	}
	if len(sys.Log) != 0 {
		t.Errorf("o estado só lê: %v", sys.Log)
	}
}

func TestUsbDryRunMostraPlanoENaoMuda(t *testing.T) {
	sys, st := usbtest.NewSys(), &usbtest.MemStore{}
	out, err := runUsb(t, usbDeps(t, sys, st, "windows", false, ""), "aplicar", "--dry-run")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"EXPERIMENTAL", "Teclado USB", "FFFF:0001", "Pendrive USB", "Prova de dispositivos essenciais: OK", "--dry-run: nada foi alterado", "10 minuto(s)"} {
		if !strings.Contains(out, want) {
			t.Errorf("plano sem %q:\n%s", want, out)
		}
	}
	if len(sys.Log) != 0 || sys.Task.Exists {
		t.Errorf("--dry-run mudou algo: %v", sys.Log)
	}
}

func TestUsbDryRunSemAdminFunciona(t *testing.T) {
	sys, st := usbtest.NewSys(), &usbtest.MemStore{}
	sys.Admin = false
	if _, err := runUsb(t, usbDeps(t, sys, st, "windows", false, ""), "aplicar", "--dry-run"); err != nil {
		t.Fatalf("o plano não exige administrador: %v", err)
	}
}

func TestUsbAplicarSemAdminRecusaAntesDePerguntar(t *testing.T) {
	sys, st := usbtest.NewSys(), &usbtest.MemStore{}
	sys.Admin = false
	out, err := runUsb(t, usbDeps(t, sys, st, "windows", true, "s\n"), "aplicar")
	if err == nil || !strings.Contains(err.Error(), "administrador") || !strings.Contains(err.Error(), "nunca eleva") {
		t.Fatalf("err = %v", err)
	}
	if strings.Contains(out, "[s/N]") || len(sys.Log) != 0 {
		t.Errorf("perguntou ou mudou algo sem administrador: %q %v", out, sys.Log)
	}
}

func TestUsbAplicarSemYesESemTerminalErraAntesDeMudar(t *testing.T) {
	sys, st := usbtest.NewSys(), &usbtest.MemStore{}
	out, err := runUsb(t, usbDeps(t, sys, st, "windows", false, ""), "aplicar")
	if err == nil || !strings.Contains(err.Error(), "--yes") {
		t.Fatalf("err = %v", err)
	}
	if !strings.Contains(out, "Prova de dispositivos essenciais") || len(sys.Log) != 0 {
		t.Errorf("o plano deve aparecer antes e nada pode mudar: %v", sys.Log)
	}
}

func TestUsbAplicarComYesAplicaComReversao(t *testing.T) {
	sys, st := usbtest.NewSys(), &usbtest.MemStore{}
	out, err := runUsb(t, usbDeps(t, sys, st, "windows", false, ""), "aplicar", "--yes", "--prazo", "5")
	if err != nil {
		t.Fatalf("err = %v\n%s", err, out)
	}
	want := usbNow.Add(5 * time.Minute)
	if !sys.Task.Exists || !sys.Task.Next.Equal(want) {
		t.Fatalf("tarefa = %#v", sys.Task)
	}
	for _, w := range []string{"REVERTE SOZINHO às " + want.Local().Format("15:04"), "toolkit usb confirmar", "toolkit usb desfazer", "reduz o risco, não o elimina"} {
		if !strings.Contains(out, w) {
			t.Errorf("saída sem %q:\n%s", w, out)
		}
	}
	// ordem: a tarefa antes da primeira chave
	arm, firstReg := -1, -1
	for i, l := range sys.Log {
		if l == "task-arm" && arm < 0 {
			arm = i
		}
		if strings.HasPrefix(l, "reg-write:") && firstReg < 0 {
			firstReg = i
		}
	}
	if arm < 0 || firstReg < 0 || arm > firstReg {
		t.Fatalf("ordem: %v", sys.Log)
	}
}

func TestUsbPrazoInvalidoRecusaSemMudar(t *testing.T) {
	for _, p := range []string{"1", "0", "-3", "61"} {
		sys, st := usbtest.NewSys(), &usbtest.MemStore{}
		_, err := runUsb(t, usbDeps(t, sys, st, "windows", false, ""), "aplicar", "--yes", "--prazo", p)
		if !errors.Is(err, usb.ErrPrazo) {
			t.Errorf("prazo %s: err = %v", p, err)
		}
		if len(sys.Log) != 0 {
			t.Errorf("prazo %s mudou algo", p)
		}
	}
}

func TestUsbTecladoDesmarcadoNaoExisteNoCLIMasSemProvaRecusa(t *testing.T) {
	sys, st := usbtest.NewSys(), &usbtest.MemStore{}
	// máquina sem disco interno: a prova não fecha, o CLI recusa e nada muda
	var keep []usb.Device
	for _, d := range sys.Devs {
		if d.Class != "DiskDrive" || strings.HasPrefix(d.InstanceID, "USBSTOR") {
			keep = append(keep, d)
		}
	}
	sys.Devs = keep
	out, err := runUsb(t, usbDeps(t, sys, st, "windows", false, ""), "aplicar", "--yes")
	if err == nil || !strings.Contains(err.Error(), "nada foi alterado") {
		t.Fatalf("err = %v", err)
	}
	if !strings.Contains(out, "RECUSADO") || len(sys.Log) != 0 {
		t.Errorf("recusa sem aviso ou mudou algo: %v\n%s", sys.Log, out)
	}
}

func TestUsbRevalidaDepoisDoSN(t *testing.T) {
	sys, st := usbtest.NewSys(), &usbtest.MemStore{}
	reads := 0
	sys.OnRead = func() {
		reads++
		if reads == 2 { // a releitura DEPOIS da confirmação: um pendrive foi retirado durante a pergunta
			sys.Devs = usbtest.Devices(false)
		}
	}
	out, err := runUsb(t, usbDeps(t, sys, st, "windows", true, "s\n"), "aplicar")
	if err == nil || !strings.Contains(err.Error(), "o estado mudou desde o plano mostrado") {
		t.Fatalf("err = %v\n%s", err, out)
	}
	if len(sys.Log) != 0 || sys.Task.Exists {
		t.Fatalf("mudou algo apesar do plano velho: %v", sys.Log)
	}
}

func TestUsbConfirmarCancelaAReversao(t *testing.T) {
	sys, st := usbtest.NewSys(), &usbtest.MemStore{}
	d := usbDeps(t, sys, st, "windows", false, "")
	if _, err := runUsb(t, d, "aplicar", "--yes"); err != nil {
		t.Fatal(err)
	}
	out, err := runUsb(t, d, "confirmar")
	if err != nil || !strings.Contains(out, "reversão automática foi cancelada") {
		t.Fatalf("%v %s", err, out)
	}
	if sys.Task.Exists || len(sys.Tree.Entries) == 0 {
		t.Fatal("a tarefa deveria sair e a política ficar")
	}
	if _, err := runUsb(t, d, "confirmar"); err == nil || !strings.Contains(err.Error(), "não há reversão pendente") {
		t.Fatalf("segunda confirmação: %v", err)
	}
	if out, _ := runUsb(t, d); !strings.Contains(out, "aplicado e confirmado") {
		t.Fatalf("estado: %s", out)
	}
}

func TestUsbConfirmarSemAdmin(t *testing.T) {
	sys, st := usbtest.NewSys(), &usbtest.MemStore{}
	d := usbDeps(t, sys, st, "windows", false, "")
	if _, err := runUsb(t, d, "aplicar", "--yes"); err != nil {
		t.Fatal(err)
	}
	sys.Admin = false
	if _, err := runUsb(t, d, "confirmar"); err == nil || !strings.Contains(err.Error(), "administrador") {
		t.Fatalf("err = %v", err)
	}
	if !sys.Task.Exists {
		t.Fatal("removeu a tarefa sem administrador")
	}
}

func TestUsbDesfazer(t *testing.T) {
	sys, st := usbtest.NewSys(), &usbtest.MemStore{}
	d := usbDeps(t, sys, st, "windows", false, "")
	out, err := runUsb(t, d, "desfazer")
	if err != nil || !strings.Contains(out, "Nada a desfazer") {
		t.Fatalf("nunca aplicado: %v %s", err, out)
	}
	if _, err := runUsb(t, d, "aplicar", "--yes"); err != nil {
		t.Fatal(err)
	}
	n := sys.Writes()
	out, err = runUsb(t, d, "desfazer", "--dry-run")
	if err != nil || !strings.Contains(out, "O que será restaurado") || sys.Writes() != n {
		t.Fatalf("dry-run: %v\n%s", err, out)
	}
	if _, err := runUsb(t, d, "desfazer"); err == nil || !strings.Contains(err.Error(), "--yes") {
		t.Fatalf("sem yes e sem terminal: %v", err)
	}
	out, err = runUsb(t, d, "desfazer", "--yes")
	if err != nil || !strings.Contains(out, "Restaurado") {
		t.Fatalf("desfazer: %v\n%s", err, out)
	}
	if len(sys.Tree.Entries) != 0 || sys.Tree.KeyExists || sys.Task.Exists {
		t.Fatalf("não restaurou: %v", sys.Tree)
	}
}

func TestUsbDesfazerEstadoOriginalDesconhecido(t *testing.T) {
	sys, st := usbtest.NewSys(), &usbtest.MemStore{}
	d := usbDeps(t, sys, st, "windows", false, "")
	if _, err := runUsb(t, d, "aplicar", "--yes"); err != nil {
		t.Fatal(err)
	}
	_ = st.Remove() // o backup sumiu depois de aplicar
	n := sys.Writes()
	out, err := runUsb(t, d, "desfazer", "--yes")
	if err == nil || !strings.Contains(out, "ESTADO ORIGINAL DESCONHECIDO") || strings.Contains(out, "Nada a desfazer") {
		t.Fatalf("err=%v\n%s", err, out)
	}
	if !strings.Contains(out, "WinRE") || sys.Writes() != n {
		t.Fatalf("recuperação manual ausente ou escreveu: %d != %d", sys.Writes(), n)
	}
	// --dry-run mostra o mesmo sem erro
	if out, err := runUsb(t, d, "desfazer", "--dry-run"); err != nil || !strings.Contains(out, "ESTADO ORIGINAL DESCONHECIDO") {
		t.Fatalf("dry-run: %v\n%s", err, out)
	}
	if out, _ := runUsb(t, d); !strings.Contains(out, "ESTADO ORIGINAL DESCONHECIDO") {
		t.Fatalf("o estado deve dizer o mesmo: %s", out)
	}
}

func TestUsbDesfazerRevalidaDepoisDoSN(t *testing.T) {
	sys, st := usbtest.NewSys(), &usbtest.MemStore{}
	d := usbDeps(t, sys, st, "windows", false, "")
	if _, err := runUsb(t, d, "aplicar", "--yes"); err != nil {
		t.Fatal(err)
	}
	// terminal com resposta "s"; o snapshot some entre o plano e a confirmação
	d2 := usbDeps(t, sys, st, "windows", true, "s\n")
	d2.Stdin = &swapReader{r: strings.NewReader("s\n"), onRead: func() { _ = st.Remove() }}
	n := sys.Writes()
	_, err := runUsb(t, d2, "desfazer")
	if err == nil || sys.Writes() != n {
		t.Fatalf("err=%v writes=%d->%d", err, n, sys.Writes())
	}
}

type swapReader struct {
	r      *strings.Reader
	onRead func()
	done   bool
}

func (s *swapReader) Read(p []byte) (int, error) {
	if !s.done {
		s.done = true
		s.onRead()
	}
	return s.r.Read(p)
}

func TestUsbRevertEHidden(t *testing.T) {
	sys, st := usbtest.NewSys(), &usbtest.MemStore{}
	d := usbDeps(t, sys, st, "windows", false, "")
	if _, err := runUsb(t, d, "aplicar", "--yes"); err != nil {
		t.Fatal(err)
	}
	out, err := runUsb(t, d, "reverter")
	if err != nil || !strings.Contains(out, "Restaurado") {
		t.Fatalf("%v %s", err, out)
	}
	if len(sys.Tree.Entries) != 0 || sys.Task.Exists {
		t.Fatal("a reversão automática não restaurou")
	}
	var o, e bytes.Buffer
	cmd := NewRootCmdWith(&o, &e, d)
	cmd.SetArgs([]string{"usb", "--help"})
	_ = cmd.ExecuteContext(context.Background())
	if strings.Contains(o.String(), "reverter") {
		t.Error("o subcomando reverter é de uso interno e não deve aparecer na ajuda")
	}
	for _, w := range []string{"EXPERIMENTAL", "aplicar", "confirmar", "desfazer", "reduz o risco, não o elimina"} {
		if !strings.Contains(o.String(), w) {
			t.Errorf("ajuda sem %q", w)
		}
	}
}

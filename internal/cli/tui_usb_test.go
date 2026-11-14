package cli

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/PiluTech/toolkit/internal/tui"
	"github.com/PiluTech/toolkit/internal/usb"
	"github.com/PiluTech/toolkit/internal/usb/usbtest"
)

func usbBe(t *testing.T, sys *usbtest.Sys, st *usbtest.MemStore) *usbBackend {
	t.Helper()
	return &usbBackend{deps: usbDeps(t, sys, st, "windows", false, "")}
}

func TestTUIUsbForaDoWindowsSoAvisa(t *testing.T) {
	sys, st := usbtest.NewSys(), &usbtest.MemStore{}
	d := usbDeps(t, sys, st, "linux", false, "")
	d.Usb.System = nil // qualquer comando real falharia o teste
	info, err := (&usbBackend{deps: d}).Load(context.Background())
	if err != nil || info.Supported || info.OS != "linux" {
		t.Fatalf("info=%#v err=%v", info, err)
	}
}

func TestTUIUsbLoadMostraDispositivosEstadoEEdicao(t *testing.T) {
	sys, st := usbtest.NewSys(), &usbtest.MemStore{}
	be := usbBe(t, sys, st)
	info, err := be.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !info.Supported || !info.EditionOK || !info.IsAdmin || info.State != tui.UsbClean || len(info.Devices) != 2 {
		t.Fatalf("info = %#v", info)
	}
	sys.EditionID = usb.EditionHome
	info, _ = usbBe(t, sys, st).Load(context.Background())
	if info.EditionOK {
		t.Fatal("Home não é suportado")
	}
	if len(sys.Log) != 0 {
		t.Fatalf("Load só lê: %v", sys.Log)
	}
}

func TestTUIUsbAplicarPorIDDeUsoUnico(t *testing.T) {
	sys, st := usbtest.NewSys(), &usbtest.MemStore{}
	be := usbBe(t, sys, st)
	ctx := context.Background()
	p, err := be.Plan(ctx, nil)
	if err != nil || !p.OK || p.Allowed != 2 || p.Blocked != 0 || p.Prazo != usb.PrazoPadrao {
		t.Fatalf("plano = %#v err=%v", p, err)
	}
	if _, err := be.Apply(ctx, "plano-que-nao-existe"); !errors.Is(err, tui.ErrUsbStalePlan) {
		t.Fatalf("ID inexistente: %v", err)
	}
	if sys.Writes() != 0 {
		t.Fatal("ID inválido não pode escrever")
	}
	// o ID inválido já consumiu (descartou) o plano p: ele não vale mais
	if _, err := be.Apply(ctx, p.ID); !errors.Is(err, tui.ErrUsbStalePlan) {
		t.Fatalf("o plano descartado não pode ser usado: %v", err)
	}
	p, _ = be.Plan(ctx, nil)
	res, err := be.Apply(ctx, p.ID)
	if err != nil || res.Entries == 0 || res.Deadline == "" {
		t.Fatalf("res=%#v err=%v", res, err)
	}
	if _, err := be.Apply(ctx, p.ID); !errors.Is(err, tui.ErrUsbStalePlan) {
		t.Fatalf("uso único: %v", err)
	}
}

func TestTUIUsbPlanoVelhoDepoisQueOSistemaMudou(t *testing.T) {
	sys, st := usbtest.NewSys(), &usbtest.MemStore{}
	be := usbBe(t, sys, st)
	ctx := context.Background()
	p, _ := be.Plan(ctx, nil)
	sys.Devs = usbtest.Devices(false) // o pendrive foi retirado depois do plano
	if _, err := be.Apply(ctx, p.ID); !errors.Is(err, tui.ErrUsbStalePlan) {
		t.Fatalf("err = %v", err)
	}
	if sys.Writes() != 0 || sys.Task.Exists {
		t.Fatalf("mudou algo com o plano velho: %v", sys.Log)
	}
}

func TestTUIUsbSemAdminRecusaAntesDeMudar(t *testing.T) {
	sys, st := usbtest.NewSys(), &usbtest.MemStore{}
	be := usbBe(t, sys, st)
	ctx := context.Background()
	p, _ := be.Plan(ctx, nil)
	if p.IsAdmin != true {
		t.Fatal("IsAdmin")
	}
	sys.Admin = false
	if _, err := be.Apply(ctx, p.ID); err == nil {
		t.Fatal("devia falhar")
	}
	sys.Admin = true
	p, _ = be.Plan(ctx, nil)
	sys.Admin = false
	_, err := be.Apply(ctx, p.ID)
	if !errors.Is(err, tui.ErrUsbStalePlan) && !errors.Is(err, tui.ErrUsbNeedsAdmin) {
		t.Fatalf("err = %v", err)
	}
	if len(sys.Log) != 0 {
		t.Fatalf("mudou algo sem administrador: %v", sys.Log)
	}
}

func TestTUIUsbTecladoDesmarcadoRecusaNoPlano(t *testing.T) {
	sys, st := usbtest.NewSys(), &usbtest.MemStore{}
	be := usbBe(t, sys, st)
	p, err := be.Plan(context.Background(), []string{usbtest.KeyboardKey})
	if err != nil {
		t.Fatal(err)
	}
	if p.OK || !strings.Contains(p.Refusal, "Teclado") || p.Blocked != 1 {
		t.Fatalf("plano = %#v", p)
	}
	if _, err := be.Apply(context.Background(), p.ID); err == nil {
		t.Fatal("o plano recusado não pode ser aplicado")
	}
	if len(sys.Log) != 0 {
		t.Fatalf("mudou algo: %v", sys.Log)
	}
}

func TestTUIUsbManterEDesfazerPorID(t *testing.T) {
	sys, st := usbtest.NewSys(), &usbtest.MemStore{}
	be := usbBe(t, sys, st)
	ctx := context.Background()
	if p, _ := be.KeepPlan(ctx); p.OK {
		t.Fatal("sem nada aplicado não há o que manter")
	}
	p, _ := be.Plan(ctx, nil)
	if _, err := be.Apply(ctx, p.ID); err != nil {
		t.Fatal(err)
	}
	info, _ := be.Load(ctx)
	if info.State != tui.UsbArmed || info.Deadline == "" {
		t.Fatalf("info = %#v", info)
	}
	kp, _ := be.KeepPlan(ctx)
	if !kp.OK || !strings.Contains(kp.Text, "removida") {
		t.Fatalf("keep plan = %#v", kp)
	}
	if _, err := be.Keep(ctx, kp.ID); err != nil {
		t.Fatal(err)
	}
	if sys.Task.Exists {
		t.Fatal("manter remove a tarefa")
	}
	info, _ = be.Load(ctx)
	if info.State != tui.UsbActive {
		t.Fatalf("info = %#v", info)
	}
	up, _ := be.UndoPlan(ctx)
	if !up.OK {
		t.Fatalf("undo plan = %#v", up)
	}
	// o snapshot some entre o plano e a confirmação: o plano é velho
	_ = st.Remove()
	if _, err := be.Undo(ctx, up.ID); !errors.Is(err, tui.ErrUsbStalePlan) {
		t.Fatalf("err = %v", err)
	}
}

func TestTUIUsbEstadoDesconhecidoNaoViraConfirmacao(t *testing.T) {
	sys, st := usbtest.NewSys(), &usbtest.MemStore{}
	be := usbBe(t, sys, st)
	ctx := context.Background()
	p, _ := be.Plan(ctx, nil)
	if _, err := be.Apply(ctx, p.ID); err != nil {
		t.Fatal(err)
	}
	_ = st.Remove()
	n := sys.Writes()
	info, _ := usbBe(t, sys, st).Load(ctx)
	if info.State != tui.UsbUnknown || info.Reason == "" {
		t.Fatalf("info = %#v", info)
	}
	up, _ := usbBe(t, sys, st).UndoPlan(ctx)
	if up.OK || !strings.Contains(up.Text, "ESTADO ORIGINAL DESCONHECIDO") || !strings.Contains(up.Refusal, "ESTADO ORIGINAL DESCONHECIDO") {
		t.Fatalf("undo plan = %#v", up)
	}
	if _, err := be.Undo(ctx, up.ID); err == nil {
		t.Fatal("o desfazer não pode rodar sem snapshot")
	}
	if sys.Writes() != n {
		t.Fatal("escreveu sem snapshot")
	}
}

func TestTUIUsbAbaMontadaNaAreaSeguranca(t *testing.T) {
	_, areas := inicioDe(t, "windows", true)
	var found bool
	for _, a := range areas {
		if a.Key != "seguranca" {
			continue
		}
		for _, tb := range a.Tabs {
			if tb.Title() == "USB permitidos (experimental)" {
				found = true
			}
		}
	}
	if !found {
		t.Fatal("a seção USB permitidos (experimental) tem de estar na área Segurança")
	}
}

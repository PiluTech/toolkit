package usb

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
)

func mustAnalyze(t *testing.T, devs []Device, excluded ...string) *Analysis {
	t.Helper()
	a, err := Analyze(devs, excluded)
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	return a
}

func proofFor(a *Analysis, name string) *ProofLine {
	for i := range a.Proof.Lines {
		if a.Proof.Lines[i].Name == name {
			return &a.Proof.Lines[i]
		}
	}
	return nil
}

func TestMaquinaBaseComTecladoUSBTemProvaCompleta(t *testing.T) {
	a := mustAnalyze(t, machine(usbKeyboard("0001")))
	if !a.Proof.OK {
		t.Fatalf("prova falhou: %v", a.Proof.Failures)
	}
	// essenciais de TODOS os barramentos aparecem na prova, todos permitidos
	for _, n := range []string{"Teclado PS/2 padrão", "Touchpad I2C HID", "Teclado Bluetooth", "Disco NVMe", "Disco SATA", "Controlador NVMe", "Controlador SATA", "Controlador xHCI", "Hub raiz USB 3", "Teclado HID", "Controle de mídia HID"} {
		l := proofFor(a, n)
		if l == nil {
			t.Errorf("%q fora da prova", n)
			continue
		}
		if !l.Allowed {
			t.Errorf("%q bloqueado na prova", n)
		}
	}
}

func TestTecladoI2CPS2BluetoothNaoSaoAfetados(t *testing.T) {
	a := mustAnalyze(t, machine())
	for _, n := range []string{"Teclado PS/2 padrão", "Touchpad I2C HID", "Teclado Bluetooth"} {
		l := proofFor(a, n)
		if l == nil || !l.Allowed {
			t.Fatalf("%q deveria estar permitido: %#v", n, l)
		}
		if !strings.Contains(l.By, "nenhuma regra") {
			t.Errorf("%q: evidência = %q", n, l.By)
		}
	}
	if proofFor(a, "Touchpad I2C HID").Bus != "ACPI" {
		t.Errorf("barramento do touchpad = %q", proofFor(a, "Touchpad I2C HID").Bus)
	}
}

func TestDiscosNVMeESATAEControladoresPermitidos(t *testing.T) {
	a := mustAnalyze(t, machine())
	for _, n := range []string{"Disco NVMe", "Controlador NVMe", "Disco SATA", "Controlador SATA"} {
		if l := proofFor(a, n); l == nil || !l.Allowed {
			t.Errorf("%q deveria estar permitido", n)
		}
	}
}

func TestPoliticaNuncaContemClasseAmplaNemDenyUnspecified(t *testing.T) {
	a := mustAnalyze(t, machine(usbKeyboard("0001"), usbPendrive("0003")))
	for _, c := range a.Policy.DenyClasses {
		low := strings.ToLower(c)
		for _, bad := range []string{guidKeyboard, guidMouse, guidHID, guidDisk, guidHDC, guidSCSI, guidBT, guidSystem} {
			if low == bad {
				t.Fatalf("classe ampla %s no bloqueio", c)
			}
		}
	}
	want := []string{"{36fc9e60-c465-11cf-8056-444553540000}", "{88bae032-5a81-49f0-bc3d-a4ff138216d6}"}
	var got []string
	for _, c := range a.Policy.DenyClasses {
		got = append(got, strings.ToLower(c))
	}
	if !slices.Equal(got, want) {
		t.Fatalf("classes negadas = %v", got)
	}
	for _, e := range a.Policy.Entries() {
		if strings.Contains(e.Path, "DenyUnspecified") || strings.Contains(e.Path, "DenyDeviceIDs") || strings.Contains(e.Path, "Retroactive") {
			t.Fatalf("a política escreveu %q", e.Path)
		}
	}
}

func TestListaSoTemVIDPIDOuOsGenericosFechados(t *testing.T) {
	a := mustAnalyze(t, machine(usbKeyboard("0001"), usbPendrive("0003")))
	for _, id := range a.Policy.AllowIDs {
		if _, ok := vidpid(id); ok {
			continue
		}
		if !slices.Contains(genericAllowIDs, id) {
			t.Errorf("ID %q na lista: nem VID/PID nem do conjunto genérico fechado", id)
		}
	}
	for _, g := range genericAllowIDs {
		if !slices.Contains(a.Policy.AllowIDs, g) {
			t.Errorf("genérico %q ausente da lista", g)
		}
	}
	// ID compatível genérico NUNCA entra (liberaria a classe inteira)
	for _, bad := range []string{`USB\COMPOSITE`, `HID_DEVICE`, `HID_DEVICE_SYSTEM_KEYBOARD`, `USB\Class_03`, `HID_DEVICE_UP:0001_U:0006`} {
		if slices.Contains(a.Policy.AllowIDs, bad) {
			t.Errorf("ID compatível genérico %q entrou na lista", bad)
		}
	}
}

func TestListaIncluiTodosOsNosDoComposto(t *testing.T) {
	a := mustAnalyze(t, machine(usbKeyboard("0001")))
	for _, id := range []string{
		`USB\VID_FFFF&PID_0001`, `USB\VID_FFFF&PID_0001&REV_0100`,
		`USB\VID_FFFF&PID_0001&MI_00`, `USB\VID_FFFF&PID_0001&MI_01`,
		`HID\VID_FFFF&PID_0001&MI_00`, `HID\VID_FFFF&PID_0001&MI_01`,
	} {
		if !slices.Contains(a.Policy.AllowIDs, id) {
			t.Errorf("ID do nó %q fora da lista", id)
		}
	}
}

func TestDispositivosListadosSoSaoUSBConectados(t *testing.T) {
	a := mustAnalyze(t, machine(usbKeyboard("0001"), usbPendrive("0003")))
	var names []string
	for _, u := range a.Units {
		names = append(names, u.Name)
		if !u.Selected {
			t.Errorf("%q deveria vir marcado por padrão", u.Name)
		}
		if u.VIDPID == "" {
			t.Errorf("%q sem VID:PID", u.Name)
		}
	}
	slices.Sort(names)
	want := []string{"Pendrive USB", "Rádio Bluetooth", "Teclado USB composto"}
	if !slices.Equal(names, want) {
		t.Fatalf("dispositivos = %v, queria %v (nunca interno não-USB, nunca hub raiz)", names, want)
	}
}

func TestTecladoDesmarcadoRecusa(t *testing.T) {
	devs := machine(usbKeyboard("0001"), usbPendrive("0003"))
	a := mustAnalyze(t, devs, `USB\VID_FFFF&PID_0001\6&9999&0&1`)
	if a.Proof.OK {
		t.Fatal("teclado conectado desmarcado: a prova NÃO pode passar")
	}
	if len(a.Proof.Failures) == 0 || !strings.Contains(strings.Join(a.Proof.Failures, "\n"), "Teclado") {
		t.Fatalf("falhas = %v", a.Proof.Failures)
	}
	if l := proofFor(a, "Teclado HID"); l == nil || l.Allowed {
		t.Fatalf("o nó HID do teclado desmarcado devia aparecer bloqueado: %#v", l)
	}
}

func TestMouseDesmarcadoRecusa(t *testing.T) {
	mouse := []Device{
		dev(`USB\VID_FFFF&PID_0007\6&mouse&0&3`, hub30, "Mouse USB", "USB", []string{`USB\VID_FFFF&PID_0007&REV_0100`, `USB\VID_FFFF&PID_0007`}),
		dev(`HID\VID_FFFF&PID_0007\8&mouse&0&0000`, `USB\VID_FFFF&PID_0007\6&mouse&0&3`, "Mouse HID", "Mouse", []string{`HID\VID_FFFF&PID_0007&REV_0100`, `HID\VID_FFFF&PID_0007`}),
	}
	devs := machine(usbKeyboard("0001"), mouse)
	if a := mustAnalyze(t, devs); !a.Proof.OK {
		t.Fatalf("com tudo marcado deveria passar: %v", a.Proof.Failures)
	}
	if a := mustAnalyze(t, devs, `USB\VID_FFFF&PID_0007\6&mouse&0&3`); a.Proof.OK {
		t.Fatal("mouse desmarcado: a prova NÃO pode passar")
	}
}

func TestPendriveDesmarcadoPassaEFicaBloqueado(t *testing.T) {
	devs := machine(usbKeyboard("0001"), usbPendrive("0003"))
	a := mustAnalyze(t, devs, `USB\VID_FFFF&PID_0003\0&pen&0&2`)
	if !a.Proof.OK {
		t.Fatalf("o pendrive não é essencial: %v", a.Proof.Failures)
	}
	if slices.Contains(a.Policy.AllowIDs, `USB\VID_FFFF&PID_0003`) {
		t.Fatal("pendrive desmarcado entrou na lista")
	}
	for _, u := range a.Units {
		if u.Name == "Pendrive USB" && (u.Selected || u.Blocked != true) {
			t.Fatalf("unidade do pendrive: %#v", u)
		}
	}
}

func TestPendriveSemTecladoEDiscoUSBNaoSaoEssenciais(t *testing.T) {
	a := mustAnalyze(t, machine(usbPendrive("0003")))
	if proofFor(a, "Pendrive (disco)") != nil {
		t.Fatal("disco USB removível não é o disco do sistema: não é essencial")
	}
	if proofFor(a, "Disco NVMe") == nil {
		t.Fatal("disco interno é essencial")
	}
}

func TestRadioBluetoothDesmarcadoBloqueiaOTecladoBluetooth(t *testing.T) {
	devs := machine()
	a := mustAnalyze(t, devs, `USB\VID_FFFF&PID_0002\5&7777&0&1`)
	if a.Proof.OK {
		t.Fatal("sem o rádio Bluetooth o teclado Bluetooth perde o pai: a prova NÃO pode passar")
	}
	if l := proofFor(a, "Teclado Bluetooth"); l == nil || l.Allowed {
		t.Fatalf("teclado Bluetooth deveria estar bloqueado por herança: %#v", l)
	}
}

func TestNoDoCompostoSemVIDPIDBloqueadoRecusa(t *testing.T) {
	devs := machine(usbKeyboard("0001"))
	// um nó do composto sem VID/PID em nenhum ID de hardware, da classe USBDevice (negada): bloquearia o conjunto
	devs = append(devs, dev(`USB\VID_FFFF&PID_0001&MI_02\7&aaaa&0&0002`, `USB\VID_FFFF&PID_0001\6&9999&0&1`, "Interface genérica", "USBDevice", []string{`USB\INTERFACE_SEM_ID`}))
	a := mustAnalyze(t, devs)
	if a.Proof.OK {
		t.Fatal("nó do composto bloqueado: a prova NÃO pode passar (todos os nós têm de ficar permitidos)")
	}
	if !strings.Contains(strings.Join(a.Proof.Failures, "\n"), "Interface genérica") {
		t.Fatalf("falhas = %v", a.Proof.Failures)
	}
}

func TestControladorUSBSemIDGenericoRecusa(t *testing.T) {
	devs := baseMachine()
	i := idx(devs, "Controlador xHCI")
	devs[i].CompatibleIDs = []string{`PCI\CC_9999`} // sem PCI\CC_0C03: ficaria bloqueado pela classe USB
	a := mustAnalyze(t, devs)
	if a.Proof.OK {
		t.Fatal("controlador USB bloqueado: a prova NÃO pode passar")
	}
}

func TestIDCompativelCasaComALista(t *testing.T) {
	// o controlador xHCI só casa pelo ID COMPATÍVEL PCI\CC_0C03: a avaliação olha hardware E compatíveis
	a := mustAnalyze(t, baseMachine())
	if l := proofFor(a, "Controlador xHCI"); l == nil || !l.Allowed || !strings.Contains(l.By, `PCI\CC_0C03`) {
		t.Fatalf("controlador xHCI: %#v", l)
	}
}

func TestCasamentoDeIDIgnoraMaiusculas(t *testing.T) {
	devs := machine(usbKeyboard("0001"))
	for i := range devs {
		if devs[i].Name == "Teclado USB composto" {
			devs[i].HardwareIDs = []string{`usb\vid_ffff&pid_0001&rev_0100`, `usb\vid_ffff&pid_0001`}
		}
	}
	if a := mustAnalyze(t, devs); !a.Proof.OK {
		t.Fatalf("IDs em minúsculas deveriam casar: %v", a.Proof.Failures)
	}
}

func TestArvoreIncompletaNaAnaliseRecusa(t *testing.T) {
	devs := machine(usbKeyboard("0001"))
	var cut []Device
	for _, d := range devs {
		if d.InstanceID != hub30 {
			cut = append(cut, d)
		}
	}
	if _, err := Analyze(cut, nil); !errors.Is(err, ErrTreeIncomplete) {
		t.Fatalf("err = %v", err)
	}
}

func TestExclusaoDeInstanciaDesconhecidaRecusa(t *testing.T) {
	if _, err := Analyze(machine(), []string{`USB\VID_FFFF&PID_9999\nao-existe`}); err == nil {
		t.Fatal("exclusão de dispositivo que não existe deve ser erro (seleção velha)")
	}
}

func TestMuitosIDsRecusa(t *testing.T) {
	devs := machine()
	for i := 0; i < MaxAllowIDs+5; i++ {
		pid := fmt.Sprintf("%04X", i)
		inst := `USB\VID_FFFE&PID_` + pid + `\x` + pid
		devs = append(devs, dev(inst, hub30, "USB "+pid, "USBDevice", []string{`USB\VID_FFFE&PID_` + pid}))
	}
	if _, err := Analyze(devs, nil); err == nil {
		t.Fatal("lista acima do limite deveria recusar")
	}
}

func TestEntradasDaPoliticaEmOrdemSegura(t *testing.T) {
	a := mustAnalyze(t, machine(usbKeyboard("0001")))
	es := a.Policy.Entries()
	if es[0].Path != "AllowDenyLayered" {
		t.Fatalf("a camada tem de ser a PRIMEIRA escrita (sem ela o Deny vence o Allow): %q", es[0].Path)
	}
	last := es[len(es)-1]
	if last.Path != "DenyDeviceClasses" {
		t.Fatalf("a negação tem de ser a ÚLTIMA escrita: %q", last.Path)
	}
	posAllow, posAllowFlag := -1, -1
	for i, e := range es {
		switch e.Path {
		case "AllowDeviceIDs":
			posAllowFlag = i
		case `AllowDeviceIDs\1`:
			posAllow = i
		}
		if err := checkRel(e.Path); err != nil {
			t.Errorf("caminho %q fora da lista fechada: %v", e.Path, err)
		}
	}
	if posAllow < 0 || posAllowFlag < posAllow {
		t.Fatalf("a lista tem de ser gravada antes de ligar AllowDeviceIDs: %d %d", posAllow, posAllowFlag)
	}
}

func TestCheckRelListaFechada(t *testing.T) {
	ok := []string{"AllowDenyLayered", "AllowDeviceIDs", "DenyDeviceClasses", `AllowDeviceIDs\1`, `AllowDeviceIDs\200`, `DenyDeviceClasses\2`}
	for _, p := range ok {
		if err := checkRel(p); err != nil {
			t.Errorf("%q deveria passar: %v", p, err)
		}
	}
	bad := []string{"", "DenyUnspecified", "DenyDeviceIDs", `DenyDeviceIDs\1`, `AllowDeviceIDs\0`, `AllowDeviceIDs\201`, `AllowDeviceIDs\01`, `AllowDeviceIDs\a`,
		`..\Run`, `AllowDeviceIDs\..\x`, `HKLM\SOFTWARE\x`, `allowdeviceids`, `AllowDeviceIDs\`, `AllowDeviceIDs\1\2`, "AllowDenyLayered ", `DenyDeviceClasses\3`}
	for _, p := range bad {
		if err := checkRel(p); !errors.Is(err, ErrEntradaInvalida) {
			t.Errorf("%q deveria ser recusado: %v", p, err)
		}
	}
}

// Mouse USB SIMPLES (não composto): o nó USB do dispositivo é de classe HIDClass ("USB Input Device"), que a política NÃO
// nega. Desmarcá-lo o tiraria da lista sem que o avaliador o bloqueasse: a regra "essencial desmarcado = recusa" é
// explícita e independe da classe (revisão do advisor).
func simpleMouse() []Device {
	top := `USB\VID_FFFF&PID_0008\6&smp&0&3`
	return []Device{
		dev(top, hub30, "Mouse USB simples", "HIDClass", []string{`USB\VID_FFFF&PID_0008&REV_0100`, `USB\VID_FFFF&PID_0008`}),
		dev(`HID\VID_FFFF&PID_0008\8&smp&0&0000`, top, "Mouse HID simples", "Mouse", []string{`HID\VID_FFFF&PID_0008&REV_0100`, `HID\VID_FFFF&PID_0008`}),
	}
}

func TestMouseUSBSimplesDesmarcadoRecusaMesmoSemBloqueioPelaClasse(t *testing.T) {
	devs := machine(usbKeyboard("0001"), simpleMouse())
	if a := mustAnalyze(t, devs); !a.Proof.OK {
		t.Fatalf("com tudo marcado deveria passar: %v", a.Proof.Failures)
	}
	a := mustAnalyze(t, devs, `USB\VID_FFFF&PID_0008\6&smp&0&3`)
	if a.Proof.OK {
		t.Fatal("mouse simples desmarcado: a prova NÃO pode passar (a política não o bloquearia, mas o essencial não pode ficar fora da lista)")
	}
	if !strings.Contains(strings.Join(a.Proof.Failures, "\n"), "Mouse USB simples") || !strings.Contains(strings.Join(a.Proof.Failures, "\n"), "desmarcado") {
		t.Fatalf("falhas = %v", a.Proof.Failures)
	}
	for _, u := range a.Units {
		if u.Name == "Mouse USB simples" && (u.Selected || u.Blocked || !u.Essential) {
			t.Fatalf("o avaliador não bloquearia o nó HIDClass: %#v", u)
		}
	}
}

func TestPlanoDizQueDesmarcadoNaoBloqueadoPelaPoliticaNaoEBloqueado(t *testing.T) {
	f := newFake(t)
	f.devs = machine(usbKeyboard("0001"), simpleMouse(), []Device{
		dev(`USB\VID_FFFF&PID_0009\6&cam&0&4`, hub30, "Webcam USB simples", "HIDClass", []string{`USB\VID_FFFF&PID_0009`}),
	})
	p, err := BuildPlan(context.Background(), f, []string{`USB\VID_FFFF&PID_0009\6&cam&0&4`})
	if err != nil {
		t.Fatal(err)
	}
	// a webcam é HIDClass, então é essencial por classe (HID): desmarcar recusa; o texto não pode dizer BLOQUEADO
	txt := p.Format()
	if strings.Contains(txt, "Webcam USB simples  FFFF:0009  classe HIDClass  conectado agora · essencial (teclado, mouse ou outro HID) -> BLOQUEADO") {
		t.Fatalf("disse BLOQUEADO para um nó que a política não bloqueia:\n%s", txt)
	}
	if !strings.Contains(txt, "NÃO o bloquearia") {
		t.Fatalf("o plano devia dizer que a política não bloquearia:\n%s", txt)
	}
	if p.OK() {
		t.Fatal("HID essencial desmarcado: plano recusado")
	}
}

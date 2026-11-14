package usb

import (
	"errors"
	"strings"
	"testing"
)

func TestParseDevicesValido(t *testing.T) {
	js := `{"count":2,"devices":[
	 {"InstanceId":"HTREE\\ROOT\\0","Parent":"","Name":"Raiz","Class":"System","ClassGuid":"{4d36e97d-e325-11ce-bfc1-08002be10318}","HardwareIds":"HTREE\\ROOT\\0","CompatibleIds":null},
	 {"InstanceId":"USB\\VID_FFFF&PID_0001\\1","Parent":"HTREE\\ROOT\\0","Name":"Teclado","Class":"USB","ClassGuid":"{36fc9e60-c465-11cf-8056-444553540000}","HardwareIds":["USB\\VID_FFFF&PID_0001&REV_0100","USB\\VID_FFFF&PID_0001"],"CompatibleIds":["USB\\COMPOSITE"]}]}`
	devs, err := ParseDevices([]byte(js))
	if err != nil {
		t.Fatal(err)
	}
	if len(devs) != 2 {
		t.Fatalf("devs = %d", len(devs))
	}
	// o PowerShell devolve UM único ID como texto, não como lista
	if len(devs[0].HardwareIDs) != 1 || devs[0].HardwareIDs[0] != `HTREE\ROOT\0` {
		t.Fatalf("hardware ids de um só valor: %#v", devs[0].HardwareIDs)
	}
	if len(devs[1].HardwareIDs) != 2 || devs[1].CompatibleIDs[0] != `USB\COMPOSITE` {
		t.Fatalf("ids: %#v", devs[1])
	}
}

func TestParseDevicesRecusaLeituraIncompleta(t *testing.T) {
	cases := map[string]string{
		"json cortado":       `{"count":2,"devices":[{"InstanceId":"A\\B","Parent":"","Name":"x","Class":"System","ClassGuid":"","HardwareIds":[],"CompatibleIds":[]}`,
		"contagem diferente": `{"count":3,"devices":[{"InstanceId":"A\\B","Parent":"","Name":"x","Class":"System","ClassGuid":"","HardwareIds":[],"CompatibleIds":[]}]}`,
		"sem dispositivos":   `{"count":0,"devices":[]}`,
		"vazio":              ``,
		"lixo":               `not json`,
		"sem instancia":      `{"count":1,"devices":[{"InstanceId":"","Parent":"","Name":"x","Class":"System","ClassGuid":"","HardwareIds":[],"CompatibleIds":[]}]}`,
		"id com controle":    "{\"count\":1,\"devices\":[{\"InstanceId\":\"A\\\\B\\u001b[2J\",\"Parent\":\"\",\"Name\":\"x\",\"Class\":\"System\",\"ClassGuid\":\"\",\"HardwareIds\":[],\"CompatibleIds\":[]}]}",
		"id enorme":          `{"count":1,"devices":[{"InstanceId":"` + strings.Repeat("A", 400) + `","Parent":"","Name":"x","Class":"System","ClassGuid":"","HardwareIds":[],"CompatibleIds":[]}]}`,
	}
	for name, js := range cases {
		if _, err := ParseDevices([]byte(js)); err == nil {
			t.Errorf("%s: aceitou leitura inválida", name)
		}
	}
}

func TestParseDevicesNeutralizaTextoMalicioso(t *testing.T) {
	js := "{\"count\":1,\"devices\":[{\"InstanceId\":\"HTREE\\\\ROOT\\\\0\",\"Parent\":\"\",\"Name\":\"Teclado\\u001b[31m\\u202eevil\",\"Class\":\"System\",\"ClassGuid\":\"\",\"HardwareIds\":[],\"CompatibleIds\":[]}]}"
	devs, err := ParseDevices([]byte(js))
	if err != nil {
		t.Fatal(err)
	}
	n := devs[0].Name
	if strings.ContainsRune(n, 0x1b) || strings.ContainsRune(n, 0x202e) {
		t.Fatalf("nome com ESC/bidi não foi neutralizado: %q", n)
	}
}

func TestParseDevicesAceitaBOM(t *testing.T) {
	js := "\ufeff" + `{"count":1,"devices":[{"InstanceId":"HTREE\\ROOT\\0","Parent":"","Name":"Raiz","Class":"System","ClassGuid":"","HardwareIds":[],"CompatibleIds":[]}]}`
	if _, err := ParseDevices([]byte(js)); err != nil {
		t.Fatal(err)
	}
}

func TestArvoreCompletaDaMaquinaBase(t *testing.T) {
	tr, err := NewDeviceTree(machine(usbKeyboard("0001")))
	if err != nil {
		t.Fatal(err)
	}
	kb := `HID\VID_FFFF&PID_0001&MI_00\8&bbbb&0&0000`
	got := tr.Ancestors(kb)
	if len(got) != 6 || got[len(got)-1] != rootID {
		t.Fatalf("ancestrais do teclado = %v", got)
	}
}

func TestArvoreIncompletaRecusa(t *testing.T) {
	devs := machine(usbKeyboard("0001"))
	// remove o hub raiz: o teclado USB fica com pai desconhecido
	var cut []Device
	for _, d := range devs {
		if d.InstanceID != hub30 {
			cut = append(cut, d)
		}
	}
	if _, err := NewDeviceTree(cut); !errors.Is(err, ErrTreeIncomplete) {
		t.Fatalf("err = %v, queria ErrTreeIncomplete", err)
	}
}

func TestArvoreSemRaizOuComDuasRaizesRecusa(t *testing.T) {
	devs := baseMachine()
	devs[1].Parent = "" // o HAL vira uma segunda raiz
	if _, err := NewDeviceTree(devs); !errors.Is(err, ErrTreeIncomplete) {
		t.Fatalf("duas raízes: %v", err)
	}
	if _, err := NewDeviceTree(baseMachine()[1:]); !errors.Is(err, ErrTreeIncomplete) {
		t.Fatalf("sem a raiz: %v", err)
	}
}

func TestArvoreComCicloRecusa(t *testing.T) {
	devs := baseMachine()
	devs[1].Parent = `ACPI\PNP0303\4&2222&0` // HAL -> teclado -> HAL
	if _, err := NewDeviceTree(devs); !errors.Is(err, ErrTreeIncomplete) {
		t.Fatalf("ciclo: %v", err)
	}
}

func TestArvoreInstanciaRepetidaRecusa(t *testing.T) {
	devs := append(baseMachine(), baseMachine()[2])
	if _, err := NewDeviceTree(devs); !errors.Is(err, ErrTreeIncomplete) {
		t.Fatalf("duplicada: %v", err)
	}
}

func TestBarramentoPeloPrefixoDaInstancia(t *testing.T) {
	cases := map[string]string{
		`USB\VID_FFFF&PID_0001\1`:               "USB",
		`USBSTOR\DISK&VEN_FAKE\0`:               "USB",
		`ACPI\PNP0303\4&2222&0`:                 "PS/2",
		`ACPI\FFFF0001\4&3333&0`:                "ACPI",
		`BTHENUM\DEV_AABBCCDDEEFF\7&8`:          "Bluetooth",
		`BTHLE\DEV_AABBCCDDEEFF\7&8`:            "Bluetooth",
		`PCI\VEN_FFFF&DEV_0002\3&1`:             "PCI",
		`HID\FFFF0001&COL01\5&4444&0&0000`:      "HID",
		`SCSI\DISK&VEN_NVME&PROD_FAKE\5&5555&0`: "SCSI",
		`HTREE\ROOT\0`:                          "raiz",
		`SEMBARRA`:                              "outro",
	}
	for inst, want := range cases {
		if got := busOf(inst); got != want {
			t.Errorf("busOf(%q) = %q, queria %q", inst, got, want)
		}
	}
}

func TestVIDPID(t *testing.T) {
	if v, ok := vidpid(`USB\VID_ffff&PID_0001&REV_0100`); !ok || v != "FFFF:0001" {
		t.Fatalf("vidpid = %q %v", v, ok)
	}
	if _, ok := vidpid(`PCI\CC_0C03`); ok {
		t.Fatal("PCI\\CC_0C03 não tem VID/PID")
	}
	if _, ok := vidpid(`BTHENUM\VID&0001057e_PID&2009`); ok {
		t.Fatal("formato Bluetooth (VID&...) não é VID_xxxx&PID_xxxx")
	}
}

package usb

// Fixtures SINTÉTICAS (repositório público): VID_FFFF/PID_xxxx e nomes inventados, nunca IDs de máquina ou cliente real.

const (
	guidUSB       = "{36fc9e60-c465-11cf-8056-444553540000}"
	guidUSBDevice = "{88bae032-5a81-49f0-bc3d-a4ff138216d6}"
	guidHID       = "{745a17a0-74d3-11d0-b6fe-00a0c90f57da}"
	guidKeyboard  = "{4d36e96b-e325-11ce-bfc1-08002be10318}"
	guidMouse     = "{4d36e96f-e325-11ce-bfc1-08002be10318}"
	guidDisk      = "{4d36e967-e325-11ce-bfc1-08002be10318}"
	guidSCSI      = "{4d36e97b-e325-11ce-bfc1-08002be10318}"
	guidHDC       = "{4d36e96a-e325-11ce-bfc1-08002be10318}"
	guidBT        = "{e0cbf06c-cd8b-4647-bb8a-263b43f0f974}"
	guidSystem    = "{4d36e97d-e325-11ce-bfc1-08002be10318}"
)

var classGUIDs = map[string]string{
	"USB": guidUSB, "USBDevice": guidUSBDevice, "HIDClass": guidHID, "Keyboard": guidKeyboard, "Mouse": guidMouse,
	"DiskDrive": guidDisk, "SCSIAdapter": guidSCSI, "HDC": guidHDC, "Bluetooth": guidBT, "System": guidSystem,
}

type devOpt func(*Device)

func compat(ids ...string) devOpt { return func(d *Device) { d.CompatibleIDs = ids } }

// dev monta um dispositivo: instância, pai, nome, classe e IDs de hardware.
func dev(inst, parent, name, class string, hw []string, opts ...devOpt) Device {
	d := Device{InstanceID: inst, Parent: parent, Name: name, Class: class, ClassGUID: classGUIDs[class], HardwareIDs: hw}
	for _, o := range opts {
		o(&d)
	}
	return d
}

const (
	rootID = `HTREE\ROOT\0`
	halID  = `ACPI_HAL\PNP0C08\0`
	pciUSB = `PCI\VEN_FFFF&DEV_0001&SUBSYS_00000000&REV_01\3&11583659&0&A0`
	hub30  = `USB\ROOT_HUB30\4&1111&0&0`
)

// baseMachine é uma máquina sem nenhum USB externo: raiz, HAL, controlador USB, hub raiz, NVMe, SATA, teclado PS/2,
// touchpad I2C e o rádio Bluetooth (USB interno) com um teclado Bluetooth.
func baseMachine() []Device {
	return []Device{
		dev(rootID, "", "Raiz", "System", []string{"HTREE\\ROOT\\0"}),
		dev(halID, rootID, "ACPI x64", "System", []string{"ACPI_HAL\\PNP0C08"}),
		// teclado PS/2
		dev(`ACPI\PNP0303\4&2222&0`, halID, "Teclado PS/2 padrão", "Keyboard", []string{`ACPI\PNP0303`}),
		// touchpad I2C (HID sobre I2C)
		dev(`ACPI\FFFF0001\4&3333&0`, halID, "Controlador I2C", "System", []string{`ACPI\FFFF0001`}),
		dev(`HID\FFFF0001&COL01\5&4444&0&0000`, `ACPI\FFFF0001\4&3333&0`, "Touchpad I2C HID", "Mouse", []string{`HID\FFFF0001&COL01`}),
		// controlador USB (PCI) e hub raiz
		dev(pciUSB, halID, "Controlador xHCI", "USB", []string{`PCI\VEN_FFFF&DEV_0001&SUBSYS_00000000&REV_01`}, compat(`PCI\CC_0C0330`, `PCI\CC_0C03`)),
		dev(hub30, pciUSB, "Hub raiz USB 3", "USB", []string{`USB\ROOT_HUB30`}),
		// NVMe
		dev(`PCI\VEN_FFFF&DEV_0002&SUBSYS_00000000&REV_01\3&11583659&0&B0`, halID, "Controlador NVMe", "SCSIAdapter", []string{`PCI\VEN_FFFF&DEV_0002&SUBSYS_00000000&REV_01`}),
		dev(`SCSI\DISK&VEN_NVME&PROD_FAKE\5&5555&0&000000`, `PCI\VEN_FFFF&DEV_0002&SUBSYS_00000000&REV_01\3&11583659&0&B0`, "Disco NVMe", "DiskDrive", []string{`SCSI\DISKNVME____FAKE`}),
		// SATA
		dev(`PCI\VEN_FFFF&DEV_0003&SUBSYS_00000000&REV_01\3&11583659&0&B8`, halID, "Controlador SATA", "HDC", []string{`PCI\VEN_FFFF&DEV_0003&SUBSYS_00000000&REV_01`}),
		dev(`IDE\DISKFAKE_SATA\5&6666&0&0.0.0`, `PCI\VEN_FFFF&DEV_0003&SUBSYS_00000000&REV_01\3&11583659&0&B8`, "Disco SATA", "DiskDrive", []string{`IDE\DISKFAKE_SATA`}),
		// rádio Bluetooth (USB interno, composto) com o adaptador e um teclado Bluetooth
		dev(`USB\VID_FFFF&PID_0002\5&7777&0&1`, hub30, "Rádio Bluetooth", "USB", []string{`USB\VID_FFFF&PID_0002&REV_0001`, `USB\VID_FFFF&PID_0002`}, compat(`USB\COMPOSITE`)),
		dev(`USB\VID_FFFF&PID_0002&MI_00\6&7777&0&0000`, `USB\VID_FFFF&PID_0002\5&7777&0&1`, "Adaptador Bluetooth", "Bluetooth", []string{`USB\VID_FFFF&PID_0002&REV_0001&MI_00`, `USB\VID_FFFF&PID_0002&MI_00`}),
		dev(`BTHENUM\DEV_AABBCCDDEEFF\7&8888&0&BLUETOOTHDEVICE_AABBCCDDEEFF`, `USB\VID_FFFF&PID_0002&MI_00\6&7777&0&0000`, "Teclado Bluetooth", "Keyboard", []string{`BTHENUM\DEV_AABBCCDDEEFF`}),
	}
}

// usbKeyboard é um teclado USB COMPOSTO (dispositivo, duas interfaces, nó HID de teclado e nó HID de controle de mídia).
func usbKeyboard(pid string) []Device {
	base := `USB\VID_FFFF&PID_` + pid
	top := base + `\6&9999&0&1`
	mi0 := base + `&MI_00\7&aaaa&0&0000`
	mi1 := base + `&MI_01\7&aaaa&0&0001`
	return []Device{
		dev(top, hub30, "Teclado USB composto", "USB", []string{base + `&REV_0100`, base}, compat(`USB\COMPOSITE`)),
		dev(mi0, top, "Dispositivo de entrada USB", "HIDClass", []string{base + `&REV_0100&MI_00`, base + `&MI_00`}, compat(`USB\Class_03&SubClass_01&Prot_01`)),
		dev(`HID\VID_FFFF&PID_`+pid+`&MI_00\8&bbbb&0&0000`, mi0, "Teclado HID", "Keyboard", []string{`HID\VID_FFFF&PID_` + pid + `&REV_0100&MI_00`, `HID\VID_FFFF&PID_` + pid + `&MI_00`}, compat(`HID_DEVICE_SYSTEM_KEYBOARD`, `HID_DEVICE_UP:0001_U:0006`, `HID_DEVICE`)),
		dev(mi1, top, "Dispositivo de entrada USB (mídia)", "HIDClass", []string{base + `&REV_0100&MI_01`, base + `&MI_01`}, compat(`USB\Class_03`)),
		dev(`HID\VID_FFFF&PID_`+pid+`&MI_01\8&bbbb&0&0001`, mi1, "Controle de mídia HID", "HIDClass", []string{`HID\VID_FFFF&PID_` + pid + `&REV_0100&MI_01`, `HID\VID_FFFF&PID_` + pid + `&MI_01`}, compat(`HID_DEVICE_UP:000C_U:0001`, `HID_DEVICE`)),
	}
}

// usbPendrive é um pendrive (armazenamento pessoal): nó USB e o disco USBSTOR.
func usbPendrive(pid string) []Device {
	base := `USB\VID_FFFF&PID_` + pid
	top := base + `\0&pen&0&2`
	return []Device{
		dev(top, hub30, "Pendrive USB", "USB", []string{base + `&REV_0001`, base}, compat(`USB\Class_08&SubClass_06&Prot_50`)),
		dev(`USBSTOR\DISK&VEN_FAKE&PROD_PEN&REV_1.00\0&pen&0`, top, "Pendrive (disco)", "DiskDrive", []string{`USBSTOR\DISKFAKE_PEN`}),
	}
}

func machine(parts ...[]Device) []Device {
	out := baseMachine()
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}

func idx(devs []Device, name string) int {
	for i, d := range devs {
		if d.Name == name {
			return i
		}
	}
	return -1
}

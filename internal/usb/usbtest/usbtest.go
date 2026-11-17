// Package usbtest traz um Windows em memória (usb.System) e um Store em memória para os testes dos pacotes que usam o
// pacote usb (CLI e TUI). Nenhum teste que o use toca o registro, o Agendador ou o disco. Dados SINTÉTICOS (repositório
// público): VID_FFFF/PID_xxxx e nomes inventados.
package usbtest

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/PiluTech/toolkit/internal/usb"
)

// Sys é o System em memória. Os campos exportados são o estado; Log registra as mutações, na ordem.
type Sys struct {
	EditionID string
	Admin     bool
	Devs      []usb.Device
	DevsErr   error
	Tree      usb.RegTree
	Task      usb.TaskInfo
	Now       time.Time
	Log       []string
	FailOn    string // prefixo da operação que falha
	OnRead    func() // chamado a cada leitura do PnP (gancho de teste)
	StagedSHA string
}

// NewSys devolve um Windows Pro com administrador, um teclado e um pendrive USB.
func NewSys() *Sys {
	return &Sys{EditionID: usb.EditionPro, Admin: true, Devs: Devices(true), Tree: usb.RegTree{Entries: map[string]usb.RegValue{}},
		Now: time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC), StagedSHA: strings.Repeat("a", 64)}
}

func (s *Sys) op(name string) error {
	if s.FailOn != "" && strings.HasPrefix(name, s.FailOn) {
		return errors.New("falha simulada em " + name)
	}
	s.Log = append(s.Log, name)
	return nil
}

// Writes conta as escritas no registro.
func (s *Sys) Writes() int {
	n := 0
	for _, l := range s.Log {
		if strings.HasPrefix(l, "reg-") || strings.HasPrefix(l, "key-") {
			n++
		}
	}
	return n
}

// Edition implementa usb.System.
func (s *Sys) Edition(context.Context) (string, error) { return s.EditionID, nil }

// IsAdmin implementa usb.System.
func (s *Sys) IsAdmin(context.Context) bool { return s.Admin }

// Devices implementa usb.System.
func (s *Sys) Devices(context.Context) ([]usb.Device, error) {
	if s.OnRead != nil {
		s.OnRead()
	}
	return s.Devs, s.DevsErr
}

// ReadTree implementa usb.System.
func (s *Sys) ReadTree(context.Context) (usb.RegTree, error) {
	out := usb.RegTree{KeyExists: s.Tree.KeyExists, Entries: map[string]usb.RegValue{}, Subkeys: append([]string(nil), s.Tree.Subkeys...)}
	for k, v := range s.Tree.Entries {
		out.Entries[k] = v
	}
	return out, nil
}

// RegWrite implementa usb.System.
func (s *Sys) RegWrite(_ context.Context, rel string, v usb.RegValue) error {
	if err := s.op("reg-write:" + rel); err != nil {
		return err
	}
	s.Tree.KeyExists = true
	if sub, _, ok := strings.Cut(rel, `\`); ok && !has(s.Tree.Subkeys, sub) {
		s.Tree.Subkeys = append(s.Tree.Subkeys, sub)
		sort.Strings(s.Tree.Subkeys)
	}
	s.Tree.Entries[rel] = v
	return nil
}

// RegDelete implementa usb.System.
func (s *Sys) RegDelete(_ context.Context, rel string) error {
	if err := s.op("reg-delete:" + rel); err != nil {
		return err
	}
	delete(s.Tree.Entries, rel)
	return nil
}

// KeyDelete implementa usb.System.
func (s *Sys) KeyDelete(_ context.Context, rel string) error {
	if err := s.op("key-delete:" + rel); err != nil {
		return err
	}
	if rel == "" {
		s.Tree.KeyExists = false
		return nil
	}
	var keep []string
	for _, k := range s.Tree.Subkeys {
		if k != rel {
			keep = append(keep, k)
		}
	}
	s.Tree.Subkeys = keep
	return nil
}

func has(l []string, s string) bool {
	for _, x := range l {
		if x == s {
			return true
		}
	}
	return false
}

// ExportReg implementa usb.System (não grava arquivo: o MemStore não precisa).
func (s *Sys) ExportReg(context.Context, string) error { return s.op("export-reg") }

// ProtectDir implementa usb.System.
func (s *Sys) ProtectDir(context.Context, string) error { return s.op("protect-dir") }

// StageBinary implementa usb.System.
func (s *Sys) StageBinary(context.Context, string) (usb.StagedBinary, error) {
	if err := s.op("stage-binary"); err != nil {
		return usb.StagedBinary{}, err
	}
	return usb.StagedBinary{Path: usb.ProtectedDir + `\` + usb.RevertBinary, SourceSHA: s.StagedSHA, CopySHA: s.StagedSHA}, nil
}

// ArmTask implementa usb.System.
func (s *Sys) ArmTask(_ context.Context, spec usb.TaskSpec) error {
	if err := s.op("task-arm"); err != nil {
		return err
	}
	s.Task = usb.TaskInfo{Exists: true, Enabled: true, Exe: spec.Exe, Args: spec.Args, User: "SYSTEM", Next: s.Now.Add(time.Duration(spec.Minutes) * time.Minute)}
	return nil
}

// ReadTask implementa usb.System.
func (s *Sys) ReadTask(context.Context) (usb.TaskInfo, error) { return s.Task, nil }

// DeleteTask implementa usb.System.
func (s *Sys) DeleteTask(context.Context) error {
	if err := s.op("task-delete"); err != nil {
		return err
	}
	s.Task = usb.TaskInfo{}
	return nil
}

// MemStore é o usb.Store em memória (valida a estrutura como o FileStore: o snapshot passa pelo mesmo caminho de validação).
type MemStore struct {
	sn      *usb.Snapshot
	corrupt bool
	Fail    bool
	note    string
}

// SaveRevertResult e LoadRevertResult implementam usb.RevertLogger (o rastro da reversão automática, em memória).
func (m *MemStore) SaveRevertResult(note string) error { m.note = note; return nil }

// LoadRevertResult implementa usb.RevertLogger.
func (m *MemStore) LoadRevertResult() string { return m.note }

// Folder implementa usb.Store.
func (m *MemStore) Folder() string { return `C:\ProgramData\toolkit` }

// RegPath implementa usb.Store.
func (m *MemStore) RegPath() string { return `C:\ProgramData\toolkit\` + usb.RegFile }

// FileSHA256 implementa usb.Store.
func (m *MemStore) FileSHA256(string) (string, error) { return strings.Repeat("d", 64), nil }

// Load implementa usb.Store.
func (m *MemStore) Load() (*usb.Snapshot, error) {
	if m.corrupt {
		return nil, fmt.Errorf("%w: arquivo adulterado (simulado)", usb.ErrBackupInvalid)
	}
	if m.sn == nil {
		return nil, nil
	}
	cp := *m.sn
	return &cp, nil
}

// Save implementa usb.Store.
func (m *MemStore) Save(sn *usb.Snapshot) error {
	if m.Fail {
		return errors.New("falha simulada ao gravar o backup")
	}
	cp := *sn
	m.sn = &cp
	return nil
}

// Remove implementa usb.Store.
func (m *MemStore) Remove() error { m.sn, m.corrupt = nil, false; return nil }

// Corrupt simula arquivo adulterado: o Load passa a devolver ErrBackupInvalid.
func (m *MemStore) Corrupt() { m.corrupt = true }

var _ usb.Store = (*MemStore)(nil)
var _ usb.System = (*Sys)(nil)

// Devices devolve uma máquina sintética: teclado PS/2, controlador USB, hub raiz, NVMe, um teclado USB e, com pendrive,
// um pendrive USB.
func Devices(pendrive bool) []usb.Device {
	d := func(inst, parent, name, class, guid string, hw []string, cp ...string) usb.Device {
		return usb.Device{InstanceID: inst, Parent: parent, Name: name, Class: class, ClassGUID: guid, HardwareIDs: hw, CompatibleIDs: cp}
	}
	const (
		root = `HTREE\ROOT\0`
		pci  = `PCI\VEN_FFFF&DEV_0001&SUBSYS_00000000&REV_01\3&1&0&A0`
		hub  = `USB\ROOT_HUB30\4&1&0&0`
		kb   = `USB\VID_FFFF&PID_0001\6&9&0&1`
		nvme = `PCI\VEN_FFFF&DEV_0002&SUBSYS_00000000&REV_01\3&1&0&B0`
	)
	out := []usb.Device{
		d(root, "", "Raiz", "System", "{4d36e97d-e325-11ce-bfc1-08002be10318}", []string{`HTREE\ROOT\0`}),
		d(`ACPI\PNP0303\4&2&0`, root, "Teclado PS/2 padrão", "Keyboard", "{4d36e96b-e325-11ce-bfc1-08002be10318}", []string{`ACPI\PNP0303`}),
		d(pci, root, "Controlador xHCI", "USB", "{36fc9e60-c465-11cf-8056-444553540000}", []string{`PCI\VEN_FFFF&DEV_0001&SUBSYS_00000000&REV_01`}, `PCI\CC_0C0330`, `PCI\CC_0C03`),
		d(hub, pci, "Hub raiz USB 3", "USB", "{36fc9e60-c465-11cf-8056-444553540000}", []string{`USB\ROOT_HUB30`}),
		d(nvme, root, "Controlador NVMe", "SCSIAdapter", "{4d36e97b-e325-11ce-bfc1-08002be10318}", []string{`PCI\VEN_FFFF&DEV_0002&SUBSYS_00000000&REV_01`}),
		d(`SCSI\DISK&VEN_NVME\5&5&0&0`, nvme, "Disco NVMe", "DiskDrive", "{4d36e967-e325-11ce-bfc1-08002be10318}", []string{`SCSI\DISKNVME____FAKE`}),
		d(kb, hub, "Teclado USB", "USB", "{36fc9e60-c465-11cf-8056-444553540000}", []string{`USB\VID_FFFF&PID_0001&REV_0100`, `USB\VID_FFFF&PID_0001`}),
		d(`HID\VID_FFFF&PID_0001\8&b&0&0`, kb, "Teclado HID", "Keyboard", "{4d36e96b-e325-11ce-bfc1-08002be10318}", []string{`HID\VID_FFFF&PID_0001&REV_0100`, `HID\VID_FFFF&PID_0001`}),
	}
	if pendrive {
		pen := `USB\VID_FFFF&PID_0003\0&p&0&2`
		out = append(out,
			d(pen, hub, "Pendrive USB", "USB", "{36fc9e60-c465-11cf-8056-444553540000}", []string{`USB\VID_FFFF&PID_0003&REV_0001`, `USB\VID_FFFF&PID_0003`}),
			d(`USBSTOR\DISK&VEN_FAKE\0&p&0`, pen, "Pendrive (disco)", "DiskDrive", "{4d36e967-e325-11ce-bfc1-08002be10318}", []string{`USBSTOR\DISKFAKE_PEN`}))
	}
	return out
}

// KeyboardKey e PendriveKey são as instâncias dos dispositivos USB do Devices (a chave da seleção).
const (
	KeyboardKey = `USB\VID_FFFF&PID_0001\6&9&0&1`
	PendriveKey = `USB\VID_FFFF&PID_0003\0&p&0&2`
)

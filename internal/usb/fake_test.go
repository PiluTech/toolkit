package usb

import (
	"context"
	"errors"
	"os"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"
)

var t0 = time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)

// fakeSys é o Windows em memória: nenhum teste do pacote toca o registro, o Agendador ou o disco do sistema.
type fakeSys struct {
	t       *testing.T
	edition string
	admin   bool
	devs    []Device
	devsErr error
	tree    RegTree
	task    TaskInfo
	now     time.Time
	dir     string // pasta "protegida" (temporária) onde o Store grava
	log     []string
	failOn  string // prefixo da operação que falha
	hook    func(op string)

	protectCalls int
	staged       StagedBinary
	armSkew      func(*TaskInfo) // muda a tarefa DEPOIS de armada (simula conferência que falha)
	dropWrite    string          // RegWrite deste caminho "funciona" mas não grava (releitura pós-ação)
	taskReadErr  error
	treeReadErr  error
	failed       bool   // uma falha de failOn já aconteceu
	failAfter    string // depois da primeira falha, também falham as operações com este prefixo
}

func newFake(t *testing.T) *fakeSys {
	t.Helper()
	return &fakeSys{t: t, edition: EditionPro, admin: true, devs: machine(usbKeyboard("0001"), usbPendrive("0003")),
		tree: RegTree{Entries: map[string]RegValue{}}, now: t0, dir: t.TempDir(),
		staged: StagedBinary{Path: ProtectedDir + `\` + RevertBinary, SourceSHA: strings.Repeat("a", 64), CopySHA: strings.Repeat("a", 64)}}
}

func (f *fakeSys) op(name string) error {
	if f.hook != nil {
		f.hook(name)
	}
	if f.failOn != "" && strings.HasPrefix(name, f.failOn) {
		f.failed = true
		return errors.New("falha simulada em " + name)
	}
	if f.failed && f.failAfter != "" && strings.HasPrefix(name, f.failAfter) {
		return errors.New("falha simulada (depois da primeira) em " + name)
	}
	f.log = append(f.log, name)
	return nil
}

func (f *fakeSys) writes() int {
	return f.count("reg-write:") + f.count("reg-delete:") + f.count("key-delete:")
}
func (f *fakeSys) count(prefix string) int {
	n := 0
	for _, l := range f.log {
		if strings.HasPrefix(l, prefix) {
			n++
		}
	}
	return n
}
func (f *fakeSys) first(prefix string) int {
	for i, l := range f.log {
		if strings.HasPrefix(l, prefix) {
			return i
		}
	}
	return -1
}

func (f *fakeSys) Edition(context.Context) (string, error) { return f.edition, nil }
func (f *fakeSys) IsAdmin(context.Context) bool            { return f.admin }
func (f *fakeSys) Devices(context.Context) ([]Device, error) {
	return f.devs, f.devsErr
}

func (f *fakeSys) ReadTree(context.Context) (RegTree, error) {
	if f.treeReadErr != nil {
		return RegTree{}, f.treeReadErr
	}
	out := RegTree{KeyExists: f.tree.KeyExists, Entries: map[string]RegValue{}, Subkeys: append([]string(nil), f.tree.Subkeys...)}
	for k, v := range f.tree.Entries {
		out.Entries[k] = v
	}
	return out, nil
}

func (f *fakeSys) addSubkey(rel string) {
	for _, s := range f.tree.Subkeys {
		if s == rel {
			return
		}
	}
	f.tree.Subkeys = append(f.tree.Subkeys, rel)
	sort.Strings(f.tree.Subkeys)
}

func (f *fakeSys) RegWrite(_ context.Context, rel string, v RegValue) error {
	if err := checkWrite(rel, v); err != nil {
		f.t.Errorf("RegWrite fora da lista fechada: %v", err)
		return err
	}
	if err := f.op("reg-write:" + rel); err != nil {
		return err
	}
	if rel == f.dropWrite {
		return nil
	}
	f.tree.KeyExists = true
	if sub, _, ok := strings.Cut(rel, `\`); ok {
		f.addSubkey(sub)
	}
	f.tree.Entries[rel] = v
	return nil
}

func (f *fakeSys) RegDelete(_ context.Context, rel string) error {
	if err := f.op("reg-delete:" + rel); err != nil {
		return err
	}
	delete(f.tree.Entries, rel)
	return nil
}

func (f *fakeSys) KeyDelete(_ context.Context, rel string) error {
	if err := f.op("key-delete:" + rel); err != nil {
		return err
	}
	for p := range f.tree.Entries {
		if rel == "" || strings.HasPrefix(p, rel+`\`) {
			f.t.Errorf("KeyDelete(%q) com valor %q ainda dentro: apagaria política às cegas", rel, p)
		}
	}
	if rel == "" {
		if len(f.tree.Subkeys) > 0 {
			f.t.Errorf("KeyDelete(Restrictions) com subchaves %v", f.tree.Subkeys)
		}
		f.tree.KeyExists = false
		return nil
	}
	var keep []string
	for _, s := range f.tree.Subkeys {
		if s != rel {
			keep = append(keep, s)
		}
	}
	f.tree.Subkeys = keep
	return nil
}

func (f *fakeSys) ExportReg(_ context.Context, file string) error {
	if err := f.op("export-reg"); err != nil {
		return err
	}
	return os.WriteFile(file, []byte("Windows Registry Editor Version 5.00\r\n"), 0o600)
}

func (f *fakeSys) ProtectDir(context.Context, string) error {
	f.protectCalls++
	return f.op("protect-dir")
}

func (f *fakeSys) StageBinary(context.Context, string) (StagedBinary, error) {
	if err := f.op("stage-binary"); err != nil {
		return StagedBinary{}, err
	}
	return f.staged, nil
}

func (f *fakeSys) ArmTask(_ context.Context, spec TaskSpec) error {
	if err := f.op("task-arm:" + strconv.Itoa(spec.Minutes) + ":" + spec.Args); err != nil {
		return err
	}
	f.task = TaskInfo{Exists: true, Enabled: true, Exe: spec.Exe, Args: spec.Args, User: "SYSTEM", Next: f.now.Add(time.Duration(spec.Minutes) * time.Minute)}
	if f.armSkew != nil {
		f.armSkew(&f.task)
	}
	return nil
}

func (f *fakeSys) ReadTask(context.Context) (TaskInfo, error) { return f.task, f.taskReadErr }

func (f *fakeSys) DeleteTask(context.Context) error {
	if err := f.op("task-delete"); err != nil {
		return err
	}
	f.task = TaskInfo{}
	return nil
}

// fileStore devolve o FileStore na pasta temporária do fake.
func (f *fakeSys) fileStore() FileStore { return FileStore{Dir: f.dir} }

// spyStore envolve um Store e registra/falha as operações (a ordem backup -> tarefa -> chaves se confere pelo log do fake).
type spyStore struct {
	Store
	f       *fakeSys
	failSav bool
	failRem bool
}

func (s *spyStore) Save(sn *Snapshot) error {
	if err := s.f.op("backup-save"); err != nil {
		return err
	}
	if s.failSav {
		return errors.New("falha simulada ao gravar o backup")
	}
	return s.Store.Save(sn)
}

func (s *spyStore) Remove() error {
	if s.failRem {
		return errors.New("falha simulada ao remover o backup")
	}
	_ = s.f.op("backup-remove")
	return s.Store.Remove()
}

func newSpy(f *fakeSys) *spyStore { return &spyStore{Store: f.fileStore(), f: f} }

func applyOpts(f *fakeSys) ApplyOptions {
	return ApplyOptions{Prazo: PrazoPadrao, Now: f.now}
}

func mustApply(t *testing.T, f *fakeSys, st Store) *ApplyResult {
	t.Helper()
	res, err := Apply(context.Background(), f, st, applyOpts(f))
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	return res
}

func treeKeys(tr RegTree) []string {
	var out []string
	for k := range tr.Entries {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

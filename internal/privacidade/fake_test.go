package privacidade

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/PiluTech/toolkit/internal/exec"
)

// fakeSys é o Windows em memória: nenhum teste do pacote toca o registro, os serviços ou as tarefas reais.
type fakeSys struct {
	edition string
	admin   bool
	regs    map[string]RegValue
	svcs    map[string]*ServiceState
	tasks   map[string]*TaskState
	log     []string // mutações, na ordem
	failOn  string   // "operação" que falha (prefixo do log), p.ex. "reg-write:HKCU\\...\\Enabled"
	onWrite func()   // gancho chamado ANTES de cada mutação (testa a ordem backup -> mudança)
}

func newFake(edition string, admin bool) *fakeSys {
	return &fakeSys{edition: edition, admin: admin, regs: map[string]RegValue{}, svcs: map[string]*ServiceState{}, tasks: map[string]*TaskState{}}
}

func rkey(key, name string) string { return strings.ToUpper(key) + "|" + name }

func (f *fakeSys) mutate(op string, needsAdmin bool) error {
	if f.onWrite != nil {
		f.onWrite()
	}
	if needsAdmin && !f.admin {
		return exec.ErrNotElevated
	}
	if f.failOn != "" && strings.HasPrefix(op, f.failOn) {
		return errors.New("falha simulada em " + op)
	}
	f.log = append(f.log, op)
	return nil
}

func (f *fakeSys) Edition(context.Context) (string, error) { return f.edition, nil }
func (f *fakeSys) IsAdmin(context.Context) bool            { return f.admin }

func (f *fakeSys) RegRead(_ context.Context, key, name string) (RegValue, bool, error) {
	v, ok := f.regs[rkey(key, name)]
	return v, ok, nil
}

func (f *fakeSys) RegWrite(_ context.Context, key, name string, v RegValue) error {
	if err := f.mutate("reg-write:"+key+`\`+name, isHKLM(key)); err != nil {
		return err
	}
	f.regs[rkey(key, name)] = v
	return nil
}

func (f *fakeSys) RegDelete(_ context.Context, key, name string) error {
	if err := f.mutate("reg-delete:"+key+`\`+name, isHKLM(key)); err != nil {
		return err
	}
	delete(f.regs, rkey(key, name))
	return nil
}

func (f *fakeSys) ServiceRead(_ context.Context, name string) (ServiceState, error) {
	if s, ok := f.svcs[name]; ok {
		return *s, nil
	}
	return ServiceState{}, nil
}

func (f *fakeSys) ServiceConfigure(_ context.Context, name, startType string) error {
	if err := f.mutate("svc-config:"+name+"="+startType, true); err != nil {
		return err
	}
	f.svcs[name].StartType = startType
	return nil
}

func (f *fakeSys) ServiceStop(_ context.Context, name string) error {
	if err := f.mutate("svc-stop:"+name, true); err != nil {
		return err
	}
	f.svcs[name].Running = false
	return nil
}

func (f *fakeSys) ServiceStart(_ context.Context, name string) error {
	if err := f.mutate("svc-start:"+name, true); err != nil {
		return err
	}
	f.svcs[name].Running = true
	return nil
}

func (f *fakeSys) TaskRead(_ context.Context, path string) (TaskState, error) {
	if t, ok := f.tasks[path]; ok {
		return *t, nil
	}
	return TaskState{}, nil
}

func (f *fakeSys) TaskSetEnabled(_ context.Context, path string, enabled bool) error {
	if err := f.mutate(fmt.Sprintf("task:%s=%v", path, enabled), true); err != nil {
		return err
	}
	f.tasks[path].Enabled = enabled
	return nil
}

// withAllServicesAndTasks cria os serviços e tarefas do catálogo no estado ORIGINAL do Windows (ligados).
func (f *fakeSys) withAllServicesAndTasks() *fakeSys {
	for _, s := range Settings() {
		for _, sv := range s.Svcs {
			f.svcs[sv.Name] = &ServiceState{Exists: true, StartType: "automatic", Running: true}
		}
		for _, tk := range s.Tasks {
			f.tasks[tk.Path] = &TaskState{Exists: true, Enabled: true}
		}
	}
	return f
}

func (f *fakeSys) writes() []string {
	out := append([]string(nil), f.log...)
	return out
}

func (f *fakeSys) regValue(key, name string) (RegValue, bool) {
	v, ok := f.regs[rkey(key, name)]
	return v, ok
}

// memStore é o Store em memória.
type memStore struct {
	b        *Backup
	saves    int
	failSave bool
}

func (m *memStore) Load() (*Backup, error) {
	if m.b == nil {
		return nil, nil
	}
	cp := *m.b
	cp.Entries = map[string]Entry{}
	for k, v := range m.b.Entries {
		cp.Entries[k] = v
	}
	return &cp, nil
}

func (m *memStore) Save(b *Backup) error {
	if m.failSave {
		return errors.New("disco cheio")
	}
	m.saves++
	cp := *b
	cp.Entries = map[string]Entry{}
	for k, v := range b.Entries {
		cp.Entries[k] = v
	}
	m.b = &cp
	return nil
}

func (m *memStore) Remove() error { m.b = nil; return nil }
func (m *memStore) Path() string  { return `C:\ProgramData\toolkit\privacidade-backup.json` }

func sortedIDs(b *Backup) []string {
	var ids []string
	for id := range b.Entries {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

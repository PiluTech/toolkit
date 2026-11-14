package animacoes

import (
	"context"
	"errors"
	"strings"

	"github.com/PiluTech/toolkit/internal/privacidade"
)

const (
	sidA = "S-1-5-21-1111111111-2222222222-3333333333-1001"
	sidB = "S-1-5-21-1111111111-2222222222-3333333333-1002"
)

// fakeSys é o registro do usuário em memória: nenhum teste do pacote toca o Windows real.
type fakeSys struct {
	regs    map[string]RegValue
	log     []string // mutações, na ordem
	failOn  string   // prefixo da operação que falha
	sid     string
	onWrite func() // chamado ANTES de cada mutação (ordem backup -> mudança)
}

func newFake() *fakeSys {
	return &fakeSys{sid: sidA, regs: map[string]RegValue{}}
}

func rkey(key, name string) string { return strings.ToUpper(key) + "|" + name }

func (f *fakeSys) mutate(op string) error {
	if f.onWrite != nil {
		f.onWrite()
	}
	if f.failOn != "" && strings.HasPrefix(op, f.failOn) {
		return errors.New("falha simulada em " + op)
	}
	f.log = append(f.log, op)
	return nil
}

func (f *fakeSys) RegRead(_ context.Context, key, name string) (RegValue, bool, error) {
	v, ok := f.regs[rkey(key, name)]
	return v, ok, nil
}

func (f *fakeSys) RegWrite(_ context.Context, key, name string, v RegValue) error {
	if err := f.mutate("reg-write:" + key + `\` + name); err != nil {
		return err
	}
	f.regs[rkey(key, name)] = v
	return nil
}

func (f *fakeSys) RegDelete(_ context.Context, key, name string) error {
	if err := f.mutate("reg-delete:" + key + `\` + name); err != nil {
		return err
	}
	delete(f.regs, rkey(key, name))
	return nil
}

func (f *fakeSys) UserSID(context.Context) (string, error) { return f.sid, nil }

func (f *fakeSys) get(key, name string) (RegValue, bool) {
	v, ok := f.regs[rkey(key, name)]
	return v, ok
}

// memStore é o Store em memória.
type memStore struct {
	b        *privacidade.Backup
	saves    int
	failSave bool
	onSave   func()
}

func (m *memStore) Load() (*privacidade.Backup, error) {
	if m.b == nil {
		return nil, nil
	}
	cp := *m.b
	cp.Entries = map[string]privacidade.Entry{}
	for k, v := range m.b.Entries {
		cp.Entries[k] = v
	}
	return &cp, nil
}

func (m *memStore) Save(b *privacidade.Backup) error {
	if m.onSave != nil {
		m.onSave()
	}
	if m.failSave {
		return errors.New("disco cheio")
	}
	m.saves++
	cp := *b
	cp.Entries = map[string]privacidade.Entry{}
	for k, v := range b.Entries {
		cp.Entries[k] = v
	}
	m.b = &cp
	return nil
}

func (m *memStore) Remove() error { m.b = nil; return nil }
func (m *memStore) Path() string {
	return `C:\Users\x\AppData\Local\toolkit\otimizacao-animacoes-backup.json`
}

func str(s string) RegValue { return RegValue{Kind: privacidade.RegString, Str: s} }

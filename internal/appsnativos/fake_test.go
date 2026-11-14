package appsnativos

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/PiluTech/toolkit/internal/exec"
)

// fakeSys é o Windows em memória: nenhum teste do pacote remove app de verdade.
type fakeSys struct {
	admin       bool
	sid         string
	installed   []Package // visão do usuário atual
	others      []Package // pacotes só vistos com allUsers
	provisioned []Provisioned
	log         []string // mutações, na ordem
	lists       []string // leituras, na ordem
	failOn      string   // prefixo do log que falha
	onMutate    func()   // chamado ANTES de cada mutação
}

func newFake(admin bool) *fakeSys {
	return &fakeSys{admin: admin, sid: "S-1-5-21-1-2-3-1001"}
}

func pkg(name string) Package {
	return Package{Name: name, PackageFullName: name + "_1.2.3.0_x64__8wekyb3d8bbwe", PackageFamilyName: name + "_8wekyb3d8bbwe",
		Version: "1.2.3.0", Publisher: "CN=Microsoft Corporation, O=Microsoft Corporation, L=Redmond, S=Washington, C=US", SignatureKind: "Store"}
}

func prov(name string) Provisioned {
	return Provisioned{DisplayName: name, PackageName: name + "_1.2.3.0_neutral_~_8wekyb3d8bbwe", Version: "1.2.3.0"}
}

func (f *fakeSys) IsAdmin(context.Context) bool { return f.admin }

func (f *fakeSys) UserSID(context.Context) (string, error) { return f.sid, nil }

func (f *fakeSys) ListInstalled(_ context.Context, all bool) ([]Package, error) {
	f.lists = append(f.lists, fmt.Sprintf("installed:all=%v", all))
	if all && !f.admin {
		return nil, exec.ErrNotElevated
	}
	out := append([]Package(nil), f.installed...)
	if all {
		out = append(out, f.others...)
	}
	return out, nil
}

func (f *fakeSys) ListProvisioned(context.Context) ([]Provisioned, error) {
	f.lists = append(f.lists, "provisioned")
	if !f.admin {
		return nil, exec.ErrNotElevated
	}
	return append([]Provisioned(nil), f.provisioned...), nil
}

func (f *fakeSys) mutate(op string, needsAdmin bool) error {
	if f.onMutate != nil {
		f.onMutate()
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

func (f *fakeSys) RemoveForUser(_ context.Context, full string) error {
	return f.mutate("remove-user:"+full, false)
}

func (f *fakeSys) RemoveAllUsers(_ context.Context, full string) error {
	return f.mutate("remove-all:"+full, true)
}

func (f *fakeSys) RemoveProvisioned(_ context.Context, name string) error {
	return f.mutate("remove-prov:"+name, true)
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

func (m *memStore) Path() string { return `C:\ProgramData\toolkit\apps-nativos-backup.json` }

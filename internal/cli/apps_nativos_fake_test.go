package cli

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/PiluTech/toolkit/internal/appsnativos"
	"github.com/PiluTech/toolkit/internal/exec"
	"github.com/PiluTech/toolkit/internal/exec/exectest"
)

// natSys é o Windows em memória: nenhum teste remove app de verdade.
type natSys struct {
	admin       bool
	installed   []appsnativos.Package
	others      []appsnativos.Package
	provisioned []appsnativos.Provisioned
	removes     []string
	// onList (opcional) roda no início de cada ListInstalled, com o número da chamada: simula outro processo.
	lists  int
	onList func(call int)
}

func newNatSys(admin bool, names ...string) *natSys {
	s := &natSys{admin: admin}
	for _, n := range names {
		s.installed = append(s.installed, natPkg(n))
	}
	return s
}

func natPkg(name string) appsnativos.Package {
	return appsnativos.Package{Name: name, PackageFullName: name + "_1.2.3.0_x64__8wekyb3d8bbwe", PackageFamilyName: name + "_8wekyb3d8bbwe",
		Version: "1.2.3.0", Publisher: "CN=Microsoft Corporation", SignatureKind: "Store"}
}

func (s *natSys) IsAdmin(context.Context) bool            { return s.admin }
func (s *natSys) UserSID(context.Context) (string, error) { return "S-1-5-21-1-2-3-1001", nil }
func (s *natSys) ListInstalled(_ context.Context, all bool) ([]appsnativos.Package, error) {
	s.lists++
	if s.onList != nil {
		s.onList(s.lists)
	}
	if all && !s.admin {
		return nil, exec.ErrNotElevated
	}
	out := append([]appsnativos.Package(nil), s.installed...)
	if all {
		out = append(out, s.others...)
	}
	return out, nil
}
func (s *natSys) ListProvisioned(context.Context) ([]appsnativos.Provisioned, error) {
	if !s.admin {
		return nil, exec.ErrNotElevated
	}
	return append([]appsnativos.Provisioned(nil), s.provisioned...), nil
}
func (s *natSys) RemoveForUser(_ context.Context, full string) error {
	s.removes = append(s.removes, "user:"+full)
	return nil
}
func (s *natSys) RemoveAllUsers(_ context.Context, full string) error {
	if !s.admin {
		return exec.ErrNotElevated
	}
	s.removes = append(s.removes, "all:"+full)
	return nil
}
func (s *natSys) RemoveProvisioned(_ context.Context, name string) error {
	if !s.admin {
		return exec.ErrNotElevated
	}
	s.removes = append(s.removes, "prov:"+name)
	return nil
}

// natSysNoSID não informa o SID (o backup por usuário não pode ser separado).
type natSysNoSID struct{ *natSys }

func (natSysNoSID) UserSID() {}

type natStore struct{ b *appsnativos.Backup }

func (m *natStore) Load() (*appsnativos.Backup, error) {
	if m.b == nil {
		return nil, nil
	}
	cp := *m.b
	cp.Entries = map[string]appsnativos.Entry{}
	for k, v := range m.b.Entries {
		cp.Entries[k] = v
	}
	return &cp, nil
}
func (m *natStore) Save(b *appsnativos.Backup) error {
	cp := *b
	cp.Entries = map[string]appsnativos.Entry{}
	for k, v := range b.Entries {
		cp.Entries[k] = v
	}
	m.b = &cp
	return nil
}
func (m *natStore) Path() string {
	return `C:\ProgramData\toolkit\apps-nativos-S-1-5-21-1-2-3-1001.json`
}

func natDeps(t *testing.T, sys appsnativos.System, st *natStore, goos string, tty bool, input string) Deps {
	t.Helper()
	d := Deps{GOOS: goos, EmbeddedYML: []byte(embedded), NewRunner: func() exec.Runner { return exectest.New(t) },
		UpdateCheck: UpdateCheckDeps{Env: func(string) string { return "" }},
		Stdin:       strings.NewReader(input), Interactive: func() bool { return tty },
		AppsNat: AppsNatDeps{System: sys, Now: func() time.Time { return time.Date(2026, 11, 14, 10, 0, 0, 0, time.UTC) }}}
	if st != nil {
		d.AppsNat.Store = st
	}
	return d
}

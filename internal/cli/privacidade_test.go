package cli

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/PiluTech/toolkit/internal/exec"
	"github.com/PiluTech/toolkit/internal/exec/exectest"
	"github.com/PiluTech/toolkit/internal/privacidade"
)

// privSys é o Windows em memória: nenhum teste toca o registro, os serviços ou as tarefas reais.
type privSys struct {
	admin  bool
	edit   string
	regs   map[string]privacidade.RegValue
	svcs   map[string]*privacidade.ServiceState
	tasks  map[string]*privacidade.TaskState
	writes int
	// gate bloqueia a PRÓXIMA leitura de edição (armed) até o teste liberar: simula um plano lento/cancelado.
	armed   atomic.Bool
	entered chan struct{}
	gate    chan struct{}
}

func newPrivSys(admin bool) *privSys {
	s := &privSys{admin: admin, edit: privacidade.EditionPro, regs: map[string]privacidade.RegValue{},
		svcs: map[string]*privacidade.ServiceState{}, tasks: map[string]*privacidade.TaskState{}}
	for _, st := range privacidade.Settings() {
		for _, sv := range st.Svcs {
			s.svcs[sv.Name] = &privacidade.ServiceState{Exists: true, StartType: "automatic", Running: true}
		}
		for _, tk := range st.Tasks {
			s.tasks[tk.Path] = &privacidade.TaskState{Exists: true, Enabled: true}
		}
	}
	return s
}

func pk(key, name string) string { return strings.ToUpper(key) + "|" + name }

func (s *privSys) Edition(context.Context) (string, error) {
	if s.armed.CompareAndSwap(true, false) {
		s.entered <- struct{}{}
		<-s.gate
	}
	return s.edit, nil
}
func (s *privSys) IsAdmin(context.Context) bool { return s.admin }
func (s *privSys) RegRead(_ context.Context, k, n string) (privacidade.RegValue, bool, error) {
	v, ok := s.regs[pk(k, n)]
	return v, ok, nil
}
func (s *privSys) RegWrite(_ context.Context, k, n string, v privacidade.RegValue) error {
	if strings.HasPrefix(k, `HKLM\`) && !s.admin {
		return exec.ErrNotElevated
	}
	s.writes++
	s.regs[pk(k, n)] = v
	return nil
}
func (s *privSys) RegDelete(_ context.Context, k, n string) error {
	s.writes++
	delete(s.regs, pk(k, n))
	return nil
}
func (s *privSys) ServiceRead(_ context.Context, n string) (privacidade.ServiceState, error) {
	if v, ok := s.svcs[n]; ok {
		return *v, nil
	}
	return privacidade.ServiceState{}, nil
}
func (s *privSys) ServiceConfigure(_ context.Context, n, t string) error {
	s.writes++
	s.svcs[n].StartType = t
	return nil
}
func (s *privSys) ServiceStop(_ context.Context, n string) error {
	s.writes++
	s.svcs[n].Running = false
	return nil
}
func (s *privSys) ServiceStart(_ context.Context, n string) error {
	s.writes++
	s.svcs[n].Running = true
	return nil
}
func (s *privSys) TaskRead(_ context.Context, p string) (privacidade.TaskState, error) {
	if v, ok := s.tasks[p]; ok {
		return *v, nil
	}
	return privacidade.TaskState{}, nil
}
func (s *privSys) TaskSetEnabled(_ context.Context, p string, e bool) error {
	s.writes++
	s.tasks[p].Enabled = e
	return nil
}

type privStore struct{ b *privacidade.Backup }

func (m *privStore) Load() (*privacidade.Backup, error) {
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
func (m *privStore) Save(b *privacidade.Backup) error { m.b = b; return nil }
func (m *privStore) Remove() error                    { m.b = nil; return nil }
func (m *privStore) Path() string                     { return `C:\ProgramData\toolkit\privacidade-backup.json` }

func privDeps(t *testing.T, sys *privSys, st *privStore, goos string, tty bool, input string) Deps {
	t.Helper()
	return Deps{GOOS: goos, EmbeddedYML: []byte(embedded), NewRunner: func() exec.Runner { return exectest.New(t) },
		UpdateCheck: UpdateCheckDeps{Env: func(string) string { return "" }},
		Stdin:       strings.NewReader(input), Interactive: func() bool { return tty },
		Priv: PrivDeps{System: sys, Store: st, Now: func() time.Time { return time.Date(2026, 11, 14, 10, 0, 0, 0, time.UTC) }}}
}

func runPriv(t *testing.T, deps Deps, args ...string) (string, error) {
	t.Helper()
	var o, e bytes.Buffer
	cmd := NewRootCmdWith(&o, &e, deps)
	cmd.SetArgs(append([]string{"privacidade", "--no-update-check"}, args...))
	err := cmd.ExecuteContext(context.Background())
	return o.String(), err
}

func TestPrivacidadeSemArgsSoMostra(t *testing.T) {
	sys, st := newPrivSys(false), &privStore{}
	out, err := runPriv(t, privDeps(t, sys, st, "windows", false, ""))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"perfil basico", "perfil recomendado", "->", "exige administrador", "toolkit privacidade aplicar"} {
		if !strings.Contains(out, want) {
			t.Errorf("faltou %q na saída:\n%s", want, out)
		}
	}
	if sys.writes != 0 || st.b != nil {
		t.Errorf("sem argumentos nada pode mudar: writes=%d backup=%v", sys.writes, st.b)
	}
}

func TestPrivacidadeForaDoWindows(t *testing.T) {
	for _, args := range [][]string{{}, {"aplicar", "--perfil", "basico", "--yes"}, {"desfazer", "--yes"}} {
		sys, st := newPrivSys(true), &privStore{}
		_, err := runPriv(t, privDeps(t, sys, st, "linux", true, ""), args...)
		if err == nil || !strings.Contains(err.Error(), "só existe no Windows") {
			t.Errorf("%v: erro = %v", args, err)
		}
		if sys.writes != 0 || st.b != nil {
			t.Errorf("%v: nada pode mudar", args)
		}
	}
}

func TestPrivacidadeAplicarPerfilObrigatorioEValido(t *testing.T) {
	deps := privDeps(t, newPrivSys(true), &privStore{}, "windows", true, "")
	if _, err := runPriv(t, deps, "aplicar"); err == nil || !strings.Contains(err.Error(), "informe o perfil") {
		t.Errorf("sem perfil: %v", err)
	}
	if _, err := runPriv(t, deps, "aplicar", "--perfil", "xis"); err == nil || !strings.Contains(err.Error(), "desconhecido") {
		t.Errorf("perfil ruim: %v", err)
	}
}

func TestPrivacidadeDryRunMostraPlanoENaoMuda(t *testing.T) {
	sys, st := newPrivSys(true), &privStore{}
	out, err := runPriv(t, privDeps(t, sys, st, "windows", false, ""), "aplicar", "--perfil", "recomendado", "--dry-run")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "não definido -> 1") || !strings.Contains(out, "desfazer:") || !strings.Contains(out, "--dry-run: nada foi alterado") {
		t.Errorf("plano incompleto:\n%s", out)
	}
	if sys.writes != 0 || st.b != nil {
		t.Errorf("--dry-run mudou algo: writes=%d", sys.writes)
	}
}

func TestPrivacidadeSemYesESemTerminalErraAntesDeMudar(t *testing.T) {
	sys, st := newPrivSys(true), &privStore{}
	out, err := runPriv(t, privDeps(t, sys, st, "windows", false, ""), "aplicar", "--perfil", "basico")
	if err == nil || !strings.Contains(err.Error(), "--yes") {
		t.Fatalf("erro = %v", err)
	}
	if !strings.Contains(out, "->") {
		t.Errorf("o plano deve aparecer antes do erro:\n%s", out)
	}
	if sys.writes != 0 || st.b != nil {
		t.Error("nada pode mudar")
	}
}

func TestPrivacidadeConfirmacaoNegativaCancela(t *testing.T) {
	for _, in := range []string{"n\n", "\n", "talvez\n"} {
		sys, st := newPrivSys(true), &privStore{}
		out, err := runPriv(t, privDeps(t, sys, st, "windows", true, in), "aplicar", "--perfil", "basico")
		if err != nil {
			t.Fatalf("%q: %v", in, err)
		}
		if !strings.Contains(out, "[s/N]") || !strings.Contains(out, "Cancelado") {
			t.Errorf("%q: saída:\n%s", in, out)
		}
		if sys.writes != 0 || st.b != nil {
			t.Errorf("%q: nada pode mudar", in)
		}
	}
}

func TestPrivacidadeAplicarEDesfazerIdaEVolta(t *testing.T) {
	sys, st := newPrivSys(true), &privStore{}
	sys.regs[pk(`HKCU\Software\Microsoft\Windows\CurrentVersion\AdvertisingInfo`, "Enabled")] = privacidade.DWORD(1)
	orig := len(sys.regs)
	out, err := runPriv(t, privDeps(t, sys, st, "windows", true, "s\n"), "aplicar", "--perfil", "recomendado")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Aplicado:") || !strings.Contains(out, "privacidade-backup.json") {
		t.Errorf("saída:\n%s", out)
	}
	if st.b == nil || sys.writes == 0 {
		t.Fatal("deveria ter backup e mudanças")
	}
	if sys.svcs["DiagTrack"].StartType != "disabled" || sys.svcs["DiagTrack"].Running {
		t.Error("DiagTrack deveria estar desativado e parado")
	}
	// de novo: nada a fazer
	if out, err := runPriv(t, privDeps(t, sys, st, "windows", false, ""), "aplicar", "--perfil", "recomendado", "--yes"); err != nil || !strings.Contains(out, "Nada a fazer") {
		t.Errorf("segunda vez: %v\n%s", err, out)
	}

	dry, err := runPriv(t, privDeps(t, sys, st, "windows", false, ""), "desfazer", "--dry-run")
	if err != nil || !strings.Contains(dry, "O que será restaurado") || st.b == nil {
		t.Fatalf("desfazer --dry-run: %v\n%s", err, dry)
	}
	out, err = runPriv(t, privDeps(t, sys, st, "windows", false, ""), "desfazer", "--yes")
	if err != nil || !strings.Contains(out, "Restaurado:") {
		t.Fatalf("desfazer: %v\n%s", err, out)
	}
	if st.b != nil {
		t.Error("o backup deve sumir depois de desfazer tudo")
	}
	if s := sys.svcs["DiagTrack"]; s.StartType != "automatic" || !s.Running {
		t.Errorf("DiagTrack não voltou: %+v", *s)
	}
	if len(sys.regs) != orig {
		t.Errorf("registro não voltou ao original: %d != %d", len(sys.regs), orig)
	}
	if v := sys.regs[pk(`HKCU\Software\Microsoft\Windows\CurrentVersion\AdvertisingInfo`, "Enabled")]; v.DWORD != 1 {
		t.Errorf("valor original do usuário perdido: %+v", v)
	}
}

func TestPrivacidadeSemAdminRecusaAntesDeMudar(t *testing.T) {
	sys, st := newPrivSys(false), &privStore{}
	out, err := runPriv(t, privDeps(t, sys, st, "windows", true, "s\n"), "aplicar", "--perfil", "recomendado")
	if err == nil || !strings.Contains(err.Error(), "como administrador") || !strings.Contains(err.Error(), "--somente-usuario") {
		t.Fatalf("erro = %v", err)
	}
	if strings.Contains(out, "[s/N]") {
		t.Error("não deve perguntar quando vai recusar")
	}
	if sys.writes != 0 || st.b != nil {
		t.Error("nada pode mudar")
	}
	// o básico (só usuário) funciona sem administrador
	if _, err := runPriv(t, privDeps(t, sys, st, "windows", false, ""), "aplicar", "--perfil", "basico", "--yes"); err != nil {
		t.Errorf("básico sem admin: %v", err)
	}
}

func TestPrivacidadeSomenteUsuarioIgnoraItensDeAdmin(t *testing.T) {
	sys, st := newPrivSys(false), &privStore{}
	out, err := runPriv(t, privDeps(t, sys, st, "windows", false, ""), "aplicar", "--perfil", "recomendado", "--somente-usuario", "--yes")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if !strings.Contains(out, "--somente-usuario") || strings.Contains(out, "DiagTrack") {
		t.Errorf("saída:\n%s", out)
	}
	for k := range sys.regs {
		if strings.HasPrefix(k, `HKLM\`) {
			t.Errorf("escreveu em HKLM sem administrador: %s", k)
		}
	}
	if !sys.svcs["DiagTrack"].Running {
		t.Error("serviço não pode mudar")
	}
}

func TestPrivacidadeDesfazerSemBackup(t *testing.T) {
	sys, st := newPrivSys(true), &privStore{}
	out, err := runPriv(t, privDeps(t, sys, st, "windows", false, ""), "desfazer", "--yes")
	if !errors.Is(err, privacidade.ErrNoBackup) || !strings.Contains(out, "Não há backup") {
		t.Errorf("err=%v out=%s", err, out)
	}
}

func TestPrivacidadeDesfazerSemAdminRecusa(t *testing.T) {
	sys, st := newPrivSys(true), &privStore{}
	if _, err := runPriv(t, privDeps(t, sys, st, "windows", false, ""), "aplicar", "--perfil", "recomendado", "--yes"); err != nil {
		t.Fatal(err)
	}
	sys.admin = false
	w := sys.writes
	_, err := runPriv(t, privDeps(t, sys, st, "windows", false, ""), "desfazer", "--yes")
	if err == nil || !strings.Contains(err.Error(), "administrador") || sys.writes != w || st.b == nil {
		t.Errorf("err=%v writes=%d backup=%v", err, sys.writes-w, st.b)
	}
}

func TestPrivacidadeDesfazerSemYesESemTerminal(t *testing.T) {
	sys, st := newPrivSys(true), &privStore{}
	if _, err := runPriv(t, privDeps(t, sys, st, "windows", false, ""), "aplicar", "--perfil", "basico", "--yes"); err != nil {
		t.Fatal(err)
	}
	w := sys.writes
	_, err := runPriv(t, privDeps(t, sys, st, "windows", false, ""), "desfazer")
	if err == nil || !strings.Contains(err.Error(), "--yes") || sys.writes != w {
		t.Errorf("err=%v", err)
	}
}

func TestPrivacidadeSaidaNaoTrazControle(t *testing.T) {
	sys, st := newPrivSys(true), &privStore{}
	sys.regs[pk(`HKCU\Software\Microsoft\Windows\CurrentVersion\AdvertisingInfo`, "Enabled")] = privacidade.RegValue{Kind: privacidade.RegString, Str: "\x1b[31mvermelho\x07"}
	out, err := runPriv(t, privDeps(t, sys, st, "windows", false, ""), "aplicar", "--perfil", "basico", "--dry-run")
	if err != nil {
		t.Fatal(err)
	}
	if strings.ContainsAny(out, "\x1b\x07") {
		t.Errorf("a saída tem caractere de controle: %q", out)
	}
}

func TestUndoDryRunSemPendenciasPreservaOBackup(t *testing.T) {
	sys := newPrivSys(false)
	key := `HKCU\Software\Microsoft\Windows\CurrentVersion\AdvertisingInfo`
	e := privacidade.Entry{ID: "reg:" + key + "|Enabled", Kind: "reg", Key: key, Name: "Enabled"} // não existia, e continua não existindo
	st := &privStore{b: &privacidade.Backup{Version: 1, Entries: map[string]privacidade.Entry{e.ID: e}}}
	if out, err := runPriv(t, privDeps(t, sys, st, "windows", false, ""), "desfazer", "--dry-run"); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if st.b == nil || len(st.b.Entries) != 1 {
		t.Fatal("desfazer --dry-run apagou o backup")
	}
	if _, err := runPriv(t, privDeps(t, sys, st, "windows", false, ""), "desfazer"); err != nil || st.b != nil {
		t.Errorf("sem --dry-run o backup sem pendências deve ser removido: %v %v", err, st.b)
	}
}

func TestPrivacidadeSemSIDNaoUsaBackupCompartilhado(t *testing.T) {
	deps := privDeps(t, newPrivSys(true), nil, "windows", false, "")
	deps.Priv.Store = nil // store real: o backup é por SID e o System de teste não informa SID
	out, err := runPriv(t, deps, "desfazer")
	if err == nil || !strings.Contains(err.Error(), "SID") {
		t.Fatalf("err=%v\n%s", err, out)
	}
}

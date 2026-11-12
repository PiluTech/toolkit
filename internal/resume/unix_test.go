package resume_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/PiluTech/toolkit/internal/exec/exectest"
	"github.com/PiluTech/toolkit/internal/platform"
	"github.com/PiluTech/toolkit/internal/resume"
	"github.com/PiluTech/toolkit/internal/resume/resumetest"
)

const unitPath = "/etc/systemd/system/toolkit-resume.service"

func newSystemd(r *exectest.FakeRunner, root bool) resume.Systemd {
	return resume.Systemd{Runner: r, IsRoot: root, BinDir: "/usr/local/bin", StatePath: "/var/lib/toolkit/state.json"}
}

func TestSystemdUnitFile(t *testing.T) {
	s := newSystemd(nil, true)
	got, err := s.UnitFile(resume.Spec{Args: []string{"resume", "--yes"}, LogPath: "/var/lib/toolkit/logs/resume.log"})
	if err != nil {
		t.Fatal(err)
	}
	want := `[Unit]
Description=toolkit: continuar a receita depois do reboot
Wants=network-online.target
After=network-online.target
ConditionPathExists=/var/lib/toolkit/state.json

[Service]
Type=oneshot
ProtectHome=true
ExecStart=/usr/local/bin/toolkit resume --yes --no-tui
StandardOutput=append:/var/lib/toolkit/logs/resume.log
StandardError=append:/var/lib/toolkit/logs/resume.log

[Install]
WantedBy=multi-user.target
`
	if got != want {
		t.Fatalf("unit:\n%s", got)
	}
	// --no-tui não duplica e o log é opcional
	got, _ = s.UnitFile(resume.Spec{Args: []string{"resume", "--no-tui"}})
	if strings.Count(got, "--no-tui") != 1 || strings.Contains(got, "StandardOutput") {
		t.Fatalf("unit:\n%s", got)
	}
}

func TestSystemdUnitFileRejects(t *testing.T) {
	s := newSystemd(nil, true)
	if _, err := s.UnitFile(resume.Spec{Args: []string{"resume", "x;reboot"}}); err == nil {
		t.Error("arg inválido")
	}
	if _, err := s.UnitFile(resume.Spec{Args: []string{"resume"}, LogPath: "/tmp/a b.log"}); err == nil {
		t.Error("espaço no log")
	}
	if _, err := s.UnitFile(resume.Spec{Args: []string{"resume"}, LogPath: "/tmp/%n.log"}); err == nil {
		t.Error("especificador % no log")
	}
	bad := s
	bad.BinDir = "/opt/my tools"
	if _, err := bad.UnitFile(resume.Spec{Args: []string{"resume"}}); err == nil {
		t.Error("espaço no BinDir")
	}
}

var fakeBin = []byte("binario-do-toolkit")

func fakeRead(string) ([]byte, error) { return fakeBin, nil }

func binSum() string { h := sha256.Sum256(fakeBin); return hex.EncodeToString(h[:]) }

// scriptState: state e diretórios acima, até /var/lib, de root e sem escrita para outros.
func scriptState(r *exectest.FakeRunner) {
	r.On("stat -c \"%u %a\" -- /var/lib/toolkit/state.json", exectest.Exit(0, "0 600\n"))
	r.On("stat -c \"%u %a\" -- /var/lib/toolkit", exectest.Exit(0, "0 755\n"))
	r.On("stat -c \"%u %a\" -- /var/lib", exectest.Exit(0, "0 755\n"))
}

func newSystemdRead(r *exectest.FakeRunner, root bool) resume.Systemd {
	s := newSystemd(r, root)
	s.ReadFile = fakeRead
	return s
}

func TestSystemdRegisterAsRootAndNonRoot(t *testing.T) {
	for _, root := range []bool{true, false} {
		r := exectest.New(t)
		s := newSystemdRead(r, root)
		script := func(line string) {
			if root {
				r.On(line, exectest.Exit(0, ""))
			} else {
				r.OnElevated(line, exectest.Exit(0, ""))
			}
		}
		scriptState(r)
		script("install -d -m 0755 -o root -g root /usr/local/bin")
		script("tee -- /usr/local/bin/toolkit.new")
		script("chown root:root -- /usr/local/bin/toolkit.new")
		script("chmod 0755 -- /usr/local/bin/toolkit.new")
		r.On("sha256sum -- /usr/local/bin/toolkit.new", exectest.Exit(0, binSum()+"  /usr/local/bin/toolkit.new\n"))
		script("mv -f -- /usr/local/bin/toolkit.new /usr/local/bin/toolkit")
		script("tee -- " + unitPath)
		script("chmod 0644 -- " + unitPath)
		script("systemctl daemon-reload")
		r.On("sha256sum -- /usr/local/bin/toolkit", exectest.Exit(0, binSum()+"  /usr/local/bin/toolkit\n"))
		script("systemctl enable -- toolkit-resume.service")
		err := s.Register(context.Background(), resume.Spec{Executable: "/home/ana/dl/toolkit", Args: []string{"resume"}, LogPath: "/var/lib/toolkit/logs/r.log"})
		if err != nil {
			t.Fatalf("root=%v: %v", root, err)
		}
		calls := r.Calls()
		var names []string
		for _, c := range calls {
			names = append(names, c.Name)
			if c.ReadOnly && c.Elevate {
				t.Fatalf("leitura elevada: %v", c)
			}
			if !c.ReadOnly && c.Elevate == root {
				t.Fatalf("root=%v: Elevate errado em %v", root, c)
			}
		}
		// ordem: confere o state ANTES de tocar em qualquer coisa, e confere o destino antes do enable
		want := "stat stat stat install tee chown chmod sha256sum mv tee chmod systemctl sha256sum systemctl"
		if strings.Join(names, " ") != want {
			t.Fatalf("ordem:\n%s\n%s", strings.Join(names, " "), want)
		}
		// o binário e a unit vão por stdin do tee (uma só leitura do original), nunca por argumento
		if calls[4].Stdin == nil || calls[9].Stdin == nil {
			t.Fatal("tee sem stdin")
		}
		got, _ := io.ReadAll(calls[4].Stdin)
		if string(got) != string(fakeBin) {
			t.Fatalf("o tee deve receber os bytes lidos uma vez: %q", got)
		}
		// enable, nunca start
		last := calls[len(calls)-1]
		if last.Name != "systemctl" || last.Args[0] != "enable" {
			t.Fatalf("o último passo deve ser o enable: %v", last)
		}
	}
}

// O destino com outro conteúdo (trocado entre a cópia e o enable, ou cópia corrompida) impede o enable.
func TestSystemdRegisterRefusesHashMismatch(t *testing.T) {
	for _, stage := range []string{"copia", "final"} {
		r := exectest.New(t)
		s := newSystemdRead(r, true)
		scriptState(r)
		for _, l := range []string{
			"install -d -m 0755 -o root -g root /usr/local/bin", "tee -- /usr/local/bin/toolkit.new",
			"chown root:root -- /usr/local/bin/toolkit.new", "chmod 0755 -- /usr/local/bin/toolkit.new",
			"mv -f -- /usr/local/bin/toolkit.new /usr/local/bin/toolkit", "tee -- " + unitPath, "chmod 0644 -- " + unitPath, "systemctl daemon-reload",
		} {
			r.On(l, exectest.Exit(0, ""))
		}
		bad := strings.Repeat("0", 64) + "  x\n"
		good := binSum() + "  x\n"
		if stage == "copia" {
			r.On("sha256sum -- /usr/local/bin/toolkit.new", exectest.Exit(0, bad))
		} else {
			r.On("sha256sum -- /usr/local/bin/toolkit.new", exectest.Exit(0, good))
			r.On("sha256sum -- /usr/local/bin/toolkit", exectest.Exit(0, bad))
		}
		err := s.Register(context.Background(), resume.Spec{Executable: "/home/ana/dl/toolkit", Args: []string{"resume"}})
		if !errors.Is(err, resume.ErrBinaryMismatch) {
			t.Fatalf("%s: err = %v", stage, err)
		}
		for _, c := range r.Calls() {
			if c.Name == "systemctl" && c.Args[0] == "enable" {
				t.Fatalf("%s: habilitou a unit com binário divergente", stage)
			}
			if stage == "copia" && c.Name == "mv" {
				t.Fatal("copia divergente não pode virar o binário do root")
			}
		}
	}
}

// Dono e modo do state (e dos diretórios acima, até /var/lib), não só o prefixo.
func TestSystemdRegisterChecksStateOwnership(t *testing.T) {
	cases := map[string][3]string{
		"state de usuário":       {"1000 600", "0 755", "0 755"},
		"diretório do usuário":   {"0 600", "1000 755", "0 755"},
		"diretório gravável":     {"0 600", "0 777", "0 755"},
		"grupo escreve no state": {"0 620", "0 755", "0 755"},
		"/var/lib gravável":      {"0 600", "0 755", "0 775"},
		"stat ilegível":          {"lixo", "0 755", "0 755"},
	}
	for name, out := range cases {
		r := exectest.New(t)
		s := newSystemdRead(r, true)
		r.On("stat -c \"%u %a\" -- /var/lib/toolkit/state.json", exectest.Exit(0, out[0]+"\n"))
		r.On("stat -c \"%u %a\" -- /var/lib/toolkit", exectest.Exit(0, out[1]+"\n"))
		r.On("stat -c \"%u %a\" -- /var/lib", exectest.Exit(0, out[2]+"\n"))
		err := s.Register(context.Background(), resume.Spec{Executable: "/home/ana/dl/toolkit", Args: []string{"resume"}})
		if !errors.Is(err, resume.ErrUnsafeRootPath) {
			t.Errorf("%s: err = %v", name, err)
		}
		for _, c := range r.Calls() {
			if !c.ReadOnly {
				t.Errorf("%s: executou mutação antes de recusar: %v", name, c)
			}
		}
	}
}

func TestSystemdRegisterSkipsCopyWhenAlreadyInBinDir(t *testing.T) {
	r := exectest.New(t)
	s := newSystemdRead(r, true)
	scriptState(r)
	for _, l := range []string{"install -d -m 0755 -o root -g root /usr/local/bin", "tee -- " + unitPath, "chmod 0644 -- " + unitPath, "systemctl daemon-reload", "systemctl enable -- toolkit-resume.service"} {
		r.On(l, exectest.Exit(0, ""))
	}
	r.On("sha256sum -- /usr/local/bin/toolkit", exectest.Exit(0, binSum()+"  x\n"))
	if err := s.Register(context.Background(), resume.Spec{Executable: "/usr/local/bin/toolkit", Args: []string{"resume"}}); err != nil {
		t.Fatal(err)
	}
	for _, c := range r.Calls() {
		if c.Name == "mv" || (c.Name == "tee" && c.Args[1] == "/usr/local/bin/toolkit.new") {
			t.Fatal("não copia o binário sobre ele mesmo")
		}
	}
}

func TestSystemdRegisterErrorStops(t *testing.T) {
	r := exectest.New(t)
	s := newSystemdRead(r, true)
	scriptState(r)
	r.On("install -d -m 0755 -o root -g root /usr/local/bin", exectest.Exit(1, ""))
	if err := s.Register(context.Background(), resume.Spec{Executable: "/usr/local/bin/toolkit", Args: []string{"resume"}}); err == nil {
		t.Fatal("falha do install deve parar o registro")
	}
	if n := len(r.Calls()); n != 4 { // 3 stat + install
		t.Fatalf("deve parar no primeiro erro: %v", r.Calls())
	}
}

func TestSystemdRegisterReadErrorRunsNothing(t *testing.T) {
	r := exectest.New(t)
	s := newSystemd(r, true)
	s.ReadFile = func(string) ([]byte, error) { return nil, errors.New("sumiu") }
	if err := s.Register(context.Background(), resume.Spec{Executable: "/home/ana/dl/toolkit", Args: []string{"resume"}}); err == nil {
		t.Fatal("binário ilegível")
	}
	if len(r.Calls()) != 0 {
		t.Fatalf("nada pode executar: %v", r.Calls())
	}
}

func TestSystemdUnregister(t *testing.T) {
	r := exectest.New(t)
	s := newSystemd(r, false)
	r.On("test -f "+unitPath, exectest.Exit(1, ""), exectest.Exit(0, ""))
	if err := s.Unregister(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(r.Calls()) != 1 {
		t.Fatal("não registrada: só consulta")
	}
	r.OnElevated("systemctl disable -- toolkit-resume.service", exectest.Exit(0, ""))
	r.OnElevated("rm -f -- "+unitPath, exectest.Exit(0, ""))
	r.OnElevated("systemctl daemon-reload", exectest.Exit(0, ""))
	if err := s.Unregister(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.IsRegistered(context.Background()); !got {
		t.Fatal("o roteiro repete a última resposta (arquivo existe)")
	}
	if !r.Calls()[0].ReadOnly {
		t.Fatal("test -f é leitura")
	}
}

func newLaunchd(r *exectest.FakeRunner, fsys *resumetest.MemFS) resume.Launchd {
	return resume.Launchd{Runner: r, FS: fsys, Home: "/Users/ana", UID: "501", BinDir: "/Users/ana/.local/bin"}
}

func TestLaunchdRegisterUnregister(t *testing.T) {
	r := exectest.New(t)
	fsys := resumetest.NewMemFS()
	fsys.Src["/tmp/dl/toolkit"] = []byte("BIN")
	l := newLaunchd(r, fsys)
	r.On("launchctl bootout gui/501/dev.pilutech.toolkit", exectest.Exit(113, "")) // não carregado
	r.On("launchctl bootstrap gui/501 /Users/ana/Library/LaunchAgents/dev.pilutech.toolkit.plist", exectest.Exit(0, ""))
	if err := l.Register(context.Background(), resume.Spec{Executable: "/tmp/dl/toolkit", Args: []string{"resume", "--yes"}}); err != nil {
		t.Fatal(err)
	}
	if string(fsys.Files["/Users/ana/.local/bin/toolkit"]) != "BIN" || fsys.Perms["/Users/ana/.local/bin/toolkit"] != 0o755 {
		t.Fatal("binário")
	}
	cmd := string(fsys.Files["/Users/ana/.local/bin/toolkit-resume.command"])
	if cmd != "#!/bin/sh\n# gerado pelo toolkit: continua a receita depois do reboot\nexec '/Users/ana/.local/bin/toolkit' resume --yes\n" || fsys.Perms["/Users/ana/.local/bin/toolkit-resume.command"] != 0o755 {
		t.Fatalf("command:\n%s", cmd)
	}
	plist := string(fsys.Files["/Users/ana/Library/LaunchAgents/dev.pilutech.toolkit.plist"])
	for _, w := range []string{
		"<key>Label</key>\n\t<string>dev.pilutech.toolkit</string>",
		"<string>/usr/bin/open</string>", "<string>-a</string>", "<string>Terminal</string>",
		"<string>/Users/ana/.local/bin/toolkit-resume.command</string>",
		"<key>RunAtLoad</key>\n\t<true/>", "<key>LaunchOnlyOnce</key>\n\t<true/>",
	} {
		if !strings.Contains(plist, w) {
			t.Errorf("plist sem %q:\n%s", w, plist)
		}
	}
	if ok, _ := l.IsRegistered(context.Background()); !ok {
		t.Fatal("registrado")
	}
	if err := l.Unregister(context.Background()); err != nil {
		t.Fatal(err)
	}
	if ok, _ := l.IsRegistered(context.Background()); ok || fsys.Exists("/Users/ana/.local/bin/toolkit-resume.command") {
		t.Fatal("Unregister apaga plist e .command")
	}
	if err := l.Unregister(context.Background()); err != nil {
		t.Fatalf("idempotente: %v", err)
	}
}

func TestLaunchdPlistEscapesXML(t *testing.T) {
	l := resume.Launchd{Home: "/Users/a", UID: "1", BinDir: "/Users/a/b&c<d>"}
	p := l.Plist()
	if strings.Contains(p, "b&c<d>") || !strings.Contains(p, "b&amp;c&lt;d&gt;") {
		t.Fatalf("plist:\n%s", p)
	}
}

func TestLaunchdRejects(t *testing.T) {
	r := exectest.New(t)
	for _, l := range []resume.Launchd{
		{Runner: r, FS: resumetest.NewMemFS(), Home: "relativo", UID: "501", BinDir: "/b"},
		{Runner: r, FS: resumetest.NewMemFS(), Home: "/h", UID: "5;01", BinDir: "/b"},
		{Runner: r, FS: resumetest.NewMemFS(), Home: "/h", UID: "", BinDir: "/b"},
	} {
		if err := l.Register(context.Background(), resume.Spec{Executable: "/x", Args: []string{"resume"}}); err == nil {
			t.Errorf("deveria recusar %+v", l)
		}
	}
	l := newLaunchd(r, resumetest.NewMemFS())
	if err := l.Register(context.Background(), resume.Spec{Executable: "/x", Args: []string{"resume", "$(calc)"}}); err == nil {
		t.Error("arg inválido")
	}
	if got, err := l.CommandFile(resume.Spec{Args: []string{"resume"}}); err != nil || !strings.Contains(got, "exec '/Users/ana/.local/bin/toolkit' resume") {
		t.Fatalf("%q %v", got, err)
	}
}

func TestLaunchdShQuote(t *testing.T) {
	l := resume.Launchd{BinDir: "/Users/o'neil/bin"}
	got, _ := l.CommandFile(resume.Spec{Args: []string{"resume"}})
	if !strings.Contains(got, `exec '/Users/o'\''neil/bin/toolkit' resume`) {
		t.Fatalf("%s", got)
	}
}

func TestNew(t *testing.T) {
	r := exectest.New(t)
	for os, want := range map[platform.OS]string{platform.Windows: "resume.SchTask", platform.Linux: "resume.Systemd", platform.Darwin: "resume.Launchd"} {
		reg, err := resume.New(resume.Config{Info: platform.Info{OS: os}, Runner: r, FS: resumetest.NewMemFS()})
		if err != nil {
			t.Fatal(err)
		}
		if got := typeName(reg); got != want {
			t.Errorf("%s: %s", os, got)
		}
	}
	if _, err := resume.New(resume.Config{Info: platform.Info{OS: "plan9"}}); err == nil {
		t.Fatal("SO desconhecido")
	}
}

func typeName(v any) string {
	switch v.(type) {
	case resume.SchTask:
		return "resume.SchTask"
	case resume.Systemd:
		return "resume.Systemd"
	case resume.Launchd:
		return "resume.Launchd"
	}
	return "?"
}

func TestOSFS(t *testing.T) {
	dir := t.TempDir()
	f := resume.OSFS{}
	p := dir + "/a/b.txt"
	if err := f.MkdirAll(dir+"/a", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := f.WriteFile(p, []byte("x"), 0o644); err != nil || !f.Exists(p) {
		t.Fatalf("%v", err)
	}
	if err := f.CopyFile(p, dir+"/a/c.txt", 0o755); err != nil || !f.Exists(dir+"/a/c.txt") {
		t.Fatalf("%v", err)
	}
	if err := f.Remove(p); err != nil || f.Exists(p) {
		t.Fatal("remove")
	}
	if err := f.Remove(p); err != nil {
		t.Fatalf("remover inexistente é sucesso: %v", err)
	}
	if err := f.CopyFile(dir+"/nao", dir+"/x", 0o755); err == nil {
		t.Fatal("copiar inexistente")
	}
}

// A unit roda como ROOT: binário, state e log que um usuário comum pode alterar são escalada de
// privilégio no boot. Tudo é recusado ANTES de qualquer comando.
func TestSystemdRefusesUserWritablePaths(t *testing.T) {
	spec := resume.Spec{Executable: "/home/ana/dl/toolkit", Args: []string{"resume"}, LogPath: "/var/lib/toolkit/logs/r.log"}
	cases := map[string]resume.Systemd{
		"BinDir do usuário":      {BinDir: "/home/ana/.local/bin", StatePath: "/var/lib/toolkit/state.json"},
		"BinDir em /tmp":         {BinDir: "/tmp/bin", StatePath: "/var/lib/toolkit/state.json"},
		"BinDir com ..":          {BinDir: "/usr/local/../../home/ana/bin", StatePath: "/var/lib/toolkit/state.json"},
		"BinDir relativo":        {BinDir: "usr/local/bin", StatePath: "/var/lib/toolkit/state.json"},
		"state na home":          {BinDir: "/usr/local/lib/toolkit", StatePath: "/home/ana/.local/state/toolkit/state.json"},
		"state em /var/lib/..":   {BinDir: "/usr/local/lib/toolkit", StatePath: "/var/lib/../../home/ana/state.json"},
		"state em /var/tmp":      {BinDir: "/usr/local/lib/toolkit", StatePath: "/var/tmp/state.json"},
		"state vazio":            {BinDir: "/usr/local/lib/toolkit"},
		"BinDir vizinho de /usr": {BinDir: "/usrx/bin", StatePath: "/var/lib/toolkit/state.json"},
	}
	for name, s := range cases {
		r := exectest.New(t) // qualquer comando inesperado falha o teste
		s.Runner, s.IsRoot = r, true
		err := s.Register(context.Background(), spec)
		if !errors.Is(err, resume.ErrUnsafeRootPath) {
			t.Errorf("%s: err = %v, quero ErrUnsafeRootPath", name, err)
		}
		if len(r.Calls()) != 0 {
			t.Errorf("%s: executou comando antes de recusar: %v", name, r.Calls())
		}
	}
	// log gravável por usuário
	s := newSystemd(exectest.New(t), true)
	for _, lp := range []string{"/home/ana/.local/state/toolkit/logs/r.log", "/tmp/r.log", "/var/logx/r.log"} {
		sp := spec
		sp.LogPath = lp
		if _, err := s.UnitFile(sp); !errors.Is(err, resume.ErrUnsafeRootPath) {
			t.Errorf("log %s: err = %v", lp, err)
		}
	}
}

// Sem BinDir, o root executa a cópia em /usr/local/lib/toolkit (root:root 0755), e a unit bloqueia /home.
func TestSystemdDefaultsToRootOwnedBinDir(t *testing.T) {
	r := exectest.New(t)
	s := resume.Systemd{Runner: r, IsRoot: true, StatePath: "/var/lib/toolkit/state.json"}
	unit, err := s.UnitFile(resume.Spec{Args: []string{"resume"}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(unit, "ExecStart=/usr/local/lib/toolkit/toolkit resume --no-tui\n") || !strings.Contains(unit, "ProtectHome=true\n") {
		t.Fatalf("unit:\n%s", unit)
	}
	if resume.RootBinDir != "/usr/local/lib/toolkit" {
		t.Fatal(resume.RootBinDir)
	}
	s.ReadFile = fakeRead
	scriptState(r)
	for _, line := range []string{
		"install -d -m 0755 -o root -g root /usr/local/lib/toolkit",
		"tee -- /usr/local/lib/toolkit/toolkit.new", "chown root:root -- /usr/local/lib/toolkit/toolkit.new", "chmod 0755 -- /usr/local/lib/toolkit/toolkit.new",
		"mv -f -- /usr/local/lib/toolkit/toolkit.new /usr/local/lib/toolkit/toolkit",
		"tee -- " + unitPath, "chmod 0644 -- " + unitPath, "systemctl daemon-reload", "systemctl enable -- toolkit-resume.service",
	} {
		r.On(line, exectest.Exit(0, ""))
	}
	r.On("sha256sum -- /usr/local/lib/toolkit/toolkit.new", exectest.Exit(0, binSum()+"  x\n"))
	r.On("sha256sum -- /usr/local/lib/toolkit/toolkit", exectest.Exit(0, binSum()+"  x\n"))
	if err := s.Register(context.Background(), resume.Spec{Executable: "/home/ana/.local/bin/toolkit", Args: []string{"resume"}}); err != nil {
		t.Fatal(err)
	}
}

// New nunca repassa o BinDir do usuário ao systemd (o de quem não é root é ~/.local/bin).
func TestNewLinuxIgnoresUserBinDir(t *testing.T) {
	reg, err := resume.New(resume.Config{
		Info:      platform.Info{OS: platform.Linux},
		Paths:     platform.Paths{BinDir: "/home/ana/.local/bin"},
		Runner:    exectest.New(t),
		StatePath: "/var/lib/toolkit/state.json",
	})
	if err != nil {
		t.Fatal(err)
	}
	sd := reg.(resume.Systemd)
	if sd.BinDir != "" {
		t.Fatalf("BinDir do usuário vazou para o systemd: %q", sd.BinDir)
	}
}

// Sem ReadFile injetado, só o próprio executável é lido (por /proc/self/exe no Linux).
func TestSystemdDefaultReadsOnlyOwnExecutable(t *testing.T) {
	r := exectest.New(t) // nada pode executar: a leitura falha antes de qualquer comando
	s := newSystemd(r, true)
	err := s.Register(context.Background(), resume.Spec{Executable: "/home/ana/dl/toolkit", Args: []string{"resume"}})
	if !errors.Is(err, resume.ErrNotOwnExecutable) {
		t.Fatalf("err = %v, quero ErrNotOwnExecutable", err)
	}
	if len(r.Calls()) != 0 {
		t.Fatalf("executou comando: %v", r.Calls())
	}
	self, _ := os.Executable()
	if _, err := resume.ReadOwnExecutableForTest(self); err != nil {
		t.Fatalf("o próprio executável deve ser lido: %v", err)
	}
	if _, err := resume.ReadOwnExecutableForTest("/bin/sh"); !errors.Is(err, resume.ErrNotOwnExecutable) {
		t.Fatalf("/bin/sh não é o executável em execução: %v", err)
	}
}

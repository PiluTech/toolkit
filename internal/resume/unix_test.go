package resume_test

import (
	"context"
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

func TestSystemdRegisterAsRootAndNonRoot(t *testing.T) {
	for _, root := range []bool{true, false} {
		r := exectest.New(t)
		s := newSystemd(r, root)
		script := func(line string) {
			if root {
				r.On(line, exectest.Exit(0, ""))
			} else {
				r.OnElevated(line, exectest.Exit(0, ""))
			}
		}
		script("install -d -m 0755 /usr/local/bin")
		script("install -m 0755 -- /home/ana/dl/toolkit /usr/local/bin/toolkit")
		script("tee -- " + unitPath)
		script("chmod 0644 -- " + unitPath)
		script("systemctl daemon-reload")
		script("systemctl enable -- toolkit-resume.service")
		err := s.Register(context.Background(), resume.Spec{Executable: "/home/ana/dl/toolkit", Args: []string{"resume"}, LogPath: "/var/lib/toolkit/logs/r.log"})
		if err != nil {
			t.Fatalf("root=%v: %v", root, err)
		}
		calls := r.Calls()
		if len(calls) != 6 {
			t.Fatalf("root=%v: %d chamadas", root, len(calls))
		}
		for _, c := range calls {
			if c.Elevate == root || c.ReadOnly {
				t.Fatalf("root=%v: Elevate/ReadOnly errado em %v", root, c)
			}
		}
		// a unit vai por stdin do tee, nunca por argumento
		if calls[2].Stdin == nil {
			t.Fatal("tee sem stdin")
		}
		// enable, nunca start
		for _, c := range calls {
			if c.Name == "systemctl" && len(c.Args) > 0 && (c.Args[0] == "start" || c.Args[0] == "enable --now") {
				t.Fatalf("não pode iniciar a unit agora: %v", c)
			}
		}
	}
}

func TestSystemdRegisterSkipsCopyWhenAlreadyInBinDir(t *testing.T) {
	r := exectest.New(t)
	s := newSystemd(r, true)
	for _, l := range []string{"install -d -m 0755 /usr/local/bin", "tee -- " + unitPath, "chmod 0644 -- " + unitPath, "systemctl daemon-reload", "systemctl enable -- toolkit-resume.service"} {
		r.On(l, exectest.Exit(0, ""))
	}
	if err := s.Register(context.Background(), resume.Spec{Executable: "/usr/local/bin/toolkit", Args: []string{"resume"}}); err != nil {
		t.Fatal(err)
	}
	for _, c := range r.Calls() {
		if c.Name == "install" && len(c.Args) > 0 && c.Args[0] == "-m" {
			t.Fatal("não copia o binário sobre ele mesmo")
		}
	}
}

func TestSystemdRegisterErrorStops(t *testing.T) {
	r := exectest.New(t)
	s := newSystemd(r, true)
	r.On("install -d -m 0755 /usr/local/bin", exectest.Exit(1, ""))
	if err := s.Register(context.Background(), resume.Spec{Executable: "/usr/local/bin/toolkit", Args: []string{"resume"}}); err == nil {
		t.Fatal("falha do install deve parar o registro")
	}
	if len(r.Calls()) != 1 {
		t.Fatalf("deve parar no primeiro erro: %v", r.Calls())
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

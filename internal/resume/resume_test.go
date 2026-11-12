package resume_test

import (
	"context"
	"encoding/base64"
	"strings"
	"testing"
	"unicode/utf16"

	"github.com/PiluTech/toolkit/internal/exec"
	"github.com/PiluTech/toolkit/internal/exec/exectest"
	"github.com/PiluTech/toolkit/internal/resume"
	"github.com/PiluTech/toolkit/internal/resume/resumetest"
)

const winPS = `C:\Windows\System32\WindowsPowerShell\v1.0\powershell.exe`

func decodePS(t *testing.T, c exec.Cmd) string {
	t.Helper()
	if c.Name != winPS || len(c.Args) != 6 || c.Args[4] != "-EncodedCommand" {
		t.Fatalf("comando inesperado: %+v", c)
	}
	raw, err := base64.StdEncoding.DecodeString(c.Args[5])
	if err != nil {
		t.Fatal(err)
	}
	u := make([]uint16, len(raw)/2)
	for i := range u {
		u[i] = uint16(raw[2*i]) | uint16(raw[2*i+1])<<8
	}
	return string(utf16.Decode(u))
}

func ps(script string, readOnly bool) exec.Cmd {
	// repete a montagem do pacote para roteirizar o FakeRunner (o texto do script é o que importa nos testes)
	u := utf16.Encode([]rune(script))
	b := make([]byte, 0, len(u)*2)
	for _, c := range u {
		b = append(b, byte(c), byte(c>>8))
	}
	return exec.Cmd{Name: winPS, ReadOnly: readOnly,
		Args: []string{"-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-EncodedCommand", base64.StdEncoding.EncodeToString(b)}}
}

func TestSchTaskRegister(t *testing.T) {
	fsys := resumetest.NewMemFS()
	fsys.Src[`C:\Users\ana\Downloads\toolkit.exe`] = []byte("EXE")
	runner := exectest.New(t)
	s := resume.SchTask{Runner: runner, FS: fsys, BinDir: `C:\ProgramData\toolkit\bin`}
	sp := resume.Spec{Executable: `C:\Users\ana\Downloads\toolkit.exe`, Args: []string{"resume", "--yes"}, User: `PC\ana`, Elevated: true}
	script, err := s.RegisterScript(sp)
	if err != nil {
		t.Fatal(err)
	}
	want := `$ErrorActionPreference = 'Stop'
$action = New-ScheduledTaskAction -Execute 'C:\ProgramData\toolkit\bin\toolkit.exe' -Argument 'resume --yes'
$trigger = New-ScheduledTaskTrigger -AtLogOn -User 'PC\ana'
$principal = New-ScheduledTaskPrincipal -UserId 'PC\ana' -LogonType Interactive -RunLevel Highest
Register-ScheduledTask -TaskName 'dev.pilutech.toolkit' -Action $action -Trigger $trigger -Principal $principal -Force | Out-Null`
	if script != want {
		t.Fatalf("script:\n%s", script)
	}
	runner.On(ps(script, false).String(), exectest.Exit(0, ""))
	if err := s.Register(context.Background(), sp); err != nil {
		t.Fatal(err)
	}
	if string(fsys.Files[`C:\ProgramData\toolkit\bin\toolkit.exe`]) != "EXE" || !fsys.Dirs[`C:\ProgramData\toolkit\bin`] {
		t.Fatal("o binário deve ser copiado para BinDir antes de registrar")
	}
	calls := runner.Calls()
	if len(calls) != 1 || calls[0].ReadOnly || decodePS(t, calls[0]) != script {
		t.Fatalf("%v", calls)
	}
}

func TestSchTaskLimitedAndQuoting(t *testing.T) {
	s := resume.SchTask{BinDir: `C:\Users\o'neil\AppData\Local\toolkit\bin`}
	script, err := s.RegisterScript(resume.Spec{Args: []string{"resume"}, User: "o'neil"})
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range []string{`-RunLevel Limited`, `-User 'o''neil'`, `'C:\Users\o''neil\AppData\Local\toolkit\bin\toolkit.exe'`} {
		if !strings.Contains(script, w) {
			t.Errorf("falta %q em:\n%s", w, script)
		}
	}
}

func TestSchTaskRejects(t *testing.T) {
	s := resume.SchTask{BinDir: `C:\bin`}
	bad := []resume.Spec{
		{Args: []string{"resume"}},                           // sem usuário
		{Args: []string{"resume", "'; calc; '"}, User: "a"},  // injeção em args
		{Args: []string{"resume", "a b"}, User: "a"},         // espaço
		{Args: []string{"rodar"}, User: "a"},                 // não começa por resume
		{Args: nil, User: "a"},                               // sem args
		{Args: []string{"resume"}, User: "a\nb"},             // controle no usuário
		{Args: []string{"resume", "--x=$(calc)"}, User: "a"}, // $ e parênteses
	}
	for i, sp := range bad {
		if _, err := s.RegisterScript(sp); err == nil {
			t.Errorf("caso %d: deveria recusar %+v", i, sp)
		}
	}
	if _, err := (resume.SchTask{BinDir: "C:\\b\x00"}).RegisterScript(resume.Spec{Args: []string{"resume"}, User: "a"}); err == nil {
		t.Error("BinDir com controle")
	}
}

func TestSchTaskUnregisterAndIsRegistered(t *testing.T) {
	runner := exectest.New(t)
	s := resume.SchTask{Runner: runner, FS: resumetest.NewMemFS(), BinDir: `C:\bin`}
	unreg := "$ErrorActionPreference = 'Stop'\nif (Get-ScheduledTask -TaskName 'dev.pilutech.toolkit' -ErrorAction SilentlyContinue) { Unregister-ScheduledTask -TaskName 'dev.pilutech.toolkit' -Confirm:$false }"
	runner.On(ps(unreg, false).String(), exectest.Exit(0, ""))
	if err := s.Unregister(context.Background()); err != nil {
		t.Fatal(err)
	}
	q := "if (Get-ScheduledTask -TaskName 'dev.pilutech.toolkit' -ErrorAction SilentlyContinue) { exit 0 } else { exit 1 }"
	runner.On(ps(q, true).String(), exectest.Exit(0, ""), exectest.Exit(1, ""), exectest.Exit(5, ""))
	for i, want := range []bool{true, false} {
		got, err := s.IsRegistered(context.Background())
		if err != nil || got != want {
			t.Fatalf("%d: %v %v", i, got, err)
		}
	}
	if _, err := s.IsRegistered(context.Background()); err == nil {
		t.Fatal("código inesperado deve ser erro")
	}
	for _, c := range runner.Calls() {
		if strings.Contains(decodePS(t, c), "exit 0 } else") && !c.ReadOnly {
			t.Fatal("IsRegistered é leitura")
		}
	}
}

func TestSchTaskRegisterFailure(t *testing.T) {
	fsys := resumetest.NewMemFS()
	fsys.Src["x.exe"] = []byte("E")
	runner := exectest.New(t)
	s := resume.SchTask{Runner: runner, FS: fsys, BinDir: `C:\bin`}
	sp := resume.Spec{Executable: "x.exe", Args: []string{"resume"}, User: "a"}
	script, _ := s.RegisterScript(sp)
	runner.On(ps(script, false).String(), exectest.Reply{Result: exec.Result{ExitCode: 1, Stderr: []byte("acesso negado\x1b[0m")}})
	err := s.Register(context.Background(), sp)
	if err == nil || !strings.Contains(err.Error(), "acesso negado") || strings.Contains(err.Error(), "\x1b") {
		t.Fatalf("err=%v", err)
	}
}

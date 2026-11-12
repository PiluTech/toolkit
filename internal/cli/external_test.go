package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/PiluTech/toolkit/internal/detect"
	"github.com/PiluTech/toolkit/internal/exec"
	"github.com/PiluTech/toolkit/internal/exec/exectest"
	"github.com/PiluTech/toolkit/internal/tui"
)

const extCatalog = `apps:
  discord:
    name: Discord
    category: comunicacao
    windows: [{ provider: winget, id: Discord.Discord }]
    macos:   [{ provider: brew, cask: discord }]
    linux:   [{ provider: flatpak, id: com.discordapp.Discord }]
    detect:
      macos: { app_bundle: Discord.app, bundle_id: com.hnc.Discord }
      windows: { display_name: Discord }
      linux: { desktop_id: discord.desktop }
  vlc:
    name: VLC
    category: midia
    macos: [{ provider: brew, cask: vlc }]
`

const discordPlist = `<?xml version="1.0" encoding="UTF-8"?>
<plist version="1.0"><dict>
<key>CFBundleIdentifier</key><string>com.hnc.Discord</string>
<key>CFBundleShortVersionString</key><string>0.0.350</string>
</dict></plist>`

// discordDmg é o cenário do bug: o Discord veio do .dmg (/Applications/Discord.app) e o brew não o conhece.
func discordDmg() fstest.MapFS {
	return fstest.MapFS{"Applications/Discord.app/Contents/Info.plist": {Data: []byte(discordPlist)}}
}

func macFake(t *testing.T) *exectest.FakeRunner {
	t.Helper()
	return exectest.New(t).WithPath("brew", "/opt/homebrew/bin/brew").
		On("brew list --versions --cask discord", exectest.Reply{Result: exec.Result{ExitCode: 1}}).
		On("brew list --versions --cask vlc", exectest.Reply{Result: exec.Result{ExitCode: 1}})
}

func macDeps(f *exectest.FakeRunner, fsys fstest.MapFS) Deps {
	return Deps{GOOS: "darwin", EmbeddedYML: []byte(extCatalog), NewRunner: func() exec.Runner { return f },
		CheckElevation: func(exec.Cmd) error { return nil }, Interactive: func() bool { return false },
		Detector: func(r exec.Runner) ExternalDetector {
			return detect.New(r, detect.Options{GOOS: "darwin", FS: fsys, Home: "/Users/ana"})
		}}
}

func runCmd(t *testing.T, deps Deps, args ...string) (out, errOut string, err error) {
	t.Helper()
	var o, e bytes.Buffer
	cmd := NewRootCmdWith(&o, &e, deps)
	cmd.SetArgs(args)
	err = cmd.ExecuteContext(context.Background())
	return o.String(), e.String(), err
}

func noMutation(t *testing.T, f *exectest.FakeRunner) {
	t.Helper()
	for _, c := range f.Calls() {
		if !c.ReadOnly || c.Elevate {
			t.Errorf("mutação inesperada: %s", c)
		}
		if strings.Contains(c.String(), "install") || strings.Contains(c.String(), "update") {
			t.Errorf("não podia instalar nem atualizar: %s", c)
		}
	}
}

const wantDiscordExternal = "já instalado fora do Homebrew em /Applications/Discord.app (versão 0.0.350): nada a fazer"

func TestInstallPulaAppInstaladoForaDoGerenciador(t *testing.T) {
	f := macFake(t)
	// Sem --yes e sem terminal: o plano vazio não pergunta nem exige confirmação.
	out, _, err := runCmd(t, macDeps(f, discordDmg()), "install", "discord")
	if err != nil {
		t.Fatalf("o app já está instalado: não pode falhar: %v\n%s", err, out)
	}
	if !strings.Contains(out, wantDiscordExternal) || !strings.Contains(out, "Nada a fazer.") {
		t.Fatalf("saída:\n%s", out)
	}
	noMutation(t, f)
}

func TestInstallDryRunPulaAppInstaladoForaDoGerenciador(t *testing.T) {
	f := macFake(t)
	out, _, err := runCmd(t, macDeps(f, discordDmg()), "install", "discord", "--dry-run")
	if err != nil || !strings.Contains(out, wantDiscordExternal) || strings.Contains(out, "brew install") {
		t.Fatalf("err=%v\n%s", err, out)
	}
	noMutation(t, f)
}

func TestInstallFalhaSeUmDosAppsFalhaMasPulaOExterno(t *testing.T) {
	// discord fora do gerenciador (pula); vlc não está em lugar nenhum (instala normalmente).
	f := macFake(t).On("brew update", exectest.Exit(0, "")).On("brew install --cask vlc", exectest.Exit(0, ""))
	out, _, err := runCmd(t, macDeps(f, discordDmg()), "install", "discord", "vlc", "--yes")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if !strings.Contains(out, wantDiscordExternal) || !strings.Contains(out, "instalado com brew") {
		t.Fatalf("saída:\n%s", out)
	}
	for _, c := range f.Calls() {
		if strings.Contains(c.String(), "install --cask discord") {
			t.Fatalf("o discord não pode ser instalado pelo brew: %s", c)
		}
	}
}

func TestInstallSemAppForaInstalaPeloBrew(t *testing.T) {
	f := macFake(t).On("brew update", exectest.Exit(0, "")).On("brew install --cask discord", exectest.Exit(0, ""))
	out, _, err := runCmd(t, macDeps(f, fstest.MapFS{}), "install", "discord", "--yes")
	if err != nil || !strings.Contains(out, "instalado com brew") || strings.Contains(out, "fora do") {
		t.Fatalf("err=%v\n%s", err, out)
	}
}

func TestInstallInstaladoPeloBrewNaoViraExterno(t *testing.T) {
	f := exectest.New(t).WithPath("brew", "/opt/homebrew/bin/brew").
		On("brew list --versions --cask discord", exectest.Exit(0, "discord 0.0.360\n"))
	out, _, err := runCmd(t, macDeps(f, discordDmg()), "install", "discord", "--dry-run")
	if err != nil || !strings.Contains(out, "já instalado (brew, versão 0.0.360)") || strings.Contains(out, "fora do") {
		t.Fatalf("err=%v\n%s", err, out)
	}
}

func TestInstallSemDetectorLigadoMantemOComportamentoAntigo(t *testing.T) {
	f := macFake(t).On("brew update", exectest.Exit(0, "")).On("brew install --cask discord", exectest.Exit(0, ""))
	d := macDeps(f, discordDmg())
	d.Detector = nil
	out, _, err := runCmd(t, d, "install", "discord", "--yes")
	if err != nil || !strings.Contains(out, "instalado com brew") {
		t.Fatalf("err=%v\n%s", err, out)
	}
}

func TestListInstalledJSON(t *testing.T) {
	f := macFake(t)
	out, _, err := runCmd(t, macDeps(f, discordDmg()), "list", "--installed", "--json")
	if err != nil {
		t.Fatal(err)
	}
	var items []listItem
	if err := json.Unmarshal([]byte(out), &items); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	d, v := items[0], items[1]
	if d.Installed == nil || !*d.Installed || d.Source != "external" || d.ExternalManager != "Homebrew" || d.ExternalPath != "/Applications/Discord.app" || d.InstalledVersion != "0.0.350" {
		t.Fatalf("discord: %+v", d)
	}
	if v.Installed == nil || *v.Installed || v.Source != "" {
		t.Fatalf("vlc: %+v", v)
	}
	noMutation(t, f)
}

func TestListSemInstalledNaoTocaNoSistema(t *testing.T) {
	f := exectest.New(t) // qualquer comando falha o teste
	out, _, err := runCmd(t, macDeps(f, discordDmg()), "list", "--json")
	if err != nil || strings.Contains(out, "installed") || len(f.Calls()) != 0 {
		t.Fatalf("err=%v calls=%d\n%s", err, len(f.Calls()), out)
	}
}

func TestListInstalledTabela(t *testing.T) {
	f := macFake(t)
	out, _, err := runCmd(t, macDeps(f, discordDmg()), "list", "--installed")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"INSTALADO", "instalado (fora do Homebrew) 0.0.350 em /Applications/Discord.app", "não instalado"} {
		if !strings.Contains(out, want) {
			t.Errorf("saída sem %q:\n%s", want, out)
		}
	}
}

func TestListInstalledSoVaiParaOSistemaAtual(t *testing.T) {
	_, _, err := runCmd(t, macDeps(exectest.New(t), nil), "list", "--installed", "--os", "windows")
	if err == nil || !strings.Contains(err.Error(), "--installed só vale para o sistema atual") {
		t.Fatalf("err=%v", err)
	}
}

func TestListInstalledSanitizaTextoDeFora(t *testing.T) {
	plist := strings.Replace(discordPlist, "0.0.350", "1.0&#x202E;x", 1)
	fsys := fstest.MapFS{"Applications/Discord.app/Contents/Info.plist": {Data: []byte(plist)}}
	out, _, err := runCmd(t, macDeps(macFake(t), fsys), "list", "--installed")
	if err != nil || strings.ContainsRune(out, '\u202e') {
		t.Fatalf("err=%v out=%q", err, out)
	}
}

func TestAppsBackendStatusExterno(t *testing.T) {
	f := macFake(t)
	b := &appsBackend{deps: macDeps(f, discordDmg())}
	st := b.Status(context.Background(), "discord")
	want := tui.AppStatus{Installed: true, Version: "0.0.350", External: true, ExternalManager: "Homebrew", ExternalWhere: "/Applications/Discord.app"}
	if st != want {
		t.Fatalf("%+v", st)
	}
	st = b.Status(context.Background(), "vlc")
	if st.Installed || st.External || st.Err != nil {
		t.Fatalf("vlc: %+v", st)
	}
	noMutation(t, f)
}

func TestPlannerNaoPlanejaComandoParaAppExterno(t *testing.T) {
	f := macFake(t)
	p := &tuiPlanner{deps: macDeps(f, discordDmg()), rd: newTUIEnv(t).rd()}
	plan, err := p.Plan(context.Background(), tui.Selection{Apps: []tui.Mark{{ID: "discord", Name: "Discord"}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Commands) != 0 || len(plan.Approved) != 0 || len(plan.Problems) != 0 {
		t.Fatalf("o plano não pode ter comandos: %+v", plan)
	}
	if !plan.NothingToDo {
		t.Fatalf("NothingToDo deveria ser true: %+v", plan)
	}
	joined := strings.Join(plan.Changes, "\n")
	if !strings.Contains(joined, "já instalado fora do Homebrew em /Applications/Discord.app") {
		t.Fatalf("o plano deveria explicar: %q", joined)
	}
}

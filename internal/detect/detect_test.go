package detect

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"testing/fstest"

	"github.com/PiluTech/toolkit/internal/catalog"
	"github.com/PiluTech/toolkit/internal/exec"
)

// stub é um exec.Runner que nunca executa nada: responde pelo handler e guarda as chamadas.
type stub struct {
	mu     sync.Mutex
	handle func(exec.Cmd) (exec.Result, error)
	paths  map[string]bool
	calls  []exec.Cmd
}

func (s *stub) Run(_ context.Context, c exec.Cmd) (exec.Result, error) {
	s.mu.Lock()
	s.calls = append(s.calls, c)
	s.mu.Unlock()
	if s.handle == nil {
		return exec.Result{}, errors.New("sem roteiro")
	}
	return s.handle(c)
}

func (s *stub) LookPath(name string) (string, error) {
	if s.paths[name] {
		return "/usr/bin/" + name, nil
	}
	return "", errors.New("não achado")
}

func (s *stub) assertReadOnly(t *testing.T) {
	t.Helper()
	for _, c := range s.calls {
		if !c.ReadOnly || c.Elevate {
			t.Errorf("comando não é só leitura: %s", c)
		}
	}
}

func ok(out string) (exec.Result, error) { return exec.Result{Stdout: []byte(out)}, nil }

const plistTpl = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>CFBundleName</key><string>Discord</string>
	<key>CFBundleIdentifier</key><string>%ID%</string>
	%EXTRA%
	<key>LSUIElement</key><true/>
	<key>Nested</key><dict><key>CFBundleShortVersionString</key><string>9.9.9</string></dict>
</dict>
</plist>`

func plist(id, extra string) *fstest.MapFile {
	s := strings.NewReplacer("%ID%", id, "%EXTRA%", extra).Replace(plistTpl)
	return &fstest.MapFile{Data: []byte(s)}
}

func app(t *testing.T, yml string) catalog.App {
	t.Helper()
	c, err := catalog.Parse("t.yaml", []byte("apps:\n  x:\n    name: X\n    category: dev\n    any: [{ provider: flatpak, id: a.b.C }]\n    detect:\n"+yml))
	if err != nil {
		t.Fatal(err)
	}
	a, _ := c.Get("x")
	return a
}

func find(t *testing.T, d *Detector, a catalog.App) (Found, bool) {
	t.Helper()
	f, ok, err := d.Find(context.Background(), a)
	if err != nil {
		t.Fatal(err)
	}
	return f, ok
}

const discordMac = "      macos: { app_bundle: Discord.app, bundle_id: com.hnc.Discord }\n"

func TestMacDiscordInstaladoPeloDmg(t *testing.T) {
	fsys := fstest.MapFS{
		"Applications/Discord.app/Contents/Info.plist": plist("com.hnc.Discord", "<key>CFBundleShortVersionString</key><string>0.0.350</string><key>CFBundleVersion</key><string>350</string>"),
	}
	d := New(&stub{}, Options{GOOS: "darwin", FS: fsys})
	f, found := find(t, d, app(t, discordMac))
	if !found || f.Where != "/Applications/Discord.app" || f.Version != "0.0.350" {
		t.Fatalf("achado=%v %+v", found, f)
	}
}

func TestMacEmHomeApplications(t *testing.T) {
	fsys := fstest.MapFS{"Users/p/Applications/Discord.app/Contents/Info.plist": plist("com.hnc.Discord", "<key>CFBundleShortVersionString</key><string>1.2</string>")}
	d := New(&stub{}, Options{GOOS: "darwin", FS: fsys, Home: "/Users/p"})
	f, found := find(t, d, app(t, discordMac))
	if !found || f.Where != "/Users/p/Applications/Discord.app" || f.Version != "1.2" {
		t.Fatalf("%v %+v", found, f)
	}
	// Sem Home, a pasta do usuário nem é olhada.
	d = New(&stub{}, Options{GOOS: "darwin", FS: fsys})
	if _, found := find(t, d, app(t, discordMac)); found {
		t.Fatal("sem Home não pode achar em ~/Applications")
	}
}

func TestMacNomeComAcentoEEspaco(t *testing.T) {
	fsys := fstest.MapFS{"Applications/Visual Studio Código.app/Contents/Info.plist": plist("x.y", "<key>CFBundleShortVersionString</key><string>1.90</string>")}
	d := New(&stub{}, Options{GOOS: "darwin", FS: fsys})
	f, found := find(t, d, app(t, "      macos: { app_bundle: \"Visual Studio Código.app\" }\n"))
	if !found || f.Where != "/Applications/Visual Studio Código.app" || f.Version != "1.90" {
		t.Fatalf("%v %+v", found, f)
	}
}

func TestMacBundleSemInfoPlist(t *testing.T) {
	fsys := fstest.MapFS{"Applications/Discord.app/Contents/MacOS/Discord": &fstest.MapFile{Data: []byte("x")}}
	d := New(&stub{}, Options{GOOS: "darwin", FS: fsys})
	f, found := find(t, d, app(t, discordMac))
	if !found || f.Version != "" || f.Where != "/Applications/Discord.app" {
		t.Fatalf("o bundle existe mesmo sem Info.plist: %v %+v", found, f)
	}
}

func TestMacPlistSemShortVersionUsaBuildESemNada(t *testing.T) {
	fsys := fstest.MapFS{"Applications/Discord.app/Contents/Info.plist": plist("com.hnc.Discord", "<key>CFBundleVersion</key><string>350</string>")}
	f, _ := find(t, New(&stub{}, Options{GOOS: "darwin", FS: fsys}), app(t, discordMac))
	if f.Version != "350" {
		t.Fatalf("esperava CFBundleVersion: %+v", f)
	}
	fsys = fstest.MapFS{"Applications/Discord.app/Contents/Info.plist": plist("com.hnc.Discord", "")}
	f, found := find(t, New(&stub{}, Options{GOOS: "darwin", FS: fsys}), app(t, discordMac))
	if !found || f.Version != "" {
		t.Fatalf("sem versão alguma: %v %+v", found, f)
	}
}

func TestMacVersaoComBidiEhSanitizadaEESCNoXMLNaoPassa(t *testing.T) {
	// U+202E é XML válido: sai escapado, nunca cru.
	fsys := fstest.MapFS{"Applications/Discord.app/Contents/Info.plist": plist("com.hnc.Discord", "<key>CFBundleShortVersionString</key><string>1.0&#x202E;0</string>")}
	f, found := find(t, New(&stub{}, Options{GOOS: "darwin", FS: fsys}), app(t, discordMac))
	if !found || strings.ContainsRune(f.Version, '\u202e') || !strings.Contains(f.Version, `\u202e`) {
		t.Fatalf("versão crua no terminal: %q", f.Version)
	}
	// ESC não é XML 1.0 válido: o plist inteiro é ignorado (o bundle continua achado, sem versão).
	fsys = fstest.MapFS{"Applications/Discord.app/Contents/Info.plist": plist("com.hnc.Discord", "<key>CFBundleShortVersionString</key><string>1.0&#27;]0;pwn&#7;</string>")}
	f, found = find(t, New(&stub{}, Options{GOOS: "darwin", FS: fsys}), app(t, discordMac))
	if !found || f.Version != "" {
		t.Fatalf("%v %q", found, f.Version)
	}
}

func TestMacPlistBinarioUsaPlutilSoLeitura(t *testing.T) {
	fsys := fstest.MapFS{"Applications/Discord.app/Contents/Info.plist": &fstest.MapFile{Data: []byte("bplist00\x00\x01")}}
	s := &stub{handle: func(c exec.Cmd) (exec.Result, error) {
		if c.Name != "/usr/bin/plutil" || strings.Join(c.Args, " ") != "-convert json -o - /Applications/Discord.app/Contents/Info.plist" {
			t.Errorf("comando inesperado: %s", c)
		}
		return ok(`{"CFBundleIdentifier":"com.hnc.Discord","CFBundleShortVersionString":"2.0\u001b[31m"}`)
	}}
	f, found := find(t, New(s, Options{GOOS: "darwin", FS: fsys}), app(t, discordMac))
	if !found || strings.Contains(f.Version, "\x1b") || !strings.HasPrefix(f.Version, "2.0") {
		t.Fatalf("%v %+v", found, f)
	}
	if len(s.calls) != 1 {
		t.Fatalf("chamadas: %d", len(s.calls))
	}
	s.assertReadOnly(t)
}

func TestMacPlutilFalhandoNaoDerrubaADeteccao(t *testing.T) {
	fsys := fstest.MapFS{"Applications/Discord.app/Contents/Info.plist": &fstest.MapFile{Data: []byte("bplist00")}}
	s := &stub{handle: func(exec.Cmd) (exec.Result, error) { return exec.Result{ExitCode: 1}, nil }}
	f, found := find(t, New(s, Options{GOOS: "darwin", FS: fsys}), app(t, discordMac))
	if !found || f.Version != "" {
		t.Fatalf("%v %+v", found, f)
	}
}

func TestMacBundleRenomeadoAchadoPeloIdentificador(t *testing.T) {
	fsys := fstest.MapFS{
		"Applications/Outro.app/Contents/Info.plist":       plist("com.exemplo.outro", ""),
		"Applications/Discord PTB.app/Contents/Info.plist": plist("COM.HNC.DISCORD", "<key>CFBundleShortVersionString</key><string>3.1</string>"),
	}
	f, found := find(t, New(&stub{}, Options{GOOS: "darwin", FS: fsys}), app(t, discordMac))
	if !found || f.Where != "/Applications/Discord PTB.app" || f.Version != "3.1" {
		t.Fatalf("%v %+v", found, f)
	}
}

func TestMacNaoInstalado(t *testing.T) {
	fsys := fstest.MapFS{"Applications/Outro.app/Contents/Info.plist": plist("com.exemplo.outro", "")}
	if _, found := find(t, New(&stub{}, Options{GOOS: "darwin", FS: fsys, Home: "/Users/p"}), app(t, discordMac)); found {
		t.Fatal("não deveria achar")
	}
	// Arquivo (não pasta) com o nome do bundle não conta.
	fsys = fstest.MapFS{"Applications/Discord.app": &fstest.MapFile{Data: []byte("x")}}
	if _, found := find(t, New(&stub{}, Options{GOOS: "darwin", FS: fsys}), app(t, "      macos: { app_bundle: Discord.app }\n")); found {
		t.Fatal("arquivo não é bundle")
	}
	// Sem FS nada é lido.
	if _, found := find(t, New(&stub{}, Options{GOOS: "darwin"}), app(t, discordMac)); found {
		t.Fatal("sem FS")
	}
}

func TestSemBlocoDeDeteccaoParaOSONaoFazNada(t *testing.T) {
	s := &stub{}
	d := New(s, Options{GOOS: "windows"})
	if _, found := find(t, d, app(t, discordMac)); found || len(s.calls) != 0 {
		t.Fatalf("achou=%v chamadas=%d", found, len(s.calls))
	}
	// App sem detect nenhum.
	c, _ := catalog.Parse("t.yaml", []byte("apps:\n  x:\n    name: X\n    category: dev\n    any: [{ provider: flatpak, id: a.b.C }]\n"))
	a, _ := c.Get("x")
	if _, found := find(t, New(s, Options{GOOS: "darwin"}), a); found {
		t.Fatal("sem detect")
	}
}

const registro = "Git\t2.45.1\r\nDiscord\t1.0.9170\r\n7-Zip 24.08 (x64)\t24.08\r\nGit Extensions\t5.0\r\nMozilla Firefox (x64 pt-BR)\t129.0\r\nDis\x1b[31mcord Falso\t1\r\nProgramação Estúdio\t2.0\r\n"

func winStub(out string) *stub {
	return &stub{handle: func(exec.Cmd) (exec.Result, error) { return ok(out) }}
}

func TestWindowsRegistroUninstall(t *testing.T) {
	s := winStub(registro)
	d := New(s, Options{GOOS: "windows"})
	f, found := find(t, d, app(t, "      windows: { display_name: Discord }\n"))
	if !found || f.Version != "1.0.9170" || !strings.Contains(f.Where, `"Discord"`) {
		t.Fatalf("%v %+v", found, f)
	}
	// O único comando é o PowerShell fixo, só leitura, sem nada do catálogo no script.
	if len(s.calls) != 1 || s.calls[0].Name != `C:\Windows\System32\WindowsPowerShell\v1.0\powershell.exe` {
		t.Fatalf("chamadas: %v", s.calls)
	}
	if strings.Contains(strings.Join(s.calls[0].Args, " "), "Discord") {
		t.Fatal("o nome do catálogo não pode entrar no script")
	}
	s.assertReadOnly(t)
	for _, k := range []string{"HKLM:", "HKCU:", "WOW6432Node"} {
		if !strings.Contains(s.calls[0].Args[3], k) {
			t.Fatalf("o script não cobre %s", k)
		}
	}
}

func TestWindowsCasamentoPorNome(t *testing.T) {
	cases := []struct {
		display, version string
		want             bool
	}{
		{"Git", "2.45.1", true},
		{"7-Zip", "24.08", true},
		{"Mozilla Firefox", "129.0", true},
		{"Programação Estúdio", "2.0", true},
		{"git", "2.45.1", true},
		{"Extensions", "", false},
		{"Dis", "", false},
	}
	for _, tc := range cases {
		f, found := find(t, New(winStub(registro), Options{GOOS: "windows"}), app(t, "      windows: { display_name: \""+tc.display+"\" }\n"))
		if found != tc.want {
			t.Errorf("%q: achou=%v (%+v)", tc.display, found, f)
		}
		if found && tc.version != "" && f.Version != tc.version {
			t.Errorf("%q: versão %q", tc.display, f.Version)
		}
	}
	// "Git" não casa com "Git Extensions" (a entrada só com ela).
	if _, found := find(t, New(winStub("Git Extensions\t5.0\r\n"), Options{GOOS: "windows"}), app(t, "      windows: { display_name: Git }\n")); found {
		t.Fatal("Git Extensions não é Git")
	}
}

func TestWindowsNomeComESCNaSaidaEhSanitizado(t *testing.T) {
	// O display_name do catálogo com ESC é recusado pelo parser; aqui o que importa é a saída do sistema.
	f, found := find(t, New(winStub("Discord (x64)\t1.0\x1b[0m\r\n"), Options{GOOS: "windows"}), app(t, "      windows: { display_name: Discord }\n"))
	if !found || strings.ContainsAny(f.Version+f.Where, "\x1b\x07") {
		t.Fatalf("saída crua: %q %q", f.Where, f.Version)
	}
}

func TestWindowsFalhaDoPowerShellNaoEhDeteccao(t *testing.T) {
	spec := app(t, "      windows: { display_name: Discord }\n")
	for name, h := range map[string]func(exec.Cmd) (exec.Result, error){
		"erro": func(exec.Cmd) (exec.Result, error) { return exec.Result{}, errors.New("boom") },
		"exit": func(exec.Cmd) (exec.Result, error) {
			return exec.Result{ExitCode: 1, Stdout: []byte("Discord\t1")}, nil
		},
		"vazio":    func(exec.Cmd) (exec.Result, error) { return ok("") },
		"sem nome": func(exec.Cmd) (exec.Result, error) { return ok("\t1.0\r\n") },
	} {
		if _, found := find(t, New(&stub{handle: h}, Options{GOOS: "windows"}), spec); found {
			t.Errorf("%s: não deveria achar", name)
		}
	}
}

func TestMatchDisplayName(t *testing.T) {
	cases := []struct {
		reg, want string
		ok        bool
	}{
		{"Discord", "Discord", true},
		{"Python 3.12.7 (64-bit)", "Python", true},
		{"Microsoft Visual Studio Code (User)", "Microsoft Visual Studio Code", true},
		{"VLC media player", "VLC media player", true},
		{"Git Extensions", "Git", false},
		{"Python Launcher", "Python", false},
		{"7-Zip", "7-Zip", true},
		{"(x64)", "", false},
		{"", "Git", false},
	}
	for _, tc := range cases {
		if got := MatchDisplayName(tc.reg, tc.want); got != tc.ok {
			t.Errorf("%q x %q = %v", tc.reg, tc.want, got)
		}
	}
}

const linuxSpec = "      linux: { desktop_id: discord.desktop, flatpak_id: com.discordapp.Discord, snap: discord }\n"

func TestLinuxDesktop(t *testing.T) {
	fsys := fstest.MapFS{"usr/share/applications/discord.desktop": &fstest.MapFile{Data: []byte("[Desktop Entry]")}}
	f, found := find(t, New(&stub{}, Options{GOOS: "linux", FS: fsys}), app(t, linuxSpec))
	if !found || f.Where != "/usr/share/applications/discord.desktop" {
		t.Fatalf("%v %+v", found, f)
	}
	fsys = fstest.MapFS{"home/p/.local/share/applications/discord.desktop": &fstest.MapFile{Data: []byte("x")}}
	f, found = find(t, New(&stub{}, Options{GOOS: "linux", FS: fsys, Home: "/home/p"}), app(t, linuxSpec))
	if !found || f.Where != "/home/p/.local/share/applications/discord.desktop" {
		t.Fatalf("%v %+v", found, f)
	}
}

func TestLinuxFlatpakESnap(t *testing.T) {
	s := &stub{paths: map[string]bool{"flatpak": true, "snap": true}, handle: func(c exec.Cmd) (exec.Result, error) {
		switch c.Name {
		case "flatpak":
			return ok("org.outro.App\t1.0\ncom.discordapp.Discord\t0.0.60\n")
		case "snap":
			return ok("Name     Version  Rev  Tracking       Publisher  Notes\ndiscord  0.0.58   200  latest/stable  snapcrafters  -\n")
		}
		return exec.Result{}, errors.New("inesperado")
	}}
	f, found := find(t, New(s, Options{GOOS: "linux"}), app(t, linuxSpec))
	if !found || f.Where != "flatpak (com.discordapp.Discord)" || f.Version != "0.0.60" {
		t.Fatalf("flatpak: %v %+v", found, f)
	}
	f, found = find(t, New(s, Options{GOOS: "linux"}), app(t, "      linux: { snap: discord }\n"))
	if !found || f.Where != "snap (discord)" || f.Version != "0.0.58" {
		t.Fatalf("snap: %v %+v", found, f)
	}
	s.assertReadOnly(t)
}

func TestLinuxSemFlatpakNemSnapNoPATHNaoExecutaNada(t *testing.T) {
	s := &stub{}
	if _, found := find(t, New(s, Options{GOOS: "linux", FS: fstest.MapFS{}}), app(t, linuxSpec)); found || len(s.calls) != 0 {
		t.Fatalf("achou=%v chamadas=%d", found, len(s.calls))
	}
}

func TestLinuxNaoEncontradoComGerenciadoresPresentes(t *testing.T) {
	s := &stub{paths: map[string]bool{"flatpak": true, "snap": true}, handle: func(c exec.Cmd) (exec.Result, error) {
		if c.Name == "snap" {
			return exec.Result{ExitCode: 1, Stderr: []byte("error: no matching snaps installed")}, nil
		}
		return ok("org.outro.App\t1.0\n")
	}}
	if _, found := find(t, New(s, Options{GOOS: "linux"}), app(t, linuxSpec)); found {
		t.Fatal("não deveria achar")
	}
}

func TestContextoCancelado(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _, err := New(&stub{}, Options{GOOS: "darwin", FS: fstest.MapFS{}}).Find(ctx, app(t, discordMac))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err=%v", err)
	}
}

func TestParseXMLPlistInvalido(t *testing.T) {
	for name, in := range map[string]string{"vazio": "", "lixo": "<<<", "sem dict": "<plist></plist>"} {
		if m, ok := parseXMLPlist([]byte(in)); ok {
			t.Errorf("%s: aceitou %v", name, m)
		}
	}
}

package detect

import (
	"context"
	"io/fs"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"testing/fstest"
	"time"

	"github.com/PiluTech/toolkit/internal/exec"
)

// ---- (d) flatpak e snap por caminho ABSOLUTO ----

func TestFlatpakAndSnapRunByTheAbsolutePathFoundByLookPath(t *testing.T) {
	s := &stub{paths: map[string]bool{"flatpak": true, "snap": true}, handle: func(c exec.Cmd) (exec.Result, error) {
		return ok("com.discordapp.Discord\t0.0.60\n")
	}}
	_, _ = find(t, New(s, Options{GOOS: "linux"}), app(t, linuxSpec))
	_, _ = find(t, New(s, Options{GOOS: "linux"}), app(t, "      linux: { snap: discord }\n"))
	var names []string
	for _, c := range s.calls {
		names = append(names, c.Name)
		if !filepath.IsAbs(c.Name) {
			t.Errorf("o comando deve ir por caminho absoluto: %q", c.Name)
		}
	}
	if strings.Join(names, ",") != "/usr/bin/flatpak,/usr/bin/snap" {
		t.Fatalf("comandos: %v", names)
	}
	s.assertReadOnly(t)
}

type relPathStub struct{ stub }

func (*relPathStub) LookPath(string) (string, error) { return "flatpak", nil } // relativo: não vira caminho absoluto

func TestRelativeLookPathResultFallsBackToTheName(t *testing.T) {
	s := &relPathStub{stub{handle: func(exec.Cmd) (exec.Result, error) { return ok("x\t1\n") }}}
	_, _ = find(t, New(s, Options{GOOS: "linux"}), app(t, "      linux: { flatpak_id: com.discordapp.Discord }\n"))
	if len(s.calls) != 1 || s.calls[0].Name != "flatpak" {
		t.Fatalf("sem caminho absoluto usa o nome: %+v", s.calls)
	}
}

// ---- (b) cache da varredura por bundle_id ----

// countFS conta as aberturas de Info.plist (o custo que a varredura repetida pagava).
type countFS struct {
	fs.FS
	opens *atomic.Int32
}

// Stat não conta como abertura (o fs.Stat genérico abriria o arquivo).
func (c countFS) Stat(name string) (fs.FileInfo, error) { return fs.Stat(c.FS, name) }

func (c countFS) Open(name string) (fs.File, error) {
	if strings.HasSuffix(name, "Info.plist") {
		c.opens.Add(1)
	}
	return c.FS.Open(name)
}

func macFS() fstest.MapFS {
	plist := func(id, ver string) *fstest.MapFile {
		return &fstest.MapFile{Data: []byte(`<?xml version="1.0"?><plist version="1.0"><dict><key>CFBundleIdentifier</key><string>` + id + `</string><key>CFBundleShortVersionString</key><string>` + ver + `</string></dict></plist>`)}
	}
	return fstest.MapFS{
		"Applications/Discord.app/Contents/Info.plist": plist("com.hnc.Discord", "0.0.300"),
		"Applications/Slack.app/Contents/Info.plist":   plist("com.tinyspeck.slackmacgap", "4.0"),
		"Applications/Zoom.app/Contents/Info.plist":    plist("us.zoom.xos", "6.0"),
	}
}

const macSpecID = "      macos: { bundle_id: com.hnc.Discord }\n"

func TestBundleScanIsCachedAcrossQueries(t *testing.T) {
	var opens atomic.Int32
	now := time.Date(2026, 11, 12, 12, 0, 0, 0, time.UTC)
	d := New(&stub{}, Options{GOOS: "darwin", FS: countFS{macFS(), &opens}, Now: func() time.Time { return now }})
	for i := 0; i < 5; i++ { // a TUI pergunta vários apps em seguida
		if f, found := find(t, d, app(t, macSpecID)); !found || f.Where != "/Applications/Discord.app" || f.Version != "0.0.300" {
			t.Fatalf("consulta %d: %v %+v", i, found, f)
		}
	}
	if n := opens.Load(); n != 3 {
		t.Fatalf("cada Info.plist é lido UMA vez por varredura (3 apps), foram %d aberturas", n)
	}
	// outro app (outro bundle_id) reaproveita o mesmo índice
	if _, found := find(t, d, app(t, "      macos: { bundle_id: us.zoom.xos }\n")); !found {
		t.Fatal("o índice serve para qualquer bundle_id")
	}
	if n := opens.Load(); n != 3 {
		t.Fatalf("sem nova varredura: %d", n)
	}
	// depois do TTL varre de novo; Invalidate também
	now = now.Add(cacheTTL + time.Second)
	find(t, d, app(t, macSpecID))
	if n := opens.Load(); n != 6 {
		t.Fatalf("depois do TTL: %d", n)
	}
	d.Invalidate()
	find(t, d, app(t, macSpecID))
	if n := opens.Load(); n != 9 {
		t.Fatalf("depois do Invalidate: %d", n)
	}
}

// O app instalado DEPOIS da varredura em cache só aparece depois do TTL ou do Invalidate (documentado).
func TestInvalidateMakesANewlyInstalledAppVisible(t *testing.T) {
	fsys := macFS()
	delete(fsys, "Applications/Discord.app/Contents/Info.plist")
	d := New(&stub{}, Options{GOOS: "darwin", FS: fsys})
	if _, found := find(t, d, app(t, macSpecID)); found {
		t.Fatal("ainda não instalado")
	}
	fsys["Applications/Discord.app/Contents/Info.plist"] = macFS()["Applications/Discord.app/Contents/Info.plist"]
	if _, found := find(t, d, app(t, macSpecID)); found {
		t.Fatal("o cache ainda vale")
	}
	d.Invalidate()
	if _, found := find(t, d, app(t, macSpecID)); !found {
		t.Fatal("depois do Invalidate o app aparece")
	}
}

func TestCancelledScanIsNotCached(t *testing.T) {
	var opens atomic.Int32
	d := New(&stub{}, Options{GOOS: "darwin", FS: countFS{macFS(), &opens}})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, found, _ := d.Find(ctx, app(t, macSpecID)); found {
		t.Fatal("cancelado")
	}
	if _, found := find(t, d, app(t, macSpecID)); !found {
		t.Fatal("a varredura cancelada não pode ficar em cache vazia")
	}
}

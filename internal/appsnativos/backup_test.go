package appsnativos

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func readFile(t *testing.T, p string) []byte {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func writeFile(t *testing.T, p string, b []byte) {
	t.Helper()
	if err := os.WriteFile(p, b, 0o600); err != nil {
		t.Fatal(err)
	}
}

func goodEntry() Entry {
	return Entry{ID: "package:Microsoft.BingWeather_1.2.3.0_x64__8wekyb3d8bbwe", Kind: "package", App: "bing-weather", Name: "Microsoft.BingWeather",
		PackageFullName: "Microsoft.BingWeather_1.2.3.0_x64__8wekyb3d8bbwe", PackageFamilyName: "Microsoft.BingWeather_8wekyb3d8bbwe",
		Version: "1.2.3.0", Publisher: "CN=Microsoft Corporation", Scope: "user", Date: time.Date(2026, 11, 14, 0, 0, 0, 0, time.UTC)}
}

func TestNewStoreUsaOSIDNoNomeEValida(t *testing.T) {
	st, err := NewStore(t.TempDir(), "S-1-5-21-1-2-3-1001")
	if err != nil || filepath.Base(st.File) != "apps-nativos-S-1-5-21-1-2-3-1001.json" || st.Owner != "S-1-5-21-1-2-3-1001" {
		t.Fatalf("%+v %v", st, err)
	}
	for _, bad := range []string{"", "../x", `S-1-5\..\x`, "S-1-5-21-1-2-3-1001/../../x", "joao"} {
		if _, err := NewStore(t.TempDir(), bad); err == nil {
			t.Errorf("SID %q deveria ser recusado", bad)
		}
	}
}

func TestFileStoreSalvaAtomicoComPermissao0600(t *testing.T) {
	dir := t.TempDir()
	st, _ := NewStore(filepath.Join(dir, "sub"), "S-1-5-21-1-2-3-1001")
	if b, err := st.Load(); b != nil || err != nil {
		t.Fatalf("sem arquivo: %v %v", b, err)
	}
	e := goodEntry()
	if err := st.Save(&Backup{Version: 1, Created: e.Date, Entries: map[string]Entry{e.ID: e}}); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" {
		fi, _ := os.Stat(st.File)
		if fi.Mode().Perm() != 0o600 {
			t.Errorf("permissão = %v", fi.Mode().Perm())
		}
		di, _ := os.Stat(filepath.Dir(st.File))
		if di.Mode().Perm() != 0o700 {
			t.Errorf("permissão da pasta = %v", di.Mode().Perm())
		}
	}
	ents, _ := os.ReadDir(filepath.Dir(st.File))
	if len(ents) != 1 {
		t.Errorf("sobrou temporário: %v", ents)
	}
	b, err := st.Load()
	if err != nil || b.Owner != st.Owner || b.Entries[e.ID] != e {
		t.Errorf("round trip: %+v %v", b, err)
	}
	// regravar substitui sem deixar lixo
	if err := st.Save(b); err != nil {
		t.Fatal(err)
	}
	if ents, _ = os.ReadDir(filepath.Dir(st.File)); len(ents) != 1 {
		t.Errorf("sobrou temporário: %v", ents)
	}
}

func TestFileStoreRecusaDonoErradoVersaoEArquivoRuim(t *testing.T) {
	dir := t.TempDir()
	st, _ := NewStore(dir, "S-1-5-21-1-2-3-1001")
	writeFile(t, st.File, []byte(`{"version":1,"owner":"S-1-5-21-9-9-9-1001","created":"2026-11-14T00:00:00Z","entries":{}}`))
	if _, err := st.Load(); err == nil || !strings.Contains(err.Error(), "outro usuário") {
		t.Errorf("dono errado: %v", err)
	}
	if err := st.Save(&Backup{Version: 1, Entries: map[string]Entry{}}); err == nil {
		t.Error("Save sobre arquivo de outro dono tem de recusar")
	}
	if !strings.Contains(string(readFile(t, st.File)), "9-9-9") {
		t.Error("o arquivo do outro dono não pode ser sobrescrito")
	}
	writeFile(t, st.File, []byte(`{"version":7,"owner":"S-1-5-21-1-2-3-1001"}`))
	if _, err := st.Load(); err == nil {
		t.Error("versão desconhecida tem de falhar")
	}
	writeFile(t, st.File, []byte(`{nao e json`))
	if _, err := st.Load(); err == nil {
		t.Error("arquivo ilegível tem de falhar")
	}
}

func TestValidateBackup(t *testing.T) {
	good := goodEntry()
	mk := func(f func(*Entry)) *Backup {
		e := good
		f(&e)
		return &Backup{Version: 1, Entries: map[string]Entry{e.ID: e}}
	}
	if err := ValidateBackup(&Backup{Version: 1, Entries: map[string]Entry{good.ID: good}}); err != nil {
		t.Fatalf("backup bom: %v", err)
	}
	bad := map[string]*Backup{
		"loja": mk(func(e *Entry) {
			e.Name, e.PackageFullName = "Microsoft.WindowsStore", "Microsoft.WindowsStore_1.0.0.0_x64__8wekyb3d8bbwe"
			e.ID = "package:" + e.PackageFullName
		}),
		"webview2": mk(func(e *Entry) {
			e.Name, e.PackageFullName = "Microsoft.WebView2Runtime", "Microsoft.WebView2Runtime_1.0.0.0_x64__8wekyb3d8bbwe"
			e.ID = "package:" + e.PackageFullName
		}),
		"configurações": mk(func(e *Entry) {
			e.Name, e.PackageFullName = "Windows.ImmersiveControlPanel", "Windows.ImmersiveControlPanel_1.0.0.0_neutral_neutral_cw5n1h2txyewy"
			e.ID = "package:" + e.PackageFullName
		}),
		"app do catálogo, nome de outro": mk(func(e *Entry) { e.Name = "Microsoft.BingNews" }),
		"id não bate com a chave":        mk(func(e *Entry) { e.ID = "package:outro" }),
		"full name de outro pacote":      mk(func(e *Entry) { e.PackageFullName = "Microsoft.WindowsStore_1.0.0.0_x64__8wekyb3d8bbwe" }),
		"injeção no full name": mk(func(e *Entry) {
			e.PackageFullName = "Microsoft.BingWeather_1'; calc"
			e.ID = "package:" + e.PackageFullName
		}),
		"family inválida":                mk(func(e *Entry) { e.PackageFamilyName = "x y" }),
		"versão":                         mk(func(e *Entry) { e.Version = "a b" }),
		"kind":                           mk(func(e *Entry) { e.Kind = "reg" }),
		"escopo":                         mk(func(e *Entry) { e.Scope = "root" }),
		"publisher gigante":              mk(func(e *Entry) { e.Publisher = strings.Repeat("x", 5000) }),
		"provisionado no escopo usuário": mk(func(e *Entry) { e.Kind = "provisioned"; e.ID = "provisioned:" + e.PackageFullName }),
	}
	for name, b := range bad {
		if err := ValidateBackup(b); err == nil || !errorsIs(err, ErrBackupInvalido) {
			t.Errorf("%s: deveria ser inválido: %v", name, err)
		}
	}
}

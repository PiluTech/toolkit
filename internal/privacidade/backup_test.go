package privacidade

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func sampleBackup() *Backup {
	return &Backup{Version: 1, Created: t0, Entries: map[string]Entry{
		"reg:a":  {ID: "reg:a", Kind: "reg", Key: `HKCU\X`, Name: "N", Existed: true, RegKind: RegString, Str: "olá"},
		"svc:b":  {ID: "svc:b", Kind: "service", Service: "DiagTrack", StartType: "automatic", Running: true},
		"task:c": {ID: "task:c", Kind: "task", Task: `\a\b`, Enabled: true},
	}}
}

func TestFileStoreAusenteEhNil(t *testing.T) {
	s := FileStore{File: filepath.Join(t.TempDir(), "sub", "b.json")}
	b, err := s.Load()
	if b != nil || err != nil {
		t.Errorf("Load = %v %v", b, err)
	}
	if s.Path() != s.File {
		t.Error("Path")
	}
	if err := s.Remove(); err != nil {
		t.Errorf("Remove de ausente: %v", err)
	}
}

func TestFileStoreIdaEVolta(t *testing.T) {
	s := FileStore{File: filepath.Join(t.TempDir(), "novo", "dir", "b.json")}
	in := sampleBackup()
	if err := s.Save(in); err != nil {
		t.Fatal(err)
	}
	out, err := s.Load()
	if err != nil {
		t.Fatal(err)
	}
	if out.Version != 1 || !out.Created.Equal(in.Created) || len(out.Entries) != 3 {
		t.Fatalf("%+v", out)
	}
	for id, e := range in.Entries {
		if out.Entries[id] != e {
			t.Errorf("%s: %+v != %+v", id, out.Entries[id], e)
		}
	}
	if err := s.Remove(); err != nil {
		t.Fatal(err)
	}
	if b, _ := s.Load(); b != nil {
		t.Error("Remove não apagou")
	}
}

func TestFileStorePermissoes0600(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("modo Unix")
	}
	dir := filepath.Join(t.TempDir(), "d")
	s := FileStore{File: filepath.Join(dir, "b.json")}
	if err := s.Save(sampleBackup()); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(s.File)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("arquivo %v", fi.Mode().Perm())
	}
	di, _ := os.Stat(dir)
	if di.Mode().Perm() != 0o700 {
		t.Errorf("pasta %v", di.Mode().Perm())
	}
}

func TestFileStoreEscritaAtomicaSemSobras(t *testing.T) {
	dir := t.TempDir()
	s := FileStore{File: filepath.Join(dir, "b.json")}
	if err := s.Save(sampleBackup()); err != nil {
		t.Fatal(err)
	}
	b2 := sampleBackup()
	b2.Entries["extra"] = Entry{ID: "extra", Kind: "reg"}
	if err := s.Save(b2); err != nil {
		t.Fatal(err)
	}
	ents, _ := os.ReadDir(dir)
	if len(ents) != 1 || ents[0].Name() != "b.json" {
		t.Errorf("sobrou temporário: %v", ents)
	}
	if got, _ := s.Load(); len(got.Entries) != 4 {
		t.Errorf("entradas: %d", len(got.Entries))
	}
}

func TestFileStoreFalhaNaoApagaOAnterior(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("modo Unix")
	}
	dir := t.TempDir()
	s := FileStore{File: filepath.Join(dir, "b.json")}
	if err := s.Save(sampleBackup()); err != nil {
		t.Fatal(err)
	}
	// o alvo do rename é uma pasta: o rename falha e o temporário é limpo
	bad := FileStore{File: filepath.Join(dir, "pasta")}
	if err := os.Mkdir(bad.File, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bad.File, "x"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := bad.Save(sampleBackup()); err == nil {
		t.Fatal("esperava erro")
	}
	ents, _ := os.ReadDir(dir)
	for _, e := range ents {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Errorf("temporário sobrou: %s", e.Name())
		}
	}
	if b, err := s.Load(); err != nil || len(b.Entries) != 3 {
		t.Errorf("backup anterior afetado: %v %v", b, err)
	}
}

func TestFileStoreJSONInvalidoEhErroClaro(t *testing.T) {
	f := filepath.Join(t.TempDir(), "b.json")
	for _, content := range []string{"", "{nao e json", "[]", `{"version": "x"}`} {
		if err := os.WriteFile(f, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		b, err := FileStore{File: f}.Load()
		if err == nil || b != nil || !strings.Contains(err.Error(), "ilegível") || !strings.Contains(err.Error(), f) {
			t.Errorf("%q: b=%v err=%v", content, b, err)
		}
	}
}

func TestFileStoreVersaoDesconhecida(t *testing.T) {
	f := filepath.Join(t.TempDir(), "b.json")
	for _, v := range []string{`{"version":2,"entries":{}}`, `{"entries":{}}`} {
		_ = os.WriteFile(f, []byte(v), 0o600)
		if _, err := (FileStore{File: f}).Load(); err == nil || !strings.Contains(err.Error(), "versão desconhecida") {
			t.Errorf("%s: %v", v, err)
		}
	}
}

func TestFileStoreEntriesNuloViraMapa(t *testing.T) {
	f := filepath.Join(t.TempDir(), "b.json")
	_ = os.WriteFile(f, []byte(`{"version":1}`), 0o600)
	b, err := FileStore{File: f}.Load()
	if err != nil || b.Entries == nil {
		t.Fatalf("%v %v", b, err)
	}
	b.Entries["x"] = Entry{} // não pode dar panic
}

func TestFileStoreNaoSegueLinkSimbolico(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink exige privilégio")
	}
	dir := t.TempDir()
	alvo := filepath.Join(dir, "alvo-sensivel.txt")
	if err := os.WriteFile(alvo, []byte("intocado"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "b.json")
	if err := os.Symlink(alvo, link); err != nil {
		t.Fatal(err)
	}
	if err := (FileStore{File: link}).Save(sampleBackup()); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(alvo); string(data) != "intocado" {
		t.Errorf("o Save escreveu através do link: %q", data)
	}
	fi, _ := os.Lstat(link)
	if fi.Mode()&os.ModeSymlink != 0 {
		t.Error("o link deveria ter sido substituído pelo arquivo")
	}
	// Remove também só apaga o link, nunca o alvo
	link2 := filepath.Join(dir, "c.json")
	_ = os.Symlink(alvo, link2)
	if err := (FileStore{File: link2}).Remove(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(alvo); err != nil {
		t.Error("Remove apagou o alvo do link")
	}
}

func TestFileStoreJSONEstavel(t *testing.T) {
	s := FileStore{File: filepath.Join(t.TempDir(), "b.json")}
	_ = s.Save(sampleBackup())
	data, _ := os.ReadFile(s.File)
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil || raw["version"] != float64(1) {
		t.Errorf("%v %v", raw, err)
	}
}

func TestFileStoreComApply(t *testing.T) {
	// integração Apply -> FileStore -> BuildUndo -> ApplyUndo (arquivo real, sistema falso)
	s := FileStore{File: filepath.Join(t.TempDir(), "privacidade-backup.json")}
	f := newFake(EditionPro, true).withAllServicesAndTasks()
	f.regs[rkey(kAdv, nAdv)] = DWORD(1)
	if _, _, err := buildApply(t, f, s, PerfilRecomendado); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(s.File); err != nil {
		t.Fatal(err)
	}
	// reaplicar não sobrescreve o original
	f.regs[rkey(kAdv, nAdv)] = DWORD(5)
	if _, _, err := buildApply(t, f, s, PerfilRecomendado); err != nil {
		t.Fatal(err)
	}
	bk, _ := s.Load()
	if e := bk.Entries[regID(kAdv, nAdv)]; e.DWORD != 1 {
		t.Errorf("original sobrescrito: %+v", e)
	}
	plan, bk2, err := BuildUndo(t.Context(), f, s, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ApplyUndo(t.Context(), f, s, plan, bk2); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(s.File); !os.IsNotExist(err) {
		t.Error("backup deveria ter sido removido")
	}
	if v, _ := f.regValue(kAdv, nAdv); v != DWORD(1) {
		t.Errorf("%+v", v)
	}
}

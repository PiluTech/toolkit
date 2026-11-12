package update

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestSaveRemovesStaleTempFilesButKeepsFreshOnesAndOthers(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "toolkit")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 12, 12, 0, 0, 0, time.UTC)
	mk := func(name string, age time.Duration) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(p, now.Add(-age), now.Add(-age)); err != nil {
			t.Fatal(err)
		}
		return p
	}
	stale1 := mk("update-check.json.123456", time.Hour)
	stale2 := mk("update-check.json.abc", 11*time.Minute)
	fresh := mk("update-check.json.999", time.Minute) // de outro processo vivo
	other := mk("outro-arquivo.json.1", 48*time.Hour) // fora do padrão: nunca mexe
	sub := filepath.Join(dir, "update-check.json.dir")
	if err := os.Mkdir(sub, 0o700); err != nil {
		t.Fatal(err)
	}
	c := &Checker{Path: filepath.Join(dir, cacheFile), Now: func() time.Time { return now }}
	if err := c.save(CacheEntry{}); err != nil {
		t.Fatal(err)
	}
	for _, gone := range []string{stale1, stale2} {
		if _, err := os.Stat(gone); !os.IsNotExist(err) {
			t.Errorf("a sobra velha %s devia ser apagada", filepath.Base(gone))
		}
	}
	for _, kept := range []string{fresh, other, sub, c.Path} {
		if _, err := os.Stat(kept); err != nil {
			t.Errorf("%s não podia ser apagado: %v", filepath.Base(kept), err)
		}
	}
}

func TestSaveTightensAnExistingOpenCacheDirectory(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("modos de Unix")
	}
	dir := filepath.Join(t.TempDir(), "toolkit")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	c := &Checker{Path: filepath.Join(dir, cacheFile), Now: time.Now}
	if err := c.save(CacheEntry{}); err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Stat(dir); fi.Mode().Perm() != 0o700 {
		t.Fatalf("a pasta existente deve ficar 0700, está %v", fi.Mode().Perm())
	}
	if fi, _ := os.Stat(c.Path); fi.Mode().Perm() != 0o600 {
		t.Fatalf("o cache 0600: %v", fi.Mode().Perm())
	}
}

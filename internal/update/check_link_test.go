package update

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// Crivo no #48: a pasta do cache que é um LINK não leva chmod (apertaria o destino, que pode não ser nosso).
func TestSaveDoesNotChmodThroughASymlinkedCacheDirectory(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("modos e links de Unix")
	}
	base := t.TempDir()
	target := filepath.Join(base, "alvo")
	if err := os.Mkdir(target, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(target, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(base, "toolkit")
	if err := os.Symlink(target, link); err != nil {
		t.Skip(err)
	}
	c := &Checker{Path: filepath.Join(link, cacheFile), Now: time.Now}
	if err := c.save(CacheEntry{}); err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Stat(target); fi.Mode().Perm() != 0o755 {
		t.Fatalf("o destino do link não pode ser alterado: %v", fi.Mode().Perm())
	}
}

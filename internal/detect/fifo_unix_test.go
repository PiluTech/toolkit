//go:build !windows

package detect

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"testing/fstest"
	"time"
)

// ---- (a) O_NONBLOCK: a corrida Stat -> Open com um FIFO ----

// swapFS diz "arquivo regular" no Stat e entrega o FIFO real no Open (a troca no meio da corrida).
type swapFS struct {
	fs.FS
	fifo string
}

func (s swapFS) Stat(name string) (fs.FileInfo, error) {
	if strings.HasSuffix(name, "Info.plist") {
		return fstest.MapFS{"x": &fstest.MapFile{Data: []byte("x")}}.Stat("x") // mente: regular
	}
	return fs.Stat(s.FS, name)
}

func (s swapFS) Open(name string) (fs.File, error) {
	if strings.HasSuffix(name, "Info.plist") {
		return OSFS{}.Open(strings.TrimPrefix(s.fifo, "/"))
	}
	return s.FS.Open(name)
}

func TestInfoPlistSwappedForAFIFOAfterTheStatDoesNotBlock(t *testing.T) {
	dir := t.TempDir()
	fifo := filepath.Join(dir, "Info.plist")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Skip("sem mkfifo: " + err.Error())
	}
	d := New(&stub{}, Options{GOOS: "darwin", FS: swapFS{fstest.MapFS{"Applications/X.app/Contents": &fstest.MapFile{Mode: fs.ModeDir}}, fifo}})
	done := make(chan bool, 1)
	go func() {
		_, ok := d.readPlist(context.Background(), "/Applications/X.app")
		done <- ok
	}()
	select {
	case ok := <-done:
		if ok {
			t.Fatal("um FIFO não é Info.plist")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("travou abrindo o FIFO (sem O_NONBLOCK)")
	}
}

// OSFS: abrir um FIFO sem escritor volta na hora, e o descritor aberto mostra que não é arquivo regular.
func TestOSFSOpensAFIFOWithoutBlocking(t *testing.T) {
	fifo := filepath.Join(t.TempDir(), "pipe")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Skip("sem mkfifo: " + err.Error())
	}
	type res struct {
		f   fs.File
		err error
	}
	ch := make(chan res, 1)
	go func() { f, err := OSFS{}.Open(strings.TrimPrefix(fifo, "/")); ch <- res{f, err} }()
	select {
	case r := <-ch:
		if r.err != nil {
			t.Fatal(r.err)
		}
		defer func() { _ = r.f.Close() }()
		if st, err := r.f.Stat(); err != nil || st.Mode().IsRegular() {
			t.Fatalf("o descritor aberto é um FIFO: %v %v", st, err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("OSFS.Open travou num FIFO")
	}
}

func TestOSFSReadsRegularFilesAndRejectsBadPaths(t *testing.T) {
	p := filepath.Join(t.TempDir(), "a.txt")
	if err := os.WriteFile(p, []byte("oi"), 0o600); err != nil {
		t.Fatal(err)
	}
	b, err := fs.ReadFile(OSFS{}, strings.TrimPrefix(p, "/"))
	if err != nil || string(b) != "oi" {
		t.Fatalf("%q %v", b, err)
	}
	for _, bad := range []string{"../etc/passwd", "/abs", "a/../b", ""} {
		if _, err := (OSFS{}).Open(bad); !errors.Is(err, fs.ErrInvalid) {
			t.Errorf("%q: %v", bad, err)
		}
	}
}

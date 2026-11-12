package artifact

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
)

func makeZip(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	names := make([]string, 0, len(files))
	for n := range files {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		w, err := zw.Create(n)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write([]byte(files[n]))
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

type tarEntry struct {
	name, body, link string
	typ              byte
	mode             int64
}

func makeTarGz(t *testing.T, entries ...tarEntry) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, e := range entries {
		typ := e.typ
		if typ == 0 {
			typ = tar.TypeReg
		}
		mode := e.mode
		if mode == 0 {
			mode = 0o644
		}
		h := &tar.Header{Name: e.name, Typeflag: typ, Mode: mode, Linkname: e.link}
		if typ == tar.TypeReg {
			h.Size = int64(len(e.body))
		}
		if err := tw.WriteHeader(h); err != nil {
			t.Fatal(err)
		}
		if typ == tar.TypeReg {
			_, _ = tw.Write([]byte(e.body))
		}
	}
	_ = tw.Close()
	_ = gz.Close()
	return buf.Bytes()
}

func writeFile(t *testing.T, b []byte) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "arq")
	if err := os.WriteFile(p, b, 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestExtractZipOK(t *testing.T) {
	dest := t.TempDir()
	z := makeZip(t, map[string]string{"a/b.txt": "oi", "c.txt": "yo"})
	if err := Extract("zip", writeFile(t, z), dest, DefaultLimits); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(dest, "a", "b.txt")); string(b) != "oi" {
		t.Fatalf("%q", b)
	}
}

func TestExtractTarGzOKEPermissoes(t *testing.T) {
	dest := t.TempDir()
	tg := makeTarGz(t,
		tarEntry{name: "d/", typ: tar.TypeDir, mode: 0o755},
		tarEntry{name: "d/run", body: "#!/bin/sh", mode: 0o4755}, // setuid é descartado
		tarEntry{name: "d/dados", body: "x", mode: 0o666},
	)
	if err := Extract("tar.gz", writeFile(t, tg), dest, DefaultLimits); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" {
		st, _ := os.Stat(filepath.Join(dest, "d", "run"))
		if st.Mode().Perm() != 0o755 || st.Mode()&os.ModeSetuid != 0 {
			t.Fatalf("modo de run = %v", st.Mode())
		}
		st, _ = os.Stat(filepath.Join(dest, "d", "dados"))
		if st.Mode().Perm() != 0o644 {
			t.Fatalf("modo de dados = %v", st.Mode())
		}
	}
}

func TestExtractRecusaEntradasPerigosas(t *testing.T) {
	cases := map[string][]byte{
		"zip ..":         makeZip(t, map[string]string{"../fora": "x"}),
		"zip .. no meio": makeZip(t, map[string]string{"a/../../fora": "x"}),
		"zip absoluto":   makeZip(t, map[string]string{"/etc/x": "x"}),
		"zip volume":     makeZip(t, map[string]string{"C:/x": "x"}),
		"zip barra inv.": makeZip(t, map[string]string{"a\\..\\x": "x"}),
		"zip NUL":        makeZip(t, map[string]string{"a\x00b": "x"}),
	}
	for name, z := range cases {
		dest := t.TempDir()
		if err := Extract("zip", writeFile(t, z), dest, DefaultLimits); err == nil {
			t.Errorf("%s: esperava erro", name)
		}
		parent := filepath.Dir(dest)
		if _, err := os.Stat(filepath.Join(parent, "fora")); err == nil {
			t.Errorf("%s: escreveu fora do destino", name)
		}
	}
	tcases := map[string][]byte{
		"tar ..":        makeTarGz(t, tarEntry{name: "../fora", body: "x"}),
		"tar abs":       makeTarGz(t, tarEntry{name: "/tmp/x", body: "x"}),
		"tar symlink":   makeTarGz(t, tarEntry{name: "l", typ: tar.TypeSymlink, link: "/etc/passwd"}),
		"tar hardlink":  makeTarGz(t, tarEntry{name: "l", typ: tar.TypeLink, link: "x"}),
		"tar fifo":      makeTarGz(t, tarEntry{name: "f", typ: tar.TypeFifo}),
		"tar duplicado": makeTarGz(t, tarEntry{name: "a", body: "1"}, tarEntry{name: "a", body: "2"}),
	}
	for name, tg := range tcases {
		if err := Extract("tar.gz", writeFile(t, tg), t.TempDir(), DefaultLimits); err == nil {
			t.Errorf("%s: esperava erro", name)
		}
	}
}

func TestExtractLimites(t *testing.T) {
	z := makeZip(t, map[string]string{"a": strings.Repeat("x", 100), "b": strings.Repeat("y", 100)})
	if err := Extract("zip", writeFile(t, z), t.TempDir(), Limits{MaxFiles: 10, MaxTotal: 150}); err == nil {
		t.Error("tamanho total: esperava erro")
	}
	if err := Extract("zip", writeFile(t, z), t.TempDir(), Limits{MaxFiles: 1, MaxTotal: 1 << 20}); err == nil {
		t.Error("número de arquivos: esperava erro")
	}
	tg := makeTarGz(t, tarEntry{name: "a", body: strings.Repeat("x", 100)}, tarEntry{name: "b", body: strings.Repeat("y", 100)})
	if err := Extract("tar.gz", writeFile(t, tg), t.TempDir(), Limits{MaxFiles: 10, MaxTotal: 150}); err == nil {
		t.Error("tar tamanho total: esperava erro")
	}
}

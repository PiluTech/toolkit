package artifact

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// Limits protege contra bombas de descompressão.
type Limits struct {
	MaxFiles int   // número máximo de entradas
	MaxTotal int64 // bytes descompactados no total
}

// DefaultLimits permite 20 mil entradas e 4 GiB descompactados.
var DefaultLimits = Limits{MaxFiles: 20000, MaxTotal: 4 << 30}

// Extract extrai um arquivo "zip" ou "tar.gz" (já com o hash conferido) em dest, que deve existir e estar vazio.
// Só entram diretórios e arquivos regulares: caminho absoluto, com volume, com barra invertida, NUL ou ".."
// é erro; symlink, hardlink, dispositivo e FIFO também; entrada repetida também. Setuid, setgid e sticky são
// descartados (modo final 0644 ou 0755). O total descompactado é medido pelo que é realmente lido, não pelo
// que o cabeçalho declara.
func Extract(kind, archive, dest string, lim Limits) error {
	switch kind {
	case "zip":
		return extractZip(archive, dest, lim)
	case "tar.gz":
		return extractTarGz(archive, dest, lim)
	}
	return fmt.Errorf("tipo de arquivo compactado %q não suportado", kind)
}

// entryPath valida o nome de uma entrada e devolve o caminho dentro de dest.
func entryPath(dest, name string) (string, error) {
	if name == "" || strings.ContainsAny(name, "\\\x00") || strings.HasPrefix(name, "/") || (len(name) >= 2 && name[1] == ':') {
		return "", fmt.Errorf("entrada %q com caminho não permitido", printableName(name))
	}
	clean := path.Clean(name)
	for _, seg := range strings.Split(name, "/") {
		if seg == ".." {
			return "", fmt.Errorf("entrada %q sai do diretório de destino", printableName(name))
		}
	}
	if clean == "." {
		return "", nil
	}
	return filepath.Join(dest, filepath.FromSlash(clean)), nil
}

func printableName(s string) string {
	return strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return '?'
		}
		return r
	}, s)
}

func fileMode(exec bool) os.FileMode {
	if exec {
		return 0o755
	}
	return 0o644
}

// writeEntry grava r em p (criação exclusiva) lendo no máximo remaining+1 bytes e devolve quantos leu.
func writeEntry(p string, r io.Reader, mode os.FileMode, remaining int64) (int64, error) {
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return 0, err
	}
	f, err := os.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return 0, err
	}
	n, err := io.Copy(f, io.LimitReader(r, remaining+1))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil && n > remaining {
		err = errors.New("o conteúdo descompactado passa do limite")
	}
	if err == nil {
		err = os.Chmod(p, mode) // exato, sem depender do umask
	}
	return n, err
}

func extractZip(archive, dest string, lim Limits) error {
	zr, err := zip.OpenReader(archive)
	if err != nil {
		return fmt.Errorf("abrindo zip: %w", err)
	}
	defer func() { _ = zr.Close() }()
	if len(zr.File) > lim.MaxFiles {
		return fmt.Errorf("zip com %d entradas (limite %d)", len(zr.File), lim.MaxFiles)
	}
	var total int64
	for _, f := range zr.File {
		p, err := entryPath(dest, f.Name)
		if err != nil {
			return err
		}
		if p == "" {
			continue
		}
		mode := f.Mode()
		switch {
		case mode.IsDir() || strings.HasSuffix(f.Name, "/"):
			if err := os.MkdirAll(p, 0o755); err != nil {
				return err
			}
		case mode.IsRegular():
			rc, err := f.Open()
			if err != nil {
				return err
			}
			n, err := writeEntry(p, rc, fileMode(mode.Perm()&0o111 != 0), lim.MaxTotal-total)
			_ = rc.Close()
			if err != nil {
				return fmt.Errorf("entrada %q: %w", printableName(f.Name), err)
			}
			total += n
		default:
			return fmt.Errorf("entrada %q não é arquivo nem diretório (link ou dispositivo)", printableName(f.Name))
		}
	}
	return nil
}

func extractTarGz(archive, dest string, lim Limits) error {
	f, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("abrindo tar.gz: %w", err)
	}
	defer func() { _ = gz.Close() }()
	tr := tar.NewReader(gz)
	var total int64
	count := 0
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("lendo tar: %w", err)
		}
		if h.Typeflag == tar.TypeXGlobalHeader {
			continue
		}
		if count++; count > lim.MaxFiles {
			return fmt.Errorf("tar com mais de %d entradas", lim.MaxFiles)
		}
		p, err := entryPath(dest, h.Name)
		if err != nil {
			return err
		}
		if p == "" {
			continue
		}
		switch h.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(p, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			n, err := writeEntry(p, tr, fileMode(h.Mode&0o111 != 0), lim.MaxTotal-total)
			if err != nil {
				return fmt.Errorf("entrada %q: %w", printableName(h.Name), err)
			}
			total += n
		default:
			return fmt.Errorf("entrada %q não é arquivo nem diretório (link ou dispositivo)", printableName(h.Name))
		}
	}
}

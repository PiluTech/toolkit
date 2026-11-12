package resume

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// FS é a parte do disco que os registradores de Windows e macOS usam (arquivos do usuário). Injetável:
// os testes usam um FS em memória e nada toca o disco real.
type FS interface {
	MkdirAll(path string, perm fs.FileMode) error
	WriteFile(path string, data []byte, perm fs.FileMode) error
	CopyFile(src, dst string, perm fs.FileMode) error
	Remove(path string) error // inexistente = sucesso
	Exists(path string) bool
}

// OSFS é o FS real.
type OSFS struct{}

// MkdirAll cria o diretório.
func (OSFS) MkdirAll(p string, perm fs.FileMode) error { return os.MkdirAll(p, perm) }

// WriteFile grava de forma atômica (tmp + rename), recusando destino que seja link simbólico.
func (OSFS) WriteFile(p string, data []byte, perm fs.FileMode) error {
	if fi, err := os.Lstat(p); err == nil && !fi.Mode().IsRegular() {
		return fmt.Errorf("%s: não é arquivo regular", p)
	}
	tmp, err := os.CreateTemp(filepath.Dir(p), ".toolkit-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(name)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(name)
		return err
	}
	if err := os.Chmod(name, perm); err != nil {
		_ = os.Remove(name)
		return err
	}
	if err := os.Rename(name, p); err != nil {
		_ = os.Remove(name)
		return err
	}
	return nil
}

// CopyFile copia src para dst (atômico, perm no destino); src tem de ser arquivo regular.
func (o OSFS) CopyFile(src, dst string, perm fs.FileMode) error {
	in, err := os.Open(src) //nolint:gosec // o chamador passa o binário em execução
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	fi, err := in.Stat()
	if err != nil {
		return err
	}
	if !fi.Mode().IsRegular() {
		return fmt.Errorf("%s: não é arquivo regular", src)
	}
	data, err := io.ReadAll(in)
	if err != nil {
		return err
	}
	return o.WriteFile(dst, data, perm)
}

// Remove apaga o arquivo; inexistente é sucesso.
func (OSFS) Remove(p string) error {
	if err := os.Remove(p); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

// Exists diz se o caminho existe.
func (OSFS) Exists(p string) bool {
	_, err := os.Lstat(p)
	return err == nil
}

var argRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._=-]*$|^--?[A-Za-z0-9][A-Za-z0-9._=-]*$`)

// ValidArgs recusa argumento que não seja palavra ou flag simples: o Spec só leva `resume` e flags, e
// isso evita qualquer injeção nas linhas de comando, unit e plist montadas a partir dele.
func ValidArgs(args []string) error {
	if len(args) == 0 || args[0] != "resume" {
		return errors.New("o primeiro argumento da retomada deve ser \"resume\"")
	}
	for _, a := range args {
		if !argRe.MatchString(a) {
			return fmt.Errorf("argumento de retomada inválido: %q", a)
		}
	}
	return nil
}

// validPath recusa caminho vazio, com controle ou com caracteres que têm sentido especial em unit systemd
// (espaço, %, $, aspas, barra invertida em caminho Unix) quando strict.
func validPath(p string, strict bool) error {
	if p == "" {
		return errors.New("caminho vazio")
	}
	for _, r := range p {
		if r < 0x20 || r == 0x7f {
			return fmt.Errorf("caminho com caractere de controle: %q", p)
		}
	}
	if strict && strings.ContainsAny(p, " \t%$\"'\\") {
		return fmt.Errorf("caminho com espaço ou caractere especial não é aceito na retomada: %q", p)
	}
	return nil
}

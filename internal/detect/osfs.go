package detect

import (
	"io/fs"
	"os"
	"syscall"
)

// OSFS é o sistema de arquivos real a partir da raiz "/" (os caminhos são absolutos sem a barra inicial, como no
// Options.FS). Diferente do os.DirFS, o Open usa O_NONBLOCK: abrir um FIFO para leitura NÃO bloqueia (sem escritor,
// o open comum espera para sempre). Com isso a corrida entre o Stat e o Open (alguém troca o Info.plist por um FIFO
// no meio) não trava o toolkit: quem abre confere o tipo no descritor aberto (File.Stat = fstat) e recusa o que
// não for arquivo regular.
type OSFS struct{}

// Open abre name (relativo à raiz "/") só para leitura e sem bloquear.
func (OSFS) Open(name string) (fs.File, error) {
	if !fs.ValidPath(name) {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrInvalid}
	}
	f, err := os.OpenFile("/"+name, os.O_RDONLY|syscall.O_NONBLOCK, 0) //nolint:gosec // leitura, caminho validado
	if err != nil {
		return nil, err
	}
	return f, nil
}

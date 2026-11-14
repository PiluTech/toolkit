package animacoes

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"

	"github.com/PiluTech/toolkit/internal/privacidade"
)

// Erros do pacote (os de backup são os da privacidade, para o chamador tratar igual).
var (
	// ErrNoBackup: não há nada a desfazer.
	ErrNoBackup = errors.New("não há backup de animações para desfazer")
	// ErrBackupOutroDono: o backup pertence a outro usuário. Nada foi alterado.
	ErrBackupOutroDono = privacidade.ErrBackupOutroDono
	// ErrBackupInvalido: o backup tem uma entrada que o catálogo não produziria. Nada foi alterado e o arquivo foi preservado.
	ErrBackupInvalido = errors.New("backup de animações com entrada fora do catálogo")
)

// SIDer é implementado pelo System que sabe o SID do usuário do processo. Interface opcional: o núcleo FALHA FECHADO
// quando ela não existe.
type SIDer interface {
	UserSID(ctx context.Context) (string, error)
}

var reSID = regexp.MustCompile(`^S-1-[0-9]{1,12}(-[0-9]{1,12}){1,15}$`)

// UserSID devolve o SID do usuário que roda o toolkit.
func UserSID(ctx context.Context, sys System) (string, error) {
	s, ok := sys.(SIDer)
	if !ok {
		return "", errors.New("este System não informa o SID do usuário; o backup por usuário não pode ser separado")
	}
	sid, err := s.UserSID(ctx)
	if err != nil {
		return "", err
	}
	if !reSID.MatchString(sid) {
		return "", errors.New("SID do usuário em formato inesperado")
	}
	return sid, nil
}

// NewStore monta o arquivo de backup (JSON 0600, escrita atômica, dono gravado e conferido) do usuário. O SID vira parte
// do nome do arquivo, por isso é validado.
func NewStore(dir, sid string) (privacidade.FileStore, error) {
	if !reSID.MatchString(sid) {
		return privacidade.FileStore{}, errors.New("SID do usuário inválido")
	}
	return privacidade.FileStore{File: filepath.Join(dir, "otimizacao-animacoes-backup-"+sid+".json"), Owner: sid}, nil
}

func regID(key, name string) string { return "reg:" + key + "|" + name }

// catalogIndex indexa os alvos do catálogo pelo ID do backup.
func catalogIndex() map[string]Item {
	ix := map[string]Item{}
	for _, it := range Items() {
		ix[regID(it.Key, it.Name)] = it
	}
	return ix
}

func validKind(k privacidade.RegKind) bool {
	return k == privacidade.RegDWORD || k == privacidade.RegString || k == privacidade.RegExpandString
}

// validate confere TODAS as entradas do backup contra o catálogo (chave do mapa, ID, tipo, chave e nome idênticos a um
// item do catálogo; tipo de valor conhecido). Roda ANTES de ler ou escrever no sistema: o backup é dado não confiável.
func validate(bk *privacidade.Backup) error {
	ix := catalogIndex()
	for id, e := range bk.Entries {
		bad := func(format string, a ...any) error {
			return fmt.Errorf("%w: %q: %s", ErrBackupInvalido, printableID(id), fmt.Sprintf(format, a...))
		}
		if e.ID != id {
			return bad("o ID da entrada não confere com a chave")
		}
		if e.Kind != "reg" {
			return bad("tipo de entrada desconhecido")
		}
		it, ok := ix[id]
		if !ok || it.Key != e.Key || it.Name != e.Name {
			return bad("valor de registro que o toolkit não altera")
		}
		if e.Existed && !validKind(e.RegKind) {
			return bad("tipo de valor desconhecido (%d)", int(e.RegKind))
		}
	}
	return nil
}

func printableID(s string) string {
	r := []rune(s)
	if len(r) > 80 {
		r = append(r[:80], '…')
	}
	for i, c := range r {
		if c < 0x20 || c == 0x7f {
			r[i] = '?'
		}
	}
	return string(r)
}

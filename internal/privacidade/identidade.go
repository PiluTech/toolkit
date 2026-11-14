package privacidade

import (
	"context"
	"errors"
	"fmt"
	"regexp"
)

// SIDer é implementado pelo System que sabe o SID do usuário do processo (o windowsSystem real; o fake dos testes).
// Interface opcional: o núcleo a usa por type assertion e FALHA FECHADO quando ela não existe.
type SIDer interface {
	UserSID(ctx context.Context) (string, error)
}

// ErrBackupOutroDono indica que o backup pertence a outro usuário (ou a outro escopo). Nada foi alterado.
var ErrBackupOutroDono = errors.New("o backup de privacidade pertence a outro usuário")

// reSID aceita só o formato S-1-<autoridade>-<subautoridades>: é o que entra no nome do arquivo de backup.
var reSID = regexp.MustCompile(`^S-1-[0-9]{1,12}(-[0-9]{1,12}){1,15}$`)

func validSID(s string) bool { return reSID.MatchString(s) }

// UserSID devolve o SID do usuário que roda o toolkit. Sem suporte do System ou com saída inválida é erro.
func UserSID(ctx context.Context, sys System) (string, error) {
	s, ok := sys.(SIDer)
	if !ok {
		return "", errors.New("este System não informa o SID do usuário; o backup por usuário não pode ser separado")
	}
	sid, err := s.UserSID(ctx)
	if err != nil {
		return "", err
	}
	if !validSID(sid) {
		return "", fmt.Errorf("SID do usuário em formato inesperado")
	}
	return sid, nil
}

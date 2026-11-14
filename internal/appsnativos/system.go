package appsnativos

import (
	"context"
	"errors"
	"fmt"
	"regexp"
)

// Package é um pacote AppX/MSIX instalado, como o Get-AppxPackage o descreve.
type Package struct {
	Name              string
	PackageFullName   string
	PackageFamilyName string
	Version           string
	Publisher         string
	IsFramework       bool
	// SignatureKind: "None", "Developer", "Enterprise", "Store" ou "System".
	SignatureKind string
	NonRemovable  bool
}

// Provisioned é um pacote provisionado na imagem do Windows (instalado automaticamente para todo usuário novo).
type Provisioned struct {
	// DisplayName é o nome do pacote (o mesmo Name do Get-AppxPackage).
	DisplayName string
	PackageName string
	Version     string
}

// System é o Windows visto pelo toolkit. As leituras nunca exigem administrador, exceto ListInstalled com allUsers e
// ListProvisioned; as remoções para todos os usuários e do provisionamento exigem administrador (a implementação real as
// envia com Elevate e o Runner recusa quando o processo não está elevado: exec.ErrNotElevated).
type System interface {
	// IsAdmin diz se o processo está elevado.
	IsAdmin(ctx context.Context) bool
	// ListInstalled lista os pacotes do usuário atual (allUsers=false) ou de todos os usuários (allUsers=true; só administrador).
	ListInstalled(ctx context.Context, allUsers bool) ([]Package, error)
	// ListProvisioned lista os pacotes provisionados na imagem (só administrador).
	ListProvisioned(ctx context.Context) ([]Provisioned, error)
	// RemoveForUser remove o pacote só do usuário atual.
	RemoveForUser(ctx context.Context, packageFullName string) error
	// RemoveAllUsers remove o pacote de todos os usuários (administrador).
	RemoveAllUsers(ctx context.Context, packageFullName string) error
	// RemoveProvisioned tira o pacote da imagem, para não voltar em usuário novo (administrador).
	RemoveProvisioned(ctx context.Context, packageName string) error
}

// SIDer é implementado pelo System que sabe o SID do usuário do processo (o windowsSystem real; o fake dos testes).
// Interface opcional: o núcleo a usa por type assertion e FALHA FECHADO quando ela não existe.
type SIDer interface {
	UserSID(ctx context.Context) (string, error)
}

// Erros do pacote.
var (
	// ErrNeedsAdmin: o escopo escolhido exige administrador e o processo não está elevado. Nada foi alterado.
	ErrNeedsAdmin = errors.New("remover para todos os usuários e do provisionamento exige administrador; abra o terminal como administrador")
	// ErrItemInvalido: um item do plano, ou do backup, não é algo que o catálogo removeria. Nada foi alterado.
	ErrItemInvalido = errors.New("item fora do catálogo de apps nativos ou protegido")
	// ErrBackupOutroDono: o backup pertence a outro usuário. Nada foi alterado.
	ErrBackupOutroDono = errors.New("o backup de apps nativos pertence a outro usuário")
	// ErrBackupInvalido: o backup tem uma entrada que o catálogo não produziria (adulterado ou de outra versão).
	ErrBackupInvalido = errors.New("backup de apps nativos com entrada fora do catálogo")
	// ErrNoBackup: não há backup para consultar.
	ErrNoBackup = errors.New("não há backup de apps nativos removidos")
)

var (
	reSID      = regexp.MustCompile(`^S-1-[0-9]{1,12}(-[0-9]{1,12}){1,15}$`)
	reFullName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._~-]{0,250}$`)
	reFamily   = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9.]{0,100}_[a-z0-9]{13}$`)
	reVersion  = regexp.MustCompile(`^[0-9][0-9.]{0,50}$`)
)

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

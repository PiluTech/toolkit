package appsnativos

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"
)

// Tipos de entrada e de escopo gravados no backup.
const (
	kindPackage     = "package"
	kindProvisioned = "provisioned"
	scopeUser       = "user"
	scopeTodos      = "todos"

	maxPublisher = 300
)

// Entry é a identidade de UM pacote removido, guardada antes da remoção. Não restaura nada sozinha: só alimenta
// `toolkit apps-nativos reinstalar`, que IMPRIME como reinstalar.
type Entry struct {
	ID                string    `json:"id"`   // "package:<PackageFullName>" ou "provisioned:<PackageName>"
	Kind              string    `json:"kind"` // "package" ou "provisioned"
	App               string    `json:"app"`  // id do catálogo
	Name              string    `json:"name"`
	PackageFullName   string    `json:"package_full_name"`
	PackageFamilyName string    `json:"package_family_name,omitempty"`
	Version           string    `json:"version"`
	Publisher         string    `json:"publisher,omitempty"`
	Scope             string    `json:"scope"` // "user" ou "todos"
	Date              time.Time `json:"date"`
}

// Backup reúne as entradas dos pacotes removidos. O primeiro registro de cada pacote vence.
type Backup struct {
	Version int `json:"version"`
	// Owner é o SID do usuário que removeu.
	Owner   string           `json:"owner,omitempty"`
	Created time.Time        `json:"created"`
	Entries map[string]Entry `json:"entries"`
}

// Store guarda o backup (nil, nil = ainda não existe).
type Store interface {
	Load() (*Backup, error)
	Save(*Backup) error
	Path() string
}

// FileStore guarda o backup num arquivo JSON (0600) por escrita atômica (temporário + rename). Com Owner, o arquivo
// só é aceito se o dono gravado dentro dele for o mesmo (ErrBackupOutroDono) e o Save grava o dono.
type FileStore struct {
	File  string
	Owner string
}

// NewStore monta o Store do usuário: `apps-nativos-<SID>.json` na pasta do estado. O SID é validado (vira parte do nome).
func NewStore(dir, sid string) (FileStore, error) {
	if !validSID(sid) {
		return FileStore{}, errors.New("SID do usuário inválido")
	}
	return FileStore{File: filepath.Join(dir, "apps-nativos-"+sid+".json"), Owner: sid}, nil
}

// Path devolve o caminho do arquivo.
func (s FileStore) Path() string { return s.File }

// Load lê o backup; arquivo ausente = nil, nil. Arquivo ilegível, de versão desconhecida ou de outro dono é erro.
func (s FileStore) Load() (*Backup, error) {
	data, err := os.ReadFile(s.File)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var b Backup
	if err := json.Unmarshal(data, &b); err != nil {
		return nil, fmt.Errorf("o backup %s está ilegível: %w", s.File, err)
	}
	if b.Version != 1 {
		return nil, fmt.Errorf("o backup %s é de uma versão desconhecida (%d)", s.File, b.Version)
	}
	if s.Owner != "" && b.Owner != s.Owner {
		return nil, fmt.Errorf("%w (%s)", ErrBackupOutroDono, s.File)
	}
	if b.Entries == nil {
		b.Entries = map[string]Entry{}
	}
	return &b, nil
}

// Save grava o backup. Com Owner, recusa sobrescrever o arquivo de outro dono.
func (s FileStore) Save(b *Backup) error {
	if s.Owner != "" {
		if _, err := s.Load(); err != nil {
			return err
		}
		cp := *b
		cp.Owner = s.Owner
		b = &cp
	}
	if err := os.MkdirAll(filepath.Dir(s.File), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(b, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.File), ".apps-nativos-*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer func() { _ = os.Remove(name) }() // some se o rename deu certo; limpa se algo falhou
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(name, s.File)
}

// identity descreve um pacote para a validação (usada nos itens do plano e nas entradas do backup).
type identity struct {
	app, name, full, family, version, publisher string
	kind, scope                                 string
	isFramework, nonRemovable                   bool
	signatureKind                               string
}

// checkIdentity confere uma identidade contra o catálogo e a lista de negação: o app existe, o Name é EXATAMENTE o dele,
// não é protegido, os campos têm o formato fechado e o nome completo começa pelo Name.
func checkIdentity(id identity) error {
	bad := func(format string, a ...any) error {
		return fmt.Errorf("%w: %s", ErrItemInvalido, fmt.Sprintf(format, a...))
	}
	app, ok := Find(id.app)
	if !ok {
		return bad("app %q", printableShort(id.app))
	}
	if id.name != app.Name || !ValidName(id.name) {
		return bad("o pacote %q não é o de %s", printableShort(id.name), app.ID)
	}
	if Protected(id.name) {
		return bad("%s é protegido", id.name)
	}
	if id.kind != kindPackage && id.kind != kindProvisioned {
		return bad("tipo %q", printableShort(id.kind))
	}
	if id.scope != scopeUser && id.scope != scopeTodos {
		return bad("escopo %q", printableShort(id.scope))
	}
	if id.kind == kindProvisioned && id.scope != scopeTodos {
		return bad("o provisionamento só sai no escopo de todos os usuários")
	}
	if !reFullName.MatchString(id.full) || !strings.HasPrefix(id.full, id.name+"_") {
		return bad("nome completo %q não é de %s", printableShort(id.full), id.name)
	}
	if id.family == "" && id.kind == kindPackage {
		return bad("sem família de pacote")
	}
	if id.family != "" && (!reFamily.MatchString(id.family) || !strings.HasPrefix(id.family, id.name+"_")) {
		return bad("família %q não é de %s", printableShort(id.family), id.name)
	}
	if !reVersion.MatchString(id.version) {
		return bad("versão %q", printableShort(id.version))
	}
	if len([]rune(id.publisher)) > maxPublisher {
		return bad("publisher longo demais")
	}
	for _, r := range id.publisher {
		if unicode.IsControl(r) {
			return bad("publisher com caractere de controle")
		}
	}
	if id.isFramework || id.nonRemovable || strings.EqualFold(id.signatureKind, "System") {
		return bad("%s é framework, não removível ou do sistema", id.name)
	}
	return nil
}

// ValidateBackup confere TODAS as entradas contra o catálogo (identidade) antes de qualquer uso: um backup é dado não
// confiável. Entrada fora do catálogo, protegida ou fora do formato é ErrBackupInvalido.
func ValidateBackup(b *Backup) error {
	for id, e := range b.Entries {
		if err := validateEntry(id, e); err != nil {
			return fmt.Errorf("%w: %q: %s", ErrBackupInvalido, printableShort(id), strings.TrimPrefix(err.Error(), ErrItemInvalido.Error()+": "))
		}
	}
	return nil
}

func validateEntry(id string, e Entry) error {
	if e.ID != id || id != e.Kind+":"+e.PackageFullName {
		return fmt.Errorf("%w: o ID da entrada não confere", ErrItemInvalido)
	}
	return checkIdentity(identity{app: e.App, name: e.Name, full: e.PackageFullName, family: e.PackageFamilyName,
		version: e.Version, publisher: e.Publisher, kind: e.Kind, scope: e.Scope})
}

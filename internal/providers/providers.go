// Package providers define o contrato dos gerenciadores de pacote (winget, brew, apt, dnf, pacman,
// flatpak...) e a escolha do instalador de cada app. Cada provider só monta exec.Cmd e interpreta a
// saída: nada aqui executa processo fora do exec.Runner.
package providers

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/PiluVitu/toolkit/internal/catalog"
	"github.com/PiluVitu/toolkit/internal/tmpl"
)

// Erros tipados. Os providers devolvem estes (embrulhados com %w) para o chamador decidir.
var (
	// ErrNotFound: o gerenciador não conhece o pacote.
	ErrNotFound = errors.New("pacote não encontrado")
	// ErrNoApplicableInstaller: o pacote existe mas não há instalador para esta máquina; o orquestrador tenta o próximo.
	ErrNoApplicableInstaller = errors.New("nenhum instalador aplicável nesta máquina")
	// ErrHashMismatch: o hash do instalador não confere. Nunca retenta nem ignora a verificação.
	ErrHashMismatch = errors.New("o hash do instalador não confere")
	// ErrNeedsAdmin: a operação exige privilégio de administrador.
	ErrNeedsAdmin = errors.New("a operação exige privilégio de administrador")
	// ErrRetryable: falha transitória (pacote em uso, outra instalação em andamento).
	ErrRetryable = errors.New("falha transitória, tente de novo")
	// ErrVersionUnsupported: o gerenciador não instala a versão pedida (brew, pacman e flatpak só têm a atual).
	ErrVersionUnsupported = errors.New("o gerenciador não permite escolher esta versão")
	// ErrInvalidRef: campo do instalador inválido para o provider (charset, "-" inicial, campo que não é dele).
	ErrInvalidRef = errors.New("instalador inválido")
)

// PackageRef é UMA entrada de instalação do catálogo, com os templates {{...}} já resolvidos.
// Campos que o provider não usa ficam vazios; Provider.Validate recusa os que não são dele.
type PackageRef struct {
	AppID    string
	Provider string
	ID       string // winget id / flatpak app id
	Package  string // apt, dnf, pacman
	Formula  string // brew
	Cask     string // brew
	Source   string // winget --source / flatpak remote
	Repo     string // github-release
	Asset    string // github-release
	URL      string // url
	SHA256   string // url (obrigatório) / github-release (opcional)
	Type     string
	Args     []string
	Line     int // linha no YAML, para mensagens de erro
}

// InstallResult substitui o "error" puro: instalar pode dar certo E pedir reboot.
type InstallResult struct {
	AlreadyInstalled bool   // nada foi feito (idempotência)
	RebootRequired   bool   // winget 0x8A150109, msi/dism 3010
	Version          string // versão que ficou instalada, se o provider souber
}

// Provider é um gerenciador de pacote.
type Provider interface {
	Name() string
	// Available diz se o gerenciador existe na máquina (via Runner.LookPath, então é mockável).
	Available(ctx context.Context) bool
	// Validate checa os campos do PackageRef (charset, "-" inicial, campos alheios) antes de qualquer comando.
	Validate(ref PackageRef) error
	// IsInstalled lê (ReadOnly) se o pacote está instalado e em que versão.
	IsInstalled(ctx context.Context, ref PackageRef) (bool, string, error)
	// ListVersions devolve as versões instaláveis; ["latest"] se o gerenciador não lista.
	ListVersions(ctx context.Context, ref PackageRef) ([]string, error)
	// Install instala. version vazia = a padrão. Versão que o gerenciador não permite = ErrVersionUnsupported
	// (nunca instala a última em silêncio).
	Install(ctx context.Context, ref PackageRef, version string) (InstallResult, error)
}

// Preparer é opcional: prepara o gerenciador uma vez por execução (brew update, apt-get update). O provider
// guarda o estado "já preparado" na própria instância. Conta como mutação (não é ReadOnly).
type Preparer interface {
	Prepare(ctx context.Context) error
}

// Latest é a única "versão" dos gerenciadores que não listam versões.
const Latest = "latest"

var (
	nameRe    = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+@/-]*$`)
	versionRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+~:-]*$`)
)

// MaxFieldLen limita cada campo que vira argumento de comando.
const MaxFieldLen = 200

// CheckName valida um nome de pacote, id ou remote que vira argumento de comando (inclusive sob sudo):
// começa por letra ou número (nunca "-", que seria lido como opção), só letras, números e . _ + @ / -,
// sem "..", até MaxFieldLen. O "--" antes do argumento é uma segunda camada, não a única.
func CheckName(field, s string) error {
	if s == "" {
		return fmt.Errorf("%w: %q vazio", ErrInvalidRef, field)
	}
	if len(s) > MaxFieldLen || !nameRe.MatchString(s) || strings.Contains(s, "..") {
		return fmt.Errorf("%w: %s %q com caracteres não permitidos", ErrInvalidRef, field, s)
	}
	return nil
}

// ValidVersion valida a versão que o usuário pede ou que um gerenciador devolve. Aceita ":" depois do
// primeiro caractere (época do Debian, "1:2.39.2-1") e "~"; por isso não reaproveita tmpl.ValidValue, que
// também alimenta URLs e fica mais estrito.
func ValidVersion(v string) bool {
	return len(v) <= MaxFieldLen && versionRe.MatchString(v) && !strings.Contains(v, "..")
}

// OnlyFields devolve ErrInvalidRef se ref tiver algum campo preenchido fora de allowed (nomes: id, package,
// formula, cask, source, repo, asset, url, sha256, type, args).
func OnlyFields(ref PackageRef, allowed ...string) error {
	ok := map[string]bool{}
	for _, a := range allowed {
		ok[a] = true
	}
	fields := map[string]bool{
		"id": ref.ID != "", "package": ref.Package != "", "formula": ref.Formula != "", "cask": ref.Cask != "",
		"source": ref.Source != "", "repo": ref.Repo != "", "asset": ref.Asset != "", "url": ref.URL != "",
		"sha256": ref.SHA256 != "", "type": ref.Type != "", "args": len(ref.Args) > 0,
	}
	for name, set := range fields {
		if set && !ok[name] {
			return fmt.Errorf("%w: o campo %q não vale para o provider %q", ErrInvalidRef, name, ref.Provider)
		}
	}
	return nil
}

// RefFrom resolve os templates do instalador com vars e monta o PackageRef. A URL é revalidada depois da
// expansão (o valor expandido pode mudar o host).
func RefFrom(app catalog.App, in catalog.Installer, vars map[string]string) (PackageRef, error) {
	ref := PackageRef{AppID: app.ID, Provider: in.Provider, SHA256: in.SHA256, Type: in.Type, Line: in.Line}
	for _, f := range []struct {
		dst *string
		src string
	}{
		{&ref.ID, in.ID}, {&ref.Package, in.Package}, {&ref.Formula, in.Formula}, {&ref.Cask, in.Cask},
		{&ref.Source, in.Source}, {&ref.Repo, in.Repo}, {&ref.Asset, in.Asset}, {&ref.URL, in.URL},
	} {
		if f.src == "" {
			continue
		}
		v, err := tmpl.Expand(f.src, vars)
		if err != nil {
			return PackageRef{}, fmt.Errorf("app %q: %w", app.ID, err)
		}
		*f.dst = v
	}
	for _, a := range in.Args {
		v, err := tmpl.Expand(a, vars)
		if err != nil {
			return PackageRef{}, fmt.Errorf("app %q: %w", app.ID, err)
		}
		ref.Args = append(ref.Args, v)
	}
	if ref.URL != "" {
		if err := catalog.CheckURL(ref.URL, false); err != nil {
			return PackageRef{}, fmt.Errorf("app %q: %w", app.ID, err)
		}
	}
	return ref, nil
}

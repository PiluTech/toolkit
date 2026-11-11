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
	"unicode"

	"github.com/PiluVitu/toolkit/internal/catalog"
	"github.com/PiluVitu/toolkit/internal/exec"
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
	// SupportsVersion diz, SEM executar nada (nem leitura, nem Prepare), se o gerenciador instala esta versão
	// (version nunca é vazia nem "latest" aqui). Não suportar = ErrVersionUnsupported (brew, pacman, flatpak só
	// têm a atual). O orquestrador chama antes de Prepare, para não rodar apt-get update/brew update à toa.
	SupportsVersion(ref PackageRef, version string) error
	// Install instala. version vazia = a padrão. Versão que o gerenciador não permite = ErrVersionUnsupported
	// (nunca instala a última em silêncio). Chamado por InstallApp, que já validou a versão.
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
//
// Atenção: "@" e "/" são aceitos de propósito e SÃO SINTAXE do gerenciador: "python@3.12" escolhe a fórmula
// versionada no brew, "pkg/suite" escolhe o suite no apt e "tap/formula" um tap no brew. Um nome com "@" ou "/"
// pode então escolher outra versão ou origem do que o id parece dizer; o autor do catálogo responde por isso
// (um catálogo local é confiança do usuário), e a versão escolhida pelo usuário nunca entra por aqui: vai por
// Install(version), validada por ValidVersion.
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

// Runners reúne os dois Runners de um provider. Read só aceita leitura (Available, IsInstalled, ListVersions);
// Mutate recusa ReadOnly=true (Install, Prepare). O contrato deixa de depender da disciplina de cada builder:
// um Cmd no Runner errado volta ErrWrongRunner em vez de executar.
type Runners struct{ Read, Mutate exec.Runner }

// NewRunners embrulha r nos dois guardas.
func NewRunners(r exec.Runner) Runners {
	return Runners{Read: exec.ReadOnlyRunner(r), Mutate: exec.MutatingRunner(r)}
}

// CheckVersionArg valida a versão pedida: vazia, Latest ou ValidVersion; senão ErrInvalidRef.
func CheckVersionArg(v string) error {
	if v == "" || v == Latest || ValidVersion(v) {
		return nil
	}
	return fmt.Errorf("%w: versão %q com caracteres não permitidos", ErrInvalidRef, v)
}

var epochRe = regexp.MustCompile(`^[0-9]+:`)

// NormalizeVersion tira a época do Debian ("1:2.45-1" → "2.45-1").
func NormalizeVersion(v string) string { return epochRe.ReplaceAllString(v, "") }

// SameVersion compara a versão instalada (have) com a pedida (want). Sem época no pedido, a época é ignorada:
// "1:2.45-1" casa com "2.45-1" e, quando want não traz revisão do pacote, com "2.45" (o apt mostra a revisão, o
// usuário pede a versão do upstream). Com época no pedido ela TEM de ser a mesma (ausente = 0): "3:2.45" não é
// "1:2.45-1", porque o apt ordena pela época primeiro.
func SameVersion(have, want string) bool {
	if we := epochOf(want); we != "" {
		he := epochOf(have)
		if he == "" {
			he = "0"
		}
		if he != we {
			return false
		}
	}
	h, w := NormalizeVersion(have), NormalizeVersion(want)
	if h == w {
		return true
	}
	return !strings.Contains(w, "-") && strings.HasPrefix(h, w+"-")
}

// epochOf devolve a época ("1" em "1:2.45-1", sem os dois pontos) ou "" quando a versão não tem.
func epochOf(v string) string {
	if loc := epochRe.FindString(v); loc != "" {
		return strings.TrimSuffix(loc, ":")
	}
	return ""
}

// Snippet devolve a primeira linha não vazia de uma saída de comando, só com caracteres imprimíveis e no
// máximo 200 caracteres, para mensagens de erro: a saída de um gerenciador (ou de um pacote) nunca chega crua
// ao terminal.
func Snippet(b []byte) string {
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimRight(line, "\r")
		if i := strings.LastIndex(line, "\r"); i >= 0 {
			line = line[i+1:]
		}
		var sb strings.Builder
		for _, r := range line {
			if unicode.IsPrint(r) || r == ' ' || r == '\t' {
				if r == '\t' {
					r = ' '
				}
				sb.WriteRune(r)
			}
		}
		s := strings.TrimSpace(sb.String())
		if s == "" {
			continue
		}
		if rs := []rune(s); len(rs) > 200 {
			s = string(rs[:200]) + "…"
		}
		return s
	}
	return ""
}

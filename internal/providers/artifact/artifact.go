// Package artifact baixa um instalador por URL (com SHA-256 obrigatório) e o instala conforme o tipo. É o miolo
// compartilhado dos providers url e github-release:
//
//	msi      msiexec.exe /i <arquivo> /qn /norestart <args>   (Windows, exige administrador)
//	exe      <arquivo> <args>                                 (Windows; args silenciosos obrigatórios)
//	pkg      installer -pkg <arquivo> -target /               (macOS, sudo -n)
//	dmg      hdiutil attach, instala o .pkg ou copia o .app para /Applications, hdiutil detach (macOS)
//	deb      apt-get install -y --no-remove <arquivo>         (Linux, sudo -n)
//	rpm      dnf install -y <arquivo> (ou rpm -U sem dnf)     (Linux, sudo -n)
//	zip      extrai em <AppsDir>/<id> (Extract, sem executar nada)
//	tar.gz   idem
//
// Os args do catálogo só valem para msi e exe e vão sempre DEPOIS do arquivo, nunca no lugar de opção. O
// download vai para uma pasta temporária 0700 apagada ao fim; o instalador só roda depois do hash conferido
// (download.File). Na simulação (Env.Planner não nulo, o DryRunRunner) NADA é baixado nem executado: o plano
// registra o download e os comandos como seriam.
package artifact

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/PiluTech/toolkit/internal/download"
	"github.com/PiluTech/toolkit/internal/exec"
	"github.com/PiluTech/toolkit/internal/providers"
)

// Item é o que os providers entregam para instalar.
type Item struct {
	AppID   string // id do app no catálogo (nome da pasta de extração)
	URL     string
	SHA256  string
	Type    string // vazio = pela extensão de Name
	Name    string // nome do arquivo (extensão decide o tipo e o nome gravado)
	Args    []string
	MaxSize int64
	Header  http.Header
}

// Env reúne o que o instalador usa; tudo injetável para os testes.
type Env struct {
	GOOS string
	Rn   providers.Runners
	// Planner não nulo = simulação (o Runner de origem é o DryRunRunner): nada é baixado, extraído nem executado.
	Planner    exec.Planner
	Downloader *download.Downloader
	// AppsDir devolve onde os arquivos compactados são extraídos (padrão: DefaultAppsDir do SO).
	AppsDir func() (string, error)
	// TempDir cria a pasta temporária (padrão: download.NewTempDir).
	TempDir func() (string, func(), error)
	// ReadDir lista o volume montado de um dmg (padrão: os.ReadDir).
	ReadDir func(string) ([]fs.DirEntry, error)
}

const installTimeout = 30 * time.Minute

var (
	appIDRe  = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)
	hexRe    = regexp.MustCompile(`^[0-9a-fA-F]{64}$`)
	simDir   = "<pasta temporária>"
	pkgEnv   = []string{"DEBIAN_FRONTEND=noninteractive", "LC_ALL=C"}
	lcAll    = []string{"LC_ALL=C"}
	extTypes = []struct{ suffix, typ string }{
		{".tar.gz", "tar.gz"}, {".tgz", "tar.gz"}, {".msi", "msi"}, {".exe", "exe"}, {".pkg", "pkg"}, {".dmg", "dmg"},
		{".deb", "deb"}, {".rpm", "rpm"}, {".zip", "zip"}, {".appimage", "appimage"},
	}
)

// TypeOf devolve o tipo: explicit se vier, senão pela extensão de name ("" se não reconhece).
func TypeOf(name, explicit string) string {
	if explicit != "" {
		return explicit
	}
	low := strings.ToLower(name)
	for _, e := range extTypes {
		if strings.HasSuffix(low, e.suffix) {
			return e.typ
		}
	}
	return ""
}

func invalid(format string, a ...any) error {
	return fmt.Errorf("%w: %s", providers.ErrInvalidRef, fmt.Sprintf(format, a...))
}

// ValidateItem confere, sem rede nem comando, se o item pode ser instalado neste SO (ErrInvalidRef se não).
func ValidateItem(goos string, it Item) error {
	if !appIDRe.MatchString(it.AppID) {
		return invalid("id do app %q inválido", it.AppID)
	}
	if !hexRe.MatchString(it.SHA256) {
		return invalid("sha256 obrigatório (64 caracteres hexadecimais)")
	}
	typ := TypeOf(it.Name, it.Type)
	osOf := map[string]string{"msi": "windows", "exe": "windows", "pkg": "darwin", "dmg": "darwin", "deb": "linux", "rpm": "linux"}
	switch {
	case typ == "":
		return invalid("não foi possível deduzir o tipo de %q; declare type", it.Name)
	case typ == "appimage":
		return invalid("o tipo appimage ainda não é suportado")
	case typ != "zip" && typ != "tar.gz" && osOf[typ] == "":
		return invalid("tipo %q desconhecido", typ)
	case osOf[typ] != "" && osOf[typ] != goos:
		return invalid("o tipo %s só se instala em %s (sistema atual: %s)", typ, osOf[typ], goos)
	}
	if len(it.Args) > 0 && typ != "msi" && typ != "exe" {
		return invalid("args só valem para msi e exe, não para %s", typ)
	}
	if typ == "exe" && len(it.Args) == 0 {
		return invalid("um exe sem args abriria o instalador interativo: declare os args silenciosos")
	}
	for _, a := range it.Args {
		if a == "" {
			return invalid("args com item vazio")
		}
	}
	return nil
}

// DefaultAppsDir é a pasta de extração dos arquivos compactados, sempre do usuário: LOCALAPPDATA\toolkit\apps
// (Windows), $XDG_DATA_HOME/toolkit/apps ou ~/.local/share/toolkit/apps (Linux), ~/Library/Application
// Support/toolkit/apps (macOS). Home precisa ser absoluto.
func DefaultAppsDir(goos string, getenv func(string) string, home string) (string, error) {
	abs := func(p string) bool {
		return filepath.IsAbs(p) || (goos == "windows" && (strings.HasPrefix(p, `\\`) || (len(p) > 2 && p[1] == ':' && p[2] == '\\')))
	}
	if !abs(home) {
		return "", errors.New("sem diretório home absoluto para o usuário")
	}
	switch goos {
	case "windows":
		base := getenv("LOCALAPPDATA")
		if !abs(base) {
			base = filepath.Join(home, "AppData", "Local")
		}
		return filepath.Join(base, "toolkit", "apps"), nil
	case "darwin":
		return filepath.Join(home, "Library", "Application Support", "toolkit", "apps"), nil
	case "linux":
		base := getenv("XDG_DATA_HOME")
		if !filepath.IsAbs(base) {
			base = filepath.Join(home, ".local", "share")
		}
		return filepath.Join(base, "toolkit", "apps"), nil
	}
	return "", fmt.Errorf("sistema %q sem pasta de apps", goos)
}

func (e Env) appsDir() (string, error) {
	if e.AppsDir != nil {
		return e.AppsDir()
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return DefaultAppsDir(e.GOOS, os.Getenv, home)
}

func (e Env) tempDir() (string, func(), error) {
	if e.TempDir != nil {
		return e.TempDir()
	}
	return download.NewTempDir()
}

func (e Env) readDir(p string) ([]fs.DirEntry, error) {
	if e.ReadDir != nil {
		return e.ReadDir(p)
	}
	return os.ReadDir(p)
}

func (e Env) downloader() *download.Downloader {
	if e.Downloader != nil {
		return e.Downloader
	}
	return &download.Downloader{}
}

func maxMiB(it Item) int64 {
	if it.MaxSize > 0 {
		return it.MaxSize >> 20
	}
	return download.DefaultMaxSize >> 20
}

// Install baixa, confere o hash e instala (ou, na simulação, só registra o plano).
func (e Env) Install(ctx context.Context, it Item) (providers.InstallResult, error) {
	if err := ValidateItem(e.GOOS, it); err != nil {
		return providers.InstallResult{}, err
	}
	typ := TypeOf(it.Name, it.Type)
	name := download.SafeName(it.Name)
	sim := e.Planner != nil
	dir := simDir
	if sim {
		e.Planner.Note(fmt.Sprintf("baixar %s (sha256 %s, até %d MiB) para uma pasta temporária", it.URL, strings.ToLower(it.SHA256), maxMiB(it)))
	} else {
		d, cleanup, err := e.tempDir()
		if err != nil {
			return providers.InstallResult{}, err
		}
		defer cleanup()
		dir = d
		res, err := e.downloader().File(ctx, download.Request{URL: it.URL, SHA256: it.SHA256, MaxSize: it.MaxSize, Dir: dir, Name: name, Header: it.Header})
		if err != nil {
			if errors.Is(err, download.ErrHashMismatch) {
				return providers.InstallResult{}, fmt.Errorf("%w: %w", providers.ErrHashMismatch, err)
			}
			return providers.InstallResult{}, err
		}
		name = filepath.Base(res.Path)
	}
	file := filepath.Join(dir, name)
	switch typ {
	case "msi":
		return e.run(ctx, "msiexec.exe", exec.Cmd{Name: "msiexec.exe", Args: append([]string{"/i", file, "/qn", "/norestart"}, it.Args...), Elevate: true, Timeout: installTimeout}, msiResult)
	case "exe":
		return e.run(ctx, file, exec.Cmd{Name: file, Args: it.Args, Timeout: installTimeout}, exeResult)
	case "pkg":
		return e.run(ctx, "installer", pkgCmd(file), genericResult("installer"))
	case "deb":
		return e.run(ctx, "apt-get", exec.Cmd{Name: "apt-get", Args: []string{"install", "-y", "--no-remove", file}, Env: pkgEnv, Elevate: true, Timeout: installTimeout}, genericResult("apt-get"))
	case "rpm":
		if _, err := e.Rn.Read.LookPath("dnf"); err == nil {
			return e.run(ctx, "dnf", exec.Cmd{Name: "dnf", Args: []string{"install", "-y", file}, Env: lcAll, Elevate: true, Timeout: installTimeout}, genericResult("dnf"))
		}
		return e.run(ctx, "rpm", exec.Cmd{Name: "rpm", Args: []string{"-U", "--", file}, Env: lcAll, Elevate: true, Timeout: installTimeout}, genericResult("rpm"))
	case "dmg":
		return e.installDMG(ctx, dir, file, sim)
	default: // zip, tar.gz
		return e.installArchive(ctx, typ, file, it.AppID, sim)
	}
}

func pkgCmd(file string) exec.Cmd {
	return exec.Cmd{Name: "installer", Args: []string{"-pkg", file, "-target", "/"}, Elevate: true, Timeout: installTimeout}
}

type resultFn func(exec.Result) (providers.InstallResult, error)

// run executa UMA mutação pelo Runner e interpreta o resultado.
func (e Env) run(ctx context.Context, name string, c exec.Cmd, interpret resultFn) (providers.InstallResult, error) {
	res, err := e.Rn.Mutate.Run(ctx, c)
	if err != nil {
		if errors.Is(err, exec.ErrNotElevated) {
			return providers.InstallResult{}, fmt.Errorf("%s: %w", name, providers.ErrNeedsAdmin)
		}
		return providers.InstallResult{}, fmt.Errorf("%s: %w", name, err)
	}
	return interpret(res)
}

func msiResult(res exec.Result) (providers.InstallResult, error) {
	switch res.ExitCode {
	case 0:
		return providers.InstallResult{}, nil
	case 3010, 1641:
		return providers.InstallResult{RebootRequired: true}, nil
	case 1638:
		return providers.InstallResult{AlreadyInstalled: true}, nil
	case 1618:
		return providers.InstallResult{}, fmt.Errorf("msiexec: outra instalação em andamento: %w", providers.ErrRetryable)
	case 1925:
		return providers.InstallResult{}, fmt.Errorf("msiexec: %w", providers.ErrNeedsAdmin)
	}
	return providers.InstallResult{}, fmt.Errorf("msiexec saiu com código %d", res.ExitCode)
}

func exeResult(res exec.Result) (providers.InstallResult, error) {
	switch res.ExitCode {
	case 0:
		return providers.InstallResult{}, nil
	case 3010:
		return providers.InstallResult{RebootRequired: true}, nil
	case 740: // ERROR_ELEVATION_REQUIRED
		return providers.InstallResult{}, fmt.Errorf("instalador: %w", providers.ErrNeedsAdmin)
	}
	return providers.InstallResult{}, fmt.Errorf("o instalador saiu com código %d: %s", res.ExitCode, providers.Snippet(res.Stderr))
}

func genericResult(name string) resultFn {
	return func(res exec.Result) (providers.InstallResult, error) {
		if res.ExitCode == 0 {
			return providers.InstallResult{}, nil
		}
		low := strings.ToLower(string(res.Stderr))
		switch {
		case strings.Contains(low, "could not get lock") || strings.Contains(low, "unable to acquire the dpkg") || strings.Contains(low, "another app is currently holding"):
			return providers.InstallResult{}, fmt.Errorf("%s: %w", name, providers.ErrRetryable)
		case strings.Contains(low, "permission denied") || strings.Contains(low, "operation not permitted") || strings.Contains(low, "are you root") || strings.Contains(low, "must be root"):
			return providers.InstallResult{}, fmt.Errorf("%s: %w", name, providers.ErrNeedsAdmin)
		}
		return providers.InstallResult{}, fmt.Errorf("%s saiu com código %d: %s", name, res.ExitCode, providers.Snippet(res.Stderr))
	}
}

func (e Env) installDMG(ctx context.Context, dir, file string, sim bool) (providers.InstallResult, error) {
	mnt := filepath.Join(dir, "mnt")
	attach := exec.Cmd{Name: "hdiutil", Args: []string{"attach", "-nobrowse", "-readonly", "-noautoopen", "-mountpoint", mnt, file},
		Stdin: strings.NewReader("Y\n"), Timeout: installTimeout} // o "Y" aceita a licença que alguns dmg exibem
	if !sim {
		if err := os.MkdirAll(mnt, 0o700); err != nil {
			return providers.InstallResult{}, err
		}
	}
	if _, err := e.run(ctx, "hdiutil", attach, genericResult("hdiutil attach")); err != nil {
		return providers.InstallResult{}, err
	}
	defer func() { // o volume sai mesmo com erro ou cancelamento
		detach := exec.Cmd{Name: "hdiutil", Args: []string{"detach", mnt}, Timeout: installTimeout}
		_, _ = e.Rn.Mutate.Run(context.WithoutCancel(ctx), detach)
	}()
	if sim {
		e.Planner.Note("instalar o .pkg do volume (installer -pkg ... -target /) ou copiar o .app para /Applications")
		return providers.InstallResult{}, nil
	}
	entries, err := e.readDir(mnt)
	if err != nil {
		return providers.InstallResult{}, fmt.Errorf("lendo o volume montado: %w", err)
	}
	var pkgs, apps []string
	for _, en := range entries {
		n := en.Name()
		switch {
		case strings.ContainsAny(n, "\x00/\\") || strings.IndexFunc(n, func(r rune) bool { return r < 0x20 || r == 0x7f }) >= 0:
		case strings.HasSuffix(strings.ToLower(n), ".pkg") && !en.IsDir():
			pkgs = append(pkgs, n)
		case strings.HasSuffix(strings.ToLower(n), ".app") && en.IsDir():
			apps = append(apps, n)
		}
	}
	switch {
	case len(pkgs) == 1 && len(apps) == 0:
		return e.run(ctx, "installer", pkgCmd(filepath.Join(mnt, pkgs[0])), genericResult("installer"))
	case len(apps) == 1 && len(pkgs) == 0:
		cp := exec.Cmd{Name: "ditto", Args: []string{filepath.Join(mnt, apps[0]), "/Applications/" + apps[0]}, Timeout: installTimeout}
		return e.run(ctx, "ditto", cp, genericResult("ditto"))
	}
	return providers.InstallResult{}, fmt.Errorf("o dmg deve conter exatamente um .pkg ou um .app na raiz (achei %d .pkg e %d .app)", len(pkgs), len(apps))
}

// installArchive extrai em <AppsDir>/<id>: primeiro numa pasta de staging do mesmo diretório, e só quando tudo
// extraiu a versão anterior é trocada. Uma extração que falha não apaga o que já estava lá.
func (e Env) installArchive(_ context.Context, typ, file, appID string, sim bool) (providers.InstallResult, error) {
	apps, err := e.appsDir()
	if err != nil {
		return providers.InstallResult{}, err
	}
	final := filepath.Join(apps, appID)
	if sim {
		e.Planner.Note(fmt.Sprintf("extrair o %s em %s (substitui a versão anterior)", typ, final))
		return providers.InstallResult{}, nil
	}
	if err := os.MkdirAll(apps, 0o755); err != nil {
		return providers.InstallResult{}, err
	}
	stage, err := os.MkdirTemp(apps, ".toolkit-extract-")
	if err != nil {
		return providers.InstallResult{}, err
	}
	defer func() { _ = os.RemoveAll(stage) }()
	if err := Extract(typ, file, stage, DefaultLimits); err != nil {
		return providers.InstallResult{}, err
	}
	if err := os.RemoveAll(final); err != nil {
		return providers.InstallResult{}, err
	}
	if err := os.Rename(stage, final); err != nil {
		return providers.InstallResult{}, err
	}
	if err := os.Chmod(final, 0o755); err != nil {
		return providers.InstallResult{}, err
	}
	return providers.InstallResult{}, nil
}

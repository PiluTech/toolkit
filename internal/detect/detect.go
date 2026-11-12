// Package detect acha, SÓ POR LEITURA, um app já instalado FORA do gerenciador do catálogo: o .app que veio do
// .dmg no macOS, o programa do instalador no Windows (registro Uninstall), o .desktop, o flatpak ou o snap no
// Linux. Sem isso o toolkit diz "não instalado" e o gerenciador falha ("already an App at /Applications/X.app").
//
// Nada aqui altera o sistema: os comandos são sempre exec.Cmd com ReadOnly, por caminho absoluto quando o
// comando é do sistema, e os arquivos vêm de um fs.FS injetável. A detecção é de MELHOR ESFORÇO: falha de
// leitura (plutil ausente, PowerShell falhando) vira "não detectado", nunca um erro que impeça o install (só o
// cancelamento do contexto é devolvido). Todo texto que vem de fora (plist, registro, saída de comando,
// nome de arquivo) passa por textsafe antes de ser guardado.
package detect

import (
	"context"
	"io/fs"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/PiluTech/toolkit/internal/catalog"
	"github.com/PiluTech/toolkit/internal/exec"
	"github.com/PiluTech/toolkit/internal/textsafe"
)

// Found descreve a instalação achada fora do gerenciador.
type Found struct {
	// Where é o lugar, para a mensagem ("/Applications/Discord.app", "flatpak (com.x.Y)"). Já sanitizado.
	Where string
	// Version é a versão lida do Info.plist, do registro, do flatpak ou do snap; vazia se não deu para ler. Já sanitizada.
	Version string
}

// Options configura o detector. Tudo é injetável: os testes nunca tocam o disco nem o sistema reais.
type Options struct {
	GOOS string // "windows", "darwin" ou "linux"
	// FS é a raiz do sistema de arquivos (os.DirFS("/") no Unix); nil = sem leitura de disco. Os caminhos são
	// absolutos sem a barra inicial ("Applications/Discord.app").
	FS fs.FS
	// Home é a pasta do usuário (absoluta); vazia = só os locais do sistema.
	Home string
	// Now é o relógio do cache da varredura (nil = time.Now).
	Now func() time.Time
}

// Detector procura apps fora do gerenciador.
type Detector struct {
	rn   exec.Runner
	opts Options

	mu       sync.Mutex
	macIndex map[string]Found // bundle_id (minúsculo) -> bundle achado, da varredura de /Applications e ~/Applications
	macAt    time.Time        // quando a varredura foi feita (vale cacheTTL)
	macBuilt bool
}

// cacheTTL é por quanto tempo a varredura de .app por bundle_id é reaproveitada: a TUI pergunta o estado de
// vários apps em seguida e antes cada pergunta lia todos os Info.plist de novo. Curto de propósito: um app
// instalado agora aparece em segundos; depois de uma execução quem muda o sistema chama Invalidate.
const cacheTTL = 30 * time.Second

func (d *Detector) now() time.Time {
	if d.opts.Now != nil {
		return d.opts.Now()
	}
	return time.Now()
}

// Invalidate descarta a varredura em cache (chamar depois de instalar ou remover algo).
func (d *Detector) Invalidate() {
	d.mu.Lock()
	d.macIndex, d.macBuilt = nil, false
	d.mu.Unlock()
}

// New cria o detector sobre o Runner (só leituras).
func New(r exec.Runner, o Options) *Detector { return &Detector{rn: r, opts: o} }

// Find procura o app segundo o bloco detect do catálogo para o SO do detector. ok é false quando não há bloco,
// quando nada foi achado ou quando a leitura falhou; o erro só vem do cancelamento do contexto.
func (d *Detector) Find(ctx context.Context, app catalog.App) (Found, bool, error) {
	spec, ok := app.DetectFor(d.opts.GOOS)
	if !ok {
		return Found{}, false, nil
	}
	if err := ctx.Err(); err != nil {
		return Found{}, false, err
	}
	var (
		f     Found
		found bool
	)
	switch d.opts.GOOS {
	case "darwin":
		f, found = d.findMac(ctx, spec)
	case "windows":
		f, found = d.findWindows(ctx, spec)
	case "linux":
		f, found = d.findLinux(ctx, spec)
	}
	if err := ctx.Err(); err != nil {
		return Found{}, false, err
	}
	return f, found, nil
}

// clean sanitiza texto de fora e limita o tamanho (n runas).
func clean(s string, n int) string {
	s = strings.TrimSpace(textsafe.Printable(s))
	if utf8.RuneCountInString(s) > n {
		s = string([]rune(s)[:n]) + "…"
	}
	return s
}

// rel converte um caminho absoluto no caminho relativo da fs.FS raiz.
func rel(abs string) string { return strings.TrimPrefix(abs, "/") }

// absClean diz se p é absoluto, sem "..", para entrar no fs.FS.
func absClean(p string) bool {
	return strings.HasPrefix(p, "/") && fs.ValidPath(rel(p))
}

func (d *Detector) readOnly(c exec.Cmd) exec.Cmd {
	c.ReadOnly = true
	return c
}

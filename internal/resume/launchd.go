package resume

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"strings"

	"github.com/PiluTech/toolkit/internal/buildinfo"
	"github.com/PiluTech/toolkit/internal/exec"
)

// Launchd registra a retomada como LaunchAgent do usuário (não LaunchDaemon: o brew não roda como root e
// um daemon rodaria sem sessão). O agente roda no login (RunAtLoad + LaunchOnlyOnce) e abre o Terminal
// com um .command, para o usuário ver o progresso e digitar a senha do sudo se alguma etapa pedir.
type Launchd struct {
	Runner exec.Runner
	FS     FS
	Home   string // home absoluto do usuário (~/Library/LaunchAgents)
	UID    string // uid numérico do usuário (launchctl bootstrap gui/<uid>)
	BinDir string // o binário e o .command ficam aqui
}

var _ Registrar = Launchd{}

// Label é o label do agente: dev.pilutech.<name>.
func Label() string { return buildinfo.Identifier() }

func (l Launchd) plistPath() string { return l.Home + "/Library/LaunchAgents/" + Label() + ".plist" }
func (l Launchd) exe() string       { return l.BinDir + "/" + buildinfo.Name }
func (l Launchd) command() string   { return l.BinDir + "/" + buildinfo.Name + "-resume.command" }

// shQuote devolve s entre aspas simples para sh (' vira '\”).
func shQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

// CommandFile é o .command que o Terminal executa (público para o teste).
func (l Launchd) CommandFile(sp Spec) (string, error) {
	if err := ValidArgs(sp.Args); err != nil {
		return "", err
	}
	return "#!/bin/sh\n# gerado pelo " + buildinfo.Name + ": continua a receita depois do reboot\nexec " +
		shQuote(l.exe()) + " " + strings.Join(sp.Args, " ") + "\n", nil
}

func xmlEscape(s string) string {
	var b bytes.Buffer
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}

// Plist monta o LaunchAgent (público para o teste).
func (l Launchd) Plist() string {
	return `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>` + xmlEscape(Label()) + `</string>
	<key>ProgramArguments</key>
	<array>
		<string>/usr/bin/open</string>
		<string>-a</string>
		<string>Terminal</string>
		<string>` + xmlEscape(l.command()) + `</string>
	</array>
	<key>RunAtLoad</key>
	<true/>
	<key>LaunchOnlyOnce</key>
	<true/>
</dict>
</plist>
`
}

func (l Launchd) check() error {
	if !strings.HasPrefix(l.Home, "/") || l.UID == "" || strings.Trim(l.UID, "0123456789") != "" {
		return errors.New("retomada no macOS exige home absoluto e uid numérico do usuário")
	}
	for _, p := range []string{l.Home, l.BinDir} {
		if err := validPath(p, false); err != nil {
			return err
		}
	}
	return nil
}

func (l Launchd) bootout() exec.Cmd {
	return exec.Cmd{Name: "launchctl", Args: []string{"bootout", "gui/" + l.UID + "/" + Label()}}
}

// Register instala o binário e o .command, grava o plist e carrega o agente (bootstrap).
func (l Launchd) Register(ctx context.Context, sp Spec) error {
	if err := l.check(); err != nil {
		return err
	}
	cmdFile, err := l.CommandFile(sp)
	if err != nil {
		return err
	}
	if sp.Executable == "" {
		return errors.New("falta o caminho do binário")
	}
	for _, d := range []string{l.BinDir, l.Home + "/Library/LaunchAgents"} {
		if err := l.FS.MkdirAll(d, 0o755); err != nil {
			return fmt.Errorf("criar %s: %w", d, err)
		}
	}
	if sp.Executable != l.exe() {
		if err := l.FS.CopyFile(sp.Executable, l.exe(), 0o755); err != nil {
			return fmt.Errorf("copiar o binário para %s: %w", l.exe(), err)
		}
	}
	if err := l.FS.WriteFile(l.command(), []byte(cmdFile), 0o755); err != nil {
		return err
	}
	if err := l.FS.WriteFile(l.plistPath(), []byte(l.Plist()), 0o644); err != nil {
		return err
	}
	// Recarrega: se já havia um agente carregado, bootstrap falharia. O bootout é best-effort.
	_, _ = l.Runner.Run(ctx, l.bootout())
	return runOK(ctx, l.Runner, exec.Cmd{Name: "launchctl", Args: []string{"bootstrap", "gui/" + l.UID, l.plistPath()}}, "carregar o LaunchAgent")
}

// Unregister descarrega o agente e apaga o plist e o .command. Não registrado = sucesso.
func (l Launchd) Unregister(ctx context.Context) error {
	if err := l.check(); err != nil {
		return err
	}
	if !l.FS.Exists(l.plistPath()) {
		return nil
	}
	_, _ = l.Runner.Run(ctx, l.bootout()) // não carregado (reboot) não é erro
	if err := l.FS.Remove(l.plistPath()); err != nil {
		return err
	}
	return l.FS.Remove(l.command())
}

// IsRegistered diz se o plist existe.
func (l Launchd) IsRegistered(context.Context) (bool, error) {
	if err := l.check(); err != nil {
		return false, err
	}
	return l.FS.Exists(l.plistPath()), nil
}

package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	osexec "os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/spf13/cobra"

	"github.com/PiluTech/toolkit/internal/buildinfo"
	"github.com/PiluTech/toolkit/internal/msg"
	"github.com/PiluTech/toolkit/internal/update"
)

// UpdateDeps são os pontos injetáveis do `toolkit update` (nil/zero = valores reais).
type UpdateDeps struct {
	APIBase, DownloadBase string                 // release de teste (padrão: GitHub)
	AllowedHosts          []string               // hosts permitidos (padrão: os do GitHub); testes trocam
	Current               string                 // versão em execução (padrão buildinfo.Version)
	GOOS, GOARCH          string                 // padrão runtime
	Exe                   func() (string, error) // binário em execução (padrão os.Executable)
}

func (d UpdateDeps) config(deps Deps) (update.Config, error) {
	exe := ""
	var err error
	if d.Exe != nil {
		exe, err = d.Exe()
	} else {
		exe, err = os.Executable()
	}
	if err != nil {
		return update.Config{}, fmt.Errorf("não achei o caminho do binário: %w", err)
	}
	c := update.Config{Invoked: d.invoked(exe), AllowedHosts: d.AllowedHosts, APIBase: d.APIBase, DownloadBase: d.DownloadBase, Current: d.Current, GOOS: d.GOOS, GOARCH: d.GOARCH, Exe: exe, Downloader: deps.downloader(), Token: deps.githubToken}
	if c.Current == "" {
		c.Current = buildinfo.Version
	}
	if c.GOOS == "" {
		c.GOOS = runtime.GOOS
	}
	if c.GOARCH == "" {
		c.GOARCH = runtime.GOARCH
	}
	return c, nil
}

// invoked é o caminho pelo qual o binário foi chamado (os.Args[0], achado no PATH se vier sem barra): no Linux
// os.Executable() já devolve o caminho resolvido e esconderia um link simbólico.
func (d UpdateDeps) invoked(exe string) string {
	if d.Exe != nil { // teste: o binário é o informado
		return exe
	}
	a0 := ""
	if len(os.Args) > 0 {
		a0 = os.Args[0]
	}
	if a0 == "" {
		return exe
	}
	if !strings.ContainsRune(a0, os.PathSeparator) {
		if p, err := osexec.LookPath(a0); err == nil {
			a0 = p
		}
	}
	if abs, err := filepath.Abs(a0); err == nil {
		return abs
	}
	return exe
}

type updateFlags struct {
	check, dryRun bool
	version       string
}

func newUpdateCmd(deps Deps) *cobra.Command {
	var f updateFlags
	cmd := &cobra.Command{
		Use:   "update",
		Short: msg.UpdateShort,
		Long:  msg.UpdateLong,
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runUpdate(cmd.Context(), cmd.OutOrStdout(), deps, f)
		},
	}
	cmd.Flags().BoolVar(&f.check, "check", false, msg.UpdateFlagCheck)
	cmd.Flags().BoolVar(&f.dryRun, "dry-run", false, msg.UpdateFlagDryRun)
	cmd.Flags().StringVar(&f.version, "version", "", msg.UpdateFlagVersion)
	return cmd
}

func runUpdate(ctx context.Context, out io.Writer, deps Deps, f updateFlags) error {
	if ctx == nil {
		ctx = context.Background()
	}
	cfg, err := deps.Update.config(deps)
	if err != nil {
		return err
	}
	update.CleanupOld(cfg.Exe)
	// a versão pedida é validada ANTES de qualquer requisição (ela entra na URL)
	var rel update.Release
	if f.version != "" {
		rel, err = update.ReleaseByTag(ctx, cfg, f.version)
		if errors.Is(err, update.ErrRateLimited) {
			// a versão é explícita: segue sem as notas (a API só serve para elas); o download não usa a API
			v, perr := update.ParseVersion(f.version)
			if perr != nil {
				return perr
			}
			rel, err = update.Release{Tag: v.String()}, nil
			_, _ = fmt.Fprintln(out, msg.UpdateNoNotes)
		}
		if err != nil {
			return err
		}
	} else if rel, err = update.Latest(ctx, cfg); err != nil {
		return err
	}
	plan, err := update.MakePlan(cfg, rel)
	if err != nil {
		return err
	}
	cur := printable(cfg.Current)
	tag := plan.Target.String()

	if f.check {
		switch {
		case plan.CurrentParsed == nil:
			_, _ = fmt.Fprintf(out, msg.UpdateDevBuild+"\n", cur)
			_, _ = fmt.Fprintf(out, "Última release: %s\n", tag)
		case plan.Newer:
			_, _ = fmt.Fprintf(out, msg.UpdateAvailable+"\n", cur, tag)
			printNotes(out, rel)
			_, _ = fmt.Fprintln(out, msg.UpdateHowTo)
		default:
			_, _ = fmt.Fprintf(out, msg.UpdateUpToDate+"\n", cur)
		}
		return nil
	}

	explicit := f.version != ""
	switch {
	case plan.CurrentParsed == nil && !explicit:
		return fmt.Errorf(msg.UpdateDevBuild, cur)
	case plan.Same:
		_, _ = fmt.Fprintf(out, msg.UpdateSame+"\n", cur)
		return nil
	case plan.Downgrade && !explicit:
		_, _ = fmt.Fprintf(out, msg.UpdateNewerInstalled+"\n", cur, tag, tag)
		return nil
	case plan.Downgrade:
		_, _ = fmt.Fprintf(out, msg.UpdateDowngrade+"\n", cur, tag)
	}
	if plan.Newer {
		printNotes(out, rel)
	}
	if f.dryRun {
		_, _ = fmt.Fprintf(out, msg.UpdateDryRunLine1+"\n", plan.AssetURL, plan.ChecksumsURL)
		_, _ = fmt.Fprintf(out, msg.UpdateDryRunLine2+"\n", printable(plan.Exe), tag)
		return nil
	}
	if err := update.Apply(ctx, cfg, plan); err != nil {
		return err
	}
	if plan.CurrentParsed != nil {
		_, _ = fmt.Fprintf(out, msg.UpdateDone+"\n", cur, tag)
	} else {
		_, _ = fmt.Fprintf(out, msg.UpdateDoneFrom+"\n", tag)
	}
	return nil
}

func printNotes(out io.Writer, rel update.Release) {
	if rel.Notes == "" {
		return
	}
	_, _ = fmt.Fprintln(out, msg.UpdateNotes)
	_, _ = fmt.Fprintln(out, rel.Notes)
	if rel.URL != "" {
		_, _ = fmt.Fprintln(out, rel.URL)
	}
}

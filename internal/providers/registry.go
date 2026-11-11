package providers

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/PiluVitu/toolkit/internal/catalog"
)

// Registry guarda os providers por nome.
type Registry struct {
	byName map[string]Provider
}

// NewRegistry registra os providers (o último de mesmo nome vence).
func NewRegistry(ps ...Provider) *Registry {
	r := &Registry{byName: map[string]Provider{}}
	for _, p := range ps {
		r.byName[p.Name()] = p
	}
	return r
}

// Get devolve o provider pelo nome.
func (r *Registry) Get(name string) (Provider, bool) {
	p, ok := r.byName[name]
	return p, ok
}

// Candidate é um instalador do app cujo provider existe, está disponível e aceitou o PackageRef.
type Candidate struct {
	Provider Provider
	Ref      PackageRef
}

// Skip explica por que um instalador da lista foi descartado.
type Skip struct {
	Provider string
	Reason   string
}

// Candidates percorre, na ordem do YAML, os instaladores do SO (fallback any) e devolve os aplicáveis: provider
// registrado, Available e com Validate ok. O resto vira Skip (para a mensagem de erro e o diagnóstico).
func (r *Registry) Candidates(ctx context.Context, app catalog.App, goos string, vars map[string]string) ([]Candidate, []Skip) {
	var cands []Candidate
	var skips []Skip
	for _, in := range app.InstallersFor(goos) {
		p, ok := r.byName[in.Provider]
		if !ok {
			skips = append(skips, Skip{in.Provider, "provider ainda não suportado"})
			continue
		}
		ref, err := RefFrom(app, in, vars)
		if err != nil {
			skips = append(skips, Skip{in.Provider, err.Error()})
			continue
		}
		if err := p.Validate(ref); err != nil {
			skips = append(skips, Skip{in.Provider, err.Error()})
			continue
		}
		if !p.Available(ctx) {
			skips = append(skips, Skip{in.Provider, "gerenciador não encontrado nesta máquina"})
			continue
		}
		cands = append(cands, Candidate{Provider: p, Ref: ref})
	}
	return cands, skips
}

// NoInstallerError monta o erro de "nenhum candidato" com o motivo de cada instalador descartado.
func NoInstallerError(app catalog.App, goos string, skips []Skip) error {
	if len(skips) == 0 {
		return fmt.Errorf("app %q: %w (sem instaladores para %s)", app.ID, ErrNoApplicableInstaller, goos)
	}
	parts := make([]string, len(skips))
	for i, s := range skips {
		parts[i] = s.Provider + ": " + s.Reason
	}
	return fmt.Errorf("app %q: %w (%s)", app.ID, ErrNoApplicableInstaller, strings.Join(parts, "; "))
}

// Outcome é o resultado de InstallApp.
type Outcome struct {
	Provider string
	Ref      PackageRef
	Result   InstallResult
	// Tried lista os providers que recusaram (não aplicável, não encontrado, versão não suportada) antes do que deu certo.
	Tried []string
}

// InstallApp instala o app pelo primeiro candidato que serve. A versão é validada UMA vez, no começo (vazia,
// Latest ou ValidVersion; senão ErrInvalidRef antes de qualquer método de provider). O chamador continua
// passando por App.VersionVars quando o id tem template: esta checagem é a segunda camada, e a única para
// apps com versions: true, cuja versão (com época do Debian, por exemplo) não passa por tmpl.ValidValue.
//
// Para cada candidato: IsInstalled (leitura) primeiro, de modo que rodar duas vezes não reinstala (com versão,
// "já instalado" usa SameVersion: época do Debian ignorada); depois SupportsVersion (sem mutação); depois
// Prepare (se o provider tiver); depois Install. ErrNoApplicableInstaller, ErrNotFound e ErrVersionUnsupported
// passam para o próximo candidato (se todos recusarem, o último erro é o resultado); qualquer outro erro
// encerra.
func InstallApp(ctx context.Context, cands []Candidate, version string) (Outcome, error) {
	if err := CheckVersionArg(version); err != nil {
		return Outcome{}, err
	}
	if len(cands) == 0 {
		return Outcome{}, ErrNoApplicableInstaller
	}
	pinned := version != "" && version != Latest
	var out Outcome
	var lastErr error
	for _, c := range cands {
		if err := ctx.Err(); err != nil {
			return out, err
		}
		installed, have, err := c.Provider.IsInstalled(ctx, c.Ref)
		if err != nil && !skippable(err) {
			return out, fmt.Errorf("%s: %w", c.Provider.Name(), err)
		}
		if err == nil && installed && (!pinned || SameVersion(have, version)) {
			out.Provider, out.Ref = c.Provider.Name(), c.Ref
			out.Result = InstallResult{AlreadyInstalled: true, Version: have}
			return out, nil
		}
		if pinned {
			if err := c.Provider.SupportsVersion(c.Ref, version); err != nil {
				if skippable(err) {
					out.Tried = append(out.Tried, c.Provider.Name())
					lastErr = fmt.Errorf("%s: %w", c.Provider.Name(), err)
					continue
				}
				return out, fmt.Errorf("%s: %w", c.Provider.Name(), err)
			}
		}
		if p, ok := c.Provider.(Preparer); ok {
			if err := p.Prepare(ctx); err != nil {
				return out, fmt.Errorf("%s: %w", c.Provider.Name(), err)
			}
		}
		res, err := c.Provider.Install(ctx, c.Ref, version)
		if err != nil {
			if skippable(err) {
				out.Tried = append(out.Tried, c.Provider.Name())
				lastErr = fmt.Errorf("%s: %w", c.Provider.Name(), err)
				continue
			}
			return out, fmt.Errorf("%s: %w", c.Provider.Name(), err)
		}
		out.Provider, out.Ref, out.Result = c.Provider.Name(), c.Ref, res
		return out, nil
	}
	return out, lastErr
}

func skippable(err error) bool {
	return errors.Is(err, ErrNoApplicableInstaller) || errors.Is(err, ErrNotFound) || errors.Is(err, ErrVersionUnsupported)
}

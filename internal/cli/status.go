package cli

import (
	"context"

	"github.com/PiluTech/toolkit/internal/catalog"
	"github.com/PiluTech/toolkit/internal/exec"
	"github.com/PiluTech/toolkit/internal/providers"
	"github.com/PiluTech/toolkit/internal/tui"
)

// appState é o estado de um app na máquina, só por leitura: instalado pelo gerenciador do catálogo, instalado FORA
// dele (External não é nil; Installed também fica true) ou não instalado.
type appState struct {
	Installed bool
	Version   string
	External  *providers.External
	// DetectGap, com Installed false, diz por que "não instalado" seria enganoso: DetectIncomplete (a leitura do
	// registro falhou em parte) ou DetectNotApplicable (o app não tem detecção no Windows, ex.: Python e apps da
	// Microsoft Store). Vazio = a leitura foi completa e o app não está.
	DetectGap string
}

// Valores de appState.DetectGap.
const (
	DetectIncomplete    = tui.DetectIncomplete
	DetectNotApplicable = tui.DetectNotApplicable
)

// readState lê o estado do app pelo primeiro instalador aplicável (a mesma regra do install): primeiro o
// gerenciador (IsInstalled) e, só se ele disser "não instalado", a detecção fora do gerenciador. Nada é executado
// que mude o sistema.
func readState(ctx context.Context, deps Deps, rn exec.Runner, reg *providers.Registry, app catalog.App) (appState, error) {
	vars, _, err := resolveVersion(app, "")
	if err != nil {
		vars = map[string]string{}
	}
	cands, skips := reg.Candidates(ctx, app, deps.GOOS, vars)
	if len(cands) == 0 {
		return appState{}, providers.NoInstallerError(app, deps.GOOS, skips)
	}
	c := cands[0]
	ok, version, err := c.Provider.IsInstalled(ctx, c.Ref)
	if err != nil {
		return appState{}, err
	}
	if ok {
		return appState{Installed: true, Version: version}, nil
	}
	if find := deps.externalFinder(rn, app); find != nil {
		ext, found, err := find(ctx)
		if err != nil {
			return appState{}, err
		}
		if found {
			ext.Manager = providers.DisplayName(c.Provider.Name())
			return appState{Installed: true, Version: ext.Version, External: &ext}, nil
		}
		if ext.Incomplete {
			return appState{DetectGap: DetectIncomplete}, nil
		}
		if deps.GOOS == "windows" {
			if _, has := app.DetectFor(deps.GOOS); !has {
				return appState{DetectGap: DetectNotApplicable}, nil
			}
		}
	}
	return appState{}, nil
}

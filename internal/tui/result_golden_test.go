package tui

import (
	"errors"
	"fmt"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func goldenOpts() Options { return Options{Version: "v0.1.0", Mono: true, NoMotion: true} }

func TestGoldenFaixaPrivacidadeOK(t *testing.T) {
	for _, sz := range goldenSizes() {
		be := &fakePriv{info: privSample(true), plan: PrivPlan{ID: "p", Text: planText, Pending: 3, IsAdmin: true}, applyRes: PrivResult{Applied: 3, BackupPath: `C:\ProgramData\toolkit\backups\privacidade-20261114-1200.json`}}
		tab := privTab(t, be)
		pkey(tab, "down", "a", "s")
		golden(t, fmt.Sprintf("faixa_priv_ok_%dx%d", sz[0], sz[1]), sized(goldenOpts(), sz[0], sz[1], tab).render())
	}
}

func TestGoldenFaixaPrivacidadeParcialEErro(t *testing.T) {
	for _, sz := range goldenSizes() {
		be := &fakePriv{info: privSample(true), plan: PrivPlan{ID: "p", Text: planText, Pending: 3, IsAdmin: true}, applyRes: PrivResult{Applied: 2, BackupPath: `C:\ProgramData\toolkit\backups\privacidade-20261114-1200.json`}, applyErr: errors.New(`HKLM\SOFTWARE\Policies\Microsoft\Windows\DataCollection: acesso negado`)}
		tab := privTab(t, be)
		pkey(tab, "down", "a", "s")
		golden(t, fmt.Sprintf("faixa_priv_parcial_%dx%d", sz[0], sz[1]), sized(goldenOpts(), sz[0], sz[1], tab).render())
	}
	be := &fakePriv{info: privSample(true), plan: PrivPlan{ID: "p", Text: planText, Pending: 3, IsAdmin: true}, applyErr: ErrPrivNeedsAdmin}
	tab := privTab(t, be)
	pkey(tab, "a", "s")
	golden(t, "faixa_priv_erro_80x24", sized(goldenOpts(), 80, 24, tab).render())
}

func TestGoldenFaixaAppsNativosOK(t *testing.T) {
	for _, sz := range goldenSizes() {
		be := &fakeNat{info: natSample(true), plan: NatPlan{ID: "p", Text: natPlanText, Pending: 2, IsAdmin: true}, applyRes: NatResult{Removed: 2, BackupPath: `C:\ProgramData\toolkit\backups\apps-nativos-20261114-1200.json`}}
		tab := natTab(t, be)
		nkey(tab, "t", "r", "s")
		golden(t, fmt.Sprintf("faixa_nat_ok_%dx%d", sz[0], sz[1]), sized(goldenOpts(), sz[0], sz[1], tab).render())
	}
}

func TestGoldenFaixaExecucaoComFalha(t *testing.T) {
	m, _, _ := runMarked(t, scriptOK)
	mkey(m, "1")
	golden(t, "faixa_exec_falha_80x24", m.render())
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	golden(t, "faixa_exec_falha_120x40", m.render())
}

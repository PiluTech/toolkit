package tui

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
)

// Com o painel de detalhe (>= 110 colunas) a lista fica estreita: nenhuma linha da lista pode cortar com
// reticências nem passar da largura. Dados de pior caso: versão com época, nomes de até 11 colunas (nome mais longo corta, inteiro no painel e na linha de estado), perfil recomendado.
func worstTabs(t *testing.T) map[int]Tab {
	t.Helper()
	tabs := detailGoldenTabs(t)
	be := &fakeBackend{
		apps: []AppInfo{
			{ID: "git", Name: "Git", Category: "dev", Managers: []string{"apt", "flatpak"}},
			{ID: "code", Name: "Visual Code", Category: "dev", Managers: []string{"flatpak"}, Versions: true},
			{ID: "ff", Name: "Firefox", Category: "navegadores", Managers: []string{"flatpak"}},
			{ID: "pg", Name: "PostgreSQL", Category: "dev", Managers: []string{"apt"}},
		},
		status: map[string]AppStatus{
			"git":  {Installed: true, Version: "1:2.43.0-1ubuntu7.1"},
			"code": {Installed: true, External: true, ExternalManager: "Homebrew", Version: "1:2.43.0-1ubuntu7.1"},
			"ff":   {Installed: true, Version: "133.0"},
			"pg":   {Installed: true, Version: "16.4-0ubuntu0.24.04.1"},
		},
	}
	tabs[idxApps] = loaded(t, be)
	p := tabs[idxPriv].(*PrivacyTab)
	pkey(p, "down") // perfil recomendado
	return tabs
}

func TestListaComPainelNuncaCortaComReticencias(t *testing.T) {
	for _, tc := range []struct {
		name string
		idx  int
	}{{"apps", idxApps}, {"priv", idxPriv}} {
		for _, w := range []int{110, 115, 120, 130} {
			m := detailGoldenModel(w, 36, tc.idx, worstTabs(t))
			for _, l := range strings.Split(plain(m.render()), "\n") {
				left, _, ok := strings.Cut(l, "│")
				if !ok {
					continue // rodapé, cabeçalho
				}
				if strings.Contains(left, "…") {
					t.Errorf("%s w=%d: lista corta com reticências: %q", tc.name, w, left)
				}
				if lipgloss.Width(l) > w {
					t.Errorf("%s w=%d: linha passa da largura: %q", tc.name, w, l)
				}
			}
		}
	}
}

func TestListaComPainelMostraAInformacaoCompleta(t *testing.T) {
	for _, w := range []int{110, 115, 120, 130} {
		v := plain(detailGoldenModel(w, 36, idxApps, worstTabs(t)).render())
		left := leftColumn(v)
		for _, want := range []string{"1:2.43.0-1ubuntu7.1", "16.4-0ubuntu0.24.04.1"} {
			if !strings.Contains(left, want) {
				t.Errorf("apps w=%d: falta a versão %q na lista:\n%s", w, want, left)
			}
		}
		pv := leftColumn(plain(detailGoldenModel(w, 36, idxPriv, worstTabs(t)).render()))
		flat := strings.Join(strings.Fields(pv), " ")
		for _, want := range []string{"só o que é do usuário · não exige administrador", "básico + política, serviço e tarefas · exige administrador"} {
			if !strings.Contains(flat, want) {
				t.Errorf("priv w=%d: falta a descrição %q:\n%s", w, want, pv)
			}
		}
	}
}

func leftColumn(v string) string {
	var out []string
	for _, l := range strings.Split(v, "\n") {
		left, _, ok := strings.Cut(l, "│")
		if ok {
			out = append(out, left)
		}
	}
	return strings.Join(out, "\n")
}

// Versão extrema (época + esm + sufixo): a versão continua inteira; quem cede é o nome (inteiro no painel).
func TestListaComPainelVersaoExtremaNaoCorta(t *testing.T) {
	const ver = "1:2.43.0-1ubuntu7.3+esm2~24.04.1"
	for _, w := range []int{110, 115, 120, 130} {
		tabs := detailGoldenTabs(t)
		tabs[idxApps] = loaded(t, &fakeBackend{
			apps:   []AppInfo{{ID: "git", Name: "Git", Category: "dev", Managers: []string{"apt"}}},
			status: map[string]AppStatus{"git": {Installed: true, Version: ver}},
		})
		left := leftColumn(plain(detailGoldenModel(w, 36, idxApps, tabs).render()))
		if !strings.Contains(left, ver) || strings.Contains(left, "…") {
			t.Errorf("w=%d: a versão tem de aparecer inteira:\n%s", w, left)
		}
	}
}

// O aviso "Nunca rodou" da aba Diagnóstico quebra na largura da lista, nunca termina em reticências.
func TestDiagnosticoAvisoNuncaRodouQuebra(t *testing.T) {
	for _, w := range []int{110, 115, 120, 130} {
		v := leftColumn(plain(detailGoldenModel(w, 36, idxDiag, worstTabs(t)).render()))
		if strings.Contains(v, "…") {
			t.Errorf("w=%d: reticências na lista:\n%s", w, v)
		}
		if flat := strings.Join(strings.Fields(v), " "); !strings.Contains(flat, "a Revisão mostra o que será executado.") {
			t.Errorf("w=%d: aviso incompleto:\n%s", w, v)
		}
	}
}

// Lupa #75: app INSTALADO com versão longa e marcado para uma versão FIXA: a versão escolhida aparece inteira (a medida
// e o desenho usam a mesma composição: a marcada vale no lugar da instalada).
func TestAppInstaladoComVersaoLongaEMarcadoMostraAVersaoEscolhidaInteira(t *testing.T) {
	const longa = "1:2.43.0-1ubuntu7.3+esm2~24.04.1"
	for _, w := range []int{110, 115, 120, 130} {
		tabs := worstTabs(t)
		apps := tabs[idxApps].(*AppsTab)
		for i := range apps.apps {
			_ = i
		}
		apps.be.(*fakeBackend).status["code"] = AppStatus{Installed: true, Version: longa}
		apps.status["code"] = AppStatus{Installed: true, Version: longa}
		apps.marks["code"] = "3.12"
		m := detailGoldenModel(w, 36, idxApps, tabs)
		out := plain(m.render())
		var row string
		for _, l := range strings.Split(out, "\n") {
			if left, _, ok := strings.Cut(l, "│"); ok && strings.Contains(left, "Visual Code") {
				row = left
			}
		}
		if !strings.Contains(row, "→ 3.12") {
			t.Errorf("w=%d: a versão escolhida (→ 3.12) tem de aparecer inteira na linha: %q", w, row)
		}
		if strings.Contains(row, "…") {
			t.Errorf("w=%d: a linha corta com reticências: %q", w, row)
		}
	}
}

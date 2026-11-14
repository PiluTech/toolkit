package cli

import (
	"fmt"

	"github.com/PiluTech/toolkit/internal/msg"
	"github.com/PiluTech/toolkit/internal/tui"
)

// inicioGoal descreve um objetivo do Início: o texto e o destino (a chave da área e o título da seção dentro dela).
type inicioGoal struct {
	title, desc, area, section string
}

// newInicioTab monta o Início da TUI ("O que você quer fazer?"): um objetivo por linha, com a área e a seção de destino
// achadas pelas outras áreas (areas, as de índice 1 em diante; o Início é a área 0), então a ordem delas pode mudar sem
// desviar o Enter. As etiquetas vêm só do que o cli já sabe (goos e admin): nada é lido do sistema.
func newInicioTab(areas []tui.AreaDef, goos string, admin bool, th tui.Theme) tui.Tab {
	goals := []inicioGoal{
		{msg.TUIInicioApps, msg.TUIInicioAppsDesc, "instalar", "Apps"},
		{msg.TUIInicioReceitas, msg.TUIInicioReceitasDesc, "instalar", "Receitas"},
		{msg.TUIInicioSaude, msg.TUIInicioSaudeDesc, "maquina", "Diagnóstico"},
		{msg.TUIInicioNativos, msg.TUIInicioNativosDesc, "maquina", "Apps nativos"},
		{msg.TUIInicioOtimizacao, msg.TUIInicioOtimizacaoDesc, "maquina", msg.TUISecaoOtimizacao},
		{msg.TUIInicioPrivacidade, msg.TUIInicioPrivDesc, "seguranca", "Privacidade"},
		{msg.TUIInicioRede, msg.TUIInicioRedeDesc, "rede", msg.TUISecaoAnalise},
		{msg.TUIInicioHistorico, msg.TUIInicioHistoricoDesc, "historico", msg.TUISecaoExecucoes},
	}
	var items []tui.InicioItem
	for _, g := range goals {
		it := tui.InicioItem{Title: g.title, Desc: g.desc, Tag: inicioTag(g.section, goos, admin)}
		for ai, a := range areas {
			if a.Key != g.area {
				continue
			}
			for si, t := range a.Tabs {
				if t.Title() == g.section {
					// o Início é a área 0: as demais começam em 1
					it.Area, it.Section = ai+1, si
					it.Dest = fmt.Sprintf("%d %s › %s", ai+1, a.Title, t.Title())
				}
			}
		}
		items = append(items, it)
	}
	return tui.NewInicioTab(th, msg.TUIAreaInicio, msg.TUIAreaInicioShort, msg.TUIInicioPergunta, items)
}

// inicioTag é a etiqueta de estado ou requisito de um objetivo, só com o que o cli já sabe: GOOS e administrador.
func inicioTag(section, goos string, admin bool) string {
	switch section {
	case "Apps nativos", msg.TUISecaoOtimizacao:
		if goos != "windows" {
			return msg.TUIInicioTagSoWindows
		}
	case "Privacidade":
		switch {
		case goos != "windows":
			return msg.TUIInicioTagSoWindows
		case !admin:
			return msg.TUIInicioTagAdmin
		}
	}
	return ""
}

package animacoes

import (
	"github.com/PiluTech/toolkit/internal/diagfmt"
	"github.com/PiluTech/toolkit/internal/privacidade"
)

// Item é um valor do registro que o toolkit define para reduzir a animação. O catálogo é FECHADO e revisável: só entram
// valores confirmados em fonte da Microsoft (ver docs/otimizacao.md, "A VERIFICAR" para o que ficou de fora), todos no
// HKCU, nenhum serviço, tarefa ou máscara binária.
type Item struct {
	ID        string
	Key, Name string
	Value     RegValue
	Title     func(Lang) string
	Why       func(Lang) string
	// Effect diz, sem prometer ganho, o que muda na tela e quando.
	Effect func(Lang) string
}

func tr(pt, en, es string) func(Lang) string {
	return func(l Lang) string { return diagfmt.T(l, pt, en, es) }
}

// Items devolve TODOS os itens (o catálogo fechado).
//
// MinAnimate: REG_SZ "0"/"1" em HKCU\Control Panel\Desktop\WindowMetrics (referência de chaves de registro do Windows
// no TechNet: tipo REG_SZ, faixa 0 ou 1, padrão 1). Fora daqui, por falta de fonte oficial: TaskbarAnimations e
// VisualFXSetting (só relatos da comunidade).
func Items() []Item {
	return []Item{
		{ID: "animar-janelas",
			Key: `HKCU\Control Panel\Desktop\WindowMetrics`, Name: "MinAnimate", Value: RegValue{Kind: privacidade.RegString, Str: "0"},
			Title:  tr("Animação de janelas ao minimizar e maximizar desligada", "Window minimize/maximize animation off", "Animación de ventanas al minimizar y maximizar desactivada"),
			Why:    tr("Valor MinAnimate do usuário atual: com \"0\" o Windows não anima as janelas.", "Current user's MinAnimate value: with \"0\" Windows does not animate windows.", "Valor MinAnimate del usuario actual: con \"0\" Windows no anima las ventanas."),
			Effect: tr("As janelas minimizam e maximizam sem animação.", "Windows minimize and maximize without animation.", "Las ventanas se minimizan y maximizan sin animación.")},
	}
}

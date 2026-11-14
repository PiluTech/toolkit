package tui

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/rivo/uniseg"

	"github.com/PiluTech/toolkit/internal/textsafe"
)

type appMode int

const (
	modeNav    appMode = iota // navegar nas abas
	modeReview                // tela de Revisão do plano
	modeRun                   // execução em andamento (aba Log; as outras só leitura)
)

type planReadyMsg struct {
	id   int // da Revisão que pediu o plano: plano de outra Revisão é ignorado
	plan Plan
	err  error
}

type runEventMsg struct{ ev Event }

type runClosedMsg struct{}

// reviewScreen é a "Revisão do plano": a seleção, os avisos, o que muda e os comandos exatamente como
// serão executados. Foco inicial em Voltar (decisão 2); `s` executa.
type reviewScreen struct {
	id      int       // identifica esta Revisão (um plano atrasado de outra nunca a preenche)
	sel     Selection // a seleção que foi planejada: é ESSA que Executar entrega ao executor
	theme   Theme
	loading bool
	err     error
	plan    Plan
	focus   int // 0 Voltar, 1 Executar
	scroll  int
	frame   int
}

func newReview(th Theme, id int, sel Selection) *reviewScreen {
	return &reviewScreen{theme: th, loading: true, id: id, sel: sel}
}

// canRun: Executar só existe com plano pronto, sem problemas e com algo a fazer.
func (r *reviewScreen) canRun() bool {
	return !r.loading && r.err == nil && len(r.plan.Problems) == 0 && !r.plan.NothingToDo
}

func (r *reviewScreen) lines(w int) []string {
	t := r.theme
	if r.loading {
		return []string{" " + t.Accent().Render(t.Spin(r.frame)) + " Lendo o estado do sistema…"}
	}
	var out []string
	add := func(s ...string) { out = append(out, s...) }
	add(" " + t.Title().Render(titleCase(t, "Revisão do plano")))
	if r.err != nil {
		add("", " "+t.Err().Render(SymFail+" erro:")+" não deu para montar o plano", " "+textsafe.Line(r.err.Error()), "", " Nada foi executado.")
		return out
	}
	if r.plan.NothingToDo {
		add("", " Nada a fazer: tudo já está instalado.")
		return out
	}
	add(wrap(" "+textsafe.Line(r.plan.Summary), w, 1)...)
	add(wrap(" "+textsafe.Line(r.plan.Env), w, 1)...)
	add("")
	if len(r.plan.Problems) > 0 {
		add(" " + t.Err().Render(titleCase(t, "Não dá para executar")))
		for _, p := range r.plan.Problems {
			add(wrap(" "+SymFail+" "+textsafe.Line(p), w, 5)...)
		}
		add("", " Nada foi executado.")
		return out
	}
	if n := len(r.plan.Warnings); n > 0 {
		add(" " + t.Warn().Render(titleCase(t, "Avisos ("+itoa(n)+")")))
		for _, x := range r.plan.Warnings {
			add(wrap(" "+SymWarn+" "+textsafe.Line(x), w, 5)...)
		}
		add("")
	}
	if len(r.plan.Changes) > 0 {
		add(" " + t.Title().Render(titleCase(t, "O que muda")))
		for _, x := range r.plan.Changes {
			add(wrap("   "+textsafe.Line(x), w, 5)...)
		}
		add("")
	}
	if len(r.plan.Commands) > 0 {
		add(" " + t.Title().Render(titleCase(t, "Exatamente como será executado")))
		for _, x := range r.plan.Commands {
			add(wrap("   "+textsafe.Line(x), w, 5)...) // quebra com recuo, nunca corta o comando
		}
	}
	return out
}

// wrap quebra s em linhas de até w colunas com recuo nas continuações; palavra maior que a linha é
// quebrada com "\" no fim (comando nunca é cortado).
func wrap(s string, w, indent int) []string {
	if w < 10 || lipgloss.Width(s) <= w {
		return []string{s}
	}
	pad := strings.Repeat(" ", indent)
	var out []string
	cur := ""
	for _, word := range strings.Split(s, " ") {
		for lipgloss.Width(word) > w-indent {
			room := w - indent - 1 // sobra a coluna da "\\" do fim
			if cur != "" && cur != pad {
				out, cur = append(out, cur), pad
			}
			head, rest := splitCells(word, room)
			out = append(out, cur+head+"\\")
			cur, word = pad, rest
		}
		switch {
		case cur == "" || cur == pad:
			cur += word
		case lipgloss.Width(cur)+1+lipgloss.Width(word) <= w:
			cur += " " + word
		default:
			out, cur = append(out, cur), pad+word
		}
	}
	if strings.TrimSpace(cur) != "" {
		out = append(out, cur)
	}
	return out
}

// buttons é a linha de estado da Revisão: o botão em foco leva ">" (nunca só reverso).
func (r *reviewScreen) buttons() string {
	back, run := "  [ Voltar ]", "  [ Executar ]"
	if r.focus == 0 {
		back = "> [ Voltar ]"
	} else {
		run = "> [ Executar ]"
	}
	if !r.canRun() {
		return back
	}
	return back + "     " + run
}

// splitCells corta s em (início, resto) com o início ocupando no máximo n CÉLULAS, medindo por GRAFEMA
// (cluster de grafemas do Unicode: ❤️ = U+2764 + U+FE0F ocupa 2 células, uma família 👨‍👩‍👧 com ZWJ é um
// grafema só, um "e" + acento combinante é um só) com a mesma medida do layout (lipgloss.Width). Nunca separa
// um grafema; pelo menos um sai por chamada, para a quebra sempre avançar.
func splitCells(s string, n int) (string, string) {
	used, i := 0, 0
	rest := s
	for rest != "" {
		cluster, next, _, _ := uniseg.FirstGraphemeClusterInString(rest, -1)
		w := lipgloss.Width(cluster)
		if used+w > n && i > 0 {
			return s[:i], s[i:]
		}
		used += w
		i += len(cluster)
		rest = next
	}
	return s, ""
}

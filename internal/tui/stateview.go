package tui

import (
	"strings"
	"unicode/utf8"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/PiluTech/toolkit/internal/textsafe"
)

// Estados padronizados (fase 5 do redesign, docs/tui.md "Estados"). Toda tela que não é a lista normal (carregando,
// erro, sem administrador, fora do Windows, vazio, terminal pequeno) tem a MESMA forma, de cima para baixo:
//
//	TÍTULO  símbolo + palavra na frente ("✗ Erro", "! Precisa de administrador", "… Carregando")
//	FRASE   uma frase do que houve (o motivo do erro vem logo abaixo dela)
//	PASSOS  "1. ..." do que fazer
//	"Nada foi alterado."  nos estados de erro e de recusa
//
// Cada função devolve EXATAMENTE h linhas de no máximo w colunas, quebra o texto nos espaços (nunca corta com
// reticências), passa todo texto de fora por textsafe e, quando h é pequeno, abandona primeiro o que vale menos:
// título, depois a frase, depois "Nada foi alterado.", depois os passos e por fim o resto do motivo. Cor só no
// símbolo; em Mono sai símbolo e palavra, sem sequência de estilo.

// Símbolos dos estados que ainda não existiam em theme.go.
const (
	SymInfo = "i" // título informativo (vazio, fora do sistema, terminal pequeno)
	SymLoad = "…" // carregando sem movimento (--no-motion): fixo, sempre com a palavra "Carregando" ao lado
)

// Títulos fixos de cada estado (símbolo + palavra).
const (
	titleLoading     = "Carregando"
	titleError       = SymFail + " Erro"
	titleNeedsAdmin  = SymWarn + " Precisa de administrador"
	titleUnsupported = SymInfo + " Não disponível neste sistema"
	titleEmpty       = SymInfo + " Nada por aqui"
	titleSoon        = SymInfo + " Ainda não existe nesta versão"
	titleTooSmall    = SymInfo + " Terminal pequeno"

	// NothingChanged é a última linha de todo erro ou recusa.
	NothingChanged = "Nada foi alterado."
	// NeverElevates diz, em todo pedido de administrador, que o toolkit não sobe de privilégio sozinho.
	NeverElevates = "O toolkit nunca eleva sozinho."
)

// stateBlock é o conteúdo de um estado já em texto puro (limpo por textsafe), antes de quebrar e pintar.
type stateBlock struct {
	title     string   // já pintado (símbolo colorido + palavra)
	lead      []string // a frase
	detail    []string // o motivo (erro, razão): linhas extras, entram depois da frase
	hints     []string // linhas sem número (contexto, ex.: "Esc cancela.")
	steps     []string // passos numerados
	tail      string   // "Nada foi alterado." ou "q sair"
	tailFirst bool     // a cauda vem ANTES da frase na prioridade (o "q sair" do terminal pequeno sobrevive a tudo)
}

// StateLoading é o estado carregando: spinner animado (ou "…" fixo com --no-motion) e a palavra "Carregando", a
// frase "Lendo <what>…" e, se a aba cancela, as dicas extras (ex.: "Esc cancela."). frame é o quadro do spinner.
func StateLoading(th Theme, w, h int, what string, frame int, hints ...string) []string {
	sym := SymLoad
	if !th.NoMotion {
		sym = spinnerFrames[((frame%len(spinnerFrames))+len(spinnerFrames))%len(spinnerFrames)]
	}
	b := stateBlock{title: th.Accent().Render(sym + " " + titleLoading), lead: []string{"Lendo " + safeWhat(what, "os dados") + "…"}}
	for _, hn := range hints {
		b.hints = append(b.hints, safe(hn))
	}
	return b.render(w, h)
}

// StateError é o erro de leitura ou de execução: o que houve, o motivo (err passa por textsafe e pode ter várias
// linhas), os passos (ex.: "Tecle c para tentar de novo.") e "Nada foi alterado.".
func StateError(th Theme, w, h int, what string, err error, steps ...string) []string {
	lead := "Algo deu errado."
	if what = safeWhat(what, ""); what != "" {
		lead = "Não deu para ler " + what + "."
	}
	b := stateBlock{title: th.Err().Render(titleError), lead: []string{lead}, steps: safeAll(steps), tail: NothingChanged}
	if err != nil {
		b.detail = errorLines(err)
	}
	return b.render(w, h)
}

// StateErrorAfterChange é o erro de RELEITURA depois de uma ação que já mudou o sistema (aplicar, restaurar, remover):
// nunca diz "Nada foi alterado" (seria falso). Diz que não deu para atualizar a leitura e que o resultado da ação e o
// backup continuam valendo.
func StateErrorAfterChange(th Theme, w, h int, what string, err error, steps ...string) []string {
	lead := "Não foi possível atualizar a leitura"
	if what = safeWhat(what, ""); what != "" {
		lead += " de " + what
	}
	lead += ". As alterações já feitas continuam valendo (o backup guarda os valores originais)."
	b := stateBlock{title: th.Err().Render(titleError), lead: []string{lead}, steps: safeAll(steps)}
	if err != nil {
		b.detail = errorLines(err)
	}
	return b.render(w, h)
}

// StateNeedsAdmin é a recusa por falta de administrador. Sem passos, mostra os do Windows e os do Linux/macOS; as
// abas passam AdminSteps(goos, comando) para dar só o do sistema atual.
func StateNeedsAdmin(th Theme, w, h int, what string, steps ...string) []string {
	if len(steps) == 0 {
		steps = AdminSteps("", "")
	}
	lead := upperFirst(safeWhat(what, "Isto")) + ": precisa de administrador e este terminal não é. " + NeverElevates
	b := stateBlock{title: th.Warn().Render(titleNeedsAdmin), lead: []string{lead}, steps: safeAll(steps), tail: NothingChanged}
	return b.render(w, h)
}

// StateUnsupported é "só existe no Windows (este sistema é X)": reason diz por quê, por sistema (pode ser vazio).
func StateUnsupported(th Theme, w, h int, what, reason string, osName string) []string {
	sys := safe(osName)
	if sys == "" {
		sys = "desconhecido"
	}
	lead := upperFirst(safeWhat(what, "Isto")) + ": só existe no Windows (este sistema é " + sys + ")."
	b := stateBlock{title: th.Title().Render(titleUnsupported), lead: []string{lead}, tail: NothingChanged}
	if r := safe(reason); r != "" {
		b.detail = []string{r}
	}
	return b.render(w, h)
}

// StateEmpty é "nada por aqui": what é a frase inteira (ex.: "Nenhuma receita para este sistema.") e os passos dizem
// o que fazer para ter conteúdo. Não é erro, então não leva "Nada foi alterado.".
func StateEmpty(th Theme, w, h int, what string, steps ...string) []string {
	return stateBlock{title: th.Title().Render(titleEmpty), lead: sentenceLines(what, "Nada por aqui."), steps: safeAll(steps)}.render(w, h)
}

// StateSoon é a seção que ainda não existe nesta versão (Rede/Análise); what é a explicação.
func StateSoon(th Theme, w, h int, what string, steps ...string) []string {
	return stateBlock{title: th.Title().Render(titleSoon), lead: sentenceLines(what, ""), steps: safeAll(steps)}.render(w, h)
}

// StateTooSmall é a tela única abaixo do tamanho mínimo: título, a frase com os tamanhos, os dois caminhos e a
// dica "q sair", que sobrevive mesmo com uma linha de altura.
func StateTooSmall(th Theme, w, h, needW, needH int) []string {
	b := stateBlock{
		title:     th.Title().Render(titleTooSmall),
		lead:      []string{"A interface precisa de " + itoa(needW) + "×" + itoa(needH) + " e este tem " + itoa(w) + "×" + itoa(h) + "."},
		steps:     []string{"Aumente a janela.", "Ou rode toolkit list / toolkit install ... sem a interface."},
		tail:      "q sair",
		tailFirst: true,
	}
	return b.render(w, h)
}

// AdminSteps são os passos para abrir como administrador, por sistema ("windows", "linux", "darwin"; outro ou vazio
// dá os dois jeitos). cmd é o comando do toolkit a repetir com sudo (vazio = só "sudo toolkit").
func AdminSteps(goos, cmd string) []string {
	sudo := "sudo toolkit"
	if c := strings.TrimSpace(cmd); c != "" {
		sudo += " " + c
	}
	switch goos {
	case "windows":
		return []string{"Feche o toolkit (q).", "Botão direito no Terminal > Executar como administrador.", "Rode toolkit de novo."}
	case "linux", "darwin":
		return []string{"Feche o toolkit (q).", "Rode de novo com sudo: " + sudo + "."}
	}
	return []string{"Feche o toolkit (q).", "No Windows: botão direito no Terminal > Executar como administrador.", "No Linux ou macOS: " + sudo + "."}
}

// ---- montagem ----

func safe(s string) string { return strings.TrimSpace(textsafe.Line(s)) }

func safeAll(in []string) []string {
	var out []string
	for _, s := range in {
		if s = safe(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}

func safeWhat(what, def string) string {
	if s := safe(what); s != "" {
		return s
	}
	return def
}

// sentenceLines separa o texto em linhas (cada uma limpa por textsafe); vazio vira def.
func sentenceLines(s, def string) []string {
	var out []string
	for _, l := range strings.Split(textsafe.Multiline(s), "\n") {
		if l = safe(l); l != "" {
			out = append(out, l)
		}
	}
	if len(out) == 0 && def != "" {
		return []string{def}
	}
	return out
}

// errorLines vira a mensagem do erro em "Motivo: ..." e, se ela tiver várias linhas, as outras com recuo.
func errorLines(err error) []string {
	var out []string
	for _, l := range strings.Split(textsafe.Multiline(err.Error()), "\n") {
		if l = strings.TrimSpace(textsafe.Printable(l)); l == "" {
			continue
		}
		if len(out) == 0 {
			l = "Motivo: " + l
		} else {
			l = "  " + l
		}
		out = append(out, l)
	}
	if len(out) == 0 {
		return []string{"Motivo: sem mensagem."}
	}
	return out
}

func upperFirst(s string) string {
	r, n := utf8.DecodeRuneInString(s)
	if n == 0 {
		return s
	}
	return strings.ToUpper(string(r)) + s[n:]
}

// wrapped quebra text em linhas de até w colunas, com o prefixo first na primeira e hang nas seguintes. Nunca corta
// com reticências: o que não cabe numa linha desce para a próxima (palavra maior que a linha é partida).
func wrapped(text string, w int, first, hang string) []string {
	room := max(w-lipgloss.Width(first), 1)
	var out []string
	for i, l := range strings.Split(ansi.Wrap(text, room, " "), "\n") {
		pre := hang
		if i == 0 {
			pre = first
		}
		out = append(out, guard(pre+strings.TrimRight(l, " "), w))
	}
	return out
}

// guard é a última rede: nenhuma linha passa de w colunas (só acontece com janelas de poucas colunas).
func guard(s string, w int) string {
	if lipgloss.Width(s) <= w {
		return s
	}
	return ansi.Truncate(s, max(w, 1), "")
}

// render quebra e distribui os blocos em exatamente h linhas, respeitando a prioridade quando falta altura.
func (b stateBlock) render(w, h int) []string {
	w = max(w, 1)
	if h <= 0 {
		return []string{}
	}
	var lead, detail, hints, steps []string
	for _, l := range b.lead {
		lead = append(lead, wrapped(l, w, " ", " ")...)
	}
	for _, l := range b.detail {
		hang := " "
		if strings.HasPrefix(l, "  ") {
			hang = "   "
		}
		detail = append(detail, wrapped(strings.TrimLeft(l, " "), w, hang, "   ")...)
	}
	for _, l := range b.hints {
		hints = append(hints, wrapped(l, w, " ", " ")...)
	}
	for i, l := range b.steps {
		num := " " + itoa(i+1) + ". "
		steps = append(steps, wrapped(l, w, num, strings.Repeat(" ", lipgloss.Width(num)))...)
	}
	var tail []string
	if b.tail != "" {
		tail = wrapped(b.tail, w, " ", " ")
	}

	// Orçamento por prioridade (cada bloco leva só o que cabe, do começo): título, [cauda], frase, 1ª linha do
	// motivo, dicas, [cauda], passos, resto do motivo, linha em branco.
	rem := h
	take := func(src []string) []string {
		n := min(len(src), max(rem, 0))
		rem -= n
		return src[:n]
	}
	var tTail []string
	if b.tailFirst {
		tTail = take(tail)
	}
	tTitle := take(wrapped(b.title, w, " ", "   "))
	tLead := take(lead)
	d0 := take(detail[:min(len(detail), 1)])
	tHints := take(hints)
	if !b.tailFirst {
		tTail = take(tail)
	}
	tSteps := take(steps)
	tDetail := take(detail[len(d0):])
	blank := rem > 0 && len(tSteps) > 0 && len(tLead)+len(d0)+len(tDetail)+len(tHints) > 0

	out := append([]string{}, tTitle...)
	out = append(out, tLead...)
	out = append(out, d0...)
	out = append(out, tDetail...)
	out = append(out, tHints...)
	if blank {
		out = append(out, "")
	}
	out = append(out, tSteps...)
	if !b.tailFirst {
		out = append(out, tTail...)
	} else {
		// a cauda do terminal pequeno é a última linha; o espaço que sobra fica entre o corpo e ela
		for len(out)+len(tTail) < h {
			out = append(out, "")
		}
		out = append(out, tTail...)
	}
	for len(out) < h {
		out = append(out, "")
	}
	return out[:h]
}

// adminResultLines são as linhas da faixa de resultado quando a ação exigiu administrador (as abas de privacidade e
// de apps nativos só existem no Windows): a recusa, os passos e a frase de que o toolkit não eleva sozinho.
func adminResultLines() []string {
	steps := AdminSteps("windows", "")
	steps[len(steps)-1] += " " + NeverElevates
	lines := []string{NothingChanged}
	for i, s := range steps {
		lines = append(lines, itoa(i+1)+". "+s)
	}
	return lines
}

// unsupportedWhy é o motivo, por sistema, de uma seção só do Windows não existir aqui.
func unsupportedWhy(goos, what string) string {
	switch goos {
	case "darwin":
		return "No macOS " + what + " não usam o registro nem as políticas do Windows."
	case "linux":
		return "No Linux " + what + " não usam o registro nem as políticas do Windows."
	}
	return upperFirst(what) + " dependem do registro e das políticas do Windows."
}

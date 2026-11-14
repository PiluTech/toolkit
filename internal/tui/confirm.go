package tui

import (
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/PiluTech/toolkit/internal/textsafe"
)

// PlanLine é uma linha estruturada do plano: o que muda (atual -> novo). Admin marca o item que exige
// administrador com "[admin]" em negrito (nunca só cor).
type PlanLine struct {
	Label  string
	From   string
	To     string
	Detail string
	Admin  bool
}

// Button é um botão da confirmação. Disabled não ativa nem recebe foco e aparece como
// "( Label: Reason )".
type Button struct {
	Label    string
	Key      string // "n" = cancelar (nunca confirma); qualquer outra = confirmar
	Disabled bool
	Reason   string
}

// ConfirmResult é o que uma tecla produziu na confirmação.
type ConfirmResult int

// Resultados de Confirm.Update.
const (
	ConfirmNone   ConfirmResult = iota // nada decidido (foco, rolagem ou tecla ignorada)
	ConfirmAccept                      // o botão de confirmar foi ativado
	ConfirmCancel                      // cancelado
)

// Confirm é a tela única de confirmação: título, resumo, aviso, backup e como desfazer, o PLANO inteiro
// (rolável) e só depois os botões. O foco inicial é Cancelar (padrão seguro); só ativa quem o usuário
// escolheu (Enter no botão, "s"/"y" para confirmar). Esc e "n" cancelam; qualquer outra tecla não confirma.
type Confirm struct {
	Title, Summary, Warn, Backup, Undo string
	Plan                               []PlanLine
	// Text é o plano em texto livre (o que o backend já formatou); usado além de Plan, como corpo rolável.
	Text    string
	Buttons []Button
	Focus   int

	scroll int
	room   int // linhas visíveis do plano na última desenhada (tamanho da página)
	total  int // linhas do plano na última desenhada
}

// NewConfirm monta a confirmação com os botões [Cancelar, accept]; o foco começa em Cancelar. Quando
// disabledReason não é vazio o botão de confirmar fica indisponível com esse motivo.
func NewConfirm(accept, disabledReason string) *Confirm {
	return &Confirm{Buttons: []Button{
		{Label: "Cancelar", Key: "n"},
		{Label: accept, Key: "s", Disabled: disabledReason != "", Reason: disabledReason},
	}}
}

func (c *Confirm) isCancel(i int) bool { return c.Buttons[i].Key == "n" }

// canAccept diz se existe um botão de confirmar ativo.
func (c *Confirm) canAccept() bool {
	for i, b := range c.Buttons {
		if !c.isCancel(i) && !b.Disabled {
			return true
		}
	}
	return false
}

// Hint é a dica curta de teclas da linha de status durante a confirmação.
func (c *Confirm) Hint() string {
	if c.canAccept() {
		return "Tab muda o botão · Enter ativa · s confirma · Esc cancela"
	}
	return "Enter ou Esc cancelam · ↑↓ rolam o plano"
}

// keyHints são os atalhos da barra de teclas durante a confirmação ("s" só aparece quando confirmar é possível).
func (c *Confirm) keyHints() []KeyHint {
	if c.canAccept() {
		return []KeyHint{{"Tab", "botão"}, {"Enter", "ativa"}, {"s", "confirma"}, {"Esc", "cancela"}, {"↑↓", "rolar"}}
	}
	return []KeyHint{{"Enter", "cancela"}, {"Esc", "cancela"}, {"↑↓", "rolar"}}
}

func (c *Confirm) move(d int) {
	n := len(c.Buttons)
	for i := 1; i <= n; i++ {
		j := ((c.Focus+d*i)%n + n) % n
		if !c.Buttons[j].Disabled {
			c.Focus = j
			return
		}
	}
}

// Update trata uma tecla. Só devolve ConfirmAccept para botão ativo (Enter focado, "s", "y"); Esc nunca confirma.
func (c *Confirm) Update(k string) ConfirmResult {
	if c.Focus < 0 || c.Focus >= len(c.Buttons) || c.Buttons[c.Focus].Disabled {
		c.Focus = 0 // Cancelar é sempre o primeiro e nunca fica desabilitado
	}
	switch k {
	case "tab", "right", "l":
		c.move(1)
	case "shift+tab", "left", "h":
		c.move(-1)
	case "enter":
		if c.isCancel(c.Focus) {
			return ConfirmCancel
		}
		return ConfirmAccept
	case "s", "S", "y", "Y":
		if c.canAccept() {
			return ConfirmAccept
		}
	case "n", "N", "esc", "escape":
		return ConfirmCancel
	case "up":
		c.scroll = max(c.scroll-1, 0)
	case "down":
		c.scroll++
	case "pgup":
		c.scroll = max(c.scroll-max(c.room-1, 1), 0)
	case "pgdown":
		c.scroll += max(c.room-1, 1)
	}
	return ConfirmNone
}

func (c *Confirm) buttonTokens(th Theme) []string {
	out := make([]string, len(c.Buttons))
	for i, b := range c.Buttons {
		lab := textsafe.Line(b.Label)
		switch {
		case b.Disabled:
			out[i] = "( " + lab + ": " + textsafe.Line(b.Reason) + " )"
		case i == c.Focus:
			out[i] = th.Active().Render(">[ " + lab + " ]<")
		default:
			out[i] = " [ " + lab + " ] "
		}
	}
	return out
}

func (c *Confirm) buttonLines(th Theme, w int) []string {
	toks := c.buttonTokens(th)
	one := " " + strings.Join(toks, "   ")
	if lipgloss.Width(one) <= w {
		return []string{one}
	}
	var out []string
	for _, tk := range toks {
		out = append(out, wrapLine(" "+tk, w)...)
	}
	return out
}

func (c *Confirm) labelled(th Theme, label, text string, w int) []string {
	if strings.TrimSpace(text) == "" {
		return nil
	}
	return wrapLine(" "+th.Title().Render(label)+" "+textsafe.Line(text), w)
}

func (c *Confirm) headLines(th Theme, w int) []string {
	var out []string
	add := func(s ...string) { out = append(out, s...) }
	if c.Title != "" {
		add(" " + th.Title().Render(titleCase(th, textsafe.Line(c.Title))))
	}
	if c.Summary != "" {
		add(wrapLine(" "+textsafe.Line(c.Summary), w)...)
	}
	if c.Warn != "" {
		add(wrapLine(" "+th.Warn().Render(SymWarn+" aviso:")+" "+textsafe.Line(c.Warn), w)...)
	}
	add(c.labelled(th, "Backup:", c.Backup, w)...)
	add(c.labelled(th, "Como desfazer:", c.Undo, w)...)
	return out
}

func (c *Confirm) planLines(th Theme, w int) []string {
	var out []string
	for _, p := range c.Plan {
		head := " " + textsafe.Line(p.Label)
		if p.Admin {
			head += " " + th.Title().Render("[admin]")
		}
		out = append(out, wrapLine(head, w)...)
		if p.From != "" || p.To != "" {
			out = append(out, wrapLine("     "+textsafe.Line(p.From)+" -> "+textsafe.Line(p.To), w)...)
		}
		if p.Detail != "" {
			out = append(out, wrapLine("     "+textsafe.Line(p.Detail), w)...)
		}
	}
	if strings.TrimSpace(c.Text) != "" {
		for _, l := range strings.Split(strings.TrimRight(textsafe.Multiline(c.Text), "\n"), "\n") {
			if strings.TrimSpace(l) == "" {
				out = append(out, "")
				continue
			}
			out = append(out, wrapLine(" "+l, w)...)
		}
	}
	return out
}

// View desenha exatamente h linhas de no máximo w colunas: a EVIDÊNCIA (cabeçalho + plano) como UMA lista rolável e, por
// último, os botões (sempre visíveis). Cabeçalho e plano rolam juntos: em qualquer altura suportada toda linha da evidência
// é alcançável (nunca fica área rolável vazia).
func (c *Confirm) View(th Theme, w, h int) []string {
	btn := c.buttonLines(th, w)
	evidence := c.headLines(th, w)
	if plan := c.planLines(th, w); len(plan) > 0 {
		if len(evidence) > 0 {
			evidence = append(evidence, "")
		}
		evidence = append(evidence, plan...)
	}
	c.total = len(evidence)

	if len(btn) >= h { // altura ínfima: só os botões que cabem
		c.room = 1
		return fitLines(btn, h)
	}
	space := h - len(btn)
	if len(evidence) <= space-0 && len(evidence)+1+len(btn) <= h { // tudo cabe: os botões logo depois
		out := append(append([]string{}, evidence...), "")
		c.room, c.scroll = max(space, 1), 0
		return fitLines(append(out, btn...), h)
	}
	body := space
	if space > 1 {
		body = space - 1 // uma linha para o indicador "↓ mais N linhas"
	}
	c.room = max(body, 1)
	c.scroll = max(min(c.scroll, max(len(evidence)-body, 0)), 0)
	end := min(c.scroll+body, len(evidence))
	out := append([]string{}, evidence[c.scroll:end]...)
	if space > 1 {
		left := len(evidence) - end
		ind := " " + SymMore + " mais " + plural(left, "linha", "linhas")
		if left == 0 {
			ind = " " + SymUpdate + " fim (PgUp volta)"
		}
		out = append(out, th.Muted().Render(ind))
	}
	for len(out) < space {
		out = append(out, "")
	}
	out = append(out[:space], btn...)
	return fitLines(out, h)
}

// fitLines devolve exatamente h linhas (completa com vazias, corta o excesso do fim).
func fitLines(l []string, h int) []string {
	for len(l) < h {
		l = append(l, "")
	}
	return l[:h]
}

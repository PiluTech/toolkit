package tui

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/PiluTech/toolkit/internal/textsafe"
)

type logItem struct {
	name   string
	status ItemStatus
	dur    time.Duration
	err    string
	lines  []string
}

// LogTab é a aba Log: os itens do lote em cima (símbolo, nome, estado, duração) e, embaixo, o log do item
// sob o cursor. Linha com \r substitui a última em vez de empilhar; "f" segue o fim.
type LogTab struct {
	theme    Theme
	items    []logItem
	cur      int
	scroll   int // linhas rolando para cima a partir do fim; só vale com follow desligado
	follow   bool
	frame    int
	running  bool
	title    string // "Terminou com 1 falha · 2 ok · 1 falhou"
	logPath  string
	logWarn  string
	lastH    int
	question string // pergunta [s/N] pendente (reinício, interromper)
}

// NewLogTab cria a aba. logPath aparece no estado vazio ("Logs anteriores: <pasta>").
func NewLogTab(th Theme, logPath string) *LogTab {
	return &LogTab{theme: th, follow: true, logPath: textsafe.Line(logPath)}
}

// Title implementa Tab.
func (t *LogTab) Title() string { return "Log" }

// Short implementa Tab.
func (t *LogTab) Short() string { return "Log" }

// Init implementa Tab.
func (t *LogTab) Init() tea.Cmd { return nil }

// Capturing implementa Tab.
func (t *LogTab) Capturing() bool { return false }

// Selected implementa Tab.
func (t *LogTab) Selected() int { return 0 }

// Running diz se há execução em andamento.
func (t *LogTab) Running() bool { return t.running }

// Summary é o título do resumo final ("" enquanto roda ou sem execução).
func (t *LogTab) Summary() string { return t.title }

// Outcome resume a execução para a faixa de resultado da aba de origem (nil = nada rodou ou ainda roda).
func (t *LogTab) Outcome() *Result {
	if t.running || len(t.items) == 0 {
		return nil
	}
	var ok, stopped int
	var failed, reboot []string
	for _, it := range t.items {
		switch it.status {
		case ItemOK, ItemSkipped:
			ok++
		case ItemFailed:
			failed = append(failed, it.name)
		case ItemReboot:
			reboot = append(reboot, it.name)
		case ItemNotRun, ItemCanceled, ItemQueued, ItemRunning:
			stopped++
		}
	}
	head := "Instalação concluída"
	switch {
	case stopped > 0:
		head = "Execução interrompida"
	case len(reboot) > 0:
		head = "Pendente de reinício" // a execução NÃO terminou: há receita esperando reinício
	}
	// A pendência de reinício vem PRIMEIRO no título: a faixa compacta (uma linha) corta o fim, nunca o começo.
	var parts []string
	if len(reboot) > 0 {
		parts = append(parts, fmt.Sprintf("%d aguardando reinício", len(reboot)))
	}
	if ok > 0 || len(reboot) == 0 {
		parts = append(parts, fmt.Sprintf("%d ok", ok))
	}
	if len(failed) > 0 {
		parts = append(parts, fmt.Sprintf("%d falharam", len(failed)))
	}
	if stopped > 0 {
		parts = append(parts, fmt.Sprintf("%d não executados", stopped))
	}
	r := &Result{Kind: ResultOK, Title: head + ": " + strings.Join(parts, ", ")}
	switch {
	case len(failed) > 0 && ok == 0 && len(reboot) == 0:
		r.Kind = ResultError
	case len(failed) > 0 || stopped > 0:
		r.Kind = ResultPartial
	case len(reboot) > 0:
		r.Kind = ResultPending
	}
	if len(failed) > 0 {
		r.Lines = append(r.Lines, "Falharam: "+strings.Join(failed, ", "))
	}
	if len(reboot) > 0 {
		r.Lines = append(r.Lines, "Aguardando reinício: "+strings.Join(reboot, ", "))
	}
	r.Lines = append(r.Lines, "Detalhes: aba Log")
	return r
}

// Warn registra o aviso "não deu para gravar o log em arquivo" (a execução continua).
func (t *LogTab) Warn(msg string) { t.logWarn = textsafe.Line(msg) }

// Ask define a pergunta [s/N] mostrada na linha de estado ("" limpa).
func (t *LogTab) Ask(q string) { t.question = textsafe.Line(q) }

// Apply aplica um evento do executor.
func (t *LogTab) Apply(ev Event) {
	switch ev.Kind {
	case EventItems:
		t.items = make([]logItem, len(ev.Items))
		for i, n := range ev.Items {
			t.items[i] = logItem{name: textsafe.Line(n), status: ItemQueued}
		}
		t.running, t.title, t.cur, t.scroll, t.follow = true, "", 0, 0, true
	case EventItemStarted:
		if it := t.at(ev.Index); it != nil {
			it.status = ItemRunning
			if t.follow {
				t.cur = ev.Index
			}
		}
	case EventLog:
		if it := t.at(ev.Index); it != nil {
			t.append(it, ev.Line)
		}
	case EventItemDone:
		if it := t.at(ev.Index); it != nil {
			it.status, it.dur, it.err = ev.Status, ev.Duration, textsafe.Line(ev.Err)
		}
	case EventWarn:
		t.Warn(ev.Line)
	case EventDone:
		t.running, t.question = false, ""
		t.title = t.summaryTitle()
		for i := range t.items {
			if it := &t.items[i]; it.status == ItemFailed {
				t.cur = i // o cursor vai para a primeira falha
				break
			}
		}
	}
}

func (t *LogTab) at(i int) *logItem {
	if i < 0 || i >= len(t.items) {
		return nil
	}
	return &t.items[i]
}

// append junta uma linha de log; "\r" dentro da linha substitui a última (barras de progresso).
func (t *LogTab) append(it *logItem, raw string) {
	if i := strings.LastIndex(raw, "\r"); i >= 0 {
		if raw[i+1:] != "" || i == len(raw)-1 {
			raw = raw[i+1:]
		}
		if len(it.lines) > 0 {
			it.lines = it.lines[:len(it.lines)-1]
		}
	}
	it.lines = append(it.lines, textsafe.Line(raw))
}

func (t *LogTab) summaryTitle() string {
	var ok, fail, reboot int
	for _, it := range t.items {
		switch it.status {
		case ItemOK, ItemSkipped:
			ok++
		case ItemFailed:
			fail++
		case ItemReboot:
			reboot++
		}
	}
	head := "Terminou"
	if fail > 0 {
		head += " com " + plural(fail, "falha", "falhas")
	}
	parts := []string{fmt.Sprintf("%d ok", ok)}
	if fail > 0 {
		parts = append(parts, fmt.Sprintf("%d falhou", fail))
	}
	if reboot > 0 {
		parts = append(parts, fmt.Sprintf("%d aguardando reinício", reboot))
	}
	return head + " · " + strings.Join(parts, " · ")
}

// Update trata o spinner e as teclas da aba.
func (t *LogTab) Update(msg tea.Msg) (Tab, tea.Cmd) {
	switch msg := msg.(type) {
	case tickMsg:
		t.frame++
		if t.running {
			return t, t.theme.Tick()
		}
	case tea.KeyPressMsg:
		page := max(t.lastH/2, 1)
		switch msg.String() {
		case "up", "k":
			if t.focusLog() {
				t.scroll++
				t.follow = false
			} else {
				t.cur = max(t.cur-1, 0)
			}
		case "down", "j":
			if t.scroll > 0 {
				t.scroll--
			} else {
				t.cur = min(t.cur+1, max(len(t.items)-1, 0))
			}
		case "pgup":
			t.scroll += page
			t.follow = false
		case "pgdown":
			t.scroll = max(t.scroll-page, 0)
		case "f":
			t.follow = !t.follow
			if t.follow {
				t.scroll = 0
			}
		}
	}
	return t, nil
}

func (t *LogTab) focusLog() bool { return false }

// Keys implementa Tab.
func (t *LogTab) Keys() []KeyHint {
	return []KeyHint{{"↑↓", "itens"}, {"PgUp/PgDn", "rolar o log"}, {"f", "seguir o fim"}}
}

// Status implementa Tab: a pergunta pendente, o aviso de log, o resumo ou o caminho do log.
func (t *LogTab) Status() string {
	switch {
	case t.question != "":
		return t.question + " [s/N]"
	case t.logWarn != "":
		return SymWarn + " " + t.logWarn
	case t.title != "":
		return t.title
	case t.logPath != "":
		return "log: " + t.logPath
	}
	return ""
}

// More implementa Tab.
func (t *LogTab) More() int { return 0 }

func (t *LogTab) spin() string { return t.theme.Spin(t.frame) }

// View desenha os itens (até 6 linhas), a régua e o log do item sob o cursor.
func (t *LogTab) View(w, h int) string {
	t.lastH = h
	if len(t.items) == 0 {
		msg := " Nada rodou nesta sessão. Marque apps ou receitas e tecle Enter."
		if t.logPath != "" {
			msg += "\n Logs anteriores: " + t.logPath
		}
		return msg
	}
	var out []string
	if t.title != "" {
		out = append(out, " "+t.theme.Title().Render(titleCase(t.theme, t.title)))
	}
	rows := min(len(t.items), 6)
	top := max(min(t.cur-rows+1, len(t.items)-rows), 0)
	for i := top; i < len(t.items) && i < top+rows; i++ {
		out = append(out, t.itemLine(i, w))
	}
	cur := t.items[t.cur]
	title := " " + cur.name + " "
	out = append(out, clip(SymRule+t.theme.Title().Render(title)+strings.Repeat(SymRule, max(w-lipgloss.Width(title)-1, 1)), w))
	room := max(h-len(out), 1)
	lines := cur.lines
	if len(lines) == 0 {
		lines = []string{t.theme.Muted().Render("(sem saída ainda)")}
	}
	end := len(lines) - t.scroll
	end = max(min(end, len(lines)), min(room, len(lines)))
	start := max(end-room, 0)
	for _, l := range lines[start:end] {
		out = append(out, " "+l)
	}
	for i := range out {
		out[i] = clip(out[i], w)
	}
	return strings.Join(out, "\n")
}

func (t *LogTab) itemLine(i, w int) string {
	it := t.items[i]
	cursor := " "
	if i == t.cur {
		cursor = ">"
	}
	sym, word, style := t.itemState(it)
	dur := ""
	if it.dur > 0 {
		dur = it.dur.Round(time.Second).String()
	}
	name := cell(it.name, max(w-3-2-26-2-8, 12))
	if i == t.cur {
		name = t.theme.Bold().Render(name)
	}
	return " " + cursor + " " + name + "  " + style.Render(cell(sym+" "+word, 26)) + "  " + t.theme.Muted().Render(dur)
}

func (t *LogTab) itemState(it logItem) (string, string, Paint) {
	switch it.status {
	case ItemOK:
		return SymOK, "ok", t.theme.OK()
	case ItemSkipped:
		return SymSkipped, "pulado", t.theme.Muted()
	case ItemFailed:
		return SymFail, "falhou", t.theme.Err()
	case ItemReboot:
		return SymWarn, "aguardando reinício", t.theme.Warn()
	case ItemRunning:
		return t.spin(), "executando", t.theme.Accent()
	case ItemCanceled:
		return SymWarn, "interrompido", t.theme.Warn()
	case ItemNotRun:
		return SymPending, "não executado", t.theme.Muted()
	}
	return SymPending, "na fila", Paint{}
}

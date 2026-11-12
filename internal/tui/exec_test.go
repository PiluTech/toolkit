package tui

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

type fakePlanner struct {
	plan Plan
	err  error
	got  Selection
}

func (f *fakePlanner) Plan(_ context.Context, s Selection) (Plan, error) {
	f.got = s
	return f.plan, f.err
}

// fakeRunner roda um roteiro de eventos; observa o cancelamento do ctx.
type fakeRunner struct {
	mu     sync.Mutex
	script func(ctx context.Context, sel Selection, ch chan<- Event)
	gotSel Selection
}

func (f *fakeRunner) Run(ctx context.Context, sel Selection, ch chan<- Event) {
	defer close(ch)
	f.mu.Lock()
	f.gotSel = sel
	f.mu.Unlock()
	f.script(ctx, sel, ch)
}

func okPlan() Plan {
	return Plan{
		Summary:  "Vai instalar 1 app e rodar 1 receita. Nada foi executado ainda.",
		Env:      "Linux amd64 · usuário ana",
		Warnings: []string{"O apt roda com `sudo -n`: se o sudo pedir senha, rode `sudo -v` antes."},
		Changes:  []string{"instalar Git (apt)", "receita linux-dev: 1 etapa"},
		Commands: []string{"sudo -n -- env DEBIAN_FRONTEND=noninteractive apt-get install -y --no-remove git"},
	}
}

// rootWithExec monta a raiz com Apps (git, carregado), Receitas (vazia), Diagnóstico e Log.
func rootWithExec(t *testing.T, p *fakePlanner, r *fakeRunner) (*Model, *AppsTab, *LogTab) {
	t.Helper()
	be := &fakeBackend{apps: sampleApps(), status: defaultStatus()}
	apps := loaded(t, be)
	rec := loadedRecipes(t, recipeSample(), false)
	diag := loadedRecipes(t, recipeSample(), true)
	log := NewLogTab(Theme{Mono: true}, "/var/log/toolkit")
	m := sized(Options{Version: "v0.1.0", Mono: true}, 80, 24, apps, rec, diag, log)
	m.SetExec(context.Background(), p, r)
	return m, apps, log
}

func mkey(m *Model, k string) tea.Cmd {
	switch k {
	case "enter":
		return press(m, tea.KeyEnter, 0, "")
	case "esc":
		return press(m, tea.KeyEscape, 0, "")
	case "tab":
		return press(m, tea.KeyTab, 0, "")
	case "space":
		return press(m, ' ', 0, " ")
	}
	return press(m, rune(k[0]), 0, k)
}

// pump drena os eventos do executor pela própria raiz, como o Bubble Tea faria.
func pump(t *testing.T, m *Model) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for m.events != nil {
		if time.Now().After(deadline) {
			t.Fatal("o executor não terminou")
		}
		msg := waitEvent(m.events)()
		m.Update(msg)
	}
}

func scriptOK(_ context.Context, _ Selection, ch chan<- Event) {
	ch <- Event{Kind: EventItems, Items: []string{"Git", "Receita linux-dev"}}
	ch <- Event{Kind: EventItemStarted, Index: 0, Name: "Git"}
	ch <- Event{Kind: EventLog, Index: 0, Line: "Lendo listas de pacotes..."}
	ch <- Event{Kind: EventItemDone, Index: 0, Status: ItemOK, Duration: 3 * time.Second}
	ch <- Event{Kind: EventItemStarted, Index: 1}
	ch <- Event{Kind: EventLog, Index: 1, Line: "etapa pkgs: apt-get install"}
	ch <- Event{Kind: EventItemDone, Index: 1, Status: ItemFailed, Err: "apt-get saiu com 100", Duration: 5 * time.Second}
	ch <- Event{Kind: EventDone}
}

func TestEnterWithoutSelectionOnlyAsks(t *testing.T) {
	m, _, _ := rootWithExec(t, &fakePlanner{plan: okPlan()}, &fakeRunner{script: scriptOK})
	if cmd := mkey(m, "enter"); cmd != nil || m.mode != modeNav {
		t.Fatal("sem marcados não abre a Revisão")
	}
	if !strings.Contains(m.render(), "marque ao menos um item") {
		t.Fatalf("aviso:\n%s", m.render())
	}
}

func TestReviewFlowBackAndExecute(t *testing.T) {
	p := &fakePlanner{plan: okPlan()}
	r := &fakeRunner{script: scriptOK}
	m, apps, log := rootWithExec(t, p, r)
	mkey(m, "space") // marca Git
	cmd := mkey(m, "enter")
	if m.mode != modeReview || cmd == nil {
		t.Fatal("Enter abre a Revisão")
	}
	if !strings.Contains(m.render(), "Lendo o estado do sistema…") {
		t.Fatalf("carregando:\n%s", m.render())
	}
	// o plano chega: o que o Planner recebeu é a seleção marcada
	msg := cmd().(tea.BatchMsg)[0]()
	m.Update(msg)
	if len(p.got.Apps) != 1 || p.got.Apps[0].ID != "git" {
		t.Fatalf("seleção: %+v", p.got)
	}
	golden(t, "review_80x24", m.render())
	// foco inicial em Voltar; Esc volta sem executar
	if !strings.Contains(m.render(), "> [ Voltar ]") {
		t.Fatal("o foco começa em Voltar")
	}
	mkey(m, "esc")
	if m.mode != modeNav || apps.Selected() != 1 {
		t.Fatal("Esc volta e mantém a seleção")
	}
	// de novo: Tab põe o foco em Executar, Enter executa
	cmd = mkey(m, "enter")
	m.Update(cmd().(tea.BatchMsg)[0]())
	mkey(m, "tab")
	if !strings.Contains(m.render(), "> [ Executar ]") {
		t.Fatalf("foco em Executar:\n%s", m.render())
	}
	mkey(m, "enter")
	if m.mode != modeRun || m.Active() != 3 {
		t.Fatalf("executa e vai para a aba Log: mode=%v ativa=%d", m.mode, m.Active())
	}
	pump(t, m)
	if m.mode != modeNav || log.Running() {
		t.Fatal("terminou")
	}
	if got := log.Summary(); got != "Terminou com 1 falha · 1 ok · 1 falhou" {
		t.Fatalf("resumo: %q", got)
	}
	if log.cur != 1 {
		t.Fatalf("o cursor vai para a primeira falha: %d", log.cur)
	}
	if len(r.gotSel.Apps) != 1 {
		t.Fatalf("o executor recebeu %+v", r.gotSel)
	}
	golden(t, "log_done_80x24", m.render())
}

func TestExecuteWithSKey(t *testing.T) {
	m, _, _ := rootWithExec(t, &fakePlanner{plan: okPlan()}, &fakeRunner{script: scriptOK})
	mkey(m, "space")
	cmd := mkey(m, "enter")
	m.Update(cmd().(tea.BatchMsg)[0]())
	mkey(m, "s")
	if m.mode != modeRun {
		t.Fatal("s executa")
	}
	pump(t, m)
}

func TestReviewProblemsNothingToDoAndError(t *testing.T) {
	cases := map[string]struct {
		plan Plan
		err  error
		want []string
	}{
		"problemas":    {plan: Plan{Summary: "x", Problems: []string{"app \"vlc\": sem instalador para linux"}}, want: []string{"NÃO DÁ PARA EXECUTAR", "vlc", "Nada foi executado."}},
		"nada a fazer": {plan: Plan{NothingToDo: true}, want: []string{"Nada a fazer: tudo já está instalado."}},
		"erro":         {err: errors.New("catálogo inválido"), want: []string{"erro:", "catálogo inválido", "Nada foi executado."}},
	}
	for name, c := range cases {
		m, _, _ := rootWithExec(t, &fakePlanner{plan: c.plan, err: c.err}, &fakeRunner{script: scriptOK})
		mkey(m, "space")
		cmd := mkey(m, "enter")
		m.Update(cmd().(tea.BatchMsg)[0]())
		out := m.render()
		for _, w := range c.want {
			if !strings.Contains(out, w) {
				t.Errorf("%s: faltou %q:\n%s", name, w, out)
			}
		}
		if strings.Contains(out, "[ Executar ]") {
			t.Errorf("%s: sem Executar", name)
		}
		mkey(m, "s") // s não executa
		if m.mode != modeReview {
			t.Errorf("%s: s executou um plano que não pode rodar", name)
		}
		mkey(m, "tab")
		if m.review.focus != 0 {
			t.Errorf("%s: o foco não sai de Voltar", name)
		}
	}
}

// Comando longo quebra com recuo e nunca é cortado; script PowerShell fica legível.
func TestReviewWrapsLongCommandsWithoutCutting(t *testing.T) {
	long := "sudo -n -- env DEBIAN_FRONTEND=noninteractive apt-get install -y --no-remove " + strings.Repeat("pacote-com-nome-bem-comprido ", 6)
	ps := "powershell.exe -NoProfile -Command Write-Output 'ok' (enviado como -EncodedCommand)"
	p := okPlan()
	p.Commands = []string{long, ps, strings.Repeat("x", 150)}
	m, _, _ := rootWithExec(t, &fakePlanner{plan: p}, &fakeRunner{script: scriptOK})
	m.Update(tea.WindowSizeMsg{Width: 60, Height: 40})
	mkey(m, "space")
	cmd := mkey(m, "enter")
	m.Update(cmd().(tea.BatchMsg)[0]())
	out := m.render()
	flat := strings.NewReplacer("\\\n", "", "\n", " ").Replace(out)
	flat = strings.Join(strings.Fields(flat), "")
	for _, w := range []string{strings.Join(strings.Fields(long), ""), strings.Repeat("x", 150), "(enviadocomo-EncodedCommand)"} {
		if !strings.Contains(flat, w) {
			t.Fatalf("o comando foi cortado (%q ausente):\n%s", w, out)
		}
	}
	for i, l := range strings.Split(out, "\n") {
		if lipgloss.Width(l) > 60 {
			t.Fatalf("linha %d passa de 60 colunas: %q", i, l)
		}
	}
}

func TestReviewScrollsAndHasMoreHint(t *testing.T) {
	p := okPlan()
	for i := 0; i < 30; i++ {
		p.Changes = append(p.Changes, "instalar app"+itoa(i))
	}
	m, _, _ := rootWithExec(t, &fakePlanner{plan: p}, &fakeRunner{script: scriptOK})
	mkey(m, "space")
	cmd := mkey(m, "enter")
	m.Update(cmd().(tea.BatchMsg)[0]())
	if out := m.render(); !strings.Contains(out, "↓ mais") || strings.Contains(out, "app29") {
		t.Fatalf("o plano rola:\n%s", out)
	}
	for i := 0; i < 60; i++ {
		mkey(m, "j")
	}
	out := m.render()
	if !strings.Contains(out, "COMO SERÁ EXECUTADO") || strings.Count(out, "\n")+1 != 24 {
		t.Fatalf("o fim do plano aparece e a tela mantém 24 linhas:\n%s", out)
	}
}

func TestDuringRunSpaceIsBlockedAndTabsStayReadable(t *testing.T) {
	release := make(chan struct{})
	r := &fakeRunner{script: func(_ context.Context, _ Selection, ch chan<- Event) {
		ch <- Event{Kind: EventItems, Items: []string{"Git"}}
		ch <- Event{Kind: EventItemStarted, Index: 0}
		<-release
		ch <- Event{Kind: EventItemDone, Index: 0, Status: ItemOK}
		ch <- Event{Kind: EventDone}
	}}
	m, apps, _ := rootWithExec(t, &fakePlanner{plan: okPlan()}, r)
	mkey(m, "space")
	cmd := mkey(m, "enter")
	m.Update(cmd().(tea.BatchMsg)[0]())
	mkey(m, "s")
	m.Update(waitEvent(m.events)()) // EventItems
	m.Update(waitEvent(m.events)()) // EventItemStarted
	mkey(m, "1")                    // aba Apps (leitura)
	if m.Active() != 0 {
		t.Fatal("as abas continuam navegáveis")
	}
	before := apps.Selected()
	mkey(m, "space")
	mkey(m, "enter")
	if apps.Selected() != before || m.mode != modeRun {
		t.Fatal("espaço e Enter ficam bloqueados durante a execução")
	}
	if !strings.Contains(m.render(), "bloqueado durante a execução") {
		t.Fatalf("aviso:\n%s", m.render())
	}
	close(release)
	pump(t, m)
}

func TestInterruptAsksAndCancels(t *testing.T) {
	var sawCancel sync.WaitGroup
	sawCancel.Add(1)
	r := &fakeRunner{script: func(ctx context.Context, _ Selection, ch chan<- Event) {
		ch <- Event{Kind: EventItems, Items: []string{"Git", "Outro"}}
		ch <- Event{Kind: EventItemStarted, Index: 0}
		<-ctx.Done() // o item atual é encerrado pelo cancelamento
		sawCancel.Done()
		ch <- Event{Kind: EventItemDone, Index: 0, Status: ItemCanceled}
		ch <- Event{Kind: EventItemDone, Index: 1, Status: ItemNotRun}
		ch <- Event{Kind: EventDone}
	}}
	m, _, log := rootWithExec(t, &fakePlanner{plan: okPlan()}, r)
	mkey(m, "space")
	cmd := mkey(m, "enter")
	m.Update(cmd().(tea.BatchMsg)[0]())
	mkey(m, "s")
	m.Update(waitEvent(m.events)())
	m.Update(waitEvent(m.events)())
	// q pergunta; outra tecla = não (padrão N)
	mkey(m, "q")
	if !strings.Contains(m.render(), "Interromper? O item atual é encerrado e a fila é descartada. [s/N]") || m.Quitting() {
		t.Fatalf("pergunta:\n%s", m.render())
	}
	mkey(m, "x")
	if m.asking != askNone || m.cancel == nil {
		t.Fatal("qualquer tecla que não seja s é não")
	}
	select {
	case <-m.ctxDone():
		t.Fatal("cancelou sem confirmar")
	default:
	}
	mkey(m, "q")
	mkey(m, "s")
	done := make(chan struct{})
	go func() { sawCancel.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("o executor não viu o cancelamento")
	}
	pump(t, m)
	if !strings.Contains(log.Status(), "Terminou") || log.items[0].status != ItemCanceled || log.items[1].status != ItemNotRun {
		t.Fatalf("itens: %+v status %q", log.items, log.Status())
	}
}

// ctxDone é só para o teste: o ctx da execução em andamento.
func (m *Model) ctxDone() <-chan struct{} {
	c := make(chan struct{})
	if m.cancel == nil {
		close(c)
	}
	return c
}

func TestRebootPromptRepliesToExecutor(t *testing.T) {
	for _, answer := range []string{"s", "n"} {
		got := make(chan bool, 1)
		r := &fakeRunner{script: func(_ context.Context, _ Selection, ch chan<- Event) {
			reply := make(chan bool, 1)
			ch <- Event{Kind: EventItems, Items: []string{"Receita wsl-dev"}}
			ch <- Event{Kind: EventRebootPrompt, Prompt: "Reiniciar agora para continuar a receita wsl-dev?", Reply: reply}
			got <- <-reply
			ch <- Event{Kind: EventItemDone, Index: 0, Status: ItemReboot}
			ch <- Event{Kind: EventDone}
		}}
		m, _, _ := rootWithExec(t, &fakePlanner{plan: okPlan()}, r)
		mkey(m, "space")
		cmd := mkey(m, "enter")
		m.Update(cmd().(tea.BatchMsg)[0]())
		mkey(m, "s")
		m.Update(waitEvent(m.events)())
		m.Update(waitEvent(m.events)())
		if out := m.render(); !strings.Contains(out, "Reiniciar agora para continuar a receita wsl-dev? [s/N]") {
			t.Fatalf("pergunta de reinício:\n%s", out)
		}
		mkey(m, answer)
		if v := <-got; v != (answer == "s") {
			t.Fatalf("resposta %q entregue como %v", answer, v)
		}
		pump(t, m)
	}
}

func TestLogCarriageReturnReplacesLastLine(t *testing.T) {
	l := NewLogTab(Theme{Mono: true}, "")
	l.Apply(Event{Kind: EventItems, Items: []string{"x"}})
	for _, s := range []string{"baixando 10%", "baixando 50%\rbaixando 51%", "\rbaixando 100%", "fim"} {
		l.Apply(Event{Kind: EventLog, Index: 0, Line: s})
	}
	want := []string{"baixando 100%", "fim"}
	if got := l.items[0].lines; len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("linhas: %q", got)
	}
}

func TestLogEmptyFollowAndScroll(t *testing.T) {
	l := NewLogTab(Theme{Mono: true}, "/var/log/toolkit")
	v := plain(l.View(80, 10))
	if !strings.Contains(v, "Nada rodou nesta sessão.") || !strings.Contains(v, "Logs anteriores: /var/log/toolkit") {
		t.Fatalf("vazio:\n%s", v)
	}
	l.Apply(Event{Kind: EventItems, Items: []string{"x"}})
	l.Apply(Event{Kind: EventItemStarted, Index: 0})
	for i := 0; i < 30; i++ {
		l.Apply(Event{Kind: EventLog, Index: 0, Line: "linha " + itoa(i)})
	}
	if v := plain(l.View(80, 10)); !strings.Contains(v, "linha 29") || strings.Contains(v, "linha 0\n") {
		t.Fatalf("segue o fim:\n%s", v)
	}
	l.Update(tea.KeyPressMsg{Code: tea.KeyPgUp})
	if l.follow {
		t.Fatal("rolar para cima desliga o seguir")
	}
	if v := plain(l.View(80, 10)); strings.Contains(v, "linha 29") {
		t.Fatalf("rolou para cima:\n%s", v)
	}
	l.Update(tea.KeyPressMsg{Code: 'f', Text: "f"})
	if !l.follow || !strings.Contains(plain(l.View(80, 10)), "linha 29") {
		t.Fatal("f volta a seguir o fim")
	}
}

func TestLogWarnsWhenFileLogFails(t *testing.T) {
	l := NewLogTab(Theme{Mono: true}, "")
	l.Warn("Não deu para gravar o log em arquivo; a execução continua e o log fica só na tela.")
	if !strings.Contains(l.Status(), "! Não deu para gravar o log em arquivo") {
		t.Fatalf("status: %q", l.Status())
	}
}

// Texto de fora no plano e no log nunca vira sequência de terminal.
func TestPlanAndLogTextIsSanitized(t *testing.T) {
	p := okPlan()
	p.Summary, p.Env = "res\x1b[2Jumo", "env\x1b]0;x\x07"
	p.Warnings = []string{"av\x1b[31miso"}
	p.Commands = []string{"cmd \x1b[2J\u202e fim"}
	p.Problems = nil
	m, _, log := rootWithExec(t, &fakePlanner{plan: p}, &fakeRunner{script: func(_ context.Context, _ Selection, ch chan<- Event) {
		ch <- Event{Kind: EventItems, Items: []string{"It\x1b[2Jem"}}
		ch <- Event{Kind: EventLog, Index: 0, Line: "saída\x1b]0;pwn\x07 \x1b[31mcor"}
		ch <- Event{Kind: EventItemDone, Index: 0, Status: ItemFailed, Err: "err\x1b[2J"}
		ch <- Event{Kind: EventDone}
	}})
	mkey(m, "space")
	cmd := mkey(m, "enter")
	m.Update(cmd().(tea.BatchMsg)[0]())
	if strings.ContainsAny(m.render(), "\x1b\x07\u202e") {
		t.Fatalf("plano vazou controle: %q", m.render())
	}
	mkey(m, "s")
	pump(t, m)
	if strings.ContainsAny(m.render(), "\x1b\x07\u202e") || strings.ContainsAny(log.Status(), "\x1b\x07") {
		t.Fatalf("log vazou controle: %q", m.render())
	}
}

func TestReviewAndRunScreensFitTheWindow(t *testing.T) {
	for _, sz := range [][2]int{{60, 16}, {80, 24}, {100, 30}} {
		m, _, _ := rootWithExec(t, &fakePlanner{plan: okPlan()}, &fakeRunner{script: scriptOK})
		m.Update(tea.WindowSizeMsg{Width: sz[0], Height: sz[1]})
		mkey(m, "space")
		cmd := mkey(m, "enter")
		check := func(stage string) {
			rows := strings.Split(m.render(), "\n")
			if len(rows) != sz[1] {
				t.Errorf("%v %s: %d linhas", sz, stage, len(rows))
			}
			for i, l := range rows {
				if lipgloss.Width(l) > sz[0] {
					t.Errorf("%v %s linha %d: %d colunas", sz, stage, i+1, lipgloss.Width(l))
				}
			}
		}
		check("carregando")
		m.Update(cmd().(tea.BatchMsg)[0]())
		check("revisão")
		mkey(m, "s")
		pump(t, m)
		check("resumo")
	}
}

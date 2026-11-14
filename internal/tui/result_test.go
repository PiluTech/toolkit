package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// stubResultTab é uma aba mínima que implementa Resulter (e pode capturar teclas).
type stubResultTab struct {
	*LogTab
	res       *Result
	capturing bool
}

func (s *stubResultTab) Title() string   { return "Stub" }
func (s *stubResultTab) Short() string   { return "Stub" }
func (s *stubResultTab) Capturing() bool { return s.capturing }
func (s *stubResultTab) Result() *Result { return s.res }
func (s *stubResultTab) Dismiss()        { s.res = nil }

var _ Resulter = (*stubResultTab)(nil)

func newStub(r *Result) *stubResultTab {
	return &stubResultTab{LogTab: NewLogTab(Theme{Mono: true}, ""), res: r}
}

func sampleResult(k ResultKind) *Result {
	return &Result{Kind: k, Title: "Perfil recomendado aplicado", Lines: []string{"3 mudança(s)", "Backup em C:\\ProgramData\\toolkit\\backup.json", "Para desfazer: tecle d nesta aba"}}
}

func TestResultKindsSimboloEPalavraEmMono(t *testing.T) {
	th := Theme{Mono: true}
	cases := map[ResultKind][2]string{
		ResultOK: {SymOK, "feito"}, ResultPartial: {SymWarn, "parcial"}, ResultError: {SymFail, "falhou"}, ResultInfo: {"i", "info"},
	}
	for k, want := range cases {
		lines := renderResult(th, Result{Kind: k, Title: "Titulo"}, 80, 24)
		if len(lines) == 0 || !strings.HasPrefix(strings.TrimSpace(lines[0]), want[0]+" "+want[1]) {
			t.Errorf("kind %d: %q", k, lines)
		}
		for _, l := range lines {
			if strings.Contains(l, "\x1b") {
				t.Errorf("mono emitiu estilo: %q", l)
			}
		}
	}
}

func TestResultQuebraSemReticenciasEmTresLarguras(t *testing.T) {
	long := "Backup em C:\\Users\\ana\\AppData\\Local\\toolkit\\backups\\privacidade-2026-11-14-120000-abcdef0123456789.json depois do plano"
	for _, w := range []int{60, 80, 120} {
		r := Result{Kind: ResultOK, Title: "Perfil recomendado aplicado com um título bem comprido para forçar a quebra da primeira linha", Lines: []string{long, "Para desfazer: tecle d nesta aba"}}
		lines := renderResult(Theme{Mono: true}, r, w, 40)
		joined := strings.Join(lines, "\n")
		if strings.Contains(joined, "…") {
			t.Errorf("w=%d: reticências:\n%s", w, joined)
		}
		for _, l := range lines {
			if lipgloss.Width(l) > w {
				t.Errorf("w=%d: linha larga (%d): %q", w, lipgloss.Width(l), l)
			}
		}
		// nada some: toda palavra do texto aparece na faixa (a barra invertida de quebra de palavra não conta)
		flat := strings.NewReplacer("\\", "", "\n", "").Replace(joined)
		flat = strings.Join(strings.Fields(flat), "")
		for _, word := range strings.Fields(long) {
			if !strings.Contains(flat, strings.ReplaceAll(strings.ReplaceAll(word, "\\", ""), " ", "")) {
				t.Errorf("w=%d: perdeu %q", w, word)
			}
		}
		if !strings.Contains(joined, "[x] fechar") {
			t.Errorf("w=%d: sem a dica", w)
		}
	}
}

func TestResultUmaLinhaEmTerminalPequeno(t *testing.T) {
	lines := renderResult(Theme{Mono: true}, *sampleResult(ResultOK), 70, 19)
	if len(lines) != 1 {
		t.Fatalf("%d linhas: %q", len(lines), lines)
	}
	if !strings.Contains(lines[0], "Perfil recomendado aplicado") || !strings.Contains(lines[0], "[x] fechar") || !strings.Contains(lines[0], SymOK) {
		t.Errorf("compacta: %q", lines[0])
	}
	if lipgloss.Width(lines[0]) > 70 {
		t.Errorf("larga: %d", lipgloss.Width(lines[0]))
	}
}

func TestResultMaximoQuatroLinhasDeDetalhe(t *testing.T) {
	r := Result{Kind: ResultOK, Title: "T", Lines: []string{"a", "b", "c", "d", "e", "f"}}
	out := strings.Join(renderResult(Theme{Mono: true}, r, 80, 40), "\n")
	if !strings.Contains(out, "d") || strings.Contains(out, "   e\n") {
		t.Errorf("detalhes:\n%s", out)
	}
}

func TestResultTextoExternoLimpo(t *testing.T) {
	r := &Result{Kind: ResultError, Title: "falha \x1b[31mvermelha\x1b[0m", Lines: []string{"erro \x07 com bel\u202e"}}
	m := sized(Options{Mono: true}, 80, 24, newStub(r))
	out := m.render()
	if strings.ContainsAny(out, "\x1b\x07\u202e") {
		t.Errorf("texto externo sem limpar: %q", out)
	}
}

func TestFaixaAparecePersisteEFechaComX(t *testing.T) {
	a, b := newStub(sampleResult(ResultOK)), newStub(nil)
	m := sized(Options{Mono: true}, 80, 24, a, b)
	out := m.render()
	if !strings.Contains(out, "Perfil recomendado aplicado") || !strings.Contains(out, "Para desfazer: tecle d nesta aba") {
		t.Fatalf("sem faixa:\n%s", out)
	}
	if rows := strings.Split(out, "\n"); len(rows) != 24 {
		t.Fatalf("a tela tem %d linhas", len(rows))
	}
	// trocar de aba e voltar: a faixa persiste; na outra aba não aparece
	mkey(m, "tab")
	if strings.Contains(m.render(), "Perfil recomendado aplicado") {
		t.Fatal("a faixa é da outra aba")
	}
	mkey(m, "tab")
	if !strings.Contains(m.render(), "Perfil recomendado aplicado") {
		t.Fatal("a faixa não persistiu")
	}
	mkey(m, "x")
	if a.res != nil || strings.Contains(m.render(), "Perfil recomendado aplicado") {
		t.Fatal("x não fechou")
	}
}

func TestXNaoFechaQuandoAAbaCaptura(t *testing.T) {
	a := newStub(sampleResult(ResultOK))
	a.capturing = true
	m := sized(Options{Mono: true}, 80, 24, a)
	mkey(m, "x")
	if a.res == nil {
		t.Fatal("x virou texto da aba e não pode fechar a faixa")
	}
}

func TestFaixaNaoRoubaOutrasTeclas(t *testing.T) {
	a, b := newStub(sampleResult(ResultOK)), newStub(nil)
	m := sized(Options{Mono: true}, 80, 24, a, b)
	mkey(m, "2")
	if m.Active() != 1 {
		t.Fatal("os números continuam trocando de aba")
	}
	mkey(m, "1")
	mkey(m, "?")
	if m.modal != modalHelp || a.res == nil {
		t.Fatal("? abre a ajuda e a faixa fica")
	}
}

func TestFaixaTiraLinhasDoCorpo(t *testing.T) {
	a := newStub(sampleResult(ResultOK))
	m := sized(Options{Mono: true}, 80, 24, a)
	with := strings.Split(m.render(), "\n")
	a.res = nil
	without := strings.Split(m.render(), "\n")
	if len(with) != 24 || len(without) != 24 {
		t.Fatalf("altura: %d e %d", len(with), len(without))
	}
	// a régua de baixo, a linha de estado e a barra de teclas ficam no mesmo lugar
	if with[23] != without[23] {
		t.Errorf("barra de teclas mudou:\n%q\n%q", with[23], without[23])
	}
}

func TestFaixaEmTerminalPequenoEUmaLinha(t *testing.T) {
	a := newStub(sampleResult(ResultOK))
	m := sized(Options{Mono: true}, 80, 18, a)
	out := m.render()
	if strings.Contains(out, "Para desfazer") || !strings.Contains(out, "Perfil recomendado aplicado") {
		t.Fatalf("compacta:\n%s", out)
	}
	if len(strings.Split(out, "\n")) != 18 {
		t.Fatal("altura")
	}
}

// --- integração: Privacidade ---

func TestPrivacidadeAplicadoViraFaixa(t *testing.T) {
	be := &fakePriv{info: privSample(true), plan: PrivPlan{ID: "p", Text: planText, Pending: 3, IsAdmin: true}, applyRes: PrivResult{Applied: 3, BackupPath: "C:\\bk\\priv.json"}}
	tab := privTab(t, be)
	pkey(tab, "down", "a", "s")
	r := tab.Result()
	if r == nil || r.Kind != ResultOK || r.Title != "Perfil recomendado aplicado" {
		t.Fatalf("resultado: %+v", r)
	}
	joined := strings.Join(r.Lines, "|")
	for _, want := range []string{"3 mudança(s)", "Backup em C:\\bk\\priv.json", "Para desfazer: tecle d nesta aba"} {
		if !strings.Contains(joined, want) {
			t.Errorf("falta %q em %q", want, joined)
		}
	}
	if len(tab.Status()) > 60 {
		t.Errorf("a Status deve ser curta: %q", tab.Status())
	}
	tab.Dismiss()
	if tab.Result() != nil {
		t.Fatal("Dismiss não limpou")
	}
}

func TestPrivacidadeFalhaParcialEErro(t *testing.T) {
	be := &fakePriv{info: privSample(true), plan: PrivPlan{ID: "p", Text: planText, Pending: 3, IsAdmin: true}, applyRes: PrivResult{Applied: 2, BackupPath: "C:\\bk.json"}, applyErr: errors.New("acesso negado")}
	tab := privTab(t, be)
	pkey(tab, "down", "a", "s")
	r := tab.Result()
	if r == nil || r.Kind != ResultPartial {
		t.Fatalf("parcial: %+v", r)
	}
	j := strings.Join(r.Lines, "|")
	if !strings.Contains(j, "2 mudança(s)") || !strings.Contains(j, "acesso negado") || !strings.Contains(j, "d") {
		t.Errorf("linhas: %q", j)
	}
	be2 := &fakePriv{info: privSample(true), plan: PrivPlan{ID: "p", Text: planText, Pending: 3, IsAdmin: true}, applyErr: errors.New("boom")}
	tab2 := privTab(t, be2)
	pkey(tab2, "down", "a", "s")
	if r := tab2.Result(); r == nil || r.Kind != ResultError || strings.Contains(strings.Join(r.Lines, "|"), "Backup em") {
		t.Fatalf("erro: %+v", r)
	}
}

func TestPrivacidadeRecusasNaoDizemQueMudouAlgo(t *testing.T) {
	for _, tc := range []struct {
		err  error
		kind ResultKind
	}{{ErrPrivNeedsAdmin, ResultError}, {ErrPrivStalePlan, ResultInfo}} {
		be := &fakePriv{info: privSample(true), plan: PrivPlan{ID: "p", Text: planText, Pending: 3, IsAdmin: true}, applyErr: tc.err}
		tab := privTab(t, be)
		pkey(tab, "down", "a", "s")
		r := tab.Result()
		if r == nil || r.Kind != tc.kind || !strings.Contains(strings.ToLower(strings.Join(r.Lines, "|")), "nada foi alterado") {
			t.Errorf("%v: %+v", tc.err, r)
		}
	}
}

func TestPrivacidadeRestauradoEFaixa(t *testing.T) {
	be := &fakePriv{info: privSample(true), plan: PrivPlan{ID: "p", Text: planText, Pending: 3, IsAdmin: true}, undoRes: PrivResult{Applied: 4}}
	tab := privTab(t, be)
	pkey(tab, "d", "s")
	r := tab.Result()
	if r == nil || r.Kind != ResultOK || r.Title != "Restaurado" || !strings.Contains(strings.Join(r.Lines, "|"), "4 item(ns)") {
		t.Fatalf("restaurado: %+v", r)
	}
}

// --- integração: Apps nativos ---

func TestAppsNativosRemovidosViraFaixa(t *testing.T) {
	be := &fakeNat{info: natSample(true), plan: NatPlan{ID: "p", Text: natPlanText, Pending: 2, IsAdmin: true}, applyRes: NatResult{Removed: 2, BackupPath: "C:\\bk\\nat.json"}}
	tab := natTab(t, be)
	nkey(tab, "t", "r", "s")
	r := tab.Result()
	if r == nil || r.Kind != ResultOK || !strings.Contains(r.Title, "2") {
		t.Fatalf("resultado: %+v", r)
	}
	j := strings.Join(r.Lines, "|")
	if !strings.Contains(j, "Backup em C:\\bk\\nat.json") || !strings.Contains(j, "Para reinstalar: tecle i") {
		t.Errorf("linhas: %q", j)
	}
	tab.Dismiss()
	if tab.Result() != nil {
		t.Fatal("Dismiss")
	}
}

// --- integração: execuções ---

// runMarked marca o Git na aba Apps, abre a Revisão, executa e drena a execução.
func runMarked(t *testing.T, script func(context.Context, Selection, chan<- Event)) (*Model, *AppsTab, *LogTab) {
	t.Helper()
	m, apps, log := rootWithExec(t, &fakePlanner{plan: okPlan()}, &fakeRunner{script: script})
	mkey(m, "space")
	cmd := mkey(m, "enter")
	m.Update(cmd().(tea.BatchMsg)[0]())
	mkey(m, "s")
	pump(t, m)
	return m, apps, log
}

func TestExecucaoComFalhaMostraFaixaNaAbaDeOrigem(t *testing.T) {
	m, _, _ := runMarked(t, scriptOK)
	// o resumo está na aba Log; a faixa aparece ao voltar para Apps (origem)
	mkey(m, "1")
	r := m.currentResult()
	if r == nil || r.Kind != ResultPartial || r.Title != "Instalação concluída: 1 ok, 1 falharam" {
		t.Fatalf("resultado: %+v", r)
	}
	out := m.render()
	for _, want := range []string{"Receita linux-dev", "Detalhes: aba Log", "[x] fechar"} {
		if !strings.Contains(out, want) {
			t.Errorf("falta %q:\n%s", want, out)
		}
	}
	// receitas não foram marcadas: sem faixa lá
	mkey(m, "2")
	if m.currentResult() != nil {
		t.Fatal("só a aba de origem recebe a faixa")
	}
	mkey(m, "1")
	mkey(m, "x")
	if m.currentResult() != nil {
		t.Fatal("x fecha a faixa da execução")
	}
}

func TestExecucaoSemFalhaEInterrompida(t *testing.T) {
	m, _, _ := runMarked(t, func(_ context.Context, _ Selection, ch chan<- Event) {
		ch <- Event{Kind: EventItems, Items: []string{"Git", "Curl"}}
		ch <- Event{Kind: EventItemDone, Index: 0, Status: ItemOK}
		ch <- Event{Kind: EventItemDone, Index: 1, Status: ItemOK}
		ch <- Event{Kind: EventDone}
	})
	mkey(m, "1")
	if r := m.currentResult(); r == nil || r.Kind != ResultOK || r.Title != "Instalação concluída: 2 ok" {
		t.Fatalf("ok: %+v", r)
	}
	m2, _, _ := runMarked(t, func(_ context.Context, _ Selection, ch chan<- Event) {
		ch <- Event{Kind: EventItems, Items: []string{"Git", "Curl"}}
		ch <- Event{Kind: EventItemDone, Index: 0, Status: ItemFailed, Err: "x"}
		ch <- Event{Kind: EventItemDone, Index: 1, Status: ItemNotRun}
		ch <- Event{Kind: EventDone}
	})
	mkey(m2, "1")
	if r := m2.currentResult(); r == nil || r.Kind != ResultError || !strings.Contains(r.Title, "interrompida") {
		t.Fatalf("interrompida: %+v", r)
	}
}

func TestNovaExecucaoSubstituiAFaixaAnterior(t *testing.T) {
	m, _, _ := runMarked(t, scriptOK)
	mkey(m, "1")
	first := m.currentResult()
	if first == nil {
		t.Fatal("sem faixa")
	}
	cmd := mkey(m, "enter") // a marca continua na aba Apps
	m.Update(cmd().(tea.BatchMsg)[0]())
	mkey(m, "s")
	if len(m.results) != 0 {
		t.Fatalf("a faixa antiga some quando outra execução começa: %v", m.results)
	}
	pump(t, m)
}

// logWith monta um LogTab terminado com os itens nos estados dados.
func logWith(statuses ...ItemStatus) *LogTab {
	lt := NewLogTab(Theme{Mono: true, NoMotion: true}, "")
	names := make([]string, len(statuses))
	for i := range statuses {
		names[i] = fmt.Sprintf("Item%d", i+1)
	}
	lt.Apply(Event{Kind: EventItems, Items: names})
	for i, st := range statuses {
		lt.Apply(Event{Kind: EventItemDone, Index: i, Status: st})
	}
	lt.Apply(Event{Kind: EventDone})
	return lt
}

// Lupa #68: ItemReboot é PENDENTE (a receita espera reinício), nunca "feito / concluída".
func TestOutcomeSoReinicioNaoEConcluida(t *testing.T) {
	r := logWith(ItemReboot).Outcome()
	if r == nil || r.Kind == ResultOK || strings.Contains(r.Title, "concluída") || strings.Contains(r.Title, "0 ok") {
		t.Fatalf("só reinício não pode ser concluído: %+v", r)
	}
	if !strings.Contains(r.Title, "reinício") || !strings.Contains(r.Title, "1") {
		t.Errorf("o título deve dizer a quantidade aguardando reinício: %q", r.Title)
	}
	if !strings.Contains(strings.Join(r.Lines, "\n"), "Item1") {
		t.Errorf("a linha deve nomear o item pendente: %v", r.Lines)
	}
}

func TestOutcomeOKMaisReinicioEPendente(t *testing.T) {
	r := logWith(ItemOK, ItemOK, ItemReboot).Outcome()
	if r == nil || r.Kind == ResultOK || strings.Contains(r.Title, "concluída") {
		t.Fatalf("ok + reinício continua pendente: %+v", r)
	}
	if !strings.Contains(r.Title, "1 aguardando reinício") || !strings.Contains(r.Title, "2 ok") {
		t.Errorf("título: %q", r.Title)
	}
}

func TestOutcomeFalhaMaisReinicioMencionaOsDois(t *testing.T) {
	r := logWith(ItemOK, ItemFailed, ItemReboot).Outcome()
	if r == nil || r.Kind != ResultPartial || !strings.Contains(r.Title, "reinício") || !strings.Contains(r.Title, "falharam") {
		t.Fatalf("%+v", r)
	}
}

// A faixa COMPACTA (janela < 20 linhas) preserva a pendência de reinício no título.
func TestFaixaCompactaPreservaAguardandoReinicio(t *testing.T) {
	for _, st := range [][]ItemStatus{{ItemReboot}, {ItemOK, ItemReboot}} {
		r := logWith(st...).Outcome()
		for _, w := range []int{60, 80} {
			lines := renderResult(Theme{Mono: true, NoMotion: true}, *r, w, 16)
			if len(lines) != 1 {
				t.Fatalf("compacta é uma linha: %d", len(lines))
			}
			txt := ansi.Strip(lines[0])
			if !strings.Contains(txt, "reinício") || strings.Contains(txt, "feito") || strings.Contains(txt, "concluída") {
				t.Errorf("w=%d %v: compacta perdeu o reinício ou diz feito: %q", w, st, txt)
			}
		}
	}
}

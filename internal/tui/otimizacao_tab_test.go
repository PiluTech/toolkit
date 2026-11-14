package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/PiluTech/toolkit/internal/diagfmt"
	"github.com/PiluTech/toolkit/internal/otimizacao"
)

type fakeOtim struct {
	mu      sync.Mutex
	rep     *otimizacao.Report
	err     error
	calls   int
	ctxs    []context.Context
	hold    chan struct{} // se não nil, Load espera até ser fechado (ou o contexto cair)
	started chan struct{} // se não nil, recebe um sinal quando o Load começa
}

func (f *fakeOtim) Load(ctx context.Context) (*otimizacao.Report, error) {
	f.mu.Lock()
	f.calls++
	f.ctxs = append(f.ctxs, ctx)
	hold, started := f.hold, f.started
	f.mu.Unlock()
	if started != nil {
		started <- struct{}{}
	}
	if hold != nil {
		select {
		case <-hold:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return f.rep, f.err
}

func otimReport() *otimizacao.Report {
	deg := true
	free := uint64(40)
	avail := uint64(1 << 30)
	return &otimizacao.Report{OS: "windows", Supported: true,
		CPU:      &otimizacao.CPU{Samples: 3, AvgPct: 91, PeakPct: 99, Top: []otimizacao.Process{{Name: "chrome", PID: 7, CPUPct: 50}}},
		Memory:   &otimizacao.Memory{TotalBytes: 16 << 30, AvailableBytes: &avail, Top: []otimizacao.Process{{Name: "chrome", PID: 7, MemBytes: 2 << 30}}},
		Disk:     &otimizacao.Disk{Drive: "C", Name: "Disco Fake", Medium: "HDD", SizeBytes: 1000, FreeBytes: &free},
		Startup:  &otimizacao.Startup{Total: 14, Items: []otimizacao.StartupItem{{Name: "AppUm", Command: "um.exe"}}},
		Services: &otimizacao.Services{AutoCount: 90, OutsideCount: 10, Failed: []otimizacao.Service{{Name: "SvcFalhou", ExitCode: 1}}},
		Boot:     &otimizacao.Boot{Found: true, DurationMs: 99000, Degraded: &deg},
		Skipped:  []otimizacao.Skipped{{Area: "boot", Reason: "exige administrador", NeedsAdmin: true}}}
}

func newOtim(be OptimizeBackend, goos string) *OptimizeTab {
	return NewOptimizeTab(context.Background(), be, Theme{Mono: true, NoMotion: true}, diagfmt.PT, goos)
}

func okey(t *OptimizeTab, keys ...string) tea.Cmd {
	var last tea.Cmd
	for _, k := range keys {
		var m tea.KeyPressMsg
		switch k {
		case "esc":
			m = tea.KeyPressMsg{Code: tea.KeyEscape}
		case "enter":
			m = tea.KeyPressMsg{Code: tea.KeyEnter}
		case "down":
			m = tea.KeyPressMsg{Code: tea.KeyDown}
		case "up":
			m = tea.KeyPressMsg{Code: tea.KeyUp}
		case "pgdown":
			m = tea.KeyPressMsg{Code: tea.KeyPgDown}
		case "pgup":
			m = tea.KeyPressMsg{Code: tea.KeyPgUp}
		default:
			m = tea.KeyPressMsg{Code: rune(k[0]), Text: k}
		}
		_, last = t.Update(m)
	}
	return last
}

func otimSettle(t *testing.T, tab *OptimizeTab, cmd tea.Cmd) {
	t.Helper()
	if cmd == nil {
		t.Fatal("sem comando")
	}
	for _, m := range dRunBatch(cmd) {
		tab.Update(m)
	}
}

func loadedOtim(t *testing.T) (*OptimizeTab, *fakeOtim) {
	t.Helper()
	fo := &fakeOtim{rep: otimReport()}
	tab := newOtim(fo, "windows")
	otimSettle(t, tab, okey(tab, "c"))
	return tab, fo
}

func TestOtimNaoLeNadaAoAbrirEMostraComoLer(t *testing.T) {
	fo := &fakeOtim{rep: otimReport()}
	tab := newOtim(fo, "windows")
	if tab.Init() != nil || fo.calls != 0 {
		t.Fatal("Init não lê: a amostra de CPU concorreria com a abertura da TUI")
	}
	v := tab.View(80, 16)
	if !strings.Contains(v, "ainda não foram lidos") || !strings.Contains(v, "Tecle c") {
		t.Fatalf("estado inicial:\n%s", v)
	}
	if tab.Capturing() || tab.Selected() != 0 || tab.More() != 0 {
		t.Fatal("a seção nunca captura teclas e não marca nada")
	}
	if tab.Title() != "Otimização" || tab.Short() != "Otim" {
		t.Fatalf("títulos: %q %q", tab.Title(), tab.Short())
	}
	if tab.Detail(44, 20) != "" {
		t.Fatal("sem leitura não há painel")
	}
}

func TestOtimLeResumoEDados(t *testing.T) {
	tab, fo := loadedOtim(t)
	if fo.calls != 1 {
		t.Fatalf("calls=%d", fo.calls)
	}
	var all strings.Builder
	for _, l := range tab.lines {
		all.WriteString(l + "\n")
	}
	for _, want := range []string{"Otimização do Windows", "CPU com uso alto", "O que fazer:", "Pouca memória disponível", "O disco do sistema é um HDD", "Muitos itens", "Serviço automático parado com erro", "Não lido:", "== CPU ==", "chrome", "Segurança: só leitura"} {
		if !strings.Contains(all.String(), want) {
			t.Errorf("faltou %q:\n%s", want, all.String())
		}
	}
	tab.View(80, 6)
	if tab.More() == 0 {
		t.Fatal("More conta as linhas abaixo da dobra")
	}
	before := tab.top
	okey(tab, "down", "down", "j")
	if tab.top != before+3 {
		t.Fatalf("rolagem: %d", tab.top)
	}
	okey(tab, "G")
	tab.View(80, 6)
	if tab.More() != 0 {
		t.Fatalf("no fim não sobra nada: %d", tab.More())
	}
	okey(tab, "g")
	if tab.top != 0 {
		t.Fatal("g volta ao início")
	}
	okey(tab, "pgdown")
	if tab.top == 0 {
		t.Fatal("PgDn rola uma página")
	}
	okey(tab, "pgup")
	if tab.top != 0 {
		t.Fatal("PgUp volta")
	}
}

func TestOtimCLeDeNovoEEnterTambem(t *testing.T) {
	tab, fo := loadedOtim(t)
	otimSettle(t, tab, okey(tab, "c"))
	otimSettle(t, tab, okey(tab, "enter"))
	if fo.calls != 3 {
		t.Fatalf("calls=%d", fo.calls)
	}
}

func TestOtimEscCancelaALeituraEDescartaARespostaTardia(t *testing.T) {
	fo := &fakeOtim{rep: otimReport(), hold: make(chan struct{}), started: make(chan struct{}, 1)}
	tab := newOtim(fo, "windows")
	cmd := okey(tab, "c")
	if !tab.loading {
		t.Fatal("deveria estar carregando")
	}
	if v := tab.View(80, 16); !strings.Contains(v, "Carregando") || !strings.Contains(v, "Esc cancela") {
		t.Fatalf("carregando:\n%s", v)
	}
	if k := tab.Keys(); len(k) != 1 || k[0].Key != "Esc" {
		t.Fatalf("carregando só oferece Esc: %v", k)
	}
	done := make(chan []tea.Msg)
	go func() { done <- dRunBatch(cmd) }()
	<-fo.started
	okey(tab, "esc")
	if tab.loading || !tab.cancelled {
		t.Fatalf("Esc cancela: loading=%v cancelled=%v", tab.loading, tab.cancelled)
	}
	fo.mu.Lock()
	ctx0 := fo.ctxs[0]
	fo.mu.Unlock()
	if ctx0.Err() == nil {
		t.Fatal("o contexto da leitura devia ter sido cancelado")
	}
	for _, m := range <-done {
		tab.Update(m) // a resposta tardia é descartada
	}
	if tab.loaded || tab.err != nil {
		t.Fatalf("a resposta de uma leitura cancelada não vira estado: loaded=%v err=%v", tab.loaded, tab.err)
	}
	v := tab.View(80, 16)
	if !strings.Contains(v, "Leitura cancelada") || !strings.Contains(v, "Nada foi alterado") {
		t.Fatalf("cancelado:\n%s", v)
	}
	// uma nova leitura depois do cancelamento funciona
	fo.mu.Lock()
	fo.hold, fo.started = nil, nil
	fo.mu.Unlock()
	otimSettle(t, tab, okey(tab, "c"))
	if !tab.loaded {
		t.Fatal("c depois de cancelar lê de novo")
	}
}

func TestOtimEscForaDeLeituraNaoFazNada(t *testing.T) {
	tab, fo := loadedOtim(t)
	okey(tab, "esc")
	if !tab.loaded || fo.calls != 1 || tab.cancelled {
		t.Fatal("Esc sem leitura em andamento não muda nada")
	}
}

func TestOtimErro(t *testing.T) {
	fo := &fakeOtim{err: errors.New("o PowerShell não respondeu\nsegunda linha")}
	tab := newOtim(fo, "windows")
	otimSettle(t, tab, okey(tab, "c"))
	v := tab.View(80, 16)
	for _, want := range []string{"Erro", "Não deu para ler", "o PowerShell não respondeu", "Tente de novo (c)", "Nada foi alterado"} {
		if !strings.Contains(v, want) {
			t.Errorf("faltou %q:\n%s", want, v)
		}
	}
	if tab.Status() != "Falhou. c lê de novo." || tab.Detail(44, 10) != "" {
		t.Fatalf("status/detalhe do erro: %q", tab.Status())
	}
	fo.err, fo.rep = nil, otimReport()
	otimSettle(t, tab, okey(tab, "c"))
	if !tab.loaded || tab.err != nil {
		t.Fatal("c depois do erro lê de novo")
	}
}

func TestOtimRelatorioNilVirouErro(t *testing.T) {
	tab := newOtim(&fakeOtim{}, "windows")
	otimSettle(t, tab, okey(tab, "c"))
	if tab.err == nil {
		t.Fatal("relatório vazio é erro")
	}
}

// Fora do Windows: a seção só avisa, sem chamar o backend, mesmo com a tecla c.
func TestOtimForaDoWindowsSoAvisa(t *testing.T) {
	for _, goos := range []string{"darwin", "linux"} {
		fo := &fakeOtim{rep: otimReport()}
		tab := newOtim(fo, goos)
		if cmd := okey(tab, "c", "enter", "esc"); cmd != nil {
			t.Errorf("%s: nenhuma tecla gera leitura", goos)
		}
		v := tab.View(80, 16)
		for _, want := range []string{"só existe no Windows", "este sistema é", "Nada foi alterado"} {
			if !strings.Contains(v, want) {
				t.Errorf("%s: faltou %q:\n%s", goos, want, v)
			}
		}
		if fo.calls != 0 || tab.Keys() != nil || tab.Detail(44, 20) != "" || tab.More() != 0 {
			t.Errorf("%s: calls=%d keys=%v", goos, fo.calls, tab.Keys())
		}
		if h := tab.HelpKeys(); len(h) != 1 || !strings.Contains(h[0].Help, "só existe no Windows") {
			t.Errorf("%s: ajuda: %+v", goos, h)
		}
		if !strings.Contains(tab.Status(), "só existe no Windows") {
			t.Errorf("%s: status %q", goos, tab.Status())
		}
	}
}

func TestOtimDetalheTrazLimiaresELista(t *testing.T) {
	tab, _ := loadedOtim(t)
	d := tab.Detail(DetailWidth, 40)
	for _, want := range []string{"COMO LER", "LIMIARES USADOS", "CPU", "80%", "Espaço livre", "10%", "O QUE O TOOLKIT NUNCA FAZ AQUI", "Não desativar serviço", "Sem \"pontuação de PC\""} {
		if !strings.Contains(strings.ToUpper(d), strings.ToUpper(want)) {
			t.Errorf("faltou %q no painel:\n%s", want, d)
		}
	}
	for _, l := range strings.Split(d, "\n") {
		if len([]rune(l)) > DetailWidth+4 {
			t.Errorf("linha larga demais (%d): %q", len([]rune(l)), l)
		}
	}
}

// Nome de processo, de serviço e de item de inicialização vêm do sistema: nada chega à tela como controle.
func TestOtimTextoDoSistemaSaiLimpo(t *testing.T) {
	rep := otimReport()
	evil := "x\u001b]0;pwn\u0007\u202eevil\u2028"
	rep.CPU.Top[0].Name = evil
	rep.Startup.Items[0].Name = evil
	rep.Services.Failed[0].Name = evil
	tab := newOtim(&fakeOtim{rep: rep}, "windows")
	otimSettle(t, tab, okey(tab, "c"))
	for _, h := range []int{6, 40} {
		v := tab.View(80, h)
		for _, bad := range []string{"\u001b", "\u0007", "\u202e", "\u2028"} {
			if strings.Contains(v, bad) {
				t.Errorf("h=%d: a tela trouxe %U", h, []rune(bad)[0])
			}
		}
	}
}

func TestOtimTeclasNoVocabularioUnico(t *testing.T) {
	tab := newOtim(&fakeOtim{rep: otimReport()}, "windows")
	check := func(state string) {
		have := map[string]bool{}
		for _, h := range helpKeysOf(tab) {
			have[h.Key] = true
			if strings.TrimSpace(h.Help) == "" {
				t.Errorf("%s: %q sem frase", state, h.Key)
			}
		}
		for _, k := range tab.Keys() {
			if r := []rune(k.Key); len(r) == 1 && r[0] >= 'a' && r[0] <= 'z' {
				def := act(k.Key)
				if def.Label == "" || baseLabel(k.Label) != def.Label {
					t.Errorf("%s: %q fora da legenda (%q)", state, k.Key, k.Label)
				}
				if !have[k.Key] {
					t.Errorf("%s: a ajuda não explica %q", state, k.Key)
				}
			}
		}
	}
	check("vazio")
	otimSettle(t, tab, okey(tab, "c"))
	check("lido")
	tab.loaded, tab.loading = false, true
	check("lendo")
}

func TestOtimStatus(t *testing.T) {
	tab := newOtim(&fakeOtim{rep: otimReport()}, "windows")
	if !strings.Contains(tab.Status(), "ainda não lidos") {
		t.Errorf("vazio: %q", tab.Status())
	}
	otimSettle(t, tab, okey(tab, "c"))
	if !strings.Contains(tab.Status(), "só leitura") {
		t.Errorf("lido: %q", tab.Status())
	}
}

// ---- goldens: a seção dentro da área Máquina, Windows falso e fora do Windows ----

func otimModel(w, h int, otim Tab) *Model {
	ds := areaDefs()
	ds[2].Tabs = []Tab{ds[2].Tabs[0], ds[2].Tabs[1], otim}
	m := NewWithAreas(Options{Version: "v0.1.0", Mono: true, NoMotion: true}, ds)
	m.Update(sizeMsg(w, h))
	m.setActive(5) // 0 Início | 1 Apps 2 Receitas | 3 Diagnóstico 4 Apps nativos 5 Otimização
	return m
}

func TestGoldenOtimizacao(t *testing.T) {
	sizes := [][2]int{{80, 24}, {120, 40}, {60, 16}}
	for _, sz := range sizes {
		tab, _ := loadedOtim(t)
		golden(t, fmt.Sprintf("otimizacao_windows_%dx%d", sz[0], sz[1]), otimModel(sz[0], sz[1], tab).render())
		idle := newOtim(&fakeOtim{rep: otimReport()}, "windows")
		golden(t, fmt.Sprintf("otimizacao_windows_vazio_%dx%d", sz[0], sz[1]), otimModel(sz[0], sz[1], idle).render())
		mac := newOtim(&fakeOtim{rep: otimReport()}, "darwin")
		golden(t, fmt.Sprintf("otimizacao_darwin_%dx%d", sz[0], sz[1]), otimModel(sz[0], sz[1], mac).render())
	}
}

func TestGoldenOtimizacaoCarregandoErro(t *testing.T) {
	for _, sz := range [][2]int{{80, 24}} {
		fo := &fakeOtim{rep: otimReport(), hold: make(chan struct{})}
		tab := newOtim(fo, "windows")
		okey(tab, "c")
		golden(t, fmt.Sprintf("otimizacao_carregando_%dx%d", sz[0], sz[1]), otimModel(sz[0], sz[1], tab).render())
		bad := newOtim(&fakeOtim{err: errors.New("PowerShell indisponível")}, "windows")
		otimSettle(t, bad, okey(bad, "c"))
		golden(t, fmt.Sprintf("otimizacao_erro_%dx%d", sz[0], sz[1]), otimModel(sz[0], sz[1], bad).render())
	}
}

// A ajuda "?" da área Máquina inclui a seção Otimização com as teclas do vocabulário.
func TestOtimNaAreaMaquinaTabChega(t *testing.T) {
	tab := newOtim(&fakeOtim{rep: otimReport()}, "windows")
	m := otimModel(80, 24, tab)
	if !strings.Contains(m.render(), "[Otimização]") {
		t.Fatalf("a seção aparece ativa na barra de seções:\n%s", m.render())
	}
	// Tab a partir de Apps nativos chega nela
	m.setActive(4)
	m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	if m.Active() != 5 {
		t.Fatalf("Tab de Apps nativos leva à Otimização: %d", m.Active())
	}
}

// Todas as áreas sem leitura: a seção mostra o resultado INCONCLUSIVO, não "nenhum gargalo evidente".
func TestOtimTudoSemLeituraMostraInconclusivo(t *testing.T) {
	rep := &otimizacao.Report{OS: "windows", Supported: true}
	for _, a := range []string{"cpu", "memoria", "disco", "inicializacao", "servicos", "boot"} {
		rep.Skipped = append(rep.Skipped, otimizacao.Skipped{Area: a, Reason: "não foi possível ler"})
	}
	tab := newOtim(&fakeOtim{rep: rep}, "windows")
	otimSettle(t, tab, okey(tab, "c"))
	all := strings.ToLower(strings.Join(tab.lines, "\n"))
	if !strings.Contains(all, "inconclusivo: nada foi lido") || strings.Contains(all, "nenhum gargalo") {
		t.Fatalf("seção:\n%s", all)
	}
}

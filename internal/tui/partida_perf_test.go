package tui

import (
	"context"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

// Backends que demoram, para simular a partida real (detecção, registro, PowerShell, winget).
type slowApps struct {
	fakeBackend
	d time.Duration
}

func (s *slowApps) Load(ctx context.Context) ([]AppInfo, error) {
	time.Sleep(s.d)
	return s.fakeBackend.Load(ctx)
}

type slowPriv struct {
	*fakePriv
	d time.Duration
}

func (s *slowPriv) Load(ctx context.Context) (PrivInfo, error) {
	time.Sleep(s.d)
	return s.fakePriv.Load(ctx)
}

type slowNat struct {
	*fakeNat
	d time.Duration
}

func (s *slowNat) Load(ctx context.Context) (NatInfo, error) {
	time.Sleep(s.d)
	return s.fakeNat.Load(ctx)
}

type slowRecipes struct {
	fakeRecipes
	d time.Duration
}

func (s *slowRecipes) Load(ctx context.Context) (RecipesData, error) {
	time.Sleep(s.d)
	return s.fakeRecipes.Load(ctx)
}

// loop é um laço mínimo do Bubble Tea sem terminal: executa os comandos em goroutines e entrega as mensagens ao
// Model numa única goroutine, como o Program faz. Mede quantas mensagens de quadro (tickMsg) chegam e quanto cada
// Update demora.
type loop struct {
	m      *Model
	msgs   chan tea.Msg
	ticks  int
	total  int
	maxUpd time.Duration
}

func newLoop(m *Model) *loop { return &loop{m: m, msgs: make(chan tea.Msg, 1<<16)} }

func (l *loop) run(cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	go func() {
		msg := cmd()
		switch v := msg.(type) {
		case nil:
		case tea.BatchMsg:
			for _, c := range v {
				l.run(c)
			}
		default:
			select {
			case l.msgs <- msg:
			default:
			}
		}
	}()
}

func (l *loop) pump(d time.Duration) {
	end := time.After(d)
	for {
		select {
		case <-end:
			return
		case msg := <-l.msgs:
			if _, ok := msg.(tickMsg); ok {
				l.ticks++
			}
			l.total++
			t0 := time.Now()
			_, cmd := l.m.Update(msg)
			if el := time.Since(t0); el > l.maxUpd {
				l.maxUpd = el
			}
			l.run(cmd)
		}
	}
}

func startupModel(delay time.Duration) *Model {
	th := Theme{Mono: true, Dark: true}
	ctx := context.Background()
	apps := &slowApps{fakeBackend: fakeBackend{apps: sampleApps()}, d: delay}
	rec := &slowRecipes{fakeRecipes: fakeRecipes{data: recipeSample()}, d: delay}
	tabs := []Tab{
		NewAppsTab(ctx, apps, th, "linux"),
		NewRecipesTab(ctx, rec, th, false),
		NewNativeAppsTab(ctx, &slowNat{fakeNat: &fakeNat{}, d: delay}, th),
		NewPrivacyTab(ctx, &slowPriv{fakePriv: &fakePriv{}, d: delay}, th),
	}
	return New(Options{}, tabs...)
}

// Regressão do travamento da partida: cada aba que carrega agenda um quadro do spinner e todo quadro era entregue
// a TODAS as abas, que agendavam outro cada uma: com K abas carregando os quadros multiplicavam por K a cada 100 ms
// (4, 80, 1.280, 20.480 por janela de 200 ms com 4 abas). A raiz agora mantém UM relógio de quadros.
func TestPartidaTicksNaoMultiplicam(t *testing.T) {
	m := startupModel(1500 * time.Millisecond)
	l := newLoop(m)
	l.run(m.Init())
	l.pump(1200 * time.Millisecond)
	// um quadro a cada 100 ms: ~12 em 1,2 s (folga para o agendador); a explosão passava de 100 mil
	if l.ticks > 30 {
		t.Fatalf("%d quadros em 1,2 s (esperado ~12): os quadros estão se multiplicando", l.ticks)
	}
	if l.ticks < 3 {
		t.Fatalf("só %d quadros: o spinner não pode parar enquanto as abas carregam", l.ticks)
	}
}

// initTab conta quantas vezes o Init da aba foi pedido.
type initTab struct {
	fakeTab
	inits int
}

func (f *initTab) Init() tea.Cmd { f.inits++; return nil }

// A partida só carrega a seção que aparece: as outras (catálogo + estado de cada app, receitas, apps do Windows,
// privacidade) carregam na PRIMEIRA visita e nunca de novo. Antes todas carregavam juntas na abertura.
func TestInitSoCarregaASecaoAtivaENaPrimeiraVisita(t *testing.T) {
	a, b, c := &initTab{fakeTab: fakeTab{title: "A", short: "A"}}, &initTab{fakeTab: fakeTab{title: "B", short: "B"}}, &initTab{fakeTab: fakeTab{title: "C", short: "C"}}
	m := New(Options{}, a, b, c)
	m.Init()
	if a.inits != 1 || b.inits != 0 || c.inits != 0 {
		t.Fatalf("na abertura só a ativa carrega: a=%d b=%d c=%d", a.inits, b.inits, c.inits)
	}
	m.Update(key('2', "2"))
	if b.inits != 1 || c.inits != 0 {
		t.Fatalf("a primeira visita carrega a seção: b=%d c=%d", b.inits, c.inits)
	}
	m.Update(key('1', "1"))
	m.Update(key('2', "2"))
	if a.inits != 1 || b.inits != 1 {
		t.Fatalf("revisitar não recarrega: a=%d b=%d", a.inits, b.inits)
	}
}

// bigApps devolve n apps sintéticos em categorias variadas, com o estado de cada um já lido.
func bigApps(n int) (*fakeBackend, []AppInfo) {
	cats := []string{"dev", "browser", "media", "util", "office"}
	be := &fakeBackend{status: map[string]AppStatus{}}
	for i := 0; i < n; i++ {
		id := "app-" + itoa(i)
		be.apps = append(be.apps, AppInfo{ID: id, Name: "Aplicativo de teste número " + itoa(i), Category: cats[i%len(cats)], Managers: []string{"winget", "brew"}, Versions: i%7 == 0})
		switch i % 4 {
		case 0:
			be.status[id] = AppStatus{Installed: true, Version: "1.2." + itoa(i)}
		case 1:
			be.status[id] = AppStatus{Installed: true, External: true, ExternalManager: "winget", ExternalWhere: "Programas e recursos do Windows [HKLM]"}
		}
	}
	return be, be.apps
}

func bigModel(t testing.TB, n int) *Model {
	be, _ := bigApps(n)
	tab := newApps(be)
	tab.lastH = 40
	tab.Update(tab.loadCmd()())
	for len(tab.pending) > 0 || len(tab.queue) > 0 {
		var ids []string
		for id := range tab.pending {
			ids = append(ids, id)
		}
		if len(ids) == 0 {
			t.Fatal("fila parada")
		}
		for _, id := range ids {
			tab.Update(appStateMsg{id: id, st: be.status[id]})
		}
	}
	m := New(Options{Version: "v0.4.0"}, tab)
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	return m
}

func BenchmarkViewApps300(b *testing.B) {
	m := bigModel(b, 300)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = m.View()
	}
}

func BenchmarkTeclaSetaApps300(b *testing.B) { // Update + View por tecla, como o Program faz
	m := bigModel(b, 300)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
		_ = m.View()
	}
}

// firstFrame e keyLatency medem o que o Paulo sentiu: a primeira tela e a seta durante a carga.
func TestPartidaPrimeiraTelaENaoBloqueiaTecla(t *testing.T) {
	m := startupModel(4 * time.Second)
	t0 := time.Now()
	cmd := m.Init()
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	first := m.View().Content
	if el := time.Since(t0); el > 250*time.Millisecond {
		t.Fatalf("a primeira tela levou %v (teto 250 ms): Init/Update não podem esperar I/O", el)
	}
	if first == "" {
		t.Fatal("primeira tela vazia")
	}
	l := newLoop(m)
	l.run(cmd)
	l.pump(1500 * time.Millisecond) // carga em andamento (spinner animando)
	// a seta entra na fila junto com o resto: tem de ser tratada e desenhada rápido, sem fila atrás de quadros
	sent := time.Now()
	l.msgs <- tea.KeyPressMsg{Code: tea.KeyDown}
	var handled time.Duration
	done := false // não usar handled == 0: no Windows time.Since pode dar 0 para intervalos curtos
	end := time.After(2 * time.Second)
	for !done {
		select {
		case <-end:
			t.Fatal("a seta não foi tratada em 2 s: fila entupida")
		case msg := <-l.msgs:
			_, cmd := l.m.Update(msg)
			_ = l.m.View()
			l.run(cmd)
			if _, ok := msg.(tea.KeyPressMsg); ok {
				handled, done = time.Since(sent), true
			}
		}
	}
	t.Logf("seta tratada e desenhada em %v; %d quadros na partida", handled, l.ticks)
	if handled > 100*time.Millisecond {
		t.Fatalf("seta tratada e desenhada em %v (teto 100 ms)", handled)
	}
}

// Teto do View com volume realista (300 apps): o desenho de uma tecla não pode crescer sem aviso.
func TestViewCom300AppsTemTeto(t *testing.T) {
	m := bigModel(t, 300)
	const n = 200
	t0 := time.Now()
	for i := 0; i < n; i++ {
		m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
		_ = m.View()
	}
	if avg := time.Since(t0) / n; avg > 50*time.Millisecond {
		t.Fatalf("tecla + View com 300 apps: %v em média (teto 50 ms; local ~0,2 ms, runner compartilhado ~9 ms)", avg)
	}
}

// Depois de uma execução só as seções que já foram abertas são relidas; a nunca aberta carrega do zero na visita.
func TestRunClosedReleSoSecoesJaAbertas(t *testing.T) {
	a := &reloadSpy{fakeTab: fakeTab{title: "A", short: "A"}}
	b := &reloadSpy{fakeTab: fakeTab{title: "B", short: "B"}}
	m := New(Options{}, a, b)
	m.Init() // abre só A
	m.Update(runClosedMsg{})
	if a.reloads != 1 || b.reloads != 0 {
		t.Fatalf("releituras: a=%d b=%d (esperado 1 e 0)", a.reloads, b.reloads)
	}
}

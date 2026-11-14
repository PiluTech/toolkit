package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"unicode"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// helpAreas monta as seções REAIS (as mesmas do `toolkit`, com backends de mentira), já carregadas, na ordem das áreas:
// 0 Início | 1 Instalar (Apps, Receitas) | 2 Máquina (Diagnóstico, Apps nativos, Otimização) | 3 Segurança (Privacidade) | 4 Rede | 5 Histórico.
func helpAreas(t *testing.T) []AreaDef {
	t.Helper()
	th := Theme{Mono: true, NoMotion: true}
	apps := loaded(t, &fakeBackend{apps: sampleApps()})
	apps.theme = th
	rec := loadedRecipes(t, recipeSample(), false)
	diag := newDiag(t, &fakeMachine{rep: machineReport()})
	nat := natTab(t, &fakeNat{info: natSample(true), plan: NatPlan{Text: "plano", Pending: 1}})
	priv := privTab(t, &fakePriv{info: privSample(true), plan: PrivPlan{Text: "plano", Pending: 1, NeedsAdmin: true, IsAdmin: true}})
	otim := newOtim(&fakeOtim{rep: otimReport()}, "windows")
	inicio := NewInicioTab(th, "Início", "Iníc", "O que você quer fazer?", []InicioItem{{Title: "Instalar apps", Desc: "d", Tag: "✓", Area: 1, Section: 0, Dest: "Apps"}})
	rede := NewInfoTab(th, "Análise", "Anál", "A análise de rede ainda não existe.")
	execs := NewLogTab(th, "")
	execs.SetNames("Execuções", "Exec", "detalhes")
	return []AreaDef{
		{Key: "inicio", Title: "Início", Short: "Iníc", Tabs: []Tab{inicio}},
		{Key: "instalar", Title: "Instalar", Short: "Inst", Tabs: []Tab{apps, rec}},
		{Key: "maquina", Title: "Máquina", Short: "Máq", Tabs: []Tab{diag, nat, otim}},
		{Key: "seguranca", Title: "Segurança", Short: "Seg", Tabs: []Tab{priv}},
		{Key: "rede", Title: "Rede", Short: "Rede", Tabs: []Tab{rede}},
		{Key: "historico", Title: "Histórico", Short: "Hist", Tabs: []Tab{execs}},
	}
}

func helpModel(t *testing.T, w, h int) *Model {
	t.Helper()
	m := NewWithAreas(Options{Version: "v0.1.0", Mono: true, NoMotion: true}, helpAreas(t))
	m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	return m
}

func baseLabel(l string) string {
	return strings.TrimSuffix(strings.TrimSuffix(l, "…"), " ✓")
}

func TestLegendaSemTeclasDuplicadas(t *testing.T) {
	seen := map[string]string{}
	for _, k := range actionKeys {
		if k.Key == "" || k.Label == "" || k.Help == "" {
			t.Errorf("entrada incompleta: %+v", k)
		}
		if prev, dup := seen[k.Key]; dup {
			t.Errorf("tecla %q repetida na tabela de ações (%s)", k.Key, prev)
		}
		seen[k.Key] = k.Label
	}
	for _, area := range []bool{false, true} {
		for _, g := range globalKeys(area, []string{"A", "B"}) {
			if g.Key == "" || g.Help == "" {
				t.Errorf("global incompleta: %+v", g)
			}
			if prev, dup := seen[g.Key]; dup {
				t.Errorf("global %q repetida (%s)", g.Key, prev)
			}
			if !area {
				continue
			}
			for _, f := range strings.Fields(g.Key) {
				if len(f) == 1 && f[0] >= 'a' && f[0] <= 'z' {
					if _, clash := seen[f]; clash {
						t.Errorf("global %q colide com uma ação", f)
					}
				}
			}
		}
	}
	// o sentido de cada letra do pedido
	for k, want := range map[string]string{"a": "aplicar", "d": "desfazer", "p": "ver plano", "c": "ler de novo", "r": "remover", "m": "alternar", "i": "como reinstalar"} {
		if got := act(k).Label; got != want {
			t.Errorf("%s = %q, quer %q", k, got, want)
		}
	}
	// só as ações que abrem confirmação levam reticências
	for _, k := range actionKeys {
		wantConf := k.Key == "a" || k.Key == "d" || k.Key == "r"
		if k.Confirms != wantConf {
			t.Errorf("%s: Confirms=%v", k.Key, k.Confirms)
		}
		if strings.Contains(k.Label, "…") {
			t.Errorf("%s: o rótulo da tabela não leva reticências (vêm de Confirms)", k.Key)
		}
	}
}

// states percorre os estados com teclas diferentes de cada seção real e devolve as Keys() de cada um.
func realKeyStates(t *testing.T) map[string][]KeyHint {
	t.Helper()
	ds := helpAreas(t)
	get := func(a, s int) Tab { return ds[a].Tabs[s] }
	snap := map[string][]KeyHint{}
	add := func(name string, tb Tab) { snap[name] = tb.Keys() }

	apps := get(1, 0).(*AppsTab)
	add("apps", apps)
	apps.Update(key('/', "/"))
	add("apps/busca", apps)
	apps.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	apps.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	apps.Update(key('v', "v"))
	add("apps/versoes", apps)

	rec := get(1, 1).(*RecipesTab)
	add("receitas", rec)
	rec.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	add("receitas/foco", rec)

	diag := get(2, 0).(*DiagTab)
	add("diagnostico", diag)
	diag.Update(key('m', "m"))
	add("diagnostico/maquina", diag)

	nat := get(2, 1).(*NativeAppsTab)
	add("nativos", nat)
	nkey(nat, "space", "p")
	add("nativos/plano", nat)
	nkey(nat, "esc", "r")
	add("nativos/confirma", nat)

	otim := get(2, 2).(*OptimizeTab)
	add("otimizacao", otim)
	otim.loading = true
	add("otimizacao/lendo", otim)
	otim.loading, otim.loaded = false, true
	add("otimizacao/lido", otim)
	otim.loaded = false
	foraWin := newOtim(&fakeOtim{}, "darwin")
	add("otimizacao/foradowindows", foraWin)

	priv := get(3, 0).(*PrivacyTab)
	add("privacidade", priv)
	pkey(priv, "p")
	add("privacidade/plano", priv)

	add("inicio", get(0, 0))
	add("rede", get(4, 0))
	add("execucoes", get(5, 0))
	return snap
}

// REGRA: a mesma letra tem o mesmo sentido em todas as seções. Percorre as seções reais (montadas por NewWithAreas) e
// confere cada letra de ação de Keys() contra a legenda: rótulo igual (ou variante permitida) e "…" só em quem confirma.
func TestVocabularioDeTeclasIgualEmTodasAsSecoes(t *testing.T) {
	m := NewWithAreas(Options{Mono: true}, helpAreas(t))
	if len(m.tabs) != 9 {
		t.Fatalf("seções: %d", len(m.tabs))
	}
	used := map[string]map[string]string{}
	for state, hints := range realKeyStates(t) {
		for _, h := range hints {
			r := []rune(h.Key)
			if len(r) != 1 || !unicode.IsLower(r[0]) && h.Key != "espaço" {
				continue
			}
			def := act(h.Key)
			if def.Label == "" {
				t.Errorf("%s: a tecla %q não está na legenda (keys.go)", state, h.Key)
				continue
			}
			base := baseLabel(h.Label)
			ok := base == def.Label
			for _, a := range def.Alt {
				ok = ok || base == a
			}
			if !ok {
				t.Errorf("%s: %q com o rótulo %q; a legenda diz %q (variantes %v)", state, h.Key, base, def.Label, def.Alt)
			}
			if strings.HasSuffix(h.Label, "…") != def.Confirms {
				t.Errorf("%s: %q reticências=%v, legenda diz %v", state, h.Key, strings.HasSuffix(h.Label, "…"), def.Confirms)
			}
			if used[h.Key] == nil {
				used[h.Key] = map[string]string{}
			}
			used[h.Key][state] = base
		}
	}
	// as cinco letras do pedido nunca mudam de sentido
	for _, k := range []string{"a", "d", "p", "c", "r"} {
		labels := map[string]bool{}
		for _, l := range used[k] {
			labels[l] = true
		}
		if len(labels) > 1 {
			t.Errorf("%q com mais de um sentido: %v", k, used[k])
		}
	}
}

// toda letra da barra é explicada na ajuda da seção (HelpKeys), com frase, e as letras da ajuda são da legenda.
func TestAjudaDaSecaoExplicaTodaLetraDaBarra(t *testing.T) {
	for _, tb := range NewWithAreas(Options{Mono: true}, helpAreas(t)).tabs {
		help := helpKeysOf(tb)
		have := map[string]bool{}
		for _, h := range help {
			have[h.Key] = true
			if strings.TrimSpace(h.Help) == "" {
				t.Errorf("%s: %q sem frase de ajuda", tb.Title(), h.Key)
			}
			if r := []rune(h.Key); len(r) == 1 && unicode.IsLower(r[0]) && act(h.Key).Label == "" {
				t.Errorf("%s: a letra %q da ajuda não está na legenda", tb.Title(), h.Key)
			}
		}
		for _, k := range tb.Keys() {
			if r := []rune(k.Key); len(r) == 1 && unicode.IsLower(r[0]) && !have[k.Key] {
				t.Errorf("%s: a barra tem %q mas a ajuda não o explica", tb.Title(), k.Key)
			}
		}
	}
}

// Conflito conhecido: em Apps nativos "c" ler de novo (ou nada), nunca marca o perfil completo.
func TestAppsNativosCNaoMarcaPerfilCompleto(t *testing.T) {
	be := &fakeNat{info: natSample(true)}
	tab := natTab(t, be)
	nkey(tab, "c")
	if len(tab.selection().Apps) != 0 {
		t.Fatalf("c marcou %v", tab.selection().Apps)
	}
	nkey(tab, "b")
	basic := len(tab.selection().Apps)
	if basic == 0 {
		t.Fatal("b deve marcar o perfil básico")
	}
	nkey(tab, "t")
	if all := len(tab.selection().Apps); all <= basic || all != len(tab.info.Apps) {
		t.Fatalf("t deve marcar todo o catálogo: %d de %d (básico %d)", all, len(tab.info.Apps), basic)
	}
	// "u" é a tecla global da versão nova: a seção não a usa mais para "todos os usuários"; quem alterna é o "m"
	nkey(tab, "u")
	if tab.allUsers {
		t.Fatal("u não deve alternar o escopo")
	}
	nkey(tab, "m")
	if !tab.allUsers {
		t.Fatal("m deve alternar todos os usuários")
	}
}

// "d" é desfazer em toda parte: no Diagnóstico os detalhes passaram para o "e".
func TestDiagnosticoDetalhesNoE(t *testing.T) {
	tab := loadedRecipes(t, recipeSample(), true)
	tab.Update(key('d', "d"))
	if tab.details {
		t.Fatal("d não pode mais ligar os detalhes")
	}
	tab.Update(key('e', "e"))
	if !tab.details {
		t.Fatal("e liga os detalhes")
	}
	var got string
	for _, k := range tab.Keys() {
		if k.Key == "e" {
			got = k.Label
		}
	}
	if got != "detalhes ✓" {
		t.Fatalf("dica do e: %q", got)
	}
}

// a confirmação mostra a dica só na barra; a linha de estado diz o estado, sem repetir as teclas.
func TestConfirmacaoNaoRepeteDicaNaLinhaDeEstado(t *testing.T) {
	be := &fakePriv{info: privSample(true), plan: PrivPlan{Text: "plano", Pending: 2, NeedsAdmin: true, IsAdmin: true}}
	tab := privTab(t, be)
	pkey(tab, "a")
	if !tab.asking {
		t.Fatal("a deveria abrir a confirmação")
	}
	st := tab.Status()
	for _, k := range tab.Keys() {
		if strings.Contains(st, k.Key+" ") && len(k.Key) > 1 {
			t.Errorf("a linha de estado repete a dica %q: %q", k.Key, st)
		}
	}
	for _, bad := range []string{"Enter", "Esc", "Tab"} {
		if strings.Contains(st, bad) {
			t.Errorf("a linha de estado repete teclas (%s): %q", bad, st)
		}
	}
	if strings.TrimSpace(st) == "" {
		t.Error("a linha de estado deveria dizer o estado da confirmação")
	}
}

// pressKey é a tecla da raiz.
// goArea abre a área n direto (a tecla de número do Início é dela, e a navegação sairia num Cmd).
func goArea(m *Model, n int) { m.gotoArea(n) }

func rootKey(m *Model, k string) {
	var msg tea.KeyPressMsg
	switch k {
	case "esc":
		msg = tea.KeyPressMsg{Code: tea.KeyEscape}
	case "down":
		msg = tea.KeyPressMsg{Code: tea.KeyDown}
	case "up":
		msg = tea.KeyPressMsg{Code: tea.KeyUp}
	case "pgdown":
		msg = tea.KeyPressMsg{Code: tea.KeyPgDown}
	case "pgup":
		msg = tea.KeyPressMsg{Code: tea.KeyPgUp}
	default:
		msg = tea.KeyPressMsg{Code: rune(k[0]), Text: k}
	}
	m.Update(msg)
}

func helpPages(t *testing.T, m *Model) (pages [][]string, more []string) {
	t.Helper()
	for i := 0; i < 200; i++ {
		lines := strings.Split(m.render(), "\n")
		pages = append(pages, lines)
		rule := lines[len(lines)-3]
		more = append(more, rule)
		if !strings.Contains(rule, "mais") {
			return pages, more
		}
		rootKey(m, "pgdown")
	}
	t.Fatal("a ajuda nunca chega ao fim")
	return
}

func TestAjudaGeradaCabeEmTodosOsTamanhos(t *testing.T) {
	for _, sz := range [][2]int{{60, 16}, {80, 24}, {120, 40}, {60, 40}, {120, 16}, {70, 20}} {
		for _, area := range []int{'1', '2', '3', '5'} {
			m := helpModel(t, sz[0], sz[1])
			goArea(m, area-'0')
			rootKey(m, "?")
			if m.modal != modalHelp {
				t.Fatalf("%v: a ajuda não abriu", sz)
			}
			pages, _ := helpPages(t, m)
			var all []string
			for _, p := range pages {
				if len(p) != sz[1] {
					t.Fatalf("%v: %d linhas, quer %d", sz, len(p), sz[1])
				}
				for _, l := range p {
					if w := lipgloss.Width(l); w > sz[0] {
						t.Errorf("%v: linha com %d colunas: %q", sz, w, l)
					}
					if strings.Contains(l, "\x1b") {
						t.Errorf("%v: Mono com sequência de estilo: %q", sz, l)
					}
				}
				all = append(all, p...)
			}
			joined := strings.Join(strings.Fields(strings.Join(all, "\n")), " ")
			for _, want := range []string{"em qualquer lugar", "nesta seção:", "como ler a tela"} {
				if !strings.Contains(strings.ToLower(joined), want) {
					t.Errorf("%v área %c: falta o bloco %q", sz, area, want)
				}
			}
			// texto sem reticências de corte: nada é cortado, tudo quebra; a ajuda inteira aparece rolando
			for _, p := range pages {
				for _, l := range p[2 : len(p)-3] {
					if strings.HasSuffix(strings.TrimRight(l, " "), "…") && !strings.Contains(l, "  ") {
						t.Errorf("%v: linha cortada com reticências: %q", sz, l)
					}
				}
			}
			for _, g := range globalKeys(true, m.areaTitles()) {
				if !strings.Contains(joined, strings.Join(strings.Fields(g.Help), " ")) {
					t.Errorf("%v: frase global ausente: %q", sz, g.Help)
				}
			}
			for _, h := range helpKeysOf(m.tabs[m.active]) {
				if !strings.Contains(joined, strings.Join(strings.Fields(h.Help), " ")) {
					t.Errorf("%v: frase da seção ausente: %q", sz, h.Help)
				}
			}
		}
	}
}

func TestAjudaRolaEFecha(t *testing.T) {
	m := helpModel(t, 60, 16)
	goArea(m, 1)
	rootKey(m, "?")
	first := m.render()
	if !strings.Contains(first, "mais") {
		t.Fatalf("60x16 deveria ter rolagem:\n%s", first)
	}
	rootKey(m, "down")
	if m.render() == first {
		t.Fatal("seta para baixo deveria rolar a ajuda")
	}
	rootKey(m, "up")
	if m.render() != first {
		t.Fatal("seta para cima deveria voltar")
	}
	rootKey(m, "esc")
	for _, k := range []string{"?", "esc", "q"} {
		rootKey(m, "?")
		if m.modal != modalHelp {
			t.Fatalf("antes de %s a ajuda deveria estar aberta", k)
		}
		rootKey(m, k)
		if m.modal != modalNone || m.Quitting() {
			t.Fatalf("%s deveria fechar a ajuda sem sair", k)
		}
	}
	// reabrir começa do topo
	rootKey(m, "?")
	rootKey(m, "pgdown")
	rootKey(m, "esc")
	rootKey(m, "?")
	if m.render() != first {
		t.Fatal("a ajuda reaberta começa do topo")
	}
}

func TestAjudaMudaDeBlocoComASecao(t *testing.T) {
	want := map[string][]string{
		"1": {"Nesta seção: Apps", "buscar", "versões"},
		"2": {"Nesta seção: Diagnóstico", "alterna", "detalhes"},
		"3": {"Nesta seção: Privacidade", "aplicar…", "desfazer…", "ver plano"},
		"5": {"Nesta seção: Execuções", "seguir o fim"},
	}
	for k, wants := range want {
		m := helpModel(t, 120, 60)
		goArea(m, int(k[0]-'0'))
		rootKey(m, "?")
		scr := m.render()
		for _, w := range wants {
			if !strings.Contains(strings.ToLower(scr), strings.ToLower(w)) {
				t.Errorf("área %s: falta %q:\n%s", k, w, scr)
			}
		}
	}
	// trocar de seção com a ajuda fechada muda o bloco
	m := helpModel(t, 120, 60)
	goArea(m, 1)
	rootKey(m, "?")
	a := m.render()
	rootKey(m, "?")
	rootKey(m, "tab")
	rootKey(m, "?")
	if b := m.render(); b == a || !strings.Contains(b, "NESTA SEÇÃO: RECEITAS") {
		t.Fatalf("a seção seguinte deveria trazer o seu bloco:\n%s", b)
	}
	// Apps nativos: as teclas de perfil novas e a decisão ficam na tela
	m = helpModel(t, 120, 60)
	goArea(m, 2)
	rootKey(m, "tab")
	rootKey(m, "?")
	scr := m.render()
	for _, w := range []string{"nesta seção: apps nativos", "b ", "básico", "t ", "todos", "remover…", "como reinstalar"} {
		if !strings.Contains(strings.ToLower(scr), w) {
			t.Errorf("Apps nativos: falta %q:\n%s", w, scr)
		}
	}
}

func TestAjudaMarcaReticenciasEExplica(t *testing.T) {
	m := helpModel(t, 120, 60)
	goArea(m, 3)
	rootKey(m, "?")
	scr := m.render()
	if !strings.Contains(scr, "…") || !strings.Contains(scr, "confirmação") {
		t.Fatalf("a ajuda explica o que as reticências querem dizer:\n%s", scr)
	}
	for _, w := range []string{"✓", "!", "✗", "i ", "faixa de resultado", "admin: sim", ">[ ]<"} {
		if !strings.Contains(scr, w) {
			t.Errorf("Como ler a tela: falta %q", w)
		}
	}
}

func TestAjudaTextoSeguroNoTitulo(t *testing.T) {
	ds := areaDefs()
	ds[1].Tabs[0] = &fakeTab{title: "Ap\x1b[31mps\x07", short: "A", body: "x"}
	m := NewWithAreas(Options{Mono: true}, ds)
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	goArea(m, 1)
	rootKey(m, "?")
	scr := m.render()
	i := strings.Index(scr, "NESTA")
	if i < 0 {
		t.Fatal("falta o bloco da seção")
	}
	scr = scr[i:] // o cabeçalho das seções não é da ajuda
	if strings.ContainsAny(scr, "\x1b\x07") {
		t.Fatalf("a ajuda deixou passar controle: %q", scr)
	}
	if !strings.Contains(scr, "NESTA SEÇÃO:") {
		t.Fatal("falta o bloco da seção")
	}
}

func TestAjudaModoLegadoMostraAbas(t *testing.T) {
	m := sized(Options{Mono: true}, 80, 40, fakes()...)
	rootKey(m, "?")
	scr := m.render()
	if !strings.Contains(scr, "EM QUALQUER LUGAR") || !strings.Contains(scr, "NESTA SEÇÃO: APPS") || !strings.Contains(scr, "aba") {
		t.Fatalf("ajuda do modo legado:\n%s", scr)
	}
}

func TestAjudaGoldens(t *testing.T) {
	cases := []struct {
		name string
		area string
		tab  bool
	}{{"apps", "1", false}, {"maquina", "2", false}, {"priv", "3", false}}
	for _, c := range cases {
		for _, sz := range [][2]int{{80, 24}, {120, 40}, {60, 16}} {
			m := helpModel(t, sz[0], sz[1])
			goArea(m, int(c.area[0]-'0'))
			rootKey(m, "?")
			golden(t, fmt.Sprintf("ajuda_%s_%dx%d", c.name, sz[0], sz[1]), m.render())
		}
	}
}

// Fora do Windows (e carregando ou com erro) a barra não oferece teclas de ação: a ajuda "?" não pode listá-las.
func TestAjudaSemAcoesQuandoASecaoNaoAgeFora_NativosEPrivacidade(t *testing.T) {
	th := Theme{Mono: true, NoMotion: true}
	cases := []struct {
		name string
		tab  func() Tab
	}{
		{"nativos fora do Windows", func() Tab { return natTab(t, &fakeNat{info: NatInfo{Supported: false, OS: "darwin"}}) }},
		{"privacidade fora do Windows", func() Tab { return privTab(t, &fakePriv{info: PrivInfo{Supported: false, OS: "darwin"}}) }},
		{"nativos com erro", func() Tab { return natTab(t, &fakeNat{loadErr: errors.New("falhou")}) }},
		{"privacidade com erro", func() Tab { return privTab(t, &fakePriv{loadErr: errors.New("falhou")}) }},
		{"nativos carregando", func() Tab { return NewNativeAppsTab(context.Background(), &fakeNat{info: natSample(true)}, th) }},
		{"privacidade carregando", func() Tab { return NewPrivacyTab(context.Background(), &fakePriv{info: privSample(true)}, th) }},
	}
	for _, c := range cases {
		tab := c.tab()
		if len(tab.Keys()) != 0 {
			t.Fatalf("%s: a barra devia estar sem teclas de ação: %v", c.name, tab.Keys())
		}
		m := NewWithAreas(Options{Mono: true, NoMotion: true}, []AreaDef{{Key: "x", Title: "X", Short: "X", Tabs: []Tab{tab}}})
		m.Update(tea.WindowSizeMsg{Width: 100, Height: 60})
		help := strings.Join(m.helpLines(), "\n")
		help, _, _ = strings.Cut(help, "EM QUALQUER LUGAR") // só o bloco "Nesta seção"
		for _, bad := range []string{"remover", "aplicar", "desfazer", "ver plano", "como reinstalar", "marcar", "básico"} {
			if strings.Contains(help, bad) {
				t.Errorf("%s: a ajuda lista %q, que a barra não oferece:\n%s", c.name, bad, help)
			}
		}
	}
	// a explicação curta fora do Windows
	tab := natTab(t, &fakeNat{info: NatInfo{Supported: false, OS: "darwin"}})
	if h := helpKeysOf(tab); len(h) != 1 || !strings.Contains(h[0].Help, "só existe no Windows; aqui não há ações") {
		t.Errorf("explicação curta ausente: %+v", h)
	}
}

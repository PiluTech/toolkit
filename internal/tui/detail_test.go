package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
)

// detailTab é uma seção falsa que implementa Detailer: guarda a largura que recebeu em View e Detail.
type detailTab struct {
	fakeTab
	detail         string
	viewW, detailW int
	detailH, viewH int
	detailCalls    int
	result         *Result
	listLine       string
}

func (d *detailTab) View(w, h int) string {
	d.viewW, d.viewH = w, h
	line := d.listLine
	if line == "" {
		line = " lista"
	}
	return strings.Repeat(line+"\n", h)
}

func (d *detailTab) Detail(w, h int) string {
	d.detailCalls++
	d.detailW, d.detailH = w, h
	return d.detail
}

func (d *detailTab) Result() *Result { return d.result }
func (d *detailTab) Dismiss()        { d.result = nil }

func newDetailTab(detail string) *detailTab {
	return &detailTab{fakeTab: fakeTab{title: "Apps", short: "Apps"}, detail: detail}
}

func detailModel(w, h int, tab Tab) *Model {
	return sized(Options{Version: "v0.1.0", Mono: true, NoMotion: true}, w, h, tab)
}

// bodyOf devolve só as linhas do corpo (entre as duas réguas do modo legado: cabeçalho+régua em cima, régua+estado+teclas embaixo).
func bodyOf(m *Model) []string {
	rows := strings.Split(m.render(), "\n")
	return rows[2 : len(rows)-3]
}

func TestDetalheDivisaoPorLargura(t *testing.T) {
	cases := []struct {
		w, h     int
		panel    bool
		listW    int
		detailWd int
	}{
		{60, 24, false, 60, 0},
		{80, 24, false, 80, 0},
		{109, 24, false, 109, 0},
		{110, 24, true, 65, 44},
		{110, 30, true, 65, 44},
		{120, 40, true, 75, 44},
		{200, 50, true, 155, 44},
	}
	for _, c := range cases {
		tab := newDetailTab(" Git\n id git")
		m := detailModel(c.w, c.h, tab)
		body := bodyOf(m)
		if len(body) != c.h-5 {
			t.Fatalf("%dx%d: corpo com %d linhas, quero %d", c.w, c.h, len(body), c.h-5)
		}
		if tab.viewW != c.listW {
			t.Errorf("%dx%d: a lista recebeu %d colunas, quero %d", c.w, c.h, tab.viewW, c.listW)
		}
		if tab.viewH != c.h-5 {
			t.Errorf("%dx%d: a lista recebeu %d linhas, quero %d", c.w, c.h, tab.viewH, c.h-5)
		}
		for i, l := range body {
			if lipgloss.Width(l) > c.w {
				t.Errorf("%dx%d linha %d passa da largura: %q", c.w, c.h, i, l)
			}
			idx := strings.Index(l, "│")
			if c.panel {
				if idx < 0 || lipgloss.Width(l[:idx]) != c.listW {
					t.Errorf("%dx%d linha %d: separador fora da coluna %d: %q", c.w, c.h, i, c.listW, l)
				}
			} else if idx >= 0 {
				t.Errorf("%dx%d: não devia haver painel: %q", c.w, c.h, l)
			}
		}
		if c.panel {
			if tab.detailW != c.detailWd || tab.detailH != c.h-5 {
				t.Errorf("%dx%d: Detail(%d,%d), quero (%d,%d)", c.w, c.h, tab.detailW, tab.detailH, c.detailWd, c.h-5)
			}
			if !strings.Contains(body[0], "│ Git") || !strings.Contains(body[1], "│ id git") {
				t.Errorf("%dx%d: o texto do painel não está logo depois do separador:\n%s", c.w, c.h, strings.Join(body, "\n"))
			}
		} else if tab.detailCalls != 0 {
			t.Errorf("%dx%d: abaixo de 110 colunas o Detail nem deve ser chamado", c.w, c.h)
		}
	}
}

func TestDetalheVazioNaoAbrePainel(t *testing.T) {
	tab := newDetailTab("")
	m := detailModel(120, 40, tab)
	_ = m.render()
	if tab.viewW != 120 {
		t.Fatalf("Detail vazio: a lista devia ter a largura toda, recebeu %d", tab.viewW)
	}
	if strings.Contains(m.render(), "│") {
		t.Fatalf("Detail vazio não pode desenhar separador:\n%s", m.render())
	}
}

func TestDetalheNaoApareceForaDaVisaoNormal(t *testing.T) {
	setup := map[string]func(m *Model){
		"execução":  func(m *Model) { m.mode = modeRun },
		"pergunta":  func(m *Model) { m.asking, m.question = askInterrupt, "Interromper?" },
		"ajuda":     func(m *Model) { m.modal = modalHelp },
		"versão":    func(m *Model) { m.modal = modalUpdate },
		"revisão":   func(m *Model) { m.mode = modeReview; m.review = &reviewScreen{} },
		"aviso":     nil,
		"tamanho":   nil,
		"confirmar": nil,
	}
	for name, f := range setup {
		if f == nil {
			continue
		}
		tab := newDetailTab(" Git")
		m := detailModel(120, 40, tab)
		f(m)
		out := m.render()
		for _, l := range strings.Split(out, "\n")[2:] {
			if strings.Contains(l, "│") && strings.Contains(l, "Git") {
				t.Errorf("%s: o painel não devia aparecer:\n%s", name, out)
				break
			}
		}
	}
}

func TestDetalheFaixaDeResultadoEmLarguraTotal(t *testing.T) {
	tab := newDetailTab(" Git")
	tab.result = &Result{Kind: ResultOK, Title: "Instalado: Git", Lines: []string{"Para desfazer: toolkit remove git"}}
	m := detailModel(120, 40, tab)
	rows := strings.Split(m.render(), "\n")
	// legado: linha 1 abas, 2 régua, depois a faixa; nenhuma linha da faixa tem o separador
	foundTitle := false
	stripEnd := 2
	for i, l := range rows[2:] {
		if strings.HasPrefix(l, SymRule) {
			stripEnd = 2 + i
			break
		}
	}
	if stripEnd == 2 {
		t.Fatalf("régua da faixa não encontrada:\n%s", m.render())
	}
	for i, l := range rows[2:stripEnd] {
		if strings.Contains(l, "Instalado: Git") {
			foundTitle = true
			if !strings.HasSuffix(strings.TrimRight(l, " "), "[x] fechar") {
				t.Errorf("a faixa devia usar a largura toda (dica [x] fechar à direita): %q", l)
			}
		}
		if strings.Contains(l, "│") {
			t.Errorf("linha %d da faixa tem o separador: %q", i, l)
		}
	}
	if !foundTitle {
		t.Fatalf("faixa não encontrada:\n%s", m.render())
	}
	if !strings.Contains(m.render(), "│ Git") {
		t.Errorf("o painel continua ao lado da lista com a faixa aberta:\n%s", m.render())
	}
	if tab.detailH != tab.viewH {
		t.Errorf("lista (%d) e painel (%d) com alturas diferentes", tab.viewH, tab.detailH)
	}
}

func TestDetalheRenderNuncaPassaDaJanela(t *testing.T) {
	for _, sz := range [][2]int{{110, 24}, {110, 30}, {120, 40}, {160, 50}} {
		for _, mono := range []bool{true, false} {
			tab := newDetailTab(" Git\n\n " + strings.Repeat("palavra ", 40))
			m := sized(Options{Version: "v0.1.0", Mono: mono}, sz[0], sz[1], tab)
			rows := strings.Split(m.render(), "\n")
			if len(rows) != sz[1] {
				t.Errorf("%v: %d linhas", sz, len(rows))
			}
			for i, r := range rows {
				if lipgloss.Width(r) > sz[0] {
					t.Errorf("%v linha %d: %d colunas", sz, i, lipgloss.Width(r))
				}
			}
		}
	}
}

func TestDetalheCorpoCortadoMostraMaisLinhas(t *testing.T) {
	d := newDetail(Theme{Mono: true}, 44)
	for i := 0; i < 12; i++ {
		d.para(fmt.Sprintf("linha %d", i))
	}
	got := strings.Split(d.render(5), "\n")
	if len(got) != 5 {
		t.Fatalf("%d linhas, quero 5", len(got))
	}
	if got[3] != " linha 3" || !strings.Contains(got[4], SymMore+" mais 8 linhas") {
		t.Fatalf("corte errado:\n%s", strings.Join(got, "\n"))
	}
	if all := d.render(40); strings.Contains(all, "mais") {
		t.Fatalf("cabendo, nada é cortado: %q", all)
	}
	if strings.Contains(strings.Join(got, ""), "…") {
		t.Fatal("o painel nunca usa reticências")
	}
}

func TestDetalheQuebraSemReticenciasETextsafe(t *testing.T) {
	d := newDetail(Theme{Mono: true}, 44)
	d.name("Nome \x1b[31mvermelho\x1b[0m \u202ebidi " + strings.Repeat("longo", 20))
	d.para("Palavra enorme: " + strings.Repeat("Q", 100))
	d.item("item " + strings.Repeat("abc ", 30))
	out := d.render(60)
	if strings.ContainsAny(out, "\x1b\u202e") {
		t.Fatalf("saiu controle ou bidi: %q", out)
	}
	if strings.Contains(out, "…") {
		t.Fatalf("o painel quebra, nunca corta com reticências:\n%s", out)
	}
	for i, l := range strings.Split(out, "\n") {
		if lipgloss.Width(l) > 44 {
			t.Errorf("linha %d com %d colunas: %q", i, lipgloss.Width(l), l)
		}
	}
	// a palavra de 100 colunas aparece inteira, repartida em várias linhas
	if got := strings.Count(out, "Q"); got != 100 {
		t.Errorf("perdeu texto na quebra: %d Q, quero 100", got)
	}
}

func detailLines(t *testing.T, s string, w int) []string {
	t.Helper()
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		if lipgloss.Width(l) > w {
			t.Errorf("linha %d com %d colunas (> %d): %q", i, lipgloss.Width(l), w, l)
		}
		if strings.ContainsAny(l, "\x1b\u202e\u200b") {
			t.Errorf("linha %d com controle: %q", i, l)
		}
		if strings.Contains(l, "…") {
			t.Errorf("linha %d com reticências: %q", i, l)
		}
		if l != strings.TrimRight(l, " ") {
			t.Errorf("linha %d termina em espaço: %q", i, l)
		}
	}
	return lines
}

func mustContain(t *testing.T, s string, parts ...string) {
	t.Helper()
	flat := strings.Join(strings.Fields(s), " ") // o painel quebra linhas: confere o texto corrido
	for _, p := range parts {
		if !strings.Contains(flat, p) {
			t.Errorf("falta %q em:\n%s", p, s)
		}
	}
}

func mustNotContain(t *testing.T, s string, parts ...string) {
	t.Helper()
	for _, p := range parts {
		if strings.Contains(s, p) {
			t.Errorf("não devia ter %q em:\n%s", p, s)
		}
	}
}

// --- Apps ---

func detailApps(t *testing.T) *AppsTab {
	t.Helper()
	be := &fakeBackend{
		apps: []AppInfo{
			{ID: "git", Name: "Git", Category: "dev", Managers: []string{"apt", "flatpak"}},
			{ID: "python", Name: "Python", Category: "dev", Managers: []string{"apt"}, Versions: true},
			{ID: "discord", Name: "Discord", Category: "chat", Managers: []string{"brew"}},
			{ID: "vlc", Name: "VLC", Category: "midia", NoInstaller: true},
			{ID: "evil", Name: "Evil \u202eapp\x1b[31m " + strings.Repeat("nome-muito-longo-", 8), Category: "dev", Managers: []string{"apt"}},
		},
		status: map[string]AppStatus{
			"git":     {Installed: true, Version: "1:2.43.0-1"},
			"python":  {},
			"discord": {Installed: true, External: true, ExternalManager: "Homebrew", ExternalWhere: "/Applications/Discord.app"},
			"evil":    {Err: errors.New("brew saiu com 1\nsegunda linha")},
		},
	}
	return loaded(t, be)
}

// moveTo leva o cursor da aba ao app com o id dado (a lista é agrupada por categoria).
func moveTo(t *testing.T, tab *AppsTab, id string) {
	t.Helper()
	for i := 0; i < 20; i++ {
		if a, ok := tab.current(); ok && a.ID == id {
			return
		}
		send(tab, "down")
	}
	t.Fatalf("app %q não encontrado", id)
}

func TestDetalheAppsInstaladoComGerenciadores(t *testing.T) {
	tab := detailApps(t)
	moveTo(t, tab, "git")
	out := tab.Detail(44, 40)
	lines := detailLines(t, out, 44)
	mustContain(t, out, "Git", "git", "dev", "✓ instalado", "1:2.43.0-1", "apt", "flatpak", "sudo")
	if !strings.HasPrefix(lines[0], " Git") {
		t.Errorf("primeira linha deve ser o nome: %q", lines[0])
	}
}

func TestDetalheAppsSegueOCursorEMostraVersoes(t *testing.T) {
	tab := detailApps(t)
	moveTo(t, tab, "python")
	out := tab.Detail(44, 40)
	detailLines(t, out, 44)
	mustContain(t, out, "Python", "○ não instalado", "versão fixa", "tecla v")
	mustNotContain(t, out, "Git")
}

func TestDetalheAppsExterno(t *testing.T) {
	tab := detailApps(t)
	moveTo(t, tab, "discord")
	out := tab.Detail(44, 40)
	detailLines(t, out, 44)
	mustContain(t, out, "Discord", "instalado (fora do Homebrew)", "/Applications/Discord.app", "brew")
}

func TestDetalheAppsSemInstalador(t *testing.T) {
	tab := detailApps(t)
	moveTo(t, tab, "vlc")
	out := tab.Detail(44, 40)
	detailLines(t, out, 44)
	mustContain(t, out, "VLC", "sem instalador", "Linux", "não pode ser marcado")
}

func TestDetalheAppsTextoDeForaLimpoEQuebrado(t *testing.T) {
	tab := detailApps(t)
	moveTo(t, tab, "evil")
	out := tab.Detail(44, 60)
	detailLines(t, out, 44)
	mustContain(t, out, "não deu para verificar", "brew saiu com 1")
	mustNotContain(t, out, "\x1b", "\u202e")
}

func TestDetalheAppsMarcadoComVersao(t *testing.T) {
	tab := detailApps(t)
	moveTo(t, tab, "python")
	send(tab, "space") // python tem lista: o espaço abre a lista de versões
	tab.Update(versionsLoadedMsg{id: "python", versions: []string{"3.12", "3.11"}})
	send(tab, "down", "enter")
	out := tab.Detail(44, 40)
	mustContain(t, out, "marcado para a Revisão", "3.12")
}

func TestDetalheAppsSemPainelNosEstadosEspeciais(t *testing.T) {
	tab := newApps(&fakeBackend{apps: sampleApps()})
	if tab.Detail(44, 30) != "" {
		t.Error("carregando: sem painel")
	}
	tab = newApps(&fakeBackend{loadErr: errors.New("x")})
	tab.Update(tab.loadCmd()())
	if tab.Detail(44, 30) != "" {
		t.Error("erro: sem painel")
	}
	tab = newApps(&fakeBackend{})
	tab.Update(tab.loadCmd()())
	if tab.Detail(44, 30) != "" {
		t.Error("vazio: sem painel")
	}
	tab = detailApps(t)
	moveTo(t, tab, "python")
	send(tab, "v")
	if tab.mode != modeVersions {
		t.Fatal("v devia abrir a lista de versões do Python")
	}
	if tab.Detail(44, 30) != "" {
		t.Error("lista de versões aberta: sem painel")
	}
	send(tab, "esc", "/", "zzzz")
	if tab.Detail(44, 30) != "" {
		t.Error("busca sem resultado: sem painel")
	}
}

// --- Receitas e Diagnóstico ---

func TestDetalheReceitas(t *testing.T) {
	tab := loadedRecipes(t, recipeSample(), false)
	out := tab.Detail(44, 40)
	detailLines(t, out, 44)
	mustContain(t, out, "Ambiente WSL para desenvolvimento", "wsl-dev", "Instala o WSL e as ferramentas.",
		"aguardando reinício", "exige administrador", "Habilitar o WSL", "Instalar a distro", "Instalar ferramentas", "reinicia")
	// etapas em ordem
	i1, i2, i3 := strings.Index(out, "Habilitar o WSL"), strings.Index(out, "Instalar a distro"), strings.Index(out, "Instalar ferramentas")
	if i1 >= i2 || i2 >= i3 {
		t.Errorf("etapas fora de ordem:\n%s", out)
	}
	rsend(tab, "down")
	out = tab.Detail(44, 40)
	mustContain(t, out, "Ambiente Linux", "falhou", "não exige administrador", "Pacotes", "apt-get saiu com 100")
	mustNotContain(t, out, "Habilitar o WSL")
}

func TestDetalheReceitasEtapaEmFoco(t *testing.T) {
	tab := loadedRecipes(t, recipeSample(), false)
	rsend(tab, "right", "down")
	out := tab.Detail(44, 40)
	if !strings.Contains(out, "> 2 Instalar a distro") {
		t.Errorf("a etapa em foco leva '>':\n%s", out)
	}
}

func TestDetalheDiagnosticoDelegaESomeNaVisaoMaquina(t *testing.T) {
	d := newDiag(t, &fakeMachine{})
	out := d.Detail(44, 40)
	detailLines(t, out, 44)
	mustContain(t, out, "Diagnóstico de memória", "diag-memoria", "Informações de RAM", "Agendar o diagnóstico", "exige administrador")
	d.machine = true
	if d.Detail(44, 40) != "" {
		t.Error("visão Máquina: sem painel")
	}
}

func TestDetalheReceitasQuebraTextoLongoELimpa(t *testing.T) {
	data := RecipesData{Recipes: []RecipeInfo{{ID: "x", Name: "Receita \u202eruim", Description: "descrição \x1b[31mlonga " + strings.Repeat("palavra ", 30),
		Steps: []StepInfo{{ID: "a", Name: "Etapa " + strings.Repeat("comprida ", 10), Reboot: "if-needed"}}}}}
	tab := loadedRecipes(t, data, false)
	out := tab.Detail(44, 60)
	detailLines(t, out, 44)
	mustContain(t, out, "pode reiniciar")
}

func TestDetalheReceitasSemPainelNosEstados(t *testing.T) {
	tab := NewRecipesTab(context.Background(), &fakeRecipes{data: recipeSample()}, Theme{Mono: true}, false)
	if tab.Detail(44, 30) != "" {
		t.Error("carregando: sem painel")
	}
	tab = loadedRecipes(t, RecipesData{}, false)
	if tab.Detail(44, 30) != "" {
		t.Error("vazio: sem painel")
	}
	tab = NewRecipesTab(context.Background(), &fakeRecipes{err: errors.New("x")}, Theme{Mono: true}, false)
	tab.Update(recipesLoadedMsg{err: errors.New("x")})
	if tab.Detail(44, 30) != "" {
		t.Error("erro: sem painel")
	}
}

// --- Apps nativos ---

func TestDetalheAppsNativos(t *testing.T) {
	tab := natTab(t, &fakeNat{info: natSample(false)})
	out := tab.Detail(44, 40)
	detailLines(t, out, 44)
	mustContain(t, out, "MSN Clima", "bing-weather", "básico", "✓ instalado", "tecla i", "nada é executado")
	nkey(tab, "down")
	out = tab.Detail(44, 40)
	mustContain(t, out, "Skype", "○ ausente")
	nkey(tab, "down")
	out = tab.Detail(44, 40)
	mustContain(t, out, "Vínculo com o Celular", "completo", "phone-link")
	mustNotContain(t, out, "Skype")
}

func TestDetalheAppsNativosEscopo(t *testing.T) {
	tab := natTab(t, &fakeNat{info: natSample(true)})
	nkey(tab, "m")
	out := tab.Detail(44, 40)
	mustContain(t, out, "todos os usuários", "administrador")
}

func TestDetalheAppsNativosSemPainelForaDaLista(t *testing.T) {
	tab := NewNativeAppsTab(context.Background(), &fakeNat{info: natSample(false)}, Theme{Mono: true, NoMotion: true})
	if tab.Detail(44, 30) != "" {
		t.Error("carregando: sem painel")
	}
	tab = natTab(t, &fakeNat{info: NatInfo{Supported: false, OS: "darwin"}})
	if tab.Detail(44, 30) != "" {
		t.Error("não suportado: sem painel")
	}
	tab = natTab(t, &fakeNat{info: natSample(false), plan: NatPlan{ID: "p", Text: natPlanText, Pending: 1}})
	nkey(tab, "space", "r")
	if !tab.asking {
		t.Fatal("r devia abrir a confirmação")
	}
	if tab.Detail(44, 30) != "" {
		t.Error("confirmação: sem painel")
	}
	tab = natTab(t, &fakeNat{info: natSample(false), plan: NatPlan{ID: "p", Text: natPlanText, Pending: 1}})
	nkey(tab, "space", "p")
	if !tab.view {
		t.Fatal("p devia mostrar o plano")
	}
	if tab.Detail(44, 30) != "" {
		t.Error("plano na tela: sem painel")
	}
	tab = natTab(t, &fakeNat{loadErr: errors.New("x")})
	if tab.Detail(44, 30) != "" {
		t.Error("erro: sem painel")
	}
}

// --- Privacidade ---

func TestDetalhePrivacidadePerfis(t *testing.T) {
	tab := privTab(t, &fakePriv{info: privSample(false)})
	out := tab.Detail(44, 40)
	detailLines(t, out, 44)
	mustContain(t, out, "basico", "Não exige administrador", "1 aplicado", "1 pendente", "tecla d", "backup", "tecla p")
	pkey(tab, "down")
	out = tab.Detail(44, 40)
	detailLines(t, out, 44)
	mustContain(t, out, "recomendado", "Exige administrador", "este terminal não é", "2 pendentes", "1 parcial", "política", "tecla d")
	tab = privTab(t, &fakePriv{info: privSample(true)})
	pkey(tab, "down")
	out = tab.Detail(44, 40)
	mustContain(t, out, "este terminal é administrador")
}

func TestDetalhePrivacidadeSoUsuario(t *testing.T) {
	tab := privTab(t, &fakePriv{info: privSample(true)})
	pkey(tab, "m")
	out := tab.Detail(44, 40)
	mustContain(t, out, "só os itens do usuário")
}

func TestDetalhePrivacidadeSemPainelForaDaLista(t *testing.T) {
	tab := NewPrivacyTab(context.Background(), &fakePriv{info: privSample(false)}, Theme{Mono: true, NoMotion: true})
	if tab.Detail(44, 30) != "" {
		t.Error("carregando: sem painel")
	}
	tab = privTab(t, &fakePriv{info: PrivInfo{Supported: false, OS: "darwin"}})
	if tab.Detail(44, 30) != "" {
		t.Error("não suportado: sem painel")
	}
	tab = privTab(t, &fakePriv{info: privSample(true), plan: PrivPlan{Text: planText, Pending: 1}})
	pkey(tab, "a")
	if !tab.asking {
		t.Fatal("a devia abrir a confirmação")
	}
	if tab.Detail(44, 30) != "" {
		t.Error("confirmação: sem painel")
	}
	tab = privTab(t, &fakePriv{info: privSample(true), plan: PrivPlan{Text: planText, Pending: 1}})
	pkey(tab, "p")
	if tab.Detail(44, 30) != "" {
		t.Error("plano na tela: sem painel")
	}
}

// --- Início ---

func TestDetalheInicio(t *testing.T) {
	tab := newInicio(false, false)
	out := tab.Detail(44, 40)
	detailLines(t, out, 44)
	mustContain(t, out, "Instalar programas", "Escolha os programas do catálogo", "Enter abre 1 Instalar › Apps")
	tab.Update(key('j', "j"))
	tab.Update(key('j', "j"))
	tab.Update(key('j', "j"))
	out = tab.Detail(44, 40)
	mustContain(t, out, "Remover apps nativos do Windows", "! só Windows", "Enter abre 2 Máquina › Apps nativos")
	for i := 0; i < 2; i++ {
		tab.Update(key('j', "j"))
	}
	out = tab.Detail(44, 40)
	mustContain(t, out, "Analisar a rede", "○ em breve", "ainda não existe nesta versão")
	mustNotContain(t, out, "Enter abre")
}

func TestDetalheInicioSemItensNaoAbrePainel(t *testing.T) {
	tab := NewInicioTab(Theme{Mono: true}, "Início", "Iníc", "O que?", nil)
	if tab.Detail(44, 30) != "" {
		t.Error("sem itens: sem painel")
	}
}

// Todas as seções com painel declaram o contrato.
func TestDetalheContrato(_ *testing.T) {
	var _ Detailer = (*AppsTab)(nil)
	var _ Detailer = (*RecipesTab)(nil)
	var _ Detailer = (*DiagTab)(nil)
	var _ Detailer = (*NativeAppsTab)(nil)
	var _ Detailer = (*PrivacyTab)(nil)
	var _ Detailer = (*InicioTab)(nil)
}

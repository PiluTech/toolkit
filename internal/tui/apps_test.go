package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

type fakeBackend struct {
	mu       sync.Mutex
	apps     []AppInfo
	loadErr  error
	status   map[string]AppStatus
	versions map[string][]string
	verErr   error
	asked    []string
}

func (f *fakeBackend) Load(context.Context) ([]AppInfo, error) { return f.apps, f.loadErr }
func (f *fakeBackend) Status(_ context.Context, id string) AppStatus {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.asked = append(f.asked, id)
	return f.status[id]
}
func (f *fakeBackend) Versions(_ context.Context, id string) ([]string, error) {
	return f.versions[id], f.verErr
}

func sampleApps() []AppInfo {
	return []AppInfo{
		{ID: "git", Name: "Git", Category: "dev", Managers: []string{"apt"}},
		{ID: "python", Name: "Python", Category: "dev", Managers: []string{"apt"}, Versions: true},
		{ID: "firefox", Name: "Firefox", Category: "navegadores", Managers: []string{"flatpak"}},
		{ID: "vlc", Name: "VLC", Category: "midia", NoInstaller: true},
	}
}

func newApps(be *fakeBackend) *AppsTab {
	return NewAppsTab(context.Background(), be, Theme{Mono: true, Dark: true}, "linux")
}

// loaded devolve a aba já com o catálogo carregado e os estados resolvidos (sem passar pelo Bubble Tea).
func loaded(t *testing.T, be *fakeBackend) *AppsTab {
	t.Helper()
	tab := newApps(be)
	tab.lastH = 19
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
	return tab
}

func key(code rune, text string) tea.KeyPressMsg { return tea.KeyPressMsg{Code: code, Text: text} }

func send(tab *AppsTab, keys ...string) {
	for _, k := range keys {
		switch k {
		case "up":
			tab.Update(tea.KeyPressMsg{Code: tea.KeyUp})
		case "down":
			tab.Update(tea.KeyPressMsg{Code: tea.KeyDown})
		case "enter":
			tab.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
		case "esc":
			tab.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
		case "backspace":
			tab.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
		case "space":
			tab.Update(tea.KeyPressMsg{Code: ' ', Text: " "})
		default:
			tab.Update(key(rune(k[0]), k))
		}
	}
}

func plain(s string) string { return ansi.Strip(s) }

func defaultStatus() map[string]AppStatus {
	return map[string]AppStatus{
		"git":     {Installed: true, Version: "2.43.0"},
		"python":  {},
		"firefox": {Err: errors.New("flatpak: remote ausente")},
	}
}

func TestAppsLoadingThenGroupedList(t *testing.T) {
	be := &fakeBackend{apps: sampleApps(), status: defaultStatus()}
	tab := newApps(be)
	if v := plain(tab.View(80, 19)); !strings.Contains(v, "Lendo o catálogo…") {
		t.Fatalf("carregando: %q", v)
	}
	tab = loaded(t, be)
	v := plain(tab.View(80, 19))
	for _, want := range []string{"DEV", "MIDIA", "NAVEGADORES", "> [ ] Git", "✓ instalado", "2.43.0", "○ não instalado", "✗ não deu para verificar", "- sem instalador"} {
		if !strings.Contains(v, want) {
			t.Errorf("faltou %q:\n%s", want, v)
		}
	}
	// categorias em ordem e sem linha vazia entre elas
	if strings.Index(v, "DEV") > strings.Index(v, "MIDIA") || strings.Index(v, "MIDIA") > strings.Index(v, "NAVEGADORES") {
		t.Fatalf("ordem das categorias:\n%s", v)
	}
}

func TestAppsGolden80x24Mono(t *testing.T) {
	be := &fakeBackend{apps: sampleApps(), status: defaultStatus()}
	m := sized(Options{Version: "v0.1.0", Mono: true}, 80, 24, loaded(t, be))
	m.tabs = append(m.tabs, fakes()[1:]...)
	golden(t, "apps_80x24", m.render())
}

func TestAppsCursorSkipsHeadersAndStatusLine(t *testing.T) {
	be := &fakeBackend{apps: sampleApps(), status: defaultStatus()}
	tab := loaded(t, be)
	if s := tab.Status(); !strings.Contains(s, "git — Git (apt)") {
		t.Fatalf("status: %q", s)
	}
	send(tab, "down", "down", "down") // git -> python -> vlc -> firefox (pula os títulos MIDIA e NAVEGADORES)
	if a, _ := tab.current(); a.ID != "firefox" {
		t.Fatalf("cursor em %q", a.ID)
	}
	if s := tab.Status(); !strings.Contains(s, "não deu para verificar: flatpak: remote ausente") {
		t.Fatalf("o motivo do erro vai na linha de estado: %q", s)
	}
	send(tab, "G")
	if a, _ := tab.current(); a.ID != "firefox" {
		t.Fatalf("G: %q", a.ID)
	}
	send(tab, "g")
	if a, _ := tab.current(); a.ID != "git" {
		t.Fatalf("g: %q", a.ID)
	}
	send(tab, "up") // no topo não anda
	if a, _ := tab.current(); a.ID != "git" {
		t.Fatalf("up no topo: %q", a.ID)
	}
}

func TestAppsMarkUnmarkAndOrder(t *testing.T) {
	be := &fakeBackend{apps: sampleApps(), status: defaultStatus()}
	tab := loaded(t, be)
	send(tab, "space") // git
	send(tab, "G")     // firefox (a última categoria)
	send(tab, "space")
	m := tab.Marked()
	if len(m) != 2 || m[0].ID != "git" || m[1].ID != "firefox" || tab.Selected() != 2 {
		t.Fatalf("marcados: %+v", m)
	}
	if !strings.Contains(plain(tab.View(80, 19)), "[x] Git") {
		t.Fatal("a marca [x] aparece (texto, não só cor)")
	}
	send(tab, "space")
	if tab.Selected() != 1 {
		t.Fatal("espaço de novo desmarca")
	}
}

func TestAppsNoInstallerCannotBeMarked(t *testing.T) {
	be := &fakeBackend{apps: sampleApps(), status: defaultStatus()}
	tab := loaded(t, be)
	send(tab, "down", "down", "space") // git -> python -> vlc
	if tab.Selected() != 0 || !strings.Contains(tab.Status(), "sem instalador para Linux") {
		t.Fatalf("sel=%d status=%q", tab.Selected(), tab.Status())
	}
	if _, ok := tab.status["vlc"]; ok {
		t.Fatal("app sem instalador não é verificado")
	}
}

func TestAppsVersionsModal(t *testing.T) {
	be := &fakeBackend{apps: sampleApps(), status: defaultStatus(), versions: map[string][]string{"python": {"3.12", "3.11", "3.10"}}}
	tab := loaded(t, be)
	send(tab, "down") // python
	cmd := tab.toggleViaKey()
	if !tab.Capturing() || cmd == nil {
		t.Fatal("espaço em app com versões abre a lista")
	}
	// carregando: latest já é escolhível
	if v := plain(tab.View(80, 19)); !strings.Contains(v, "Consultando apt…") || !strings.Contains(v, "(•) latest") {
		t.Fatalf("carregando versões:\n%s", v)
	}
	tab.Update(cmdMsg(cmd, "versions"))
	v := plain(tab.View(80, 19))
	for _, want := range []string{"VERSÕES DE PYTHON", "(•) latest", "( ) 3.12", "( ) 3.11"} {
		if !strings.Contains(v, want) {
			t.Fatalf("faltou %q:\n%s", want, v)
		}
	}
	// Enter já em latest confirma: marcado sem versão fixa
	send(tab, "enter")
	if tab.Capturing() || tab.marks["python"] != "" || tab.Selected() != 1 {
		t.Fatalf("latest: %+v %v", tab.marks, tab.Capturing())
	}
	// v reabre; escolhe 3.11 (descendo 2) e confirma
	cmd = tab.openVersionsKey()
	tab.Update(cmdMsg(cmd, "versions"))
	send(tab, "down", "down", "enter")
	if tab.marks["python"] != "3.11" {
		t.Fatalf("versão escolhida: %q", tab.marks["python"])
	}
	if row := plain(tab.View(80, 19)); !strings.Contains(row, "→ 3.11") {
		t.Fatalf("a versão escolhida aparece na linha:\n%s", row)
	}
	if mk := tab.Marked(); len(mk) != 1 || mk[0].Version != "3.11" {
		t.Fatalf("Marked: %+v", mk)
	}
	// Esc cancela sem alterar
	cmd = tab.openVersionsKey()
	tab.Update(cmdMsg(cmd, "versions"))
	send(tab, "down", "esc")
	if tab.marks["python"] != "3.11" || tab.Capturing() {
		t.Fatal("Esc cancela")
	}
	// espaço em app marcado desmarca (não reabre a lista)
	send(tab, "space")
	if tab.Selected() != 0 || tab.Capturing() {
		t.Fatal("espaço em marcado desmarca")
	}
}

// toggleViaKey / openVersionsKey rodam as teclas de verdade e devolvem o comando que a aba pediu.
func (t *AppsTab) toggleViaKey() tea.Cmd {
	return t.keyList(tea.KeyPressMsg{Code: ' ', Text: " "})
}
func (t *AppsTab) openVersionsKey() tea.Cmd { return t.keyList(key('v', "v")) }

// cmdMsg executa o comando (ou o lote) e devolve a primeira mensagem do tipo pedido.
func cmdMsg(cmd tea.Cmd, kind string) tea.Msg {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	if b, ok := msg.(tea.BatchMsg); ok {
		for _, c := range b {
			if c == nil {
				continue
			}
			if m := c(); kind == "versions" {
				if _, ok := m.(versionsLoadedMsg); ok {
					return m
				}
			}
		}
		return nil
	}
	return msg
}

func TestAppsVersionsEmptyAndError(t *testing.T) {
	be := &fakeBackend{apps: sampleApps(), status: defaultStatus(), versions: map[string][]string{"python": nil}}
	tab := loaded(t, be)
	send(tab, "down")
	tab.Update(cmdMsg(tab.toggleViaKey(), "versions"))
	if v := plain(tab.View(80, 19)); !strings.Contains(v, "O gerenciador não informou outras versões.") || !strings.Contains(v, "(•) latest") {
		t.Fatalf("vazio:\n%s", v)
	}
	send(tab, "esc")
	be.verErr = errors.New("apt-cache: sem rede\x1b[2J")
	tab.Update(cmdMsg(tab.toggleViaKey(), "versions"))
	v := plain(tab.View(80, 19))
	if !strings.Contains(v, "não deu para listar as versões") || !strings.Contains(v, "latest continua valendo") || strings.Contains(v, "\x1b") {
		t.Fatalf("erro:\n%q", v)
	}
	send(tab, "enter") // latest continua valendo
	if _, ok := tab.marks["python"]; !ok {
		t.Fatal("latest continua marcável mesmo com erro")
	}
}

func TestAppsVersionsLongListScrollsAndDedupsLatest(t *testing.T) {
	var vs []string
	vs = append(vs, "latest")
	for i := 0; i < 350; i++ {
		vs = append(vs, fmt.Sprintf("1.%d.0", i))
	}
	be := &fakeBackend{apps: sampleApps(), status: defaultStatus(), versions: map[string][]string{"python": vs}}
	tab := loaded(t, be)
	send(tab, "down")
	tab.Update(cmdMsg(tab.toggleViaKey(), "versions"))
	if n := len(tab.ver.versions); n != 351 { // latest (uma só) + 350
		t.Fatalf("%d versões", n)
	}
	send(tab, "G")
	v := plain(tab.View(80, 19))
	if !strings.Contains(v, "1.349.0") || strings.Count(v, "\n") > 19 {
		t.Fatalf("o fim da lista aparece e cabe no corpo:\n%s", v)
	}
	send(tab, "g")
	if v := plain(tab.View(80, 19)); !strings.Contains(v, "↓ mais") || !strings.Contains(v, "(•) latest") {
		t.Fatalf("topo:\n%s", v)
	}
}

func TestAppsSearchIsText(t *testing.T) {
	be := &fakeBackend{apps: sampleApps(), status: defaultStatus()}
	tab := loaded(t, be)
	send(tab, "/")
	if !tab.Capturing() {
		t.Fatal("a busca captura as teclas")
	}
	send(tab, "f", "i", "r")
	v := plain(tab.View(80, 19))
	if !strings.Contains(v, "/ fir▌") || !strings.Contains(v, "1 de 4 apps") || !strings.Contains(v, "Firefox") || strings.Contains(v, "Git") {
		t.Fatalf("filtro:\n%s", v)
	}
	// q, j, 1, ? e Tab são texto, não comandos
	send(tab, "q", "j", "1")
	if tab.filter != "firqj1" {
		t.Fatalf("filtro = %q", tab.filter)
	}
	if v := plain(tab.View(80, 19)); !strings.Contains(v, `Nada com "firqj1". Esc limpa a busca.`) {
		t.Fatalf("sem resultado:\n%s", v)
	}
	send(tab, "backspace", "backspace", "backspace")
	if tab.filter != "fir" {
		t.Fatalf("backspace: %q", tab.filter)
	}
	send(tab, "enter") // fixa o filtro e sai da digitação
	if tab.Capturing() || tab.filter != "fir" {
		t.Fatal("Enter fixa o filtro")
	}
	send(tab, "esc") // limpa
	if tab.filter != "" || tab.matches() != 4 {
		t.Fatalf("Esc limpa: %q %d", tab.filter, tab.matches())
	}
	send(tab, "/", "z", "esc")
	if tab.Capturing() || tab.filter != "" {
		t.Fatal("Esc na digitação limpa e sai")
	}
}

// Pela raiz: digitar q, ?, Tab ou número na busca não sai, não abre ajuda e não troca de aba; Ctrl+C sai.
func TestAppsSearchThroughRootDoesNotTriggerGlobalKeys(t *testing.T) {
	be := &fakeBackend{apps: sampleApps(), status: defaultStatus()}
	tab := loaded(t, be)
	m := sized(Options{Mono: true}, 80, 24, tab)
	m.tabs = append(m.tabs, fakes()[1:]...)
	press(m, '/', 0, "/")
	for _, r := range "q?2" {
		press(m, r, 0, string(r))
	}
	press(m, tea.KeyTab, 0, "")
	if m.Quitting() || m.Active() != 0 || m.modal != modalNone || tab.filter != "q?2" {
		t.Fatalf("quit=%v ativa=%d modal=%v filtro=%q", m.Quitting(), m.Active(), m.modal, tab.filter)
	}
	if cmd := press(m, 'c', tea.ModCtrl, ""); cmd == nil || !m.Quitting() {
		t.Fatal("Ctrl+C sai mesmo digitando")
	}
}

func TestAppsStatusWorkersAreLimitedAndVisibleFirst(t *testing.T) {
	var apps []AppInfo
	for i := 0; i < 60; i++ {
		apps = append(apps, AppInfo{ID: fmt.Sprintf("app%02d", i), Name: fmt.Sprintf("App %02d", i), Category: "dev", Managers: []string{"apt"}})
	}
	be := &fakeBackend{apps: apps, status: map[string]AppStatus{}}
	tab := newApps(be)
	tab.lastH = 10
	_, cmd := tab.Update(tab.loadCmd()())
	if cmd == nil {
		t.Fatal("deve pedir os primeiros estados")
	}
	if len(tab.pending) != maxStatusWorkers {
		t.Fatalf("pendentes: %d, máximo %d", len(tab.pending), maxStatusWorkers)
	}
	// rola até o fim: o que ficou visível passa à frente da fila
	send(tab, "G")
	if len(tab.queue) == 0 || tab.queue[0] < "app40" {
		t.Fatalf("o fim da lista (visível) deve ir à frente da fila: %v", tab.queue[:3])
	}
	// cada resposta libera o próximo, sem passar do limite
	for _, id := range sortedKeys(tab.pending) {
		_, c := tab.Update(appStateMsg{id: id, st: AppStatus{}})
		if c == nil {
			t.Fatal("deve puxar o próximo da fila")
		}
		if len(tab.pending) > maxStatusWorkers {
			t.Fatalf("passou do limite: %d", len(tab.pending))
		}
	}
}

func sortedKeys(m map[string]bool) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}

func TestAppsScrollMoreAndPaging(t *testing.T) {
	var apps []AppInfo
	for i := 0; i < 100; i++ {
		apps = append(apps, AppInfo{ID: fmt.Sprintf("a%03d", i), Name: fmt.Sprintf("App %03d", i), Category: "dev"})
	}
	tab := NewAppsTab(context.Background(), &fakeBackend{apps: apps, status: map[string]AppStatus{}}, Theme{Mono: true}, "linux")
	tab.Update(tab.loadCmd()())
	tab.View(80, 15)
	first := tab.More()
	if first <= 0 {
		t.Fatalf("More = %d", first)
	}
	send(tab, "G")
	tab.View(80, 15)
	if tab.More() != 0 {
		t.Fatalf("no fim More deve ser 0: %d", tab.More())
	}
	if v := plain(tab.View(80, 15)); !strings.Contains(v, "App 099") || strings.Count(v, "\n")+1 > 15 {
		t.Fatalf("o fim da lista cabe no corpo:\n%s", v)
	}
	send(tab, "g")
	tab.Update(tea.KeyPressMsg{Code: tea.KeyPgDown})
	if a, _ := tab.current(); a.ID == "a000" || a.ID == "a001" {
		t.Fatalf("PgDn deve andar uma página: %q", a.ID)
	}
	v := plain(tab.View(80, 15))
	if !strings.Contains(v, "> ") {
		t.Fatalf("o cursor segue visível:\n%s", v)
	}
}

func TestAppsColumnsByWidth(t *testing.T) {
	be := &fakeBackend{apps: sampleApps(), status: defaultStatus()}
	for _, w := range []int{60, 65, 70, 79, 80, 120} {
		tab := loaded(t, be)
		send(tab, "down", "space")
		tab.Update(cmdMsg(tab.openVersionsKey(), "versions"))
		send(tab, "enter")
		v := tab.View(w, 19)
		for i, l := range strings.Split(v, "\n") {
			if lipgloss.Width(l) > w {
				t.Errorf("w=%d linha %d com %d colunas: %q", w, i, lipgloss.Width(l), plain(l))
			}
		}
		pv := plain(v)
		if w < 80 && strings.Contains(pv, "apt ") && strings.Contains(pv, "  apt") {
			t.Errorf("w=%d: sem a coluna VIA abaixo de 80:\n%s", w, pv)
		}
		if w >= 80 && !strings.Contains(pv, "flatpak") {
			t.Errorf("w=%d: com a coluna VIA:\n%s", w, pv)
		}
		if !strings.Contains(pv, "✓") {
			t.Errorf("w=%d: o estado sempre tem símbolo:\n%s", w, pv)
		}
	}
	// 60 a 69: a versão instalada vem junto do estado
	tab := loaded(t, be)
	if v := plain(tab.View(65, 19)); !strings.Contains(v, "✓ instalado 2.43.0") {
		t.Fatalf("65 colunas:\n%s", v)
	}
}

func TestAppsLongNamesAndVersionsNeverBreakTheLayout(t *testing.T) {
	apps := []AppInfo{{ID: "x", Name: strings.Repeat("Nome muito comprido ", 6), Category: "dev", Managers: []string{"apt"}}}
	be := &fakeBackend{apps: apps, status: map[string]AppStatus{"x": {Installed: true, Version: "1:2.43.0-1ubuntu7.3+esm2~24.04.1"}}}
	tab := loaded(t, be)
	for _, w := range []int{60, 80, 100} {
		for _, l := range strings.Split(tab.View(w, 19), "\n") {
			if lipgloss.Width(l) > w {
				t.Fatalf("w=%d: %q", w, plain(l))
			}
		}
	}
	if v := plain(tab.View(80, 19)); !strings.Contains(v, "…") {
		t.Fatalf("o nome longo é cortado com reticências:\n%s", v)
	}
	if !strings.Contains(tab.Status(), "Nome muito comprido Nome muito comprido") {
		t.Fatal("o nome inteiro aparece na linha de estado")
	}
}

func TestAppsEmptyAndErrorStates(t *testing.T) {
	be := &fakeBackend{}
	tab := newApps(be)
	tab.Update(tab.loadCmd()())
	if v := plain(tab.View(80, 19)); !strings.Contains(v, "Nenhum app do catálogo tem instalador para Linux.") || !strings.Contains(v, "toolkit list --os") {
		t.Fatalf("vazio:\n%s", v)
	}
	be2 := &fakeBackend{loadErr: errors.New("cat.yaml:3:5: app \"x\": categoria inválida\ncat.yaml:9:1: chave repetida")}
	tab = newApps(be2)
	tab.Update(tab.loadCmd()())
	v := plain(tab.View(80, 19))
	for _, want := range []string{"erro:", "cat.yaml:3:5", "cat.yaml:9:1", "rode sem --catalog"} {
		if !strings.Contains(v, want) {
			t.Fatalf("faltou %q:\n%s", want, v)
		}
	}
	if tab.More() != 0 {
		t.Fatal("erro não rola")
	}
}

// Texto de fora (nomes do catálogo, versões, erros do gerenciador) nunca vira sequência de terminal.
func TestAppsOutsideTextIsSanitized(t *testing.T) {
	apps := []AppInfo{{ID: "ev\x1b]0;pwn\x07", Name: "Ev\x1b[2Jil\u202e", Category: "dev\x1b[31m", Managers: []string{"ap\x1bt"}, Versions: true}}
	be := &fakeBackend{apps: apps, versions: map[string][]string{"ev\x1b]0;pwn\x07": {"1\x1b[2J.0", "2\n.0"}},
		status: map[string]AppStatus{"ev\x1b]0;pwn\x07": {Installed: true, Version: "9\x1b[31m", Err: nil}}}
	tab := NewAppsTab(context.Background(), be, Theme{Mono: true}, "linux")
	tab.Update(tab.loadCmd()())
	for _, id := range sortedKeys(tab.pending) {
		tab.Update(appStateMsg{id: id, st: be.status[tab.apps[0].ID]})
	}
	send(tab, "space")
	tab.Update(cmdMsg(tab.openVersionsKey(), "versions"))
	check := func(s string) {
		t.Helper()
		if strings.ContainsAny(s, "\x1b\x07\u202e") {
			t.Fatalf("sequência de controle vazou: %q", s)
		}
	}
	check(tab.View(80, 19))
	check(tab.Status())
	send(tab, "esc")
	tab.Update(appStateMsg{id: tab.apps[0].ID, st: AppStatus{Err: errors.New("falhou\x1b[2J")}})
	check(tab.View(80, 19))
	check(tab.Status())
}

func TestAppsSpinnerRunsOnlyWhileBusy(t *testing.T) {
	be := &fakeBackend{apps: sampleApps(), status: defaultStatus()}
	tab := newApps(be)
	if _, cmd := tab.Update(tickMsg{}); cmd == nil {
		t.Fatal("o spinner continua enquanto carrega")
	}
	tab = loaded(t, be)
	if _, cmd := tab.Update(tickMsg{}); cmd != nil {
		t.Fatal("sem nada carregando o tick para (nenhum redesenho à toa)")
	}
}

func TestAppsKeysAndStatusByMode(t *testing.T) {
	be := &fakeBackend{apps: sampleApps(), status: defaultStatus()}
	tab := loaded(t, be)
	if k := tab.Keys(); k[1].Key != "espaço" {
		t.Fatalf("lista: %v", k)
	}
	send(tab, "/")
	if k := tab.Keys(); k[0].Key != "Enter" || k[1].Key != "Esc" {
		t.Fatalf("busca: %v", k)
	}
	send(tab, "esc", "down")
	tab.Update(cmdMsg(tab.toggleViaKey(), "versions"))
	if k := tab.Keys(); k[1].Label != "escolher" {
		t.Fatalf("versões: %v", k)
	}
	if !strings.Contains(tab.Status(), "versões de Python") {
		t.Fatalf("status: %q", tab.Status())
	}
}

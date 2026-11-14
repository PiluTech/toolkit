package tui

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
)

var (
	stMono   = Theme{Mono: true, NoMotion: true}
	stMotion = Theme{Mono: true}
	stColor  = Theme{Dark: true}
)

type stateCase struct {
	name string
	fn   func(th Theme, w, h int) []string
}

func stateCases() []stateCase {
	long := "O catálogo local tem um problema muito longo: " + strings.Repeat("caminho/muito/longo/", 8) + "arquivo.yaml:12:3"
	return []stateCase{
		{"carregando", func(th Theme, w, h int) []string { return StateLoading(th, w, h, "o catálogo", 2, "Esc cancela.") }},
		{"erro", func(th Theme, w, h int) []string {
			return StateError(th, w, h, "o catálogo", errors.New(long+"\nsegunda linha"), "Tecle c para tentar de novo.", "Veja o log.")
		}},
		{"admin", func(th Theme, w, h int) []string {
			return StateNeedsAdmin(th, w, h, "a privacidade", AdminSteps("windows", "")...)
		}},
		{"nao-suportado", func(th Theme, w, h int) []string {
			return StateUnsupported(th, w, h, "a privacidade", "Os ajustes são do registro do Windows.", "macOS")
		}},
		{"vazio", func(th Theme, w, h int) []string {
			return StateEmpty(th, w, h, "Nenhuma receita para este sistema.", "Veja o que existe: toolkit recipe list")
		}},
		{"em-breve", func(th Theme, w, h int) []string {
			return StateSoon(th, w, h, "A análise de rede ainda não existe nesta versão.")
		}},
		{"terminal-pequeno", func(th Theme, w, h int) []string { return StateTooSmall(th, w, h, 60, 16) }},
	}
}

func TestStateFormaFixaTamanhoExato(t *testing.T) {
	for _, c := range stateCases() {
		for _, th := range []Theme{stMono, stMotion, stColor} {
			for _, w := range []int{1, 5, 40, 60, 80, 120} {
				for _, h := range []int{0, 1, 2, 3, 5, 12, 24} {
					got := c.fn(th, w, h)
					if len(got) != h {
						t.Fatalf("%s w=%d h=%d: %d linhas, quer exatamente %d", c.name, w, h, len(got), h)
					}
					for _, l := range got {
						if lipgloss.Width(l) > w {
							t.Fatalf("%s w=%d h=%d: linha com %d colunas: %q", c.name, w, h, lipgloss.Width(l), l)
						}
						if strings.Contains(l, "\n") {
							t.Fatalf("%s: linha com quebra embutida: %q", c.name, l)
						}
					}
				}
			}
		}
	}
}

func TestStateTitulosESimbolosNaFrente(t *testing.T) {
	want := map[string]string{
		"carregando": "… Carregando", "erro": "✗ Erro", "admin": "! Precisa de administrador",
		"nao-suportado": "i Não disponível neste sistema", "vazio": "i Nada por aqui",
		"em-breve": "i Ainda não existe nesta versão", "terminal-pequeno": "i Terminal pequeno",
	}
	for _, c := range stateCases() {
		got := c.fn(stMono, 80, 24)
		if got[0] != " "+want[c.name] {
			t.Errorf("%s: título %q, quer %q", c.name, got[0], " "+want[c.name])
		}
	}
}

func TestStateNothingChangedSoEmErroERecusa(t *testing.T) {
	for _, c := range stateCases() {
		flat := strings.Join(c.fn(stMono, 80, 24), "\n")
		has := strings.Contains(flat, "Nada foi alterado.")
		should := c.name == "erro" || c.name == "admin" || c.name == "nao-suportado"
		if has != should {
			t.Errorf("%s: 'Nada foi alterado.'=%v, quer %v\n%s", c.name, has, should, flat)
		}
	}
}

func TestStateErroTemFraseMotivoPassosNaOrdem(t *testing.T) {
	got := strings.Join(StateError(stMono, 80, 24, "as receitas", errors.New("disco cheio"), "Tecle c para tentar de novo."), "\n")
	order := []string{"✗ Erro", "Não deu para ler as receitas.", "Motivo: disco cheio", "1. Tecle c para tentar de novo.", "Nada foi alterado."}
	pos := -1
	for _, s := range order {
		i := strings.Index(got, s)
		if i <= pos {
			t.Fatalf("%q fora de ordem ou ausente:\n%s", s, got)
		}
		pos = i
	}
}

func TestStateErroSemErroSemPassos(t *testing.T) {
	got := strings.Join(StateError(stMono, 60, 10, "", nil), "\n")
	if !strings.Contains(got, "Algo deu errado.") || strings.Contains(got, "Motivo") || !strings.Contains(got, "Nada foi alterado.") {
		t.Fatalf("erro mínimo:\n%s", got)
	}
}

func TestStateCarregandoMovimentoESemMovimento(t *testing.T) {
	a := StateLoading(stMotion, 80, 6, "o catálogo", 0)[0]
	b := StateLoading(stMotion, 80, 6, "o catálogo", 1)[0]
	if a == b {
		t.Fatalf("com movimento o quadro muda: %q = %q", a, b)
	}
	for _, f := range []int{0, 1, 2, 3, 7, -1} {
		l := StateLoading(stMotion, 80, 6, "o catálogo", f)[0]
		if !strings.HasSuffix(l, " Carregando") {
			t.Errorf("quadro %d sem a palavra ao lado: %q", f, l)
		}
	}
	for _, f := range []int{0, 1, 2, 3} {
		got := StateLoading(stMono, 80, 6, "o catálogo", f)
		if got[0] != " … Carregando" || got[1] != " Lendo o catálogo…" {
			t.Errorf("sem movimento, quadro %d: %q", f, got[:2])
		}
	}
}

func TestStateCarregandoAceitaEscCancela(t *testing.T) {
	got := strings.Join(StateLoading(stMono, 80, 6, "a máquina", 0, "Esc cancela."), "\n")
	if !strings.Contains(got, " Esc cancela.") {
		t.Fatalf("dica ausente:\n%s", got)
	}
	if strings.Contains(strings.Join(StateLoading(stMono, 80, 6, "a máquina", 0), "\n"), "Esc") {
		t.Fatal("sem dica, sem Esc")
	}
}

func TestStateMonoNaoEmiteEstilo(t *testing.T) {
	for _, c := range stateCases() {
		if flat := strings.Join(c.fn(stMono, 80, 24), "\n"); strings.Contains(flat, "\x1b") {
			t.Errorf("%s: Mono emitiu sequência de estilo: %q", c.name, flat)
		}
		if flat := strings.Join(c.fn(stColor, 80, 24), "\n"); c.name != "carregando" && c.name != "terminal-pequeno" && !strings.Contains(flat, "\x1b") && c.name != "vazio" && c.name != "nao-suportado" && c.name != "em-breve" {
			t.Errorf("%s: o tema colorido deveria pintar o símbolo", c.name)
		}
	}
}

func TestStateQuebraSemReticencias(t *testing.T) {
	for _, w := range []int{40, 60, 80, 120} {
		for _, c := range stateCases() {
			flat := strings.Join(c.fn(stMono, w, 40), "\n")
			if strings.Contains(flat, "…") && c.name != "carregando" {
				t.Errorf("%s w=%d: reticências no texto: %q", c.name, w, flat)
			}
		}
		// o texto inteiro aparece (quebrado): tirando os espaços, o erro longo está lá
		got := strings.Join(StateError(stMono, w, 40, "o catálogo", errors.New(strings.Repeat("abcdef ", 30))), "\n")
		if strings.Count(strings.ReplaceAll(got, " ", ""), "abcdef") != 30 {
			t.Errorf("w=%d: texto do erro cortado:\n%s", w, got)
		}
	}
}

func TestStateTextsafeEmTudoQueVemDeFora(t *testing.T) {
	evil := "x\x1b]0;preso\x07y\u202eoops\r\nz"
	cases := [][]string{
		StateLoading(stMono, 80, 12, evil, 0, evil),
		StateError(stMono, 80, 12, evil, errors.New(evil), evil),
		StateNeedsAdmin(stMono, 80, 12, evil, evil),
		StateUnsupported(stMono, 80, 12, evil, evil, evil),
		StateEmpty(stMono, 80, 12, evil, evil),
		StateSoon(stMono, 80, 12, evil, evil),
	}
	for i, got := range cases {
		flat := strings.Join(got, "\n")
		for _, bad := range []string{"\x1b", "\x07", "\u202e", "\r"} {
			if strings.Contains(flat, bad) {
				t.Errorf("caso %d: %q chegou à tela:\n%s", i, bad, flat)
			}
		}
	}
	if flat := strings.Join(StateError(stMono, 80, 12, "x", errors.New(evil)), "\n"); !strings.Contains(flat, `\x1b`) || !strings.Contains(flat, `\u202e`) {
		t.Errorf("o texto perigoso deve sair escapado, não sumir:\n%s", flat)
	}
}

func TestStateAlturaPequenaDegradaPorPrioridade(t *testing.T) {
	steps := []string{"Primeiro passo.", "Segundo passo."}
	e := func(h int) string {
		return strings.Join(StateError(stMono, 80, h, "o catálogo", errors.New("falhou"), steps...), "\n")
	}
	// h=1: só o título
	if got := e(1); !strings.Contains(got, "✗ Erro") || strings.Contains(got, "Não deu") {
		t.Errorf("h=1:\n%s", got)
	}
	// h=2: título e frase
	if got := e(2); !strings.Contains(got, "Não deu para ler o catálogo.") || strings.Contains(got, "1.") {
		t.Errorf("h=2:\n%s", got)
	}
	// h=3: o motivo (1ª linha) vem antes da cauda e dos passos
	if got := e(3); !strings.Contains(got, "Motivo: falhou") || strings.Contains(got, "1.") {
		t.Errorf("h=3:\n%s", got)
	}
	// h=4: 'Nada foi alterado.' antes dos passos
	if got := e(4); !strings.Contains(got, "Nada foi alterado.") || strings.Contains(got, "1.") {
		t.Errorf("h=4:\n%s", got)
	}
	// h=5: primeiro passo; h=7: todos
	if got := e(5); !strings.Contains(got, "1. Primeiro passo.") || strings.Contains(got, "2.") {
		t.Errorf("h=5:\n%s", got)
	}
	if got := e(8); !strings.Contains(got, "2. Segundo passo.") {
		t.Errorf("h=8:\n%s", got)
	}
}

func TestStateErroLongoNaoEngoleOsPassos(t *testing.T) {
	var parts []string
	for i := 0; i < 30; i++ {
		parts = append(parts, fmt.Sprintf("a.yaml:%d:1: erro %d", i, i))
	}
	got := strings.Join(StateError(stMono, 80, 12, "o catálogo", errors.New(strings.Join(parts, "\n")), "Corrija o arquivo ou rode sem --catalog."), "\n")
	if !strings.Contains(got, "1. Corrija o arquivo ou rode sem --catalog.") || !strings.Contains(got, "a.yaml:0:1") {
		t.Fatalf("o passo e o começo do erro cabem:\n%s", got)
	}
}

func TestStateUnsupportedDizOSistema(t *testing.T) {
	got := strings.Join(StateUnsupported(stMono, 80, 12, "os apps nativos", "O Windows guarda os apps de outro jeito.", "Linux"), "\n")
	for _, w := range []string{"Os apps nativos: só existe no Windows (este sistema é Linux).", "O Windows guarda os apps de outro jeito.", "Nada foi alterado."} {
		if !strings.Contains(got, w) {
			t.Errorf("falta %q:\n%s", w, got)
		}
	}
}

func TestAdminStepsPorSistema(t *testing.T) {
	win := strings.Join(AdminSteps("windows", "privacidade"), "|")
	if !strings.Contains(win, "Botão direito no Terminal > Executar como administrador") || strings.Contains(win, "sudo") {
		t.Errorf("windows: %s", win)
	}
	for _, g := range []string{"linux", "darwin"} {
		u := strings.Join(AdminSteps(g, "privacidade apply"), "|")
		if !strings.Contains(u, "sudo toolkit privacidade apply") || strings.Contains(u, "Botão direito") {
			t.Errorf("%s: %s", g, u)
		}
	}
	generic := strings.Join(AdminSteps("", ""), "|")
	if !strings.Contains(generic, "botão direito") || !strings.Contains(generic, "sudo toolkit") {
		t.Errorf("genérico: %s", generic)
	}
	if !strings.Contains(flatten(StateNeedsAdmin(stMono, 80, 12, "a privacidade")), "O toolkit nunca eleva sozinho.") {
		t.Error("a frase 'nunca eleva sozinho' é obrigatória")
	}
}

func TestStateTooSmallFormaFixa(t *testing.T) {
	got := flatten(StateTooSmall(stMono, 50, 12, 60, 16))
	for _, w := range []string{"i Terminal pequeno", "A interface precisa de 60×16 e este tem 50×12.", "1. Aumente a janela.", "2. Ou rode toolkit list / toolkit install ... sem a interface.", "q sair"} {
		if !strings.Contains(got, w) {
			t.Errorf("falta %q:\n%s", w, got)
		}
	}
	for _, h := range []int{1, 2, 3} {
		out := StateTooSmall(stMono, 30, h, 60, 16)
		if !strings.Contains(out[len(out)-1], "q sair") {
			t.Errorf("h=%d: a última linha é o 'q sair': %q", h, out)
		}
	}
}

// flatten junta as linhas quebradas num texto só, para conferir frases inteiras.
func flatten(lines []string) string {
	return strings.Join(strings.Fields(strings.Join(lines, "\n")), " ")
}

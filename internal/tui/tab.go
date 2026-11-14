package tui

import tea "charm.land/bubbletea/v2"

// KeyHint é um atalho da barra de teclas. Os de menor prioridade (os últimos da lista) saem primeiro
// quando a janela é estreita; "? ajuda" e "q sair" são da raiz e nunca saem.
type KeyHint struct {
	Key   string // "↑↓", "espaço", "/"
	Label string // "mover", "marcar", "buscar"
}

// Tab é uma aba da TUI (Apps, Receitas, Diagnóstico, Log). A raiz cuida de tamanho, troca de aba,
// modais e modo; o que não é dela vai para a aba ativa. Todo texto que vem de fora (catálogo, saída de
// gerenciador, notas, erros) passa por textsafe antes de chegar em View/Status.
type Tab interface {
	// Init devolve o comando inicial da aba (carregar dados, iniciar o spinner); nil = nada.
	Init() tea.Cmd
	// Capturing diz se a aba está recebendo texto ou mostrando um modal: a raiz então repassa TODAS as
	// teclas à aba (exceto Ctrl+C), inclusive q, ?, Tab e os números, que viram texto da busca.
	Capturing() bool
	// Title é o nome inteiro da aba ("Receitas"); Short é o nome curto (60 a 69 colunas: "Rec").
	Title() string
	Short() string
	// Update trata uma mensagem que a raiz não consumiu.
	Update(msg tea.Msg) (Tab, tea.Cmd)
	// View desenha o corpo com exatamente h linhas de no máximo w colunas (a raiz recorta o excesso).
	View(w, h int) string
	// Keys são os atalhos da aba, do mais para o menos importante.
	Keys() []KeyHint
	// Status é o texto da linha de estado (item sob o cursor, caminho do log, pergunta [s/N]).
	Status() string
	// More é quantas linhas do corpo ficaram abaixo da dobra (0 = nada a rolar).
	More() int
	// Selected é quantos itens estão marcados (alimenta "Enter revisa N marcados").
	Selected() int
}

// Reloader é implementada pelas abas que mostram o estado do sistema: depois de uma execução elas o releem.
type Reloader interface{ Reload() tea.Cmd }

// Detailer é implementada pelas seções que têm painel de detalhe (docs/tui.md, fase 7). A partir de DetailMinWidth
// colunas a raiz pergunta a Detail(w, h) o texto do item sob o cursor (w = DetailWidth) e o desenha à direita da lista,
// que recebe o resto da largura. O painel é SÓ LEITURA (nunca repete ação) e usa só o que a seção já tem em memória.
// Devolver "" significa "nada a detalhar agora" (carregando, erro, vazio, não suportado, plano ou confirmação na tela, modal
// da seção): a raiz então desenha a lista na largura toda. Texto de fora passa por textsafe e QUEBRA; nunca reticências.
type Detailer interface {
	Detail(w, h int) string
}

// ResultKind é o tipo de um resultado: dá o símbolo e a palavra da faixa (✓ feito, ! parcial, ✗ falhou, i info, ! pendente).
type ResultKind int

// Tipos de resultado.
const (
	ResultOK ResultKind = iota
	ResultPartial
	ResultError
	ResultInfo
	ResultPending // a ação não terminou: espera algo (ex.: reinício); nunca é "feito"
)

// Result é o resultado de uma ação, mostrado na FAIXA da própria tela até a pessoa fechá-la (docs/tui.md).
// Title diz o que foi feito; Lines (até 4) dizem o que falhou, quanto mudou, onde está o backup e COMO DESFAZER.
// Todo texto vem já limpo por textsafe (a raiz limpa de novo ao desenhar).
type Result struct {
	Kind  ResultKind
	Title string
	Lines []string
}

// Resulter é implementada pelas abas que têm resultado a mostrar. A raiz desenha a faixa logo abaixo do
// cabeçalho quando Result() != nil e repassa a tecla x a Dismiss (a aba não capturando teclas).
type Resulter interface {
	Result() *Result
	Dismiss()
}

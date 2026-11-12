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

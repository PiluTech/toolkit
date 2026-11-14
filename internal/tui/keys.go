package tui

import "strings"

// Legenda ÚNICA de teclas (fase 6 do redesign). Toda dica da barra de teclas (Tab.Keys) e toda linha da ajuda "?"
// nasce desta tabela: a mesma letra tem o mesmo sentido em todas as seções. O rótulo com reticências ("aplicar…")
// avisa que a tecla abre uma confirmação (nada muda antes de você confirmar). Letra nova entra AQUI primeiro; o teste
// de vocabulário (keys_test.go) percorre as seções reais e reprova letra fora da tabela ou com outro rótulo.

// KeyDef é uma entrada da legenda. Label é o rótulo curto da barra (sem reticências; elas vêm de Confirms), Alt são os
// rótulos permitidos quando o objeto da tecla muda de seção mas o sentido não (ex.: "m" alterna "só usuário" numa
// seção e "máquina" noutra) e Help é a frase da ajuda.
type KeyDef struct {
	Key      string
	Label    string
	Alt      []string
	Confirms bool
	Help     string
}

// Hint é a dica da barra: o rótulo, com "…" quando a tecla abre uma confirmação.
func (k KeyDef) Hint() KeyHint {
	l := k.Label
	if k.Confirms {
		l += "…"
	}
	return KeyHint{Key: k.Key, Label: l}
}

// as troca o rótulo por uma das variantes permitidas (Alt); fora delas devolve o rótulo canônico (o teste reprova).
func (k KeyDef) as(label string) KeyDef {
	for _, a := range k.Alt {
		if a == label {
			k.Label = label
			return k
		}
	}
	return k
}

// on marca o rótulo de uma tecla que liga e desliga ("só usuário ✓").
func (k KeyDef) on(v bool) KeyDef {
	if v {
		k.Label += " ✓"
	}
	return k
}

// says dá a frase da ajuda desta seção para a tecla (a letra e o rótulo continuam os da legenda).
func (k KeyDef) says(help string) KeyDef {
	k.Help = help
	return k
}

// globalKeys são as teclas que valem em qualquer seção (a seção só as usa quando o corpo permite: o "/" busca onde
// há lista, por exemplo). Ordem = ordem da ajuda.
func globalKeys(areaMode bool, titles []string) []KeyDef {
	areas := KeyDef{Key: "1-" + itoa(len(titles)), Help: "aba: " + strings.Join(titles, ", ") + "."}
	tabs := KeyDef{Key: "Tab", Help: "próxima aba; Shift+Tab, a anterior."}
	if areaMode {
		areas = KeyDef{Key: "0-" + itoa(len(titles)-1), Help: "área: " + strings.Join(titles, ", ") + "."}
		tabs = KeyDef{Key: "Tab", Help: "próxima seção da área; Shift+Tab, a anterior."}
	}
	return []KeyDef{
		areas,
		tabs,
		{Key: "↑↓ j k", Help: "move o cursor ou rola."},
		{Key: "g G", Help: "início ou fim; PgUp e PgDn, uma página."},
		{Key: "← → h l", Help: "foco nas etapas ou volta à lista."},
		{Key: "/", Help: "busca onde há lista; Esc limpa."},
		{Key: "Enter", Help: "abre, revisa ou aciona o botão em foco."},
		{Key: "Esc", Help: "volta um nível ou cancela."},
		{Key: "x", Help: "fecha a faixa de resultado."},
		{Key: "?", Help: "esta ajuda."},
		{Key: "q", Help: "sai (Ctrl+C também); na execução pergunta antes."},
	}
}

// actionKeys é a tabela de AÇÕES por sentido: cada letra significa UMA coisa em todas as seções.
var actionKeys = []KeyDef{
	{Key: "a", Label: "aplicar", Confirms: true, Help: "aplica o perfil escolhido."},
	{Key: "d", Label: "desfazer", Confirms: true, Help: "desfaz o que o toolkit aplicou."},
	{Key: "p", Label: "ver plano", Help: "mostra o que mudaria, sem alterar nada."},
	{Key: "c", Label: "ler de novo", Help: "lê o estado outra vez (recarrega)."},
	{Key: "r", Label: "remover", Confirms: true, Help: "remove o que está marcado."},
	{Key: "m", Label: "alternar", Alt: []string{"só usuário", "todos os usuários", "máquina", "receitas"}, Help: "alterna o escopo ou a visão."},
	{Key: "i", Label: "como reinstalar", Help: "mostra, só como texto, como reinstalar."},
	{Key: "b", Label: "básico", Help: "marca o perfil básico (o conservador)."},
	{Key: "t", Label: "todos", Help: "marca todo o catálogo da seção."},
	{Key: "e", Label: "detalhes", Help: "mostra também a saída original de cada etapa."},
	{Key: "v", Label: "versões", Help: "abre a lista de versões do app."},
	{Key: "espaço", Label: "marcar", Help: "marca ou desmarca o item."},
	{Key: "s", Label: "confirmar", Alt: []string{"confirma", "executar"}, Help: "executa na Revisão, confirma uma confirmação e responde sim a uma pergunta [s/N]."},
	{Key: "f", Label: "seguir o fim", Help: "liga e desliga o seguir o fim do log."},
	{Key: "u", Label: "versão nova", Help: "abre as notas da versão nova (quando há)."},
}

// act devolve a entrada da tabela de ações (chave desconhecida devolve só a chave: o teste de vocabulário a reprova).
func act(key string) KeyDef {
	for _, k := range actionKeys {
		if k.Key == key {
			return k
		}
	}
	return KeyDef{Key: key}
}

// helpKeysOf devolve as teclas de uma seção para a ajuda: a lista própria (SectionHelper) ou, na falta dela, as dicas
// da barra com a frase da tabela. Seção sem teclas devolve nil.
func helpKeysOf(t Tab) []KeyDef {
	if h, ok := t.(SectionHelper); ok {
		return h.HelpKeys()
	}
	var out []KeyDef
	for _, h := range t.Keys() {
		d := KeyDef{Key: h.Key, Label: strings.TrimSuffix(h.Label, "…"), Confirms: strings.HasSuffix(h.Label, "…")}
		if a := act(h.Key); a.Label != "" {
			d.Help = a.Help
		} else {
			d.Help = navHelp(h.Key)
		}
		out = append(out, d)
	}
	return out
}

// SectionHelper é implementada pela seção que explica as suas teclas (uma frase por tecla, na ordem da barra). As
// teclas de ação vêm de act(...).says(...): a letra e o rótulo são os da legenda; a frase é desta seção.
type SectionHelper interface{ HelpKeys() []KeyDef }

// navDef monta uma entrada de navegação (não é letra de ação) com a frase da seção.
func navDef(key, label, help string) KeyDef { return KeyDef{Key: key, Label: label, Help: help} }

// navHelp é a frase padrão das teclas de navegação que aparecem nas barras (as seções com comportamento próprio
// implementam SectionHelper e dizem o seu).
func navHelp(key string) string {
	switch key {
	case "↑↓":
		return "move o cursor (ou rola o texto)."
	case "Enter":
		return "abre ou revisa."
	case "/":
		return "busca na lista; Esc limpa."
	case "→":
		return "foco nas etapas; ← volta à lista."
	case "←":
		return "volta à lista."
	case "Esc":
		return "volta ou cancela."
	case "PgUp/PgDn":
		return "rola uma página."
	}
	if strings.HasPrefix(key, "1-") || strings.HasPrefix(key, "0-") {
		return "vai direto para o item."
	}
	return "veja a ajuda da seção."
}

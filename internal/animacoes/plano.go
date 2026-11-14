package animacoes

import (
	"context"
	"fmt"
	"strings"

	"github.com/PiluTech/toolkit/internal/diagfmt"
	"github.com/PiluTech/toolkit/internal/privacidade"
)

// Change é uma mudança do plano (um valor do registro do usuário).
type Change struct {
	Item    string // id do item do catálogo ("desfazer" no plano de desfazer)
	ID      string // chave do backup
	Target  string // "HKCU\\...\\MinAnimate"
	Before  string // estado atual, legível
	After   string // estado depois
	Pending bool   // false = já está assim
	Undo    string // comando manual que restaura o estado de ANTES desta mudança

	orig  privacidade.Entry                         // estado de antes, para o backup
	apply func(ctx context.Context, s System) error // aplica a mudança
}

// Plan é o que `aplicar` (ou `desfazer`) faria.
type Plan struct {
	Undo    bool
	Changes []Change
}

// Pending conta as mudanças pendentes.
func (p *Plan) Pending() int {
	n := 0
	for _, c := range p.Changes {
		if c.Pending {
			n++
		}
	}
	return n
}

func describeReg(v RegValue, exists bool, l Lang) string {
	if !exists {
		return diagfmt.T(l, "não definido", "not set", "no definido")
	}
	if v.Kind == privacidade.RegString || v.Kind == privacidade.RegExpandString {
		return `"` + v.Str + `"`
	}
	return fmt.Sprintf("%d", v.DWORD)
}

func regUndo(key, name string, v RegValue, exists bool) string {
	if !exists {
		return fmt.Sprintf(`reg delete "%s" /v %s /f`, key, name)
	}
	switch v.Kind {
	case privacidade.RegString:
		return fmt.Sprintf(`reg add "%s" /v %s /t REG_SZ /d "%s" /f`, key, name, v.Str)
	case privacidade.RegExpandString:
		return fmt.Sprintf(`reg add "%s" /v %s /t REG_EXPAND_SZ /d "%s" /f`, key, name, v.Str)
	}
	return fmt.Sprintf(`reg add "%s" /v %s /t REG_DWORD /d %d /f`, key, name, v.DWORD)
}

// Build lê o estado atual (só leitura) e monta o plano. A ausência de um valor só vira "não definido" quando o System a
// provou; qualquer erro de leitura aborta o plano.
func Build(ctx context.Context, sys System, l Lang) (*Plan, error) {
	plan := &Plan{}
	for _, it := range Items() {
		cur, exists, err := sys.RegRead(ctx, it.Key, it.Name)
		if err != nil {
			return nil, fmt.Errorf("%s\\%s: %w", it.Key, it.Name, err)
		}
		id := regID(it.Key, it.Name)
		c := Change{Item: it.ID, ID: id, Target: it.Key + `\` + it.Name,
			Before: describeReg(cur, exists, l), After: describeReg(it.Value, true, l), Pending: !exists || cur != it.Value,
			Undo: regUndo(it.Key, it.Name, cur, exists),
			orig: privacidade.Entry{ID: id, Kind: "reg", Key: it.Key, Name: it.Name, Existed: exists, RegKind: cur.Kind, DWORD: cur.DWORD, Str: cur.Str}}
		key, name, want := it.Key, it.Name, it.Value
		c.apply = func(ctx context.Context, s System) error { return s.RegWrite(ctx, key, name, want) }
		plan.Changes = append(plan.Changes, c)
	}
	return plan, nil
}

// Format mostra o plano: selo experimental, o que muda (antes -> depois), o efeito esperado, quando ele vale e o comando
// manual que desfaz cada item. Não promete ganho de desempenho.
func (p *Plan) Format(l Lang) string {
	t := func(pt, en, es string) string { return diagfmt.T(l, pt, en, es) }
	var b strings.Builder
	if p.Undo {
		b.WriteString(t("Restaurar o estado anterior das animações (EXPERIMENTAL)", "Restore the previous animation state (EXPERIMENTAL)", "Restaurar el estado anterior de las animaciones (EXPERIMENTAL)") + "\n")
	} else {
		b.WriteString(t("Reduzir animações do Windows (EXPERIMENTAL)", "Reduce Windows animations (EXPERIMENTAL)", "Reducir animaciones de Windows (EXPERIMENTAL)") + "\n")
		b.WriteString(t("Função experimental: os valores ainda não foram confirmados numa máquina de teste. Só mexe no usuário atual (HKCU), sem administrador, e tudo volta com o desfazer.",
			"Experimental feature: the values have not yet been confirmed on a test machine. It only touches the current user (HKCU), without administrator, and everything comes back with undo.",
			"Función experimental: los valores aún no se confirmaron en una máquina de prueba. Solo toca al usuario actual (HKCU), sin administrador, y todo vuelve con deshacer.") + "\n")
	}
	byID := map[string]Item{}
	for _, it := range Items() {
		byID[it.ID] = it
	}
	for _, c := range p.Changes {
		it, known := byID[c.Item]
		title, effect := t("Restaurar o valor original", "Restore the original value", "Restaurar el valor original"), ""
		if known {
			title, effect = it.Title(l), it.Effect(l)
		}
		fmt.Fprintf(&b, "\n%s\n", title)
		if effect != "" && !p.Undo {
			fmt.Fprintf(&b, "  %s\n", effect)
		}
		if c.Pending {
			fmt.Fprintf(&b, "  ! %s: %s -> %s\n      %s %s\n", c.Target, c.Before, c.After, t("desfazer:", "undo:", "deshacer:"), c.Undo)
		} else {
			fmt.Fprintf(&b, "  ✓ %s: %s (%s)\n", c.Target, c.Before, t("já está assim", "already so", "ya está así"))
		}
	}
	if !p.Undo {
		fmt.Fprintf(&b, "\n%s\n", t("Algumas animações só valem depois de reabrir a sessão do Windows ou o Explorer; o toolkit não reinicia o Explorer sozinho.",
			"Some animations only take effect after you sign in again or reopen Explorer; the toolkit does not restart Explorer by itself.",
			"Algunas animaciones solo se aplican tras volver a iniciar sesión o reabrir el Explorador; el toolkit no reinicia el Explorador por sí solo."))
		fmt.Fprintf(&b, "%s\n", t("Reduzir animação melhora a sensação de resposta, não prova ganho de desempenho.",
			"Reducing animation improves how responsive it feels; it does not prove a performance gain.",
			"Reducir la animación mejora la sensación de respuesta; no prueba una ganancia de rendimiento."))
	}
	fmt.Fprintf(&b, "\n%d %s.\n", p.Pending(), t("mudança(s) pendente(s)", "pending change(s)", "cambio(s) pendiente(s)"))
	return b.String()
}

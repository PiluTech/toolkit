// Package diagfmt transforma a saída crua dos diagnósticos (JSON do PowerShell, /proc/meminfo, system_profiler...)
// num resumo legível, em pt-BR com en e es. Cada renderizador é uma função pura (texto cru -> texto), registrada pelo
// nome que a receita usa no campo `render` da etapa. Falha de leitura nunca derruba o diagnóstico: o chamador mostra a
// saída original.
package diagfmt

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
)

// Lang é o idioma da saída.
type Lang int

// Idiomas suportados (pt-BR é a origem e o padrão).
const (
	PT Lang = iota
	EN
	ES
)

// LangFromEnv escolhe o idioma por TOOLKIT_LANG, LC_ALL, LC_MESSAGES e LANG, nessa ordem: o primeiro VALOR NÃO VAZIO
// decide; en* é inglês, es* é espanhol e qualquer outro (inclusive pt e C) é pt-BR.
func LangFromEnv(get func(string) string) Lang {
	for _, k := range []string{"TOOLKIT_LANG", "LC_ALL", "LC_MESSAGES", "LANG"} {
		v := strings.ToLower(strings.TrimSpace(get(k)))
		if v == "" {
			continue
		}
		switch {
		case strings.HasPrefix(v, "en"):
			return EN
		case strings.HasPrefix(v, "es"):
			return ES
		}
		return PT
	}
	return PT
}

// T escolhe o texto do idioma.
func T(l Lang, pt, en, es string) string {
	switch l {
	case EN:
		return en
	case ES:
		return es
	}
	return pt
}

// Renderer transforma a saída crua de uma etapa no resumo legível.
type Renderer func(raw string, l Lang) (string, error)

var registry = map[string]Renderer{}

// Register liga um nome de `render` a um renderizador (chamado nos init dos arquivos deste pacote).
func Register(name string, r Renderer) {
	if _, dup := registry[name]; dup {
		panic("diagfmt: renderizador repetido: " + name)
	}
	registry[name] = r
}

// Known diz se o nome de `render` existe.
func Known(name string) bool { _, ok := registry[name]; return ok }

// Names lista os renderizadores (para a mensagem de erro do parse).
func Names() []string {
	out := make([]string, 0, len(registry))
	for n := range registry {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// Render aplica o renderizador. Erro = o chamador mostra a saída original.
func Render(name, raw string, l Lang) (string, error) {
	r, ok := registry[name]
	if !ok {
		return "", fmt.Errorf("renderizador desconhecido: %s", name)
	}
	return r(raw, l)
}

// Number formata com o separador decimal do idioma e no máximo dec casas, sem zeros inúteis: 5,1 (pt/es) e 5.1 (en).
func Number(f float64, dec int, l Lang) string {
	s := strconv.FormatFloat(f, 'f', dec, 64)
	if strings.Contains(s, ".") {
		s = strings.TrimRight(strings.TrimRight(s, "0"), ".")
	}
	if l != EN {
		s = strings.Replace(s, ".", ",", 1)
	}
	return s
}

// Bytes formata uma capacidade em GB (ou MB/TB). binary=true usa potências de 1024 (memória RAM: 8589934592 = 8 GB,
// como o Windows mostra); false usa 1000 (discos: 256060514304 = 256 GB, como o fabricante anuncia).
func Bytes(b uint64, binary bool, l Lang) string {
	base := 1000.0
	if binary {
		base = 1024.0
	}
	v := float64(b)
	units := []string{"B", "KB", "MB", "GB", "TB", "PB"}
	i := 0
	for v >= base && i < len(units)-1 {
		v /= base
		i++
	}
	dec := 1
	if v >= 100 || i == 0 {
		dec = 0
	}
	// 7.99 GB vira 8 GB, não 8,0
	if math.Abs(v-math.Round(v)) < 0.05 {
		dec = 0
	}
	return Number(v, dec, l) + " " + units[i]
}

// Percent formata 0.65 como 65%.
func Percent(frac float64) string { return strconv.Itoa(int(math.Round(frac*100))) + "%" }

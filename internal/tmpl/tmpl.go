// Package tmpl é um substituidor estrito de {{variável}}: só troca texto, sem lógica nem funções
// (por isso não usa text/template). Variável desconhecida é erro.
package tmpl

import (
	"fmt"
	"strings"
)

// Variáveis aceitas nos campos de texto do catálogo.
const (
	OS             = "os"
	Arch           = "arch"
	Version        = "version"
	Major          = "major"
	MajorMinor     = "major_minor"
	ToolkitVersion = "toolkit_version"
)

// Known é a lista fechada de variáveis.
var Known = map[string]bool{
	OS: true, Arch: true, Version: true, Major: true, MajorMinor: true, ToolkitVersion: true,
}

// VersionVars dependem da versão escolhida pelo usuário.
var VersionVars = map[string]bool{Version: true, Major: true, MajorMinor: true}

// Names devolve os nomes usados em s, na ordem. Erra em "{{" sem fechar ou em nome vazio.
func Names(s string) ([]string, error) {
	var names []string
	rest := s
	for {
		i := strings.Index(rest, "{{")
		if i < 0 {
			if strings.Contains(rest, "}}") {
				return nil, fmt.Errorf("%q: \"}}\" sem \"{{\"", s)
			}
			return names, nil
		}
		j := strings.Index(rest[i:], "}}")
		if j < 0 {
			return nil, fmt.Errorf("%q: \"{{\" sem fechar", s)
		}
		name := strings.TrimSpace(rest[i+2 : i+j])
		if name == "" {
			return nil, fmt.Errorf("%q: variável vazia", s)
		}
		names = append(names, name)
		rest = rest[i+j+2:]
	}
}

// Validate confere que todas as variáveis de s são conhecidas.
func Validate(s string) error {
	names, err := Names(s)
	if err != nil {
		return err
	}
	for _, n := range names {
		if !Known[n] {
			return fmt.Errorf("%q: variável desconhecida %q", s, n)
		}
	}
	return nil
}

// Expand troca cada {{nome}} pelo valor em vars. Nome desconhecido ou sem valor é erro.
func Expand(s string, vars map[string]string) (string, error) {
	if _, err := Names(s); err != nil {
		return "", err
	}
	var b strings.Builder
	rest := s
	for {
		i := strings.Index(rest, "{{")
		if i < 0 {
			b.WriteString(rest)
			return b.String(), nil
		}
		j := strings.Index(rest[i:], "}}")
		name := strings.TrimSpace(rest[i+2 : i+j])
		if !Known[name] {
			return "", fmt.Errorf("%q: variável desconhecida %q", s, name)
		}
		v, ok := vars[name]
		if !ok {
			return "", fmt.Errorf("%q: sem valor para %q", s, name)
		}
		b.WriteString(rest[:i])
		b.WriteString(v)
		rest = rest[i+j+2:]
	}
}

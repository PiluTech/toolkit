package catalog

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"regexp"
	"strings"
)

var (
	templateRe = regexp.MustCompile(`\{\{[^{}]*\}\}`)
	hostRe     = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)+$`)
)

// CheckURL valida uma URL de download do catálogo. Regras (decisão: só nomes públicos de máquina, nada local):
//   - esquema https, host não vazio, sem userinfo (usuario:senha@), sem fragmento;
//   - sem espaço nem caractere de controle;
//   - host só ASCII em minúsculas (punycode para IDN), com pelo menos um ponto: "localhost", nomes de um só
//     rótulo e IP literal (v4 ou v6) são recusados; porta explícita só se for 443;
//   - com templates=true (texto do catálogo, antes da expansão), {{variável}} só pode aparecer depois do host
//     (no caminho ou na query); com templates=false (URL já expandida) qualquer "{{" é erro.
func CheckURL(raw string, templates bool) error {
	if raw == "" {
		return errors.New("url vazia")
	}
	for _, r := range raw {
		if r <= 0x20 || r == 0x7f || r == '\\' {
			return fmt.Errorf("url com espaço, caractere de controle ou barra invertida: %q", raw)
		}
	}
	if !strings.HasPrefix(raw, "https://") {
		return errors.New("url deve começar com https://")
	}
	authority := strings.TrimPrefix(raw, "https://")
	if i := strings.IndexAny(authority, "/?#"); i >= 0 {
		authority = authority[:i]
	}
	if strings.Contains(authority, "{{") || strings.Contains(authority, "}}") {
		return errors.New("url com variável de template no host (só é permitida no caminho)")
	}
	check := raw
	if templates {
		check = templateRe.ReplaceAllString(raw, "x")
	}
	if strings.Contains(check, "{{") || strings.Contains(check, "}}") {
		return errors.New("url com template mal formado ou não expandido")
	}
	u, err := url.Parse(check)
	if err != nil {
		return fmt.Errorf("url inválida: %w", err)
	}
	switch {
	case u.Scheme != "https":
		return errors.New("url deve usar https")
	case u.User != nil:
		return errors.New("url com usuário/senha (userinfo) não é aceita")
	case u.Fragment != "" || strings.Contains(check, "#"):
		return errors.New("url com fragmento (#) não é aceita")
	case u.Hostname() == "":
		return errors.New("url sem host")
	}
	if p := u.Port(); p != "" && p != "443" {
		return fmt.Errorf("porta %s não é aceita (só 443 ou a padrão)", p)
	}
	host := u.Hostname()
	if net.ParseIP(host) != nil {
		return fmt.Errorf("IP literal %q não é aceito: use um nome de host público", host)
	}
	if !hostRe.MatchString(host) {
		return fmt.Errorf("host %q inválido (use um nome público em minúsculas com ponto; localhost e nomes locais são recusados)", host)
	}
	return nil
}

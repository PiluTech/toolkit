package catalog

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"

	"go.yaml.in/yaml/v3"
)

// Campos do bloco `detect:` por SO. Cada campo só vale no SO dele; chave de outro SO e chave desconhecida
// são erros diferentes (como nos instaladores).
var detectFields = map[string][]string{
	KeyMacOS:   {"app_bundle", "bundle_id"},
	KeyWindows: {"display_name"},
	KeyLinux:   {"desktop_id", "flatpak_id", "snap"},
}

var detectOSKeys = []string{KeyWindows, KeyMacOS, KeyLinux}

var (
	// Nome de bundle: letras, números e marcas (macOS grava nomes decompostos), espaço e . _ + & ( ) ' -, sem
	// "/" nem "\" (nunca sai da pasta de apps), terminando em .app.
	appBundleRe = regexp.MustCompile(`^[\p{L}\p{N}][\p{L}\p{N}\p{M} ._+&()'-]*\.app$`)
	bundleIDRe  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9.-]*$`)
	desktopIDRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*\.desktop$`)
	flatpakIDRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)
	snapRe      = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)
)

const maxDetectLen = 200

func (p *parser) parseDetect(appID string, n *yaml.Node) map[string]Detect {
	if n.Kind != yaml.MappingNode {
		p.errAt(n, "app %q: detect deve ser um mapa (macos, windows, linux)", appID)
		return nil
	}
	if len(n.Content) == 0 {
		p.errAt(n, "app %q: detect precisa de ao menos um SO (%s)", appID, strings.Join(detectOSKeys, ", "))
		return nil
	}
	out := map[string]Detect{}
	seen := map[string]bool{}
	for i := 0; i+1 < len(n.Content); i += 2 {
		k, v := n.Content[i], n.Content[i+1]
		if p.dup(seen, k, fmt.Sprintf("app %q (detect)", appID)) {
			continue
		}
		if !slices.Contains(detectOSKeys, k.Value) {
			p.errAt(k, "app %q: detect: chave desconhecida %q (use: %s)", appID, k.Value, strings.Join(detectOSKeys, ", "))
			continue
		}
		if d, ok := p.parseDetectOS(appID, k.Value, v); ok {
			out[k.Value] = d
		}
	}
	return out
}

func (p *parser) parseDetectOS(appID, osKey string, n *yaml.Node) (Detect, bool) {
	start := len(p.errs)
	d := Detect{Line: n.Line, Col: n.Column}
	if n.Kind != yaml.MappingNode {
		p.errAt(n, "app %q: detect.%s deve ser um mapa de campos (%s)", appID, osKey, strings.Join(detectFields[osKey], ", "))
		return d, false
	}
	if len(n.Content) == 0 {
		p.errAt(n, "app %q: detect.%s precisa de ao menos um campo (%s)", appID, osKey, strings.Join(detectFields[osKey], ", "))
		return d, false
	}
	allowed := detectFields[osKey]
	seen := map[string]bool{}
	for i := 0; i+1 < len(n.Content); i += 2 {
		k, v := n.Content[i], n.Content[i+1]
		if p.dup(seen, k, fmt.Sprintf("app %q (detect.%s)", appID, osKey)) {
			continue
		}
		if !slices.Contains(allowed, k.Value) {
			if p.isDetectFieldOfOtherOS(k.Value) {
				p.errAt(k, "app %q: o campo %q não vale para detect.%s (aceita: %s)", appID, k.Value, osKey, strings.Join(allowed, ", "))
			} else {
				p.errAt(k, "app %q: detect.%s: campo desconhecido %q (aceita: %s)", appID, osKey, k.Value, strings.Join(allowed, ", "))
			}
			continue
		}
		if v.Kind != yaml.ScalarNode || v.Tag != "!!str" {
			p.errAt(v, "app %q: detect.%s: %q deve ser um texto", appID, osKey, k.Value)
			continue
		}
		val := v.Value
		if val == "" || strings.TrimSpace(val) != val {
			p.errAt(v, "app %q: detect.%s: %q não pode ser vazio nem ter espaço nas pontas", appID, osKey, k.Value)
			continue
		}
		if utf8.RuneCountInString(val) > maxDetectLen || (strings.Contains(val, "..") && k.Value != "display_name") {
			p.errAt(v, "app %q: detect.%s: %q com tamanho ou formato inválido", appID, osKey, k.Value)
			continue
		}
		var ok bool
		switch k.Value {
		case "app_bundle":
			ok = appBundleRe.MatchString(val)
			d.AppBundle = val
		case "bundle_id":
			ok = bundleIDRe.MatchString(val)
			d.BundleID = val
		case "display_name":
			ok = true // texto livre (já sem controle e formatação pela carga); casa com o DisplayName do registro
			d.DisplayName = val
		case "desktop_id":
			ok = desktopIDRe.MatchString(val)
			d.DesktopID = val
		case "flatpak_id":
			ok = flatpakIDRe.MatchString(val)
			d.FlatpakID = val
		case "snap":
			ok = snapRe.MatchString(val)
			d.Snap = val
		}
		if !ok {
			p.errAt(v, "app %q: detect.%s: %s %q com caracteres ou formato não permitidos (%s)", appID, osKey, k.Value, val, detectHint(k.Value))
		}
	}
	return d, len(p.errs) == start
}

func (p *parser) isDetectFieldOfOtherOS(name string) bool {
	for _, fs := range detectFields {
		if slices.Contains(fs, name) {
			return true
		}
	}
	return false
}

func detectHint(field string) string {
	switch field {
	case "app_bundle":
		return "nome do bundle terminado em .app, sem barras, como Discord.app"
	case "bundle_id":
		return "letras, números, ponto e hífen, como com.hnc.Discord"
	case "desktop_id":
		return "arquivo terminado em .desktop, sem barras"
	case "flatpak_id":
		return "letras, números e . _ -"
	case "snap":
		return "minúsculas, números e hífen"
	}
	return ""
}

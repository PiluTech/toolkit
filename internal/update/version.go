// Package update implementa `toolkit update`: descobre a release, baixa o binário do SO, confere o SHA-256 pelo
// checksums.txt da MESMA release (com o pacote download: https, host público, limite de tamanho, hash antes do
// uso) e troca o binário em execução de forma atômica. Nunca usa sudo e nunca instala sem o hash conferido.
package update

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Version é uma versão semântica "vMAJOR.MINOR.PATCH[-pré]".
type Version struct {
	Major, Minor, Patch int
	Pre                 string
}

var versionRe = regexp.MustCompile(`^v?(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(?:-([0-9A-Za-z][0-9A-Za-z.]*))?$`)

// ErrBadVersion indica que a versão não é "vX.Y.Z[-pré]".
var ErrBadVersion = errors.New("versão inválida: use algo como v0.1.0")

// ParseVersion aceita com ou sem o "v" inicial. Qualquer outra coisa (inclusive "dev") é ErrBadVersion: o valor
// entra na URL de download, então só este formato passa.
func ParseVersion(s string) (Version, error) {
	m := versionRe.FindStringSubmatch(strings.TrimSpace(s))
	if m == nil {
		return Version{}, fmt.Errorf("%w (recebido %q)", ErrBadVersion, truncate(s, 40))
	}
	n := func(x string) int { v, _ := strconv.Atoi(x); return v }
	return Version{Major: n(m[1]), Minor: n(m[2]), Patch: n(m[3]), Pre: m[4]}, nil
}

func truncate(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}

// String devolve "vX.Y.Z[-pré]".
func (v Version) String() string {
	s := fmt.Sprintf("v%d.%d.%d", v.Major, v.Minor, v.Patch)
	if v.Pre != "" {
		s += "-" + v.Pre
	}
	return s
}

// Compare devolve -1, 0 ou 1 (a pré-release é MENOR que a versão final, como no semver).
func Compare(a, b Version) int {
	for _, p := range [][2]int{{a.Major, b.Major}, {a.Minor, b.Minor}, {a.Patch, b.Patch}} {
		if p[0] != p[1] {
			if p[0] < p[1] {
				return -1
			}
			return 1
		}
	}
	switch {
	case a.Pre == b.Pre:
		return 0
	case a.Pre == "":
		return 1
	case b.Pre == "":
		return -1
	}
	return comparePre(a.Pre, b.Pre)
}

// comparePre compara identificadores de pré-release separados por ponto (numérico < texto).
func comparePre(a, b string) int {
	as, bs := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(as) && i < len(bs); i++ {
		x, y := as[i], bs[i]
		xn, xe := strconv.Atoi(x)
		yn, ye := strconv.Atoi(y)
		switch {
		case xe == nil && ye == nil && xn != yn:
			if xn < yn {
				return -1
			}
			return 1
		case xe == nil && ye != nil:
			return -1
		case xe != nil && ye == nil:
			return 1
		case xe != nil && ye != nil && x != y:
			if x < y {
				return -1
			}
			return 1
		}
	}
	switch {
	case len(as) < len(bs):
		return -1
	case len(as) > len(bs):
		return 1
	}
	return 0
}

// AssetName é o nome do binário da release para o SO e a arquitetura ("toolkit_linux_arm64", ".exe" no Windows).
func AssetName(goos, goarch string) (string, error) {
	switch goos {
	case "windows", "linux", "darwin":
	default:
		return "", fmt.Errorf("sistema sem binário de release: %q", goos)
	}
	switch goarch {
	case "amd64", "arm64":
	default:
		return "", fmt.Errorf("arquitetura sem binário de release: %q", goarch)
	}
	name := "toolkit_" + goos + "_" + goarch
	if goos == "windows" {
		name += ".exe"
	}
	return name, nil
}

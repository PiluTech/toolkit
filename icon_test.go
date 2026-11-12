package toolkit_test

import (
	"crypto/sha256"
	"encoding/hex"
	"image/png"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

func TestIconePNGTemDimensoesECantosTransparentes(t *testing.T) {
	for _, tam := range []int{1024, 512, 256, 128, 64} {
		nome := filepath.Join("assets", "icon", "toolkit-"+strconv.Itoa(tam)+".png")
		testarPNG(t, nome, tam)
	}
	testarPNG(t, filepath.Join("assets", "icon", "toolkit-site.png"), 1024)
}

func testarPNG(t *testing.T, nome string, tam int) {
	t.Helper()
	f, err := os.Open(nome)
	if err != nil {
		t.Fatalf("falta %s: %v", nome, err)
	}
	defer func() { _ = f.Close() }()
	img, err := png.Decode(f)
	if err != nil {
		t.Fatalf("%s não é PNG válido: %v", nome, err)
	}
	b := img.Bounds()
	if b.Dx() != tam || b.Dy() != tam {
		t.Errorf("%s: %dx%d, esperado %dx%d", nome, b.Dx(), b.Dy(), tam, tam)
	}
	// Canto (0,0): fora do quadrado arredondado, tem de ser totalmente transparente.
	if _, _, _, a := img.At(0, 0).RGBA(); a != 0 {
		t.Errorf("%s: canto superior esquerdo deveria ser transparente (alfa=%d)", nome, a)
	}
	// Centro: dentro do ícone, opaco.
	if _, _, _, a := img.At(b.Dx()/2, b.Dy()/2).RGBA(); a == 0 {
		t.Errorf("%s: centro deveria ser opaco", nome)
	}
}

func TestIconeSVGFonteBateComOHashRegistrado(t *testing.T) {
	svg, err := os.ReadFile(filepath.Join("assets", "icon", "toolkit.svg"))
	if err != nil {
		t.Fatalf("falta o SVG fonte: %v", err)
	}
	reg, err := os.ReadFile(filepath.Join("assets", "icon", "toolkit.svg.sha256"))
	if err != nil {
		t.Fatalf("falta o hash registrado: %v", err)
	}
	campos := strings.Fields(string(reg))
	if len(campos) < 1 || len(campos[0]) != 64 {
		t.Fatalf("toolkit.svg.sha256 mal formado: %q", reg)
	}
	soma := sha256.Sum256(svg)
	if got := hex.EncodeToString(soma[:]); got != campos[0] {
		t.Errorf("o SVG mudou (sha256 %s) mas o registro diz %s; rode scripts/gerar-icone.sh", got, campos[0])
	}
}

func TestReadmeReferenciaIconeQueExiste(t *testing.T) {
	data, err := os.ReadFile("README.md")
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`<img[^>]*src="([^"]+)"[^>]*>`).FindStringSubmatch(string(data))
	if m == nil {
		t.Fatal("o README deveria ter a imagem do ícone no cabeçalho")
	}
	if strings.HasPrefix(m[1], "/") || strings.Contains(m[1], "://") {
		t.Errorf("caminho do ícone deve ser relativo: %s", m[1])
	}
	if _, err := os.Stat(filepath.FromSlash(m[1])); err != nil {
		t.Errorf("o README aponta para %s, que não existe: %v", m[1], err)
	}
	if !regexp.MustCompile(`<img[^>]*alt="[^"]+"`).Match(data) {
		t.Error("a imagem do ícone precisa de texto alternativo (alt)")
	}
}

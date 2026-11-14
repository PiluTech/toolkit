package animacoes

import (
	"strings"
	"testing"

	"github.com/PiluTech/toolkit/internal/diagfmt"
)

// O catálogo é FECHADO: só estes valores, todos no HKCU. Valor novo entra aqui junto com a fonte que o confirma.
func TestCatalogoFechado(t *testing.T) {
	want := map[string]bool{`HKCU\Control Panel\Desktop\WindowMetrics|MinAnimate`: true}
	items := Items()
	if len(items) != len(want) {
		t.Fatalf("o catálogo tem %d itens, queria %d", len(items), len(want))
	}
	for _, it := range items {
		if !strings.HasPrefix(it.Key, `HKCU\`) {
			t.Errorf("%s: fora do HKCU (animações são por usuário)", it.ID)
		}
		if !want[it.Key+"|"+it.Name] {
			t.Errorf("valor fora do catálogo fechado: %s\\%s", it.Key, it.Name)
		}
		if it.Title(diagfmt.PT) == "" || it.Why(diagfmt.PT) == "" || it.Effect(diagfmt.PT) == "" {
			t.Errorf("%s: faltou texto", it.ID)
		}
	}
}

func TestMinAnimateEhTextoZero(t *testing.T) {
	it := Items()[0]
	if it.Value != str("0") {
		t.Fatalf("MinAnimate é REG_SZ \"0\" (desliga a animação): %+v", it.Value)
	}
}

// Nada de UserPreferencesMask (máscara binária frágil), serviço, tarefa, HKLM ou valor fora do plano.
func TestCatalogoNaoTemOQueNaoFazer(t *testing.T) {
	for _, it := range Items() {
		low := strings.ToLower(it.Key + it.Name)
		for _, bad := range []string{"userpreferencesmask", "hklm", "services", "transparency", "powercfg", "policies"} {
			if strings.Contains(low, bad) {
				t.Errorf("%s contém %q", it.ID, bad)
			}
		}
	}
}

func TestIDsUnicos(t *testing.T) {
	seen := map[string]bool{}
	for _, it := range Items() {
		if seen[it.ID] || seen[regID(it.Key, it.Name)] {
			t.Errorf("ID repetido: %s", it.ID)
		}
		seen[it.ID] = true
	}
}

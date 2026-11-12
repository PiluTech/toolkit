package tui

import (
	"strings"
	"testing"
)

func externalBackend() *fakeBackend {
	st := defaultStatus()
	st["python"] = AppStatus{Installed: true, External: true, ExternalManager: "Homebrew", ExternalWhere: "/Applications/Python.app", Version: "3.12"}
	return &fakeBackend{apps: sampleApps(), status: st}
}

func TestAppsInstaladoForaDoGerenciadorMostraSimboloEPalavras(t *testing.T) {
	tab := loaded(t, externalBackend())
	// Largura >= 100: a coluna de estado comporta o texto completo.
	v := plain(tab.View(110, 19))
	if !strings.Contains(v, "✓ instalado (fora do Homebrew)") {
		t.Fatalf("texto completo ausente:\n%s", v)
	}
	// Nunca depende de cor: o modo sem cor tem o mesmo texto (o Theme do teste é Mono).
	if strings.Contains(v, "não instalado") && !strings.Contains(v, "○ não instalado") {
		t.Fatalf("estado sem símbolo:\n%s", v)
	}
}

func TestAppsInstaladoForaDoGerenciadorEmColunaEstreitaUsaTextoCurto(t *testing.T) {
	tab := loaded(t, externalBackend())
	v := plain(tab.View(80, 19))
	if !strings.Contains(v, "✓ instalado (fora)") || strings.Contains(v, "Homebrew") {
		t.Fatalf("texto curto esperado em 80 colunas:\n%s", v)
	}
	v = plain(tab.View(60, 19))
	if !strings.Contains(v, "✓ instalado (fora)") {
		t.Fatalf("texto curto esperado em 60 colunas:\n%s", v)
	}
}

func TestAppsExternoSemNomeDoGerenciadorUsaTextoCurto(t *testing.T) {
	be := externalBackend()
	be.status["python"] = AppStatus{Installed: true, External: true}
	v := plain(loaded(t, be).View(110, 19))
	if !strings.Contains(v, "✓ instalado (fora)") {
		t.Fatalf("\n%s", v)
	}
}

func TestAppsExternoSanitizaTextoDeFora(t *testing.T) {
	be := externalBackend()
	be.status["python"] = AppStatus{Installed: true, External: true, ExternalManager: "Home\x1b[2Jbrew", ExternalWhere: "/A\x1b]0;x\x07", Version: "1\x1b[0m"}
	v := loaded(t, be).View(110, 19)
	if strings.ContainsAny(v, "\x07") || strings.Contains(v, "\x1b[2J") || strings.Contains(v, "\x1b]0;") {
		t.Fatalf("sequência de terminal crua na tela: %q", v)
	}
}

func TestAppsInstaladoNormalContinuaIgual(t *testing.T) {
	v := plain(loaded(t, externalBackend()).View(110, 19))
	if !strings.Contains(v, "✓ instalado") || !strings.Contains(v, "2.43.0") {
		t.Fatalf("git instalado pelo gerenciador:\n%s", v)
	}
}

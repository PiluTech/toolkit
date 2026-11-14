package tui

import (
	"fmt"
	"strings"
	"testing"
)

const privGoldenPlan = "Perfil: recomendado\n\n" +
	"ID de publicidade desligado\n  ! HKCU\\Software\\Microsoft\\Windows\\CurrentVersion\\AdvertisingInfo\\Enabled: 1 -> 0\n      desfazer: reg add\n" +
	"Dados de diagnóstico no mínimo [exige administrador]\n  ! HKLM\\SOFTWARE\\Policies\\Microsoft\\Windows\\DataCollection\\AllowTelemetry: não definido -> 0\n      desfazer: reg delete\n" +
	"Serviço de telemetria [exige administrador]\n  ! serviço DiagTrack: em execução -> desativado\n      desfazer: sc config\n"

func goldenSizes() [][2]int { return [][2]int{{80, 24}, {120, 40}} }

func TestGoldenConfirmPrivacidadeSemAdmin(t *testing.T) {
	for _, sz := range goldenSizes() {
		be := &fakePriv{info: privSample(false), plan: PrivPlan{Text: privGoldenPlan, Pending: 3, NeedsAdmin: true}}
		tab := privTab(t, be)
		tab.theme = Theme{Mono: true, NoMotion: true}
		pkey(tab, "down", "a")
		m := sized(Options{Version: "v0.1.0", Mono: true, NoMotion: true}, sz[0], sz[1], tab)
		golden(t, fmt.Sprintf("confirm_priv_semadmin_%dx%d", sz[0], sz[1]), m.render())
	}
}

func TestGoldenConfirmPrivacidadeAdmin(t *testing.T) {
	be := &fakePriv{info: privSample(true), plan: PrivPlan{Text: privGoldenPlan, Pending: 3, NeedsAdmin: true, IsAdmin: true}}
	tab := privTab(t, be)
	pkey(tab, "down", "a")
	m := sized(Options{Version: "v0.1.0", Mono: true, NoMotion: true}, 80, 24, tab)
	golden(t, "confirm_priv_admin_80x24", m.render())
}

func TestGoldenConfirmAppsNativos(t *testing.T) {
	text := "Apps nativos do Windows\nEscopo: somente o usuário atual\n\nSerá removido:\n" +
		"  - MSN Clima: Microsoft.BingWeather_4.53.33420.0_x64__8wekyb3d8bbwe\n" +
		"  - Skype: Microsoft.SkypeApp 15.0.0.0\n  - Vínculo com o Celular: Microsoft.YourPhone 1.24.0.0\n"
	for _, sz := range goldenSizes() {
		be := &fakeNat{info: natSample(true), plan: NatPlan{ID: "p", Text: text, Pending: 3, IsAdmin: true}}
		tab := natTab(t, be)
		nkey(tab, "c", "r")
		m := sized(Options{Version: "v0.1.0", Mono: true, NoMotion: true}, sz[0], sz[1], tab)
		golden(t, fmt.Sprintf("confirm_nat_%dx%d", sz[0], sz[1]), m.render())
	}
}

// A confirmação é a única tela da aba enquanto pergunta: a linha de estado traz só a dica de teclas e a tela
// continua com a altura e a largura da janela.
func TestConfirmNaAbaMantemATelaDaJanela(t *testing.T) {
	be := &fakePriv{info: privSample(true), plan: PrivPlan{Text: privGoldenPlan + strings.Repeat("linha extra do plano\n", 40), Pending: 3, IsAdmin: true}}
	tab := privTab(t, be)
	pkey(tab, "a")
	m := sized(Options{Mono: true, NoMotion: true}, 80, 24, tab)
	rows := strings.Split(m.render(), "\n")
	if len(rows) != 24 {
		t.Fatalf("%d linhas", len(rows))
	}
	if st := rows[len(rows)-2]; !strings.Contains(st, "Tab muda o botão · Enter ativa · s confirma · Esc cancela") || strings.Contains(st, "[s/N]") {
		t.Errorf("status: %q", st)
	}
	if !strings.Contains(m.render(), SymMore+" mais") {
		t.Error("plano longo precisa do indicador de linhas abaixo")
	}
}

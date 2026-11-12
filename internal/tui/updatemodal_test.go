package tui

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

func withLatest() Options {
	return Options{Version: "v0.1.0", Latest: "v0.2.0", LatestNotes: "- corrige o X\n- novo Y", LatestURL: "https://github.com/PiluTech/toolkit/releases/tag/v0.2.0", Mono: true}
}

func TestUKeyOpensNotesWithCloseAsDefault(t *testing.T) {
	m := sized(withLatest(), 80, 24, fakes()...)
	press(m, 'u', 0, "u")
	r := m.render()
	for _, want := range []string{"VERSÃO NOVA", "v0.1.0 ↑ v0.2.0", "corrige o X", "novo Y", "releases/tag/v0.2.0", "> [ Fechar ]", "[ Atualizar ]", "toolkit update"} {
		if !strings.Contains(r, want) {
			t.Errorf("modal sem %q:\n%s", want, r)
		}
	}
	// Enter com o foco padrão (Fechar) fecha e NÃO pede atualização
	press(m, tea.KeyEnter, 0, "")
	if m.modal != modalNone || m.UpdateRequested() || m.Quitting() {
		t.Fatalf("Enter em Fechar só fecha: modal=%v req=%v quit=%v", m.modal, m.UpdateRequested(), m.Quitting())
	}
}

func TestUKeyEscClosesAndTabThenEnterRequestsUpdate(t *testing.T) {
	m := sized(withLatest(), 80, 24, fakes()...)
	press(m, 'u', 0, "u")
	press(m, tea.KeyEscape, 0, "")
	if m.modal != modalNone {
		t.Fatal("Esc fecha")
	}
	press(m, 'u', 0, "u")
	press(m, tea.KeyTab, 0, "")
	if !strings.Contains(m.render(), "> [ Atualizar ]") {
		t.Fatalf("o foco foi para Atualizar:\n%s", m.render())
	}
	cmd := press(m, tea.KeyEnter, 0, "")
	if !m.UpdateRequested() || !m.Quitting() || cmd == nil {
		t.Fatal("Enter em Atualizar pede a atualização e sai")
	}
}

func TestUKeyAShortcutRequestsUpdate(t *testing.T) {
	m := sized(withLatest(), 80, 24, fakes()...)
	press(m, 'u', 0, "u")
	press(m, 'a', 0, "a")
	if !m.UpdateRequested() {
		t.Fatal("a = atualizar")
	}
}

func TestUKeyDoesNothingWithoutNewVersion(t *testing.T) {
	m := sized(Options{Version: "v0.1.0", Mono: true}, 80, 24, fakes()...)
	before := m.render()
	press(m, 'u', 0, "u")
	if m.modal != modalNone || m.render() != before {
		t.Fatal("sem versão nova o u não abre nada")
	}
}

func TestUKeyUnavailableDuringRun(t *testing.T) {
	m := sized(withLatest(), 80, 24, fakes()...)
	m.mode = modeRun
	press(m, 'u', 0, "u")
	if m.modal != modalNone {
		t.Fatal("durante a execução o u não abre o modal")
	}
	if !strings.Contains(m.notice, "indisponível durante a execução") {
		t.Fatalf("aviso: %q", m.notice)
	}
	if !strings.Contains(m.render(), "indisponível durante a execução") {
		t.Fatalf("a linha de estado explica:\n%s", m.render())
	}
}

func TestUKeyInsideReviewDoesNothing(t *testing.T) {
	m := sized(withLatest(), 80, 24, fakes()...)
	m.mode, m.review = modeReview, newReview(m.theme, 1, Selection{})
	press(m, 'u', 0, "u")
	if m.modal != modalNone {
		t.Fatal("na Revisão o u não abre o modal")
	}
}

func TestUKeyGoesToSearchWhenTabIsTyping(t *testing.T) {
	tab := &fakeTab{title: "Apps", short: "Apps", body: " x", capturing: true}
	m := sized(withLatest(), 80, 24, tab, &fakeTab{title: "Log", short: "Log"})
	press(m, 'u', 0, "u")
	if m.modal != modalNone || len(tab.got) == 0 {
		t.Fatal("digitando na busca, o u é texto")
	}
}

func TestUpdateModalSanitizesNotesAndFitsWindow(t *testing.T) {
	o := withLatest()
	o.LatestNotes = "linha\x1b[2J\x07 ruim\n" + strings.Repeat("palavra ", 60)
	for _, sz := range [][2]int{{60, 16}, {80, 24}, {120, 40}} {
		m := sized(o, sz[0], sz[1], fakes()...)
		press(m, 'u', 0, "u")
		r := m.render()
		if strings.Contains(r, "\x1b") || strings.Contains(r, "\x07") {
			t.Fatalf("escape nas notas: %q", r)
		}
		if n := len(strings.Split(r, "\n")); n != sz[1] {
			t.Errorf("%v: %d linhas", sz, n)
		}
	}
}

func TestUpdateModalScrollsLongNotes(t *testing.T) {
	o := withLatest()
	var b strings.Builder
	for i := 0; i < 40; i++ {
		b.WriteString("item " + itoa(i) + "\n")
	}
	o.LatestNotes = b.String()
	m := sized(o, 80, 20, fakes()...)
	press(m, 'u', 0, "u")
	if strings.Contains(m.render(), "item 39") {
		t.Fatal("notas longas não cabem de início")
	}
	for i := 0; i < 60; i++ {
		press(m, tea.KeyDown, 0, "")
	}
	if !strings.Contains(m.render(), "item 39") {
		t.Fatalf("rolou até o fim:\n%s", m.render())
	}
}

func TestCtrlCQuitsFromUpdateModal(t *testing.T) {
	m := sized(withLatest(), 80, 24, fakes()...)
	press(m, 'u', 0, "u")
	press(m, 'c', tea.ModCtrl, "")
	if !m.Quitting() || m.UpdateRequested() {
		t.Fatal("Ctrl+C sai sem pedir atualização")
	}
}

func TestLateUpdateInfoFillsBannerOnlyIfEmpty(t *testing.T) {
	m := sized(Options{Version: "v0.1.0", Mono: true}, 80, 24, fakes()...)
	m.Update(UpdateInfoMsg{Tag: "v0.2.0", Notes: "n", URL: "https://x.example/r"})
	if !strings.Contains(m.render(), "↑ v0.2.0 disponível (u)") {
		t.Fatalf("a faixa aparece quando a consulta chega:\n%s", m.render())
	}
	m.Update(UpdateInfoMsg{Tag: "v0.9.9"})
	if !strings.Contains(m.render(), "v0.2.0") || strings.Contains(m.render(), "v0.9.9") {
		t.Fatal("o que já estava na faixa não é trocado")
	}
}

func TestRunDeliversUpdatesAndReturnsErrUpdateRequested(t *testing.T) {
	ch := make(chan UpdateInfo, 1)
	in, w := io.Pipe()
	defer func() { _ = w.Close() }()
	done := make(chan error, 1)
	go func() {
		done <- run(context.Background(), Options{Version: "v0.1.0", Mono: true, Updates: ch},
			[]tea.ProgramOption{tea.WithInput(in), tea.WithOutput(io.Discard), tea.WithWindowSize(80, 24)}, &fakeTab{title: "Apps", short: "Apps"})
	}()
	ch <- UpdateInfo{Tag: "v0.2.0", Notes: "n"}
	time.Sleep(100 * time.Millisecond)
	_, _ = w.Write([]byte("u")) // abre o modal (só existe porque a consulta chegou)
	_, _ = w.Write([]byte("a")) // Atualizar
	select {
	case err := <-done:
		if !errors.Is(err, ErrUpdateRequested) {
			t.Fatalf("err = %v, quero ErrUpdateRequested", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("o Run não voltou")
	}
}

func TestRunWithUpdatesChannelDoesNotLeakOnQuit(t *testing.T) {
	ch := make(chan UpdateInfo) // ninguém envia: a goroutine tem de sair com o ctx
	ctx, cancel := context.WithCancel(context.Background())
	in, _ := io.Pipe()
	done := make(chan error, 1)
	go func() {
		done <- run(ctx, Options{Mono: true, Updates: ch}, []tea.ProgramOption{tea.WithInput(in), tea.WithOutput(io.Discard), tea.WithWindowSize(80, 24)}, &fakeTab{title: "Apps", short: "Apps"})
	}()
	time.Sleep(50 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		t.Logf("Run voltou: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("o Run não voltou")
	}
}

package tui

import (
	"context"
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/PiluTech/toolkit/internal/diagfmt"
	"github.com/PiluTech/toolkit/internal/diagnostico"
)

type fakeMachine struct {
	rep   *diagnostico.Report
	err   error
	calls int
	ctxs  []context.Context
}

func (f *fakeMachine) Load(ctx context.Context) (*diagnostico.Report, error) {
	f.calls++
	f.ctxs = append(f.ctxs, ctx)
	return f.rep, f.err
}

func machineReport() *diagnostico.Report {
	return &diagnostico.Report{
		OS:       "windows",
		Hardware: diagnostico.Hardware{Vendor: "Acme", Model: "X1", Hostname: "PC-DA-SALA", Serial: "SN-12345", UUID: "UUID-AAAA-BBBB"},
		Disks:    []diagnostico.Disk{{Name: "Disco A", Serial: "DISKSERIAL99", SizeBytes: 500e9}},
		NICs:     []diagnostico.NIC{{Name: "Ethernet", MAC: "AA:BB:CC:DD:EE:FF"}},
		Skipped:  []diagnostico.Skipped{{Area: "smart", Reason: "exige administrador", NeedsAdmin: true}},
	}
}

func newDiag(t *testing.T, mb MachineBackend) *DiagTab {
	t.Helper()
	d := NewDiagTab(context.Background(), &fakeRecipes{data: recipeSample()}, mb, Theme{Mono: true}, diagfmt.PT)
	d.Update(recipesLoadedMsg{data: recipeSample()})
	d.rec.lastH = 19
	return d
}

func dpress(d *DiagTab, keys ...string) tea.Cmd {
	var last tea.Cmd
	for _, k := range keys {
		var m tea.KeyPressMsg
		switch k {
		case "esc":
			m = tea.KeyPressMsg{Code: tea.KeyEscape}
		case "enter":
			m = tea.KeyPressMsg{Code: tea.KeyEnter}
		case "down":
			m = tea.KeyPressMsg{Code: tea.KeyDown}
		case "up":
			m = tea.KeyPressMsg{Code: tea.KeyUp}
		default:
			m = tea.KeyPressMsg{Code: rune(k[0]), Text: k}
		}
		_, last = d.Update(m)
	}
	return last
}

// settle executa o comando de leitura e entrega a resposta à aba.
func settle(t *testing.T, d *DiagTab, cmd tea.Cmd) {
	t.Helper()
	if cmd == nil {
		t.Fatal("sem comando")
	}
	for _, m := range dRunBatch(cmd) {
		d.Update(m)
	}
}

func dRunBatch(cmd tea.Cmd) []tea.Msg {
	msg := cmd()
	if b, ok := msg.(tea.BatchMsg); ok {
		var out []tea.Msg
		for _, c := range b {
			if c == nil {
				continue
			}
			if m := c(); m != nil {
				if _, isTick := m.(tickMsg); !isTick {
					out = append(out, m)
				}
			}
		}
		return out
	}
	return []tea.Msg{msg}
}

func TestDiagTabReceitasContinuamEHintDeMaquina(t *testing.T) {
	d := newDiag(t, &fakeMachine{rep: machineReport()})
	v := d.View(100, 19)
	if !strings.Contains(v, "Diagnóstico de memória") || !strings.Contains(v, "Diagnóstico de rede") {
		t.Fatalf("lista de receitas de diagnóstico ausente:\n%s", v)
	}
	var keys []string
	for _, k := range d.Keys() {
		keys = append(keys, k.Key+" "+k.Label)
	}
	if !strings.Contains(strings.Join(keys, "|"), "m máquina") {
		t.Fatalf("falta o atalho m: %v", keys)
	}
	dpress(d, "space")
	if d.Selected() != 1 || len(d.Marked()) != 1 {
		t.Fatalf("marcar receita: selected=%d marked=%d", d.Selected(), len(d.Marked()))
	}
}

func TestDiagTabMostraMaquinaSemMascara(t *testing.T) {
	fm := &fakeMachine{rep: machineReport()}
	d := newDiag(t, fm)
	settle(t, d, dpress(d, "m"))
	if fm.calls != 1 {
		t.Fatalf("calls=%d", fm.calls)
	}
	var all strings.Builder
	for _, l := range d.lines {
		all.WriteString(l + "\n")
	}
	for _, want := range []string{"PC-DA-SALA", "SN-12345", "UUID-AAAA-BBBB", "DISKSERIAL99", "AA:BB:CC:DD:EE:FF", "Com administrador o diagnóstico também lê"} {
		if !strings.Contains(all.String(), want) {
			t.Errorf("falta %q no relatório:\n%s", want, all.String())
		}
	}
	if d.Selected() != 0 {
		t.Fatal("na visão Máquina nada fica marcado")
	}
	// rolando pelos dados com uma janela baixa
	d.View(80, 6)
	if d.More() == 0 {
		t.Fatal("More deveria contar linhas abaixo da dobra")
	}
	before := d.top
	dpress(d, "down", "down")
	if d.top != before+2 {
		t.Fatalf("down não rolou: %d -> %d", before, d.top)
	}
	dpress(d, "G")
	if d.More() != 0 {
		t.Fatalf("G deveria ir ao fim, More=%d", d.More())
	}
	dpress(d, "g")
	if d.top != 0 {
		t.Fatal("g volta ao topo")
	}
}

func TestDiagTabLendoErroERecarregar(t *testing.T) {
	fm := &fakeMachine{err: errors.New("falhou\x1b[31m feio")}
	d := newDiag(t, fm)
	cmd := dpress(d, "m")
	if v := d.View(80, 10); !strings.Contains(v, "Lendo a máquina") {
		t.Fatalf("estado lendo:\n%s", v)
	}
	settle(t, d, cmd)
	v := d.View(80, 10)
	if !strings.Contains(v, "não deu para ler a máquina") || strings.Contains(v, "\x1b") {
		t.Fatalf("estado de erro:\n%q", v)
	}
	fm.err, fm.rep = nil, machineReport()
	settle(t, d, dpress(d, "c"))
	if fm.calls != 2 || !strings.Contains(d.View(100, 40), "SN-12345") {
		t.Fatalf("recarregar: calls=%d", fm.calls)
	}
	settle(t, d, dpress(d, "enter"))
	if fm.calls != 3 {
		t.Fatalf("enter recarrega: calls=%d", fm.calls)
	}
}

func TestDiagTabEscCancelaELogoVolta(t *testing.T) {
	fm := &fakeMachine{rep: machineReport()}
	d := newDiag(t, fm)
	cmd := dpress(d, "m")
	dpress(d, "esc")
	if d.machine {
		t.Fatal("esc deveria voltar às receitas")
	}
	if fm.calls != 0 { // o comando ainda não rodou
		t.Fatal("inesperado")
	}
	msgs := dRunBatch(cmd)
	if fm.ctxs[0].Err() == nil {
		t.Fatal("o contexto da leitura deveria estar cancelado")
	}
	for _, m := range msgs {
		d.Update(m)
	}
	if d.loaded {
		t.Fatal("resposta de leitura cancelada deve ser descartada")
	}
	dpress(d, "m")
	dpress(d, "m")
	if d.machine {
		t.Fatal("m na visão Máquina volta às receitas")
	}
}

func TestDiagTabBuscaDasReceitasRecebeM(t *testing.T) {
	d := newDiag(t, &fakeMachine{rep: machineReport()})
	dpress(d, "/")
	if !d.Capturing() {
		t.Fatal("busca captura")
	}
	dpress(d, "m")
	if d.machine || d.rec.filter != "m" {
		t.Fatalf("m na busca é texto: machine=%v filter=%q", d.machine, d.rec.filter)
	}
}

func TestDiagTabLimpaTextoDoSistemaEEstreito(t *testing.T) {
	rep := machineReport()
	rep.Hardware.Hostname = "pc\x1b]0;pwn\x07x"
	rep.Hardware.Model = strings.Repeat("M", 300)
	d := newDiag(t, &fakeMachine{rep: rep})
	settle(t, d, dpress(d, "m"))
	for _, w := range []int{80, 40, 20} {
		v := d.View(w, 12)
		if strings.ContainsAny(v, "\x1b\x07") {
			t.Fatalf("sequência de controle vazou: %q", v)
		}
		for _, l := range strings.Split(v, "\n") {
			if lipgloss.Width(l) > w {
				t.Fatalf("linha passa de %d colunas (%d): %q", w, lipgloss.Width(l), l)
			}
		}
	}
}

func TestDiagTabVazioAntesDeLer(t *testing.T) {
	d := newDiag(t, &fakeMachine{})
	d.machine = true
	if v := d.View(80, 5); !strings.Contains(v, "Tecle c") {
		t.Fatalf("estado inicial: %q", v)
	}
	if d.More() != 0 || d.Status() != "" {
		t.Fatal("sem relatório não há o que rolar")
	}
}

// Pela raiz, como o Paulo usa: aba Diagnóstico, o atalho m aparece na barra (80 colunas), m abre a máquina e Enter recarrega.
func TestDiagTabPelaRaiz(t *testing.T) {
	fm := &fakeMachine{rep: machineReport()}
	d := newDiag(t, fm)
	m := sized(Options{Mono: true}, 80, 24, &fakeTab{title: "Apps", short: "Apps"}, &fakeTab{title: "Receitas", short: "Rec"}, d, &fakeTab{title: "Log", short: "Log"})
	press(m, '3', 0, "3")
	v := ansi.Strip(m.View().Content)
	if !strings.Contains(v, "m máquina") || !strings.Contains(v, "Enter revisar") {
		t.Fatalf("barra de teclas em 80 colunas:\n%s", v)
	}
	cmd := press(m, 'm', 0, "m")
	for _, msg := range dRunBatch(cmd) {
		m.Update(msg)
	}
	v = ansi.Strip(m.View().Content)
	for _, want := range []string{"SN-12345", "UUID-AAAA-BBBB", "PC-DA-SALA", "Com administrador o diagnóstico também lê"} {
		if !strings.Contains(v, want) {
			t.Fatalf("falta %q na primeira página:\n%s", want, v)
		}
	}
	seen := v
	for range 10 { // rolando pelas páginas até achar os dados de rede e de disco
		_, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyPgDown})
		seen += ansi.Strip(m.View().Content)
	}
	for _, want := range []string{"AA:BB:CC:DD:EE:FF", "DISKSERIAL99"} {
		if !strings.Contains(seen, want) {
			t.Fatalf("falta %q rolando:\n%s", want, seen)
		}
	}
	t.Log("\n" + v)
}

// A identificação da máquina (nome, série, UUID, MAC) vem ANTES dos achados: o Paulo a quer na primeira tela.
func TestDiagTabIdentificacaoNoTopo(t *testing.T) {
	rep := &diagnostico.Report{OS: "linux",
		Hardware: diagnostico.Hardware{Hostname: "PC-TOPO", Serial: "SN-TOPO", UUID: "UUID-TOPO"},
		NICs:     []diagnostico.NIC{{Name: "eth0", MAC: "02:00:00:00:00:09"}}}
	tab := &DiagTab{lang: diagnostico.Lang(0)}
	lines := tab.render(rep)
	head := strings.Join(lines[:min(8, len(lines))], "\n")
	for _, want := range []string{"PC-TOPO", "SN-TOPO", "UUID-TOPO", "02:00:00:00:00:09"} {
		if !strings.Contains(head, want) {
			t.Errorf("faltou %q no topo:\n%s", want, head)
		}
	}
}

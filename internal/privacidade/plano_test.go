package privacidade

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/PiluTech/toolkit/internal/diagfmt"
)

const (
	kAdv   = `HKCU\Software\Microsoft\Windows\CurrentVersion\AdvertisingInfo`
	kTele  = `HKLM\SOFTWARE\Policies\Microsoft\Windows\DataCollection`
	kAct   = `HKLM\SOFTWARE\Policies\Microsoft\Windows\System`
	nTele  = "AllowTelemetry"
	nAdv   = "Enabled"
	taskAp = `\Microsoft\Windows\Application Experience\Microsoft Compatibility Appraiser`
)

func change(t *testing.T, p *Plan, target string) Change {
	t.Helper()
	for _, c := range p.Changes {
		if c.Target == target {
			return c
		}
	}
	t.Fatalf("mudança %q não está no plano", target)
	return Change{}
}

func TestBuildMostraAntesEDepois(t *testing.T) {
	f := newFake(EditionPro, false)
	f.regs[rkey(kAdv, nAdv)] = DWORD(1)
	p, err := Build(context.Background(), f, PerfilBasico, diagfmt.PT)
	if err != nil {
		t.Fatal(err)
	}
	c := change(t, p, kAdv+`\`+nAdv)
	if !c.Pending || c.Before != "1" || c.After != "0" || c.Admin || c.Kind != "reg" {
		t.Errorf("mudança: %+v", c)
	}
	if want := `reg add "` + kAdv + `" /v Enabled /t REG_DWORD /d 1 /f`; c.Undo != want {
		t.Errorf("undo = %q", c.Undo)
	}
	// valor ausente: "não definido" e o desfazer apaga
	c2 := change(t, p, `HKCU\Software\Microsoft\Windows\CurrentVersion\Privacy\TailoredExperiencesWithDiagnosticDataEnabled`)
	if !c2.Pending || c2.Before != "não definido" || !strings.HasPrefix(c2.Undo, "reg delete") {
		t.Errorf("ausente: %+v", c2)
	}
	if p.Edition != EditionPro || p.Perfil != PerfilBasico || p.IsAdmin {
		t.Errorf("cabeçalho do plano: %+v", p)
	}
}

func TestBuildNaoMuda(t *testing.T) {
	f := newFake(EditionPro, true).withAllServicesAndTasks()
	if _, err := Build(context.Background(), f, PerfilRecomendado, diagfmt.PT); err != nil {
		t.Fatal(err)
	}
	if len(f.writes()) != 0 || len(f.regs) != 0 {
		t.Errorf("Build mexeu no sistema: %v", f.writes())
	}
}

func TestBuildItensJaNoValorFicamNaoPendentes(t *testing.T) {
	f := newFake(EditionHome, true).withAllServicesAndTasks()
	// DiagTrack já desativado e parado; uma tarefa já desabilitada; AllowTelemetry já em 1
	f.svcs["DiagTrack"] = &ServiceState{Exists: true, StartType: "disabled"}
	f.tasks[taskAp].Enabled = false
	f.regs[rkey(kTele, nTele)] = DWORD(1)
	f.regs[rkey(kAdv, nAdv)] = DWORD(0)
	p, err := Build(context.Background(), f, PerfilRecomendado, diagfmt.PT)
	if err != nil {
		t.Fatal(err)
	}
	for _, target := range []string{kTele + `\` + nTele, kAdv + `\` + nAdv, "serviço DiagTrack", "tarefa " + taskAp} {
		if c := change(t, p, target); c.Pending {
			t.Errorf("%s deveria estar fora das pendências", target)
		}
	}
	if !change(t, p, "tarefa "+`\Microsoft\Windows\Autochk\Proxy`).Pending {
		t.Error("tarefa habilitada deveria estar pendente")
	}
	out := p.Format(diagfmt.PT)
	if strings.Contains(out, "! "+kAdv) || !strings.Contains(out, "✓ "+kAdv+`\Enabled: 0 (já está assim)`) {
		t.Errorf("Format não separa o que já está certo:\n%s", out)
	}
	if p.Pending() == len(p.Changes) || p.Pending() == 0 {
		t.Errorf("Pending = %d de %d", p.Pending(), len(p.Changes))
	}
}

func TestBuildServicoRodandoMasDesativadoContinuaPendente(t *testing.T) {
	f := newFake(EditionPro, true).withAllServicesAndTasks()
	f.svcs["DiagTrack"] = &ServiceState{Exists: true, StartType: "disabled", Running: true}
	p, _ := Build(context.Background(), f, PerfilRecomendado, diagfmt.PT)
	c := change(t, p, "serviço DiagTrack")
	if !c.Pending || c.Before != "disabled, em execução" || c.After != "disabled, parado" {
		t.Errorf("%+v", c)
	}
}

func TestBuildAllowTelemetryPorEdicao(t *testing.T) {
	for ed, want := range map[string]string{EditionHome: "1", EditionPro: "1", EditionEnterprise: "0", EditionEducation: "0"} {
		f := newFake(ed, true).withAllServicesAndTasks()
		p, err := Build(context.Background(), f, PerfilRecomendado, diagfmt.PT)
		if err != nil {
			t.Fatal(err)
		}
		if c := change(t, p, kTele+`\`+nTele); c.After != want {
			t.Errorf("%s: depois=%s, esperado %s", ed, c.After, want)
		}
	}
}

func TestBuildPerfilBasicoNaoTemAdmin(t *testing.T) {
	f := newFake(EditionPro, false).withAllServicesAndTasks()
	p, err := Build(context.Background(), f, PerfilBasico, diagfmt.PT)
	if err != nil {
		t.Fatal(err)
	}
	if p.NeedsAdmin() {
		t.Error("básico não deveria exigir administrador")
	}
	for _, c := range p.Changes {
		if c.Admin || c.Kind != "reg" || isHKLM(c.Target) {
			t.Errorf("mudança de admin no básico: %+v", c)
		}
	}
}

func TestBuildRecomendadoMarcaAdmin(t *testing.T) {
	f := newFake(EditionPro, false).withAllServicesAndTasks()
	p, _ := Build(context.Background(), f, PerfilRecomendado, diagfmt.PT)
	if !p.NeedsAdmin() {
		t.Fatal("recomendado deveria exigir administrador")
	}
	for _, c := range p.Changes {
		if c.Admin != (c.Kind != "reg" || isHKLM(c.Target)) {
			t.Errorf("flag Admin errada: %+v", c)
		}
	}
	// item não pendente não exige administrador
	if (&Plan{Changes: []Change{{Admin: true, Pending: false}}}).NeedsAdmin() {
		t.Error("item não pendente não deveria exigir administrador")
	}
}

func TestBuildServicoETarefaAusentesViramMissing(t *testing.T) {
	f := newFake(EditionPro, true) // sem serviços nem tarefas
	p, err := Build(context.Background(), f, PerfilRecomendado, diagfmt.PT)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range p.Changes {
		if c.Kind != "reg" {
			t.Errorf("serviço/tarefa inexistente virou mudança: %+v", c)
		}
	}
	if len(p.Missing) != 1+7 {
		t.Errorf("Missing = %d: %v", len(p.Missing), p.Missing)
	}
	out := p.Format(diagfmt.PT)
	if !strings.Contains(out, "Não existe nesta versão do Windows") || !strings.Contains(out, "serviço DiagTrack") {
		t.Errorf("Format sem os ausentes:\n%s", out)
	}
}

type edErrSys struct{ *fakeSys }

func (edErrSys) Edition(context.Context) (string, error) { return "", errors.New("wmi quebrado") }

func TestBuildEdicaoDesconhecidaEhErro(t *testing.T) {
	_, err := Build(context.Background(), edErrSys{newFake(EditionPro, true)}, PerfilBasico, diagfmt.PT)
	if err == nil || !strings.Contains(err.Error(), "edição") || !strings.Contains(err.Error(), "wmi quebrado") {
		t.Errorf("err = %v", err)
	}
}

func TestBuildEdicaoOutraUsaMinimoObrigatorio(t *testing.T) {
	f := newFake(EditionOther, true).withAllServicesAndTasks()
	p, _ := Build(context.Background(), f, PerfilRecomendado, diagfmt.PT)
	if c := change(t, p, kTele+`\`+nTele); c.After != "1" {
		t.Errorf("edição outra: %s", c.After)
	}
	if !strings.Contains(p.Format(diagfmt.PT), "Edição do Windows: outra") {
		t.Error("nome da edição")
	}
}

type regErrSys struct{ *fakeSys }

func (regErrSys) RegRead(context.Context, string, string) (RegValue, bool, error) {
	return RegValue{}, false, errors.New("acesso negado")
}

func TestBuildErroDeLeituraPropaga(t *testing.T) {
	_, err := Build(context.Background(), regErrSys{newFake(EditionPro, true)}, PerfilBasico, diagfmt.PT)
	if err == nil || !strings.Contains(err.Error(), "acesso negado") {
		t.Errorf("err = %v", err)
	}
}

func TestFormatMostraTudoAntes(t *testing.T) {
	f := newFake(EditionPro, false).withAllServicesAndTasks()
	p, _ := Build(context.Background(), f, PerfilRecomendado, diagfmt.PT)
	out := p.Format(diagfmt.PT)
	for _, want := range []string{
		"Perfil: recomendado", "Edição do Windows: Pro", "Nota: no Home e no Pro",
		"[exige administrador]", "! " + kTele + `\AllowTelemetry: não definido -> 1`, "desfazer: reg delete",
		"! serviço DiagTrack: automatic, em execução -> disabled, parado", "desfazer: sc config DiagTrack start= auto & sc start DiagTrack",
		"! tarefa " + taskAp + ": habilitada -> desabilitada", `desfazer: schtasks /Change /TN "` + taskAp + `" /ENABLE`,
		"mudança(s) pendente(s)", "para aplicar abra o terminal como administrador",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("Format sem %q:\n%s", want, out)
		}
	}
	// todo ajuste do perfil aparece com título
	for _, s := range ParaPerfil(PerfilRecomendado) {
		if !strings.Contains(out, s.Title(diagfmt.PT)) {
			t.Errorf("ajuste %s ausente do Format", s.ID)
		}
	}
	// admin: sem o aviso de abrir como administrador
	f.admin = true
	p2, _ := Build(context.Background(), f, PerfilRecomendado, diagfmt.PT)
	if strings.Contains(p2.Format(diagfmt.PT), "abra o terminal como administrador") {
		t.Error("aviso de administrador mesmo com administrador")
	}
}

func TestFormatBasicoSemNotaDeEnterprise(t *testing.T) {
	f := newFake(EditionEnterprise, true).withAllServicesAndTasks()
	p, _ := Build(context.Background(), f, PerfilRecomendado, diagfmt.EN)
	out := p.Format(diagfmt.EN)
	if strings.Contains(out, "Note: on Home and Pro") || !strings.Contains(out, "Windows edition: Enterprise") || !strings.Contains(out, "pending change(s)") {
		t.Errorf("EN/Enterprise:\n%s", out)
	}
}

func TestDescribeETemplatesDeDesfazer(t *testing.T) {
	if got := describeReg(RegValue{Kind: RegString, Str: "abc"}, true, diagfmt.PT); got != `"abc"` {
		t.Error(got)
	}
	if got := regUndo("HKCU\\X", "N", RegValue{Kind: RegString, Str: "abc"}, true); got != `reg add "HKCU\X" /v N /t REG_SZ /d "abc" /f` {
		t.Error(got)
	}
	for in, want := range map[string]string{"automatic": "auto", "manual": "demand", "disabled": "disabled", "delayed-auto": "delayed-auto"} {
		if startTypeSC(in) != want {
			t.Errorf("startTypeSC(%s)", in)
		}
	}
	if got := serviceUndo("X", ServiceState{StartType: "manual"}); got != "sc config X start= demand" {
		t.Error(got)
	}
	if got := taskUndo(`\a\b`, false); got != `schtasks /Change /TN "\a\b" /DISABLE` {
		t.Error(got)
	}
}

func TestRegUndoEDescribePreservamREG_EXPAND_SZ(t *testing.T) {
	v := RegValue{Kind: RegExpandString, Str: `%USERPROFILE%\x`}
	if got := regUndo(`HKCU\K`, "N", v, true); !strings.Contains(got, "/t REG_EXPAND_SZ") || strings.Contains(got, "REG_DWORD") {
		t.Errorf("regUndo: %s", got)
	}
	if got := describeReg(v, true, diagfmt.PT); got != `"%USERPROFILE%\x"` {
		t.Errorf("describeReg: %s", got)
	}
}

package privacidade

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/PiluTech/toolkit/internal/diagfmt"
	"github.com/PiluTech/toolkit/internal/exec"
)

var t0 = time.Date(2026, 11, 14, 10, 0, 0, 0, time.UTC)

func buildApply(t *testing.T, f *fakeSys, st Store, p Perfil) (*Plan, *Result, error) {
	t.Helper()
	plan, err := Build(context.Background(), f, p, diagfmt.PT)
	if err != nil {
		t.Fatal(err)
	}
	res, err := Apply(context.Background(), f, st, plan, t0)
	return plan, res, err
}

func TestApplyBasicoGravaBackupAntesDeMudar(t *testing.T) {
	f := newFake(EditionPro, false)
	f.regs[rkey(kAdv, nAdv)] = DWORD(1)
	st := &memStore{}
	var savesAtFirstWrite = -1
	var backupAtFirstWrite *Backup
	f.onWrite = func() {
		if savesAtFirstWrite < 0 {
			savesAtFirstWrite = st.saves
			backupAtFirstWrite, _ = st.Load()
		}
	}
	_, res, err := buildApply(t, f, st, PerfilBasico)
	if err != nil {
		t.Fatal(err)
	}
	if savesAtFirstWrite != 1 || backupAtFirstWrite == nil {
		t.Fatalf("o backup tinha de estar salvo antes da primeira mudança (saves=%d)", savesAtFirstWrite)
	}
	// o backup já cobria TUDO o que seria mudado
	if len(backupAtFirstWrite.Entries) != len(res.Applied) {
		t.Errorf("backup antes: %d entradas, aplicadas: %d", len(backupAtFirstWrite.Entries), len(res.Applied))
	}
	e := backupAtFirstWrite.Entries[regID(kAdv, nAdv)]
	if !e.Existed || e.DWORD != 1 || e.Kind != "reg" {
		t.Errorf("entrada do ID de publicidade: %+v", e)
	}
	if v, _ := f.regValue(kAdv, nAdv); v != DWORD(0) {
		t.Errorf("valor final = %+v", v)
	}
	if res.BackupPath != st.Path() || res.AlreadyOK != 0 || st.b.Version != 1 || !st.b.Created.Equal(t0) {
		t.Errorf("resultado/backup: %+v %+v", res, st.b)
	}
}

func TestApplyRecomendadoMudaTudo(t *testing.T) {
	f := newFake(EditionPro, true).withAllServicesAndTasks()
	st := &memStore{}
	plan, res, err := buildApply(t, f, st, PerfilRecomendado)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Applied) != plan.Pending() {
		t.Errorf("aplicadas %d de %d", len(res.Applied), plan.Pending())
	}
	if v, _ := f.regValue(kTele, nTele); v != DWORD(1) {
		t.Errorf("AllowTelemetry = %+v", v)
	}
	if s := f.svcs["DiagTrack"]; s.StartType != "disabled" || s.Running {
		t.Errorf("DiagTrack: %+v", s)
	}
	if f.tasks[taskAp].Enabled {
		t.Error("tarefa ainda habilitada")
	}
	if len(st.b.Entries) != len(plan.Changes) {
		t.Errorf("backup com %d entradas para %d mudanças", len(st.b.Entries), len(plan.Changes))
	}
	if ids := sortedIDs(st.b); len(ids) != len(plan.Changes) || ids[0] != "reg:"+kAdv+"|"+nAdv && !strings.HasPrefix(ids[0], "reg:") {
		t.Errorf("ids do backup: %v", ids)
	}
	svc := st.b.Entries["svc:DiagTrack"]
	if svc.StartType != "automatic" || !svc.Running || svc.Service != "DiagTrack" {
		t.Errorf("backup do serviço: %+v", svc)
	}
	if tk := st.b.Entries["task:"+taskAp]; !tk.Enabled || tk.Task != taskAp {
		t.Errorf("backup da tarefa: %+v", tk)
	}
	if reg := st.b.Entries[regID(kTele, nTele)]; reg.Existed {
		t.Errorf("AllowTelemetry não existia: %+v", reg)
	}
	// segunda rodada: nada pendente, nada de escrita
	n := len(f.writes())
	plan2, res2, err := buildApply(t, f, st, PerfilRecomendado)
	if err != nil || len(res2.Applied) != 0 || res2.AlreadyOK != len(plan2.Changes) || len(f.writes()) != n {
		t.Errorf("reaplicar: %+v %v writes %d->%d", res2, err, n, len(f.writes()))
	}
}

func TestApplyPrimeiroOriginalVence(t *testing.T) {
	f := newFake(EditionPro, false)
	f.regs[rkey(kAdv, nAdv)] = DWORD(1)
	st := &memStore{}
	if _, _, err := buildApply(t, f, st, PerfilBasico); err != nil {
		t.Fatal(err)
	}
	first := st.b.Entries[regID(kAdv, nAdv)]
	// o usuário (ou o Windows) volta o valor para 7 e reaplica: o backup NÃO pode virar 7
	f.regs[rkey(kAdv, nAdv)] = DWORD(7)
	if _, _, err := buildApply(t, f, st, PerfilBasico); err != nil {
		t.Fatal(err)
	}
	if got := st.b.Entries[regID(kAdv, nAdv)]; got != first || got.DWORD != 1 {
		t.Errorf("backup sobrescrito: %+v (antes %+v)", got, first)
	}
	if !st.b.Created.Equal(t0) {
		t.Error("Created mudou")
	}
}

func TestApplySemAdminRecusaAntesDeTudo(t *testing.T) {
	f := newFake(EditionPro, false).withAllServicesAndTasks()
	st := &memStore{}
	_, res, err := buildApply(t, f, st, PerfilRecomendado)
	if !errors.Is(err, ErrNeedsAdmin) {
		t.Fatalf("err = %v", err)
	}
	// nem os itens HKCU do perfil foram mudados, nem backup gravado
	if len(f.writes()) != 0 || st.saves != 0 || st.b != nil || len(res.Applied) != 0 {
		t.Errorf("tocou em algo: writes=%v saves=%d", f.writes(), st.saves)
	}
}

func TestApplySemAdminMasSoHKCUFunciona(t *testing.T) {
	f := newFake(EditionPro, false).withAllServicesAndTasks()
	st := &memStore{}
	if _, _, err := buildApply(t, f, st, PerfilBasico); err != nil {
		t.Fatal(err)
	}
}

func TestApplyAdminSomentePendenteImporta(t *testing.T) {
	// recomendado, mas os itens de admin já estão certos: só HKCU pendente, sem admin: funciona
	f := newFake(EditionPro, false).withAllServicesAndTasks()
	for _, s := range Settings() {
		if !s.Admin {
			continue
		}
		for _, r := range s.Regs {
			f.regs[rkey(r.Key, r.Name)] = r.Value(EditionPro)
		}
		for _, sv := range s.Svcs {
			f.svcs[sv.Name] = &ServiceState{Exists: true, StartType: "disabled"}
		}
		for _, tk := range s.Tasks {
			f.tasks[tk.Path].Enabled = false
		}
	}
	st := &memStore{}
	_, res, err := buildApply(t, f, st, PerfilRecomendado)
	if err != nil || len(res.Applied) == 0 {
		t.Fatalf("err=%v res=%+v", err, res)
	}
}

func TestApplyDryRunNaoEscreveNada(t *testing.T) {
	// dry-run = Build + Format sem Apply: nem sistema nem backup mudam
	f := newFake(EditionPro, true).withAllServicesAndTasks()
	st := &memStore{}
	plan, err := Build(context.Background(), f, PerfilRecomendado, diagfmt.PT)
	if err != nil {
		t.Fatal(err)
	}
	_ = plan.Format(diagfmt.PT)
	if len(f.writes()) != 0 || st.saves != 0 || st.b != nil {
		t.Error("dry-run escreveu algo")
	}
}

func TestApplyPlanoVazioNaoGravaBackup(t *testing.T) {
	f := newFake(EditionPro, true)
	st := &memStore{}
	res, err := Apply(context.Background(), f, st, &Plan{Changes: []Change{{Pending: false}}}, t0)
	if err != nil || res.AlreadyOK != 1 || st.saves != 0 {
		t.Errorf("res=%+v err=%v saves=%d", res, err, st.saves)
	}
}

func TestApplyBackupNaoGravaNaoMudaNada(t *testing.T) {
	f := newFake(EditionPro, false)
	st := &memStore{failSave: true}
	_, res, err := buildApply(t, f, st, PerfilBasico)
	if err == nil || !strings.Contains(err.Error(), "nada foi alterado") || !strings.Contains(err.Error(), "disco cheio") {
		t.Fatalf("err = %v", err)
	}
	if len(f.writes()) != 0 || len(res.Applied) != 0 {
		t.Errorf("mudou sem backup: %v", f.writes())
	}
}

type loadErrStore struct{ memStore }

func (*loadErrStore) Load() (*Backup, error) { return nil, errors.New("backup ilegível") }

func TestApplyBackupIlegivelNaoMudaNada(t *testing.T) {
	f := newFake(EditionPro, false)
	plan, _ := Build(context.Background(), f, PerfilBasico, diagfmt.PT)
	_, err := Apply(context.Background(), f, &loadErrStore{}, plan, t0)
	if err == nil || len(f.writes()) != 0 {
		t.Errorf("err=%v writes=%v", err, f.writes())
	}
}

func TestApplyErroNoMeioParaEBackupCobreTudo(t *testing.T) {
	f := newFake(EditionPro, true).withAllServicesAndTasks()
	f.failOn = "svc-stop:DiagTrack"
	st := &memStore{}
	plan, res, err := buildApply(t, f, st, PerfilRecomendado)
	if err == nil || !strings.Contains(err.Error(), "serviço DiagTrack") || !strings.Contains(err.Error(), "falha simulada") {
		t.Fatalf("err = %v", err)
	}
	// parou: as tarefas, que vêm depois, não foram tocadas
	if !f.tasks[taskAp].Enabled {
		t.Error("tarefa mudou depois da falha")
	}
	// o que veio antes foi aplicado e está em res.Applied
	if v, _ := f.regValue(kTele, nTele); v != DWORD(1) {
		t.Error("AllowTelemetry deveria ter sido aplicado antes da falha")
	}
	if len(res.Applied) == 0 || len(res.Applied) >= plan.Pending() {
		t.Errorf("Applied = %d de %d", len(res.Applied), plan.Pending())
	}
	// o backup cobre tudo o que mudou (inclusive o serviço que falhou no meio: configure passou, stop não)
	for _, c := range res.Applied {
		if _, ok := st.b.Entries[c.ID]; !ok {
			t.Errorf("%s mudou sem backup", c.ID)
		}
	}
	if e := st.b.Entries["svc:DiagTrack"]; e.StartType != "automatic" || !e.Running {
		t.Errorf("backup do serviço parcial: %+v", e)
	}
	// e dá para desfazer o que sobrou
	f.failOn = ""
	up, bk, err := BuildUndo(context.Background(), f, st, diagfmt.PT)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ApplyUndo(context.Background(), f, st, up, bk); err != nil {
		t.Fatal(err)
	}
	if s := f.svcs["DiagTrack"]; s.StartType != "automatic" {
		t.Errorf("desfazer não restaurou o serviço: %+v", s)
	}
}

func TestApplyElevacaoRecusadaNoMeioViraErro(t *testing.T) {
	// IsAdmin diz true mas o sistema recusa (ex.: token mudou): o erro propaga e o backup existe
	f := newFake(EditionPro, true).withAllServicesAndTasks()
	st := &memStore{}
	plan, _ := Build(context.Background(), f, PerfilRecomendado, diagfmt.PT)
	f.admin = false
	sys := adminLiar{f}
	_, err := Apply(context.Background(), sys, st, plan, t0)
	if !errors.Is(err, exec.ErrNotElevated) || st.b == nil {
		t.Errorf("err=%v backup=%v", err, st.b)
	}
}

type adminLiar struct{ *fakeSys }

func (adminLiar) IsAdmin(context.Context) bool { return true }

// ---------- desfazer ----------

func TestUndoRestauraValorAnteriorEAusente(t *testing.T) {
	f := newFake(EditionPro, true).withAllServicesAndTasks()
	f.regs[rkey(kAdv, nAdv)] = DWORD(1)
	f.regs[rkey(kAct, "EnableActivityFeed")] = RegValue{Kind: RegString, Str: "talvez"}
	st := &memStore{}
	if _, _, err := buildApply(t, f, st, PerfilRecomendado); err != nil {
		t.Fatal(err)
	}
	if v, _ := f.regValue(kAdv, nAdv); v != DWORD(0) {
		t.Fatal("pré-condição")
	}
	plan, bk, err := BuildUndo(context.Background(), f, st, diagfmt.PT)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Perfil != "desfazer" || plan.Pending() != len(plan.Changes) {
		t.Errorf("plano de desfazer: %+v", plan)
	}
	ch := change(t, plan, kAdv+`\`+nAdv)
	if ch.Before != "0" || ch.After != "1" {
		t.Errorf("%+v", ch)
	}
	if out := plan.Format(diagfmt.PT); !strings.Contains(out, "Restaurar o estado anterior") || !strings.Contains(out, "[exige administrador]") {
		t.Error(out)
	}
	res, err := ApplyUndo(context.Background(), f, st, plan, bk)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Applied) != len(plan.Changes) {
		t.Errorf("aplicadas %d de %d", len(res.Applied), len(plan.Changes))
	}
	if v, ok := f.regValue(kAdv, nAdv); !ok || v != DWORD(1) {
		t.Errorf("valor anterior não voltou: %+v %v", v, ok)
	}
	if v, ok := f.regValue(kAct, "EnableActivityFeed"); !ok || v != (RegValue{Kind: RegString, Str: "talvez"}) {
		t.Errorf("string anterior não voltou: %+v %v", v, ok)
	}
	// AllowTelemetry NÃO existia: a chave é apagada
	if _, ok := f.regValue(kTele, nTele); ok {
		t.Error("valor que não existia antes deveria ter sido apagado")
	}
	if !hasWrite(f, "reg-delete:"+kTele+`\`+nTele) {
		t.Errorf("faltou reg-delete: %v", f.writes())
	}
	// serviço e tarefa
	for _, n := range []string{"DiagTrack"} {
		if s := f.svcs[n]; s.StartType != "automatic" || !s.Running {
			t.Errorf("%s: %+v", n, s)
		}
	}
	if !f.tasks[taskAp].Enabled {
		t.Error("tarefa não voltou a habilitada")
	}
	// backup apagado depois de restaurar tudo
	if st.b != nil {
		t.Error("backup deveria ter sido removido")
	}
}

func hasWrite(f *fakeSys, w string) bool {
	for _, x := range f.writes() {
		if x == w {
			return true
		}
	}
	return false
}

func TestUndoServicoQueEstavaParadoOuManual(t *testing.T) {
	f := newFake(EditionPro, true).withAllServicesAndTasks()
	f.svcs["DiagTrack"] = &ServiceState{Exists: true, StartType: "manual", Running: false}
	f.tasks[taskAp].Enabled = false // já desabilitada na origem: não vira pendente nem entra no backup
	st := &memStore{}
	if _, _, err := buildApply(t, f, st, PerfilRecomendado); err != nil {
		t.Fatal(err)
	}
	if _, ok := st.b.Entries["task:"+taskAp]; ok {
		t.Error("tarefa que já estava certa não deveria ir ao backup")
	}
	f.log = nil
	plan, bk, _ := BuildUndo(context.Background(), f, st, diagfmt.PT)
	if _, err := ApplyUndo(context.Background(), f, st, plan, bk); err != nil {
		t.Fatal(err)
	}
	if s := f.svcs["DiagTrack"]; s.StartType != "manual" || s.Running {
		t.Errorf("DiagTrack deveria voltar manual e parado: %+v", s)
	}
	if hasWrite(f, "svc-start:DiagTrack") {
		t.Error("não deveria ter iniciado um serviço que estava parado")
	}
	if f.tasks[taskAp].Enabled {
		t.Error("tarefa que estava desabilitada ficou habilitada")
	}
}

func TestUndoServicoQueEstavaEmExecucaoETiposDeInicio(t *testing.T) {
	f := newFake(EditionPro, true)
	f.svcs["DiagTrack"] = &ServiceState{Exists: true, StartType: "delayed-auto", Running: true}
	st := &memStore{b: &Backup{Version: 1, Entries: map[string]Entry{
		"svc:DiagTrack": {ID: "svc:DiagTrack", Kind: "service", Service: "DiagTrack", StartType: "delayed-auto", Running: true},
	}}}
	// serviço parado e desativado agora
	f.svcs["DiagTrack"] = &ServiceState{Exists: true, StartType: "disabled"}
	plan, bk, err := BuildUndo(context.Background(), f, st, diagfmt.PT)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ApplyUndo(context.Background(), f, st, plan, bk); err != nil {
		t.Fatal(err)
	}
	if s := f.svcs["DiagTrack"]; s.StartType != "delayed-auto" || !s.Running {
		t.Errorf("%+v", s)
	}
}

func TestUndoSemBackup(t *testing.T) {
	f := newFake(EditionPro, true)
	if _, _, err := BuildUndo(context.Background(), f, &memStore{}, diagfmt.PT); !errors.Is(err, ErrNoBackup) {
		t.Errorf("sem arquivo: %v", err)
	}
	if _, _, err := BuildUndo(context.Background(), f, &memStore{b: &Backup{Version: 1, Entries: map[string]Entry{}}}, diagfmt.PT); !errors.Is(err, ErrNoBackup) {
		t.Errorf("backup vazio: %v", err)
	}
}

func TestUndoItemQueJaEstaComoAntesNaoEhPendente(t *testing.T) {
	f := newFake(EditionPro, true)
	f.regs[rkey(kAdv, nAdv)] = DWORD(1)
	st := &memStore{b: &Backup{Version: 1, Entries: map[string]Entry{
		regID(kAdv, nAdv):                 {ID: regID(kAdv, nAdv), Kind: "reg", Key: kAdv, Name: nAdv, Existed: true, DWORD: 1},
		regID(kAct, "EnableActivityFeed"): {ID: regID(kAct, "EnableActivityFeed"), Kind: "reg", Key: kAct, Name: "EnableActivityFeed", Existed: false},
	}}}
	plan, bk, err := BuildUndo(context.Background(), f, st, diagfmt.PT)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Pending() != 0 {
		t.Fatalf("nada pendente esperado: %+v", plan.Changes)
	}
	res, err := ApplyUndo(context.Background(), f, st, plan, bk)
	if err != nil || res.AlreadyOK != 2 || len(f.writes()) != 0 || st.b != nil {
		t.Errorf("res=%+v err=%v writes=%v backup=%v", res, err, f.writes(), st.b)
	}
}

func TestUndoServicoETarefaAusentesSaoIgnorados(t *testing.T) {
	f := newFake(EditionPro, true) // nada existe
	st := &memStore{b: &Backup{Version: 1, Entries: map[string]Entry{
		"svc:DiagTrack":  {ID: "svc:DiagTrack", Kind: "service", Service: "DiagTrack", StartType: "automatic", Running: true},
		"task:" + taskAp: {ID: "task:" + taskAp, Kind: "task", Task: taskAp, Enabled: true},
	}}}
	plan, _, err := BuildUndo(context.Background(), f, st, diagfmt.PT)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Changes) != 0 || len(plan.Missing) != 2 {
		t.Errorf("%+v", plan)
	}
}

func TestUndoSemAdminRecusaAntesDeTudo(t *testing.T) {
	f := newFake(EditionPro, true).withAllServicesAndTasks()
	st := &memStore{}
	if _, _, err := buildApply(t, f, st, PerfilRecomendado); err != nil {
		t.Fatal(err)
	}
	f.admin = false
	f.log = nil
	plan, bk, err := BuildUndo(context.Background(), f, st, diagfmt.PT)
	if err != nil {
		t.Fatal(err)
	}
	_, err = ApplyUndo(context.Background(), f, st, plan, bk)
	if !errors.Is(err, ErrNeedsAdmin) || len(f.writes()) != 0 || st.b == nil {
		t.Errorf("err=%v writes=%v backup=%v", err, f.writes(), st.b)
	}
}

func TestUndoErroNoMeioMantemSoOQueFalta(t *testing.T) {
	f := newFake(EditionPro, true).withAllServicesAndTasks()
	st := &memStore{}
	if _, _, err := buildApply(t, f, st, PerfilRecomendado); err != nil {
		t.Fatal(err)
	}
	total := len(st.b.Entries)
	f.failOn = "svc-config:DiagTrack"
	f.log = nil
	plan, bk, _ := BuildUndo(context.Background(), f, st, diagfmt.PT)
	res, err := ApplyUndo(context.Background(), f, st, plan, bk)
	if err == nil || !strings.Contains(err.Error(), "serviço DiagTrack") {
		t.Fatalf("err = %v", err)
	}
	if st.b == nil || len(st.b.Entries) != total-len(res.Applied) || len(res.Applied) == 0 {
		t.Fatalf("backup restante: %d (total %d, desfeitas %d)", len(st.b.Entries), total, len(res.Applied))
	}
	if _, ok := st.b.Entries["svc:DiagTrack"]; !ok {
		t.Error("o item que falhou tem de continuar no backup")
	}
	for _, c := range res.Applied {
		if _, ok := st.b.Entries[c.ID]; ok {
			t.Errorf("%s já foi restaurado e ainda está no backup", c.ID)
		}
	}
	// nova tentativa termina o serviço
	f.failOn = ""
	plan, bk, err = BuildUndo(context.Background(), f, st, diagfmt.PT)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ApplyUndo(context.Background(), f, st, plan, bk); err != nil || st.b != nil {
		t.Errorf("segunda tentativa: %v backup=%v", err, st.b)
	}
	if s := f.svcs["DiagTrack"]; s.StartType != "automatic" || !s.Running {
		t.Errorf("%+v", s)
	}
}

func TestUndoRoundTripComoTudoEraAntes(t *testing.T) {
	f := newFake(EditionEnterprise, true).withAllServicesAndTasks()
	f.regs[rkey(kTele, nTele)] = DWORD(3)
	f.regs[rkey(kAdv, nAdv)] = DWORD(1)
	snapRegs := map[string]RegValue{}
	for k, v := range f.regs {
		snapRegs[k] = v
	}
	st := &memStore{}
	if _, _, err := buildApply(t, f, st, PerfilRecomendado); err != nil {
		t.Fatal(err)
	}
	plan, bk, _ := BuildUndo(context.Background(), f, st, diagfmt.PT)
	if _, err := ApplyUndo(context.Background(), f, st, plan, bk); err != nil {
		t.Fatal(err)
	}
	if len(f.regs) != len(snapRegs) {
		t.Errorf("registro: %d valores, esperado %d", len(f.regs), len(snapRegs))
	}
	for k, v := range snapRegs {
		if f.regs[k] != v {
			t.Errorf("%s = %+v, era %+v", k, f.regs[k], v)
		}
	}
}

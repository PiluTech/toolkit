package usb

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestAplicarOrdemObrigatoria(t *testing.T) {
	f := newFake(t)
	st := newSpy(f)
	// no instante da PRIMEIRA chave, o backup já existe e a tarefa já está armada E conferida
	var firstWriteChecked bool
	f.hook = func(op string) {
		if !strings.HasPrefix(op, "reg-write:") || firstWriteChecked {
			return
		}
		firstWriteChecked = true
		if sn, err := st.Load(); err != nil || sn == nil {
			t.Errorf("a primeira chave foi escrita sem backup válido (%v)", err)
		}
		if !f.task.Exists || !f.task.Enabled {
			t.Error("a primeira chave foi escrita sem a tarefa de reversão armada")
		}
	}
	res := mustApply(t, f, st)
	if !firstWriteChecked {
		t.Fatal("nenhuma chave foi escrita")
	}
	order := []string{"protect-dir", "backup-save", "stage-binary", "task-arm:10:" + RevertArgs, "reg-write:AllowDenyLayered"}
	last := -1
	for _, op := range order {
		i := f.first(op)
		if i < 0 || i <= last {
			t.Fatalf("ordem errada em %q (índice %d, anterior %d): %v", op, i, last, f.log)
		}
		last = i
	}
	if f.first("reg-write:DenyDeviceClasses") < f.first("reg-write:AllowDeviceIDs") {
		t.Fatalf("a negação foi gravada antes da liberação: %v", f.log)
	}
	if !res.Deadline.Equal(t0.Add(10 * time.Minute)) {
		t.Fatalf("prazo = %v", res.Deadline)
	}
	if !f.tree.KeyExists || f.tree.Entries["AllowDenyLayered"].DWORD != 1 {
		t.Fatalf("política não gravada: %v", treeKeys(f.tree))
	}
}

func TestAplicarFalhaAntesDaPrimeiraChaveNaoEscreveNada(t *testing.T) {
	type tc struct {
		name  string
		setup func(f *fakeSys, st *spyStore)
	}
	cases := []tc{
		{"leitura do PnP falha", func(f *fakeSys, _ *spyStore) { f.devsErr = errors.New("powershell falhou") }},
		{"arvore incompleta", func(f *fakeSys, _ *spyStore) { f.devs = f.devs[1:] }},
		{"teclado desmarcado", func(*fakeSys, *spyStore) {}}, // tratado abaixo via Excluded
		{"pasta protegida falha", func(f *fakeSys, _ *spyStore) { f.failOn = "protect-dir" }},
		{"backup falha", func(_ *fakeSys, s *spyStore) { s.failSav = true }},
		{"copia do binario falha", func(f *fakeSys, _ *spyStore) { f.failOn = "stage-binary" }},
		{"hash da copia diverge", func(f *fakeSys, _ *spyStore) { f.staged.CopySHA = strings.Repeat("b", 64) }},
		{"armar a tarefa falha", func(f *fakeSys, _ *spyStore) { f.failOn = "task-arm" }},
		{"tarefa desabilitada", func(f *fakeSys, _ *spyStore) { f.armSkew = func(ti *TaskInfo) { ti.Enabled = false } }},
		{"comando da tarefa diferente", func(f *fakeSys, _ *spyStore) { f.armSkew = func(ti *TaskInfo) { ti.Args = "usb desfazer --yes" } }},
		{"binario da tarefa diferente", func(f *fakeSys, _ *spyStore) { f.armSkew = func(ti *TaskInfo) { ti.Exe = `C:\Users\Public\x.exe` } }},
		{"tarefa nao e do SYSTEM", func(f *fakeSys, _ *spyStore) { f.armSkew = func(ti *TaskInfo) { ti.User = "BUILTIN\\Users" } }},
		{"proximo disparo no passado", func(f *fakeSys, _ *spyStore) { f.armSkew = func(ti *TaskInfo) { ti.Next = t0.Add(-time.Minute) } }},
		{"proximo disparo zerado", func(f *fakeSys, _ *spyStore) { f.armSkew = func(ti *TaskInfo) { ti.Next = time.Time{} } }},
		{"proximo disparo alem do prazo", func(f *fakeSys, _ *spyStore) { f.armSkew = func(ti *TaskInfo) { ti.Next = t0.Add(3 * time.Hour) } }},
		{"tarefa some apos armar", func(f *fakeSys, _ *spyStore) { f.armSkew = func(ti *TaskInfo) { *ti = TaskInfo{} } }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newFake(t)
			st := newSpy(f)
			c.setup(f, st)
			opts := applyOpts(f)
			if c.name == "teclado desmarcado" {
				opts.Excluded = []string{`USB\VID_FFFF&PID_0001\6&9999&0&1`}
			}
			_, err := Apply(context.Background(), f, st, opts)
			if err == nil {
				t.Fatal("Apply devia falhar")
			}
			if n := f.writes(); n != 0 {
				t.Fatalf("ZERO alterações no registro esperadas, houve %d: %v", n, f.log)
			}
			if f.task.Exists {
				t.Error("a tarefa criada foi deixada para trás (zero alterações inclui a tarefa)")
			}
			if sn, _ := st.Load(); sn != nil {
				t.Error("o backup deste Apply foi deixado para trás")
			}
		})
	}
}

func TestAplicarSemProvaDizOMotivo(t *testing.T) {
	f := newFake(t)
	opts := applyOpts(f)
	opts.Excluded = []string{`USB\VID_FFFF&PID_0001\6&9999&0&1`}
	_, err := Apply(context.Background(), f, newSpy(f), opts)
	if !errors.Is(err, ErrNoProof) {
		t.Fatalf("err = %v, queria ErrNoProof", err)
	}
	if !strings.Contains(err.Error(), "Teclado") {
		t.Fatalf("a mensagem não diz qual dispositivo: %v", err)
	}
}

func TestAplicarSemAdminRecusaAntesDeTudo(t *testing.T) {
	f := newFake(t)
	f.admin = false
	_, err := Apply(context.Background(), f, newSpy(f), applyOpts(f))
	if !errors.Is(err, ErrNeedsAdmin) {
		t.Fatalf("err = %v", err)
	}
	if len(f.log) != 0 || f.protectCalls != 0 {
		t.Fatalf("nada podia ser tentado sem administrador: %v", f.log)
	}
}

func TestAplicarEdicoes(t *testing.T) {
	for ed, ok := range map[string]bool{EditionPro: true, EditionEnterprise: true, EditionEducation: true, EditionHome: false, EditionOther: false, "": false} {
		f := newFake(t)
		f.edition = ed
		_, err := Apply(context.Background(), f, newSpy(f), applyOpts(f))
		if ok && err != nil {
			t.Errorf("%q deveria aplicar: %v", ed, err)
		}
		if !ok {
			if !errors.Is(err, ErrEdition) {
				t.Errorf("%q: err = %v, queria ErrEdition", ed, err)
			}
			if len(f.log) != 0 {
				t.Errorf("%q: mudou algo antes de recusar: %v", ed, f.log)
			}
		}
	}
}

func TestAplicarPrazo(t *testing.T) {
	for p, ok := range map[int]bool{-1: false, 0: false, 1: false, 2: true, 10: true, 60: true, 61: false} {
		f := newFake(t)
		opts := applyOpts(f)
		opts.Prazo = p
		_, err := Apply(context.Background(), f, newSpy(f), opts)
		if ok != (err == nil) {
			t.Errorf("prazo %d: err = %v", p, err)
		}
		if !ok && (!errors.Is(err, ErrPrazo) || len(f.log) != 0) {
			t.Errorf("prazo %d: %v / %v", p, err, f.log)
		}
	}
}

func TestAplicarRecusaPoliticaExistente(t *testing.T) {
	cases := map[string]func(f *fakeSys){
		"valor de outra origem": func(f *fakeSys) {
			f.tree = RegTree{KeyExists: true, Entries: map[string]RegValue{"DenyUnspecified": DWORD(1)}}
		},
		"lista de IDs de outra origem": func(f *fakeSys) {
			f.tree = RegTree{KeyExists: true, Subkeys: []string{"AllowDeviceIDs"}, Entries: map[string]RegValue{`AllowDeviceIDs\1`: String(`USB\VID_AAAA&PID_0001`)}}
		},
		"colide com valor do toolkit": func(f *fakeSys) {
			f.tree = RegTree{KeyExists: true, Entries: map[string]RegValue{"AllowDenyLayered": DWORD(0)}}
		},
		"subchave vazia de outra origem": func(f *fakeSys) {
			f.tree = RegTree{KeyExists: true, Subkeys: []string{"DenyDeviceIDs"}, Entries: map[string]RegValue{}}
		},
	}
	for name, setup := range cases {
		f := newFake(t)
		setup(f)
		_, err := Apply(context.Background(), f, newSpy(f), applyOpts(f))
		if !errors.Is(err, ErrExistingPolicy) {
			t.Errorf("%s: err = %v, queria ErrExistingPolicy", name, err)
		}
		if f.writes() != 0 || f.task.Exists {
			t.Errorf("%s: mudou algo: %v", name, f.log)
		}
	}
}

func TestAplicarAceitaChaveRestrictionsVazia(t *testing.T) {
	f := newFake(t)
	f.tree = RegTree{KeyExists: true, Entries: map[string]RegValue{}}
	st := newSpy(f)
	mustApply(t, f, st)
	sn, _ := st.Load()
	if sn == nil || !sn.KeyExisted {
		t.Fatalf("o snapshot tem de registrar que a chave JÁ existia (vazia): %#v", sn)
	}
	if f.first("export-reg") < 0 {
		t.Error("com a chave existente, o .reg de segurança tem de ser exportado")
	}
	if sn.RegSHA256 == "" {
		t.Error("o hash do .reg não foi gravado")
	}
}

func TestAplicarDuasVezesRecusa(t *testing.T) {
	f := newFake(t)
	st := newSpy(f)
	mustApply(t, f, st)
	n := len(f.log)
	_, err := Apply(context.Background(), f, st, applyOpts(f))
	if !errors.Is(err, ErrExistingPolicy) {
		t.Fatalf("segunda aplicação: %v", err)
	}
	if len(f.log) != n {
		t.Fatalf("a segunda aplicação mexeu em algo: %v", f.log[n:])
	}
}

func TestAplicarFalhaNoMeioRestauraTudoELimpa(t *testing.T) {
	f := newFake(t)
	st := newSpy(f)
	f.failOn = "reg-write:DenyDeviceClasses\\2"
	res, err := Apply(context.Background(), f, st, applyOpts(f))
	if err == nil {
		t.Fatal("devia falhar")
	}
	if res == nil || !res.Restored {
		t.Fatalf("o Apply devia tentar a restauração imediata: %#v", res)
	}
	if len(f.tree.Entries) != 0 || f.tree.KeyExists || len(f.tree.Subkeys) != 0 {
		t.Fatalf("a restauração imediata não devolveu o registro ao original: %v %v", treeKeys(f.tree), f.tree.Subkeys)
	}
	if f.task.Exists {
		t.Error("com o registro restaurado a tarefa não precisa ficar")
	}
	if sn, _ := st.Load(); sn != nil {
		t.Error("o backup deste Apply não deveria ficar com o registro já restaurado")
	}
}

func TestAplicarFalhaNoMeioSeARestauracaoFalhaTarefaEBackupFicamDeReserva(t *testing.T) {
	f := newFake(t)
	st := newSpy(f)
	f.failOn = "reg-write:DenyDeviceClasses\\2"
	f.failAfter = "reg-delete:" // a restauração imediata também falha
	res, err := Apply(context.Background(), f, st, applyOpts(f))
	if err == nil || res == nil || res.Restored {
		t.Fatalf("res=%#v err=%v", res, err)
	}
	if !f.task.Exists {
		t.Error("a tarefa de reversão tem de ficar armada como reserva")
	}
	if sn, _ := st.Load(); sn == nil {
		t.Error("o backup tem de ficar: é o que a reserva restaura")
	}
}

func TestAplicarReleituraPosAcaoDetectaEscritaPerdida(t *testing.T) {
	f := newFake(t)
	f.dropWrite = "AllowDenyLayered" // o reg.exe "funcionou" mas o valor não ficou
	res, err := Apply(context.Background(), f, newSpy(f), applyOpts(f))
	if err == nil {
		t.Fatal("a releitura devia notar que a camada não foi gravada")
	}
	if res == nil || !res.Restored {
		t.Fatalf("devia restaurar: %#v", res)
	}
	if len(f.tree.Entries) != 0 {
		t.Fatalf("sobrou: %v", treeKeys(f.tree))
	}
}

func TestPlanoDeterministicoEListaOsDispositivos(t *testing.T) {
	f := newFake(t)
	p1, err := BuildPlan(context.Background(), f, nil)
	if err != nil {
		t.Fatal(err)
	}
	p2, _ := BuildPlan(context.Background(), f, nil)
	if p1.Format() != p2.Format() {
		t.Fatal("mesma entrada, planos diferentes")
	}
	txt := p1.Format()
	for _, want := range []string{"Teclado USB composto", "FFFF:0001", "Pendrive USB", "FFFF:0003", "conectado agora", "EXPERIMENTAL", "reduz o risco, não o elimina", "Teclado PS/2 padrão", "Disco NVMe"} {
		if !strings.Contains(txt, want) {
			t.Errorf("plano sem %q:\n%s", want, txt)
		}
	}
	if !p1.OK() {
		t.Fatalf("plano deveria estar OK: %v", p1.Refusals)
	}
	if f.writes() != 0 || len(f.log) != 0 {
		t.Fatalf("o plano só lê: %v", f.log)
	}
}

func TestPlanoSemAdminFunciona(t *testing.T) {
	f := newFake(t)
	f.admin = false
	p, err := BuildPlan(context.Background(), f, nil)
	if err != nil || !p.OK() {
		t.Fatalf("plano sem admin: %v %v", err, p)
	}
	if !strings.Contains(p.Format(), "administrador") {
		t.Error("o plano deve avisar que aplicar exige administrador")
	}
}

func TestPlanoRecusaHomeEPoliticaExistente(t *testing.T) {
	f := newFake(t)
	f.edition = EditionHome
	p, err := BuildPlan(context.Background(), f, nil)
	if err != nil {
		t.Fatal(err)
	}
	if p.OK() || !strings.Contains(strings.Join(p.Refusals, " "), "edição") {
		t.Fatalf("Home devia ser recusada: %v", p.Refusals)
	}
	f = newFake(t)
	f.tree = RegTree{KeyExists: true, Entries: map[string]RegValue{"DenyUnspecified": DWORD(1)}}
	p, _ = BuildPlan(context.Background(), f, nil)
	if p.OK() {
		t.Fatal("política existente devia ser recusada no plano")
	}
}

func TestPlanoTecladoDesmarcadoMostraARecusa(t *testing.T) {
	f := newFake(t)
	p, err := BuildPlan(context.Background(), f, []string{`USB\VID_FFFF&PID_0001\6&9999&0&1`})
	if err != nil {
		t.Fatal(err)
	}
	if p.OK() {
		t.Fatal("teclado desmarcado: o plano não pode estar OK")
	}
	if !strings.Contains(p.Format(), "RECUSADO") {
		t.Errorf("o plano devia dizer RECUSADO:\n%s", p.Format())
	}
}

func TestPlanoNaoContemClasseAmplaNemDenyUnspecified(t *testing.T) {
	f := newFake(t)
	p, _ := BuildPlan(context.Background(), f, nil)
	txt := strings.ToLower(p.Format())
	for _, bad := range []string{"denyunspecified", guidKeyboard, guidMouse, guidHID, guidDisk} {
		if strings.Contains(txt, bad) {
			t.Errorf("o plano menciona %q", bad)
		}
	}
	if !slices.Contains(p.Policy.DenyClasses, denyClassGUIDs[0]) {
		t.Error("as classes de USB faltam")
	}
}

func TestAplicarComPlanoMostradoDiferenteDoRecalculadoRecusaAntesDoBackup(t *testing.T) {
	f := newFake(t)
	st := newSpy(f)
	shown, err := BuildPlan(context.Background(), f, nil)
	if err != nil {
		t.Fatal(err)
	}
	opts := applyOpts(f)
	opts.ExpectedPlan = shown.Format()
	// o plano mostrado foi calculado com o Prazo padrão (10) e o Apply com o mesmo: confere
	if _, err := Apply(context.Background(), f, st, opts); err != nil {
		t.Fatalf("plano idêntico deve passar: %v", err)
	}

	// um pendrive entra durante a pergunta: o plano recalculado é outro
	f2 := newFake(t)
	st2 := newSpy(f2)
	shown2, _ := BuildPlan(context.Background(), f2, nil)
	f2.devs = machine(usbKeyboard("0001")) // o pendrive saiu
	o2 := applyOpts(f2)
	o2.ExpectedPlan = shown2.Format()
	if _, err := Apply(context.Background(), f2, st2, o2); !errors.Is(err, ErrPlanChanged) {
		t.Fatalf("err = %v, queria ErrPlanChanged", err)
	}
	if len(f2.log) != 0 || f2.task.Exists {
		t.Fatalf("nenhum passo (nem backup, pasta ou tarefa) antes de recusar: %v", f2.log)
	}

	// o prazo diferente também muda o plano mostrado
	f3 := newFake(t)
	shown3, _ := BuildPlan(context.Background(), f3, nil)
	o3 := applyOpts(f3)
	o3.Prazo = 5
	o3.ExpectedPlan = shown3.Format() // mostrado com 10 min, aplicado com 5
	if _, err := Apply(context.Background(), f3, newSpy(f3), o3); !errors.Is(err, ErrPlanChanged) {
		t.Fatalf("prazo diferente: %v", err)
	}
}

// O relógio real carrega a leitura monotônica; o backup relido (do JSON) não. A comparação do relido não pode abortar todo
// aplicar de verdade só por isso.
func TestAplicarComRelogioRealEFileStore(t *testing.T) {
	f := newFake(t)
	f.now = time.Now()
	st := f.fileStore()
	opts := ApplyOptions{Prazo: PrazoPadrao, Now: time.Now()}
	if _, err := Apply(context.Background(), f, st, opts); err != nil {
		t.Fatalf("Apply com time.Now(): %v", err)
	}
	if sn, err := st.Load(); err != nil || sn == nil {
		t.Fatalf("snapshot: %v %v", sn, err)
	}
}

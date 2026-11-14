package cli

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/PiluTech/toolkit/internal/privacidade"
	"github.com/PiluTech/toolkit/internal/tui"
)

// Lupa #85: com --yes não há pergunta, mas ainda existe a janela entre a leitura que monta o plano mostrado e a de
// revalidação. Estes testes mudam o sistema ou o backup ENTRE as duas leituras (hook no System/Store falso).

func mustStale(t *testing.T, err error) {
	t.Helper()
	if err == nil || !strings.Contains(err.Error(), staleMsg) {
		t.Fatalf("esperava %q, veio err=%v", staleMsg, err)
	}
}

func TestPrivacidadeAplicarYesRecusaSeEstadoMudaEntreLeituras(t *testing.T) {
	sys, st := newPrivSys(true), &privStore{}
	sys.onEdition = func(call int) {
		if call == 2 { // início da releitura de revalidação
			sys.regs[pk(advKey, "Enabled")] = privacidade.DWORD(1)
		}
	}
	_, err := runPriv(t, privDeps(t, sys, st, "windows", false, ""), "aplicar", "--perfil", "basico", "--yes")
	mustStale(t, err)
	if sys.writes != 0 || st.b != nil {
		t.Errorf("zero mutações: writes=%d backup=%v", sys.writes, st.b)
	}
	if v := sys.regs[pk(advKey, "Enabled")]; v.DWORD != 1 {
		t.Errorf("valor do outro processo perdido: %+v", v)
	}
}

func TestPrivacidadeDesfazerYesRecusaSeBackupOuEstadoMudaEntreLeituras(t *testing.T) {
	cases := map[string]func(sys *privSys, st *privStore){
		"estado": func(sys *privSys, _ *privStore) { sys.svcs["DiagTrack"].StartType = "manual" },
		"backup": func(_ *privSys, st *privStore) { // a Edition #1 vem depois do Load do plano: a releitura já vê a mudança
			for k, e := range st.b.Entries {
				if e.Kind == "reg" {
					e.Existed = !e.Existed
					st.b.Entries[k] = e
					return
				}
			}
		},
	}
	for name, mut := range cases {
		t.Run(name, func(t *testing.T) {
			sys, st := newPrivSys(true), &privStore{}
			if _, err := runPriv(t, privDeps(t, sys, st, "windows", false, ""), "aplicar", "--perfil", "recomendado", "--yes"); err != nil {
				t.Fatal(err)
			}
			w := sys.writes
			sys.editions = 0
			at := map[bool]int{true: 2, false: 1}[name == "estado"]
			sys.onEdition = func(call int) {
				if call == at {
					mut(sys, st)
				}
			}
			_, err := runPriv(t, privDeps(t, sys, st, "windows", false, ""), "desfazer", "--yes")
			mustStale(t, err)
			if sys.writes != w || st.b == nil {
				t.Errorf("nada pode mudar: writes=%d backup=%v", sys.writes-w, st.b)
			}
		})
	}
}

func TestAnimacoesDesfazerYesRecusaSeBackupOuEstadoMudaEntreLeituras(t *testing.T) {
	cases := map[string]func(sys *animSys, st *privStore){
		"estado": func(sys *animSys, _ *privStore) { sys.set(animStr("5")) },
		"backup": func(_ *animSys, st *privStore) {
			for id, e := range st.b.Entries {
				e.Existed, e.Str = false, ""
				st.b.Entries[id] = e
			}
		},
	}
	for name, mut := range cases {
		t.Run(name, func(t *testing.T) {
			sys, st := animApplied(t)
			before, _ := json.Marshal(st.b)
			w := sys.writes
			sys.afterRead = func() { mut(sys, st) } // depois da 1ª leitura (a do plano mostrado)
			_, err := runAnim(t, animDeps(t, sys, st, "windows", false, ""), "animacoes", "desfazer", "--yes")
			mustStale(t, err)
			after, _ := json.Marshal(st.b)
			if sys.writes != w || st.b == nil || (name == "estado" && string(before) != string(after)) {
				t.Errorf("nada pode mudar: writes=%d backup=%v", sys.writes-w, st.b)
			}
		})
	}
}

func TestAppsNativosRemoverYesRecusaSeEstadoMudaEntreLeituras(t *testing.T) {
	sys, st := newNatSys(false, pkgWeather), &natStore{}
	sys.onList = func(call int) {
		if call == 2 { // início da releitura de revalidação
			sys.installed[0].Version = "9.9.9.0"
			sys.installed[0].PackageFullName = pkgWeather + "_9.9.9.0_x64__8wekyb3d8bbwe"
		}
	}
	_, err := runNat(t, natDeps(t, sys, st, "windows", false, ""), "remover", "--app", "bing-weather", "--yes")
	mustStale(t, err)
	if len(sys.removes) != 0 || st.b != nil {
		t.Errorf("nada pode mudar: removes=%v backup=%v", sys.removes, st.b)
	}
}

// ---- desfazer com ZERO pendências: a remoção do backup também só vem depois da revalidação ----

// privZeroStore: backup com uma entrada de registro que não existia e continua não existindo (zero pendências).
func privZeroStore() (*privSys, *privStore) {
	e := privacidade.Entry{ID: "reg:" + advKey + "|Enabled", Kind: "reg", Key: advKey, Name: "Enabled"}
	return newPrivSys(false), &privStore{b: &privacidade.Backup{Version: 1, Entries: map[string]privacidade.Entry{e.ID: e}}}
}

func TestPrivacidadeDesfazerZeroPendenciasRevalidaAntesDeRemover(t *testing.T) {
	cases := map[string]func(sys *privSys, st *privStore){
		"estado": func(sys *privSys, _ *privStore) { sys.regs[pk(advKey, "Enabled")] = privacidade.DWORD(1) },
		"backup": func(_ *privSys, st *privStore) {
			for k, e := range st.b.Entries {
				e.Existed = true
				st.b.Entries[k] = e
			}
		},
		"sumiu": func(_ *privSys, st *privStore) { st.b = nil },
	}
	for name, mut := range cases {
		for _, yes := range []bool{true, false} {
			t.Run(name+map[bool]string{true: "/yes", false: "/pergunta"}[yes], func(t *testing.T) {
				sys, st := privZeroStore()
				orig, _ := json.Marshal(st.b)
				at := map[bool]int{true: 2, false: 1}[name == "estado"]
				sys.onEdition = func(call int) {
					if call == at {
						mut(sys, st)
					}
				}
				deps, args := privDeps(t, sys, st, "windows", !yes, "s\n"), []string{"desfazer"}
				if yes {
					args = append(args, "--yes")
				}
				_, err := runPriv(t, deps, args...)
				mustStale(t, err)
				if name == "sumiu" {
					return
				}
				if st.b == nil {
					t.Fatal("o backup foi removido sem revalidar")
				}
				if name == "estado" {
					if cur, _ := json.Marshal(st.b); string(cur) != string(orig) {
						t.Errorf("backup alterado:\n%s\n%s", orig, cur)
					}
				}
			})
		}
	}
}

func TestPrivacidadeDesfazerZeroPendenciasPerguntaERemove(t *testing.T) {
	sys, st := privZeroStore()
	out, err := runPriv(t, privDeps(t, sys, st, "windows", true, "s\n"), "desfazer")
	if err != nil || st.b != nil {
		t.Fatalf("err=%v backup=%v\n%s", err, st.b, out)
	}
	sys, st = privZeroStore()
	if _, err = runPriv(t, privDeps(t, sys, st, "windows", true, "n\n"), "desfazer"); err != nil || st.b == nil {
		t.Errorf("recusa da pergunta não remove: err=%v backup=%v", err, st.b)
	}
	sys, st = privZeroStore()
	if _, err = runPriv(t, privDeps(t, sys, st, "windows", false, ""), "desfazer"); err == nil || st.b == nil {
		t.Errorf("sem terminal e sem --yes exige --yes e preserva o backup: err=%v", err)
	}
}

// animZero: backup e estado iguais (zero pendências).
func animZero(t *testing.T) (*animSys, *privStore) {
	t.Helper()
	sys, st := animApplied(t)
	sys.set(animStr("1")) // volta ao valor original: nada a restaurar
	return sys, st
}

func TestAnimacoesDesfazerZeroPendenciasRevalidaAntesDeRemover(t *testing.T) {
	cases := map[string]func(sys *animSys, st *privStore){
		"estado": func(sys *animSys, _ *privStore) { sys.set(animStr("5")) },
		"backup": func(_ *animSys, st *privStore) {
			for id, e := range st.b.Entries {
				e.Existed, e.Str = false, ""
				st.b.Entries[id] = e
			}
		},
		"sumiu": func(_ *animSys, st *privStore) { st.b = nil },
	}
	for name, mut := range cases {
		for _, yes := range []bool{true, false} {
			t.Run(name+map[bool]string{true: "/yes", false: "/pergunta"}[yes], func(t *testing.T) {
				sys, st := animZero(t)
				orig, _ := json.Marshal(st.b)
				sys.afterRead = func() { mut(sys, st) }
				d, args := animDeps(t, sys, st, "windows", !yes, "s\n"), []string{"animacoes", "desfazer"}
				if yes {
					args = append(args, "--yes")
				}
				_, err := runAnim(t, d, args...)
				mustStale(t, err)
				if name == "sumiu" {
					return
				}
				if st.b == nil {
					t.Fatal("o backup foi removido sem revalidar")
				}
				if name == "estado" {
					if cur, _ := json.Marshal(st.b); string(cur) != string(orig) {
						t.Errorf("backup alterado:\n%s\n%s", orig, cur)
					}
				}
			})
		}
	}
}

func TestAnimacoesDesfazerZeroPendenciasPerguntaERemove(t *testing.T) {
	sys, st := animZero(t)
	if _, err := runAnim(t, animDeps(t, sys, st, "windows", true, "s\n"), "animacoes", "desfazer"); err != nil || st.b != nil {
		t.Fatalf("err=%v backup=%v", err, st.b)
	}
	sys, st = animZero(t)
	if _, err := runAnim(t, animDeps(t, sys, st, "windows", true, "n\n"), "animacoes", "desfazer"); err != nil || st.b == nil {
		t.Errorf("recusa da pergunta não remove: err=%v backup=%v", err, st.b)
	}
	sys, st = animZero(t)
	if _, err := runAnim(t, animDeps(t, sys, st, "windows", false, ""), "animacoes", "desfazer"); err == nil || st.b == nil {
		t.Errorf("sem terminal e sem --yes exige --yes e preserva o backup: err=%v", err)
	}
	sys, st = animZero(t)
	if _, err := runAnim(t, animDeps(t, sys, st, "windows", false, ""), "animacoes", "desfazer", "--dry-run"); err != nil || st.b == nil {
		t.Errorf("--dry-run nunca remove: err=%v backup=%v", err, st.b)
	}
}

// ---- TUI: o plano de desfazer por ID já revalida; zero pendências não é exceção ----

func TestPrivacyBackendUndoZeroPendenciasRevalida(t *testing.T) {
	ctx := context.Background()
	for _, name := range []string{"estado", "backup"} {
		t.Run(name, func(t *testing.T) {
			sys, st := privZeroStore()
			b := privBackend(t, sys, st, "windows")
			if up, err := b.UndoPlan(ctx); err != nil || up.Pending != 0 {
				t.Fatalf("%+v %v", up, err)
			}
			id := b.lastID()
			if name == "estado" {
				sys.regs[pk(advKey, "Enabled")] = privacidade.DWORD(1)
			} else {
				for k, e := range st.b.Entries {
					e.Existed = true
					st.b.Entries[k] = e
				}
			}
			if _, err := b.Undo(ctx, id); !errors.Is(err, tui.ErrPrivStalePlan) || st.b == nil {
				t.Fatalf("err=%v backup=%v", err, st.b)
			}
		})
	}
	sys, st := privZeroStore() // caminho feliz
	b := privBackend(t, sys, st, "windows")
	if _, err := b.UndoPlan(ctx); err != nil {
		t.Fatal(err)
	}
	if res, err := b.Undo(ctx, b.lastID()); err != nil || !res.Nothing || st.b != nil {
		t.Fatalf("res=%+v err=%v backup=%v", res, err, st.b)
	}
}

func TestAnimBackendUndoZeroPendenciasRevalida(t *testing.T) {
	ctx := context.Background()
	for _, name := range []string{"estado", "backup"} {
		t.Run(name, func(t *testing.T) {
			sys, st := animZero(t)
			b := newAnimBackend(t, sys, st, "windows")
			up, err := b.UndoPlan(ctx)
			if err != nil || up.Pending != 0 {
				t.Fatalf("%+v %v", up, err)
			}
			if name == "estado" {
				sys.set(animStr("5"))
			} else {
				st.b.Created = st.b.Created.AddDate(0, 0, 1)
			}
			if _, err := b.Undo(ctx, up.ID); !errors.Is(err, tui.ErrAnimStalePlan) || st.b == nil {
				t.Fatalf("err=%v backup=%v", err, st.b)
			}
		})
	}
	sys, st := animZero(t)
	b := newAnimBackend(t, sys, st, "windows")
	up, _ := b.UndoPlan(ctx)
	if res, err := b.Undo(ctx, up.ID); err != nil || !res.Nothing || st.b != nil {
		t.Fatalf("res=%+v err=%v backup=%v", res, err, st.b)
	}
}

// Lupa #88: a saída NUNCA diz que o backup foi removido antes de Remove() retornar nil: cancelar, sem terminal, estado ou
// backup alterado e erro de Remove() deixam o backup e a saída não afirma que apagou; no sucesso a frase vem DEPOIS da pergunta.
func TestDesfazerZeroPendenciasSoAnunciaRemocaoDepoisDeRemover(t *testing.T) {
	type cli struct {
		name string
		run  func(t *testing.T, tty bool, input string, hook func(), removeErr error, args ...string) (out string, err error, backup func() bool)
	}
	privRun := func(t *testing.T, tty bool, input string, hook func(), removeErr error, args ...string) (string, error, func() bool) {
		sys, st := privZeroStore()
		st.removeErr = removeErr
		if hook != nil {
			sys.onEdition = func(call int) {
				if call == 2 { // na releitura DEPOIS da confirmação
					sys.regs[pk(advKey, "Enabled")] = privacidade.DWORD(1)
				}
			}
		}
		out, err := runPriv(t, privDeps(t, sys, st, "windows", tty, input), append([]string{"desfazer"}, args...)...)
		return out, err, func() bool { return st.b != nil }
	}
	animRun := func(t *testing.T, tty bool, input string, hook func(), removeErr error, args ...string) (string, error, func() bool) {
		sys, st := animZero(t)
		st.removeErr = removeErr
		if hook != nil {
			sys.afterRead = func() { sys.set(animStr("5")) }
		}
		out, err := runAnim(t, animDeps(t, sys, st, "windows", tty, input), append([]string{"animacoes", "desfazer"}, args...)...)
		return out, err, func() bool { return st.b != nil }
	}
	for _, c := range []cli{{"privacidade", privRun}, {"animacoes", animRun}} {
		fail := map[string]struct {
			tty       bool
			input     string
			hook      func()
			removeErr error
			args      []string
		}{
			"cancelar (n)":        {tty: true, input: "n\n"},
			"sem terminal":        {tty: false},
			"estado alterado":     {tty: true, input: "s\n", hook: func() {}},
			"Remove() com erro":   {tty: true, input: "s\n", removeErr: errors.New("disco cheio")},
			"--yes + Remove erro": {tty: false, removeErr: errors.New("disco cheio"), args: []string{"--yes"}},
		}
		for name, f := range fail {
			t.Run(c.name+"/"+name, func(t *testing.T) {
				out, _, hasBackup := c.run(t, f.tty, f.input, f.hook, f.removeErr, f.args...)
				if !hasBackup() {
					t.Fatal("o backup tem de ficar")
				}
				if strings.Contains(strings.ToLower(out), "foi removido") {
					t.Errorf("a saída afirma que o backup foi removido sem ter sido:\n%s", out)
				}
			})
		}
		t.Run(c.name+"/sucesso anuncia só depois", func(t *testing.T) {
			out, err, hasBackup := c.run(t, true, "s\n", nil, nil)
			if err != nil || hasBackup() {
				t.Fatalf("err=%v backup=%v\n%s", err, hasBackup(), out)
			}
			iq, ir := strings.Index(out, "Remover o backup?"), strings.Index(out, "O backup foi removido")
			if iq < 0 || ir < 0 || ir < iq {
				t.Errorf("'O backup foi removido' deve vir DEPOIS da pergunta (pergunta=%d, aviso=%d):\n%s", iq, ir, out)
			}
		})
	}
}

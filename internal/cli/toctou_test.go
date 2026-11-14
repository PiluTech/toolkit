package cli

import (
	"io"
	"reflect"
	"strings"
	"testing"

	"github.com/PiluTech/toolkit/internal/privacidade"
)

// mutatingReader é o stdin da confirmação: na primeira leitura (a pergunta [s/N]) muda o sistema e só então responde "s".
type mutatingReader struct {
	mutate func()
	r      io.Reader
	done   bool
}

func (m *mutatingReader) Read(p []byte) (int, error) {
	if !m.done {
		m.done = true
		m.mutate()
	}
	return m.r.Read(p)
}

const staleMsg = "o estado mudou desde o plano mostrado; nada foi alterado"

const advKey = `HKCU\Software\Microsoft\Windows\CurrentVersion\AdvertisingInfo`

func TestPrivacidadeAplicarRecusaSeEstadoMudouNaPergunta(t *testing.T) {
	sys, st := newPrivSys(true), &privStore{}
	deps := privDeps(t, sys, st, "windows", true, "")
	deps.Stdin = &mutatingReader{r: strings.NewReader("s\n"), mutate: func() {
		sys.regs[pk(advKey, "Enabled")] = privacidade.DWORD(1) // era ausente no plano mostrado
	}}
	_, err := runPriv(t, deps, "aplicar", "--perfil", "basico")
	if err == nil || !strings.Contains(err.Error(), staleMsg) {
		t.Fatalf("err=%v", err)
	}
	if sys.writes != 0 || st.b != nil {
		t.Errorf("nada pode mudar: writes=%d backup=%v", sys.writes, st.b)
	}
	if v := sys.regs[pk(advKey, "Enabled")]; v.DWORD != 1 {
		t.Errorf("valor do outro processo perdido: %+v", v)
	}
}

func TestPrivacidadeDesfazerRecusaSeBackupOuEstadoMudouNaPergunta(t *testing.T) {
	cases := map[string]func(sys *privSys, st *privStore){
		"estado": func(sys *privSys, _ *privStore) { sys.svcs["DiagTrack"].StartType = "manual" },
		"backup": func(_ *privSys, st *privStore) {
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
			before := reflect.ValueOf(st.b).Elem().Interface()
			deps := privDeps(t, sys, st, "windows", true, "")
			deps.Stdin = &mutatingReader{r: strings.NewReader("s\n"), mutate: func() { mut(sys, st) }}
			_, err := runPriv(t, deps, "desfazer")
			if err == nil || !strings.Contains(err.Error(), staleMsg) {
				t.Fatalf("err=%v", err)
			}
			if sys.writes != w || st.b == nil {
				t.Errorf("nada pode mudar: writes=%d backup=%v", sys.writes-w, st.b)
			}
			if name == "estado" && reflect.DeepEqual(before, reflect.ValueOf(st.b).Elem().Interface()) == false {
				t.Error("backup alterado")
			}
		})
	}
}

func TestAppsNativosRemoverRecusaSeEstadoMudouNaPergunta(t *testing.T) {
	sys, st := newNatSys(false, pkgWeather), &natStore{}
	deps := natDeps(t, sys, st, "windows", true, "")
	deps.Stdin = &mutatingReader{r: strings.NewReader("s\n"), mutate: func() {
		sys.installed[0].Version = "9.9.9.0" // o app foi atualizado durante a pergunta
		sys.installed[0].PackageFullName = pkgWeather + "_9.9.9.0_x64__8wekyb3d8bbwe"
	}}
	_, err := runNat(t, deps, "remover", "--app", "bing-weather")
	if err == nil || !strings.Contains(err.Error(), staleMsg) {
		t.Fatalf("err=%v", err)
	}
	if len(sys.removes) != 0 || st.b != nil {
		t.Errorf("nada pode mudar: removes=%v backup=%v", sys.removes, st.b)
	}
}

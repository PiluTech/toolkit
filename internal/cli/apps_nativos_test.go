package cli

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/PiluTech/toolkit/internal/appsnativos"
)

func runNat(t *testing.T, deps Deps, args ...string) (string, error) {
	t.Helper()
	var o, e bytes.Buffer
	cmd := NewRootCmdWith(&o, &e, deps)
	cmd.SetArgs(append([]string{"apps-nativos", "--no-update-check"}, args...))
	err := cmd.ExecuteContext(context.Background())
	return o.String(), err
}

const (
	pkgWeather = "Microsoft.BingWeather"
	pkgNews    = "Microsoft.BingNews"
	pkgStore   = "Microsoft.WindowsStore"
)

func TestAppsNativosSemArgsSoListaOCatalogo(t *testing.T) {
	sys, st := newNatSys(false, pkgWeather, pkgStore), &natStore{}
	out, err := runNat(t, natDeps(t, sys, st, "windows", false, ""))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"bing-weather", "instalado", "ausente", "basico", "completo", "toolkit apps-nativos remover"} {
		if !strings.Contains(out, want) {
			t.Errorf("faltou %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "windows-store") || len(sys.removes) != 0 || st.b != nil {
		t.Errorf("a loja não é do catálogo e nada pode mudar: %v", sys.removes)
	}
}

func TestAppsNativosForaDoWindows(t *testing.T) {
	for goos, want := range map[string]string{"darwin": "protegido", "linux": "distribuição"} {
		for _, args := range [][]string{{}, {"remover", "--perfil", "basico", "--yes"}, {"reinstalar"}} {
			sys, st := newNatSys(true, pkgWeather), &natStore{}
			_, err := runNat(t, natDeps(t, sys, st, goos, true, ""), args...)
			if err == nil || !strings.Contains(err.Error(), want) {
				t.Errorf("%s %v: erro = %v", goos, args, err)
			}
			if len(sys.removes) != 0 || st.b != nil {
				t.Errorf("%s %v: nada pode mudar", goos, args)
			}
		}
	}
}

func TestAppsNativosRemoverExigeSelecaoValida(t *testing.T) {
	deps := natDeps(t, newNatSys(true, pkgWeather), &natStore{}, "windows", true, "")
	if _, err := runNat(t, deps, "remover"); err == nil || !strings.Contains(err.Error(), "informe o que remover") {
		t.Errorf("sem seleção: %v", err)
	}
	if _, err := runNat(t, deps, "remover", "--perfil", "xis"); err == nil || !strings.Contains(err.Error(), "desconhecido") {
		t.Errorf("perfil ruim: %v", err)
	}
	for _, id := range []string{"windows-store", "Microsoft.WindowsStore", "x'; calc #"} {
		if _, err := runNat(t, deps, "remover", "--app", id); err == nil || !strings.Contains(err.Error(), "não está no catálogo") {
			t.Errorf("app %q: %v", id, err)
		}
	}
}

func TestAppsNativosPlanoAntesDryRunNaoMuda(t *testing.T) {
	sys, st := newNatSys(false, pkgWeather, pkgNews), &natStore{}
	out, err := runNat(t, natDeps(t, sys, st, "windows", false, ""), "remover", "--perfil", "basico", "--dry-run")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"MSN Clima", pkgWeather, pkgNews, "1.2.3.0", "--dry-run: nada foi alterado"} {
		if !strings.Contains(out, want) {
			t.Errorf("faltou %q:\n%s", want, out)
		}
	}
	if len(sys.removes) != 0 || st.b != nil {
		t.Errorf("--dry-run mudou algo: %v", sys.removes)
	}
}

func TestAppsNativosSemYesESemTerminalErraAntesDeMudar(t *testing.T) {
	sys, st := newNatSys(false, pkgWeather), &natStore{}
	out, err := runNat(t, natDeps(t, sys, st, "windows", false, ""), "remover", "--app", "bing-weather")
	if err == nil || !strings.Contains(err.Error(), "--yes") {
		t.Fatalf("erro = %v", err)
	}
	if !strings.Contains(out, pkgWeather) {
		t.Errorf("o plano deve aparecer antes do erro:\n%s", out)
	}
	if len(sys.removes) != 0 || st.b != nil {
		t.Error("nada pode mudar")
	}
}

func TestAppsNativosConfirmacaoNegativaCancela(t *testing.T) {
	for _, in := range []string{"n\n", "\n", "talvez\n"} {
		sys, st := newNatSys(false, pkgWeather), &natStore{}
		out, err := runNat(t, natDeps(t, sys, st, "windows", true, in), "remover", "--app", "bing-weather")
		if err != nil {
			t.Fatalf("%q: %v", in, err)
		}
		if !strings.Contains(out, "[s/N]") || !strings.Contains(out, "Cancelado") {
			t.Errorf("%q: saída:\n%s", in, out)
		}
		if len(sys.removes) != 0 || st.b != nil {
			t.Errorf("%q: nada pode mudar", in)
		}
	}
}

func TestAppsNativosTodosUsuariosSemAdminRecusaAntesDeMudar(t *testing.T) {
	sys, st := newNatSys(false, pkgWeather), &natStore{}
	out, err := runNat(t, natDeps(t, sys, st, "windows", true, "s\n"), "remover", "--app", "bing-weather", "--todos-usuarios")
	if err == nil || !strings.Contains(err.Error(), "como administrador") {
		t.Fatalf("erro = %v", err)
	}
	if strings.Contains(out, "[s/N]") {
		t.Error("não deve perguntar quando vai recusar")
	}
	if len(sys.removes) != 0 || st.b != nil {
		t.Error("nada pode mudar")
	}
	// o escopo do usuário funciona sem administrador
	if _, err := runNat(t, natDeps(t, sys, st, "windows", false, ""), "remover", "--app", "bing-weather", "--yes"); err != nil {
		t.Errorf("usuário sem admin: %v", err)
	}
}

func TestAppsNativosRemoverEReinstalarSoTexto(t *testing.T) {
	sys, st := newNatSys(false, pkgWeather, pkgNews), &natStore{}
	out, err := runNat(t, natDeps(t, sys, st, "windows", true, "s\n"), "remover", "--perfil", "basico")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "[s/N]") || !strings.Contains(out, "Removido: 2") || !strings.Contains(out, "apps-nativos-S-1-5-21") {
		t.Errorf("saída:\n%s", out)
	}
	if len(sys.removes) != 2 || !strings.HasPrefix(sys.removes[0], "user:") || st.b == nil || len(st.b.Entries) != 2 {
		t.Fatalf("removes=%v backup=%v", sys.removes, st.b)
	}
	// segunda vez: nada instalado, nada a fazer
	sys.installed = nil
	if out, err := runNat(t, natDeps(t, sys, st, "windows", false, ""), "remover", "--perfil", "basico", "--yes"); err != nil || !strings.Contains(out, "Nada a fazer") {
		t.Errorf("segunda vez: %v\n%s", err, out)
	}

	n := len(sys.removes)
	out, err = runNat(t, natDeps(t, sys, st, "windows", false, ""), "reinstalar")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	for _, want := range []string{"MSN Clima", "Microsoft Store", "Add-AppxPackage -RegisterByFamilyName -MainPackage Microsoft.BingWeather_8wekyb3d8bbwe", "não executa"} {
		if !strings.Contains(out, want) {
			t.Errorf("reinstalar sem %q:\n%s", want, out)
		}
	}
	if len(sys.removes) != n {
		t.Error("reinstalar não executa nada")
	}
}

func TestAppsNativosTodosUsuariosComAdminRemoveProvisionado(t *testing.T) {
	sys, st := newNatSys(true, pkgWeather), &natStore{}
	sys.provisioned = []appsnativos.Provisioned{{DisplayName: pkgWeather, PackageName: pkgWeather + "_1.2.3.0_neutral_~_8wekyb3d8bbwe", Version: "1.2.3.0"}}
	if _, err := runNat(t, natDeps(t, sys, st, "windows", false, ""), "remover", "--app", "bing-weather", "--todos-usuarios", "--yes"); err != nil {
		t.Fatal(err)
	}
	if len(sys.removes) != 2 || !strings.HasPrefix(sys.removes[0], "all:") || !strings.HasPrefix(sys.removes[1], "prov:") {
		t.Errorf("removes = %v", sys.removes)
	}
}

func TestAppsNativosReinstalarSemBackup(t *testing.T) {
	out, err := runNat(t, natDeps(t, newNatSys(false), &natStore{}, "windows", false, ""), "reinstalar")
	if !errors.Is(err, appsnativos.ErrNoBackup) || !strings.Contains(out, "Não há backup") {
		t.Errorf("err=%v out=%s", err, out)
	}
}

func TestAppsNativosSemSIDNaoUsaBackupCompartilhado(t *testing.T) {
	deps := natDeps(t, newNatSys(true), nil, "windows", false, "")
	deps.AppsNat.System = natSysNoSID{newNatSys(true)}
	out, err := runNat(t, deps, "reinstalar")
	if err == nil || !strings.Contains(err.Error(), "SID") {
		t.Fatalf("err=%v\n%s", err, out)
	}
}

func TestAppsNativosSaidaNaoTrazControle(t *testing.T) {
	sys, st := newNatSys(false, pkgWeather), &natStore{}
	sys.installed[0].Publisher = "CN=\x1b]0;pwn\x07, O=\u202eMS"
	for _, args := range [][]string{{}, {"remover", "--app", "bing-weather", "--dry-run"}} {
		out, err := runNat(t, natDeps(t, sys, st, "windows", false, ""), args...)
		if err != nil {
			t.Fatal(err)
		}
		if strings.ContainsAny(out, "\x1b\x07\u202e") {
			t.Errorf("%v: a saída tem caractere de controle: %q", args, out)
		}
	}
}

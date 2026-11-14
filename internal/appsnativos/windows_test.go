package appsnativos

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/PiluTech/toolkit/internal/exec"
)

const psExe = `C:\Windows\System32\WindowsPowerShell\v1.0\powershell.exe`

type rep struct {
	code   int
	stdout string
	stderr string
	err    error
}

// recRunner registra todo Cmd e responde na ordem; comando a mais falha o teste.
type recRunner struct {
	t       *testing.T
	replies []rep
	calls   []exec.Cmd
}

func (r *recRunner) Run(_ context.Context, c exec.Cmd) (exec.Result, error) {
	r.t.Helper()
	r.calls = append(r.calls, c)
	if len(r.replies) == 0 {
		r.t.Errorf("comando inesperado: %s", c)
		return exec.Result{}, errors.New("sem roteiro")
	}
	rp := r.replies[0]
	r.replies = r.replies[1:]
	return exec.Result{ExitCode: rp.code, Stdout: []byte(rp.stdout), Stderr: []byte(rp.stderr)}, rp.err
}

func (r *recRunner) LookPath(string) (string, error) { return "", errors.New("não usado") }

func newWin(t *testing.T, admin bool, replies ...rep) (System, *recRunner) {
	rr := &recRunner{t: t, replies: replies}
	return NewWindowsSystem(rr, func() bool { return admin }), rr
}

func wantArgs(script string) []string {
	return []string{"-NoProfile", "-NonInteractive", "-Command", script}
}

const listJSON = `[{"Name":"Microsoft.BingWeather","PackageFullName":"Microsoft.BingWeather_4.25.0.0_x64__8wekyb3d8bbwe","PackageFamilyName":"Microsoft.BingWeather_8wekyb3d8bbwe","Version":"4.25.0.0","Publisher":"CN=Microsoft Corporation","IsFramework":false,"SignatureKind":"Store","NonRemovable":false},` +
	`{"Name":"Microsoft.VCLibs.140.00","PackageFullName":"Microsoft.VCLibs.140.00_14.0.0.0_x64__8wekyb3d8bbwe","PackageFamilyName":"Microsoft.VCLibs.140.00_8wekyb3d8bbwe","Version":"14.0.0.0","Publisher":"CN=Microsoft","IsFramework":true,"SignatureKind":"Store","NonRemovable":false}]`

func TestScriptsSaoASCIISemInterpolacao(t *testing.T) {
	for name, s := range map[string]string{"list": scriptListInstalled(false), "listAll": scriptListInstalled(true), "prov": scriptListProvisioned,
		"user": scriptRemoveUser, "all": scriptRemoveAll, "prov-rm": scriptRemoveProvisioned} {
		for _, r := range s {
			if r > 0x7e || (r < 0x20) {
				t.Errorf("%s: caractere não ASCII ou de controle %U", name, r)
			}
		}
	}
	for _, s := range []string{scriptRemoveUser, scriptRemoveAll, scriptRemoveProvisioned} {
		if !strings.Contains(s, "$env:"+pkgEnv) {
			t.Errorf("o nome do pacote entra por variável de ambiente: %s", s)
		}
	}
	if strings.Contains(scriptListInstalled(false), "-AllUsers") || !strings.Contains(scriptListInstalled(true), "-AllUsers") {
		t.Error("-AllUsers só quando pedido")
	}
}

func TestListInstalledComandoExatoEParse(t *testing.T) {
	sys, rr := newWin(t, false, rep{stdout: "\ufeff" + listJSON})
	pk, err := sys.ListInstalled(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	if len(pk) != 2 || pk[0].Name != "Microsoft.BingWeather" || pk[0].Version != "4.25.0.0" || pk[0].SignatureKind != "Store" || !pk[1].IsFramework {
		t.Errorf("parse = %+v", pk)
	}
	c := rr.calls[0]
	if c.Name != psExe || !slices.Equal(c.Args, wantArgs(scriptListInstalled(false))) || !c.ReadOnly || c.Elevate || len(c.Env) != 0 {
		t.Errorf("cmd = %+v", c)
	}
	sys, rr = newWin(t, true, rep{stdout: listJSON})
	if _, err := sys.ListInstalled(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	if c := rr.calls[0]; !slices.Equal(c.Args, wantArgs(scriptListInstalled(true))) || !c.ReadOnly || c.Elevate {
		t.Errorf("allUsers cmd = %+v", c)
	}
}

func TestListInstalledAceitaObjetoUnicoEIgnoraNomeEstranho(t *testing.T) {
	one := `{"Name":"Microsoft.People","PackageFullName":"Microsoft.People_1.0.0.0_x64__8wekyb3d8bbwe","PackageFamilyName":"Microsoft.People_8wekyb3d8bbwe","Version":"1.0.0.0","Publisher":"CN=\u00e9","IsFramework":false,"SignatureKind":"Store","NonRemovable":false}`
	sys, _ := newWin(t, false, rep{stdout: one})
	pk, err := sys.ListInstalled(context.Background(), false)
	if err != nil || len(pk) != 1 || pk[0].Publisher != "CN=é" {
		t.Errorf("%+v %v", pk, err)
	}
	guid := `[{"Name":"1527c705-839a-4832-9118-54d4Bd6a0c89","PackageFullName":"x","PackageFamilyName":"y","Version":"1","Publisher":"z","IsFramework":false,"SignatureKind":"System","NonRemovable":true}]`
	sys, _ = newWin(t, false, rep{stdout: guid})
	if pk, err := sys.ListInstalled(context.Background(), false); err != nil || len(pk) != 1 {
		t.Errorf("nome com hífen vem do sistema e só não casa o catálogo: %v %v", pk, err)
	}
}

func TestListInstalledFalhasViramErro(t *testing.T) {
	cases := map[string]rep{
		"json inválido":         {stdout: `{nao`},
		"vazio":                 {stdout: "  "},
		"exit != 0":             {code: 1, stderr: "Get-AppxPackage : acesso negado"},
		"erro do runner":        {err: errors.New("não iniciou")},
		"tipo errado":           {stdout: `"texto"`},
		"campo com tipo errado": {stdout: `[{"Name":5}]`},
	}
	for name, rp := range cases {
		sys, _ := newWin(t, false, rp)
		if pk, err := sys.ListInstalled(context.Background(), false); err == nil || pk != nil {
			t.Errorf("%s: deveria falhar (%v %v)", name, pk, err)
		}
	}
}

func TestListProvisioned(t *testing.T) {
	out := `[{"DisplayName":"Microsoft.BingWeather","PackageName":"Microsoft.BingWeather_4.25.0.0_neutral_~_8wekyb3d8bbwe","Version":"4.25.0.0"}]`
	sys, rr := newWin(t, true, rep{stdout: out})
	pv, err := sys.ListProvisioned(context.Background())
	if err != nil || len(pv) != 1 || pv[0].DisplayName != "Microsoft.BingWeather" || pv[0].PackageName == "" {
		t.Fatalf("%+v %v", pv, err)
	}
	c := rr.calls[0]
	if c.Name != psExe || !slices.Equal(c.Args, wantArgs(scriptListProvisioned)) || !c.ReadOnly || c.Elevate {
		t.Errorf("cmd = %+v", c)
	}
	sys, _ = newWin(t, true, rep{stdout: "lixo"})
	if _, err := sys.ListProvisioned(context.Background()); err == nil {
		t.Error("json inválido tem de falhar")
	}
}

func TestRemocoesComandoExatoElevacaoEEnv(t *testing.T) {
	const full = "Microsoft.BingWeather_4.25.0.0_x64__8wekyb3d8bbwe"
	const pname = "Microsoft.BingWeather_4.25.0.0_neutral_~_8wekyb3d8bbwe"
	type tc struct {
		name    string
		call    func(System) error
		script  string
		elevate bool
		env     string
	}
	for _, c := range []tc{
		{"user", func(s System) error { return s.RemoveForUser(context.Background(), full) }, scriptRemoveUser, false, full},
		{"all", func(s System) error { return s.RemoveAllUsers(context.Background(), full) }, scriptRemoveAll, true, full},
		{"prov", func(s System) error { return s.RemoveProvisioned(context.Background(), pname) }, scriptRemoveProvisioned, true, pname},
	} {
		sys, rr := newWin(t, true, rep{})
		if err := c.call(sys); err != nil {
			t.Errorf("%s: %v", c.name, err)
			continue
		}
		got := rr.calls[0]
		if got.Name != psExe || !slices.Equal(got.Args, wantArgs(c.script)) || got.ReadOnly || got.Elevate != c.elevate || !slices.Equal(got.Env, []string{pkgEnv + "=" + c.env}) {
			t.Errorf("%s: cmd = %+v", c.name, got)
		}
	}
}

func TestRemocaoRecusaNomeForaDoFormatoSemExecutar(t *testing.T) {
	for _, bad := range []string{"", "a b", "x';calc", "x\"y", "$(calc)", "a`b", "-Force", "a\nb", "x|y", strings.Repeat("a", 300)} {
		sys, rr := newWin(t, true)
		for _, call := range []func() error{
			func() error { return sys.RemoveForUser(context.Background(), bad) },
			func() error { return sys.RemoveAllUsers(context.Background(), bad) },
			func() error { return sys.RemoveProvisioned(context.Background(), bad) },
		} {
			if err := call(); !errors.Is(err, ErrEntradaInvalida) {
				t.Errorf("%q: erro = %v", bad, err)
			}
		}
		if len(rr.calls) != 0 {
			t.Errorf("%q: nenhum comando pode rodar: %v", bad, rr.calls)
		}
	}
}

func TestRemocaoFalhasViramErro(t *testing.T) {
	sys, _ := newWin(t, true, rep{code: 1, stderr: "Remove-AppxPackage : falha \x1b[31mvermelha\x1b[0m"})
	err := sys.RemoveForUser(context.Background(), "Microsoft.BingWeather_1.0.0.0_x64__8wekyb3d8bbwe")
	if err == nil || strings.Contains(err.Error(), "\x1b") {
		t.Errorf("exit != 0 é erro com texto limpo: %v", err)
	}
	sys, _ = newWin(t, false, rep{err: exec.ErrNotElevated})
	if err := sys.RemoveAllUsers(context.Background(), "Microsoft.BingWeather_1.0.0.0_x64__8wekyb3d8bbwe"); !errors.Is(err, ErrNeedsAdmin) {
		t.Errorf("sem elevação = ErrNeedsAdmin: %v", err)
	}
}

func TestIsAdminEUserSID(t *testing.T) {
	sys, _ := newWin(t, true)
	if !sys.IsAdmin(context.Background()) {
		t.Error("IsAdmin")
	}
	if NewWindowsSystem(&recRunner{t: t}, nil).IsAdmin(context.Background()) {
		t.Error("sem callback = não admin")
	}
	sys, rr := newWin(t, false, rep{stdout: "\"PC\\\\user\",\"S-1-5-21-1-2-3-1001\"\r\n"})
	sid, err := UserSID(context.Background(), sys)
	if err != nil || sid != "S-1-5-21-1-2-3-1001" {
		t.Errorf("%q %v", sid, err)
	}
	if c := rr.calls[0]; c.Name != `C:\Windows\System32\whoami.exe` || !c.ReadOnly || c.Elevate {
		t.Errorf("cmd = %+v", c)
	}
	sys, _ = newWin(t, false, rep{stdout: "lixo"})
	if _, err := UserSID(context.Background(), sys); err == nil {
		t.Error("saída ruim tem de falhar")
	}
	if _, err := UserSID(context.Background(), noSID{}); err == nil {
		t.Error("System sem SIDer falha fechado")
	}
}

type noSID struct{ System }

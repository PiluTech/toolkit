package privacidade

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/PiluTech/toolkit/internal/exec"
)

// reply é a resposta roteirizada de um comando.
type reply struct {
	code   int
	stdout string
	stderr string
	err    error
}

// recRunner registra todo Cmd recebido e responde por "exe arg arg..." (exe = só o nome do arquivo).
// Comando sem roteiro falha o teste.
type recRunner struct {
	t       *testing.T
	replies map[string]reply
	calls   []exec.Cmd
}

func newRec(t *testing.T) *recRunner {
	t.Helper()
	return &recRunner{t: t, replies: map[string]reply{}}
}

func (r *recRunner) on(line string, rp reply) *recRunner {
	r.replies[line] = rp
	return r
}

func lineOf(c exec.Cmd) string {
	name := c.Name
	if i := strings.LastIndex(name, `\`); i >= 0 {
		name = name[i+1:]
	}
	name = strings.TrimSuffix(name, ".exe")
	return strings.Join(append([]string{name}, c.Args...), " ")
}

func (r *recRunner) Run(_ context.Context, c exec.Cmd) (exec.Result, error) {
	r.t.Helper()
	r.calls = append(r.calls, c)
	switch c.Name {
	case `C:\Windows\System32\reg.exe`, `C:\Windows\System32\sc.exe`, `C:\Windows\System32\schtasks.exe`:
	default:
		r.t.Errorf("executável fora da lista, ou sem caminho absoluto: %q", c.Name)
	}
	rp, ok := r.replies[lineOf(c)]
	if !ok {
		r.t.Errorf("comando inesperado: %s", lineOf(c))
		return exec.Result{}, errors.New("comando inesperado")
	}
	return exec.Result{ExitCode: rp.code, Stdout: []byte(rp.stdout), Stderr: []byte(rp.stderr)}, rp.err
}

func (r *recRunner) LookPath(string) (string, error) { return "", errors.New("sem PATH") }

func newSys(r exec.Runner, admin bool) System {
	return NewWindowsSystem(r, func() bool { return admin })
}

var ctx = context.Background()

func TestIsAdmin(t *testing.T) {
	if !newSys(newRec(t), true).IsAdmin(ctx) {
		t.Fatal("esperava admin")
	}
	if newSys(newRec(t), false).IsAdmin(ctx) {
		t.Fatal("não esperava admin")
	}
	if NewWindowsSystem(newRec(t), nil).IsAdmin(ctx) {
		t.Fatal("callback nil deve dar false")
	}
}

func TestRegReadDWORD(t *testing.T) {
	r := newRec(t).on(`reg query HKCU\Software\Microsoft\Windows\CurrentVersion\AdvertisingInfo /v Enabled`, reply{
		stdout: "\r\nHKEY_CURRENT_USER\\Software\\Microsoft\\Windows\\CurrentVersion\\AdvertisingInfo\r\n    Enabled    REG_DWORD    0x1\r\n\r\n",
	})
	v, ok, err := newSys(r, false).RegRead(ctx, `HKCU\Software\Microsoft\Windows\CurrentVersion\AdvertisingInfo`, "Enabled")
	if err != nil || !ok || v.Kind != RegDWORD || v.DWORD != 1 {
		t.Fatalf("v=%+v ok=%v err=%v", v, ok, err)
	}
}

func TestRegReadDWORDHexGrande(t *testing.T) {
	r := newRec(t).on(`reg query HKLM\SOFTWARE\X /v N`, reply{stdout: "\r\nHKEY_LOCAL_MACHINE\\SOFTWARE\\X\r\n    N    REG_DWORD    0xffffffff\r\n"})
	v, ok, err := newSys(r, false).RegRead(ctx, `HKLM\SOFTWARE\X`, "N")
	if err != nil || !ok || v.DWORD != 0xffffffff {
		t.Fatalf("v=%+v ok=%v err=%v", v, ok, err)
	}
}

func TestRegReadStringComEspacosEAcentos(t *testing.T) {
	r := newRec(t).on(`reg query HKCU\Software\X /v Texto`, reply{
		stdout: "\r\nHKEY_CURRENT_USER\\Software\\X\r\n    Texto    REG_SZ    Olá mundo  com espaços  \r\n\r\n",
	})
	v, ok, err := newSys(r, false).RegRead(ctx, `HKCU\Software\X`, "Texto")
	if err != nil || !ok || v.Kind != RegString || v.Str != "Olá mundo  com espaços  " {
		t.Fatalf("v=%+v ok=%v err=%v", v, ok, err)
	}
}

func TestRegReadCabecalhoTraduzidoEOutrosValores(t *testing.T) {
	// linhas de outros valores e de outro idioma antes; o nome "Foo" não pode casar com "FooBar".
	r := newRec(t).on(`reg query HKCU\Software\X /v Foo`, reply{
		stdout: "\r\nHKEY_CURRENT_USER\\Software\\X\r\n    FooBar    REG_DWORD    0x9\r\n    foo    REG_DWORD    0x2\r\n",
	})
	v, ok, err := newSys(r, false).RegRead(ctx, `HKCU\Software\X`, "Foo")
	if err != nil || !ok || v.DWORD != 2 {
		t.Fatalf("v=%+v ok=%v err=%v", v, ok, err)
	}
}

func TestRegReadInexistente(t *testing.T) {
	// valor ausente com a chave existente: confirmado pela listagem da chave (sem /v).
	r := newRec(t).
		on(`reg query HKCU\Software\X /v Nada`, reply{code: 1, stderr: "ERRO: o sistema não pôde localizar a chave ou o valor do Registro especificado.\r\n"}).
		on(`reg query HKCU\Software\X`, reply{stdout: "\r\nHKEY_CURRENT_USER\\Software\\X\r\n    Outro    REG_DWORD    0x1\r\n\r\n"})
	_, ok, err := newSys(r, false).RegRead(ctx, `HKCU\Software\X`, "Nada")
	if err != nil || ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
}

func TestRegReadChaveInexistenteConfirmadaPelaChavePai(t *testing.T) {
	r := newRec(t).
		on(`reg query HKCU\Software\X /v Nada`, reply{code: 1, stderr: "x"}).
		on(`reg query HKCU\Software\X`, reply{code: 1, stderr: "x"}).
		on(`reg query HKCU\Software`, reply{stdout: "\r\nHKEY_CURRENT_USER\\Software\r\nHKEY_CURRENT_USER\\Software\\Outra\r\n"})
	_, ok, err := newSys(r, false).RegRead(ctx, `HKCU\Software\X`, "Nada")
	if err != nil || ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	for _, c := range r.calls {
		if !c.ReadOnly || c.Elevate {
			t.Errorf("leitura com flags erradas: %+v", c)
		}
	}
}

func TestRegReadAcessoNegadoNaoViraAusencia(t *testing.T) {
	// /v falha com exit 1 e a chave existe (a chave pai a lista), mas não dá para lê-la: erro, nunca existed=false.
	r := newRec(t).
		on(`reg query HKLM\SOFTWARE\Policies\X /v N`, reply{code: 1, stderr: "ERROR: Access is denied.\r\n"}).
		on(`reg query HKLM\SOFTWARE\Policies\X`, reply{code: 1, stderr: "ERROR: Access is denied.\r\n"}).
		on(`reg query HKLM\SOFTWARE\Policies`, reply{stdout: "\r\nHKEY_LOCAL_MACHINE\\SOFTWARE\\Policies\r\nHKEY_LOCAL_MACHINE\\SOFTWARE\\Policies\\x\r\n"})
	_, ok, err := newSys(r, false).RegRead(ctx, `HKLM\SOFTWARE\Policies\X`, "N")
	if err == nil || ok {
		t.Fatalf("esperava erro de leitura, ok=%v err=%v", ok, err)
	}
}

func TestRegReadFalhaNaChavePaiEErro(t *testing.T) {
	r := newRec(t).
		on(`reg query HKCU\Software\X /v N`, reply{code: 1}).
		on(`reg query HKCU\Software\X`, reply{code: 1}).
		on(`reg query HKCU\Software`, reply{code: 5, stderr: "negado"})
	if _, ok, err := newSys(r, false).RegRead(ctx, `HKCU\Software\X`, "N"); err == nil || ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
}

func TestRegReadValorExisteMasVFalhouEErro(t *testing.T) {
	r := newRec(t).
		on(`reg query HKCU\Software\X /v N`, reply{code: 1}).
		on(`reg query HKCU\Software\X`, reply{stdout: "\r\nHKEY_CURRENT_USER\\Software\\X\r\n    N    REG_DWORD    0x1\r\n"})
	if _, ok, err := newSys(r, false).RegRead(ctx, `HKCU\Software\X`, "N"); err == nil || ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
}

func TestRegReadExpandSZPreservaTipoEConteudo(t *testing.T) {
	r := newRec(t).on(`reg query HKCU\Software\X /v P`, reply{
		stdout: "\r\nHKEY_CURRENT_USER\\Software\\X\r\n    P    REG_EXPAND_SZ    %SystemRoot%\\System32;\"a b\"\r\n",
	})
	v, ok, err := newSys(r, false).RegRead(ctx, `HKCU\Software\X`, "P")
	want := RegValue{Kind: RegExpandString, Str: `%SystemRoot%\System32;"a b"`}
	if err != nil || !ok || v != want {
		t.Fatalf("v=%+v ok=%v err=%v", v, ok, err)
	}
	// volta como REG_EXPAND_SZ, com o mesmo conteúdo
	w := newRec(t).on(`reg add HKCU\Software\X /v P /t REG_EXPAND_SZ /d %SystemRoot%\System32;"a b" /f`, reply{})
	if err := newSys(w, false).RegWrite(ctx, `HKCU\Software\X`, "P", v); err != nil {
		t.Fatal(err)
	}
}

func TestRegReadStringPreservaEspacosDasPontas(t *testing.T) {
	r := newRec(t).on(`reg query HKCU\Software\X /v S`, reply{stdout: "    S    REG_SZ      ab  \r\n"})
	v, ok, err := newSys(r, false).RegRead(ctx, `HKCU\Software\X`, "S")
	if err != nil || !ok || v.Kind != RegString || v.Str != "  ab  " {
		t.Fatalf("v=%q ok=%v err=%v", v.Str, ok, err)
	}
}

func TestRegWriteAceitaPercentEAspas(t *testing.T) {
	r := newRec(t).on(`reg add HKCU\Software\X /v S /t REG_SZ /d 100% "ok" & ^ /f`, reply{})
	if err := newSys(r, false).RegWrite(ctx, `HKCU\Software\X`, "S", RegValue{Kind: RegString, Str: `100% "ok" & ^`}); err != nil {
		t.Fatal(err)
	}
}

func TestRegReadRecusaTextoQueNaoVoltaria(t *testing.T) {
	// terminado em barra: RegWrite recusaria, então a leitura também (Apply para antes de mudar).
	r := newRec(t).on(`reg query HKCU\Software\X /v S`, reply{stdout: "    S    REG_SZ    C:\\dir\\\r\n"})
	if _, _, err := newSys(r, false).RegRead(ctx, `HKCU\Software\X`, "S"); !errors.Is(err, ErrEntradaInvalida) {
		t.Fatalf("err=%v", err)
	}
}

func TestRegReadFalhasSaoErro(t *testing.T) {
	r := newRec(t).
		on(`reg query HKCU\Software\A /v N`, reply{code: 2, stderr: "falha"}).
		on(`reg query HKCU\Software\B /v N`, reply{stdout: "lixo sem valor\r\n"}).
		on(`reg query HKCU\Software\C /v N`, reply{stdout: "    N    REG_DWORD    zzz\r\n"}).
		on(`reg query HKCU\Software\D /v N`, reply{stdout: "    N    REG_BINARY    00\r\n"}).
		on(`reg query HKCU\Software\E /v N`, reply{err: errors.New("não iniciou")})
	s := newSys(r, false)
	for _, k := range []string{"A", "B", "C", "D", "E"} {
		if _, _, err := s.RegRead(ctx, `HKCU\Software\`+k, "N"); err == nil {
			t.Errorf("%s: esperava erro", k)
		}
	}
}

func TestRegWriteComandosExatos(t *testing.T) {
	r := newRec(t).
		on(`reg add HKCU\Software\X /v Dw /t REG_DWORD /d 0 /f`, reply{}).
		on(`reg add HKLM\SOFTWARE\Policies\Y /v Sz /t REG_SZ /d Olá mundo /f`, reply{}).
		on(`reg add HKLM\SOFTWARE\Policies\Y /v Dw /t REG_DWORD /d 4294967295 /f`, reply{})
	s := newSys(r, true)
	if err := s.RegWrite(ctx, `HKCU\Software\X`, "Dw", DWORD(0)); err != nil {
		t.Fatal(err)
	}
	if err := s.RegWrite(ctx, `HKLM\SOFTWARE\Policies\Y`, "Sz", RegValue{Kind: RegString, Str: "Olá mundo"}); err != nil {
		t.Fatal(err)
	}
	if err := s.RegWrite(ctx, `HKLM\SOFTWARE\Policies\Y`, "Dw", DWORD(0xffffffff)); err != nil {
		t.Fatal(err)
	}
	want := []bool{false, true, true}
	for i, c := range r.calls {
		if c.ReadOnly || c.Elevate != want[i] {
			t.Errorf("call %d: ReadOnly=%v Elevate=%v", i, c.ReadOnly, c.Elevate)
		}
	}
}

func TestRegWriteErroDoComando(t *testing.T) {
	r := newRec(t).on(`reg add HKLM\SOFTWARE\Y /v Dw /t REG_DWORD /d 1 /f`, reply{code: 1, stderr: "ERROR: Access is denied.\r\n"})
	if err := newSys(r, true).RegWrite(ctx, `HKLM\SOFTWARE\Y`, "Dw", DWORD(1)); err == nil || !strings.Contains(err.Error(), "Access is denied") {
		t.Fatalf("err=%v", err)
	}
}

func TestRegWriteSemElevacaoPropagaErrNotElevated(t *testing.T) {
	r := newRec(t).on(`reg add HKLM\SOFTWARE\Y /v Dw /t REG_DWORD /d 1 /f`, reply{err: exec.ErrNotElevated})
	err := newSys(r, false).RegWrite(ctx, `HKLM\SOFTWARE\Y`, "Dw", DWORD(1))
	if !errors.Is(err, exec.ErrNotElevated) {
		t.Fatalf("err=%v", err)
	}
}

func TestRegDeleteExistente(t *testing.T) {
	r := newRec(t).
		on(`reg query HKLM\SOFTWARE\Y /v Dw`, reply{stdout: "\r\nHKEY_LOCAL_MACHINE\\SOFTWARE\\Y\r\n    Dw    REG_DWORD    0x1\r\n"}).
		on(`reg delete HKLM\SOFTWARE\Y /v Dw /f`, reply{})
	if err := newSys(r, true).RegDelete(ctx, `HKLM\SOFTWARE\Y`, "Dw"); err != nil {
		t.Fatal(err)
	}
	if len(r.calls) != 2 || !r.calls[0].ReadOnly || r.calls[0].Elevate || r.calls[1].ReadOnly || !r.calls[1].Elevate {
		t.Fatalf("flags: %+v", r.calls)
	}
}

func TestRegDeleteInexistenteNaoExecutaDelete(t *testing.T) {
	r := newRec(t).on(`reg query HKCU\Software\Y /v Dw`, reply{code: 1, stderr: "ERROR: The system was unable to find the specified registry key or value."}).
		on(`reg query HKCU\Software\Y`, reply{stdout: "\r\nHKEY_CURRENT_USER\\Software\\Y\r\n"})
	if err := newSys(r, false).RegDelete(ctx, `HKCU\Software\Y`, "Dw"); err != nil {
		t.Fatal(err)
	}
	for _, c := range r.calls {
		if !c.ReadOnly {
			t.Fatalf("delete executado: %+v", c)
		}
	}
}

func TestRegDeleteHKCUSemElevate(t *testing.T) {
	r := newRec(t).
		on(`reg query HKCU\Software\Y /v Dw`, reply{stdout: "    Dw    REG_DWORD    0x0\r\n"}).
		on(`reg delete HKCU\Software\Y /v Dw /f`, reply{})
	if err := newSys(r, false).RegDelete(ctx, `HKCU\Software\Y`, "Dw"); err != nil {
		t.Fatal(err)
	}
	if r.calls[1].Elevate {
		t.Fatal("HKCU não precisa de Elevate")
	}
}

func TestRegDeleteFalhaDoDelete(t *testing.T) {
	r := newRec(t).
		on(`reg query HKCU\Software\Y /v Dw`, reply{stdout: "    Dw    REG_DWORD    0x0\r\n"}).
		on(`reg delete HKCU\Software\Y /v Dw /f`, reply{code: 1, stderr: "ERROR: Access is denied."})
	if err := newSys(r, false).RegDelete(ctx, `HKCU\Software\Y`, "Dw"); err == nil {
		t.Fatal("esperava erro")
	}
}

func TestServiceReadStartTypes(t *testing.T) {
	cases := []struct {
		name, start string
		want        string
	}{
		{"auto", "2   AUTO_START", "automatic"},
		{"delayed", "2   AUTO_START (DELAYED)", "delayed-auto"},
		{"demand", "3   DEMAND_START", "manual"},
		{"disabled", "4   DISABLED", "disabled"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := newRec(t).
				on(`sc qc Svc`, reply{stdout: "[SC] QueryServiceConfig SUCCESS\r\n\r\nSERVICE_NAME: Svc\r\n        TIPO               : 20  WIN32_SHARE_PROCESS\r\n        START_TYPE         : " + c.start + "\r\n        ERROR_CONTROL      : 1   NORMAL\r\n        NOME_EXIBIÇÃO      : Serviço de Teste\r\n"}).
				on(`sc query Svc`, reply{stdout: "\r\nSERVICE_NAME: Svc\r\n        TYPE               : 20  WIN32_SHARE_PROCESS\r\n        STATE              : 1  STOPPED\r\n        WIN32_EXIT_CODE    : 0  (0x0)\r\n"})
			st, err := newSys(r, false).ServiceRead(ctx, "Svc")
			if err != nil || !st.Exists || st.StartType != c.want || st.Running {
				t.Fatalf("st=%+v err=%v", st, err)
			}
			for _, call := range r.calls {
				if !call.ReadOnly || call.Elevate {
					t.Errorf("leitura com flags erradas: %+v", call)
				}
			}
		})
	}
}

func TestServiceReadRunning(t *testing.T) {
	r := newRec(t).
		on(`sc qc Svc`, reply{stdout: "        START_TYPE         : 2   AUTO_START\r\n"}).
		on(`sc query Svc`, reply{stdout: "        STATE              : 4  RUNNING\r\n                                (STOPPABLE, NOT_PAUSABLE, ACCEPTS_SHUTDOWN)\r\n"})
	st, err := newSys(r, false).ServiceRead(ctx, "Svc")
	if err != nil || !st.Running || st.StartType != "automatic" {
		t.Fatalf("st=%+v err=%v", st, err)
	}
}

func TestServiceReadInexistente(t *testing.T) {
	r := newRec(t).on(`sc qc Nada`, reply{code: 1060, stdout: "[SC] OpenService FAILED 1060:\r\n\r\nO serviço especificado não existe como um serviço instalado.\r\n"})
	st, err := newSys(r, false).ServiceRead(ctx, "Nada")
	if err != nil || st.Exists {
		t.Fatalf("st=%+v err=%v", st, err)
	}
	if len(r.calls) != 1 {
		t.Fatalf("não deveria consultar o estado: %d", len(r.calls))
	}
}

func TestServiceReadFalhas(t *testing.T) {
	r := newRec(t).
		on(`sc qc A`, reply{code: 5, stdout: "Acesso negado"}).
		on(`sc qc B`, reply{stdout: "sem start type"}).
		on(`sc qc C`, reply{stdout: "START_TYPE : 1 SYSTEM_START"}).
		on(`sc qc D`, reply{stdout: "START_TYPE : 3 DEMAND_START"}).
		on(`sc query D`, reply{stdout: "sem state"})
	s := newSys(r, false)
	for _, n := range []string{"A", "B", "C", "D"} {
		if _, err := s.ServiceRead(ctx, n); err == nil {
			t.Errorf("%s: esperava erro", n)
		}
	}
}

func TestServiceConfigure(t *testing.T) {
	cases := map[string]string{"automatic": "auto", "delayed-auto": "delayed-auto", "manual": "demand", "disabled": "disabled"}
	for in, out := range cases {
		r := newRec(t).on(`sc config Svc start= `+out, reply{})
		if err := newSys(r, true).ServiceConfigure(ctx, "Svc", in); err != nil {
			t.Fatalf("%s: %v", in, err)
		}
		c := r.calls[0]
		if c.ReadOnly || !c.Elevate || len(c.Args) != 4 || c.Args[2] != "start=" || c.Args[3] != out {
			t.Errorf("%s: %+v", in, c)
		}
	}
}

func TestServiceConfigureErro(t *testing.T) {
	r := newRec(t).on(`sc config Svc start= disabled`, reply{code: 5, stdout: "[SC] OpenService FAILED 5:\r\n\r\nAcesso negado.\r\n"})
	if err := newSys(r, true).ServiceConfigure(ctx, "Svc", "disabled"); err == nil {
		t.Fatal("esperava erro")
	}
}

func TestServiceStopStart(t *testing.T) {
	cases := []struct {
		op   string
		code int
		ok   bool
	}{
		{"stop", 0, true}, {"stop", 1062, true}, {"stop", 5, false},
		{"start", 0, true}, {"start", 1056, true}, {"start", 1058, false},
	}
	for _, c := range cases {
		r := newRec(t).on("sc "+c.op+" Svc", reply{code: c.code, stdout: "saída"})
		s := newSys(r, true)
		var err error
		if c.op == "stop" {
			err = s.ServiceStop(ctx, "Svc")
		} else {
			err = s.ServiceStart(ctx, "Svc")
		}
		if (err == nil) != c.ok {
			t.Errorf("%s %d: err=%v", c.op, c.code, err)
		}
		if cl := r.calls[0]; cl.ReadOnly || !cl.Elevate {
			t.Errorf("%s: flags %+v", c.op, cl)
		}
	}
}

const taskXML = `<?xml version="1.0" encoding="UTF-16"?>
<Task version="1.2" xmlns="http://schemas.microsoft.com/windows/2004/02/mit/task">
  <Triggers>
    <TimeTrigger>
      <Enabled>false</Enabled>
    </TimeTrigger>
  </Triggers>
  <Settings>
    <MultipleInstancesPolicy>IgnoreNew</MultipleInstancesPolicy>
    %s
  </Settings>
  <Actions Context="Author"><Exec><Command>x.exe</Command></Exec></Actions>
</Task>
`

func xmlWith(enabled string) string { return strings.Replace(taskXML, "%s", enabled, 1) }

func TestTaskRead(t *testing.T) {
	cases := []struct {
		name, xml string
		want      bool
	}{
		{"habilitada ausente", xmlWith(""), true},
		{"habilitada explicita", xmlWith("<Enabled>true</Enabled>"), true},
		{"desabilitada", xmlWith("<Enabled>false</Enabled>"), false},
		{"desabilitada espacos", xmlWith("<Enabled> False </Enabled>"), false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := newRec(t).on(`schtasks /Query /TN \Microsoft\Windows\Application Experience\ProgramDataUpdater /XML`, reply{stdout: c.xml})
			st, err := newSys(r, false).TaskRead(ctx, `\Microsoft\Windows\Application Experience\ProgramDataUpdater`)
			if err != nil || !st.Exists || st.Enabled != c.want {
				t.Fatalf("st=%+v err=%v", st, err)
			}
			if cl := r.calls[0]; !cl.ReadOnly || cl.Elevate {
				t.Errorf("flags %+v", cl)
			}
		})
	}
}

func TestTaskReadUTF16(t *testing.T) {
	src := xmlWith("<Enabled>false</Enabled>")
	b := []byte{0xFF, 0xFE}
	for _, u := range src {
		b = append(b, byte(u), byte(u>>8))
	}
	r := newRec(t).on(`schtasks /Query /TN \T\X /XML`, reply{stdout: string(b)})
	st, err := newSys(r, false).TaskRead(ctx, `\T\X`)
	if err != nil || !st.Exists || st.Enabled {
		t.Fatalf("st=%+v err=%v", st, err)
	}
}

func TestTaskReadInexistente(t *testing.T) {
	msgs := []string{
		"ERROR: The system cannot find the file specified.\r\n",
		"ERRO: O sistema não pode encontrar o arquivo especificado.\r\n",
		"ERRO: Falha inesperada qualquer.\r\n", // exit 1 sem outra pista = não existe
	}
	for _, m := range msgs {
		r := newRec(t).on(`schtasks /Query /TN \T\X /XML`, reply{code: 1, stderr: m})
		st, err := newSys(r, false).TaskRead(ctx, `\T\X`)
		if err != nil || st.Exists {
			t.Errorf("%q: st=%+v err=%v", m, st, err)
		}
	}
}

func TestTaskReadAcessoNegadoEErro(t *testing.T) {
	for _, m := range []string{"ERROR: Access is denied.\r\n", "ERRO: Acesso negado.\r\n"} {
		r := newRec(t).on(`schtasks /Query /TN \T\X /XML`, reply{code: 1, stderr: m})
		if _, err := newSys(r, false).TaskRead(ctx, `\T\X`); err == nil {
			t.Errorf("%q: esperava erro", m)
		}
	}
	r := newRec(t).on(`schtasks /Query /TN \T\X /XML`, reply{code: 3, stderr: "outro"})
	if _, err := newSys(r, false).TaskRead(ctx, `\T\X`); err == nil {
		t.Error("exit 3: esperava erro")
	}
	r = newRec(t).on(`schtasks /Query /TN \T\X /XML`, reply{stdout: "sem xml"})
	if _, err := newSys(r, false).TaskRead(ctx, `\T\X`); err == nil {
		t.Error("saída sem <Task>: esperava erro")
	}
}

func TestTaskSetEnabled(t *testing.T) {
	for flag, enabled := range map[string]bool{"/ENABLE": true, "/DISABLE": false} {
		r := newRec(t).on(`schtasks /Change /TN \T\X `+flag, reply{})
		if err := newSys(r, true).TaskSetEnabled(ctx, `\T\X`, enabled); err != nil {
			t.Fatal(err)
		}
		if c := r.calls[0]; c.ReadOnly || !c.Elevate {
			t.Errorf("%s: %+v", flag, c)
		}
	}
	r := newRec(t).on(`schtasks /Change /TN \T\X /DISABLE`, reply{code: 1, stderr: "ERROR: Access is denied."})
	if err := newSys(r, true).TaskSetEnabled(ctx, `\T\X`, false); err == nil {
		t.Fatal("esperava erro")
	}
}

func TestEdition(t *testing.T) {
	cases := map[string]string{
		"Core": EditionHome, "CoreN": EditionHome, "CoreSingleLanguage": EditionHome, "CoreCountrySpecific": EditionHome,
		"Professional": EditionPro, "ProfessionalN": EditionPro, "ProfessionalWorkstation": EditionPro, "ProfessionalEducation": EditionPro,
		"Enterprise": EditionEnterprise, "EnterpriseN": EditionEnterprise, "EnterpriseS": EditionEnterprise, "ServerStandard": EditionEnterprise,
		"Education": EditionEducation, "EducationN": EditionEducation,
		"CloudEdition": EditionOther, "": EditionOther,
	}
	for id, want := range cases {
		r := newRec(t).on(`reg query HKLM\SOFTWARE\Microsoft\Windows NT\CurrentVersion /v EditionID`, reply{
			stdout: "\r\nHKEY_LOCAL_MACHINE\\SOFTWARE\\Microsoft\\Windows NT\\CurrentVersion\r\n    EditionID    REG_SZ    " + id + "\r\n",
		})
		got, err := newSys(r, false).Edition(ctx)
		if err != nil || got != want {
			t.Errorf("%q: got %q err=%v, want %q", id, got, err, want)
		}
	}
}

func TestEditionSemValorEhOther(t *testing.T) {
	r := newRec(t).on(`reg query HKLM\SOFTWARE\Microsoft\Windows NT\CurrentVersion /v EditionID`, reply{code: 1}).
		on(`reg query HKLM\SOFTWARE\Microsoft\Windows NT\CurrentVersion`, reply{stdout: "\r\nHKEY_LOCAL_MACHINE\\SOFTWARE\\Microsoft\\Windows NT\\CurrentVersion\r\n    Outro    REG_SZ    x\r\n"})
	got, err := newSys(r, false).Edition(ctx)
	if err != nil || got != EditionOther {
		t.Fatalf("got %q err=%v", got, err)
	}
}

// Entradas maliciosas: erro ANTES de executar qualquer coisa.
func TestEntradasInvalidasNaoExecutamNada(t *testing.T) {
	r := newRec(t) // sem roteiro: qualquer execução falha o teste
	s := newSys(r, true)
	badKeys := []string{
		``, `HKCU`, `HKCU\`, `HKEY_CURRENT_USER\Software`, `hkcu\Software\X`, `HKCU\Software\..\X`, `HKCU\Software\X"`,
		`HKCU\Software\X;calc`, "HKCU\\Software\\X\r\n", `HKCU\Software\X /f`, `HKCU\Software\X  Y`, `HKCU\Software\X\`,
		`HKCU\\Software`, `HKCU\Software\ X`, `HKCU\Software\X&calc`, `HKCU\Software\X|y`, `HKCU\Software\%PATH%`, `HKCU\Software/X`,
		"HKCU\\Software\\X\x00",
	}
	badNames := []string{``, `a b`, `a"b`, `a/b`, `-x`, `a;b`, "a\r\nb", `a..b`, `a\b`, `a&b`}
	for _, k := range badKeys {
		if _, _, err := s.RegRead(ctx, k, "N"); !errors.Is(err, ErrEntradaInvalida) {
			t.Errorf("RegRead key %q: err=%v", k, err)
		}
		if err := s.RegWrite(ctx, k, "N", DWORD(1)); !errors.Is(err, ErrEntradaInvalida) {
			t.Errorf("RegWrite key %q: err=%v", k, err)
		}
		if err := s.RegDelete(ctx, k, "N"); !errors.Is(err, ErrEntradaInvalida) {
			t.Errorf("RegDelete key %q: err=%v", k, err)
		}
	}
	for _, n := range badNames {
		if _, _, err := s.RegRead(ctx, `HKCU\Software\X`, n); !errors.Is(err, ErrEntradaInvalida) {
			t.Errorf("RegRead name %q: err=%v", n, err)
		}
		if err := s.RegWrite(ctx, `HKCU\Software\X`, n, DWORD(1)); !errors.Is(err, ErrEntradaInvalida) {
			t.Errorf("RegWrite name %q: err=%v", n, err)
		}
		if err := s.RegDelete(ctx, `HKCU\Software\X`, n); !errors.Is(err, ErrEntradaInvalida) {
			t.Errorf("RegDelete name %q: err=%v", n, err)
		}
		if _, err := s.ServiceRead(ctx, n); !errors.Is(err, ErrEntradaInvalida) {
			t.Errorf("ServiceRead %q: err=%v", n, err)
		}
		if err := s.ServiceConfigure(ctx, n, "disabled"); !errors.Is(err, ErrEntradaInvalida) {
			t.Errorf("ServiceConfigure %q: err=%v", n, err)
		}
		if err := s.ServiceStop(ctx, n); !errors.Is(err, ErrEntradaInvalida) {
			t.Errorf("ServiceStop %q: err=%v", n, err)
		}
		if err := s.ServiceStart(ctx, n); !errors.Is(err, ErrEntradaInvalida) {
			t.Errorf("ServiceStart %q: err=%v", n, err)
		}
	}
	for _, st := range []string{``, `auto`, `demand`, `Disabled`, `disabled;x`, `automatic `} {
		if err := s.ServiceConfigure(ctx, "Svc", st); !errors.Is(err, ErrEntradaInvalida) {
			t.Errorf("startType %q: err=%v", st, err)
		}
	}
	badPaths := []string{
		``, `Tarefa`, `\`, `\T\`, `\T\..\X`, `\T\X"`, `\T\X;calc`, "\\T\\X\r\n", `\T\X /DISABLE`, `\T\X  Y`, `\\T\X`, `\T\ X`, `\T\X&y`, `\T/X`, `\T\%X%`,
	}
	for _, p := range badPaths {
		if _, err := s.TaskRead(ctx, p); !errors.Is(err, ErrEntradaInvalida) {
			t.Errorf("TaskRead %q: err=%v", p, err)
		}
		if err := s.TaskSetEnabled(ctx, p, true); !errors.Is(err, ErrEntradaInvalida) {
			t.Errorf("TaskSetEnabled %q: err=%v", p, err)
		}
	}
	badValues := []RegValue{
		{Kind: RegString, Str: "a\r\nb"}, {Kind: RegString, Str: `x\`}, {Kind: RegExpandString, Str: "a\x00b"}, {Kind: RegExpandString, Str: `%X%\`},
		{Kind: RegKind(99)},
	}
	for _, v := range badValues {
		if err := s.RegWrite(ctx, `HKCU\Software\X`, "N", v); !errors.Is(err, ErrEntradaInvalida) {
			t.Errorf("RegWrite value %+v: err=%v", v, err)
		}
	}
	if len(r.calls) != 0 {
		t.Fatalf("executou %d comando(s) com entrada inválida: %+v", len(r.calls), r.calls)
	}
}

func TestMensagemDeEntradaInvalidaEmPortugues(t *testing.T) {
	_, _, err := newSys(newRec(t), false).RegRead(ctx, `HKCU\Software\..\X`, "N")
	if err == nil || !strings.Contains(err.Error(), "chave do registro") {
		t.Fatalf("err=%v", err)
	}
}

// Nenhuma leitura pode sair com Elevate ou sem ReadOnly; nenhuma escrita com ReadOnly.
func TestLeiturasNuncaElevamEEscritasNuncaSaoReadOnly(t *testing.T) {
	r := newRec(t).
		on(`reg query HKLM\SOFTWARE\X /v N`, reply{stdout: "    N    REG_DWORD    0x1\r\n"}).
		on(`sc qc S`, reply{stdout: "START_TYPE : 3 DEMAND_START"}).
		on(`sc query S`, reply{stdout: "STATE : 4 RUNNING"}).
		on(`schtasks /Query /TN \T\X /XML`, reply{stdout: xmlWith("")}).
		on(`reg add HKLM\SOFTWARE\X /v N /t REG_DWORD /d 1 /f`, reply{}).
		on(`reg delete HKLM\SOFTWARE\X /v N /f`, reply{}).
		on(`sc config S start= disabled`, reply{}).
		on(`sc stop S`, reply{}).
		on(`sc start S`, reply{}).
		on(`schtasks /Change /TN \T\X /DISABLE`, reply{})
	s := newSys(r, true)
	_, _, _ = s.RegRead(ctx, `HKLM\SOFTWARE\X`, "N")
	_, _ = s.ServiceRead(ctx, "S")
	_, _ = s.TaskRead(ctx, `\T\X`)
	nReads := len(r.calls)
	_ = s.RegWrite(ctx, `HKLM\SOFTWARE\X`, "N", DWORD(1))
	_ = s.RegDelete(ctx, `HKLM\SOFTWARE\X`, "N") // faz query antes do delete
	_ = s.ServiceConfigure(ctx, "S", "disabled")
	_ = s.ServiceStop(ctx, "S")
	_ = s.ServiceStart(ctx, "S")
	_ = s.TaskSetEnabled(ctx, `\T\X`, false)
	for i, c := range r.calls {
		isRead := i < nReads || (c.Args[0] == "query" && c.ReadOnly)
		if isRead && (!c.ReadOnly || c.Elevate) {
			t.Errorf("leitura %d com flags erradas: %s %+v", i, lineOf(c), c)
		}
		if !isRead && (c.ReadOnly || !c.Elevate) {
			t.Errorf("escrita %d com flags erradas: %s %+v", i, lineOf(c), c)
		}
	}
}

func TestUsaGuardasDeLeituraEMutacao(t *testing.T) {
	// um runner que recusa tudo que não for do tipo certo seria ErrWrongRunner; aqui garantimos que os Cmds
	// reais passam pelos guardas sem ele.
	r := newRec(t).
		on(`reg query HKCU\Software\X /v N`, reply{stdout: "    N    REG_DWORD    0x1\r\n"}).
		on(`reg add HKCU\Software\X /v N /t REG_DWORD /d 1 /f`, reply{})
	s := newSys(r, false)
	if _, _, err := s.RegRead(ctx, `HKCU\Software\X`, "N"); errors.Is(err, exec.ErrWrongRunner) {
		t.Fatal(err)
	}
	if err := s.RegWrite(ctx, `HKCU\Software\X`, "N", DWORD(1)); errors.Is(err, exec.ErrWrongRunner) {
		t.Fatal(err)
	}
}

// Dois níveis ausentes (ex.: ...\Siuf\Rules e ...\Siuf): sobe até um ancestral listável e prova a ausência da primeira
// subchave faltante.
func TestRegReadChaveEPaiAusentesSubeAteAncestralListavel(t *testing.T) {
	r := newRec(t).
		on(`reg query HKCU\Software\Microsoft\Siuf\Rules /v NumberOfSIUFInPeriod`, reply{code: 1}).
		on(`reg query HKCU\Software\Microsoft\Siuf\Rules`, reply{code: 1}).
		on(`reg query HKCU\Software\Microsoft\Siuf`, reply{code: 1}).
		on(`reg query HKCU\Software\Microsoft`, reply{stdout: "\r\nHKEY_CURRENT_USER\\Software\\Microsoft\r\nHKEY_CURRENT_USER\\Software\\Microsoft\\Windows\r\n"})
	_, ok, err := newSys(r, false).RegRead(ctx, `HKCU\Software\Microsoft\Siuf\Rules`, "NumberOfSIUFInPeriod")
	if err != nil || ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
}

// O ancestral lista a primeira subchave faltante, mas ela não é legível: erro (acesso negado), nunca ausência.
func TestRegReadAncestralListaAChaveMasNaoELegivelEErro(t *testing.T) {
	r := newRec(t).
		on(`reg query HKLM\SOFTWARE\Policies\A\B /v N`, reply{code: 1}).
		on(`reg query HKLM\SOFTWARE\Policies\A\B`, reply{code: 1}).
		on(`reg query HKLM\SOFTWARE\Policies\A`, reply{code: 1, stderr: "ERROR: Access is denied.\r\n"}).
		on(`reg query HKLM\SOFTWARE\Policies`, reply{stdout: "\r\nHKEY_LOCAL_MACHINE\\SOFTWARE\\Policies\r\nHKEY_LOCAL_MACHINE\\SOFTWARE\\Policies\\a\r\n"})
	if _, ok, err := newSys(r, false).RegRead(ctx, `HKLM\SOFTWARE\Policies\A\B`, "N"); err == nil || ok {
		t.Fatalf("esperava erro, ok=%v err=%v", ok, err)
	}
}

// A subida é limitada: sem ancestral listável dentro do limite, erro (não prova ausência).
func TestRegReadSubidaLimitada(t *testing.T) {
	key := `HKCU\A\B\C\D\E\F\G\H\I\J\K`
	r := newRec(t).on(`reg query `+key+` /v N`, reply{code: 1})
	p := key
	for i := 0; i < 12; i++ {
		r.on(`reg query `+p, reply{code: 1})
		if j := strings.LastIndex(p, `\`); j > 0 {
			p = p[:j]
		}
	}
	if _, ok, err := newSys(r, false).RegRead(ctx, key, "N"); err == nil || ok {
		t.Fatalf("esperava erro, ok=%v err=%v", ok, err)
	}
}

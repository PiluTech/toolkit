package usb

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/PiluTech/toolkit/internal/exec"
)

// winSim é um Windows SIMULADO por trás do exec.Runner: interpreta só os comandos que o toolkit monta (reg.exe e os scripts
// fixos do PowerShell), guarda o estado em memória e registra TODA chamada para o teste do contrato. Nada real é tocado.
type winSim struct {
	t       *testing.T
	now     time.Time
	edition string
	devs    []Device
	tree    RegTree
	task    *simTask
	locked  bool // pasta protegida criada
	calls   []exec.Cmd
	failPS  string // trecho do script que falha
	badSDDL bool
}

type simTask struct {
	minutes int
	next    time.Time
}

const goodSDDL = `O:BAG:SYD:PAI(A;OICI;FA;;;SY)(A;OICI;FA;;;BA)`

func newSim(t *testing.T) *winSim {
	return &winSim{t: t, now: t0, edition: "Professional", devs: machine(usbKeyboard("0001"), usbPendrive("0003")), tree: RegTree{Entries: map[string]RegValue{}}}
}

func (s *winSim) LookPath(string) (string, error) { return "", os.ErrNotExist }

func ok(out string) (exec.Result, error) { return exec.Result{Stdout: []byte(out)}, nil }
func code(c int, out string) (exec.Result, error) {
	return exec.Result{ExitCode: c, Stdout: []byte(out)}, nil
}

func (s *winSim) Run(_ context.Context, c exec.Cmd) (exec.Result, error) {
	s.calls = append(s.calls, c)
	switch c.Name {
	case regExe:
		return s.reg(c.Args)
	case psExe:
		if len(c.Args) != 4 || c.Args[0] != "-NoProfile" || c.Args[1] != "-NonInteractive" || c.Args[2] != "-Command" {
			s.t.Errorf("argumentos do powershell inesperados: %v", c.Args)
			return code(1, "")
		}
		return s.ps(c.Args[3])
	}
	s.t.Errorf("executável inesperado: %s", c.Name)
	return code(1, "")
}

func (s *winSim) reg(a []string) (exec.Result, error) {
	switch a[0] {
	case "query":
		key := a[1]
		if key == editionKey {
			return ok(regRootName(editionKey) + "\r\n    EditionID    REG_SZ    " + s.edition + "\r\n")
		}
		if key == RestrictionsKey {
			if !s.tree.KeyExists {
				return code(1, "")
			}
			return ok(s.renderTree())
		}
		if key == restrictionsDir {
			return code(1, "") // DeviceInstall não existe
		}
		return ok(regRootName(key) + "\r\n" + regRootName(key) + `\Outra` + "\r\n") // ancestral listável, sem DeviceInstall
	case "add":
		rel := strings.TrimPrefix(strings.TrimPrefix(a[1], RestrictionsKey), `\`)
		name := a[3]
		path := name
		if rel != "" {
			path = rel + `\` + name
		}
		var v RegValue
		if a[5] == "REG_DWORD" {
			n, _ := strconv.Atoi(a[7])
			v = DWORD(uint32(n))
		} else {
			v = String(a[7])
		}
		s.tree.KeyExists = true
		if rel != "" {
			f := &fakeSys{tree: s.tree}
			f.addSubkey(rel)
			s.tree.Subkeys = f.tree.Subkeys
		}
		s.tree.Entries[path] = v
		return ok("")
	case "delete":
		rel := strings.TrimPrefix(strings.TrimPrefix(a[1], RestrictionsKey), `\`)
		if len(a) >= 5 && a[2] == "/v" {
			path := a[3]
			if rel != "" {
				path = rel + `\` + a[3]
			}
			delete(s.tree.Entries, path)
			return ok("")
		}
		if rel == "" {
			s.tree.KeyExists = false
		} else {
			var keep []string
			for _, k := range s.tree.Subkeys {
				if k != rel {
					keep = append(keep, k)
				}
			}
			s.tree.Subkeys = keep
		}
		return ok("")
	case "export":
		return ok("")
	}
	s.t.Errorf("reg.exe com verbo inesperado: %v", a)
	return code(1, "")
}

func (s *winSim) renderTree() string {
	var b strings.Builder
	root := regRootName(RestrictionsKey)
	b.WriteString(root + "\r\n")
	emit := func(prefix string) {
		for k, v := range s.tree.Entries {
			dir, name := "", k
			if i := strings.LastIndex(k, `\`); i >= 0 {
				dir, name = k[:i], k[i+1:]
			}
			if dir != prefix {
				continue
			}
			if v.Kind == 0 {
				fmt.Fprintf(&b, "    %s    REG_DWORD    0x%x\r\n", name, v.DWORD)
			} else {
				fmt.Fprintf(&b, "    %s    REG_SZ    %s\r\n", name, v.Str)
			}
		}
		b.WriteString("\r\n")
	}
	emit("")
	for _, sub := range s.tree.Subkeys {
		b.WriteString(root + `\` + sub + "\r\n")
		emit(sub)
	}
	return b.String()
}

func (s *winSim) ps(script string) (exec.Result, error) {
	if s.failPS != "" && strings.Contains(script, s.failPS) {
		return exec.Result{ExitCode: 1, Stderr: []byte("falha simulada")}, nil
	}
	switch {
	case strings.Contains(script, "Get-PnpDevice"):
		env := map[string]any{"count": len(s.devs)}
		var raws []map[string]any
		for _, d := range s.devs {
			raws = append(raws, map[string]any{"InstanceId": d.InstanceID, "Parent": d.Parent, "Name": d.Name, "Class": d.Class, "ClassGuid": d.ClassGUID, "HardwareIds": d.HardwareIDs, "CompatibleIds": d.CompatibleIDs})
		}
		env["devices"] = raws
		b, _ := json.Marshal(env)
		return ok(string(b))
	case strings.Contains(script, "SetAccessRuleProtection"):
		s.locked = true
		return ok("")
	case strings.Contains(script, "GetSecurityDescriptorSddlForm"):
		sddl := goodSDDL
		if s.badSDDL {
			sddl = `O:BAG:SYD:AI(A;OICI;FA;;;SY)(A;OICI;FA;;;BA)(A;OICI;0x1200a9;;;BU)`
		}
		return ok(fmt.Sprintf(`{"reparse":false,"sddl":%q}`, sddl))
	case strings.Contains(script, "Register-ScheduledTask"):
		i := strings.Index(script, "AddMinutes(")
		j := strings.Index(script[i:], ")")
		n, _ := strconv.Atoi(script[i+len("AddMinutes(") : i+j])
		s.task = &simTask{minutes: n, next: s.now.Add(time.Duration(n) * time.Minute)}
		return ok("")
	case strings.Contains(script, "Unregister-ScheduledTask"):
		s.task = nil
		return ok("")
	case strings.Contains(script, "Get-ScheduledTask"):
		if s.task == nil {
			return ok(`{"exists":false}`)
		}
		return ok(fmt.Sprintf(`{"exists":true,"enabled":true,"exe":%q,"args":%q,"user":"S-1-5-18","next":%q,"actions":1,"triggers":1}`, taskExe(), RevertArgs, s.task.next.Format(time.RFC3339Nano)))
	}
	s.t.Errorf("script inesperado: %.80s", script)
	return code(1, "")
}

func simSystem(t *testing.T, s *winSim) (*windowsSystem, FileStore) {
	t.Helper()
	dir := t.TempDir()
	w := NewWindowsSystem(s, func() bool { return true }).(*windowsSystem)
	w.dir = dir
	w.stage = func(string) (StagedBinary, error) {
		return StagedBinary{Path: taskExe(), SourceSHA: strings.Repeat("c", 64), CopySHA: strings.Repeat("c", 64)}, nil
	}
	return w, FileStore{Dir: dir}
}

// --- contrato dos comandos ---

func TestScriptsSaoASCIISemAspasDuplas(t *testing.T) {
	scripts := map[string]string{"protectDir": protectDirScript, "readDir": readDirScript, "armTask": armTaskScript(10), "readTask": readTaskScript, "deleteTask": deleteTaskScript, "devices": readDevicesScript}
	for name, sc := range scripts {
		if err := asciiOnly(sc); err != nil {
			t.Errorf("%s: %v", name, err)
		}
		if strings.Contains(sc, `"`) {
			t.Errorf("%s tem aspas duplas (o Go as escapa na linha de comando; scripts não precisam delas)", name)
		}
	}
	if asciiOnly("Get-PnpDevice \u00e7") == nil || asciiOnly("a\x00b") == nil {
		t.Error("asciiOnly deveria recusar não-ASCII e NUL")
	}
}

func TestContratoFluxoCompletoAplicarConfirmarDesfazer(t *testing.T) {
	sim := newSim(t)
	w, st := simSystem(t, sim)
	if _, err := Apply(context.Background(), w, st, ApplyOptions{Prazo: 10, Now: t0}); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if sim.task == nil || sim.task.minutes != 10 || !sim.locked {
		t.Fatalf("estado simulado: %#v", sim.task)
	}
	if err := Confirm(context.Background(), w, st); err != nil {
		t.Fatalf("Confirm: %v", err)
	}
	if sim.task != nil {
		t.Fatal("a tarefa deveria ter saído")
	}
	if _, err := Undo(context.Background(), w, st); err != nil {
		t.Fatalf("Undo: %v", err)
	}
	if sim.tree.KeyExists || len(sim.tree.Entries) != 0 {
		t.Fatalf("o registro não voltou ao original: %v", sim.tree)
	}

	var regWrites, taskOps int
	for _, c := range sim.calls {
		if c.Elevate {
			t.Errorf("Elevate em %s %v: o toolkit nunca eleva sozinho", c.Name, c.Args)
		}
		if c.Name != regExe && c.Name != psExe {
			t.Errorf("executável fora do contrato: %s", c.Name)
		}
		if !strings.HasPrefix(c.Name, `C:\Windows\System32\`) {
			t.Errorf("caminho não absoluto: %s", c.Name)
		}
		if c.Name == regExe {
			verb := c.Args[0]
			switch verb {
			case "add", "delete", "export":
				if c.ReadOnly {
					t.Errorf("mutação marcada ReadOnly: %v", c.Args)
				}
				if verb != "export" {
					regWrites++
				}
				if !strings.HasPrefix(c.Args[1], RestrictionsKey) { // sensível a maiúsculas, prefixo exato
					t.Errorf("escrita no registro fora de Restrictions: %v", c.Args)
				}
				if len(c.Args[1]) > len(RestrictionsKey) && c.Args[1][len(RestrictionsKey)] != '\\' {
					t.Errorf("chave fora de Restrictions (prefixo parcial): %v", c.Args)
				}
			case "query":
				if !c.ReadOnly {
					t.Errorf("leitura sem ReadOnly: %v", c.Args)
				}
			default:
				t.Errorf("verbo do reg.exe fora do contrato: %v", c.Args)
			}
			for _, a := range c.Args {
				if strings.HasPrefix(a, "HKCU") || strings.Contains(a, "HKEY_CURRENT_USER") || strings.Contains(a, "CurrentControlSet") {
					t.Errorf("chave fora do contrato: %v", c.Args)
				}
			}
		}
		if c.Name == psExe {
			script := c.Args[3]
			if err := asciiOnly(script); err != nil {
				t.Errorf("script não ASCII: %v", err)
			}
			mut := strings.Contains(script, "Register-ScheduledTask") || strings.Contains(script, "Unregister-ScheduledTask") || strings.Contains(script, "Set-Acl")
			if mut == c.ReadOnly {
				t.Errorf("ReadOnly=%v incoerente com o script %.60s", c.ReadOnly, script)
			}
			if strings.Contains(script, "Register-ScheduledTask") || strings.Contains(script, "Unregister-ScheduledTask") || strings.Contains(script, "Get-ScheduledTask") {
				taskOps++
				if !strings.Contains(script, "'"+TaskName+"'") || !strings.Contains(script, "'"+TaskPath+"'") {
					t.Errorf("operação de tarefa que não é só a do toolkit: %.100s", script)
				}
				if strings.Contains(script, "-Force") || strings.Contains(script, "Get-ScheduledTask |") || strings.Contains(script, "-TaskName *") {
					t.Errorf("operação ampla/forçada de tarefa: %.100s", script)
				}
			}
			for _, bad := range []string{"Invoke-Expression", "iex ", "Start-Process", "DownloadString", "Remove-Item", "-ExecutionPolicy", "Restart-Computer", "shutdown"} {
				if strings.Contains(script, bad) {
					t.Errorf("script usa %q", bad)
				}
			}
		}
	}
	if regWrites == 0 || taskOps == 0 {
		t.Fatalf("o fluxo não exercitou escritas (%d) e tarefa (%d)", regWrites, taskOps)
	}
}

func TestContratoNenhumaEscritaEmDenyUnspecifiedNemClassesAmplas(t *testing.T) {
	sim := newSim(t)
	w, st := simSystem(t, sim)
	if _, err := Apply(context.Background(), w, st, ApplyOptions{Prazo: 10, Now: t0}); err != nil {
		t.Fatal(err)
	}
	for _, c := range sim.calls {
		if c.Name != regExe || c.Args[0] != "add" {
			continue
		}
		line := strings.Join(c.Args, " ")
		for _, bad := range []string{"DenyUnspecified", "DenyDeviceIDs", "Retroactive", strings.ToLower(guidKeyboard), strings.ToLower(guidMouse), strings.ToLower(guidHID), strings.ToLower(guidDisk)} {
			if strings.Contains(strings.ToLower(line), strings.ToLower(bad)) {
				t.Errorf("escrita com %q: %s", bad, line)
			}
		}
	}
}

func TestContratoSemAdminNenhumComandoDeMutacao(t *testing.T) {
	sim := newSim(t)
	w, st := simSystem(t, sim)
	w.elevated = func() bool { return false }
	if _, err := Apply(context.Background(), w, st, ApplyOptions{Prazo: 10, Now: t0}); err == nil {
		t.Fatal("devia recusar")
	}
	if len(sim.calls) != 1 { // só a leitura da edição
		t.Fatalf("chamadas = %d (só a edição pode ser lida antes de recusar): %v", len(sim.calls), sim.calls)
	}
}

func TestContratoPlanoSoLeitura(t *testing.T) {
	sim := newSim(t)
	w, _ := simSystem(t, sim)
	if _, err := BuildPlan(context.Background(), w, nil); err != nil {
		t.Fatal(err)
	}
	for _, c := range sim.calls {
		if !c.ReadOnly || c.Elevate {
			t.Errorf("o plano executou algo que não é leitura: %v %v", c.Name, c.Args)
		}
	}
}

func TestContratoACLAmplaAbortaAntesDeQualquerChave(t *testing.T) {
	sim := newSim(t)
	sim.badSDDL = true
	w, st := simSystem(t, sim)
	if _, err := Apply(context.Background(), w, st, ApplyOptions{Prazo: 10, Now: t0}); err == nil {
		t.Fatal("ACL ampla devia abortar")
	}
	for _, c := range sim.calls {
		if c.Name == regExe && c.Args[0] != "query" {
			t.Fatalf("escreveu no registro com a ACL ampla: %v", c.Args)
		}
	}
	if sim.task != nil {
		t.Fatal("criou tarefa com a ACL ampla")
	}
}

func TestContratoFalhaDoPowerShellAoArmarAbortaSemChaves(t *testing.T) {
	sim := newSim(t)
	sim.failPS = "Register-ScheduledTask"
	w, st := simSystem(t, sim)
	if _, err := Apply(context.Background(), w, st, ApplyOptions{Prazo: 10, Now: t0}); err == nil {
		t.Fatal("devia falhar")
	}
	for _, c := range sim.calls {
		if c.Name == regExe && c.Args[0] == "add" {
			t.Fatalf("escreveu chave sem a tarefa armada: %v", c.Args)
		}
	}
	if sn, _ := st.Load(); sn != nil {
		t.Error("backup deste Apply ficou")
	}
}

// --- unidades da implementação real ---

func TestEditionMapeamento(t *testing.T) {
	for id, want := range map[string]string{"Professional": EditionPro, "ProfessionalWorkstation": EditionPro, "Enterprise": EditionEnterprise, "EnterpriseS": EditionEnterprise,
		"Education": EditionEducation, "Core": EditionHome, "CoreN": EditionHome, "CoreSingleLanguage": EditionHome, "ServerStandard": EditionOther, "IoTEnterprise": EditionOther, "": EditionOther} {
		sim := newSim(t)
		sim.edition = id
		w, _ := simSystem(t, sim)
		got, err := w.Edition(context.Background())
		if err != nil || got != want {
			t.Errorf("%q -> %q (%v), queria %q", id, got, err, want)
		}
	}
}

func TestReadTreeAusenciaProvadaEAcessoNegado(t *testing.T) {
	sim := newSim(t)
	w, _ := simSystem(t, sim)
	tr, err := w.ReadTree(context.Background())
	if err != nil || tr.KeyExists || len(tr.Entries) != 0 {
		t.Fatalf("ausente: %v %v", tr, err)
	}
	// acesso negado: reg query sai 1 e o ancestral LISTA a chave => existe mas não abriu
	denied := &winSim{t: t, tree: RegTree{}}
	rn := &denyRunner{winSim: denied}
	w2 := NewWindowsSystem(rn, nil)
	if _, err := w2.ReadTree(context.Background()); err == nil {
		t.Fatal("acesso negado não pode virar 'ausente'")
	}
	// nenhum ancestral lista: não prova nada
	rn2 := &noProofRunner{winSim: denied}
	if _, err := NewWindowsSystem(rn2, nil).ReadTree(context.Background()); err == nil {
		t.Fatal("sem prova de ausência tem de ser erro")
	}
}

type denyRunner struct{ *winSim }

func (d *denyRunner) Run(_ context.Context, c exec.Cmd) (exec.Result, error) {
	switch c.Args[1] {
	case RestrictionsKey:
		return exec.Result{ExitCode: 1}, nil
	case restrictionsDir:
		return exec.Result{Stdout: []byte(regRootName(restrictionsDir) + "\r\n" + regRootName(RestrictionsKey) + "\r\n")}, nil
	}
	return exec.Result{ExitCode: 1}, nil
}

type noProofRunner struct{ *winSim }

func (d *noProofRunner) Run(context.Context, exec.Cmd) (exec.Result, error) {
	return exec.Result{ExitCode: 1}, nil
}

func TestParseRegListTiposENomes(t *testing.T) {
	out := regRootName(RestrictionsKey) + "\r\n    (Default)    REG_SZ    (value not set)\r\n    AllowDenyLayered    REG_DWORD    0x1\r\n\r\n" +
		regRootName(RestrictionsKey) + `\AllowDeviceIDs` + "\r\n    1    REG_SZ    USB\\VID_FFFF&PID_0001\r\n    2    REG_EXPAND_SZ    %X%\r\n"
	vals, subs, err := parseRegList(out, RestrictionsKey)
	if err != nil {
		t.Fatal(err)
	}
	if vals["AllowDenyLayered"].DWORD != 1 || vals[`AllowDeviceIDs\1`].Str != `USB\VID_FFFF&PID_0001` || vals[`AllowDeviceIDs\2`].Str != "%X%" || len(subs) != 1 || subs[0] != "AllowDeviceIDs" {
		t.Fatalf("vals=%v subs=%v", vals, subs)
	}
	for name, bad := range map[string]string{
		"multi_sz":  regRootName(RestrictionsKey) + "\r\n    X    REG_MULTI_SZ    a\\0b\r\n",
		"binary":    regRootName(RestrictionsKey) + "\r\n    X    REG_BINARY    00FF\r\n",
		"default":   regRootName(RestrictionsKey) + "\r\n    (Default)    REG_SZ    algo\r\n",
		"cabecalho": "HKEY_CURRENT_USER\\Software\\X\r\n    A    REG_DWORD    0x1\r\n",
		"dword":     regRootName(RestrictionsKey) + "\r\n    A    REG_DWORD    zz\r\n",
		"formato":   regRootName(RestrictionsKey) + "\r\n    lixo sem tipo\r\n",
	} {
		if _, _, err := parseRegList(bad, RestrictionsKey); err == nil {
			t.Errorf("%s: aceitou", name)
		}
	}
}

func TestRegWriteEDeleteRecusamForaDaListaSemExecutar(t *testing.T) {
	sim := newSim(t)
	w, _ := simSystem(t, sim)
	for _, p := range []string{"DenyUnspecified", `DenyDeviceIDs\1`, `..\x`, `AllowDeviceIDs\0`} {
		if err := w.RegWrite(context.Background(), p, DWORD(1)); err == nil {
			t.Errorf("RegWrite(%q) aceito", p)
		}
		if err := w.RegDelete(context.Background(), p); err == nil {
			t.Errorf("RegDelete(%q) aceito", p)
		}
	}
	if err := w.RegWrite(context.Background(), "AllowDenyLayered", DWORD(0)); err == nil {
		t.Error("valor 0 na camada aceito")
	}
	if err := w.KeyDelete(context.Background(), "Run"); err == nil {
		t.Error("KeyDelete fora da lista aceito")
	}
	if len(sim.calls) != 0 {
		t.Fatalf("executou comandos mesmo com entrada inválida: %v", sim.calls)
	}
}

func TestKeyDeleteSoQuandoVazia(t *testing.T) {
	sim := newSim(t)
	sim.tree = RegTree{KeyExists: true, Subkeys: []string{"AllowDeviceIDs"}, Entries: map[string]RegValue{`AllowDeviceIDs\1`: String(`USB\VID_AAAA&PID_0001`)}}
	w, _ := simSystem(t, sim)
	if err := w.KeyDelete(context.Background(), "AllowDeviceIDs"); err == nil {
		t.Fatal("apagou subchave com valor dentro")
	}
	if err := w.KeyDelete(context.Background(), ""); err == nil {
		t.Fatal("apagou Restrictions com conteúdo")
	}
	for _, c := range sim.calls {
		if c.Name == regExe && c.Args[0] == "delete" {
			t.Fatalf("executou delete: %v", c.Args)
		}
	}
}

func TestArmTaskRecusaComandoQueNaoEODoToolkit(t *testing.T) {
	sim := newSim(t)
	w, _ := simSystem(t, sim)
	for _, spec := range []TaskSpec{
		{Exe: `C:\Users\Public\x.exe`, Args: RevertArgs, Minutes: 10},
		{Exe: taskExe(), Args: "usb desfazer --yes", Minutes: 10},
		{Exe: taskExe(), Args: RevertArgs, Minutes: 1},
		{Exe: taskExe(), Args: RevertArgs, Minutes: 61},
		{Exe: taskExe(), Args: RevertArgs + "; calc", Minutes: 10},
	} {
		if err := w.ArmTask(context.Background(), spec); err == nil {
			t.Errorf("aceitou %v", spec)
		}
	}
	if len(sim.calls) != 0 {
		t.Fatalf("executou: %v", sim.calls)
	}
	if err := w.ArmTask(context.Background(), TaskSpec{Exe: taskExe(), Args: RevertArgs, Minutes: 7}); err != nil {
		t.Fatal(err)
	}
	sc := sim.calls[0].Args[3]
	for _, want := range []string{"AddMinutes(7)", "S-1-5-18", "-StartWhenAvailable", "'" + taskExe() + "'", "'" + RevertArgs + "'"} {
		if !strings.Contains(sc, want) {
			t.Errorf("script sem %q:\n%s", want, sc)
		}
	}
}

func TestReadTaskVariacoes(t *testing.T) {
	run := func(out string) (TaskInfo, error) {
		rn := &psOut{out: out}
		return NewWindowsSystem(rn, nil).ReadTask(context.Background())
	}
	ti, err := run(`{"exists":false}`)
	if err != nil || ti.Exists {
		t.Fatalf("ausente: %v %v", ti, err)
	}
	ti, err = run(`{"exists":true,"enabled":true,"exe":"C:\\x.exe","args":"usb reverter","user":"SYSTEM","next":"2026-11-14T12:10:00.0000000Z","actions":1,"triggers":1}`)
	if err != nil || !ti.Exists || !ti.Enabled || ti.User != "SYSTEM" || !ti.Next.Equal(t0.Add(10*time.Minute)) {
		t.Fatalf("ti=%#v err=%v", ti, err)
	}
	ti, _ = run(`{"exists":true,"enabled":true,"exe":"C:\\x.exe","args":"usb reverter","user":"SYSTEM","next":null,"actions":2,"triggers":1}`)
	if !ti.Next.IsZero() || !strings.Contains(ti.Exe, "ação") {
		t.Fatalf("duas ações devem virar programa que não confere: %#v", ti)
	}
	if _, err := run(`not json`); err == nil {
		t.Fatal("lixo aceito")
	}
	if _, err := run(`{"exists":true,"next":"ontem"}`); err == nil {
		t.Fatal("data ilegível aceita")
	}
}

type psOut struct{ out string }

func (p *psOut) Run(context.Context, exec.Cmd) (exec.Result, error) {
	return exec.Result{Stdout: []byte(p.out)}, nil
}
func (p *psOut) LookPath(string) (string, error) { return "", os.ErrNotExist }

func TestCheckDirSDDL(t *testing.T) {
	if err := checkDirSDDL(goodSDDL); err != nil {
		t.Fatal(err)
	}
	if err := checkDirSDDL(`O:BAG:SYD:PAI(A;OICI;FA;;;BA)(A;OICI;FA;;;SY)`); err != nil {
		t.Fatalf("ordem das ACEs não importa: %v", err)
	}
	for name, s := range map[string]string{
		"dono Users":      `O:BUG:SYD:PAI(A;OICI;FA;;;SY)(A;OICI;FA;;;BA)`,
		"sem protecao":    `O:BAG:SYD:AI(A;OICI;FA;;;SY)(A;OICI;FA;;;BA)`,
		"ACE de Users":    `O:BAG:SYD:PAI(A;OICI;FA;;;SY)(A;OICI;FA;;;BA)(A;OICI;0x1200a9;;;BU)`,
		"Everyone":        `O:BAG:SYD:PAI(A;OICI;FA;;;SY)(A;OICI;FA;;;BA)(A;OICI;FA;;;WD)`,
		"Authenticated":   `O:BAG:SYD:PAI(A;OICI;FA;;;SY)(A;OICI;FA;;;BA)(A;OICI;FA;;;AU)`,
		"so SYSTEM":       `O:BAG:SYD:PAI(A;OICI;FA;;;SY)`,
		"sem heranca":     `O:BAG:SYD:PAI(A;;FA;;;SY)(A;;FA;;;BA)`,
		"direito parcial": `O:BAG:SYD:PAI(A;OICI;0x1200a9;;;SY)(A;OICI;FA;;;BA)`,
		"negacao":         `O:BAG:SYD:PAI(D;OICI;FA;;;SY)(A;OICI;FA;;;BA)`,
		"vazio":           ``,
	} {
		if err := checkDirSDDL(s); err == nil {
			t.Errorf("%s: aceitou %q", name, s)
		}
	}
}

func TestProtectDirRecusaPontoDeReparseEPastaInesperada(t *testing.T) {
	sim := newSim(t)
	w, _ := simSystem(t, sim)
	if err := w.ProtectDir(context.Background(), `C:\Outra`); err == nil {
		t.Fatal("pasta inesperada aceita")
	}
	rn := &psOut{out: `{"reparse":true,"sddl":"` + goodSDDL + `"}`}
	w2 := NewWindowsSystem(rn, nil).(*windowsSystem)
	if err := w2.ProtectDir(context.Background(), ProtectedDir); err == nil || !strings.Contains(err.Error(), "reparse") {
		t.Fatalf("ponto de reparse aceito: %v", err)
	}
}

func TestCopyAndHashConfereACopia(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(t.TempDir(), "toolkit.exe")
	if err := os.WriteFile(src, []byte("binario sintetico"), 0o700); err != nil {
		t.Fatal(err)
	}
	sb, err := copyAndHash(src, dir)
	if err != nil {
		t.Fatal(err)
	}
	if sb.SourceSHA == "" || sb.SourceSHA != sb.CopySHA {
		t.Fatalf("hashes: %#v", sb)
	}
	b, _ := os.ReadFile(filepath.Join(dir, RevertBinary))
	if string(b) != "binario sintetico" {
		t.Fatalf("cópia = %q", b)
	}
	ents, _ := os.ReadDir(dir)
	for _, e := range ents {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Errorf("temporário sobrou: %s", e.Name())
		}
	}
}

func TestDevicesLeitura(t *testing.T) {
	sim := newSim(t)
	w, _ := simSystem(t, sim)
	devs, err := w.Devices(context.Background())
	if err != nil || len(devs) != len(sim.devs) {
		t.Fatalf("devs=%d err=%v", len(devs), err)
	}
	c := sim.calls[0]
	if !c.ReadOnly || c.Elevate || c.Name != psExe {
		t.Fatalf("cmd = %#v", c)
	}
	if !strings.Contains(c.Args[3], "-PresentOnly") || !strings.Contains(c.Args[3], "DEVPKEY_Device_Parent") {
		t.Fatal("o script não lê os presentes com o pai")
	}
}

func TestContratoChaveVaziaPreExistenteExportaOReg(t *testing.T) {
	sim := newSim(t)
	sim.tree = RegTree{KeyExists: true, Entries: map[string]RegValue{}}
	w, st := simSystem(t, sim)
	// o .reg de verdade seria gravado pelo reg.exe; aqui o simulador não cria o arquivo: grava um sintético no lugar
	if err := os.WriteFile(st.RegPath(), []byte("Windows Registry Editor Version 5.00\r\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Apply(context.Background(), w, st, ApplyOptions{Prazo: 10, Now: t0}); err != nil {
		t.Fatal(err)
	}
	var exported bool
	for _, c := range sim.calls {
		if c.Name == regExe && c.Args[0] == "export" {
			exported = true
			if c.Args[1] != RestrictionsKey || c.ReadOnly || c.Elevate || !strings.HasSuffix(c.Args[2], RegFile) || c.Args[3] != "/y" {
				t.Errorf("export fora do contrato: %v", c.Args)
			}
		}
	}
	if !exported {
		t.Fatal("a chave existia: o .reg de segurança tinha de ser exportado")
	}
}

func TestExportRegSoParaOArquivoDaPastaProtegida(t *testing.T) {
	sim := newSim(t)
	w, _ := simSystem(t, sim)
	for _, f := range []string{`C:\Users\Public\usb-restrictions-backup.reg`, w.dir + `\outro.reg`, `..\usb-restrictions-backup.reg`, w.dir + `\sub\` + RegFile, ""} {
		if err := w.ExportReg(context.Background(), f); err == nil {
			t.Errorf("aceitou exportar para %q", f)
		}
	}
	if len(sim.calls) != 0 {
		t.Fatalf("executou: %v", sim.calls)
	}
}

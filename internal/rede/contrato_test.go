package rede

import (
	"context"
	"encoding/base64"
	"os"
	"path"
	"regexp"
	"strings"
	"testing"
	"unicode/utf16"

	"github.com/PiluTech/toolkit/internal/diagfmt"
	"github.com/PiluTech/toolkit/internal/exec"
)

// Contrato da função de rede (docs/rede.md): só leitura, sem Elevate, sem varredura, sem escrita, sem internet, e todo
// comando numa LISTA FECHADA. O FakeRunner falha o teste com qualquer comando sem roteiro, então Calls() é o conjunto COMPLETO
// do que o pacote emite com fixtures completas (nas duas formas: sem e com administrador).

var linAllowed = map[string]bool{
	"ip -j addr": true, "ip -j route show default": true, "ip -j -6 route show default": true,
	"nmcli -t -f ACTIVE,SSID,SECURITY dev wifi list --rescan no": true,
	"resolvectl status": true, "ss -H -t -u -l -n -p": true, "ufw status": true, "firewall-cmd --state": true, "nft list ruleset": true,
}

var macAllowed = map[string]bool{
	"networksetup -listallhardwareports": true, "ifconfig -a": true, "netstat -rn -f inet": true, "netstat -rn -f inet6": true,
	"ipconfig getsummary en0": true, "scutil --dns": true, "netstat -an -p tcp": true, "lsof -nP -iTCP -sTCP:LISTEN": true,
	"socketfilterfw --getglobalstate": true, "sysctl -n net.inet6.ip6.use_tempaddr": true, "pfctl -s info": true,
}

// cmdlets que os scripts do Windows podem usar: só Get-* (e os de pipeline/JSON).
var psAllowed = map[string]bool{
	"Get-NetAdapter": true, "Get-NetIPAddress": true, "Get-NetRoute": true, "Get-NetConnectionProfile": true,
	"Get-DnsClientServerAddress": true, "Get-Command": true, "Get-DnsClientDohServerAddress": true, "Get-Process": true,
	"Get-NetTCPConnection": true, "Get-NetUDPEndpoint": true, "Get-NetFirewallProfile": true, "Get-NetIPv6Protocol": true,
	"ForEach-Object": true, "Where-Object": true, "ConvertTo-Json": true,
}

var psBanned = []string{"Set-", "New-", "Remove-", "Enable-", "Disable-", "Add-", "Start-", "Stop-", "Restart-", "Invoke-", "Register-", "Clear-", "Export-", "Out-File", "Send-", "Test-NetConnection", "Test-Connection", "Resolve-DnsName", "Invoke-WebRequest", "netsh", "Start-Process"}

var cmdletRe = regexp.MustCompile(`\b[A-Z][a-z]+-[A-Z][A-Za-z0-9]+`)

// banidos em QUALQUER linha de comando (varredura, segredo, escrita, ferramentas de sondagem).
var cmdBanned = []string{"scan", "show-secrets", "key=clear", "show profile", "show networks", "setverbose", "nmap", "ping", "arp ", "curl", "wget", "traceroute", "-Sy", " set ", " add ", " delete ", "--rescan yes"}

// checkBanned reprova qualquer termo de varredura, segredo ou escrita. A única ocorrência de "scan" permitida é a do
// "--rescan no" do nmcli (a lista em cache, sem varredura).
func checkBanned(t *testing.T, line string) {
	t.Helper()
	l := strings.ReplaceAll(line, "--rescan no", "")
	for _, b := range cmdBanned {
		if strings.Contains(" "+l+" ", b) {
			t.Errorf("termo proibido %q em: %s", b, line)
		}
	}
}

func decodePS(t *testing.T, args []string) string {
	t.Helper()
	for i, a := range args {
		if a == "-EncodedCommand" && i+1 < len(args) {
			raw, err := base64.StdEncoding.DecodeString(args[i+1])
			if err != nil || len(raw)%2 != 0 {
				t.Fatalf("EncodedCommand inválido: %v", err)
			}
			u := make([]uint16, len(raw)/2)
			for j := range u {
				u[j] = uint16(raw[2*j]) | uint16(raw[2*j+1])<<8
			}
			return string(utf16.Decode(u))
		}
	}
	t.Fatalf("comando do PowerShell sem -EncodedCommand: %v", args)
	return ""
}

func checkReadOnlyCalls(t *testing.T, label string, calls []exec.Cmd) {
	t.Helper()
	if len(calls) == 0 {
		t.Fatalf("%s: nenhum comando emitido", label)
	}
	for _, c := range calls {
		if !c.ReadOnly {
			t.Errorf("%s: %s %v não é ReadOnly", label, c.Name, c.Args)
		}
		if c.Elevate {
			t.Errorf("%s: %s tem Elevate", label, c.Name)
		}
		if c.Stdin != nil || c.Secret {
			t.Errorf("%s: %s usa stdin/segredo", label, c.Name)
		}
	}
}

func TestContratoLinux(t *testing.T) {
	for _, admin := range []bool{false, true} {
		_, f := newLinFX().run(t, admin)
		checkReadOnlyCalls(t, "linux", f.Calls())
		for _, c := range f.Calls() {
			line := path.Base(c.Name) + " " + strings.Join(c.Args, " ")
			if !linAllowed[line] {
				t.Errorf("admin=%v: comando fora da lista fechada: %s", admin, line)
			}
			if !path.IsAbs(c.Name) {
				t.Errorf("caminho do executável deve ser absoluto: %s", c.Name)
			}
			if len(c.Env) != 1 || c.Env[0] != "LC_ALL=C" {
				t.Errorf("todo comando Unix leva LC_ALL=C: %v", c.Env)
			}
			checkBanned(t, line)
		}
		// o nft (que pede root) só é lido com administrador
		sawNft := false
		for _, c := range f.Calls() {
			sawNft = sawNft || path.Base(c.Name) == "nft"
		}
		if sawNft != admin {
			t.Errorf("admin=%v mas nft lido=%v", admin, sawNft)
		}
	}
}

func TestContratoMac(t *testing.T) {
	for _, admin := range []bool{false, true} {
		_, f := newMacFX().run(t, admin)
		checkReadOnlyCalls(t, "macos", f.Calls())
		for _, c := range f.Calls() {
			line := path.Base(c.Name) + " " + strings.Join(c.Args, " ")
			if !macAllowed[line] {
				t.Errorf("admin=%v: comando fora da lista fechada: %s", admin, line)
			}
			if !path.IsAbs(c.Name) {
				t.Errorf("caminho do executável deve ser absoluto: %s", c.Name)
			}
			checkBanned(t, line)
		}
		sawPf := false
		for _, c := range f.Calls() {
			sawPf = sawPf || path.Base(c.Name) == "pfctl"
		}
		if sawPf != admin {
			t.Errorf("admin=%v mas pfctl lido=%v", admin, sawPf)
		}
	}
}

func TestContratoWindows(t *testing.T) {
	for _, admin := range []bool{false, true} {
		_, f := newWinFX().run(t, admin)
		checkReadOnlyCalls(t, "windows", f.Calls())
		ps := 0
		for _, c := range f.Calls() {
			switch c.Name {
			case winNetsh:
				if strings.Join(c.Args, " ") != "wlan show interfaces" {
					t.Errorf("netsh fora da lista fechada: %v", c.Args)
				}
			case `C:\Windows\System32\WindowsPowerShell\v1.0\powershell.exe`:
				ps++
				script := decodePS(t, c.Args)
				for i := 0; i < len(script); i++ {
					if script[i] >= 0x80 {
						t.Fatalf("o script deve ser ASCII (byte %d): %q", i, script)
					}
				}
				for _, m := range cmdletRe.FindAllString(script, -1) {
					if !psAllowed[m] {
						t.Errorf("cmdlet fora da lista: %s\n%s", m, script)
					}
				}
				// toda consulta Get-* roda com -ErrorAction Stop (dentro do try/catch que dá o status por subconsulta)
				probe := "Get-Command Get-DnsClientDohServerAddress -ErrorAction SilentlyContinue"
				body := strings.ReplaceAll(script, probe, "")
				if g, e := strings.Count(body, "Get-"), strings.Count(body, "-ErrorAction Stop"); g != e {
					t.Errorf("%d consultas Get-* mas %d com -ErrorAction Stop:\n%s", g, e, script)
				}
				for _, b := range psBanned {
					if strings.Contains(script, b) {
						t.Errorf("termo proibido %q no script:\n%s", b, script)
					}
				}
			default:
				t.Errorf("executável fora da lista: %s", c.Name)
			}
		}
		if ps != 6 {
			t.Errorf("esperava 6 scripts, vieram %d", ps)
		}
	}
}

type recRunner struct{ calls []exec.Cmd }

func (r *recRunner) Run(_ context.Context, c exec.Cmd) (exec.Result, error) {
	r.calls = append(r.calls, c)
	return exec.Result{}, nil
}
func (r *recRunner) LookPath(string) (string, error) { return "", os.ErrNotExist }

// Deps.run normaliza TODO comando para leitura sem Elevate, e o ReadOnlyRunner recusa o contrário.
func TestRunNormalizaParaLeituraSemElevate(t *testing.T) {
	rr := &recRunner{}
	d := Deps{Runner: rr}
	if _, err := d.run(context.Background(), exec.Cmd{Name: "/bin/x", Elevate: true}); err != nil {
		t.Fatal(err)
	}
	if len(rr.calls) != 1 || rr.calls[0].Elevate || !rr.calls[0].ReadOnly {
		t.Errorf("run deve entregar ReadOnly e sem Elevate: %+v", rr.calls)
	}
	// o guarda do exec recusa o que não é leitura
	if _, err := exec.ReadOnlyRunner(rr).Run(context.Background(), exec.Cmd{Name: "/bin/x", Elevate: true, ReadOnly: true}); err == nil {
		t.Error("o ReadOnlyRunner deve recusar Elevate")
	}
}

// Nenhum arquivo do pacote fala com a internet, abre socket ou executa processo por conta própria.
func TestPacoteSemRedeNemProcessoProprio(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	banned := []string{`"net/http"`, `"net"`, `"os/exec"`, "net.Dial", "http.Get", "http.Client", "net.Listen", "syscall.Socket", `"golang.org/x/net`}
	n := 0
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		n++
		b, err := os.ReadFile(e.Name())
		if err != nil {
			t.Fatal(err)
		}
		for _, s := range banned {
			if strings.Contains(string(b), s) {
				t.Errorf("%s contém %s: a análise não acessa a internet nem executa processo fora do exec.Runner", e.Name(), s)
			}
		}
		if strings.Contains(e.Name(), "_windows") || strings.Contains(e.Name(), "_linux") || strings.Contains(e.Name(), "_darwin") {
			t.Errorf("%s: arquivo com sufixo de SO muda a compilação (use switch em tempo de execução)", e.Name())
		}
	}
	if n < 7 {
		t.Errorf("poucos arquivos lidos (%d)", n)
	}
}

func TestNomeDeInterfaceSeguro(t *testing.T) {
	for _, ok := range []string{"en0", "eth0", "wlp2s0", "enp0s3", "eth0.100", "br-lan", "veth1@if5", "Wi-Fi0"} {
		if !safeIface(ok) {
			t.Errorf("%q deveria passar", ok)
		}
	}
	for _, bad := range []string{"", "-setverbose", "--help", "-x", "a b", "a;b", "a\nb", "a/b", "../x", "..", ".hidden", "x\x1b[2J", strings.Repeat("a", 33), "a`b", "a$b"} {
		if safeIface(bad) {
			t.Errorf("%q não pode passar", bad)
		}
	}
}

// Texto malicioso do sistema (SSID, nome de processo) é preservado no dado e NEUTRALIZADO na saída pelo textsafe do chamador:
// aqui só se confere que o relatório não inventa nem corta nada; a limpeza em si está nos testes do cli e da tui.
func TestRelatorioCarregaTextoDoSistemaSemAlterar(t *testing.T) {
	r := &Report{OS: "linux", WiFi: &WiFi{SSID: "A\x1b[2JB\u202eC", Security: SecWPA2}, Listeners: []Listener{{Proto: "tcp", Addr: "0.0.0.0", Port: 23, Process: "x\x1b]0;preso\x07"}}, ListenersRead: true}
	full := Full(r, diagfmt.PT)
	if !strings.Contains(full, "A\x1b[2JB\u202eC") || !strings.Contains(full, "x\x1b]0;preso\x07") {
		t.Error("o Full não deve alterar o dado (a camada de saída é que limpa)")
	}
}

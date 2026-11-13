package recipes_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"

	toolkit "github.com/PiluTech/toolkit"
	"github.com/PiluTech/toolkit/internal/platform"
	"github.com/PiluTech/toolkit/internal/recipes"
)

// Receitas diag-rede (Linux e macOS): só leitura, sem sudo, sem rede real. Os scripts rodam em /bin/sh com um PATH
// que só tem ferramentas de texto reais (awk, sed, sort...) e FALSAS para tudo que toca a rede ou o sistema
// (ip, ping, curl, networksetup...), cada uma registrando como foi chamada.

var diagStepIDs = []string{"interfaces", "dns", "alcance", "wifi", "portas"}

func diagRecipe(t *testing.T, os platform.OS) *recipes.Recipe {
	t.Helper()
	set, err := recipes.LoadFS(toolkit.Recipes, "recipes")
	if err != nil {
		t.Fatalf("as receitas embutidas devem validar:\n%v", err)
	}
	r, ok := set.Get(os, "diag-rede")
	if !ok {
		t.Fatalf("diag-rede ausente para %v", os)
	}
	return r
}

func TestDiagRedeCarregaComoDiagnosticoSoLeitura(t *testing.T) {
	for _, o := range []platform.OS{platform.Linux, platform.Darwin} {
		r := diagRecipe(t, o)
		if r.Kind != recipes.KindDiagnostic || r.RequiresAdmin {
			t.Errorf("%v: deve ser diagnostic e sem admin: %+v", o, r)
		}
		var ids []string
		for _, s := range r.Steps {
			ids = append(ids, s.ID)
			if s.Run == nil || s.Run.Sh == "" || s.Check != nil || len(s.Install) != 0 || s.Reboot != recipes.RebootNever || s.Timeout <= 0 {
				t.Errorf("%v/%s: etapa deve ter só run sh, sem check/install/reboot e com timeout", o, s.ID)
			}
		}
		if strings.Join(ids, ",") != strings.Join(diagStepIDs, ",") {
			t.Errorf("%v: etapas %v, quer %v", o, ids, diagStepIDs)
		}
	}
}

// Nada que mude rede, DNS, firewall, rotas ou o sistema, e nada com privilégio.
var diagForbidden = []*regexp.Regexp{
	regexp.MustCompile(`\b(sudo|doas|su|pkexec)\b`),
	regexp.MustCompile(`\b(iptables|ip6tables|nft|ufw|pfctl|firewall-cmd)\b`),
	regexp.MustCompile(`\b(systemctl|service|launchctl|killall|kill|pkill|rm|mv|cp|dd|tee|chmod|chown|mkdir|touch|curl .*(-X|--data|-d |-T |-o [^/])|wget)\b`),
	regexp.MustCompile(`\bip +(-[0-9a-z]+ +)*(route|addr|address|link|neigh|rule|netns|tuntap|tunnel|maddr) +(add|del|delete|change|replace|flush|set|append|prepend)\b`),
	regexp.MustCompile(`\broute +(-n +)?(add|delete|change|flush|-n +flush)\b`),
	regexp.MustCompile(`\bifconfig +\S+ +(up|down|mtu|inet|inet6|alias|-alias|delete|ether|lladdr)\b`),
	regexp.MustCompile(`\bnetworksetup +-(set|add|remove|create|delete|rename|detect|ordernetworkservices|switchtolocation)`),
	regexp.MustCompile(`\bscutil +--set\b`),
	regexp.MustCompile(`\bresolvectl +(dns|domain|llmnr|mdns|dnssec|dnsovertls|flush-caches|reset|revert|log-level|monitor|query)\b`),
	regexp.MustCompile(`\bdscacheutil +-(flushcache|cachedump)\b`),
	regexp.MustCompile(`\bnmcli +(c|con|connection|r|radio|n|networking|g|general|d|dev|device) +(up|down|add|modify|delete|reload|connect|disconnect|reapply|on|off|hostname)\b`),
	regexp.MustCompile(`\b(ping|traceroute|nc|ncat|nmap|telnet|ssh)\b .*\$\(`), // nada de substituição em destino
	regexp.MustCompile(`>[^&>/ -][^ ]*`), // redirecionamento para arquivo (só /dev/null e >&)
}

func TestDiagRedeNaoMudaNadaNemUsaPrivilegio(t *testing.T) {
	for _, o := range []platform.OS{platform.Linux, platform.Darwin} {
		for _, s := range diagRecipe(t, o).Steps {
			// o awk usa > e >= em comparações: tira os programas awk entre aspas simples antes de procurar redirecionamento
			script := regexp.MustCompile(`(?s)awk (-[A-Za-z]+ ?"?[^ ']*"? )*'[^']*'`).ReplaceAllString(s.Run.Sh, "awk X")
			script = strings.NewReplacer("2>&1", "", ">/dev/null", "", "2>/dev/null", "", ">&2", "").Replace(script)
			for _, re := range diagForbidden {
				if m := re.FindString(script); m != "" {
					t.Errorf("%v/%s: comando proibido (%v): %q", o, s.ID, re, m)
				}
			}
		}
	}
}

func TestDiagRedeSintaxeShOk(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("sem /bin/sh no Windows")
	}
	for _, o := range []platform.OS{platform.Linux, platform.Darwin} {
		for _, s := range diagRecipe(t, o).Steps {
			cmd := exec.CommandContext(context.Background(), "/bin/sh", "-n", "-c", s.Run.Sh)
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Errorf("%v/%s: sh -n: %v\n%s", o, s.ID, err, out)
			}
		}
	}
}

// Chaves de idioma: cada etapa tem UM bloco case "$lang" com pt (*), en e es com as MESMAS variáveis m_*, e toda
// $m_x usada no script está definida nos três idiomas.
var (
	langLine = regexp.MustCompile(`(?m)^\s*(en|es|\*)\) (.*) ;;$`)
	keyDef   = regexp.MustCompile(`\b(m_[a-z0-9_]+)=`)
	keyUse   = regexp.MustCompile(`\$\{?(m_[a-z0-9_]+)`)
)

func TestDiagRedeChavesDeIdiomaIguaisNosTresIdiomas(t *testing.T) {
	for _, o := range []platform.OS{platform.Linux, platform.Darwin} {
		for _, s := range diagRecipe(t, o).Steps {
			sets := map[string][]string{}
			for _, m := range langLine.FindAllStringSubmatch(s.Run.Sh, -1) {
				var keys []string
				for _, k := range keyDef.FindAllStringSubmatch(m[2], -1) {
					keys = append(keys, k[1])
				}
				sort.Strings(keys)
				sets[m[1]] = keys
			}
			if len(sets) != 3 {
				t.Errorf("%v/%s: esperava os três idiomas (en, es, *=pt), achei %d", o, s.ID, len(sets))
				continue
			}
			if fmt.Sprint(sets["en"]) != fmt.Sprint(sets["es"]) || fmt.Sprint(sets["en"]) != fmt.Sprint(sets["*"]) {
				t.Errorf("%v/%s: chaves diferentes entre idiomas:\n en=%v\n es=%v\n pt=%v", o, s.ID, sets["en"], sets["es"], sets["*"])
			}
			defined := map[string]bool{}
			for _, k := range sets["*"] {
				defined[k] = true
			}
			for _, u := range keyUse.FindAllStringSubmatch(s.Run.Sh, -1) {
				if !defined[u[1]] {
					t.Errorf("%v/%s: $%s usada e sem tradução", o, s.ID, u[1])
				}
			}
			if !strings.Contains(s.Run.Sh, `case "$L" in en*|EN*) lang=en ;; es*|ES*) lang=es ;; *) lang=pt ;; esac`) {
				t.Errorf("%v/%s: falta a escolha de idioma padrão (TOOLKIT_LANG > LC_ALL > LC_MESSAGES > LANG)", o, s.ID)
			}
		}
	}
}

// ---- execução com ferramentas falsas ----

var realTools = []string{"awk", "sed", "sort", "uniq", "head", "tail", "grep", "tr", "cut", "cat", "env"}

type fakeEnv struct {
	dir string // PATH
	log string
}

// newToolbox cria o PATH: ferramentas de texto reais (links) + as falsas pedidas (nome -> corpo do script).
func newToolbox(t *testing.T, fakes map[string]string) fakeEnv {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("os scripts sh das receitas não rodam no Windows")
	}
	dir := t.TempDir()
	for _, n := range realTools {
		p, err := exec.LookPath(n)
		if err != nil {
			t.Skipf("ferramenta %s ausente: %v", n, err)
		}
		if err := os.Symlink(p, filepath.Join(dir, n)); err != nil {
			t.Fatal(err)
		}
	}
	log := filepath.Join(t.TempDir(), "calls.log")
	for name, body := range fakes {
		src := "#!/bin/sh\n" +
			`printf '%s\n' "` + name + ` $*" >>"$STUB_LOG"` + "\n" +
			`case ",${STUB_FAIL:-}," in *",` + name + `,"*) echo "falha simulada de ` + name + `" >&2; exit 1 ;; esac` + "\n" + body + "\n"
		if err := os.WriteFile(filepath.Join(dir, name), []byte(src), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return fakeEnv{dir: dir, log: log}
}

// runStep roda a etapa em /bin/sh com ambiente MÍNIMO (nada herdado) e devolve saída, chamadas e status.
func (f fakeEnv) runStep(t *testing.T, script string, env ...string) (out string, calls []string, code int) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "/bin/sh", "-c", script)
	cmd.Dir = t.TempDir()
	cmd.Env = append([]string{"PATH=" + f.dir, "STUB_LOG=" + f.log, "STUB_ENVLOG=" + f.log + ".env", "HOME=" + cmd.Dir}, env...)
	var b bytes.Buffer
	cmd.Stdout, cmd.Stderr = &b, &b
	err := cmd.Run()
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		code = ee.ExitCode()
	} else if err != nil {
		t.Fatalf("falha ao rodar: %v", err)
	}
	if data, err := os.ReadFile(f.log); err == nil {
		calls = strings.Split(strings.TrimSpace(string(data)), "\n")
		_ = os.Remove(f.log)
	}
	if len(calls) == 1 && calls[0] == "" {
		calls = nil
	}
	return b.String(), calls, code
}

func stepScript(t *testing.T, r *recipes.Recipe, id string) string {
	t.Helper()
	for _, s := range r.Steps {
		if s.ID == id {
			return s.Run.Sh
		}
	}
	t.Fatalf("etapa %s ausente", id)
	return ""
}

const linuxIP = `case "$*" in
"-brief address show") cat <<'E'
lo               UNKNOWN        127.0.0.1/8 ::1/128
eth0             UP             192.168.1.10/24 203.0.113.7/32 2001:db8:abcd:12::5/64 fe80::1c2:3dff:fe4e:5f60/64
E
;;
"-o link show") cat <<'E'
1: lo: <LOOPBACK,UP,LOWER_UP> mtu 65536 qdisc noqueue state UNKNOWN mode DEFAULT group default qlen 1000\    link/loopback 00:00:00:00:00:00 brd 00:00:00:00:00:00
2: eth0: <BROADCAST,MULTICAST,UP,LOWER_UP> mtu 1500 qdisc fq_codel state UP mode DEFAULT group default qlen 1000\    link/ether 52:54:00:12:34:56 brd ff:ff:ff:ff:ff:ff
E
;;
"route show default") echo "default via 192.168.1.1 dev eth0 proto dhcp src 203.0.113.7 metric 100" ;;
"-6 route show default") echo "default via fe80::1 dev eth0 proto ra metric 1024 expires 1700sec" ;;
esac`

const pingOK = `if [ -n "${STUB_PING_ERR:-}" ]; then printf 'From 203.0.113.7 icmp_seq=1 Destination Host Unreachable\n3 packets transmitted, 0 received, +3 errors, 100%% packet loss, time 2003ms\n'; exit 1; fi
printf 'PING x (x) 56(84) bytes of data.\n64 bytes from x: icmp_seq=1 ttl=57 time=10.1 ms\n\n--- x ping statistics ---\n3 packets transmitted, 3 received, 0%% packet loss, time 2003ms\nrtt min/avg/max/mdev = 10.1/11.2/12.3/0.9 ms\n'`

const curlOK = `printf 'ENV SSLKEYLOGFILE=%s\n' "${SSLKEYLOGFILE:-}" >>"$STUB_ENVLOG"
if [ -n "${STUB_CURL_ERR:-}" ]; then echo "curl: (7) Failed to connect to 203.0.113.7 port 443" >&2; exit 7; fi
printf '200 0.011 0.045 0.098'`

func linuxFakes() map[string]string {
	return map[string]string{
		"ip":         linuxIP,
		"resolvectl": `printf 'Global\n         Protocols: -LLMNR\nLink 2 (eth0)\nCurrent DNS Server: 192.168.1.1\n       DNS Servers: 192.168.1.1 1.1.1.1\n'`,
		"getent":     `printf '93.184.216.34   STREAM example.com\n93.184.216.34   DGRAM\n2606:2800:21f:cb07::1 STREAM example.com\n'`,
		"ping":       pingOK,
		"curl":       curlOK,
		"ss":         `printf 'State  Recv-Q Send-Q Local Address:Port Peer Address:Port Process\nLISTEN 0 128 0.0.0.0:22 0.0.0.0:*\nLISTEN 0 4096 127.0.0.1:631 0.0.0.0:*\nLISTEN 0 511 [::]:80 [::]:*\nLISTEN 0 100 [::1]:25 [::]:*\n'`,
		"nmcli": `case "$*" in
"-t -f DEVICE,TYPE,STATE,CONNECTION device status") printf 'wlan0:wifi:connected:MinhaRedeSecreta\neth0:ethernet:connected:Wired connection 1\n' ;;
"-t -f IN-USE,SIGNAL,CHAN,RATE dev wifi list --rescan no") printf '*:72:6:270 Mbit/s\n :40:11:130 Mbit/s\n' ;;
esac`,
	}
}

const macIfconfig = `cat <<'E'
lo0: flags=8049<UP,LOOPBACK,RUNNING,MULTICAST> mtu 16384
	inet 127.0.0.1 netmask 0xff000000
	inet6 ::1 prefixlen 128
	status: active
en0: flags=8863<UP,BROADCAST,SMART,RUNNING,SIMPLEX,MULTICAST> mtu 1500
	ether aa:bb:cc:dd:ee:ff
	inet6 fe80::1c2:3dff:fe4e:5f60%en0 prefixlen 64 secured scopeid 0xb
	inet 192.168.1.20 netmask 0xffffff00 broadcast 192.168.1.255
	inet 203.0.113.9 netmask 0xffffff00 broadcast 203.0.113.255
	inet6 2001:db8:abcd:12::9 prefixlen 64 autoconf secured
	nd6 options=201<PERFORMNUD,DAD>
	media: autoselect
	status: active
awdl0: flags=8943<UP,BROADCAST,RUNNING,PROMISC,SIMPLEX,MULTICAST> mtu 1484
	ether 11:22:33:44:55:66
	inet6 fe80::9c5a:1ff:fe00:1%awdl0 prefixlen 64 scopeid 0xf
	status: inactive
gif0: flags=8010<POINTOPOINT,MULTICAST> mtu 1280
E`

func macFakes() map[string]string {
	return map[string]string{
		"ifconfig": macIfconfig,
		"route": `case "$*" in
"-n get default") printf '   route to: default\ndestination: default\n       mask: default\n    gateway: 192.168.1.1\n  interface: en0\n      flags: <UP,GATEWAY,DONE,STATIC,PRCLONING,GLOBAL>\n' ;;
*) echo "route: writing to routing socket: not in table" >&2; exit 1 ;;
esac`,
		"scutil":      `printf 'DNS configuration\n\nresolver #1\n  nameserver[0] : 192.168.1.1\n  nameserver[1] : 1.1.1.1\n  if_index : 11 (en0)\n\nDNS configuration (for scoped queries)\n\nresolver #1\n  nameserver[0] : 10.9.9.9\n'`,
		"dscacheutil": `printf 'name: example.com\nipv6_address: 2606:2800:21f:cb07::1\n\nname: example.com\nip_address: 93.184.216.34\n'`,
		"ping":        pingOK,
		"curl":        curlOK,
		"networksetup": `case "$*" in
"-listallhardwareports") printf 'Hardware Port: Ethernet\nDevice: en5\nEthernet Address: aa:aa:aa:aa:aa:aa\n\nHardware Port: Wi-Fi\nDevice: en0\nEthernet Address: aa:bb:cc:dd:ee:ff\n' ;;
"-getairportpower en0") echo "Wi-Fi Power (en0): On" ;;
"-getairportnetwork en0") echo "Current Wi-Fi Network: MinhaRedeSecreta" ;;
esac`,
		"netstat": `printf 'Active Internet connections (including servers)\nProto Recv-Q Send-Q  Local Address          Foreign Address        (state)\ntcp4  0 0  192.168.1.20.52000  17.57.145.1.5223  ESTABLISHED\ntcp4  0 0  127.0.0.1.631  *.*  LISTEN\ntcp6  0 0  ::1.631  *.*  LISTEN\ntcp4  0 0  *.5000  *.*  LISTEN\ntcp46 0 0  *.7000  *.*  LISTEN\n'`,
	}
}

type osCase struct {
	os      platform.OS
	fakes   func() map[string]string
	allowed []*regexp.Regexp
}

func re(ss ...string) []*regexp.Regexp {
	var out []*regexp.Regexp
	for _, s := range ss {
		out = append(out, regexp.MustCompile("^"+s+"$"))
	}
	return out
}

const curlArgs = `curl -q -sS -L --proto =https --proto-redir =https --max-redirs 3 --connect-timeout 5 --max-time 10 -o /dev/null -w %\{http_code\} %\{time_connect\} %\{time_appconnect\} %\{time_total\} https://[A-Za-z0-9._~:/-]+`

var diagCases = []osCase{
	{platform.Linux, linuxFakes, re(
		`ip (-brief address show|-o link show|route show default|-6 route show default)`,
		`resolvectl status`, `getent ahosts [A-Za-z0-9._-]+`, `(host|nslookup) [A-Za-z0-9._-]+`,
		`ping -c 3 -W 2 [A-Za-z0-9._:-]+`, curlArgs, `ss -ltn`,
		`nmcli -t -f DEVICE,TYPE,STATE,CONNECTION device status`,
		`nmcli -t -f IN-USE,SIGNAL,CHAN,RATE dev wifi list --rescan no`)},
	{platform.Darwin, macFakes, re(
		`ifconfig -a`, `route -n get (-inet6 )?default`, `scutil --dns`, `dscacheutil -q host -a name [A-Za-z0-9._-]+`,
		`ping -c 3 -t 6 [A-Za-z0-9._:-]+`, curlArgs,
		`networksetup (-listallhardwareports|-getairportpower en0|-getairportnetwork en0)`,
		`netstat -an -p tcp`)},
}

// runAll roda as cinco etapas e junta saída e chamadas (verifica status 0 em todas).
func runAll(t *testing.T, c osCase, f fakeEnv, env ...string) (map[string]string, []string) {
	t.Helper()
	r := diagRecipe(t, c.os)
	outs := map[string]string{}
	var calls []string
	for _, id := range diagStepIDs {
		out, cl, code := f.runStep(t, stepScript(t, r, id), env...)
		if code != 0 {
			t.Errorf("%v/%s: status %d (diagnóstico nunca falha por achado):\n%s", c.os, id, code, out)
		}
		for _, b := range []byte(out) {
			if b < 0x20 && b != '\n' && b != '\t' {
				t.Errorf("%v/%s: caractere de controle %#x na saída", c.os, id, b)
				break
			}
		}
		outs[id] = out
		calls = append(calls, cl...)
	}
	return outs, calls
}

func TestDiagRedeSaidaMascaradaEResumida(t *testing.T) {
	for _, c := range diagCases {
		f := newToolbox(t, c.fakes())
		outs, calls := runAll(t, c, f)
		all := strings.Join([]string{outs["interfaces"], outs["dns"], outs["alcance"], outs["wifi"], outs["portas"]}, "\n")
		// dado sensível nunca aparece: MAC, SSID, IPv4 e IPv6 públicos do usuário
		for _, secret := range []string{"52:54:00:12:34:56", "aa:bb:cc:dd:ee:ff", "11:22:33:44:55:66", "MinhaRedeSecreta", "203.0.113.", "2001:db8", "5223", "52000"} {
			if strings.Contains(all, secret) {
				t.Errorf("%v: dado sensível %q na saída:\n%s", c.os, secret, all)
			}
		}
		// o que é privado e útil continua
		for _, want := range []string{"192.168.1.1", "203.x.x.x", "2001:x:x", "fe80::", "1500", "ok"} {
			if !strings.Contains(all, want) {
				t.Errorf("%v: faltou %q na saída:\n%s", c.os, want, all)
			}
		}
		if !strings.Contains(outs["wifi"], "[nome oculto]") || !strings.Contains(outs["wifi"], "Adaptador") {
			t.Errorf("%v wifi: %q", c.os, outs["wifi"])
		}
		if !strings.Contains(outs["alcance"], "HTTPS https://example.com/: ok (HTTP 200; tcp 0.011 s; tls 0.045 s; total 0.098 s)") {
			t.Errorf("%v alcance: %q", c.os, outs["alcance"])
		}
		if !strings.Contains(outs["alcance"], "3 packets transmitted, 3 received") {
			t.Errorf("%v: resumo do ping ausente: %q", c.os, outs["alcance"])
		}
		for _, want := range []string{"631 só esta máquina", "toda a rede"} {
			if !strings.Contains(outs["portas"], want) {
				t.Errorf("%v portas: faltou %q: %q", c.os, want, outs["portas"])
			}
		}
		if strings.Contains(outs["dns"], "10.9.9.9") {
			t.Errorf("%v dns: resolvedor de consultas com escopo não deveria entrar: %q", c.os, outs["dns"])
		}
		if len(calls) == 0 {
			t.Errorf("%v: nenhuma ferramenta foi chamada", c.os)
		}
	}
}

func TestDiagRedeSoChamaComandosDeLeituraDaLista(t *testing.T) {
	for _, c := range diagCases {
		f := newToolbox(t, c.fakes())
		_, calls := runAll(t, c, f)
		for _, call := range calls {
			ok := false
			for _, a := range c.allowed {
				if a.MatchString(call) {
					ok = true
					break
				}
			}
			if !ok {
				t.Errorf("%v: chamada fora da lista de leitura: %q", c.os, call)
			}
		}
	}
}

func TestDiagRedeIdiomaPelaVariavelDeAmbiente(t *testing.T) {
	routes := map[string]string{"pt": "Rota padrão", "en": "Default route", "es": "Ruta predeterminada"}
	envs := []struct {
		lang string
		env  []string
	}{
		{"pt", nil},
		{"pt", []string{"LC_ALL=C"}},
		{"pt", []string{"LANG=pt_BR.UTF-8"}},
		{"en", []string{"TOOLKIT_LANG=en"}},
		{"en", []string{"LANG=en_US.UTF-8"}},
		{"es", []string{"LANG=es_ES.UTF-8"}},
		{"es", []string{"LC_ALL=es_MX.UTF-8", "LANG=en_US.UTF-8"}}, // LC_ALL vence o LANG
		{"en", []string{"TOOLKIT_LANG=en", "LC_ALL=es_ES.UTF-8"}},  // TOOLKIT_LANG vence tudo
		{"pt", []string{"TOOLKIT_LANG=fr", "LANG=es_ES.UTF-8"}},    // idioma desconhecido: pt-BR (não cai no LANG)
	}
	for _, c := range diagCases {
		f := newToolbox(t, c.fakes())
		r := diagRecipe(t, c.os)
		for _, e := range envs {
			out, _, code := f.runStep(t, stepScript(t, r, "interfaces"), e.env...)
			if code != 0 || !strings.Contains(out, routes[e.lang]) {
				t.Errorf("%v %v: quer %q (%s):\n%s", c.os, e.env, routes[e.lang], e.lang, out)
			}
			for other, w := range routes {
				if other != e.lang && strings.Contains(out, w) {
					t.Errorf("%v %v: misturou idiomas (%q em %s)", c.os, e.env, w, e.lang)
				}
			}
		}
	}
}

func TestDiagRedeAlvosExternosConfiguraveis(t *testing.T) {
	for _, c := range diagCases {
		f := newToolbox(t, c.fakes())
		r := diagRecipe(t, c.os)
		_, calls, _ := f.runStep(t, stepScript(t, r, "alcance"), "TOOLKIT_DIAG_HOST=192.0.2.7", "TOOLKIT_DIAG_URL=https://example.org/status")
		joined := strings.Join(calls, "\n")
		if !strings.Contains(joined, "192.0.2.7") || !strings.Contains(joined, "https://example.org/status") || strings.Contains(joined, "1.1.1.1") || strings.Contains(joined, "https://example.com/") {
			t.Errorf("%v: alvos configurados não foram usados:\n%s", c.os, joined)
		}
		_, calls, _ = f.runStep(t, stepScript(t, r, "dns"), "TOOLKIT_DIAG_NAME=example.net")
		if !strings.Contains(strings.Join(calls, "\n"), "example.net") || strings.Contains(strings.Join(calls, "\n"), "example.com") {
			t.Errorf("%v: nome DNS configurado não foi usado: %v", c.os, calls)
		}
	}
}

func TestDiagRedeRecusaAlvoInvalidoSemChamarNada(t *testing.T) {
	cases := []struct{ step, env string }{
		{"alcance", "TOOLKIT_DIAG_HOST=-c1"},
		{"alcance", "TOOLKIT_DIAG_HOST=a b"},
		{"alcance", "TOOLKIT_DIAG_HOST=$(id)"},
		{"alcance", "TOOLKIT_DIAG_HOST=x;id"},
		{"alcance", "TOOLKIT_DIAG_URL=http://example.com/"},
		{"alcance", "TOOLKIT_DIAG_URL=https://example.com/;id"},
		{"alcance", "TOOLKIT_DIAG_URL=https://exa mple.com/"},
		{"alcance", "TOOLKIT_DIAG_URL=https://x/$(id)"},
		{"alcance", "TOOLKIT_DIAG_URL=https://"},
		{"alcance", "TOOLKIT_DIAG_URL=file:///etc/passwd"},
		{"dns", "TOOLKIT_DIAG_NAME=-x"},
		{"dns", "TOOLKIT_DIAG_NAME=a;b"},
		{"dns", "TOOLKIT_DIAG_NAME=a b"},
		{"dns", "TOOLKIT_DIAG_NAME=$(id)"},
	}
	for _, c := range diagCases {
		f := newToolbox(t, c.fakes())
		r := diagRecipe(t, c.os)
		for _, k := range cases {
			out, calls, code := f.runStep(t, stepScript(t, r, k.step), k.env)
			if code != 0 || !strings.Contains(out, "inválid") {
				t.Errorf("%v %s: %s deveria ser recusado com mensagem (status %d):\n%s", c.os, k.step, k.env, code, out)
			}
			for _, call := range calls {
				if strings.HasPrefix(call, "ping") || strings.HasPrefix(call, "curl") || strings.HasPrefix(call, "getent") || strings.HasPrefix(call, "dscacheutil") {
					t.Errorf("%v %s: %s não deveria chamar %q", c.os, k.step, k.env, call)
				}
			}
		}
	}
}

func TestDiagRedeFalhaDeRedeEhAchadoNaoErro(t *testing.T) {
	for _, c := range diagCases {
		f := newToolbox(t, c.fakes())
		outs, _ := runAll(t, c, f, "STUB_FAIL=ping,curl,getent,dscacheutil,resolvectl,route,ip,ifconfig")
		if !strings.Contains(outs["alcance"], "falhou") {
			t.Errorf("%v: alcance deveria dizer falhou:\n%s", c.os, outs["alcance"])
		}
	}
}

func TestDiagRedeSemFerramentasOpcionaisAvisaEContinua(t *testing.T) {
	for _, c := range diagCases {
		f := newToolbox(t, map[string]string{}) // PATH só com ferramentas de texto
		outs, calls := runAll(t, c, f)
		if len(calls) != 0 {
			t.Errorf("%v: sem ferramentas não há chamadas: %v", c.os, calls)
		}
		for id, out := range outs {
			if strings.TrimSpace(out) == "" {
				t.Errorf("%v/%s: sem ferramentas a saída não pode ser vazia", c.os, id)
			}
		}
		if !strings.Contains(outs["alcance"], "o comando 'ping' não existe") || !strings.Contains(outs["alcance"], "o comando 'curl' não existe") {
			t.Errorf("%v: alcance sem ping/curl: %q", c.os, outs["alcance"])
		}
	}
}

// Lupa #55 (1): o curl NUNCA lê a .curlrc do usuário (data-binary, Authorization, request=POST, cookie-jar, trace) nem
// grava chave TLS (SSLKEYLOGFILE). Prova com o curl REAL contra uma porta recusada (sem rede) e uma .curlrc fictícia
// em HOME e em CURL_HOME: nada de trace, cookie-jar nem corpo gravado; sem -q o curl criaria esses arquivos.
func TestDiagRedeCurlIgnoraConfiguracaoDoUsuario(t *testing.T) {
	real, err := exec.LookPath("curl")
	if err != nil {
		t.Skip("curl real ausente")
	}
	for _, c := range diagCases {
		r := diagRecipe(t, c.os)
		script := stepScript(t, r, "alcance")
		for _, withQ := range []bool{true, false} {
			f := newToolbox(t, map[string]string{"ping": pingOK}) // sem curl falso
			if err := os.Symlink(real, filepath.Join(f.dir, "curl")); err != nil {
				t.Fatal(err)
			}
			home := t.TempDir()
			trace, jar, body, keylog := filepath.Join(home, "trace.log"), filepath.Join(home, "cookies.txt"), filepath.Join(home, "saida.bin"), filepath.Join(home, "keys.log")
			rc := "trace = \"" + trace + "\"\ncookie-jar = \"" + jar + "\"\nrequest = \"POST\"\ndata-binary = \"segredo=1\"\nheader = \"Authorization: Bearer SEGREDO\"\noutput = \"" + body + "\"\n"
			for _, name := range []string{".curlrc", "_curlrc"} {
				if err := os.WriteFile(filepath.Join(home, name), []byte(rc), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			sc := script
			if !withQ { // controle: sem o -q a mesma .curlrc É lida (o teste tem de pegar a regressão)
				sc = strings.Replace(sc, "curl -q ", "curl ", 1)
			}
			out, _, code := f.runStep(t, sc, "HOME="+home, "CURL_HOME="+home, "SSLKEYLOGFILE="+keylog, "TOOLKIT_DIAG_URL=https://127.0.0.1:1/")
			if code != 0 {
				t.Fatalf("%v: status %d:\n%s", c.os, code, out)
			}
			var leaked []string
			for _, p := range []string{trace, jar, body, keylog} {
				if _, err := os.Stat(p); err == nil {
					leaked = append(leaked, filepath.Base(p))
				}
			}
			if withQ && len(leaked) != 0 {
				t.Errorf("%v: com -q o curl não pode ler a .curlrc nem gravar nada, mas criou %v", c.os, leaked)
			}
			if !withQ && len(leaked) == 0 {
				t.Errorf("%v: o controle sem -q deveria ler a .curlrc fictícia (senão o teste não prova nada)", c.os)
			}
		}
	}
}

// -q é o PRIMEIRO argumento do curl, e a chave TLS (SSLKEYLOGFILE) não chega ao curl.
func TestDiagRedeCurlComQPrimeiroESemSSLKEYLOGFILE(t *testing.T) {
	for _, c := range diagCases {
		f := newToolbox(t, c.fakes())
		_, calls, _ := f.runStep(t, stepScript(t, diagRecipe(t, c.os), "alcance"), "SSLKEYLOGFILE=/tmp/nao-pode.log")
		found := false
		for _, call := range calls {
			if strings.HasPrefix(call, "curl ") {
				found = true
				if !strings.HasPrefix(call, "curl -q ") {
					t.Errorf("%v: -q deve ser o primeiro argumento do curl: %q", c.os, call)
				}
			}
		}
		if !found {
			t.Errorf("%v: curl não foi chamado", c.os)
		}
		data, _ := os.ReadFile(f.log + ".env")
		if strings.Contains(string(data), "/tmp/nao-pode.log") || !strings.Contains(string(data), "SSLKEYLOGFILE=\n") {
			t.Errorf("%v: SSLKEYLOGFILE chegou ao curl: %q", c.os, data)
		}
	}
}

// Lupa #55 (2): se o DNS devolve o IP público da própria máquina (o mesmo que a etapa interfaces mascarou), ele não
// reaparece; o mesmo vale para as caudas de erro do ping e do curl. Alvo, servidor DNS e gateway continuam como são.
func TestDiagRedeMascaraRespostasDeDNSEErrosDeRede(t *testing.T) {
	for _, c := range diagCases {
		fakes := c.fakes()
		own4, own6 := "203.0.113.7", "2001:db8:abcd:12::5"
		if c.os == platform.Linux {
			fakes["getent"] = "printf '" + own4 + "   STREAM example.com\n" + own6 + " STREAM example.com\n192.168.1.50 STREAM example.com\n'"
		} else {
			fakes["dscacheutil"] = "printf 'name: example.com\nipv6_address: " + own6 + "\n\nname: example.com\nip_address: " + own4 + "\n\nname: example.com\nip_address: 192.168.1.50\n'"
		}
		f := newToolbox(t, fakes)
		r := diagRecipe(t, c.os)
		dns, _, _ := f.runStep(t, stepScript(t, r, "dns"))
		if strings.Contains(dns, own4) || strings.Contains(dns, own6) || strings.Contains(dns, "2001:db8") {
			t.Errorf("%v: o IP público da própria máquina reapareceu no DNS:\n%s", c.os, dns)
		}
		if !strings.Contains(dns, "203.x.x.x") || !strings.Contains(dns, "2001:x:x") || !strings.Contains(dns, "192.168.1.50") || !strings.Contains(dns, "192.168.1.1") {
			t.Errorf("%v: máscara/preservação do DNS erradas:\n%s", c.os, dns)
		}
		alc, _, code := f.runStep(t, stepScript(t, r, "alcance"), "STUB_PING_ERR=1", "STUB_CURL_ERR=1")
		if code != 0 || strings.Contains(alc, "203.0.113.7") {
			t.Errorf("%v: cauda de erro do ping/curl com IP público (status %d):\n%s", c.os, code, alc)
		}
		if !strings.Contains(alc, "203.x.x.x") || !strings.Contains(alc, "falhou") {
			t.Errorf("%v: erro mascarado ausente:\n%s", c.os, alc)
		}
		// o alvo configurado continua visível (exceção documentada)
		if !strings.Contains(alc, "https://example.com/") || !strings.Contains(alc, "1.1.1.1") {
			t.Errorf("%v: alvo deveria aparecer como é:\n%s", c.os, alc)
		}
	}
}

// nslookup (terceira opção no Linux) não lê o ~/.nslookuprc do usuário: roda com HOME=/.
func TestDiagRedeLinuxNslookupNaoLeConfigDoUsuario(t *testing.T) {
	fakes := map[string]string{"nslookup": `printf 'HOME=%s\n' "$HOME" >>"$STUB_ENVLOG"; printf 'Server: 192.168.1.1\nAddress: 192.168.1.1#53\n\nName: example.com\nAddress: 93.184.216.34\n'`}
	f := newToolbox(t, fakes)
	out, _, code := f.runStep(t, stepScript(t, diagRecipe(t, platform.Linux), "dns"))
	data, _ := os.ReadFile(f.log + ".env")
	if code != 0 || !strings.Contains(string(data), "HOME=/\n") || !strings.Contains(out, "93.x.x.x") {
		t.Errorf("nslookup deve rodar com HOME=/ e a resposta sair mascarada (status %d):\n%s\n%s", code, data, out)
	}
}

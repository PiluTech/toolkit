package otimizacao

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/PiluTech/toolkit/internal/diagfmt"
	"github.com/PiluTech/toolkit/internal/exec"
	"github.com/PiluTech/toolkit/internal/exec/exectest"
)

var testNow = time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)

// Fixtures SINTÉTICAS: nada aqui veio de uma máquina real (nomes e números inventados).
const (
	fxPerf     = `{"cpu":[10,92,88],"lat":[{"n":"1000000000000","d":"1000","f":"10000000","q":"0","t":"1000000000"},{"n":"1000020000000","d":"1100","f":"10000000","q":"30000000","t":"1010000000"},{"n":"1000036000000","d":"1200","f":"10000000","q":"50000000","t":"1020000000"}],"lp":4,"top":[{"Name":"chrome#2","Pid":4242,"Cpu":240},{"Name":"editor","Pid":77,"Cpu":"40"},{"Name":null,"Pid":null,"Cpu":null}]}`
	fxMem      = `{"total":17179869184,"avail":1073741824,"committed":15032385536,"limit":17179869184,"swap":[{"Alloc":2048,"Used":1900}],"top":[{"Name":"chrome","Pid":4242,"Mem":2147483648},{"Name":"editor","Pid":77,"Mem":"1073741824"}]}`
	fxDisk     = `{"drive":"C","size":512110190592,"free":30000000000,"name":"Disco Fake","media":"HDD","bus":"SATA"}`
	fxStartup  = `{"wmi":[{"Name":"AppUm","Cmd":"\"C:\\Programas\\AppUm\\um.exe\" --minimizado","Loc":"HKU\\x\\Run"},{"Name":"AppDois","Cmd":"C:\\Programas\\dois.exe","Loc":"Startup"}],"run":[{"Name":"appum","Cmd":"C:\\Programas\\AppUm\\um.exe","Loc":"HKCU:\\Run"},{"Name":"AppTres","Cmd":"tres.exe /x","Loc":"HKLM:\\Run"}]}`
	fxServices = `[{"N":"Svc1","D":"Servico Um","S":"Running","E":0,"P":"\"C:\\Programas\\Um\\um.exe\" -s"},{"N":"Svc2","D":"Servico Dois","S":"Stopped","E":1,"P":"C:\\Programas\\Dois\\dois.exe"},{"N":"Win1","D":"Do Windows","S":"Stopped","E":0,"P":"C:\\Windows\\system32\\svchost.exe -k x"},{"N":"Win2","D":"Do Windows 2","S":"Stopped","E":1077,"P":"C:\\Programas\\Tres\\tres.exe"},{"N":"Win3","D":"Do Windows 3","S":"Stopped","E":1,"P":"%SystemRoot%\\system32\\x.exe"}]`
	fxBootOK   = `{"status":"ok","t":"2026-11-14T08:00:00Z","boot":"95000","degr":"true"}`
)

func fxAll() map[string]string {
	return map[string]string{
		scriptPerf: fxPerf, scriptMem: fxMem, scriptDisk: fxDisk, scriptStartup: fxStartup, scriptServices: fxServices, scriptBoot: fxBootOK,
	}
}

func timeoutOf(script string) time.Duration {
	if script == scriptPerf {
		return perfTimeout
	}
	return readTimeout
}

func runWin(t *testing.T, s map[string]string, d Deps) (*Report, *exectest.FakeRunner) {
	t.Helper()
	f := exectest.New(t)
	for sc, out := range s {
		f.On(psCmd(sc, timeoutOf(sc)).String(), exectest.Exit(0, out))
	}
	d.Runner = f
	d.Now = func() time.Time { return testNow }
	return Collect(context.Background(), "windows", d), f
}

func TestColetaWindowsCompleta(t *testing.T) {
	r, _ := runWin(t, fxAll(), Deps{Lang: diagfmt.PT})
	if !r.Supported || r.OS != "windows" || !r.Generated.Equal(testNow) {
		t.Fatalf("cabeçalho: %+v", r)
	}
	if len(r.Skipped) != 0 {
		t.Fatalf("nada devia ficar sem leitura: %+v", r.Skipped)
	}
	c := r.CPU
	if c == nil || c.Samples != 3 || c.Logical != 4 || c.PeakPct != 92 || int(c.AvgPct*100) != 6333 {
		t.Fatalf("cpu: %+v", c)
	}
	// o contador por processo soma os núcleos: 240 em 4 processadores = 60% da máquina; "chrome#2" perde o "#2"
	if len(c.Top) != 2 || c.Top[0].Name != "chrome" || c.Top[0].CPUPct != 60 || c.Top[1].Name != "editor" || c.Top[1].CPUPct != 10 {
		t.Fatalf("top cpu (o nulo é descartado): %+v", c.Top)
	}
	m := r.Memory
	if m == nil || m.TotalBytes != 17179869184 || m.AvailableBytes == nil || *m.AvailableBytes != 1073741824 || m.CommitBytes != 15032385536 || m.PageAllocBytes != 2048<<20 || m.PageUsedBytes != 1900<<20 {
		t.Fatalf("memória: %+v", m)
	}
	if len(m.Top) != 2 || m.Top[0].Name != "chrome" || m.Top[1].MemBytes != 1073741824 {
		t.Fatalf("top memória (número como texto): %+v", m.Top)
	}
	d := r.Disk
	if d == nil || d.Drive != "C" || d.Medium != "HDD" || d.FreeBytes == nil || *d.FreeBytes != 30000000000 {
		t.Fatalf("disco: %+v", d)
	}
	if d.QueueAvg == nil || d.QueuePeak == nil || *d.QueuePeak != 3 || d.LatencyMs == nil || int(math.Round(*d.LatencyMs)) != 18 {
		t.Fatalf("fila e latência vêm da amostra de desempenho: %+v", d)
	}
	s := r.Startup
	if s == nil || s.Total != 3 || len(s.Items) != 3 || s.Items[0].Name != "AppDois" || s.Items[1].Command != "tres.exe" || s.Items[2].Command != "um.exe" {
		t.Fatalf("inicialização (a Run key repetida não conta duas vezes; só o nome do programa): %+v", s)
	}
	sv := r.Services
	if sv == nil || sv.AutoCount != 5 || sv.OutsideCount != 3 {
		t.Fatalf("serviços: %+v", sv)
	}
	if len(sv.Failed) != 2 || sv.Failed[0].Name != "Svc2" || sv.Failed[1].Name != "Win3" {
		t.Fatalf("só parado com código de erro (0 e 1077 não contam), em qualquer pasta, conta como falha: %+v", sv.Failed)
	}
	b := r.Boot
	if b == nil || !b.Found || b.DurationMs != 95000 || b.Degraded == nil || !*b.Degraded || !b.When.Equal(time.Date(2026, 11, 14, 8, 0, 0, 0, time.UTC)) {
		t.Fatalf("boot: %+v", b)
	}
}

// Todo comando que o coletor emite é de leitura, sem Elevate, pelo caminho absoluto do PowerShell.
func TestColetaTodoComandoEhLeituraSemElevate(t *testing.T) {
	_, f := runWin(t, fxAll(), Deps{Lang: diagfmt.PT, Admin: true})
	calls := f.Calls()
	if len(calls) != 6 {
		t.Fatalf("esperava 6 leituras, vieram %d", len(calls))
	}
	for _, c := range calls {
		if !c.ReadOnly || c.Elevate {
			t.Errorf("comando fora do contrato (ReadOnly=%v Elevate=%v): %s", c.ReadOnly, c.Elevate, c.Name)
		}
		if c.Name != `C:\Windows\System32\WindowsPowerShell\v1.0\powershell.exe` {
			t.Errorf("o PowerShell vai por caminho absoluto: %q", c.Name)
		}
	}
}

// O ReadOnlyRunner recusa mutação: um Runner que registra tudo prova que nada escapou dele.
func TestColetaUsaOReadOnlyRunner(t *testing.T) {
	rr := &recordRunner{}
	Collect(context.Background(), "windows", Deps{Runner: rr, Lang: diagfmt.PT})
	if len(rr.cmds) == 0 {
		t.Fatal("o coletor devia ter chamado o Runner")
	}
	for _, c := range rr.cmds {
		if !c.ReadOnly || c.Elevate {
			t.Errorf("chegou ao Runner um comando que não é de leitura: %+v", c)
		}
	}
}

type recordRunner struct{ cmds []exec.Cmd }

func (r *recordRunner) Run(_ context.Context, c exec.Cmd) (exec.Result, error) {
	r.cmds = append(r.cmds, c)
	return exec.Result{ExitCode: 1}, nil
}
func (r *recordRunner) LookPath(string) (string, error) { return "", errors.New("nf") }

// Os scripts são texto fixo, ASCII puro e só de leitura (nenhum verbo que escreve, remove, para ou cria).
func TestScriptsSaoASCIIeSoLeitura(t *testing.T) {
	forbidden := []string{"Set-", "Remove-", "Stop-", "New-", "Disable-", "Enable-", "Start-Service", "Start-Process", "Invoke-", "Out-File", "Add-", "Clear-", "Restart-", "reg ", "sc ", "sc.exe", "net ", "taskkill", "dism", ">", "|iex", "Install-"}
	for _, sc := range allScripts {
		for i := 0; i < len(sc); i++ {
			if sc[i] > 0x7e || (sc[i] < 0x20 && sc[i] != '\n') {
				t.Fatalf("byte fora do ASCII em %.40q: %q", sc, sc[i])
			}
		}
		low := strings.ToLower(sc)
		for _, w := range forbidden {
			if strings.Contains(low, strings.ToLower(w)) {
				t.Errorf("script com %q (não é leitura): %.80s", w, sc)
			}
		}
		if !strings.Contains(sc, "SilentlyContinue") {
			t.Errorf("o script precisa de SilentlyContinue: %.60s", sc)
		}
	}
	if len(allScripts) != 6 {
		t.Fatalf("scripts: %d", len(allScripts))
	}
}

// Fora do Windows o coletor NÃO chama o Runner (um fake sem roteiro falharia o teste) e devolve "só existe no Windows".
func TestColetaForaDoWindowsNaoExecutaNada(t *testing.T) {
	for _, goos := range []string{"darwin", "linux", "freebsd"} {
		f := exectest.New(t)
		r := Collect(context.Background(), goos, Deps{Runner: f, Now: func() time.Time { return testNow }})
		if r.Supported || r.OS != goos || r.CPU != nil || r.Memory != nil || r.Disk != nil || r.Startup != nil || r.Services != nil || r.Boot != nil {
			t.Errorf("%s: %+v", goos, r)
		}
		if len(f.Calls()) != 0 {
			t.Errorf("%s: chamou o Runner", goos)
		}
		if got := Analyze(r, diagfmt.PT); len(got) != 0 {
			t.Errorf("%s: sem achados fora do Windows: %+v", goos, got)
		}
	}
}

// Campos nulos, objeto único no lugar de lista, números como texto e lixo: nada derruba a leitura.
func TestParseTolerante(t *testing.T) {
	s := fxAll()
	s[scriptPerf] = `{"cpu":15,"q":null,"lat":"x","lp":null,"top":{"Name":"solo","Pid":"9","Cpu":"8"}}`
	s[scriptMem] = `{"total":"1048576","avail":null,"committed":null,"limit":null,"swap":null,"top":null}`
	s[scriptDisk] = `{"drive":"C","size":1000,"free":500,"name":null,"media":"Unspecified","bus":null}`
	s[scriptStartup] = `{"wmi":null,"run":{"Name":"UmSo","Cmd":null,"Loc":null}}`
	s[scriptServices] = `{"N":"Solo","D":null,"S":null,"E":null,"P":null}`
	s[scriptBoot] = `{"status":"ok","t":"lixo","boot":null,"degr":null}`
	r, _ := runWin(t, s, Deps{Lang: diagfmt.PT})
	if r.CPU == nil || r.CPU.Samples != 1 || r.CPU.AvgPct != 15 {
		t.Fatalf("cpu com escalar: %+v", r.CPU)
	}
	// sem núcleos informados, o % por processo não é inventado (assume 1): 8 fica 8
	if len(r.CPU.Top) != 1 || r.CPU.Top[0].Name != "solo" || r.CPU.Top[0].PID != 9 || r.CPU.Top[0].CPUPct != 8 {
		t.Fatalf("top com objeto único: %+v", r.CPU.Top)
	}
	if r.Memory != nil || !hasSkipped(r, "memoria") {
		t.Fatalf("memória só com o total não é leitura (ausente não é zero): %+v %+v", r.Memory, r.Skipped)
	}
	if r.Disk == nil || r.Disk.Medium != "" || r.Disk.FreeBytes == nil || r.Disk.QueueAvg != nil {
		t.Fatalf("Unspecified é desconhecido, nunca HDD: %+v", r.Disk)
	}
	if r.Startup == nil || r.Startup.Total != 1 {
		t.Fatalf("startup: %+v", r.Startup)
	}
	if r.Services == nil || r.Services.AutoCount != 1 {
		t.Fatalf("serviços: %+v", r.Services)
	}
	if r.Boot == nil || !r.Boot.Found || !r.Boot.When.IsZero() || r.Boot.Degraded != nil {
		t.Fatalf("boot com data inválida: %+v", r.Boot)
	}
}

func TestAmostraTodaZeradaNaoViraCPUOciosa(t *testing.T) {
	s := fxAll()
	s[scriptPerf] = `{"cpu":[0,0,0],"q":[],"lat":[],"lp":4,"top":[]}`
	r, _ := runWin(t, s, Deps{Lang: diagfmt.PT})
	if r.CPU != nil {
		t.Fatalf("amostras todas zero = sem leitura válida, não CPU ociosa: %+v", r.CPU)
	}
	if !hasSkipped(r, "cpu") {
		t.Fatalf("faltou o Skipped da cpu: %+v", r.Skipped)
	}
}

func hasSkipped(r *Report, area string) bool {
	for _, s := range r.Skipped {
		if s.Area == area {
			return true
		}
	}
	return false
}

func TestBootSemEventoENaoERapido(t *testing.T) {
	for _, st := range []string{"none", "nolog"} {
		s := fxAll()
		s[scriptBoot] = `{"status":"` + st + `"}`
		r, _ := runWin(t, s, Deps{Lang: diagfmt.PT})
		if r.Boot == nil || r.Boot.Found || r.Boot.DurationMs != 0 {
			t.Fatalf("%s: sem evento = sem dado: %+v", st, r.Boot)
		}
	}
}

func TestBootAcessoNegado(t *testing.T) {
	s := fxAll()
	s[scriptBoot] = `{"status":"denied"}`
	r, _ := runWin(t, s, Deps{Lang: diagfmt.PT})
	if r.Boot != nil || len(r.Skipped) != 1 || r.Skipped[0].Area != "boot" || !r.Skipped[0].NeedsAdmin {
		t.Fatalf("sem admin: Skipped.NeedsAdmin: %+v %+v", r.Boot, r.Skipped)
	}
	if !strings.Contains(r.Skipped[0].Reason, "administrador") {
		t.Errorf("a razão diz o que fazer: %q", r.Skipped[0].Reason)
	}
	r2, _ := runWin(t, s, Deps{Lang: diagfmt.PT, Admin: true})
	if r2.Boot != nil || len(r2.Skipped) != 1 || r2.Skipped[0].NeedsAdmin {
		t.Fatalf("como admin, negado continua sem dado e não pede admin de novo: %+v", r2.Skipped)
	}
}

func TestAreaQueFalhaViraSkippedEASeguinteSegue(t *testing.T) {
	f := exectest.New(t)
	f.On(psCmd(scriptPerf, perfTimeout).String(), exectest.Exit(1, ""))
	f.On(psCmd(scriptMem, readTimeout).String(), exectest.Exit(0, "isto não é json"))
	f.On(psCmd(scriptDisk, readTimeout).String(), exectest.Exit(0, ""))
	f.On(psCmd(scriptStartup, readTimeout).String(), exectest.Exit(0, `{"wmi":[],"run":[]}`))
	f.On(psCmd(scriptServices, readTimeout).String(), exectest.Exit(0, `[{"N":"A","D":"a","S":"Running","E":0,"P":"C:\\x\\a.exe"}]`))
	f.On(psCmd(scriptBoot, readTimeout).String(), exectest.Exit(0, `{"status":"error"}`))
	r := Collect(context.Background(), "windows", Deps{Runner: f, Lang: diagfmt.EN})
	for _, a := range []string{"cpu", "memoria", "disco", "boot"} {
		if !hasSkipped(r, a) {
			t.Errorf("faltou o Skipped de %s: %+v", a, r.Skipped)
		}
	}
	if r.Services == nil || r.Services.AutoCount != 1 {
		t.Fatalf("a área seguinte continua lida: %+v", r.Services)
	}
	if r.Startup == nil || r.Startup.Total != 0 {
		t.Fatalf("lista vazia de inicialização é um dado válido (0 itens): %+v", r.Startup)
	}
}

func TestContextoCancelado(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	f := exectest.New(t) // sem roteiro: qualquer chamada falharia o teste
	r := Collect(ctx, "windows", Deps{Runner: f})
	if len(f.Calls()) != 0 || len(r.Skipped) == 0 {
		t.Fatalf("cancelado: nada executa e as áreas ficam sem leitura: calls=%d skipped=%d", len(f.Calls()), len(r.Skipped))
	}
}

// Nome de processo e de serviço vêm do sistema: controle, ESC e bidi nunca chegam ao relatório nem ao JSON como bytes de controle.
func TestNomesMaliciososSaemLimpos(t *testing.T) {
	evil := "x\u001b]0;pwn\u0007\u202eexe.txt\u2028y"
	s := fxAll()
	j, _ := json.Marshal(map[string]any{"cpu": []float64{50}, "lp": 1, "top": []map[string]any{{"Name": evil, "Pid": 1, "Cpu": 5}}})
	s[scriptPerf] = string(j)
	j, _ = json.Marshal([]map[string]any{{"N": evil, "D": evil, "S": "Stopped", "E": 2, "P": evil}})
	s[scriptServices] = string(j)
	r, _ := runWin(t, s, Deps{Lang: diagfmt.PT})
	out := Summary(r, Analyze(r, diagfmt.PT), diagfmt.PT) + Full(r, diagfmt.PT)
	for _, bad := range []string{"\u001b", "\u0007", "\u202e", "\u2028"} {
		if strings.Contains(out, bad) {
			t.Errorf("o relatório trouxe %U do sistema", []rune(bad)[0])
		}
	}
	b, err := json.Marshal(r)
	if err != nil || !json.Valid(b) {
		t.Fatalf("json: %v", err)
	}
}

func TestListasCurtas(t *testing.T) {
	var items []map[string]any
	for i := 0; i < 200; i++ {
		items = append(items, map[string]any{"N": "Svc" + itoa(i), "D": "d", "S": "Stopped", "E": 1, "P": "C:\\p\\x" + itoa(i) + ".exe"})
	}
	j, _ := json.Marshal(items)
	s := fxAll()
	s[scriptServices] = string(j)
	r, _ := runWin(t, s, Deps{Lang: diagfmt.PT})
	if r.Services.AutoCount != 200 || r.Services.OutsideCount != 200 || len(r.Services.Outside) > maxList || len(r.Services.Failed) > maxList {
		t.Fatalf("a contagem é inteira e a lista é curta: %d %d %d %d", r.Services.AutoCount, r.Services.OutsideCount, len(r.Services.Outside), len(r.Services.Failed))
	}
}

// ---- latência do disco pelos contadores BRUTOS (PERF_AVERAGE_TIMER) ----

// perfRawJSON monta o JSON do script de desempenho com amostras brutas {n, d, f} do disco (n = ticks acumulados,
// d = transferências acumuladas, f = ticks por segundo). Sintético.
func perfRawJSON(samples ...[3]string) string {
	var parts []string
	for _, s := range samples {
		parts = append(parts, `{"n":"`+s[0]+`","d":"`+s[1]+`","f":"`+s[2]+`"}`)
	}
	return `{"cpu":[10,20,30],"q":[0.5,0.5,0.5],"lat":[` + strings.Join(parts, ",") + `],"lp":4,"top":[]}`
}

func latencyOf(t *testing.T, perf string) *float64 {
	t.Helper()
	s := fxAll()
	s[scriptPerf] = perf
	r, _ := runWin(t, s, Deps{Lang: diagfmt.PT})
	if r.Disk == nil {
		t.Fatalf("o disco devia ter sido lido: %+v", r.Skipped)
	}
	return r.Disk.LatencyMs
}

// Frequência 10 MHz; em 200 transferências, 6e7 ticks = 6 s /10e6... = 0,030 s por operação = 30 ms (bate o limiar de 25 ms).
// O valor formatado do CIM seria um uint32 em SEGUNDOS: 0,030 viraria 0 e nunca bateria o limiar.
func TestLatenciaDoDiscoPelosContadoresBrutos(t *testing.T) {
	for _, c := range []struct {
		name  string
		perf  string
		want  float64
		fires bool
	}{
		{"30 ms", perfRawJSON([3]string{"1000000000000", "1000", "10000000"}, [3]string{"1000030000000", "1100", "10000000"}, [3]string{"1000060000000", "1200", "10000000"}), 30, true},
		{"24,9 ms", perfRawJSON([3]string{"1000000000000", "1000", "10000000"}, [3]string{"1000024900000", "1100", "10000000"}, [3]string{"1000049800000", "1200", "10000000"}), 24.9, false},
	} {
		lat := latencyOf(t, c.perf)
		if lat == nil || math.Abs(*lat-c.want) > 0.001 {
			t.Fatalf("%s: latência = %v, quer %v ms", c.name, lat, c.want)
		}
		s := fxAll()
		s[scriptPerf] = c.perf
		r, _ := runWin(t, s, Deps{Lang: diagfmt.PT})
		got := false
		for _, f := range Analyze(r, diagfmt.PT) {
			if f.Area == "disco" && strings.Contains(f.Title, "lento para responder") {
				got = true
			}
		}
		if got != c.fires {
			t.Errorf("%s: achado de latência = %v, quer %v", c.name, got, c.fires)
		}
	}
}

// Amostra única, base sem variação, contador que volta atrás ou frequência zero: SEM LEITURA, nunca "0 ms" nem "ok".
func TestLatenciaSemLeituraNuncaVira0ms(t *testing.T) {
	for name, perf := range map[string]string{
		"amostra única":   perfRawJSON([3]string{"1000000000000", "1000", "10000000"}),
		"base zero":       perfRawJSON([3]string{"1000000000000", "1000", "10000000"}, [3]string{"1000030000000", "1000", "10000000"}),
		"contador voltou": perfRawJSON([3]string{"1000030000000", "1100", "10000000"}, [3]string{"1000000000000", "1200", "10000000"}),
		"frequência zero": perfRawJSON([3]string{"1", "10", "0"}, [3]string{"2", "20", "0"}),
		"sem lista":       `{"cpu":[10,20,30],"q":[1],"lat":null,"lp":4,"top":[]}`,
		"valor antigo":    `{"cpu":[10,20,30],"q":[1],"lat":[0,0,0],"lp":4,"top":[]}`,
	} {
		if lat := latencyOf(t, perf); lat != nil {
			t.Errorf("%s: devia ser sem leitura, veio %.3f ms", name, *lat)
		}
	}
}

// O script lê os contadores BRUTOS (os formatados são inteiros em segundos) e o teste de contrato dos scripts continua valendo.
func TestScriptPerfLeContadoresBrutosDeLatencia(t *testing.T) {
	for _, w := range []string{"Win32_PerfRawData_PerfDisk_PhysicalDisk", "AvgDisksecPerTransfer_Base", "Frequency_PerfTime"} {
		if !strings.Contains(scriptPerf, w) {
			t.Errorf("o script de desempenho precisa de %s", w)
		}
	}
	if strings.Contains(scriptPerf, "$d.AvgDisksecPerTransfer") {
		t.Error("o AvgDisksecPerTransfer formatado é inteiro em segundos: não pode alimentar a latência")
	}
}

// ---- presença dos campos: ausente não é zero ----

func collectWith(t *testing.T, over map[string]string) (*Report, []Finding) {
	t.Helper()
	s := fxAll()
	for k, v := range over {
		s[k] = v
	}
	r, _ := runWin(t, s, Deps{Lang: diagfmt.PT})
	return r, Analyze(r, diagfmt.PT)
}

func TestMemoriaDisponivelAusenteNaoEAvaliada(t *testing.T) {
	r, fs := collectWith(t, map[string]string{scriptMem: `{"total":17179869184,"avail":null,"committed":null,"limit":null,"swap":null,"top":null}`})
	for _, f := range fs {
		if f.Area == "memoria" {
			t.Errorf("sem RAM disponível não há achado de memória (nem 'disponível 0'): %+v", f)
		}
	}
	if r.Memory != nil && r.Memory.AvailableBytes != nil {
		t.Errorf("disponível ausente tem de continuar ausente: %v", *r.Memory.AvailableBytes)
	}
	if !hasSkipped(r, "memoria") {
		t.Errorf("a leitura que faltou tem de aparecer em Skipped: %+v", r.Skipped)
	}
	if strings.Contains(Full(r, diagfmt.PT), "disponível: 0") {
		t.Error("o relatório completo não diz 'disponível 0' para dado ausente")
	}
}

func TestMemoriaDisponivelZeroRealEPressao(t *testing.T) {
	r, fs := collectWith(t, map[string]string{scriptMem: `{"total":17179869184,"avail":0,"committed":null,"limit":null,"swap":null,"top":null}`})
	if r.Memory == nil || r.Memory.AvailableBytes == nil || *r.Memory.AvailableBytes != 0 {
		t.Fatalf("o zero real é uma medida: %+v", r.Memory)
	}
	if !has(fs, "memoria", SevWarn) {
		t.Errorf("RAM disponível 0 de verdade é pressão de memória: %+v", fs)
	}
}

func TestDiscoSoComDriveNaoEAreaLida(t *testing.T) {
	r, fs := collectWith(t, map[string]string{
		scriptDisk: `{"drive":"C","size":null,"free":null,"name":null,"media":null,"bus":null}`,
		scriptPerf: `{"cpu":[10,20,30],"q":[],"lat":[],"lp":4,"top":[]}`,
	})
	if r.Disk != nil {
		t.Fatalf("só com 'drive' (sem tamanho, livre, tipo, fila ou latência) a área não foi lida: %+v", r.Disk)
	}
	if !hasSkipped(r, "disco") {
		t.Errorf("faltou o Skipped do disco: %+v", r.Skipped)
	}
	for _, f := range fs {
		if f.Area == "disco" {
			t.Errorf("sem medida não há achado de disco: %+v", f)
		}
	}
}

// Relatório montado à mão: o mesmo vale na análise (Disk só com Drive e Memory só com o total não contam como lidos).
func TestAnaliseNaoContaAreaSemMedida(t *testing.T) {
	r := okReport()
	r.Disk = &Disk{Drive: "C"}
	r.Memory = &Memory{TotalBytes: 1000}
	fs := Analyze(r, diagfmt.PT)
	if has(fs, "sistema", SevOK) {
		t.Errorf("nenhuma medida válida: não pode ser SevOK: %+v", fs)
	}
	if !has(fs, "sistema", SevInfo) {
		t.Errorf("deve dizer que o resultado é inconclusivo: %+v", fs)
	}
}

// ---- resultado inconclusivo ----

func TestTodasAsConsultasFalhandoEInconclusivo(t *testing.T) {
	for _, c := range []struct {
		l    diagfmt.Lang
		want string
	}{
		{diagfmt.PT, "inconclusivo: nada foi lido"},
		{diagfmt.EN, "inconclusive: nothing was read"},
		{diagfmt.ES, "inconcluso: no se leyó nada"},
	} {
		f := exectest.New(t)
		for sc := range fxAll() {
			f.On(psCmd(sc, timeoutOf(sc)).String(), exectest.Exit(1, ""))
		}
		r := Collect(context.Background(), "windows", Deps{Runner: f, Lang: c.l, Now: func() time.Time { return testNow }})
		out := Summary(r, Analyze(r, c.l), c.l)
		if !strings.Contains(out, c.want) {
			t.Errorf("%v: faltou %q:\n%s", c.l, c.want, out)
		}
		for _, bad := range []string{"nenhum gargalo", "no evident", "ningún cuello"} {
			if strings.Contains(strings.ToLower(out), bad) {
				t.Errorf("%v: nada foi lido, não pode dizer %q:\n%s", c.l, bad, out)
			}
		}
	}
}

// Com ao menos uma área relevante lida e sem achado, continua "nenhum gargalo evidente".
func TestAlgoLidoSemAchadoContinuaSemGargalo(t *testing.T) {
	r := okReport()
	r.CPU = &CPU{Samples: 3, AvgPct: 10, PeakPct: 20}
	out := Summary(r, Analyze(r, diagfmt.PT), diagfmt.PT)
	if !strings.Contains(out, "nenhum gargalo evidente") || strings.Contains(out, "inconclusivo") {
		t.Errorf("área lida e sem achado:\n%s", out)
	}
}

// ---- fila do disco pelos contadores BRUTOS (PERF_COUNTER_100NS_QUEUELEN_TYPE) ----

// queueRawJSON monta o JSON de desempenho com amostras {n,d,f,q,t}: q = AvgDiskQueueLength bruto (integral acumulada) e
// t = Timestamp_Sys100NS (em 100 ns). A fila média entre duas amostras é (q1-q0)/(t1-t0). Sintético.
func queueRawJSON(samples ...[2]string) string {
	var parts []string
	for _, s := range samples {
		parts = append(parts, `{"n":"1000","d":"10","f":"10000000","q":"`+s[0]+`","t":"`+s[1]+`"}`)
	}
	return `{"cpu":[10,20,30],"lat":[` + strings.Join(parts, ",") + `],"lp":4,"top":[]}`
}

func queueOf(t *testing.T, perf string) (avg, peak *float64) {
	t.Helper()
	s := fxAll()
	s[scriptPerf] = perf
	r, _ := runWin(t, s, Deps{Lang: diagfmt.PT})
	if r.Disk == nil {
		t.Fatalf("o disco devia ter sido lido: %+v", r.Skipped)
	}
	return r.Disk.QueueAvg, r.Disk.QueuePeak
}

func queueFires(t *testing.T, perf string) bool {
	t.Helper()
	s := fxAll()
	s[scriptPerf] = perf
	r, _ := runWin(t, s, Deps{Lang: diagfmt.PT})
	for _, f := range Analyze(r, diagfmt.PT) {
		if strings.Contains(strings.ToLower(f.Title), "fila") {
			return true
		}
	}
	return false
}

// Lupa #79: intervalos de 1,9 e 2,9 (média real 2,4) DEVEM alertar (limiar >= 2); o valor formatado do CIM é inteiro e
// truncado daria (1+2)/2 = 1,5 e o alerta sumiria.
func TestFilaDoDiscoPelosContadoresBrutosComFracoes(t *testing.T) {
	// 1 s = 1e7 unidades de 100 ns; dq = fila x dt
	alerta := queueRawJSON([2]string{"0", "1000000000"}, [2]string{"19000000", "1010000000"}, [2]string{"48000000", "1020000000"})
	avg, peak := queueOf(t, alerta)
	if avg == nil || math.Abs(*avg-2.4) > 0.001 || peak == nil || math.Abs(*peak-2.9) > 0.001 {
		t.Fatalf("fila média/pico = %v / %v, quer 2,4 / 2,9", avg, peak)
	}
	if !queueFires(t, alerta) {
		t.Error("média 2,4 (>= 2) devia alertar")
	}
	abaixo := queueRawJSON([2]string{"0", "1000000000"}, [2]string{"19000000", "1010000000"}, [2]string{"38000000", "1020000000"}) // 1,9 e 1,9
	if a, _ := queueOf(t, abaixo); a == nil || math.Abs(*a-1.9) > 0.001 {
		t.Fatalf("fila média = %v, quer 1,9", a)
	}
	if queueFires(t, abaixo) {
		t.Error("média 1,9 (< 2) não devia alertar")
	}
	igual := queueRawJSON([2]string{"0", "1000000000"}, [2]string{"20000000", "1010000000"}, [2]string{"40000000", "1020000000"}) // 2,0
	if !queueFires(t, igual) {
		t.Error("média exatamente 2 (>= 2) devia alertar")
	}
}

// Sem leitura válida a fila é nil (nunca 0 e nunca 'ok'): amostra única, tempo que não avança, contador que voltou.
func TestFilaDoDiscoSemLeituraValida(t *testing.T) {
	for name, perf := range map[string]string{
		"uma amostra":      queueRawJSON([2]string{"0", "1000000000"}),
		"tempo parado":     queueRawJSON([2]string{"0", "1000000000"}, [2]string{"19000000", "1000000000"}),
		"contador voltou":  queueRawJSON([2]string{"50000000", "1000000000"}, [2]string{"10000000", "1010000000"}),
		"sem campos q e t": `{"cpu":[10,20,30],"lat":[{"n":"1000","d":"10","f":"10000000"},{"n":"2000","d":"20","f":"10000000"}],"lp":4,"top":[]}`,
		"formato antigo q": `{"cpu":[10,20,30],"q":[0.5,3,2],"lat":[],"lp":4,"top":[]}`,
	} {
		s := fxAll()
		s[scriptPerf] = perf
		r, _ := runWin(t, s, Deps{Lang: diagfmt.PT})
		if r.Disk != nil && (r.Disk.QueueAvg != nil || r.Disk.QueuePeak != nil) {
			t.Errorf("%s: fila sem leitura válida tem de ser nil: %v / %v", name, r.Disk.QueueAvg, r.Disk.QueuePeak)
		}
	}
}

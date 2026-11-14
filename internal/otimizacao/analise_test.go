package otimizacao

import (
	"strings"
	"testing"

	"github.com/PiluTech/toolkit/internal/diagfmt"
)

func u64(n uint64) *uint64   { return &n }
func f64(n float64) *float64 { return &n }
func bptr(b bool) *bool      { return &b }
func okReport() *Report      { return &Report{OS: "windows", Supported: true, Generated: testNow} }
func has(fs []Finding, area string, sev Severity) bool {
	for _, f := range fs {
		if f.Area == area && f.Severity == sev {
			return true
		}
	}
	return false
}
func areas(fs []Finding, area string) int {
	n := 0
	for _, f := range fs {
		if f.Area == area {
			n++
		}
	}
	return n
}

// Fronteiras de cada limiar: abaixo, igual e acima.
func TestLimiarCPU(t *testing.T) {
	for _, c := range []struct {
		avg  float64
		want bool
	}{{79.9, false}, {80, true}, {80.1, true}} {
		r := okReport()
		r.CPU = &CPU{Samples: 3, AvgPct: c.avg, PeakPct: 100}
		if got := areas(Analyze(r, diagfmt.PT), "cpu") > 0; got != c.want {
			t.Errorf("média %.1f: achado=%v, quer %v", c.avg, got, c.want)
		}
	}
}

func TestLimiarMemoriaEmUso(t *testing.T) {
	const total = 1000
	for _, c := range []struct {
		avail uint64
		want  bool
	}{{101, false}, {100, true}, {99, true}} { // 90% em uso = 10% disponível
		r := okReport()
		r.Memory = &Memory{TotalBytes: total, AvailableBytes: c.avail}
		if got := has(Analyze(r, diagfmt.PT), "memoria", SevWarn); got != c.want {
			t.Errorf("disponível %d de %d: atenção=%v, quer %v", c.avail, total, got, c.want)
		}
	}
	// sem leitura do disponível (0 por nulo) NÃO vira "memória lotada": o dado ausente não é uso de 100%
	r := okReport()
	r.Memory = &Memory{TotalBytes: total}
	if got := Analyze(r, diagfmt.PT); has(got, "memoria", SevWarn) {
		t.Errorf("sem leitura do disponível não gera achado: %+v", got)
	}
}

func TestLimiarCommitEPaginacao(t *testing.T) {
	for _, c := range []struct {
		used uint64
		want bool
	}{{899, false}, {900, true}, {901, true}} {
		r := okReport()
		r.Memory = &Memory{TotalBytes: 1000, AvailableBytes: 1000, CommitBytes: c.used, CommitLimit: 1000}
		if got := has(Analyze(r, diagfmt.PT), "memoria", SevWarn); got != c.want {
			t.Errorf("commit %d/1000: %v", c.used, got)
		}
	}
	for _, c := range []struct {
		used uint64
		want bool
	}{{799, false}, {800, true}, {801, true}} {
		r := okReport()
		r.Memory = &Memory{TotalBytes: 1000, AvailableBytes: 1000, PageAllocBytes: 1000, PageUsedBytes: c.used}
		if got := has(Analyze(r, diagfmt.PT), "memoria", SevInfo); got != c.want {
			t.Errorf("paginação %d/1000: %v", c.used, got)
		}
	}
}

func TestLimiarEspacoLivre(t *testing.T) {
	for _, c := range []struct {
		free uint64
		sev  Severity
		any  bool
	}{{100, SevOK, false}, {99, SevWarn, true}, {51, SevWarn, true}, {50, SevWarn, true}, {49, SevBad, true}} {
		// tamanho 1000: <10% (100) = atenção; <5% (50) = problema; igual ao limite NÃO passa para a faixa pior
		r := okReport()
		r.Disk = &Disk{Drive: "C", SizeBytes: 1000, FreeBytes: u64(c.free)}
		fs := Analyze(r, diagfmt.PT)
		if c.any != (areas(fs, "disco") > 0) || (c.any && !has(fs, "disco", c.sev)) {
			t.Errorf("livre %d/1000: %+v", c.free, fs)
		}
	}
	// livre desconhecido (nil) não é "disco cheio"
	r := okReport()
	r.Disk = &Disk{Drive: "C", SizeBytes: 1000}
	if areas(Analyze(r, diagfmt.PT), "disco") != 0 {
		t.Error("espaço livre desconhecido não gera achado")
	}
}

func TestLimiarFilaELatencia(t *testing.T) {
	for _, c := range []struct {
		q    float64
		want bool
	}{{1.99, false}, {2, true}, {2.01, true}} {
		r := okReport()
		r.Disk = &Disk{Drive: "C", QueueAvg: f64(c.q)}
		if got := areas(Analyze(r, diagfmt.PT), "disco") > 0; got != c.want {
			t.Errorf("fila %.2f: %v", c.q, got)
		}
	}
	for _, c := range []struct {
		ms   float64
		want bool
	}{{24.9, false}, {25, true}, {25.1, true}} {
		r := okReport()
		r.Disk = &Disk{Drive: "C", LatencyMs: f64(c.ms)}
		if got := areas(Analyze(r, diagfmt.PT), "disco") > 0; got != c.want {
			t.Errorf("latência %.1f ms: %v", c.ms, got)
		}
	}
}

func TestDiscoDoSistemaHDD(t *testing.T) {
	for _, c := range []struct {
		medium string
		want   bool
	}{{"HDD", true}, {"SSD", false}, {"", false}} { // desconhecido (VM, RAID) nunca é HDD
		r := okReport()
		r.Disk = &Disk{Drive: "C", Medium: c.medium}
		if got := has(Analyze(r, diagfmt.PT), "disco", SevWarn); got != c.want {
			t.Errorf("tipo %q: %v", c.medium, got)
		}
	}
}

func TestLimiarInicializacao(t *testing.T) {
	for _, c := range []struct {
		n    int
		want bool
	}{{10, false}, {11, true}, {40, true}} { // "muitos" = mais de 10
		r := okReport()
		r.Startup = &Startup{Total: c.n}
		if got := has(Analyze(r, diagfmt.PT), "inicializacao", SevInfo); got != c.want {
			t.Errorf("%d itens: %v", c.n, got)
		}
	}
}

func TestLimiarServicos(t *testing.T) {
	for _, c := range []struct {
		n    int
		want bool
	}{{25, false}, {26, true}} {
		r := okReport()
		r.Services = &Services{AutoCount: 100, OutsideCount: c.n}
		if got := has(Analyze(r, diagfmt.PT), "servicos", SevInfo); got != c.want {
			t.Errorf("%d fora da pasta do Windows: %v", c.n, got)
		}
	}
	r := okReport()
	r.Services = &Services{Failed: []Service{{Name: "X", ExitCode: 1}}}
	if !has(Analyze(r, diagfmt.PT), "servicos", SevInfo) {
		t.Error("serviço automático parado com erro é um achado informativo")
	}
}

func TestBootDegradadoESemEvento(t *testing.T) {
	r := okReport()
	r.Boot = &Boot{Found: true, DurationMs: 90000, Degraded: bptr(true)}
	if !has(Analyze(r, diagfmt.PT), "boot", SevInfo) {
		t.Error("boot com a marca de degradação do Windows é achado")
	}
	r.Boot = &Boot{Found: true, DurationMs: 20000, Degraded: bptr(false)}
	if areas(Analyze(r, diagfmt.PT), "boot") != 0 {
		t.Error("boot sem degradação não gera achado")
	}
	// sem evento = SEM DADO, em qualquer idioma, e nunca "rápido"
	for _, l := range []diagfmt.Lang{diagfmt.PT, diagfmt.EN, diagfmt.ES} {
		r.Boot = &Boot{Found: false}
		fs := Analyze(r, l)
		if !has(fs, "boot", SevInfo) {
			t.Fatalf("%v: sem evento aparece como informação", l)
		}
		// a única menção a "rápido" é a frase que NEGA a conclusão (noEventNote); tirada ela, nenhuma sobra
		out := strings.ToLower(strings.ReplaceAll(Summary(r, fs, l)+Full(r, l), noEventNote(l), ""))
		if !strings.Contains(strings.ToLower(Summary(r, fs, l)), strings.ToLower(noEventNote(l))) {
			t.Errorf("%v: o resumo precisa dizer que sem evento não é boot rápido", l)
		}
		for _, w := range []string{"rápid", "rapid", "fast", "veloz", "quick"} {
			if strings.Contains(out, w) {
				t.Errorf("%v: 'sem evento' não pode virar %q: %s", l, w, out)
			}
		}
	}
}

func TestSemNadaLidoNaoDizTudoBem(t *testing.T) {
	fs := Analyze(okReport(), diagfmt.PT)
	if len(fs) != 1 || fs[0].Severity != SevInfo {
		t.Fatalf("nada lido: um aviso de informação, nunca 'tudo certo': %+v", fs)
	}
	r := okReport()
	r.CPU = &CPU{Samples: 3, AvgPct: 5, PeakPct: 9}
	fs = Analyze(r, diagfmt.PT)
	if len(fs) != 1 || fs[0].Severity != SevOK {
		t.Fatalf("algo lido e sem problema: um OK: %+v", fs)
	}
}

func TestOrdemDosAchados(t *testing.T) {
	r := everythingReport()
	fs := Analyze(r, diagfmt.PT)
	for i := 1; i < len(fs); i++ {
		if fs[i].Severity > fs[i-1].Severity {
			t.Fatalf("do mais grave ao mais leve: %+v", fs)
		}
	}
}

// everythingReport dispara todos os achados.
func everythingReport() *Report {
	r := okReport()
	r.CPU = &CPU{Samples: 3, AvgPct: 97, PeakPct: 100, Top: []Process{{Name: "chrome", PID: 1, CPUPct: 60}}}
	r.Memory = &Memory{TotalBytes: 1000, AvailableBytes: 10, CommitBytes: 990, CommitLimit: 1000, PageAllocBytes: 1000, PageUsedBytes: 900, Top: []Process{{Name: "chrome", PID: 1, MemBytes: 500}}}
	r.Disk = &Disk{Drive: "C", Medium: "HDD", SizeBytes: 1000, FreeBytes: u64(10), QueueAvg: f64(9), LatencyMs: f64(90)}
	r.Startup = &Startup{Total: 30, Items: []StartupItem{{Name: "A", Command: "a.exe"}}}
	r.Services = &Services{AutoCount: 120, OutsideCount: 50, Outside: []Service{{Name: "S1"}}, Failed: []Service{{Name: "F1", ExitCode: 1}}}
	r.Boot = &Boot{Found: true, DurationMs: 120000, Degraded: bptr(true)}
	return r
}

// A lista fixa do que NUNCA recomendar: nenhum achado (título, detalhe, ação), em nenhum idioma, contém os termos.
func TestListaDoQueNuncaRecomendar(t *testing.T) {
	reports := []*Report{everythingReport()}
	r2 := everythingReport()
	r2.Boot = &Boot{Found: false}
	reports = append(reports, r2)
	for _, l := range []diagfmt.Lang{diagfmt.PT, diagfmt.EN, diagfmt.ES} {
		for _, r := range reports {
			for _, f := range Analyze(r, l) {
				text := strings.ToLower(f.Title + " | " + f.Detail + " | " + f.Action)
				for _, term := range NeverRecommendTerms {
					if strings.Contains(text, strings.ToLower(term)) {
						t.Errorf("%v: o achado %q cita %q, que o toolkit nunca recomenda: %s", l, f.Area, term, text)
					}
				}
				if f.Area == "servicos" {
					for _, term := range neverServiceTerms {
						if strings.Contains(text, term) {
							t.Errorf("%v: achado de serviço fala em %q: %s", l, term, text)
						}
					}
				}
			}
		}
	}
	if len(NeverRecommendTerms) < 10 || len(NeverList(diagfmt.PT)) < 5 {
		t.Fatal("a lista fixa não pode encolher sem ninguém ver")
	}
	// os três idiomas têm a lista inteira
	if len(NeverList(diagfmt.EN)) != len(NeverList(diagfmt.PT)) || len(NeverList(diagfmt.ES)) != len(NeverList(diagfmt.PT)) {
		t.Fatal("a lista tem o mesmo tamanho nos três idiomas")
	}
}

// Todo achado tem "o que fazer" só como texto e nunca manda executar nada do toolkit (nenhum comando do toolkit na ação).
func TestAcoesSaoSoTexto(t *testing.T) {
	for _, l := range []diagfmt.Lang{diagfmt.PT, diagfmt.EN, diagfmt.ES} {
		for _, f := range Analyze(everythingReport(), l) {
			if strings.Contains(strings.ToLower(f.Action), "toolkit ") && !strings.Contains(strings.ToLower(f.Action), "toolkit não") && !strings.Contains(strings.ToLower(f.Action), "toolkit does not") && !strings.Contains(strings.ToLower(f.Action), "toolkit no ") {
				t.Errorf("%v: a ação manda rodar o toolkit: %q", l, f.Action)
			}
		}
	}
}

func TestSummaryIdiomasEFronteiras(t *testing.T) {
	r := everythingReport()
	r.Skipped = []Skipped{{Area: "boot", Reason: "exige administrador", NeedsAdmin: true}}
	for _, c := range []struct {
		l    diagfmt.Lang
		want []string
	}{
		{diagfmt.PT, []string{"Otimização do Windows", "O que fazer:", "Não lido", "nada foi alterado"}},
		{diagfmt.EN, []string{"Windows optimization", "What to do:", "Not read", "nothing was changed"}},
		{diagfmt.ES, []string{"Optimización de Windows", "Qué hacer:", "No leído", "no se cambió nada"}},
	} {
		out := Summary(r, Analyze(r, c.l), c.l)
		for _, w := range c.want {
			if !strings.Contains(out, w) {
				t.Errorf("%v: faltou %q:\n%s", c.l, w, out)
			}
		}
	}
}

func TestFullTrazOsDados(t *testing.T) {
	r := everythingReport()
	out := Full(r, diagfmt.PT)
	for _, w := range []string{"CPU", "chrome", "Memória", "Disco do sistema", "HDD", "Inicialização", "Serviços", "Boot", "120"} {
		if !strings.Contains(out, w) {
			t.Errorf("faltou %q:\n%s", w, out)
		}
	}
	// relatório vazio não quebra
	_ = Full(okReport(), diagfmt.EN)
	_ = Summary(okReport(), Analyze(okReport(), diagfmt.EN), diagfmt.EN)
}

func TestLegenda(t *testing.T) {
	for _, l := range []diagfmt.Lang{diagfmt.PT, diagfmt.EN, diagfmt.ES} {
		out := Legend(l)
		if !strings.Contains(out, "80") || !strings.Contains(out, "10") || len(strings.Split(out, "\n")) < 8 {
			t.Errorf("%v: a legenda traz os limiares e a lista do que não se faz:\n%s", l, out)
		}
	}
}

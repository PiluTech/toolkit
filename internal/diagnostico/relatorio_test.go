package diagnostico

import (
	"strings"
	"testing"
	"time"

	"github.com/PiluTech/toolkit/internal/diagfmt"
)

func sample() *Report {
	return &Report{OS: "windows", Generated: time.Date(2026, 11, 14, 10, 0, 0, 0, time.UTC),
		Hardware: Hardware{Vendor: "ACME", Model: "Note 15", CPU: "CPU X 3 GHz", Cores: 4, Threads: 8, Uptime: 50 * time.Hour},
		Memory:   Memory{TotalBytes: 8 << 30, UsedBytes: 5 << 30, SwapTotalBytes: 2 << 30, SwapUsedBytes: 1 << 30},
		Disks:    []Disk{{Name: "LITEON CV8", Medium: "SSD", Bus: "NVMe", SizeBytes: 256060514304, FreeBytes: 50 << 30, Health: HealthOK, HealthRaw: "Healthy", WearPct: ip(12), TempC: fp(41), PowerOnHours: ip(1234)}},
		Battery:  &Battery{Percent: ip(80), Status: "descarregando", HealthPct: ip(55), CycleCount: ip(412)},
		Temps:    []Temp{{Sensor: "CPU", Celsius: 61}}, GPUs: []GPU{{Name: "GPU Y", Driver: "31.0", VRAMBytes: 4 << 30}},
		Events:  []Event{{When: time.Date(2026, 11, 13, 9, 0, 0, 0, time.UTC), Source: "disk", ID: "7", Level: "erro", Message: "falha em C:\\Users\\paulo\\x", Count: 3}},
		Crashes: []Crash{{When: time.Date(2026, 11, 1, 8, 0, 0, 0, time.UTC), Kind: CrashBSOD, Detail: "0x0000007E"}},
		Skipped: []Skipped{{Area: "temperatura", Reason: "rode como administrador"}}}
}

func TestSummaryTemSimboloPalavraEAcao(t *testing.T) {
	r := sample()
	out := Summary(r, Analyze(r, diagfmt.PT), diagfmt.PT)
	for _, want := range []string{"Diagnóstico da máquina (Windows)", "! atenção: Bateria gasta", "55% da capacidade original", "O que fazer: Considere trocar a bateria", "! atenção: Tela azul (BSOD)", "- info: Não foi possível ler: temperatura"} {
		if !strings.Contains(out, want) {
			t.Errorf("faltou %q:\n%s", want, out)
		}
	}
	if strings.Index(out, "Bateria gasta") > strings.Index(out, "Não foi possível ler") {
		t.Errorf("info vem depois dos problemas:\n%s", out)
	}
	ok := Summary(&Report{OS: "linux"}, Analyze(&Report{OS: "linux"}, diagfmt.EN), diagfmt.EN)
	if !strings.Contains(ok, "✓ ok: Nothing bad found") || !strings.Contains(ok, "Result: nothing bad found") {
		t.Errorf("%s", ok)
	}
}

func TestFullTemTodasAsAreasESemTexto(t *testing.T) {
	out := Full(sample(), diagfmt.PT)
	for _, want := range []string{"== Hardware ==", "Núcleos / threads: 4 / 8", "Ligado há: 2 dias 2 h", "== Memória ==", "8 GB no total, 5 GB em uso (63%)", "Swap: 1 GB / 2 GB",
		"== Discos ==", "LITEON CV8 · SSD · NVMe · 256 GB · 50 GB livres · saúde: Healthy · desgaste 12% · 41 °C · 1234 h ligado", "== Bateria ==", "80% · descarregando · capacidade 55% · 412 ciclos",
		"== Temperaturas ==", "- CPU: 61 °C", "== GPU ==", "GPU Y · driver 31.0 · 4 GB", "== Erros recentes (período lido) ==", "disk (7) ×3 [erro]", "== Falhas graves (30 dias) ==", "bsod 0x0000007E", "== Não lido ==", "temperatura: rode como administrador"} {
		if !strings.Contains(out, want) {
			t.Errorf("faltou %q:\n%s", want, out)
		}
	}
	// nada é mascarado: o caminho com o nome do usuário aparece como veio
	if !strings.Contains(out, `C:\Users\paulo\x`) {
		t.Errorf("o texto do evento aparece como o sistema informou:\n%s", out)
	}
	empty := Full(&Report{OS: "linux"}, diagfmt.ES)
	for _, want := range []string{"no leída", "ningún disco leído", "sin batería", "ningún sensor leído"} {
		if !strings.Contains(empty, want) {
			t.Errorf("vazio es: faltou %q:\n%s", want, empty)
		}
	}
}

// Decisão do Paulo: série, UUID, nome da máquina e MACs aparecem no relatório completo (ferramenta de análise).
func TestFullMostraSerieUUIDHostnameENICs(t *testing.T) {
	r := sample()
	r.Hardware.Serial, r.Hardware.UUID, r.Hardware.Hostname = "SN-FAKE-0001", "00000000-0000-4000-8000-000000000001", "HOST-FAKE"
	r.NICs = []NIC{{Name: "Ethernet", MAC: "02:00:00:00:00:01"}, {Name: "Wi-Fi", MAC: "02:00:00:00:00:02"}}
	r.Disks[0].Serial = "DSK-FAKE-9"
	out := Full(r, diagfmt.PT)
	for _, want := range []string{"Nome da máquina: HOST-FAKE", "Número de série: SN-FAKE-0001", "UUID (ID de hardware): 00000000-0000-4000-8000-000000000001",
		"== Rede (placas físicas) ==", "- Ethernet: 02:00:00:00:00:01", "- Wi-Fi: 02:00:00:00:00:02", "série DSK-FAKE-9"} {
		if !strings.Contains(out, want) {
			t.Errorf("faltou %q:\n%s", want, out)
		}
	}
	if en := Full(r, diagfmt.EN); !strings.Contains(en, "Serial number: SN-FAKE-0001") || !strings.Contains(en, "Machine name: HOST-FAKE") {
		t.Errorf("en:\n%s", en)
	}
}

// Sem administrador o resumo diz o que se ganharia com ele (só as áreas com NeedsAdmin) e como rodar; com administrador
// (ou sem nada a ganhar) a linha não aparece.
func TestSummaryDizOQueSeGanhaComAdmin(t *testing.T) {
	r := &Report{OS: "linux", Skipped: []Skipped{{Area: "smart", Reason: "x", NeedsAdmin: true}, {Area: "gpu", Reason: "y"}, {Area: "temperatura", Reason: "z", NeedsAdmin: true}}}
	out := Summary(r, Analyze(r, diagfmt.PT), diagfmt.PT)
	if !strings.Contains(out, "Com administrador o diagnóstico também lê: saúde SMART dos discos, temperatura (rode com sudo (sudo toolkit diagnostico)).") || strings.Contains(out, "GPU (rode") {
		t.Errorf("só as áreas com NeedsAdmin:\n%s", out)
	}
	r.OS = "windows"
	if out := Summary(r, Analyze(r, diagfmt.PT), diagfmt.PT); !strings.Contains(out, "abra o terminal como administrador e rode de novo") {
		t.Errorf("windows:\n%s", out)
	}
	if out := Summary(r, Analyze(r, diagfmt.EN), diagfmt.EN); !strings.Contains(out, "With administrator rights the diagnosis also reads: disk SMART health, temperature") {
		t.Errorf("en:\n%s", out)
	}
	r.Admin = true
	if out := Summary(r, Analyze(r, diagfmt.PT), diagfmt.PT); strings.Contains(out, "Com administrador") {
		t.Errorf("já é admin, sem a linha:\n%s", out)
	}
	if out := Summary(&Report{OS: "linux"}, Analyze(&Report{OS: "linux"}, diagfmt.PT), diagfmt.PT); strings.Contains(out, "Com administrador") {
		t.Errorf("nada a ganhar, sem a linha:\n%s", out)
	}
}

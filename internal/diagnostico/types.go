// Package diagnostico reúne, SÓ PARA LEITURA, o estado de saúde da máquina (hardware, discos com SMART, bateria,
// temperatura, GPU e erros recentes), transforma em achados ("o que está ruim e o que fazer") e monta o resumo e o
// relatório completo (as informações do PC aparecem como são: nada é mascarado). Os coletores de cada SO (coleta_win.go, coleta_lin.go,
// coleta_mac.go) só usam o exec.Runner de leitura e as funções de arquivo injetadas em Deps: nenhum teste toca o
// sistema real e nada pede administrador (o que exigiria admin vira um item em Report.Skipped, com a instrução).
package diagnostico

import (
	"time"

	"github.com/PiluTech/toolkit/internal/diagfmt"
)

// Health é a saúde normalizada de um disco (a leitura crua fica em Disk.HealthRaw).
type Health int

// Saúde de disco.
const (
	HealthUnknown Health = iota // sem leitura (SMART indisponível ou exige administrador)
	HealthOK
	HealthWarn // atenção: desgaste alto, setores realocados, status "Warning"
	HealthBad  // com problema: SMART "Failing"/"Unhealthy"
)

// CrashKind é o tipo de falha grave registrada pelo sistema.
type CrashKind string

// Tipos de falha.
const (
	CrashBSOD        CrashKind = "bsod"         // Windows: tela azul (BugCheck, evento 1001)
	CrashKernelPower CrashKind = "kernel-power" // Windows: desligamento inesperado (Kernel-Power 41)
	CrashKernelPanic CrashKind = "kernel-panic" // macOS: panic; Linux: kernel panic/oops
	CrashApp         CrashKind = "app"          // falha de aplicativo (relatório de crash)
)

// Report é tudo o que a coleta leu. Campos ausentes = não lido (ver Skipped); ponteiros nil = sem dado.
type Report struct {
	OS        string // "windows", "linux" ou "darwin"
	Generated time.Time
	Hardware  Hardware
	Memory    Memory
	Disks     []Disk
	Battery   *Battery // nil = sem bateria (desktop) ou não lida
	Temps     []Temp
	GPUs      []GPU
	Events    []Event  // erros e críticos recentes (7 dias), agrupados por origem+id, mais recentes primeiro
	Crashes   []Crash  // BSOD, desligamento inesperado, kernel panic, crash de app (30 dias), mais recentes primeiro
	Stability *float64 // Windows: índice de confiabilidade (0 a 10), nil nos outros
	NICs      []NIC    // placas de rede físicas (nome e MAC)
	Skipped   []Skipped
	// Admin: a coleta rodou como administrador (Windows) ou root (Linux e macOS). Com admin os coletores leem mais (SMART no
	// Linux, temperatura no macOS, o que o firmware só expõe a administrador); sem admin tudo continua funcionando e o que
	// ficou de fora aparece em Skipped com NeedsAdmin.
	Admin bool
}

// NIC é uma placa de rede física.
type NIC struct {
	Name string
	MAC  string
}

// Hardware é a identificação da máquina (sem número de série).
type Hardware struct {
	Vendor, Model string
	Serial        string // número de série da máquina (BIOS/placa); vazio = não lido
	UUID          string // UUID/ID de hardware; vazio = não lido
	Hostname      string // nome da máquina
	CPU           string
	Cores         int // núcleos físicos (0 = desconhecido)
	Threads       int // threads lógicas (0 = desconhecido)
	Firmware      string
	Uptime        time.Duration // 0 = desconhecido
}

// Memory é a RAM e o swap (o detalhe dos pentes é do diag-memoria).
type Memory struct {
	TotalBytes, UsedBytes         uint64 // UsedBytes 0 = desconhecido
	SwapTotalBytes, SwapUsedBytes uint64
}

// Disk é um disco físico.
type Disk struct {
	Name               string // modelo ou nome
	Serial             string // número de série do disco; vazio = não lido
	Medium             string // "SSD", "HDD" ou ""
	Bus                string // "NVMe", "SATA", "USB"... ou ""
	SizeBytes          uint64
	FreeBytes          uint64 // 0 = desconhecido (soma dos volumes do disco quando o SO informa)
	Health             Health
	HealthRaw          string // texto do SO ("Healthy", "Verified", "PASSED"...)
	TempC              *float64
	WearPct            *int // desgaste do SSD (0 a 100; 100 = vida esgotada)
	PowerOnHours       *int
	ReallocatedSectors *int
}

// Battery é a bateria (nil em Report quando não há).
type Battery struct {
	Percent    *int   // carga atual (0 a 100)
	Status     string // "carregando", "descarregando", "cheia" (já no idioma da coleta) ou ""
	CycleCount *int
	HealthPct  *int   // capacidade atual / capacidade de projeto (0 a 100); nil = desconhecido
	Condition  string // texto do SO ("Normal", "Service Recommended", "Replace Soon"...)
}

// Temp é um sensor de temperatura.
type Temp struct {
	Sensor  string
	Celsius float64
}

// GPU é uma placa de vídeo.
type GPU struct {
	Name, Driver string
	VRAMBytes    uint64 // 0 = desconhecido ou memória compartilhada
}

// Event é um erro (ou crítico) recente do log do sistema, agrupado.
type Event struct {
	When    time.Time // o mais recente do grupo
	Source  string    // provedor, unidade ou processo
	ID      string    // id do evento quando existe
	Level   string    // "erro" ou "crítico" (no idioma da coleta)
	Message string    // primeira linha da mensagem, já limpa (o relatório ainda a redige)
	Count   int       // ocorrências no período (>= 1)
}

// Crash é uma falha grave registrada.
type Crash struct {
	When   time.Time
	Kind   CrashKind
	Detail string // código de parada, nome do processo, resumo
}

// Skipped é uma área que não pôde ser lida, com o motivo e o que fazer (nunca silêncio).
type Skipped struct {
	Area   string // "smart", "temperatura", "bateria", "eventos"... (id estável)
	Reason string // frase no idioma da coleta, com a instrução quando houver
	// NeedsAdmin: rodando como administrador/root esta área seria lida (o resumo lista o que se ganha com admin).
	NeedsAdmin bool
}

// Severity é a gravidade de um achado.
type Severity int

// Gravidades (em ordem crescente).
const (
	SevOK   Severity = iota // tudo certo
	SevInfo                 // informação útil, sem ação
	SevWarn                 // atenção: vale agir
	SevBad                  // problema: agir logo
)

// Finding é uma linha do resumo "o que está ruim e o que fazer".
type Finding struct {
	Area     string // "disco", "memoria", "bateria", "temperatura", "gpu", "erros", "falhas", "sistema"
	Severity Severity
	Title    string // o que está ruim (ou ok)
	Detail   string // números e contexto
	Action   string // o que fazer ("" quando não há ação)
}

// Lang é o idioma da saída (o mesmo do diagfmt).
type Lang = diagfmt.Lang

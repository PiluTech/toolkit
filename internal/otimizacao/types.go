// Package otimizacao lê, SÓ PARA LEITURA, os gargalos de desempenho do Windows (CPU, memória, disco do sistema,
// inicialização, serviços automáticos e tempo de boot), transforma a leitura em achados com "o que fazer" em texto e monta o
// resumo e o relatório completo. Nada aqui muda o sistema, pede administrador ou executa o "o que fazer": o coletor só usa
// o exec.Runner de leitura (exec.ReadOnlyRunner recusa mutação e Elevate) com scripts de PowerShell de texto fixo.
// Fora do Windows o coletor não executa nada e devolve Report.Supported=false ("só existe no Windows").
// A parte B do plano (reduzir animações, com plano, backup e desfazer) NÃO está aqui: virá em outro PR, em arquivos
// separados da leitura.
package otimizacao

import (
	"time"

	"github.com/PiluTech/toolkit/internal/diagfmt"
	"github.com/PiluTech/toolkit/internal/diagnostico"
)

// Lang é o idioma da saída (o mesmo do diagfmt).
type Lang = diagfmt.Lang

// Tipos compartilhados com o diagnóstico: o mesmo achado, a mesma gravidade e o mesmo "não lido".
type (
	// Finding é uma linha do resumo "o que está ruim e o que fazer".
	Finding = diagnostico.Finding
	// Severity é a gravidade de um achado.
	Severity = diagnostico.Severity
	// Skipped é uma área que não pôde ser lida, com o motivo (nunca silêncio).
	Skipped = diagnostico.Skipped
)

// Gravidades (as do diagnóstico).
const (
	SevOK   = diagnostico.SevOK
	SevInfo = diagnostico.SevInfo
	SevWarn = diagnostico.SevWarn
	SevBad  = diagnostico.SevBad
)

// Report é tudo o que a coleta leu. Ponteiro nil = sem dado (a área aparece em Skipped quando falhou).
type Report struct {
	OS        string    `json:"os"`
	Generated time.Time `json:"generated"`
	// Supported é false fora do Windows: nada foi lido.
	Supported bool `json:"supported"`
	// Admin: o terminal é de administrador. As leituras continuam sendo de leitura; só o log de boot pode exigir.
	Admin    bool      `json:"admin"`
	CPU      *CPU      `json:"cpu,omitempty"`
	Memory   *Memory   `json:"memory,omitempty"`
	Disk     *Disk     `json:"disk,omitempty"`
	Startup  *Startup  `json:"startup,omitempty"`
	Services *Services `json:"services,omitempty"`
	Boot     *Boot     `json:"boot,omitempty"`
	Skipped  []Skipped `json:"skipped,omitempty"`
}

// Process é um processo da lista dos que mais usam CPU ou memória (só nome, PID e números).
type Process struct {
	Name     string  `json:"name"`
	PID      int     `json:"pid"`
	CPUPct   float64 `json:"cpuPct,omitempty"` // % da máquina inteira (já dividido pelos processadores lógicos)
	MemBytes uint64  `json:"memBytes,omitempty"`
}

// CPU é o uso do processador numa amostra curta.
type CPU struct {
	Samples int       `json:"samples"`
	AvgPct  float64   `json:"avgPct"`
	PeakPct float64   `json:"peakPct"`
	Logical int       `json:"logical,omitempty"`
	Top     []Process `json:"top,omitempty"`
}

// Memory é a RAM, o commit e o arquivo de paginação.
type Memory struct {
	TotalBytes     uint64 `json:"totalBytes"`
	AvailableBytes uint64 `json:"availableBytes"` // inclui o cache reutilizável ("Disponível" do Gerenciador de Tarefas)
	CommitBytes    uint64 `json:"commitBytes,omitempty"`
	CommitLimit    uint64 `json:"commitLimitBytes,omitempty"`
	PageAllocBytes uint64 `json:"pageAllocBytes,omitempty"`
	PageUsedBytes  uint64 `json:"pageUsedBytes,omitempty"`
	// Top são os processos com mais memória (conjunto de trabalho).
	Top []Process `json:"top,omitempty"`
}

// Disk é o disco do sistema: tipo, espaço livre e, quando a amostra trouxe, fila e latência.
type Disk struct {
	Drive     string   `json:"drive"`
	Name      string   `json:"name,omitempty"`
	Medium    string   `json:"medium,omitempty"` // "SSD", "HDD" ou "" (Unspecified = desconhecido, nunca HDD)
	Bus       string   `json:"bus,omitempty"`
	SizeBytes uint64   `json:"sizeBytes,omitempty"`
	FreeBytes *uint64  `json:"freeBytes,omitempty"`
	QueueAvg  *float64 `json:"queueAvg,omitempty"`
	QueuePeak *float64 `json:"queuePeak,omitempty"`
	LatencyMs *float64 `json:"latencyMs,omitempty"` // média por operação, em milissegundos
}

// StartupItem é um item registrado para iniciar com o Windows.
type StartupItem struct {
	Name    string `json:"name"`
	Command string `json:"command,omitempty"` // só o nome do programa, sem caminho nem argumentos
	Where   string `json:"where,omitempty"`
}

// Startup são os itens registrados para iniciar (a fonte não informa o peso de cada um nem se o usuário já o desligou).
type Startup struct {
	Total int           `json:"total"`
	Items []StartupItem `json:"items,omitempty"` // lista curta, em ordem alfabética
}

// Service é um serviço automático.
type Service struct {
	Name     string `json:"name"`
	Display  string `json:"display,omitempty"`
	State    string `json:"state,omitempty"`
	ExitCode int    `json:"exitCode,omitempty"`
	Program  string `json:"program,omitempty"` // só o nome do executável
}

// Services são os serviços de início automático. O toolkit NUNCA decide desativar nenhum.
type Services struct {
	AutoCount int `json:"autoCount"`
	// OutsideWindows são os que rodam de fora da pasta do Windows (a maioria é de terceiros: é uma heurística).
	OutsideCount int       `json:"outsideWindowsCount"`
	Outside      []Service `json:"outsideWindows,omitempty"`
	// Failed são os automáticos parados com código de saída de erro (os parados por gatilho ou início atrasado não entram).
	Failed []Service `json:"failed,omitempty"`
}

// Boot é o último boot registrado no log Diagnostics-Performance (evento 100). Found=false = SEM DADO: nunca "boot rápido".
type Boot struct {
	Found      bool      `json:"found"`
	When       time.Time `json:"when,omitempty"`
	DurationMs int       `json:"durationMs,omitempty"` // 0 = o evento não trouxe a duração
	Degraded   *bool     `json:"degraded,omitempty"`   // a marca de degradação do próprio Windows; nil = não informada
}

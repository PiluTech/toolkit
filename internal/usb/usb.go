// Package usb é o modo empresa do Windows: permitir só os USB conhecidos (PR14 do plano). FUNÇÃO EXPERIMENTAL: não foi
// executada numa máquina real; tudo o que depende do comportamento do Windows está marcado "A VERIFICAR em VM" em
// docs/usb.md, e NADA se aplica por padrão (só com `toolkit usb aplicar` ou a ação da TUI, depois do plano e da
// confirmação).
//
// O mecanismo é a política oficial "Restrições de instalação de dispositivos"
// (HKLM\SOFTWARE\Policies\Microsoft\Windows\DeviceInstall\Restrictions) em ordem de camadas (AllowDenyLayered), com a
// negação LIMITADA às duas classes de configuração de USB e a liberação por ID de hardware (VID/PID) dos USB conectados
// hoje. Nunca se bloqueia por classe de teclado, mouse, HID ou disco, nunca se usa DenyUnspecified (global) e nada é
// escrito fora da lista fechada de valores de closedTargets.
//
// LIMITE HONESTO: a política só controla a instalação ou atualização de driver de dispositivo novo (o que já está
// instalado continua) e um dispositivo que CLONA o VID/PID de um permitido passa. Isto reduz o risco, não o elimina.
//
// Segurança do desenho: aplicar só acontece depois de PROVAR, pela árvore PnP, que todo dispositivo essencial (e todos os
// seus pais) continua permitido; com o backup protegido gravado e a tarefa de reversão armada e conferida ANTES da
// primeira chave; e a reversão restaura exatamente o snapshot, sem apagar a chave Restrictions às cegas.
//
// Todo acesso ao Windows passa pela interface System; os testes usam uma implementação em memória.
package usb

import (
	"context"
	"errors"
	"time"

	"github.com/PiluTech/toolkit/internal/privacidade"
)

// RegValue é um valor do registro (tipo preservado): o mesmo da privacidade.
type RegValue = privacidade.RegValue

// DWORD cria um valor numérico.
func DWORD(n uint32) RegValue { return privacidade.DWORD(n) }

// String cria um valor REG_SZ.
func String(s string) RegValue { return RegValue{Kind: privacidade.RegString, Str: s} }

// Locais e nomes FIXOS (nunca vêm de variável de ambiente).
const (
	// RestrictionsKey é a ÚNICA chave que a função lê e escreve (sensível a maiúsculas: o prefixo é conferido exato).
	RestrictionsKey = `HKLM\SOFTWARE\Policies\Microsoft\Windows\DeviceInstall\Restrictions`
	// ProtectedDir guarda o backup e a cópia do binário da reversão: ACL só para SYSTEM e Administradores.
	ProtectedDir = `C:\ProgramData\toolkit`
	// BackupFile e RegFile ficam dentro de ProtectedDir.
	BackupFile = "usb-backup.json"
	RegFile    = "usb-restrictions-backup.reg"
	// RevertBinary é a cópia do toolkit que a tarefa de reversão executa (dentro de ProtectedDir).
	RevertBinary = "toolkit-usb-revert.exe"
	// TaskPath e TaskName identificam a ÚNICA tarefa agendada que a função cria, confere e remove.
	TaskPath = `\PiluTech\`
	TaskName = "toolkit-usb-reverter"
	// RevertArgs é o argumento EXATO da tarefa (subcomando oculto que restaura o snapshot).
	RevertArgs = "usb reverter"

	// PrazoPadrao, PrazoMinimo e PrazoMaximo são o prazo (minutos) da reversão automática (decisão 7 do plano).
	PrazoPadrao = 10
	PrazoMinimo = 2
	PrazoMaximo = 60

	// MaxAllowIDs limita a lista de IDs permitidos (e o tamanho do snapshot).
	MaxAllowIDs = 200
)

// Classes de configuração de USB que a política nega (Microsoft, cenário de USB): "USB" e "USBDevice". Lista FECHADA.
// A confirmação de que bastam para impedir o filho HID de um dispositivo novo é A VERIFICAR em VM.
var denyClassGUIDs = []string{"{36fc9e60-c465-11cf-8056-444553540000}", "{88BAE032-5A81-49f0-BC3D-A4FF138216D6}"}

// genericAllowIDs é o conjunto FECHADO de IDs genéricos que entram na lista (a fonte [2] do plano): controladores e hubs
// raiz USB. Todo outro ID da lista tem VID e PID. ID compatível genérico de HID/classe NUNCA entra.
var genericAllowIDs = []string{`PCI\CC_0C03`, `PCI\CC_0C0330`, `PNP0CA1`, `USB\ROOT_HUB20`, `USB\ROOT_HUB30`, `USB\USB20_HUB`}

// Erros do pacote.
var (
	// ErrNeedsAdmin: aplicar, confirmar e desfazer escrevem em HKLM e na tarefa do sistema. Nada foi alterado.
	ErrNeedsAdmin = errors.New("o modo empresa de USB exige administrador")
	// ErrEdition: edição do Windows sem suporte documentado (Home e as demais não listadas).
	ErrEdition = errors.New("esta edição do Windows não documenta a política de restrição de instalação de dispositivos")
	// ErrNoProof: não foi possível PROVAR que os dispositivos essenciais continuam permitidos. Nada foi alterado.
	ErrNoProof = errors.New("não foi possível provar que os dispositivos essenciais continuam permitidos")
	// ErrPrazo: prazo fora de PrazoMinimo..PrazoMaximo.
	ErrPrazo = errors.New("prazo da reversão fora do intervalo permitido")
	// ErrExistingPolicy: já há política de restrição (de outra origem ou do toolkit) que o toolkit não mistura.
	ErrExistingPolicy = errors.New("já existe política de restrição de instalação de dispositivos")
	// ErrBackupInvalid: o backup existe mas está corrompido, adulterado ou fora da estrutura esperada.
	ErrBackupInvalid = errors.New("backup de USB corrompido, adulterado ou inválido")
	// ErrOriginalUnknown: houve aplicação e o snapshot sumiu ou é inválido: nada é restaurado às cegas.
	ErrOriginalUnknown = errors.New("ESTADO ORIGINAL DESCONHECIDO")
	// ErrConflict: um valor que o toolkit gravou agora tem outro conteúdo (outra origem mexeu): nada é restaurado.
	ErrConflict = errors.New("um valor gravado pelo toolkit foi alterado por outra origem")
	// ErrNothingToConfirm: não há tarefa de reversão do toolkit para cancelar.
	ErrNothingToConfirm = errors.New("não há reversão pendente para confirmar")
	// ErrTaskUnexpected: a tarefa de reversão não é exatamente a que o toolkit criaria.
	ErrTaskUnexpected = errors.New("a tarefa de reversão não é a esperada")
	// ErrPlanChanged: o plano recalculado agora não é o que foi mostrado e confirmado. Nada foi alterado.
	ErrPlanChanged = errors.New("o estado mudou desde o plano mostrado")
	// ErrEntradaInvalida: caminho de valor fora da lista fechada, nome ou valor fora do formato seguro.
	ErrEntradaInvalida = errors.New("entrada inválida")
	// ErrTreeIncomplete: a árvore PnP lida está incompleta ou inconsistente.
	ErrTreeIncomplete = errors.New("árvore de dispositivos incompleta")
)

// Edições aceitas (a política é documentada só para elas; Home e o resto = ErrEdition).
const (
	EditionPro        = "pro"
	EditionEnterprise = "enterprise"
	EditionEducation  = "education"
	EditionHome       = "home"
	EditionOther      = "other"
)

// RegTree é o que há hoje sob a chave Restrictions (valores por caminho RELATIVO, ex. "AllowDenyLayered" ou
// "AllowDeviceIDs\1"), mais as subchaves. Só REG_DWORD, REG_SZ e REG_EXPAND_SZ: outro tipo faz a leitura falhar (um valor
// que não dá para restaurar exatamente nunca é sobrescrito).
type RegTree struct {
	KeyExists bool
	Entries   map[string]RegValue
	Subkeys   []string // caminhos relativos das subchaves
}

// StagedBinary é a cópia do toolkit feita na pasta protegida para a tarefa de reversão.
type StagedBinary struct {
	Path               string
	SourceSHA, CopySHA string // SHA-256 (hex) do binário em execução e da cópia: têm de ser iguais
}

// TaskSpec é o que a tarefa de reversão deve fazer: executar Exe com Args, como SYSTEM, uma vez, daqui a Minutes.
type TaskSpec struct {
	Exe, Args string
	Minutes   int
}

// TaskInfo é a tarefa de reversão como o Agendador a mostra.
type TaskInfo struct {
	Exists  bool
	Enabled bool
	Exe     string
	Args    string
	User    string    // "SYSTEM" ou o SID S-1-5-18
	Next    time.Time // próximo disparo (UTC); zero = sem disparo
}

// System é o Windows visto pelo toolkit. Leituras (Edition, IsAdmin, Devices, ReadTree, ReadTask) nunca exigem
// administrador e são ReadOnly; escritas nunca levam Elevate (a função recusa ANTES, por IsAdmin, e nunca eleva sozinha).
// Toda escrita no registro só aceita caminhos da lista fechada (checkRel).
type System interface {
	Edition(ctx context.Context) (string, error)
	IsAdmin(ctx context.Context) bool
	// Devices lê o PnP (dispositivos presentes, com pai, classe e IDs). Leitura incompleta é erro.
	Devices(ctx context.Context) ([]Device, error)

	// ReadTree lê tudo o que há sob Restrictions (KeyExists=false quando a ausência está provada).
	ReadTree(ctx context.Context) (RegTree, error)
	// RegWrite e RegDelete mexem num VALOR (caminho relativo da lista fechada). Apagar o que não existe não é erro.
	RegWrite(ctx context.Context, rel string, v RegValue) error
	RegDelete(ctx context.Context, rel string) error
	// KeyDelete apaga uma subchave da lista fechada (ou a própria Restrictions, rel "") SÓ quando ela está vazia.
	KeyDelete(ctx context.Context, rel string) error
	// ExportReg exporta a chave Restrictions para file (reg export), dentro da pasta protegida.
	ExportReg(ctx context.Context, file string) error

	// ProtectDir cria dir (se faltar), recusa ponto de reparse, define dono e ACL (SYSTEM e Administradores, sem herança),
	// RELÊ a ACL e devolve erro se ela não for exatamente essa.
	ProtectDir(ctx context.Context, dir string) error
	// StageBinary copia o toolkit em execução para dir e confere o SHA-256 da cópia contra o do original.
	StageBinary(ctx context.Context, dir string) (StagedBinary, error)

	ArmTask(ctx context.Context, spec TaskSpec) error
	ReadTask(ctx context.Context) (TaskInfo, error)
	DeleteTask(ctx context.Context) error
}

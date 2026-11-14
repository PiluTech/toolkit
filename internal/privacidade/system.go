// Package privacidade reduz a coleta de dados do Windows (telemetria, ID de anúncio, histórico de atividades,
// sugestões e anúncios do Iniciar) em dois perfis, MOSTRANDO antes o que muda, guardando o valor original de cada
// chave, serviço e tarefa agendada, e desfazendo tudo com `toolkit privacidade desfazer`. Nada roda sem confirmação,
// administrador só é exigido na hora de aplicar o que mexe em HKLM, serviços ou tarefas, e nunca se edita o arquivo hosts
// nem se remove componente do sistema.
//
// Todo acesso ao Windows passa pela interface System; os testes usam uma implementação em memória e nenhum teste toca o
// registro, os serviços ou as tarefas reais.
package privacidade

import "context"

// RegKind é o tipo de um valor do registro (só os que as configurações usam).
type RegKind int

// Tipos de valor do registro.
const (
	RegDWORD RegKind = iota
	RegString
	// RegExpandString é o REG_EXPAND_SZ: o texto é guardado SEM expandir as %variáveis% e volta com o mesmo tipo.
	RegExpandString
)

// RegValue é um valor do registro.
type RegValue struct {
	Kind  RegKind
	DWORD uint32
	Str   string
}

// DWORD cria um valor numérico.
func DWORD(n uint32) RegValue { return RegValue{Kind: RegDWORD, DWORD: n} }

// ServiceState é o estado de um serviço do Windows.
type ServiceState struct {
	Exists bool
	// StartType normalizado: "automatic", "delayed-auto", "manual" ou "disabled".
	StartType string
	Running   bool
}

// TaskState é o estado de uma tarefa agendada.
type TaskState struct {
	Exists  bool
	Enabled bool
}

// Edição do Windows (normalizada).
const (
	EditionHome       = "home"
	EditionPro        = "pro"
	EditionEnterprise = "enterprise"
	EditionEducation  = "education"
	EditionOther      = "other"
)

// System é o Windows visto pelo toolkit. As chaves vêm no formato "HKCU\\Software\\..." ou "HKLM\\SOFTWARE\\...".
// As leituras nunca exigem administrador; as escritas em HKLM, serviços e tarefas exigem (a implementação real as
// envia com Elevate e o Runner recusa quando o processo não está elevado: ErrNotElevated).
type System interface {
	// Edition devolve a edição (EditionHome, EditionPro...).
	Edition(ctx context.Context) (string, error)
	// IsAdmin diz se o processo está elevado.
	IsAdmin(ctx context.Context) bool

	// RegRead lê um valor; exists=false quando a chave ou o valor não existem (não é erro).
	RegRead(ctx context.Context, key, name string) (v RegValue, exists bool, err error)
	RegWrite(ctx context.Context, key, name string, v RegValue) error
	// RegDelete apaga o valor (não a chave); apagar o que não existe não é erro.
	RegDelete(ctx context.Context, key, name string) error

	// ServiceRead lê o estado de um serviço; Exists=false quando ele não existe (não é erro).
	ServiceRead(ctx context.Context, name string) (ServiceState, error)
	// ServiceConfigure muda o tipo de início ("automatic", "delayed-auto", "manual" ou "disabled").
	ServiceConfigure(ctx context.Context, name, startType string) error
	ServiceStop(ctx context.Context, name string) error
	ServiceStart(ctx context.Context, name string) error

	// TaskRead lê uma tarefa pelo caminho completo ("\\Microsoft\\Windows\\...\\Nome"); Exists=false quando não existe.
	TaskRead(ctx context.Context, path string) (TaskState, error)
	TaskSetEnabled(ctx context.Context, path string, enabled bool) error
}

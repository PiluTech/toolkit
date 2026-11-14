package privacidade

import (
	"errors"
	"fmt"
)

// ErrBackupInvalido indica que o backup tem uma entrada que o catálogo do toolkit não produziria. Nada foi alterado e o arquivo
// foi preservado (pode ter sido adulterado ou ser de outra versão).
var ErrBackupInvalido = errors.New("backup de privacidade com entrada fora do catálogo")

// validRegKind lista os tipos de valor que o catálogo e o backup usam.
func validRegKind(k RegKind) bool { return k == RegDWORD || k == RegString || k == RegExpandString }

func validStartType(s string) bool {
	switch s {
	case "automatic", "delayed-auto", "manual", "disabled":
		return true
	}
	return false
}

// catalogIndex indexa TODOS os alvos do catálogo (não só os de um perfil) pelo ID do backup.
type catalogIndex struct {
	regs  map[string]RegSpec
	svcs  map[string]ServiceSpec
	tasks map[string]TaskSpec
}

func newCatalogIndex() catalogIndex {
	ix := catalogIndex{regs: map[string]RegSpec{}, svcs: map[string]ServiceSpec{}, tasks: map[string]TaskSpec{}}
	for _, s := range Settings() {
		for _, r := range s.Regs {
			ix.regs[regID(r.Key, r.Name)] = r
		}
		for _, sv := range s.Svcs {
			ix.svcs["svc:"+sv.Name] = sv
		}
		for _, tk := range s.Tasks {
			ix.tasks["task:"+tk.Path] = tk
		}
	}
	// Já esteve no catálogo (quebra a sincronização Intune/MDM) e saiu: backups de versões anteriores ainda podem
	// restaurá-lo ao estado original.
	for _, name := range legacyServices {
		ix.svcs["svc:"+name] = ServiceSpec{Name: name}
	}
	return ix
}

var legacyServices = []string{"dmwappushservice"}

// validateEntry confere UMA entrada do backup contra o catálogo: a chave do mapa, o ID da entrada e o alvo (chave e nome,
// serviço ou caminho da tarefa) têm de coincidir EXATAMENTE com um item do catálogo; tipo de valor e tipo de início têm
// de ser conhecidos. Kind desconhecido é recusado. Roda ANTES de qualquer leitura do sistema ou escrita.
func (ix catalogIndex) validateEntry(id string, e Entry) error {
	bad := func(format string, a ...any) error {
		return fmt.Errorf("%w: %q: %s", ErrBackupInvalido, printableID(id), fmt.Sprintf(format, a...))
	}
	if e.ID != id {
		return bad("o ID da entrada não confere com a chave")
	}
	switch e.Kind {
	case "reg":
		spec, ok := ix.regs[id]
		if !ok || spec.Key != e.Key || spec.Name != e.Name {
			return bad("valor de registro que o toolkit não altera")
		}
		if e.Existed && !validRegKind(e.RegKind) {
			return bad("tipo de valor desconhecido (%d)", int(e.RegKind))
		}
	case "service":
		sv, ok := ix.svcs[id]
		if !ok || sv.Name != e.Service {
			return bad("serviço que o toolkit não altera")
		}
		if !validStartType(e.StartType) {
			return bad("tipo de início desconhecido")
		}
	case "task":
		tk, ok := ix.tasks[id]
		if !ok || tk.Path != e.Task {
			return bad("tarefa agendada que o toolkit não altera")
		}
	default:
		return bad("tipo de entrada desconhecido")
	}
	return nil
}

// printableID limita e limpa o ID para a mensagem de erro (vem de um arquivo que pode ter sido adulterado).
func printableID(s string) string {
	r := []rune(s)
	if len(r) > 80 {
		r = append(r[:80], '…')
	}
	for i, c := range r {
		if c < 0x20 || c == 0x7f {
			r[i] = '?'
		}
	}
	return string(r)
}

// Package state guarda o andamento das receitas em disco (state.json), com escrita atômica, lock e
// versão de schema, para que a retomada depois de um reboot saiba de onde continuar.
package state

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// SchemaVersion é a versão do formato do state.json. Arquivo de versão MAIOR é recusado (um binário
// antigo não reescreve o que não entende).
const SchemaVersion = 1

// MaxFileSize limita a leitura do state.json.
const MaxFileSize = 4 << 20

// Erros do Store.
var (
	// ErrCorrupt: o arquivo existe mas não é um state válido. Nunca é descartado em silêncio.
	ErrCorrupt = errors.New("state.json corrompido")
	// ErrNewerSchema: o arquivo foi gravado por uma versão mais nova do toolkit.
	ErrNewerSchema = errors.New("state.json de uma versão mais nova do toolkit")
	// ErrLocked: outro processo do toolkit está gravando o state.
	ErrLocked = errors.New("state.json em uso por outro processo do toolkit")
	// ErrUnsafePath: o caminho do state é link simbólico ou não é arquivo regular.
	ErrUnsafePath = errors.New("caminho do state inseguro (link simbólico ou não é arquivo regular)")
)

// StepStatus é o estado de uma etapa.
type StepStatus string

// Estados de etapa.
const (
	StepPending        StepStatus = "pending"
	StepRunning        StepStatus = "running"
	StepDone           StepStatus = "done"
	StepSkipped        StepStatus = "skipped"
	StepFailed         StepStatus = "failed"
	StepAwaitingReboot StepStatus = "awaiting-reboot"
)

// RecipeStatus é o estado de uma receita.
type RecipeStatus string

// Estados de receita.
const (
	RecipeRunning        RecipeStatus = "running"
	RecipeAwaitingReboot RecipeStatus = "awaiting-reboot"
	RecipeDone           RecipeStatus = "done"
	RecipeFailed         RecipeStatus = "failed"
)

// Options são as flags originais da execução, guardadas para a retomada repetir o mesmo comportamento.
type Options struct {
	Yes     bool `json:"yes,omitempty"`
	Verbose bool `json:"verbose,omitempty"`
}

// File é o conteúdo do state.json.
type File struct {
	SchemaVersion  int                     `json:"schema_version"`
	ToolkitVersion string                  `json:"toolkit_version"`
	Active         string                  `json:"active,omitempty"` // receita em andamento (no máximo uma)
	Recipes        map[string]*RecipeState `json:"recipes"`
}

// RecipeState é o andamento de uma receita.
type RecipeState struct {
	RecipeID   string       `json:"recipe_id"`
	RecipeHash string       `json:"recipe_hash"`
	RunID      string       `json:"run_id"`
	User       string       `json:"user,omitempty"` // quem iniciou (a retomada roda como esse usuário)
	Status     RecipeStatus `json:"status"`
	Options    Options      `json:"options"`
	Steps      []StepState  `json:"steps"`
	StartedAt  time.Time    `json:"started_at"`
	UpdatedAt  time.Time    `json:"updated_at"`
}

// StepState é o andamento de uma etapa.
type StepState struct {
	ID             string     `json:"id"`
	Status         StepStatus `json:"status"`
	Attempts       int        `json:"attempts"`
	ResumeAttempts int        `json:"resume_attempts"`   // proteção contra loop de reboot
	BootID         string     `json:"boot_id,omitempty"` // boot em que o reboot foi pedido
	ExitCode       int        `json:"exit_code,omitempty"`
	Error          string     `json:"error,omitempty"`
	StartedAt      *time.Time `json:"started_at,omitempty"`
	FinishedAt     *time.Time `json:"finished_at,omitempty"`
}

// Store lê e grava o state.
type Store interface {
	Load(ctx context.Context) (*File, error) // arquivo inexistente = File vazio, não erro
	Save(ctx context.Context, f *File) error // atômico: tmp + fsync + rename, sob lock
	// Update faz Load + fn + Save sob o mesmo lock (leitura-modificação-escrita sem corrida).
	Update(ctx context.Context, fn func(*File) error) error
	Path() string
}

// NewFile devolve um File vazio na versão atual.
func NewFile(toolkitVersion string) *File {
	return &File{SchemaVersion: SchemaVersion, ToolkitVersion: toolkitVersion, Recipes: map[string]*RecipeState{}}
}

// NewRecipeState cria o andamento de uma receita nova, com todas as etapas pendentes.
func NewRecipeState(recipeID, hash, runID, user string, stepIDs []string, opts Options, now time.Time) *RecipeState {
	rs := &RecipeState{
		RecipeID: recipeID, RecipeHash: hash, RunID: runID, User: user,
		Status: RecipeRunning, Options: opts, StartedAt: now, UpdatedAt: now,
	}
	for _, id := range stepIDs {
		rs.Steps = append(rs.Steps, StepState{ID: id, Status: StepPending})
	}
	return rs
}

// Step devolve a etapa id (nil se não existe).
func (r *RecipeState) Step(id string) *StepState {
	for i := range r.Steps {
		if r.Steps[i].ID == id {
			return &r.Steps[i]
		}
	}
	return nil
}

// Validate confere as invariantes de um File lido do disco.
func (f *File) Validate() error {
	if f.Active != "" && f.Recipes[f.Active] == nil {
		return fmt.Errorf("%w: active %q não existe em recipes", ErrCorrupt, f.Active)
	}
	for key, r := range f.Recipes {
		if r == nil || r.RecipeID != key {
			return fmt.Errorf("%w: receita %q inconsistente", ErrCorrupt, key)
		}
		switch r.Status {
		case RecipeRunning, RecipeAwaitingReboot, RecipeDone, RecipeFailed:
		default:
			return fmt.Errorf("%w: receita %q com status %q", ErrCorrupt, key, r.Status)
		}
		seen := map[string]bool{}
		for _, s := range r.Steps {
			switch s.Status {
			case StepPending, StepRunning, StepDone, StepSkipped, StepFailed, StepAwaitingReboot:
			default:
				return fmt.Errorf("%w: etapa %q de %q com status %q", ErrCorrupt, s.ID, key, s.Status)
			}
			if s.ID == "" || seen[s.ID] {
				return fmt.Errorf("%w: etapa %q repetida ou sem id em %q", ErrCorrupt, s.ID, key)
			}
			seen[s.ID] = true
		}
	}
	return nil
}

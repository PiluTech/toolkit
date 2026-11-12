// Package resumetest traz um Registrar falso para os testes da engine.
package resumetest

import (
	"context"
	"sync"

	"github.com/PiluVitu/toolkit/internal/resume"
)

// Fake registra as chamadas e guarda se há retomada registrada.
type Fake struct {
	mu            sync.Mutex
	Registered    bool
	Specs         []resume.Spec
	Unregisters   int
	RegisterErr   error
	UnregisterErr error
	// Log recebe "register" e "unregister" na ordem das chamadas (testes de ordem com o Rebooter).
	Log *[]string
}

var _ resume.Registrar = (*Fake)(nil)

// Register guarda o Spec.
func (f *Fake) Register(_ context.Context, s resume.Spec) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Log != nil {
		*f.Log = append(*f.Log, "register")
	}
	if f.RegisterErr != nil {
		return f.RegisterErr
	}
	f.Registered = true
	f.Specs = append(f.Specs, s)
	return nil
}

// Unregister remove o registro.
func (f *Fake) Unregister(context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Log != nil {
		*f.Log = append(*f.Log, "unregister")
	}
	f.Unregisters++
	if f.UnregisterErr != nil {
		return f.UnregisterErr
	}
	f.Registered = false
	return nil
}

// IsRegistered diz se há registro.
func (f *Fake) IsRegistered(context.Context) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.Registered, nil
}

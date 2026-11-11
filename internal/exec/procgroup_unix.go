//go:build !windows

package exec

import (
	osexec "os/exec"
	"sync"
	"syscall"
	"time"
)

// killGrace é o intervalo entre o SIGTERM e o SIGKILL no grupo (menor que o WaitDelay do Run).
const killGrace = time.Second

// procGroup põe o filho num grupo de processos próprio e, ao cancelar, encerra o grupo inteiro
// (SIGTERM, depois SIGKILL), alcançando netos que o filho direto tenha criado.
type procGroup struct {
	mu    sync.Mutex
	timer *time.Timer
}

func (g *procGroup) attach(cmd *osexec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		pid := cmd.Process.Pid
		if err := syscall.Kill(-pid, syscall.SIGTERM); err != nil {
			return cmd.Process.Kill()
		}
		g.mu.Lock()
		g.timer = time.AfterFunc(killGrace, func() { _ = syscall.Kill(-pid, syscall.SIGKILL) })
		g.mu.Unlock()
		return nil
	}
}

func (*procGroup) started(*osexec.Cmd) {}

// close não para o timer de SIGKILL de propósito: se o filho morreu no SIGTERM mas um neto o ignorou,
// o SIGKILL ainda o alcança (num grupo vazio, o kill só devolve ESRCH).
func (*procGroup) close() {}

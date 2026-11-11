//go:build windows

package exec

import (
	osexec "os/exec"
	"sync"
	"syscall"
)

const processSetQuota = 0x0100

var (
	kernel32               = syscall.NewLazyDLL("kernel32.dll")
	procCreateJobObjectW   = kernel32.NewProc("CreateJobObjectW")
	procAssignProcessToJob = kernel32.NewProc("AssignProcessToJobObject")
	procTerminateJobObject = kernel32.NewProc("TerminateJobObject")
)

// procGroup põe o filho num Job Object e, ao cancelar, termina o job inteiro (filho e netos).
// Há uma janela mínima entre o início do processo e a atribuição ao job; se o job não puder ser
// criado, cai para encerrar só o filho.
type procGroup struct {
	mu  sync.Mutex
	job syscall.Handle
}

func (g *procGroup) attach(cmd *osexec.Cmd) {
	cmd.Cancel = func() error {
		g.mu.Lock()
		job := g.job
		g.mu.Unlock()
		if job != 0 {
			if r, _, _ := procTerminateJobObject.Call(uintptr(job), 1); r != 0 {
				return nil
			}
		}
		return cmd.Process.Kill()
	}
}

func (g *procGroup) started(cmd *osexec.Cmd) {
	job, _, _ := procCreateJobObjectW.Call(0, 0)
	if job == 0 {
		return
	}
	proc, err := syscall.OpenProcess(syscall.PROCESS_TERMINATE|processSetQuota, false, uint32(cmd.Process.Pid))
	if err != nil {
		_ = syscall.CloseHandle(syscall.Handle(job))
		return
	}
	defer func() { _ = syscall.CloseHandle(proc) }()
	if r, _, _ := procAssignProcessToJob.Call(job, uintptr(proc)); r == 0 {
		_ = syscall.CloseHandle(syscall.Handle(job))
		return
	}
	g.mu.Lock()
	g.job = syscall.Handle(job)
	g.mu.Unlock()
}

func (g *procGroup) close() {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.job != 0 {
		_ = syscall.CloseHandle(g.job)
		g.job = 0
	}
}

//go:build !windows

package state

import (
	"errors"
	"syscall"
)

// pidAlive diz se há um processo com esse pid (sinal 0 não envia nada). EPERM = existe, de outro dono.
func pidAlive(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

//go:build !windows

package state

import (
	"bytes"
	"errors"
	"os"
	"strconv"
	"strings"
	"syscall"
)

// pidAlive diz se há um processo com esse pid (sinal 0 não envia nada). EPERM = existe, de outro dono.
func pidAlive(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

// procStart devolve o instante de início do processo (campo 22 de /proc/<pid>/stat, em ticks desde o
// boot) ou "" onde isso não existe (macOS, BSD). Serve para distinguir o dono do lock de outro
// processo que ganhou o mesmo pid.
func procStart(pid int) string {
	b, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat") //nolint:gosec // pid numérico
	if err != nil {
		return ""
	}
	// o nome do processo (campo 2) vem entre parênteses e pode ter espaços: conta a partir do último ')'
	i := bytes.LastIndexByte(b, ')')
	if i < 0 {
		return ""
	}
	f := strings.Fields(string(b[i+1:]))
	if len(f) < 20 { // o campo 22 do arquivo é o 20º depois do ')'
		return ""
	}
	return f[19]
}

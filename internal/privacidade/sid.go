package privacidade

import (
	"context"
	"encoding/csv"
	"fmt"
	"strings"
)

// whoamiExe, por caminho absoluto (nunca pelo PATH).
const whoamiExe = `C:\Windows\System32\whoami.exe`

// UserSID lê o SID do usuário do processo: `whoami /user /fo csv /nh` devolve uma linha "DOMINIO\usuario","S-1-5-21-...".
// Só leitura. O SID é do token do processo (o mesmo usuário com ou sem elevação por UAC).
func (w *windowsSystem) UserSID(ctx context.Context) (string, error) {
	c, r, err := w.readCmd(ctx, whoamiExe, "/user", "/fo", "csv", "/nh")
	if err != nil {
		return "", fmt.Errorf("whoami: %w", err)
	}
	if r.ExitCode != 0 {
		return "", cmdFailed(c, r)
	}
	rec, err := csv.NewReader(strings.NewReader(strings.TrimSpace(string(r.Stdout)))).Read()
	if err != nil || len(rec) != 2 || !validSID(rec[1]) {
		return "", fmt.Errorf("whoami: saída inesperada, não deu para ler o SID do usuário")
	}
	return rec[1], nil
}

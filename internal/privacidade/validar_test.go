package privacidade

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/PiluTech/toolkit/internal/diagfmt"
)

// tampered monta backups adulterados que apontam para fora do catálogo.
func tampered() map[string]Entry {
	return map[string]Entry{
		"wuauserv": {ID: "svc:wuauserv", Kind: "service", Service: "wuauserv", StartType: "disabled", Running: false},
		"defender": {ID: `reg:HKLM\SOFTWARE\Policies\Microsoft\Windows Defender|DisableAntiSpyware`, Kind: "reg",
			Key: `HKLM\SOFTWARE\Policies\Microsoft\Windows Defender`, Name: "DisableAntiSpyware", Existed: true, RegKind: RegDWORD, DWORD: 1},
		"login": {ID: `reg:HKLM\SOFTWARE\Microsoft\Windows\CurrentVersion\Policies\System|EnableLUA`, Kind: "reg",
			Key: `HKLM\SOFTWARE\Microsoft\Windows\CurrentVersion\Policies\System`, Name: "EnableLUA", Existed: true, RegKind: RegDWORD, DWORD: 0},
		"tarefa-alheia":     {ID: `task:\Microsoft\Windows\Defrag\ScheduledDefrag`, Kind: "task", Task: `\Microsoft\Windows\Defrag\ScheduledDefrag`, Enabled: false},
		"kind-desconhecido": {ID: "x:1", Kind: "script", Key: "x"},
		"id-diferente":      {ID: "svc:DiagTrack", Kind: "service", Service: "wuauserv", StartType: "disabled"},
		"id-da-chave":       {ID: "reg:outro", Kind: "reg", Key: hkcuAdvKey, Name: "Enabled", Existed: true, RegKind: RegDWORD},
		"start-invalido":    {ID: "svc:DiagTrack", Kind: "service", Service: "DiagTrack", StartType: "boot"},
		"chave-case":        {ID: regID(hkcuAdvKey, "enabled"), Kind: "reg", Key: hkcuAdvKey, Name: "enabled", Existed: true, RegKind: RegDWORD},
	}
}

const hkcuAdvKey = `HKCU\Software\Microsoft\Windows\CurrentVersion\AdvertisingInfo`

func TestBuildUndoRecusaBackupAdulterado(t *testing.T) {
	for nome, e := range tampered() {
		t.Run(nome, func(t *testing.T) {
			f := newFake(EditionPro, true).withAllServicesAndTasks()
			f.svcs["wuauserv"] = &ServiceState{Exists: true, StartType: "automatic", Running: true}
			f.tasks[`\Microsoft\Windows\Defrag\ScheduledDefrag`] = &TaskState{Exists: true, Enabled: true}
			// um item VÁLIDO junto: a recusa é do backup inteiro
			valido := Entry{ID: regID(hkcuAdvKey, "Enabled"), Kind: "reg", Key: hkcuAdvKey, Name: "Enabled", Existed: true, RegKind: RegDWORD, DWORD: 1}
			st := &memStore{b: &Backup{Version: 1, Created: t0, Entries: map[string]Entry{valido.ID: valido, e.ID: e}}}
			plan, _, err := BuildUndo(context.Background(), f, st, diagfmt.PT)
			if err == nil || plan != nil {
				t.Fatalf("backup adulterado aceito: plan=%v err=%v", plan, err)
			}
			if len(f.writes()) != 0 {
				t.Errorf("mutações: %v", f.writes())
			}
			if st.b == nil || len(st.b.Entries) != 2 {
				t.Error("o backup não pode ser apagado nem mudado")
			}
		})
	}
}

func TestBuildUndoAdulteradoPreservaArquivoByteAByte(t *testing.T) {
	for nome, e := range tampered() {
		t.Run(nome, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "b.json")
			s := FileStore{File: path}
			if err := s.Save(&Backup{Version: 1, Created: t0, Entries: map[string]Entry{e.ID: e}}); err != nil {
				t.Fatal(err)
			}
			before, _ := os.ReadFile(path)
			f := newFake(EditionPro, true).withAllServicesAndTasks()
			f.svcs["wuauserv"] = &ServiceState{Exists: true, StartType: "automatic", Running: true}
			if _, _, err := BuildUndo(context.Background(), f, s, diagfmt.PT); err == nil {
				t.Fatal("aceitou")
			}
			after, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(before, after) {
				t.Errorf("arquivo mudou ou sumiu: %v", err)
			}
			if len(f.writes()) != 0 {
				t.Errorf("mutações: %v", f.writes())
			}
		})
	}
}

// O formato/catálogo antigo nunca foi publicado: o desfazer só conhece o catálogo ATUAL, sem exceções (dmwappushservice
// saiu do catálogo e um backup que o cite é recusado).
func TestBuildUndoSemExcecaoParaServicoQueSaiuDoCatalogo(t *testing.T) {
	ix := newCatalogIndex()
	if _, ok := ix.svcs["svc:dmwappushservice"]; ok {
		t.Fatal("dmwappushservice não deve estar no índice de validação")
	}
}

package usb

import (
	"context"
	"strings"
	"testing"
)

// Seguimento da revisão do Crivo no #91 (delta de68760).

// 1) A reversão não pode depender só do RestartCount (o Agendador pode não tratar saída != 0 como falha): o gatilho
// também repete a cada minuto por 30 min; o Undo apaga a tarefa ao concluir, então a repetição para sozinha.
func TestArmTaskGatilhoRepeteALemDoRestart(t *testing.T) {
	s := armTaskScript(10)
	var trig string
	for _, l := range strings.Split(s, "\n") {
		if strings.HasPrefix(l, "$t=New-ScheduledTaskTrigger") {
			trig = l
		}
	}
	for _, want := range []string{"-Once", "-RepetitionInterval (New-TimeSpan -Minutes 1)", "-RepetitionDuration (New-TimeSpan -Minutes 30)"} {
		if !strings.Contains(trig, want) {
			t.Errorf("o gatilho não tem %q: %q", want, trig)
		}
	}
	if !strings.Contains(s, "-RestartCount 3") {
		t.Error("o RestartCount continua (as duas proteções somam)")
	}
	if err := asciiOnly(s); err != nil {
		t.Error(err)
	}
}

// 2) Executável do Windows SEMPRE por caminho absoluto (nunca pelo PATH): o icacls roda com privilégio.
func TestIcaclsPorCaminhoAbsoluto(t *testing.T) {
	s := protectDirScript
	if strings.Contains(s, "& icacls") {
		t.Error("icacls resolvido pelo PATH")
	}
	if !strings.Contains(s, `C:\Windows\System32\icacls.exe`) {
		t.Error("o caminho absoluto do icacls não aparece")
	}
	if strings.Count(s, `System32\icacls.exe`) < 2 {
		t.Error("tanto o /reset quanto o /setowner usam o caminho absoluto")
	}
}

// 3) A ACL dos FILHOS é conferida por SDDL: dono Administradores e só SYSTEM e Administradores com FullControl HERDADO.
func TestCheckChildSDDL(t *testing.T) {
	good := []string{
		`O:BAG:SYD:AI(A;ID;FA;;;SY)(A;ID;FA;;;BA)`,
		`O:S-1-5-32-544G:SYD:AI(A;OICIID;FA;;;BA)(A;OICIID;FA;;;SY)`,
	}
	for _, s := range good {
		if err := checkChildSDDL("x", s); err != nil {
			t.Errorf("%s recusado: %v", s, err)
		}
	}
	bad := map[string]string{
		"dono usuário":        `O:S-1-5-21-1-2-3-1001G:SYD:AI(A;ID;FA;;;SY)(A;ID;FA;;;BA)`,
		"ACE de Users":        `O:BAG:SYD:AI(A;ID;FA;;;SY)(A;ID;FA;;;BA)(A;ID;0x1200a9;;;BU)`,
		"ACE própria do dono": `O:BAG:SYD:AI(A;;FA;;;SY)(A;ID;FA;;;BA)`,
		"DACL protegida":      `O:BAG:SYD:PAI(A;;FA;;;SY)(A;;FA;;;BA)`,
		"só uma ACE":          `O:BAG:SYD:AI(A;ID;FA;;;SY)`,
		"sem DACL":            `O:BAG:SY`,
		"ACE de usuário FA":   `O:BAG:SYD:AI(A;ID;FA;;;SY)(A;ID;FA;;;S-1-5-21-1-2-3-1001)`,
	}
	for name, s := range bad {
		if err := checkChildSDDL("x.reg", s); err == nil {
			t.Errorf("%s aceito: %s", name, s)
		}
	}
}

func TestProtectDirConfereOsFilhos(t *testing.T) {
	run := func(children string) error {
		rn := &psOut{out: `{"reparse":false,"sddl":"` + goodSDDL + `","children":` + children + `}`}
		w := NewWindowsSystem(rn, nil).(*windowsSystem)
		return w.ProtectDir(context.Background(), ProtectedDir)
	}
	good := `[{"name":"usb-backup.json","reparse":false,"sddl":"O:BAG:SYD:AI(A;ID;FA;;;SY)(A;ID;FA;;;BA)"}]`
	if err := run(good); err != nil {
		t.Fatalf("filhos corretos recusados: %v", err)
	}
	if err := run(`[]`); err != nil {
		t.Fatalf("sem filhos: %v", err)
	}
	planted := `[{"name":"usb-restrictions-backup.reg","reparse":false,"sddl":"O:S-1-5-21-1-2-3-1001G:SYD:(A;;FA;;;S-1-5-21-1-2-3-1001)"}]`
	if err := run(planted); err == nil || !strings.Contains(err.Error(), "usb-restrictions-backup.reg") {
		t.Fatalf("arquivo plantado com ACL do usuário aceito: %v", err)
	}
	link := `[{"name":"x","reparse":true,"sddl":"O:BAG:SYD:AI(A;ID;FA;;;SY)(A;ID;FA;;;BA)"}]`
	if err := run(link); err == nil || !strings.Contains(err.Error(), "reparse") {
		t.Fatalf("filho que é link aceito: %v", err)
	}
	// a leitura traz os filhos (o script lista com -Force e lê o SDDL de cada um)
	if !strings.Contains(readDirScript, "Get-ChildItem -LiteralPath $d -Force") || !strings.Contains(readDirScript, "children=") {
		t.Error("o readDirScript não lê os filhos")
	}
}

// 4) c:\windows\temp e c:\windows\tasks são graváveis por usuário comum: só System32 vale como pasta de administrador.
func TestExeFolderWarningSoSystem32DoWindows(t *testing.T) {
	for _, ok := range []string{`C:\Windows\System32\t.exe`, `c:\windows\system32\drivers\t.exe`, `C:\Program Files\t\t.exe`} {
		if w := ExeFolderWarning(ok); w != "" {
			t.Errorf("%s não deveria avisar", ok)
		}
	}
	for _, bad := range []string{`C:\Windows\Temp\t.exe`, `c:\windows\tasks\t.exe`, `C:\Windows\t.exe`} {
		if w := ExeFolderWarning(bad); w == "" {
			t.Errorf("%s deveria avisar", bad)
		}
	}
}

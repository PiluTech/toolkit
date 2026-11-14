package usb

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

func sampleSnapshot() *Snapshot {
	return &Snapshot{
		Version: 1, Created: t0, KeyExisted: false, Prazo: 10, TaskExe: ProtectedDir + `\` + RevertBinary,
		Targets: []Target{
			{Path: "AllowDenyLayered", Existed: false, Want: DWORD(1)},
			{Path: `AllowDeviceIDs\1`, Existed: false, Want: String(`USB\VID_FFFF&PID_0001`)},
		},
	}
}

func TestBackupIdaEVoltaEArquivoAtomico(t *testing.T) {
	dir := t.TempDir()
	st := FileStore{Dir: dir}
	sn := sampleSnapshot()
	if err := st.Save(sn); err != nil {
		t.Fatal(err)
	}
	got, err := st.Load()
	if err != nil || got == nil {
		t.Fatalf("Load: %v %v", got, err)
	}
	if len(got.Targets) != 2 || got.Targets[1].Want.Str != `USB\VID_FFFF&PID_0001` || got.TaskExe != sn.TaskExe {
		t.Fatalf("snapshot = %#v", got)
	}
	ents, _ := os.ReadDir(dir)
	for _, e := range ents {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Errorf("sobrou temporário %s", e.Name())
		}
	}
	if runtime.GOOS != "windows" {
		if fi, _ := os.Stat(st.BackupPath()); fi.Mode().Perm() != 0o600 {
			t.Errorf("permissão = %v", fi.Mode().Perm())
		}
	}
}

func TestBackupAusenteNaoEErro(t *testing.T) {
	got, err := FileStore{Dir: t.TempDir()}.Load()
	if got != nil || err != nil {
		t.Fatalf("got=%v err=%v", got, err)
	}
}

func TestBackupNaoCriaAPastaSemACL(t *testing.T) {
	st := FileStore{Dir: filepath.Join(t.TempDir(), "naoexiste")}
	if err := st.Save(sampleSnapshot()); err == nil {
		t.Fatal("Save não pode criar a pasta: quem cria e protege é o ProtectDir")
	}
}

func TestBackupRecusaCorrompidoAdulteradoEInvalido(t *testing.T) {
	good := func(t *testing.T) (FileStore, string, []byte) {
		dir := t.TempDir()
		st := FileStore{Dir: dir}
		if err := st.Save(sampleSnapshot()); err != nil {
			t.Fatal(err)
		}
		raw, _ := os.ReadFile(st.BackupPath())
		return st, st.BackupPath(), raw
	}
	write := func(t *testing.T, p string, b []byte) {
		t.Helper()
		if err := os.WriteFile(p, b, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	cases := map[string]func(t *testing.T, p string, raw []byte){
		"truncado": func(t *testing.T, p string, raw []byte) { write(t, p, raw[:len(raw)/2]) },
		"lixo":     func(t *testing.T, p string, _ []byte) { write(t, p, []byte("nao e json")) },
		"vazio":    func(t *testing.T, p string, _ []byte) { write(t, p, nil) },
		"hash trocado": func(t *testing.T, p string, raw []byte) {
			write(t, p, []byte(strings.Replace(string(raw), `"sha256": "`, `"sha256": "00`, 1)))
		},
		"conteudo trocado": func(t *testing.T, p string, raw []byte) {
			write(t, p, []byte(strings.Replace(string(raw), `"prazo": 10`, `"prazo": 99`, 1)))
		},
		"campo desconhecido": func(t *testing.T, p string, raw []byte) {
			write(t, p, []byte(strings.Replace(string(raw), `"snapshot": {`, `"snapshot": {"extra": 1,`, 1)))
		},
		"arquivo enorme": func(t *testing.T, p string, _ []byte) { write(t, p, []byte(strings.Repeat("a", 5<<20))) },
	}
	for name, mut := range cases {
		t.Run(name, func(t *testing.T) {
			st, p, raw := good(t)
			mut(t, p, raw)
			got, err := st.Load()
			if got != nil || !errors.Is(err, ErrBackupInvalid) {
				t.Fatalf("got=%v err=%v, queria ErrBackupInvalid", got, err)
			}
		})
	}
}

func TestBackupComHashCertoMasEstruturaForaDaListaFechadaRecusa(t *testing.T) {
	bad := map[string]func(s *Snapshot){
		"caminho fora da lista":   func(s *Snapshot) { s.Targets[0].Path = "DenyUnspecified" },
		"caminho com ..":          func(s *Snapshot) { s.Targets[0].Path = `AllowDeviceIDs\..\x` },
		"versão desconhecida":     func(s *Snapshot) { s.Version = 2 },
		"prazo fora do limite":    func(s *Snapshot) { s.Prazo = 0 },
		"alvo repetido":           func(s *Snapshot) { s.Targets = append(s.Targets, s.Targets[0]) },
		"valor novo inválido":     func(s *Snapshot) { s.Targets[1].Want = String("a\x1b[2J") },
		"flag com valor errado":   func(s *Snapshot) { s.Targets[0].Want = DWORD(7) },
		"regfile estranho":        func(s *Snapshot) { s.RegFile = `..\..\Windows\x.reg` },
		"hash do reg inválido":    func(s *Snapshot) { s.RegSHA256 = "xyz" },
		"subchave desconhecida":   func(s *Snapshot) { s.SubkeysExisted = []string{"Run"} },
		"binário da tarefa vazio": func(s *Snapshot) { s.TaskExe = "" },
		"alvos demais":            func(s *Snapshot) { s.Targets = manyTargets(MaxAllowIDs + 20) },
	}
	for name, mut := range bad {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			sn := sampleSnapshot()
			mut(sn)
			raw, err := marshalEnvelope(sn) // hash CERTO: só a validação de estrutura pode barrar
			if err != nil {
				t.Fatal(err)
			}
			st := FileStore{Dir: dir}
			if err := os.WriteFile(st.BackupPath(), raw, 0o600); err != nil {
				t.Fatal(err)
			}
			if got, err := st.Load(); got != nil || !errors.Is(err, ErrBackupInvalid) {
				t.Fatalf("got=%v err=%v", got, err)
			}
			if err := st.Save(sn); err == nil {
				t.Fatal("Save também não pode gravar um snapshot inválido")
			}
		})
	}
}

func manyTargets(n int) []Target {
	var out []Target
	for i := 1; i <= n; i++ {
		out = append(out, Target{Path: `AllowDeviceIDs\` + strconv.Itoa(i), Want: String(`USB\VID_FFFF&PID_0001`)})
	}
	return out
}

func TestBackupRemoveApagaOJSONEORegSemErro(t *testing.T) {
	st := FileStore{Dir: t.TempDir()}
	if err := st.Remove(); err != nil {
		t.Fatalf("remover o que não existe não é erro: %v", err)
	}
	_ = st.Save(sampleSnapshot())
	_ = os.WriteFile(st.RegPath(), []byte("x"), 0o600)
	if err := st.Remove(); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{st.BackupPath(), st.RegPath()} {
		if _, err := os.Stat(p); err == nil {
			t.Errorf("%s ainda existe", p)
		}
	}
}

func TestBackupDoRegConfereHash(t *testing.T) {
	st := FileStore{Dir: t.TempDir()}
	_ = os.WriteFile(st.RegPath(), []byte("abc"), 0o600)
	h, err := st.FileSHA256(st.RegPath())
	if err != nil || h != "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad" {
		t.Fatalf("hash = %q %v", h, err)
	}
}

package usb

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"github.com/PiluTech/toolkit/internal/privacidade"
	"github.com/PiluTech/toolkit/internal/textsafe"
)

// maxBackupBytes limita o arquivo de backup lido (é dado não confiável).
const maxBackupBytes = 4 << 20

var (
	reHex64   = regexp.MustCompile(`^[0-9a-f]{64}$`)
	reExePath = regexp.MustCompile(`^[A-Za-z]:\\[A-Za-z0-9 _.()\\-]{1,240}$`)
)

// Target é um valor que o Apply escreve, com o ORIGINAL (Existed/Orig) e o valor novo (Want). Orig só vale com Existed.
type Target struct {
	Path    string   `json:"path"`
	Existed bool     `json:"existed"`
	Orig    RegValue `json:"orig"`
	Want    RegValue `json:"want"`
}

// Snapshot é o estado ORIGINAL guardado antes da primeira chave: de cada alvo da lista fechada (inclusive "não existia"),
// se a chave Restrictions e as subchaves já existiam, e o que a tarefa de reversão executa. O hash SHA-256 do envelope
// detecta CORRUPÇÃO; quem impede adulteração é a ACL da pasta protegida (um arquivo que o administrador reescreve com hash
// novo não seria detectado, por isso a restauração também só mexe na lista fechada e confere o estado atual).
type Snapshot struct {
	Version        int       `json:"version"`
	Created        time.Time `json:"created"`
	KeyExisted     bool      `json:"key_existed"`
	SubkeysExisted []string  `json:"subkeys_existed"`
	Targets        []Target  `json:"targets"`
	RegFile        string    `json:"reg_file"`
	RegSHA256      string    `json:"reg_sha256"`
	TaskExe        string    `json:"task_exe"`
	Prazo          int       `json:"prazo"`
}

type envelope struct {
	SHA256   string   `json:"sha256"`
	Snapshot Snapshot `json:"snapshot"`
}

func hashSnapshot(sn *Snapshot) (string, error) {
	b, err := json.Marshal(sn)
	if err != nil {
		return "", err
	}
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:]), nil
}

func marshalEnvelope(sn *Snapshot) ([]byte, error) {
	h, err := hashSnapshot(sn)
	if err != nil {
		return nil, err
	}
	return json.MarshalIndent(envelope{SHA256: h, Snapshot: *sn}, "", "  ")
}

func invalidBackup(format string, a ...any) error {
	return fmt.Errorf("%w: %s", ErrBackupInvalid, fmt.Sprintf(format, a...))
}

func validKind(k privacidade.RegKind) bool {
	return k == privacidade.RegDWORD || k == privacidade.RegString || k == privacidade.RegExpandString
}

// validate confere a ESTRUTURA do snapshot contra a lista fechada (o arquivo é dado não confiável).
func (sn *Snapshot) validate() error {
	if sn.Version != 1 {
		return invalidBackup("versão %d desconhecida", sn.Version)
	}
	if sn.Prazo < PrazoMinimo || sn.Prazo > PrazoMaximo {
		return invalidBackup("prazo %d fora de %d..%d", sn.Prazo, PrazoMinimo, PrazoMaximo)
	}
	if !reExePath.MatchString(sn.TaskExe) {
		return invalidBackup("caminho da tarefa fora do formato")
	}
	if sn.RegFile != "" && sn.RegFile != RegFile {
		return invalidBackup("arquivo .reg fora do esperado")
	}
	if sn.RegSHA256 != "" && !reHex64.MatchString(sn.RegSHA256) {
		return invalidBackup("hash do .reg inválido")
	}
	for _, s := range sn.SubkeysExisted {
		if s != nameAllow && s != nameDeny {
			return invalidBackup("subchave %q fora da lista fechada", printableRel(s))
		}
	}
	if len(sn.Targets) == 0 || len(sn.Targets) > MaxAllowIDs+8 {
		return invalidBackup("%d alvos (esperado 1..%d)", len(sn.Targets), MaxAllowIDs+8)
	}
	seen := map[string]bool{}
	for _, t := range sn.Targets {
		if err := checkRel(t.Path); err != nil {
			return invalidBackup("alvo fora da lista fechada: %q", printableRel(t.Path))
		}
		if seen[t.Path] {
			return invalidBackup("alvo repetido: %q", t.Path)
		}
		seen[t.Path] = true
		if err := checkWrite(t.Path, t.Want); err != nil {
			return invalidBackup("valor novo de %q inválido", t.Path)
		}
		if t.Existed {
			if !validKind(t.Orig.Kind) || len(t.Orig.Str) > 4096 {
				return invalidBackup("valor original de %q inválido", t.Path)
			}
			if err := checkOrigText(t.Orig); err != nil {
				return invalidBackup("valor original de %q inválido", t.Path)
			}
		}
	}
	return nil
}

func checkOrigText(v RegValue) error {
	for _, r := range v.Str {
		if r < 0x20 || r == 0x7f {
			return ErrEntradaInvalida
		}
	}
	return nil
}

// Store guarda o snapshot (e o .reg) na pasta protegida. Load devolve nil, nil quando não existe; arquivo corrompido,
// adulterado ou fora da estrutura é ErrBackupInvalid. Save NÃO cria a pasta: quem a cria e protege é System.ProtectDir.
type Store interface {
	Folder() string
	Load() (*Snapshot, error)
	Save(*Snapshot) error
	Remove() error
	RegPath() string
	FileSHA256(path string) (string, error)
}

// RevertLogger é o Store que guarda o rastro da reversão automática (opcional: FileStore o implementa).
type RevertLogger interface {
	// SaveRevertResult grava o texto do resultado (vazio apaga o rastro).
	SaveRevertResult(note string) error
	// LoadRevertResult devolve o texto gravado, já limpo (vazio = nenhum).
	LoadRevertResult() string
}

// RevertResultFile é o arquivo do rastro, dentro da pasta protegida.
const RevertResultFile = "usb-revert-result.txt"

// SaveRevertResult grava o rastro (até 600 bytes, 0600); vazio apaga.
func (s FileStore) SaveRevertResult(note string) error {
	path := filepath.Join(s.Dir, RevertResultFile)
	if note == "" {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}
	if len(note) > 600 {
		note = note[:600]
	}
	return os.WriteFile(path, []byte(note), 0o600)
}

// LoadRevertResult lê o rastro (limite de tamanho; texto de fora passa por textsafe).
func (s FileStore) LoadRevertResult() string {
	f, err := os.Open(filepath.Join(s.Dir, RevertResultFile))
	if err != nil {
		return ""
	}
	defer func() { _ = f.Close() }()
	b, _ := io.ReadAll(io.LimitReader(f, 700))
	return textsafe.Line(string(b))
}

// FileStore é o Store em arquivo (usb-backup.json, escrita atômica, 0600).
type FileStore struct{ Dir string }

// Folder devolve a pasta.
func (s FileStore) Folder() string { return s.Dir }

// BackupPath é o arquivo do snapshot.
func (s FileStore) BackupPath() string { return filepath.Join(s.Dir, BackupFile) }

// RegPath é o arquivo .reg exportado.
func (s FileStore) RegPath() string { return filepath.Join(s.Dir, RegFile) }

// Load lê e valida o snapshot.
func (s FileStore) Load() (*Snapshot, error) {
	f, err := os.Open(s.BackupPath())
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	data, err := io.ReadAll(io.LimitReader(f, maxBackupBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxBackupBytes {
		return nil, invalidBackup("arquivo acima de %d bytes", maxBackupBytes)
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var env envelope
	if err := dec.Decode(&env); err != nil {
		return nil, invalidBackup("ilegível ou cortado")
	}
	if dec.More() {
		return nil, invalidBackup("conteúdo depois do fim do JSON")
	}
	want, err := hashSnapshot(&env.Snapshot)
	if err != nil || want != env.SHA256 {
		return nil, invalidBackup("o hash SHA-256 não confere (arquivo alterado ou corrompido)")
	}
	if err := env.Snapshot.validate(); err != nil {
		return nil, err
	}
	return &env.Snapshot, nil
}

// Save valida e grava de forma atômica (temporário na mesma pasta + rename).
func (s FileStore) Save(sn *Snapshot) error {
	if err := sn.validate(); err != nil {
		return err
	}
	if fi, err := os.Stat(s.Dir); err != nil || !fi.IsDir() {
		return fmt.Errorf("a pasta do backup %s não existe: ela é criada e protegida antes de gravar", s.Dir)
	}
	data, err := marshalEnvelope(sn)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(s.Dir, ".usb-backup-*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer func() { _ = os.Remove(name) }()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(name, s.BackupPath())
}

// Remove apaga o snapshot e o .reg (ausente não é erro).
func (s FileStore) Remove() error {
	var first error
	for _, p := range []string{s.BackupPath(), s.RegPath()} {
		if err := os.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) && first == nil {
			first = err
		}
	}
	return first
}

// FileSHA256 devolve o SHA-256 (hex) de um arquivo.
func (s FileStore) FileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	if _, err := io.Copy(h, io.LimitReader(f, 64<<20)); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

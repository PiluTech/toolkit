package usb

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/PiluTech/toolkit/internal/textsafe"
)

// Device é um dispositivo PnP PRESENTE (lido de Get-PnpDevice/Get-PnpDeviceProperty). Todo texto vem validado e limpo
// por ParseDevices: a saída do PnP é dado NÃO confiável.
type Device struct {
	InstanceID    string
	Parent        string // vazio só na raiz da árvore
	Name          string
	Class         string
	ClassGUID     string
	HardwareIDs   []string
	CompatibleIDs []string
}

// Limites da leitura (o JSON vem de um processo externo).
const (
	maxDevices   = 4000
	maxIDLen     = 260
	maxIDsPerDev = 64
	maxJSONBytes = 16 << 20
)

var (
	reClassName = regexp.MustCompile(`^[A-Za-z0-9_. -]{0,64}$`)
	reClassGUID = regexp.MustCompile(`^(\{[0-9A-Fa-f]{8}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{12}\})?$`)
	reVIDPID    = regexp.MustCompile(`(?i)VID_([0-9A-F]{4})&PID_([0-9A-F]{4})`)
)

// stringList aceita o que o ConvertTo-Json faz com uma propriedade de texto: null, um texto só (sem lista) ou uma lista.
type stringList []string

func (s *stringList) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	switch {
	case bytes.Equal(b, []byte("null")):
		*s = nil
		return nil
	case len(b) > 0 && b[0] == '"':
		var one string
		if err := json.Unmarshal(b, &one); err != nil {
			return err
		}
		*s = []string{one}
		return nil
	}
	var many []string
	if err := json.Unmarshal(b, &many); err != nil {
		return err
	}
	*s = many
	return nil
}

type rawDevice struct {
	InstanceID    string     `json:"InstanceId"`
	Parent        string     `json:"Parent"`
	Name          string     `json:"Name"`
	Class         string     `json:"Class"`
	ClassGUID     string     `json:"ClassGuid"`
	HardwareIDs   stringList `json:"HardwareIds"`
	CompatibleIDs stringList `json:"CompatibleIds"`
}

type rawEnvelope struct {
	Count   int         `json:"count"`
	Devices []rawDevice `json:"devices"`
}

// printableASCII aceita só ASCII imprimível (0x20..0x7E): é o que os IDs de dispositivo usam; ESC, controle, bidi e
// qualquer outro caractere fazem a leitura inteira ser recusada (falha fechada: sem prova, sem aplicar).
func printableASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < 0x20 || s[i] > 0x7e {
			return false
		}
	}
	return true
}

func checkID(what, s string, allowEmpty bool) error {
	if s == "" && allowEmpty {
		return nil
	}
	if s == "" || len(s) > maxIDLen || !printableASCII(s) {
		return fmt.Errorf("%w: %s fora do formato seguro", ErrTreeIncomplete, what)
	}
	return nil
}

// ParseDevices lê o JSON do script de leitura do PnP: {"count":N,"devices":[...]}. JSON cortado, contagem que não bate,
// lista vazia, ID com caractere de controle ou acima do limite = erro (a leitura é a base da PROVA: incompleta não serve).
func ParseDevices(data []byte) ([]Device, error) {
	data = bytes.TrimPrefix(bytes.TrimSpace(data), []byte("\xef\xbb\xbf"))
	if len(data) == 0 || len(data) > maxJSONBytes {
		return nil, fmt.Errorf("%w: leitura do PnP vazia ou grande demais", ErrTreeIncomplete)
	}
	var env rawEnvelope
	if err := json.Unmarshal(data, &env); err != nil {
		return nil, fmt.Errorf("%w: leitura do PnP ilegível ou cortada: %w", ErrTreeIncomplete, err)
	}
	if len(env.Devices) == 0 || len(env.Devices) > maxDevices || env.Count != len(env.Devices) {
		return nil, fmt.Errorf("%w: a leitura do PnP trouxe %d dispositivo(s) mas anuncia %d", ErrTreeIncomplete, len(env.Devices), env.Count)
	}
	out := make([]Device, 0, len(env.Devices))
	for _, r := range env.Devices {
		if err := checkID("ID de instância", r.InstanceID, false); err != nil {
			return nil, err
		}
		if err := checkID("ID do pai", r.Parent, true); err != nil {
			return nil, err
		}
		if !reClassName.MatchString(r.Class) || !reClassGUID.MatchString(r.ClassGUID) {
			return nil, fmt.Errorf("%w: classe fora do formato seguro", ErrTreeIncomplete)
		}
		if len(r.HardwareIDs) > maxIDsPerDev || len(r.CompatibleIDs) > maxIDsPerDev {
			return nil, fmt.Errorf("%w: IDs demais num dispositivo", ErrTreeIncomplete)
		}
		d := Device{InstanceID: r.InstanceID, Parent: r.Parent, Class: r.Class, ClassGUID: r.ClassGUID}
		name := textsafe.Line(r.Name)
		if len([]rune(name)) > 120 {
			name = string([]rune(name)[:120])
		}
		d.Name = name
		for _, id := range r.HardwareIDs {
			if err := checkID("ID de hardware", id, false); err != nil {
				return nil, err
			}
			d.HardwareIDs = append(d.HardwareIDs, id)
		}
		for _, id := range r.CompatibleIDs {
			if err := checkID("ID compatível", id, false); err != nil {
				return nil, err
			}
			d.CompatibleIDs = append(d.CompatibleIDs, id)
		}
		out = append(out, d)
	}
	return out, nil
}

// DeviceTree é a árvore de pais dos dispositivos presentes.
type DeviceTree struct {
	devs  []Device
	byKey map[string]int // instância em minúsculas -> índice
}

func lk(s string) string { return strings.ToLower(s) }

// NewDeviceTree monta a árvore e RECUSA (ErrTreeIncomplete) instância repetida, pai desconhecido, ciclo e qualquer número de
// raízes diferente de um. Nada disso é "provavelmente seguro".
func NewDeviceTree(devs []Device) (*DeviceTree, error) {
	t := &DeviceTree{devs: devs, byKey: make(map[string]int, len(devs))}
	roots := 0
	for i, d := range devs {
		k := lk(d.InstanceID)
		if _, dup := t.byKey[k]; dup {
			return nil, fmt.Errorf("%w: instância repetida (%s)", ErrTreeIncomplete, textsafe.Line(d.InstanceID))
		}
		t.byKey[k] = i
		if d.Parent == "" {
			roots++
		}
	}
	if roots != 1 {
		return nil, fmt.Errorf("%w: a árvore tem %d raiz(es), esperada 1", ErrTreeIncomplete, roots)
	}
	for _, d := range devs {
		if d.Parent == "" {
			continue
		}
		if _, ok := t.byKey[lk(d.Parent)]; !ok {
			return nil, fmt.Errorf("%w: o pai de %q não aparece na leitura", ErrTreeIncomplete, d.Name)
		}
	}
	for _, d := range devs { // ciclo: subir mais vezes que o número de nós
		cur, steps := d, 0
		for cur.Parent != "" {
			steps++
			if steps > len(devs) {
				return nil, fmt.Errorf("%w: ciclo de pais em %q", ErrTreeIncomplete, d.Name)
			}
			cur = devs[t.byKey[lk(cur.Parent)]]
		}
	}
	return t, nil
}

// Get devolve o dispositivo pela instância.
func (t *DeviceTree) Get(inst string) (Device, bool) {
	i, ok := t.byKey[lk(inst)]
	if !ok {
		return Device{}, false
	}
	return t.devs[i], true
}

// Ancestors devolve as instâncias dos pais, do mais próximo à raiz (sem o próprio).
func (t *DeviceTree) Ancestors(inst string) []string {
	var out []string
	d, ok := t.Get(inst)
	for ok && d.Parent != "" {
		out = append(out, d.Parent)
		d, ok = t.Get(d.Parent)
	}
	return out
}

// busOf diz o barramento pelo prefixo do ID de instância (o que o PnP usa para separar os enumeradores).
func busOf(inst string) string {
	pre, rest, found := strings.Cut(inst, `\`)
	if !found {
		return "outro"
	}
	switch strings.ToUpper(pre) {
	case "USB", "USBSTOR", "USBPRINT":
		return "USB"
	case "BTHENUM", "BTHLE", "BTH":
		return "Bluetooth"
	case "ACPI":
		seg, _, _ := strings.Cut(strings.ToUpper(rest), `\`)
		if strings.HasPrefix(seg, "PNP03") || strings.HasPrefix(seg, "PNP0F") {
			return "PS/2"
		}
		return "ACPI"
	case "HTREE":
		return "raiz"
	}
	return strings.ToUpper(pre)
}

// vidpid extrai "VVVV:PPPP" (maiúsculas) de um ID com VID_xxxx&PID_xxxx.
func vidpid(id string) (string, bool) {
	m := reVIDPID.FindStringSubmatch(id)
	if m == nil {
		return "", false
	}
	return strings.ToUpper(m[1]) + ":" + strings.ToUpper(m[2]), true
}

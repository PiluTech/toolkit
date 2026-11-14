package usb

import (
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/PiluTech/toolkit/internal/privacidade"
)

// Entry é uma escrita da política: caminho RELATIVO a Restrictions e o valor.
type Entry struct {
	Path  string
	Value RegValue
}

// Nomes da lista fechada de valores/subchaves (relativos a RestrictionsKey).
const (
	nameLayered = "AllowDenyLayered"
	nameAllow   = "AllowDeviceIDs"
	nameDeny    = "DenyDeviceClasses"
)

var reIDValue = regexp.MustCompile(`^[A-Za-z0-9_\\&.{}-]{1,200}$`)

// checkRel confere um caminho relativo contra a LISTA FECHADA de alvos: os três valores de ligação e os valores numerados
// (1..MaxAllowIDs, sem zero à esquerda) das duas subchaves. Qualquer outro caminho (DenyUnspecified, DenyDeviceIDs,
// "..", outra raiz, maiúsculas diferentes) é recusado ANTES de tocar no registro.
func checkRel(rel string) error {
	switch rel {
	case nameLayered, nameAllow, nameDeny:
		return nil
	}
	sub, num, ok := strings.Cut(rel, `\`)
	if ok {
		limit := 0
		switch sub {
		case nameAllow:
			limit = MaxAllowIDs
		case nameDeny:
			limit = len(denyClassGUIDs)
		}
		n, err := strconv.Atoi(num)
		if limit > 0 && err == nil && n >= 1 && n <= limit && strconv.Itoa(n) == num {
			return nil
		}
	}
	return fmt.Errorf("%w: %q não está na lista fechada de alvos do toolkit", ErrEntradaInvalida, printableRel(rel))
}

func printableRel(s string) string {
	r := []rune(s)
	if len(r) > 60 {
		r = append(r[:60], '…')
	}
	for i, c := range r {
		if c < 0x20 || c == 0x7f {
			r[i] = '?'
		}
	}
	return string(r)
}

// checkWrite confere caminho E valor: os três valores de ligação são DWORD 1; os numerados são texto no formato de ID.
func checkWrite(rel string, v RegValue) error {
	if err := checkRel(rel); err != nil {
		return err
	}
	switch rel {
	case nameLayered, nameAllow, nameDeny:
		if v.Kind != privacidade.RegDWORD || v.DWORD != 1 {
			return fmt.Errorf("%w: %s só aceita DWORD 1", ErrEntradaInvalida, rel)
		}
		return nil
	}
	if v.Kind != privacidade.RegString || !reIDValue.MatchString(v.Str) {
		return fmt.Errorf("%w: valor de %s fora do formato de ID", ErrEntradaInvalida, rel)
	}
	return nil
}

// Policy é a política que o toolkit grava: ordem em camadas, liberação por ID e negação só das duas classes de USB.
type Policy struct {
	AllowIDs    []string
	DenyClasses []string
}

// Entries devolve as escritas NA ORDEM SEGURA: a camada primeiro (sem ela a negação venceria a liberação e tudo de USB seria
// bloqueado), depois a lista de IDs e só então a negação, por último.
func (p Policy) Entries() []Entry {
	out := []Entry{{nameLayered, DWORD(1)}}
	for i, id := range p.AllowIDs {
		out = append(out, Entry{nameAllow + `\` + strconv.Itoa(i+1), String(id)})
	}
	out = append(out, Entry{nameAllow, DWORD(1)})
	for i, c := range p.DenyClasses {
		out = append(out, Entry{nameDeny + `\` + strconv.Itoa(i+1), String(c)})
	}
	return append(out, Entry{nameDeny, DWORD(1)})
}

// verdict avalia UM dispositivo na ordem em camadas: ID (hardware E compatíveis) > classe > sem regra (permitido, porque o
// toolkit não liga DenyUnspecified).
func (p Policy) verdict(d Device) (bool, string) {
	allow := make(map[string]bool, len(p.AllowIDs))
	for _, id := range p.AllowIDs {
		allow[lk(id)] = true
	}
	for _, id := range slices.Concat(d.HardwareIDs, d.CompatibleIDs) {
		if allow[lk(id)] {
			return true, "permitido pelo ID " + id
		}
	}
	for _, c := range p.DenyClasses {
		if d.ClassGUID != "" && strings.EqualFold(c, d.ClassGUID) {
			return false, "a classe " + classLabel(d) + " é negada pela política e o dispositivo não está na lista de IDs"
		}
	}
	return true, "nenhuma regra o nega (a classe " + classLabel(d) + " não está na lista de bloqueio)"
}

func classLabel(d Device) string {
	if d.Class != "" {
		return d.Class
	}
	return "sem classe"
}

// ProofLine é a evidência de UM dispositivo essencial: nome, classe, barramento, se fica permitido e por quê.
type ProofLine struct {
	Name, Class, Bus string
	Allowed          bool
	By               string
	Chain            []string // nomes dos pais, do mais próximo à raiz
}

// Proof é a prova de que os dispositivos essenciais continuam permitidos. OK só quando há linhas, nenhuma bloqueada e o
// disco interno foi visto.
type Proof struct {
	Lines    []ProofLine
	Failures []string
	OK       bool
}

// Unit é um dispositivo USB que o usuário vê (e marca ou desmarca): um nó USB que não é interface (&MI_) nem hub raiz.
type Unit struct {
	Key       string // ID de instância
	Name      string
	VIDPID    string
	Class     string
	Selected  bool
	Essential bool // contém teclado, mouse ou outro HID: desmarcar recusa
	Blocked   bool // a política o bloquearia ao reconectar
	IDs       []string
}

// Analysis é o resultado da análise do PnP: a política candidata, os dispositivos e a prova.
type Analysis struct {
	Policy Policy
	Units  []Unit
	Proof  Proof
}

func isUnitNode(d Device) bool {
	pre, rest, ok := strings.Cut(d.InstanceID, `\`)
	if !ok || !strings.EqualFold(pre, "USB") {
		return false
	}
	seg, _, _ := strings.Cut(strings.ToUpper(rest), `\`)
	return !strings.Contains(seg, "&MI_") && !strings.HasPrefix(seg, "ROOT_HUB")
}

func essentialByClass(d Device, attached bool) bool {
	switch strings.ToLower(d.Class) {
	case "keyboard", "mouse", "hidclass":
		return true
	case "diskdrive", "hdc", "scsiadapter", "usb":
		return !attached // disco, controlador e hub raiz INTERNOS; os USB externos são escolha do usuário
	}
	return false
}

// Analyze monta a política candidata (IDs de hardware com VID/PID dos USB conectados e marcados, mais o conjunto genérico
// fechado) e PROVA, avaliando-a contra a árvore de pais, que todo dispositivo essencial continua permitido. excluded são
// as instâncias (Unit.Key) desmarcadas; uma instância que não existe mais é erro (seleção velha). Árvore incompleta =
// ErrTreeIncomplete.
func Analyze(devs []Device, excluded []string) (*Analysis, error) {
	tree, err := NewDeviceTree(devs)
	if err != nil {
		return nil, err
	}
	// dono USB de cada nó: o USB visível mais próximo, subindo a partir dele.
	owner := make(map[string]string, len(devs))
	var ownerOf func(d Device) string
	ownerOf = func(d Device) string {
		if o, ok := owner[lk(d.InstanceID)]; ok {
			return o
		}
		o := ""
		switch {
		case isUnitNode(d):
			o = d.InstanceID
		case d.Parent != "":
			p, _ := tree.Get(d.Parent)
			o = ownerOf(p)
		}
		owner[lk(d.InstanceID)] = o
		return o
	}
	units := map[string]*Unit{}
	var order []string
	for _, d := range devs {
		if isUnitNode(d) {
			v, _ := vidpid(d.InstanceID)
			units[lk(d.InstanceID)] = &Unit{Key: d.InstanceID, Name: d.Name, VIDPID: v, Class: d.Class, Selected: true}
			order = append(order, lk(d.InstanceID))
		}
	}
	for _, x := range excluded {
		u, ok := units[lk(x)]
		if !ok {
			return nil, fmt.Errorf("o dispositivo %q não está conectado (a seleção é de uma leitura antiga: veja o plano de novo)", printableRel(x))
		}
		u.Selected = false
	}

	// política: o conjunto genérico fechado + VID/PID de todo nó dos USB marcados
	ids := map[string]string{}
	for _, g := range genericAllowIDs {
		ids[lk(g)] = g
	}
	for _, d := range devs {
		u := units[lk(ownerOf(d))]
		if u == nil || !u.Selected {
			continue
		}
		for _, h := range d.HardwareIDs { // só ID de HARDWARE com VID e PID; compatível nunca entra
			if _, ok := vidpid(h); ok && reIDValue.MatchString(h) {
				ids[lk(h)] = h
				u.IDs = append(u.IDs, h)
			}
		}
	}
	pol := Policy{DenyClasses: slices.Clone(denyClassGUIDs)}
	for _, v := range ids {
		pol.AllowIDs = append(pol.AllowIDs, v)
	}
	sort.Strings(pol.AllowIDs)
	if len(pol.AllowIDs) > MaxAllowIDs {
		return nil, fmt.Errorf("%w: a lista teria %d IDs (limite %d)", ErrNoProof, len(pol.AllowIDs), MaxAllowIDs)
	}

	// efetivo: um nó é bloqueado se ELE ou qualquer pai for bloqueado
	eff := map[string]bool{}
	why := map[string]string{}
	var effective func(d Device) bool
	effective = func(d Device) bool {
		k := lk(d.InstanceID)
		if v, ok := eff[k]; ok {
			return v
		}
		ok, by := pol.verdict(d)
		why[k] = by
		if ok && d.Parent != "" {
			p, _ := tree.Get(d.Parent)
			if !effective(p) {
				ok = false
				why[k] = "bloqueado porque um dispositivo pai (" + p.Name + ") fica bloqueado"
			}
		}
		eff[k] = ok
		return ok
	}

	// dispositivos essenciais (qualquer barramento) e, para os USB, todos os nós do mesmo dispositivo (composto)
	ess := map[string]bool{}
	for _, d := range devs {
		if essentialByClass(d, ownerOf(d) != "") {
			ess[lk(d.InstanceID)] = true
		}
	}
	for _, d := range devs {
		if o := ownerOf(d); o != "" && ess[lk(d.InstanceID)] {
			units[lk(o)].Essential = true
		}
	}
	for _, d := range devs {
		if o := ownerOf(d); o != "" && units[lk(o)].Essential {
			ess[lk(d.InstanceID)] = true
		}
	}

	var pr Proof
	haveDisk := false
	for _, d := range devs {
		if !ess[lk(d.InstanceID)] {
			continue
		}
		if strings.EqualFold(d.Class, "DiskDrive") && ownerOf(d) == "" {
			haveDisk = true
		}
		l := ProofLine{Name: d.Name, Class: d.Class, Bus: lineBus(d, tree), Allowed: effective(d), By: why[lk(d.InstanceID)]}
		for _, a := range tree.Ancestors(d.InstanceID) {
			p, _ := tree.Get(a)
			l.Chain = append(l.Chain, p.Name)
		}
		pr.Lines = append(pr.Lines, l)
		if !l.Allowed {
			pr.Failures = append(pr.Failures, fmt.Sprintf("%s (%s, %s) ficaria BLOQUEADO: %s", l.Name, classLabel(d), l.Bus, l.By))
		}
	}
	// Teclado, mouse ou outro HID DESMARCADO é recusa por regra, qualquer que seja a classe do nó USB: a política só nega as duas
	// classes de USB, então um dispositivo simples (nó de classe HIDClass) ficaria "permitido" pelo avaliador mesmo desmarcado;
	// o toolkit não aceita deixar um essencial fora da lista.
	for _, k := range order {
		if u := units[k]; u.Essential && !u.Selected {
			pr.Failures = append(pr.Failures, fmt.Sprintf("%s (%s) está desmarcado, mas contém teclado, mouse ou outro HID: o toolkit recusa deixá-lo fora da lista (marque-o de volta)", u.Name, dashStr(u.VIDPID)))
		}
	}
	if !haveDisk {
		pr.Failures = append(pr.Failures, "nenhum disco interno apareceu na leitura: não dá para provar que o disco do sistema continua permitido")
	}
	sort.SliceStable(pr.Lines, func(i, j int) bool {
		a, b := pr.Lines[i], pr.Lines[j]
		if a.Bus != b.Bus {
			return a.Bus < b.Bus
		}
		if a.Class != b.Class {
			return a.Class < b.Class
		}
		return a.Name < b.Name
	})
	pr.OK = len(pr.Failures) == 0 && len(pr.Lines) > 0

	an := &Analysis{Policy: pol, Proof: pr}
	for _, k := range order {
		u := units[k]
		d, _ := tree.Get(u.Key)
		u.Blocked = !effective(d)
		slices.Sort(u.IDs)
		u.IDs = slices.Compact(u.IDs)
		an.Units = append(an.Units, *u)
	}
	sort.SliceStable(an.Units, func(i, j int) bool {
		if an.Units[i].Name != an.Units[j].Name {
			return an.Units[i].Name < an.Units[j].Name
		}
		return an.Units[i].Key < an.Units[j].Key
	})
	return an, nil
}

func dashStr(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// lineBus é o barramento mostrado na prova: o do nó; HID herda o do pai (o HID é lógico, o físico é o do pai).
func lineBus(d Device, tree *DeviceTree) string {
	b := busOf(d.InstanceID)
	cur := d
	for b == "HID" && cur.Parent != "" {
		p, ok := tree.Get(cur.Parent)
		if !ok {
			break
		}
		cur = p
		if pb := busOf(p.InstanceID); pb != "HID" {
			return pb
		}
	}
	return b
}

package privacidade

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/PiluTech/toolkit/internal/diagfmt"
)

// Erros do pacote.
var (
	// ErrNeedsAdmin: há mudanças que exigem administrador e o processo não está elevado. Nada foi alterado.
	ErrNeedsAdmin = errors.New("este perfil mexe em política, serviços e tarefas do Windows e exige administrador")
	// ErrNoBackup: não há nada a desfazer.
	ErrNoBackup = errors.New("não há backup de privacidade para desfazer")
)

// Change é uma mudança do plano (uma chave de registro, um serviço ou uma tarefa).
type Change struct {
	Setting string // id do ajuste
	Kind    string // "reg", "service" ou "task"
	ID      string // chave do backup
	Target  string // o que muda, legível ("HKCU\\...\\Enabled", "serviço DiagTrack", "tarefa ...")
	Before  string // estado atual, legível
	After   string // estado depois
	Pending bool   // false = já está assim (nada a fazer)
	Admin   bool
	Undo    string // comando manual que restaura o estado de ANTES desta mudança

	orig  Entry                                     // estado de antes, para o backup
	apply func(ctx context.Context, s System) error // aplica a mudança
}

// Plan é o que `aplicar` faria.
type Plan struct {
	Perfil  Perfil
	Edition string
	IsAdmin bool
	Changes []Change
	// Missing lista o que não existe nesta versão do Windows (serviço ou tarefa ausente): não é erro, é só ignorado.
	Missing []string
}

// NeedsAdmin diz se alguma mudança PENDENTE exige administrador.
func (p *Plan) NeedsAdmin() bool {
	for _, c := range p.Changes {
		if c.Pending && c.Admin {
			return true
		}
	}
	return false
}

// Pending conta as mudanças pendentes.
func (p *Plan) Pending() int {
	n := 0
	for _, c := range p.Changes {
		if c.Pending {
			n++
		}
	}
	return n
}

func regID(key, name string) string { return "reg:" + key + "|" + name }

func describeReg(v RegValue, exists bool, l Lang) string {
	if !exists {
		return diagfmt.T(l, "não definido", "not set", "no definido")
	}
	if v.Kind == RegString {
		return `"` + v.Str + `"`
	}
	return fmt.Sprintf("%d", v.DWORD)
}

func regUndo(key, name string, v RegValue, exists bool) string {
	if !exists {
		return fmt.Sprintf(`reg delete "%s" /v %s /f`, key, name)
	}
	if v.Kind == RegString {
		return fmt.Sprintf(`reg add "%s" /v %s /t REG_SZ /d "%s" /f`, key, name, v.Str)
	}
	return fmt.Sprintf(`reg add "%s" /v %s /t REG_DWORD /d %d /f`, key, name, v.DWORD)
}

func startTypeSC(t string) string {
	switch t {
	case "automatic":
		return "auto"
	case "manual":
		return "demand"
	}
	return t // delayed-auto e disabled
}

func describeService(st ServiceState, l Lang) string {
	run := diagfmt.T(l, "parado", "stopped", "detenido")
	if st.Running {
		run = diagfmt.T(l, "em execução", "running", "en ejecución")
	}
	return st.StartType + ", " + run
}

func serviceUndo(name string, st ServiceState) string {
	cmd := fmt.Sprintf("sc config %s start= %s", name, startTypeSC(st.StartType))
	if st.Running {
		cmd += fmt.Sprintf(" & sc start %s", name)
	}
	return cmd
}

func describeTask(en bool, l Lang) string {
	if en {
		return diagfmt.T(l, "habilitada", "enabled", "habilitada")
	}
	return diagfmt.T(l, "desabilitada", "disabled", "deshabilitada")
}

func taskUndo(path string, enabled bool) string {
	flag := "/DISABLE"
	if enabled {
		flag = "/ENABLE"
	}
	return fmt.Sprintf(`schtasks /Change /TN "%s" %s`, path, flag)
}

// Build lê o estado atual (só leitura, sem administrador) e monta o plano do perfil.
func Build(ctx context.Context, sys System, p Perfil, l Lang) (*Plan, error) {
	ed, err := sys.Edition(ctx)
	if err != nil {
		return nil, fmt.Errorf("não deu para descobrir a edição do Windows: %w", err)
	}
	plan := &Plan{Perfil: p, Edition: ed, IsAdmin: sys.IsAdmin(ctx)}
	for _, s := range ParaPerfil(p) {
		for _, r := range s.Regs {
			cur, exists, err := sys.RegRead(ctx, r.Key, r.Name)
			if err != nil {
				return nil, fmt.Errorf("%s\\%s: %w", r.Key, r.Name, err)
			}
			want := r.Value(ed)
			c := Change{Setting: s.ID, Kind: "reg", ID: regID(r.Key, r.Name), Target: r.Key + `\` + r.Name, Admin: isHKLM(r.Key),
				Before: describeReg(cur, exists, l), After: describeReg(want, true, l), Pending: !exists || cur != want,
				Undo: regUndo(r.Key, r.Name, cur, exists),
				orig: Entry{ID: regID(r.Key, r.Name), Kind: "reg", Key: r.Key, Name: r.Name, Existed: exists, RegKind: cur.Kind, DWORD: cur.DWORD, Str: cur.Str}}
			key, name := r.Key, r.Name
			c.apply = func(ctx context.Context, s System) error { return s.RegWrite(ctx, key, name, want) }
			plan.Changes = append(plan.Changes, c)
		}
		for _, sv := range s.Svcs {
			st, err := sys.ServiceRead(ctx, sv.Name)
			if err != nil {
				return nil, fmt.Errorf("serviço %s: %w", sv.Name, err)
			}
			if !st.Exists {
				plan.Missing = append(plan.Missing, diagfmt.T(l, "serviço ", "service ", "servicio ")+sv.Name)
				continue
			}
			name := sv.Name
			c := Change{Setting: s.ID, Kind: "service", ID: "svc:" + name, Target: diagfmt.T(l, "serviço ", "service ", "servicio ") + name, Admin: true,
				Before: describeService(st, l), After: "disabled, " + diagfmt.T(l, "parado", "stopped", "detenido"),
				Pending: st.StartType != "disabled" || st.Running, Undo: serviceUndo(name, st),
				orig: Entry{ID: "svc:" + name, Kind: "service", Service: name, StartType: st.StartType, Running: st.Running}}
			c.apply = func(ctx context.Context, s System) error {
				if err := s.ServiceConfigure(ctx, name, "disabled"); err != nil {
					return err
				}
				return s.ServiceStop(ctx, name)
			}
			plan.Changes = append(plan.Changes, c)
		}
		for _, tk := range s.Tasks {
			st, err := sys.TaskRead(ctx, tk.Path)
			if err != nil {
				return nil, fmt.Errorf("tarefa %s: %w", tk.Path, err)
			}
			if !st.Exists {
				plan.Missing = append(plan.Missing, diagfmt.T(l, "tarefa ", "task ", "tarea ")+tk.Path)
				continue
			}
			path := tk.Path
			c := Change{Setting: s.ID, Kind: "task", ID: "task:" + path, Target: diagfmt.T(l, "tarefa ", "task ", "tarea ") + path, Admin: true,
				Before: describeTask(st.Enabled, l), After: describeTask(false, l), Pending: st.Enabled, Undo: taskUndo(path, st.Enabled),
				orig: Entry{ID: "task:" + path, Kind: "task", Task: path, Enabled: st.Enabled}}
			c.apply = func(ctx context.Context, s System) error { return s.TaskSetEnabled(ctx, path, false) }
			plan.Changes = append(plan.Changes, c)
		}
	}
	return plan, nil
}

// Format mostra o plano: o que muda (antes -> depois), o que já está assim, o que exige administrador, a edição e o
// comando que desfaz cada item.
func (p *Plan) Format(l Lang) string {
	t := func(pt, en, es string) string { return diagfmt.T(l, pt, en, es) }
	var b strings.Builder
	fmt.Fprintf(&b, "%s: %s\n", t("Perfil", "Profile", "Perfil"), p.Perfil)
	fmt.Fprintf(&b, "%s: %s\n", t("Edição do Windows", "Windows edition", "Edición de Windows"), editionName(p.Edition, l))
	if p.Edition == EditionHome || p.Edition == EditionPro {
		fmt.Fprintln(&b, t("Nota: no Home e no Pro o mínimo de dados de diagnóstico é \"obrigatório\" (valor 1); o Windows ignora 0 nessas edições. Não dá para desligar a coleta obrigatória sem trocar de edição.",
			"Note: on Home and Pro the minimum diagnostic data is \"required\" (value 1); Windows ignores 0 on these editions. The required collection cannot be turned off without changing edition.",
			"Nota: en Home y Pro el mínimo de datos de diagnóstico es \"obligatorio\" (valor 1); Windows ignora 0 en estas ediciones. No se puede desactivar la recopilación obligatoria sin cambiar de edición."))
	}
	byID := map[string]Setting{}
	for _, s := range Settings() {
		byID[s.ID] = s
	}
	var order []string
	groups := map[string][]Change{}
	for _, c := range p.Changes {
		if _, ok := groups[c.Setting]; !ok {
			order = append(order, c.Setting)
		}
		groups[c.Setting] = append(groups[c.Setting], c)
	}
	for _, id := range order {
		s, known := byID[id]
		if !known { // plano de desfazer: os itens vêm do backup, não do catálogo
			s = Setting{ID: id, Admin: groupAdmin(groups[id]),
				Title: tr("Restaurar o estado anterior", "Restore the previous state", "Restaurar el estado anterior"),
				Why:   tr("Cada item volta ao valor que tinha antes do toolkit mexer.", "Each item goes back to the value it had before the toolkit changed it.", "Cada elemento vuelve al valor que tenía antes de que el toolkit lo cambiara.")}
		}
		admin := ""
		if s.Admin {
			admin = " [" + t("exige administrador", "needs administrator", "requiere administrador") + "]"
		}
		fmt.Fprintf(&b, "\n%s%s\n  %s\n", s.Title(l), admin, s.Why(l))
		for _, c := range groups[id] {
			if c.Pending {
				fmt.Fprintf(&b, "  ! %s: %s -> %s\n      %s %s\n", c.Target, c.Before, c.After, t("desfazer:", "undo:", "deshacer:"), c.Undo)
			} else {
				fmt.Fprintf(&b, "  ✓ %s: %s (%s)\n", c.Target, c.Before, t("já está assim", "already so", "ya está así"))
			}
		}
	}
	if len(p.Missing) > 0 {
		sort.Strings(p.Missing)
		fmt.Fprintf(&b, "\n%s: %s\n", t("Não existe nesta versão do Windows (ignorado)", "Not present in this Windows version (ignored)", "No existe en esta versión de Windows (ignorado)"), strings.Join(p.Missing, "; "))
	}
	n := p.Pending()
	fmt.Fprintf(&b, "\n%d %s", n, t("mudança(s) pendente(s)", "pending change(s)", "cambio(s) pendiente(s)"))
	if p.NeedsAdmin() && !p.IsAdmin {
		fmt.Fprintf(&b, "; %s", t("para aplicar abra o terminal como administrador", "to apply, open the terminal as administrator", "para aplicar, abra el terminal como administrador"))
	}
	b.WriteString(".\n")
	return b.String()
}

func groupAdmin(cs []Change) bool {
	for _, c := range cs {
		if c.Admin {
			return true
		}
	}
	return false
}

func editionName(ed string, l Lang) string {
	switch ed {
	case EditionHome:
		return "Home"
	case EditionPro:
		return "Pro"
	case EditionEnterprise:
		return "Enterprise"
	case EditionEducation:
		return "Education"
	}
	return diagfmt.T(l, "outra", "other", "otra")
}

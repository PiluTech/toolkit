package diagnostico

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/PiluTech/toolkit/internal/diagfmt"
)

// Símbolos do resumo (sempre com a palavra da gravidade ao lado: nada depende só de cor ou forma).
const (
	symOK   = "✓"
	symInfo = "-"
	symWarn = "!"
	symBad  = "✗"
)

func sevWord(s Severity, l Lang) (sym, word string) {
	switch s {
	case SevBad:
		return symBad, diagfmt.T(l, "problema", "problem", "problema")
	case SevWarn:
		return symWarn, diagfmt.T(l, "atenção", "attention", "atención")
	case SevInfo:
		return symInfo, diagfmt.T(l, "info", "info", "info")
	}
	return symOK, "ok"
}

// Summary é o resumo "o que está ruim e o que fazer": um achado por bloco, do mais grave ao mais leve.
func Summary(r *Report, fs []Finding, l Lang) string {
	t := func(pt, en, es string) string { return diagfmt.T(l, pt, en, es) }
	var b strings.Builder
	fmt.Fprintf(&b, "%s (%s)\n\n", t("Diagnóstico da máquina", "Machine diagnosis", "Diagnóstico del equipo"), osName(r.OS))
	bad, warn := 0, 0
	for _, f := range fs {
		switch f.Severity {
		case SevBad:
			bad++
		case SevWarn:
			warn++
		}
	}
	switch {
	case bad > 0:
		fmt.Fprintf(&b, "%s: %d %s, %d %s.\n\n", t("Resultado", "Result", "Resultado"), bad, t("problema(s)", "problem(s)", "problema(s)"), warn, t("atenção", "attention", "atención"))
	case warn > 0:
		fmt.Fprintf(&b, "%s: %d %s.\n\n", t("Resultado", "Result", "Resultado"), warn, t("ponto(s) de atenção", "point(s) of attention", "punto(s) de atención"))
	default:
		fmt.Fprintf(&b, "%s: %s.\n\n", t("Resultado", "Result", "Resultado"), t("nada de ruim encontrado", "nothing bad found", "no se encontró nada malo"))
	}
	for _, f := range fs {
		sym, word := sevWord(f.Severity, l)
		fmt.Fprintf(&b, "%s %s: %s", sym, word, f.Title)
		if f.Detail != "" {
			fmt.Fprintf(&b, " — %s", f.Detail)
		}
		b.WriteByte('\n')
		if f.Action != "" {
			fmt.Fprintf(&b, "    %s %s\n", t("O que fazer:", "What to do:", "Qué hacer:"), f.Action)
		}
	}
	return strings.TrimRight(b.String(), "\n") + "\n"
}

func osName(os string) string {
	switch os {
	case "windows":
		return "Windows"
	case "darwin":
		return "macOS"
	case "linux":
		return "Linux"
	}
	return os
}

func dur(d time.Duration, l Lang) string {
	if d <= 0 {
		return ""
	}
	days := int(d.Hours()) / 24
	hours := int(d.Hours()) % 24
	if days > 0 {
		return fmt.Sprintf("%d %s %d h", days, diagfmt.T(l, "dias", "days", "días"), hours)
	}
	return fmt.Sprintf("%d h %d min", hours, int(d.Minutes())%60)
}

// Full é o relatório completo: todos os dados lidos, por área, como o sistema os informou (nada é mascarado).
func Full(r *Report, l Lang) string {
	t := func(pt, en, es string) string { return diagfmt.T(l, pt, en, es) }
	var b strings.Builder
	h := func(title string) { fmt.Fprintf(&b, "\n== %s ==\n", title) }
	fmt.Fprintf(&b, "%s — %s\n", t("Relatório completo do diagnóstico", "Full diagnosis report", "Informe completo del diagnóstico"), osName(r.OS))
	if !r.Generated.IsZero() {
		fmt.Fprintf(&b, "%s\n", r.Generated.Format("2006-01-02 15:04"))
	}

	h(t("Hardware", "Hardware", "Hardware"))
	hw := r.Hardware
	line := func(k, v string) {
		if strings.TrimSpace(v) != "" {
			fmt.Fprintf(&b, "%s: %s\n", k, v)
		}
	}
	line(t("Fabricante", "Vendor", "Fabricante"), hw.Vendor)
	line(t("Modelo", "Model", "Modelo"), hw.Model)
	line("CPU", hw.CPU)
	if hw.Cores > 0 || hw.Threads > 0 {
		line(t("Núcleos / threads", "Cores / threads", "Núcleos / hilos"), fmt.Sprintf("%d / %d", hw.Cores, hw.Threads))
	}
	line(t("Firmware", "Firmware", "Firmware"), hw.Firmware)
	line(t("Ligado há", "Uptime", "Encendido hace"), dur(hw.Uptime, l))

	h(t("Memória", "Memory", "Memoria"))
	if r.Memory.TotalBytes > 0 {
		s := gb(r.Memory.TotalBytes, l) + " " + t("no total", "total", "en total")
		if r.Memory.UsedBytes > 0 {
			s += fmt.Sprintf(", %s %s (%s)", gb(r.Memory.UsedBytes, l), t("em uso", "in use", "en uso"), diagfmt.Percent(float64(r.Memory.UsedBytes)/float64(r.Memory.TotalBytes)))
		}
		fmt.Fprintln(&b, s)
		if r.Memory.SwapTotalBytes > 0 {
			fmt.Fprintf(&b, "Swap: %s / %s\n", gb(r.Memory.SwapUsedBytes, l), gb(r.Memory.SwapTotalBytes, l))
		}
	} else {
		fmt.Fprintln(&b, t("não lida", "not read", "no leída"))
	}

	h(t("Discos", "Disks", "Discos"))
	if len(r.Disks) == 0 {
		fmt.Fprintln(&b, t("nenhum disco lido", "no disks read", "ningún disco leído"))
	}
	for _, d := range r.Disks {
		parts := []string{d.Name}
		for _, p := range []string{d.Medium, d.Bus} {
			if p != "" {
				parts = append(parts, p)
			}
		}
		if d.SizeBytes > 0 {
			parts = append(parts, diagfmt.Bytes(d.SizeBytes, false, l))
		}
		if d.FreeBytes > 0 {
			parts = append(parts, gb(d.FreeBytes, l)+" "+t("livres", "free", "libres"))
		}
		if d.HealthRaw != "" {
			parts = append(parts, t("saúde: ", "health: ", "salud: ")+d.HealthRaw)
		} else {
			parts = append(parts, t("saúde: não lida", "health: not read", "salud: no leída"))
		}
		if d.WearPct != nil {
			parts = append(parts, fmt.Sprintf("%s %d%%", t("desgaste", "wear", "desgaste"), *d.WearPct))
		}
		if d.TempC != nil {
			parts = append(parts, fmt.Sprintf("%.0f °C", *d.TempC))
		}
		if d.PowerOnHours != nil {
			parts = append(parts, fmt.Sprintf("%d h %s", *d.PowerOnHours, t("ligado", "powered on", "encendido")))
		}
		if d.ReallocatedSectors != nil {
			parts = append(parts, fmt.Sprintf("%s %d", t("setores realocados", "reallocated sectors", "sectores reasignados"), *d.ReallocatedSectors))
		}
		fmt.Fprintf(&b, "- %s\n", strings.Join(parts, " · "))
	}

	h(t("Bateria", "Battery", "Batería"))
	if bt := r.Battery; bt == nil {
		fmt.Fprintln(&b, t("sem bateria (ou não lida)", "no battery (or not read)", "sin batería (o no leída)"))
	} else {
		var p []string
		if bt.Percent != nil {
			p = append(p, fmt.Sprintf("%d%%", *bt.Percent))
		}
		if bt.Status != "" {
			p = append(p, bt.Status)
		}
		if bt.HealthPct != nil {
			p = append(p, fmt.Sprintf("%s %d%%", t("capacidade", "capacity", "capacidad"), *bt.HealthPct))
		}
		if bt.CycleCount != nil {
			p = append(p, fmt.Sprintf("%d %s", *bt.CycleCount, t("ciclos", "cycles", "ciclos")))
		}
		if bt.Condition != "" {
			p = append(p, bt.Condition)
		}
		fmt.Fprintln(&b, strings.Join(p, " · "))
	}

	h(t("Temperaturas", "Temperatures", "Temperaturas"))
	if len(r.Temps) == 0 {
		fmt.Fprintln(&b, t("nenhum sensor lido", "no sensors read", "ningún sensor leído"))
	}
	temps := append([]Temp(nil), r.Temps...)
	sort.SliceStable(temps, func(i, j int) bool { return temps[i].Sensor < temps[j].Sensor })
	for _, tp := range temps {
		fmt.Fprintf(&b, "- %s: %.0f °C\n", tp.Sensor, tp.Celsius)
	}

	h("GPU")
	if len(r.GPUs) == 0 {
		fmt.Fprintln(&b, t("nenhuma lida", "none read", "ninguna leída"))
	}
	for _, g := range r.GPUs {
		p := []string{g.Name}
		if g.Driver != "" {
			p = append(p, t("driver ", "driver ", "controlador ")+g.Driver)
		}
		if g.VRAMBytes > 0 {
			p = append(p, gb(g.VRAMBytes, l))
		}
		fmt.Fprintf(&b, "- %s\n", strings.Join(p, " · "))
	}

	h(t("Erros recentes (período lido)", "Recent errors (period read)", "Errores recientes (período leído)"))
	if len(r.Events) == 0 {
		fmt.Fprintln(&b, t("nenhum erro registrado (ou log não lido)", "no errors recorded (or log not read)", "ningún error registrado (o registro no leído)"))
	}
	for _, e := range r.Events {
		src := e.Source
		if e.ID != "" {
			src += " (" + e.ID + ")"
		}
		when := ""
		if !e.When.IsZero() {
			when = e.When.Format("2006-01-02 15:04") + " "
		}
		fmt.Fprintf(&b, "- %s%s ×%d [%s] %s\n", when, src, max(e.Count, 1), e.Level, e.Message)
	}

	h(t("Falhas graves (30 dias)", "Serious failures (30 days)", "Fallos graves (30 días)"))
	if len(r.Crashes) == 0 {
		fmt.Fprintln(&b, t("nenhuma registrada (ou não lidas)", "none recorded (or not read)", "ninguno registrado (o no leído)"))
	}
	for _, c := range r.Crashes {
		when := ""
		if !c.When.IsZero() {
			when = c.When.Format("2006-01-02 15:04") + " "
		}
		fmt.Fprintf(&b, "- %s%s %s\n", when, c.Kind, c.Detail)
	}
	if r.Stability != nil {
		fmt.Fprintf(&b, "%s: %s/10\n", t("Índice de confiabilidade", "Reliability index", "Índice de fiabilidad"), diagfmt.Number(*r.Stability, 1, l))
	}

	if len(r.Skipped) > 0 {
		h(t("Não lido", "Not read", "No leído"))
		for _, s := range r.Skipped {
			fmt.Fprintf(&b, "- %s: %s\n", areaName(s.Area, l), s.Reason)
		}
	}
	return b.String()
}

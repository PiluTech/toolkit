package otimizacao

import (
	"fmt"
	"strings"
	"time"

	"github.com/PiluTech/toolkit/internal/diagfmt"
)

// Símbolos do resumo (sempre com a palavra da gravidade ao lado: nada depende só de cor ou forma).
func sevWord(s Severity, l Lang) (sym, word string) {
	switch s {
	case SevBad:
		return "✗", diagfmt.T(l, "problema", "problem", "problema")
	case SevWarn:
		return "!", diagfmt.T(l, "atenção", "attention", "atención")
	case SevInfo:
		return "-", "info"
	}
	return "✓", "ok"
}

// areaName é o nome legível de uma área (id estável) no idioma l.
func areaName(a string, l Lang) string {
	t := func(pt, en, es string) string { return diagfmt.T(l, pt, en, es) }
	switch a {
	case "cpu":
		return "CPU"
	case "memoria":
		return t("memória", "memory", "memoria")
	case "disco":
		return t("disco do sistema", "system disk", "disco del sistema")
	case "inicializacao":
		return t("inicialização", "startup", "inicio")
	case "servicos":
		return t("serviços", "services", "servicios")
	case "boot":
		return t("tempo de boot", "boot time", "tiempo de arranque")
	}
	return a
}

// Summary é o resumo "o que está ruim e o que fazer": um achado por bloco, do mais grave ao mais leve, as áreas não lidas
// (com o que fazer) e a linha de segurança (só leitura).
func Summary(r *Report, fs []Finding, l Lang) string {
	t := func(pt, en, es string) string { return diagfmt.T(l, pt, en, es) }
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n\n", t("Otimização do Windows — gargalos (só leitura)", "Windows optimization — bottlenecks (read-only)", "Optimización de Windows — cuellos de botella (solo lectura)"))
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
		fmt.Fprintf(&b, "%s: %s.\n\n", t("Resultado", "Result", "Resultado"), t("nenhum gargalo evidente", "no evident bottleneck", "ningún cuello de botella evidente"))
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
	if len(r.Skipped) > 0 {
		fmt.Fprintf(&b, "\n%s\n", t("Não lido:", "Not read:", "No leído:"))
		for _, s := range r.Skipped {
			fmt.Fprintf(&b, "- %s: %s\n", areaName(s.Area, l), s.Reason)
		}
	}
	fmt.Fprintf(&b, "\n%s\n", t("Segurança: só leitura, sem administrador e sem escrita; nada foi alterado. Só números e limiares documentados, sem pontuação; o \"o que fazer\" é texto para você decidir.",
		"Security: read-only, no administrator and no writes; nothing was changed. Only numbers and documented thresholds, no score; \"what to do\" is text for you to decide.",
		"Seguridad: solo lectura, sin administrador y sin escritura; no se cambió nada. Solo números y umbrales documentados, sin puntuación; el \"qué hacer\" es texto para que usted decida."))
	return strings.TrimRight(b.String(), "\n") + "\n"
}

func opt[T any](p *T, f func(T) string, none string) string {
	if p == nil {
		return none
	}
	return f(*p)
}

// Full é o relatório completo: todos os dados lidos, por área, como o sistema os informou.
func Full(r *Report, l Lang) string {
	t := func(pt, en, es string) string { return diagfmt.T(l, pt, en, es) }
	none := t("sem dado", "no data", "sin dato")
	var b strings.Builder
	h := func(s string) { fmt.Fprintf(&b, "== %s ==\n", s) }
	if c := r.CPU; c != nil {
		h("CPU")
		fmt.Fprintf(&b, "%s: %s | %s: %s | %s: %d\n", t("média", "average", "media"), pct(c.AvgPct, l), t("pico", "peak", "pico"), pct(c.PeakPct, l), t("leituras", "readings", "lecturas"), c.Samples)
		if c.Logical > 0 {
			fmt.Fprintf(&b, "%s: %d\n", t("processadores lógicos", "logical processors", "procesadores lógicos"), c.Logical)
		}
		for _, p := range c.Top {
			fmt.Fprintf(&b, "  %s (PID %d): %s\n", p.Name, p.PID, pct(p.CPUPct, l))
		}
		b.WriteByte('\n')
	}
	if m := r.Memory; m != nil {
		h(t("Memória", "Memory", "Memoria"))
		fmt.Fprintf(&b, "%s: %s | %s: %s\n", t("total", "total", "total"), gb(m.TotalBytes, l), t("disponível", "available", "disponible"), gb(m.AvailableBytes, l))
		if m.CommitLimit > 0 {
			fmt.Fprintf(&b, "commit: %s / %s\n", gb(m.CommitBytes, l), gb(m.CommitLimit, l))
		}
		if m.PageAllocBytes > 0 {
			fmt.Fprintf(&b, "%s: %s / %s\n", t("arquivo de paginação em uso", "paging file in use", "archivo de paginación en uso"), gb(m.PageUsedBytes, l), gb(m.PageAllocBytes, l))
		}
		for _, p := range m.Top {
			fmt.Fprintf(&b, "  %s (PID %d): %s\n", p.Name, p.PID, gb(p.MemBytes, l))
		}
		b.WriteByte('\n')
	}
	if d := r.Disk; d != nil {
		h(t("Disco do sistema", "System disk", "Disco del sistema"))
		medium := d.Medium
		if medium == "" {
			medium = t("tipo desconhecido", "unknown type", "tipo desconocido")
		}
		fmt.Fprintf(&b, "%s: %s | %s | %s\n", d.Drive+":", d.Name, medium, d.Bus)
		fmt.Fprintf(&b, "%s: %s / %s\n", t("livre", "free", "libre"), opt(d.FreeBytes, func(v uint64) string { return gb(v, l) }, none), gb(d.SizeBytes, l))
		fmt.Fprintf(&b, "%s: %s (%s %s)\n", t("fila média", "average queue", "cola media"), opt(d.QueueAvg, func(v float64) string { return diagfmt.Number(v, 2, l) }, none), t("pico", "peak", "pico"), opt(d.QueuePeak, func(v float64) string { return diagfmt.Number(v, 2, l) }, none))
		fmt.Fprintf(&b, "%s: %s\n\n", t("latência média", "average latency", "latencia media"), opt(d.LatencyMs, func(v float64) string { return diagfmt.Number(v, 1, l) + " ms" }, none))
	}
	if s := r.Startup; s != nil {
		h(t("Inicialização", "Startup", "Inicio"))
		fmt.Fprintf(&b, "%d %s (%s)\n", s.Total, t("itens registrados para iniciar", "items registered to start", "elementos registrados para iniciar"), t("a fonte não informa o peso de cada um", "the source does not report each one's weight", "la fuente no informa el peso de cada uno"))
		for _, it := range s.Items {
			fmt.Fprintf(&b, "  %s — %s\n", it.Name, it.Command)
		}
		if s.Total > len(s.Items) {
			fmt.Fprintf(&b, "  … %d %s\n", s.Total-len(s.Items), t("a mais", "more", "más"))
		}
		b.WriteByte('\n')
	}
	if sv := r.Services; sv != nil {
		h(t("Serviços automáticos", "Automatic services", "Servicios automáticos"))
		fmt.Fprintf(&b, "%d %s; %d %s\n", sv.AutoCount, t("no total", "in total", "en total"), sv.OutsideCount, t("fora da pasta do Windows", "outside the Windows folder", "fuera de la carpeta de Windows"))
		for _, s := range sv.Outside {
			fmt.Fprintf(&b, "  %s (%s) — %s\n", s.Name, s.State, s.Program)
		}
		if sv.OutsideCount > len(sv.Outside) {
			fmt.Fprintf(&b, "  … %d %s\n", sv.OutsideCount-len(sv.Outside), t("a mais", "more", "más"))
		}
		for _, s := range sv.Failed {
			fmt.Fprintf(&b, "  %s: %s (%s %d)\n", t("parado com erro", "stopped with an error", "detenido con error"), s.Name, t("código", "code", "código"), s.ExitCode)
		}
		b.WriteByte('\n')
	}
	if bt := r.Boot; bt != nil {
		h("Boot")
		if !bt.Found {
			fmt.Fprintf(&b, "%s. %s\n\n", none, noEventNote(l))
		} else {
			when := none
			if !bt.When.IsZero() {
				when = bt.When.Local().Format(time.DateTime)
			}
			dur := none
			if bt.DurationMs > 0 {
				dur = diagfmt.Number(float64(bt.DurationMs)/1000, 1, l) + " s"
			}
			deg := none
			if bt.Degraded != nil {
				deg = t("não", "no", "no")
				if *bt.Degraded {
					deg = t("sim", "yes", "sí")
				}
			}
			fmt.Fprintf(&b, "%s: %s | %s: %s | %s: %s\n\n", t("registrado em", "recorded at", "registrado el"), when, t("duração", "duration", "duración"), dur, t("marcado como lento pelo Windows", "flagged slow by Windows", "marcado como lento por Windows"), deg)
		}
	}
	if len(r.Skipped) > 0 {
		h(t("Não lido", "Not read", "No leído"))
		for _, s := range r.Skipped {
			fmt.Fprintf(&b, "- %s: %s\n", areaName(s.Area, l), s.Reason)
		}
		b.WriteByte('\n')
	}
	if b.Len() == 0 {
		return t("Nada foi lido.", "Nothing was read.", "No se leyó nada.") + "\n"
	}
	return strings.TrimRight(b.String(), "\n") + "\n"
}

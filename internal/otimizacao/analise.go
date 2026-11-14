package otimizacao

import (
	"fmt"
	"sort"
	"strings"

	"github.com/PiluTech/toolkit/internal/diagfmt"
)

// Limiares das regras (documentados em docs/otimizacao.md). Cada comparação é a escrita abaixo, e o teste de cada um cobre
// abaixo, igual e acima da fronteira. Os de memória livre e espaço livre são os MESMOS do diagnóstico (10% e 5%; 90%): as
// duas telas não podem se contradizer na mesma máquina.
const (
	cpuAvgWarn    = 80.0 // média da amostra >= 80%
	memUsePct     = 90   // memória em uso >= 90% do total (disponível <= 10%)
	commitPct     = 90   // commit >= 90% do limite
	pagePct       = 80   // arquivo de paginação em uso >= 80% do alocado
	freeWarnPct   = 10   // espaço livre < 10% do disco do sistema (atenção)
	freeBadPct    = 5    // espaço livre < 5% (problema)
	queueWarn     = 2.0  // fila média do disco >= 2
	latencyWarnMs = 25.0 // latência média por operação >= 25 ms
	startupMany   = 10   // MAIS de 10 itens registrados para iniciar
	servicesMany  = 25   // MAIS de 25 serviços automáticos fora da pasta do Windows
)

// NeverRecommendTerms é a lista fixa (em minúsculas) de termos que NENHUM achado pode conter, em nenhum idioma: o que o
// toolkit nunca recomenda nesta função. O teste de regressão percorre todos os achados nos três idiomas.
var NeverRecommendTerms = []string{
	"defender", "firewall", "smartscreen", "user account control", "controle de conta", "control de cuentas",
	"regedit", "registro do windows", "windows registry", "registro de windows", "limpar o registro", "clean the registry",
	"dism", "telemetria", "telemetry", "hklm", "windows update", "atualização do windows", "actualización de windows",
	"centro de segurança", "security center", "centro de seguridad",
	"pontuação", "puntuación", "score", "otimizador", "optimizer", "optimizador",
}

// neverServiceTerms são palavras que os achados de SERVIÇO não podem usar (nenhum serviço é alterado nem sugerido parar).
var neverServiceTerms = []string{"desativ", "desabilit", "disable", "desactiv", "deshabilit", "stop the", "parar o", "pare o", "detener", "encerr"}

// NeverList é a lista fixa do que o toolkit NUNCA recomenda nesta função, em frases (a mesma de docs/otimizacao.md).
func NeverList(l Lang) []string {
	t := func(pt, en, es string) string { return diagfmt.T(l, pt, en, es) }
	return []string{
		t("Não desativar serviço de segurança nem qualquer outro: nesta função nenhum serviço é alterado.",
			"Never disable a security service or any other: no service is changed by this feature.",
			"No desactivar ningún servicio de seguridad ni ningún otro: esta función no cambia ningún servicio."),
		t("Nada de \"otimizadores\" agressivos: limpar o registro, desligar o arquivo de paginação, o SmartScreen ou o controle de conta, mexer em HKLM, desligar a telemetria.",
			"No aggressive \"optimizers\": registry cleaning, turning off the paging file, SmartScreen or account control, touching HKLM, turning off telemetry.",
			"Nada de \"optimizadores\" agresivos: limpiar el registro, apagar el archivo de paginación, SmartScreen o el control de cuentas, tocar HKLM, apagar la telemetría."),
		t("Não remover componentes do sistema nem do Windows Update, não apagar arquivos do sistema, não usar o dism.",
			"Never remove system or Windows Update components, delete system files or use dism.",
			"No quitar componentes del sistema ni de Windows Update, no borrar archivos del sistema, no usar dism."),
		t("Não encerrar processo, nem desligar item de inicialização ou tarefa agendada: o toolkit só mostra; quem decide é você.",
			"Never end a process or turn off a startup item or scheduled task: the toolkit only shows; you decide.",
			"No cerrar procesos ni apagar elementos de inicio o tareas programadas: el toolkit solo muestra; usted decide."),
		t("Sem \"pontuação de PC\" nem promessa de ganho de desempenho: só números, limiares explícitos e o que fazer em texto.",
			"No \"PC score\" and no performance-gain promise: only numbers, explicit thresholds and what to do as text.",
			"Sin \"puntuación del PC\" ni promesa de mejora de rendimiento: solo números, umbrales explícitos y qué hacer en texto."),
	}
}

// noEventNote é a frase que acompanha o boot "sem evento": o oposto de "rápido".
func noEventNote(l Lang) string {
	return diagfmt.T(l, "Isto NÃO significa que o boot seja rápido: significa que não há medida.",
		"This does NOT mean the boot is fast: it means there is no measurement.",
		"Esto NO significa que el arranque sea rápido: significa que no hay medición.")
}

func gb(b uint64, l Lang) string { return diagfmt.Bytes(b, true, l) }

func pct(f float64, l Lang) string { return diagfmt.Number(f, 0, l) + "%" }

func procList(ps []Process, byMem bool, l Lang) string {
	var parts []string
	for _, p := range ps {
		if byMem {
			parts = append(parts, fmt.Sprintf("%s (%s)", p.Name, gb(p.MemBytes, l)))
		} else {
			parts = append(parts, fmt.Sprintf("%s (%s)", p.Name, pct(p.CPUPct, l)))
		}
	}
	return strings.Join(parts, ", ")
}

func nameList(ss []Service) string {
	var parts []string
	for _, s := range ss {
		parts = append(parts, s.Name)
	}
	return strings.Join(parts, ", ")
}

// Analyze transforma a leitura em achados ("o que está ruim e o que fazer"), do mais grave ao mais leve. O "o que fazer"
// é SEMPRE texto: nada é executado. Fora do Windows (nada lido) devolve vazio.
func Analyze(r *Report, l Lang) []Finding {
	if !r.Supported {
		return nil
	}
	t := func(pt, en, es string) string { return diagfmt.T(l, pt, en, es) }
	var f []Finding
	add := func(area string, sev Severity, title, detail, action string) {
		f = append(f, Finding{Area: area, Severity: sev, Title: title, Detail: detail, Action: action})
	}
	read := 0

	if c := r.CPU; c != nil {
		read++
		if c.AvgPct >= cpuAvgWarn {
			d := fmt.Sprintf("%s %s, %s %s (%s %d %s; %s %s)", t("média", "average", "media"), pct(c.AvgPct, l), t("pico", "peak", "pico"), pct(c.PeakPct, l),
				t("amostra de", "sample of", "muestra de"), c.Samples, t("leituras", "readings", "lecturas"), t("limiar", "threshold", "umbral"), pct(cpuAvgWarn, l))
			if len(c.Top) > 0 {
				d += "; " + t("mais pesados agora: ", "heaviest now: ", "más pesados ahora: ") + procList(c.Top, false, l)
			}
			add("cpu", SevWarn, t("CPU com uso alto", "High CPU usage", "Uso alto de la CPU"), d,
				t("Abra o Gerenciador de Tarefas (Ctrl+Shift+Esc), veja quais programas mais usam a CPU e feche os que você não precisa. É uma amostra curta: se o uso alto for só agora, espere acabar. O toolkit não encerra processos.",
					"Open Task Manager (Ctrl+Shift+Esc), see which programs use the most CPU and close the ones you do not need. It is a short sample: if the high usage is only right now, wait for it to finish. The toolkit does not end processes.",
					"Abra el Administrador de tareas (Ctrl+Mayús+Esc), vea qué programas usan más CPU y cierre los que no necesite. Es una muestra corta: si el uso alto es solo ahora, espere a que termine. El toolkit no cierra procesos."))
		}
	}

	if m := r.Memory; m != nil {
		read++
		if m.TotalBytes > 0 && m.AvailableBytes > 0 && m.AvailableBytes <= m.TotalBytes {
			used := m.TotalBytes - m.AvailableBytes
			if used*100 >= m.TotalBytes*memUsePct {
				d := fmt.Sprintf("%s %s %s (%s %s; %s %d%%)", gb(m.AvailableBytes, l), t("disponíveis de", "available of", "disponibles de"), gb(m.TotalBytes, l),
					t("em uso", "in use", "en uso"), pct(float64(used)*100/float64(m.TotalBytes), l), t("limiar", "threshold", "umbral"), memUsePct)
				if len(m.Top) > 0 {
					d += "; " + t("maiores agora: ", "largest now: ", "mayores ahora: ") + procList(m.Top, true, l)
				}
				add("memoria", SevWarn, t("Pouca memória disponível", "Low available memory", "Poca memoria disponible"), d,
					t("Feche os programas que você não usa (os maiores estão na lista) ou, se isso se repete, amplie a memória do computador.",
						"Close the programs you are not using (the largest are listed) or, if this keeps happening, add more memory to the computer.",
						"Cierre los programas que no usa (los mayores están en la lista) o, si se repite, amplíe la memoria del equipo."))
			}
		}
		if m.CommitLimit > 0 && m.CommitBytes*100 >= m.CommitLimit*commitPct {
			add("memoria", SevWarn, t("Memória virtual (commit) perto do limite", "Virtual memory (commit) near its limit", "Memoria virtual (commit) cerca del límite"),
				fmt.Sprintf("%s %s %s (%s %d%%)", gb(m.CommitBytes, l), t("de", "of", "de"), gb(m.CommitLimit, l), t("limiar", "threshold", "umbral"), commitPct),
				t("Feche programas que não usa ou amplie a memória do computador: com o commit no limite, programas podem falhar.",
					"Close programs you do not use or add more memory to the computer: with commit at its limit, programs can fail.",
					"Cierre programas que no use o amplíe la memoria del equipo: con el commit en el límite, los programas pueden fallar."))
		}
		if m.PageAllocBytes > 0 && m.PageUsedBytes*100 >= m.PageAllocBytes*pagePct {
			add("memoria", SevInfo, t("Arquivo de paginação muito usado", "Paging file heavily used", "Archivo de paginación muy usado"),
				fmt.Sprintf("%s %s %s (%s %d%%)", gb(m.PageUsedBytes, l), t("de", "of", "de"), gb(m.PageAllocBytes, l), t("limiar", "threshold", "umbral"), pagePct),
				t("É um sinal de pouca memória: feche programas ou amplie a memória. O toolkit não mexe no arquivo de paginação.",
					"It is a sign of low memory: close programs or add more memory. The toolkit does not touch the paging file.",
					"Es señal de poca memoria: cierre programas o amplíe la memoria. El toolkit no toca el archivo de paginación."))
		}
	}

	if d := r.Disk; d != nil {
		read++
		name := d.Drive + ":"
		if d.Name != "" {
			name += " (" + d.Name + ")"
		}
		if d.Medium == "HDD" {
			add("disco", SevWarn, t("O disco do sistema é um HDD: ", "The system disk is an HDD: ", "El disco del sistema es un HDD: ")+name,
				t("disco mecânico, bem mais lento que um SSD", "mechanical disk, much slower than an SSD", "disco mecánico, mucho más lento que un SSD"),
				t("Trocar por um SSD melhora a velocidade mais do que qualquer ajuste de software.",
					"Replacing it with an SSD improves speed more than any software tweak.",
					"Cambiarlo por un SSD mejora la velocidad más que cualquier ajuste de software."))
		}
		if d.SizeBytes > 0 && d.FreeBytes != nil && *d.FreeBytes <= d.SizeBytes {
			sev := SevOK
			switch {
			case *d.FreeBytes*100 < d.SizeBytes*freeBadPct:
				sev = SevBad
			case *d.FreeBytes*100 < d.SizeBytes*freeWarnPct:
				sev = SevWarn
			}
			if sev != SevOK {
				add("disco", sev, t("Pouco espaço livre no disco do sistema: ", "Low free space on the system disk: ", "Poco espacio libre en el disco del sistema: ")+name,
					fmt.Sprintf("%s %s %s (%s %d%%; %s %d%%)", gb(*d.FreeBytes, l), t("livres de", "free of", "libres de"), gb(d.SizeBytes, l),
						t("limiar de atenção", "attention threshold", "umbral de atención"), freeWarnPct, t("de problema", "problem", "de problema"), freeBadPct),
					t("Libere espaço (lixeira, downloads, caches e arquivos grandes) ou mova dados para outro disco.",
						"Free up space (trash, downloads, caches, large files) or move data to another disk.",
						"Libere espacio (papelera, descargas, cachés y archivos grandes) o mueva datos a otro disco."))
			}
		}
		if d.QueueAvg != nil && *d.QueueAvg >= queueWarn {
			add("disco", SevWarn, t("Fila alta no disco do sistema: ", "High queue on the system disk: ", "Cola alta en el disco del sistema: ")+name,
				fmt.Sprintf("%s %s (%s %s; %s %s)", t("fila média", "average queue", "cola media"), diagfmt.Number(*d.QueueAvg, 1, l), t("pico", "peak", "pico"), diagfmt.Number(deref(d.QueuePeak), 1, l), t("limiar", "threshold", "umbral"), diagfmt.Number(queueWarn, 1, l)),
				diskAction(l))
		}
		if d.LatencyMs != nil && *d.LatencyMs >= latencyWarnMs {
			add("disco", SevWarn, t("Disco do sistema lento para responder: ", "System disk slow to respond: ", "Disco del sistema lento para responder: ")+name,
				fmt.Sprintf("%s %s ms (%s %s ms)", t("latência média", "average latency", "latencia media"), diagfmt.Number(*d.LatencyMs, 0, l), t("limiar", "threshold", "umbral"), diagfmt.Number(latencyWarnMs, 0, l)),
				diskAction(l))
		}
	}

	if s := r.Startup; s != nil {
		read++
		if s.Total > startupMany {
			add("inicializacao", SevInfo, t("Muitos itens registrados para iniciar com o Windows", "Many items registered to start with Windows", "Muchos elementos registrados para iniciar con Windows"),
				fmt.Sprintf("%d %s (%s %d); %s", s.Total, t("itens", "items", "elementos"), t("limiar: mais de", "threshold: more than", "umbral: más de"), startupMany,
					t("alguns podem já estar desligados em Configurações (o toolkit não lê esse estado) e a fonte não informa o peso de cada um",
						"some may already be turned off in Settings (the toolkit does not read that state) and the source does not report each one's weight",
						"algunos pueden estar ya apagados en Configuración (el toolkit no lee ese estado) y la fuente no informa el peso de cada uno")),
				t("Abra Configurações > Aplicativos > Inicialização e desligue os que você não usa. O toolkit não desliga nada.",
					"Open Settings > Apps > Startup and turn off the ones you do not use. The toolkit does not turn anything off.",
					"Abra Configuración > Aplicaciones > Inicio y apague los que no use. El toolkit no apaga nada."))
		}
	}

	if sv := r.Services; sv != nil {
		read++
		if len(sv.Failed) > 0 {
			add("servicos", SevInfo, t("Serviço automático parado com erro", "Automatic service stopped with an error", "Servicio automático detenido con error"),
				nameList(sv.Failed),
				t("Informativo: confira no Visualizador de Eventos se algo que você usa deixou de funcionar. O toolkit não inicia, não para e não muda serviços.",
					"For information: check Event Viewer to see whether something you use stopped working. The toolkit does not start, stop or change services.",
					"Informativo: revise el Visor de eventos por si algo que usa dejó de funcionar. El toolkit no inicia, no detiene y no cambia servicios."))
		}
		if sv.OutsideCount > servicesMany {
			add("servicos", SevInfo, t("Muitos serviços automáticos fora da pasta do Windows", "Many automatic services outside the Windows folder", "Muchos servicios automáticos fuera de la carpeta de Windows"),
				fmt.Sprintf("%d %s %d %s (%s %d)", sv.OutsideCount, t("de", "of", "de"), sv.AutoCount, t("serviços automáticos (a maioria é de programas instalados)", "automatic services (most belong to installed programs)", "servicios automáticos (la mayoría son de programas instalados)"), t("limiar: mais de", "threshold: more than", "umbral: más de"), servicesMany),
				t("Informativo: o toolkit não altera serviços. Se quiser reduzir, revise os programas instalados em Configurações > Aplicativos e remova os que você não usa.",
					"For information: the toolkit does not change services. To reduce them, review the installed programs in Settings > Apps and remove the ones you do not use.",
					"Informativo: el toolkit no cambia servicios. Si quiere reducirlos, revise los programas instalados en Configuración > Aplicaciones y quite los que no use."))
		}
	}

	if b := r.Boot; b != nil {
		switch {
		case !b.Found:
			add("boot", SevInfo, t("Tempo de boot: sem dado", "Boot time: no data", "Tiempo de arranque: sin dato"),
				t("o log Diagnostics-Performance não tem o evento 100 (o Windows só o registra em certas condições e algumas versões não o geram). ",
					"the Diagnostics-Performance log has no event 100 (Windows only records it under certain conditions and some versions do not generate it). ",
					"el registro Diagnostics-Performance no tiene el evento 100 (Windows solo lo registra en ciertas condiciones y algunas versiones no lo generan). ")+noEventNote(l),
				"")
		case b.Degraded != nil && *b.Degraded:
			d := t("o próprio Windows marcou o último boot como com lentidão (evento 100)", "Windows itself flagged the last boot as slow (event 100)", "el propio Windows marcó el último arranque como lento (evento 100)")
			if b.DurationMs > 0 {
				d += fmt.Sprintf("; %s %s s", t("duração", "duration", "duración"), diagfmt.Number(float64(b.DurationMs)/1000, 0, l))
			}
			add("boot", SevInfo, t("O Windows detectou lentidão no último boot", "Windows detected a slow last boot", "Windows detectó lentitud en el último arranque"), d,
				t("Veja o que atrasa o boot: itens de inicialização (Configurações > Aplicativos > Inicialização) e drivers desatualizados. O toolkit não muda nada disso.",
					"See what slows the boot: startup items (Settings > Apps > Startup) and outdated drivers. The toolkit does not change any of it.",
					"Vea qué retrasa el arranque: elementos de inicio (Configuración > Aplicaciones > Inicio) y controladores desactualizados. El toolkit no cambia nada de eso."))
		}
	}

	if len(f) == 0 {
		if read == 0 {
			add("sistema", SevInfo, t("Nada foi lido", "Nothing was read", "No se leyó nada"), t("veja as áreas não lidas abaixo", "see the unread areas below", "vea las áreas no leídas abajo"), "")
		} else {
			add("sistema", SevOK, t("Nenhum gargalo evidente nas leituras", "No evident bottleneck in the readings", "Ningún cuello de botella evidente en las lecturas"),
				t("isto não é garantia de desempenho: são números de um instante", "this is not a performance guarantee: they are numbers from a moment in time", "esto no es garantía de rendimiento: son números de un instante"), "")
		}
	}
	sort.SliceStable(f, func(i, j int) bool { return f[i].Severity > f[j].Severity })
	return f
}

func deref(p *float64) float64 {
	if p == nil {
		return 0
	}
	return *p
}

func diskAction(l Lang) string {
	return diagfmt.T(l, "O disco está sendo muito usado: veja no Gerenciador de Tarefas (aba Desempenho > Disco) o que lê e grava, e espere acabar (atualizações e verificações costumam causar isso). O toolkit não encerra nada.",
		"The disk is heavily used: check in Task Manager (Performance > Disk) what is reading and writing, and wait for it to finish (updates and scans often cause this). The toolkit does not end anything.",
		"El disco se usa mucho: vea en el Administrador de tareas (Rendimiento > Disco) qué lee y escribe, y espere a que termine (las actualizaciones y los análisis suelen causarlo). El toolkit no cierra nada.")
}

// Legend é a legenda de limiares e a lista do que nunca se faz (painel de detalhe da TUI e docs).
func Legend(l Lang) string {
	t := func(pt, en, es string) string { return diagfmt.T(l, pt, en, es) }
	var b strings.Builder
	b.WriteString(t("Limiares usados", "Thresholds used", "Umbrales usados") + "\n")
	fmt.Fprintf(&b, "- CPU: %s >= %d%%\n", t("média da amostra", "sample average", "media de la muestra"), int(cpuAvgWarn))
	fmt.Fprintf(&b, "- %s: >= %d%% %s\n", t("Memória", "Memory", "Memoria"), memUsePct, t("em uso; commit >= 90% do limite", "in use; commit >= 90% of the limit", "en uso; commit >= 90% del límite"))
	fmt.Fprintf(&b, "- %s: >= %d%% %s\n", t("Paginação", "Paging file", "Paginación"), pagePct, t("do alocado", "of the allocated size", "de lo asignado"))
	fmt.Fprintf(&b, "- %s: < %d%% %s, < %d%% %s\n", t("Espaço livre", "Free space", "Espacio libre"), freeWarnPct, t("atenção", "attention", "atención"), freeBadPct, t("problema", "problem", "problema"))
	fmt.Fprintf(&b, "- %s: %s >= %s; %s >= %d ms\n", t("Disco", "Disk", "Disco"), t("fila", "queue", "cola"), diagfmt.Number(queueWarn, 1, l), t("latência", "latency", "latencia"), int(latencyWarnMs))
	fmt.Fprintf(&b, "- %s: > %d\n", t("Inicialização", "Startup", "Inicio"), startupMany)
	fmt.Fprintf(&b, "- %s: > %d\n", t("Serviços fora da pasta do Windows", "Services outside the Windows folder", "Servicios fuera de la carpeta de Windows"), servicesMany)
	b.WriteString("\n" + t("O que o toolkit nunca faz aqui", "What the toolkit never does here", "Lo que el toolkit nunca hace aquí") + "\n")
	for _, n := range NeverList(l) {
		b.WriteString("- " + n + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

package diagnostico

import (
	"fmt"
	"sort"
	"strings"

	"github.com/PiluTech/toolkit/internal/diagfmt"
)

// Limiares das regras (documentados em docs/diagnostico.md).
const (
	wearWarn, wearBad           = 80, 90 // % de desgaste do SSD
	freeWarn, freeBad           = 0.10, 0.05
	diskTempWarn, diskTempBad   = 65.0, 75.0
	cpuTempWarn, cpuTempBad     = 80.0, 90.0
	batteryWarn, batteryBad     = 60, 40 // % da capacidade original
	memUseWarn                  = 0.90
	swapUseWarn                 = 0.80
	repeatedEvents              = 20
	bsodWarn, bsodBad           = 1, 3
	kernelPowerWarn             = 1
	appCrashRepeated            = 5
	stabilityWarn, stabilityBad = 5.0, 3.0
)

func gb(b uint64, l Lang) string { return diagfmt.Bytes(b, true, l) }

// Analyze transforma a leitura em achados ("o que está ruim e o que fazer"), do mais grave ao mais leve. Sem nenhum
// problema devolve um único achado SevOK. Os textos saem no idioma l.
func Analyze(r *Report, l Lang) []Finding {
	var f []Finding
	add := func(area string, sev Severity, title, detail, action string) {
		f = append(f, Finding{Area: area, Severity: sev, Title: title, Detail: detail, Action: action})
	}
	t := func(pt, en, es string) string { return diagfmt.T(l, pt, en, es) }

	for _, d := range r.Disks {
		name := d.Name
		if name == "" {
			name = t("disco sem nome", "unnamed disk", "disco sin nombre")
		}
		switch d.Health {
		case HealthBad:
			add("disco", SevBad, t("Disco com problema: ", "Failing disk: ", "Disco con problemas: ")+name,
				t("o próprio disco informa defeito (", "the disk itself reports a fault (", "el propio disco informa un fallo (")+d.HealthRaw+")",
				t("Faça backup AGORA dos dados importantes e troque o disco.", "Back up your important data NOW and replace the disk.", "Haga una copia de seguridad AHORA de los datos importantes y cambie el disco."))
		case HealthWarn:
			add("disco", SevWarn, t("Disco em atenção: ", "Disk needs attention: ", "Disco en atención: ")+name,
				t("estado informado: ", "reported state: ", "estado informado: ")+d.HealthRaw,
				t("Faça backup dos dados e acompanhe; planeje a troca do disco.", "Back up your data and keep an eye on it; plan to replace the disk.", "Haga una copia de seguridad y vigílelo; planifique el cambio del disco."))
		}
		if d.WearPct != nil && *d.WearPct >= wearWarn {
			sev := SevWarn
			if *d.WearPct >= wearBad {
				sev = SevBad
			}
			add("disco", sev, t("SSD bastante desgastado: ", "SSD heavily worn: ", "SSD muy desgastado: ")+name,
				fmt.Sprintf("%d%% %s", *d.WearPct, t("da vida útil consumida", "of its lifespan used", "de su vida útil consumida")),
				t("Mantenha backup em dia e planeje a troca do SSD.", "Keep your backups current and plan to replace the SSD.", "Mantenga las copias de seguridad al día y planifique el cambio del SSD."))
		}
		if d.ReallocatedSectors != nil && *d.ReallocatedSectors > 0 {
			sev := SevWarn
			if *d.ReallocatedSectors >= 100 {
				sev = SevBad
			}
			add("disco", sev, t("Setores realocados no disco: ", "Reallocated sectors on the disk: ", "Sectores reasignados en el disco: ")+name,
				fmt.Sprintf("%d", *d.ReallocatedSectors),
				t("O disco já está remapeando áreas defeituosas: faça backup e planeje a troca.", "The disk is already remapping bad areas: back up and plan to replace it.", "El disco ya está remapeando áreas defectuosas: haga una copia de seguridad y planifique el cambio."))
		}
		if d.ReadErrorsUncorrected != nil && *d.ReadErrorsUncorrected > 0 {
			add("disco", SevWarn, t("Erros de leitura não corrigidos no disco: ", "Uncorrected read errors on the disk: ", "Errores de lectura no corregidos en el disco: ")+name,
				fmt.Sprintf("%d", *d.ReadErrorsUncorrected),
				t("O disco falhou em ler dados e não conseguiu corrigir: faça backup e confira o disco com a ferramenta do fabricante.", "The disk failed to read data and could not correct it: back up and check the disk with the manufacturer's tool.", "El disco falló al leer datos y no pudo corregirlos: haga una copia de seguridad y revise el disco con la herramienta del fabricante."))
		}
		if d.SizeBytes > 0 && d.FreeBytes > 0 {
			frac := float64(d.FreeBytes) / float64(d.SizeBytes)
			if frac < freeWarn {
				sev := SevWarn
				if frac < freeBad {
					sev = SevBad
				}
				add("disco", sev, t("Pouco espaço livre: ", "Low free space: ", "Poco espacio libre: ")+name,
					fmt.Sprintf("%s %s %s (%d%%)", gb(d.FreeBytes, l), t("livres de", "free of", "libres de"), gb(d.SizeBytes, l), int(frac*100)),
					t("Libere espaço (lixeira, downloads, caches e arquivos grandes) ou mova dados para outro disco.", "Free up space (trash, downloads, caches, large files) or move data to another disk.", "Libere espacio (papelera, descargas, cachés y archivos grandes) o mueva datos a otro disco."))
			}
		}
		if d.TempC != nil && *d.TempC >= diskTempWarn {
			sev := SevWarn
			if *d.TempC >= diskTempBad {
				sev = SevBad
			}
			add("temperatura", sev, t("Disco quente: ", "Hot disk: ", "Disco caliente: ")+name, fmt.Sprintf("%.0f °C", *d.TempC),
				t("Melhore a ventilação do equipamento e verifique se o disco não está sob uso intenso.", "Improve the device's airflow and check the disk is not under heavy load.", "Mejore la ventilación del equipo y compruebe que el disco no está bajo uso intenso."))
		}
	}

	if b := r.Battery; b != nil {
		if b.HealthPct != nil && *b.HealthPct < batteryWarn {
			sev := SevWarn
			if *b.HealthPct < batteryBad {
				sev = SevBad
			}
			add("bateria", sev, t("Bateria gasta", "Worn battery", "Batería desgastada"),
				fmt.Sprintf("%d%% %s", *b.HealthPct, t("da capacidade original", "of the original capacity", "de la capacidad original")),
				t("Considere trocar a bateria; a autonomia já caiu bastante.", "Consider replacing the battery; its runtime has dropped a lot.", "Considere cambiar la batería; la autonomía ha bajado mucho."))
		}
		switch strings.ToLower(strings.TrimSpace(b.Condition)) {
		case "service recommended", "check battery", "poor":
			add("bateria", SevWarn, t("O sistema recomenda serviço na bateria", "The system recommends battery service", "El sistema recomienda revisar la batería"), b.Condition,
				t("Leve a um serviço autorizado ou troque a bateria.", "Take it to a service center or replace the battery.", "Llévela a un servicio técnico o cambie la batería."))
		case "replace soon", "replace now":
			add("bateria", SevBad, t("O sistema pede troca da bateria", "The system asks for a battery replacement", "El sistema pide cambiar la batería"), b.Condition,
				t("Troque a bateria.", "Replace the battery.", "Cambie la batería."))
		}
	}

	for _, tp := range r.Temps {
		if tp.Celsius >= cpuTempWarn {
			sev := SevWarn
			if tp.Celsius >= cpuTempBad {
				sev = SevBad
			}
			add("temperatura", sev, t("Temperatura alta: ", "High temperature: ", "Temperatura alta: ")+tp.Sensor, fmt.Sprintf("%.0f °C", tp.Celsius),
				t("Limpe as entradas de ar e as ventoinhas, apoie o equipamento em superfície firme e, se persistir, troque a pasta térmica.", "Clean the air vents and fans, keep the device on a hard surface and, if it persists, replace the thermal paste.", "Limpie las rejillas y los ventiladores, apoye el equipo en una superficie firme y, si persiste, cambie la pasta térmica."))
		}
	}

	if m := r.Memory; m.TotalBytes > 0 {
		if m.UsedBytes > 0 && float64(m.UsedBytes)/float64(m.TotalBytes) >= memUseWarn {
			add("memoria", SevWarn, t("Memória quase toda em uso", "Memory almost fully used", "Memoria casi toda en uso"),
				fmt.Sprintf("%s %s %s", gb(m.UsedBytes, l), t("de", "of", "de"), gb(m.TotalBytes, l)),
				t("Feche programas e abas pesados; se for sempre assim, considere mais memória RAM.", "Close heavy programs and tabs; if it is always like this, consider more RAM.", "Cierre programas y pestañas pesados; si siempre es así, considere más memoria RAM."))
		}
		if m.SwapTotalBytes > 0 && float64(m.SwapUsedBytes)/float64(m.SwapTotalBytes) >= swapUseWarn {
			add("memoria", SevWarn, t("Área de troca (swap) quase cheia", "Swap almost full", "Área de intercambio (swap) casi llena"),
				fmt.Sprintf("%s %s %s", gb(m.SwapUsedBytes, l), t("de", "of", "de"), gb(m.SwapTotalBytes, l)),
				t("O sistema está sem memória de verdade: feche programas ou aumente a RAM.", "The system is short on real memory: close programs or add RAM.", "El sistema no tiene memoria real suficiente: cierre programas o amplíe la RAM."))
		}
	}

	for _, e := range r.Events {
		if e.Count >= repeatedEvents {
			id := e.Source
			if e.ID != "" {
				id += " (" + e.ID + ")"
			}
			add("erros", SevWarn, t("Erro repetido no log do sistema: ", "Repeated error in the system log: ", "Error repetido en el registro del sistema: ")+id,
				fmt.Sprintf("%d %s", e.Count, t("vezes no período lido", "times in the period read", "veces en el período leído")),
				t("Veja os detalhes no relatório completo (toolkit diagnostico --completo) e pesquise o código do erro.", "See the details in the full report (toolkit diagnostico --completo) and search for the error code.", "Vea los detalles en el informe completo (toolkit diagnostico --completo) y busque el código del error."))
		}
	}

	var bsod, kpower, panics int
	apps := map[string]int{}
	var bsodCodes []string
	for _, c := range r.Crashes {
		switch c.Kind {
		case CrashBSOD:
			bsod++
			if c.Detail != "" && len(bsodCodes) < 3 {
				bsodCodes = append(bsodCodes, c.Detail)
			}
		case CrashKernelPower:
			kpower++
		case CrashKernelPanic:
			panics++
		case CrashApp:
			if c.Detail != "" {
				apps[c.Detail]++
			}
		}
	}
	if bsod >= bsodWarn {
		sev := SevWarn
		if bsod >= bsodBad {
			sev = SevBad
		}
		detail := fmt.Sprintf("%d %s", bsod, t("nos últimos 30 dias", "in the last 30 days", "en los últimos 30 días"))
		if len(bsodCodes) > 0 {
			detail += " (" + strings.Join(bsodCodes, ", ") + ")"
		}
		add("falhas", sev, t("Tela azul (BSOD)", "Blue screen (BSOD)", "Pantalla azul (BSOD)"), detail,
			t("Atualize o Windows e os drivers, rode \"sfc /scannow\" e o diagnóstico de memória; se repetir, teste a RAM e o disco.", "Update Windows and drivers, run \"sfc /scannow\" and the memory diagnostic; if it repeats, test the RAM and the disk.", "Actualice Windows y los controladores, ejecute \"sfc /scannow\" y el diagnóstico de memoria; si se repite, pruebe la RAM y el disco."))
	}
	if kpower >= kernelPowerWarn {
		add("falhas", SevWarn, t("Desligamento inesperado", "Unexpected shutdown", "Apagado inesperado"),
			fmt.Sprintf("%d %s", kpower, t("vezes nos últimos 30 dias", "times in the last 30 days", "veces en los últimos 30 días")),
			t("Verifique fonte e tomada, superaquecimento e a bateria; desligamentos assim também podem ser a tela azul sem registro.", "Check the power supply and outlet, overheating and the battery; such shutdowns can also be a blue screen with no record.", "Compruebe la fuente y el enchufe, el sobrecalentamiento y la batería; estos apagados también pueden ser una pantalla azul sin registro."))
	}
	if panics > 0 {
		add("falhas", SevBad, t("Falha grave do sistema (kernel panic)", "Serious system failure (kernel panic)", "Fallo grave del sistema (kernel panic)"),
			fmt.Sprintf("%d %s", panics, t("nos últimos 30 dias", "in the last 30 days", "en los últimos 30 días")),
			t("Atualize o sistema e os drivers/extensões, desconecte acessórios novos e teste memória e disco; se repetir, procure assistência.", "Update the system and drivers/extensions, unplug new accessories and test memory and disk; if it repeats, seek service.", "Actualice el sistema y los controladores/extensiones, desconecte accesorios nuevos y pruebe la memoria y el disco; si se repite, busque asistencia."))
	}
	names := make([]string, 0, len(apps))
	for n := range apps {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		if apps[n] >= appCrashRepeated {
			add("falhas", SevWarn, t("Programa que trava sempre: ", "Program that keeps crashing: ", "Programa que se cierra siempre: ")+n,
				fmt.Sprintf("%d %s", apps[n], t("falhas nos últimos 30 dias", "crashes in the last 30 days", "fallos en los últimos 30 días")),
				t("Atualize ou reinstale o programa; se for um driver ou serviço, atualize-o.", "Update or reinstall the program; if it is a driver or service, update it.", "Actualice o reinstale el programa; si es un controlador o servicio, actualícelo."))
		}
	}

	if r.Stability != nil && *r.Stability < stabilityWarn {
		sev := SevWarn
		if *r.Stability < stabilityBad {
			sev = SevBad
		}
		add("sistema", sev, t("Índice de confiabilidade do Windows baixo", "Low Windows reliability index", "Índice de fiabilidad de Windows bajo"),
			fmt.Sprintf("%s/10", diagfmt.Number(*r.Stability, 1, l)),
			t("Abra o Monitor de Confiabilidade (perfmon /rel) para ver quais falhas derrubaram a nota.", "Open the Reliability Monitor (perfmon /rel) to see which failures lowered the score.", "Abra el Monitor de fiabilidad (perfmon /rel) para ver qué fallos bajaron la nota."))
	}

	for _, s := range r.Skipped {
		add("sistema", SevInfo, t("Não foi possível ler: ", "Could not read: ", "No se pudo leer: ")+areaName(s.Area, l), s.Reason, "")
	}

	if len(f) == 0 || onlyInfo(f) {
		f = append([]Finding{{Area: "sistema", Severity: SevOK, Title: t("Nada de ruim encontrado", "Nothing bad found", "No se encontró nada malo"),
			Detail: t("nenhum problema nas áreas lidas", "no problems in the areas that were read", "ningún problema en las áreas leídas")}}, f...)
	}
	sort.SliceStable(f, func(i, j int) bool { return f[i].Severity > f[j].Severity })
	if f[len(f)-1].Severity == SevOK && onlyInfo(f) { // sem problema: o "tudo ok" vem antes das informações
		f = append([]Finding{f[len(f)-1]}, f[:len(f)-1]...)
	}
	return f
}

func onlyInfo(f []Finding) bool {
	for _, x := range f {
		if x.Severity >= SevWarn {
			return false
		}
	}
	return true
}

// areaName traduz o id estável de uma área não lida (Skipped.Area) para o idioma da saída; id desconhecido sai como veio.
func areaName(id string, l Lang) string {
	t := func(pt, en, es string) string { return diagfmt.T(l, pt, en, es) }
	switch id {
	case "hardware":
		return t("hardware", "hardware", "hardware")
	case "memoria":
		return t("memória", "memory", "memoria")
	case "discos":
		return t("discos", "disks", "discos")
	case "smart":
		return t("saúde SMART dos discos", "disk SMART health", "salud SMART de los discos")
	case "bateria":
		return t("bateria", "battery", "batería")
	case "temperatura":
		return t("temperatura", "temperature", "temperatura")
	case "gpu":
		return "GPU"
	case "eventos":
		return t("erros do log do sistema", "system log errors", "errores del registro del sistema")
	case "falhas":
		return t("falhas graves", "serious failures", "fallos graves")
	case "estabilidade":
		return t("índice de confiabilidade", "reliability index", "índice de fiabilidad")
	}
	return id
}

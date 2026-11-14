package privacidade

import "github.com/PiluTech/toolkit/internal/diagfmt"

// Lang é o idioma da saída (o mesmo do diagfmt).
type Lang = diagfmt.Lang

// Perfil é o conjunto de ajustes que se aplica.
type Perfil string

// Perfis.
const (
	// PerfilBasico só mexe no que é do USUÁRIO (HKCU): ID de publicidade, experiências personalizadas, sugestões e anúncios do
	// Iniciar, dicas e pedido de feedback. Não exige administrador.
	PerfilBasico Perfil = "basico"
	// PerfilRecomendado é o básico mais a política de dados de diagnóstico, o histórico de atividades, o serviço
	// DiagTrack e as tarefas agendadas de coleta. Exige administrador (só na hora de aplicar).
	PerfilRecomendado Perfil = "recomendado"
)

// RegSpec é um valor de registro a definir. Value recebe a edição do Windows (alguns valores dependem dela).
type RegSpec struct {
	Key, Name string
	Value     func(edition string) RegValue
}

func fixed(n uint32) func(string) RegValue { return func(string) RegValue { return DWORD(n) } }

// ServiceSpec é um serviço a desativar e parar.
type ServiceSpec struct{ Name string }

// TaskSpec é uma tarefa agendada a desabilitar (caminho completo).
type TaskSpec struct{ Path string }

// Setting é um ajuste (um item do plano, com o que ele faz e o que muda).
type Setting struct {
	ID    string
	Admin bool // exige administrador (HKLM, serviços ou tarefas)
	Title func(Lang) string
	Why   func(Lang) string
	Regs  []RegSpec
	Svcs  []ServiceSpec
	Tasks []TaskSpec
}

func tr(pt, en, es string) func(Lang) string {
	return func(l Lang) string { return diagfmt.T(l, pt, en, es) }
}

const (
	hkcuCDM    = `HKCU\Software\Microsoft\Windows\CurrentVersion\ContentDeliveryManager`
	hklmDataC  = `HKLM\SOFTWARE\Policies\Microsoft\Windows\DataCollection`
	hklmSystem = `HKLM\SOFTWARE\Policies\Microsoft\Windows\System`
)

// Settings devolve TODOS os ajustes (o perfil escolhe quais valem). O catálogo é fixo e revisável: só valores de
// registro, serviços e tarefas de telemetria documentados; nada de arquivo hosts, nada de remover componente do sistema,
// nenhum serviço que não seja de coleta.
func Settings() []Setting {
	return []Setting{
		{ID: "id-publicidade",
			Title: tr("ID de publicidade desligado", "Advertising ID off", "ID de publicidad desactivado"),
			Why:   tr("Os apps deixam de usar um identificador seu para anúncios personalizados.", "Apps stop using an identifier of yours for personalized ads.", "Las apps dejan de usar un identificador suyo para anuncios personalizados."),
			Regs:  []RegSpec{{`HKCU\Software\Microsoft\Windows\CurrentVersion\AdvertisingInfo`, "Enabled", fixed(0)}}},
		{ID: "experiencias-personalizadas",
			Title: tr("Experiências personalizadas com dados de diagnóstico desligadas", "Tailored experiences with diagnostic data off", "Experiencias personalizadas con datos de diagnóstico desactivadas"),
			Why:   tr("A Microsoft não usa seus dados de diagnóstico para sugerir dicas, anúncios e recomendações.", "Microsoft does not use your diagnostic data to suggest tips, ads and recommendations.", "Microsoft no usa sus datos de diagnóstico para sugerir consejos, anuncios y recomendaciones."),
			Regs:  []RegSpec{{`HKCU\Software\Microsoft\Windows\CurrentVersion\Privacy`, "TailoredExperiencesWithDiagnosticDataEnabled", fixed(0)}}},
		{ID: "sugestoes-iniciar",
			Title: tr("Sugestões e anúncios no menu Iniciar desligados", "Suggestions and ads in the Start menu off", "Sugerencias y anuncios en el menú Inicio desactivados"),
			Why:   tr("O Iniciar para de mostrar apps sugeridos e conteúdo patrocinado.", "Start stops showing suggested apps and sponsored content.", "Inicio deja de mostrar apps sugeridas y contenido patrocinado."),
			Regs: []RegSpec{
				{hkcuCDM, "SubscribedContent-338388Enabled", fixed(0)}, {hkcuCDM, "SubscribedContent-338389Enabled", fixed(0)},
				{hkcuCDM, "SubscribedContent-353694Enabled", fixed(0)}, {hkcuCDM, "SubscribedContent-353696Enabled", fixed(0)},
				{hkcuCDM, "SystemPaneSuggestionsEnabled", fixed(0)}}},
		{ID: "apps-sugeridos",
			Title: tr("Instalação automática de apps sugeridos desligada", "Automatic installation of suggested apps off", "Instalación automática de apps sugeridas desactivada"),
			Why:   tr("O Windows deixa de instalar sozinho apps patrocinados e pré-instalados do fabricante.", "Windows stops silently installing sponsored and OEM pre-installed apps.", "Windows deja de instalar por sí solo apps patrocinadas y preinstaladas del fabricante."),
			Regs: []RegSpec{
				{hkcuCDM, "ContentDeliveryAllowed", fixed(0)}, {hkcuCDM, "SilentInstalledAppsEnabled", fixed(0)},
				{hkcuCDM, "OemPreInstalledAppsEnabled", fixed(0)}, {hkcuCDM, "PreInstalledAppsEnabled", fixed(0)}}},
		{ID: "dicas-boas-vindas",
			Title: tr("Dicas, truques e boas-vindas do Windows desligados", "Windows tips, tricks and welcome experience off", "Consejos, trucos y bienvenida de Windows desactivados"),
			Why:   tr("Menos notificações de \"dicas\" e a tela de boas-vindas depois das atualizações.", "Fewer \"tips\" notifications and no welcome screen after updates.", "Menos notificaciones de \"consejos\" y sin pantalla de bienvenida tras las actualizaciones."),
			Regs: []RegSpec{
				{hkcuCDM, "SoftLandingEnabled", fixed(0)}, {hkcuCDM, "SubscribedContent-310093Enabled", fixed(0)},
				{hkcuCDM, "SubscribedContent-338387Enabled", fixed(0)}}},
		{ID: "feedback",
			Title: tr("Pedidos de feedback desligados", "Feedback requests off", "Solicitudes de opinión desactivadas"),
			Why:   tr("O Windows deixa de pedir sua opinião com frequência.", "Windows stops asking for your feedback.", "Windows deja de pedir su opinión."),
			Regs:  []RegSpec{{`HKCU\Software\Microsoft\Windows\CurrentVersion\Siuf\Rules`, "NumberOfSIUFInPeriod", fixed(0)}}},

		{ID: "dados-diagnostico", Admin: true,
			Title: tr("Dados de diagnóstico no mínimo permitido", "Diagnostic data at the minimum allowed", "Datos de diagnóstico al mínimo permitido"),
			Why: tr("Política de coleta: no Home e no Pro o mínimo possível é \"obrigatório\" (valor 1; o Windows ignora 0 nessas edições); no Enterprise e no Education vai a \"segurança\" (0). Também desliga as notificações de feedback e limita logs e dumps de diagnóstico.",
				"Collection policy: on Home and Pro the minimum possible is \"required\" (value 1; Windows ignores 0 on these editions); on Enterprise and Education it goes to \"security\" (0). It also turns off feedback notifications and limits diagnostic logs and dumps.",
				"Política de recopilación: en Home y Pro el mínimo posible es \"obligatorio\" (valor 1; Windows ignora 0 en estas ediciones); en Enterprise y Education pasa a \"seguridad\" (0). También desactiva las notificaciones de opinión y limita los registros y volcados de diagnóstico."),
			Regs: []RegSpec{
				{hklmDataC, "AllowTelemetry", func(ed string) RegValue {
					if ed == EditionEnterprise || ed == EditionEducation {
						return DWORD(0)
					}
					return DWORD(1)
				}},
				{hklmDataC, "DoNotShowFeedbackNotifications", fixed(1)}, {hklmDataC, "LimitDiagnosticLogCollection", fixed(1)},
				{hklmDataC, "LimitDumpCollection", fixed(1)}}},
		{ID: "historico-atividades", Admin: true,
			Title: tr("Histórico de atividades desligado", "Activity history off", "Historial de actividades desactivado"),
			Why:   tr("O Windows deixa de registrar e de enviar à Microsoft o histórico do que você abre e faz.", "Windows stops recording and sending to Microsoft the history of what you open and do.", "Windows deja de registrar y de enviar a Microsoft el historial de lo que abre y hace."),
			Regs: []RegSpec{
				{hklmSystem, "EnableActivityFeed", fixed(0)}, {hklmSystem, "PublishUserActivities", fixed(0)}, {hklmSystem, "UploadUserActivities", fixed(0)}}},
		{ID: "servicos-telemetria", Admin: true,
			Title: tr("Serviço de telemetria desativado (DiagTrack)", "Telemetry service disabled (DiagTrack)", "Servicio de telemetría desactivado (DiagTrack)"),
			Why:   tr("\"Experiências do Usuário Conectado e Telemetria\" (DiagTrack): fica parado e desativado. O dmwappushservice não é mexido: desativá-lo quebra a sincronização com Intune/MDM.", "\"Connected User Experiences and Telemetry\" (DiagTrack): stopped and disabled. dmwappushservice is left alone: disabling it breaks Intune/MDM sync.", "\"Experiencias del usuario conectado y telemetría\" (DiagTrack): detenido y desactivado. dmwappushservice no se toca: desactivarlo rompe la sincronización con Intune/MDM."),
			Svcs:  []ServiceSpec{{"DiagTrack"}}},
		{ID: "tarefas-coleta", Admin: true,
			Title: tr("Tarefas agendadas de coleta desabilitadas", "Collection scheduled tasks disabled", "Tareas programadas de recopilación deshabilitadas"),
			Why:   tr("Compatibilidade de aplicativos (Appraiser, ProgramDataUpdater), Programa de Aperfeiçoamento da Experiência do Cliente e coletor de diagnóstico de disco.", "Application compatibility (Appraiser, ProgramDataUpdater), the Customer Experience Improvement Program and the disk diagnostic collector.", "Compatibilidad de aplicaciones (Appraiser, ProgramDataUpdater), el Programa para la mejora de la experiencia del cliente y el recopilador de diagnóstico de disco."),
			Tasks: []TaskSpec{
				{`\Microsoft\Windows\Application Experience\Microsoft Compatibility Appraiser`},
				{`\Microsoft\Windows\Application Experience\ProgramDataUpdater`},
				{`\Microsoft\Windows\Customer Experience Improvement Program\Consolidator`},
				{`\Microsoft\Windows\Customer Experience Improvement Program\UsbCeip`},
				{`\Microsoft\Windows\Customer Experience Improvement Program\KernelCeipTask`},
				{`\Microsoft\Windows\DiskDiagnostic\Microsoft-Windows-DiskDiagnosticDataCollector`},
				{`\Microsoft\Windows\Autochk\Proxy`}}},
	}
}

// ParaPerfil devolve os ajustes do perfil: o básico só os que não exigem administrador; o recomendado, todos.
func ParaPerfil(p Perfil) []Setting {
	var out []Setting
	for _, s := range Settings() {
		if p == PerfilRecomendado || !s.Admin {
			out = append(out, s)
		}
	}
	return out
}

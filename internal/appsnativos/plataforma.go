package appsnativos

import "github.com/PiluTech/toolkit/internal/diagfmt"

// UnsupportedMessage devolve a explicação de por que `apps-nativos` não faz nada neste SO (goos = runtime.GOOS), ou ""
// no Windows. O chamador a imprime e sai com código de erro, sem tocar em nada.
func UnsupportedMessage(goos string, l Lang) string {
	t := func(pt, en, es string) string { return diagfmt.T(l, pt, en, es) }
	switch goos {
	case "windows":
		return ""
	case "darwin":
		return t("No macOS os apps do sistema ficam em um volume protegido (somente leitura) e o toolkit não os remove. Nada foi alterado.",
			"On macOS the system apps live on a protected (read-only) volume and the toolkit does not remove them. Nothing was changed.",
			"En macOS las apps del sistema están en un volumen protegido (solo lectura) y el toolkit no las elimina. No se cambió nada.")
	case "linux":
		return t("No Linux esta versão não remove pacotes da distribuição. Nada foi alterado.",
			"On Linux this version does not remove distribution packages. Nothing was changed.",
			"En Linux esta versión no elimina paquetes de la distribución. No se cambió nada.")
	}
	return t("Este comando só existe no Windows. Nada foi alterado.", "This command only exists on Windows. Nothing was changed.", "Este comando solo existe en Windows. No se cambió nada.")
}

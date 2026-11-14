package privacidade

import (
	"strings"
	"testing"

	"github.com/PiluTech/toolkit/internal/diagfmt"
)

func TestCatalogoSoChavesEsperadas(t *testing.T) {
	seen := map[string]bool{}
	for _, s := range Settings() {
		if s.ID == "" || seen[s.ID] {
			t.Fatalf("id vazio ou repetido: %q", s.ID)
		}
		seen[s.ID] = true
		if s.Title(diagfmt.PT) == "" || s.Why(diagfmt.PT) == "" || s.Title(diagfmt.EN) == "" || s.Why(diagfmt.ES) == "" {
			t.Errorf("%s: texto vazio em algum idioma", s.ID)
		}
		if len(s.Regs)+len(s.Svcs)+len(s.Tasks) == 0 {
			t.Errorf("%s: ajuste vazio", s.ID)
		}
		hasHKLM := len(s.Svcs) > 0 || len(s.Tasks) > 0
		for _, r := range s.Regs {
			switch {
			case strings.HasPrefix(r.Key, `HKCU\Software\Microsoft\Windows\CurrentVersion\`):
			case strings.HasPrefix(r.Key, `HKLM\SOFTWARE\Policies\Microsoft\Windows\`):
				hasHKLM = true
			default:
				t.Errorf("%s: chave fora da lista permitida: %s", s.ID, r.Key)
			}
			if isHKLM(r.Key) != strings.HasPrefix(r.Key, "HKLM") {
				t.Errorf("isHKLM errado para %s", r.Key)
			}
			if r.Name == "" || r.Value == nil {
				t.Errorf("%s: valor sem nome ou sem função", s.ID)
			}
		}
		// Admin tem de bater com o que o ajuste realmente toca.
		if s.Admin != hasHKLM {
			t.Errorf("%s: Admin=%v mas hasHKLM=%v", s.ID, s.Admin, hasHKLM)
		}
	}
}

func TestCatalogoServicosETarefasPermitidos(t *testing.T) {
	allowedSvc := map[string]bool{"DiagTrack": true, "dmwappushservice": true}
	for _, s := range Settings() {
		for _, sv := range s.Svcs {
			if !allowedSvc[sv.Name] {
				t.Errorf("serviço fora da lista de telemetria: %s", sv.Name)
			}
		}
		for _, tk := range s.Tasks {
			if !strings.HasPrefix(tk.Path, `\Microsoft\Windows\`) {
				t.Errorf("tarefa fora de \\Microsoft\\Windows: %s", tk.Path)
			}
			if strings.HasSuffix(tk.Path, `\`) {
				t.Errorf("caminho de pasta, não de tarefa: %s", tk.Path)
			}
		}
	}
}

func TestCatalogoNadaDeHostsNemRemocao(t *testing.T) {
	for _, s := range Settings() {
		blob := strings.ToLower(s.ID)
		for _, r := range s.Regs {
			blob += " " + strings.ToLower(r.Key+r.Name)
		}
		for _, sv := range s.Svcs {
			blob += " " + strings.ToLower(sv.Name)
		}
		for _, tk := range s.Tasks {
			blob += " " + strings.ToLower(tk.Path)
		}
		for _, bad := range []string{"hosts", "etc", "appx", "remove", "uninstall", "defender", "windowsupdate", "wuauserv"} {
			if strings.Contains(blob, bad) {
				t.Errorf("%s: contém %q", s.ID, bad)
			}
		}
	}
}

func TestCatalogoValoresDosRegistros(t *testing.T) {
	for _, s := range Settings() {
		for _, r := range s.Regs {
			for _, ed := range []string{EditionHome, EditionPro, EditionEnterprise, EditionEducation, EditionOther} {
				v := r.Value(ed)
				if v.Kind != RegDWORD || v.DWORD > 1 {
					t.Errorf("%s\\%s (%s): esperado DWORD 0 ou 1, veio %+v", r.Key, r.Name, ed, v)
				}
			}
		}
	}
}

func allowTelemetry(t *testing.T) RegSpec {
	t.Helper()
	for _, s := range Settings() {
		for _, r := range s.Regs {
			if r.Name == "AllowTelemetry" {
				return r
			}
		}
	}
	t.Fatal("AllowTelemetry não está no catálogo")
	return RegSpec{}
}

func TestAllowTelemetryPorEdicao(t *testing.T) {
	r := allowTelemetry(t)
	if r.Key != `HKLM\SOFTWARE\Policies\Microsoft\Windows\DataCollection` {
		t.Fatalf("chave: %s", r.Key)
	}
	want := map[string]uint32{EditionHome: 1, EditionPro: 1, EditionEnterprise: 0, EditionEducation: 0, EditionOther: 1}
	for ed, n := range want {
		if got := r.Value(ed); got != DWORD(n) {
			t.Errorf("%s: AllowTelemetry=%+v, esperado %d", ed, got, n)
		}
	}
}

func TestPerfilBasicoSoUsuario(t *testing.T) {
	b := ParaPerfil(PerfilBasico)
	if len(b) == 0 {
		t.Fatal("perfil básico vazio")
	}
	for _, s := range b {
		if s.Admin || len(s.Svcs) > 0 || len(s.Tasks) > 0 {
			t.Errorf("%s no básico exige admin/serviço/tarefa", s.ID)
		}
		for _, r := range s.Regs {
			if isHKLM(r.Key) {
				t.Errorf("%s no básico toca HKLM: %s", s.ID, r.Key)
			}
		}
	}
}

func TestPerfilRecomendadoEhSuperconjuntoDoBasico(t *testing.T) {
	ids := func(ss []Setting) map[string]bool {
		m := map[string]bool{}
		for _, s := range ss {
			m[s.ID] = true
		}
		return m
	}
	b, r := ids(ParaPerfil(PerfilBasico)), ids(ParaPerfil(PerfilRecomendado))
	for id := range b {
		if !r[id] {
			t.Errorf("%s está no básico mas não no recomendado", id)
		}
	}
	if len(r) != len(Settings()) {
		t.Errorf("recomendado tem %d, catálogo %d", len(r), len(Settings()))
	}
	for _, id := range []string{"dados-diagnostico", "historico-atividades", "servicos-telemetria", "tarefas-coleta"} {
		if !r[id] || b[id] {
			t.Errorf("%s deveria estar só no recomendado", id)
		}
	}
}

func TestParaPerfilDesconhecidoEhComoBasico(t *testing.T) {
	if got := len(ParaPerfil("xyz")); got != len(ParaPerfil(PerfilBasico)) {
		t.Errorf("perfil desconhecido devolveu %d ajustes", got)
	}
}

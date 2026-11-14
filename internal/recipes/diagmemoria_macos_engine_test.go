package recipes

import (
	"context"
	"strings"
	"testing"

	toolkit "github.com/PiluTech/toolkit"
	"github.com/PiluTech/toolkit/internal/exec/exectest"
	"github.com/PiluTech/toolkit/internal/platform"
)

// Lupa #56, ponta da engine: o sh da receita com system_profiler válido e diskutil ausente/exit 1 sai com status 0 e a
// seção "### apfs" vazia (provado com o sh real em diagmemoria_macos_test.go); a engine então resume só com o profiler:
// a etapa Discos NÃO falha, o interno e o externo do mesmo modelo continuam dois discos (nada se junta por modelo) e a
// saída crua não aparece (sem --detalhes).
func TestMacDiscosSemMapaAPFSNaEngine(t *testing.T) {
	set, err := LoadFS(toolkit.Recipes, "recipes")
	if err != nil {
		t.Fatal(err)
	}
	r, ok := set.Get(platform.Darwin, "diag-memoria")
	if !ok {
		t.Fatal("receita ausente")
	}
	var discos Step
	for _, st := range r.Steps {
		if st.ID == "discos" {
			discos = st
		}
	}
	only := *r // só a etapa Discos (as outras precisariam de roteiro próprio no runner falso)
	only.Steps = []Step{discos}
	profiler := `{"SPNVMeDataType":[{"_name":"c","_items":[{"_name":"SSD X1","device_model":"SSD X1","bsd_name":"disk0","size_in_bytes":500277792768,"smart_status":"Verified","device_serial":"SN-INTERNO"}]}],"SPStorageDataType":[{"_name":"Data","bsd_name":"disk3s5","physical_drive":{"device_name":"SSD X1","medium_type":"ssd","protocol":"Apple Fabric","smart_status":"Verified"}},{"_name":"Backup","bsd_name":"disk6s2","physical_drive":{"device_name":"SSD X1","medium_type":"ssd","protocol":"USB","smart_status":"Not Supported"}}]}`
	for name, stdout := range map[string]string{
		"diskutil ausente ou exit 1": "### profiler\n" + profiler + "\n### apfs\n",
	} {
		e := newEnv(t, platform.Info{OS: platform.Darwin, User: "ana", BootID: "b", IsAdmin: false})
		e.eng.Getenv = envMap(nil)
		e.recipes[r.ID] = &only
		e.run.On(sh(discos.Run.Sh), exectest.Exit(0, stdout))
		rep, err := e.eng.Run(context.Background(), &only, RunOptions{})
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		var st StepReport
		for _, s := range rep.Steps {
			if s.ID == "discos" {
				st = s
			}
		}
		if st.Outcome != OutcomeDone {
			t.Fatalf("%s: a etapa Discos não pode falhar sem o mapa APFS: %+v", name, st)
		}
		if strings.Count(st.Output, "Disco: SSD X1") != 3 || !strings.Contains(st.Output, "verificado") || !strings.Contains(st.Output, "não suportado") {
			t.Errorf("%s: sem o mapa APFS nada se junta por modelo (interno, volume do Storage e externo ficam separados):\n%s", name, st.Output)
		}
		for _, raw := range []string{"SPNVMeDataType", "SN-INTERNO", "bsd_name", "###"} {
			if strings.Contains(st.Output, raw) {
				t.Errorf("%s: sem --detalhes a saída crua não aparece (%q):\n%s", name, raw, st.Output)
			}
		}
	}
}

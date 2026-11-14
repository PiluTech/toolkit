package recipes_test

import (
	"runtime"
	"strings"
	"testing"

	toolkit "github.com/PiluTech/toolkit"
	"github.com/PiluTech/toolkit/internal/platform"
	"github.com/PiluTech/toolkit/internal/recipes"
)

// Lupa #56: o mapa APFS (diskutil apfs list) é complemento OPCIONAL da etapa Discos do macOS. Roda o sh REAL da receita
// com ferramentas falsas: com o system_profiler válido e o diskutil ausente ou saindo com 1, a etapa termina com
// status 0 e a seção "### apfs" vazia; a falha do coletor principal (system_profiler) continua sendo a falha da etapa.
const macProfilerJSON = `printf '%s' '{"SPNVMeDataType":[{"_name":"c","_items":[{"_name":"SSD X1","device_model":"SSD X1","bsd_name":"disk0","size_in_bytes":500277792768,"smart_status":"Verified","device_serial":"SN-INTERNO"}]}],"SPStorageDataType":[{"_name":"Data","bsd_name":"disk3s5","physical_drive":{"device_name":"SSD X1","medium_type":"ssd","protocol":"Apple Fabric","smart_status":"Verified"}},{"_name":"Backup","bsd_name":"disk6s2","physical_drive":{"device_name":"SSD X1","medium_type":"ssd","protocol":"USB","smart_status":"Not Supported"}}]}'`

func macDiscosStep(t *testing.T) recipes.Step {
	t.Helper()
	set, err := recipes.LoadFS(toolkit.Recipes, "recipes")
	if err != nil {
		t.Fatal(err)
	}
	r, ok := set.Get(platform.Darwin, "diag-memoria")
	if !ok {
		t.Fatal("diag-memoria do macOS ausente")
	}
	for _, st := range r.Steps {
		if st.ID == "discos" {
			return st
		}
	}
	t.Fatal("etapa discos ausente")
	return recipes.Step{}
}

func TestMacDiscosAPFSOpcionalNoSh(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("sh")
	}
	st := macDiscosStep(t)
	cases := []struct {
		name  string
		fakes map[string]string
		code  int
		apfs  bool // a seção apfs tem conteúdo
	}{
		{"diskutil ausente", map[string]string{"system_profiler": macProfilerJSON}, 0, false},
		{"diskutil sai com 1", map[string]string{"system_profiler": macProfilerJSON, "diskutil": `exit 1`}, 0, false},
		{"diskutil ok", map[string]string{"system_profiler": macProfilerJSON, "diskutil": `echo '<plist version="1.0"><dict></dict></plist>'`}, 0, true},
		{"coletor principal falha", map[string]string{"system_profiler": `exit 3`, "diskutil": `echo ok`}, 3, true},
	}
	for _, c := range cases {
		f := newToolbox(t, c.fakes)
		out, _, code := f.runStep(t, st.Run.Sh)
		if code != c.code {
			t.Errorf("%s: status %d, quer %d:\n%s", c.name, code, c.code, out)
		}
		_, after, _ := strings.Cut(out, "### apfs")
		if got := strings.TrimSpace(after) != ""; got != c.apfs {
			t.Errorf("%s: seção apfs com conteúdo = %v, quer %v:\n%s", c.name, got, c.apfs, out)
		}
	}
}

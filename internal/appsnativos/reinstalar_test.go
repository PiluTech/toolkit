package appsnativos

import (
	"errors"
	"strings"
	"testing"
)

func errorsIs(err, target error) bool { return errors.Is(err, target) }

func bkWith(es ...Entry) *memStore {
	b := &Backup{Version: 1, Entries: map[string]Entry{}}
	for _, e := range es {
		b.Entries[e.ID] = e
	}
	return &memStore{b: b}
}

func TestReinstalarSoImprimeTexto(t *testing.T) {
	rp, err := BuildReinstall(bkWith(goodEntry()))
	if err != nil {
		t.Fatal(err)
	}
	out := rp.Format(0)
	for _, w := range []string{"MSN Clima", "Microsoft Store", "Add-AppxPackage -RegisterByFamilyName -MainPackage Microsoft.BingWeather_8wekyb3d8bbwe", "não executa"} {
		if !strings.Contains(out, w) {
			t.Errorf("faltou %q:\n%s", w, out)
		}
	}
	if strings.Contains(out, "winget") {
		t.Errorf("sem StoreID conhecido o texto não cita winget:\n%s", out)
	}
	for l, w := range map[Lang]string{1: "does not run", 2: "no ejecuta"} {
		if !strings.Contains(rp.Format(l), w) {
			t.Errorf("idioma %d sem %q", l, w)
		}
	}
}

func TestReinstalarComStoreIDCitaWinget(t *testing.T) {
	old := catalogo[0].StoreID
	catalogo[0].StoreID = "9WZDNCRFJ3Q2"
	defer func() { catalogo[0].StoreID = old }()
	rp, err := BuildReinstall(bkWith(goodEntry()))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(rp.Format(0), "winget install --id 9WZDNCRFJ3Q2 --source msstore") {
		t.Errorf("\n%s", rp.Format(0))
	}
}

func TestReinstalarEscopoTodosNaoOfereceRegistrar(t *testing.T) {
	e := goodEntry()
	e.Scope = "todos"
	rp, err := BuildReinstall(bkWith(e))
	if err != nil {
		t.Fatal(err)
	}
	if out := rp.Format(0); strings.Contains(out, "Add-AppxPackage") || !strings.Contains(out, "Microsoft Store") {
		t.Errorf("com o provisionamento removido só resta a Store:\n%s", out)
	}
}

func TestReinstalarBackupAdulteradoNaoImprimeNada(t *testing.T) {
	e := goodEntry()
	e.Name, e.PackageFullName = "Microsoft.WindowsStore", "Microsoft.WindowsStore_1.0.0.0_x64__8wekyb3d8bbwe"
	e.ID = "package:" + e.PackageFullName
	rp, err := BuildReinstall(bkWith(e))
	if !errors.Is(err, ErrBackupInvalido) || rp != nil {
		t.Errorf("rp=%v err=%v", rp, err)
	}
}

func TestReinstalarSemBackupELimpaTexto(t *testing.T) {
	if _, err := BuildReinstall(&memStore{}); !errors.Is(err, ErrNoBackup) {
		t.Errorf("%v", err)
	}
	if _, err := BuildReinstall(bkWith()); !errors.Is(err, ErrNoBackup) {
		t.Errorf("vazio: %v", err)
	}
	e := goodEntry()
	e.Publisher = "CN=\x1b[31mX"
	if rp, err := BuildReinstall(bkWith(e)); !errors.Is(err, ErrBackupInvalido) || rp != nil || (err != nil && strings.Contains(err.Error(), "\x1b")) {
		t.Errorf("controle no backup é adulteração: %v", err)
	}
}

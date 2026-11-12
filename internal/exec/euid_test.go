package exec

import (
	"strings"
	"testing"
)

// euid() decide se o Elevate usa sudo: root não usa, não-root usa, e com GOOS injetado e sem Euid é não-root.
func TestOSRunnerEuidDecidesSudo(t *testing.T) {
	elevated := Cmd{Name: "apt-get", Args: []string{"install", "-y", "git"}, Elevate: true, Env: []string{"LC_ALL=C"}}
	cases := []struct {
		name     string
		r        OSRunner
		wantSudo bool
		wantLine string
	}{
		{"root não usa sudo", OSRunner{GOOS: "linux", Euid: func() int { return 0 }, IsElevated: func() bool { return true }}, false, "LC_ALL=C apt-get install -y git"},
		{"não-root usa sudo", OSRunner{GOOS: "linux", Euid: func() int { return 1000 }}, true, "sudo -n -- env LC_ALL=C apt-get install -y git"},
		{"GOOS injetado sem Euid é não-root", OSRunner{GOOS: "linux"}, true, "sudo -n -- env LC_ALL=C apt-get install -y git"},
		{"Windows nunca usa sudo", OSRunner{GOOS: "windows", Euid: func() int { return 1000 }, IsElevated: func() bool { return true }}, false, "LC_ALL=C apt-get install -y git"},
	}
	for _, c := range cases {
		c.r.IsElevated = orTrue(c.r.IsElevated)
		if got := c.r.UsesSudo(elevated); got != c.wantSudo {
			t.Errorf("%s: UsesSudo = %v, quero %v", c.name, got, c.wantSudo)
		}
		if got := c.r.Display(elevated); got != c.wantLine {
			t.Errorf("%s: Display = %q, quero %q", c.name, got, c.wantLine)
		}
	}
	// sem Elevate nunca usa sudo
	if (OSRunner{GOOS: "linux", Euid: func() int { return 1000 }}).UsesSudo(Cmd{Name: "git"}) {
		t.Error("comando sem Elevate não usa sudo")
	}
}

func orTrue(f func() bool) func() bool {
	if f != nil {
		return f
	}
	return func() bool { return true }
}

// Cmd com Secret não mostra os Args, mas o Elevate continua valendo: UsesSudo acende o aviso mesmo assim.
func TestUsesSudoCountsSecretCommands(t *testing.T) {
	secret := Cmd{Name: "apt-get", Args: []string{"install", "--senha=segredo"}, Elevate: true, Secret: true}
	r := OSRunner{GOOS: "linux", Euid: func() int { return 1000 }}
	if strings.Contains(r.Display(secret), "sudo") {
		t.Fatal("o Display de Secret não mostra Args nem sudo")
	}
	if !r.UsesSudo(secret) {
		t.Fatal("o passo elevado com Secret roda via sudo e o aviso tem de acender")
	}
	if (OSRunner{GOOS: "linux", Euid: func() int { return 0 }}).UsesSudo(secret) {
		t.Fatal("como root não usa sudo")
	}
}

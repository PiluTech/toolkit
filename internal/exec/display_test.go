package exec

import "testing"

func TestDisplayMostraEnvESudoComoSeraExecutado(t *testing.T) {
	r := OSRunner{GOOS: "linux"}
	cases := []struct {
		name string
		cmd  Cmd
		want string
	}{
		{"elevado com Env: sudo -n -- env", Cmd{Name: "apt-get", Args: []string{"install", "-y", "git"}, Env: []string{"DEBIAN_FRONTEND=noninteractive", "LC_ALL=C"}, Elevate: true},
			"sudo -n -- env DEBIAN_FRONTEND=noninteractive LC_ALL=C apt-get install -y git"},
		{"elevado sem Env", Cmd{Name: "pacman", Args: []string{"-S", "git"}, Elevate: true}, "sudo -n -- pacman -S git"},
		{"sem Elevate com Env: prefixo VAR=valor", Cmd{Name: "brew", Args: []string{"install", "git"}, Env: []string{"HOMEBREW_NO_AUTO_UPDATE=1"}}, "HOMEBREW_NO_AUTO_UPDATE=1 brew install git"},
		{"simples", Cmd{Name: "ls", Args: []string{"-l"}}, "ls -l"},
		{"Secret esconde os Args", Cmd{Name: "tool", Args: []string{"--token", "x"}, Secret: true, Elevate: true}, "tool ***"},
	}
	for _, c := range cases {
		if got := r.Display(c.cmd); got != c.want {
			t.Errorf("%s: %q, queria %q", c.name, got, c.want)
		}
	}
}

func TestDisplayWindowsElevadoSemTokenCaiNoString(t *testing.T) {
	r := OSRunner{GOOS: "windows"}
	c := Cmd{Name: "dism.exe", Args: []string{"/online"}, Elevate: true}
	if got := r.Display(c); got != c.String() {
		t.Errorf("got %q", got)
	}
	r.IsElevated = func() bool { return true }
	if got := r.Display(c); got != "dism.exe /online" {
		t.Errorf("elevado: %q", got)
	}
}

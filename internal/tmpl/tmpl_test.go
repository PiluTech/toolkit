package tmpl

import "testing"

func TestExpand(t *testing.T) {
	vars := map[string]string{OS: "linux", Arch: "arm64", MajorMinor: "3.12"}
	cases := []struct{ in, want string }{
		{"sem variável", "sem variável"},
		{"x_{{os}}_{{arch}}.zip", "x_linux_arm64.zip"},
		{"python{{ major_minor }}", "python3.12"},
		{"{{os}}{{os}}", "linuxlinux"},
	}
	for _, c := range cases {
		got, err := Expand(c.in, vars)
		if err != nil || got != c.want {
			t.Errorf("%q: %q %v", c.in, got, err)
		}
	}
}

func TestExpandErros(t *testing.T) {
	for _, in := range []string{"{{nada}}", "{{os", "os}}", "{{}}", "{{version}}"} {
		if _, err := Expand(in, map[string]string{OS: "linux"}); err == nil {
			t.Errorf("%q deveria falhar", in)
		}
	}
}

func TestValidate(t *testing.T) {
	if err := Validate("a-{{os}}-{{toolkit_version}}"); err != nil {
		t.Fatal(err)
	}
	if err := Validate("{{funcao arg}}"); err == nil {
		t.Fatal("variável com argumento deveria falhar")
	}
	if err := Validate("{{os}"); err == nil {
		t.Fatal("sem fechar deveria falhar")
	}
}

func TestNames(t *testing.T) {
	got, err := Names("{{os}}/{{ arch }}")
	if err != nil || len(got) != 2 || got[0] != "os" || got[1] != "arch" {
		t.Fatalf("%v %v", got, err)
	}
}

package diagfmt

import "testing"

func TestLangFromEnv(t *testing.T) {
	env := func(m map[string]string) func(string) string { return func(k string) string { return m[k] } }
	cases := []struct {
		env  map[string]string
		want Lang
	}{
		{nil, PT},
		{map[string]string{"LANG": "pt_BR.UTF-8"}, PT},
		{map[string]string{"LANG": "en_US.UTF-8"}, EN},
		{map[string]string{"LANG": "es_MX.UTF-8"}, ES},
		{map[string]string{"LC_ALL": "C", "LANG": "en_US"}, PT},
		{map[string]string{"LC_ALL": "es_ES", "LANG": "en_US"}, ES},
		{map[string]string{"TOOLKIT_LANG": "en", "LC_ALL": "es_ES"}, EN},
		{map[string]string{"TOOLKIT_LANG": "fr", "LANG": "es_ES"}, PT},
		{map[string]string{"TOOLKIT_LANG": "  ", "LANG": "es_ES"}, ES},
		{map[string]string{"TOOLKIT_LANG": "EN"}, EN},
	}
	for _, c := range cases {
		if got := LangFromEnv(env(c.env)); got != c.want {
			t.Errorf("%v: %v, quer %v", c.env, got, c.want)
		}
	}
}

func TestBytesENumber(t *testing.T) {
	cases := []struct {
		b      uint64
		binary bool
		l      Lang
		want   string
	}{
		{8589934592, true, PT, "8 GB"},
		{8589934592, true, EN, "8 GB"},
		{17179869184, true, PT, "16 GB"},
		{8478000000, true, PT, "7,9 GB"},
		{8478000000, true, EN, "7.9 GB"},
		{256060514304, false, PT, "256 GB"},
		{1000204886016, false, PT, "1 TB"},
		{500107862016, false, ES, "500 GB"},
		{512 * 1024 * 1024, true, PT, "512 MB"},
		{0, true, PT, "0 B"},
		{1536 * 1024 * 1024, true, PT, "1,5 GB"},
	}
	for _, c := range cases {
		if got := Bytes(c.b, c.binary, c.l); got != c.want {
			t.Errorf("Bytes(%d,%v,%v) = %q, quer %q", c.b, c.binary, c.l, got, c.want)
		}
	}
	if Number(5.1, 1, PT) != "5,1" || Number(5.1, 1, EN) != "5.1" || Number(5.0, 1, PT) != "5" || Percent(0.65) != "65%" {
		t.Error("Number/Percent")
	}
}

func TestRegistry(t *testing.T) {
	Register("teste-x", func(raw string, l Lang) (string, error) { return T(l, "a", "b", "c") + raw, nil })
	if !Known("teste-x") || Known("nao-existe") {
		t.Fatal("Known")
	}
	out, err := Render("teste-x", "!", ES)
	if err != nil || out != "c!" {
		t.Fatalf("%q %v", out, err)
	}
	if _, err := Render("nao-existe", "", PT); err == nil {
		t.Fatal("renderizador desconhecido deve ser erro")
	}
}

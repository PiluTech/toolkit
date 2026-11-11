package exec

import "testing"

func TestCmdString(t *testing.T) {
	cases := []struct {
		name string
		cmd  Cmd
		want string
	}{
		{"simples", Cmd{Name: "winget", Args: []string{"list"}}, "winget list"},
		{"com espaço", Cmd{Name: "x", Args: []string{"a b", ""}}, `x "a b" ""`},
		{"secreto", Cmd{Name: "gh", Args: []string{"--token", "abc"}, Secret: true}, "gh ***"},
		{"secreto sem args", Cmd{Name: "gh", Secret: true}, "gh"},
	}
	for _, c := range cases {
		if got := c.cmd.String(); got != c.want {
			t.Errorf("%s: %q, quer %q", c.name, got, c.want)
		}
	}
}

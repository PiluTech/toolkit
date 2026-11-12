package platform

import (
	"context"
	"errors"
	"testing"

	"github.com/PiluTech/toolkit/internal/exec/exectest"
)

func TestIsElevatedViaPowerShell(t *testing.T) {
	line := psCmd(elevatedScript).String()
	cases := []struct {
		name  string
		reply exectest.Reply
		want  bool
	}{
		{"elevado", exectest.Exit(0, "True\r\n"), true},
		{"minúsculas", exectest.Exit(0, "true"), true},
		{"não elevado", exectest.Exit(0, "False\n"), false},
		{"saída estranha", exectest.Exit(0, "sim"), false},
		{"exit != 0", exectest.Exit(1, "True"), false},
		{"erro do runner", exectest.Reply{Err: errors.New("sem powershell")}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := exectest.New(t).On(line, c.reply)
			if got := IsElevatedViaPowerShell(context.Background(), r); got != c.want {
				t.Fatalf("got %v", got)
			}
			if cmds := r.Calls(); len(cmds) != 1 || !cmds[0].ReadOnly {
				t.Fatalf("deve ser uma leitura: %+v", cmds)
			}
		})
	}
	if IsElevatedViaPowerShell(context.Background(), nil) {
		t.Fatal("sem runner = não elevado")
	}
}

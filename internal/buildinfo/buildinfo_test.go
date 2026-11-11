package buildinfo

import (
	"strings"
	"testing"
)

func TestIdentifier(t *testing.T) {
	if got, want := Identifier(), "dev.pilutech."+Name; got != want {
		t.Fatalf("Identifier() = %q, quer %q", got, want)
	}
}

func TestString(t *testing.T) {
	oldV, oldC, oldD := Version, Commit, Date
	t.Cleanup(func() { Version, Commit, Date = oldV, oldC, oldD })
	Version, Commit, Date = "1.2.3", "abc1234", "2026-11-11"

	got := String()
	for _, part := range []string{Name, "1.2.3", "abc1234", "2026-11-11"} {
		if !strings.Contains(got, part) {
			t.Errorf("String() = %q não contém %q", got, part)
		}
	}
}

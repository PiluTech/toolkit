package platform

import "testing"

func TestResolvePaths(t *testing.T) {
	home := "/home/u"
	cases := []struct {
		name string
		info Info
		env  Env
		want Paths
	}{
		{"windows admin", Info{OS: Windows, IsAdmin: true}, Env{Getenv: env(map[string]string{"ProgramData": `D:\PD`})},
			Paths{`D:\PD\toolkit`, `D:\PD\toolkit\logs`, `D:\PD\toolkit\bin`}},
		{"windows admin sem ProgramData", Info{OS: Windows, IsAdmin: true}, Env{},
			Paths{`C:\ProgramData\toolkit`, `C:\ProgramData\toolkit\logs`, `C:\ProgramData\toolkit\bin`}},
		{"windows usuário", Info{OS: Windows}, Env{Getenv: env(map[string]string{"LOCALAPPDATA": `C:\L`})},
			Paths{`C:\L\toolkit`, `C:\L\toolkit\logs`, `C:\L\toolkit\bin`}},
		{"windows usuário sem LOCALAPPDATA", Info{OS: Windows}, Env{Home: `C:\Users\u`},
			Paths{`C:\Users\u\AppData\Local\toolkit`, `C:\Users\u\AppData\Local\toolkit\logs`, `C:\Users\u\AppData\Local\toolkit\bin`}},
		{"linux root", Info{OS: Linux, IsAdmin: true}, Env{Home: "/root"},
			Paths{"/var/lib/toolkit", "/var/lib/toolkit/logs", "/usr/local/bin"}},
		{"linux usuário com XDG", Info{OS: Linux}, Env{Home: home, Getenv: env(map[string]string{"XDG_STATE_HOME": "/x"})},
			Paths{"/x/toolkit", "/x/toolkit/logs", home + "/.local/bin"}},
		{"linux usuário sem XDG", Info{OS: Linux}, Env{Home: home},
			Paths{home + "/.local/state/toolkit", home + "/.local/state/toolkit/logs", home + "/.local/bin"}},
		{"macos admin", Info{OS: Darwin, IsAdmin: true}, Env{Home: "/var/root"},
			Paths{"/Library/Application Support/toolkit", "/Library/Application Support/toolkit/logs", "/usr/local/bin"}},
		{"macos usuário", Info{OS: Darwin}, Env{Home: "/Users/u"},
			Paths{"/Users/u/Library/Application Support/toolkit", "/Users/u/Library/Application Support/toolkit/logs", "/Users/u/.local/bin"}},
	}
	for _, c := range cases {
		if got := ResolvePaths(c.info, c.env); got != c.want {
			t.Errorf("%s:\n got  %+v\n want %+v", c.name, got, c.want)
		}
	}
}

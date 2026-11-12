package detect

import (
	"context"
	"io/fs"
	"path"
	"strings"
	"time"

	"github.com/PiluTech/toolkit/internal/catalog"
	"github.com/PiluTech/toolkit/internal/exec"
)

func (d *Detector) desktopDirs() []string {
	dirs := []string{"/usr/share/applications"}
	if h := path.Clean(d.opts.Home); d.opts.Home != "" && absClean(h) {
		dirs = append(dirs, h+"/.local/share/applications")
	}
	return dirs
}

// findLinux procura, nesta ordem: o .desktop (instalação por pacote avulso, AppImage integrado, tarball),
// o flatpak e o snap. O flatpak e o snap só são consultados quando o catálogo os declara.
func (d *Detector) findLinux(ctx context.Context, spec catalog.Detect) (Found, bool) {
	if spec.DesktopID != "" && d.opts.FS != nil {
		for _, dir := range d.desktopDirs() {
			p := dir + "/" + spec.DesktopID
			if st, err := fs.Stat(d.opts.FS, rel(p)); err == nil && st.Mode().IsRegular() {
				return Found{Where: clean(p, 300)}, true
			}
		}
	}
	if spec.FlatpakID != "" {
		if f, ok := d.findFlatpak(ctx, spec.FlatpakID); ok {
			return f, true
		}
	}
	if spec.Snap != "" {
		if f, ok := d.findSnap(ctx, spec.Snap); ok {
			return f, true
		}
	}
	return Found{}, false
}

func (d *Detector) has(name string) bool {
	if d.rn == nil {
		return false
	}
	_, err := d.rn.LookPath(name)
	return err == nil
}

func (d *Detector) findFlatpak(ctx context.Context, id string) (Found, bool) {
	if !d.has("flatpak") {
		return Found{}, false
	}
	res, err := d.rn.Run(ctx, d.readOnly(exec.Cmd{Name: "flatpak", Args: []string{"list", "--app", "--columns=application,version"},
		Env: []string{"LC_ALL=C"}, Timeout: time.Minute}))
	if err != nil || res.ExitCode != 0 {
		return Found{}, false
	}
	for _, line := range strings.Split(string(res.Stdout), "\n") {
		cols := strings.Split(line, "\t")
		if strings.TrimSpace(cols[0]) != id {
			continue
		}
		ver := ""
		if len(cols) > 1 {
			ver = clean(cols[1], 64)
		}
		return Found{Where: "flatpak (" + clean(id, 200) + ")", Version: ver}, true
	}
	return Found{}, false
}

func (d *Detector) findSnap(ctx context.Context, name string) (Found, bool) {
	if !d.has("snap") {
		return Found{}, false
	}
	res, err := d.rn.Run(ctx, d.readOnly(exec.Cmd{Name: "snap", Args: []string{"list", name}, Env: []string{"LC_ALL=C"}, Timeout: 30 * time.Second}))
	if err != nil || res.ExitCode != 0 {
		return Found{}, false
	}
	// Cabeçalho "Name Version Rev Tracking Publisher Notes" e depois uma linha por snap.
	for _, line := range strings.Split(string(res.Stdout), "\n") {
		f := strings.Fields(line)
		if len(f) >= 2 && f[0] == name {
			return Found{Where: "snap (" + clean(name, 100) + ")", Version: clean(f[1], 64)}, true
		}
	}
	return Found{}, false
}

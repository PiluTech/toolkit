package detect

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"io"
	"io/fs"
	"path"
	"strings"
	"time"

	"github.com/PiluTech/toolkit/internal/catalog"
	"github.com/PiluTech/toolkit/internal/exec"
)

// plutil é fixo: um plutil achado no PATH poderia ser plantado.
const plutil = "/usr/bin/plutil"

const (
	maxPlist     = 1 << 20 // Info.plist maior que isto é ignorado
	maxScan      = 500     // entradas .app olhadas por pasta ao procurar por bundle_id
	maxBinPlists = 40      // quantos plists binários (plutil) a busca por bundle_id aceita
)

// appDirs são os lugares de apps do usuário e do sistema, em caminho absoluto.
func (d *Detector) appDirs() []string {
	dirs := []string{"/Applications"}
	if h := path.Clean(d.opts.Home); d.opts.Home != "" && absClean(h) {
		dirs = append(dirs, h+"/Applications")
	}
	return dirs
}

func (d *Detector) findMac(ctx context.Context, spec catalog.Detect) (Found, bool) {
	if d.opts.FS == nil {
		return Found{}, false
	}
	if spec.AppBundle != "" {
		for _, dir := range d.appDirs() {
			p := dir + "/" + spec.AppBundle
			if st, err := fs.Stat(d.opts.FS, rel(p)); err == nil && st.IsDir() {
				ver := ""
				if info, ok := d.readPlist(ctx, p); ok {
					ver = info.version()
				}
				return Found{Where: clean(p, 300), Version: ver}, true
			}
		}
	}
	if spec.BundleID != "" {
		idx, ok := d.bundleIndex(ctx)
		if !ok {
			return Found{}, false
		}
		if f, found := idx[strings.ToLower(spec.BundleID)]; found {
			return f, true
		}
	}
	return Found{}, false
}

// bundleIndex varre UMA vez (por cacheTTL) os .app de /Applications e ~/Applications, lendo cada Info.plist, e
// devolve bundle_id (minúsculo) -> bundle. A TUI consulta vários apps em seguida: antes cada consulta relia tudo.
// ok é false se o contexto foi cancelado no meio (nada é guardado).
func (d *Detector) bundleIndex(ctx context.Context) (map[string]Found, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.macBuilt && d.now().Sub(d.macAt) < cacheTTL {
		return d.macIndex, true
	}
	idx := map[string]Found{}
	bin := 0
	for _, dir := range d.appDirs() {
		entries, err := fs.ReadDir(d.opts.FS, rel(dir))
		if err != nil {
			continue
		}
		for i, e := range entries {
			if i >= maxScan {
				break
			}
			if ctx.Err() != nil {
				return nil, false
			}
			name := e.Name()
			if !strings.HasSuffix(name, ".app") || strings.Contains(name, "/") {
				continue
			}
			p := dir + "/" + name
			if st, err := fs.Stat(d.opts.FS, rel(p)); err != nil || !st.IsDir() {
				continue
			}
			info, ok := d.readPlistBudget(ctx, p, &bin)
			if !ok || info.id == "" {
				continue
			}
			key := strings.ToLower(info.id)
			if _, dup := idx[key]; !dup { // o primeiro (sistema antes do usuário) vale
				idx[key] = Found{Where: clean(p, 300), Version: info.version()}
			}
		}
	}
	d.macIndex, d.macAt, d.macBuilt = idx, d.now(), true
	return idx, true
}

type plistInfo struct{ id, short, build string }

// version prefere CFBundleShortVersionString e cai para CFBundleVersion; sem nenhum, vazia.
func (p plistInfo) version() string {
	v := p.short
	if strings.TrimSpace(v) == "" {
		v = p.build
	}
	return clean(v, 64)
}

func (d *Detector) readPlist(ctx context.Context, appPath string) (plistInfo, bool) {
	n := 0
	return d.readPlistBudget(ctx, appPath, &n)
}

// readPlistBudget lê Contents/Info.plist do bundle. XML é lido por um parser mínimo; plist binário (o que o
// plutil converte) vai ao /usr/bin/plutil pelo Runner, no máximo maxBinPlists vezes por busca (bin). Sem
// Info.plist, ilegível ou grande demais: ok false (o bundle continua existindo; só não há id nem versão).
func (d *Detector) readPlistBudget(ctx context.Context, appPath string, bin *int) (plistInfo, bool) {
	p := appPath + "/Contents/Info.plist"
	// Stat ANTES do Open: abrir um FIFO para leitura já bloqueia até aparecer um escritor.
	if st, err := fs.Stat(d.opts.FS, rel(p)); err != nil || !st.Mode().IsRegular() {
		return plistInfo{}, false
	}
	f, err := d.opts.FS.Open(rel(p))
	if err != nil {
		return plistInfo{}, false
	}
	defer func() { _ = f.Close() }()
	// O Stat acima e o Open são passos separados: alguém pode trocar o arquivo por um FIFO entre os dois. O OSFS
	// abre com O_NONBLOCK (não trava) e aqui o tipo é conferido NO DESCRITOR aberto (fstat).
	if st, err := f.Stat(); err != nil || !st.Mode().IsRegular() {
		return plistInfo{}, false
	}
	data, err := io.ReadAll(io.LimitReader(f, maxPlist+1))
	if err != nil || len(data) > maxPlist {
		return plistInfo{}, false
	}
	if bytes.HasPrefix(data, []byte("bplist")) {
		if *bin >= maxBinPlists || d.rn == nil {
			return plistInfo{}, false
		}
		*bin++
		return d.plutilJSON(ctx, p)
	}
	m, ok := parseXMLPlist(data)
	if !ok {
		return plistInfo{}, false
	}
	return plistInfo{id: m["CFBundleIdentifier"], short: m["CFBundleShortVersionString"], build: m["CFBundleVersion"]}, true
}

func (d *Detector) plutilJSON(ctx context.Context, plistPath string) (plistInfo, bool) {
	res, err := d.rn.Run(ctx, d.readOnly(exec.Cmd{Name: plutil, Args: []string{"-convert", "json", "-o", "-", plistPath}, Timeout: 10 * time.Second}))
	if err != nil || res.ExitCode != 0 || len(res.Stdout) > maxPlist {
		return plistInfo{}, false
	}
	var raw map[string]any
	if json.Unmarshal(res.Stdout, &raw) != nil {
		return plistInfo{}, false
	}
	str := func(k string) string { s, _ := raw[k].(string); return s }
	return plistInfo{id: str("CFBundleIdentifier"), short: str("CFBundleShortVersionString"), build: str("CFBundleVersion")}, true
}

// parseXMLPlist lê as chaves de texto do dicionário de topo de um plist XML. Só <string> importa; o resto é pulado.
func parseXMLPlist(data []byte) (map[string]string, bool) {
	dec := xml.NewDecoder(bytes.NewReader(data))
	dec.Strict = false
	dec.Entity = map[string]string{} // nenhuma entidade customizada é expandida
	out := map[string]string{}
	inTop := false
	key := ""
	for {
		tok, err := dec.Token()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return out, inTop
			}
			return nil, false
		}
		se, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		switch {
		case !inTop && se.Name.Local == "plist":
			continue
		case !inTop && se.Name.Local == "dict":
			inTop = true
			continue
		case !inTop:
			if dec.Skip() != nil {
				return nil, false
			}
			continue
		}
		// Filho do dicionário de topo: <key> ou um valor.
		if se.Name.Local == "key" {
			var s string
			if dec.DecodeElement(&s, &se) != nil {
				return nil, false
			}
			key = s
			continue
		}
		if se.Name.Local == "string" && key != "" {
			var s string
			if dec.DecodeElement(&s, &se) != nil {
				return nil, false
			}
			if _, dup := out[key]; !dup {
				out[key] = s
			}
		} else if dec.Skip() != nil {
			return nil, false
		}
		key = ""
	}
}

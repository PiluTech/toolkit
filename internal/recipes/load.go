package recipes

import (
	"errors"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strings"

	"github.com/PiluVitu/toolkit/internal/catalog"
	"github.com/PiluVitu/toolkit/internal/platform"
)

// dirOS mapeia a pasta recipes/<so> para o SO da receita.
var dirOS = map[string]platform.OS{"windows": platform.Windows, "linux": platform.Linux, "macos": platform.Darwin}

// Set é o conjunto de receitas carregadas (várias OS podem ter o mesmo id, como diag-memoria).
type Set struct {
	list []*Recipe
}

// LoadFS lê root/<so>/<id>.yaml de fsys (root costuma ser "recipes"). Erros de todas as receitas são
// reunidos. A pasta tem de bater com o campo os da receita, e o id com o nome do arquivo.
func LoadFS(fsys fs.FS, root string) (*Set, error) {
	var errs catalog.Errors
	set := &Set{}
	osDirs, err := fs.ReadDir(fsys, root)
	if err != nil {
		return nil, err
	}
	for _, d := range osDirs {
		if !d.IsDir() {
			continue
		}
		want, ok := dirOS[d.Name()]
		if !ok {
			errs = append(errs, catalog.Error{File: path.Join(root, d.Name()), Msg: "pasta de SO desconhecida (use windows, linux ou macos)"})
			continue
		}
		files, err := fs.ReadDir(fsys, path.Join(root, d.Name()))
		if err != nil {
			return nil, err
		}
		for _, f := range files {
			if f.IsDir() || !strings.HasSuffix(f.Name(), ".yaml") {
				continue
			}
			p := path.Join(root, d.Name(), f.Name())
			data, err := fs.ReadFile(fsys, p)
			if err != nil {
				return nil, err
			}
			r, err := Parse(p, data)
			if err != nil {
				var es catalog.Errors
				if !errors.As(err, &es) {
					es = catalog.Errors{{File: p, Msg: err.Error()}}
				}
				errs = append(errs, es...)
				continue
			}
			if r.OS != want {
				errs = append(errs, catalog.Error{File: p, Msg: fmt.Sprintf("a receita é de %s mas está na pasta %s", osName(r.OS), d.Name())})
				continue
			}
			set.list = append(set.list, r)
		}
	}
	if len(errs) > 0 {
		return nil, errs
	}
	sort.Slice(set.list, func(i, j int) bool {
		if set.list[i].OS != set.list[j].OS {
			return set.list[i].OS < set.list[j].OS
		}
		return set.list[i].ID < set.list[j].ID
	})
	return set, nil
}

// For devolve as receitas do SO, ordenadas por id.
func (s *Set) For(os platform.OS) []*Recipe {
	var out []*Recipe
	for _, r := range s.list {
		if r.OS == os {
			out = append(out, r)
		}
	}
	return out
}

// Get acha a receita id do SO.
func (s *Set) Get(os platform.OS, id string) (*Recipe, bool) {
	for _, r := range s.list {
		if r.OS == os && r.ID == id {
			return r, true
		}
	}
	return nil, false
}

// All devolve todas as receitas.
func (s *Set) All() []*Recipe { return append([]*Recipe(nil), s.list...) }

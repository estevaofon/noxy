package modsrc

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/estevaofon/noxy/internal/stdlib"
)

// DiskSource resolve na ordem que a VM e o compilador sempre usaram (spec
// §0): NOXY_PATH → <projeto>/noxy_libs → <raiz>/noxy_libs → <raiz>/stdlib →
// <raiz>/<nome> → os mesmos relativos ao cwd → stdlib embutida. Selada
// (modo aplicacao) nao tem SearchPaths nem SearchCwd: nada fora da raiz
// sombreia o app.
type DiskSource struct {
	Root        string
	ProjectRoot string
	SearchPaths []string
	SearchCwd   bool
}

func NewDisk(root, projectRoot string) *DiskSource {
	d := &DiskSource{Root: root, ProjectRoot: projectRoot, SearchCwd: true}
	if noxyPath := os.Getenv("NOXY_PATH"); noxyPath != "" {
		d.SearchPaths = filepath.SplitList(noxyPath)
	}
	return d
}

func NewSealed(root, projectRoot string) *DiskSource {
	return &DiskSource{Root: root, ProjectRoot: projectRoot}
}

func (d *DiskSource) Key() string {
	root, err := filepath.Abs(d.Root)
	if err != nil {
		root = d.Root
	}
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		root = resolved
	} else {
		root = filepath.Clean(root)
	}
	seal := "open"
	if !d.SearchCwd {
		seal = "sealed"
	}
	return root + "\x00" + strings.Join(d.SearchPaths, string(os.PathListSeparator)) + "\x00" + seal
}

func (d *DiskSource) candidates(suffix string) []string {
	out := make([]string, 0, 16)
	for _, searchRoot := range d.SearchPaths {
		out = append(out,
			filepath.Join(searchRoot, suffix, suffix+".nx"),
			filepath.Join(searchRoot, suffix),
			filepath.Join(searchRoot, suffix+".nx"),
		)
	}
	if d.ProjectRoot != "" {
		out = append(out,
			filepath.Join(d.ProjectRoot, "noxy_libs", suffix, suffix+".nx"),
			filepath.Join(d.ProjectRoot, "noxy_libs", suffix),
		)
	}
	out = append(out,
		filepath.Join(d.Root, "noxy_libs", suffix, suffix+".nx"),
		filepath.Join(d.Root, "noxy_libs", suffix),
		filepath.Join(d.Root, "stdlib", suffix),
		filepath.Join(d.Root, suffix),
	)
	if d.SearchCwd {
		out = append(out,
			filepath.Join("noxy_libs", suffix, suffix+".nx"),
			filepath.Join("noxy_libs", suffix),
			filepath.Join("stdlib", suffix),
			suffix,
		)
	}
	return out
}

// locate devolve o primeiro candidato existente (absoluto, limpo) e se e
// diretorio.
func (d *DiskSource) locate(suffix string) (string, bool, bool) {
	for _, candidate := range d.candidates(suffix) {
		info, err := os.Stat(candidate)
		if err != nil {
			continue
		}
		absolute, err := filepath.Abs(candidate)
		if err != nil {
			return "", false, false
		}
		return filepath.Clean(absolute), info.IsDir(), true
	}
	return "", false, false
}

func (d *DiskSource) Resolve(name string) (Module, error) {
	name = strings.TrimSpace(name)
	pathName := strings.ReplaceAll(name, ".", string(filepath.Separator))
	path, isDir, found := d.locate(pathName + ".nx")
	if !found || isDir {
		path, isDir, found = d.locate(pathName)
	}
	if found {
		if !isDir {
			return Module{Name: name, Kind: KindFile, Path: path}, nil
		}
		base := filepath.Base(path)
		for _, entry := range []string{base + ".nx", "main.nx"} {
			entryPath := filepath.Join(path, entry)
			if info, err := os.Stat(entryPath); err == nil && !info.IsDir() {
				return Module{Name: name, Kind: KindFile, Path: entryPath}, nil
			}
		}
		return Module{Name: name, Kind: KindDirectory, Path: path}, nil
	}
	content, err := stdlib.FS.ReadFile(strings.ReplaceAll(name, ".", "/") + ".nx")
	if err != nil {
		return Module{}, fmt.Errorf("%w: %s", ErrNotFound, name)
	}
	return Module{Name: name, Kind: KindEmbedded, Content: string(content)}, nil
}

func (d *DiskSource) ReadFile(path string) ([]byte, error) { return os.ReadFile(path) }

func (d *DiskSource) ReadDir(path string) ([]Entry, error) {
	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, err
	}
	out := make([]Entry, 0, len(entries))
	for _, entry := range entries {
		out = append(out, Entry{Name: entry.Name(), IsDir: entry.IsDir()})
	}
	return out, nil
}

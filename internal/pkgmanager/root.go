package pkgmanager

import (
	"os"
	"path/filepath"
)

// FindRoot e a UNICA definicao de raiz do projeto (spec §3.0): o ancestral
// mais proximo de start (inclusive) que contem noxy.mod. --sync/--get partem
// do cwd; VM e compilador partem do diretorio do script. Ausencia e caso
// normal (script solto), por isso bool e nao error.
func FindRoot(start string) (string, bool) {
	dir, err := filepath.Abs(start)
	if err != nil {
		return "", false
	}
	if resolved, err := filepath.EvalSymlinks(dir); err == nil {
		dir = resolved
	}
	for {
		if info, err := os.Stat(filepath.Join(dir, "noxy.mod")); err == nil && !info.IsDir() {
			return dir, true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false
		}
		dir = parent
	}
}

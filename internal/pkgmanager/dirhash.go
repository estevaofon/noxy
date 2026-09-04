package pkgmanager

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// treeHashExcluded: primeiro segmento de caminho que nao entra no hash.
// .git ja foi removido no clone; bin/ sao assets por plataforma (linhas
// proprias no noxy.sum); noxy_libs/ e vendorizacao acidental.
var treeHashExcluded = map[string]bool{".git": true, "bin": true, "noxy_libs": true}

// TreeHash e o Hash1 do dirhash do Go com saida hex: sha256 das linhas
// "<sha256 hex do arquivo>  <caminho com />\n" ordenadas por caminho
// (spec §3.3). Symlink e erro, como no Go.
func TreeHash(dir string) (string, error) {
	var lines []string
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, relErr := filepath.Rel(dir, path)
		if relErr != nil {
			return relErr
		}
		if rel == "." {
			return nil
		}
		rel = filepath.ToSlash(rel)
		first := rel
		if i := strings.IndexByte(rel, '/'); i >= 0 {
			first = rel[:i]
		}
		if treeHashExcluded[first] {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Type()&fs.ModeSymlink != 0 {
			return fmt.Errorf("package contains a symlink: %s", rel)
		}
		if d.IsDir() || !d.Type().IsRegular() {
			return nil
		}
		f, openErr := os.Open(path)
		if openErr != nil {
			return openErr
		}
		defer f.Close()
		h := sha256.New()
		if _, copyErr := io.Copy(h, f); copyErr != nil {
			return copyErr
		}
		lines = append(lines, fmt.Sprintf("%x  %s\n", h.Sum(nil), rel))
		return nil
	})
	if err != nil {
		return "", err
	}
	sort.Strings(lines)
	h := sha256.New()
	for _, line := range lines {
		h.Write([]byte(line))
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

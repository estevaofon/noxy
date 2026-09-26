package bundle

import (
	"archive/zip"
	"bytes"
	"fmt"
	"os"
	"path"
	"sort"
	"strings"
)

// File e um arquivo do payload: caminho relativo a raiz do projeto com "/".
type File struct {
	Path       string
	Data       []byte
	Executable bool
}

// ValidPath: relativo, com "/", ja limpo (sem ".", "..", "//", barra final),
// sem "\" nem letra de unidade.
func ValidPath(p string) bool {
	if p == "" || strings.HasPrefix(p, "/") || strings.Contains(p, "\\") {
		return false
	}
	if len(p) >= 2 && p[1] == ':' {
		return false
	}
	if path.Clean(p) != p {
		return false
	}
	for _, seg := range strings.Split(p, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return false
		}
	}
	return true
}

// Pack monta o zip do payload: manifesto + arquivos, ordem lexicografica,
// deflate, timestamps zerados (spec §4.3) — mesmo conteudo, mesmos bytes.
func Pack(m *Manifest, files []File) ([]byte, error) {
	manifest, err := m.Encode()
	if err != nil {
		return nil, err
	}
	all := make([]File, 0, len(files)+1)
	all = append(all, File{Path: ManifestName, Data: manifest})
	all = append(all, files...)
	sort.SliceStable(all, func(i, j int) bool { return all[i].Path < all[j].Path })
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	seen := make(map[string]bool, len(all))
	for _, f := range all {
		if !ValidPath(f.Path) {
			return nil, fmt.Errorf("invalid payload path %q", f.Path)
		}
		if seen[f.Path] {
			return nil, fmt.Errorf("duplicate payload path %q", f.Path)
		}
		seen[f.Path] = true
		header := &zip.FileHeader{Name: f.Path, Method: zip.Deflate}
		mode := os.FileMode(0o644)
		if f.Executable {
			mode = 0o755
		}
		header.SetMode(mode)
		w, err := zw.CreateHeader(header)
		if err != nil {
			return nil, err
		}
		if _, err := w.Write(f.Data); err != nil {
			return nil, err
		}
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

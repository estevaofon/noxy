package build

import (
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/estevaofon/noxy/internal/bundle"
)

const darwinWarning = "warning: macOS output is experimental and was not validated on this platform (see docs/BUILD.md)"

// Write gera o executavel (spec §6.6): runtime + zip + trailer em
// <saida>.tmp, chmod, rename. Reprodutivel: mesmo projeto, mesmos bytes.
func Write(p *Plan, opts Options) (string, int64, error) {
	diag := opts.Diag
	if diag == nil {
		diag = os.Stderr
	}
	out := opts.Output
	if out == "" {
		out = strings.TrimSuffix(filepath.Base(p.Entry), ".nx")
	}
	if runtime.GOOS == "windows" && !strings.HasSuffix(strings.ToLower(out), ".exe") {
		out += ".exe"
	}
	files := make([]bundle.File, 0, len(p.Files))
	for _, f := range p.Files {
		data, err := os.ReadFile(f.Abs)
		if err != nil {
			return "", 0, err
		}
		files = append(files, bundle.File{Path: f.Path, Data: data, Executable: f.Executable})
	}
	payload, err := bundle.Pack(p.Manifest(), files)
	if err != nil {
		return "", 0, err
	}
	runtimeBytes, err := bundle.RuntimeBytes(p.Runtime)
	if err != nil {
		return "", 0, fmt.Errorf("runtime %s: %w", p.Runtime, err)
	}
	trailer := bundle.Trailer{SHA256: sha256.Sum256(payload), Offset: uint64(len(runtimeBytes)), Size: uint64(len(payload))}
	if dir := filepath.Dir(out); dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return "", 0, err
		}
	}
	tmp := out + ".tmp"
	if err := writeAll(tmp, runtimeBytes, payload, trailer.Marshal()); err != nil {
		os.Remove(tmp)
		return "", 0, err
	}
	if runtime.GOOS != "windows" {
		if err := os.Chmod(tmp, 0o755); err != nil {
			os.Remove(tmp)
			return "", 0, err
		}
	}
	if err := os.Rename(tmp, out); err != nil {
		os.Remove(tmp)
		return "", 0, err
	}
	if runtime.GOOS == "darwin" {
		fmt.Fprintln(diag, darwinWarning)
	}
	return out, int64(len(runtimeBytes) + len(payload) + bundle.TrailerSize), nil
}

func writeAll(path string, parts ...[]byte) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
	if err != nil {
		return err
	}
	for _, part := range parts {
		if _, err := io.Copy(f, strings.NewReader(string(part))); err != nil {
			f.Close()
			return err
		}
	}
	return f.Close()
}

package pkgmanager

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSumFileRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "noxy.sum")
	s, err := ParseSumFile(path)
	if err != nil {
		t.Fatalf("missing file must parse as empty: %v", err)
	}
	s.Set("github_com/acme/zstd", "ext.wasm", "abc123")
	s.Set("github_com/acme/zstd", "noxy_ext.toml", "def456")
	if err := s.Save(path); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "github_com/acme/zstd ext.wasm sha256:abc123") {
		t.Fatalf("format: %s", data)
	}
	back, err := ParseSumFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := back.Lookup("github_com/acme/zstd", "ext.wasm"); !ok || got != "abc123" {
		t.Fatalf("lookup: %q %v", got, ok)
	}
}

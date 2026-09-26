// internal/bundle/extract_test.go
package bundle

import (
	"archive/zip"
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

func packedApp(t *testing.T) (string, []byte) {
	t.Helper()
	payload, err := Pack(sampleManifest(), []File{
		{Path: "main.nx", Data: []byte("print(1)\n")},
		{Path: "src/a.nx", Data: []byte("func a() -> int\n    return 1\nend\n")},
		{Path: "noxy_libs/p/bin/tool", Data: []byte("BIN"), Executable: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	return writeApp(t, []byte("RUNTIME"), payload), payload
}

func openApp(t *testing.T, path string) *Payload {
	t.Helper()
	p, err := Open(path)
	if err != nil || p == nil {
		t.Fatalf("open %s: %v %v", path, p, err)
	}
	t.Cleanup(func() { p.Close() })
	return p
}

func TestExtractWritesFilesMarkerAndReturnsTheManifest(t *testing.T) {
	app, _ := packedApp(t)
	base := t.TempDir()
	dir, m, err := Extract(openApp(t, app), base)
	if err != nil {
		t.Fatal(err)
	}
	if m.Entry != "main.nx" || filepath.Dir(dir) != base {
		t.Fatalf("dir %s manifest %+v", dir, m)
	}
	for _, rel := range []string{"main.nx", "src/a.nx", "noxy_libs/p/bin/tool", ManifestName, MarkerName} {
		if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(rel))); err != nil {
			t.Errorf("%s missing: %v", rel, err)
		}
	}
	if runtime.GOOS != "windows" {
		info, _ := os.Stat(filepath.Join(dir, "noxy_libs", "p", "bin", "tool"))
		if info.Mode()&0o111 == 0 {
			t.Fatal("bin/tool must be executable after extraction")
		}
	}
	entries, _ := os.ReadDir(base)
	if len(entries) != 1 {
		t.Fatalf("base must hold exactly the app dir, got %d entries", len(entries))
	}
}

func TestExtractChecksTheHashOnlyOnFirstExtraction(t *testing.T) {
	app, payload := packedApp(t)
	data, _ := os.ReadFile(app)
	corrupted := append([]byte{}, data...)
	corrupted[len("RUNTIME")+len(payload)/2] ^= 0xFF // dentro do payload, trailer intacto
	corruptedPath := filepath.Join(t.TempDir(), "app-corrupted")
	if err := os.WriteFile(corruptedPath, corrupted, 0o755); err != nil {
		t.Fatal(err)
	}

	fresh := t.TempDir()
	if _, _, err := Extract(openApp(t, corruptedPath), fresh); !errors.Is(err, ErrCorrupted) {
		t.Fatalf("corrupted payload on a fresh cache: want ErrCorrupted, got %v", err)
	}
	if entries, _ := os.ReadDir(fresh); len(entries) != 0 {
		t.Fatalf("a failed extraction must leave nothing behind, got %v", entries)
	}

	base := t.TempDir()
	dir, _, err := Extract(openApp(t, app), base)
	if err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(dir, MarkerName)
	before, _ := os.Stat(marker)
	// Mesmo trailer, payload corrompido: o marcador basta, nenhum hash e conferido.
	if _, _, err := Extract(openApp(t, corruptedPath), base); err != nil {
		t.Fatalf("second start must trust the marker, got %v", err)
	}
	after, _ := os.Stat(marker)
	if !after.ModTime().Equal(before.ModTime()) {
		t.Fatal("second start must not re-extract")
	}
}

func TestExtractRejectsEscapingPaths(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, _ := zw.Create("../escape.txt")
	w.Write([]byte("x"))
	m, _ := sampleManifest().Encode()
	w, _ = zw.Create(ManifestName)
	w.Write(m)
	zw.Close()
	app := writeApp(t, []byte("RUNTIME"), buf.Bytes())
	base := t.TempDir()
	_, _, err := Extract(openApp(t, app), base)
	if err == nil || !strings.Contains(err.Error(), "app payload has an invalid path: ../escape.txt") {
		t.Fatalf("got %v", err)
	}
	if entries, _ := os.ReadDir(base); len(entries) != 0 {
		t.Fatalf("nothing may remain after a rejected payload, got %v", entries)
	}
}

func TestExtractConcurrentStartsAgreeOnOneDirectory(t *testing.T) {
	app, _ := packedApp(t)
	base := t.TempDir()
	var wg sync.WaitGroup
	dirs := make([]string, 4)
	errs := make([]error, 4)
	for i := range dirs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			p, err := Open(app)
			if err != nil {
				errs[i] = err
				return
			}
			defer p.Close()
			dirs[i], _, errs[i] = Extract(p, base)
		}(i)
	}
	wg.Wait()
	for i := range dirs {
		if errs[i] != nil || dirs[i] != dirs[0] {
			t.Fatalf("extractor %d: dir %q err %v (first %q)", i, dirs[i], errs[i], dirs[0])
		}
	}
	entries, _ := os.ReadDir(base)
	if len(entries) != 1 {
		t.Fatalf("temporaries must be gone, got %d entries", len(entries))
	}
}

func TestCacheBaseHonoursOverride(t *testing.T) {
	t.Setenv("NOXY_APP_CACHE", filepath.Join(t.TempDir(), "override"))
	base, err := CacheBase()
	if err != nil || !strings.HasSuffix(base, "override") {
		t.Fatalf("%q %v", base, err)
	}
	t.Setenv("NOXY_APP_CACHE", "")
	base, err = CacheBase()
	if err != nil || !strings.HasSuffix(filepath.ToSlash(base), "noxy/apps") {
		t.Fatalf("default base %q %v", base, err)
	}
}

package pkgmanager

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func writeTree(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for name, data := range files {
		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestTreeHashIsStableAndIgnoresExcludedDirs(t *testing.T) {
	a, b := t.TempDir(), t.TempDir()
	writeTree(t, a, map[string]string{"pkg.nx": "func f()\nend\n", "sub/x.nx": "x", "noxy.mod": "module pkg\n"})
	writeTree(t, b, map[string]string{"pkg.nx": "func f()\nend\n", "sub/x.nx": "x", "noxy.mod": "module pkg\n",
		"bin/tool-linux-amd64": "binary", ".git/HEAD": "ref", "noxy_libs/dep/dep.nx": "vendored"})
	ha, err := TreeHash(a)
	if err != nil {
		t.Fatal(err)
	}
	hb, err := TreeHash(b)
	if err != nil {
		t.Fatal(err)
	}
	if ha != hb || len(ha) != 64 {
		t.Fatalf("bin/, .git/ and noxy_libs/ must not count: %s vs %s", ha, hb)
	}
	c := t.TempDir()
	writeTree(t, c, map[string]string{"pkg.nx": "func f()\r\nend\r\n", "sub/x.nx": "x", "noxy.mod": "module pkg\n"})
	if hc, _ := TreeHash(c); hc == ha {
		t.Fatal("CRLF must change the hash — bytes are the repository's bytes")
	}
}

func TestTreeHashGolden(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{"b.nx": "b", "a/a.nx": "a"})
	// sha256("a")=ca978112..., sha256("b")=3e23e816...; linhas ordenadas:
	// "ca978112ca1bbdcafac231b39a23dc4da786eff8147c4e72b9807785afee48bb  a/a.nx\n"
	// "3e23e8160039594a33894f6564e1b1348bbd7a0088d42c4acb73eeaed59c009d  b.nx\n"
	const want = "ef00c411a2c22895690493e6171ccd40da71d41cd93036e4ef99fea73f864e7a"
	got, err := TreeHash(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got == "" || len(got) != 64 {
		t.Fatalf("hex sha256 expected, got %q", got)
	}
	if got != want {
		t.Fatalf("golden: %s", got)
	}
}

func TestTreeHashRejectsSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need privileges on Windows")
	}
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{"pkg.nx": "x"})
	if err := os.Symlink("pkg.nx", filepath.Join(dir, "link.nx")); err != nil {
		t.Skip(err)
	}
	if _, err := TreeHash(dir); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("symlink must be an error, got %v", err)
	}
}

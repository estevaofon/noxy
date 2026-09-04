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
	// sha256("a")=ca978112..., sha256("b")=3e23e816...; linhas ordenadas por caminho:
	// "ca978112ca1bbdcafac231b39a23dc4da786eff8147c4e72b9807785afee48bb  a/a.nx\n"
	// "3e23e8160039594a33894f6564e1b1348bbd7a0088d42c4acb73eeaed59c009d  b.nx\n"
	const want = "cb5f791053dc9fe3eb77b34e5c645116d93ba4d24d660160ce5b075842037809"
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

func TestTreeHashSortsByPathNotHash(t *testing.T) {
	dir := t.TempDir()
	// Choose contents such that hash order and path order differ.
	// sha256("z")=594e519a..., sha256("a")=ca978112ca...; paths: a.nx < z.nx
	writeTree(t, dir, map[string]string{"z.nx": "z", "a.nx": "a"})
	// Hash order would be: 594e519a... z.nx, ca978112... a.nx (alphabetically 5 < c)
	// Path order is: a.nx, z.nx
	// TreeHash must use path order (spec §3.3), not hash order.
	got, err := TreeHash(dir)
	if err != nil {
		t.Fatal(err)
	}
	// Expected: path-sorted lines
	// sha256("a")=ca978112ca1bbdcafac231b39a23dc4da786eff8147c4e72b9807785afee48bb
	// sha256("z")=594e519ae499312b29433b7dd8a97ff068defcba9755b6d5d00e84c524d67b06
	const wantSorted = "fc25b6ea2c14beb8dbd38b8506926ff0038f4d8d7ec50424f27bd33efa07ce17"
	if got != wantSorted {
		t.Fatalf("path-sorted hash: expected %s, got %s", wantSorted, got)
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

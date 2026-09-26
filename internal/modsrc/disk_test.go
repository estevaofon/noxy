package modsrc

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestDiskResolveOrderAndKinds(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "noxy_libs", "pkg", "pkg.nx"), "let v: int = 1\n")
	write(t, filepath.Join(root, "src", "a.nx"), "let a: int = 1\n")
	write(t, filepath.Join(root, "src", "dir", "x.nx"), "let x: int = 1\n")
	write(t, filepath.Join(root, "withmain", "main.nx"), "let m: int = 1\n")
	write(t, filepath.Join(root, "strings.nx"), "func shadow() -> int\n    return 1\nend\n")
	d := &DiskSource{Root: root, ProjectRoot: root}

	cases := []struct {
		name string
		kind Kind
		path string
	}{
		{"pkg", KindFile, filepath.Join(root, "noxy_libs", "pkg", "pkg.nx")},
		{"src.a", KindFile, filepath.Join(root, "src", "a.nx")},
		{"src.dir", KindDirectory, filepath.Join(root, "src", "dir")},
		{"src", KindDirectory, filepath.Join(root, "src")},
		{"withmain", KindFile, filepath.Join(root, "withmain", "main.nx")},
		{"strings", KindFile, filepath.Join(root, "strings.nx")},
	}
	for _, tc := range cases {
		m, err := d.Resolve(tc.name)
		if err != nil || m.Kind != tc.kind || m.Path != tc.path || m.Name != tc.name {
			t.Errorf("%s: got %+v err %v, want kind %d path %s", tc.name, m, err, tc.kind, tc.path)
		}
	}
	io, err := d.Resolve("io")
	if err != nil || io.Kind != KindEmbedded || io.Content == "" || io.Path != "" {
		t.Fatalf("io must come from the embedded stdlib: %+v %v", io, err)
	}
	if _, err := d.Resolve("nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
	entries, err := d.ReadDir(filepath.Join(root, "src"))
	if err != nil || len(entries) != 2 || entries[0].Name != "a.nx" || entries[0].IsDir || entries[1].Name != "dir" || !entries[1].IsDir {
		t.Fatalf("ReadDir: %+v %v", entries, err)
	}
}

func TestDiskProjectLibsBeforeRootLibs(t *testing.T) {
	project := t.TempDir()
	sub := filepath.Join(project, "examples")
	write(t, filepath.Join(project, "noxy_libs", "m", "m.nx"), "let where: string = \"project\"\n")
	write(t, filepath.Join(sub, "noxy_libs", "m", "m.nx"), "let where: string = \"root\"\n")
	d := &DiskSource{Root: sub, ProjectRoot: project}
	m, err := d.Resolve("m")
	if err != nil || m.Path != filepath.Join(project, "noxy_libs", "m", "m.nx") {
		t.Fatalf("project noxy_libs must win: %+v %v", m, err)
	}
	if m, err := (&DiskSource{Root: sub}).Resolve("m"); err != nil || m.Path != filepath.Join(sub, "noxy_libs", "m", "m.nx") {
		t.Fatalf("without a project root the script dir wins: %+v %v", m, err)
	}
}

func TestSealedIgnoresNoxyPathAndCwd(t *testing.T) {
	root := t.TempDir()
	searchPath := t.TempDir()
	write(t, filepath.Join(searchPath, "far", "far.nx"), "let f: int = 1\n")
	cwd := t.TempDir()
	write(t, filepath.Join(cwd, "noxy_libs", "near", "near.nx"), "let n: int = 1\n")
	t.Setenv("NOXY_PATH", searchPath)
	t.Chdir(cwd)

	open := NewDisk(root, "")
	if m, err := open.Resolve("far"); err != nil || m.Path != filepath.Join(searchPath, "far", "far.nx") {
		t.Fatalf("open source must honour NOXY_PATH: %+v %v", m, err)
	}
	if _, err := open.Resolve("near"); err != nil {
		t.Fatalf("open source must search the cwd: %v", err)
	}
	sealed := NewSealed(root, "")
	if _, err := sealed.Resolve("far"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("sealed source must ignore NOXY_PATH, got %v", err)
	}
	if _, err := sealed.Resolve("near"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("sealed source must ignore the cwd, got %v", err)
	}
	if open.Key() == sealed.Key() {
		t.Fatal("keys must differ between an open and a sealed source")
	}
	t.Setenv("NOXY_PATH", "")
	if NewDisk(root, "").Key() == open.Key() {
		t.Fatal("key must change with the search paths")
	}
}

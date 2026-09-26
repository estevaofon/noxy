package build

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/estevaofon/noxy/internal/bundle"
)

func fakeRuntime(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "noxy")
	if err := os.WriteFile(path, []byte("RUNTIME-BYTES"), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestWriteCreatesOutputDirectoryAndIsDeterministic(t *testing.T) {
	root := writeProject(t, map[string]string{
		"noxy.mod":       "module app\n\ninclude web\n",
		"main.nx":        "use src.a as a\nprint(a.one())\n",
		"src/a.nx":       "func one() -> int\n    return 1\nend\n",
		"web/index.html": "<html></html>\n",
	})
	rt := fakeRuntime(t)
	out := filepath.Join(t.TempDir(), "dist", "app")
	opts := Options{Entry: filepath.Join(root, "main.nx"), Output: out, Runtime: rt, Diag: &bytes.Buffer{}}
	plan, err := MakePlan(opts)
	if err != nil {
		t.Fatal(err)
	}
	got, size, err := Write(plan, opts)
	if err != nil {
		t.Fatal(err)
	}
	wantOut := out
	if runtime.GOOS == "windows" {
		wantOut += ".exe"
	}
	if got != wantOut {
		t.Fatalf("output %q, want %q", got, wantOut)
	}
	info, err := os.Stat(got)
	if err != nil || info.Size() != size || (runtime.GOOS != "windows" && info.Mode()&0o111 == 0) {
		t.Fatalf("stat %v size %d/%d mode %v", err, info.Size(), size, info.Mode())
	}
	if _, err := os.Stat(got + ".tmp"); err == nil {
		t.Fatal("temporary must be renamed away")
	}
	rtBytes, err := bundle.RuntimeBytes(got)
	if err != nil || string(rtBytes) != "RUNTIME-BYTES" {
		t.Fatalf("runtime prefix %q %v", rtBytes, err)
	}
	p, err := bundle.Open(got)
	if err != nil || p == nil {
		t.Fatalf("open: %v %v", p, err)
	}
	defer p.Close()
	dir, m, err := bundle.Extract(p, t.TempDir())
	if err != nil || m.Entry != "main.nx" || m.Includes[0] != "web" {
		t.Fatalf("extract: %v %+v", err, m)
	}
	for _, rel := range []string{"main.nx", "src/a.nx", "web/index.html", "noxy.mod"} {
		if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(rel))); err != nil {
			t.Errorf("%s: %v", rel, err)
		}
	}
	first, _ := os.ReadFile(got)
	if _, _, err := Write(plan, opts); err != nil {
		t.Fatal(err)
	}
	second, _ := os.ReadFile(got)
	if !bytes.Equal(first, second) {
		t.Fatal("two builds of the same project must be byte-identical")
	}
}

func TestWriteFromAnAppDoesNotNestPayloads(t *testing.T) {
	root := writeProject(t, map[string]string{"main.nx": "print(1)\n"})
	rt := fakeRuntime(t)
	first := filepath.Join(t.TempDir(), "app1")
	opts := Options{Entry: filepath.Join(root, "main.nx"), Output: first, Runtime: rt, Diag: &bytes.Buffer{}}
	plan, err := MakePlan(opts)
	if err != nil {
		t.Fatal(err)
	}
	firstOut, _, err := Write(plan, opts)
	if err != nil {
		t.Fatal(err)
	}
	opts.Runtime = firstOut
	opts.Output = filepath.Join(t.TempDir(), "app2")
	plan.Runtime = firstOut
	secondOut, _, err := Write(plan, opts)
	if err != nil {
		t.Fatal(err)
	}
	rtBytes, err := bundle.RuntimeBytes(secondOut)
	if err != nil || string(rtBytes) != "RUNTIME-BYTES" {
		t.Fatalf("runtime of the second app %q %v — payload nested", rtBytes, err)
	}
}

func TestWriteDefaultOutputIsTheEntryName(t *testing.T) {
	root := writeProject(t, map[string]string{"hello.nx": "print(1)\n"})
	t.Chdir(t.TempDir())
	opts := Options{Entry: filepath.Join(root, "hello.nx"), Runtime: fakeRuntime(t), Diag: &bytes.Buffer{}}
	plan, err := MakePlan(opts)
	if err != nil {
		t.Fatal(err)
	}
	out, _, err := Write(plan, opts)
	if err != nil {
		t.Fatal(err)
	}
	want := "hello"
	if runtime.GOOS == "windows" {
		want += ".exe"
	}
	if out != want {
		t.Fatalf("output %q, want %q", out, want)
	}
	if _, err := os.Stat(want); err != nil {
		t.Fatalf("default output must land in the cwd: %v", err)
	}
}

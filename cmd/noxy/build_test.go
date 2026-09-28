// cmd/noxy/build_test.go
package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/noxylang/noxy/internal/bundle"
	"github.com/noxylang/noxy/internal/ext/exttest"
)

func exeSuffix() string {
	if runtime.GOOS == "windows" {
		return ".exe"
	}
	return ""
}

// buildNoxy compila o noxy deste checkout (como sync_flags_test.go).
func buildNoxy(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "noxy"+exeSuffix())
	build := exec.Command("go", "build", "-o", bin, ".")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	return bin
}

func repoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	return root
}

// appEnv: ambiente do app sem as variaveis do noxy, mais as pedidas.
func appEnv(extra ...string) []string {
	env := make([]string, 0, len(os.Environ())+len(extra))
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, "NOXY_INTERPRETER=") || strings.HasPrefix(kv, "NOXY_APP_CACHE=") || strings.HasPrefix(kv, "NOXY_PATH=") {
			continue
		}
		env = append(env, kv)
	}
	return append(env, extra...)
}

func run(t *testing.T, dir string, env []string, name string, args ...string) (string, error) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func buildExampleApp(t *testing.T, bin string) string {
	t.Helper()
	out := filepath.Join(t.TempDir(), "app"+exeSuffix())
	text, err := run(t, repoRoot(t), appEnv(), bin, "build", "noxy_examples/build_app.nx", "--include", "noxy_examples/build_app_assets", "-o", out)
	if err != nil || !strings.Contains(text, "noxy build: wrote "+out) {
		t.Fatalf("build: %v\n%s", err, text)
	}
	return out
}

func TestBuildAppRunsWithoutNoxyLibs(t *testing.T) {
	bin := buildNoxy(t)
	app := buildExampleApp(t, bin)

	cache := t.TempDir()
	cwd := t.TempDir() // sem noxy_libs, sem noxy_examples
	decoy := t.TempDir()
	if err := os.MkdirAll(filepath.Join(decoy, "math_lib"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(decoy, "math_lib", "math_lib.nx"), []byte("func add(a: int, b: int) -> int\n    return -1\nend\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	env := appEnv("NOXY_APP_CACHE="+cache, "NOXY_PATH="+decoy)

	out, err := run(t, cwd, env, app, "um", "dois")
	if err != nil {
		t.Fatalf("app: %v\n%s", err, out)
	}
	for _, want := range []string{"hello from the asset\n", "10 + 20 = 30\n", "args: um dois\n"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "-1") {
		t.Fatalf("the sealed app must ignore NOXY_PATH:\n%s", out)
	}
	entries, err := os.ReadDir(cache)
	if err != nil || len(entries) != 1 {
		t.Fatalf("cache must hold one app dir: %v %v", entries, err)
	}
	marker := filepath.Join(cache, entries[0].Name(), bundle.MarkerName)
	before, err := os.Stat(marker)
	if err != nil {
		t.Fatalf("marker: %v", err)
	}
	if _, err := os.Stat(filepath.Join(cache, entries[0].Name(), "noxy_examples", "build_app_assets", "greeting.txt")); err != nil {
		t.Fatalf("asset must be extracted: %v", err)
	}

	again, err := run(t, cwd, env, app, "um", "dois")
	if err != nil || again != out {
		t.Fatalf("second run: %v\n%s", err, again)
	}
	after, _ := os.Stat(marker)
	if !after.ModTime().Equal(before.ModTime()) {
		t.Fatal("second start must not re-extract")
	}
}

func TestBuildAppInterpreterEscapeHatch(t *testing.T) {
	bin := buildNoxy(t)
	app := buildExampleApp(t, bin)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "other.nx"), []byte("print(\"interp\")\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	env := appEnv("NOXY_APP_CACHE="+t.TempDir(), "NOXY_INTERPRETER=1")
	out, err := run(t, dir, env, app, "other.nx")
	if err != nil || strings.TrimSpace(out) != "interp" {
		t.Fatalf("NOXY_INTERPRETER=1 must run the plain interpreter: %v\n%s", err, out)
	}
	if out, err := run(t, dir, env, app, "--version"); err != nil || !strings.HasPrefix(out, "Noxy v") {
		t.Fatalf("--version under the escape hatch: %v\n%s", err, out)
	}
	second := filepath.Join(t.TempDir(), "app2"+exeSuffix())
	if out, err := run(t, dir, env, app, "build", "other.nx", "-o", second); err != nil {
		t.Fatalf("build from an app: %v\n%s", err, out)
	}
	rt1, err1 := bundle.RuntimeBytes(bin)
	rt2, err2 := bundle.RuntimeBytes(second)
	if err1 != nil || err2 != nil || len(rt1) != len(rt2) {
		t.Fatalf("payload nested: runtime %d vs %d (%v %v)", len(rt1), len(rt2), err1, err2)
	}
	if out, err := run(t, t.TempDir(), appEnv("NOXY_APP_CACHE="+t.TempDir()), second); err != nil || strings.TrimSpace(out) != "interp" {
		t.Fatalf("app built from an app: %v\n%s", err, out)
	}
}

func TestBuildListAndErrorsGoToTheRightStreams(t *testing.T) {
	bin := buildNoxy(t)
	root := repoRoot(t)
	out, err := run(t, root, appEnv(), bin, "build", "--list", "noxy_examples/build_app.nx", "--include", "noxy_examples/build_app_assets")
	if err != nil {
		t.Fatalf("--list: %v\n%s", err, out)
	}
	for _, want := range []string{"entry      noxy_examples/build_app.nx", "modules    ", "  noxy_libs/math_lib/math_lib.nx", "includes   1 files", "  noxy_examples/build_app_assets/greeting.txt"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if _, err := os.Stat(filepath.Join(root, "build_app"+exeSuffix())); err == nil {
		t.Fatal("--list must not write the executable")
	}

	project := t.TempDir()
	if err := os.MkdirAll(filepath.Join(project, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(project, "main.nx"), []byte("use src.bad as bad\nprint(1)\n"), 0o644)
	os.WriteFile(filepath.Join(project, "src", "bad.nx"), []byte("let x: int = \"s\"\n"), 0o644)
	cmd := exec.Command(bin, "build", "main.nx")
	cmd.Dir = project
	cmd.Env = appEnv()
	stdout, stderr := new(strings.Builder), new(strings.Builder)
	cmd.Stdout, cmd.Stderr = stdout, stderr
	err = cmd.Run()
	if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() != 1 {
		t.Fatalf("compile error in a module must exit 1: %v", err)
	}
	if !strings.Contains(stderr.String(), "noxy build: src/bad.nx: ") || stdout.String() != "" {
		t.Fatalf("stdout %q stderr %q", stdout.String(), stderr.String())
	}
	if _, err := os.Stat(filepath.Join(project, "main"+exeSuffix())); err == nil {
		t.Fatal("a failed build must not leave an executable")
	}

	cmd = exec.Command(bin, "build")
	cmd.Env = appEnv()
	if err := cmd.Run(); err == nil || err.(*exec.ExitError).ExitCode() != 2 {
		t.Fatalf("missing entry must exit 2: %v", err)
	}
}

const guestManifest = `
name = "guest"
abi = 1
kind = "process"

[binaries]
%s = "%s"

[[export]]
name = "guest_add"
params = ["int", "int"]
returns = "int"
`

func TestBuildAppWithAProcessExtension(t *testing.T) {
	bin := buildNoxy(t)
	guest := exttest.BuildProcessGuest(t)
	asset := filepath.Base(guest)
	project := t.TempDir()
	pkg := filepath.Join(project, "noxy_libs", "guest")
	if err := os.MkdirAll(filepath.Join(pkg, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"noxy.mod":                      "module app\n",
		"main.nx":                       "use guest as g\nprint(\"sum: \" + to_str(g.add(2, 3)))\n",
		"noxy_libs/guest/noxy_ext.toml": fmt.Sprintf(guestManifest, runtime.GOOS+"-"+runtime.GOARCH, asset),
		"noxy_libs/guest/guest.nx":      "func add(a: int, b: int) -> int\n    return guest_add(a, b)\nend\n",
	}
	for rel, content := range files {
		if err := os.WriteFile(filepath.Join(project, filepath.FromSlash(rel)), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	guestBytes, err := os.ReadFile(guest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pkg, "bin", asset), guestBytes, 0o755); err != nil {
		t.Fatal(err)
	}
	app := filepath.Join(t.TempDir(), "app"+exeSuffix())
	if out, err := run(t, project, appEnv(), bin, "build", "main.nx", "-o", app); err != nil || !strings.Contains(out, "1 extensions (guest)") {
		t.Fatalf("build: %v\n%s", err, out)
	}
	cache := t.TempDir()
	out, err := run(t, t.TempDir(), appEnv("NOXY_APP_CACHE="+cache), app)
	if err != nil || !strings.Contains(out, "sum: 5") {
		t.Fatalf("app with extension: %v\n%s", err, out)
	}
	entries, _ := os.ReadDir(cache)
	if len(entries) != 1 {
		t.Fatalf("cache entries %v", entries)
	}
	extracted := filepath.Join(cache, entries[0].Name(), "noxy_libs", "guest", "bin", asset)
	info, err := os.Stat(extracted)
	if err != nil || (runtime.GOOS != "windows" && info.Mode()&0o111 == 0) {
		t.Fatalf("plugin must be extracted and executable: %v %v", info, err)
	}
}

func TestBuildAppRunsThroughASymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need privileges on Windows")
	}
	bin := buildNoxy(t)
	app := buildExampleApp(t, bin)
	link := filepath.Join(t.TempDir(), "noxy-editor")
	if err := os.Symlink(app, link); err != nil {
		t.Fatal(err)
	}
	out, err := run(t, t.TempDir(), appEnv("NOXY_APP_CACHE="+t.TempDir()), link, "x")
	if err != nil || !strings.Contains(out, "args: x\n") {
		t.Fatalf("through a symlink: %v\n%s", err, out)
	}
}

func TestBuildRejectsModuleOnlyReachableThroughTheCwd(t *testing.T) {
	bin := buildNoxy(t)
	project := t.TempDir()
	files := map[string]string{
		"noxy.mod":   "module app\n",
		"util.nx":    "let greeting: string = \"hi\"\n",
		"src/app.nx": "use util\nprint(util.greeting)\n",
	}
	for rel, content := range files {
		path := filepath.Join(project, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if out, err := run(t, project, appEnv(), bin, "src/app.nx"); err != nil || strings.TrimSpace(out) != "hi" {
		t.Fatalf("the interpreter finds util through the cwd: %v\n%s", err, out)
	}
	cmd := exec.Command(bin, "build", "src/app.nx")
	cmd.Dir = project
	cmd.Env = appEnv()
	stdout, stderr := new(strings.Builder), new(strings.Builder)
	cmd.Stdout, cmd.Stderr = stdout, stderr
	err := cmd.Run()
	if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() != 1 {
		t.Fatalf("build must exit 1: %v\nstdout %q\nstderr %q", err, stdout.String(), stderr.String())
	}
	if !strings.Contains(stderr.String(), "module util resolves through the current directory or NOXY_PATH") || stdout.String() != "" {
		t.Fatalf("stdout %q stderr %q", stdout.String(), stderr.String())
	}
}

func TestBuildAppIgnoresAForeignNoxyModAboveTheCache(t *testing.T) {
	bin := buildNoxy(t)
	write := func(root string, files map[string]string) {
		for rel, content := range files {
			path := filepath.Join(root, filepath.FromSlash(rel))
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	project := t.TempDir()
	write(project, map[string]string{
		"main.nx":                    "use helper\nprint(helper.message())\n",
		"noxy_libs/helper/helper.nx": "func message() -> string\n    return \"own helper\"\nend\n",
	})
	app := filepath.Join(t.TempDir(), "app"+exeSuffix())
	if out, err := run(t, project, appEnv(), bin, "build", "main.nx", "-o", app); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	outer := t.TempDir()
	write(outer, map[string]string{
		"noxy.mod":                   "module foreign\n",
		"noxy_libs/helper/helper.nx": "func message() -> string\n    return \"SHADOWED\"\nend\n",
	})
	out, err := run(t, t.TempDir(), appEnv("NOXY_APP_CACHE="+filepath.Join(outer, "cache")), app)
	if err != nil || strings.TrimSpace(out) != "own helper" {
		t.Fatalf("a noxy.mod above the cache must not shadow the app: %v\n%s", err, out)
	}
}

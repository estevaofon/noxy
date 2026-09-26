package build

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/estevaofon/noxy/internal/ext/exttest"
)

// writeProject grava os arquivos (chaves com "/") num diretorio novo.
func writeProject(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for rel, content := range files {
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// FindRoot resolve symlinks (macOS: /var → /private/var); alinhe o root.
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		root = resolved
	}
	return root
}

func planOf(t *testing.T, root string, entry string, includes ...string) (*Plan, error) {
	t.Helper()
	return MakePlan(Options{Entry: filepath.Join(root, filepath.FromSlash(entry)), Includes: includes, Runtime: os.Args[0], Diag: &bytes.Buffer{}})
}

func names(mods []Module) string {
	out := make([]string, len(mods))
	for i, m := range mods {
		out[i] = m.Name
		if m.Dir {
			out[i] += "/"
		}
	}
	return strings.Join(out, " ")
}

func paths(files []File) string {
	out := make([]string, len(files))
	for i, f := range files {
		out[i] = f.Path
	}
	return strings.Join(out, " ")
}

func TestPlanWalksTransitiveUsesAndDirectoryModules(t *testing.T) {
	root := writeProject(t, map[string]string{
		"noxy.mod":         "module app\n",
		"main.nx":          "use src.a as a\nprint(a.quad(2))\n",
		"src/a.nx":         "use src.b select helper\nuse lib as l\nfunc quad(n: int) -> int\n    return helper(helper(n))\nend\n",
		"src/b.nx":         "func helper(n: int) -> int\n    return n * 2\nend\n",
		"lib/x.nx":         "func one() -> int\n    return 1\nend\n",
		"lib/README.md":    "not a module\n",
		"lib/sub/y.nx":     "let y: int = 2\n",
		"lib/assets/f.txt": "not a module either\n",
	})
	plan, err := planOf(t, root, "main.nx")
	if err != nil {
		t.Fatal(err)
	}
	if plan.Root != root || plan.Entry != "main.nx" || plan.Target != runtime.GOOS+"/"+runtime.GOARCH {
		t.Fatalf("plan %+v", plan)
	}
	if got := names(plan.Modules); got != "src.a src.b lib/ lib.assets/ lib.sub/ lib.sub.y lib.x" {
		t.Fatalf("modules %q", got)
	}
	if got := paths(plan.Files); got != "lib/sub/y.nx lib/x.nx main.nx noxy.mod src/a.nx src/b.nx" {
		t.Fatalf("files %q", got)
	}
}

func TestPlanSkipsEmbeddedStdlibButKeepsLocalShadow(t *testing.T) {
	root := writeProject(t, map[string]string{
		"main.nx":    "use io\nuse strings select shadow\nprint(shadow())\n",
		"strings.nx": "func shadow() -> int\n    return 1\nend\n",
	})
	plan, err := planOf(t, root, "main.nx")
	if err != nil {
		t.Fatal(err)
	}
	if got := paths(plan.Files); got != "main.nx strings.nx" {
		t.Fatalf("files %q (io is embedded, strings.nx shadows the stdlib)", got)
	}
}

func TestPlanReportsCompileErrorWithTheModulePath(t *testing.T) {
	root := writeProject(t, map[string]string{
		"main.nx":    "use src.bad as bad\nprint(1)\n",
		"src/bad.nx": "let x: int = \"not an int\"\n",
	})
	_, err := planOf(t, root, "main.nx")
	if err == nil || !strings.HasPrefix(err.Error(), "src/bad.nx: ") {
		t.Fatalf("want the module path as prefix, got %v", err)
	}
}

func TestPlanRejectsModuleOutsideTheRoot(t *testing.T) {
	far := t.TempDir()
	if err := os.MkdirAll(filepath.Join(far, "far"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(far, "far", "far.nx"), []byte("let f: int = 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("NOXY_PATH", far)
	root := writeProject(t, map[string]string{"main.nx": "use far\nprint(far.f)\n"})
	_, err := planOf(t, root, "main.nx")
	if err == nil || !strings.Contains(err.Error(), "module far resolves to") || !strings.Contains(err.Error(), "outside the project root") {
		t.Fatalf("got %v", err)
	}
}

func TestPlanMissingModuleNamesTheUseSite(t *testing.T) {
	root := writeProject(t, map[string]string{"main.nx": "print(1)\nuse nope\n"})
	_, err := planOf(t, root, "main.nx")
	if err == nil || !strings.Contains(err.Error(), "main.nx:2: module not found: nope") {
		t.Fatalf("got %v", err)
	}
}

func TestPlanIncludesUnionWithoutDuplicates(t *testing.T) {
	root := writeProject(t, map[string]string{
		"noxy.mod":        "module app\n\ninclude web\n",
		"main.nx":         "print(1)\n",
		"web/index.html":  "<html></html>\n",
		"web/vendor/x.js": "1\n",
		"docs/readme.txt": "r\n",
	})
	plan, err := planOf(t, root, "main.nx", "./web/", "web/vendor/x.js", "docs/readme.txt")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(plan.Includes, " ") != "docs/readme.txt web web/vendor/x.js" {
		t.Fatalf("includes %v", plan.Includes)
	}
	if got := paths(plan.Files); got != "docs/readme.txt main.nx noxy.mod web/index.html web/vendor/x.js" {
		t.Fatalf("files %q", got)
	}
	if got := paths(plan.IncludedFiles()); got != "docs/readme.txt web/index.html web/vendor/x.js" {
		t.Fatalf("included %q", got)
	}
	if _, err := planOf(t, root, "main.nx", "assets"); err == nil || !strings.Contains(err.Error(), "include assets not found") {
		t.Fatalf("missing include: %v", err)
	}
	if _, err := planOf(t, root, "main.nx", "../x"); err == nil || !strings.Contains(err.Error(), "outside the project root") {
		t.Fatalf("escaping include: %v", err)
	}
}

func TestPlanEntryInASubdirectoryUsesTheProjectRoot(t *testing.T) {
	root := writeProject(t, map[string]string{
		"noxy.mod":               "module app\n",
		"noxy.sum":               "",
		"examples/app.nx":        "use math\nprint(math.add(1, 2))\n",
		"noxy_libs/math/math.nx": "func add(a: int, b: int) -> int\n    return a + b\nend\n",
	})
	plan, err := planOf(t, root, "examples/app.nx")
	if err != nil {
		t.Fatal(err)
	}
	if plan.Entry != "examples/app.nx" || plan.Root != root {
		t.Fatalf("entry %q root %q", plan.Entry, plan.Root)
	}
	if got := paths(plan.Files); got != "examples/app.nx noxy.mod noxy.sum noxy_libs/math/math.nx" {
		t.Fatalf("files %q", got)
	}
}

func TestPlanRefusesSysLoadPlugin(t *testing.T) {
	root := writeProject(t, map[string]string{"main.nx": "sys_load_plugin(\"old\", \"./old\")\n"})
	_, err := planOf(t, root, "main.nx")
	if err == nil || !strings.Contains(err.Error(), "sys_load_plugin is not supported in built executables (removed in v0.27.0)") {
		t.Fatalf("got %v", err)
	}
}

const testExtManifest = `
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

// writeGuestExtension instala noxy_libs/guest com o guest do SDK em bin/<asset>.
func writeGuestExtension(t *testing.T, root string, platform string) string {
	t.Helper()
	guest := exttest.BuildProcessGuest(t)
	asset := filepath.Base(guest)
	pkg := filepath.Join(root, "noxy_libs", "guest")
	if err := os.MkdirAll(filepath.Join(pkg, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := fmt.Sprintf(testExtManifest, platform, asset)
	if err := os.WriteFile(filepath.Join(pkg, "noxy_ext.toml"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	wrapper := "func add(a: int, b: int) -> int\n    return guest_add(a, b)\nend\n"
	if err := os.WriteFile(filepath.Join(pkg, "guest.nx"), []byte(wrapper), 0o644); err != nil {
		t.Fatal(err)
	}
	bin, err := os.ReadFile(guest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pkg, "bin", asset), bin, 0o755); err != nil {
		t.Fatal(err)
	}
	return asset
}

func TestPlanRecordsTheProcessExtensionOfThisPlatform(t *testing.T) {
	root := writeProject(t, map[string]string{"noxy.mod": "module app\n", "main.nx": "use guest as g\nprint(g.add(2, 3))\n"})
	asset := writeGuestExtension(t, root, runtime.GOOS+"-"+runtime.GOARCH)
	plan, err := planOf(t, root, "main.nx")
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Extensions) != 1 {
		t.Fatalf("extensions %+v", plan.Extensions)
	}
	e := plan.Extensions[0]
	bin, _ := os.ReadFile(filepath.Join(root, "noxy_libs", "guest", "bin", asset))
	sum := sha256.Sum256(bin)
	if e.Name != "guest" || e.Kind != "process" || e.Module != "guest" || e.Dir != "noxy_libs/guest" || e.Artifact != "bin/"+asset || e.SHA256 != hex.EncodeToString(sum[:]) || e.Size != int64(len(bin)) {
		t.Fatalf("extension %+v", e)
	}
	if got := paths(plan.Files); got != "main.nx noxy.mod noxy_libs/guest/bin/"+asset+" noxy_libs/guest/guest.nx noxy_libs/guest/noxy_ext.toml" {
		t.Fatalf("files %q", got)
	}
	for _, f := range plan.Files {
		if strings.HasPrefix(f.Path, "noxy_libs/guest/bin/") && !f.Executable {
			t.Fatalf("%s must be executable", f.Path)
		}
	}
	m := plan.Manifest()
	if m.Extensions[0].SHA256 != e.SHA256 || m.Entry != "main.nx" || m.Format != 1 || m.Kind != "source" {
		t.Fatalf("manifest %+v", m)
	}

	if err := os.RemoveAll(filepath.Join(root, "noxy_libs", "guest", "bin")); err != nil {
		t.Fatal(err)
	}
	_, err = planOf(t, root, "main.nx")
	if err == nil || !strings.Contains(err.Error(), "binary bin/"+asset+" not found — run 'noxy --sync' to download it") {
		t.Fatalf("missing binary: %v", err)
	}
}

func TestPlanRejectsAnExtensionWithoutThisPlatform(t *testing.T) {
	root := writeProject(t, map[string]string{"noxy.mod": "module app\n", "main.nx": "use guest as g\nprint(g.add(2, 3))\n"})
	writeGuestExtension(t, root, "plan9-mips")
	_, err := planOf(t, root, "main.nx")
	if err == nil || !strings.Contains(err.Error(), "has no binary for "+runtime.GOOS+"/"+runtime.GOARCH) || !strings.Contains(err.Error(), "published: plan9/mips") {
		t.Fatalf("got %v", err)
	}
}

func TestListPrintsModulesExtensionsAndIncludes(t *testing.T) {
	root := writeProject(t, map[string]string{
		"noxy.mod":       "module app\n\ninclude web\n",
		"main.nx":        "use src.a as a\nuse guest as g\nprint(a.one() + g.add(1, 1))\n",
		"src/a.nx":       "func one() -> int\n    return 1\nend\n",
		"web/index.html": "<html></html>\n",
	})
	asset := writeGuestExtension(t, root, runtime.GOOS+"-"+runtime.GOARCH)
	plan, err := planOf(t, root, "main.nx")
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := plan.List(&out); err != nil {
		t.Fatal(err)
	}
	text := out.String()
	for _, want := range []string{"entry      main.nx", "target     " + runtime.GOOS + "/" + runtime.GOARCH, "modules    2", "  src/a.nx", "  noxy_libs/guest/guest.nx", "extensions 1", "  guest  process  " + runtime.GOOS + "/" + runtime.GOARCH + "  noxy_libs/guest/bin/" + asset, "includes   1 files", "  web/index.html"} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q in:\n%s", want, text)
		}
	}
	if HumanSize(1536) != "1.5 KB" || HumanSize(23826816) != "22.7 MB" || HumanSize(12) != "12 B" {
		t.Fatalf("HumanSize: %s %s %s", HumanSize(1536), HumanSize(23826816), HumanSize(12))
	}
}

func TestPlanFollowsSymlinkedIncludes(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need privileges on Windows")
	}
	root := writeProject(t, map[string]string{
		"main.nx":    "print(1)\n",
		"real/b.txt": "b\n",
	})
	if err := os.Symlink("real", filepath.Join(root, "web")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("b.txt", filepath.Join(root, "real", "link.txt")); err != nil {
		t.Fatal(err)
	}
	plan, err := planOf(t, root, "main.nx", "web")
	if err != nil {
		t.Fatal(err)
	}
	if got := paths(plan.IncludedFiles()); got != "web/b.txt web/link.txt" {
		t.Fatalf("included %q (files %q)", got, paths(plan.Files))
	}
}

func TestPlanRejectsAnEmptyInclude(t *testing.T) {
	root := writeProject(t, map[string]string{"main.nx": "print(1)\n"})
	if err := os.MkdirAll(filepath.Join(root, "empty"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := planOf(t, root, "main.nx", "empty"); err == nil || !strings.Contains(err.Error(), "include empty contains no files") {
		t.Fatalf("got %v", err)
	}
}

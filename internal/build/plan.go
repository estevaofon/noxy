// Package build monta o payload do `noxy build` (spec 2026-09-26 §6): caminha
// o grafo de `use` com a MESMA resolucao de `noxy entry.nx`, compila cada
// modulo no build (sem executar) e recolhe extensoes da plataforma e assets.
package build

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/estevaofon/noxy/internal/ast"
	"github.com/estevaofon/noxy/internal/bundle"
	"github.com/estevaofon/noxy/internal/compiler"
	"github.com/estevaofon/noxy/internal/ext"
	"github.com/estevaofon/noxy/internal/lexer"
	"github.com/estevaofon/noxy/internal/modsrc"
	"github.com/estevaofon/noxy/internal/parser"
	"github.com/estevaofon/noxy/internal/pkgmanager"
	"github.com/estevaofon/noxy/internal/version"
	"github.com/estevaofon/noxy/internal/vm"
)

type Options struct {
	Entry    string    // entry.nx (qualquer forma; vira absoluto)
	Output   string    // -o; "" = nome do entry sem .nx, no cwd
	Includes []string  // --include, relativos a raiz do projeto
	Runtime  string    // binario a copiar (o noxy que esta rodando)
	Out      io.Writer // progresso (stdout); nil = io.Discard
	Diag     io.Writer // avisos (stderr); nil = os.Stderr
}

type Module struct {
	Name string
	Path string // relativo a raiz, "/"
	Dir  bool
}

type Extension struct {
	Name, Kind, Module, Dir, Artifact, SHA256 string
	Size                                      int64
}

type File struct {
	Path       string // relativo a raiz, "/"
	Size       int64
	Executable bool
	Abs        string
}

type Plan struct {
	Root       string // raiz do projeto (dir do noxy.mod, ou do entry)
	Entry      string // relativo a raiz
	Target     string // "goos/goarch" (v1: o host)
	Runtime    string
	Modules    []Module
	Extensions []Extension
	Includes   []string
	Files      []File
}

var errSysLoadPlugin = errors.New("sys_load_plugin is not supported in built executables (removed in v0.27.0)")

type walker struct {
	plan   *Plan
	src    modsrc.Source
	sealed modsrc.Source // a resolucao do app (sem cwd, sem NOXY_PATH)
	vm     *vm.VM
	seen   map[string]bool
	files  map[string]File
}

// MakePlan resolve a raiz, compila o entry, caminha e compila os modulos,
// recolhe extensoes e includes. Nao escreve nada.
func MakePlan(opts Options) (*Plan, error) {
	diag := opts.Diag
	if diag == nil {
		diag = os.Stderr
	}
	entryAbs, err := filepath.Abs(opts.Entry)
	if err != nil {
		return nil, err
	}
	if info, err := os.Stat(entryAbs); err != nil || info.IsDir() {
		return nil, fmt.Errorf("entry file not found: %s", opts.Entry)
	}
	if resolved, err := filepath.EvalSymlinks(entryAbs); err == nil {
		entryAbs = resolved
	}
	entryDir := filepath.Dir(entryAbs)
	projectRoot, hasMod := pkgmanager.FindRoot(entryDir)
	root := entryDir
	if hasMod {
		root = projectRoot
	} else {
		projectRoot = ""
	}
	p := &Plan{Root: root, Target: runtime.GOOS + "/" + runtime.GOARCH, Runtime: opts.Runtime}
	entryRel, err := relUnder(root, entryAbs)
	if err != nil {
		return nil, fmt.Errorf("entry %s is outside the project root %s", opts.Entry, root)
	}
	p.Entry = entryRel

	machine := vm.NewWithConfig(vm.VMConfig{RootPath: entryDir, ProjectRoot: projectRoot})
	defer machine.CloseExtensions()
	w := &walker{plan: p, src: machine.Config.Source, sealed: modsrc.NewSealed(entryDir, projectRoot), vm: machine, seen: map[string]bool{}, files: map[string]File{}}

	content, err := os.ReadFile(entryAbs)
	if err != nil {
		return nil, err
	}
	program, err := parseSource(entryRel, string(content))
	if err != nil {
		return nil, err
	}
	if len(compiler.PluginNativeNames(program)) != 0 {
		return nil, errSysLoadPlugin
	}
	if err := w.addFile(entryRel, entryAbs, false); err != nil {
		return nil, err
	}
	// O entry compila como em runWithConfig: so os nativos da VM sao
	// conhecidos (as extensoes so entram quando os modulos carregam).
	c := compiler.NewWithStateAndRoot(make(map[string]ast.NoxyType), make(map[string]*ast.StructStatement), entryAbs, entryDir)
	c.SetModuleSource(machine.Config.Source)
	c.SetKnownGlobals(machine.GlobalNames())
	_, _, entryErr := c.Compile(program)
	for _, warning := range c.Warnings() {
		fmt.Fprintln(diag, warning)
	}
	// Um erro num modulo e mais especifico que o do entry (que pode ser so
	// "failed to resolve wildcard module"): a caminhada roda mesmo assim e
	// tem prioridade.
	if err := w.visitUses(program, entryRel); err != nil {
		return nil, err
	}
	if entryErr != nil {
		return nil, fmt.Errorf("%s: %v", entryRel, entryErr)
	}
	for _, name := range []string{"noxy.mod", "noxy.sum"} {
		abs := filepath.Join(root, name)
		if info, err := os.Stat(abs); err == nil && !info.IsDir() {
			if err := w.addFile(name, abs, false); err != nil {
				return nil, err
			}
		}
	}
	var modIncludes []string
	if hasMod {
		cfg, err := pkgmanager.ParseModFile(filepath.Join(root, "noxy.mod"))
		if err != nil {
			return nil, err
		}
		modIncludes = cfg.Include
	}
	if err := w.addIncludes(modIncludes, opts.Includes); err != nil {
		return nil, err
	}
	p.Files = make([]File, 0, len(w.files))
	for _, f := range w.files {
		p.Files = append(p.Files, f)
	}
	sort.Slice(p.Files, func(i, j int) bool { return p.Files[i].Path < p.Files[j].Path })
	return p, nil
}

func parseSource(rel, content string) (*ast.Program, error) {
	p := parser.New(lexer.New(content))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		return nil, fmt.Errorf("%s: %s", rel, strings.Join(p.Errors(), "; "))
	}
	return program, nil
}

// relUnder: caminho relativo a root, "/"; lexical primeiro (um package
// linkado por symlink DENTRO de noxy_libs continua valendo), com
// EvalSymlinks como segunda chance (macOS /var → /private/var).
func relUnder(root, abs string) (string, error) {
	try := func(p string) (string, bool) {
		rel, err := filepath.Rel(root, p)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return "", false
		}
		return filepath.ToSlash(rel), true
	}
	if rel, ok := try(abs); ok {
		return rel, nil
	}
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		if rel, ok := try(resolved); ok {
			return rel, nil
		}
	}
	return "", fmt.Errorf("%s is outside %s", abs, root)
}

// visitUses percorre TODO UseStmt do programa (inclusive dentro de corpos
// de funcao), na ordem do fonte.
func (w *walker) visitUses(program *ast.Program, from string) error {
	var walkErr error
	ast.Inspect(program, func(node ast.Node) bool {
		if walkErr != nil {
			return false
		}
		use, ok := node.(*ast.UseStmt)
		if !ok {
			return true
		}
		walkErr = w.visitModule(use.Module, from, use.Token.Line)
		return walkErr == nil
	})
	return walkErr
}

func (w *walker) visitModule(name, from string, line int) error {
	name = strings.TrimSpace(name)
	if w.seen[name] {
		return nil
	}
	w.seen[name] = true
	m, err := w.src.Resolve(name)
	if err != nil {
		if errors.Is(err, modsrc.ErrNotFound) {
			return fmt.Errorf("%s:%d: module not found: %s%s", from, line, name, pkgmanager.SyncHint(w.vm.Config.ProjectRoot, name))
		}
		return err
	}
	var rel string
	if m.Kind != modsrc.KindEmbedded {
		if rel, err = relUnder(w.plan.Root, m.Path); err != nil {
			return fmt.Errorf("module %s resolves to %s, outside the project root %s", name, m.Path, w.plan.Root)
		}
	}
	if err := w.checkSealed(name, m, rel); err != nil {
		return err
	}
	switch m.Kind {
	case modsrc.KindEmbedded:
		return nil
	case modsrc.KindFile:
		w.plan.Modules = append(w.plan.Modules, Module{Name: name, Path: rel})
		if err := w.addFile(rel, m.Path, false); err != nil {
			return err
		}
		if err := w.visitExtension(m, rel); err != nil {
			return err
		}
		if err := w.vm.CompileModule(m); err != nil {
			return fmt.Errorf("%s: %v", rel, err)
		}
		content, err := w.src.ReadFile(m.Path)
		if err != nil {
			return err
		}
		program, err := parseSource(rel, string(content))
		if err != nil {
			return err
		}
		if len(compiler.PluginNativeNames(program)) != 0 {
			return errSysLoadPlugin
		}
		return w.visitUses(program, rel)
	case modsrc.KindDirectory:
		w.plan.Modules = append(w.plan.Modules, Module{Name: name, Path: rel, Dir: true})
		entries, err := w.src.ReadDir(m.Path)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			if entry.IsDir {
				// A VM ignora um subdiretorio que nao resolve (loadResolvedDirectory).
				if _, err := w.src.Resolve(name + "." + entry.Name); err != nil {
					continue
				}
				if err := w.visitModule(name+"."+entry.Name, rel, 0); err != nil {
					return err
				}
				continue
			}
			if !strings.HasSuffix(entry.Name, ".nx") {
				continue
			}
			if err := w.visitModule(name+"."+strings.TrimSuffix(entry.Name, ".nx"), rel, 0); err != nil {
				return err
			}
		}
		return nil
	}
	return nil
}

// checkSealed: o app resolve com a DiskSource selada (mesmo Root e
// ProjectRoot, relativos ao diretorio extraido); um modulo que so o cwd ou o
// NOXY_PATH acham passaria no build e faltaria no app (spec §6.2). Mesmo
// Kind e mesmo caminho (ou o mesmo caminho no payload, para um NOXY_PATH que
// aponta para dentro da raiz por outro caminho) sao exigidos.
func (w *walker) checkSealed(name string, m modsrc.Module, rel string) error {
	sealed, err := w.sealed.Resolve(name)
	if err == nil && sealed.Kind == m.Kind {
		if m.Kind == modsrc.KindEmbedded || filepath.Clean(sealed.Path) == filepath.Clean(m.Path) {
			return nil
		}
		if sealedRel, err := relUnder(w.plan.Root, sealed.Path); err == nil && sealedRel == rel {
			return nil
		}
	}
	where := m.Path
	if m.Kind == modsrc.KindEmbedded {
		where = "embedded stdlib"
	}
	return fmt.Errorf("module %s resolves through the current directory or NOXY_PATH (%s); a built executable only searches the project — move it under noxy_libs/ or next to the entry", name, where)
}

// visitExtension: noxy_ext.toml ao lado do modulo → manifesto + artefato da
// plataforma no payload (spec §6.4).
func (w *walker) visitExtension(m modsrc.Module, rel string) error {
	dir := filepath.Dir(m.Path)
	manifestPath := filepath.Join(dir, "noxy_ext.toml")
	manifestData, err := w.src.ReadFile(manifestPath)
	if err != nil {
		return nil
	}
	manifest, err := ext.ParseManifest(manifestData)
	if err != nil {
		return err
	}
	relDir := path.Dir(rel)
	if err := w.addFile(relDir+"/noxy_ext.toml", manifestPath, false); err != nil {
		return err
	}
	artifact := manifest.Wasm
	executable := false
	if manifest.Kind == ext.KindProcess {
		goos, goarch, _ := strings.Cut(w.plan.Target, "/")
		asset, ok := manifest.BinaryFor(goos, goarch)
		if !ok {
			return fmt.Errorf("extension %q has no binary for %s (published: %s)", manifest.Name, w.plan.Target, strings.Join(manifest.PublishedPlatforms(), ", "))
		}
		artifact = "bin/" + asset
		executable = true
	}
	abs := filepath.Join(dir, filepath.FromSlash(artifact))
	data, err := os.ReadFile(abs)
	if err != nil {
		if os.IsNotExist(err) && manifest.Kind == ext.KindProcess {
			return fmt.Errorf("extension %q: binary %s not found — run 'noxy --sync' to download it", manifest.Name, artifact)
		}
		return fmt.Errorf("extension %q: %w", manifest.Name, err)
	}
	sum := sha256.Sum256(data)
	w.plan.Extensions = append(w.plan.Extensions, Extension{Name: manifest.Name, Kind: manifest.Kind, Module: m.Name, Dir: relDir, Artifact: artifact, SHA256: hex.EncodeToString(sum[:]), Size: int64(len(data))})
	return w.addFile(relDir+"/"+artifact, abs, executable)
}

func (w *walker) addFile(rel, abs string, executable bool) error {
	rel = path.Clean(filepath.ToSlash(rel))
	if !bundle.ValidPath(rel) {
		return fmt.Errorf("invalid payload path %q", rel)
	}
	if rel == bundle.ManifestName || rel == bundle.MarkerName {
		return fmt.Errorf("%s is a reserved name in the project root", rel)
	}
	if _, dup := w.files[rel]; dup {
		return nil
	}
	info, err := os.Stat(abs)
	if err != nil {
		return err
	}
	if runtime.GOOS != "windows" && info.Mode()&0o111 != 0 {
		executable = true
	}
	w.files[rel] = File{Path: rel, Size: info.Size(), Executable: executable, Abs: abs}
	return nil
}

// addIncludes: uniao de `include` do noxy.mod e --include, sem duplicata,
// validados; diretorio entra recursivamente. Um include que e symlink e
// seguido (o caminho no payload continua o lexical, <include>/<rel>); dentro
// dele, symlink para arquivo entra (lido pelo link), symlink para diretorio
// nao e seguido (spec §6.5). Include sem nenhum arquivo e erro.
func (w *walker) addIncludes(modIncludes, flagIncludes []string) error {
	seen := map[string]bool{}
	for _, raw := range append(append([]string{}, modIncludes...), flagIncludes...) {
		clean, err := pkgmanager.ValidateIncludePath(raw)
		if err != nil {
			return err
		}
		if seen[clean] {
			continue
		}
		seen[clean] = true
		w.plan.Includes = append(w.plan.Includes, clean)
		abs := filepath.Join(w.plan.Root, filepath.FromSlash(clean))
		info, err := os.Stat(abs)
		if err != nil {
			return fmt.Errorf("include %s not found", clean)
		}
		if !info.IsDir() {
			if err := w.addFile(clean, abs, false); err != nil {
				return err
			}
			continue
		}
		walkRoot, err := filepath.EvalSymlinks(abs)
		if err != nil {
			return fmt.Errorf("include %s: %w", clean, err)
		}
		count := 0
		err = filepath.WalkDir(walkRoot, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			if !d.Type().IsRegular() {
				if d.Type()&fs.ModeSymlink == 0 {
					return nil
				}
				if target, err := os.Stat(p); err != nil || !target.Mode().IsRegular() {
					return nil
				}
			}
			rel, err := filepath.Rel(walkRoot, p)
			if err != nil {
				return err
			}
			count++
			return w.addFile(clean+"/"+filepath.ToSlash(rel), p, false)
		})
		if err != nil {
			return err
		}
		if count == 0 {
			return fmt.Errorf("include %s contains no files", clean)
		}
	}
	sort.Strings(w.plan.Includes)
	return nil
}

// IncludedFiles: os arquivos do payload que vieram dos includes.
func (p *Plan) IncludedFiles() []File {
	out := make([]File, 0)
	for _, f := range p.Files {
		for _, include := range p.Includes {
			if f.Path == include || strings.HasPrefix(f.Path, include+"/") {
				out = append(out, f)
				break
			}
		}
	}
	return out
}

func (p *Plan) Manifest() *bundle.Manifest {
	m := &bundle.Manifest{Format: bundle.FormatVersion, Kind: bundle.KindSource, Noxy: version.Version, Target: p.Target, Entry: p.Entry,
		Modules: make([]bundle.ModuleEntry, 0, len(p.Modules)), Extensions: make([]bundle.ExtensionEntry, 0, len(p.Extensions)), Includes: append([]string{}, p.Includes...)}
	for _, mod := range p.Modules {
		m.Modules = append(m.Modules, bundle.ModuleEntry{Name: mod.Name, Path: mod.Path, Dir: mod.Dir})
	}
	for _, e := range p.Extensions {
		m.Extensions = append(m.Extensions, bundle.ExtensionEntry{Name: e.Name, Kind: e.Kind, Module: e.Module, Dir: e.Dir, Artifact: e.Artifact, SHA256: e.SHA256})
	}
	return m
}

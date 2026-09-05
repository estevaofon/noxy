package vm

import (
	"errors"
	"fmt"
	"noxy-vm/internal/ast"
	"noxy-vm/internal/compiler"
	"noxy-vm/internal/lexer"
	"noxy-vm/internal/parser"
	"noxy-vm/internal/pkgmanager"
	"noxy-vm/internal/stdlib"
	"noxy-vm/internal/value"
	"os"
	"path/filepath"
	"strings"
)

type resolvedModuleKind uint8

const (
	resolvedEmbeddedModule resolvedModuleKind = iota
	resolvedFileModule
	resolvedDirectoryModule
)

type resolvedModule struct {
	Key     moduleKey
	Name    string
	Kind    resolvedModuleKind
	Path    string
	Content string
}

func (vm *VM) resolveModule(name string) (resolvedModule, error) {
	root, err := filepath.Abs(vm.Config.RootPath)
	if err != nil {
		return resolvedModule{}, fmt.Errorf("resolve module root: %w", err)
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		if !os.IsNotExist(err) {
			return resolvedModule{}, fmt.Errorf("resolve module root: %w", err)
		}
		root = filepath.Clean(root)
	}
	canonicalName := strings.TrimSpace(name)
	key := moduleKey{Root: root + "\x00" + os.Getenv("NOXY_PATH"), Name: canonicalName}
	pathName := strings.ReplaceAll(canonicalName, ".", string(filepath.Separator))

	checkLocations := func(suffix string) (string, bool, bool) {
		candidates := make([]string, 0, 16)
		if noxyPath := os.Getenv("NOXY_PATH"); noxyPath != "" {
			for _, searchRoot := range filepath.SplitList(noxyPath) {
				candidates = append(candidates,
					filepath.Join(searchRoot, suffix, suffix+".nx"),
					filepath.Join(searchRoot, suffix),
					filepath.Join(searchRoot, suffix+".nx"),
				)
			}
		}
		if project := vm.Config.ProjectRoot; project != "" {
			candidates = append(candidates,
				filepath.Join(project, "noxy_libs", suffix, suffix+".nx"),
				filepath.Join(project, "noxy_libs", suffix),
			)
		}
		candidates = append(candidates,
			filepath.Join(vm.Config.RootPath, "noxy_libs", suffix, suffix+".nx"),
			filepath.Join(vm.Config.RootPath, "noxy_libs", suffix),
			filepath.Join(vm.Config.RootPath, "stdlib", suffix),
			filepath.Join(vm.Config.RootPath, suffix),
			filepath.Join("noxy_libs", suffix, suffix+".nx"),
			filepath.Join("noxy_libs", suffix),
			filepath.Join("stdlib", suffix),
			suffix,
		)
		for _, candidate := range candidates {
			info, statErr := os.Stat(candidate)
			if statErr == nil {
				absolute, absErr := filepath.Abs(candidate)
				if absErr != nil {
					return "", false, false
				}
				return filepath.Clean(absolute), info.IsDir(), true
			}
		}
		return "", false, false
	}

	path, isDir, found := checkLocations(pathName + ".nx")
	if !found || isDir {
		path, isDir, found = checkLocations(pathName)
	}
	if found {
		if isDir {
			baseName := filepath.Base(path)
			for _, candidate := range []string{baseName + ".nx", "main.nx"} {
				entryPath := filepath.Join(path, candidate)
				if info, statErr := os.Stat(entryPath); statErr == nil && !info.IsDir() {
					return resolvedModule{Key: key, Name: canonicalName, Kind: resolvedFileModule, Path: entryPath}, nil
				}
			}
			return resolvedModule{Key: key, Name: canonicalName, Kind: resolvedDirectoryModule, Path: path}, nil
		}
		return resolvedModule{Key: key, Name: canonicalName, Kind: resolvedFileModule, Path: path}, nil
	}

	content, readErr := stdlib.FS.ReadFile(pathName + ".nx")
	if readErr != nil {
		return resolvedModule{}, fmt.Errorf("module not found: %s%s", canonicalName, vm.syncHint(canonicalName))
	}
	return resolvedModule{Key: key, Name: canonicalName, Kind: resolvedEmbeddedModule, Content: string(content)}, nil
}

// syncHint: so no caminho de erro, le <ProjectRoot>/noxy.mod e, se o modulo
// pedido e (ou esta sob) uma dependencia declarada, aponta o comando (spec §6).
func (vm *VM) syncHint(moduleName string) string {
	if vm.Config.ProjectRoot == "" {
		return ""
	}
	cfg, err := pkgmanager.ParseModFile(filepath.Join(vm.Config.ProjectRoot, "noxy.mod"))
	if err != nil {
		return ""
	}
	for _, module := range cfg.Requires() {
		local := strings.ReplaceAll(pkgmanager.LocalPath(module), "/", ".")
		if moduleName == local || strings.HasPrefix(moduleName, local+".") {
			return " (required by noxy.mod) — run 'noxy --sync'"
		}
	}
	return ""
}

func (vm *VM) loadModule(name string) (value.Value, error) {
	source, err := vm.resolveModule(name)
	if err != nil {
		return value.NewNull(), err
	}
	var parent *moduleKey
	if count := len(vm.moduleLoadStack); count != 0 {
		parentKey := vm.moduleLoadStack[count-1]
		parent = &parentKey
	}
	return vm.shared.Modules.Do(source.Key, parent, func() (value.Value, error) {
		vm.moduleLoadStack = append(vm.moduleLoadStack, source.Key)
		defer func() {
			vm.moduleLoadStack = vm.moduleLoadStack[:len(vm.moduleLoadStack)-1]
		}()
		return vm.loadResolvedModule(source)
	})
}

func (vm *VM) loadResolvedModule(source resolvedModule) (value.Value, error) {
	switch source.Kind {
	case resolvedDirectoryModule:
		return vm.loadResolvedDirectory(source)
	case resolvedEmbeddedModule:
		return vm.compileAndRunModule(source, source.Content)
	case resolvedFileModule:
		// Deteccao de extensao: se o pacote do modulo tem noxy_ext.toml ao
		// lado, carrega o WASM e registra os exports como natives ANTES de
		// compilar o wrapper .nx (que referencia esses natives).
		manifestPath := filepath.Join(filepath.Dir(source.Path), "noxy_ext.toml")
		if _, statErr := os.Stat(manifestPath); statErr == nil {
			if err := vm.ensureExtensionLoaded(filepath.Dir(source.Path)); err != nil {
				return value.NewNull(), fmt.Errorf("failed to load extension for module %s: %w", source.Name, err)
			}
		}
		content, err := os.ReadFile(source.Path)
		if err != nil {
			return value.NewNull(), err
		}
		text := string(content)
		if err := requireValidUTF8("module "+source.Path, text); err != nil {
			return value.NewNull(), err
		}
		return vm.compileAndRunModule(source, text)
	default:
		return value.NewNull(), fmt.Errorf("unknown resolved module kind for %s", source.Name)
	}
}

func (vm *VM) loadResolvedDirectory(source resolvedModule) (value.Value, error) {
	files, err := os.ReadDir(source.Path)
	if err != nil {
		return value.NewNull(), err
	}
	moduleEnvironment := value.NewGlobalEnvironment(vm.shared.Root)
	for _, file := range files {
		if file.IsDir() {
			submoduleName := source.Name + "." + file.Name()
			submodule, loadErr := vm.loadModule(submoduleName)
			if loadErr != nil {
				var cycleErr *moduleCycleError
				if errors.As(loadErr, &cycleErr) {
					return value.NewNull(), fmt.Errorf("failed to load submodule %s: %w", submoduleName, loadErr)
				}
				continue
			}
			moduleEnvironment.SetLocal(file.Name(), submodule)
			continue
		}
		if !strings.HasSuffix(file.Name(), ".nx") {
			continue
		}
		baseName := strings.TrimSuffix(file.Name(), ".nx")
		submoduleName := source.Name + "." + baseName
		submodule, loadErr := vm.loadModule(submoduleName)
		if loadErr != nil {
			return value.NewNull(), fmt.Errorf("failed to load submodule %s: %w", submoduleName, loadErr)
		}
		moduleEnvironment.SetLocal(baseName, submodule)
	}
	return moduleEnvironment.ExportMap(), nil
}

func (vm *VM) compileAndRunModule(source resolvedModule, content string) (value.Value, error) {
	l := lexer.New(content)
	p := parser.New(l)
	program := p.ParseProgram()
	if len(p.Errors()) > 0 {
		if source.Kind == resolvedEmbeddedModule {
			return value.NewNull(), fmt.Errorf("parse error in embedded module %s: %v", source.Name, p.Errors())
		}
		return value.NewNull(), fmt.Errorf("parse error in module %s: %v", source.Name, p.Errors())
	}
	compilerPath := source.Path
	if source.Kind == resolvedEmbeddedModule {
		compilerPath = source.Name
	}
	c := compiler.NewWithStateAndRoot(make(map[string]ast.NoxyType), make(map[string]*ast.StructStatement), compilerPath, vm.Config.RootPath)
	// Issue #47 parte 3: o modulo enxerga os nativos ja registrados na raiz
	// (inclusive os da extensao carregada logo acima) e os que o proprio
	// modulo registra via sys_load_plugin.
	c.SetKnownGlobals(append(vm.GlobalNames(), compiler.PluginNativeNames(program)...))
	code, _, err := c.Compile(program)
	// Aviso do compilador de um modulo carregado em runtime e diagnostico
	// da VM: os.Stderr (AGENTS.md, regra "Saida"), nunca stdout (issue #61 item 3).
	for _, warning := range c.Warnings() {
		fmt.Fprintln(os.Stderr, warning)
	}
	if err != nil {
		return value.NewNull(), err
	}
	moduleEnvironment := value.NewGlobalEnvironment(vm.shared.Root)
	modFn := &value.ObjFunction{Name: source.Name, Arity: 0, Chunk: code, Environment: moduleEnvironment}
	modClosure := &value.ObjClosure{Function: modFn, Upvalues: []*value.ObjUpvalue{}, Environment: moduleEnvironment}
	callerFrameCount := vm.frameCount
	vm.push(value.Value{Type: value.VAL_FUNCTION, Obj: modClosure})
	if ok, callErr := vm.callValue(vm.peek(0), 0, nil, 0); !ok {
		return value.NewNull(), callErr
	}
	startFrameCount := vm.frameCount
	if err := vm.run(startFrameCount, nil); err != nil {
		return value.NewNull(), err
	}
	if callerFrameCount > 0 {
		vm.pop()
	}
	return moduleEnvironment.ExportMap(), nil
}

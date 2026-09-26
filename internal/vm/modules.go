package vm

import (
	"errors"
	"fmt"
	"github.com/estevaofon/noxy/internal/ast"
	"github.com/estevaofon/noxy/internal/chunk"
	"github.com/estevaofon/noxy/internal/compiler"
	"github.com/estevaofon/noxy/internal/lexer"
	"github.com/estevaofon/noxy/internal/modsrc"
	"github.com/estevaofon/noxy/internal/parser"
	"github.com/estevaofon/noxy/internal/pkgmanager"
	"github.com/estevaofon/noxy/internal/value"
	"os"
	"path/filepath"
	"strings"
)

type resolvedModule struct {
	Key    moduleKey
	Module modsrc.Module
}

// resolveModule: Source.Resolve + chave do cache; "module not found" ganha
// a dica de --sync como sempre.
func (vm *VM) resolveModule(name string) (resolvedModule, error) {
	canonicalName := strings.TrimSpace(name)
	m, err := vm.Config.Source.Resolve(canonicalName)
	if err != nil {
		if errors.Is(err, modsrc.ErrNotFound) {
			return resolvedModule{}, fmt.Errorf("module not found: %s%s", canonicalName, pkgmanager.SyncHint(vm.Config.ProjectRoot, canonicalName))
		}
		return resolvedModule{}, err
	}
	return resolvedModule{Key: moduleKey{Root: vm.Config.Source.Key(), Name: canonicalName}, Module: m}, nil
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
	switch source.Module.Kind {
	case modsrc.KindDirectory:
		return vm.loadResolvedDirectory(source)
	case modsrc.KindEmbedded:
		code, err := vm.compileModule(source.Module, source.Module.Content)
		if err != nil {
			return value.NewNull(), err
		}
		return vm.runModule(source, code)
	case modsrc.KindFile:
		content, err := vm.prepareFileModule(source.Module)
		if err != nil {
			return value.NewNull(), err
		}
		code, err := vm.compileModule(source.Module, content)
		if err != nil {
			return value.NewNull(), err
		}
		return vm.runModule(source, code)
	default:
		return value.NewNull(), fmt.Errorf("unknown resolved module kind for %s", source.Module.Name)
	}
}

// prepareFileModule: se o pacote do modulo tem noxy_ext.toml ao lado,
// carrega a extensao e registra os exports como natives ANTES de compilar o
// wrapper .nx (que referencia esses natives); depois le e valida o fonte.
func (vm *VM) prepareFileModule(m modsrc.Module) (string, error) {
	dir := filepath.Dir(m.Path)
	if _, err := vm.Config.Source.ReadFile(filepath.Join(dir, "noxy_ext.toml")); err == nil {
		if err := vm.ensureExtensionLoaded(dir); err != nil {
			return "", fmt.Errorf("failed to load extension for module %s: %w", m.Name, err)
		}
	}
	content, err := vm.Config.Source.ReadFile(m.Path)
	if err != nil {
		return "", err
	}
	text := string(content)
	if err := requireValidUTF8("module "+m.Path, text); err != nil {
		return "", err
	}
	return text, nil
}

func (vm *VM) loadResolvedDirectory(source resolvedModule) (value.Value, error) {
	entries, err := vm.Config.Source.ReadDir(source.Module.Path)
	if err != nil {
		return value.NewNull(), err
	}
	moduleEnvironment := value.NewGlobalEnvironment(vm.shared.Root)
	for _, entry := range entries {
		if entry.IsDir {
			submoduleName := source.Module.Name + "." + entry.Name
			submodule, loadErr := vm.loadModule(submoduleName)
			if loadErr != nil {
				var cycleErr *moduleCycleError
				if errors.As(loadErr, &cycleErr) {
					return value.NewNull(), fmt.Errorf("failed to load submodule %s: %w", submoduleName, loadErr)
				}
				continue
			}
			moduleEnvironment.SetLocal(entry.Name, submodule)
			continue
		}
		if !strings.HasSuffix(entry.Name, ".nx") {
			continue
		}
		baseName := strings.TrimSuffix(entry.Name, ".nx")
		submoduleName := source.Module.Name + "." + baseName
		submodule, loadErr := vm.loadModule(submoduleName)
		if loadErr != nil {
			return value.NewNull(), fmt.Errorf("failed to load submodule %s: %w", submoduleName, loadErr)
		}
		moduleEnvironment.SetLocal(baseName, submodule)
	}
	return moduleEnvironment.ExportMap(), nil
}

// compileModule e a metade "compilar" da carga de um modulo: parse,
// compilador com os nativos ja registrados (inclusive os da extensao
// carregada em prepareFileModule) e a mesma Source da VM.
func (vm *VM) compileModule(m modsrc.Module, content string) (*chunk.Chunk, error) {
	l := lexer.New(content)
	p := parser.New(l)
	program := p.ParseProgram()
	if len(p.Errors()) > 0 {
		if m.Kind == modsrc.KindEmbedded {
			return nil, fmt.Errorf("parse error in embedded module %s: %v", m.Name, p.Errors())
		}
		return nil, fmt.Errorf("parse error in module %s: %v", m.Name, p.Errors())
	}
	compilerPath := m.Path
	if m.Kind == modsrc.KindEmbedded {
		compilerPath = m.Name
	}
	c := compiler.NewWithStateAndRoot(make(map[string]ast.NoxyType), make(map[string]*ast.StructStatement), compilerPath, vm.Config.RootPath)
	c.SetModuleSource(vm.Config.Source)
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
	return code, err
}

// runModule executa o chunk do modulo num ambiente proprio e devolve o
// ExportMap — a metade "executar", inalterada.
func (vm *VM) runModule(source resolvedModule, code *chunk.Chunk) (value.Value, error) {
	moduleEnvironment := value.NewGlobalEnvironment(vm.shared.Root)
	modFn := &value.ObjFunction{Name: source.Module.Name, Arity: 0, Chunk: code, Environment: moduleEnvironment}
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

// CompileModule e a checagem de compilacao do `noxy build` (spec §6.3):
// carrega a extensao ao lado do modulo (sem subir processo), compila com os
// nativos conhecidos e descarta o chunk. Nada executa.
func (vm *VM) CompileModule(m modsrc.Module) error {
	content := m.Content
	if m.Kind == modsrc.KindFile {
		var err error
		if content, err = vm.prepareFileModule(m); err != nil {
			return err
		}
	}
	_, err := vm.compileModule(m, content)
	return err
}

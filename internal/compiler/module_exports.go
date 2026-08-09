package compiler

import (
	"noxy-vm/internal/ast"
	"noxy-vm/internal/lexer"
	"noxy-vm/internal/parser"
	"noxy-vm/internal/stdlib"
	"os"
	"path/filepath"
	"strings"
)

type moduleDiscoveryState struct {
	active   map[string]bool
	failures int
}

func (c *Compiler) discoverModuleExports(module string) map[string]struct{} {
	state := c.moduleDiscovery
	if state == nil {
		state = &moduleDiscoveryState{active: make(map[string]bool)}
	}
	return c.discoverModuleExportsWithState(module, state)
}

func (c *Compiler) discoverModuleExportsWithState(module string, state *moduleDiscoveryState) map[string]struct{} {
	exports := make(map[string]struct{})
	program, directoryExports, ok := c.loadModuleDeclarations(module, state)
	if !ok {
		return exports
	}
	for _, name := range directoryExports {
		exports[name] = struct{}{}
	}
	if program == nil {
		return exports
	}

	for _, statement := range program.Statements {
		switch declaration := statement.(type) {
		case *ast.LetStmt:
			exports[declaration.Name.Value] = struct{}{}
		case *ast.FunctionStatement:
			exports[declaration.Name] = struct{}{}
		case *ast.StructStatement:
			exports[declaration.Name] = struct{}{}
		case *ast.UseStmt:
			switch {
			case declaration.SelectAll:
				// OP_IMPORT_FROM_ALL writes shared VM globals, not the module's
				// returned moduleGlobals map, so wildcard side effects are not exports.
			case len(declaration.Selectors) > 0:
				for _, name := range declaration.Selectors {
					exports[name] = struct{}{}
				}
			default:
				name := declaration.Alias
				if name == "" {
					parts := strings.Split(declaration.Module, ".")
					name = parts[len(parts)-1]
				}
				exports[name] = struct{}{}
			}
		}
	}
	return exports
}

func (c *Compiler) predeclareImport(declaration *ast.UseStmt) {
	switch {
	case declaration.SelectAll:
		for name := range c.discoverModuleExports(declaration.Module) {
			c.globals[name] = nil
		}
	case len(declaration.Selectors) > 0:
		for _, name := range declaration.Selectors {
			c.globals[name] = nil
		}
	default:
		name := declaration.Alias
		if name == "" {
			parts := strings.Split(declaration.Module, ".")
			name = parts[len(parts)-1]
		}
		c.globals[name] = nil
	}
}

func (c *Compiler) loadModuleDeclarations(module string, state *moduleDiscoveryState) (*ast.Program, []string, bool) {
	if state.active[module] {
		state.failures++
		return nil, nil, false
	}
	state.active[module] = true
	defer delete(state.active, module)

	pathName := strings.ReplaceAll(module, ".", string(filepath.Separator))
	for _, candidate := range c.moduleFileCandidates(pathName) {
		info, err := os.Stat(candidate)
		if err != nil {
			continue
		}
		if info.IsDir() {
			base := filepath.Base(candidate)
			for _, entry := range []string{base + ".nx", "main.nx"} {
				entryPath := filepath.Join(candidate, entry)
				if entryInfo, entryErr := os.Stat(entryPath); entryErr == nil && !entryInfo.IsDir() {
					return c.parseModuleDeclarationsFile(entryPath, state)
				}
			}
			entries, readErr := os.ReadDir(candidate)
			if readErr != nil {
				return nil, nil, false
			}
			names := make([]string, 0, len(entries))
			for _, entry := range entries {
				if entry.IsDir() {
					_, _, loadable := c.loadModuleDeclarations(module+"."+entry.Name(), state)
					if loadable {
						names = append(names, entry.Name())
					}
				} else if strings.HasSuffix(entry.Name(), ".nx") {
					name := strings.TrimSuffix(entry.Name(), ".nx")
					_, _, loadable := c.loadModuleDeclarations(module+"."+name, state)
					if !loadable {
						return nil, nil, false
					}
					names = append(names, name)
				}
			}
			return nil, names, true
		}
		return c.parseModuleDeclarationsFile(candidate, state)
	}

	embedPath := strings.ReplaceAll(module, ".", "/") + ".nx"
	content, err := stdlib.FS.ReadFile(embedPath)
	if err != nil {
		return nil, nil, false
	}
	return c.parseModuleDeclarations(content, module, state)
}

func (c *Compiler) moduleFileCandidates(pathName string) []string {
	root := "."
	if c.FileName != "" && c.FileName != "REPL" {
		root = filepath.Dir(c.FileName)
	}

	var searchRoots []string
	if noxyPath := os.Getenv("NOXY_PATH"); noxyPath != "" {
		searchRoots = append(searchRoots, filepath.SplitList(noxyPath)...)
	}

	var candidates []string
	addSuffix := func(suffix string) {
		for _, searchRoot := range searchRoots {
			candidates = append(candidates,
				filepath.Join(searchRoot, suffix, suffix+".nx"),
				filepath.Join(searchRoot, suffix),
				filepath.Join(searchRoot, suffix+".nx"),
			)
		}
		candidates = append(candidates,
			filepath.Join(root, "noxy_libs", suffix, suffix+".nx"),
			filepath.Join(root, "noxy_libs", suffix),
			filepath.Join(root, "stdlib", suffix),
			filepath.Join(root, suffix),
			filepath.Join("noxy_libs", suffix, suffix+".nx"),
			filepath.Join("noxy_libs", suffix),
			filepath.Join("stdlib", suffix),
			suffix,
		)
	}
	addSuffix(pathName + ".nx")
	addSuffix(pathName)
	return candidates
}

func (c *Compiler) parseModuleDeclarationsFile(path string, state *moduleDiscoveryState) (*ast.Program, []string, bool) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, false
	}
	return c.parseModuleDeclarations(content, path, state)
}

func (c *Compiler) parseModuleDeclarations(content []byte, fileName string, state *moduleDiscoveryState) (*ast.Program, []string, bool) {
	p := parser.New(lexer.New(string(content)))
	program := p.ParseProgram()
	if len(p.Errors()) > 0 {
		return nil, nil, false
	}
	failuresBefore := state.failures
	for _, statement := range program.Statements {
		declaration, ok := statement.(*ast.UseStmt)
		if !ok || declaration.SelectAll {
			continue
		}
		if len(declaration.Selectors) > 0 {
			exports := c.discoverModuleExportsWithState(declaration.Module, state)
			for _, selector := range declaration.Selectors {
				if _, exists := exports[selector]; !exists {
					return nil, nil, false
				}
			}
			continue
		}
		if _, _, loadable := c.loadModuleDeclarations(declaration.Module, state); !loadable {
			return nil, nil, false
		}
	}
	validator := NewWithState(make(map[string]ast.NoxyType), make(map[string]*ast.StructStatement), fileName)
	validator.moduleDiscovery = state
	if _, _, err := validator.Compile(program); err != nil {
		return nil, nil, false
	}
	if state.failures != failuresBefore {
		return nil, nil, false
	}
	return program, nil, true
}

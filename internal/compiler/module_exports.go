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

func (c *Compiler) discoverModuleExports(module string) map[string]struct{} {
	return c.discoverModuleExportsFrom(module, make(map[string]bool))
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

func (c *Compiler) discoverModuleExportsFrom(module string, visiting map[string]bool) map[string]struct{} {
	exports := make(map[string]struct{})
	if visiting[module] {
		return exports
	}
	visiting[module] = true
	defer delete(visiting, module)

	program, directoryExports, ok := c.loadModuleDeclarations(module)
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
				for name := range c.discoverModuleExportsFrom(declaration.Module, visiting) {
					exports[name] = struct{}{}
				}
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

func (c *Compiler) loadModuleDeclarations(module string) (*ast.Program, []string, bool) {
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
					return parseModuleDeclarationsFile(entryPath)
				}
			}
			entries, readErr := os.ReadDir(candidate)
			if readErr != nil {
				return nil, nil, false
			}
			names := make([]string, 0, len(entries))
			for _, entry := range entries {
				if entry.IsDir() {
					names = append(names, entry.Name())
				} else if strings.HasSuffix(entry.Name(), ".nx") {
					names = append(names, strings.TrimSuffix(entry.Name(), ".nx"))
				}
			}
			return nil, names, true
		}
		return parseModuleDeclarationsFile(candidate)
	}

	embedPath := strings.ReplaceAll(module, ".", "/") + ".nx"
	content, err := stdlib.FS.ReadFile(embedPath)
	if err != nil {
		return nil, nil, false
	}
	return parseModuleDeclarations(content)
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

func parseModuleDeclarationsFile(path string) (*ast.Program, []string, bool) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, false
	}
	return parseModuleDeclarations(content)
}

func parseModuleDeclarations(content []byte) (*ast.Program, []string, bool) {
	p := parser.New(lexer.New(string(content)))
	program := p.ParseProgram()
	if len(p.Errors()) > 0 {
		return nil, nil, false
	}
	return program, nil, true
}

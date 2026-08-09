package vm

import (
	"fmt"
	"noxy-vm/internal/ast"
	"noxy-vm/internal/compiler"
	"noxy-vm/internal/lexer"
	"noxy-vm/internal/parser"
	"noxy-vm/internal/stdlib"
	"noxy-vm/internal/value"
	"os"
	"path/filepath"
	"strings"
)

func (vm *VM) loadModule(name string) (value.Value, error) {
	// Convert dot notation to path path separator
	pathName := strings.ReplaceAll(name, ".", string(filepath.Separator))

	// Search paths candidates (File .nx OR Directory)
	// We prefer file over directory if both exist? usually explicit file wins.
	// But let's check both possibilities.

	var path string
	var isDir bool
	// found := false // Unused

	// Helper to check locations
	checkLocations := func(suffix string) bool {
		candidates := []string{}

		// 0. NOXY_PATH Environment Variable
		noxyPath := os.Getenv("NOXY_PATH")
		if noxyPath != "" {
			paths := filepath.SplitList(noxyPath)
			for _, p := range paths {
				// Check for:
				// $PATH/mod/mod.nx
				// $PATH/mod (dir)
				// $PATH/mod.nx (file)
				candidates = append(candidates, filepath.Join(p, suffix, suffix+".nx"))
				candidates = append(candidates, filepath.Join(p, suffix))
				candidates = append(candidates, filepath.Join(p, suffix+".nx"))
			}
		}

		// 1. RootPath (Config)
		candidates = append(candidates, filepath.Join(vm.Config.RootPath, "noxy_libs", suffix, suffix+".nx"))
		candidates = append(candidates, filepath.Join(vm.Config.RootPath, "noxy_libs", suffix))
		candidates = append(candidates, filepath.Join(vm.Config.RootPath, "stdlib", suffix))
		candidates = append(candidates, filepath.Join(vm.Config.RootPath, suffix))

		// 2. Fallbacks (Working Directory)
		candidates = append(candidates, filepath.Join("noxy_libs", suffix, suffix+".nx"))
		candidates = append(candidates, filepath.Join("noxy_libs", suffix))
		candidates = append(candidates, filepath.Join("stdlib", suffix))
		candidates = append(candidates, suffix)

		for _, p := range candidates {
			// fmt.Printf("Checking path: %s\n", p)
			info, err := os.Stat(p)
			if err == nil {
				// fmt.Printf("Found: %s (IsDir: %v)\n", p, info.IsDir())
				path = p
				isDir = info.IsDir()
				// found = true
				return true
			}
		}
		return false
	}

	// 1. Check for .nx file
	if checkLocations(pathName+".nx") && !isDir {
		// Found file
	} else if checkLocations(pathName) {
		// Found directory OR file (noxy_libs/mod/mod.nx)
		// If isDir is false, it means we found .../mod.nx via the mod name check,
		// which acts as the module entry point.
	} else {
		// Not found on disk, check embedded stdlib
		// Stdlib is flat in embed.go usually? Or structure preserved?
		// We moved stdlib/* to internal/stdlib.
		// So internal/stdlib has *.nx files directly.
		// Name would be "time" "io" etc.
		// pathName for "time" is "time".
		// embedded file is "time.nx".

		// Check if it exists in embedded FS
		embedPath := pathName + ".nx"
		content, err := stdlib.FS.ReadFile(embedPath)
		if err == nil {
			// Found in embedded stdlib!
			l := lexer.New(string(content))
			p := parser.New(l)
			prog := p.ParseProgram()
			if len(p.Errors()) > 0 {
				return value.NewNull(), fmt.Errorf("parse error in embedded module %s: %v", name, p.Errors())
			}
			c := compiler.NewWithStateAndRoot(make(map[string]ast.NoxyType), make(map[string]*ast.StructStatement), name, vm.Config.RootPath)
			chunk, _, err := c.Compile(prog)
			if err != nil {
				return value.NewNull(), err
			}
			moduleGlobals := make(map[string]value.Value)
			modFn := &value.ObjFunction{
				Name:    name,
				Arity:   0,
				Chunk:   chunk,
				Globals: moduleGlobals,
			}
			modClosure := &value.ObjClosure{Function: modFn, Upvalues: []*value.ObjUpvalue{}, Globals: moduleGlobals}
			modVal := value.Value{Type: value.VAL_FUNCTION, Obj: modClosure}
			vm.push(modVal)
			if ok, err := vm.callValue(modVal, 0, nil, 0); !ok {
				return value.NewNull(), err
			}
			err = vm.run(vm.frameCount) // Run until return
			if err != nil {
				return value.NewNull(), err
			}
			vm.pop() // Pop result
			return value.NewMapWithData(moduleGlobals), nil
		}

		return value.NewNull(), fmt.Errorf("module not found: %s", name)
	}

	// Case 1: Directory Import (Implicit Module)
	if isDir {
		// Check for entry point file (e.g. <dirname>.nx, main.nx)
		// If found, load that file directly instead of treating dir as a map of files.
		// Removed mod.nx and lib.nx to avoid shadowing directory contents when those files exist as children.
		baseName := filepath.Base(path)
		candidates := []string{baseName + ".nx", "main.nx"}

		for _, cand := range candidates {
			entryPath := filepath.Join(path, cand)
			if _, err := os.Stat(entryPath); err == nil {
				// Found entry point, verify it is not a directory
				// (It shouldn't be given the extension, but safety first)
				path = entryPath
				isDir = false
				goto FileImport
			}
		}

		files, err := os.ReadDir(path)
		if err != nil {
			return value.NewNull(), err
		}

		moduleGlobals := make(map[string]value.Value)

		for _, f := range files {
			if f.IsDir() {
				// Recursively load subdirectory as a nested module
				subDirName := name + "." + f.Name()
				subMod, err := vm.loadModule(subDirName)
				if err != nil {
					// Ignore subdirectories that fail to load (e.g., empty or invalid)
					continue
				}
				moduleGlobals[f.Name()] = subMod
			} else if strings.HasSuffix(f.Name(), ".nx") {
				baseName := strings.TrimSuffix(f.Name(), ".nx")
				subModuleName := name + "." + baseName

				// Recursive load
				subMod, err := vm.loadModule(subModuleName)
				if err != nil {
					return value.NewNull(), fmt.Errorf("failed to load submodule %s: %v", subModuleName, err)
				}
				moduleGlobals[baseName] = subMod
			}
		}
		return value.NewMapWithData(moduleGlobals), nil
	}

FileImport:
	// Case 2: File Import
	content, err := os.ReadFile(path)
	if err != nil {
		return value.NewNull(), err
	}

	l := lexer.New(string(content))
	p := parser.New(l)
	prog := p.ParseProgram()
	if len(p.Errors()) > 0 {
		return value.NewNull(), fmt.Errorf("parse error in module %s: %v", name, p.Errors())
	}

	c := compiler.NewWithStateAndRoot(make(map[string]ast.NoxyType), make(map[string]*ast.StructStatement), path, vm.Config.RootPath)
	chunk, _, err := c.Compile(prog)
	if err != nil {
		return value.NewNull(), err
	}

	// Create isolated Module Globals
	moduleGlobals := make(map[string]value.Value)

	// Prepare Module Function
	modFn := &value.ObjFunction{
		Name:    name,
		Arity:   0,
		Chunk:   chunk,
		Globals: moduleGlobals,
	}
	modClosure := &value.ObjClosure{Function: modFn, Upvalues: []*value.ObjUpvalue{}, Globals: moduleGlobals}
	modVal := value.Value{Type: value.VAL_FUNCTION, Obj: modClosure}

	// Execute Module Synchronously
	vm.push(modVal)
	if ok, err := vm.callValue(modVal, 0, nil, 0); !ok {
		return value.NewNull(), err
	}

	// Run until this frame returns
	startFrameCount := vm.frameCount
	err = vm.run(startFrameCount)
	if err != nil {
		return value.NewNull(), err
	}

	// Module execution finished.
	// The result of module (usually null) is on stack. Pop it.
	vm.pop()

	// Return the Module Map
	return value.NewMapWithData(moduleGlobals), nil
}

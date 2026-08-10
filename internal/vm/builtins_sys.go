package vm

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"noxy-vm/internal/plugin"
	"noxy-vm/internal/value"
)

func (vm *VM) defineSystemBuiltins() {
	// Sys Module
	vm.DefineNative("sys_os", func(args []value.Value) value.Value {
		return value.NewString(runtime.GOOS)
	})

	vm.DefineNative("sys_exec", func(args []value.Value) value.Value {
		if len(args) < 2 {
			return value.NewNull()
		}
		cmdStr := args[0].String()
		structDef, ok := args[1].Obj.(*value.ObjStruct)
		if !ok {
			return value.NewNull()
		}

		var cmd *exec.Cmd
		if os.PathSeparator == '\\' {
			cmd = exec.Command("cmd", "/C", cmdStr)
		} else {
			cmd = exec.Command("sh", "-c", cmdStr)
		}

		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr

		err := cmd.Run()
		exitCode := 0
		okVal := true

		var outputStr string = "" // No captured output for sys_exec

		if err != nil {
			okVal = false
			if exitErr, ok := err.(*exec.ExitError); ok {
				exitCode = exitErr.ExitCode()
			} else {
				exitCode = 1
			}
		}

		inst := value.NewInstance(structDef).Obj.(*value.ObjInstance)
		inst.Fields["exit_code"] = value.NewInt(int64(exitCode))
		inst.Fields["output"] = value.NewString(outputStr)
		inst.Fields["ok"] = value.NewBool(okVal)

		return value.Value{Type: value.VAL_OBJ, Obj: inst}
	})

	vm.DefineNative("sys_exec_output", func(args []value.Value) value.Value {
		if len(args) < 2 {
			return value.NewNull()
		}
		cmdStr := args[0].String()
		structDef, ok := args[1].Obj.(*value.ObjStruct)
		if !ok {
			return value.NewNull()
		}

		var cmd *exec.Cmd
		if os.PathSeparator == '\\' {
			cmd = exec.Command("cmd", "/C", cmdStr)
		} else {
			cmd = exec.Command("sh", "-c", cmdStr)
		}

		outBytes, err := cmd.CombinedOutput()
		outputStr := string(outBytes)

		// OK indicates execution completion, regardless of exit code.
		okVal := true
		exitCode := 0

		if err != nil {
			if exitErr, ok := err.(*exec.ExitError); ok {
				exitCode = exitErr.ExitCode()
			} else {
				exitCode = 1
			}
			okVal = false
		} else {
			okVal = true
		}

		inst := value.NewInstance(structDef).Obj.(*value.ObjInstance)
		inst.Fields["exit_code"] = value.NewInt(int64(exitCode))
		inst.Fields["output"] = value.NewString(strings.TrimSpace(outputStr))
		inst.Fields["ok"] = value.NewBool(okVal)

		return value.Value{Type: value.VAL_OBJ, Obj: inst}
	})

	vm.DefineNative("sys_load_plugin", func(args []value.Value) value.Value {
		if len(args) < 2 {
			return value.NewBool(false)
		}
		name := args[0].String()
		cmdName := args[1].String()

		// Intelligent Path Search
		var cmdPath string
		found := false

		// 1. Check absolute path or PATH override
		if filepath.IsAbs(cmdName) {
			if _, err := os.Stat(cmdName); err == nil {
				cmdPath = cmdName
				found = true
			}
		} else {
			// 2. Check path provided directly (PATH lookup)
			if path, err := exec.LookPath(cmdName); err == nil {
				cmdPath = path
				found = true
			}
		}

		// 3. Check Current Working Directory (explicitly)
		if !found {
			cwd, _ := os.Getwd()
			localPath := filepath.Join(cwd, cmdName)
			// Add .exe on Windows if not present
			if runtime.GOOS == "windows" && !strings.HasSuffix(localPath, ".exe") {
				localPath += ".exe"
			}
			if _, err := os.Stat(localPath); err == nil {
				cmdPath = localPath
				found = true
			}
		}

		// 4. Check noxy_libs recursively (Depth restricted)
		if !found {
			cwd, _ := os.Getwd()
			libsDir := filepath.Join(cwd, "noxy_libs")
			filepath.Walk(libsDir, func(path string, info os.FileInfo, err error) error {
				if found {
					return filepath.SkipDir // Stop if found
				}
				if err != nil {
					return nil // Ignore errors
				}
				if info.IsDir() {
					if info.Name() == ".git" {
						return filepath.SkipDir
					}
					return nil
				}

				fname := info.Name()
				isMatch := fname == cmdName
				if runtime.GOOS == "windows" {
					isMatch = fname == cmdName || fname == cmdName+".exe"
				}

				if isMatch {
					cmdPath = path
					found = true
					return filepath.SkipDir // Abort walk
				}
				return nil
			})
		}

		if !found {
			fmt.Printf("Plugin Load Error: command not found: %s\n", cmdName)
			return value.NewBool(false)
		}

		client, err := plugin.LoadPlugin(name, cmdPath)
		if err != nil {
			fmt.Printf("Plugin Load Error: failed to load plugin: %v\n", err)
			return value.NewBool(false)
		}

		// Define Native dynamically
		nativeName := name + "_request" // e.g. dynamodb_request
		vm.DefineNative(nativeName, func(args []value.Value) value.Value {
			if len(args) < 1 {
				return value.NewNull()
			}
			method := args[0].String()
			params := args[1:]
			return client.Call(method, params)
		})

		return value.NewBool(true)
	})

	vm.DefineNative("sys_getenv", func(args []value.Value) value.Value {
		if len(args) < 2 {
			return value.NewNull()
		}
		key := args[0].String()
		structDef, ok := args[1].Obj.(*value.ObjStruct)
		if !ok {
			return value.NewNull()
		}

		val, found := os.LookupEnv(key)

		inst := value.NewInstance(structDef).Obj.(*value.ObjInstance)
		inst.Fields["value"] = value.NewString(val)
		inst.Fields["ok"] = value.NewBool(found)

		return value.Value{Type: value.VAL_OBJ, Obj: inst}
	})

	vm.DefineNative("sys_setenv", func(args []value.Value) value.Value {
		if len(args) < 2 {
			return value.NewBool(false)
		}
		key := args[0].String()
		val := args[1].String()
		err := os.Setenv(key, val)
		return value.NewBool(err == nil)
	})

	vm.DefineNative("sys_getcwd", func(args []value.Value) value.Value {
		dir, err := os.Getwd()
		if err != nil {
			return value.NewString("")
		}
		return value.NewString(dir)
	})

	vm.DefineNative("sys_argv", func(args []value.Value) value.Value {
		// Convert os.Args to string[]
		vals := make([]value.Value, len(os.Args))
		for i, a := range os.Args {
			vals[i] = value.NewString(a)
		}
		return value.NewArray(vals)
	})

	vm.DefineNative("sys_sleep", func(args []value.Value) value.Value {
		if len(args) < 1 {
			return value.NewNull()
		}
		ms := args[0].AsInt
		time.Sleep(time.Duration(ms) * time.Millisecond)
		return value.NewNull()
	})

	vm.DefineNative("sys_exit", func(args []value.Value) value.Value {
		code := 0
		if len(args) > 0 {
			code = int(args[0].AsInt)
		}
		_ = vm.shared.Terminal.close()
		vm.shared.exitProcess(code)
		return value.NewNull()
	})
}

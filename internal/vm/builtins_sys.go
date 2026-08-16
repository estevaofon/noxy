package vm

import (
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"noxy-vm/internal/plugin"
	"noxy-vm/internal/value"
)

func (vm *VM) defineSystemBuiltins() {
	vm.DefineNative("sys_signal_notify", func(args []value.Value) value.Value {
		if len(args) < 1 || args[0].Type != value.VAL_CHANNEL {
			return value.NewBool(false)
		}
		chObj := args[0].Obj.(*value.ObjChannel)

		vm.shared.SignalSubMu.Lock()
		defer vm.shared.SignalSubMu.Unlock()

		if vm.shared.ActiveSignalChan != nil {
			signal.Stop(vm.shared.ActiveSignalChan)
			close(vm.shared.ActiveSignalChan)
			vm.shared.ActiveSignalChan = nil
		}

		goChan := make(chan os.Signal, 1)
		signal.Notify(goChan, os.Interrupt, syscall.SIGTERM)
		vm.shared.ActiveSignalChan = goChan

		go func(goCh chan os.Signal, nxChan *value.ObjChannel) {
			for sig := range goCh {
				func() {
					defer func() {
						recover() // handle send on closed channel gracefully
					}()

					sigVal := int64(0)
					if sig == os.Interrupt {
						sigVal = 2 // SIGINT
					} else if sig == syscall.SIGTERM {
						sigVal = 15 // SIGTERM
					}

					nxChan.Chan <- value.NewInt(sigVal)
				}()
			}
		}(goChan, chObj)

		return value.NewBool(true)
	})

	vm.DefineNative("sys_signal_stop", func(args []value.Value) value.Value {
		vm.shared.SignalSubMu.Lock()
		defer vm.shared.SignalSubMu.Unlock()

		if vm.shared.ActiveSignalChan != nil {
			signal.Stop(vm.shared.ActiveSignalChan)
			close(vm.shared.ActiveSignalChan)
			vm.shared.ActiveSignalChan = nil
			return value.NewBool(true)
		}
		return value.NewBool(false)
	})

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
		inst.Fields["error"] = value.NewString("")

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

		// ok is true only when the process both started and exited with code
		// 0. A non-zero exit (an *exec.ExitError) and a failure to start both
		// report ok=false; exit_code distinguishes them.
		okVal := true
		exitCode := 0
		errMsg := ""

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

		// The process's output is an external byte source labelled as
		// text: it must be valid UTF-8 before it is handed back as a Noxy
		// string, regardless of exit code — a crashing command's partial
		// output is just as untrusted as a clean one's. This does not
		// collide with the "process completed" meaning of ok above — a
		// process that ran and produced binary output is still reported
		// as a distinct, diagnosable case via ok=false plus a UTF-8 error
		// message.
		if verifyErr := requireValidUTF8("sys.exec_output", outputStr); verifyErr != nil {
			okVal = false
			errMsg = verifyErr.Error()
		}

		outputField := ""
		if errMsg == "" {
			outputField = strings.TrimSpace(outputStr)
		}

		inst := value.NewInstance(structDef).Obj.(*value.ObjInstance)
		inst.Fields["exit_code"] = value.NewInt(int64(exitCode))
		inst.Fields["output"] = value.NewString(outputField)
		inst.Fields["ok"] = value.NewBool(okVal)
		inst.Fields["error"] = value.NewString(errMsg)

		return value.Value{Type: value.VAL_OBJ, Obj: inst}
	})

	vm.DefineContextualNative("sys_load_plugin", func(context value.NativeContext, args []value.Value) (value.Value, error) {
		machine, contextErr := nativeVM(context)
		if contextErr != nil {
			return value.NewNull(), contextErr
		}
		if len(args) < 2 {
			return value.NewBool(false), nil
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
			return value.NewBool(false), nil
		}

		client, err := plugin.LoadPlugin(name, cmdPath)
		if err != nil {
			fmt.Printf("Plugin Load Error: failed to load plugin: %v\n", err)
			return value.NewBool(false), nil
		}

		// Define Native dynamically
		nativeName := name + "_request" // e.g. dynamodb_request
		machine.DefineNative(nativeName, func(args []value.Value) value.Value {
			if len(args) < 1 {
				return value.NewNull()
			}
			method := args[0].String()
			params := args[1:]
			return client.Call(method, params)
		})

		return value.NewBool(true), nil
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

		// ok's pre-existing meaning is "the variable is set" (found).
		// Invalid UTF-8 in a set variable is a distinct, diagnosable case:
		// it must not be reported the same way as "unset", so it also
		// clears ok but carries its own error message instead of the
		// empty one every other path here keeps.
		okVal := found
		errMsg := ""
		valueField := val
		if found {
			if verifyErr := requireValidUTF8("sys.getenv", val); verifyErr != nil {
				okVal = false
				errMsg = verifyErr.Error()
				valueField = ""
			}
		}

		inst := value.NewInstance(structDef).Obj.(*value.ObjInstance)
		inst.Fields["value"] = value.NewString(valueField)
		inst.Fields["ok"] = value.NewBool(okVal)
		inst.Fields["error"] = value.NewString(errMsg)

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
		os.Exit(code)
		return value.NewNull()
	})
}

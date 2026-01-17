package plugin

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"noxy-vm/internal/value"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
)

// Request sent to plugin
type PluginRequest struct {
	Method string        `json:"method"`
	Params []interface{} `json:"params"`
}

// Response received from plugin
type PluginResponse struct {
	Result interface{} `json:"result,omitempty"`
	Error  string      `json:"error,omitempty"`
}

type PluginClient struct {
	Name    string
	Cmd     *exec.Cmd
	Stdin   io.WriteCloser
	Stdout  *bufio.Scanner
	Running bool
	Lock    sync.Mutex
}

var (
	LoadedPlugins = make(map[string]*PluginClient)
	PluginsLock   sync.Mutex
)

func LoadPlugin(name string, executableName string) (*PluginClient, error) {
	PluginsLock.Lock()
	defer PluginsLock.Unlock()

	if client, ok := LoadedPlugins[name]; ok {
		return client, nil
	}

	// Resolve executable path
	var execPath string
	// 1. Check PATH
	path, err := exec.LookPath(executableName)
	if err == nil {
		execPath = path
	} else {
		// 2. Check noxy_libs/<plugin>/<plugin> (local or relative to root)
		// We need root path, but plugin lookup is generic?
		// Actually, sys_load_plugin doesn't pass root.
		// For now we check "./noxy_libs/<plugin>/<plugin>"

		// If name matches executableName, assumes plugin follows folder convention
		// Try: ./noxy_libs/<name>/<executableName>
		noxyLibPath := filepath.Join("noxy_libs", name, executableName)
		if _, err := os.Stat(noxyLibPath); err == nil {
			execPath, _ = filepath.Abs(noxyLibPath)
		} else {
			// Try with .exe extension for Windows if not found
			if _, err := os.Stat(noxyLibPath + ".exe"); err == nil {
				execPath, _ = filepath.Abs(noxyLibPath + ".exe")
			} else {
				// 3. Try relative to current dir
				if _, err := os.Stat(executableName); err == nil {
					execPath, _ = filepath.Abs(executableName)
				}
			}
		}
	}

	cmd := exec.Command(execPath)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to create stdin pipe: %v", err)
	}

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to create stdout pipe: %v", err)
	}

	cmd.Stderr = os.Stderr // Pass stderr through

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to start plugin process: %v", err)
	}

	client := &PluginClient{
		Name:    name,
		Cmd:     cmd,
		Stdin:   stdin,
		Stdout:  bufio.NewScanner(stdoutPipe),
		Running: true,
	}

	LoadedPlugins[name] = client
	return client, nil
}

func (c *PluginClient) Call(method string, args []value.Value) value.Value {
	c.Lock.Lock()
	defer c.Lock.Unlock()

	if !c.Running {
		return value.NewNull()
	}

	// Marshal args to JSON
	jsonArgs := make([]interface{}, len(args))
	for i, arg := range args {
		jsonArgs[i] = ValueToInterface(arg)
	}

	req := PluginRequest{
		Method: method,
		Params: jsonArgs,
	}

	reqBytes, err := json.Marshal(req)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Plugin Error: failed to marshal request: %v\n", err)
		return value.NewNull()
	}

	// Send Request
	if _, err := c.Stdin.Write(append(reqBytes, '\n')); err != nil {
		fmt.Fprintf(os.Stderr, "Plugin Error: failed to write to plugin: %v\n", err)
		c.Running = false
		return value.NewNull()
	}

	// Read Response
	if c.Stdout.Scan() {
		respBytes := c.Stdout.Bytes()
		var resp PluginResponse
		if err := json.Unmarshal(respBytes, &resp); err != nil {
			fmt.Fprintf(os.Stderr, "Plugin Error: failed to unmarshal response: %v\n", err)
			return value.NewNull()
		}

		if resp.Error != "" {
			// Maybe return error object? For now basic null or print?
			// Let's print for debug, return null
			fmt.Fprintf(os.Stderr, "Plugin Remote Error: %s\n", resp.Error)
			return value.NewNull()
		}

		return InterfaceToValue(resp.Result)
	} else {
		if err := c.Stdout.Err(); err != nil {
			fmt.Fprintf(os.Stderr, "Plugin Error: read failed: %v\n", err)
		} else {
			fmt.Fprintf(os.Stderr, "Plugin Error: unexpected EOF\n")
		}
		c.Running = false
		return value.NewNull()
	}
}

// Helpers to convert between Value and Go interface{} for JSON

func ValueToInterface(v value.Value) interface{} {
	switch v.Type {
	case value.VAL_NULL:
		return nil
	case value.VAL_BOOL:
		return v.AsBool
	case value.VAL_INT:
		return v.AsInt
	case value.VAL_FLOAT:
		return v.AsFloat
	case value.VAL_OBJ:
		switch o := v.Obj.(type) {
		case string:
			return o
		case *value.ObjArray:
			arr := make([]interface{}, len(o.Elements))
			for i, e := range o.Elements {
				arr[i] = ValueToInterface(e)
			}
			return arr
		case *value.ObjMap:
			m := make(map[string]interface{})
			for k, val := range o.Data {
				if keyStr, ok := k.(string); ok {
					m[keyStr] = ValueToInterface(val)
				} else {
					m[fmt.Sprintf("%v", k)] = ValueToInterface(val)
				}
			}
			return m
		default:
			return fmt.Sprintf("%v", v.Obj)
		}
	default:
		return nil
	}
}

func InterfaceToValue(i interface{}) value.Value {
	if i == nil {
		return value.NewNull()
	}
	switch v := i.(type) {
	case bool:
		return value.NewBool(v)
	case float64:
		// JSON numbers are floats. Check if integer?
		if float64(int64(v)) == v {
			return value.NewInt(int64(v))
		}
		return value.NewFloat(v)
	case string:
		return value.NewString(v)
	case []interface{}:
		arr := make([]value.Value, len(v))
		for idx, elm := range v {
			arr[idx] = InterfaceToValue(elm)
		}
		return value.NewArray(arr)
	case map[string]interface{}:
		m := make(map[string]value.Value)
		for k, val := range v {
			m[k] = InterfaceToValue(val)
		}
		return value.NewMapWithData(m)
	default:
		return value.NewString(fmt.Sprintf("%v", v))
	}
}

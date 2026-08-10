package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/signal"
	"sync"
	"syscall"
)

type pluginRequest struct {
	Method string        `json:"method"`
	Params []interface{} `json:"params"`
}

type pluginResponse struct {
	Result interface{} `json:"result,omitempty"`
	Error  string      `json:"error,omitempty"`
}

type pluginServer struct {
	runtime *terminalRuntime
	encoder *json.Encoder
	writeMu sync.Mutex
	workers sync.WaitGroup
}

func newPluginServer(runtime *terminalRuntime, output io.Writer) *pluginServer {
	return &pluginServer{
		runtime: runtime,
		encoder: json.NewEncoder(output),
	}
}

func (server *pluginServer) handle(request pluginRequest) pluginResponse {
	switch request.Method {
	case "is_terminal":
		return pluginResponse{Result: server.runtime.isTerminal()}
	case "open_raw":
		return pluginResponse{Result: server.runtime.openRaw()}
	case "read_key":
		key, ok := server.runtime.readKey()
		if !ok {
			return pluginResponse{Result: nil}
		}
		return pluginResponse{Result: key}
	case "last_error":
		return pluginResponse{Result: server.runtime.lastError()}
	case "close":
		return pluginResponse{Result: server.runtime.close()}
	default:
		return pluginResponse{Error: "unknown method: " + request.Method}
	}
}

func (server *pluginServer) write(response pluginResponse) {
	server.writeMu.Lock()
	defer server.writeMu.Unlock()
	_ = server.encoder.Encode(response)
}

func (server *pluginServer) serve(input io.Reader) error {
	scanner := bufio.NewScanner(input)
	for scanner.Scan() {
		line := append([]byte(nil), scanner.Bytes()...)
		var request pluginRequest
		if err := json.Unmarshal(line, &request); err != nil {
			server.write(pluginResponse{Error: err.Error()})
			continue
		}

		if request.Method == "read_key" {
			server.workers.Add(1)
			go func(request pluginRequest) {
				defer server.workers.Done()
				server.write(server.handle(request))
			}(request)
			continue
		}

		server.write(server.handle(request))
	}

	server.runtime.shutdown()
	server.workers.Wait()
	return scanner.Err()
}

func main() {
	runtime := newTerminalRuntime(xTermDriver{})
	defer runtime.shutdown()

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(signals)
	go func() {
		<-signals
		runtime.shutdown()
		os.Exit(0)
	}()

	server := newPluginServer(runtime, os.Stdout)
	if err := server.serve(os.Stdin); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

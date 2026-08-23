package ext

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
)

const hostModuleName = "noxy:host/v1"

const defaultCallTimeout = 30 * time.Second

type LoaderConfig struct {
	PermittedImports []string
	// CallTimeout limita cada nx_call (0 → defaultCallTimeout). Um guest em
	// loop infinito vira trap por cancelamento de contexto, nao um processo
	// travado sem saida.
	CallTimeout time.Duration
	// MaxInstances limita o pool do modo stateless (0 → runtime.NumCPU()).
	MaxInstances int
}

type callState struct {
	failMsg string
	failed  bool
}

type callStateKey struct{}

type instance struct {
	mod   api.Module
	alloc api.Function
	free  api.Function
	call  api.Function
}

type Module struct {
	Manifest *Manifest

	runtime     wazero.Runtime
	compiled    wazero.CompiledModule
	limits      Limits
	callTimeout time.Duration
	mu          sync.Mutex
	single      *instance
	failed      bool
	pool        chan *instance
	// slots e um semaforo de capacidade (buffered, pre-preenchido no load):
	// release devolve a vaga SEMPRE, inclusive para instancia envenenada —
	// sem isso, traps com o pool esgotado deixariam goroutinas bloqueadas
	// para sempre em <-pool (lost wakeup).
	slots  chan struct{}
	nextID atomic.Uint64
}

func LoadModule(ctx context.Context, wasmBytes []byte, manifest *Manifest, cfg LoaderConfig) (*Module, error) {
	pages := uint32(manifest.MemoryMaxMB) * 16 // paginas wasm de 64 KiB
	runtimeConfig := wazero.NewRuntimeConfig().
		WithCloseOnContextDone(true).
		WithMemoryLimitPages(pages)
	// Cache de compilacao persistente: sem ele, todo `noxy script.nx`
	// recompila o .wasm do zero a cada execucao. Falhar ao criar o cache
	// nao e fatal — o load segue sem cache.
	if userCache, cacheErr := os.UserCacheDir(); cacheErr == nil {
		dir := filepath.Join(userCache, "noxy", "wazero")
		if cache, dirErr := wazero.NewCompilationCacheWithDir(dir); dirErr == nil {
			runtimeConfig = runtimeConfig.WithCompilationCache(cache)
		}
	}
	r := wazero.NewRuntimeWithConfig(ctx, runtimeConfig)

	hostBuilder := r.NewHostModuleBuilder(hostModuleName)
	hostBuilder.NewFunctionBuilder().
		WithFunc(func(ctx context.Context, mod api.Module, ptr, size uint32) {
			state, _ := ctx.Value(callStateKey{}).(*callState)
			if state == nil {
				return
			}
			state.failed = true
			if data, ok := mod.Memory().Read(ptr, size); ok {
				state.failMsg = string(data)
			}
		}).Export("nx_fail")
	hostBuilder.NewFunctionBuilder().
		WithFunc(func(ctx context.Context, mod api.Module, level, ptr, size uint32) {
			if data, ok := mod.Memory().Read(ptr, size); ok {
				fmt.Fprintf(os.Stderr, "[ext %s] %s\n", manifest.Name, data)
			}
		}).Export("nx_log")
	if _, err := hostBuilder.Instantiate(ctx); err != nil {
		r.Close(ctx)
		return nil, fmt.Errorf("extension %q: host module: %w", manifest.Name, err)
	}

	permitted := map[string]bool{hostModuleName: true}
	for _, name := range cfg.PermittedImports {
		permitted[name] = true
	}
	if permitted["wasi_snapshot_preview1"] {
		wasi_snapshot_preview1.MustInstantiate(ctx, r)
	}

	compiled, err := r.CompileModule(ctx, wasmBytes)
	if err != nil {
		r.Close(ctx)
		return nil, fmt.Errorf("extension %q: compile: %w", manifest.Name, err)
	}
	for _, def := range compiled.ImportedFunctions() {
		moduleName, name, isImport := def.Import()
		if isImport && !permitted[moduleName] {
			r.Close(ctx)
			return nil, fmt.Errorf(
				"extension %q imports %q from ungranted module %q (spec §9)",
				manifest.Name, name, moduleName)
		}
	}
	exports := compiled.ExportedFunctions()
	for _, required := range []string{"nx_abi_version", "nx_alloc", "nx_free", "nx_call"} {
		if _, ok := exports[required]; !ok {
			r.Close(ctx)
			return nil, fmt.Errorf("extension %q does not export %q (ABI v1, spec §2)", manifest.Name, required)
		}
	}

	callTimeout := cfg.CallTimeout
	if callTimeout == 0 {
		callTimeout = defaultCallTimeout
	}
	maxInstances := cfg.MaxInstances
	if maxInstances == 0 {
		maxInstances = runtime.NumCPU()
	}
	m := &Module{
		Manifest:    manifest,
		runtime:     r,
		compiled:    compiled,
		limits:      DefaultLimits(),
		callTimeout: callTimeout,
	}
	if manifest.Concurrency == "stateless" {
		m.pool = make(chan *instance, maxInstances)
		m.slots = make(chan struct{}, maxInstances)
		for i := 0; i < maxInstances; i++ {
			m.slots <- struct{}{}
		}
	}
	// Instancia ansiosa: erros de _initialize/handshake aparecem no load,
	// nao na primeira chamada.
	first, err := m.newInstance(ctx)
	if err != nil {
		r.Close(ctx)
		return nil, err
	}
	if m.pool != nil {
		m.pool <- first
	} else {
		m.single = first
	}
	return m, nil
}

func (m *Module) newInstance(ctx context.Context) (*instance, error) {
	name := fmt.Sprintf("%s#%d", m.Manifest.Name, m.nextID.Add(1))
	mod, err := m.runtime.InstantiateModule(ctx, m.compiled, wazero.NewModuleConfig().WithName(name))
	if err != nil {
		return nil, fmt.Errorf("extension %q: instantiate: %w", m.Manifest.Name, err)
	}
	if initFn := mod.ExportedFunction("_initialize"); initFn != nil {
		if _, err := initFn.Call(ctx); err != nil {
			mod.Close(ctx)
			return nil, fmt.Errorf("extension %q: _initialize: %w", m.Manifest.Name, err)
		}
	}
	versionFn := mod.ExportedFunction("nx_abi_version")
	results, err := versionFn.Call(ctx)
	if err != nil {
		mod.Close(ctx)
		return nil, fmt.Errorf("extension %q: nx_abi_version: %w", m.Manifest.Name, err)
	}
	if got := uint32(results[0]); got != supportedABI {
		mod.Close(ctx)
		return nil, fmt.Errorf(
			"extension %q speaks ABI %d, host supports %d (min_noxy %q)",
			m.Manifest.Name, got, supportedABI, m.Manifest.MinNoxy)
	}
	return &instance{
		mod:   mod,
		alloc: mod.ExportedFunction("nx_alloc"),
		free:  mod.ExportedFunction("nx_free"),
		call:  mod.ExportedFunction("nx_call"),
	}, nil
}

func (m *Module) Close(ctx context.Context) error {
	return m.runtime.Close(ctx)
}

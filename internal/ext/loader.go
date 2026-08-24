package ext

import (
	"context"
	"fmt"
	"io"
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
	// MaxInstances limita o pool do modo stateless (0 → runtime.GOMAXPROCS(0),
	// spec §5).
	MaxInstances int
	// Log e o destino de nx_log (nil → os.Stderr). A spec fala em "diagOut",
	// mas nao ha campo diagOut no VM ainda — stderr explicito ate existir
	// (achado de revisao).
	Log io.Writer
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
	// Cinto e suspensorio (achado de revisao): ParseManifest ja rejeita
	// memory_max_mb negativo, mas um Manifest pode chegar aqui construido a
	// mao (testes, chamadores futuros). uint32(negativo)*16 estoura para um
	// numero de paginas gigantesco e wazero.WithMemoryLimitPages entra em
	// panico (nao erro) acima de 65536 paginas — sem essa guarda isso
	// derrubaria a VM inteira sem recover.
	if manifest.MemoryMaxMB <= 0 {
		return nil, fmt.Errorf("extension %q: memory_max_mb %d is not a positive page count", manifest.Name, manifest.MemoryMaxMB)
	}
	pages := uint32(manifest.MemoryMaxMB) * 16 // paginas wasm de 64 KiB
	if pages > 65536 {
		return nil, fmt.Errorf("extension %q: memory_max_mb %d exceeds the wazero page limit", manifest.Name, manifest.MemoryMaxMB)
	}
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

	logOut := cfg.Log
	if logOut == nil {
		logOut = os.Stderr
	}

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
				fmt.Fprintf(logOut, "[ext %s] %s\n", manifest.Name, data)
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
	if err := validateABISignatures(exports); err != nil {
		r.Close(ctx)
		return nil, fmt.Errorf("extension %q: %w", manifest.Name, err)
	}

	callTimeout := cfg.CallTimeout
	if callTimeout == 0 {
		callTimeout = defaultCallTimeout
	}
	maxInstances := cfg.MaxInstances
	if maxInstances == 0 {
		maxInstances = runtime.GOMAXPROCS(0)
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

// abiSignature declara os tipos esperados (spec §2) de um export ABI v1.
type abiSignature struct {
	params  []api.ValueType
	results []api.ValueType
}

var requiredABISignatures = map[string]abiSignature{
	"nx_abi_version": {params: nil, results: []api.ValueType{api.ValueTypeI32}},
	"nx_alloc":       {params: []api.ValueType{api.ValueTypeI32}, results: []api.ValueType{api.ValueTypeI32}},
	"nx_free":        {params: []api.ValueType{api.ValueTypeI32, api.ValueTypeI32}, results: nil},
	"nx_call":        {params: []api.ValueType{api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32}, results: []api.ValueType{api.ValueTypeI64}},
}

// validateABISignatures confere que os quatro exports obrigatorios do ABI v1
// tem a assinatura exata declarada na spec §2. Sem essa checagem, um export
// com aridade errada (ex.: nx_call com um parametro a menos) so falha na
// primeira chamada real, e falha como panico do host ao indexar results[0]
// em vez de um erro de carregamento (achado de revisao).
func validateABISignatures(exports map[string]api.FunctionDefinition) error {
	for _, name := range []string{"nx_abi_version", "nx_alloc", "nx_free", "nx_call"} {
		want := requiredABISignatures[name]
		def, ok := exports[name]
		if !ok {
			continue // presenca ja foi checada pelo chamador
		}
		if !sameValueTypes(def.ParamTypes(), want.params) || !sameValueTypes(def.ResultTypes(), want.results) {
			return fmt.Errorf("export %q has signature %s -> %s, want %s -> %s (ABI v1, spec §2)",
				name,
				formatValueTypes(def.ParamTypes()), formatValueTypes(def.ResultTypes()),
				formatValueTypes(want.params), formatValueTypes(want.results))
		}
	}
	return nil
}

func sameValueTypes(got, want []api.ValueType) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func formatValueTypes(types []api.ValueType) string {
	if len(types) == 0 {
		return "()"
	}
	s := "("
	for i, t := range types {
		if i > 0 {
			s += ", "
		}
		s += api.ValueTypeName(t)
	}
	return s + ")"
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

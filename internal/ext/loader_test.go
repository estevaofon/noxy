package ext

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"noxy-vm/internal/ext/exttest"

	"github.com/tetratelabs/wazero/api"
)

func testManifest(tb testing.TB, concurrency string) *Manifest {
	tb.Helper()
	m, err := ParseManifest([]byte(`
name = "guest"
abi = 1
concurrency = "` + concurrency + `"

[[export]]
name = "guest_echo"
params = ["any"]
returns = "any"

[[export]]
name = "guest_fail"
params = []
returns = "any"

[[export]]
name = "guest_trap"
params = []
returns = "any"

[[export]]
name = "guest_sha256"
params = ["bytes"]
returns = "bytes"

[[export]]
name = "guest_loop"
params = []
returns = "any"

[[export]]
name = "guest_badtype"
params = []
returns = "int"

[[export]]
name = "guest_echobytes"
params = ["bytes"]
returns = "bytes"
`))
	if err != nil {
		tb.Fatalf("manifest: %v", err)
	}
	return m
}

var wasiPermits = LoaderConfig{PermittedImports: []string{"wasi_snapshot_preview1"}}

func TestLoadModuleRejectsUngrantedImports(t *testing.T) {
	wasm := exttest.BuildGuest(t, "")
	_, err := LoadModule(context.Background(), wasm, testManifest(t, "single"), LoaderConfig{})
	if err == nil || !strings.Contains(err.Error(), "wasi_snapshot_preview1") {
		t.Fatalf("import gate must name the offending module, got %v", err)
	}
}

// TestLoadModuleAcceptsCustomLogWriter prova apenas que LoaderConfig.Log e
// aceito e usado para instanciar o modulo (spec diz "diagOut"; nx_log ia
// direto para os.Stderr sem passar por config — achado de revisao). Nenhum
// export do guest de teste chama nx_log hoje, entao isto e um teste de
// encanamento (config chega ao host module), nao de conteudo do log.
func TestLoadModuleAcceptsCustomLogWriter(t *testing.T) {
	wasm := exttest.BuildGuest(t, "")
	var buf bytes.Buffer
	cfg := LoaderConfig{PermittedImports: []string{"wasi_snapshot_preview1"}, Log: &buf}
	m, err := LoadModule(context.Background(), wasm, testManifest(t, "single"), cfg)
	if err != nil {
		t.Fatalf("load with custom Log writer: %v", err)
	}
	defer m.Close(context.Background())
}

// TestLoadModuleRejectsNegativeMemoryMaxMB e o cinto-e-suspensorio do
// achado de revisao: ParseManifest ja rejeita memory_max_mb negativo, mas um
// *Manifest pode chegar aqui construido a mao. Sem esta guarda,
// uint32(-1)*16 estoura para um numero de paginas gigantesco e
// wazero.WithMemoryLimitPages entra em PANICO (nao erro) acima de 65536
// paginas — este teste prova que LoadModule devolve um erro ANTES de tocar
// em wazero, nunca chamando WithMemoryLimitPages com o valor estourado.
func TestLoadModuleRejectsNegativeMemoryMaxMB(t *testing.T) {
	m := testManifest(t, "single")
	m.MemoryMaxMB = -1
	_, err := LoadModule(context.Background(), []byte{}, m, LoaderConfig{})
	if err == nil || !strings.Contains(err.Error(), "memory_max_mb") {
		t.Fatalf("expected an error naming memory_max_mb, got %v", err)
	}
}

func TestLoadModuleHappyPath(t *testing.T) {
	wasm := exttest.BuildGuest(t, "")
	m, err := LoadModule(context.Background(), wasm, testManifest(t, "single"), wasiPermits)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	defer m.Close(context.Background())
}

func TestLoadModuleRejectsWrongABIVersion(t *testing.T) {
	wasm := exttest.BuildGuest(t, "-X main.abiVersionStr=99")
	_, err := LoadModule(context.Background(), wasm, testManifest(t, "single"), wasiPermits)
	if err == nil || !strings.Contains(err.Error(), "99") {
		t.Fatalf("handshake must report both versions, got %v", err)
	}
}

// fakeFuncDef implementa so os metodos de api.FunctionDefinition que
// validateABISignatures usa (ParamTypes/ResultTypes) — embute a interface
// (nil) para satisfazer os demais metodos sem precisar de um modulo wasm de
// verdade (achado de revisao: assinatura ABI errada so falhava na primeira
// chamada, como panico em results[0], nao no load).
type fakeFuncDef struct {
	api.FunctionDefinition
	params  []api.ValueType
	results []api.ValueType
}

func (f fakeFuncDef) ParamTypes() []api.ValueType  { return f.params }
func (f fakeFuncDef) ResultTypes() []api.ValueType { return f.results }

func validABIExports() map[string]api.FunctionDefinition {
	return map[string]api.FunctionDefinition{
		"nx_abi_version": fakeFuncDef{results: []api.ValueType{api.ValueTypeI32}},
		"nx_alloc":       fakeFuncDef{params: []api.ValueType{api.ValueTypeI32}, results: []api.ValueType{api.ValueTypeI32}},
		"nx_free":        fakeFuncDef{params: []api.ValueType{api.ValueTypeI32, api.ValueTypeI32}},
		"nx_call":        fakeFuncDef{params: []api.ValueType{api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32}, results: []api.ValueType{api.ValueTypeI64}},
	}
}

func TestValidateABISignaturesAcceptsCorrectShape(t *testing.T) {
	if err := validateABISignatures(validABIExports()); err != nil {
		t.Fatalf("correct ABI v1 signatures must be accepted, got %v", err)
	}
}

func TestValidateABISignaturesRejectsWrongArity(t *testing.T) {
	exports := validABIExports()
	// nx_call com um parametro a menos: sem esta checagem, isso so falha na
	// primeira chamada real com panico ao indexar results[0].
	exports["nx_call"] = fakeFuncDef{
		params:  []api.ValueType{api.ValueTypeI32, api.ValueTypeI32},
		results: []api.ValueType{api.ValueTypeI64},
	}
	err := validateABISignatures(exports)
	if err == nil || !strings.Contains(err.Error(), "nx_call") {
		t.Fatalf("expected an error naming nx_call, got %v", err)
	}
}

func TestValidateABISignaturesRejectsWrongResultType(t *testing.T) {
	exports := validABIExports()
	// nx_alloc devolvendo i64 em vez de i32.
	exports["nx_alloc"] = fakeFuncDef{
		params:  []api.ValueType{api.ValueTypeI32},
		results: []api.ValueType{api.ValueTypeI64},
	}
	err := validateABISignatures(exports)
	if err == nil || !strings.Contains(err.Error(), "nx_alloc") {
		t.Fatalf("expected an error naming nx_alloc, got %v", err)
	}
}

// Gate positivo: um modulo sem import NENHUM (o modulo wasm vazio de 8
// bytes) passa pelo gate e falha adiante, na checagem de exports — prova
// que o gate nao exige WASI nem host module para um guest limpo.
func TestLoadModuleImportGatePassesCleanModule(t *testing.T) {
	emptyWasm := []byte{0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00}
	_, err := LoadModule(context.Background(), emptyWasm, testManifest(t, "single"), LoaderConfig{})
	if err == nil || strings.Contains(err.Error(), "ungranted") {
		t.Fatalf("clean module must pass the import gate, got %v", err)
	}
	if !strings.Contains(err.Error(), "nx_abi_version") {
		t.Fatalf("expected missing-export error, got %v", err)
	}
}

package ext

import (
	"strings"
	"testing"
	"time"
)

const validManifest = `
name = "zstd"
abi = 1
min_noxy = "0.17.0"
concurrency = "stateless"

[[export]]
name = "zstd_compress"
params = ["bytes", "int"]
returns = "bytes"
`

func TestManifestParsesValid(t *testing.T) {
	m, err := ParseManifest([]byte(validManifest))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if m.Name != "zstd" || m.ABI != 1 || m.Concurrency != "stateless" {
		t.Fatalf("fields: %#v", m)
	}
	if m.Wasm != "ext.wasm" || m.MemoryMaxMB != 64 {
		t.Fatalf("defaults: %#v", m)
	}
	if len(m.Exports) != 1 || m.Exports[0].Name != "zstd_compress" {
		t.Fatalf("exports: %#v", m.Exports)
	}
}

func mustFail(t *testing.T, src, wantSubstr string) {
	t.Helper()
	if _, err := ParseManifest([]byte(src)); err == nil || !strings.Contains(err.Error(), wantSubstr) {
		t.Fatalf("want error containing %q, got %v", wantSubstr, err)
	}
}

func TestManifestRejects(t *testing.T) {
	mustFail(t, strings.Replace(validManifest, "abi = 1", "abi = 2", 1), "abi")
	mustFail(t, strings.Replace(validManifest, `"zstd_compress"`, `"compress"`, 1), "zstd_")
	mustFail(t, validManifest+"\nunknown_key = true\n", "unknown_key")
	mustFail(t, strings.Replace(validManifest, `"stateless"`, `"parallel"`, 1), "concurrency")
	mustFail(t, strings.Replace(validManifest, `["bytes", "int"]`, `["ref int"]`, 1), "type")
	mustFail(t, strings.Replace(validManifest, `name = "zstd"`, `name = "Zstd!"`, 1), "name")
	mustFail(t, strings.Replace(validManifest, `"zstd_compress"`, `"zstd_Foo-Bar"`, 1), "export")
	// M1: capabilities declaradas sao rejeitadas (host nao implementa nenhuma)
	mustFail(t, strings.Replace(validManifest, `abi = 1`, "abi = 1\ncapabilities = [\"net\"]", 1), "capabilities")
	// stateless nao pode declarar export stateful (spec §5)
	mustFail(t, validManifest+"\n[[export]]\nname = \"zstd_new\"\nparams = [\"int\"]\nreturns = \"int\"\nstateful = true\n", "stateful")
	// memory_max_mb negativo nao pode passar: uint32(negativo)*16 estoura em
	// LoadModule e wazero.WithMemoryLimitPages entra em panico acima de 65536
	// paginas (achado de revisao — sem essa rejeicao a VM inteira cai sem
	// recover). Este teste nunca chega em wazero: so exercita ParseManifest.
	mustFail(t, validManifest+"\nmemory_max_mb = -1\n", "memory_max_mb")
}

func TestManifestTypeVocabulary(t *testing.T) {
	for _, good := range []string{"int", "float", "bool", "string", "bytes", "any", "void", "int[]", "map[string]int", "Compressor"} {
		src := strings.Replace(validManifest, `returns = "bytes"`, `returns = "`+good+`"`, 1)
		if _, err := ParseManifest([]byte(src)); err != nil {
			t.Fatalf("type %q must be accepted: %v", good, err)
		}
	}
	mustFail(t, strings.Replace(validManifest, `returns = "bytes"`, `returns = "chan int"`, 1), "type")
}

func TestManifestMinNoxy(t *testing.T) {
	m, err := ParseManifest([]byte(validManifest))
	if err != nil {
		t.Fatal(err)
	}
	if err := m.CheckMinNoxy("v0.17.1"); err != nil {
		t.Fatalf("v0.17.1 >= 0.17.0 must pass: %v", err)
	}
	if err := m.CheckMinNoxy("v0.16.9"); err == nil {
		t.Fatal("v0.16.9 < 0.17.0 must fail")
	}
}

const validProcessManifest = `
name = "term"
abi = 1
kind = "process"
concurrency = "concurrent"
capabilities = ["tty"]
call_timeout_ms = 1000
handshake_timeout_ms = 250

[binaries]
linux-amd64 = "noxy-plugin-term-linux-amd64"
windows-amd64 = "noxy-plugin-term-windows-amd64.exe"

[[export]]
name = "term_read_key"
params = []
returns = "string"
timeout_ms = 0

[[export]]
name = "term_close"
params = []
returns = "void"
`

func TestProcessManifestParses(t *testing.T) {
	m, err := ParseManifest([]byte(validProcessManifest))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if m.Kind != KindProcess || m.Concurrency != "concurrent" || m.Wasm != "" || m.MemoryMaxMB != 0 {
		t.Fatalf("fields: %#v", m)
	}
	if got := m.CallTimeout(0); got != 0 {
		t.Fatalf("timeout_ms = 0 means no deadline, got %v", got)
	}
	if got := m.CallTimeout(1); got != time.Second {
		t.Fatalf("export without timeout_ms inherits call_timeout_ms, got %v", got)
	}
	if got := m.HandshakeTimeout(); got != 250*time.Millisecond {
		t.Fatalf("handshake: %v", got)
	}
	asset, ok := m.BinaryFor("windows", "amd64")
	if !ok || asset != "noxy-plugin-term-windows-amd64.exe" {
		t.Fatalf("BinaryFor: %q %v", asset, ok)
	}
	if _, ok := m.BinaryFor("freebsd", "amd64"); ok {
		t.Fatal("freebsd is not published")
	}
	if got := m.PublishedPlatforms(); len(got) != 2 || got[0] != "linux/amd64" || got[1] != "windows/amd64" {
		t.Fatalf("PublishedPlatforms: %v", got)
	}
	if got := m.Capabilities; len(got) != 1 || got[0] != "tty" {
		t.Fatalf("process capabilities are declarative and kept: %v", got)
	}
}

func TestProcessManifestDefaults(t *testing.T) {
	src := strings.NewReplacer("call_timeout_ms = 1000\n", "", "handshake_timeout_ms = 250\n", "", "timeout_ms = 0\n", "").Replace(validProcessManifest)
	m, err := ParseManifest([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	if m.CallTimeout(0) != 30*time.Second || m.HandshakeTimeout() != 5*time.Second {
		t.Fatalf("defaults: call %v handshake %v", m.CallTimeout(0), m.HandshakeTimeout())
	}
	wasm, err := ParseManifest([]byte(validManifest))
	if err != nil || wasm.Kind != KindWasm {
		t.Fatalf("kind defaults to wasm: %v %#v", err, wasm)
	}
}

func TestProcessManifestRejects(t *testing.T) {
	p := validProcessManifest
	mustFail(t, strings.Replace(p, `kind = "process"`, `kind = "dylib"`, 1), "kind")
	mustFail(t, strings.Replace(p, "[binaries]\nlinux-amd64 = \"noxy-plugin-term-linux-amd64\"\nwindows-amd64 = \"noxy-plugin-term-windows-amd64.exe\"\n", "", 1), "binaries")
	mustFail(t, strings.Replace(p, `linux-amd64 =`, `Linux_AMD64 =`, 1), "binaries key")
	mustFail(t, strings.Replace(p, `"noxy-plugin-term-linux-amd64"`, `"dist/noxy-plugin-term"`, 1), "asset name")
	mustFail(t, strings.Replace(p, `"noxy-plugin-term-linux-amd64"`, `".."`, 1), "asset name")
	mustFail(t, strings.Replace(p, `"noxy-plugin-term-linux-amd64"`, `"."`, 1), "asset name")
	mustFail(t, strings.Replace(p, `"noxy-plugin-term-windows-amd64.exe"`, `"noxy-plugin-term-windows-amd64"`, 1), ".exe")
	mustFail(t, strings.Replace(p, `kind = "process"`, "kind = \"process\"\nwasm = \"ext.wasm\"", 1), "wasm")
	mustFail(t, strings.Replace(p, `kind = "process"`, "kind = \"process\"\nmemory_max_mb = 64", 1), "memory_max_mb")
	mustFail(t, strings.Replace(p, `["tty"]`, `["Net!"]`, 1), "capability")
	mustFail(t, strings.Replace(p, `call_timeout_ms = 1000`, `call_timeout_ms = -1`, 1), "call_timeout_ms")
	mustFail(t, strings.Replace(p, `handshake_timeout_ms = 250`, `handshake_timeout_ms = -5`, 1), "handshake_timeout_ms")
	mustFail(t, strings.Replace(p, `timeout_ms = 0`, `timeout_ms = -1`, 1), "timeout_ms")
	mustFail(t, strings.Replace(p, `kind = "process"`, "kind = \"process\"\nrestart = true", 1), "restart")
	// stateless continua proibindo stateful, tambem em processo
	mustFail(t, strings.Replace(strings.Replace(p, `"concurrent"`, `"stateless"`, 1), "returns = \"void\"\n", "returns = \"void\"\nstateful = true\n", 1), "stateful")
	// restart so com stateless
	ok := strings.Replace(strings.Replace(p, `"concurrent"`, `"stateless"`, 1), `kind = "process"`, "kind = \"process\"\nrestart = true", 1)
	if _, err := ParseManifest([]byte(ok)); err != nil {
		t.Fatalf("restart with stateless must parse: %v", err)
	}
}

func TestWasmManifestRejectsProcessKeys(t *testing.T) {
	w := validManifest
	mustFail(t, strings.Replace(w, `abi = 1`, "abi = 1\n[binaries]\nlinux-amd64 = \"x\"", 1), "binaries")
	mustFail(t, strings.Replace(w, `abi = 1`, "abi = 1\ncall_timeout_ms = 10", 1), "call_timeout_ms")
	mustFail(t, strings.Replace(w, `abi = 1`, "abi = 1\nhandshake_timeout_ms = 10", 1), "handshake_timeout_ms")
	mustFail(t, strings.Replace(w, `abi = 1`, "abi = 1\nrestart = false", 1), "restart")
	mustFail(t, strings.Replace(w, `"stateless"`, `"concurrent"`, 1), "concurrent")
	mustFail(t, w+"timeout_ms = 5\n", "timeout_ms")
}

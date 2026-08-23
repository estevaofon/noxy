package ext

import (
	"strings"
	"testing"
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
	// stateless nao pode declarar export stateful (spec §5)
	mustFail(t, validManifest+"\n[[export]]\nname = \"zstd_new\"\nparams = [\"int\"]\nreturns = \"int\"\nstateful = true\n", "stateful")
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

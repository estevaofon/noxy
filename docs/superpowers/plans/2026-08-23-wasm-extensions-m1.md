# WASM Extension Mechanism (M1) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship phase M1 of the WASM extension mechanism: NXB codec, manifest, wazero loader with import gate and poisoning, VM integration registering extension exports as natives, minimal `noxy.sum` hashing, benchmarks, and docs.

**Architecture:** New package `internal/ext` owns everything wazero-facing (codec, manifest, loader, instances). The VM only gains a small glue file that detects `noxy_ext.toml` next to a resolved module and registers each manifest export as a signed contextual native. Test guests are written in Go and built on the fly with `GOOS=wasip1 GOARCH=wasm` (`//go:wasmexport`), so CI needs no extra toolchain.

**Tech Stack:** Go 1.24 (`//go:wasmexport` requires it), `github.com/tetratelabs/wazero` (pure-Go WASM runtime), `github.com/BurntSushi/toml` (pure-Go TOML).

**Spec:** `docs/superpowers/specs/2026-08-23-wasm-extension-mechanism-design.md` — read it before starting any task. Spec section numbers (§N) below refer to it.

## Global Constraints

- `CGO_ENABLED=0` cross-builds must keep passing: `linux/amd64`, `darwin/arm64`, `windows/amd64` (CI enforces a superset; see `.github/workflows/network-deadlines.yml`).
- Go module is `noxy-vm`; imports are `noxy-vm/internal/...`.
- ABI v1 exactly as spec §2: guest exports `nx_abi_version`, `nx_alloc`, `nx_free`, `nx_call`; host module name `"noxy:host/v1"` with `nx_fail` and `nx_log` only; `nx_call` returns `(ptr << 32) | len`, `0` = failed.
- NXB tags exactly as spec §2: `0x00 null, 0x01 bool, 0x02 int, 0x03 float, 0x04 string, 0x05 bytes, 0x06 array, 0x07 map, 0x08 struct`; little-endian scalars; `u32` lengths; depth cap 64; per-crossing size cap 64 MB default.
- Export names must start with `<manifest name>_` (spec §1).
- Manifest `abi` must equal `1`; unknown manifest keys are errors, not warnings (spec §10).
- Code comments follow the repo idiom: Portuguese, only where they state a constraint the code cannot (see `internal/value/cow.go` for tone). No “what the next line does” comments.
- Full gate after every task: `go test ./internal/... -count=1`.
- Commit messages in Portuguese, `feat(ext): ...` / `feat(pkgmanager): ...` style, ending with the Claude co-author trailer.
- Work on a feature branch off `develop` (create via superpowers:using-git-worktrees at execution start): `feature/issue-78-wasm-ext-m1`.

---

### Task 1: NXB codec

**Files:**
- Create: `internal/ext/nxb.go`
- Test: `internal/ext/nxb_test.go`

**Interfaces:**
- Consumes: `noxy-vm/internal/value` (`Value`, `NewInt/NewFloat/NewBool/NewNull/NewString/NewBytes/NewArray/NewMap`, `Retain`, `ObjArray`, `ObjMap.Snapshot/Set`, `ObjInstance`, `VAL_*` tags, accessors `Int()/Float()/Bool()`).
- Produces (used by Tasks 4–6):
  - `func EncodeArgs(args []value.Value, limits Limits) ([]byte, error)` — `u32` count + concatenated values.
  - `func EncodeValue(v value.Value, limits Limits) ([]byte, error)`
  - `func DecodeValue(data []byte, limits Limits) (value.Value, error)` — errors unless exactly one value consumes all bytes.
  - `func DecodeArgs(data []byte, limits Limits) ([]value.Value, error)` — inverse of `EncodeArgs` (used by tests and future SDKs).
  - `type Limits struct { MaxBytes int }` and `func DefaultLimits() Limits` (`MaxBytes: 64 << 20`).

- [ ] **Step 1: Write the failing tests**

```go
package ext

import (
	"strings"
	"testing"

	"noxy-vm/internal/value"
)

func mustRoundTrip(t *testing.T, v value.Value) value.Value {
	t.Helper()
	data, err := EncodeValue(v, DefaultLimits())
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	back, err := DecodeValue(data, DefaultLimits())
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	return back
}

func TestNXBScalarRoundTrip(t *testing.T) {
	if got := mustRoundTrip(t, value.NewInt(-42)); got.Type != value.VAL_INT || got.Int() != -42 {
		t.Fatalf("int round trip: %#v", got)
	}
	if got := mustRoundTrip(t, value.NewFloat(3.5)); got.Type != value.VAL_FLOAT || got.Float() != 3.5 {
		t.Fatalf("float round trip: %#v", got)
	}
	if got := mustRoundTrip(t, value.NewBool(true)); got.Type != value.VAL_BOOL || !got.Bool() {
		t.Fatalf("bool round trip: %#v", got)
	}
	if got := mustRoundTrip(t, value.NewNull()); got.Type != value.VAL_NULL {
		t.Fatalf("null round trip: %#v", got)
	}
	if got := mustRoundTrip(t, value.NewString("héllo")); got.Type != value.VAL_OBJ || got.Obj.(string) != "héllo" {
		t.Fatalf("string round trip: %#v", got)
	}
	if got := mustRoundTrip(t, value.NewBytes("\x00\x01\xff")); got.Type != value.VAL_BYTES || got.Obj.(string) != "\x00\x01\xff" {
		t.Fatalf("bytes round trip: %#v", got)
	}
}

func TestNXBArrayMapRoundTrip(t *testing.T) {
	arr := value.NewArray([]value.Value{value.NewInt(1), value.NewString("a")})
	got := mustRoundTrip(t, arr)
	elems := got.Obj.(*value.ObjArray).Elements
	if len(elems) != 2 || elems[0].Int() != 1 || elems[1].Obj.(string) != "a" {
		t.Fatalf("array round trip: %#v", got)
	}

	m := value.NewMap()
	m.Obj.(*value.ObjMap).Set("k", value.NewInt(7))
	m.Obj.(*value.ObjMap).Set(int64(3), value.NewString("x"))
	back := mustRoundTrip(t, m).Obj.(*value.ObjMap)
	if v, ok := back.Get("k"); !ok || v.Int() != 7 {
		t.Fatalf("map string key: %#v", back.Snapshot())
	}
	if v, ok := back.Get(int64(3)); !ok || v.Obj.(string) != "x" {
		t.Fatalf("map int key: %#v", back.Snapshot())
	}
}

func TestNXBStructEncodesDecodesAsMap(t *testing.T) {
	def := &value.ObjStruct{Name: "Point", Fields: []string{"x", "y"}}
	inst := value.NewInstanceWith(def, map[string]value.Value{
		"x": value.NewInt(1), "y": value.NewInt(2),
	})
	back := mustRoundTrip(t, inst)
	mp, ok := back.Obj.(*value.ObjMap)
	if !ok {
		t.Fatalf("struct must decode to struct-shaped map (spec §3), got %#v", back)
	}
	if v, ok := mp.Get("y"); !ok || v.Int() != 2 {
		t.Fatalf("field y: %#v", mp.Snapshot())
	}
}

func TestNXBRejectsNonEncodable(t *testing.T) {
	ch := value.NewChannel(1)
	if _, err := EncodeValue(ch, DefaultLimits()); err == nil || !strings.Contains(err.Error(), "channel") {
		t.Fatalf("channel must be rejected by name, got %v", err)
	}
}

func TestNXBDepthCap(t *testing.T) {
	v := value.NewArray(nil)
	for i := 0; i < 70; i++ {
		v = value.NewArray([]value.Value{v})
	}
	if _, err := EncodeValue(v, DefaultLimits()); err == nil || !strings.Contains(err.Error(), "depth") {
		t.Fatalf("expected depth error, got %v", err)
	}
}

func TestNXBSizeCap(t *testing.T) {
	big := value.NewBytes(strings.Repeat("a", 100))
	if _, err := EncodeValue(big, Limits{MaxBytes: 50}); err == nil {
		t.Fatal("expected size cap error")
	}
}

func TestNXBDeterministicMapEncoding(t *testing.T) {
	build := func() value.Value {
		m := value.NewMap()
		om := m.Obj.(*value.ObjMap)
		om.Set("b", value.NewInt(2))
		om.Set(int64(9), value.NewInt(9))
		om.Set("a", value.NewInt(1))
		return m
	}
	d1, err1 := EncodeValue(build(), DefaultLimits())
	d2, err2 := EncodeValue(build(), DefaultLimits())
	if err1 != nil || err2 != nil || string(d1) != string(d2) {
		t.Fatalf("map encoding must be deterministic")
	}
}

func TestNXBTruncatedInputFails(t *testing.T) {
	data, _ := EncodeValue(value.NewString("hello"), DefaultLimits())
	if _, err := DecodeValue(data[:len(data)-2], DefaultLimits()); err == nil {
		t.Fatal("truncated input must fail")
	}
}

func TestNXBArgsRoundTrip(t *testing.T) {
	args := []value.Value{value.NewInt(1), value.NewBytes("zz")}
	data, err := EncodeArgs(args, DefaultLimits())
	if err != nil {
		t.Fatalf("encode args: %v", err)
	}
	back, err := DecodeArgs(data, DefaultLimits())
	if err != nil || len(back) != 2 || back[0].Int() != 1 || back[1].Obj.(string) != "zz" {
		t.Fatalf("args round trip: %v %#v", err, back)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/ext/ -run TestNXB -v`
Expected: FAIL (package does not compile — functions undefined).

- [ ] **Step 3: Implement the codec**

```go
// Package ext implementa o mecanismo de extensoes WASM (spec
// docs/superpowers/specs/2026-08-23-wasm-extension-mechanism-design.md).
package ext

import (
	"encoding/binary"
	"fmt"
	"math"
	"sort"

	"noxy-vm/internal/value"
)

// Tags NXB v1 — append-only (spec §10): valores existentes nunca mudam.
const (
	nxbNull   = 0x00
	nxbBool   = 0x01
	nxbInt    = 0x02
	nxbFloat  = 0x03
	nxbString = 0x04
	nxbBytes  = 0x05
	nxbArray  = 0x06
	nxbMap    = 0x07
	nxbStruct = 0x08
)

const maxNXBDepth = 64

type Limits struct {
	MaxBytes int
}

func DefaultLimits() Limits { return Limits{MaxBytes: 64 << 20} }

type nxbEncoder struct {
	buf    []byte
	limits Limits
}

func (e *nxbEncoder) grow(n int) error {
	if len(e.buf)+n > e.limits.MaxBytes {
		return fmt.Errorf("nxb: encoded payload exceeds %d bytes", e.limits.MaxBytes)
	}
	return nil
}

func (e *nxbEncoder) writeU32(v uint32) {
	e.buf = binary.LittleEndian.AppendUint32(e.buf, v)
}

func (e *nxbEncoder) encode(v value.Value, depth int) error {
	if depth > maxNXBDepth {
		return fmt.Errorf("nxb: value nesting exceeds depth %d", maxNXBDepth)
	}
	if err := e.grow(9); err != nil {
		return err
	}
	switch v.Type {
	case value.VAL_NULL:
		e.buf = append(e.buf, nxbNull)
	case value.VAL_BOOL:
		b := byte(0)
		if v.Bool() {
			b = 1
		}
		e.buf = append(e.buf, nxbBool, b)
	case value.VAL_INT:
		e.buf = append(e.buf, nxbInt)
		e.buf = binary.LittleEndian.AppendUint64(e.buf, uint64(v.Int()))
	case value.VAL_FLOAT:
		e.buf = append(e.buf, nxbFloat)
		e.buf = binary.LittleEndian.AppendUint64(e.buf, math.Float64bits(v.Float()))
	case value.VAL_BYTES:
		return e.encodeBlob(nxbBytes, v.Obj.(string))
	case value.VAL_OBJ:
		switch obj := v.Obj.(type) {
		case string:
			return e.encodeBlob(nxbString, obj)
		case *value.ObjArray:
			e.buf = append(e.buf, nxbArray)
			e.writeU32(uint32(len(obj.Elements)))
			for _, element := range obj.Elements {
				if err := e.encode(element, depth+1); err != nil {
					return err
				}
			}
		case *value.ObjMap:
			return e.encodeMap(obj, depth)
		case *value.ObjInstance:
			return e.encodeStruct(obj, depth)
		default:
			return fmt.Errorf("nxb: value of kind %T cannot cross the extension boundary", obj)
		}
	case value.VAL_FUNCTION, value.VAL_NATIVE:
		return fmt.Errorf("nxb: callable values cannot cross the extension boundary")
	case value.VAL_CHANNEL:
		return fmt.Errorf("nxb: channel values cannot cross the extension boundary")
	case value.VAL_WAITGROUP:
		return fmt.Errorf("nxb: waitgroup values cannot cross the extension boundary")
	case value.VAL_REF:
		return fmt.Errorf("nxb: ref values cannot cross the extension boundary")
	case value.VAL_TASK:
		return fmt.Errorf("nxb: task values cannot cross the extension boundary")
	default:
		return fmt.Errorf("nxb: unsupported value type %d", v.Type)
	}
	return nil
}

func (e *nxbEncoder) encodeBlob(tag byte, data string) error {
	if err := e.grow(5 + len(data)); err != nil {
		return err
	}
	e.buf = append(e.buf, tag)
	e.writeU32(uint32(len(data)))
	e.buf = append(e.buf, data...)
	return nil
}

// encodeMap ordena as chaves (ints antes de strings, cada grupo em ordem)
// para que a codificacao seja deterministica (spec §2, "deterministic").
func (e *nxbEncoder) encodeMap(obj *value.ObjMap, depth int) error {
	snapshot := obj.Snapshot()
	intKeys := make([]int64, 0, len(snapshot))
	strKeys := make([]string, 0, len(snapshot))
	for key := range snapshot {
		switch k := key.(type) {
		case int64:
			intKeys = append(intKeys, k)
		case string:
			strKeys = append(strKeys, k)
		default:
			return fmt.Errorf("nxb: map key of type %T cannot cross the extension boundary", key)
		}
	}
	sort.Slice(intKeys, func(i, j int) bool { return intKeys[i] < intKeys[j] })
	sort.Strings(strKeys)
	e.buf = append(e.buf, nxbMap)
	e.writeU32(uint32(len(snapshot)))
	for _, k := range intKeys {
		if err := e.encode(value.NewInt(k), depth+1); err != nil {
			return err
		}
		if err := e.encode(snapshot[k], depth+1); err != nil {
			return err
		}
	}
	for _, k := range strKeys {
		if err := e.encode(value.NewString(k), depth+1); err != nil {
			return err
		}
		if err := e.encode(snapshot[k], depth+1); err != nil {
			return err
		}
	}
	return nil
}

func (e *nxbEncoder) encodeStruct(obj *value.ObjInstance, depth int) error {
	e.buf = append(e.buf, nxbStruct)
	name := obj.Struct.Name
	if err := e.grow(5 + len(name)); err != nil {
		return err
	}
	e.writeU32(uint32(len(name)))
	e.buf = append(e.buf, name...)
	e.writeU32(uint32(len(obj.Struct.Fields)))
	for _, field := range obj.Struct.Fields {
		if err := e.encodeBlob(nxbString, field); err != nil {
			return err
		}
		if err := e.encode(obj.Fields[field], depth+1); err != nil {
			return err
		}
	}
	return nil
}

func EncodeValue(v value.Value, limits Limits) ([]byte, error) {
	e := &nxbEncoder{limits: limits}
	if err := e.encode(v, 0); err != nil {
		return nil, err
	}
	return e.buf, nil
}

func EncodeArgs(args []value.Value, limits Limits) ([]byte, error) {
	e := &nxbEncoder{limits: limits}
	e.writeU32(uint32(len(args)))
	for _, arg := range args {
		if err := e.encode(arg, 0); err != nil {
			return nil, err
		}
	}
	return e.buf, nil
}

type nxbDecoder struct {
	data []byte
	pos  int
}

func (d *nxbDecoder) readByte() (byte, error) {
	if d.pos >= len(d.data) {
		return 0, fmt.Errorf("nxb: truncated input at offset %d", d.pos)
	}
	b := d.data[d.pos]
	d.pos++
	return b, nil
}

func (d *nxbDecoder) readU32() (uint32, error) {
	if d.pos+4 > len(d.data) {
		return 0, fmt.Errorf("nxb: truncated input at offset %d", d.pos)
	}
	v := binary.LittleEndian.Uint32(d.data[d.pos:])
	d.pos += 4
	return v, nil
}

func (d *nxbDecoder) readU64() (uint64, error) {
	if d.pos+8 > len(d.data) {
		return 0, fmt.Errorf("nxb: truncated input at offset %d", d.pos)
	}
	v := binary.LittleEndian.Uint64(d.data[d.pos:])
	d.pos += 8
	return v, nil
}

func (d *nxbDecoder) readBlob() (string, error) {
	n, err := d.readU32()
	if err != nil {
		return "", err
	}
	if d.pos+int(n) > len(d.data) {
		return "", fmt.Errorf("nxb: truncated blob at offset %d", d.pos)
	}
	s := string(d.data[d.pos : d.pos+int(n)])
	d.pos += int(n)
	return s, nil
}

func (d *nxbDecoder) decode(depth int) (value.Value, error) {
	if depth > maxNXBDepth {
		return value.NewNull(), fmt.Errorf("nxb: value nesting exceeds depth %d", maxNXBDepth)
	}
	tag, err := d.readByte()
	if err != nil {
		return value.NewNull(), err
	}
	switch tag {
	case nxbNull:
		return value.NewNull(), nil
	case nxbBool:
		b, err := d.readByte()
		if err != nil {
			return value.NewNull(), err
		}
		return value.NewBool(b != 0), nil
	case nxbInt:
		v, err := d.readU64()
		if err != nil {
			return value.NewNull(), err
		}
		return value.NewInt(int64(v)), nil
	case nxbFloat:
		v, err := d.readU64()
		if err != nil {
			return value.NewNull(), err
		}
		return value.NewFloat(math.Float64frombits(v)), nil
	case nxbString:
		s, err := d.readBlob()
		if err != nil {
			return value.NewNull(), err
		}
		return value.NewString(s), nil
	case nxbBytes:
		s, err := d.readBlob()
		if err != nil {
			return value.NewNull(), err
		}
		return value.NewBytes(s), nil
	case nxbArray:
		count, err := d.readU32()
		if err != nil {
			return value.NewNull(), err
		}
		elements := make([]value.Value, 0, count)
		for i := uint32(0); i < count; i++ {
			element, err := d.decode(depth + 1)
			if err != nil {
				return value.NewNull(), err
			}
			elements = append(elements, element)
		}
		// RC: o array e dono duravel de cada elemento (construtor retem),
		// espelhando json_population.go.
		return value.NewArray(elements), nil
	case nxbMap:
		return d.decodeMap(depth)
	case nxbStruct:
		// Struct volta como map com forma de struct (spec §3): o nome e
		// descartado, a validacao a jusante e estrutural.
		if _, err := d.readBlob(); err != nil {
			return value.NewNull(), err
		}
		return d.decodeFields(depth)
	default:
		return value.NewNull(), fmt.Errorf("nxb: unknown tag 0x%02x at offset %d", tag, d.pos-1)
	}
}

func (d *nxbDecoder) decodeMap(depth int) (value.Value, error) {
	count, err := d.readU32()
	if err != nil {
		return value.NewNull(), err
	}
	result := value.NewMap()
	mapping := result.Obj.(*value.ObjMap)
	for i := uint32(0); i < count; i++ {
		key, err := d.decode(depth + 1)
		if err != nil {
			return value.NewNull(), err
		}
		item, err := d.decode(depth + 1)
		if err != nil {
			return value.NewNull(), err
		}
		value.Retain(item) // RC: o map e dono duravel de cada valor
		switch key.Type {
		case value.VAL_INT:
			mapping.Set(key.Int(), item)
		case value.VAL_OBJ:
			s, ok := key.Obj.(string)
			if !ok {
				return value.NewNull(), fmt.Errorf("nxb: invalid map key")
			}
			mapping.Set(s, item)
		default:
			return value.NewNull(), fmt.Errorf("nxb: invalid map key tag")
		}
	}
	return result, nil
}

func (d *nxbDecoder) decodeFields(depth int) (value.Value, error) {
	count, err := d.readU32()
	if err != nil {
		return value.NewNull(), err
	}
	result := value.NewMap()
	mapping := result.Obj.(*value.ObjMap)
	for i := uint32(0); i < count; i++ {
		nameTag, err := d.readByte()
		if err != nil || nameTag != nxbString {
			return value.NewNull(), fmt.Errorf("nxb: struct field name must be a string")
		}
		name, err := d.readBlob()
		if err != nil {
			return value.NewNull(), err
		}
		item, err := d.decode(depth + 1)
		if err != nil {
			return value.NewNull(), err
		}
		value.Retain(item) // RC: o map e dono duravel de cada valor
		mapping.Set(name, item)
	}
	return result, nil
}

func DecodeValue(data []byte, limits Limits) (value.Value, error) {
	if len(data) > limits.MaxBytes {
		return value.NewNull(), fmt.Errorf("nxb: payload exceeds %d bytes", limits.MaxBytes)
	}
	d := &nxbDecoder{data: data}
	v, err := d.decode(0)
	if err != nil {
		return value.NewNull(), err
	}
	if d.pos != len(data) {
		return value.NewNull(), fmt.Errorf("nxb: %d trailing bytes after value", len(data)-d.pos)
	}
	return v, nil
}

func DecodeArgs(data []byte, limits Limits) ([]value.Value, error) {
	if len(data) > limits.MaxBytes {
		return nil, fmt.Errorf("nxb: payload exceeds %d bytes", limits.MaxBytes)
	}
	d := &nxbDecoder{data: data}
	count, err := d.readU32()
	if err != nil {
		return nil, err
	}
	args := make([]value.Value, 0, count)
	for i := uint32(0); i < count; i++ {
		arg, err := d.decode(0)
		if err != nil {
			return nil, err
		}
		args = append(args, arg)
	}
	if d.pos != len(data) {
		return nil, fmt.Errorf("nxb: %d trailing bytes after arguments", len(data)-d.pos)
	}
	return args, nil
}
```

Note for the implementer: if `value.NewInstanceWith` or field access differs from what the test assumes, check `internal/value/value.go:768-793` — the constructors exist as `NewInstance`, `NewInstanceWith`, `NewInstanceAdopting`.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/ext/ -run TestNXB -v` → all PASS. Then `go test ./internal/... -count=1` → PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/ext/nxb.go internal/ext/nxb_test.go
git commit -m "feat(ext): codec NXB v1 para a fronteira de extensões (issue #78, spec §2)"
```

---

### Task 2: Manifest parsing and validation

**Files:**
- Create: `internal/ext/manifest.go`
- Test: `internal/ext/manifest_test.go`
- Modify: `go.mod` (add `github.com/BurntSushi/toml`)

**Interfaces:**
- Produces (used by Tasks 4–7):
  - `type Manifest struct { Name string; ABI int; MinNoxy string; Concurrency string; Capabilities []string; MemoryMaxMB int; Wasm string; Exports []ExportDecl }`
  - `type ExportDecl struct { Name string; Params []string; Returns string; Stateful bool }`
  - `func ParseManifest(data []byte) (*Manifest, error)` — parse + full validation, no version check.
  - `func (m *Manifest) CheckMinNoxy(current string) error` — compares `min_noxy` against `internal/version.Version` format (`v0.17.1`).
  - Defaults applied by `ParseManifest`: `Concurrency` empty → `"single"`; `Wasm` empty → `"ext.wasm"`; `MemoryMaxMB` 0 → 64; ceiling 256 (`hostMemoryCeilingMB`).

- [ ] **Step 1: Add the TOML dependency**

Run: `go get github.com/BurntSushi/toml@latest` then `go mod tidy`.
Verify it is pure Go: `go build ./...` still works with `$env:CGO_ENABLED = "0"`.

- [ ] **Step 2: Write the failing tests**

```go
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
	// stateless nao pode declarar export stateful (spec §5)
	mustFail(t, validManifest+"\n[[export]]\nname = \"zstd_new\"\nparams = [\"int\"]\nreturns = \"int\"\nstateful = true\n", "stateful")
	// M1: capabilities declaradas sao rejeitadas (host nao implementa nenhuma)
	mustFail(t, strings.Replace(validManifest, `abi = 1`, "abi = 1\ncapabilities = [\"net\"]", 1), "capabilities")
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
```

- [ ] **Step 3: Run tests to verify they fail**

Run: `go test ./internal/ext/ -run TestManifest -v` → FAIL (undefined symbols).

- [ ] **Step 4: Implement**

```go
package ext

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"
)

const (
	defaultMemoryMB     = 64
	hostMemoryCeilingMB = 256
	supportedABI        = 1
)

type ExportDecl struct {
	Name     string   `toml:"name"`
	Params   []string `toml:"params"`
	Returns  string   `toml:"returns"`
	Stateful bool     `toml:"stateful"`
}

type Manifest struct {
	Name         string       `toml:"name"`
	ABI          int          `toml:"abi"`
	MinNoxy      string       `toml:"min_noxy"`
	Concurrency  string       `toml:"concurrency"`
	Capabilities []string     `toml:"capabilities"`
	MemoryMaxMB  int          `toml:"memory_max_mb"`
	Wasm         string       `toml:"wasm"`
	Exports      []ExportDecl `toml:"export"`
}

var manifestNameRE = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

var scalarTypeNames = map[string]bool{
	"int": true, "float": true, "bool": true, "string": true,
	"bytes": true, "any": true,
}

var structTypeNameRE = regexp.MustCompile(`^[A-Z][A-Za-z0-9_]*$`)

// validTypeName aceita o vocabulario da spec §7: escalares, void, T[],
// map[K]V e nomes de struct declarados no wrapper .nx.
func validTypeName(name string) bool {
	switch {
	case scalarTypeNames[name], name == "void":
		return true
	case strings.HasSuffix(name, "[]"):
		return validTypeName(strings.TrimSuffix(name, "[]"))
	case strings.HasPrefix(name, "map[") && strings.Contains(name, "]"):
		closing := strings.Index(name, "]")
		key := name[len("map["):closing]
		elem := name[closing+1:]
		return (key == "string" || key == "int") && elem != "" && validTypeName(elem)
	default:
		return structTypeNameRE.MatchString(name)
	}
}

func ParseManifest(data []byte) (*Manifest, error) {
	var m Manifest
	meta, err := toml.Decode(string(data), &m)
	if err != nil {
		return nil, fmt.Errorf("noxy_ext.toml: %w", err)
	}
	// Chaves desconhecidas sao erro, nao warning (spec §10): typo falha na
	// publicacao, nao silenciosamente em runtime.
	if undecoded := meta.Undecoded(); len(undecoded) != 0 {
		return nil, fmt.Errorf("noxy_ext.toml: unknown key %q", undecoded[0].String())
	}
	if !manifestNameRE.MatchString(m.Name) {
		return nil, fmt.Errorf("noxy_ext.toml: invalid extension name %q", m.Name)
	}
	if m.ABI != supportedABI {
		return nil, fmt.Errorf("noxy_ext.toml: unsupported abi %d (host supports %d)", m.ABI, supportedABI)
	}
	switch m.Concurrency {
	case "":
		m.Concurrency = "single"
	case "single", "stateless":
	default:
		return nil, fmt.Errorf("noxy_ext.toml: invalid concurrency %q", m.Concurrency)
	}
	if m.Wasm == "" {
		m.Wasm = "ext.wasm"
	}
	if m.MemoryMaxMB == 0 {
		m.MemoryMaxMB = defaultMemoryMB
	}
	if m.MemoryMaxMB > hostMemoryCeilingMB {
		return nil, fmt.Errorf("noxy_ext.toml: memory_max_mb %d exceeds host ceiling %d", m.MemoryMaxMB, hostMemoryCeilingMB)
	}
	if len(m.Exports) == 0 {
		return nil, fmt.Errorf("noxy_ext.toml: at least one [[export]] is required")
	}
	// M1 nao implementa capability nenhuma: aceitar a declaracao seria
	// prometer o que o host ignora (revisao do plano, item 6).
	if len(m.Capabilities) != 0 {
		return nil, fmt.Errorf("noxy_ext.toml: capabilities are not supported in this phase (M1)")
	}
	prefix := m.Name + "_"
	seen := map[string]bool{}
	for _, exp := range m.Exports {
		if !strings.HasPrefix(exp.Name, prefix) {
			return nil, fmt.Errorf("noxy_ext.toml: export %q must start with %q", exp.Name, prefix)
		}
		if seen[exp.Name] {
			return nil, fmt.Errorf("noxy_ext.toml: duplicate export %q", exp.Name)
		}
		seen[exp.Name] = true
		for _, p := range exp.Params {
			if p == "void" || !validTypeName(p) {
				return nil, fmt.Errorf("noxy_ext.toml: export %q: invalid param type %q", exp.Name, p)
			}
		}
		if exp.Returns == "" || !validTypeName(exp.Returns) {
			return nil, fmt.Errorf("noxy_ext.toml: export %q: invalid return type %q", exp.Name, exp.Returns)
		}
		if m.Concurrency == "stateless" && exp.Stateful {
			return nil, fmt.Errorf("noxy_ext.toml: stateless extension cannot declare stateful export %q", exp.Name)
		}
	}
	return &m, nil
}

func parseVersion(v string) ([3]int, error) {
	var out [3]int
	trimmed := strings.TrimPrefix(strings.TrimSpace(v), "v")
	parts := strings.Split(trimmed, ".")
	if len(parts) != 3 {
		return out, fmt.Errorf("invalid version %q", v)
	}
	for i, part := range parts {
		n, err := strconv.Atoi(part)
		if err != nil {
			return out, fmt.Errorf("invalid version %q", v)
		}
		out[i] = n
	}
	return out, nil
}

func (m *Manifest) CheckMinNoxy(current string) error {
	if m.MinNoxy == "" {
		return nil
	}
	minimum, err := parseVersion(m.MinNoxy)
	if err != nil {
		return fmt.Errorf("noxy_ext.toml: %w", err)
	}
	have, err := parseVersion(current)
	if err != nil {
		return err
	}
	for i := 0; i < 3; i++ {
		if have[i] != minimum[i] {
			if have[i] < minimum[i] {
				return fmt.Errorf("extension %q requires noxy >= %s (running %s)", m.Name, m.MinNoxy, current)
			}
			return nil
		}
	}
	return nil
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/ext/ -v` → PASS. Then `go test ./internal/... -count=1`.

- [ ] **Step 6: Commit**

```bash
git add go.mod go.sum internal/ext/manifest.go internal/ext/manifest_test.go
git commit -m "feat(ext): parsing e validação do noxy_ext.toml (issue #78, spec §7)"
```

---

### Task 3: Go test guest fixture and build helper

**Files:**
- Create: `internal/ext/testdata/guest/main.go` (guest wasm source — `testdata/` is invisible to `go build ./...`)
- Create: `internal/ext/exttest/exttest.go` (build helper, importable by `internal/vm` tests too)
- Test: `internal/ext/exttest/exttest_test.go`

**Interfaces:**
- Produces: `func BuildGuest(tb testing.TB, ldflags string) []byte` — compiles the guest with `GOOS=wasip1 GOARCH=wasm -buildmode=c-shared`, caches per-`ldflags` per process, `tb.Skip`s if the local Go toolchain lacks wasip1 support (it won't on Go 1.24, but keeps the failure mode readable).
- Guest `nx_call` dispatch by `fn_index`: `0` = echo (returns args bytes verbatim), `1` = fail (calls `nx_fail("boom from guest")`, returns 0), `2` = trap (panics), `3` = sha256 (returns raw NXB `bytes` value of the sha256 of the whole args payload — used by the benchmark).
- The guest imports `wasi_snapshot_preview1` (Go runtime requirement) — this is exactly what makes it double as the import-gate rejection fixture in Task 4.

- [ ] **Step 1: Write the guest**

```go
// Guest de teste do mecanismo de extensoes: compilado para wasip1/wasm em
// tempo de teste por exttest.BuildGuest. O fn_index de nx_call despacha:
// 0 echo, 1 fail, 2 trap (panic), 3 sha256 do payload.
package main

import (
	"crypto/sha256"
	"encoding/binary"
	"unsafe"
)

//go:wasmimport noxy:host/v1 nx_fail
func nxFail(ptr, size uint32)

//go:wasmimport noxy:host/v1 nx_log
func nxLog(level, ptr, size uint32)

// abiVersionStr e sobrescrivel com -ldflags "-X main.abiVersionStr=99"
// para o teste de handshake de versao.
var abiVersionStr = "1"

var allocs = map[uint32][]byte{}

//go:wasmexport nx_abi_version
func nxABIVersion() uint32 {
	v := uint32(0)
	for _, c := range abiVersionStr {
		v = v*10 + uint32(c-'0')
	}
	return v
}

//go:wasmexport nx_alloc
func nxAlloc(size uint32) uint32 {
	if size == 0 {
		return 0
	}
	buf := make([]byte, size)
	ptr := uint32(uintptr(unsafe.Pointer(&buf[0])))
	allocs[ptr] = buf
	return ptr
}

//go:wasmexport nx_free
func nxFree(ptr, size uint32) {
	delete(allocs, ptr)
}

func region(ptr, size uint32) []byte {
	if size == 0 {
		return nil
	}
	return unsafe.Slice((*byte)(unsafe.Pointer(uintptr(ptr))), size)
}

func retBytes(data []byte) uint64 {
	ptr := nxAlloc(uint32(len(data)))
	copy(region(ptr, uint32(len(data))), data)
	return uint64(ptr)<<32 | uint64(len(data))
}

//go:wasmexport nx_call
func nxCall(fnIndex, argsPtr, argsLen uint32) uint64 {
	args := region(argsPtr, argsLen)
	switch fnIndex {
	case 0: // echo
		return retBytes(args)
	case 1: // fail
		msg := []byte("boom from guest")
		p := nxAlloc(uint32(len(msg)))
		copy(region(p, uint32(len(msg))), msg)
		nxFail(p, uint32(len(msg)))
		return 0
	case 2: // trap
		panic("guest trap")
	case 3: // sha256 do payload cru, devolvido como NXB bytes
		sum := sha256.Sum256(args)
		out := make([]byte, 0, 5+32)
		out = append(out, 0x05)
		out = binary.LittleEndian.AppendUint32(out, 32)
		out = append(out, sum[:]...)
		return retBytes(out)
	default:
		return 0
	}
}

func main() {}
```

- [ ] **Step 2: Write the build helper**

```go
// Package exttest compila o guest de teste (testdata/guest) para
// wasip1/wasm sob demanda, para que os testes de internal/ext e
// internal/vm nao precisem de toolchain alem do proprio Go.
package exttest

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
)

var (
	mu    sync.Mutex
	cache = map[string][]byte{}
)

func repoRoot(tb testing.TB) string {
	_, self, _, ok := runtime.Caller(0)
	if !ok {
		tb.Fatal("exttest: cannot locate caller")
	}
	// internal/ext/exttest/exttest.go -> raiz do repo
	return filepath.Dir(filepath.Dir(filepath.Dir(filepath.Dir(self))))
}

// BuildGuest compila testdata/guest com os ldflags dados e devolve os bytes
// do .wasm. O resultado e cacheado por ldflags dentro do processo.
func BuildGuest(tb testing.TB, ldflags string) []byte {
	tb.Helper()
	mu.Lock()
	defer mu.Unlock()
	if data, ok := cache[ldflags]; ok {
		return data
	}
	root := repoRoot(tb)
	out := filepath.Join(tb.TempDir(), "guest.wasm")
	args := []string{"build", "-buildmode=c-shared", "-o", out}
	if ldflags != "" {
		args = append(args, "-ldflags", ldflags)
	}
	args = append(args, "./internal/ext/testdata/guest")
	cmd := exec.Command("go", args...)
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "GOOS=wasip1", "GOARCH=wasm", "CGO_ENABLED=0")
	output, err := cmd.CombinedOutput()
	if err != nil {
		tb.Fatalf("exttest: go build guest: %v\n%s", err, output)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		tb.Fatalf("exttest: read guest: %v", err)
	}
	cache[ldflags] = data
	return data
}
```

- [ ] **Step 3: Write the smoke test**

```go
package exttest

import "testing"

func TestBuildGuestProducesWasm(t *testing.T) {
	data := BuildGuest(t, "")
	// Preambulo wasm: \0asm
	if len(data) < 8 || string(data[:4]) != "\x00asm" {
		t.Fatalf("not a wasm binary (%d bytes)", len(data))
	}
	if again := BuildGuest(t, ""); len(again) != len(data) {
		t.Fatal("cache must return the same artifact")
	}
}
```

- [ ] **Step 4: Run the test**

Run: `go test ./internal/ext/exttest/ -v`
Expected: PASS. If `//go:wasmexport` fails to compile, confirm the local toolchain is ≥ go1.24 (`go version`); the repo's `go.mod` already demands `toolchain go1.24.11`. If `-buildmode=c-shared` is rejected for wasip1, retry without it and with `-o guest.wasm`; on Go 1.24 c-shared is the documented mode for reactor-style wasmexport modules — record whichever works in a code comment.

- [ ] **Step 5: Commit**

```bash
git add internal/ext/testdata/guest/main.go internal/ext/exttest/
git commit -m "feat(ext): guest de teste wasip1 e helper de build (issue #78)"
```

---

### Task 4: Loader — runtime, host module, import gate, handshake

**Files:**
- Create: `internal/ext/loader.go`
- Test: `internal/ext/loader_test.go`
- Modify: `go.mod` (add `github.com/tetratelabs/wazero`)

**Interfaces:**
- Consumes: `Manifest` (Task 2), `exttest.BuildGuest` (Task 3).
- Produces (used by Tasks 5–6):
  - `type LoaderConfig struct { PermittedImports []string; CallTimeout time.Duration; MaxInstances int }` — extra import module names allowed beyond `"noxy:host/v1"` (production passes nil in M1; tests pass `[]string{"wasi_snapshot_preview1"}` for the Go guest, and when that name is permitted the loader also instantiates wazero's bundled WASI host module); `CallTimeout` bounds each `nx_call` (0 → 30s default); `MaxInstances` bounds the stateless pool (0 → `runtime.NumCPU()`).
  - `type Module struct` with fields `Manifest *Manifest` (exported) and unexported `runtime`, `compiled`, `mu`, `single`, `pool chan *instance`, `slots chan struct{}` (capacity semaphore, prefilled — see Task 5), `failed bool`, `nextID atomic.Uint64`, `limits Limits`, `callTimeout time.Duration`.
  - LoadModule also enables wazero's persistent compilation cache (`os.UserCacheDir()/noxy/wazero`), best-effort: without it every `noxy script.nx` recompiles the wasm from scratch.
  - `func LoadModule(ctx context.Context, wasmBytes []byte, manifest *Manifest, cfg LoaderConfig) (*Module, error)`
  - `func (m *Module) Close(ctx context.Context) error`
  - unexported `func (m *Module) newInstance(ctx context.Context) (*instance, error)` — instantiate + `_initialize` (if exported) + `nx_abi_version` handshake; `type instance struct { mod api.Module; alloc, free, call api.Function; }`.
  - Per-call host state: `type callState struct { failMsg string; failed bool }`, `type callStateKey struct{}` — `nx_fail` reads it from the call context (Task 5 injects it).

- [ ] **Step 1: Add wazero**

Run: `go get github.com/tetratelabs/wazero@latest` then `go mod tidy && go build ./...`.

- [ ] **Step 2: Write the failing tests**

```go
package ext

import (
	"context"
	"strings"
	"testing"

	"noxy-vm/internal/ext/exttest"
)

func testManifest(t *testing.T, concurrency string) *Manifest {
	t.Helper()
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
`))
	if err != nil {
		t.Fatalf("manifest: %v", err)
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
```

- [ ] **Step 3: Run tests to verify they fail**

Run: `go test ./internal/ext/ -run TestLoadModule -v` → FAIL (undefined symbols).

- [ ] **Step 4: Implement the loader**

```go
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
```

Note: if wazero's `FunctionDefinition.Import()` signature differs in the resolved version, check `api.FunctionDefinition` godoc — the information (module name, import name) is there under possibly `ImportName()`/`ModuleName()`; adapt while keeping the gate behavior and error message.

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/ext/ -run TestLoadModule -v` → PASS. Then `go test ./internal/... -count=1`.

- [ ] **Step 6: Commit**

```bash
git add go.mod go.sum internal/ext/loader.go internal/ext/loader_test.go
git commit -m "feat(ext): loader wazero com gate de imports e handshake de ABI (issue #78, spec §2, §9)"
```

---

### Task 5: Call convention, concurrency modes, poisoning

**Files:**
- Create: `internal/ext/call.go`
- Modify: `internal/ext/testdata/guest/main.go` (add fn_index 4 = infinite loop, fn_index 5 = declared-type liar)
- Test: `internal/ext/call_test.go`

**Interfaces:**
- Consumes: everything from Tasks 1–4 (including `Module.slots`/`Module.callTimeout` from Task 4's LoadModule).
- Produces (used by Task 6):
  - `func (m *Module) Call(ctx context.Context, fnIndex int, args []value.Value) (value.Value, error)` — full boundary: acquire instance → encode → alloc/write → `nx_call` under a `context.WithTimeout(ctx, m.callTimeout)` → read/decode → declared-return check → release instance. Error strings match spec §6: `extension '<name>' failed: <msg>`, `extension '<name>' trapped: <err>`, `extension '<name>' is poisoned by an earlier trap`, protocol violations name the extension. A timeout cancels the guest via the context (wazero `WithCloseOnContextDone`) and surfaces as a trap.
  - Stateless acquire/release uses the `slots` capacity semaphore: acquire takes a slot, then reuses a pooled instance or creates one; release ALWAYS returns the slot, and only non-poisoned instances return to the pool. No `created` counter, no mutex on this path — this is the fix for the lost-wakeup deadlock found in plan review.
  - Guest fixture gains: fn_index 4 (`guest_loop`: `for {}`, fixture for the timeout test) and fn_index 5 (`guest_badtype`: declared `returns = "int"` in the test manifest but returns a valid NXB string — fixture for the declared-type check).

- [ ] **Step 1: Write the failing tests**

```go
package ext

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"noxy-vm/internal/ext/exttest"
	"noxy-vm/internal/value"
)

func loadTestModule(t *testing.T, concurrency string) *Module {
	t.Helper()
	m, err := LoadModule(context.Background(), exttest.BuildGuest(t, ""), testManifest(t, concurrency), wasiPermits)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	t.Cleanup(func() { m.Close(context.Background()) })
	return m
}

func TestCallSha256RoundTrip(t *testing.T) {
	m := loadTestModule(t, "single")
	// Ida-e-volta completo usa fnIndex 3 (sha256): o retorno do guest e um
	// valor NXB legitimo (bytes). O echo (fnIndex 0) devolve o payload de
	// args verbatim — que NAO e um valor NXB unico — e por isso serve de
	// fixture para o teste de violacao de protocolo mais abaixo.
	sum, err := m.Call(context.Background(), 3, []value.Value{value.NewBytes("abc")})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if sum.Type != value.VAL_BYTES || len(sum.Obj.(string)) != 32 {
		t.Fatalf("sha256 must return 32 bytes, got %#v", sum)
	}
}

func TestCallFailBecomesError(t *testing.T) {
	m := loadTestModule(t, "single")
	_, err := m.Call(context.Background(), 1, nil)
	if err == nil || !strings.Contains(err.Error(), "extension 'guest' failed: boom from guest") {
		t.Fatalf("declared failure must carry the guest message, got %v", err)
	}
	// Falha declarada NAO envenena: a proxima chamada funciona.
	if _, err := m.Call(context.Background(), 3, []value.Value{value.NewBytes("x")}); err != nil {
		t.Fatalf("call after fail: %v", err)
	}
}

func TestCallTrapPoisonsSingleMode(t *testing.T) {
	m := loadTestModule(t, "single")
	_, err := m.Call(context.Background(), 2, nil)
	if err == nil || !strings.Contains(err.Error(), "extension 'guest' trapped") {
		t.Fatalf("trap must surface as trapped error, got %v", err)
	}
	_, err = m.Call(context.Background(), 3, []value.Value{value.NewBytes("x")})
	if err == nil || !strings.Contains(err.Error(), "poisoned by an earlier trap") {
		t.Fatalf("single mode must stay poisoned, got %v", err)
	}
}

func TestCallTrapReplacesStatelessInstance(t *testing.T) {
	m := loadTestModule(t, "stateless")
	if _, err := m.Call(context.Background(), 2, nil); err == nil {
		t.Fatal("trap must error")
	}
	// Stateless: instancia envenenada e descartada, a proxima chamada cria
	// uma fresca (spec §6).
	if _, err := m.Call(context.Background(), 3, []value.Value{value.NewBytes("x")}); err != nil {
		t.Fatalf("stateless must recover after trap: %v", err)
	}
}

func TestCallStatelessConcurrent(t *testing.T) {
	m := loadTestModule(t, "stateless")
	var wg sync.WaitGroup
	errs := make(chan error, 16)
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := m.Call(context.Background(), 3, []value.Value{value.NewBytes("payload")})
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent call: %v", err)
		}
	}
}

func TestCallProtocolViolation(t *testing.T) {
	m := loadTestModule(t, "single")
	// guest_echo (fnIndex 0) declara "any" e devolve o payload de args cru,
	// que nao e um valor NXB unico: violacao de protocolo (o decode falha),
	// nomeando a extensao. (A checagem de tipo declarado tem teste proprio
	// abaixo — aqui returns = "any" a pula.)
	_, err := m.Call(context.Background(), 0, []value.Value{value.NewInt(1)})
	if err == nil || !strings.Contains(err.Error(), "guest") {
		t.Fatalf("protocol violation must name the extension, got %v", err)
	}
}

func TestCallDeclaredTypeMismatch(t *testing.T) {
	m := loadTestModule(t, "single")
	// guest_badtype (fnIndex 5) declara returns = "int" no manifesto mas
	// devolve uma string NXB valida: checkDeclaredReturn TEM de recusar.
	_, err := m.Call(context.Background(), 5, nil)
	if err == nil || !strings.Contains(err.Error(), `declared return type "int"`) {
		t.Fatalf("declared-type mismatch must be enforced, got %v", err)
	}
}

func TestCallTimeoutBecomesTrap(t *testing.T) {
	m, err := LoadModule(context.Background(), exttest.BuildGuest(t, ""), testManifest(t, "single"),
		LoaderConfig{PermittedImports: []string{"wasi_snapshot_preview1"}, CallTimeout: 300 * time.Millisecond})
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	t.Cleanup(func() { m.Close(context.Background()) })
	start := time.Now()
	_, err = m.Call(context.Background(), 4, nil) // guest_loop: for {}
	if err == nil || !strings.Contains(err.Error(), "trapped") {
		t.Fatalf("infinite loop must become a trap via context cancellation, got %v", err)
	}
	if time.Since(start) > 10*time.Second {
		t.Fatal("timeout did not bound the call")
	}
}

func TestCallStatelessTrapDoesNotDeadlock(t *testing.T) {
	// MaxInstances = 1: o trap fecha a instancia SEM devolve-la ao pool; a
	// chamada seguinte so avanca se a VAGA (slot) for liberada. Regressao
	// do lost-wakeup apontado na revisao do plano. Rodar com -race.
	m, err := LoadModule(context.Background(), exttest.BuildGuest(t, ""), testManifest(t, "stateless"),
		LoaderConfig{PermittedImports: []string{"wasi_snapshot_preview1"}, MaxInstances: 1})
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	t.Cleanup(func() { m.Close(context.Background()) })
	if _, err := m.Call(context.Background(), 2, nil); err == nil {
		t.Fatal("trap must error")
	}
	done := make(chan error, 1)
	go func() {
		_, callErr := m.Call(context.Background(), 3, []value.Value{value.NewBytes("x")})
		done <- callErr
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("call after trap: %v", err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("deadlock: slot was not released after poisoned instance")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/ext/ -run TestCall -v` → FAIL (`m.Call` undefined).

- [ ] **Step 3: Implement**

```go
package ext

import (
	"context"
	"fmt"
	"strings"

	"noxy-vm/internal/value"
)

// acquire devolve uma instancia pronta e a funcao de release. No modo
// single, serializa por mutex; no stateless, um semaforo de vagas (slots)
// governa a capacidade: a vaga volta SEMPRE no release — inclusive quando a
// instancia foi envenenada e fechada — senao um trap com o pool esgotado
// deixaria goroutinas bloqueadas para sempre (lost wakeup; revisao do plano).
func (m *Module) acquire(ctx context.Context) (*instance, func(poisoned bool), error) {
	if m.pool == nil {
		m.mu.Lock()
		if m.failed {
			m.mu.Unlock()
			return nil, nil, fmt.Errorf("extension '%s' is poisoned by an earlier trap", m.Manifest.Name)
		}
		inst := m.single
		release := func(poisoned bool) {
			if poisoned {
				m.failed = true
				m.single = nil
				inst.mod.Close(context.Background())
			}
			m.mu.Unlock()
		}
		return inst, release, nil
	}

	<-m.slots // vaga de capacidade; devolvida incondicionalmente no release
	var inst *instance
	select {
	case inst = <-m.pool:
	default:
		created, err := m.newInstance(ctx)
		if err != nil {
			m.slots <- struct{}{}
			return nil, nil, err
		}
		inst = created
	}
	release := func(poisoned bool) {
		if poisoned {
			inst.mod.Close(context.Background())
		} else {
			m.pool <- inst
		}
		m.slots <- struct{}{}
	}
	return inst, release, nil
}

func (m *Module) Call(ctx context.Context, fnIndex int, args []value.Value) (value.Value, error) {
	name := m.Manifest.Name
	encoded, err := EncodeArgs(args, m.limits)
	if err != nil {
		return value.NewNull(), fmt.Errorf("extension '%s': %w", name, err)
	}
	inst, release, err := m.acquire(ctx)
	if err != nil {
		return value.NewNull(), err
	}
	poisoned := false
	defer func() { release(poisoned) }()

	state := &callState{}
	// Timeout por chamada: com WithCloseOnContextDone(true) no runtime, o
	// cancelamento derruba o guest em execucao — um loop infinito vira trap.
	timedCtx, cancel := context.WithTimeout(ctx, m.callTimeout)
	defer cancel()
	callCtx := context.WithValue(timedCtx, callStateKey{}, state)

	argsPtr := uint64(0)
	if len(encoded) != 0 {
		results, err := inst.alloc.Call(callCtx, uint64(len(encoded)))
		if err != nil {
			poisoned = true
			return value.NewNull(), fmt.Errorf("extension '%s' trapped: %v", name, err)
		}
		argsPtr = results[0]
		if !inst.mod.Memory().Write(uint32(argsPtr), encoded) {
			return value.NewNull(), fmt.Errorf("extension '%s': nx_alloc returned an out-of-memory region", name)
		}
	}

	results, err := inst.call.Call(callCtx, uint64(fnIndex), argsPtr, uint64(len(encoded)))
	if err != nil {
		poisoned = true
		return value.NewNull(), fmt.Errorf("extension '%s' trapped: %v", name, err)
	}
	// Os args so sao liberados DEPOIS de copiar o retorno: liberar antes
	// funcionaria hoje (o guest nao roda com o host no controle), mas e
	// fragilidade gratuita — revisao do plano.
	freeArgs := func() {
		if len(encoded) != 0 {
			inst.free.Call(callCtx, argsPtr, uint64(len(encoded)))
		}
	}

	packed := results[0]
	if packed == 0 {
		freeArgs()
		if state.failed {
			return value.NewNull(), fmt.Errorf("extension '%s' failed: %s", name, state.failMsg)
		}
		return value.NewNull(), fmt.Errorf("extension '%s': call returned 0 without nx_fail", name)
	}
	retPtr := uint32(packed >> 32)
	retLen := uint32(packed & 0xffffffff)
	data, ok := inst.mod.Memory().Read(retPtr, retLen)
	if !ok {
		freeArgs()
		return value.NewNull(), fmt.Errorf("extension '%s': result region out of guest memory", name)
	}
	// Copia antes do free: data aponta para a memoria linear do guest.
	owned := make([]byte, len(data))
	copy(owned, data)
	inst.free.Call(callCtx, uint64(retPtr), uint64(retLen))
	freeArgs()

	result, err := DecodeValue(owned, m.limits)
	if err != nil {
		return value.NewNull(), fmt.Errorf("extension '%s': invalid result: %w", name, err)
	}
	declared := ""
	if fnIndex >= 0 && fnIndex < len(m.Manifest.Exports) {
		declared = m.Manifest.Exports[fnIndex].Returns
	}
	if err := checkDeclaredReturn(result, declared); err != nil {
		return value.NewNull(), fmt.Errorf("extension '%s': %w", name, err)
	}
	return result, nil
}

// checkDeclaredReturn confere a forma do valor devolvido contra o tipo
// declarado no manifesto (spec §6, "protocol violation"): uma extensao
// mentirosa e pega na fronteira, nao a jusante.
func checkDeclaredReturn(v value.Value, declared string) error {
	switch {
	case declared == "" || declared == "any":
		return nil
	case declared == "void":
		if v.Type != value.VAL_NULL {
			return fmt.Errorf("declared void but returned a value")
		}
	case declared == "int":
		if v.Type != value.VAL_INT {
			return typeMismatch(declared)
		}
	case declared == "float":
		if v.Type != value.VAL_FLOAT {
			return typeMismatch(declared)
		}
	case declared == "bool":
		if v.Type != value.VAL_BOOL {
			return typeMismatch(declared)
		}
	case declared == "string":
		if v.Type != value.VAL_OBJ {
			return typeMismatch(declared)
		}
		if _, ok := v.Obj.(string); !ok {
			return typeMismatch(declared)
		}
	case declared == "bytes":
		if v.Type != value.VAL_BYTES {
			return typeMismatch(declared)
		}
	case strings.HasSuffix(declared, "[]"):
		if v.Type != value.VAL_OBJ {
			return typeMismatch(declared)
		}
		if _, ok := v.Obj.(*value.ObjArray); !ok {
			return typeMismatch(declared)
		}
	default:
		// map[...]... e nomes de struct chegam como map (spec §3).
		if v.Type != value.VAL_OBJ {
			return typeMismatch(declared)
		}
		if _, ok := v.Obj.(*value.ObjMap); !ok {
			return typeMismatch(declared)
		}
	}
	return nil
}

func typeMismatch(declared string) error {
	return fmt.Errorf("result does not match declared return type %q", declared)
}
```

Also extend the guest fixture — add these cases to the `switch fnIndex` in `internal/ext/testdata/guest/main.go`:

```go
	case 4: // loop infinito — fixture do teste de timeout de chamada
		for {
		}
	case 5: // NXB string valida com returns declarado "int" (fixture do
		// teste de checkDeclaredReturn: extensao mentirosa e pega na fronteira)
		msg := []byte("oops")
		out := make([]byte, 0, 5+len(msg))
		out = append(out, 0x04)
		out = binary.LittleEndian.AppendUint32(out, uint32(len(msg)))
		out = append(out, msg...)
		return retBytes(out)
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/ext/ -v -timeout 120s` → PASS (including `-race`: `go test ./internal/ext/ -race -run 'TestCallStateless' -timeout 120s`). Then `go test ./internal/... -count=1`.

- [ ] **Step 5: Commit**

```bash
git add internal/ext/call.go internal/ext/call_test.go
git commit -m "feat(ext): convenção de chamada, modos de concorrência e poisoning (issue #78, spec §5, §6)"
```

---

### Task 6: VM integration — detect manifest, register natives, E2E

**Files:**
- Create: `internal/vm/extensions.go`
- Modify: `internal/vm/vm.go` (add `SharedState` fields: `ExtMu sync.Mutex`, `Ext map[string]*ext.Module`, `ExtNames map[string]string` — next to the other registries around `internal/vm/vm.go:56-79`)
- Modify: `internal/vm/modules.go` (hook in `loadResolvedModule`, `resolvedFileModule` case, currently line ~133)
- Test: `internal/vm/extensions_e2e_test.go`

**Interfaces:**
- Consumes: `ext.ParseManifest`, `(*Manifest).CheckMinNoxy`, `ext.LoadModule`, `(*ext.Module).Call`, `value.NewContextualNativeWithSignature` via `vm.DefineContextualNativeWithSignature` (`internal/vm/vm.go:166-180`), `version.Version`, test helpers `write`/`captureVMSourceAtRoot` pattern from `internal/vm/generics_modules_e2e_test.go:23-51`.
- Produces:
  - `func (vm *VM) ensureExtensionLoaded(dir string) error` — idempotent per directory, cross-package name collision is an error.
  - package-level test hook `var extensionLoaderPermits []string` (nil in production; the E2E test sets it to `{"wasi_snapshot_preview1"}` because the Go guest needs WASI — real guests won't; grants land in M2).

- [ ] **Step 1: Write the failing E2E test**

```go
package vm

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"noxy-vm/internal/ext/exttest"
	"noxy-vm/internal/value"
)

const testExtManifest = `
name = "guest"
abi = 1

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
`

const testExtWrapper = `
func sha(data: bytes) -> bytes
    return guest_sha256(data)
end
`

func writeExtensionPackage(t *testing.T, root string) {
	t.Helper()
	pkg := filepath.Join(root, "noxy_libs", "guest")
	if err := os.MkdirAll(pkg, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile := func(name string, data []byte) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(pkg, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	writeFile("noxy_ext.toml", []byte(testExtManifest))
	writeFile("ext.wasm", exttest.BuildGuest(t, ""))
	writeFile("guest.nx", []byte(testExtWrapper))
}

func TestExtensionEndToEnd(t *testing.T) {
	root := t.TempDir()
	writeExtensionPackage(t, root)
	extensionLoaderPermits = []string{"wasi_snapshot_preview1"}
	t.Cleanup(func() { extensionLoaderPermits = nil })

	captured := captureVMSourceAtRoot(t, root, `
use guest as g
test_report(g.sha(to_bytes("abc")))
`)
	if captured.Type != value.VAL_BYTES || len(captured.Obj.(string)) != 32 {
		t.Fatalf("expected 32 sha bytes, got %#v", captured)
	}
}

func TestExtensionFailureIsRuntimeError(t *testing.T) {
	root := t.TempDir()
	writeExtensionPackage(t, root)
	extensionLoaderPermits = []string{"wasi_snapshot_preview1"}
	t.Cleanup(func() { extensionLoaderPermits = nil })

	machine := NewWithConfig(VMConfig{RootPath: root})
	code := compileVMSourceAtRoot(t, root, `
use guest as g
let x: any = guest_fail()
`)
	err := machine.Interpret(code)
	if err == nil || !strings.Contains(err.Error(), "boom from guest") {
		t.Fatalf("guest failure must surface as runtime error, got %v", err)
	}
}
```

Note: `captureVMSourceAtRoot` and `compileVMSourceAtRoot` live in `internal/vm/generics_modules_e2e_test.go` (same package) — reuse, don't duplicate. `guest_fail` is reachable without the wrapper because extension natives are globals, like all natives.

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/vm/ -run TestExtension -v`
Expected: FAIL — module loads but `guest_sha256` is an `undefined global variable` (no manifest detection yet), and `extensionLoaderPermits` is undefined (compile error first).

- [ ] **Step 3: Implement the glue**

`internal/vm/extensions.go`:

```go
package vm

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"noxy-vm/internal/ext"
	"noxy-vm/internal/value"
	"noxy-vm/internal/version"
)

// extensionLoaderPermits permite aos testes liberar modulos de import extras
// (o guest de teste em Go precisa de wasi_snapshot_preview1). Em producao e
// nil ate as capabilities chegarem (M2, spec §9).
var extensionLoaderPermits []string

// ensureExtensionLoaded carrega a extensao declarada em dir/noxy_ext.toml
// uma unica vez por SharedState e registra cada export como native global
// assinada. Nao ha caminho de descarga: modulos vivem ate o processo.
func (vm *VM) ensureExtensionLoaded(dir string) error {
	shared := vm.shared
	shared.ExtMu.Lock()
	defer shared.ExtMu.Unlock()
	if shared.Ext == nil {
		shared.Ext = make(map[string]*ext.Module)
		shared.ExtNames = make(map[string]string)
	}
	if _, loaded := shared.Ext[dir]; loaded {
		return nil
	}

	manifestData, err := os.ReadFile(filepath.Join(dir, "noxy_ext.toml"))
	if err != nil {
		return fmt.Errorf("extension manifest: %w", err)
	}
	manifest, err := ext.ParseManifest(manifestData)
	if err != nil {
		return err
	}
	if err := manifest.CheckMinNoxy(version.Version); err != nil {
		return err
	}
	if other, exists := shared.ExtNames[manifest.Name]; exists && other != dir {
		return fmt.Errorf("extension name %q already loaded from %s", manifest.Name, other)
	}
	wasmPath := filepath.Join(dir, manifest.Wasm)
	wasmBytes, err := os.ReadFile(wasmPath)
	if err != nil {
		return fmt.Errorf("extension %q: %w", manifest.Name, err)
	}
	if err := vm.verifyExtensionSum(dir, manifest, wasmBytes); err != nil {
		return err
	}

	module, err := ext.LoadModule(context.Background(), wasmBytes, manifest,
		ext.LoaderConfig{PermittedImports: extensionLoaderPermits})
	if err != nil {
		return err
	}
	for i, exp := range manifest.Exports {
		index := i
		sig := value.NativeSignature{
			Arity:      len(exp.Params),
			Params:     make([]value.ParamInfo, len(exp.Params)),
			ReturnType: signatureTypeName(exp.Returns),
		}
		for j, p := range exp.Params {
			sig.Params[j] = value.ParamInfo{TypeName: signatureTypeName(p)}
		}
		vm.DefineContextualNativeWithSignature(exp.Name, sig,
			func(_ value.NativeContext, args []value.Value) (value.Value, error) {
				return module.Call(context.Background(), index, args)
			})
	}
	shared.Ext[dir] = module
	shared.ExtNames[manifest.Name] = dir
	return nil
}

// signatureTypeName mapeia o vocabulario do manifesto para os TypeName que
// call_validation.go entende. M1: escalares passam direto; compostos e
// structs viram "any" (a checagem concreta acontece na fronteira NXB —
// checkDeclaredReturn — e no wrapper .nx tipado).
func signatureTypeName(declared string) string {
	switch declared {
	case "int", "float", "bool", "string", "bytes", "any", "void":
		return declared
	default:
		return "any"
	}
}

// verifyExtensionSum e preenchido na tarefa de noxy.sum; ate la, aceita.
func (vm *VM) verifyExtensionSum(dir string, manifest *ext.Manifest, wasmBytes []byte) error {
	return nil
}
```

Hook in `internal/vm/modules.go`, inside `loadResolvedModule`, `case resolvedFileModule:` — before reading the module file:

```go
	case resolvedFileModule:
		manifestPath := filepath.Join(filepath.Dir(source.Path), "noxy_ext.toml")
		if _, statErr := os.Stat(manifestPath); statErr == nil {
			if err := vm.ensureExtensionLoaded(filepath.Dir(source.Path)); err != nil {
				return value.NewNull(), fmt.Errorf("failed to load extension for module %s: %w", source.Name, err)
			}
		}
		content, err := os.ReadFile(source.Path)
		...
```

`SharedState` additions in `internal/vm/vm.go` (alongside `Files`, `Databases`, etc.):

```go
	// Extensoes WASM carregadas (spec 2026-08-23): chave = diretorio do
	// pacote; ExtNames detecta colisao de nome entre pacotes distintos.
	ExtMu    sync.Mutex
	Ext      map[string]*ext.Module
	ExtNames map[string]string
```

Check before finishing: `DefineContextualNativeWithSignature` at `internal/vm/vm.go:166-180` uses `vm.SetGlobal` (overwrite semantics) — the cross-package collision guard above (`ExtNames`) is what prevents silent rebinding, since within one package the manifest's duplicate-export check already ran.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/vm/ -run TestExtension -v` → PASS. Then the full gate `go test ./internal/... -count=1` and the example suite: `go run ./cmd/noxy noxy_examples/run_all_tests_concurrent.nx`.

- [ ] **Step 5: Commit**

```bash
git add internal/vm/extensions.go internal/vm/vm.go internal/vm/modules.go internal/vm/extensions_e2e_test.go
git commit -m "feat(vm): carga de extensões WASM no resolveModule e registro como natives assinadas (issue #78, spec §1, §7)"
```

---

### Task 7: Minimal `noxy.sum` — hash on `--get`, verify on load

**Files:**
- Create: `internal/pkgmanager/sumfile.go`
- Modify: `internal/pkgmanager/manager.go` (`downloadPackage`, after the `.git` removal at line ~81)
- Modify: `internal/vm/extensions.go` (`verifyExtensionSum` stub from Task 6)
- Test: `internal/pkgmanager/sumfile_test.go`, extend `internal/vm/extensions_e2e_test.go`

**Interfaces:**
- Produces:
  - `type SumFile struct { Entries map[string]string }` — key `"<pkgpath> <filename>"` (pkgpath = `noxy_libs`-relative with forward slashes, e.g. `github_com/acme/zstd`), value `"sha256:<hex>"`.
  - `func ParseSumFile(path string) (*SumFile, error)` (missing file → empty SumFile, no error)
  - `func (s *SumFile) Set(pkg, file, hexDigest string)`, `func (s *SumFile) Lookup(pkg, file string) (string, bool)`, `func (s *SumFile) Save(path string) error` (sorted lines: `<pkg> <file> sha256:<hex>`)
  - `func SumFilePath(root string) string` — THE single resolver for where `noxy.sum` lives (`filepath.Join(root, "noxy.sum")`). Writer (pkgmanager) and reader (VM) both go through it; plan review found they resolved different paths (cwd vs RootPath), which made verification silently fall into the no-entry path.
  - `func RecordExtensionSums(root, targetDir, localPath string) error` — exported so the VM-side integration test can exercise the exact writer the `--get` path uses and prove the keys match.
  - In `manager.go`: after `.git` removal, call `RecordExtensionSums(".", targetDir, localPath)` — `--get` runs at the project root, the same convention `noxy.mod` already relies on.
  - In `extensions.go`: `verifyExtensionSum` finds `noxy.sum` under `vm.Config.RootPath`; if the file exists **and** has an entry for this package's wasm, compare sha256 and refuse mismatch (`extension artifact mismatch for <pkg>/<file>: noxy.sum has <a>, disk has <b>`); no file or no entry → allow (M1 trust-on-first-use; full policy is the `noxy.sum` spec, spec §15). The package key is `source dir` relative to `<RootPath>/noxy_libs` with forward slashes; a dir outside `noxy_libs` (e.g. stdlib dev layout) skips verification.

- [ ] **Step 1: Write the failing tests**

```go
package pkgmanager

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSumFileRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "noxy.sum")
	s, err := ParseSumFile(path)
	if err != nil {
		t.Fatalf("missing file must parse as empty: %v", err)
	}
	s.Set("github_com/acme/zstd", "ext.wasm", "abc123")
	s.Set("github_com/acme/zstd", "noxy_ext.toml", "def456")
	if err := s.Save(path); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "github_com/acme/zstd ext.wasm sha256:abc123") {
		t.Fatalf("format: %s", data)
	}
	back, err := ParseSumFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := back.Lookup("github_com/acme/zstd", "ext.wasm"); !ok || got != "abc123" {
		t.Fatalf("lookup: %q %v", got, ok)
	}
}
```

And in `internal/vm/extensions_e2e_test.go` add:

```go
func TestExtensionSumMismatchRefusesLoad(t *testing.T) {
	root := t.TempDir()
	writeExtensionPackage(t, root)
	// A E2E instala em noxy_libs/guest; um noxy.sum com hash errado para o
	// ext.wasm deve recusar a carga (spec §8).
	sum := "guest ext.wasm sha256:0000000000000000000000000000000000000000000000000000000000000000\n"
	if err := os.WriteFile(filepath.Join(root, "noxy.sum"), []byte(sum), 0o644); err != nil {
		t.Fatal(err)
	}
	extensionLoaderPermits = []string{"wasi_snapshot_preview1"}
	t.Cleanup(func() { extensionLoaderPermits = nil })

	machine := NewWithConfig(VMConfig{RootPath: root})
	code := compileVMSourceAtRoot(t, root, "use guest as g\n")
	err := machine.Interpret(code)
	if err == nil || !strings.Contains(err.Error(), "mismatch") {
		t.Fatalf("sum mismatch must refuse load, got %v", err)
	}
}

// Ida-e-volta escritor→leitor: grava via o MESMO RecordExtensionSums do
// --get, adultera o artefato, e exige que a carga acuse mismatch. So passa
// se caminho do noxy.sum E formato da chave coincidirem entre pkgmanager e
// vm (revisao do plano: divergencia cwd/RootPath falhava em silencio).
func TestExtensionSumRoundTripViaPkgmanager(t *testing.T) {
	root := t.TempDir()
	writeExtensionPackage(t, root)
	pkgDir := filepath.Join(root, "noxy_libs", "guest")
	if err := pkgmanager.RecordExtensionSums(root, pkgDir, "guest"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pkgDir, "ext.wasm"), []byte("tampered"), 0o644); err != nil {
		t.Fatal(err)
	}
	extensionLoaderPermits = []string{"wasi_snapshot_preview1"}
	t.Cleanup(func() { extensionLoaderPermits = nil })

	machine := NewWithConfig(VMConfig{RootPath: root})
	code := compileVMSourceAtRoot(t, root, "use guest as g\n")
	err := machine.Interpret(code)
	if err == nil || !strings.Contains(err.Error(), "mismatch") {
		t.Fatalf("writer/reader must agree on noxy.sum path and key, got %v", err)
	}
}
```

(the test file imports `noxy-vm/internal/pkgmanager`.)

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./internal/pkgmanager/ ./internal/vm/ -run 'TestSum|TestExtensionSum' -v` → FAIL.

- [ ] **Step 3: Implement**

`internal/pkgmanager/sumfile.go`:

```go
package pkgmanager

import (
	"fmt"
	"os"
	"sort"
	"strings"
)

// SumFile e a forma minima (M1) do noxy.sum: uma linha por artefato de
// extensao, "<pkg> <arquivo> sha256:<hex>". O formato completo (fontes,
// TOFU, assinaturas) e spec separada — spec §8 e §15.
type SumFile struct {
	Entries map[string]string
}

func sumKey(pkg, file string) string { return pkg + " " + file }

func ParseSumFile(path string) (*SumFile, error) {
	s := &SumFile{Entries: map[string]string{}}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return s, nil
		}
		return nil, err
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) != 3 || !strings.HasPrefix(fields[2], "sha256:") {
			return nil, fmt.Errorf("noxy.sum: malformed line %q", line)
		}
		s.Entries[sumKey(fields[0], fields[1])] = strings.TrimPrefix(fields[2], "sha256:")
	}
	return s, nil
}

func (s *SumFile) Set(pkg, file, hexDigest string) {
	s.Entries[sumKey(pkg, file)] = hexDigest
}

func (s *SumFile) Lookup(pkg, file string) (string, bool) {
	digest, ok := s.Entries[sumKey(pkg, file)]
	return digest, ok
}

func (s *SumFile) Save(path string) error {
	lines := make([]string, 0, len(s.Entries))
	for key, digest := range s.Entries {
		lines = append(lines, key+" sha256:"+digest)
	}
	sort.Strings(lines)
	return os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644)
}

// SumFilePath e o UNICO resolvedor do caminho do noxy.sum: escrita
// (pkgmanager) e leitura (vm) passam por aqui — caminhos divergentes fariam
// a verificacao cair em silencio no ramo "sem entrada" (revisao do plano).
func SumFilePath(root string) string {
	return filepath.Join(root, "noxy.sum")
}
```

(add `path/filepath` to sumfile.go imports.)

In `manager.go`, after the `.git` removal block (line ~81), add:

```go
	// Artefatos executaveis (extensoes WASM) entram no noxy.sum ao serem
	// baixados — sem integridade nao ha distribuicao de binarios (spec §8).
	// "--get" roda na raiz do projeto (mesma convencao do noxy.mod).
	if err := RecordExtensionSums(".", targetDir, localPath); err != nil {
		fmt.Printf("Warning: failed to record noxy.sum entries: %s\n", err)
	}
```

with:

```go
func RecordExtensionSums(root, targetDir, localPath string) error {
	manifestPath := filepath.Join(targetDir, "noxy_ext.toml")
	manifestData, err := os.ReadFile(manifestPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	wasmName := "ext.wasm"
	for _, line := range strings.Split(string(manifestData), "\n") {
		// So a chave exata "wasm" — um prefixo pegaria "wasm_qualquer_coisa"
		// (revisao do plano).
		key, after, found := strings.Cut(line, "=")
		if found && strings.TrimSpace(key) == "wasm" {
			wasmName = strings.Trim(strings.TrimSpace(after), `"`)
		}
	}
	sums, err := ParseSumFile(SumFilePath(root))
	if err != nil {
		return err
	}
	pkg := strings.ReplaceAll(localPath, "\\", "/")
	sums.Set(pkg, "noxy_ext.toml", sha256Hex(manifestData))
	wasmData, err := os.ReadFile(filepath.Join(targetDir, wasmName))
	if err != nil {
		return err
	}
	sums.Set(pkg, wasmName, sha256Hex(wasmData))
	return sums.Save(SumFilePath(root))
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
```

(add `crypto/sha256` and `encoding/hex` to `manager.go` imports; the crude line-scan for `wasm =` avoids importing `internal/ext` into `pkgmanager` — acceptable for M1, note it in a comment).

Replace `verifyExtensionSum` in `internal/vm/extensions.go`:

```go
func (vm *VM) verifyExtensionSum(dir string, manifest *ext.Manifest, wasmBytes []byte) error {
	rootAbs, err := filepath.Abs(vm.Config.RootPath)
	if err != nil {
		return nil
	}
	libs := filepath.Join(rootAbs, "noxy_libs")
	dirAbs, err := filepath.Abs(dir)
	if err != nil {
		return nil
	}
	rel, err := filepath.Rel(libs, dirAbs)
	if err != nil || strings.HasPrefix(rel, "..") {
		return nil // fora de noxy_libs (layout de desenvolvimento): sem verificacao
	}
	sums, err := pkgmanager.ParseSumFile(pkgmanager.SumFilePath(rootAbs))
	if err != nil {
		return err
	}
	pkg := filepath.ToSlash(rel)
	want, ok := sums.Lookup(pkg, manifest.Wasm)
	if !ok {
		return nil // sem entrada: TOFU do M1 (spec §15, noxy.sum spec pendente)
	}
	sum := sha256.Sum256(wasmBytes)
	got := hex.EncodeToString(sum[:])
	if got != want {
		return fmt.Errorf("extension artifact mismatch for %s/%s: noxy.sum has sha256:%s, disk has sha256:%s",
			pkg, manifest.Wasm, want, got)
	}
	return nil
}
```

(imports: `crypto/sha256`, `encoding/hex`, `strings`, `noxy-vm/internal/pkgmanager`.)

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/pkgmanager/ ./internal/vm/ -run 'TestSum|TestExtension' -v` → PASS. Full gate: `go test ./internal/... -count=1`.

- [ ] **Step 5: Commit**

```bash
git add internal/pkgmanager/sumfile.go internal/pkgmanager/sumfile_test.go internal/pkgmanager/manager.go internal/vm/extensions.go internal/vm/extensions_e2e_test.go
git commit -m "feat(pkgmanager): noxy.sum mínimo para artefatos de extensão e verificação na carga (issue #78, spec §8)"
```

---

### Task 8: Cross-build check, benchmarks, size measurement

**Files:**
- Create: `internal/ext/bench_test.go`

**Interfaces:**
- Consumes: `Module.Call`, `exttest.BuildGuest`, fixtures fnIndex 0/3.
- Produces: recorded numbers for the spec §11 gates — paste them into the eventual PR description.

- [ ] **Step 1: Write the benchmarks**

```go
package ext

import (
	"context"
	"crypto/sha256"
	"strings"
	"testing"

	"noxy-vm/internal/ext/exttest"
	"noxy-vm/internal/value"
)

func benchModule(b *testing.B) *Module {
	b.Helper()
	manifest, err := ParseManifest([]byte(`
name = "guest"
abi = 1
concurrency = "stateless"

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
`))
	if err != nil {
		b.Fatal(err)
	}
	m, err := LoadModule(context.Background(), exttest.BuildGuest(b, ""), manifest,
		LoaderConfig{PermittedImports: []string{"wasi_snapshot_preview1"}})
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { m.Close(context.Background()) })
	return m
}

// Gate da spec §11: ida-e-volta (bytes 1KB) abaixo de 5 µs no runner amd64.
func BenchmarkExtRoundTrip1KB(b *testing.B) {
	m := benchModule(b)
	payload := value.NewBytes(strings.Repeat("a", 1024))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := m.Call(context.Background(), 3, []value.Value{payload}); err != nil {
			b.Fatal(err)
		}
	}
}

// Gate da spec §11: sha256 de 1 MB no guest dentro de 3x do nativo.
func BenchmarkExtSHA256_1MB(b *testing.B) {
	m := benchModule(b)
	payload := value.NewBytes(strings.Repeat("a", 1<<20))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := m.Call(context.Background(), 3, []value.Value{payload}); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkNativeSHA256_1MB(b *testing.B) {
	payload := []byte(strings.Repeat("a", 1<<20))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sha256.Sum256(payload)
	}
}

// Custo de LoadModule com o cache de compilacao persistente quente
// (revisao do plano: sem cache, todo `noxy script.nx` recompila o wasm —
// para CLI isso pode dominar scripts curtos). Registre no PR tambem o
// tempo FRIO: apague o diretorio de cache (os.UserCacheDir()/noxy/wazero),
// rode uma iteracao, e compare.
func BenchmarkLoadModuleWarm(b *testing.B) {
	wasm := exttest.BuildGuest(b, "")
	manifest := testManifest(b, "single")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m, err := LoadModule(context.Background(), wasm, manifest,
			LoaderConfig{PermittedImports: []string{"wasi_snapshot_preview1"}})
		if err != nil {
			b.Fatal(err)
		}
		m.Close(context.Background())
	}
}
```

For `testManifest(b, ...)` to work from a benchmark, change its signature (Task 4's test file) from `t *testing.T` to `tb testing.TB` — pre-authorized by the controller's scan ruling; `benchModule` may then also reuse it instead of duplicating the manifest literal.

- [ ] **Step 2: Run and record**

Run: `go test ./internal/ext/ -bench . -benchtime=2s -run XXX`
Record all four numbers (round-trip, guest sha256, native sha256, warm load), plus the cold-load time with the wazero cache directory removed. Compare against the §11 gates. Note: `BenchmarkExtSHA256_1MB` includes NXB copies of the 1 MB payload — that is the honest boundary cost and is what the 3× budget covers. If a gate fails on the dev machine, still record — the gates bind on the CI amd64 runner; flag the numbers in the PR for a decision, do not silently proceed.

- [ ] **Step 3: Cross-build check**

```bash
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build ./...
CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build ./...
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build ./...
```

All three must succeed (invariant 2/3 of issue #78).

- [ ] **Step 4: Measure binary size delta**

```bash
git stash list >/dev/null  # garantir arvore limpa
go build -o noxy_new.exe ./cmd/noxy
git worktree add ../noxy-base develop 2>/dev/null || true
(cd ../noxy-base && go build -o noxy_base.exe ./cmd/noxy)
ls -la noxy_new.exe ../noxy-base/noxy_base.exe
```

Record both sizes and the delta. Spec §11: expected ~4–6 MB; **if the delta exceeds 8 MB**, stop and raise it — the spec calls for a `noxy_noext` build-tag escape hatch decision before merging. Clean up: `git worktree remove ../noxy-base`; delete the local `noxy_new.exe`.

- [ ] **Step 5: Commit**

```bash
git add internal/ext/bench_test.go
git commit -m "feat(ext): benchmarks dos gates de custo da spec §11 (issue #78)"
```

---

### Task 9: Documentation

**Files:**
- Create: `docs/EXTENSIONS.md`
- Modify: `CHANGELOG.md` (new entry at top, repo style — check the existing top entry's format first)
- Modify: `docs/PACKAGE_MANAGER.md` (add `noxy.sum` note)

**Interfaces:** none (docs only).

- [ ] **Step 1: Write `docs/EXTENSIONS.md`**

```markdown
# Noxy WASM Extensions (experimental, M1)

Extensions let third parties ship native-performance modules — compression,
hashing, codecs, parsers — as a single platform-independent `.wasm` artifact,
loaded by the VM's embedded WebAssembly runtime (wazero). Design:
`docs/superpowers/specs/2026-08-23-wasm-extension-mechanism-design.md`.

## Package layout

```
my_ext/
├── noxy_ext.toml     # manifest
├── ext.wasm          # compiled extension (wasm32, no WASI)
└── my_ext.nx         # typed Noxy wrapper
```

Install with `noxy --get github.com/you/my_ext` (artifact hashes are recorded
in `noxy.sum`); import with `use github_com.you.my_ext.my_ext as my_ext`.

## Manifest reference

```toml
name = "zstd"            # ^[a-z][a-z0-9_]*$; export prefix
abi = 1                  # only 1 is supported
min_noxy = "0.18.0"      # optional minimum VM version
concurrency = "stateless" # "single" (default) | "stateless"
memory_max_mb = 64        # optional; host ceiling 256
capabilities = []         # M1: must be empty
wasm = "ext.wasm"         # optional artifact name

[[export]]
name = "zstd_compress"    # must start with "<name>_"
params = ["bytes", "int"] # int float bool string bytes any T[] map[K]V Struct
returns = "bytes"         # ... or "void"
stateful = false          # true = mints handles; forbidden under stateless
```

Unknown keys are errors. `stateless` extensions get an instance pool and may
be called concurrently; `single` extensions get one instance and calls are
serialized — required whenever the extension keeps state behind handles,
because a handle only means something to the instance that minted it.

## ABI v1 summary

The guest exports `nx_abi_version() -> u32` (return 1),
`nx_alloc(u32) -> u32`, `nx_free(u32, u32)`, and
`nx_call(fn_index: u32, args_ptr: u32, args_len: u32) -> u64` returning
`(ptr << 32) | len` of the NXB-encoded result, or 0 after calling the host's
`nx_fail(ptr, len)`. Host imports live in module `"noxy:host/v1"`: `nx_fail`
and `nx_log(level, ptr, len)`. Everything crosses **by copy** in NXB
(tag byte + little-endian scalars + u32-length blobs; tags: null 0x00,
bool 0x01, int 0x02, float 0x03, string 0x04, bytes 0x05, array 0x06,
map 0x07, struct 0x08). Functions, channels, tasks and `ref` values do not
cross. Structs arrive back in Noxy as struct-shaped maps.

Target `wasm32-unknown-unknown` (Rust) or equivalent. WASI is **not**
provided: an extension importing anything outside `noxy:host/v1` fails to
load, which is also the permission model — a capability-free extension is a
pure function of its arguments. A complete minimal guest in Rust lives at
`internal/ext/testdata/rustguest/` (allocator, `nx_call` dispatch, `nx_fail`,
NXB bytes result) — copy it as your starting point.

## Errors

`nx_fail` + return 0 → Noxy runtime error `extension 'x' failed: <msg>`
(capturable with `call_result`). A trap (out-of-bounds, unreachable, memory
cap) → `extension 'x' trapped: ...` and the instance is **poisoned**: closed
and, in `single` mode, the whole extension refuses further calls in this
process. State held by a poisoned instance is gone — document that in your
extension's README.

## Granularity (normative)

The unit of a call is a buffer, a document, a batch — never an element. A
boundary crossing costs on the order of a microsecond plus copies; a per-item
call in a hot loop is two orders of magnitude slower than a builtin.
```

- [ ] **Step 2: CHANGELOG entry**

Read the top entry of `CHANGELOG.md` first and mirror its heading format exactly. Content (Portuguese, matching repo style): mecanismo experimental de extensões WASM (issue #78, fase M1) — pacote `internal/ext` (codec NXB, manifesto, loader wazero com gate de imports, modos single/stateless, poisoning), carga via `use` quando o pacote traz `noxy_ext.toml`, exports registradas como natives assinadas, `noxy.sum` mínimo no `--get`, benchmarks; sem capabilities nesta fase.

- [ ] **Step 3: `docs/PACKAGE_MANAGER.md` note**

Append a short section:

```markdown
## Integrity (`noxy.sum`)

When a downloaded package contains a WASM extension (`noxy_ext.toml`),
`noxy --get` records the sha256 of the manifest and of the `.wasm` artifact
in `noxy.sum` next to your `noxy.mod`. At load time the VM refuses an
artifact whose hash does not match its entry. Packages without extensions
are not hashed yet; the full integrity design is tracked separately.
```

- [ ] **Step 4: Full verification**

Run: `go test ./internal/... -count=1` and `go run ./cmd/noxy noxy_examples/run_all_tests_concurrent.nx` → both green.

- [ ] **Step 5: Commit**

```bash
git add docs/EXTENSIONS.md CHANGELOG.md docs/PACKAGE_MANAGER.md
git commit -m "docs: guia de autoria de extensões WASM, noxy.sum no PACKAGE_MANAGER e CHANGELOG (issue #78)"
```

---

### Task 10: Rust reference guest — real import-gate positive path and honest §11 numbers

Added after plan review (items 8 and 9) once Rust became available locally. The Go
wasip1 guest cannot exercise a capability-free load (it needs WASI) and its runtime
distorts the boundary-overhead numbers. A minimal Rust guest on
`wasm32-unknown-unknown` — no WASI, ABI v1 only — fixes both and is the seed of the
Rust SDK. **CI has no Rust:** the compiled `.wasm` is committed next to its Cargo
source; Go tests only consume the binary.

**Files:**
- Create: `internal/ext/testdata/rustguest/Cargo.toml`
- Create: `internal/ext/testdata/rustguest/src/lib.rs`
- Create: `internal/ext/testdata/rustguest/.gitignore` (`target/`)
- Create: `internal/ext/testdata/rustguest/README.md` (rebuild steps)
- Create: `internal/ext/testdata/rustguest/rustguest.wasm` (committed build artifact)
- Create: `internal/ext/rustguest_test.go` (tests + benchmarks)

**Interfaces:**
- Consumes: `LoadModule`, `Module.Call`, `ParseManifest`, `EncodeArgs` (Tasks 1–5).
- Produces: a guest that implements ABI v1 with zero imports outside `noxy:host/v1`, loaded with `LoaderConfig{}` (no permits) — the first genuine capability-free extension the loader has ever run. fn_index dispatch: 0 = echobytes (copy-echo, no compute), 1 = fail ("boom from rust guest"), 2 = trap (`unreachable`), 3 = sha256 of the raw args payload (NXB bytes).

Rust toolchain on this machine: `C:\Users\sandr\.cargo\bin` — prepend it to PATH in the shell (`$env:PATH = "C:\Users\sandr\.cargo\bin;" + $env:PATH` in PowerShell; `export PATH="/c/Users/sandr/.cargo/bin:$PATH"` in bash).

- [ ] **Step 1: Write the Cargo project**

`Cargo.toml`:

```toml
[package]
name = "rustguest"
version = "0.1.0"
edition = "2021"

[lib]
crate-type = ["cdylib"]

[dependencies]
sha2 = { version = "0.10", default-features = false }

[profile.release]
opt-level = 3
lto = true
panic = "abort"
codegen-units = 1
```

`src/lib.rs`:

```rust
//! Guest de referencia do ABI v1 do Noxy (spec §2): sem WASI, sem imports
//! fora de `noxy:host/v1`. Fixture do gate positivo de imports e dos
//! benchmarks da §11. fn_index: 0 echobytes, 1 fail, 2 trap, 3 sha256.

use sha2::{Digest, Sha256};
use std::alloc::{alloc, dealloc, Layout};

#[link(wasm_import_module = "noxy:host/v1")]
extern "C" {
    fn nx_fail(ptr: u32, len: u32);
    #[allow(dead_code)]
    fn nx_log(level: u32, ptr: u32, len: u32);
}

#[no_mangle]
pub extern "C" fn nx_abi_version() -> u32 {
    1
}

#[no_mangle]
pub extern "C" fn nx_alloc(size: u32) -> u32 {
    if size == 0 {
        return 0;
    }
    let layout = Layout::from_size_align(size as usize, 1).unwrap();
    unsafe { alloc(layout) as u32 }
}

#[no_mangle]
pub extern "C" fn nx_free(ptr: u32, size: u32) {
    if ptr == 0 || size == 0 {
        return;
    }
    let layout = Layout::from_size_align(size as usize, 1).unwrap();
    unsafe { dealloc(ptr as *mut u8, layout) }
}

/// Devolve `data` numa regiao nova: (ptr << 32) | len. Payload vazio ainda
/// aloca 1 byte — 0 e o sentinela de falha do ABI.
fn ret_raw(data: &[u8]) -> u64 {
    if data.is_empty() {
        let ptr = nx_alloc(1);
        return (ptr as u64) << 32;
    }
    let ptr = nx_alloc(data.len() as u32);
    let out = unsafe { core::slice::from_raw_parts_mut(ptr as *mut u8, data.len()) };
    out.copy_from_slice(data);
    ((ptr as u64) << 32) | (data.len() as u64)
}

/// NXB bytes: tag 0x05 + u32 LE len + payload.
fn ret_nxb_bytes(payload: &[u8]) -> u64 {
    let mut out = Vec::with_capacity(5 + payload.len());
    out.push(0x05);
    out.extend_from_slice(&(payload.len() as u32).to_le_bytes());
    out.extend_from_slice(payload);
    ret_raw(&out)
}

fn fail(msg: &str) -> u64 {
    unsafe { nx_fail(msg.as_ptr() as u32, msg.len() as u32) }
    0
}

#[no_mangle]
pub extern "C" fn nx_call(fn_index: u32, args_ptr: u32, args_len: u32) -> u64 {
    let args: &[u8] = if args_len == 0 {
        &[]
    } else {
        unsafe { core::slice::from_raw_parts(args_ptr as *const u8, args_len as usize) }
    };
    match fn_index {
        // echobytes: args = u32 count + um valor NXB bytes; devolve o valor
        // tal qual (ja e tag+len+payload) — cópia pura, sem compute.
        0 => {
            if args.len() < 4 {
                return fail("echobytes expects one bytes argument");
            }
            ret_raw(&args[4..])
        }
        1 => fail("boom from rust guest"),
        2 => core::arch::wasm32::unreachable(),
        3 => ret_nxb_bytes(&Sha256::digest(args)),
        _ => fail("unknown fn_index"),
    }
}
```

`.gitignore`: `target/`

`README.md`:

```markdown
# rustguest — ABI v1 reference guest

Minimal Noxy extension guest in Rust: no WASI, only `noxy:host/v1` imports.
Used by `internal/ext/rustguest_test.go` as the capability-free load fixture
and for the spec §11 benchmarks. **The compiled `rustguest.wasm` is committed**
because CI has no Rust toolchain; rebuild it after editing `src/lib.rs`:

    rustup target add wasm32-unknown-unknown
    cargo build --release --target wasm32-unknown-unknown
    cp target/wasm32-unknown-unknown/release/rustguest.wasm rustguest.wasm
```

- [ ] **Step 2: Build it**

From `internal/ext/testdata/rustguest/`, with `C:\Users\sandr\.cargo\bin` on PATH:
`rustup target add wasm32-unknown-unknown` then
`cargo build --release --target wasm32-unknown-unknown`, then copy
`target/wasm32-unknown-unknown/release/rustguest.wasm` to `rustguest.wasm`.
Record the artifact size in the report (expect tens of KB). Commit `Cargo.lock` too.

- [ ] **Step 3: Write the failing tests and benchmarks**

`internal/ext/rustguest_test.go`:

```go
package ext

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"strings"
	"testing"

	"noxy-vm/internal/value"
)

//go:embed testdata/rustguest/rustguest.wasm
var rustGuestWasm []byte

func rustManifest(tb testing.TB) *Manifest {
	tb.Helper()
	m, err := ParseManifest([]byte(`
name = "rust"
abi = 1
concurrency = "single"

[[export]]
name = "rust_echobytes"
params = ["bytes"]
returns = "bytes"

[[export]]
name = "rust_fail"
params = []
returns = "any"

[[export]]
name = "rust_trap"
params = []
returns = "any"

[[export]]
name = "rust_sha256"
params = ["bytes"]
returns = "bytes"
`))
	if err != nil {
		tb.Fatalf("manifest: %v", err)
	}
	return m
}

// Gate positivo REAL: guest sem WASI carrega com LoaderConfig{} — nenhum
// import fora de noxy:host/v1.
func loadRustGuest(tb testing.TB) *Module {
	tb.Helper()
	m, err := LoadModule(context.Background(), rustGuestWasm, rustManifest(tb), LoaderConfig{})
	if err != nil {
		tb.Fatalf("load rust guest without permits: %v", err)
	}
	tb.Cleanup(func() { m.Close(context.Background()) })
	return m
}

func TestRustGuestLoadsWithoutPermits(t *testing.T) {
	loadRustGuest(t)
}

func TestRustGuestEchoBytes(t *testing.T) {
	m := loadRustGuest(t)
	got, err := m.Call(context.Background(), 0, []value.Value{value.NewBytes("héllo")})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if got.Type != value.VAL_BYTES || got.Obj.(string) != "héllo" {
		t.Fatalf("echo: %#v", got)
	}
}

func TestRustGuestFailAndTrap(t *testing.T) {
	m := loadRustGuest(t)
	_, err := m.Call(context.Background(), 1, nil)
	if err == nil || !strings.Contains(err.Error(), "extension 'rust' failed: boom from rust guest") {
		t.Fatalf("fail: %v", err)
	}
	_, err = m.Call(context.Background(), 2, nil)
	if err == nil || !strings.Contains(err.Error(), "extension 'rust' trapped") {
		t.Fatalf("trap: %v", err)
	}
}

func TestRustGuestSha256MatchesNative(t *testing.T) {
	m := loadRustGuest(t)
	arg := value.NewBytes("abc")
	got, err := m.Call(context.Background(), 3, []value.Value{arg})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	// O guest faz sha256 do payload cru de args (mesma convencao do guest Go).
	raw, _ := EncodeArgs([]value.Value{arg}, DefaultLimits())
	want := sha256.Sum256(raw)
	if got.Type != value.VAL_BYTES || got.Obj.(string) != string(want[:]) {
		t.Fatalf("sha256 mismatch")
	}
}

// Numeros honestos da spec §11 para um guest de qualidade nativa (compare
// com BenchmarkExt* do guest Go).
func BenchmarkRustRoundTrip1KB(b *testing.B) {
	m := loadRustGuest(b)
	payload := value.NewBytes(strings.Repeat("a", 1024))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := m.Call(context.Background(), 0, []value.Value{payload}); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkRustSHA256_1MB(b *testing.B) {
	m := loadRustGuest(b)
	payload := value.NewBytes(strings.Repeat("a", 1<<20))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := m.Call(context.Background(), 3, []value.Value{payload}); err != nil {
			b.Fatal(err)
		}
	}
}
```

- [ ] **Step 4: Run the tests and benchmarks**

Run: `go test ./internal/ext/ -run TestRustGuest -v` → PASS (the load test fails with an "ungranted module" error if the Rust build accidentally pulled WASI — that's the point of the test).
Run: `go test ./internal/ext/ -bench 'Rust|Ext' -benchtime=2s -run XXX` and record Rust vs Go-guest numbers side by side for round-trip and sha256, plus the native sha256 baseline. Compare against the §11 gates.

- [ ] **Step 5: Full gate and commit**

Run: `go test ./internal/... -count=1`. Then:

```bash
git add internal/ext/testdata/rustguest internal/ext/rustguest_test.go
git commit -m "feat(ext): guest de referência em Rust (sem WASI) — gate positivo real e números honestos da §11 (issue #78)"
```

---

## Out of scope for this plan (tracked in spec §14–§15)

- Rust and TinyGo guest SDKs and the published zstd reference extension — external repositories; they consume the ABI this plan freezes.
- Capability modules (`noxy:cap/*`), `grant` lines in `noxy.mod` — M2.
- Full `noxy.sum` spec (sources, TOFU policy, per-platform assets) — separate spec.
- Guest→host callbacks, streaming convention, compile caching, REPL poisoning semantics — spec §15 follow-ups.
```

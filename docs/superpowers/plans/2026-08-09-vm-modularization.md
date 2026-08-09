# VM Modularization Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Raise `internal/vm` statement coverage from 37.7% to at least 60.0%, then split `vm.go` into focused files without changing observable behavior.

**Architecture:** Keep every extracted file in `internal/vm` and `package vm`. Establish and commit a test-only characterization safety net first; then use source-layout tests as red-green gates for mechanical extraction of builtins, stack/calls, modules, and the executor.

**Tech Stack:** Go 1.24, Go `testing`, Go coverage tooling, Noxy lexer/parser/compiler/bytecode VM, PowerShell verification commands.

## Global Constraints

- Work only in branch `refactor/modularize-vm` and its isolated worktree.
- No production extraction starts before `internal/vm` coverage reaches 60.0%.
- Do not change public APIs, AST, compiler contracts, opcodes, bytecode, error strings, line reporting, or language semantics.
- Preserve native names, signatures, final implementations, the effective final registry, and the duplicate `strings_contains` and `strings_replace` overwrite order. Unique cross-domain registrations may be grouped.
- Keep all new production files in `internal/vm` with `package vm`; create no subpackages.
- Keep `run` as one switch-based loop and move it only after every other extraction.
- Use hermetic test resources: `t.TempDir()`, loopback ports assigned by the OS, bounded waits, and no internet access.
- Keep statement coverage at or above 60.0% after the safety-net gate and every extraction.

---

## Planned File Ownership

| File | Final ownership |
| --- | --- |
| `internal/vm/vm.go` | VM state, constructors, `runtimeError`, public native/global/module registration APIs |
| `internal/vm/builtins.go` | Ordered calls to domain registration methods |
| `internal/vm/builtins_core.go` | Print, conversions, formatting, and base encodings |
| `internal/vm/builtins_collections.go` | Arrays, maps, bytes conversion, and collection mutation/query helpers |
| `internal/vm/builtins_concurrency.go` | Spawn, channels, and wait groups |
| `internal/vm/builtins_time.go` | Time and calendar functions |
| `internal/vm/builtins_strings.go` | String functions and duplicate effective registrations |
| `internal/vm/builtins_io.go` | Input and file functions |
| `internal/vm/builtins_sys.go` | OS, environment, process, and plugin functions |
| `internal/vm/builtins_crypto.go` | Random, PBKDF2, and AES-GCM functions |
| `internal/vm/builtins_net.go` | Network listener, connection, IO, close, blocking, and select functions |
| `internal/vm/builtins_sqlite.go` | SQLite handles, statements, binds, execution, query, and cleanup |
| `internal/vm/builtins_json.go` | JSON natives and Noxy/Go conversion/population bridges currently in `vm.go` |
| `internal/vm/stack.go` | Stack helpers, constant readers, equality/truth helpers, and upvalues |
| `internal/vm/calls.go` | `callValue`, `call`, and `copyValue` |
| `internal/vm/modules.go` | `loadModule` and module compilation/execution/cache flow |
| `internal/vm/executor.go` | `Interpret`, `InterpretWithGlobals`, and `run` |

---

### Task 1: Consolidate VM Test Harnesses

**Files:**
- Create: `internal/vm/vm_test_helpers_test.go`
- Modify: `internal/vm/vm_test.go`
- Modify: `internal/vm/function_types_test.go`

**Interfaces:**
- Produces: `compileVMSource(t *testing.T, source string) *chunk.Chunk`
- Produces: `interpretVMSource(t *testing.T, machine *VM, source string) error`
- Produces: `captureVMSource(t *testing.T, source string) value.Value`
- Produces: `requireBuiltin(t *testing.T, machine *VM, name string) *value.ObjNative`
- Produces: `callBuiltin(t *testing.T, machine *VM, name string, args ...value.Value) value.Value`

- [ ] **Step 1: Record the clean baseline**

Run:

```powershell
$env:GOCACHE = Join-Path $env:TEMP 'noxy-vm-refactor-go-cache'
go test ./internal/vm
```

Expected: PASS with zero failures.

- [ ] **Step 2: Add the shared test harness**

Create `internal/vm/vm_test_helpers_test.go` with these helpers:

```go
package vm

import (
	"noxy-vm/internal/chunk"
	"noxy-vm/internal/compiler"
	"noxy-vm/internal/lexer"
	"noxy-vm/internal/parser"
	"noxy-vm/internal/value"
	"testing"
)

func compileVMSource(t *testing.T, source string) *chunk.Chunk {
	t.Helper()
	l := lexer.New(source)
	p := parser.New(l)
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}
	code, _, err := compiler.New().Compile(program)
	if err != nil {
		t.Fatalf("compiler error: %v", err)
	}
	return code
}

func interpretVMSource(t *testing.T, machine *VM, source string) error {
	t.Helper()
	return machine.Interpret(compileVMSource(t, source))
}

func captureVMSource(t *testing.T, source string) value.Value {
	t.Helper()
	machine := New()
	captured := value.NewNull()
	machine.DefineNative("test_report", func(args []value.Value) value.Value {
		if len(args) != 0 {
			captured = args[0]
		}
		return value.NewNull()
	})
	if err := interpretVMSource(t, machine, source); err != nil {
		t.Fatalf("vm error: %v", err)
	}
	return captured
}

func requireBuiltin(t *testing.T, machine *VM, name string) *value.ObjNative {
	t.Helper()
	actual, ok := machine.GetGlobal(name)
	if !ok {
		t.Fatalf("builtin %q is not registered", name)
	}
	native, ok := actual.Obj.(*value.ObjNative)
	if actual.Type != value.VAL_NATIVE || !ok || native == nil {
		t.Fatalf("global %q is not a native: %#v", name, actual)
	}
	return native
}

func callBuiltin(t *testing.T, machine *VM, name string, args ...value.Value) value.Value {
	t.Helper()
	return requireBuiltin(t, machine, name).Fn(args)
}
```

- [ ] **Step 3: Replace duplicated compile/interpret setup**

Change `runTypedFunctionProgram` and `runTypedFunctionProgramError` to call
`compileVMSource`/`interpretVMSource`. Change `runVmTests` to call
`captureVMSource(t, "test_report("+tt.input+")")`. Do not change assertions or
test cases.

- [ ] **Step 4: Verify harness behavior**

Run:

```powershell
gofmt -w internal/vm/vm_test_helpers_test.go internal/vm/vm_test.go internal/vm/function_types_test.go
go test ./internal/vm
```

Expected: PASS with the same test behavior as the baseline.

- [ ] **Step 5: Commit the test harness**

```powershell
git add internal/vm/vm_test_helpers_test.go internal/vm/vm_test.go internal/vm/function_types_test.go
git commit -m "test(vm): consolidate execution harness"
```

---

### Task 2: Characterize Executor and Stack Semantics

**Files:**
- Create: `internal/vm/executor_characterization_test.go`
- Create: `internal/vm/stack_characterization_test.go`

**Interfaces:**
- Consumes: `captureVMSource`, `compileVMSource`
- Covers: generic and specialized arithmetic, jumps/loops, globals/locals, properties/indexes, arrays/maps, stack primitives, constant readers, equality, falsey values, and upvalues

- [ ] **Step 1: Add compiler-driven executor tests**

Create table-driven tests that call `captureVMSource` for these complete source
cases and assert the reported value type and payload:

```go
func TestExecutorCharacterization(t *testing.T) {
	tests := []struct {
		name   string
		source string
		want   value.Value
	}{
		{"integer modulo", `test_report(17 % 5)`, value.NewInt(2)},
		{"float arithmetic", `test_report((2.5 + 1.5) * 2.0)`, value.NewFloat(8)},
		{"bitwise", `test_report((6 & 3) | (1 << 3))`, value.NewInt(10)},
		{"short circuit true", `let called: bool = false
func side_effect() -> bool
    called = true
    return false
end
let result: bool = true || side_effect()
test_report(result && !called)`, value.NewBool(true)},
		{"short circuit false", `let called: bool = false
func side_effect() -> bool
    called = true
    return true
end
let result: bool = false && side_effect()
test_report(!result && !called)`, value.NewBool(true)},
		{"string concatenate", `test_report("no" + "xy")`, value.NewString("noxy")},
		{"bytes equality", `test_report(b"abc" == b"abc")`, value.NewBool(true)},
		{"mixed numeric equality", `test_report(42 == 42.0)`, value.NewBool(true)},
		{"loop and locals", `let total: int = 0
let i: int = 0
while i < 5 do
    total = total + i
    i = i + 1
end
test_report(total)`, value.NewInt(10)},
		{"array index update", `let values: int[] = [1, 2]
values[1] = 42
test_report(values[1])`, value.NewInt(42)},
		{"map index update", `let values: map[string, int] = {"x": 1}
values["x"] = 42
test_report(values["x"])`, value.NewInt(42)},
		{"struct property update", `struct Box
    value: int
end
let box: Box = Box(1)
box.value = 42
test_report(box.value)`, value.NewInt(42)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := captureVMSource(t, tt.source)
			if !valuesEqual(got, tt.want) {
				t.Fatalf("got %s (%v), want %s (%v)", got.String(), got.Type, tt.want.String(), tt.want.Type)
			}
		})
	}
}
```

- [ ] **Step 2: Add direct stack and helper characterization**

Add tests with these assertions:

```go
func TestLegacyConstantReaders(t *testing.T) {
	machine := New()
	machine.chunk = chunk.New()
	machine.chunk.Constants = []value.Value{value.NewInt(7), value.NewInt(42)}
	machine.chunk.Code = []byte{0, 1, 0, 1}
	machine.ip = 0
	if got := machine.readConstant(); got.AsInt != 7 {
		t.Fatalf("readConstant=%v", got)
	}
	machine.ip = 2
	if got := machine.readShort(); got != 1 {
		t.Fatalf("readShort=%d, want 1", got)
	}
}

func TestFalseyAndEqualityMatrix(t *testing.T) {
	if !isFalsey(value.NewNull()) || !isFalsey(value.NewBool(false)) || isFalsey(value.NewBool(true)) {
		t.Fatal("falsey semantics changed")
	}
	pairs := []struct {
		left, right value.Value
		want        bool
	}{
		{value.NewNull(), value.NewNull(), true},
		{value.NewInt(42), value.NewFloat(42), true},
		{value.NewFloat(42), value.NewInt(42), true},
		{value.NewBytes("x"), value.NewBytes("x"), true},
		{value.NewString("x"), value.NewString("x"), true},
		{value.NewInt(1), value.NewBool(true), false},
	}
	for _, pair := range pairs {
		if got := valuesEqual(pair.left, pair.right); got != pair.want {
			t.Fatalf("valuesEqual(%v, %v)=%v, want %v", pair.left, pair.right, got, pair.want)
		}
	}
}
```

Add `TestStackLIFOAndSlotClearing`, verifying `push`, `peek`, `pop`,
`stackTop`, and that the popped slot is cleared. Add
`TestUpvalueCaptureAndClose`, which captures the same slot twice, asserts
pointer identity, closes it, mutates the original stack slot, and confirms the
closed value remains unchanged.

- [ ] **Step 3: Verify executor characterization**

```powershell
gofmt -w internal/vm/executor_characterization_test.go internal/vm/stack_characterization_test.go
go test ./internal/vm -run 'TestExecutorCharacterization|TestLegacyConstantReaders|TestFalseyAndEqualityMatrix|TestStack|TestUpvalue' -v
go test ./internal/vm
```

Expected: every characterization test and the complete VM package pass.

- [ ] **Step 4: Commit executor and stack tests**

```powershell
git add internal/vm/executor_characterization_test.go internal/vm/stack_characterization_test.go
git commit -m "test(vm): characterize executor and stack"
```

---

### Task 3: Characterize Calls, References, and Modules

**Files:**
- Create: `internal/vm/calls_characterization_test.go`
- Modify: `internal/vm/references_test.go`
- Modify: `internal/vm/module_exports_test.go`

**Interfaces:**
- Consumes: `captureVMSource`, `interpretVMSource`
- Covers: dynamic/script/native/constructor calls, arity failures, shallow copy, global reference storage, module caching, and missing modules

- [ ] **Step 1: Add call-boundary tests**

Add compiler-driven tests for:

```noxy
func change(values: int[]) -> int
    append(values, 9)
    return length(values)
end
let original: int[] = [1]
let local_length: int = change(original)
test_report(length(original) * 10 + local_length)
```

Expected result: `12`, proving the callee receives a shallow top-level copy.

Add a nested-composite case proving inner values remain shared, an exact
closure call returning `42`, a bare-function dynamic call returning `42`, and
a struct constructor call. Add malformed native and constructor calls that
assert the current error text and that `machine.stackTop` is unchanged after
validation failure.

- [ ] **Step 2: Cover direct global reference storage**

Append tests to `references_test.go` that create a `value.ObjRef` with
`REF_GLOBAL`, `Name: "answer"`, and `GlobalOwner: &globals`. Verify
`lookupGlobalReferenceValue` returns `41`; verify `storeGlobalReferenceValue`
stores `42`; verify nil owner and missing key return the current errors.

- [ ] **Step 3: Cover module cache identity and missing-module errors**

Append tests that:

1. write `answer.nx` under `t.TempDir()`;
2. load it twice with one `VM`;
3. assert the second returned object shares the cached module object;
4. load an absent module and assert the current error contains its name;
5. verify a child VM constructed with shared state observes the same cache.

- [ ] **Step 4: Verify and commit boundary tests**

```powershell
gofmt -w internal/vm/calls_characterization_test.go internal/vm/references_test.go internal/vm/module_exports_test.go
go test ./internal/vm
git add internal/vm/calls_characterization_test.go internal/vm/references_test.go internal/vm/module_exports_test.go
git commit -m "test(vm): characterize calls references and modules"
```

Expected: PASS; the commit changes test files only.

---

### Task 4: Characterize Pure Builtins

**Files:**
- Create: `internal/vm/builtins_registry_test.go`
- Create: `internal/vm/builtins_core_test.go`
- Create: `internal/vm/builtins_strings_test.go`
- Create: `internal/vm/builtins_time_test.go`
- Create: `internal/vm/builtins_collections_test.go`
- Create: `internal/vm/builtins_json_test.go`

**Interfaces:**
- Consumes: `requireBuiltin`, `callBuiltin`
- Covers: final builtin registry, conversions, base encodings, formatting, string operations, deterministic time/calendar operations, collection operations, and JSON conversion

- [ ] **Step 1: Snapshot the effective builtin registry**

Compare `vm.shared.Globals` entries whose type is `VAL_NATIVE` with this exact
sorted unique list:

```go
expected := []string{
	"append", "base62_decode", "base62_encode", "base64_decode",
	"base64_encode", "chan_close", "chan_is_closed", "chan_recv",
	"chan_send", "contains", "crypto_aes256_gcm_decrypt",
	"crypto_aes256_gcm_encrypt", "crypto_pbkdf2_sha256",
	"crypto_random_bytes", "delete", "fmt", "has_key", "hex",
	"hex_decode", "hex_encode", "input", "io_close", "io_exists",
	"io_mkdir", "io_open", "io_read", "io_read_bytes", "io_read_lines",
	"io_remove", "io_stat", "io_write", "iprint", "json_dumps",
	"json_loads", "json_parse", "keys", "length", "make_chan",
	"make_wg", "net_accept", "net_close", "net_connect", "net_listen",
	"net_recv", "net_select", "net_send", "net_setblocking", "ord",
	"pop", "print", "slice", "spawn", "sqlite_bind_float",
	"sqlite_bind_int", "sqlite_bind_text", "sqlite_close", "sqlite_exec",
	"sqlite_exec_params", "sqlite_finalize", "sqlite_open",
	"sqlite_prepare", "sqlite_query", "sqlite_reset", "sqlite_step_exec",
	"strings_char_at", "strings_contains", "strings_count",
	"strings_ends_with", "strings_from_char_code", "strings_index_of",
	"strings_is_alnum", "strings_is_alpha", "strings_is_digit",
	"strings_is_empty", "strings_is_space", "strings_join_count",
	"strings_pad_left", "strings_repeat", "strings_replace",
	"strings_replace_first", "strings_reverse", "strings_split",
	"strings_starts_with", "strings_substring", "strings_to_lower",
	"strings_to_upper", "strings_trim", "sys_argv", "sys_exec",
	"sys_exec_output", "sys_exit", "sys_getcwd", "sys_getenv",
	"sys_load_plugin", "sys_os", "sys_setenv", "sys_sleep",
	"time_add_days", "time_add_seconds", "time_after", "time_before",
	"time_days_in_month", "time_diff", "time_diff_duration", "time_format",
	"time_format_custom", "time_format_date", "time_format_time",
	"time_from_timestamp", "time_is_leap_year", "time_make_datetime",
	"time_month_name", "time_now", "time_now_datetime", "time_now_ms",
	"time_parse", "time_parse_date", "time_sleep", "time_to_timestamp",
	"time_weekday_name", "to_bytes", "to_float", "to_int", "to_str",
	"wg_add", "wg_done", "wg_wait",
}
```

Separately assert these exact native signatures:

- `delete`: arity 2; parameters `ref map`, `any`; return `void`;
- `append`: arity 2; first parameter `ref array`; return `void`;
- `pop`: arity 1; parameter `ref array`; return `any`;
- `json_loads`: arity 2; parameters `string`, `ref any`; return `bool`.

- [ ] **Step 2: Add core builtin tables**

Use `callBuiltin` to assert both success and invalid-argument sentinel behavior
for:

- `to_str`, `to_int`, and `to_float` with null, bool, int, float, and string;
- `hex`, `hex_encode`, `hex_decode`;
- `base64_encode`, `base64_decode`;
- `base62_encode`, `base62_decode`;
- `fmt` with `%s`, `%d`, `%f`, `%T`, escaped percent, and insufficient args.

Each table must assert `Value.Type` and payload, not only `String()`.

- [ ] **Step 3: Add string and deterministic time tables**

Exercise every registered `strings_` native and `ord` with normal, empty, and
short-argument cases. Assert Unicode-sensitive functions using the behavior
currently produced, without redefining expected Unicode semantics.

Exercise deterministic time functions: `time_make_datetime`,
`time_to_timestamp`, `time_from_timestamp`, `time_diff`, `time_add_days`,
`time_before`, `time_after`, `time_is_leap_year`, `time_days_in_month`,
`time_weekday_name`, `time_month_name`, parsing, and formatting. Compare stable
fields and formatted strings; do not assert the wall-clock value of `time_now`.

- [ ] **Step 4: Add collection and JSON tables**

Exercise `length`, `keys`, `delete`, `append`, `pop`, `slice`, `contains`,
`has_key`, and `to_bytes` with arrays, maps, strings, bytes, empty collections,
invalid argument types, and boundary indexes. Verify mutations through the
actual referenced target.

Exercise `json_dumps`, `json_parse`, `jsonValToGo`, `goValToNoxy`, and scalar,
array, map, and struct cases for `jsonReplacementCompatible`. Reuse the
existing typed `json_loads` tests instead of duplicating them.

- [ ] **Step 5: Verify and commit pure builtin coverage**

```powershell
gofmt -w internal/vm/builtins_registry_test.go internal/vm/builtins_core_test.go internal/vm/builtins_strings_test.go internal/vm/builtins_time_test.go internal/vm/builtins_collections_test.go internal/vm/builtins_json_test.go
go test ./internal/vm
git add internal/vm/builtins_registry_test.go internal/vm/builtins_core_test.go internal/vm/builtins_strings_test.go internal/vm/builtins_time_test.go internal/vm/builtins_collections_test.go internal/vm/builtins_json_test.go
git commit -m "test(vm): characterize pure builtins"
```

Expected: PASS; no production file changes.

---

### Task 5: Characterize Stateful Builtins

**Files:**
- Create: `internal/vm/builtins_concurrency_test.go`
- Create: `internal/vm/builtins_io_test.go`
- Create: `internal/vm/builtins_sys_test.go`
- Create: `internal/vm/builtins_crypto_test.go`
- Create: `internal/vm/builtins_net_test.go`
- Create: `internal/vm/builtins_sqlite_test.go`

**Interfaces:**
- Consumes: `callBuiltin`, `interpretVMSource`
- Covers: channels/wait groups/spawn, filesystem handles, safe OS/environment operations, crypto round trips, loopback networking, and temporary SQLite

- [ ] **Step 1: Characterize concurrency primitives**

Test buffered and unbuffered `make_chan`, `chan_send`, `chan_recv`,
`chan_close`, and `chan_is_closed` with bounded goroutines. Test `make_wg`,
`wg_add`, `wg_done`, and `wg_wait` with a completion channel and a two-second
timeout. Run a Noxy `spawn` program whose worker sends `42` through a channel;
assert receipt instead of sleeping.

- [ ] **Step 2: Characterize file and safe system primitives**

Under `t.TempDir()`, exercise `io_mkdir`, `io_open`, `io_write`, `io_close`,
`io_read`, `io_read_bytes`, `io_read_lines`, `io_stat`, `io_exists`, and
`io_remove`. Assert handle invalidation and the current sentinels for unknown
file descriptors.

Exercise `sys_os`, `sys_getcwd`, `sys_getenv`, `sys_setenv`, `sys_argv`, and
zero-duration sleep. Use `t.Setenv` for isolation. For `sys_exec` and
`sys_exec_output`, cover only invalid/empty argument behavior so the test does
not depend on shell programs. Assert `sys_load_plugin` invalid-path behavior.

- [ ] **Step 3: Characterize crypto round trips**

Assert:

- `crypto_random_bytes(32)` returns 32 bytes and invalid lengths return the
  current sentinel;
- PBKDF2 returns the requested output length for fixed password/salt/rounds;
- AES-256-GCM encrypt followed by decrypt returns the original bytes;
- invalid key, nonce, and ciphertext shapes preserve current failure values.

- [ ] **Step 4: Characterize loopback networking**

Bind a TCP listener to `127.0.0.1` with an OS-assigned port. Use the Noxy
network natives to connect, accept, send, receive, select, set blocking mode,
and close both handles. All goroutines select on a two-second timeout. Assert
the shared-state handle maps are empty after close.

- [ ] **Step 5: Characterize SQLite lifecycle**

Use a database path under `t.TempDir()` and exercise `sqlite_open`,
`sqlite_exec`, `sqlite_exec_params`, `sqlite_prepare`, all three bind natives,
`sqlite_step_exec`, `sqlite_reset`, `sqlite_query`, `sqlite_finalize`, and
`sqlite_close`. Create one table, insert deterministic rows, and assert the
returned Noxy row structure. Verify invalid handles return current sentinels.

- [ ] **Step 6: Verify and commit stateful builtin coverage**

```powershell
gofmt -w internal/vm/builtins_concurrency_test.go internal/vm/builtins_io_test.go internal/vm/builtins_sys_test.go internal/vm/builtins_crypto_test.go internal/vm/builtins_net_test.go internal/vm/builtins_sqlite_test.go
go test ./internal/vm -count=1
git add internal/vm/builtins_concurrency_test.go internal/vm/builtins_io_test.go internal/vm/builtins_sys_test.go internal/vm/builtins_crypto_test.go internal/vm/builtins_net_test.go internal/vm/builtins_sqlite_test.go
git commit -m "test(vm): characterize stateful builtins"
```

Expected: PASS without internet, user files, fixed ports, or timing sleeps.

---

### Task 6: Enforce the 60 Percent Safety Gate

**Files:**
- Modify only test files from Tasks 2–5 if the named coverage areas below remain below the required threshold

**Interfaces:**
- Produces: a test-only history with `internal/vm` statement coverage at or above 60.0%

- [ ] **Step 1: Measure uncached coverage**

```powershell
$profile = Join-Path $env:TEMP 'noxy-vm-safety-net.cover'
go test -count=1 -coverprofile $profile ./internal/vm
go tool cover -func=$profile
```

Expected: total statement coverage is at least 60.0%.

- [ ] **Step 2: Close any remaining named gaps before extraction**

If the first measurement is below 60.0%, add cases only for these already
approved boundaries until it passes: untested opcode arms in `run`, invalid
arity/type arms in builtin closures, IO cleanup, network cleanup, SQLite
cleanup, JSON conversion variants, and global-reference lookup/store. Do not
modify production files to raise the percentage.

- [ ] **Step 3: Verify the safety-net commit boundary**

```powershell
git diff HEAD -- internal/vm ':!internal/vm/*_test.go'
git status --short
```

Expected: no uncommitted production changes; all commits after the design are
test-only. If Step 2 added tests, commit them:

```powershell
git add internal/vm/*_test.go
git commit -m "test(vm): complete modularization safety net"
```

- [ ] **Step 4: Run package and repository baselines**

```powershell
go test ./internal/vm
go test ./internal/...
go test ./...
```

Expected: all commands pass. Production extraction is now permitted.

---

### Task 7: Extract Builtins by Domain

**Files:**
- Create: `internal/vm/architecture_test.go`
- Create: `internal/vm/builtins.go`
- Create: `internal/vm/builtins_core.go`
- Create: `internal/vm/builtins_collections.go`
- Create: `internal/vm/builtins_concurrency.go`
- Create: `internal/vm/builtins_time.go`
- Create: `internal/vm/builtins_strings.go`
- Create: `internal/vm/builtins_io.go`
- Create: `internal/vm/builtins_sys.go`
- Create: `internal/vm/builtins_crypto.go`
- Create: `internal/vm/builtins_net.go`
- Create: `internal/vm/builtins_sqlite.go`
- Create: `internal/vm/builtins_json.go`
- Modify: `internal/vm/vm.go`

**Interfaces:**
- Produces: private methods `defineBuiltins`, `defineCoreBuiltins`, `defineCollectionBuiltins`, `defineConcurrencyBuiltins`, `defineTimeBuiltins`, `defineStringBuiltins`, `defineIOBuiltins`, `defineSystemBuiltins`, `defineCryptoBuiltins`, `defineNetworkBuiltins`, `defineSQLiteBuiltins`, and `defineJSONBuiltins`
- Preserves: `NewWithShared(shared *SharedState, cfg VMConfig) *VM`

- [ ] **Step 1: Add a source-layout assertion helper and failing builtin test**

Create `architecture_test.go` using `go/parser` and `go/token`:

```go
package vm

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

func requireSourceFunctions(t *testing.T, filename string, names ...string) {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), filename, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", filename, err)
	}
	found := make(map[string]bool)
	for _, declaration := range file.Decls {
		if function, ok := declaration.(*ast.FuncDecl); ok {
			found[function.Name.Name] = true
		}
	}
	for _, name := range names {
		if !found[name] {
			t.Errorf("%s does not declare %s", filename, name)
		}
	}
}

func TestBuiltinSourceLayout(t *testing.T) {
	expected := map[string][]string{
		"builtins.go":             {"defineBuiltins"},
		"builtins_core.go":        {"defineCoreBuiltins"},
		"builtins_collections.go": {"defineCollectionBuiltins"},
		"builtins_concurrency.go": {"defineConcurrencyBuiltins"},
		"builtins_time.go":        {"defineTimeBuiltins"},
		"builtins_strings.go":     {"defineStringBuiltins"},
		"builtins_io.go":          {"defineIOBuiltins"},
		"builtins_sys.go":         {"defineSystemBuiltins"},
		"builtins_crypto.go":      {"defineCryptoBuiltins"},
		"builtins_net.go":         {"defineNetworkBuiltins"},
		"builtins_sqlite.go":      {"defineSQLiteBuiltins"},
		"builtins_json.go":        {"defineJSONBuiltins"},
	}
	for filename, names := range expected {
		requireSourceFunctions(t, filename, names...)
	}
}
```

- [ ] **Step 2: Run the structural test to verify RED**

```powershell
go test ./internal/vm -run TestBuiltinSourceLayout -v
```

Expected: FAIL because `builtins.go` does not exist.

- [ ] **Step 3: Replace the constructor registration block with orchestration**

Reduce `NewWithShared` to state initialization followed by:

```go
vm.defineBuiltins()
return vm
```

Create `builtins.go`:

```go
package vm

func (vm *VM) defineBuiltins() {
	vm.defineCoreBuiltins()
	vm.defineConcurrencyBuiltins()
	vm.defineTimeBuiltins()
	vm.defineIOBuiltins()
	vm.defineStringBuiltins()
	vm.defineCryptoBuiltins()
	vm.defineSystemBuiltins()
	vm.defineCollectionBuiltins()
	vm.defineNetworkBuiltins()
	vm.defineSQLiteBuiltins()
	vm.defineJSONBuiltins()
}
```

- [ ] **Step 4: Move builtin registrations byte-for-byte by domain**

Move each existing `DefineNative`/`DefineNativeWithSignature` closure without
editing its body. Use this exact ownership:

- core: `print`, `iprint`, `to_str`, `to_int`, `to_float`, `hex`,
  `hex_encode`, `hex_decode`, `base64_encode`, `base64_decode`,
  `base62_encode`, `base62_decode`, `fmt`;
- concurrency: `spawn`, `make_chan`, `chan_send`, `chan_close`,
  `chan_is_closed`, `chan_recv`, `make_wg`, `wg_add`, `wg_done`, `wg_wait`;
- time: every native whose name starts `time_`;
- IO: `input` and every native whose name starts `io_`;
- strings: `ord` and every native whose name starts `strings_`, retaining both
  registrations of `strings_contains` and `strings_replace` in their current
  relative order;
- crypto: every native whose name starts `crypto_`;
- system: every native whose name starts `sys_`, including plugin bridge code;
- collections: `length`, `keys`, `delete`, `append`, `pop`, `slice`,
  `contains`, `has_key`, `to_bytes`;
- network: every native whose name starts `net_` and its shared helpers/state
  access;
- SQLite: every native whose name starts `sqlite_` and its statement helpers;
- JSON: `json_dumps`, `json_parse`, `json_loads`, `jsonValToGo`, `goValToNoxy`,
  `populateTarget`, `populateObj`, `populateRef`, and
  `jsonReplacementCompatible`.

Keep imports with the file that uses them. Do not reorder closure statements
inside a domain method.

- [ ] **Step 5: Verify GREEN, behavior, and coverage**

```powershell
gofmt -w internal/vm/vm.go internal/vm/builtins*.go internal/vm/architecture_test.go
go test ./internal/vm -run TestBuiltinSourceLayout -v
$profile = Join-Path $env:TEMP 'noxy-vm-builtins-split.cover'
go test -coverprofile $profile ./internal/vm
go tool cover -func=$profile
go test ./internal/...
```

Expected: structural and behavioral tests pass; total coverage remains at least
60.0%; the registry snapshot remains identical.

- [ ] **Step 6: Commit builtin extraction**

```powershell
git add internal/vm
git commit -m "refactor(vm): split builtin implementations by domain"
```

---

### Task 8: Extract Stack, Upvalues, and Calls

**Files:**
- Modify: `internal/vm/architecture_test.go`
- Create: `internal/vm/stack.go`
- Create: `internal/vm/calls.go`
- Modify: `internal/vm/vm.go`

**Interfaces:**
- `stack.go`: `readShort`, `isFalsey`, `valuesEqual`, `readConstant`, `push`, `pop`, `peek`, `captureUpvalue`, `closeUpvalue`
- `calls.go`: `callValue`, `call`, `copyValue`

- [ ] **Step 1: Extend the architecture test and verify RED**

Add source-layout assertions for every function in the Interfaces block.

```powershell
go test ./internal/vm -run TestStackAndCallSourceLayout -v
```

Expected: FAIL because `stack.go` and `calls.go` do not exist.

- [ ] **Step 2: Move complete declarations without editing bodies**

Move the listed declarations from `vm.go` to `stack.go` and `calls.go`. Retain
receivers, parameter names, comments, panics, and error strings exactly.

- [ ] **Step 3: Verify GREEN and coverage**

```powershell
gofmt -w internal/vm/vm.go internal/vm/stack.go internal/vm/calls.go internal/vm/architecture_test.go
go test ./internal/vm -run TestStackAndCallSourceLayout -v
$profile = Join-Path $env:TEMP 'noxy-vm-stack-calls.cover'
go test -coverprofile $profile ./internal/vm
go tool cover -func=$profile
go test ./internal/...
```

Expected: PASS and at least 60.0% coverage.

- [ ] **Step 4: Commit stack and call extraction**

```powershell
git add internal/vm
git commit -m "refactor(vm): separate stack and call mechanics"
```

---

### Task 9: Extract Module Loading

**Files:**
- Modify: `internal/vm/architecture_test.go`
- Create: `internal/vm/modules.go`
- Modify: `internal/vm/vm.go`

**Interfaces:**
- Produces unchanged method: `func (vm *VM) loadModule(name string) (value.Value, error)`

- [ ] **Step 1: Add and fail the module layout assertion**

Assert `modules.go` declares `loadModule`.

```powershell
go test ./internal/vm -run TestModuleSourceLayout -v
```

Expected: FAIL because `modules.go` does not exist.

- [ ] **Step 2: Move module loading as one declaration**

Move the complete `loadModule` method, including embedded-stdlib resolution,
filesystem module resolution, compiler setup, cache/lock interaction, module
frame construction, synchronous execution, result pop, and returned map. Move
the imports `ast`, `compiler`, `lexer`, `parser`, `stdlib`, and `filepath` out
of `vm.go` when no longer used there.

- [ ] **Step 3: Verify GREEN, module behavior, and coverage**

```powershell
gofmt -w internal/vm/vm.go internal/vm/modules.go internal/vm/architecture_test.go
go test ./internal/vm -run 'TestModuleSourceLayout|TestRuntime.*Module|TestImportedFunction' -v
$profile = Join-Path $env:TEMP 'noxy-vm-modules.cover'
go test -coverprofile $profile ./internal/vm
go tool cover -func=$profile
go test ./internal/...
```

Expected: PASS and at least 60.0% coverage.

- [ ] **Step 4: Commit module extraction**

```powershell
git add internal/vm
git commit -m "refactor(vm): isolate module loading"
```

---

### Task 10: Move the Executor Last

**Files:**
- Modify: `internal/vm/architecture_test.go`
- Create: `internal/vm/executor.go`
- Modify: `internal/vm/vm.go`

**Interfaces:**
- Produces unchanged methods: `Interpret`, `InterpretWithGlobals`, `run`
- Leaves in `vm.go`: constants, state types, constructors, `runtimeError`, `DefineNative`, `DefineNativeWithSignature`, `SetGlobal`, `GetGlobal`, `SetModule`, `GetModule`

- [ ] **Step 1: Add executor and final `vm.go` boundary assertions**

Assert `executor.go` declares `Interpret`, `InterpretWithGlobals`, and `run`.
Read `vm.go` and assert it contains fewer than 400 physical lines. Parse its
top-level functions and assert it contains exactly the functions listed in the
Interfaces block plus constructors and `runtimeError`.

- [ ] **Step 2: Run the layout test to verify RED**

```powershell
go test ./internal/vm -run TestExecutorSourceLayout -v
```

Expected: FAIL because `executor.go` does not exist and `vm.go` exceeds 400
lines.

- [ ] **Step 3: Move execution entry points and the loop verbatim**

Move complete declarations for `Interpret`, `InterpretWithGlobals`, and `run`
to `executor.go`. Keep the opcode switch in its existing order and do not
extract handlers, rename locals, alter stack operations, or rewrite errors.

- [ ] **Step 4: Verify GREEN, coverage, and repository tests**

```powershell
gofmt -w internal/vm/vm.go internal/vm/executor.go internal/vm/architecture_test.go
go test ./internal/vm -run TestExecutorSourceLayout -v
$profile = Join-Path $env:TEMP 'noxy-vm-final-layout.cover'
go test -count=1 -coverprofile $profile ./internal/vm
go tool cover -func=$profile
go test ./internal/...
go test ./...
```

Expected: all commands pass; coverage is at least 60.0%; `vm.go` is below 400
lines.

- [ ] **Step 5: Commit executor extraction**

```powershell
git add internal/vm
git commit -m "refactor(vm): isolate bytecode executor"
```

---

### Task 11: Final Verification and Scope Audit

**Files:**
- Modify only files required to fix formatting, import, or mechanical movement defects exposed by verification

**Interfaces:**
- Verifies the complete design and AGENTS.md workflow

- [ ] **Step 1: Audit the diff for behavioral changes**

```powershell
git diff develop...HEAD --stat
git diff develop...HEAD -- internal/chunk internal/ast internal/compiler internal/parser internal/value
git diff --check
```

Expected: no changes under AST, chunk, compiler, parser, or value; no whitespace
errors; production diffs under `internal/vm` consist of moved code and
constructor orchestration.

- [ ] **Step 2: Run formatting, tests, coverage, vet, and build**

```powershell
gofmt -w internal/vm
$profile = Join-Path $env:TEMP 'noxy-vm-final.cover'
go test -count=1 -coverprofile $profile ./internal/vm
go tool cover -func=$profile
go test ./internal/...
go test ./...
go vet ./...
go build -o noxy.exe ./cmd/noxy
```

Expected: all supported commands pass; statement coverage is at least 60.0%.

- [ ] **Step 3: Run the race detector**

```powershell
go test -race ./internal/vm
```

Expected: PASS. If the Go/Windows toolchain reports that race instrumentation
is unsupported, record the exact toolchain error in the handoff and do not
describe the race check as passing.

- [ ] **Step 4: Run the Noxy concurrent integration suite**

```powershell
go run cmd/noxy/main.go noxy_examples/run_all_tests_concurrent.nx
```

Expected: final report contains `Falhou: 0` and `TODOS TESTES PASSARAM.`

- [ ] **Step 5: Verify final architecture and clean status**

```powershell
(Get-Content internal/vm/vm.go | Measure-Object -Line).Lines
git status --short
git log --oneline develop..HEAD
```

Expected: `vm.go` is below 400 lines; status is clean except for the ignored
`noxy.exe`; history shows the design, test-only safety net, and responsibility-
focused extraction commits.

- [ ] **Step 6: Commit verification-only corrections if present**

If verification exposed import or mechanical movement corrections, commit only
those corrections:

```powershell
git add internal/vm
git commit -m "fix(vm): preserve behavior after modularization"
```

If no corrections were needed, leave history unchanged.

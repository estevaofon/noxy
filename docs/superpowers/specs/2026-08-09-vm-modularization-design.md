# VM Modularization Design

## Context

`internal/vm/vm.go` currently combines VM state, construction, builtin
registration and implementation, interpretation, opcode dispatch, calls,
stack management, module loading, and resource integrations. The package is
already split for focused reference, runtime-type, call-validation, and JSON
population logic, but `vm.go` remains the dominant change surface.

The refactoring starts from `develop` at commit `adf289d`. The initial
`internal/vm` statement coverage is 37.7%, and the complete `go test ./...`
baseline passes.

## Goal

Split `internal/vm/vm.go` into focused files inside the existing `vm` package,
after establishing a characterization-test safety net of at least 60%
statement coverage for `internal/vm`.

The refactoring must preserve observable behavior exactly: public APIs,
bytecode, opcodes, language semantics, native contracts, error text, line
reporting, resource behavior, and the effective final builtin registry remain
unchanged.

## Non-Goals

- Do not add, remove, or renumber opcodes.
- Do not change the AST, parser, compiler, bytecode format, or language spec.
- Do not change public VM symbols or introduce new public interfaces.
- Do not redesign the opcode dispatch loop or create one handler per opcode.
- Do not fix pre-existing behavioral defects discovered by characterization.
- Do not remove duplicate native registrations or rename native functions.
- Do not optimize execution or make performance claims in this branch.
- Do not create `internal/vm` subpackages.

## Chosen Approach

All extracted files remain in `internal/vm` with `package vm`. This preserves
access to the current private state and helpers and avoids new interfaces,
exports, dependency cycles, or runtime indirection.

Subpackages were rejected because they would require exposing internal state
or designing new interfaces, which is incompatible with a behavior-preserving
refactor. Extracting only builtins was rejected because it would leave calls,
modules, stack management, and execution concentrated in `vm.go`.

## Target File Structure

| File | Responsibility |
| --- | --- |
| `vm.go` | Constants, `CallFrame`, `SharedState`, `VM`, constructors, public global/module accessors, and public lifecycle API that does not belong to the executor |
| `executor.go` | `Interpret`, `InterpretWithGlobals`, and the unchanged `run` opcode loop |
| `stack.go` | Stack reads/writes, constant reads, truth/equality helpers, upvalue capture, and upvalue close operations |
| `calls.go` | Callable dispatch, script calls, constructor calls, native calls, argument copying, and composite shallow-copy behavior |
| `modules.go` | Module resolution, compilation, execution, cache interaction, wildcard exports, and module-frame preparation |
| `references.go` | Existing reference resolution, read, and write helpers; it remains the reference boundary |
| `builtins.go` | Ordered orchestration of builtin registration only |
| `builtins_core.go` | Printing, scalar conversions, formatting, encoding, and other core primitives |
| `builtins_collections.go` | Array, map, length, mutation, membership, slicing, and collection helpers |
| `builtins_concurrency.go` | Spawn, channels, wait groups, and concurrency primitives |
| `builtins_time.go` | Time creation, conversion, parsing, formatting, sleeping, and calendar helpers |
| `builtins_strings.go` | String operations, including the current duplicate registrations in their effective order |
| `builtins_io.go` | File and stream operations |
| `builtins_sys.go` | Process, environment, operating-system, plugin-loading, and command-execution operations |
| `builtins_crypto.go` | Cryptographic and secure-random operations |
| `builtins_net.go` | TCP/UDP resource and selection operations |
| `builtins_sqlite.go` | SQLite connections, statements, parameters, queries, and cleanup |
| `builtins_json.go` | JSON entry points and conversion bridges to the existing population helpers |
| `call_validation.go` | Existing parameter-mode validation; unchanged in responsibility |
| `json_population.go` | Existing atomic typed JSON population; unchanged in responsibility |
| `runtime_type_validation.go` | Existing runtime schema validation; unchanged in responsibility |

The final `vm.go` should contain fewer than 400 lines. The executor loop may
remain large because splitting its cases is outside this branch.

## Runtime Data Flow

Construction remains:

1. `New` delegates to `NewWithConfig`.
2. `NewWithConfig` creates shared state and delegates to `NewWithShared`.
3. `NewWithShared` initializes VM-local state and invokes
   `vm.defineBuiltins()`.
4. `defineBuiltins` invokes domain registration functions. Unique names may be
   grouped by domain; duplicate names retain their relative overwrite order so
   the effective final registry remains identical.

Execution remains:

1. `Interpret` or `InterpretWithGlobals` prepares the initial call frame.
2. `run` reads and dispatches bytecode.
3. Opcode cases delegate to existing stack, call, reference, runtime-type,
   JSON, and module helpers.
4. Calls and module execution may enter nested frames without changing stack
   ownership or global-map ownership.

Only source-file ownership changes. Values, locks, maps, frames, and resources
keep their current runtime ownership.

## Error and Compatibility Rules

- Preserve all returned error messages byte-for-byte unless `gofmt` changes
  source layout without affecting string content.
- Preserve file and line attribution from `runtimeError`.
- Preserve stack state on failed calls and constructor validation.
- Preserve legacy no-op or sentinel behavior in mutating natives.
- Preserve panic behavior for existing internal-limit assertions.
- Preserve global/module locking and resource locking.
- Preserve native names, signatures, variadic rules, and the effective final
  registry. Literal cross-domain order for unique names is not observable and
  may change when registrations are grouped by domain.
- Preserve duplicate `strings_contains` and `strings_replace` registrations;
  preserve their relative overwrite order. Cleanup belongs to a separate
  behavioral change.

## Test Safety Net

No production source is moved until a test-only commit brings
`internal/vm` statement coverage to at least 60%.

The test suite will add:

- `vm_test_helpers_test.go` for compiling and executing Noxy snippets and for
  inspecting results without duplicating setup;
- `executor_characterization_test.go` for constants, stack behavior, control
  flow, arithmetic, collections, properties, closures, specialized opcodes,
  type markers, and malformed bytecode errors;
- `calls_characterization_test.go` for script functions, closures, natives,
  constructors, arity, shallow copying, failed calls, and stack preservation;
- `modules_characterization_test.go` for imports, aliases, selected and
  wildcard exports, caching, ownership, cycles, and failure propagation;
- domain-focused `builtins_*_test.go` files for successful and invalid calls.

Tests that require resources must be hermetic:

- files and SQLite databases use `t.TempDir()`;
- networking binds loopback on an operating-system-assigned port;
- concurrent tests use bounded waits and explicit timeouts;
- no test depends on internet access, external services, or user state.

Coverage is measured with:

```powershell
$coverage = Join-Path $env:TEMP 'noxy-vm.cover'
go test -coverprofile $coverage ./internal/vm
go tool cover -func $coverage
```

The extraction phase cannot begin until the total reported statement coverage
is at least 60.0% and tests explicitly exercise executor, calls, stack/upvalue,
module, reference, and builtin boundaries. Coverage must remain at or above
60.0% after every production extraction.

After the safety-net commit, an architecture test will be introduced in a
failing state to assert the approved file boundaries. The minimal production
movement makes that test pass, providing the red-green gate for the refactor.

## Migration Sequence

1. Add only characterization tests and shared test helpers.
2. Reach at least 60.0% package coverage and commit the safety net separately.
3. Add the failing architecture boundary test.
4. Extract builtin registration and implementations by domain.
5. Extract stack and upvalue helpers.
6. Extract callable dispatch and shallow-copy helpers.
7. Extract module loading and execution.
8. Move interpretation entry points and the complete opcode loop last.
9. Verify formatting, coverage, race behavior, all Go tests, the build, and the
   Noxy concurrent integration runner.

Each extraction must compile and pass `go test ./internal/vm` before the next
one begins. Commits remain responsibility-focused so a failed extraction can
be reverted independently.

## Verification

The final verification commands are:

```powershell
gofmt -w internal/vm
$coverage = Join-Path $env:TEMP 'noxy-vm.cover'
go test -coverprofile $coverage ./internal/vm
go tool cover -func $coverage
go test ./internal/...
go test ./...
go test -race ./internal/vm
go vet ./...
go build -o noxy.exe ./cmd/noxy
go run cmd/noxy/main.go noxy_examples/run_all_tests_concurrent.nx
```

If the race detector is unsupported by the local Go/Windows toolchain, that
limitation is reported explicitly rather than treated as a passing result.

## Acceptance Criteria

- The test-only safety-net commit precedes every production extraction.
- `internal/vm` statement coverage is at least 60.0% before and after extraction.
- `vm.go` contains fewer than 400 lines and only its approved responsibilities.
- All target files exist and contain only their approved responsibilities.
- No public VM API, opcode, bytecode, or language behavior changes.
- The effective final native registry and duplicate overwrite behavior remain
  unchanged; unique cross-domain registrations may be grouped.
- All supported final verification commands pass with zero failures.
- The worktree contains no unrelated changes or generated tracked artifacts.

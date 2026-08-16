# Noxy Language Specification

## Overview

Noxy is statically typed, with explicit dynamic boundaries through `any`, bare
`func`, untyped native primitives, and plugins without signatures. Designed for
educational purposes and practical applications, it supports structs,
references, arrays, f-strings, and a module system.
The current implementation is a **Stack-based VM** written in **Go**.

---

## 1. Lexical Structure

### 1.1 Comments

```noxy
// Single line comment
```

### 1.2 Keywords

| Category | Keywords |
|----------|----------|
| Declarations | `let`, `func`, `struct` |
| Control Flow | `if`, `elif`, `then`, `else`, `end`, `while`, `do`, `return`, `break`, `for`, `in`, `defer` |
| Types | `int`, `float`, `string`, `str`, `bool`, `void`, `ref`, `bytes`, `func` |
| Literals | `true`, `false`, `null` |
| Modules | `use`, `select`, `as` |
| Specials | `zeros` |

### 1.3 Operators

| Category | Operators |
|----------|-----------|
| Arithmetic | `+`, `-`, `*`, `/`, `%` |
| Comparison | `>`, `<`, `>=`, `<=`, `==`, `!=` |
| Logical | `&&`, `||`, `!` |
| Bitwise | `&`, `|`, `^`, `~`, `<<`, `>>` |
| Assignment | `=` |
| Reference | `ref` |
| Function Return | `->` |

### 1.4 Delimiters

| Symbol | Usage |
|--------|-------|
| `(` `)` | Parentheses for expressions and function calls |
| `[` `]` | Brackets for arrays and indexing |
| `{` `}` | Braces for f-string interpolation and map literals |
| `,` | Separator for parameters and elements |
| `:` | Separator for types in declarations |
| `.` | Access struct fields or module members |

---

## 2. Type System

### 2.0 Fundamental Typing Rules

#### Static Typing and Type-Stable Variables

Noxy uses **type-stable variables**: a variable's declared type remains stable,
while the value stored in it may be mutable.

1. **The type of a variable is defined at declaration and cannot be changed.**
2. Attempts to assign a value of a different type result in a compilation error.
3. There is no implicit conversion between types (except where explicitly documented).

```noxy
let x: int = 42
x = 100          // ✓ OK - same type (int)
x = 3.14         // ✗ ERROR - cannot assign float to int variable
x = "text"       // ✗ ERROR - cannot assign string to int variable
```

#### Compile-Time Type Checking

- Outside the explicit dynamic boundaries listed above, type errors are detected
  **before** execution.
- The compiler checks compatibility in assignments, exact function calls, and
  operations. Dynamic boundaries validate the contracts available at runtime.

### 2.1 Primitive Types

| Type | Description | Example |
|------|-------------|---------|
| `int` | 64-bit Integer | `42`, `-10`, `0` |
| `float` | Double precision Floating Point | `3.14`, `-0.5`, `1.0` |
| `string` | Character string | `"Hello"`, `""` |
| `bool` | Boolean value | `true`, `false` |
| `void` | Absence of value (function return only) | - |
| `bytes` | Raw byte sequence | `b"Data"`, `hex_decode("FF")` |

### 2.2 Composite Types

#### Shallow-Copy Semantics

Arrays, maps, and structs are heap-backed composite values. When one of these values is passed to a function parameter that is not declared with `ref`, Noxy creates a **shallow copy**:

1. A new top-level array, map, or struct instance is created for the parameter.
2. Immediate elements, entries, or fields are copied into that new container.
3. Primitive values are independent after the copy.
4. Nested composite values keep their identity and therefore remain shared with the caller.

Consequently, replacing or directly mutating the top-level copy does not affect the caller, but mutating a nested composite value can be observed by the caller. This is **not** a deep copy.

Parameters declared with `ref` skip the shallow copy and access the caller's original value directly.

#### Concurrency and composite values

Shared routines use synchronized global bindings, module state, maps, and runtime handle registries. An individual binding lookup/update or map operation is safe from the Go runtime's concurrent-map crash, but synchronization is not recursive and does not make a read-modify-write sequence atomic. Normal calls and `spawn_task` use the shallow-copy parameter rules above. The legacy detached `spawn` is a compatibility exception and forwards argument values directly, preserving the top-level identity of mutable composites. Concurrent compound operations or mutation through any shared identity require coordination through channels or another explicit single-owner protocol.

This runtime foundation does not change any public Noxy syntax, builtin signature, result shape, or shallow-copy rule.

#### Arrays (Dynamic and Fixed)

**1. Dynamic Arrays (Recommended)**
```noxy
// Declaration (starts empty)
let dynamic: int[] 

// Operations
append(dynamic, 10)
length(dynamic)
```

**2. Fixed Size Arrays**
```noxy
let fixed: int[5] = [1, 2, 3, 4, 5]
let zeroed: int[100] = zeros(100)
```

**Pass-by-Value Behavior**:
Arrays are passed by **VALUE** using a shallow copy by default. The outer array is independent, but nested arrays, maps, or structs remain shared. Use `ref` when the function must modify the caller's outer array directly.

#### Maps (Hashmaps)

```noxy
// Type: map[Key, Value]
let scores: map[string, int] = {"Alice": 100}
scores["Bob"] = 50
```

**Pass-by-Value Behavior**:
Maps are passed by **VALUE** using a shallow copy by default. The outer map is independent, but nested composite values remain shared. Use `ref` when the function must modify the caller's outer map directly.

#### Structs

```noxy
struct Person
    name: string
    age: int
end
```

**Pass-by-Value Behavior**:
Structs are passed by **VALUE** using a shallow copy by default. Direct fields belong to the copied instance, but nested composite fields remain shared. Use `ref` when the function must modify the caller's original struct instance directly.

---
### 2.3 The `ref` Operator

The `ref` operator produces an explicit first-class reference value according
to the operand's type:

1. For an addressable operand of type `T`, `ref value` creates a `ref T` that
   points to that operand's storage.
2. For an operand whose type is already `ref T`, `ref reference` forwards the
   existing reference value. Its result remains `ref T`; it does not take the
   address of the reference variable or create `ref ref T`.

The forwarding form is useful when an existing reference crosses a dynamic
boundary whose signature is not available for contextual conversion.

#### L-Value Requirement
Creating a new reference requires an **addressable value** (L-Value). The
operand must be a variable, a struct field, or an array/map index. A non-reference
temporary, such as an ordinary function result or a literal, is not
addressable. Forwarding an expression that already has type `ref T` does not
create a new reference and therefore does not require a second storage slot.

Captured variables are addressable through their upvalue storage. Non-null
literals and plain function-result temporaries are not addressable. A function
result whose declared type is already `ref T` is a reference value and may be
passed directly. `null` remains the explicit nullable `ref T` value: it is
accepted without pretending that it owns a storage slot.

**Correct Usage:**
```noxy
let err: Error = Error("msg")
let r: ref Error = ref err      // OK: 'err' is a variable
let forwarded: ref Error = ref r // OK: forwards the existing ref Error
```

**Incorrect Usage:**
```noxy
let r: ref Error = ref Error("msg") // ERROR: Cannot take reference of temporary value
```

### 2.3 Reference Semantics (`ref`)
The `ref` keyword creates references to addressable values or explicitly
forwards existing reference values. Noxy unifies reference usage through
**"Automatic Dereference"** and **"Type-Based Assignment"**.

#### 1. Automatic Dereference (Expressions)
You can use a reference (`ref T`) in expressions just like a normal value. The compiler automatically assumes you want the **value**.
```noxy
let x: int = 10
let r: ref int = ref x

// 1. Reading (Auto-Dereference)
// You can use the reference directly to READ the value.
// The compiler automatically follows the pointer.
let y: int = r + 1   // Compiler auto-derefs 'r' -> 11
print(r)             // Prints 10
```
This applies to both Local Variables and Struct Fields.

#### 2. Writing (Update vs Rebind)
The distinction between modifying the *value* and modifying the *pointer* is made explicit by syntax:

**A. Value Update (Explicit `*`)**
To update the content of the memory pointed to by a reference, you MUST use the dereference operator `*`.
```noxy
*r = 20      // DESTROY/UPDATE: Writes 20 into the memory of 'x'
*box.val = 30 // Writes 30 into the memory pointed to by 'box.val'
```

**B. Pointer Rebind (Standard `=`)**
To change the reference itself (make it point to something else), use standard assignment.
*Note: The type of the RHS must be a Reference (`ref T`).*
```noxy
let z: int = 99
r = ref z    // REBIND: 'r' now points to 'z' (does not affect 'x')
```

#### 3. Strict Type Safety
The compiler enforces these rules to prevent ambiguity:
```noxy
r = 50       // ERROR: Cannot assign 'int' to 'ref int'. Did you mean '*r = 50'?
*r = ref z   // ERROR: Cannot assign 'ref int' to 'int'.
```

#### 4. Reference Patterns

These patterns allow Noxy to safely support smart pointers and mutable bindings.

##### Pattern A: Mutable Bindings (Pass-by-Reference)

Functions can modify external variables through references:

```noxy
func double_it(val: ref int)
    *val = val * 2  // UPDATE: writes to original variable
end

func swap(a: ref int, b: ref int)
    let val_a: int = a  // Read values (auto-deref)
    let val_b: int = b
    *a = val_b          // UPDATE: write to address of 'a' using '*'
    *b = val_a          // UPDATE: write to address of 'b' using '*'
end

let x: int = 10
double_it(x)      // exact signature borrows addressable x; x is now 20
double_it(ref x)  // explicit reference is also valid; x is now 40

let a: int = 100
let b: int = 200
swap(ref a, ref b)  // a=200, b=100
```

> **Note**: This syntax makes swaps safe and explicit. `a = b` would try to rebind the pointer `a` to point to the same place as `b` (if `b` were a reference expression), which is not what you want in a swap.

##### Pattern B: Dynamic Aliases

A local reference can be rebound to point to different variables:

```noxy
let counter_A: int = 0
let counter_B: int = 0

let active: ref int = ref counter_A

*active = *active + 1     // Updates counter_A (now 1)
active = ref counter_B    // REBIND: now points to counter_B
*active = *active + 1     // Updates counter_B (now 1)
// Result: counter_A=1, counter_B=1
```

##### Pattern C: Smart Pointers (Observer Pattern)

Structs with reference fields can dynamically switch their data source:

```noxy
struct Observer
    name: string
    target: ref int
end

let temperature: int = 20
let humidity: int = 50

let sensor: Observer = Observer("Main", ref temperature)

// Read through reference
print(sensor.target)  // 20 (auto-deref)

// UPDATE value
*sensor.target = 25   // temperature is now 25

// REBIND to different source
sensor.target = ref humidity  // Now watching humidity
*sensor.target = 70           // humidity is now 70
```

##### Summary Table: Type-Based Assignment

| LHS Type | RHS Type | Syntax | Action |
|----------|----------|--------|--------|
| `ref T` | `T` | `*r = val` | **UPDATE** – writes into memory |
| `ref T` | `ref T` | `r = ref x`| **REBIND** – changes pointer |
| `T` | `T` | `x = val` | Standard assignment |

#### Memory Safety (Captured Variables)
Noxy ensures memory safety when using `ref`.
- If you create a `ref` to a **local variable**, that variable is automatically **Captured** (moved to the Heap) by the compiler.
- Implemented via **Upvalues**, this ensures that the variable survives the end of the function scope.

```noxy
func create_safe_ref() -> ref int
    let x: int = 42
    return ref x // Safe! 'x' is promoted to Heap because it is referenced.
end
```

---

## 3. Variable Declarations

```noxy
let name: type = value
```

Variables can be reassigned, but the new value **MUST** be of the same type as declared.

---

## 4. Functions

### 4.1 Definition

```noxy
func name(param1: type1, param2: type2) -> returnType
    // body
    return value
end

// Anonymous Function / Closure
let adder: func = func(x: int) -> int
    return x + 1
end

// Nested Functions
func makeAdder(x: int) -> func
    return func(y: int) -> int
        return x + y // Captures 'x'
    end
end
```

### 4.2 Function Types

Every function value is callable. Noxy provides two ways to describe how precisely it may be called.

An **exact function type** records parameter and return types:

```noxy
func(int, int) -> int
func(string) -> void
func(ref int) -> void

let exact: func(int) -> int = double

func apply(f: func(int) -> int, value: int) -> int
    return f(value)
end
```

Calls through exact types are checked during compilation: arity, argument types, `ref` addressability, and return types must match. Exact signatures are invariant; parameter count, parameter types, `ref` modifiers, and return types must be identical. `null` satisfies `ref`, struct, and `any` contracts, but not primitive value or callable contracts. An omitted return annotation on a function declaration or literal means `void`.

When an exact parameter is `ref T`, an addressable expression of type `T` is
converted contextually at the call site. Both the concise and explicit forms
are valid:

```noxy
let value: int = 10
double_it(value)       // exact signature borrows addressable value
double_it(ref value)   // explicit reference is also valid
```

This contextual conversion is limited to exact script signatures and public
native contracts known by the compiler. It never applies at a dynamic
boundary.

Bare `func` is the **dynamic callable type**. It guarantees only that the value is callable:

```noxy
let dynamic: func = exact       // exact-to-dynamic widening is valid
let exact_again: func(int) -> int = dynamic // ERROR: no implicit narrowing
```

Calls through bare `func` are checked by the runtime because their arity and result type are not statically known. Their compile-time result is `any`, so a function returning their result must either declare `any` or replace the bare callable with an exact signature. Explicit `ref` arguments remain references across this dynamic boundary. This keeps dynamic callbacks, decorators, handlers, and heterogeneous callable collections available without pretending their results are statically known:

```noxy
let callbacks: func[] = [no_arguments, two_arguments]
```

Because bare `func` has no exact signature, a reference argument must be
written explicitly as `ref value`; `dynamic(value)` passes a plain value and
never infers or manufactures a reference. The same rule applies to untyped
native primitives, plugins without signatures, and dynamic module members.

Use parentheses for an array whose elements are exact functions. Without parentheses, the array belongs to the return type:

```noxy
let transforms: (func(int) -> int)[] = [double, increment]
let factory: func(int) -> int[]
```

Exact function types may also appear in parameters, returns, struct fields, map values, channels, and references. `any`, native functions, plugins, and untyped module exports remain dynamic boundaries and retain runtime validation; an unknown native value cannot be implicitly narrowed to an exact function type.

### 4.3 Parameter Passing Semantics (CRITICAL)

Noxy uses **Pass-by-Value** by default. Primitive values are copied directly. Composite values (arrays, maps, and structs) receive a **shallow copy** of their top-level container.

#### Pass-by-Value / Shallow Copy (Default)

When a composite value is passed to a parameter without `ref`, the function receives a new top-level container. Replacing or directly mutating that top-level container does not affect the caller:

```noxy
func modify(arr: int[]) -> void
    append(arr, 999) // Modifies local copy only
end

let list: int[] = [1, 2, 3]
modify(list)
// list is still [1, 2, 3]
```

The copy is shallow. Nested composite values are not recursively copied and remain shared:

```noxy
struct Box
    values: int[]
end

func replace_nested(box: Box) -> void
    box.values = [100, 200] // Replaces a field only in the copied Box
end

func mutate_nested(box: Box) -> void
    box.values[0] = 99 // Mutates the shared nested array
end

let values: int[] = [1, 2]
let box: Box = Box(values)

replace_nested(box)
// box.values is still [1, 2]

mutate_nested(box)
// box.values is now [99, 2]
// values is also [99, 2]
```

#### Pass-by-Reference (`ref`)
To skip the shallow copy and let a function access the caller's original top-level value, use `ref` in the parameter type.

```noxy
func modify(arr: ref int[]) -> void
    append(arr, 999) // Modifies the ORIGINAL list
end

let list: int[] = [1, 2, 3]
modify(list)
// list is now [1, 2, 3, 999]
```

This applies to Structs and Maps as well.

---

## 5. Structs

```noxy
struct Point
    x: int,
    y: int
end

// Constructor
let p: Point = Point(10, 20)

// Field Access
p.x = 15
```

### Self-Reference
Structs can reference themselves using `ref`.

```noxy
struct Node
    value: int
    next: ref Node
end
```

---

## 6. Control Flow

### If-Then-Else
```noxy
if condition then
    // ...
elif condition2 then
    // ...
else
    // ...
end
```

### While Loop
```noxy
while condition do
    // ...
end
```

### For Loop
Use `for ... in` to iterate over collections (arrays or maps).

**Arrays** (Iterates over values):
```noxy
for item in array do
    print(item)
end
```

**Maps** (Iterates over keys):
```noxy
for key in map do
    print(map[key])
end
```

**Strings** (Iterates over characters):
```noxy
for char in "hello" do
    print(char)
end
```

### Defer and deterministic cleanup

`defer` registers a call to run when its containing call frame exits. Its
operand must be a real call; arbitrary expressions and non-call operations
such as `defer addr(ref value)` are compile-time errors.

```noxy
let file: io.File = io.open(path, "w")
defer io.close(file)
```

The callee and every argument are evaluated immediately, from left to right,
when the `defer` statement executes. An evaluation or registration error does
not register that call, although calls registered earlier in the frame still
run. For typed Noxy functions and signed natives, non-`ref` arguments receive
the normal top-level shallow copy at registration time, while `ref` parameters
retain their reference. Nested composite identities therefore remain shared as
described in [Shallow-Copy Semantics](#shallow-copy-semantics). Legacy untyped
natives retain values using their existing dynamic calling convention because
they expose no parameter-mode metadata. Struct constructors retain evaluated
field values directly, matching their existing constructor semantics.

Deferred calls execute once in last-in, first-out (LIFO) order for each frame.
A `defer` inside a loop registers once per executed iteration and belongs to
the containing frame, not to the loop or block. The same frame rule applies to
ordinary functions, the main script, module initialization, and functions run
by detached `spawn`. Deferred calls run on explicit and implicit returns, on
successful script or module completion, and while unwinding runtime errors.
Nested calls finish their own deferred work before the owning frame continues
with its next older entry.

An ordinary return value from a deferred call is discarded, including an
error-shaped Noxy result value. A runtime failure is different: the original
runtime error remains primary, every observable deferred failure is collected
in LIFO execution order with its registration location, and older deferred
calls continue to run. Nested cleanup and module failures remain nested,
structured causes rather than being flattened into one message. If cleanup is
the first failure, it converts an otherwise successful return into a runtime
error.

Resource cleanup uses the existing explicit close APIs. Register dependent
resources after their owners so LIFO order closes the dependent first; for
example, register `sqlite.close(db)` before `sqlite.finalize(statement)`.
Current file, network, and SQLite close builtins suppress some underlying
Go/operating-system close errors and return success-compatible Noxy values.
Those suppressed errors cannot participate in defer aggregation; only runtime
errors observable from the deferred call are aggregated. Likewise, the
ordinary result from `io.close_result(...)` is discarded when deferred.

---

## 7. Expressions

### Mathematical
`+`, `-`, `*`, `/`, `%`

### Comparison
`>`, `<`, `>=`, `<=`, `==`, `!=`

### Logical
- `&&` (AND)
- `||` (OR)
- `!` (NOT)

### Bitwise
- `&` (AND)
- `|` (OR)
- `^` (XOR)
- `~` (NOT)
- `<<`, `>>` (Shift)

---

## 8. F-Strings

String interpolation with `f"..."`.

```noxy
let name: string = "Noxy"
print(f"Hello, {name}!")
```

## 9. Built-in Functions

### I/O
- `print(expr)`: Prints to stdout.

### Conversions
- `to_str(val)`
- `to_int(val)`
- `to_float(val)`
- `to_bytes(val)`

### Conversões numéricas

`to_int` e `to_float` **levantam erro de runtime** quando a conversão é
impossível. Use-os quando uma falha seria um bug do programa.

```noxy
to_int(5.9)      // 5, truncamento em direção a zero
to_int("5")      // 5
to_int("5.5")    // erro: uma string decimal não é um inteiro
to_int("abc")    // erro
to_int(true)     // erro: bool não é número em Noxy
```

Para entrada não confiável, use a forma `_result`, do módulo `convert`, que
nunca levanta:

```noxy
use convert select *

let porta: IntResult = to_int_result(getenv("PORT").value)
if porta.ok then
    print("porta " + to_str(porta.value))
else
    print("PORT inválida: " + porta.error)
end
```

Essa é a mesma convenção de `io.close` / `io.close_result`. Como funções Noxy
têm retorno único, o struct de resultado ocupa o lugar do par `value, err` do
Go.

Validar antes de converter não é uma alternativa correta: `is_digit` aceita
`"9999999999999999999"`, que estoura `int64`, e não há como checar o intervalo
sem converter. Não existe `is_float`.

### Collections
- `length(arr_or_map)`
- `append(arr, val)`
- `pop(arr)`
- `keys(map)`: Returns array of keys.
- `has_key(map, key)`: Returns bool.
- `delete(map, key)`

### Utils
- `addr(ref var)`: Returns the memory address/identity of a variable as a string.
- `zeros(n)`: create zeroed array.
- `hex_encode(data: bytes) -> string`: Converts bytes to hexadecimal string.
- `hex_decode(hex: string) -> bytes`: Converts hexadecimal string to bytes.
- `fmt(format, args...)`: printf-style formatting.
  - `%s`: String
  - `%d`: Integer (Base 10)
  - `%x`, `%X`: Integer (Hex)
  - `%b`: Integer (Binary)
  - `%f`: Float
  - `%.Nf`: Float with N decimal places
  - `%e`: Float (Scientific notation)
  - `%v`: Any value (Default representation)
  - `%t`: Boolean
  - `%q`: Quoted string/bytes

```noxy
let msg: string = fmt("Value: %d, Hex: %x", 255, 255)
// "Value: 255, Hex: ff"

// Hex encoding example
let data: bytes = b"Hello"
let hex: string = hex_encode(data)  // "48656c6c6f"
let back: bytes = hex_decode(hex)   // b"Hello"
```

### Concurrency and Supervised Tasks

`spawn(function, ...arguments)` starts a detached Noxy routine and immediately returns `null`. It exposes no handle and does not propagate the worker's result, runtime error, or panic to its caller. For compatibility, it also forwards arguments without the normal top-level shallow copy, so mutable composite arguments keep the same identity in caller and worker and require explicit coordination to avoid races. Its existing validation and asynchronous diagnostics remain compatible.

`spawn_task(function, ...arguments)` instead validates a Noxy function or closure, arity, and parameter modes synchronously, launches it in a shared child VM, and returns an opaque task handle. Handles may be stored as `any`, passed, printed, and compared by identity, but cannot be constructed or inspected by Noxy code.

`task_await(handle)` waits indefinitely. `task_await(handle, timeout_ms)` accepts a non-negative integer number of milliseconds; zero performs an immediate poll. Invalid handles or timeouts are synchronous runtime errors in the caller.

Each successful await returns a fresh `map[string, any]` envelope:

| Status | `value` | `error` |
|---|---|---|
| `"ok"` | The task's return value, including `null` for a void return | `null` |
| `"error"` | `null` | A structured failure map |
| `"timeout"` | `null` | `null` |

For `"error"`, the failure map contains string fields `kind`, `message`, and `stack`. `kind` is `"runtime"` for a Noxy runtime error or `"panic"` for a recovered Go panic. Runtime stacks are Noxy stacks captured at the failure point; panic stacks are Go stacks. The panic boundary covers the supervised task's main goroutine only, not independent goroutines started by native code.

Task completion is published exactly once and can be awaited consistently by multiple sequential or concurrent waiters. Each envelope and failure map is a fresh container, but an `"ok"` composite value preserves its original identity. Consequently, replay means the same terminal outcome and returned-value identity, not independent deep snapshots.

Timeout is local and non-terminal: it does not cancel the worker, consume the result, or mutate the task. Completion is preferred whenever it is observably available at the deadline. A wait that returns `"timeout"` may therefore be followed by a later wait that returns `"ok"` or `"error"`.

Supervised tasks share globals, module state, runtime resources, closure environments, and the VM configuration. Ordinary composite arguments retain Noxy's top-level shallow-copy semantics and `ref` arguments retain reference identity. Shared references, nested composites, returned composite values, and closure upvalues require explicit concurrency coordination.

---

## 10. Module System

### Basic Import
```noxy
use strings
print(strings.to_upper("hello"))
```

### Alias
```noxy
use strings as s
print(s.to_lower("HELLO"))
```

### Selective Import
```noxy
use strings select to_upper, to_lower
print(to_upper("hello"))
```

---

## 11. Standard Library

Noxy comes with a comprehensive standard library. Available modules include:

| Module | Description |
|--------|-------------|
| `io` | Input/Output operations (read/write files) |
| `strings` | String manipulation (upper, lower, replace, split) |
| `time` | Time and Date functions |
| `sys` | System interactions (argv, exit, env) |
| `net` | Network sockets (TCP/UDP) |
| `http` | HTTP Client and Server |
| `json` | JSON parsing and stringification |
| `crypto` | Cryptographic functions (hashing, UUID) |
| `sqlite` | SQLite database support |
| `rand` | Random number generation |

### Strings

The `strings` module provides text manipulation functions. All index-based
string operations work on **Unicode rune (code point) positions**, not raw
bytes.

#### `substring(s, start, end_idx) -> string`

Returns the sub-string of `s` from rune index `start` (inclusive) to
`end_idx` (exclusive). Negative indices count from the end of the string
(Python-style: `-1` is the last rune, `-2` the second-to-last, etc.).
After resolving negatives, both indices are clamped to `[0, len]` where
`len` is the rune count of `s`. When `start >= end_idx` after resolution,
an empty string is returned.

```noxy
use strings
strings.substring("Hello", 1, 4)    // "ell"
strings.substring("Hello", 0, 2)    // "He"
strings.substring("Hello", 3, 100)  // "lo"   (end clamped to length)
strings.substring("Hello", -2, 5)   // "lo"   (-2 → index 3)
strings.substring("Hello", 0, -1)   // "Hell" (-1 → index 4)
strings.substring("Hello", -3, -1)  // "ll"   (-3 → 2, -1 → 4)
strings.substring("aé🙂z", 1, 3)   // "é🙂"  (rune-based, not byte-based)
```

### Indexação de strings

Uma `string` é indexada por **caractere** (code point Unicode), não por byte.
`length`, `substring`, `char_at`, `index_of`, `slice` e `reverse` usam todos a
mesma unidade, então compõem entre si:

```noxy
use strings select *

let nome: string = substring(linha, 0, index_of(linha, ":"))
```

`length("café")` é `4`, não `5`.

Um code point não é sempre um caractere percebido pelo usuário: `é` pode ser um
code point ou dois, e um emoji com modificador é vários. Só um modelo de
grapheme cluster seria exato, e ele não oferece índice inteiro em tempo
constante. Noxy adota a aproximação por code point, como Python.

`bytes` é o oposto: indexado por **octeto**, através de `length`, `slice` e
acesso por elemento. As funções de `strings` recusam um `bytes` e apontam
`to_str`, que é a ponte explícita entre os dois tipos.

### Invariante UTF-8

Toda `string` Noxy contém UTF-8 válido. A validação acontece uma única vez, na
fronteira onde bytes viram texto, e não em cada operação — dentro do
invariante, toda operação por caractere é correta por construção.

```noxy
use strings select *

let dados: bytes = io.read_bytes(arquivo).data
if is_valid_utf8(dados) then
    let texto: string = to_str(dados)
else
    print("conteúdo não é UTF-8")
end
```

`to_str` levanta erro de runtime sobre bytes inválidos, indicando o offset:

```text
to_str: bytes are not valid UTF-8 at byte offset 5
```

Funções que já possuem struct de resultado — `io.read`, `io.read_lines`,
`sqlite.query`, `sys.exec_output`, `sys.getenv` — reportam pelos campos `ok` e
de erro que já têm, em vez de levantar. Levantar fica reservado às conversões
puras.

Para lidar com bytes arbitrários deliberadamente, mantenha o valor como
`bytes`: `io.read_bytes` e `net.recv` já devolvem `bytes` e não validam nada.

`is_valid_utf8` aceita apenas `bytes`. Passar uma `string` — mesmo através de
`use strings select *`, o caminho que qualquer código real usa — levanta erro
de runtime nomeando o tipo recebido, porque o invariante já respondeu: se o
valor já é `string`, perguntar de novo é a pergunta errada.

O invariante vale em toda fronteira descrita neste documento, com uma exceção:
o script de entrada passado na linha de comando é lido por um caminho separado
do carregamento de módulos e ainda não é validado (registrado como trabalho
futuro no CHANGELOG).

Noxy não aplica normalização Unicode (NFC/NFD). Comparação é byte-exata, como
em Python.

### Network sockets

`net.listen(host, 0)` asks the operating system to choose an available port.
On success, the returned `Socket.port` is that assigned non-zero port, so it
can be passed directly to `net.connect` without inspecting native resources.

#### Persistent I/O deadlines

The `net` module supports portable blocking sockets with optional positive
deadlines:

```noxy
net.settimeout(sock, 250)
net.setblocking(sock, true)
```

`settimeout(sock, timeout_ms)` selects timed mode. `timeout_ms` must be a
positive `int` whose conversion to Go `time.Duration` milliseconds does not
overflow. Each `recv`, `send`, or listener `accept` starts with a fresh
deadline, so time spent between configuration and the operation does not
consume the operation's timeout.

`setblocking(sock, true)` clears the persistent timeout and restores
indefinite blocking, including I/O that is already pending. The compatibility
call `setblocking(sock, false)` is deprecated and remains an unconditional
no-op. Poll readiness does not depend on this compatibility call. It does not
inspect the socket or alter an existing timeout, and an expired deadline is
never used to simulate non-blocking behavior.

The latest successfully completed `settimeout` or `setblocking(..., true)`
call determines the persistent mode. A rejected call leaves that mode
unchanged. Configuration belongs to the shared network resource, so related
VMs observe the same mode.

For effective configuration calls, a socket may be the existing map-backed
`Socket` value or a typed `Socket` instance. The `fd` field must exist, be an
exact `int`, fit the platform descriptor type without narrowing, resolve to a
listener before a connected socket, and name an open resource. `fd` is
authoritative; mutable `open`, `addr`, and `port` fields are not trusted.
`settimeout` validates exact arity, timeout type and range, socket shape and
descriptor, lookup, then open state. `setblocking` retains its compatibility
behavior for malformed arity/type; only the exact `true` branch performs
socket validation.

#### Non-consuming readiness polling

`net.poll(read, write, error, timeout_ms)` (the public wrapper for
`net_select`) observes readiness without performing I/O. The three input sets
are independent fixed arrays with 64 entries each. For each set, only entries
0 through 63 are considered. The corresponding output is another 64-entry
array in stable input order; unused output entries are null. Duplicate socket
occurrences are retained as separate output occurrences. `read_count`,
`write_count`, and `error_count` are the exact numbers of values copied to
their respective outputs, not the number of distinct resources.

`timeout_ms` must be an `int` representable as Go `time.Duration`
milliseconds. A negative value is an error. Zero performs one immediate poll.
A positive value supplies one global wall-clock bound for the whole operation,
including every candidate in all three sets; it is not a per-socket or
per-set timeout. Poll may return earlier when readiness or a local concurrent
close is observed. With no live candidates, a positive poll waits for the same
global bound and returns empty sets.

Poll is strictly observational. It does not call `Read` or `Accept`, peek at
payload bytes, query or clear `SO_ERROR`, or install, clear, or restore a
socket deadline. It therefore neither consumes bytes nor accepts a pending
connection, and it does not mutate the persistent mode established by
`settimeout` or `setblocking(..., true)`.

For a connected socket, ordinary payload data is read readiness only. An
orderly EOF is always readable. Explicit native hangup, error, or invalid
descriptor events satisfy every set in which that connected socket was
requested. Payload data may coexist with EOF, hangup, or error readiness; poll
must not discard it, and a later `socket_recv` can still consume the pending
bytes. Out-of-band or urgent data is outside the `net.poll` API and has no
separate readiness category.

A listening socket with a pending connection has normal read readiness only.
A listener terminal event satisfies requested read and error sets, but a
listener is never returned in the write set. Poll never calls `Accept`, so a
pending connection remains available to a later explicit `net.accept`.

Closing a requested socket or listener concurrently in the same runtime wakes
a blocked positive poll. The detached resource is revalidated and omitted
from every output set; stale descriptor reuse cannot make the old resource
ready.

Windows and Linux receive runtime verification. The poller backend has compile
support for Windows and for `aix`, `darwin`, `dragonfly`, `freebsd`, `linux`,
`netbsd`, `openbsd`, and `solaris`; this is compile support, not a claim of
runtime verification on those Unix targets. Full-repository cross-build gates
currently cover `linux/amd64`, `darwin/amd64`, and `freebsd/amd64`; existing
SQLite dependencies prevent full-repository builds for the other listed Unix
targets even though the poller backend itself compiles there. On any platform
outside the Windows/Unix build-tag list, polling a live resource fails with
`network polling is not supported on this platform`; no consuming fallback is
used.

Accepted and connected sockets explicitly clear prior deadlines before being
registered and start in indefinite blocking mode; a listener timeout is not
inherited. Failure to clear that initial deadline closes the connection and
returns the existing `Socket{open:false}` result. Listener accept timeout also
uses that existing result shape.

Read and write expiry is reported as `"operation timed out"` using portable
deadline-error classification. A receive that obtained bytes before an error
remains successful with those bytes. A failed partial send reports `ok=false`
and its actual transferred `count`. If a multi-step configuration cannot
restore its exact prior absolute deadline snapshots after failure, the runtime
marks the shared resource closed, detaches its OS handles and buffers, and
closes them rather than leaving pending I/O in an unknown deadline state. The
poisoned entry remains invalid in the shared registry until ordinary
`net_close` removes the handle. If concurrent close invalidates a deadline
transition, that close owns cleanup; the stale transition cannot commit,
register, roll back, poison, or close the resource again.

---

## 13. Implementation Notes

- **VM**: Stack-based Virtual Machine.
- **Language**: Go.
- **Compilation**: Source (.nx) -> Bytecode (Chunk).
- **Execution**: The VM executes the bytecode instructions.

### Memory Model
- **Value Types**: Primitives (`int`, `float`, `bool`) are stored directly on the stack.
- **Heap-Backed Composite Types**: Objects (`struct`, `array`, `map`) are allocated on the heap.
    - **Variables**: Store a pointer to the heap object.
    - **Assignment/Casting**: Within the same scope, assigning a composite value copies its pointer, so both variables refer to the same object.
    - **Function Calls**: A parameter without `ref` receives a **shallow copy** of the top-level composite container. Nested composite values remain shared.
    - **Reference Parameters**: A parameter declared with `ref` receives access to the caller's original value and no shallow copy is performed.

---
*Version: 0.2.0*
*Language: Noxy*
*Implementation: Stack VM (Go)*

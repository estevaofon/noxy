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
| Declarations | `let`, `global`, `func`, `struct` |
| Control Flow | `if`, `elif`, `then`, `else`, `end`, `while`, `do`, `return`, `break`, `for`, `in` |
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

The `ref` operator creates a reference (pointer) to an existing variable.

#### L-Value Requirement
You can **ONLY** take a reference of an **addressable value** (L-Value). This means the operand must be a variable, a struct field, or an array/map index.
**You CANNOT take a reference of a temporary value (R-Value), such as a function call result or a literal.**

Captured variables are addressable through their upvalue storage. Non-null
literals and plain function-result temporaries are not addressable. A function
result whose declared type is already `ref T` is a reference value and may be
passed directly. `null` remains the explicit nullable `ref T` value: it is
accepted without pretending that it owns a storage slot.

**Correct Usage:**
```noxy
let err: Error = Error("msg")
let r: ref Error = ref err      // OK: 'err' is a variable
```

**Incorrect Usage:**
```noxy
let r: ref Error = ref Error("msg") // ERROR: Cannot take reference of temporary value
```

### 2.3 Reference Semantics (`ref`)
The `ref` keyword allows creating pointers to existing values. Noxy unifies reference usage through **"Automatic Dereference"** and **"Type-Based Assignment"**.

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
global name: type = value
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
*Version: 1.4.0*
*Language: Noxy*
*Implementation: Stack VM (Go)*

# Noxy Language Specification

## Overview

Noxy is a statically typed programming language. Designed for educational purposes and practical applications, it supports structs, references, arrays, f-strings, and a module system.
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

#### Static and Immutable Typing

Noxy is a **statically typed** language with **immutable types**:

1. **The type of a variable is defined at declaration and can NEVER be changed.**
2. Attempts to assign a value of a different type result in a compilation error.
3. There is no implicit conversion between types (except where explicitly documented).

```noxy
let x: int = 42
x = 100          // ✓ OK - same type (int)
x = 3.14         // ✗ ERROR - cannot assign float to int variable
x = "text"       // ✗ ERROR - cannot assign string to int variable
```

#### Compile-Time Type Checking

- All type errors are detected **before** execution.
- The compiler checks compatibility in assignments, function calls, and operations.

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
Arrays are passed by **VALUE** (Copy) by default. To modify the original array in a function, use `ref`.

#### Maps (Hashmaps)

```noxy
// Type: map[Key, Value]
let scores: map[string, int] = {"Alice": 100}
scores["Bob"] = 50
```

**Pass-by-Value Behavior**:
Maps are passed by **VALUE** (Copy) by default. To modify the original map in a function, use `ref`.

#### Structs

```noxy
struct Person
    name: string,
    age: int
end
```

**Pass-by-Value Behavior**:
Structs are passed by **VALUE** (Copy) by default. A deep copy is performed. To pass by reference (so modifications affect the original), use `ref`.

---
### 2.3 The `ref` Operator

The `ref` operator creates a reference (pointer) to an existing variable.

#### L-Value Requirement
You can **ONLY** take a reference of an **addressable value** (L-Value). This means the operand must be a variable, a struct field, or an array/map index.
**You CANNOT take a reference of a temporary value (R-Value), such as a function call result or a literal.**

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
    val = val * 2  // UPDATE: writes to original variable
end

func swap(a: ref int, b: ref int)
    let val_a: int = a  // Read values (auto-deref)
    let val_b: int = b
    *a = val_b          // UPDATE: write to address of 'a' using '*'
    *b = val_a          // UPDATE: write to address of 'b' using '*'
end

let x: int = 10
double_it(ref x)  // x is now 20

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

### 4.2 Parameter Passing Semantics (CRITICAL)

Noxy uses **Pass-by-Value** by default for ALL types, including composite types (Arrays, Maps, Structs).

#### Pass-by-Value (Default)
When a variable is passed to a function, a **COPY** is made. Modifications inside the function **DO NOT** affect the original variable.

```noxy
func modify(arr: int[]) -> void
    append(arr, 999) // Modifies local copy only
end

let list: int[] = [1, 2, 3]
modify(list)
// list is still [1, 2, 3]
```

#### Pass-by-Reference (`ref`)
To allow a function to modify the original variable, you must explicitly use the `ref` keyword in the parameter type.

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
    value: int,
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
- **Reference Types**: Objects (`struct`, `array`, `map`) are allocated on the heap.
    - **Variables**: Store a pointer to the heap object.
    - **Assignment/Casting**: By default, assigns the pointer (reference copy) for variables within the same scope.
    - **Function Calls**: Performs a **Deep Copy** (or Shallow Copy of top-level container) to ensure Pass-by-Value semantics, unless `ref` is specified.

---
*Version: 1.2*
*Language: Noxy*
*Implementation: Stack VM (Go)*

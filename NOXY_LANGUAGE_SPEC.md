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
| Control Flow | `if`, `elif`, `then`, `else`, `end`, `while`, `do`, `return`, `break` |
| Types | `int`, `float`, `string`, `str`, `bool`, `void`, `ref`, `bytes` |
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
- `zeros(n)`: create zeroed array.
- `fmt(format, args...)`: printf-style formatting.

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

## 12. Implementation Notes

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

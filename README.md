[![noxy 0.14.1](https://img.shields.io/badge/noxy-0.14.1-blue)](CHANGELOG.md)

# Noxy VM 🚀

A complete bytecode virtual machine for the **Noxy** programming language, written in Go. [Official Website.](https://noxylang.com/)

For a complete guide, consult the [NOXY_LANGUAGE_SPEC.md](docs/NOXY_LANGUAGE_SPEC.md).
For real projects built with the language, see the [Showcase](docs/SHOWCASE.md).

<p align="center">
<img width="200" height="200" alt="noxy" src="https://github.com/user-attachments/assets/acfc226e-f129-43ed-97df-25dda7c97fcf" />
</p>

## What is Noxy VM?

Noxy VM is a bytecode compiler and virtual machine for the Noxy language created by Estêvão Fonseca. This implementation compiles source code into bytecode and executes it on a stack-based VM, offering high performance.

Noxy is statically typed, with explicit dynamic boundaries through `any`, bare
`func`, untyped native primitives, and plugins without signatures. Its
variables are type-stable: the declared type does not change, while values may
be mutable.

### The Zen of Noxy

The philosophy that guides the language and its future decisions — short by
design, in the spirit of the Zen of Python: a compass, not a rulebook. It also
opens the [language spec](docs/NOXY_LANGUAGE_SPEC.md#the-zen-of-noxy).

```text
Simplicity is sophistication.
Typing is safety — and the compiler speaks first.
Dynamic exists, but it is explicit: any says what it is.
Variables are copies, unless explicitly stated otherwise.
Sharing is ref. There is no other way.
CoW + ref is one heck of a duo!
An error is a value, not an exception.
One rule, everywhere: file, module, REPL.
Consistency comes before performance.
Performance is measured afterwards — without changing semantics.
Lean core, vast ecosystem.
Fixing beats staying compatible, until 1.0 says otherwise.
```

### Features

- ✅ Bytecode compiler
- ✅ High-performance stack-based VM
- ✅ Primitive types: `int`, `float`, `string`, `bool`, `bytes`
- ✅ Structs with typed fields (global and local scope)
- ✅ Dynamic arrays with `append`, `pop`, `contains`
- ✅ Maps (hashmaps) with literals `{key: value}`
- ✅ Functions with recursion
- ✅ Reference system (`ref`)
- ✅ F-strings with interpolation
- ✅ Single and double quote support
- ✅ Line tracking for debugging
- ✅ SQLite database support (Thread-safe)
- ✅ HTTP server support
- ✅ Value semantics with copy-on-write (composites are independent values; `ref` is the only sharing mechanism)
- ✅ First-class functions
- ✅ Closures
- ✅ Generics with zero runtime cost (monomorphization: `func first<T>(arr: T[]) -> T`, `struct Stack<T>`, always inferred from usage)
- ✅ Concurrency (noxy routines) [docs/CONCURRENCY.md](docs/CONCURRENCY.md)
- ✅ Garbage collection
- ✅ Built-in modules (io, net, http, sqlite)
- ✅ Package manager (see [docs/PACKAGE_MANAGER.md](docs/PACKAGE_MANAGER.md))

## Installation

```bash
# Clone the repository
git clone https://github.com/estevaofon/noxy.git
cd noxy-vm

# Build
go build -o noxy ./cmd/noxy

# Or run directly
go run ./cmd/noxy/main.go file.nx
```

## Usage

```bash
# Run a Noxy program
./noxy program.nx

# Or with go run
go run ./cmd/noxy/main.go program.nx

# Start Interactive REPL
./noxy

# Diagnostics go to stderr — redirect to capture them with the program output
./noxy program.nx > out.txt 2>&1
```

The program's own output (`print`, `iprint`) goes to **stdout**; everything the
VM/CLI reports — parser, compiler and runtime errors, hints, "Error reading
file" — goes to **stderr**. A failing run (including a missing script file)
exits with code `1`.

## Interactive REPL

Noxy includes a powerful REPL (Read-Eval-Print Loop) for interactive coding. Just run `noxy` without arguments.

```noxy
Noxy REPL v0.14.1
Type 'exit' to quit.
>>> let x: int = 10
>>> x + 5
15
>>> if true then
...     print("Multiline support!")
... end
Multiline support!
```

On Linux and macOS the REPL edits the line in place: ←/→ move the cursor,
↑/↓ walk the session history, Home/End, Ctrl-A/E/K/U/W/L behave as in
readline, Ctrl-C discards the line (and the block being typed) and Ctrl-D
exits. On Windows the console provides the same through its own line editing.

## Quick Example

```noxy
func main()
    let x: int = 10
    let y: int = 20
    print(f"Sum: {x + y}")

    struct Person
        name: string
        age: int
    end

    let p: Person = Person("Ana", 25)
    print(p.name)

    // Dynamic arrays
    let nums: int[] = []
    append(nums, 1)
    append(nums, 2)
    print(f"Length: {length(nums)}")

    // Maps
    let scores: map[string, int] = {"Alice": 100, "Bob": 95}
    print(f"Alice: {scores['Alice']}")
end
main()
```

Output:
```
Sum: 30
Ana
Length: 2
Alice: 100
```

## Testing
 
How to run the interpreter tests:
 
```bash
# Run all unit tests (Lexer, Parser, Compiler, VM)
go test ./...
 
# Run integration tests (Noxy scripts)
go run cmd/noxy/main.go noxy_examples/run_all_tests_concurrent.nx
```
 
## Architecture

```
noxy-vm/
├── cmd/noxy/main.go      # Main CLI
├── internal/
│   ├── lexer/            # Tokenization
│   ├── token/            # Token types
│   ├── parser/           # Recursive descent parser → AST
│   ├── ast/              # AST nodes
│   ├── compiler/         # AST → Bytecode Compiler
│   ├── chunk/            # Bytecode and operations
│   ├── value/            # Value system (int, float, string, etc.)
│   └── vm/               # Stack-based virtual machine
```

```mermaid
flowchart TB
    subgraph INPUT["📄 SOURCE"]
        A[("program.nx")]
    end

    subgraph FRONTEND["🔍 FRONTEND"]
        direction TB
        B["🔤 <b>LEXER</b><br/><i>Tokenization</i><br/><code>let, func, if → Tokens</code>"]
        C["🌳 <b>PARSER</b><br/><i>Syntax Analysis</i><br/><code>Tokens → AST</code>"]
    end

    subgraph BACKEND["⚙️ BACKEND"]
        direction TB
        D["📦 <b>COMPILER</b><br/><i>Code Generation</i><br/><code>AST → Bytecode</code>"]
        E["💾 <b>CHUNK</b><br/><i>Bytecode Storage</i><br/><code>OpCodes + Constants</code>"]
    end

    subgraph RUNTIME["🚀 RUNTIME"]
        direction TB
        F["🖥️ <b>VIRTUAL MACHINE</b><br/><i>Stack-Based Execution</i><br/><code>Interpret Bytecode</code>"]
        G["📚 <b>STDLIB</b><br/><i>Native Modules</i><br/><code>io, net, http, sqlite...</code>"]
    end

    subgraph OUTPUT["✨ RESULT"]
        H[("Execution<br/>Output")]
    end

    A ==> B
    B ==> C
    C ==> D
    D ==> E
    E ==> F
    G <-.-> F
    F ==> H
```

## Data Types

### Primitives
```noxy
let x: int = 42
let pi: float = 3.14159
let name: string = "Noxy"
let active: bool = true
let data: bytes = b"hello"
```

### Dynamic Arrays
```noxy
let nums: int[] = []
append(nums, 10)
append(nums, 20)
print(length(nums))     // 2
print(pop(nums))        // 20
print(contains(nums, 10)) // true
```

### Maps
```noxy
let scores: map[string, int] = {"Alice": 100, "Bob": 95}
scores["Charlie"] = 88
print(has_key(scores, "Alice"))  // true
print(scores["Alice"])           // 100
```

### Bytes
```noxy
let b: bytes = b"hello"
print(b[0])  // 104 (ASCII 'h')

let from_str: bytes = to_bytes("text")
let from_int: bytes = to_bytes(65)  // b"A"
```

### Generics

Generic functions and structs are monomorphized at compile time — zero
runtime cost, always instantiated by inference (no explicit `first<int>(x)`
syntax). See [NOXY_LANGUAGE_SPEC.md §6](docs/NOXY_LANGUAGE_SPEC.md) for the
full contract.

```noxy
struct Stack<T>
    items: T[]
end

func push<T>(s: ref Stack<T>, item: T)
    append(s.items, item)
end

func peek<T>(s: Stack<T>) -> T
    return s.items[length(s.items) - 1]
end

let ints: Stack<int> = Stack([])   // T inferred from the `let` annotation
push(ref ints, 10)
push(ref ints, 20)
print(peek(ints))  // 20
```

## Builtin Functions

| Function | Description |
|--------|-----------|
| `print(expr)` | Prints value to stdout |
| `eprint(expr)` | Prints value to stderr |
| `input(prompt)` | Reads one line from stdin (`""` at end of input) |
| `fmt(format, args...)` | printf-style formatting (`%s`, `%d`, `%.2f`, ...) |
| `to_str(val)` | Converts to string |
| `length(arr)` | Length of array/string |
| `append(arr, val)` | Appends element to array |
| `pop(arr)` | Removes and returns last element |
| `contains(arr, val)` | Checks if value exists |
| `has_key(map, key)` | Checks if key exists in map |
| `to_bytes(val)` | Converts string/int/array to bytes |
| `zeros(n)` | Array of n zeros |
| `range(stop)`, `range(start, stop, step)` | Integer sequence as `int[]` (Python semantics, no import) |
| `time_now()` | Current timestamp in ms |

## VM Opcodes

The VM uses the following main opcodes:

| Opcode | Description |
|--------|-----------|
| `OP_CONSTANT` | Loads constant |
| `OP_ADD/SUB/MUL/DIV` | Arithmetic operations |
| `OP_EQUAL/LESS/GREATER` | Comparisons |
| `OP_JUMP/JUMP_IF_FALSE` | Flow control |
| `OP_CALL/RETURN` | Function calls |
| `OP_ARRAY/OP_MAP` | Collection creation |
| `OP_GET_INDEX/SET_INDEX` | Index access |

## Disassembly

The compiler generates bytecode that can be visualized:

```
== main ==
0000    1 OP_CONSTANT         0 '<fn main>'
0002    | OP_SET_GLOBAL       1 'main'
0004    | OP_POP
0005    | OP_GET_GLOBAL       2 'main'
0007    | OP_CALL             0

== main ==
0000    3 OP_CONSTANT         0 '10'
0002    | OP_CONSTANT         1 '20'
0004    5 OP_GET_LOCAL        1
...
```

## Performance

The bytecode VM offers high performance, especially for:
- Intensive loops
- Recursive function calls
- Operations with large arrays

## License

MIT License

---

*Bytecode implementation of the Noxy language in Go.*

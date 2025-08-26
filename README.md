# Noxy

A statically-typed programming language with LLVM backend compilation, featuring advanced data structures and algorithms. [Visit the Official Website](https://estevaofon.github.io/noxy/)

<p align="center">
  <img width="300" height="300" alt="ChatGPT Image 24 de ago  de 2025, 10_52_55" src="https://github.com/user-attachments/assets/8af825b7-fc42-4e0b-8aab-da9bba99b6e0" />
</p>

## Overview

Noxy is a powerful programming language designed for educational purposes and practical applications. It features static typing, LLVM-based compilation, and supports advanced programming constructs including structs, auto-referencing, dynamic assignments, and complex algorithms.

## Features

### Core Language Features
- **Static Type System**: Supports `int`, `float`, `string`, `bool`, and array types
- **LLVM Backend**: Compiles to native machine code using LLVM
- **Functions**: Define and call functions with typed parameters and return values
- **Arrays**: Fixed-size arrays with type safety and automatic conversion to pointers
- **Control Flow**: `if-else` statements and `while` loops
- **Type Casting**: Universal `to_str` function and numeric conversions
- **String Operations**: Concatenation and manipulation
- **Built-in Functions**: `print`, `length`, `strlen`, and conversion functions

### Advanced Features
- **Structs**: User-defined composite data types with constructors
- **Auto-Reference**: Self-referencing structs using `ref` operator
- **Dynamic Assignment**: Modify struct fields at runtime
- **Complex Algorithms**: Support for binary search, linked lists, trees, and graphs

## Installation

### Prerequisites

- Python 3.13 or higher
- LLVM (the project uses llvmlite for LLVM bindings)
- GCC or compatible C compiler

### Setup

1. Clone the repository:
```bash
git clone <repository-url>
cd noxy
```

2. Install dependencies using uv:
```bash
uv sync
```

## Usage

### Compiling and Running Programs

First, compile the source code into an object file using the Noxy compiler:

```bash
uv run python compiler.py --compile <source_file.nx>
```

Then link the generated object file with the casting functions written in C:

```bash
gcc -o programa.exe output.obj casting_functions.c
```

Run the executable:

```bash
./programa.exe
```

### Running Test Suite

Execute the comprehensive test suite:

```bash
uv run python compiler.py --compile testes_unitarios_automatizados.nx
gcc -o testes.exe output.obj casting_functions.c
./testes.exe
```

## Language Syntax

### Variable Declarations

```noxy
let x: int = 42
let y: float = 3.14
let texto: string = "Hello, World!"
let ativo: bool = true
```

### Arrays

```noxy
let numeros: int[5] = [1, 2, 3, 4, 5]
let matriz: float[3] = [1.1, 2.2, 3.3]
let vazio: int[0] = []

// Get array size using length function
let tamanho: int = length(numeros)  // Returns 5
```

### Structs

```noxy
struct Pessoa
    nome: string,
    idade: int,
    ativo: bool
end

// Create struct with constructor
let pessoa: Pessoa = Pessoa("João", 25, true)

// Dynamic field assignment
pessoa.idade = 26
pessoa.nome = "João Silva"
```

### Auto-Referencing Structs

```noxy
struct TreeNode
    valor: int,
    esquerda: ref TreeNode,
    direita: ref TreeNode
end

// Create nodes
let raiz: TreeNode = TreeNode(50, null, null)
let filho_esq: TreeNode = TreeNode(25, null, null)

// Link nodes
raiz.esquerda = ref filho_esq
```

### Functions

```noxy
func add(a: int, b: int) -> int
    return a + b
end

func busca_binaria(arr: int[], tamanho: int, valor: int) -> int
    let inicio: int = 0
    let fim: int = tamanho - 1
    
    while inicio <= fim do
        let meio: int = (inicio + fim) / 2
        
        if arr[meio] == valor then
            return meio
        end
        
        if arr[meio] < valor then
            inicio = meio + 1
        else
            fim = meio - 1
        end
    end
    
    return -1
end
```

### Control Flow

```noxy
if x > 10 then
    print("x is greater than 10")
else
    print("x is 10 or less")
end

while i < 10 do
    print(i)
    i = i + 1
end
```

### Type Casting

Noxy provides powerful type conversion functions centered around the universal `to_str` function:

```noxy
// Converting integers to string
let num: int = 42
let str_from_int: string = to_str(num)  // "42"

// Converting floats to string
let valor: float = 3.14159
let str_from_float: string = to_str(valor)  // "3.141590"

// Converting arrays to string representation
let numeros: int[5] = [1, 2, 3, 4, 5]
let str_from_array: string = to_str(numeros)  // "[1, 2, 3, 4, 5]"

let floats: float[3] = [1.1, 2.2, 3.3]
let str_from_float_array: string = to_str(floats)  // "[1.100000, 2.200000, 3.300000]"

// Other numeric conversions
let int_from_float: int = to_int(valor)     // 3 (truncates)
let float_from_int: float = to_float(num)   // 42.000000
```

### String Operations

```noxy
let str1: string = "Hello"
let str2: string = "World"
let result: string = str1 + " " + str2  // String concatenation
```

### Quick Start Example

Create a file `hello.nx`:

```noxy
print("Hello from Noxy!")

// Basic operations
let x: int = 10
let y: int = 20
print("Sum: ")
print(x + y)

// Struct example
struct Produto
    codigo: int,
    nome: string,
    preco: float
end

let produto: Produto = Produto(1, "Laptop", 2500.50)
print("Product: ")
print(produto.nome)
```

Run it:
```bash
uv run python compiler.py --compile hello.nx
gcc -o hello.exe output.obj casting_functions.c
./hello.exe
```

## Data Types & Operators

### Supported Types
- **int**: 64-bit integers
- **float**: Double-precision floating-point
- **string**: Null-terminated character arrays
- **bool**: Boolean values (true/false)
- **Arrays**: Fixed-size arrays (e.g., `int[5]`)
- **Structs**: User-defined composite types
- **ref**: Reference types for auto-referencing

### Operators
- **Arithmetic**: `+`, `-`, `*`, `/`, `%`
- **Comparison**: `>`, `<`, `>=`, `<=`, `==`, `!=`
- **Logical**: `&`, `|`, `!`
- **Assignment**: `=`
- **String Concatenation**: `+`
- **Reference**: `ref`

### Built-in Functions
- `print(expression)` - Output to console
- `to_str(value)` - Convert to string representation
- `to_int(float)` - Convert float to integer
- `to_float(int)` - Convert integer to float
- `strlen(string)` - String length
- `length(array)` - Array size

### Using `ref` for Auto-References
Use `ref` for self-referencing structs, function parameters that need mutation, and pointer storage. See `REF_README.md` for detailed examples.

## Compilation Details

The compiler generates LLVM IR code that is then compiled to native machine code. The process includes:

1. **Lexical Analysis**: Breaks source code into tokens
2. **Syntax Analysis**: Builds AST from tokens
3. **Semantic Analysis**: Type checking and validation
4. **Code Generation**: LLVM IR generation with advanced features
5. **Optimization**: LLVM optimization passes
6. **Execution**: Native code execution

### Advanced Compilation Features

- **Automatic Array Conversion**: Static arrays automatically converted to pointers when passed to functions
- **Reference Handling**: Proper handling of `ref` types and auto-referencing
- **Struct Field Access**: Efficient access to struct fields with dynamic assignment
- **Type Safety**: Comprehensive type checking for all language constructs

## Contributing

This is an educational project showcasing advanced compiler design concepts. Feel free to explore the code, run examples, and experiment with the language features.

## License

This project is for educational purposes.

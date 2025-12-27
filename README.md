# Noxy VM 🚀

Uma máquina virtual bytecode completa para a linguagem de programação **Noxy**, escrita em Go.

<p align="center">
<img width="300" height="300" alt="Noxy Logo" src="https://github.com/user-attachments/assets/29244835-8d84-44ad-bfd2-fd2894feac3a" />
</p>

## O que é Noxy VM?

Noxy VM é um compilador bytecode e máquina virtual para a linguagem Noxy. Diferente do interpretador tree-walking em Python, esta implementação compila o código para bytecode e o executa em uma VM stack-based, oferecendo melhor performance.

### Características

- ✅ Compilador para bytecode
- ✅ VM stack-based de alta performance
- ✅ Tipos primitivos: `int`, `float`, `string`, `bool`, `bytes`
- ✅ Structs com campos tipados (escopo global e local)
- ✅ Arrays dinâmicos com `append`, `pop`, `contains`
- ✅ Maps (hashmaps) com literais `{key: value}`
- ✅ Funções com recursão
- ✅ Sistema de referências (`ref`)
- ✅ F-strings com interpolação
- ✅ Suporte a aspas simples e duplas
- ✅ Rastreamento de linhas para debug

## Instalação

```bash
# Clone o repositório
git clone <repo-url>
cd noxy-vm

# Compile
go build -o noxy ./cmd/noxy

# Ou execute diretamente
go run ./cmd/noxy/main.go arquivo.nx
```

## Uso

```bash
# Executar um programa Noxy
./noxy programa.nx

# Ou com go run
go run ./cmd/noxy/main.go programa.nx
```

## Exemplo Rápido

```noxy
func main()
    let x: int = 10
    let y: int = 20
    print(f"Soma: {x + y}")

    struct Pessoa
        nome: string
        idade: int
    end

    let p: Pessoa = Pessoa("Ana", 25)
    print(p.nome)

    // Arrays dinâmicos
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

Saída:
```
Soma: 30
Ana
Length: 2
Alice: 100
```

## Arquitetura

```
noxy-vm/
├── cmd/noxy/main.go      # CLI principal
├── internal/
│   ├── lexer/            # Tokenização
│   ├── token/            # Tipos de tokens
│   ├── parser/           # Parser recursive descent → AST
│   ├── ast/              # Nós da AST
│   ├── compiler/         # Compilador AST → Bytecode
│   ├── chunk/            # Bytecode e operações
│   ├── value/            # Sistema de valores (int, float, string, etc.)
│   └── vm/               # Máquina virtual stack-based
```

## Tipos de Dados

### Primitivos
```noxy
let x: int = 42
let pi: float = 3.14159
let nome: string = "Noxy"
let ativo: bool = true
let dados: bytes = b"hello"
```

### Arrays Dinâmicos
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

## Funções Builtin

| Função | Descrição |
|--------|-----------|
| `print(expr)` | Imprime valor |
| `to_str(val)` | Converte para string |
| `length(arr)` | Tamanho do array/string |
| `append(arr, val)` | Adiciona elemento ao array |
| `pop(arr)` | Remove e retorna último elemento |
| `contains(arr, val)` | Verifica se valor existe |
| `has_key(map, key)` | Verifica se chave existe no map |
| `to_bytes(val)` | Converte string/int/array para bytes |
| `zeros(n)` | Array de n zeros |
| `time_now()` | Timestamp atual em ms |

## Opcodes da VM

A VM utiliza os seguintes opcodes principais:

| Opcode | Descrição |
|--------|-----------|
| `OP_CONSTANT` | Carrega constante |
| `OP_ADD/SUB/MUL/DIV` | Operações aritméticas |
| `OP_EQUAL/LESS/GREATER` | Comparações |
| `OP_JUMP/JUMP_IF_FALSE` | Controle de fluxo |
| `OP_CALL/RETURN` | Chamadas de função |
| `OP_ARRAY/OP_MAP` | Criação de coleções |
| `OP_GET_INDEX/SET_INDEX` | Acesso a índices |

## Disassembly

O compilador gera bytecode que pode ser visualizado:

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

A VM bytecode oferece melhor performance que o interpretador tree-walking Python, especialmente para:
- Loops intensivos
- Chamadas de função recursivas
- Operações com arrays grandes

## Licença

MIT License

---

*Implementação bytecode da linguagem Noxy em Go.*

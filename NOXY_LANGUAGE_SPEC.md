# Noxy Language Specification

## Visão Geral

Noxy é uma linguagem de programação estaticamente tipada. Projetada para fins educacionais e aplicações práticas, suporta structs, referências, arrays, f-strings e sistema de módulos.

---

## 1. Estrutura Léxica

### 1.1 Comentários

```noxy
// Comentário de linha única
```

### 1.2 Palavras-chave

| Categoria | Palavras-chave |
|-----------|----------------|
| Declarações | `let`, `global`, `func`, `struct` |
| Controle de Fluxo | `if`, `elif`, `then`, `else`, `end`, `while`, `do`, `return`, `break` |
| Tipos | `int`, `float`, `string`, `str`, `bool`, `void`, `ref` |
| Literais | `true`, `false`, `null` |
| Módulos | `use`, `select` |
| Especiais | `zeros` |

### 1.3 Operadores

| Categoria | Operadores |
|-----------|------------|
| Aritméticos | `+`, `-`, `*`, `/`, `%` |
| Comparação | `>`, `<`, `>=`, `<=`, `==`, `!=` |
| Lógicos | `&&`, `||`, `!` |
| Bitwise | `&`, `|`, `^`, `~`, `<<`, `>>` (Int e Bytes) |
| Atribuição | `=` |
| Referência | `ref` |
| Retorno de função | `->` |

### 1.4 Delimitadores

| Símbolo | Uso |
|---------|-----|
| `(` `)` | Parênteses para expressões e chamadas de função |
| `[` `]` | Colchetes para arrays e indexação |
| `{` `}` | Chaves para interpolação em f-strings |
| `,` | Separador de parâmetros e elementos |
| `:` | Separador de tipo em declarações |
| `.` | Acesso a campos de struct |

---

## 2. Sistema de Tipos

### 2.0 Regras Fundamentais de Tipagem

#### Tipagem Estática e Imutável

Noxy é uma linguagem **estaticamente tipada** com **tipos imutáveis**:

1. **O tipo de uma variável é definido na declaração e NUNCA pode ser alterado**
2. Tentativas de atribuir um valor de tipo diferente resultam em erro de compilação
3. Não existe conversão implícita entre tipos (exceto onde explicitamente documentado)

```noxy
let x: int = 42
x = 100          // ✓ OK - mesmo tipo (int)
x = 3.14         // ✗ ERRO - não pode atribuir float a variável int
x = "texto"      // ✗ ERRO - não pode atribuir string a variável int

let nome: string = "Ana"
nome = "Bruno"   // ✓ OK - mesmo tipo (string)
nome = 123       // ✗ ERRO - não pode atribuir int a variável string

let pessoa: Pessoa = Pessoa("João", 25)
pessoa = Pessoa("Maria", 30)  // ✓ OK - mesmo tipo (Pessoa)
pessoa = Endereco("Rua A", 10)  // ✗ ERRO - tipos diferentes
```

#### Verificação de Tipos em Tempo de Compilação

- Todos os erros de tipo são detectados **antes** da execução
- O compilador verifica compatibilidade em atribuições, chamadas de função e operações
- Operadores exigem tipos compatíveis nos operandos

```noxy
// Operadores exigem tipos compatíveis
let a: int = 5 + 3       // ✓ OK - int + int = int
let b: float = 1.5 + 2.5 // ✓ OK - float + float = float
let c: int = 5 + 1.5     // ✗ ERRO - int + float não permitido
let d: string = "a" + "b" // ✓ OK - concatenação de strings

// Comparações exigem tipos compatíveis
if x > 10 then ...       // ✓ OK se x é int
if x > 1.5 then ...      // ✗ ERRO se x é int (comparando int com float)
```

### 2.1 Tipos Primitivos

| Tipo | Descrição | Exemplo |
|------|-----------|---------|
| `int` | Inteiro de 64 bits | `42`, `-10`, `0` |
| `float` | Ponto flutuante de dupla precisão | `3.14`, `-0.5`, `1.0` |
| `string` | Cadeia de caracteres | `"Hello"`, `""` |
| `bool` | Valor booleano | `true`, `false` |
| `void` | Ausência de valor (somente retorno de função) | - |
| `bytes` | Sequência de bytes bruta | `b"Dados"`, `hex_decode("FF")` |

### 2.2 Tipos Compostos

#### Arrays (Dinâmicos e Fixos)

**1. Arrays Dinâmicos (Recomendado)**
```noxy
// Declaração (inicia vazio)
let dinamico: int[] 

// Operações
append(dinamico, 10)
append(dinamico, 20)
let ultimo: int = pop(dinamico)
let tam: int = length(dinamico)

// Verificação
if contains(dinamico, 10) then
    print("Encontrado")
end

// Passagem por Valor (Cópia)
// Ao passar para funçao, é feita uma CÓPIA profunda.
// Use 'ref int[]' para permitir modificação do original.
func add(arr: ref int[], val: int) -> void
    append(arr, val)
end
```

**2. Arrays de Tamanho Fixo**
```noxy
// Tamanho definido na declaração (Legado/Otimização)
let fixo: int[5] = [1, 2, 3, 4, 5]
let zerado: int[100] = zeros(100)
```

#### Mapas (Hashmaps)

```noxy
// Tipo: map[Chave, Valor]
// Chaves suportadas: string, int, bool (tipos imutáveis)

// Declaração vazio
let pontuacao: map[string, int] 

// Inicialização com literal
let config: map[string, bool] = {"ativo": true, "debug": false}

// Operações
pontuacao["Alice"] = 100        // Inserção/Atualização
let valor: int = pontuacao["Alice"] // Acesso
delete(pontuacao, "Alice")      // Remoção

// Verificação
if has_key(pontuacao, "Bob") then
    print("Bob existe")
end

let chaves: string[] = keys(pontuacao) // Retorna array de chaves
let tam: int = length(pontuacao)       // Retorna número de entradas

// Passagem por Valor (Cópia)
// Mapas são COPIADOS ao serem passados para funções (sem ref).
// Use 'ref map[K,V]' para modificar o original.
```

#### Structs
```noxy
struct NomeDoStruct
    campo1: tipo1,
    campo2: tipo2,
    campo3: tipo3
end
```

#### Referências
```noxy
// Campo de referência (para auto-referência)
struct Node
    valor: int,
    proximo: ref Node
end

// Variável de referência
let ptr: ref Node = null
ptr = ref algumNode

// Parâmetro por referência
func modificar(node: ref Node) -> void
    node.valor = 100
end

// Retorno por referência
func buscar(head: ref Node) -> ref Node
    return head.proximo
end
```

---

## 3. Declarações de Variáveis

### 3.1 Variáveis Locais
```noxy
let nome: tipo = valor

// Exemplos
let x: int = 42
let y: float = 3.14
let texto: string = "Hello"
let ativo: bool = true
let numeros: int[5] = [1, 2, 3, 4, 5]
```

### 3.2 Variáveis Globais
```noxy
global nome: tipo = valor

// Exemplos
global contador: int = 0
global pi: float = 3.14159
global mensagem: string = "Global"
```

### 3.3 Reatribuição

Variáveis podem ser reatribuídas, mas **o novo valor DEVE ser do mesmo tipo** que o declarado:

```noxy
// Reatribuição válida - mesmo tipo
let x: int = 42
x = 100              // ✓ OK - int para int

let texto: string = "Hello"
texto = "Novo valor" // ✓ OK - string para string

let numeros: int[5] = [1, 2, 3, 4, 5]
numeros[0] = 10      // ✓ OK - int para elemento int

let pessoa: Pessoa = Pessoa("Ana", 25)
pessoa = Pessoa("Bruno", 30)  // ✓ OK - Pessoa para Pessoa
pessoa.nome = "Carlos"        // ✓ OK - string para campo string
```

```noxy
// Reatribuição INVÁLIDA - tipos diferentes
let x: int = 42
x = 3.14             // ✗ ERRO DE COMPILAÇÃO - float não é int
x = "texto"          // ✗ ERRO DE COMPILAÇÃO - string não é int

let ativo: bool = true
ativo = 1            // ✗ ERRO DE COMPILAÇÃO - int não é bool
ativo = "true"       // ✗ ERRO DE COMPILAÇÃO - string não é bool
```

---

## 4. Funções

### 4.1 Definição de Função
```noxy
func nome(param1: tipo1, param2: tipo2) -> tipoRetorno
    // corpo da função
    return valor
end

// Função sem retorno (void pode ser omitido)
func saudar(nome: string) -> void
    print("Olá, " + nome)
end

// Função sem parâmetros
func obterValor() -> int
    return 42
end
```

### 4.2 Parâmetros de Array
```noxy
// Arrays são passados por VALOR (cópia) por padrão.
// Use 'ref' para modificar o array original na função.

func processar(arr: int[], tamanho: int) -> void
    let i: int = 0
    while i < tamanho do
        print(to_str(arr[i]))
        i = i + 1
    end
end
```

### 4.3 Semântica de Passagem de Parâmetros

#### Tipos Primitivos - Passagem por Valor
Tipos primitivos (`int`, `float`, `string`, `bool`) são **sempre passados por valor** (cópia).

```noxy
func dobrar(x: int) -> int
    x = x * 2    // modifica apenas a cópia local
    return x
end

let n: int = 5
let resultado: int = dobrar(n)
print(to_str(n))        // 5 (não foi modificado)
print(to_str(resultado)) // 10
```

#### Structs SEM ref - Passagem por Valor (Cópia)
Quando um struct é passado **sem** `ref`, uma **cópia completa** do struct é feita. Modificações dentro da função **NÃO afetam** o struct original.

```noxy
struct Contador
    valor: int
end

// Parâmetro SEM ref = cópia do struct
func incrementar_copia(c: Contador) -> void
    c.valor = c.valor + 1  // modifica apenas a CÓPIA
end

let meuContador: Contador = Contador(10)
incrementar_copia(meuContador)
print(to_str(meuContador.valor))  // 10 (NÃO foi modificado!)
```

#### Structs COM ref - Passagem por Referência
Quando um struct é passado **com** `ref`, uma **referência (ponteiro)** é passada. Modificações dentro da função **AFETAM** o struct original.

```noxy
// Parâmetro COM ref = referência ao struct original
func incrementar_ref(c: ref Contador) -> void
    c.valor = c.valor + 1  // modifica o struct ORIGINAL
end

let meuContador: Contador = Contador(10)
incrementar_ref(meuContador)
print(to_str(meuContador.valor))  // 11 (FOI modificado!)
```

#### Resumo: Quando Usar ref em Parâmetros

| Cenário | Usar `ref`? | Motivo |
|---------|-------------|--------|
| Ler campos do struct | Não | Cópia é suficiente |
| Modificar campos do struct | **Sim** | Sem ref, modifica apenas cópia |
| Struct muito grande (performance) | **Sim** | Evita cópia de dados |
| Struct com campos `ref` que serão modificados | **Sim** | Necessário para persistir mudanças |
| Primitivos que não precisam ser modificados | Não | Passagem por valor é suficiente |

```noxy
// Exemplo completo mostrando a diferença

struct Pessoa
    nome: string,
    idade: int
end

// SEM ref - não modifica o original
func fazer_aniversario_errado(p: Pessoa) -> void
    p.idade = p.idade + 1
    print(f"Dentro da função: {p.idade}")
end

// COM ref - modifica o original
func fazer_aniversario_correto(p: ref Pessoa) -> void
    p.idade = p.idade + 1
    print(f"Dentro da função: {p.idade}")
end

let joao: Pessoa = Pessoa("João", 25)

fazer_aniversario_errado(joao)
print(f"Após errado: {joao.idade}")     // 25 (não mudou)

fazer_aniversario_correto(joao)
print(f"Após correto: {joao.idade}")    // 26 (mudou!)
```

### 4.4 Parâmetros por Referência (Sintaxe)
```noxy
// Para modificar structs dentro de funções, use ref
func incrementar(contador: ref Contador) -> void
    contador.valor = contador.valor + 1
end
```

### 4.4 Funções Recursivas
```noxy
func fatorial(n: int) -> int
    if n <= 1 then
        return 1
    else
        return n * fatorial(n - 1)
    end
end
```

---

## 5. Structs

### 5.1 Definição
```noxy
struct Pessoa
    nome: string,
    idade: int,
    altura: float,
    ativo: bool
end
```

### 5.2 Construtor (Sintaxe de Chamada)
```noxy
// Construtor usa o nome do struct como função
let pessoa: Pessoa = Pessoa("João", 25, 1.75, true)
```

### 5.3 Acesso a Campos
```noxy
// Leitura
let nome: string = pessoa.nome
let idade: int = pessoa.idade

// Atribuição
pessoa.nome = "Maria"
pessoa.idade = 26
```

### 5.4 Structs Aninhados
```noxy
struct Endereco
    rua: string,
    numero: int
end

struct Funcionario
    pessoa: Pessoa,
    endereco: Endereco,
    salario: float
end

let func1: Funcionario = Funcionario(
    Pessoa("Ana", 30, 1.65, true),
    Endereco("Rua A", 100),
    5000.0
)

// Acesso multinível
print(func1.pessoa.nome)
print(to_str(func1.endereco.numero))

// Atribuição multinível
func1.pessoa.nome = "Ana Silva"
func1.endereco.cidade = "São Paulo"
```

### 5.5 Structs com Auto-referência
```noxy
struct Node
    valor: int,
    proximo: ref Node
end

// Criar nodes
let n1: Node = Node(10, null)
let n2: Node = Node(20, null)

// Conectar nodes (requer operador ref)
n1.proximo = ref n2

// Acessar através da referência
print(to_str(n1.proximo.valor))  // 20

// Comparar com null
if n1.proximo != null then
    print("Tem próximo")
end
```

### 5.6 Arrays de Structs
```noxy
let pessoas: Pessoa[3] = [
    Pessoa("Ana", 25, 1.65, true),
    Pessoa("Bruno", 30, 1.80, true),
    null
]

// Acesso a elemento e campo
print(pessoas[0].nome)

// Atribuição
pessoas[2] = Pessoa("Carla", 28, 1.70, false)
pessoas[0].idade = 26
```

---

## 6. Controle de Fluxo

### 6.1 Condicional If-Then-Else
```noxy
if condição then
    // código se verdadeiro
end

if condição then
    // código se verdadeiro
else
    // código se falso
end

if condição1 then
    // ...
elif condição2 then
    // ...
else
    // ...
end

// Condições aninhadas (com elif)
if x > 10 then
    print("Maior que 10")
elif x > 5 then
    print("Maior que 5")
else
    print("5 ou menos")
end
```

### 6.2 Loop While
```noxy
while condição do
    // código do loop
end

// Exemplo
let i: int = 0
while i < 10 do
    print(to_str(i))
    i = i + 1
end
```

### 6.3 Break
```noxy
while true do
    if condição then
        break
    end
end
```

---

## 7. Expressões

### 7.1 Expressões Aritméticas
```noxy
let soma: int = a + b
let diferenca: int = a - b
let produto: int = a * b
let quociente: int = a / b
let resto: int = a % b

// Com parênteses
let resultado: int = (a + b) * (c - d)
```

### 7.2 Expressões de Comparação
```noxy
let maior: bool = a > b
let menor: bool = a < b
let maiorIgual: bool = a >= b
let menorIgual: bool = a <= b
let igual: bool = a == b
let diferente: bool = a != b
```

### 7.3 Expressões Lógicas
```noxy
let ambos: bool = a & b       // AND
let algum: bool = a | b       // OR
let negacao: bool = !a        // NOT

// Combinações
if x > 0 & x < 100 then
    print("Entre 0 e 100")
end

if lista == null | lista.tamanho == 0 then
    print("Lista vazia")
end
```

### 7.4 Concatenação de Strings
```noxy
let completo: string = "Olá, " + nome + "!"
let info: string = "Idade: " + to_str(idade)
```

### 7.5 Acesso a Arrays
```noxy
// Leitura
let primeiro: int = arr[0]
let elemento: int = arr[i]

// Atribuição
arr[0] = 100
arr[i] = valor
```

### 7.6 Acesso a Caracteres de String
```noxy
let texto: string = "Hello"
let primeiro: string = texto[0]  // "H"
let segundo: string = texto[1]   // "e"
```

---

## 8. F-Strings (Strings Formatadas)

### 8.1 Sintaxe Básica
```noxy
f"Texto com {expressão} interpolada"

// Exemplos
print(f"Olá, {nome}!")
print(f"Soma: {a + b}")
print(f"Status: {ativo}")
```

### 8.2 Múltiplas Interpolações
```noxy
print(f"Nome: {nome}, Idade: {idade}, Altura: {altura}")
```

### 8.3 Especificadores de Formato

#### Para Inteiros
```noxy
let num: int = 42
print(f"Padrão: {num}")         // 42
print(f"Largura 5: {num:5}")    //    42
print(f"Com zeros: {num:05}")   // 00042
print(f"Hexadecimal: {num:x}")  // 2a
print(f"HEX maiúsculo: {num:X}") // 2A
print(f"Octal: {num:o}")        // 52
```

#### Para Floats
```noxy
let valor: float = 3.14159
print(f"Padrão: {valor}")           // 3.141590
print(f"2 decimais: {valor:.2f}")   // 3.14
print(f"Científico: {valor:.2e}")   // 3.14e+00
print(f"Geral: {valor:.3g}")        // 3.14
```

### 8.4 Escape de Caracteres em F-Strings
```noxy
print(f"Nome:\t{nome}")     // Tab
print(f"Linha1\nLinha2")    // Nova linha
```

---

## 9. Funções Embutidas (Built-in)

### 9.1 Entrada/Saída
```noxy
print(expressão)           // Imprime valor no console
print("Hello")             // Imprime string
print(42)                  // Imprime inteiro
print(3.14)                // Imprime float
print(true)                // Imprime bool
```

### 9.2 Conversão de Tipos
```noxy
to_str(valor)              // Converte qualquer tipo para string
to_str(42)                 // "42"
to_str(3.14)               // "3.140000"
to_str([1, 2, 3])          // "[1, 2, 3]"

to_int(valor)              // Converte float para int (trunca)
to_int(3.7)                // 3

to_float(valor)            // Converte int para float
to_float(42)               // 42.000000
```

### 9.3 Strings
```noxy
strlen(s)                  // Retorna o tamanho da string
strlen("Hello")            // 5

ord(char)                  // Retorna valor Unicode do caractere
ord("A")                   // 65
ord("a")                   // 97
```

### 9.4 Arrays
```noxy
length(arr)                // Retorna o tamanho do array
length([1, 2, 3])          // 3

zeros(n)                   // Cria array de n zeros
let arr: int[100] = zeros(100)
```

### 9.5 Bytes e Codificação
```noxy
to_bytes(val)              // Converte int, string ou array[int] para bytes
to_bytes("ABC")            // b"ABC"

hex_encode(b)              // Bytes -> String Hex
hex_encode(b"\xFF")        // "ff"

hex_decode(s)              // String Hex -> Bytes
hex_decode("ff")           // b"\xFF"

base64_encode(b)           // Bytes -> String Base64
base64_decode(s)           // String Base64 -> Bytes

### 9.6 Formatação de Strings (Estilo Printf)
A função `fmt` permite formatar strings usando especificadores de formato, similar ao `printf` do C ou `fmt.Sprintf` do Go.

```noxy
fmt(formato, argumentos...) -> string
```

**Verbos Suportados:**
- **Geral**: `%v` (valor padrão)
- **Strings**: `%s` (string), `%q` (quoted)
- **Inteiros**: `%d` (decimal), `%x`/`%X` (hex), `%b` (binário), `%o` (octal)
- **Floats**: `%f` (decimal), `%e` (científico), `%g` (geral)
- **Booleanos**: `%t` (true/false)

**Exemplos:**
```noxy
// Inteiros com padding
let s1: string = fmt("ID: %05d", 42)      // "ID: 00042"

// Floats com precisão
let s2: string = fmt("PI: %.2f", 3.14159) // "PI: 3.14"

// Hexadecimal
let s3: string = fmt("Hex: %X", 255)      // "Hex: FF"

// Múltiplos argumentos
let s4: string = fmt("%s tem %d anos", "Ana", 25)
```
```

---

## 10. Sistema de Módulos

### 10.1 Importação Básica
```noxy
// 1. Importar módulo (namespace)
use math
let res: int = math.add(1, 2)

// 2. Importar com Alias (Renomear)
use math as m
let res: int = m.add(1, 2)

// 3. Importar aninhado
use utils.string_helpers
let s: string = utils.string_helpers.upper("oi")

// 4. Importar aninhado com Alias
use utils.string_helpers as str
let s: string = str.upper("oi")
```

### 10.2 Importação Seletiva
```noxy
// Importar símbolos específicos para o escopo atual
use math select add, pi

let x: int = add(10, 20)  // Acesso direto
```

### 10.3 Importação de Pastas (Wildcard)
É possível importar todos os arquivos `.nx` de uma pasta como módulos.
```noxy
// Estrutura:
// stdlib/
//   ├── io.nx
//   ├── net.nx
//   └── ...

// Importa TODO o conteúdo da pasta 'stdlib' 
// Cada arquivo vira um módulo acessível.
use stdlib select *

// Agora podemos acessar:
let f: io.File = io.open(...)
let s: net.Socket = net.connect(...)
```

### 10.4 Estrutura de Pacotes
```
projeto/
├── main.nx
├── utils/
│   ├── math.nx
│   └── algorithms.nx
```

```noxy
// Em main.nx:

// Opção A: Importar arquivo específico
use utils.math
math.func()

// Opção B: Importar pasta utils
use utils select *
// Agora 'math' e 'algorithms' estão disponíveis
math.func()
algorithms.sort()
```

---

## 11. Referências (ref)

### 11.1 Quando Usar ref

#### Em Campos de Struct (Auto-referência)
```noxy
struct TreeNode
    valor: int,
    esquerda: ref TreeNode,
    direita: ref TreeNode
end
```

#### Em Parâmetros (Mutação)
```noxy
func modificar(node: ref Node) -> void
    node.valor = 100
end
```

#### Em Variáveis
```noxy
let atual: ref Node = null
atual = ref n1
```

#### Em Retorno de Função
```noxy
func buscar(head: ref Node, valor: int) -> ref Node
    while head != null do
        if head.valor == valor then
            return head
        end
        head = head.proximo
    end
    return null
end
```

### 11.2 Operador ref
```noxy
// Usar 'ref' ao atribuir a um campo/variável do tipo ref
node.proximo = ref outroNode

// NÃO é necessário ao ler
let proximo: ref Node = node.proximo
```

### 11.3 Comparação com null
```noxy
if node.proximo == null then
    print("Fim da lista")
end

if node.proximo != null then
    print("Tem próximo")
end
```

---

## 12. Padrões Comuns

### 12.1 Lista Encadeada
```noxy
struct Node
    valor: int,
    proximo: ref Node
end

func append(node: ref Node, valor: int)
    if node.proximo == null then
        node.proximo = ref Node(valor, null)
    else
        append(node.proximo, valor)
    end
end

let head: Node = Node(10, null)
append(head, 20)
append(head, 30)
```

### 12.2 Árvore Binária
```noxy
struct TreeNode
    data: int,
    left: ref TreeNode,
    right: ref TreeNode
end

let root: TreeNode = TreeNode(1, null, null)
root.left = Node(2, null, null)
root.right = Node(3, null, null)

func pre_order(node: ref TreeNode)
    if node != null then
        print(to_str(node.data))
        pre_order(node.left)
        pre_order(node.right)
    end
end
```

### 12.3 Padrão Optional
```noxy
struct OptionalString
    has_value: bool,
    value: string
end

func buscar(key: string) -> OptionalString
    // busca...
    if encontrado then
        return OptionalString(true, valor)
    end
    return OptionalString(false, "")
end

let resultado: OptionalString = buscar("chave")
if resultado.has_value then
    print(resultado.value)
end
```



---

## 13. Biblioteca Padrão (Resumo)

A Noxy Standard Library (`stdlib`) é distribuída junto com o interpretador.

### 13.1 Módulos Principais
| Módulo | Descrição |
|--------|-----------|
| `io` | Operações de arquivo e sistema de arquivos |
| `time` | Tempo e data (timestamp, sleep) |
| `strings` | Manipulação avançada de strings (split, join, replace) |
| `net` | Sockets e rede básica |
| `http` | Cliente e Servidor HTTP |
| `json_parser` | Parser e serializer JSON |
| `sqlite` | Banco de dados SQLite |
| `uuid` | Geração de UUIDs |
| `rand` | Geração de números aleatórios |
| `math` | Funções matemáticas básicas |
| `sys` | Informações do sistema e argumentos |

---

## 14. Gramática Formal (EBNF Simplificada)

```ebnf
program        = { statement } ;

statement      = let_stmt
               | global_stmt
               | func_def
               | struct_def
               | if_stmt
               | while_stmt
               | return_stmt
               | break_stmt
               | use_stmt
               | assignment
               | expression ;

let_stmt       = "let" IDENTIFIER ":" type "=" expression ;
global_stmt    = "global" IDENTIFIER ":" type "=" expression ;

func_def       = "func" IDENTIFIER "(" [ param_list ] ")" [ "->" type ] 
                 { statement } "end" ;
param_list     = param { "," param } ;
param          = IDENTIFIER ":" type ;

struct_def     = "struct" IDENTIFIER 
                 { field_def } "end" ;
field_def      = IDENTIFIER ":" type [ "," ] ;

if_stmt        = "if" expression "then" { statement } 
                 [ "else" { statement } ] "end" ;

while_stmt     = "while" expression "do" { statement } "end" ;

return_stmt    = "return" [ expression ] ;
break_stmt     = "break" ;

use_stmt       = "use" module_path "select" import_list ;
module_path    = IDENTIFIER { "." IDENTIFIER } ;
import_list    = "*" | IDENTIFIER { "," IDENTIFIER } ;

assignment     = lvalue "=" expression ;
lvalue         = IDENTIFIER { "." IDENTIFIER | "[" expression "]" } ;

type           = "int" | "float" | "string" | "str" | "bool" | "void"
               | IDENTIFIER                     (* struct name *)
               | type "[" [ NUMBER ] "]"        (* array type *)
               | "ref" type ;                   (* reference type *)

expression     = or_expr ;
or_expr        = and_expr { "|" and_expr } ;
and_expr       = not_expr { "&" not_expr } ;
not_expr       = [ "!" ] comparison ;
comparison     = additive { ( ">" | "<" | ">=" | "<=" | "==" | "!=" ) additive } ;
additive       = multiplicative { ( "+" | "-" ) multiplicative } ;
multiplicative = unary { ( "*" | "/" | "%" ) unary } ;
unary          = [ "-" ] postfix ;
postfix        = primary { "." IDENTIFIER | "[" expression "]" | "(" [ arg_list ] ")" } ;
primary        = NUMBER | FLOAT | STRING | FSTRING
               | "true" | "false" | "null"
               | IDENTIFIER
               | "ref" expression
               | "(" expression ")"
               | "[" [ expression { "," expression } ] "]"
               | "zeros" "(" expression ")" ;

arg_list       = expression { "," expression } ;

IDENTIFIER     = letter { letter | digit | "_" } ;
NUMBER         = digit { digit } ;
FLOAT          = digit { digit } "." digit { digit } ;
STRING         = '"' { character } '"' ;
FSTRING        = 'f"' { character | "{" expression [ ":" format_spec ] "}" } '"' ;
format_spec    = [ width ] [ "." precision ] [ format_type ] ;
```

---

## 15. Compilação e Execução

### 15.1 Processo de Compilação
```bash
# Compilar código Noxy para objeto
uv run python compiler.py --compile arquivo.nx

# Linkar com funções de runtime em C
gcc -o programa.exe output.obj casting_functions.c

# Executar
./programa.exe
```

### 15.2 Arquivos Gerados
- `output.ll` - Código LLVM IR (intermediário)
- `output.obj` - Arquivo objeto compilado

---

## 16. Exemplos Completos

### 16.1 Hello World
```noxy
print("Hello from Noxy!")
```

### 16.2 Calculadora Simples
```noxy
func add(a: int, b: int) -> int
    return a + b
end

func multiply(a: int, b: int) -> int
    return a * b
end

let x: int = 10
let y: int = 5

print(f"Soma: {add(x, y)}")
print(f"Produto: {multiply(x, y)}")
```

### 16.3 Bubble Sort
```noxy
func bubblesort(array: int[], tamanho: int) -> void
    let desordenado: bool = true
    while desordenado do
        desordenado = false
        let i: int = 0
        while i < (tamanho - 1) do
            if array[i] > array[i + 1] then
                let temp: int = array[i]
                array[i] = array[i + 1]
                array[i + 1] = temp
                desordenado = true
            end
            i = i + 1
        end
    end
end

let arr: int[6] = [5, 3, 8, 6, 2, 7]
bubblesort(arr, 6)
print(to_str(arr))  // [2, 3, 5, 6, 7, 8]
```

### 16.4 Lista Encadeada Completa
```noxy
struct Node
    valor: int,
    proximo: ref Node
end

func append(node: ref Node, valor: int)
    if node.proximo == null then
        node.proximo = ref Node(valor, null)
    else
        append(node.proximo, valor)
    end
end

func print_list(node: ref Node)
    while node != null do
        print(to_str(node.valor))
        node = node.proximo
    end
end

let head: Node = Node(10, null)
append(head, 20)
append(head, 30)
append(head, 40)

print_list(head)  // 10 20 30 40
```

### 16.5 Estrutura de Dados com HashMap (Padrão)
```noxy
struct Node
    key: string,
    value: string,
    next: ref Node
end

struct OptionalString
    has_value: bool,
    value: string
end

let capacity: int = 16
let buckets: Node[16] = [null, null, null, null, null, null, null, null, 
                          null, null, null, null, null, null, null, null]

func hash(chave: string, tamanho: int) -> int
    let hash_value: int = 0
    let i: int = 0
    while i < strlen(chave) do
        hash_value = (hash_value * 31 + ord(chave[i])) % tamanho
        i = i + 1
    end
    return hash_value
end

func put(key: string, value: string) -> void
    let h: int = hash(key, capacity)
    if buckets[h] == null then
        buckets[h] = Node(key, value, null)
    else
        // adicionar ou atualizar...
    end
end

func get(key: string) -> OptionalString
    let h: int = hash(key, capacity)
    if buckets[h] != null then
        let node: Node = buckets[h]
        while node != null do
            if node.key == key then
                return OptionalString(true, node.value)
            end
            node = node.next
        end
    end
    return OptionalString(false, "")
end
```

---

## 17. Notas de Implementação para o Interpretador

### 17.1 Lexer
- Tokenizar comentários `//` (ignorar até fim de linha)
- Suportar strings com escape (`\n`, `\t`, `\"`)
- Reconhecer f-strings com prefixo `f"`
- Números inteiros e floats
- Identificadores e palavras-chave

### 17.2 Parser
- Usar recursive descent parsing
- Precedência de operadores (do menor para maior):
  1. `|` (OR)
  2. `&` (AND)
  3. `!` (NOT)
  4. `>`, `<`, `>=`, `<=`, `==`, `!=`
  5. `+`, `-`
  6. `*`, `/`, `%`
  7. Unário `-`
  8. Postfix `.`, `[]`, `()`

### 17.3 Sistema de Tipos (CRÍTICO)

#### Tipagem Estática Imutável
- **O tipo de uma variável é fixado na declaração e NUNCA pode mudar**
- Em cada atribuição, verificar se o tipo do valor é igual ao tipo declarado
- Erro de compilação se tipos forem incompatíveis

```
// Tabela de símbolos deve armazenar:
{
  "nome_variavel": {
    "tipo": TipoDeclarado,    // IMUTÁVEL após criação
    "valor": ValorAtual,
    "escopo": "local" | "global"
  }
}
```

#### Verificações Obrigatórias
1. **Declaração**: Tipo do valor inicial deve corresponder ao tipo declarado
2. **Reatribuição**: Tipo do novo valor deve ser IGUAL ao tipo declarado
3. **Chamada de função**: Tipos dos argumentos devem corresponder aos parâmetros
4. **Retorno de função**: Tipo do valor retornado deve corresponder ao tipo declarado
5. **Operações**: Operandos devem ter tipos compatíveis

### 17.4 Semântica de Passagem de Structs (CRÍTICO)

#### Struct SEM ref = Cópia Completa (Passagem por Valor)
```
func exemplo(p: Pessoa) -> void
    // p é uma CÓPIA independente
    // modificações em p NÃO afetam o original
end
```

Implementação:
1. Ao chamar função, criar cópia profunda (deep copy) do struct
2. Função trabalha com a cópia
3. Original permanece inalterado

#### Struct COM ref = Ponteiro (Passagem por Referência)
```
func exemplo(p: ref Pessoa) -> void
    // p é um PONTEIRO para o original
    // modificações em p AFETAM o original
end
```

Implementação:
1. Ao chamar função, passar referência/ponteiro para o struct
2. Função trabalha diretamente no original
3. Modificações persistem após retorno

#### Tabela de Comportamento
| Tipo Parâmetro | O que é passado | Modificações afetam original? |
|----------------|-----------------|-------------------------------|
| `int`, `float`, `bool`, `string` | Cópia do valor | Não |
| `Struct` (sem ref) | Cópia completa do struct | **Não** |
| `ref Struct` | Ponteiro/referência | **Sim** |
| `int[]`, `Struct[]` | Ponteiro para array | Sim (elementos) |

### 17.5 Verificações de Tipo Adicionais
- Arrays têm tamanho fixo conhecido em tempo de compilação
- Structs são tipos nominais (identificados por nome, não por estrutura)
- Referências podem ser `null`
- Tipos são verificados em tempo de compilação, não em runtime

### 17.6 Memória
- Structs são alocados no heap (via malloc ou equivalente)
- Arrays podem ser stack ou heap dependendo do contexto
- Referências são ponteiros
- Para passagem por valor de structs: fazer deep copy

### 17.7 Runtime
- Implementar `print` para todos os tipos
- Implementar conversões `to_str`, `to_int`, `to_float`
- Implementar `strlen`, `ord`, `length`, `zeros`
- F-strings requerem concatenação dinâmica

### 17.8 Erros de Tipo (Mensagens Sugeridas)
```
// Erro de tipo em atribuição
Erro: Não é possível atribuir valor do tipo 'float' a variável 'x' do tipo 'int'
      O tipo de uma variável não pode ser alterado após a declaração.

// Erro de tipo em chamada de função
Erro: Argumento 1 da função 'processar' tem tipo 'string', esperado 'int'

// Erro de tipo em operação
Erro: Operador '+' não suportado entre tipos 'int' e 'string'

// Erro de tipo em retorno
Erro: Função 'calcular' deve retornar 'int', mas retorna 'float'
```

---

*Versão: 1.1*
*Linguagem: Noxy*
*Backend: LLVM (original) / A ser implementado (interpretador)*


# F-strings em Noxy

Este documento explica como usar f-strings (strings formatadas) na linguagem Noxy, uma funcionalidade similar às f-strings do Python.

## Sintaxe Básica

Uma f-string é criada prefixando uma string com `f` e incluindo expressões dentro de chaves `{}`:

```noxy
f"Texto {expressao} mais texto"
```

## Exemplos de Uso

### 1. Variáveis Simples
```noxy
let nome: string = "Maria"
print(f"Olá, {nome}!")
// Saída: Olá, Maria!
```

### 2. Múltiplas Variáveis
```noxy
let nome: string = "João"
let idade: int = 25
print(f"Meu nome é {nome} e tenho {idade} anos")
// Saída: Meu nome é João e tenho 25 anos
```

### 3. Diferentes Tipos de Dados
```noxy
let numero: int = 42
let pi: float = 3.14159
let ativo: bool = true
print(f"Número: {numero}, Pi: {pi}, Ativo: {ativo}")
// Saída: Número: 42, Pi: 3.141590, Ativo: true
```

### 4. Expressões Matemáticas
```noxy
let x: int = 10
let y: int = 5
print(f"A soma de {x} e {y} é {x + y}")
// Saída: A soma de 10 e 5 é 15
```

### 5. Escape Sequences
```noxy
let nome: string = "Ana"
print(f"Nome:\t{nome}\nStatus:\tAtivo")
// Saída:
// Nome:    Ana
// Status:  Ativo
```

## Tipos Suportados

As f-strings em Noxy suportam conversão automática dos seguintes tipos:

- **int**: Convertido para decimal (ex: `42` → `"42"`)
- **float**: Convertido com 6 casas decimais (ex: `3.14` → `"3.140000"`)
- **bool**: Convertido para `"true"` ou `"false"`
- **string**: Usado diretamente

## Funcionalidades Avançadas

### F-strings em Estruturas de Controle
```noxy
if idade >= 18 then
    print(f"{nome} é maior de idade")
else
    print(f"{nome} é menor de idade")
end
```

### F-strings em Loops
```noxy
let i: int = 0
while i < 3 do
    print(f"Iteração {i + 1}")
    i = i + 1
end
```

### F-strings como Valores de Retorno
```noxy
func criar_saudacao(nome: string, hora: int) -> string
    return f"Bom dia, {nome}! São {hora} horas."
end
```

## Especificadores de Formato

As f-strings em Noxy agora suportam especificadores de formato similares ao Python:

### Inteiros
```noxy
let num: int = 42
print(f"Padrão: {num}")           // Saída: 42
print(f"Largura: {num:5}")        // Saída:    42
print(f"Zeros: {num:05}")         // Saída: 00042
print(f"Hex: {num:x}")            // Saída: 2a
print(f"HEX: {num:X}")            // Saída: 2A
print(f"Octal: {num:o}")          // Saída: 52
```

### Floats
```noxy
let valor: float = 3.14159
print(f"Padrão: {valor}")         // Saída: 3.141590
print(f"2 decimais: {valor:.2f}") // Saída: 3.14
print(f"Científico: {valor:.2e}") // Saída: 3.14e+00
print(f"Geral: {valor:.3g}")      // Saída: 3.14
```

## Limitações Atuais

1. **Aninhamento**: F-strings aninhadas não são suportadas
2. **Formatação Avançada**: Alguns especificadores como `%` (porcentagem) ainda não são suportados
3. **Expressões Complexas**: Expressões muito complexas podem não ser suportadas

## Exemplo Completo

Veja o arquivo `exemplo_fstrings.noxy` para um exemplo completo demonstrando todas as funcionalidades das f-strings em Noxy.

## Implementação Técnica

As f-strings são implementadas através de:
1. **Lexer**: Identifica f-strings e extrai expressões
2. **Parser**: Analisa expressões embutidas
3. **Codegen**: Converte tipos automaticamente e concatena strings
4. **Runtime**: Usa funções C padrão (`sprintf`, `strcat`, etc.)

## Compilação

Para compilar código com f-strings:
```bash
uv run python compiler.py --compile arquivo.noxy
gcc -mcmodel=large casting_functions.c output.obj -o programa.exe
```

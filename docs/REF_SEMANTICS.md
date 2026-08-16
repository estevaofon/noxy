# Referências em Noxy

Noxy usa `ref T` para representar uma referência de primeira classe a um valor
do tipo `T`. Leituras fazem dereference automático, mas escrita e rebind usam
sintaxes distintas. Assim, o tipo declarado de uma variável permanece estável
mesmo quando seu valor ou o armazenamento referenciado são mutáveis.

## Resumo

| Form | Meaning |
|---|---|
| `f(value)` where exact parameter is `ref T` | Contextual reference conversion |
| `ref value` | Create a first-class reference |
| `*reference = value` | Update referenced storage |
| `reference = ref other` | Rebind the reference value |

## 1. Conversão contextual em chamadas exatas

Quando a assinatura exata de uma função espera `ref T`, uma expressão
endereçável do tipo `T` é convertida contextualmente em referência:

```noxy
func increment(value: ref int) -> void
    *value = value + 1
end

let answer: int = 41
increment(answer)       // empréstimo contextual; answer passa a ser 42
increment(ref answer)   // a referência explícita também é válida
```

São endereçáveis variáveis locais e globais, campos de structs, slots de arrays
e maps e variáveis capturadas. Literais não nulos e temporários comuns
produzidos por funções não são endereçáveis:

```noxy
increment(41)          // erro de compilação: not addressable
increment(make_int())  // erro se make_int() retorna int
```

Um resultado que já tenha o tipo declarado `ref T` é um valor de referência e
pode ser passado diretamente. O literal `null` também é aceito como o valor
nullable explícito de `ref T`; ele não possui nem simula um slot de
armazenamento.

A conversão contextual só existe quando o compilador conhece a assinatura
exata, incluindo contratos públicos nativos conhecidos pelo compilador.

## 2. Referências explícitas em fronteiras dinâmicas

`ref value` cria uma referência de primeira classe. Ela pode ser guardada,
retornada ou passada através de uma fronteira dinâmica.

O tipo bare `func` não contém a assinatura dos parâmetros. Por isso, ele exige
`ref value` para transportar uma referência; a chamada não infere referências:

```noxy
let dynamic: func = increment
let answer: int = 41

dynamic(ref answer) // envia ref int
dynamic(answer)     // envia int; o runtime rejeita antes de entrar na função
```

Primitivas nativas sem tipo, plugins sem assinatura e membros de módulo cujo
tipo exato não é conhecido são fronteiras dinâmicas equivalentes. Também nelas
a referência deve ser explícita.

Se uma variável já possui tipo `ref T`, a forma explícita encaminha o valor de
referência existente, sem criar `ref ref T`:

```noxy
let pointer: ref int = ref answer
dynamic(ref pointer) // encaminha o mesmo ref int
```

## 3. Leitura automática

Uma referência usada onde `T` é necessário é lida automaticamente:

```noxy
let answer: int = 41
let pointer: ref int = ref answer
let next: int = pointer + 1
print(pointer) // 41
```

O dereference automático vale para expressões e acesso a campos. Ele não muda
as regras explícitas de escrita.

## 4. Update do armazenamento

`*reference = value` escreve no armazenamento apontado:

```noxy
func double_it(value: ref int) -> void
    *value = value * 2
end
```

Uma atribuição bare a uma variável `ref T` não atualiza o alvo. Portanto,
`value = value * 2` é inválido: o lado direito é `T`, mas um rebind requer
`ref T`.

Campos e índices do valor referenciado continuam usando a sintaxe normal:

```noxy
func rename(person: ref Person) -> void
    person.name = "Noxy"
end
```

## 5. Rebind da referência

`reference = ref other` troca o alvo armazenado na variável de referência:

```noxy
let first: int = 10
let second: int = 20
let pointer: ref int = ref first

pointer = ref second
*pointer = 21
```

O rebind de um parâmetro muda apenas o valor de referência local do parâmetro;
ele não faz rebind da variável de referência mantida pelo chamador.

## 6. `null` e tempo de vida

`null` é um valor válido de `ref T`. Pode ser armazenado, comparado, retornado
ou substituído por rebind. Leituras por dereference automático propagam
`null`; tentar atualizar através de `null` é erro de runtime.

Referências podem escapar por retorno, closure, campo ou global. Quando o alvo
é uma variável local capturada, a VM usa o armazenamento de upvalue para que a
referência permaneça válida depois que a função criadora retorna.

## 7. Parâmetros comuns e semântica de valor

Parâmetros sem `ref` seguem a semântica de valor de Noxy (0.4.0+): primitivos
são copiados, e arrays, maps e structs se comportam como cópias profundas
independentes em qualquer profundidade, implementadas com copy-on-write.
**`ref` é o único mecanismo de compartilhamento da linguagem** — quando uma
assinatura não tem `ref`, o chamador tem a garantia de que seus dados não
serão tocados.

## 8. Refs para dentro de contêineres e copy-on-write

Um `ref` criado para dentro de um contêiner (`ref arr[0]`, campo de struct)
fixa a identidade daquele contêiner no momento da criação — a base do caminho
é unicizada nesse instante. Aresta documentada: se o contêiner for copiado
*depois* da criação do ref (ex.: `let b = a`), uma escrita através do ref
pré-existente ainda é visível pela cópia que não materializou seu clone.
Crie refs depois de compartilhar, não antes.

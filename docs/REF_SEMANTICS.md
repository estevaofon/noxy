# Referências em Noxy: Guia Completo

## O que é `ref`?

Em Noxy, `ref` tem dois usos distintos:

1. **Como tipo**: `ref Node` significa "ponteiro para Node"
2. **Como operador**: `ref x` cria uma referência que permite modificação

---

## 1. `ref` como Tipo de Variável

Quando você declara uma variável com tipo `ref Node`, ela armazena um **ponteiro** para um Node:

```noxy
let current: ref Node = algumNode
```

Isso significa que `current` contém um **endereço de memória** que aponta para um Node.

### Exemplo Visual

```
Variável 'current'          Memória
┌─────────────┐             ┌─────────────────┐
│ ponteiro ───────────────▶ │ Node            │
└─────────────┘             │  valor: 20      │
                            │  proximo: ────────▶ ...
                            └─────────────────┘
```

---

## 2. `ref` como Parâmetro de Função

Quando um parâmetro é declarado como `ref`, você está permitindo que a função **modifique o valor original** do chamador:

```noxy
func modificar(n: ref Node)
    n.valor = 999    // Modifica o Node original ✓
    n = outroNode    // CUIDADO: Modifica a variável do chamador!
end
```

### Diferença Crítica

| Código | Parâmetro `ref` | Variável local `ref Node` |
|--------|-----------------|---------------------------|
| `x.campo = val` | Modifica o objeto ✓ | Modifica o objeto ✓ |
| `x = outro` | **Modifica a variável do chamador!** | Só muda o ponteiro local |

---

## 3. Exemplo Prático: O Mito da Destruição

Muitos pensam que como `ref` permite alterar o objeto, reatribuir a variável também altera o chamador. **Isso não é verdade para ponteiros!**

### O que realmente acontece

```noxy
let global_node: Node = Node(10, null)

func traverse(node: ref Node)
    // Isso é seguro! 'node' é uma variável local que contém um endereço.
    // Mudar o endereço que 'node' aponta NÃO muda 'global_node'.
    while node != null do
        print(to_str(node.valor))
        node = node.proximo  // ← Só muda a variável local 'node'
    end
end

traverse(ref global_node)
// global_node continua apontando para o início da lista!
```

### Então, qual é o risco?

O risco é **confundir o leitor**. Reusar o parâmetro para iterar é má prática, mas não corrompe a memória do chamador.

O **verdadeiro poder (e perigo)** do `ref` é alterar o CONTEÚDO:

```noxy
func destruir(node: ref Node)
    // Isso é PERIGOSO. Atribuir um VALOR sobrescreve a memória original!
    *node = Node(666, null) 
end
// global_node agora teria ID 666!
```

---

## 4. Modificando Campos vs Modificando o Ponteiro

A Noxy introduziu uma distinção sintática explícita para evitar ambiguidades:

1.  **Rebind** (`ptr = ref outro`): Muda para ONDE o ponteiro aponta.
2.  **Update** (`*ptr = valor`): Muda o VALOR no endereço apontado.

### Modificar um campo (Sempre Update se não for ref)

Para campos primitivos, a atribuição é direta e modifica o objeto original (graças ao auto-deref na leitura do ponteiro struct):

```noxy
func mudar_valor(node: ref Node)
    node.valor = 999  // OK: Acessa o objeto original e muda o campo
end
```

### Modificar o ponteiro (Rebind) vs Sobrescrever (Update)

```noxy
// 1. REBIND (Mudar a seta)
func rebind_exemplo(node: ref Node)
    // Muda a variável LOCAL 'node' para apontar para outro lugar
    node = Node(999, null)  // ERRO! Não pode atribuir valor a ref.
    
    let novo: Node = Node(999, null)
    node = ref novo // OK: Agora 'node' aponta para 'novo'
    // O Node original do chamador NÃO foi afetado.
end

// 2. UPDATE DESTRUTIVO (Mudar o alvo)
func update_exemplo(node: ref Node)
    // Usa o asterisco (*) para dizer: "Vá no endereço e escreva lá"
    *node = Node(999, null)
    // AGORA o objeto original do chamador FOI DESTRUÍDO/SUBSTITUÍDO!
end
```

### Tabela Verdade da Sintaxe

| Sintaxe | Ação | Efeito no Chamador |
| :--- | :--- | :--- |
| `r = ref x` | **Rebind** | Nenhum (só muda variável local `r`) |
| `*r = x` | **Update** | **Destrutivo** (altera memória original) |
| `r = x` | **Erro** | Proteção contra bugs |

---

## 6. Analogia com C

Para quem conhece C, a semântica é similar a:

```c
// Parâmetro ref em Noxy = ponteiro para ponteiro em C
void func_ref(Node** node) {
    *node = (*node)->next;  // Modifica o ponteiro original!
}

// Variável local ref Node = ponteiro simples em C
void func_local(Node** node) {
    Node* current = *node;    // Cópia local do ponteiro
    current = current->next;  // Só muda o local
}
```

## 7. `ref Node` vs `Node` em Variáveis Locais

Uma pergunta comum: posso usar `Node` em vez de `ref Node` para a variável local?

```noxy
// Opção A: com ref
let current: ref Node = node

// Opção B: sem ref
let current: Node = node
```

### Para reassignação simples: ambos funcionam igual

```noxy
func teste_com_ref(node: ref Node)
    let local: ref Node = node
    local = Node(999, null)  // Não afeta o original
end

func teste_sem_ref(node: ref Node)
    let local: Node = node
    local = Node(888, null)  // Também não afeta o original
end
```

### Para traversar linked lists: use `ref Node`

O campo `proximo` é do tipo `ref Node`. Para manter compatibilidade de tipos:

```noxy
struct Node
    valor: int,
    proximo: ref Node  // ← É ref Node!
end

func traverse(head: ref Node)
    // CORRETO: tipos compatíveis
    let current: ref Node = head.proximo
    while current != null do
        current = current.proximo  // ref Node = ref Node ✓
    end
end
```

### Regra prática

| Situação | Use |
|----------|-----|
| Apenas isolar reassignação | `Node` ou `ref Node` (ambos funcionam) |
| Traversar estruturas com campos `ref` | `ref Node` (para compatibilidade de tipos) |
| Modificar campos do objeto | Ambos funcionam (o objeto é o mesmo) |

---

## Resumo

| Situação | Código | Efeito |
|----------|--------|--------|
| Parâmetro `ref`, atribuir campo | `n.valor = x` | Modifica objeto ✓ |
| Parâmetro `ref`, rebind | `n = ref outro` | Só muda variável local `n` |
| Parâmetro `ref`, update | `*n = valor` | **Modifica original do chamador!** |
| Variável local `ref T`, update | `*v = valor` | Modifica objeto apontado ✓ |
| Variável local `ref T`, rebind | `v = ref outro` | Só muda ponteiro local |

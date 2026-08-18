# Genéricos por Monomorfização

**Data:** 2026-08-18 · **Branch:** `feat/generics` (base: `develop` @ ebf0067, v0.6.0)
**Status:** aprovado em discussão; spec formal para implementação
**Release alvo:** 0.7.0 (aditivo — nenhuma quebra de código existente)

## 1. Objetivo

Eliminar a assimetria entre builtins e código de usuário: hoje `append`, `length` e
`contains` funcionam para qualquer array porque o compilador os conhece; código de
usuário não consegue escrever essa abstração e cai em `any`, sem checagem estática.

Entregar **funções e structs genéricos** com **custo zero em runtime**: o bytecode de
uma instância monomorfizada é idêntico ao de código especializado escrito à mão —
opcodes tipados (`OP_ADD_INT` etc.) continuam disparando dentro de código genérico.
Nenhuma mudança na VM.

## 2. Contrato de linguagem (visível ao usuário)

### 2.1 Sintaxe

Parâmetros de tipo entre `<>` após o nome da declaração, utilizáveis em qualquer
posição de tipo (parâmetros, retorno, campos, corpo):

```noxy
func map<A, B>(arr: A[], fn: func(A) -> B) -> B[]
    let out: B[] = []
    for item in arr do
        append(out, fn(item))
    end
    return out
end

struct Stack<T>
    items: T[]
end

struct Node<T>
    value: T,
    next: ref Node<T>      // auto-referência funciona
end
```

Tipos genéricos aparecem **somente em posição de tipo** (anotações, campos,
retorno): `Stack<int>`, `Node<string>`, `Stack<Stack<int>>`. Não existe
instanciação explícita em posição de expressão (`first<int>(x)` não existe) —
zero ambiguidade com os operadores `<` / `>`.

### 2.2 Uso — sempre por inferência

```noxy
map(nums, dobra)               // A, B unificados dos argumentos
let s: Stack<int> = Stack([])  // T dos argumentos; indeterminado (array vazio)
                               // → resolvido pela anotação do let (target-typing)
```

Viável porque todo `let` em Noxy exige anotação de tipo — a linguagem já obriga o
usuário a escrever a informação de que a instanciação precisa.

### 2.3 Regras semânticas

1. **Monomorfização nominal**: cada instanciação é um tipo/função distinto em
   compile e runtime. `Stack<int>` e `Stack<string>` são structs nominais
   diferentes (a validação de tipos CoW os distingue sem mudança alguma).
2. **Genéricos só no top level** — sem `func`/`struct` genérico aninhado dentro de
   função.
3. **Funções genéricas como valores via target-typing** (ver §3): uma genérica
   instancia em qualquer posição de valor cujo tipo alvo seja um tipo de função
   concreto. Sem alvo concreto (`func` nu, `any`, corrente sem âncora) → erro de
   compilação claro.
4. **Sem constraints na v1**: `T` é irrestrito. Operações no corpo são checadas
   **por instanciação** — `soma<T>` com `+` no corpo compila para `soma<int>` e
   falha para `soma<Ponto>` com erro apontando o call site que instanciou
   (estilo template C++, com mensagem legível — ver §9).
5. **Inferência impossível é erro**, nunca fallback silencioso para `any`:
   conflito (`T=int` vs `T=string`) ou indeterminação sem anotação produzem erro
   com sugestão de anotar.

## 3. Funções genéricas como valores (target-typing)

Princípio: **toda função que existe em runtime é objeto de primeira classe, sem
exceção.** O que não é valor é o *template* — entidade de compile-time que nunca
existe em runtime (mesma linha de Go/Rust/C++). A instanciação acontece no ponto
em que a genérica vira valor, guiada pelo tipo alvo:

```noxy
let f: func(int) -> int = identity     // let anotado → identity<int>, closure comum
map(nums, identity)                     // param esperado func(A)->B com A,B já
                                        // ancorados por nums → identity<int>
let fs: func(int) -> int[] = [dobra, identity]  // elemento de array tipado
campo.transform = identity              // campo de struct → tipo do campo é o alvo
return identity                         // retorno declarado é o alvo
```

**Ordem de resolução no caso argumento-de-chamada-genérica** (unificação
bidirecional): resolve os parâmetros de tipo pelos argumentos não-genéricos
primeiro; em seguida, o tipo esperado do argumento-função — possivelmente ainda
**parcialmente** concreto (`func(int) -> B`) — é unificado contra a assinatura
do template do argumento (`identity: func(T) -> T`), com os bindings propagando
nos dois sentidos (`T=int ⇒ B=int`). Se ao final da propagação todos os
parâmetros de tipo de ambos os lados estiverem resolvidos, ambas as instâncias
são geradas; senão, erro pedindo anotação.

**Posições onde genérica não pode virar valor** (erro de compilação, mensagem em
§9): anotação `func` nua, `any`, e corrente de genéricos sem âncora
(`compose(identity, identity)` sem nada que fixe o tipo do meio). São exatamente
as posições onde funções normais já perdem checagem estática hoje — a regra é
uma só: onde o compilador sabe o tipo, genérica é valor comum; onde não sabe,
ela não pode nascer.

Depois de instanciada, a função circula livremente (arrays, campos, `defer`,
tasks) — é um closure indistinguível de função escrita à mão.

## 4. Estratégia: monomorfização lazy por substituição de AST

Decisões (com alternativas descartadas):

- **Monomorfização** (não erasure): erasure faria todo código genérico cair no
  caminho lento da VM — na contramão do gargalo de performance já identificado
  no cross-runtime. Monomorfização entrega bytecode idêntico ao especializado.
- **Substituição de AST na frente do compilador** (não type-environment no core):
  a maquinaria de unificação/clonagem/registry vive em arquivos novos; o core
  ganha **hooks enumerados** (não difusos): interceptação de call site genérico
  em `compileCallExpression` (antes do `c.Compile(call.Function)`, que hoje
  emite o callee antes dos argumentos — compiler.go:1850), e threading de tipo
  esperado nas posições de target-typing do §3 — `LetStmt`, `ReturnStmt`,
  elemento de array literal, atribuição a campo e argumento de chamada. Fora
  desses hooks, o caminho de compilação permanece o atual.
- **Lazy, dirigida por call sites, memoizada**: templates não geram código;
  instâncias nascem sob demanda quando um uso as exige, uma vez por tupla de
  tipos por unidade de compilação.

Mecânica da instanciação:

1. Call site (ou posição de target-typing) é interceptado **antes** do caminho
   normal de compilação; os argumentos são compilados fora de ordem (bytecode
   descartado — estamos no pass 1, ver §5) só para obter seus tipos, e a tupla
   de tipos é inferida via unificação (§7).
2. Nome decorado calculado, **qualificado pelo módulo definidor do template**:
   `main::first<int>`, `colecoes::map<int,string>`, `main::Stack<int>`.
   Identificadores de usuário não contêm `<` nem `::`, então não colidem com
   instâncias; e duas instâncias só compartilham nome se vêm do **mesmo**
   template com a **mesma** tupla — caso em que são o mesmo código e qualquer
   sobrescrita é dedup idempotente (ver §8).
3. Memo consultado; se ausente, **registra o nome decorado antes** de clonar
   (recursão e auto-referência terminam), clona o AST do template substituindo
   `T→tipo concreto` em toda posição de tipo, e agenda a declaração
   monomorfizada resultante — um `FunctionStatement`/`StructStatement` comum.
4. O nó do AST é **reescrito** para referenciar o nome decorado (`map` →
   identificador `colecoes::map<int,string>`; idem posições de target-typing).
   O pass 2 compila o AST reescrito pelo caminho 100% normal — nenhuma lógica
   genérica ativa no pass 2.

Cascata genérico→genérico é natural: o corpo clonado de `map<int,string>` contém
chamadas com tipos já concretos, que disparam novas instanciações pelo mesmo
caminho. Instanciar `Stack<Stack<int>>` exige resolver `Stack<int>` antes — a
ordem de dependência das declarações sintéticas sai de graça da ordem de criação
no memo.

## 5. Arquitetura two-pass

Problema: o compilador é single-pass streaming, mas uma instância descoberta
dentro do corpo de `f` precisa estar **definida como global antes** de qualquer
call site executar — e `OP_CLOSURE` captura `frame.Environment` em runtime, então
uma função não pode virar constante pré-fabricada em compile-time.

```
parse → [há genéricos no programa ou nos módulos usados?]
              │não                        │sim
              ▼                           ▼
      compila normal              Pass 1: compila descartando bytecode;
      (custo exatamente zero)             call sites genéricos são interceptados,
                                          argumentos compilados fora de ordem para
                                          inferência, instâncias coletadas
                                          (memoizadas) e o AST reescrito para os
                                          nomes decorados (§4)
                                          ▼
                                  Pass 2: prepende as declarações monomorfizadas
                                          como statements sintéticos no topo do
                                          Program (structs antes de funções) e
                                          compila o AST reescrito pelo caminho
                                          100% normal
```

- No pass 2 tudo é código comum: instâncias viram globals via
  `OP_CLOSURE`/`OP_SET_GLOBAL`, aproveitam o cache de globals existente.
  **Zero mudança na VM.**
- **A regra vale para todo caminho que compila um `Program`** — não só o
  compilador do programa principal. Três outros caminhos compilam módulos e
  precisam do mesmo tratamento "declaração genérica ⇒ registra template, pula
  corpo" (+ two-pass quando o módulo usa os próprios templates internamente):
  1. o **validator** de módulos em compile-time (`module_exports.go:305` compila
     o módulo inteiro; um template com `T` livre falharia e o módulo viraria
     "not loadable" silenciosamente);
  2. o **runtime** do `use` (`vm/modules.go` — `compileAndRunModule` recompila
     cada módulo com compiler novo);
  3. os caminhos de **predeclare** (`predeclareStructs` /
     `predeclareGlobalBindings`), que hoje registrariam o template cru com
     `TypeParamType` nos tipos.
- A detecção "há genéricos?" varre as declarações do programa e dos módulos
  usados (via `loadModuleDeclarations`, com cache). Para import seletivo isso
  carrega declarações que o predeclare atual não carrega — custo pequeno, mas
  **medido pelo gate `startup_generics` (§11), não afirmado**. Programa sem
  genéricos continua pulando o pass 1 inteiro.
- Custo quando há genéricos: a compilação ~dobra, sobre uma base de poucos ms
  (startup total hoje: 63ms). Protegido por gate de benchmark (§11).
- REPL: o two-pass aplica-se por linha de input; o registry de templates entra
  no **estado compartilhado da sessão** (mudança de assinatura em
  `NewWithState`, que hoje compartilha só globals e structs entre linhas).

## 6. Mudanças em lexer/parser/AST

**AST** (`internal/ast`):

- `TypeParams []string` em `FunctionStatement` e `StructStatement`
  (vazio = declaração comum; caminho atual intocado).
- `GenericType{Name string, Args []NoxyType}` para `Stack<int>` em posição de
  tipo; `String()` produz o nome de exibição. Na resolução, `Name` é resolvido
  para o template visível naquele escopo e o tipo passa a carregar a identidade
  **qualificada** (§4) — dois structs `Stack` de módulos diferentes nunca
  unificam como o mesmo tipo nominal.
- `TypeParamType{Name string}` para `T` — nó distinto de `PrimitiveType` para
  não colidir com um struct real chamado `T`.

**Parser** (`internal/parser`):

- `<T, U>` opcional após o nome em `parseFunctionStatement` /
  `parseStructStatement`; os nomes entram em escopo de tipo durante o parse da
  declaração (params, retorno, campos e corpo).
- `parseAtomicType`: `IDENT` seguido de `<` em posição de tipo → `GenericType`
  com lista de argumentos.
- **Split de `>>` e `>=`**: dentro de lista de argumentos de tipo, o token
  `SHIFT_RIGHT` é dividido em dois `GT` (truque padrão C#/Java) —
  `Stack<Stack<int>>` parseia — e o token `GTE` é dividido em `GT` + `=`, para
  que `let s: Stack<int>= Stack([])` (sem espaço) não quebre o parse do tipo.

**Lexer**: nenhuma mudança.

## 7. Unificação e inferência

Uma função, dois usos: `unify(tipoEsperado, tipoConcreto, bindings)` — casamento
estrutural que percorre os construtores de tipo:

- `T` vs `int` → `T=int`; `T[]` vs `int[]` → `T=int`; `map[K,V]` vs
  `map[string,int]` → `K=string, V=int`; idem `ref`, `chan`, `func(...)->...`,
  `GenericType` (args ponto a ponto).
- Binding conflitante (`T=int` e depois `T=string`) → erro de unificação.
- **`T` não pode bindar um tipo `ref`** (erro de unificação na v1): a decisão
  own-vs-borrow do compilador é sintática sobre o tipo declarado (let `ref` sem
  own, `OP_SET_GLOBAL_BORROW`, auto-deref de argumentos), e `T = ref Ponto`
  mudaria a semântica de posse do corpo do template conforme a instanciação —
  exatamente a classe de sutileza que o trabalho de CoW/RC caça. A forma
  idiomática continua disponível: `func f<T>(r: ref T)` declara o `ref`
  explicitamente e `T` binda o tipo do elemento.
- Argumento de tipo desconhecido/`any` (ex.: literal `[]` vazio) não contribui
  binding — outro argumento ou o alvo do target-typing resolve.

Usos:

1. **Chamada direta**: argumentos como âncora; anotação do `let` envolvente como
   hint para `T` que só aparece no retorno (`let xs: int[] = vazio()`).
2. **Target-typing** (§3): unifica a assinatura do template contra o tipo de
   função alvo.
3. **Construtor de struct genérico**: campos (posicionais) contra argumentos;
   restante pela anotação do `let`.

`T` não resolvido ao final → erro (§9), nunca `any` implícito.

## 8. Cross-módulo e regra de escopo de definição

A instância nasce **no contexto do importador** — inevitável: o módulo não pode
compilar `processa<Ponto>` para um `Ponto` que só existe no importador. O
importador já parseia as declarações do módulo em compile-time
(`loadModuleDeclarations`); templates exportados entram no registry do
importador e instanciam no two-pass dele, como globals sob nome decorado.

**Regra de escopo de definição (v1, obrigatória):** todo identificador livre no
corpo de um template **deve resolver no top-level do módulo que o define** (ou
builtins, ou outros genéricos). Validado na instanciação contra o AST de
declarações do módulo, que o compilador já possui. Isso proíbe por construção a
captura de nomes do escopo do importador (quase dynamic scoping), que criaria
código dependente de um vazamento e inviabilizaria a evolução abaixo.

**Imports carregam tipos declarados (pré-requisito da inferência).** Hoje todo
nome importado entra no compilador com tipo apagado (`c.globals[name] = nil` —
compiler.go, caso `UseStmt`), o que faria `map(nums_importado, f)` falhar a
inferência mesmo em código correto. A v1 muda o predeclare de imports
(seletivo e `select *`) para registrar os **tipos declarados** dos exports,
extraídos do AST que `loadModuleDeclarations` já possui (`LetStmt.Type`,
assinaturas de função, structs). Benefício colateral: melhora a checagem
estática de todo código cross-módulo, genérico ou não.

**Templates não são acessíveis via namespace.** Na forma `use m` (módulo como
objeto), `m.processa(nums)` não tem como funcionar — o template não existe como
valor em runtime e o member access não carrega tipo estático. Erro de
compilação dedicado (§9): "template genérico não é acessível via namespace —
use `select`".

**Instâncias exportadas são inofensivas por construção.** Instâncias são
globals comuns e portanto entram no `ExportMap` do módulo (e em `select *`).
Como o nome decorado é qualificado pelo módulo definidor do template (§4),
duas instâncias só têm o mesmo nome se vêm do mesmo template com a mesma tupla
— mesmo código; sobrescrita no import é dedup idempotente, nunca shadowing de
código diferente. (A alternativa — filtrar nomes com `<` do
`OP_IMPORT_FROM_ALL` — exigiria mudança na VM e foi descartada.)

Consequência prática na v1: as dependências do template precisam estar visíveis
**também** no importador —

| Forma de import | Dependências do template visíveis? | Resultado |
|---|---|---|
| `use m select *` | sim — tudo entra nos globals | funciona sempre |
| `use m select f` (seletivo) | só se importadas junto | erro acionável: "adicione X ao select ou use select *" |
| `use m` (namespace) | n/a | template via `m.f` é erro de compilação dedicado |
| corpo autossuficiente (params + builtins + genéricos) | irrelevante | funciona sempre |

**Tabela de evolução** (cada passo só afrouxa exigências; nunca quebra código):

| | v1 (esta spec) | v2 (env binding) | v3 (privates) |
|---|---|---|---|
| Corpo pode referenciar | só nomes do módulo definidor | idem | idem |
| Importador precisa importar dependências | sim (`select *` resolve) | não — environment do módulo bindado na instância | não |
| Helper privado usado por genérico | n/a (não há privates) | funciona | funciona |

A v2 (bindar `ObjFunction.Environment` do módulo na instância, dando semântica
de resolução no local de definição, como Go/Rust) é rota registrada, não
compromisso desta entrega.

## 9. Catálogo de erros

Mensagens usam o **nome de exibição** (`soma<Ponto>`, sem o qualificador de
módulo do §4, que é identidade interna); o módulo definidor aparece por extenso
quando relevante ("de 'colecoes'"). Toda mensagem de erro de corpo carrega a
**cadeia de instanciação**:

```
[linha 12] em soma<Ponto> (instanciado na linha 40): operador '+' não definido para Ponto
```

| Erro | Exemplo | Mensagem (essência) |
|---|---|---|
| Inferência impossível | `let s = vazio()` sem anotação utilizável | "não foi possível inferir T em 'vazio' — anote o tipo" |
| Conflito de unificação | `indice_de(idades, "30")` | "T inferido como int (argumento 1) e string (argumento 2)" |
| Valor sem alvo concreto | `let g: func = map` | "função genérica 'map' precisa de tipo concreto — anote a assinatura completa ou chame diretamente" |
| Aridade de args de tipo | `Stack<int,string>` para `Stack<T>` | "'Stack' espera 1 argumento de tipo, recebeu 2" |
| Param de tipo fora de escopo | `T` usado fora da declaração | "tipo 'T' não declarado" |
| Genérico aninhado | `func` genérica dentro de função | "declaração genérica só é permitida no top level" |
| Template via namespace | `m.processa(nums)` com `use m` | "template genérico 'processa' não é acessível via namespace — use `select`" |
| `T` bindando ref | `identity(r)` com `r: ref Ponto` resolvendo `T=ref Ponto` | "T não pode ser um tipo ref — declare o parâmetro como `ref T`" |
| Escopo de definição (§8) | template referencia nome fora do módulo | "'processa<Ponto>' referencia 'validar', não declarado no módulo 'colecoes'" |
| Dependência não importada (§8) | import seletivo sem a dependência | "'processa<Ponto>' precisa de 'ajuda' de 'colecoes' — adicione ao select ou use select *" |
| Erro de corpo por instanciação | `soma(pontos)` | cadeia de instanciação, como acima |

## 10. Runtime — nada muda

- Nomes decorados são identidades nominais distintas: `areTypesCompatible`
  compara por string e a validação de tipos CoW de runtime funciona sem
  qualquer alteração.
- Instâncias de struct genérico são `StructStatement` comuns pós-substituição:
  construtor posicional, `JSONDynamicFields`, member access — tudo pelo caminho
  existente.
- `OP_CALL_STATIC` continua elegível: assinaturas de instâncias são exatas.
- Builtins (`append`, `length`, `contains`…) dentro de corpo genérico operam
  sobre tipos concretos pós-substituição — caminho normal.

## 11. Testes e benchmarks

**Unit** — `unify` (bindings, conflitos, tipos aninhados), substituição de AST
(todas as posições de tipo, auto-referência `ref Node<T>`), parser (`<T>`,
`GenericType`, split de `>>`).

**Compiler** —
- Memoização: duas chamadas `first(ints)` → uma instância.
- **Igualdade de bytecode** (o teste mais forte): `first<int>` monomorfizado
  emite a mesma sequência de opcodes que a versão escrita à mão — prova
  executável do custo-zero em runtime, e proteção permanente contra regressão.
- Ordem das declarações sintéticas (structs antes de funções; dependências antes
  de dependentes).

**E2E `.nx`** (entram no corpus do runner) — map/filter/reduce; `Stack<T>` com
push/pop via funções; `Node<T>` lista ligada; construtor com target-typing
(`Stack([])`); todas as posições de target-typing do §3 (incluindo unificação
bidirecional `map(nums, identity)`); cross-módulo (`select *`, seletivo com e
sem dependências); **dois módulos com templates homônimos instanciados na mesma
tupla via `select *`** (prova do dedup por nome qualificado, §8); inferência
sobre **dados importados** (prova do predeclare tipado, §8); **template dentro
de módulo** passando pelo validator e pelo `compileAndRunModule` (módulo que
usa os próprios genéricos internamente); genérico chamando genérico;
`Stack<Stack<int>>`; `Stack<int>=` sem espaço (split de `GTE`); recursão
genérica. **Negativos**: um exemplo por linha do catálogo do §9 — incluindo
`use m` namespace e `T` bindando ref.

**Benchmarks com gate** —
- `startup_generics`: variante do bench de startup com programa usando
  genéricos; gate contra regressão do caso Lambda.
- `generic_vs_hand`: laço quente com função genérica vs especializada à mão;
  gate de razão 1.0x (protegido também pelo teste de igualdade de bytecode).
- Protocolo padrão do projeto: intercalado, mediana, RESULTS.md.

**Dogfood** — módulo `collections` escrito em Noxy (map/filter/reduce/contains
genéricos): exatamente a abstração que era impossível, como prova de que a
assimetria com builtins acabou.

## 12. Fora de escopo (v1)

- **Constraints** (`T: comparable`) — checagem por instanciação cobre a v1.
- **Instanciação explícita em expressão** (`first<int>(x)`) — inferência +
  target-typing cobrem; sintaxe pode ser adicionada depois sem quebra.
- **Template como valor sem alvo concreto** — exige erasure; descartado.
- **Env binding cross-módulo (v2) e privates (v3)** — rota registrada no §8.
- **Genéricos aninhados em função** — top level apenas.

## 13. Riscos e mitigações

| Risco | Mitigação |
|---|---|
| Explosão de código (muitas instâncias) | memo por unidade de compilação; instâncias são pequenas; aceito na v1 — medir antes de otimizar |
| Compile time em programas grandes | detecção pula o pass 1 sem genéricos (mas carrega declarações de módulos em import seletivo — custo coberto pelo gate); gate `startup_generics`; saídas conhecidas (cache de descoberta) se o número aparecer |
| Qualidade de mensagens de erro | catálogo do §9 é requisito de aceite, com teste negativo por linha |
| Interação com CoW/validação runtime | identidades nominais decoradas usam o caminho existente; testes E2E de semântica de valor com structs genéricos |

# Changelog

## [0.7.1] - 2026-08-19

### Fixed (BREAKING) — Igualdade estrita de ref: `==`/`!=` nunca dereferencia implicitamente

- **O caso misto `ref` vs valor virou erro de compilação com hint.** Completa
  a regra iniciada pela correção de identidade abaixo: em `==`/`!=` um
  operando `ref` nunca é dereferenciado implicitamente. O `=` já recusava
  conversão implícita de ref nas duas direções pelo mesmo motivo — o
  significado deve ser evidente na sintaxe, nunca decidido por tipos que não
  aparecem no código. Antes, `x == y` perguntava identidade ou valor
  dependendo dos tipos estáticos dos dois lados; agora cada pergunta tem sua
  sintaxe:

  ```noxy
  ra == rb     // identidade de slot
  ra == null   // o próprio ref é nulo?
  *ra == 1     // valor apontado, explícito
  ra == 1      // ERRO: cannot compare ref int with int: a ref is never
               //       implicitly dereferenced in '=='
               //   hint: use '*ra' to compare the referenced value
  ```

- **A ambiguidade do null foi resolvida de graça pela mesma regra**: um ref
  VÁLIDO apontando para um slot que contém `null` não é mais "igual a null"
  — `r == null` pergunta sobre o próprio ref e `*r == null` sobre o valor
  apontado, duas perguntas que o deref implícito tornava indistinguíveis. O
  padrão comum `no.proximo != null` continua funcionando idêntico (o
  terminador é um ref nulo de verdade).

- Em runtime, `OP_EQUAL` deixou de resolver o caso misto: na fronteira
  dinâmica (ex.: campo `ref` lido via membro de `any`), ref vs valor é
  simplesmente diferente (`false`), e ref vs ref segue por identidade.

- Migração: comparações mistas quebram **em compilação**, com o hint
  apontando o conserto (`*r`). Código que usava `r == null` para perguntar
  "o slot apontado está vazio?" (padrão fill-null-slot) migra para
  `*r == null` — no repositório, isso alcançou exatamente 4 dos 170
  exemplos (`bst.nx`, `binary_tree.nx`, `linked_list.nx`,
  `test_explicit_deref.nx`), migrados nesta release; travessias com
  `cur != null`/`no.proximo != null` não mudam. Spec atualizada: §2.2
  (regra 7) e §2.3 (exceção 1 reescrita, com o par
  `r == null`/`*r == null`). Testes:
  `internal/compiler/ref_equality_strict_test.go`,
  `internal/vm/ref_equality_strict_runtime_test.go` e as suítes de
  semântica em `noxy_examples/`.

### Fixed (BREAKING) — `==`/`!=` entre dois `ref` compara identidade

- **`ra == rb` com ambos os lados `ref T` agora compara identidade de slot**,
  não o valor apontado — a semântica que a spec já descrevia em §2.2 regra 7
  (*"`ref` values compare by slot identity and are not dereferenced"*) e que
  a VM já implementava corretamente em `valuesEqual`
  (`internal/vm/stack.go`), com teste unitário próprio
  (`TestRefEqualityBySlotIdentity`). O bug estava um andar acima: o
  compilador emitia `OP_DEREF` nos **dois** operandos de qualquer operador
  binário, inclusive `==`/`!=`, então a comparação de identidade era
  inalcançável a partir de código Noxy. Na prática, dois refs para variáveis
  distintas que por acaso guardassem o mesmo valor davam `true`, e a
  igualdade passava a seguir o conteúdo das variáveis ao longo do tempo.

  ```noxy
  let a: int = 1
  let b: int = 1
  let ra: ref int = ref a
  let rb: ref int = ref b

  ra == rb    // antes: true   agora: false  (slots distintos)
  ```

  O caso **misto** (ref contra não-ref) chegou a ser preservado com
  auto-deref em runtime, mas ainda nesta release a regra foi completada e
  ele passou a ser rejeitado em compilação — ver "Igualdade estrita de ref"
  abaixo, que descreve a semântica final: `no.proximo != null` continua
  intacto (nulidade do próprio ref), e `contador == 10` migra para
  `*contador == 10`.

  Migração: código que dependia de `ref == ref` como comparação de valor deve
  dereferenciar explicitamente um dos lados (`*ra == *rb`) ou comparar contra
  o valor (`ra == b`). Spec atualizada em §2.2 (regra 7) e §2.3, que agora
  registra `==`/`!=` entre dois refs como exceção ao auto-deref (ao lado da
  segunda exceção, explicitada nesta mesma release — ver "Hint de deref na
  atribuição" abaixo).

### Added

- `noxy_examples/language_semantics_test.nx`: suíte de testes unitários de
  **semântica** da linguagem (134 asserções em 12 grupos — aritmética,
  curto-circuito, semântica de valor/CoW, `ref`, igualdade estrutural,
  strings/bytes, closures, funções de primeira classe, `defer`, genéricos,
  coleções, controle de fluxo). Diferente dos demais exemplos, que provam
  apenas que o programa executa sem erro, cada asserção afirma um
  comportamento observável e o arquivo sai com código 1 quando alguma falha,
  reportando todas de uma vez. Entra automaticamente no
  `run_all_tests_concurrent.nx`.

- **Ordenação lexicográfica de strings**: `<`, `>`, `<=`, `>=` agora aceitam
  duas strings, comparando byte a byte — dentro do invariante UTF-8, isso é
  idêntico à ordem por code point, como em Python. Antes, `"abc" < "abd"`
  compilava e estourava em **runtime** com `operands must be numbers`, embora
  a spec listasse os operadores de comparação sem restringi-los a números.
  Misturar string com número, ou ordenar `bytes`, continua erro de runtime —
  agora com a mensagem `operands must be numbers or strings`; `bytes` seguem
  deliberadamente fora da ordenação (a ponte explícita é `to_str`). `ref
  string` participa pelo valor apontado (auto-deref de expressão, já emitido
  pelo compilador). Spec atualizada em §8 (Comparison) e §12 (comparação
  byte-exata). Testes: `internal/vm/string_ordering_test.go` e o grupo
  "ordenacao de strings" da suíte de semântica.

- `noxy_examples/language_semantics_test2.nx`: parte 2 da suíte de semântica
  (131 asserções em 12 grupos — conversões numéricas, structs de resultado do
  `convert`, `fmt`, as três formas de import, stdlib `strings` por code
  point, ordenação de strings, `bytes` por octeto, arrays fixos e containers
  aninhados, listas ligadas com `ref Node`/`GNode<T>`, `ref` avançado
  (entrada de map, forwarding, escape de frame, closures compartilhando
  slot, `defer` com `ref`), `when`/`case` e a fronteira `any`). Mesmo
  contrato da parte 1: cada asserção afirma comportamento observável e o
  arquivo sai com código 1 em falha. Entra automaticamente no
  `run_all_tests_concurrent.nx`.

### Changed

- **Hint de deref na atribuição `x = r`**: atribuir um `ref T` a um alvo que
  espera `T` (variável local, global ou capturada, índice de array, valor de
  map, campo de struct) continua erro de compilação — atribuição não faz
  auto-deref, agora explicitado na spec (§2.3, exceção 2, com linha nova na
  tabela de Type-Based Assignment) — mas a mensagem passa a apontar o
  conserto: `hint: use '*r' to read the referenced value`, espelhando o hint
  já existente da direção inversa (`r = 50` → `use '*r = ...' to update the
  referenced value`). O hint só aparece quando o deref de fato consertaria o
  programa. A spec também corrige o exemplo `*r = ref z` de "Strict Type
  Safety", que documentava um erro que a implementação nunca emitiu: no alvo
  `*r =` o `*` já desfez a ambiguidade update/rebind, então um RHS `ref` é
  lido como em qualquer expressão (`*r = s` equivale a `*r = *s`) —
  comportamento agora fixado em teste na suíte de semântica. Testes:
  `internal/compiler/assign_deref_hint_test.go`.

## [0.7.0] - 2026-08-18

### Added

- **Genéricos por monomorfização**: funções e structs no top level podem
  declarar parâmetros de tipo entre `<>` (`func first<T>(arr: T[]) -> T`,
  `struct Stack<T>`), usáveis em qualquer posição de tipo (parâmetros,
  retorno, campos, corpo). Toda instanciação é por **inferência a partir do
  uso** — não existe sintaxe de instanciação explícita em posição de
  expressão (`first<int>(x)` não existe), o que mantém `<`/`>` sem
  ambiguidade com os operadores de comparação. Cada instanciação
  (`Stack<int>`, `Stack<string>`) é um tipo/função nominal **distinto**,
  monomorfizado em tempo de compilação: o bytecode gerado é idêntico ao de
  código especializado escrito à mão (provado por teste de igualdade de
  opcodes), sem overhead de runtime e **nenhuma mudança na VM**. Funções
  genéricas também circulam como valores de primeira classe via
  target-typing — anotação de `let`, retorno declarado, elemento de array,
  campo de struct, argumento de chamada, com unificação bidirecional quando
  o argumento também é genérico (`aplica(nums, identity)`). Cross-módulo,
  templates são importáveis via `select`/`select *` (dependências do corpo
  do template precisam estar visíveis no importador); acessar um template
  pelo namespace (`use m` seguido de `m.f(...)`) é erro de compilação
  dedicado. V1 não tem constraints (`T` é irrestrito; o corpo é checado por
  instanciação), é restrita ao top level (sem genérico aninhado em função) e
  não permite `T` bindar um tipo `ref` (idioma: declarar o parâmetro como
  `ref T`). Documentado em `docs/NOXY_LANGUAGE_SPEC.md` §6; spec de design em
  `docs/superpowers/specs/2026-08-18-generics-design.md`.

  - **Limitação documentada (v1)**: `print`/`%T` de uma instância de struct
    genérico mostra o nome **qualificado** (`<main::Caixa<int> instance>`),
    não o nome de exibição (`<Caixa instance>`) — vazamento cosmético
    aceito, `value.go` usa `Struct.Name` sem distinguir display name de
    identidade interna. Comportamento de v1 documentado, não um bug.

- Módulo `collections` (`noxy_examples/collections.nx`), escrito em Noxy
  puro usando os genéricos novos: `map_arr<A, B>`, `filter<T>`, `reduce<T,
  R>`, `contains_val<T>` — a mesma classe de abstração que antes só existia
  como builtin (`append`/`length`/`contains`), agora escrevível em código de
  usuário. (`map` é palavra reservada de tipo — `map[K, V]` — por isso a
  função de transformação chama-se `map_arr`, não `map`.)

### Changed (BREAKING) — imports tipados

Nomes importados (`use m select ...`/`select *`) passam a carregar o **tipo
declarado** dos exports em vez de entrar apagados (`nil`) no compilador —
pré-requisito para a inferência de tipo genérico funcionar sobre dados e
funções vindos de outro módulo (`primeiro(numeros_importado)`).

- **Código cross-módulo dinâmico que hoje compila com tipo apagado pode
  passar a falhar em compile-time.** Um argumento cujo tipo estático era
  `nil` (permissivo por padrão) agora carrega um tipo real — inclusive `any`
  explícito, que a checagem **estrita** de argumento de chamada (assinatura
  exata) rejeita onde antes não havia checagem nenhuma. Migração: anotar o
  valor num `let` intermediário com tipo concreto antes de passá-lo a um
  parâmetro de assinatura exata (a checagem de `let` é permissiva e emite
  guarda de tipo em runtime), ou corrigir o erro de tipo latente que a
  checagem revelou.
- Um exemplo do corpus foi corrigido por essa via:
  `noxy_examples/mergesort_with_slice.nx` passava o retorno de
  `array_utils.slice` (declarado `any`, curry dinâmico) direto como
  argumento de `merge_sort(arr: int[])`; o fix introduz `let left_slice:
  int[] = slice(...)` antes da chamada, em vez de mudar `array_utils.nx`
  (stdlib empacotada com o compilador, fora do escopo de um fix de corpus).
- Critério de aceite: o corpus `.nx` existente inteiro (167 arquivos)
  continua compilando e passando após o fix acima —
  `noxy_examples/run_all_tests_concurrent.nx` reporta 167/167.

### Changed

- A variável de `for ... in` passa a receber o **tipo estático do elemento**
  da coleção quando ele é conhecido (array → tipo do elemento; map → tipo da
  chave, que é o que o laço produz). Antes ela entrava sempre sem tipo.
  Requisito dos genéricos — sem isso, `identity(v)` dentro de um for-each
  chega à unificação sem âncora e `T` fica sem binding —, mas o efeito é
  geral: **pode revelar erros de tipo latentes** em código que hoje compila,
  mesma classe (e mesma migração) da mudança de imports tipados acima.
  Coleção de tipo desconhecido continua produzindo variável sem tipo.

### Fixed

- **REPL preserva structs declarados entre linhas.** Cada linha recebia um
  mapa de structs novo, então um `struct Point ... end` digitado numa linha
  simplesmente não existia na linha seguinte. Bug **pré-existente** (não
  introduzido pelos genéricos), corrigido junto porque a mesma linha de
  código passou a persistir também o registry de templates genéricos da
  sessão (`cmd/noxy/main.go`, spec §5).

- Operadores aritméticos (`+`, `-`, `*`, `/`, `%`) sobre structs agora são
  erro de **compilação**, não mais crash de runtime. Antes, `a + b` com
  `a`/`b` struct compilava silenciosamente e só estourava em runtime
  (`operands must be numbers, strings or bytes`, `internal/vm/executor.go`)
  quando a linha executava — inclusive dentro do corpo de uma instância
  genérica monomorfizada, onde o erro escaparia por completo da cadeia de
  instanciação (que só envolve erros de compilação). Nenhum programa válido
  existente depende desse comportamento — a VM sempre crashava nesses casos
  — então nenhum programa quebra; a checagem nova (`internal/compiler`)
  só torna o erro visível mais cedo, com a linha do próprio operador.

## [0.6.0] - 2026-08-18

### Added

- Suíte de benchmark cross-runtime em `benchmarks/cross_runtime/`, comparando o
  VM com CPython 3.13, Lua 5.4 e Go nativo na mesma carga. Sete benches
  (`startup`, `loop_arith`, `map_churn`, `mandelbrot`, `string_ops`,
  `bubblesort`, `fib`) escritos em Noxy e Python; `startup`, `loop_arith` e
  `fib` também em Lua e Go, como calibração — o Lua é o comparável direto
  (bytecode puro, sem JIT, sem inline cache) e o Go é o teto do hospedeiro.
  Cada implementação imprime a mesma linha `CHECKSUM:` e o harness
  (`run_cross_runtime.ps1`) aborta se divergirem, então a comparação é sempre
  da mesma carga. Medição intercalada entre runtimes, mínimo de N execuções em
  vez de mediana (sob carga a distribuição só tem cauda à direita) e cópia dos
  fontes para disco local — medir de dentro do repo, que fica em OneDrive,
  inflava os tempos em ~2x por filtro de sync e antivírus no read. Resultado em
  `benchmarks/cross_runtime/results/`: descontado o piso de processo, o Noxy
  está 1,8x a 9,6x atrás do CPython e ~14x a ~15x atrás do Lua, com o custo
  concentrado em chamada de função, acesso indexado a array e operação de
  string; o despacho de bytecode puro fica a 1,8x do CPython e o startup ganha
  dele (63ms contra 94ms). O ranking se repetiu em cinco rodadas; as
  magnitudes variam, porque o número líquido amplifica ruído do piso de
  processo. Não altera comportamento da linguagem.

- Flags `--cpuprofile`/`--memprofile` na CLI (`cmd/noxy/main.go`), para
  gravar profile de CPU e de heap do programa Noxy em execução — parte da
  infraestrutura de medição da fase 1 de perf de dispatch/chamadas (ver
  seção Performance abaixo).

### Performance

- Fase 1 de perf de dispatch e chamadas (branch `perf/vm-dispatch-fase1`,
  spec `docs/superpowers/specs/2026-08-17-vm-perf-static-typing-research.md`).
  `OP_CALL_STATIC` (`internal/vm/calls.go`) pula `validateParameterModes` em
  chamadas de closure cujo call site o compilador provou estaticamente
  (`isExact`, `internal/compiler/compiler.go`); o `CallFrame` de cada chamada
  passou a reusar um array de valores em vez de alocar um novo a cada
  chamada (allocs/op de uma chamada típica: 1012 → 10). Mais 13 opcodes
  especializados de despacho, todos por APPEND ao fim do bloco de constantes
  de `internal/chunk/chunk.go`: seis pares comparação+salto fundidos para
  inteiro (`OP_JUMP_IF_LT_INT`/`LE`/`GT`/`GE`/`EQ`/`NE_INT`),
  `OP_INC_LOCAL_INT` (funde `i = i +- K` numa soma direta no slot, sem
  tráfego de pilha) e seis opcodes `_FLOAT` espelhando os `_INT` de
  aritmética e comparação quando os dois lados são estaticamente float.

  Medido nos três benches alvo (`benchmarks/cross_runtime/`, mínimo de 9
  execuções intercaladas): `fib` 804,9→380,8ms (**~2,1x**), `loop_arith`
  519,4→319,4ms (**~1,63x**), `mandelbrot` 428,6→246,2ms (**~1,74x**) —
  todos acima da estimativa por task. No comparativo cross-runtime contra
  CPython 3.13, `fib` foi de 7,9x atrás para 3,4x, e `loop_arith` de 1,8x
  para ~1,1x (gap praticamente fechado). RC intocado — nenhum funil de
  retain/release muda de lugar ou de contagem; os opcodes novos só operam
  sobre escalares (int/float), que não participam de RC.
  `go vet ./...` limpo, `go test ./...` verde, `-race` verde em
  `internal/vm` e `internal/value`, corpus de exemplos 164/164 idêntico.

  **Regressão aceita e rastreada, não corrigida nesta fase:**
  `bench_share_mutate` (pior caso de CoW por construção — `let b = a`
  seguido de mutação de um array grande) piorou em média **+8,7%** numa
  bissecção dedicada (3 sessões intercaladas, piso de ruído medido em
  ~1,2-1,4%, então o efeito é real e reproduzível, não ruído). O reuso de
  `CallFrame` é o maior contribuinte isolado, mas não fecha a conta sozinho —
  o padrão é compatível com pressão de GC difusa espalhada por vários
  commits da fase, e nenhum símbolo da fase 1 aparece no profile do bench.
  Detalhes completos: `benchmarks/RESULTS.md`, seção "develop (f107508) ×
  fase 1 de dispatch e chamadas".

### Fixed

- `break` dentro de `for ... in` voltou a sair do laço. O branch do
  `ForStatement` em `internal/compiler/compiler.go` empilhava o laço em
  `c.loops` mas nunca fazia o pareamento que o `while` já fazia: não patchava
  os `loop.BreakJumps` nem desempilhava o laço no fim. O jump do `break`
  ficava com o operando de placeholder (`0xffff`), o `ip` saltava para fora do
  chunk e o laço principal do executor (`internal/vm/executor.go`, guarda
  `ip >= len(c.Code)`) retornava sucesso — o programa terminava em silêncio,
  com exit 0 e saída truncada, em vez de continuar depois do laço. Como o laço
  também nunca era desempilhado, um `break` posterior mirava o `for` já
  encerrado (mesmo jump nunca patchado) e `break` fora de qualquer laço deixava
  de ser erro de compilação. Bug pré-existente (reproduz em 0.4.0 e 0.5.0),
  descoberto na verificação final do PR #33. `#compiler` @estevaofon

- `if cond then break end` na mesma linha voltou a fechar no `end` correto.
  `parseBreakStatement` (`internal/parser/parser.go`) avançava um token depois
  do `break`, violando o contrato dos laços de statement
  (`ParseProgram`/`parseBlockStatement`/`parseCaseBody` esperam `curToken` no
  último token do statement e fazem o `nextToken` eles mesmos). Na forma
  multilinha o `NEWLINE` absorvia o excesso e o bug não aparecia; na forma
  inline o `end` do `if` era engolido e o bloco só terminava no `end` do laço
  externo — `SyntaxError: expected 'end' after for loop, found EOF`. Afetava
  também `while` (o idioma usado nos exemplos de `docs/concurrency.md`).
  `#parser` @estevaofon

## [0.5.0] - 2026-08-17

### Performance

- Validação de tipos em runtime passou a confiar na tag `RuntimeType` em O(1)
  (`internal/vm/runtime_type_validation.go`). Antes, toda chamada com
  assinatura estaticamente conhecida varria o contêiner inteiro do argumento a
  cada chamada — duas vezes (prova + aplicação), inclusive através de `ref` e
  em funções que só leem — o que tornava O(N²) qualquer laço quente que
  passasse um map/array grande para função tipada do mesmo módulo. Era o custo
  que mascarava o ganho do `ref` no NoxyDB: corrigido o CoW, o laço de puts
  continuava quadrático pela varredura. Agora, tag presente e aceita vale como
  prova (os elementos foram validados quando a tag foi gravada e as escritas
  tipadas validam na entrada); a primeira marcação (tag ausente) continua
  varrendo tudo antes de gravar a tag, e conflito de tag continua rejeitado.
  Medido no repro (struct→struct→map, helper `ref` chamado antes de cada put):
  N=4000 caiu de 6.715ms para 157ms — de quadrático para flat. Benchmark novo
  `benchmarks/bench_typed_call_map.nx` ancora o padrão (2.689ms → 145ms a
  N=2500, checksums idênticos); contrato fixado em
  `internal/vm/runtime_type_validation_test.go` (confiança na tag, primeira
  marcação ainda varre, conflito rejeitado).

- Unicidade de arrays/maps/instâncias passou a ser decidida por um contador
  `Owners` de referências duráveis (`internal/value/cow.go`,
  `IsShared = Owners > 1`) no lugar do bit sticky `Shared`, que só ligava e
  nunca desligava — qualquer passagem por valor condenava o contêiner a
  clonar para sempre, mesmo depois de o alias que motivou a marca deixar de
  existir (compartilhamento morto). O clone agora só acontece quando existe
  um segundo dono vivo no momento da mutação; o modelo é o CoW do Swift
  adaptado a bytecode com GC (spec
  `docs/superpowers/specs/2026-08-17-cow-rc-uniqueness-design.md`, fase 1).
  Caso emblemático (NoxyDB): um helper `database_file(db)` chamado por valor
  dentro do laço de puts marcava `db`/`state`/o map de payloads como
  compartilhados para sempre — 3 clones por put, O(N²). Benchmark novo
  `benchmarks/bench_value_call_mutate.nx` ancora o padrão do NoxyDB (helper
  por valor em laço de mutação com map crescendo): **−93,5%** na intercalada
  final (mediana de 9), de quadrático para flat (~1,5s → ~100ms a N=2500); o
  laço de puts por valor caiu de 3 clones/put para O(1) clones no laço
  inteiro (`TestByValueCallLoopClonesO1AfterFlip`: 600 → ≤8 clones em 200
  iterações). Corpus de exemplos 130/130 idêntico em todas as verificações
  (após o flip e após cada round de correção); `go test ./...` verde,
  `-race` verde em `internal/value` e na suíte completa de `internal/vm`
  (contador `Owners` é atômico; o requisito é o mesmo do ARC sob tasks
  paralelas). Custo do bookkeeping: mesmo após a limpeza do bit morto,
  `bench_map_churn` (+10,9%) e `bench_spawn_sum` (+10,4%) seguem acima do
  gate ≤~5% da suíte intercalada — escrita intensa em map paga inc/dec por
  elemento em cada operação, e os laços quentes dos workers de task pagam a
  passagem pelos funis de RC no rebind de locais escalares (Retain/Release
  são no-ops em primitivos; o custo é o funil por iteração, não contagem).
  Aceito e documentado como o preço do RC nesta fase; as válvulas
  apontadas para quando isso for revisitado: drops precisos da fase 2 e
  elisão de pares inc/dec no mesmo bytecode (spec §8, risco 3), mais um
  fast path para stores de valores escalares apontado na investigação da
  fase 1 (fora do texto da spec). Tabela completa e interpretação em
  `benchmarks/RESULTS.md`.

### Changed

- `benchmarks/RESULTS.md` virou registro corrido de comparações (mais recente
  primeiro): ganhou a seção da validação O(1) pela tag (PR #31) com a tabela
  intercalada develop × candidato — `bench_call_light` −97,1%,
  `bench_typed_call_map` −94,3% (mediana de 5 intercaladas), `share_mutate`
  −65,7%, sem regressões — e o "achado colateral" da seção CoW foi anotado
  como resolvido, apontando para a seção nova.

### Added

- Site publicado (`docs/index.html`) ganhou a seção **What's New in v0.4.0**
  (`#whats-new`, com link no menu e badge no hero): seis cartões de migração
  com o antes/depois de cada breaking change da semântica de valor —
  atribuição não aliasa, leitura de contêiner copia, mutação aninhada não
  vaza, `==` estrutural, `spawn` sem exceção de identidade e `append`
  guardando valor independente. Entraram também um cartão de feature
  (*Value Semantics*), um cartão de sintaxe (*Value Semantics & ref*) e uma
  aba de exemplo executável, além de links para spec, `REF_SEMANTICS.md`,
  guia de concorrência e CHANGELOG no rodapé (que apontavam para `#`).
  Todos os números citados nos cartões foram medidos rodando os trechos.

- `docs/SHOWCASE.md`: vitrine dos projetos reais escritos em Noxy, começando
  pelo [NoxyDB](https://github.com/estevaofon/NoxyDB) — banco de dados
  documento-chave/valor persistente escrito inteiramente em Noxy. Cada entrada
  descreve o projeto e mapeia quais áreas da linguagem e da stdlib ele exercita
  (CoW, JSON nativo, `http_server`, concorrência), com um template no fim para
  novos projetos. O arquivo entrou no `exclude:` do Jekyll (`docs/_config.yml`):
  fica no repositório, fora do site publicado.

### Fixed

- Exemplos do site voltaram a compilar na 0.4.0. Quatro dos oito exemplos da
  landing page estavam quebrados — não era regressão da CoW, mas código que
  nunca foi reexecutado depois da semântica de referência: *Binary Tree*
  (`cannot assign Node to ref Node`), *Linked List* (`ref Node(...)` não é
  endereçável), *HashMap* (`...` literal no array e o mesmo `ref` de
  temporário) e *HTTP Server* (`expected ref HttpServer, got object` em
  runtime, faltava `ref server`). O exemplo de *Concurrency* definia `main()`
  sem nunca chamar e imprimia nada, e o cartão *Self-Referencing* repetia o
  `ref` de temporário. Todos foram reescritos no idioma 0.4.0 (campos de
  struct por valor, travessia por `ref`, `ref current.next` para o cursor) e
  verificados executando cada trecho extraído do HTML: dos 27 blocos, os 23
  que são programa completo rodam e os 4 restantes são fragmentos ilustrativos
  de cartão (sem struct/função ao redor, por design).

- Build do GitHub Pages voltou a passar: `superpowers/` entrou no `exclude:`
  do Jekyll (`docs/_config.yml`). Os planos internos em
  `docs/superpowers/plans` contêm literais de struct no estilo Go (`{{...}}`)
  que o Liquid interpretava como variável não terminada, derrubando o build
  (`Liquid::SyntaxError` em `2026-08-14-runtime-defer-unwind.md:308`). Os
  documentos continuam no repositório; apenas saem do site publicado.

- `has_key` e `keys` entraram na allowlist de natives só-leitura do CoW
  (`internal/vm/cow_natives.go`). Sem elas, passar um map para qualquer um dos
  dois o marcava `Shared` e a mutação seguinte clonava a estrutura inteira —
  o padrão ler-antes-de-escrever (o laço normal de banco/cache/índice) ficava
  O(N²). Medido no repro: `has_key`+`put` a N=5000 caiu de 5.807ms para 15ms,
  agora linear e igual à leitura por índice. Regressão ancorada no contador de
  clones (`TestHasKeyThenWriteDoesNotClone`, `TestKeysThenWriteDoesNotClone`),
  com caso negativo garantindo o default conservador para natives fora da
  allowlist (`TestUnlistedNativeStillMarksArgs`).

- Escrita através de `ref` para um nó com exatamente um dono durável agora
  acontece **in-place e é visível** — sob o bit sticky antigo, o bind por
  valor que criava um segundo dono temporário ligava a marca para sempre, e a
  mutação seguinte através do `ref` podia clonar em vez de mutar, perdendo a
  escrita. O teste committado pina o valor correto: **107** (antes: 50 —
  escrita perdida) para o mesmo programa (lista encadeada, escrita via
  `setit(ref n, v)` seguida de escrita via `let u: ref Node = ...;
  u.valor = 77`). A investigação da Task 7 confirmou adicionalmente que o
  comportamento antigo era dependente da forma do vínculo (o próprio
  merge-base já imprimia 107 quando o mesmo alias era escrito só via
  parâmetro `ref`, sem a passagem por valor intermediária) — variantes
  registradas no relatório da task, não na suíte. O resultado correto pelo
  contrato CoW 0.4.0 é 107 em qualquer forma (§2, regra 6: mutação através
  de `ref` é sempre visível). Pinado por
  `TestRefWriteToUniquelyOwnedNodeMutatesInPlace`.

## [0.4.0] - 2026-08-16

### Changed (BREAKING) — Semântica de valor com copy-on-write

Arrays, maps e structs passam a se comportar como **cópias profundas
independentes em qualquer vínculo sem `ref`** — atribuição, chamada, leitura
e escrita de contêiner, canais, `spawn`/`spawn_task` e captura de `defer`.
A implementação é copy-on-write: nada é copiado no vínculo; o clone acontece
lazily, um nível por vez, na primeira mutação de um valor compartilhado.
`ref` vira o único mecanismo de compartilhamento da linguagem. Spec de design:
`docs/superpowers/specs/2026-08-16-cow-value-semantics-design.md`.

- **`let b = a` e `x = y` deixam de aliasar.** Antes, mutar `b` era visível
  em `a`. Migração: quem dependia do aliasing usa `ref`.
- **Ler de contêiner deixa de aliasar** (`let p = arr[0]; p.x = 1` não altera
  mais `arr[0]`). Migração: mutar pelo caminho (`arr[0].x = 1`) ou `ref`.
- **Mutação aninhada via parâmetro não vaza mais.** A antiga cópia rasa
  copiava só o contêiner externo; `f(a)` seguido de `a[0].x = 1` dentro de
  `f` era visível no chamador. Agora o parâmetro é independente em qualquer
  profundidade. Migração: declarar o parâmetro `ref`.
- **`append(dest, item)` guarda um valor independente** — mutar `item` depois
  não altera `dest`. O alvo de `append`/`pop`/`delete` compartilhado é
  clonado antes da mutação (CoW pelo slot).
- **`spawn` perde a exceção de identidade**: seus argumentos seguem a mesma
  semântica de valor de `spawn_task` e chamadas normais. `chan_send` entrega
  valor independente — dados passados por canal ficam livres de race por
  construção. Migração para estado compartilhado: globals coordenados ou
  canais.
- **`==`/`!=` de compostos vira estrutural** (recursivo por conteúdo);
  `[1, 2] == [1, 2]` agora é `true`. Refs comparam por identidade de slot e
  não são dereferenciados. Antes, compostos comparavam por identidade de
  ponteiro — instável através de chamadas. Migração para identidade: comparar
  refs ou uma chave própria.
- **Natives com assinatura mantêm a cópia ansiosa** dos args compostos
  não-ref: o corpo em Go muta fora do copy-on-write do bytecode, e a cópia é
  a única proteção do chamador. Natives sem assinatura marcam os args
  conservadoramente; uma allowlist auditada de natives só-leitura
  (`internal/vm/cow_natives.go`) evita o custo onde é provado desnecessário.
- **Limitação documentada**: um `ref` criado para dentro de um contêiner
  (`ref arr[0]`, campo) fixa a identidade do contêiner na criação (a base é
  unicizada nesse momento). Se o contêiner for copiado DEPOIS, escrita
  através do ref pré-existente ainda é visível pela cópia não materializada.
  Crie refs depois de compartilhar. Ver `docs/REF_SEMANTICS.md` §8.
- **Leniência preservada**: campos tipados `ref T` que recebiam valores
  planos (o checker antigo não validava campos quando a base era ref)
  continuam aceitos, e o caminho de mutação tolera valor plano em slot de
  tipo ref — programas como `noxy_examples/stack.nx` e `linked_list.nx`
  seguem rodando sem alteração.

### Added

- Bit `Shared` atômico em arrays/maps/instâncias + opcodes de caminho de
  mutação (`OP_GET_LOCAL_MUT`, `OP_GET_GLOBAL_MUT`, `OP_GET_UPVALUE_MUT`,
  `OP_GET_INDEX_MUT`, `OP_GET_PROP_MUT`, `OP_DEREF_MUT`, `OP_MARK_SHARED`)
  com lowering de lvalues no compilador (`compileLValueBase`).
- Contador de clones CoW (`vm.CloneCountValue`) para testes e diagnóstico —
  chamadas só-leitura custam 0 clones (coberto por teste).
- Suite de benchmarks em `benchmarks/` com harness (`run_benchmarks.ps1`),
  comparação de corpus (`compare_examples.ps1`) e resultados antes/depois
  commitados (`benchmarks/RESULTS.md`).
- `noxy_examples/shallow_copy.nx` reescrito como demonstração da semântica
  de valor.

## [0.3.0] - 2026-08-16

### Changed (BREAKING)
- **`to_int` e `to_float` levantam erro** em vez de devolver `0` / `0.0` quando
  a conversão é impossível. `to_int("abc")` era indistinguível de
  `to_int("0")`. A forma leniente por `strconv.ParseFloat` também some:
  `to_int("12.75")` devolvia `12` e agora levanta, como qualquer outra string
  decimal. Migração: chamadas sobre entrada não confiável passam a usar
  `to_int_result` / `to_float_result` do módulo `convert`, com ramo explícito
  de falha.
- **`index_of` devolve índice em caractere**, não em byte, alinhado a
  `substring`, `char_at`, `length` e `slice`. Texto ASCII não é afetado.
- **Funções de `strings` recusam `bytes`** e apontam `to_str`. Antes operavam
  sobre a forma de exibição `b"..."`.
- **`ord` devolve o code point** de uma string de um caractere e exige
  exatamente um caractere. Antes devolvia o primeiro byte UTF-8.
- **Toda `string` Noxy contém UTF-8 válido.** `to_str` levanta erro sobre bytes
  inválidos em vez de retaggear sem inspeção. Antes, o byte inválido sobrevivia
  na string mas decodificava como U+FFFD em toda operação por caractere:
  fatiar `h` + `0xFF` + `i` reescrevia três bytes como cinco, em silêncio e sem
  volta. Migração: use `io.read_bytes`, mantenha o valor como `bytes`, ou use
  `strings.is_valid_utf8` antes de decodificar.
- **`io.read`, `io.read_lines` e `sqlite.query`** reportam conteúdo não-UTF-8
  pelos campos `ok` e `error` que já possuíam, em vez de levantar.
  `io.read_bytes` e `net.recv` seguem inalterados como as saídas brutas.
- **`sys.exec_output` e `sys.getenv`** passam a reportar conteúdo não-UTF-8 da
  mesma forma, mas por um campo `error` que **não existia antes** — ver o item
  seguinte sobre a forma dos structs.
- **`SysResult` e `EnvResult` ganham um campo `error: string`** (em
  `internal/stdlib/sys.nx`). `SysResult` passa de
  `(exit_code, output, ok)` para `(exit_code, output, ok, error)` e
  `EnvResult` de `(value, ok)` para `(value, ok, error)`. Qualquer código que
  construa um desses structs posicionalmente precisa passar o campo novo;
  acesso por campo (`r.ok`, `r.output`) não é afetado.
- **Carregar um `.nx` que não seja UTF-8 válido falha** com erro nomeando o
  arquivo, em vez de lexar bytes mal formados.
- **O script de entrada passado na linha de comando não está coberto por este
  invariante.** `cmd/noxy/main.go` lê o arquivo principal por um caminho
  separado do carregamento de módulos; um `.nx` de entrada com bytes
  inválidos ainda é lexado sem checagem. Módulos importados via `use` são
  validados. Esta lacuna fica registrada como trabalho futuro.

### Removed (BREAKING)
- **A palavra-chave `global` foi removida do léxico.** Já não fazia parte da
  sintaxe da linguagem — o parser sempre a rejeitava — mas o lexer ainda a
  reconhecia como palavra reservada, produzindo `invalid syntax "global"` em
  vez do diagnóstico comum de `let` ausente. Declare variáveis de topo com
  `let`; uma função pode reatribuir um `let` de topo normalmente.

### Added

- `defer call(...)` with immediate argument capture and frame-level LIFO cleanup across functions, scripts, modules, loops, and spawned functions.
- Portable positive TCP read, write, and accept timeouts through `net.settimeout`.
- **Módulo `convert`** com `to_int_result`, `to_float_result`, `IntResult` e
  `FloatResult`.
- **`strings.char_code(s)`**, inverso de `from_char_code(code)`.
- **Guards de arquitetura**: nenhum native registrado duas vezes, nenhum marcador
  de debug em fonte de produção, fontes embarcados da stdlib em UTF-8 válido.
- **`strings.is_valid_utf8(b: bytes)`**, o caminho de checar-antes-de-decodificar.
  O parâmetro é estritamente `bytes`: passar uma `string` — inclusive através
  de `use strings select *` — levanta erro de runtime nomeando o tipo
  recebido.
- **`http_server` agora faz framing incremental** (ponto 13 do PR #17): lê o
  bloco de headers e o corpo `Content-Length` até completar, em vez de um
  único `socket_recv`. Ver `docs/HTTP_SERVER.md`.
  - `HttpServer` ganha `max_header_bytes`, `max_body_bytes`,
    `header_timeout_ms`, `body_timeout_ms`, `write_timeout_ms` e
    `read_chunk_bytes`, com defaults documentados e proteção contra
    slowloris por deadline absoluto de cada fase.
  - **`bind_server(server: ref HttpServer) -> bool`** separa o bind do loop de
    accept e escreve a porta real de volta em `server.port`, tornando a porta
    `0` utilizável.
  - Requisições inválidas recebem 400, 408, 413, 414, 431, 501 ou 505 com
    `Content-Length` byte-exato, em vez de desconexão silenciosa.
  - **`count_header(headers, count, name)`** em `http_parser`.
- **Escapes `\uNNNN` e `\u{...}`** em todo literal de string, com o codepoint
  validado — surrogates e valores acima de `0x10FFFF` são rejeitados na
  léxica.
- **Escape `\xNN`** em literais de bytes, para escrever um byte bruto.
  Recusado em literal de string, onde construiria UTF-8 inválido.
- **`strings.codes(s) -> int[]`**, que decodifica a string uma vez e devolve
  todos os codepoints. Um laço com `char_at` redecodifica a string inteira a
  cada chamada e é quadrático no seu tamanho.

### Fixed

- **REPL e `input()` congelavam em terminais com raw mode vazado no Windows.**
  O modo de entrada do console é estado compartilhado do terminal: quando um
  programa em raw mode (ex.: um jogo via `noxy-plugin-terminal`) morria sem
  restaurar, `ENABLE_LINE_INPUT`/`ENABLE_ECHO_INPUT` ficavam desligados e o
  próximo REPL naquele terminal mostrava `>>> ` mas nunca recebia a linha —
  digitação invisível, Enter sem efeito, "resolvia" só abrindo outro terminal
  (o PowerShell mascara o problema porque o PSReadLine redefine o modo a cada
  prompt). Agora `internal/console.EnsureLineInput` normaliza o modo do console
  antes de cada prompt do REPL e de cada `input()`.
- Normal returns and runtime failures now share safe frame unwinding, preserving primary errors while collecting observable cleanup failures.
- `net.setblocking(sock, true)` now restores indefinite blocking, while the deprecated `false` branch remains a compatibility no-op.
- `net.poll`/`net_select` now reports non-consuming readiness through independent 64-entry read, write, and error sets, with immediate zero-time polls, one global positive timeout, portable EOF/hangup projection, and concurrent-close wakeups that omit detached resources.
- `net.listen(host, 0)` now returns the operating-system-assigned port in `Socket.port`, allowing collision-free loopback listeners.
- **`parse_url` cortava host e path no lugar errado** para autoridade com
  caractere não-ASCII: `http://münchen.de/path` devolvia host `münchen.de/` e
  path `path`.
- **Saída de debug embarcada foi removida de quatro pontos.** `net_send`
  imprimia uma linha em stdout para um argumento malformado, e o cliente HTTP
  imprimia uma linha a cada requisição, corrompendo a saída de qualquer
  programa que o usasse. Havia ainda dois resquícios mortos: um comentário
  marcado como debug em `executor.go` e um `printf` de debug comentado em
  `parser.go`. Um guard de arquitetura agora falha o build se qualquer
  marcador voltar.
- **`strings_contains` e `strings_replace` estavam registrados duas vezes**, com
  a segunda cópia inalcançável — uma correção aplicada a ela seria
  silenciosamente descartada.
- **24 linhas de comentário da stdlib**, em `http.nx`, `strings.nx`, `time.nx`
  e `io.nx`, tiveram os acentos restaurados após uma conversão de encoding
  com perda.
- **Vincular um valor `bytes` como parâmetro SQL corrompia o conteúdo.**
  `sqliteParameter` não tinha caso para `bytes` e caía no formato de exibição
  de depuração, `b"..."`, adicionando um prefixo e um sufixo espúrios a todo
  valor gravado. Independente de validade UTF-8. Corrigido para passar o
  conteúdo bruto.
- **Struct com campo de tipo importado nunca compilava.** `use pkg select *`
  vinculava o tipo importado como valor, mas nunca ensinava ao compilador o
  layout de campos desse tipo em outra unidade de compilação. Uma struct
  local com um campo desse tipo — exatamente a forma de
  `HttpServer.listener: Socket` — construía metadado de tipo em runtime
  incompleto, e toda chamada ao construtor levantava
  `struct constructor has incomplete runtime type metadata`,
  incondicionalmente. `new_server()` nunca funcionou antes desta correção.
- **Resolução de global/propriedade/import/closure truncava acima de 255
  constantes no pool.** `OP_GET_GLOBAL`, `OP_SET_GLOBAL`, `OP_GET_PROPERTY`,
  `OP_SET_PROPERTY`, `OP_IMPORT`, `OP_CLOSURE`, `OP_REF_GLOBAL`,
  `OP_REF_PROPERTY` e `OP_CONTEXT_REF_PROPERTY` codificavam o índice no pool
  de constantes em um único byte. Como `AddConstant` nunca deduplica — cada
  literal de string e cada referência a um nome global reivindica seu
  próprio slot — um chunk moderadamente grande já ultrapassa 255 constantes;
  a partir daí o índice `byte(256)` volta para `0` silenciosamente, e a
  instrução lê a constante errada. Observado como
  `undefined global variable 'strings'` num exemplo padrão de 85 linhas, e
  como panic de asserção de tipo em casos mais adversos. Todos os nove
  opcodes agora codificam um índice de 16 bits.
- **`bind_server` reescrevia `server.running` para o mesmo valor já
  presente**, uma escrita desnecessária que corria (`-race`) contra leitura
  concorrente de outro campo da mesma struct no padrão bind-depois-spawn que
  a própria função existe para viabilizar. O loop de accept de `serve()`
  também deixou de reler `server.running` a cada iteração; ele reage à
  falha de `accept()` — já sincronizada no registro de sockets — quando
  `stop_server` fecha o listener de outra goroutine, em vez de repetir a
  leitura de um campo comum de struct, que não é sincronizado entre threads
  como globals e maps.
- **`response_error` declarava contagem de runas como `Content-Length`**,
  subestimando o tamanho de qualquer mensagem não-ASCII.
- **Handler que falhava vazava o socket do cliente.** A conexão agora fecha
  por `defer` em todo caminho de saída, incluindo falha do handler.
- **`get_header` cortava valores contendo `:`**, então
  `Host: example.com:8080` devolvia `example.com`.
- **Escape unicode de quatro dígitos era lexado como texto literal.** Uma
  sequência ANSI de limpar tela escrita como ESC escapado por `\u` saía
  como oito caracteres visíveis. `conway.nx`, `conway_random.nx` e
  `langtons_ant.nx` usavam esse padrão e os três imprimiam o texto de escape
  em vez de limpar a tela.

## [0.2.0] - 2026-08-13

### Added

- Observable strict JSON encoding through `json.dumps_result`, with explicit
  success, payload, and error fields. `#stdlib` @estevaofon
- Experimental external terminal package backed by a Go plugin.
- Complete Space Invaders example using the external terminal package.
- Neo Arcade Space Invaders 2 example with colored terminal sprites, deterministic starfield, smoke validation, and safe interactive terminal cleanup. `#examples` @estevaofon

### Fixed

- Stateful native calls now receive the actively executing VM instead of a VM captured during builtin registration.
- File, network, SQLite database, and SQLite statement handles now have shared ownership and synchronized lifecycle state across VMs sharing one runtime.
- Concurrent requests for the same module now share one successful initialization, with coordinated failure retry and import-cycle detection.
- Global bindings, module exports, maps, registries, and per-resource mutable state no longer expose the migrated concurrent Go-map crash paths.
- Strict JSON encoding now rejects unsupported values, non-finite floats,
  cycles, typed nil containers, and invalid UTF-8 without lossy fallback.
  `#vm` @estevaofon

### Follow-up

- Corrected `net_select` polling semantics remain follow-up work; this foundation only moves the existing buffered state into shared synchronized resources.
- Supervised `spawn` and task values remain follow-up work; this foundation does not change current public spawn behavior.

## [0.1.0] - 2026-08-10

### Added

- Repository-local skill for safe, transactional Noxy version updates by major, minor, patch, or explicit target, with default-minor behavior, dry-run validation, and rollback coverage. `#tooling` @estevaofon

## [1.5.0] - 2026-08-09

### Added

- Typed public native contracts and runtime reference-mode validation.
- Safe references to captured upvalues.

### Fixed

- Reference documentation now distinguishes contextual passing, update, and rebind.
- Dynamic calls reject incompatible reference modes before entering the callee.

## [1.4.0] - 2026-08-08

## Added

- Assinaturas exatas `func(T...) -> R` com verificação de aridade, argumentos, referências e retornos em compile time. `#compiler` @estevaofon

## Changed

- `func` sem assinatura permanece como fronteira dinâmica, enquanto funções nomeadas, closures e upvalues preservam seus tipos exatos. `#compiler` @estevaofon
- Exemplos, biblioteca padrão e especificação foram migrados para a nova sintaxe tipada. `#docs` @estevaofon

## Fixed

- Separação entre `null` e tipos dinâmicos desconhecidos, incluindo validação recursiva de callables e predeclaração de structs. `#compiler` @estevaofon

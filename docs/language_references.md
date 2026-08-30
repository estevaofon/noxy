# Referências de projeto da linguagem

Este documento registra **onde as escolhas arquiteturais do Noxy têm
precedente** — em outras linguagens e na literatura — e o que esses
precedentes ensinam sobre onde cada escolha costuma doer.

Existe por dois motivos:

1. **Evitar que a arquitetura seja declarada "inviável" por desconhecimento.**
   Quem chega ao código (humano ou agente de IA) e vê semântica de valor com
   copy-on-write como padrão, `ref` como único mecanismo de compartilhamento e
   um contador de donos por composto pode achar que é uma invenção frágil. Não
   é: é o desenho do Swift, do Nim, do PHP, do Qt e do R, com formalização
   acadêmica própria. Antes de propor trocar o modelo, leia o precedente.
2. **Ajudar a linguagem a evoluir com o aprendizado passado.** Cada precedente
   já pagou o custo de descobrir os pontos fracos do modelo. A seção
   "O que o precedente ensina" lista esses pontos e como o Noxy responde a cada
   um hoje — ao mexer no runtime ou na spec, é a lista de armadilhas
   conhecidas.

O contrato de linguagem em si está em
[`NOXY_LANGUAGE_SPEC.md`](NOXY_LANGUAGE_SPEC.md) (§2.2 semântica de valor,
§2.3 referências) e em [`REF_SEMANTICS.md`](REF_SEMANTICS.md); o mecanismo de
unicidade está em
[`superpowers/specs/2026-08-17-cow-rc-uniqueness-design.md`](superpowers/specs/2026-08-17-cow-rc-uniqueness-design.md).
Este arquivo não repete nada disso — só situa as escolhas.

---

## 1. Semântica de valor com copy-on-write + `ref` explícito para compartilhar

**A escolha.** Arrays, maps e structs são passados e atribuídos por **valor**:
a cópia é independente em qualquer profundidade através dos campos de valor.
O runtime implementa isso com copy-on-write: nada é copiado até que um
composto com mais de um dono vivo seja mutado. Compartilhar exige `ref` —
visível no call site (`f(ref x)`) ou declarado no tipo (`next: ref Node`).
A unicidade é decidida por um contador de donos duráveis (`Owners`) em cada
composto (`internal/value/cow.go`); a memória continua sendo do GC do Go — o
contador é só oráculo de "posso mutar in-place?".

### Precedentes diretos (valor + COW por padrão, referência explícita)

**Swift** — o precedente mais forte e o modelo declarado do Noxy. `Array`,
`Dictionary`, `String` e structs têm semântica de valor com COW implementado
via `isKnownUniquelyReferenced` sobre o refcount do ARC; classes são o tipo
de referência explícito. A Apple defendeu o modelo publicamente em *Building
Better Apps with Value Types in Swift* (WWDC 2015, sessão 414). A tabela de
correspondência Swift → Noxy está em §2 da spec de RC-uniqueness: refcount do
ARC ↔ `Owners`; `isKnownUniquelyReferenced()` ↔ `Owners == 1`; parâmetro
`+0 guaranteed` ↔ temporário de pilha que não conta.

**Nim** — o mais próximo do desenho "referência como ponto de
compartilhamento": objetos, `seq` e `string` são valores; `ref` é a palavra
que cria o tipo compartilhado. Nim teve strings/seqs com COW nas versões
antigas e migrou para move semantics + destrutores (ARC/ORC, Nim 1.x); a
história dessa migração é leitura útil para saber onde o COW puro doeu.

**PHP** (Zend engine) — arrays têm semântica de valor com COW por refcount;
`&` cria referência. Décadas de produção em escala.

**Qt (C++)** — *implicit sharing*: `QString`, `QList`, `QMap` são valores com
COW por refcount atômico; `QSharedPointer` é a referência explícita. Desde as
primeiras versões do framework.

**R e MATLAB** — semântica de valor com COW é o modelo central das duas (R via
`NAMED`, e por contagem de referências desde o R 4.0). Precedente científico e
acadêmico longo.

**C#** (struct vs. class) e **Go** (arrays vs. slices/maps) têm a divisão
valor/referência sem COW — precedente para a divisão, não para o mecanismo.

### Precedente acadêmico

O termo formal é **Mutable Value Semantics (MVS)**. Racordon, Abrahams,
Sullivan e Sekhon, *Implementation Strategies for Mutable Value Semantics*
(Journal of Object Technology, 2022), é o paper de referência; a linguagem
**Hylo** (ex-Val), de Dave Abrahams — o mesmo autor dos value types do Swift —
é a implementação de pesquisa. Hylo vai além do Swift: elimina referências de
primeira classe e compartilha só via `inout`/projeções, resolvendo
estaticamente o que Swift resolve com refcount em runtime.

Modelos vizinhos, que resolvem o mesmo problema por outro caminho e valem
como comparação: estruturas persistentes com compartilhamento estrutural
(Okasaki; HAMT de Bagwell, usado por Clojure e Scala) — imutabilidade em vez
de COW, custo O(log n) por atualização em vez de O(n) no pior caso, mas sem
mutação in-place quando o valor é único. A spec de RC-uniqueness (§9) já
registra estrutura persistente como a resposta a snapshots genuinamente
vivos, fora de escopo hoje.

### O que o precedente ensina sobre onde dói — e a resposta atual do Noxy

1. **COW exige saber se o valor é único.** Swift, PHP e Qt usam refcount;
   R usa `NAMED`/refcount; Hylo resolve estaticamente. Em linguagens com GC de
   rastreamento e sem refcount, o caminho conhecido é uma flag sticky de
   "já foi compartilhado", que copia mais do que o necessário — foi o primeiro
   desenho do Noxy (bit `Shared`, CoW 0.4.0) e produziu O(N²) por
   *compartilhamento morto* (clone pago quando o alias que o motivou já
   morreu). A resposta atual é o contador `Owners` (2026-08-17): a mesma
   família do Swift, com a vantagem de que o contador não gerencia memória —
   contar a mais degrada para uma cópia extra, nunca corrompe; contar a menos
   é o único erro grave, e é o que os testes `cow_*_test.go` e
   `container_owners_test.go` cercam.

2. **Cliff de performance por cópia acidental.** Em Swift, `arr[i] += 1`
   dentro de um laço com o array referenciado em dois lugares copia a cada
   iteração — é o bug de performance número um do modelo. No Noxy o mesmo
   cliff existe sempre que houver um segundo dono vivo; a mitigação de hoje é
   o contador exato (donos mortos não contam) e os opcodes de leitura "para
   mutação" que unicizam cada nível do caminho uma vez só
   (`internal/chunk/chunk.go`, `internal/compiler/cow_lowering.go`). Ao
   otimizar, o alvo é o mesmo do Swift: reduzir donos temporários, não
   afrouxar a regra de clonagem.

3. **Concorrência.** `std::string` do libstdc++ era COW e o C++11 baniu isso
   (invalidação de iteradores e custo de sincronização do refcount). No Noxy,
   a política é: dados entregues a outra routine por argumento ou canal
   seguem semântica de valor (independentes por construção); só `ref`,
   globais e upvalues compartilham, e a coordenação é explícita
   ([concurrency.md](concurrency.md)). O contador `Owners` é atômico para
   sobreviver a esse compartilhamento declarado. Qualquer mudança que faça um
   composto cruzar routines sem `ref` precisa revisitar este ponto.

4. **Dois mundos.** A confusão struct-vs-class do Swift é a crítica
   recorrente ao modelo: o usuário precisa saber qual semântica cada tipo tem.
   Hylo responde tirando referências de primeira classe; Nim responde com
   `ref` sintaticamente visível. O Noxy fica entre os dois: `ref` é visível no
   call site (`f(ref x)`, regra que também vale para builtins) **e** a aresta
   compartilhada de um struct está no tipo do campo (spec §2.2 regra 6), não
   na chamada. Não há tipo que seja "de referência por natureza" — a
   referência é sempre uma anotação sobre um slot.

5. **Sem `new`: célula por promoção de local.** `ref x` sobre uma local
   promove o slot a uma célula no heap que vive enquanto houver referência
   ([REF_SEMANTICS.md](REF_SEMANTICS.md), "Tempo de vida"). O precedente é o
   *boxing* de capturas de closure em Swift e Nim e, na literatura, a
   conversão de variáveis escapantes em células (*closure conversion* /
   *escape analysis*). O custo — uma alocação por local referenciada — é o
   mesmo dos precedentes.

**Resumo.** A escolha tem precedente sólido (Swift, Nim, PHP, Qt, R) e
literatura formal (MVS/Hylo). O que vale escrutinar em cada mudança não é a
arquitetura, mas o mecanismo de detecção de unicidade e a política sob
concorrência — aí é onde os precedentes divergem entre si e onde o Noxy já
trocou de desenho uma vez.

---

## Como manter este documento

- Cada seção cobre **uma** decisão arquitetural: a escolha, os precedentes
  (com a fonte), o que o precedente ensina e a resposta atual do Noxy.
- Ao trocar um mecanismo (como a troca do bit `Shared` pelo contador
  `Owners`), atualize a "resposta atual" e deixe o desenho anterior
  registrado como lição — é o histórico que evita repetir o erro.
- Citações devem ser verificáveis: nome da linguagem e versão, sessão/paper
  com autores e ano. Precedente que ninguém consegue conferir não conta.

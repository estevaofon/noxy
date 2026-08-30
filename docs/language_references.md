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
de referência explícito. A Apple defendeu o modelo publicamente em [*Building
Better Apps with Value Types in Swift*](https://nonstrict.eu/wwdcindex/wwdc2015/414/)
(WWDC 2015, sessão 414; a receita com `isKnownUniquelyReferenced` está em
[*OptimizationTips*](https://github.com/apple/swift/blob/main/docs/OptimizationTips.rst)
do repositório do Swift). A tabela de
correspondência Swift → Noxy está em §2 da spec de RC-uniqueness: refcount do
ARC ↔ `Owners`; `isKnownUniquelyReferenced()` ↔ `Owners == 1`; parâmetro
`+0 guaranteed` ↔ temporário de pilha que não conta.

**Nim** — o mais próximo do desenho "referência como ponto de
compartilhamento": objetos, `seq` e `string` são valores; `ref` é a palavra
que cria o tipo compartilhado. Nim teve strings/seqs com COW nas versões
antigas e migrou para move semantics + destrutores
([ARC/ORC](https://nim-lang.org/blog/2020/10/15/introduction-to-arc-orc-in-nim.html),
Nim 1.x; [*Destructors and Move Semantics*](https://nim-lang.org/docs/destructors.html));
a história dessa migração é leitura útil para saber onde o COW puro doeu.

**PHP** (Zend engine) — arrays têm semântica de valor com COW por refcount
(`SEPARATE_ARRAY` quando `refcount > 1`; [PHP Internals
Book](https://www.phpinternalsbook.com/php7/zvals/memory_management.html));
`&` cria referência. Décadas de produção em escala.

**Qt (C++)** — [*implicit sharing*](https://doc.qt.io/qt-6/implicit-sharing.html):
`QString`, `QList`, `QMap` são valores com COW por refcount atômico;
`QSharedPointer` é a referência explícita. Desde as primeiras versões do
framework.

**R e MATLAB** — semântica de valor com COW é o modelo central das duas (R via
`NAMED`, substituído por contagem de referências no R 4.0 — [R
Internals](https://cran.r-project.org/doc/manuals/r-release/R-ints.html)
§1.1.2). Precedente científico e acadêmico longo.

**C#** (struct vs. class) e **Go** (arrays vs. slices/maps) têm a divisão
valor/referência sem COW — precedente para a divisão, não para o mecanismo.

### Precedente acadêmico

O termo formal é **Mutable Value Semantics (MVS)**. Racordon, Shabalin,
Zheng, Abrahams e Saeta, [*Implementation Strategies for Mutable Value
Semantics*](https://www.jot.fm/issues/issue_2022_02/article2.pdf) (Journal
of Object Technology 21(2), 2022, doi:10.5381/jot.2022.21.2.a2), é o paper de
referência; a linguagem [**Hylo**](https://www.hylo-lang.org/) (ex-Val), de
Dave Abrahams — o mesmo autor dos value types do Swift — é a implementação de
pesquisa. Hylo vai além do Swift: elimina referências de
primeira classe e compartilha só via `inout`/projeções, resolvendo
estaticamente o que Swift resolve com refcount em runtime.

Modelos vizinhos, que resolvem o mesmo problema por outro caminho e valem
como comparação: estruturas persistentes com compartilhamento estrutural
([Okasaki, 1996](https://www.cs.cmu.edu/~rwh/students/okasaki.pdf); HAMT de
[Bagwell, 2001](https://lampwww.epfl.ch/papers/idealhashtrees.pdf), usado por
Clojure e Scala) — imutabilidade em vez
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
   (invalidação de iteradores e custo de sincronização do refcount —
   [N2668](https://www.open-std.org/jtc1/sc22/wg21/docs/papers/2008/n2668.htm),
   que motivou o [dual ABI do
   libstdc++](https://gcc.gnu.org/onlinedocs/libstdc++/manual/using_dual_abi.html)). No Noxy,
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

## Referências

Fontes verificáveis por decisão. Ao acrescentar um precedente, acrescente a
entrada aqui, com link.

### Semântica de valor com copy-on-write e `ref` explícito

**Literatura — mutable value semantics e cópia por unicidade**

- Racordon, D.; Shabalin, D.; Zheng, D.; Abrahams, D.; Saeta, B. *Implementation
  Strategies for Mutable Value Semantics*. Journal of Object Technology 21(2),
  2022. doi:10.5381/jot.2022.21.2.a2 —
  [PDF](https://www.jot.fm/issues/issue_2022_02/article2.pdf) ·
  [página do JOT](https://www.jot.fm/contents/issue_2022_02/article2.html) ·
  [preprint arXiv *Native Implementation of Mutable Value
  Semantics*](https://arxiv.org/abs/2106.12678).
- Hylo (ex-Val), linguagem de pesquisa de MVS — [hylo-lang.org](https://www.hylo-lang.org/).
- Apple. *Swift Ownership Manifesto* — registra os custos do COW por refcount
  ("reference counting overhead, complex performance characteristics") e a
  resposta por ownership —
  [GitHub](https://github.com/apple/swift/blob/main/docs/OwnershipManifesto.md).

**Literatura — unicidade como licença para mutar in-place** (a raiz teórica do
"`Owners == 1` → muta; `> 1` → clona")

- Wadler, P. *Linear Types can Change the World!* IFIP TC2 Working Conference
  on Programming Concepts and Methods, 1990 —
  [Semantic Scholar](https://www.semanticscholar.org/paper/Linear-Types-can-Change-the-World!-Wadler/24c850390fba27fc6f3241cb34ce7bc6f3765627).
- Baker, H. G. *Lively Linear Lisp: "Look Ma, No Garbage!"* ACM SIGPLAN
  Notices 27(8), 1992. doi:10.1145/142137.142162 —
  [PDF](https://www.cs.utexas.edu/~hunt/research/hash-cons/hash-cons-papers/BakerLinearLisp.pdf).
- Barendsen, E.; Smetsers, S. *Uniqueness typing for functional languages with
  graph rewriting semantics*. Mathematical Structures in Computer Science
  6(6), 1996. doi:10.1017/S0960129500070109 —
  [Cambridge Core](https://www.cambridge.org/core/services/aop-cambridge-core/content/view/0BF63E550377419604F633CB61A78496/S0960129500070109a.pdf/uniqueness-typing-for-functional-languages-with-graph-rewriting-semantics.pdf)
  (base do sistema de tipos de unicidade do Clean).
- Ullrich, S.; de Moura, L. *Counting Immutable Beans: Reference Counting
  Optimized for Purely Functional Programming*. IFL 2019 —
  [arXiv:1908.05647](https://arxiv.org/abs/1908.05647) (Lean 4: refcount como
  oráculo para atualização destrutiva quando único).
- Reinking, A.; Xie, N.; de Moura, L.; Leijen, D. *Perceus: Garbage Free
  Reference Counting with Reuse*. PLDI 2021 —
  [Microsoft Research](https://www.microsoft.com/en-us/research/publication/perceus-garbage-free-reference-counting-with-reuse/)
  (Koka: reúso in-place decidido por refcount; a mesma família do `Owners`).
- Lorenzen, A.; Leijen, D.; Swierstra, W. *FP²: Fully in-Place Functional
  Programming*. Proc. ACM Program. Lang. 7 (ICFP), 2023.
  doi:10.1145/3607840 — [ACM](https://dl.acm.org/doi/10.1145/3607840) ·
  [preprint](https://www.microsoft.com/en-us/research/wp-content/uploads/2023/07/fip.pdf).

**Literatura — a alternativa persistente**

- Okasaki, C. *Purely Functional Data Structures*. Tese de doutorado, CMU,
  1996 — [PDF](https://www.cs.cmu.edu/~rwh/students/okasaki.pdf).
- Bagwell, P. *Ideal Hash Trees*. EPFL, 2001 —
  [PDF](https://lampwww.epfl.ch/papers/idealhashtrees.pdf).

**Linguagens e sistemas em produção**

- Swift — Gregor, D.; Dudney, B. *Building Better Apps with Value Types in
  Swift*. WWDC 2015, sessão 414 —
  [índice e transcrição](https://nonstrict.eu/wwdcindex/wwdc2015/414/) ·
  [slides (PDF)](https://docs.huihoo.com/apple/wwdc/2015/414_building_better_apps_with_value_types_in_swift.pdf);
  *OptimizationTips* — "Use copy-on-write semantics for large values" —
  [GitHub](https://github.com/apple/swift/blob/main/docs/OptimizationTips.rst).
- Nim — *Destructors and Move Semantics* —
  [nim-lang.org](https://nim-lang.org/docs/destructors.html); Yarantsev, D.
  *Introduction to ARC/ORC in Nim*, 2020 —
  [blog](https://nim-lang.org/blog/2020/10/15/introduction-to-arc-orc-in-nim.html).
- PHP — *Memory management*, PHP Internals Book —
  [phpinternalsbook.com](https://www.phpinternalsbook.com/php7/zvals/memory_management.html).
- Qt — *Implicit Sharing* — [doc.qt.io](https://doc.qt.io/qt-6/implicit-sharing.html).
- R — *R Internals*, §1.1.2 (campo `named` e a substituição por contagem de
  referências) —
  [cran.r-project.org](https://cran.r-project.org/doc/manuals/r-release/R-ints.html).
- C++ — Meredith, A.; Boehm, H.; Crowl, L.; Dimov, P.; Krügler, D.
  *Concurrency Modifications to Basic String*, N2668, 2008 —
  [open-std.org](https://www.open-std.org/jtc1/sc22/wg21/docs/papers/2008/n2668.htm);
  GCC, *Dual ABI* —
  [gcc.gnu.org](https://gcc.gnu.org/onlinedocs/libstdc++/manual/using_dual_abi.html).

---

## Como manter este documento

- Cada seção cobre **uma** decisão arquitetural: a escolha, os precedentes
  (com a fonte), o que o precedente ensina e a resposta atual do Noxy.
- Ao trocar um mecanismo (como a troca do bit `Shared` pelo contador
  `Owners`), atualize a "resposta atual" e deixe o desenho anterior
  registrado como lição — é o histórico que evita repetir o erro.
- Citações devem ser verificáveis e ter link na seção "Referências": nome da
  linguagem e versão, sessão/paper
  com autores e ano. Precedente que ninguém consegue conferir não conta.

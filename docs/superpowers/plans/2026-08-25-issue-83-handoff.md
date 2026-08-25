# Handoff — issue #83 (acesso exclusivo a contêiner emprestado)

**Data:** 2026-08-25 · **Para:** sessão nova, sem o contexto da investigação
**Spec:** `docs/superpowers/specs/2026-08-25-issue-83-exclusive-access-design.md`
**Branch da spec:** `fix/issue-83-exclusive-access` (de `develop` limpa @ `20c207c`)
**Branch do protótipo:** `fix/issue-83-borrow-scope` @ `b96f5a7` — **não é a solução**

## Estado

| | |
|---|---|
| Design | commitado, com hipóteses falsificáveis (§7.5) |
| Implementação | **nenhuma** |
| `develop` | intacta, nada commitado nela |
| Issue #83 | comentada com os seis repros, a armadilha e a rota |
| Nada foi enviado ao remoto | os dois branches são locais |

Issues abertas nesta investigação, **independentes do #83**:
- **#86** — `DATA RACE` em `ObjInstance.Fields` (`executor.go:1454`/`:1520`), invisível para a CI por buraco no filtro do job de rede. Conserte a CI primeiro; é barato e sem ele qualquer correção regride sem aviso.
- **#87** — `ch06_keycount_ref.nx` não compila desde a migração do #82.

## Leia nesta ordem

1. **§0 da spec** — a decisão em uma página.
2. **§1.1** — os seis repros. São o critério de aceitação; a issue traz só o primeiro.
3. **§1.3** — a armadilha. Leia antes de escrever qualquer linha.
4. **§7.5** — as hipóteses, o que está validado e o que não está.

## Faça nesta ordem

### Passo 0 — validação adversarial de H4 (antes de qualquer código)

A hipótese "os seis repros esgotam o problema" **falhou três vezes** durante a
investigação, sempre com os testes tendo sido rodados — só que sobre os casos
imaginados pelo autor. Escrever código antes de validar isso é arriscar trabalho
jogado fora, porque um sétimo repro muda o desenho.

Protocolo: agente independente, sem o contexto de quem escreveu, instruído a
**procurar um sétimo repro**, começando pelo **repro F** (o alias dentro de um valor,
com o call site sem menção à raiz). Achar um é **sucesso**. Todo repro novo entra na
§1.1 e vira teste antes de a implementação seguir.

### Passo 1 — P1 (§2): convenção na assinatura

`ref T` de parâmetro é empréstimo e não pode ser guardado; `own ref T` declara o que
guarda. Fecha os repros A e B.

- R11 (empréstimo só em posição de argumento) tem protótipo pronto em
  `fix/issue-83-borrow-scope`: `internal/compiler/borrow_scope.go` mais o gancho no
  `case *ast.PrefixExpression` de `compiler.go`. Reaproveite; a base é `develop` limpa.
- R12 (`own ref`) não existe. Parser, `Owned bool` no `*ast.Parameter`, e as duas
  checagens locais da §2.3.
- **Modo warning primeiro** (canal `diagOut`, `cmd/noxy/warnings_test.go` trava a
  propriedade). O gate do corpus é `NOXY_SCAN=1 go test ./internal/compiler -run
  TestBorrowCorpusScan -v` (também no branch do protótipo).

### Passo 2 — P2 (§3): ordem de avaliação

A **semântica já está validada** (§7.5 H2) — não a re-derive. O que falta é codegen:
avaliar os argumentos não-empréstimo antes de criar os empréstimos, sem mudar a ordem
na pilha. Critério: **igualdade de bytecode** em toda chamada que não misture
empréstimo com argumento por valor.

### Passo 3 — P3 (§4): exclusividade dinâmica

Fecha D, E e F. **Antes de escrever a implementação**, estenda
`internal/vm/inline_guard_test.go` a `Retain`/`Release` — a propriedade de inline some
sem aviso e nenhum teste funcional a pega. Baseline medido: `Retain` custa 67 de um
orçamento de 80 (13 nós de folga); `Release` custa 80, sem folga, mas não é onde a
checagem entra.

## Armadilhas registradas

**Não chame `unicize` no contêiner no momento da escrita.** É o reflexo óbvio e é pior
que o bug: a escrita vai para um clone anônimo e some. Todo teste precisa da
contraprova — *a escrita através do empréstimo continua chegando no original*. Um teste
de "a cópia está isolada" passa numa implementação que perde a escrita.

**Não infira o escape com pré-passe de ponto fixo.** Torna a legalidade de uma chamada
dependente do corpo de uma função a vários níveis de distância. §2.2 da spec tem o
contraexemplo.

**Não proponha pin sem ler a §8.** `func Retain(v Value) bool` não pode substituir o
valor, então a cópia ansiosa viraria enumeração dos 41+ sites que chamam
`value.Retain`. A exclusividade cabe no `Retain` porque só precisa detectar e falhar.

**Não feche C–F estaticamente sem passar pelo repro F primeiro.** É o teste mais barato
de "esta ideia já foi descartada?".

## Decisões que são do autor da linguagem, não da implementação

1. **O critério da issue muda.** O programa do "Critério" do #83 passa a ser erro de
   compilação em vez de imprimir `1`. Em D–F, o programa falha alto em vez de corromper
   em silêncio. Se a preferência for manter o programa rodando, a rota é o pin e esta
   spec é a solução errada — a §8 registra os dois lados.
2. **Prioridade.** #47 (typo em nome global só explode em runtime) e #75 (`int + string`
   só falha em runtime) têm custo/benefício melhor e deveriam vir antes. O #83 exige
   sequência específica e aparece em 5 sites reais do corpus, todos em testes de
   semântica ou no port do K&R.
3. **`own` como grafia.** Alternativas com precedente: `inout` (Swift), `scoped`
   (C#, sentido invertido), `var` (Nim).

## O que foi verificado, e o que não foi

**Verificado, rodando:** os seis repros vazam em v0.19.0; a cópia feita *antes* do `ref`
já fica corretamente isolada; a semântica de P2 (§7.5 H2); custos de inline de
`Retain`/`Release`; o corpus (386 arquivos, 5 com aviso, 7 sites, nenhum em
`internal/stdlib/`); lista encadeada, árvore e grafo não disparam nada; `go test ./...`
verde com o protótipo.

**Não verificado:** `own ref` (não existe); o codegen de P2; qualquer parte de P3; e a
completude do conjunto de repros — que é o Passo 0.

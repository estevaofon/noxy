# Keywords de tipo contextuais, identificadores Unicode e token de erro tipado (issue #134)

**Data:** 2026-09-04 · **Branch:** `feat/issue-134-contextual-type-keywords-unicode-identifiers`, a partir de `develop` (pós #135, v0.23.4)
**Status:** aprovado, em implementação neste PR · **Issue:** [#134](https://github.com/estevaofon/noxy/issues/134) (consolida #128, #132) · **Relação:** #126 itens 3 e 5 (PR #127): o erro `'map' is a keyword and cannot be used as a name` e `lexer.IsReason`, que este design restringe e remove, respectivamente.

Dois follow-ups do frontend, os dois na direção de **expandir** o que a linguagem aceita. Item 1 é do parser: as nove keywords de tipo passam a ser contextuais. Item 2 é do lexer e do pacote `token`: identificadores Unicode, caractere ilegal lido como runa inteira, coluna contada em runas, e um tipo de token `LEXER_ERROR` no lugar da heurística `IsReason`. Nenhum dos dois toca compilador ou VM. Os itens são independentes; a ordem de implementação sugerida é **2 → 1**, porque o item 2 muda os testes de diagnóstico que o item 1 reescreve.

## 0. Fatos verificados antes do design (2026-09-04, `develop` em 25a21bb)

Probes no scratchpad, binário construído de `develop`:

| Programa | Hoje |
|---|---|
| `let int: int = 5` / `print(int)` | `[1:5] SyntaxError: 'int' is a keyword and cannot be used as a name` e, na linha 2, `invalid syntax "int"` — `int` não tem prefixo de expressão |
| `struct S` com campos `map: int` e `n: int`; `S(1, 2)` | campo `map` **descartado em silêncio** (`parseStructStatement` pula todo token que não é `IDENTIFIER`); o erro só aparece no construtor: `function 'S' expects 1 arguments, got 2` |
| `use src.map as map` + `map.tile()` | erro de keyword na linha 1 e três erros em cascata na linha 2 |
| `let s: string = “abc”` (aspas curvas coladas) | **seis** `invalid syntax` — um por byte (`"â"`, `"\u0080"`, `"\u009c"`, e o mesmo para `”`): `newToken` copia um byte e o converte para runa como inteiro |
| `let café = 1` | `expected ':', found illegal token` mais dois `invalid syntax` por letra acentuada (`"Ã"`, `"©"`): `isLetter` é ASCII |
| `let s: string = "aé" @ 1` | `[1:23] SyntaxError: invalid syntax "@"` — o `@` está na **coluna 22** em caracteres; `readChar` conta bytes |
| `lexer.IsReason` | `utf8.RuneCountInString(literal) > 1`; único consumidor é `noPrefixParseFnError` (`parser.go` ~1824) |
| `isIdentifierName` (`internal/compiler/runtime_types.go:493`) | já usa `unicode.IsLetter`/`IsDigit` — precedente interno para a definição de identificador |

## 1. Item 1 — keywords de tipo contextuais

### 1.1 Precedente e decisão

Go: `int`, `string`, `any`, `error` são identificadores pré-declarados, não keywords; `var int = 5` compila. Rust: `i32`, `str` idem. C#, Swift e Kotlin têm keywords contextuais, reservadas só onde têm significado; Swift aceita a maioria das keywords como nome de membro depois de `.`; JavaScript aceita palavra reservada como nome de propriedade. A regra estabelecida é reservar o mínimo que a gramática exige.

No Noxy a ambiguidade existe em **uma** posição: nome de tipo (`map[...]` vs. um struct `map`; `int` primitivo vs. struct `int`). Em posição de valor o parser sabe pelo contexto que ali só cabe um nome.

Alternativa descartada: **lexer emite `IDENTIFIER` para as nove palavras e o parser de tipo compara o literal** (o modelo de Go). Mais limpo conceitualmente, mas reescreve o `switch` de `parseAtomicType`, muda `Display()` e as mensagens `expected ':', found int` que `expect_peek_errors_test` fixa, e `map`/`chan` em posição de tipo são construtores sintáticos (`map[K, V]`, `chan T`), não identificadores. Mais churn pelo mesmo ganho semântico.

### 1.2 Tokens e helper

O lexer continua a produzir `TYPE_INT`, `TYPE_FLOAT`, `TYPE_STRING`, `TYPE_BOOL`, `TYPE_BYTES`, `TYPE_VOID`, `TYPE_ANY`, `MAP` e `CHAN`; o parser de tipo depende deles e não muda. `REF` **não** é contextual: `ref` é operador de prefixo em expressão (`ref x`) e de tipo (`ref T`), com significado nas duas posições. `FUNC` idem.

No parser:

```go
// isNameToken: IDENTIFIER ou uma das nove keywords de tipo contextuais.
func isNameToken(t token.TokenType) bool
// expectName avança se peek é nome; senão peekError(token.IDENTIFIER) — a
// mensagem "expected identifier, found X" não muda para os tokens que
// continuam proibidos.
func (p *Parser) expectName() bool
```

`isTypeKeyword` (usada por `peekError` para o erro do #126) fica como está, `REF` incluído — o erro continua correto onde um `expectPeek(IDENTIFIER)` sobreviver.

### 1.3 Sites que passam a aceitar nome

| Site (`parser.go`) | Hoje | Agora |
|---|---|---|
| `parseLetStatement` nome | `expectPeek(IDENTIFIER)` | `expectName()` |
| `parseFunctionStatement` nome | idem | `expectName()` |
| `parseFunctionParameters` (primeiro e demais) | idem | `expectName()` |
| `parseForStatement` variável | idem | `expectName()` |
| `parseUseStatement` caminho, segmentos, alias, seletores | idem | `expectName()` |
| `parseMemberAccess` membro após `.` | idem | `expectName()` |
| `parseFunctionLiteral` nome opcional (`let f = func any(...)`) | `peekTokenIs(IDENTIFIER)` | `isNameToken(p.peekToken.Type)` |
| `parseStructStatement` nome de campo | `curToken.Type != IDENTIFIER` → pula em silêncio | `isNameToken(curToken.Type)`; senão **erro** formatado a partir de `curToken` (`peekError` usa `peekToken`, então é um `fmt.Sprintf` próprio): `[l:c] SyntaxError: expected identifier, found X` com `X = curToken.Type.Display()`, e retorno `nil`. Campo chamado `ref` cai nessa mesma mensagem genérica (`found ref`): `ref` não é nome em posição alguma |
| expressão (`parseExpression`) | sem prefixo → `invalid syntax "int"` | `registerPrefix` dos nove tokens → `parseIdentifier` |

`parseIdentifier` produz `ast.Identifier{Value: Literal}`; o literal de `TYPE_INT` é `"int"`, então o compilador vê o mesmo nó que veria para qualquer identificador. `s.map = v`, `ref s.map` e `{s.map}` em f-string caem em `parseMemberAccess` (a f-string é re-lexada e re-parseada, `parseFString` ~1166). `for map in xs do`, `use m select map` e `func any()` caem nas linhas da tabela.

### 1.4 O que continua reservado

- **Nome de struct** (`struct map`) e **parâmetros de tipo** (`struct Box<map>`, `func f<int>()`): posições de tipo; `expectPeek(IDENTIFIER)` mantido, erro do #126 mantido.
- **Nome após `.` num tipo qualificado** (`let x: io.map`): posição de tipo; mantido.
- **Keywords de controle e declaração** (`let`, `if`, `for`, `in`, `end`, `use`, `as`, `select`, `true`, `null`, `zeros`, …): significado em posição de statement e de expressão; `expectName` falha nelas com a mensagem genérica de hoje (`expected identifier, found if`).

### 1.5 Tipo qualificado por alias que é keyword

Com `use src.map as map` legal, `let t: map.Tile = map.mk()` precisa parsear. Em `parseAtomicType`, **antes** do `switch` sobre `curToken.Type`, um `if isContextualTypeKeyword(p.curToken.Type) && p.peekTokenIs(token.DOT)` (as nove keywords, sem `IDENTIFIER`) desvia para o mesmo código do ramo `IDENTIFIER`, extraído numa função `parseNamedType(name string)` que recebe o nome inicial e faz o resto (segmentos `.Nome`, genéricos `Nome<...>`). O teste de `activeTypeParams` fica só no ramo `IDENTIFIER`: parâmetro de tipo nunca é keyword (§1.4). `map[` e `chan T` seguem inalterados, e `let v: map string = {}` continua a dar `expected '[', found string` (`expect_peek_errors_test`): só `DOT` desvia. `int.X` e afins também caem aqui — em posição de tipo um primitivo nunca é seguido de `.`, então não há ambiguidade.

### 1.6 Compilador e VM: nada muda

O compilador nunca compara nome de variável, função, parâmetro, campo ou módulo contra a lista de primitivos — os `"int"`/`"string"` de `arith_operand_checks.go`, `builtin_return_types.go` e `compiler.go` ~3277 são nomes de **tipo**. Um `let int = 5` é um global `int` como outro qualquer; `int(5)` sem declaração é `undefined variable 'int'`, não erro de sintaxe. Nome de campo `map` num `ObjInstance` é uma string como outra.

## 2. Item 2 — lexer: token de erro tipado, identificadores Unicode, runa inteira, coluna em runas

### 2.1 `token.LEXER_ERROR`

Novo tipo em `internal/token/token.go` (grupo "Especiais"), com entrada `lexer error` no mapa `tokenDisplay` de `internal/token/display.go` (sem ela, `Display()` cairia no fallback `"LEXER_ERROR"`). Todo ramo de `NextToken` que hoje grava uma razão de `readQuoted` em `ILLEGAL` (string, string de aspas simples, bytes, f-string) grava em `LEXER_ERROR`. `ILLEGAL` fica **só** para caractere desconhecido, produzido pelo `else` de `NextToken`. `lexer.IsReason` é removida; `noPrefixParseFnError` decide por tipo:

```go
case token.LEXER_ERROR: msg = "[l:c] SyntaxError: " + Literal      // razão + hint do lexer
case token.ILLEGAL:     msg = "[l:c] SyntaxError: invalid syntax %q" // caractere
```

`expectPeek` que encontra um `LEXER_ERROR` continua a dizer `expected X, found lexer error` e a razão aparece no `noPrefixParseFnError` seguinte — mesmo comportamento de hoje com `illegal token`, só com o nome certo.

### 2.2 Identificadores Unicode

Definição (spec §1.3 novo, igual a Go): identificador começa com `_` ou letra (`unicode.IsLetter`) e continua com letra, dígito (`unicode.IsDigit`) ou `_`. Números continuam a usar o `isDigit` ASCII (`readNumber`, hex, binário, expoente; não existe separador `_` em número — `1_000` já lexa hoje como `INT` + `IDENTIFIER`, e isso não muda). `1é` lexa como `INT "1"` + `IDENTIFIER "é"`, o mesmo que `1ex` hoje (`lexer_test.go` ~226) e o que o scanner de Go faz; quem reclama é o parser. O lexer continua a avançar por byte (`l.ch byte`); ganha `currentRune() (r rune, size int)` que decodifica a runa em `l.position` com `utf8.DecodeRuneInString`, e `advanceRune(size)` que chama `readChar` `size` vezes. `isLetter(byte)` vira `isIdentStart(rune)` e `isIdentPart(rune)`; o `default` de `NextToken` e `readIdentifier` decidem pela runa. Os ramos `b"` e `f"` não mudam (o `b`/`f` é ASCII; se não vem aspa, cai em `readIdentifier`, que já lê a runa seguinte). Keyword continua a ser `LookupIdent` sobre o literal — `café` nunca colide.

Precedente: Go, Python, Swift e Rust aceitam identificadores Unicode. Fora do escopo: normalização NFC (Swift faz, Go não) — o literal é comparado byte a byte, como em Go.

### 2.3 Caractere desconhecido é uma runa

`newToken(token.ILLEGAL, l.ch)` no `else` de `NextToken` vira: decodifica a runa em `l.position`, literal = `string(r)`, grava linha/coluna, `advanceRune(size)` e **retorna cedo**, como os ramos de identificador e número já fazem — a cauda comum de `NextToken` (`l.readChar(); return tok`) avançaria um byte a mais. `“abc”` gera **dois** `invalid syntax "“"` / `"”"` em vez de seis. Byte inválido em UTF-8 (`utf8.RuneError` com `size == 1`) vira um `ILLEGAL` com literal `"�"` — um por byte, que `%q` imprime de forma legível. A invariante "toda razão tem mais de uma runa" deixa de importar: a distinção é o tipo.

### 2.4 Coluna conta runas

`readChar` incrementa `l.column` só quando o byte lido **não** é continuação UTF-8 (`ch&0xC0 != 0x80`). Efeito: `[linha:coluna]` aponta o caractere em qualquer linha com acento ou aspas curvas — hoje aponta o byte e erra por um a cada caractere multibyte anterior na linha. Linhas ASCII não mudam; nenhum teste fixa coluna em fonte não-ASCII. Tabs continuam a contar 1. Caso-limite aceito: um byte de continuação **órfão** (arquivo corrompido, `0x80` solto) vira `ILLEGAL "�"` mas não conta coluna, então o resto da linha fica uma coluna à esquerda — o arquivo já é inválido e o diagnóstico aponta o byte; fora do escopo corrigir.

## 3. Spec, CHANGELOG, exemplo

- **§1.2 Keywords**: tabela ganha a coluna "Reserved where": Types → *type position only* (exceto `ref` e `func`: *everywhere*); demais categorias → *everywhere*. O parágrafo sobre `map`/`chan`/`any` é reescrito: as keywords de tipo são contextuais; `let map = {}`, `func any()`, `use src.map as map`, campo `map: int`, `s.map` e `map.Tile` em anotação são legais; `struct map` e parâmetro de tipo `map` não. `str` continua sem ser keyword.
- **§1.3 Identifiers** (novo): definição Unicode; Operators e Delimiters renumeram para 1.4 e 1.5 (grep no site e no README por `§1.3`/`§1.4` antes de renumerar).
- **§5 Structs**: "a field may have any name, type keywords included (`map: int`)".
- **§6.5 (nota de genéricos, spec ~1525)**: a nota "`map` is a type keyword, so it cannot be used as the name of a generic function… call it `map_arr`" fica falsa — `func map<A, B>(...)` passa a parsear (`expectName` + `parseTypeParameters`). Reescrever: `map` era reservada antes desta versão; o módulo `collections` mantém `map_arr` por compatibilidade (renomear a função da stdlib seria quebra e está fora do escopo). Mesmo ajuste no comentário de cabeçalho de `noxy_examples/collections.nx` (linhas 4-6).
- **Comentários que ficam falsos**: `peekError` e `isTypeKeyword` (`parser.go` ~109-136) usam `let map: int`, `use src.map`, `func f(any: int)` como exemplos do erro — trocar por `struct map`, `struct Box<map>`, `let x: io.map`. `TestStrIsAnIdentifierAndTypeKeywordsStayReserved` (`internal/lexer/lexer_test.go` ~236): as asserções de `LookupIdent` continuam verdadeiras (o lexer segue emitindo `MAP`), mas nome e comentário passam a dizer "tokens de tipo continuam saindo do lexer; o parser é quem os aceita como nome". `internal/vm/string_ordering_test.go:10` cita "§1.3/§8" para operadores → "§1.4/§8" após a renumeração (as demais menções a "§1.3" em `internal/vm` referem-se a specs de design, não à spec da linguagem; não mexer).
- **CHANGELOG** em `## [Unreleased]` (o release data a seção, como no #133): `Added` — keywords de tipo contextuais; identificadores Unicode. `Changed` — caractere ilegal diagnosticado por runa (um `invalid syntax` por caractere); coluna dos diagnósticos em caracteres, não bytes; `lexer.IsReason` sai (API interna). Sem `BREAKING`: só programas que não compilavam passam a compilar, e a coluna muda só onde já estava errada.
- **Exemplo** `noxy_examples/contextual_keywords_unicode.nx`: `let int`, `func any`, campo `map`, `s.map = v`, `ref s.map`, f-string, `let café`, com `assert` próprio; entra no runner.

## 4. Testes

- `internal/parser/syntax_errors_test.go`: `TestKeywordAsNameIsASingleError` troca os casos que passam a compilar (`use src.map as map`, `let map`, `let chan`, `func f(any: int)`) por `struct map`, `struct Box<map>`, `func f<int>()` e `let x: io.map`; `TestResyncFlagDoesNotLeakAcrossStatements` troca `y = obj.map` (agora válido) por `let x: Caixa<io.map>`; caso `pasted curly quotes` passa a exigir exatamente dois erros; caso novo: `let café = 1 @` reporta `[1:14]` (coluna em caracteres; em bytes seria 15).
- `internal/parser/syntax_errors_test.go` (novo `TestContextualTypeKeywordsAreNames`): cada linha do plano de teste da issue parseia sem erro e produz o nó esperado (`LetStmt.Name = "int"`, `StructField.Name = "map"`, `MemberAccess.Member = "map"`, `Identifier` em `print(map)`); `struct P` com token que não é nome em posição de campo (`5`) dá `expected identifier, found integer`, não silêncio.
- `internal/lexer/escapes_test.go`: `requireIllegal` exige `token.ILLEGAL` para as razões de escape malformado (`\x` fora de bytes, `\u01`, surrogates); passa a exigir `token.LEXER_ERROR` (renomear para `requireLexerError`).
- `internal/parser/syntax_errors_test.go`: caso novo em `TestContextualTypeKeywordsAreNames` para o nome opcional do literal de função (`let f = func any(x: int) -> int … end`) e para `func map<A, B>(...)` genérico.
- `internal/lexer/literals_test.go`: `TestIsReasonCountsRunesNotBytes` vira `TestLexerErrorAndIllegalAreDistinctTypes` (`"abc` → `LEXER_ERROR`; `@` e `“` → `ILLEGAL` com literal de uma runa); novo `TestUnicodeIdentifiers` (`café`, `área`, `_x1`, `π` → `IDENTIFIER`; `1é` → `INT` + `IDENTIFIER`); novo `TestColumnCountsRunes`.
- `internal/vm/*_test.go` (novo `contextual_keywords_test.go`): programa de string com `struct S { map: int }`, `let int`, `func any`, `s.map = v`, `ref s.map`, f-string, e módulo `src/map.nx` importado como `map` com `map.tile()` e `let t: map.Tile`; compara a saída.
- Verificação obrigatória (AGENTS.md): `go build ./... && go vet ./...`, `go test ./internal/... -count=1`, `go run ./cmd/noxy noxy_examples/run_all_tests_concurrent.nx`.

## 5. Fora do escopo

- Normalização Unicode de identificadores (NFC) e homóglifos.
- `ref` e `func` como nomes.
- Nome de struct ou parâmetro de tipo com keyword de tipo.
- Qualquer mudança no compilador, na VM ou no REPL (o REPL lê linhas pelo mesmo lexer e herda tudo).

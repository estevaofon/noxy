# Keywords de tipo contextuais, identificadores Unicode e token de erro tipado — plano de implementação (issue #134)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** as nove keywords de tipo (`int`, `float`, `string`, `bool`, `bytes`, `void`, `any`, `map`, `chan`) passam a ser aceitas como nome em toda posição de valor; o lexer aceita identificadores Unicode, lê um caractere desconhecido como runa inteira, conta coluna em caracteres e distingue razão de erro (`LEXER_ERROR`) de caractere ilegal (`ILLEGAL`) por tipo de token.

**Architecture:** o lexer continua a emitir os tokens de tipo; o parser ganha `isNameToken`/`expectName()` nos sites de posição de valor, um prefixo de expressão que trata esses tokens como `Identifier`, e um desvio em `parseAtomicType` para `alias.Tipo` quando o alias é keyword. No lexer, `currentRune`/`advanceRune` decodificam UTF-8 sobre o avanço por byte existente; `readChar` só conta coluna em byte que não é continuação. Compilador e VM não mudam. Ordem: lexer (Tasks 1-2) antes do parser (Tasks 3-4), porque os testes de diagnóstico que o parser reescreve dependem dos tipos de token novos.

**Tech Stack:** Go 1.25, módulo `noxy-vm`; testes `go test` por pacote; programas Noxy de string nos testes (`runModuleProgram` em `internal/vm/module_exports_test.go`).

**Spec:** `docs/superpowers/specs/2026-09-04-issue-134-contextual-type-keywords-unicode-identifiers-design.md` — o plano argumenta a partir dela; leia as duas.

## Global Constraints

- Verificação obrigatória após qualquer modificação (AGENTS.md): `go build ./... && go vet ./...`, `go test ./internal/... -count=1`, `go run ./cmd/noxy noxy_examples/run_all_tests_concurrent.nx`.
- `gofmt -d` limpo nos arquivos tocados; `git diff --numstat` sem arquivo reescrito por EOL (checkout pode ser CRLF).
- Commits: `tipo(escopo): descrição em português (issue #134)`, com o rodapé `Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>` e `Claude-Session: https://claude.ai/code/session_01Kc5SesDL6fvcJY6mNmMfwU`.
- **Compilador e VM não mudam** (spec §1.6). Se uma task parecer exigir mudança em `internal/compiler` ou `internal/vm` (fora de `*_test.go`), pare e reporte.
- **`ref` e `func` continuam reservados em toda posição** (spec §1.2). `isContextualTypeKeyword` tem exatamente nove tokens.
- **`isTypeKeyword` e o erro `'X' is a keyword and cannot be used as a name` não mudam de texto** (spec §1.2, §1.4): só os sites que o disparam encolhem.
- **Números continuam ASCII** (`isDigit` de byte; spec §2.2). Só identificadores usam `unicode`.
- Um implementador por vez na árvore (memória do projeto: implementadores paralelos colidem no índice do git).
- Trabalhe na branch `feat/issue-134-contextual-type-keywords-unicode-identifiers` (já existe, a partir de `develop`).

---

## Mapa de arquivos

| Arquivo | Responsabilidade neste plano |
|---|---|
| `internal/token/token.go`, `internal/token/display.go` | tipo `LEXER_ERROR` e seu nome de exibição (Task 1) |
| `internal/lexer/lexer.go` | razões viram `LEXER_ERROR`; `IsReason` sai (Task 1); `currentRune`/`advanceRune`, `isIdentStart`/`isIdentPart`, `readIdentifier` por runa, `ILLEGAL` por runa, coluna por runa (Task 2) |
| `internal/lexer/escapes_test.go`, `internal/lexer/literals_test.go` | `requireLexerError`; teste de tipos distintos (Task 1) |
| `internal/lexer/unicode_identifiers_test.go` (novo) | identificadores Unicode, um `ILLEGAL` por runa, coluna por runa (Task 2) |
| `internal/lexer/lexer_test.go` | nome/comentário de `TestStrIsAnIdentifierAndTypeKeywordsStayReserved` (Task 3) |
| `internal/parser/parser.go` | `noPrefixParseFnError` por tipo (Task 1); `isContextualTypeKeyword`, `isNameToken`, `expectName`, prefixos, sites de valor, campo de struct, literal de função, comentários (Task 3); `parseNamedType` e desvio `alias.Tipo` (Task 4) |
| `internal/parser/syntax_errors_test.go` | casos de keyword/resync/aspas curvas reescritos (Tasks 2, 3) |
| `internal/parser/contextual_keywords_test.go` (novo) | keywords como nome, nós do AST, campo inválido, `map.Tile` (Tasks 3, 4) |
| `internal/vm/contextual_keywords_test.go` (novo) | programa ponta a ponta com módulo `src/map.nx` (Task 5) |
| `noxy_examples/contextual_keywords_unicode.nx` (novo), `noxy_examples/collections.nx` | exemplo no runner; comentário de `map_arr` (Task 5) |
| `docs/NOXY_LANGUAGE_SPEC.md`, `CHANGELOG.md`, `internal/vm/string_ordering_test.go` | §1.2, §1.3 novo, §5, nota de genéricos; `[Unreleased]`; referência §1.4 (Task 6) |

---

### Task 1: `token.LEXER_ERROR` no lugar de `lexer.IsReason`

**Files:**
- Modify: `internal/token/token.go` (bloco `// Especiais`, ~linha 118)
- Modify: `internal/token/display.go` (mapa `tokenDisplay`, ~linha 74)
- Modify: `internal/lexer/lexer.go` (ramos `"`, `'`, `b"`, `f"` de `NextToken`, ~linhas 164-210; `IsReason`, ~linhas 563-577)
- Modify: `internal/parser/parser.go` (`noPrefixParseFnError`, ~linha 1818)
- Modify: `internal/lexer/escapes_test.go` (`requireIllegal`, ~linha 29), `internal/lexer/literals_test.go` (usos de `requireIllegal`; `TestIsReasonCountsRunesNotBytes`, ~linha 117)

**Interfaces:**
- Produces: `token.LEXER_ERROR TokenType = "LEXER_ERROR"`, exibido como `lexer error`. Invariante para as tasks seguintes: `token.ILLEGAL` só sai do ramo `else` do `default` de `NextToken` (caractere desconhecido); toda razão de `readQuoted` sai como `LEXER_ERROR`.

- [ ] **Step 1: Escrever os testes que falham**

Em `internal/lexer/escapes_test.go`, renomear o helper e trocar o tipo esperado:

```go
func requireLexerError(t *testing.T, source string, wantReason string) {
	t.Helper()
	got := firstToken(t, source)
	if got.Type != token.LEXER_ERROR {
		t.Fatalf("lexing %s produced %s (%q), want LEXER_ERROR", source, got.Type, got.Literal)
	}
	if !strings.Contains(got.Literal, wantReason) {
		t.Fatalf("lexing %s reported %q, want it to mention %q", source, got.Literal, wantReason)
	}
}
```

Trocar todos os usos: `grep -rl requireIllegal internal/lexer/ | xargs sed -i 's/requireIllegal(/requireLexerError(/g'`.

Em `internal/lexer/literals_test.go`, substituir `TestIsReasonCountsRunesNotBytes` (e o comentário acima dele) por:

```go
// Issue #134: a razao escrita pelo lexer ("unterminated string",
// UnclosedBraceReason) e o caractere desconhecido copiado verbatim sao tokens
// de TIPOS distintos — LEXER_ERROR e ILLEGAL. Antes a distincao era
// lexer.IsReason, uma contagem de runas do literal (issue #126), que nada
// impunha: uma razao de uma palavra ou um ILLEGAL com mais de uma runa
// mudava o diagnostico do parser sem quebrar teste algum.
func TestLexerErrorAndIllegalAreDistinctTypes(t *testing.T) {
	if got := firstToken(t, `"abc`); got.Type != token.LEXER_ERROR || got.Literal != "unterminated string" {
		t.Fatalf("token = %#v, want LEXER_ERROR \"unterminated string\"", got)
	}
	if got := firstToken(t, `f"{x`); got.Type != token.LEXER_ERROR || got.Literal != UnclosedBraceReason {
		t.Fatalf("token = %#v, want LEXER_ERROR with UnclosedBraceReason", got)
	}
	if got := firstToken(t, "@"); got.Type != token.ILLEGAL || got.Literal != "@" {
		t.Fatalf("token = %#v, want ILLEGAL \"@\"", got)
	}
}
```

- [ ] **Step 2: Rodar para ver falhar**

Run: `go test ./internal/lexer/ -run 'TestLexerErrorAndIllegalAreDistinctTypes|TestUnterminated|TestUnicodeEscape|TestFString' -count=1`
Expected: FAIL — `undefined: token.LEXER_ERROR` (compilação do pacote de teste).

- [ ] **Step 3: Implementar**

`internal/token/token.go`, no bloco `// Especiais`:

```go
	// Especiais
	NEWLINE TokenType = "NEWLINE"
	EOF     TokenType = "EOF"
	// ILLEGAL e um caractere fora do alfabeto da linguagem, copiado como
	// literal (uma runa). LEXER_ERROR e uma RAZAO escrita pelo lexer
	// ("unterminated string", lexer.UnclosedBraceReason) — o parser imprime
	// o literal como diagnostico. Sao tipos distintos de proposito (issue
	// #134): a distincao anterior era contar runas do literal.
	ILLEGAL     TokenType = "ILLEGAL"
	LEXER_ERROR TokenType = "LEXER_ERROR"
```

`internal/token/display.go`, no mapa `tokenDisplay`, logo após `ILLEGAL: "illegal token",`:

```go
	LEXER_ERROR: "lexer error",
```

`internal/lexer/lexer.go`: nos quatro ramos de `NextToken` que fazem `if reason != "" { tok.Type = token.ILLEGAL; tok.Literal = reason }` (casos `'"'`, `'\''`, `'b'` e `'f'`), trocar `tok.Type = token.ILLEGAL` por `tok.Type = token.LEXER_ERROR`. Um `sed` seguro, porque o `else` do `default` usa `newToken(token.ILLEGAL, l.ch)` e não esse padrão:

```bash
sed -i 's/^\(\t\t\t\|\t\t\t\t\)tok.Type = token.ILLEGAL$/\1tok.Type = token.LEXER_ERROR/' internal/lexer/lexer.go
grep -n "token.ILLEGAL\|token.LEXER_ERROR" internal/lexer/lexer.go
```

Expected do grep: quatro `LEXER_ERROR` e um único `token.ILLEGAL` (o `newToken` do `default`).

Remover `IsReason` inteira (a função e o comentário de doc de `// IsReason reports whether` até o `}` que fecha `func IsReason`). O import `unicode/utf8` continua em uso (`utf8.AppendRune` em `readEscape`).

`internal/parser/parser.go`, `noPrefixParseFnError`:

```go
func (p *Parser) noPrefixParseFnError(t token.TokenType) {
	msg := fmt.Sprintf("[%d:%d] SyntaxError: invalid syntax %q", p.curToken.Line, p.curToken.Column, p.curToken.Literal)
	switch p.curToken.Type {
	case token.EOF:
		msg = fmt.Sprintf("[%d:%d] SyntaxError: unexpected EOF", p.curToken.Line, p.curToken.Column)
	case token.LEXER_ERROR:
		// O literal e a razao que o lexer escreveu ("unterminated string",
		// lexer.UnclosedBraceReason + hint) e vira o diagnostico. Um
		// ILLEGAL (caractere desconhecido, ex.: "@") fica com o
		// "invalid syntax %q" generico acima. A distincao e por TIPO de
		// token (issue #134), nao mais por comprimento do literal.
		msg = fmt.Sprintf("[%d:%d] SyntaxError: %s", p.curToken.Line, p.curToken.Column, p.curToken.Literal)
	}
	p.errors = append(p.errors, msg)
}
```

O import `noxy-vm/internal/lexer` em `parser.go` continua em uso (`lexer.New` em `parseFString`).

- [ ] **Step 4: Rodar os testes**

Run: `go build ./... && go test ./internal/lexer/ ./internal/parser/ -count=1`
Expected: PASS. Em particular `TestSyntaxErrorMessages/unterminated string` continua a produzir `SyntaxError: unterminated string` e `unknown character` continua `invalid syntax "@"`.

- [ ] **Step 5: Commit**

```bash
gofmt -l internal/token internal/lexer internal/parser
git add internal/token/token.go internal/token/display.go internal/lexer/lexer.go internal/parser/parser.go internal/lexer/escapes_test.go internal/lexer/literals_test.go
git commit -m "refactor(lexer,token): LEXER_ERROR distingue razão de caractere ilegal por tipo, IsReason sai (issue #134)

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01Kc5SesDL6fvcJY6mNmMfwU"
```

---

### Task 2: identificadores Unicode, `ILLEGAL` por runa, coluna por runa

**Files:**
- Modify: `internal/lexer/lexer.go` (imports; `readChar` ~linha 24; `default` de `NextToken` ~linha 223; `readIdentifier` ~linha 260; `isLetter` ~linha 618)
- Create: `internal/lexer/unicode_identifiers_test.go`
- Modify: `internal/parser/syntax_errors_test.go` (caso `pasted curly quotes` ~linha 115; teste novo)

**Interfaces:**
- Produces: `(*Lexer).currentRune() (rune, int)`, `(*Lexer).advanceRune(size int)`, `isIdentStart(rune) bool`, `isIdentPart(rune) bool`. `isLetter(byte)` deixa de existir. `Token.Column` passa a ser índice de caractere (1-based) na linha.

- [ ] **Step 1: Escrever os testes que falham**

`internal/lexer/unicode_identifiers_test.go`:

```go
package lexer

import (
	"testing"

	"noxy-vm/internal/token"
)

type wantToken struct {
	typ token.TokenType
	lit string
}

func requireTokens(t *testing.T, source string, want []wantToken) {
	t.Helper()
	lex := New(source)
	for i, w := range want {
		got := lex.NextToken()
		if got.Type != w.typ || got.Literal != w.lit {
			t.Fatalf("source %q: token %d = %s %q, want %s %q", source, i, got.Type, got.Literal, w.typ, w.lit)
		}
	}
	if got := lex.NextToken(); got.Type != token.EOF {
		t.Fatalf("source %q: trailing token %s %q, want EOF", source, got.Type, got.Literal)
	}
}

// Issue #134 (spec §1.3): identificador comeca com letra ou '_' e continua
// com letra, digito ou '_', letras e digitos por unicode.IsLetter/IsDigit —
// como Go. Antes isLetter era ASCII e cada letra acentuada virava um
// ILLEGAL por byte.
func TestUnicodeIdentifiers(t *testing.T) {
	requireTokens(t, "let café = área + _x1 + π", []wantToken{
		{token.LET, "let"}, {token.IDENTIFIER, "café"}, {token.ASSIGN, "="},
		{token.IDENTIFIER, "área"}, {token.PLUS, "+"}, {token.IDENTIFIER, "_x1"},
		{token.PLUS, "+"}, {token.IDENTIFIER, "π"},
	})
}

// Uma keyword continua a ser reconhecida pelo literal exato; um identificador
// que a estende com uma letra Unicode e um identificador comum.
func TestUnicodeIdentifierDoesNotCollideWithKeyword(t *testing.T) {
	requireTokens(t, "for forê in", []wantToken{
		{token.FOR, "for"}, {token.IDENTIFIER, "forê"}, {token.IN, "in"},
	})
}

// Numero continua ASCII: `1é` e INT "1" seguido de IDENTIFIER "é", o mesmo
// que `1ex` (TestNextToken) e o que o scanner de Go faz.
func TestDigitThenUnicodeLetterIsNumberThenIdentifier(t *testing.T) {
	requireTokens(t, "1é", []wantToken{{token.INT, "1"}, {token.IDENTIFIER, "é"}})
}

// Caractere fora do alfabeto vira UM ILLEGAL por caractere, nao um por byte:
// aspas curvas coladas de um editor sao dois tokens, nao seis (spec §2.3).
func TestUnknownCharacterIsOneIllegalPerRune(t *testing.T) {
	requireTokens(t, "“abc”", []wantToken{
		{token.ILLEGAL, "“"}, {token.IDENTIFIER, "abc"}, {token.ILLEGAL, "”"},
	})
}

// Column conta caracteres, nao bytes (spec §2.4): o '@' de `let s = "aé" @`
// esta na coluna 14; em bytes seria 15.
func TestColumnCountsRunes(t *testing.T) {
	lex := New("let s = \"aé\" @\ncafé x")
	var at, x token.Token
	for {
		tok := lex.NextToken()
		if tok.Type == token.EOF {
			break
		}
		if tok.Type == token.ILLEGAL {
			at = tok
		}
		if tok.Type == token.IDENTIFIER && tok.Literal == "x" {
			x = tok
		}
	}
	if at.Line != 1 || at.Column != 14 {
		t.Fatalf("'@' at [%d:%d], want [1:14]", at.Line, at.Column)
	}
	if x.Line != 2 || x.Column != 6 {
		t.Fatalf("'x' at [%d:%d], want [2:6]", x.Line, x.Column)
	}
}
```

`internal/parser/syntax_errors_test.go`: no caso `pasted curly quotes report invalid syntax, not a bare byte` de `TestSyntaxErrorMessages`, trocar o comentário e o `notWant`:

```go
		{
			// Issue #126/#134: aspas curvas coladas de um editor de texto.
			// Cada caractere e UM token ILLEGAL com a runa inteira como
			// literal e vira `invalid syntax "“"` — nunca o byte cru
			// impresso sem aspas (o sintoma do #126).
			name:    "pasted curly quotes report invalid syntax, not a bare byte",
			source:  "let s: string = “abc”\n",
			want:    []string{`invalid syntax "“"`, `invalid syntax "”"`},
			notWant: []string{"SyntaxError: â", `""`},
		},
```

Atenção: `%q` do Go imprime `“` como `"“"` (caractere legível; só escapa não-imprimíveis), então o `want` acima é o texto literal com as aspas curvas dentro das aspas retas. Se o teste falhar por diferença de escape, imprima `joined` e ajuste o `want` para exatamente o que `%q` produz — a exigência é uma linha por CARACTERE.

E acrescentar, depois de `TestSyntaxErrorsCarryLineAndColumn`:

```go
// Issue #134: coluna em caracteres, e um diagnostico por caractere ilegal.
func TestIllegalCharacterDiagnosticsCountRunes(t *testing.T) {
	p := New(lexer.New("let s: string = “abc”\n"))
	_ = p.ParseProgram()
	if got := len(p.Errors()); got != 2 {
		t.Fatalf("want 2 errors (one per pasted quote), got %d: %q", got, p.Errors())
	}

	p = New(lexer.New("let café = 1 @\n"))
	_ = p.ParseProgram()
	joined := strings.Join(p.Errors(), "\n")
	if !strings.Contains(joined, `[1:14] SyntaxError: invalid syntax "@"`) {
		t.Fatalf("errors=%q: want the '@' reported at column 14 (characters, not bytes)", p.Errors())
	}
}
```

- [ ] **Step 2: Rodar para ver falhar**

Run: `go test ./internal/lexer/ -run 'Unicode|Rune|DigitThen' -count=1 && go test ./internal/parser/ -run 'TestIllegalCharacterDiagnosticsCountRunes|TestSyntaxErrorMessages' -count=1`
Expected: FAIL — `TestUnicodeIdentifiers` recebe `ILLEGAL "Ã"` no lugar de `IDENTIFIER "café"`; `TestColumnCountsRunes` vê coluna 15; o parser vê 6 erros.

- [ ] **Step 3: Implementar**

`internal/lexer/lexer.go`, imports:

```go
import (
	"unicode"
	"unicode/utf8"

	"noxy-vm/internal/token"
)
```

`readChar`:

```go
func (l *Lexer) readChar() {
	if l.readPosition >= len(l.input) {
		l.ch = 0
	} else {
		l.ch = l.input[l.readPosition]
	}
	l.position = l.readPosition
	l.readPosition += 1
	// Coluna em CARACTERES, nao em bytes (issue #134): um byte de
	// continuacao UTF-8 (10xxxxxx) pertence ao caractere que o byte
	// anterior comecou e nao abre coluna nova. Linhas ASCII nao mudam.
	if l.ch&0xC0 != 0x80 {
		l.column++
	}
}
```

Novos helpers, logo após `peekChar`:

```go
// currentRune decodifica o caractere UTF-8 que comeca em l.position. Para
// ASCII (o caso comum) devolve rune(l.ch) e size 1 sem decodificar. Um byte
// invalido decodifica como utf8.RuneError com size 1.
func (l *Lexer) currentRune() (r rune, size int) {
	if l.ch < utf8.RuneSelf {
		return rune(l.ch), 1
	}
	return utf8.DecodeRuneInString(l.input[l.position:])
}

// advanceRune consome o caractere inteiro devolvido por currentRune.
func (l *Lexer) advanceRune(size int) {
	for i := 0; i < size; i++ {
		l.readChar()
	}
}
```

`default` de `NextToken`:

```go
	default:
		r, size := l.currentRune()
		if isIdentStart(r) {
			tok.Literal = l.readIdentifier()
			tok.Type = token.LookupIdent(tok.Literal)
			tok.Line = startLine
			tok.Column = startColumn
			return tok
		} else if isDigit(l.ch) {
			tok.Type, tok.Literal = l.readNumber()
			tok.Line = startLine
			tok.Column = startColumn
			return tok
		} else {
			// Caractere fora do alfabeto: UM token ILLEGAL por CARACTERE,
			// com a runa inteira como literal (issue #134). Retorna cedo,
			// como os ramos acima: a cauda comum de NextToken faz um
			// readChar, que avancaria so um byte de um caractere multibyte.
			tok = token.Token{Type: token.ILLEGAL, Literal: string(r), Line: startLine, Column: startColumn}
			l.advanceRune(size)
			return tok
		}
	}
```

`readIdentifier`:

```go
func (l *Lexer) readIdentifier() string {
	position := l.position
	for {
		r, size := l.currentRune()
		if !isIdentPart(r) {
			break
		}
		l.advanceRune(size)
	}
	return l.input[position:l.position]
}
```

Substituir `isLetter` (fim do arquivo) por:

```go
// isIdentStart / isIdentPart definem identificador como Go (spec §1.3):
// comeca com letra ou '_', continua com letra, digito ou '_', com letra e
// digito por unicode.IsLetter/IsDigit. Numeros NAO passam por aqui —
// readNumber continua a usar o isDigit ASCII abaixo.
func isIdentStart(r rune) bool {
	return r == '_' || unicode.IsLetter(r)
}

func isIdentPart(r rune) bool {
	return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r)
}
```

`isDigit(byte)` e `newToken` ficam. Confirmar que nada mais usa `isLetter`: `grep -n isLetter internal/lexer/*.go` deve devolver nada.

- [ ] **Step 4: Rodar os testes**

Run: `go build ./... && go test ./internal/lexer/ ./internal/parser/ -count=1`
Expected: PASS. `TestNextToken`, `TestNumberLiterals`/expoentes (`1ex`) e `TestUnknownCharacterIsIllegalToken` (`@`) continuam a passar.

- [ ] **Step 5: Commit**

```bash
gofmt -l internal/lexer internal/parser
git add internal/lexer/lexer.go internal/lexer/unicode_identifiers_test.go internal/parser/syntax_errors_test.go
git commit -m "feat(lexer): identificadores Unicode, caractere ilegal por runa e coluna em caracteres (issue #134)

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01Kc5SesDL6fvcJY6mNmMfwU"
```

---

### Task 3: keywords de tipo contextuais no parser

**Files:**
- Modify: `internal/parser/parser.go` (`New` ~linha 55; `peekError`/`isTypeKeyword` ~linhas 108-145; `parseLetStatement` ~381; `parseUseStatement` ~496-535; `parseFunctionStatement` ~1387; `parseFunctionLiteral` ~1445; `parseFunctionParameters` ~1492 e ~1522; `parseStructStatement` ~1775; `parseMemberAccess` ~1810; `parseForStatement` ~1838)
- Create: `internal/parser/contextual_keywords_test.go`
- Modify: `internal/parser/syntax_errors_test.go` (`TestKeywordAsNameIsASingleError` ~linha 147; `TestResyncFlagDoesNotLeakAcrossStatements` ~linha 203)
- Modify: `internal/lexer/lexer_test.go` (`TestStrIsAnIdentifierAndTypeKeywordsStayReserved` ~linha 236)

**Interfaces:**
- Produces: `isContextualTypeKeyword(token.TokenType) bool` (exatamente os nove tokens), `isNameToken(token.TokenType) bool`, `(*Parser).expectName() bool`. Task 4 usa `isContextualTypeKeyword`.

- [ ] **Step 1: Escrever os testes que falham**

`internal/parser/contextual_keywords_test.go`:

```go
package parser

import (
	"strings"
	"testing"

	"noxy-vm/internal/ast"
	"noxy-vm/internal/lexer"
)

// Issue #134 (spec §1.2): as keywords de tipo sao contextuais — reservadas
// so em posicao de tipo. Em toda posicao de VALOR sao nomes comuns. Cada
// programa aqui era erro de sintaxe (ou, no campo de struct, descarte em
// silencio) e passa a parsear sem erro.
func TestContextualTypeKeywordsAreNames(t *testing.T) {
	cases := []struct{ name, source string }{
		{"let with annotation named int", "let int: int = 5\nprint(int)\n"},
		{"let inferred named map", "let map = 1\n"},
		{"assignment to name named int", "let int = 1\nint = 2\n"},
		{"func named any", "func any() -> int\n    return 1\nend\n"},
		{"params named map and chan", "func f(map: int[][], chan: int) -> int\n    return chan\nend\n"},
		{"for variable named string", "for string in [1] do\n    print(string)\nend\n"},
		{"use path segment and alias named map", "use src.map as map\nprint(map.tile())\n"},
		{"use select names int and float", "use src.util select int, float\n"},
		{"struct field named map", "struct S\n    map: int\n    n: int\nend\n"},
		{"member read write ref and f-string", "print(s.map)\ns.map = 3\nlet r = ref s.map\nprint(f\"{s.map}\")\n"},
		{"function literal named any", "let f = func any(x: int) -> int\n    return x\nend\n"},
		{"generic func named map", "func map<A, B>(xs: A[], fn: func(A) -> B) -> B[]\n    let out: B[] = []\n    return out\nend\n"},
		{"every contextual keyword as a let name", "let float = 1\nlet string = 2\nlet bool = 3\nlet bytes = 4\nlet void = 5\nlet any = 6\nlet chan = 7\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := New(lexer.New(tc.source))
			_ = p.ParseProgram()
			if len(p.Errors()) != 0 {
				t.Fatalf("source %q deveria parsear: %v", tc.source, p.Errors())
			}
		})
	}
}

// O no produzido e o mesmo Identifier de um nome comum: o compilador nao
// distingue `int` de `x`.
func TestContextualKeywordNodesAreIdentifiers(t *testing.T) {
	p := New(lexer.New("let int: int = 5\nstruct S\n    map: int\n    n: int\nend\nprint(s.map, int)\n"))
	program := p.ParseProgram()
	checkParserErrors(t, p)

	let, ok := program.Statements[0].(*ast.LetStmt)
	if !ok || let.Name.Value != "int" {
		t.Fatalf("statement 0 = %#v, want let named \"int\"", program.Statements[0])
	}
	st, ok := program.Statements[1].(*ast.StructStatement)
	if !ok || len(st.FieldsList) != 2 || st.FieldsList[0].Name != "map" || st.FieldsList[1].Name != "n" {
		t.Fatalf("statement 1 = %#v, want struct with fields [map n]", program.Statements[1])
	}
	es, ok := program.Statements[2].(*ast.ExpressionStmt)
	if !ok {
		t.Fatalf("statement 2 = %#v, want expression statement", program.Statements[2])
	}
	call, ok := es.Expression.(*ast.CallExpression)
	if !ok || len(call.Arguments) != 2 {
		t.Fatalf("expression = %#v, want call with 2 arguments", es.Expression)
	}
	member, ok := call.Arguments[0].(*ast.MemberAccessExpression)
	if !ok || member.Member != "map" {
		t.Fatalf("argument 0 = %#v, want member access .map", call.Arguments[0])
	}
	ident, ok := call.Arguments[1].(*ast.Identifier)
	if !ok || ident.Value != "int" {
		t.Fatalf("argument 1 = %#v, want identifier int", call.Arguments[1])
	}
}

// Campo de struct cujo token nao e nome: antes era pulado em silencio (o
// campo sumia e o erro so aparecia no construtor); agora e erro de sintaxe.
// `ref` nao e nome em posicao alguma.
func TestStructFieldThatIsNotANameIsAnError(t *testing.T) {
	cases := []struct{ name, source, want string }{
		{"integer literal as field", "struct P\n    x: int\n    5: int\nend\n", "expected identifier, found integer"},
		{"ref as field name", "struct P\n    ref: int\nend\n", "expected identifier, found ref"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := New(lexer.New(tc.source))
			_ = p.ParseProgram()
			joined := strings.Join(p.Errors(), "\n")
			if !strings.Contains(joined, tc.want) {
				t.Fatalf("source %q: errors=%q, want %q", tc.source, joined, tc.want)
			}
		})
	}
}
```

`internal/parser/syntax_errors_test.go`, `TestKeywordAsNameIsASingleError`: substituir o comentário e os casos por

```go
// Issue #126 item 5 / #134: keyword em posicao de TIPO onde se espera um
// nome (`struct map`, `struct Box<map>`, `func f<int>()`) e `ref` em
// qualquer posicao de nome dizem UM erro que nomeia a keyword, e o parser
// pula o resto da linha (recuperacao em modo panico com ponto de
// sincronizacao — Crafting Interpreters §6.3.3). As keywords de tipo em
// posicao de VALOR (`let map`, `use src.map as map`, `func f(any: int)`)
// sao nomes desde a #134 e nao chegam aqui.
func TestKeywordAsNameIsASingleError(t *testing.T) {
	cases := []struct {
		name   string
		source string
		want   string
	}{
		{"struct named map", "struct map end\nlet y: int = 2\n", "[1:8] SyntaxError: 'map' is a keyword and cannot be used as a name"},
		{"struct type parameter named map", "struct Box<map> end\nlet y: int = 2\n", "[1:12] SyntaxError: 'map' is a keyword and cannot be used as a name"},
		{"func type parameter named int", "func f<int>() end\nlet y: int = 2\n", "[1:8] SyntaxError: 'int' is a keyword and cannot be used as a name"},
		{"let named ref", "let ref: int = 1\nlet y: int = 2\n", "[1:5] SyntaxError: 'ref' is a keyword and cannot be used as a name"},
		{"param named ref", "func f(ref: int)\nend\n", "'ref' is a keyword and cannot be used as a name"},
	}
```

(o corpo do laço não muda). Atenção ao `end` **na mesma linha** dos três primeiros casos: a ressincronização pula até o fim da linha do erro, e um `end` numa linha própria no topo do arquivo viraria um segundo erro (`invalid syntax "end"`) — verificado com o binário de `develop`. No caso `param named ref` o `end` pode ficar em linha própria porque `parseFunctionStatement` chama `skipUntilEnd()` quando os parâmetros falham. Em `TestResyncFlagDoesNotLeakAcrossStatements`, trocar o caso `member access inside assignment RHS` (`y = obj.map`, que agora é válido) por

```go
		{
			"qualified type inside generic argument",
			"let x: Caixa<io.map> = 5\n",
			"'map' is a keyword and cannot be used as a name",
		},
```

e no comentário acima do teste substituir a frase sobre `y = obj.map` por: "em `let x: Caixa<io.map> = 5` quem falha é o ramo de tipo qualificado dentro do argumento genérico". Os dois casos restantes são de posição de tipo, o único lugar onde a keyword ainda é erro.

`internal/lexer/lexer_test.go`: renomear `TestStrIsAnIdentifierAndTypeKeywordsStayReserved` para `TestStrIsAnIdentifierAndTypeKeywordsKeepTheirTokens` e trocar o comentário por:

```go
// Issue #126 item 5: `str` era reservado em token.go (TYPE_STR) mas o parser
// nunca o consumia — reserva morta que so custava o identificador. Agora e
// um nome comum. `map`, `chan`, `any`, `string`... continuam a SAIR do lexer
// como tokens de tipo (o parser de tipo depende deles); desde a #134 e o
// parser quem os aceita como nome em posicao de valor (spec §1.2).
```

- [ ] **Step 2: Rodar para ver falhar**

Run: `go test ./internal/parser/ -run 'Contextual|StructFieldThatIsNotAName|TestKeywordAsNameIsASingleError|TestResyncFlag' -count=1`
Expected: FAIL — `TestContextualTypeKeywordsAreNames` recebe `'int' is a keyword and cannot be used as a name`; `TestStructFieldThatIsNotANameIsAnError/integer literal` não encontra erro nenhum.

- [ ] **Step 3: Implementar**

`internal/parser/parser.go`. Em `New`, depois de `p.registerPrefix(token.IDENTIFIER, p.parseIdentifier)`:

```go
	// Keyword de tipo contextual em posicao de EXPRESSAO e um identificador
	// (`print(map)`, `int + 1` com `let int = 5`): o literal do token ja e
	// o nome (issue #134, spec §1.2). O parser de tipo nunca passa por aqui.
	for _, t := range []token.TokenType{
		token.TYPE_INT, token.TYPE_FLOAT, token.TYPE_STRING, token.TYPE_BOOL,
		token.TYPE_BYTES, token.TYPE_VOID, token.TYPE_ANY, token.MAP, token.CHAN,
	} {
		p.registerPrefix(t, p.parseIdentifier)
	}
```

Logo após `isTypeKeyword`, os helpers:

```go
// isContextualTypeKeyword reporta se t e' uma das NOVE keywords de tipo
// contextuais (issue #134, spec §1.2): reservadas so em posicao de tipo,
// nome livre em toda posicao de valor — como int/string/any em Go, que sao
// identificadores pre-declarados. `ref` e `func` ficam de fora: sao
// operadores de prefixo em expressao alem de tipo, com significado nas duas
// posicoes. E' um subconjunto de isTypeKeyword.
func isContextualTypeKeyword(t token.TokenType) bool {
	switch t {
	case token.TYPE_INT, token.TYPE_FLOAT, token.TYPE_STRING, token.TYPE_BOOL,
		token.TYPE_BYTES, token.TYPE_VOID, token.TYPE_ANY, token.MAP, token.CHAN:
		return true
	default:
		return false
	}
}

// isNameToken reporta se t pode nomear variavel, funcao, parametro, campo,
// modulo ou membro: IDENTIFIER ou keyword de tipo contextual.
func isNameToken(t token.TokenType) bool {
	return t == token.IDENTIFIER || isContextualTypeKeyword(t)
}

// expectName e' o expectPeek(IDENTIFIER) das posicoes de VALOR. Falha pelo
// mesmo peekError(IDENTIFIER): o que sobra ali e' keyword de controle
// (`let if` → "expected identifier, found if"), pontuacao, ou `ref` (que
// ainda recebe o erro do #126) — nenhuma mensagem muda.
func (p *Parser) expectName() bool {
	if isNameToken(p.peekToken.Type) {
		p.nextToken()
		return true
	}
	p.peekError(token.IDENTIFIER)
	return false
}
```

Atualizar os dois comentários que citam os exemplos antigos. Em `peekError`, trocar o trecho `(`let map: int`, `use src.map`,\n\t\t// `func f(any: int)`)` por `(`struct map`, `struct Box<map>`,\n\t\t// `let x: io.map`, `let ref: int`)`. Em `isTypeKeyword`, trocar "Sao as reservadas que um usuario plausivelmente tentaria usar como nome (issue #126 item 5)" por "Sao as reservadas que um usuario plausivelmente tentaria usar como nome (issue #126 item 5); desde a #134 as nove contextuais (isContextualTypeKeyword) so chegam aqui em posicao de TIPO — nome de struct, parametro de tipo, tipo qualificado".

Trocar `expectPeek(token.IDENTIFIER)` por `expectName()` exatamente nestes sites (e em nenhum outro):

| Função | Ocorrências |
|---|---|
| `parseLetStatement` | 1 (nome) |
| `parseUseStatement` | 4 (primeiro segmento, segmentos após `.`, alias após `as`, seletores) |
| `parseFunctionStatement` | 1 (nome) |
| `parseFunctionParameters` | 2 (primeiro parâmetro; demais após `,`) |
| `parseMemberAccess` | 1 |
| `parseForStatement` | 1 (variável) |

Ficam com `expectPeek(token.IDENTIFIER)`: `parseTypeParameters`, `parseStructStatement` (nome do struct) e o tipo qualificado em `parseAtomicType`. Conferir ao final: `grep -c "expectPeek(token.IDENTIFIER)" internal/parser/parser.go` deve dar **3**.

`parseFunctionLiteral`, nome opcional:

```go
	// Optional Name (e.g. func myName(...) ...) — keyword de tipo contextual
	// tambem e' nome aqui (issue #134).
	if isNameToken(p.peekToken.Type) {
		p.nextToken()
		lit.Name = p.curToken.Literal
	}
```

`parseStructStatement`, laço de campos — substituir o bloco `if p.curToken.Type != token.IDENTIFIER { ... p.nextToken(); continue }` por:

```go
		if !isNameToken(p.curToken.Type) {
			// Issue #134: antes, um token que nao era IDENTIFIER aqui era
			// pulado em silencio — um campo `map: int` sumia da struct e
			// o erro so aparecia no construtor ("expects 1 arguments, got
			// 2"). peekError formata a partir de peekToken; o ofensor aqui
			// e' curToken.
			p.errors = append(p.errors, fmt.Sprintf("[%d:%d] SyntaxError: expected identifier, found %s",
				p.curToken.Line, p.curToken.Column, p.curToken.Type.Display()))
			return nil
		}
```

- [ ] **Step 4: Rodar os testes**

Run: `go build ./... && go test ./internal/lexer/ ./internal/parser/ -count=1`
Expected: PASS, inclusive `expect_peek_errors_test.go` inteiro (`let without name` → `expected identifier, found ':'`; `for without variable` → `expected identifier, found`; `param without colon` → `expected ':', found int`).

- [ ] **Step 5: Commit**

```bash
gofmt -l internal/parser internal/lexer
git add internal/parser/parser.go internal/parser/contextual_keywords_test.go internal/parser/syntax_errors_test.go internal/lexer/lexer_test.go
git commit -m "feat(parser): keywords de tipo contextuais — nome livre em posição de valor, campo de struct nunca some em silêncio (issue #134)

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01Kc5SesDL6fvcJY6mNmMfwU"
```

---

### Task 4: tipo qualificado por alias que é keyword (`map.Tile`)

**Files:**
- Modify: `internal/parser/parser.go` (`parseAtomicType`, ~linhas 805-885)
- Modify: `internal/parser/contextual_keywords_test.go`

**Interfaces:**
- Consumes: `isContextualTypeKeyword` (Task 3).
- Produces: `(*Parser).parseNamedType(name string) ast.NoxyType` — corpo do antigo ramo `IDENTIFIER` de `parseAtomicType` a partir do nome inicial (segmentos `.Nome`, genéricos `Nome<...>`).

- [ ] **Step 1: Escrever o teste que falha**

Acrescentar em `internal/parser/contextual_keywords_test.go`:

```go
// Spec §1.5: com `use src.map as map` legal, `map.Tile` em anotacao e' um
// tipo qualificado — `map.` e `map[` se distinguem por um token de
// lookahead. `chan T` e `map[K, V]` continuam construtores de tipo.
func TestContextualKeywordAliasQualifiesAType(t *testing.T) {
	p := New(lexer.New("let t: map.Tile = map.tile()\nlet u: map.Box<int> = map.box(1)\nlet m: map[string, int] = {}\nlet c: chan int = make_chan()\n"))
	program := p.ParseProgram()
	checkParserErrors(t, p)

	let, ok := program.Statements[0].(*ast.LetStmt)
	if !ok || let.Type == nil || let.Type.String() != "map.Tile" {
		t.Fatalf("statement 0 = %#v, want let with type map.Tile", program.Statements[0])
	}
	generic, ok := program.Statements[1].(*ast.LetStmt)
	if !ok || generic.Type == nil {
		t.Fatalf("statement 1 = %#v, want let with generic type", program.Statements[1])
	}
	if gt, isGeneric := generic.Type.(*ast.GenericType); !isGeneric || gt.Name != "map.Box" || len(gt.Args) != 1 {
		t.Fatalf("statement 1 type = %#v, want GenericType map.Box<int>", generic.Type)
	}
}

// `map` seguido de outra coisa que nao '.' nem '[' continua a ser o erro de
// construtor de tipo, nao um tipo chamado map.
func TestMapTypeStillRequiresBracket(t *testing.T) {
	p := New(lexer.New("let v: map string = {}\n"))
	_ = p.ParseProgram()
	joined := strings.Join(p.Errors(), "\n")
	if !strings.Contains(joined, "expected '[', found string") {
		t.Fatalf("errors=%q, want expected '[', found string", p.Errors())
	}
}
```

- [ ] **Step 2: Rodar para ver falhar**

Run: `go test ./internal/parser/ -run 'TestContextualKeywordAliasQualifiesAType|TestMapTypeStillRequiresBracket' -count=1`
Expected: `TestContextualKeywordAliasQualifiesAType` FAIL com `expected '[', found '.'`; `TestMapTypeStillRequiresBracket` já PASS.

- [ ] **Step 3: Implementar**

Em `parseAtomicType`, entre o bloco `// Grouped Type (T)` e `var t ast.NoxyType`, inserir:

```go
	// Issue #134 (spec §1.5): keyword de tipo contextual seguida de '.' e'
	// um alias de modulo qualificando um tipo (`use src.map as map` +
	// `let t: map.Tile`). Um token de lookahead decide: `map[` e `chan T`
	// caem no switch abaixo como antes. Parametro de tipo nunca e' keyword
	// (spec §1.4), entao activeTypeParams nao e' consultado aqui.
	if isContextualTypeKeyword(p.curToken.Type) && p.peekTokenIs(token.DOT) {
		return p.parseNamedType(p.curToken.Literal)
	}
```

No `case token.IDENTIFIER:` do switch, deixar só:

```go
	case token.IDENTIFIER:
		if p.activeTypeParams[p.curToken.Literal] {
			return &ast.TypeParamType{Name: p.curToken.Literal}
		}
		return p.parseNamedType(p.curToken.Literal)
```

e mover o resto do ramo (do `name := p.curToken.Literal` até `t = &ast.PrimitiveType{Name: name}`) para a função nova, logo após `parseAtomicType`:

```go
// parseNamedType completa um tipo nomeado a partir do primeiro segmento ja'
// consumido (curToken): segmentos qualificados `.Nome` (io.File) e
// argumentos genericos `Nome<T, U>`. Ao retornar, curToken esta' no ultimo
// token do tipo. O segmento apos '.' exige IDENTIFIER — keyword ali e'
// posicao de tipo e recebe o erro do #126 (`let x: io.map`).
func (p *Parser) parseNamedType(name string) ast.NoxyType {
	// Support dot notation for types (e.g. io.File)
	for p.peekTokenIs(token.DOT) {
		p.nextToken() // eat .
		if !p.expectPeek(token.IDENTIFIER) {
			return nil
		}
		name += "." + p.curToken.Literal
	}
	// Tipo generico em posicao de anotacao: Nome<arg1, arg2>
	if p.peekTokenIs(token.LT) {
		p.nextToken() // eat nome; curToken = <
		args := []ast.NoxyType{}
		for {
			p.nextToken() // avanca para o inicio do proximo tipo
			arg := p.parseType()
			if arg == nil {
				return nil
			}
			args = append(args, arg)
			p.splitCompositeGT() // divide >> ou >= pendentes ANTES de checar peek
			if p.peekTokenIs(token.COMMA) {
				p.nextToken()
				continue
			}
			break
		}
		if !p.expectPeek(token.GT) {
			return nil
		}
		return &ast.GenericType{Name: name, Args: args}
	}
	return &ast.PrimitiveType{Name: name}
}
```

Atenção ao fim de `parseAtomicType`: o ramo `IDENTIFIER` antigo atribuía `t = &ast.PrimitiveType{Name: name}` e caía no `return t` comum; agora retorna direto. Confirmar que o `return t` final continua a servir os demais `case`s (TYPE_INT etc.) e que nenhum `case` ficou sem atribuir `t`.

- [ ] **Step 4: Rodar os testes**

Run: `go build ./... && go test ./internal/parser/ ./internal/compiler/ -count=1`
Expected: PASS — `generics_parser_test.go`, `nullable_type_test.go`, `function_type_test.go` e `expect_peek_errors_test.go` (`qualified type with dangling dot`, `generic type without closing gt`) cobrem o código movido.

- [ ] **Step 5: Commit**

```bash
gofmt -l internal/parser
git add internal/parser/parser.go internal/parser/contextual_keywords_test.go
git commit -m "feat(parser): alias que é keyword de tipo qualifica tipo em anotação — map.Tile (issue #134)

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01Kc5SesDL6fvcJY6mNmMfwU"
```

---

### Task 5: ponta a ponta na VM, exemplo no runner, comentário de `collections.nx`

**Files:**
- Create: `internal/vm/contextual_keywords_test.go`
- Create: `noxy_examples/contextual_keywords_unicode.nx`
- Modify: `noxy_examples/collections.nx` (linhas 4-6)

**Interfaces:**
- Consumes: `runModuleProgram(t, root, source) (value.Value, error)` e o nativo `test_report` que ele define (`internal/vm/module_exports_test.go` ~573).

- [ ] **Step 1: Escrever o teste que falha**

`internal/vm/contextual_keywords_test.go`:

```go
package vm

import (
	"os"
	"path/filepath"
	"testing"
)

// Issue #134 ponta a ponta: keyword de tipo como nome de variavel, funcao,
// campo, alias de modulo e segmento de caminho (`src/map.nx`), com escrita,
// ref e f-string sobre o campo; e identificador Unicode. O compilador e a VM
// nao mudaram — o teste garante que nenhum deles trata `int`/`map` como
// nome especial.
func TestContextualTypeKeywordsEndToEnd(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "src"), 0o700); err != nil {
		t.Fatal(err)
	}
	module := "struct Tile\n    map: int\nend\nfunc tile(n: int) -> Tile\n    return Tile(n)\nend\n"
	if err := os.WriteFile(filepath.Join(root, "src", "map.nx"), []byte(module), 0o600); err != nil {
		t.Fatal(err)
	}
	source := `use src.map as map
struct S
    map: int
    n: int
end
func any(x: int) -> int
    return x + 1
end
func bump(v: ref int) -> void
    *v = *v + 1
end
let int: int = 5
let s: S = S(1, 2)
s.map = any(s.map)
bump(ref s.map)
let t: map.Tile = map.tile(int)
let café = t.map
test_report(f"{s.map}-{s.n}-{café}")
`
	reported, err := runModuleProgram(t, root, source)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := reported.Obj.(string)
	if !ok || got != "3-2-5" {
		t.Fatalf("reported = %#v, want \"3-2-5\"", reported)
	}
}
```

- [ ] **Step 2: Rodar para ver falhar**

Run: `go test ./internal/vm/ -run TestContextualTypeKeywordsEndToEnd -count=1`
Expected: PASS já — Tasks 1-4 entregaram o parser. Se FALHAR, o erro aponta uma suposição da spec §1.6 quebrada (compilador ou VM tratando o nome de forma especial): pare, registre a mensagem exata e reporte antes de mudar qualquer coisa fora de `_test.go`.

- [ ] **Step 3: Exemplo e comentário**

`noxy_examples/contextual_keywords_unicode.nx`:

```noxy
// contextual_keywords_unicode: keywords de tipo (int, map, chan, any...) sao
// contextuais desde a issue #134 — reservadas so em posicao de tipo, nome
// livre em posicao de valor — e identificadores aceitam letras Unicode
// (spec §1.2, §1.3). Roda no runner; sai com 1 se alguma asserção falhar.
use sys select exit

func assert(condition: bool, message: string) -> void
    if !condition then
        print("contextual_keywords_unicode: FAIL - " + message)
        exit(1)
    end
end

struct Tile
    map: int
    chan: string
end

func any(x: int) -> int
    return x * 2
end

func bump(v: ref int) -> void
    *v = *v + 1
end

let int: int = 5
let map: map[string, int] = {"a": 1}
let café: string = "café"
let área = 3

let t: Tile = Tile(7, "c")
assert(t.map == 7, "campo map")
assert(t.chan == "c", "campo chan")
t.map = 8
bump(ref t.map)
assert(t.map == 9, "escrita e ref em campo map")
assert(f"{t.map}-{int}" == "9-5", "f-string com membro map")
assert(any(int) == 10, "func any")
assert(map["a"] == 1, "map como nome de variavel com tipo map[...]")
assert(café == "café" && área == 3, "identificadores Unicode")

for string in [1, 2] do
    assert(string > 0, "for com variavel chamada string")
end

print("contextual_keywords_unicode: OK")
```

Run: `go run ./cmd/noxy noxy_examples/contextual_keywords_unicode.nx`
Expected: `contextual_keywords_unicode: OK`, exit 0.

`noxy_examples/collections.nx`, linhas 4-6, trocar por:

```noxy
// Nota: `map` era palavra reservada em toda posicao ate a issue #134; desde
// entao e' keyword contextual e `func map<A, B>` parseia. A funcao de
// transformacao continua `map_arr` por compatibilidade com quem ja importa.
```

Run: `go run ./cmd/noxy noxy_examples/collections.nx`
Expected: mesma saída de antes, exit 0.

- [ ] **Step 4: Rodar a suíte e o runner**

Run: `go test ./internal/vm/ -count=1 && go run ./cmd/noxy noxy_examples/run_all_tests_concurrent.nx`
Expected: PASS; o runner lista `contextual_keywords_unicode.nx` entre os aprovados.

- [ ] **Step 5: Commit**

```bash
gofmt -l internal/vm
git add internal/vm/contextual_keywords_test.go noxy_examples/contextual_keywords_unicode.nx noxy_examples/collections.nx
git commit -m "test(vm,examples): keywords contextuais e identificadores Unicode ponta a ponta; exemplo no runner (issue #134)

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01Kc5SesDL6fvcJY6mNmMfwU"
```

---

### Task 6: spec da linguagem, CHANGELOG, referência a §1.4

**Files:**
- Modify: `docs/NOXY_LANGUAGE_SPEC.md` (§1.2 ~linha 46; §1.3/§1.4 ~linhas 62-86; §5 ~linha 1114; nota de genéricos ~linha 1525)
- Modify: `CHANGELOG.md` (topo, antes de `## [0.23.4]`)
- Modify: `internal/vm/string_ordering_test.go:10`

- [ ] **Step 1: Spec §1.2**

Substituir a tabela e o parágrafo de §1.2 por:

```markdown
### 1.2 Keywords

| Category | Keywords | Reserved where |
|----------|----------|----------------|
| Declarations | `let`, `func`, `struct` | everywhere |
| Control Flow | `if`, `elif`, `then`, `else`, `end`, `while`, `do`, `return`, `break`, `continue`, `for`, `in`, `defer`, `when`, `case`, `default`, `try` | everywhere |
| Types | `int`, `float`, `string`, `bool`, `void`, `bytes`, `any`, `map`, `chan` | type position only |
| Type operators | `ref`, `func` | everywhere |
| Literals | `true`, `false`, `null` | everywhere |
| Modules | `use`, `select`, `as` | everywhere |
| Specials | `zeros` | everywhere |

The type keywords are **contextual**: they are reserved only where a type is
expected — a struct name (`struct map` → `'map' is a keyword and cannot be
used as a name`), a type parameter (`struct Box<map>`), the member of a
qualified type (`io.map`). Everywhere a *name* is expected they are ordinary
identifiers, as `int` and `any` are in Go: `let int: int = 5`, `let map = {}`,
`func any() -> int`, a parameter `map: Tile[][]`, `use src.map as map` with
`map.tile()` and `let t: map.Tile`, a struct field `map: int` with `s.map`,
`s.map = v`, `ref s.map` and `{s.map}` in an f-string. `ref` and `func` are
prefix operators in expressions as well as in types, so they stay reserved
everywhere. `str` is not a keyword.
```

- [ ] **Step 2: Spec §1.3 novo e renumeração**

Inserir antes de `### 1.3 Operators`:

```markdown
### 1.3 Identifiers

An identifier starts with a letter or `_` and continues with letters, digits
or `_`. Letters and digits are Unicode (`unicode.IsLetter`, `unicode.IsDigit`),
as in Go: `let café = 1`, `func área() -> float`. Identifiers are compared
byte for byte — no Unicode normalization is applied, so `é` written as one
code point and as `e` plus a combining accent are different names. Numeric
literals use ASCII digits only.

A character outside the language's alphabet is one diagnostic per character
(`invalid syntax "“"` for a pasted curly quote), and the column in a
diagnostic's `[line:column]` counts characters, not bytes.
```

Renumerar `### 1.3 Operators` → `### 1.4 Operators` e `### 1.4 Delimiters` → `### 1.5 Delimiters`. Confirmar com `grep -n "§1\.[3-5]" docs/*.md README.md docs/index.html` que nenhuma referência cruzada existe fora das specs de design (`docs/superpowers/`), que não mudam.

`internal/vm/string_ordering_test.go:10`: trocar `(§1.3/§8)` por `(§1.4/§8)`.

- [ ] **Step 3: Spec §5 e nota de genéricos**

Em §5, após o parágrafo "A struct is **nominal and its fields are fixed**…", acrescentar:

```markdown
A field may have any name, type keywords included: `map: int` is a field
called `map`, read and written as `s.map` (§1.2).
```

Na nota de §6 (~linha 1525) substituir o bloco `> **Note:** `map` is a type keyword…` por:

```markdown
> **Note:** `map` was reserved in every position before v0.23.5, which is why
> the `collections` module's transformation function is called `map_arr`.
> `map` is a contextual keyword now (§1.2) and `func map<A, B>(...)` parses;
> `map_arr` keeps its name for compatibility.
```

Se a próxima versão não for 0.23.5, escrever "before this version" — o release ajusta.

- [ ] **Step 4: CHANGELOG**

Inserir no topo de `CHANGELOG.md`, antes de `## [0.23.4] - 2026-09-04`:

```markdown
## [Unreleased]

Issue #134 — frontend: keywords de tipo contextuais, identificadores Unicode e
token de erro tipado no lexer.

### Added
- **Keywords de tipo são contextuais** (§1.2): `int`, `float`, `string`,
  `bool`, `bytes`, `void`, `any`, `map` e `chan` ficam reservadas só em
  posição de tipo — nome de struct (`struct map`), parâmetro de tipo
  (`Box<map>`) e membro de tipo qualificado (`io.map`) continuam com o erro
  `'map' is a keyword and cannot be used as a name`. Em toda posição de valor
  são nomes comuns: `let int: int = 5`, `let map = {}`, `func any() -> int`,
  parâmetro `map: Tile[][]`, `use src.map as map` com `map.tile()` e
  `let t: map.Tile`, campo `map: int` com `s.map`, `s.map = v`, `ref s.map` e
  `{s.map}` em f-string. Precedente: `int`, `string` e `any` são
  identificadores pré-declarados em Go; C#, Swift e Kotlin reservam keywords
  só onde têm significado. Um campo de struct com esses nomes era
  **descartado em silêncio** (o erro só aparecia no construtor); agora
  compila — e qualquer token que não é nome em posição de campo é erro de
  sintaxe. `ref` e `func` continuam reservados em toda posição.
- **Identificadores Unicode** (§1.3): `let café = 1`, `func área()` — letra
  e dígito por `unicode.IsLetter`/`IsDigit`, como Go, Python, Swift e Rust.
  Sem normalização: nomes comparam byte a byte.

### Changed
- **Caractere fora do alfabeto é um diagnóstico por caractere**, não por
  byte: `“abc”` colado de um editor dá dois `invalid syntax "“"`/`"”"`, não
  seis. A coluna de `[linha:coluna]` passa a contar caracteres — em linha
  com acento ela apontava o byte.
- Interno: o lexer distingue razão de erro (`token.LEXER_ERROR`:
  `unterminated string`, chave aberta em f-string) de caractere ilegal
  (`token.ILLEGAL`) pelo tipo do token; `lexer.IsReason` (contagem de runas
  do literal, 0.23.3) sai. `Display()` do novo tipo é `lexer error`.

```

- [ ] **Step 5: Verificar e commitar**

Run: `go test ./internal/vm/ -run TestString -count=1 && grep -n "1.3 Identifiers\|1.4 Operators\|1.5 Delimiters" docs/NOXY_LANGUAGE_SPEC.md`
Expected: PASS; três linhas de cabeçalho.

```bash
git add docs/NOXY_LANGUAGE_SPEC.md CHANGELOG.md internal/vm/string_ordering_test.go
git commit -m "docs(spec,changelog): keywords de tipo contextuais, identificadores Unicode, coluna em caracteres (issue #134)

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01Kc5SesDL6fvcJY6mNmMfwU"
```

---

### Task 7: verificação final

**Files:** nenhum novo.

- [ ] **Step 1: Verificação obrigatória do AGENTS.md**

```bash
go build ./... && go vet ./... && go test ./internal/... -count=1 && go run ./cmd/noxy noxy_examples/run_all_tests_concurrent.nx
```

Expected: build e vet limpos; todos os pacotes `ok`; runner sem falhas.

- [ ] **Step 2: gofmt e EOL**

```bash
gofmt -l internal/ cmd/
git diff --numstat develop..HEAD
```

Expected: `gofmt -l` vazio; nenhum arquivo com contagem de linhas igual ao tamanho inteiro do arquivo (sinal de reescrita por CRLF).

- [ ] **Step 3: Probes da issue com o binário novo**

```bash
go build -o /tmp/noxy-134 ./cmd/noxy
printf 'struct map\nend\n' | /tmp/noxy-134 /dev/stdin; echo "exit=$?"
printf 'let if = 1\n' | /tmp/noxy-134 /dev/stdin; echo "exit=$?"
printf 'let s: string = \xe2\x80\x9cabc\xe2\x80\x9d\n' | /tmp/noxy-134 /dev/stdin
```

Expected, na ordem: `'map' is a keyword and cannot be used as a name` (exit ≠ 0); `expected identifier, found if` (exit ≠ 0); exatamente duas linhas `invalid syntax`, nas colunas 17 e 21.

Se a CLI não aceitar `/dev/stdin`, gravar os programas em arquivos no scratchpad e passar o caminho.

- [ ] **Step 4: Reportar**

Sem commit. Listar ao usuário: os seis commits, o resultado das três verificações, e qualquer desvio da spec que tenha sido necessário (esperado: nenhum).

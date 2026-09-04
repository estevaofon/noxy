# Issue #126 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fechar os cinco achados do Deadrail: módulo `math` na stdlib, `m.f()`/`m.x` tipados pelo namespace, `pop` com índice opcional e `swap_remove`, aspas dentro de `{}` em f-string, e `any`/`map`/`chan` no §1.2 com `str` livre e erro único para keyword em posição de nome.

**Architecture:** Cada item é independente e segue o padrão que o repositório já usa para aquela camada: native `<mod>_nome` + wrapper `.nx` (stdlib), `compileBuiltinCall` + `unicizeThroughRefValue` (builtins mutáveis), `readQuoted` (lexer), `expectPeek` (parser), `importedBindingType` + `programViewType` (compilador). Item 2 é o único que altera comportamento observável de programas que hoje compilam (quebra deliberada, documentada).

**Tech Stack:** Go 1.25, módulo `noxy-vm`; testes `go test`; corpus `.nx` em `noxy_examples/`.

**Spec:** `docs/superpowers/specs/2026-09-03-issue-126-math-namespace-typing-design.md`

## Global Constraints

- Branch `feat/issue-126-math-namespace-typing`, commits `tipo(escopo): descrição em português (issue #126)`, rodapé `Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>` e `Claude-Session: https://claude.ai/code/session_01JPXzv6ZwqzCjsDXhDzG86p`.
- Verificação obrigatória após qualquer modificação (AGENTS.md): `go build ./... && go vet ./...`, `go test ./internal/... -count=1`, `go run ./cmd/noxy noxy_examples/run_all_tests_concurrent.nx` (da raiz).
- Guardas de arquitetura não são afrouxadas: `builtins_registry_test.go` (snapshot ordenado), `architecture_test.go` (arquivo ↔ `define<Area>Builtins`), `stdlib_hygiene_test.go`.
- Native novo: validar `len(args)` e `args[i].Type`; nunca asserção nua; erro tipado, nunca `null` para argumento inválido (#121). `str, ok := val.Obj.(T)`.
- Contêiner devolvido é dono dos filhos (`value.Retain`/`value.Release` só nos pontos do molde de `pop`).
- Compilador nunca escreve em stdout/stderr.
- `gofmt -d` limpo nos arquivos tocados; `git diff --numstat` sem arquivo reescrito por EOL.
- Todo `docs/*.md` passa pelo Liquid do Pages: `{{` literal só dentro de `<!-- {% raw %} -->`.
- Domínio inválido em `math` é erro de runtime (`math.sqrt: domain error (x < 0), got -1`), nunca NaN. Overflow para `±Inf` não é checado.
- `delete` continua só de map. `pop(ref arr[, i])` e `swap_remove(ref arr, i)` devolvem o elemento removido; posição inexistente é erro de runtime (`pop from empty array` para `pop` sem índice em array vazio; `array index out of bounds` para índice fora de `[0, len)`), nunca `null`.
- Item 2: tipo do membro de namespace = `importedBindingType` traduzido por `programViewType`; parte não nomeável ⇒ tipo inteiro `nil` (dinâmico), nunca meio-tipado.

---

### Task 1: `str` deixa de ser keyword

**Files:**
- Modify: `internal/token/token.go:46` (constante `TYPE_STR`), `:138` (entrada `"str"` em `keywords`)
- Modify: `internal/token/display.go:26`
- Test: `internal/lexer/lexer_test.go`

**Interfaces:**
- Produces: `token.LookupIdent("str") == token.IDENTIFIER`.

- [ ] **Step 1: Escrever o teste que falha**

Adicionar ao final de `internal/lexer/lexer_test.go`:

```go
// Issue #126 item 5: `str` era reservado em token.go (TYPE_STR) mas o parser
// nunca o consumia — reserva morta que so custava o identificador. Agora e
// um nome comum; `map`, `chan` e `any` continuam keywords (spec §1.2).
func TestStrIsAnIdentifierAndTypeKeywordsStayReserved(t *testing.T) {
	if got := token.LookupIdent("str"); got != token.IDENTIFIER {
		t.Fatalf("LookupIdent(\"str\") = %s, want IDENTIFIER", got)
	}
	for _, kw := range []struct {
		word string
		want token.TokenType
	}{{"map", token.MAP}, {"chan", token.CHAN}, {"any", token.TYPE_ANY}, {"string", token.TYPE_STRING}} {
		if got := token.LookupIdent(kw.word); got != kw.want {
			t.Fatalf("LookupIdent(%q) = %s, want %s", kw.word, got, kw.want)
		}
	}
	l := New("let str: int = 1\n")
	first := l.NextToken()
	second := l.NextToken()
	if first.Type != token.LET || second.Type != token.IDENTIFIER || second.Literal != "str" {
		t.Fatalf("tokens = %s %s(%q), want LET IDENTIFIER(\"str\")", first.Type, second.Type, second.Literal)
	}
}
```

- [ ] **Step 2: Rodar e ver falhar**

Run: `go test ./internal/lexer -run TestStrIsAnIdentifier -count=1`
Expected: FAIL com `LookupIdent("str") = TYPE_STR, want IDENTIFIER`.

- [ ] **Step 3: Remover a reserva**

Em `internal/token/token.go`: apagar a linha `TYPE_STR    TokenType = "TYPE_STR"` e a linha `"str":      TYPE_STR,`. Em `internal/token/display.go`: apagar a linha `TYPE_STR:    "str",`.

- [ ] **Step 4: Rodar e ver passar**

Run: `go build ./... && go test ./internal/token ./internal/lexer ./internal/parser -count=1`
Expected: PASS (nenhum outro arquivo referencia `TYPE_STR`; `go build` confirma).

- [ ] **Step 5: Commit**

```bash
git add internal/token/token.go internal/token/display.go internal/lexer/lexer_test.go
git commit -m "fix(token): 'str' deixa de ser keyword — reserva morta que o parser nunca consumia (issue #126)"
```

---

### Task 2: keyword em posição de nome dá um erro só, com o nome da keyword

**Files:**
- Modify: `internal/parser/parser.go:12-32` (struct `Parser`), `:98-104` (`peekError`), `:122-135` (`ParseProgram`), `:1179-1197` (`parseBlockStatement`)
- Test: `internal/parser/syntax_errors_test.go`

**Interfaces:**
- Produces: campo `Parser.resyncToLine bool`; método `(p *Parser) resyncAfterFailedStatement()`.

- [ ] **Step 1: Escrever os testes que falham**

Adicionar em `internal/parser/syntax_errors_test.go` (nova função, após `TestSyntaxErrorMessages`):

```go
// Issue #126 item 5: keyword onde se espera um nome (`use src.map as map`,
// `let map: int = 1`) dizia "expected identifier, found map" e, como o parser
// nao sincroniza, cada token seguinte virava mais um "invalid syntax". Agora e
// UM erro que nomeia a keyword, e o parser pula o resto da linha (recuperacao
// em modo panico com ponto de sincronizacao — Crafting Interpreters §6.3.3).
func TestKeywordAsNameIsASingleError(t *testing.T) {
	cases := []struct {
		name   string
		source string
		want   string
	}{
		{"use module named map", "use src.map as map\nlet x: int = 1\n", "[1:9] SyntaxError: 'map' is a keyword and cannot be used as a name"},
		{"use alias named map", "use src.level as map\nlet x: int = 1\n", "[1:18] SyntaxError: 'map' is a keyword and cannot be used as a name"},
		{"let named map", "let map: int = 1\nlet y: int = 2\n", "[1:5] SyntaxError: 'map' is a keyword and cannot be used as a name"},
		{"let named chan inside block", "func f()\n    let chan: int = 1\n    let y: int = 2\nend\n", "[2:9] SyntaxError: 'chan' is a keyword and cannot be used as a name"},
		{"param named any", "func f(any: int)\nend\n", "'any' is a keyword and cannot be used as a name"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := New(lexer.New(tc.source))
			_ = p.ParseProgram()
			errs := p.Errors()
			if len(errs) != 1 {
				t.Fatalf("want exactly 1 error, got %d: %v", len(errs), errs)
			}
			if !strings.Contains(errs[0], tc.want) {
				t.Fatalf("error %q does not contain %q", errs[0], tc.want)
			}
			if !strings.Contains(errs[0], "hint: rename it") {
				t.Fatalf("error %q has no rename hint", errs[0])
			}
		})
	}
}
```

- [ ] **Step 2: Rodar e ver falhar**

Run: `go test ./internal/parser -run TestKeywordAsNameIsASingleError -count=1`
Expected: FAIL (mensagem `expected identifier, found map` e mais de um erro).

- [ ] **Step 3: Implementar**

Em `internal/parser/parser.go`, no struct `Parser` (após `pendingTokens`):

```go
	// resyncToLine e ligado por peekError quando uma KEYWORD aparece onde
	// se esperava um identificador (`let map: int`, `use src.map`): o
	// statement falhou com um erro que ja nomeia a causa, e os tokens que
	// sobram na linha so produziriam "invalid syntax" em cascata. ParseProgram
	// e parseBlockStatement consomem a marca pulando ate o fim da linha
	// (recuperacao em modo panico com ponto de sincronizacao). E especifica
	// deste erro para nao mudar as demais mensagens que syntax_errors_test
	// fixa.
	resyncToLine bool
```

Substituir `peekError` inteiro (linhas 98-104) por:

```go
func (p *Parser) peekError(t token.TokenType) {
	if t == token.IDENTIFIER && p.peekToken.Type != token.IDENTIFIER && token.LookupIdent(p.peekToken.Literal) == p.peekToken.Type && p.peekToken.Type != token.EOF && p.peekToken.Type != token.NEWLINE {
		// Keyword em posicao de nome (issue #126 item 5).
		p.errors = append(p.errors, fmt.Sprintf("[%d:%d] SyntaxError: '%s' is a keyword and cannot be used as a name\n  hint: rename it (e.g. '%s_' or a more specific word)",
			p.peekToken.Line, p.peekToken.Column, p.peekToken.Literal, p.peekToken.Literal))
		p.resyncToLine = true
		return
	}
	msg := fmt.Sprintf("[%d:%d] SyntaxError: expected %s, found %s",
		p.peekToken.Line, p.peekToken.Column, t.Display(), p.peekToken.Type.Display())
	p.errors = append(p.errors, msg)
}

// resyncAfterFailedStatement consome a marca resyncToLine: avanca ate o
// NEWLINE (ou EOF) da linha em que o statement falhou, para que o laco
// chamador retome no proximo statement em vez de reinterpretar o resto da
// linha. Sem marca, nao faz nada.
func (p *Parser) resyncAfterFailedStatement() {
	if !p.resyncToLine {
		return
	}
	p.resyncToLine = false
	for !p.curTokenIs(token.NEWLINE) && !p.curTokenIs(token.EOF) {
		p.nextToken()
	}
}
```

A condição `token.LookupIdent(p.peekToken.Literal) == p.peekToken.Type` é o que distingue keyword (literal `map`, tipo `MAP`) de pontuação (`:` tem literal `:` e `LookupIdent(":")` é `IDENTIFIER` ≠ `COLON`).

Em `ParseProgram` e em `parseBlockStatement`, trocar o corpo do laço:

```go
		stmt := p.parseStatement()
		if stmt != nil {
			program.Statements = append(program.Statements, stmt)
		} else {
			p.resyncAfterFailedStatement()
		}
		p.nextToken()
```

(em `parseBlockStatement` é `block.Statements`). Também em `parseCaseBody` (`parser.go:1199`) se o laço tiver a mesma forma — conferir com `sed -n 1199,1220p internal/parser/parser.go` e aplicar o mesmo `else`.

- [ ] **Step 4: Rodar e ver passar**

Run: `go test ./internal/parser -count=1`
Expected: PASS, inclusive `TestTruncatedConstructsReportExpectedToken` (os casos dele têm `found newline`/`found ':'`/`found int` — `int` é keyword de tipo: o caso `"let without colon", "let x int = 1"` espera `expected ':', found int`, e continua igual porque `t` ali é `COLON`, não `IDENTIFIER`).

Se o caso `"param without colon"` ou outro mudar, o teste diz; a condição `t == token.IDENTIFIER` deve limitar a mudança aos sites que pedem nome.

- [ ] **Step 5: Commit**

```bash
git add internal/parser/parser.go internal/parser/syntax_errors_test.go
git commit -m "fix(parser): keyword em posição de nome dá um erro só, nomeando a keyword, e sincroniza na linha (issue #126)"
```

---

### Task 3: aspas dentro de `{}` de f-string (PEP 701)

**Files:**
- Modify: `internal/lexer/lexer.go:499-528` (`readQuoted`)
- Modify: `internal/parser/parser.go:1680-1687` (`noPrefixParseFnError`), `:1046-1048` (hint de `unclosed brace`)
- Modify: `internal/parser/parser_test.go:174-195` (comentário e casos)
- Test: `internal/lexer/literals_test.go`, `internal/parser/syntax_errors_test.go`

**Interfaces:**
- Produces: `FSTRING` cujo literal contém, dentro de `{...}`, aspas iguais às do delimitador; token `ILLEGAL` com literal `unclosed brace in f-string\n  hint: ...` quando uma `{` de expressão não fecha na linha.

- [ ] **Step 1: Testes do lexer que falham**

Adicionar ao final de `internal/lexer/literals_test.go`:

```go
// Issue #126 item 3 (PEP 701): dentro de `{...}` de uma f-string, uma aspa
// igual a do delimitador abre um literal aninhado em vez de fechar a
// f-string. O lexer conta a profundidade de chaves; o parser continua
// recebendo o literal inteiro e re-lexando cada `{...}`.
func TestFStringQuotesInsideBraces(t *testing.T) {
	cases := []struct {
		name, source, want string
	}{
		{"double inside double", `f"n = {fmt("%03d", n)}"`, `n = {fmt("%03d", n)}`},
		{"single inside single", `f'{fmt('%d', n)}'`, `{fmt('%d', n)}`},
		{"map literal with space", `f"{ {"a": 1}["a"] }"`, `{ {"a": 1}["a"] }`},
		{"escaped quote inside nested string stays verbatim", `f"{s + "a\"b"}"`, `{s + "a\"b"}`},
		{"literal braces still escape at depth zero", `f"{{x}} = {x}"`, `{{x}} = {x}`},
		{"nested braces close in order", `f"{{{x}}}"`, `{{{x}}}`},
		{"brace char inside nested string does not count", `f"{"}"}!"`, `{"}"}!`},
		{"text after the expression", `f"{a}: {b}"`, `{a}: {b}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			requireLiteral(t, tc.source, token.FSTRING, tc.want)
		})
	}
}

func TestFStringUnclosedBraceIsReportedByTheLexer(t *testing.T) {
	requireIllegal(t, "f\"{x\"\n", "unclosed brace in f-string")
	requireIllegal(t, "f\"{x\"\n", "hint:")
	requireIllegal(t, `f"{x`, "unclosed brace in f-string")
	requireIllegal(t, `f"{"abc}"`, "unterminated")
}
```

- [ ] **Step 2: Rodar e ver falhar**

Run: `go test ./internal/lexer -run 'TestFStringQuotesInsideBraces|TestFStringUnclosedBrace' -count=1`
Expected: FAIL (`f"n = {fmt("` lexa como FSTRING `n = {fmt(`).

- [ ] **Step 3: Implementar no lexer**

Substituir `readQuoted` (linhas 499-528 de `internal/lexer/lexer.go`) por:

```go
// readQuoted scans the body of a quoted literal. An empty reason means
// success; otherwise it describes why the literal is illegal.
//
// F-strings are brace-aware (issue #126, the PEP 701 rule): inside a `{...}`
// expression a quote equal to the delimiter opens a nested string literal
// (copied verbatim, escapes included — the parser re-lexes the expression)
// instead of closing the f-string. At depth 0 `{{` and `}}` are literal
// braces and do not change the depth. A `{` still open at a raw newline or
// at EOF is reported here, next to its cause.
func (l *Lexer) readQuoted(quote byte, kind literalKind) (string, string) {
	l.readChar() // Skip opening quote

	var out []byte
	depth := 0 // f-string only: `{` of expressions still open

	for {
		if l.ch == 0 {
			if depth > 0 {
				return string(out), unclosedBraceReason
			}
			return string(out), "unterminated " + kind.name()
		}
		if l.ch == quote && depth == 0 {
			break
		}
		switch {
		case kind == literalFString && depth > 0 && l.ch == '\n':
			return string(out), unclosedBraceReason
		case kind == literalFString && depth == 0 && (l.ch == '{' || l.ch == '}') && l.peekChar() == l.ch:
			out = append(out, l.ch, l.ch)
			l.readChar()
		case kind == literalFString && l.ch == '{':
			depth++
			out = append(out, '{')
		case kind == literalFString && depth > 0 && l.ch == '}':
			depth--
			out = append(out, '}')
		case kind == literalFString && depth > 0 && (l.ch == '"' || l.ch == '\''):
			var reason string
			out, reason = l.readNestedQuoted(out, l.ch)
			if reason != "" {
				return string(out), reason
			}
		case l.ch == '\\':
			l.readChar() // Skip backslash
			if l.ch == 0 {
				return string(out), "unterminated " + kind.name()
			}
			var reason string
			out, reason = l.readEscape(out, kind)
			if reason != "" {
				return string(out), reason
			}
		default:
			out = append(out, l.ch)
		}
		l.readChar()
	}
	return string(out), ""
}

const unclosedBraceReason = "unclosed brace in f-string\n  hint: every '{' that starts an expression needs a matching '}'; write '{{' for a literal brace"

// readNestedQuoted copies a string literal that appears INSIDE an f-string
// expression, verbatim (opening quote, body with escapes untouched, closing
// quote), leaving l.ch on the closing quote. The parser re-lexes the
// expression, so the escapes are interpreted there.
func (l *Lexer) readNestedQuoted(out []byte, quote byte) ([]byte, string) {
	out = append(out, quote)
	l.readChar()
	for {
		if l.ch == 0 {
			return out, "unterminated string inside f-string expression"
		}
		if l.ch == quote {
			return append(out, quote), ""
		}
		if l.ch == '\\' {
			out = append(out, '\\')
			l.readChar()
			if l.ch == 0 {
				return out, "unterminated string inside f-string expression"
			}
		}
		out = append(out, l.ch)
		l.readChar()
	}
}
```

- [ ] **Step 4: Rodar os testes do lexer**

Run: `go test ./internal/lexer -count=1`
Expected: PASS. `TestUnterminatedLiteralsStillReported` (`f"abc` → `unterminated`) continua passando porque `depth == 0`. Se o caso `f"{"abc}"` não der `unterminated`, a razão precisa conter a palavra (`unterminated string inside f-string expression` contém).

- [ ] **Step 5: Parser — ILLEGAL vira `SyntaxError: <razão>` e o hint residual**

Em `internal/parser/parser.go`, substituir `noPrefixParseFnError` por:

```go
func (p *Parser) noPrefixParseFnError(t token.TokenType) {
	msg := fmt.Sprintf("[%d:%d] SyntaxError: invalid syntax %q", p.curToken.Line, p.curToken.Column, p.curToken.Literal)
	switch p.curToken.Type {
	case token.EOF:
		msg = fmt.Sprintf("[%d:%d] SyntaxError: unexpected EOF", p.curToken.Line, p.curToken.Column)
	case token.ILLEGAL:
		// O literal de um ILLEGAL e a razao que o lexer escreveu
		// ("unterminated string", "unclosed brace in f-string" + hint).
		msg = fmt.Sprintf("[%d:%d] SyntaxError: %s", p.curToken.Line, p.curToken.Column, p.curToken.Literal)
	}
	p.errors = append(p.errors, msg)
}
```

Em `parseFString` (linha ~1047), o erro residual passa a carregar o mesmo hint:

```go
			if j >= len(literal) {
				p.errors = append(p.errors, fmt.Sprintf("[%d:%d] SyntaxError: %s", line, column, "unclosed brace in f-string\n  hint: every '{' that starts an expression needs a matching '}'; write '{{' for a literal brace"))
				return nil
			}
```

- [ ] **Step 5b: Parser — a varredura de `{...}` também pula literais aninhados**

O laço de `parseFString` que procura a `}` correspondente (`parser.go:1036-1048`) conta chaves sem olhar aspas: com o lexer novo, `f"{"}"}!"` chega ao parser como `{"}"}!` e a `}` dentro das aspas fecharia a expressão cedo demais. Substituir o laço por:

```go
			braceCount := 1
			j := i + 1
		scan:
			for ; j < len(literal); j++ {
				switch literal[j] {
				case '{':
					braceCount++
				case '}':
					braceCount--
					if braceCount == 0 {
						break scan
					}
				case '"', '\'':
					// Literal aninhado (PEP 701, issue #126): a expressao pode
					// conter strings; chaves dentro delas nao contam.
					j = skipNestedQuoted(literal, j)
				}
			}
```

e adicionar, após `parseFString`:

```go
// skipNestedQuoted devolve o indice da aspa que fecha o literal de string
// aberto em literal[start] (pulando `\x`), ou len(literal)-1 se ele nao
// fecha — o chamador entao cai em "unclosed brace".
func skipNestedQuoted(literal string, start int) int {
	quote := literal[start]
	for k := start + 1; k < len(literal); k++ {
		switch literal[k] {
		case '\\':
			k++
		case quote:
			return k
		}
	}
	return len(literal) - 1
}
```

Caso de teste correspondente (adicionar à lista do Step 6): `"f\"{\"}\"}!\"\n"` deve compilar sem erro, e no teste ponta a ponta do Step 7 acrescentar `{"}"}` à f-string com saída `n = 007 ! }`.

- [ ] **Step 6: Atualizar testes do parser**

Em `internal/parser/parser_test.go`, `TestFStringBraceEscapesAndTrailingTokenError`: substituir o comentário e a lista de fontes por:

```go
	// Issue #126 item 3 (PEP 701): o lexer conta chaves, entao aspas iguais
	// as do delimitador dentro de `{...}` abrem um literal aninhado — nao ha
	// mais a regra "use f'...' quando a expressao tem aspas duplas".
	for _, source := range []string{
		"f\"{{x}}\"\n", "f\"{{{x}}}\"\n", "f'{\"a\"}'\n", "f'{ {\"a\": 1}[\"a\"] }'\n",
		"f\"{\"a\"}\"\n", "f\"{ {\"a\": 1}[\"a\"] }\"\n", "f\"n = {fmt(\"%03d\", n)}\"\n", "f'{fmt('%d', n)}'\n",
	} {
```

Em `internal/parser/syntax_errors_test.go`, no caso `"f-string with unclosed brace"`, trocar `want` por `[]string{"SyntaxError: unclosed brace in f-string", "hint: every '{' that starts an expression"}`. Adicionar o caso:

```go
		{
			name:   "unterminated string is a SyntaxError with the lexer reason",
			source: "let s: string = \"abc\n",
			want:   []string{"SyntaxError: unterminated string"},
		},
```

- [ ] **Step 7: Teste ponta a ponta**

Adicionar em `internal/vm/builtins_core_test.go` (ou, se não houver um lugar de f-string ali, em `internal/vm/vm_test.go`) :

```go
// Issue #126 item 3: aspas duplas dentro de `{}` de uma f"..." (PEP 701).
func TestFStringDoubleQuotesInsideBracesRunEndToEnd(t *testing.T) {
	got := captureVMSource(t, "let n: int = 7\ntest_report(f\"n = {fmt(\"%03d\", n)} {\"!\"} {\"}\"}\")\n")
	if s, _ := got.Obj.(string); s != "n = 007 ! }" {
		t.Fatalf("got %q, want %q", s, "n = 007 ! }")
	}
}
```

- [ ] **Step 8: Rodar e ver passar**

Run: `go test ./internal/lexer ./internal/parser ./internal/vm -run 'FString|SyntaxError|Truncated' -count=1`
Expected: PASS.

- [ ] **Step 9: Commit**

```bash
git add internal/lexer/lexer.go internal/lexer/literals_test.go internal/parser/parser.go internal/parser/parser_test.go internal/parser/syntax_errors_test.go internal/vm/
git commit -m "feat(lexer): aspas iguais ao delimitador dentro de {} de f-string (PEP 701); ILLEGAL vira SyntaxError com a razão (issue #126)"
```

---

### Task 4: `pop(ref arr[, i])` com índice opcional e native `swap_remove`

**Files:**
- Modify: `internal/vm/builtins_collections.go:193-226` (bloco de `pop`)
- Modify: `internal/vm/builtins_registry_test.go:13-49` (snapshot: `swap_remove`)
- Modify: `internal/vm/builtins_collections_test.go:126-140` (expectativas antigas de `pop`)
- Modify: `internal/vm/native_signatures_test.go` (assinatura fixada de `pop`)
- Test: `internal/vm/builtins_collections_test.go`

**Interfaces:**
- Produces: native `pop(ref arr[, i]) -> elemento` com `NativeSignature{Arity: 1, Variadic: true, Params: [{IsRef: true, "ref array"}, {"int"}], ReturnType: "any"}`; native `swap_remove(ref arr, i) -> elemento` com `{Arity: 2, Params: [{IsRef: true, "ref array"}, {"int"}], ReturnType: "any"}`. Erros: `pop from empty array` (sem índice, vazio), `array index out of bounds` (índice fora de `[0, len)`), `<nome>: index must be an int, got <tipo>`, `pop: expects 1 or 2 arguments, got N`, `swap_remove: expects exactly 2 arguments, got N`.

Decisão registrada na spec (§4, revisão de 2026-09-03): **não há `remove_at`**. `pop` ganha índice opcional como `list.pop([i])` do Python; `pop` em posição inexistente — inclusive `pop(ref xs)` em array vazio, que hoje devolve `null` sob retorno tipado `T` — vira erro de runtime (regra da #121). `swap_remove` continua builtin próprio (Rust `Vec::swap_remove`).

- [ ] **Step 1: Testes que falham**

Em `internal/vm/builtins_collections_test.go`, no teste existente (linhas ~126-140), trocar as duas expectativas antigas de `pop` em array vazio e de `pop` sobre não-ref:

```go
	assertBuiltinValue(t, callBuiltin(t, machine, "pop", arrayRef), value.NewInt(1))
	assertBuiltinArray(t, storedArray, nil)
	// Issue #126 (regra da #121): pop em array vazio e erro, nao null sentinela.
	if _, err := requireBuiltin(t, machine, "pop").Invoke(machine, []value.Value{arrayRef}); err == nil || !strings.Contains(err.Error(), "pop from empty array") {
		t.Fatalf("pop on empty array: err = %v, want pop from empty array", err)
	}
```

e, para o `invalidArray` (argumento que não é ref), trocar `assertBuiltinValue(t, callBuiltin(t, machine, "pop", invalidArray), value.NewNull())` por uma chamada via `Invoke` esperando erro (qualquer erro; o texto vem de `unicizeThroughRefValue`), mantendo `assertBuiltinArray(t, invalidArray, []value.Value{value.NewInt(9)})` logo depois. Se a suíte tiver outra asserção de `pop` vazio → null (`grep -n '"pop"' internal/vm/*_test.go`), aplicar a mesma troca.

Adicionar a função nova:

```go
// Issue #126 item 4: pop com indice opcional (Python list.pop([i])) e
// swap_remove (Rust Vec::swap_remove). Devolvem o elemento; posicao
// inexistente e erro, como indexar.
func TestPopWithIndexAndSwapRemoveBuiltins(t *testing.T) {
	machine := New()
	stored := value.NewArray([]value.Value{value.NewInt(10), value.NewInt(20), value.NewInt(30), value.NewInt(40)})
	ref := pointerReference(&stored)

	assertBuiltinValue(t, callBuiltin(t, machine, "pop", ref, value.NewInt(1)), value.NewInt(20))
	assertBuiltinArray(t, stored, []value.Value{value.NewInt(10), value.NewInt(30), value.NewInt(40)})

	assertBuiltinValue(t, callBuiltin(t, machine, "swap_remove", ref, value.NewInt(0)), value.NewInt(10))
	assertBuiltinArray(t, stored, []value.Value{value.NewInt(40), value.NewInt(30)})

	assertBuiltinValue(t, callBuiltin(t, machine, "pop", ref), value.NewInt(30))
	assertBuiltinValue(t, callBuiltin(t, machine, "swap_remove", ref, value.NewInt(0)), value.NewInt(40))
	assertBuiltinArray(t, stored, nil)

	for _, name := range []string{"pop", "swap_remove"} {
		stored = value.NewArray([]value.Value{value.NewInt(1)})
		ref = pointerReference(&stored)
		for _, idx := range []int64{1, -1} {
			_, err := requireBuiltin(t, machine, name).Invoke(machine, []value.Value{ref, value.NewInt(idx)})
			if err == nil || !strings.Contains(err.Error(), "array index out of bounds") {
				t.Fatalf("%s(%d): err = %v, want array index out of bounds", name, idx, err)
			}
		}
		assertBuiltinArray(t, stored, []value.Value{value.NewInt(1)})
		if _, err := requireBuiltin(t, machine, name).Invoke(machine, []value.Value{ref, value.NewString("0")}); err == nil || !strings.Contains(err.Error(), "index must be an int, got string") {
			t.Fatalf("%s with string index: err = %v", name, err)
		}
	}
	if _, err := requireBuiltin(t, machine, "pop").Invoke(machine, []value.Value{ref, value.NewInt(0), value.NewInt(0)}); err == nil || !strings.Contains(err.Error(), "pop: expects 1 or 2 arguments, got 3") {
		t.Fatalf("pop arity: err = %v", err)
	}
	if _, err := requireBuiltin(t, machine, "swap_remove").Invoke(machine, []value.Value{ref}); err == nil || !strings.Contains(err.Error(), "swap_remove: expects exactly 2 arguments, got 1") {
		t.Fatalf("swap_remove arity: err = %v", err)
	}
}
```

(`strings` no import do arquivo: conferir com `head -12`.)

Em `internal/vm/native_signatures_test.go`, no teste que fixa as assinaturas (`TestBuiltinNativeSignatures`, ~linha 85), atualizar a entrada de `pop` para `Arity: 1, Variadic: true, Params: [{IsRef: true, TypeName: "ref array"}, {IsRef: false, TypeName: "int"}], ReturnType: "any"` e adicionar `swap_remove` com `Arity: 2` e os mesmos `Params`/`ReturnType`, no formato das entradas existentes.

Os testes de CoW e ponta a ponta (compilam fonte Noxy com `pop(ref a, 1)`/`swap_remove`) ficam para a Task 5, que ensina o compilador.

- [ ] **Step 2: Rodar e ver falhar**

Run: `go test ./internal/vm -run 'TestPopWithIndexAndSwapRemoveBuiltins|TestBuiltinNativeSignatures' -count=1`
Expected: FAIL (`builtin "swap_remove" is not registered`; `pop` com 2 args devolve null).

- [ ] **Step 3: Implementar**

Em `internal/vm/builtins_collections.go`, substituir o bloco de `pop` (de `popSignature := ...` até o fechamento do `DefineContextualNativeWithSignature("pop", ...)`) por:

```go
	// Issue #126 item 4: pop com indice opcional (Python list.pop([i])) e
	// swap_remove (Rust Vec::swap_remove, O(1) sem preservar ordem). Posicao
	// inexistente — inclusive pop em array vazio, que devolvia null sob um
	// retorno tipado T — e erro de runtime (regra da #121). `delete`
	// continua so de map, como em Go.
	popSignature := value.NativeSignature{
		Arity:    1,
		Variadic: true, // 1 ou 2 argumentos: Variadic + len(Params) == 2 (defer.go)
		Params: []value.ParamInfo{
			{IsRef: true, TypeName: "ref array"},
			{IsRef: false, TypeName: "int"},
		},
		ReturnType: "any",
	}
	vm.DefineContextualNativeWithSignature("pop", popSignature, func(context value.NativeContext, args []value.Value) (value.Value, error) {
		if len(args) < 1 || len(args) > 2 {
			return value.NewNull(), fmt.Errorf("pop: expects 1 or 2 arguments, got %d", len(args))
		}
		return removeArrayElement(context, "pop", args, false)
	})
	swapRemoveSignature := value.NativeSignature{
		Arity: 2,
		Params: []value.ParamInfo{
			{IsRef: true, TypeName: "ref array"},
			{IsRef: false, TypeName: "int"},
		},
		ReturnType: "any",
	}
	vm.DefineContextualNativeWithSignature("swap_remove", swapRemoveSignature, func(context value.NativeContext, args []value.Value) (value.Value, error) {
		if len(args) != 2 {
			return value.NewNull(), fmt.Errorf("swap_remove: expects exactly 2 arguments, got %d", len(args))
		}
		return removeArrayElement(context, "swap_remove", args, true)
	})
```

E, no fim do arquivo:

```go
// removeArrayElement e o corpo comum de pop (sem indice: o ultimo; com
// indice: aquela posicao, preservando a ordem, O(n)) e swap_remove (troca com
// o ultimo, O(1)). Passa pelo mesmo funil de CoW de append/delete
// (unicizeThroughRefValue). Posicao inexistente e erro com a mesma mensagem
// da indexacao; argumento invalido e erro tipado, nunca null (#121).
func removeArrayElement(context value.NativeContext, name string, args []value.Value, swap bool) (value.Value, error) {
	machine, contextErr := nativeVM(context)
	if contextErr != nil {
		return value.NewNull(), contextErr
	}
	if len(args) == 2 && args[1].Type != value.VAL_INT {
		return value.NewNull(), fmt.Errorf("%s: index must be an int, got %s", name, runtimeTypeName(args[1]))
	}
	arrVal, err := machine.unicizeThroughRefValue(args[0])
	if err != nil {
		return value.NewNull(), err
	}
	arr, ok := arrVal.Obj.(*value.ObjArray)
	if arrVal.Type != value.VAL_OBJ || !ok {
		return value.NewNull(), fmt.Errorf("%s: expects an array, got %s", name, runtimeTypeName(arrVal))
	}
	last := len(arr.Elements) - 1
	idx := int64(last)
	if len(args) == 2 {
		idx = args[1].Int()
	}
	if last < 0 && len(args) == 1 {
		return value.NewNull(), fmt.Errorf("pop from empty array")
	}
	if idx < 0 || idx > int64(last) {
		return value.NewNull(), fmt.Errorf("array index out of bounds")
	}
	removed := arr.Elements[idx]
	if swap {
		arr.Elements[idx] = arr.Elements[last]
	} else {
		copy(arr.Elements[idx:], arr.Elements[idx+1:])
	}
	arr.Elements[last] = value.NewNull()
	arr.Elements = arr.Elements[:last]
	value.Release(removed) // RC: o array solta a posse duravel do elemento removido; o chamador recebe o valor
	return removed, nil
}
```

Conferir que `fmt` está importado em `builtins_collections.go`; se não, adicionar. O `pop` antigo devolvia `null` para argumento não-ref (`unicizeThroughRefValue` com erro) — agora o erro sobe; é o comportamento desejado (#121).

Em `internal/vm/builtins_registry_test.go`, inserir `"swap_remove"` em ordem lexicográfica (entre `"strings_to_upper"`/último `strings_*` e `"sys_argv"`); o teste imprime a ordem esperada se errar.

- [ ] **Step 4: Rodar e ver passar**

Run: `go test ./internal/vm -run 'TestPopWithIndexAndSwapRemoveBuiltins|TestBuiltinNativeSignatures|TestBuiltinRegistrySnapshot|TestBuiltinSourceLayout|TestEveryNativeIsRegisteredExactlyOnce|Collection' -count=1`
Expected: PASS. Depois `go test ./internal/vm -count=1` inteiro: qualquer teste que dependia de `pop` vazio → `null` deve ser atualizado para esperar o erro (listar no relatório).

- [ ] **Step 5: Commit**

```bash
git add internal/vm/builtins_collections.go internal/vm/builtins_collections_test.go internal/vm/native_signatures_test.go internal/vm/builtins_registry_test.go
git commit -m "feat(vm): pop com índice opcional e swap_remove — posição inexistente é erro, inclusive pop em array vazio (issue #126)"
```

---

### Task 5: `pop(ref arr[, i])` e `swap_remove` no compilador

**Files:**
- Modify: `internal/compiler/builtin_calls.go:46` (filtro), `:69` (aridade), `:125-134` (case `pop`)
- Modify: `internal/compiler/narrowing.go:347-352` (`pureBuiltins`)
- Test: `internal/compiler/builtin_calls_test.go`, `internal/compiler/function_conformance_examples_test.go`, `internal/vm/native_signatures_test.go`, `internal/vm/cow_builtins_test.go`, `noxy_examples/type_errors/swap_remove_without_ref.nx`, `noxy_examples/test_pop_index.nx`

**Interfaces:**
- Consumes: natives da Task 4.
- Produces: `pop(ref xs)`, `pop(ref xs, i)` e `swap_remove(ref xs, i)` compilam com tipo de retorno `T` para `xs: T[]`.

- [ ] **Step 1: Testes que falham**

Em `internal/compiler/builtin_calls_test.go`, ver como `TestMutatingBuiltinTypeContracts` (linha ~327) e `TestMutatingBuiltinArityContracts` (~284) chamam o helper (nome e assinatura; tabela `{src, want}` ou chamadas diretas) e adicionar no mesmo estilo:

```go
	// Issue #126 item 4
	{`let xs: int[] = [1]
swap_remove(xs, 0)`, "argument 1 to 'swap_remove': expected ref T[], got int[]"},
	{`let m: map[string, int] = {}
swap_remove(ref m, 0)`, "swap_remove expects an array, got map[string, int]"},
	{`let xs: int[] = [1]
swap_remove(ref xs, "0")`, "argument 2 to 'swap_remove': expected int, got string"},
	{`let xs: int[] = [1]
pop(ref xs, "0")`, "argument 2 to 'pop': expected int, got string"},
	{`let xs: int[] = [1]
swap_remove(ref xs)`, "swap_remove expects 2 arguments, got 1"},
	{`let xs: int[] = [1]
pop(ref xs, 0, 0)`, "pop expects 1 or 2 arguments, got 3"},
	{`let xs: string[] = ["a"]
let n: int = pop(ref xs, 0)`, "expected int, got string"},
```

Se `TestMutatingBuiltinArityContracts` fixa `pop expects 1 arguments, got 2` para `pop(ref xs, 0)`, esse caso antigo sai (agora é válido) — substituí-lo pelo `pop(ref xs, 0, 0)` acima.

Caso positivo:

```go
func TestPopWithIndexAndSwapRemoveReturnElementType(t *testing.T) {
	src := `let xs: string[] = ["a", "b", "c"]
let first = pop(ref xs, 0)
let last = pop(ref xs)
let only = swap_remove(ref xs, 0)
`
	for _, name := range []string{"first", "last", "only"} {
		if got := inferredLetType(t, src, name); got != "string" {
			t.Fatalf("%s: %s, want string", name, got)
		}
	}
}
```

Em `internal/vm/native_signatures_test.go`, após `TestTypedMutatingBuiltinsPreserveSourceSyntax`:

```go
func TestTypedPopWithIndexAndSwapRemove(t *testing.T) {
	got := runTypedFunctionProgram(t, `
let values: int[] = [10, 20, 30, 40]
let a: int = pop(ref values, 1)
let b: int = swap_remove(ref values, 0)
let c: int = pop(ref values)
test_report(a * 1000 + b * 10 + c + length(values) * 100000)`)
	testExpectedObject(t, 100000+20000+400+30, got)
}

func TestPopOutOfRangeAndEmptyAreRuntimeErrors(t *testing.T) {
	err := interpretOrCompileErr(t, New(), "let values: int[] = [1]\nlet x: int = pop(ref values, 5)\n")
	if err == nil || !strings.Contains(err.Error(), "array index out of bounds") {
		t.Fatalf("err = %v, want array index out of bounds", err)
	}
	err = interpretOrCompileErr(t, New(), "let values: int[] = []\nlet x: int = pop(ref values)\n")
	if err == nil || !strings.Contains(err.Error(), "pop from empty array") {
		t.Fatalf("err = %v, want pop from empty array", err)
	}
}
```

Em `internal/vm/cow_builtins_test.go`, após `TestDeleteUnicizesSharedTarget`:

```go
func TestPopWithIndexUnicizesSharedTarget(t *testing.T) {
	machine, original := newMarkingVM()
	if err := interpretVMSource(t, machine, `let a: int[]
append(ref a, 1)
append(ref a, 2)
append(ref a, 3)
test_mark_shared(a)
let removed: int = pop(ref a, 1)
`); err != nil {
		t.Fatalf("vm error: %v", err)
	}
	after, _ := machine.GetGlobal("a")
	if after.Obj == original.Obj {
		t.Fatal("pop com indice em array Shared deveria ter clonado (CoW)")
	}
	if n := len(original.Obj.(*value.ObjArray).Elements); n != 3 {
		t.Fatalf("o objeto original não pode ter sido mutado, tem %d elementos", n)
	}
	got := after.Obj.(*value.ObjArray).Elements
	if len(got) != 2 || got[0].Int() != 1 || got[1].Int() != 3 {
		t.Fatalf("o clone deve refletir o pop(ref a, 1), tem %v", got)
	}
	removed, _ := machine.GetGlobal("removed")
	if removed.Int() != 2 {
		t.Fatalf("removed = %v, want 2", removed)
	}
}

func TestSwapRemoveUnicizesSharedTarget(t *testing.T) {
	machine, original := newMarkingVM()
	if err := interpretVMSource(t, machine, `let a: int[]
append(ref a, 1)
append(ref a, 2)
append(ref a, 3)
test_mark_shared(a)
let removed: int = swap_remove(ref a, 0)
`); err != nil {
		t.Fatalf("vm error: %v", err)
	}
	after, _ := machine.GetGlobal("a")
	if after.Obj == original.Obj {
		t.Fatal("swap_remove em array Shared deveria ter clonado (CoW)")
	}
	if n := len(original.Obj.(*value.ObjArray).Elements); n != 3 {
		t.Fatalf("o objeto original não pode ter sido mutado, tem %d elementos", n)
	}
	got := after.Obj.(*value.ObjArray).Elements
	if len(got) != 2 || got[0].Int() != 3 || got[1].Int() != 2 {
		t.Fatalf("o clone deve refletir o swap_remove ([3, 2]), tem %v", got)
	}
}

// O elemento composto removido continua vivo no chamador: o array solta a
// posse (Release) e o valor devolvido e o mesmo objeto, sem double free.
func TestPopWithIndexReleasesCompositeElementOnce(t *testing.T) {
	machine := New()
	if err := interpretVMSource(t, machine, `let xs: int[][] = [[1], [2, 2], [3]]
let mid: int[] = pop(ref xs, 1)
let n: int = length(mid) + length(xs)
`); err != nil {
		t.Fatalf("vm error: %v", err)
	}
	n, _ := machine.GetGlobal("n")
	if n.Int() != 4 {
		t.Fatalf("n = %v, want 4 (2 + 2)", n)
	}
}
```

Criar `noxy_examples/type_errors/swap_remove_without_ref.nx` (mesmo formato de `ref_builtin_without_ref.nx` — copiar o cabeçalho de comentário dele):

```noxy
// swap_remove exige `ref` no argumento 1 (spec §2.3 R5) — erro de compilação:
// argument 1 to 'swap_remove': expected ref T[], got int[]
let xs: int[] = [1, 2, 3]
let v: int = swap_remove(xs, 1)
```

E registrar na tabela de `TestTypedFunctionInvalidConformanceExamplesFail` em `internal/compiler/function_conformance_examples_test.go` (linha ~48, após `"ref builtin without ref"`):

```go
		{"swap_remove without ref", "swap_remove_without_ref.nx", "argument 1 to 'swap_remove': expected ref T[], got int[]\n  hint: use 'ref xs'"},
```

(`conformanceDiagnosticMatches` compara por sufixo, então o hint faz parte do `want`.)

- [ ] **Step 2: Rodar e ver falhar**

Run: `go test ./internal/compiler -run 'TestMutatingBuiltin|TestPopWithIndexAndSwapRemoveReturnElementType|Conformance' -count=1`
Expected: FAIL (`swap_remove` é global desconhecido; `pop(ref xs, 0)` dá `pop expects 1 arguments, got 2`).

- [ ] **Step 3: Implementar**

Em `internal/compiler/builtin_calls.go`:

Linha 46, trocar o filtro por:

```go
	switch name {
	case "append", "pop", "delete", "json_loads", "range", "call_result", "swap_remove":
	default:
		return false, nil, nil
	}
```

Linhas 62-75 (aridade), trocar o `if name == "range" {...} else if wantArity := ...` por:

```go
	switch {
	case name == "range":
		if len(call.Arguments) < 1 || len(call.Arguments) > 3 {
			return true, nil, fmt.Errorf(
				"[line %d] range expects 1 to 3 arguments, got %d",
				c.currentLine, len(call.Arguments),
			)
		}
	case name == "pop":
		// Issue #126 item 4: indice opcional (Python list.pop([i])).
		if len(call.Arguments) < 1 || len(call.Arguments) > 2 {
			return true, nil, fmt.Errorf(
				"[line %d] pop expects 1 or 2 arguments, got %d",
				c.currentLine, len(call.Arguments),
			)
		}
	default:
		if wantArity := map[string]int{"append": 2, "delete": 2, "json_loads": 2, "swap_remove": 2}[name]; len(call.Arguments) != wantArity {
			return true, nil, fmt.Errorf(
				"[line %d] %s expects %d arguments, got %d",
				c.currentLine, name, wantArity, len(call.Arguments),
			)
		}
	}
```

Substituir o `case "pop":` (linhas ~125-134) por:

```go
	case "pop", "swap_remove":
		// Issue #126 item 4: pop(ref xs[, i]) e swap_remove(ref xs, i)
		// devolvem o elemento (Python list.pop, Rust Vec::swap_remove).
		// Mesmo contrato de ref no arg 1; arg 2, quando presente, e um int
		// por valor.
		container, err := c.compileBuiltinRefArgument(call.Arguments[0], "argument 1 to '"+name+"'", "ref T[]")
		if err != nil {
			return true, nil, err
		}
		array, ok := container.(*ast.ArrayType)
		if !ok {
			return true, nil, fmt.Errorf("[line %d] %s expects an array, got %s", c.currentLine, name, noxyTypeName(container))
		}
		if len(call.Arguments) == 2 {
			index, err := c.compileBuiltinValueArgument(call.Arguments[1])
			if err != nil {
				return true, nil, err
			}
			intType := &ast.PrimitiveType{Name: "int"}
			if _, explicitRef := asRefType(index); explicitRef {
				return true, nil, fmt.Errorf(
					"[line %d] argument 2 to '%s': expected int, got %s%s",
					c.currentLine, name, noxyTypeName(index), c.derefReadHint(intType, index, call.Arguments[1]),
				)
			}
			if !c.areStrictTypesCompatible(intType, index) {
				return true, nil, fmt.Errorf(
					"[line %d] argument 2 to '%s': expected int, got %s",
					c.currentLine, name, noxyTypeName(index),
				)
			}
		}
		c.emitCall(len(call.Arguments), emission, false)
		return true, array.ElementType, nil
```

Em `internal/compiler/narrowing.go`, na inicialização de `pureBuiltins`, trocar `"append": {}, "pop": {}, "delete": {},` por `"append": {}, "pop": {}, "delete": {}, "swap_remove": {},` e ajustar o comentário acima (`append/pop/delete/swap_remove`). Atualizar também o comentário de `compileBuiltinRefArgument` (`builtin_calls.go:21-23`) para citar `swap_remove`.

- [ ] **Step 4: Rodar e ver passar**

Run: `go test ./internal/compiler ./internal/vm -run 'Pop|SwapRemove|MutatingBuiltin|NativeSignatures|Narrowing|Conformance|Cow' -count=1`
Expected: PASS, inclusive os testes de CoW e ponta a ponta do Step 1.

- [ ] **Step 5: Varredura do corpus por `pop` em array possivelmente vazio**

`pop(ref xs)` em array vazio agora erra. Antes de rodar o runner, revisar cada uso: `grep -rn 'pop(ref' noxy_examples tests internal/stdlib noxy_libs --include='*.nx'`. Usos a olhar com atenção: `noxy_examples/my_stack.nx:14,21` (protegido por `length(...) >= 1`?), `brainfuck.nx:44`, `test_generics_basics.nx:36`, `language_semantics_test.nx:629`, `KandR_in_noxy/calc_stack.nx`. Um uso que dependia de `null` (testava `!= null` ou usava o `null` como "vazio") passa a checar `length(xs) > 0` antes; listar cada arquivo alterado no relatório e no CHANGELOG (Task 10).

Run: `go run ./cmd/noxy noxy_examples/run_all_tests_concurrent.nx`
Expected: exit 0.

- [ ] **Step 6: Exemplo no corpus**

Criar `noxy_examples/test_pop_index.nx` (adicionar `use sys select exit` no topo, como em `language_semantics_test.nx:19`; `!` é a negação booleana):

```noxy
// test_pop_index.nx — pop com índice opcional e swap_remove (issue #126 item 4)
use sys select exit

func assert(cond: bool, name: string) -> void
    if cond then
        print("PASS: " + name)
    else
        print("FAIL: " + name)
        exit(1)
    end
end

let xs: int[] = [10, 20, 30, 40]
let removed: int = pop(ref xs, 1)
assert(removed == 20, "pop(ref xs, i) devolve o elemento")
assert(length(xs) == 3, "pop(ref xs, i) encolhe o array")
assert(xs[0] == 10 && xs[1] == 30 && xs[2] == 40, "pop(ref xs, i) preserva a ordem")

let last: int = pop(ref xs)
assert(last == 40 && length(xs) == 2, "pop(ref xs) tira o ultimo")

let swapped: int = swap_remove(ref xs, 0)
assert(swapped == 10, "swap_remove devolve o elemento")
assert(length(xs) == 1 && xs[0] == 30, "swap_remove traz o ultimo para o buraco")

// Valor semantico: a copia feita antes nao muda (CoW)
let ys: int[] = [1, 2, 3]
let copia: int[] = ys
let _r: int = pop(ref ys, 0)
assert(length(copia) == 3, "copia anterior nao ve a remocao")
assert(length(ys) == 2, "original encolheu")

// Loop de jogo: remover mortos no meio, do fim para o inicio
struct Bala
    x: int
    viva: bool
end
let balas: Bala[] = [Bala(1, true), Bala(2, false), Bala(3, true), Bala(4, false)]
let i: int = length(balas) - 1
while i >= 0 do
    if !balas[i].viva then
        let _b: Bala = swap_remove(ref balas, i)
    end
    i = i - 1
end
assert(length(balas) == 2, "so as vivas ficam")
assert(balas[0].viva && balas[1].viva, "todas vivas")

print("test_pop_index: OK")
```

Run: `go run ./cmd/noxy noxy_examples/test_pop_index.nx`
Expected: linhas `PASS: ...` e `test_pop_index: OK`, exit 0.

- [ ] **Step 7: Commit**

```bash
git add internal/compiler/builtin_calls.go internal/compiler/builtin_calls_test.go internal/compiler/narrowing.go internal/compiler/function_conformance_examples_test.go internal/vm/native_signatures_test.go internal/vm/cow_builtins_test.go noxy_examples/type_errors/swap_remove_without_ref.nx noxy_examples/test_pop_index.nx
git commit -m "feat(compiler): pop(ref xs[, i]) e swap_remove tipados — ref T[] + int, devolvem T (issue #126)"
```

(incluir no `git add` os `.nx` do corpus ajustados no Step 5.)

---

### Task 6: natives `math_*`

**Files:**
- Create: `internal/vm/builtins_math.go`
- Modify: `internal/vm/builtins.go:35-48` (chamar `vm.defineMathBuiltins()`)
- Modify: `internal/vm/architecture_test.go:60-73` (`"builtins_math.go": {"defineMathBuiltins"}`)
- Modify: `internal/vm/builtins_registry_test.go` (snapshot)
- Test: `internal/vm/builtins_math_test.go`

**Interfaces:**
- Produces: natives `math_sqrt`, `math_cbrt`, `math_abs`, `math_floor`, `math_ceil`, `math_round`, `math_trunc`, `math_sin`, `math_cos`, `math_tan`, `math_asin`, `math_acos`, `math_atan`, `math_exp`, `math_log`, `math_log2`, `math_log10` (1 arg), `math_pow`, `math_fmod`, `math_atan2`, `math_hypot`, `math_min`, `math_max` (2 args), `math_clamp` (3 args). Todos `float -> float`, erro `math.<nome>: domain error (<condição>), got <valores>` fora do domínio, `math.<nome>: <rótulo> must be a float, got <tipo>` para tipo errado.

- [ ] **Step 1: Teste que falha**

Criar `internal/vm/builtins_math_test.go`:

```go
package vm

import (
	"math"
	"strings"
	"testing"

	"noxy-vm/internal/value"
)

// Issue #126 item 1: wrappers finos sobre o math do Go. Dominio invalido e
// erro tipado (como `1.0 / 0.0` e a #121), nunca NaN; overflow para Inf nao
// e checado (como o overflow de int, spec §8).
func TestMathBuiltinsScalarTables(t *testing.T) {
	machine := New()
	f := value.NewFloat
	tests := []struct {
		name    string
		builtin string
		args    []value.Value
		want    float64
	}{
		{"sqrt", "math_sqrt", []value.Value{f(2)}, math.Sqrt2},
		{"sqrt zero", "math_sqrt", []value.Value{f(0)}, 0},
		{"cbrt", "math_cbrt", []value.Value{f(-27)}, -3},
		{"abs", "math_abs", []value.Value{f(-2.5)}, 2.5},
		{"floor", "math_floor", []value.Value{f(-2.5)}, -3},
		{"ceil", "math_ceil", []value.Value{f(-2.5)}, -2},
		{"round half away from zero", "math_round", []value.Value{f(2.5)}, 3},
		{"round negative half away from zero", "math_round", []value.Value{f(-2.5)}, -3},
		{"trunc", "math_trunc", []value.Value{f(-2.7)}, -2},
		{"sin", "math_sin", []value.Value{f(math.Pi / 2)}, 1},
		{"cos", "math_cos", []value.Value{f(0)}, 1},
		{"tan", "math_tan", []value.Value{f(0)}, 0},
		{"asin", "math_asin", []value.Value{f(1)}, math.Pi / 2},
		{"acos", "math_acos", []value.Value{f(1)}, 0},
		{"atan", "math_atan", []value.Value{f(1)}, math.Pi / 4},
		{"atan2", "math_atan2", []value.Value{f(1), f(1)}, math.Pi / 4},
		{"atan2 quadrant", "math_atan2", []value.Value{f(1), f(-1)}, 3 * math.Pi / 4},
		{"hypot", "math_hypot", []value.Value{f(3), f(4)}, 5},
		{"exp", "math_exp", []value.Value{f(1)}, math.E},
		{"log", "math_log", []value.Value{f(math.E)}, 1},
		{"log2", "math_log2", []value.Value{f(8)}, 3},
		{"log10", "math_log10", []value.Value{f(1000)}, 3},
		{"pow", "math_pow", []value.Value{f(2), f(10)}, 1024},
		{"pow negative base integer exponent", "math_pow", []value.Value{f(-2), f(3)}, -8},
		{"pow overflow is Inf", "math_pow", []value.Value{f(10), f(400)}, math.Inf(1)},
		{"fmod keeps sign of x", "math_fmod", []value.Value{f(-7), f(3)}, -1},
		{"min", "math_min", []value.Value{f(1), f(-1)}, -1},
		{"max", "math_max", []value.Value{f(1), f(-1)}, 1},
		{"clamp below", "math_clamp", []value.Value{f(-5), f(0), f(10)}, 0},
		{"clamp inside", "math_clamp", []value.Value{f(5), f(0), f(10)}, 5},
		{"clamp above", "math_clamp", []value.Value{f(50), f(0), f(10)}, 10},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := callBuiltin(t, machine, tc.builtin, tc.args...)
			if got.Type != value.VAL_FLOAT {
				t.Fatalf("%s returned %s, want float", tc.builtin, runtimeTypeName(got))
			}
			if math.IsInf(tc.want, 0) {
				if !math.IsInf(got.Float(), 1) {
					t.Fatalf("%s = %v, want +Inf", tc.builtin, got.Float())
				}
				return
			}
			if math.Abs(got.Float()-tc.want) > 1e-12 {
				t.Fatalf("%s = %v, want %v", tc.builtin, got.Float(), tc.want)
			}
		})
	}
}

func TestMathBuiltinsRejectInvalidDomainAndArguments(t *testing.T) {
	machine := New()
	f := value.NewFloat
	cases := []struct {
		builtin string
		args    []value.Value
		want    string
	}{
		{"math_sqrt", []value.Value{f(-1)}, "math.sqrt: domain error (x < 0), got x=-1"},
		{"math_log", []value.Value{f(0)}, "math.log: domain error (x <= 0), got x=0"},
		{"math_log2", []value.Value{f(-3)}, "math.log2: domain error (x <= 0)"},
		{"math_log10", []value.Value{f(0)}, "math.log10: domain error (x <= 0)"},
		{"math_asin", []value.Value{f(2)}, "math.asin: domain error (x outside [-1, 1]), got x=2"},
		{"math_acos", []value.Value{f(-1.5)}, "math.acos: domain error (x outside [-1, 1])"},
		{"math_fmod", []value.Value{f(1), f(0)}, "math.fmod: domain error (y == 0), got x=1, y=0"},
		{"math_pow", []value.Value{f(0), f(-1)}, "math.pow: domain error (x == 0 and y < 0), got x=0, y=-1"},
		{"math_pow", []value.Value{f(-8), f(0.5)}, "math.pow: domain error (x < 0 and y not an integer), got x=-8, y=0.5"},
		{"math_clamp", []value.Value{f(1), f(10), f(0)}, "math.clamp: domain error (lo > hi), got lo=10, hi=0"},
		{"math_sqrt", []value.Value{value.NewInt(4)}, "math.sqrt: x must be a float, got int"},
		{"math_atan2", []value.Value{f(1), value.NewString("x")}, "math.atan2: x must be a float, got string"},
		{"math_sqrt", []value.Value{}, "math.sqrt: expects exactly 1 argument, got 0"},
		{"math_pow", []value.Value{f(1)}, "math.pow: expects exactly 2 arguments, got 1"},
	}
	for _, tc := range cases {
		t.Run(tc.want, func(t *testing.T) {
			_, err := requireBuiltin(t, machine, tc.builtin).Invoke(machine, tc.args)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("%s%v: err = %v, want %q", tc.builtin, tc.args, err, tc.want)
			}
		})
	}
}
```

- [ ] **Step 2: Rodar e ver falhar**

Run: `go test ./internal/vm -run 'TestMathBuiltins' -count=1`
Expected: FAIL com `builtin "math_sqrt" is not registered`.

- [ ] **Step 3: Implementar**

Criar `internal/vm/builtins_math.go`:

```go
package vm

import (
	"fmt"
	"math"

	"noxy-vm/internal/value"
)

// Issue #126 item 1: modulo `math` da stdlib — wrappers finos sobre o math
// do Go, expostos pelo wrapper tipado internal/stdlib/math.nx (float ->
// float). Dominio invalido e ERRO tipado, nao NaN: o proprio Noxy faz
// `1.0 / 0.0` ser erro de runtime (OP_DIV_FLOAT), a #121 decidiu que
// argumento invalido nunca vira sentinela, e Python `math` lanca
// `ValueError: math domain error`. Overflow para ±Inf (exp(1000.0)) nao e
// checado, como o overflow de int (spec §8).

func mathArity(native string, args []value.Value, want int) error {
	if len(args) == want {
		return nil
	}
	plural := "s"
	if want == 1 {
		plural = ""
	}
	return fmt.Errorf("%s: expects exactly %d argument%s, got %d", native, want, plural, len(args))
}

func mathFloatArgument(native, label string, arg value.Value) (float64, error) {
	if arg.Type != value.VAL_FLOAT {
		return 0, fmt.Errorf("%s: %s must be a float, got %s", native, label, runtimeTypeName(arg))
	}
	return arg.Float(), nil
}

func mathDomainError(native, condition, got string) error {
	return fmt.Errorf("%s: domain error (%s), got %s", native, condition, got)
}

func (vm *VM) defineMathBuiltins() {
	unary := func(name string, fn func(float64) float64, check func(float64) error) {
		native := "math." + name
		vm.DefineContextualNative("math_"+name, func(_ value.NativeContext, args []value.Value) (value.Value, error) {
			if err := mathArity(native, args, 1); err != nil {
				return value.NewNull(), err
			}
			x, err := mathFloatArgument(native, "x", args[0])
			if err != nil {
				return value.NewNull(), err
			}
			if check != nil {
				if err := check(x); err != nil {
					return value.NewNull(), err
				}
			}
			return value.NewFloat(fn(x)), nil
		})
	}
	binary := func(name, aLabel, bLabel string, fn func(float64, float64) float64, check func(float64, float64) error) {
		native := "math." + name
		vm.DefineContextualNative("math_"+name, func(_ value.NativeContext, args []value.Value) (value.Value, error) {
			if err := mathArity(native, args, 2); err != nil {
				return value.NewNull(), err
			}
			a, err := mathFloatArgument(native, aLabel, args[0])
			if err != nil {
				return value.NewNull(), err
			}
			b, err := mathFloatArgument(native, bLabel, args[1])
			if err != nil {
				return value.NewNull(), err
			}
			if check != nil {
				if err := check(a, b); err != nil {
					return value.NewNull(), err
				}
			}
			return value.NewFloat(fn(a, b)), nil
		})
	}
	nonNegative := func(native string) func(float64) error {
		return func(x float64) error {
			if x < 0 {
				return mathDomainError(native, "x < 0", fmt.Sprintf("x=%g", x))
			}
			return nil
		}
	}
	positive := func(native string) func(float64) error {
		return func(x float64) error {
			if x <= 0 {
				return mathDomainError(native, "x <= 0", fmt.Sprintf("x=%g", x))
			}
			return nil
		}
	}
	unitInterval := func(native string) func(float64) error {
		return func(x float64) error {
			if x < -1 || x > 1 {
				return mathDomainError(native, "x outside [-1, 1]", fmt.Sprintf("x=%g", x))
			}
			return nil
		}
	}

	unary("sqrt", math.Sqrt, nonNegative("math.sqrt"))
	unary("cbrt", math.Cbrt, nil)
	unary("abs", math.Abs, nil)
	unary("floor", math.Floor, nil)
	unary("ceil", math.Ceil, nil)
	unary("round", math.Round, nil) // metade afasta de zero, como Go (nao banker's)
	unary("trunc", math.Trunc, nil)
	unary("sin", math.Sin, nil)
	unary("cos", math.Cos, nil)
	unary("tan", math.Tan, nil)
	unary("asin", math.Asin, unitInterval("math.asin"))
	unary("acos", math.Acos, unitInterval("math.acos"))
	unary("atan", math.Atan, nil)
	unary("exp", math.Exp, nil)
	unary("log", math.Log, positive("math.log"))
	unary("log2", math.Log2, positive("math.log2"))
	unary("log10", math.Log10, positive("math.log10"))

	binary("pow", "x", "y", math.Pow, func(x, y float64) error {
		if x == 0 && y < 0 {
			return mathDomainError("math.pow", "x == 0 and y < 0", fmt.Sprintf("x=%g, y=%g", x, y))
		}
		if x < 0 && y != math.Trunc(y) {
			return mathDomainError("math.pow", "x < 0 and y not an integer", fmt.Sprintf("x=%g, y=%g", x, y))
		}
		return nil
	})
	binary("fmod", "x", "y", math.Mod, func(x, y float64) error {
		if y == 0 {
			return mathDomainError("math.fmod", "y == 0", fmt.Sprintf("x=%g, y=%g", x, y))
		}
		return nil
	})
	binary("atan2", "y", "x", math.Atan2, nil)
	binary("hypot", "x", "y", math.Hypot, nil)
	binary("min", "a", "b", math.Min, nil)
	binary("max", "a", "b", math.Max, nil)

	vm.DefineContextualNative("math_clamp", func(_ value.NativeContext, args []value.Value) (value.Value, error) {
		const native = "math.clamp"
		if err := mathArity(native, args, 3); err != nil {
			return value.NewNull(), err
		}
		x, err := mathFloatArgument(native, "x", args[0])
		if err != nil {
			return value.NewNull(), err
		}
		lo, err := mathFloatArgument(native, "lo", args[1])
		if err != nil {
			return value.NewNull(), err
		}
		hi, err := mathFloatArgument(native, "hi", args[2])
		if err != nil {
			return value.NewNull(), err
		}
		if lo > hi {
			return value.NewNull(), mathDomainError(native, "lo > hi", fmt.Sprintf("lo=%g, hi=%g", lo, hi))
		}
		return value.NewFloat(math.Min(math.Max(x, lo), hi)), nil
	})

	// clamp_int e o unico `_int` com native: os demais (abs_int, min_int,
	// max_int) sao Noxy puro no wrapper, mas codigo Noxy nao tem como
	// levantar erro de runtime, e `lo > hi` tem de errar como no clamp float.
	vm.DefineContextualNative("math_clamp_int", func(_ value.NativeContext, args []value.Value) (value.Value, error) {
		const native = "math.clamp_int"
		if err := mathArity(native, args, 3); err != nil {
			return value.NewNull(), err
		}
		for i, label := range []string{"x", "lo", "hi"} {
			if args[i].Type != value.VAL_INT {
				return value.NewNull(), fmt.Errorf("%s: %s must be an int, got %s", native, label, runtimeTypeName(args[i]))
			}
		}
		x, lo, hi := args[0].Int(), args[1].Int(), args[2].Int()
		if lo > hi {
			return value.NewNull(), mathDomainError(native, "lo > hi", fmt.Sprintf("lo=%d, hi=%d", lo, hi))
		}
		if x < lo {
			return value.NewInt(lo), nil
		}
		if x > hi {
			return value.NewInt(hi), nil
		}
		return value.NewInt(x), nil
	})
}
```

Acrescentar aos testes da Task 6 (na tabela de `TestMathBuiltinsRejectInvalidDomainAndArguments`):

```go
		{"math_clamp_int", []value.Value{value.NewInt(1), value.NewInt(10), value.NewInt(0)}, "math.clamp_int: domain error (lo > hi), got lo=10, hi=0"},
		{"math_clamp_int", []value.Value{f(1), value.NewInt(0), value.NewInt(10)}, "math.clamp_int: x must be an int, got float"},
```

e um teste positivo separado:

```go
func TestMathClampIntBuiltin(t *testing.T) {
	machine := New()
	for _, tc := range []struct{ x, lo, hi, want int64 }{{-5, 0, 10, 0}, {5, 0, 10, 5}, {50, 0, 10, 10}} {
		got := callBuiltin(t, machine, "math_clamp_int", value.NewInt(tc.x), value.NewInt(tc.lo), value.NewInt(tc.hi))
		if got.Type != value.VAL_INT || got.Int() != tc.want {
			t.Fatalf("clamp_int(%d, %d, %d) = %v, want %d", tc.x, tc.lo, tc.hi, got, tc.want)
		}
	}
}
```

No snapshot do registry, `"math_clamp_int"` entra logo após `"math_clamp"`.

Em `internal/vm/builtins.go`, adicionar `vm.defineMathBuiltins()` após `vm.defineStringBuiltins()`.

Em `internal/vm/architecture_test.go`, adicionar `"builtins_math.go": {"defineMathBuiltins"},` no map de `TestBuiltinSourceLayout`.

Em `internal/vm/builtins_registry_test.go`, inserir em ordem (entre `"make_wg"` e `"net_accept"`): `"math_abs", "math_acos", "math_asin", "math_atan", "math_atan2", "math_cbrt", "math_ceil", "math_clamp", "math_cos", "math_exp", "math_floor", "math_fmod", "math_hypot", "math_log", "math_log10", "math_log2", "math_max", "math_min", "math_pow", "math_round", "math_sin", "math_sqrt", "math_tan", "math_trunc",` — o teste ordena e compara; se a ordem lexicográfica estiver errada ele imprime a esperada.

- [ ] **Step 4: Rodar e ver passar**

Run: `go test ./internal/vm -run 'TestMathBuiltins|TestBuiltinRegistrySnapshot|TestBuiltinSourceLayout|TestEveryNativeIsRegisteredExactlyOnce' -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/vm/builtins_math.go internal/vm/builtins_math_test.go internal/vm/builtins.go internal/vm/architecture_test.go internal/vm/builtins_registry_test.go
git commit -m "feat(vm): natives math_* — sqrt, trig, atan2, floor/round, pow, fmod, min/max/clamp; domínio inválido é erro tipado (issue #126)"
```

---

### Task 7: wrapper `math.nx`, exemplo e desambiguação do `noxy_examples/math.nx`

**Files:**
- Create: `internal/stdlib/math.nx`
- Rename: `noxy_examples/math.nx` → `noxy_examples/math_module_example.nx`
- Modify: `noxy_examples/test_import.nx:4`, `noxy_examples/test_import_all.nx:4`
- Create: `noxy_examples/test_math_stdlib.nx`
- Test: `internal/vm/builtins_math_test.go` (ponta a ponta), `internal/vm/stdlib_hygiene_test.go` (já existe; passa a cobrir `math.nx`)

**Interfaces:**
- Consumes: natives `math_*` da Task 6.
- Produces: módulo `math` com `sqrt`…`clamp` (`float`), `abs_int`, `min_int`, `max_int`, `clamp_int` (`int`), `PI`, `E`.

- [ ] **Step 1: Testes ponta a ponta que falham**

Adicionar em `internal/vm/builtins_math_test.go`:

```go
func TestMathModuleViaSelectIsTyped(t *testing.T) {
	got := captureVMSource(t, `use math select sqrt, atan2, floor, PI, abs_int, clamp_int, hypot
let d: float = hypot(3.0, 4.0)
let ang: float = atan2(1.0, 1.0) * 4.0
let f: float = floor(2.7)
let n: int = abs_int(-3) + clamp_int(50, 0, 10)
test_report(f"{d} {ang == PI} {f} {n} {sqrt(16.0)}")
`)
	if s, _ := got.Obj.(string); s != "5.000000 true 2.000000 13 4.000000" {
		t.Fatalf("got %q", s)
	}
}

func TestMathModuleSelectRejectsIntArgumentAtCompileTime(t *testing.T) {
	err := interpretOrCompileErr(t, New(), "use math select sqrt\nlet x: float = sqrt(4)\n")
	if err == nil || !strings.Contains(err.Error(), "expected float, got int") {
		t.Fatalf("err = %v, want compile-time float/int mismatch", err)
	}
}

func TestMathModuleDomainErrorSurfacesAsRuntimeError(t *testing.T) {
	err := interpretOrCompileErr(t, New(), "use math select sqrt\nlet x: float = sqrt(-1.0)\n")
	if err == nil || !strings.Contains(err.Error(), "math.sqrt: domain error (x < 0)") {
		t.Fatalf("err = %v, want domain error", err)
	}
}
```

(Formato de float em `to_str`/f-string é `%f` — `5.000000`; se `captureVMSource` não resolver `use math` porque o teste roda sem `RootPath`, usar `runModuleProgram(t, t.TempDir(), src)` de `module_exports_test.go`, que resolve a stdlib embutida como último candidato.)

- [ ] **Step 2: Rodar e ver falhar**

Run: `go test ./internal/vm -run 'TestMathModule' -count=1`
Expected: FAIL com `failed to import module 'math'` ou `module not found`.

- [ ] **Step 3: Escrever o wrapper**

Criar `internal/stdlib/math.nx`:

```noxy
// math.nx - funções matemáticas sobre float (wrappers tipados dos natives
// math_*). Ângulos em radianos. Domínio inválido (sqrt(-1.0), log(0.0),
// asin(2.0), fmod(x, 0.0), pow(0.0, -1.0), pow(-8.0, 0.5)) é erro de runtime,
// nunca NaN; overflow para Inf não é checado. round afasta de zero na metade
// (round(2.5) = 3.0, round(-2.5) = -3.0), como o math.Round do Go.

let PI: float = 3.141592653589793
let E: float = 2.718281828459045

// Raízes e potências

func sqrt(x: float) -> float
    return math_sqrt(x)
end

func cbrt(x: float) -> float
    return math_cbrt(x)
end

func pow(x: float, y: float) -> float
    return math_pow(x, y)
end

func hypot(x: float, y: float) -> float
    return math_hypot(x, y)
end

func exp(x: float) -> float
    return math_exp(x)
end

func log(x: float) -> float
    return math_log(x)
end

func log2(x: float) -> float
    return math_log2(x)
end

func log10(x: float) -> float
    return math_log10(x)
end

// Arredondamento e sinal

func abs(x: float) -> float
    return math_abs(x)
end

func floor(x: float) -> float
    return math_floor(x)
end

func ceil(x: float) -> float
    return math_ceil(x)
end

func round(x: float) -> float
    return math_round(x)
end

func trunc(x: float) -> float
    return math_trunc(x)
end

func fmod(x: float, y: float) -> float
    return math_fmod(x, y)
end

// Trigonometria (radianos)

func sin(x: float) -> float
    return math_sin(x)
end

func cos(x: float) -> float
    return math_cos(x)
end

func tan(x: float) -> float
    return math_tan(x)
end

func asin(x: float) -> float
    return math_asin(x)
end

func acos(x: float) -> float
    return math_acos(x)
end

func atan(x: float) -> float
    return math_atan(x)
end

func atan2(y: float, x: float) -> float
    return math_atan2(y, x)
end

// Mínimo, máximo e clamp

func min(a: float, b: float) -> float
    return math_min(a, b)
end

func max(a: float, b: float) -> float
    return math_max(a, b)
end

func clamp(x: float, lo: float, hi: float) -> float
    return math_clamp(x, lo, hi)
end

// Versões inteiras (C# Math.Abs/Min/Max/Clamp, Rust i64::abs/min/max/clamp);
// sem sobrecarga na linguagem, o sufixo segue strconv.Itoa/FormatInt do Go.

func abs_int(x: int) -> int
    if x < 0 then
        return 0 - x
    end
    return x
end

func min_int(a: int, b: int) -> int
    if a < b then
        return a
    end
    return b
end

func max_int(a: int, b: int) -> int
    if a > b then
        return a
    end
    return b
end

// clamp_int e native porque codigo Noxy nao tem como levantar erro de runtime
// (spec §7: runtime error vem do runtime); o dominio lo > hi tem de errar
// como o clamp de float.
func clamp_int(x: int, lo: int, hi: int) -> int
    return math_clamp_int(x, lo, hi)
end
```

- [ ] **Step 4: Renomear o exemplo homônimo e ajustar os imports**

```bash
git mv noxy_examples/math.nx noxy_examples/math_module_example.nx
sed -i 's/^use math select add, multiply, factorial/use math_module_example select add, multiply, factorial/' noxy_examples/test_import.nx
sed -i 's/^use math select \*/use math_module_example select */' noxy_examples/test_import_all.nx
sed -i 's|// math.nx - Módulo de funções matemáticas|// math_module_example.nx - módulo de exemplo (renomeado: `math` agora é a stdlib, issue #126)|' noxy_examples/math_module_example.nx
grep -rn 'math' noxy_examples/test_import.nx noxy_examples/test_import_all.nx
```

Conferir se `math.nx` está na lista `exclusions` do runner (não está, pela listagem) e se algum outro `.nx`/doc referencia `noxy_examples/math.nx` (`grep -rn 'math\.nx' --include=*.nx --include=*.md . | grep -v superpowers`).

- [ ] **Step 5: Exemplo da stdlib**

Criar `noxy_examples/test_math_stdlib.nx`:

```noxy
// test_math_stdlib.nx — módulo math da stdlib (issue #126 item 1)
use math
use math select sqrt, PI

func assert(cond: bool, name: string) -> void
    if cond then
        print("PASS: " + name)
    else
        print("FAIL: " + name)
        exit(1)
    end
end

func near(a: float, b: float) -> bool
    return math.abs(a - b) < 0.000001
end

assert(near(sqrt(2.0) * sqrt(2.0), 2.0), "sqrt")
assert(near(math.hypot(3.0, 4.0), 5.0), "hypot")
assert(near(math.atan2(1.0, 1.0) * 4.0, PI), "atan2 e PI")
assert(near(math.sin(PI / 2.0), 1.0), "sin")
assert(near(math.cos(0.0), 1.0), "cos")
assert(math.floor(2.7) == 2.0 && math.ceil(2.1) == 3.0, "floor/ceil")
assert(math.round(2.5) == 3.0 && math.round(-2.5) == -3.0, "round afasta de zero")
assert(math.trunc(-2.7) == -2.0, "trunc")
assert(near(math.fmod(7.5, 2.0), 1.5), "fmod")
assert(math.pow(2.0, 10.0) == 1024.0, "pow")
assert(near(math.log(math.E), 1.0) && math.log2(8.0) == 3.0 && math.log10(1000.0) == 3.0, "log/log2/log10")
assert(math.min(1.0, -1.0) == -1.0 && math.max(1.0, -1.0) == 1.0, "min/max")
assert(math.clamp(50.0, 0.0, 10.0) == 10.0 && math.clamp(-5.0, 0.0, 10.0) == 0.0, "clamp")
assert(math.abs_int(-3) == 3 && math.min_int(2, 9) == 2 && math.max_int(2, 9) == 9 && math.clamp_int(50, 0, 10) == 10, "versões int")

// Leque de tiros: rotação de vetor por ângulo (o que o Deadrail pré-computava fora da linguagem)
let ang: float = 12.0 * PI / 180.0
let c: float = math.cos(ang)
let s: float = math.sin(ang)
let vx: float = 1.0
let vy: float = 0.0
let rx: float = vx * c - vy * s
let ry: float = vx * s + vy * c
assert(near(math.hypot(rx, ry), 1.0), "rotação preserva o módulo")
assert(near(math.atan2(ry, rx), ang), "atan2 recupera o ângulo")

print("test_math_stdlib: OK")
```

(adicionar `use sys select exit` no topo, como na Task 5.)

Run: `go run ./cmd/noxy noxy_examples/test_math_stdlib.nx && go run ./cmd/noxy noxy_examples/test_import.nx && go run ./cmd/noxy noxy_examples/test_import_all.nx`
Expected: os três com exit 0.

- [ ] **Step 6: Rodar e ver passar**

Run: `go test ./internal/vm -run 'TestMathModule|TestStdlib|TestEmbeddedStdlib' -count=1`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/stdlib/math.nx internal/vm/builtins_math_test.go noxy_examples/math_module_example.nx noxy_examples/test_import.nx noxy_examples/test_import_all.nx noxy_examples/test_math_stdlib.nx
git commit -m "feat(stdlib): módulo math — wrappers tipados, PI/E, versões _int; exemplo math.nx renomeado para não sombrear a stdlib (issue #126)"
```

---

### Task 8: `m.f(...)` e `m.x` tipados pelo namespace

**Files:**
- Create: `internal/compiler/namespace_member_types.go`
- Modify: `internal/compiler/compiler.go:1092-1101` (fim do `case *ast.MemberAccessExpression`)
- Modify: `internal/compiler/let_inference.go:25-33` (texto do erro)
- Test: `internal/compiler/namespace_member_typing_test.go` (novo)

**Interfaces:**
- Consumes: `importedBindingType(module, name)` (`module_exports.go:649`), `programViewType(t, origin)` (`member_types.go:63`), `isShadowedByLocal` (`generics_target.go:94`).
- Produces: `(c *Compiler) namespaceMemberType(access *ast.MemberAccessExpression) ast.NoxyType` — `nil` quando não é acesso a namespace ou quando o tipo não é nomeável.

- [ ] **Step 1: Testes que falham**

Criar `internal/compiler/namespace_member_typing_test.go`:

```go
package compiler

import "testing"

// Issue #126 item 2: `m.f(...)` e `m.x` pelo namespace carregam o tipo
// declarado pelo modulo (o mesmo que `select` registra), traduzido para a
// visao do programa pela regra da #58 item 1 (programViewType). Com isso a
// chamada por namespace ganha aridade, tipo de argumento e tipo de retorno
// — antes, tipo nil: nada conferido, `let v = m.f()` nao inferia.

const rollModule = `struct V
    x: float
    y: float
end
let total: int = 0
let limit = 10
func roll(n: int) -> int
    return n * 2
end
func norm(v: V) -> V
    return V(v.x, v.y)
end
func bump() -> void
    total = total + 1
end
`

func rollRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	writeModuleFile(t, root, "m.nx", rollModule)
	return root
}

func TestNamespaceCallInfersReturnType(t *testing.T) {
	err := compileSourceAtRoot(t, rollRoot(t), "use m\nlet v = m.roll(6)\nlet s: string = v\n")
	requireErrorMentions(t, err, "expected string, got int")
}

func TestNamespaceCallReturnTypeIsCheckedInAnnotatedLet(t *testing.T) {
	err := compileSourceAtRoot(t, rollRoot(t), "use m\nlet s: string = m.roll(6)\n")
	requireErrorMentions(t, err, "expected string, got int")
}

func TestNamespaceCallChecksArgumentTypes(t *testing.T) {
	err := compileSourceAtRoot(t, rollRoot(t), "use m\nlet v: int = m.roll(\"x\")\n")
	requireErrorMentions(t, err, "argument 1 to 'm.roll': expected int, got string")
}

func TestNamespaceCallChecksArity(t *testing.T) {
	err := compileSourceAtRoot(t, rollRoot(t), "use m\nlet v: int = m.roll(1, 2)\n")
	requireErrorMentions(t, err, "expects 1 arguments, got 2")
}

func TestNamespaceCallReturningModuleStructIsNamedByAlias(t *testing.T) {
	err := compileSourceAtRoot(t, rollRoot(t), "use m as vec\nlet p = vec.norm(vec.V(1.0, 2.0))\nlet s: string = p\n")
	requireErrorMentions(t, err, "expected string, got vec.V")
}

func TestNamespaceConstructorChecksFieldTypes(t *testing.T) {
	err := compileSourceAtRoot(t, rollRoot(t), "use m\nlet p: m.V = m.V(1, 2.0)\n")
	requireErrorMentions(t, err, "argument 1 to 'm.V': expected float, got int")
}

func TestNamespaceStructReturnPrefersSelectedName(t *testing.T) {
	err := compileSourceAtRoot(t, rollRoot(t), "use m\nuse m select V\nlet p = m.norm(V(1.0, 2.0))\nlet s: string = p\n")
	requireErrorMentions(t, err, "expected string, got V")
}

func TestNamespaceStructReturnUsesFirstDeclaredAliasButMatchesBoth(t *testing.T) {
	err := compileSourceAtRoot(t, rollRoot(t), "use m as a\nuse m as b\nlet p = b.norm(b.V(1.0, 2.0))\nlet q: b.V = p\nlet r: a.V = p\nlet s: string = p\n")
	requireErrorMentions(t, err, "expected string, got a.V")
}

func TestNamespaceModuleVariableIsTyped(t *testing.T) {
	err := compileSourceAtRoot(t, rollRoot(t), "use m\nlet n = m.total\nlet k = m.limit\nlet s: string = n\n")
	requireErrorMentions(t, err, "expected string, got int")
	err = compileSourceAtRoot(t, rollRoot(t), "use m\nlet s: string = m.limit\n")
	requireErrorMentions(t, err, "expected string, got int")
}

func TestNamespaceVoidCallCannotBeBound(t *testing.T) {
	err := compileSourceAtRoot(t, rollRoot(t), "use m\nlet v = m.bump()\n")
	requireErrorMentions(t, err, "cannot infer type for 'v'")
}

func TestNamespaceFunctionAsValueIsTyped(t *testing.T) {
	err := compileSourceAtRoot(t, rollRoot(t), "use m\nlet g = m.roll\nlet s: string = g(2)\n")
	requireErrorMentions(t, err, "expected string, got int")
}

func TestNamespaceMemberStaysDynamicWhenStructIsUnnameable(t *testing.T) {
	// `use m select roll` nao importa V; um segundo modulo que devolve V
	// pelo namespace so e nomeavel se `use m` existir. Sem isso, dinamico.
	root := t.TempDir()
	writeModuleFile(t, root, "m.nx", rollModule)
	writeModuleFile(t, root, "w.nx", "use m select V, norm\nfunc make() -> V\n    return norm(V(0.0, 0.0))\nend\n")
	err := compileSourceAtRoot(t, root, "use w\nlet s: string = w.make()\n")
	requireNoError(t, err)
	err = compileSourceAtRoot(t, root, "use w\nlet p = w.make()\n")
	requireErrorMentions(t, err, "cannot infer type for 'p'")
}

func TestLocalShadowingNamespaceAliasIsNotTyped(t *testing.T) {
	err := compileSourceAtRoot(t, rollRoot(t), `use m
struct Box
    roll: int
end
func f(m: Box) -> int
    let s: string = m.roll
    return 0
end
`)
	requireErrorMentions(t, err, "expected string, got int")
	// e o alias sombreado por um parametro sem campo `roll` continua
	// dinamico (tipo do struct local, nao do modulo):
	err = compileSourceAtRoot(t, rollRoot(t), `use m
struct Box
    v: int
end
func f(m: Box) -> int
    let s: string = m.v
    return 0
end
`)
	requireErrorMentions(t, err, "expected string, got int")
}

func TestNamespaceGenericTemplateStillRejected(t *testing.T) {
	root := t.TempDir()
	writeModuleFile(t, root, "g.nx", "func id<T>(x: T) -> T\n    return x\nend\n")
	err := compileSourceAtRoot(t, root, "use g\nlet v = g.id(1)\n")
	requireErrorMentions(t, err, "não é acessível via namespace")
}

func TestNamespaceCallInsideFunctionBodyIsTyped(t *testing.T) {
	err := compileSourceAtRoot(t, rollRoot(t), "use m\nfunc f() -> string\n    let v = m.roll(1)\n    return v\nend\n")
	requireErrorMentions(t, err, "expected string, got int")
}
```

Texto de aridade: `function '%s' expects %d arguments, got %d` (`compiler.go:2611`), logo o teste de aridade acima deve esperar `function 'm.roll' expects 1 arguments, got 2`. `callableName` (`function_types.go:100`) hoje devolve `expression.String()` para não-identificador, e `MemberAccessExpression.String()` é `(m.roll)` com parênteses — o Step 3 estende `callableName` para produzir `m.roll`.

- [ ] **Step 2: Rodar e ver falhar**

Run: `go test ./internal/compiler -run 'Namespace' -count=1`
Expected: FAIL na maioria (hoje `let v = m.roll(6)` dá `cannot infer`, `let s: string = m.roll(6)` compila).

- [ ] **Step 3: Implementar**

Criar `internal/compiler/namespace_member_types.go`:

```go
package compiler

import "noxy-vm/internal/ast"

// namespaceMemberType devolve o tipo estatico de `alias.member` quando alias
// e um `use m [as alias]` (forma de namespace) nao sombreado por local ou
// upvalue — ou nil (dinamico) em qualquer outro caso.
//
// Issue #126 item 2: ate aqui o membro de namespace nao carregava tipo
// (importNamespace registra o modulo como objeto opaco), entao `m.f(...)`
// nao conferia aridade, argumentos nem retorno, e `let v = m.f()` nao
// inferia — enquanto `use m select f` conhecia a assinatura. O tipo vem da
// MESMA fonte do select (importedBindingType: funcao, construtor de struct
// ou `let` de topo, ja com anotacoes resolvidas) e e traduzido para a visao
// do programa pela regra da #58 item 1 (programViewType): `V` escrito
// dentro de vec.nx vira `vec.V` (primeiro alias declarado) ou `V` (se ha
// `select V`); qualquer parte que o programa nao consegue nomear torna o
// tipo INTEIRO dinamico, nunca meio-tipado. Template generico nao tem tipo
// de valor (importedBindingType devolve ok=false) e continua recusado em
// compileCallExpression com o hint de `select`.
//
// O bytecode nao muda (OP_GET_PROPERTY no objeto modulo); so o tipo.
func (c *Compiler) namespaceMemberType(access *ast.MemberAccessExpression) ast.NoxyType {
	base, ok := access.Left.(*ast.Identifier)
	if !ok || c.isShadowedByLocal(base.Value) {
		return nil
	}
	module, isNamespace := c.namespaceImports[base.Value]
	if !isNamespace {
		return nil
	}
	declared, ok := c.importedBindingType(module, access.Member)
	if !ok || declared == nil {
		return nil
	}
	translated, ok := c.programViewType(declared, module)
	if !ok {
		return nil
	}
	return translated
}
```

Em `internal/compiler/compiler.go`, no `case *ast.MemberAccessExpression`, trocar

```go
		fieldType := c.memberType(leftType, n.Member)
		if key, ok := stableKey(n); ok {
```

por

```go
		fieldType := c.memberType(leftType, n.Member)
		if fieldType == nil && leftType == nil {
			// `m.x` / `m.f` com m namespace (issue #126 item 2).
			fieldType = c.namespaceMemberType(n)
		}
		if key, ok := stableKey(n); ok {
```

Em `internal/compiler/function_types.go:100-105`, `callableName` passa a nomear `alias.membro` sem os parênteses de `String()`:

```go
func callableName(expression ast.Expression) string {
	switch callee := expression.(type) {
	case *ast.Identifier:
		return callee.Value
	case *ast.MemberAccessExpression:
		// `m.roll(...)` (issue #126 item 2): "argument 1 to 'm.roll'", nao
		// "(m.roll)" como MemberAccessExpression.String() imprime.
		if base, ok := callee.Left.(*ast.Identifier); ok {
			return base.Value + "." + callee.Member
		}
	}
	return expression.String()
}
```

Em `internal/compiler/let_inference.go:25-33`, atualizar o texto do erro e o comentário: remover "a namespace member 'm.x'" da lista, ficando `"its type is not known here (a global declared later, a module member the program cannot name, or a builtin without a static return type)"`. Atualizar todo teste que fixa o texto antigo (`grep -rn "a namespace member 'm.x'" internal cmd docs`).

- [ ] **Step 4: Rodar e ver passar; depois a suíte do compilador e da VM inteira**

Run: `go test ./internal/compiler -run 'Namespace' -count=1`
Expected: PASS.

Run: `go test ./internal/compiler ./internal/vm ./cmd/... -count=1`
Expected: falhas SÓ nos testes que documentavam o comportamento antigo. Candidatos conhecidos, a atualizar (não afrouxar — trocar a expectativa pela nova regra):
- `internal/compiler/member_access_typing_test.go:152-158` `TestModuleFieldTypeUsesNamespaceAlias`: remover o comentário "(`m.f(...)` via namespace e chamada dinamica…)"; o programa continua válido como está (`let res: d.QueryResult = d.q()` bate por igualdade).
- Qualquer teste com `cannot infer` sobre `m.x`: vira inferência válida — reescrever para conferir o tipo inferido.
- `internal/vm/module_exports_test.go:615` `TestNamespaceAndSelectNameTheSameStruct`, `:663`: devem continuar verdes (tipos batem). Se um falhar com `expected X, got Y`, o caso é real: investigar se a tradução escolheu o nome certo antes de mexer no teste.
- `internal/vm/stdlib_nullable_contracts_test.go` e `CHANGELOG` 0.23.2: `let texto: bytes = crypto.aes256_gcm_decrypt(k, d)` passa a ser erro de compilação — se há teste fixando o comportamento antigo (null em silêncio), vira teste do erro `expected bytes, got bytes?`.

- [ ] **Step 5: Varredura do corpus**

```bash
go run ./cmd/noxy noxy_examples/run_all_tests_concurrent.nx
for f in tests/test_features/*.nx tests/test_errors/*.nx noxy_libs/**/*.nx; do go run ./cmd/noxy --disassembly "$f" >/dev/null 2>/tmp/claude-1000/-home-estevao-Documents-go-projects-noxy/c5c780cf-1484-4085-a8ce-f8d899ea1662/scratchpad/err.txt || { echo "== $f"; head -3 /tmp/claude-1000/-home-estevao-Documents-go-projects-noxy/c5c780cf-1484-4085-a8ce-f8d899ea1662/scratchpad/err.txt; }; done
```

(se `--disassembly` executa o programa, trocar por um modo que só compile — conferir `go run ./cmd/noxy --help`; senão compilar via um teste Go temporário no scratchpad que chame `compiler.NewWithStateAndRoot` sobre cada arquivo.) Todo erro novo do tipo `expected X, got Y` em código do corpus é ou (a) programa que estava errado e agora é pego — corrigir o `.nx` e listar no CHANGELOG, ou (b) tradução errada — bug a corrigir no compilador. Registrar cada caso na mensagem do commit.

- [ ] **Step 6: Commit**

```bash
git add internal/compiler/namespace_member_types.go internal/compiler/namespace_member_typing_test.go internal/compiler/compiler.go internal/compiler/let_inference.go internal/compiler/member_access_typing_test.go
git commit -m "feat(compiler): m.f() e m.x tipados pelo namespace — mesma assinatura do select, traduzida para a visão do programa (issue #126)"
```

(incluir no `git add` qualquer teste ou `.nx` ajustado no Step 4/5.)

---

### Task 9: revisão adversarial do item 2 e REPL

**Files:**
- Test: `internal/compiler/namespace_member_typing_test.go`, `internal/compiler/unknown_type_test.go:153` (`TestReplCarriesNamespaceImportsAcrossLines`)

- [ ] **Step 1: REPL carrega o tipo entre linhas**

Em `internal/compiler/namespace_member_typing_test.go`, adicionar (mesma montagem de `TestReplCarriesNamespaceImportsAcrossLines`, `unknown_type_test.go:153`; imports `lexer`, `parser`, `ast` como lá):

```go
func TestReplNamespaceCallIsTypedOnLaterLine(t *testing.T) {
	// REPL: cada linha e um compilador novo; `use m` numa linha e
	// `m.roll(1)` na seguinte tem de ver o tipo — namespaceImports e
	// namespaceOrder viajam em ModuleState.
	root := rollRoot(t)
	globals := make(map[string]ast.NoxyType)
	structs := make(map[string]*ast.StructStatement)
	var modules *ModuleState
	for _, line := range []string{
		"use m\n",
		"let v = m.roll(1)\n",
		"let s: string = v\n",
		"let p = m.norm(m.V(1.0, 2.0))\n",
		"let bad: string = p\n",
	} {
		program := parser.New(lexer.New(line)).ParseProgram()
		c := NewWithStateAndRoot(globals, structs, "REPL", root)
		c.SetModuleState(modules)
		_, _, err := c.Compile(program)
		modules = c.ModuleState()
		switch {
		case strings.HasPrefix(line, "let s"):
			requireErrorMentions(t, err, "expected string, got int")
		case strings.HasPrefix(line, "let bad"):
			requireErrorMentions(t, err, "expected string, got m.V")
		default:
			requireNoError(t, err)
		}
	}
}
```

- [ ] **Step 2: Rodar**

Run: `go test ./internal/compiler -run 'Repl' -count=1`
Expected: PASS (namespaceImports já viaja em `ModuleState`; se falhar, `SetModuleState` não copia `namespaceOrder` — corrigir em `compiler.go:286-320`).

- [ ] **Step 3: Revisão adversarial independente (memória do projeto: completude não se auto-valida)**

Despachar um agente SEM o contexto desta sessão com o prompt:

> Repo /home/estevao/Documents/go_projects/noxy, branch feat/issue-126-math-namespace-typing. O commit "feat(compiler): m.f() e m.x tipados pelo namespace" passou a dar tipo estático a `m.f(...)`/`m.x` via `namespaceMemberType` (internal/compiler/namespace_member_types.go), traduzindo tipos com `programViewType`. Sua tarefa é ACHAR UM CASO A MAIS em que isso produza tipo errado, nome que o programa não consegue escrever, erro de compilação num programa que era válido, ou falha de runtime nova. Escreva programas Noxy em um TempDir com módulos (`use m`, `use m as a`, `use m select ...`, módulo que reexporta struct de outro, `let` de módulo sem anotação, struct genérico, `ref` em parâmetro, `T?` de retorno, chamada dentro de closure/generic, alias sombreado por upvalue, `m.f` passado como valor para `func` tipado, encadeamento `m.f().campo`, REPL via ModuleState) e rode com `go run ./cmd/noxy` ou testes Go em internal/compiler. Não confirme que funciona: procure o que quebra. Reporte cada caso com programa mínimo e saída.

Cada caso achado vira teste em `namespace_member_typing_test.go` (correção + teste, ou teste de caracterização se ficar fora do escopo, com comentário dizendo qual é a saída errada de hoje).

- [ ] **Step 4: Commit**

```bash
git add internal/compiler/
git commit -m "test(compiler): REPL e casos da revisão adversarial do namespace tipado (issue #126)"
```

---

### Task 10: documentação — spec, CHANGELOG, README, site

**Files:**
- Modify: `docs/NOXY_LANGUAGE_SPEC.md` §1.2 (`:52`), §3 (`:724`), §8 (`:2055-2066`), §9 (`:2178-2183`), §10 (`:2271-2274`), §11 (após `:2477`), §12 (`:2511-2523` e nova seção antes de `### I/O`)
- Modify: `CHANGELOG.md` (topo), `README.md:130`, `docs/index.html:203,378-379`

- [ ] **Step 1: Spec §1.2**

Linha 52: `| Types | \`int\`, \`float\`, \`string\`, \`bool\`, \`void\`, \`bytes\`, \`any\`, \`ref\`, \`func\`, \`map\`, \`chan\` |` e, após a tabela, o parágrafo:

```markdown
`map` and `chan` are type constructors (`map[K, V]`, `chan T`), `any` is the
dynamic type (§2.1); all three are reserved everywhere, so they cannot name a
variable, a parameter or a module (`use src.map` → `'map' is a keyword and
cannot be used as a name`). `str` is not a keyword.
```

- [ ] **Step 2: Spec §3**

Substituir a linha 724 (`| a namespace member — ...`) por nada (remover) e acrescentar, após a tabela, antes de "The core builtins…":

```markdown
A member reached through a namespace import — `m.f(...)`, `m.x`, `m.T(...)`
after `use m` — has the type the module declared for it, translated to the
program's view exactly as a field of a module struct is (§11), so `let v =
m.roll(6)` binds `v: int` and `let p = vec.norm(v)` binds `p: vec.V`. Only a
member whose type the program cannot name (a struct of a third module that
was never imported) stays dynamic and needs an annotation.
```

- [ ] **Step 3: Spec §8**

Após o bloco de código de overflow (linha ~2066), adicionar:

```markdown
`%` is defined for `int` only; the float remainder, roots, powers, rounding
and trigonometry live in the `math` module (§12): `math.fmod(x, y)`,
`math.sqrt(x)`, `math.floor(x)`, `math.atan2(y, x)`.
```

- [ ] **Step 4: Spec §9**

Substituir o parágrafo "**Double quotes inside `{}`** close an `f"..."` string: …" e seu exemplo por:

```markdown
**Quotes inside `{}`.** An interpolated expression may contain string
literals delimited by the same quote as the f-string (the Python 3.12 rule,
PEP 701): the lexer tracks brace depth, so the inner quote opens a nested
literal instead of closing the f-string.

```noxy
let n: int = 7
print(f"n = {fmt("%03d", n)}")   // n = 007
print(f"{"a"}")                   // a
```

A `{` that is still open at the end of the line is reported where it starts:
`SyntaxError: unclosed brace in f-string` with the hint `every '{' that starts
an expression needs a matching '}'; write '{{' for a literal brace`.
```

Ajustar também a frase anterior "(see the quoting rule below): `f'{fmt("%10s", x)}'`" para "`f"{fmt("%10s", x)}"`".

- [ ] **Step 5: Spec §10 Collections**

Substituir a linha `- \`pop(ref arr)\`` por:

```markdown
- `pop(ref arr) -> T`, `pop(ref arr, i) -> T`: removes and returns the last
  element, or the element at index `i` (the rest shifts down, order
  preserved, O(n)) — Python's `list.pop([i])`. A position that does not
  exist is a **runtime error**: `pop from empty array` for `pop(ref arr)` on
  an empty array, `array index out of bounds` for an index outside
  `[0, length)`. Test `length(arr) > 0` first; `pop` never returns `null`.
- `swap_remove(ref arr, i) -> T`: removes and returns the element at `i` by
  moving the **last** element into its place (O(1), order not preserved) —
  what a game loop wants when order does not matter. Same range rule.
  Both go through the same copy-on-write path as `append`: a copy taken
  before the call does not see the removal. `delete` remains map-only.
```

- [ ] **Step 6: Spec §11**

Após a seção "Member access on values of module struct types" (antes de "### Unknown type names"):

```markdown
### Member access through a namespace

`m.f(...)`, `m.x` and `m.T(...)` after `use m` (or `use m as alias`) are
**statically typed** with the declaration the module exports — the same
signature `use m select f` binds — translated to the program's view by the
table above (`V` inside `vec.nx` reads as `vec.V`, or `V` after `select V`).
Consequently a namespace call is checked like any typed call: arity,
argument types (`argument 1 to 'm.roll': expected int, got string`) and
result type (`let s: string = m.roll(6)` is `expected string, got int`), and
`let v = m.roll(6)` infers `v: int`. A `T?` result is checked too: `let t:
bytes = crypto.aes256_gcm_decrypt(k, d)` is `expected bytes, got bytes?`. A
member whose type the program cannot name (a struct of a module it never
imported, an instance of a module's generic struct) stays dynamic. A generic
template is still not reachable through the namespace — import it with
`select`. The module object itself has no type: `m` alone is not a value.
```

- [ ] **Step 7: Spec §12**

Tabela (após a linha `| \`strings\` | ...`): `| \`math\` | Floating-point math: roots, powers, rounding, trigonometry, `min`/`max`/`clamp`, `PI`/`E` |`.

Nova seção antes de `### I/O (\`io\`)`:

```markdown
### Math (`math`)

Thin wrappers over the host's `math` library, all on `float`; angles are in
radians. `PI` and `E` are module bindings (`math.PI`, or `use math select
PI`).

| Function | Contract |
|----------|----------|
| `sqrt(x)`, `cbrt(x)` | square and cube root; `sqrt` needs `x >= 0` |
| `pow(x, y)` | `x` to the power `y`; errors for `x == 0 && y < 0` and for `x < 0` with a non-integer `y` |
| `abs(x)` | absolute value |
| `floor(x)`, `ceil(x)`, `round(x)`, `trunc(x)` | rounding, as `float`; `round` sends halves **away from zero** (`round(2.5) = 3.0`, `round(-2.5) = -3.0`), like Go — use `to_int` for an `int` |
| `fmod(x, y)` | remainder with the sign of `x` (`fmod(-7.0, 3.0) = -1.0`); `y == 0` is an error |
| `sin(x)`, `cos(x)`, `tan(x)`, `asin(x)`, `acos(x)`, `atan(x)` | trigonometry; `asin`/`acos` need `-1 <= x <= 1` |
| `atan2(y, x)` | angle of the vector `(x, y)` in `(-PI, PI]` — note the argument order, as in C |
| `hypot(x, y)` | `sqrt(x*x + y*y)` without intermediate overflow |
| `exp(x)`, `log(x)`, `log2(x)`, `log10(x)` | exponential and logarithms; `log*` need `x > 0` |
| `min(a, b)`, `max(a, b)`, `clamp(x, lo, hi)` | on `float`; `clamp` needs `lo <= hi` |
| `abs_int(x)`, `min_int(a, b)`, `max_int(a, b)`, `clamp_int(x, lo, hi)` | the same on `int` (no overloading in the language, hence the suffix) |

**Domain errors raise.** An argument outside the function's domain is a
runtime error, never `NaN` — the same rule as `1.0 / 0.0` (§8) and as
Python's `math`:

```text
Runtime error: native 'math_sqrt' failed: math.sqrt: domain error (x < 0), got x=-1
```

Overflow is not checked: `exp(1000.0)` and `pow(10.0, 400.0)` return `+Inf`,
as `int` arithmetic wraps (§8). Arguments must be `float`: `sqrt(4)` is a
compile-time error (`expected float, got int`); write `sqrt(4.0)` or
`sqrt(to_float(n))`.

```noxy
use math
let ang: float = math.atan2(dir.y, dir.x)     // radians → degrees: * 180.0 / math.PI
let d: float = math.hypot(dx, dy)
let speed: float = math.clamp(v, 0.0, MAX)
```
```

- [ ] **Step 8: CHANGELOG**

Inserir no topo de `CHANGELOG.md`, logo após `# Changelog`:

```markdown
## [Unreleased]

Issue #126 — achados do [Deadrail](https://github.com/estevaofon/deadrail)
(shooter em Noxy): sem `math`, sem tipo em `m.f()`, sem remoção por índice,
aspas dentro de `{}` fechavam a f-string, e `map`/`chan`/`any`/`str` reservados
fora do §1.2.

### Added
- **Módulo `math`** (§12): `sqrt`, `cbrt`, `pow`, `abs`, `floor`, `ceil`,
  `round`, `trunc`, `fmod`, `sin`, `cos`, `tan`, `asin`, `acos`, `atan`,
  `atan2`, `hypot`, `exp`, `log`, `log2`, `log10`, `min`, `max`, `clamp`
  (`float`), `abs_int`/`min_int`/`max_int`/`clamp_int` (`int`), `PI`, `E`.
  Domínio inválido (`sqrt(-1.0)`, `log(0.0)`, `fmod(x, 0.0)`, `pow(0.0,
  -1.0)`, `pow(-8.0, 0.5)`, `clamp` com `lo > hi`) é erro de runtime tipado
  (`math.sqrt: domain error (x < 0), got x=-1`), como `1.0 / 0.0` e a #121 —
  nunca NaN; overflow para `Inf` não é checado. `round` afasta de zero na
  metade (Go). Um `math.nx` local continua sombreando a stdlib (regra de
  resolução do `use`): `noxy_examples/math.nx` virou
  `math_module_example.nx` por isso.
- **`pop(ref arr, i)` e `swap_remove(ref arr, i) -> T`** (§10): remoção
  por índice devolvendo o elemento — `pop` ganha o índice opcional de
  `list.pop([i])` do Python (preserva a ordem); `swap_remove` é builtin
  próprio (Rust `Vec::swap_remove`), O(1), não preserva ordem. Mesmo funil
  de CoW do `append`. `delete` continua só de map, como em Go.
- **Aspas iguais ao delimitador dentro de `{}` de f-string** (§9, regra da
  PEP 701): `f"n = {fmt("%03d", n)}"` compila. O lexer conta a profundidade
  de chaves; `{{`/`}}` e `f'...'` seguem iguais. Chave de expressão aberta no
  fim da linha é `unclosed brace in f-string` com hint. Token ilegal do lexer
  (string não terminada) agora aparece como `SyntaxError: <razão>` em vez de
  `invalid syntax "..."`.
- **Keyword em posição de nome** (`let map: int`, `use src.map as map`) é
  **um** erro, `'map' is a keyword and cannot be used as a name` + hint, e o
  parser sincroniza no fim da linha em vez de cascatear `invalid syntax`.

### Changed (BREAKING)
- **`m.f(...)`, `m.x` e `m.T(...)` pelo namespace têm tipo estático** (§3,
  §11): a assinatura que o módulo declara — a mesma que `use m select f`
  registra — traduzida para a visão do programa (`V` de `vec.nx` vira
  `vec.V`; regra da #58). A chamada por namespace passa a conferir aridade,
  tipo de argumento e retorno, e `let v = m.roll(6)` infere.

  | Programa | Antes | Agora |
  |---|---|---|
  | `let v = m.roll(6)` | `cannot infer type for 'v'` | `v: int` |
  | `let s: string = m.roll(6)` (`roll -> int`) | compilava; falhava em runtime | `type mismatch … expected string, got int` |
  | `m.roll("x")`, `m.roll(1, 2)` | compilava; falhava em runtime | erro de argumento / aridade |
  | `let t: bytes = crypto.aes256_gcm_decrypt(k, d)` | compilava e recebia null em silêncio (0.23.2) | `expected bytes, got bytes?` |
  | `let n = counter.total` | `cannot infer` | `n: int` (leitura viva, como antes) |
  | membro cujo tipo o programa não sabe nomear | dinâmico | dinâmico (inalterado) |

  Migração: corrigir o tipo escrito (o erro diz qual é), ou `let t: bytes? =
  crypto.aes256_gcm_decrypt(k, d)` e testar `!= null` para os `T?` da stdlib
  (`time.parse*`, `sqlite.prepare`, `crypto.aes256_gcm_decrypt`). A classe
  "tipo desconhecido" da fronteira dinâmica (#122) fica reduzida a plugin,
  `any` e membro não nomeável.
- **`pop` em posição inexistente é erro de runtime** (§10): `pop(ref xs)`
  em array vazio devolvia `null` sob um retorno tipado `T` — o "null de
  aridade num builtin tipado" que a 0.23.2 deixou pendente. Agora é `pop
  from empty array`; índice fora de `[0, length)` é `array index out of
  bounds`, como indexar. Regra da #121: argumento inválido é erro, nunca
  null sentinela. Migração: `if length(xs) > 0 then let v = pop(ref xs)
  end` no lugar de testar o resultado contra `null`.
- **`str` deixa de ser palavra reservada**: era `TYPE_STR` em `token.go`
  sem nenhum uso no parser. `let str: string = ...` compila. `any`, `map` e
  `chan` entram na tabela §1.2, onde faltavam.
```

Se o repositório não usa `[Unreleased]` (Keep a Changelog aceita), trocar pelo próximo número com data ao fazer o release — deixar `[Unreleased]` neste PR.

- [ ] **Step 9: README e site**

`README.md:130`: `(io, net, http, sqlite, json, strings, math, crypto, time)`.
`docs/index.html:203`: `io, strings, math, time, sys, net, http, json, crypto, sqlite, rand and errors`.
`docs/index.html:378-379`: `// io, strings, math, time, sys, net, http,` / `// json, crypto, sqlite, rand, errors`.

- [ ] **Step 10: Conferir Liquid e commit**

`grep -n '{{' docs/NOXY_LANGUAGE_SPEC.md | sed -n 1,20p` — o novo texto de §9 contém `{{` fora de `raw`; conferir como o §9 existente lida (linha ~2158 já tem `{{x}}` num bloco de código: ver se há `<!-- {% raw %} -->` em volta e replicar).

```bash
git add docs/NOXY_LANGUAGE_SPEC.md CHANGELOG.md README.md docs/index.html
git commit -m "docs(spec,changelog): math, namespace tipado, pop com índice/swap_remove, aspas em {} de f-string, keywords do §1.2 (issue #126)"
```

---

### Task 11: verificação final

- [ ] **Step 1: Suíte completa**

```bash
go build ./... && go vet ./...
go test ./internal/... -count=1
go test ./cmd/... -count=1
(cd sdk/noxyplugin && go test ./... -count=1)
go run ./cmd/noxy noxy_examples/run_all_tests_concurrent.nx
```

Expected: tudo verde; o runner termina com exit 0 e inclui `test_math_stdlib.nx`, `test_pop_index.nx`, `test_import.nx`, `test_import_all.nx`.

- [ ] **Step 2: gofmt e EOL**

```bash
gofmt -l internal/ cmd/ | grep -v -f <(git ls-files --others --exclude-standard) ; git diff --numstat develop...HEAD | sort -rn | head
```

Expected: `gofmt -l` sem os arquivos tocados; nenhum arquivo com contagem igual ao seu total de linhas (reescrita por EOL).

- [ ] **Step 3: Reproduções da issue**

Rodar cada reprodução da issue #126 num arquivo do scratchpad e conferir a saída nova:

```bash
S=/tmp/claude-1000/-home-estevao-Documents-go-projects-noxy/c5c780cf-1484-4085-a8ce-f8d899ea1662/scratchpad
printf 'use math\nprint(math.sqrt(2.0))\nlet l = math.floor(2.5)\nprint(l)\n' > $S/r1.nx && go run ./cmd/noxy $S/r1.nx
printf 'func roll(n: int) -> int\n    return n * 2\nend\n' > $S/m.nx && printf 'use m\nlet v = m.roll(6)\nprint(v)\n' > $S/r2.nx && go run ./cmd/noxy $S/r2.nx
printf 'let n: int = 7\nprint(f"n = {fmt("%%03d", n)}")\n' > $S/r3.nx && go run ./cmd/noxy $S/r3.nx
printf 'let xs: int[] = [10, 20, 30]\nlet r: int = pop(ref xs, 1)\nprint(xs)\nprint(r)\nlet e: int[] = []\nlet z: int = pop(ref e)\n' > $S/r4.nx; go run ./cmd/noxy $S/r4.nx; echo "exit=$?"
printf 'use src.map as map\n' > $S/r5.nx; go run ./cmd/noxy $S/r5.nx; echo "exit=$?"
```

Expected: `1.414214` / `2.000000`; `12`; `n = 007`; `[10, 30]` / `20` seguido de `pop from empty array` com exit ≠ 0; um único `SyntaxError: 'map' is a keyword and cannot be used as a name`.

- [ ] **Step 4: Commit final (se algo mudou) e resumo para o PR**

Escrever o corpo do PR com: os cinco itens, a quebra do item 2 com a tabela do CHANGELOG, a lista de `.nx` do corpus que precisaram de correção (Task 8 Step 5), e o resultado da revisão adversarial (Task 9). Rodapé:

```
🤖 Generated with [Claude Code](https://claude.com/claude-code)

https://claude.ai/code/session_01JPXzv6ZwqzCjsDXhDzG86p
```

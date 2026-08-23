# VM Perf — Strings: fast path ASCII + `to_str(int)` (issue #66, item 2) — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Eliminar as alocações de `[]rune` em `substring`/`char_at`/`s[i]`/`slice` quando a string é ASCII e tirar o `fmt` do caminho de `to_str(int)`, sem mudar semântica, saída ou mensagens de erro — e medir o impacto (issue #66, item 2, etapa 1).

**Architecture:** Um helper `isASCII(s)` (varredura por byte, sem alocar) decide em cada site entre o ramo novo (índices em bytes, fatia de `string` compartilhada) e o ramo atual (`[]rune`), que fica intocado; bytes ≥ 0x80 — inclusive inválidos — nunca entram no ramo novo. `Value.String()` de `VAL_INT` passa a `strconv.FormatInt`; `to_str` de escalar pula `requireValidUTF8`. Nada toca `run()`, opcodes, compilador ou RC.

**Tech Stack:** Go 1.26, Windows PowerShell 5.1 com cópias BOM dos scripts de `benchmarks/` (pwsh 7 não está na máquina), `go build -gcflags=-m=2`, `noxy --cpuprofile`.

**Spec:** `docs/superpowers/specs/2026-08-22-vm-perf-issue-66-string-ascii-fastpath-design.md`

## Global Constraints

- Branch `perf/issue-66-string-ascii-fastpath`, worktree `.claude/worktrees/perf-issue-66-strings`, base `origin/develop` 73cf11a (v0.15.0). Um commit por task.
- **Semântica, saída e mensagens de erro idênticas** (ASCII e não-ASCII); corpus `noxy_examples/` sem falhas (`go run ./cmd/noxy noxy_examples/run_all_tests_concurrent.nx`); diff de saída base × head vazio (`compare_examples.ps1`).
- **RC intocado** (spec CoW-RC §4.2): nenhum `Retain`/`Release` muda. **Nenhum opcode novo** (se surgir necessidade, só por append ao fim de `internal/chunk/chunk.go`).
- Guards de inline (`internal/vm/inline_guard_test.go`) continuam verdes: `push` ≤ 20, `pop` ≤ 20, `Retain`/`Release` ≤ 80, `NeverTracked` ≤ 20, `arrayTagIsRefSlot` ≤ 20, `ensureCallCapacity` ≤ 80; novo: `isASCII` inlinável (≤ 80 — não é chamada de `run()`).
- `go test ./...` verde; `go test -race ./internal/value ./internal/vm` verde.
- Repo CRLF (`core.autocrlf=true`): editar com Edit tool; conferir diffs com `git diff --numstat` (linhas trocadas ≈ linhas tocadas, nunca o arquivo inteiro).
- Binários de benchmark em disco local: `$S\bench\` onde `$S = C:\Users\sandr\AppData\Local\Temp\claude\C--Users-sandr-Documents-noxy\58670b25-86ee-451c-a716-ecd4cec33bde\scratchpad` (`noxy_base.exe` já está lá, buildado de 73cf11a). Máquina sem `go test`/build concorrente durante medições.
- Todos os comandos assumem cwd = raiz do worktree, Git Bash.

---

### Task 1: `isASCII` + guard de inline

**Files:**
- Create: `internal/vm/strings_ascii.go`
- Create: `internal/vm/strings_ascii_test.go`
- Modify: `internal/vm/inline_guard_test.go` (nova função de teste no fim)

**Interfaces:**
- Produces: `func isASCII(s string) bool` — true sse todo byte de `s` é `< utf8.RuneSelf` (0x80); `isASCII("") == true`.

- [ ] **Step 1: Teste (falha: função não existe)**

```go
package vm

import "testing"

func TestIsASCII(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"", true},
		{"item_123456", true},
		{"\x00\x7f", true},
		{"caf\u00e9", false},
		{"\U0001F642", false},
		{"abc\x80", false},
		{"\xffabc", false},
	}
	for _, tc := range cases {
		if got := isASCII(tc.in); got != tc.want {
			t.Errorf("isASCII(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}
```

- [ ] **Step 2: Rodar** `go test ./internal/vm -run TestIsASCII` → FAIL (undefined: isASCII).

- [ ] **Step 3: Implementar** `internal/vm/strings_ascii.go`:

```go
package vm

import "unicode/utf8"

// isASCII responde se s pode ser indexada por byte: todo byte < 0x80 e um
// code point inteiro, entao indice em runes == indice em bytes. Qualquer byte
// >= 0x80 — continuacao multibyte ou lixo invalido — devolve false e o site
// cai no caminho []rune de sempre. Sem alocacao (issue #66, item 2).
func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= utf8.RuneSelf {
			return false
		}
	}
	return true
}
```

- [ ] **Step 4: Rodar** `go test ./internal/vm -run TestIsASCII` → PASS.

- [ ] **Step 5: Guard de inline** — acrescentar ao fim de `inline_guard_test.go`:

```go
// isASCII (strings_ascii.go) decide o fast path de substring/char_at/s[i]/
// slice (issue #66, item 2). E chamada de builtins e de getIndexGeneric —
// funcoes normais, orcamento 80 — nunca de dentro de run().
func TestIsASCIIStaysInlinable(t *testing.T) {
	build := exec.Command("go", "build", "-gcflags=-m=2", "./")
	output, err := build.CombinedOutput()
	if err != nil {
		t.Fatalf("go build -gcflags=-m=2 failed: %v\n%s", err, output)
	}
	pattern := regexp.MustCompile(`can inline isASCII with cost (\d+)`)
	match := pattern.FindStringSubmatch(string(output))
	if match == nil {
		t.Fatalf("o compilador nao inlina isASCII — procure por 'cannot inline isASCII' em `go build -gcflags=-m=2 ./internal/vm`")
	}
	cost, convErr := strconv.Atoi(match[1])
	if convErr != nil {
		t.Fatalf("custo ilegivel em %q: %v", match[0], convErr)
	}
	if cost > inlineNormalMaxCost {
		t.Errorf("isASCII tem custo de inline %d, maximo %d", cost, inlineNormalMaxCost)
	}
}
```

- [ ] **Step 6: Rodar** `go test ./internal/vm -run 'TestIsASCII|TestPushStaysInlined|TestRetainRelease'` → PASS. Anotar o custo reportado (`go build -gcflags=-m=2 ./internal/vm 2>&1 | grep "inline isASCII"`).

- [ ] **Step 7: Commit** `perf(vm): isASCII — varredura por byte sem alocar, com guard de inline (issue #66, item 2)`.

---

### Task 2: `strings_substring` e `strings_char_at` com fast path ASCII

**Files:**
- Modify: `internal/vm/builtins_strings.go` (natives `strings_substring` ~l.278 e `strings_char_at` ~l.402)
- Modify: `internal/vm/builtins_strings_test.go` (tabela `TestStringBuiltinsScalarTables`)

**Interfaces:**
- Consumes: `isASCII` (Task 1).
- Produces: `func clampSubstringRange(start, end, n int) (lo, hi int, ok bool)` em `builtins_strings.go` — o clamp de `substring` (negativos contam do fim, depois clamp a `[0, n]`; `ok == false` quando `lo >= hi`), compartilhado pelos dois ramos.

- [ ] **Step 1: Testes (falham hoje? NÃO — são de equivalência; escrevê-los antes garante que o ramo novo os honra)**

Acrescentar à tabela de `TestStringBuiltinsScalarTables` (o `want` é escrito à mão):

```go
		// issue #66 item 2: ramo ASCII e ramo rune dao o mesmo resultado
		{name: "substring ascii mid", builtin: "strings_substring", args: []value.Value{value.NewString("item_12345"), value.NewInt(5), value.NewInt(6)}, want: value.NewString("1")},
		{name: "substring ascii whole", builtin: "strings_substring", args: []value.Value{value.NewString("abc"), value.NewInt(0), value.NewInt(3)}, want: value.NewString("abc")},
		{name: "substring ascii end at n", builtin: "strings_substring", args: []value.Value{value.NewString("abc"), value.NewInt(2), value.NewInt(3)}, want: value.NewString("c")},
		{name: "substring ascii start at n", builtin: "strings_substring", args: []value.Value{value.NewString("abc"), value.NewInt(3), value.NewInt(5)}, want: value.NewString("")},
		{name: "substring ascii negative both", builtin: "strings_substring", args: []value.Value{value.NewString("abcdef"), value.NewInt(-4), value.NewInt(-1)}, want: value.NewString("cde")},
		{name: "substring ascii negative overflow", builtin: "strings_substring", args: []value.Value{value.NewString("abc"), value.NewInt(-10), value.NewInt(-10)}, want: value.NewString("")},
		{name: "substring accent negative", builtin: "strings_substring", args: []value.Value{value.NewString("caf\u00e9s"), value.NewInt(-2), value.NewInt(5)}, want: value.NewString("\u00e9s")},
		{name: "substring emoji boundaries", builtin: "strings_substring", args: []value.Value{value.NewString("a\U0001F642b"), value.NewInt(1), value.NewInt(2)}, want: value.NewString("\U0001F642")},
		{name: "substring mixed past end", builtin: "strings_substring", args: []value.Value{value.NewString("\u00e9a"), value.NewInt(1), value.NewInt(9)}, want: value.NewString("a")},
		{name: "char at ascii first", builtin: "strings_char_at", args: []value.Value{value.NewString("abc"), value.NewInt(0)}, want: value.NewString("a")},
		{name: "char at ascii last", builtin: "strings_char_at", args: []value.Value{value.NewString("abc"), value.NewInt(2)}, want: value.NewString("c")},
		{name: "char at ascii past end", builtin: "strings_char_at", args: []value.Value{value.NewString("abc"), value.NewInt(3)}, want: value.NewString("")},
		{name: "char at ascii negative", builtin: "strings_char_at", args: []value.Value{value.NewString("abc"), value.NewInt(-1)}, want: value.NewString("")},
		{name: "char at accent", builtin: "strings_char_at", args: []value.Value{value.NewString("caf\u00e9"), value.NewInt(3)}, want: value.NewString("\u00e9")},
		{name: "char at emoji past end", builtin: "strings_char_at", args: []value.Value{value.NewString("a\U0001F642"), value.NewInt(2)}, want: value.NewString("")},
```

- [ ] **Step 2: Rodar** `go test ./internal/vm -run TestStringBuiltinsScalarTables` → PASS (baseline: o ramo rune já faz isso). Anotar: é o contrato que o ramo novo tem de manter.

- [ ] **Step 3: Implementar.** Em `builtins_strings.go`, antes de `defineStringBuiltins`:

```go
// clampSubstringRange e o clamp de strings.substring: indices negativos contam
// do fim (Python), depois tudo e preso a [0, n]; ok == false quando a faixa
// fica vazia. n e o comprimento em runes (ou em bytes quando a string e ASCII
// — issue #66, item 2); os dois ramos passam por aqui para nao divergirem.
func clampSubstringRange(start, end, n int) (lo, hi int, ok bool) {
	if start < 0 {
		start = n + start
	}
	if end < 0 {
		end = n + end
	}
	if start < 0 {
		start = 0
	}
	if end < 0 {
		end = 0
	}
	if start > n {
		start = n
	}
	if end > n {
		end = n
	}
	return start, end, start < end
}
```

Corpo de `strings_substring` a partir de `s := args[0].String()`:

```go
		s := args[0].String()
		start := int(args[1].Int())
		end := int(args[2].Int())
		if isASCII(s) {
			lo, hi, ok := clampSubstringRange(start, end, len(s))
			if !ok {
				return value.NewString(""), nil
			}
			return value.NewString(s[lo:hi]), nil
		}
		runes := []rune(s)
		lo, hi, ok := clampSubstringRange(start, end, len(runes))
		if !ok {
			return value.NewString(""), nil
		}
		return value.NewString(string(runes[lo:hi])), nil
```

Corpo de `strings_char_at` a partir de `s := args[0].String()`:

```go
		s := args[0].String()
		idx := int(args[1].Int())
		if isASCII(s) {
			if idx < 0 || idx >= len(s) {
				return value.NewString(""), nil
			}
			return value.NewString(s[idx:idx+1]), nil
		}
		runes := []rune(s)
		if idx < 0 || idx >= len(runes) {
			return value.NewString(""), nil
		}
		return value.NewString(string(runes[idx])), nil
```

- [ ] **Step 4: Rodar** `go test ./internal/vm -run 'TestStringBuiltinsScalarTables|TestStrings'` → PASS; `go run ./cmd/noxy noxy_examples/test_substring.nx` → todos `ok`.

- [ ] **Step 5: Commit** `perf(vm): substring/char_at por byte slicing quando a string e ASCII; clamp compartilhado (issue #66, item 2)`.

---

### Task 3: `s[i]` (`getIndexGeneric`) e `slice()` de string

**Files:**
- Modify: `internal/vm/index_ops.go` (~l.124-136, ramo `string` de `getIndexGeneric`)
- Modify: `internal/vm/builtins_collections.go` (~l.247-256, caso string de `slice`)
- Modify: `internal/vm/arithmetic_semantics_test.go` (`TestStringIndexingIsByCodePointAndBoundsChecked`)
- Modify: `internal/vm/builtins_collections_test.go` (`TestSliceBuiltin`, tabela `stringTests`)

**Interfaces:**
- Consumes: `isASCII` (Task 1).

- [ ] **Step 1: Testes de equivalência (passam no baseline).** Em `TestStringIndexingIsByCodePointAndBoundsChecked`, acrescentar aos `cases` de erro:

```go
		{"ascii past end", "let s: string = \"item_1\"\nlet i: int = 6\nprint(s[i])\n", "string index out of bounds"},
		{"ascii negative", "let s: string = \"item_1\"\nlet i: int = -1\nprint(s[i])\n", "string index out of bounds"},
		{"empty string", "let s: string = \"\"\nlet i: int = 0\nprint(s[i])\n", "string index out of bounds"},
```

e, antes dos `cases`, um segundo `captureVMSource` ASCII:

```go
	gotASCII := captureVMSource(t, "let s: string = \"item_1\"\ntest_report([s[0], s[4], s[5]])\n")
	wantASCII := []string{"i", "_", "1"}
	for i, cell := range semArray(t, gotASCII) {
		if s, ok := cell.Obj.(string); !ok || s != wantASCII[i] {
			t.Fatalf("ascii célula %d: got %s, want %q", i, cell.String(), wantASCII[i])
		}
	}
```

Em `TestSliceBuiltin.stringTests`:

```go
		{name: "ascii string mid", sequence: value.NewString("item_12345"), start: 5, end: 6, want: value.NewString("1")},
		{name: "ascii string clamps", sequence: value.NewString("abc"), start: -2, end: 99, want: value.NewString("abc")},
		{name: "ascii string reversed", sequence: value.NewString("abc"), start: 2, end: 1, want: value.NewString("")},
		{name: "accent string end clamp", sequence: value.NewString("caf\u00e9"), start: 3, end: 10, want: value.NewString("\u00e9")},
```

- [ ] **Step 2: Rodar** `go test ./internal/vm -run 'TestStringIndexingIsByCodePointAndBoundsChecked|TestSliceBuiltin'` → PASS.

- [ ] **Step 3: Implementar.** `index_ops.go`, ramo string:

```go
		} else if str, ok := collectionVal.Obj.(string); ok {
			// String indexing e por code point (spec §12). Se a string e ASCII,
			// code point == byte e a fatia de 1 byte compartilha o storage —
			// sem []rune (issue #66, item 2). Nao-ASCII: decodifica como antes.
			if indexVal.Type != value.VAL_INT {
				return vm.runtimeError(c, ip, "string index must be integer")
			}
			idx := int(indexVal.Int())
			if isASCII(str) {
				if idx < 0 || idx >= len(str) {
					return vm.runtimeError(c, ip, "string index out of bounds")
				}
				vm.push(value.NewString(str[idx : idx+1]))
				return nil
			}
			runes := []rune(str)
			if idx < 0 || idx >= len(runes) {
				return vm.runtimeError(c, ip, "string index out of bounds")
			}
			vm.push(value.NewString(string(runes[idx])))
			return nil
		}
```

`builtins_collections.go`, caso string de `slice`:

```go
			if str, ok := seq.Obj.(string); ok {
				if isASCII(str) {
					start = clamp(start, len(str))
					end = clamp(end, len(str))
					if start > end {
						return value.NewString("")
					}
					return value.NewString(str[start:end])
				}
				runes := []rune(str)
				start = clamp(start, len(runes))
				end = clamp(end, len(runes))
				if start > end {
					return value.NewString("")
				}
				return value.NewString(string(runes[start:end]))
			}
```

- [ ] **Step 4: Rodar** `go test ./internal/vm` (pacote inteiro) → PASS; `go vet ./internal/vm` limpo.

- [ ] **Step 5: Commit** `perf(vm): s[i] e slice() de string por byte quando ASCII, mesma mensagem de erro (issue #66, item 2)`.

---

### Task 4: `Value.String()` de int via `strconv` e `to_str` escalar sem validação

**Files:**
- Modify: `internal/value/value.go` (`func (v Value) String()`, caso `VAL_INT`, ~l.632)
- Create: `internal/value/string_int_test.go`
- Modify: `internal/vm/builtins_core.go` (`to_str`, ~l.53-75)
- Modify: `internal/vm/builtins_core_test.go` (tabela com `to_str`)

- [ ] **Step 1: Testes.** `internal/value/string_int_test.go`:

```go
package value

import (
	"fmt"
	"math"
	"testing"
)

// Value.String() de int usa strconv.FormatInt (issue #66, item 2): tem de ser
// byte a byte igual ao "%d" de antes, inclusive nos extremos.
func TestIntStringMatchesPercentD(t *testing.T) {
	for _, n := range []int64{0, 1, -1, 7, -42, 1234567890123, math.MaxInt64, math.MinInt64} {
		if got, want := NewInt(n).String(), fmt.Sprintf("%d", n); got != want {
			t.Errorf("NewInt(%d).String() = %q, want %q", n, got, want)
		}
	}
}
```

Em `builtins_core_test.go`, acrescentar à tabela que já tem `to_str int`:

```go
		{name: "to_str int zero", builtin: "to_str", args: []value.Value{value.NewInt(0)}, want: value.NewString("0")},
		{name: "to_str int min", builtin: "to_str", args: []value.Value{value.NewInt(-9223372036854775808)}, want: value.NewString("-9223372036854775808")},
		{name: "to_str float negative", builtin: "to_str", args: []value.Value{value.NewFloat(-0.5)}, want: value.NewString("-0.500000")},
		{name: "to_str bool false", builtin: "to_str", args: []value.Value{value.NewBool(false)}, want: value.NewString("false")},
```

- [ ] **Step 2: Rodar** `go test ./internal/value -run TestIntStringMatchesPercentD` e `go test ./internal/vm -run TestCoreBuiltins` (ou o nome da função que contém a tabela) → PASS (baseline).

- [ ] **Step 3: Implementar.** `value.go`: `case VAL_INT: return strconv.FormatInt(v.Int(), 10)` (import `strconv`). `builtins_core.go`, `to_str`, entre o ramo `VAL_BYTES` e o `result := args[0].String()`:

```go
		// Escalares renderizam ASCII por construcao: nada a validar (issue #66,
		// item 2 — to_str(int) e o termo mais caro de string_ops).
		switch args[0].Type {
		case value.VAL_INT, value.VAL_FLOAT, value.VAL_BOOL, value.VAL_NULL:
			return value.NewString(args[0].String()), nil
		}
```

- [ ] **Step 4: Rodar** `go test ./internal/value ./internal/vm` → PASS (inclui `TestToStrValidatesUTF8` em `builtins_convert_test.go`, que tem de continuar passando).

- [ ] **Step 5: Commit** `perf(value,vm): Value.String() de int via strconv.FormatInt; to_str de escalar sem requireValidUTF8 (issue #66, item 2)`.

---

### Task 5: Verificação completa + medição + relatório

**Files:**
- Create: `benchmarks/results/2026-08-22-issue-66-strings-raw.md`
- Modify: `benchmarks/RESULTS.md` (nova seção no topo, após o título)
- Modify: `benchmarks/cross_runtime/results/cross_runtime.md` (regravado pelo script)

- [ ] **Step 1: Verificação**: `go test ./...`; `go test -race ./internal/value ./internal/vm`; `go run ./cmd/noxy noxy_examples/run_all_tests_concurrent.nx` (0 falhas); guards de inline.

- [ ] **Step 2: Binários**: `go build -o "$S/bench/noxy_str.exe" ./cmd/noxy` (head). Opcional, para atribuir o ganho: `noxy_s1.exe` do commit da Task 3 (só ASCII, sem `to_str`) via `git stash`/checkout do commit no worktree → build → voltar.

- [ ] **Step 3: Cópias BOM** dos três scripts (`benchmarks/_bom_interleaved_compare.ps1`, `_bom_compare_examples.ps1`, `cross_runtime/_bom_run_cross_runtime.ps1`) — já em `.git/info/exclude`. Gerar com PowerShell: `[IO.File]::WriteAllText(dst, (Get-Content -Raw -Encoding UTF8 src), (New-Object System.Text.UTF8Encoding $true))`.

- [ ] **Step 4: Medir** (sem go test/build concorrente):
  - `powershell -NoProfile -File benchmarks/_bom_interleaved_compare.ps1 -Baseline $S/bench/noxy_base.exe -Candidate $S/bench/noxy_str.exe -BaselineLabel v0150 -CandidateLabel str -Runs 9`
  - `powershell -NoProfile -File benchmarks/_bom_compare_examples.ps1 -Baseline ... -Candidate ...` → 0 divergentes
  - `powershell -NoProfile -File benchmarks/cross_runtime/_bom_run_cross_runtime.ps1 -Noxy $S/bench/noxy_str.exe -NoxyBaseline $S/bench/noxy_base.exe -BaselineLabel v0150`
  - A/B focado `string_ops` 11 intercaladas (loop em PowerShell medindo `Measure-Command`), e perfil `--cpuprofile` do head em `string_ops_10x.nx`.
  - Gates CoW ≤ +5 %: `bench_typed_call_map`, `bench_share_mutate`, `bench_call_light`, `bench_conway`; sentinela `bench_generic_vs_hand`.

- [ ] **Step 5: Relatório**: raw em `benchmarks/results/2026-08-22-issue-66-strings-raw.md` (tabelas dos scripts, carga por passo, perfis antes/depois); seção nova no topo de `RESULTS.md` no formato da seção do item 1 (verificação completa, headline, cross-runtime, leitura, gates, o que resta). Apagar as cópias `_bom_*`.

- [ ] **Step 6: Commit** `docs(bench): fast path ASCII de strings + to_str(int) medidos contra v0.15.0 (issue #66, item 2)`.

---

### Task 6: Bump v0.15.1 (6 pontos) + CHANGELOG

**Files:**
- Modify: `internal/version/version.go`; `CHANGELOG.md`; `README.md` (badge linha 1 + banner do REPL); `AGENTS.md` (linha "Versão"); spec `sys.version` (`grep -rn "0.15.0" docs/ spec* --include=*.md`); `docs/index.html` (hero badge ~l.58 e `print(sys.version)` ~l.385).

- [ ] **Step 1:** `grep -rn "0\.15\.0" --include=*.go --include=*.md --include=*.html . | grep -v node_modules | grep -v CHANGELOG` → lista exata dos pontos; trocar por `0.15.1` em cada um (Edit tool).
- [ ] **Step 2:** CHANGELOG: entrada `## [0.15.1] - 2026-08-22` com "Desempenho" (os 4 sites + to_str) e referência a `RESULTS.md`/issue #66 item 2.
- [ ] **Step 3:** `go test ./internal/version/...` (se existir teste de versão) e `go run ./cmd/noxy --version` → `Noxy v0.15.1`.
- [ ] **Step 4: Commit** `chore(version): noxy v0.15.1 — CHANGELOG (strings ASCII + to_str(int), issue #66 item 2), README, AGENTS, spec, site, version.go`.

---

### Task 7: Finalizar branch (superpowers:finishing-a-development-branch)

- [ ] `git diff --numstat origin/develop` — conferir que nenhum arquivo foi reescrito inteiro (CRLF).
- [ ] Push `-u origin perf/issue-66-string-ascii-fastpath`.
- [ ] PR base `develop`, título `perf/issue-66-string-ascii-fastpath - Strings: fast path ASCII em substring/char_at/s[i]/slice e to_str(int) sem fmt (issue #66, item 2)`, label `not available to review`, `--assignee @me`, body Summary / Components / Test Plan (checkboxes) com `Refs #66`.
- [ ] Comentário na #66 com a tabela de números (headline + cross-runtime) e o que resta (wrapper de `substring`, boxing).
- [ ] Reportar ao usuário e parar.

# Módulo `regex` — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Módulo `regex` na stdlib do Noxy sobre o motor RE2 do Go (`regexp`), com índices em **runas** compondo com `strings.substring`/`length`.

**Architecture:** Par nativa/wrapper no padrão dos módulos existentes: `internal/vm/builtins_regex.go` registra as nativas `regex_*` (handles de regex compilada em `machine.shared.Regexes`, um `handleRegistry` como o do sqlite), e `internal/stdlib/regex.nx` declara os structs (`Regex`, `CompileResult`, `Match`, `MatchResult`, `MatchesResult`) e wrappers finos que passam templates de instância para as nativas (padrão `sqlite_query`). Offsets em bytes do `regexp` são convertidos para índices de runa por um conversor incremental com fast path ASCII (issue #66, item 2).

**Tech Stack:** Go stdlib `regexp` + `unicode/utf8` (nenhuma dependência nova no `go.mod`); infraestrutura existente da VM (`handleRegistry`, `DefineContextualNative`, `value.NewInstanceWith`).

**Spec:** embutida abaixo (seção "API contratada") — decidida em conversa em 2026-08-23; não há doc separado.

## Global Constraints

- Nenhuma dependência nova no `go.mod` (só `regexp`/`unicode/utf8` da stdlib Go).
- **Todos os índices expostos ao Noxy são em runas** (não bytes), exclusivos no fim — mesmíssimo contrato de `strings.substring` (`builtins_strings.go:304`, spec §12 "Indexação de strings"). Precedente da conversão: `strings_index_of` (`builtins_strings.go:127-133`).
- Semântica RE2: sem lookahead/lookbehind/backreferences; tempo linear. Documentar como limitação, não contornar.
- Campo `end` é proibido (palavra-chave do Noxy) — usar `end_idx`, como `substring`.
- Erros: padrão inválido em `compile` → `CompileResult.ok=false` (resultado, não raise). Handle inválido/liberado em qualquer nativa e padrão inválido nos atalhos `matches`/`search` → **erro de runtime** (raise; capturável com `call_result`), seguindo "raise for bugs, results for data" (spec §7).
- "Não casou" NÃO é erro: `MatchResult.ok=false` sem campo `error`.
- Comentários e mensagens de commit em pt-BR, no estilo do repo; código/identificadores em inglês.
- Versão final: **0.18.0** (minor — módulo novo), branch `feat/regex-module` a partir de `main`.
- `go test ./...` verde ao fim de cada task; teste de higiene `TestStdlibWrappersCallOnlyRegisteredNatives` e `TestBuiltinSourceLayout` inclusos.

## API contratada (spec embutida)

```noxy
// use regex
struct Regex          handle: int, pattern: string
struct CompileResult  ok: bool, regex: Regex, error: string
struct Match          text: string, start: int, end_idx: int,
                      groups: string[], group_starts: int[], group_ends: int[]
struct MatchResult    ok: bool, match: Match
struct MatchesResult  ok: bool, matches: Match[]

func compile(pattern: string) -> CompileResult
func free(re: Regex) -> void
func is_match(re: Regex, s: string) -> bool
func find(re: Regex, s: string) -> MatchResult
func find_all(re: Regex, s: string) -> MatchesResult
func replace(re: Regex, s: string, replacement: string) -> string  // $1, ${name}
func split(re: Regex, s: string) -> string[]
// Atalhos (compilam com cache interno; padrão inválido → runtime error):
func matches(pattern: string, s: string) -> bool
func search(pattern: string, s: string) -> MatchResult
```

Convenções de `Match`: `groups[0]` é o match inteiro (`== text`); grupo que não participou vem `""` com `group_starts[i] == group_ends[i] == -1`. `start`/`end_idx` e os arrays de grupo são índices de runa tais que `strings.substring(s, m.start, m.end_idx) == m.text` para qualquer string UTF-8 válida.

Nativas registradas (todas contextuais, via `nativeVM(context)`):

| Nativa | Assinatura (args) | Devolve |
|---|---|---|
| `regex_compile` | `(pattern, CompileResult-template)` | instância `CompileResult` |
| `regex_free` | `(Regex-instance)` | `bool` (false em double-free) |
| `regex_is_match` | `(Regex-instance, s)` | `bool` |
| `regex_find` | `(Regex-instance, s, MatchResult-template)` | instância `MatchResult` |
| `regex_find_all` | `(Regex-instance, s, MatchesResult-template, Match-template)` | instância `MatchesResult` |
| `regex_replace` | `(Regex-instance, s, replacement)` | `string` |
| `regex_split` | `(Regex-instance, s)` | `string[]` |
| `regex_quick_is_match` | `(pattern, s)` | `bool` |
| `regex_quick_find` | `(pattern, s, MatchResult-template)` | instância `MatchResult` |

Os templates carregam os structs para a nativa instanciar (padrão `sqlite_query(db, sql, QueryResult(...), Row([]))` — `builtins_sqlite.go:290-355`). O template de `MatchResult` traz uma instância de `Match` no campo `match`, de onde a nativa extrai o `*value.ObjStruct` de `Match`.

---

### Task 1: Conversor byte→runa (`runeConverter`)

**Files:**
- Create: `internal/vm/regex_runes.go`
- Test: `internal/vm/regex_runes_test.go`

**Interfaces:**
- Consumes: `isASCII(s string) bool` (já existe em `internal/vm/strings_ascii.go:9`).
- Produces: `newRuneConverter(s string) *runeConverter` e `(*runeConverter).index(byteOff int) int` — Tasks 3 e 4 dependem. `index` aceita offsets em **qualquer ordem** (grupos regridem em relação ao fim do match), sempre em fronteira de runa; devolve o índice de runa correspondente.

- [ ] **Step 1: Write the failing test**

```go
// internal/vm/regex_runes_test.go
package vm

import "testing"

func TestRuneConverterASCIIFastPath(t *testing.T) {
	converter := newRuneConverter("hello world")
	for _, byteOff := range []int{0, 5, 11} {
		if got := converter.index(byteOff); got != byteOff {
			t.Fatalf("index(%d) = %d, want %d (ASCII: byte == runa)", byteOff, got, byteOff)
		}
	}
}

func TestRuneConverterUnicodeMonotonic(t *testing.T) {
	// "aé🙂z": a=1 byte, é=2 bytes, 🙂=4 bytes, z=1 byte
	// byte:  a=0, é=1, 🙂=3, z=7, fim=8
	// runa:  a=0, é=1, 🙂=2, z=3, fim=4
	converter := newRuneConverter("aé🙂z")
	cases := []struct{ byteOff, want int }{{0, 0}, {1, 1}, {3, 2}, {7, 3}, {8, 4}}
	for _, c := range cases {
		if got := converter.index(c.byteOff); got != c.want {
			t.Fatalf("index(%d) = %d, want %d", c.byteOff, got, c.want)
		}
	}
}

func TestRuneConverterRegression(t *testing.T) {
	// Grupos de um match chegam fora de ordem: fim do match antes do início
	// do grupo 1. O conversor tem de aceitar regressão sem se perder.
	converter := newRuneConverter("aé🙂z")
	if got := converter.index(8); got != 4 {
		t.Fatalf("index(8) = %d, want 4", got)
	}
	if got := converter.index(1); got != 1 {
		t.Fatalf("index(1) apos index(8) = %d, want 1", got)
	}
	if got := converter.index(3); got != 2 {
		t.Fatalf("index(3) = %d, want 2", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/vm -run TestRuneConverter -v`
Expected: FAIL — `undefined: newRuneConverter`

- [ ] **Step 3: Write minimal implementation**

```go
// internal/vm/regex_runes.go
package vm

import "unicode/utf8"

// runeMark é um checkpoint byteOff -> runeIdx já visitado, para converter
// offsets fora de ordem sem rescanear a string desde o começo.
type runeMark struct {
	byteOff int
	runeIdx int
}

// runeConverter traduz offsets de BYTE (como o regexp do Go devolve) em
// índices de RUNA (como o Noxy indexa strings — ver strings_index_of em
// builtins_strings.go). ASCII curto-circuita: byte == runa (issue #66,
// item 2). Offsets sempre chegam em fronteira de runa porque vêm do
// próprio regexp sobre s.
type runeConverter struct {
	s     string
	ascii bool
	marks []runeMark // ordenado por byteOff; marks[0] = {0, 0}
}

func newRuneConverter(s string) *runeConverter {
	return &runeConverter{s: s, ascii: isASCII(s), marks: []runeMark{{0, 0}}}
}

func (converter *runeConverter) index(byteOff int) int {
	if converter.ascii {
		return byteOff
	}
	// Maior mark com byteOff <= alvo (busca binária).
	lo, hi := 0, len(converter.marks)-1
	for lo < hi {
		mid := (lo + hi + 1) / 2
		if converter.marks[mid].byteOff <= byteOff {
			lo = mid
		} else {
			hi = mid - 1
		}
	}
	mark := converter.marks[lo]
	runeIdx := mark.runeIdx + utf8.RuneCountInString(converter.s[mark.byteOff:byteOff])
	if byteOff > converter.marks[len(converter.marks)-1].byteOff {
		converter.marks = append(converter.marks, runeMark{byteOff, runeIdx})
	}
	return runeIdx
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/vm -run TestRuneConverter -v`
Expected: PASS (3 testes)

- [ ] **Step 5: Commit**

```bash
git add internal/vm/regex_runes.go internal/vm/regex_runes_test.go
git commit -m "feat(regex): runeConverter byte->runa com fast path ASCII e checkpoints"
```

---

### Task 2: Registry de handles + `regex_compile`/`regex_free`

**Files:**
- Modify: `internal/vm/vm.go:59-64` (campos de `SharedState`, junto de `Databases`/`Statements`)
- Modify: `internal/vm/builtins.go` (`initializeState` ~linha 10-20; `defineBuiltins` ~linha 36-49)
- Create: `internal/vm/builtins_regex.go`
- Modify: `internal/vm/architecture_test.go:61-74` (mapa de `TestBuiltinSourceLayout`)
- Test: `internal/vm/builtins_regex_test.go`

**Interfaces:**
- Consumes: `handleRegistry[T]` (`internal/vm/resources.go:16`), `nativeVM(context)` (mesmo helper que `builtins_sqlite.go:14` usa), `value.NewInstanceWith`, `value.NewInstance`.
- Produces: `machine.shared.Regexes *handleRegistry[*regexp.Regexp]`; nativas `regex_compile(pattern, CompileResult-template)` e `regex_free(Regex-instance) -> bool`; helper `regexFromInstance(machine *VM, instance *value.ObjInstance) (*regexp.Regexp, bool)` que Tasks 3-5 consomem; helpers de teste `newRegexTestDefinitions()`/`regexTemplate(...)` que as tasks seguintes reutilizam.

- [ ] **Step 1: Write the failing test**

```go
// internal/vm/builtins_regex_test.go
package vm

import (
	"strings"
	"testing"

	"noxy-vm/internal/value"
)

type regexTestDefinitions struct {
	regex         value.Value
	compileResult value.Value
	match         value.Value
	matchResult   value.Value
	matchesResult value.Value
}

func newRegexTestDefinitions() regexTestDefinitions {
	return regexTestDefinitions{
		regex:         value.NewStruct("Regex", []string{"handle", "pattern"}),
		compileResult: value.NewStruct("CompileResult", []string{"ok", "regex", "error"}),
		match:         value.NewStruct("Match", []string{"text", "start", "end_idx", "groups", "group_starts", "group_ends"}),
		matchResult:   value.NewStruct("MatchResult", []string{"ok", "match"}),
		matchesResult: value.NewStruct("MatchesResult", []string{"ok", "matches"}),
	}
}

func regexTemplate(definition value.Value) value.Value {
	return value.NewInstance(definition.Obj.(*value.ObjStruct))
}

// compileTemplate monta CompileResult(false, Regex(0, ""), "") como o
// wrapper regex.nx fará: o template carrega uma instância de Regex no
// campo regex, de onde a nativa extrai o struct.
func compileTemplate(definitions regexTestDefinitions) value.Value {
	template := regexTemplate(definitions.compileResult)
	instance := template.Obj.(*value.ObjInstance)
	instance.Fields["regex"] = regexTemplate(definitions.regex)
	return template
}

func compileRegexForTest(t *testing.T, machine *VM, definitions regexTestDefinitions, pattern string) *value.ObjInstance {
	t.Helper()
	result := requireBuiltinInstance(t,
		callBuiltin(t, machine, "regex_compile", value.NewString(pattern), compileTemplate(definitions)),
		definitions.compileResult)
	if !result.Fields["ok"].Bool() {
		t.Fatalf("compile(%q) falhou: %s", pattern, result.Fields["error"].String())
	}
	return result.Fields["regex"].Obj.(*value.ObjInstance)
}

func TestRegexCompileOk(t *testing.T) {
	machine := New()
	definitions := newRegexTestDefinitions()
	regexInstance := compileRegexForTest(t, machine, definitions, "[0-9]+")
	if handle := regexInstance.Fields["handle"].Int(); handle <= 0 {
		t.Fatalf("handle = %d, want > 0", handle)
	}
	if pattern := regexInstance.Fields["pattern"].String(); pattern != "[0-9]+" {
		t.Fatalf("pattern = %q, want \"[0-9]+\"", pattern)
	}
}

func TestRegexCompileInvalidPattern(t *testing.T) {
	machine := New()
	definitions := newRegexTestDefinitions()
	result := requireBuiltinInstance(t,
		callBuiltin(t, machine, "regex_compile", value.NewString("(unclosed"), compileTemplate(definitions)),
		definitions.compileResult)
	if result.Fields["ok"].Bool() {
		t.Fatal("compile de padrão inválido devolveu ok=true")
	}
	if errorText := result.Fields["error"].String(); !strings.Contains(errorText, "missing closing") {
		t.Fatalf("error = %q, want mensagem do parser RE2", errorText)
	}
	// Regex de erro tem handle 0 (nunca registrado; registry começa em 1).
	regexInstance := result.Fields["regex"].Obj.(*value.ObjInstance)
	if handle := regexInstance.Fields["handle"].Int(); handle != 0 {
		t.Fatalf("handle no erro = %d, want 0", handle)
	}
}

func TestRegexFreeAndDoubleFree(t *testing.T) {
	machine := New()
	definitions := newRegexTestDefinitions()
	regexInstance := compileRegexForTest(t, machine, definitions, "abc")
	regexValue := value.Value{Type: value.VAL_OBJ, Obj: regexInstance}
	if freed := callBuiltin(t, machine, "regex_free", regexValue); !freed.Bool() {
		t.Fatal("free devolveu false para handle válido")
	}
	if freed := callBuiltin(t, machine, "regex_free", regexValue); freed.Bool() {
		t.Fatal("double-free devolveu true")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/vm -run "TestRegexCompile|TestRegexFree" -v`
Expected: FAIL — `builtin "regex_compile" is not registered`

- [ ] **Step 3: Write minimal implementation**

Em `internal/vm/vm.go`, no struct `SharedState` (junto de `Databases`/`Statements`, ~linha 63-64), adicionar:

```go
	Regexes      *handleRegistry[*regexp.Regexp]
```

(e `"regexp"` nos imports de `vm.go`).

Em `internal/vm/builtins.go`, dentro de `initializeState` (após `shared.Statements = ...`):

```go
		shared.Regexes = newHandleRegistry[*regexp.Regexp]()
```

e em `defineBuiltins()` (após `vm.defineJSONBuiltins()`):

```go
	vm.defineRegexBuiltins()
```

Criar `internal/vm/builtins_regex.go`:

```go
package vm

import (
	"fmt"
	"regexp"

	"noxy-vm/internal/value"
)

// regexFromInstance resolve o handle de uma instância Regex para a regex
// compilada; ok=false para handle liberado/inválido (inclui o handle 0 do
// CompileResult de erro).
func regexFromInstance(machine *VM, instance *value.ObjInstance) (*regexp.Regexp, bool) {
	return machine.shared.Regexes.get(int(instance.Fields["handle"].Int()))
}

func (vm *VM) defineRegexBuiltins() {
	vm.DefineContextualNative("regex_compile", func(context value.NativeContext, args []value.Value) (value.Value, error) {
		machine, err := nativeVM(context)
		if err != nil {
			return value.NewNull(), err
		}
		if len(args) != 2 {
			return value.NewNull(), fmt.Errorf("regex.compile: expects 2 arguments, got %d", len(args))
		}
		resultTemplate, ok := args[1].Obj.(*value.ObjInstance)
		if !ok {
			return value.NewNull(), fmt.Errorf("regex.compile: invalid result template")
		}
		regexTemplate, ok := resultTemplate.Fields["regex"].Obj.(*value.ObjInstance)
		if !ok {
			return value.NewNull(), fmt.Errorf("regex.compile: result template missing regex instance")
		}
		pattern := args[0].String()
		compiled, compileErr := regexp.Compile(pattern)
		if compileErr != nil {
			// RC: NewInstanceWith retém a instância Regex aninhada.
			failed := value.NewInstanceWith(regexTemplate.Struct, map[string]value.Value{
				"handle":  value.NewInt(0),
				"pattern": value.NewString(pattern),
			})
			return value.NewInstanceWith(resultTemplate.Struct, map[string]value.Value{
				"ok":    value.NewBool(false),
				"regex": failed,
				"error": value.NewString(compileErr.Error()),
			}), nil
		}
		handle := machine.shared.Regexes.add(compiled)
		compiledInstance := value.NewInstanceWith(regexTemplate.Struct, map[string]value.Value{
			"handle":  value.NewInt(int64(handle)),
			"pattern": value.NewString(pattern),
		})
		return value.NewInstanceWith(resultTemplate.Struct, map[string]value.Value{
			"ok":    value.NewBool(true),
			"regex": compiledInstance,
			"error": value.NewString(""),
		}), nil
	})

	vm.DefineContextualNative("regex_free", func(context value.NativeContext, args []value.Value) (value.Value, error) {
		machine, err := nativeVM(context)
		if err != nil {
			return value.NewNull(), err
		}
		if len(args) != 1 {
			return value.NewBool(false), nil
		}
		instance, ok := args[0].Obj.(*value.ObjInstance)
		if !ok {
			return value.NewBool(false), nil
		}
		_, removed := machine.shared.Regexes.remove(int(instance.Fields["handle"].Int()))
		return value.NewBool(removed), nil
	})
}
```

Em `internal/vm/architecture_test.go`, no mapa de `TestBuiltinSourceLayout` (após a linha de `builtins_json.go`):

```go
		"builtins_regex.go":       {"defineRegexBuiltins"},
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/vm -run "TestRegexCompile|TestRegexFree|TestBuiltinSourceLayout" -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/vm/vm.go internal/vm/builtins.go internal/vm/builtins_regex.go internal/vm/builtins_regex_test.go internal/vm/architecture_test.go
git commit -m "feat(regex): registry de handles, regex_compile e regex_free"
```

---

### Task 3: `regex_is_match` e `regex_find`

**Files:**
- Modify: `internal/vm/builtins_regex.go`
- Test: `internal/vm/builtins_regex_test.go`

**Interfaces:**
- Consumes: `regexFromInstance` (Task 2), `runeConverter` (Task 1), helpers de teste da Task 2.
- Produces: nativas `regex_is_match(Regex, s) -> bool` e `regex_find(Regex, s, MatchResult-template) -> MatchResult`; helper `buildMatchInstance(matchStruct *value.ObjStruct, s string, pairs []int, converter *runeConverter) value.Value` que a Task 4 reutiliza. Convenções de `Match` conforme "API contratada" (grupo ausente: `""`/`-1`/`-1`).

- [ ] **Step 1: Write the failing tests**

Acrescentar em `internal/vm/builtins_regex_test.go`:

```go
// matchResultTemplate monta MatchResult(false, Match("", -1, -1, [], [], []))
// como o wrapper regex.nx fará.
func matchResultTemplate(definitions regexTestDefinitions) value.Value {
	template := regexTemplate(definitions.matchResult)
	instance := template.Obj.(*value.ObjInstance)
	instance.Fields["match"] = regexTemplate(definitions.match)
	return template
}

func regexValueFor(instance *value.ObjInstance) value.Value {
	return value.Value{Type: value.VAL_OBJ, Obj: instance}
}

func intArrayFields(t *testing.T, got value.Value) []int64 {
	t.Helper()
	array, ok := got.Obj.(*value.ObjArray)
	if !ok {
		t.Fatalf("valor não é array: %#v", got)
	}
	items := make([]int64, len(array.Elements))
	for index, item := range array.Elements {
		items[index] = item.Int()
	}
	return items
}

func stringArrayFields(t *testing.T, got value.Value) []string {
	t.Helper()
	array, ok := got.Obj.(*value.ObjArray)
	if !ok {
		t.Fatalf("valor não é array: %#v", got)
	}
	items := make([]string, len(array.Elements))
	for index, item := range array.Elements {
		items[index] = item.String()
	}
	return items
}

func TestRegexIsMatch(t *testing.T) {
	machine := New()
	definitions := newRegexTestDefinitions()
	regexInstance := compileRegexForTest(t, machine, definitions, "[0-9]+")
	if !callBuiltin(t, machine, "regex_is_match", regexValueFor(regexInstance), value.NewString("abc123")).Bool() {
		t.Fatal("is_match(\"[0-9]+\", \"abc123\") = false, want true")
	}
	if callBuiltin(t, machine, "regex_is_match", regexValueFor(regexInstance), value.NewString("abc")).Bool() {
		t.Fatal("is_match(\"[0-9]+\", \"abc\") = true, want false")
	}
}

func TestRegexIsMatchInvalidHandleRaises(t *testing.T) {
	machine := New()
	definitions := newRegexTestDefinitions()
	regexInstance := compileRegexForTest(t, machine, definitions, "abc")
	callBuiltin(t, machine, "regex_free", regexValueFor(regexInstance))
	_, err := requireBuiltin(t, machine, "regex_is_match").Invoke(machine,
		[]value.Value{regexValueFor(regexInstance), value.NewString("abc")})
	if err == nil || !strings.Contains(err.Error(), "invalid regex handle") {
		t.Fatalf("handle liberado: err = %v, want \"invalid regex handle\"", err)
	}
}

func TestRegexFindWithGroupsUnicode(t *testing.T) {
	machine := New()
	definitions := newRegexTestDefinitions()
	// Sujeito com runas multibyte ANTES do match: "aé🙂 12-34"
	// runas: a=0 é=1 🙂=2 espaço=3 1=4 2=5 -=6 3=7 4=8 (length = 9)
	regexInstance := compileRegexForTest(t, machine, definitions, "([0-9]+)-([0-9]+)")
	result := requireBuiltinInstance(t,
		callBuiltin(t, machine, "regex_find", regexValueFor(regexInstance),
			value.NewString("aé🙂 12-34"), matchResultTemplate(definitions)),
		definitions.matchResult)
	if !result.Fields["ok"].Bool() {
		t.Fatal("find não casou, want match")
	}
	matchInstance := result.Fields["match"].Obj.(*value.ObjInstance)
	if text := matchInstance.Fields["text"].String(); text != "12-34" {
		t.Fatalf("text = %q, want \"12-34\"", text)
	}
	// Índices em RUNAS (não bytes): match começa na runa 4, termina na 9.
	if start := matchInstance.Fields["start"].Int(); start != 4 {
		t.Fatalf("start = %d, want 4 (runas, não bytes)", start)
	}
	if endIdx := matchInstance.Fields["end_idx"].Int(); endIdx != 9 {
		t.Fatalf("end_idx = %d, want 9", endIdx)
	}
	if groups := stringArrayFields(t, matchInstance.Fields["groups"]); len(groups) != 3 || groups[0] != "12-34" || groups[1] != "12" || groups[2] != "34" {
		t.Fatalf("groups = %v, want [12-34 12 34]", groups)
	}
	if starts := intArrayFields(t, matchInstance.Fields["group_starts"]); len(starts) != 3 || starts[1] != 4 || starts[2] != 7 {
		t.Fatalf("group_starts = %v, want [4 4 7]", starts)
	}
	if ends := intArrayFields(t, matchInstance.Fields["group_ends"]); len(ends) != 3 || ends[1] != 6 || ends[2] != 9 {
		t.Fatalf("group_ends = %v, want [9 6 9]", ends)
	}
}

func TestRegexFindNoMatchAndAbsentGroup(t *testing.T) {
	machine := New()
	definitions := newRegexTestDefinitions()
	regexInstance := compileRegexForTest(t, machine, definitions, "a(b)?c")
	// Não casou: ok=false, sem erro.
	missed := requireBuiltinInstance(t,
		callBuiltin(t, machine, "regex_find", regexValueFor(regexInstance),
			value.NewString("zzz"), matchResultTemplate(definitions)),
		definitions.matchResult)
	if missed.Fields["ok"].Bool() {
		t.Fatal("find em \"zzz\" devolveu ok=true")
	}
	// Grupo opcional ausente: "" com -1/-1.
	hit := requireBuiltinInstance(t,
		callBuiltin(t, machine, "regex_find", regexValueFor(regexInstance),
			value.NewString("ac"), matchResultTemplate(definitions)),
		definitions.matchResult)
	matchInstance := hit.Fields["match"].Obj.(*value.ObjInstance)
	if groups := stringArrayFields(t, matchInstance.Fields["groups"]); groups[1] != "" {
		t.Fatalf("grupo ausente = %q, want \"\"", groups[1])
	}
	if starts := intArrayFields(t, matchInstance.Fields["group_starts"]); starts[1] != -1 {
		t.Fatalf("group_starts[1] = %d, want -1", starts[1])
	}
	if ends := intArrayFields(t, matchInstance.Fields["group_ends"]); ends[1] != -1 {
		t.Fatalf("group_ends[1] = %d, want -1", ends[1])
	}
}
```

Nota: se `value.ObjArray`/campo `Elements` tiver outro nome no repo, conferir em `internal/value` (`grep -n "type ObjArray" internal/value/*.go`) e ajustar os dois helpers — o resto dos testes não muda.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/vm -run "TestRegexIsMatch|TestRegexFind" -v`
Expected: FAIL — `builtin "regex_is_match" is not registered`

- [ ] **Step 3: Write minimal implementation**

Acrescentar em `internal/vm/builtins_regex.go` (dentro de `defineRegexBuiltins`, e o helper no nível do arquivo):

```go
// buildMatchInstance monta uma instância Match a partir dos pares de offset
// em BYTES do regexp (FindStringSubmatchIndex): groups[0] é o match inteiro;
// grupo que não participou (offset -1) vira "" com índices -1. Todos os
// offsets válidos são convertidos para índices de RUNA pelo converter.
func buildMatchInstance(matchStruct *value.ObjStruct, s string, pairs []int, converter *runeConverter) value.Value {
	total := len(pairs) / 2
	groups := make([]value.Value, total)
	starts := make([]value.Value, total)
	ends := make([]value.Value, total)
	for index := 0; index < total; index++ {
		lo, hi := pairs[2*index], pairs[2*index+1]
		if lo < 0 {
			groups[index] = value.NewString("")
			starts[index] = value.NewInt(-1)
			ends[index] = value.NewInt(-1)
			continue
		}
		groups[index] = value.NewString(s[lo:hi])
		starts[index] = value.NewInt(int64(converter.index(lo)))
		ends[index] = value.NewInt(int64(converter.index(hi)))
	}
	// RC: NewInstanceWith retém os arrays compostos; escalares são no-op.
	return value.NewInstanceWith(matchStruct, map[string]value.Value{
		"text":         groups[0],
		"start":        starts[0],
		"end_idx":      ends[0],
		"groups":       value.NewArray(groups),
		"group_starts": value.NewArray(starts),
		"group_ends":   value.NewArray(ends),
	})
}

// missedMatchResult devolve MatchResult{ok:false} reaproveitando a instância
// Match vazia do template (campo match tipado exige uma instância).
func missedMatchResult(resultTemplate *value.ObjInstance) value.Value {
	return value.NewInstanceWith(resultTemplate.Struct, map[string]value.Value{
		"ok":    value.NewBool(false),
		"match": resultTemplate.Fields["match"],
	})
}
```

e as nativas:

```go
	vm.DefineContextualNative("regex_is_match", func(context value.NativeContext, args []value.Value) (value.Value, error) {
		machine, err := nativeVM(context)
		if err != nil {
			return value.NewNull(), err
		}
		if len(args) != 2 {
			return value.NewBool(false), fmt.Errorf("regex.is_match: expects 2 arguments, got %d", len(args))
		}
		instance, ok := args[0].Obj.(*value.ObjInstance)
		if !ok {
			return value.NewBool(false), fmt.Errorf("regex.is_match: first argument must be a Regex")
		}
		compiled, valid := regexFromInstance(machine, instance)
		if !valid {
			return value.NewBool(false), fmt.Errorf("regex.is_match: invalid regex handle %d", instance.Fields["handle"].Int())
		}
		return value.NewBool(compiled.MatchString(args[1].String())), nil
	})

	vm.DefineContextualNative("regex_find", func(context value.NativeContext, args []value.Value) (value.Value, error) {
		machine, err := nativeVM(context)
		if err != nil {
			return value.NewNull(), err
		}
		if len(args) != 3 {
			return value.NewNull(), fmt.Errorf("regex.find: expects 3 arguments, got %d", len(args))
		}
		instance, ok := args[0].Obj.(*value.ObjInstance)
		if !ok {
			return value.NewNull(), fmt.Errorf("regex.find: first argument must be a Regex")
		}
		resultTemplate, ok := args[2].Obj.(*value.ObjInstance)
		if !ok {
			return value.NewNull(), fmt.Errorf("regex.find: invalid result template")
		}
		matchTemplate, ok := resultTemplate.Fields["match"].Obj.(*value.ObjInstance)
		if !ok {
			return value.NewNull(), fmt.Errorf("regex.find: result template missing match instance")
		}
		compiled, valid := regexFromInstance(machine, instance)
		if !valid {
			return value.NewNull(), fmt.Errorf("regex.find: invalid regex handle %d", instance.Fields["handle"].Int())
		}
		subject := args[1].String()
		pairs := compiled.FindStringSubmatchIndex(subject)
		if pairs == nil {
			return missedMatchResult(resultTemplate), nil
		}
		converter := newRuneConverter(subject)
		return value.NewInstanceWith(resultTemplate.Struct, map[string]value.Value{
			"ok":    value.NewBool(true),
			"match": buildMatchInstance(matchTemplate.Struct, subject, pairs, converter),
		}), nil
	})
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/vm -run "TestRegexIsMatch|TestRegexFind" -v`
Expected: PASS (4 testes)

- [ ] **Step 5: Commit**

```bash
git add internal/vm/builtins_regex.go internal/vm/builtins_regex_test.go
git commit -m "feat(regex): is_match e find com grupos e índices em runas"
```

---

### Task 4: `regex_find_all`

**Files:**
- Modify: `internal/vm/builtins_regex.go`
- Test: `internal/vm/builtins_regex_test.go`

**Interfaces:**
- Consumes: `buildMatchInstance`, `regexFromInstance`, `runeConverter`, helpers de teste (Tasks 1-3).
- Produces: nativa `regex_find_all(Regex, s, MatchesResult-template, Match-template) -> MatchesResult` — um único `runeConverter` por chamada (os matches vêm em ordem crescente; os checkpoints amortizam a conversão).

- [ ] **Step 1: Write the failing test**

```go
func TestRegexFindAllUnicode(t *testing.T) {
	machine := New()
	definitions := newRegexTestDefinitions()
	regexInstance := compileRegexForTest(t, machine, definitions, "[0-9]+")
	// "é1🙂22x333": runas é=0 1=1 🙂=2 2=3,4 x=5 3=6,7,8
	result := requireBuiltinInstance(t,
		callBuiltin(t, machine, "regex_find_all", regexValueFor(regexInstance),
			value.NewString("é1🙂22x333"),
			regexTemplate(definitions.matchesResult), regexTemplate(definitions.match)),
		definitions.matchesResult)
	if !result.Fields["ok"].Bool() {
		t.Fatal("find_all devolveu ok=false")
	}
	matches, ok := result.Fields["matches"].Obj.(*value.ObjArray)
	if !ok || len(matches.Elements) != 3 {
		t.Fatalf("matches = %#v, want 3 itens", result.Fields["matches"])
	}
	wants := []struct {
		text          string
		start, endIdx int64
	}{{"1", 1, 2}, {"22", 3, 5}, {"333", 6, 9}}
	for index, want := range wants {
		matchInstance := matches.Elements[index].Obj.(*value.ObjInstance)
		if text := matchInstance.Fields["text"].String(); text != want.text {
			t.Fatalf("matches[%d].text = %q, want %q", index, text, want.text)
		}
		if start := matchInstance.Fields["start"].Int(); start != want.start {
			t.Fatalf("matches[%d].start = %d, want %d", index, start, want.start)
		}
		if endIdx := matchInstance.Fields["end_idx"].Int(); endIdx != want.endIdx {
			t.Fatalf("matches[%d].end_idx = %d, want %d", index, endIdx, want.endIdx)
		}
	}
}

func TestRegexFindAllEmpty(t *testing.T) {
	machine := New()
	definitions := newRegexTestDefinitions()
	regexInstance := compileRegexForTest(t, machine, definitions, "[0-9]+")
	result := requireBuiltinInstance(t,
		callBuiltin(t, machine, "regex_find_all", regexValueFor(regexInstance),
			value.NewString("sem digitos"),
			regexTemplate(definitions.matchesResult), regexTemplate(definitions.match)),
		definitions.matchesResult)
	if !result.Fields["ok"].Bool() {
		t.Fatal("find_all sem matches deve devolver ok=true com array vazio")
	}
	matches := result.Fields["matches"].Obj.(*value.ObjArray)
	if len(matches.Elements) != 0 {
		t.Fatalf("matches = %d itens, want 0", len(matches.Elements))
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/vm -run TestRegexFindAll -v`
Expected: FAIL — `builtin "regex_find_all" is not registered`

- [ ] **Step 3: Write minimal implementation**

```go
	vm.DefineContextualNative("regex_find_all", func(context value.NativeContext, args []value.Value) (value.Value, error) {
		machine, err := nativeVM(context)
		if err != nil {
			return value.NewNull(), err
		}
		if len(args) != 4 {
			return value.NewNull(), fmt.Errorf("regex.find_all: expects 4 arguments, got %d", len(args))
		}
		instance, ok := args[0].Obj.(*value.ObjInstance)
		if !ok {
			return value.NewNull(), fmt.Errorf("regex.find_all: first argument must be a Regex")
		}
		resultTemplate, ok := args[2].Obj.(*value.ObjInstance)
		if !ok {
			return value.NewNull(), fmt.Errorf("regex.find_all: invalid result template")
		}
		matchTemplate, ok := args[3].Obj.(*value.ObjInstance)
		if !ok {
			return value.NewNull(), fmt.Errorf("regex.find_all: invalid match template")
		}
		compiled, valid := regexFromInstance(machine, instance)
		if !valid {
			return value.NewNull(), fmt.Errorf("regex.find_all: invalid regex handle %d", instance.Fields["handle"].Int())
		}
		subject := args[1].String()
		allPairs := compiled.FindAllStringSubmatchIndex(subject, -1)
		converter := newRuneConverter(subject)
		matchValues := make([]value.Value, 0, len(allPairs))
		for _, pairs := range allPairs {
			matchValues = append(matchValues, buildMatchInstance(matchTemplate.Struct, subject, pairs, converter))
		}
		return value.NewInstanceWith(resultTemplate.Struct, map[string]value.Value{
			"ok":      value.NewBool(true),
			"matches": value.NewArray(matchValues),
		}), nil
	})
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/vm -run TestRegexFindAll -v`
Expected: PASS (2 testes)

- [ ] **Step 5: Commit**

```bash
git add internal/vm/builtins_regex.go internal/vm/builtins_regex_test.go
git commit -m "feat(regex): find_all com conversor de runas amortizado"
```

---

### Task 5: `regex_replace` e `regex_split`

**Files:**
- Modify: `internal/vm/builtins_regex.go`
- Test: `internal/vm/builtins_regex_test.go`

**Interfaces:**
- Consumes: `regexFromInstance`, helpers de teste.
- Produces: nativas `regex_replace(Regex, s, replacement) -> string` (semântica `ReplaceAllString` do Go: `$1`, `${name}`) e `regex_split(Regex, s) -> string[]` (semântica `Split(s, -1)`).

- [ ] **Step 1: Write the failing tests**

```go
func TestRegexReplace(t *testing.T) {
	machine := New()
	definitions := newRegexTestDefinitions()
	regexInstance := compileRegexForTest(t, machine, definitions, "([0-9]+)-([0-9]+)")
	got := callBuiltin(t, machine, "regex_replace", regexValueFor(regexInstance),
		value.NewString("a 12-34 b 5-6"), value.NewString("$2/$1"))
	if got.String() != "a 34/12 b 6/5" {
		t.Fatalf("replace = %q, want \"a 34/12 b 6/5\"", got.String())
	}
}

func TestRegexSplit(t *testing.T) {
	machine := New()
	definitions := newRegexTestDefinitions()
	regexInstance := compileRegexForTest(t, machine, definitions, ", *")
	got := callBuiltin(t, machine, "regex_split", regexValueFor(regexInstance),
		value.NewString("a,  b, c"))
	if items := stringArrayFields(t, got); len(items) != 3 || items[0] != "a" || items[1] != "b" || items[2] != "c" {
		t.Fatalf("split = %v, want [a b c]", items)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/vm -run "TestRegexReplace|TestRegexSplit" -v`
Expected: FAIL — `builtin "regex_replace" is not registered`

- [ ] **Step 3: Write minimal implementation**

```go
	vm.DefineContextualNative("regex_replace", func(context value.NativeContext, args []value.Value) (value.Value, error) {
		machine, err := nativeVM(context)
		if err != nil {
			return value.NewNull(), err
		}
		if len(args) != 3 {
			return value.NewNull(), fmt.Errorf("regex.replace: expects 3 arguments, got %d", len(args))
		}
		instance, ok := args[0].Obj.(*value.ObjInstance)
		if !ok {
			return value.NewNull(), fmt.Errorf("regex.replace: first argument must be a Regex")
		}
		compiled, valid := regexFromInstance(machine, instance)
		if !valid {
			return value.NewNull(), fmt.Errorf("regex.replace: invalid regex handle %d", instance.Fields["handle"].Int())
		}
		return value.NewString(compiled.ReplaceAllString(args[1].String(), args[2].String())), nil
	})

	vm.DefineContextualNative("regex_split", func(context value.NativeContext, args []value.Value) (value.Value, error) {
		machine, err := nativeVM(context)
		if err != nil {
			return value.NewNull(), err
		}
		if len(args) != 2 {
			return value.NewNull(), fmt.Errorf("regex.split: expects 2 arguments, got %d", len(args))
		}
		instance, ok := args[0].Obj.(*value.ObjInstance)
		if !ok {
			return value.NewNull(), fmt.Errorf("regex.split: first argument must be a Regex")
		}
		compiled, valid := regexFromInstance(machine, instance)
		if !valid {
			return value.NewNull(), fmt.Errorf("regex.split: invalid regex handle %d", instance.Fields["handle"].Int())
		}
		parts := compiled.Split(args[1].String(), -1)
		items := make([]value.Value, len(parts))
		for index, part := range parts {
			items[index] = value.NewString(part)
		}
		return value.NewArray(items), nil
	})
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/vm -run "TestRegexReplace|TestRegexSplit" -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/vm/builtins_regex.go internal/vm/builtins_regex_test.go
git commit -m "feat(regex): replace com \$1/\${name} e split"
```

---

### Task 6: Atalhos `regex_quick_is_match`/`regex_quick_find` com cache de padrão

**Files:**
- Modify: `internal/vm/vm.go` (campo de cache em `SharedState`)
- Modify: `internal/vm/builtins_regex.go`
- Test: `internal/vm/builtins_regex_test.go`

**Interfaces:**
- Consumes: `buildMatchInstance`, `missedMatchResult`, `matchResultTemplate` (teste).
- Produces: nativas `regex_quick_is_match(pattern, s) -> bool` e `regex_quick_find(pattern, s, MatchResult-template) -> MatchResult`; cache `SharedState.RegexPatternCache sync.Map` (pattern → `*regexp.Regexp`). Padrão inválido → erro de runtime com a mensagem do RE2.

- [ ] **Step 1: Write the failing tests**

```go
func TestRegexQuickIsMatchAndCache(t *testing.T) {
	machine := New()
	if !callBuiltin(t, machine, "regex_quick_is_match", value.NewString("[0-9]+"), value.NewString("x1")).Bool() {
		t.Fatal("quick_is_match = false, want true")
	}
	// Segunda chamada com o mesmo padrão usa o cache (mesma instância).
	first, _ := machine.shared.RegexPatternCache.Load("[0-9]+")
	callBuiltin(t, machine, "regex_quick_is_match", value.NewString("[0-9]+"), value.NewString("y"))
	second, _ := machine.shared.RegexPatternCache.Load("[0-9]+")
	if first == nil || first != second {
		t.Fatal("padrão não foi cacheado entre chamadas")
	}
}

func TestRegexQuickInvalidPatternRaises(t *testing.T) {
	machine := New()
	_, err := requireBuiltin(t, machine, "regex_quick_is_match").Invoke(machine,
		[]value.Value{value.NewString("(unclosed"), value.NewString("x")})
	if err == nil || !strings.Contains(err.Error(), "missing closing") {
		t.Fatalf("padrão inválido: err = %v, want mensagem RE2", err)
	}
}

func TestRegexQuickFind(t *testing.T) {
	machine := New()
	definitions := newRegexTestDefinitions()
	result := requireBuiltinInstance(t,
		callBuiltin(t, machine, "regex_quick_find", value.NewString("[0-9]+"),
			value.NewString("é12"), matchResultTemplate(definitions)),
		definitions.matchResult)
	if !result.Fields["ok"].Bool() {
		t.Fatal("quick_find não casou")
	}
	matchInstance := result.Fields["match"].Obj.(*value.ObjInstance)
	if start := matchInstance.Fields["start"].Int(); start != 1 {
		t.Fatalf("start = %d, want 1 (runa, não byte)", start)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/vm -run TestRegexQuick -v`
Expected: FAIL — `builtin "regex_quick_is_match" is not registered`

- [ ] **Step 3: Write minimal implementation**

Em `internal/vm/vm.go`, no `SharedState` (junto de `Regexes`), adicionar (+ import `"sync"` se já não houver):

```go
	RegexPatternCache sync.Map // pattern string -> *regexp.Regexp (atalhos regex.matches/search)
```

(`sync.Map` zero-value é utilizável — nada a inicializar em `initializeState`.)

Em `internal/vm/builtins_regex.go`, helper + nativas:

```go
// cachedRegex compila e cacheia padrões dos atalhos (regex.matches/search).
// Regex compilada é imutável e segura entre goroutines, então o cache é
// global e nunca expira.
func cachedRegex(machine *VM, pattern string) (*regexp.Regexp, error) {
	if cached, ok := machine.shared.RegexPatternCache.Load(pattern); ok {
		return cached.(*regexp.Regexp), nil
	}
	compiled, err := regexp.Compile(pattern)
	if err != nil {
		return nil, err
	}
	actual, _ := machine.shared.RegexPatternCache.LoadOrStore(pattern, compiled)
	return actual.(*regexp.Regexp), nil
}
```

```go
	vm.DefineContextualNative("regex_quick_is_match", func(context value.NativeContext, args []value.Value) (value.Value, error) {
		machine, err := nativeVM(context)
		if err != nil {
			return value.NewNull(), err
		}
		if len(args) != 2 {
			return value.NewBool(false), fmt.Errorf("regex.matches: expects 2 arguments, got %d", len(args))
		}
		compiled, compileErr := cachedRegex(machine, args[0].String())
		if compileErr != nil {
			return value.NewBool(false), fmt.Errorf("regex.matches: %s", compileErr.Error())
		}
		return value.NewBool(compiled.MatchString(args[1].String())), nil
	})

	vm.DefineContextualNative("regex_quick_find", func(context value.NativeContext, args []value.Value) (value.Value, error) {
		machine, err := nativeVM(context)
		if err != nil {
			return value.NewNull(), err
		}
		if len(args) != 3 {
			return value.NewNull(), fmt.Errorf("regex.search: expects 3 arguments, got %d", len(args))
		}
		resultTemplate, ok := args[2].Obj.(*value.ObjInstance)
		if !ok {
			return value.NewNull(), fmt.Errorf("regex.search: invalid result template")
		}
		matchTemplate, ok := resultTemplate.Fields["match"].Obj.(*value.ObjInstance)
		if !ok {
			return value.NewNull(), fmt.Errorf("regex.search: result template missing match instance")
		}
		compiled, compileErr := cachedRegex(machine, args[0].String())
		if compileErr != nil {
			return value.NewNull(), fmt.Errorf("regex.search: %s", compileErr.Error())
		}
		subject := args[1].String()
		pairs := compiled.FindStringSubmatchIndex(subject)
		if pairs == nil {
			return missedMatchResult(resultTemplate), nil
		}
		converter := newRuneConverter(subject)
		return value.NewInstanceWith(resultTemplate.Struct, map[string]value.Value{
			"ok":    value.NewBool(true),
			"match": buildMatchInstance(matchTemplate.Struct, subject, pairs, converter),
		}), nil
	})
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/vm -run TestRegexQuick -v`
Expected: PASS (3 testes)

- [ ] **Step 5: Commit**

```bash
git add internal/vm/vm.go internal/vm/builtins_regex.go internal/vm/builtins_regex_test.go
git commit -m "feat(regex): atalhos matches/search com cache de padrão"
```

---

### Task 7: `internal/stdlib/regex.nx` + teste de integração `use regex`

**Files:**
- Create: `internal/stdlib/regex.nx` (embed automático: `embed.go` usa `//go:embed *.nx`)
- Test: `internal/vm/builtins_regex_test.go` (integração via `interpretVMSource`)

**Interfaces:**
- Consumes: todas as nativas das Tasks 2-6; helper de teste `captureVMSource` (`vm_test_helpers_test.go:56`).
- Produces: módulo `use regex` completo, com a API contratada. O teste de higiene `TestStdlibWrappersCallOnlyRegisteredNatives` valida wrapper↔nativa automaticamente.

- [ ] **Step 1: Write the failing integration test**

```go
func TestRegexModuleIntegration(t *testing.T) {
	captured := captureVMSource(t, `use regex

let comp: regex.CompileResult = regex.compile("([0-9]+)-([0-9]+)")
let ok1: bool = comp.ok
let re: regex.Regex = comp.regex

let m: regex.MatchResult = regex.find(re, "aé🙂 12-34")
let all: regex.MatchesResult = regex.find_all(re, "1-2 e 3-4")
let swapped: string = regex.replace(re, "12-34", "$2/$1")
let parts: string[] = regex.split(regex.compile(", *").regex, "a, b,  c")
let quick: bool = regex.matches("^a", "abc")
let bad: regex.CompileResult = regex.compile("(unclosed")

test_report([
    ok1,
    m.ok, m.match.text, m.match.start, m.match.end_idx, m.match.groups[1],
    length(all.matches),
    swapped,
    length(parts),
    quick,
    bad.ok,
    regex.free(re) == null
])`)
	array, ok := captured.Obj.(*value.ObjArray)
	if !ok {
		t.Fatalf("test_report não recebeu array: %#v", captured)
	}
	// Nota: regex.free devolve void; `== null` só força o uso — o item 11
	// existe para o wrapper ser exercitado, o valor não é verificado.
	wants := []struct {
		index int
		check func(value.Value) bool
		label string
	}{
		{0, func(v value.Value) bool { return v.Bool() }, "compile.ok"},
		{1, func(v value.Value) bool { return v.Bool() }, "find.ok"},
		{2, func(v value.Value) bool { return v.String() == "12-34" }, "match.text"},
		{3, func(v value.Value) bool { return v.Int() == 4 }, "match.start em runas"},
		{4, func(v value.Value) bool { return v.Int() == 9 }, "match.end_idx em runas"},
		{5, func(v value.Value) bool { return v.String() == "12" }, "groups[1]"},
		{6, func(v value.Value) bool { return v.Int() == 2 }, "find_all count"},
		{7, func(v value.Value) bool { return v.String() == "34/12" }, "replace"},
		{8, func(v value.Value) bool { return v.Int() == 3 }, "split count"},
		{9, func(v value.Value) bool { return v.Bool() }, "matches atalho"},
		{10, func(v value.Value) bool { return !v.Bool() }, "compile inválido ok=false"},
	}
	for _, want := range wants {
		if !want.check(array.Elements[want.index]) {
			t.Fatalf("%s: item %d = %#v", want.label, want.index, array.Elements[want.index])
		}
	}
}
```

Nota: se `regex.free(re) == null` não compilar (comparação com void), trocar o item 11 por uma chamada em statement próprio antes do `test_report` e reduzir o array para 11 itens.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/vm -run TestRegexModuleIntegration -v`
Expected: FAIL — módulo `regex` inexistente (erro do loader de módulos)

- [ ] **Step 3: Write the stdlib module**

```noxy
// internal/stdlib/regex.nx
// Módulo regex para Noxy — motor RE2 (Go regexp): tempo linear garantido,
// sem lookahead/lookbehind/backreferences.
// TODOS os índices (start, end_idx, group_starts, group_ends) são em RUNAS,
// exclusivos no fim — compõem com strings.substring e length (spec §12).

struct Regex
    handle: int
    pattern: string
end

struct CompileResult
    ok: bool
    regex: Regex
    error: string
end

// Um match: groups[0] é o match inteiro; grupo que não participou vem ""
// com group_starts/group_ends -1.
struct Match
    text: string
    start: int
    end_idx: int
    groups: string[]
    group_starts: int[]
    group_ends: int[]
end

// "Não casou" não é erro: ok=false e match vazio.
struct MatchResult
    ok: bool
    match: Match
end

struct MatchesResult
    ok: bool
    matches: Match[]
end

// --- Compilação explícita (recomendada em loops) ---

func compile(pattern: string) -> CompileResult
    return regex_compile(pattern, CompileResult(false, Regex(0, ""), ""))
end

func free(re: Regex) -> void
    regex_free(re)
end

// --- Operações sobre Regex compilada ---
// Handle inválido/liberado é bug do programa: erro de runtime (capturável
// com call_result), não resultado.

func is_match(re: Regex, s: string) -> bool
    return regex_is_match(re, s)
end

func find(re: Regex, s: string) -> MatchResult
    return regex_find(re, s, MatchResult(false, Match("", -1, -1, [], [], [])))
end

func find_all(re: Regex, s: string) -> MatchesResult
    return regex_find_all(re, s, MatchesResult(false, []), Match("", -1, -1, [], [], []))
end

// replacement suporta $1, $2, ${name} (sintaxe ReplaceAllString do Go).
func replace(re: Regex, s: string, replacement: string) -> string
    return regex_replace(re, s, replacement)
end

func split(re: Regex, s: string) -> string[]
    return regex_split(re, s)
end

// --- Atalhos com padrão em string (cache interno; padrão inválido é
// erro de runtime com a mensagem do RE2) ---

func matches(pattern: string, s: string) -> bool
    return regex_quick_is_match(pattern, s)
end

func search(pattern: string, s: string) -> MatchResult
    return regex_quick_find(pattern, s, MatchResult(false, Match("", -1, -1, [], [], [])))
end
```

- [ ] **Step 4: Run tests to verify they pass (integração + higiene + suite vm)**

Run: `go test ./internal/vm -run "TestRegexModuleIntegration|TestStdlibWrappersCallOnlyRegisteredNatives" -v && go test ./internal/vm`
Expected: PASS — a higiene confirma que todo `regex_*` chamado pelo `.nx` está registrado

- [ ] **Step 5: Commit**

```bash
git add internal/stdlib/regex.nx internal/vm/builtins_regex_test.go
git commit -m "feat(regex): módulo stdlib regex.nx com wrappers e teste de integração"
```

---

### Task 8: Exemplo, documentação, versão 0.18.0 e verificação final

**Files:**
- Create: `noxy_examples/test_regex.nx`
- Modify: `docs/NOXY_LANGUAGE_SPEC.md` (nova subseção em §12, antes de `### Network sockets`, ~linha 2359)
- Modify: `CHANGELOG.md` (topo)
- Modify: `internal/version/version.go` (`v0.17.1` → `v0.18.0`)
- Modify: `README.md:111` e `README.md:160` (listas de módulos)
- Modify: `docs/index.html:203` e `docs/index.html:379` (listas de módulos do site)

**Interfaces:**
- Consumes: módulo completo (Task 7).
- Produces: release 0.18.0 documentado; exemplo executável no corpus.

- [ ] **Step 1: Escrever o exemplo**

```noxy
// noxy_examples/test_regex.nx
use regex
use strings

print("Testes do módulo regex...")

// Compilação explícita com grupos
let comp: regex.CompileResult = regex.compile("([0-9]{4})-([0-9]{2})-([0-9]{2})")
if !comp.ok then
    eprint("compile falhou: " + comp.error)
end
let re: regex.Regex = comp.regex

let texto: string = "aniversário: 1990-05-17, festa: 2026-08-23"
let m: regex.MatchResult = regex.find(re, texto)
if m.ok then
    print("primeira data: " + m.match.text)
    print("ano: " + m.match.groups[1])
    // Índices em runas: compõem com strings.substring mesmo com acento antes
    print("recorte: " + strings.substring(texto, m.match.start, m.match.end_idx))
end

let todas: regex.MatchesResult = regex.find_all(re, texto)
print("datas encontradas: " + to_str(length(todas.matches)))

print("iso->br: " + regex.replace(re, texto, "$3/$2/$1"))

let csv: regex.Regex = regex.compile(", *").regex
for parte in regex.split(csv, "um,  dois, três") do
    print("parte: " + parte)
end

// Atalhos
if regex.matches("^ani", texto) then
    print("começa com 'ani'")
end

regex.free(re)
regex.free(csv)
print("Testes finalizados.")
```

- [ ] **Step 2: Rodar o exemplo**

Run: `go run ./cmd/noxy noxy_examples/test_regex.nx` (ou `go build -o noxy.exe ./cmd/noxy && ./noxy.exe noxy_examples/test_regex.nx`)
Expected: saída com `primeira data: 1990-05-17`, `ano: 1990`, `recorte: 1990-05-17`, `datas encontradas: 2`, `iso->br: aniversário: 17/05/1990, festa: 23/08/2026`, as 3 partes e `Testes finalizados.` — sem erro. Se `strings.substring` divergir do recorte, PARAR: é bug de conversão de índice (voltar à Task 3/4), não ajustar o exemplo.

- [ ] **Step 3: Spec — nova subseção em §12**

Inserir em `docs/NOXY_LANGUAGE_SPEC.md`, imediatamente antes de `### Network sockets` (~linha 2359):

```markdown
### Regex (`regex`)

RE2 engine (Go `regexp`): guaranteed linear time, **no**
lookahead/lookbehind/backreferences. All indices exposed to Noxy —
`start`, `end_idx`, `group_starts`, `group_ends` — are **rune indices**,
end-exclusive, so `strings.substring(s, m.start, m.end_idx) == m.text`
holds for any valid UTF-8 subject (same contract as `substring` and
`index_of`; see *Indexação de strings*).

| Struct | Fields |
|---|---|
| `Regex` | `handle: int`, `pattern: string` |
| `CompileResult` | `ok: bool`, `regex: Regex`, `error: string` |
| `Match` | `text: string`, `start: int`, `end_idx: int`, `groups: string[]`, `group_starts: int[]`, `group_ends: int[]` |
| `MatchResult` | `ok: bool`, `match: Match` |
| `MatchesResult` | `ok: bool`, `matches: Match[]` |

`groups[0]` is the whole match (`== text`); a capture group that did not
participate yields `""` with `-1`/`-1` indices. A failed match is **not**
an error: `find` returns `ok=false` with an empty `match`.

| Function | Contract |
|---|---|
| `compile(pattern) -> CompileResult` | `ok=false` + RE2 message on invalid pattern (`regex.handle == 0`) |
| `free(re) -> void` | Releases the compiled regex; using a freed handle is a runtime error |
| `is_match(re, s) -> bool` | Whether `s` contains a match |
| `find(re, s) -> MatchResult` | First match with groups |
| `find_all(re, s) -> MatchesResult` | Every non-overlapping match, left to right |
| `replace(re, s, replacement) -> string` | Replaces every match; `$1`, `$2`, `${name}` expand (Go `ReplaceAllString`) |
| `split(re, s) -> string[]` | Splits `s` around every match |
| `matches(pattern, s) -> bool` | Shortcut: compiles with an internal cache; invalid pattern is a runtime error |
| `search(pattern, s) -> MatchResult` | Shortcut form of `find` |

```noxy
use regex
let re: regex.Regex = regex.compile("([0-9]+)-([0-9]+)").regex
let m: regex.MatchResult = regex.find(re, "aé🙂 12-34")
m.match.start                    // 4  (rune index, not byte offset)
m.match.groups[2]                // "34"
regex.replace(re, "12-34", "$2/$1")   // "34/12"
```
```

- [ ] **Step 4: CHANGELOG, versão, README e site**

`internal/version/version.go`: `const Version = "v0.18.0"`.

Topo do `CHANGELOG.md` (acima de `## [0.17.1]`):

```markdown
## [0.18.0] - 2026-08-23

Módulo `regex` na stdlib: motor RE2 do Go (tempo linear, sem
lookahead/backreferences), índices em runas compondo com
`strings.substring`. Nenhuma dependência nova; nenhum opcode novo.

### Added

- **Módulo `regex`** — `compile`/`free`, `is_match`, `find`, `find_all`,
  `replace` (`$1`/`${name}`), `split` e os atalhos `matches`/`search` com
  cache interno de padrão. Todos os índices de `Match` (`start`,
  `end_idx`, `group_starts`, `group_ends`) são em **runas**, exclusivos no
  fim — `strings.substring(s, m.start, m.end_idx) == m.text` para qualquer
  UTF-8 válido, mesmo contrato de `substring`/`index_of`. Conversão
  byte→runa com fast path ASCII e checkpoints amortizados (issue #66,
  item 2). Padrão inválido em `compile` é resultado (`ok=false` + mensagem
  RE2); handle liberado ou padrão inválido nos atalhos é erro de runtime
  (capturável com `call_result`). Semântica RE2: sem
  lookahead/lookbehind/backreferences. Spec §12 (Regex), exemplo em
  `noxy_examples/test_regex.nx`.
```

`README.md:111`: acrescentar `regex` à lista `(io, net, http, sqlite, json, strings, crypto, time)` → `(io, net, http, sqlite, json, strings, regex, crypto, time)`.
`README.md:160`: `- ✅ Built-in modules (io, net, http, sqlite)` → `- ✅ Built-in modules (io, net, http, sqlite, regex)`.
`docs/index.html:203`: `io, strings, time, sys, net, http, json, crypto, sqlite, rand and errors` → `io, strings, regex, time, sys, net, http, json, crypto, sqlite, rand and errors`.
`docs/index.html:379`: `// json, crypto, sqlite, rand, errors` → `// json, crypto, sqlite, regex, rand, errors`.

(Linhas são as do snapshot de 2026-08-23; localizar por conteúdo se o arquivo tiver mudado.)

- [ ] **Step 5: Verificação final (suite completa + corpus)**

Run: `go test ./...`
Expected: PASS em todos os pacotes.

Corpus (PowerShell, na raiz — todos os exemplos continuam executando):

```powershell
go build -o noxy.exe ./cmd/noxy
$failed = 0
Get-ChildItem noxy_examples -Filter *.nx | ForEach-Object {
    & .\noxy.exe $_.FullName *> $null
    if ($LASTEXITCODE -ne 0 -and $_.Name -notin @("division_error.nx", "file_read_error_go.nx", "watch_file.nx")) {
        Write-Host "FALHOU: $($_.Name)"; $failed++
    }
}
Write-Host "corpus: $failed falhas"
```

Expected: `corpus: 0 falhas` (a lista de exclusão são exemplos que falham por design — erro proposital ou interativo; conferir contra o comportamento em `main` antes de ampliá-la).

- [ ] **Step 6: Commit**

```bash
git add noxy_examples/test_regex.nx docs/NOXY_LANGUAGE_SPEC.md CHANGELOG.md internal/version/version.go README.md docs/index.html
git commit -m "chore(version): noxy v0.18.0 — módulo regex (RE2, índices em runas); CHANGELOG, README, spec, site, version.go"
```

---

## Self-Review (executado em 2026-08-23)

1. **Cobertura da API contratada:** `compile`/`free` → Task 2; `is_match`/`find` → Task 3; `find_all` → Task 4; `replace`/`split` → Task 5; `matches`/`search` → Task 6; structs + wrappers + integração → Task 7; docs/versão/exemplo → Task 8. Contrato de runas → Tasks 1, 3, 4 e testes de integração. Sem lacunas.
2. **Placeholders:** nenhum TBD/TODO; todos os steps têm código completo. Duas notas de contingência explícitas (nome de `ObjArray.Elements` na Task 3; `== null` sobre void na Task 7) com instrução concreta de ajuste.
3. **Consistência de tipos:** `regexFromInstance`, `buildMatchInstance`, `missedMatchResult`, `newRuneConverter`/`index`, `cachedRegex` e os helpers de teste usam os mesmos nomes e assinaturas em todas as tasks; nomes de campos (`end_idx`, `group_starts`, `group_ends`) idênticos em nativas, `.nx`, testes e spec.

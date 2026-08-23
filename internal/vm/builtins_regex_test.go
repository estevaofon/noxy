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
	matches, ok := result.Fields["matches"].Obj.(*value.ObjArray)
	if !ok {
		t.Fatalf("matches should be ObjArray: %#v", result.Fields["matches"])
	}
	if len(matches.Elements) != 0 {
		t.Fatalf("matches = %d itens, want 0", len(matches.Elements))
	}
}

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
    regex.is_match(re, "12-34"),
    regex.search("[0-9]+", "é12").ok,
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
		{11, func(v value.Value) bool { return v.Bool() }, "is_match wrapper"},
		{12, func(v value.Value) bool { return v.Bool() }, "search wrapper"},
	}
	for _, want := range wants {
		if !want.check(array.Elements[want.index]) {
			t.Fatalf("%s: item %d = %#v", want.label, want.index, array.Elements[want.index])
		}
	}
}

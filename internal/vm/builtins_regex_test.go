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

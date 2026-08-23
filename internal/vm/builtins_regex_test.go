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

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
}

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

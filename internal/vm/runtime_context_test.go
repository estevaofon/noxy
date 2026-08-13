package vm

import (
	"testing"

	"noxy-vm/internal/value"
)

func TestContextualNativeReceivesCallingVM(t *testing.T) {
	parent := NewWithConfig(VMConfig{RootPath: "parent"})
	child := NewWithShared(parent.shared, VMConfig{RootPath: "child"})
	parent.DefineContextualNative("active_root", func(context value.NativeContext, _ []value.Value) (value.Value, error) {
		machine, err := nativeVM(context)
		if err != nil {
			return value.NewNull(), err
		}
		return value.NewString(machine.Config.RootPath), nil
	})
	assertBuiltinValue(t, callBuiltin(t, child, "active_root"), value.NewString("child"))
}

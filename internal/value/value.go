package value

import "fmt"

type ValueType int

const (
	VAL_BOOL ValueType = iota
	VAL_NULL
	VAL_INT
	VAL_FLOAT
	VAL_OBJ // String, Arrays, Structs, etc (allocated)
	VAL_FUNCTION
	VAL_NATIVE
)

type Value struct {
	Type    ValueType
	AsBool  bool
	AsInt   int64
	AsFloat float64
	Obj     interface{} // Heap allocated object
}

type ObjFunction struct {
	Name  string
	Arity int
	Chunk interface{} // Avoid cyclic import for now, or use *chunk.Chunk if we fix import cycle
}

type NativeFunc func(args []Value) Value

type ObjNative struct {
	Name string
	Fn   NativeFunc
}

func (v Value) String() string {
	switch v.Type {
	case VAL_BOOL:
		return fmt.Sprintf("%t", v.AsBool)
	case VAL_NULL:
		return "null"
	case VAL_INT:
		return fmt.Sprintf("%d", v.AsInt)
	case VAL_FLOAT:
		return fmt.Sprintf("%f", v.AsFloat)
	case VAL_OBJ:
		return fmt.Sprintf("%s", v.Obj)
	case VAL_FUNCTION:
		return fmt.Sprintf("<fn %s>", v.Obj.(*ObjFunction).Name)
	case VAL_NATIVE:
		return fmt.Sprintf("<native fn %s>", v.Obj.(*ObjNative).Name)
	default:
		return "unknown"
	}
}

// Helper constructors
func NewInt(v int64) Value {
	return Value{Type: VAL_INT, AsInt: v}
}

func NewFloat(v float64) Value {
	return Value{Type: VAL_FLOAT, AsFloat: v}
}

func NewBool(v bool) Value {
	return Value{Type: VAL_BOOL, AsBool: v}
}

func NewNull() Value {
	return Value{Type: VAL_NULL}
}

func NewString(v string) Value {
	return Value{Type: VAL_OBJ, Obj: v}
}

func NewFunction(name string, arity int, chunk interface{}) Value {
	return Value{
		Type: VAL_FUNCTION,
		Obj:  &ObjFunction{Name: name, Arity: arity, Chunk: chunk},
	}
}

func NewNative(name string, fn NativeFunc) Value {
	return Value{
		Type: VAL_NATIVE,
		Obj:  &ObjNative{Name: name, Fn: fn},
	}
}

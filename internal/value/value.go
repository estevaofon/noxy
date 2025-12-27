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

type ObjArray struct {
	Elements []Value
}

func (oa *ObjArray) String() string {
	s := "["
	for i, e := range oa.Elements {
		// Avoid infinite recursion if element is self
		if e.Type == VAL_OBJ {
			if arr, ok := e.Obj.(*ObjArray); ok && arr == oa {
				s += "<cycle>"
			} else {
				s += e.String()
			}
		} else {
			s += e.String()
		}

		if i < len(oa.Elements)-1 {
			s += ", "
		}
	}
	s += "]"
	return s
}

type ObjStruct struct {
	Name   string
	Fields []string
}

func (os *ObjStruct) String() string {
	return fmt.Sprintf("<struct %s>", os.Name)
}

type ObjInstance struct {
	Struct *ObjStruct
	Fields map[string]Value
}

func (oi *ObjInstance) String() string {
	return fmt.Sprintf("<%s instance>", oi.Struct.Name)
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
		// Check for specific object types if they don't implement String() naturally via fmt?
		// But ObjArray implements String(), so fmt.Sprintf("%s", v.Obj) should work if v.Obj satisfies fmt.Stringer?
		// Or we can type switch here.
		switch o := v.Obj.(type) {
		case *ObjArray:
			return o.String()
		case *ObjStruct:
			return o.String()
		case *ObjInstance:
			return o.String()
		case string:
			return o
		default:
			return fmt.Sprintf("%v", v.Obj)
		}
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

func NewArray(elements []Value) Value {
	return Value{Type: VAL_OBJ, Obj: &ObjArray{Elements: elements}}
}

func NewStruct(name string, fields []string) Value {
	return Value{Type: VAL_OBJ, Obj: &ObjStruct{Name: name, Fields: fields}}
}

func NewInstance(def *ObjStruct) Value {
	return Value{Type: VAL_OBJ, Obj: &ObjInstance{Struct: def, Fields: make(map[string]Value)}}
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

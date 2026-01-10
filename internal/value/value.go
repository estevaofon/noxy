package value

import (
	"fmt"
	"sync"
)

type ValueType int

const (
	VAL_BOOL ValueType = iota
	VAL_NULL
	VAL_INT
	VAL_FLOAT
	VAL_OBJ // String, Arrays, Structs, etc (allocated)
	VAL_FUNCTION
	VAL_NATIVE
	VAL_BYTES
	VAL_CHANNEL
	VAL_WAITGROUP
)

type Value struct {
	Type    ValueType
	AsBool  bool
	AsInt   int64
	AsFloat float64
	Obj     interface{} // Heap allocated object
}

type ParamInfo struct {
	IsRef bool
}

type ObjFunction struct {
	Name         string
	Arity        int
	UpvalueCount int // Added for Closures
	Params       []ParamInfo
	Chunk        interface{}
	Globals      map[string]Value // Module/Context globals
}

type ObjUpvalue struct {
	Location *Value // Pointer to stack variable or Closed field
	Closed   Value  // The closed value
	Next     *ObjUpvalue
}

type ObjClosure struct {
	Function *ObjFunction
	Upvalues []*ObjUpvalue
	Globals  map[string]Value // Context globals
}

func (oc *ObjClosure) String() string {
	return fmt.Sprintf("<fn %s>", oc.Function.Name)
}

func (oc *ObjClosure) Format(f fmt.State, verb rune) {
	fmt.Fprint(f, oc.String())
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

func (oa *ObjArray) Format(f fmt.State, verb rune) {
	switch verb {
	case 'T':
		fmt.Fprint(f, "array")
	case 's', 'v':
		fmt.Fprint(f, oa.String())
	default:
		fmt.Fprintf(f, "%%!%c(*ObjArray=%s)", verb, oa.String())
	}
}

type ObjMap struct {
	Data map[interface{}]Value
}

func (om *ObjMap) String() string {
	s := "{"
	i := 0
	for k, v := range om.Data {
		s += fmt.Sprintf("%v: %s", k, v.String())
		if i < len(om.Data)-1 {
			s += ", "
		}
		i++
	}
	s += "}"
	return s
}

func (om *ObjMap) Format(f fmt.State, verb rune) {
	switch verb {
	case 'T':
		fmt.Fprint(f, "map")
	case 's', 'v':
		fmt.Fprint(f, om.String())
	default:
		fmt.Fprintf(f, "%%!%c(*ObjMap=%s)", verb, om.String())
	}
}

type ObjStruct struct {
	Name   string
	Fields []string
}

func (os *ObjStruct) String() string {
	return fmt.Sprintf("<struct %s>", os.Name)
}

func (os *ObjStruct) Format(f fmt.State, verb rune) {
	switch verb {
	case 'T':
		fmt.Fprint(f, "struct definition") // Or just "struct"
	case 's', 'v':
		fmt.Fprint(f, os.String())
	default:
		fmt.Fprintf(f, "%%!%c(*ObjStruct=%s)", verb, os.String())
	}
}

type ObjInstance struct {
	Struct *ObjStruct
	Fields map[string]Value
}

func (oi *ObjInstance) String() string {
	return fmt.Sprintf("<%s instance>", oi.Struct.Name)
}

func (oi *ObjInstance) Format(f fmt.State, verb rune) {
	switch verb {
	case 'T':
		fmt.Fprint(f, oi.Struct.Name)
	case 's', 'v':
		fmt.Fprint(f, oi.String())
	default:
		fmt.Fprintf(f, "%%!%c(*ObjInstance=%s)", verb, oi.String())
	}
}

type ObjChannel struct {
	Chan   chan Value
	Closed bool
	Lock   sync.Mutex
}

func (oc *ObjChannel) String() string {
	return fmt.Sprintf("<channel %p>", oc.Chan)
}

func (oc *ObjChannel) Format(f fmt.State, verb rune) {
	switch verb {
	case 'T':
		fmt.Fprint(f, "channel")
	case 's', 'v':
		fmt.Fprint(f, oc.String())
	default:
		fmt.Fprintf(f, "%%!%c(*ObjChannel=%s)", verb, oc.String())
	}
}

type ObjWaitGroup struct {
	Wg *sync.WaitGroup
}

func (ow *ObjWaitGroup) String() string {
	return fmt.Sprintf("<waitgroup %p>", ow.Wg)
}

func (ow *ObjWaitGroup) Format(f fmt.State, verb rune) {
	switch verb {
	case 'T':
		fmt.Fprint(f, "waitgroup")
	case 's', 'v':
		fmt.Fprint(f, ow.String())
	default:
		fmt.Fprintf(f, "%%!%c(*ObjWaitGroup=%s)", verb, ow.String())
	}
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
		case *ObjMap:
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
		// Check if it's ObjFunction or ObjClosure (if we share tag)
		if fn, ok := v.Obj.(*ObjFunction); ok {
			return fmt.Sprintf("<fn %s>", fn.Name)
		}
		if cl, ok := v.Obj.(*ObjClosure); ok {
			return fmt.Sprintf("<fn %s>", cl.Function.Name)
		}
		return "<fn unknown>"
	case VAL_NATIVE:
		return fmt.Sprintf("<native fn %s>", v.Obj.(*ObjNative).Name)
	case VAL_BYTES:
		return fmt.Sprintf("b\"%s\"", v.Obj.(string))
	case VAL_CHANNEL:
		return v.Obj.(*ObjChannel).String()
	case VAL_WAITGROUP:
		return v.Obj.(*ObjWaitGroup).String()
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

func NewMap() Value {
	return Value{Type: VAL_OBJ, Obj: &ObjMap{Data: make(map[interface{}]Value)}}
}

func NewMapWithData(data map[string]Value) Value {
	m := make(map[interface{}]Value)
	for k, v := range data {
		m[k] = v
	}
	return Value{Type: VAL_OBJ, Obj: &ObjMap{Data: m}}
}

func NewStruct(name string, fields []string) Value {
	return Value{Type: VAL_OBJ, Obj: &ObjStruct{Name: name, Fields: fields}}
}

func NewInstance(def *ObjStruct) Value {
	return Value{Type: VAL_OBJ, Obj: &ObjInstance{Struct: def, Fields: make(map[string]Value)}}
}

func NewFunction(name string, arity int, upvalueCount int, params []ParamInfo, chunk interface{}, globals map[string]Value) Value {
	return Value{
		Type: VAL_FUNCTION,
		Obj:  &ObjFunction{Name: name, Arity: arity, UpvalueCount: upvalueCount, Params: params, Chunk: chunk, Globals: globals},
	}
}

func NewClosure(fn *ObjFunction) Value {
	return Value{
		Type: VAL_FUNCTION, // Reuse VAL_FUNCTION to mean "Callable" (VM will assume ObjClosure or handle translation?)
		// Wait, if I reuse VAL_FUNCTION, then Obj must be ObjClosure wrapper.
		// But existing code expects ObjFunction.
		// If I strictly wrap, I must update all casts.
		// Alternatively, VAL_FUNCTION remains ObjFunction, and VAL_CLOSURE is new.
		// But closures ARE the main way functions exist at runtime.
		// Let's use VAL_CLOSURE for runtime closures.
		// VAL_FUNCTION remains for compile-time constants (prototypes).
		// When VM executes call, it expects CLOSURE? Or Both?
		// Both is messy.
		// Better: VM expects VAL_CLOSURE.
		// When loading a function constant, we wrap it in a Closure if it has 0 upvalues?
		// Or OP_CLOSURE instruction creates the closure.
		// So runtime stack always has VAL_CLOSURE for user functions.
		// 'spawn' expects function. Should handle closure.
		Obj: &ObjClosure{Function: fn, Upvalues: make([]*ObjUpvalue, fn.UpvalueCount)},
	}
}

// Note: If I reuse VAL_FUNCTION tag for ObjClosure, I MUST update all `v.Obj.(*ObjFunction)` casts.
// This is a big refactor.
// To minimize diff, I can keep VAL_FUNCTION as ObjFunction and add VAL_CLOSURE.
// But calling logic needs to handle both or just one.
// Let's TRY to add VAL_CLOSURE tag.

func NewNative(name string, fn NativeFunc) Value {
	return Value{
		Type: VAL_NATIVE,
		Obj:  &ObjNative{Name: name, Fn: fn},
	}
}

func NewBytes(v string) Value {
	return Value{Type: VAL_BYTES, Obj: v}
}

func NewChannel(size int) Value {
	return Value{Type: VAL_CHANNEL, Obj: &ObjChannel{Chan: make(chan Value, size)}}
}

type BytesWrapper struct {
	Str string
}

func NewWaitGroup() Value {
	return Value{Type: VAL_WAITGROUP, Obj: &ObjWaitGroup{Wg: &sync.WaitGroup{}}}
}

func (bw BytesWrapper) Format(f fmt.State, verb rune) {
	switch verb {
	case 'T':
		fmt.Fprint(f, "bytes")
	case 's', 'v':
		fmt.Fprint(f, bw.Str)
	case 'q':
		fmt.Fprintf(f, "%q", bw.Str)
	case 'x':
		fmt.Fprintf(f, "%x", bw.Str)
	default:
		fmt.Fprintf(f, "%%!%c(bytes=%s)", verb, bw.Str)
	}
}

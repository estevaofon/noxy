package value

import (
	"fmt"
	"strings"
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
	VAL_REF
)

type Value struct {
	Type    ValueType
	AsBool  bool
	AsInt   int64
	AsFloat float64
	Obj     interface{} // Heap allocated object
}

type ParamInfo struct {
	IsRef    bool
	TypeName string
}

type NativeSignature struct {
	Arity      int
	Variadic   bool
	Params     []ParamInfo
	ReturnType string
}

type RuntimeTypeKind uint8

const (
	TYPE_ANY RuntimeTypeKind = iota
	TYPE_NULL
	TYPE_BOOL
	TYPE_INT
	TYPE_FLOAT
	TYPE_STRING
	TYPE_BYTES
	TYPE_ARRAY
	TYPE_MAP
	TYPE_REF
	TYPE_STRUCT
	TYPE_VOID
	TYPE_CALLABLE
	TYPE_CHANNEL
)

// RuntimeTypeInfo is narrow metadata used at typed native boundaries and on
// callable/channel objects whose runtime shape alone cannot prove their static
// type. It is not a new parameter mode and does not affect Value identity.
type RuntimeTypeInfo struct {
	Kind         RuntimeTypeKind
	Name         string
	Element      *RuntimeTypeInfo
	Key          *RuntimeTypeInfo
	Value        *RuntimeTypeInfo
	Fields       map[string]*RuntimeTypeInfo
	Params       []*RuntimeTypeInfo
	ParamIsRef   []bool
	Return       *RuntimeTypeInfo
	CallableBare bool
}

func (t *RuntimeTypeInfo) String() string {
	if t == nil {
		return "unknown"
	}
	switch t.Kind {
	case TYPE_ANY:
		return "any"
	case TYPE_NULL:
		return "null"
	case TYPE_BOOL:
		return "bool"
	case TYPE_INT:
		return "int"
	case TYPE_FLOAT:
		return "float"
	case TYPE_STRING:
		return "string"
	case TYPE_BYTES:
		return "bytes"
	case TYPE_ARRAY:
		return t.Element.String() + "[]"
	case TYPE_MAP:
		return "map[" + t.Key.String() + ", " + t.Value.String() + "]"
	case TYPE_REF:
		return "ref " + t.Element.String()
	case TYPE_STRUCT:
		return t.Name
	case TYPE_VOID:
		return "void"
	case TYPE_CALLABLE:
		if t.CallableBare {
			return "func"
		}
		params := make([]string, len(t.Params))
		for i, param := range t.Params {
			params[i] = param.String()
		}
		return "func(" + strings.Join(params, ", ") + ") -> " + t.Return.String()
	case TYPE_CHANNEL:
		return "chan " + t.Element.String()
	default:
		return "unknown"
	}
}

type ObjFunction struct {
	Name         string
	Arity        int
	UpvalueCount int // Added for Closures
	Params       []ParamInfo
	Chunk        interface{}
	Globals      map[string]Value // Module/Context globals
	RuntimeType  *RuntimeTypeInfo
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
	Name      string
	Fn        NativeFunc
	Signature *NativeSignature
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
	Name              string
	Fields            []string
	JSONDynamicFields map[string]bool
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
	Chan        chan Value
	Closed      bool
	Lock        sync.Mutex
	ElementType *RuntimeTypeInfo
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

type RefType int

const (
	REF_PTR RefType = iota
	REF_GLOBAL
	REF_PROPERTY
	REF_INDEX
	REF_UPVALUE // New RefType for captured locals
)

type ObjRef struct {
	RefType     RefType
	JSONDynamic bool // Declared any target: JSON may replace its concrete runtime type.
	TargetType  *RuntimeTypeInfo
	Name        string // For Global or Property Name
	GlobalOwner *map[string]Value
	Ptr         *Value      // For Local (unsafe if escapes)
	Upvalue     *ObjUpvalue // For Local (safe, captured)
	Container   Value       // For Property/Index (Object, Array, Map)
	Index       Value       // For Index
}

func (or *ObjRef) String() string {
	if or == nil {
		return "<invalid reference>"
	}
	switch or.RefType {
	case REF_GLOBAL:
		return fmt.Sprintf("<ref global %s>", or.Name)
	case REF_UPVALUE:
		return fmt.Sprintf("<ref upvalue %p>", or.Upvalue)
	case REF_PROPERTY:
		return fmt.Sprintf("<ref prop %s>", or.Name)
	case REF_INDEX:
		switch or.Index.Type {
		case VAL_INT:
			return fmt.Sprintf("<ref index %d>", or.Index.AsInt)
		case VAL_OBJ:
			if key, ok := or.Index.Obj.(string); ok {
				return fmt.Sprintf("<ref index %s>", key)
			}
		}
		return "<invalid reference>"
	default:
		return fmt.Sprintf("<ref %p>", or.Ptr)
	}
}

func (or *ObjRef) Format(f fmt.State, verb rune) {
	switch verb {
	case 'T':
		fmt.Fprint(f, "reference")
	case 's', 'v':
		fmt.Fprint(f, or.String())
	default:
		fmt.Fprintf(f, "%%!%c(*ObjRef=%s)", verb, or.String())
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
	case VAL_REF:
		ref, ok := v.Obj.(*ObjRef)
		if !ok || ref == nil {
			return "<invalid reference>"
		}
		return ref.String()
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

func NewRuntimeTypeInfo(v *RuntimeTypeInfo) Value {
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
		Obj:  &ObjClosure{Function: fn, Upvalues: make([]*ObjUpvalue, fn.UpvalueCount)},
	}
}

func NewNative(name string, fn NativeFunc) Value {
	return Value{
		Type: VAL_NATIVE,
		Obj:  &ObjNative{Name: name, Fn: fn, Signature: nil},
	}
}

func NewNativeWithSignature(name string, signature NativeSignature, fn NativeFunc) Value {
	return Value{
		Type: VAL_NATIVE,
		Obj:  &ObjNative{Name: name, Fn: fn, Signature: &signature},
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

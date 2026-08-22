package value

import (
	"fmt"
	"strings"
	"testing"
)

// Format/String dos objetos do runtime: é o que `%v`/`%s` no `fmt` builtin e
// os dumps de depuração mostram. Nenhum verbo além de %s passava por teste; o
// contrato é: %s/%v dão String(), qualquer outro verbo dá a marca
// "%!verbo(tipo=...)" no estilo do pacote fmt. (O %T do builtin `fmt` não
// chega a Format: é trocado por runtimeTypeName antes — ver builtins_core.go.)

func TestObjectFormatVerbs(t *testing.T) {
	def := NewStruct("P", []string{"x"}).Obj.(*ObjStruct)
	cases := []struct {
		name   string
		obj    any
		text   string
		badFmt string
	}{
		{"array", NewArray([]Value{NewInt(1)}).Obj.(*ObjArray), "[1]", "%!d(*ObjArray=[1])"},
		{"map", NewMap().Obj.(*ObjMap), "{}", "%!d(*ObjMap={})"},
		{"struct definition", def, "<struct P>", "%!d(*ObjStruct=<struct P>)"},
		{"instance", NewInstance(def).Obj.(*ObjInstance), "<P instance>", "%!d(*ObjInstance=<P instance>)"},
		{"reference", &ObjRef{RefType: REF_GLOBAL, Name: "g"}, "<ref global g>", "%!d(*ObjRef=<ref global g>)"},
		{"bytes", BytesWrapper{Str: "ab"}, "ab", "%!d(bytes=ab)"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := fmt.Sprintf("%s|%v", tc.obj, tc.obj); got != tc.text+"|"+tc.text {
				t.Fatalf("%%s|%%v = %q, want %q", got, tc.text+"|"+tc.text)
			}
			if got := fmt.Sprintf("%d", tc.obj); got != tc.badFmt {
				t.Fatalf("%%d = %q, want %q", got, tc.badFmt)
			}
			// %T nunca chega a Format: o pacote fmt resolve o verbo antes de
			// consultar Formatter e imprime o tipo Go — o nome Noxy vem de
			// runtimeTypeName no builtin `fmt` (issue #61 item 4: os antigos
			// `case 'T'` eram codigo morto).
			if got := fmt.Sprintf("%T", tc.obj); !strings.HasPrefix(got, "*value.") && !strings.HasPrefix(got, "value.") {
				t.Fatalf("%%T = %q, want o tipo Go (Format nao intercepta %%T)", got)
			}
		})
	}
	if got := fmt.Sprintf("%q|%x", BytesWrapper{Str: "ab"}, BytesWrapper{Str: "ab"}); got != "\"ab\"|6162" {
		t.Fatalf("bytes %%q|%%x = %q", got)
	}
	channel := NewChannel(1).Obj.(*ObjChannel)
	if got := fmt.Sprintf("%v", channel); !strings.HasPrefix(got, "<channel 0x") {
		t.Fatalf("channel %%v = %q", got)
	}
	if got := fmt.Sprintf("%d", channel); !strings.HasPrefix(got, "%!d(*ObjChannel=<channel ") {
		t.Fatalf("channel %%d = %q", got)
	}
	wg := NewWaitGroup().Obj.(*ObjWaitGroup)
	if got := fmt.Sprintf("%s", wg); !strings.HasPrefix(got, "<waitgroup 0x") {
		t.Fatalf("waitgroup %%s = %q", got)
	}
	if got := fmt.Sprintf("%d", wg); !strings.HasPrefix(got, "%!d(*ObjWaitGroup=") {
		t.Fatalf("waitgroup %%d = %q", got)
	}
	closure := &ObjClosure{Function: &ObjFunction{Name: "f"}}
	if got := fmt.Sprintf("%v|%s", closure, closure.String()); got != "<fn f>|<fn f>" {
		t.Fatalf("closure = %q", got)
	}
}

func TestObjRefStringByKind(t *testing.T) {
	var nilRef *ObjRef
	if nilRef.String() != "<invalid reference>" {
		t.Fatalf("ref nil: %q", nilRef.String())
	}
	cases := []struct {
		name string
		ref  *ObjRef
		want string
	}{
		{"global", &ObjRef{RefType: REF_GLOBAL, Name: "g"}, "<ref global g>"},
		{"property", &ObjRef{RefType: REF_PROPERTY, Name: "x"}, "<ref prop x>"},
		{"int index", &ObjRef{RefType: REF_INDEX, Index: NewInt(3)}, "<ref index 3>"},
		{"string index", &ObjRef{RefType: REF_INDEX, Index: NewString("k")}, "<ref index k>"},
		{"other index", &ObjRef{RefType: REF_INDEX, Index: NewFloat(1.5)}, "<invalid reference>"},
	}
	for _, tc := range cases {
		if got := tc.ref.String(); got != tc.want {
			t.Fatalf("%s: %q, want %q", tc.name, got, tc.want)
		}
	}
	x := NewInt(1)
	if got := (&ObjRef{RefType: REF_PTR, Ptr: &x}).String(); !strings.HasPrefix(got, "<ref 0x") {
		t.Fatalf("ref ptr: %q", got)
	}
	if got := (&ObjRef{RefType: REF_UPVALUE, Upvalue: NewClosedUpvalue(x)}).String(); !strings.HasPrefix(got, "<ref upvalue 0x") {
		t.Fatalf("ref upvalue: %q", got)
	}
}

func TestValueStringForRuntimeOnlyKinds(t *testing.T) {
	if got := NewChannel(0).String(); !strings.HasPrefix(got, "<channel 0x") {
		t.Fatalf("channel: %q", got)
	}
	if got := NewWaitGroup().String(); !strings.HasPrefix(got, "<waitgroup 0x") {
		t.Fatalf("waitgroup: %q", got)
	}
	fn := &ObjFunction{Name: "f", UpvalueCount: 0}
	if got := NewClosure(fn).String(); got != "<fn f>" {
		t.Fatalf("closure value: %q", got)
	}
	if got := (Value{Type: VAL_FUNCTION, Obj: "nope"}).String(); got != "<fn unknown>" {
		t.Fatalf("função com Obj estranho: %q", got)
	}
	if got := (Value{Type: VAL_TASK}).String(); got != "<invalid task>" {
		t.Fatalf("task nula: %q", got)
	}
	if got := (Value{Type: ValueType(200)}).String(); got != "unknown" {
		t.Fatalf("tipo desconhecido: %q", got)
	}
}

func TestRuntimeTypeInfoString(t *testing.T) {
	var nilInfo *RuntimeTypeInfo
	if nilInfo.String() != "unknown" {
		t.Fatalf("nil: %q", nilInfo.String())
	}
	intInfo := &RuntimeTypeInfo{Kind: TYPE_INT}
	cases := []struct {
		info *RuntimeTypeInfo
		want string
	}{
		{&RuntimeTypeInfo{Kind: TYPE_ANY}, "any"},
		{&RuntimeTypeInfo{Kind: TYPE_NULL}, "null"},
		{&RuntimeTypeInfo{Kind: TYPE_BOOL}, "bool"},
		{intInfo, "int"},
		{&RuntimeTypeInfo{Kind: TYPE_FLOAT}, "float"},
		{&RuntimeTypeInfo{Kind: TYPE_STRING}, "string"},
		{&RuntimeTypeInfo{Kind: TYPE_BYTES}, "bytes"},
		{&RuntimeTypeInfo{Kind: TYPE_VOID}, "void"},
		{&RuntimeTypeInfo{Kind: TYPE_CALLABLE, CallableBare: true}, "func"},
		{&RuntimeTypeInfo{Kind: TYPE_CHANNEL, Element: intInfo}, "chan int"},
		{&RuntimeTypeInfo{Kind: TYPE_STRUCT, Name: "P"}, "P"},
		{&RuntimeTypeInfo{Kind: RuntimeTypeKind(99)}, "unknown"},
	}
	for _, tc := range cases {
		if got := tc.info.String(); got != tc.want {
			t.Fatalf("kind %v: %q, want %q", tc.info.Kind, got, tc.want)
		}
	}
}

// Os métodos de ObjUpvalue toleram receptor nil (devolvem zero) e, numa
// caixa aberta, Store/Load/PointsTo/LocationAddress enxergam o slot.
func TestUpvalueNilReceiverAndOpenCellBasics(t *testing.T) {
	var nilUp *ObjUpvalue
	nilUp.MarkBorrowed()
	nilUp.SetNext(nil)
	if nilUp.IsBorrowed() || nilUp.PointsTo(nil) || nilUp.Store(NewInt(1)) || nilUp.Close(nil) || nilUp.Next() != nil {
		t.Fatal("métodos em upvalue nil deveriam devolver zero")
	}
	if addr, ok := nilUp.LocationAddress(); ok || addr != "" {
		t.Fatalf("LocationAddress em nil: %q %v", addr, ok)
	}
	slot := NewInt(1)
	open := NewOpenUpvalue(&slot, nil)
	if !open.PointsTo(&slot) || !open.Store(NewInt(2)) || slot.AsInt != 2 {
		t.Fatal("upvalue aberto deveria apontar e escrever no slot")
	}
	if addr, ok := open.LocationAddress(); !ok || !strings.HasPrefix(addr, "0x") {
		t.Fatalf("LocationAddress aberto: %q %v", addr, ok)
	}
	closed := NewClosedUpvalue(NewInt(7))
	if v, ok := closed.Load(); !ok || v.AsInt != 7 {
		t.Fatalf("upvalue fechado: %v %v", v, ok)
	}
}

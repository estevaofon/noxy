package value

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"unsafe"
)

type ValueType uint8

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
	VAL_TASK
)

// objKind e a dica que os construtores de internal/value carimbam em Value
// para ownersOf (cow.go) decidir sem type switch. Zero = "desconhecido":
// um Value{Type: VAL_OBJ, Obj: x} montado fora dos construtores cai no
// caminho lento (o type switch de sempre) e continua correto — a dica so
// acelera, nunca decide sozinha.
type objKind uint8

const (
	objKindUnknown  objKind = iota
	objKindNoOwners         // string, *ObjStruct, *RuntimeTypeInfo: VAL_OBJ sem contador
	objKindArray
	objKindMap
	objKindInstance
)

// Value e o operando universal da VM: 32 bytes (fase 2 de perf, issue #37;
// eram 48 com tag int, bool com padding e int64/float64 em campos separados).
// Type e a tag; num guarda int64 (VAL_INT) ou os bits de um float64
// (VAL_FLOAT); b guarda VAL_BOOL; Obj, o objeto alocado dos demais tipos.
// Leia pelos acessores Int()/Float()/Bool() — ler o campo errado para a tag
// devolve lixo (num e compartilhado entre int e float), nunca zero.
// layout_test.go trava o tamanho.
type Value struct {
	Type ValueType
	kind objKind
	b    bool
	num  uint64
	Obj  interface{} // Heap allocated object
}

// Int devolve o inteiro de um VAL_INT. Em qualquer outra tag o resultado e
// indefinido (os bits de num) — o chamador garante a tag.
func (v Value) Int() int64 { return int64(v.num) }

// Float devolve o float de um VAL_FLOAT (bits em num).
func (v Value) Float() float64 { return math.Float64frombits(v.num) }

// Bool devolve o valor de um VAL_BOOL.
func (v Value) Bool() bool { return v.b }

// SetInt grava o inteiro no lugar, sem tocar na tag — e o `AsInt +=` de
// OP_INC_LOCAL_INT (8 bytes escritos em vez dos 32 de um Value novo).
func (v *Value) SetInt(n int64) { v.num = uint64(n) }

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
	Size         int
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
		element := t.Element.String()
		if t.Element != nil && t.Element.Kind == TYPE_CALLABLE && !t.Element.CallableBare {
			element = "(" + element + ")"
		}
		if t.Size > 0 {
			return fmt.Sprintf("%s[%d]", element, t.Size)
		}
		return element + "[]"
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
	Environment  *GlobalEnvironment
	RuntimeType  *RuntimeTypeInfo
	// ParamsUntracked: nenhum parametro pode carregar contador RC (todos sem
	// `ref` e de tipo int/float/bool/string/bytes — Retain e no-op para eles).
	// Calculado pelo compilador; autoriza o fast path de OP_CALL_STATIC a
	// montar o frame sem o laco ownSlot (perf issue #66, item 3).
	ParamsUntracked bool
}

type ObjUpvalue struct {
	mu       sync.RWMutex
	location *Value
	closed   Value
	next     *ObjUpvalue
	// borrowed marca que a caixa foi aberta sobre um slot EMPRESTADO (slot de
	// tipo `ref`, que nunca é dono durável — spec §4.2). Uma caixa emprestada
	// não retém o que guarda, então os funis de escrita que trocam o conteúdo
	// (OP_SET_UPVALUE, OP_GET_UPVALUE_MUT) também não podem soltar o valor
	// velho: soltar o que nunca se reteve é dec a menos e faria o objeto
	// parecer único, mutando no lugar algo compartilhado.
	borrowed bool
}

func NewOpenUpvalue(location *Value, next *ObjUpvalue) *ObjUpvalue {
	return &ObjUpvalue{location: location, next: next}
}

// NewClosedUpvalue cria uma caixa ja fechada sobre um valor que nunca morou
// em slot de pilha — a "variavel anonima na heap" que json_loads usa para
// preencher um slot `ref T` nulo (spec 2026-08-20-ref-slot-invariant, §5.2):
// o analogo exato de `let novo: T = ...; slot = ref novo` depois que o frame
// fecha. A caixa e possuidora (nao emprestada); o chamador retem o valor em
// nome dela. PointsTo(slot de pilha) e sempre falso, entao retargetOwnedSlot
// a ignora.
func NewClosedUpvalue(v Value) *ObjUpvalue {
	upvalue := &ObjUpvalue{closed: v}
	upvalue.location = &upvalue.closed
	return upvalue
}

// MarkBorrowed registra que a caixa empresta (não possui) o que guarda. É
// idempotente: capturas repetidas do mesmo slot chegam à mesma conclusão.
func (upvalue *ObjUpvalue) MarkBorrowed() {
	if upvalue == nil {
		return
	}
	upvalue.mu.Lock()
	defer upvalue.mu.Unlock()
	upvalue.borrowed = true
}

// IsBorrowed informa se a caixa empresta o valor em vez de possuí-lo.
func (upvalue *ObjUpvalue) IsBorrowed() bool {
	if upvalue == nil {
		return false
	}
	upvalue.mu.RLock()
	defer upvalue.mu.RUnlock()
	return upvalue.borrowed
}

func (upvalue *ObjUpvalue) IsValid() bool {
	if upvalue == nil {
		return false
	}
	upvalue.mu.RLock()
	defer upvalue.mu.RUnlock()
	return upvalue.location != nil
}

func (upvalue *ObjUpvalue) PointsTo(location *Value) bool {
	if upvalue == nil || location == nil {
		return false
	}
	upvalue.mu.RLock()
	defer upvalue.mu.RUnlock()
	return upvalue.location == location
}

func (upvalue *ObjUpvalue) Load() (Value, bool) {
	if upvalue == nil {
		return Value{}, false
	}
	upvalue.mu.RLock()
	defer upvalue.mu.RUnlock()
	if upvalue.location == nil {
		return Value{}, false
	}
	return *upvalue.location, true
}

func (upvalue *ObjUpvalue) Store(updated Value) bool {
	if upvalue == nil {
		return false
	}
	upvalue.mu.Lock()
	defer upvalue.mu.Unlock()
	if upvalue.location == nil {
		return false
	}
	*upvalue.location = updated
	return true
}

func (upvalue *ObjUpvalue) Close(location *Value) bool {
	if upvalue == nil || location == nil {
		return false
	}
	upvalue.mu.Lock()
	defer upvalue.mu.Unlock()
	if upvalue.location != location {
		return false
	}
	upvalue.closed = *location
	upvalue.location = &upvalue.closed
	return true
}

// RelocateOpenUpvalues migra as caixas ABERTAS de old para grown quando a
// pilha do VM e realocada: copia o conteudo e reaponta cada location para o
// MESMO indice no array novo. Caixa fechada (location aponta para closed) ou
// que nao aponta para dentro de old fica como esta.
//
// A COPIA acontece AQUI DENTRO, com todas as caixas travadas, e nao no
// chamador: uma task roda em VM proprio mas escreve por Store() em caixas
// cujo location aponta para a pilha do VM que a criou (OP_SET_UPVALUE e
// companhia). Um Store que caisse entre o `copy` e o reaponte daquela caixa
// gravaria no array MORTO e a escrita se perderia — travar tudo antes de
// copiar fecha essa janela.
//
// Percorre a lista pelo campo `next` DIRETAMENTE, lido sob o lock da propria
// caixa, e nunca por Next(): Next() toma RLock e travaria contra o mu.Lock
// que ja seguramos na mesma caixa. As listas de VMs distintos sao disjuntas
// (cada captureUpvalue so cria caixas sobre a pilha do seu proprio VM), entao
// segurar varios locks de uma vez nao cria ciclo de ordenacao.
//
// Assume GC nao-movel (o Go atual): as comparacoes de endereco abaixo so
// valem porque `old` nao se move enquanto esta funcao roda.
func RelocateOpenUpvalues(head *ObjUpvalue, old, grown []Value) {
	locked := make([]*ObjUpvalue, 0, 8)
	for upvalue := head; upvalue != nil; {
		upvalue.mu.Lock()
		locked = append(locked, upvalue)
		upvalue = upvalue.next
	}
	defer func() {
		for i := len(locked) - 1; i >= 0; i-- {
			locked[i].mu.Unlock()
		}
	}()

	copy(grown, old)
	if len(old) == 0 || len(grown) < len(old) {
		return
	}
	base := uintptr(unsafe.Pointer(&old[0]))
	size := unsafe.Sizeof(Value{})
	limit := base + uintptr(len(old))*size
	for _, upvalue := range locked {
		addr := uintptr(unsafe.Pointer(upvalue.location))
		if addr < base || addr >= limit {
			continue
		}
		upvalue.location = &grown[(addr-base)/size]
	}
}

func (upvalue *ObjUpvalue) Next() *ObjUpvalue {
	if upvalue == nil {
		return nil
	}
	upvalue.mu.RLock()
	defer upvalue.mu.RUnlock()
	return upvalue.next
}

func (upvalue *ObjUpvalue) SetNext(next *ObjUpvalue) {
	if upvalue == nil {
		return
	}
	upvalue.mu.Lock()
	defer upvalue.mu.Unlock()
	upvalue.next = next
}

func (upvalue *ObjUpvalue) LocationAddress() (string, bool) {
	if upvalue == nil {
		return "", false
	}
	upvalue.mu.RLock()
	defer upvalue.mu.RUnlock()
	if upvalue.location == nil {
		return "", false
	}
	return fmt.Sprintf("%p", upvalue.location), true
}

type ObjClosure struct {
	Function    *ObjFunction
	Upvalues    []*ObjUpvalue
	Environment *GlobalEnvironment
}

func (oc *ObjClosure) String() string {
	return fmt.Sprintf("<fn %s>", oc.Function.Name)
}

func (oc *ObjClosure) Format(f fmt.State, verb rune) {
	fmt.Fprint(f, oc.String())
}

// ObjHeader e o prefixo comum dos compostos que o RC rastreia (array, map,
// instancia). Owners conta referencias duraveis (RC-uniqueness, spec
// 2026-08-17) e e a unica fonte de unicidade — o antigo bit sticky Shared foi
// removido na Task 8. TEM de ser o primeiro campo das tres structs (offset 0
// — layout_test.go trava): e o que um estagio 3 com unsafe.Pointer usaria
// para alcancar o contador sem o tipo concreto (issue #37).
type ObjHeader struct {
	Owners atomic.Int32
}

type ObjArray struct {
	ObjHeader
	Elements    []Value
	RuntimeType atomic.Pointer[RuntimeTypeInfo]
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
	case 's', 'v':
		fmt.Fprint(f, oa.String())
	default:
		fmt.Fprintf(f, "%%!%c(*ObjArray=%s)", verb, oa.String())
	}
}

type ObjMap struct {
	ObjHeader
	store       *bindingStore
	storeOnce   sync.Once
	RuntimeType atomic.Pointer[RuntimeTypeInfo]
}

func (om *ObjMap) String() string {
	s := "{"
	i := 0
	values := om.Snapshot()
	for k, v := range values {
		s += fmt.Sprintf("%v: %s", k, v.String())
		if i < len(values)-1 {
			s += ", "
		}
		i++
	}
	s += "}"
	return s
}

func (om *ObjMap) Format(f fmt.State, verb rune) {
	switch verb {
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
	// RefFields marca os campos declarados `ref T`. E a fonte unica de
	// runtime para a pergunta "este slot e ref?" (OP_REF_PROPERTY,
	// OP_SET_PROPERTY — spec 2026-08-20-ref-slot-invariant §6.1): O(1) por
	// nome e presente tambem nos structs que o builder JSON cria sem
	// ConstructorType. Nil quando o struct nao tem campo ref (lookup em mapa
	// nil e valido e barato).
	RefFields       map[string]bool
	ConstructorType *RuntimeTypeInfo
	// index e nome -> posicao do slot em ObjInstance.Slots (a ordem de
	// Fields). Construido UMA vez por NewStruct/BuildFieldIndex e nunca mais
	// escrito: e por isso que pode ser lido por N routines sem sincronizacao
	// — a unica escrita concorrente que existia era no map por INSTANCIA
	// (issue #86), que deixou de existir. Nil (literal montado a mao em
	// teste) cai em varredura linear, correta e so mais lenta.
	index map[string]int
}

// FieldIsRef informa se o campo foi declarado `ref T` (nil-safe).
func (os *ObjStruct) FieldIsRef(name string) bool {
	return os != nil && os.RefFields[name]
}

// BuildFieldIndex (re)constroi o indice nome -> slot a partir de Fields.
// Chamar depois de fechar a lista de campos e ANTES de a definicao ser
// publicada para outra routine.
func (os *ObjStruct) BuildFieldIndex() {
	index := make(map[string]int, len(os.Fields))
	for i, field := range os.Fields {
		index[field] = i
	}
	os.index = index
}

// FieldIndex devolve o slot do campo DECLARADO name (nil-safe). E a fonte de
// runtime para "este nome existe na declaracao?" (issue #61 item 2: escrita
// via `any` num campo inexistente criava a propriedade em silencio) e o
// caminho QUENTE de OP_GET_PROPERTY/OP_SET_PROPERTY: um lookup num map
// so-leitura, o mesmo custo do lookup em instance.Fields que existia antes.
func (os *ObjStruct) FieldIndex(name string) (int, bool) {
	if os == nil {
		return 0, false
	}
	if os.index != nil {
		i, ok := os.index[name]
		return i, ok
	}
	for i, field := range os.Fields {
		if field == name {
			return i, true
		}
	}
	return 0, false
}

// HasField informa se name e um campo DECLARADO do struct (nil-safe).
func (os *ObjStruct) HasField(name string) bool {
	_, ok := os.FieldIndex(name)
	return ok
}

func (os *ObjStruct) String() string {
	return fmt.Sprintf("<struct %s>", os.Name)
}

func (os *ObjStruct) Format(f fmt.State, verb rune) {
	switch verb {
	case 's', 'v':
		fmt.Fprint(f, os.String())
	default:
		fmt.Fprintf(f, "%%!%c(*ObjStruct=%s)", verb, os.String())
	}
}

// ObjInstance guarda os campos em Slots, um por campo declarado, na ordem de
// Struct.Fields — nao num map por instancia. Foi o map que a issue #86
// derrubou: duas routines alcancando a mesma instancia (por `ref`, global,
// upvalue ou campo `ref` dentro de um valor — #92) batiam em
// `map[string]Value` cru e o runtime Go morria com `fatal error: concurrent
// map read and map write`, irrecuperavel. Com slice, struct fica no mesmo
// status de ObjArray.Elements que concurrency.md ja documenta: uma corrida
// continua sendo corrida (valor rasgado e possivel; coordene), mas nunca
// derruba o processo. Slot nao preenchido e null (struct e nominal e de
// campos fixos, spec §5; so um native em Go deixa slot por preencher).
type ObjInstance struct {
	ObjHeader
	Struct *ObjStruct
	Slots  []Value
}

// Get le o campo DECLARADO name; ok=false so para nome fora da declaracao.
func (oi *ObjInstance) Get(name string) (Value, bool) {
	i, ok := oi.Struct.FieldIndex(name)
	if !ok || i >= len(oi.Slots) {
		return Value{}, false
	}
	return oi.Slots[i], true
}

// Field le o campo name; null para nome fora da declaracao. Para natives e
// testes, onde o nome e uma constante conhecida.
func (oi *ObjInstance) Field(name string) Value {
	if v, ok := oi.Get(name); ok {
		return v
	}
	return NewNull()
}

// Set escreve o campo DECLARADO name; false para nome fora da declaracao.
// NAO retem nem solta: a disciplina RC (retain-antes-de-release) e do
// chamador, como era com a escrita direta no map.
func (oi *ObjInstance) Set(name string, v Value) bool {
	i, ok := oi.Struct.FieldIndex(name)
	if !ok || i >= len(oi.Slots) {
		return false
	}
	oi.Slots[i] = v
	return true
}

// MustSet e Set para natives que montam instancias de structs que eles
// mesmos declararam: nome fora da declaracao e bug no codigo Go, nao erro do
// programa Noxy — panic com o nome do struct e do campo.
func (oi *ObjInstance) MustSet(name string, v Value) {
	if !oi.Set(name, v) {
		panic(fmt.Sprintf("struct %s has no field %q", oi.Struct.Name, name))
	}
}

// Len e o numero de campos declarados (= slots).
func (oi *ObjInstance) Len() int { return len(oi.Slots) }

// Range percorre os campos em ORDEM DE DECLARACAO.
func (oi *ObjInstance) Range(fn func(name string, v Value)) {
	for i, v := range oi.Slots {
		if i < len(oi.Struct.Fields) {
			fn(oi.Struct.Fields[i], v)
		}
	}
}

// Snapshot copia os campos para um map novo (caminho frio: JSON, testes).
func (oi *ObjInstance) Snapshot() map[string]Value {
	out := make(map[string]Value, len(oi.Slots))
	oi.Range(func(name string, v Value) { out[name] = v })
	return out
}

func (oi *ObjInstance) String() string {
	return fmt.Sprintf("<%s instance>", oi.Struct.Name)
}

func (oi *ObjInstance) Format(f fmt.State, verb rune) {
	switch verb {
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
	JSONDynamic atomic.Bool // Declared any target: JSON may replace its concrete runtime type.
	TargetType  atomic.Pointer[RuntimeTypeInfo]
	Name        string // For Global or Property Name
	GlobalOwner *GlobalEnvironment
	Ptr         *Value      // For Local (unsafe if escapes)
	Upvalue     *ObjUpvalue // For Local (safe, captured)
	Container   Value       // For Property/Index (Object, Array, Map)
	Index       Value       // For Index
	// Base e o LUGAR do contêiner, quando ele é conhecido (issue #83). Um
	// empréstimo — `ref a[i]`, `ref p.x` — não denota um objeto, denota um
	// LUGAR dentro de um composto que o copy-on-write pode bifurcar. Congelar
	// o *ObjArray/*ObjInstance do instante da criação (o campo Container) é o
	// bug: quem copia depois compartilha aquele objeto, e a escrita através do
	// empréstimo vaza para a cópia — ou, se o CoW já bifurcou o caminho, cai
	// num objeto órfão e some.
	//
	// Com Base preenchido (um VAL_REF para o lugar do PAI, recursivamente até
	// um ref de célula — REF_PTR/REF_GLOBAL/REF_UPVALUE, que o CoW não move),
	// a escrita re-resolve o caminho inteiro no momento em que acontece,
	// unicizando e gravando o clone de volta em cada nível. É a mesma caminhada
	// que a família *_MUT já faz para `a[i].x = v`; a diferença é o INSTANTE.
	//
	// Zero (Type != VAL_REF) mantém o comportamento antigo via Container: os
	// ObjRef construídos fora do compilador (natives, JSON, bytecode de teste)
	// não têm lugar de pai para oferecer.
	//
	Base Value
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
			return fmt.Sprintf("<ref index %d>", or.Index.Int())
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
	case 's', 'v':
		fmt.Fprint(f, or.String())
	default:
		fmt.Fprintf(f, "%%!%c(*ObjRef=%s)", verb, or.String())
	}
}

func (v Value) String() string {
	switch v.Type {
	case VAL_BOOL:
		return fmt.Sprintf("%t", v.Bool())
	case VAL_NULL:
		return "null"
	case VAL_INT:
		// Igual a "%d" byte a byte, sem o pp do fmt nem boxing do int —
		// to_str(int) era o maior termo de string_ops (issue #66, item 2).
		return strconv.FormatInt(v.Int(), 10)
	case VAL_FLOAT:
		return fmt.Sprintf("%f", v.Float())
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
	case VAL_TASK:
		task, ok := v.Obj.(*ObjTask)
		if !ok || task == nil {
			return "<invalid task>"
		}
		return task.String()
	default:
		return "unknown"
	}
}

// Helper constructors
func NewInt(v int64) Value {
	return Value{Type: VAL_INT, num: uint64(v)}
}

func NewFloat(v float64) Value {
	return Value{Type: VAL_FLOAT, num: math.Float64bits(v)}
}

func NewBool(v bool) Value {
	return Value{Type: VAL_BOOL, b: v}
}

func NewNull() Value {
	return Value{Type: VAL_NULL}
}

// Os construtores abaixo carimbam a dica `kind` que ownersOf (cow.go) usa
// para achar o contador de donos sem type switch. Fora deste pacote nao ha
// como carimbar (campo nao exportado): um Value{Type: VAL_OBJ, Obj: x}
// montado a mao cai no caminho lento de ownersOf, que continua correto.

func NewString(v string) Value {
	return Value{Type: VAL_OBJ, kind: objKindNoOwners, Obj: v}
}

func NewRuntimeTypeInfo(v *RuntimeTypeInfo) Value {
	return Value{Type: VAL_OBJ, kind: objKindNoOwners, Obj: v}
}

// NewArray cria um array que e DONO DURAVEL de cada elemento composto
// (Retain; no-op em escalares e strings) — a mesma regra de OP_ARRAY no
// executor. Quem ja reteve os elementos em nome do array usa NewArrayAdopting.
func NewArray(elements []Value) Value {
	for _, element := range elements {
		Retain(element)
	}
	return Value{Type: VAL_OBJ, kind: objKindArray, Obj: &ObjArray{Elements: elements}}
}

// NewArrayAdopting cria um array ADOTANDO elementos que o chamador JA reteve
// em nome do array (move): nao retem de novo. Uso restrito aos sites que
// transferem posse — OP_ARRAY (executor.go), copyValue (calls.go) e o merge
// de causes do call_result (builtins_call_result.go); qualquer outro uso
// precisa de comentario `// RC: move` explicando quem reteve.
func NewArrayAdopting(elements []Value) Value {
	return Value{Type: VAL_OBJ, kind: objKindArray, Obj: &ObjArray{Elements: elements}}
}

func NewMap() Value {
	mapping := &ObjMap{store: newBindingStore(nil)}
	mapping.ensureStore()
	return Value{Type: VAL_OBJ, kind: objKindMap, Obj: mapping}
}

// NewMapWithData cria um map que e DONO DURAVEL de cada valor composto
// (Retain; no-op em escalares e strings) — a mesma regra de OP_MAP.
func NewMapWithData(data map[string]Value) Value {
	values := make(map[interface{}]Value, len(data))
	for k, v := range data {
		Retain(v)
		values[k] = v
	}
	mapping := NewMap()
	mapping.Obj.(*ObjMap).Replace(values)
	return mapping
}

func NewStruct(name string, fields []string) Value {
	def := &ObjStruct{Name: name, Fields: fields}
	def.BuildFieldIndex()
	return Value{Type: VAL_OBJ, kind: objKindNoOwners, Obj: def}
}

// newSlots aloca um slot null por campo declarado.
func newSlots(def *ObjStruct) []Value {
	n := 0
	if def != nil {
		n = len(def.Fields)
	}
	slots := make([]Value, n)
	for i := range slots {
		slots[i] = Value{Type: VAL_NULL}
	}
	return slots
}

// NewInstance cria uma instancia com todos os slots null; quem escreve
// compostos nela depois precisa reter a mao (como calls.go:callPreparedValue
// faz) ou usar NewInstanceWith.
func NewInstance(def *ObjStruct) Value {
	return Value{Type: VAL_OBJ, kind: objKindInstance, Obj: &ObjInstance{Struct: def, Slots: newSlots(def)}}
}

// NewInstanceWith cria uma instancia ja com os campos dados, retendo cada
// valor composto — a mesma regra do construtor de struct em bytecode
// (calls.go:callPreparedValue, "campo e dono duravel"). Escalares e strings
// sao no-op em Retain. Nome fora da declaracao e bug do chamador (panic).
func NewInstanceWith(def *ObjStruct, fields map[string]Value) Value {
	inst := NewInstance(def)
	obj := inst.Obj.(*ObjInstance)
	for name, field := range fields {
		Retain(field)
		obj.MustSet(name, field)
	}
	return inst
}

// NewInstanceAdopting cria uma instancia ADOTANDO slots que o chamador JA
// reteve em nome dela (move): nao retem de novo — o analogo de
// NewArrayAdopting. Uso restrito aos sites que transferem posse (o clone CoW
// de instancia em calls.go); qualquer outro uso precisa de comentario
// `// RC: move` explicando quem reteve. len(slots) tem de ser
// len(def.Fields).
func NewInstanceAdopting(def *ObjStruct, slots []Value) Value {
	return Value{Type: VAL_OBJ, kind: objKindInstance, Obj: &ObjInstance{Struct: def, Slots: slots}}
}

func NewFunction(name string, arity int, upvalueCount int, params []ParamInfo, chunk interface{}, environment *GlobalEnvironment) Value {
	return Value{
		Type: VAL_FUNCTION,
		Obj:  &ObjFunction{Name: name, Arity: arity, UpvalueCount: upvalueCount, Params: params, Chunk: chunk, Environment: environment},
	}
}

func NewClosure(fn *ObjFunction) Value {
	return Value{
		Type: VAL_FUNCTION, // Reuse VAL_FUNCTION to mean "Callable" (VM will assume ObjClosure or handle translation?)
		Obj:  &ObjClosure{Function: fn, Upvalues: make([]*ObjUpvalue, fn.UpvalueCount), Environment: fn.Environment},
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

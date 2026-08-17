package vm

import (
	"testing"

	"noxy-vm/internal/chunk"
	"noxy-vm/internal/value"
)

// Os chunks destes testes terminam sem OP_RETURN de propósito: o loop do
// executor cai fora ao fim do código e deixa a pilha intacta para inspeção.
// Frame raiz: stack[0] = script closure, LocalBase = 1 (slot local 0 = stack[1]).

func TestGetLocalMutClonesSharedAndWritesBack(t *testing.T) {
	machine := New()
	arr := value.NewArray([]value.Value{value.NewInt(10)})
	// spec §3: a precondição "compartilhado" é Owners > 1 (dois donos
	// duráveis), não mais o bit sticky que value.MarkShared liga.
	shareByOwners(arr)

	code := &chunk.Chunk{}
	constIdx := code.AddConstant(arr)
	code.Write(byte(chunk.OP_CONSTANT), 1)
	code.Write(byte(constIdx), 1)
	code.Write(byte(chunk.OP_GET_LOCAL_MUT), 1)
	code.Write(0, 1)

	if err := machine.Interpret(code); err != nil {
		t.Fatalf("vm error: %v", err)
	}

	slotVal := machine.stack[1]
	if slotVal.Obj == arr.Obj {
		t.Fatal("slot deveria conter o clone, não o original compartilhado")
	}
	if value.IsShared(slotVal) {
		t.Fatal("clone no slot deve ter um único dono (o próprio slot)")
	}
	if machine.stack[2].Obj != slotVal.Obj {
		t.Fatal("valor empilhado deve ser o mesmo clone gravado no slot")
	}
}

func TestGetLocalMutLeavesUnsharedAlone(t *testing.T) {
	machine := New()
	arr := value.NewArray([]value.Value{value.NewInt(10)})

	code := &chunk.Chunk{}
	constIdx := code.AddConstant(arr)
	code.Write(byte(chunk.OP_CONSTANT), 1)
	code.Write(byte(constIdx), 1)
	code.Write(byte(chunk.OP_GET_LOCAL_MUT), 1)
	code.Write(0, 1)

	if err := machine.Interpret(code); err != nil {
		t.Fatalf("vm error: %v", err)
	}
	if machine.stack[1].Obj != arr.Obj {
		t.Fatal("objeto não-Shared não pode ser clonado")
	}
}

func TestGetIndexMutClonesSharedChild(t *testing.T) {
	machine := New()
	child := value.NewArray([]value.Value{value.NewInt(5)})
	parent := value.NewArray([]value.Value{child})
	// spec §3: dois donos duráveis (o elemento de parent, que o OP_ARRAY real
	// teria retido, mais um alias qualquer) em vez do bit sticky.
	shareByOwners(child)

	code := &chunk.Chunk{}
	parentIdx := code.AddConstant(parent)
	zeroIdx := code.AddConstant(value.NewInt(0))
	code.Write(byte(chunk.OP_CONSTANT), 1)
	code.Write(byte(parentIdx), 1)
	code.Write(byte(chunk.OP_CONSTANT), 1)
	code.Write(byte(zeroIdx), 1)
	code.Write(byte(chunk.OP_GET_INDEX_MUT), 1)

	if err := machine.Interpret(code); err != nil {
		t.Fatalf("vm error: %v", err)
	}

	parentObj := parent.Obj.(*value.ObjArray)
	if parentObj.Elements[0].Obj == child.Obj {
		t.Fatal("filho compartilhado deveria ter sido clonado e gravado de volta em Elements[0]")
	}
	if machine.stack[1].Obj != parentObj.Elements[0].Obj {
		t.Fatal("valor empilhado deve ser o clone gravado no pai")
	}
	if value.IsShared(parentObj.Elements[0]) {
		t.Fatal("clone deve ter um único dono (o elemento do pai)")
	}
}

func TestGetPropMutClonesSharedField(t *testing.T) {
	machine := New()
	field := value.NewArray([]value.Value{value.NewInt(5)})
	structDef := &value.ObjStruct{Name: "Box", Fields: []string{"data"}}
	inst := value.NewInstance(structDef)
	inst.Obj.(*value.ObjInstance).Fields["data"] = field
	// spec §3: dois donos duráveis (o campo da instância, que o construtor
	// real teria retido, mais um alias qualquer) em vez do bit sticky.
	shareByOwners(field)

	code := &chunk.Chunk{}
	instIdx := code.AddConstant(inst)
	nameIdx := code.AddConstant(value.NewString("data"))
	code.Write(byte(chunk.OP_CONSTANT), 1)
	code.Write(byte(instIdx), 1)
	code.Write(byte(chunk.OP_GET_PROP_MUT), 1)
	code.Write(byte(nameIdx>>8), 1)
	code.Write(byte(nameIdx&0xff), 1)

	if err := machine.Interpret(code); err != nil {
		t.Fatalf("vm error: %v", err)
	}

	instObj := inst.Obj.(*value.ObjInstance)
	if instObj.Fields["data"].Obj == field.Obj {
		t.Fatal("campo compartilhado deveria ter sido clonado e gravado de volta")
	}
	if machine.stack[1].Obj != instObj.Fields["data"].Obj {
		t.Fatal("valor empilhado deve ser o clone gravado no campo")
	}
}

func TestDerefMutUnicizesThroughGlobalSlot(t *testing.T) {
	machine := New()
	arr := value.NewArray([]value.Value{value.NewInt(3)})
	env := machine.shared.Root
	env.SetLocal("g", arr)
	// spec §3: dois donos duráveis (o slot global g, que o OP_SET_GLOBAL real
	// teria retido, mais um alias qualquer) em vez do bit sticky.
	shareByOwners(arr)
	refVal := value.Value{Type: value.VAL_REF, Obj: &value.ObjRef{
		RefType:     value.REF_GLOBAL,
		Name:        "g",
		GlobalOwner: env,
	}}

	code := &chunk.Chunk{}
	refIdx := code.AddConstant(refVal)
	code.Write(byte(chunk.OP_CONSTANT), 1)
	code.Write(byte(refIdx), 1)
	code.Write(byte(chunk.OP_DEREF_MUT), 1)

	if err := machine.Interpret(code); err != nil {
		t.Fatalf("vm error: %v", err)
	}

	stored, ok := env.GetLocal("g")
	if !ok {
		t.Fatal("global g sumiu")
	}
	if stored.Obj == arr.Obj {
		t.Fatal("composto compartilhado atrás do ref deveria ter sido clonado no slot")
	}
	if machine.stack[1].Obj != stored.Obj {
		t.Fatal("valor empilhado deve ser o clone gravado no slot global")
	}
}

func TestMarkSharedOpcode(t *testing.T) {
	machine := New()
	arr := value.NewArray([]value.Value{value.NewInt(1)})

	code := &chunk.Chunk{}
	constIdx := code.AddConstant(arr)
	code.Write(byte(chunk.OP_CONSTANT), 1)
	code.Write(byte(constIdx), 1)
	code.Write(byte(chunk.OP_MARK_SHARED), 1)

	if err := machine.Interpret(code); err != nil {
		t.Fatalf("vm error: %v", err)
	}
	// spec §3: depois da chave, value.IsShared lê Owners — o bit sticky virou
	// dead-weight (ninguém mais o lê) e só será removido na Task 8. Enquanto o
	// opcode existir, auditamos exatamente o que ele ainda faz: ligar o bit no
	// objeto do topo da pilha.
	if !arr.Obj.(*value.ObjArray).Shared.Load() {
		t.Fatal("OP_MARK_SHARED deve ligar o bit do topo da pilha")
	}
	if value.IsShared(arr) {
		t.Fatal("o bit não decide mais unicidade: sem donos duráveis, o valor é único")
	}
}

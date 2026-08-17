package vm

import "noxy-vm/internal/value"

// stampReadonlyArgs estampa o flag CoW de native só-leitura no registro,
// para o hot path de chamada não pagar lookup de mapa por nome.
func stampReadonlyArgs(v value.Value) value.Value {
	if native, ok := v.Obj.(*value.ObjNative); ok && native != nil {
		native.ReadonlyArgs = readonlyNatives[native.Name]
	}
	return v
}

// readonlyNatives lista natives sem assinatura que comprovadamente não retêm
// nem mutam seus argumentos: seus args compostos não precisam do Retain
// conservador em callValue. Só entra aqui native auditado — o default
// conservador é reter (RC: assume posse durável indefinida).
var readonlyNatives = map[string]bool{
	"length":      true,
	"to_str":      true,
	"to_int":      true,
	"to_float":    true,
	"fmt":         true,
	"typeof":      true,
	"chan_recv":   true, // recebe o canal; o payload foi retido no send
	"test_report": true, // harness de teste: apenas observa
	"has_key":     true, // só consulta o mapa; devolve bool novo
	"keys":        true, // Snapshot + array novo; não compartilha estrutura
}

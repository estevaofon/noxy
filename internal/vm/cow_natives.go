package vm

// readonlyNatives lista natives sem assinatura que comprovadamente não retêm
// nem mutam seus argumentos: seus args compostos não precisam de MarkShared.
// Só entra aqui native auditado — o default conservador é marcar.
var readonlyNatives = map[string]bool{
	"length":      true,
	"to_str":      true,
	"to_int":      true,
	"to_float":    true,
	"fmt":         true,
	"typeof":      true,
	"chan_recv":   true, // recebe o canal; o payload foi marcado no send
	"test_report": true, // harness de teste: apenas observa
}

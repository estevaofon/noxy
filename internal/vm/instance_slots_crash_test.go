//go:build !race

package vm

import (
	"testing"

	"github.com/estevaofon/noxy/internal/value"
)

// Issue #86: duas routines escrevendo o mesmo ObjInstance derrubavam o
// processo inteiro com `fatal error: concurrent map read and map write` —
// irrecuperavel, nem call_result nem spawn_task seguram. Com os campos em
// slots (value.ObjInstance.Slots) nao existe map por instancia: a corrida
// continua sendo corrida (escritas perdidas, como em `counter = counter + 1`
// sobre um global), mas o runtime Go nao morre.
//
// O programa e o da #92: o alias chega por um campo `ref` dentro de um valor
// passado POR VALOR — nenhum `ref` no call site do spawn_task.
//
// Fora do detector de corrida de proposito (`//go:build !race`): sob -race o
// que este programa faz E uma corrida de dados, e o detector tem de continuar
// acusando; o que este teste fixa e a barra que concurrency.md promete —
// "does not crash the Go runtime".
func TestInstanceFieldsRaceDoesNotCrashRuntime(t *testing.T) {
	got := captureVMSource(t, `struct Node
    value: int
    next: ref Node?
end
struct Box
    tag: string
    inner: ref Node
end
func worker(b: Box) -> int
    let i: int = 0
    while i < 20000 do
        b.inner.value = b.inner.value + 1
        i = i + 1
    end
    return b.inner.value
end
let alvo: Node = Node(0, null)
let caixa: Box = Box("a", ref alvo)
let t1: any = spawn_task(worker, caixa)
let t2: any = spawn_task(worker, caixa)
let e1: any = task_await(t1)
let e2: any = task_await(t2)
test_report([e1["status"], e2["status"], alvo.value])`)
	arr, ok := got.Obj.(*value.ObjArray)
	if !ok || len(arr.Elements) != 3 {
		t.Fatalf("test_report deveria receber [status, status, value], veio %s", got.String())
	}
	for i := 0; i < 2; i++ {
		if s, _ := arr.Elements[i].Obj.(string); s != "ok" {
			t.Fatalf("worker %d nao terminou com ok: %s", i+1, arr.Elements[i].String())
		}
	}
	// Escritas podem se perder (corrida documentada), mas cada worker fez
	// pelo menos as suas: o valor final esta entre 20000 e 40000.
	final := arr.Elements[2].Int()
	if final < 20000 || final > 40000 {
		t.Fatalf("alvo.value=%d fora de [20000, 40000]", final)
	}
}

package vm

// Task 15 (spec §10/§11): interações de runtime E2E entre generics e as
// demais features de linguagem — lambda, defer, spawn/task, when/case,
// json, f-string e semântica de valor (CoW). Instâncias monomorfizadas são
// código comum no pass 2: nenhuma dessas features precisa saber que um tipo
// ou uma chamada veio de um template genérico.

import (
	"testing"

	"noxy-vm/internal/value"
)

func TestLambdaInsideGenericBody(t *testing.T) {
	got := captureVMSource(t, `
func aplica_dobro<T>(x: T, f: func(T) -> T) -> T
    return f(x)
end
let quadruplo: int = aplica_dobro(2, func(n: int) -> int
    return n * 4
end)
test_report(quadruplo)
`)
	expectInt(t, got, 8, "lambda como argumento de generica")
}

// §11: defer registra a chamada no frame corrente (aqui, o script) e a
// executa no fim do frame — o alvo é uma instância monomorfizada
// (soma_caixa<int>) chamada com um struct genérico instanciado (Caixa<int>).
func TestDeferWithInstantiatedGeneric(t *testing.T) {
	got := captureVMSource(t, `
struct Caixa<T>
    valor: T
end
func soma_caixa<T>(c: Caixa<T>) -> T
    return c.valor + c.valor
end
let c: Caixa<int> = Caixa(21)
defer test_report(soma_caixa(c))
`)
	expectInt(t, got, 42, "defer chamando instancia generica")
}

// §11: spawn_task roda o worker em outra goroutine; o corpo do worker
// constrói um Caixa<int> e chama uma função genérica instanciada sobre ele.
// task_await sincroniza e devolve o envelope {status, value, error} comum a
// qualquer task — nada disso muda por o valor ter vindo de generics.
func TestSpawnWithInstantiatedGeneric(t *testing.T) {
	got := captureVMSource(t, `
struct Caixa<T>
    valor: T
end
func triplica<T>(c: Caixa<T>) -> T
    return c.valor + c.valor + c.valor
end
func worker() -> int
    let c: Caixa<int> = Caixa(14)
    return triplica(c)
end
let task: any = spawn_task(worker)
let result: any = task_await(task)
test_report(result["value"])
`)
	expectInt(t, got, 42, "spawn_task executando instancia generica")
}

// when/case é o select de canais da linguagem (cada `case` precisa ser
// chan_recv/chan_send); aqui o valor que atravessa o canal é o campo de um
// struct genérico instanciado, provando que Caixa<int>.valor é um int comum
// tanto para chan_send quanto para o binding do case.
func TestWhenOverGenericStruct(t *testing.T) {
	got := captureVMSource(t, `
struct Caixa<T>
    valor: T
end
let c: Caixa<int> = Caixa(7)
let ch: any = make_chan(1)
chan_send(ch, c.valor)
when
    case recebido = chan_recv(ch) then
        test_report(recebido * 6)
    default
        test_report(-1)
end
`)
	expectInt(t, got, 42, "when/case sobre campo de struct generico")
}

// f-string interpolando um campo de struct genérico e json_dumps serializando
// a instância inteira — ambos tratam Caixa<int> como um struct nominal comum.
func TestJSONAndFStringWithGenericStruct(t *testing.T) {
	got := captureVMSource(t, `
struct Caixa<T>
    valor: T
end
let c: Caixa<int> = Caixa(42)
let mensagem: string = f"valor={c.valor}"
let doc: string = json_dumps(c)
test_report(mensagem == "valor=42" && doc == "{\"valor\":42}")
`)
	assertBuiltinValue(t, got, value.NewBool(true))
}

// §13: semantica de valor vale para structs genericos
func TestCoWValueSemanticsWithGenericStruct(t *testing.T) {
	got := captureVMSource(t, `
struct Caixa<T>
    valor: T
end
let a: Caixa<int> = Caixa(1)
let b: Caixa<int> = a
b.valor = 99
test_report(a.valor)
`)
	expectInt(t, got, 1, "atribuicao de struct generico e copia (CoW)")
}

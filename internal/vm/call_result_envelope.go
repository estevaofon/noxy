package vm

import (
	"fmt"

	"github.com/estevaofon/noxy/internal/value"
)

// resultDefs sao as definicoes de struct que o COMPILADOR entrega a
// call_result como dois argumentos ocultos (issue #105 item 2): a instancia
// monomorfizada errors::Result<R> do call site e o struct errors.Failure.
// O envelope e uma instancia REAL de struct — fmt("%T") reporta o nome da
// instancia, compara igual a um Result construido a mao e passa pela
// validacao nominal de runtime.
type resultDefs struct {
	result  *value.ObjStruct
	failure *value.ObjStruct
}

func resultDefsFromArgs(args []value.Value) (resultDefs, error) {
	if len(args) < 2 {
		return resultDefs{}, fmt.Errorf("call_result needs 'use errors select *' in scope: its result is errors.Result<T>")
	}
	resultDef, okResult := args[0].Obj.(*value.ObjStruct)
	failureDef, okFailure := args[1].Obj.(*value.ObjStruct)
	if args[0].Type != value.VAL_OBJ || !okResult || resultDef == nil || args[1].Type != value.VAL_OBJ || !okFailure || failureDef == nil {
		return resultDefs{}, fmt.Errorf("call_result needs 'use errors select *' in scope: its result is errors.Result<T>")
	}
	return resultDefs{result: resultDef, failure: failureDef}, nil
}

// okEnvelope: NewInstanceWith retem `result` (unico campo composto alem do
// Failure vazio) — e isso, e so isso, que da ao envelope a posse de
// r.value. Sem esse dono, uma composta devolvida fresca (Owners=0) chegaria
// a Owners=1 no primeiro `let` do lado Noxy, IsShared ficaria falso e a
// mutacao nesse binding vazaria para r.value (TestCallResultValueSemantics).
func (defs resultDefs) okEnvelope(result value.Value) value.Value {
	return value.NewInstanceWith(defs.result, map[string]value.Value{
		"ok":      value.NewBool(true),
		"value":   result,
		"failure": defs.emptyFailure(),
	})
}

// failureEnvelope: a arvore de Failure (este envelope, failure e as causes)
// e construida com NewInstanceWith/NewArray, que retem cada filho composto
// (o pai e dono duravel — mesma regra de OP_MAP/OP_ARRAY). Sem esse dono o
// primeiro `let f: any = r.failure` do lado Noxy levaria o filho a
// Owners=1, IsShared falso, e a mutacao reescreveria o envelope
// (TestCallResultFailureAliasDoesNotMutateEnvelope). Strings e escalares
// sao no-op em Retain (ownersOf so rastreia compostos).
func (defs resultDefs) failureEnvelope(err error) value.Value {
	return value.NewInstanceWith(defs.result, map[string]value.Value{
		"ok":      value.NewBool(false),
		"value":   value.NewNull(),
		"failure": defs.failureOf(err),
	})
}

// emptyFailure e o sentinela de sucesso: Failure("", "", "", []) — os dois
// campos do envelope existem sempre e `ok` diz qual vale (spec §7).
func (defs resultDefs) emptyFailure() value.Value {
	return defs.newFailure("", "", "", value.NewArray([]value.Value{}))
}

func (defs resultDefs) newFailure(kind, message, stack string, causes value.Value) value.Value {
	return value.NewInstanceWith(defs.failure, map[string]value.Value{
		"kind":    value.NewString(kind),
		"message": value.NewString(message),
		"stack":   value.NewString(stack),
		"causes":  causes,
	})
}

// failureOf converte a arvore de erro do unwinding no shape Failure.
// UnwindError com Primary vira a falha primaria com cada DeferredError em
// causes (ordem LIFO ja garantida por finalizeCurrentFrame); cleanup-first
// (Primary nil) promove a PRIMEIRA falha diferida a primaria e agrega as
// demais sob as causes dela (design §2, "Cleanup as first failure").
func (defs resultDefs) failureOf(err error) value.Value {
	if panicErr, ok := err.(*boundaryPanicError); ok {
		return defs.newFailure("panic", panicErr.payload, panicErr.stack, value.NewArray([]value.Value{}))
	}
	if unwind, ok := err.(*UnwindError); ok {
		if unwind.Primary != nil {
			return defs.failureWithCauses(unwind.Primary, unwind.Deferred)
		}
		if len(unwind.Deferred) > 0 {
			return defs.deferredFailure(&unwind.Deferred[0], unwind.Deferred[1:])
		}
	}
	if deferred, ok := err.(*DeferredError); ok {
		return defs.deferredFailure(deferred, nil)
	}
	if deferred, ok := err.(DeferredError); ok {
		return defs.deferredFailure(&deferred, nil)
	}
	return defs.failureWithCauses(err, nil)
}

func (defs resultDefs) failureWithCauses(primary error, deferred []DeferredError) value.Value {
	causes := make([]value.Value, 0, len(deferred))
	for index := range deferred {
		causes = append(causes, defs.deferredFailure(&deferred[index], nil))
	}
	message := ""
	if primary != nil {
		message = primary.Error()
	}
	return defs.newFailure("runtime", message, deepestRuntimeStack(primary), value.NewArray(causes))
}

// deferredFailure constroi a Failure de uma falha diferida: a causa vira a
// falha (aninhando as proprias causes dela recursivamente via failureOf) e
// a localizacao de REGISTRO do defer entra como frame mais externo do stack
// — forma-envelope da promessa da spec de defer ("with its registration
// location"). siblings sao falhas diferidas posteriores promovidas para as
// causes desta (apenas no caso cleanup-first).
func (defs resultDefs) deferredFailure(deferred *DeferredError, siblings []DeferredError) value.Value {
	failure := defs.failureOf(deferred.Cause)
	instance := failure.Obj.(*value.ObjInstance)

	stackValue, _ := instance.Get("stack")
	stack, _ := stackValue.Obj.(string)
	registrationFrame := fmt.Sprintf("[%s] defer registration", deferred.Registration)
	if stack == "" {
		stack = registrationFrame
	} else {
		stack = stack + "\n" + registrationFrame
	}
	instance.MustSet("stack", value.NewString(stack))

	if len(siblings) > 0 {
		// merge, nao substitui: a falha promovida ja pode ter causes proprias
		// (o Cause dela era um *UnwindError com seu proprio Deferred) —
		// preserva-las primeiro e so entao anexa os siblings (falhas
		// diferidas posteriores no mesmo frame, cleanup-first) por cima.
		previous, hadPrevious := instance.Get("causes")
		inner := existingCauses(instance)
		causes := make([]value.Value, 0, len(inner)+len(siblings))
		// RC: os herdados apenas TROCAM de array — o retain que o array
		// antigo registrou passa a valer pelo novo, entao re-reter aqui seria
		// um dono a mais que ninguem solta (composto IsShared para sempre,
		// clonando a cada mutacao).
		causes = append(causes, inner...)
		for index := range siblings {
			sibling := defs.deferredFailure(&siblings[index], nil)
			value.Retain(sibling) // filho novo: o array vira dono duravel
			causes = append(causes, sibling)
		}
		// RC: move — os herdados transferem o retain do array antigo, os
		// irmaos novos foram retidos no laco acima; NewArrayAdopting nao
		// retem de novo. MustSet NAO solta o valor sobrescrito: a troca e a
		// mao e segue a ordem segura — o array novo ganha a instancia como
		// dono ANTES de o antigo perder o dele.
		replacement := value.NewArrayAdopting(causes)
		value.Retain(replacement)
		instance.MustSet("causes", replacement)
		if hadPrevious {
			value.Release(previous)
		}
	}
	return failure
}

// existingCauses le o array "causes" ja presente numa Failure recem
// construida por failureOf — sempre presente (failureWithCauses e
// deferredFailure ambos o preenchem, mesmo vazio) mas lido com defesa
// contra shape inesperado.
func existingCauses(instance *value.ObjInstance) []value.Value {
	causesValue, ok := instance.Get("causes")
	if !ok {
		return nil
	}
	array, ok := causesValue.Obj.(*value.ObjArray)
	if !ok || array == nil {
		return nil
	}
	return array.Elements
}

package value

import (
	"math"
	"sync/atomic"
)

// IsShared informa se o composto tem mais de um dono durável vivo — a
// unicidade é decidida pelo contador Owners (spec §3: "posse única por
// contagem de referências duráveis"). O antigo bit sticky Shared foi
// removido na Task 8; Owners é a única fonte de verdade.
func IsShared(v Value) bool {
	owners := ownersOf(v)
	return owners != nil && owners.Load() > 1
}

// NeverTracked responde se v CERTAMENTE nao tem contador RC: escalares
// (Type != VAL_OBJ) e os VAL_OBJ que os construtores carimbaram como sem dono
// (string, *ObjStruct, *RuntimeTypeInfo). E a conferencia que as escritas NORC
// da indexacao tipada (issue #66, item 1) fazem antes de pular Retain/Release:
// se o valor novo ou o velho nao for comprovadamente sem contador (composto,
// ou VAL_OBJ sem carimbo — kind zero), o chamador cai no caminho generico, que
// retem. Conservador por construcao: nunca devolve true para um composto.
// Chamada de dentro de run(): tem de caber em 20 no inliner
// (inline_guard_test.go).
func NeverTracked(v Value) bool {
	return v.Type != VAL_OBJ || v.kind == objKindNoOwners
}

// ownersSaturation impede overflow do contador; acima disso o valor se
// comporta como permanentemente compartilhado (equivalente ao sticky).
const ownersSaturation = math.MaxInt32 / 2

// ownersOf devolve o contador de donos do composto (o Owners do ObjHeader
// embutido em array/map/instancia), ou nil para tudo o que o RC nao rastreia.
//
// A dica kind carimbada pelos construtores de internal/value tira do type
// switch os VAL_OBJ que nunca tem contador (string, *ObjStruct,
// *RuntimeTypeInfo — objKindNoOwners): uma comparacao de byte. O resto
// (compostos, escalares com Obj nil, Values montados fora dos construtores
// com kind zero) segue pelo type switch de sempre — a dica nunca decide
// sozinha, entao um carimbo ausente nao pode virar under-count
// (owners_test.go cobre os dois caminhos). Nao ha checagem de Type: o unico
// dono de *ObjArray/*ObjMap/*ObjInstance em Obj e VAL_OBJ, e Obj nil
// (escalares) cai no default do switch.
//
// ORCAMENTO: Retain e Release embutem este corpo e precisam ficar em <= 80
// (orcamento normal do inliner; sao inlinados nos sites de internal/vm fora
// de run()). Qualquer no a mais aqui — uma chamada custa 57, um segundo
// switch por kind com type assertion por caso custa ~40 — tira Release do
// inline; foi medido: a versao "switch no kind + assertion checada + caminho
// lento embutido" custava 73 e levou Retain a 105 e Release a 119.
// inline_guard_test.go (internal/vm) trava a propriedade.
func ownersOf(v Value) *atomic.Int32 {
	if v.kind == objKindNoOwners {
		return nil
	}
	switch obj := v.Obj.(type) {
	case *ObjArray:
		return &obj.Owners
	case *ObjMap:
		return &obj.Owners
	case *ObjInstance:
		return &obj.Owners
	}
	return nil
}

// Retain registra um dono durável novo. Retorna true se o valor é um
// composto rastreável (chamador decide se registra o slot para release).
func Retain(v Value) bool {
	owners := ownersOf(v)
	if owners == nil {
		return false
	}
	if owners.Load() < ownersSaturation {
		owners.Add(1)
	}
	return true
}

// Release solta um dono durável. Nunca desce abaixo de zero (dec a mais é
// proibido por design; o clamp protege contra funis duplicados).
func Release(v Value) {
	owners := ownersOf(v)
	if owners == nil {
		return
	}
	for {
		current := owners.Load()
		// Sai quando current esta fora de (0, ownersSaturation): <= 0 e o
		// clamp do dec a mais, >= ownersSaturation e o "permanentemente
		// compartilhado". A comparacao unica em uint32 (current-1 negativo
		// vira um numero enorme) e equivalente a `current <= 0 || current >=
		// ownersSaturation` e e a forma que cabe no orcamento de inline de
		// Release (ver ownersOf): duas comparacoes custam os 3 nos que a
		// dica kind acrescentou la.
		if uint32(current-1) >= ownersSaturation-1 {
			return
		}
		if owners.CompareAndSwap(current, current-1) {
			return
		}
	}
}

// OwnersCount é introspecção para testes; -1 para não-compostos.
func OwnersCount(v Value) int32 {
	owners := ownersOf(v)
	if owners == nil {
		return -1
	}
	return owners.Load()
}

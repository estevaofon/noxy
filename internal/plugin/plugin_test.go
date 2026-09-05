package plugin

import (
	"testing"

	"github.com/estevaofon/noxy/internal/value"
)

// InterfaceToValue constroi via value.NewArray/NewMapWithData: o contêiner e
// dono duravel de cada filho composto sem que o plugin precise de *VM.
func TestInterfaceToValueContainersOwnNestedChildren(t *testing.T) {
	converted := InterfaceToValue([]interface{}{
		map[string]interface{}{"itens": []interface{}{1.0, 2.0}},
		"texto",
	})
	outer, ok := converted.Obj.(*value.ObjArray)
	if !ok || len(outer.Elements) != 2 {
		t.Fatalf("esperado array de 2 elementos, veio %#v", converted)
	}
	nested := outer.Elements[0]
	if got := value.OwnersCount(nested); got != 1 {
		t.Fatalf("map aninhado deve ter o array como unico dono: Owners=%d", got)
	}
	items, found := nested.Obj.(*value.ObjMap).Get("itens")
	if !found {
		t.Fatal("map aninhado deve conter a chave itens")
	}
	if got := value.OwnersCount(items); got != 1 {
		t.Fatalf("array aninhado deve ter o map como unico dono: Owners=%d", got)
	}
	if got := value.OwnersCount(outer.Elements[1]); got != -1 {
		t.Fatalf("string nao tem contador: Owners=%d", got)
	}
	if got := value.OwnersCount(converted); got != 0 {
		t.Fatalf("o contêiner raiz nasce sem dono: Owners=%d", got)
	}
}

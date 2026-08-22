package value

import "testing"

// NeverTracked e o teste que as escritas NORC da indexacao tipada fazem em
// runtime antes de pular Retain/Release (issue #66, item 1): so pode dizer
// "true" para o que o RC comprovadamente nao rastreia. A direcao segura e
// "false" — o chamador cai no caminho generico, que retem.
func TestNeverTrackedIsTrueOnlyForValuesWithoutOwners(t *testing.T) {
	inst := NewInstanceWith(&ObjStruct{Name: "P"}, map[string]Value{})
	cases := []struct {
		name string
		v    Value
		want bool
	}{
		{"int", NewInt(1), true},
		{"float", NewFloat(1.5), true},
		{"bool", NewBool(true), true},
		{"null", NewNull(), true},
		{"string (carimbada)", NewString("x"), true},
		{"bytes", Value{Type: VAL_BYTES, Obj: "ab"}, true},
		{"ref", Value{Type: VAL_REF, Obj: &ObjRef{}}, true},
		{"array", NewArray(nil), false},
		{"map", NewMap(), false},
		{"instance", inst, false},
		// VAL_OBJ montado fora dos construtores (kind zero): nao se sabe,
		// entao false — o caminho generico decide por ownersOf.
		{"string sem carimbo", Value{Type: VAL_OBJ, Obj: "x"}, false},
		{"array sem carimbo", Value{Type: VAL_OBJ, Obj: &ObjArray{}}, false},
	}
	for _, tc := range cases {
		if got := NeverTracked(tc.v); got != tc.want {
			t.Errorf("%s: NeverTracked = %v, esperado %v", tc.name, got, tc.want)
		}
	}
}

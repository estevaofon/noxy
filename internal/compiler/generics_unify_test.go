package compiler

import (
	"strings"
	"testing"

	"noxy-vm/internal/ast"
)

func tInt() ast.NoxyType               { return &ast.PrimitiveType{Name: "int"} }
func tString() ast.NoxyType            { return &ast.PrimitiveType{Name: "string"} }
func tAny() ast.NoxyType               { return &ast.PrimitiveType{Name: "any"} }
func tNull() ast.NoxyType              { return &ast.PrimitiveType{Name: "null"} }
func tParam(n string) ast.NoxyType     { return &ast.TypeParamType{Name: n} }
func tArr(e ast.NoxyType) ast.NoxyType { return &ast.ArrayType{ElementType: e} }

func TestUnifyTable(t *testing.T) {
	cases := []struct {
		name     string
		expected ast.NoxyType
		actual   ast.NoxyType
		want     map[string]string // binding esperado (String())
		errPart  string            // "" = sem erro
	}{
		{"T contra int", tParam("T"), tInt(), map[string]string{"T": "int"}, ""},
		{"T[] contra int[]", tArr(tParam("T")), tArr(tInt()), map[string]string{"T": "int"}, ""},
		{"map[K,V]", &ast.MapType{KeyType: tParam("K"), ValueType: tParam("V")},
			&ast.MapType{KeyType: tString(), ValueType: tInt()},
			map[string]string{"K": "string", "V": "int"}, ""},
		{"func(A)->B", &ast.FunctionType{Params: []ast.NoxyType{tParam("A")}, Return: tParam("B")},
			&ast.FunctionType{Params: []ast.NoxyType{tInt()}, Return: tString()},
			map[string]string{"A": "int", "B": "string"}, ""},
		{"chan T", &ast.ChanType{ElementType: tParam("T")}, &ast.ChanType{ElementType: tInt()},
			map[string]string{"T": "int"}, ""},
		{"GenericType args", &ast.GenericType{Name: "Stack", Args: []ast.NoxyType{tParam("T")}},
			&ast.GenericType{Name: "Stack", Args: []ast.NoxyType{tInt()}},
			map[string]string{"T": "int"}, ""},
		{"conflito", tParam("T"), tInt(), nil, ""}, // preparado abaixo com segundo unify
		{"any nao binda", tParam("T"), tAny(), map[string]string{}, ""},
		{"null nao binda", tParam("T"), tNull(), map[string]string{}, ""},
		{"T nao binda ref", tParam("T"), &ast.RefType{ElementType: tInt()}, nil, "não pode ser um tipo ref"},
		{"construtor divergente", tArr(tParam("T")), tInt(), nil, "esperava"},
		{"map[T,any] contra map[int,string]: any folgado no leaf nao erra",
			&ast.MapType{KeyType: tParam("T"), ValueType: tAny()},
			&ast.MapType{KeyType: tInt(), ValueType: tString()},
			map[string]string{"T": "int"}, ""},
		{"ref int contra null: null folgado nao erra",
			&ast.RefType{ElementType: tInt()}, tNull(), map[string]string{}, ""},
		{"func bare contra FunctionType: callable folgado nao erra",
			&ast.PrimitiveType{Name: "func"},
			&ast.FunctionType{Params: []ast.NoxyType{tInt()}, Return: tString()},
			map[string]string{}, ""},
		// Task 6 (spec 2026-08-24-explicit-ref, R5): actual sem `ref` contra
		// `ref T` nao e mismatch estrutural AQUI — a inferencia segue pelo
		// elemento (T binda com o tipo cru) e o erro de R5 (call site espera
		// `ref x`) fica por conta de compileCallExpression, depois da
		// monomorfizacao (generics_unify.go, case *ast.RefType).
		{"ref T contra int: infere pelo elemento",
			&ast.RefType{ElementType: tParam("T")}, tInt(),
			map[string]string{"T": "int"}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b := map[string]ast.NoxyType{}
			err := unify(tc.expected, tc.actual, b)
			if tc.name == "conflito" {
				if err != nil {
					t.Fatal(err)
				}
				err = unify(tc.expected, tString(), b) // T=int já bindado; string conflita
				if err == nil || !strings.Contains(err.Error(), "inferido como") {
					t.Fatalf("esperava conflito, veio %v", err)
				}
				return
			}
			if tc.errPart != "" {
				if err == nil || !strings.Contains(err.Error(), tc.errPart) {
					t.Fatalf("esperava erro contendo %q, veio %v", tc.errPart, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			for k, v := range tc.want {
				got, ok := b[k]
				if !ok || got.String() != v {
					t.Fatalf("binding %s = %v, quer %s", k, got, v)
				}
			}
		})
	}
}

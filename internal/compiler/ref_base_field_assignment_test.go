package compiler

import (
	"strings"
	"testing"

	"noxy-vm/internal/value"
)

// Issue #50, Parte 1: atribuicao a campo atraves de uma base `ref` e checada
// exatamente como atraves de uma base valor (spec §2.0 regras 1-2; o
// compilador conhece o tipo da base e do campo — nao e fronteira dinamica).
const refBasePrelude = `
struct Node
    valor: int
    proximo: ref Node
end
`

func TestFieldAssignmentThroughRefBaseIsTypeChecked(t *testing.T) {
	_, err := compileFunctionSource(t, refBasePrelude+`
func estraga(node: ref Node)
    node.valor = "texto"
end`)
	if err == nil {
		t.Fatal("esperava erro de tipo via base ref")
	}
	want := "type mismatch in field assignment: expected int, got string"
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("erro = %q, esperava conter %q", err, want)
	}
}

func TestRefFieldThroughRefBaseRejectsRawReferent(t *testing.T) {
	cases := map[string]string{
		"construtor": `
func liga(node: ref Node)
    node.proximo = Node(9, null)
end`,
		"variavel por valor": `
func liga(node: ref Node, outro: Node)
    node.proximo = outro
end`,
	}
	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := compileFunctionSource(t, refBasePrelude+src)
			if err == nil {
				t.Fatal("esperava 'cannot assign Node to ref Node'")
			}
			if !strings.Contains(err.Error(), "cannot assign Node to ref Node") {
				t.Fatalf("erro = %q", err)
			}
		})
	}
}

func TestRefBaseFieldAssignmentAcceptsTypedForms(t *testing.T) {
	_, err := compileFunctionSource(t, refBasePrelude+`
func formas(node: ref Node, outro: ref Node)
    node.valor = 5
    let novo: Node = Node(7, null)
    node.proximo = ref novo
    node.proximo = null
    node.proximo = outro.proximo
    node.proximo = outro
    *node.proximo = Node(8, null)
end`)
	if err != nil {
		t.Fatalf("formas validas via base ref devem compilar: %v", err)
	}
}

// RefFields e a fonte de runtime da pergunta "este campo e ref?" e tem de
// bater com ConstructorType.ParamIsRef (spec §6.1) para todo struct que o
// compilador emite.
func TestStructDefinitionMarksRefFieldsConsistentlyWithConstructorType(t *testing.T) {
	code, _, err := New().Compile(parse(refBasePrelude + `
struct Plain
    a: int
    b: string
end
let n: Node = Node(1, null)
let p: Plain = Plain(1, "x")`))
	if err != nil {
		t.Fatal(err)
	}
	seen := 0
	for _, constant := range code.Constants {
		definition, ok := constant.Obj.(*value.ObjStruct)
		if !ok {
			continue
		}
		seen++
		if definition.ConstructorType == nil || len(definition.ConstructorType.ParamIsRef) != len(definition.Fields) {
			t.Fatalf("struct %s sem ConstructorType alinhado aos campos", definition.Name)
		}
		for i, field := range definition.Fields {
			wantRef := definition.ConstructorType.ParamIsRef[i]
			if definition.FieldIsRef(field) != wantRef {
				t.Fatalf("struct %s campo %s: FieldIsRef=%v, ConstructorType.ParamIsRef=%v", definition.Name, field, definition.FieldIsRef(field), wantRef)
			}
		}
		if definition.Name == "Node" && !definition.FieldIsRef("proximo") {
			t.Fatal("Node.proximo deveria estar em RefFields")
		}
		if definition.Name == "Plain" && definition.RefFields != nil {
			t.Fatalf("Plain nao tem campo ref; RefFields deveria ser nil, veio %v", definition.RefFields)
		}
	}
	if seen != 2 {
		t.Fatalf("esperava 2 definicoes de struct nas constantes, vi %d", seen)
	}
}

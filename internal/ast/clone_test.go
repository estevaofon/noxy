package ast

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestClonerCoversEveryNode enumera os tipos concretos do pacote que
// implementam Statement/Expression/NoxyType (via análise do fonte ast.go) e
// falha se clone.go não tiver um case para cada um. Um nó esquecido =
// AST compartilhado entre instâncias = contaminação silenciosa (§11 da spec).
func TestClonerCoversEveryNode(t *testing.T) {
	fset := token.NewFileSet()
	nodeNames := map[string]bool{}
	for _, file := range []string{"ast.go"} {
		parsed, err := parser.ParseFile(fset, file, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(parsed, func(n ast.Node) bool {
			fn, ok := n.(*ast.FuncDecl)
			if !ok || fn.Recv == nil || len(fn.Recv.List) == 0 {
				return true
			}
			name := fn.Name.Name
			if name != "statementNode" && name != "expressionNode" && name != "String" {
				return true
			}
			recv := fn.Recv.List[0].Type
			if star, ok := recv.(*ast.StarExpr); ok {
				if ident, ok := star.X.(*ast.Ident); ok {
					nodeNames[ident.Name] = true
				}
			}
			return true
		})
	}
	delete(nodeNames, "Program") // Program não precisa de clone (clonamos statements)
	delete(nodeNames, "Parameter")
	delete(nodeNames, "StructField") // clonados por dentro dos donos; cases próprios não exigidos

	cloneSrc, err := os.ReadFile(filepath.Join(".", "clone.go"))
	if err != nil {
		t.Fatalf("clone.go ausente: %v", err)
	}
	for name := range nodeNames {
		if !strings.Contains(string(cloneSrc), "*"+name+":") {
			t.Errorf("clone.go sem case para *%s", name)
		}
	}
}

func TestCloneStatementNoAliasing(t *testing.T) {
	original := &LetStmt{
		Name: &Identifier{Value: "x"},
		Type: &ArrayType{ElementType: &TypeParamType{Name: "T"}},
		Value: &CallExpression{
			Function:  &Identifier{Value: "make"},
			Arguments: []Expression{&IntegerLiteral{Value: 1}},
		},
	}
	clone := CloneStatement(original).(*LetStmt)
	// mutar o clone em profundidade
	clone.Name.Value = "y"
	clone.Type.(*ArrayType).ElementType = &PrimitiveType{Name: "int"}
	clone.Value.(*CallExpression).Arguments[0] = &IntegerLiteral{Value: 99}
	if original.Name.Value != "x" {
		t.Fatal("Name compartilhado entre original e clone")
	}
	if _, ok := original.Type.(*ArrayType).ElementType.(*TypeParamType); !ok {
		t.Fatal("Type compartilhado entre original e clone")
	}
	if original.Value.(*CallExpression).Arguments[0].(*IntegerLiteral).Value != 1 {
		t.Fatal("Arguments compartilhado entre original e clone")
	}
}

func TestCloneFunctionStatementDeep(t *testing.T) {
	original := &FunctionStatement{
		Name:       "first",
		TypeParams: []string{"T"},
		Parameters: []*Parameter{{Name: "arr", Type: &ArrayType{ElementType: &TypeParamType{Name: "T"}}}},
		ReturnType: &TypeParamType{Name: "T"},
		Body: &BlockStatement{Statements: []Statement{
			&ReturnStmt{ReturnValue: &IndexExpression{
				Left:  &Identifier{Value: "arr"},
				Index: &IntegerLiteral{Value: 0},
			}},
		}},
	}
	clone := CloneStatement(original).(*FunctionStatement)
	clone.Parameters[0].Type = &PrimitiveType{Name: "int"}
	clone.Body.Statements[0].(*ReturnStmt).ReturnValue = &NullLiteral{}
	if _, ok := original.Parameters[0].Type.(*ArrayType); !ok {
		t.Fatal("Parameter.Type compartilhado")
	}
	if _, ok := original.Body.Statements[0].(*ReturnStmt).ReturnValue.(*IndexExpression); !ok {
		t.Fatal("Body compartilhado")
	}
}

func TestCloneStructStatementBothFieldMirrors(t *testing.T) {
	original := &StructStatement{
		Name:       "Stack",
		TypeParams: []string{"T"},
		Fields:     map[string]NoxyType{"items": &ArrayType{ElementType: &TypeParamType{Name: "T"}}},
		FieldsList: []*StructField{{Name: "items", Type: &ArrayType{ElementType: &TypeParamType{Name: "T"}}}},
	}
	clone := CloneStatement(original).(*StructStatement)
	clone.FieldsList[0].Type = &PrimitiveType{Name: "int"}
	clone.Fields["items"] = &PrimitiveType{Name: "int"}
	if _, ok := original.FieldsList[0].Type.(*ArrayType); !ok {
		t.Fatal("FieldsList compartilhado")
	}
	if _, ok := original.Fields["items"].(*ArrayType); !ok {
		t.Fatal("Fields (map) compartilhado")
	}
}

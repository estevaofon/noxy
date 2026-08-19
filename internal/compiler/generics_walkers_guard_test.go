package compiler

// Guard por reflexão dos WALKERS EXAUSTIVOS de genéricos (I4 da revisão final
// de branch), irmão de ast.TestClonerCoversEveryNode.
//
// substituteInStatement/substituteInExpression (generics_substitute.go) e
// collectBoundNamesIn*/collectFreeIn* (generics.go) fazem `panic` no default —
// a disciplina certa (nó novo = falha barulhenta, não substituição silenciosa
// pela metade), mas um panic só dispara se algum teste exercitar exatamente o
// nó novo. Os comentários desses walkers já afirmavam ter cobertura de guard;
// até este arquivo existir, não tinham: só o cloner era guardado, em
// internal/ast/clone_test.go.
//
// O guard enumera os nós de ast.go (tipos com receiver statementNode /
// expressionNode) e exige um `case` para cada um em CADA walker,
// individualmente — checagem por função, não por arquivo: generics.go abriga
// dois walkers de statement e dois de expression, e um `strings.Contains` no
// arquivo inteiro daria por coberto um nó que só um deles trata.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"testing"
)

// astNodeNames devolve os nós concretos de internal/ast/ast.go separados em
// statements e expressions, pela mesma análise de fonte que
// ast.TestClonerCoversEveryNode usa (receiver de statementNode/expressionNode).
func astNodeNames(t *testing.T) (statements, expressions map[string]bool) {
	t.Helper()
	statements = map[string]bool{}
	expressions = map[string]bool{}

	parsed, err := parser.ParseFile(token.NewFileSet(), filepath.Join("..", "ast", "ast.go"), nil, 0)
	if err != nil {
		t.Fatalf("ast.go ilegivel: %v", err)
	}
	ast.Inspect(parsed, func(n ast.Node) bool {
		fn, ok := n.(*ast.FuncDecl)
		if !ok || fn.Recv == nil || len(fn.Recv.List) == 0 {
			return true
		}
		var target map[string]bool
		switch fn.Name.Name {
		case "statementNode":
			target = statements
		case "expressionNode":
			target = expressions
		default:
			return true
		}
		star, ok := fn.Recv.List[0].Type.(*ast.StarExpr)
		if !ok {
			return true
		}
		if ident, ok := star.X.(*ast.Ident); ok {
			target[ident.Name] = true
		}
		return true
	})

	if len(statements) == 0 || len(expressions) == 0 {
		t.Fatalf("analise de ast.go nao achou nós (statements=%d, expressions=%d)", len(statements), len(expressions))
	}
	return statements, expressions
}

// typeSwitchCases devolve os nomes de tipo `*ast.X` que aparecem em algum
// `case` de type switch dentro da função funcName do arquivo file. Um case
// com vários tipos (`case *ast.Identifier, *ast.IntegerLiteral:`) contribui
// todos eles.
func typeSwitchCases(t *testing.T, file, funcName string) map[string]bool {
	t.Helper()
	parsed, err := parser.ParseFile(token.NewFileSet(), file, nil, 0)
	if err != nil {
		t.Fatalf("%s ilegivel: %v", file, err)
	}

	covered := map[string]bool{}
	found := false
	for _, decl := range parsed.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Name.Name != funcName {
			continue
		}
		found = true
		ast.Inspect(fn, func(n ast.Node) bool {
			clause, ok := n.(*ast.CaseClause)
			if !ok {
				return true
			}
			for _, expr := range clause.List {
				star, ok := expr.(*ast.StarExpr)
				if !ok {
					continue
				}
				selector, ok := star.X.(*ast.SelectorExpr)
				if !ok {
					continue
				}
				if pkg, ok := selector.X.(*ast.Ident); ok && pkg.Name == "ast" {
					covered[selector.Sel.Name] = true
				}
			}
			return true
		})
	}
	if !found {
		t.Fatalf("%s nao declara %s — o guard ficou apontando para um nome morto", file, funcName)
	}
	return covered
}

func TestGenericWalkersCoverEveryNode(t *testing.T) {
	statements, expressions := astNodeNames(t)

	walkers := []struct {
		file     string
		function string
		nodes    map[string]bool
	}{
		{"generics_substitute.go", "substituteInStatement", statements},
		{"generics_substitute.go", "substituteInExpression", expressions},
		{"generics.go", "collectBoundNamesInStatement", statements},
		{"generics.go", "collectBoundNamesInExpression", expressions},
		{"generics.go", "collectFreeInStatement", statements},
		{"generics.go", "collectFreeInExpression", expressions},
	}

	for _, walker := range walkers {
		covered := typeSwitchCases(t, walker.file, walker.function)
		for name := range walker.nodes {
			if !covered[name] {
				t.Errorf("%s (%s) sem case para *ast.%s — o walker fica cego para esse nó (panic em runtime, no melhor caso)", walker.function, walker.file, name)
			}
		}
	}
}

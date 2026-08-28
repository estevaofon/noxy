package ast

// Inspect percorre a arvore em pre-ordem chamando fn em cada no (statement
// ou expression); fn devolve false para nao descer nos filhos daquele no.
// Tipos (NoxyType) nao sao visitados — sao anotacoes, nao codigo.
func Inspect(node Node, fn func(Node) bool) {
	if node == nil || !fn(node) {
		return
	}
	switch n := node.(type) {
	case *Program:
		for _, s := range n.Statements {
			Inspect(s, fn)
		}
	case *BlockStatement:
		if n == nil {
			return
		}
		for _, s := range n.Statements {
			Inspect(s, fn)
		}
	case *LetStmt:
		if n.Value != nil {
			Inspect(n.Value, fn)
		}
	case *AssignStmt:
		Inspect(n.Target, fn)
		Inspect(n.Value, fn)
	case *ReturnStmt:
		if n.ReturnValue != nil {
			Inspect(n.ReturnValue, fn)
		}
	case *DeferStmt:
		if n.Call != nil {
			Inspect(n.Call, fn)
		}
	case *ExpressionStmt:
		if n.Expression != nil {
			Inspect(n.Expression, fn)
		}
	case *IfStatement:
		Inspect(n.Condition, fn)
		inspectBlock(n.Consequence, fn)
		inspectBlock(n.Alternative, fn)
	case *WhileStatement:
		Inspect(n.Condition, fn)
		inspectBlock(n.Body, fn)
	case *ForStatement:
		Inspect(n.Collection, fn)
		inspectBlock(n.Body, fn)
	case *FunctionStatement:
		inspectBlock(n.Body, fn)
	case *WhenStatement:
		for _, clause := range n.Cases {
			if clause.Condition != nil {
				Inspect(clause.Condition, fn)
			}
			inspectBlock(clause.Body, fn)
		}
	case *CallExpression:
		Inspect(n.Function, fn)
		for _, arg := range n.Arguments {
			Inspect(arg, fn)
		}
	case *InfixExpression:
		Inspect(n.Left, fn)
		Inspect(n.Right, fn)
	case *PrefixExpression:
		Inspect(n.Right, fn)
	case *MemberAccessExpression:
		Inspect(n.Left, fn)
	case *IndexExpression:
		Inspect(n.Left, fn)
		Inspect(n.Index, fn)
	case *ArrayLiteral:
		for _, e := range n.Elements {
			Inspect(e, fn)
		}
	case *MapLiteral:
		for _, k := range n.Keys {
			Inspect(k, fn)
		}
		for _, v := range n.Values {
			Inspect(v, fn)
		}
	case *FunctionLiteral:
		inspectBlock(n.Body, fn)
	}
}

func inspectBlock(block *BlockStatement, fn func(Node) bool) {
	if block == nil {
		return
	}
	Inspect(block, fn)
}

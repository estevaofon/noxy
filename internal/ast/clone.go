package ast

// Clones profundos de nós do AST, usados pela monomorfização de genéricos
// (spec 2026-08-18-generics-design.md §4): cada instância recebe uma cópia
// exclusiva do corpo do template. Nó compartilhado entre instâncias seria
// contaminação silenciosa — o guard TestClonerCoversEveryNode impõe cobertura.

func CloneStatement(s Statement) Statement {
	if s == nil {
		return nil
	}
	switch n := s.(type) {
	case *AssignStmt:
		return &AssignStmt{Token: n.Token, Target: CloneExpression(n.Target), Value: CloneExpression(n.Value)}
	case *LetStmt:
		return &LetStmt{Token: n.Token, Name: cloneIdentifier(n.Name), Type: CloneType(n.Type), Value: CloneExpression(n.Value)}
	case *ReturnStmt:
		return &ReturnStmt{Token: n.Token, ReturnValue: CloneExpression(n.ReturnValue)}
	case *DeferStmt:
		return &DeferStmt{Token: n.Token, Call: cloneCallExpression(n.Call)}
	case *BreakStmt:
		return &BreakStmt{Token: n.Token}
	case *UseStmt:
		return &UseStmt{
			Token:     n.Token,
			Module:    n.Module,
			Alias:     n.Alias,
			Selectors: append([]string(nil), n.Selectors...),
			SelectAll: n.SelectAll,
		}
	case *ExpressionStmt:
		return &ExpressionStmt{Token: n.Token, Expression: CloneExpression(n.Expression)}
	case *BlockStatement:
		return CloneBlock(n)
	case *IfStatement:
		return &IfStatement{
			Token:       n.Token,
			Condition:   CloneExpression(n.Condition),
			Consequence: CloneBlock(n.Consequence),
			Alternative: CloneBlock(n.Alternative),
		}
	case *WhileStatement:
		return &WhileStatement{Token: n.Token, Condition: CloneExpression(n.Condition), Body: CloneBlock(n.Body)}
	case *FunctionStatement:
		return &FunctionStatement{
			Token:      n.Token,
			Name:       n.Name,
			TypeParams: append([]string(nil), n.TypeParams...),
			Parameters: cloneParameters(n.Parameters),
			ReturnType: CloneType(n.ReturnType),
			Body:       CloneBlock(n.Body),
		}
	case *ForStatement:
		return &ForStatement{
			Token:      n.Token,
			Identifier: n.Identifier,
			Collection: CloneExpression(n.Collection),
			Body:       CloneBlock(n.Body),
		}
	case *StructStatement:
		fields := make(map[string]NoxyType, len(n.Fields))
		list := make([]*StructField, len(n.FieldsList))
		for i, f := range n.FieldsList {
			cloned := &StructField{Name: f.Name, Type: CloneType(f.Type)}
			list[i] = cloned
			fields[f.Name] = cloned.Type
		}
		return &StructStatement{
			Token:      n.Token,
			Name:       n.Name,
			TypeParams: append([]string(nil), n.TypeParams...),
			Fields:     fields,
			FieldsList: list,
		}
	case *WhenStatement:
		cases := make([]*CaseClause, len(n.Cases))
		for i, c := range n.Cases {
			cases[i] = cloneCaseClause(c)
		}
		return &WhenStatement{Token: n.Token, Cases: cases}
	default:
		panic("CloneStatement: nó sem case — adicione aqui e o guard passa")
	}
}

func CloneExpression(e Expression) Expression {
	if e == nil {
		return nil
	}
	switch n := e.(type) {
	case *Identifier:
		return cloneIdentifier(n)
	case *IntegerLiteral:
		return &IntegerLiteral{Token: n.Token, Value: n.Value}
	case *FloatLiteral:
		return &FloatLiteral{Token: n.Token, Value: n.Value}
	case *StringLiteral:
		return &StringLiteral{Token: n.Token, Value: n.Value}
	case *BytesLiteral:
		return &BytesLiteral{Token: n.Token, Value: n.Value}
	case *NullLiteral:
		return &NullLiteral{Token: n.Token}
	case *ZerosLiteral:
		return &ZerosLiteral{Token: n.Token, Size: CloneExpression(n.Size)}
	case *Boolean:
		return &Boolean{Token: n.Token, Value: n.Value}
	case *PrefixExpression:
		return &PrefixExpression{Token: n.Token, Operator: n.Operator, Right: CloneExpression(n.Right)}
	case *InfixExpression:
		return &InfixExpression{Token: n.Token, Left: CloneExpression(n.Left), Operator: n.Operator, Right: CloneExpression(n.Right)}
	case *FunctionLiteral:
		return &FunctionLiteral{
			Token:      n.Token,
			Parameters: cloneParameters(n.Parameters),
			Body:       CloneBlock(n.Body),
			Name:       n.Name,
			ReturnType: CloneType(n.ReturnType),
		}
	case *CallExpression:
		return cloneCallExpression(n)
	case *ArrayLiteral:
		elements := make([]Expression, len(n.Elements))
		for i, el := range n.Elements {
			elements[i] = CloneExpression(el)
		}
		return &ArrayLiteral{Token: n.Token, Elements: elements}
	case *MapLiteral:
		keys := make([]Expression, len(n.Keys))
		values := make([]Expression, len(n.Values))
		pairs := make(map[Expression]Expression, len(n.Pairs))
		for i := range n.Keys {
			k := CloneExpression(n.Keys[i])
			v := CloneExpression(n.Values[i])
			keys[i] = k
			values[i] = v
			pairs[k] = v
		}
		return &MapLiteral{Token: n.Token, Pairs: pairs, Keys: keys, Values: values}
	case *IndexExpression:
		return &IndexExpression{Token: n.Token, Left: CloneExpression(n.Left), Index: CloneExpression(n.Index)}
	case *MemberAccessExpression:
		return &MemberAccessExpression{Token: n.Token, Left: CloneExpression(n.Left), Member: n.Member}
	default:
		panic("CloneExpression: nó sem case — adicione aqui e o guard passa")
	}
}

func CloneType(t NoxyType) NoxyType {
	if t == nil {
		return nil
	}
	switch n := t.(type) {
	case *PrimitiveType:
		return &PrimitiveType{Name: n.Name}
	case *ArrayType:
		return &ArrayType{ElementType: CloneType(n.ElementType), Size: n.Size}
	case *MapType:
		return &MapType{KeyType: CloneType(n.KeyType), ValueType: CloneType(n.ValueType)}
	case *RefType:
		return &RefType{ElementType: CloneType(n.ElementType)}
	case *ChanType:
		return &ChanType{ElementType: CloneType(n.ElementType)}
	case *FunctionType:
		params := make([]NoxyType, len(n.Params))
		for i, p := range n.Params {
			params[i] = CloneType(p)
		}
		return &FunctionType{Params: params, Return: CloneType(n.Return)}
	case *GenericType:
		args := make([]NoxyType, len(n.Args))
		for i, a := range n.Args {
			args[i] = CloneType(a)
		}
		return &GenericType{Name: n.Name, Args: args}
	case *TypeParamType:
		return &TypeParamType{Name: n.Name}
	default:
		panic("CloneType: tipo sem case")
	}
}

func CloneBlock(b *BlockStatement) *BlockStatement {
	if b == nil {
		return nil
	}
	statements := make([]Statement, len(b.Statements))
	for i, s := range b.Statements {
		statements[i] = CloneStatement(s)
	}
	return &BlockStatement{Token: b.Token, Statements: statements}
}

func cloneIdentifier(i *Identifier) *Identifier {
	if i == nil {
		return nil
	}
	clone := *i
	return &clone
}

func cloneParameters(ps []*Parameter) []*Parameter {
	if ps == nil {
		return nil
	}
	clones := make([]*Parameter, len(ps))
	for i, p := range ps {
		clones[i] = &Parameter{Name: p.Name, Type: CloneType(p.Type)}
	}
	return clones
}

func cloneCallExpression(c *CallExpression) *CallExpression {
	if c == nil {
		return nil
	}
	args := make([]Expression, len(c.Arguments))
	for i, a := range c.Arguments {
		args[i] = CloneExpression(a)
	}
	return &CallExpression{Token: c.Token, Function: CloneExpression(c.Function), Arguments: args}
}

func cloneCaseClause(c *CaseClause) *CaseClause {
	if c == nil {
		return nil
	}
	return &CaseClause{
		Token:     c.Token,
		IsDefault: c.IsDefault,
		Condition: CloneStatement(c.Condition),
		Body:      CloneBlock(c.Body),
	}
}

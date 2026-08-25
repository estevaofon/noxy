package compiler

import (
	"fmt"
	"noxy-vm/internal/ast"
)

func normalizeReturnType(t ast.NoxyType) ast.NoxyType {
	if t == nil {
		return &ast.PrimitiveType{Name: "void"}
	}
	return t
}

func newFunctionType(params []*ast.Parameter, result ast.NoxyType) *ast.FunctionType {
	types := make([]ast.NoxyType, len(params))
	for i, param := range params {
		types[i] = param.Type
	}
	return &ast.FunctionType{Params: types, Return: normalizeReturnType(result)}
}

func isBareFunctionType(t ast.NoxyType) bool {
	p, ok := t.(*ast.PrimitiveType)
	return ok && p.Name == "func"
}

func isCallableType(t ast.NoxyType) bool {
	if isBareFunctionType(t) {
		return true
	}
	_, ok := t.(*ast.FunctionType)
	return ok
}

func isNullType(t ast.NoxyType) bool {
	p, ok := t.(*ast.PrimitiveType)
	return ok && p.Name == "null"
}

func (c *Compiler) acceptsNull(t ast.NoxyType) bool {
	if isAny(t) || isNullType(t) {
		return true
	}
	if _, ok := t.(*ast.RefType); ok {
		return true
	}
	primitive, ok := t.(*ast.PrimitiveType)
	if !ok {
		return false
	}
	// Pela DECLARACAO, nao por nome simples em c.structs: o nome pode ser
	// qualificado (`io.File`) ou traduzido de um campo de struct de modulo
	// (`mod_a.Inner` — memberType), e `a.f = null` / `w.o.i = null` tem de
	// valer igual para os tres (#58).
	return c.structDeclaration(primitive.Name) != nil
}

func (c *Compiler) containsCallableType(t ast.NoxyType, visiting map[string]bool) bool {
	switch typed := t.(type) {
	case *ast.FunctionType:
		return true
	case *ast.PrimitiveType:
		if typed.Name == "func" {
			return true
		}
		definition := c.structDeclaration(typed.Name)
		if definition == nil {
			return false
		}
		if visiting == nil {
			visiting = make(map[string]bool)
		}
		if visiting[typed.Name] {
			return false
		}
		visiting[typed.Name] = true
		defer delete(visiting, typed.Name)
		for _, field := range definition.FieldsList {
			if c.containsCallableType(field.Type, visiting) {
				return true
			}
		}
		return false
	case *ast.ArrayType:
		return c.containsCallableType(typed.ElementType, visiting)
	case *ast.MapType:
		return c.containsCallableType(typed.KeyType, visiting) ||
			c.containsCallableType(typed.ValueType, visiting)
	case *ast.ChanType:
		return c.containsCallableType(typed.ElementType, visiting)
	case *ast.RefType:
		return c.containsCallableType(typed.ElementType, visiting)
	default:
		return false
	}
}

func noxyTypeName(t ast.NoxyType) string {
	if t == nil {
		return "unknown"
	}
	return t.String()
}

func callableName(expression ast.Expression) string {
	if identifier, ok := expression.(*ast.Identifier); ok {
		return identifier.Value
	}
	return expression.String()
}

// arithmeticOperators sao os operadores infixos que a VM so executa sobre
// numeros (e, no caso exclusivo de '+', tambem strings/bytes) — nunca sobre
// struct. Usado pelo compilador para recusar 'a + b' com operando struct em
// tempo de compilacao, em vez de deixar estourar no runtime (executor.go,
// "operands must be numbers...").
var arithmeticOperators = map[string]bool{
	"+": true,
	"-": true,
	"*": true,
	"/": true,
	"%": true,
}

// structOperandName devolve o nome do primeiro operando (esquerda, depois
// direita) cujo tipo estatico e um struct registrado em c.structs — "",
// false quando nenhum dos dois e. Usado pelo InfixExpression para montar a
// mensagem do catalogo §9 ("operador '+' não definido para Ponto") com o
// nome do struct ofensor.
func (c *Compiler) structOperandName(leftType, rightType ast.NoxyType) (string, bool) {
	for _, t := range [...]ast.NoxyType{leftType, rightType} {
		if prim, ok := t.(*ast.PrimitiveType); ok {
			if c.structDeclaration(prim.Name) != nil {
				return prim.Name, true
			}
		}
	}
	return "", false
}

// unwrapRefType serve as posicoes que ATRAVESSAM a referencia (R4: base de
// '.' e '[]', memberType); nao e usado para ler um ref como valor.
func unwrapRefType(t ast.NoxyType) ast.NoxyType {
	if ref, ok := t.(*ast.RefType); ok {
		return ref.ElementType
	}
	return t
}

func indexElementType(container ast.NoxyType) ast.NoxyType {
	switch typed := unwrapRefType(container).(type) {
	case *ast.ArrayType:
		return typed.ElementType
	case *ast.MapType:
		return typed.ValueType
	default:
		return nil
	}
}

func sameExactType(left, right ast.NoxyType) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	switch l := left.(type) {
	case *ast.PrimitiveType:
		r, ok := right.(*ast.PrimitiveType)
		return ok && l.Name == r.Name
	case *ast.ArrayType:
		r, ok := right.(*ast.ArrayType)
		return ok && l.Size == r.Size && sameExactType(l.ElementType, r.ElementType)
	case *ast.MapType:
		r, ok := right.(*ast.MapType)
		return ok && sameExactType(l.KeyType, r.KeyType) && sameExactType(l.ValueType, r.ValueType)
	case *ast.ChanType:
		r, ok := right.(*ast.ChanType)
		return ok && sameExactType(l.ElementType, r.ElementType)
	case *ast.RefType:
		r, ok := right.(*ast.RefType)
		return ok && sameExactType(l.ElementType, r.ElementType)
	case *ast.FunctionType:
		r, ok := right.(*ast.FunctionType)
		if !ok || len(l.Params) != len(r.Params) || !sameExactType(l.Return, r.Return) {
			return false
		}
		for i := range l.Params {
			if !sameExactType(l.Params[i], r.Params[i]) {
				return false
			}
		}
		return true
	default:
		return false
	}
}

func (c *Compiler) areStrictTypesCompatible(expected, actual ast.NoxyType) bool {
	if expected == nil {
		return true
	}
	if actual == nil {
		return !c.containsCallableType(expected, nil)
	}
	if isAny(expected) {
		return true
	}
	if isNullType(actual) {
		return c.acceptsNull(expected)
	}
	if isAny(actual) {
		return false
	}
	if isBareFunctionType(expected) {
		return isCallableType(actual)
	}
	if isBareFunctionType(actual) {
		return isBareFunctionType(expected)
	}
	if _, ok := expected.(*ast.FunctionType); ok {
		// sameExactType e uma funcao PURA (compara nomes de folha), entao nao
		// enxerga que `Point` e `geometry.Point` sao a MESMA declaracao: sem o
		// typesEquivalent aqui, `func(Point) -> int` recusaria uma lambda
		// anotada com o nome qualificado. O default abaixo nao cobre este caso
		// — FunctionType sai por este return (#56 §8c).
		return sameExactType(expected, actual) || c.typesEquivalent(expected, actual)
	}
	switch e := expected.(type) {
	case *ast.ArrayType:
		a, ok := actual.(*ast.ArrayType)
		return ok && (e.Size == 0 || e.Size == a.Size) &&
			c.areStrictTypesCompatible(e.ElementType, a.ElementType)
	case *ast.MapType:
		a, ok := actual.(*ast.MapType)
		return ok && c.areStrictTypesCompatible(e.KeyType, a.KeyType) &&
			c.areStrictTypesCompatible(e.ValueType, a.ValueType)
	case *ast.ChanType:
		a, ok := actual.(*ast.ChanType)
		return ok && c.areStrictTypesCompatible(e.ElementType, a.ElementType)
	case *ast.RefType:
		a, ok := actual.(*ast.RefType)
		return ok && c.areStrictTypesCompatible(e.ElementType, a.ElementType)
	default:
		return expected.String() == actual.String() || c.typesEquivalent(expected, actual)
	}
}

func commonInferredType(left, right ast.NoxyType) ast.NoxyType {
	if isCallableType(left) && isCallableType(right) {
		return &ast.PrimitiveType{Name: "func"}
	}
	return &ast.PrimitiveType{Name: "any"}
}

func (c *Compiler) predeclareGlobalBindings(statements []ast.Statement) error {
	seen := make(map[string]struct{})
	letSeen := make(map[string]int)
	for _, statement := range statements {
		switch declaration := statement.(type) {
		case *ast.UseStmt:
			if err := c.predeclareImport(declaration); err != nil {
				return err
			}
		case *ast.FunctionStatement:
			if _, duplicate := seen[declaration.Name]; duplicate {
				return fmt.Errorf("[line %d] duplicate function '%s'", declaration.Token.Line, declaration.Name)
			}
			seen[declaration.Name] = struct{}{}
			// Genericos (§5): um template NAO tem tipo concreto — seus params
			// carregam TypeParamType. Registra-lo aqui o copiaria para
			// c.programBindings, e applyProgramBindings o injetaria de volta em
			// c.globals durante todo corpo de funcao compilado. A identidade do
			// template vive no GenericRegistry; quem entra em globals sao as
			// INSTANCIAS, que o pass 2 prepende como declaracoes comuns e
			// passam por aqui normalmente.
			if len(declaration.TypeParams) > 0 {
				continue
			}
			// §4, terceira familia de hooks: a assinatura pode anotar um struct
			// generico. O predeclare e o PRIMEIRO leitor dessas anotacoes (roda
			// antes de qualquer statement compilar), entao a resolucao tem de
			// acontecer aqui tambem — senao o tipo publicado carregaria
			// `Caixa<int>` enquanto o resto do programa fala `main::Caixa<int>`,
			// e a checagem de tipos de uma chamada adiantada divergiria.
			if err := c.resolveSignatureAnnotations(declaration.Parameters, &declaration.ReturnType, declaration.Token.Line); err != nil {
				return err
			}
			c.globals[declaration.Name] = newFunctionType(declaration.Parameters, declaration.ReturnType)
		case *ast.LetStmt:
			// Mesma regra do check local do LetStmt (compiler.go): dois `let`
			// do mesmo nome no MESMO escopo global e redeclaracao — dentro de
			// um Program via letSeen, entre linhas do REPL via sessionLets
			// (spec §3: a sessao se comporta como um arquivo digitado linha a
			// linha). letSeen e por chamada; sessionLets e so leitura aqui.
			if prevLine, duplicate := letSeen[declaration.Name.Value]; duplicate {
				return fmt.Errorf(
					"[line %d] variable '%s' redeclared in this scope (previous declaration at line %d); hint: to update the value, use '%s = ...' without 'let'",
					declaration.Token.Line, declaration.Name.Value, prevLine, declaration.Name.Value)
			}
			if c.sessionLets != nil {
				if _, duplicate := c.sessionLets[declaration.Name.Value]; duplicate {
					return fmt.Errorf(
						"[line %d] variable '%s' redeclared in this scope (previously declared in this session); hint: to update the value, use '%s = ...' without 'let'",
						declaration.Token.Line, declaration.Name.Value, declaration.Name.Value)
				}
			}
			letSeen[declaration.Name.Value] = declaration.Token.Line
			if c.programLets == nil {
				c.programLets = make(map[string]int)
			}
			c.programLets[declaration.Name.Value] = declaration.Token.Line
			resolved, err := c.resolveAnnotation(declaration.Type, declaration.Token.Line)
			if err != nil {
				return err
			}
			declaration.Type = resolved
			c.globals[declaration.Name.Value] = declaration.Type
		case *ast.StructStatement:
			// Mesma regra do template de funcao: o construtor de um struct
			// generico nao tem assinatura concreta antes da substituicao.
			if len(declaration.TypeParams) > 0 {
				continue
			}
			// Campos ja resolvidos por predeclareStructs (que roda antes); a
			// chamada aqui e o fast path idempotente que mantem este ponto de
			// leitura correto por si.
			if err := c.resolveStructFieldAnnotations(declaration, declaration.Token.Line); err != nil {
				return err
			}
			params := make([]ast.NoxyType, 0, len(declaration.FieldsList))
			for _, field := range declaration.FieldsList {
				params = append(params, field.Type)
			}
			c.globals[declaration.Name] = newStructFunctionType(declaration.Name, params)
		}
	}
	// Segunda varredura: `let` de topo SEM anotacao (issue #41) — infere o
	// tipo do RHS agora que funcoes, structs e lets anotados ja estao em
	// c.globals (ver let_inference.go).
	return c.inferGlobalLetTypes(statements)
}

func newStructFunctionType(name string, params []ast.NoxyType) *ast.FunctionType {
	return &ast.FunctionType{
		Params: params,
		Return: &ast.PrimitiveType{Name: name},
	}
}

func (c *Compiler) predeclareStructs(statements []ast.Statement) error {
	for _, statement := range statements {
		definition, ok := statement.(*ast.StructStatement)
		if ok {
			// Genericos (§5): a tabela de structs e consultada por resolucao de
			// campo e por runtimeTypeInfo, que precisam de tipos concretos. Um
			// template tem campos com TypeParamType — ele fica so no
			// GenericRegistry; as instancias monomorfizadas e que sao
			// StructStatement comuns e entram aqui.
			if len(definition.TypeParams) > 0 {
				continue
			}
			// §4, terceira familia de hooks: campo que anota um struct generico
			// resolve aqui, no primeiro leitor da declaracao — a tabela de
			// structs alimenta resolucao de campo e runtimeTypeInfo, que exigem
			// tipos concretos e de identidade nominal final.
			if err := c.resolveStructFieldAnnotations(definition, definition.Token.Line); err != nil {
				return err
			}
			c.structs[definition.Name] = definition
		}
	}
	return nil
}

func blockGuaranteesReturn(block *ast.BlockStatement) bool {
	if block == nil {
		return false
	}
	for _, statement := range block.Statements {
		if statementGuaranteesReturn(statement) {
			return true
		}
	}
	return false
}

func statementGuaranteesReturn(statement ast.Statement) bool {
	switch s := statement.(type) {
	case *ast.ReturnStmt:
		return true
	case *ast.BlockStatement:
		return blockGuaranteesReturn(s)
	case *ast.IfStatement:
		return s.Alternative != nil &&
			blockGuaranteesReturn(s.Consequence) &&
			blockGuaranteesReturn(s.Alternative)
	case *ast.WhenStatement:
		if len(s.Cases) == 0 {
			return false
		}
		hasDefault := false
		for _, clause := range s.Cases {
			hasDefault = hasDefault || clause.IsDefault
			if !blockGuaranteesReturn(clause.Body) {
				return false
			}
		}
		return hasDefault
	default:
		return false
	}
}

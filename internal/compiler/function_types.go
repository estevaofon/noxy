package compiler

import (
	"fmt"
	"strings"

	"github.com/estevaofon/noxy/internal/ast"
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

// acceptsNull (spec §2.4, issue #105 fase 2): null so entra em `T?`, `any`
// (o buraco dinamico declarado) e no proprio tipo null. Struct e `ref` nus
// NUNCA sao null — o precedente de chan/func (§3) estendido a eles. O
// espelho de runtime e runtimeValueMatchesType/walkRuntimeValueType (VM).
func (c *Compiler) acceptsNull(t ast.NoxyType) bool {
	return isAny(t) || isNullType(t) || isNullable(t)
}

func (c *Compiler) containsCallableType(t ast.NoxyType, visiting map[*ast.StructStatement]bool) bool {
	switch typed := t.(type) {
	case *ast.FunctionType:
		return true
	case *ast.PrimitiveType:
		if typed.Name == "func" {
			return true
		}
		definition := c.structDeclarationOf(typed)
		if definition == nil {
			return false
		}
		if visiting == nil {
			visiting = make(map[*ast.StructStatement]bool)
		}
		// Issue #133: marca de ciclo por DECLARACAO — dois homonimos de
		// modulos distintos nao compartilham a marca.
		if visiting[definition] {
			return false
		}
		visiting[definition] = true
		defer delete(visiting, definition)
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
	case *ast.NullableType:
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
	// Caminho rapido do caso comum.
	if ident, ok := expression.(*ast.Identifier); ok {
		return ident.Value
	}
	// `m.roll(...)` (issue #126 item 2): "argument 1 to 'm.roll'", nao
	// "(m.roll)" como MemberAccessExpression.String() imprime — e o mesmo
	// vale para cadeias mais fundas (`o.inner.cb`), que stableKey ja
	// canoniza (nullable.go) para os hints.
	if key, ok := stableKey(expression); ok {
		return key
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
			if c.structDeclarationOf(prim) != nil {
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
	case *ast.NullableType:
		r, ok := right.(*ast.NullableType)
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
	// Nulidade (spec §2.4): T? aceita T, T? e null; T nao aceita T?.
	if expectedElem, ok := nonNull(expected); ok {
		if actualElem, ok := nonNull(actual); ok {
			return c.areStrictTypesCompatible(expectedElem, actualElem)
		}
		return c.areStrictTypesCompatible(expectedElem, actual)
	}
	if isNullable(actual) {
		return false
	}
	if isAny(actual) {
		// #118: `any` atravessa para um slot concreto em qualquer posicao —
		// argumento e return como o `let` — e a guarda de runtime
		// (emitDynamicBoundaryGuard) confere o valor — para um slot `ref T`,
		// o alvo da referencia (walk TYPE_REF). A mesma regra do tipo
		// desconhecido (actual == nil, acima): a excecao e o callable exato,
		// inclusive aninhado, que nunca sofre narrowing implicito (spec §4.2).
		return !c.containsCallableType(expected, nil)
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
			c.strictCompatibleNested(e.ElementType, a.ElementType)
	case *ast.MapType:
		a, ok := actual.(*ast.MapType)
		return ok && c.strictCompatibleNested(e.KeyType, a.KeyType) &&
			c.strictCompatibleNested(e.ValueType, a.ValueType)
	case *ast.ChanType:
		a, ok := actual.(*ast.ChanType)
		return ok && c.strictCompatibleNested(e.ElementType, a.ElementType)
	case *ast.RefType:
		a, ok := actual.(*ast.RefType)
		return ok && c.strictCompatibleNested(e.ElementType, a.ElementType)
	default:
		return c.typesEquivalent(expected, actual)
	}
}

// strictCompatibleNested e a regra dos tipos ANINHADOS — elemento de array,
// chave/valor de map, payload de chan, alvo de ref: invariantes. `any` so
// atravessa a fronteira dinamica no TOPO do tipo (areStrictTypesCompatible):
// `any[]` nao e `int[]` e `ref any` nao e `ref int` — o slot apontado pode
// mudar de tipo depois da checagem (revisao do #119).
func (c *Compiler) strictCompatibleNested(expected, actual ast.NoxyType) bool {
	if isAny(actual) {
		return isAny(expected)
	}
	return c.areStrictTypesCompatible(expected, actual)
}

func commonInferredType(left, right ast.NoxyType) ast.NoxyType {
	if isCallableType(left) && isCallableType(right) {
		return &ast.PrimitiveType{Name: "func"}
	}
	return &ast.PrimitiveType{Name: "any"}
}

// declareGlobalName registra `name` no namespace global unico do Program
// (issue #47 parte 2) e acusa colisao com qualquer especie ja declarada —
// neste Program (declared) ou em linha anterior da sessao (sessionBindings).
// Mensagens que ja eram contrato (let x let, func x func) nao mudam;
// import x import nunca colide (re-importar e idempotente); func x func
// entre linhas de sessao e permitido (iteracao no REPL).
func (c *Compiler) declareGlobalName(declared map[string]GlobalDecl, name, kind string, line int) error {
	if isInstanceName(name) {
		// Instancia monomorfizada: o modulo que a exporta e o programa que a
		// prependa falam do MESMO struct/funcao gerado — nao e redeclaracao.
		return nil
	}
	if prev, ok := declared[name]; ok && !(kind == "import" && prev.Kind == "import") {
		switch {
		case kind == "variable" && prev.Kind == "variable":
			return fmt.Errorf(
				"[line %d] variable '%s' redeclared in this scope (previous declaration at line %d); hint: to update the value, use '%s = ...' without 'let'",
				line, name, prev.Line, name)
		case kind == "function" && prev.Kind == "function":
			return fmt.Errorf("[line %d] duplicate function '%s'", line, name)
		default:
			return fmt.Errorf("[line %d] '%s' redeclared in this scope (previous declaration as %s at line %d)", line, name, prev.Kind, prev.Line)
		}
	}
	if prev, ok := c.sessionBindings[name]; ok && !(kind == "import" && prev.Kind == "import") && !(kind == "function" && prev.Kind == "function") {
		if kind == "variable" && prev.Kind == "variable" {
			return fmt.Errorf(
				"[line %d] variable '%s' redeclared in this scope (previously declared in this session); hint: to update the value, use '%s = ...' without 'let'",
				line, name, name)
		}
		return fmt.Errorf("[line %d] '%s' redeclared in this scope (previously declared as %s in this session)", line, name, prev.Kind)
	}
	if kind == "variable" && c.sessionLets != nil {
		if _, duplicate := c.sessionLets[name]; duplicate {
			return fmt.Errorf(
				"[line %d] variable '%s' redeclared in this scope (previously declared in this session); hint: to update the value, use '%s = ...' without 'let'",
				line, name, name)
		}
	}
	declared[name] = GlobalDecl{Kind: kind, Line: line}
	if c.programBindingsByKind == nil {
		c.programBindingsByKind = make(map[string]GlobalDecl)
	}
	c.programBindingsByKind[name] = GlobalDecl{Kind: kind, Line: line}
	return nil
}

// importedNames devolve os nomes que um `use` vincula no escopo do
// importador: os seletores, todos os exports (select *) ou o alias/ultimo
// segmento do modulo (forma namespace).
func (c *Compiler) importedNames(declaration *ast.UseStmt) []string {
	switch {
	case declaration.SelectAll:
		exports, _ := c.discoverModuleExports(declaration.Module)
		names := make([]string, 0, len(exports))
		for name := range exports {
			names = append(names, name)
		}
		return names
	case len(declaration.Selectors) > 0:
		return declaration.Selectors
	default:
		name := declaration.Alias
		if name == "" {
			parts := strings.Split(declaration.Module, ".")
			name = parts[len(parts)-1]
		}
		return []string{name}
	}
}

func (c *Compiler) predeclareGlobalBindings(statements []ast.Statement) error {
	declared := make(map[string]GlobalDecl)
	for _, statement := range statements {
		switch declaration := statement.(type) {
		case *ast.UseStmt:
			for _, name := range c.importedNames(declaration) {
				if err := c.declareGlobalName(declared, name, "import", declaration.Token.Line); err != nil {
					return err
				}
			}
			if err := c.predeclareImport(declaration); err != nil {
				return err
			}
		case *ast.FunctionStatement:
			if err := c.declareGlobalName(declared, declaration.Name, "function", declaration.Token.Line); err != nil {
				return err
			}
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
			// um Program via declared, entre linhas do REPL via sessionLets/
			// sessionBindings (spec §3: a sessao se comporta como um arquivo
			// digitado linha a linha).
			if err := c.declareGlobalName(declared, declaration.Name.Value, "variable", declaration.Token.Line); err != nil {
				return err
			}
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
			if err := c.declareGlobalName(declared, declaration.Name, "struct", declaration.Token.Line); err != nil {
				return err
			}
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
			c.globals[declaration.Name] = newStructFunctionType(declaration, params)
		}
	}
	// Segunda varredura: `let` de topo SEM anotacao (issue #41) — infere o
	// tipo do RHS agora que funcoes, structs e lets anotados ja estao em
	// c.globals (ver let_inference.go).
	return c.inferGlobalLetTypes(statements)
}

// newStructFunctionType e o tipo do construtor de decl: o retorno carrega a
// DECLARACAO (issue #133), exceto para instancia generica, que segue por
// nome (spec §1.6).
func newStructFunctionType(decl *ast.StructStatement, params []ast.NoxyType) *ast.FunctionType {
	result := &ast.PrimitiveType{Name: decl.Name}
	if !isGenericInstanceName(decl.Name) {
		result.Decl = decl
	}
	return &ast.FunctionType{Params: params, Return: result}
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

package compiler

import (
	"strings"

	"noxy-vm/internal/ast"
)

// instanceName monta o nome qualificado de uma instancia monomorfizada:
// "<modulo>::<base><arg1,arg2,...>", com args por String() e separados por
// virgula SEM espaco (deliberadamente diferente de GenericType.String(), que
// usa ", " — instanceName produz um identificador estavel para uso como
// chave de registro/global, nao uma representacao "bonita" de tipo).
func instanceName(module, base string, args []ast.NoxyType) string {
	parts := make([]string, len(args))
	for i, a := range args {
		parts[i] = a.String()
	}
	return module + "::" + base + "<" + strings.Join(parts, ",") + ">"
}

// substituteType clona t substituindo todo TypeParamType cujo nome tenha
// binding em b pelo tipo bindado (tambem clonado, para nao compartilhar nos
// entre instancias). TypeParamType sem binding e preservado como esta
// (parametro de tipo ainda livre — nao deveria ocorrer em uso normal, ja que
// b cobre todos os TypeParams do template, mas preferimos preservar a
// deixar nil).
//
// GenericType mantem sua forma (Name + Args substituidos): mesmo que todos
// os Args fiquem concretos apos a substituicao, a resolucao para o nome
// qualificado da instancia (e.g. GenericType{Node, [int]} -> "main::Node<int>")
// acontece na Task 9, nao aqui.
func substituteType(t ast.NoxyType, b map[string]ast.NoxyType) ast.NoxyType {
	if t == nil {
		return nil
	}
	switch n := t.(type) {
	case *ast.TypeParamType:
		if bound, ok := b[n.Name]; ok {
			return ast.CloneType(bound)
		}
		return &ast.TypeParamType{Name: n.Name}
	case *ast.PrimitiveType:
		return &ast.PrimitiveType{Name: n.Name}
	case *ast.ArrayType:
		return &ast.ArrayType{ElementType: substituteType(n.ElementType, b), Size: n.Size}
	case *ast.MapType:
		return &ast.MapType{KeyType: substituteType(n.KeyType, b), ValueType: substituteType(n.ValueType, b)}
	case *ast.RefType:
		return &ast.RefType{ElementType: substituteType(n.ElementType, b)}
	case *ast.ChanType:
		return &ast.ChanType{ElementType: substituteType(n.ElementType, b)}
	case *ast.FunctionType:
		params := make([]ast.NoxyType, len(n.Params))
		for i, p := range n.Params {
			params[i] = substituteType(p, b)
		}
		return &ast.FunctionType{Params: params, Return: substituteType(n.Return, b)}
	case *ast.GenericType:
		args := make([]ast.NoxyType, len(n.Args))
		for i, a := range n.Args {
			args[i] = substituteType(a, b)
		}
		return &ast.GenericType{Name: n.Name, Args: args}
	default:
		return t
	}
}

// substituteFunction clona tpl.Decl (Task 2), zera TypeParams, renomeia para
// name e substitui, in-place no clone, todo campo de tipo alcancavel a
// partir da declaracao: Parameters, ReturnType e toda anotacao de tipo no
// corpo (LetStmt.Type, FunctionLiteral aninhado, etc. — via
// substituteInBlock/substituteInStatement/substituteInExpression). O
// template original (tpl.Decl) nunca e tocado.
func substituteFunction(tpl *FuncTemplate, b map[string]ast.NoxyType, name string) *ast.FunctionStatement {
	clone := ast.CloneStatement(tpl.Decl).(*ast.FunctionStatement)
	clone.Name = name
	clone.TypeParams = nil
	for _, p := range clone.Parameters {
		p.Type = substituteType(p.Type, b)
	}
	clone.ReturnType = substituteType(clone.ReturnType, b)
	substituteInBlock(clone.Body, b)
	return clone
}

// substituteStruct e o analogo de substituteFunction para structs: clona
// tpl.Decl, zera TypeParams, renomeia e substitui os tipos nos dois
// espelhos de campos (FieldsList, a fonte de verdade ordenada, e Fields, o
// mapa por nome) de forma consistente — ambos apontam para os mesmos nos de
// tipo apos a substituicao.
func substituteStruct(tpl *StructTemplate, b map[string]ast.NoxyType, name string) *ast.StructStatement {
	clone := ast.CloneStatement(tpl.Decl).(*ast.StructStatement)
	clone.Name = name
	clone.TypeParams = nil
	fields := make(map[string]ast.NoxyType, len(clone.FieldsList))
	for _, f := range clone.FieldsList {
		f.Type = substituteType(f.Type, b)
		fields[f.Name] = f.Type
	}
	clone.Fields = fields
	return clone
}

// substituteInBlock percorre cada statement de um bloco substituindo campos
// de tipo in-place. Usado para o corpo de funcoes (substituteFunction) e
// recursivamente para blocos aninhados (if/while/for/when/func literal).
func substituteInBlock(blk *ast.BlockStatement, b map[string]ast.NoxyType) {
	if blk == nil {
		return
	}
	for _, s := range blk.Statements {
		substituteInStatement(s, b)
	}
}

// substituteInStatement anda no clone de um statement trocando somente
// campos de tipo (NoxyType), recursando em sub-statements/expressions para
// alcancar anotacoes aninhadas (ex.: let dentro de if dentro de while).
// Cobre todo case de ast.CloneStatement — nó sem case aqui e bug (panic).
//
// A exaustividade e verificada estaticamente por
// TestGenericWalkersCoverEveryNode (generics_walkers_guard_test.go), o
// equivalente para estes walkers do que ast.TestClonerCoversEveryNode e para
// o cloner: o panic sozinho so dispara se algum teste exercitar exatamente o
// nó novo, e um nó esquecido aqui significa instancia com anotacao NAO
// substituida — TypeParamType vazando para o pass 2.
func substituteInStatement(s ast.Statement, b map[string]ast.NoxyType) {
	if s == nil {
		return
	}
	switch n := s.(type) {
	case *ast.LetStmt:
		n.Type = substituteType(n.Type, b)
		substituteInExpression(n.Value, b)
	case *ast.AssignStmt:
		substituteInExpression(n.Target, b)
		substituteInExpression(n.Value, b)
	case *ast.ReturnStmt:
		substituteInExpression(n.ReturnValue, b)
	case *ast.DeferStmt:
		substituteInExpression(n.Call, b)
	case *ast.BreakStmt:
		// sem tipo, sem sub-no.
	case *ast.ContinueStmt:
		// sem tipo, sem sub-no.
	case *ast.UseStmt:
		// sem tipo, sem sub-no.
	case *ast.ExpressionStmt:
		substituteInExpression(n.Expression, b)
	case *ast.BlockStatement:
		substituteInBlock(n, b)
	case *ast.IfStatement:
		substituteInExpression(n.Condition, b)
		substituteInBlock(n.Consequence, b)
		substituteInBlock(n.Alternative, b)
	case *ast.WhileStatement:
		substituteInExpression(n.Condition, b)
		substituteInBlock(n.Body, b)
	case *ast.FunctionStatement:
		// func aninhado (se o parser permitir): substitui seus proprios
		// campos de tipo tambem, pelo mesmo binding do escopo externo.
		for _, p := range n.Parameters {
			p.Type = substituteType(p.Type, b)
		}
		n.ReturnType = substituteType(n.ReturnType, b)
		substituteInBlock(n.Body, b)
	case *ast.ForStatement:
		substituteInExpression(n.Collection, b)
		substituteInBlock(n.Body, b)
	case *ast.StructStatement:
		// struct aninhado (se o parser permitir): mesma logica dos dois
		// espelhos de substituteStruct.
		fields := make(map[string]ast.NoxyType, len(n.FieldsList))
		for _, f := range n.FieldsList {
			f.Type = substituteType(f.Type, b)
			fields[f.Name] = f.Type
		}
		n.Fields = fields
	case *ast.WhenStatement:
		for _, c := range n.Cases {
			substituteInStatement(c.Condition, b)
			substituteInBlock(c.Body, b)
		}
	default:
		panic("substituteInStatement: nó sem case — adicione aqui e o guard passa")
	}
}

// substituteInExpression e o analogo de substituteInStatement para
// expressoes: a unica expressao com campos de tipo diretos e FunctionLiteral
// (Parameters/ReturnType); as demais so precisam de recursao estrutural para
// alcancar FunctionLiteral/LetStmt aninhados em profundidade.
func substituteInExpression(e ast.Expression, b map[string]ast.NoxyType) {
	if e == nil {
		return
	}
	switch n := e.(type) {
	case *ast.Identifier, *ast.IntegerLiteral, *ast.FloatLiteral, *ast.StringLiteral,
		*ast.BytesLiteral, *ast.NullLiteral, *ast.Boolean:
		// sem tipo, sem sub-no.
	case *ast.ZerosLiteral:
		substituteInExpression(n.Size, b)
	case *ast.PrefixExpression:
		substituteInExpression(n.Right, b)
	case *ast.InfixExpression:
		substituteInExpression(n.Left, b)
		substituteInExpression(n.Right, b)
	case *ast.FunctionLiteral:
		for _, p := range n.Parameters {
			p.Type = substituteType(p.Type, b)
		}
		n.ReturnType = substituteType(n.ReturnType, b)
		substituteInBlock(n.Body, b)
	case *ast.CallExpression:
		substituteInExpression(n.Function, b)
		for _, a := range n.Arguments {
			substituteInExpression(a, b)
		}
	case *ast.ArrayLiteral:
		for _, el := range n.Elements {
			substituteInExpression(el, b)
		}
	case *ast.MapLiteral:
		// Keys/Values sao os espelhos ordenados; Pairs mapeia por
		// identidade de ponteiro para os mesmos nos — mutar os nos
		// apontados por Keys/Values in-place mantem Pairs consistente sem
		// reconstrucao.
		for i := range n.Keys {
			substituteInExpression(n.Keys[i], b)
		}
		for i := range n.Values {
			substituteInExpression(n.Values[i], b)
		}
	case *ast.IndexExpression:
		substituteInExpression(n.Left, b)
		substituteInExpression(n.Index, b)
	case *ast.MemberAccessExpression:
		substituteInExpression(n.Left, b)
	default:
		panic("substituteInExpression: nó sem case — adicione aqui e o guard passa")
	}
}

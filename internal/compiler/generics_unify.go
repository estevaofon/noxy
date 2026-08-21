package compiler

import (
	"fmt"

	"noxy-vm/internal/ast"
)

// conflictError e o erro estruturado que unify devolve quando um parametro de
// tipo ja tem binding e o novo valor observado diverge (comparacao por
// String()). Error() produz exatamente a mesma mensagem "T inferido como X e
// Y" que TestUnifyTable/TestInferenceConflictError ja verificam por
// substring — nada muda para quem so olha err.Error(). O que o tipo
// estruturado acrescenta e os campos Param/Existing/New, que um chamador com
// mais contexto (compileGenericCallSite, generics.go) pode extrair via
// errors.As para compor a mensagem do §9 com atribuicao por argumento ("T
// inferido como int (argumento 1) e string (argumento 2)") sem parsear
// texto.
type conflictError struct {
	Param    string
	Existing ast.NoxyType
	New      ast.NoxyType
}

func (e *conflictError) Error() string {
	return fmt.Sprintf("%s inferido como %s e %s", e.Param, e.Existing.String(), e.New.String())
}

// unify casa estruturalmente expected (um tipo de template, possivelmente
// contendo TypeParamType) contra actual (um tipo concreto observado num site
// de chamada), acumulando bindings de parâmetro de tipo em bindings.
//
// Regras (§7 da spec):
//   - TypeParamType em posição expected binda actual (uma cópia via
//     ast.CloneType, para não compartilhar nós entre o template e a
//     instância inferida).
//   - actual == ref é proibido como alvo de binding: erro "não pode ser um
//     tipo ref".
//   - actual any/null não contribui binding (não é erro; unify retorna nil
//     sem tocar em bindings).
//   - Se T já tem binding e o novo valor diverge (comparação por String()),
//     erro de conflito contendo "inferido como".
//   - expected concreto (sem TypeParamType) delega para comparação
//     estrutural por construtor; mismatch de construtor é erro contendo
//     "esperava".
//   - GenericType unifica Args ponto a ponto (mesmo Name e aridade).
//
// Compatibilidade "nunca mais estrita que areTypesCompatible": unify é
// consumida durante a inferência de tipos em sites de chamada genéricos, e
// não pode rejeitar uma chamada que o checador de tipos "de verdade"
// (Compiler.areTypesCompatible, compiler.go) aceitaria — isso quebraria
// chamadas estruturalmente válidas. Por isso, na posição expected concreta
// (fora de um TypeParamType), replicamos as mesmas três folgas de
// areTypesCompatible antes de comparar construtor a construtor: expected
// "any" aceita qualquer actual; actual "any" ou "null" nunca é erro (não
// binda, não falha); expected "func" (bare) aceita qualquer actual chamável
// (isCallableType: FunctionType ou "func" bare). Reusamos os helpers de
// pacote isAny/isNullType/isBareFunctionType/isCallableType (definidos em
// compiler.go e function_types.go) para não duplicar essas regras com uma
// semântica levemente diferente.
//
// O que NÃO replicamos aqui é Compiler.acceptsNull (que decide se um actual
// null é aceito por um expected nomeado consultando c.structs) — unify é uma
// função pura, sem acesso à tabela de structs do compilador. Nesses casos
// "não temos certeza": preferimos não bindar e não errar, e deixar a
// checagem de tipos da pass 2 (com o *Compiler completo) decidir depois. Ou
// seja, unify pode ser mais permissiva que areTypesCompatible em casos que
// dependem de contexto que ela não tem — nunca mais estrita.
//
// nil: expected ou actual nil é tratado conservadoramente — sem binding, sem
// erro. Ambos os lados nil não deveriam ocorrer em uso real (a spec exige
// tipos resolvidos antes de chamar unify); optamos por não travar aqui e
// deixar validações posteriores (Tasks 8/10/11) pegarem tipos ausentes com
// mensagens mais específicas ao contexto delas.
func unify(expected, actual ast.NoxyType, bindings map[string]ast.NoxyType) error {
	if expected == nil || actual == nil {
		return nil
	}

	if tp, ok := expected.(*ast.TypeParamType); ok {
		if _, isRef := actual.(*ast.RefType); isRef {
			return fmt.Errorf("%s não pode ser um tipo ref (tentativa de bindar %s)", tp.Name, actual.String())
		}
		if isAny(actual) || isNullType(actual) {
			return nil
		}
		if existing, ok := bindings[tp.Name]; ok {
			if !looselySameType(existing, actual) {
				return &conflictError{Param: tp.Name, Existing: existing, New: actual}
			}
			return nil
		}
		bindings[tp.Name] = ast.CloneType(actual)
		return nil
	}

	// expected concreto: folgas de compatibilidade que espelham
	// areTypesCompatible antes de exigir casamento de construtor (ver
	// comentário da função).
	if isAny(expected) {
		return nil
	}
	if isAny(actual) || isNullType(actual) {
		return nil
	}
	if isBareFunctionType(expected) {
		if isCallableType(actual) {
			return nil
		}
		return fmt.Errorf("esperava %s, encontrado %s", expected.String(), actual.String())
	}

	switch exp := expected.(type) {
	case *ast.PrimitiveType:
		act, ok := actual.(*ast.PrimitiveType)
		if !ok || act.Name != exp.Name {
			return fmt.Errorf("esperava %s, encontrado %s", expected.String(), actual.String())
		}
		return nil

	case *ast.ArrayType:
		act, ok := actual.(*ast.ArrayType)
		if !ok {
			return fmt.Errorf("esperava %s, encontrado %s", expected.String(), actual.String())
		}
		return unify(exp.ElementType, act.ElementType, bindings)

	case *ast.MapType:
		act, ok := actual.(*ast.MapType)
		if !ok {
			return fmt.Errorf("esperava %s, encontrado %s", expected.String(), actual.String())
		}
		if err := unify(exp.KeyType, act.KeyType, bindings); err != nil {
			return err
		}
		return unify(exp.ValueType, act.ValueType, bindings)

	case *ast.RefType:
		act, ok := actual.(*ast.RefType)
		if !ok {
			return fmt.Errorf("esperava %s, encontrado %s", expected.String(), actual.String())
		}
		return unify(exp.ElementType, act.ElementType, bindings)

	case *ast.ChanType:
		act, ok := actual.(*ast.ChanType)
		if !ok {
			return fmt.Errorf("esperava %s, encontrado %s", expected.String(), actual.String())
		}
		return unify(exp.ElementType, act.ElementType, bindings)

	case *ast.FunctionType:
		act, ok := actual.(*ast.FunctionType)
		if !ok {
			return fmt.Errorf("esperava %s, encontrado %s", expected.String(), actual.String())
		}
		if len(exp.Params) != len(act.Params) {
			return fmt.Errorf("esperava %s, encontrado %s", expected.String(), actual.String())
		}
		for i, p := range exp.Params {
			if err := unify(p, act.Params[i], bindings); err != nil {
				return err
			}
		}
		return unify(exp.Return, act.Return, bindings)

	case *ast.GenericType:
		act, ok := actual.(*ast.GenericType)
		if !ok {
			return fmt.Errorf("esperava %s, encontrado %s", expected.String(), actual.String())
		}
		if exp.Name != act.Name || len(exp.Args) != len(act.Args) {
			return fmt.Errorf("esperava %s, encontrado %s", expected.String(), actual.String())
		}
		for i, a := range exp.Args {
			if err := unify(a, act.Args[i], bindings); err != nil {
				return err
			}
		}
		return nil

	default:
		return fmt.Errorf("esperava %s, encontrado %s", expected.String(), actual.String())
	}
}

// containsTypeParam reporta se t contém, em qualquer profundidade, um
// TypeParamType — ou seja, se t ainda é um tipo de template não instanciado.
// Exportado dentro do pacote (nome exato containsTypeParam) porque Tasks
// 8/10/11 consomem esta função para decidir quando delegar para unify em vez
// de comparação estrutural direta.
func containsTypeParam(t ast.NoxyType) bool {
	if t == nil {
		return false
	}
	switch n := t.(type) {
	case *ast.TypeParamType:
		return true
	case *ast.PrimitiveType:
		return false
	case *ast.ArrayType:
		return containsTypeParam(n.ElementType)
	case *ast.MapType:
		return containsTypeParam(n.KeyType) || containsTypeParam(n.ValueType)
	case *ast.RefType:
		return containsTypeParam(n.ElementType)
	case *ast.ChanType:
		return containsTypeParam(n.ElementType)
	case *ast.FunctionType:
		for _, p := range n.Params {
			if containsTypeParam(p) {
				return true
			}
		}
		return containsTypeParam(n.Return)
	case *ast.GenericType:
		for _, a := range n.Args {
			if containsTypeParam(a) {
				return true
			}
		}
		return false
	default:
		return false
	}
}

package compiler

// Terceira familia de hooks (spec §4): toda resolucao de GenericType em
// posicao de anotacao instancia o struct. Aqui ficam os contratos de
// compilador — validacao de aridade/escopo do §9, memoizacao por tupla e
// ordem das declaracoes sinteticas.

import (
	"strings"
	"testing"

	"noxy-vm/internal/ast"
	"noxy-vm/internal/chunk"
	"noxy-vm/internal/value"
)

func TestGenericStructArityError(t *testing.T) {
	_, _, err := New().Compile(parse("struct Caixa<T>\n    valor: T\nend\nlet c: Caixa<int, string> = null"))
	if err == nil || !strings.Contains(err.Error(), "espera 1 argumento de tipo, recebeu 2") {
		t.Fatalf("esperava erro de aridade, veio %v", err)
	}
}

// §9: parametro de tipo fora do template nao e um tipo. Na fonte, `T` fora de
// uma declaracao generica nem chega como TypeParamType — o parser so produz
// esse no dentro de um template (activeTypeParams), entao ali `T` e apenas um
// nome de tipo desconhecido e o erro vem da checagem de tipos normal. O no
// TypeParamType alcanca resolveAnnotation apenas por uma substituicao que
// deixou parametro de tipo livre, e a mensagem do catalogo cobre esse caso.
func TestTypeParamAnnotationIsRejected(t *testing.T) {
	_, err := New().resolveAnnotation(&ast.TypeParamType{Name: "T"}, 4)
	want := "[line 4] tipo 'T' não declarado"
	if err == nil || err.Error() != want {
		t.Fatalf("erro = %v, quer %q", err, want)
	}
	// Composicao: o parametro de tipo livre e reportado em qualquer profundidade.
	_, err = New().resolveAnnotation(&ast.ArrayType{ElementType: &ast.TypeParamType{Name: "U"}}, 9)
	if err == nil || err.Error() != "[line 9] tipo 'U' não declarado" {
		t.Fatalf("erro = %v, quer o mesmo erro para U na linha 9", err)
	}
}

// GenericType cujo Name nao e template: nao existe nada para instanciar.
func TestUnknownGenericTypeNameError(t *testing.T) {
	_, _, err := New().Compile(parse("func id<T>(x: T) -> T\n    return x\nend\nlet c: Caixa<int> = null"))
	if err == nil || !strings.Contains(err.Error(), "'Caixa' não é um tipo genérico declarado") {
		t.Fatalf("esperava erro de template inexistente, veio %v", err)
	}
}

// instanceStructNames lista, na ordem, as instancias de struct que o pass 1
// prependou (instancia = nome com o qualificador de modulo, §4).
func instanceStructNames(program *ast.Program) []string {
	names := []string{}
	for _, statement := range program.Statements {
		if structDecl, ok := statement.(*ast.StructStatement); ok && strings.Contains(structDecl.Name, "::") {
			names = append(names, structDecl.Name)
		}
	}
	return names
}

// Memoizacao por tupla: a anotacao e o construtor pedem a MESMA instancia, e
// ela e declarada uma unica vez.
func TestGenericStructMemoizedAcrossAnnotationAndConstructor(t *testing.T) {
	program := parse("struct Caixa<T>\n    valor: T\nend\nlet a: Caixa<int> = Caixa(1)\nlet b: Caixa<int> = Caixa(2)")
	if _, _, err := New().Compile(program); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(instanceStructNames(program), " "); got != "main::Caixa<int>" {
		t.Fatalf("instancias prependadas = %q, quer apenas main::Caixa<int>", got)
	}
}

// §4/§5: cascata Caixa<Caixa<int>> resolve a instancia interna ANTES da
// externa, e a ordem da fila (dependencia antes de dependente) e o que faz o
// pass 2 compilar tudo pelo caminho normal.
func TestNestedGenericStructOrdersDependencyFirst(t *testing.T) {
	program := parse("struct Caixa<T>\n    valor: T\nend\nlet d: Caixa<Caixa<int>>? = null")
	if _, _, err := New().Compile(program); err != nil {
		t.Fatal(err)
	}
	got := strings.Join(instanceStructNames(program), " ")
	want := "main::Caixa<int> main::Caixa<main::Caixa<int>>"
	if got != want {
		t.Fatalf("ordem = %q, quer %q", got, want)
	}
}

// Instancia de struct entra na fila antes da instancia de funcao que a usa: o
// pass 2 compila statements em ordem e o construtor precisa existir como
// global antes do corpo que o chama rodar.
func TestStructInstancePrecedesFunctionInstance(t *testing.T) {
	program := parse(`struct Caixa<T>
    valor: T
end
func embala<T>(v: T) -> Caixa<T>
    let out: Caixa<T> = Caixa(v)
    return out
end
let c: Caixa<int> = embala(7)`)
	if _, _, err := New().Compile(program); err != nil {
		t.Fatal(err)
	}
	structIndex, funcIndex := -1, -1
	for index, statement := range program.Statements {
		switch decl := statement.(type) {
		case *ast.StructStatement:
			if decl.Name == "main::Caixa<int>" {
				structIndex = index
			}
		case *ast.FunctionStatement:
			if decl.Name == "main::embala<int>" {
				funcIndex = index
			}
		}
	}
	if structIndex < 0 || funcIndex < 0 {
		t.Fatalf("instancias nao encontradas (struct=%d, func=%d)", structIndex, funcIndex)
	}
	if structIndex > funcIndex {
		t.Fatalf("struct em %d vem depois da funcao em %d", structIndex, funcIndex)
	}
}

// §9: erro na resolucao dos CAMPOS de uma instancia carrega a cadeia de
// instanciacao, com o nome de exibicao (sem qualificador de modulo).
func TestFieldErrorInsideInstanceCarriesChain(t *testing.T) {
	_, _, err := New().Compile(parse(`struct Caixa<T>
    valor: T
end
struct Par<A>
    dentro: Caixa<A, A>
end
let p: Par<int>`))
	if err == nil {
		t.Fatal("esperava erro de aridade dentro do campo da instancia")
	}
	for _, fragment := range []string{"em Par<int> (instanciado na linha 7)", "espera 1 argumento de tipo, recebeu 2"} {
		if !strings.Contains(err.Error(), fragment) {
			t.Fatalf("erro %q nao contem %q", err.Error(), fragment)
		}
	}
}

// structConstant acha a definicao de struct que o pass 2 emitiu como constante
// para um nome de instancia.
func structConstant(t *testing.T, code *chunk.Chunk, name string) *value.ObjStruct {
	t.Helper()
	for _, constant := range code.Constants {
		definition, ok := constant.Obj.(*value.ObjStruct)
		if ok && definition.Name == name {
			return definition
		}
	}
	t.Fatalf("definicao de struct %q nao encontrada nas constantes", name)
	return nil
}

// §10: a instancia e um StructStatement comum pos-substituicao, entao ela sai do
// pass 2 com tudo que um struct escrito a mao tem — tipo de construtor com a
// identidade nominal qualificada e JSONDynamicFields para os campos `any`
// (inclusive quando o `any` vem do argumento de tipo).
func TestGenericStructInstanceIsAnOrdinaryStruct(t *testing.T) {
	code, _, err := New().Compile(parse(`struct Caixa<T>
    valor: T,
    extra: any
end
let c: Caixa<int> = Caixa(1, null)
let d: Caixa<any>?`))
	if err != nil {
		t.Fatal(err)
	}
	concrete := structConstant(t, code, "main::Caixa<int>")
	if concrete.JSONDynamicFields["valor"] {
		t.Fatal("campo 'valor' de Caixa<int> nao deveria ser dinamico")
	}
	if !concrete.JSONDynamicFields["extra"] {
		t.Fatal("campo 'extra' (any) deveria estar em JSONDynamicFields")
	}
	if concrete.ConstructorType == nil || concrete.ConstructorType.Return == nil {
		t.Fatal("instancia sem tipo de construtor")
	}
	if got := concrete.ConstructorType.Return.Name; got != "main::Caixa<int>" {
		t.Fatalf("retorno do construtor = %q, quer main::Caixa<int>", got)
	}
	// T=any: a substituicao propaga a dinamicidade para o campo do parametro.
	if !structConstant(t, code, "main::Caixa<any>").JSONDynamicFields["valor"] {
		t.Fatal("campo 'valor' de Caixa<any> deveria ser dinamico")
	}
}

// Instancia pedida DENTRO de um corpo de funcao: o compilador do corpo e um
// filho com copias proprias de structs/globals, e e nele que a instancia tem de
// ficar visivel (member access e construtor) — mesmo quando o memo ja foi criado
// por outro compilador.
func TestGenericStructInstanceVisibleInsideFunctionBody(t *testing.T) {
	if _, _, err := New().Compile(parse(`struct Caixa<T>
    valor: T
end
let global: Caixa<int> = Caixa(1)
func soma() -> int
    let local: Caixa<int> = Caixa(41)
    return local.valor + global.valor
end
soma()`)); err != nil {
		t.Fatal(err)
	}
}

// A anotacao e reescrita in-place para o nome qualificado da instancia: e o
// que faz o pass 2 (e a validacao CoW de runtime) tratar a instancia como um
// struct nominal comum.
func TestAnnotationRewrittenToQualifiedName(t *testing.T) {
	program := parse("struct Caixa<T>\n    valor: T\nend\nlet xs: Caixa<int>[] = []")
	c := New()
	if _, _, err := c.Compile(program); err != nil {
		t.Fatal(err)
	}
	var letStmt *ast.LetStmt
	for _, statement := range program.Statements {
		if candidate, ok := statement.(*ast.LetStmt); ok && candidate.Name.Value == "xs" {
			letStmt = candidate
		}
	}
	if letStmt == nil {
		t.Fatal("let 'xs' desapareceu do programa")
	}
	arrayType, ok := letStmt.Type.(*ast.ArrayType)
	if !ok {
		t.Fatalf("tipo do let = %T, quer *ast.ArrayType", letStmt.Type)
	}
	element, ok := arrayType.ElementType.(*ast.PrimitiveType)
	if !ok || element.Name != "main::Caixa<int>" {
		t.Fatalf("elemento = %s, quer main::Caixa<int>", arrayType.ElementType.String())
	}
	if _, registered := c.structs["main::Caixa<int>"]; !registered {
		t.Fatal("instancia nao registrada em c.structs")
	}
}

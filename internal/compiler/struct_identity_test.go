package compiler

import (
	"testing"

	"noxy-vm/internal/ast"
	"noxy-vm/internal/lexer"
	"noxy-vm/internal/parser"
)

// Issue #133: o tipo de struct carrega a identidade da declaracao (Decl);
// Name e so a grafia. Clonar ou substituir um tipo copia o PONTEIRO — clonar
// a declaracao quebraria a identidade em silencio.
func TestCloneAndSubstitutePreserveStructDecl(t *testing.T) {
	decl := &ast.StructStatement{Name: "P"}
	original := &ast.ArrayType{ElementType: &ast.PrimitiveType{Name: "P", Decl: decl}}

	cloned := ast.CloneType(original).(*ast.ArrayType).ElementType.(*ast.PrimitiveType)
	if cloned.Decl != decl {
		t.Fatalf("CloneType lost Decl: got %p, want %p", cloned.Decl, decl)
	}
	substituted := substituteType(original, map[string]ast.NoxyType{}).(*ast.ArrayType).ElementType.(*ast.PrimitiveType)
	if substituted.Decl != decl {
		t.Fatalf("substituteType lost Decl: got %p, want %p", substituted.Decl, decl)
	}
}

func TestResolveAnnotationBindsDeclInPlaceWithoutAllocating(t *testing.T) {
	// Um `let` anotado com composto de struct (P[], ref P, P?, map, func):
	// o no da anotacao volta pelo MESMO ponteiro (fast path intacto) e cada
	// PrimitiveType de struct dentro dele sai com Decl preenchido.
	src := `struct P
    x: int
end
let a: P[] = []
let b: map[string, P] = {}
let f: func(P) -> P? = func(p: P) -> P? return null end
let p: P = P(1)
let r: ref P = ref p
`
	program := parseForTest(t, src)
	// Os ponteiros das anotacoes ANTES de compilar: e a propriedade "in place"
	// do nome do teste — resolveAnnotation preenche Decl no proprio no, nunca
	// devolve um no novo.
	before := make(map[*ast.LetStmt]ast.NoxyType)
	for _, statement := range program.Statements {
		if let, ok := statement.(*ast.LetStmt); ok && let.Type != nil {
			before[let] = let.Type
		}
	}
	if len(before) != 5 {
		t.Fatalf("anotacoes capturadas: %d, want 5", len(before))
	}
	c := NewWithState(make(map[string]ast.NoxyType), make(map[string]*ast.StructStatement), "main.nx")
	if _, _, err := c.Compile(program); err != nil {
		t.Fatalf("compile: %v", err)
	}
	decl := c.structs["P"]
	if decl == nil {
		t.Fatal("struct P not registered")
	}
	for _, statement := range program.Statements {
		let, ok := statement.(*ast.LetStmt)
		if !ok {
			continue
		}
		if original, seen := before[let]; seen && let.Type != original {
			t.Fatalf("let %s: anotacao trocada de no (%p -> %p), nao foi resolvida in place",
				let.Name.Value, original, let.Type)
		}
		var prims []*ast.PrimitiveType
		collectStructPrimitives(let.Type, &prims)
		if len(prims) == 0 {
			t.Fatalf("let %s: no struct primitives found in %s", let.Name.Value, let.Type)
		}
		for _, prim := range prims {
			if prim.Decl != decl {
				t.Fatalf("let %s: %s has Decl %p, want %p", let.Name.Value, prim.Name, prim.Decl, decl)
			}
		}
	}
}

// collectStructPrimitives junta os PrimitiveType nao-builtin de t.
func collectStructPrimitives(t ast.NoxyType, out *[]*ast.PrimitiveType) {
	switch typed := t.(type) {
	case *ast.PrimitiveType:
		if !isBuiltinTypeName(typed.Name) {
			*out = append(*out, typed)
		}
	case *ast.ArrayType:
		collectStructPrimitives(typed.ElementType, out)
	case *ast.MapType:
		collectStructPrimitives(typed.KeyType, out)
		collectStructPrimitives(typed.ValueType, out)
	case *ast.RefType:
		collectStructPrimitives(typed.ElementType, out)
	case *ast.NullableType:
		collectStructPrimitives(typed.ElementType, out)
	case *ast.ChanType:
		collectStructPrimitives(typed.ElementType, out)
	case *ast.FunctionType:
		for _, p := range typed.Params {
			collectStructPrimitives(p, out)
		}
		collectStructPrimitives(typed.Return, out)
	}
}

func parseForTest(t *testing.T, src string) *ast.Program {
	t.Helper()
	p := parser.New(lexer.New(src))
	program := p.ParseProgram()
	if len(p.Errors()) > 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}
	return program
}

func TestGenericInstanceAnnotationHasNoDecl(t *testing.T) {
	src := `struct Caixa<T>
    v: T
end
let c: Caixa<int> = Caixa(1)
`
	program := parseForTest(t, src)
	c := NewWithState(make(map[string]ast.NoxyType), make(map[string]*ast.StructStatement), "main.nx")
	if _, _, err := c.Compile(program); err != nil {
		t.Fatalf("compile: %v", err)
	}
	for _, statement := range program.Statements {
		if let, ok := statement.(*ast.LetStmt); ok {
			prim, isPrim := let.Type.(*ast.PrimitiveType)
			if !isPrim || !isGenericInstanceName(prim.Name) {
				t.Fatalf("annotation not flattened to instance name: %s", let.Type)
			}
			if prim.Decl != nil {
				t.Fatalf("generic instance must not carry Decl (spec §1.6), got %p", prim.Decl)
			}
		}
	}
}

func TestTypesEquivalentUsesDeclNotName(t *testing.T) {
	c := New()
	a := &ast.StructStatement{Name: "V"}
	b := &ast.StructStatement{Name: "V"}
	if !c.typesEquivalent(&ast.PrimitiveType{Name: "V", Decl: a}, &ast.PrimitiveType{Name: "base.V", Decl: a}) {
		t.Fatal("same Decl with different Name must be equivalent")
	}
	if c.typesEquivalent(&ast.PrimitiveType{Name: "V", Decl: a}, &ast.PrimitiveType{Name: "V", Decl: b}) {
		t.Fatal("different Decl with the same Name must NOT be equivalent")
	}
	if !c.areTypesCompatible(&ast.PrimitiveType{Name: "V", Decl: a}, &ast.PrimitiveType{Name: "w.V", Decl: a}) {
		t.Fatal("areTypesCompatible must follow Decl")
	}
	if c.areTypesCompatible(&ast.PrimitiveType{Name: "V", Decl: a}, &ast.PrimitiveType{Name: "V", Decl: b}) {
		t.Fatal("areTypesCompatible must not unify two declarations by Name")
	}
	if c.areStrictTypesCompatible(&ast.PrimitiveType{Name: "V", Decl: a}, &ast.PrimitiveType{Name: "V", Decl: b}) {
		t.Fatal("areStrictTypesCompatible must not unify two declarations by Name")
	}
	if !c.typesEquivalent(&ast.PrimitiveType{Name: "int"}, &ast.PrimitiveType{Name: "int"}) {
		t.Fatal("primitives still compare by name")
	}
}

func TestMemberTypeFollowsDeclNotName(t *testing.T) {
	// Um no com Decl e Name canonico (`base.V`, que structDeclaration NAO
	// resolve: `base` nao e alias) tem de resolver campo, slot e default
	// pela declaracao.
	program := parseForTest(t, "struct V\n    x: int\nend\n")
	c := NewWithState(make(map[string]ast.NoxyType), make(map[string]*ast.StructStatement), "main.nx")
	if _, _, err := c.Compile(program); err != nil {
		t.Fatalf("compile: %v", err)
	}
	decl := c.structs["V"]
	canonical := &ast.PrimitiveType{Name: "base.V", Decl: decl}
	if got := c.memberType(canonical, "x"); got == nil || got.String() != "int" {
		t.Fatalf("memberType via Decl: got %v, want int", got)
	}
	if _, ok := c.fieldSlot(canonical, "x"); !ok {
		t.Fatal("fieldSlot must resolve via Decl for a program struct")
	}
	if c.typeWithoutDefault(canonical) == nil {
		t.Fatal("struct via Decl has no default value (spec §3)")
	}
	if err := c.checkDeclaredType(canonical, 1, "variable 'v'"); err != nil {
		t.Fatalf("a type with Decl is known regardless of its Name: %v", err)
	}
}

// Caracterizacao (spec §1.5, no orfao): um no com grafia canonica e SEM Decl
// nao resolve — e por isso que todo site que reconstroi um PrimitiveType a
// partir de outro tem de carregar Decl junto.
func TestCanonicalNameWithoutDeclDoesNotResolve(t *testing.T) {
	program := parseForTest(t, "struct V\n    x: int\nend\n")
	c := NewWithState(make(map[string]ast.NoxyType), make(map[string]*ast.StructStatement), "main.nx")
	if _, _, err := c.Compile(program); err != nil {
		t.Fatalf("compile: %v", err)
	}
	orphan := &ast.PrimitiveType{Name: "base.V"}
	if c.structDeclarationOf(orphan) != nil {
		t.Fatal("an orphan canonical name must not resolve by name")
	}
	if c.typesEquivalent(orphan, &ast.PrimitiveType{Name: "V", Decl: c.structs["V"]}) {
		t.Fatal("orphan must not be equivalent to the declaration")
	}
}

func TestForwardReferenceThroughGenericArgumentCompiles(t *testing.T) {
	// Issue #133 (achado de revisao, round 1): um struct LOCAL pode nomear
	// outro struct declarado MAIS ADIANTE no mesmo programa atraves do
	// argumento de um generico (`c: Caixa<B>`, com `struct B` so depois de
	// `struct A`). unknownFieldTypeError so pode confirmar B quando B ja
	// estiver registrado em c.structs — para um template LOCAL isso so
	// acontece quando predeclareStructs (ou o case *ast.StructStatement)
	// chega em B, o que ainda NAO aconteceu quando A e predeclarado. A
	// checagem eager de ensureStructInstance (generics_structs.go) pula esse
	// caso (so roda para template IMPORTADO) e deixa o caminho de sempre
	// cobrir a referencia adiantada.
	src := `struct Caixa<T>
    v: T
end
struct A
    c: Caixa<B>
end
struct B
    x: int
end
`
	program := parseForTest(t, src)
	c := NewWithState(make(map[string]ast.NoxyType), make(map[string]*ast.StructStatement), "main.nx")
	if _, _, err := c.Compile(program); err != nil {
		t.Fatalf("forward reference through generic argument must compile: %v", err)
	}
}

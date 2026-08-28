package compiler

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"noxy-vm/internal/ast"
	"noxy-vm/internal/lexer"
	"noxy-vm/internal/parser"
)

// Issue #58 item 1: o acesso a membro de um valor tipado por um struct de
// MODULO (`a.f` com `f: io.File`, `res.rows` com `res: db.QueryResult`) e
// tipado estaticamente, com o tipo do campo TRADUZIDO para a visao do
// programa: `Row[]` escrito dentro de db.nx vira `db.Row[]` (namespace),
// `Row` (select) ou nil/dinamico (programa sem como nomear o struct).

// writeModuleFile grava source em root/name — helper para montar modulos
// .nx num TempDir (espelho do `write` de internal/vm).
func writeModuleFile(t *testing.T, root, name, source string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, name), []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
}

// compileSourceAtRoot compila source com moduleRoot=root (para que `use m`
// resolva os arquivos escritos por writeModuleFile) e devolve o erro de
// compilacao, se houver.
func compileSourceAtRoot(t *testing.T, root, source string) error {
	t.Helper()
	l := lexer.New(source)
	p := parser.New(l)
	program := p.ParseProgram()
	if len(p.Errors()) > 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}
	c := NewWithState(make(map[string]ast.NoxyType), make(map[string]*ast.StructStatement), filepath.Join(root, "main.nx"))
	_, _, err := c.Compile(program)
	return err
}

func requireErrorMentions(t *testing.T, err error, wants ...string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected a compile error mentioning %q, got none", wants)
	}
	for _, want := range wants {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q does not mention %q", err.Error(), want)
		}
	}
}

func requireNoError(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected compile error: %v", err)
	}
}

// dbModule e um modulo com um struct (QueryResult) cujos campos nomeiam
// OUTRO struct do modulo (Row) em posicoes aninhadas — o caso de
// sqlite.QueryResult.rows que quebrou read_passwords.nx na 0.12.0.
const dbModule = `struct Row
    v: int
    name: string
end
struct QueryResult
    rows: Row[]
    count: int
    by_name: map[string, Row]
end
func q() -> QueryResult
    let r: Row = Row(1, "a")
    return QueryResult([r], 1, {"a": r})
end
`

func dbRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	writeModuleFile(t, root, "db.nx", dbModule)
	return root
}

const modAModule = `struct Inner
    v: int
end
struct Outer
    i: Inner
end
func make(v: int) -> Outer
    return Outer(Inner(v))
end
`

func modARoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	writeModuleFile(t, root, "mod_a.nx", modAModule)
	return root
}

// --- leitura -------------------------------------------------------------

func TestMemberAccessOnQualifiedStructValueIsTyped(t *testing.T) {
	_, err := compileFunctionSource(t, `use io
struct A
    f: io.File
end
let a: A = A(io.stdin())
let p: int = a.f.path
`)
	requireErrorMentions(t, err, "[line 6]", "type mismatch in 'p' declaration: expected int, got string")
}

func TestMemberAccessOnQualifiedStructValueAcceptsDeclaredType(t *testing.T) {
	_, err := compileFunctionSource(t, `use io
struct A
    f: io.File
end
let a: A = A(io.stdin())
let p: string = a.f.path
let fd: int = a.f.fd
`)
	requireNoError(t, err)
}

// --- traducao do tipo do campo para a visao do programa ------------------

func TestModuleFieldTypeIsTranslatedToNamespaceName(t *testing.T) {
	err := compileSourceAtRoot(t, dbRoot(t), `use db
let res: db.QueryResult = db.q()
let s: string = res.rows
`)
	requireErrorMentions(t, err, "[line 3]", "expected string, got db.Row[]")
}

func TestModuleFieldTypeTranslatedToNamespaceNameMatchesAnnotation(t *testing.T) {
	err := compileSourceAtRoot(t, dbRoot(t), `use db
let res: db.QueryResult = db.q()
let r: db.Row = res.rows[0]
let n: int = res.count
let m: map[string, db.Row] = res.by_name
`)
	requireNoError(t, err)
}

func TestModuleFieldTypeUsesNamespaceAlias(t *testing.T) {
	// (`m.f(...)` via namespace e chamada dinamica — sem tipo de retorno
	// estatico —, por isso o resultado passa por um `let` anotado.)
	err := compileSourceAtRoot(t, dbRoot(t), `use db as d
let res: d.QueryResult = d.q()
let s: string = res.rows
`)
	requireErrorMentions(t, err, "expected string, got d.Row[]")
}

func TestModuleFieldTypeUsesFirstDeclaredNamespaceAlias(t *testing.T) {
	err := compileSourceAtRoot(t, dbRoot(t), `use db as first
use db as second
let res: second.QueryResult = second.q()
let s: string = res.rows
`)
	requireErrorMentions(t, err, "expected string, got first.Row[]")
}

func TestModuleFieldTypePrefersSelectedName(t *testing.T) {
	err := compileSourceAtRoot(t, dbRoot(t), `use db
use db select Row
let res: db.QueryResult = db.q()
let s: string = res.rows
`)
	requireErrorMentions(t, err, "expected string, got Row[]")
}

func TestModuleFieldTypeNamespaceAndSelectedAnnotationsBothMatch(t *testing.T) {
	err := compileSourceAtRoot(t, dbRoot(t), `use db
use db select Row
let res: db.QueryResult = db.q()
let r1: db.Row = res.rows[0]
let r2: Row = res.rows[0]
`)
	requireNoError(t, err)
}

func TestModuleFieldTypeSelectedOwnerStillTranslates(t *testing.T) {
	// Dono importado por select, campo que nomeia outro struct do modulo que
	// o programa so consegue nomear pelo namespace.
	err := compileSourceAtRoot(t, dbRoot(t), `use db
use db select QueryResult, q
let res: QueryResult = q()
let s: string = res.rows
`)
	requireErrorMentions(t, err, "expected string, got db.Row[]")
}

func TestModuleFieldTypeUnnameableStructStaysDynamic(t *testing.T) {
	// Sem `use db` e sem `select Row`, o programa nao tem como escrever o
	// tipo de `rows`: o campo fica dinamico (nil), nunca `Row[]` vazado do
	// escopo do modulo — este programa compila (e falharia so em runtime).
	err := compileSourceAtRoot(t, dbRoot(t), `use db select QueryResult, q
let res: QueryResult = q()
let s: string = res.rows
`)
	requireNoError(t, err)
}

func TestModuleFieldTypePartiallyUnnameableStaysDynamic(t *testing.T) {
	// `map[string, Row]` com Row nao nomeavel: o tipo INTEIRO vira dinamico,
	// nao um `map[string, ???]` meio-tipado.
	err := compileSourceAtRoot(t, dbRoot(t), `use db select QueryResult, q
let res: QueryResult = q()
let s: string = res.by_name
`)
	requireNoError(t, err)
}

func TestModuleFieldTypeSelectedNameWithoutNamespaceIsKept(t *testing.T) {
	err := compileSourceAtRoot(t, dbRoot(t), `use db select QueryResult, Row, q
let res: QueryResult = q()
let s: string = res.rows
`)
	requireErrorMentions(t, err, "expected string, got Row[]")
}

// --- encadeamento --------------------------------------------------------

func TestNestedModuleStructChainIsTyped(t *testing.T) {
	err := compileSourceAtRoot(t, modARoot(t), `use mod_a
struct W
    o: mod_a.Outer
end
let w: W = W(mod_a.make(5))
let s: string = w.o.i.v
`)
	requireErrorMentions(t, err, "[line 6]", "expected string, got int")
}

func TestNestedModuleStructChainIntermediateIsNamedByNamespace(t *testing.T) {
	err := compileSourceAtRoot(t, modARoot(t), `use mod_a
struct W
    o: mod_a.Outer
end
let w: W = W(mod_a.make(5))
let s: string = w.o.i
`)
	requireErrorMentions(t, err, "expected string, got mod_a.Inner")
}

// --- instancia generica do proprio modulo -----------------------------------

// Um campo de struct de modulo cujo tipo e uma INSTANCIA de template do
// proprio modulo (`c: Caixa<int>` dentro de g.nx) e resolvido pelo validador
// do modulo com o nome interno `main::Caixa<int>` — que NAO e identidade
// global: o importador nomeia a mesma instancia `g::Caixa<int>` e um
// template homonimo LOCAL tambem gera `main::Caixa<int>`. O programa nao tem
// como escrever esse nome: o campo fica DINAMICO (comportamento da 0.12.0),
// nunca o nome cru (que rejeitava `let k: Caixa<int> = h.c` — programa valido
// — ou, pior, tipava o campo pelo template local homonimo).
const genericModule = `struct Meta
    k: int
end
struct Caixa<T>
    v: T
    meta: Meta
end
struct H
    c: Caixa<int>
end
func mk() -> H
    return H(Caixa(1, Meta(2)))
end
`

func TestModuleFieldTypedAsModuleOwnGenericInstanceStaysDynamic(t *testing.T) {
	root := t.TempDir()
	writeModuleFile(t, root, "g.nx", genericModule)
	err := compileSourceAtRoot(t, root, `use g
use g select Caixa, Meta
let h: g.H = g.mk()
let k: Caixa<int> = h.c
let s: string = h.c
`)
	requireNoError(t, err)
}

// --- null em campo de struct de modulo ----------------------------------

// `null` e aceito onde um struct ANULAVEL e esperado (spec §2.4); o nome do
// struct pode ser qualificado (`io.File?`) — e, no campo traduzido de modulo
// (`mod_a.Inner`, nao anulavel), o erro nomeia o tipo traduzido com o hint
// `mod_a.Inner?`: a checagem resolve pela declaracao, nao por nome simples.
func TestNullAssignableToQualifiedAndTranslatedStructFields(t *testing.T) {
	_, err := compileFunctionSource(t, `use io
struct A
    f: io.File?
end
let a: A = A(io.stdin())
a.f = null
let g: io.File? = null
`)
	requireNoError(t, err)
	err = compileSourceAtRoot(t, modARoot(t), `use mod_a
struct W
    o: mod_a.Outer
end
let w: W = W(mod_a.make(5))
w.o.i = null
`)
	requireErrorMentions(t, err, "expected mod_a.Inner, got null", "hint: declare it as 'mod_a.Inner?' to allow null")
}

// --- escrita e ref -------------------------------------------------------

func TestFieldAssignmentOnQualifiedStructValueIsTyped(t *testing.T) {
	_, err := compileFunctionSource(t, `use io
struct A
    f: io.File
end
let a: A = A(io.stdin())
a.f.fd = "x"
`)
	requireErrorMentions(t, err, "[line 6]", "type mismatch in field assignment: expected int, got string")
}

func TestNestedChainAssignmentIsTyped(t *testing.T) {
	err := compileSourceAtRoot(t, modARoot(t), `use mod_a
struct W
    o: mod_a.Outer
end
let w: W = W(mod_a.make(5))
w.o.i.v = "x"
`)
	requireErrorMentions(t, err, "[line 6]", "type mismatch in field assignment: expected int, got string")
}

func TestRefArgumentOnQualifiedStructFieldIsTyped(t *testing.T) {
	_, err := compileFunctionSource(t, `use io
struct A
    f: io.File
end
func inc(r: ref int) -> void
    *r = *r + 1
end
let a: A = A(io.stdin())
inc(ref a.f.path)
`)
	requireErrorMentions(t, err, "[line 9]", "argument 1 to 'inc': expected ref int, got ref string")
}

package compiler

// Redeclaracao de `let` no mesmo escopo e erro de compilacao (spec §3): o
// segundo `let` criava um binding novo silenciosamente — inclusive trocando
// o tipo, furando a regra nº1 da §2.0 ("o tipo e definido na declaracao e
// nao pode mudar"). Reatribuicao (`x = ...`) segue sendo o caminho para
// atualizar o valor. Shadowing em escopo INTERNO continua legal: for-var
// sobre let externo, let em bloco if, corpo sombreando parametro (params em
// depth 1, corpo BlockStatement em depth 2).

import (
	"strings"
	"testing"

	"noxy-vm/internal/ast"
)

const redeclaredText = "redeclared in this scope"

func requireRedeclarationError(t *testing.T, src string, wantPrev string) {
	t.Helper()
	_, _, err := New().Compile(parse(src))
	if err == nil {
		t.Fatal("redeclaracao de let no mesmo escopo deveria falhar na compilacao")
	}
	if !strings.Contains(err.Error(), redeclaredText) {
		t.Fatalf("erro deveria citar %q: %v", redeclaredText, err)
	}
	if !strings.Contains(err.Error(), wantPrev) {
		t.Fatalf("erro deveria apontar a declaracao anterior (%q): %v", wantPrev, err)
	}
}

func requireCompiles(t *testing.T, src string) {
	t.Helper()
	if _, _, err := New().Compile(parse(src)); err != nil {
		t.Fatalf("programa valido nao deveria falhar: %v", err)
	}
}

func TestLocalLetRedeclarationSameScopeFails(t *testing.T) {
	requireRedeclarationError(t, `func f()
    let x: int = 1
    let x: string = "virei string"
end`, "previous declaration at line 2")
}

func TestLocalLetRedeclarationSameTypeFails(t *testing.T) {
	requireRedeclarationError(t, `func f()
    let x: int = 1
    let x: int = 2
end`, "previous declaration at line 2")
}

func TestLetShadowingInInnerBlockAllowed(t *testing.T) {
	requireCompiles(t, `func f()
    let x: int = 1
    if x > 0 then
        let x: string = "sombra interna"
        print(x)
    end
    print(x)
end`)
}

func TestLetAfterForLoopVarAllowed(t *testing.T) {
	requireCompiles(t, `func f()
    for i in [1, 2, 3] do
        print(i)
    end
    let i: int = 2
    print(i)
end`)
}

func TestLetShadowingParamAllowed(t *testing.T) {
	requireCompiles(t, `func f(x: int)
    let x: string = "corpo sombreia parametro"
    print(x)
end`)
}

func TestGlobalLetRedeclarationFails(t *testing.T) {
	requireRedeclarationError(t, `let x: int = 1
let x: string = "virei string"`, "previous declaration at line 1")
}

func TestGlobalLetAfterTopLevelForAllowed(t *testing.T) {
	// O padrao do crivo.nx: a variavel do for e escopada ao loop, entao o
	// let seguinte e declaracao nova, nao redeclaracao.
	requireCompiles(t, `for i in [1, 2, 3] do
    print(i)
end
let i: int = 2
print(i)`)
}

func TestReplSessionReLetFails(t *testing.T) {
	// REPL sem excecao (decisao 2026-08-20): a sessao se comporta como um
	// arquivo digitado linha a linha. O compilador CHECA contra sessionLets;
	// quem registra e o loop do REPL, apos a linha compilar com sucesso.
	structs := make(map[string]*ast.StructStatement)
	session := make(map[string]int)

	c1 := NewWithState(make(map[string]ast.NoxyType), structs, "REPL")
	c1.SetSessionLets(session)
	if _, _, err := c1.Compile(parse(`let x: int = 1`)); err != nil {
		t.Fatalf("primeira linha do REPL falhou: %v", err)
	}
	for name, line := range c1.ProgramLets() {
		session[name] = line
	}

	c2 := NewWithState(c1.GetGlobals(), structs, "REPL")
	c2.SetSessionLets(session)
	_, _, err := c2.Compile(parse(`let x: string = "re-let de sessao"`))
	if err == nil {
		t.Fatal("re-let entre linhas da sessao deveria falhar")
	}
	if !strings.Contains(err.Error(), "previously declared in this session") {
		t.Fatalf("erro deveria citar a sessao: %v", err)
	}
}

func TestReplFailedLineDoesNotBurnTheName(t *testing.T) {
	// Uma linha rejeitada nao registra o nome: ProgramLets so e lido pelo
	// REPL quando a compilacao inteira da linha teve sucesso, entao aqui o
	// contrato e que a linha seguinte com o MESMO nome compila.
	structs := make(map[string]*ast.StructStatement)
	session := make(map[string]int)

	c1 := NewWithState(make(map[string]ast.NoxyType), structs, "REPL")
	c1.SetSessionLets(session)
	if _, _, err := c1.Compile(parse(`let x: int = "tipo errado"`)); err == nil {
		t.Fatal("linha com type mismatch deveria falhar")
	}
	// REPL nao faz merge apos erro — session continua vazio.

	c2 := NewWithState(make(map[string]ast.NoxyType), structs, "REPL")
	c2.SetSessionLets(session)
	if _, _, err := c2.Compile(parse(`let x: int = 1`)); err != nil {
		t.Fatalf("nome de linha rejeitada nao deveria estar queimado: %v", err)
	}
}

// Issue #47 parte 2: o escopo global e UM namespace — let, func, struct e
// import colidem entre especies, nao so let x let.

func TestGlobalLetOverFunctionFails(t *testing.T) {
	requireRedeclarationError(t, "func x() -> int\n    return 1\nend\nlet x: int = 42\n", "previous declaration as function at line 1")
}

func TestGlobalFunctionOverLetFails(t *testing.T) {
	requireRedeclarationError(t, "let x: int = 42\nfunc x() -> int\n    return 1\nend\n", "previous declaration as variable at line 1")
}

func TestGlobalStructOverFunctionFails(t *testing.T) {
	requireRedeclarationError(t, "func P() -> int\n    return 1\nend\nstruct P\n    x: int\nend\n", "previous declaration as function at line 1")
}

func TestGlobalStructOverStructFails(t *testing.T) {
	requireRedeclarationError(t, "struct P\n    x: int\nend\nstruct P\n    y: int\nend\n", "previous declaration as struct at line 1")
}

func TestGlobalLetOverSelectiveImportFails(t *testing.T) {
	requireRedeclarationError(t, "use sys select os\nlet os: int = 5\n", "previous declaration as import at line 1")
}

func TestGlobalLetOverNamespaceImportFails(t *testing.T) {
	requireRedeclarationError(t, "use strings\nlet strings: int = 5\n", "previous declaration as import at line 1")
}

func TestGlobalFunctionOverStarImportFails(t *testing.T) {
	requireRedeclarationError(t, "use http_client select *\nfunc delete(n: int) -> int\n    return n\nend\n", "previous declaration as import at line 1")
}

func TestReplSessionFunctionThenLetFails(t *testing.T) {
	structs := make(map[string]*ast.StructStatement)
	session := make(map[string]GlobalDecl)
	c1 := NewWithState(make(map[string]ast.NoxyType), structs, "REPL")
	c1.SetSessionBindings(session)
	if _, _, err := c1.Compile(parse("func f() -> int\n    return 1\nend\n")); err != nil {
		t.Fatalf("first line: %v", err)
	}
	for name, d := range c1.ProgramBindings() {
		session[name] = d
	}
	c2 := NewWithState(c1.GetGlobals(), structs, "REPL")
	c2.SetSessionBindings(session)
	_, _, err := c2.Compile(parse("let f: int = 2\n"))
	if err == nil || !strings.Contains(err.Error(), "previously declared as function in this session") {
		t.Fatalf("want function/let collision across REPL lines, got %v", err)
	}
}

func TestReplSessionFunctionRedefinitionAllowed(t *testing.T) {
	structs := make(map[string]*ast.StructStatement)
	session := make(map[string]GlobalDecl)
	c1 := NewWithState(make(map[string]ast.NoxyType), structs, "REPL")
	c1.SetSessionBindings(session)
	if _, _, err := c1.Compile(parse("func f() -> int\n    return 1\nend\n")); err != nil {
		t.Fatalf("first line: %v", err)
	}
	for name, d := range c1.ProgramBindings() {
		session[name] = d
	}
	c2 := NewWithState(c1.GetGlobals(), structs, "REPL")
	c2.SetSessionBindings(session)
	if _, _, err := c2.Compile(parse("func f() -> int\n    return 2\nend\n")); err != nil {
		t.Fatalf("redefining a function across REPL lines must stay allowed, got %v", err)
	}
}

func TestSharedGlobalsWithoutSessionAllowed(t *testing.T) {
	// Fora do REPL ninguem arma SetSessionLets: duas compilacoes que por
	// acaso compartilhem o mapa de globals (fronteiras de modulo) nao se
	// enxergam — o check global e por Program.
	structs := make(map[string]*ast.StructStatement)
	c1 := NewWithState(make(map[string]ast.NoxyType), structs, "a.nx")
	if _, _, err := c1.Compile(parse(`let x: int = 1`)); err != nil {
		t.Fatalf("primeira compilacao falhou: %v", err)
	}
	c2 := NewWithState(c1.GetGlobals(), structs, "b.nx")
	if _, _, err := c2.Compile(parse(`let x: string = "outro programa"`)); err != nil {
		t.Fatalf("sem sessao armada nao ha memoria entre Programs: %v", err)
	}
}

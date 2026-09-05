package compiler

import (
	"strings"
	"testing"

	"github.com/estevaofon/noxy/internal/ast"
	"github.com/estevaofon/noxy/internal/lexer"
	"github.com/estevaofon/noxy/internal/parser"
)

// Issue #58 item 2: um nome de tipo que nao foi declarado em lugar nenhum
// (nem `struct T` no programa, nem `use m select T`, nem primitivo) e erro
// de COMPILACAO — `unknown type 'T'` com hint — em campo de struct,
// parametro, retorno e `let`. Antes: parametro/retorno/campo compilavam em
// silencio (o construtor falhava em runtime com "incomplete runtime type
// metadata") e `let` rejeitava pela via errada ("type mismatch").

const unknownTypeHint = "hint: declare 'struct Inexistente' or import it with 'use m select Inexistente'"

func requireNotMentions(t *testing.T, err error, unwanted string) {
	t.Helper()
	if err != nil && strings.Contains(err.Error(), unwanted) {
		t.Fatalf("error %q should not mention %q", err.Error(), unwanted)
	}
}

// --- as quatro posicoes ---------------------------------------------------

func TestLetWithUndeclaredTypeIsUnknownTypeError(t *testing.T) {
	_, err := compileFunctionSource(t, "let x: Inexistente = 1\n")
	requireErrorMentions(t, err, "[line 1]", "variable 'x': unknown type 'Inexistente'", unknownTypeHint)
	requireNotMentions(t, err, "type mismatch")
}

func TestLetWithoutInitializerUndeclaredTypeIsUnknownTypeError(t *testing.T) {
	_, err := compileFunctionSource(t, "let x: Inexistente\n")
	requireErrorMentions(t, err, "[line 1]", "variable 'x': unknown type 'Inexistente'", unknownTypeHint)
}

func TestLocalLetWithUndeclaredTypeIsUnknownTypeError(t *testing.T) {
	_, err := compileFunctionSource(t, "func f() -> void\n    let x: Inexistente = 1\nend\n")
	requireErrorMentions(t, err, "[line 2]", "variable 'x': unknown type 'Inexistente'", unknownTypeHint)
}

func TestFunctionParameterUndeclaredTypeIsUnknownTypeError(t *testing.T) {
	_, err := compileFunctionSource(t, "func f(x: Inexistente) -> void\nend\n")
	requireErrorMentions(t, err, "[line 1]", "function 'f' parameter 'x': unknown type 'Inexistente'", unknownTypeHint)
}

func TestFunctionReturnUndeclaredTypeIsUnknownTypeError(t *testing.T) {
	_, err := compileFunctionSource(t, "func f() -> Inexistente\n    return null\nend\n")
	requireErrorMentions(t, err, "[line 1]", "function 'f' return type: unknown type 'Inexistente'", unknownTypeHint)
}

func TestStructFieldUndeclaredTypeIsUnknownTypeError(t *testing.T) {
	_, err := compileFunctionSource(t, "struct A\n    b: Inexistente\nend\n")
	requireErrorMentions(t, err, "[line 1]", "struct 'A' field 'b': unknown type 'Inexistente'", unknownTypeHint)
}

func TestStructFieldUndeclaredTypeInsideFunctionIsUnknownTypeError(t *testing.T) {
	_, err := compileFunctionSource(t, "func f() -> void\n    struct A\n        b: Inexistente\n    end\nend\n")
	requireErrorMentions(t, err, "[line 2]", "struct 'A' field 'b': unknown type 'Inexistente'", unknownTypeHint)
}

func TestFunctionLiteralParameterUndeclaredTypeIsUnknownTypeError(t *testing.T) {
	_, err := compileFunctionSource(t, "let f: func = func(x: Inexistente) -> int\n    return 1\nend\n")
	requireErrorMentions(t, err, "[line 1]", "parameter 'x': unknown type 'Inexistente'", unknownTypeHint)
}

func TestUndeclaredTypeNestedInCompositeIsUnknownTypeError(t *testing.T) {
	_, err := compileFunctionSource(t, "func f(xs: map[string, Inexistente[]]) -> void\nend\n")
	requireErrorMentions(t, err, "function 'f' parameter 'xs': unknown type 'Inexistente'")
}

func TestUndeclaredTypeInsideRefAndFunctionTypeIsUnknownTypeError(t *testing.T) {
	_, err := compileFunctionSource(t, "struct A\n    cb: func(ref Inexistente) -> int\nend\n")
	requireErrorMentions(t, err, "struct 'A' field 'cb': unknown type 'Inexistente'")
}

// --- forma qualificada fora do campo de struct mantem a mensagem da 0.12.0 --

func TestQualifiedUnknownStructInLetKeepsModuleMessage(t *testing.T) {
	_, err := compileFunctionSource(t, "use io\nlet f: io.Nope = null\n")
	requireErrorMentions(t, err, "[line 2]", "variable 'f': cannot resolve type 'io.Nope': module 'io' has no struct 'Nope'", "hint:")
	requireNotMentions(t, err, "type mismatch")
}

func TestQualifiedUnknownNamespaceInParameterKeepsModuleMessage(t *testing.T) {
	_, err := compileFunctionSource(t, "func f(x: foo.Bar) -> void\nend\n")
	requireErrorMentions(t, err, "[line 1]", "function 'f' parameter 'x': cannot resolve type 'foo.Bar': 'foo' is not an imported module", "hint:", "use foo")
}

// --- o que NAO pode disparar ---------------------------------------------

func TestForwardReferenceBetweenTopLevelStructsStillCompiles(t *testing.T) {
	_, err := compileFunctionSource(t, "struct A\n    b: B\n    next: ref A?\nend\nstruct B\n    a: ref A?\n    v: int\nend\nlet a: A = A(B(null, 1), null)\n")
	requireNoError(t, err)
}

func TestFunctionUsingStructDeclaredLaterStillCompiles(t *testing.T) {
	_, err := compileFunctionSource(t, "func make(v: int) -> P\n    return P(v)\nend\nstruct P\n    v: int\nend\nlet p: P = make(1)\n")
	requireNoError(t, err)
}

func TestStructDeclaredInsideFunctionStillCompiles(t *testing.T) {
	_, err := compileFunctionSource(t, "func f() -> int\n    struct L\n        v: int\n        next: ref L?\n    end\n    let l: L = L(1, null)\n    return l.v\nend\n")
	requireNoError(t, err)
}

func TestGenericInstanceAnnotationsStillCompile(t *testing.T) {
	_, err := compileFunctionSource(t, "struct Caixa<T>\n    v: T\nend\nfunc g(c: Caixa<int>) -> Caixa<int>\n    let d: Caixa<int> = c\n    return d\nend\nlet c: Caixa<int> = Caixa(1)\nlet xs: Caixa<string>[] = []\n")
	requireNoError(t, err)
}

func TestTypeParamInsideGenericBodyStillCompiles(t *testing.T) {
	_, err := compileFunctionSource(t, "func id<T>(x: T) -> T\n    let y: T = x\n    return y\nend\nlet n: int = id(1)\n")
	requireNoError(t, err)
}

func TestImportedStructBySelectAndQualifiedStillCompile(t *testing.T) {
	_, err := compileFunctionSource(t, "use io\nuse io select File\nfunc h(f: File) -> int\n    return f.fd\nend\nfunc k(f: io.File) -> io.File\n    return f\nend\nstruct A\n    f: File\n    g: io.File\nend\nlet x: File = io.stdin()\nlet y: io.File = io.stdin()\n")
	requireNoError(t, err)
}

func TestUseAfterFunctionThatNamesItsStructStillCompiles(t *testing.T) {
	// Imports de topo sao predeclarados: a ordem entre o `use` e a funcao
	// que nomeia o struct importado nao importa (como ja nao importava para
	// chamar uma funcao declarada depois).
	_, err := compileFunctionSource(t, "func h(f: File) -> int\n    return f.fd\nend\nuse io select File\n")
	requireNoError(t, err)
}

func TestUseInsideFunctionBodyMakesStructVisibleThere(t *testing.T) {
	_, err := compileFunctionSource(t, "func h() -> int\n    use io select File\n    let f: File = io.stdin()\n    return f.fd\nend\nuse io\n")
	requireNoError(t, err)
}

func TestStructsPersistedAcrossCompilationsAreKnown(t *testing.T) {
	// REPL: a tabela de structs e compartilhada entre linhas (NewWithState);
	// um `let` numa linha posterior enxerga o struct da anterior.
	globals := make(map[string]ast.NoxyType)
	structs := make(map[string]*ast.StructStatement)
	for _, line := range []string{"struct P\n    v: int\nend\n", "let p: P = P(1)\n"} {
		program := parser.New(lexer.New(line)).ParseProgram()
		c := NewWithState(globals, structs, "REPL")
		if _, _, err := c.Compile(program); err != nil {
			t.Fatalf("line %q: unexpected compile error: %v", line, err)
		}
	}
}

func TestReplCarriesNamespaceImportsAcrossLines(t *testing.T) {
	// REPL: cada linha e um compilador novo que compartilha globals/structs;
	// o estado de modulos (aliases de `use m` e o cache de descoberta) tem de
	// acompanhar, senao `use io` numa linha e `let f: io.File` na seguinte
	// acusaria "'io' is not an imported module" — e o acesso a membro de um
	// valor tipado por struct de modulo perderia a origem (structOrigin) na
	// linha seguinte.
	root := dbRoot(t)
	globals := make(map[string]ast.NoxyType)
	structs := make(map[string]*ast.StructStatement)
	var modules *ModuleState
	for _, line := range []string{
		"use io\n",
		"let f: io.File = io.stdin()\n",
		"let p: string = f.path\n",
		// Struct importado por select numa linha anterior: a ORIGEM (db) tem
		// de ser lembrada para `res.rows` ser traduzido para o caminho
		// canonico `db.Row[]` (issue #133: Row nao nomeavel pelo programa,
		// mas o valor continua tipado) — senao a mensagem vazaria `Row[]` cru.
		"use db select QueryResult, q\n",
		"let res: QueryResult = q()\n",
		"let bad1: string = res.rows\n",
		"use db\n",
		"let r2: db.QueryResult = db.q()\n",
		"let bad: string = r2.rows\n",
	} {
		program := parser.New(lexer.New(line)).ParseProgram()
		c := NewWithStateAndRoot(globals, structs, "REPL", root)
		c.SetModuleState(modules)
		_, _, err := c.Compile(program)
		modules = c.ModuleState()
		if strings.HasPrefix(line, "let bad") {
			requireErrorMentions(t, err, "expected string, got db.Row[]")
			continue
		}
		if err != nil {
			t.Fatalf("line %q: unexpected compile error: %v", line, err)
		}
	}
}

func TestFunctionSignatureCannotNameStructDeclaredInItsOwnBody(t *testing.T) {
	// A assinatura e resolvida no escopo em que a funcao e declarada: um
	// struct que so existe dentro do corpo nao e nomeavel por quem chama —
	// antes compilava (retorno dinamico) e agora e erro (documentado, 0.13.0).
	_, err := compileFunctionSource(t, "func make() -> Pair\n    struct Pair\n        a: int\n    end\n    return Pair(1)\nend\n")
	requireErrorMentions(t, err, "[line 1]", "function 'make' return type: unknown type 'Pair'")
}

func TestStructReexportedBySelectiveImportIsKnownToImporter(t *testing.T) {
	// `a` importa T de `b` por `use b select T` e a reexporta (uma funcao de
	// `a` devolve T; `use a select *` no programa liga T como valor — ver
	// discoverModuleExports). A DECLARACAO de T tem de vir junto: sem isso
	// `let t: T = mk()` acusaria `unknown type 'T'` enquanto `T(...)` e um
	// nome perfeitamente chamavel no programa.
	root := t.TempDir()
	writeModuleFile(t, root, "b.nx", "struct T\n    v: int\nend\n")
	writeModuleFile(t, root, "a.nx", "use b select T\nfunc mk() -> T\n    return T(1)\nend\n")
	for _, program := range []string{
		"use a select *\nlet t: T = mk()\nlet n: int = t.v\n",
		"use a select T, mk\nlet t: T = mk()\nlet n: int = t.v\n",
	} {
		if err := compileSourceAtRoot(t, root, program); err != nil {
			t.Fatalf("program %q: unexpected compile error: %v", program, err)
		}
	}
}

func TestInstanceOfImportedTemplateNeedsItsStructDependencyImported(t *testing.T) {
	// §6.4: a instancia e compilada no contexto do importador, entao o struct
	// que o template nomeia num campo tem de estar visivel la — erro
	// acionavel em vez de construtor quebrado em runtime.
	// (O valor Meta vem do modulo para que o call site do construtor feche
	// no pass 1 e o que dispare seja a checagem dos campos da instancia,
	// compilada como struct comum no pass 2.)
	root := t.TempDir()
	writeModuleFile(t, root, "caixas.nx", "struct Meta\n    k: int\nend\nstruct Caixa<T>\n    v: T\n    meta: Meta\nend\nfunc make_meta(k: int) -> Meta\n    return Meta(k)\nend\n")
	// A linha e a da INSTANCIACAO no programa (nao a do template dentro do
	// modulo) e o hint nomeia o modulo que declara o struct que falta.
	err := compileSourceAtRoot(t, root, "use caixas select Caixa, make_meta\nlet c: Caixa<int> = Caixa(1, make_meta(2))\n")
	requireErrorMentions(t, err, "[line 2]", "struct 'Caixa<int>' field 'meta': unknown type 'Meta'", "hint:", "use caixas select Meta")
	err = compileSourceAtRoot(t, root, "use caixas select Caixa, Meta, make_meta\nlet c: Caixa<int> = Caixa(1, make_meta(2))\nlet k: int = c.meta.k\n")
	requireNoError(t, err)
}

func TestUnknownTypeHintNamesTheDeclaringAndReexportingModules(t *testing.T) {
	// Issue #133 (spec §1.7): so a anotacao ESCRITA exige grafia; quando
	// falta, o hint diz de onde importar — o declarante e quem reexporta.
	root := t.TempDir()
	writeModuleFile(t, root, "base.nx", "struct V\n    x: int\nend\nfunc mkv() -> V\n    return V(1)\nend\n")
	writeModuleFile(t, root, "mid.nx", "use base select *\n")
	err := compileSourceAtRoot(t, root, "use mid select mkv\nlet v: V = mkv()\n")
	requireErrorMentions(t, err, "variable 'v': unknown type 'V'", "add 'use base' or 'use mid select V' to name this type")
	err = compileSourceAtRoot(t, root, "use base select mkv\nlet v: V = mkv()\n")
	requireErrorMentions(t, err, "unknown type 'V'", "add 'use base' or 'use base select V' to name this type")
	// Sem candidato: hint generico de hoje.
	err = compileSourceAtRoot(t, root, "let v: Nada = 1\n")
	requireErrorMentions(t, err, "unknown type 'Nada'", "declare 'struct Nada' or import it with 'use m select Nada'")
}

func TestUnknownTypeHintReexporterMatchesTheChosenOriginsDeclarationNotJustTheName(t *testing.T) {
	// Issue #133 (review round 1): dois modulos DIFERENTES podem cada um
	// declarar o seu proprio `struct V` sem serem a MESMA declaracao. O
	// reexportador so pode entrar no hint se o export dele apontar para o
	// MESMO ponteiro da declaracao do modulo escolhido como origem — nao
	// para qualquer decl de outro modulo que por acaso tenha o mesmo nome.
	root := t.TempDir()
	writeModuleFile(t, root, "a.nx", "struct V\n    x: int\nend\nfunc f() -> V\n    return V(1)\nend\n")
	writeModuleFile(t, root, "b.nx", "struct V\n    y: int\nend\nfunc g() -> void\nend\n")
	writeModuleFile(t, root, "bridge.nx", "use b select *\n")
	// bridge reexporta a V de b (nao relacionada a de a) — nao pode aparecer
	// no hint como se reexportasse a V de a, mesmo com "a" < "b" ordenando
	// "a" como origem escolhida. Desde a revisao adversarial do #133 (caso 5)
	// esta forma tem DUAS origens carregadas (a declara V, e b tambem, pela
	// cadeia de bridge), entao o hint lista as duas em vez de escolher uma:
	// a garantia que interessa continua sendo que bridge nunca e apresentado
	// como reexportador da V de a.
	err := compileSourceAtRoot(t, root, "use a select f\nuse bridge select g\nlet v: V = f()\n")
	requireErrorMentions(t, err, "unknown type 'V'", "'V' is declared by modules a and b")
	requireNotMentions(t, err, "use b select V")
	requireNotMentions(t, err, "use bridge select V")

	// bridge2 reexporta a MESMA V de a: agora sim entra no hint.
	writeModuleFile(t, root, "bridge2.nx", "use a select *\nfunc g2() -> void\nend\n")
	err = compileSourceAtRoot(t, root, "use a select f\nuse bridge2 select g2\nlet v: V = f()\n")
	requireErrorMentions(t, err, "unknown type 'V'", "add 'use a' or 'use bridge2 select V' to name this type")
}

func TestUnknownTypeHintNamesEveryDeclaringModuleWhenSeveralDeclareTheName(t *testing.T) {
	// Issue #133 (revisao adversarial, caso 5): com DOIS modulos carregados
	// declarando `struct V`, o hint escolhia um por ordem alfabetica e
	// apontava, metade das vezes, para a declaracao errada — seguir o hint
	// trocava `unknown type 'V'` por `expected V, got otherdiff.V`. Sem o
	// contexto que produziu o erro, a resposta honesta e listar os
	// candidatos e deixar a escolha com quem escreve.
	root := t.TempDir()
	writeModuleFile(t, root, "base.nx", "struct V\n    x: int\nend\nfunc f() -> V\n    return V(1)\nend\n")
	writeModuleFile(t, root, "otherdiff.nx", "struct V\n    s: string\nend\nfunc g() -> V\n    return V(\"hi\")\nend\n")
	err := compileSourceAtRoot(t, root, "use base select f\nuse otherdiff select g\nlet v: V = f()\n")
	requireErrorMentions(t, err,
		"variable 'v': unknown type 'V'",
		"'V' is declared by modules base and otherdiff; add 'use <module>' or 'use <module> select V' for the one you mean")
}

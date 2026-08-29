package vm

// E2E cross-modulo de genericos (spec §8): templates importados, predeclare
// tipado de imports, validacao de escopo de definicao e o erro de namespace.
// Padrao de setup copiado de module_exports_test.go:21 (t.TempDir() +
// os.WriteFile + compiler com RootPath apontando para o TempDir).

import (
	"noxy-vm/internal/ast"
	"noxy-vm/internal/chunk"
	"noxy-vm/internal/compiler"
	"noxy-vm/internal/lexer"
	"noxy-vm/internal/parser"
	"noxy-vm/internal/value"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// write grava source em root/name — helper compartilhado pelos testes deste
// arquivo para montar modulos .nx num TempDir.
func write(t *testing.T, root, name, source string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, name), []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
}

// captureVMSourceAtRoot compila e roda source com o RootPath do VM apontando
// para root (para que `use <modulo>` resolva os arquivos escritos por
// write), devolvendo o ultimo valor passado a test_report. Espelha
// captureVMSource (vm_test_helpers_test.go), so com RootPath configurado —
// mesmo padrao de compiler.NewWithStateAndRoot(...) que
// module_exports_test.go:150 (compileModuleProgram) usa.
func captureVMSourceAtRoot(t *testing.T, root, source string) value.Value {
	t.Helper()
	machine := NewWithConfig(VMConfig{RootPath: root})
	// CloseExtensions antes do TempDir se desfazer: um backend por processo
	// (kind = "process") mantem bin/<asset> aberto enquanto o guest esta de
	// pe, e no Windows isso faz o RemoveAll do t.TempDir() falhar com
	// "Access is denied" (achado ao rodar os testes da issue #80). t.Cleanup
	// e LIFO, entao registrar aqui — depois do t.TempDir() do chamador —
	// garante que o processo morre antes da limpeza do diretorio.
	t.Cleanup(machine.CloseExtensions)
	captured := value.NewNull()
	machine.DefineNative("test_report", func(args []value.Value) value.Value {
		if len(args) != 0 {
			captured = args[0]
		}
		return value.NewNull()
	})
	code := compileVMSourceAtRoot(t, root, source)
	if err := machine.Interpret(code); err != nil {
		t.Fatalf("vm error: %v", err)
	}
	return captured
}

// compileVMSourceAtRoot compila source (sem rodar) com moduleRoot=root.
func compileVMSourceAtRoot(t *testing.T, root, source string) *chunk.Chunk {
	t.Helper()
	l := lexer.New(source)
	p := parser.New(l)
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}
	code, _, err := compiler.NewWithStateAndRoot(make(map[string]ast.NoxyType), make(map[string]*ast.StructStatement), filepath.Join(root, "main.nx"), root).Compile(program)
	if err != nil {
		t.Fatalf("compiler error: %v", err)
	}
	return code
}

// compileErrAtRoot compila source com moduleRoot=root e devolve o erro (nil
// se compilou com sucesso) — para os testes negativos deste arquivo, que so
// querem inspecionar a mensagem de erro sem rodar a VM.
func compileErrAtRoot(t *testing.T, root, source string) error {
	t.Helper()
	l := lexer.New(source)
	p := parser.New(l)
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}
	_, _, err := compiler.NewWithStateAndRoot(make(map[string]ast.NoxyType), make(map[string]*ast.StructStatement), filepath.Join(root, "main.nx"), root).Compile(program)
	return err
}

func TestGenericCrossModuleSelectStar(t *testing.T) {
	root := t.TempDir()
	write(t, root, "colecoes.nx", `
func primeiro<T>(arr: T[]) -> T
    return arr[0]
end
`)
	got := captureVMSourceAtRoot(t, root, `
use colecoes select *
let nums: int[] = [42]
test_report(primeiro(nums))
`)
	expectInt(t, got, 42, "template importado por select *")
}

func TestGenericCrossModuleSelectiveWithDependency(t *testing.T) {
	root := t.TempDir()
	write(t, root, "colecoes.nx", `
func ajuda(x: int) -> int
    return x + 1
end
func processa<T>(arr: T[]) -> int
    return ajuda(length(arr))
end
`)
	// sem a dependencia: erro acionavel
	err := compileErrAtRoot(t, root, "use colecoes select processa\nlet ns: int[] = [1]\nprocessa(ns)")
	if err == nil || !strings.Contains(err.Error(), "adicione ao select") {
		t.Fatalf("esperava erro de dependencia, veio %v", err)
	}
	// com a dependencia: funciona
	got := captureVMSourceAtRoot(t, root, `
use colecoes select processa, ajuda
let ns: int[] = [1, 2]
test_report(processa(ns))
`)
	expectInt(t, got, 3, "dependencia importada junto")
}

func TestGenericTemplateViaNamespaceIsError(t *testing.T) {
	root := t.TempDir()
	write(t, root, "colecoes.nx", "func primeiro<T>(arr: T[]) -> T\n    return arr[0]\nend\n")
	err := compileErrAtRoot(t, root, "use colecoes\nlet ns: int[] = [1]\ncolecoes.primeiro(ns)")
	if err == nil || !strings.Contains(err.Error(), "não é acessível via namespace") {
		t.Fatalf("esperava erro de namespace, veio %v", err)
	}
}

func TestImportedDataInference(t *testing.T) {
	// §8: predeclare tipado — inferir T a partir de um global importado
	root := t.TempDir()
	write(t, root, "dados.nx", "let numeros: int[] = [5, 6]\n")
	got := captureVMSourceAtRoot(t, root, `
use dados select numeros
func primeiro<T>(arr: T[]) -> T
    return arr[0]
end
test_report(primeiro(numeros))
`)
	expectInt(t, got, 5, "inferencia sobre dado importado tipado")
}

func TestModuleUsingOwnTemplatesInternally(t *testing.T) {
	// §5: compileAndRunModule tambem precisa do two-pass
	root := t.TempDir()
	write(t, root, "interno.nx", `
func id<T>(x: T) -> T
    return x
end
let resultado: int = id(11)
`)
	got := captureVMSourceAtRoot(t, root, "use interno select resultado\ntest_report(resultado)")
	expectInt(t, got, 11, "modulo monomorfiza os proprios templates")
}

// TestHomonymousTemplatesDedupIsSafe prova a metade R8 do §8: dois modulos
// com templates homonimos ("marca<T>") importados via select* nunca fazem
// suas INSTANCIAS colidirem, mesmo que o registry (mapa FLAT por nome) so
// consiga apontar para UM template por vez sob o nome "marca".
//
// r1 e chamado logo apos `use a select *` — nesse momento so o template de
// "a" esta registrado sob "marca", entao marca(0) instancia e executa
// a::marca<int> (corpo "return 1"), memoizado no two-pass do IMPORTADOR.
// `use b select *` entao SOBRESCREVE a entrada de NOME do registry — mesmo
// comportamento de "ultimo import vence" que select* ja tem hoje para
// globals comuns (nao e uma regressao nova desta task). r2, chamado depois,
// resolve "marca" contra o template de "b" e instancia b::marca<int> (corpo
// "return 2") — um simbolo QUALIFICADO DIFERENTE de a::marca<int>, nunca o
// mesmo global reescrito.
//
// A asserção concreta (r1==1 E r2==2, combinados num unico valor para caber
// no captureVMSourceAtRoot, que so retem o ultimo test_report) prova as duas
// metades de R8 ao mesmo tempo: a resolução por NOME de fato troca (r2 usa o
// template de "b", não voltou a chamar o de "a" por engano) e a instância
// JÁ CRIADA de a::marca<int> continua intacta e correta (r1 permanece 1
// mesmo depois do registry ser sobrescrito por "b" — se as duas
// instâncias colidissem no mesmo global qualificado, r1 teria virado 2
// silenciosamente).
func TestHomonymousTemplatesDedupIsSafe(t *testing.T) {
	root := t.TempDir()
	write(t, root, "a.nx", "func marca<T>(x: T) -> int\n    return 1\nend\n")
	write(t, root, "b.nx", "func marca<T>(x: T) -> int\n    return 2\nend\n")
	got := captureVMSourceAtRoot(t, root, `
use a select *
let r1: int = marca(0)
use b select *
let r2: int = marca(0)
test_report(r1 * 10 + r2)
`)
	expectInt(t, got, 12, "a::marca<int> e b::marca<int> nunca colidem (R8)")
}

// TestTemplateDependencyValidationChecksModuleIdentity cobre o achado da
// revisao pos-Task-12: validateImportedTemplateScope tratava uma dependencia
// que e ELA MESMA um template generico como satisfeita assim que QUALQUER
// template com aquele nome BARE existisse em algum lugar do registry
// (mapa flat por nome, R8) — sem checar se o template registrado vem do
// MESMO modulo que processa<T> precisa. Cenario concreto: "colecoes" declara
// processa<T> chamando ajuda<U> (mesmo modulo); um modulo NAO RELACIONADO
// "outro" tambem exporta um generico homonimo "ajuda". Importar so
// `processa` de colecoes e SEPARADAMENTE `ajuda` de outro fazia a validacao
// passar (achava "outro"::ajuda no registry) e o corpo clonado de processa
// chamaria "outro"::ajuda em silencio — a classe exata de bug que o §8
// existe pra prevenir. Corrigido comparando o Module do template achado no
// registry contra moduleQualifier(tpl.Module) antes de aceitar a
// dependencia como satisfeita.
func TestTemplateDependencyValidationChecksModuleIdentity(t *testing.T) {
	root := t.TempDir()
	write(t, root, "colecoes.nx", `
func ajuda<U>(x: U) -> int
    return 100
end
func processa<T>(arr: T[]) -> int
    return ajuda(arr[0])
end
`)
	write(t, root, "outro.nx", `
func ajuda<U>(x: U) -> int
    return 999
end
`)

	t.Run("ajuda do modulo errado nao satisfaz a dependencia", func(t *testing.T) {
		// processa precisa do 'ajuda' de colecoes; so o de "outro" foi
		// importado — a validacao tem de recusar, nao aceitar o homonimo
		// errado silenciosamente.
		err := compileErrAtRoot(t, root, `
use colecoes select processa
use outro select ajuda
let ns: int[] = [1]
processa(ns)
`)
		if err == nil || !strings.Contains(err.Error(), "adicione ao select") {
			t.Fatalf("esperava erro de dependencia (modulo errado), veio %v", err)
		}
	})

	t.Run("ajuda do modulo certo satisfaz e e a que roda", func(t *testing.T) {
		// "outro" importado ANTES: registry.Funcs["ajuda"] fica sobrescrito
		// por colecoes (ultimo import vence, mesma semantica de select* já
		// provada em TestHomonymousTemplatesDedupIsSafe) — processa's corpo
		// e a propria validacao tem de enxergar o 'ajuda' de colecoes (valor
		// de retorno 100), nunca o de "outro" (999).
		got := captureVMSourceAtRoot(t, root, `
use outro select ajuda
use colecoes select processa, ajuda
let ns: int[] = [1]
test_report(processa(ns))
`)
		expectInt(t, got, 100, "processa deve chamar ajuda de colecoes, nao de outro")
	})
}

// I1 da revisao final de branch: `use` DENTRO de corpo de funcao (forma legal
// da linguagem — ver TestRuntimeFunctionBodyOnlyWildcardDoesNotInvalidateModule
// em module_exports_test.go) que traga um template generico registrava o
// template no meio da compilacao, DEPOIS da decisao hasGenerics()/two-pass
// (que so varre `use` de TOPO). A chamada seguinte batia no guard defensivo
// do pass 2 com "bug do compilador de genéricos" e uma linha deslocada.
// Agora e um erro acionavel, com a saida obvia. Suporte de verdade a `use`
// aninhado de template esta fora de escopo.
func TestNestedUseImportingTemplateIsActionableError(t *testing.T) {
	root := t.TempDir()
	write(t, root, "colecoes.nx", "func ident<T>(x: T) -> T\n    return x\nend\n")

	cases := []struct {
		name    string
		program string
	}{
		{
			name:    "select nominal",
			program: "func run() -> int\n    use colecoes select ident\n    return ident(41)\nend\nrun()",
		},
		{
			name:    "select *",
			program: "func run() -> int\n    use colecoes select *\n    return ident(41)\nend\nrun()",
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			err := compileErrAtRoot(t, root, tt.program)
			if err == nil {
				t.Fatal("esperava erro para template importado em corpo de funcao")
			}
			want := "template genérico importado dentro de corpo de função não é suportado — mova o 'use' para o top level"
			if !strings.Contains(err.Error(), want) {
				t.Fatalf("erro = %v, quer conter %q", err, want)
			}
			// A linha e a do `use`, nao a do call site nem a linha 1 do
			// guard defensivo antigo.
			if !strings.HasPrefix(err.Error(), "[line 2]") {
				t.Fatalf("erro = %v, quer prefixo [line 2] (a linha do `use`)", err)
			}
		})
	}
}

// I1, contra-prova: `use` aninhado de modulo SEM template continua legal — a
// checagem nova nao pode transformar o caso suportado em erro.
func TestNestedUseWithoutTemplateStillCompiles(t *testing.T) {
	root := t.TempDir()
	write(t, root, "comum.nx", "func dobro(x: int) -> int\n    return x * 2\nend\n")
	got := captureVMSourceAtRoot(t, root, `
func run() -> int
    use comum select dobro
    return dobro(21)
end
test_report(run())
`)
	expectInt(t, got, 42, "use aninhado sem generico continua valido")
}

// I3, primeira linha sem cobertura do catalogo §9: "conflito de shadowing".
// O corpo do template importado referencia 'base', que existe NOS DOIS lados
// com tipos DIFERENTES — sem o gate, o 'base' do importador seria capturado
// em silencio no lugar do binding do modulo definidor.
func TestImportedTemplateShadowingConflictIsError(t *testing.T) {
	root := t.TempDir()
	write(t, root, "colecoes.nx", `
let base: int = 10
func processa<T>(arr: T[]) -> int
    return base + length(arr)
end
`)
	err := compileErrAtRoot(t, root, `
use colecoes select processa
let base: string = "outro"
let ns: int[] = [1]
processa(ns)
`)
	if err == nil {
		t.Fatal("esperava erro de shadowing entre importador e modulo definidor")
	}
	for _, fragment := range []string{"conflito de shadowing", "'base'", "no importador", "'colecoes'"} {
		if !strings.Contains(err.Error(), fragment) {
			t.Fatalf("erro = %v, quer conter %q", err, fragment)
		}
	}
}

// I3, segunda linha sem cobertura do catalogo §9: "referencia 'X', não
// declarado no módulo 'Y'". O nome livre do corpo do template NAO existe no
// modulo definidor mas resolve no importador — o unico caso onde a
// heuristica "ausente dos dois lados = builtin" nao vale, porque ha risco
// real de captura por homonimo.
func TestImportedTemplateReferencesUndeclaredNameIsError(t *testing.T) {
	root := t.TempDir()
	write(t, root, "colecoes.nx", `
func processa<T>(arr: T[]) -> int
    externo()
    return length(arr)
end
`)
	err := compileErrAtRoot(t, root, `
use colecoes select processa
func externo() -> void
end
let ns: int[] = [1]
processa(ns)
`)
	if err == nil {
		t.Fatal("esperava erro de nome nao declarado no modulo definidor")
	}
	for _, fragment := range []string{"referencia 'externo'", "não declarado no módulo 'colecoes'"} {
		if !strings.Contains(err.Error(), fragment) {
			t.Fatalf("erro = %v, quer conter %q", err, fragment)
		}
	}
}

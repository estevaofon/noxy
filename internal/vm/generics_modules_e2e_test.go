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

package compiler

// Testes de compilador para cross-modulo de genericos (spec §8): o validator
// (loadModuleDeclarations/discoverModuleExports, ja exercitado pela Task 5)
// continua carregando um modulo cujo UNICO conteudo e um template — a Task 8
// ja fazia o validator pular templates ao compilar o modulo sozinho, este
// teste so garante que o caminho de descoberta de exports concorda. O
// segundo teste fixa a regra do §8/R8 sobre homonimo func/struct: nomes que
// colidem entre as duas familias de template tem de ser rejeitados na
// REGISTRO, nao silenciosamente colidir em instanceName mais tarde.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/estevaofon/noxy/internal/ast"
)

func TestModuleWithOnlyATemplateIsLoadable(t *testing.T) {
	root := t.TempDir()
	modulePath := filepath.Join(root, "soterrado.nx")
	if err := os.WriteFile(modulePath, []byte("func primeiro<T>(arr: T[]) -> T\n    return arr[0]\nend\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	compiler := NewWithState(make(map[string]ast.NoxyType), make(map[string]*ast.StructStatement), filepath.Join(root, "main.nx"))
	exports, loadable := compiler.discoverModuleExports("soterrado")
	if !loadable {
		t.Fatal("modulo com so um template generico deveria continuar loadable")
	}
	if _, ok := exports["primeiro"]; !ok {
		t.Fatal("export do template 'primeiro' nao foi descoberto")
	}
}

// TestFuncStructTemplateHomonymCollisionIsRejected fixa a nota da revisao da
// Task 9 (relatada no brief da Task 12): instanceName (generics_substitute.go)
// qualifica so por modulo+base+args, sem marcar familia (func vs struct) —
// `func Foo<T>` e `struct Foo<T>` com o MESMO nome colidiriam no MESMO nome
// qualificado (`main::Foo<int>`) se ambos fossem instanciados com a mesma
// tupla, um sobrescrevendo o c.globals/c.structs do outro em silencio.
// registerFuncTemplate/registerStructTemplate (generics.go) fecham isso na
// fronteira de REGISTRO: a segunda declaracao a reivindicar o nome, seja
// qual for a familia, e um erro de compilacao claro.
func TestFuncStructTemplateHomonymCollisionIsRejected(t *testing.T) {
	t.Run("struct depois de func", func(t *testing.T) {
		_, _, err := New().Compile(parse(`
func Foo<T>(x: T) -> T
    return x
end
struct Foo<T>
    valor: T
end
`))
		if err == nil {
			t.Fatal("esperava erro de colisao func/struct, compilou sem erro")
		}
		if !strings.Contains(err.Error(), "Foo") {
			t.Fatalf("mensagem de erro nao menciona o nome colidido: %v", err)
		}
	})

	t.Run("func depois de struct", func(t *testing.T) {
		_, _, err := New().Compile(parse(`
struct Bar<T>
    valor: T
end
func Bar<T>(x: T) -> T
    return x
end
`))
		if err == nil {
			t.Fatal("esperava erro de colisao struct/func, compilou sem erro")
		}
		if !strings.Contains(err.Error(), "Bar") {
			t.Fatalf("mensagem de erro nao menciona o nome colidido: %v", err)
		}
	})
}

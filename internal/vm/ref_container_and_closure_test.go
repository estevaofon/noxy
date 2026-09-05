package vm

import (
	"strings"
	"testing"

	"github.com/estevaofon/noxy/internal/value"
)

// Refs cujo contêiner é ele mesmo um ref (`ref r.x`, `ref r[i]` com r: ref
// Point / ref int[]), deref de ref nulo, encaminhamento `ref r` de um
// parâmetro ref, `addr` para cada espécie de referência, closure dentro de
// closure (upvalue de upvalue) e inicialização padrão de struct/func. São
// ramos do executor e do compilador que o perfil mostrou sem teste Go.

func TestRefToFieldAndIndexThroughRefContainerWritesThrough(t *testing.T) {
	got := captureVMSource(t, `
struct Point
    x: int
    y: int
end
func main()
    let p: Point = Point(1, 2)
    let r: ref Point = ref p
    let rx: ref int = ref r.x
    *rx = 9
    let a: int[] = [1, 2]
    let ra: ref int[] = ref a
    let e: ref int = ref ra[1]
    *e = 7
    test_report([p.x, a[1]])
end
main()
`)
	cells := semArray(t, got)
	if len(cells) != 2 || cells[0].Int() != 9 || cells[1].Int() != 7 {
		t.Fatalf("[p.x, a[1]] = %s, want [9, 7]", got.String())
	}
}

// I5 (revisao final #82): ler atraves de um ref nulo passou de null
// silencioso a erro de runtime, tambem no topo do programa. (Spec §2.4: o
// null so alcanca o OP_DEREF pela fronteira `any` — no caminho tipado a
// leitura de um `ref int?` sem teste e erro de compilacao.)
func TestDerefOfNullRefErrorsAtTopLevel(t *testing.T) {
	err := interpretVMSource(t, New(), "let a: any = null\nprint(*a)\n")
	if err == nil || !strings.Contains(err.Error(), "cannot dereference null reference") {
		t.Fatalf("*null: err = %v, want 'cannot dereference null reference'", err)
	}
}

// R1 (spec 2026-08-24-explicit-ref): um parametro `ref int` ja e a
// referencia — encaminha-lo para outra chamada `ref T` e so passa-lo direto,
// sem `ref` (que agora seria "ja e uma referencia").
func TestRefParameterIsForwardedAsPlainValue(t *testing.T) {
	got := captureVMSource(t, `
func inc(r: ref int) -> void
    *r = *r + 1
end
func fwd(r: ref int) -> void
    inc(r)
end
let x: int = 1
fwd(ref x)
test_report(x)
`)
	if got.Type != value.VAL_INT || got.Int() != 2 {
		t.Fatalf("x = %s, want 2", got.String())
	}
}

// addr(ref …) identifica o alvo por espécie: global pelo nome, campo pela
// posição no dono, elemento pelo índice; local/upvalue pelo endereço da caixa.
func TestAddrDescribesEachReferenceKind(t *testing.T) {
	got := captureVMSource(t, `
struct Point
    x: int
    y: int
end
let g: int = 1
func kinds() -> string[]
    let p: Point = Point(1, 2)
    let arr: int[] = [1]
    let up: int = 3
    let inner: func() -> string = func() -> string
        return addr(ref up)
    end
    return [addr(ref g), addr(ref p.x), addr(ref arr[0]), inner()]
end
test_report(kinds())
`)
	cells := semArray(t, got)
	if len(cells) != 4 {
		t.Fatalf("esperava 4 endereços, obtido %s", got.String())
	}
	texts := make([]string, len(cells))
	for i, cell := range cells {
		texts[i], _ = cell.Obj.(string)
	}
	if texts[0] != "<global g>" {
		t.Fatalf("addr(ref g) = %q, want <global g>", texts[0])
	}
	if !strings.HasPrefix(texts[1], "<prop x of ") {
		t.Fatalf("addr(ref p.x) = %q, want prefixo '<prop x of '", texts[1])
	}
	// issue #83: `addr` de um empréstimo passou a descrever o CAMINHO. O
	// formato antigo, "<index 0>", não era identidade nenhuma — omitia o
	// contêiner, então `addr(ref a[0])` e `addr(ref b[0])` eram indistinguíveis
	// para dois arrays diferentes, que é exatamente o que a spec diz que `addr`
	// serve para responder ("gives the identity as a printable value").
	if !strings.HasPrefix(texts[2], "<index 0 of ") {
		t.Fatalf("addr(ref arr[0]) = %q, want prefixo '<index 0 of '", texts[2])
	}
	if texts[3] == "" || strings.HasPrefix(texts[3], "<") {
		t.Fatalf("addr(ref upvalue) = %q, want endereço da caixa", texts[3])
	}
}

// Closure dentro de closure: a interna captura o upvalue da intermediária
// (OP_CLOSURE com isLocal=false + resolveUpvalue em dois níveis).
func TestClosureCapturesUpvalueOfEnclosingClosure(t *testing.T) {
	got := captureVMSource(t, `
func outer() -> int
    let x: int = 10
    let mid: func() -> func() -> int = func() -> func() -> int
        return func() -> int
            return x + 1
        end
    end
    return mid()()
end
test_report(outer())
`)
	if got.Type != value.VAL_INT || got.Int() != 11 {
		t.Fatalf("closure aninhada devolveu %s, want 11", got.String())
	}
}

// Spec §2.4 (issue #105): `let p: Point` e `let r: ref int` sem inicializador
// deixaram de existir — struct e ref nus nunca são null, e o checador pede o
// inicializador ou o `?`. Só `Point?`, `ref int?` e `any` começam em null.
// (`let f: func` e `let c: chan int` já eram erro desde a issue #61 item 1;
// ver internal/compiler/let_default_init_test.go.)
func TestDefaultInitializationRequiresNullableForStructAndRef(t *testing.T) {
	for _, tc := range []struct{ decl, want string }{
		{"let p: Point", "variable 'p' needs an initializer: Point has no default value"},
		{"let r: ref int", "variable 'r' needs an initializer: ref int has no default value"},
	} {
		err := interpretOrCompileErr(t, New(), "struct Point\n    x: int\nend\n"+tc.decl+"\n")
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("%s: err = %v, want %q", tc.decl, err, tc.want)
		}
	}
	got := captureVMSource(t, `
struct Point
    x: int
end
let p: Point?
let r: ref int?
let a: any
test_report([to_str(p), to_str(r), to_str(a)])
`)
	for i, cell := range semArray(t, got) {
		if s, ok := cell.Obj.(string); !ok || s != "null" {
			t.Fatalf("célula %d = %s, want null", i, cell.String())
		}
	}
}

// API do executor: InterpretWithGlobals(code, nil) roda no ambiente raiz;
// InterpretWithEnvironment(code, nil) é erro, não panic.
func TestInterpretWithGlobalsAndEnvironmentNilArguments(t *testing.T) {
	machine := New()
	code := compileVMSource(t, "let x: int = 1\n")
	if err := machine.InterpretWithGlobals(code, nil); err != nil {
		t.Fatalf("InterpretWithGlobals(nil) deveria usar o ambiente raiz: %v", err)
	}
	if _, ok := machine.GetGlobal("x"); !ok {
		t.Fatal("x deveria ter sido definido no ambiente raiz")
	}
	if err := machine.InterpretWithEnvironment(code, nil); err == nil || !strings.Contains(err.Error(), "requires a global environment") {
		t.Fatalf("InterpretWithEnvironment(nil) deveria falhar com erro claro, obtido %v", err)
	}
}

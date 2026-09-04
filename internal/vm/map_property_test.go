package vm

import (
	"strings"
	"testing"
)

// Issue #133: o objeto do namespace de um modulo e um ObjMap sobre o
// bindingStore do modulo (GlobalEnvironment.ExportMap). Escrever nele pelo
// OP_SET_PROPERTY e tomar `ref` de um membro (REF_PROPERTY) precisam de um
// ramo de mapa — testados aqui pela fronteira `any`, que chega aos mesmos
// opcodes sem depender do compilador.

func TestSetPropertyOnMapThroughAnyWritesTheEntry(t *testing.T) {
	root := writeModuleFiles(t, map[string]string{})
	reported, err := runModuleProgram(t, root, `let m: any = {"x": 1}
m.x = 2
test_report(m.x)
`)
	if err != nil {
		t.Fatal(err)
	}
	if reported.Int() != 2 {
		t.Fatalf("got %v, want 2", reported)
	}
}

func TestSetPropertyOnMapThroughAnyRejectsMissingKey(t *testing.T) {
	root := writeModuleFiles(t, map[string]string{})
	_, err := runModuleProgram(t, root, "let m: any = {\"x\": 1}\nm.y = 2\n")
	if err == nil || !strings.Contains(err.Error(), "undefined property 'y' in module/map") {
		t.Fatalf("error=%v", err)
	}
}

func TestSetPropertyOnMapThroughAnyReleasesTheOldComposite(t *testing.T) {
	// RC: o array antigo e liberado, o novo retido — o programa continua
	// lendo o novo depois de a variavel original sair de escopo.
	root := writeModuleFiles(t, map[string]string{})
	reported, err := runModuleProgram(t, root, `let m: any = {"xs": [1, 2]}
func swap() -> void
    let fresh: int[] = [7, 8, 9]
    m.xs = fresh
end
swap()
let xs: int[] = m.xs
test_report(length(xs))
`)
	if err != nil {
		t.Fatal(err)
	}
	if reported.Int() != 3 {
		t.Fatalf("got %v, want 3", reported)
	}
}

func TestRefPropertyOnMapThroughAnyMutatesTheEntry(t *testing.T) {
	root := writeModuleFiles(t, map[string]string{})
	reported, err := runModuleProgram(t, root, `let m: any = {"x": 1}
func bump(n: ref int) -> void
    *n = *n + 1
end
bump(ref m.x)
test_report(m.x)
`)
	if err != nil {
		t.Fatal(err)
	}
	if reported.Int() != 2 {
		t.Fatalf("got %v, want 2", reported)
	}
}

func TestRefPropertyOnMapThroughAnyIntermediateStep(t *testing.T) {
	// `ref m.p.x`: o passo intermediario (descend) atravessa o mapa.
	root := writeModuleFiles(t, map[string]string{})
	reported, err := runModuleProgram(t, root, `struct P
    x: int
end
let m: any = {"p": P(1)}
func bump(n: ref int) -> void
    *n = *n + 1
end
bump(ref m.p.x)
let p: P = m.p
test_report(p.x)
`)
	if err != nil {
		t.Fatal(err)
	}
	if reported.Int() != 2 {
		t.Fatalf("got %v, want 2", reported)
	}
}

// Issue #133 (revisao adversarial, caso 3): com tipo estatico map[K, V] a
// escrita com ponto e a mesma de `m["chave"] = v` — checada no compilador e
// gravada na MESMA entrada em runtime.
func TestMapDotWriteOnStaticMapTypeWritesTheEntry(t *testing.T) {
	root := writeModuleFiles(t, map[string]string{})
	reported, err := runModuleProgram(t, root, `let mm: map[string, int] = {"a": 1}
mm.a = 2
test_report(mm["a"])
`)
	if err != nil {
		t.Fatal(err)
	}
	if reported.Int() != 2 {
		t.Fatalf("got %v, want 2", reported)
	}
}

// Issue #133: as duas guardas de R1 (spec §2.3) sobre uma ENTRADA de mapa que
// ja guarda uma referencia — a escrita (field_ops.go, OP_SET_PROPERTY) e o
// `ref` sobre ela (executor.go, OP_REF_PROPERTY). O `*m.r = 5` continua fora
// daqui: OP_SET_PROPERTY_DEREF tem panic pre-existente, em issue proprio.
func TestMapEntryHoldingRefRefusesRawWrite(t *testing.T) {
	root := writeModuleFiles(t, map[string]string{})
	_, err := runModuleProgram(t, root, `let n: int = 1
let m: any = {"r": ref n}
m.r = 5
`)
	if err == nil || !strings.Contains(err.Error(), "slot 'r' already holds a reference") {
		t.Fatalf("error=%v", err)
	}
}

func TestMapEntryHoldingRefRefusesReReference(t *testing.T) {
	root := writeModuleFiles(t, map[string]string{})
	_, err := runModuleProgram(t, root, `let n: int = 1
let m: any = {"r": ref n}
func bump(x: ref int) -> void
    *x = *x + 1
end
bump(ref m.r)
`)
	if err == nil || !strings.Contains(err.Error(), "slot 'r' already holds a reference") {
		t.Fatalf("error=%v", err)
	}
}

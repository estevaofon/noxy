package vm

import (
	"testing"

	"noxy-vm/internal/value"
)

func assertReportedInt(t *testing.T, got value.Value, want int64) {
	t.Helper()
	if got.Type != value.VAL_INT || got.AsInt != want {
		t.Fatalf("reported %#v, want int %d", got, want)
	}
}

// Um struct do programa com um campo tipado pelo nome QUALIFICADO de um
// struct de outro modulo (`file: io.File`, `quando: time.DateTime`,
// `partes: strings.SplitResult`) tem de ser construivel: a forma qualificada
// e a forma por `select` designam o mesmo tipo nominal (#56 item 8) em TODAS
// as posicoes, inclusive campo de struct. Antes, o ConstructorType do struct
// local ficava incompleto (c.structs nao conhece "io.File") e todo construtor
// falhava com "struct constructor has incomplete runtime type metadata".
func TestStructWithQualifiedStdlibStructFieldIsConstructible(t *testing.T) {
	tests := []struct {
		name   string
		source string
		want   int64
	}{
		{"io.File, direct argument", `use io
struct Reader
    file: io.File
    pos: int
end
let r: Reader = Reader(io.stdin(), 7)
test_report(r.pos)`, 7},
		{"io.File, argument via variable", `use io
struct Reader
    file: io.File
    pos: int
end
let f: io.File = io.stdin()
let r: Reader = Reader(f, 8)
test_report(r.pos)`, 8},
		{"io.File, constructor inside a function", `use io
struct Reader
    file: io.File
    pos: int
end
func make() -> Reader
    return Reader(io.stdin(), 9)
end
let r: Reader = make()
test_report(r.pos)`, 9},
		{"io.File, struct passed as argument", `use io
struct Reader
    file: io.File
    pos: int
end
func pos_of(r: Reader) -> int
    return r.pos
end
test_report(pos_of(Reader(io.stdin(), 10)))`, 10},
		{"time.DateTime", `use time
struct Evento
    nome: string
    quando: time.DateTime
end
let e: Evento = Evento("x", time.make_datetime(2026, 8, 21, 0, 0, 0))
test_report(e.quando.year)`, 2026},
		{"strings.SplitResult", `use strings
struct Linha
    partes: strings.SplitResult
end
let l: Linha = Linha(strings.split("a,b,c", ","))
test_report(l.partes.count)`, 3},
		{"control: same type via select", `use io
use io select File
struct Reader
    file: File
    pos: int
end
let r: Reader = Reader(io.stdin(), 11)
test_report(r.pos)`, 11},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assertReportedInt(t, captureVMSource(t, test.source), test.want)
		})
	}
}

// O mesmo para um modulo do usuario importado como namespace (`use geometry2`
// e campo `a: geometry2.Point`).
func TestStructWithQualifiedUserModuleStructFieldIsConstructible(t *testing.T) {
	root := t.TempDir()
	write(t, root, "geometry2.nx", `struct Point
    x: int
    y: int
end
func origin() -> Point
    return Point(0, 0)
end
`)
	got := captureVMSourceAtRoot(t, root, `use geometry2
struct Segment
    a: geometry2.Point
    b: geometry2.Point
end
let s: Segment = Segment(geometry2.Point(1, 2), geometry2.origin())
test_report(s.a.x + s.a.y + s.b.x)`)
	assertReportedInt(t, got, 3)
}

// Os campos de um struct IMPORTADO sao resolvidos no escopo de structs do
// modulo de ORIGEM, nao no registro do importador: `Outer` (de mod_a) tem um
// campo `i: Inner`, e `Inner` nunca foi importado pelo programa. Vale para a
// forma por `select Outer` e para a forma qualificada `mod_a.Outer`.
func TestImportedStructFieldWithNestedModuleStructIsConstructible(t *testing.T) {
	root := t.TempDir()
	write(t, root, "mod_a.nx", `struct Inner
    v: int
end
struct Outer
    i: Inner
end
func make(v: int) -> Outer
    return Outer(Inner(v))
end
`)
	tests := []struct {
		name   string
		source string
	}{
		{"select by name", `use mod_a
use mod_a select Outer
struct W
    o: Outer
end
let w: W = W(mod_a.make(5))
test_report(w.o.i.v)`},
		{"qualified", `use mod_a
struct W
    o: mod_a.Outer
end
let w: W = W(mod_a.make(5))
test_report(w.o.i.v)`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assertReportedInt(t, captureVMSourceAtRoot(t, root, test.source), 5)
		})
	}
}

// A forma qualificada e a forma por select sao intercambiaveis no caminho
// ESTATICO: um valor tipado `File` entra num campo `io.File` e vice-versa, e
// o campo lido de volta aceita a outra anotacao (typesEquivalent, #56 item 8).
func TestQualifiedAndSelectedFieldTypesAreInterchangeable(t *testing.T) {
	got := captureVMSource(t, `use io
use io select File
struct ViaQualified
    file: io.File
end
struct ViaSelect
    file: File
end
let a: File = io.stdin()
let b: io.File = io.stdin()
let q: ViaQualified = ViaQualified(a)
let s: ViaSelect = ViaSelect(b)
let back: File = q.file
let back2: io.File = s.file
let fd1: int = back.fd
let fd2: int = back2.fd
test_report(fd1 - fd2)`)
	assertReportedInt(t, got, 0)
}

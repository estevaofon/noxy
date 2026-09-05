package ext

import (
	"strings"
	"testing"

	"github.com/estevaofon/noxy/internal/value"
)

func mustRoundTrip(t *testing.T, v value.Value) value.Value {
	t.Helper()
	data, err := EncodeValue(v, DefaultLimits())
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	back, err := DecodeValue(data, DefaultLimits())
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	return back
}

func TestNXBScalarRoundTrip(t *testing.T) {
	if got := mustRoundTrip(t, value.NewInt(-42)); got.Type != value.VAL_INT || got.Int() != -42 {
		t.Fatalf("int round trip: %#v", got)
	}
	if got := mustRoundTrip(t, value.NewFloat(3.5)); got.Type != value.VAL_FLOAT || got.Float() != 3.5 {
		t.Fatalf("float round trip: %#v", got)
	}
	if got := mustRoundTrip(t, value.NewBool(true)); got.Type != value.VAL_BOOL || !got.Bool() {
		t.Fatalf("bool round trip: %#v", got)
	}
	if got := mustRoundTrip(t, value.NewNull()); got.Type != value.VAL_NULL {
		t.Fatalf("null round trip: %#v", got)
	}
	if got := mustRoundTrip(t, value.NewString("héllo")); got.Type != value.VAL_OBJ || got.Obj.(string) != "héllo" {
		t.Fatalf("string round trip: %#v", got)
	}
	if got := mustRoundTrip(t, value.NewBytes("\x00\x01\xff")); got.Type != value.VAL_BYTES || got.Obj.(string) != "\x00\x01\xff" {
		t.Fatalf("bytes round trip: %#v", got)
	}
}

func TestNXBArrayMapRoundTrip(t *testing.T) {
	arr := value.NewArray([]value.Value{value.NewInt(1), value.NewString("a")})
	got := mustRoundTrip(t, arr)
	elems := got.Obj.(*value.ObjArray).Elements
	if len(elems) != 2 || elems[0].Int() != 1 || elems[1].Obj.(string) != "a" {
		t.Fatalf("array round trip: %#v", got)
	}

	m := value.NewMap()
	m.Obj.(*value.ObjMap).Set("k", value.NewInt(7))
	m.Obj.(*value.ObjMap).Set(int64(3), value.NewString("x"))
	back := mustRoundTrip(t, m).Obj.(*value.ObjMap)
	if v, ok := back.Get("k"); !ok || v.Int() != 7 {
		t.Fatalf("map string key: %#v", back.Snapshot())
	}
	if v, ok := back.Get(int64(3)); !ok || v.Obj.(string) != "x" {
		t.Fatalf("map int key: %#v", back.Snapshot())
	}
}

func TestNXBStructEncodesDecodesAsMap(t *testing.T) {
	def := &value.ObjStruct{Name: "Point", Fields: []string{"x", "y"}}
	inst := value.NewInstanceWith(def, map[string]value.Value{
		"x": value.NewInt(1), "y": value.NewInt(2),
	})
	back := mustRoundTrip(t, inst)
	mp, ok := back.Obj.(*value.ObjMap)
	if !ok {
		t.Fatalf("struct must decode to struct-shaped map (spec §3), got %#v", back)
	}
	if v, ok := mp.Get("y"); !ok || v.Int() != 2 {
		t.Fatalf("field y: %#v", mp.Snapshot())
	}
}

func TestNXBRejectsNonEncodable(t *testing.T) {
	ch := value.NewChannel(1)
	if _, err := EncodeValue(ch, DefaultLimits()); err == nil || !strings.Contains(err.Error(), "channel") {
		t.Fatalf("channel must be rejected by name, got %v", err)
	}
}

func TestNXBDepthCap(t *testing.T) {
	v := value.NewArray(nil)
	for i := 0; i < 70; i++ {
		v = value.NewArray([]value.Value{v})
	}
	if _, err := EncodeValue(v, DefaultLimits()); err == nil || !strings.Contains(err.Error(), "depth") {
		t.Fatalf("expected depth error, got %v", err)
	}
}

func TestNXBSizeCap(t *testing.T) {
	big := value.NewBytes(strings.Repeat("a", 100))
	if _, err := EncodeValue(big, Limits{MaxBytes: 50}); err == nil {
		t.Fatal("expected size cap error")
	}
}

func TestNXBDeterministicMapEncoding(t *testing.T) {
	build := func() value.Value {
		m := value.NewMap()
		om := m.Obj.(*value.ObjMap)
		om.Set("b", value.NewInt(2))
		om.Set(int64(9), value.NewInt(9))
		om.Set("a", value.NewInt(1))
		return m
	}
	d1, err1 := EncodeValue(build(), DefaultLimits())
	d2, err2 := EncodeValue(build(), DefaultLimits())
	if err1 != nil || err2 != nil || string(d1) != string(d2) {
		t.Fatalf("map encoding must be deterministic")
	}
}

func TestNXBTruncatedInputFails(t *testing.T) {
	data, _ := EncodeValue(value.NewString("hello"), DefaultLimits())
	if _, err := DecodeValue(data[:len(data)-2], DefaultLimits()); err == nil {
		t.Fatal("truncated input must fail")
	}
}

// TestNXBArrayCountBombIsBounded prova que um guest hostil nao consegue mais
// pedir uma pre-alocacao de ~128 GiB anunciando um count de array gigante
// sem ter os bytes correspondentes no payload (achado de revisao: o hint de
// capacidade agora e limitado pelos bytes restantes, entao a alocacao real
// fica na casa de bytes, nao gigabytes, e o decode falha rapido por
// truncamento).
func TestNXBArrayCountBombIsBounded(t *testing.T) {
	bomb := []byte{nxbArray, 0xFF, 0xFF, 0xFF, 0xFF}
	_, err := DecodeValue(bomb, DefaultLimits())
	if err == nil || !strings.Contains(err.Error(), "truncated") {
		t.Fatalf("expected a truncation error, got %v", err)
	}
}

// TestNXBArgsCountBombIsBounded e o mesmo achado, mas no caminho de
// DecodeArgs (chamado com os argumentos de uma extension call) — o count de
// argumentos vem dos mesmos bytes nao confiaveis do guest.
func TestNXBArgsCountBombIsBounded(t *testing.T) {
	bomb := []byte{0xFF, 0xFF, 0xFF, 0xFF}
	_, err := DecodeArgs(bomb, DefaultLimits())
	if err == nil || !strings.Contains(err.Error(), "truncated") {
		t.Fatalf("expected a truncation error, got %v", err)
	}
}

func TestNXBArgsRoundTrip(t *testing.T) {
	args := []value.Value{value.NewInt(1), value.NewBytes("zz")}
	data, err := EncodeArgs(args, DefaultLimits())
	if err != nil {
		t.Fatalf("encode args: %v", err)
	}
	back, err := DecodeArgs(data, DefaultLimits())
	if err != nil || len(back) != 2 || back[0].Int() != 1 || back[1].Obj.(string) != "zz" {
		t.Fatalf("args round trip: %v %#v", err, back)
	}
}

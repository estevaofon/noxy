package ext

import (
	"testing"

	"github.com/estevaofon/noxy/internal/value"
)

func TestHelloBodyCarriesExportsInOrder(t *testing.T) {
	body, err := helloBody("v0.23.0", "term", []string{"term_a", "term_b"}, DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	m, err := decodeBodyMap(body, DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	if proto, _ := mapString(m, "protocol"); proto != ProtocolVersion {
		t.Fatalf("protocol: %q", proto)
	}
	if ext, _ := mapString(m, "extension"); ext != "term" {
		t.Fatalf("extension: %q", ext)
	}
	if noxy, _ := mapString(m, "noxy"); noxy != "v0.23.0" {
		t.Fatalf("noxy: %q", noxy)
	}
	exports, ok := m.Get("exports")
	if !ok {
		t.Fatal("exports missing")
	}
	elems := exports.Obj.(*value.ObjArray).Elements
	if len(elems) != 2 || elems[0].Obj.(string) != "term_a" || elems[1].Obj.(string) != "term_b" {
		t.Fatalf("exports order: %#v", elems)
	}
}

func TestMapAccessorsTolerateMissingAndWrongTypes(t *testing.T) {
	body, _ := encodeStringMap(map[string]value.Value{
		"message": value.NewString("boom"),
		"level":   value.NewInt(2),
	}, DefaultLimits())
	m, err := decodeBodyMap(body, DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	if s, ok := mapString(m, "message"); !ok || s != "boom" {
		t.Fatalf("message: %q %v", s, ok)
	}
	if n, ok := mapInt(m, "level"); !ok || n != 2 {
		t.Fatalf("level: %d %v", n, ok)
	}
	if _, ok := mapString(m, "level"); ok {
		t.Fatal("level is an int, not a string")
	}
	if _, ok := mapString(m, "absent"); ok {
		t.Fatal("absent key must report !ok")
	}
}

func TestDecodeBodyMapRejectsNonMap(t *testing.T) {
	body, _ := EncodeValue(value.NewInt(1), DefaultLimits())
	if _, err := decodeBodyMap(body, DefaultLimits()); err == nil {
		t.Fatal("an int body is not a map")
	}
}

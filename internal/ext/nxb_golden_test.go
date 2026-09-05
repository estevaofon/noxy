package ext

import (
	"bytes"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/estevaofon/noxy/internal/value"
)

func loadNXBVectors(t testing.TB) map[string][]byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "nxb", "vectors.txt"))
	if err != nil {
		t.Fatal(err)
	}
	out := map[string][]byte{}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) != 2 {
			t.Fatalf("malformed vector line %q", line)
		}
		raw, err := hex.DecodeString(fields[1])
		if err != nil {
			t.Fatalf("vector %s: %v", fields[0], err)
		}
		out[fields[0]] = raw
	}
	return out
}

func TestNXBGoldenVectors(t *testing.T) {
	vectors := loadNXBVectors(t)
	strMap := value.NewMap()
	strMap.Obj.(*value.ObjMap).Set("b", value.NewInt(1))
	strMap.Obj.(*value.ObjMap).Set("a", value.NewBool(true))
	intMap := value.NewMap()
	intMap.Obj.(*value.ObjMap).Set(int64(2), value.NewString("x"))
	intMap.Obj.(*value.ObjMap).Set(int64(1), value.NewString("y"))
	point := value.NewInstanceWith(&value.ObjStruct{Name: "Point", Fields: []string{"x", "y"}},
		map[string]value.Value{"x": value.NewInt(1), "y": value.NewFloat(2)})
	cases := []struct {
		name string
		v    value.Value
	}{
		{"null", value.NewNull()},
		{"bool_true", value.NewBool(true)},
		{"int_minus_two", value.NewInt(-2)},
		{"float_one_point_five", value.NewFloat(1.5)},
		{"string_ola", value.NewString("olá")},
		{"bytes_two", value.NewBytes("\x00\xff")},
		{"array_int_string", value.NewArray([]value.Value{value.NewInt(7), value.NewString("a")})},
		{"map_string", strMap},
		{"map_int", intMap},
		{"struct_point", point},
	}
	if len(cases) != len(vectors) {
		t.Fatalf("every vector needs a case: %d cases, %d vectors", len(cases), len(vectors))
	}
	for _, c := range cases {
		want, ok := vectors[c.name]
		if !ok {
			t.Fatalf("vector %s missing from vectors.txt", c.name)
		}
		got, err := EncodeValue(c.v, DefaultLimits())
		if err != nil {
			t.Fatalf("%s: encode: %v", c.name, err)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("%s: encode\n got %x\nwant %x", c.name, got, want)
		}
		decoded, err := DecodeValue(want, DefaultLimits())
		if err != nil {
			t.Fatalf("%s: decode: %v", c.name, err)
		}
		if c.name == "struct_point" {
			// Struct volta como map (spec wasm §3): confere os campos.
			m := decoded.Obj.(*value.ObjMap)
			if x, _ := m.Get("x"); x.Int() != 1 {
				t.Fatalf("struct_point x: %#v", x)
			}
			if y, _ := m.Get("y"); y.Float() != 2 {
				t.Fatalf("struct_point y: %#v", y)
			}
			continue
		}
		again, err := EncodeValue(decoded, DefaultLimits())
		if err != nil || !bytes.Equal(again, want) {
			t.Fatalf("%s: decode/encode round trip\n got %x\nwant %x (%v)", c.name, again, want, err)
		}
	}
}

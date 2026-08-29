package noxyplugin

import (
	"bytes"
	"encoding/hex"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func loadVectors(t *testing.T) map[string][]byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "internal", "ext", "testdata", "nxb", "vectors.txt"))
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
		raw, err := hex.DecodeString(fields[1])
		if err != nil {
			t.Fatal(err)
		}
		out[fields[0]] = raw
	}
	return out
}

func TestNXBGoldenVectorsSDK(t *testing.T) {
	vectors := loadVectors(t)
	cases := []struct {
		name string
		v    any
	}{
		{"null", nil},
		{"bool_true", true},
		{"int_minus_two", int64(-2)},
		{"float_one_point_five", 1.5},
		{"string_ola", "olá"},
		{"bytes_two", []byte{0x00, 0xff}},
		{"array_int_string", []any{int64(7), "a"}},
		{"map_string", map[string]any{"b": int64(1), "a": true}},
		{"map_int", map[int64]any{2: "x", 1: "y"}},
		{"struct_point", Struct{Name: "Point", Fields: []Field{Field{"x", int64(1)}, Field{"y", 2.0}}}},
	}
	if len(cases) != len(vectors) {
		t.Fatalf("%d cases for %d vectors", len(cases), len(vectors))
	}
	for _, c := range cases {
		want := vectors[c.name]
		got, err := encodeValue(nil, c.v, 0)
		if err != nil {
			t.Fatalf("%s: encode: %v", c.name, err)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("%s: encode\n got %x\nwant %x", c.name, got, want)
		}
		decoded, err := decodeValue(want)
		if err != nil {
			t.Fatalf("%s: decode: %v", c.name, err)
		}
		if !reflect.DeepEqual(decoded, c.v) {
			t.Fatalf("%s: decode\n got %#v\nwant %#v", c.name, decoded, c.v)
		}
	}
}

func TestNXBGoTypesEncode(t *testing.T) {
	vectors := loadVectors(t)
	for _, v := range []any{int(-2), int32(-2), int16(-2), int8(-2)} {
		got, err := encodeValue(nil, v, 0)
		if err != nil || !bytes.Equal(got, vectors["int_minus_two"]) {
			t.Fatalf("%T must encode as int: %x %v", v, got, err)
		}
	}
	got, _ := encodeValue(nil, []string{"a"}, 0)
	want, _ := encodeValue(nil, []any{"a"}, 0)
	if !bytes.Equal(got, want) {
		t.Fatal("typed slices encode as arrays")
	}
	got, _ = encodeValue(nil, map[string]int{"b": 1}, 0)
	want, _ = encodeValue(nil, map[string]any{"b": int64(1)}, 0)
	if !bytes.Equal(got, want) {
		t.Fatal("typed maps encode as maps")
	}
	if _, err := encodeValue(nil, uint64(1<<63), 0); err == nil {
		t.Fatal("uint64 above MaxInt64 cannot cross")
	}
	if _, err := encodeValue(nil, make(chan int), 0); err == nil {
		t.Fatal("a channel cannot cross")
	}
	if _, err := encodeValue(nil, map[bool]any{true: 1}, 0); err == nil {
		t.Fatal("map keys must be strings or ints")
	}
}

func TestNXBArgsAndStringMap(t *testing.T) {
	data, err := encodeArgs([]any{int64(1), "x"})
	if err != nil {
		t.Fatal(err)
	}
	args, err := decodeArgs(data)
	if err != nil || len(args) != 2 || args[0] != int64(1) || args[1] != "x" {
		t.Fatalf("args: %#v %v", args, err)
	}
	if _, err := decodeArgs(append(data, 0x00)); err == nil {
		t.Fatal("trailing bytes after the arguments are an error")
	}
	body, _ := encodeValue(nil, map[string]any{"protocol": "noxy-plugin/1", "exports": []any{"a"}}, 0)
	m, err := decodeStringMap(body)
	if err != nil || m["protocol"] != "noxy-plugin/1" {
		t.Fatalf("string map: %#v %v", m, err)
	}
	intBody, _ := encodeValue(nil, int64(3), 0)
	if _, err := decodeStringMap(intBody); err == nil {
		t.Fatal("an int is not a string map")
	}
}

package noxyplugin

import (
	"encoding/binary"
	"fmt"
	"math"
	"reflect"
	"sort"
)

// NXB v1 sobre tipos Go (spec wasm §2; vetores dourados em
// internal/ext/testdata/nxb). Mapeamento na spec 2026-08-29 §9.3.
const (
	tagNull   = 0x00
	tagBool   = 0x01
	tagInt    = 0x02
	tagFloat  = 0x03
	tagString = 0x04
	tagBytes  = 0x05
	tagArray  = 0x06
	tagMap    = 0x07
	tagStruct = 0x08

	maxDepth = 64
)

// Struct e um struct Noxy cruzando a fronteira: nome e campos na ordem de
// declaracao. Na volta ao host vira um map com forma de struct.
type Struct struct {
	Name   string
	Fields []Field
}

type Field struct {
	Name  string
	Value any
}

func appendU32(buf []byte, v uint32) []byte { return binary.LittleEndian.AppendUint32(buf, v) }

func appendInt(buf []byte, v int64) []byte {
	return binary.LittleEndian.AppendUint64(append(buf, tagInt), uint64(v))
}

func appendFloat(buf []byte, v float64) []byte {
	return binary.LittleEndian.AppendUint64(append(buf, tagFloat), math.Float64bits(v))
}

func appendBlob(buf []byte, tag byte, data []byte) []byte {
	buf = appendU32(append(buf, tag), uint32(len(data)))
	return append(buf, data...)
}

func encodeValue(buf []byte, v any, depth int) ([]byte, error) {
	if depth > maxDepth {
		return nil, fmt.Errorf("nxb: value nesting exceeds depth %d", maxDepth)
	}
	switch x := v.(type) {
	case nil:
		return append(buf, tagNull), nil
	case bool:
		b := byte(0)
		if x {
			b = 1
		}
		return append(buf, tagBool, b), nil
	case int:
		return appendInt(buf, int64(x)), nil
	case int8:
		return appendInt(buf, int64(x)), nil
	case int16:
		return appendInt(buf, int64(x)), nil
	case int32:
		return appendInt(buf, int64(x)), nil
	case int64:
		return appendInt(buf, x), nil
	case uint8:
		return appendInt(buf, int64(x)), nil
	case uint16:
		return appendInt(buf, int64(x)), nil
	case uint32:
		return appendInt(buf, int64(x)), nil
	case uint:
		if uint64(x) > math.MaxInt64 {
			return nil, fmt.Errorf("nxb: %d does not fit in an int", x)
		}
		return appendInt(buf, int64(x)), nil
	case uint64:
		if x > math.MaxInt64 {
			return nil, fmt.Errorf("nxb: %d does not fit in an int", x)
		}
		return appendInt(buf, int64(x)), nil
	case float32:
		return appendFloat(buf, float64(x)), nil
	case float64:
		return appendFloat(buf, x), nil
	case string:
		return appendBlob(buf, tagString, []byte(x)), nil
	case []byte:
		return appendBlob(buf, tagBytes, x), nil
	case Struct:
		return encodeStruct(buf, x, depth)
	case *Struct:
		if x == nil {
			return append(buf, tagNull), nil
		}
		return encodeStruct(buf, *x, depth)
	case []any:
		return encodeArray(buf, x, depth)
	case map[string]any:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		buf = appendU32(append(buf, tagMap), uint32(len(x)))
		for _, k := range keys {
			buf = appendBlob(buf, tagString, []byte(k))
			var err error
			if buf, err = encodeValue(buf, x[k], depth+1); err != nil {
				return nil, err
			}
		}
		return buf, nil
	case map[int64]any:
		keys := make([]int64, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
		buf = appendU32(append(buf, tagMap), uint32(len(x)))
		for _, k := range keys {
			buf = appendInt(buf, k)
			var err error
			if buf, err = encodeValue(buf, x[k], depth+1); err != nil {
				return nil, err
			}
		}
		return buf, nil
	case map[any]any:
		return encodeMixedMap(buf, x, depth)
	}
	return encodeReflect(buf, reflect.ValueOf(v), depth)
}

func encodeArray(buf []byte, items []any, depth int) ([]byte, error) {
	buf = appendU32(append(buf, tagArray), uint32(len(items)))
	for _, item := range items {
		var err error
		if buf, err = encodeValue(buf, item, depth+1); err != nil {
			return nil, err
		}
	}
	return buf, nil
}

func encodeStruct(buf []byte, s Struct, depth int) ([]byte, error) {
	buf = appendU32(append(buf, tagStruct), uint32(len(s.Name)))
	buf = append(buf, s.Name...)
	buf = appendU32(buf, uint32(len(s.Fields)))
	for _, f := range s.Fields {
		buf = appendBlob(buf, tagString, []byte(f.Name))
		var err error
		if buf, err = encodeValue(buf, f.Value, depth+1); err != nil {
			return nil, err
		}
	}
	return buf, nil
}

// encodeMixedMap: ints antes de strings, cada grupo ordenado — a mesma
// ordem deterministica do host.
func encodeMixedMap(buf []byte, m map[any]any, depth int) ([]byte, error) {
	var ints []int64
	var strs []string
	for k := range m {
		switch key := k.(type) {
		case int64:
			ints = append(ints, key)
		case int:
			ints = append(ints, int64(key))
		case string:
			strs = append(strs, key)
		default:
			return nil, fmt.Errorf("nxb: map key of type %T cannot cross the boundary", k)
		}
	}
	sort.Slice(ints, func(i, j int) bool { return ints[i] < ints[j] })
	sort.Strings(strs)
	buf = appendU32(append(buf, tagMap), uint32(len(m)))
	var err error
	for _, k := range ints {
		buf = appendInt(buf, k)
		item, ok := m[k]
		if !ok {
			item = m[int(k)]
		}
		if buf, err = encodeValue(buf, item, depth+1); err != nil {
			return nil, err
		}
	}
	for _, k := range strs {
		buf = appendBlob(buf, tagString, []byte(k))
		if buf, err = encodeValue(buf, m[k], depth+1); err != nil {
			return nil, err
		}
	}
	return buf, nil
}

// encodeReflect cobre slices e maps tipados ([]string, map[string]int, ...)
// e ponteiros; qualquer outro tipo Go nao cruza a fronteira.
func encodeReflect(buf []byte, rv reflect.Value, depth int) ([]byte, error) {
	switch rv.Kind() {
	case reflect.Ptr:
		if rv.IsNil() {
			return append(buf, tagNull), nil
		}
		return encodeValue(buf, rv.Elem().Interface(), depth)
	case reflect.Slice, reflect.Array:
		items := make([]any, rv.Len())
		for i := range items {
			items[i] = rv.Index(i).Interface()
		}
		return encodeArray(buf, items, depth)
	case reflect.Map:
		mixed := make(map[any]any, rv.Len())
		iter := rv.MapRange()
		for iter.Next() {
			key := iter.Key()
			switch key.Kind() {
			case reflect.String:
				mixed[key.String()] = iter.Value().Interface()
			case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
				mixed[key.Int()] = iter.Value().Interface()
			default:
				return nil, fmt.Errorf("nxb: map key of type %s cannot cross the boundary", key.Type())
			}
		}
		return encodeMixedMap(buf, mixed, depth)
	}
	if !rv.IsValid() {
		return append(buf, tagNull), nil
	}
	return nil, fmt.Errorf("nxb: Go value of type %s cannot cross the boundary", rv.Type())
}

func encodeArgs(args []any) ([]byte, error) {
	buf := appendU32(nil, uint32(len(args)))
	for _, arg := range args {
		var err error
		if buf, err = encodeValue(buf, arg, 0); err != nil {
			return nil, err
		}
	}
	return buf, nil
}

type decoder struct {
	data []byte
	pos  int
}

func (d *decoder) u8() (byte, error) {
	if d.pos >= len(d.data) {
		return 0, fmt.Errorf("nxb: truncated input at offset %d", d.pos)
	}
	b := d.data[d.pos]
	d.pos++
	return b, nil
}

func (d *decoder) u32() (uint32, error) {
	if d.pos+4 > len(d.data) {
		return 0, fmt.Errorf("nxb: truncated input at offset %d", d.pos)
	}
	v := binary.LittleEndian.Uint32(d.data[d.pos:])
	d.pos += 4
	return v, nil
}

func (d *decoder) u64() (uint64, error) {
	if d.pos+8 > len(d.data) {
		return 0, fmt.Errorf("nxb: truncated input at offset %d", d.pos)
	}
	v := binary.LittleEndian.Uint64(d.data[d.pos:])
	d.pos += 8
	return v, nil
}

func (d *decoder) blob() ([]byte, error) {
	n, err := d.u32()
	if err != nil {
		return nil, err
	}
	if d.pos+int(n) > len(d.data) {
		return nil, fmt.Errorf("nxb: truncated blob at offset %d", d.pos)
	}
	out := make([]byte, n)
	copy(out, d.data[d.pos:d.pos+int(n)])
	d.pos += int(n)
	return out, nil
}

func (d *decoder) value(depth int) (any, error) {
	if depth > maxDepth {
		return nil, fmt.Errorf("nxb: value nesting exceeds depth %d", maxDepth)
	}
	tag, err := d.u8()
	if err != nil {
		return nil, err
	}
	switch tag {
	case tagNull:
		return nil, nil
	case tagBool:
		b, err := d.u8()
		return b != 0, err
	case tagInt:
		v, err := d.u64()
		return int64(v), err
	case tagFloat:
		v, err := d.u64()
		return math.Float64frombits(v), err
	case tagString:
		b, err := d.blob()
		return string(b), err
	case tagBytes:
		return d.blob()
	case tagArray:
		count, err := d.u32()
		if err != nil {
			return nil, err
		}
		items := make([]any, 0, min(int(count), len(d.data)-d.pos))
		for i := uint32(0); i < count; i++ {
			item, err := d.value(depth + 1)
			if err != nil {
				return nil, err
			}
			items = append(items, item)
		}
		return items, nil
	case tagMap:
		return d.decodeMap(depth)
	case tagStruct:
		name, err := d.blob()
		if err != nil {
			return nil, err
		}
		count, err := d.u32()
		if err != nil {
			return nil, err
		}
		s := Struct{Name: string(name)}
		for i := uint32(0); i < count; i++ {
			nameTag, err := d.u8()
			if err != nil || nameTag != tagString {
				return nil, fmt.Errorf("nxb: struct field name must be a string")
			}
			field, err := d.blob()
			if err != nil {
				return nil, err
			}
			item, err := d.value(depth + 1)
			if err != nil {
				return nil, err
			}
			s.Fields = append(s.Fields, Field{Name: string(field), Value: item})
		}
		return s, nil
	}
	return nil, fmt.Errorf("nxb: unknown tag 0x%02x at offset %d", tag, d.pos-1)
}

// decodeMap devolve map[string]any quando toda chave e string (inclusive o
// mapa vazio), map[int64]any quando toda chave e int, map[any]any se
// misturado.
func (d *decoder) decodeMap(depth int) (any, error) {
	count, err := d.u32()
	if err != nil {
		return nil, err
	}
	keys := make([]any, 0, min(int(count), len(d.data)-d.pos))
	values := make([]any, 0, cap(keys))
	strKeys, intKeys := 0, 0
	for i := uint32(0); i < count; i++ {
		key, err := d.value(depth + 1)
		if err != nil {
			return nil, err
		}
		switch key.(type) {
		case string:
			strKeys++
		case int64:
			intKeys++
		default:
			return nil, fmt.Errorf("nxb: invalid map key")
		}
		item, err := d.value(depth + 1)
		if err != nil {
			return nil, err
		}
		keys = append(keys, key)
		values = append(values, item)
	}
	switch {
	case intKeys == 0:
		out := make(map[string]any, len(keys))
		for i, k := range keys {
			out[k.(string)] = values[i]
		}
		return out, nil
	case strKeys == 0:
		out := make(map[int64]any, len(keys))
		for i, k := range keys {
			out[k.(int64)] = values[i]
		}
		return out, nil
	default:
		out := make(map[any]any, len(keys))
		for i, k := range keys {
			out[k] = values[i]
		}
		return out, nil
	}
}

func decodeValue(data []byte) (any, error) {
	d := &decoder{data: data}
	v, err := d.value(0)
	if err != nil {
		return nil, err
	}
	if d.pos != len(data) {
		return nil, fmt.Errorf("nxb: %d trailing bytes after value", len(data)-d.pos)
	}
	return v, nil
}

func decodeArgs(data []byte) ([]any, error) {
	d := &decoder{data: data}
	count, err := d.u32()
	if err != nil {
		return nil, err
	}
	args := make([]any, 0, min(int(count), len(data)-d.pos))
	for i := uint32(0); i < count; i++ {
		arg, err := d.value(0)
		if err != nil {
			return nil, err
		}
		args = append(args, arg)
	}
	if d.pos != len(data) {
		return nil, fmt.Errorf("nxb: %d trailing bytes after arguments", len(data)-d.pos)
	}
	return args, nil
}

// decodeStringMap le os corpos de HELLO/ERROR/LOG (mapas de chave string).
func decodeStringMap(data []byte) (map[string]any, error) {
	v, err := decodeValue(data)
	if err != nil {
		return nil, err
	}
	m, ok := v.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("body is not a string-keyed map")
	}
	return m, nil
}

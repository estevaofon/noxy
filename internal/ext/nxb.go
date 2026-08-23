// Package ext implementa o mecanismo de extensoes WASM (spec
// docs/superpowers/specs/2026-08-23-wasm-extension-mechanism-design.md).
package ext

import (
	"encoding/binary"
	"fmt"
	"math"
	"sort"

	"noxy-vm/internal/value"
)

// Tags NXB v1 — append-only (spec §10): valores existentes nunca mudam.
const (
	nxbNull   = 0x00
	nxbBool   = 0x01
	nxbInt    = 0x02
	nxbFloat  = 0x03
	nxbString = 0x04
	nxbBytes  = 0x05
	nxbArray  = 0x06
	nxbMap    = 0x07
	nxbStruct = 0x08
)

const maxNXBDepth = 64

type Limits struct {
	MaxBytes int
}

func DefaultLimits() Limits { return Limits{MaxBytes: 64 << 20} }

type nxbEncoder struct {
	buf    []byte
	limits Limits
}

func (e *nxbEncoder) grow(n int) error {
	if len(e.buf)+n > e.limits.MaxBytes {
		return fmt.Errorf("nxb: encoded payload exceeds %d bytes", e.limits.MaxBytes)
	}
	return nil
}

func (e *nxbEncoder) writeU32(v uint32) {
	e.buf = binary.LittleEndian.AppendUint32(e.buf, v)
}

func (e *nxbEncoder) encode(v value.Value, depth int) error {
	if depth > maxNXBDepth {
		return fmt.Errorf("nxb: value nesting exceeds depth %d", maxNXBDepth)
	}
	if err := e.grow(9); err != nil {
		return err
	}
	switch v.Type {
	case value.VAL_NULL:
		e.buf = append(e.buf, nxbNull)
	case value.VAL_BOOL:
		b := byte(0)
		if v.Bool() {
			b = 1
		}
		e.buf = append(e.buf, nxbBool, b)
	case value.VAL_INT:
		e.buf = append(e.buf, nxbInt)
		e.buf = binary.LittleEndian.AppendUint64(e.buf, uint64(v.Int()))
	case value.VAL_FLOAT:
		e.buf = append(e.buf, nxbFloat)
		e.buf = binary.LittleEndian.AppendUint64(e.buf, math.Float64bits(v.Float()))
	case value.VAL_BYTES:
		return e.encodeBlob(nxbBytes, v.Obj.(string))
	case value.VAL_OBJ:
		switch obj := v.Obj.(type) {
		case string:
			return e.encodeBlob(nxbString, obj)
		case *value.ObjArray:
			e.buf = append(e.buf, nxbArray)
			e.writeU32(uint32(len(obj.Elements)))
			for _, element := range obj.Elements {
				if err := e.encode(element, depth+1); err != nil {
					return err
				}
			}
		case *value.ObjMap:
			return e.encodeMap(obj, depth)
		case *value.ObjInstance:
			return e.encodeStruct(obj, depth)
		default:
			return fmt.Errorf("nxb: value of kind %T cannot cross the extension boundary", obj)
		}
	case value.VAL_FUNCTION, value.VAL_NATIVE:
		return fmt.Errorf("nxb: callable values cannot cross the extension boundary")
	case value.VAL_CHANNEL:
		return fmt.Errorf("nxb: channel values cannot cross the extension boundary")
	case value.VAL_WAITGROUP:
		return fmt.Errorf("nxb: waitgroup values cannot cross the extension boundary")
	case value.VAL_REF:
		return fmt.Errorf("nxb: ref values cannot cross the extension boundary")
	case value.VAL_TASK:
		return fmt.Errorf("nxb: task values cannot cross the extension boundary")
	default:
		return fmt.Errorf("nxb: unsupported value type %d", v.Type)
	}
	return nil
}

func (e *nxbEncoder) encodeBlob(tag byte, data string) error {
	if err := e.grow(5 + len(data)); err != nil {
		return err
	}
	e.buf = append(e.buf, tag)
	e.writeU32(uint32(len(data)))
	e.buf = append(e.buf, data...)
	return nil
}

// encodeMap ordena as chaves (ints antes de strings, cada grupo em ordem)
// para que a codificacao seja deterministica (spec §2, "deterministic").
func (e *nxbEncoder) encodeMap(obj *value.ObjMap, depth int) error {
	snapshot := obj.Snapshot()
	intKeys := make([]int64, 0, len(snapshot))
	strKeys := make([]string, 0, len(snapshot))
	for key := range snapshot {
		switch k := key.(type) {
		case int64:
			intKeys = append(intKeys, k)
		case string:
			strKeys = append(strKeys, k)
		default:
			return fmt.Errorf("nxb: map key of type %T cannot cross the extension boundary", key)
		}
	}
	sort.Slice(intKeys, func(i, j int) bool { return intKeys[i] < intKeys[j] })
	sort.Strings(strKeys)
	e.buf = append(e.buf, nxbMap)
	e.writeU32(uint32(len(snapshot)))
	for _, k := range intKeys {
		if err := e.encode(value.NewInt(k), depth+1); err != nil {
			return err
		}
		if err := e.encode(snapshot[k], depth+1); err != nil {
			return err
		}
	}
	for _, k := range strKeys {
		if err := e.encode(value.NewString(k), depth+1); err != nil {
			return err
		}
		if err := e.encode(snapshot[k], depth+1); err != nil {
			return err
		}
	}
	return nil
}

func (e *nxbEncoder) encodeStruct(obj *value.ObjInstance, depth int) error {
	e.buf = append(e.buf, nxbStruct)
	name := obj.Struct.Name
	if err := e.grow(5 + len(name)); err != nil {
		return err
	}
	e.writeU32(uint32(len(name)))
	e.buf = append(e.buf, name...)
	e.writeU32(uint32(len(obj.Struct.Fields)))
	for _, field := range obj.Struct.Fields {
		if err := e.encodeBlob(nxbString, field); err != nil {
			return err
		}
		if err := e.encode(obj.Fields[field], depth+1); err != nil {
			return err
		}
	}
	return nil
}

func EncodeValue(v value.Value, limits Limits) ([]byte, error) {
	e := &nxbEncoder{limits: limits}
	if err := e.encode(v, 0); err != nil {
		return nil, err
	}
	return e.buf, nil
}

func EncodeArgs(args []value.Value, limits Limits) ([]byte, error) {
	e := &nxbEncoder{limits: limits}
	e.writeU32(uint32(len(args)))
	for _, arg := range args {
		if err := e.encode(arg, 0); err != nil {
			return nil, err
		}
	}
	return e.buf, nil
}

type nxbDecoder struct {
	data []byte
	pos  int
}

func (d *nxbDecoder) readByte() (byte, error) {
	if d.pos >= len(d.data) {
		return 0, fmt.Errorf("nxb: truncated input at offset %d", d.pos)
	}
	b := d.data[d.pos]
	d.pos++
	return b, nil
}

func (d *nxbDecoder) readU32() (uint32, error) {
	if d.pos+4 > len(d.data) {
		return 0, fmt.Errorf("nxb: truncated input at offset %d", d.pos)
	}
	v := binary.LittleEndian.Uint32(d.data[d.pos:])
	d.pos += 4
	return v, nil
}

func (d *nxbDecoder) readU64() (uint64, error) {
	if d.pos+8 > len(d.data) {
		return 0, fmt.Errorf("nxb: truncated input at offset %d", d.pos)
	}
	v := binary.LittleEndian.Uint64(d.data[d.pos:])
	d.pos += 8
	return v, nil
}

func (d *nxbDecoder) readBlob() (string, error) {
	n, err := d.readU32()
	if err != nil {
		return "", err
	}
	if d.pos+int(n) > len(d.data) {
		return "", fmt.Errorf("nxb: truncated blob at offset %d", d.pos)
	}
	s := string(d.data[d.pos : d.pos+int(n)])
	d.pos += int(n)
	return s, nil
}

func (d *nxbDecoder) decode(depth int) (value.Value, error) {
	if depth > maxNXBDepth {
		return value.NewNull(), fmt.Errorf("nxb: value nesting exceeds depth %d", maxNXBDepth)
	}
	tag, err := d.readByte()
	if err != nil {
		return value.NewNull(), err
	}
	switch tag {
	case nxbNull:
		return value.NewNull(), nil
	case nxbBool:
		b, err := d.readByte()
		if err != nil {
			return value.NewNull(), err
		}
		return value.NewBool(b != 0), nil
	case nxbInt:
		v, err := d.readU64()
		if err != nil {
			return value.NewNull(), err
		}
		return value.NewInt(int64(v)), nil
	case nxbFloat:
		v, err := d.readU64()
		if err != nil {
			return value.NewNull(), err
		}
		return value.NewFloat(math.Float64frombits(v)), nil
	case nxbString:
		s, err := d.readBlob()
		if err != nil {
			return value.NewNull(), err
		}
		return value.NewString(s), nil
	case nxbBytes:
		s, err := d.readBlob()
		if err != nil {
			return value.NewNull(), err
		}
		return value.NewBytes(s), nil
	case nxbArray:
		count, err := d.readU32()
		if err != nil {
			return value.NewNull(), err
		}
		elements := make([]value.Value, 0, count)
		for i := uint32(0); i < count; i++ {
			element, err := d.decode(depth + 1)
			if err != nil {
				return value.NewNull(), err
			}
			elements = append(elements, element)
		}
		// RC: o array e dono duravel de cada elemento (construtor retem),
		// espelhando json_population.go.
		return value.NewArray(elements), nil
	case nxbMap:
		return d.decodeMap(depth)
	case nxbStruct:
		// Struct volta como map com forma de struct (spec §3): o nome e
		// descartado, a validacao a jusante e estrutural.
		if _, err := d.readBlob(); err != nil {
			return value.NewNull(), err
		}
		return d.decodeFields(depth)
	default:
		return value.NewNull(), fmt.Errorf("nxb: unknown tag 0x%02x at offset %d", tag, d.pos-1)
	}
}

func (d *nxbDecoder) decodeMap(depth int) (value.Value, error) {
	count, err := d.readU32()
	if err != nil {
		return value.NewNull(), err
	}
	result := value.NewMap()
	mapping := result.Obj.(*value.ObjMap)
	for i := uint32(0); i < count; i++ {
		key, err := d.decode(depth + 1)
		if err != nil {
			return value.NewNull(), err
		}
		item, err := d.decode(depth + 1)
		if err != nil {
			return value.NewNull(), err
		}
		value.Retain(item) // RC: o map e dono duravel de cada valor
		switch key.Type {
		case value.VAL_INT:
			mapping.Set(key.Int(), item)
		case value.VAL_OBJ:
			s, ok := key.Obj.(string)
			if !ok {
				return value.NewNull(), fmt.Errorf("nxb: invalid map key")
			}
			mapping.Set(s, item)
		default:
			return value.NewNull(), fmt.Errorf("nxb: invalid map key tag")
		}
	}
	return result, nil
}

func (d *nxbDecoder) decodeFields(depth int) (value.Value, error) {
	count, err := d.readU32()
	if err != nil {
		return value.NewNull(), err
	}
	result := value.NewMap()
	mapping := result.Obj.(*value.ObjMap)
	for i := uint32(0); i < count; i++ {
		nameTag, err := d.readByte()
		if err != nil || nameTag != nxbString {
			return value.NewNull(), fmt.Errorf("nxb: struct field name must be a string")
		}
		name, err := d.readBlob()
		if err != nil {
			return value.NewNull(), err
		}
		item, err := d.decode(depth + 1)
		if err != nil {
			return value.NewNull(), err
		}
		value.Retain(item) // RC: o map e dono duravel de cada valor
		mapping.Set(name, item)
	}
	return result, nil
}

func DecodeValue(data []byte, limits Limits) (value.Value, error) {
	if len(data) > limits.MaxBytes {
		return value.NewNull(), fmt.Errorf("nxb: payload exceeds %d bytes", limits.MaxBytes)
	}
	d := &nxbDecoder{data: data}
	v, err := d.decode(0)
	if err != nil {
		return value.NewNull(), err
	}
	if d.pos != len(data) {
		return value.NewNull(), fmt.Errorf("nxb: %d trailing bytes after value", len(data)-d.pos)
	}
	return v, nil
}

func DecodeArgs(data []byte, limits Limits) ([]value.Value, error) {
	if len(data) > limits.MaxBytes {
		return nil, fmt.Errorf("nxb: payload exceeds %d bytes", limits.MaxBytes)
	}
	d := &nxbDecoder{data: data}
	count, err := d.readU32()
	if err != nil {
		return nil, err
	}
	args := make([]value.Value, 0, count)
	for i := uint32(0); i < count; i++ {
		arg, err := d.decode(0)
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

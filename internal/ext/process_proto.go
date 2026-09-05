package ext

import (
	"fmt"

	"github.com/estevaofon/noxy/internal/value"
)

// Corpos dos quadros fora de chamada (HELLO, ERROR, LOG — spec §2.4, §2.6)
// sao mapas NXB com chaves string. Chaves desconhecidas sao ignoradas: e o
// unico canal aditivo da v1 (spec §2.8).

func encodeStringMap(fields map[string]value.Value, limits Limits) ([]byte, error) {
	m := value.NewMap()
	obj := m.Obj.(*value.ObjMap)
	for key, item := range fields {
		obj.Set(key, item)
	}
	return EncodeValue(m, limits)
}

// helloBody e o HELLO do host: versao do protocolo, versao do noxy, nome da
// extensao e os exports na ordem do manifesto (= fn index).
func helloBody(noxyVersion, extName string, exports []string, limits Limits) ([]byte, error) {
	names := make([]value.Value, len(exports))
	for i, name := range exports {
		names[i] = value.NewString(name)
	}
	return encodeStringMap(map[string]value.Value{
		"protocol":  value.NewString(ProtocolVersion),
		"noxy":      value.NewString(noxyVersion),
		"extension": value.NewString(extName),
		"exports":   value.NewArray(names),
	}, limits)
}

func decodeBodyMap(body []byte, limits Limits) (*value.ObjMap, error) {
	v, err := DecodeValue(body, limits)
	if err != nil {
		return nil, err
	}
	m, ok := v.Obj.(*value.ObjMap)
	if v.Type != value.VAL_OBJ || !ok {
		return nil, fmt.Errorf("body is not a map")
	}
	return m, nil
}

func mapString(m *value.ObjMap, key string) (string, bool) {
	v, ok := m.Get(key)
	if !ok || v.Type != value.VAL_OBJ {
		return "", false
	}
	s, ok := v.Obj.(string)
	return s, ok
}

func mapInt(m *value.ObjMap, key string) (int64, bool) {
	v, ok := m.Get(key)
	if !ok || v.Type != value.VAL_INT {
		return 0, false
	}
	return v.Int(), true
}

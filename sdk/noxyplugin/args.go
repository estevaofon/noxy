// sdk/noxyplugin/args.go
package noxyplugin

import "fmt"

// Args sao os argumentos de uma chamada, ja decodificados (§9.3): int64,
// float64, bool, string, []byte, []any, map[string]any / map[int64]any /
// map[any]any, Struct, nil.
type Args []any

func (a Args) count(want int) error {
	if len(a) != want {
		return fmt.Errorf("expected %d arguments, got %d", want, len(a))
	}
	return nil
}

func (a Args) at(i int) (any, error) {
	if i < 0 || i >= len(a) {
		return nil, fmt.Errorf("argument %d: missing", i+1)
	}
	return a[i], nil
}

func argTypeError(i int, want string, got any) error {
	return fmt.Errorf("argument %d: expected %s, got %s", i+1, want, typeName(got))
}

// typeName nomeia o valor no vocabulario da Noxy, para mensagens.
func typeName(v any) string {
	switch v.(type) {
	case nil:
		return "null"
	case bool:
		return "bool"
	case int64:
		return "int"
	case float64:
		return "float"
	case string:
		return "string"
	case []byte:
		return "bytes"
	case []any:
		return "array"
	case map[string]any, map[int64]any, map[any]any:
		return "map"
	case Struct:
		return "struct"
	}
	return fmt.Sprintf("%T", v)
}

func (a Args) Int(i int) (int64, error) {
	v, err := a.at(i)
	if err != nil {
		return 0, err
	}
	n, ok := v.(int64)
	if !ok {
		return 0, argTypeError(i, "int", v)
	}
	return n, nil
}

func (a Args) Float(i int) (float64, error) {
	v, err := a.at(i)
	if err != nil {
		return 0, err
	}
	f, ok := v.(float64)
	if !ok {
		return 0, argTypeError(i, "float", v)
	}
	return f, nil
}

func (a Args) Bool(i int) (bool, error) {
	v, err := a.at(i)
	if err != nil {
		return false, err
	}
	b, ok := v.(bool)
	if !ok {
		return false, argTypeError(i, "bool", v)
	}
	return b, nil
}

func (a Args) String(i int) (string, error) {
	v, err := a.at(i)
	if err != nil {
		return "", err
	}
	s, ok := v.(string)
	if !ok {
		return "", argTypeError(i, "string", v)
	}
	return s, nil
}

func (a Args) Bytes(i int) ([]byte, error) {
	v, err := a.at(i)
	if err != nil {
		return nil, err
	}
	b, ok := v.([]byte)
	if !ok {
		return nil, argTypeError(i, "bytes", v)
	}
	return b, nil
}

func (a Args) Array(i int) ([]any, error) {
	v, err := a.at(i)
	if err != nil {
		return nil, err
	}
	items, ok := v.([]any)
	if !ok {
		return nil, argTypeError(i, "array", v)
	}
	return items, nil
}

func (a Args) Map(i int) (map[string]any, error) {
	v, err := a.at(i)
	if err != nil {
		return nil, err
	}
	m, ok := v.(map[string]any)
	if !ok {
		return nil, argTypeError(i, "map", v)
	}
	return m, nil
}

func (a Args) IntMap(i int) (map[int64]any, error) {
	v, err := a.at(i)
	if err != nil {
		return nil, err
	}
	m, ok := v.(map[int64]any)
	if !ok {
		return nil, argTypeError(i, "map", v)
	}
	return m, nil
}

func (a Args) Struct(i int) (Struct, error) {
	v, err := a.at(i)
	if err != nil {
		return Struct{}, err
	}
	s, ok := v.(Struct)
	if !ok {
		return Struct{}, argTypeError(i, "struct", v)
	}
	return s, nil
}

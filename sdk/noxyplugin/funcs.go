// sdk/noxyplugin/funcs.go
package noxyplugin

import (
	"context"
	"fmt"
	"reflect"
)

// Func0..Func5 adaptam funcoes Go tipadas a Handler: conferem a aridade e
// convertem cada argumento (§9.2) — a checagem do lado do plugin, gemea do
// checkDeclaredReturn do host.

func Func0[R any](f func(context.Context) (R, error)) Handler {
	return func(ctx context.Context, args Args) (any, error) {
		if err := args.count(0); err != nil {
			return nil, err
		}
		return f(ctx)
	}
}

func Func1[A, R any](f func(context.Context, A) (R, error)) Handler {
	return func(ctx context.Context, args Args) (any, error) {
		if err := args.count(1); err != nil {
			return nil, err
		}
		a, err := arg[A](args, 0)
		if err != nil {
			return nil, err
		}
		return f(ctx, a)
	}
}

func Func2[A, B, R any](f func(context.Context, A, B) (R, error)) Handler {
	return func(ctx context.Context, args Args) (any, error) {
		if err := args.count(2); err != nil {
			return nil, err
		}
		a, err := arg[A](args, 0)
		if err != nil {
			return nil, err
		}
		b, err := arg[B](args, 1)
		if err != nil {
			return nil, err
		}
		return f(ctx, a, b)
	}
}

func Func3[A, B, C, R any](f func(context.Context, A, B, C) (R, error)) Handler {
	return func(ctx context.Context, args Args) (any, error) {
		if err := args.count(3); err != nil {
			return nil, err
		}
		a, err := arg[A](args, 0)
		if err != nil {
			return nil, err
		}
		b, err := arg[B](args, 1)
		if err != nil {
			return nil, err
		}
		c, err := arg[C](args, 2)
		if err != nil {
			return nil, err
		}
		return f(ctx, a, b, c)
	}
}

func Func4[A, B, C, D, R any](f func(context.Context, A, B, C, D) (R, error)) Handler {
	return func(ctx context.Context, args Args) (any, error) {
		if err := args.count(4); err != nil {
			return nil, err
		}
		a, err := arg[A](args, 0)
		if err != nil {
			return nil, err
		}
		b, err := arg[B](args, 1)
		if err != nil {
			return nil, err
		}
		c, err := arg[C](args, 2)
		if err != nil {
			return nil, err
		}
		d, err := arg[D](args, 3)
		if err != nil {
			return nil, err
		}
		return f(ctx, a, b, c, d)
	}
}

func Func5[A, B, C, D, E, R any](f func(context.Context, A, B, C, D, E) (R, error)) Handler {
	return func(ctx context.Context, args Args) (any, error) {
		if err := args.count(5); err != nil {
			return nil, err
		}
		a, err := arg[A](args, 0)
		if err != nil {
			return nil, err
		}
		b, err := arg[B](args, 1)
		if err != nil {
			return nil, err
		}
		c, err := arg[C](args, 2)
		if err != nil {
			return nil, err
		}
		d, err := arg[D](args, 3)
		if err != nil {
			return nil, err
		}
		e, err := arg[E](args, 4)
		if err != nil {
			return nil, err
		}
		return f(ctx, a, b, c, d, e)
	}
}

func arg[T any](args Args, i int) (T, error) {
	var zero T
	converted, err := coerce(args[i], reflect.TypeOf(&zero).Elem())
	if err != nil {
		return zero, fmt.Errorf("argument %d: %w", i+1, err)
	}
	if converted == nil {
		return zero, nil
	}
	return converted.(T), nil
}

var structType = reflect.TypeOf(Struct{})

// noxyName nomeia um tipo Go de parametro no vocabulario da Noxy.
func noxyName(t reflect.Type) string {
	switch t.Kind() {
	case reflect.Bool:
		return "bool"
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return "int"
	case reflect.Float32, reflect.Float64:
		return "float"
	case reflect.String:
		return "string"
	case reflect.Slice:
		if t.Elem().Kind() == reflect.Uint8 {
			return "bytes"
		}
		return "array"
	case reflect.Map:
		return "map"
	case reflect.Struct:
		if t == structType {
			return "struct"
		}
	}
	return t.String()
}

// coerce converte um valor decodificado para o tipo Go do parametro.
func coerce(v any, t reflect.Type) (any, error) {
	if t.Kind() == reflect.Interface {
		return v, nil
	}
	if v == nil {
		switch t.Kind() {
		case reflect.Slice, reflect.Map, reflect.Ptr:
			return nil, nil
		}
		return nil, fmt.Errorf("expected %s, got null", noxyName(t))
	}
	mismatch := func() error { return fmt.Errorf("expected %s, got %s", noxyName(t), typeName(v)) }
	rv := reflect.New(t).Elem()
	switch t.Kind() {
	case reflect.Bool:
		b, ok := v.(bool)
		if !ok {
			return nil, mismatch()
		}
		rv.SetBool(b)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		n, ok := v.(int64)
		if !ok {
			return nil, mismatch()
		}
		if rv.OverflowInt(n) {
			return nil, fmt.Errorf("int %d overflows %s", n, t)
		}
		rv.SetInt(n)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		n, ok := v.(int64)
		if !ok {
			return nil, mismatch()
		}
		if n < 0 || rv.OverflowUint(uint64(n)) {
			return nil, fmt.Errorf("int %d overflows %s", n, t)
		}
		rv.SetUint(uint64(n))
	case reflect.Float32, reflect.Float64:
		f, ok := v.(float64)
		if !ok {
			return nil, mismatch()
		}
		rv.SetFloat(f)
	case reflect.String:
		s, ok := v.(string)
		if !ok {
			return nil, mismatch()
		}
		rv.SetString(s)
	case reflect.Slice:
		if t.Elem().Kind() == reflect.Uint8 {
			b, ok := v.([]byte)
			if !ok {
				return nil, mismatch()
			}
			rv.SetBytes(b)
			break
		}
		items, ok := v.([]any)
		if !ok {
			return nil, mismatch()
		}
		out := reflect.MakeSlice(t, len(items), len(items))
		for i, item := range items {
			c, err := coerce(item, t.Elem())
			if err != nil {
				return nil, fmt.Errorf("element %d: %w", i, err)
			}
			if c != nil {
				out.Index(i).Set(reflect.ValueOf(c))
			}
		}
		rv.Set(out)
	case reflect.Map:
		out := reflect.MakeMap(t)
		set := func(key any, item any) error {
			k, err := coerce(key, t.Key())
			if err != nil {
				return fmt.Errorf("key %v: %w", key, err)
			}
			c, err := coerce(item, t.Elem())
			if err != nil {
				return fmt.Errorf("key %v: %w", key, err)
			}
			if c == nil {
				out.SetMapIndex(reflect.ValueOf(k), reflect.Zero(t.Elem()))
				return nil
			}
			out.SetMapIndex(reflect.ValueOf(k), reflect.ValueOf(c))
			return nil
		}
		switch src := v.(type) {
		case map[string]any:
			for k, item := range src {
				if err := set(k, item); err != nil {
					return nil, err
				}
			}
		case map[int64]any:
			for k, item := range src {
				if err := set(k, item); err != nil {
					return nil, err
				}
			}
		case map[any]any:
			for k, item := range src {
				if err := set(k, item); err != nil {
					return nil, err
				}
			}
		default:
			return nil, mismatch()
		}
		rv.Set(out)
	case reflect.Struct:
		if t != structType {
			return nil, fmt.Errorf("unsupported parameter type %s", t)
		}
		s, ok := v.(Struct)
		if !ok {
			return nil, mismatch()
		}
		rv.Set(reflect.ValueOf(s))
	default:
		return nil, fmt.Errorf("unsupported parameter type %s", t)
	}
	return rv.Interface(), nil
}

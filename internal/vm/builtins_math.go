package vm

import (
	"fmt"
	"math"

	"noxy-vm/internal/value"
)

// Issue #126 item 1: modulo `math` da stdlib — wrappers finos sobre o math
// do Go, expostos pelo wrapper tipado internal/stdlib/math.nx (float ->
// float). Dominio invalido e ERRO tipado, nao NaN: o proprio Noxy faz
// `1.0 / 0.0` ser erro de runtime (OP_DIV_FLOAT), a #121 decidiu que
// argumento invalido nunca vira sentinela, e Python `math` lanca
// `ValueError: math domain error`. Overflow para ±Inf (exp(1000.0)) nao e
// checado, como o overflow de int (spec §8).

func mathArity(native string, args []value.Value, want int) error {
	if len(args) == want {
		return nil
	}
	plural := "s"
	if want == 1 {
		plural = ""
	}
	return fmt.Errorf("%s: expects exactly %d argument%s, got %d", native, want, plural, len(args))
}

func mathFloatArgument(native, label string, arg value.Value) (float64, error) {
	if arg.Type != value.VAL_FLOAT {
		return 0, fmt.Errorf("%s: %s must be a float, got %s", native, label, runtimeTypeName(arg))
	}
	return arg.Float(), nil
}

func mathDomainError(native, condition, got string) error {
	return fmt.Errorf("%s: domain error (%s), got %s", native, condition, got)
}

func (vm *VM) defineMathBuiltins() {
	unary := func(name string, fn func(float64) float64, check func(float64) error) {
		native := "math." + name
		vm.DefineContextualNative("math_"+name, func(_ value.NativeContext, args []value.Value) (value.Value, error) {
			if err := mathArity(native, args, 1); err != nil {
				return value.NewNull(), err
			}
			x, err := mathFloatArgument(native, "x", args[0])
			if err != nil {
				return value.NewNull(), err
			}
			if check != nil {
				if err := check(x); err != nil {
					return value.NewNull(), err
				}
			}
			return value.NewFloat(fn(x)), nil
		})
	}
	binary := func(name, aLabel, bLabel string, fn func(float64, float64) float64, check func(float64, float64) error) {
		native := "math." + name
		vm.DefineContextualNative("math_"+name, func(_ value.NativeContext, args []value.Value) (value.Value, error) {
			if err := mathArity(native, args, 2); err != nil {
				return value.NewNull(), err
			}
			a, err := mathFloatArgument(native, aLabel, args[0])
			if err != nil {
				return value.NewNull(), err
			}
			b, err := mathFloatArgument(native, bLabel, args[1])
			if err != nil {
				return value.NewNull(), err
			}
			if check != nil {
				if err := check(a, b); err != nil {
					return value.NewNull(), err
				}
			}
			return value.NewFloat(fn(a, b)), nil
		})
	}
	nonNegative := func(native string) func(float64) error {
		return func(x float64) error {
			if x < 0 {
				return mathDomainError(native, "x < 0", fmt.Sprintf("x=%g", x))
			}
			return nil
		}
	}
	positive := func(native string) func(float64) error {
		return func(x float64) error {
			if x <= 0 {
				return mathDomainError(native, "x <= 0", fmt.Sprintf("x=%g", x))
			}
			return nil
		}
	}
	unitInterval := func(native string) func(float64) error {
		return func(x float64) error {
			if x < -1 || x > 1 {
				return mathDomainError(native, "x outside [-1, 1]", fmt.Sprintf("x=%g", x))
			}
			return nil
		}
	}

	unary("sqrt", math.Sqrt, nonNegative("math.sqrt"))
	unary("cbrt", math.Cbrt, nil)
	unary("abs", math.Abs, nil)
	unary("floor", math.Floor, nil)
	unary("ceil", math.Ceil, nil)
	unary("round", math.Round, nil) // metade afasta de zero, como Go (nao banker's)
	unary("trunc", math.Trunc, nil)
	unary("sin", math.Sin, nil)
	unary("cos", math.Cos, nil)
	unary("tan", math.Tan, nil)
	unary("asin", math.Asin, unitInterval("math.asin"))
	unary("acos", math.Acos, unitInterval("math.acos"))
	unary("atan", math.Atan, nil)
	unary("exp", math.Exp, nil)
	unary("log", math.Log, positive("math.log"))
	unary("log2", math.Log2, positive("math.log2"))
	unary("log10", math.Log10, positive("math.log10"))

	binary("pow", "x", "y", math.Pow, func(x, y float64) error {
		if x == 0 && y < 0 {
			return mathDomainError("math.pow", "x == 0 and y < 0", fmt.Sprintf("x=%g, y=%g", x, y))
		}
		if x < 0 && y != math.Trunc(y) {
			return mathDomainError("math.pow", "x < 0 and y not an integer", fmt.Sprintf("x=%g, y=%g", x, y))
		}
		return nil
	})
	binary("fmod", "x", "y", math.Mod, func(x, y float64) error {
		if y == 0 {
			return mathDomainError("math.fmod", "y == 0", fmt.Sprintf("x=%g, y=%g", x, y))
		}
		return nil
	})
	binary("atan2", "y", "x", math.Atan2, nil)
	binary("hypot", "x", "y", math.Hypot, nil)
	binary("min", "a", "b", math.Min, nil)
	binary("max", "a", "b", math.Max, nil)

	vm.DefineContextualNative("math_clamp", func(_ value.NativeContext, args []value.Value) (value.Value, error) {
		const native = "math.clamp"
		if err := mathArity(native, args, 3); err != nil {
			return value.NewNull(), err
		}
		x, err := mathFloatArgument(native, "x", args[0])
		if err != nil {
			return value.NewNull(), err
		}
		lo, err := mathFloatArgument(native, "lo", args[1])
		if err != nil {
			return value.NewNull(), err
		}
		hi, err := mathFloatArgument(native, "hi", args[2])
		if err != nil {
			return value.NewNull(), err
		}
		if lo > hi {
			return value.NewNull(), mathDomainError(native, "lo > hi", fmt.Sprintf("lo=%g, hi=%g", lo, hi))
		}
		return value.NewFloat(math.Min(math.Max(x, lo), hi)), nil
	})

	// clamp_int e o unico `_int` com native: os demais (abs_int, min_int,
	// max_int) sao Noxy puro no wrapper, mas codigo Noxy nao tem como
	// levantar erro de runtime, e `lo > hi` tem de errar como no clamp float.
	vm.DefineContextualNative("math_clamp_int", func(_ value.NativeContext, args []value.Value) (value.Value, error) {
		const native = "math.clamp_int"
		if err := mathArity(native, args, 3); err != nil {
			return value.NewNull(), err
		}
		for i, label := range []string{"x", "lo", "hi"} {
			if args[i].Type != value.VAL_INT {
				return value.NewNull(), fmt.Errorf("%s: %s must be an int, got %s", native, label, runtimeTypeName(args[i]))
			}
		}
		x, lo, hi := args[0].Int(), args[1].Int(), args[2].Int()
		if lo > hi {
			return value.NewNull(), mathDomainError(native, "lo > hi", fmt.Sprintf("lo=%d, hi=%d", lo, hi))
		}
		if x < lo {
			return value.NewInt(lo), nil
		}
		if x > hi {
			return value.NewInt(hi), nil
		}
		return value.NewInt(x), nil
	})
}

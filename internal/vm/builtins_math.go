package vm

import (
	"fmt"
	"math"

	"github.com/estevaofon/noxy/internal/value"
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

// mathUnaryNative e mathBinaryNative CONSTROEM a closure da nativa sem
// registra-la: quem registra e sempre `vm.DefineContextualNative("math_x",
// ...)` com o nome literal no site de chamada, porque
// TestStdlibWrappersCallOnlyRegisteredNatives (stdlib_hygiene_test.go)
// so enxerga natives cujo primeiro argumento e um BasicLit — um nome
// concatenado em runtime (`"math_"+name`) fica invisivel ao scanner
// estatico e faz o teste falhar achando que math.nx chama uma nativa nao
// registrada.
func mathUnaryNative(name string, fn func(float64) float64, check func(float64) error) value.ContextualNativeFunc {
	native := "math." + name
	return func(_ value.NativeContext, args []value.Value) (value.Value, error) {
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
	}
}

func mathBinaryNative(name, aLabel, bLabel string, fn func(float64, float64) float64, check func(float64, float64) error) value.ContextualNativeFunc {
	native := "math." + name
	return func(_ value.NativeContext, args []value.Value) (value.Value, error) {
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
	}
}

func (vm *VM) defineMathBuiltins() {
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

	vm.DefineContextualNative("math_sqrt", mathUnaryNative("sqrt", math.Sqrt, nonNegative("math.sqrt")))
	vm.DefineContextualNative("math_cbrt", mathUnaryNative("cbrt", math.Cbrt, nil))
	vm.DefineContextualNative("math_abs", mathUnaryNative("abs", math.Abs, nil))
	vm.DefineContextualNative("math_floor", mathUnaryNative("floor", math.Floor, nil))
	vm.DefineContextualNative("math_ceil", mathUnaryNative("ceil", math.Ceil, nil))
	vm.DefineContextualNative("math_round", mathUnaryNative("round", math.Round, nil)) // metade afasta de zero, como Go (nao banker's)
	vm.DefineContextualNative("math_trunc", mathUnaryNative("trunc", math.Trunc, nil))
	vm.DefineContextualNative("math_sin", mathUnaryNative("sin", math.Sin, nil))
	vm.DefineContextualNative("math_cos", mathUnaryNative("cos", math.Cos, nil))
	vm.DefineContextualNative("math_tan", mathUnaryNative("tan", math.Tan, nil))
	vm.DefineContextualNative("math_asin", mathUnaryNative("asin", math.Asin, unitInterval("math.asin")))
	vm.DefineContextualNative("math_acos", mathUnaryNative("acos", math.Acos, unitInterval("math.acos")))
	vm.DefineContextualNative("math_atan", mathUnaryNative("atan", math.Atan, nil))
	vm.DefineContextualNative("math_exp", mathUnaryNative("exp", math.Exp, nil))
	vm.DefineContextualNative("math_log", mathUnaryNative("log", math.Log, positive("math.log")))
	vm.DefineContextualNative("math_log2", mathUnaryNative("log2", math.Log2, positive("math.log2")))
	vm.DefineContextualNative("math_log10", mathUnaryNative("log10", math.Log10, positive("math.log10")))

	vm.DefineContextualNative("math_pow", mathBinaryNative("pow", "x", "y", math.Pow, func(x, y float64) error {
		if x == 0 && y < 0 {
			return mathDomainError("math.pow", "x == 0 and y < 0", fmt.Sprintf("x=%g, y=%g", x, y))
		}
		if x < 0 && y != math.Trunc(y) {
			return mathDomainError("math.pow", "x < 0 and y not an integer", fmt.Sprintf("x=%g, y=%g", x, y))
		}
		return nil
	}))
	vm.DefineContextualNative("math_fmod", mathBinaryNative("fmod", "x", "y", math.Mod, func(x, y float64) error {
		if y == 0 {
			return mathDomainError("math.fmod", "y == 0", fmt.Sprintf("x=%g, y=%g", x, y))
		}
		return nil
	}))
	vm.DefineContextualNative("math_atan2", mathBinaryNative("atan2", "y", "x", math.Atan2, nil))
	vm.DefineContextualNative("math_hypot", mathBinaryNative("hypot", "x", "y", math.Hypot, nil))
	vm.DefineContextualNative("math_min", mathBinaryNative("min", "a", "b", math.Min, nil))
	vm.DefineContextualNative("math_max", mathBinaryNative("max", "a", "b", math.Max, nil))

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

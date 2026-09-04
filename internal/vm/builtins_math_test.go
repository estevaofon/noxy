package vm

import (
	"math"
	"strings"
	"testing"

	"noxy-vm/internal/value"
)

// Issue #126 item 1: wrappers finos sobre o math do Go. Dominio invalido e
// erro tipado (como `1.0 / 0.0` e a #121), nunca NaN; overflow para Inf nao
// e checado (como o overflow de int, spec §8).
func TestMathBuiltinsScalarTables(t *testing.T) {
	machine := New()
	f := value.NewFloat
	tests := []struct {
		name    string
		builtin string
		args    []value.Value
		want    float64
	}{
		{"sqrt", "math_sqrt", []value.Value{f(2)}, math.Sqrt2},
		{"sqrt zero", "math_sqrt", []value.Value{f(0)}, 0},
		{"cbrt", "math_cbrt", []value.Value{f(-27)}, -3},
		{"abs", "math_abs", []value.Value{f(-2.5)}, 2.5},
		{"floor", "math_floor", []value.Value{f(-2.5)}, -3},
		{"ceil", "math_ceil", []value.Value{f(-2.5)}, -2},
		{"round half away from zero", "math_round", []value.Value{f(2.5)}, 3},
		{"round negative half away from zero", "math_round", []value.Value{f(-2.5)}, -3},
		{"trunc", "math_trunc", []value.Value{f(-2.7)}, -2},
		{"sin", "math_sin", []value.Value{f(math.Pi / 2)}, 1},
		{"cos", "math_cos", []value.Value{f(0)}, 1},
		{"tan", "math_tan", []value.Value{f(0)}, 0},
		{"asin", "math_asin", []value.Value{f(1)}, math.Pi / 2},
		{"acos", "math_acos", []value.Value{f(1)}, 0},
		{"atan", "math_atan", []value.Value{f(1)}, math.Pi / 4},
		{"atan2", "math_atan2", []value.Value{f(1), f(1)}, math.Pi / 4},
		{"atan2 quadrant", "math_atan2", []value.Value{f(1), f(-1)}, 3 * math.Pi / 4},
		{"hypot", "math_hypot", []value.Value{f(3), f(4)}, 5},
		{"exp", "math_exp", []value.Value{f(1)}, math.E},
		{"log", "math_log", []value.Value{f(math.E)}, 1},
		{"log2", "math_log2", []value.Value{f(8)}, 3},
		{"log10", "math_log10", []value.Value{f(1000)}, 3},
		{"pow", "math_pow", []value.Value{f(2), f(10)}, 1024},
		{"pow negative base integer exponent", "math_pow", []value.Value{f(-2), f(3)}, -8},
		{"pow overflow is Inf", "math_pow", []value.Value{f(10), f(400)}, math.Inf(1)},
		{"fmod keeps sign of x", "math_fmod", []value.Value{f(-7), f(3)}, -1},
		{"min", "math_min", []value.Value{f(1), f(-1)}, -1},
		{"max", "math_max", []value.Value{f(1), f(-1)}, 1},
		{"clamp below", "math_clamp", []value.Value{f(-5), f(0), f(10)}, 0},
		{"clamp inside", "math_clamp", []value.Value{f(5), f(0), f(10)}, 5},
		{"clamp above", "math_clamp", []value.Value{f(50), f(0), f(10)}, 10},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := callBuiltin(t, machine, tc.builtin, tc.args...)
			if got.Type != value.VAL_FLOAT {
				t.Fatalf("%s returned %s, want float", tc.builtin, runtimeTypeName(got))
			}
			if math.IsInf(tc.want, 0) {
				if !math.IsInf(got.Float(), 1) {
					t.Fatalf("%s = %v, want +Inf", tc.builtin, got.Float())
				}
				return
			}
			if math.Abs(got.Float()-tc.want) > 1e-12 {
				t.Fatalf("%s = %v, want %v", tc.builtin, got.Float(), tc.want)
			}
		})
	}
}

func TestMathBuiltinsRejectInvalidDomainAndArguments(t *testing.T) {
	machine := New()
	f := value.NewFloat
	cases := []struct {
		builtin string
		args    []value.Value
		want    string
	}{
		{"math_sqrt", []value.Value{f(-1)}, "math.sqrt: domain error (x < 0), got x=-1"},
		{"math_log", []value.Value{f(0)}, "math.log: domain error (x <= 0), got x=0"},
		{"math_log2", []value.Value{f(-3)}, "math.log2: domain error (x <= 0)"},
		{"math_log10", []value.Value{f(0)}, "math.log10: domain error (x <= 0)"},
		{"math_asin", []value.Value{f(2)}, "math.asin: domain error (x outside [-1, 1]), got x=2"},
		{"math_acos", []value.Value{f(-1.5)}, "math.acos: domain error (x outside [-1, 1])"},
		{"math_fmod", []value.Value{f(1), f(0)}, "math.fmod: domain error (y == 0), got x=1, y=0"},
		{"math_pow", []value.Value{f(0), f(-1)}, "math.pow: domain error (x == 0 and y < 0), got x=0, y=-1"},
		{"math_pow", []value.Value{f(-8), f(0.5)}, "math.pow: domain error (x < 0 and y not an integer), got x=-8, y=0.5"},
		{"math_clamp", []value.Value{f(1), f(10), f(0)}, "math.clamp: domain error (lo > hi), got lo=10, hi=0"},
		{"math_sqrt", []value.Value{value.NewInt(4)}, "math.sqrt: x must be a float, got int"},
		{"math_atan2", []value.Value{f(1), value.NewString("x")}, "math.atan2: x must be a float, got string"},
		{"math_sqrt", []value.Value{}, "math.sqrt: expects exactly 1 argument, got 0"},
		{"math_pow", []value.Value{f(1)}, "math.pow: expects exactly 2 arguments, got 1"},
		{"math_clamp_int", []value.Value{value.NewInt(1), value.NewInt(10), value.NewInt(0)}, "math.clamp_int: domain error (lo > hi), got lo=10, hi=0"},
		{"math_clamp_int", []value.Value{f(1), value.NewInt(0), value.NewInt(10)}, "math.clamp_int: x must be an int, got float"},
	}
	for _, tc := range cases {
		t.Run(tc.want, func(t *testing.T) {
			_, err := requireBuiltin(t, machine, tc.builtin).Invoke(machine, tc.args)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("%s%v: err = %v, want %q", tc.builtin, tc.args, err, tc.want)
			}
		})
	}
}

func TestMathClampIntBuiltin(t *testing.T) {
	machine := New()
	for _, tc := range []struct{ x, lo, hi, want int64 }{{-5, 0, 10, 0}, {5, 0, 10, 5}, {50, 0, 10, 10}} {
		got := callBuiltin(t, machine, "math_clamp_int", value.NewInt(tc.x), value.NewInt(tc.lo), value.NewInt(tc.hi))
		if got.Type != value.VAL_INT || got.Int() != tc.want {
			t.Fatalf("clamp_int(%d, %d, %d) = %v, want %d", tc.x, tc.lo, tc.hi, got, tc.want)
		}
	}
}

package vm

import (
	"math"
	"testing"
	"time"

	"noxy-vm/internal/value"
)

func TestValidateNetworkPollArguments(t *testing.T) {
	empty := value.NewArray(nil)
	maximum := int64(math.MaxInt64) / int64(time.Millisecond)
	tests := []struct {
		name      string
		args      []value.Value
		want      time.Duration
		wantError string
	}{
		{"zero", []value.Value{empty, empty, empty, value.NewInt(0)}, 0, ""},
		{"positive", []value.Value{empty, empty, empty, value.NewInt(25)}, 25 * time.Millisecond, ""},
		{"negative", []value.Value{empty, empty, empty, value.NewInt(-1)}, 0, "network poll timeout must be non-negative"},
		{"overflow", []value.Value{empty, empty, empty, value.NewInt(maximum + 1)}, 0, "network poll timeout is too large"},
		{"wrong arity", []value.Value{empty}, 0, "net_select expects exactly 4 arguments"},
		{"wrong set", []value.Value{value.NewNull(), empty, empty, value.NewInt(0)}, 0, "net_select read, write, and error arguments must be arrays"},
		{"wrong timeout", []value.Value{empty, empty, empty, value.NewString("0")}, 0, "network poll timeout must be an int"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, got, err := validateNetworkPollArguments(test.args)
			if test.wantError != "" {
				if err == nil || err.Error() != test.wantError {
					t.Fatalf("error=%v want=%q", err, test.wantError)
				}
				return
			}
			if err != nil || got != test.want {
				t.Fatalf("timeout=%v error=%v want=%v", got, err, test.want)
			}
		})
	}
}

func TestSelectResultPadsTruncatesAndCountsCopiedValues(t *testing.T) {
	values := make([]value.Value, 65)
	for i := range values {
		values[i] = socketValue(i+1, "test", 0, true)
	}
	result := selectResult(values, values[:2], values[:1])
	mapping := requireBuiltinMap(t, result)
	assertBuiltinValue(t, requireTestMapValue(t, mapping, "read_count"), value.NewInt(64))
	assertBuiltinValue(t, requireTestMapValue(t, mapping, "write_count"), value.NewInt(2))
	assertBuiltinValue(t, requireTestMapValue(t, mapping, "error_count"), value.NewInt(1))
	read := requireTestMapValue(t, mapping, "read").Obj.(*value.ObjArray)
	if len(read.Elements) != 64 {
		t.Fatalf("read length=%d", len(read.Elements))
	}
}

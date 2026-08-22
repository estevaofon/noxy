package value

import (
	"fmt"
	"math"
	"testing"
)

// Value.String() de int usa strconv.FormatInt (issue #66, item 2): tem de ser
// byte a byte igual ao "%d" de antes, inclusive nos extremos.
func TestIntStringMatchesPercentD(t *testing.T) {
	for _, n := range []int64{0, 1, -1, 7, -42, 1234567890123, math.MaxInt64, math.MinInt64} {
		if got, want := NewInt(n).String(), fmt.Sprintf("%d", n); got != want {
			t.Errorf("NewInt(%d).String() = %q, want %q", n, got, want)
		}
	}
}

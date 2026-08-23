package vm

import "testing"

func TestIsASCII(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"", true},
		{"item_123456", true},
		{"\x00\x7f", true},
		{"caf\u00e9", false},
		{"\U0001F642", false},
		{"abc\x80", false},
		{"\xffabc", false},
	}
	for _, tc := range cases {
		if got := isASCII(tc.in); got != tc.want {
			t.Errorf("isASCII(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

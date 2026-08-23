package vm

import "testing"

func TestRuneConverterASCIIFastPath(t *testing.T) {
	converter := newRuneConverter("hello world")
	for _, byteOff := range []int{0, 5, 11} {
		if got := converter.index(byteOff); got != byteOff {
			t.Fatalf("index(%d) = %d, want %d (ASCII: byte == runa)", byteOff, got, byteOff)
		}
	}
}

func TestRuneConverterUnicodeMonotonic(t *testing.T) {
	// "aé🙂z": a=1 byte, é=2 bytes, 🙂=4 bytes, z=1 byte
	// byte:  a=0, é=1, 🙂=3, z=7, fim=8
	// runa:  a=0, é=1, 🙂=2, z=3, fim=4
	converter := newRuneConverter("aé🙂z")
	cases := []struct{ byteOff, want int }{{0, 0}, {1, 1}, {3, 2}, {7, 3}, {8, 4}}
	for _, c := range cases {
		if got := converter.index(c.byteOff); got != c.want {
			t.Fatalf("index(%d) = %d, want %d", c.byteOff, got, c.want)
		}
	}
}

func TestRuneConverterRegression(t *testing.T) {
	// Grupos de um match chegam fora de ordem: fim do match antes do início
	// do grupo 1. O conversor tem de aceitar regressão sem se perder.
	converter := newRuneConverter("aé🙂z")
	if got := converter.index(8); got != 4 {
		t.Fatalf("index(8) = %d, want 4", got)
	}
	if got := converter.index(1); got != 1 {
		t.Fatalf("index(1) apos index(8) = %d, want 1", got)
	}
	if got := converter.index(3); got != 2 {
		t.Fatalf("index(3) = %d, want 2", got)
	}
}

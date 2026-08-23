package exttest

import "testing"

func TestBuildGuestProducesWasm(t *testing.T) {
	data := BuildGuest(t, "")
	// Preambulo wasm: \0asm
	if len(data) < 8 || string(data[:4]) != "\x00asm" {
		t.Fatalf("not a wasm binary (%d bytes)", len(data))
	}
	if again := BuildGuest(t, ""); len(again) != len(data) {
		t.Fatal("cache must return the same artifact")
	}
}

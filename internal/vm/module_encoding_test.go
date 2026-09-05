package vm

import (
	"github.com/estevaofon/noxy/internal/value"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestModuleSourceRejectsInvalidUTF8 verifies that using a .nx module whose
// on-disk bytes are not valid UTF-8 fails with a diagnosable error naming the
// file, rather than being silently lexed as mis-tokenised source.
func TestModuleSourceRejectsInvalidUTF8(t *testing.T) {
	root := t.TempDir()
	// 0xFF is never a valid UTF-8 lead byte.
	invalid := []byte("let marker: int = 1 // bad byte: \xffend\n")
	modulePath := filepath.Join(root, "badbytes.nx")
	if err := os.WriteFile(modulePath, invalid, 0o600); err != nil {
		t.Fatal(err)
	}

	code := compileModuleProgram(t, root, "use badbytes\n")
	err := NewWithConfig(VMConfig{RootPath: root}).Interpret(code)
	if err == nil {
		t.Fatal("using a module with invalid UTF-8 bytes succeeded, want error")
	}
	if !strings.Contains(err.Error(), "badbytes.nx") {
		t.Fatalf("error=%q, want it to mention the file badbytes.nx", err.Error())
	}
	if !strings.Contains(err.Error(), "UTF-8") {
		t.Fatalf("error=%q, want it to mention UTF-8", err.Error())
	}
}

// TestModuleSourceWithAccentedCharactersLoadsNormally is a companion to
// TestModuleSourceRejectsInvalidUTF8: it ensures the new validity check does
// not reject legitimate, valid UTF-8 source that merely contains non-ASCII
// characters (e.g. accented letters in comments and string literals).
func TestModuleSourceWithAccentedCharactersLoadsNormally(t *testing.T) {
	root := t.TempDir()
	valid := "// coração, ação, café\nlet greeting: string = \"olá, mundo\"\nlet answer: int = 42\n"
	modulePath := filepath.Join(root, "accented.nx")
	if err := os.WriteFile(modulePath, []byte(valid), 0o600); err != nil {
		t.Fatal(err)
	}

	code := compileModuleProgram(t, root, "use accented\n")
	machine := NewWithConfig(VMConfig{RootPath: root})
	if err := machine.Interpret(code); err != nil {
		t.Fatalf("using a valid accented module failed: %v", err)
	}
	imported, ok := machine.GetGlobal("accented")
	if !ok {
		t.Fatal("missing accented module global")
	}
	exports, ok := imported.Obj.(*value.ObjMap)
	if !ok {
		t.Fatalf("accented module export=%T, want *value.ObjMap", imported.Obj)
	}
	answer, ok := exports.Get("answer")
	if !ok || answer.Type != value.VAL_INT || answer.Int() != 42 {
		t.Fatalf("accented module answer=%v, want int 42", answer)
	}
	greeting, ok := exports.Get("greeting")
	if !ok || greeting.Type != value.VAL_OBJ || greeting.Obj.(string) != "olá, mundo" {
		t.Fatalf("accented module greeting=%v, want %q", greeting, "olá, mundo")
	}
}

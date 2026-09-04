package compiler

import (
	"testing"

	"noxy-vm/internal/ast"
)

// Issue #133: o tipo de struct carrega a identidade da declaracao (Decl);
// Name e so a grafia. Clonar ou substituir um tipo copia o PONTEIRO — clonar
// a declaracao quebraria a identidade em silencio.
func TestCloneAndSubstitutePreserveStructDecl(t *testing.T) {
	decl := &ast.StructStatement{Name: "P"}
	original := &ast.ArrayType{ElementType: &ast.PrimitiveType{Name: "P", Decl: decl}}

	cloned := ast.CloneType(original).(*ast.ArrayType).ElementType.(*ast.PrimitiveType)
	if cloned.Decl != decl {
		t.Fatalf("CloneType lost Decl: got %p, want %p", cloned.Decl, decl)
	}
	substituted := substituteType(original, map[string]ast.NoxyType{}).(*ast.ArrayType).ElementType.(*ast.PrimitiveType)
	if substituted.Decl != decl {
		t.Fatalf("substituteType lost Decl: got %p, want %p", substituted.Decl, decl)
	}
}

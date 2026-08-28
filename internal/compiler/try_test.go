package compiler

import (
	"strings"
	"testing"

	"noxy-vm/internal/lexer"
	"noxy-vm/internal/parser"
)

// Issue #105 item 2: `try expr` propaga a falha de um Result<T>.

func TestTryPropagatesInResultFunction(t *testing.T) {
	src := "use errors select *\nuse convert select *\nfunc parse(s: string) -> Result<int>\n    let n: int = try to_int_result(s)\n    return Ok(n * 2)\nend\n"
	if _, err := compileFunctionSource(t, src); err != nil {
		t.Fatal(err)
	}
}

func TestTryAsStatement(t *testing.T) {
	src := "use errors select *\nfunc falha() -> Result<bool>\n    return Err(\"nope\")\nend\nfunc usa() -> Result<int>\n    try falha()\n    return Ok(1)\nend\n"
	if _, err := compileFunctionSource(t, src); err != nil {
		t.Fatal(err)
	}
}

func TestTryOutsideResultFunctionFails(t *testing.T) {
	src := "use errors select *\nuse convert select *\nfunc main() -> void\n    let n: int = try to_int_result(\"1\")\nend\n"
	_, err := compileFunctionSource(t, src)
	want := "'try' requires the enclosing function to return Result<T> (found void)"
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("want %q, got %v", want, err)
	}
}

func TestTryOnNonResultFails(t *testing.T) {
	src := "use errors select *\nfunc f() -> Result<int>\n    let n: int = try 42\n    return Ok(n)\nend\n"
	_, err := compileFunctionSource(t, src)
	if err == nil || !strings.Contains(err.Error(), "'try' expects a Result<T>, got int") {
		t.Fatalf("got %v", err)
	}
}

func TestTryAtTopLevelFails(t *testing.T) {
	_, err := compileFunctionSource(t, "use errors select *\nuse convert select *\nlet n: int = try to_int_result(\"1\")\n")
	if err == nil || !strings.Contains(err.Error(), "'try' outside a function") {
		t.Fatalf("got %v", err)
	}
}

func TestTryIsReserved(t *testing.T) {
	p := parser.New(lexer.New("let try: int = 1\n"))
	p.ParseProgram()
	if len(p.Errors()) == 0 {
		t.Fatalf("'try' must be a reserved word")
	}
}

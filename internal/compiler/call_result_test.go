package compiler

import (
	"strings"
	"testing"
)

// Issue #105 item 2: call_result devolve errors::Result<R> tipado pelo callee.

func TestCallResultIsTypedByCallee(t *testing.T) {
	c, err := compileFunctionSource(t, "use errors select *\nfunc dobro(x: int) -> int\n    return x * 2\nend\nlet r = call_result(dobro, 21)\n")
	if err != nil {
		t.Fatal(err)
	}
	if got := c.GetGlobals()["r"].String(); got != "errors::Result<int>" {
		t.Fatalf("r: %s", got)
	}
}

func TestCallResultOnNativeUsesCoreReturnType(t *testing.T) {
	c, err := compileFunctionSource(t, "use errors select *\nlet r = call_result(to_int, \"1\")\n")
	if err != nil {
		t.Fatal(err)
	}
	if got := c.GetGlobals()["r"].String(); got != "errors::Result<int>" {
		t.Fatalf("r: %s", got)
	}
}

func TestCallResultOnConstructorAndVoidAndUnknown(t *testing.T) {
	src := "use errors select *\nstruct P\n    x: int\nend\nfunc nada() -> void\nend\nlet a = call_result(P, 7)\nlet b = call_result(nada)\nlet c = call_result(print, 1)\n"
	c, err := compileFunctionSource(t, src)
	if err != nil {
		t.Fatal(err)
	}
	globals := c.GetGlobals()
	if got := globals["a"].String(); got != "errors::Result<P>" {
		t.Fatalf("a: %s", got)
	}
	if got := globals["b"].String(); got != "errors::Result<any>" {
		t.Fatalf("b: %s", got)
	}
	if got := globals["c"].String(); got != "errors::Result<any>" {
		t.Fatalf("c: %s", got)
	}
}

func TestCallResultWithoutErrorsImportFails(t *testing.T) {
	_, err := compileFunctionSource(t, "func f() -> int\n    return 1\nend\nlet r = call_result(f)\n")
	want := "call_result needs 'use errors select *' in scope: its result is errors.Result<T>"
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("want %q, got %v", want, err)
	}
}

func TestResultOkNarrowsValue(t *testing.T) {
	src := "use errors select *\nfunc dobro(x: int) -> int\n    return x * 2\nend\nlet r = call_result(dobro, 21)\nlet n: int = 0\nif r.ok then\n    n = r.value\nend\n"
	if _, err := compileFunctionSource(t, src); err != nil {
		t.Fatal(err)
	}
}

func TestResultValueWithoutOkTestNeedsNullCheck(t *testing.T) {
	src := "use errors select *\nfunc dobro(x: int) -> int\n    return x * 2\nend\nlet r = call_result(dobro, 21)\nlet n: int = r.value\n"
	_, err := compileFunctionSource(t, src)
	if err == nil || !strings.Contains(err.Error(), "expected int, got int?\n  hint: 'r.value' may be null; test it first") {
		t.Fatalf("got %v", err)
	}
}

func TestResultConstructorsTargetTyped(t *testing.T) {
	src := "use errors select *\nfunc parse(s: string) -> Result<int>\n    if s == \"\" then\n        return err(\"empty\")\n    end\n    return ok(length(s))\nend\nlet r: Result<int> = parse(\"ab\")\n"
	if _, err := compileFunctionSource(t, src); err != nil {
		t.Fatal(err)
	}
}

package value

import "testing"

func TestGlobalEnvironmentResolvesAndShadowsParent(t *testing.T) {
	root := NewGlobalEnvironment(nil)
	root.SetLocal("value", NewInt(1))
	child := NewGlobalEnvironment(root)
	if got, _ := child.Resolve("value"); got.AsInt != 1 {
		t.Fatalf("inherited value=%v", got)
	}
	owner, ok := child.ResolveOwner("value")
	if !ok || owner != root {
		t.Fatal("wrong inherited owner")
	}
	child.SetLocal("value", NewInt(2))
	owner, _ = child.ResolveOwner("value")
	if owner != child {
		t.Fatal("shadow did not become local")
	}
}

func TestGlobalEnvironmentExportsAreLiveAndLocalOnly(t *testing.T) {
	root := NewGlobalEnvironment(nil)
	root.SetLocal("builtin", NewInt(1))
	module := NewGlobalEnvironment(root)
	module.SetLocal("answer", NewInt(41))
	exports := module.ExportMap().Obj.(*ObjMap)
	module.SetLocal("answer", NewInt(42))
	if got, _ := exports.Get("answer"); got.AsInt != 42 {
		t.Fatalf("live export=%v", got)
	}
	if _, inherited := exports.Get("builtin"); inherited {
		t.Fatal("exports leaked parent binding")
	}
}

func TestGlobalEnvironmentLocalOperationsAndSnapshots(t *testing.T) {
	environment := NewGlobalEnvironmentFrom(map[string]Value{
		"seed": NewInt(1),
	}, nil)

	if got, found := environment.GetLocal("seed"); !found || got.AsInt != 1 {
		t.Fatalf("seed=(%v,%t)", got, found)
	}
	if environment.DefineLocalIfAbsent("seed", NewInt(2)) {
		t.Fatal("existing local binding was redefined")
	}
	if !environment.DefineLocalIfAbsent("new", NewInt(3)) {
		t.Fatal("missing local binding was not defined")
	}

	snapshot := environment.LocalSnapshot()
	snapshot["seed"] = NewInt(0)
	if got, _ := environment.GetLocal("seed"); got.AsInt != 1 {
		t.Fatal("local snapshot mutated environment")
	}

	environment.ReplaceLocal(map[string]Value{"replacement": NewInt(4)})
	if _, found := environment.GetLocal("seed"); found {
		t.Fatal("replace retained old local binding")
	}
	if got, found := environment.GetLocal("replacement"); !found || got.AsInt != 4 {
		t.Fatalf("replacement=(%v,%t)", got, found)
	}
}

func TestGlobalEnvironmentNilDoesNotResolve(t *testing.T) {
	var environment *GlobalEnvironment
	if _, found := environment.GetLocal("missing"); found {
		t.Fatal("nil environment resolved a local binding")
	}
	if _, found := environment.Resolve("missing"); found {
		t.Fatal("nil environment resolved a binding")
	}
	if owner, found := environment.ResolveOwner("missing"); found || owner != nil {
		t.Fatalf("nil resolve owner=(%v,%t)", owner, found)
	}
}

package main

import (
	"strings"
	"testing"
)

func TestParseBuildArgsAcceptsFlagsAfterTheEntry(t *testing.T) {
	got, err := parseBuildArgs([]string{"editor.nx", "-o", "dist/noxy-editor", "--include", "web", "--include=docs/x.txt", "--list"})
	if err != nil {
		t.Fatal(err)
	}
	if got.entry != "editor.nx" || got.output != "dist/noxy-editor" || strings.Join(got.includes, ",") != "web,docs/x.txt" || !got.list {
		t.Fatalf("%+v", got)
	}
	if got, err := parseBuildArgs([]string{"-o=app", "main.nx"}); err != nil || got.output != "app" || got.entry != "main.nx" {
		t.Fatalf("%+v %v", got, err)
	}
	if got, err := parseBuildArgs([]string{"--help"}); err != nil || !got.help {
		t.Fatalf("%+v %v", got, err)
	}
}

func TestParseBuildArgsRejectsBadUsage(t *testing.T) {
	for _, args := range [][]string{{}, {"a.nx", "b.nx"}, {"a.nx", "-o"}, {"a.nx", "--bogus"}} {
		if _, err := parseBuildArgs(args); err == nil {
			t.Errorf("%v must be rejected", args)
		}
	}
}

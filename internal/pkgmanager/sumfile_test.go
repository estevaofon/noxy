package pkgmanager

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSumFileV2RoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "noxy.sum")
	s, err := ParseSumFile(path)
	if err != nil {
		t.Fatalf("missing file must parse as empty: %v", err)
	}
	s.SetTree("github.com/acme/zstd", "v0.3.0", "aaaa")
	s.SetArtifact("github.com/acme/zstd", "v0.3.0", "noxy_ext.toml", "bbbb")
	s.SetArtifact("github.com/acme/zstd", "v0.3.0", "bin/zstd-linux-amd64", "cccc")
	s.SetTree("github.com/acme/alpha", "v1.0.0", "dddd")
	if err := s.Save(path); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	want := "github.com/acme/alpha v1.0.0 sha256:dddd\n" +
		"github.com/acme/zstd v0.3.0 bin/zstd-linux-amd64 sha256:cccc\n" +
		"github.com/acme/zstd v0.3.0 noxy_ext.toml sha256:bbbb\n" +
		"github.com/acme/zstd v0.3.0 sha256:aaaa\n"
	if string(data) != want {
		t.Fatalf("got:\n%s\nwant:\n%s", data, want)
	}
	back, err := ParseSumFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if v, d, ok := back.TreeHash("github.com/acme/zstd"); !ok || v != "v0.3.0" || d != "aaaa" {
		t.Fatalf("TreeHash: %q %q %v", v, d, ok)
	}
	if d, ok := back.Lookup("github.com/acme/zstd", "noxy_ext.toml"); !ok || d != "bbbb" {
		t.Fatalf("Lookup ignores version: %q %v", d, ok)
	}
	if got := back.Modules(); len(got) != 2 || got[0] != "github.com/acme/alpha" {
		t.Fatalf("Modules: %v", got)
	}
	if arts := back.Artifacts("github.com/acme/zstd"); len(arts) != 2 || arts["bin/zstd-linux-amd64"] != "cccc" {
		t.Fatalf("Artifacts: %v", arts)
	}
	back.DropModule("github.com/acme/zstd")
	if _, _, ok := back.TreeHash("github.com/acme/zstd"); ok || len(back.Modules()) != 1 {
		t.Fatal("DropModule must remove every line of the module")
	}
}

func TestSumFileRefusesTwoVersionsOfOneModule(t *testing.T) {
	s := &SumFile{}
	s.SetTree("github.com/acme/x", "v1.0.0", "aa")
	s.SetArtifact("github.com/acme/x", "v1.1.0", "noxy_ext.toml", "bb")
	if err := s.Save(filepath.Join(t.TempDir(), "noxy.sum")); err == nil || !strings.Contains(err.Error(), "two versions") {
		t.Fatalf("one version per module (spec §3.2), got %v", err)
	}
}

func TestSumFileReadsV1LinesAsUnversioned(t *testing.T) {
	path := filepath.Join(t.TempDir(), "noxy.sum")
	v1 := "github_com/estevaofon/noxy_dynamodb bin/noxy-plugin-dynamodb-linux-amd64 sha256:69fe\n" +
		"github_com/estevaofon/noxy_dynamodb noxy_ext.toml sha256:bcca\n"
	if err := os.WriteFile(path, []byte(v1), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := ParseSumFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if d, ok := s.Lookup("github.com/estevaofon/noxy_dynamodb", "noxy_ext.toml"); !ok || d != "bcca" {
		t.Fatalf("v1 key must be read through ModulePath: %q %v", d, ok)
	}
	if _, _, ok := s.TreeHash("github.com/estevaofon/noxy_dynamodb"); ok {
		t.Fatal("a v1 module has no tree hash")
	}
	if v, ok := s.Version("github.com/estevaofon/noxy_dynamodb"); !ok || v != "" {
		t.Fatalf("v1 version is unknown (empty): %q %v", v, ok)
	}
	// Um Save regrava só o que foi re-registrado: linhas v1 são descartadas.
	s.DropModule("github.com/estevaofon/noxy_dynamodb")
	s.SetTree("github.com/estevaofon/noxy_dynamodb", "v0.3.0", "ee")
	if err := s.Save(path); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	if strings.Contains(string(data), "github_com/") {
		t.Fatalf("v1 lines must not survive a save:\n%s", data)
	}
	bad := filepath.Join(t.TempDir(), "noxy.sum")
	_ = os.WriteFile(bad, []byte("only two\n"), 0o644)
	if _, err := ParseSumFile(bad); err == nil {
		t.Fatal("malformed line must fail")
	}
}

func TestModulePathLocalPathRoundTrip(t *testing.T) {
	for module, local := range map[string]string{
		"github.com/estevaofon/quicksort": "github_com/estevaofon/quicksort",
		"gitlab.example.org/g/r":          "gitlab_example_org/g/r",
		"guest":                           "guest",
	} {
		if LocalPath(module) != local || ModulePath(local) != module {
			t.Fatalf("%s ↔ %s: got %s / %s", module, local, LocalPath(module), ModulePath(local))
		}
	}
}

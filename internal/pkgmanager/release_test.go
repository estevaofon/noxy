package pkgmanager

import (
	"strings"
	"testing"
)

func TestReleaseBaseURL(t *testing.T) {
	got, err := ReleaseBaseURL("github.com/estevaofon/noxy_terminal", "v0.2.0")
	if err != nil || got != "https://github.com/estevaofon/noxy_terminal/releases/download/v0.2.0/" {
		t.Fatalf("%q %v", got, err)
	}
	if _, err := ReleaseBaseURL("github.com/estevaofon", "v0.2.0"); err == nil {
		t.Fatal("a path without a repo cannot have releases")
	}
	if _, err := ReleaseBaseURL("github.com/estevaofon/noxy_terminal", "HEAD"); err == nil {
		t.Fatal("HEAD is not a release tag")
	}
}

func TestParseChecksums(t *testing.T) {
	data := []byte("" +
		"0000000000000000000000000000000000000000000000000000000000000001  noxy-plugin-a-linux-amd64\n" +
		"0000000000000000000000000000000000000000000000000000000000000002 *noxy-plugin-a-windows-amd64.exe\n" +
		"\n# comment\n")
	sums, err := ParseChecksums(data)
	if err != nil {
		t.Fatal(err)
	}
	if sums["noxy-plugin-a-linux-amd64"] != strings.Repeat("0", 63)+"1" || sums["noxy-plugin-a-windows-amd64.exe"] != strings.Repeat("0", 63)+"2" {
		t.Fatalf("parsed: %v", sums)
	}
	if _, err := ParseChecksums([]byte("nothex  file\n")); err == nil {
		t.Fatal("a non-hex digest is malformed")
	}
	if _, err := ParseChecksums([]byte("0000000000000000000000000000000000000000000000000000000000000001\n")); err == nil {
		t.Fatal("a line without a file name is malformed")
	}
}

func TestNewestSemverTag(t *testing.T) {
	ls := "" +
		"aaa\trefs/tags/v0.9.1\n" +
		"bbb\trefs/tags/v0.10.0\n" +
		"ccc\trefs/tags/v0.10.0^{}\n" +
		"ddd\trefs/tags/experimental\n" +
		"eee\trefs/tags/v0.2.0\n"
	tag, ok := newestSemverTag(ls)
	if !ok || tag != "v0.10.0" {
		t.Fatalf("%q %v", tag, ok)
	}
	if _, ok := newestSemverTag("ddd\trefs/tags/experimental\n"); ok {
		t.Fatal("no semver tag → not found")
	}
	if tag, ok := newestSemverTag("x\trefs/tags/1.2.3\n"); !ok || tag != "1.2.3" {
		t.Fatalf("tags without the v prefix count too: %q %v", tag, ok)
	}
}

func TestToGitURL(t *testing.T) {
	if got := toGitURL("github.com/a/b"); got != "https://github.com/a/b" {
		t.Fatal(got)
	}
	if got := toGitURL("git@github.com:a/b.git"); got != "git@github.com:a/b.git" {
		t.Fatal(got)
	}
	if got := toGitURL("https://x/y"); got != "https://x/y" {
		t.Fatal(got)
	}
}

package pkgmanager

import (
	"testing"
	"time"
)

func TestParseVersionNormalizesPrefix(t *testing.T) {
	for _, in := range []string{"1.2.3", "v1.2.3"} {
		v, err := ParseVersion(in)
		if err != nil || v.String() != "v1.2.3" {
			t.Fatalf("%q → %v %v", in, v, err)
		}
	}
	for _, bad := range []string{"HEAD", "1.2", "v1.2.3.4", "abc", ""} {
		if _, err := ParseVersion(bad); err == nil {
			t.Fatalf("%q must not parse", bad)
		}
	}
}

func TestCompareVersionsOrdersTagsAndPseudoVersions(t *testing.T) {
	order := []string{
		"v0.0.0-20260101000000-aaaaaaaaaaaa", // sem tag base
		"v0.0.0-20260102000000-bbbbbbbbbbbb", // timestamp maior
		"v0.1.0",
		"v0.1.1-0.20260301000000-cccccccccccc", // pseudo acima da base v0.1.0
		"v0.1.1-0.20260302000000-dddddddddddd",
		"v0.1.1",                               // release acima da sua pré-release
		"v1.0.0",
	}
	for i := 0; i+1 < len(order); i++ {
		a, _ := ParseVersion(order[i])
		b, _ := ParseVersion(order[i+1])
		if CompareVersions(a, b) != -1 || CompareVersions(b, a) != 1 || CompareVersions(a, a) != 0 {
			t.Fatalf("%s must sort before %s", order[i], order[i+1])
		}
	}
}

func TestPseudoVersionForms(t *testing.T) {
	ts := time.Date(2026, 9, 4, 15, 30, 0, 0, time.UTC)
	sha := "abcdef1234567890abcdef1234567890abcdef12"
	if got := PseudoVersion("", ts, sha); got != "v0.0.0-20260904153000-abcdef123456" {
		t.Fatalf("no base: %s", got)
	}
	if got := PseudoVersion("v0.1.0", ts, sha); got != "v0.1.1-0.20260904153000-abcdef123456" {
		t.Fatalf("base v0.1.0: %s", got)
	}
	if got := PseudoVersion("2.3.4", ts, sha); got != "v2.3.5-0.20260904153000-abcdef123456" {
		t.Fatalf("base without v: %s", got)
	}
	for _, s := range []string{"v0.0.0-20260904153000-abcdef123456", "v0.1.1-0.20260904153000-abcdef123456"} {
		if !IsPseudoVersion(s) || pseudoSHA(s) != "abcdef123456" {
			t.Fatalf("%s must be a pseudo-version with sha abcdef123456", s)
		}
	}
	if IsPseudoVersion("v0.1.0") || IsPseudoVersion("v1.0.0-rc1") || pseudoSHA("v0.1.0") != "" {
		t.Fatal("tags are not pseudo-versions")
	}
	if !IsSemverTag("v0.1.0") || !IsSemverTag("0.1.0") || IsSemverTag("v1.0.0-rc1") || IsSemverTag("HEAD") {
		t.Fatal("IsSemverTag: release tags only")
	}
}

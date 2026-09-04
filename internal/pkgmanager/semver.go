package pkgmanager

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Version e uma versao semver 2.0.0 sem metadados de build. Pre e a parte
// apos o '-' ("" = release). Pseudo-versoes (spec §4.1) sao pre-releases:
// v0.0.0-<ts>-<sha12> (sem tag base) e vX.Y.(Z+1)-0.<ts>-<sha12>.
type Version struct {
	Major, Minor, Patch int
	Pre                 string
}

var (
	versionRE   = regexp.MustCompile(`^v?(\d+)\.(\d+)\.(\d+)(?:-([0-9A-Za-z.-]+))?$`)
	pseudoPreRE = regexp.MustCompile(`^(?:0\.)?(\d{14})-([0-9a-f]{12})$`)
)

func ParseVersion(s string) (Version, error) {
	m := versionRE.FindStringSubmatch(s)
	if m == nil {
		return Version{}, fmt.Errorf("invalid version %q (want vMAJOR.MINOR.PATCH)", s)
	}
	var v Version
	v.Major, _ = strconv.Atoi(m[1])
	v.Minor, _ = strconv.Atoi(m[2])
	v.Patch, _ = strconv.Atoi(m[3])
	v.Pre = m[4]
	return v, nil
}

func (v Version) String() string {
	s := fmt.Sprintf("v%d.%d.%d", v.Major, v.Minor, v.Patch)
	if v.Pre != "" {
		s += "-" + v.Pre
	}
	return s
}

func NormalizeVersion(s string) (string, error) {
	v, err := ParseVersion(s)
	if err != nil {
		return "", err
	}
	return v.String(), nil
}

// CompareVersions segue semver §11: release > pre-release da mesma tripla;
// identificadores de pre-release comparam por '.', numerico < alfanumerico.
func CompareVersions(a, b Version) int {
	for _, d := range []int{a.Major - b.Major, a.Minor - b.Minor, a.Patch - b.Patch} {
		if d < 0 {
			return -1
		}
		if d > 0 {
			return 1
		}
	}
	switch {
	case a.Pre == b.Pre:
		return 0
	case a.Pre == "":
		return 1
	case b.Pre == "":
		return -1
	}
	as, bs := strings.Split(a.Pre, "."), strings.Split(b.Pre, ".")
	for i := 0; i < len(as) && i < len(bs); i++ {
		an, aNum := strconv.Atoi(as[i])
		bn, bNum := strconv.Atoi(bs[i])
		switch {
		case aNum == nil && bNum == nil:
			if an != bn {
				if an < bn {
					return -1
				}
				return 1
			}
		case aNum == nil:
			return -1
		case bNum == nil:
			return 1
		default:
			if c := strings.Compare(as[i], bs[i]); c != 0 {
				return c
			}
		}
	}
	switch {
	case len(as) < len(bs):
		return -1
	case len(as) > len(bs):
		return 1
	}
	return 0
}

func IsSemverTag(s string) bool {
	v, err := ParseVersion(s)
	return err == nil && v.Pre == ""
}

func IsPseudoVersion(s string) bool {
	return pseudoSHA(s) != ""
}

func pseudoSHA(s string) string {
	v, err := ParseVersion(s)
	if err != nil {
		return ""
	}
	m := pseudoPreRE.FindStringSubmatch(v.Pre)
	if m == nil {
		return ""
	}
	return m[2]
}

// PseudoVersion segue a forma do Go: com tag base vX.Y.Z ancestral do commit,
// vX.Y.(Z+1)-0.<ts>-<sha12>, que vence a base no MVS; sem tag, v0.0.0-<ts>-<sha12>.
func PseudoVersion(baseTag string, commitTime time.Time, sha string) string {
	ts := commitTime.UTC().Format("20060102150405")
	if len(sha) > 12 {
		sha = sha[:12]
	}
	base, err := ParseVersion(baseTag)
	if baseTag == "" || err != nil || base.Pre != "" {
		return fmt.Sprintf("v0.0.0-%s-%s", ts, sha)
	}
	return fmt.Sprintf("v%d.%d.%d-0.%s-%s", base.Major, base.Minor, base.Patch+1, ts, sha)
}

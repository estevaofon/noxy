package pkgmanager

import (
	"encoding/hex"
	"fmt"
	"net/http"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Costuras trocadas pelos testes (servidor httptest, repositorio local).
var (
	gitURLFor        = toGitURL
	releaseBaseURL   = ReleaseBaseURL
	resolveNewestTag = func(gitURL string) (string, error) {
		out, err := gitLsRemoteTags(gitURL)
		if err != nil {
			return "", err
		}
		tag, ok := newestSemverTag(out)
		if !ok {
			return "", fmt.Errorf("no semver tag found — process extensions are installed from a tagged release")
		}
		return tag, nil
	}
	httpClient = &http.Client{Timeout: 60 * time.Second}
)

func toGitURL(repoURL string) string {
	if strings.HasPrefix(repoURL, "http") || strings.HasPrefix(repoURL, "git@") {
		return repoURL
	}
	return "https://" + repoURL
}

// ReleaseBaseURL deriva a URL dos assets de uma release (spec §8.2):
// github.com/<user>/<repo> + tag → .../releases/download/<tag>/. Forges com
// o mesmo layout funcionam pela mesma regra.
func ReleaseBaseURL(repoPath, tag string) (string, error) {
	path := strings.TrimPrefix(strings.TrimPrefix(repoPath, "https://"), "http://")
	parts := strings.Split(strings.TrimSuffix(path, "/"), "/")
	if len(parts) < 3 || tag == "" || tag == "HEAD" {
		return "", fmt.Errorf("cannot derive a release URL from %q@%q", repoPath, tag)
	}
	return "https://" + strings.Join(parts[:3], "/") + "/releases/download/" + tag + "/", nil
}

// ParseChecksums le o formato do sha256sum: "<hex>  <nome>" por linha
// ("*nome" do modo binario e aceito).
func ParseChecksums(data []byte) (map[string]string, error) {
	out := map[string]string{}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			return nil, fmt.Errorf("checksums.txt: malformed line %q", line)
		}
		digest := strings.ToLower(fields[0])
		if raw, err := hex.DecodeString(digest); err != nil || len(raw) != 32 {
			return nil, fmt.Errorf("checksums.txt: %q is not a sha256 digest", fields[0])
		}
		out[strings.TrimPrefix(fields[1], "*")] = digest
	}
	return out, nil
}

var semverTagRE = regexp.MustCompile(`^v?(\d+)\.(\d+)\.(\d+)$`)

// newestSemverTag escolhe a maior tag semver numa saida de
// `git ls-remote --tags` (linhas "<sha>\trefs/tags/<tag>"; "^{}" e a tag
// anotada resolvida — mesma versao).
func newestSemverTag(lsRemote string) (string, bool) {
	var best string
	var bestV [3]int
	found := false
	for _, line := range strings.Split(lsRemote, "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			continue
		}
		tag := strings.TrimSuffix(strings.TrimPrefix(fields[1], "refs/tags/"), "^{}")
		m := semverTagRE.FindStringSubmatch(tag)
		if m == nil {
			continue
		}
		var v [3]int
		for i := 0; i < 3; i++ {
			v[i], _ = strconv.Atoi(m[i+1])
		}
		if !found || v[0] > bestV[0] || (v[0] == bestV[0] && (v[1] > bestV[1] || (v[1] == bestV[1] && v[2] > bestV[2]))) {
			best, bestV, found = tag, v, true
		}
	}
	return best, found
}

func gitLsRemoteTags(gitURL string) (string, error) {
	out, err := exec.Command("git", "ls-remote", "--tags", gitURL).Output()
	if err != nil {
		return "", fmt.Errorf("git ls-remote --tags %s: %w", gitURL, err)
	}
	return string(out), nil
}

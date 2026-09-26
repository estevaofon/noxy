package pkgmanager

import (
	"errors"
	"fmt"
	"os"
	"path"
	"regexp"
	"slices"
	"sort"
	"strings"
)

const HeadVersion = "HEAD"

type ModuleConfig struct {
	Module      string
	NoxyVersion string
	Require     map[string]string // modulo → versao normalizada ou HEAD
	// Include: diretiva `include <caminho>` (spec 2026-09-26 §3.2) — assets
	// que `noxy build` embute, relativos ao diretorio do noxy.mod, com "/".
	// --sync e --get nao a usam, mas Save a preserva.
	Include []string
}

func NewModuleConfig() *ModuleConfig {
	return &ModuleConfig{Require: make(map[string]string)}
}

// Caminho de modulo e host/user/repo nu (spec §3.1): sem esquema, sem "@",
// host com ponto. "github_com/..." e caminho LOCAL, nao passa.
var modulePathRE = regexp.MustCompile(`^[A-Za-z0-9-]+(\.[A-Za-z0-9-]+)+(/[A-Za-z0-9._-]+){2,}$`)

func ValidateModulePath(path string) error {
	if !modulePathRE.MatchString(path) {
		return fmt.Errorf("module path must be host/user/repo, got %q", path)
	}
	return nil
}

// ValidateIncludePath aceita so caminho relativo com "/" que fique sob a
// raiz do projeto, e devolve-o limpo ("./web/" → "web").
func ValidateIncludePath(p string) (string, error) {
	p = strings.TrimSpace(p)
	if p == "" {
		return "", errors.New("include path is empty")
	}
	if strings.Contains(p, "\\") {
		return "", fmt.Errorf("include \"%s\": use forward slashes", p)
	}
	if strings.HasPrefix(p, "/") || (len(p) >= 2 && p[1] == ':') {
		return "", fmt.Errorf("include \"%s\" is outside the project root", p)
	}
	clean := path.Clean(p)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return "", fmt.Errorf("include \"%s\" is outside the project root", p)
	}
	return clean, nil
}

func ParseModFile(path string) (*ModuleConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	config := NewModuleConfig()
	for i, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "//") {
			continue
		}
		parts := strings.Fields(line)
		switch parts[0] {
		case "module":
			if len(parts) >= 2 {
				config.Module = parts[1]
			}
		case "noxy":
			if len(parts) >= 2 {
				config.NoxyVersion = parts[1]
			}
		case "require":
			if len(parts) < 3 {
				return nil, fmt.Errorf("noxy.mod:%d: require <module> <version>", i+1)
			}
			if err := ValidateModulePath(parts[1]); err != nil {
				return nil, fmt.Errorf("noxy.mod:%d: %w", i+1, err)
			}
			version := parts[2]
			if version != HeadVersion {
				normalized, err := NormalizeVersion(version)
				if err != nil {
					return nil, fmt.Errorf("noxy.mod:%d: %w (use a tag, a pseudo-version or HEAD)", i+1, err)
				}
				version = normalized
			}
			config.Require[parts[1]] = version
		case "include":
			if len(parts) < 2 {
				return nil, fmt.Errorf("noxy.mod:%d: include <path>", i+1)
			}
			clean, err := ValidateIncludePath(parts[1])
			if err != nil {
				return nil, fmt.Errorf("noxy.mod:%d: %w", i+1, err)
			}
			if !slices.Contains(config.Include, clean) {
				config.Include = append(config.Include, clean)
			}
		}
	}
	return config, nil
}

// Requires devolve os modulos em ordem lexicografica — a unica ordem que
// Save, o lock e a saida do --sync usam.
func (c *ModuleConfig) Requires() []string {
	out := make([]string, 0, len(c.Require))
	for m := range c.Require {
		out = append(out, m)
	}
	sort.Strings(out)
	return out
}

func (c *ModuleConfig) Save(path string) error {
	var sb strings.Builder
	if c.Module != "" {
		fmt.Fprintf(&sb, "module %s\n\n", c.Module)
	}
	if c.NoxyVersion != "" {
		fmt.Fprintf(&sb, "noxy %s\n\n", c.NoxyVersion)
	}
	for _, m := range c.Requires() {
		version := c.Require[m]
		if version != HeadVersion {
			normalized, err := NormalizeVersion(version)
			if err != nil {
				return fmt.Errorf("noxy.mod: require %s: %w", m, err)
			}
			version = normalized
		}
		fmt.Fprintf(&sb, "require %s %s\n", m, version)
	}
	if len(c.Include) > 0 {
		includes := append([]string(nil), c.Include...)
		sort.Strings(includes)
		if s := sb.String(); !strings.HasSuffix(s, "\n\n") {
			sb.WriteString("\n")
		}
		for _, include := range includes {
			fmt.Fprintf(&sb, "include %s\n", include)
		}
	}
	return os.WriteFile(path, []byte(sb.String()), 0o644)
}

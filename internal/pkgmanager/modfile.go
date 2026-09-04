package pkgmanager

import (
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
)

const HeadVersion = "HEAD"

type ModuleConfig struct {
	Module      string
	NoxyVersion string
	Require     map[string]string // modulo → versao normalizada ou HEAD
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
	return os.WriteFile(path, []byte(sb.String()), 0o644)
}

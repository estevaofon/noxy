package pkgmanager

import (
	"fmt"
	"io/ioutil"
	"strings"
)

type ModuleConfig struct {
	Module  string
	Require map[string]string
}

func NewModuleConfig() *ModuleConfig {
	return &ModuleConfig{
		Require: make(map[string]string),
	}
}

func ParseModFile(path string) (*ModuleConfig, error) {
	data, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, err
	}

	config := NewModuleConfig()
	lines := strings.Split(string(data), "\n")

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "//") {
			continue
		}

		parts := strings.Fields(line)
		if len(parts) == 0 {
			continue
		}

		switch parts[0] {
		case "module":
			if len(parts) >= 2 {
				config.Module = parts[1]
			}
		case "require":
			if len(parts) >= 3 {
				// require <pkg> <version>
				config.Require[parts[1]] = parts[2]
			}
		}
	}

	return config, nil
}

func (c *ModuleConfig) Save(path string) error {
	var sb strings.Builder

	if c.Module != "" {
		sb.WriteString(fmt.Sprintf("module %s\n\n", c.Module))
	}

	if len(c.Require) > 0 {
		for pkg, ver := range c.Require {
			sb.WriteString(fmt.Sprintf("require %s %s\n", pkg, ver))
		}
	}

	return ioutil.WriteFile(path, []byte(sb.String()), 0644)
}

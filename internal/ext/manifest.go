package ext

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"
)

const (
	defaultMemoryMB     = 64
	hostMemoryCeilingMB = 256
	supportedABI        = 1
)

type ExportDecl struct {
	Name     string   `toml:"name"`
	Params   []string `toml:"params"`
	Returns  string   `toml:"returns"`
	Stateful bool     `toml:"stateful"`
}

type Manifest struct {
	Name         string       `toml:"name"`
	ABI          int          `toml:"abi"`
	MinNoxy      string       `toml:"min_noxy"`
	Concurrency  string       `toml:"concurrency"`
	Capabilities []string     `toml:"capabilities"`
	MemoryMaxMB  int          `toml:"memory_max_mb"`
	Wasm         string       `toml:"wasm"`
	Exports      []ExportDecl `toml:"export"`
}

var manifestNameRE = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

var scalarTypeNames = map[string]bool{
	"int": true, "float": true, "bool": true, "string": true,
	"bytes": true, "any": true,
}

var structTypeNameRE = regexp.MustCompile(`^[A-Z][A-Za-z0-9_]*$`)

// validTypeName aceita o vocabulario da spec §7: escalares, void, T[],
// map[K]V e nomes de struct declarados no wrapper .nx.
func validTypeName(name string) bool {
	switch {
	case scalarTypeNames[name], name == "void":
		return true
	case strings.HasSuffix(name, "[]"):
		return validTypeName(strings.TrimSuffix(name, "[]"))
	case strings.HasPrefix(name, "map[") && strings.Contains(name, "]"):
		closing := strings.Index(name, "]")
		key := name[len("map["):closing]
		elem := name[closing+1:]
		return (key == "string" || key == "int") && elem != "" && validTypeName(elem)
	default:
		return structTypeNameRE.MatchString(name)
	}
}

func ParseManifest(data []byte) (*Manifest, error) {
	var m Manifest
	meta, err := toml.Decode(string(data), &m)
	if err != nil {
		return nil, fmt.Errorf("noxy_ext.toml: %w", err)
	}
	// Chaves desconhecidas sao erro, nao warning (spec §10): typo falha na
	// publicacao, nao silenciosamente em runtime.
	if undecoded := meta.Undecoded(); len(undecoded) != 0 {
		return nil, fmt.Errorf("noxy_ext.toml: unknown key %q", undecoded[0].String())
	}
	if !manifestNameRE.MatchString(m.Name) {
		return nil, fmt.Errorf("noxy_ext.toml: invalid extension name %q", m.Name)
	}
	if m.ABI != supportedABI {
		return nil, fmt.Errorf("noxy_ext.toml: unsupported abi %d (host supports %d)", m.ABI, supportedABI)
	}
	switch m.Concurrency {
	case "":
		m.Concurrency = "single"
	case "single", "stateless":
	default:
		return nil, fmt.Errorf("noxy_ext.toml: invalid concurrency %q", m.Concurrency)
	}
	if m.Wasm == "" {
		m.Wasm = "ext.wasm"
	}
	if m.MemoryMaxMB == 0 {
		m.MemoryMaxMB = defaultMemoryMB
	}
	if m.MemoryMaxMB < 0 {
		return nil, fmt.Errorf("noxy_ext.toml: memory_max_mb %d must not be negative", m.MemoryMaxMB)
	}
	if m.MemoryMaxMB > hostMemoryCeilingMB {
		return nil, fmt.Errorf("noxy_ext.toml: memory_max_mb %d exceeds host ceiling %d", m.MemoryMaxMB, hostMemoryCeilingMB)
	}
	if len(m.Exports) == 0 {
		return nil, fmt.Errorf("noxy_ext.toml: at least one [[export]] is required")
	}
	prefix := m.Name + "_"
	seen := map[string]bool{}
	for _, exp := range m.Exports {
		if !strings.HasPrefix(exp.Name, prefix) {
			return nil, fmt.Errorf("noxy_ext.toml: export %q must start with %q", exp.Name, prefix)
		}
		if !manifestNameRE.MatchString(exp.Name) {
			return nil, fmt.Errorf("noxy_ext.toml: invalid export name %q", exp.Name)
		}
		if seen[exp.Name] {
			return nil, fmt.Errorf("noxy_ext.toml: duplicate export %q", exp.Name)
		}
		seen[exp.Name] = true
		for _, p := range exp.Params {
			if p == "void" || !validTypeName(p) {
				return nil, fmt.Errorf("noxy_ext.toml: export %q: invalid param type %q", exp.Name, p)
			}
		}
		if exp.Returns == "" || !validTypeName(exp.Returns) {
			return nil, fmt.Errorf("noxy_ext.toml: export %q: invalid return type %q", exp.Name, exp.Returns)
		}
		if m.Concurrency == "stateless" && exp.Stateful {
			return nil, fmt.Errorf("noxy_ext.toml: stateless extension cannot declare stateful export %q", exp.Name)
		}
	}
	// M1 nao implementa capability nenhuma: aceitar a declaracao seria
	// prometer o que o host ignora (revisao do plano, item 6).
	if len(m.Capabilities) != 0 {
		return nil, fmt.Errorf("noxy_ext.toml: capabilities are not supported in this phase (M1)")
	}
	return &m, nil
}

func parseVersion(v string) ([3]int, error) {
	var out [3]int
	trimmed := strings.TrimPrefix(strings.TrimSpace(v), "v")
	parts := strings.Split(trimmed, ".")
	if len(parts) != 3 {
		return out, fmt.Errorf("invalid version %q", v)
	}
	for i, part := range parts {
		n, err := strconv.Atoi(part)
		if err != nil {
			return out, fmt.Errorf("invalid version %q", v)
		}
		out[i] = n
	}
	return out, nil
}

func (m *Manifest) CheckMinNoxy(current string) error {
	if m.MinNoxy == "" {
		return nil
	}
	minimum, err := parseVersion(m.MinNoxy)
	if err != nil {
		return fmt.Errorf("noxy_ext.toml: %w", err)
	}
	have, err := parseVersion(current)
	if err != nil {
		return err
	}
	for i := 0; i < 3; i++ {
		if have[i] != minimum[i] {
			if have[i] < minimum[i] {
				return fmt.Errorf("extension %q requires noxy >= %s (running %s)", m.Name, m.MinNoxy, current)
			}
			return nil
		}
	}
	return nil
}

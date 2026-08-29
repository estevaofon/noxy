package ext

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

const (
	defaultMemoryMB     = 64
	hostMemoryCeilingMB = 256
	supportedABI        = 1

	KindWasm    = "wasm"
	KindProcess = "process"

	defaultCallTimeoutMs      = 30000
	defaultHandshakeTimeoutMs = 5000
)

type ExportDecl struct {
	Name     string   `toml:"name"`
	Params   []string `toml:"params"`
	Returns  string   `toml:"returns"`
	Stateful bool     `toml:"stateful"`
	// TimeoutMs so vale em kind = "process"; nil herda call_timeout_ms.
	TimeoutMs *int `toml:"timeout_ms"`
}

type Manifest struct {
	Name         string       `toml:"name"`
	ABI          int          `toml:"abi"`
	Kind         string       `toml:"kind"`
	MinNoxy      string       `toml:"min_noxy"`
	Concurrency  string       `toml:"concurrency"`
	Capabilities []string     `toml:"capabilities"`
	MemoryMaxMB  int          `toml:"memory_max_mb"`
	Wasm         string       `toml:"wasm"`
	Exports      []ExportDecl `toml:"export"`

	// Chaves de kind = "process" (spec 2026-08-29 §7). Ponteiros distinguem
	// "ausente" de "0" — 0 significa sem prazo.
	Binaries           map[string]string `toml:"binaries"`
	CallTimeoutMs      *int              `toml:"call_timeout_ms"`
	HandshakeTimeoutMs *int              `toml:"handshake_timeout_ms"`
	Restart            bool              `toml:"restart"`
}

var manifestNameRE = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

var (
	binaryKeyRE  = regexp.MustCompile(`^[a-z0-9]+-[a-z0-9]+$`)
	assetNameRE  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)
	capabilityRE = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
)

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
	switch m.Kind {
	case "":
		m.Kind = KindWasm
	case KindWasm, KindProcess:
	default:
		return nil, fmt.Errorf("noxy_ext.toml: invalid kind %q (wasm | process)", m.Kind)
	}
	switch m.Concurrency {
	case "":
		m.Concurrency = "single"
	case "single", "stateless":
	case "concurrent":
		if m.Kind != KindProcess {
			return nil, fmt.Errorf("noxy_ext.toml: concurrency \"concurrent\" is only valid for kind = \"process\"")
		}
	default:
		return nil, fmt.Errorf("noxy_ext.toml: invalid concurrency %q", m.Concurrency)
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
	if m.Kind == KindProcess {
		if err := m.validateProcessKeys(meta); err != nil {
			return nil, err
		}
		return &m, nil
	}
	if err := m.validateWasmKeys(meta); err != nil {
		return nil, err
	}
	return &m, nil
}

// validateProcessKeys aplica as regras de kind = "process" (spec §7): as
// chaves do wasm nao existem aqui, [binaries] e obrigatoria e cada asset e
// um nome de arquivo (vai parar em bin/), .exe no Windows.
func (m *Manifest) validateProcessKeys(meta toml.MetaData) error {
	if m.Wasm != "" {
		return fmt.Errorf("noxy_ext.toml: key \"wasm\" is not valid for kind = \"process\"")
	}
	if meta.IsDefined("memory_max_mb") {
		return fmt.Errorf("noxy_ext.toml: key \"memory_max_mb\" is not valid for kind = \"process\" (no sandbox, no cap)")
	}
	if len(m.Binaries) == 0 {
		return fmt.Errorf("noxy_ext.toml: kind = \"process\" requires a [binaries] table with at least one entry")
	}
	for key, asset := range m.Binaries {
		if !binaryKeyRE.MatchString(key) {
			return fmt.Errorf("noxy_ext.toml: invalid binaries key %q (want \"<goos>-<goarch>\")", key)
		}
		if !assetNameRE.MatchString(asset) {
			return fmt.Errorf("noxy_ext.toml: invalid asset name %q for %s (a file name, no path)", asset, key)
		}
		if strings.HasPrefix(key, "windows-") && !strings.HasSuffix(asset, ".exe") {
			return fmt.Errorf("noxy_ext.toml: windows asset %q must end in .exe", asset)
		}
	}
	for _, c := range m.Capabilities {
		if !capabilityRE.MatchString(c) {
			return fmt.Errorf("noxy_ext.toml: invalid capability name %q", c)
		}
	}
	if m.CallTimeoutMs != nil && *m.CallTimeoutMs < 0 {
		return fmt.Errorf("noxy_ext.toml: call_timeout_ms must not be negative")
	}
	if m.HandshakeTimeoutMs != nil && *m.HandshakeTimeoutMs < 0 {
		return fmt.Errorf("noxy_ext.toml: handshake_timeout_ms must not be negative")
	}
	if m.Restart && m.Concurrency != "stateless" {
		return fmt.Errorf("noxy_ext.toml: restart = true requires concurrency = \"stateless\" (handles would dangle)")
	}
	for _, exp := range m.Exports {
		if exp.TimeoutMs != nil && *exp.TimeoutMs < 0 {
			return fmt.Errorf("noxy_ext.toml: export %q: timeout_ms must not be negative", exp.Name)
		}
	}
	return nil
}

// validateWasmKeys mantem as regras do M1 e rejeita as chaves de processo.
func (m *Manifest) validateWasmKeys(meta toml.MetaData) error {
	if m.Binaries != nil {
		return fmt.Errorf("noxy_ext.toml: key \"binaries\" is only valid for kind = \"process\"")
	}
	if m.CallTimeoutMs != nil {
		return fmt.Errorf("noxy_ext.toml: key \"call_timeout_ms\" is only valid for kind = \"process\"")
	}
	if m.HandshakeTimeoutMs != nil {
		return fmt.Errorf("noxy_ext.toml: key \"handshake_timeout_ms\" is only valid for kind = \"process\"")
	}
	if meta.IsDefined("restart") {
		return fmt.Errorf("noxy_ext.toml: key \"restart\" is only valid for kind = \"process\"")
	}
	for _, exp := range m.Exports {
		if exp.TimeoutMs != nil {
			return fmt.Errorf("noxy_ext.toml: export %q: timeout_ms is only valid for kind = \"process\"", exp.Name)
		}
	}
	if m.Wasm == "" {
		m.Wasm = "ext.wasm"
	}
	if m.MemoryMaxMB == 0 {
		m.MemoryMaxMB = defaultMemoryMB
	}
	if m.MemoryMaxMB < 0 {
		return fmt.Errorf("noxy_ext.toml: memory_max_mb %d must not be negative", m.MemoryMaxMB)
	}
	if m.MemoryMaxMB > hostMemoryCeilingMB {
		return fmt.Errorf("noxy_ext.toml: memory_max_mb %d exceeds host ceiling %d", m.MemoryMaxMB, hostMemoryCeilingMB)
	}
	// M1 nao implementa capability nenhuma: aceitar a declaracao seria
	// prometer o que o host ignora (revisao do plano, item 6).
	if len(m.Capabilities) != 0 {
		return fmt.Errorf("noxy_ext.toml: capabilities are not supported in this phase (M1)")
	}
	return nil
}

// CallTimeout e o prazo do export (spec §4.3): timeout_ms do export, senao
// call_timeout_ms, senao 30 s. Zero = sem prazo.
func (m *Manifest) CallTimeout(export int) time.Duration {
	if export >= 0 && export < len(m.Exports) && m.Exports[export].TimeoutMs != nil {
		return time.Duration(*m.Exports[export].TimeoutMs) * time.Millisecond
	}
	if m.CallTimeoutMs != nil {
		return time.Duration(*m.CallTimeoutMs) * time.Millisecond
	}
	return defaultCallTimeoutMs * time.Millisecond
}

func (m *Manifest) HandshakeTimeout() time.Duration {
	if m.HandshakeTimeoutMs != nil {
		return time.Duration(*m.HandshakeTimeoutMs) * time.Millisecond
	}
	return defaultHandshakeTimeoutMs * time.Millisecond
}

func (m *Manifest) BinaryFor(goos, goarch string) (string, bool) {
	asset, ok := m.Binaries[goos+"-"+goarch]
	return asset, ok
}

// PublishedPlatforms lista "goos/goarch" em ordem, para mensagens de erro.
func (m *Manifest) PublishedPlatforms() []string {
	out := make([]string, 0, len(m.Binaries))
	for key := range m.Binaries {
		out = append(out, strings.Replace(key, "-", "/", 1))
	}
	sort.Strings(out)
	return out
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

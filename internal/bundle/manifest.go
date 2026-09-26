package bundle

import (
	"encoding/json"
	"fmt"
)

const (
	FormatVersion = 1
	KindSource    = "source"
	ManifestName  = "noxy-app.json"
	MarkerName    = ".noxy-app-ok"
)

type ModuleEntry struct {
	Name string `json:"name"`
	Path string `json:"path"`
	Dir  bool   `json:"dir,omitempty"`
}

type ExtensionEntry struct {
	Name     string `json:"name"`
	Kind     string `json:"kind"`
	Module   string `json:"module"`
	Dir      string `json:"dir"`
	Artifact string `json:"artifact"`
	SHA256   string `json:"sha256"`
}

// Manifest e o noxy-app.json do payload (spec §4.4).
type Manifest struct {
	Format     int              `json:"format"`
	Kind       string           `json:"kind"`
	Noxy       string           `json:"noxy"`
	Target     string           `json:"target"`
	Entry      string           `json:"entry"`
	Modules    []ModuleEntry    `json:"modules"`
	Extensions []ExtensionEntry `json:"extensions"`
	Includes   []string         `json:"includes"`
}

func (m *Manifest) Encode() ([]byte, error) {
	return json.MarshalIndent(m, "", "  ")
}

// DecodeManifest recusa formato desconhecido (um runtime antigo diante de
// um payload novo) e entry fora da raiz.
func DecodeManifest(data []byte) (*Manifest, error) {
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("%s: %w", ManifestName, err)
	}
	if m.Format != FormatVersion {
		return nil, fmt.Errorf("unsupported app payload format %d", m.Format)
	}
	if !ValidPath(m.Entry) {
		return nil, fmt.Errorf("%s: invalid entry %q", ManifestName, m.Entry)
	}
	return &m, nil
}

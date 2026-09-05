package pkgmanager

import (
	"path/filepath"
	"strings"
)

// SyncHint: so no caminho de erro, le <projectRoot>/noxy.mod e, se o modulo
// pedido e (ou esta sob) uma dependencia declarada, aponta o comando (spec
// §6). Compartilhado entre a VM (modulo nao encontrado em tempo de
// execucao) e o compilador (`use pkg select *` resolve em tempo de
// compilacao, antes de chegar ao resolveModule da VM).
func SyncHint(projectRoot, moduleName string) string {
	if projectRoot == "" {
		return ""
	}
	cfg, err := ParseModFile(filepath.Join(projectRoot, "noxy.mod"))
	if err != nil {
		return ""
	}
	for _, module := range cfg.Requires() {
		local := strings.ReplaceAll(LocalPath(module), "/", ".")
		if moduleName == local || strings.HasPrefix(moduleName, local+".") {
			return " (required by noxy.mod) — run 'noxy --sync'"
		}
	}
	return ""
}

// cmd/noxy/appmode.go — modo aplicacao (spec 2026-09-26 §3.3, §5): o binario
// carrega um payload NOXYAPP1 e NOXY_INTERPRETER nao esta definida.
package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/noxylang/noxy/internal/bundle"
	"github.com/noxylang/noxy/internal/modsrc"
	"github.com/noxylang/noxy/internal/vm"
)

// appModeExitCode roda o programa embutido e devolve (exit code, true).
// (0, false) = este binario e o noxy comum, ou NOXY_INTERPRETER esta
// definida: a CLI normal segue. Erro de leitura do proprio executavel conta
// como "sem payload"; trailer inconsistente e fatal.
func appModeExitCode() (int, bool) {
	if os.Getenv("NOXY_INTERPRETER") != "" {
		return 0, false
	}
	exe, err := os.Executable()
	if err != nil {
		return 0, false
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	payload, err := bundle.Open(exe)
	if err != nil {
		if errors.Is(err, bundle.ErrInconsistentTrailer) {
			fmt.Fprintf(diagOut, "Error: %s\n", err)
			return 1, true
		}
		return 0, false
	}
	if payload == nil {
		return 0, false
	}
	base, err := bundle.CacheBase()
	if err != nil {
		payload.Close()
		fmt.Fprintf(diagOut, "Error: %s\n", err)
		return 1, true
	}
	appDir, manifest, err := bundle.Extract(payload, base)
	payload.Close()
	if err != nil {
		fmt.Fprintf(diagOut, "Error: %s\n", err)
		return 1, true
	}
	entry := filepath.Join(appDir, filepath.FromSlash(manifest.Entry))
	content, ok := loadScript(entry)
	if !ok {
		return 1, true
	}
	// Exatamente como `noxy <entry> <args>`: argv[1] e o entry extraido,
	// cwd fica o do usuario.
	os.Args = append([]string{os.Args[0], entry}, os.Args[1:]...)
	rootPath := filepath.Dir(entry)
	// Nunca sobe acima de <appdir> (FindRoot acharia um noxy.mod alheio
	// acima do cache, cujo noxy_libs sombrearia o app): o projeto e <appdir>
	// quando o build embutiu um noxy.mod, senao nao ha projeto (spec §5.3).
	projectRoot := ""
	if info, err := os.Stat(filepath.Join(appDir, "noxy.mod")); err == nil && !info.IsDir() {
		projectRoot = appDir
	}
	cfg := vm.VMConfig{RootPath: rootPath, ProjectRoot: projectRoot, Source: modsrc.NewSealed(rootPath, projectRoot)}
	return runWithVMConfig(entry, content, cfg, false), true
}

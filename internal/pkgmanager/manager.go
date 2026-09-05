package pkgmanager

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"github.com/estevaofon/noxy/internal/ext"
	"github.com/estevaofon/noxy/internal/version"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const NoxyLibsDir = "noxy_libs"

// Get e "adicionar ou atualizar" (spec §5.4): resolve a versao, grava a
// linha require no noxy.mod da raiz e delega ao sync. Nao toca o lock: se a
// versao ja e a do lock, o hash existente vale e um download divergente e
// erro; so versao diferente troca as linhas do modulo, dentro do sync.
func Get(pkgArg string) error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	root, ok := FindRoot(cwd)
	if !ok {
		root = cwd
	}
	module, ref := splitPackageArg(pkgArg)
	if err := ValidateModulePath(module); err != nil {
		return err
	}
	modPath := filepath.Join(root, "noxy.mod")
	var cfg *ModuleConfig
	if _, err := os.Stat(modPath); os.IsNotExist(err) {
		cfg = NewModuleConfig()
		cfg.Module = filepath.Base(root)
	} else if cfg, err = ParseModFile(modPath); err != nil {
		return err
	}
	libs := filepath.Join(root, NoxyLibsDir)
	cleanStaleTemps(libs)
	f := newFetcher(libs, os.Stdout)
	defer f.cleanup()
	fmt.Printf("Getting package %s...\n", pkgArg)
	resolved, err := f.resolve(module, ref)
	if err != nil {
		return err
	}
	if resolved != ref {
		fmt.Printf("Resolved %s to %s\n", module, resolved)
	}
	cfg.NoxyVersion = version.Version
	cfg.Require[module] = resolved
	if err := cfg.Save(modPath); err != nil {
		return fmt.Errorf("failed to update noxy.mod: %w", err)
	}
	return syncWith(root, SyncOptions{Out: os.Stdout}, f)
}

func splitPackageArg(pkgArg string) (repoURL, version string) {
	parts := strings.SplitN(pkgArg, "@", 2)
	if len(parts) == 2 && parts[1] != "" {
		return parts[0], parts[1]
	}
	return parts[0], "HEAD"
}

// readManifest devolve (nil, nil, nil) quando o pacote nao e uma extensao;
// um manifesto presente mas invalido falha o --get — binarios dependem
// dele, e um typo nao pode virar "pacote sem extensao" em silencio.
func readManifest(dir string) (*ext.Manifest, []byte, error) {
	data, err := os.ReadFile(filepath.Join(dir, "noxy_ext.toml"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil, nil
		}
		return nil, nil, err
	}
	manifest, err := ext.ParseManifest(data)
	if err != nil {
		return nil, nil, err
	}
	return manifest, data, nil
}

// Os bytes do pacote sao os do repositorio em qualquer maquina: sem isso,
// core.autocrlf=true (default do git no Windows) reescreve noxy_ext.toml
// em CRLF e o hash gravado no noxy.sum deixa de valer para o colega no
// Linux (spec §8.1, lockfile portavel).
func gitClone(url, dir string) error {
	cmd := exec.Command("git", "-c", "core.autocrlf=false", "-c", "core.eol=lf", "clone", url, dir)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func gitCheckout(dir, version string) error {
	if _, err := os.Stat(filepath.Join(dir, ".git")); os.IsNotExist(err) {
		return nil
	}
	cmd := exec.Command("git", "-c", "core.autocrlf=false", "-c", "core.eol=lf", "-C", dir, "checkout", version)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// RecordExtensionSums grava manifesto + .wasm de uma extensao wasm em
// targetDir sob a chave (module, version). Exportada para o teste de
// integracao da VM exercitar o mesmo escritor do --sync.
func RecordExtensionSums(root, targetDir, module, version string) error {
	manifestData, err := os.ReadFile(filepath.Join(targetDir, "noxy_ext.toml"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	manifest, err := ext.ParseManifest(manifestData)
	if err != nil {
		return nil
	}
	sums, err := ParseSumFile(SumFilePath(root))
	if err != nil {
		return err
	}
	sums.SetArtifact(module, version, "noxy_ext.toml", sha256Hex(manifestData))
	wasmData, err := os.ReadFile(filepath.Join(targetDir, manifest.Wasm))
	if err != nil {
		return err
	}
	sums.SetArtifact(module, version, manifest.Wasm, sha256Hex(wasmData))
	return sums.Save(SumFilePath(root))
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

package pkgmanager

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"noxy-vm/internal/ext"
	"noxy-vm/internal/version"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

const NoxyLibsDir = "noxy_libs"

func Get(pkgArg string) error {
	visited := make(map[string]bool)
	return downloadPackage(pkgArg, true, visited)
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

func manifestKindAt(dir string) string {
	manifest, _, err := readManifest(dir)
	if err != nil || manifest == nil {
		return ""
	}
	return manifest.Kind
}

func downloadPackage(pkgArg string, isRoot bool, visited map[string]bool) error {
	repoURL, version := splitPackageArg(pkgArg)
	cacheKey := repoURL + "@" + version
	if visited[cacheKey] {
		return nil
	}
	visited[cacheKey] = true

	localPath := LocalPath(repoURL)
	targetDir := filepath.Join(NoxyLibsDir, filepath.FromSlash(localPath))
	if isRoot {
		fmt.Printf("Getting package %s...\n", pkgArg)
	} else {
		fmt.Printf("Getting dependency %s...\n", pkgArg)
	}

	// Clone fresco num diretorio temporario irmao; o destino so e tocado no
	// fim (spec §8.1): o antigo "existe → git pull" nao atualizava nada
	// depois de remover o .git, e com binarios em disco um diretorio velho
	// guardaria um asset velho.
	if err := os.MkdirAll(filepath.Dir(targetDir), 0o755); err != nil {
		return err
	}
	tmpDir, err := os.MkdirTemp(filepath.Dir(targetDir), ".get-"+filepath.Base(targetDir)+"-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpDir)
	if err := gitClone(gitURLFor(repoURL), tmpDir); err != nil {
		return fmt.Errorf("failed to clone package: %w", err)
	}

	// Sem versao, uma extensao por processo precisa de uma tag: os assets
	// pendem de uma release (spec §8.1, passo 2).
	resolved := version
	if version == "HEAD" && manifestKindAt(tmpDir) == ext.KindProcess {
		tag, err := resolveNewestTag(gitURLFor(repoURL))
		if err != nil {
			return fmt.Errorf("%s: %w", repoURL, err)
		}
		fmt.Printf("Resolved %s to %s\n", repoURL, tag)
		resolved = tag
	}
	if resolved != "HEAD" {
		if err := gitCheckout(tmpDir, resolved); err != nil {
			return fmt.Errorf("failed to checkout version %s: %w", resolved, err)
		}
	}
	if err := os.RemoveAll(filepath.Join(tmpDir, ".git")); err != nil {
		fmt.Printf("Warning: failed to remove .git directory: %s\n", err)
	}

	manifest, manifestData, err := readManifest(tmpDir)
	if err != nil {
		return err
	}
	var binarySums map[string]string
	if manifest != nil && manifest.Kind == ext.KindProcess {
		base, err := releaseBaseURL(repoURL, resolved)
		if err != nil {
			return err
		}
		binarySums, err = fetchProcessBinaries(httpClient, base, manifest, tmpDir, runtime.GOOS, runtime.GOARCH, os.Stdout)
		if err != nil {
			return fmt.Errorf("%s@%s: %w", repoURL, resolved, err)
		}
	}

	if err := os.RemoveAll(targetDir); err != nil {
		return err
	}
	if err := os.Rename(tmpDir, targetDir); err != nil {
		return fmt.Errorf("failed to install package: %w", err)
	}

	if manifest != nil {
		sumVersion := resolved
		if sumVersion == HeadVersion { // provisorio ate a Task 9
			sumVersion = "v0.0.0"
		}
		var sumErr error
		if manifest.Kind == ext.KindProcess {
			sumErr = recordProcessSums(".", repoURL, sumVersion, manifestData, binarySums)
		} else {
			sumErr = RecordExtensionSums(".", targetDir, repoURL, sumVersion)
		}
		if sumErr != nil {
			fmt.Printf("Warning: failed to record noxy.sum entries: %s\n", sumErr)
		}
		if len(manifest.Capabilities) != 0 {
			fmt.Printf("%s declares: %s\n", manifest.Name, strings.Join(manifest.Capabilities, ", "))
		}
	}

	if isRoot {
		if err := updateModFile(repoURL, resolved); err != nil {
			fmt.Printf("Warning: failed to update noxy.mod: %s\n", err)
		}
	}

	pkgModPath := filepath.Join(targetDir, "noxy.mod")
	if _, err := os.Stat(pkgModPath); err == nil {
		config, err := ParseModFile(pkgModPath)
		if err != nil {
			fmt.Printf("Warning: failed to parse %s: %s\n", pkgModPath, err)
		} else {
			for depPkg, depVer := range config.Require {
				depArg := depPkg
				if depVer != "" {
					depArg = depPkg + "@" + depVer
				}
				if err := downloadPackage(depArg, false, visited); err != nil {
					fmt.Printf("Warning: failed to download dependency %s: %s\n", depArg, err)
				}
			}
		}
	}

	if isRoot {
		fmt.Println("Done.")
	}
	return nil
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

func updateModFile(pkg, pkgVersion string) error {
	modPath := "noxy.mod"
	var config *ModuleConfig

	if _, err := os.Stat(modPath); os.IsNotExist(err) {
		// Create new
		config = NewModuleConfig()
		cwd, _ := os.Getwd()
		config.Module = filepath.Base(cwd)
	} else {
		var err error
		config, err = ParseModFile(modPath)
		if err != nil {
			return err
		}
	}

	config.NoxyVersion = version.Version
	config.Require[pkg] = pkgVersion
	return config.Save(modPath)
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

// recordProcessSums grava manifesto + bin/<asset> de TODAS as plataformas
// publicadas (spec §8.1, passo 6).
func recordProcessSums(root, module, version string, manifestData []byte, binaries map[string]string) error {
	sums, err := ParseSumFile(SumFilePath(root))
	if err != nil {
		return err
	}
	sums.SetArtifact(module, version, "noxy_ext.toml", sha256Hex(manifestData))
	for asset, digest := range binaries {
		sums.SetArtifact(module, version, "bin/"+asset, digest)
	}
	return sums.Save(SumFilePath(root))
}

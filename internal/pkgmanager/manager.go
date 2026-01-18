package pkgmanager

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const NoxyLibsDir = "noxy_libs"

func Get(pkgArg string) error {
	// 1. Parse argument: github.com/user/repo@version
	parts := strings.Split(pkgArg, "@")
	repoURL := parts[0] // e.g., github.com/user/repo
	version := "HEAD"
	if len(parts) > 1 {
		version = parts[1]
	}

	// Ensure we have a valid URL (assume https for now if no scheme)
	gitURL := repoURL
	if !strings.HasPrefix(gitURL, "http") && !strings.HasPrefix(gitURL, "git@") {
		gitURL = "https://" + gitURL
	}

	// 2. Prepare target directory
	// Store in noxy_libs/<domain>/<user>/<repo>
	// Replace dots in domain with underscores (e.g. github.com -> github_com)
	parts = strings.Split(repoURL, "/")
	if len(parts) > 0 {
		parts[0] = strings.ReplaceAll(parts[0], ".", "_")
	}
	localPath := strings.Join(parts, "/")
	targetDir := filepath.Join(NoxyLibsDir, filepath.FromSlash(localPath))

	fmt.Printf("Getting package %s...\n", pkgArg)

	// Check if already exists
	if _, err := os.Stat(targetDir); !os.IsNotExist(err) {
		fmt.Printf("Updating existing package in %s...\n", targetDir)
		// It exists, try to pull or checkout
		// Ideally we should check if it's a git repo
		if err := gitPull(targetDir); err != nil {
			return fmt.Errorf("failed to update package: %w", err)
		}
	} else {
		// Clone it
		fmt.Printf("Cloning into %s...\n", targetDir)
		if err := gitClone(gitURL, targetDir); err != nil {
			return fmt.Errorf("failed to clone package: %w", err)
		}
	}

	// 3. Checkout version
	if version != "HEAD" {
		fmt.Printf("Checking out version %s...\n", version)
		if err := gitCheckout(targetDir, version); err != nil {
			return fmt.Errorf("failed to checkout version %s: %w", version, err)
		}
	}

	// 4. Remove .git directory to avoid nested repo issues
	if err := os.RemoveAll(filepath.Join(targetDir, ".git")); err != nil {
		fmt.Printf("Warning: failed to remove .git directory: %s\n", err)
	}

	// 5. Update noxy.mod
	if err := updateModFile(repoURL, version); err != nil {
		fmt.Printf("Warning: failed to update noxy.mod: %s\n", err)
	}

	fmt.Println("Done.")
	return nil
}

func gitClone(url, dir string) error {
	cmd := exec.Command("git", "clone", url, dir)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func gitPull(dir string) error {
	cmd := exec.Command("git", "-C", dir, "pull")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func gitCheckout(dir, version string) error {
	cmd := exec.Command("git", "-C", dir, "checkout", version)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func updateModFile(pkg, version string) error {
	modPath := "noxy.mod"
	var config *ModuleConfig

	if _, err := os.Stat(modPath); os.IsNotExist(err) {
		// Create new
		config = NewModuleConfig()
		// Try to guess module name from current dir or just leave blank?
		// We'll leave module name blank or default to something if needed.
		cwd, _ := os.Getwd()
		config.Module = filepath.Base(cwd)
	} else {
		var err error
		config, err = ParseModFile(modPath)
		if err != nil {
			return err
		}
	}

	config.Require[pkg] = version
	return config.Save(modPath)
}

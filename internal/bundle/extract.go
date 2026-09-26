package bundle

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
)

var ErrCorrupted = errors.New("app payload is corrupted: sha256 mismatch")

var errInvalidPayloadPath = errors.New("app payload has an invalid path")

// CacheBase: NOXY_APP_CACHE ou <UserCacheDir>/noxy/apps (spec §3.3, §5.2).
func CacheBase() (string, error) {
	if dir := os.Getenv("NOXY_APP_CACHE"); dir != "" {
		return dir, nil
	}
	base, err := os.UserCacheDir()
	if err != nil {
		return "", errors.New("cannot determine the user cache directory; set NOXY_APP_CACHE")
	}
	return filepath.Join(base, "noxy", "apps"), nil
}

// Extract garante <base>/<CacheKey> extraido e devolve o diretorio e o
// manifesto. Com o marcador presente nao le o payload nem confere hash
// (decisao da spec §5.2). Sem marcador: hash, zip para um temporario ao
// lado, marcador, rename — outra instancia que vencer a corrida e aceita;
// um <appdir> sem marcador e velho e e substituido (spec §5.2).
func Extract(p *Payload, base string) (string, *Manifest, error) {
	appDir := filepath.Join(base, p.CacheKey())
	if m, err := readExtracted(appDir); err == nil {
		return appDir, m, nil
	}
	data, err := io.ReadAll(p.Reader())
	if err != nil {
		return "", nil, fmt.Errorf("app payload: %w", err)
	}
	if sha256.Sum256(data) != p.Trailer.SHA256 {
		return "", nil, ErrCorrupted
	}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", nil, fmt.Errorf("app payload: %w", err)
	}

	// Validate manifest before creating any directories on disk
	var manifestData []byte
	for _, f := range zr.File {
		if f.Name == ManifestName {
			rc, err := f.Open()
			if err != nil {
				return "", nil, fmt.Errorf("app payload: %w", err)
			}
			manifestData, err = io.ReadAll(rc)
			rc.Close()
			if err != nil {
				return "", nil, fmt.Errorf("app payload: %w", err)
			}
			break
		}
	}
	if manifestData == nil {
		return "", nil, fmt.Errorf("app payload: %s missing", ManifestName)
	}
	m, err := DecodeManifest(manifestData)
	if err != nil {
		return "", nil, err
	}

	if err := os.MkdirAll(base, 0o755); err != nil {
		return "", nil, fmt.Errorf("cannot extract app payload to %s: %w", appDir, err)
	}
	tmp, err := os.MkdirTemp(base, p.CacheKey()+".tmp-"+strconv.Itoa(os.Getpid())+"-")
	if err != nil {
		return "", nil, fmt.Errorf("cannot extract app payload to %s: %w", appDir, err)
	}
	if err := unzipTo(zr, tmp); err != nil {
		os.RemoveAll(tmp)
		if errors.Is(err, errInvalidPayloadPath) {
			return "", nil, err
		}
		return "", nil, fmt.Errorf("cannot extract app payload to %s: %w", appDir, err)
	}
	marker := hex.EncodeToString(p.Trailer.SHA256[:]) + "\n"
	if err := os.WriteFile(filepath.Join(tmp, MarkerName), []byte(marker), 0o644); err != nil {
		os.RemoveAll(tmp)
		return "", nil, fmt.Errorf("cannot extract app payload to %s: %w", appDir, err)
	}
	if err := os.Rename(tmp, appDir); err != nil {
		// <appdir> so nasce do rename de um temporario que ja tem o marcador:
		// sem marcador e resto de algo interrompido (ou plantado), nunca uma
		// extracao em andamento. Remove e tenta o rename uma vez mais.
		if _, statErr := os.Stat(filepath.Join(appDir, MarkerName)); os.IsNotExist(statErr) {
			if os.RemoveAll(appDir) == nil {
				err = os.Rename(tmp, appDir)
			}
		}
		if err != nil {
			os.RemoveAll(tmp)
			if m, readErr := readExtracted(appDir); readErr == nil {
				return appDir, m, nil
			}
			return "", nil, fmt.Errorf("cannot extract app payload to %s: %w", appDir, err)
		}
	}
	m, err = readExtracted(appDir)
	if err != nil {
		os.RemoveAll(appDir)
		return "", nil, fmt.Errorf("cannot extract app payload to %s: %w", appDir, err)
	}
	return appDir, m, nil
}

func readExtracted(appDir string) (*Manifest, error) {
	if _, err := os.Stat(filepath.Join(appDir, MarkerName)); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(filepath.Join(appDir, ManifestName))
	if err != nil {
		return nil, err
	}
	return DecodeManifest(data)
}

func unzipTo(zr *zip.Reader, dir string) error {
	for _, f := range zr.File {
		if !ValidPath(f.Name) || f.Name == MarkerName {
			return fmt.Errorf("%w: %s", errInvalidPayloadPath, f.Name)
		}
		dest := filepath.Join(dir, filepath.FromSlash(f.Name))
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(dest, 0o755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return err
		}
		mode := os.FileMode(0o644)
		if f.Mode()&0o111 != 0 {
			mode = 0o755
		}
		if err := writeEntry(f, dest, mode); err != nil {
			return err
		}
	}
	return nil
}

func writeEntry(f *zip.File, dest string, mode os.FileMode) error {
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	out, err := os.OpenFile(dest, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, rc); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

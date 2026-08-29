package pkgmanager

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"noxy-vm/internal/ext"
)

const fetchManifest = `
name = "guest"
abi = 1
kind = "process"

[binaries]
linux-amd64 = "guest-linux-amd64"
windows-amd64 = "guest-windows-amd64.exe"

[[export]]
name = "guest_noop"
params = []
returns = "void"
`

func hexSum(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// serveRelease publica files em <url>/rel/<nome>.
func serveRelease(t *testing.T, files map[string][]byte) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, ok := files[strings.TrimPrefix(r.URL.Path, "/rel/")]
		if !ok {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(data)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func releaseFiles(linux, windows []byte) map[string][]byte {
	checksums := hexSum(linux) + "  guest-linux-amd64\n" + hexSum(windows) + "  guest-windows-amd64.exe\n"
	return map[string][]byte{
		"guest-linux-amd64":       linux,
		"guest-windows-amd64.exe": windows,
		"checksums.txt":           []byte(checksums),
	}
}

func fetchManifestParsed(t *testing.T) *ext.Manifest {
	t.Helper()
	m, err := ext.ParseManifest([]byte(fetchManifest))
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestFetchProcessBinariesDownloadsOnlyThePlatformAsset(t *testing.T) {
	linux, windows := []byte("linux bits"), []byte("windows bits")
	srv := serveRelease(t, releaseFiles(linux, windows))
	dir := t.TempDir()
	sums, err := fetchProcessBinaries(srv.Client(), srv.URL+"/rel/", fetchManifestParsed(t), dir, "linux", "amd64", io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if sums["guest-linux-amd64"] != hexSum(linux) || sums["guest-windows-amd64.exe"] != hexSum(windows) {
		t.Fatalf("all published hashes must be returned: %v", sums)
	}
	got, err := os.ReadFile(filepath.Join(dir, "bin", "guest-linux-amd64"))
	if err != nil || string(got) != "linux bits" {
		t.Fatalf("asset on disk: %q %v", got, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "bin", "guest-windows-amd64.exe")); !os.IsNotExist(err) {
		t.Fatal("only the platform asset is downloaded")
	}
	if runtime.GOOS != "windows" {
		info, _ := os.Stat(filepath.Join(dir, "bin", "guest-linux-amd64"))
		if info.Mode()&0o111 == 0 {
			t.Fatal("POSIX asset must be executable")
		}
	}
}

func TestFetchProcessBinariesErrors(t *testing.T) {
	linux, windows := []byte("linux bits"), []byte("windows bits")
	m := fetchManifestParsed(t)

	srv := serveRelease(t, releaseFiles(linux, windows))
	_, err := fetchProcessBinaries(srv.Client(), srv.URL+"/rel/", m, t.TempDir(), "darwin", "arm64", io.Discard)
	if err == nil || !strings.Contains(err.Error(), "no binary for darwin/arm64") || !strings.Contains(err.Error(), "linux/amd64") {
		t.Fatalf("unpublished platform: %v", err)
	}

	files := releaseFiles(linux, windows)
	files["checksums.txt"] = []byte(hexSum(linux) + "  guest-linux-amd64\n")
	srv2 := serveRelease(t, files)
	_, err = fetchProcessBinaries(srv2.Client(), srv2.URL+"/rel/", m, t.TempDir(), "linux", "amd64", io.Discard)
	if err == nil || !strings.Contains(err.Error(), "does not list \"guest-windows-amd64.exe\"") {
		t.Fatalf("asset missing from checksums: %v", err)
	}

	files = releaseFiles(linux, windows)
	files["guest-linux-amd64"] = []byte("tampered")
	srv3 := serveRelease(t, files)
	dir := t.TempDir()
	_, err = fetchProcessBinaries(srv3.Client(), srv3.URL+"/rel/", m, dir, "linux", "amd64", io.Discard)
	if err == nil || !strings.Contains(err.Error(), "mismatch") {
		t.Fatalf("corrupted asset: %v", err)
	}
	entries, _ := os.ReadDir(filepath.Join(dir, "bin"))
	if len(entries) != 0 {
		t.Fatalf("a corrupted download leaves nothing behind, found %v", entries)
	}

	files = releaseFiles(linux, windows)
	delete(files, "guest-linux-amd64")
	srv4 := serveRelease(t, files)
	_, err = fetchProcessBinaries(srv4.Client(), srv4.URL+"/rel/", m, t.TempDir(), "linux", "amd64", io.Discard)
	if err == nil || !strings.Contains(err.Error(), "404") {
		t.Fatalf("missing asset names the status: %v", err)
	}
}

func TestRecordProcessSums(t *testing.T) {
	root := t.TempDir()
	err := recordProcessSums(root, "github_com/acme/guest", []byte(fetchManifest), map[string]string{
		"guest-linux-amd64":       strings.Repeat("a", 64),
		"guest-windows-amd64.exe": strings.Repeat("b", 64),
	})
	if err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(SumFilePath(root))
	for _, want := range []string{
		"github_com/acme/guest noxy_ext.toml sha256:" + hexSum([]byte(fetchManifest)),
		"github_com/acme/guest bin/guest-linux-amd64 sha256:" + strings.Repeat("a", 64),
		"github_com/acme/guest bin/guest-windows-amd64.exe sha256:" + strings.Repeat("b", 64),
	} {
		if !strings.Contains(string(data), want) {
			t.Fatalf("noxy.sum missing %q:\n%s", want, data)
		}
	}
}

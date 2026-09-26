// internal/bundle/pack_test.go
package bundle

import (
	"archive/zip"
	"bytes"
	"strings"
	"testing"
)

func sampleManifest() *Manifest {
	return &Manifest{Format: FormatVersion, Kind: KindSource, Noxy: "v0.26.0", Target: "linux/amd64", Entry: "main.nx",
		Modules: []ModuleEntry{{Name: "src.a", Path: "src/a.nx"}}, Extensions: []ExtensionEntry{}, Includes: []string{"web"}}
}

func TestManifestRoundTripAndFormatCheck(t *testing.T) {
	data, err := sampleManifest().Encode()
	if err != nil {
		t.Fatal(err)
	}
	m, err := DecodeManifest(data)
	if err != nil || m.Entry != "main.nx" || m.Modules[0].Name != "src.a" || m.Includes[0] != "web" {
		t.Fatalf("decode: %+v %v", m, err)
	}
	if _, err := DecodeManifest([]byte(`{"format": 2, "entry": "main.nx"}`)); err == nil || !strings.Contains(err.Error(), "unsupported app payload format 2") {
		t.Fatalf("format 2: %v", err)
	}
	if _, err := DecodeManifest([]byte(`{"format": 1, "entry": "../x.nx"}`)); err == nil {
		t.Fatal("entry escaping the root must be rejected")
	}
}

func TestValidPath(t *testing.T) {
	for _, ok := range []string{"main.nx", "src/a.nx", "noxy_libs/github_com/x/y/bin/z", "web/vendor/x.js"} {
		if !ValidPath(ok) {
			t.Errorf("%q must be valid", ok)
		}
	}
	for _, bad := range []string{"", "/abs", "C:/x", "a\\b", "../x", "a/../b", "./a", "a/", "a//b"} {
		if ValidPath(bad) {
			t.Errorf("%q must be invalid", bad)
		}
	}
}

func TestPackIsDeterministicSortedAndCarriesTheManifest(t *testing.T) {
	files := []File{{Path: "src/a.nx", Data: []byte("func a() -> int\n    return 1\nend\n")}, {Path: "bin/tool", Data: []byte("BIN"), Executable: true}, {Path: "main.nx", Data: []byte("print(1)\n")}}
	one, err := Pack(sampleManifest(), files)
	if err != nil {
		t.Fatal(err)
	}
	two, err := Pack(sampleManifest(), files)
	if err != nil || !bytes.Equal(one, two) {
		t.Fatalf("two packs of the same input differ (err %v)", err)
	}
	zr, err := zip.NewReader(bytes.NewReader(one), int64(len(one)))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, f := range zr.File {
		names = append(names, f.Name)
		if f.Method != zip.Deflate {
			t.Errorf("%s: method %d, want deflate", f.Name, f.Method)
		}
		if !f.Modified.IsZero() && f.Modified.Year() > 1980 {
			t.Errorf("%s: timestamp %v must be zeroed", f.Name, f.Modified)
		}
	}
	want := []string{"bin/tool", "main.nx", ManifestName, "src/a.nx"}
	if strings.Join(names, ",") != strings.Join(want, ",") {
		t.Fatalf("entries %v, want %v", names, want)
	}
	if zr.File[0].Mode()&0o111 == 0 {
		t.Fatal("bin/tool must be executable in the zip")
	}
	if zr.File[1].Mode()&0o111 != 0 {
		t.Fatal("main.nx must not be executable")
	}
}

func TestPackRejectsInvalidAndDuplicatePaths(t *testing.T) {
	if _, err := Pack(sampleManifest(), []File{{Path: "../x", Data: nil}}); err == nil || !strings.Contains(err.Error(), "invalid payload path") {
		t.Fatalf("invalid: %v", err)
	}
	if _, err := Pack(sampleManifest(), []File{{Path: "a.nx"}, {Path: "a.nx"}}); err == nil || !strings.Contains(err.Error(), "duplicate payload path") {
		t.Fatalf("duplicate: %v", err)
	}
	if _, err := Pack(sampleManifest(), []File{{Path: ManifestName}}); err == nil {
		t.Fatal("a file named noxy-app.json must collide with the manifest")
	}
}

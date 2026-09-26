package bundle

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
)

// writeApp grava runtime + payload + trailer(hash real do payload) e devolve o caminho.
func writeApp(t *testing.T, runtime, payload []byte) string {
	t.Helper()
	sum := sha256.Sum256(payload)
	tr := Trailer{SHA256: sum, Offset: uint64(len(runtime)), Size: uint64(len(payload))}
	path := filepath.Join(t.TempDir(), "app")
	data := append(append(append([]byte{}, runtime...), payload...), tr.Marshal()...)
	if err := os.WriteFile(path, data, 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestTrailerRoundTrip(t *testing.T) {
	var sum [32]byte
	for i := range sum {
		sum[i] = byte(i)
	}
	in := Trailer{SHA256: sum, Offset: 12345, Size: 678}
	b := in.Marshal()
	if len(b) != TrailerSize || string(b[56:]) != Magic {
		t.Fatalf("marshal: len %d, tail %q", len(b), b[56:])
	}
	for _, zero := range b[48:56] {
		if zero != 0 {
			t.Fatalf("reserved bytes must be zero: %v", b[48:56])
		}
	}
	out, ok := ParseTrailer(b)
	if !ok || out != in {
		t.Fatalf("round trip: ok=%v %+v", ok, out)
	}
	if _, ok := ParseTrailer(b[:63]); ok {
		t.Fatal("short trailer must not parse")
	}
	b[63] = 'X'
	if _, ok := ParseTrailer(b); ok {
		t.Fatal("wrong magic must not parse")
	}
}

func TestOpenWithoutPayloadIsNotAnError(t *testing.T) {
	for _, content := range []string{"abc", string(bytes.Repeat([]byte("plain noxy bytes "), 10))} {
		path := filepath.Join(t.TempDir(), "noxy")
		if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
			t.Fatal(err)
		}
		p, err := Open(path)
		if err != nil || p != nil {
			t.Fatalf("plain file: payload=%v err=%v", p, err)
		}
	}
}

func TestOpenRejectsInconsistentTrailer(t *testing.T) {
	runtime := bytes.Repeat([]byte("R"), 100)
	payload := []byte("PAYLOAD")
	tr := Trailer{Offset: 100, Size: 999}
	path := filepath.Join(t.TempDir(), "app")
	data := append(append(append([]byte{}, runtime...), payload...), tr.Marshal()...)
	if err := os.WriteFile(path, data, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(path); !errors.Is(err, ErrInconsistentTrailer) {
		t.Fatalf("want ErrInconsistentTrailer, got %v", err)
	}
	if _, err := RuntimeBytes(path); !errors.Is(err, ErrInconsistentTrailer) {
		t.Fatalf("RuntimeBytes: want ErrInconsistentTrailer, got %v", err)
	}
}

func TestOpenReadsPayloadSectionAndCacheKey(t *testing.T) {
	runtime := bytes.Repeat([]byte("R"), 100)
	payload := []byte("ZIPDATA")
	path := writeApp(t, runtime, payload)
	p, err := Open(path)
	if err != nil || p == nil {
		t.Fatalf("open: %v %v", p, err)
	}
	defer p.Close()
	got, err := io.ReadAll(p.Reader())
	if err != nil || string(got) != "ZIPDATA" {
		t.Fatalf("section: %q %v", got, err)
	}
	sum := sha256.Sum256(payload)
	if p.CacheKey() != hex.EncodeToString(sum[:8]) {
		t.Fatalf("cache key %q", p.CacheKey())
	}
	if p.Trailer.Offset != 100 || p.Trailer.Size != 7 {
		t.Fatalf("trailer %+v", p.Trailer)
	}
}

func TestRuntimeBytesStopAtPayload(t *testing.T) {
	runtime := []byte("RUNTIME-BYTES")
	app := writeApp(t, runtime, []byte("payload"))
	got, err := RuntimeBytes(app)
	if err != nil || !bytes.Equal(got, runtime) {
		t.Fatalf("app: %q %v", got, err)
	}
	plain := filepath.Join(t.TempDir(), "noxy")
	if err := os.WriteFile(plain, runtime, 0o755); err != nil {
		t.Fatal(err)
	}
	got, err = RuntimeBytes(plain)
	if err != nil || !bytes.Equal(got, runtime) {
		t.Fatalf("plain: %q %v", got, err)
	}
}

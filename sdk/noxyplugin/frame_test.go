package noxyplugin

import (
	"bytes"
	"errors"
	"io"
	"testing"
)

func TestFrameLayoutMatchesHost(t *testing.T) {
	var buf bytes.Buffer
	if err := writeFrame(&buf, frame{Kind: kindCall, ID: 7, Fn: 2, Body: []byte{0x00}}); err != nil {
		t.Fatal(err)
	}
	want := []byte{0x0d, 0, 0, 0, 0x02, 0, 0, 0, 7, 0, 0, 0, 2, 0, 0, 0, 0}
	if !bytes.Equal(buf.Bytes(), want) {
		t.Fatalf("layout:\n got % x\nwant % x", buf.Bytes(), want)
	}
	f, err := readFrame(&buf, 1<<20)
	if err != nil || f.Kind != kindCall || f.ID != 7 || f.Fn != 2 || len(f.Body) != 1 {
		t.Fatalf("round trip: %#v %v", f, err)
	}
}

func TestFrameReadErrors(t *testing.T) {
	if _, err := readFrame(bytes.NewReader(nil), 1<<20); !errors.Is(err, io.EOF) {
		t.Fatalf("clean EOF: %v", err)
	}
	if _, err := readFrame(bytes.NewReader([]byte{0x0b, 0, 0, 0}), 1<<20); err == nil || errors.Is(err, io.EOF) {
		t.Fatalf("length below header must be an error, got %v", err)
	}
	if _, err := readFrame(bytes.NewReader([]byte{0x0c, 0, 0, 0, 0x09, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}), 1<<20); err == nil {
		t.Fatal("unknown kind must be an error")
	}
}

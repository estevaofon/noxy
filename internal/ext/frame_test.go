package ext

import (
	"bytes"
	"errors"
	"io"
	"testing"
)

func TestFrameRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	in := Frame{Kind: FrameCall, ID: 7, Fn: 2, Body: []byte{0x00}}
	if err := WriteFrame(&buf, in); err != nil {
		t.Fatal(err)
	}
	// length = 12 (cabecalho) + 1 (corpo); tudo LE (spec §2.2)
	want := []byte{
		0x0d, 0x00, 0x00, 0x00, // length
		0x02, 0x00, 0x00, 0x00, // kind, flags, reserved
		0x07, 0x00, 0x00, 0x00, // id
		0x02, 0x00, 0x00, 0x00, // fn
		0x00, // body
	}
	if !bytes.Equal(buf.Bytes(), want) {
		t.Fatalf("layout:\n got % x\nwant % x", buf.Bytes(), want)
	}
	out, err := ReadFrame(&buf, DefaultLimits().MaxBytes)
	if err != nil {
		t.Fatal(err)
	}
	if out.Kind != FrameCall || out.ID != 7 || out.Fn != 2 || !bytes.Equal(out.Body, []byte{0x00}) {
		t.Fatalf("round trip: %#v", out)
	}
}

func TestFrameEmptyBody(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteFrame(&buf, Frame{Kind: FrameCancel, ID: 3}); err != nil {
		t.Fatal(err)
	}
	if buf.Len() != 16 {
		t.Fatalf("CANCEL is header only (length 12), got %d bytes", buf.Len())
	}
	out, err := ReadFrame(&buf, 0)
	if err != nil || out.Kind != FrameCancel || out.ID != 3 || len(out.Body) != 0 {
		t.Fatalf("empty body: %#v %v", out, err)
	}
}

func readViolation(t *testing.T, raw []byte, maxBody int, wantDetail string) {
	t.Helper()
	_, err := ReadFrame(bytes.NewReader(raw), maxBody)
	var perr *ProtocolError
	if !errors.As(err, &perr) || !bytes.Contains([]byte(perr.Detail), []byte(wantDetail)) {
		t.Fatalf("want ProtocolError containing %q, got %v", wantDetail, err)
	}
}

func TestFrameViolations(t *testing.T) {
	readViolation(t, []byte{0x0b, 0, 0, 0}, 1<<20, "below header size")
	readViolation(t, []byte{0xff, 0xff, 0xff, 0x7f}, 1<<20, "exceeds")
	valid := func(kind, flags byte) []byte {
		return []byte{0x0c, 0, 0, 0, kind, flags, 0, 0, 1, 0, 0, 0, 0, 0, 0, 0}
	}
	readViolation(t, valid(0x09, 0), 1<<20, "unknown frame kind 0x09")
	readViolation(t, valid(0x00, 0), 1<<20, "unknown frame kind 0x00")
	readViolation(t, valid(FrameCall, 0x01), 1<<20, "flags")
}

func TestFrameEOFClassification(t *testing.T) {
	if _, err := ReadFrame(bytes.NewReader(nil), 1<<20); !errors.Is(err, io.EOF) {
		t.Fatalf("no bytes at all must be a clean io.EOF, got %v", err)
	}
	if _, err := ReadFrame(bytes.NewReader([]byte{0x0d, 0, 0, 0, FrameCall}), 1<<20); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("truncated header must be io.ErrUnexpectedEOF, got %v", err)
	}
	truncatedBody := []byte{0x0e, 0, 0, 0, FrameResult, 0, 0, 0, 1, 0, 0, 0, 0, 0, 0, 0, 0x02}
	if _, err := ReadFrame(bytes.NewReader(truncatedBody), 1<<20); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("truncated body must be io.ErrUnexpectedEOF, got %v", err)
	}
}

func TestFrameBoundaryMaxBody(t *testing.T) {
	// Body of exactly maxBody bytes is accepted
	body64 := make([]byte, 64)
	var buf bytes.Buffer
	if err := WriteFrame(&buf, Frame{Kind: FrameCall, ID: 1, Fn: 1, Body: body64}); err != nil {
		t.Fatal(err)
	}
	out, err := ReadFrame(&buf, 64)
	if err != nil || len(out.Body) != 64 {
		t.Fatalf("body of exactly maxBody (64) bytes should be accepted: %v", err)
	}
}

func TestFrameBoundaryExceedsMax(t *testing.T) {
	// Body of exactly maxBody+1 bytes is rejected with ProtocolError containing "exceeds"
	raw := []byte{
		0x4d, 0x00, 0x00, 0x00, // length = 12 + 65 = 77
		FrameCall, 0, 0, 0, // kind, flags, reserved
		1, 0, 0, 0, // id
		1, 0, 0, 0, // fn
	}
	readViolation(t, raw, 64, "exceeds")
}

func TestFrameKindOutOfRange(t *testing.T) {
	// kind 0x07 (just above FrameCancel) is rejected
	valid := func(kind, flags byte) []byte {
		return []byte{0x0c, 0, 0, 0, kind, flags, 0, 0, 1, 0, 0, 0, 0, 0, 0, 0}
	}
	readViolation(t, valid(0x07, 0), 1<<20, "unknown frame kind 0x07")
}

func TestFrameReservedBitsIsolated(t *testing.T) {
	// Reserved byte 6 non-zero with flags=0
	reserved6 := []byte{0x0c, 0, 0, 0, FrameCall, 0, 1, 0, 1, 0, 0, 0, 0, 0, 0, 0}
	readViolation(t, reserved6, 1<<20, "flags")
	// Reserved byte 7 non-zero with flags=0
	reserved7 := []byte{0x0c, 0, 0, 0, FrameCall, 0, 0, 1, 1, 0, 0, 0, 0, 0, 0, 0}
	readViolation(t, reserved7, 1<<20, "flags")
}

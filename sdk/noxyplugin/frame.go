package noxyplugin

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

// Espelho do codec de quadros do host (noxy-plugin/1, spec §2.2): u32
// length | kind | flags | reserved u16 | id u32 | fn u32 | corpo NXB.
const (
	protocolVersion = "noxy-plugin/1"
	headerSize      = 12

	kindHello  byte = 0x01
	kindCall   byte = 0x02
	kindResult byte = 0x03
	kindError  byte = 0x04
	kindLog    byte = 0x05
	kindCancel byte = 0x06
)

type frame struct {
	Kind byte
	ID   uint32
	Fn   uint32
	Body []byte
}

func writeFrame(w io.Writer, f frame) error {
	length := headerSize + len(f.Body)
	buf := make([]byte, 4+headerSize, 4+length)
	binary.LittleEndian.PutUint32(buf[0:4], uint32(length))
	buf[4] = f.Kind
	binary.LittleEndian.PutUint32(buf[8:12], f.ID)
	binary.LittleEndian.PutUint32(buf[12:16], f.Fn)
	buf = append(buf, f.Body...)
	_, err := w.Write(buf)
	return err
}

func readFrame(r io.Reader, maxBody int) (frame, error) {
	var head [4 + headerSize]byte
	if _, err := io.ReadFull(r, head[:4]); err != nil {
		return frame{}, err
	}
	length := binary.LittleEndian.Uint32(head[:4])
	if length < headerSize {
		return frame{}, fmt.Errorf("protocol violation: frame length %d below header size", length)
	}
	if uint64(length)-headerSize > uint64(maxBody) {
		return frame{}, fmt.Errorf("protocol violation: frame body exceeds %d bytes", maxBody)
	}
	if _, err := io.ReadFull(r, head[4:]); err != nil {
		return frame{}, truncated(err)
	}
	kind := head[4]
	if kind < kindHello || kind > kindCancel {
		return frame{}, fmt.Errorf("protocol violation: unknown frame kind 0x%02x", kind)
	}
	if head[5] != 0 || head[6] != 0 || head[7] != 0 {
		return frame{}, errors.New("protocol violation: non-zero flags/reserved bits")
	}
	f := frame{
		Kind: kind,
		ID:   binary.LittleEndian.Uint32(head[8:12]),
		Fn:   binary.LittleEndian.Uint32(head[12:16]),
		Body: make([]byte, int(length)-headerSize),
	}
	if _, err := io.ReadFull(r, f.Body); err != nil {
		return frame{}, truncated(err)
	}
	return f, nil
}

func truncated(err error) error {
	if errors.Is(err, io.EOF) {
		return io.ErrUnexpectedEOF
	}
	return err
}

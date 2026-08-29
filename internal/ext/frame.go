package ext

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

// Protocolo noxy-plugin/1 (spec 2026-08-29 §2.2, §2.3): um quadro e
// u32 length (bytes apos o campo: cabecalho + corpo) | kind u8 | flags u8 |
// reserved u16 | id u32 | fn u32 | corpo NXB. Tudo little-endian; flags e
// reserved sao zero na v1 e nao sao ponto de extensao.
const (
	ProtocolVersion = "noxy-plugin/1"

	frameHeaderSize = 12

	FrameHello  byte = 0x01
	FrameCall   byte = 0x02
	FrameResult byte = 0x03
	FrameError  byte = 0x04
	FrameLog    byte = 0x05
	FrameCancel byte = 0x06
)

type Frame struct {
	Kind byte
	ID   uint32
	Fn   uint32
	Body []byte
}

// ProtocolError marca um fluxo que perdeu o enquadramento: nao ha
// ressincronizacao (spec §2.2) — o host trata como trap e mata o processo,
// o plugin sai com status 2.
type ProtocolError struct{ Detail string }

func (e *ProtocolError) Error() string { return "protocol violation: " + e.Detail }

func WriteFrame(w io.Writer, f Frame) error {
	length := frameHeaderSize + len(f.Body)
	buf := make([]byte, 4+frameHeaderSize, 4+length)
	binary.LittleEndian.PutUint32(buf[0:4], uint32(length))
	buf[4] = f.Kind
	binary.LittleEndian.PutUint32(buf[8:12], f.ID)
	binary.LittleEndian.PutUint32(buf[12:16], f.Fn)
	buf = append(buf, f.Body...)
	_, err := w.Write(buf)
	return err
}

// ReadFrame le um quadro inteiro. io.EOF so quando nenhum byte do quadro
// foi lido (fim limpo); um quadro cortado no meio e io.ErrUnexpectedEOF;
// qualquer inconsistencia de cabecalho e *ProtocolError.
func ReadFrame(r io.Reader, maxBody int) (Frame, error) {
	var head [4 + frameHeaderSize]byte
	if _, err := io.ReadFull(r, head[:4]); err != nil {
		return Frame{}, err
	}
	length := binary.LittleEndian.Uint32(head[:4])
	if length < frameHeaderSize {
		return Frame{}, &ProtocolError{Detail: fmt.Sprintf("frame length %d below header size %d", length, frameHeaderSize)}
	}
	if uint64(length)-frameHeaderSize > uint64(maxBody) {
		return Frame{}, &ProtocolError{Detail: fmt.Sprintf("frame body of %d bytes exceeds the %d byte limit", length-frameHeaderSize, maxBody)}
	}
	if _, err := io.ReadFull(r, head[4:]); err != nil {
		return Frame{}, unexpected(err)
	}
	kind := head[4]
	if kind < FrameHello || kind > FrameCancel {
		return Frame{}, &ProtocolError{Detail: fmt.Sprintf("unknown frame kind 0x%02x", kind)}
	}
	if head[5] != 0 || head[6] != 0 || head[7] != 0 {
		return Frame{}, &ProtocolError{Detail: "non-zero flags/reserved bits in a v1 frame"}
	}
	f := Frame{
		Kind: kind,
		ID:   binary.LittleEndian.Uint32(head[8:12]),
		Fn:   binary.LittleEndian.Uint32(head[12:16]),
		Body: make([]byte, int(length)-frameHeaderSize),
	}
	if _, err := io.ReadFull(r, f.Body); err != nil {
		return Frame{}, unexpected(err)
	}
	return f, nil
}

// unexpected: depois do campo length, qualquer EOF e um quadro truncado.
func unexpected(err error) error {
	if errors.Is(err, io.EOF) {
		return io.ErrUnexpectedEOF
	}
	return err
}

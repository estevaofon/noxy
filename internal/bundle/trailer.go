// Package bundle e o formato do executavel gerado por `noxy build` (spec
// 2026-09-26 §4): [runtime][payload zip][trailer de 64 bytes]. O trailer
// localiza o payload; o hash so e conferido na extracao (§5.2).
package bundle

import (
	"encoding/binary"
	"encoding/hex"
	"errors"
	"io"
	"os"
)

const (
	Magic       = "NOXYAPP1"
	TrailerSize = 64
)

var ErrInconsistentTrailer = errors.New("app payload trailer is inconsistent with the file size")

// Trailer e o rodape fixo: sha256 do payload, offset e tamanho (u64 LE),
// 8 bytes reservados, magic.
type Trailer struct {
	SHA256 [32]byte
	Offset uint64
	Size   uint64
}

func (t Trailer) Marshal() []byte {
	b := make([]byte, TrailerSize)
	copy(b[0:32], t.SHA256[:])
	binary.LittleEndian.PutUint64(b[32:40], t.Offset)
	binary.LittleEndian.PutUint64(b[40:48], t.Size)
	copy(b[56:64], Magic)
	return b
}

// ParseTrailer aceita exatamente 64 bytes terminados pelo magic.
func ParseTrailer(b []byte) (Trailer, bool) {
	if len(b) != TrailerSize || string(b[56:64]) != Magic {
		return Trailer{}, false
	}
	var t Trailer
	copy(t.SHA256[:], b[0:32])
	t.Offset = binary.LittleEndian.Uint64(b[32:40])
	t.Size = binary.LittleEndian.Uint64(b[40:48])
	return t, true
}

// consistent: o payload cabe exatamente entre o runtime e o trailer.
func (t Trailer) consistent(total uint64) bool {
	if total < TrailerSize || t.Offset >= total || t.Size > total-TrailerSize {
		return false
	}
	return t.Offset == total-TrailerSize-t.Size
}

// Payload e o trecho zip do executavel em path, aberto para leitura.
type Payload struct {
	Path    string
	Trailer Trailer
	file    *os.File
}

// Open le o trailer de path. Sem magic (ou arquivo curto): (nil, nil) — e o
// noxy comum. Magic com offset/tamanho que nao batem com o arquivo:
// ErrInconsistentTrailer.
func Open(path string) (*Payload, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}
	if info.Size() < TrailerSize {
		f.Close()
		return nil, nil
	}
	buf := make([]byte, TrailerSize)
	if _, err := f.ReadAt(buf, info.Size()-TrailerSize); err != nil {
		f.Close()
		return nil, err
	}
	t, ok := ParseTrailer(buf)
	if !ok {
		f.Close()
		return nil, nil
	}
	if !t.consistent(uint64(info.Size())) {
		f.Close()
		return nil, ErrInconsistentTrailer
	}
	return &Payload{Path: path, Trailer: t, file: f}, nil
}

func (p *Payload) Reader() *io.SectionReader {
	return io.NewSectionReader(p.file, int64(p.Trailer.Offset), int64(p.Trailer.Size))
}

func (p *Payload) Close() error { return p.file.Close() }

// CacheKey: os 16 primeiros hex do sha256 do payload — o nome do diretorio
// de extracao (spec §5.2).
func (p *Payload) CacheKey() string { return hex.EncodeToString(p.Trailer.SHA256[:8]) }

// RuntimeBytes devolve os bytes do runtime em path: o arquivo inteiro se
// ele nao carrega payload, senao [0:offset) — `noxy build` rodando de
// dentro de um app (NOXY_INTERPRETER=1) nao aninha payloads (spec §4.1).
func RuntimeBytes(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(data) < TrailerSize {
		return data, nil
	}
	t, ok := ParseTrailer(data[len(data)-TrailerSize:])
	if !ok {
		return data, nil
	}
	if !t.consistent(uint64(len(data))) {
		return nil, ErrInconsistentTrailer
	}
	return data[:t.Offset], nil
}

// Guest de teste do mecanismo de extensoes: compilado para wasip1/wasm em
// tempo de teste por exttest.BuildGuest. O fn_index de nx_call despacha:
// 0 echo, 1 fail, 2 trap (panic), 3 sha256 do payload.
package main

import (
	"crypto/sha256"
	"encoding/binary"
	"unsafe"
)

//go:wasmimport noxy:host/v1 nx_fail
func nxFail(ptr, size uint32)

//go:wasmimport noxy:host/v1 nx_log
func nxLog(level, ptr, size uint32)

// abiVersionStr e sobrescrivel com -ldflags "-X main.abiVersionStr=99"
// para o teste de handshake de versao.
var abiVersionStr = "1"

var allocs = map[uint32][]byte{}

//go:wasmexport nx_abi_version
func nxABIVersion() uint32 {
	v := uint32(0)
	for _, c := range abiVersionStr {
		v = v*10 + uint32(c-'0')
	}
	return v
}

//go:wasmexport nx_alloc
func nxAlloc(size uint32) uint32 {
	if size == 0 {
		return 0
	}
	buf := make([]byte, size)
	ptr := uint32(uintptr(unsafe.Pointer(&buf[0])))
	allocs[ptr] = buf
	return ptr
}

//go:wasmexport nx_free
func nxFree(ptr, size uint32) {
	delete(allocs, ptr)
}

func region(ptr, size uint32) []byte {
	if size == 0 {
		return nil
	}
	return unsafe.Slice((*byte)(unsafe.Pointer(uintptr(ptr))), size)
}

func retBytes(data []byte) uint64 {
	ptr := nxAlloc(uint32(len(data)))
	copy(region(ptr, uint32(len(data))), data)
	return uint64(ptr)<<32 | uint64(len(data))
}

//go:wasmexport nx_call
func nxCall(fnIndex, argsPtr, argsLen uint32) uint64 {
	args := region(argsPtr, argsLen)
	switch fnIndex {
	case 0: // echo
		return retBytes(args)
	case 1: // fail
		msg := []byte("boom from guest")
		p := nxAlloc(uint32(len(msg)))
		copy(region(p, uint32(len(msg))), msg)
		nxFail(p, uint32(len(msg)))
		return 0
	case 2: // trap
		panic("guest trap")
	case 3: // sha256 do payload cru, devolvido como NXB bytes
		sum := sha256.Sum256(args)
		out := make([]byte, 0, 5+32)
		out = append(out, 0x05)
		out = binary.LittleEndian.AppendUint32(out, 32)
		out = append(out, sum[:]...)
		return retBytes(out)
	default:
		return 0
	}
}

func main() {}

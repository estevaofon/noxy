//go:build windows

package vm

import (
	"errors"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

type wsaPollFD struct {
	FD      uintptr
	Events  int16
	Revents int16
}

const (
	pollRDNORM  int16 = 0x0100
	pollWRNORM  int16 = 0x0010
	pollERR     int16 = 0x0001
	pollHUP     int16 = 0x0002
	pollNVAL    int16 = 0x0004
	pollPRI     int16 = 0x0400
	socketError int32 = -1
)

var (
	ws2                 = windows.NewLazySystemDLL("Ws2_32.dll")
	procWSAPoll         = ws2.NewProc("WSAPoll")
	procWSAGetLastError = ws2.NewProc("WSAGetLastError")
	callWSAPoll         = func(first *wsaPollFD, count uint32, timeout int32) (uintptr, uintptr, error) {
		return procWSAPoll.Call(
			uintptr(unsafe.Pointer(first)),
			uintptr(count),
			uintptr(timeout),
		)
	}
	callWSAGetLastError = func() (uintptr, uintptr, error) {
		return procWSAGetLastError.Call()
	}
)

type windowsNetworkOps struct {
	socket      func(int, int, int) (windows.Handle, error)
	bind        func(windows.Handle, windows.Sockaddr) error
	getsockname func(windows.Handle) (windows.Sockaddr, error)
	setNonblock func(windows.Handle, bool) error
	sendto      func(windows.Handle, []byte, int, windows.Sockaddr) error
	closeSocket func(windows.Handle) error
}

func systemWindowsNetworkOps() windowsNetworkOps {
	return windowsNetworkOps{
		socket:      windows.Socket,
		bind:        windows.Bind,
		getsockname: windows.Getsockname,
		setNonblock: func(handle windows.Handle, nonblocking bool) error {
			return syscall.SetNonblock(syscall.Handle(handle), nonblocking)
		},
		sendto:      windows.Sendto,
		closeSocket: windows.Closesocket,
	}
}

type windowsNetworkWake struct {
	reader  windows.Handle
	writer  windows.Handle
	address *windows.SockaddrInet4
	ops     windowsNetworkOps
}

func newWindowsNetworkWake(ops windowsNetworkOps) (*windowsNetworkWake, error) {
	reader, err := ops.socket(windows.AF_INET, windows.SOCK_DGRAM, windows.IPPROTO_UDP)
	if err != nil {
		return nil, err
	}
	fail := func(setupErr error, handles ...windows.Handle) (*windowsNetworkWake, error) {
		errorsToJoin := []error{setupErr}
		for index := len(handles) - 1; index >= 0; index-- {
			errorsToJoin = append(errorsToJoin, ops.closeSocket(handles[index]))
		}
		return nil, errors.Join(errorsToJoin...)
	}

	loopback := &windows.SockaddrInet4{Addr: [4]byte{127, 0, 0, 1}}
	if err := ops.bind(reader, loopback); err != nil {
		return fail(err, reader)
	}
	bound, err := ops.getsockname(reader)
	if err != nil {
		return fail(err, reader)
	}
	address, ok := bound.(*windows.SockaddrInet4)
	if !ok {
		return fail(errors.New("Windows UDP wake address is not IPv4"), reader)
	}

	writer, err := ops.socket(windows.AF_INET, windows.SOCK_DGRAM, windows.IPPROTO_UDP)
	if err != nil {
		return fail(err, reader)
	}
	if err := ops.setNonblock(reader, true); err != nil {
		return fail(err, reader, writer)
	}
	if err := ops.setNonblock(writer, true); err != nil {
		return fail(err, reader, writer)
	}
	return &windowsNetworkWake{
		reader:  reader,
		writer:  writer,
		address: address,
		ops:     ops,
	}, nil
}

func (wake *windowsNetworkWake) descriptor() uintptr { return uintptr(wake.reader) }

func (wake *windowsNetworkWake) signal() error {
	err := wake.ops.sendto(wake.writer, []byte{1}, 0, wake.address)
	if errors.Is(err, windows.WSAEWOULDBLOCK) {
		return nil
	}
	return err
}

func (wake *windowsNetworkWake) close() error {
	return errors.Join(
		wake.ops.closeSocket(wake.writer),
		wake.ops.closeSocket(wake.reader),
	)
}

func windowsPollEvents(interests networkInterest) int16 {
	var events int16
	if interests&networkReadable != 0 {
		events |= pollRDNORM
	}
	if interests&networkWritable != 0 {
		events |= pollWRNORM
	}
	return events
}

func normalizeWindowsPollEvents(events int16) networkEvent {
	var normalized networkEvent
	if events&pollRDNORM != 0 {
		normalized |= networkReadReady
	}
	if events&pollWRNORM != 0 {
		normalized |= networkWriteReady
	}
	if events&(pollHUP|pollERR|pollNVAL) != 0 {
		normalized |= networkReadReady | networkWriteReady | networkErrorReady
	}
	return normalized
}

func windowsNetworkWait(descriptors []networkPollFD, wakeDescriptor uintptr, timeout int32) (networkPollBatch, error) {
	pollfds := make([]wsaPollFD, len(descriptors)+1)
	pollfds[0] = wsaPollFD{FD: wakeDescriptor, Events: pollRDNORM}
	for index, descriptor := range descriptors {
		interests := descriptor.interests
		if descriptor.listener {
			interests &^= networkWritable
		}
		pollfds[index+1] = wsaPollFD{
			FD:     descriptor.descriptor,
			Events: windowsPollEvents(interests),
		}
	}

	result, _, _ := callWSAPoll(&pollfds[0], uint32(len(pollfds)), timeout)
	if int32(result) == socketError {
		code, _, _ := callWSAGetLastError()
		return networkPollBatch{}, syscall.Errno(code)
	}

	batch := networkPollBatch{events: make([]networkEvent, len(descriptors))}
	if pollfds[0].Revents&(pollRDNORM|pollHUP|pollERR|pollNVAL) != 0 {
		batch.woke = true
	}
	for index, descriptor := range descriptors {
		event := normalizeWindowsPollEvents(pollfds[index+1].Revents)
		if descriptor.listener {
			event &^= networkWriteReady
		}
		batch.events[index] = event
	}
	return batch, nil
}

func systemNetworkPlatform() networkPlatform {
	return networkPlatform{
		newWake: func() (platformNetworkWake, error) {
			return newWindowsNetworkWake(systemWindowsNetworkOps())
		},
		wait: windowsNetworkWait,
	}
}

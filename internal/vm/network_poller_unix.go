//go:build aix || darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris

package vm

import (
	"errors"
	"fmt"

	"golang.org/x/sys/unix"
)

type unixNetworkOps struct {
	socketpair  func(int, int, int) ([2]int, error)
	setNonblock func(int, bool) error
	write       func(int, []byte) (int, error)
	close       func(int) error
}

func systemUnixNetworkOps() unixNetworkOps {
	return unixNetworkOps{
		socketpair:  unix.Socketpair,
		setNonblock: unix.SetNonblock,
		write:       unix.Write,
		close:       unix.Close,
	}
}

type unixNetworkWake struct {
	reader int
	writer int
	ops    unixNetworkOps
}

func newUnixNetworkWake(ops unixNetworkOps) (*unixNetworkWake, error) {
	pair, err := ops.socketpair(unix.AF_UNIX, unix.SOCK_STREAM, 0)
	if err != nil {
		return nil, err
	}
	fail := func(setupErr error) (*unixNetworkWake, error) {
		return nil, errors.Join(
			setupErr,
			ops.close(pair[1]),
			ops.close(pair[0]),
		)
	}
	if err := ops.setNonblock(pair[0], true); err != nil {
		return fail(err)
	}
	if err := ops.setNonblock(pair[1], true); err != nil {
		return fail(err)
	}
	return &unixNetworkWake{reader: pair[0], writer: pair[1], ops: ops}, nil
}

func (wake *unixNetworkWake) descriptor() uintptr { return uintptr(wake.reader) }

func (wake *unixNetworkWake) signal() error {
	for {
		_, err := wake.ops.write(wake.writer, []byte{1})
		if errors.Is(err, unix.EINTR) {
			continue
		}
		if errors.Is(err, unix.EAGAIN) || errors.Is(err, unix.EWOULDBLOCK) {
			return nil
		}
		return err
	}
}

func (wake *unixNetworkWake) close() error {
	return errors.Join(
		wake.ops.close(wake.writer),
		wake.ops.close(wake.reader),
	)
}

func unixPollEvents(interests networkInterest) uint16 {
	var events uint16
	if interests&networkReadable != 0 {
		events |= uint16(unix.POLLIN)
	}
	if interests&networkWritable != 0 {
		events |= uint16(unix.POLLOUT)
	}
	return events
}

func normalizeUnixPollEvents(events uint16) networkEvent {
	var normalized networkEvent
	if events&uint16(unix.POLLIN) != 0 {
		normalized |= networkReadReady
	}
	if events&uint16(unix.POLLOUT) != 0 {
		normalized |= networkWriteReady
	}
	if events&(uint16(unix.POLLHUP)|uint16(unix.POLLERR)|uint16(unix.POLLNVAL)|uint16(networkPollReadHangup)) != 0 {
		normalized |= networkReadReady | networkWriteReady | networkErrorReady
	}
	return normalized
}

var callUnixPoll = unix.Poll

func unixPollDescriptor(descriptor uintptr) (int32, error) {
	converted := int32(descriptor)
	if converted < 0 || uintptr(converted) != descriptor {
		return 0, fmt.Errorf("network poll descriptor %d cannot be represented as int32", descriptor)
	}
	return converted, nil
}

func unixNetworkWait(descriptors []networkPollFD, wakeDescriptor uintptr, timeout int32) (networkPollBatch, error) {
	wakeFD, err := unixPollDescriptor(wakeDescriptor)
	if err != nil {
		return networkPollBatch{}, err
	}
	pollfds := make([]unix.PollFd, len(descriptors)+1)
	pollfds[0] = unix.PollFd{Fd: wakeFD, Events: unix.POLLIN}
	for index, descriptor := range descriptors {
		fd, descriptorErr := unixPollDescriptor(descriptor.descriptor)
		if descriptorErr != nil {
			return networkPollBatch{}, descriptorErr
		}
		interests := descriptor.interests
		if descriptor.listener {
			interests &^= networkWritable
		}
		pollfds[index+1] = unix.PollFd{Fd: fd}
		events := unixPollEvents(interests)
		if events&uint16(unix.POLLIN) != 0 {
			pollfds[index+1].Events |= unix.POLLIN
		}
		if events&uint16(unix.POLLOUT) != 0 {
			pollfds[index+1].Events |= unix.POLLOUT
		}
		if !descriptor.listener {
			addNetworkPollReadHangup(&pollfds[index+1])
		}
	}

	if _, err := callUnixPoll(pollfds, int(timeout)); err != nil {
		if errors.Is(err, unix.EINTR) {
			return networkPollBatch{interrupted: true}, nil
		}
		return networkPollBatch{}, err
	}

	batch := networkPollBatch{events: make([]networkEvent, len(descriptors))}
	wakeEvents := uint16(pollfds[0].Revents)
	if wakeEvents&(uint16(unix.POLLIN)|uint16(unix.POLLHUP)|uint16(unix.POLLERR)|uint16(unix.POLLNVAL)|uint16(networkPollReadHangup)) != 0 {
		batch.woke = true
	}
	for index, descriptor := range descriptors {
		event := normalizeUnixPollEvents(uint16(pollfds[index+1].Revents))
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
			return newUnixNetworkWake(systemUnixNetworkOps())
		},
		wait: unixNetworkWait,
	}
}

//go:build linux || freebsd

package vm

import "golang.org/x/sys/unix"

const networkPollReadHangup int16 = unix.POLLRDHUP

func addNetworkPollReadHangup(descriptor *unix.PollFd) {
	descriptor.Events |= unix.POLLRDHUP
}

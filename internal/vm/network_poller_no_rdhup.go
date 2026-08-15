//go:build aix || darwin || dragonfly || netbsd || openbsd || solaris

package vm

import "golang.org/x/sys/unix"

const networkPollReadHangup int16 = 0

func addNetworkPollReadHangup(*unix.PollFd) {}

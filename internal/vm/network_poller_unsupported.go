//go:build !windows && !aix && !darwin && !dragonfly && !freebsd && !linux && !netbsd && !openbsd && !solaris

package vm

import "fmt"

const unsupportedNetworkPollingError = "network polling is not supported on this platform"

func systemNetworkPlatform() networkPlatform {
	return networkPlatform{
		newWake: func() (platformNetworkWake, error) {
			return nil, fmt.Errorf(unsupportedNetworkPollingError)
		},
		wait: func([]networkPollFD, uintptr, int32) (networkPollBatch, error) {
			return networkPollBatch{}, fmt.Errorf(unsupportedNetworkPollingError)
		},
	}
}

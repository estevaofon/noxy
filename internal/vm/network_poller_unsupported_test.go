//go:build !windows && !aix && !darwin && !dragonfly && !freebsd && !linux && !netbsd && !openbsd && !solaris

package vm

import "testing"

func TestUnsupportedNetworkPlatformReturnsStableErrors(t *testing.T) {
	const want = "network polling is not supported on this platform"
	platform := systemNetworkPlatform()
	wake, err := platform.newWake()
	if wake != nil || err == nil || err.Error() != want {
		t.Fatalf("newWake=(%v, %v), want (nil, %q)", wake, err, want)
	}
	batch, err := platform.wait(nil, 0, 0)
	if err == nil || err.Error() != want {
		t.Fatalf("wait=(%+v, %v), want error %q", batch, err, want)
	}
}

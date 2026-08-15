package vm

import (
	"fmt"
	"math"
	"sync"
	"time"

	"noxy-vm/internal/value"
)

type platformNetworkWake interface {
	descriptor() uintptr
	signal() error
	close() error
}

type networkWakeState uint8

const (
	networkWakeOpen networkWakeState = iota
	networkWakeSignaled
	networkWakeClosed
)

type networkWake struct {
	mu       sync.Mutex
	state    networkWakeState
	platform platformNetworkWake
}

func newNetworkWake(platform platformNetworkWake) *networkWake {
	return &networkWake{platform: platform}
}

func (wake *networkWake) Signal() error {
	wake.mu.Lock()
	defer wake.mu.Unlock()
	if wake.state != networkWakeOpen {
		return nil
	}
	if err := wake.platform.signal(); err != nil {
		return err
	}
	wake.state = networkWakeSignaled
	return nil
}

func (wake *networkWake) Close() error {
	wake.mu.Lock()
	defer wake.mu.Unlock()
	if wake.state == networkWakeClosed {
		return nil
	}
	wake.state = networkWakeClosed
	return wake.platform.close()
}

const networkSetCapacity = 64

type networkInterest uint8

const (
	networkReadable networkInterest = 1 << iota
	networkWritable
	networkErrorInterest
)

type networkEvent uint8

const (
	networkReadReady networkEvent = 1 << iota
	networkWriteReady
	networkErrorReady
)

func validateNetworkPollArguments(args []value.Value) ([3]*value.ObjArray, time.Duration, error) {
	var sets [3]*value.ObjArray
	if len(args) != 4 {
		return sets, 0, fmt.Errorf("net_select expects exactly 4 arguments")
	}
	for index := 0; index < 3; index++ {
		array, ok := args[index].Obj.(*value.ObjArray)
		if args[index].Type != value.VAL_OBJ || !ok || array == nil {
			return sets, 0, fmt.Errorf("net_select read, write, and error arguments must be arrays")
		}
		sets[index] = array
	}
	if args[3].Type != value.VAL_INT {
		return sets, 0, fmt.Errorf("network poll timeout must be an int")
	}
	milliseconds := args[3].AsInt
	if milliseconds < 0 {
		return sets, 0, fmt.Errorf("network poll timeout must be non-negative")
	}
	maximum := int64(math.MaxInt64) / int64(time.Millisecond)
	if milliseconds > maximum {
		return sets, 0, fmt.Errorf("network poll timeout is too large")
	}
	return sets, time.Duration(milliseconds) * time.Millisecond, nil
}

func boundedNetworkCandidates(array *value.ObjArray) []value.Value {
	if len(array.Elements) <= networkSetCapacity {
		return array.Elements
	}
	return array.Elements[:networkSetCapacity]
}

func fixedNetworkSet(ready []value.Value) value.Value {
	elements := make([]value.Value, networkSetCapacity)
	for i := range elements {
		elements[i] = value.NewNull()
	}
	copy(elements, ready[:min(len(ready), networkSetCapacity)])
	return value.NewArray(elements)
}

func selectResult(read, write, errors []value.Value) value.Value {
	read = read[:min(len(read), networkSetCapacity)]
	write = write[:min(len(write), networkSetCapacity)]
	errors = errors[:min(len(errors), networkSetCapacity)]
	return value.NewMapWithData(map[string]value.Value{
		"read": fixedNetworkSet(read), "read_count": value.NewInt(int64(len(read))),
		"write": fixedNetworkSet(write), "write_count": value.NewInt(int64(len(write))),
		"error": fixedNetworkSet(errors), "error_count": value.NewInt(int64(len(errors))),
	})
}

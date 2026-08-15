package vm

import (
	"errors"
	"fmt"
	"math"
	"reflect"
	"sync"
	"syscall"
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

type networkPollFD struct {
	descriptor uintptr
	interests  networkInterest
	listener   bool
}

type networkPollBatch struct {
	events      []networkEvent
	woke        bool
	interrupted bool
}

type networkPlatform struct {
	newWake func() (platformNetworkWake, error)
	wait    func([]networkPollFD, uintptr, int32) (networkPollBatch, error)
}

type networkPoller struct {
	platform networkPlatform
	now      func() time.Time
	sleep    func(time.Duration)
}

type networkRegistration struct {
	handle          int
	requested       networkInterest
	nativeInterests networkInterest
	pollable        bool
	listener        *ListenerResource
	socket          *SocketResource
	attached        any
	raw             syscall.RawConn
}

type networkOccurrence struct {
	candidate    value.Value
	registration *networkRegistration
}

type networkAcquisitionStage uint8

const (
	networkAcquireSyscallConn networkAcquisitionStage = iota
	networkAcquireControl
)

type networkAcquisitionError struct {
	registration    *networkRegistration
	stage           networkAcquisitionStage
	callbackEntered bool
	err             error
}

func (failure *networkAcquisitionError) Error() string { return failure.err.Error() }
func (failure *networkAcquisitionError) Unwrap() error { return failure.err }

func networkPollMilliseconds(remaining time.Duration) int32 {
	if remaining <= 0 {
		return 0
	}
	if remaining > time.Second {
		remaining = time.Second
	}
	milliseconds := remaining / time.Millisecond
	if remaining%time.Millisecond != 0 {
		milliseconds++
	}
	return int32(milliseconds)
}

func withNetworkDescriptors(
	registrations []*networkRegistration,
	pollfds []networkPollFD,
	index int,
	wait func([]networkPollFD) (networkPollBatch, error),
) (batch networkPollBatch, failure *networkAcquisitionError, err error) {
	if index == len(registrations) {
		batch, err = wait(pollfds)
		return batch, nil, err
	}
	registration := registrations[index]
	callbackEntered := false
	controlErr := registration.raw.Control(func(fd uintptr) {
		callbackEntered = true
		pollfds[index] = networkPollFD{
			descriptor: fd,
			interests:  registration.nativeInterests,
			listener:   registration.listener != nil,
		}
		batch, failure, err = withNetworkDescriptors(registrations, pollfds, index+1, wait)
	})
	if failure != nil || err != nil {
		return batch, failure, err
	}
	if controlErr != nil {
		return batch, &networkAcquisitionError{
			registration:    registration,
			stage:           networkAcquireControl,
			callbackEntered: callbackEntered,
			err:             controlErr,
		}, nil
	}
	return batch, nil, nil
}

func (poller *networkPoller) Poll(shared *SharedState, sets [3]*value.ObjArray, timeout time.Duration) (result value.Value, err error) {
	result = value.NewNull()
	now := poller.networkNow()
	deadline := time.Time{}
	if timeout > 0 {
		deadline = now().Add(timeout)
	}
	registrations, occurrences := collectNetworkRegistrations(shared, sets)
	registrations = initiallyOpenNetworkRegistrations(shared, registrations)
	if len(registrations) == 0 {
		if timeout > 0 {
			remaining := deadline.Sub(now())
			if remaining > 0 {
				poller.networkSleep()(remaining)
			}
		}
		return selectResult(nil, nil, nil), nil
	}

	if poller.platform.newWake == nil || poller.platform.wait == nil {
		return result, fmt.Errorf("network poll platform is unavailable")
	}
	platformWake, wakeErr := poller.platform.newWake()
	if wakeErr != nil {
		return result, wakeErr
	}
	if platformWake == nil {
		return result, fmt.Errorf("network poll wake is unavailable")
	}
	wake := newNetworkWake(platformWake)
	defer func() {
		for _, registration := range registrations {
			unregisterNetworkWake(registration, wake)
		}
		err = errors.Join(err, wake.Close())
		if err != nil {
			result = value.NewNull()
		}
	}()

	pollRegistrations := make([]*networkRegistration, 0, len(registrations))
	attachedCount := 0
	for _, registration := range registrations {
		if !attachNetworkRegistration(shared, registration, wake) {
			continue
		}
		attachedCount++
		if !registration.pollable {
			continue
		}
		failure := acquireNetworkRawConn(registration)
		if failure != nil {
			if !networkRegistrationCurrent(shared, failure.registration) {
				return selectResult(nil, nil, nil), nil
			}
			return result, failure
		}
		pollRegistrations = append(pollRegistrations, registration)
	}
	if attachedCount == 0 {
		return selectResult(nil, nil, nil), nil
	}

	ready := make(map[*networkRegistration]networkInterest, len(pollRegistrations))
	pollfds := make([]networkPollFD, len(pollRegistrations))
	waitOnce := func(milliseconds int32) (networkPollBatch, *networkAcquisitionError, error) {
		return withNetworkDescriptors(pollRegistrations, pollfds, 0, func(descriptors []networkPollFD) (networkPollBatch, error) {
			return poller.platform.wait(descriptors, platformWake.descriptor(), milliseconds)
		})
	}

	if timeout == 0 {
		batch, failure, waitErr := waitOnce(0)
		if failure != nil {
			if !networkRegistrationCurrent(shared, failure.registration) {
				return selectResult(nil, nil, nil), nil
			}
			return result, failure
		}
		if waitErr != nil {
			return result, waitErr
		}
		applyNetworkEvents(pollRegistrations, batch.events, ready)
		return reconstructNetworkResult(shared, occurrences, ready), nil
	}

	for {
		remaining := deadline.Sub(now())
		if remaining <= 0 {
			break
		}
		batch, failure, waitErr := waitOnce(networkPollMilliseconds(remaining))
		if failure != nil {
			if !networkRegistrationCurrent(shared, failure.registration) {
				return reconstructNetworkResult(shared, occurrences, ready), nil
			}
			return result, failure
		}
		if waitErr != nil {
			return result, waitErr
		}
		applyNetworkEvents(pollRegistrations, batch.events, ready)
		if len(ready) != 0 {
			break
		}
		if anyNetworkRegistrationClosed(shared, registrations) {
			break
		}
	}
	return reconstructNetworkResult(shared, occurrences, ready), nil
}

func (poller *networkPoller) networkNow() func() time.Time {
	if poller.now != nil {
		return poller.now
	}
	return time.Now
}

func (poller *networkPoller) networkSleep() func(time.Duration) {
	if poller.sleep != nil {
		return poller.sleep
	}
	return time.Sleep
}

func collectNetworkRegistrations(shared *SharedState, sets [3]*value.ObjArray) ([]*networkRegistration, [3][]networkOccurrence) {
	var occurrences [3][]networkOccurrence
	registrations := make([]*networkRegistration, 0)
	byHandle := make(map[int]*networkRegistration)
	interests := [3]networkInterest{networkReadable, networkWritable, networkErrorInterest}
	for setIndex, set := range sets {
		if set == nil {
			continue
		}
		for _, candidate := range boundedNetworkCandidates(set) {
			handle, extractionErr := networkSocketDescriptor(candidate)
			if extractionErr != nil {
				continue
			}
			registration, exists := byHandle[handle]
			if !exists {
				registration = lookupNetworkRegistration(shared, handle)
				if registration == nil {
					continue
				}
				byHandle[handle] = registration
				registrations = append(registrations, registration)
			}
			registration.requested |= interests[setIndex]
			occurrences[setIndex] = append(occurrences[setIndex], networkOccurrence{
				candidate:    candidate,
				registration: registration,
			})
		}
	}
	for _, registration := range registrations {
		registration.nativeInterests = registration.requested
		if registration.listener != nil {
			registration.nativeInterests &^= networkWritable
			registration.pollable = registration.nativeInterests&(networkReadable|networkErrorInterest) != 0
		} else {
			registration.pollable = true
		}
	}
	return registrations, occurrences
}

func lookupNetworkRegistration(shared *SharedState, handle int) *networkRegistration {
	if shared == nil {
		return nil
	}
	if listener, exists := shared.Listeners.get(handle); exists {
		return &networkRegistration{handle: handle, listener: listener}
	}
	if socket, exists := shared.Sockets.get(handle); exists {
		return &networkRegistration{handle: handle, socket: socket}
	}
	return nil
}

func initiallyOpenNetworkRegistrations(shared *SharedState, registrations []*networkRegistration) []*networkRegistration {
	open := make([]*networkRegistration, 0, len(registrations))
	for _, registration := range registrations {
		if networkRegistrationOpen(shared, registration) {
			open = append(open, registration)
		}
	}
	return open
}

func networkRegistrationOpen(shared *SharedState, registration *networkRegistration) bool {
	if shared == nil || registration == nil {
		return false
	}
	if registration.listener != nil {
		resource := registration.listener
		resource.stateMu.Lock()
		defer resource.stateMu.Unlock()
		current, exists := shared.Listeners.get(registration.handle)
		return exists && current == resource && !resource.closed && resource.listener != nil
	}
	if registration.socket == nil {
		return false
	}
	resource := registration.socket
	resource.stateMu.Lock()
	defer resource.stateMu.Unlock()
	current, exists := shared.Sockets.get(registration.handle)
	return exists && current == resource && !resource.closed && resource.connection != nil
}

func attachNetworkRegistration(shared *SharedState, registration *networkRegistration, wake *networkWake) bool {
	if registration.listener != nil {
		resource := registration.listener
		resource.stateMu.Lock()
		defer resource.stateMu.Unlock()
		current, exists := shared.Listeners.get(registration.handle)
		if !exists || current != resource || resource.closed || resource.listener == nil {
			return false
		}
		if resource.pollWaiters == nil {
			resource.pollWaiters = make(map[*networkWake]struct{})
		}
		resource.pollWaiters[wake] = struct{}{}
		registration.attached = resource.listener
		return true
	}
	resource := registration.socket
	resource.stateMu.Lock()
	defer resource.stateMu.Unlock()
	current, exists := shared.Sockets.get(registration.handle)
	if !exists || current != resource || resource.closed || resource.connection == nil {
		return false
	}
	if resource.pollWaiters == nil {
		resource.pollWaiters = make(map[*networkWake]struct{})
	}
	resource.pollWaiters[wake] = struct{}{}
	registration.attached = resource.connection
	return true
}

func acquireNetworkRawConn(registration *networkRegistration) *networkAcquisitionError {
	connection, ok := registration.attached.(syscall.Conn)
	if !ok {
		return &networkAcquisitionError{
			registration: registration,
			stage:        networkAcquireSyscallConn,
			err:          fmt.Errorf("network resource does not expose SyscallConn"),
		}
	}
	raw, err := connection.SyscallConn()
	if err != nil {
		return &networkAcquisitionError{
			registration: registration,
			stage:        networkAcquireSyscallConn,
			err:          err,
		}
	}
	registration.raw = raw
	return nil
}

func unregisterNetworkWake(registration *networkRegistration, wake *networkWake) {
	if registration.listener != nil {
		registration.listener.stateMu.Lock()
		delete(registration.listener.pollWaiters, wake)
		registration.listener.stateMu.Unlock()
		return
	}
	registration.socket.stateMu.Lock()
	delete(registration.socket.pollWaiters, wake)
	registration.socket.stateMu.Unlock()
}

func networkRegistrationCurrent(shared *SharedState, registration *networkRegistration) bool {
	if registration.attached == nil {
		return false
	}
	if registration.listener != nil {
		resource := registration.listener
		resource.stateMu.Lock()
		defer resource.stateMu.Unlock()
		current, exists := shared.Listeners.get(registration.handle)
		return exists && current == resource && !resource.closed && sameNetworkAttachment(resource.listener, registration.attached)
	}
	resource := registration.socket
	resource.stateMu.Lock()
	defer resource.stateMu.Unlock()
	current, exists := shared.Sockets.get(registration.handle)
	return exists && current == resource && !resource.closed && sameNetworkAttachment(resource.connection, registration.attached)
}

func sameNetworkAttachment(current, attached any) bool {
	if current == nil || attached == nil || reflect.TypeOf(current) != reflect.TypeOf(attached) {
		return false
	}
	if !reflect.TypeOf(current).Comparable() {
		return false
	}
	return current == attached
}

func anyNetworkRegistrationClosed(shared *SharedState, registrations []*networkRegistration) bool {
	for _, registration := range registrations {
		if registration.attached != nil && !networkRegistrationCurrent(shared, registration) {
			return true
		}
	}
	return false
}

func applyNetworkEvents(registrations []*networkRegistration, events []networkEvent, ready map[*networkRegistration]networkInterest) {
	for index, registration := range registrations {
		if index >= len(events) {
			break
		}
		event := events[index]
		var interests networkInterest
		if event&networkErrorReady != 0 {
			interests = registration.requested
			if registration.listener != nil {
				interests &^= networkWritable
			}
		} else {
			if event&networkReadReady != 0 {
				interests |= registration.requested & networkReadable
			}
			if event&networkWriteReady != 0 && registration.listener == nil {
				interests |= registration.requested & networkWritable
			}
		}
		if interests != 0 {
			ready[registration] |= interests
		}
	}
}

func reconstructNetworkResult(shared *SharedState, occurrences [3][]networkOccurrence, ready map[*networkRegistration]networkInterest) value.Value {
	interests := [3]networkInterest{networkReadable, networkWritable, networkErrorInterest}
	sets := [3][]value.Value{}
	valid := make(map[*networkRegistration]bool)
	checked := make(map[*networkRegistration]bool)
	for setIndex, candidates := range occurrences {
		for _, occurrence := range candidates {
			registration := occurrence.registration
			if !checked[registration] {
				checked[registration] = true
				valid[registration] = networkRegistrationCurrent(shared, registration)
			}
			interest := interests[setIndex]
			if registration.listener != nil && interest == networkWritable {
				continue
			}
			if valid[registration] && ready[registration]&interest != 0 {
				sets[setIndex] = append(sets[setIndex], occurrence.candidate)
			}
		}
	}
	return selectResult(sets[0], sets[1], sets[2])
}

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

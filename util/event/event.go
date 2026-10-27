package event

import "github.com/anacrolix/sync"

//
// Taken from missinggo.Event
//

// Events are boolean flags that provide a channel that's closed when true.
// This could go in the sync package, but that's more of a debug wrapper on
// the standard library sync.

// Event ...
type Event struct {
	ch     chan struct{}
	closed bool
	mu     sync.Mutex
}

// LockedChan ...
func (me *Event) LockedChan(lock sync.Locker) <-chan struct{} {
	lock.Lock()
	ch := me.C()
	lock.Unlock()
	return ch
}

// C Returns a chan that is closed when the event is true.
// Locked: goroutines started together race here and could each get their own chan, so Set() would close only one of them.
func (me *Event) C() <-chan struct{} {
	me.mu.Lock()
	defer me.mu.Unlock()

	if me.ch == nil {
		me.ch = make(chan struct{})
	}
	return me.ch
}

// Clear ...
// TODO: Merge into Set.
func (me *Event) Clear() {
	me.mu.Lock()
	defer me.mu.Unlock()

	if me.closed {
		me.ch = nil
		me.closed = false
	}
}

// Set the event to true/on.
func (me *Event) Set() (first bool) {
	me.mu.Lock()
	defer me.mu.Unlock()

	if me.closed {
		return false
	}
	if me.ch == nil {
		me.ch = make(chan struct{})
	}
	close(me.ch)
	me.closed = true
	return true
}

// IsSet ...
// TODO: Change to Get.
func (me *Event) IsSet() bool {
	return me.closed
}

// Wait ...
func (me *Event) Wait() {
	<-me.C()
}

// SetBool ...
// TODO: Merge into Set.
func (me *Event) SetBool(b bool) {
	if b {
		me.Set()
	} else {
		me.Clear()
	}
}

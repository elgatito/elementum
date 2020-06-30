// Package broadcast implements broadcast type messaging (or event) in a Go idiomatic
// way. This is based on the design invented at [1] and the actual implementation
// from [2], modified to only expose channels, which are the prefered way of
// communicating in Go.
//
// The comments are orignally from the gibb package.
//
// [1] http://rogpeppe.wordpress.com/2009/12/01/concurrent-idioms-1-broadcasting-values-in-go-with-linked-channels/
// [2] https://github.com/dagoof/gibb
package broadcast

import (
	"sync"
)

// message contains a snapshot of a value.
type message struct {
	v interface{}
	c chan message
}

// Broadcaster provides Receivers that can be read from. Every value that is
// written to a broadcaster will be sent to any active Receivers. No messages
// are dropped, and are delivered in order. Receivers that fail to keep up with
// producers can oom your system because no messages are dropped.
type Broadcaster struct {
	mx sync.Mutex
	c  chan message
}

// Receiver can be Read from in various ways. All reads are concurrency-safe.
type receiver struct {
	mx sync.Mutex
	c  chan message
}

const (
	// WATCHED ...
	WATCHED = iota
)

// LocalBroadcasters ...
var LocalBroadcasters = map[int]*Broadcaster{}

// NewBroadcaster creates a new broadcaster with the necessary internal
// structure. The uninitialized broadcaster is unsuitable to be listened or
// written to.
func NewBroadcaster() *Broadcaster {
	return &Broadcaster{
		sync.Mutex{},
		make(chan message, 1),
	}
}

// NewLocalBroadcaster ...
func NewLocalBroadcaster() *Broadcaster {
	return NewBroadcaster()
}

// Write a value to all listening receivers.
func (b *Broadcaster) Write(v interface{}) {
	c := make(chan message, 1)
	b.mx.Lock()
	defer b.mx.Unlock()

	b.c <- message{v, c}
	b.c = c
}

// Broadcast ...
func (b *Broadcaster) Broadcast(v interface{}) {
	b.Write(v)
}

// Signal ...
func (b *Broadcaster) Signal() {
	b.Write(nil)
}

// Listen creates a receiver that can read written values.
func (b *Broadcaster) Listen() (<-chan interface{}, chan<- interface{}) {
	b.mx.Lock()
	defer b.mx.Unlock()

	r := receiver{sync.Mutex{}, b.c}

	vc := make(chan interface{})
	cc := make(chan interface{})

	go func() {
		r.mx.Lock()
		defer r.mx.Unlock()
		defer close(vc)

		for {
			select {
			case <-cc:
				return
			case msg, ok := <-r.c:
				if !ok {
					return
				}
				r.c <- msg
				r.c = msg.c
				vc <- msg.v
			}
		}
	}()

	return vc, cc
}

// Close Closes the broadcaster, this also closes all the listening channels.
func (b *Broadcaster) Close() {
	close(b.c)
}

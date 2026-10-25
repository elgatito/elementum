package util

import (
	"fmt"
	"sync"
)

// Locker wraps a map of mutexes.  Each key locks separately.
type Locker struct {
	ml sync.Mutex      // lock for entry map
	ma map[any]*mentry // entry map
}

type mentry struct {
	m   *Locker    // point back to M, so we can synchronize removing this mentry when cnt==0
	el  sync.Mutex // entry-specific lock
	cnt int        // reference count
	key any        // key in ma
}

// Unlocker provides an Unlock method to release the lock.
type Unlocker interface {
	Unlock()
}

// NewLocker returns an initalized M.
func NewLocker() *Locker {
	return &Locker{ma: make(map[any]*mentry)}
}

// Lock acquires a lock corresponding to this key.
// This method will never return nil and Unlock() must be called
// to release the lock when done.
func (m *Locker) Lock(key any) Unlocker {

	// read or create entry for this key atomically
	m.ml.Lock()
	e, ok := m.ma[key]
	if !ok {
		e = &mentry{m: m, key: key}
		m.ma[key] = e
	}
	e.cnt++ // ref count
	m.ml.Unlock()

	// acquire lock, will block here until e.cnt==1
	e.el.Lock()

	return e
}

// Unlock releases the lock for this entry.
func (me *mentry) Unlock() {

	m := me.m

	// decrement and if needed remove entry atomically
	m.ml.Lock()
	e, ok := m.ma[me.key]
	if !ok { // entry must exist
		m.ml.Unlock()
		panic(fmt.Errorf("Unlock requested for key=%v but no entry found", me.key))
	}
	e.cnt--        // ref count
	if e.cnt < 1 { // if it hits zero then we own it and remove from map
		delete(m.ma, me.key)
	}
	m.ml.Unlock()

	// now that map stuff is handled, we unlock and let
	// anything else waiting on this key through
	e.el.Unlock()

}

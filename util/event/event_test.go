package event

import (
	"sync"
	"testing"
	"time"
)

func TestConcurrentCClosedBySet(t *testing.T) {
	var e Event
	var wg sync.WaitGroup
	chs := make([]<-chan struct{}, 100)
	for i := range chs {
		wg.Go(func() { chs[i] = e.C() })
	}
	wg.Wait()

	e.Set()
	for _, ch := range chs {
		select {
		case <-ch:
		case <-time.After(time.Second):
			t.Fatal("channel from C() was not closed by Set()")
		}
	}
}

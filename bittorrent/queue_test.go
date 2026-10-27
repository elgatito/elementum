package bittorrent

import (
	"strconv"
	"sync"
	"testing"
)

func TestQueueConcurrentAccess(t *testing.T) {
	q := NewQueue(nil)
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(2)
		go func(i int) {
			defer wg.Done()
			tr := &Torrent{infoHash: strconv.Itoa(i)}
			q.Add(tr)
			q.Delete(tr)
		}(i)
		go func() {
			defer wg.Done()
			seen := map[string]bool{}
			for _, tr := range q.All() {
				if seen[tr.InfoHash()] {
					t.Errorf("duplicate torrent %s in snapshot", tr.InfoHash())
				}
				seen[tr.InfoHash()] = true
			}
		}()
	}
	wg.Wait()

	if n := len(q.All()); n != 0 {
		t.Fatalf("expected empty queue, got %d", n)
	}
}

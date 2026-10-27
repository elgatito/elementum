package bittorrent

import "sync"

// Queue represents list of torrents inside of a session
type Queue struct {
	s        *Service
	mu       sync.RWMutex
	torrents []*Torrent
}

// NewQueue contructor for empty Queue
func NewQueue(s *Service) *Queue {
	return &Queue{
		s:        s,
		torrents: []*Torrent{},
	}
}

// Add torrent to the queue
func (q *Queue) Add(t *Torrent) bool {
	q.mu.Lock()
	defer q.mu.Unlock()

	if q.findByHash(t.InfoHash()) != nil {
		return false
	}

	q.torrents = append(q.torrents, t)
	return true
}

// Delete removes torrent from the queue
func (q *Queue) Delete(t *Torrent) bool {
	q.mu.Lock()
	defer q.mu.Unlock()

	idx := -1
	for i, ti := range q.torrents {
		if ti.InfoHash() == t.InfoHash() {
			idx = i
			break
		}
	}

	if idx < 0 {
		return false
	}

	q.torrents = append(q.torrents[:idx], q.torrents[idx+1:]...)
	return true
}

// All returns a snapshot of the queue, safe to iterate while torrents are added or removed
func (q *Queue) All() []*Torrent {
	q.mu.RLock()
	defer q.mu.RUnlock()

	res := make([]*Torrent, len(q.torrents))
	copy(res, q.torrents)
	return res
}

// FindByHash checks if torrent with infohash is in the queue
func (q *Queue) FindByHash(hash string) *Torrent {
	q.mu.RLock()
	defer q.mu.RUnlock()

	return q.findByHash(hash)
}

func (q *Queue) findByHash(hash string) *Torrent {
	for _, t := range q.torrents {
		if t.InfoHash() == hash {
			return t
		}
	}

	return nil
}

// FindByURI checks if torrent with infohash is in the queue
func (q *Queue) FindByURI(uri string) *Torrent {
	q.mu.RLock()
	defer q.mu.RUnlock()

	for _, t := range q.torrents {
		if t.torrentFile == uri {
			return t
		}
	}

	return nil
}

// Clean would cleanup torrents list,
// should be used in case of a service reload
func (q *Queue) Clean() {
	q.mu.Lock()
	defer q.mu.Unlock()

	q.torrents = []*Torrent{}
}

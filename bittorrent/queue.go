package bittorrent

// Queue represents list of torrents inside of a session
type Queue struct {
	s        *Service
	torrents []*Torrent
}

// NewQueue contructor for empty Queue
func NewQueue(s *Service) *Queue {
	return &Queue{
		s,
		[]*Torrent{},
	}
}

// Add torrent to the queue
func (q *Queue) Add(t *Torrent) bool {
	if q.FindByHash(t.InfoHash()) != nil {
		return false
	}

	q.torrents = append(q.torrents, t)
	return true
}

// Delete removes torrent from the queue
func (q *Queue) Delete(t *Torrent) bool {
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

// All returns all queue
func (q *Queue) All() []*Torrent {
	return q.torrents
}

// FindByHash checks if torrent with infohash is in the queue
func (q *Queue) FindByHash(hash string) *Torrent {
	for _, t := range q.torrents {
		if t.InfoHash() == hash {
			return t
		}
	}

	return nil
}

// Clean would cleanup torrents list,
// should be used in case of a service reload
func (q *Queue) Clean() {
	q.torrents = []*Torrent{}
}

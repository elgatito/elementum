package uid

import (
	"bytes"
	"sync"

	"github.com/anacrolix/missinggo/perf"
	"github.com/goccy/go-json"
	"github.com/op/go-logging"

	"github.com/elgatito/elementum/cache"
)

var (
	l = &Library{
		UIDs:   []*UniqueIDs{},
		Movies: []*Movie{},
		Shows:  []*Show{},

		Mu:         map[MutexType]*sync.RWMutex{},
		Containers: map[ContainerType]*LibraryContainer{},
	}

	log = logging.MustGetLogger("uid")
)

// Get returns singleton instance for Library
func Get() *Library {
	return l
}

// Store is storing Containers in cache for future quick-restore.
func (l *Library) Store() error {
	defer perf.ScopeTimer()()

	l.globalMutex.RLock()
	defer l.globalMutex.RUnlock()

	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(l.Containers); err != nil {
		log.Errorf("Could not encode library state: %s", err)
		return err
	}

	return cache.NewDBStore().SetBytes(cache.LibraryStateKey, buf.Bytes(), cache.LibraryStateExpire)
}

// Restore is restoring Containers from cache to have it in the memory.
func (l *Library) Restore() error {
	defer perf.ScopeTimer()()

	b, err := cache.NewDBStore().GetBytes(cache.LibraryStateKey)
	if err != nil {
		return err
	}

	dump := Library{}

	buf := bytes.NewBuffer(b)
	if err := json.NewDecoder(buf).Decode(&dump.Containers); err != nil {
		log.Errorf("Could not decode library state: %s", err)
		return err
	}

	l.globalMutex.Lock()
	defer l.globalMutex.Unlock()

	l.Containers = dump.Containers

	return nil
}

func (l *Library) GetContainer(id ContainerType) *LibraryContainer {
	l.globalMutex.RLock()
	if ret, ok := l.Containers[id]; ok {
		l.globalMutex.RUnlock()
		return ret
	}
	l.globalMutex.RUnlock()

	l.globalMutex.Lock()
	defer l.globalMutex.Unlock()

	l.Containers[id] = NewLibraryContainer()
	return l.Containers[id]
}

func (l *Library) GetMutex(id MutexType) *sync.RWMutex {
	l.globalMutex.RLock()
	if ret, ok := l.Mu[id]; ok {
		l.globalMutex.RUnlock()
		return ret
	}
	l.globalMutex.RUnlock()

	l.globalMutex.Lock()
	defer l.globalMutex.Unlock()

	l.Mu[id] = &sync.RWMutex{}
	return l.Mu[id]
}

// IsWatched returns watched state
func (e *Episode) IsWatched() bool {
	return e.UIDs != nil && e.UIDs.Playcount != 0
}

// IsWatched returns watched state
func (s *Show) IsWatched() bool {
	return s.UIDs != nil && s.UIDs.Playcount != 0
}

// IsWatched returns watched state
func (m *Movie) IsWatched() bool {
	return m.UIDs != nil && m.UIDs.Playcount != 0
}

func HasMovies() bool {
	return l != nil && l.Movies != nil && len(l.Movies) > 0
}

func HasShows() bool {
	return l != nil && l.Shows != nil && len(l.Shows) > 0
}

// GetUIDsFromKodi returns UIDs object for provided Kodi ID
func GetUIDsFromKodi(kodiID int) *UniqueIDs {
	if kodiID == 0 {
		return nil
	}

	mu := l.GetMutex(UIDsMutex)
	mu.RLock()
	defer mu.RUnlock()

	for _, u := range l.UIDs {
		if u.Kodi == kodiID {
			return u
		}
	}

	return nil
}

// GetShowForEpisode returns 'show' and 'episode'
func GetShowForEpisode(kodiID int) (*Show, *Episode) {
	if kodiID == 0 {
		return nil, nil
	}

	mu := l.GetMutex(ShowsMutex)
	mu.RLock()
	defer mu.RUnlock()

	for _, s := range l.Shows {
		for _, e := range s.Episodes {
			if e.UIDs.Kodi == kodiID {
				return s, e
			}
		}
	}

	return nil, nil
}

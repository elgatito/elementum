package tracker

import (
	"fmt"
	"time"

	"github.com/elgatito/elementum/cache"
	"github.com/elgatito/elementum/database"
)

// StateExpire is how long activities and list snapshots are kept as diff base.
const StateExpire = 30 * 24 * time.Hour

func cacheKey(p Provider, parts ...any) string {
	return fmt.Sprintf("com.tracker.%s.", p.ID()) + fmt.Sprint(parts...)
}

// ClearState drops every activity snapshot and list snapshot of a tracker, forcing a full sync.
func ClearState(id string) {
	database.GetCache().DeleteWithPrefix(database.CommonBucket, []byte(fmt.Sprintf("com.tracker.%s.", id)))
}

// ActivityState compares the current activities of a tracker with the ones saved by the last successful sync.
type ActivityState struct {
	key      string
	Previous *Activities
	Current  *Activities
}

// GetActivities reads the current activities of p. A non-empty source saves them right away,
// an empty source (the library sync) saves them only through Save, after the sync succeeded.
func GetActivities(p Provider, source string) (*ActivityState, error) {
	s := &ActivityState{
		key:      cacheKey(p, "activities.", source),
		Previous: NewActivities(),
	}
	_ = cache.NewDBStore().Get(s.key, s.Previous)

	var err error
	s.Current, err = p.LastActivities()
	if err == nil && source != "" {
		err = s.Save()
	}
	return s, err
}

// Save stores the current activities as the base for the next comparison.
func (s *ActivityState) Save() error {
	if s.Current == nil {
		return nil
	}
	return cache.NewDBStore().Set(s.key, s.Current, StateExpire)
}

// HasPrevious reports whether a previous sync stored activities.
func (s *ActivityState) HasPrevious() bool {
	return s.Previous != nil && len(s.Previous.Buckets) > 0
}

// Changed reports whether bucket b moved since the last sync. Unknown state counts as changed.
func (s *ActivityState) Changed(b Bucket) bool {
	if !s.HasPrevious() || s.Current == nil {
		return true
	}
	return s.Current.Buckets[b].After(s.Previous.Buckets[b])
}

// Any reports whether any bucket moved since the last sync.
func (s *ActivityState) Any() bool {
	if !s.HasPrevious() || s.Current == nil {
		return true
	}
	for b := range s.Current.Buckets {
		if s.Changed(b) {
			return true
		}
	}
	return false
}

// ListKind selects which list FetchList reads.
type ListKind string

const (
	KindWatchlist  ListKind = "watchlist"
	KindCollection ListKind = "collection"
	KindList       ListKind = "list"
)

// FetchList returns the list as stored by the previous successful fetch and the current one.
// On error the snapshot is kept, so callers must not diff against a failed fetch.
func FetchList(p Provider, kind ListKind, listID string, t MediaType, refresh bool) (prev, cur []*Item, err error) {
	store := cache.NewDBStore()
	key := cacheKey(p, "snapshot.", kind, ".", listID, ".", int(t))
	_ = store.Get(key, &prev)

	switch kind {
	case KindWatchlist:
		cur, err = p.Watchlist(t, refresh)
	case KindCollection:
		c, ok := p.(Collector)
		if !ok {
			return prev, nil, ErrUnsupported
		}
		cur, err = c.Collection(t, refresh)
	case KindList:
		l, ok := p.(ListReader)
		if !ok {
			return prev, nil, ErrUnsupported
		}
		cur, err = l.ListItems(listID, t, refresh)
	default:
		return prev, nil, fmt.Errorf("unknown list kind %q", kind)
	}

	if err == nil {
		_ = store.Set(key, cur, StateExpire)
	}
	return
}

// FetchWatched returns watched items as stored by the previous successful fetch and the current ones.
func FetchWatched(p Provider, t MediaType, refresh bool) (prev, cur []*WatchedItem, err error) {
	store := cache.NewDBStore()
	key := cacheKey(p, "snapshot.watched.", int(t))
	_ = store.Get(key, &prev)

	if cur, err = p.Watched(t, refresh); err == nil {
		_ = store.Set(key, cur, StateExpire)
	}
	return
}

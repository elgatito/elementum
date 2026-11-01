package tracker

import (
	"errors"
	"strconv"
	"sync"

	"github.com/elgatito/elementum/config"
)

var (
	mu        sync.RWMutex
	providers = map[string]Provider{}
)

// Register adds a tracker. It is called from main for every supported tracker.
func Register(p Provider) {
	mu.Lock()
	defer mu.Unlock()
	providers[p.ID()] = p
}

// Get returns a registered tracker by id, or nil.
func Get(id string) Provider {
	mu.RLock()
	defer mu.RUnlock()
	return providers[id]
}

// All returns every registered tracker.
func All() []Provider {
	mu.RLock()
	defer mu.RUnlock()
	ret := make([]Provider, 0, len(providers))
	for _, p := range providers {
		ret = append(ret, p)
	}
	return ret
}

// Selected returns the tracker chosen in settings, authorized or not, or nil.
func Selected() Provider {
	return Get(config.Get().TrackerProvider)
}

// Active returns the selected tracker when it is authorized, or nil.
func Active() Provider {
	if p := Selected(); p != nil && p.Authorized() {
		return p
	}
	return nil
}

// IsActive reports whether the tracker with this id is the active one.
func IsActive(id string) bool {
	p := Active()
	return p != nil && p.ID() == id
}

// HasCollection reports whether the active tracker has a collection.
func HasCollection() bool {
	_, ok := Active().(Collector)
	return ok
}

// AddTo adds one item to a Location of the active tracker. A missing list or an unsupported Location is skipped.
func AddTo(loc Location, listID int, item *Item) error {
	return write(true, loc, listID, item)
}

// RemoveFrom removes one item from a Location of the active tracker. A missing list or an unsupported Location is skipped.
func RemoveFrom(loc Location, listID int, item *Item) error {
	return write(false, loc, listID, item)
}

func write(add bool, loc Location, listID int, item *Item) error {
	p := Active()
	if p == nil {
		return ErrNotAuthorized
	}
	if loc == LocationList && listID == 0 {
		return nil
	}

	do := func(loc Location) error {
		if add {
			return p.Add(loc, strconv.Itoa(listID), []*Item{item})
		}
		return p.Remove(loc, strconv.Itoa(listID), []*Item{item})
	}
	err := do(loc)
	if errors.Is(err, ErrUnsupported) && loc != LocationWatchlist {
		// the watchlist is the one location every tracker has (Simkl has nothing else)
		log.Debugf("%s has no location %d, using the watchlist", p.Name(), loc)
		err = do(LocationWatchlist)
	}
	if errors.Is(err, ErrUnsupported) {
		log.Debugf("Skipping %s write for location %d: %s", p.Name(), loc, err)
		return nil
	} else if err != nil {
		log.Warningf("Could not update %s: %s", p.Name(), err)
	}
	return err
}

// SetWatched marks items watched or unwatched on the active tracker.
func SetWatched(items []*WatchedItem, watched bool) error {
	p := Active()
	if p == nil {
		return ErrNotAuthorized
	}
	if err := p.SetWatched(items, watched); err != nil {
		log.Warningf("Could not set watched state on %s: %s", p.Name(), err)
		return err
	}
	return nil
}

// Scrobble reports playback to the active tracker.
func Scrobble(action ScrobbleAction, item *Item, progress float64) {
	if p := Active(); p != nil {
		if err := p.Scrobble(action, item, progress); err != nil {
			log.Warningf("Scrobble %s to %s failed: %s", action, p.Name(), err)
		}
	}
}

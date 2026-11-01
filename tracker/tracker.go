// Package tracker defines the interface every external tracking service (Trakt, Simkl, MDBList)
// implements, so library sync and playback work with whichever one the user selected.
package tracker

import (
	"errors"
	"time"

	"github.com/op/go-logging"
)

var log = logging.MustGetLogger("tracker")

// MediaType is the kind of media an Item describes.
type MediaType int

const (
	MovieType MediaType = iota
	ShowType
	SeasonType
	EpisodeType
)

// Location is where an item lives on a tracker. Values match trakt_sync_*_location settings.
type Location int

const (
	LocationCollection Location = iota
	LocationWatchlist
	LocationList
)

// ScrobbleAction is a playback event reported to a tracker.
type ScrobbleAction string

const (
	ScrobbleStart ScrobbleAction = "start"
	ScrobblePause ScrobbleAction = "pause"
	ScrobbleStop  ScrobbleAction = "stop"
)

var (
	// ErrUnsupported is returned for features a tracker does not have. Callers treat it as a skip.
	ErrUnsupported = errors.New("not supported by this tracker")
	// ErrNotAuthorized is returned when the tracker has no valid credentials.
	ErrNotAuthorized = errors.New("tracker is not authorized")
	// ErrLocked is returned when the tracker account is locked.
	ErrLocked = errors.New("tracker account is locked")
)

// Provider is implemented by every tracker.
type Provider interface {
	ID() string   // used in setting ids and cache keys
	Name() string // display name
	// MinSyncInterval is the shortest allowed time between background syncs.
	MinSyncInterval() time.Duration

	Authorized() bool
	Authorize() error
	Deauthorize() error
	// RefreshToken renews the access token when it is close to expiry.
	RefreshToken() error

	LastActivities() (*Activities, error)
	Watchlist(t MediaType, refresh bool) ([]*Item, error)
	Watched(t MediaType, refresh bool) ([]*WatchedItem, error)

	// Add and Remove return ErrUnsupported for a Location the tracker lacks.
	Add(loc Location, listID string, items []*Item) error
	Remove(loc Location, listID string, items []*Item) error
	SetWatched(items []*WatchedItem, watched bool) error
	// Scrobble reports playback; progress is in percent.
	Scrobble(action ScrobbleAction, item *Item, progress float64) error
}

// Collector is implemented by trackers with a collection.
type Collector interface {
	Collection(t MediaType, refresh bool) ([]*Item, error)
}

// PlaybackReader is implemented by trackers that keep playback progress.
type PlaybackReader interface {
	Paused(t MediaType, refresh bool) ([]*PausedItem, error)
}

// ListReader is implemented by trackers with user lists.
type ListReader interface {
	Lists() ([]*List, error)
	ListItems(listID string, t MediaType, refresh bool) ([]*Item, error)
}

// HiddenReader is implemented by trackers that let users hide items.
type HiddenReader interface {
	Hidden(section string, refresh bool) ([]*Item, error)
}

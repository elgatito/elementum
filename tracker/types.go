package tracker

import (
	"fmt"
	"time"
)

//go:generate go tool msgp -o msgp.go -io=false -tests=false
//msgp:tag json
//msgp:timezone utc
//msgp:replace MediaType with:int

// IDs are external ids of an item. TMDB is the one Elementum resolves everything through.
type IDs struct {
	TMDB   int    `json:"tmdb"`
	IMDB   string `json:"imdb"`
	TVDB   int    `json:"tvdb"`
	Trakt  int    `json:"trakt"`  // set only by the Trakt adapter, library containers use it as an extra key
	Native string `json:"native"` // the tracker's own id, used for writes when present
}

// Item is a movie, show, season or episode. Season and episode items carry the show's IDs.
type Item struct {
	Type        MediaType `json:"type"`
	IDs         IDs       `json:"ids"`
	Title       string    `json:"title"`
	Year        int       `json:"year"`
	Season      int       `json:"season"`
	Episode     int       `json:"episode"`
	EpisodeTMDB int       `json:"episode_tmdb"` // TMDB id of the episode itself, when known
	ListedAt    time.Time `json:"listed_at"`
	UpdatedAt   time.Time `json:"updated_at"` // last change of the item's metadata on the tracker, zero if unknown
}

// Key identifies an item across trackers: TMDB first, then IMDB, then TVDB.
func (i *Item) Key() string {
	id := ""
	switch {
	case i.IDs.TMDB != 0:
		id = fmt.Sprintf("tmdb:%d", i.IDs.TMDB)
	case i.IDs.IMDB != "":
		id = "imdb:" + i.IDs.IMDB
	case i.IDs.TVDB != 0:
		id = fmt.Sprintf("tvdb:%d", i.IDs.TVDB)
	default:
		id = "native:" + i.IDs.Native
	}
	switch i.Type {
	case SeasonType:
		return fmt.Sprintf("%d:%s:%d", i.Type, id, i.Season)
	case EpisodeType:
		return fmt.Sprintf("%d:%s:%d:%d", i.Type, id, i.Season, i.Episode)
	}
	return fmt.Sprintf("%d:%s", i.Type, id)
}

// WatchedEpisode is one watched episode of a show.
type WatchedEpisode struct {
	Season        int       `json:"season"`
	Number        int       `json:"number"`
	Plays         int       `json:"plays"`
	LastWatchedAt time.Time `json:"last_watched_at"`
}

// WatchedItem is a watched movie or show. For writes it can also be a season or an episode.
type WatchedItem struct {
	Item
	Plays         int              `json:"plays"`
	LastWatchedAt time.Time        `json:"last_watched_at"`
	Episodes      []WatchedEpisode `json:"episodes"` // shows only
	Watched       bool             `json:"watched"`  // shows: whole show is watched
	KodiKey       uint64           `json:"-"`        // library bookkeeping for write-back
}

// PausedItem is a movie, or an episode carrying the show's IDs, with playback progress.
type PausedItem struct {
	Item
	Progress float64   `json:"progress"` // percent, 0-100
	Runtime  int       `json:"runtime"`  // seconds, 0 if unknown
	PausedAt time.Time `json:"paused_at"`
}

// List is a user list on a tracker.
type List struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	ItemCount int       `json:"item_count"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Bucket is one normalized "last changed" timestamp.
type Bucket = string

const (
	MoviesWatched       Bucket = "movies.watched"
	MoviesCollected     Bucket = "movies.collected"
	MoviesWatchlisted   Bucket = "movies.watchlisted"
	MoviesPaused        Bucket = "movies.paused"
	MoviesHidden        Bucket = "movies.hidden"
	EpisodesWatched     Bucket = "episodes.watched"
	EpisodesCollected   Bucket = "episodes.collected"
	EpisodesWatchlisted Bucket = "episodes.watchlisted"
	EpisodesPaused      Bucket = "episodes.paused"
	ShowsWatchlisted    Bucket = "shows.watchlisted"
	ShowsHidden         Bucket = "shows.hidden"
	SeasonsWatchlisted  Bucket = "seasons.watchlisted"
	SeasonsHidden       Bucket = "seasons.hidden"
	ListsUpdated        Bucket = "lists.updated"
)

// Activities are the last change timestamps of a tracker account. Buckets a tracker cannot report are absent.
type Activities struct {
	Buckets map[string]time.Time `json:"buckets"`
}

// NewActivities returns empty Activities.
func NewActivities() *Activities {
	return &Activities{Buckets: map[string]time.Time{}}
}

// Set stores t for every bucket in bs.
func (a *Activities) Set(t time.Time, bs ...Bucket) {
	for _, b := range bs {
		a.Buckets[b] = t
	}
}

// SetLatest stores t for every bucket in bs when it is newer than the stored value.
func (a *Activities) SetLatest(t time.Time, bs ...Bucket) {
	for _, b := range bs {
		if t.After(a.Buckets[b]) {
			a.Buckets[b] = t
		}
	}
}

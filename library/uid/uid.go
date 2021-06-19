package uid

import (
	"sync"
	"time"

	"github.com/elgatito/elementum/xbmc"
	"github.com/op/go-logging"
)

const (
	movieType   = "movie"
	showType    = "show"
	episodeType = "episode"

	trueType  = "true"
	falseType = "false"
)

const (
	// MovieType ...
	MovieType = iota
	// ShowType ...
	ShowType
	// SeasonType ...
	SeasonType
	// EpisodeType ...
	EpisodeType
)

// Status represents library bool statuses
type Status struct {
	IsOverall  bool
	IsMovies   bool
	IsShows    bool
	IsEpisodes bool
	IsTrakt    bool
	IsKodi     bool
}

// UniqueIDs represents all IDs for a library item
type UniqueIDs struct {
	MediaType int    `json:"media"`
	Kodi      int    `json:"kodi"`
	TMDB      int    `json:"tmdb"`
	TVDB      int    `json:"tvdb"`
	IMDB      string `json:"imdb"`
	Trakt     int    `json:"trakt"`
	Playcount int    `json:"playcount"`
}

// Movie represents Movie content type
type Movie struct {
	ID        int
	Title     string
	File      string
	Year      int
	DateAdded time.Time
	UIDs      *UniqueIDs
	XbmcUIDs  *xbmc.UniqueIDs
	Resume    *Resume
}

// Show represents Show content type
type Show struct {
	ID        int
	Title     string
	Year      int
	DateAdded time.Time
	Seasons   []*Season
	Episodes  []*Episode
	UIDs      *UniqueIDs
	XbmcUIDs  *xbmc.UniqueIDs
}

// Season represents Season content type
type Season struct {
	ID       int
	Title    string
	Season   int
	Episodes int
	UIDs     *UniqueIDs
	XbmcUIDs *xbmc.UniqueIDs
}

// Episode represents Episode content type
type Episode struct {
	ID        int
	Title     string
	Season    int
	Episode   int
	File      string
	DateAdded time.Time
	UIDs      *UniqueIDs
	XbmcUIDs  *xbmc.UniqueIDs
	Resume    *Resume
}

// Resume shows watched progress information
type Resume struct {
	Position float64 `json:"position"`
	Total    float64 `json:"total"`
}

// Library represents library
type Library struct {
	Mu lMutex

	// Stores all the unique IDs collected
	UIDs []*UniqueIDs

	Movies []*Movie
	Shows  []*Show

	WatchedTraktMovies []uint64
	WatchedTraktShows  []uint64

	Pending Status
	Running Status
}

type lMutex struct {
	UIDs   sync.RWMutex
	Movies sync.RWMutex
	Shows  sync.RWMutex
	Trakt  sync.RWMutex
}

var (
	l = &Library{
		UIDs:   []*UniqueIDs{},
		Movies: []*Movie{},
		Shows:  []*Show{},

		WatchedTraktMovies: []uint64{},
		WatchedTraktShows:  []uint64{},
	}

	log = logging.MustGetLogger("uid")
)

// Get returns singleton instance for Library
func Get() *Library {
	return l
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

	l.Mu.UIDs.Lock()
	defer l.Mu.UIDs.Unlock()

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

	l.Mu.Shows.RLock()
	defer l.Mu.Shows.RUnlock()

	for _, s := range l.Shows {
		for _, e := range s.Episodes {
			if e.UIDs.Kodi == kodiID {
				return s, e
			}
		}
	}

	return nil, nil
}

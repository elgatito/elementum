package library

import (
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/elgatito/elementum/xbmc"
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

// Library represents library
type Library struct {
	mu lMutex

	// Stores all the unique IDs collected
	UIDs []*UniqueIDs

	Movies []*Movie
	Shows  []*Show

	WatchedTrakt []uint64

	Pending Status
	Running Status
}

type lMutex struct {
	UIDs   sync.RWMutex
	Movies sync.RWMutex
	Shows  sync.RWMutex
	Trakt  sync.RWMutex
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

// DBItem ...
type DBItem struct {
	ID       int `json:"id"`
	State    int `json:"state"`
	Type     int `json:"type"`
	TVShowID int `json:"showid"`
}

type removedEpisode struct {
	ID       int
	ShowID   int
	ShowName string
	Season   int
	Episode  int
}

// ToString ...
func (r *Resume) ToString() string {
	if r.Position == 0 {
		return ""
	}

	t1 := time.Now()
	t2 := t1.Add(time.Duration(int(r.Position)) * time.Second)

	diff := t2.Sub(t1)
	return fmt.Sprintf("%d:%02d:%02d", int(diff.Hours()), int(math.Mod(diff.Minutes(), 60)), int(math.Mod(diff.Seconds(), 60)))
}

// Reset ...
func (r *Resume) Reset() {
	log.Debugf("Resetting stored resume position")
	r.Position = 0
	r.Total = 0
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

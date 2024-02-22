package uid

import (
	"sync"
	"time"

	"github.com/elgatito/elementum/xbmc"
)

type MutexType int
type ContainerType int
type MediaItemType int
type ScraperType int

const (
	UIDsMutex MutexType = iota
	MoviesMutex
	ShowsMutex
	TraktMutex
)

const (
	WatchlistedMoviesContainer ContainerType = iota
	WatchedMoviesContainer
	CollectedMoviesContainer
	UserlistedMoviesContainer

	WatchlistedShowsContainer
	WatchedShowsContainer
	CollectedShowsContainer
	UserlistedShowsContainer
)

const (
	MovieType MediaItemType = iota
	ShowType
	SeasonType
	EpisodeType
)

const (
	TVDBScraper ScraperType = iota
	TMDBScraper
	TraktScraper
	IMDBScraper
)

// Status represents library bool statuses
type Status struct {
	IsOverall    bool
	IsMovies     bool
	IsShows      bool
	IsEpisodes   bool
	IsTrakt      bool
	IsKodi       bool
	IsKodiMovies bool
	IsKodiShows  bool
}

// UniqueIDs represents all IDs for a library item
type UniqueIDs struct {
	MediaType MediaItemType `json:"media"`
	Kodi      int           `json:"kodi"`
	TMDB      int           `json:"tmdb"`
	TVDB      int           `json:"tvdb"`
	IMDB      string        `json:"imdb"`
	Trakt     int           `json:"trakt"`
	Playcount int           `json:"playcount"`
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

type LibraryContainer struct {
	Mu sync.RWMutex

	Items map[uint64]struct{}
}

type Library struct {
	Mu map[MutexType]*sync.RWMutex

	// Stores all the unique IDs collected
	UIDs []*UniqueIDs

	Movies []*Movie
	Shows  []*Show

	Containers map[ContainerType]*LibraryContainer

	Pending Status
	Running Status

	globalMutex sync.RWMutex
}

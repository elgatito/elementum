package playcount

import (
	"fmt"
	"sync"

	"github.com/cespare/xxhash"
)

const (
	// TVDBScraper ...
	TVDBScraper = iota
	// TMDBScraper ...
	TMDBScraper
	// TraktScraper ...
	TraktScraper
	// IMDBScraper ...
	IMDBScraper
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

// Watched stores all "watched" items
var (
	// Mu is a global lock for Playcount package
	Mu = sync.RWMutex{}

	// Watched contains uint64 hashed bools
	Watched = []uint64{}
)

// WatchedState just a simple bool with Int() conversion
type WatchedState bool

func searchForKey(k uint64) WatchedState {
	Mu.RLock()
	defer Mu.RUnlock()

	for _, v := range Watched {
		if v == k {
			return true
		}
	}

	return false
}

// GetWatchedMovieByTMDB checks whether item is watched
func GetWatchedMovieByTMDB(id int) (ret WatchedState) {
	return searchForKey(xxhash.Sum64String(fmt.Sprintf("%d_%d_%d", MovieType, TMDBScraper, id)))
}

// GetWatchedMovieByIMDB checks whether item is watched
func GetWatchedMovieByIMDB(id string) (ret WatchedState) {
	return searchForKey(xxhash.Sum64String(fmt.Sprintf("%d_%d_%s", MovieType, IMDBScraper, id)))
}

// GetWatchedMovieByTrakt checks whether item is watched
func GetWatchedMovieByTrakt(id int) (ret WatchedState) {
	return searchForKey(xxhash.Sum64String(fmt.Sprintf("%d_%d_%d", MovieType, TraktScraper, id)))
}

// GetWatchedShowByTMDB checks whether item is watched
func GetWatchedShowByTMDB(id int) (ret WatchedState) {
	return searchForKey(xxhash.Sum64String(fmt.Sprintf("%d_%d_%d", ShowType, TMDBScraper, id)))
}

// GetWatchedShowByTVDB checks whether item is watched
func GetWatchedShowByTVDB(id int) (ret WatchedState) {
	return searchForKey(xxhash.Sum64String(fmt.Sprintf("%d_%d_%d", ShowType, TVDBScraper, id)))
}

// GetWatchedShowByTrakt checks whether item is watched
func GetWatchedShowByTrakt(id int) (ret WatchedState) {
	return searchForKey(xxhash.Sum64String(fmt.Sprintf("%d_%d_%d", ShowType, TraktScraper, id)))
}

// GetWatchedSeasonByTMDB checks whether item is watched
func GetWatchedSeasonByTMDB(id int, season int) (ret WatchedState) {
	return searchForKey(xxhash.Sum64String(fmt.Sprintf("%d_%d_%d_%d", SeasonType, TMDBScraper, id, season)))
}

// GetWatchedSeasonByTVDB checks whether item is watched
func GetWatchedSeasonByTVDB(id int, season, episode int) (ret WatchedState) {
	return searchForKey(xxhash.Sum64String(fmt.Sprintf("%d_%d_%d_%d", SeasonType, TVDBScraper, id, season)))
}

// GetWatchedSeasonByTrakt checks whether item is watched
func GetWatchedSeasonByTrakt(id int, season int) (ret WatchedState) {
	return searchForKey(xxhash.Sum64String(fmt.Sprintf("%d_%d_%d_%d", SeasonType, TraktScraper, id, season)))
}

// GetWatchedEpisodeByTMDB checks whether item is watched
func GetWatchedEpisodeByTMDB(id int, season, episode int) (ret WatchedState) {
	return searchForKey(xxhash.Sum64String(fmt.Sprintf("%d_%d_%d_%d_%d", EpisodeType, TMDBScraper, id, season, episode)))
}

// GetWatchedEpisodeByTVDB checks whether item is watched
func GetWatchedEpisodeByTVDB(id int, season, episode int) (ret WatchedState) {
	return searchForKey(xxhash.Sum64String(fmt.Sprintf("%d_%d_%d_%d_%d", EpisodeType, TVDBScraper, id, season, episode)))
}

// GetWatchedEpisodeByTrakt checks whether item is watched
func GetWatchedEpisodeByTrakt(id int, season, episode int) (ret WatchedState) {
	return searchForKey(xxhash.Sum64String(fmt.Sprintf("%d_%d_%d_%d_%d", EpisodeType, TraktScraper, id, season, episode)))
}

// Int converts bool to int
func (w WatchedState) Int() (r int) {
	if w {
		r = 1
	}

	return
}

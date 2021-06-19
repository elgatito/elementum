package scrape

import (
	"fmt"
	"math/rand"
	"strconv"
	"strings"
	"time"

	"github.com/anacrolix/missinggo/perf"
	"github.com/jmcvetta/napping"
	"github.com/op/go-logging"

	"github.com/elgatito/elementum/bittorrent"
	"github.com/elgatito/elementum/cache"
	"github.com/elgatito/elementum/config"
	"github.com/elgatito/elementum/database"
	"github.com/elgatito/elementum/library"
	"github.com/elgatito/elementum/library/uid"
	"github.com/elgatito/elementum/providers"
	"github.com/elgatito/elementum/tmdb"
	"github.com/elgatito/elementum/trakt"
	"github.com/elgatito/elementum/util"
	"github.com/elgatito/elementum/xbmc"
)

const (
	timeFormat = "2006-01-02T15:04:05.000Z"
)

const (
	// StrategyEachProvider ...
	StrategyEachProvider = iota
	// StrategyOverall ...
	StrategyOverall
	// Strategy4k ...
	Strategy4k
	// Strategy1080p ...
	Strategy1080p
)

var (
	log = logging.MustGetLogger("proxy")

	updateTicker *time.Ticker
	closer       = util.Event{}

	libraryUpdated = false
)

// Stop cancels active timeout
func Stop() {
	defer closer.Set()

	if updateTicker == nil {
		return
	}

	updateTicker.Stop()
}

// Start initiates timeout for updates
func Start() {
	updateTicker = time.NewTicker(180 * time.Second)
	go runUpdater()

	go func() {
		closing := closer.C()

		select {
		case <-closing:
			log.Info("Closing scraper...")
			return
		case <-updateTicker.C:
			go runUpdater()
			updateTicker.Stop()
		}
	}()
}

func runUpdater() {
	if !config.Get().AutoScrapeEnabled {
		return
	}

	cacheDB := database.GetCache()

	// Check if previous execution was in less then N hours, filled in settings.
	if last, err := cacheDB.GetCached(database.CommonBucket, cache.ScraperLastExecutionKey); err == nil && last != "" {
		if lastTime, err := time.Parse(timeFormat, last); err == nil && lastTime.Add(time.Hour*time.Duration(config.Get().AutoScrapePerHours)).After(time.Now()) {
			return
		}
	}

	defer func() {
		// Save this execution time
		cacheDB.SetCached(database.CommonBucket, cache.ScraperLastExecutionExpire, cache.ScraperLastExecutionKey, time.Now().Format(timeFormat))

		// Update Kodi library if needed
		if libraryUpdated {
			xbmc.VideoLibraryScanDirectory(library.MoviesLibraryPath(), true)
		}
	}()

	defer perf.ScopeTimer()()

	movies, err := GetMovies()
	if err != nil {
		return
	}

	strategy := config.Get().AutoScrapeStrategy
	expect := config.Get().AutoScrapeStrategyExpect
	authCompleted := false
	libraryUpdated = false

	for _, m := range movies {
		if m == nil || m.Movie == nil || m.Movie.IDs == nil || m.Movie.IDs.TMDB == 0 {
			continue
		}

		keyExists := GetMovieExistsKey(m.Movie.IDs.TMDB)

		// If Movie is already checked and is processed - skip it
		if v, err := cacheDB.GetCachedBool(database.CommonBucket, keyExists); err == nil && v {
			addMovieToLibrary(m.Movie)
			continue
		}

		// Make sure we have working login sessions, because later we will not login
		if !authCompleted {
			getTorrents(m.Movie, true)
			authCompleted = true
		}

		log.Debugf("Searching for movie: %s ", m.Movie.Title)
		torrents := getTorrents(m.Movie, false)
		log.Debugf("Found torrents: %d ", len(torrents))
		if len(torrents) == 0 {
			continue
		} else if strategy == StrategyEachProvider && countEachProvider(torrents) < expect {
			continue
		} else if strategy == StrategyOverall && countOverall(torrents) < expect {
			continue
		} else if strategy == Strategy4k && countResolution(torrents, bittorrent.Resolution4k) < expect {
			continue
		} else if strategy == Strategy1080p && countResolution(torrents, bittorrent.Resolution1080p) < expect {
			continue
		}

		// We checked the strategy and expectation and movie is considered active
		cacheDB.SetCachedBool(database.CommonBucket, cache.ScraperMovieExistsExpire, keyExists, true)
		addMovieToLibrary(m.Movie)

		// Just sleep a little
		time.Sleep(time.Duration(rand.Intn(5)+config.Get().AutoScrapeInterval) * time.Second)
	}
}

// Check minimum number of torrents for each provider
func countEachProvider(torrents []*bittorrent.TorrentFile) int {
	found := map[string]int{}

	for _, t := range torrents {
		for _, p := range strings.Split(t.Provider, ", ") {
			found[p]++
		}
	}

	max := 0
	for _, c := range found {
		if max == 0 || c < max {
			max = c
		}
	}

	return max
}

// Check overall number of found torrents
func countOverall(torrents []*bittorrent.TorrentFile) (res int) {
	return len(torrents)
}

// Check number of torrents with specific resolution
func countResolution(torrents []*bittorrent.TorrentFile, resolution int) (res int) {
	for _, t := range torrents {
		if t.Resolution == resolution {
			res++
		}
	}

	return
}

func addMovieToLibrary(m *trakt.Movie) {
	// If movie is positive and we don't have it in the library - add it.
	if !config.Get().AutoScrapeLibraryEnabled || uid.IsAddedToLibrary(strconv.Itoa(m.IDs.TMDB), library.MovieType) {
		return
	}

	library.AddMovie(strconv.Itoa(m.IDs.TMDB), false)
	if config.Get().TraktToken != "" && config.Get().TraktSyncAddedMovies {
		go trakt.SyncAddedItem("movies", strconv.Itoa(m.IDs.TMDB), config.Get().TraktSyncAddedMoviesLocation)
	}

	libraryUpdated = true
}

// GetMovies Gets list of trending movies from Trakt
func GetMovies() (movies []*trakt.Movies, err error) {
	cacheStore := cache.NewDBStore()
	key := fmt.Sprintf(cache.ScraperMoviesListKey, config.Get().AutoScrapeLimitMovies)

	if err := cacheStore.Get(key, &movies); err != nil || len(movies) == 0 {
		defer perf.ScopeTimer()()

		params := napping.Params{
			"page":     "1",
			"limit":    strconv.Itoa(config.Get().AutoScrapeLimitMovies),
			"extended": "full",
		}.AsUrlValues()
		resp, err := trakt.Get("movies/trending", params)

		if err != nil {
			return movies, err
		} else if resp.Status() != 200 {
			return movies, fmt.Errorf("Bad status getting movies: %d", resp.Status())
		}

		if errUnm := resp.Unmarshal(&movies); errUnm != nil {
			log.Warning(errUnm)
		}

		cacheStore.Set(key, movies, cache.ScraperMoviesListExpire)
	}

	return movies, nil
}

// Search for Movie on connected providers
func getTorrents(m *trakt.Movie, withAuth bool) []*bittorrent.TorrentFile {
	movie := tmdb.GetMovieByID(strconv.Itoa(m.IDs.TMDB), config.Get().Language)
	if movie == nil {
		return nil
	}

	searchers := providers.GetMovieSearchers()
	if len(searchers) == 0 {
		return nil
	}

	return providers.SearchMovieSilent(searchers, movie, withAuth)
}

// GetMovieExistsKey ...
func GetMovieExistsKey(tmdbID int) string {
	return fmt.Sprintf(cache.ScraperMovieExistsKey, tmdbID, config.Get().AutoScrapeStrategy, config.Get().AutoScrapeLibraryEnabled)
}

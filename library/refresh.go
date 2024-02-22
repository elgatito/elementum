package library

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/anacrolix/missinggo/perf"
	"github.com/cespare/xxhash"
	"github.com/karrick/godirwalk"

	"github.com/elgatito/elementum/cache"
	"github.com/elgatito/elementum/config"
	"github.com/elgatito/elementum/library/playcount"
	"github.com/elgatito/elementum/library/uid"
	"github.com/elgatito/elementum/tmdb"
	"github.com/elgatito/elementum/trakt"
	"github.com/elgatito/elementum/util"
	"github.com/elgatito/elementum/xbmc"
)

var (
	movieRegexp = regexp.MustCompile(`^plugin://plugin.video.elementum.*/movie/\w+/(\d+)`)
	showRegexp  = regexp.MustCompile(`^plugin://plugin.video.elementum.*/show/\w+/(\d+)/(\d+)/(\d+)`)
)

// RefreshOnScan is launched when scan is finished
func RefreshOnScan() error {
	l := uid.Get()
	l.Pending.IsOverall = true
	l.Running.IsKodi = false

	return nil
}

// RefreshKodi runs Kodi library refresh
func RefreshKodi() error {
	l := uid.Get()
	if l.Running.IsKodi {
		return nil
	}

	l.Running.IsKodi = true
	l.Pending.IsKodi = false
	if xbmcHost, err := xbmc.GetLocalXBMCHost(); xbmcHost != nil && err == nil {
		xbmcHost.VideoLibraryScan()
	}
	l.Running.IsKodi = false

	return nil
}

// Refresh is updating library from Kodi
func Refresh() error {
	l := uid.Get()
	if l.Running.IsTrakt || !config.Get().LibraryEnabled {
		return nil
	}

	l.Pending.IsOverall = false
	l.Running.IsOverall = true
	defer func() {
		l.Running.IsOverall = false
		util.FreeMemoryGC()
	}()

	now := time.Now()

	if err := RefreshMovies(); err != nil {
		log.Debugf("RefreshMovies got an error: %v", err)
	}
	if err := RefreshShows(); err != nil {
		log.Debugf("RefreshShows got an error: %v", err)
	}

	log.Debugf("Library refresh finished in %s", time.Since(now))
	return nil
}

// RefreshMovies updates movies in the library
func RefreshMovies() error {
	xbmcHost, err := xbmc.GetLocalXBMCHost()
	if xbmcHost == nil || err != nil {
		return err
	}

	l := uid.Get()
	if l.Running.IsMovies || l.Running.IsKodi || !config.Get().LibraryEnabled || !config.Get().LibrarySyncEnabled || (!config.Get().LibrarySyncPlaybackEnabled && xbmcHost.PlayerIsPlaying()) {
		return nil
	}

	l.Pending.IsMovies = false
	l.Running.IsMovies = true

	defer func() {
		RefreshUIDs()
		l.Store()
		l.Running.IsMovies = false
	}()

	started := time.Now()
	movies, err := xbmcHost.VideoLibraryGetMovies()
	if err != nil {
		return err
	} else if movies != nil && movies.Limits != nil && movies.Limits.Total == 0 {
		return nil
	} else if movies == nil || movies.Movies == nil {
		return errors.New("Could not fetch Movies from Kodi")
	}

	defer func() {
		log.Debugf("Fetched %d movies from Kodi Library in %s", len(movies.Movies), time.Since(started))
	}()

	mu := l.GetMutex(uid.MoviesMutex)
	mu.Lock()

	l.Movies = make([]*uid.Movie, 0, len(movies.Movies))
	for _, m := range movies.Movies {
		m.UniqueIDs.Kodi = m.ID
		if m.UniqueIDs.IMDB == "" && m.IMDBNumber != "" && strings.HasPrefix(m.IMDBNumber, "tt") {
			m.UniqueIDs.IMDB = m.IMDBNumber
		}

		lm := &uid.Movie{
			ID:        m.ID,
			Title:     m.Title,
			File:      m.File,
			Year:      m.Year,
			DateAdded: m.DateAdded.Time,
			Resume:    &uid.Resume{},
			UIDs:      &uid.UniqueIDs{Kodi: m.ID, Playcount: m.PlayCount},
			XbmcUIDs:  &m.UniqueIDs,
		}

		if m.Resume != nil {
			lm.Resume.Position = m.Resume.Position
			lm.Resume.Total = m.Resume.Total
		}

		l.Movies = append(l.Movies, lm)
	}

	for _, m := range l.Movies {
		parseUniqueID(MovieType, m.UIDs, m.XbmcUIDs, m.File, m.Year)
	}
	mu.Unlock()

	return nil
}

// RefreshShows updates shows in the library
func RefreshShows() error {
	xbmcHost, err := xbmc.GetLocalXBMCHost()
	if xbmcHost == nil || err != nil {
		return err
	}

	l := uid.Get()
	if l.Running.IsShows || l.Running.IsKodi || !config.Get().LibraryEnabled || !config.Get().LibrarySyncEnabled || (!config.Get().LibrarySyncPlaybackEnabled && xbmcHost.PlayerIsPlaying()) {
		return nil
	}

	l.Pending.IsShows = false
	l.Running.IsShows = true

	defer func() {
		RefreshUIDs()
		l.Store()
		l.Running.IsShows = false
	}()

	started := time.Now()
	shows, err := xbmcHost.VideoLibraryGetShows()
	if err != nil {
		return err
	} else if shows != nil && shows.Limits != nil && shows.Limits.Total == 0 {
		return nil
	} else if shows == nil || shows.Shows == nil {
		return errors.New("Could not fetch Shows from Kodi")
	}

	defer func() {
		log.Debugf("Fetched %d shows from Kodi Library in %s", len(shows.Shows), time.Since(started))
	}()

	mu := l.GetMutex(uid.ShowsMutex)
	mu.Lock()

	l.Shows = make([]*uid.Show, 0, len(shows.Shows))
	for _, s := range shows.Shows {
		s.UniqueIDs.Kodi = s.ID
		if s.UniqueIDs.IMDB == "" && s.IMDBNumber != "" && strings.HasPrefix(s.IMDBNumber, "tt") {
			s.UniqueIDs.IMDB = s.IMDBNumber
		}

		l.Shows = append(l.Shows, &uid.Show{
			ID:        s.ID,
			Title:     s.Title,
			Seasons:   []*uid.Season{},
			Episodes:  []*uid.Episode{},
			Year:      s.Year,
			DateAdded: s.DateAdded.Time,
			UIDs:      &uid.UniqueIDs{Kodi: s.ID, Playcount: s.PlayCount},
			XbmcUIDs:  &s.UniqueIDs,
		})

		parseUniqueID(ShowType, l.Shows[len(l.Shows)-1].UIDs, l.Shows[len(l.Shows)-1].XbmcUIDs, "", l.Shows[len(l.Shows)-1].Year)
	}
	mu.Unlock()

	if err := RefreshSeasons(); err != nil {
		log.Debugf("RefreshSeasons got an error: %v", err)
	}
	if err := RefreshEpisodes(); err != nil {
		log.Debugf("RefreshEpisodes got an error: %v", err)
	}

	// TODO: This needs refactor to avoid setting global Lock on processing,
	// should use temporary container to process and then sync to Shows
	mu.Lock()
	for _, show := range l.Shows {
		// Step 1: try to get information from what we get from Kodi
		parseUniqueID(ShowType, show.UIDs, show.XbmcUIDs, "", show.Year)

		// Step 2: if TMDB not found - try to find it from episodes
		if show.UIDs.TMDB == 0 {
			for _, e := range show.Episodes {
				if !strings.HasSuffix(e.File, ".strm") {
					continue
				}

				u := &uid.UniqueIDs{}
				parseUniqueID(EpisodeType, u, e.XbmcUIDs, e.File, 0)
				if u.TMDB != 0 {
					show.UIDs.TMDB = u.TMDB
					break
				}
			}
		}

		if show.UIDs.TMDB == 0 {
			continue
		}

		for _, s := range show.Seasons {
			s.UIDs.TMDB = show.UIDs.TMDB
			s.UIDs.TVDB = show.UIDs.TVDB
			s.UIDs.IMDB = show.UIDs.IMDB

			parseUniqueID(SeasonType, s.UIDs, s.XbmcUIDs, "", 0)
		}
		for _, e := range show.Episodes {
			e.UIDs.TMDB = show.UIDs.TMDB
			e.UIDs.TVDB = show.UIDs.TVDB
			e.UIDs.IMDB = show.UIDs.IMDB

			parseUniqueID(EpisodeType, e.UIDs, e.XbmcUIDs, "", 0)
		}
	}
	mu.Unlock()

	return nil
}

// RefreshSeasons updates seasons list for selected show in the library
func RefreshSeasons() error {
	xbmcHost, err := xbmc.GetLocalXBMCHost()
	if xbmcHost == nil || err != nil {
		return err
	}

	started := time.Now()
	l := uid.Get()

	// Collect all shows IDs for possibly doing one-by-one calls to Kodi
	mu := l.GetMutex(uid.ShowsMutex)
	mu.Lock()
	shows := make([]int, 0, len(l.Shows))
	for _, s := range l.Shows {
		shows = append(shows, s.ID)
	}
	mu.Unlock()

	seasons, err := xbmcHost.VideoLibraryGetAllSeasons(shows)
	if err != nil {
		return err
	} else if seasons == nil || seasons.Seasons == nil {
		return errors.New("Could not fetch Seasons from Kodi")
	}

	defer func() {
		log.Debugf("Fetched %d seasons from Kodi Library in %s", len(seasons.Seasons), time.Since(started))
	}()

	mu.Lock()
	cleanupCheck := map[int]bool{}
	for _, s := range seasons.Seasons {
		c, err := uid.FindShowByKodi(s.TVShowID)
		if err != nil || c == nil || c.Seasons == nil {
			continue
		}

		if _, ok := cleanupCheck[s.TVShowID]; !ok {
			c.Seasons = []*uid.Season{}
			cleanupCheck[s.TVShowID] = true
		}

		s.UniqueIDs.Kodi = s.ID

		c.Seasons = append(c.Seasons, &uid.Season{
			ID:       s.ID,
			Title:    s.Title,
			Season:   s.Season,
			Episodes: s.Episodes,
			UIDs:     &uid.UniqueIDs{Kodi: s.ID, Playcount: s.PlayCount},
			XbmcUIDs: &s.UniqueIDs,
		})
	}
	mu.Unlock()

	return nil
}

// RefreshEpisodes updates episodes list for selected show in the library
func RefreshEpisodes() error {
	l := uid.Get()
	if !l.Running.IsShows && len(pendingShows) == 0 {
		return nil
	}

	l.Pending.IsEpisodes = false
	l.Running.IsEpisodes = true

	defer func() {
		l.Running.IsEpisodes = false
	}()

	xbmcHost, err := xbmc.GetLocalXBMCHost()
	if xbmcHost == nil || err != nil {
		return err
	}

	started := time.Now()

	mu := l.GetMutex(uid.ShowsMutex)

	var shows []int
	if len(pendingShows) != 0 {
		defer func() {
			RefreshUIDs()
		}()

		shows = make([]int, 0, len(pendingShows))
		for i := range pendingShows {
			shows = append(shows, i)
		}
		pendingShows = map[int]bool{}
	} else {
		mu.Lock()
		shows = make([]int, 0, len(l.Shows))
		for _, i := range l.Shows {
			shows = append(shows, i.XbmcUIDs.Kodi)
		}
		mu.Unlock()
	}

	episodes, err := xbmcHost.VideoLibraryGetAllEpisodes(shows)
	if err != nil {
		return err
	} else if episodes == nil || episodes.Episodes == nil {
		return errors.New("Could not fetch Episodes from Kodi")
	}

	defer func() {
		log.Debugf("Fetched %d episodes from Kodi Library in %s", len(episodes.Episodes), time.Since(started))
	}()

	mu.Lock()
	cleanupCheck := map[int]bool{}
	for _, e := range episodes.Episodes {
		c, err := uid.FindShowByKodi(e.TVShowID)
		if err != nil || c == nil || c.Episodes == nil {
			continue
		}

		if _, ok := cleanupCheck[e.TVShowID]; !ok {
			c.Episodes = []*uid.Episode{}
			cleanupCheck[e.TVShowID] = true
		}

		e.UniqueIDs.Kodi = e.ID
		e.UniqueIDs.TMDB = ""
		e.UniqueIDs.TVDB = ""
		e.UniqueIDs.Trakt = ""
		e.UniqueIDs.Unknown = ""

		c.Episodes = append(c.Episodes, &uid.Episode{
			ID:        e.ID,
			Title:     e.Title,
			Season:    e.Season,
			Episode:   e.Episode,
			File:      e.File,
			DateAdded: e.DateAdded.Time,
			Resume:    &uid.Resume{},
			UIDs:      &uid.UniqueIDs{Kodi: e.ID, Playcount: e.PlayCount},
			XbmcUIDs:  &e.UniqueIDs,
		})

		if e.Resume != nil {
			c.Episodes[len(c.Episodes)-1].Resume.Position = e.Resume.Position
			c.Episodes[len(c.Episodes)-1].Resume.Total = e.Resume.Total
		}
	}
	mu.Unlock()

	return nil
}

// RefreshMovie ...
func RefreshMovie(kodiID, action int) {
	uids := uid.GetUIDsFromKodi(kodiID)
	if uids == nil || uids.TMDB == 0 {
		return
	}
	PlanMoviesUpdate()

	defer RefreshUIDs()

	mu := uid.Get().GetMutex(uid.MoviesMutex)

	if action == ActionDelete || action == ActionSafeDelete {
		if action == ActionDelete {
			if _, _, err := RemoveMovie(uids.TMDB, false); err != nil {
				log.Warning("Nothing left to remove from Elementum")
			}
		}

		l := uid.Get()
		mu.Lock()
		foundIndex := -1
		for i, m := range l.Movies {
			if m.UIDs.Kodi == kodiID {
				foundIndex = i
				break
			}
		}
		if foundIndex != -1 {
			l.Movies = append(l.Movies[:foundIndex], l.Movies[foundIndex+1:]...)
		}
		mu.Unlock()
	} else if action == ActionUpdate {
		deleteDBItem(uids.TMDB, ShowType, false, false)
	}
}

// RefreshShow ...
func RefreshShow(kodiID, action int) {
	uids := uid.GetUIDsFromKodi(kodiID)
	if uids == nil || uids.TMDB == 0 {
		return
	}
	PlanShowUpdate(uids.Kodi)

	defer RefreshUIDs()

	l := uid.Get()
	mu := l.GetMutex(uid.ShowsMutex)

	if action == ActionDelete || action == ActionSafeDelete {
		if action == ActionDelete {
			id := strconv.Itoa(uids.TMDB)
			if _, _, err := RemoveShow(id, false); err != nil {
				log.Warning("Nothing left to remove from Elementum")
			}
		}

		mu.Lock()
		foundIndex := -1
		for i, s := range l.Shows {
			if s.UIDs.Kodi == kodiID {
				foundIndex = i
				break
			}
		}
		if foundIndex != -1 {
			l.Shows = append(l.Shows[:foundIndex], l.Shows[foundIndex+1:]...)
		}
		mu.Unlock()
	} else if action == ActionUpdate {
		deleteDBItem(uids.TMDB, ShowType, false, false)
	}
}

// RefreshEpisode ...
func RefreshEpisode(kodiID, action int) {
	s, e := uid.GetLibraryEpisode(kodiID)
	if s == nil || e == nil {
		return
	}
	PlanShowUpdate(s.UIDs.Kodi)

	defer RefreshUIDs()

	l := uid.Get()
	mu := l.GetMutex(uid.ShowsMutex)

	if action == ActionDelete || action == ActionSafeDelete {
		if action == ActionDelete {
			RemoveEpisode(e.UIDs.TMDB, s.UIDs.TMDB, e.Season, e.Episode)
		}

		mu.Lock()
		sIndex := -1
		eIndex := -1
		for i, sh := range l.Shows {
			if sh.ID == s.UIDs.Kodi {
				sIndex = i
				break
			}
		}

		for i, e := range l.Shows[sIndex].Episodes {
			if e.ID == kodiID {
				eIndex = i
				break
			}
		}
		if eIndex != -1 {
			l.Shows[sIndex].Episodes = append(l.Shows[sIndex].Episodes[:eIndex], l.Shows[sIndex].Episodes[eIndex+1:]...)
		}
		mu.Unlock()
	} else if action == ActionUpdate {
		updateDBItem(e.UIDs.TMDB, StateActive, EpisodeType, s.UIDs.TMDB)
	}
}

// RefreshUIDs updates unique IDs for each library item
// This collects already saved UIDs for easier access
func RefreshUIDs() error {
	return RefreshUIDsRunner(false)
}

// RefreshUIDsRunner completes RefreshUIDs target
func RefreshUIDsRunner(force bool) error {
	l := uid.Get()
	if !force && (l.Running.IsTrakt || l.Running.IsKodi) {
		return nil
	}

	defer perf.ScopeTimer()()

	now := time.Now()

	moviesContainer := l.GetContainer(uid.WatchedMoviesContainer)
	showsContainer := l.GetContainer(uid.WatchedShowsContainer)

	mu := l.GetMutex(uid.UIDsMutex)
	mu.Lock()
	defer mu.Unlock()

	playcount.Mu.Lock()
	defer playcount.Mu.Unlock()

	playcount.Watched = map[uint64]playcount.WatchedState{}
	l.UIDs = []*uid.UniqueIDs{}

	moviesContainer.Mu.RLock()
	for k := range moviesContainer.Items {
		playcount.Watched[k] = true
	}
	moviesContainer.Mu.RUnlock()

	showsContainer.Mu.RLock()
	for k := range showsContainer.Items {
		playcount.Watched[k] = true
	}
	showsContainer.Mu.RUnlock()

	mu = l.GetMutex(uid.MoviesMutex)
	mu.RLock()
	for _, m := range l.Movies {
		m.UIDs.MediaType = MovieType
		l.UIDs = append(l.UIDs, m.UIDs)

		if m.UIDs.Playcount > 0 {
			playcount.Watched[xxhash.Sum64String(fmt.Sprintf("%d_%d_%d", MovieType, TMDBScraper, m.UIDs.TMDB))] = true
			playcount.Watched[xxhash.Sum64String(fmt.Sprintf("%d_%d_%d", MovieType, TraktScraper, m.UIDs.Trakt))] = true
			playcount.Watched[xxhash.Sum64String(fmt.Sprintf("%d_%d_%s", MovieType, IMDBScraper, m.UIDs.IMDB))] = true
		}
	}
	mu.RUnlock()

	mu = l.GetMutex(uid.ShowsMutex)
	mu.RLock()
	for _, s := range l.Shows {
		s.UIDs.MediaType = ShowType
		l.UIDs = append(l.UIDs, s.UIDs)

		if s.UIDs.Playcount > 0 {
			playcount.Watched[xxhash.Sum64String(fmt.Sprintf("%d_%d_%d", ShowType, TMDBScraper, s.UIDs.TMDB))] = true
			playcount.Watched[xxhash.Sum64String(fmt.Sprintf("%d_%d_%d", ShowType, TraktScraper, s.UIDs.Trakt))] = true
			playcount.Watched[xxhash.Sum64String(fmt.Sprintf("%d_%d_%d", ShowType, TVDBScraper, s.UIDs.TVDB))] = true
		}

		for _, se := range s.Seasons {
			se.UIDs.MediaType = SeasonType
			l.UIDs = append(l.UIDs, se.UIDs)

			if se.UIDs.Playcount > 0 {
				playcount.Watched[xxhash.Sum64String(fmt.Sprintf("%d_%d_%d_%d", SeasonType, TMDBScraper, s.UIDs.TMDB, se.Season))] = true
				playcount.Watched[xxhash.Sum64String(fmt.Sprintf("%d_%d_%d_%d", SeasonType, TraktScraper, s.UIDs.Trakt, se.Season))] = true
				playcount.Watched[xxhash.Sum64String(fmt.Sprintf("%d_%d_%d_%d", SeasonType, TVDBScraper, s.UIDs.TVDB, se.Season))] = true
			}
		}

		for _, e := range s.Episodes {
			e.UIDs.MediaType = EpisodeType
			l.UIDs = append(l.UIDs, e.UIDs)

			if e.UIDs.Playcount > 0 {
				playcount.Watched[xxhash.Sum64String(fmt.Sprintf("%d_%d_%d_%d_%d", EpisodeType, TMDBScraper, s.UIDs.TMDB, e.Season, e.Episode))] = true
				playcount.Watched[xxhash.Sum64String(fmt.Sprintf("%d_%d_%d_%d_%d", EpisodeType, TraktScraper, s.UIDs.Trakt, e.Season, e.Episode))] = true
				playcount.Watched[xxhash.Sum64String(fmt.Sprintf("%d_%d_%d_%d_%d", EpisodeType, TVDBScraper, s.UIDs.TVDB, e.Season, e.Episode))] = true
			}
		}
	}
	mu.RUnlock()

	log.Debugf("UIDs refresh finished in %s", time.Since(now))
	return nil
}

func parseUniqueID(entityType uid.MediaItemType, i *uid.UniqueIDs, xbmcIDs *xbmc.UniqueIDs, fileName string, entityYear int) {
	i.MediaType = entityType

	convertKodiIDsToLibrary(i, xbmcIDs)

	if i.TMDB != 0 {
		return
	}

	// If this is a strm file then we try to get TMDB id from it
	if len(fileName) > 0 {
		id, err := findTMDBInFile(fileName, xbmcIDs.Unknown)
		if id != 0 {
			i.TMDB = id
			return
		} else if err != nil {
			log.Debugf("Error reading TMDB ID from the file %s: %#v", fileName, err)
		}
	}

	// We should not query for each episode, has no sense,
	// since we need only TVShow ID to be resolved
	if entityType == EpisodeType {
		return
	}

	// If we get here - we have no TMDB, so try to resolve it
	if len(i.IMDB) != 0 {
		i.TMDB = findTMDBIDs(entityType, "imdb_id", i.IMDB)
		if i.TMDB != 0 {
			return
		}
	}
	if i.TVDB != 0 {
		i.TMDB = findTMDBIDs(entityType, "tvdb_id", strconv.Itoa(i.TVDB))
		if i.TMDB != 0 {
			return
		}
	}

	// We don't have any Named IDs, only 'Unknown' so let's try to fetch it
	if xbmcIDs.Unknown != "" {
		localID, _ := strconv.Atoi(xbmcIDs.Unknown)
		if localID == 0 {
			return
		}

		// Try to treat as it is a TMDB id inside of Unknown field
		if entityType == MovieType {
			m := tmdb.GetMovie(localID, config.Get().Language)
			if m != nil {
				dt, err := time.Parse(time.DateOnly, m.FirstAirDate)
				if err != nil || dt.Year() == entityYear {
					i.TMDB = m.ID
					return
				}
			}
		} else if entityType == ShowType {
			s := tmdb.GetShow(localID, config.Get().Language)
			if s != nil {
				dt, err := time.Parse(time.DateOnly, s.FirstAirDate)
				if err != nil || dt.Year() == entityYear {
					i.TMDB = s.ID
					return
				}
			}

			// If not found, try to search as TVDB id
			id := findTMDBIDsWithYear(ShowType, "tvdb_id", xbmcIDs.Unknown, entityYear)
			if id != 0 {
				i.TMDB = id
				return
			}
		}
	}
}

func convertKodiIDsToLibrary(i *uid.UniqueIDs, xbmcIDs *xbmc.UniqueIDs) {
	if i == nil || xbmcIDs == nil {
		return
	}

	i.Kodi = xbmcIDs.Kodi
	i.IMDB = xbmcIDs.IMDB
	i.TMDB, _ = strconv.Atoi(xbmcIDs.TMDB)
	i.TVDB, _ = strconv.Atoi(xbmcIDs.TVDB)
	i.Trakt, _ = strconv.Atoi(xbmcIDs.Trakt)

	// Checking alternative fields
	// 		TheMovieDB
	if i.TMDB == 0 && len(xbmcIDs.TheMovieDB) > 0 {
		i.TMDB, _ = strconv.Atoi(xbmcIDs.TheMovieDB)
	}

	// 		IMDB
	if len(xbmcIDs.Unknown) > 0 && strings.HasPrefix(xbmcIDs.Unknown, "tt") {
		i.IMDB = xbmcIDs.Unknown
	}
}

func findTMDBInFile(fileName string, pattern string) (id int, err error) {
	if len(fileName) == 0 || !strings.HasSuffix(fileName, ".strm") {
		return
	}

	// Let's cache file search, it's bad to do that, anyway,
	// but we check only .strm files and do that once per 2 weeks
	cacheKey := fmt.Sprintf(cache.LibraryResolveFileKey, fileName)
	if err := cacheStore.Get(cacheKey, &id); err == nil {
		return id, nil
	}
	defer func() {
		if id == 0 {
			log.Debugf("Count not get ID from the file %s with pattern %s", fileName, pattern)
		}
		cacheStore.Set(cacheKey, id, cache.LibraryResolveFileExpire)
	}()

	if _, errStat := os.Stat(fileName); errStat != nil {
		return 0, errStat
	}

	fileContent, errRead := os.ReadFile(fileName)
	if errRead != nil {
		return 0, errRead
	}

	// Dummy check. If Unknown is found in the strm file - we treat it as tmdb id
	if len(pattern) > 1 && bytes.Contains(fileContent, []byte("/"+pattern)) {
		id, _ = strconv.Atoi(pattern)
		return
	}

	// Reading the strm file and passing to a regexp to get TMDB ID
	// This can't be done with episodes, since it has Show ID and not Episode ID
	if matches := resolveRegexp.FindSubmatch(fileContent); len(matches) > 1 {
		id, _ = strconv.Atoi(string(matches[1]))
		return
	}

	return
}

func findTMDBIDsWithYear(entityType int, source string, id string, year int) int {
	results := tmdb.Find(id, source)
	reserveID := 0

	if results != nil {
		if entityType == MovieType && len(results.MovieResults) > 0 {
			for _, e := range results.MovieResults {
				dt, err := time.Parse(time.DateOnly, e.FirstAirDate)
				if err != nil || year == 0 || dt.Year() == 0 {
					reserveID = e.ID
					continue
				}
				if dt.Year() == year {
					return e.ID
				}
			}
		} else if entityType == ShowType && len(results.TVResults) > 0 {
			for _, e := range results.TVResults {
				dt, err := time.Parse(time.DateOnly, e.FirstAirDate)
				if err != nil || year == 0 || dt.Year() == 0 {
					reserveID = e.ID
					continue
				}
				if dt.Year() == year {
					return e.ID
				}
			}
		} else if entityType == EpisodeType && len(results.TVEpisodeResults) > 0 {
			for _, e := range results.TVEpisodeResults {
				dt, err := time.Parse(time.DateOnly, e.FirstAirDate)
				if err != nil || year == 0 || dt.Year() == 0 {
					reserveID = e.ID
					continue
				}
				if dt.Year() == year {
					return e.ID
				}
			}
		}
	}

	return reserveID
}

func findTMDBIDs(entityType uid.MediaItemType, source string, id string) int {
	results := tmdb.Find(id, source)
	if results != nil {
		if entityType == MovieType && len(results.MovieResults) == 1 && results.MovieResults[0] != nil {
			return results.MovieResults[0].ID
		} else if entityType == ShowType && len(results.TVResults) == 1 && results.TVResults[0] != nil {
			return results.TVResults[0].ID
		} else if entityType == EpisodeType && len(results.TVEpisodeResults) == 1 && results.TVEpisodeResults[0] != nil {
			return results.TVEpisodeResults[0].ID
		}
	}

	return 0
}

func findTraktIDs(entityType int, source int, id string) (ids *trakt.IDs) {
	switch entityType {
	case MovieType:
		var r *trakt.Movie
		if source == TMDBScraper {
			r = trakt.GetMovieByTMDB(id)
		} else if source == TraktScraper {
			r = trakt.GetMovie(id)
		}
		if r != nil && r.IDs != nil {
			ids = r.IDs
		}
	case ShowType:
		var r *trakt.Show
		if source == TMDBScraper {
			r = trakt.GetShowByTMDB(id)
		} else if source == TraktScraper {
			r = trakt.GetShow(id)
		}
		if r != nil && r.IDs != nil {
			ids = r.IDs
		}
	case EpisodeType:
		var r *trakt.Episode
		if source == TMDBScraper {
			r = trakt.GetEpisodeByTMDB(id)
		} else if source == TraktScraper {
			r = trakt.GetEpisodeByID(id)
		}
		if r != nil && r.IDs != nil {
			ids = r.IDs
		}
	}

	return
}

// RefreshLocal checks media directory to save up-to-date strm library
func RefreshLocal() error {
	l := uid.Get()
	if l.Running.IsOverall {
		return nil
	}

	l.Pending.IsOverall = false
	l.Running.IsOverall = true
	defer func() {
		l.Running.IsOverall = false
		util.FreeMemoryGC()
	}()

	refreshLocalMovies()
	refreshLocalShows()

	return nil
}

func refreshLocalMovies() {
	if !config.Get().LibraryEnabled {
		return
	}

	moviesLibraryPath := MoviesLibraryPath()
	if _, err := os.Stat(moviesLibraryPath); err != nil {
		return
	}

	defer perf.ScopeTimer()()

	begin := time.Now()
	addon := []byte(config.Get().Info.ID)
	files := searchStrm(moviesLibraryPath)
	IDs := map[int]bool{}
	for _, f := range files {
		fileContent, err := os.ReadFile(f)
		if err != nil || len(fileContent) == 0 || !bytes.Contains(fileContent, addon) {
			continue
		}

		if matches := movieRegexp.FindSubmatch(fileContent); len(matches) > 1 {
			id, _ := strconv.Atoi(string(matches[1]))
			IDs[id] = true
		}
	}

	if len(IDs) == 0 {
		return
	}

	for id := range IDs {
		updateDBItem(id, StateActive, MovieType, 0)
	}
	log.Debugf("Finished updating %d local movies in %s", len(IDs), time.Since(begin))
}

func refreshLocalShows() {
	if !config.Get().LibraryEnabled {
		return
	}

	showsLibraryPath := ShowsLibraryPath()
	if _, err := os.Stat(showsLibraryPath); err != nil {
		return
	}

	defer perf.ScopeTimer()()

	begin := time.Now()
	addon := []byte(config.Get().Info.ID)
	files := searchStrm(showsLibraryPath)
	IDs := map[int]bool{}
	for _, f := range files {
		fileContent, err := os.ReadFile(f)
		if err != nil || len(fileContent) == 0 || !bytes.Contains(fileContent, addon) {
			continue
		}

		if matches := showRegexp.FindSubmatch(fileContent); len(matches) > 1 {
			showID, _ := strconv.Atoi(string(matches[1]))

			if !wasRemoved(showID, ShowType) {
				IDs[showID] = true
			}
		}
	}

	if len(IDs) == 0 {
		return
	}

	for id := range IDs {
		updateDBItem(id, StateActive, ShowType, id)
	}
	log.Debugf("Finished updating %d local shows in %s", len(IDs), time.Since(begin))
}

func searchStrm(dir string) []string {
	defer perf.ScopeTimer()()

	ret := []string{}
	foundDirs := map[string]struct{}{}

	godirwalk.Walk(dir, &godirwalk.Options{
		FollowSymbolicLinks: true,
		Callback: func(osPathname string, de *godirwalk.Dirent) error {
			if !strings.HasSuffix(osPathname, ".strm") {
				return nil
			}

			// Make sure we return only one file per directory, no need to get all of them
			dir := filepath.Dir(osPathname)
			if _, ok := foundDirs[dir]; ok {
				return nil
			}

			foundDirs[dir] = struct{}{}
			ret = append(ret, osPathname)
			return nil
		},
	})

	return ret
}

// MarkKodiRefresh ...
func MarkKodiRefresh() {
	uid.Get().Running.IsKodi = true
}

// MarkKodiUpdated ...
func MarkKodiUpdated() {
	isKodiUpdated = true
}

// MarkKodiAdded ...
func MarkKodiAdded() {
	isKodiAdded = true
}

// PlanOverallUpdate ...
func PlanOverallUpdate() {
	uid.Get().Pending.IsOverall = true
}

// PlanKodiUpdate ...
func PlanKodiUpdate() {
	uid.Get().Pending.IsKodi = true
}

// PlanTraktUpdate ...
func PlanTraktUpdate() {
	uid.Get().Pending.IsTrakt = true
}

// PlanMoviesUpdate ...
func PlanMoviesUpdate() {
	uid.Get().Pending.IsMovies = true
}

// PlanShowsUpdate ...
func PlanShowsUpdate() {
	uid.Get().Pending.IsShows = true
}

// PlanShowUpdate ...
func PlanShowUpdate(showID int) {
	lock.Lock()
	pendingShows[showID] = true
	lock.Unlock()
	uid.Get().Pending.IsEpisodes = true
}

// PlanKodiShowsUpdate ...
func PlanKodiShowsUpdate() {
	uid.Get().Pending.IsKodiShows = true
}

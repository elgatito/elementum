package library

import (
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/anacrolix/missinggo/perf"
	"github.com/cespare/xxhash"

	"github.com/elgatito/elementum/cache"
	"github.com/elgatito/elementum/config"
	"github.com/elgatito/elementum/library/uid"
	"github.com/elgatito/elementum/tmdb"
	"github.com/elgatito/elementum/trakt"
	"github.com/elgatito/elementum/xbmc"
)

var (
	// IsTraktInitialized used to mark if we need only incremental updates from Trakt
	IsTraktInitialized bool
	isKodiAdded        bool
	isKodiUpdated      bool
)

// RefreshTrakt gets user activities from Trakt
// to see if we need to add movies/set watched status and so on
func RefreshTrakt() error {
	xbmcHost, err := xbmc.GetLocalXBMCHost()
	if xbmcHost == nil || err != nil {
		log.Debugf("Stopping Trakt refresh due to missing XBMC host")
		return err
	}

	l := uid.Get()
	if config.Get().TraktToken == "" || !config.Get().TraktSyncEnabled || (!config.Get().TraktSyncPlaybackEnabled && xbmcHost.PlayerIsPlaying()) {
		// Even if sync is disabled, check if current Trakt auth is fine to use.
		if config.Get().TraktToken != "" && !config.Get().TraktAuthorized {
			trakt.GetLastActivities()
		}

		l.Pending.IsTrakt = false
		return nil
	} else if l.Running.IsTrakt {
		log.Debugf("TraktSync: already in scanning")
		return nil
	} else if l.Running.IsOverall {
		return nil
	}

	l.Pending.IsTrakt = false
	l.Running.IsTrakt = true
	defer func() {
		l.Running.IsTrakt = false
	}()

	log.Infof("Running Trakt sync")
	started := time.Now()
	defer func() {
		log.Infof("Trakt sync finished in %s", time.Since(started))
	}()

	activities, err := trakt.GetActivities("")
	if err != nil {
		log.Warningf("Cannot get activities: %s", err)
		if err == trakt.ErrLocked {
			go trakt.NotifyLocked()
		}

		return err
	}

	// If nothing changed from last check - skip everything
	isFirstRun := !IsTraktInitialized || isKodiUpdated
	if !activities.All() && !isFirstRun {
		log.Debugf("Skipping Trakt sync due to stale activities")
		return nil
	}

	isErrored := false
	defer func() {
		if !isErrored {
			activities.SaveCurrent()
		}
	}()

	if isFirstRun {
		IsTraktInitialized = true
		isKodiUpdated = false
		isKodiAdded = false
	}

	// Movies
	if isFirstRun || isKodiAdded || activities.MoviesWatched() {
		if err := RefreshTraktWatched(xbmcHost, MovieType, activities.MoviesWatched()); err != nil {
			isErrored = true
		}
	}
	if isFirstRun || activities.MoviesCollected() {
		if err := RefreshTraktCollected(xbmcHost, MovieType, activities.MoviesCollected()); err != nil {
			isErrored = true
		}
	}
	if isFirstRun || activities.MoviesWatchlisted() {
		if err := RefreshTraktWatchlisted(xbmcHost, MovieType, activities.MoviesWatchlisted()); err != nil {
			isErrored = true
		}
	}
	if isFirstRun || isKodiAdded || activities.MoviesPaused() {
		if err := RefreshTraktPaused(xbmcHost, MovieType, activities.MoviesPaused()); err != nil {
			isErrored = true
		}
	}
	if isFirstRun || activities.MoviesHidden() {
		if err := RefreshTraktHidden(xbmcHost, MovieType, activities.MoviesHidden()); err != nil {
			isErrored = true
		}
	}

	// Episodes
	if isFirstRun || isKodiAdded || activities.EpisodesWatched() {
		if err := RefreshTraktWatched(xbmcHost, EpisodeType, activities.EpisodesWatched()); err != nil {
			isErrored = true
		}
	}
	if isFirstRun || activities.EpisodesCollected() {
		if err := RefreshTraktCollected(xbmcHost, EpisodeType, activities.EpisodesCollected()); err != nil {
			isErrored = true
		}
	}
	if isFirstRun || activities.EpisodesWatchlisted() {
		if err := RefreshTraktWatchlisted(xbmcHost, EpisodeType, activities.EpisodesWatchlisted()); err != nil {
			isErrored = true
		}
	}
	if isFirstRun || isKodiAdded || activities.EpisodesPaused() {
		if err := RefreshTraktPaused(xbmcHost, EpisodeType, activities.EpisodesPaused()); err != nil {
			isErrored = true
		}
	}

	// Shows
	if isFirstRun || activities.ShowsWatchlisted() {
		if err := RefreshTraktWatchlisted(xbmcHost, ShowType, activities.ShowsWatchlisted()); err != nil {
			isErrored = true
		}
	}
	if isFirstRun || activities.ShowsHidden() {
		if err := RefreshTraktHidden(xbmcHost, ShowType, activities.ShowsHidden()); err != nil {
			isErrored = true
		}
	}

	// Seasons
	if isFirstRun || activities.SeasonsWatchlisted() {
		if err := RefreshTraktWatchlisted(xbmcHost, SeasonType, activities.SeasonsWatchlisted()); err != nil {
			isErrored = true
		}
	}
	if isFirstRun || activities.SeasonsHidden() {
		if err := RefreshTraktHidden(xbmcHost, SeasonType, activities.SeasonsHidden()); err != nil {
			isErrored = true
		}
	}

	// Lists
	if isFirstRun || activities.ListsUpdated() {
		if err := RefreshTraktLists(xbmcHost, activities.ListsUpdated()); err != nil {
			isErrored = true
		}
	}

	return nil
}

// RefreshTraktWatched ...
func RefreshTraktWatched(xbmcHost *xbmc.XBMCHost, itemType int, isRefreshNeeded bool) error {
	if config.Get().TraktToken == "" || !config.Get().TraktSyncWatched {
		return nil
	}

	l := uid.Get()
	mu := l.GetMutex(uid.TraktMutex)
	mu.Lock()
	defer mu.Unlock()

	started := time.Now()
	defer func() {
		log.Debugf("Trakt sync watched for '%s' finished in %s", ItemTypes[itemType], time.Since(started))
		RefreshUIDsRunner(true)
	}()

	if itemType == MovieType {
		return refreshTraktMoviesWatched(xbmcHost, isRefreshNeeded)
	} else if itemType == EpisodeType || itemType == SeasonType || itemType == ShowType {
		return refreshTraktShowsWatched(xbmcHost, isRefreshNeeded)
	}

	return nil
}

func refreshTraktMoviesWatched(xbmcHost *xbmc.XBMCHost, isRefreshNeeded bool) error {
	l := uid.Get()
	l.Running.IsMovies = true

	container := l.GetContainer(uid.WatchedMoviesContainer)
	if container == nil {
		l.Running.IsMovies = false
		return ErrNoContainer
	}

	container.Mu.Lock()

	defer func() {
		container.Mu.Unlock()
		l.Store()
		l.Running.IsMovies = false
	}()

	previous, _ := trakt.PreviousWatchedMovies()
	current, err := trakt.WatchedMovies(isRefreshNeeded)
	if err != nil {
		log.Warningf("Got error from getting watched movies: %s", err)
		return err
	} else if len(current) == 0 || config.Get().LibraryReadOnly {
		return nil
	}

	MoviesToUID(uid.WatchedMoviesContainer, current.ToMovies())

	cacheStore := cache.NewDBStore()

	lastPlaycount := map[uint64]bool{}
	syncPlaycount := map[uint64]bool{}

	lastCacheKey := fmt.Sprintf(cache.LibraryWatchedPlaycountKey, "movies")
	syncCacheKey := fmt.Sprintf(cache.LibrarySyncPlaycountKey, "movies")

	// Should parse all movies for Watched marks, but process only difference,
	// to avoid overwriting Kodi unwatched items
	watchedMovies := trakt.DiffWatchedMovies(previous, current, true)
	unwatchedMovies := trakt.DiffWatchedMovies(current, previous, false)

	fileKey := uint64(0)
	missedItems := uid.NewLibraryContainer()

	// Sync local items with exact list
	for _, m := range watchedMovies {
		updateMovieWatched(xbmcHost, m, true)
	}
	for _, m := range unwatchedMovies {
		updateMovieWatched(xbmcHost, m, false)
	}

	cacheStore.Get(lastCacheKey, &lastPlaycount)
	cacheStore.Get(syncCacheKey, &syncPlaycount)
	defer cacheStore.Set(lastCacheKey, &lastPlaycount, cache.LibraryWatchedPlaycountExpire)
	defer cacheStore.Set(syncCacheKey, &syncPlaycount, cache.LibrarySyncPlaycountExpire)

	for _, m := range current {
		if m == nil || m.Movie == nil || m.Movie.IDs == nil {
			continue
		}

		if r := getKodiMovieByTraktIDs(m.Movie.IDs); r != nil {
			// Check if we previously set this item as watched, to avoid re-setting local items again and again.
			fileKey = xxhash.Sum64String(r.File)
			if _, ok := lastPlaycount[fileKey]; ok && !r.IsWatched() {
				continue
			}

			// Update local item Watched status if it is unwatched or was added after it is was watched
			if !r.IsWatched() {
				lastPlaycount[fileKey] = true
				updateMovieWatched(xbmcHost, m, true)
			}
		} else {
			missedItems.Add(MovieType, m.Movie.IDs.ToUniqueIDs())
		}
	}

	if !config.Get().TraktSyncWatchedBack || len(l.Movies) == 0 {
		return nil
	}

	syncWatchMovies := []*trakt.WatchedItem{}
	syncUnwatchMovies := []*trakt.WatchedItem{}

	mu := l.GetMutex(uid.MoviesMutex)
	mu.Lock()
	for _, m := range l.Movies {
		if m.UIDs.TMDB == 0 {
			continue
		}

		fileKey = xxhash.Sum64String(m.File)
		previousRun, isDone := syncPlaycount[fileKey]

		has := container.Has(MovieType, m.UIDs)
		if (has && m.IsWatched()) || (!has && !m.IsWatched() || (isDone && previousRun == m.IsWatched())) {
			continue
		}

		item := &trakt.WatchedItem{
			KodiKey:   fileKey,
			MediaType: "movie",
			Movie:     m.UIDs.TMDB,
			Watched:   !has && m.IsWatched(),
		}
		if item.Watched {
			syncWatchMovies = append(syncWatchMovies, item)
		} else {
			syncUnwatchMovies = append(syncUnwatchMovies, item)
		}
	}
	mu.Unlock()

	if len(syncUnwatchMovies) > 0 {
		if _, err := trakt.SetMultipleWatched(syncUnwatchMovies); err == nil {
			// Set cached entry to avoid running same item again
			for _, i := range syncUnwatchMovies {
				delete(lastPlaycount, i.KodiKey)
				syncPlaycount[i.KodiKey] = i.Watched
			}
		}
	}
	if len(syncWatchMovies) > 0 {
		if _, err := trakt.SetMultipleWatched(syncWatchMovies); err == nil {
			// Set cached entry to avoid running same item again
			for _, i := range syncWatchMovies {
				syncPlaycount[i.KodiKey] = i.Watched
			}
		}
	}

	return nil
}

func refreshTraktShowsWatched(xbmcHost *xbmc.XBMCHost, isRefreshNeeded bool) error {
	l := uid.Get()
	l.Running.IsShows = true

	container := l.GetContainer(uid.WatchedShowsContainer)
	if container == nil {
		l.Running.IsShows = false
		return ErrNoContainer
	}

	container.Mu.Lock()

	defer func() {
		container.Mu.Unlock()
		l.Store()
		l.Running.IsShows = false
	}()

	previous, _ := trakt.PreviousWatchedShows()
	current, err := trakt.WatchedShows(isRefreshNeeded)
	if err != nil {
		log.Warningf("Got error from getting watched shows: %s", err)
		return err
	} else if len(current) == 0 || config.Get().LibraryReadOnly {
		// Kind of strange check to make sure Trakt watched items are not empty
		return nil
	}

	cacheStore := cache.NewDBStore()

	lastPlaycount := map[uint64]bool{}
	syncPlaycount := map[uint64]bool{}

	lastCacheKey := fmt.Sprintf(cache.LibraryWatchedPlaycountKey, "shows")
	syncCacheKey := fmt.Sprintf(cache.LibrarySyncPlaycountKey, "shows")

	// Should parse all shows for Watched marks, but process only difference,
	// to avoid overwriting Kodi unwatched items
	watchedShows := trakt.DiffWatchedShows(previous, current)
	unwatchedShows := trakt.DiffWatchedShows(current, previous)

	fileKey := uint64(0)
	missedItems := uid.NewLibraryContainer()
	container.Clear()

	cacheStore.Get(lastCacheKey, &lastPlaycount)
	cacheStore.Get(syncCacheKey, &syncPlaycount)
	defer cacheStore.Set(lastCacheKey, &lastPlaycount, cache.LibraryWatchedPlaycountExpire)
	defer cacheStore.Set(syncCacheKey, &syncPlaycount, cache.LibrarySyncPlaycountExpire)

	// Sync local items with exact list
	for _, s := range watchedShows {
		updateShowWatched(xbmcHost, s, true)
	}
	for _, s := range unwatchedShows {
		updateShowWatched(xbmcHost, s, false)
	}

	for _, s := range current {
		if s == nil || s.Show == nil || s.Show.IDs == nil {
			continue
		}

		tmdbShow := tmdb.GetShowByID(strconv.Itoa(s.Show.IDs.TMDB), config.Get().Language)
		completedSeasons := 0
		for _, season := range s.Seasons {
			if season == nil || season.Episodes == nil {
				continue
			}

			if tmdbShow != nil {
				if sc := tmdbShow.GetSeasonEpisodes(season.Number); sc != 0 && sc == len(season.Episodes) && season.Number > 0 {
					completedSeasons++

					container.Add(SeasonType, s.Show.IDs.ToUniqueIDs(), season.Number)
				}
			}

			for _, episode := range season.Episodes {
				container.Add(EpisodeType, s.Show.IDs.ToUniqueIDs(), season.Number, episode.Number)
			}
		}

		if tmdbShow != nil && ((completedSeasons == tmdbShow.CountRealSeasons() && tmdbShow.CountRealSeasons() != 0) || s.Watched) {
			s.Watched = true

			container.Add(ShowType, s.Show.IDs.ToUniqueIDs())
		}

		if r := getKodiShowByTraktIDs(s.Show.IDs); r != nil {
			toRun := false
			for _, season := range s.Seasons {
				for _, episode := range season.Episodes {
					if e := r.GetEpisode(season.Number, episode.Number); e != nil {
						fileKey = xxhash.Sum64String(e.File)
						if _, ok := lastPlaycount[fileKey]; ok && !e.IsWatched() {
							// Reset Plays to mark this episode as non-watched
							episode.Plays = 0
							continue
						}

						if !e.IsWatched() {
							lastPlaycount[fileKey] = true
							toRun = true
						}
					} else {
						missedItems.Add(EpisodeType, s.Show.IDs.ToUniqueIDs(), season.Number, episode.Number)
					}
				}
			}

			if toRun || r.DateAdded.After(s.LastWatchedAt) {
				updateShowWatched(xbmcHost, s, true)
			}
		} else {
			missedItems.Add(ShowType, s.Show.IDs.ToUniqueIDs())
		}
	}

	if !config.Get().TraktSyncWatchedBack || len(l.Shows) == 0 {
		return nil
	}

	syncWatchShows := []*trakt.WatchedItem{}
	syncUnwatchShows := []*trakt.WatchedItem{}

	mu := l.GetMutex(uid.ShowsMutex)
	mu.Lock()
	for _, s := range l.Shows {
		if s.UIDs.TMDB == 0 || container.Has(ShowType, s.UIDs) {
			continue
		} else if missedItems.Has(ShowType, s.UIDs) {
			continue
		}

		for _, e := range s.Episodes {
			fileKey = xxhash.Sum64String(e.File)
			previousRun, isDone := syncPlaycount[fileKey]

			has := container.Has(EpisodeType, s.UIDs, e.Season, e.Episode) ||
				container.Has(SeasonType, s.UIDs, e.Season)
			if (has && e.IsWatched()) || (!has && !e.IsWatched()) || (isDone && previousRun == e.IsWatched()) {
				continue
			} else if missedItems.Has(EpisodeType, s.UIDs, e.Season, e.Episode) {
				// Item not in Kodi library
				continue
			}

			item := &trakt.WatchedItem{
				KodiKey:   fileKey,
				MediaType: "episode",
				Show:      s.UIDs.TMDB,
				Season:    e.Season,
				Episode:   e.Episode,
				Watched:   !has && e.IsWatched(),
			}
			if item.Watched {
				syncWatchShows = append(syncWatchShows, item)
			} else {
				syncUnwatchShows = append(syncUnwatchShows, item)
			}
		}
	}
	mu.Unlock()

	if len(syncUnwatchShows) > 0 {
		if _, err := trakt.SetMultipleWatched(syncUnwatchShows); err == nil {
			// Set cached entry to avoid running same item again
			for _, i := range syncUnwatchShows {
				delete(lastPlaycount, i.KodiKey)
				syncPlaycount[i.KodiKey] = i.Watched
			}
		}
	}
	if len(syncWatchShows) > 0 {
		if _, err := trakt.SetMultipleWatched(syncWatchShows); err == nil {
			// Set cached entry to avoid running same item again
			for _, i := range syncWatchShows {
				syncPlaycount[i.KodiKey] = i.Watched
			}
		}
	}

	return nil
}

func getKodiMovieByTraktIDs(ids *trakt.IDs) (r *uid.Movie) {
	if r == nil && ids.TMDB != 0 {
		r, _ = uid.GetMovieByTMDB(ids.TMDB)
	}
	if r == nil && ids.IMDB != "" {
		r, _ = uid.GetMovieByIMDB(ids.IMDB)
	}
	return
}

func getKodiShowByTraktIDs(ids *trakt.IDs) (r *uid.Show) {
	if r == nil && ids.TMDB != 0 {
		r, _ = uid.FindShowByTMDB(ids.TMDB)
	}
	if r == nil && ids.IMDB != "" {
		r, _ = uid.FindShowByIMDB(ids.IMDB)
	}
	return
}

func updateMovieWatched(xbmcHost *xbmc.XBMCHost, m *trakt.WatchedMovie, watched bool) {
	if m == nil || m.Movie == nil || m.Movie.IDs == nil {
		return
	}

	var r = getKodiMovieByTraktIDs(m.Movie.IDs)
	if r == nil {
		return
	}

	// Resetting Resume state to avoid having old resume states,
	// when item is watched on another device
	if watched {
		if m.Plays <= 0 {
			return
		}

		// Skip setting item as watched if it is the same LastPlayed (or newer) as in Trakt
		if r.IsWatched() && !r.LastPlayed.IsZero() && !m.LastWatchedAt.After(r.LastPlayed) {
			return
		}
		r.UIDs.Playcount++
		xbmcHost.SetMovieWatchedWithDate(r.UIDs.Kodi, r.UIDs.Playcount, 0, 0, m.LastWatchedAt)
		// TODO: There should be a check for allowing resume state, otherwise we always reset it for already searched items
		// } else if watched && r.IsWatched() && r.Resume != nil && r.Resume.Position > 0 {
		// 	xbmc.SetMovieWatchedWithDate(r.UIDs.Kodi, 1, 0, 0, m.LastWatchedAt)
	} else if !watched && r.IsWatched() {
		r.UIDs.Playcount = 0
		xbmcHost.SetMoviePlaycount(r.UIDs.Kodi, 0)
	}
}

func updateShowWatched(xbmcHost *xbmc.XBMCHost, s *trakt.WatchedShow, watched bool) {
	if s == nil || s.Show == nil || s.Show.IDs == nil {
		return
	}

	var r = getKodiShowByTraktIDs(s.Show.IDs)
	if r == nil {
		return
	}

	if watched && s.Watched && !r.IsWatched() {
		r.UIDs.Playcount = 1
		xbmcHost.SetShowWatchedWithDate(r.UIDs.Kodi, 1, s.LastWatchedAt)
	}

	for _, season := range s.Seasons {
		for _, episode := range season.Episodes {
			if episode.Plays <= 0 {
				continue
			}

			e := r.GetEpisode(season.Number, episode.Number)
			if e != nil {
				// Resetting Resume state to avoid having old resume states,
				// when item is watched on another device
				if watched && !e.IsWatched() {
					// Skip setting item as watched if it is the same LastPlayed (or newer) as in Trakt
					if !e.LastPlayed.IsZero() && !episode.LastWatchedAt.After(e.LastPlayed) {
						continue
					}

					e.UIDs.Playcount = 1
					xbmcHost.SetEpisodeWatchedWithDate(e.UIDs.Kodi, 1, 0, 0, episode.LastWatchedAt)
					// TODO: There should be a check for allowing resume state, otherwise we always reset it for already searched items
					// } else if watched && e.IsWatched() && e.Resume != nil && e.Resume.Position > 0 {
					//   xbmc.SetEpisodeWatchedWithDate(e.UIDs.Kodi, 1, 0, 0, episode.LastWatchedAt)
				} else if !watched && e.IsWatched() {
					e.UIDs.Playcount = 0
					xbmcHost.SetEpisodePlaycount(e.UIDs.Kodi, 0)
				}
			}
		}
	}
}

// RefreshTraktCollected ...
func RefreshTraktCollected(xbmcHost *xbmc.XBMCHost, itemType int, isRefreshNeeded bool) error {
	if config.Get().TraktToken == "" {
		return nil
	}

	if itemType == MovieType {
		if err := SyncMoviesList("collection", false, isRefreshNeeded); err != nil {
			log.Warningf("TraktSync: Got error from SyncMoviesList for Collection: %s", err)
			return err
		}
	} else if itemType == EpisodeType || itemType == SeasonType || itemType == ShowType {
		if err := SyncShowsList("collection", false, isRefreshNeeded); err != nil {
			log.Warningf("TraktSync: Got error from SyncShowsList for Collection: %s", err)
			return err
		}
	}

	return nil
}

// RefreshTraktWatchlisted ...
func RefreshTraktWatchlisted(xbmcHost *xbmc.XBMCHost, itemType int, isRefreshNeeded bool) error {
	if config.Get().TraktToken == "" {
		return nil
	}

	if itemType == MovieType {
		if err := SyncMoviesList("watchlist", false, isRefreshNeeded); err != nil {
			log.Warningf("TraktSync: Got error from SyncMoviesList for Watchlist: %s", err)
			return err
		}
	} else if itemType == EpisodeType || itemType == SeasonType || itemType == ShowType {
		if err := SyncShowsList("watchlist", false, isRefreshNeeded); err != nil {
			log.Warningf("TraktSync: Got error from SyncShowsList for Watchlist: %s", err)
			return err
		}
	}

	return nil
}

// RefreshTraktPaused ...
func RefreshTraktPaused(xbmcHost *xbmc.XBMCHost, itemType int, isRefreshNeeded bool) error {
	if config.Get().TraktToken == "" || !config.Get().TraktSyncPlaybackProgress {
		return nil
	}

	cacheStore := cache.NewDBStore()
	lastUpdates := map[int]time.Time{}

	cacheKey := fmt.Sprintf(cache.TraktPausedLastUpdatesKey, itemType)
	cacheStore.Get(cacheKey, &lastUpdates)
	defer func() {
		cacheStore.Set(cacheKey, &lastUpdates, cache.TraktPausedLastUpdatesExpire)
	}()

	started := time.Now()
	defer func() {
		log.Debugf("Trakt sync paused for '%s' finished in %s", ItemTypes[itemType], time.Since(started))
	}()

	l := uid.Get()

	if itemType == MovieType {
		l.Running.IsMovies = true
		defer func() {
			l.Running.IsMovies = false
		}()

		movies, err := trakt.PausedMovies(isRefreshNeeded)
		if err != nil {
			log.Warningf("TraktSync: Got error from PausedMovies: %s", err)
			return err
		} else if config.Get().LibraryReadOnly {
			return nil
		}

		for _, m := range movies {
			if m == nil || m.Movie == nil || m.Movie.IDs == nil || m.Movie.IDs.TMDB == 0 || int(m.Progress) <= 0 || m.Movie.Runtime <= 0 {
				continue
			}

			if lm, err := uid.GetMovieByTMDB(m.Movie.IDs.TMDB); err == nil {
				if t, ok := lastUpdates[m.Movie.IDs.Trakt]; ok && !t.Before(m.PausedAt) {
					continue
				}

				lastUpdates[m.Movie.IDs.Trakt] = m.PausedAt
				runtime := m.Movie.Runtime * 60

				xbmcHost.SetMovieProgressWithDate(lm.UIDs.Kodi, runtime/100*int(m.Progress), runtime, m.PausedAt)
			}
		}
	} else if itemType == EpisodeType || itemType == SeasonType || itemType == ShowType {
		l.Running.IsShows = true
		defer func() {
			l.Running.IsShows = false
		}()

		shows, err := trakt.PausedShows(isRefreshNeeded)
		if err != nil {
			log.Warningf("TraktSync: Got error from PausedShows: %s", err)
			return err
		} else if config.Get().LibraryReadOnly {
			return nil
		}

		for _, s := range shows {
			if s == nil || s.Show == nil || s.Show.IDs == nil || s.Show.IDs.TMDB == 0 || int(s.Progress) <= 0 ||
				s.Episode == nil || s.Episode.IDs == nil || s.Episode.Runtime <= 0 {
				continue
			}

			if ls, err := uid.GetShowByTMDB(s.Show.IDs.TMDB); err == nil {
				e := ls.GetEpisode(s.Episode.Season, s.Episode.Number)
				if e == nil {
					continue
				} else if t, ok := lastUpdates[s.Episode.IDs.Trakt]; ok && !t.Before(s.PausedAt) {
					continue
				}

				lastUpdates[s.Episode.IDs.Trakt] = s.PausedAt
				runtime := s.Episode.Runtime * 60

				xbmcHost.SetEpisodeProgressWithDate(e.UIDs.Kodi, runtime/100*int(s.Progress), runtime, s.PausedAt)
			}
		}
	}

	return nil
}

// RefreshTraktHidden ...
func RefreshTraktHidden(xbmcHost *xbmc.XBMCHost, itemType int, isRefreshNeeded bool) error {
	if config.Get().TraktToken == "" || !config.Get().TraktSyncHidden {
		return nil
	}

	// https://trakt.docs.apiary.io/#reference/users/hidden-items/get-hidden-items
	if itemType == ShowType {
		// calendar and recommendations for shows are handled on trakt side, progress_watched should be handled manually
		if _, err := trakt.ListHiddenShows("progress_watched", isRefreshNeeded); err != nil {
			log.Warningf("TraktSync: Got error from SyncShowsList for Watched Progress: %s", err)
			return err
		}
	} else if itemType == MovieType {
		// calendar and recommendations for movies are handled on trakt side, and they are the only options
	} else if itemType == SeasonType {
		// looks like website does not allow to hide seasons, so we also can ignore them
	}

	return nil
}

// RefreshTraktLists ...
func RefreshTraktLists(xbmcHost *xbmc.XBMCHost, isRefreshNeeded bool) error {
	if config.Get().TraktToken == "" {
		return nil
	}

	lists := trakt.Userlists()
	for _, list := range lists {
		if list == nil || list.IDs == nil {
			continue
		}

		if err := SyncMoviesList(strconv.Itoa(list.IDs.Trakt), false, isRefreshNeeded); err != nil {
			continue
		}
		if err := SyncShowsList(strconv.Itoa(list.IDs.Trakt), false, isRefreshNeeded); err != nil {
			continue
		}
	}

	return nil
}

func syncMoviesRemovedBack(movies []*trakt.Movies) error {
	xbmcHost, err := xbmc.GetLocalXBMCHost()
	if xbmcHost == nil || err != nil {
		return errors.New("No Kodi instance found")
	}

	for _, m := range movies {
		if m == nil || m.Movie == nil || m.Movie.IDs == nil {
			continue
		}

		if kodiMovie, err := uid.GetMovieByTMDB(m.Movie.IDs.TMDB); err == nil && kodiMovie != nil {
			movie, paths, err := RemoveMovie(m.Movie.IDs.TMDB, true)
			if err != nil {
				log.Warningf("Could not remove movie from Kodi library: %s", err)
			} else if movie != nil && paths != nil {
				for _, path := range paths {
					xbmcHost.VideoLibraryCleanDirectory(path, "movies", false)
				}
				xbmcHost.VideoLibraryRemoveMovie(kodiMovie.XbmcUIDs.Kodi)
			}
		}
	}

	return nil
}

func syncShowsRemovedBack(shows []*trakt.Shows) error {
	xbmcHost, err := xbmc.GetLocalXBMCHost()
	if xbmcHost == nil || err != nil {
		return errors.New("No Kodi instance found")
	}

	for _, s := range shows {
		if s == nil || s.Show == nil || s.Show.IDs == nil {
			continue
		}

		if kodiShow, err := uid.FindShowByTMDB(s.Show.IDs.TMDB); err == nil && kodiShow != nil {
			show, paths, err := RemoveShow(strconv.Itoa(s.Show.IDs.TMDB), true)
			if err != nil {
				log.Warningf("Could not remove show from Kodi library: %s", err)
			} else if show != nil && paths != nil {
				for _, path := range paths {
					xbmcHost.VideoLibraryCleanDirectory(path, "tvshows", false)
				}
				xbmcHost.VideoLibraryRemoveTVShow(kodiShow.XbmcUIDs.Kodi)
			}
		}
	}

	return nil
}

func MoviesToUIDLocked(containerType uid.ContainerType, movies []*trakt.Movies) {
	container := uid.Get().GetContainer(containerType)
	if container == nil {
		return
	}

	container.Mu.Lock()
	defer container.Mu.Unlock()

	MoviesToUID(containerType, movies)
}

func MoviesToUID(containerType uid.ContainerType, movies []*trakt.Movies) {
	defer perf.ScopeTimer()()

	l := uid.Get()
	container := l.GetContainer(containerType)
	if container == nil {
		return
	}

	container.Clear()
	for _, m := range movies {
		if m == nil || m.Movie == nil || m.Movie.IDs == nil {
			continue
		}

		container.Add(MovieType, m.Movie.IDs.ToUniqueIDs())
	}

	l.Store()
	log.Debugf("Synced %d movies items into container type %d", len(movies), containerType)
}

func ShowsToUIDLocked(containerType uid.ContainerType, shows []*trakt.Shows) {
	container := uid.Get().GetContainer(containerType)
	if container == nil {
		return
	}

	container.Mu.Lock()
	defer container.Mu.Unlock()

	ShowsToUID(containerType, shows)
}

func ShowsToUID(containerType uid.ContainerType, shows []*trakt.Shows) {
	defer perf.ScopeTimer()()

	l := uid.Get()
	container := l.GetContainer(containerType)
	if container == nil {
		return
	}

	container.Clear()
	for _, s := range shows {
		if s == nil || s.Show == nil || s.Show.IDs == nil {
			continue
		}

		container.Add(ShowType, s.Show.IDs.ToUniqueIDs())
	}

	l.Store()
	log.Debugf("Synced %d shows items into container type %d", len(shows), containerType)
}

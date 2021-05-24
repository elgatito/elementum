package library

import (
	"fmt"
	"strconv"
	"time"

	"github.com/cespare/xxhash"
	"github.com/elgatito/elementum/cache"
	"github.com/elgatito/elementum/config"
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
	if config.Get().TraktToken == "" || !config.Get().TraktSyncEnabled || (!config.Get().TraktSyncPlaybackEnabled && xbmc.PlayerIsPlaying()) {
		// Even if sync is disabled, check if current Trakt auth is fine to use.
		if config.Get().TraktToken != "" && !config.Get().TraktAuthorized {
			trakt.GetLastActivities()
		}

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

	cacheStore := cache.NewDBStore()
	lastActivities, err := trakt.GetLastActivities()
	if err != nil {
		log.Warningf("Cannot get activities: %s", err)
		if err == trakt.ErrLocked {
			go trakt.NotifyLocked()
		}

		return err
	}
	var previousActivities trakt.UserActivities
	_ = cacheStore.Get(cache.TraktActivitiesKey, &previousActivities)

	// If nothing changed from last check - skip everything
	isFirstRun := !IsTraktInitialized || isKodiUpdated
	if !lastActivities.All.After(previousActivities.All) && !isFirstRun {
		log.Debugf("Skipping Trakt sync due to stale activities")
		return nil
	}

	isErrored := false
	defer func() {
		if !isErrored {
			_ = cacheStore.Set(cache.TraktActivitiesKey, lastActivities, cache.TraktActivitiesExpire)
		}
	}()

	if isFirstRun {
		l.mu.Trakt.Lock()
		l.WatchedTraktMovies = []uint64{}
		l.WatchedTraktShows = []uint64{}
		l.mu.Trakt.Unlock()

		IsTraktInitialized = true
		isKodiUpdated = false
		isKodiAdded = false
	}

	// Movies
	if isFirstRun || isKodiAdded || lastActivities.Movies.WatchedAt.After(previousActivities.Movies.WatchedAt) {
		if err := RefreshTraktWatched(MovieType, lastActivities.Movies.WatchedAt.After(previousActivities.Movies.WatchedAt)); err != nil {
			isErrored = true
		}
	}
	if isFirstRun || lastActivities.Movies.CollectedAt.After(previousActivities.Movies.CollectedAt) {
		if err := RefreshTraktCollected(MovieType, lastActivities.Movies.CollectedAt.After(previousActivities.Movies.CollectedAt)); err != nil {
			isErrored = true
		}
	}
	if isFirstRun || lastActivities.Movies.WatchlistedAt.After(previousActivities.Movies.WatchlistedAt) {
		if err := RefreshTraktWatchlisted(MovieType, lastActivities.Movies.WatchlistedAt.After(previousActivities.Movies.WatchlistedAt)); err != nil {
			isErrored = true
		}
	}
	if isFirstRun || isKodiAdded || lastActivities.Movies.PausedAt.After(previousActivities.Movies.PausedAt) {
		if err := RefreshTraktPaused(MovieType, lastActivities.Movies.PausedAt.After(previousActivities.Movies.PausedAt)); err != nil {
			isErrored = true
		}
	}
	if isFirstRun || lastActivities.Movies.HiddenAt.After(previousActivities.Movies.HiddenAt) {
		if err := RefreshTraktHidden(MovieType, lastActivities.Movies.HiddenAt.After(previousActivities.Movies.HiddenAt)); err != nil {
			isErrored = true
		}
	}

	// Episodes
	if isFirstRun || isKodiAdded || lastActivities.Episodes.WatchedAt.After(previousActivities.Episodes.WatchedAt) {
		if err := RefreshTraktWatched(EpisodeType, lastActivities.Episodes.WatchedAt.After(previousActivities.Episodes.WatchedAt)); err != nil {
			isErrored = true
		}
	}
	if isFirstRun || lastActivities.Episodes.CollectedAt.After(previousActivities.Episodes.CollectedAt) {
		if err := RefreshTraktCollected(EpisodeType, lastActivities.Episodes.CollectedAt.After(previousActivities.Episodes.CollectedAt)); err != nil {
			isErrored = true
		}
	}
	if isFirstRun || lastActivities.Episodes.WatchlistedAt.After(previousActivities.Episodes.WatchlistedAt) {
		if err := RefreshTraktWatchlisted(EpisodeType, lastActivities.Episodes.WatchlistedAt.After(previousActivities.Episodes.WatchlistedAt)); err != nil {
			isErrored = true
		}
	}
	if isFirstRun || isKodiAdded || lastActivities.Episodes.PausedAt.After(previousActivities.Episodes.PausedAt) {
		if err := RefreshTraktPaused(EpisodeType, lastActivities.Episodes.PausedAt.After(previousActivities.Episodes.PausedAt)); err != nil {
			isErrored = true
		}
	}

	// Shows
	if isFirstRun || lastActivities.Shows.WatchlistedAt.After(previousActivities.Shows.WatchlistedAt) {
		if err := RefreshTraktWatchlisted(ShowType, lastActivities.Shows.WatchlistedAt.After(previousActivities.Shows.WatchlistedAt)); err != nil {
			isErrored = true
		}
	}
	if isFirstRun || lastActivities.Shows.HiddenAt.After(previousActivities.Shows.HiddenAt) {
		if err := RefreshTraktHidden(ShowType, lastActivities.Shows.HiddenAt.After(previousActivities.Shows.HiddenAt)); err != nil {
			isErrored = true
		}
	}

	// Seasons
	if isFirstRun || lastActivities.Seasons.WatchlistedAt.After(previousActivities.Seasons.WatchlistedAt) {
		if err := RefreshTraktWatchlisted(SeasonType, lastActivities.Seasons.WatchlistedAt.After(previousActivities.Seasons.WatchlistedAt)); err != nil {
			isErrored = true
		}
	}
	if isFirstRun || lastActivities.Seasons.HiddenAt.After(previousActivities.Seasons.HiddenAt) {
		if err := RefreshTraktHidden(SeasonType, lastActivities.Seasons.HiddenAt.After(previousActivities.Seasons.HiddenAt)); err != nil {
			isErrored = true
		}
	}

	// Lists
	if isFirstRun || lastActivities.Lists.UpdatedAt.After(previousActivities.Lists.UpdatedAt) {
		if err := RefreshTraktLists(lastActivities.Lists.UpdatedAt.After(previousActivities.Lists.UpdatedAt)); err != nil {
			isErrored = true
		}
	}

	return nil
}

// RefreshTraktWatched ...
func RefreshTraktWatched(itemType int, isRefreshNeeded bool) error {
	if config.Get().TraktToken == "" || !config.Get().TraktSyncWatched {
		return nil
	}

	l.mu.Trakt.Lock()
	defer l.mu.Trakt.Unlock()

	started := time.Now()
	defer func() {
		log.Debugf("Trakt sync watched for '%d' finished in %s", itemType, time.Since(started))
		RefreshUIDsRunner(true)
	}()

	if itemType == MovieType {
		return refreshTraktMoviesWatched(isRefreshNeeded)
	} else if itemType == EpisodeType || itemType == SeasonType || itemType == ShowType {
		return refreshTraktShowsWatched(isRefreshNeeded)
	}

	return nil
}

func refreshTraktMoviesWatched(isRefreshNeeded bool) error {
	l.Running.IsMovies = true
	defer func() {
		l.Running.IsMovies = false
	}()

	previous, _ := trakt.PreviousWatchedMovies()
	current, err := trakt.WatchedMovies(isRefreshNeeded)
	if err != nil {
		log.Warningf("Got error from getting watched movies: %s", err)
		return err
	} else if len(current) == 0 {
		return nil
	}

	cacheStore := cache.NewDBStore()

	lastPlaycount := map[uint64]bool{}
	syncPlaycount := map[uint64]bool{}

	lastCacheKey := fmt.Sprintf(cache.LibraryWatchedPlaycountKey, "movies")
	syncCacheKey := fmt.Sprintf(cache.LibrarySyncPlaycountKey, "movies")

	// Should parse all movies for Watched marks, but process only difference,
	// to avoid overwriting Kodi unwatched items
	watchedMovies := trakt.DiffWatchedMovies(previous, current)
	unwatchedMovies := trakt.DiffWatchedMovies(current, previous)

	fileKey := uint64(0)
	missedItems := []uint64{}
	l.WatchedTraktMovies = []uint64{}

	// Sync local items with exact list
	for _, m := range watchedMovies {
		updateMovieWatched(m, true)
	}
	for _, m := range unwatchedMovies {
		updateMovieWatched(m, false)
	}

	cacheStore.Get(lastCacheKey, &lastPlaycount)
	cacheStore.Get(syncCacheKey, &syncPlaycount)
	defer cacheStore.Set(lastCacheKey, &lastPlaycount, cache.LibraryWatchedPlaycountExpire)
	defer cacheStore.Set(syncCacheKey, &syncPlaycount, cache.LibrarySyncPlaycountExpire)

	for _, m := range current {
		l.WatchedTraktMovies = addXXItem(l.WatchedTraktMovies, MovieType, m.Movie.IDs)

		if r := getKodiMovieByTraktIDs(m.Movie.IDs); r != nil {
			// Check if we previously set this item as watched, to avoid re-setting local items again and again.
			fileKey = xxhash.Sum64String(r.File)
			if _, ok := lastPlaycount[fileKey]; ok && !r.IsWatched() {
				continue
			}

			// Update local item Watched status if it is unwatched or was added after it is was watched
			if !r.IsWatched() || r.DateAdded.After(m.LastWatchedAt) {
				lastPlaycount[fileKey] = true
				updateMovieWatched(m, true)
			}
		} else {
			missedItems = addXXItem(missedItems, MovieType, m.Movie.IDs)
		}
	}

	if !config.Get().TraktSyncWatchedBack || len(l.Movies) == 0 {
		return nil
	}

	syncWatchMovies := []*trakt.WatchedItem{}
	syncUnwatchMovies := []*trakt.WatchedItem{}

	l.mu.Movies.Lock()
	for _, m := range l.Movies {
		if m.UIDs.TMDB == 0 {
			continue
		}

		fileKey = xxhash.Sum64String(m.File)
		previousRun, isDone := syncPlaycount[fileKey]

		has := hasXXItem(l.WatchedTraktMovies, MovieType, m.UIDs)
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
	l.mu.Movies.Unlock()

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

func refreshTraktShowsWatched(isRefreshNeeded bool) error {
	l.Running.IsShows = true
	defer func() {
		l.Running.IsShows = false
	}()

	previous, _ := trakt.PreviousWatchedShows()
	current, err := trakt.WatchedShows(isRefreshNeeded)
	if err != nil {
		log.Warningf("Got error from getting watched shows: %s", err)
		return err
	} else if len(current) == 0 {
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
	missedItems := []uint64{}
	l.WatchedTraktShows = []uint64{}

	cacheStore.Get(lastCacheKey, &lastPlaycount)
	cacheStore.Get(syncCacheKey, &syncPlaycount)
	defer cacheStore.Set(lastCacheKey, &lastPlaycount, cache.LibraryWatchedPlaycountExpire)
	defer cacheStore.Set(syncCacheKey, &syncPlaycount, cache.LibrarySyncPlaycountExpire)

	// Sync local items with exact list
	for _, s := range watchedShows {
		updateShowWatched(s, true)
	}
	for _, s := range unwatchedShows {
		updateShowWatched(s, false)
	}

	for _, s := range current {
		tmdbShow := tmdb.GetShowByID(strconv.Itoa(s.Show.IDs.TMDB), config.Get().Language)
		completedSeasons := 0
		for _, season := range s.Seasons {
			if tmdbShow != nil {
				if sc := tmdbShow.GetSeasonEpisodes(season.Number); sc != 0 && sc == len(season.Episodes) && season.Number > 0 {
					completedSeasons++

					l.WatchedTraktShows = addXXItem(l.WatchedTraktShows, SeasonType, s.Show.IDs, season.Number)
				}
			}

			for _, episode := range season.Episodes {
				l.WatchedTraktShows = addXXItem(l.WatchedTraktShows, EpisodeType, s.Show.IDs, season.Number, episode.Number)
			}
		}

		if tmdbShow != nil && (completedSeasons == tmdbShow.CountRealSeasons() || s.Watched) {
			s.Watched = true

			l.WatchedTraktShows = addXXItem(l.WatchedTraktShows, ShowType, s.Show.IDs)
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
						missedItems = addXXItem(missedItems, EpisodeType, s.Show.IDs, season.Number, episode.Number)
					}
				}
			}

			if toRun || r.DateAdded.After(s.LastWatchedAt) {
				updateShowWatched(s, true)
			}
		} else {
			missedItems = addXXItem(missedItems, ShowType, s.Show.IDs)
		}
	}

	if !config.Get().TraktSyncWatchedBack || len(l.Shows) == 0 {
		return nil
	}

	syncWatchShows := []*trakt.WatchedItem{}
	syncUnwatchShows := []*trakt.WatchedItem{}

	l.mu.Shows.Lock()
	for _, s := range l.Shows {
		if s.UIDs.TMDB == 0 || hasXXItem(l.WatchedTraktShows, ShowType, s.UIDs) {
			continue
		} else if hasXXItem(missedItems, ShowType, s.UIDs) {
			continue
		}

		for _, e := range s.Episodes {
			fileKey = xxhash.Sum64String(e.File)
			previousRun, isDone := syncPlaycount[fileKey]

			has := hasXXItem(l.WatchedTraktShows, EpisodeType, s.UIDs, e.Season, e.Episode) ||
				hasXXItem(l.WatchedTraktShows, SeasonType, s.UIDs, e.Season)
			if (has && e.IsWatched()) || (!has && !e.IsWatched()) || (isDone && previousRun == e.IsWatched()) {
				continue
			} else if hasXXItem(missedItems, EpisodeType, s.UIDs, e.Season, e.Episode) {
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
	l.mu.Shows.Unlock()

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

func addXXItem(ary []uint64, media int, uids *trakt.IDs, ids ...int) []uint64 {
	traktKey, tmdbKey, imdbKey := getXXItem(ary, media, uids.Trakt, uids.TMDB, uids.IMDB, ids...)

	if traktKey != 0 {
		ary = append(ary, traktKey)
	}
	if tmdbKey != 0 {
		ary = append(ary, tmdbKey)
	}
	if imdbKey != 0 {
		ary = append(ary, imdbKey)
	}

	return ary
}

func hasXXItem(ary []uint64, media int, uids *UniqueIDs, ids ...int) bool {
	traktKey, tmdbKey, imdbKey := getXXItem(ary, media, uids.Trakt, uids.TMDB, uids.IMDB, ids...)

	for _, item := range ary {
		if item == traktKey || item == tmdbKey || item == imdbKey {
			return true
		}
	}

	return false
}

func getXXItem(ary []uint64, media int, traktID int, tmdbID int, imdbID string, ids ...int) (traktKey, tmdbKey, imdbKey uint64) {
	if media == MovieType {
		if traktID != 0 {
			traktKey = xxhash.Sum64String(fmt.Sprintf("%d_%d_%d", media, TraktScraper, traktID))
		}
		if tmdbID != 0 {
			tmdbKey = xxhash.Sum64String(fmt.Sprintf("%d_%d_%d", media, TMDBScraper, tmdbID))
		}
		if imdbID != "" {
			imdbKey = xxhash.Sum64String(fmt.Sprintf("%d_%d_%s", media, IMDBScraper, imdbID))
		}
	} else if media == ShowType {
		if traktID != 0 {
			traktKey = xxhash.Sum64String(fmt.Sprintf("%d_%d_%d", media, TraktScraper, traktID))
		}
		if tmdbID != 0 {
			tmdbKey = xxhash.Sum64String(fmt.Sprintf("%d_%d_%d", media, TMDBScraper, tmdbID))
		}
		if imdbID != "" {
			imdbKey = xxhash.Sum64String(fmt.Sprintf("%d_%d_%s", media, IMDBScraper, imdbID))
		}
	} else if media == SeasonType {
		if traktID != 0 {
			tmdbKey = xxhash.Sum64String(fmt.Sprintf("%d_%d_%d_%d", media, TraktScraper, traktID, ids[0]))
		}
		if tmdbID != 0 {
			tmdbKey = xxhash.Sum64String(fmt.Sprintf("%d_%d_%d_%d", media, TMDBScraper, tmdbID, ids[0]))
		}
		if imdbID != "" {
			imdbKey = xxhash.Sum64String(fmt.Sprintf("%d_%d_%s_%d", media, IMDBScraper, imdbID, ids[0]))
		}
	} else if media == EpisodeType {
		if traktID != 0 {
			traktKey = xxhash.Sum64String(fmt.Sprintf("%d_%d_%d_%d_%d", media, TraktScraper, traktID, ids[0], ids[1]))
		}
		if tmdbID != 0 {
			tmdbKey = xxhash.Sum64String(fmt.Sprintf("%d_%d_%d_%d_%d", media, TMDBScraper, tmdbID, ids[0], ids[1]))
		}
		if imdbID != "" {
			imdbKey = xxhash.Sum64String(fmt.Sprintf("%d_%d_%s_%d_%d", media, IMDBScraper, imdbID, ids[0], ids[1]))
		}
	}

	return
}

func getKodiMovieByTraktIDs(ids *trakt.IDs) (r *Movie) {
	if r == nil && ids.TMDB != 0 {
		r, _ = GetMovieByTMDB(ids.TMDB)
	}
	if r == nil && ids.IMDB != "" {
		r, _ = GetMovieByIMDB(ids.IMDB)
	}
	return
}

func getKodiShowByTraktIDs(ids *trakt.IDs) (r *Show) {
	if r == nil && ids.TMDB != 0 {
		r, _ = findShowByTMDB(ids.TMDB)
	}
	if r == nil && ids.IMDB != "" {
		r, _ = findShowByIMDB(ids.IMDB)
	}
	return
}

func updateMovieWatched(m *trakt.WatchedMovie, watched bool) {
	var r = getKodiMovieByTraktIDs(m.Movie.IDs)
	if r == nil {
		return
	}

	// Resetting Resume state to avoid having old resume states,
	// when item is watched on another device
	if watched && !r.IsWatched() {
		if m.Plays <= 0 {
			return
		}

		r.UIDs.Playcount = 1
		xbmc.SetMovieWatchedWithDate(r.UIDs.Kodi, 1, 0, 0, m.LastWatchedAt)
		// TODO: There should be a check for allowing resume state, otherwise we always reset it for already searched items
		// } else if watched && r.IsWatched() && r.Resume != nil && r.Resume.Position > 0 {
		// 	xbmc.SetMovieWatchedWithDate(r.UIDs.Kodi, 1, 0, 0, m.LastWatchedAt)
	} else if !watched && r.IsWatched() {
		r.UIDs.Playcount = 0
		xbmc.SetMoviePlaycount(r.UIDs.Kodi, 0)
	}
}

func updateShowWatched(s *trakt.WatchedShow, watched bool) {
	var r = getKodiShowByTraktIDs(s.Show.IDs)
	if r == nil {
		return
	}

	if watched && s.Watched && !r.IsWatched() {
		r.UIDs.Playcount = 1
		xbmc.SetShowWatchedWithDate(r.UIDs.Kodi, 1, s.LastWatchedAt)
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
					e.UIDs.Playcount = 1
					xbmc.SetEpisodeWatchedWithDate(e.UIDs.Kodi, 1, 0, 0, episode.LastWatchedAt)
					// TODO: There should be a check for allowing resume state, otherwise we always reset it for already searched items
					// } else if watched && e.IsWatched() && e.Resume != nil && e.Resume.Position > 0 {
					//   xbmc.SetEpisodeWatchedWithDate(e.UIDs.Kodi, 1, 0, 0, episode.LastWatchedAt)
				} else if !watched && e.IsWatched() {
					e.UIDs.Playcount = 0
					xbmc.SetEpisodePlaycount(e.UIDs.Kodi, 0)
				}
			}
		}
	}
}

// RefreshTraktCollected ...
func RefreshTraktCollected(itemType int, isRefreshNeeded bool) error {
	if config.Get().TraktToken == "" || !config.Get().TraktSyncCollections {
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
func RefreshTraktWatchlisted(itemType int, isRefreshNeeded bool) error {
	if config.Get().TraktToken == "" || !config.Get().TraktSyncWatchlist {
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
func RefreshTraktPaused(itemType int, isRefreshNeeded bool) error {
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
		log.Debugf("Trakt sync paused for '%d' finished in %s", itemType, time.Since(started))
	}()

	if itemType == MovieType {
		l.Running.IsMovies = true
		defer func() {
			l.Running.IsMovies = false
		}()

		movies, err := trakt.PausedMovies(isRefreshNeeded)
		if err != nil {
			log.Warningf("TraktSync: Got error from PausedMovies: %s", err)
			return err
		}

		for _, m := range movies {
			if m.Movie.IDs.TMDB == 0 || int(m.Progress) <= 0 || m.Movie.Runtime <= 0 {
				continue
			}

			if lm, err := GetMovieByTMDB(m.Movie.IDs.TMDB); err == nil {
				if t, ok := lastUpdates[m.Movie.IDs.Trakt]; ok && !t.Before(m.PausedAt) {
					continue
				}

				lastUpdates[m.Movie.IDs.Trakt] = m.PausedAt
				runtime := m.Movie.Runtime * 60

				xbmc.SetMovieProgressWithDate(lm.UIDs.Kodi, runtime/100*int(m.Progress), runtime, m.PausedAt)
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
		}

		for _, s := range shows {
			if s.Show.IDs.TMDB == 0 || int(s.Progress) <= 0 || s.Episode.Runtime <= 0 {
				continue
			}

			if ls, err := GetShowByTMDB(s.Show.IDs.TMDB); err == nil {
				e := ls.GetEpisode(s.Episode.Season, s.Episode.Number)
				if e == nil {
					continue
				} else if t, ok := lastUpdates[s.Episode.IDs.Trakt]; ok && !t.Before(s.PausedAt) {
					continue
				}

				lastUpdates[s.Episode.IDs.Trakt] = s.PausedAt
				runtime := s.Episode.Runtime * 60

				xbmc.SetEpisodeProgressWithDate(e.UIDs.Kodi, runtime/100*int(s.Progress), runtime, s.PausedAt)
			}
		}
	}

	return nil
}

// RefreshTraktHidden ...
func RefreshTraktHidden(itemType int, isRefreshNeeded bool) error {
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
func RefreshTraktLists(isRefreshNeeded bool) error {
	if config.Get().TraktToken == "" || !config.Get().TraktSyncUserlists {
		return nil
	}

	lists := trakt.Userlists()
	for _, list := range lists {
		if err := SyncMoviesList(strconv.Itoa(list.IDs.Trakt), false, isRefreshNeeded); err != nil {
			continue
		}
		if err := SyncShowsList(strconv.Itoa(list.IDs.Trakt), false, isRefreshNeeded); err != nil {
			continue
		}
	}

	return nil
}

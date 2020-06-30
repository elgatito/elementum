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
		return nil
	} else if l.Running.IsTrakt {
		log.Debugf("TraktSync: already in scanning")
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
		return err
	}
	var previousActivities trakt.UserActivities
	_ = cacheStore.Get("com.trakt.last_activities", &previousActivities)

	// If nothing changed from last check - skip everything
	isFirstRun := !IsTraktInitialized || isKodiUpdated
	if !lastActivities.All.After(previousActivities.All) && !isFirstRun {
		log.Debugf("Skipping Trakt sync due to stale activities")
		return nil
	}

	isErrored := false
	defer func() {
		if !isErrored {
			_ = cacheStore.Set("com.trakt.last_activities", lastActivities, 30*24*time.Hour)
		}
	}()

	if isFirstRun {
		l.mu.Trakt.Lock()
		l.WatchedTrakt = []uint64{}
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
		err := RefreshTraktWatchlisted(SeasonType, lastActivities.Seasons.WatchlistedAt.After(previousActivities.Seasons.WatchlistedAt))
		if err != nil {
			isErrored = true
		}
	}
	if isFirstRun || lastActivities.Seasons.HiddenAt.After(previousActivities.Seasons.HiddenAt) {
		err := RefreshTraktHidden(SeasonType, lastActivities.Seasons.HiddenAt.After(previousActivities.Seasons.HiddenAt))
		if err != nil {
			isErrored = true
		}
	}

	// Lists
	if isFirstRun || lastActivities.Lists.UpdatedAt.After(previousActivities.Lists.UpdatedAt) {
		err := RefreshTraktLists(lastActivities.Lists.UpdatedAt.After(previousActivities.Lists.UpdatedAt))
		if err != nil {
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

	cacheStore := cache.NewDBStore()
	lastPlaycount := map[uint64]bool{}
	cacheKey := fmt.Sprintf("WatchedLastPlaycount.%d", itemType)

	if itemType == MovieType {
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
			// Kind of strange check to make sure Trakt watched items are not empty
			return nil
		} else if len(previous) > 50 && len(current) > 50 && len(previous)/len(current) > 2 {
			// If current*2 < previous then skipping updates
			return nil
		}

		// Should parse all movies for Watched marks, but process only difference,
		// to avoid overwriting Kodi unwatched items
		watchedMovies := trakt.DiffWatchedMovies(previous, current)
		unwatchedMovies := trakt.DiffWatchedMovies(current, previous)

		watchedTraktMovies := make([]int, 0, len(current))

		syncSingleMoviesDelete := []*trakt.WatchedItem{}
		syncSingleMoviesAdd := []*trakt.WatchedItem{}

		cacheStore.Get(cacheKey, &lastPlaycount)
		for _, m := range current {
			if m.Plays > 1 && config.Get().TraktSyncWatchedSingle {
				syncSingleMoviesDelete = append(syncSingleMoviesDelete, &trakt.WatchedItem{
					MediaType: "movie",
					Movie:     m.Movie.IDs.TMDB,
					Watched:   false,
				})
				syncSingleMoviesAdd = append(syncSingleMoviesAdd, &trakt.WatchedItem{
					MediaType: "movie",
					Movie:     m.Movie.IDs.TMDB,
					Watched:   true,
					WatchedAt: m.LastWatchedAt,
				})
			}

			if r := getKodiMovieByTraktIDs(m.Movie.IDs); r != nil {
				watchedTraktMovies = append(watchedTraktMovies, r.UIDs.TMDB)

				if _, ok := lastPlaycount[xxhash.Sum64String(r.File)]; ok && !r.IsWatched() {
					delete(lastPlaycount, xxhash.Sum64String(r.File))
					continue
				}
				lastPlaycount[xxhash.Sum64String(r.File)] = true

				if !r.IsWatched() || r.DateAdded.After(m.LastWatchedAt) {
					updateMovieWatched(m, true)
				}
			}

			l.WatchedTrakt = append(l.WatchedTrakt,
				xxhash.Sum64String(fmt.Sprintf("%d_%d_%d", MovieType, TraktScraper, m.Movie.IDs.Trakt)),
				xxhash.Sum64String(fmt.Sprintf("%d_%d_%d", MovieType, TMDBScraper, m.Movie.IDs.TMDB)),
				xxhash.Sum64String(fmt.Sprintf("%d_%d_%s", MovieType, IMDBScraper, m.Movie.IDs.IMDB)))
		}
		cacheStore.Set(cacheKey, &lastPlaycount, 30*24*time.Hour)

		for _, m := range watchedMovies {
			updateMovieWatched(m, true)
		}
		for _, m := range unwatchedMovies {
			updateMovieWatched(m, false)
		}

		// If we have items that have multiple "plays", we should leave only 1,
		// it will take off the load from Trakt API
		if len(syncSingleMoviesDelete) > 0 && len(syncSingleMoviesAdd) > 0 {
			trakt.SetMultipleWatched(syncSingleMoviesDelete)
			trakt.SetMultipleWatched(syncSingleMoviesAdd)
		}

		if !config.Get().TraktSyncWatchedBack || len(l.Movies) == 0 {
			return nil
		}

		syncMovies := []*trakt.WatchedItem{}
		l.mu.Movies.Lock()
		for _, m := range l.Movies {
			if m.UIDs.TMDB == 0 {
				continue
			}

			has := hasItem(watchedTraktMovies, m.UIDs.TMDB)
			if (has && m.IsWatched()) || (!has && !m.IsWatched()) {
				continue
			}

			syncMovies = append(syncMovies, &trakt.WatchedItem{
				MediaType: "movie",
				Movie:     m.UIDs.TMDB,
				Watched:   !has && m.IsWatched(),
			})
		}
		l.mu.Movies.Unlock()

		if len(syncMovies) > 0 {
			trakt.SetMultipleWatched(syncMovies)
		}
	} else if itemType == EpisodeType || itemType == SeasonType || itemType == ShowType {
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
		} else if len(previous) > 5 && len(current) > 5 && len(previous)/len(current) > 2 {
			// If current*2 < previous then skipping updates
			return nil
		}

		// Should parse all shows for Watched marks, but process only difference,
		// to avoid overwriting Kodi unwatched items
		watchedShows := trakt.DiffWatchedShows(previous, current)
		unwatchedShows := trakt.DiffWatchedShows(current, previous)

		watchedTraktShows := []int{}
		watchedTraktEpisodes := []int{}

		cacheStore.Get(cacheKey, &lastPlaycount)
		for _, s := range current {
			syncSingleShowsDelete := []*trakt.WatchedItem{}
			syncSingleShowsAdd := []*trakt.WatchedItem{}

			tmdbShow := tmdb.GetShowByID(strconv.Itoa(s.Show.IDs.TMDB), config.Get().Language)
			completedSeasons := 0
			for _, season := range s.Seasons {
				if tmdbShow != nil {
					if sc := tmdbShow.GetSeasonEpisodes(season.Number); sc != 0 && sc == len(season.Episodes) {
						completedSeasons++

						l.WatchedTrakt = append(l.WatchedTrakt,
							xxhash.Sum64String(fmt.Sprintf("%d_%d_%d_%d", SeasonType, TMDBScraper, s.Show.IDs.TMDB, season.Number)),
							xxhash.Sum64String(fmt.Sprintf("%d_%d_%d_%d", SeasonType, TraktScraper, s.Show.IDs.Trakt, season.Number)))
					}
				}

				for _, episode := range season.Episodes {
					if episode.Plays > 1 && config.Get().TraktSyncWatchedSingle {
						syncSingleShowsDelete = append(syncSingleShowsDelete, &trakt.WatchedItem{
							MediaType: "episode",
							Show:      s.Show.IDs.TMDB,
							Season:    season.Number,
							Episode:   episode.Number,
							Watched:   false,
						})
						syncSingleShowsAdd = append(syncSingleShowsAdd, &trakt.WatchedItem{
							MediaType: "episode",
							Show:      s.Show.IDs.TMDB,
							Season:    season.Number,
							Episode:   episode.Number,
							Watched:   true,
							WatchedAt: episode.LastWatchedAt,
						})
					}

					l.WatchedTrakt = append(l.WatchedTrakt,
						xxhash.Sum64String(fmt.Sprintf("%d_%d_%d_%d_%d", EpisodeType, TMDBScraper, s.Show.IDs.TMDB, season.Number, episode.Number)),
						xxhash.Sum64String(fmt.Sprintf("%d_%d_%d_%d_%d", EpisodeType, TraktScraper, s.Show.IDs.Trakt, season.Number, episode.Number)))
				}
			}

			if tmdbShow != nil && (completedSeasons == len(tmdbShow.Seasons) || s.Watched) {
				s.Watched = true

				l.WatchedTrakt = append(l.WatchedTrakt,
					xxhash.Sum64String(fmt.Sprintf("%d_%d_%d", ShowType, TMDBScraper, s.Show.IDs.TMDB)),
					xxhash.Sum64String(fmt.Sprintf("%d_%d_%d", ShowType, TraktScraper, s.Show.IDs.Trakt)))
			}

			if r := getKodiShowByTraktIDs(s.Show.IDs); r != nil {
				if s.Watched {
					watchedTraktShows = append(watchedTraktShows, r.UIDs.Kodi)
				}

				toRun := false
				for _, season := range s.Seasons {
					for _, episode := range season.Episodes {
						if e := r.GetEpisode(season.Number, episode.Number); e != nil {
							watchedTraktEpisodes = append(watchedTraktEpisodes, e.UIDs.Kodi)

							if _, ok := lastPlaycount[xxhash.Sum64String(e.File)]; ok && !r.IsWatched() {
								delete(lastPlaycount, xxhash.Sum64String(e.File))
								continue
							}
							lastPlaycount[xxhash.Sum64String(e.File)] = true

							if !e.IsWatched() {
								toRun = true
							}
						}
					}
				}

				if toRun || r.DateAdded.After(s.LastWatchedAt) {
					updateShowWatched(s, true)
				}
			}

			// If we have items that have multiple "plays", we should leave only 1,
			// it will take off the load from Trakt API
			if len(syncSingleShowsDelete) > 0 && len(syncSingleShowsAdd) > 0 {
				trakt.SetMultipleWatched(syncSingleShowsDelete)
				trakt.SetMultipleWatched(syncSingleShowsAdd)
			}
		}
		cacheStore.Set(cacheKey, &lastPlaycount, 30*24*time.Hour)

		for _, s := range watchedShows {
			updateShowWatched(s, true)
		}
		for _, s := range unwatchedShows {
			updateShowWatched(s, false)
		}

		if !config.Get().TraktSyncWatchedBack || len(l.Shows) == 0 {
			return nil
		}

		syncShows := []*trakt.WatchedItem{}
		l.mu.Shows.Lock()
		for _, s := range l.Shows {
			if s.UIDs.TMDB == 0 || hasItem(watchedTraktShows, s.UIDs.Kodi) {
				continue
			}

			for _, e := range s.Episodes {
				has := hasItem(watchedTraktEpisodes, e.UIDs.Kodi)
				if (has && e.IsWatched()) || (!has && !e.IsWatched()) {
					continue
				}

				syncShows = append(syncShows, &trakt.WatchedItem{
					MediaType: "episode",
					Show:      s.UIDs.TMDB,
					Season:    e.Season,
					Episode:   e.Episode,
					Watched:   !has && e.IsWatched(),
				})
			}
		}
		l.mu.Shows.Unlock()

		if len(syncShows) > 0 {
			trakt.SetMultipleWatched(syncShows)
		}
	}

	return nil
}

func hasItem(ary []int, item int) bool {
	for _, i := range ary {
		if i == item {
			return true
		}
	}

	return false
}

func hasStringItem(ary []string, item string) bool {
	for _, i := range ary {
		if i == item {
			return true
		}
	}

	return false
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

	cacheKey := fmt.Sprintf("PausedLastUpdates.%d", itemType)
	cacheStore.Get(cacheKey, &lastUpdates)
	defer func() {
		cacheStore.Set(cacheKey, &lastUpdates, 30*24*time.Hour)
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

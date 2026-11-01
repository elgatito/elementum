package library

import (
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/anacrolix/missinggo/perf"
	"github.com/cespare/xxhash/v2"

	"github.com/elgatito/elementum/cache"
	"github.com/elgatito/elementum/config"
	"github.com/elgatito/elementum/library/uid"
	"github.com/elgatito/elementum/tmdb"
	"github.com/elgatito/elementum/tracker"
	"github.com/elgatito/elementum/trakt"
	"github.com/elgatito/elementum/xbmc"
)

var (
	// IsTrackerInitialized used to mark if we need only incremental updates from the tracker
	IsTrackerInitialized bool
	isKodiAdded          bool
	isKodiUpdated        bool

	lastTrackerID   string
	lastTrackerSync time.Time
)

// RefreshTracker gets user activities from the active tracker
// to see if we need to add movies/set watched status and so on
func RefreshTracker() error {
	return refreshTracker(false)
}

// ResyncTracker runs a full sync with the active tracker right away, ignoring the minimal sync interval.
func ResyncTracker() error {
	IsTrackerInitialized = false
	return refreshTracker(true)
}

func refreshTracker(force bool) error {
	xbmcHost, err := xbmc.GetLocalXBMCHost()
	if xbmcHost == nil || err != nil {
		log.Debugf("Stopping tracker refresh due to missing XBMC host")
		return err
	}

	l := uid.Get()
	p := tracker.Active()
	if p == nil || !config.Get().TraktSyncEnabled || (!config.Get().TraktSyncPlaybackEnabled && xbmcHost.PlayerIsPlaying()) {
		// Even if sync is disabled, check if current Trakt auth is fine to use.
		if config.Get().TraktToken != "" && !config.Get().TraktAuthorized {
			trakt.GetLastActivities()
		}

		l.Pending.IsTracker = false
		return nil
	} else if l.Running.IsTracker {
		log.Debugf("TrackerSync: already in scanning")
		return nil
	} else if l.Running.IsOverall {
		return nil
	}

	// Switching trackers starts over, as previous state belongs to another tracker
	if p.ID() != lastTrackerID {
		lastTrackerID = p.ID()
		IsTrackerInitialized = false
	}
	if !force && IsTrackerInitialized && time.Since(lastTrackerSync) < p.MinSyncInterval() {
		l.Pending.IsTracker = false
		return nil
	}

	l.Pending.IsTracker = false
	l.Running.IsTracker = true
	defer func() {
		l.Running.IsTracker = false
	}()

	log.Infof("Running %s sync", p.Name())
	started := time.Now()
	lastTrackerSync = started
	defer func() {
		log.Infof("%s sync finished in %s", p.Name(), time.Since(started))
	}()

	activities, err := tracker.GetActivities(p, "")
	if err != nil {
		log.Warningf("Cannot get activities: %s", err)
		return err
	}

	// If nothing changed from last check - skip everything
	isFirstRun := !IsTrackerInitialized || isKodiUpdated
	if !activities.Any() && !isFirstRun {
		log.Debugf("Skipping %s sync due to stale activities", p.Name())
		return nil
	}

	isErrored := false
	defer func() {
		if !isErrored {
			activities.Save()
		}
	}()

	if isFirstRun {
		IsTrackerInitialized = true
		isKodiUpdated = false
		isKodiAdded = false
	}

	run := func(f func() error) {
		if err := f(); err != nil {
			isErrored = true
		}
	}

	// Movies
	if isFirstRun || isKodiAdded || activities.Changed(tracker.MoviesWatched) {
		run(func() error {
			return RefreshTrackerWatched(xbmcHost, p, MovieType, activities.Changed(tracker.MoviesWatched))
		})
	}
	if isFirstRun || activities.Changed(tracker.MoviesCollected) {
		run(func() error {
			return RefreshTrackerCollected(p, MovieType, activities.Changed(tracker.MoviesCollected))
		})
	}
	if isFirstRun || activities.Changed(tracker.MoviesWatchlisted) {
		run(func() error {
			return RefreshTrackerWatchlisted(p, MovieType, activities.Changed(tracker.MoviesWatchlisted))
		})
	}
	if isFirstRun || isKodiAdded || activities.Changed(tracker.MoviesPaused) {
		run(func() error {
			return RefreshTrackerPaused(xbmcHost, p, MovieType, activities.Changed(tracker.MoviesPaused))
		})
	}
	if isFirstRun || activities.Changed(tracker.MoviesHidden) {
		run(func() error { return RefreshTrackerHidden(p, MovieType, activities.Changed(tracker.MoviesHidden)) })
	}

	// Episodes
	if isFirstRun || isKodiAdded || activities.Changed(tracker.EpisodesWatched) {
		run(func() error {
			return RefreshTrackerWatched(xbmcHost, p, EpisodeType, activities.Changed(tracker.EpisodesWatched))
		})
	}
	if isFirstRun || activities.Changed(tracker.EpisodesCollected) {
		run(func() error {
			return RefreshTrackerCollected(p, EpisodeType, activities.Changed(tracker.EpisodesCollected))
		})
	}
	if isFirstRun || activities.Changed(tracker.EpisodesWatchlisted) {
		run(func() error {
			return RefreshTrackerWatchlisted(p, EpisodeType, activities.Changed(tracker.EpisodesWatchlisted))
		})
	}
	if isFirstRun || isKodiAdded || activities.Changed(tracker.EpisodesPaused) {
		run(func() error {
			return RefreshTrackerPaused(xbmcHost, p, EpisodeType, activities.Changed(tracker.EpisodesPaused))
		})
	}

	// Shows
	if isFirstRun || activities.Changed(tracker.ShowsWatchlisted) {
		run(func() error {
			return RefreshTrackerWatchlisted(p, ShowType, activities.Changed(tracker.ShowsWatchlisted))
		})
	}
	if isFirstRun || activities.Changed(tracker.ShowsHidden) {
		run(func() error { return RefreshTrackerHidden(p, ShowType, activities.Changed(tracker.ShowsHidden)) })
	}

	// Seasons
	if isFirstRun || activities.Changed(tracker.SeasonsWatchlisted) {
		run(func() error {
			return RefreshTrackerWatchlisted(p, SeasonType, activities.Changed(tracker.SeasonsWatchlisted))
		})
	}
	if isFirstRun || activities.Changed(tracker.SeasonsHidden) {
		run(func() error { return RefreshTrackerHidden(p, SeasonType, activities.Changed(tracker.SeasonsHidden)) })
	}

	// Lists
	if isFirstRun || activities.Changed(tracker.ListsUpdated) {
		run(func() error { return RefreshTrackerLists(p, activities.Changed(tracker.ListsUpdated)) })
	}

	return nil
}

// RefreshTrackerWatched ...
func RefreshTrackerWatched(xbmcHost *xbmc.XBMCHost, p tracker.Provider, itemType int, isRefreshNeeded bool) error {
	if !config.Get().TraktSyncWatched {
		return nil
	}

	l := uid.Get()
	mu := l.GetMutex(uid.TrackerMutex)
	mu.Lock()
	defer mu.Unlock()

	started := time.Now()
	defer func() {
		log.Debugf("%s sync watched for '%s' finished in %s", p.Name(), ItemTypes[itemType], time.Since(started))
		RefreshUIDsRunner(true)
	}()

	if itemType == MovieType {
		return refreshTrackerMoviesWatched(xbmcHost, p, isRefreshNeeded)
	} else if itemType == EpisodeType || itemType == SeasonType || itemType == ShowType {
		return refreshTrackerShowsWatched(xbmcHost, p, isRefreshNeeded)
	}

	return nil
}

func refreshTrackerMoviesWatched(xbmcHost *xbmc.XBMCHost, p tracker.Provider, isRefreshNeeded bool) error {
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

	previous, current, err := tracker.FetchWatched(p, tracker.MovieType, isRefreshNeeded)
	if err != nil {
		log.Warningf("Got error from getting watched movies: %s", err)
		return err
	} else if len(current) == 0 || config.Get().LibraryReadOnly {
		return nil
	}

	ItemsToUID(uid.WatchedMoviesContainer, watchedToItems(current))

	cacheStore := cache.NewDBStore()

	lastPlaycount := map[uint64]bool{}
	syncPlaycount := map[uint64]bool{}

	lastCacheKey := fmt.Sprintf(cache.LibraryWatchedPlaycountKey, "movies")
	syncCacheKey := fmt.Sprintf(cache.LibrarySyncPlaycountKey, "movies")

	// Should parse all movies for Watched marks, but process only difference,
	// to avoid overwriting Kodi unwatched items
	watchedMovies := tracker.DiffWatchedMovies(previous, current, true)
	unwatchedMovies := tracker.DiffWatchedMovies(current, previous, false)

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
		if m == nil {
			continue
		}

		if r := getKodiMovieByIDs(m.IDs); r != nil {
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
			missedItems.Add(MovieType, toUniqueIDs(m.IDs))
		}
	}

	if !config.Get().TraktSyncWatchedBack || len(l.Movies) == 0 {
		return nil
	}

	syncWatchMovies := []*tracker.WatchedItem{}
	syncUnwatchMovies := []*tracker.WatchedItem{}

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

		item := &tracker.WatchedItem{
			Item:    tracker.Item{Type: tracker.MovieType, IDs: tracker.IDs{TMDB: m.UIDs.TMDB, IMDB: m.UIDs.IMDB}},
			KodiKey: fileKey,
			Watched: !has && m.IsWatched(),
		}
		if item.Watched {
			syncWatchMovies = append(syncWatchMovies, item)
		} else {
			syncUnwatchMovies = append(syncUnwatchMovies, item)
		}
	}
	mu.Unlock()

	if len(syncUnwatchMovies) > 0 {
		if err := p.SetWatched(syncUnwatchMovies, false); err == nil {
			// Set cached entry to avoid running same item again
			for _, i := range syncUnwatchMovies {
				delete(lastPlaycount, i.KodiKey)
				syncPlaycount[i.KodiKey] = i.Watched
			}
		}
	}
	if len(syncWatchMovies) > 0 {
		if err := p.SetWatched(syncWatchMovies, true); err == nil {
			// Set cached entry to avoid running same item again
			for _, i := range syncWatchMovies {
				syncPlaycount[i.KodiKey] = i.Watched
			}
		}
	}

	return nil
}

func refreshTrackerShowsWatched(xbmcHost *xbmc.XBMCHost, p tracker.Provider, isRefreshNeeded bool) error {
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

	previous, current, err := tracker.FetchWatched(p, tracker.ShowType, isRefreshNeeded)
	if err != nil {
		log.Warningf("Got error from getting watched shows: %s", err)
		return err
	} else if len(current) == 0 || config.Get().LibraryReadOnly {
		// Kind of strange check to make sure watched items are not empty
		return nil
	}

	cacheStore := cache.NewDBStore()

	lastPlaycount := map[uint64]bool{}
	syncPlaycount := map[uint64]bool{}

	lastCacheKey := fmt.Sprintf(cache.LibraryWatchedPlaycountKey, "shows")
	syncCacheKey := fmt.Sprintf(cache.LibrarySyncPlaycountKey, "shows")

	// Should parse all shows for Watched marks, but process only difference,
	// to avoid overwriting Kodi unwatched items
	watchedShows := tracker.DiffWatchedShows(previous, current)
	unwatchedShows := tracker.DiffWatchedShows(current, previous)

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
		if s == nil {
			continue
		}
		uids := toUniqueIDs(s.IDs)

		perSeason := map[int]int{}
		for _, e := range s.Episodes {
			perSeason[e.Season]++
			container.Add(EpisodeType, uids, e.Season, e.Number)
		}

		tmdbShow := tmdb.GetShowByID(strconv.Itoa(s.IDs.TMDB), config.Get().Language)
		completedSeasons := 0
		if tmdbShow != nil {
			for season, count := range perSeason {
				if sc := tmdbShow.GetSeasonEpisodes(season); sc != 0 && sc == count && season > 0 {
					completedSeasons++

					container.Add(SeasonType, uids, season)
				}
			}
		}

		if tmdbShow != nil && ((completedSeasons == tmdbShow.CountRealSeasons() && tmdbShow.CountRealSeasons() != 0) || s.Watched) {
			s.Watched = true

			container.Add(ShowType, uids)
		}

		if r := getKodiShowByIDs(s.IDs); r != nil {
			toRun := false
			for i, episode := range s.Episodes {
				if e := r.GetEpisode(episode.Season, episode.Number); e != nil {
					fileKey = xxhash.Sum64String(e.File)
					if _, ok := lastPlaycount[fileKey]; ok && !e.IsWatched() {
						// Reset Plays to mark this episode as non-watched
						s.Episodes[i].Plays = 0
						continue
					}

					if !e.IsWatched() {
						lastPlaycount[fileKey] = true
						toRun = true
					}
				} else {
					missedItems.Add(EpisodeType, uids, episode.Season, episode.Number)
				}
			}

			if toRun || r.DateAdded.After(s.LastWatchedAt) {
				updateShowWatched(xbmcHost, s, true)
			}
		} else {
			missedItems.Add(ShowType, uids)
		}
	}

	if !config.Get().TraktSyncWatchedBack || len(l.Shows) == 0 {
		return nil
	}

	syncWatchShows := []*tracker.WatchedItem{}
	syncUnwatchShows := []*tracker.WatchedItem{}

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

			item := &tracker.WatchedItem{
				Item: tracker.Item{
					Type:    tracker.EpisodeType,
					IDs:     tracker.IDs{TMDB: s.UIDs.TMDB, IMDB: s.UIDs.IMDB, TVDB: s.UIDs.TVDB},
					Season:  e.Season,
					Episode: e.Episode,
				},
				KodiKey: fileKey,
				Watched: !has && e.IsWatched(),
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
		if err := p.SetWatched(syncUnwatchShows, false); err == nil {
			// Set cached entry to avoid running same item again
			for _, i := range syncUnwatchShows {
				delete(lastPlaycount, i.KodiKey)
				syncPlaycount[i.KodiKey] = i.Watched
			}
		}
	}
	if len(syncWatchShows) > 0 {
		if err := p.SetWatched(syncWatchShows, true); err == nil {
			// Set cached entry to avoid running same item again
			for _, i := range syncWatchShows {
				syncPlaycount[i.KodiKey] = i.Watched
			}
		}
	}

	return nil
}

func getKodiMovieByIDs(ids tracker.IDs) (r *uid.Movie) {
	if r == nil && ids.TMDB != 0 {
		r, _ = uid.GetMovieByTMDB(ids.TMDB)
	}
	if r == nil && ids.IMDB != "" {
		r, _ = uid.GetMovieByIMDB(ids.IMDB)
	}
	return
}

func getKodiShowByIDs(ids tracker.IDs) (r *uid.Show) {
	if r == nil && ids.TMDB != 0 {
		r, _ = uid.FindShowByTMDB(ids.TMDB)
	}
	if r == nil && ids.IMDB != "" {
		r, _ = uid.FindShowByIMDB(ids.IMDB)
	}
	return
}

func updateMovieWatched(xbmcHost *xbmc.XBMCHost, m *tracker.WatchedItem, watched bool) {
	if m == nil {
		return
	}

	var r = getKodiMovieByIDs(m.IDs)
	if r == nil {
		return
	}

	// Resetting Resume state to avoid having old resume states,
	// when item is watched on another device
	if watched {
		if m.Plays <= 0 {
			return
		}

		// Skip setting item as watched if it is the same LastPlayed (or newer) as in the tracker
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

func updateShowWatched(xbmcHost *xbmc.XBMCHost, s *tracker.WatchedItem, watched bool) {
	if s == nil {
		return
	}

	var r = getKodiShowByIDs(s.IDs)
	if r == nil {
		return
	}

	if watched && s.Watched && !r.IsWatched() {
		r.UIDs.Playcount = 1
		xbmcHost.SetShowWatchedWithDate(r.UIDs.Kodi, 1, s.LastWatchedAt)
	}

	for _, episode := range s.Episodes {
		if episode.Plays <= 0 {
			continue
		}

		e := r.GetEpisode(episode.Season, episode.Number)
		if e != nil {
			// Resetting Resume state to avoid having old resume states,
			// when item is watched on another device
			if watched && !e.IsWatched() {
				// Skip setting item as watched if it is the same LastPlayed (or newer) as in the tracker
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

// RefreshTrackerCollected ...
func RefreshTrackerCollected(p tracker.Provider, itemType int, isRefreshNeeded bool) error {
	if _, ok := p.(tracker.Collector); !ok {
		return nil
	}

	if itemType == MovieType {
		if err := SyncMoviesList("collection", false, isRefreshNeeded); err != nil {
			log.Warningf("TrackerSync: Got error from SyncMoviesList for Collection: %s", err)
			return err
		}
	} else if itemType == EpisodeType || itemType == SeasonType || itemType == ShowType {
		if err := SyncShowsList("collection", false, isRefreshNeeded); err != nil {
			log.Warningf("TrackerSync: Got error from SyncShowsList for Collection: %s", err)
			return err
		}
	}

	return nil
}

// RefreshTrackerWatchlisted ...
func RefreshTrackerWatchlisted(p tracker.Provider, itemType int, isRefreshNeeded bool) error {
	if itemType == MovieType {
		if err := SyncMoviesList("watchlist", false, isRefreshNeeded); err != nil {
			log.Warningf("TrackerSync: Got error from SyncMoviesList for Watchlist: %s", err)
			return err
		}
	} else if itemType == EpisodeType || itemType == SeasonType || itemType == ShowType {
		if err := SyncShowsList("watchlist", false, isRefreshNeeded); err != nil {
			log.Warningf("TrackerSync: Got error from SyncShowsList for Watchlist: %s", err)
			return err
		}
	}

	return nil
}

// RefreshTrackerPaused ...
func RefreshTrackerPaused(xbmcHost *xbmc.XBMCHost, p tracker.Provider, itemType int, isRefreshNeeded bool) error {
	pr, ok := p.(tracker.PlaybackReader)
	if !ok || !config.Get().TraktSyncPlaybackProgress {
		return nil
	}

	cacheStore := cache.NewDBStore()
	lastUpdates := map[string]time.Time{}

	cacheKey := fmt.Sprintf("com.tracker.%s.paused.%d", p.ID(), itemType)
	cacheStore.Get(cacheKey, &lastUpdates)
	defer func() {
		cacheStore.Set(cacheKey, &lastUpdates, tracker.StateExpire)
	}()

	started := time.Now()
	defer func() {
		log.Debugf("%s sync paused for '%s' finished in %s", p.Name(), ItemTypes[itemType], time.Since(started))
	}()

	l := uid.Get()

	if itemType == MovieType {
		l.Running.IsMovies = true
		defer func() {
			l.Running.IsMovies = false
		}()

		movies, err := pr.Paused(tracker.MovieType, isRefreshNeeded)
		if err != nil {
			log.Warningf("TrackerSync: Got error from Paused movies: %s", err)
			return err
		} else if config.Get().LibraryReadOnly {
			return nil
		}

		for _, m := range movies {
			if m == nil || m.IDs.TMDB == 0 || int(m.Progress) <= 0 {
				continue
			}

			if lm, err := uid.GetMovieByTMDB(m.IDs.TMDB); err == nil {
				if t, ok := lastUpdates[m.Key()]; ok && !t.Before(m.PausedAt) {
					continue
				}
				// Not every tracker sends runtime
				if m.Runtime <= 0 {
					if tm := tmdb.GetMovie(m.IDs.TMDB, config.Get().Language); tm != nil {
						m.Runtime = tm.Runtime * 60
					}
				}
				if m.Runtime <= 0 {
					continue
				}

				lastUpdates[m.Key()] = m.PausedAt
				xbmcHost.SetMovieProgressWithDate(lm.UIDs.Kodi, m.Runtime/100*int(m.Progress), m.Runtime, m.PausedAt)
			}
		}
	} else if itemType == EpisodeType || itemType == SeasonType || itemType == ShowType {
		l.Running.IsShows = true
		defer func() {
			l.Running.IsShows = false
		}()

		shows, err := pr.Paused(tracker.EpisodeType, isRefreshNeeded)
		if err != nil {
			log.Warningf("TrackerSync: Got error from Paused episodes: %s", err)
			return err
		} else if config.Get().LibraryReadOnly {
			return nil
		}

		for _, s := range shows {
			if s == nil || s.IDs.TMDB == 0 || int(s.Progress) <= 0 {
				continue
			}

			if ls, err := uid.GetShowByTMDB(s.IDs.TMDB); err == nil {
				e := ls.GetEpisode(s.Season, s.Episode)
				if e == nil {
					continue
				} else if t, ok := lastUpdates[s.Key()]; ok && !t.Before(s.PausedAt) {
					continue
				}
				// Not every tracker sends runtime
				if s.Runtime <= 0 {
					if te := tmdb.GetEpisode(s.IDs.TMDB, s.Season, s.Episode, config.Get().Language); te != nil {
						s.Runtime = te.Runtime * 60
					}
				}
				if s.Runtime <= 0 {
					continue
				}

				lastUpdates[s.Key()] = s.PausedAt
				xbmcHost.SetEpisodeProgressWithDate(e.UIDs.Kodi, s.Runtime/100*int(s.Progress), s.Runtime, s.PausedAt)
			}
		}
	}

	return nil
}

// RefreshTrackerHidden ...
func RefreshTrackerHidden(p tracker.Provider, itemType int, isRefreshNeeded bool) error {
	hr, ok := p.(tracker.HiddenReader)
	if !ok || !config.Get().TraktSyncHidden {
		return nil
	}

	// https://trakt.docs.apiary.io/#reference/users/hidden-items/get-hidden-items
	if itemType == ShowType {
		// calendar and recommendations for shows are handled on trakt side, progress_watched should be handled manually
		if _, err := hr.Hidden("progress_watched", isRefreshNeeded); err != nil {
			log.Warningf("TrackerSync: Got error from Hidden for Watched Progress: %s", err)
			return err
		}
	}
	// Movies: calendar and recommendations are handled on trakt side, and they are the only options.
	// Seasons: looks like website does not allow to hide seasons, so we also can ignore them.

	return nil
}

// RefreshTrackerLists ...
func RefreshTrackerLists(p tracker.Provider, isRefreshNeeded bool) error {
	lr, ok := p.(tracker.ListReader)
	if !ok {
		return nil
	}

	lists, err := lr.Lists()
	if err != nil {
		return err
	}
	for _, list := range lists {
		if list == nil {
			continue
		}

		if err := SyncMoviesList(list.ID, false, isRefreshNeeded); err != nil {
			continue
		}
		if err := SyncShowsList(list.ID, false, isRefreshNeeded); err != nil {
			continue
		}
	}

	return nil
}

func syncMoviesRemovedBack(movies []*tracker.Item) error {
	xbmcHost, err := xbmc.GetLocalXBMCHost()
	if xbmcHost == nil || err != nil {
		return errors.New("No Kodi instance found")
	}

	for _, m := range movies {
		if m == nil || m.IDs.TMDB == 0 {
			continue
		}

		if kodiMovie, err := uid.GetMovieByTMDB(m.IDs.TMDB); err == nil && kodiMovie != nil {
			movie, paths, err := RemoveMovie(m.IDs.TMDB, true)
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

func syncShowsRemovedBack(shows []*tracker.Item) error {
	xbmcHost, err := xbmc.GetLocalXBMCHost()
	if xbmcHost == nil || err != nil {
		return errors.New("No Kodi instance found")
	}

	for _, s := range shows {
		if s == nil || s.IDs.TMDB == 0 {
			continue
		}

		if kodiShow, err := uid.FindShowByTMDB(s.IDs.TMDB); err == nil && kodiShow != nil {
			show, paths, err := RemoveShow(strconv.Itoa(s.IDs.TMDB), true)
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

// ItemsToUIDLocked replaces a library container with items, holding the container lock.
func ItemsToUIDLocked(containerType uid.ContainerType, items []*tracker.Item) {
	container := uid.Get().GetContainer(containerType)
	if container == nil {
		return
	}

	container.Mu.Lock()
	defer container.Mu.Unlock()

	ItemsToUID(containerType, items)
}

// ItemsToUID replaces a library container with items.
func ItemsToUID(containerType uid.ContainerType, items []*tracker.Item) {
	defer perf.ScopeTimer()()

	l := uid.Get()
	container := l.GetContainer(containerType)
	if container == nil {
		return
	}

	container.Clear()
	for _, i := range items {
		if i == nil {
			continue
		}

		var media uid.MediaItemType = MovieType
		if i.Type != tracker.MovieType {
			media = ShowType
		}
		container.Add(media, toUniqueIDs(i.IDs))
	}

	l.Store()
	log.Debugf("Synced %d items into container type %d", len(items), containerType)
}

func toUniqueIDs(ids tracker.IDs) *uid.UniqueIDs {
	return &uid.UniqueIDs{
		Trakt: ids.Trakt,
		IMDB:  ids.IMDB,
		TMDB:  ids.TMDB,
		TVDB:  ids.TVDB,
	}
}

func watchedToItems(watched []*tracker.WatchedItem) []*tracker.Item {
	ret := make([]*tracker.Item, 0, len(watched))
	for _, w := range watched {
		if w != nil {
			ret = append(ret, &w.Item)
		}
	}
	return ret
}

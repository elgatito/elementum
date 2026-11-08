package apiv1

import (
	"github.com/elgatito/elementum/bittorrent"
	"github.com/elgatito/elementum/config"
	"github.com/elgatito/elementum/database"
	"github.com/elgatito/elementum/library"
	"github.com/elgatito/elementum/library/playcount"
	"github.com/elgatito/elementum/library/uid"
	"github.com/elgatito/elementum/tracker"
)

// Item state comes from what Elementum already keeps in memory or caches, the same sources the Kodi list
// builders read for their overlays and context menus: the library table (one query per list), the uid
// containers the tracker sync fills, the playcount map and the library's resume points. No tracker request is
// made per item; the show-level episode state reads the tracker's cached watched list once per call.

// syncKnown reports whether the tracker collection and watchlist containers are filled: a tracker is linked,
// Elementum's tracker sync is on, and it has filled a container (they are empty until the first sync, e.g. in
// a daemon that runs without Kodi). Otherwise those flags are unknown (null), not false. An account with an
// empty watchlist and collection also reads as unknown.
func syncKnown() bool {
	if tracker.Active() == nil || !config.Get().TraktSyncEnabled {
		return false
	}
	for _, c := range []uid.ContainerType{uid.WatchlistedMoviesContainer, uid.WatchlistedShowsContainer, uid.CollectedMoviesContainer, uid.CollectedShowsContainer, uid.WatchedMoviesContainer, uid.WatchedShowsContainer} {
		container := uid.Get().GetContainer(c)
		container.Mu.RLock()
		n := len(container.Items)
		container.Mu.RUnlock()
		if n > 0 {
			return true
		}
	}
	return false
}

// Note: the API's playcount.Set (watched actions) also changes the Kodi list overlays until the next refresh. That is intended.

func inContainer(c uid.ContainerType, mediaType uid.MediaItemType, id int) bool {
	container := uid.Get().GetContainer(c)
	container.Mu.RLock()
	defer container.Mu.RUnlock()
	return container.HasWithType(mediaType, uid.TMDBScraper, id)
}

func ptr[T any](v T) *T { return &v }

// annotate fills the state flags of list items.
func annotate(items []MediaItem) []MediaItem {
	var libMovies, libShows map[int]bool
	idSet := func(mediaType int) map[int]bool {
		set := map[int]bool{}
		for _, id := range library.ActiveIDs(mediaType) {
			set[id] = true
		}
		return set
	}
	sync := syncKnown()
	for n := range items {
		i := &items[n]
		if i.Type == MediaTypeMovie {
			if libMovies == nil {
				libMovies = idSet(library.MovieType)
			}
			i.InLibrary = ptr(libMovies[i.TmdbId])
			i.setState(sync, uid.MovieType, uid.WatchlistedMoviesContainer, uid.CollectedMoviesContainer, playcount.GetWatchedMovieByTMDB(i.TmdbId))
			if m, err := uid.GetMovieByTMDB(i.TmdbId); err == nil && m != nil {
				i.Progress = fraction(m.Resume)
			}
			if i.Progress == nil {
				i.Progress = fraction(storedResume(bittorrent.TMDBResumeKey(false, i.TmdbId, 0, 0)))
			}
		} else {
			if libShows == nil {
				libShows = idSet(library.ShowType)
			}
			i.InLibrary = ptr(libShows[i.TmdbId])
			i.setState(sync, uid.ShowType, uid.WatchlistedShowsContainer, uid.CollectedShowsContainer, playcount.GetWatchedShowByTMDB(i.TmdbId))
		}
	}
	return items
}

func (i *MediaItem) setState(sync bool, mediaType uid.MediaItemType, watchlist, collection uid.ContainerType, watched playcount.WatchedState) {
	if sync {
		i.InWatchlist = ptr(inContainer(watchlist, mediaType, i.TmdbId))
		i.InCollection = ptr(inContainer(collection, mediaType, i.TmdbId))
		i.Watched = ptr(bool(watched))
	} else if watched {
		i.Watched = ptr(true) // known from the Kodi library
	}
}

// fraction is a resume point as 0-1, nil when there is none.
func fraction(r *uid.Resume) *float32 {
	if r == nil || r.Position <= 0 || r.Total <= 0 {
		return nil
	}
	return ptr(float32(min(r.Position/r.Total, 1)))
}

// storedResume reads an API session's resume point (see bittorrent.TMDBResumeKey), nil when there is none.
func storedResume(key string) *uid.Resume {
	r := &uid.Resume{}
	database.GetCache().GetCachedObject(database.CommonBucket, key, r)
	if r.Position <= 0 {
		return nil
	}
	return r
}

// episodeWatched returns a lookup of a show's watched episodes from the playcount map, the same in-memory state the
// list flags use (Kodi library and tracker sync). No tracker call is made per request. known is false when
// neither source has data, so the flags should be null.
func episodeWatched(showID int) (watched func(season, episode int) bool, known bool) {
	return func(season, episode int) bool {
		return bool(playcount.GetWatchedEpisodeByTMDB(showID, season, episode))
	}, syncKnown() || uid.HasShows()
}

// setWatched fills a season summary's flags from the episode lookup. episode_count includes episodes that have
// not aired yet, so a running season stays unwatched until all of them are.
func (s *SeasonSummary) setWatched(showID int, watched func(season, episode int) bool) {
	n := 0
	for e := 1; e <= s.EpisodeCount; e++ {
		if watched(s.SeasonNumber, e) {
			n++
		}
	}
	s.WatchedEpisodes = &n
	s.Watched = ptr(bool(playcount.GetWatchedSeasonByTMDB(showID, s.SeasonNumber)) || (s.EpisodeCount > 0 && n >= s.EpisodeCount))
}

// setWatched fills the seasons' flags and the show's: watched means at least one regular season with episodes, and all
// of those seasons watched. Without regular seasons it keeps the list flag, or false.
func (d *ShowDetails) setWatched(showID int, watched func(season, episode int) bool) {
	regular, unwatched := 0, 0
	for n := range d.Seasons {
		sn := &d.Seasons[n]
		sn.setWatched(showID, watched)
		if sn.SeasonNumber > 0 && sn.EpisodeCount > 0 {
			regular++
			if !*sn.Watched {
				unwatched++
			}
		}
	}
	if regular > 0 {
		d.Watched = ptr(unwatched == 0 || (d.Watched != nil && *d.Watched)) // keep a show-level mark (POST /watched/show)
	} else if d.Watched == nil {
		d.Watched = ptr(false)
	}
}

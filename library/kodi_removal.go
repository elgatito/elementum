package library

import (
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/elgatito/elementum/config"
	"github.com/elgatito/elementum/library/uid"
	"github.com/elgatito/elementum/tracker"
	"github.com/elgatito/elementum/xbmc"
)

// Kodi sends the same OnRemove for "Remove from library" and for "Refresh",
// which deletes the item and adds it back, so the removal is confirmed only after this delay.
const kodiRemovalDelay = 5 * time.Minute

var errNoKodiResponse = errors.New("empty response from Kodi")

// PlanKodiRemoval removes a movie or show, deleted from Kodi library by the user, from Elementum library,
// unless Kodi adds it back in the meantime. Must be called before cached Kodi IDs are refreshed.
func PlanKodiRemoval(kodiID int, mediaType int) {
	if !config.Get().LibrarySyncKodiRemovals {
		return
	}

	uids := uid.GetUIDsFromKodi(kodiID)
	if uids == nil || uids.TMDB == 0 || !IsInLibrary(uids.TMDB, mediaType) {
		return
	}

	tmdbID := uids.TMDB
	log.Infof("Kodi removed %s with TMDB %d from library, checking again in %s", ItemTypes[mediaType], tmdbID, kodiRemovalDelay)
	time.AfterFunc(kodiRemovalDelay, func() { removeKodiRemoved(tmdbID, mediaType) })
}

func removeKodiRemoved(tmdbID int, mediaType int) {
	if !config.Get().LibrarySyncKodiRemovals || !IsInLibrary(tmdbID, mediaType) {
		return
	}

	xbmcHost, _ := xbmc.GetLocalXBMCHost()
	if xbmcHost == nil {
		return
	}
	if isBack, err := isInKodiLibrary(xbmcHost, tmdbID, mediaType); err != nil {
		log.Warningf("Not removing %s with TMDB %d, cannot read Kodi library: %s", ItemTypes[mediaType], tmdbID, err)
		return
	} else if isBack {
		log.Infof("Kodi added %s with TMDB %d back, keeping it", ItemTypes[mediaType], tmdbID)
		return
	}

	cfg := config.Get()
	if mediaType == MovieType {
		if _, _, err := RemoveMovie(tmdbID, false); err != nil {
			log.Warningf("Could not remove movie %d deleted from Kodi library: %s", tmdbID, err)
		}
		if cfg.TraktSyncRemovedMovies {
			tracker.RemoveFrom(tracker.Location(cfg.TraktSyncRemovedMoviesLocation), cfg.TraktSyncRemovedMoviesList, &tracker.Item{Type: tracker.MovieType, IDs: tracker.IDs{TMDB: tmdbID}})
		}
	} else {
		if _, _, err := RemoveShow(strconv.Itoa(tmdbID), false); err != nil {
			log.Warningf("Could not remove show %d deleted from Kodi library: %s", tmdbID, err)
		}
		if cfg.TraktSyncRemovedShows {
			tracker.RemoveFrom(tracker.Location(cfg.TraktSyncRemovedShowsLocation), cfg.TraktSyncRemovedShowsList, &tracker.Item{Type: tracker.ShowType, IDs: tracker.IDs{TMDB: tmdbID}})
		}
	}
}

// isInKodiLibrary asks Kodi directly, as cached library state can be outdated or not reloaded at all.
func isInKodiLibrary(xbmcHost *xbmc.XBMCHost, tmdbID int, mediaType int) (bool, error) {
	matches := func(ids xbmc.UniqueIDs, imdbNumber, file string, year int) bool {
		if ids.IMDB == "" && strings.HasPrefix(imdbNumber, "tt") {
			ids.IMDB = imdbNumber
		}
		u := &uid.UniqueIDs{}
		parseUniqueID(uid.MediaItemType(mediaType), u, &ids, file, year)
		return u.TMDB == tmdbID
	}

	if mediaType == MovieType {
		movies, err := xbmcHost.VideoLibraryGetMovies()
		if err != nil {
			return false, err
		} else if movies == nil {
			return false, errNoKodiResponse
		}
		for _, m := range movies.Movies {
			if matches(m.UniqueIDs, m.IMDBNumber, m.File, m.Year) {
				return true, nil
			}
		}
		return false, nil
	}

	shows, err := xbmcHost.VideoLibraryGetShows()
	if err != nil {
		return false, err
	} else if shows == nil {
		return false, errNoKodiResponse
	}
	for _, s := range shows.Shows {
		if matches(s.UniqueIDs, s.IMDBNumber, "", s.Year) {
			return true, nil
		}
	}
	return false, nil
}

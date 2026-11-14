package jsonapi

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/elgatito/elementum/config"
	"github.com/elgatito/elementum/library"
	"github.com/elgatito/elementum/library/uid"
	"github.com/elgatito/elementum/tmdb"
	"github.com/elgatito/elementum/tracker"
	"github.com/elgatito/elementum/xbmc"
)

// The actions do what the Kodi context-menu routes do (api/library.go, ToggleWatched in api/trakt.go) without
// their dialogs, and without asking Kodi to scan: an Elementum-only library does not need that.

var (
	alreadyInLibrary = failed(http.StatusConflict, ErrorCodeAlreadyInLibrary, "already in the Kodi library")
	libraryFailed    = failed(http.StatusInternalServerError, ErrorCodeInternalError, "library update failed")
)

// libraryArgs checks the add/remove params and returns the library media type.
func libraryArgs(media MediaParam, id TmdbIdParam) (int, *failure) {
	if f := checkParams(nil, nil, media); f != nil {
		return 0, f
	}
	if id < 1 {
		return 0, bad("tmdb_id must be 1 or more")
	}
	if media == MediaKindMovies {
		return library.MovieType, nil
	}
	return library.ShowType, nil
}

func (Server) AddToLibrary(c *gin.Context, media MediaParam, tmdbID TmdbIdParam) {
	mediaType, f := libraryArgs(media, tmdbID)
	if f != nil {
		fail(c, f)
		return
	}
	id, lang := strconv.Itoa(tmdbID), config.Get().Language
	duplicate := uid.IsDuplicateShow
	known := func() bool { s := tmdb.GetShow(tmdbID, lang); return s != nil && s.ID != 0 }
	add := func(force bool) error { _, err := library.AddShow(id, force); return err }
	if mediaType == library.MovieType {
		duplicate = uid.IsDuplicateMovie
		known = func() bool { m := tmdb.GetMovie(tmdbID, lang); return m != nil && m.ID != 0 }
		add = func(force bool) error { _, err := library.AddMovie(id, force); return err }
	}

	switch {
	case library.IsInLibrary(tmdbID, mediaType):
	case duplicate(id):
		f = alreadyInLibrary
	case !known():
		f = tmdbNotFound
	default:
		err := add(false)
		if errors.Is(err, library.ErrVideoRemoved) { // removed before: add it again, as the Kodi dialog's yes does
			err = add(true)
		}
		if err != nil {
			log.Warningf("Adding %s %s to the library failed: %v", media, id, err)
			f = libraryFailed
		} else {
			go library.SyncTracker(mediaType, tmdbID, true)
		}
	}
	if f != nil {
		fail(c, f)
		return
	}
	c.Status(http.StatusNoContent)
}

func (Server) RemoveFromLibrary(c *gin.Context, media MediaParam, tmdbID TmdbIdParam) {
	mediaType, f := libraryArgs(media, tmdbID)
	if f != nil {
		fail(c, f)
		return
	}
	if library.IsInLibrary(tmdbID, mediaType) {
		var err error
		if mediaType == library.MovieType {
			_, _, err = library.RemoveMovie(tmdbID, false)
		} else {
			_, _, err = library.RemoveShow(strconv.Itoa(tmdbID), false)
		}
		if err != nil {
			// The database row is dropped even when the files are gone already, so the item is out of the library.
			log.Warningf("Removing %s %d from the library: %v", media, tmdbID, err)
		}
		go library.SyncTracker(mediaType, tmdbID, false)
	}
	c.Status(http.StatusNoContent)
}

func (Server) MarkWatched(c *gin.Context, kind WatchedKindParam) { markWatched(c, kind, true) }

func (Server) MarkUnwatched(c *gin.Context, kind WatchedKindParam) { markWatched(c, kind, false) }

func markWatched(c *gin.Context, kind WatchedKind, watched bool) {
	b := body[WatchedRequest](c)
	if b == nil {
		return
	}
	if f := setWatched(kind, b, watched); f != nil {
		fail(c, f)
		return
	}
	c.Status(http.StatusNoContent)
}

// setWatched marks the item in the Kodi library (when it is there and Kodi is running), on the linked tracker
// and in the in-memory watched state. With no tracker and no Kodi library entry there is nowhere to keep
// it, which is 409 tracker_not_authorized.
func setWatched(kind WatchedKind, b *WatchedRequest, watched bool) *failure {
	if f := checkParams(nil, nil, kind); f != nil {
		return f
	}
	switch {
	case b.TmdbId < 1:
		return bad("tmdb_id must be 1 or more")
	case (kind == WatchedKindSeason || kind == WatchedKindEpisode) && (b.Season == nil || *b.Season < 0):
		return bad("season (0 or more) is required for a season or an episode")
	case kind == WatchedKindEpisode && (b.Episode == nil || *b.Episode < 1):
		return bad("episode (1 or more) is required for an episode")
	}

	item := tracker.Item{IDs: tracker.IDs{TMDB: b.TmdbId}}
	count := 0 // playcount for Kodi
	if watched {
		count = 1
	}
	host, _ := xbmc.GetLocalXBMCHost()
	switch kind {
	case WatchedKindMovie:
		item.Type = tracker.MovieType
	case WatchedKindShow:
		item.Type = tracker.ShowType
	case WatchedKindSeason:
		item.Type, item.Season = tracker.SeasonType, *b.Season
	case WatchedKindEpisode:
		item.Type, item.Season, item.Episode = tracker.EpisodeType, *b.Season, *b.Episode
	}
	kodi := library.KodiWatchedSetter(item)

	// Decide first: a failure changes nothing, because Kodi's list overlays read the same playcount map.
	p := tracker.Active()
	if p == nil && (kodi == nil || host == nil) {
		return notLinked
	}
	if err := library.SetWatched(item, watched); err != nil {
		if errors.Is(err, library.ErrWatchedMetadata) {
			return tmdbFailed
		}
		if p != nil {
			return trackerFailure(p, err)
		}
		return trackerDown
	}
	if kodi != nil && host != nil {
		kodi(host, count)
	}
	return nil
}

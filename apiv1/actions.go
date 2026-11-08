package apiv1

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/elgatito/elementum/config"
	"github.com/elgatito/elementum/library"
	"github.com/elgatito/elementum/library/playcount"
	"github.com/elgatito/elementum/library/uid"
	"github.com/elgatito/elementum/tmdb"
	"github.com/elgatito/elementum/tracker"
	"github.com/elgatito/elementum/xbmc"
)

// The actions do what the Kodi context-menu routes do (api/library.go, ToggleWatched in api/trakt.go) without
// their dialogs, and without asking Kodi to scan: an Elementum-only library does not need that.

var alreadyInLibrary = newError(ErrorCodeAlreadyInLibrary, "already in the Kodi library")

var libraryFailed = failure{http.StatusInternalServerError, newError(ErrorCodeInternalError, "library update failed")}

// libraryArgs checks the add/remove params.
func libraryArgs(media MediaParam, id TmdbIdParam) *Error {
	if e := checkParams(nil, nil, media); e != nil {
		return e
	}
	if id < 1 {
		e := newError(ErrorCodeBadRequest, "tmdb_id must be 1 or more")
		return &e
	}
	return nil
}

func (Server) AddToLibrary(_ context.Context, req AddToLibraryRequestObject) (AddToLibraryResponseObject, error) {
	fail := func(f failure) (AddToLibraryResponseObject, error) {
		return AddToLibrarydefaultJSONResponse{Body: f.body, StatusCode: f.status}, nil
	}
	if e := libraryArgs(req.Media, req.TmdbId); e != nil {
		return AddToLibrary400JSONResponse{badRequest(e)}, nil
	}
	id, lang := strconv.Itoa(req.TmdbId), config.Get().Language
	if req.Media == MediaKindMovies {
		if library.IsInLibrary(req.TmdbId, library.MovieType) {
			return AddToLibrary204Response{}, nil
		} else if uid.IsDuplicateMovie(id) {
			return AddToLibrary409JSONResponse{AlreadyInLibraryJSONResponse(alreadyInLibrary)}, nil
		}
		if m := tmdb.GetMovie(req.TmdbId, lang); m == nil || m.ID == 0 {
			return AddToLibrary404JSONResponse{NotFoundJSONResponse(tmdbNotFound)}, nil
		}
		_, err := library.AddMovie(id, false)
		if errors.Is(err, library.ErrVideoRemoved) { // removed before: add it again, as the Kodi dialog's yes does
			_, err = library.AddMovie(id, true)
		}
		if err != nil {
			log.Warningf("Adding movie %s to the library failed: %v", id, err)
			return fail(libraryFailed)
		}
		if config.Get().TraktSyncAddedMovies {
			go tracker.AddTo(tracker.Location(config.Get().TraktSyncAddedMoviesLocation), config.Get().TraktSyncAddedMoviesList, &tracker.Item{Type: tracker.MovieType, IDs: tracker.IDs{TMDB: req.TmdbId}})
		}
		return AddToLibrary204Response{}, nil
	}
	if library.IsInLibrary(req.TmdbId, library.ShowType) {
		return AddToLibrary204Response{}, nil
	} else if uid.IsDuplicateShow(id) {
		return AddToLibrary409JSONResponse{AlreadyInLibraryJSONResponse(alreadyInLibrary)}, nil
	}
	if s := tmdb.GetShow(req.TmdbId, lang); s == nil || s.ID == 0 {
		return AddToLibrary404JSONResponse{NotFoundJSONResponse(tmdbNotFound)}, nil
	}
	_, err := library.AddShow(id, false)
	if errors.Is(err, library.ErrVideoRemoved) {
		_, err = library.AddShow(id, true)
	}
	if err != nil {
		log.Warningf("Adding show %s to the library failed: %v", id, err)
		return fail(libraryFailed)
	}
	if config.Get().TraktSyncAddedShows {
		go tracker.AddTo(tracker.Location(config.Get().TraktSyncAddedShowsLocation), config.Get().TraktSyncAddedShowsList, &tracker.Item{Type: tracker.ShowType, IDs: tracker.IDs{TMDB: req.TmdbId}})
	}
	return AddToLibrary204Response{}, nil
}

func (Server) RemoveFromLibrary(_ context.Context, req RemoveFromLibraryRequestObject) (RemoveFromLibraryResponseObject, error) {
	if e := libraryArgs(req.Media, req.TmdbId); e != nil {
		return RemoveFromLibrary400JSONResponse{badRequest(e)}, nil
	}
	id := strconv.Itoa(req.TmdbId)
	var err error
	if req.Media == MediaKindMovies {
		if !library.IsInLibrary(req.TmdbId, library.MovieType) {
			return RemoveFromLibrary204Response{}, nil
		}
		_, _, err = library.RemoveMovie(req.TmdbId, false)
		if config.Get().TraktSyncRemovedMovies {
			go tracker.RemoveFrom(tracker.Location(config.Get().TraktSyncRemovedMoviesLocation), config.Get().TraktSyncRemovedMoviesList, &tracker.Item{Type: tracker.MovieType, IDs: tracker.IDs{TMDB: req.TmdbId}})
		}
	} else {
		if !library.IsInLibrary(req.TmdbId, library.ShowType) {
			return RemoveFromLibrary204Response{}, nil
		}
		_, _, err = library.RemoveShow(id, false)
		if config.Get().TraktSyncRemovedShows {
			go tracker.RemoveFrom(tracker.Location(config.Get().TraktSyncRemovedShowsLocation), config.Get().TraktSyncRemovedShowsList, &tracker.Item{Type: tracker.ShowType, IDs: tracker.IDs{TMDB: req.TmdbId}})
		}
	}
	if err != nil {
		// The database row is dropped even when the files are gone already, so the item is out of the library.
		log.Warningf("Removing %s %s from the library: %v", req.Media, id, err)
	}
	return RemoveFromLibrary204Response{}, nil
}

func (Server) MarkWatched(_ context.Context, req MarkWatchedRequestObject) (MarkWatchedResponseObject, error) {
	if f := setWatched(req.Kind, req.Body, true); f != nil {
		return markWatchedFailure(f), nil
	}
	return MarkWatched204Response{}, nil
}

func (Server) MarkUnwatched(_ context.Context, req MarkUnwatchedRequestObject) (MarkUnwatchedResponseObject, error) {
	if f := setWatched(req.Kind, req.Body, false); f != nil {
		return markUnwatchedFailure(f), nil
	}
	return MarkUnwatched204Response{}, nil
}

func markWatchedFailure(f *failure) MarkWatchedResponseObject {
	switch f.status {
	case http.StatusBadRequest:
		return MarkWatched400JSONResponse{badRequest(&f.body)}
	case http.StatusConflict:
		return MarkWatched409JSONResponse{TrackerUnavailableJSONResponse(f.body)}
	}
	return MarkWatcheddefaultJSONResponse{Body: f.body, StatusCode: f.status}
}

func markUnwatchedFailure(f *failure) MarkUnwatchedResponseObject {
	switch f.status {
	case http.StatusBadRequest:
		return MarkUnwatched400JSONResponse{badRequest(&f.body)}
	case http.StatusConflict:
		return MarkUnwatched409JSONResponse{TrackerUnavailableJSONResponse(f.body)}
	}
	return MarkUnwatcheddefaultJSONResponse{Body: f.body, StatusCode: f.status}
}

// setWatched marks the item in the Kodi library (when it is there and Kodi is running), on the linked tracker
// and in the in-memory watched state. With no tracker and no Kodi library entry there is nowhere to keep
// it, which is 409 tracker_not_authorized.
func setWatched(kind WatchedKind, b *WatchedRequest, watched bool) *failure {
	if e := checkParams(nil, nil, kind); e != nil {
		return badParams(e)
	}
	bad := func(msg string) *failure {
		f := failure{http.StatusBadRequest, newError(ErrorCodeBadRequest, msg)}
		return &f
	}
	switch {
	case b == nil || b.TmdbId < 1:
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
	var show *uid.Show
	if kind != WatchedKindMovie {
		show, _ = uid.GetShowByTMDB(b.TmdbId)
	}
	// What is applied once the tracker call has succeeded: the Kodi library and the shared in-memory state.
	var kodi func(*xbmc.XBMCHost)
	var mem []int
	memType := playcount.MovieType
	switch kind {
	case WatchedKindMovie:
		item.Type, mem = tracker.MovieType, []int{b.TmdbId}
		if m, err := uid.GetMovieByTMDB(b.TmdbId); err == nil && m != nil {
			kodi = func(h *xbmc.XBMCHost) { h.SetMovieWatched(m.ID, count, 0, 0) }
		}
	case WatchedKindShow:
		item.Type, mem, memType = tracker.ShowType, []int{b.TmdbId}, playcount.ShowType
		if show != nil {
			kodi = func(h *xbmc.XBMCHost) { h.SetShowWatched(show.ID, count) }
		}
	case WatchedKindSeason:
		item.Type, item.Season, mem, memType = tracker.SeasonType, *b.Season, []int{b.TmdbId, *b.Season}, playcount.SeasonType
		if se := seasonOf(show, *b.Season); se != nil {
			kodi = func(h *xbmc.XBMCHost) { h.SetSeasonWatched(se.ID, count) }
		}
	case WatchedKindEpisode:
		item.Type, item.Season, item.Episode = tracker.EpisodeType, *b.Season, *b.Episode
		mem, memType = []int{b.TmdbId, *b.Season, *b.Episode}, playcount.EpisodeType
		if show != nil {
			if e := show.GetEpisode(*b.Season, *b.Episode); e != nil {
				kodi = func(h *xbmc.XBMCHost) { h.SetEpisodeWatched(e.ID, count, 0, 0) }
			}
		}
	}

	// Decide first: a failure changes nothing, because Kodi's list overlays read the same playcount map.
	p := tracker.Active()
	if p == nil && (kodi == nil || host == nil) {
		return &notLinked
	}
	if p != nil {
		if err := tracker.SetWatched([]*tracker.WatchedItem{{Item: item}}, watched); err != nil {
			return trackerFailure(p, err)
		}
	}
	if kodi != nil && host != nil {
		kodi(host)
	}
	// This also changes the Kodi list overlays until the next library refresh rebuilds the map (intended).
	playcount.Set(memType, watched, mem...)
	return nil
}

func seasonOf(show *uid.Show, season int) *uid.Season {
	if show == nil {
		return nil
	}
	return show.GetSeason(season)
}

package jsonapi

import (
	"cmp"
	"context"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/elgatito/elementum/bittorrent"
	"github.com/elgatito/elementum/config"
	"github.com/elgatito/elementum/library/uid"
	"github.com/elgatito/elementum/tmdb"
)

// The TMDB fetchers take no context and swallow their errors (a failed fetch looks like nil or a
// negative total), so these handlers can neither cancel them nor tell "not found" from "TMDB down".

var (
	langRe        = regexp.MustCompile(`^([a-zA-Z]{2})(?:[-_]([a-zA-Z]{2}))?$`)
	tmdbFailed    = newError(ErrorCodeInternalError, "TMDB request failed")
	tmdbNotFound  = newError(ErrorCodeNotFound, "not found")
	genreRequired = newError(ErrorCodeBadRequest, "genre is required for by_genre")
	badGenre      = newError(ErrorCodeBadRequest, "genre must be 1 or more")
	badYear       = newError(ErrorCodeBadRequest, "year must be between 1870 and 2100")
	badDetailID   = newError(ErrorCodeBadRequest, "tmdb_id must be 1 or more")
	badSeason     = newError(ErrorCodeBadRequest, "season must be 0 or more")
)

// langArg validates lang and returns TMDB's "de" or "de-DE", the configured language when unset.
func langArg(lang *string) (string, *Error) {
	if lang == nil {
		return config.Get().Language, nil
	}
	m := langRe.FindStringSubmatch(*lang)
	if m == nil {
		err := newError(ErrorCodeBadRequest, "lang must look like de or de-DE")
		return "", &err
	}
	lng := strings.ToLower(m[1])
	if m[2] != "" {
		lng += "-" + strings.ToUpper(m[2])
	}
	return lng, nil
}

// args validates the common query params and fills their defaults.
func args(page, limit *int, lang *string, enums ...interface{ Valid() bool }) (pg, lim int, lng string, e *Error) {
	if e = checkParams(page, limit, enums...); e != nil {
		return
	}
	if lng, e = langArg(lang); e != nil {
		return 0, 0, "", e
	}
	pg, lim = 1, min(max(cmp.Or(config.Get().ResultsPerPage, tmdb.TMDBResultsPerPage), 1), 100)
	if page != nil {
		pg = *page
	}
	if limit != nil {
		lim = *limit
	}
	return
}

// genreArgs checks the by_genre filters against the spec's bounds.
func genreArgs(genre, year *int) *Error {
	switch {
	case genre == nil:
		return &genreRequired
	case *genre < 1:
		return &badGenre
	case year != nil && (*year < 1870 || *year > 2100):
		return &badYear
	}
	return nil
}

func badRequest(e *Error) BadRequestJSONResponse { return BadRequestJSONResponse(*e) }

func (Server) ListMovies(_ context.Context, req ListMoviesRequestObject) (ListMoviesResponseObject, error) {
	p := req.Params
	page, limit, lang, e := args(p.Page, p.Limit, p.Lang, req.List)
	if e != nil {
		return ListMovies400JSONResponse{badRequest(e)}, nil
	}
	if req.List == MovieListNameByGenre {
		if e = genreArgs(p.Genre, p.Year); e != nil {
			return ListMovies400JSONResponse{badRequest(e)}, nil
		}
	}
	if pastLastPage(page, limit) {
		return ListMovies200JSONResponse{MediaItemPageJSONResponse(emptyPage(page, limit))}, nil
	}
	var movies tmdb.Movies
	var total int
	switch req.List {
	case MovieListNamePopular:
		movies, total = tmdb.PopularMovies(tmdb.DiscoverFilters{}, lang, page, limit, tmdb.Lite)
	case MovieListNameTrending:
		movies, total = tmdb.TrendingMovies(lang, page, limit, tmdb.Lite)
	case MovieListNameTopRated:
		movies, total = tmdb.TopRatedMovies("", lang, page, limit, tmdb.Lite)
	case MovieListNameNowPlaying:
		movies, total = tmdb.RecentMovies(tmdb.DiscoverFilters{}, lang, page, limit, tmdb.Lite)
	case MovieListNameUpcoming:
		movies, total = tmdb.UpcomingMovies(lang, page, limit, tmdb.Lite)
	case MovieListNameByGenre:
		f := tmdb.DiscoverFilters{Genre: strconv.Itoa(*p.Genre)}
		if p.Year != nil {
			f.Year = strconv.Itoa(*p.Year)
		}
		movies, total = tmdb.PopularMovies(f, lang, page, limit, tmdb.Lite)
	}
	if total < 0 {
		return ListMoviesdefaultJSONResponse{Body: tmdbFailed, StatusCode: http.StatusBadGateway}, nil
	}
	return ListMovies200JSONResponse{MediaItemPageJSONResponse(mediaPage(annotate(movieItems(movies)), total, page, limit))}, nil
}

func (Server) ListShows(_ context.Context, req ListShowsRequestObject) (ListShowsResponseObject, error) {
	p := req.Params
	page, limit, lang, e := args(p.Page, p.Limit, p.Lang, req.List)
	if e != nil {
		return ListShows400JSONResponse{badRequest(e)}, nil
	}
	if req.List == ShowListNameByGenre {
		if e = genreArgs(p.Genre, p.Year); e != nil {
			return ListShows400JSONResponse{badRequest(e)}, nil
		}
	}
	if pastLastPage(page, limit) {
		return ListShows200JSONResponse{MediaItemPageJSONResponse(emptyPage(page, limit))}, nil
	}
	var shows tmdb.Shows
	var total int
	switch req.List {
	case ShowListNamePopular:
		shows, total = tmdb.PopularShows(tmdb.DiscoverFilters{}, lang, page, limit, tmdb.Lite)
	case ShowListNameTrending:
		shows, total = tmdb.TrendingShows(lang, page, limit, tmdb.Lite)
	case ShowListNameTopRated:
		shows, total = tmdb.TopRatedShows("", lang, page, limit, tmdb.Lite)
	case ShowListNameOnTheAir:
		shows, total = tmdb.RecentEpisodes(tmdb.DiscoverFilters{}, lang, page, limit, tmdb.Lite)
	case ShowListNameByGenre:
		f := tmdb.DiscoverFilters{Genre: strconv.Itoa(*p.Genre)}
		if p.Year != nil {
			f.Year = strconv.Itoa(*p.Year)
		}
		shows, total = tmdb.PopularShows(f, lang, page, limit, tmdb.Lite)
	}
	if total < 0 {
		return ListShowsdefaultJSONResponse{Body: tmdbFailed, StatusCode: http.StatusBadGateway}, nil
	}
	return ListShows200JSONResponse{MediaItemPageJSONResponse(mediaPage(annotate(showItems(shows)), total, page, limit))}, nil
}

func (Server) ListMovieGenres(_ context.Context, req ListMovieGenresRequestObject) (ListMovieGenresResponseObject, error) {
	lang, e := langArg(req.Params.Lang)
	if e != nil {
		return ListMovieGenresdefaultJSONResponse{Body: *e, StatusCode: http.StatusBadRequest}, nil
	}
	gs := tmdb.GetMovieGenres(lang)
	if gs == nil {
		return ListMovieGenresdefaultJSONResponse{Body: tmdbFailed, StatusCode: http.StatusBadGateway}, nil
	}
	return ListMovieGenres200JSONResponse(genres(gs)), nil
}

func (Server) ListShowGenres(_ context.Context, req ListShowGenresRequestObject) (ListShowGenresResponseObject, error) {
	lang, e := langArg(req.Params.Lang)
	if e != nil {
		return ListShowGenresdefaultJSONResponse{Body: *e, StatusCode: http.StatusBadRequest}, nil
	}
	gs := tmdb.GetTVGenres(lang)
	if gs == nil {
		return ListShowGenresdefaultJSONResponse{Body: tmdbFailed, StatusCode: http.StatusBadGateway}, nil
	}
	return ListShowGenres200JSONResponse(genres(gs)), nil
}

func (Server) GetMovie(_ context.Context, req GetMovieRequestObject) (GetMovieResponseObject, error) {
	if req.TmdbId < 1 {
		return GetMoviedefaultJSONResponse{Body: badDetailID, StatusCode: http.StatusBadRequest}, nil
	}
	lang, e := langArg(req.Params.Lang)
	if e != nil {
		return GetMoviedefaultJSONResponse{Body: *e, StatusCode: http.StatusBadRequest}, nil
	}
	m := tmdb.GetMovie(req.TmdbId, lang)
	if m == nil || m.ID == 0 {
		return GetMovie404JSONResponse{NotFoundJSONResponse(tmdbNotFound)}, nil
	}
	d := movieDetails(m)
	st := annotate([]MediaItem{{Type: MediaTypeMovie, TmdbId: m.ID}})[0]
	d.InLibrary, d.InCollection, d.InWatchlist, d.Watched, d.Progress = st.InLibrary, st.InCollection, st.InWatchlist, st.Watched, st.Progress
	return GetMovie200JSONResponse(d), nil
}

func (Server) GetShow(_ context.Context, req GetShowRequestObject) (GetShowResponseObject, error) {
	if req.TmdbId < 1 {
		return GetShowdefaultJSONResponse{Body: badDetailID, StatusCode: http.StatusBadRequest}, nil
	}
	lang, e := langArg(req.Params.Lang)
	if e != nil {
		return GetShowdefaultJSONResponse{Body: *e, StatusCode: http.StatusBadRequest}, nil
	}
	s := tmdb.GetShow(req.TmdbId, lang)
	if s == nil || s.ID == 0 {
		return GetShow404JSONResponse{NotFoundJSONResponse(tmdbNotFound)}, nil
	}
	d := showDetails(s)
	st := annotate([]MediaItem{{Type: MediaTypeShow, TmdbId: s.ID}})[0]
	d.InLibrary, d.InCollection, d.InWatchlist, d.Watched = st.InLibrary, st.InCollection, st.InWatchlist, st.Watched
	if watched, known := episodeWatched(s.ID); known {
		d.setWatched(s.ID, watched)
	}
	return GetShow200JSONResponse(d), nil
}

func (Server) GetSeason(_ context.Context, req GetSeasonRequestObject) (GetSeasonResponseObject, error) {
	if req.TmdbId < 1 {
		return GetSeasondefaultJSONResponse{Body: badDetailID, StatusCode: http.StatusBadRequest}, nil
	}
	if req.Season < 0 {
		return GetSeasondefaultJSONResponse{Body: badSeason, StatusCode: http.StatusBadRequest}, nil
	}
	lang, e := langArg(req.Params.Lang)
	if e != nil {
		return GetSeasondefaultJSONResponse{Body: *e, StatusCode: http.StatusBadRequest}, nil
	}
	// seasonsCount is unused by GetSeason, so no GetShow is needed for it.
	s := tmdb.GetSeason(req.TmdbId, req.Season, lang, 0, true)
	if s == nil || s.ID == 0 {
		return GetSeason404JSONResponse{NotFoundJSONResponse(tmdbNotFound)}, nil
	}
	d := season(s)
	watched, known := episodeWatched(req.TmdbId)
	show, err := uid.GetShowByTMDB(req.TmdbId) // Kodi library resume points
	for n := range d.Episodes {
		e := &d.Episodes[n]
		if known {
			e.Watched = ptr(watched(e.SeasonNumber, e.EpisodeNumber))
		}
		if err == nil && show != nil {
			if le := show.GetEpisode(e.SeasonNumber, e.EpisodeNumber); le != nil && le.Resume != nil && le.Resume.Position > 0 {
				e.ResumeS = ptr(float32(le.Resume.Position))
			}
		}
		if r := storedResume(bittorrent.TMDBResumeKey(true, req.TmdbId, e.SeasonNumber, e.EpisodeNumber)); e.ResumeS == nil && r != nil {
			e.ResumeS = ptr(float32(r.Position))
		}
	}
	return GetSeason200JSONResponse(d), nil
}

// Search pages movie or show results by limit, like the lists.
func (Server) Search(_ context.Context, req SearchRequestObject) (SearchResponseObject, error) {
	p := req.Params
	page, limit, lang, e := args(p.Page, p.Limit, p.Lang, p.Type)
	if e == nil && strings.TrimSpace(p.Query) == "" {
		err := newError(ErrorCodeBadRequest, "query is required")
		e = &err
	}
	if e != nil {
		return Search400JSONResponse{badRequest(e)}, nil
	}
	if pastLastPage(page, limit) {
		return Search200JSONResponse{MediaItemPageJSONResponse(emptyPage(page, limit))}, nil
	}
	var items []MediaItem
	var total int
	if p.Type == SearchTypeMovie {
		var movies tmdb.Movies
		movies, total = tmdb.SearchMoviesPerPage(p.Query, lang, page, limit, tmdb.Lite)
		items = annotate(movieItems(movies))
	} else {
		var shows tmdb.Shows
		shows, total = tmdb.SearchShowsPerPage(p.Query, lang, page, limit, tmdb.Lite)
		items = annotate(showItems(shows))
	}
	if total < 0 {
		return SearchdefaultJSONResponse{Body: tmdbFailed, StatusCode: http.StatusBadGateway}, nil
	}
	return Search200JSONResponse{MediaItemPageJSONResponse(mediaPage(items, total, page, limit))}, nil
}

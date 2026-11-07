package apiv1

import (
	"cmp"
	"context"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/jmcvetta/napping"

	"github.com/elgatito/elementum/config"
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
)

// args validates the common query params and fills their defaults. lang becomes TMDB's "de" or "de-DE".
func args(page, limit *int, lang *string, enums ...interface{ Valid() bool }) (pg, lim int, lng string, e *Error) {
	if e = checkParams(page, limit, enums...); e != nil {
		return
	}
	pg, lim, lng = 1, min(max(cmp.Or(config.Get().ResultsPerPage, tmdb.TMDBResultsPerPage), 1), 100), config.Get().Language
	if page != nil {
		pg = *page
	}
	if limit != nil {
		lim = *limit
	}
	if lang != nil {
		m := langRe.FindStringSubmatch(*lang)
		if m == nil {
			err := newError(ErrorCodeBadRequest, "lang must look like de or de-DE")
			return 0, 0, "", &err
		}
		lng = strings.ToLower(m[1])
		if m[2] != "" {
			lng += "-" + strings.ToUpper(m[2])
		}
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

func today() string { return time.Now().UTC().Format(time.DateOnly) }

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
		movies, total = tmdb.PopularMovies(tmdb.DiscoverFilters{}, lang, page, limit)
	case MovieListNameTrending:
		movies, total = tmdb.TrendingMovies(lang, page, limit)
	case MovieListNameTopRated:
		movies, total = tmdb.TopRatedMovies("", lang, page, limit)
	case MovieListNameNowPlaying:
		movies, total = tmdb.RecentMovies(tmdb.DiscoverFilters{}, lang, page, limit)
	case MovieListNameUpcoming:
		movies, total = tmdb.UpcomingMovies(lang, page, limit)
	case MovieListNameByGenre:
		// Same filters as PopularMovies with a genre, plus the year.
		params := napping.Params{"sort_by": "popularity.desc", "primary_release_date.lte": today(), "with_genres": strconv.Itoa(*p.Genre)}
		if p.Year != nil {
			params["primary_release_year"] = strconv.Itoa(*p.Year)
		}
		movies, total = tmdb.DiscoverMovies(params, lang, page, limit)
	}
	if total < 0 {
		return ListMoviesdefaultJSONResponse{Body: tmdbFailed, StatusCode: http.StatusBadGateway}, nil
	}
	return ListMovies200JSONResponse{MediaItemPageJSONResponse(mediaPage(movieItems(movies), total, page, limit))}, nil
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
		shows, total = tmdb.PopularShows(tmdb.DiscoverFilters{}, lang, page, limit)
	case ShowListNameTrending:
		shows, total = tmdb.TrendingShows(lang, page, limit)
	case ShowListNameTopRated:
		shows, total = tmdb.TopRatedShows("", lang, page, limit)
	case ShowListNameOnTheAir:
		shows, total = tmdb.RecentEpisodes(tmdb.DiscoverFilters{}, lang, page, limit)
	case ShowListNameByGenre:
		// Same filters as PopularShows with a genre, plus the year.
		params := napping.Params{"sort_by": "popularity.desc", "first_air_date.lte": today(), "with_genres": strconv.Itoa(*p.Genre)}
		if p.Year != nil {
			params["first_air_date_year"] = strconv.Itoa(*p.Year)
		}
		shows, total = tmdb.DiscoverShows(params, lang, page, limit)
	}
	if total < 0 {
		return ListShowsdefaultJSONResponse{Body: tmdbFailed, StatusCode: http.StatusBadGateway}, nil
	}
	return ListShows200JSONResponse{MediaItemPageJSONResponse(mediaPage(showItems(shows), total, page, limit))}, nil
}

func (Server) ListMovieGenres(_ context.Context, req ListMovieGenresRequestObject) (ListMovieGenresResponseObject, error) {
	_, _, lang, e := args(nil, nil, req.Params.Lang)
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
	_, _, lang, e := args(nil, nil, req.Params.Lang)
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
	_, _, lang, e := args(nil, nil, req.Params.Lang)
	if e != nil {
		return GetMoviedefaultJSONResponse{Body: *e, StatusCode: http.StatusBadRequest}, nil
	}
	m := tmdb.GetMovie(req.TmdbId, lang)
	if m == nil || m.ID == 0 {
		return GetMovie404JSONResponse{NotFoundJSONResponse(tmdbNotFound)}, nil
	}
	return GetMovie200JSONResponse(movieDetails(m)), nil
}

func (Server) GetShow(_ context.Context, req GetShowRequestObject) (GetShowResponseObject, error) {
	_, _, lang, e := args(nil, nil, req.Params.Lang)
	if e != nil {
		return GetShowdefaultJSONResponse{Body: *e, StatusCode: http.StatusBadRequest}, nil
	}
	s := tmdb.GetShow(req.TmdbId, lang)
	if s == nil || s.ID == 0 {
		return GetShow404JSONResponse{NotFoundJSONResponse(tmdbNotFound)}, nil
	}
	return GetShow200JSONResponse(showDetails(s)), nil
}

func (Server) GetSeason(_ context.Context, req GetSeasonRequestObject) (GetSeasonResponseObject, error) {
	_, _, lang, e := args(nil, nil, req.Params.Lang)
	if e != nil {
		return GetSeasondefaultJSONResponse{Body: *e, StatusCode: http.StatusBadRequest}, nil
	}
	// seasonsCount is unused by GetSeason, so no GetShow is needed for it.
	s := tmdb.GetSeason(req.TmdbId, req.Season, lang, 0, true)
	if s == nil || s.ID == 0 {
		return GetSeason404JSONResponse{NotFoundJSONResponse(tmdbNotFound)}, nil
	}
	return GetSeason200JSONResponse(season(s)), nil
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
		movies, total = tmdb.SearchMoviesPerPage(p.Query, lang, page, limit)
		items = movieItems(movies)
	} else {
		var shows tmdb.Shows
		shows, total = tmdb.SearchShowsPerPage(p.Query, lang, page, limit)
		items = showItems(shows)
	}
	if total < 0 {
		return SearchdefaultJSONResponse{Body: tmdbFailed, StatusCode: http.StatusBadGateway}, nil
	}
	return Search200JSONResponse{MediaItemPageJSONResponse(mediaPage(items, total, page, limit))}, nil
}

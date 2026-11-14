package jsonapi

import (
	"cmp"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/elgatito/elementum/bittorrent"
	"github.com/elgatito/elementum/config"
	"github.com/elgatito/elementum/library/uid"
	"github.com/elgatito/elementum/tmdb"
)

// The TMDB fetchers take no context and swallow their errors (a failed fetch looks like nil or a
// negative total), so these handlers can neither cancel them nor tell "not found" from "TMDB down".

var (
	langRe       = regexp.MustCompile(`^([a-zA-Z]{2})(?:[-_]([a-zA-Z]{2}))?$`)
	tmdbFailed   = failed(http.StatusBadGateway, ErrorCodeInternalError, "TMDB request failed")
	tmdbNotFound = failed(http.StatusNotFound, ErrorCodeNotFound, "not found")
)

// langArg validates lang and returns TMDB's "de" or "de-DE", the configured language when unset.
func langArg(lang *string) (string, *failure) {
	if lang == nil {
		return config.Get().Language, nil
	}
	m := langRe.FindStringSubmatch(*lang)
	if m == nil {
		return "", bad("lang must look like de or de-DE")
	}
	lng := strings.ToLower(m[1])
	if m[2] != "" {
		lng += "-" + strings.ToUpper(m[2])
	}
	return lng, nil
}

// args validates the common query params and fills their defaults.
func args(page, limit *int, lang *string, enums ...interface{ Valid() bool }) (pg, lim int, lng string, f *failure) {
	if f = checkParams(page, limit, enums...); f != nil {
		return
	}
	if lng, f = langArg(lang); f != nil {
		return 0, 0, "", f
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

// detailArgs checks the id and lang of a details request; ok is false after it answered 400.
func detailArgs(c *gin.Context, id int, lang *string) (lng string, ok bool) {
	f := bad("tmdb_id must be 1 or more")
	if id >= 1 {
		lng, f = langArg(lang)
	}
	if f != nil {
		fail(c, f)
	}
	return lng, f == nil
}

// tmdbList answers a page of a TMDB list or search. fetch returns the page's items and the total, negative
// when TMDB failed. byGenre needs genre and takes year, within the spec's bounds.
func tmdbList(c *gin.Context, page, limit *int, lang *string, kind interface{ Valid() bool }, byGenre bool, genre, year *int,
	fetch func(f tmdb.DiscoverFilters, lang string, page, limit int) ([]MediaItem, int)) {
	pg, lim, lng, f := args(page, limit, lang, kind)
	var filters tmdb.DiscoverFilters
	switch {
	case f != nil || !byGenre:
	case genre == nil:
		f = bad("genre is required for by_genre")
	case *genre < 1:
		f = bad("genre must be 1 or more")
	case year != nil && (*year < 1870 || *year > 2100):
		f = bad("year must be between 1870 and 2100")
	default:
		filters.Genre = strconv.Itoa(*genre)
		if year != nil {
			filters.Year = strconv.Itoa(*year)
		}
	}
	if f != nil {
		fail(c, f)
		return
	}
	if pastLastPage(pg, lim) {
		c.JSON(http.StatusOK, emptyPage(pg, lim))
		return
	}
	items, total := fetch(filters, lng, pg, lim)
	if total < 0 {
		fail(c, tmdbFailed)
		return
	}
	c.JSON(http.StatusOK, mediaPage(annotate(items), total, pg, lim))
}

func (Server) ListMovies(c *gin.Context, list MovieListName, p ListMoviesParams) {
	tmdbList(c, p.Page, p.Limit, p.Lang, list, list == MovieListNameByGenre, p.Genre, p.Year, func(f tmdb.DiscoverFilters, lang string, page, limit int) ([]MediaItem, int) {
		var movies tmdb.Movies
		var total int
		switch list {
		case MovieListNameTrending:
			movies, total = tmdb.TrendingMovies(lang, page, limit, tmdb.Lite)
		case MovieListNameTopRated:
			movies, total = tmdb.TopRatedMovies("", lang, page, limit, tmdb.Lite)
		case MovieListNameNowPlaying:
			movies, total = tmdb.RecentMovies(f, lang, page, limit, tmdb.Lite)
		case MovieListNameUpcoming:
			movies, total = tmdb.UpcomingMovies(lang, page, limit, tmdb.Lite)
		default: // popular, and by_genre with its filters
			movies, total = tmdb.PopularMovies(f, lang, page, limit, tmdb.Lite)
		}
		return movieItems(movies), total
	})
}

func (Server) ListShows(c *gin.Context, list ShowListName, p ListShowsParams) {
	tmdbList(c, p.Page, p.Limit, p.Lang, list, list == ShowListNameByGenre, p.Genre, p.Year, func(f tmdb.DiscoverFilters, lang string, page, limit int) ([]MediaItem, int) {
		var shows tmdb.Shows
		var total int
		switch list {
		case ShowListNameTrending:
			shows, total = tmdb.TrendingShows(lang, page, limit, tmdb.Lite)
		case ShowListNameTopRated:
			shows, total = tmdb.TopRatedShows("", lang, page, limit, tmdb.Lite)
		case ShowListNameOnTheAir:
			shows, total = tmdb.RecentEpisodes(f, lang, page, limit, tmdb.Lite)
		default: // popular, and by_genre with its filters
			shows, total = tmdb.PopularShows(f, lang, page, limit, tmdb.Lite)
		}
		return showItems(shows), total
	})
}

// Search pages movie or show results by limit, like the lists.
func (Server) Search(c *gin.Context, p SearchParams) {
	if strings.TrimSpace(p.Query) == "" {
		fail(c, bad("query is required"))
		return
	}
	tmdbList(c, p.Page, p.Limit, p.Lang, p.Type, false, nil, nil, func(_ tmdb.DiscoverFilters, lang string, page, limit int) ([]MediaItem, int) {
		if p.Type == SearchTypeMovie {
			movies, total := tmdb.SearchMoviesPerPage(p.Query, lang, page, limit, tmdb.Lite)
			return movieItems(movies), total
		}
		shows, total := tmdb.SearchShowsPerPage(p.Query, lang, page, limit, tmdb.Lite)
		return showItems(shows), total
	})
}

func genreList(c *gin.Context, lang *string, fetch func(string) []*tmdb.Genre) {
	lng, f := langArg(lang)
	if f != nil {
		fail(c, f)
		return
	}
	gs := fetch(lng)
	if gs == nil {
		fail(c, tmdbFailed)
		return
	}
	c.JSON(http.StatusOK, genres(gs))
}

func (Server) ListMovieGenres(c *gin.Context, p ListMovieGenresParams) {
	genreList(c, p.Lang, tmdb.GetMovieGenres)
}

func (Server) ListShowGenres(c *gin.Context, p ListShowGenresParams) {
	genreList(c, p.Lang, tmdb.GetTVGenres)
}

func (Server) GetMovie(c *gin.Context, id TmdbIdParam, p GetMovieParams) {
	lang, ok := detailArgs(c, id, p.Lang)
	if !ok {
		return
	}
	m := tmdb.GetMovie(id, lang)
	if m == nil || m.ID == 0 {
		fail(c, tmdbNotFound)
		return
	}
	d := movieDetails(m)
	st := annotate([]MediaItem{{Type: MediaTypeMovie, TmdbId: m.ID}})[0]
	d.InLibrary, d.InCollection, d.InWatchlist, d.Watched, d.Progress = st.InLibrary, st.InCollection, st.InWatchlist, st.Watched, st.Progress
	c.JSON(http.StatusOK, d)
}

func (Server) GetShow(c *gin.Context, id TmdbIdParam, p GetShowParams) {
	lang, ok := detailArgs(c, id, p.Lang)
	if !ok {
		return
	}
	s := tmdb.GetShow(id, lang)
	if s == nil || s.ID == 0 {
		fail(c, tmdbNotFound)
		return
	}
	d := showDetails(s)
	st := annotate([]MediaItem{{Type: MediaTypeShow, TmdbId: s.ID}})[0]
	d.InLibrary, d.InCollection, d.InWatchlist, d.Watched = st.InLibrary, st.InCollection, st.InWatchlist, st.Watched
	if watched, known := episodeWatched(s.ID); known {
		d.setWatched(s.ID, watched)
	}
	c.JSON(http.StatusOK, d)
}

func (Server) GetSeason(c *gin.Context, id TmdbIdParam, number SeasonParam, p GetSeasonParams) {
	if number < 0 {
		fail(c, bad("season must be 0 or more"))
		return
	}
	lang, ok := detailArgs(c, id, p.Lang)
	if !ok {
		return
	}
	// seasonsCount is unused by GetSeason, so no GetShow is needed for it.
	s := tmdb.GetSeason(id, number, lang, 0, true)
	if s == nil || s.ID == 0 {
		fail(c, tmdbNotFound)
		return
	}
	d := season(s)
	watched, known := episodeWatched(id)
	show, err := uid.GetShowByTMDB(id) // Kodi library resume points
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
		if r := storedResume(bittorrent.TMDBResumeKey(true, id, e.SeasonNumber, e.EpisodeNumber)); e.ResumeS == nil && r != nil {
			e.ResumeS = ptr(float32(r.Position))
		}
	}
	c.JSON(http.StatusOK, d)
}

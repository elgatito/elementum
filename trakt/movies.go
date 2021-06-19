package trakt

import (
	"fmt"
	"math/rand"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/elgatito/elementum/cache"
	"github.com/elgatito/elementum/config"
	"github.com/elgatito/elementum/library/playcount"
	"github.com/elgatito/elementum/tmdb"
	"github.com/elgatito/elementum/util"
	"github.com/elgatito/elementum/xbmc"
	"github.com/jmcvetta/napping"
)

// Fill fanart from TMDB
func setFanart(movie *Movie) *Movie {
	if movie.Images == nil {
		movie.Images = &Images{}
	}
	if movie.Images.Poster == nil {
		movie.Images.Poster = &Sizes{}
	}
	if movie.Images.Thumbnail == nil {
		movie.Images.Thumbnail = &Sizes{}
	}
	if movie.Images.FanArt == nil {
		movie.Images.FanArt = &Sizes{}
	}
	if movie.Images.Banner == nil {
		movie.Images.Banner = &Sizes{}
	}
	if movie.Images.ClearArt == nil {
		movie.Images.ClearArt = &Sizes{}
	}

	if movie.IDs == nil || movie.IDs.TMDB == 0 {
		return movie
	}

	tmdbImages := tmdb.GetImages(movie.IDs.TMDB)
	if tmdbImages == nil {
		return movie
	}

	if len(tmdbImages.Posters) > 0 {
		posterImage := tmdb.ImageURL(tmdbImages.Posters[0].FilePath, "w1280")
		for _, image := range tmdbImages.Posters {
			if image.Iso639_1 == config.Get().Language {
				posterImage = tmdb.ImageURL(image.FilePath, "w1280")
			}
		}
		movie.Images.Poster.Full = posterImage
		movie.Images.Thumbnail.Full = posterImage
	}
	if len(tmdbImages.Backdrops) > 0 {
		backdropImage := tmdb.ImageURL(tmdbImages.Backdrops[0].FilePath, "w1280")
		for _, image := range tmdbImages.Backdrops {
			if image.Iso639_1 == config.Get().Language {
				backdropImage = tmdb.ImageURL(image.FilePath, "w1280")
			}
		}
		movie.Images.FanArt.Full = backdropImage
		movie.Images.Banner.Full = backdropImage
	}
	return movie
}

func setFanarts(movies []*Movies) []*Movies {
	wg := sync.WaitGroup{}
	for i, movie := range movies {
		wg.Add(1)
		go func(idx int, m *Movies) {
			defer wg.Done()
			movies[idx].Movie = setFanart(m.Movie)
		}(i, movie)
	}
	wg.Wait()

	return movies
}

func setCalendarFanarts(movies []*CalendarMovie) []*CalendarMovie {
	wg := sync.WaitGroup{}
	for i, movie := range movies {
		wg.Add(1)
		go func(idx int, m *CalendarMovie) {
			defer wg.Done()
			movies[idx].Movie = setFanart(m.Movie)
		}(i, movie)
	}
	wg.Wait()

	return movies
}

// GetMovie ...
func GetMovie(ID string) (movie *Movie) {
	endPoint := fmt.Sprintf("movies/%s", ID)

	params := napping.Params{
		"extended": "full,images",
	}.AsUrlValues()

	cacheStore := cache.NewDBStore()
	key := fmt.Sprintf(cache.TraktMovieKey, ID)
	if err := cacheStore.Get(key, &movie); err != nil {
		resp, err := Get(endPoint, params)

		if err != nil {
			log.Error(err)
			xbmc.Notify("Elementum", fmt.Sprintf("Failed getting Trakt movie (%s), check your logs.", ID), config.AddonIcon())
		}

		if err := resp.Unmarshal(&movie); err != nil {
			log.Warning(err)
		}

		cacheStore.Set(key, movie, cache.TraktMovieExpire)
	}

	return
}

// GetMovieByTMDB ...
func GetMovieByTMDB(tmdbID string) (movie *Movie) {
	endPoint := fmt.Sprintf("search/tmdb/%s?type=movie", tmdbID)

	params := napping.Params{}.AsUrlValues()

	cacheStore := cache.NewDBStore()
	key := fmt.Sprintf(cache.TraktMovieByTMDBKey, tmdbID)
	if err := cacheStore.Get(key, &movie); err != nil {
		resp, err := Get(endPoint, params)
		if err != nil {
			log.Error(err)
			xbmc.Notify("Elementum", "Failed getting Trakt movie using TMDB ID, check your logs.", config.AddonIcon())
			return
		}

		var results MovieSearchResults
		if err := resp.Unmarshal(&results); err != nil {
			log.Warning(err)
		}
		if results != nil && len(results) > 0 && results[0].Movie != nil {
			movie = results[0].Movie
		}
		cacheStore.Set(key, movie, cache.TraktMovieByTMDBExpire)
	}
	return
}

// SearchMovies ...
func SearchMovies(query string, page string) (movies []*Movies, err error) {
	endPoint := "search"

	params := napping.Params{
		"page":     page,
		"limit":    strconv.Itoa(config.Get().ResultsPerPage),
		"query":    query,
		"extended": "full,images",
	}.AsUrlValues()

	resp, err := Get(endPoint, params)

	if err != nil {
		return
	} else if resp.Status() != 200 {
		return movies, fmt.Errorf("Bad status searching Trakt movies: %d", resp.Status())
	}

	// TODO use response headers for pagination limits:
	// X-Pagination-Page-Count:10
	// X-Pagination-Item-Count:100

	if err := resp.Unmarshal(&movies); err != nil {
		log.Warning(err)
	}

	return
}

// TopMovies ...
func TopMovies(topCategory string, page string) (movies []*Movies, total int, err error) {
	endPoint := "movies/" + topCategory
	if topCategory == "recommendations" {
		endPoint = topCategory + "/movies"
	}

	resultsPerPage := config.Get().ResultsPerPage
	limit := resultsPerPage * PagesAtOnce
	pageInt, err := strconv.Atoi(page)
	if err != nil {
		return
	}
	page = strconv.Itoa((pageInt-1)*resultsPerPage/limit + 1)
	params := napping.Params{
		"page":     page,
		"limit":    strconv.Itoa(limit),
		"extended": "full,images",
	}.AsUrlValues()

	cacheStore := cache.NewDBStore()
	key := fmt.Sprintf(cache.TraktMoviesByCategoryKey, topCategory, page)
	totalKey := fmt.Sprintf(cache.TraktMoviesByCategoryTotalKey, topCategory)
	if err := cacheStore.Get(key, &movies); err != nil || len(movies) == 0 {
		var resp *napping.Response
		var err error

		if !config.Get().TraktAuthorized {
			resp, err = Get(endPoint, params)
		} else {
			resp, err = GetWithAuth(endPoint, params)
		}

		if err != nil {
			return movies, 0, err
		} else if resp.Status() != 200 {
			return movies, 0, fmt.Errorf("Bad status getting top %s Trakt shows: %d", topCategory, resp.Status())
		}

		if topCategory == "popular" || topCategory == "recommendations" {
			var movieList []*Movie
			if errUnm := resp.Unmarshal(&movieList); errUnm != nil {
				log.Warning(errUnm)
			}

			movieListing := make([]*Movies, 0)
			for _, movie := range movieList {
				movieItem := Movies{
					Movie: movie,
				}
				movieListing = append(movieListing, &movieItem)
			}
			movies = movieListing
		} else {
			if errUnm := resp.Unmarshal(&movies); errUnm != nil {
				log.Warning(errUnm)
			}
		}

		pagination := getPagination(resp.HttpResponse().Header)
		total = pagination.ItemCount
		if err != nil {
			log.Warning(err)
		} else {
			cacheStore.Set(totalKey, total, cache.TraktMoviesByCategoryTotalExpire)
		}

		cacheStore.Set(key, movies, cache.TraktMoviesByCategoryExpire)
	} else {
		if err := cacheStore.Get(totalKey, &total); err != nil {
			total = -1
		}
	}

	return
}

// WatchlistMovies ...
func WatchlistMovies(isUpdateNeeded bool) (movies []*Movies, err error) {
	if err := Authorized(); err != nil {
		return movies, err
	}

	endPoint := "sync/watchlist/movies"

	params := napping.Params{
		"extended": "full,images",
	}.AsUrlValues()

	cacheStore := cache.NewDBStore()

	if !isUpdateNeeded {
		if err := cacheStore.Get(cache.TraktMoviesWatchlistKey, &movies); err == nil {
			return movies, nil
		}
	}

	resp, err := GetWithAuth(endPoint, params)

	if err != nil {
		return movies, err
	} else if resp.Status() != 200 {
		return movies, fmt.Errorf("Bad status getting Trakt watchlist for movies: %d", resp.Status())
	}

	var watchlist []*WatchlistMovie
	if err := resp.Unmarshal(&watchlist); err != nil {
		log.Warning(err)
	}

	movieListing := make([]*Movies, 0)
	for _, movie := range watchlist {
		movieItem := Movies{
			Movie: movie.Movie,
		}
		movieListing = append(movieListing, &movieItem)
	}
	movies = movieListing

	cacheStore.Set(cache.TraktMoviesWatchlistKey, &movies, cache.TraktMoviesWatchlistExpire)
	return
}

// CollectionMovies ...
func CollectionMovies(isUpdateNeeded bool) (movies []*Movies, err error) {
	if errAuth := Authorized(); errAuth != nil {
		return movies, errAuth
	}

	endPoint := "sync/collection/movies"

	params := napping.Params{
		"extended": "full,images",
	}.AsUrlValues()

	cacheStore := cache.NewDBStore()

	if !isUpdateNeeded {
		if err := cacheStore.Get(cache.TraktMoviesCollectionKey, &movies); err == nil {
			return movies, nil
		}
	}

	resp, errGet := GetWithAuth(endPoint, params)

	if errGet != nil {
		return movies, errGet
	} else if resp.Status() != 200 {
		return movies, fmt.Errorf("Bad status getting Trakt collection for movies: %d", resp.Status())
	}

	var collection []*CollectionMovie
	resp.Unmarshal(&collection)

	movieListing := make([]*Movies, 0)
	for _, movie := range collection {
		movieItem := Movies{
			Movie: movie.Movie,
		}
		movieListing = append(movieListing, &movieItem)
	}
	movies = movieListing

	cacheStore.Set(cache.TraktMoviesCollectionKey, &movies, cache.TraktMoviesCollectionExpire)
	return movies, err
}

// Userlists ...
func Userlists() (lists []*List) {
	traktUsername := config.Get().TraktUsername
	if traktUsername == "" || config.Get().TraktToken == "" || !config.Get().TraktAuthorized {
		xbmc.Notify("Elementum", "LOCALIZE[30149]", config.AddonIcon())
		return lists
	}
	endPoint := fmt.Sprintf("users/%s/lists", traktUsername)

	params := napping.Params{}.AsUrlValues()

	var resp *napping.Response
	var err error

	if !config.Get().TraktAuthorized {
		resp, err = Get(endPoint, params)
	} else {
		resp, err = GetWithAuth(endPoint, params)
	}

	if err != nil {
		xbmc.Notify("Elementum", err.Error(), config.AddonIcon())
		log.Error(err)
		return lists
	}
	if resp.Status() != 200 {
		errMsg := fmt.Sprintf("Bad status getting custom lists for %s: %d", traktUsername, resp.Status())
		xbmc.Notify("Elementum", errMsg, config.AddonIcon())
		log.Warningf(errMsg)
		return lists
	}

	if err := resp.Unmarshal(&lists); err != nil {
		log.Warning(err)
	}

	sort.Slice(lists, func(i int, j int) bool {
		return lists[i].Name < lists[j].Name
	})

	return lists
}

// Likedlists ...
func Likedlists() (lists []*List) {
	traktUsername := config.Get().TraktUsername
	if traktUsername == "" || config.Get().TraktToken == "" {
		xbmc.Notify("Elementum", "LOCALIZE[30149]", config.AddonIcon())
		return lists
	}
	endPoint := "users/likes/lists"
	params := napping.Params{}.AsUrlValues()

	var resp *napping.Response
	var err error

	if !config.Get().TraktAuthorized {
		resp, err = Get(endPoint, params)
	} else {
		resp, err = GetWithAuth(endPoint, params)
	}

	if err != nil {
		xbmc.Notify("Elementum", err.Error(), config.AddonIcon())
		log.Error(err)
		return lists
	}
	if resp.Status() != 200 {
		errMsg := fmt.Sprintf("Bad status getting liked lists for %s: %d", traktUsername, resp.Status())
		xbmc.Notify("Elementum", errMsg, config.AddonIcon())
		log.Warningf(errMsg)
		return lists
	}

	inputLists := []*ListContainer{}
	if err := resp.Unmarshal(&inputLists); err != nil {
		log.Warning(err)
	}

	for _, l := range inputLists {
		lists = append(lists, l.List)
	}

	sort.Slice(lists, func(i int, j int) bool {
		return lists[i].Name < lists[j].Name
	})

	return lists
}

// TopLists ...
func TopLists(page string) (lists []*ListContainer, hasNext bool) {
	pageInt, _ := strconv.Atoi(page)

	endPoint := "lists/popular"
	params := napping.Params{
		"page":  page,
		"limit": strconv.Itoa(config.Get().ResultsPerPage),
	}.AsUrlValues()

	var resp *napping.Response
	var err error

	if !config.Get().TraktAuthorized {
		resp, err = Get(endPoint, params)
	} else {
		resp, err = GetWithAuth(endPoint, params)
	}

	if err != nil {
		xbmc.Notify("Elementum", err.Error(), config.AddonIcon())
		log.Error(err)
		return lists, hasNext
	}
	if resp.Status() != 200 {
		errMsg := fmt.Sprintf("Bad status getting top lists: %d", resp.Status())
		xbmc.Notify("Elementum", errMsg, config.AddonIcon())
		log.Warningf(errMsg)
		return lists, hasNext
	}

	if err := resp.Unmarshal(&lists); err != nil {
		log.Warning(err)
	}

	sort.Slice(lists, func(i int, j int) bool {
		return lists[i].List.Name < lists[j].List.Name
	})

	p := getPagination(resp.HttpResponse().Header)
	hasNext = p.PageCount > pageInt

	return lists, hasNext
}

// ListItemsMovies ...
func ListItemsMovies(user string, listID string, isUpdateNeeded bool) (movies []*Movies, err error) {
	if user == "" || user == "id" {
		user = config.Get().TraktUsername
	}

	endPoint := fmt.Sprintf("users/%s/lists/%s/items/movies", user, listID)

	params := napping.Params{}.AsUrlValues()

	var resp *napping.Response

	cacheStore := cache.NewDBStore()
	key := fmt.Sprintf(cache.TraktMoviesListKey, listID)

	if !isUpdateNeeded {
		if err := cacheStore.Get(key, &movies); err == nil {
			return movies, nil
		}
	}

	var errGet error
	if !config.Get().TraktAuthorized {
		resp, errGet = Get(endPoint, params)
	} else {
		resp, errGet = GetWithAuth(endPoint, params)
	}

	if errGet != nil || resp.Status() != 200 {
		return movies, errGet
	}

	var list []*ListItem
	if err = resp.Unmarshal(&list); err != nil {
		log.Warning(err)
	}

	movieListing := make([]*Movies, 0)
	for _, movie := range list {
		if movie.Movie == nil {
			continue
		}
		movieItem := Movies{
			Movie: movie.Movie,
		}
		movieListing = append(movieListing, &movieItem)
	}
	movies = movieListing

	cacheStore.Set(key, &movies, 1*time.Minute)
	return movies, err
}

// CalendarMovies ...
func CalendarMovies(endPoint string, page string) (movies []*CalendarMovie, total int, err error) {
	resultsPerPage := config.Get().ResultsPerPage
	limit := resultsPerPage * PagesAtOnce
	pageInt, err := strconv.Atoi(page)
	if err != nil {
		return
	}
	page = strconv.Itoa((pageInt-1)*resultsPerPage/limit + 1)
	params := napping.Params{
		"page":     page,
		"limit":    strconv.Itoa(limit),
		"extended": "full,images",
	}.AsUrlValues()

	cacheStore := cache.NewDBStore()
	endPointKey := strings.Replace(endPoint, "/", ".", -1)
	key := fmt.Sprintf(cache.TraktMoviesCalendarKey, endPointKey, page)
	totalKey := fmt.Sprintf(cache.TraktMoviesCalendarTotalKey, endPointKey)
	if err := cacheStore.Get(key, &movies); err != nil {
		resp, err := GetWithAuth("calendars/"+endPoint, params)

		if err != nil {
			log.Error(err)
			return movies, 0, err
		} else if resp.Status() != 200 {
			log.Warning(resp.Status())
			return movies, 0, fmt.Errorf("Bad status getting %s Trakt movies: %d", endPoint, resp.Status())
		}

		if errUnm := resp.Unmarshal(&movies); errUnm != nil {
			log.Warning(errUnm)
		}

		pagination := getPagination(resp.HttpResponse().Header)
		total = pagination.ItemCount
		if err != nil {
			total = -1
		} else {
			cacheStore.Set(totalKey, total, cache.TraktMoviesCalendarTotalExpire)
		}

		cacheStore.Set(key, &movies, cache.TraktMoviesCalendarExpire)
	} else {
		if err := cacheStore.Get(totalKey, &total); err != nil {
			total = -1
		}
	}

	return
}

// WatchedMovies ...
func WatchedMovies(isUpdateNeeded bool) ([]*WatchedMovie, error) {
	var movies []*WatchedMovie
	err := Request(
		"sync/watched/movies",
		napping.Params{},
		true,
		isUpdateNeeded,
		cache.TraktMoviesWatchedKey,
		cache.TraktMoviesWatchedExpire,
		&movies,
	)

	sort.Slice(movies, func(i int, j int) bool {
		return movies[i].LastWatchedAt.Unix() > movies[j].LastWatchedAt.Unix()
	})

	if len(movies) != 0 {
		cache.
			NewDBStore().
			Set(cache.TraktMoviesWatchedKey, &movies, cache.TraktMoviesWatchedExpire)
	}

	return movies, err
}

// PreviousWatchedMovies ...
func PreviousWatchedMovies() (movies []*WatchedMovie, err error) {
	err = cache.
		NewDBStore().
		Get(cache.TraktMoviesWatchedKey, &movies)

	return
}

// PausedMovies ...
func PausedMovies(isUpdateNeeded bool) ([]*PausedMovie, error) {
	var movies []*PausedMovie
	err := Request(
		"sync/playback/movies",
		napping.Params{
			"extended": "full",
		},
		true,
		isUpdateNeeded,
		cache.TraktMoviesPausedKey,
		cache.TraktMoviesPausedExpire,
		&movies,
	)

	return movies, err
}

// ToListItem ...
func (movie *Movie) ToListItem() (item *xbmc.ListItem) {
	if !config.Get().ForceUseTrakt && movie.IDs.TMDB != 0 {
		tmdbID := strconv.Itoa(movie.IDs.TMDB)
		if tmdbMovie := tmdb.GetMovieByID(tmdbID, config.Get().Language); tmdbMovie != nil {
			item = tmdbMovie.ToListItem()
		}
	}
	if item == nil {
		movie = setFanart(movie)
		item = &xbmc.ListItem{
			Label: movie.Title,
			Info: &xbmc.ListItemInfo{
				Count:         rand.Int(),
				Title:         movie.Title,
				OriginalTitle: movie.Title,
				Year:          movie.Year,
				Genre:         movie.Genres,
				Plot:          movie.Overview,
				PlotOutline:   movie.Overview,
				TagLine:       movie.TagLine,
				Rating:        movie.Rating,
				Votes:         strconv.Itoa(movie.Votes),
				Duration:      movie.Runtime * 60,
				MPAA:          movie.Certification,
				Code:          movie.IDs.IMDB,
				IMDBNumber:    movie.IDs.IMDB,
				Trailer:       util.TrailerURL(movie.Trailer),
				PlayCount:     playcount.GetWatchedMovieByTMDB(movie.IDs.TMDB).Int(),
				DBTYPE:        "movie",
				Mediatype:     "movie",
			},
			Art: &xbmc.ListItemArt{
				Poster:    movie.Images.Poster.Full,
				FanArt:    movie.Images.FanArt.Full,
				Banner:    movie.Images.Banner.Full,
				Thumbnail: movie.Images.Thumbnail.Full,
				ClearArt:  movie.Images.ClearArt.Full,
			},
			Thumbnail: movie.Images.Poster.Full,
			UniqueIDs: &xbmc.UniqueIDs{
				TMDB: strconv.Itoa(movie.IDs.TMDB),
			},
		}
	}

	/*if lm, err := library.GetMovieByTMDB(movie.IDs.TMDB); lm != nil && err == nil {
		item.Info.DBID = lm.UIDs.Kodi
	} else {
		fakeDBID := util.GetMovieFakeDBID(movie.IDs.TMDB)
		if fakeDBID > 0 {
			item.Info.DBID = fakeDBID
		}
	}*/

	if len(item.Info.Trailer) == 0 {
		item.Info.Trailer = util.TrailerURL(movie.Trailer)
	}

	return
}

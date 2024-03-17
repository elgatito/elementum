package api

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/anacrolix/missinggo/perf"
	"github.com/anacrolix/sync"
	"github.com/gin-gonic/gin"

	"github.com/elgatito/elementum/cache"
	"github.com/elgatito/elementum/config"
	"github.com/elgatito/elementum/database"
	"github.com/elgatito/elementum/library"
	"github.com/elgatito/elementum/library/uid"
	"github.com/elgatito/elementum/tmdb"
	"github.com/elgatito/elementum/trakt"
	"github.com/elgatito/elementum/util"
	"github.com/elgatito/elementum/xbmc"
)

func inMoviesWatchlist(tmdbID int) bool {
	if !config.Get().TraktAuthorized || !config.Get().TraktSyncEnabled {
		return false
	}

	defer perf.ScopeTimer()()

	container := uid.Get().GetContainer(uid.WatchlistedMoviesContainer)
	container.Mu.RLock()
	defer container.Mu.RUnlock()

	return container.HasWithType(library.MovieType, library.TMDBScraper, tmdbID)
}

func inShowsWatchlist(tmdbID int) bool {
	if !config.Get().TraktAuthorized || !config.Get().TraktSyncEnabled {
		return false
	}

	defer perf.ScopeTimer()()

	container := uid.Get().GetContainer(uid.WatchlistedShowsContainer)
	container.Mu.RLock()
	defer container.Mu.RUnlock()

	return container.HasWithType(library.ShowType, library.TMDBScraper, tmdbID)
}

func inMoviesCollection(tmdbID int) bool {
	if !config.Get().TraktAuthorized || !config.Get().TraktSyncEnabled {
		return false
	}

	defer perf.ScopeTimer()()

	container := uid.Get().GetContainer(uid.CollectedMoviesContainer)
	container.Mu.RLock()
	defer container.Mu.RUnlock()

	return container.HasWithType(library.MovieType, library.TMDBScraper, tmdbID)
}

func inShowsCollection(tmdbID int) bool {
	if !config.Get().TraktAuthorized || !config.Get().TraktSyncEnabled {
		return false
	}

	defer perf.ScopeTimer()()

	container := uid.Get().GetContainer(uid.CollectedShowsContainer)
	container.Mu.RLock()
	defer container.Mu.RUnlock()

	return container.HasWithType(library.ShowType, library.TMDBScraper, tmdbID)
}

//
// Authorization
//

// AuthorizeTrakt ...
func AuthorizeTrakt(ctx *gin.Context) {
	xbmcHost, _ := xbmc.GetXBMCHostWithContext(ctx)

	err := trakt.Authorize(true)
	if err == nil {
		ctx.String(200, "")
	} else {
		xbmcHost.Notify("Elementum", err.Error(), config.AddonIcon())
		ctx.String(200, "")
	}
}

// DeauthorizeTrakt ...
func DeauthorizeTrakt(ctx *gin.Context) {
	xbmcHost, _ := xbmc.GetXBMCHostWithContext(ctx)

	err := trakt.Deauthorize(true)
	if err == nil {
		ctx.String(200, "")
	} else {
		xbmcHost.Notify("Elementum", err.Error(), config.AddonIcon())
		ctx.String(200, "")
	}
}

//
// Main lists
//

// WatchlistMovies ...
func WatchlistMovies(ctx *gin.Context) {
	defer perf.ScopeTimer()()

	xbmcHost, _ := xbmc.GetXBMCHostWithContext(ctx)

	activities, err := trakt.GetActivities("WatchlistMovies")
	movies, err := trakt.WatchlistMovies(err != nil || activities.MoviesWatchlisted())
	if err != nil {
		xbmcHost.Notify("Elementum", err.Error(), config.AddonIcon())
	}
	renderTraktMovies(ctx, movies, -1, 0)
}

// WatchlistShows ...
func WatchlistShows(ctx *gin.Context) {
	defer perf.ScopeTimer()()

	xbmcHost, _ := xbmc.GetXBMCHostWithContext(ctx)

	activities, err := trakt.GetActivities("WatchlistShows")
	shows, err := trakt.WatchlistShows(err != nil || activities.ShowsWatchlisted())
	if err != nil {
		xbmcHost.Notify("Elementum", err.Error(), config.AddonIcon())
	}
	renderTraktShows(ctx, shows, -1, 0)
}

// CollectionMovies ...
func CollectionMovies(ctx *gin.Context) {
	defer perf.ScopeTimer()()

	xbmcHost, _ := xbmc.GetXBMCHostWithContext(ctx)

	activities, err := trakt.GetActivities("CollectionMovies")
	movies, err := trakt.CollectionMovies(err != nil || activities.MoviesCollected())
	if err != nil {
		xbmcHost.Notify("Elementum", err.Error(), config.AddonIcon())
	}
	renderTraktMovies(ctx, movies, -1, 0)
}

// CollectionShows ...
func CollectionShows(ctx *gin.Context) {
	defer perf.ScopeTimer()()

	xbmcHost, _ := xbmc.GetXBMCHostWithContext(ctx)

	activities, err := trakt.GetActivities("CollectionShows")
	shows, err := trakt.CollectionShows(err != nil || activities.EpisodesCollected())
	if err != nil {
		xbmcHost.Notify("Elementum", err.Error(), config.AddonIcon())
	}
	renderTraktShows(ctx, shows, -1, 0)
}

// UserlistMovies ...
func UserlistMovies(ctx *gin.Context) {
	defer perf.ScopeTimer()()

	xbmcHost, _ := xbmc.GetXBMCHostWithContext(ctx)

	user := ctx.Params.ByName("user")
	listID := ctx.Params.ByName("listId")
	pageParam := ctx.DefaultQuery("page", "1")
	page, _ := strconv.Atoi(pageParam)

	movies, err := trakt.ListItemsMovies(user, listID)
	if err != nil {
		xbmcHost.Notify("Elementum", err.Error(), config.AddonIcon())
	}
	renderTraktMovies(ctx, movies, -1, page)
}

// UserlistShows ...
func UserlistShows(ctx *gin.Context) {
	defer perf.ScopeTimer()()

	xbmcHost, _ := xbmc.GetXBMCHostWithContext(ctx)

	user := ctx.Params.ByName("user")
	listID := ctx.Params.ByName("listId")
	pageParam := ctx.DefaultQuery("page", "1")
	page, _ := strconv.Atoi(pageParam)

	shows, err := trakt.ListItemsShows(user, listID)
	if err != nil {
		xbmcHost.Notify("Elementum", err.Error(), config.AddonIcon())
	}
	renderTraktShows(ctx, shows, -1, page)
}

// func WatchlistSeasons(ctx *gin.Context) {
// 	renderTraktSeasons(trakt.Watchlist("seasons", pageParam), ctx, page)
// }

// func WatchlistEpisodes(ctx *gin.Context) {
// 	renderTraktEpisodes(trakt.Watchlist("episodes", pageParam), ctx, page)
// }

//
// Main lists actions
//

// AddMovieToWatchlist ...
func AddMovieToWatchlist(ctx *gin.Context) {
	defer perf.ScopeTimer()()

	xbmcHost, _ := xbmc.GetXBMCHostWithContext(ctx)

	tmdbID := ctx.Params.ByName("tmdbId")
	req, err := trakt.AddToWatchlist("movies", tmdbID)
	if err != nil {
		xbmcHost.Notify("Elementum", err.Error(), config.AddonIcon())
	} else if req.ResponseStatusCode != 201 {
		xbmcHost.Notify("Elementum", fmt.Sprintf("Failed with %d status code", req.ResponseStatusCode), config.AddonIcon())
	} else {
		xbmcHost.Notify("Elementum", "Movie added to watchlist", config.AddonIcon())
		database.GetCache().DeleteWithPrefix(database.CommonBucket, []byte("com.trakt.watchlist.movies"))
		database.GetCache().DeleteWithPrefix(database.CommonBucket, []byte("com.trakt.movies.watchlist"))
		if ctx != nil {
			ctx.Abort()
		}
		library.ClearPageCache(xbmcHost)
	}
}

// RemoveMovieFromWatchlist ...
func RemoveMovieFromWatchlist(ctx *gin.Context) {
	defer perf.ScopeTimer()()

	xbmcHost, _ := xbmc.GetXBMCHostWithContext(ctx)

	tmdbID := ctx.Params.ByName("tmdbId")
	_, err := trakt.RemoveFromWatchlist("movies", tmdbID)
	if err != nil {
		xbmcHost.Notify("Elementum", err.Error(), config.AddonIcon())
	} else {
		xbmcHost.Notify("Elementum", "Movie removed from watchlist", config.AddonIcon())
		database.GetCache().DeleteWithPrefix(database.CommonBucket, []byte("com.trakt.watchlist.movies"))
		database.GetCache().DeleteWithPrefix(database.CommonBucket, []byte("com.trakt.movies.watchlist"))
		if ctx != nil {
			ctx.Abort()
		}
		library.ClearPageCache(xbmcHost)
	}
}

// AddShowToWatchlist ...
func AddShowToWatchlist(ctx *gin.Context) {
	defer perf.ScopeTimer()()

	xbmcHost, _ := xbmc.GetXBMCHostWithContext(ctx)

	tmdbID := ctx.Params.ByName("showId")
	req, err := trakt.AddToWatchlist("shows", tmdbID)
	if err != nil {
		xbmcHost.Notify("Elementum", err.Error(), config.AddonIcon())
	} else if req.ResponseStatusCode != 201 {
		xbmcHost.Notify("Elementum", fmt.Sprintf("Failed %d", req.ResponseStatusCode), config.AddonIcon())
	} else {
		xbmcHost.Notify("Elementum", "Show added to watchlist", config.AddonIcon())
		database.GetCache().DeleteWithPrefix(database.CommonBucket, []byte("com.trakt.watchlist.shows"))
		database.GetCache().DeleteWithPrefix(database.CommonBucket, []byte("com.trakt.shows.watchlist"))
		if ctx != nil {
			ctx.Abort()
		}
		library.ClearPageCache(xbmcHost)
	}
}

// RemoveShowFromWatchlist ...
func RemoveShowFromWatchlist(ctx *gin.Context) {
	defer perf.ScopeTimer()()

	xbmcHost, _ := xbmc.GetXBMCHostWithContext(ctx)

	tmdbID := ctx.Params.ByName("showId")
	_, err := trakt.RemoveFromWatchlist("shows", tmdbID)
	if err != nil {
		xbmcHost.Notify("Elementum", err.Error(), config.AddonIcon())
	} else {
		xbmcHost.Notify("Elementum", "Show removed from watchlist", config.AddonIcon())
		database.GetCache().DeleteWithPrefix(database.CommonBucket, []byte("com.trakt.watchlist.shows"))
		database.GetCache().DeleteWithPrefix(database.CommonBucket, []byte("com.trakt.shows.watchlist"))
		if ctx != nil {
			ctx.Abort()
		}
		library.ClearPageCache(xbmcHost)
	}
}

// AddMovieToCollection ...
func AddMovieToCollection(ctx *gin.Context) {
	defer perf.ScopeTimer()()

	xbmcHost, _ := xbmc.GetXBMCHostWithContext(ctx)

	tmdbID := ctx.Params.ByName("tmdbId")
	req, err := trakt.AddToCollection("movies", tmdbID)
	if err != nil {
		xbmcHost.Notify("Elementum", err.Error(), config.AddonIcon())
	} else if req.ResponseStatusCode != 201 {
		xbmcHost.Notify("Elementum", fmt.Sprintf("Failed with %d status code", req.ResponseStatusCode), config.AddonIcon())
	} else {
		xbmcHost.Notify("Elementum", "Movie added to collection", config.AddonIcon())
		database.GetCache().DeleteWithPrefix(database.CommonBucket, []byte("com.trakt.collection.movies"))
		database.GetCache().DeleteWithPrefix(database.CommonBucket, []byte("com.trakt.movies.collection"))
		if ctx != nil {
			ctx.Abort()
		}
		library.ClearPageCache(xbmcHost)
	}
}

// RemoveMovieFromCollection ...
func RemoveMovieFromCollection(ctx *gin.Context) {
	defer perf.ScopeTimer()()

	xbmcHost, _ := xbmc.GetXBMCHostWithContext(ctx)

	tmdbID := ctx.Params.ByName("tmdbId")
	_, err := trakt.RemoveFromCollection("movies", tmdbID)
	if err != nil {
		xbmcHost.Notify("Elementum", err.Error(), config.AddonIcon())
	} else {
		xbmcHost.Notify("Elementum", "Movie removed from collection", config.AddonIcon())
		database.GetCache().DeleteWithPrefix(database.CommonBucket, []byte("com.trakt.collection.movies"))
		database.GetCache().DeleteWithPrefix(database.CommonBucket, []byte("com.trakt.movies.collection"))
		if ctx != nil {
			ctx.Abort()
		}
		library.ClearPageCache(xbmcHost)
	}
}

// AddShowToCollection ...
func AddShowToCollection(ctx *gin.Context) {
	defer perf.ScopeTimer()()

	xbmcHost, _ := xbmc.GetXBMCHostWithContext(ctx)

	tmdbID := ctx.Params.ByName("showId")
	req, err := trakt.AddToCollection("shows", tmdbID)
	if err != nil {
		xbmcHost.Notify("Elementum", err.Error(), config.AddonIcon())
	} else if req.ResponseStatusCode != 201 {
		xbmcHost.Notify("Elementum", fmt.Sprintf("Failed with %d status code", req.ResponseStatusCode), config.AddonIcon())
	} else {
		xbmcHost.Notify("Elementum", "Show added to collection", config.AddonIcon())
		database.GetCache().DeleteWithPrefix(database.CommonBucket, []byte("com.trakt.collection.shows"))
		database.GetCache().DeleteWithPrefix(database.CommonBucket, []byte("com.trakt.shows.collection"))
		if ctx != nil {
			ctx.Abort()
		}
		library.ClearPageCache(xbmcHost)
	}
}

// RemoveShowFromCollection ...
func RemoveShowFromCollection(ctx *gin.Context) {
	defer perf.ScopeTimer()()

	xbmcHost, _ := xbmc.GetXBMCHostWithContext(ctx)

	tmdbID := ctx.Params.ByName("showId")
	_, err := trakt.RemoveFromCollection("shows", tmdbID)
	if err != nil {
		xbmcHost.Notify("Elementum", err.Error(), config.AddonIcon())
	} else {
		xbmcHost.Notify("Elementum", "Show removed from collection", config.AddonIcon())
		database.GetCache().DeleteWithPrefix(database.CommonBucket, []byte("com.trakt.collection.shows"))
		database.GetCache().DeleteWithPrefix(database.CommonBucket, []byte("com.trakt.shows.collection"))
		if ctx != nil {
			ctx.Abort()
		}
		library.ClearPageCache(xbmcHost)
	}
}

// func AddEpisodeToWatchlist(ctx *gin.Context) {
// 	tmdbId := ctx.Params.ByName("episodeId")
// 	resp, err := trakt.AddToWatchlist("episodes", tmdbId)
// 	if err != nil {
// 		xbmc.Notify("Elementum", fmt.Sprintf("Failed: %s", err), config.AddonIcon())
// 	} else if resp.Status() != 201 {
// 		xbmc.Notify("Elementum", fmt.Sprintf("Failed: %d", resp.Status()), config.AddonIcon())
// 	} else {
// 		xbmc.Notify("Elementum", "Episode added to watchlist", config.AddonIcon())
// 	}
// }

func renderTraktMovies(ctx *gin.Context, movies []*trakt.Movies, total int, page int) {
	defer perf.ScopeTimer()()

	hasNextPage := 0
	if page > 0 {
		resultsPerPage := config.Get().ResultsPerPage

		if total == -1 {
			total = len(movies)
		}
		if total > resultsPerPage {
			if page*resultsPerPage < total {
				hasNextPage = 1
			}
		}

		if len(movies) > resultsPerPage {
			start := (page - 1) * resultsPerPage
			end := start + resultsPerPage
			if len(movies) <= end {
				movies = movies[start:]
			} else {
				movies = movies[start:end]
			}
		}
	}

	items := make(xbmc.ListItems, len(movies))
	wg := sync.WaitGroup{}
	for idx := 0; idx < len(movies); idx++ {
		wg.Add(1)
		go func(movieListing *trakt.Movies, index int) {
			defer wg.Done()
			if movieListing == nil || movieListing.Movie == nil {
				return
			}

			item := movieListing.Movie.ToListItem()
			if item == nil {
				return
			}

			// Example of adding UTF8 char into title,
			// list: https://www.utf8-chartable.de/unicode-utf8-table.pl?start=9728&number=1024&names=2&utf8=string-literal
			// item.Label += " \xe2\x98\x85"
			// item.Info.Title += " \xe2\x98\x85"

			tmdbID := strconv.Itoa(movieListing.Movie.IDs.TMDB)

			thisURL := URLForXBMC("/movie/%d/", movieListing.Movie.IDs.TMDB) + "%s/%s"

			contextLabel := playLabel
			contextTitle := fmt.Sprintf("%s (%d)", item.Info.OriginalTitle, movieListing.Movie.Year)
			contextURL := contextPlayOppositeURL(thisURL, contextTitle, false)
			if config.Get().ChooseStreamAutoMovie {
				contextLabel = linksLabel
			}

			item.Path = contextPlayURL(thisURL, contextTitle, false)

			libraryActions := [][]string{
				{contextLabel, fmt.Sprintf("PlayMedia(%s)", contextURL)},
			}
			if uid.IsDuplicateMovie(tmdbID) || uid.IsAddedToLibrary(tmdbID, library.MovieType) || library.IsInLibrary(movieListing.Movie.IDs.TMDB, library.MovieType) {
				libraryActions = append(libraryActions, []string{"LOCALIZE[30283]", fmt.Sprintf("RunPlugin(%s)", URLForXBMC("/library/movie/add/%d?force=true", movieListing.Movie.IDs.TMDB))})
				libraryActions = append(libraryActions, []string{"LOCALIZE[30253]", fmt.Sprintf("RunPlugin(%s)", URLForXBMC("/library/movie/remove/%d", movieListing.Movie.IDs.TMDB))})
			} else {
				libraryActions = append(libraryActions, []string{"LOCALIZE[30252]", fmt.Sprintf("RunPlugin(%s)", URLForXBMC("/library/movie/add/%d", movieListing.Movie.IDs.TMDB))})
			}

			toggleWatchedAction := []string{"LOCALIZE[30667]", fmt.Sprintf("RunPlugin(%s)", URLForXBMC("/movie/%d/watched", movieListing.Movie.IDs.TMDB))}
			if item.Info.PlayCount > 0 {
				toggleWatchedAction = []string{"LOCALIZE[30668]", fmt.Sprintf("RunPlugin(%s)", URLForXBMC("/movie/%d/unwatched", movieListing.Movie.IDs.TMDB))}
			}

			watchlistAction := []string{"LOCALIZE[30255]", fmt.Sprintf("RunPlugin(%s)", URLForXBMC("/movie/%d/watchlist/add", movieListing.Movie.IDs.TMDB))}
			if inMoviesWatchlist(movieListing.Movie.IDs.TMDB) {
				watchlistAction = []string{"LOCALIZE[30256]", fmt.Sprintf("RunPlugin(%s)", URLForXBMC("/movie/%d/watchlist/remove", movieListing.Movie.IDs.TMDB))}
			}

			collectionAction := []string{"LOCALIZE[30258]", fmt.Sprintf("RunPlugin(%s)", URLForXBMC("/movie/%d/collection/add", movieListing.Movie.IDs.TMDB))}
			if inMoviesCollection(movieListing.Movie.IDs.TMDB) {
				collectionAction = []string{"LOCALIZE[30259]", fmt.Sprintf("RunPlugin(%s)", URLForXBMC("/movie/%d/collection/remove", movieListing.Movie.IDs.TMDB))}
			}

			item.ContextMenu = [][]string{
				{"LOCALIZE[30619];;LOCALIZE[30214]", fmt.Sprintf("Container.Update(%s)", URLForXBMC("/movies/"))},
				toggleWatchedAction,
				watchlistAction,
				collectionAction,
				{"LOCALIZE[30034]", fmt.Sprintf("RunPlugin(%s)", URLForXBMC("/setviewmode/movies"))},
			}
			item.ContextMenu = append(libraryActions, item.ContextMenu...)

			if config.Get().Platform.Kodi < 17 {
				item.ContextMenu = append(item.ContextMenu,
					[]string{"LOCALIZE[30203]", "Action(Info)"},
					[]string{"LOCALIZE[30268]", "Action(ToggleWatched)"},
				)
			}

			item.IsPlayable = true
			items[index] = item

		}(movies[idx], idx)
	}
	wg.Wait()

	if page >= 0 && hasNextPage > 0 {
		path := ctx.Request.URL.Path
		nextpage := &xbmc.ListItem{
			Label:      "LOCALIZE[30415];;" + strconv.Itoa(page+1),
			Path:       URLForXBMC(fmt.Sprintf("%s?page=%d", path, page+1)),
			Thumbnail:  config.AddonResource("img", "nextpage.png"),
			Properties: &xbmc.ListItemProperties{SpecialSort: "bottom"},
		}
		items = append(items, nextpage)
	}
	ctx.JSON(200, xbmc.NewView("movies", items))
}

// TraktPopularMovies ...
func TraktPopularMovies(ctx *gin.Context) {
	defer perf.ScopeTimer()()

	xbmcHost, _ := xbmc.GetXBMCHostWithContext(ctx)

	pageParam := ctx.DefaultQuery("page", "1")
	page, _ := strconv.Atoi(pageParam)
	movies, total, err := trakt.TopMovies("popular", pageParam)

	if err != nil {
		xbmcHost.Notify("Elementum", err.Error(), config.AddonIcon())
	}
	renderTraktMovies(ctx, movies, total, page)
}

// TraktRecommendationsMovies ...
func TraktRecommendationsMovies(ctx *gin.Context) {
	defer perf.ScopeTimer()()

	xbmcHost, _ := xbmc.GetXBMCHostWithContext(ctx)

	page := config.Get().ResultsPerPage * -5
	pageParam := strconv.Itoa(page)
	movies, total, err := trakt.TopMovies("recommendations", pageParam)

	if err != nil {
		xbmcHost.Notify("Elementum", err.Error(), config.AddonIcon())
	}
	renderTraktMovies(ctx, movies, total, page)
}

// TraktTrendingMovies ...
func TraktTrendingMovies(ctx *gin.Context) {
	defer perf.ScopeTimer()()

	xbmcHost, _ := xbmc.GetXBMCHostWithContext(ctx)

	pageParam := ctx.DefaultQuery("page", "1")
	page, _ := strconv.Atoi(pageParam)
	movies, total, err := trakt.TopMovies("trending", pageParam)

	if err != nil {
		xbmcHost.Notify("Elementum", err.Error(), config.AddonIcon())
	}
	renderTraktMovies(ctx, movies, total, page)
}

// TraktMostPlayedMovies ...
func TraktMostPlayedMovies(ctx *gin.Context) {
	defer perf.ScopeTimer()()

	xbmcHost, _ := xbmc.GetXBMCHostWithContext(ctx)

	pageParam := ctx.DefaultQuery("page", "1")
	page, _ := strconv.Atoi(pageParam)
	movies, total, err := trakt.TopMovies("played", pageParam)

	if err != nil {
		xbmcHost.Notify("Elementum", err.Error(), config.AddonIcon())
	}
	renderTraktMovies(ctx, movies, total, page)
}

// TraktMostWatchedMovies ...
func TraktMostWatchedMovies(ctx *gin.Context) {
	defer perf.ScopeTimer()()

	xbmcHost, _ := xbmc.GetXBMCHostWithContext(ctx)

	pageParam := ctx.DefaultQuery("page", "1")
	page, _ := strconv.Atoi(pageParam)
	movies, total, err := trakt.TopMovies("watched", pageParam)

	if err != nil {
		xbmcHost.Notify("Elementum", err.Error(), config.AddonIcon())
	}
	renderTraktMovies(ctx, movies, total, page)
}

// TraktMostCollectedMovies ...
func TraktMostCollectedMovies(ctx *gin.Context) {
	defer perf.ScopeTimer()()

	xbmcHost, _ := xbmc.GetXBMCHostWithContext(ctx)

	pageParam := ctx.DefaultQuery("page", "1")
	page, _ := strconv.Atoi(pageParam)
	movies, total, err := trakt.TopMovies("collected", pageParam)

	if err != nil {
		xbmcHost.Notify("Elementum", err.Error(), config.AddonIcon())
	}
	renderTraktMovies(ctx, movies, total, page)
}

// TraktMostAnticipatedMovies ...
func TraktMostAnticipatedMovies(ctx *gin.Context) {
	defer perf.ScopeTimer()()

	xbmcHost, _ := xbmc.GetXBMCHostWithContext(ctx)

	pageParam := ctx.DefaultQuery("page", "1")
	page, _ := strconv.Atoi(pageParam)
	movies, total, err := trakt.TopMovies("anticipated", pageParam)

	if err != nil {
		xbmcHost.Notify("Elementum", err.Error(), config.AddonIcon())
	}
	renderTraktMovies(ctx, movies, total, page)
}

// TraktBoxOffice ...
func TraktBoxOffice(ctx *gin.Context) {
	defer perf.ScopeTimer()()

	xbmcHost, _ := xbmc.GetXBMCHostWithContext(ctx)

	movies, _, err := trakt.TopMovies("boxoffice", "1")
	if err != nil {
		xbmcHost.Notify("Elementum", err.Error(), config.AddonIcon())
	}
	renderTraktMovies(ctx, movies, -1, 0)
}

// TraktHistoryMovies ...
func TraktHistoryMovies(ctx *gin.Context) {
	defer perf.ScopeTimer()()

	xbmcHost, _ := xbmc.GetXBMCHostWithContext(ctx)

	pageParam := ctx.DefaultQuery("page", "1")
	page, _ := strconv.Atoi(pageParam)

	activities, err := trakt.GetActivities("HistoryMovies")

	watchedMovies, err := trakt.WatchedMovies(err != nil || activities.MoviesWatched())
	if err != nil {
		xbmcHost.Notify("Elementum", err.Error(), config.AddonIcon())
	}
	movies := make([]*trakt.Movies, 0)
	for _, movie := range watchedMovies {
		movieItem := trakt.Movies{
			Movie: movie.Movie,
		}
		movies = append(movies, &movieItem)
	}

	renderTraktMovies(ctx, movies, -1, page)
}

// TraktHistoryShows ...
func TraktHistoryShows(ctx *gin.Context) {
	defer perf.ScopeTimer()()

	xbmcHost, _ := xbmc.GetXBMCHostWithContext(ctx)

	pageParam := ctx.DefaultQuery("page", "1")
	page, _ := strconv.Atoi(pageParam)

	activities, err := trakt.GetActivities("HistoryShows")

	watchedShows, err := trakt.WatchedShows(err != nil || activities.EpisodesWatched())
	if err != nil {
		xbmcHost.Notify("Elementum", err.Error(), config.AddonIcon())
	}
	shows := make([]*trakt.Shows, 0)
	for _, show := range watchedShows {
		showItem := trakt.Shows{
			Show: show.Show,
		}
		shows = append(shows, &showItem)
	}

	renderTraktShows(ctx, shows, -1, page)
}

// TraktProgressShows ...
func TraktProgressShows(ctx *gin.Context) {
	defer perf.ScopeTimer()()

	xbmcHost, _ := xbmc.GetXBMCHostWithContext(ctx)

	shows, err := trakt.WatchedShowsProgress()
	if err != nil {
		xbmcHost.Notify("Elementum", err.Error(), config.AddonIcon())
	}

	renderProgressShows(ctx, shows, -1, 0)
}

func renderTraktShows(ctx *gin.Context, shows []*trakt.Shows, total int, page int) {
	hasNextPage := 0
	if page > 0 {
		resultsPerPage := config.Get().ResultsPerPage

		if total == -1 {
			total = len(shows)
		}
		if total > resultsPerPage {
			if page*resultsPerPage < total {
				hasNextPage = 1
			}
		}

		if len(shows) > resultsPerPage {
			start := (page - 1) * resultsPerPage
			end := start + resultsPerPage
			if len(shows) <= end {
				shows = shows[start:]
			} else {
				shows = shows[start:end]
			}
		}
	}

	items := make(xbmc.ListItems, len(shows)+hasNextPage)

	wg := sync.WaitGroup{}
	wg.Add(len(shows))

	for i, showListing := range shows {
		go func(i int, showListing *trakt.Shows) {
			defer wg.Done()

			if showListing == nil || showListing.Show == nil {
				return
			}

			item := showListing.Show.ToListItem()
			if item == nil {
				return
			}

			tmdbID := strconv.Itoa(showListing.Show.IDs.TMDB)

			item.Path = URLForXBMC("/show/%d/seasons", showListing.Show.IDs.TMDB)

			libraryActions := [][]string{}
			if uid.IsDuplicateShow(tmdbID) || uid.IsAddedToLibrary(tmdbID, library.ShowType) || library.IsInLibrary(showListing.Show.IDs.TMDB, library.ShowType) {
				libraryActions = append(libraryActions, []string{"LOCALIZE[30283]", fmt.Sprintf("RunPlugin(%s)", URLForXBMC("/library/show/add/%d?force=true", showListing.Show.IDs.TMDB))})
				libraryActions = append(libraryActions, []string{"LOCALIZE[30253]", fmt.Sprintf("RunPlugin(%s)", URLForXBMC("/library/show/remove/%d", showListing.Show.IDs.TMDB))})
			} else {
				libraryActions = append(libraryActions, []string{"LOCALIZE[30252]", fmt.Sprintf("RunPlugin(%s)", URLForXBMC("/library/show/add/%d", showListing.Show.IDs.TMDB))})
			}

			toggleWatchedAction := []string{"LOCALIZE[30667]", fmt.Sprintf("RunPlugin(%s)", URLForXBMC("/show/%d/watched", showListing.Show.IDs.TMDB))}
			if item.Info.PlayCount > 0 {
				toggleWatchedAction = []string{"LOCALIZE[30668]", fmt.Sprintf("RunPlugin(%s)", URLForXBMC("/show/%d/unwatched", showListing.Show.IDs.TMDB))}
			}

			watchlistAction := []string{"LOCALIZE[30255]", fmt.Sprintf("RunPlugin(%s)", URLForXBMC("/show/%d/watchlist/add", showListing.Show.IDs.TMDB))}
			if inShowsWatchlist(showListing.Show.IDs.TMDB) {
				watchlistAction = []string{"LOCALIZE[30256]", fmt.Sprintf("RunPlugin(%s)", URLForXBMC("/show/%d/watchlist/remove", showListing.Show.IDs.TMDB))}
			}

			collectionAction := []string{"LOCALIZE[30258]", fmt.Sprintf("RunPlugin(%s)", URLForXBMC("/show/%d/collection/add", showListing.Show.IDs.TMDB))}
			if inShowsCollection(showListing.Show.IDs.TMDB) {
				collectionAction = []string{"LOCALIZE[30259]", fmt.Sprintf("RunPlugin(%s)", URLForXBMC("/show/%d/collection/remove", showListing.Show.IDs.TMDB))}
			}

			item.ContextMenu = [][]string{
				{"LOCALIZE[30619];;LOCALIZE[30215]", fmt.Sprintf("Container.Update(%s)", URLForXBMC("/shows/"))},
				toggleWatchedAction,
				watchlistAction,
				collectionAction,
				{"LOCALIZE[30035]", fmt.Sprintf("RunPlugin(%s)", URLForXBMC("/setviewmode/tvshows"))},
			}
			item.ContextMenu = append(libraryActions, item.ContextMenu...)

			if config.Get().Platform.Kodi < 17 {
				item.ContextMenu = append(item.ContextMenu,
					[]string{"LOCALIZE[30203]", "Action(Info)"},
					[]string{"LOCALIZE[30268]", "Action(ToggleWatched)"},
				)
			}

			items[i] = item
		}(i, showListing)
	}
	wg.Wait()

	for i := len(items) - 1; i >= 0; i-- {
		if items[i] == nil {
			items = append(items[:i], items[i+1:]...)
		}
	}

	if page >= 0 && hasNextPage > 0 {
		path := ctx.Request.URL.Path
		nextpage := &xbmc.ListItem{
			Label:      "LOCALIZE[30415];;" + strconv.Itoa(page+1),
			Path:       URLForXBMC(fmt.Sprintf("%s?page=%d", path, page+1)),
			Thumbnail:  config.AddonResource("img", "nextpage.png"),
			Properties: &xbmc.ListItemProperties{SpecialSort: "bottom"},
		}
		items = append(items, nextpage)
	}
	ctx.JSON(200, xbmc.NewView("tvshows", items))
}

// TraktPopularShows ...
func TraktPopularShows(ctx *gin.Context) {
	defer perf.ScopeTimer()()

	xbmcHost, _ := xbmc.GetXBMCHostWithContext(ctx)

	pageParam := ctx.DefaultQuery("page", "1")
	page, _ := strconv.Atoi(pageParam)
	shows, total, err := trakt.TopShows("popular", pageParam)

	if err != nil {
		xbmcHost.Notify("Elementum", err.Error(), config.AddonIcon())
	}
	renderTraktShows(ctx, shows, total, page)
}

// TraktRecommendationsShows ...
func TraktRecommendationsShows(ctx *gin.Context) {
	defer perf.ScopeTimer()()

	xbmcHost, _ := xbmc.GetXBMCHostWithContext(ctx)

	page := config.Get().ResultsPerPage * -5
	pageParam := strconv.Itoa(page)
	shows, total, err := trakt.TopShows("recommendations", pageParam)

	if err != nil {
		xbmcHost.Notify("Elementum", err.Error(), config.AddonIcon())
	}
	renderTraktShows(ctx, shows, total, page)
}

// TraktTrendingShows ...
func TraktTrendingShows(ctx *gin.Context) {
	defer perf.ScopeTimer()()

	xbmcHost, _ := xbmc.GetXBMCHostWithContext(ctx)

	pageParam := ctx.DefaultQuery("page", "1")
	page, _ := strconv.Atoi(pageParam)
	shows, total, err := trakt.TopShows("trending", pageParam)

	if err != nil {
		xbmcHost.Notify("Elementum", err.Error(), config.AddonIcon())
	}
	renderTraktShows(ctx, shows, total, page)
}

// TraktMostPlayedShows ...
func TraktMostPlayedShows(ctx *gin.Context) {
	defer perf.ScopeTimer()()

	xbmcHost, _ := xbmc.GetXBMCHostWithContext(ctx)

	pageParam := ctx.DefaultQuery("page", "1")
	page, _ := strconv.Atoi(pageParam)
	shows, total, err := trakt.TopShows("played", pageParam)

	if err != nil {
		xbmcHost.Notify("Elementum", err.Error(), config.AddonIcon())
	}
	renderTraktShows(ctx, shows, total, page)
}

// TraktMostWatchedShows ...
func TraktMostWatchedShows(ctx *gin.Context) {
	defer perf.ScopeTimer()()

	xbmcHost, _ := xbmc.GetXBMCHostWithContext(ctx)

	pageParam := ctx.DefaultQuery("page", "1")
	page, _ := strconv.Atoi(pageParam)
	shows, total, err := trakt.TopShows("watched", pageParam)

	if err != nil {
		xbmcHost.Notify("Elementum", err.Error(), config.AddonIcon())
	}
	renderTraktShows(ctx, shows, total, page)
}

// TraktMostCollectedShows ...
func TraktMostCollectedShows(ctx *gin.Context) {
	defer perf.ScopeTimer()()

	xbmcHost, _ := xbmc.GetXBMCHostWithContext(ctx)

	pageParam := ctx.DefaultQuery("page", "1")
	page, _ := strconv.Atoi(pageParam)
	shows, total, err := trakt.TopShows("collected", pageParam)

	if err != nil {
		xbmcHost.Notify("Elementum", err.Error(), config.AddonIcon())
	}
	renderTraktShows(ctx, shows, total, page)
}

// TraktMostAnticipatedShows ...
func TraktMostAnticipatedShows(ctx *gin.Context) {
	defer perf.ScopeTimer()()

	xbmcHost, _ := xbmc.GetXBMCHostWithContext(ctx)

	pageParam := ctx.DefaultQuery("page", "1")
	page, _ := strconv.Atoi(pageParam)
	shows, total, err := trakt.TopShows("anticipated", pageParam)

	if err != nil {
		xbmcHost.Notify("Elementum", err.Error(), config.AddonIcon())
	}
	renderTraktShows(ctx, shows, total, page)
}

//
// Calendars
//

// TraktMyShows ...
func TraktMyShows(ctx *gin.Context) {
	defer perf.ScopeTimer()()

	xbmcHost, _ := xbmc.GetXBMCHostWithContext(ctx)

	pageParam := ctx.DefaultQuery("page", "1")
	page, _ := strconv.Atoi(pageParam)

	activities, err := trakt.GetActivities("MyShows")

	isUpdateNeeded := err != nil ||
		activities.ShowsWatchlisted() ||
		activities.EpisodesWatchlisted() ||
		activities.EpisodesCollected() ||
		activities.EpisodesWatched()

	shows, total, err := trakt.CalendarShows("my/shows", pageParam, cache.TraktShowsCalendarMyExpire, isUpdateNeeded)

	if err != nil {
		xbmcHost.Notify("Elementum", err.Error(), config.AddonIcon())
	}
	renderCalendarShows(ctx, shows, total, page)
}

// TraktMyNewShows ...
func TraktMyNewShows(ctx *gin.Context) {
	defer perf.ScopeTimer()()

	xbmcHost, _ := xbmc.GetXBMCHostWithContext(ctx)

	pageParam := ctx.DefaultQuery("page", "1")
	page, _ := strconv.Atoi(pageParam)

	activities, err := trakt.GetActivities("MyNewShows")

	isUpdateNeeded := err != nil ||
		activities.ShowsWatchlisted() ||
		activities.EpisodesWatchlisted() ||
		activities.EpisodesCollected() ||
		activities.EpisodesWatched()

	shows, total, err := trakt.CalendarShows("my/shows/new", pageParam, cache.TraktShowsCalendarMyExpire, isUpdateNeeded)

	if err != nil {
		xbmcHost.Notify("Elementum", err.Error(), config.AddonIcon())
	}
	renderCalendarShows(ctx, shows, total, page)
}

// TraktMyPremieres ...
func TraktMyPremieres(ctx *gin.Context) {
	defer perf.ScopeTimer()()

	xbmcHost, _ := xbmc.GetXBMCHostWithContext(ctx)

	pageParam := ctx.DefaultQuery("page", "1")
	page, _ := strconv.Atoi(pageParam)

	activities, err := trakt.GetActivities("MyPremieres")

	isUpdateNeeded := err != nil ||
		activities.ShowsWatchlisted() ||
		activities.EpisodesWatchlisted() ||
		activities.EpisodesCollected() ||
		activities.EpisodesWatched()

	shows, total, err := trakt.CalendarShows("my/shows/premieres", pageParam, cache.TraktShowsCalendarMyExpire, isUpdateNeeded)

	if err != nil {
		xbmcHost.Notify("Elementum", err.Error(), config.AddonIcon())
	}
	renderCalendarShows(ctx, shows, total, page)
}

// TraktMyMovies ...
func TraktMyMovies(ctx *gin.Context) {
	defer perf.ScopeTimer()()

	xbmcHost, _ := xbmc.GetXBMCHostWithContext(ctx)

	pageParam := ctx.DefaultQuery("page", "1")
	page, _ := strconv.Atoi(pageParam)

	activities, err := trakt.GetActivities("MyMovies")

	isUpdateNeeded := err != nil ||
		activities.MoviesWatchlisted() ||
		activities.MoviesCollected()

	movies, total, err := trakt.CalendarMovies("my/movies", pageParam, cache.TraktMoviesCalendarMyExpire, isUpdateNeeded)

	if err != nil {
		xbmcHost.Notify("Elementum", err.Error(), config.AddonIcon())
	}
	renderCalendarMovies(ctx, movies, total, page, false)
}

// TraktMyReleases ...
func TraktMyReleases(ctx *gin.Context) {
	defer perf.ScopeTimer()()

	xbmcHost, _ := xbmc.GetXBMCHostWithContext(ctx)

	pageParam := ctx.DefaultQuery("page", "1")
	page, _ := strconv.Atoi(pageParam)

	activities, err := trakt.GetActivities("MyReleases")

	isUpdateNeeded := err != nil ||
		activities.MoviesWatchlisted() ||
		activities.MoviesCollected()

	movies, total, err := trakt.CalendarMovies("my/dvd", pageParam, cache.TraktMoviesCalendarMyExpire, isUpdateNeeded)

	if err != nil {
		xbmcHost.Notify("Elementum", err.Error(), config.AddonIcon())
	}
	renderCalendarMovies(ctx, movies, total, page, true)
}

// TraktAllShows ...
func TraktAllShows(ctx *gin.Context) {
	defer perf.ScopeTimer()()

	xbmcHost, _ := xbmc.GetXBMCHostWithContext(ctx)

	pageParam := ctx.DefaultQuery("page", "1")
	page, _ := strconv.Atoi(pageParam)
	shows, total, err := trakt.CalendarShows("all/shows", pageParam, cache.TraktShowsCalendarAllExpire, false)

	if err != nil {
		xbmcHost.Notify("Elementum", err.Error(), config.AddonIcon())
	}
	renderCalendarShows(ctx, shows, total, page)
}

// TraktAllNewShows ...
func TraktAllNewShows(ctx *gin.Context) {
	defer perf.ScopeTimer()()

	xbmcHost, _ := xbmc.GetXBMCHostWithContext(ctx)

	pageParam := ctx.DefaultQuery("page", "1")
	page, _ := strconv.Atoi(pageParam)
	shows, total, err := trakt.CalendarShows("all/shows/new", pageParam, cache.TraktShowsCalendarAllExpire, false)

	if err != nil {
		xbmcHost.Notify("Elementum", err.Error(), config.AddonIcon())
	}
	renderCalendarShows(ctx, shows, total, page)
}

// TraktAllPremieres ...
func TraktAllPremieres(ctx *gin.Context) {
	defer perf.ScopeTimer()()

	xbmcHost, _ := xbmc.GetXBMCHostWithContext(ctx)

	pageParam := ctx.DefaultQuery("page", "1")
	page, _ := strconv.Atoi(pageParam)
	shows, total, err := trakt.CalendarShows("all/shows/premieres", pageParam, cache.TraktShowsCalendarAllExpire, false)

	if err != nil {
		xbmcHost.Notify("Elementum", err.Error(), config.AddonIcon())
	}
	renderCalendarShows(ctx, shows, total, page)
}

// TraktAllMovies ...
func TraktAllMovies(ctx *gin.Context) {
	defer perf.ScopeTimer()()

	xbmcHost, _ := xbmc.GetXBMCHostWithContext(ctx)

	pageParam := ctx.DefaultQuery("page", "1")
	page, _ := strconv.Atoi(pageParam)
	movies, total, err := trakt.CalendarMovies("all/movies", pageParam, cache.TraktMoviesCalendarAllExpire, false)

	if err != nil {
		xbmcHost.Notify("Elementum", err.Error(), config.AddonIcon())
	}
	renderCalendarMovies(ctx, movies, total, page, false)
}

// TraktAllReleases ...
func TraktAllReleases(ctx *gin.Context) {
	defer perf.ScopeTimer()()

	xbmcHost, _ := xbmc.GetXBMCHostWithContext(ctx)

	pageParam := ctx.DefaultQuery("page", "1")
	page, _ := strconv.Atoi(pageParam)
	movies, total, err := trakt.CalendarMovies("all/dvd", pageParam, cache.TraktMoviesCalendarAllExpire, false)

	if err != nil {
		xbmcHost.Notify("Elementum", err.Error(), config.AddonIcon())
	}
	renderCalendarMovies(ctx, movies, total, page, true)
}

func renderCalendarMovies(ctx *gin.Context, movies []*trakt.CalendarMovie, total int, page int, isDVD bool) {
	hasNextPage := 0
	if page > 0 {
		resultsPerPage := config.Get().ResultsPerPage

		if total == -1 {
			total = len(movies)
		}
		if total > resultsPerPage {
			if page*resultsPerPage < total {
				hasNextPage = 1
			}
		}

		if len(movies) > resultsPerPage {
			start := (page - 1) * resultsPerPage
			end := start + resultsPerPage
			if len(movies) <= end {
				movies = movies[start:]
			} else {
				movies = movies[start:end]
			}
		}
	}

	colorDate := config.Get().TraktCalendarsColorDate
	colorShow := config.Get().TraktCalendarsColorShow
	dateFormat := getCalendarsDateFormat()

	now := util.UTCBod()
	items := make(xbmc.ListItems, len(movies)+hasNextPage)

	wg := sync.WaitGroup{}
	wg.Add(len(movies))

	for i, m := range movies {
		go func(i int, movieListing *trakt.CalendarMovie) {
			defer wg.Done()

			if movieListing == nil || movieListing.Movie == nil {
				return
			}

			airDateFormat := time.DateOnly

			airDate := movieListing.Movie.Released
			if isDVD {
				airDate = movieListing.Released
			}

			if len(airDate) > 10 && strings.Contains(airDate, "T") {
				airDateFormat = time.RFC3339
			}

			aired, _ := time.Parse(airDateFormat, airDate)
			// hide expired cached items
			if aired.Before(now) {
				return
			}

			item := movieListing.Movie.ToListItem()
			if item == nil {
				return
			}

			if config.Get().TraktCalendarsHideWatched && item.Info.PlayCount == 0 {
				return
			}

			label := fmt.Sprintf(`[COLOR %s]%s[/COLOR] | [B][COLOR %s]%s[/COLOR][/B] `,
				colorDate, aired.Format(dateFormat), colorShow, item.Label)
			item.Label = label
			item.Info.Title = label

			tmdbID := strconv.Itoa(movieListing.Movie.IDs.TMDB)

			thisURL := URLForXBMC("/movie/%d/", movieListing.Movie.IDs.TMDB) + "%s/%s"

			contextLabel := playLabel
			contextTitle := fmt.Sprintf("%s (%d)", item.Info.OriginalTitle, movieListing.Movie.Year)
			contextURL := contextPlayOppositeURL(thisURL, contextTitle, false)
			if config.Get().ChooseStreamAutoMovie {
				contextLabel = linksLabel
			}

			item.Path = contextPlayURL(thisURL, contextTitle, false)

			libraryActions := [][]string{
				{contextLabel, fmt.Sprintf("PlayMedia(%s)", contextURL)},
			}
			if uid.IsDuplicateMovie(tmdbID) || uid.IsAddedToLibrary(tmdbID, library.MovieType) {
				libraryActions = append(libraryActions, []string{"LOCALIZE[30283]", fmt.Sprintf("RunPlugin(%s)", URLForXBMC("/library/movie/add/%d?force=true", movieListing.Movie.IDs.TMDB))})
				libraryActions = append(libraryActions, []string{"LOCALIZE[30253]", fmt.Sprintf("RunPlugin(%s)", URLForXBMC("/library/movie/remove/%d", movieListing.Movie.IDs.TMDB))})
			} else {
				libraryActions = append(libraryActions, []string{"LOCALIZE[30252]", fmt.Sprintf("RunPlugin(%s)", URLForXBMC("/library/movie/add/%d", movieListing.Movie.IDs.TMDB))})
			}

			toggleWatchedAction := []string{"LOCALIZE[30667]", fmt.Sprintf("RunPlugin(%s)", URLForXBMC("/movie/%d/watched", movieListing.Movie.IDs.TMDB))}
			if item.Info.PlayCount > 0 {
				toggleWatchedAction = []string{"LOCALIZE[30668]", fmt.Sprintf("RunPlugin(%s)", URLForXBMC("/movie/%d/unwatched", movieListing.Movie.IDs.TMDB))}
			}

			watchlistAction := []string{"LOCALIZE[30255]", fmt.Sprintf("RunPlugin(%s)", URLForXBMC("/movie/%d/watchlist/add", movieListing.Movie.IDs.TMDB))}
			if inMoviesWatchlist(movieListing.Movie.IDs.TMDB) {
				watchlistAction = []string{"LOCALIZE[30256]", fmt.Sprintf("RunPlugin(%s)", URLForXBMC("/movie/%d/watchlist/remove", movieListing.Movie.IDs.TMDB))}
			}

			collectionAction := []string{"LOCALIZE[30258]", fmt.Sprintf("RunPlugin(%s)", URLForXBMC("/movie/%d/collection/add", movieListing.Movie.IDs.TMDB))}
			if inMoviesCollection(movieListing.Movie.IDs.TMDB) {
				collectionAction = []string{"LOCALIZE[30259]", fmt.Sprintf("RunPlugin(%s)", URLForXBMC("/movie/%d/collection/remove", movieListing.Movie.IDs.TMDB))}
			}

			item.ContextMenu = [][]string{
				{"LOCALIZE[30619];;LOCALIZE[30214]", fmt.Sprintf("Container.Update(%s)", URLForXBMC("/movies/"))},
				toggleWatchedAction,
				watchlistAction,
				collectionAction,
				{"LOCALIZE[30034]", fmt.Sprintf("RunPlugin(%s)", URLForXBMC("/setviewmode/movies"))},
			}
			item.ContextMenu = append(libraryActions, item.ContextMenu...)

			if config.Get().Platform.Kodi < 17 {
				item.ContextMenu = append(item.ContextMenu,
					[]string{"LOCALIZE[30203]", "Action(Info)"},
					[]string{"LOCALIZE[30268]", "Action(ToggleWatched)"},
				)
			}

			item.IsPlayable = true
			items = append(items, item)
		}(i, m)
	}
	wg.Wait()

	for i := len(items) - 1; i >= 0; i-- {
		if items[i] == nil {
			items = append(items[:i], items[i+1:]...)
		}
	}

	if page >= 0 && hasNextPage > 0 {
		path := ctx.Request.URL.Path
		nextpage := &xbmc.ListItem{
			Label:      "LOCALIZE[30415];;" + strconv.Itoa(page+1),
			Path:       URLForXBMC(fmt.Sprintf("%s?page=%d", path, page+1)),
			Thumbnail:  config.AddonResource("img", "nextpage.png"),
			Properties: &xbmc.ListItemProperties{SpecialSort: "bottom"},
		}
		items = append(items, nextpage)
	}
	ctx.JSON(200, xbmc.NewView("movies", items))
}

func renderCalendarShows(ctx *gin.Context, shows []*trakt.CalendarShow, total int, page int) {
	hasNextPage := 0
	if page > 0 {
		resultsPerPage := config.Get().ResultsPerPage

		if total == -1 {
			total = len(shows)
		}
		if total > resultsPerPage {
			if page*resultsPerPage < total {
				hasNextPage = 1
			}
		}

		if len(shows) > resultsPerPage {
			start := (page - 1) * resultsPerPage
			end := start + resultsPerPage
			if len(shows) <= end {
				shows = shows[start:]
			} else {
				shows = shows[start:end]
			}
		}
	}

	language := config.Get().Language
	colorDate := config.Get().TraktCalendarsColorDate
	colorShow := config.Get().TraktCalendarsColorShow
	colorEpisode := config.Get().TraktCalendarsColorEpisode
	colorUnaired := config.Get().TraktCalendarsColorUnaired
	dateFormat := getCalendarsDateFormat()

	now := util.UTCBod()
	items := make(xbmc.ListItems, len(shows)+hasNextPage)

	wg := sync.WaitGroup{}
	wg.Add(len(shows))

	for i, s := range shows {
		go func(i int, showListing *trakt.CalendarShow) {
			defer wg.Done()
			if showListing == nil || showListing.Episode == nil {
				return
			}

			tmdbID := strconv.Itoa(showListing.Show.IDs.TMDB)
			epi := showListing.Episode
			airDate := epi.FirstAired
			seasonNumber := epi.Season
			episodeNumber := epi.Number
			episodeName := epi.Title
			showName := showListing.Show.Title
			showOriginalName := showListing.Show.Title
			airDateFormat := time.RFC3339

			var episode *tmdb.Episode
			var season *tmdb.Season
			var show *tmdb.Show

			if !config.Get().ForceUseTrakt && showListing.Show.IDs.TMDB != 0 {
				show = tmdb.GetShow(showListing.Show.IDs.TMDB, language)
				seasonsCount := 0
				if show != nil {
					seasonsCount = len(show.Seasons)
				}
				season = tmdb.GetSeason(showListing.Show.IDs.TMDB, epi.Season, language, seasonsCount, false)
				episode = tmdb.GetEpisode(showListing.Show.IDs.TMDB, epi.Season, epi.Number, language)

				if episode != nil {
					airDate, airDateFormat = episode.GetLowestAirDate(airDate, airDateFormat)

					seasonNumber = episode.SeasonNumber
					episodeNumber = episode.EpisodeNumber

					if show != nil {
						episodeName = episode.GetName(show)
					} else {
						episodeName = episode.Name
					}
				}
				if show != nil {
					showName = show.GetName()
					showOriginalName = show.OriginalName
				}
			}
			if airDate == "" {
				episodes := trakt.GetSeasonEpisodes(showListing.Show.IDs.Trakt, seasonNumber)
				for _, e := range episodes {
					if e != nil && e.Number == epi.Number {
						airDate = e.FirstAired
						airDateFormat = time.RFC3339
						break
					}
				}
			}

			aired, isAired := util.AirDateWithAiredCheck(airDate, airDateFormat, config.Get().ShowEpisodesOnReleaseDay)
			// hide expired cached items
			if aired.Before(now) {
				return
			}
			localEpisodeColor := colorEpisode
			if !isAired {
				localEpisodeColor = colorUnaired
			}

			var item *xbmc.ListItem
			if show != nil && season != nil && episode != nil {
				item = episode.ToListItem(show, season)
			} else {
				item = epi.ToListItem(showListing.Show, show)
			}
			if item == nil {
				return
			}

			if config.Get().TraktCalendarsHideWatched && item.Info.PlayCount == 0 {
				return
			}

			item.Info.Aired = airDate
			item.Info.DateAdded = airDate
			item.Info.Premiered = airDate
			item.Info.LastPlayed = airDate

			episodeLabel := fmt.Sprintf(`[COLOR %s]%s[/COLOR] | [B][COLOR %s]%s[/COLOR][/B] - [I][COLOR %s]%dx%02d %s[/COLOR][/I]`,
				colorDate, aired.Format(dateFormat), colorShow, showName, localEpisodeColor, seasonNumber, episodeNumber, episodeName)
			item.Label = episodeLabel
			item.Info.Title = episodeLabel

			itemPath := URLQuery(URLForXBMC("/search"), "q", fmt.Sprintf("%s S%02dE%02d", showOriginalName, epi.Season, epi.Number))
			if epi.Season > 100 {
				itemPath = URLQuery(URLForXBMC("/search"), "q", fmt.Sprintf("%s %d %d", showOriginalName, epi.Number, epi.Season))
			}
			item.Path = itemPath

			// TODO: calendar show episodes, but libraryActions/watchlistAction/collectionAction are for shows, which might be confusing
			libraryActions := [][]string{}
			if uid.IsDuplicateShow(tmdbID) || uid.IsAddedToLibrary(tmdbID, library.ShowType) {
				libraryActions = append(libraryActions, []string{"LOCALIZE[30283]", fmt.Sprintf("RunPlugin(%s)", URLForXBMC("/library/show/add/%d?force=true", showListing.Show.IDs.TMDB))})
				libraryActions = append(libraryActions, []string{"LOCALIZE[30253]", fmt.Sprintf("RunPlugin(%s)", URLForXBMC("/library/show/remove/%d", showListing.Show.IDs.TMDB))})
			} else {
				libraryActions = append(libraryActions, []string{"LOCALIZE[30252]", fmt.Sprintf("RunPlugin(%s)", URLForXBMC("/library/show/add/%d", showListing.Show.IDs.TMDB))})
			}

			toggleWatchedAction := []string{"LOCALIZE[30667]", fmt.Sprintf("RunPlugin(%s)", URLForXBMC("/show/%d/season/%d/episode/%d/watched", showListing.Show.IDs.TMDB, epi.Number, epi.Season))}
			if item.Info.PlayCount > 0 {
				toggleWatchedAction = []string{"LOCALIZE[30668]", fmt.Sprintf("RunPlugin(%s)", URLForXBMC("/show/%d/season/%d/episode/%d/unwatched", showListing.Show.IDs.TMDB, epi.Number, epi.Season))}
			}

			watchlistAction := []string{"LOCALIZE[30255]", fmt.Sprintf("RunPlugin(%s)", URLForXBMC("/show/%d/watchlist/add", showListing.Show.IDs.TMDB))}
			if inShowsWatchlist(showListing.Show.IDs.TMDB) {
				watchlistAction = []string{"LOCALIZE[30256]", fmt.Sprintf("RunPlugin(%s)", URLForXBMC("/show/%d/watchlist/remove", showListing.Show.IDs.TMDB))}
			}

			collectionAction := []string{"LOCALIZE[30258]", fmt.Sprintf("RunPlugin(%s)", URLForXBMC("/show/%d/collection/add", showListing.Show.IDs.TMDB))}
			if inShowsCollection(showListing.Show.IDs.TMDB) {
				collectionAction = []string{"LOCALIZE[30259]", fmt.Sprintf("RunPlugin(%s)", URLForXBMC("/show/%d/collection/remove", showListing.Show.IDs.TMDB))}
			}

			item.ContextMenu = [][]string{
				{"LOCALIZE[30619];;LOCALIZE[30215]", fmt.Sprintf("Container.Update(%s)", URLForXBMC("/shows/"))},
				toggleWatchedAction,
				watchlistAction,
				collectionAction,
				{"LOCALIZE[30035]", fmt.Sprintf("RunPlugin(%s)", URLForXBMC("/setviewmode/tvshows"))},
			}
			item.ContextMenu = append(libraryActions, item.ContextMenu...)

			if config.Get().Platform.Kodi < 17 {
				item.ContextMenu = append(item.ContextMenu,
					[]string{"LOCALIZE[30203]", "Action(Info)"},
					[]string{"LOCALIZE[30268]", "Action(ToggleWatched)"},
				)
			}

			item.IsPlayable = true

			items[i] = item
		}(i, s)
	}
	wg.Wait()

	for i := len(items) - 1; i >= 0; i-- {
		if items[i] == nil {
			items = append(items[:i], items[i+1:]...)
		}
	}

	if page >= 0 && hasNextPage > 0 {
		path := ctx.Request.URL.Path
		nextpage := &xbmc.ListItem{
			Label:      "LOCALIZE[30415];;" + strconv.Itoa(page+1),
			Path:       URLForXBMC(fmt.Sprintf("%s?page=%d", path, page+1)),
			Thumbnail:  config.AddonResource("img", "nextpage.png"),
			Properties: &xbmc.ListItemProperties{SpecialSort: "bottom"},
		}
		items = append(items, nextpage)
	}
	ctx.JSON(200, xbmc.NewView("episodes", items))
}

func renderProgressShows(ctx *gin.Context, shows []*trakt.ProgressShow, total int, page int) {
	language := config.Get().Language

	colorDate := config.Get().TraktProgressColorDate
	colorShow := config.Get().TraktProgressColorShow
	colorEpisode := config.Get().TraktProgressColorEpisode
	colorUnaired := config.Get().TraktProgressColorUnaired
	dateFormat := getProgressDateFormat()

	items := make(xbmc.ListItems, len(shows))

	wg := sync.WaitGroup{}
	wg.Add(len(shows))
	for i, s := range shows {
		go func(i int, showListing *trakt.ProgressShow) {
			defer wg.Done()
			if showListing == nil && showListing.Episode == nil {
				return
			}

			epi := showListing.Episode
			airDate := epi.FirstAired
			seasonNumber := epi.Season
			episodeNumber := epi.Number
			episodeName := epi.Title
			showName := showListing.Show.Title
			airDateFormat := time.RFC3339

			var episode *tmdb.Episode
			var season *tmdb.Season
			var show *tmdb.Show

			if !config.Get().ForceUseTrakt && showListing.Show.IDs.TMDB != 0 {
				show = tmdb.GetShow(showListing.Show.IDs.TMDB, language)
				if show != nil {
					season = tmdb.GetSeason(showListing.Show.IDs.TMDB, epi.Season, language, len(show.Seasons), false)
					episode = tmdb.GetEpisode(showListing.Show.IDs.TMDB, epi.Season, epi.Number, language)
				}

				if episode != nil {
					airDate, airDateFormat = episode.GetLowestAirDate(airDate, airDateFormat)

					seasonNumber = episode.SeasonNumber
					episodeNumber = episode.EpisodeNumber

					if show != nil {
						episodeName = episode.GetName(show)
					} else {
						episodeName = episode.Name
					}
				}
				if show != nil {
					showName = show.GetName()
				}
			}
			if airDate == "" {
				episodes := trakt.GetSeasonEpisodes(showListing.Show.IDs.Trakt, seasonNumber)
				for _, e := range episodes {
					if e != nil && e.Number == epi.Number {
						airDate = e.FirstAired
						airDateFormat = time.RFC3339
						break
					}
				}
			}

			aired, isAired := util.AirDateWithAiredCheck(airDate, airDateFormat, config.Get().ShowEpisodesOnReleaseDay)
			if config.Get().TraktProgressHideUnaired && !isAired {
				return
			}

			localEpisodeColor := colorEpisode
			if !isAired {
				localEpisodeColor = colorUnaired
			}

			var item *xbmc.ListItem
			if show != nil && season != nil && episode != nil {
				item = episode.ToListItem(show, season)
			} else {
				item = epi.ToListItem(showListing.Show, show)
			}
			if item == nil {
				return
			}

			item.Info.Aired = airDate
			item.Info.DateAdded = airDate
			item.Info.Premiered = airDate
			item.Info.LastPlayed = airDate

			episodeLabel := fmt.Sprintf(`[COLOR %s]%s[/COLOR] | [B][COLOR %s]%s[/COLOR][/B] - [I][COLOR %s]%dx%02d %s[/COLOR][/I]`,
				colorDate, aired.Format(dateFormat), colorShow, showName, localEpisodeColor, seasonNumber, episodeNumber, episodeName)
			item.Label = episodeLabel
			item.Info.Title = episodeLabel

			thisURL := URLForXBMC("/show/%d/season/%d/episode/%d/",
				showListing.Show.IDs.TMDB,
				seasonNumber,
				episodeNumber,
			) + "%s/%s"

			contextLabel := playLabel
			contextTitle := fmt.Sprintf("%s S%02dE%02d", showListing.Show.Title, seasonNumber, episodeNumber)
			contextURL := contextPlayOppositeURL(thisURL, contextTitle, false)
			if config.Get().ChooseStreamAutoShow {
				contextLabel = linksLabel
			}

			item.Path = contextPlayURL(thisURL, contextTitle, false)

			libraryActions := [][]string{
				{contextLabel, fmt.Sprintf("PlayMedia(%s)", contextURL)},
			}

			toggleWatchedAction := []string{"LOCALIZE[30667]", fmt.Sprintf("RunPlugin(%s)", URLForXBMC("/show/%d/season/%d/episode/%d/watched", showListing.Show.IDs.TMDB, seasonNumber, episodeNumber))}
			if item.Info.PlayCount > 0 {
				toggleWatchedAction = []string{"LOCALIZE[30668]", fmt.Sprintf("RunPlugin(%s)", URLForXBMC("/show/%d/season/%d/episode/%d/unwatched", showListing.Show.IDs.TMDB, seasonNumber, episodeNumber))}
			}

			item.ContextMenu = [][]string{
				toggleWatchedAction,
				{"LOCALIZE[30037]", fmt.Sprintf("RunPlugin(%s)", URLForXBMC("/setviewmode/episodes"))},
			}
			item.ContextMenu = append(libraryActions, item.ContextMenu...)

			if config.Get().Platform.Kodi < 17 {
				item.ContextMenu = append(item.ContextMenu,
					[]string{"LOCALIZE[30203]", "Action(Info)"},
					[]string{"LOCALIZE[30268]", "Action(ToggleWatched)"})
			}
			item.IsPlayable = true
			items[i] = item
		}(i, s)
	}
	wg.Wait()

	for i := len(items) - 1; i >= 0; i-- {
		if items[i] == nil {
			items = append(items[:i], items[i+1:]...)
		}
	}

	if config.Get().TraktProgressSort == trakt.ProgressSortShow {
		sort.Slice(items, func(i, j int) bool {
			return items[i].Info.TVShowTitle < items[j].Info.TVShowTitle
		})
	} else if config.Get().TraktProgressSort == trakt.ProgressSortAiredNewer {
		sort.Slice(items, func(i, j int) bool {
			id, _ := time.Parse(time.DateOnly, items[i].Info.Aired)
			jd, _ := time.Parse(time.DateOnly, items[j].Info.Aired)
			return id.After(jd)
		})
	} else if config.Get().TraktProgressSort == trakt.ProgressSortAiredOlder {
		sort.Slice(items, func(i, j int) bool {
			id, _ := time.Parse(time.DateOnly, items[i].Info.Aired)
			jd, _ := time.Parse(time.DateOnly, items[j].Info.Aired)
			return id.Before(jd)
		})
	}

	ctx.JSON(200, xbmc.NewView("episodes", items))
}

// SelectTraktUserList ...
func SelectTraktUserList(ctx *gin.Context) {
	defer perf.ScopeTimer()()

	xbmcHost, _ := xbmc.GetXBMCHostWithContext(ctx)

	action := ctx.Params.ByName("action")
	media := ctx.Params.ByName("media")

	lists := trakt.Userlists()
	items := make([]string, 0, len(lists))

	for _, l := range lists {
		items = append(items, l.Name)
	}
	choice := xbmcHost.ListDialog("LOCALIZE[30438]", items...)
	if choice >= 0 && choice < len(lists) {
		xbmcHost.SetSetting(fmt.Sprintf("trakt_sync_%s_%s_location", action, media), "2")
		xbmcHost.SetSetting(fmt.Sprintf("trakt_sync_%s_%s_list_name", action, media), lists[choice].Name)
		xbmcHost.SetSetting(fmt.Sprintf("trakt_sync_%s_%s_list", action, media), strconv.Itoa(lists[choice].IDs.Trakt))
	}

	ctx.String(200, "")
}

// ToggleWatched mark as watched or unwatched in Trakt and Kodi library
func ToggleWatched(media string, setWatched bool) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		defer perf.ScopeTimer()()

		xbmcHost, _ := xbmc.GetXBMCHostWithContext(ctx)

		var watched *trakt.WatchedItem
		var foundInLibrary bool

		// TODO: Make use of Playcount, possibly increment when Watched, use old value if in progress
		if media == movieType {
			movieID, _ := strconv.Atoi(ctx.Params.ByName("tmdbId"))

			watched = &trakt.WatchedItem{
				MediaType: media,
				Movie:     movieID,
				Watched:   setWatched,
			}

			movie, err := uid.GetMovieByTMDB(movieID)
			if err == nil {
				foundInLibrary = true

				playcount := 1
				if !setWatched {
					playcount = 0
				}

				log.Debugf("Toggle Kodi library watched for: %#v", movie)
				xbmcHost.SetMovieWatched(movie.ID, playcount, 0, 0)
			}
		} else if media == episodeType {
			showID, _ := strconv.Atoi(ctx.Params.ByName("showId"))
			seasonNumber, _ := strconv.Atoi(ctx.Params.ByName("season"))
			episodeNumber, _ := strconv.Atoi(ctx.Params.ByName("episode"))

			watched = &trakt.WatchedItem{
				MediaType: media,
				Show:      showID,
				Season:    seasonNumber,
				Episode:   episodeNumber,
				Watched:   setWatched,
			}

			show, err := uid.GetShowByTMDB(showID)
			if err == nil {
				foundInLibrary = true

				playcount := 1
				if !setWatched {
					playcount = 0
				}

				episode := show.GetEpisode(seasonNumber, episodeNumber)
				log.Debugf("Toggle Kodi library watched for: %#v", episode)
				xbmcHost.SetEpisodeWatched(episode.ID, playcount, 0, 0)
			}
		} else if media == seasonType {
			showID, _ := strconv.Atoi(ctx.Params.ByName("showId"))
			seasonNumber, _ := strconv.Atoi(ctx.Params.ByName("season"))

			watched = &trakt.WatchedItem{
				MediaType: media,
				Show:      showID,
				Season:    seasonNumber,
				Episode:   0,
				Watched:   setWatched,
			}

			show, err := uid.GetShowByTMDB(showID)
			if err == nil {
				foundInLibrary = true

				playcount := 1
				if !setWatched {
					playcount = 0
				}

				season := show.GetSeason(seasonNumber)
				log.Debugf("Set Kodi library watched to %t for: %#v", setWatched, season)
				xbmcHost.SetSeasonWatched(season.ID, playcount)
			}
		} else if media == showType {
			showID, _ := strconv.Atoi(ctx.Params.ByName("showId"))

			watched = &trakt.WatchedItem{
				MediaType: media,
				Show:      showID,
				Season:    0,
				Episode:   0,
				Watched:   setWatched,
			}

			show, err := uid.GetShowByTMDB(showID)
			if err == nil {
				foundInLibrary = true

				playcount := 1
				if !setWatched {
					playcount = 0
				}

				log.Debugf("Toggle Kodi library watched for: %#v", show)
				xbmcHost.SetShowWatched(show.ID, playcount)
			}
		}

		if config.Get().TraktToken != "" && watched != nil {
			log.Debugf("Set Trakt watched to %t for: %#v", setWatched, watched)
			go trakt.SetWatched(watched)
		}

		if !foundInLibrary {
			xbmcHost.ToggleWatched()
		}
	}
}

func getProgressDateFormat() string {
	return prepareDateFormat(config.Get().TraktProgressDateFormat)
}

func getCalendarsDateFormat() string {
	return prepareDateFormat(config.Get().TraktCalendarsDateFormat)
}

func prepareDateFormat(f string) string {
	f = strings.ToLower(f)
	f = strings.Replace(f, "yyyy", "2006", -1)
	f = strings.Replace(f, "yy", "06", -1)
	f = strings.Replace(f, "y", "6", -1)
	f = strings.Replace(f, "mm", "01", -1)
	f = strings.Replace(f, "m", "1", -1)
	f = strings.Replace(f, "dd", "02", -1)
	f = strings.Replace(f, "d", "2", -1)

	return f
}

func nextPage(page string) string {
	p, _ := strconv.Atoi(page)
	return strconv.Itoa(p + 1)
}

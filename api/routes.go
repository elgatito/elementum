package api

import (
	"fmt"
	"path"
	"time"
	"net/url"
	"net/http"
	"path/filepath"

	"github.com/gin-gonic/gin"
	"github.com/scakemyer/quasar/util"
	"github.com/scakemyer/quasar/cache"
	"github.com/scakemyer/quasar/config"
	"github.com/scakemyer/quasar/providers"
	"github.com/scakemyer/quasar/bittorrent"
	"github.com/scakemyer/quasar/api/repository"
)

const (
	DefaultCacheExpiration    = 6 * time.Hour
	RecentCacheExpiration     = 5 * time.Minute
	RepositoryCacheExpiration = 20 * time.Minute
	IndexCacheExpiration      = 15 * 24 * time.Hour // 15 days caching for index
)

func Routes(btService *bittorrent.BTService) *gin.Engine {
	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(gin.LoggerWithWriter(gin.DefaultWriter, "/torrents/list", "/notification"))

	gin.SetMode(gin.ReleaseMode)

	store := cache.NewFileStore(path.Join(config.Get().ProfilePath, "cache"))

	r.GET("/", Index)
	r.GET("/search", Search(btService))
	r.GET("/playtorrent", PlayTorrent)
	r.GET("/infolabels", InfoLabelsStored(btService))

	r.LoadHTMLGlob(filepath.Join(config.Get().Info.Path, "resources", "web", "*.html"))
	web := r.Group("/web")
	{
		web.GET("/", func(c *gin.Context) {
			c.HTML(http.StatusOK, "index.html", nil)
		})
	  web.Static("/static", filepath.Join(config.Get().Info.Path, "resources", "web", "static"))
		web.StaticFile("/favicon.ico", filepath.Join(config.Get().Info.Path, "resources", "web", "favicon.ico"))
	}

	torrents := r.Group("/torrents")
	{
		torrents.GET("/", ListTorrents(btService))
		torrents.GET("/add", AddTorrent(btService))
		torrents.GET("/pause", PauseSession(btService))
		torrents.GET("/resume", ResumeSession(btService))
		torrents.GET("/move/:torrentId", MoveTorrent(btService))
		torrents.GET("/pause/:torrentId", PauseTorrent(btService))
		torrents.GET("/resume/:torrentId", ResumeTorrent(btService))
		torrents.GET("/delete/:torrentId", RemoveTorrent(btService))

		// Web UI json
		torrents.GET("/list", ListTorrentsWeb(btService))
	}

	movies := r.Group("/movies")
	{
		movies.GET("/", cache.Cache(store, IndexCacheExpiration), MoviesIndex)
		movies.GET("/search", SearchMovies)
		movies.GET("/popular", cache.Cache(store, RecentCacheExpiration), PopularMovies)
		movies.GET("/popular/:genre", cache.Cache(store, RecentCacheExpiration), PopularMovies)
		movies.GET("/recent", cache.Cache(store, RecentCacheExpiration), RecentMovies)
		movies.GET("/recent/:genre", cache.Cache(store, RecentCacheExpiration), RecentMovies)
		movies.GET("/top", cache.Cache(store, DefaultCacheExpiration), TopRatedMovies)
		movies.GET("/imdb250", cache.Cache(store, DefaultCacheExpiration), IMDBTop250)
		movies.GET("/mostvoted", cache.Cache(store, DefaultCacheExpiration), MoviesMostVoted)
		movies.GET("/genres", cache.Cache(store, IndexCacheExpiration), MovieGenres)

		trakt := movies.Group("/trakt")
		{
			trakt.GET("/", cache.Cache(store, IndexCacheExpiration), MoviesTrakt)
			trakt.GET("/watchlist", WatchlistMovies)
			trakt.GET("/collection", CollectionMovies)
			trakt.GET("/popular", cache.Cache(store, DefaultCacheExpiration), TraktPopularMovies)
			trakt.GET("/trending", cache.Cache(store, RecentCacheExpiration), TraktTrendingMovies)
			trakt.GET("/played", cache.Cache(store, DefaultCacheExpiration), TraktMostPlayedMovies)
			trakt.GET("/watched", cache.Cache(store, DefaultCacheExpiration), TraktMostWatchedMovies)
			trakt.GET("/collected", cache.Cache(store, DefaultCacheExpiration), TraktMostCollectedMovies)
			trakt.GET("/anticipated", cache.Cache(store, DefaultCacheExpiration), TraktMostAnticipatedMovies)
			trakt.GET("/boxoffice", cache.Cache(store, DefaultCacheExpiration), TraktBoxOffice)

			lists := trakt.Group("/lists")
			{
				lists.GET("/", cache.Cache(store, RecentCacheExpiration), MoviesTraktLists)
				lists.GET("/id/:listId", UserlistMovies)
			}

			calendars := trakt.Group("/calendars")
			{
				calendars.GET("/", cache.Cache(store, IndexCacheExpiration), CalendarMovies)
				calendars.GET("/movies", cache.Cache(store, RecentCacheExpiration), TraktMyMovies)
				calendars.GET("/releases", cache.Cache(store, RecentCacheExpiration), TraktMyReleases)
				calendars.GET("/allmovies", cache.Cache(store, RecentCacheExpiration), TraktAllMovies)
				calendars.GET("/allreleases", cache.Cache(store, RecentCacheExpiration), TraktAllReleases)
			}
		}
	}
	movie := r.Group("/movie")
	{
		movie.GET("/:tmdbId/infolabels", InfoLabelsMovie(btService))
		movie.GET("/:tmdbId/links", MoviePlaySelector("links", btService, false))
		movie.GET("/:tmdbId/play", MoviePlaySelector("play", btService, false))
		movie.GET("/:tmdbId/watchlist/add", AddMovieToWatchlist)
		movie.GET("/:tmdbId/watchlist/remove", RemoveMovieFromWatchlist)
		movie.GET("/:tmdbId/collection/add", AddMovieToCollection)
		movie.GET("/:tmdbId/collection/remove", RemoveMovieFromCollection)
	}

	shows := r.Group("/shows")
	{
		shows.GET("/", cache.Cache(store, IndexCacheExpiration), TVIndex)
		shows.GET("/search", SearchShows)
		shows.GET("/popular", cache.Cache(store, RecentCacheExpiration), PopularShows)
		shows.GET("/popular/:genre", cache.Cache(store, RecentCacheExpiration), PopularShows)
		shows.GET("/recent/shows", cache.Cache(store, DefaultCacheExpiration), RecentShows)
		shows.GET("/recent/shows/:genre", cache.Cache(store, DefaultCacheExpiration), RecentShows)
		shows.GET("/recent/episodes", cache.Cache(store, RecentCacheExpiration), RecentEpisodes)
		shows.GET("/recent/episodes/:genre", cache.Cache(store, RecentCacheExpiration), RecentEpisodes)
		shows.GET("/top", cache.Cache(store, DefaultCacheExpiration), TopRatedShows)
		shows.GET("/mostvoted", cache.Cache(store, DefaultCacheExpiration), TVMostVoted)
		shows.GET("/genres", cache.Cache(store, IndexCacheExpiration), TVGenres)

		trakt := shows.Group("/trakt")
		{
			trakt.GET("/", cache.Cache(store, IndexCacheExpiration), TVTrakt)
			trakt.GET("/watchlist", WatchlistShows)
			trakt.GET("/collection", CollectionShows)
			trakt.GET("/popular", cache.Cache(store, DefaultCacheExpiration), TraktPopularShows)
			trakt.GET("/trending", cache.Cache(store, RecentCacheExpiration), TraktTrendingShows)
			trakt.GET("/played", cache.Cache(store, DefaultCacheExpiration), TraktMostPlayedShows)
			trakt.GET("/watched", cache.Cache(store, DefaultCacheExpiration), TraktMostWatchedShows)
			trakt.GET("/collected", cache.Cache(store, DefaultCacheExpiration), TraktMostCollectedShows)
			trakt.GET("/anticipated", cache.Cache(store, DefaultCacheExpiration), TraktMostAnticipatedShows)

			lists := trakt.Group("/lists")
			{
				lists.GET("/", cache.Cache(store, RecentCacheExpiration), TVTraktLists)
				lists.GET("/id/:listId", UserlistShows)
			}

			calendars := trakt.Group("/calendars")
			{
				calendars.GET("/", cache.Cache(store, IndexCacheExpiration), CalendarShows)
				calendars.GET("/shows", cache.Cache(store, RecentCacheExpiration), TraktMyShows)
				calendars.GET("/newshows", cache.Cache(store, RecentCacheExpiration), TraktMyNewShows)
				calendars.GET("/premieres", cache.Cache(store, RecentCacheExpiration), TraktMyPremieres)
				calendars.GET("/allshows", cache.Cache(store, RecentCacheExpiration), TraktAllShows)
				calendars.GET("/allnewshows", cache.Cache(store, RecentCacheExpiration), TraktAllNewShows)
				calendars.GET("/allpremieres", cache.Cache(store, RecentCacheExpiration), TraktAllPremieres)
			}
		}
	}
	show := r.Group("/show")
	{
		show.GET("/:showId/seasons", cache.Cache(store, DefaultCacheExpiration), ShowSeasons)
		show.GET("/:showId/season/:season/links", ShowSeasonLinks(btService, false))
		show.GET("/:showId/season/:season/episodes", cache.Cache(store, RecentCacheExpiration), ShowEpisodes)
		show.GET("/:showId/season/:season/episode/:episode/infolabels", InfoLabelsEpisode(btService))
		show.GET("/:showId/season/:season/episode/:episode/play", ShowEpisodePlaySelector("play", btService, false))
		show.GET("/:showId/season/:season/episode/:episode/links", ShowEpisodePlaySelector("links", btService, false))
		show.GET("/:showId/watchlist/add", AddShowToWatchlist)
		show.GET("/:showId/watchlist/remove", RemoveShowFromWatchlist)
		show.GET("/:showId/collection/add", AddShowToCollection)
		show.GET("/:showId/collection/remove", RemoveShowFromCollection)
	}
	// TODO
	// episode := r.Group("/episode")
	// {
	// 	episode.GET("/:episodeId/watchlist/add", AddEpisodeToWatchlist)
	// }

	library := r.Group("/library")
	{
		library.GET("/movie/add/:tmdbId", AddMovie)
		library.GET("/movie/remove/:tmdbId", RemoveMovie)
		library.GET("/movie/list/add/:listId", AddMoviesList)
		library.GET("/movie/play/:tmdbId", PlayMovie(btService))
		library.GET("/show/add/:tmdbId", AddShow)
		library.GET("/show/remove/:tmdbId", RemoveShow)
		library.GET("/show/list/add/:listId", AddShowsList)
		library.GET("/show/play/:showId/:season/:episode", PlayShow(btService))

		library.GET("/update", UpdateLibrary)

		// DEPRECATED
		library.GET("/play/movie/:tmdbId", PlayMovie(btService))
		library.GET("/play/show/:showId/season/:season/episode/:episode", PlayShow(btService))
	}

	provider := r.Group("/provider")
	{
		provider.GET("/", ProviderList)
		provider.GET("/:provider/check", ProviderCheck)
		provider.GET("/:provider/enable", ProviderEnable)
		provider.GET("/:provider/disable", ProviderDisable)
		provider.GET("/:provider/failure", ProviderFailure)
		provider.GET("/:provider/settings", ProviderSettings)

		provider.GET("/:provider/movie/:tmdbId", ProviderGetMovie)
		provider.GET("/:provider/show/:showId/season/:season/episode/:episode", ProviderGetEpisode)
	}

	allproviders := r.Group("/providers")
	{
		allproviders.GET("/enable", ProvidersEnableAll)
		allproviders.GET("/disable", ProvidersDisableAll)
	}

	repo := r.Group("/repository")
	{
		repo.GET("/:user/:repository/*filepath", repository.GetAddonFiles)
		repo.HEAD("/:user/:repository/*filepath", repository.GetAddonFilesHead)
	}

	trakt := r.Group("/trakt")
	{
		trakt.GET("/authorize", AuthorizeTrakt)
	}

	r.GET("/setviewmode/:content_type", SetViewMode)

	r.GET("/youtube/:id", PlayYoutubeVideo)

	r.GET("/subtitles", SubtitlesIndex)
	r.GET("/subtitle/:id", SubtitleGet)

	r.GET("/play", Play(btService))
	r.GET("/playuri", PlayURI(btService))

	r.POST("/callbacks/:cid", providers.CallbackHandler)

	r.GET("/notification", Notification(btService))

	r.GET("/versions", Versions(btService))

	cmd := r.Group("/cmd")
	{
		cmd.GET("/clear_cache", ClearCache)
		cmd.GET("/clear_page_cache", ClearPageCache)
		cmd.GET("/reset_clearances", ResetClearances)
	}

	return r
}

func UrlForHTTP(pattern string, args ...interface{}) string {
	u, _ := url.Parse(fmt.Sprintf(pattern, args...))
	return util.GetHTTPHost() + u.String()
}

func UrlForXBMC(pattern string, args ...interface{}) string {
	u, _ := url.Parse(fmt.Sprintf(pattern, args...))
	return "plugin://" + config.Get().Info.Id + u.String()
}

func UrlQuery(route string, query ...string) string {
	v := url.Values{}
	for i := 0; i < len(query); i += 2 {
		v.Add(query[i], query[i+1])
	}
	return route + "?" + v.Encode()
}

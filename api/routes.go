package api

import (
	"net/http"
	"path/filepath"
	"strings"

	"github.com/elgatito/elementum/api/repository"
	"github.com/elgatito/elementum/bittorrent"
	"github.com/elgatito/elementum/config"
	"github.com/elgatito/elementum/providers"
	"github.com/elgatito/elementum/xbmc"

	"github.com/gin-gonic/gin"
	"github.com/op/go-logging"
)

var log = logging.MustGetLogger("api")

// IPLogger records last caller IP to respond to it later (if needed user interaction)
func IPLogger() gin.HandlerFunc {
	return func(c *gin.Context) {
		if ua, ok := c.Request.Header["User-Agent"]; ok && len(ua) > 0 && ua[0] == "plugin.video.elementum" && strings.Contains(c.Request.RemoteAddr, ":") {
			xbmc.LastCallerIP = strings.Split(c.Request.RemoteAddr, ":")[0]
		}
	}
}

// CORS allows all external source to request data from Elementum
func CORS() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Access-Control-Allow-Origin", "*")
		c.Header("Access-Control-Allow-Credentials", "true")
		c.Header("Access-Control-Allow-Headers", "Content-Type, Content-Length, Accept-Encoding, X-CSRF-Token, Authorization, accept, origin, Cache-Control, X-Requested-With")
		c.Header("Access-Control-Allow-Methods", "POST, HEAD, PATCH, OPTIONS, GET, PUT")

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}

		c.Next()
	}
}

// Routes ...
func Routes(s *bittorrent.Service) *gin.Engine {
	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(gin.LoggerWithWriter(gin.DefaultWriter, "/torrents/list", "/notification"))
	r.Use(IPLogger())
	r.Use(CORS())

	gin.SetMode(gin.ReleaseMode)

	r.GET("/", Index(s))
	r.GET("/playtorrent", PlayTorrent)
	r.GET("/infolabels", InfoLabelsStored(s))
	r.GET("/changelog", Changelog)
	r.GET("/donate", Donate)
	r.GET("/settings/:addon", Settings)
	r.GET("/status", Status)

	r.Any("/info", s.ClientInfo)
	r.Any("/info/*ident", s.ClientInfo)

	history := r.Group("/history")
	{
		history.GET("", History)
		history.GET("/", History)
		history.GET("/remove", HistoryRemove)
		history.GET("/clear", HistoryClear)
	}

	search := r.Group("/search")
	{
		search.GET("", Search(s))
		search.GET("/remove", SearchRemove)
		search.GET("/clear", SearchClear)
		search.GET("/infolabels/:tmdbId", InfoLabelsSearch(s))
	}

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
		torrents.GET("/", ListTorrents(s))
		torrents.Any("/add", AddTorrent(s))
		torrents.GET("/pause", PauseSession(s))
		torrents.GET("/resume", ResumeSession(s))
		torrents.GET("/move/:torrentId", MoveTorrent(s))
		torrents.GET("/pause/:torrentId", PauseTorrent(s))
		torrents.GET("/resume/:torrentId", ResumeTorrent(s))
		torrents.GET("/delete/:torrentId", RemoveTorrent(s))
		torrents.GET("/downloadall/:torrentId", DownloadAllTorrent(s))
		torrents.GET("/undownloadall/:torrentId", UnDownloadAllTorrent(s))
		torrents.GET("/selectfile/:torrentId", SelectFileTorrent(s, true))
		torrents.GET("/downloadfile/:torrentId", SelectFileTorrent(s, false))
		torrents.GET("/assign/:torrentId/:tmdbId", AssignTorrent(s))

		// Web UI json
		torrents.GET("/list", ListTorrentsWeb(s))
	}

	movies := r.Group("/movies")
	{
		movies.GET("/", MoviesIndex)
		movies.GET("/search", SearchMovies)
		movies.GET("/autoscraped", AutoscrapedMovies)
		movies.GET("/popular", PopularMovies)
		movies.GET("/popular/genre/:genre", PopularMovies)
		movies.GET("/popular/language/:language", PopularMovies)
		movies.GET("/popular/country/:country", PopularMovies)
		movies.GET("/recent", RecentMovies)
		movies.GET("/recent/genre/:genre", RecentMovies)
		movies.GET("/recent/language/:language", RecentMovies)
		movies.GET("/recent/country/:country", RecentMovies)
		movies.GET("/top", TopRatedMovies)
		movies.GET("/imdb250", IMDBTop250)
		movies.GET("/mostvoted", MoviesMostVoted)
		movies.GET("/genres", MovieGenres)
		movies.GET("/languages", MovieLanguages)
		movies.GET("/countries", MovieCountries)
		movies.GET("/library", MovieLibrary)

		trakt := movies.Group("/trakt")
		{
			trakt.GET("/watchlist", WatchlistMovies)
			trakt.GET("/collection", CollectionMovies)
			trakt.GET("/popular", TraktPopularMovies)
			trakt.GET("/recommendations", TraktRecommendationsMovies)
			trakt.GET("/trending", TraktTrendingMovies)
			trakt.GET("/toplists", TopTraktLists)
			trakt.GET("/played", TraktMostPlayedMovies)
			trakt.GET("/watched", TraktMostWatchedMovies)
			trakt.GET("/collected", TraktMostCollectedMovies)
			trakt.GET("/anticipated", TraktMostAnticipatedMovies)
			trakt.GET("/boxoffice", TraktBoxOffice)
			trakt.GET("/history", TraktHistoryMovies)

			lists := trakt.Group("/lists")
			{
				lists.GET("/", MoviesTraktLists)
				lists.GET("/:user/:listId", UserlistMovies)
			}

			calendars := trakt.Group("/calendars")
			{
				calendars.GET("/", CalendarMovies)
				calendars.GET("/movies", TraktMyMovies)
				calendars.GET("/releases", TraktMyReleases)
				calendars.GET("/allmovies", TraktAllMovies)
				calendars.GET("/allreleases", TraktAllReleases)
			}
		}
	}
	movie := r.Group("/movie")
	{
		movie.GET("/:tmdbId/infolabels", InfoLabelsMovie(s))
		movie.GET("/:tmdbId/download", MovieRun("download", s))
		movie.GET("/:tmdbId/download/*ident", MovieRun("download", s))
		movie.GET("/:tmdbId/links", MovieRun("links", s))
		movie.GET("/:tmdbId/links/*ident", MovieRun("links", s))
		movie.GET("/:tmdbId/forcelinks", MovieRun("forcelinks", s))
		movie.GET("/:tmdbId/forcelinks/*ident", MovieRun("forcelinks", s))
		movie.GET("/:tmdbId/play", MovieRun("play", s))
		movie.GET("/:tmdbId/play/*ident", MovieRun("play", s))
		movie.GET("/:tmdbId/forceplay", MovieRun("forceplay", s))
		movie.GET("/:tmdbId/forceplay/*ident", MovieRun("forceplay", s))
		movie.GET("/:tmdbId/watchlist/add", AddMovieToWatchlist)
		movie.GET("/:tmdbId/watchlist/remove", RemoveMovieFromWatchlist)
		movie.GET("/:tmdbId/collection/add", AddMovieToCollection)
		movie.GET("/:tmdbId/collection/remove", RemoveMovieFromCollection)
		movie.GET("/:tmdbId/watched", ToggleWatched("movie", true))
		movie.GET("/:tmdbId/watched/*ident", ToggleWatched("movie", true))
		movie.GET("/:tmdbId/unwatched", ToggleWatched("movie", false))
		movie.GET("/:tmdbId/unwatched/*ident", ToggleWatched("movie", false))
	}

	shows := r.Group("/shows")
	{
		shows.GET("/", TVIndex)
		shows.GET("/search", SearchShows)
		shows.GET("/popular", PopularShows)
		shows.GET("/popular/genre/:genre", PopularShows)
		shows.GET("/popular/language/:language", PopularShows)
		shows.GET("/popular/country/:country", PopularShows)
		shows.GET("/recent/shows", RecentShows)
		shows.GET("/recent/shows/genre/:genre", RecentShows)
		shows.GET("/recent/shows/language/:language", RecentShows)
		shows.GET("/recent/shows/country/:country", RecentShows)
		shows.GET("/recent/episodes", RecentEpisodes)
		shows.GET("/recent/episodes/genre/:genre", RecentEpisodes)
		shows.GET("/recent/episodes/language/:language", RecentEpisodes)
		shows.GET("/recent/episodes/country/:country", RecentEpisodes)
		shows.GET("/top", TopRatedShows)
		shows.GET("/mostvoted", TVMostVoted)
		shows.GET("/genres", TVGenres)
		shows.GET("/languages", TVLanguages)
		shows.GET("/countries", TVCountries)
		shows.GET("/library", TVLibrary)

		trakt := shows.Group("/trakt")
		{
			trakt.GET("/watchlist", WatchlistShows)
			trakt.GET("/collection", CollectionShows)
			trakt.GET("/popular", TraktPopularShows)
			trakt.GET("/recommendations", TraktRecommendationsShows)
			trakt.GET("/trending", TraktTrendingShows)
			trakt.GET("/played", TraktMostPlayedShows)
			trakt.GET("/watched", TraktMostWatchedShows)
			trakt.GET("/collected", TraktMostCollectedShows)
			trakt.GET("/anticipated", TraktMostAnticipatedShows)
			trakt.GET("/progress", TraktProgressShows)
			trakt.GET("/history", TraktHistoryShows)

			lists := trakt.Group("/lists")
			{
				lists.GET("/", TVTraktLists)
				lists.GET("/:user/:listId", UserlistShows)
			}

			calendars := trakt.Group("/calendars")
			{
				calendars.GET("/", CalendarShows)
				calendars.GET("/shows", TraktMyShows)
				calendars.GET("/newshows", TraktMyNewShows)
				calendars.GET("/premieres", TraktMyPremieres)
				calendars.GET("/allshows", TraktAllShows)
				calendars.GET("/allnewshows", TraktAllNewShows)
				calendars.GET("/allpremieres", TraktAllPremieres)
			}
		}
	}
	show := r.Group("/show")
	{
		show.GET("/:showId/watched", ToggleWatched("show", true))
		show.GET("/:showId/watched/*ident", ToggleWatched("show", true))
		show.GET("/:showId/unwatched", ToggleWatched("show", false))
		show.GET("/:showId/unwatched/*ident", ToggleWatched("show", false))
		show.GET("/:showId/seasons", ShowSeasons)
		show.GET("/:showId/season/:season/download", ShowSeasonRun("download", s))
		show.GET("/:showId/season/:season/download/*ident", ShowSeasonRun("download", s))
		show.GET("/:showId/season/:season/links", ShowSeasonRun("links", s))
		show.GET("/:showId/season/:season/links/*ident", ShowSeasonRun("links", s))
		show.GET("/:showId/season/:season/forcelinks", ShowSeasonRun("forcelinks", s))
		show.GET("/:showId/season/:season/forcelinks/*ident", ShowSeasonRun("forcelinks", s))
		show.GET("/:showId/season/:season/play", ShowSeasonRun("play", s))
		show.GET("/:showId/season/:season/play/*ident", ShowSeasonRun("play", s))
		show.GET("/:showId/season/:season/forceplay", ShowSeasonRun("forceplay", s))
		show.GET("/:showId/season/:season/forceplay/*ident", ShowSeasonRun("forceplay", s))
		show.GET("/:showId/season/:season/watched", ToggleWatched("season", true))
		show.GET("/:showId/season/:season/watched/*ident", ToggleWatched("season", true))
		show.GET("/:showId/season/:season/unwatched", ToggleWatched("season", false))
		show.GET("/:showId/season/:season/unwatched/*ident", ToggleWatched("season", false))
		show.GET("/:showId/season/:season/episodes", ShowEpisodes)
		show.GET("/:showId/season/:season/episode/:episode/infolabels", InfoLabelsEpisode(s))
		show.GET("/:showId/season/:season/episode/:episode/play", ShowEpisodeRun("play", s))
		show.GET("/:showId/season/:season/episode/:episode/play/*ident", ShowEpisodeRun("play", s))
		show.GET("/:showId/season/:season/episode/:episode/forceplay", ShowEpisodeRun("forceplay", s))
		show.GET("/:showId/season/:season/episode/:episode/forceplay/*ident", ShowEpisodeRun("forceplay", s))
		show.GET("/:showId/season/:season/episode/:episode/download", ShowEpisodeRun("download", s))
		show.GET("/:showId/season/:season/episode/:episode/download/*ident", ShowEpisodeRun("download", s))
		show.GET("/:showId/season/:season/episode/:episode/links", ShowEpisodeRun("links", s))
		show.GET("/:showId/season/:season/episode/:episode/links/*ident", ShowEpisodeRun("links", s))
		show.GET("/:showId/season/:season/episode/:episode/forcelinks", ShowEpisodeRun("forcelinks", s))
		show.GET("/:showId/season/:season/episode/:episode/forcelinks/*ident", ShowEpisodeRun("forcelinks", s))
		show.GET("/:showId/season/:season/episode/:episode/watched", ToggleWatched("episode", true))
		show.GET("/:showId/season/:season/episode/:episode/watched/*ident", ToggleWatched("episode", true))
		show.GET("/:showId/season/:season/episode/:episode/unwatched", ToggleWatched("episode", false))
		show.GET("/:showId/season/:season/episode/:episode/unwatched/*ident", ToggleWatched("episode", false))
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
		library.GET("/movie/play/:tmdbId", PlayMovie(s))
		library.GET("/show/add/:tmdbId", AddShow)
		library.GET("/show/remove/:tmdbId", RemoveShow)
		library.GET("/show/list/add/:listId", AddShowsList)
		library.GET("/show/play/:showId/:season/:episode", PlayShow(s))

		library.GET("/update", UpdateLibrary)

		// DEPRECATED
		library.GET("/play/movie/:tmdbId", PlayMovie(s))
		library.GET("/play/show/:showId/season/:season/episode/:episode", PlayShow(s))
	}

	context := r.Group("/context")
	{
		context.GET("/media/:media/:kodiID/:action", ContextPlaySelector(s))
		context.GET("/library/:media/:kodiID/:action", ContextActionFromKodiLibrarySelector(s))
		torrents := context.Group("/torrents")
		{
			torrents.GET("/assign/:torrentId/kodi/:media/:kodiID", ContextAssignKodiSelector(s))
			torrents.GET("/assign/:torrentId/tmdb/movie/:tmdbId", ContextAssignTMDBMovieSelector(s))
			torrents.GET("/assign/:torrentId/tmdb/show/:tmdbId/season/:season", ContextAssignTMDBSeasonSelector(s))
			torrents.GET("/assign/:torrentId/tmdb/show/:tmdbId/season/:season/episode/:episode", ContextAssignTMDBEpisodeSelector(s))
		}
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
		trakt.GET("/deauthorize", DeauthorizeTrakt)
		trakt.GET("/select_list/:action/:media", SelectTraktUserList)
		trakt.GET("/update", UpdateTrakt)
	}

	r.GET("/setviewmode/:content_type", SetViewMode)

	r.GET("/subtitles", SubtitlesIndex(s))
	r.GET("/subtitle/:id", SubtitleGet)

	r.GET("/play", Play(s))
	r.GET("/play/*ident", Play(s))
	r.Any("/playuri", PlayURI(s))
	r.Any("/playuri/*ident", PlayURI(s))
	r.GET("/download", Download(s))
	r.GET("/download/*ident", Download(s))

	r.POST("/callbacks/:cid", providers.CallbackHandler)

	// r.GET("/notification", Notification(s))

	r.GET("/versions", Versions(s))

	cmd := r.Group("/cmd")
	{
		cmd.GET("/clear_cache_key/:key", ClearCache)
		cmd.GET("/clear_page_cache", ClearPageCache)
		cmd.GET("/clear_trakt_cache", ClearTraktCache)
		cmd.GET("/clear_tmdb_cache", ClearTmdbCache)

		cmd.GET("/reset_path", ResetPath)
		cmd.GET("/reset_path/:path", ResetCustomPath)
		cmd.GET("/open_path/:path", OpenCustomPath)

		cmd.GET("/paste/:type", Pastebin)

		cmd.GET("/select_interface/:type", SelectNetworkInterface)
		cmd.GET("/select_strm_language", SelectStrmLanguage)

		database := cmd.Group("/database")
		{
			database.GET("/clear_movies", ClearDatabaseMovies)
			database.GET("/clear_shows", ClearDatabaseShows)
			database.GET("/clear_torrent_history", ClearDatabaseTorrentHistory)
			database.GET("/clear_search_history", ClearDatabaseSearchHistory)
			database.GET("/clear_database", ClearDatabase)
		}

		cache := cmd.Group("/cache")
		{
			cache.GET("/clear_tmdb", ClearCacheTMDB)
			cache.GET("/clear_trakt", ClearCacheTrakt)
			cache.GET("/clear_cache", ClearCache)
		}
	}

	menu := r.Group("/menu")
	{
		menu.GET("/:type/add", MenuAdd)
		menu.GET("/:type/remove", MenuRemove)
	}

	MovieMenu.Load()
	TVMenu.Load()

	return r
}

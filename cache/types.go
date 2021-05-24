package cache

import (
	"time"
)

const (
	GeneralExpire = 7 * 24 * time.Hour

	TMDBKey    = "com.tmdb."
	TVDBKey    = "com.tvdb."
	TraktKey   = "com.trakt."
	ScraperKey = "scraper."
	LibraryKey = "library."
	FanartKey  = "fanart."

	TMDBEpisodeKey                 = TMDBKey + "episode.%d.%d.%d.%s"
	TMDBEpisodeExpire              = GeneralExpire
	TMDBFindKey                    = TMDBKey + "find.%s.%s"
	TMDBFindExpire                 = GeneralExpire
	TMDBCountriesKey               = TMDBKey + "countries.%s"
	TMDBCountriesExpire            = GeneralExpire
	TMDBLanguagesKey               = TMDBKey + "languages.%s"
	TMDBLanguagesExpire            = GeneralExpire
	TMDBMovieImagesKey             = TMDBKey + "movie.%d.images"
	TMDBMovieImagesExpire          = GeneralExpire
	TMDBMovieByIDKey               = TMDBKey + "movie.%s.%s"
	TMDBMovieByIDExpire            = GeneralExpire
	TMDBMovieGenresKey             = TMDBKey + "genres.movies.%s"
	TMDBMovieGenresExpire          = GeneralExpire
	TMDBMoviesIMDBKey              = TMDBKey + "imdb.list.%s.%d.%d"
	TMDBMoviesIMDBExpire           = 24 * time.Hour
	TMDBMoviesIMDBTotalKey         = TMDBKey + "imdb.list.%s.total"
	TMDBMoviesIMDBTotalExpire      = 24 * time.Hour
	TMDBMoviesTopMoviesKey         = TMDBKey + "topmovies.%s.%s.%s.%s.%d.%d"
	TMDBMoviesTopMoviesExpire      = 24 * time.Hour
	TMDBMoviesTopMoviesTotalKey    = TMDBKey + "topmovies.%s.%s.%s.%s.total"
	TMDBMoviesTopMoviesTotalExpire = 24 * time.Hour
	TMDBSeasonKey                  = TMDBKey + "season.%d.%d.%s"
	TMDBSeasonExpire               = GeneralExpire
	TMDBSeasonImagesKey            = TMDBKey + "show.%d.%d.images"
	TMDBSeasonImagesExpire         = GeneralExpire
	TMDBShowByIDKey                = TMDBKey + "show.%d.%s"
	TMDBShowByIDExpire             = GeneralExpire
	TMDBShowImagesKey              = TMDBKey + "show.%d.images"
	TMDBShowImagesExpire           = GeneralExpire
	TMDBShowGenresKey              = TMDBKey + "genres.shows.%s"
	TMDBShowGenresExpire           = GeneralExpire
	TMDBShowsTopShowsKey           = TMDBKey + "topshows.%s.%s.%s.%s.%d.%d"
	TMDBShowsTopShowsExpire        = 24 * time.Hour
	TMDBShowsTopShowsTotalKey      = TMDBKey + "topshows.%s.%s.%s.%s.total"
	TMDBShowsTopShowsTotalExpire   = 24 * time.Hour
	TMDBEpisodeImagesKey           = TMDBKey + "show.%d.%d.%d.images"
	TMDBEpisodeImagesExpire        = GeneralExpire

	TraktActivitiesKey                     = TraktKey + "last_activities"
	TraktActivitiesExpire                  = 30 * 24 * time.Hour
	TraktPausedLastUpdatesKey              = TraktKey + "PausedLastUpdates.%d"
	TraktPausedLastUpdatesExpire           = 30 * 24 * time.Hour
	TraktMovieKey                          = TraktKey + "movie.%s"
	TraktMovieExpire                       = GeneralExpire
	TraktMovieByTMDBKey                    = TraktKey + "movie.tmdb.%s"
	TraktMovieByTMDBExpire                 = GeneralExpire
	TraktMoviesByCategoryKey               = TraktKey + "movies.%s.%s"
	TraktMoviesByCategoryExpire            = 24 * time.Hour
	TraktMoviesByCategoryTotalKey          = TraktKey + "movies.%s.total"
	TraktMoviesByCategoryTotalExpire       = 24 * time.Hour
	TraktMoviesWatchlistKey                = TraktKey + "movies.watchlist"
	TraktMoviesWatchlistExpire             = GeneralExpire
	TraktMoviesCollectionKey               = TraktKey + "movies.collection"
	TraktMoviesCollectionExpire            = GeneralExpire
	TraktMoviesListKey                     = TraktKey + "movies.list.%s"
	TraktMoviesListExpire                  = 1 * time.Minute
	TraktMoviesCalendarKey                 = TraktKey + "movies.calendar.%s.%s"
	TraktMoviesCalendarExpire              = GeneralExpire
	TraktMoviesCalendarTotalKey            = TraktKey + "movies.calendar.%s.total"
	TraktMoviesCalendarTotalExpire         = GeneralExpire
	TraktMoviesWatchedKey                  = TraktKey + "movies.watched"
	TraktMoviesWatchedExpire               = GeneralExpire
	TraktMoviesPausedKey                   = TraktKey + "movies.paused"
	TraktMoviesPausedExpire                = GeneralExpire
	TraktShowKey                           = TraktKey + "show.%s"
	TraktShowExpire                        = GeneralExpire
	TraktShowsByCategoryKey                = TraktKey + "shows.%s.%s"
	TraktShowsByCategoryExpire             = 24 * time.Hour
	TraktShowsByCategoryTotalKey           = TraktKey + "shows.%s.total"
	TraktShowsByCategoryTotalExpire        = 24 * time.Hour
	TraktShowsWatchlistKey                 = TraktKey + "shows.watchlist"
	TraktShowsWatchlistExpire              = GeneralExpire
	TraktShowsWatchedKey                   = TraktKey + "shows.watched"
	TraktShowsWatchedExpire                = GeneralExpire
	TraktShowsPausedKey                    = TraktKey + "shows.paused"
	TraktShowsPausedExpire                 = GeneralExpire
	TraktShowsCollectionKey                = TraktKey + "shows.collection"
	TraktShowsCollectionExpire             = GeneralExpire
	TraktShowsListKey                      = TraktKey + "shows.list.%s"
	TraktShowsListExpire                   = 1 * time.Minute
	TraktShowsCalendarKey                  = TraktKey + "shows.calendar.%s.%s"
	TraktShowsCalendarExpire               = GeneralExpire
	TraktShowsCalendarTotalKey             = TraktKey + "shows.calendar.%s.total"
	TraktShowsCalendarTotalExpire          = GeneralExpire
	TraktShowsHiddenProgressKey            = TraktKey + "shows.hidden.progress"
	TraktShowsHiddenProgressExpire         = GeneralExpire
	TraktSeasonKey                         = TraktKey + "season.%d.%d"
	TraktSeasonExpire                      = GeneralExpire
	TraktEpisodeKey                        = TraktKey + "episode.%d.%d.%d"
	TraktEpisodeExpire                     = GeneralExpire
	TraktEpisodeByIDKey                    = TraktKey + "episode.id.%s"
	TraktEpisodeByIDExpire                 = GeneralExpire
	TraktEpisodeByTMDBKey                  = TraktKey + "episode.tmdb.%s"
	TraktEpisodeByTMDBExpire               = GeneralExpire
	TraktEpisodeByTVDBKey                  = TraktKey + "episode.tvdb.%s"
	TraktEpisodeByTVDBExpire               = GeneralExpire
	TraktWatchedMoviesKey                  = TraktKey + "movies.watched.list"
	TraktWatchedMoviesExpire               = GeneralExpire
	TraktWatchedShowsKey                   = TraktKey + "shows.watched.list"
	TraktWatchedShowsExpire                = GeneralExpire
	TraktWatchedShowsProgressKey           = TraktKey + "progress.watched.%d"
	TraktWatchedShowsProgressExpire        = GeneralExpire
	TraktWatchedShowsProgressWatchedKey    = TraktKey + "progress.episodes.watched.%d"
	TraktWatchedShowsProgressWatchedExpire = GeneralExpire
	TraktShowTMDBKey                       = TraktKey + "show.tmdb.%s"
	TraktShowTMDBExpire                    = GeneralExpire
	TraktShowTVDBKey                       = TraktKey + "show.tvdb.%s"
	TraktShowTVDBExpire                    = GeneralExpire
	TraktLockedAccountKey                  = TraktKey + "locked.account"
	TraktLockedAccountExpire               = 24 * time.Hour

	TVDBShowByIDKey    = TVDBKey + "show.%d.%s"
	TVDBShowByIDExpire = GeneralExpire

	FanartMovieByIDKey    = FanartKey + "movie.%d"
	FanartMovieByIDExpire = GeneralExpire
	FanartShowByIDKey     = FanartKey + "show.%d"
	FanartShowByIDExpire  = GeneralExpire

	LibraryWatchedPlaycountKey    = LibraryKey + "WatchedLastPlaycount.%s"
	LibraryWatchedPlaycountExpire = 30 * 24 * time.Hour
	LibraryShowsLastUpdatesKey    = LibraryKey + "showsLastUpdates"
	LibraryShowsLastUpdatesExpire = 7 * 24 * time.Hour
	LibraryResolveFileKey         = LibraryKey + "Resolve_File_%s"
	LibraryResolveFileExpire      = 60 * 24 * time.Hour
	LibrarySyncPlaycountKey       = LibraryKey + "SyncLastPlaycount.%s"
	LibrarySyncPlaycountExpire    = 30 * 24 * time.Hour

	ScraperLastExecutionKey    = ScraperKey + "last.execution"
	ScraperLastExecutionExpire = 60 * 60 * 24 * 30
	ScraperMoviesListKey       = ScraperKey + "movies.list.%d"
	ScraperMoviesListExpire    = 60 * 60 * 6
	ScraperMovieExistsKey      = ScraperKey + "movie.exists.%d.%d.%t"
	ScraperMovieExistsExpire   = 60 * 60 * 24 * 365
)

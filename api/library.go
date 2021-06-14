package api

import (
	"fmt"
	"strconv"

	"github.com/anacrolix/missinggo/perf"
	"github.com/gin-gonic/gin"

	"github.com/elgatito/elementum/bittorrent"
	"github.com/elgatito/elementum/config"
	"github.com/elgatito/elementum/library"
	"github.com/elgatito/elementum/trakt"
	"github.com/elgatito/elementum/xbmc"
)

const (
	playLabel  = "LOCALIZE[30023]"
	linksLabel = "LOCALIZE[30202]"

	trueType  = "true"
	falseType = "false"

	movieType   = "movie"
	showType    = "show"
	seasonType  = "season"
	episodeType = "episode"
	searchType  = "search"

	multiType = "\nmulti"
)

var (
	libraryPath       string
	moviesLibraryPath string
	showsLibraryPath  string
)

// AddMovie ...
func AddMovie(ctx *gin.Context) {
	defer perf.ScopeTimer()()

	tmdbID := ctx.Params.ByName("tmdbId")
	force := ctx.DefaultQuery("force", falseType) == trueType

	movie, err := library.AddMovie(tmdbID, force)
	if err != nil {
		isErrored := true
		if err == library.ErrVideoRemoved {
			if xbmc.DialogConfirmFocused("Elementum", fmt.Sprintf("LOCALIZE[30279];;%s", movie.Title)) {
				movie, err = library.AddMovie(tmdbID, true)
				if err == nil {
					isErrored = false
				}
			}
		}
		if isErrored {
			ctx.String(200, err.Error())
			return
		}
	}
	if config.Get().TraktToken != "" && config.Get().TraktSyncAddedMovies {
		go trakt.SyncAddedItem("movies", tmdbID, config.Get().TraktSyncAddedMoviesLocation)
	}

	label := "LOCALIZE[30277]"
	logMsg := "%s (%s) added to library"
	if force {
		label = "LOCALIZE[30286]"
		logMsg = "%s (%s) merged to library"
	}

	log.Noticef(logMsg, movie.Title, tmdbID)
	if config.Get().LibraryUpdate == 0 || (config.Get().LibraryUpdate == 1 && xbmc.DialogConfirmFocused("Elementum", fmt.Sprintf("%s;;%s", label, movie.Title))) {
		xbmc.VideoLibraryScanDirectory(library.MoviesLibraryPath(), true)
	} else {
		if ctx != nil {
			ctx.Abort()
		}
		library.ClearPageCache()
	}
}

// AddMoviesList ...
func AddMoviesList(ctx *gin.Context) {
	defer perf.ScopeTimer()()

	listID := ctx.Params.ByName("listId")
	updatingStr := ctx.DefaultQuery("updating", falseType)

	updating := false
	if updatingStr != falseType {
		updating = true
	}

	library.SyncMoviesList(listID, updating, updating)
}

// RemoveMovie ...
func RemoveMovie(ctx *gin.Context) {
	defer perf.ScopeTimer()()

	tmdbID, _ := strconv.Atoi(ctx.Params.ByName("tmdbId"))
	tmdbStr := ctx.Params.ByName("tmdbId")
	movie, paths, err := library.RemoveMovie(tmdbID)
	if err != nil {
		ctx.String(200, err.Error())
	}
	if config.Get().TraktToken != "" && config.Get().TraktSyncRemovedMovies {
		go trakt.SyncRemovedItem("movies", tmdbStr, config.Get().TraktSyncRemovedMoviesLocation)
	}

	if ctx != nil {
		if movie != nil && xbmc.DialogConfirmFocused("Elementum", fmt.Sprintf("LOCALIZE[30278];;%s", movie.Title)) {
			for _, path := range paths {
				xbmc.VideoLibraryCleanDirectory(path, "movies", false)
			}
			if m, err := library.GetMovieByTMDB(movie.ID); err == nil && m != nil {
				xbmc.VideoLibraryRemoveMovie(m.XbmcUIDs.Kodi)
			}
		} else {
			ctx.Abort()
			library.ClearPageCache()
		}
	}

}

//
// Shows externals
//

// AddShow ...
func AddShow(ctx *gin.Context) {
	defer perf.ScopeTimer()()

	tmdbID := ctx.Params.ByName("tmdbId")
	force := ctx.DefaultQuery("force", falseType) == trueType

	show, err := library.AddShow(tmdbID, force)
	if err != nil {
		isErrored := true
		if err == library.ErrVideoRemoved {
			if xbmc.DialogConfirmFocused("Elementum", fmt.Sprintf("LOCALIZE[30279];;%s", show.Name)) {
				show, err = library.AddShow(tmdbID, true)
				if err == nil {
					isErrored = false
				}
			}
		}
		if isErrored {
			ctx.String(200, err.Error())
			return
		}
	}
	if config.Get().TraktToken != "" && config.Get().TraktSyncAddedShows {
		go trakt.SyncAddedItem("shows", tmdbID, config.Get().TraktSyncAddedShowsLocation)
	}

	label := "LOCALIZE[30277]"
	logMsg := "%s (%s) added to library"
	if force {
		label = "LOCALIZE[30286]"
		logMsg = "%s (%s) merged to library"
	}

	log.Noticef(logMsg, show.Name, tmdbID)
	if config.Get().LibraryUpdate == 0 || (config.Get().LibraryUpdate == 1 && xbmc.DialogConfirmFocused("Elementum", fmt.Sprintf("%s;;%s", label, show.Name))) {
		xbmc.VideoLibraryScanDirectory(library.ShowsLibraryPath(), true)
	} else {
		library.ClearPageCache()
	}
}

// AddShowsList ...
func AddShowsList(ctx *gin.Context) {
	defer perf.ScopeTimer()()

	listID := ctx.Params.ByName("listId")
	updatingStr := ctx.DefaultQuery("updating", falseType)

	updating := false
	if updatingStr != falseType {
		updating = true
	}

	library.SyncShowsList(listID, updating, updating)
}

// RemoveShow ...
func RemoveShow(ctx *gin.Context) {
	defer perf.ScopeTimer()()

	tmdbID := ctx.Params.ByName("tmdbId")
	show, paths, err := library.RemoveShow(tmdbID)
	if err != nil {
		ctx.String(200, err.Error())
	}
	if config.Get().TraktToken != "" && config.Get().TraktSyncRemovedShows {
		go trakt.SyncRemovedItem("shows", tmdbID, config.Get().TraktSyncRemovedShowsLocation)
	}

	if ctx != nil {
		if show != nil && paths != nil && xbmc.DialogConfirmFocused("Elementum", fmt.Sprintf("LOCALIZE[30278];;%s", show.Name)) {
			for _, path := range paths {
				xbmc.VideoLibraryCleanDirectory(path, "tvshows", false)
			}
			if s, err := library.GetShowByTMDB(show.ID); err == nil && s != nil {
				xbmc.VideoLibraryRemoveTVShow(s.XbmcUIDs.Kodi)
			}
		} else {
			ctx.Abort()
			library.ClearPageCache()
		}
	}

}

// UpdateLibrary ...
func UpdateLibrary(ctx *gin.Context) {
	if err := library.Refresh(); err != nil {
		ctx.String(200, err.Error())
	}
	if config.Get().LibraryUpdate == 0 || (config.Get().LibraryUpdate == 1 && xbmc.DialogConfirmFocused("Elementum", "LOCALIZE[30288]")) {
		xbmc.VideoLibraryScan()
	}
}

// UpdateTrakt ...
func UpdateTrakt(ctx *gin.Context) {
	xbmc.Notify("Elementum", "LOCALIZE[30358]", config.AddonIcon())
	ctx.String(200, "")
	go func() {
		library.IsTraktInitialized = false
		library.RefreshTrakt()
		if config.Get().LibraryUpdate == 0 || (config.Get().LibraryUpdate == 1 && xbmc.DialogConfirmFocused("Elementum", "LOCALIZE[30288]")) {
			xbmc.VideoLibraryScan()
		}
	}()
}

// PlayMovie ...
func PlayMovie(s *bittorrent.Service) gin.HandlerFunc {
	if config.Get().ChooseStreamAutoMovie {
		return MovieRun("play", s)
	}
	return MovieRun("links", s)
}

// PlayShow ...
func PlayShow(s *bittorrent.Service) gin.HandlerFunc {
	if config.Get().ChooseStreamAutoShow {
		return ShowEpisodeRun("play", s)
	}
	return ShowEpisodeRun("links", s)
}

package api

import (
	"github.com/asdine/storm/q"
	"github.com/gin-gonic/gin"
	"github.com/op/go-logging"

	"github.com/elgatito/elementum/config"
	"github.com/elgatito/elementum/database"
	"github.com/elgatito/elementum/library"
	"github.com/elgatito/elementum/xbmc"
)

var cmdLog = logging.MustGetLogger("cmd")

// ClearCache ...
func ClearCache(ctx *gin.Context) {
	key := ctx.Params.ByName("key")
	if key != "" {
		if ctx != nil {
			ctx.Abort()
		}

		library.ClearCacheKey(key)

	} else {
		log.Debug("Removing all the cache")

		if !xbmc.DialogConfirm("Elementum", "LOCALIZE[30471]") {
			ctx.String(200, "")
			return
		}

		database.GetCache().RecreateBucket(database.CommonBucket)
	}

	xbmc.Notify("Elementum", "LOCALIZE[30200]", config.AddonIcon())
}

// ClearCacheTMDB ...
func ClearCacheTMDB(ctx *gin.Context) {
	log.Debug("Removing TMDB cache")

	library.ClearTmdbCache()

	xbmc.Notify("Elementum", "LOCALIZE[30200]", config.AddonIcon())
}

// ClearCacheTrakt ...
func ClearCacheTrakt(ctx *gin.Context) {
	log.Debug("Removing Trakt cache")

	library.ClearTraktCache()

	xbmc.Notify("Elementum", "LOCALIZE[30200]", config.AddonIcon())
}

// ClearPageCache ...
func ClearPageCache(ctx *gin.Context) {
	if ctx != nil {
		ctx.Abort()
	}
	library.ClearPageCache()
}

// ClearTraktCache ...
func ClearTraktCache(ctx *gin.Context) {
	if ctx != nil {
		ctx.Abort()
	}
	library.ClearTraktCache()
}

// ClearTmdbCache ...
func ClearTmdbCache(ctx *gin.Context) {
	if ctx != nil {
		ctx.Abort()
	}
	library.ClearTmdbCache()
}

// ResetPath ...
func ResetPath(ctx *gin.Context) {
	xbmc.SetSetting("download_path", "")
	xbmc.SetSetting("library_path", "special://temp/elementum_library/")
	xbmc.SetSetting("torrents_path", "special://temp/elementum_torrents/")
}

// ResetCustomPath ...
func ResetCustomPath(ctx *gin.Context) {
	path := ctx.Params.ByName("path")

	if path != "" {
		xbmc.SetSetting(path+"_path", "/")
	}
}

// OpenCustomPath ...
func OpenCustomPath(ctx *gin.Context) {
	path := ctx.Params.ByName("path")
	loc := ""

	if path == "library" {
		loc = config.Get().LibraryPath
	} else if path == "torrents" {
		loc = config.Get().TorrentsPath
	} else if path == "download" {
		loc = config.Get().DownloadPath
	}

	if loc != "" {
		log.Debugf("Opening %s in Kodi browser", loc)
		xbmc.OpenDirectory(loc)
	}
}

// SetViewMode ...
func SetViewMode(ctx *gin.Context) {
	contentType := ctx.Params.ByName("content_type")
	viewName := xbmc.InfoLabel("Container.Viewmode")
	viewMode := xbmc.GetCurrentView()
	cmdLog.Noticef("ViewMode: %s (%s)", viewName, viewMode)
	if viewMode != "0" {
		xbmc.SetSetting("viewmode_"+contentType, viewMode)
	}
	ctx.String(200, "")
}

// ClearDatabaseMovies ...
func ClearDatabaseMovies(ctx *gin.Context) {
	log.Debug("Removing deleted movies from database")

	query := database.GetStormDB().Select(q.Eq("MediaType", library.MovieType), q.Eq("State", library.StateDeleted))
	_ = query.Delete(&database.LibraryItem{})

	xbmc.Notify("Elementum", "LOCALIZE[30472]", config.AddonIcon())

	ctx.String(200, "")
	return

}

// ClearDatabaseShows ...
func ClearDatabaseShows(ctx *gin.Context) {
	log.Debug("Removing deleted shows from database")

	query := database.GetStormDB().Select(q.Eq("MediaType", library.ShowType), q.Eq("State", library.StateDeleted))
	_ = query.Delete(&database.LibraryItem{})

	xbmc.Notify("Elementum", "LOCALIZE[30472]", config.AddonIcon())

	ctx.String(200, "")
	return

}

// ClearDatabaseTorrentHistory ...
func ClearDatabaseTorrentHistory(ctx *gin.Context) {
	log.Debug("Removing torrent history from database")

	database.GetStormDB().Drop(&database.TorrentAssignMetadata{})
	database.GetStormDB().Drop(&database.TorrentAssignItem{})
	database.GetStormDB().Drop(&database.TorrentHistory{})

	xbmc.Notify("Elementum", "LOCALIZE[30472]", config.AddonIcon())

	ctx.String(200, "")
	return

}

// ClearDatabaseSearchHistory ...
func ClearDatabaseSearchHistory(ctx *gin.Context) {
	log.Debug("Removing search history from database")

	database.GetStormDB().Drop(&database.QueryHistory{})

	xbmc.Notify("Elementum", "LOCALIZE[30472]", config.AddonIcon())

	ctx.String(200, "")
	return

}

// ClearDatabase ...
func ClearDatabase(ctx *gin.Context) {
	log.Debug("Removing all the database")

	if !xbmc.DialogConfirm("Elementum", "LOCALIZE[30471]") {
		ctx.String(200, "")
		return
	}

	database.GetStormDB().Drop(&database.BTItem{})
	database.GetStormDB().Drop(&database.TorrentHistory{})
	database.GetStormDB().Drop(&database.TorrentAssignMetadata{})
	database.GetStormDB().Drop(&database.TorrentAssignItem{})
	database.GetStormDB().Drop(&database.QueryHistory{})

	xbmc.Notify("Elementum", "LOCALIZE[30472]", config.AddonIcon())

	ctx.String(200, "")
	return
}

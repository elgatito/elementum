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
	xbmcHost, _ := xbmc.GetXBMCHostWithContext(ctx)
	if xbmcHost == nil {
		return
	}

	key := ctx.Params.ByName("key")
	if key != "" {
		if ctx != nil {
			ctx.Abort()
		}

		library.ClearCacheKey(key)

	} else {
		log.Debug("Removing all the cache")

		if !xbmcHost.DialogConfirm("Elementum", "LOCALIZE[30471]") {
			ctx.String(200, "")
			return
		}

		database.GetCache().RecreateBucket(database.CommonBucket)
	}

	xbmcHost.Notify("Elementum", "LOCALIZE[30200]", config.AddonIcon())
}

// ClearCacheTMDB ...
func ClearCacheTMDB(ctx *gin.Context) {
	xbmcHost, _ := xbmc.GetXBMCHostWithContext(ctx)
	if xbmcHost == nil {
		return
	}

	log.Debug("Removing TMDB cache")

	library.ClearTmdbCache(xbmcHost)

	xbmcHost.Notify("Elementum", "LOCALIZE[30200]", config.AddonIcon())
}

// ClearCacheTrakt ...
func ClearCacheTrakt(ctx *gin.Context) {
	xbmcHost, _ := xbmc.GetXBMCHostWithContext(ctx)
	if xbmcHost == nil {
		return
	}

	log.Debug("Removing Trakt cache")

	library.ClearTraktCache(xbmcHost)

	xbmcHost.Notify("Elementum", "LOCALIZE[30200]", config.AddonIcon())
}

// ClearPageCache ...
func ClearPageCache(ctx *gin.Context) {
	xbmcHost, _ := xbmc.GetXBMCHostWithContext(ctx)
	if xbmcHost == nil {
		return
	}

	if ctx != nil {
		ctx.Abort()
	}
	library.ClearPageCache(xbmcHost)
}

// ClearTraktCache ...
func ClearTraktCache(ctx *gin.Context) {
	xbmcHost, _ := xbmc.GetXBMCHostWithContext(ctx)
	if xbmcHost == nil {
		return
	}

	if ctx != nil {
		ctx.Abort()
	}
	library.ClearTraktCache(xbmcHost)
}

// ClearTmdbCache ...
func ClearTmdbCache(ctx *gin.Context) {
	xbmcHost, _ := xbmc.GetXBMCHostWithContext(ctx)
	if xbmcHost == nil {
		return
	}

	if ctx != nil {
		ctx.Abort()
	}
	library.ClearTmdbCache(xbmcHost)
}

// ResetPath ...
func ResetPath(ctx *gin.Context) {
	xbmcHost, _ := xbmc.GetXBMCHostWithContext(ctx)
	if xbmcHost == nil {
		return
	}

	xbmcHost.SetSetting("download_path", "")
	xbmcHost.SetSetting("library_path", "special://temp/elementum_library/")
	xbmcHost.SetSetting("torrents_path", "special://temp/elementum_torrents/")
}

// ResetCustomPath ...
func ResetCustomPath(ctx *gin.Context) {
	xbmcHost, _ := xbmc.GetXBMCHostWithContext(ctx)
	if xbmcHost == nil {
		return
	}

	path := ctx.Params.ByName("path")

	if path != "" {
		xbmcHost.SetSetting(path+"_path", "/")
	}
}

// OpenCustomPath ...
func OpenCustomPath(ctx *gin.Context) {
	xbmcHost, _ := xbmc.GetXBMCHostWithContext(ctx)
	if xbmcHost == nil {
		return
	}

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
		xbmcHost.OpenDirectory(loc)
	}
}

// SetViewMode ...
func SetViewMode(ctx *gin.Context) {
	xbmcHost, _ := xbmc.GetXBMCHostWithContext(ctx)
	if xbmcHost == nil {
		return
	}

	contentType := ctx.Params.ByName("content_type")
	viewName := xbmcHost.InfoLabel("Container.Viewmode")
	viewMode := xbmcHost.GetCurrentView()
	cmdLog.Noticef("ViewMode: %s (%s)", viewName, viewMode)
	if viewMode != "0" {
		xbmcHost.SetSetting("viewmode_"+contentType, viewMode)
	}
	ctx.String(200, "")
}

// ClearDatabaseDeletedMovies ...
func ClearDatabaseDeletedMovies(ctx *gin.Context) {
	xbmcHost, _ := xbmc.GetXBMCHostWithContext(ctx)
	if xbmcHost == nil {
		return
	}

	log.Debug("Removing deleted movies from database")

	db := database.GetStormDB()
	if db == nil {
		return
	}

	query := db.Select(q.Eq("MediaType", library.MovieType), q.Eq("State", library.StateDeleted))
	_ = query.Delete(&database.LibraryItem{})

	xbmcHost.Notify("Elementum", "LOCALIZE[30472]", config.AddonIcon())

	ctx.String(200, "")
}

// ClearDatabaseMovies ...
func ClearDatabaseMovies(ctx *gin.Context) {
	xbmcHost, _ := xbmc.GetXBMCHostWithContext(ctx)
	if xbmcHost == nil {
		return
	}

	log.Debug("Removing movies from database")

	db := database.GetStormDB()
	if db == nil {
		return
	}

	query := db.Select(q.Eq("MediaType", library.MovieType))
	_ = query.Delete(&database.LibraryItem{})

	xbmcHost.Notify("Elementum", "LOCALIZE[30472]", config.AddonIcon())

	ctx.String(200, "")
}

// ClearDatabaseDeletedShows ...
func ClearDatabaseDeletedShows(ctx *gin.Context) {
	xbmcHost, _ := xbmc.GetXBMCHostWithContext(ctx)
	if xbmcHost == nil {
		return
	}

	log.Debug("Removing deleted shows from database")

	query := database.GetStormDB().Select(q.Eq("MediaType", library.ShowType), q.Eq("State", library.StateDeleted))
	_ = query.Delete(&database.LibraryItem{})

	xbmcHost.Notify("Elementum", "LOCALIZE[30472]", config.AddonIcon())

	ctx.String(200, "")
}

// ClearDatabaseShows ...
func ClearDatabaseShows(ctx *gin.Context) {
	xbmcHost, _ := xbmc.GetXBMCHostWithContext(ctx)
	if xbmcHost == nil {
		return
	}

	log.Debug("Removing shows from database")

	query := database.GetStormDB().Select(q.Eq("MediaType", library.ShowType))
	_ = query.Delete(&database.LibraryItem{})

	xbmcHost.Notify("Elementum", "LOCALIZE[30472]", config.AddonIcon())

	ctx.String(200, "")
}

// ClearDatabaseTorrentHistory ...
func ClearDatabaseTorrentHistory(ctx *gin.Context) {
	xbmcHost, _ := xbmc.GetXBMCHostWithContext(ctx)
	if xbmcHost == nil {
		return
	}

	log.Debug("Removing torrent history from database")

	database.GetStormDB().Drop(&database.TorrentAssignMetadata{})
	database.GetStormDB().Drop(&database.TorrentAssignItem{})
	database.GetStormDB().Drop(&database.TorrentHistory{})

	xbmcHost.Notify("Elementum", "LOCALIZE[30472]", config.AddonIcon())

	ctx.String(200, "")
}

// ClearDatabaseSearchHistory ...
func ClearDatabaseSearchHistory(ctx *gin.Context) {
	xbmcHost, _ := xbmc.GetXBMCHostWithContext(ctx)
	if xbmcHost == nil {
		return
	}

	log.Debug("Removing search history from database")

	database.GetStormDB().Drop(&database.QueryHistory{})

	xbmcHost.Notify("Elementum", "LOCALIZE[30472]", config.AddonIcon())

	ctx.String(200, "")
}

// ClearDatabase ...
func ClearDatabase(ctx *gin.Context) {
	xbmcHost, _ := xbmc.GetXBMCHostWithContext(ctx)
	if xbmcHost == nil {
		return
	}

	log.Debug("Removing all the database")

	if !xbmcHost.DialogConfirm("Elementum", "LOCALIZE[30471]") {
		ctx.String(200, "")
		return
	}

	database.GetStormDB().Drop(&database.BTItem{})
	database.GetStormDB().Drop(&database.TorrentHistory{})
	database.GetStormDB().Drop(&database.TorrentAssignMetadata{})
	database.GetStormDB().Drop(&database.TorrentAssignItem{})
	database.GetStormDB().Drop(&database.QueryHistory{})

	xbmcHost.Notify("Elementum", "LOCALIZE[30472]", config.AddonIcon())

	ctx.String(200, "")
}

// CompactDatabase ...
func CompactDatabase(ctx *gin.Context) {
	xbmcHost, _ := xbmc.GetXBMCHostWithContext(ctx)
	if xbmcHost == nil {
		return
	}

	if !xbmcHost.DialogConfirm("Elementum", "LOCALIZE[30471]") {
		ctx.String(200, "")
		return
	}

	log.Debug("Compacting database")
	if err := database.GetStorm().Compress(); err != nil {
		log.Errorf("Error compacting cache: %s", err)
		xbmcHost.Notify("Elementum", err.Error(), config.AddonIcon())
	} else {
		xbmcHost.Notify("Elementum", "LOCALIZE[30674]", config.AddonIcon())
	}

	ctx.String(200, "")
}

// CompactCache ...
func CompactCache(ctx *gin.Context) {
	xbmcHost, _ := xbmc.GetXBMCHostWithContext(ctx)
	if xbmcHost == nil {
		return
	}

	if !xbmcHost.DialogConfirm("Elementum", "LOCALIZE[30471]") {
		ctx.String(200, "")
		return
	}

	log.Debug("Compacting cache")
	if err := database.GetCache().Compress(); err != nil {
		log.Errorf("Error compacting cache: %s", err)
		xbmcHost.Notify("Elementum", err.Error(), config.AddonIcon())
	} else {
		xbmcHost.Notify("Elementum", "LOCALIZE[30674]", config.AddonIcon())
	}

	ctx.String(200, "")
}

package api

import (
	"fmt"

	"github.com/anacrolix/missinggo/perf"
	"github.com/asdine/storm"
	"github.com/gin-gonic/gin"

	"github.com/elgatito/elementum/database"
	"github.com/elgatito/elementum/xbmc"
)

// History ...
func History(ctx *gin.Context) {
	defer perf.ScopeTimer()()

	infohash := ctx.Query("infohash")
	index := ctx.DefaultQuery("index", "")
	if torrent := InTorrentsHistory(infohash); torrent != nil {
		ctx.Redirect(302, URLQuery(
			URLForXBMC("/play"), "uri", torrent.URI, "index", index,
		))
		return
	}

	items := []*xbmc.ListItem{}
	var ths []database.TorrentHistory
	if err := database.GetStormDB().AllByIndex("Dt", &ths, storm.Reverse()); err != nil {
		log.Infof("Could not get list of history items: %s", err)
	}

	for _, th := range ths {
		items = append(items, &xbmc.ListItem{
			Label: th.Name,
			Path:  torrentHistoryGetXbmcURL(th.InfoHash),
			ContextMenu: [][]string{
				{"LOCALIZE[30406]", fmt.Sprintf("RunPlugin(%s)",
					URLQuery(URLForXBMC("/history/remove"),
						"infohash", th.InfoHash,
					))},
			},
			Info: &xbmc.ListItemInfo{
				Mediatype: "video",
			},
			IsPlayable: true,
		})
	}

	ctx.JSON(200, xbmc.NewView("", items))
}

func torrentHistoryEmpty() bool {
	count, err := database.GetStormDB().Count(&database.TorrentHistory{})
	if err != nil {
		log.Infof("Could not get count for torrent history: %s", err)
	}

	return err != nil || count == 0
}

// HistoryRemove ...
func HistoryRemove(ctx *gin.Context) {
	defer perf.ScopeTimer()()

	xbmcHost, _ := xbmc.GetXBMCHostWithContext(ctx)
	if xbmcHost == nil {
		return
	}

	infohash := ctx.DefaultQuery("infohash", "")

	if len(infohash) == 0 {
		return
	}

	log.Debugf("Removing infohash '%s' with torrent history", infohash)
	var th database.TorrentHistory
	if err := database.GetStormDB().One("InfoHash", infohash, &th); err == nil {
		database.GetStormDB().DeleteStruct(&th)
		database.GetStormDB().ReIndex(&database.TorrentHistory{})
	}

	xbmcHost.Refresh()

	ctx.String(200, "")
}

// HistoryClear ...
func HistoryClear(ctx *gin.Context) {
	defer perf.ScopeTimer()()

	xbmcHost, _ := xbmc.GetXBMCHostWithContext(ctx)
	if xbmcHost == nil {
		return
	}

	log.Debugf("Cleaning queries with torrent history")
	if err := database.GetStormDB().Drop(&database.TorrentHistory{}); err != nil {
		log.Infof("Could not clean torrent history: %s", err)
	}
	database.GetStormDB().ReIndex(&database.TorrentHistory{})

	xbmcHost.Refresh()

	ctx.String(200, "")
}

func torrentHistoryGetXbmcURL(infohash string) string {
	return URLQuery(URLForXBMC("/history"), "infohash", infohash)
}

func torrentHistoryGetHTTPUrl(infohash string) string {
	return URLQuery(URLForHTTP("/history"), "infohash", infohash)
}

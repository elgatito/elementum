package api

import (
	"fmt"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/op/go-logging"
	"github.com/scakemyer/quasar/bittorrent"
	"github.com/scakemyer/quasar/config"
	"github.com/scakemyer/quasar/database"
	"github.com/scakemyer/quasar/providers"
	"github.com/scakemyer/quasar/xbmc"
)

var searchLog = logging.MustGetLogger("search")
var historyMaxSize = 50

type SearchHistory []string

func Search(btService *bittorrent.BTService) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		ctx.Writer.Header().Set("Access-Control-Allow-Origin", "*")
		query := ctx.Query("q")
		keyboard := ctx.Query("keyboard")

		if len(query) == 0 {
			historyType := ""
			if len(keyboard) > 0 || searchHistoryEmpty(historyType) {
				query = xbmc.Keyboard("", "LOCALIZE[30206]")
				if len(query) == 0 {
					return
				}
				searchHistoryAppend(ctx, historyType, query)
			} else if !searchHistoryEmpty(historyType) {
				searchHistoryList(ctx, historyType)
			}
			return
		}

		existingTorrent := ExistingTorrent(btService, query)
		if existingTorrent != "" && xbmc.DialogConfirm("Quasar", "LOCALIZE[30270]") {
			xbmc.PlayURL(UrlQuery(UrlForXBMC("/play"), "uri", existingTorrent))
			return
		}

		searchLog.Infof("Searching providers for: %s", query)

		searchers := providers.GetSearchers()
		torrents := providers.Search(searchers, query)

		if len(torrents) == 0 {
			xbmc.Notify("Quasar", "LOCALIZE[30205]", config.AddonIcon())
			return
		}

		choices := make([]string, 0, len(torrents))
		for _, torrent := range torrents {
			resolution := ""
			if torrent.Resolution > 0 {
				resolution = fmt.Sprintf("[B][COLOR %s]%s[/COLOR][/B] ", bittorrent.Colors[torrent.Resolution], bittorrent.Resolutions[torrent.Resolution])
			}

			info := make([]string, 0)
			if torrent.Size != "" {
				info = append(info, fmt.Sprintf("[B][%s][/B]", torrent.Size))
			}
			if torrent.RipType > 0 {
				info = append(info, bittorrent.Rips[torrent.RipType])
			}
			if torrent.VideoCodec > 0 {
				info = append(info, bittorrent.Codecs[torrent.VideoCodec])
			}
			if torrent.AudioCodec > 0 {
				info = append(info, bittorrent.Codecs[torrent.AudioCodec])
			}
			if torrent.Provider != "" {
				info = append(info, fmt.Sprintf(" - [B]%s[/B]", torrent.Provider))
			}

			multi := ""
			if torrent.Multi {
				multi = "\nmulti"
			}

			label := fmt.Sprintf("%s(%d / %d) %s\n%s\n%s%s",
				resolution,
				torrent.Seeds,
				torrent.Peers,
				strings.Join(info, " "),
				torrent.Name,
				torrent.Icon,
				multi,
			)
			choices = append(choices, label)
		}

		choice := xbmc.ListDialogLarge("LOCALIZE[30228]", query, choices...)
		if choice >= 0 {
			xbmc.PlayURL(UrlQuery(UrlForXBMC("/play"), "uri", torrents[choice].URI))
		}
	}
}

func searchHistoryEmpty(historyType string) bool {
	historyList := SearchHistory{}
	db.GetObject(database.HistoryBucket, "list" + historyType, &historyList)

	return historyList == nil || len(historyList) == 0
}

func searchHistoryAppend(ctx *gin.Context, historyType string, query string) {
	historyList := SearchHistory{}
	db.GetObject(database.HistoryBucket, "list" + historyType, &historyList)

	found := -1
	for i, v := range historyList {
		if v == query {
			found = i
			break
		}
	}

	if found > -1 {
		historyList = append(historyList[:found], historyList[found+1:]...)
	}

	historyLength := len(historyList)
	if historyLength == 0 || historyLength < historyMaxSize {
		historyList = append(SearchHistory{query}, historyList...)
	} else {
		historyList = append(SearchHistory{query}, historyList[:historyMaxSize-1]...)
	}

	db.SetObject(database.HistoryBucket, "list" + historyType, &historyList)

	xbmc.UpdatePath(searchHistoryGetXbmcUrl(historyType, query))
	ctx.String(200, "")
	return
}

func searchHistoryList(ctx *gin.Context, historyType string) {
	historyList := &SearchHistory{}
	db.GetObject(database.HistoryBucket, "list" + historyType, historyList)

	urlPrefix := ""
	if len(historyType) > 0 {
		urlPrefix = "/" + historyType
	}

	items := make(xbmc.ListItems, 0, len(*historyList) + 1)
	items = append(items, &xbmc.ListItem{
		Label: "LOCALIZE[30323]",
		Path: UrlQuery(UrlForXBMC(urlPrefix + "/search"), "keyboard", "1"),
		Thumbnail: config.AddonResource("img", "search.png"),
		Icon: config.AddonResource("img", "search.png"),
	})

	for _, query := range *historyList {
		item := &xbmc.ListItem{
			Label: query,
			Path: searchHistoryGetXbmcUrl(historyType, query),
		}
		items = append(items, item)
	}

	ctx.JSON(200, xbmc.NewView("", items))
}

func searchHistoryGetXbmcUrl(historyType string, query string) string {
	urlPrefix := ""
	if len(historyType) > 0 {
		urlPrefix = "/" + historyType
	}

	return UrlQuery(UrlForXBMC(urlPrefix + "/search"), "q", query)
}

func searchHistoryGetHTTPUrl(historyType string, query string) string {
	urlPrefix := ""
	if len(historyType) > 0 {
		urlPrefix = "/" + historyType
	}

	return UrlQuery(UrlForHTTP(urlPrefix + "/search"), "q", query)
}

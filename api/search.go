package api

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"

	"github.com/anacrolix/missinggo/perf"
	"github.com/asdine/storm/v3/q"
	"github.com/cespare/xxhash/v2"
	"github.com/gin-gonic/gin"
	"github.com/op/go-logging"

	"github.com/elgatito/elementum/bittorrent"
	"github.com/elgatito/elementum/config"
	"github.com/elgatito/elementum/database"
	"github.com/elgatito/elementum/providers"
	"github.com/elgatito/elementum/util"
	"github.com/elgatito/elementum/util/ip"
	"github.com/elgatito/elementum/xbmc"
)

var searchLog = logging.MustGetLogger("search")

// Search ...
func Search(s *bittorrent.Service) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		defer perf.ScopeTimer()()

		xbmcHost, _ := xbmc.GetXBMCHostWithContext(ctx)
		if xbmcHost == nil {
			return
		}

		query := ctx.Query("q")
		keyboard := ctx.Query("keyboard")
		action := ctx.Query("action")
		silent := ctx.DefaultQuery("silent", "")
		index := ctx.DefaultQuery("index", "")
		historyType := ""
		listMode := wantsLinksList(ctx)

		runAction := "/play"
		if action == "download" {
			runAction = "/download"
		}

		if len(query) == 0 {
			searchHistoryProcess(ctx, historyType, keyboard)
			return
		}

		// Update query last use date to show it on the top
		database.GetStorm().AddSearchHistory(historyType, query)

		fakeTmdbID := strconv.Itoa(int(xxhash.Sum64String(query)))
		existingTorrent := s.HasTorrentByQuery(query)
		if existingTorrent != nil && !listMode && (silent != "" || config.Get().SilentStreamStart || existingTorrent.IsPlaying.Load() || (existingTorrent.IsNextFile.Load() && config.Get().SmartEpisodeChoose) || xbmcHost.DialogConfirmFocused("Elementum", fmt.Sprintf("LOCALIZE[30608];;[B]%s[/B]", existingTorrent.Title()))) {
			xbmcHost.PlayURLWithTimeout(URLQuery(
				URLForXBMC("%s", runAction),
				"resume", existingTorrent.InfoHash(),
				"query", query,
				"tmdb", fakeTmdbID,
				"index", index,
				"type", "search"))
			return
		}

		if torrent := InTorrentsMap(xbmcHost, fakeTmdbID); torrent != nil && !listMode {
			xbmcHost.PlayURLWithTimeout(URLQuery(
				URLForXBMC("%s", runAction), "uri", torrent.URI,
				"query", query,
				"tmdb", fakeTmdbID,
				"index", index,
				"type", "search"))
			return
		}

		var torrents []*bittorrent.TorrentFile
		var err error

		if torrents, err = GetCachedTorrents(fakeTmdbID); err != nil || len(torrents) == 0 {
			torrents = searchLinks(xbmcHost, callbackHost(ctx, xbmcHost), query)

			SetCachedTorrents(fakeTmdbID, torrents)
		}

		if listMode {
			renderLinks(ctx, torrents, URLForXBMC("%s", runAction),
				"query", query,
				"tmdb", fakeTmdbID,
				"index", index,
				"type", "search")
			return
		}

		if len(torrents) == 0 {
			xbmcHost.Notify("Elementum", "LOCALIZE[30205]", config.AddonIcon())
			return
		}

		choices := make([]string, 0, len(torrents))
		for _, torrent := range torrents {
			resolution := ""
			if torrent.Resolution > 0 {
				resolution = fmt.Sprintf("[B]%s[/B] ", util.ApplyColor(bittorrent.Resolutions[torrent.Resolution], bittorrent.Colors[torrent.Resolution]))
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
				multi = multiType
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

		choice := -1
		if detectPlayAction("", searchType) == "play" {
			choice = 0
		} else {
			choice = xbmcHost.ListDialogLarge("LOCALIZE[30228]", query, choices...)
		}

		if choice >= 0 {
			AddToTorrentsMap(fakeTmdbID, torrents[choice])

			xbmcHost.PlayURLWithTimeout(URLQuery(
				URLForXBMC("%s", runAction),
				"uri", torrents[choice].URI,
				"query", query,
				"tmdb", fakeTmdbID,
				"index", index,
				"type", "search"))
			return
		}
	}
}

// callbackHost returns daemon address, that Kodi uses to send provider results back.
// Kodi requests came to the address it can reach, but Web UI could be opened by an address, that chosen Kodi can't reach.
func callbackHost(ctx *gin.Context, xbmcHost *xbmc.XBMCHost) string {
	if ctx.GetHeader("User-Agent") == "plugin.video.elementum" || xbmcHost == nil {
		return ctx.Request.Host
	}

	host := config.Args.ServerExternalIP
	if host == "" {
		localIP, err := ip.LocalIPFor(xbmcHost.Host)
		if err != nil {
			return ctx.Request.Host
		}
		host = localIP.String()
	}
	return net.JoinHostPort(host, strconv.Itoa(config.Args.LocalPort))
}

// wantsLinksList reports whether the caller wants found links in the response instead of a selection dialog in Kodi:
// direct HTTP clients (web UI, remote API) or plugin:// callers that pass "list=1".
func wantsLinksList(ctx *gin.Context) bool {
	return ctx.GetHeader("User-Agent") != "plugin.video.elementum" || ctx.Query("list") != ""
}

// renderLinks responds with torrents as playable items, each pointing to route with "uri" and query added.
func renderLinks(ctx *gin.Context, torrents []*bittorrent.TorrentFile, route string, query ...string) {
	items := make(xbmc.ListItems, 0, len(torrents))
	for _, t := range torrents {
		info := []string{fmt.Sprintf("%d / %d", t.Seeds, t.Peers)}
		if t.Resolution > 0 {
			info = append(info, bittorrent.Resolutions[t.Resolution])
		}
		if t.Size != "" {
			info = append(info, t.Size)
		}

		items = append(items, &xbmc.ListItem{
			Label: t.Name,
			// Provider name, may be colored with Kodi formatting, like "[COLOR FFFFDE00]RuTor[/COLOR]"
			Label2:     t.Provider,
			Icon:       providerIcon(ctx, t.Icon),
			Path:       URLQuery(route, append([]string{"uri", t.URI}, query...)...),
			IsPlayable: true,
			Info: &xbmc.ListItemInfo{
				Title:   t.Name,
				TagLine: strings.Join(info, " - "),
				Code:    t.InfoHash,
			},
		})
	}

	ctx.JSON(200, xbmc.NewView("", items))
}

// providerIcons keeps icon paths, that were sent to Web UI, by their IDs
var providerIcons sync.Map

// providerIcon returns provider's icon for the caller. Icon is a file path on Kodi side:
// Kodi can read it itself, but browser can't, so it gets URL of ProviderIcon instead.
func providerIcon(ctx *gin.Context, path string) string {
	if path == "" || ctx.GetHeader("User-Agent") == "plugin.video.elementum" {
		return path
	}

	id := strconv.FormatUint(xxhash.Sum64String(path), 16)
	providerIcons.Store(id, path)
	return "/providericon/" + id
}

// ProviderIcon serves provider icon to Web UI. Only paths, given by providers, can be served.
func ProviderIcon(ctx *gin.Context) {
	path, ok := providerIcons.Load(ctx.Param("id"))
	if !ok {
		ctx.Status(http.StatusNotFound)
		return
	}

	// Kodi on another machine has a path, that is not available here
	file := config.WindowsToWSLPath(path.(string))
	if _, err := os.Stat(file); err != nil {
		ctx.Status(http.StatusNotFound)
		return
	}
	ctx.Header("Cache-Control", "max-age=86400")
	ctx.File(file)
}

func searchLinks(xbmcHost *xbmc.XBMCHost, callbackHost string, query string) []*bittorrent.TorrentFile {
	searchLog.Infof("Searching providers for query: %s", query)

	searchers := providers.GetSearchers(xbmcHost, callbackHost)
	if len(searchers) == 0 {
		xbmcHost.Notify("Elementum", "LOCALIZE[30204]", config.AddonIcon())
	}

	return providers.Search(xbmcHost, searchers, query)
}

func searchHistoryProcess(ctx *gin.Context, historyType string, keyboard string) {
	xbmcHost, _ := xbmc.GetXBMCHostWithContext(ctx)
	if xbmcHost == nil {
		return
	}

	if len(keyboard) > 0 {
		query := ""
		if query = xbmcHost.Keyboard("", "LOCALIZE[30206]"); len(query) == 0 {
			return
		}
		searchHistoryAppend(ctx, historyType, query)
	} else {
		searchHistoryList(ctx, historyType)
	}
}

func searchHistoryAppend(ctx *gin.Context, historyType string, query string) {
	xbmcHost, _ := xbmc.GetXBMCHostWithContext(ctx)
	if xbmcHost == nil {
		return
	}

	database.GetStorm().AddSearchHistory(historyType, query)

	go xbmcHost.UpdatePath(searchHistoryGetXbmcURL(historyType, query))
	ctx.String(200, "")
}

func searchHistoryList(ctx *gin.Context, historyType string) {
	historyList := []string{}
	var qs []database.QueryHistory
	database.GetStormDB().Select(q.Eq("Type", historyType)).OrderBy("Dt").Reverse().Find(&qs)
	for _, q := range qs {
		historyList = append(historyList, q.Query)
	}

	urlPrefix := ""
	if len(historyType) > 0 {
		urlPrefix = "/" + historyType
	}

	items := make(xbmc.ListItems, 0, len(historyList)+1)
	items = append(items, &xbmc.ListItem{
		Label:     "LOCALIZE[30323]",
		Path:      URLQuery(URLForXBMC("%s", urlPrefix+"/search"), "keyboard", "1"),
		Thumbnail: config.AddonResource("img", "search.png"),
		Icon:      config.AddonResource("img", "search.png"),
	})

	for _, query := range historyList {
		items = append(items, &xbmc.ListItem{
			Label: query,
			Path:  searchHistoryGetXbmcURL(historyType, query),
			ContextMenu: [][]string{
				{
					"LOCALIZE[30406]",
					fmt.Sprintf("RunPlugin(%s)",
						URLQuery(URLForXBMC("/search/remove"),
							"query", query,
							"type", historyType,
						),
					),
				},
				{
					"LOCALIZE[30604]",
					fmt.Sprintf("RunPlugin(%s)",
						URLQuery(URLForXBMC("/search/clear"),
							"type", historyType,
						),
					),
				},
			},
		})
	}

	ctx.JSON(200, xbmc.NewView("", items))
}

// SearchRemove ...
func SearchRemove(ctx *gin.Context) {
	defer perf.ScopeTimer()()

	xbmcHost, _ := xbmc.GetXBMCHostWithContext(ctx)
	if xbmcHost == nil {
		return
	}

	query := ctx.DefaultQuery("query", "")
	historyType := ctx.DefaultQuery("type", "")

	if len(query) == 0 {
		return
	}

	log.Debugf("Removing query '%s' with history type '%s'", query, historyType)
	database.GetStorm().RemoveSearchHistory(historyType, query)
	xbmcHost.Refresh()

	ctx.String(200, "")
}

// SearchClear ...
func SearchClear(ctx *gin.Context) {
	defer perf.ScopeTimer()()

	xbmcHost, _ := xbmc.GetXBMCHostWithContext(ctx)
	if xbmcHost == nil {
		return
	}

	historyType := ctx.DefaultQuery("type", "")

	log.Debugf("Cleaning queries with history type %s", historyType)
	database.GetStorm().CleanSearchHistory(historyType)
	xbmcHost.Refresh()

	ctx.String(200, "")
}

func searchHistoryGetXbmcURL(historyType string, query string) string {
	urlPrefix := ""
	if len(historyType) > 0 {
		urlPrefix = "/" + historyType
	}

	return URLQuery(URLForXBMC("%s", urlPrefix+"/search"), "q", query)
}

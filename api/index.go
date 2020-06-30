package api

import (
	"strings"

	"github.com/anacrolix/missinggo/perf"
	"github.com/gin-gonic/gin"

	"github.com/elgatito/elementum/bittorrent"
	"github.com/elgatito/elementum/config"
	"github.com/elgatito/elementum/xbmc"
)

func init() {
	gin.SetMode(gin.ReleaseMode)
}

// Index ...
func Index(s *bittorrent.Service) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		action := ctx.Query("action")
		if action == "search" || action == "manualsearch" {
			SubtitlesIndex(s)(ctx)
			return
		}

		defer perf.ScopeTimer()()

		li := xbmc.ListItems{
			{Label: "LOCALIZE[30214]", Path: URLForXBMC("/movies/"), Thumbnail: config.AddonResource("img", "movies.png")},
			{Label: "LOCALIZE[30215]", Path: URLForXBMC("/shows/"), Thumbnail: config.AddonResource("img", "tv.png")},
			{Label: "LOCALIZE[30209]", Path: URLForXBMC("/search"), Thumbnail: config.AddonResource("img", "search.png")},
			{Label: "LOCALIZE[30229]", Path: URLForXBMC("/torrents/"), Thumbnail: config.AddonResource("img", "cloud.png")},
			{Label: "LOCALIZE[30216]", Path: URLForXBMC("/playtorrent"), Thumbnail: config.AddonResource("img", "magnet.png")},
			{Label: "LOCALIZE[30537]", Path: URLForXBMC("/history"), Thumbnail: config.AddonResource("img", "clock.png")},
			{Label: "LOCALIZE[30239]", Path: URLForXBMC("/provider/"), Thumbnail: config.AddonResource("img", "shield.png")},
			{Label: "LOCALIZE[30355]", Path: URLForXBMC("/changelog"), Thumbnail: config.AddonResource("img", "faq8.png")},
			{Label: "LOCALIZE[30393]", Path: URLForXBMC("/status"), Thumbnail: config.AddonResource("img", "clock.png")},
			{Label: "LOCALIZE[30527]", Path: URLForXBMC("/donate"), Thumbnail: config.AddonResource("img", "faq8.png")},
			{Label: "LOCALIZE[30579]", Path: URLForXBMC("/settings/plugin.video.elementum"), Thumbnail: config.AddonResource("img", "settings.png")},
		}

		// Adding Settings urls for each search provider found locally.
		for _, addon := range getProviders() {
			name := strings.Title(strings.ReplaceAll(addon.Name, "script.elementum.", ""))

			li = append(li, &xbmc.ListItem{Label: "LOCALIZE[30582];;" + name, Path: URLForXBMC("/settings/" + addon.ID), Thumbnail: config.AddonResource("img", "settings.png")})
		}

		ctx.JSON(200, xbmc.NewView("", li))
	}
}

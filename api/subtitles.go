package api

import (
	"net/url"
	"strconv"
	"strings"

	"github.com/elgatito/elementum/bittorrent"

	"github.com/elgatito/elementum/config"
	"github.com/elgatito/elementum/opensubtitles"
	"github.com/elgatito/elementum/util/ip"
	"github.com/elgatito/elementum/xbmc"

	"github.com/gin-gonic/gin"
	"github.com/op/go-logging"
)

var subLog = logging.MustGetLogger("subtitles")

// SubtitlesIndex ...
func SubtitlesIndex(s *bittorrent.Service) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		q := ctx.Request.URL.Query()

		xbmcHost, _ := xbmc.GetXBMCHostWithContext(ctx)

		playingFile := xbmcHost.PlayerGetPlayingFile()

		// Check if we are reading a file from Elementum
		if strings.HasPrefix(playingFile, ip.GetContextHTTPHost(ctx)) {
			playingFile = strings.Replace(playingFile, ip.GetContextHTTPHost(ctx)+"/files", config.Get().DownloadPath, 1)
			// not QueryUnescape in order to treat "+" as "+" in file name on FS
			playingFile, _ = url.PathUnescape(playingFile)
		}

		showID := 0
		if s.GetActivePlayer() != nil {
			showID = s.GetActivePlayer().Params().ShowID
		}
		payloads, preferredLanguage := opensubtitles.GetPayloads(xbmcHost, q.Get("searchstring"), strings.Split(q.Get("languages"), ","), q.Get("preferredlanguage"), showID, playingFile)
		subLog.Infof("Subtitles payload: %#v", payloads)

		results, err := opensubtitles.DoSearch(payloads, preferredLanguage)
		if err != nil {
			subLog.Errorf("Error searching subtitles: %s", err)
		}

		items := make(xbmc.ListItems, 0)

		for _, sub := range results {
			if len(sub.Attributes.Files) == 0 {
				continue
			}

			subFile := sub.Attributes.Files[0]

			item := &xbmc.ListItem{
				Label:  sub.Attributes.Language,
				Label2: subFile.FileName,
				Icon:   strconv.Itoa(int((sub.Attributes.Ratings / 2) + 0.5)),
				Path: URLQuery(URLForXBMC("/subtitle/%d", subFile.FileID),
					"file", subFile.FileName,
					"lang", sub.Attributes.Language,
					"fmt", sub.Type),
				Properties: &xbmc.ListItemProperties{},
			}
			if sub.Attributes.MovieHashMatch {
				item.Properties.SubtitlesSync = trueType
			}
			if sub.Attributes.HearingImpaired {
				item.Properties.SubtitlesHearingImpaired = trueType
			}
			items = append(items, item)
		}

		ctx.JSON(200, xbmc.NewView("", items))
	}
}

// SubtitleGet ...
func SubtitleGet(ctx *gin.Context) {
	id := ctx.Params.ByName("id")

	log.Debugf("Downloading subtitles: %s", id)
	outFile, file, _, err := opensubtitles.DoDownload(id)
	if err != nil {
		subLog.Error(err)
		ctx.String(200, err.Error())
		return
	}

	ctx.JSON(200, xbmc.NewView("", xbmc.ListItems{
		{Label: file, Path: outFile.Name()},
	}))
}

package api

import (
	"fmt"
	"net/url"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/scakemyer/quasar/bittorrent"
	"github.com/scakemyer/quasar/util"
	"github.com/scakemyer/quasar/xbmc"
)

func Play(btService *bittorrent.BTService) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		uri := ctx.Query("uri")
		index := ctx.Query("index")
		resume := ctx.Query("resume")
		library := ctx.Query("library")
		contentType := ctx.Query("type")
		tmdb := ctx.Query("tmdb")
		show := ctx.Query("show")
		season := ctx.Query("season")
		episode := ctx.Query("episode")

		if uri == "" && resume == "" {
			return
		}

		fileIndex := -1
		if index != "" {
			if position, err := strconv.Atoi(index); err == nil && position >= 0 {
				fileIndex = position
			}
		}

		resumeIndex := -1
		if resume != "" {
			if position, err := strconv.Atoi(resume); err == nil && position >= 0 {
				resumeIndex = position
			}
		}

		fromLibrary := false
		if library != "" {
			fromLibrary = true
		}

		tmdbId := 0
		if tmdb != "" {
			if id, err := strconv.Atoi(tmdb); err == nil && id > 0 {
				tmdbId = id
			}
		}

		showId := 0
		if show != "" {
			if id, err := strconv.Atoi(show); err == nil && id > 0 {
				showId = id
			}
		}

		seasonNumber := 0
		if season != "" {
			if number, err := strconv.Atoi(season); err == nil && number > 0 {
				seasonNumber = number
			}
		}

		episodeNumber := 0
		if episode != "" {
			if number, err := strconv.Atoi(episode); err == nil && number > 0 {
				episodeNumber = number
			}
		}

		params := bittorrent.BTPlayerParams{
			URI: uri,
			FromLibrary: fromLibrary,
			FileIndex: fileIndex,
			ResumeIndex: resumeIndex,
			ContentType: contentType,
			TMDBId: tmdbId,
			ShowID: showId,
			Season: seasonNumber,
			Episode: episodeNumber,
		}

		player := bittorrent.NewBTPlayer(btService, params)
		if player.Buffer() != nil {
			return
		}

		rUrl, _ := url.Parse(fmt.Sprintf("%s/files/%s", util.GetHTTPHost(), player.PlayURL()))
		ctx.Redirect(302, rUrl.String())
	}
}

func PlayTorrent(ctx *gin.Context) {
	retval := xbmc.DialogInsert()
	if retval["path"] == "" {
		return
	}
	xbmc.PlayURL(UrlQuery(UrlForXBMC("/play"), "uri", retval["path"]))
}

func PlayURI(btService *bittorrent.BTService) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		uri := ctx.Query("uri")
		index := ctx.Query("index")
		resume := ctx.Query("resume")
		library := ctx.Query("library")

		if uri == "" && resume == "" {
			return
		}

		if uri != "" {
			xbmc.PlayURL(UrlQuery(UrlForXBMC("/play"), "uri", uri, "index", index))
		} else {
			var (
				tmdb string
				show string
				season string
				episode string
				contentType string
			)
			resumeIndex, _ := strconv.Atoi(resume)
			torrentHandle := btService.Client.Torrents()[resumeIndex]

			if torrentHandle != nil {
				infoHash := torrentHandle.InfoHash().AsString()
				dbItem := btService.GetDBItem(infoHash)
				if dbItem.Type != "" {
					contentType = dbItem.Type
					if contentType == "movie" {
						tmdb = strconv.Itoa(dbItem.ID)
					} else {
						show = strconv.Itoa(dbItem.ShowID)
						season = strconv.Itoa(dbItem.Season)
						episode = strconv.Itoa(dbItem.Episode)
					}
				}
			}
			xbmc.PlayURL(UrlQuery(UrlForXBMC("/play"),
				"resume", resume,
				"index", index,
				"tmdb", tmdb,
				"show", show,
				"season", season,
				"episode", episode,
				"type", contentType,
				"library", library))
		}
		ctx.Writer.Header().Set("Access-Control-Allow-Origin", "*")
		ctx.String(200, "")
	}
}

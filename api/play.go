package api

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/anacrolix/missinggo/perf"
	"github.com/gin-gonic/gin"
	"github.com/sanity-io/litter"

	"github.com/elgatito/elementum/bittorrent"
	"github.com/elgatito/elementum/database"
	"github.com/elgatito/elementum/util"
	"github.com/elgatito/elementum/xbmc"
)

// Download ...
func Download(s *bittorrent.Service) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		rURL, _ := url.Parse(fmt.Sprintf("%s%s", util.GetContextHTTPHost(ctx), strings.Replace(ctx.Request.RequestURI, "/download", "/play", 1)+"&background=true"))
		ctx.Redirect(302, rURL.String())
		return
	}
}

// Play ...
func Play(s *bittorrent.Service) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		// Index is the file index to automatically select.
		// Starts with 0, -1 or empty means - not defined.
		// OIndex is the original file index to automatically select.
		// 		Order is just like in the torrent file, without changes.
		// NIndex is the file next to download
		// NOIndex is the original torrent file next to download
		index := ctx.Query("index")
		oindex := ctx.Query("oindex")
		nindex := ctx.Query("nindex")
		noindex := ctx.Query("noindex")

		uri := ctx.Query("uri")
		resume := ctx.Query("resume")
		doresume := ctx.DefaultQuery("doresume", "")
		query := ctx.Query("query")
		contentType := ctx.Query("type")
		tmdb := ctx.Query("tmdb")
		show := ctx.Query("show")
		season := ctx.Query("season")
		episode := ctx.Query("episode")
		background := ctx.DefaultQuery("background", "false")

		if uri == "" && resume == "" {
			return
		}

		resumePlayback := bittorrent.ResumeEmpty
		if doresume == "true" {
			resumePlayback = bittorrent.ResumeYes
		} else if doresume == "false" {
			resumePlayback = bittorrent.ResumeNo
		}

		fileIndex := strToInt(index, -1)
		originalIndex := strToInt(oindex, -1)

		nextFileIndex := strToInt(nindex, -1)
		nextOriginalIndex := strToInt(noindex, -1)

		tmdbID := strToInt(tmdb, 0)
		showID := strToInt(show, 0)
		seasonNumber := strToInt(season, 0)
		episodeNumber := strToInt(episode, 0)

		params := bittorrent.PlayerParams{
			URI:               uri,
			OriginalIndex:     originalIndex,
			FileIndex:         fileIndex,
			NextOriginalIndex: nextOriginalIndex,
			NextFileIndex:     nextFileIndex,
			ResumeHash:        resume,
			ResumePlayback:    resumePlayback,
			KodiPosition:      -1,
			ContentType:       contentType,
			TMDBId:            tmdbID,
			ShowID:            showID,
			Season:            seasonNumber,
			Episode:           episodeNumber,
			Query:             query,
			Background:        background == "true",
		}

		player := bittorrent.NewPlayer(s, params)
		log.Infof("Playing item: %s", litter.Sdump(params))

		if uri != "" {
			if t := s.GetTorrentByURI(uri); t != nil {
				player.Params().ResumeHash = t.InfoHash()
				player.SetTorrent(t)
			} else {
				//try to get hash so we can try to find torrent by hash later
				torrent := bittorrent.NewTorrentFile(uri)
				if err := torrent.Resolve(); err == nil {
					resume = torrent.InfoHash
					player.Params().ResumeHash = resume
				}
			}
		}

		if resume != "" {
			if t := s.GetTorrentByHash(resume); t != nil {
				player.SetTorrent(t)
			} else {
				//wrong "resume" was provided by user or "resume" from url not in the queue
				player.Params().ResumeHash = ""
			}
		}

		if player.Buffer() != nil || !player.HasChosenFile() || player.Params().Background {
			player.Close()
			return
		}

		rURL, _ := url.Parse(fmt.Sprintf("%s/files/%s", util.GetContextHTTPHost(ctx), player.PlayURL()))
		ctx.Redirect(302, rURL.String())
	}
}

// PlayTorrent ...
func PlayTorrent(ctx *gin.Context) {
	defer perf.ScopeTimer()()

	retval := xbmc.DialogInsert()
	if retval["path"] == "" {
		log.Errorf("No path from insert dialog: %#v", retval)
		return
	}
	xbmc.PlayURLWithTimeout(URLQuery(URLForXBMC("/play"), "uri", retval["path"]))

	ctx.String(200, "")
	return
}

// PlayURI ...
func PlayURI(s *bittorrent.Service) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		uri := ctx.Request.FormValue("uri")
		file, header, fileError := ctx.Request.FormFile("file")

		if file != nil && header != nil && fileError == nil {
			t, err := saveTorrentFile(file, header)
			if err == nil && t != "" {
				uri = t
			}
		}

		index := ctx.Query("index")
		resume := ctx.Query("resume")

		if uri == "" && resume == "" {
			return
		}

		if uri != "" {
			xbmc.PlayURL(URLQuery(URLForXBMC("/play"), "uri", uri, "index", index))
		} else {
			var (
				tmdb        string
				show        string
				season      string
				episode     string
				query       string
				contentType string
			)
			t := s.GetTorrentByHash(resume)

			if t != nil {
				infoHash := t.InfoHash()
				dbItem := database.GetStorm().GetBTItem(infoHash)
				if dbItem != nil && dbItem.Type != "" {
					contentType = dbItem.Type
					if contentType == movieType {
						tmdb = strconv.Itoa(dbItem.ID)
					} else {
						show = strconv.Itoa(dbItem.ShowID)
						season = strconv.Itoa(dbItem.Season)
						episode = strconv.Itoa(dbItem.Episode)
					}
					query = dbItem.Query
				}
			}
			xbmc.PlayURL(URLQuery(URLForXBMC("/play"),
				"resume", resume,
				"index", index,
				"tmdb", tmdb,
				"show", show,
				"season", season,
				"episode", episode,
				"query", query,
				"type", contentType))
		}
		ctx.String(200, "")
	}
}

// strToInt parses string to int, and returning default value is no int found
func strToInt(str string, def int) int {
	if str != "" {
		if i, err := strconv.Atoi(str); err == nil && i >= 0 {
			return i
		}
	}

	return def
}

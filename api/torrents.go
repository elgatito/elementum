package api

import (
	"os"
	"fmt"
	"errors"
	"strings"
	"strconv"
	"unicode"
	"path/filepath"

	"github.com/op/go-logging"
	"github.com/gin-gonic/gin"
	"github.com/dustin/go-humanize"
	"github.com/cloudflare/ahocorasick"

	"github.com/scakemyer/quasar/bittorrent"
	"github.com/scakemyer/quasar/config"
	"github.com/scakemyer/quasar/util"
	"github.com/scakemyer/quasar/xbmc"
)

var torrentsLog = logging.MustGetLogger("torrents")

type TorrentsWeb struct {
	Name         string  `json:"name"`
	Size         string  `json:"size"`
	Status       string  `json:"status"`
	Progress     float64 `json:"progress"`
	Ratio        float64 `json:"ratio"`
	TimeRatio    float64 `json:"time_ratio"`
	SeedingTime  string  `json:"seeding_time"`
	SeedTime     float64 `json:"seed_time"`
	SeedTimeLimit    int `json:"seed_time_limit"`
	DownloadRate float64 `json:"download_rate"`
	UploadRate   float64 `json:"upload_rate"`
	Seeders      int     `json:"seeders"`
	SeedersTotal int     `json:"seeders_total"`
	Peers        int     `json:"peers"`
	PeersTotal   int     `json:"peers_total"`
}

type TorrentMap struct {
	tmdbId  string
	torrent *bittorrent.TorrentFile
}
var TorrentsMap []*TorrentMap

func AddToTorrentsMap(tmdbId string, torrent *bittorrent.TorrentFile) {
	inTorrentsMap := false
	for _, torrentMap := range TorrentsMap {
		if tmdbId == torrentMap.tmdbId {
			inTorrentsMap = true
		}
	}
	if inTorrentsMap == false {
		torrentMap := &TorrentMap{
			tmdbId: tmdbId,
			torrent: torrent,
		}
		TorrentsMap = append(TorrentsMap, torrentMap)
	}
}

func InTorrentsMap(tmdbId string) (torrents []*bittorrent.TorrentFile) {
	for index, torrentMap := range TorrentsMap {
		if tmdbId == torrentMap.tmdbId {
			if xbmc.DialogConfirm("Quasar", "LOCALIZE[30260]") {
				torrents = append(torrents, torrentMap.torrent)
			} else {
				TorrentsMap = append(TorrentsMap[:index], TorrentsMap[index + 1:]...)
			}
		}
	}
	return torrents
}

func nameMatch(torrentName string, itemName string) bool {
	patterns := strings.FieldsFunc(strings.ToLower(itemName), func(r rune) bool {
		if unicode.IsSpace(r) || unicode.IsPunct(r) || unicode.IsMark(r) {
			return true
		}
		return false
	})

	m := ahocorasick.NewStringMatcher(patterns)

	found := m.Match([]byte(strings.ToLower(torrentName)))

	return len(found) >= len(patterns)
}

func ExistingTorrent(btService *bittorrent.BTService, longName string) (existingTorrent string) {
	for _, torrent := range btService.Torrents {
		if nameMatch(torrent.Name(), longName) {
			infoHash := torrent.InfoHash()
			torrentFile := filepath.Join(config.Get().TorrentsPath, fmt.Sprintf("%s.torrent", infoHash))
			torrentsLog.Debugf("Existing: %#v", torrentFile)
			return torrentFile
		}
	}

	return ""
}

func ListTorrents(btService *bittorrent.BTService) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		items := make(xbmc.ListItems, 0, len(btService.Torrents))

		torrentsLog.Info("Currently downloading:")
		for i, torrent := range btService.Torrents {
			if torrent == nil {
				continue
			}

			torrentName := torrent.Name()
			progress    := torrent.GetProgress()
			status      := torrent.GetStateString()

			torrentAction := []string{"LOCALIZE[30231]", fmt.Sprintf("XBMC.RunPlugin(%s)", UrlForXBMC("/torrents/pause/%d", i))}
			sessionAction := []string{"LOCALIZE[30233]", fmt.Sprintf("XBMC.RunPlugin(%s)", UrlForXBMC("/torrents/pause"))}

			if status == "Paused" {
				sessionAction = []string{"LOCALIZE[30234]", fmt.Sprintf("XBMC.RunPlugin(%s)", UrlForXBMC("/torrents/resume"))}
			} else if status != "Finished" {
				if progress >= 100 {
					status = "Finished"
				} else {
					status = "Downloading"
				}
				torrentAction = []string{"LOCALIZE[30235]", fmt.Sprintf("XBMC.RunPlugin(%s)", UrlForXBMC("/torrents/resume/%d", i))}
			} else if status == "Finished" || progress >= 100 {
				status = "Seeding"
			}

			color := "white"
			switch (status) {
			case "Paused":
				fallthrough
			case "Finished":
				color = "grey"
			case "Seeding":
				color = "green"
			case "Buffering":
				color = "blue"
			case "Finding":
				color = "orange"
			case "Checking":
				color = "teal"
			case "Queued":
			case "Allocating":
				color = "black"
			case "Stalled":
				color = "red"
			}

			// TODO: Add seeding time and ratio getter/output
			torrentsLog.Infof("- %.2f%% - %s - %s", progress, status, torrentName)

			var (
				tmdb string
				show string
				season string
				episode string
				contentType string
			)

			if torrent.DBItem != nil && torrent.DBItem.Type != "" {
				contentType = torrent.DBItem.Type
				if contentType == "movie" {
					tmdb    = strconv.Itoa(torrent.DBItem.ID)
				} else {
					show    = strconv.Itoa(torrent.DBItem.ShowID)
					season  = strconv.Itoa(torrent.DBItem.Season)
					episode = strconv.Itoa(torrent.DBItem.Episode)
				}
			}

			playUrl := UrlQuery(UrlForXBMC("/play"),
				"resume",  strconv.Itoa(i),
				"type",    contentType,
				"tmdb",    tmdb,
				"show",    show,
				"season",  season,
				"episode", episode)

			item := xbmc.ListItem{
				Label: fmt.Sprintf("%.2f%% - [COLOR %s]%s[/COLOR] - %s", progress, color, status, torrentName),
				Path: playUrl,
				Info: &xbmc.ListItemInfo{
					Title: torrentName,
				},
			}
			item.ContextMenu = [][]string{
				[]string{"LOCALIZE[30230]", fmt.Sprintf("XBMC.PlayMedia(%s)", playUrl)},
				torrentAction,
				[]string{"LOCALIZE[30232]", fmt.Sprintf("XBMC.RunPlugin(%s)", UrlForXBMC("/torrents/delete/%d", i))},
				[]string{"LOCALIZE[30276]", fmt.Sprintf("XBMC.RunPlugin(%s)", UrlForXBMC("/torrents/delete/%d?files=1", i))},
				[]string{"LOCALIZE[30308]", fmt.Sprintf("XBMC.RunPlugin(%s)", UrlForXBMC("/torrents/move/%d", i))},
				sessionAction,
			}
			item.IsPlayable = true
			items = append(items, &item)
		}

		ctx.JSON(200, xbmc.NewView("", items))
	}
}

func ListTorrentsWeb(btService *bittorrent.BTService) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		torrents := make([]*TorrentsWeb, 0, len(btService.Torrents))

		torrentsLog.Info("Currently downloading:")
		for _, torrent := range btService.Torrents {
			if torrent == nil {
				continue
			}

			torrentName := torrent.Name()
			progress    := torrent.GetProgress()
			status      := torrent.GetStateString()

			if status != "Finished" {
				if progress >= 100 {
					status = "Finished"
				} else {
					status = "Downloading"
				}
			} else if status == "Finished" || progress >= 100 {
				status = "Seeding"
			}

			size         := humanize.Bytes(uint64( torrent.Length() ))
			downloadRate := float64(torrent.DownloadRate) / 1024
			uploadRate   := float64(torrent.UploadRate) / 1024

			stats        := torrent.Stats()
			peers        := stats.ActivePeers
			peersTotal   := stats.TotalPeers

			t := TorrentsWeb{
				Name: torrentName,
				Size: size,
				Status: status,
				Progress: progress,
				DownloadRate: downloadRate,
				UploadRate: uploadRate,
				Peers: peers,
				PeersTotal: peersTotal,
			}
			torrents = append(torrents, &t)
		}

		ctx.Writer.Header().Set("Access-Control-Allow-Origin", "*")
		ctx.JSON(200, torrents)
	}
}

func PauseSession(btService *bittorrent.BTService) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		// TODO: Add Global Pause
		xbmc.Refresh()
		ctx.Writer.Header().Set("Access-Control-Allow-Origin", "*")
		ctx.String(200, "")
	}
}

func ResumeSession(btService *bittorrent.BTService) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		// TODO: Add Global Resume
		xbmc.Refresh()
		ctx.Writer.Header().Set("Access-Control-Allow-Origin", "*")
		ctx.String(200, "")
	}
}

func AddTorrent(btService *bittorrent.BTService) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		uri := ctx.Query("uri")
		ctx.Writer.Header().Set("Access-Control-Allow-Origin", "*")

		if uri == "" {
			ctx.String(404, "Missing torrent URI")
		}
		torrentsLog.Infof("Adding torrent from %s", uri)

		if config.Get().DownloadPath == "." {
			xbmc.Notify("Quasar", "LOCALIZE[30113]", config.AddonIcon())
			ctx.String(404, "Download path empty")
			return
		}

		_, err := btService.AddTorrent(uri)
		if err != nil {
			ctx.String(404, err.Error())
			return
		}

		torrentsLog.Infof("Downloading %s", uri)

		xbmc.Refresh()
		ctx.String(200, "")
	}
}

func ResumeTorrent(btService *bittorrent.BTService) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		torrentId := ctx.Params.ByName("torrentId")
		torrentIndex, _ := strconv.Atoi(torrentId)

		if len(btService.Torrents) >= torrentIndex {
			ctx.Error(errors.New(fmt.Sprintf("Unable to resume torrent with index %d", torrentIndex)))
		}

		btService.Torrents[torrentIndex].Resume()

		xbmc.Refresh()
		ctx.Writer.Header().Set("Access-Control-Allow-Origin", "*")
		ctx.String(200, "")
	}
}

func MoveTorrent(btService *bittorrent.BTService) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		torrentId := ctx.Params.ByName("torrentId")
		torrentIndex, _ := strconv.Atoi(torrentId)

		if len(btService.Torrents) >= torrentIndex {
			ctx.Error(errors.New(fmt.Sprintf("Unable to pause torrent with index %d", torrentIndex)))
		}

		torrentsLog.Infof("Marking %s to be moved...", btService.Torrents[torrentIndex].Name())
		btService.MarkedToMove = torrentIndex

		xbmc.Refresh()
		ctx.Writer.Header().Set("Access-Control-Allow-Origin", "*")
		ctx.String(200, "")
	}
}

func PauseTorrent(btService *bittorrent.BTService) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		torrentId := ctx.Params.ByName("torrentId")
		torrentIndex, _ := strconv.Atoi(torrentId)

		if len(btService.Torrents) >= torrentIndex {
			ctx.Error(errors.New(fmt.Sprintf("Unable to pause torrent with index %d", torrentIndex)))
		}

		btService.Torrents[torrentIndex].Pause()

		xbmc.Refresh()
		ctx.Writer.Header().Set("Access-Control-Allow-Origin", "*")
		ctx.String(200, "")
	}
}

func RemoveTorrent(btService *bittorrent.BTService) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		deleteFiles := ctx.Query("files")
		torrentId := ctx.Params.ByName("torrentId")
		torrentIndex, _ := strconv.Atoi(torrentId)

		if len(btService.Torrents) >= torrentIndex {
			ctx.Error(errors.New(fmt.Sprintf("Unable to pause torrent with index %d", torrentIndex)))
		}

		torrentsPath := config.Get().TorrentsPath
		torrentHandle := btService.Torrents[torrentIndex]

		if torrentHandle == nil {
			ctx.Error(errors.New("Invalid torrent handle"))
		}

		// Delete torrent file
		infoHash := torrentHandle.InfoHash()
		torrentFile := filepath.Join(torrentsPath, fmt.Sprintf("%s.torrent", infoHash))
		if _, err := os.Stat(torrentFile); err == nil {
			torrentsLog.Infof("Deleting torrent file at %s", torrentFile)
			defer os.Remove(torrentFile)
		}

		btService.UpdateDB(bittorrent.Delete, infoHash, 0, "")
		torrentsLog.Infof("Removed %s from database", infoHash)

		keepSetting := config.Get().KeepFilesFinished
		deleteAnswer := false
		if keepSetting == 1 && deleteFiles == "" && xbmc.DialogConfirm("Quasar", "LOCALIZE[30269]") {
			deleteAnswer = true
		} else if keepSetting == 2 {
			deleteAnswer = true
		}

		if deleteAnswer == true || deleteFiles == "true" {
			torrentsLog.Info("Removing the torrent and deleting files...")
			btService.RemoveTorrent(torrentHandle, true)
		} else {
			torrentsLog.Info("Removing the torrent without deleting files...")
			btService.RemoveTorrent(torrentHandle, false)
		}

		xbmc.Refresh()
		ctx.Writer.Header().Set("Access-Control-Allow-Origin", "*")
		ctx.String(200, "")
	}
}

func Versions(btService *bittorrent.BTService) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		type Versions struct {
			Version    string `json:"version"`
			UserAgent  string `json:"user-agent"`
		}
		versions := Versions{
			Version:    util.Version[1:len(util.Version) - 1],
			UserAgent:  btService.UserAgent,
		}
		ctx.Writer.Header().Set("Access-Control-Allow-Origin", "*")
		ctx.JSON(200, versions)
	}
}

package api

import (
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"mime/multipart"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/dustin/go-humanize"
	"github.com/gin-gonic/gin"
	"github.com/op/go-logging"

	"github.com/elgatito/elementum/bittorrent"
	"github.com/elgatito/elementum/config"
	"github.com/elgatito/elementum/database"
	"github.com/elgatito/elementum/util"
	"github.com/elgatito/elementum/xbmc"
)

var (
	torrentsLog    = logging.MustGetLogger("torrents")
	cachedTorrents = map[int]string{}
)

// TorrentsWeb ...
type TorrentsWeb struct {
	ID            string  `json:"id"`
	Name          string  `json:"name"`
	Size          string  `json:"size"`
	Status        string  `json:"status"`
	Progress      float64 `json:"progress"`
	Ratio         float64 `json:"ratio"`
	TimeRatio     float64 `json:"time_ratio"`
	SeedingTime   string  `json:"seeding_time"`
	SeedTime      float64 `json:"seed_time"`
	SeedTimeLimit int     `json:"seed_time_limit"`
	DownloadRate  float64 `json:"download_rate"`
	UploadRate    float64 `json:"upload_rate"`
	Seeders       int     `json:"seeders"`
	SeedersTotal  int     `json:"seeders_total"`
	Peers         int     `json:"peers"`
	PeersTotal    int     `json:"peers_total"`
}

// AddToTorrentsMap ...
func AddToTorrentsMap(tmdbID string, torrent *bittorrent.TorrentFile) {
	if strings.HasPrefix(torrent.URI, "magnet") {
		torrentsLog.Debugf("Saving torrent entry for TMDB: %#v", tmdbID)
		if b, err := torrent.MarshalJSON(); err == nil {
			database.Get().AddTorrentHistory(tmdbID, torrent.InfoHash, b)
		}
		
		return
	}

	b, err := ioutil.ReadFile(torrent.URI)
	if err != nil {
		return
	}

	torrentsLog.Debugf("Saving torrent entry for TMDB: %#v", tmdbID)
	database.Get().AddTorrentHistory(tmdbID, torrent.InfoHash, b)
}

// InTorrentsMap ...
func InTorrentsMap(tmdbID string) *bittorrent.TorrentFile {
	if !config.Get().UseCacheSelection {
		return nil
	}

	var infohash string
	var infohashID int64
	var b []byte
	database.Get().QueryRow(`SELECT l.infohash_id, i.infohash, i.metainfo FROM thistory_assign l LEFT JOIN thistory_metainfo i ON i.rowid = l.infohash_id WHERE l.item_id = ?`, tmdbID).Scan(&infohashID, &infohash, &b)

	if len(infohash) > 0 && len(b) > 0 {
		torrent := &bittorrent.TorrentFile{}
		if b[0] == '{' {
			torrent.UnmarshalJSON(b)
		} else {
			torrent.LoadFromBytes(b)
		}

		if len(torrent.URI) > 0 && (config.Get().SilentStreamStart || xbmc.DialogConfirmFocused("Elementum", fmt.Sprintf("LOCALIZE[30260];;[COLOR gold]%s[/COLOR]", torrent.Title), xbmc.DialogExpiration.InTorrents)) {
			return torrent
		}

		database.Get().Exec(`DELETE FROM thistory_assign WHERE item_id = ?`, tmdbID)
		var left int
		database.Get().QueryRow(`SELECT COUNT(*) FROM thistory_assign WHERE infohash_id = ?`, infohashID).Scan(&left)
		if left == 0 {
			database.Get().Exec(`DELETE FROM thistory_metainfo WHERE rowid = ?`, infohashID)
		}
	}

	return nil
}

// GetCachedTorrents searches for torrent entries in the cache
func GetCachedTorrents(tmdbID string) ([]*bittorrent.TorrentFile, error) {
	if !config.Get().UseCacheSearch {
		return nil, fmt.Errorf("Caching is disabled")
	}

	cacheDB := database.GetCache()

	var ret []*bittorrent.TorrentFile
	err := cacheDB.GetCachedObject(database.CommonBucket, tmdbID, &ret)
	if len(ret) > 0 {
		for _, t := range ret {
			if !strings.HasPrefix(t.URI, "magnet:") {
				if _, err = os.Open(t.URI); err != nil {
					return nil, fmt.Errorf("Cache is not up to date")
				}
			}
		}
	}

	return ret, err
}

// SetCachedTorrents caches torrent search results in cache
func SetCachedTorrents(tmdbID string, torrents []*bittorrent.TorrentFile) error {
	cacheDB := database.GetCache()

	return cacheDB.SetCachedObject(database.CommonBucket, config.Get().CacheSearchDuration, tmdbID, torrents)
}

// ListTorrents ...
func ListTorrents(btService *bittorrent.BTService) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		items := make(xbmc.ListItems, 0, len(btService.Torrents))
		if len(btService.Torrents) == 0 {
			ctx.JSON(200, xbmc.NewView("", items))
			return
		}

		// torrentsLog.Debug("Currently downloading:")
		for i, torrent := range btService.Torrents {
			if torrent == nil {
				continue
			}

			torrentName := torrent.Name()
			progress := torrent.GetProgress()
			status := torrent.GetStateString()

			torrentAction := []string{"LOCALIZE[30231]", fmt.Sprintf("XBMC.RunPlugin(%s)", URLForXBMC("/torrents/pause/%s", i))}
			sessionAction := []string{"LOCALIZE[30233]", fmt.Sprintf("XBMC.RunPlugin(%s)", URLForXBMC("/torrents/pause"))}

			if status == "Paused" {
				sessionAction = []string{"LOCALIZE[30234]", fmt.Sprintf("XBMC.RunPlugin(%s)", URLForXBMC("/torrents/resume"))}
			} else if status != "Finished" {
				torrentAction = []string{"LOCALIZE[30235]", fmt.Sprintf("XBMC.RunPlugin(%s)", URLForXBMC("/torrents/resume/%s", i))}
			}

			color := "white"
			switch status {
			case statusPaused:
				fallthrough
			case statusFinished:
				color = "grey"
			case statusSeeding:
				color = "green"
			case statusBuffering:
				color = "blue"
			case statusFinding:
				color = "orange"
			case statusChecking:
				color = "teal"
			case statusQueued:
			case statusAllocating:
				color = "black"
			case statusStalled:
				color = "red"
			}

			// TODO: Add seeding time and ratio getter/output
			// torrentsLog.Debugf("- %.2f%% - %s - %s", progress, status, torrentName)

			var (
				tmdb        string
				show        string
				season      string
				episode     string
				contentType string
			)

			if torrent.DBItem != nil && torrent.DBItem.Type != "" {
				contentType = torrent.DBItem.Type
				if contentType == movieType {
					tmdb = strconv.Itoa(torrent.DBItem.ID)
				} else {
					show = strconv.Itoa(torrent.DBItem.ShowID)
					season = strconv.Itoa(torrent.DBItem.Season)
					episode = strconv.Itoa(torrent.DBItem.Episode)
				}
			}

			playURL := URLQuery(URLForXBMC("/play"),
				"resume", i,
				"type", contentType,
				"tmdb", tmdb,
				"show", show,
				"season", season,
				"episode", episode)

			item := xbmc.ListItem{
				Label: fmt.Sprintf("%.2f%% - [COLOR %s]%s[/COLOR] - %s", progress, color, status, torrentName),
				Path:  playURL,
				Info: &xbmc.ListItemInfo{
					Title: torrentName,
				},
			}
			item.ContextMenu = [][]string{
				[]string{"LOCALIZE[30230]", fmt.Sprintf("XBMC.PlayMedia(%s)", playURL)},
				torrentAction,
				[]string{"LOCALIZE[30232]", fmt.Sprintf("XBMC.RunPlugin(%s)", URLForXBMC("/torrents/delete/%s", i))},
				[]string{"LOCALIZE[30276]", fmt.Sprintf("XBMC.RunPlugin(%s)", URLForXBMC("/torrents/delete/%s?files=1", i))},
				[]string{"LOCALIZE[30308]", fmt.Sprintf("XBMC.RunPlugin(%s)", URLForXBMC("/torrents/move/%s", i))},
				sessionAction,
			}
			item.IsPlayable = true
			items = append(items, &item)
		}

		ctx.JSON(200, xbmc.NewView("", items))
	}
}

// ListTorrentsWeb ...
func ListTorrentsWeb(btService *bittorrent.BTService) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		torrents := make([]*TorrentsWeb, 0, len(btService.Torrents))

		if len(btService.Torrents) == 0 {
			ctx.Writer.Header().Set("Access-Control-Allow-Origin", "*")
			ctx.JSON(200, torrents)
			return
		}

		// torrentsLog.Debugf("Currently downloading:")
		for _, torrent := range btService.Torrents {
			if torrent == nil {
				continue
			}

			torrentName := torrent.Name()
			progress := torrent.GetProgress()
			status := torrent.GetStateString()

			// if status != statusFinished {
			// 	if progress >= 100 {
			// 		status = statusFinished
			// 	} else {
			// 		status = statusDownloading
			// 	}
			// } else if status == statusFinished || progress >= 100 {
			// 	status = statusSeeding
			// }

			size := humanize.Bytes(uint64(torrent.Length()))
			downloadRate := float64(torrent.DownloadRate) / 1024
			uploadRate := float64(torrent.UploadRate) / 1024

			stats := torrent.Stats()
			peers := stats.ActivePeers
			peersTotal := stats.TotalPeers

			t := TorrentsWeb{
				ID:           torrent.InfoHash(),
				Name:         torrentName,
				Size:         size,
				Status:       status,
				Progress:     progress,
				DownloadRate: downloadRate,
				UploadRate:   uploadRate,
				Peers:        peers,
				PeersTotal:   peersTotal,
			}
			torrents = append(torrents, &t)

			// torrentsLog.Debugf("- %.2f%% - %s - %s", progress, status, torrentName)
		}

		ctx.Writer.Header().Set("Access-Control-Allow-Origin", "*")
		ctx.JSON(200, torrents)
	}
}

// PauseSession ...
func PauseSession(btService *bittorrent.BTService) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		// TODO: Add Global Pause
		xbmc.Refresh()
		ctx.Writer.Header().Set("Access-Control-Allow-Origin", "*")
		ctx.String(200, "")
	}
}

// ResumeSession ...
func ResumeSession(btService *bittorrent.BTService) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		// TODO: Add Global Resume
		xbmc.Refresh()
		ctx.Writer.Header().Set("Access-Control-Allow-Origin", "*")
		ctx.String(200, "")
	}
}

// AddTorrent ...
func AddTorrent(btService *bittorrent.BTService) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		uri := ctx.Request.FormValue("uri")
		file, header, fileError := ctx.Request.FormFile("file")

		if file != nil && header != nil && fileError == nil {
			t, err := saveTorrentFile(file, header)
			if err == nil && t != "" {
				uri = t
			}
		}

		ctx.Writer.Header().Set("Access-Control-Allow-Origin", "*")

		if uri == "" {
			ctx.String(404, "Missing torrent URI")
			return
		}
		torrentsLog.Infof("Adding torrent from %s", uri)

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

// ResumeTorrent ...
func ResumeTorrent(btService *bittorrent.BTService) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		torrentID := ctx.Params.ByName("torrentId")
		torrent, err := GetTorrentFromParam(btService, torrentID)
		if err != nil {
			ctx.Error(fmt.Errorf("Unable to resume torrent with index %s", torrentID))
			return
		}

		torrent.Resume()

		xbmc.Refresh()
		ctx.Writer.Header().Set("Access-Control-Allow-Origin", "*")
		ctx.String(200, "")
	}
}

// MoveTorrent ...
func MoveTorrent(btService *bittorrent.BTService) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		torrentID := ctx.Params.ByName("torrentId")
		torrent, err := GetTorrentFromParam(btService, torrentID)
		if err != nil {
			ctx.Error(fmt.Errorf("Unable to move torrent with index %s", torrentID))
			return
		}

		torrentsLog.Infof("Marking %s to be moved...", torrent.Name())
		btService.MarkedToMove = torrent.InfoHash()

		xbmc.Refresh()
		ctx.Writer.Header().Set("Access-Control-Allow-Origin", "*")
		ctx.String(200, "")
	}
}

// PauseTorrent ...
func PauseTorrent(btService *bittorrent.BTService) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		torrentID := ctx.Params.ByName("torrentId")
		torrent, err := GetTorrentFromParam(btService, torrentID)
		if err != nil {
			ctx.Error(fmt.Errorf("Unable to pause torrent with index %s", torrentID))
			return
		}

		torrent.Pause()

		xbmc.Refresh()
		ctx.Writer.Header().Set("Access-Control-Allow-Origin", "*")
		ctx.String(200, "")
	}
}

// RemoveTorrent ...
func RemoveTorrent(btService *bittorrent.BTService) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		deleteFiles := ctx.Query("files")

		torrentID := ctx.Params.ByName("torrentId")
		torrent, err := GetTorrentFromParam(btService, torrentID)
		if err != nil {
			ctx.Error(fmt.Errorf("Unable to remove torrent with index %s", torrentID))
			return
		}

		// Delete torrent file
		torrentsPath := config.Get().TorrentsPath
		infoHash := torrent.InfoHash()
		torrentFile := filepath.Join(torrentsPath, fmt.Sprintf("%s.torrent", infoHash))
		if _, err := os.Stat(torrentFile); err == nil {
			defer os.Remove(torrentFile)
		}

		torrentsLog.Infof("Removed %s from database", infoHash)

		keepSetting := config.Get().KeepFilesFinished
		deleteAnswer := false
		if keepSetting == 1 && deleteFiles == "" && xbmc.DialogConfirm("Elementum", "LOCALIZE[30269]") {
			deleteAnswer = true
		} else if keepSetting == 2 {
			deleteAnswer = true
		}

		if deleteAnswer == true || deleteFiles == trueType {
			torrentsLog.Info("Removing the torrent and deleting files from the web ...")
			btService.RemoveTorrent(torrent, true)
		} else {
			torrentsLog.Info("Removing the torrent without deleting files from the web ...")
			btService.RemoveTorrent(torrent, false)
		}

		xbmc.Refresh()
		ctx.Writer.Header().Set("Access-Control-Allow-Origin", "*")
		ctx.String(200, "")
	}
}

// Versions ...
func Versions(btService *bittorrent.BTService) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		type Versions struct {
			Version   string `json:"version"`
			UserAgent string `json:"user-agent"`
		}
		versions := Versions{
			Version:   util.GetVersion(),
			UserAgent: btService.UserAgent,
		}
		ctx.Writer.Header().Set("Access-Control-Allow-Origin", "*")
		ctx.JSON(200, versions)
	}
}

// GetTorrentFromParam ...
func GetTorrentFromParam(btService *bittorrent.BTService, param string) (*bittorrent.Torrent, error) {
	if len(param) == 0 {
		return nil, errors.New("Empty param")
	}

	t, ok := btService.Torrents[param]
	if !ok {
		return nil, errors.New("Torrent not found")
	}
	return t, nil
}

func saveTorrentFile(file multipart.File, header *multipart.FileHeader) (string, error) {
	if file == nil || header == nil {
		return "", fmt.Errorf("Not a valid file entry")
	}

	var err error
	path := filepath.Join(config.Get().TorrentsPath, filepath.Base(header.Filename))
	log.Debugf("Saving incoming torrent file to: %s", path)

	if _, err = os.Stat(path); err != nil && !os.IsNotExist(err) {
		err = os.Remove(path)
		if err != nil {
			return "", fmt.Errorf("Could not remove the file: %s", err)
		}
	}

	out, err := os.Create(path)
	if err != nil {
		return "", fmt.Errorf("Could not create file: %s", err)
	}
	defer out.Close()
	if _, err = io.Copy(out, file); err != nil {
		return "", fmt.Errorf("Could not write file content: %s", err)
	}

	return path, nil
}

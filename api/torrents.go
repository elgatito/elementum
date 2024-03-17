package api

import (
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/anacrolix/missinggo/perf"
	"github.com/dustin/go-humanize"
	"github.com/gin-gonic/gin"
	"github.com/op/go-logging"

	"github.com/elgatito/elementum/bittorrent"
	"github.com/elgatito/elementum/config"
	"github.com/elgatito/elementum/database"
	"github.com/elgatito/elementum/util"
	"github.com/elgatito/elementum/util/ident"
	"github.com/elgatito/elementum/xbmc"
)

var (
	torrentsLog = logging.MustGetLogger("torrents")
)

// TorrentsWeb ...
type TorrentsWeb struct {
	ID            string  `json:"id"`
	Name          string  `json:"name"`
	AddedTime     int64   `json:"added_time"`
	Size          string  `json:"size"`
	SizeBytes     int64   `json:"size_bytes"`
	Status        string  `json:"status"`
	StatusCode    int     `json:"status_code"`
	Progress      float64 `json:"progress"`
	Ratio         float64 `json:"ratio"`
	TimeRatio     float64 `json:"time_ratio"`
	SeedingTime   string  `json:"seeding_time"`
	SeedTime      float64 `json:"seed_time"`
	SeedTimeLimit int     `json:"seed_time_limit"`
	DownloadRate  float64 `json:"download_rate"`
	UploadRate    float64 `json:"upload_rate"`
	TotalDownload float64 `json:"total_download"`
	TotalUpload   float64 `json:"total_upload"`
	Seeders       int     `json:"seeders"`
	SeedersTotal  int     `json:"seeders_total"`
	Peers         int     `json:"peers"`
	PeersTotal    int     `json:"peers_total"`
}

// AddToTorrentsMap ...
func AddToTorrentsMap(tmdbID string, torrent *bittorrent.TorrentFile) {
	defer perf.ScopeTimer()()

	if strings.HasPrefix(torrent.URI, "magnet") {
		torrentsLog.Debugf("Saving torrent entry for TMDB: %#v", tmdbID)
		if b, err := torrent.MarshalJSON(); err == nil {
			database.GetStorm().AddTorrentLink(tmdbID, torrent.InfoHash, b, false)
		}

		return
	}

	b, err := os.ReadFile(torrent.URI)
	if err != nil {
		return
	}

	torrentsLog.Debugf("Saving torrent entry for TMDB: %#v", tmdbID)
	database.GetStorm().AddTorrentLink(tmdbID, torrent.InfoHash, b, false)
}

// AssignTorrent assigns torrent by its id to elementum's item by its TMDB id
func AssignTorrent(s *bittorrent.Service) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		xbmcHost, _ := xbmc.GetXBMCHostWithContext(ctx)

		torrentID := ctx.Params.ByName("torrentId")

		var infoHash string
		var metadata []byte
		var found bool

		//Try to find torrent in torrents history first
		if config.Get().UseTorrentHistory {
			var th database.TorrentHistory
			if err := database.GetStormDB().One("InfoHash", torrentID, &th); err == nil {
				infoHash = th.InfoHash
				metadata = th.Metadata
				found = true
			}
		}

		//Try to find torrent in active torrents list
		if !found {
			torrent, err := GetTorrentFromParam(s, torrentID)
			if err != nil {
				log.Error(err.Error())
				xbmcHost.Notify("Elementum", err.Error(), config.AddonIcon())
				ctx.Error(err)
				return
			}
			infoHash = torrent.InfoHash()
			metadata = torrent.GetMetadata()
		}

		tmdbID := ctx.Params.ByName("tmdbId")

		log.Infof("AssignTorrent %s to tmdbID %s", torrentID, tmdbID)

		// make old torrent disappear from "found in active torrents" dialog in runtime
		tmdbInt, _ := strconv.Atoi(tmdbID)
		var ti database.TorrentAssignItem
		if err := database.GetStormDB().One("TmdbID", tmdbInt, &ti); err == nil {
			// check that old torrent is not equal to chosen torrent
			oldInfoHash := ti.InfoHash
			if oldInfoHash != infoHash {
				oldTorrent := s.GetTorrentByHash(oldInfoHash)
				if oldTorrent != nil {
					oldTorrent.DBItem.ID = 0
					oldTorrent.DBItem.ShowID = 0
				}
			}
		}

		database.GetStorm().AddTorrentLink(tmdbID, infoHash, metadata, false)

		// TODO: if we will pass media type and season/episode number to this func, then we also can
		// update torrent's DBItem in queue so it will be used in "found in active torrents" dialog in runtime
		// torrent.DBItem.ID/ShowID/Season/Episode = tmdbInt/...
		// but this will make things uglier and it does not give much value

		ctx.JSON(200, nil)
	}
}

// InTorrentsMap ...
func InTorrentsMap(xbmcHost *xbmc.XBMCHost, tmdbID string) *bittorrent.TorrentFile {
	if !config.Get().UseCacheSelection || tmdbID == "" {
		return nil
	}

	defer perf.ScopeTimer()()

	tmdbInt, _ := strconv.Atoi(tmdbID)
	var ti database.TorrentAssignItem
	var tm database.TorrentAssignMetadata
	if err := database.GetStormDB().One("TmdbID", tmdbInt, &ti); err != nil {
		return nil
	}
	if err := database.GetStormDB().One("InfoHash", ti.InfoHash, &tm); err != nil {
		return nil
	}

	torrent := &bittorrent.TorrentFile{}
	if tm.Metadata[0] == '{' {
		torrent.UnmarshalJSON(tm.Metadata)
	} else {
		torrent.LoadFromBytes(tm.Metadata)
	}

	if len(torrent.URI) > 0 && (config.Get().SilentStreamStart || xbmcHost.DialogConfirmFocused("Elementum", fmt.Sprintf("LOCALIZE[30260];;[B]%s[/B]", torrent.Title))) {
		return torrent
	}

	database.GetStormDB().DeleteStruct(&ti)
	database.GetStorm().CleanupTorrentLink(ti.InfoHash)

	return nil
}

// InTorrentsHistory ...
func InTorrentsHistory(infohash string) *bittorrent.TorrentFile {
	if !config.Get().UseTorrentHistory || infohash == "" {
		return nil
	}

	defer perf.ScopeTimer()()

	var th database.TorrentHistory
	if err := database.GetStormDB().One("InfoHash", infohash, &th); err != nil {
		return nil
	}

	if len(infohash) > 0 && len(th.Metadata) > 0 {
		torrent := &bittorrent.TorrentFile{}
		if th.Metadata[0] == '{' {
			torrent.UnmarshalJSON(th.Metadata)
		} else {
			torrent.LoadFromBytes(th.Metadata)
		}

		if len(torrent.URI) > 0 {
			return torrent
		}
	}

	return nil
}

// GetCachedTorrents searches for torrent entries in the cache
func GetCachedTorrents(tmdbID string) ([]*bittorrent.TorrentFile, error) {
	defer perf.ScopeTimer()()

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
func ListTorrents(s *bittorrent.Service) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		defer perf.ScopeTimer()()

		xbmcHost, _ := xbmc.GetXBMCHostWithContext(ctx)

		items := make(xbmc.ListItems, 0, len(s.GetTorrents()))
		if len(s.GetTorrents()) == 0 {
			ctx.JSON(200, xbmc.NewView("", items))
			return
		}

		for _, t := range s.GetTorrents() {
			if t == nil || t.Closer.IsSet() || s.Closer.IsSet() {
				continue
			}

			torrentName := t.Name()
			progress := t.GetProgress()
			statusCode := t.GetSmartState()
			status := xbmcHost.Translate(bittorrent.StatusStrings[statusCode])

			torrentAction := []string{"LOCALIZE[30231]", fmt.Sprintf("RunPlugin(%s)", URLForXBMC("/torrents/pause/%s", t.InfoHash()))}
			sessionAction := []string{"LOCALIZE[30233]", fmt.Sprintf("RunPlugin(%s)", URLForXBMC("/torrents/pause"))}

			if s.Session.IsPaused() {
				sessionAction = []string{"LOCALIZE[30234]", fmt.Sprintf("RunPlugin(%s)", URLForXBMC("/torrents/resume"))}
			} else if t.GetPaused() {
				torrentAction = []string{"LOCALIZE[30235]", fmt.Sprintf("RunPlugin(%s)", URLForXBMC("/torrents/resume/%s", t.InfoHash()))}
			}

			color := "white"
			switch status {
			case bittorrent.StatusStrings[bittorrent.StatusPaused]:
				fallthrough
			case bittorrent.StatusStrings[bittorrent.StatusFinished]:
				color = "grey"
			case bittorrent.StatusStrings[bittorrent.StatusSeeding]:
				color = "green"
			case bittorrent.StatusStrings[bittorrent.StatusBuffering]:
				color = "blue"
			case bittorrent.StatusStrings[bittorrent.StatusFinding]:
				color = "orange"
			case bittorrent.StatusStrings[bittorrent.StatusChecking]:
				color = "teal"
			case bittorrent.StatusStrings[bittorrent.StatusFinding]:
				color = "orange"
			case bittorrent.StatusStrings[bittorrent.StatusAllocating]:
				color = "black"
			case bittorrent.StatusStrings[bittorrent.StatusStalled]:
				color = "red"
			}

			playURL := t.GetPlayURL("")

			item := xbmc.ListItem{
				Label: fmt.Sprintf("%.2f%% - %s - %s", progress, util.ApplyColor(status, color), torrentName),
				Path:  playURL,
				Info: &xbmc.ListItemInfo{
					Title: torrentName,
				},
			}

			item.ContextMenu = [][]string{
				{"LOCALIZE[30230]", fmt.Sprintf("PlayMedia(%s)", playURL)},
				torrentAction,
				{"LOCALIZE[30232]", fmt.Sprintf("RunPlugin(%s)", URLForXBMC("/torrents/delete/%s", t.InfoHash()))},
				{"LOCALIZE[30276]", fmt.Sprintf("RunPlugin(%s)", URLForXBMC("/torrents/delete/%s?files=true", t.InfoHash()))},
				{"LOCALIZE[30308]", fmt.Sprintf("RunPlugin(%s)", URLForXBMC("/torrents/move/%s", t.InfoHash()))},
				sessionAction,
			}

			if !t.IsMemoryStorage() {
				item.ContextMenu = append(item.ContextMenu, []string{"LOCALIZE[30573]", fmt.Sprintf("RunPlugin(%s)", URLForXBMC("/torrents/selectfile/%s", t.InfoHash()))})
				item.ContextMenu = append(item.ContextMenu, []string{"LOCALIZE[30612]", fmt.Sprintf("RunPlugin(%s)", URLForXBMC("/torrents/downloadfile/%s", t.InfoHash()))})

				if t.HasAvailableFiles() {
					item.ContextMenu = append(item.ContextMenu, []string{"LOCALIZE[30531]", fmt.Sprintf("RunPlugin(%s)", URLForXBMC("/torrents/downloadall/%s", t.InfoHash()))})
				} else {
					item.ContextMenu = append(item.ContextMenu, []string{"LOCALIZE[30532]", fmt.Sprintf("RunPlugin(%s)", URLForXBMC("/torrents/undownloadall/%s", t.InfoHash()))})
				}
			}

			item.IsPlayable = true
			items = append(items, &item)
		}

		ctx.JSON(200, xbmc.NewView("", items))
	}
}

// ListTorrentsWeb ...
func ListTorrentsWeb(s *bittorrent.Service) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		if s.Closer.IsSet() {
			return
		}

		defer perf.ScopeTimer()()

		xbmcHost, _ := xbmc.GetXBMCHostWithContext(ctx)

		items := make([]*TorrentsWeb, 0, len(s.GetTorrents()))
		if len(s.GetTorrents()) == 0 {
			ctx.JSON(200, items)
			return
		}

		seedTimeLimit := config.Get().SeedTimeLimit

		for _, t := range s.GetTorrents() {
			th := t.GetHandle()
			if th == nil || !th.IsValid() || !t.HasMetadata() || t.Closer.IsSet() || s.Closer.IsSet() {
				continue
			}

			torrentStatus := t.GetLastStatus(false)

			torrentName := torrentStatus.GetName()
			addedTime := t.GetAddedTime().Unix()
			progress := float64(torrentStatus.GetProgress()) * 100

			infoHash := t.InfoHash()

			statusCode := t.GetSmartState()
			status := xbmcHost.Translate(bittorrent.StatusStrings[statusCode])

			ratio := float64(0)
			allTimeDownload := float64(torrentStatus.GetAllTimeDownload())
			allTimeUpload := float64(torrentStatus.GetAllTimeUpload())
			if allTimeDownload > 0 {
				ratio = allTimeUpload / allTimeDownload
			}

			timeRatio := float64(0)
			finishedTime := float64(torrentStatus.GetFinishedTime())
			downloadTime := float64(torrentStatus.GetActiveTime()) - finishedTime
			if downloadTime > 1 {
				timeRatio = finishedTime / downloadTime
			}
			seedingTime := time.Duration(torrentStatus.GetSeedingTime()) * time.Second
			if progress == 100 && seedingTime == 0 {
				seedingTime = time.Duration(finishedTime) * time.Second
			}

			sizeBytes := t.GetSelectedSize()
			size := humanize.Bytes(uint64(sizeBytes))

			downloadRate := float64(torrentStatus.GetDownloadPayloadRate()) / 1024
			uploadRate := float64(torrentStatus.GetUploadPayloadRate()) / 1024

			seeders, seedersTotal, peers, peersTotal := t.GetConnections()

			ti := &TorrentsWeb{
				ID:            infoHash,
				Name:          torrentName,
				AddedTime:     addedTime,
				Size:          size,
				SizeBytes:     sizeBytes,
				Status:        status,
				StatusCode:    statusCode,
				Progress:      progress,
				Ratio:         ratio,
				TimeRatio:     timeRatio,
				SeedingTime:   seedingTime.String(),
				SeedTime:      seedingTime.Seconds(),
				SeedTimeLimit: seedTimeLimit,
				DownloadRate:  downloadRate,
				UploadRate:    uploadRate,
				TotalDownload: allTimeDownload,
				TotalUpload:   allTimeUpload,
				Seeders:       seeders,
				SeedersTotal:  seedersTotal,
				Peers:         peers,
				PeersTotal:    peersTotal,
			}
			items = append(items, ti)
		}

		ctx.JSON(200, items)
	}
}

// PauseSession ...
func PauseSession(s *bittorrent.Service) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		defer perf.ScopeTimer()()

		xbmcHost, _ := xbmc.GetXBMCHostWithContext(ctx)

		s.Session.Pause()

		xbmcHost.Refresh()
		ctx.String(200, "")
	}
}

// ResumeSession ...
func ResumeSession(s *bittorrent.Service) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		defer perf.ScopeTimer()()

		xbmcHost, _ := xbmc.GetXBMCHostWithContext(ctx)

		s.Session.Resume()

		xbmcHost.Refresh()
		ctx.String(200, "")
	}
}

// AddTorrent ...
func AddTorrent(s *bittorrent.Service) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		defer perf.ScopeTimer()()

		xbmcHost, _ := xbmc.GetXBMCHostWithContext(ctx)

		uri := ctx.Request.FormValue("uri")
		file, header, fileError := ctx.Request.FormFile("file")
		allFiles := ctx.Request.FormValue("all")

		if file != nil && header != nil && fileError == nil {
			t, err := saveTorrentFile(file, header)
			if err == nil && t != "" {
				uri = t
			}
		}

		if uri == "" {
			torrentsLog.Errorf("Torrent file/magnet url is empty")
			ctx.String(404, "Missing torrent URI")
			return
		}
		torrentsLog.Infof("Adding torrent from %s", uri)

		var t *bittorrent.Torrent
		var resume string
		if t = s.GetTorrentByURI(uri); t == nil {
			//try to get hash so we can try to find torrent by hash later
			torrent := bittorrent.NewTorrentFile(uri)
			if err := torrent.Resolve(); err == nil {
				resume = torrent.InfoHash
			}
		}

		if resume != "" {
			t = s.GetTorrentByHash(resume)
		}

		if t == nil {
			var err error
			t, err = s.AddTorrent(xbmcHost, bittorrent.AddOptions{URI: uri, Paused: false, DownloadStorage: config.Get().DownloadStorage, FirstTime: true, AddedTime: time.Now()})
			if err != nil {
				ctx.String(404, err.Error())
				return
			}
		}

		// Create initial BTItem entry
		database.GetStorm().UpdateBTItem(t.InfoHash(), 0, "", []string{}, t.Name(), 0, 0, 0)

		torrentsLog.Infof("Downloading %s", uri)
		if allFiles == "1" {
			// Selecting all files
			torrentsLog.Infof("Selecting all files for download")
			t.DownloadAllFiles()
			t.SaveDBFiles()
		} else {
			file, _, err := t.ChooseFile(nil)
			if err == nil && file != nil {
				t.DownloadFile(file)
				t.SaveDBFiles()
			} else {
				torrentsLog.Errorf("File was not selected")
			}
		}

		xbmcHost.Refresh()
		ctx.String(200, "")
	}
}

// ResumeTorrent ...
func ResumeTorrent(s *bittorrent.Service) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		defer perf.ScopeTimer()()

		xbmcHost, _ := xbmc.GetXBMCHostWithContext(ctx)

		torrentID := ctx.Params.ByName("torrentId")
		torrent, err := GetTorrentFromParam(s, torrentID)
		if err != nil {
			ctx.Error(fmt.Errorf("Unable to resume torrent with index %s", torrentID))
			return
		}

		torrent.Resume()

		xbmcHost.Refresh()
		ctx.String(200, "")
	}
}

// MoveTorrent ...
func MoveTorrent(s *bittorrent.Service) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		defer perf.ScopeTimer()()

		xbmcHost, _ := xbmc.GetXBMCHostWithContext(ctx)

		torrentID := ctx.Params.ByName("torrentId")
		torrent, err := GetTorrentFromParam(s, torrentID)
		if err != nil {
			ctx.Error(fmt.Errorf("Unable to move torrent with index %s", torrentID))
			return
		}

		torrentsLog.Infof("Marking %s to be moved...", torrent.Name())
		torrent.IsMarkedToMove = true

		xbmcHost.Refresh()
		ctx.String(200, "")
	}
}

// PauseTorrent ...
func PauseTorrent(s *bittorrent.Service) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		defer perf.ScopeTimer()()

		xbmcHost, _ := xbmc.GetXBMCHostWithContext(ctx)

		torrentID := ctx.Params.ByName("torrentId")
		torrent, err := GetTorrentFromParam(s, torrentID)
		if err != nil {
			ctx.Error(fmt.Errorf("Unable to pause torrent with index %s", torrentID))
			return
		}

		torrent.Pause()

		xbmcHost.Refresh()
		ctx.String(200, "")
	}
}

// RemoveTorrent ...
func RemoveTorrent(s *bittorrent.Service) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		xbmcHost, _ := xbmc.GetXBMCHostWithContext(ctx)

		deleteFiles := ctx.DefaultQuery("files", "false")

		torrentID := ctx.Params.ByName("torrentId")
		torrent, err := GetTorrentFromParam(s, torrentID)
		if err != nil {
			ctx.Error(fmt.Errorf("Unable to remove torrent with index %s", torrentID))
			return
		}

		s.RemoveTorrent(xbmcHost, torrent, bittorrent.RemoveOptions{ForceDrop: true, ForceDelete: deleteFiles == "true"})

		xbmcHost.Refresh()
		ctx.String(200, "")
	}
}

// DownloadAllTorrent ...
func DownloadAllTorrent(s *bittorrent.Service) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		defer perf.ScopeTimer()()

		xbmcHost, _ := xbmc.GetXBMCHostWithContext(ctx)

		torrentID := ctx.Params.ByName("torrentId")
		torrent, err := GetTorrentFromParam(s, torrentID)
		if err != nil {
			ctx.Error(fmt.Errorf("Unable to download all files for torrent with index %s", torrentID))
			return
		}

		torrent.DownloadAllFiles()
		torrent.SaveDBFiles()

		xbmcHost.Refresh()
		ctx.String(200, "")
	}
}

// UnDownloadAllTorrent ...
func UnDownloadAllTorrent(s *bittorrent.Service) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		defer perf.ScopeTimer()()

		xbmcHost, _ := xbmc.GetXBMCHostWithContext(ctx)

		torrentID := ctx.Params.ByName("torrentId")
		torrent, err := GetTorrentFromParam(s, torrentID)
		if err != nil {
			ctx.Error(fmt.Errorf("Unable to undownload all files for torrent with index %s", torrentID))
			return
		}

		torrent.UnDownloadAllFiles()
		torrent.SaveDBFiles()

		xbmcHost.Refresh()
		ctx.String(200, "")
	}
}

// SelectFileTorrent ...
func SelectFileTorrent(s *bittorrent.Service, isPlay bool) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		defer perf.ScopeTimer()()

		xbmcHost, _ := xbmc.GetXBMCHostWithContext(ctx)

		torrentID := ctx.Params.ByName("torrentId")
		torrent, err := GetTorrentFromParam(s, torrentID)
		if err != nil {
			ctx.Error(fmt.Errorf("Unable to select files for torrent with index %s", torrentID))
			return
		}

		file, choice, err := torrent.ChooseFile(nil)
		if err == nil && file != nil {
			if isPlay {
				url := torrent.GetPlayURL(strconv.Itoa(choice))
				log.Infof("Triggering play for: %s", url)
				xbmcHost.PlayURL(url)
			} else {
				log.Infof("Triggering download for: %s", file.Path)
				torrent.DownloadFile(file)
				torrent.SaveDBFiles()
				xbmcHost.Refresh()
			}
			return
		}

		xbmcHost.Refresh()
		ctx.String(200, "")
	}
}

// Versions ...
func Versions(s *bittorrent.Service) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		type Versions struct {
			Version   string `json:"version"`
			UserAgent string `json:"user-agent"`
		}
		versions := Versions{
			Version:   ident.GetVersion(),
			UserAgent: s.UserAgent,
		}
		ctx.JSON(200, versions)
	}
}

// GetTorrentFromParam ...
func GetTorrentFromParam(s *bittorrent.Service, param string) (*bittorrent.Torrent, error) {
	if len(param) == 0 {
		return nil, errors.New("Empty param")
	}

	defer perf.ScopeTimer()()

	t := s.GetTorrentByHash(param)
	if t == nil {
		return nil, errors.New("Torrent not found")
	}
	return t, nil
}

func saveTorrentFile(file multipart.File, header *multipart.FileHeader) (string, error) {
	if file == nil || header == nil {
		return "", fmt.Errorf("Not a valid file entry")
	}

	defer perf.ScopeTimer()()

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

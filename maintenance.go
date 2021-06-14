package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"syscall"
	"time"

	"github.com/elgatito/elementum/bittorrent"
	"github.com/elgatito/elementum/cache"
	"github.com/elgatito/elementum/config"
	"github.com/elgatito/elementum/library"
	"github.com/elgatito/elementum/xbmc"
)

const (
	movieType   = "movie"
	showType    = "tvshow"
	seasonType  = "season"
	episodeType = "episode"
)

var seekCatched = false

// Notification serves callbacks from Kodi
func Notification(r *http.Request, s *bittorrent.Service) {
	sender := r.URL.Query().Get("sender")
	method := r.URL.Query().Get("method")
	data := r.URL.Query().Get("data")

	jsonData, jsonErr := base64.StdEncoding.DecodeString(data)
	if jsonErr != nil {
		// Base64 is not URL safe and, probably, Kodi is not well encoding it,
		// so we just take it from URL and decode.
		// Hoping "data=" is always in the end of url.
		if strings.Contains(r.URL.RawQuery, "&data=") {
			data = r.URL.RawQuery[strings.Index(r.URL.RawQuery, "&data=")+6:]
		}
		jsonData, _ = base64.StdEncoding.DecodeString(data)
	}
	log.Debugf("Got notification from %s/%s: %s", sender, method, string(jsonData))

	if sender != "xbmc" {
		return
	}

	switch method {
	case "System.OnQuit":
		log.Infof("Sending SIGHUP signal to shutdown Elementum properly")
		p, err := os.FindProcess(os.Getpid())

		if err != nil {
			return
		}

		p.Signal(syscall.SIGHUP)

	case "Playlist.OnAdd":
		p := s.GetActivePlayer()
		if p == nil || p.Params().VideoDuration == 0 {
			return
		}
		var request struct {
			Item struct {
				ID   int    `json:"id"`
				Type string `json:"type"`
			} `json:"item"`
			Position int `json:"position"`
		}
		request.Position = -1

		if err := json.Unmarshal(jsonData, &request); err != nil {
			log.Error(err)
			return
		}
		p.Params().KodiPosition = request.Position

	case "Player.OnSeek":
		seekCatched = true

		p := s.GetActivePlayer()
		if p == nil || p.Params().VideoDuration == 0 {
			return
		}
		p.Params().Seeked = true
		// Run prioritization over Player's torrent
		go func() {
			// TODO: Do we need to clear deadlines? It can be just few pieces in the waitlist.
			// p.GetTorrent().ClearDeadlines()
			p.GetTorrent().PrioritizePieces()
		}()

	case "Player.OnPause":
		p := s.GetActivePlayer()
		if p == nil || p.Params().VideoDuration == 0 {
			return
		}

		if !p.Params().Paused {
			p.Params().Paused = true
		}

	case "Player.OnPlay":
		seekCatched = false

		// We should stop torrents, waiting for "next" playback
		go s.StopNextFiles()

		var p *bittorrent.Player

		// Try N times to get active player, maybe it takes more time to find active player
		for i := 0; i <= 15; i++ {
			p = s.GetActivePlayer()
			if p != nil && p.Params().VideoDuration > 0 {
				break
			}

			time.Sleep(300 * time.Millisecond) // Let player get its WatchedTime and VideoDuration
		}

		if p == nil || p.IsClosed() {
			log.Warningf("OnPlay. No active player found")
			return
		}

		go p.InitSubtitles()
		// TODO: enable when find a way to provide external audio tracks
		// go p.InitAudio()

		if p.Params().WasSeeked {
			log.Warningf("OnPlay. Player has been seeked already")
			return
		}

		if p.Params().Paused { // Prevent seeking when simply unpausing
			p.Params().Paused = false
			log.Warningf("OnPlay. Skipping seek for paused player")
			return
		}

		log.Infof("OnPlay. Resume check. Resume: %#v, StoredResume: %#v", p.Params().Resume, p.Params().StoredResume)

		p.Params().WasSeeked = true
		resumePosition := float64(-1)

		if p.Params().ResumePlayback == bittorrent.ResumeNo {
			// If we don't need to Seek, then make sure Kodi will start from the beginning and not resume on it's own
			resumePosition = 0
		} else if p.Params().StoredResume != nil && p.Params().StoredResume.Position > 0 {
			resumePosition = p.Params().StoredResume.Position
		} else if p.Params().Resume != nil && p.Params().Resume.Position > 0 {
			resumePosition = p.Params().Resume.Position
		}

		if config.Get().PlayResumeBack > 0 {
			resumePosition -= float64(config.Get().PlayResumeBack)
			if resumePosition < 0 {
				resumePosition = 0
			}
		}

		if resumePosition > -1 {
			go func(resume float64) {
				log.Infof("OnPlay. Seeking to %v", resume)

				// Waiting max 9 seconds for having video duration, only then seek is accepted by Kodi.
				for i := 1; i <= 30; i++ {
					if p.IsClosed() {
						log.Infof("OnPlay. Seek aborted due to Player close")
						return
					}

					time.Sleep(time.Duration(i*300) * time.Millisecond)
					if seekCatched {
						log.Infof("OnPlay. Seek completed")
						return
					} else if p.Params().VideoDuration <= 0 {
						log.Infof("OnPlay. Waiting for available duration")
						continue
					}

					log.Infof("OnPlay. Triggering Seek to %v", resume)
					xbmc.PlayerSeek(resume)
				}
			}(resumePosition)
		}

	case "Player.OnStop":
		p := s.GetActivePlayer()
		if p == nil || p.Params().VideoDuration <= 1 {
			return
		}

		var stopped struct {
			Ended bool `json:"end"`
			Item  struct {
				ID   int    `json:"id"`
				Type string `json:"type"`
			} `json:"item"`
		}
		if err := json.Unmarshal(jsonData, &stopped); err != nil {
			log.Error(err)
			return
		}

		progress := p.Params().WatchedProgress

		log.Infof("Stopped at %f%%", progress)

	case "Playlist.OnClear":
		// TODO: Do we need this endpoint?

	case "VideoLibrary.OnUpdate":
		var request struct {
			Item struct {
				ID   int    `json:"id"`
				Type string `json:"type"`
			} `json:"item"`
			Added     bool `json:"added"`
			Playcount int  `json:"playcount"`
		}
		request.Playcount = -1
		if err := json.Unmarshal(jsonData, &request); err != nil {
			log.Error(err)
			return
		}

		go func() {
			if request.Added {
				library.MarkKodiUpdated()
				library.MarkKodiAdded()
			}
			if request.Playcount != -1 {
				library.MarkKodiUpdated()
			}

			if request.Item.Type == movieType {
				library.RefreshMovie(request.Item.ID, library.ActionUpdate)
			} else if request.Item.Type == showType {
				library.RefreshShow(request.Item.ID, library.ActionUpdate)
			} else if request.Item.Type == episodeType {
				library.RefreshEpisode(request.Item.ID, library.ActionUpdate)
			}
		}()

	case "VideoLibrary.OnRemove":
		var item struct {
			ID   int    `json:"id"`
			Type string `json:"type"`
		}
		if err := json.Unmarshal(jsonData, &item); err != nil {
			log.Error(err)
			return
		}

		go func() {
			// TODO: to implement auto-remove we will need to change ActionSafeDelete to ActionDelete
			if item.Type == movieType {
				library.RefreshMovie(item.ID, library.ActionSafeDelete)

				// Remove local playcount history to allow re-setting watched status
				cacheStore := cache.NewDBStore()
				cacheStore.Delete(fmt.Sprintf(cache.LibraryWatchedPlaycountKey, "movies"))
			} else if item.Type == showType {
				library.RefreshShow(item.ID, library.ActionSafeDelete)

				// Remove local playcount history to allow re-setting watched status
				cacheStore := cache.NewDBStore()
				cacheStore.Delete(fmt.Sprintf(cache.LibraryWatchedPlaycountKey, "shows"))
			} else if item.Type == episodeType {
				library.RefreshEpisode(item.ID, library.ActionSafeDelete)

				// Remove local playcount history to allow re-setting watched status
				cacheStore := cache.NewDBStore()
				cacheStore.Delete(fmt.Sprintf(cache.LibraryWatchedPlaycountKey, "shows"))
			}
		}()

	case "VideoLibrary.OnScanStarted":
		go library.MarkKodiRefresh()

	case "VideoLibrary.OnScanFinished":
		go library.RefreshOnScan()

	case "VideoLibrary.OnCleanFinished":
		go library.PlanOverallUpdate()
	}
}

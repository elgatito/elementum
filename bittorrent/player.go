package bittorrent

import (
	"bufio"
	"errors"
	"fmt"
	"io/ioutil"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	lt "github.com/ElementumOrg/libtorrent-go"
	"github.com/anacrolix/missinggo/perf"
	"github.com/cespare/xxhash"
	"github.com/dustin/go-humanize"
	"github.com/sanity-io/litter"

	"github.com/elgatito/elementum/broadcast"
	"github.com/elgatito/elementum/config"
	"github.com/elgatito/elementum/database"
	"github.com/elgatito/elementum/diskusage"
	"github.com/elgatito/elementum/library"
	"github.com/elgatito/elementum/osdb"
	"github.com/elgatito/elementum/playcount"
	"github.com/elgatito/elementum/tmdb"
	"github.com/elgatito/elementum/trakt"
	"github.com/elgatito/elementum/tvdb"
	"github.com/elgatito/elementum/upnext"
	"github.com/elgatito/elementum/util"
	"github.com/elgatito/elementum/xbmc"
)

const (
	episodeMatchRegex       = `(?i)(^|\W|_|\w)(S0*%[1]d[x\W]?E?p?0*%[2]d|0*%[1]d[x\W]0*%[2]d|\W0*%[1]d0*%[2]d)(\W|_|\D)`
	singleEpisodeMatchRegex = `(?i)(^|\W|_)(Ep?0*%[1]d|0*%[1]d)(\W|_)`
)

var (
	errNoCandidates = fmt.Errorf("No candidates left")
)

const (
	// ResumeEmpty ...
	ResumeEmpty = iota
	// ResumeYes ...
	ResumeYes
	// ResumeNo ...
	ResumeNo
)

// Player ...
type Player struct {
	s                        *Service
	t                        *Torrent
	p                        *PlayerParams
	dialogProgress           *xbmc.DialogProgress
	overlayStatus            *xbmc.OverlayStatus
	next                     NextEpisode
	contentType              string
	scrobble                 bool
	overlayStatusEnabled     bool
	chosenFile               *File
	subtitlesFile            *File
	subtitlesLoaded          []string
	fileSize                 int64
	fileName                 string
	extracted                string
	hasChosenFile            bool
	isDownloading            bool
	notEnoughSpace           bool
	bufferEvents             *broadcast.Broadcaster
	bufferPiecesProgress     map[int]float64
	bufferPiecesProgressLock sync.RWMutex

	diskStatus *diskusage.DiskStatus
	closer     util.Event
	closed     bool
}

// PlayerParams ...
type PlayerParams struct {
	Playing           bool
	Paused            bool
	Seeked            bool
	WasPlaying        bool
	WasSeeked         bool
	DoneAudio         bool
	DoneSubtitles     bool
	Background        bool
	KodiPosition      int
	WatchedProgress   float64
	WatchedTime       float64
	VideoDuration     float64
	URI               string
	OriginalIndex     int
	FileIndex         int
	NextOriginalIndex int
	NextFileIndex     int
	ResumeToken       string
	ResumeHash        string
	ResumePlayback    int
	TraktScrobbled    bool
	ContentType       string
	KodiID            int
	TMDBId            int
	ShowID            int
	Season            int
	Episode           int
	AbsoluteNumber    int
	Query             string
	UpNextSent        bool
	UIDs              *library.UniqueIDs
	Resume            *library.Resume
	StoredResume      *library.Resume
}

// NextEpisode ...
type NextEpisode struct {
	f *File

	started    bool
	done       bool
	bufferSize int64
}

// CandidateFile ...
type CandidateFile struct {
	Index       int
	Filename    string
	DisplayName string
	Path        string
	Size        int64
}

// NewPlayer ...
func NewPlayer(bts *Service, params PlayerParams) *Player {
	params.Playing = true

	btp := &Player{
		s: bts,
		p: &params,

		overlayStatusEnabled: config.Get().EnableOverlayStatus == true,
		scrobble:             config.Get().Scrobble == true && params.TMDBId > 0 && config.Get().TraktToken != "",
		hasChosenFile:        false,
		fileSize:             0,
		fileName:             "",
		isDownloading:        false,
		notEnoughSpace:       false,
		bufferEvents:         broadcast.NewBroadcaster(),
		subtitlesLoaded:      []string{},
	}
	return btp
}

// GetTorrent ...
func (btp *Player) GetTorrent() *Torrent {
	return btp.t
}

// SetTorrent ...
func (btp *Player) SetTorrent(t *Torrent) {
	// Increase player counter only if this is a different torrent or it was not yet set.
	if btp.t == nil || btp.t != t {
		t.PlayerAttached++
	}

	btp.t = t

	btp.t.IsBuffering = false
	btp.t.IsBufferingFinished = false
	btp.t.IsNextFile = false
	btp.t.HasNextFile = false

	btp.t.stopNextTimer()
}

func (btp *Player) addTorrent() error {
	if btp.t == nil {
		storage := config.Get().DownloadStorage
		if btp.p.Background {
			storage = StorageFile
		}

		torrent, err := btp.s.AddTorrent(btp.p.URI, false, storage, true)
		if err != nil {
			log.Errorf("Error adding torrent to player: %s", err)
			return err
		}

		btp.SetTorrent(torrent)
	}
	if btp.t == nil || btp.t.th == nil {
		return fmt.Errorf("Unable to add torrent with URI %s", btp.p.URI)
	}

	go btp.consumeAlerts()

	log.Infof("Downloading %s", btp.t.Name())

	return nil
}

func (btp *Player) resumeTorrent() error {
	if btp.t == nil || btp.t.th == nil {
		return fmt.Errorf("Unable to resume torrent with index %s", btp.p.ResumeHash)
	}

	go btp.consumeAlerts()

	log.Infof("Resuming %s", btp.t.Name())

	btp.t.Resume()

	return nil
}

// PlayURL ...
func (btp *Player) PlayURL() string {
	if btp.t.IsRarArchive {
		extractedPath := filepath.Join(filepath.Dir(btp.chosenFile.Path), "extracted", btp.extracted)
		return util.EncodeFileURL(extractedPath)
	}
	return util.EncodeFileURL(btp.chosenFile.Path)
}

// Buffer ...
func (btp *Player) Buffer() error {
	if btp.p.ResumeHash != "" {
		if err := btp.resumeTorrent(); err != nil {
			log.Errorf("Error resuming torrent: %s", err)
			return err
		}
	} else {
		if err := btp.addTorrent(); err != nil {
			log.Errorf("Error adding torrent: %s", err)
			return err
		}
	}

	// If needed select more files for download
	if config.Get().DownloadFileStrategy != DownloadFilePlaying &&
		!btp.t.IsMemoryStorage() {
		go btp.t.SelectDownloadFiles(btp)
	}

	go btp.processMetadata()

	if btp.p.Background {
		log.Info("Skipping buffering for background download")
		btp.t.IsBuffering = false
		btp.t.IsBufferingFinished = true
		btp.t.IsNeedFinishNotification = true
	} else {
		btp.t.IsBuffering = true
	}

	buffered, done := btp.bufferEvents.Listen()
	defer close(done)

	if !btp.t.IsBufferingFinished {
		btp.dialogProgress = xbmc.NewDialogProgress("Elementum", "", "", "")
		defer btp.dialogProgress.Close()
	}

	btp.overlayStatus = xbmc.NewOverlayStatus()

	btp.GetIdent()
	go btp.waitCheckAvailableSpace()
	go btp.playerLoop()

	go btp.s.AttachPlayer(btp)

	if err := <-buffered; err != nil {
		return err.(error)
	} else if !btp.HasChosenFile() {
		return errors.New("File not chosen")
	}

	return nil
}

func (btp *Player) waitCheckAvailableSpace() {
	if btp.t.IsMemoryStorage() {
		return
	}

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if btp.hasChosenFile {
				if !btp.s.checkAvailableSpace(btp.t) {
					btp.bufferEvents.Broadcast(errors.New("Not enough space on download destination"))
					btp.notEnoughSpace = true
				}

				return
			}
		}
	}
}

func (btp *Player) processMetadata() {
	defer perf.ScopeTimer()()

	var err error
	btp.chosenFile, btp.p.FileIndex, err = btp.t.ChooseFile(btp)
	if err != nil {
		btp.bufferEvents.Broadcast(err)
		return
	}

	btp.p.ResumeToken = strconv.FormatUint(xxhash.Sum64String(btp.t.InfoHash()+btp.chosenFile.Path), 10)
	btp.hasChosenFile = true
	btp.fileSize = btp.chosenFile.Size
	btp.fileName = btp.chosenFile.Name
	btp.subtitlesFile = btp.findSubtitlesFile()

	log.Infof("Chosen file: %s", btp.fileName)
	log.Infof("Saving torrent to database")

	btp.FetchStoredResume()

	var resume *library.Resume
	if btp.p.StoredResume != nil && btp.p.StoredResume.Position > 0 {
		resume = btp.p.StoredResume
	} else if btp.p.Resume != nil && btp.p.Resume.Position > 0 {
		resume = btp.p.Resume
	} else {
		resume = nil
	}

	btp.p.ResumePlayback = ResumeNo
	if resume != nil && !btp.p.Background && config.Get().PlayResumeAction != 0 {
		if !(config.Get().SilentStreamStart ||
			btp.p.ResumePlayback == ResumeYes ||
			config.Get().PlayResumeAction == 2 ||
			xbmc.DialogConfirmFocused("Elementum", fmt.Sprintf("LOCALIZE[30535];;%s", btp.p.StoredResume.ToString()))) {
			log.Infof("Resetting stored resume")
			resume.Reset()
			btp.SaveStoredResume()
			btp.p.ResumePlayback = ResumeNo
		} else {
			btp.p.ResumePlayback = ResumeYes
		}
	}

	files := []string{}
	if btp.chosenFile != nil {
		btp.t.DownloadFileWithPriority(btp.chosenFile, 2)
		files = append(files, btp.chosenFile.Path)
	}
	if btp.subtitlesFile != nil {
		btp.t.DownloadFileWithPriority(btp.subtitlesFile, 2)
		files = append(files, btp.subtitlesFile.Path)
	}

	infoHash := btp.t.InfoHash()
	database.GetStorm().UpdateBTItem(infoHash, btp.p.TMDBId, btp.p.ContentType, files, btp.p.Query, btp.p.ShowID, btp.p.Season, btp.p.Episode)
	btp.t.DBItem = database.GetStorm().GetBTItem(infoHash)

	meta := btp.t.UpdateMetadataTitle(btp.t.Title(), btp.t.GetMetadata())
	go database.GetStorm().AddTorrentHistory(btp.t.InfoHash(), btp.t.Title(), meta)
	go database.GetStorm().AddTorrentLink(strconv.Itoa(btp.p.TMDBId), btp.t.InfoHash(), meta, true)

	if btp.t.IsRarArchive {
		// Just disable sequential download for RAR archives
		log.Info("Disabling sequential download")
		btp.t.th.SetSequentialDownload(false)
		return
	}

	// For non-memory storage prioritize current files
	if !btp.t.IsMemoryStorage() {
		filePriorities := btp.t.th.FilePriorities()
		defer lt.DeleteStdVectorInt(filePriorities)

		if btp.chosenFile != nil {
			filePriorities.Set(btp.chosenFile.Index, 4)
		}
		if btp.subtitlesFile != nil {
			filePriorities.Set(btp.subtitlesFile.Index, 4)
		}

		btp.t.th.PrioritizeFiles(filePriorities)
		btp.t.SaveDBFiles()
	}

	log.Info("Setting piece priorities")

	if !btp.p.Background {
		go btp.t.Buffer(btp.chosenFile, btp.p.ResumeHash == "")
	}
}

func (btp *Player) statusStrings(progress float64, status lt.TorrentStatus) (string, string, string) {
	defer perf.ScopeTimer()()

	if status == nil || status.Swigcptr() == 0 || btp.t.Closer.IsSet() {
		return "", "", ""
	}
	if progress < 0 {
		progress = 0
	}

	statusName := btp.t.GetStateString()
	line1 := fmt.Sprintf("%s (%.2f%%)", statusName, progress)

	// Adding buffer size to progress window
	if btp.t.IsBuffering {
		query := btp.t.BufferLength
		done := int64(float64(progress/100) * float64(query))

		line1 = fmt.Sprintf("%s (%.2f%%) | (%s / %s)", statusName, progress, humanize.Bytes(uint64(done)), humanize.Bytes(uint64(query)))
	} else if btp.t.IsMemoryStorage() {
		// For memory storage show also memory size near percents.
		line1 = fmt.Sprintf("%s (%.2f%% / %s)", statusName, progress, humanize.Bytes(uint64(btp.t.MemorySize)))
	}

	// File size
	var totalSize int64
	if btp.t.ti != nil && btp.t.ti.Swigcptr() != 0 {
		if btp.fileSize > 0 && !btp.t.IsRarArchive {
			totalSize = btp.fileSize
		} else {
			totalSize = btp.t.ti.TotalSize()
		}
		line1 += " - " + humanize.Bytes(uint64(totalSize))
	}

	// Bitrate
	if btp.t.IsPlaying && btp.p.VideoDuration > 0 {
		bps := uint64(totalSize) / uint64(btp.p.VideoDuration)
		line1 += fmt.Sprintf(" - LOCALIZE[30640] ~ %s (%.2f MBit)", humanize.Bytes(bps), float64(bps*8)/1000000)
	}

	seeds, seedsTotal, peers, peersTotal := btp.t.GetConnections()
	line2 := fmt.Sprintf("D:%.0fkB/s U:%.0fkB/s S:%d/%d P:%d/%d",
		float64(status.GetDownloadPayloadRate())/1024,
		float64(status.GetUploadPayloadRate())/1024,
		seeds, seedsTotal, peers, peersTotal,
	)
	line3 := btp.t.Name()
	if btp.fileName != "" && !btp.t.IsRarArchive {
		line3 = btp.fileName
	}
	return line1, line2, line3
}

// HasChosenFile ...
func (btp *Player) HasChosenFile() bool {
	return btp.hasChosenFile && btp.chosenFile != nil
}

func (btp *Player) findSubtitlesFile() *File {
	extension := filepath.Ext(btp.fileName)
	chosenName := btp.fileName[0 : len(btp.fileName)-len(extension)]
	srtFileName := chosenName + ".srt"

	files := btp.t.files

	var lastMatched *File
	countMatched := 0

	for _, file := range files {
		fileName := file.Path
		if strings.HasSuffix(fileName, srtFileName) {
			return file
		} else if util.HasSubtitlesExt(fileName) {
			lastMatched = file
			countMatched++
		}
	}

	if countMatched == 1 {
		return lastMatched
	}

	return nil
}

// IsClosed returns whether player is in closing stage
func (btp *Player) IsClosed() bool {
	return btp.closed
}

// Close ...
func (btp *Player) Close() {
	// Prevent double-closing
	if btp.closed {
		return
	}

	btp.closed = true
	btp.closer.Set()

	// Torrent was not initialized so just close and return
	if btp.t == nil {
		return
	}

	// Cleanup autoloaded subtitles
	if btp.subtitlesLoaded != nil && len(btp.subtitlesLoaded) > 0 && config.Get().OSDBAutoLoadDelete {
		for _, f := range btp.subtitlesLoaded {
			if _, err := os.Stat(f); err == nil {
				log.Infof("Deleting saved subtitles file at %s", f)
				defer os.Remove(f)
			}
		}
	}

	defer func() {
		go btp.s.DetachPlayer(btp)
		go btp.s.PlayerStop()
	}()

	if btp.t.HasNextFile && btp.IsWatched() {
		log.Infof("Leaving torrent '%s' awaiting for next file playback", btp.t.Name())
		btp.t.startNextTimer()
		return
	}

	// Remove torrent only if this torrent is not needed for background download or other players are using it.
	if !btp.p.Background && btp.t.PlayerAttached <= 1 {
		// If there is no chosen file - we stop the torrent and remove everything
		btp.s.RemoveTorrent(btp.t, false, btp.notEnoughSpace, btp.IsWatched())
	}
}

func (btp *Player) bufferDialog() {
	if finished, err := btp.updateBufferDialog(); finished {
		return
	} else if err != nil {
		log.Warningf("Got error from buffer dialog update: %s", err)
	}

	halfSecond := time.NewTicker(500 * time.Millisecond)
	defer halfSecond.Stop()
	oneSecond := time.NewTicker(1 * time.Second)
	defer oneSecond.Stop()

	for {
		select {
		case <-halfSecond.C:
			if btp.dialogProgress == nil {
				halfSecond.Stop()
				break
			}

			if btp.closer.IsSet() || btp.dialogProgress.IsCanceled() || btp.notEnoughSpace {
				errMsg := "User cancelled the buffering"
				log.Info(errMsg)
				btp.bufferEvents.Broadcast(errors.New(errMsg))
				btp.t.ResetBuffering()
				return
			}
		case <-oneSecond.C:
			if finished, err := btp.updateBufferDialog(); finished {
				return
			} else if err != nil {
				log.Warningf("Got error from buffer dialog update: %s", err)
			}
		}
	}
}

func (btp *Player) updateBufferDialog() (bool, error) {
	if btp.closer.IsSet() {
		log.Debugf("Player is set to closing, disabling Buffer update")
		return false, nil
	}

	if (btp.dialogProgress == nil || btp.dialogProgress.IsCanceled()) && !btp.t.IsBufferingFinished {
		log.Debugf("Dialog not yet available")
		return false, nil
	}

	defer perf.ScopeTimer()()

	// Handle "Checking" state for resumed downloads
	if btp.t.GetLastStatus(false).GetState() == StatusChecking || btp.t.IsRarArchive {
		progress := btp.t.GetBufferProgress()
		line1, line2, line3 := btp.statusStrings(progress, btp.t.GetLastStatus(false))
		if btp.dialogProgress != nil {
			btp.dialogProgress.Update(int(progress), line1, line2, line3)
		}

		if btp.t.IsRarArchive && progress >= 100 {
			archivePath := filepath.Join(btp.s.config.DownloadPath, btp.chosenFile.Path)
			destPath := filepath.Join(btp.s.config.DownloadPath, filepath.Dir(btp.chosenFile.Path), "extracted")

			if _, err := os.Stat(destPath); err == nil {
				btp.findExtracted(destPath)
				btp.setRateLimiting(true)
				btp.bufferEvents.Signal()
				return false, fmt.Errorf("File already exists")
			}
			os.MkdirAll(destPath, 0755)

			cmdName := "unrar"
			cmdArgs := []string{"e", archivePath, destPath}
			if platform := xbmc.GetPlatform(); platform.OS == "windows" {
				cmdName = "unrar.exe"
			}
			cmd := exec.Command(cmdName, cmdArgs...)

			cmdReader, err := cmd.StdoutPipe()
			if err != nil {
				log.Error(err)
				btp.bufferEvents.Broadcast(err)
				xbmc.Notify("Elementum", "LOCALIZE[30304]", config.AddonIcon())
				return false, err
			}

			scanner := bufio.NewScanner(cmdReader)
			go func() {
				for scanner.Scan() {
					log.Infof("unrar | %s", scanner.Text())
				}
			}()

			err = cmd.Start()
			if err != nil {
				log.Error(err)
				btp.bufferEvents.Broadcast(err)
				xbmc.Notify("Elementum", "LOCALIZE[30305]", config.AddonIcon())
				return false, err
			}

			err = cmd.Wait()
			if err != nil {
				log.Error(err)
				btp.bufferEvents.Broadcast(err)
				xbmc.Notify("Elementum", "LOCALIZE[30306]", config.AddonIcon())
				return false, err
			}

			btp.findExtracted(destPath)
			btp.setRateLimiting(true)
			btp.bufferEvents.Signal()
			return true, nil
		}
	} else {
		line1, line2, line3 := btp.statusStrings(btp.t.BufferProgress, btp.t.GetLastStatus(false))
		if btp.dialogProgress != nil {
			btp.dialogProgress.Update(int(btp.t.BufferProgress), line1, line2, line3)
		}
		if !btp.t.IsBuffering && btp.t.HasMetadata() && btp.t.GetState() != StatusChecking {
			btp.bufferEvents.Signal()
			btp.setRateLimiting(true)
			return true, nil
		}
	}

	return false, nil
}

func (btp *Player) findExtracted(destPath string) {
	files, err := ioutil.ReadDir(destPath)
	if err != nil {
		log.Error(err)
		btp.bufferEvents.Broadcast(err)
		xbmc.Notify("Elementum", "LOCALIZE[30307]", config.AddonIcon())
		return
	}
	if len(files) == 1 {
		log.Info("Extracted", files[0].Name())
		btp.extracted = files[0].Name()
	} else {
		for _, file := range files {
			fileName := file.Name()
			re := regexp.MustCompile(`(?i).*\.(mkv|mp4|mov|avi)`)
			if re.MatchString(fileName) {
				log.Info("Extracted", fileName)
				btp.extracted = fileName
				break
			}
		}
	}
}

func (btp *Player) updateWatchTimes() {
	ret := xbmc.GetWatchTimes()
	if ret["error"] != "" {
		return
	}
	btp.p.WatchedTime, _ = strconv.ParseFloat(ret["watchedTime"], 64)
	btp.p.VideoDuration, _ = strconv.ParseFloat(ret["videoDuration"], 64)
	if btp.p.VideoDuration > 0 {
		btp.p.WatchedProgress = btp.p.WatchedTime / btp.p.VideoDuration * 100
	}
}

func (btp *Player) playerLoop() {
	defer btp.Close()

	log.Info("Buffer loop")

	buffered, bufferDone := btp.bufferEvents.Listen()
	defer close(bufferDone)

	go btp.bufferDialog()

	if err := <-buffered; err != nil {
		log.Errorf("Error buffering: %#v", err)
		return
	}

	log.Info("Waiting for playback...")
	oneSecond := time.NewTicker(1 * time.Second)
	defer oneSecond.Stop()
	playbackTimeout := time.After(time.Duration(config.Get().BufferTimeout) * time.Second)

playbackWaitLoop:
	for {
		if btp.p.Background || xbmc.PlayerIsPlaying() {
			break playbackWaitLoop
		}
		select {
		case <-playbackTimeout:
			log.Warningf("Playback was unable to start after %d seconds. Aborting...", config.Get().BufferTimeout)
			btp.bufferEvents.Broadcast(errors.New("Playback was unable to start before timeout"))
			return
		case <-oneSecond.C:
		}
	}

	log.Info("Playback loop")
	overlayStatusActive := false
	playing := true

	btp.updateWatchTimes()
	btp.findNextFile()

	log.Infof("Got playback: %fs / %fs", btp.p.WatchedTime, btp.p.VideoDuration)
	if btp.scrobble {
		trakt.Scrobble("start", btp.p.ContentType, btp.p.TMDBId, btp.p.WatchedTime, btp.p.VideoDuration)
		btp.p.TraktScrobbled = true
	}

	btp.t.IsPlaying = true

playbackLoop:
	for {
		if btp.p.Background || xbmc.PlayerIsPlaying() == false {
			btp.t.IsPlaying = false
			break playbackLoop
		}
		select {
		case <-oneSecond.C:
			btp.updateWatchTimes()

			// Trigger UpNext notification if Player is done with initialization
			if btp.p.VideoDuration > 0 && !btp.p.UpNextSent {
				go btp.processUpNextPayload()
			}

			if btp.p.Seeked {
				btp.p.Seeked = false
				if btp.scrobble {
					go trakt.Scrobble("start", btp.p.ContentType, btp.p.TMDBId, btp.p.WatchedTime, btp.p.VideoDuration)
				}
			} else if xbmc.PlayerIsPaused() {
				if btp.overlayStatusEnabled && btp.p.Playing {
					progress := btp.t.GetProgress()
					line1, line2, line3 := btp.statusStrings(progress, btp.t.GetLastStatus(false))
					btp.overlayStatus.Update(int(progress), line1, line2, line3)
					if overlayStatusActive == false {
						btp.overlayStatus.Show()
						overlayStatusActive = true
					}
				}

				if playing == true {
					playing = false
					if btp.scrobble {
						go trakt.Scrobble("pause", btp.p.ContentType, btp.p.TMDBId, btp.p.WatchedTime, btp.p.VideoDuration)
					}
				}
			} else {
				if overlayStatusActive == true {
					btp.overlayStatus.Hide()
					overlayStatusActive = false
				}
				if playing == false {
					playing = true
					if btp.scrobble {
						go trakt.Scrobble("start", btp.p.ContentType, btp.p.TMDBId, btp.p.WatchedTime, btp.p.VideoDuration)
					}
				}
			}

			if btp.next.f != nil && !btp.next.started && btp.isReadyForNextFile() {
				btp.startNextFile()
			}
		}
	}

	log.Info("Stopped playback")
	btp.SaveStoredResume()
	btp.setRateLimiting(false)
	go func() {
		btp.GetIdent()
		btp.UpdateWatched()
		if btp.scrobble {
			if btp.IsWatched() {
				trakt.Scrobble("stop", btp.p.ContentType, btp.p.TMDBId, btp.p.WatchedTime, btp.p.VideoDuration)
			} else {
				trakt.Scrobble("pause", btp.p.ContentType, btp.p.TMDBId, btp.p.WatchedTime, btp.p.VideoDuration)
			}
		}

		btp.p.Playing = false
		btp.p.Paused = false
		btp.p.Seeked = false
		btp.p.WasPlaying = true
		btp.p.WatchedTime = 0
		btp.p.VideoDuration = 0
		btp.p.WatchedProgress = 0
	}()

	if btp.overlayStatus != nil {
		btp.overlayStatus.Close()
	}
}

func (btp *Player) isReadyForNextFile() bool {
	if btp.t.IsMemoryStorage() {
		ra := btp.t.GetReadaheadSize()
		sum := btp.t.ReadersReadaheadSum()

		btp.t.muAwaitingPieces.RLock()
		defer btp.t.muAwaitingPieces.RUnlock()

		return ra > 0 && sum > 0 && ra > sum+btp.next.bufferSize && btp.t.awaitingPieces.IsEmpty() && btp.t.lastProgress > 90
	}

	return btp.IsWatched()
}

// Params returns Params for external use
func (btp *Player) Params() *PlayerParams {
	return btp.p
}

// UpdateWatched is updating watched progress is Kodi
func (btp *Player) UpdateWatched() {
	log.Debugf("Updating Watched state: %s", litter.Sdump(btp.p))

	if btp.p.WatchedProgress == 0 {
		return
	}

	log.Infof("Currently at %f%%, KodiID: %d", btp.p.WatchedProgress, btp.p.KodiID)

	// Update Watched state for current file
	SetWatchedFile(btp.chosenFile.Path, btp.chosenFile.Size, btp.IsWatched())

	if btp.IsWatched() {
		log.Info("IsWatched")
		var watched *trakt.WatchedItem

		// TODO: Make use of Playcount, possibly increment when Watched, use old value if in progress
		if btp.p.ContentType == movieType {
			watched = &trakt.WatchedItem{
				MediaType: btp.p.ContentType,
				Movie:     btp.p.TMDBId,
				Watched:   true,
			}
			if btp.p.KodiID != 0 {
				xbmc.SetMovieWatched(btp.p.KodiID, 1, 0, 0)
			}
		} else if btp.p.ContentType == episodeType {
			watched = &trakt.WatchedItem{
				MediaType: btp.p.ContentType,
				Show:      btp.p.ShowID,
				Season:    btp.p.Season,
				Episode:   btp.p.Episode,
				Watched:   true,
			}
			if btp.p.KodiID != 0 {
				xbmc.SetEpisodeWatched(btp.p.KodiID, 1, 0, 0)
			}
		}

		if config.Get().TraktToken != "" && watched != nil && !btp.p.TraktScrobbled {
			log.Debugf("Setting Trakt watched for: %#v", watched)
			go trakt.SetWatched(watched)
		}
	} else if btp.p.WatchedTime > 180 {
		if btp.p.Resume != nil {
			log.Debugf("Updating player resume from: %#v", btp.p.Resume)
			btp.p.Resume.Position = btp.p.WatchedTime
			btp.p.Resume.Total = btp.p.VideoDuration
		}

		if btp.p.ContentType == movieType {
			xbmc.SetMovieProgress(btp.p.KodiID, int(btp.p.WatchedTime), int(btp.p.VideoDuration))
		} else if btp.p.ContentType == episodeType {
			xbmc.SetEpisodeProgress(btp.p.KodiID, int(btp.p.WatchedTime), int(btp.p.VideoDuration))
		}
	}
	time.Sleep(200 * time.Millisecond)
	xbmc.Refresh()
}

// IsWatched ...
func (btp *Player) IsWatched() bool {
	return btp.p.WatchedProgress > float64(config.Get().PlaybackPercent)
}

func (btp *Player) smartMatch(choices []*CandidateFile) {
	if !config.Get().SmartEpisodeMatch {
		return
	}

	b := btp.t.GetMetadata()
	show := tmdb.GetShow(btp.p.ShowID, config.Get().Language)
	if show == nil {
		return
	}

	var tvdbShow *tvdb.Show
	// If show is Anime, we will need Tvdb Show entry for getting absolute numbers for episodes
	if show.IsAnime() {
		tvdbID := util.StrInterfaceToInt(show.ExternalIDs.TVDBID)
		tvdbShow, _ = tvdb.GetShow(tvdbID, config.Get().Language)
	}

	hash := btp.t.InfoHash()

	for _, season := range show.Seasons {
		if season == nil || season.EpisodeCount == 0 {
			continue
		}
		tmdbSeason := tmdb.GetSeason(btp.p.ShowID, season.Season, config.Get().Language, len(show.Seasons))
		if tmdbSeason == nil {
			continue
		}

		episodes := tmdbSeason.Episodes

		for _, episode := range episodes {
			if episode == nil {
				continue
			}

			index, found := MatchEpisodeFilename(season.Season, episode.EpisodeNumber, show.CountRealSeasons() == 1, btp.p.Season, show, episode, tvdbShow, choices)
			if index >= 0 && found == 1 {
				database.GetStorm().AddTorrentLink(strconv.Itoa(episode.ID), hash, b, false)
			}
		}
	}
}

// GetIdent tries to find playing item in Kodi library
func (btp *Player) GetIdent() {
	if btp.p.TMDBId == 0 || btp.p.KodiID != 0 || btp.p.ContentType == "search" {
		return
	}

	defer perf.ScopeTimer()()

	if btp.p.ContentType == movieType {
		movie, _ := library.GetMovieByTMDB(btp.p.TMDBId)
		if movie != nil {
			btp.p.KodiID = movie.UIDs.Kodi
			btp.p.Resume = movie.Resume
			btp.p.UIDs = movie.UIDs
		}
	} else if btp.p.ContentType == episodeType && btp.p.Episode > 0 {
		show, _ := library.GetShowByTMDB(btp.p.ShowID)
		if show != nil {
			episode := show.GetEpisode(btp.p.Season, btp.p.Episode)
			if episode != nil {
				btp.p.KodiID = episode.UIDs.Kodi
				btp.p.Resume = episode.Resume
				btp.p.UIDs = episode.UIDs
			}
		}
	}

	if btp.p.KodiID == 0 {
		log.Debugf("Can't find %s for these parameters: %+v", btp.p.ContentType, btp.p)
	}
}

func (btp *Player) setRateLimiting(enable bool) {
	if btp.s.config.LimitAfterBuffering {
		settings := btp.s.PackSettings
		if enable == true {
			if btp.s.config.DownloadRateLimit > 0 {
				log.Infof("Buffer filled, rate limiting download to %s", humanize.Bytes(uint64(btp.s.config.DownloadRateLimit)))
				settings.SetInt("download_rate_limit", btp.s.config.UploadRateLimit)
			}
			if btp.s.config.UploadRateLimit > 0 {
				// If we have an upload rate, use the nicer bittyrant choker
				log.Infof("Buffer filled, rate limiting upload to %s", humanize.Bytes(uint64(btp.s.config.UploadRateLimit)))
				settings.SetInt("upload_rate_limit", btp.s.config.UploadRateLimit)
			}
		} else {
			log.Info("Resetting rate limiting")
			settings.SetInt("download_rate_limit", 0)
			settings.SetInt("upload_rate_limit", 0)
		}
		btp.s.Session.ApplySettings(settings)
	}
}

func (btp *Player) consumeAlerts() {
	log.Debugf("Consuming alerts")
	alerts, alertsDone := btp.s.Alerts()
	pc := btp.closer.C()

	defer close(alertsDone)

	for {
		select {
		case <-pc:
			log.Debugf("Stopping player alerts")
			return

		case alert, ok := <-alerts:
			if !ok { // was the alerts channel closed?
				return
			}

			switch alert.Type {
			case lt.StateChangedAlertAlertType:
				stateAlert := lt.SwigcptrStateChangedAlert(alert.Pointer)
				if btp.t != nil && btp.t.th != nil && btp.t.th.Swigcptr() != 0 && stateAlert.GetHandle().Equal(btp.t.th) {
					btp.onStateChanged(stateAlert)
				}
			}
		}
	}
}

func (btp *Player) onStateChanged(stateAlert lt.StateChangedAlert) {
	switch stateAlert.GetState() {
	case lt.TorrentStatusDownloading:
		btp.isDownloading = true
	}
}

func (btp *Player) startNextFile() {
	if (btp.p.ShowID == 0 && btp.p.Query == "") || !btp.t.HasNextFile || !btp.next.done || btp.next.f == nil || btp.t.IsBuffering || btp.next.started {
		return
	}

	btp.next.started = true
	go btp.t.Buffer(btp.next.f, false)
}

func (btp *Player) findNextFile() {
	if (btp.p.ShowID == 0 && btp.p.Query == "") || btp.next.done || !config.Get().SmartEpisodeStart {
		return
	}

	// Set mark to avoid more than once
	btp.next.done = true

	if btp.p.ShowID != 0 {
		// Searching if we have next episode in the torrent
		if btp.next.f == nil {
			btp.next.f = btp.t.GetNextEpisodeFile(btp.p.Season, btp.p.Episode+1)
		}
		if btp.next.f == nil && btp.p.AbsoluteNumber > 0 {
			btp.next.f = btp.t.GetNextSingleEpisodeFile(btp.p.AbsoluteNumber + 1)
		}
		if btp.next.f == nil {
			btp.next.f = btp.t.GetNextSingleEpisodeFile(btp.p.Episode + 1)
		}
		if btp.next.f == nil {
			// If next episode not matched, try to find first episode of next season
			btp.next.f = btp.t.GetNextEpisodeFile(btp.p.Season+1, 1)
		}
	} else if btp.p.ContentType == movieType {
		return
	} else {
		// Selecting next file from available choices as the next file
		if candidates, _, err := btp.t.GetCandidateFiles(btp); err == nil {
			if btp.p.NextFileIndex > -1 && btp.p.NextFileIndex < len(candidates) {
				btp.next.f = btp.t.files[candidates[btp.p.NextFileIndex].Index]
			} else if btp.p.NextOriginalIndex > -1 && btp.p.NextOriginalIndex < len(btp.t.files) {
				for _, f := range btp.t.files {
					if f.Index == btp.p.NextOriginalIndex {
						btp.next.f = f
						break
					}
				}
			} else if btp.p.FileIndex+1 < len(candidates) {
				btp.next.f = btp.t.files[candidates[btp.p.FileIndex+1].Index]
			} else {
				log.Debugf("No files available for next playback. Current: %d, Candidates: %d", btp.p.FileIndex, len(candidates))
			}
		}
	}

	if btp.next.f == nil {
		btp.t.HasNextFile = false
		return
	}

	btp.t.HasNextFile = true

	startBufferSize := btp.s.GetBufferSize()
	_, _, _, preBufferSize := btp.t.getBufferSize(btp.next.f.Offset, 0, startBufferSize)
	_, _, _, postBufferSize := btp.t.getBufferSize(btp.next.f.Offset, btp.next.f.Size-int64(config.Get().EndBufferSize), int64(config.Get().EndBufferSize))

	btp.next.bufferSize = preBufferSize + postBufferSize

	log.Infof("Next file prepared: %#v", btp.next.f.Path)
}

// InitAudio ...
func (btp *Player) InitAudio() {
	if btp.p.DoneAudio {
		return
	}

	filePath := btp.chosenFile.Path
	extension := filepath.Ext(filePath)

	if !util.IsAudioExt(extension) {
		_, f := filepath.Split(filePath)
		currentPath := f[0 : len(f)-len(extension)]
		collected := []string{}

		for _, f := range btp.t.files {
			if strings.Contains(f.Path, currentPath) && util.HasAudioExt(f.Path) {
				collected = append(collected, util.GetHTTPHost()+"/files/"+f.Path)
			}
		}

		if len(collected) > 0 {
			log.Debugf("Adding player audio tracks: %#v", collected)
			xbmc.PlayerSetSubtitles(collected)
		}
	}

	btp.p.DoneAudio = true
}

// InitSubtitles ...
func (btp *Player) InitSubtitles() {
	if btp.p.DoneSubtitles {
		return
	}

	if config.Get().OSDBIncludedEnabled && (!config.Get().OSDBIncludedSkipExists || len(xbmc.PlayerGetSubtitles()) == 0) {
		btp.SetSubtitles()
	}

	if config.Get().OSDBAutoLoad && (!config.Get().OSDBAutoLoadSkipExists || len(xbmc.PlayerGetSubtitles()) == 0) {
		btp.DownloadSubtitles()
	}

	btp.p.DoneSubtitles = true
}

// DownloadSubtitles ...
func (btp *Player) DownloadSubtitles() {
	payloads, preferredLanguage := osdb.GetPayloads("", []string{"English"}, xbmc.SettingsGetSettingValue("locale.subtitlelanguage"), btp.p.ShowID, xbmc.PlayerGetPlayingFile())
	log.Infof("Subtitles payload auto: %#v; %s", payloads, preferredLanguage)

	results, err := osdb.DoSearch(payloads, preferredLanguage)
	if err != nil || results == nil || len(results) == 0 {
		return
	}

	btp.subtitlesLoaded = []string{}
	for i, sub := range results {
		if i+1 > config.Get().OSDBAutoLoadCount {
			break
		}

		subPath := sub.SubFileName[:len(sub.SubFileName)-3] + sub.IDSubtitleFile + ".srt"
		_, path, err := osdb.DoDownload(subPath, sub.SubDownloadLink)
		if err != nil {
			continue
		}

		btp.subtitlesLoaded = append(btp.subtitlesLoaded, path)
	}

	if len(btp.subtitlesLoaded) > 0 {
		log.Infof("Setting subtitles to Kodi Player: %+v", btp.subtitlesLoaded)

		sort.Sort(sort.Reverse(sort.StringSlice(btp.subtitlesLoaded)))
		xbmc.PlayerSetSubtitles(btp.subtitlesLoaded)
	}
}

// SetSubtitles ...
func (btp *Player) SetSubtitles() {
	filePath := btp.chosenFile.Path
	extension := filepath.Ext(filePath)

	if !util.IsSubtitlesExt(extension) {
		// Let's search for all files that have same beginning and .srt extension
		// It is possible to have items like 'movie.french.srt'
		_, f := filepath.Split(filePath)
		currentPath := f[0 : len(f)-len(extension)]
		collected := []string{}

		for _, f := range btp.t.files {
			if strings.Contains(f.Path, currentPath) && util.HasSubtitlesExt(f.Path) {
				collected = append(collected, util.GetHTTPHost()+"/files/"+f.Path)
			}
		}

		if len(collected) > 0 {
			log.Debugf("Adding player subtitles: %#v", collected)
			xbmc.PlayerSetSubtitles(collected)
		}
	}
}

// FetchStoredResume ...
func (btp *Player) FetchStoredResume() {
	key := "stored.resume." + btp.p.ResumeToken
	if btp.p.StoredResume == nil {
		btp.p.StoredResume = &library.Resume{}
	}

	database.GetCache().GetCachedObject(database.CommonBucket, key, btp.p.StoredResume)
}

// SaveStoredResume ...
func (btp *Player) SaveStoredResume() {
	key := "stored.resume." + btp.p.ResumeToken

	if btp.p.StoredResume == nil {
		btp.p.StoredResume = &library.Resume{}
	}

	btp.p.StoredResume.Total = btp.p.VideoDuration
	btp.p.StoredResume.Position = btp.p.WatchedTime

	if btp.p.StoredResume.Total == 0 || btp.p.StoredResume.Position == 0 {
		return
	} else if btp.IsWatched() || btp.p.StoredResume.Position < 180 {
		database.GetCache().Delete(database.CommonBucket, key)
	} else {
		database.GetCache().SetCachedObject(database.CommonBucket, storedResumeExpiration, key, btp.p.StoredResume)
	}
}

func (btp *Player) processUpNextPayload() {
	btp.p.UpNextSent = true

	var err error
	var payload upnext.Payload

	if btp.p.ShowID != 0 {
		payload, err = btp.processUpNextShow()
	} else if btp.p.ContentType == movieType {
		return
	} else {
		payload, err = btp.processUpNextQuery()
	}

	if payload.PlayURL == "" || err == errNoCandidates {
		return
	} else if err != nil {
		log.Warningf("Could not prepare UpNext payload: %s", err)
	}

	xbmc.UpNextNotify(xbmc.Args{payload})
}

func (btp *Player) processUpNextShow() (upnext.Payload, error) {
	res := upnext.Payload{}

	show, season, episode, err := getShowSeasonEpisode(btp.p.ShowID, btp.p.Season, btp.p.Episode)
	if err != nil {
		log.Warningf("Cannot prepare current UpNext item: %s", err)
		return res, err
	}
	res.CurrentEpisode = btp.processUpNextEpisode(show, season, episode)

	show, season, episode, err = getNextShowSeasonEpisode(btp.p.ShowID, btp.p.Season, btp.p.Episode)
	if err != nil {
		log.Warningf("Cannot prepare next UpNext item: %s", err)
		return res, err
	}
	res.NextEpisode = btp.processUpNextEpisode(show, season, episode)

	res.PlayURL = contextPlayURL(
		URLForXBMC("/show/%d/season/%d/episode/%d/",
			show.ID,
			episode.SeasonNumber,
			episode.EpisodeNumber,
		)+"%s/%s?silent=true",
		fmt.Sprintf("%s S%02dE%02d", show.OriginalName, episode.SeasonNumber, episode.EpisodeNumber),
		false,
	)

	return res, nil
}

func (btp *Player) processUpNextQuery() (upnext.Payload, error) {
	res := upnext.Payload{}

	currentFile, _, errCurrent := btp.processUpNextFile(btp.p.FileIndex)
	if errCurrent != nil {
		log.Warningf("Cannot prepare current UpNext item: %s", errCurrent)
		return res, errCurrent
	}

	nextFile, nextIndex, errNext := btp.processUpNextFile(btp.p.FileIndex + 1)
	if errNext != nil {
		log.Warningf("Cannot prepare next UpNext item: %s", errNext)
		return res, errNext
	}

	showTitle := btp.p.Query
	if showTitle == "" {
		showTitle = btp.t.Name()
	}

	res.CurrentEpisode = upnext.Episode{
		Title:     currentFile,
		ShowTitle: showTitle,
		EpisodeID: removeTrailingMinus(strconv.Itoa(int(xxhash.Sum64String(currentFile)))),
		TVShowID:  removeTrailingMinus(strconv.Itoa(int(xxhash.Sum64String(showTitle)))),
	}
	res.NextEpisode = upnext.Episode{
		Title:     nextFile,
		ShowTitle: showTitle,
		EpisodeID: removeTrailingMinus(strconv.Itoa(int(xxhash.Sum64String(nextFile)))),
		TVShowID:  removeTrailingMinus(strconv.Itoa(int(xxhash.Sum64String(showTitle)))),
	}

	if btp.p.Query != "" {
		res.PlayURL = URLQuery(URLForXBMC("/search"), "q", btp.p.Query, "index", strconv.Itoa(nextIndex), "silent", "true")
	} else {
		res.PlayURL = URLQuery(URLForXBMC("/history"), "infohash", btp.t.InfoHash(), "index", strconv.Itoa(nextIndex), "silent", "true")
	}

	return res, nil
}

func (btp *Player) processUpNextEpisode(show *tmdb.Show, season *tmdb.Season, episode *tmdb.Episode) upnext.Episode {
	li := episode.ToListItem(show, season)
	runtime := 1800
	if len(show.EpisodeRunTime) > 0 {
		runtime = show.EpisodeRunTime[len(show.EpisodeRunTime)-1] * 60
	}

	return upnext.Episode{
		EpisodeID:  strconv.Itoa(episode.ID),
		TVShowID:   strconv.Itoa(show.ID),
		Title:      episode.Name,
		Season:     strconv.Itoa(episode.SeasonNumber),
		Episode:    strconv.Itoa(episode.EpisodeNumber),
		ShowTitle:  show.Name,
		Plot:       episode.Overview,
		Playcount:  playcount.GetWatchedEpisodeByTMDB(show.ID, episode.SeasonNumber, episode.EpisodeNumber).Int(),
		Rating:     int(episode.VoteAverage),
		FirstAired: episode.AirDate,
		Runtime:    runtime,

		Art: upnext.Art{
			Thumb:           li.Art.Thumbnail,
			TVShowClearArt:  li.Art.ClearArt,
			TVShowClearLogo: li.Art.ClearLogo,
			TVShowFanart:    li.Art.FanArt,
			TVShowLandscape: li.Art.Landscape,
			TVShowPoster:    li.Art.TvShowPoster,
		},
	}
}

func (btp *Player) processUpNextFile(index int) (string, int, error) {
	if index < 0 {
		return "", -1, errNoCandidates
	}

	if candidates, _, err := btp.t.GetCandidateFiles(btp); err == nil {
		if len(candidates) <= index {
			return "", -1, errNoCandidates
		}

		return candidates[index].Filename, index, nil
	}

	return "", -1, fmt.Errorf("Not able to find candidate file")
}

func contextPlayURL(f string, title string, forced bool) string {
	action := "links"
	if strings.Contains(f, "movie") && config.Get().ChooseStreamAutoMovie {
		action = "play"
	} else if strings.Contains(f, "show") && config.Get().ChooseStreamAutoShow {
		action = "play"
	} else if strings.Contains(f, "search") && config.Get().ChooseStreamAutoSearch {
		action = "play"
	}

	if forced {
		action = "force" + action
	}

	return fmt.Sprintf(f, action, url.PathEscape(title))
}

func getShowSeasonEpisode(showID, seasonNumber, episodeNumber int) (*tmdb.Show, *tmdb.Season, *tmdb.Episode, error) {
	if episodeNumber <= 0 {
		return nil, nil, nil, errors.New("Wrong episode number")
	}

	show := tmdb.GetShow(showID, config.Get().Language)
	if show == nil {
		return nil, nil, nil, errors.New("Unable to find show")
	}

	season := tmdb.GetSeason(showID, seasonNumber, config.Get().Language, len(show.Seasons))
	if season == nil {
		return nil, nil, nil, errors.New("Unable to find season")
	}

	if !season.HasEpisode(episodeNumber) {
		return nil, nil, nil, errors.New("Unable to find episode")
	}

	return show, season, season.GetEpisode(episodeNumber), nil
}

func getNextShowSeasonEpisode(showID, seasonNumber, episodeNumber int) (*tmdb.Show, *tmdb.Season, *tmdb.Episode, error) {
	show := tmdb.GetShow(showID, config.Get().Language)
	if show == nil {
		return nil, nil, nil, errors.New("Unable to find show")
	}

	season := tmdb.GetSeason(showID, seasonNumber, config.Get().Language, len(show.Seasons))
	if season == nil {
		return nil, nil, nil, errors.New("Unable to find season")
	}

	episodeNumber++
	if !season.HasEpisode(episodeNumber) {
		return getShowSeasonEpisode(showID, seasonNumber+1, 1)
	}

	return show, season, season.GetEpisode(episodeNumber), nil
}

// TrimChoices clears redundant folder names from files list and sorts remaining records.
func TrimChoices(choices []*CandidateFile) {
	// We are trying to see whether all files belong to the same directory.
	// If yes - we can remove that directory from printed files list
	sep := string(os.PathSeparator)
	for _, d := range strings.Split(choices[0].DisplayName, sep) {
		ret := true
		for _, c := range choices {
			if !strings.HasPrefix(c.DisplayName, d+sep) {
				ret = false
				break
			}
		}

		if ret {
			for _, c := range choices {
				c.DisplayName = strings.Replace(c.DisplayName, d+sep, "", 1)
			}
		} else {
			break
		}
	}

	sort.Slice(choices, func(i, j int) bool {
		return choices[i].DisplayName < choices[j].DisplayName
	})

	if !config.Get().ShowFilesWatched {
		return
	}

	anyWatched := false
	watched := make([]bool, len(choices))
	for i, c := range choices {
		watched[i] = IsWatchedFile(c.Path, c.Size)
		if watched[i] {
			anyWatched = true
		}
	}

	if anyWatched {
		for i, c := range choices {
			if watched[i] {
				c.DisplayName = " [COLOR green][B]+[/B][/COLOR] | " + c.DisplayName
			} else {
				c.DisplayName = " [COLOR green] [/COLOR] | " + c.DisplayName
			}
		}
	}
}

// MatchEpisodeFilename matches season and episode in the filename to get ocurrence
func MatchEpisodeFilename(s, e int, isSingleSeason bool, activeSeason int, show *tmdb.Show, episode *tmdb.Episode, tvdbShow *tvdb.Show, choices []*CandidateFile) (index, found int) {
	index = -1

	re := regexp.MustCompile(fmt.Sprintf(episodeMatchRegex, s, e))
	for i, choice := range choices {
		if re.MatchString(choice.Filename) {
			index = i
			found++
		}
	}

	if isSingleSeason && found == 0 {
		re := regexp.MustCompile(fmt.Sprintf(singleEpisodeMatchRegex, e))
		for i, choice := range choices {
			if re.MatchString(choice.Filename) {
				index = i
				found++
			}
		}
	}

	if found == 0 && show != nil && episode != nil && show.IsAnime() {
		if an, _ := show.AnimeInfoWithShow(episode, tvdbShow); an != 0 {
			re := regexp.MustCompile(fmt.Sprintf(singleEpisodeMatchRegex, an))
			for i, choice := range choices {
				if re.MatchString(choice.Filename) {
					index = i
					found++
				}
			}
		}
	}

	if found == 0 && activeSeason == s {
		re := regexp.MustCompile(fmt.Sprintf(singleEpisodeMatchRegex, e))
		for i, choice := range choices {
			if re.MatchString(choice.Filename) {
				index = i
				found++
			}
		}
	}

	return
}

func removeTrailingMinus(in string) string {
	if strings.HasPrefix(in, "-") {
		return in[1:]
	}

	return in
}

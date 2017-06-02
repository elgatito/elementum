package bittorrent

import (
	"os"
	"fmt"
	// "math"
	"sort"
	// "sync"
	"time"
	"bufio"
	"errors"
	"regexp"
	"strconv"
	"strings"
	"os/exec"
	"io/ioutil"
	// "encoding/hex"
	"path/filepath"

	"github.com/op/go-logging"
	"github.com/dustin/go-humanize"
	// "github.com/scakemyer/libtorrent-go"
	gotorrent "github.com/anacrolix/torrent"
	"github.com/scakemyer/quasar/broadcast"
	// "github.com/scakemyer/quasar/diskusage"
	"github.com/scakemyer/quasar/config"
	"github.com/scakemyer/quasar/trakt"
	"github.com/scakemyer/quasar/xbmc"
	// "github.com/zeebo/bencode"
)

const (
	// startBufferPercent = 0.005
	endBufferSize      int64 = 10 * 1024 * 1024 // 10m
	playbackMaxWait    = 20 * time.Second
	minCandidateSize   = 100 * 1024 * 1024
)

var (
	Paused        bool
	Seeked        bool
	Playing       bool
	WasPlaying    bool
	FromLibrary   bool
	WatchedTime   float64
	VideoDuration float64
)

type BTPlayer struct {
	bts                      *BTService
	log                      *logging.Logger
	dialogProgress           *xbmc.DialogProgress
	overlayStatus            *xbmc.OverlayStatus
	uri                      string
	torrentFile              string
	contentType              string
	fileIndex                int
	resumeIndex              int
	tmdbId                   int
	showId                   int
	season                   int
	episode                  int
	scrobble                 bool
	deleteAfter              bool
	keepDownloading          int
	keepFilesPlaying         int
	keepFilesFinished        int
	overlayStatusEnabled     bool
	Torrent                  *Torrent
	chosenFile               *gotorrent.File
	subtitlesFile            *gotorrent.File
	fileSize                 int64
	fileName                 string
	// lastStatus               libtorrent.TorrentStatus
	bufferPiecesProgress     map[int]float64
	// bufferPiecesProgressLock sync.RWMutex
	torrentName              string
	extracted                string
	hasChosenFile            bool
	isDownloading            bool
	notEnoughSpace           bool
	bufferEvents             *broadcast.Broadcaster
	closing                  chan interface{}

  DBID                     int
  DBTYPE                   string
}

type BTPlayerParams struct {
	URI          string
	FileIndex    int
	ResumeIndex  int
	FromLibrary  bool
	ContentType  string
	TMDBId       int
	ShowID       int
	Season       int
	Episode      int
}

type candidateFile struct {
	Index     int
	Filename  string
}

type byFilename []*candidateFile
func (a byFilename) Len() int           { return len(a) }
func (a byFilename) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }
func (a byFilename) Less(i, j int) bool { return a[i].Filename < a[j].Filename }

func NewBTPlayer(bts *BTService, params BTPlayerParams) *BTPlayer {
	Playing = true
	if params.FromLibrary {
		FromLibrary = true
	}
	btp := &BTPlayer{
		log:                  logging.MustGetLogger("btplayer"),
		bts:                  bts,
		uri:                  params.URI,
		fileIndex:            params.FileIndex,
		resumeIndex:          params.ResumeIndex,
		fileSize:             0,
		fileName:             "",
		overlayStatusEnabled: config.Get().EnableOverlayStatus == true,
		keepDownloading:      config.Get().KeepDownloading,
		keepFilesPlaying:     config.Get().KeepFilesPlaying,
		keepFilesFinished:    config.Get().KeepFilesFinished,
		scrobble:             config.Get().Scrobble == true && params.TMDBId > 0 && config.Get().TraktToken != "",
		contentType:          params.ContentType,
		tmdbId:               params.TMDBId,
		showId:               params.ShowID,
		season:               params.Season,
		episode:              params.Episode,
		torrentFile:          "",
		hasChosenFile:        false,
		isDownloading:        false,
		notEnoughSpace:       false,
		closing:              make(chan interface{}),
		bufferEvents:         broadcast.NewBroadcaster(),
		bufferPiecesProgress: map[int]float64{},
	}
	return btp
}

func (btp *BTPlayer) addTorrent() error {
	torrent, err := btp.bts.AddTorrent(btp.uri)
	if err != nil {
		return err
	}

	btp.Torrent = torrent
	infoHash := btp.Torrent.InfoHash()

	btp.torrentFile = filepath.Join(btp.bts.config.TorrentsPath, fmt.Sprintf("%s.torrent", infoHash))

	// btp.log.Infof("Checking for fast resume data in %s.fastresume", infoHash)
	// fastResumeFile := filepath.Join(btp.bts.config.TorrentsPath, fmt.Sprintf("%s.fastresume", infoHash))
	// btp.fastResumeFile = fastResumeFile
	// if _, err := os.Stat(fastResumeFile); err == nil {
	// 	btp.log.Info("Found fast resume data")
	// 	fastResumeData, err := ioutil.ReadFile(fastResumeFile)
	// 	if err != nil {
	// 		return err
	// 	}
	// 	fastResumeVector := libtorrent.NewStdVectorChar()
	// 	defer libtorrent.DeleteStdVectorChar(fastResumeVector)
	// 	for _, c := range fastResumeData {
	// 		fastResumeVector.Add(c)
	// 	}
	// 	torrentParams.SetResumeData(fastResumeVector)
	// }

	// btp.Torrent = btp.bts.Session.GetHandle().AddTorrent(torrentParams)

	if btp.Torrent == nil {
		return fmt.Errorf("Unable to add torrent with URI %s", btp.uri)
	}

	// btp.log.Info("Enabling sequential download")
	// btp.Torrent.SetSequentialDownload(true)

	// status := btp.Torrent.Status(uint(libtorrent.TorrentQueryName))

	// btp.torrentName = status.GetName()
	btp.torrentName = btp.Torrent.Info().Name
	btp.log.Infof("Downloading %s", btp.torrentName)

	// if status.GetHasMetadata() == true {
	// 	btp.onMetadataReceived()
	// }

	go btp.Torrent.Watch()

	btp.onMetadataReceived()

	return nil
}

// func (btp *BTPlayer) getLargestFile() *gotorrent.File {
// 	var target gotorrent.File
// 	var maxSize int64
//
// 	for _, file := range btp.Torrent.Files() {
// 		if maxSize < file.Length() {
// 			maxSize = file.Length()
// 			target = file
// 		}
// 	}
//
// 	return &target
// }

// func (btp *BTPlayer) percentage() float64 {
// 	info := btp.Torrent.Info()
//
// 	if info == nil {
// 		return 0
// 	}
//
// 	return float64(btp.Torrent.BytesCompleted() / (btp.Torrent.BytesCompleted() + btp.Torrent.BytesMissing())) * 100
// }
//
// func (btp *BTPlayer) downloadFile(URL string) (fileName string, err error) {
// 	var file *os.File
// 	if file, err = ioutil.TempFile(os.TempDir(), "quasar"); err != nil {
// 		return
// 	}
//
// 	defer func() {
// 		if ferr := file.Close(); ferr != nil {
// 			btp.log.Debugf("Error closing torrent file: %s", ferr)
// 		}
// 	}()
//
// 	response, err := http.Get(URL)
// 	if err != nil {
// 		return
// 	}
//
// 	defer func() {
// 		if ferr := response.Body.Close(); ferr != nil {
// 			btp.log.Debugf("Error closing torrent file: %s", ferr)
// 		}
// 	}()
//
// 	_, err = io.Copy(file, response.Body)
//
// 	return file.Name(), err
// }

func (btp *BTPlayer) resumeTorrent() error {
	// torrentsVector := btp.bts.Session.GetHandle().GetTorrents()
	// btp.Torrent = torrentsVector.Get(btp.resumeIndex)
	// go btp.consumeAlerts()
	//
	// if btp.Torrent == nil {
	// 	return fmt.Errorf("Unable to resume torrent with index %d", btp.resumeIndex)
	// }
	//
	// btp.log.Info("Enabling sequential download")
	// btp.Torrent.SetSequentialDownload(true)
	//
	// status := btp.Torrent.Status(uint(libtorrent.TorrentQueryName))
	//
	// shaHash := status.GetInfoHash().ToString()
	// infoHash := hex.EncodeToString([]byte(shaHash))
	// btp.torrentFile = filepath.Join(btp.bts.config.TorrentsPath, fmt.Sprintf("%s.torrent", infoHash))
	//
	// btp.torrentName = status.GetName()
	// btp.log.Infof("Resuming %s", btp.torrentName)
	//
	// if status.GetHasMetadata() == true {
	// 	btp.onMetadataReceived()
	// }
	//
	// btp.Torrent.AutoManaged(true)

	return nil
}

func (btp *BTPlayer) PlayURL() string {
	if btp.Torrent.IsRarArchive {
		extractedPath := filepath.Join(filepath.Dir(btp.chosenFile.Path()), "extracted", btp.extracted)
		return strings.Join(strings.Split(extractedPath, string(os.PathSeparator)), "/")
	} else {
		return strings.Join(strings.Split(btp.chosenFile.Path(), string(os.PathSeparator)), "/")
	}
}

func (btp *BTPlayer) Buffer() error {
	if btp.resumeIndex >= 0 {
		if err := btp.resumeTorrent(); err != nil {
			return err
		}
	} else {
		if err := btp.addTorrent(); err != nil {
			return err
		}
	}

	buffered, done := btp.bufferEvents.Listen()
	defer close(done)

	btp.dialogProgress = xbmc.NewDialogProgress("Quasar", "", "", "")
	defer btp.dialogProgress.Close()

	btp.overlayStatus = xbmc.NewOverlayStatus()

	go btp.waitCheckAvailableSpace()
	go btp.playerLoop()

	if err := <-buffered; err != nil {
		return err.(error)
	}
	return nil
}

func (btp *BTPlayer) waitCheckAvailableSpace() {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if btp.hasChosenFile && btp.isDownloading {
				status := btp.bts.CheckAvailableSpace(btp.Torrent)
				if !status {
					btp.bufferEvents.Broadcast(errors.New("Not enough space on download destination."))
					btp.notEnoughSpace = true
				}

				return
			}
		}
	}
}

func (btp *BTPlayer) onMetadataReceived() {
	btp.log.Info("Metadata received.")

	// if btp.resumeIndex < 0 {
	// 	btp.Torrent.AutoManaged(false)
	// 	btp.Torrent.Pause()
	// 	defer btp.Torrent.AutoManaged(true)
	// }

	btp.torrentName = btp.Torrent.Info().Name

	var err error
	btp.chosenFile, err = btp.chooseFile()
	if err != nil {
		btp.bufferEvents.Broadcast(err)
		return
	}

	infoHash := btp.Torrent.InfoHash()

	btp.hasChosenFile = true
	btp.fileSize = btp.chosenFile.Length()
	btp.fileName = filepath.Base( btp.chosenFile.Path() )
	btp.subtitlesFile = btp.findSubtitlesFile()
	btp.log.Infof("Chosen file: %s", btp.fileName)

	btp.log.Infof("Saving torrent to database")
	btp.bts.UpdateDB(Update, infoHash, btp.tmdbId, btp.contentType, int(btp.chosenFile.Offset()), btp.showId, btp.season, btp.episode)

	if btp.Torrent.IsRarArchive {
		// Just disable sequential download for RAR archives
		// btp.log.Info("Disabling sequential download")
		// btp.Torrent.SetSequentialDownload(false)
		return
	}

	btp.log.Info("Setting file priorities")
	if btp.chosenFile != nil {
		btp.Torrent.DownloadFile(btp.chosenFile)
	}
	if btp.subtitlesFile != nil {
		btp.Torrent.DownloadFile(btp.subtitlesFile)
	}

	// btp.log.Info("Setting file priorities")
	// for i, f := range btp.Torrent.Files() {
	// 	if i == btp.chosenFile {
	// 		f.Download()
	// 	} else if i == btp.subtitlesFile {
	// 		f.Download()
	// 	}
	// }

	btp.log.Info("Setting piece priorities")

	go btp.Torrent.Buffer(btp.chosenFile)


	// if btp.resumeIndex < 0 {
	// 	btp.Torrent.AutoManaged(false)
	// 	btp.Torrent.Pause()
	// 	defer btp.Torrent.AutoManaged(true)
	// }
	//
	// btp.torrentName = btp.Torrent.Status(uint(libtorrent.TorrentQueryName)).GetName()
	//
	// btp.torrentInfo = btp.Torrent.TorrentFile()
	//
	// if btp.resumeIndex < 0 {
	// 	// Save .torrent
	// 	btp.log.Infof("Saving %s", btp.torrentFile)
	// 	torrentFile := libtorrent.NewCreateTorrent(btp.torrentInfo)
	// 	defer libtorrent.DeleteCreateTorrent(torrentFile)
	// 	torrentContent := torrentFile.Generate()
	// 	bEncodedTorrent := []byte(libtorrent.Bencode(torrentContent))
	// 	ioutil.WriteFile(btp.torrentFile, bEncodedTorrent, 0644)
	// }
	//
	// // Reset fastResumeFile
	// shaHash := btp.torrentInfo.InfoHash().ToString()
	// infoHash := hex.EncodeToString([]byte(shaHash))
	// btp.fastResumeFile = filepath.Join(btp.bts.config.TorrentsPath, fmt.Sprintf("%s.fastresume", infoHash))
	// btp.partsFile = filepath.Join(btp.bts.config.DownloadPath, fmt.Sprintf(".%s.parts", infoHash))
	//
	// var err error
	// btp.chosenFile, err = btp.chooseFile()
	// if err != nil {
	// 	btp.bufferEvents.Broadcast(err)
	// 	return
	// }
	// btp.hasChosenFile = true
	// files := btp.torrentInfo.Files()
	// btp.fileSize = files.FileSize(btp.chosenFile)
	// fileName := filepath.Base(files.FilePath(btp.chosenFile))
	// btp.fileName = fileName
	// btp.log.Infof("Chosen file: %s", fileName)
	//
	// btp.subtitlesFile = btp.findSubtitlesFile()
	//
	// btp.log.Infof("Saving torrent to database")
	// btp.bts.UpdateDB(Update, infoHash, btp.tmdbId, btp.contentType, btp.chosenFile, btp.showId, btp.season, btp.episode)
	//
	// if btp.isRarArchive {
	// 	// Just disable sequential download for RAR archives
	// 	btp.log.Info("Disabling sequential download")
	// 	btp.Torrent.SetSequentialDownload(false)
	// 	return
	// }
	//
	// // Set all file priorities to 0 except chosen file
	// btp.log.Info("Setting file priorities")
	// numFiles := btp.torrentInfo.NumFiles()
	// filesPriorities := libtorrent.NewStdVectorInt()
	// defer libtorrent.DeleteStdVectorInt(filesPriorities)
	// for i := 0; i < numFiles; i++ {
	// 	if i == btp.chosenFile {
	// 		filesPriorities.Add(4)
	// 	} else if i == btp.subtitlesFile {
	// 		filesPriorities.Add(4)
	// 	} else {
	// 		filesPriorities.Add(0)
	// 	}
	// }
	// btp.Torrent.PrioritizeFiles(filesPriorities)
	//
	// btp.log.Info("Setting piece priorities")
	//
	// pieceLength := float64(btp.torrentInfo.PieceLength())
	//
	// startPiece, endPiece, _ := btp.getFilePiecesAndOffset(btp.chosenFile)
	//
	// startLength := float64(endPiece-startPiece) * float64(pieceLength) * startBufferPercent
	// if startLength < float64(btp.bts.config.BufferSize) {
	// 	startLength = float64(btp.bts.config.BufferSize)
	// }
	// startBufferPieces := int(math.Ceil(startLength / pieceLength))
	//
	// // Prefer a fixed size, since metadata are very rarely over endPiecesSize=10MB
	// // anyway.
	// endBufferPieces := int(math.Ceil(float64(endBufferSize) / pieceLength))
	//
	// piecesPriorities := libtorrent.NewStdVectorInt()
	// defer libtorrent.DeleteStdVectorInt(piecesPriorities)
	//
	// btp.bufferPiecesProgressLock.Lock()
	// defer btp.bufferPiecesProgressLock.Unlock()
	//
	// // Properly set the pieces priority vector
	// curPiece := 0
	// for _ = 0; curPiece < startPiece; curPiece++ {
	// 	piecesPriorities.Add(0)
	// }
	// for _ = 0; curPiece < startPiece + startBufferPieces; curPiece++ { // get this part
	// 	piecesPriorities.Add(7)
	// 	btp.bufferPiecesProgress[curPiece] = 0
	// 	btp.Torrent.SetPieceDeadline(curPiece, 0, 0)
	// }
	// for _ = 0; curPiece < endPiece - endBufferPieces; curPiece++ {
	// 	piecesPriorities.Add(1)
	// }
	// for _ = 0; curPiece <= endPiece; curPiece++ { // get this part
	// 	piecesPriorities.Add(7)
	// 	btp.bufferPiecesProgress[curPiece] = 0
	// 	btp.Torrent.SetPieceDeadline(curPiece, 0, 0)
	// }
	// numPieces := btp.torrentInfo.NumPieces()
	// for _ = 0; curPiece < numPieces; curPiece++ {
	// 	piecesPriorities.Add(0)
	// }
	// btp.Torrent.PrioritizePieces(piecesPriorities)
}

func (btp *BTPlayer) statusStrings(progress float64) (string, string, string) {
	line1 := fmt.Sprintf("%s (%.2f%%)", btp.Torrent.GetStateString(), progress)
	var totalSize int64
	if btp.fileSize > 0 && !btp.Torrent.IsRarArchive {
		totalSize = btp.fileSize
	} else {
		totalSize = btp.Torrent.BytesCompleted() + btp.Torrent.BytesMissing()
	}
	line1 += " - " + humanize.Bytes(uint64(totalSize))

	line2 := fmt.Sprintf("D:%.0fkB/s U:%.0fkB/s S:%d/%d",
		float64(btp.Torrent.DownloadRate) / 1024,
		float64(btp.Torrent.UploadRate) / 1024,
		btp.Torrent.Stats().ActivePeers,
		btp.Torrent.Stats().TotalPeers,
	)
	line3 := ""
	if btp.fileName != "" && !btp.Torrent.IsRarArchive {
		line3 = btp.fileName
	} else {
		line3 = btp.torrentName
	}
	return line1, line2, line3


	// line1 := fmt.Sprintf("%s (%.2f%%)", StatusStrings[int(status.GetState())], progress * 100)
	// if btp.torrentInfo != nil && btp.torrentInfo.Swigcptr() != 0 {
	// 	var totalSize int64
	// 	if btp.fileSize > 0 && !btp.isRarArchive {
	// 		totalSize = btp.fileSize
	// 	} else {
	// 		totalSize = btp.torrentInfo.TotalSize()
	// 	}
	// 	line1 += " - " + humanize.Bytes(uint64(totalSize))
	// }
	// seeders := status.GetNumSeeds()
	// line2 := fmt.Sprintf("D:%.0fkB/s U:%.0fkB/s S:%d/%d P:%d/%d",
	// 	float64(status.GetDownloadRate()) / 1024,
	// 	float64(status.GetUploadRate()) / 1024,
	// 	seeders,
	// 	status.GetNumComplete(),
	// 	status.GetNumPeers() - seeders,
	// 	status.GetNumIncomplete(),
	// )
	// line3 := ""
	// if btp.fileName != "" && !btp.isRarArchive {
	// 	line3 = btp.fileName
	// } else {
	// 	line3 = btp.torrentName
	// }
	// return line1, line2, line3
}

func (btp *BTPlayer) chooseFile() (*gotorrent.File, error) {
	var biggestFile int
	maxSize := int64(0)
	// numFiles := btp.torrentInfo.NumFiles()
	files := btp.Torrent.Files()
	var candidateFiles []int

	for i, f := range files {
		size := f.Length()
		if size > maxSize {
			maxSize = size
			biggestFile = i
		}
		if size > minCandidateSize {
			candidateFiles = append(candidateFiles, i)
		}

		fileName := filepath.Base(f.Path())
		re := regexp.MustCompile("(?i).*\\.rar")
		if re.MatchString(fileName) && size > 10 * 1024 * 1024 {
			btp.Torrent.IsRarArchive = true
			if !xbmc.DialogConfirm("Quasar", "LOCALIZE[30303]") {
				btp.notEnoughSpace = true
				return &f, errors.New("RAR archive detected and download was cancelled")
			}
			return &f, nil
		}
	}

	if len(candidateFiles) > 1 {
		btp.log.Info(fmt.Sprintf("There are %d candidate files", len(candidateFiles)))
		if btp.fileIndex >= 0 && btp.fileIndex < len(candidateFiles) {
			return &files[candidateFiles[btp.fileIndex]], nil
		}

		choices := make(byFilename, 0, len(candidateFiles))
		for _, index := range candidateFiles {
			fileName := filepath.Base(files[index].Path())
			candidate := &candidateFile{
				Index:    index,
				Filename: fileName,
			}
			choices = append(choices, candidate)
		}

		if btp.episode > 0 {
			var lastMatched int
			var foundMatches int
			// Case-insensitive, starting with a line-start or non-ascii, can have leading zeros, followed by non-ascii
			// TODO: Add logic for matching S01E0102 (double episode filename)
			re := regexp.MustCompile(fmt.Sprintf("(?i)(^|\\W)S0*?%dE0*?%d\\W", btp.season, btp.episode))
			for index, choice := range choices {
				if re.MatchString(choice.Filename) {
					lastMatched = index
					foundMatches++
				}
			}

			if foundMatches == 1 {
				return &files[choices[lastMatched].Index], nil
			}
		}

		sort.Sort(byFilename(choices))

		items := make([]string, 0, len(choices))
		for _, choice := range choices {
			items = append(items, choice.Filename)
		}

		choice := xbmc.ListDialog("LOCALIZE[30223]", items...)
		if choice >= 0 {
			return &files[choices[choice].Index], nil
		} else {
			return nil, fmt.Errorf("User cancelled")
		}
	}

	return &files[biggestFile], nil
}

func (btp *BTPlayer) findSubtitlesFile() (*gotorrent.File) {
	extension := filepath.Ext(btp.fileName)
	chosenName := btp.fileName[0:len(btp.fileName)-len(extension)]
	srtFileName := chosenName + ".srt"

	files := btp.Torrent.Files()

	var lastMatched *gotorrent.File;
	countMatched := 0;

	for _, file := range files {
		fileName := file.Path()
		if strings.HasSuffix(fileName, srtFileName) {
			return &file
		} else if strings.HasSuffix(fileName, ".srt") {
			lastMatched = &file
			countMatched++
		}
	}

	if countMatched == 1 {
		return lastMatched
	}

	return nil
}

// func (btp *BTPlayer) onStateChanged(stateAlert libtorrent.StateChangedAlert) {
// 	switch stateAlert.GetState() {
// 	case libtorrent.TorrentStatusDownloading:
// 		btp.isDownloading = true
// 	}
// }

func (btp *BTPlayer) Close() {
	close(btp.closing)

	isWatched := btp.IsWatched()
	keepDownloading := false
	if btp.keepDownloading == 2 {
		keepDownloading = false
	} else if btp.keepDownloading == 0 || xbmc.DialogConfirm("Quasar", "LOCALIZE[30146]") {
		keepDownloading = true
	} else {
		keepDownloading = false
	}

	keepSetting := 1
	if isWatched {
		keepSetting = btp.keepFilesFinished
	} else {
		keepSetting = btp.keepFilesPlaying
	}

	deleteAnswer := false
	if keepDownloading == false {
		if keepSetting == 0 {
			deleteAnswer = false
		} else if keepSetting == 2 || xbmc.DialogConfirm("Quasar", "LOCALIZE[30269]") {
			deleteAnswer = true
		}
	}

	if keepDownloading == false || deleteAnswer == true || btp.notEnoughSpace {
		// Delete torrent file
		if _, err := os.Stat(btp.torrentFile); err == nil {
			btp.log.Infof("Deleting torrent file at %s", btp.torrentFile)
			defer os.Remove(btp.torrentFile)
		}

		infoHash := btp.Torrent.InfoHash()

		btp.bts.UpdateDB(Delete, infoHash, 0, "")
		btp.log.Infof("Removed %s from database", btp.Torrent.Name())

		if btp.deleteAfter || deleteAnswer == true || btp.notEnoughSpace {
			btp.log.Info("Removing the torrent and deleting files...")
			btp.bts.RemoveTorrent(btp.Torrent, true)
		} else {
			btp.log.Info("Removing the torrent without deleting files...")
			btp.bts.RemoveTorrent(btp.Torrent, false)
		}
	}
}

func (btp *BTPlayer) bufferDialog() {
	halfSecond := time.NewTicker(500 * time.Millisecond)
	defer halfSecond.Stop()
	oneSecond := time.NewTicker(1 * time.Second)
	defer oneSecond.Stop()

	for {
		select {
		case <-halfSecond.C:
			if btp.dialogProgress.IsCanceled() || btp.notEnoughSpace {
				errMsg := "User cancelled the buffering"
				btp.log.Info(errMsg)
				btp.bufferEvents.Broadcast(errors.New(errMsg))
				return
			}
		case <-oneSecond.C:
			status := btp.Torrent.GetState()

			// Handle "Checking" state for resumed downloads
			if status == STATUS_CHECKING || btp.Torrent.IsRarArchive {
				progress := btp.Torrent.GetBufferProgress()
				line1, line2, line3 := btp.statusStrings(progress)
				btp.dialogProgress.Update(int(progress), line1, line2, line3)

				if btp.Torrent.IsRarArchive && progress >= 100 {
					archivePath := filepath.Join(btp.bts.config.DownloadPath, btp.chosenFile.Path())
					destPath := filepath.Join(btp.bts.config.DownloadPath, filepath.Dir(btp.chosenFile.Path()), "extracted")

					if _, err := os.Stat(destPath); err == nil {
						btp.findExtracted(destPath)
						btp.bufferEvents.Signal()
						return
					} else {
						os.MkdirAll(destPath, 0755)
					}

					cmdName := "unrar"
					cmdArgs := []string{"e", archivePath, destPath}
					cmd := exec.Command(cmdName, cmdArgs...)
					if platform := xbmc.GetPlatform(); platform.OS == "windows" {
						cmdName = "unrar.exe"
					}

					cmdReader, err := cmd.StdoutPipe()
					if err != nil {
						btp.log.Error(err)
						btp.bufferEvents.Broadcast(err)
						xbmc.Notify("Quasar", "LOCALIZE[30304]", config.AddonIcon())
						return
					}

					scanner := bufio.NewScanner(cmdReader)
					go func() {
						for scanner.Scan() {
							btp.log.Infof("unrar | %s", scanner.Text())
						}
					}()

					err = cmd.Start()
					if err != nil {
						btp.log.Error(err)
						btp.bufferEvents.Broadcast(err)
						xbmc.Notify("Quasar", "LOCALIZE[30305]", config.AddonIcon())
						return
					}

					err = cmd.Wait()
					if err != nil {
						btp.log.Error(err)
						btp.bufferEvents.Broadcast(err)
						xbmc.Notify("Quasar", "LOCALIZE[30306]", config.AddonIcon())
						return
					}

					btp.findExtracted(destPath)
					// btp.setRateLimiting(true)
					btp.bufferEvents.Signal()
					return
				}
			} else {
				line1, line2, line3 := btp.statusStrings(btp.Torrent.BufferProgress)
				btp.dialogProgress.Update(int(btp.Torrent.BufferProgress), line1, line2, line3)
				if !btp.Torrent.IsBuffering  && btp.Torrent.GetState() != STATUS_CHECKING {
					btp.bufferEvents.Signal()
					return
				}
			}
		}
	}
}

func (btp *BTPlayer) findExtracted(destPath string) {
	files, err := ioutil.ReadDir(destPath)
	if err != nil {
		btp.log.Error(err)
		btp.bufferEvents.Broadcast(err)
		xbmc.Notify("Quasar", "LOCALIZE[30307]", config.AddonIcon())
		return
	}
	if len(files) == 1 {
		btp.log.Info("Extracted", files[0].Name())
		btp.extracted = files[0].Name()
	} else {
		for _, file := range files {
			fileName := file.Name()
			re := regexp.MustCompile("(?i).*\\.(mkv|mp4|mov|avi)")
			if re.MatchString(fileName) {
				btp.log.Info("Extracted", fileName)
				btp.extracted = fileName
				break
			}
		}
	}
}

func updateWatchTimes() {
	ret := xbmc.GetWatchTimes()
	err := ret["error"]
	if err == "" {
		WatchedTime, _ = strconv.ParseFloat(ret["watchedTime"], 64)
		VideoDuration, _ = strconv.ParseFloat(ret["videoDuration"], 64)
	}
}

func (btp *BTPlayer) playerLoop() {
	defer btp.Close()

	btp.log.Info("Buffer loop")

	buffered, bufferDone := btp.bufferEvents.Listen()
	defer close(bufferDone)

	go btp.bufferDialog()

	if err := <-buffered; err != nil {
		return
	}

	btp.log.Info("Waiting for playback...")
	oneSecond := time.NewTicker(1 * time.Second)
	defer oneSecond.Stop()
	playbackTimeout := time.After(playbackMaxWait)

playbackWaitLoop:
	for {
		if xbmc.PlayerIsPlaying() {
			break playbackWaitLoop
		}
		select {
		case <-playbackTimeout:
			btp.log.Warningf("Playback was unable to start after %d seconds. Aborting...", playbackMaxWait / time.Second)
			btp.bufferEvents.Broadcast(errors.New("Playback was unable to start before timeout."))
			return
		case <-oneSecond.C:
		}
	}

	btp.log.Info("Playback loop")
	overlayStatusActive := false
	playing := true

	updateWatchTimes()

	btp.log.Infof("Got playback: %fs / %fs", WatchedTime, VideoDuration)
	if btp.scrobble {
		trakt.Scrobble("start", btp.contentType, btp.tmdbId, WatchedTime, VideoDuration)
	}

playbackLoop:
	for {
		if xbmc.PlayerIsPlaying() == false {
			break playbackLoop
		}
		select {
		case <-oneSecond.C:
			if Seeked {
				Seeked = false
				updateWatchTimes()
				if btp.scrobble {
					trakt.Scrobble("start", btp.contentType, btp.tmdbId, WatchedTime, VideoDuration)
				}
			} else if xbmc.PlayerIsPaused() {
				if playing == true {
					playing = false
					updateWatchTimes()
					if btp.scrobble {
						trakt.Scrobble("pause", btp.contentType, btp.tmdbId, WatchedTime, VideoDuration)
					}
				}
				if btp.overlayStatusEnabled == true {
					// status := btp.bts.GetState(btp.Torrent)
					progress := btp.Torrent.GetProgress()
					line1, line2, line3 := btp.statusStrings(progress)
					// btp.log.Debugf("Dialog overlay: %#v; %#v; %#v; %#v", progress, line1, line2, line3)
					btp.overlayStatus.Update(int(progress), line1, line2, line3)
					if overlayStatusActive == false {
						btp.overlayStatus.Show()
						overlayStatusActive = true
					}
				}
			} else {
				updateWatchTimes()
				if playing == false {
					playing = true
					if btp.scrobble {
						trakt.Scrobble("start", btp.contentType, btp.tmdbId, WatchedTime, VideoDuration)
					}
				}
				if overlayStatusActive == true {
					btp.overlayStatus.Hide()
					overlayStatusActive = false
				}
			}
		}
	}

	btp.log.Info("Stopped playback")
	if btp.scrobble {
		trakt.Scrobble("stop", btp.contentType, btp.tmdbId, WatchedTime, VideoDuration)
	}
	Paused = false
	Seeked = false
	Playing = false
	WasPlaying = true
	FromLibrary = false
	WatchedTime = 0
	VideoDuration = 0

	btp.overlayStatus.Close()
}

func (btp *BTPlayer) IsWatched() bool {
	return (WatchedTime / VideoDuration * 100) > 90
}

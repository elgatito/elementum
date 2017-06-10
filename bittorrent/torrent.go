package bittorrent

import (
	"os"
	// "io"
	// "fmt"
	"math"
	"time"
	"sync"
	// "bytes"
	// "regexp"
	"strings"
	// "net/url"
	// "net/http"
	// "crypto/sha1"
	// "encoding/hex"
	// "encoding/json"
	// "encoding/base32"
	"path/filepath"

	gotorrent "github.com/anacrolix/torrent"
	"github.com/op/go-logging"
	// "github.com/dustin/go-humanize"
	// "github.com/scakemyer/quasar/cloudhole"
	// "github.com/scakemyer/quasar/config"
	// "github.com/zeebo/bencode"

	"github.com/scakemyer/quasar/xbmc"
	qstorage "github.com/scakemyer/quasar/storage"
)

var log = logging.MustGetLogger("torrent")

const (
	STATUS_QUEUED = iota
	STATUS_CHECKING
	STATUS_FINDING
	STATUS_PAUSED
	STATUS_BUFFERING
	STATUS_DOWNLOADING
	STATUS_FINISHED
	STATUS_SEEDING
	STATUS_ALLOCATING
	STATUS_STALLED
)

var StatusStrings = []string{
	"Queued",
	"Checking",
	"Finding",
	"Paused",
	"Buffering",
	"Downloading",
	"Finished",
	"Seeding",
	"Allocating",
	"Stalled",
}

type Torrent struct {
	*gotorrent.Torrent

	bufferReader             *Reader
	postReader               *Reader

	ChosenFiles 						 []*gotorrent.File
	TorrentPath              string

	Service 								 *BTService
	DownloadRate 						 int64
	UploadRate 						   int64

	BufferProgress 					 float64
	BufferPiecesProgress     map[int]float64

	IsPlaying                bool
	IsPaused 								 bool
	IsBuffering 						 bool
	IsSeeding                bool

	IsRarArchive 						 bool

	needSeeding              bool
	needDBID                 bool

	DBID                     int
	DBTYPE                   string
	DBItem                   *DBItem

	muBuffer                 *sync.RWMutex
	muSeeding                *sync.RWMutex

	closing                  chan struct{}

	seedTicker               *time.Ticker
	dbidTicker               *time.Ticker
}


func NewTorrent(service *BTService, handle *gotorrent.Torrent, path string) *Torrent {
	t := &Torrent{
		Service: 		  service,
		Torrent: 		  handle,
		TorrentPath:  path,

		BufferPiecesProgress: map[int]float64{},
		BufferProgress:       -1,

		needSeeding:          true,
		needDBID:             true,

		muBuffer:             &sync.RWMutex{},
		muSeeding: 						&sync.RWMutex{},

		closing:              make(chan struct{}),

		seedTicker:           &time.Ticker{},
		dbidTicker:           &time.Ticker{},
	}

	<-t.GotInfo()

	return t
}

func (t *Torrent) Watch() {
	progressTicker := time.NewTicker(1 * time.Second)
	bufferTicker   := time.NewTicker(1 * time.Second)

	bufferFinished := make(chan struct{}, 1)

	downRates := []int64{0,0,0,0,0}
	upRates   := []int64{0,0,0,0,0}

	rateCounter := 0

	var downloaded   int64
	var uploaded     int64

	var dbidTries    int

	pieceLength := float64(t.Torrent.Info().PieceLength)
	pieceChange := t.Torrent.SubscribePieceStateChanges()

	defer pieceChange.Close()
	defer progressTicker.Stop()
	defer bufferTicker.Stop()
	defer t.seedTicker.Stop()
	defer t.dbidTicker.Stop()
	defer close(bufferFinished)

	for {
		select {
		case _i, ok := <-pieceChange.Values:
			if !ok {
				continue
			}
			i := _i.(gotorrent.PieceStateChange).Index

			t.muBuffer.RLock()

			if _, ok := t.BufferPiecesProgress[i]; !ok {
				t.muBuffer.RUnlock()
				continue
			}
			t.muBuffer.RUnlock()

			t.muBuffer.Lock()
			t.BufferPiecesProgress[i] = float64(t.PieceBytesMissing(i))

			progressCount := 0.0
			for _, v := range t.BufferPiecesProgress {
				progressCount += v
			}

			total := float64(len(t.BufferPiecesProgress)) * pieceLength
			t.BufferProgress = (total - progressCount) / total * 100
			t.muBuffer.Unlock()

			t.muBuffer.RLock()
			if t.BufferProgress >= 100 {
				bufferFinished <- struct{}{}
			}
			t.muBuffer.RUnlock()

		case <- bufferTicker.C:
			t.muBuffer.Lock()

			log.Noticef(strings.Repeat("=", 20))
			for i :=  range t.BufferPiecesProgress {
				if t.PieceState(i).Complete {
					continue
				}

				log.Debugf("Piece: %d, %#v", i, t.PieceState(i))
			}
			log.Noticef(strings.Repeat("=", 20))

			if t.IsBuffering {
				for i := range t.BufferPiecesProgress {
					t.BufferPiecesProgress[i] = float64(t.PieceBytesMissing(i))
				}

				progressCount := 0.0
				for _, v := range t.BufferPiecesProgress {
					progressCount += v
				}

				total := float64(len(t.BufferPiecesProgress)) * pieceLength
				t.BufferProgress = (total - progressCount) / total * 100

				if t.BufferProgress >= 100 {
					bufferFinished <- struct{}{}
				}
			}

			t.muBuffer.Unlock()

		case <- bufferFinished:
			t.muBuffer.Lock()
			log.Debugf("Buffer finished: %#v, %#v", t.IsBuffering,  t.BufferPiecesProgress)

			t.IsBuffering = false

			t.muBuffer.Unlock()

			pieceChange.Close()
			bufferTicker.Stop()
			t.Service.RestoreLimits()

			if t.bufferReader != nil {
				t.bufferReader.Close()
				t.bufferReader = nil
			}
			if t.postReader != nil {
				t.postReader.Close()
				t.postReader = nil
			}

		case <- progressTicker.C:
			// log.Noticef(strings.Repeat("=", 20))
			// for i := 0; i < 10; i++ {
			// 	log.Debugf("Progress Piece: %d, %#v", i, t.PieceState(i))
			// }
			// log.Noticef(strings.Repeat("=", 10))
			// for i := t.Torrent.NumPieces() - 5; i < t.Torrent.NumPieces(); i++ {
			// 	log.Debugf("Progress Piece: %d, %#v", i, t.PieceState(i))
			// }
			// log.Noticef(strings.Repeat("=", 20))

			downRates[rateCounter] = t.Torrent.BytesCompleted() - downloaded
			upRates[rateCounter]   = t.Torrent.Stats().DataBytesWritten - uploaded

			downloaded = t.Torrent.BytesCompleted()
			uploaded   = t.Torrent.Stats().DataBytesWritten

			t.DownloadRate = int64(average(downRates))
			t.UploadRate   = int64(average(upRates))

			rateCounter++
			if rateCounter == len(downRates) - 1 {
				rateCounter = 0
			}

			log.Debugf("ProgressTicker: %s; %#v/%#v; %#v = %#v ", t.Name(), t.DownloadRate, t.UploadRate, t.GetStateString(), t.GetProgress())
			if t.needSeeding && t.Service.GetSeedTime() > 0 && t.GetProgress() >= 100 {
				t.muSeeding.Lock()
				log.Debugf("Starting seeding timer for: %s", t.Info().Name)

				t.IsSeeding = true
				t.needSeeding = false
				t.seedTicker = time.NewTicker(time.Duration(t.Service.GetSeedTime()) * time.Second)

				t.muSeeding.Unlock()
			}

			if t.DBItem == nil {
				t.GetDBItem()
			}

			for i := 0; i < t.Torrent.NumPieces(); i++ {
				state := t.Torrent.PieceState(i)
				if state.Priority == 1 {
					continue
				} else if state.Priority == 0 && state.Complete == false {
					continue
				}

				log.Debugf("Piece with priority: %#v, %#v", i, state)
			}

		case <- t.seedTicker.C:
			log.Debugf("Stopping seeding for: %s", t.Info().Name)
			t.Torrent.SetMaxEstablishedConns(0)
			t.IsSeeding = false
			t.seedTicker.Stop()

		case <- t.dbidTicker.C:
			dbidTries++

			if t.DBID != 0 {
				t.dbidTicker.Stop()
				continue
			}

			playerID := xbmc.PlayerGetActive()
			if playerID == -1 {
				continue
			}

			if item := xbmc.PlayerGetItem(playerID); item != nil {
				t.DBID   = item.Info.Id
				t.DBTYPE = item.Info.Type

				t.dbidTicker.Stop()
			}	else if dbidTries == 10 {
				t.dbidTicker.Stop()
			}

		case <- t.closing:
			return
		}
	}
}

// Define buffer pieces for downloading prior to sending file to Kodi.
// Kodi sends two requests, one for onecoming file read handler,
// another for a piece of file from the end (probably to get codec descriptors and so on)
// We set it as post-buffer and include in required buffer pieces array.
func (t *Torrent) Buffer(file *gotorrent.File) {
	if file == nil {
		return
	}

	pieceLength  := file.Torrent().Info().PieceLength
	bufferPieces := int64(math.Ceil(float64(t.Service.GetBufferSize()) / float64(pieceLength)))

	startPiece, endPiece, _ := t.getFilePiecesAndOffset(file)

	endBufferPiece  := startPiece + bufferPieces - 1
	endBufferLength := bufferPieces * int64(pieceLength)

	postBufferPiece, _ := t.pieceFromOffset(file.Offset() + file.Length() - 3 * 1024 * 1024)
	// postBufferOffset := postBufferPiece * int64(pieceLength)
	postBufferLength := (endPiece - postBufferPiece + 1) * int64(pieceLength)

	t.muBuffer.Lock()
	t.IsBuffering = true
	t.BufferProgress = 0

	for i := startPiece; i <= endBufferPiece; i++ {
		t.BufferPiecesProgress[int(i)]	= 0
	}
	for i := postBufferPiece; i <= endPiece; i++ {
		t.BufferPiecesProgress[int(i)]	= 0
	}

	t.muBuffer.Unlock()

	log.Debugf("Setting buffer for file: %s. Pieces: %#v-%#v + %#v-%#v, Length: %#v / %#v, Offset: %#v ", file.DisplayPath(), startPiece, endBufferPiece, postBufferPiece, endPiece, pieceLength, endBufferLength, file.Offset())

	t.Service.SetBufferingLimits()

	t.bufferReader = t.NewReader(file)
	t.bufferReader.SetReadahead(endBufferLength)
	t.bufferReader.Seek(file.Offset(), os.SEEK_SET)

	log.Debugf("POSTBUF: %#v -- %#v -- %#v", postBufferPiece, postBufferLength, endPiece)
	t.postReader = t.NewReader(file)
	t.postReader.SetReadahead(postBufferLength)
	t.postReader.Seek(file.Offset() + file.Length() - postBufferLength, os.SEEK_SET)
}

func (t *Torrent) pieceFromOffset(offset int64) (int64, int64) {
	pieceLength := int64(t.Info().PieceLength)
	piece := offset / pieceLength
	pieceOffset := offset % pieceLength
	return piece, pieceOffset
}

func (t *Torrent) getFilePiecesAndOffset(f *gotorrent.File) (int64, int64, int64) {
	startPiece, offset := t.pieceFromOffset(f.Offset())
	endPiece, _ := t.pieceFromOffset(f.Offset() + f.Length())
	return startPiece, endPiece, offset
}

func (t *Torrent) GetState() int {
	// log.Debugf("Status: %#v, %#v, %#v, %#v ", t.IsBuffering, t.BytesCompleted(), t.BytesMissing(), t.Stats())

	if t.IsBuffering {
		return STATUS_BUFFERING
	}

	havePartial := false
	// log.Debugf("States: %#v", t.PieceStateRuns())
	for _, state := range t.PieceStateRuns() {
		if state.Length == 0 {
			continue
		}

		if state.Checking == true {
			return STATUS_CHECKING
		} else if state.Partial == true {
			havePartial = true
		}
	}

	progress := t.GetProgress()
	if progress == 0 {
		return STATUS_QUEUED
	}	else if progress < 100 {
		if havePartial {
			return STATUS_DOWNLOADING
		} else if t.BytesCompleted() == 0 {
			return STATUS_QUEUED
		}
	} else {
		if t.IsSeeding {
			return STATUS_SEEDING
		} else {
			return STATUS_FINISHED
		}
	}

	return STATUS_QUEUED
}

func (t *Torrent) GetStateString() string {
	return StatusStrings[ t.GetState() ]
}

func (t *Torrent) GetBufferProgress() float64 {
	progress := t.BufferProgress
	state    := t.GetState()

	if state == STATUS_CHECKING {
		total 		:= 0
		checking 	:= 0

		for _, state := range t.PieceStateRuns() {
			if state.Length == 0 {
				continue
			}

			total += state.Length
			if state.Checking == true {
				checking += state.Length
			}
		}

		log.Debugf("Buffer status checking: %#v -- %#v, == %#v", checking, total, progress)
		if total > 0 {
			progress = float64(total - checking) / float64(total) * 100
		}
	}

	if progress > 100 {
		progress = 100
	}

	return progress
}

func (t *Torrent) GetProgress() float64 {
	if t == nil {
		return 0
	}

	var total int64
	for _, f := range t.ChosenFiles {
		total += f.Length()
	}

	if total == 0 {
		return 0
	}

	progress := float64(t.BytesCompleted()) / float64(total) * 100.0
	if progress > 100 {
		progress = 100
	}

	return progress
}

func (t *Torrent) DownloadFile(f *gotorrent.File) {
	t.ChosenFiles = append(t.ChosenFiles, f)
	log.Debugf("Choosing file for download: %s", f.DisplayPath())
	log.Debugf("Offset: %#v", f.Offset())
	if t.Service.config.DownloadStorage != 1 {
		f.Download()
	}
}

func (t *Torrent) InfoHash() string {
	return t.Torrent.InfoHash().HexString()
}

func (t *Torrent) Name() string {
	return t.Torrent.Name()
}

func (t *Torrent) Drop(removeFiles bool) {
	log.Infof("Dropping torrent: %s", t.Name())

	files := []string{}
	for _, f := range t.Torrent.Files() {
		files = append(files, f.Path())
	}

	t.closing <- struct{}{}
	t.Torrent.Drop()

	if removeFiles {
		for _, f := range files {
			path := filepath.Join(t.Service.ClientConfig.DataDir, f)
			if _, err := os.Stat(path); err == nil {
				log.Infof("Deleting torrent file at %s", path)
				defer os.Remove(path)
			}
		}
	}
}

func (t *Torrent) Pause() {
	t.Torrent.SetMaxEstablishedConns(0)
	t.IsPaused = true
}

func (t *Torrent) Resume() {
	t.Torrent.SetMaxEstablishedConns(1000)
	t.IsPaused = false
}

func (t *Torrent) GetDBID() {
	if t.DBID == 0 && t.needDBID == true {
		log.Debugf("Getting DBID for torrent: %s", t.Name())
		t.needDBID = false
		t.dbidTicker = time.NewTicker(10 * time.Second)
	}
}

func (t *Torrent) GetDBItem() {
	t.DBItem = t.Service.GetDBItem(t.InfoHash())
}

func (t *Torrent) GetPlayingItem() *PlayingItem {
	if t.DBItem == nil {
		return nil
	}

	TMDBID := t.DBItem.ID
	if t.DBItem.Type != "movie" {
		TMDBID = t.DBItem.ShowID
	}

	return &PlayingItem{
		DBID:         t.DBID,
		DBTYPE:       t.DBTYPE,

		TMDBID:       TMDBID,
		Season:       t.DBItem.Season,
		Episode:      t.DBItem.Episode,

		WatchedTime:  WatchedTime,
		Duration:     VideoDuration,
	}
}

func (t *Torrent) CurrentPos(pos int64, f *gotorrent.File) {
	log.Debugf("CurrentPos: %#v, %#v, %#v", pos, t.IsBuffering, t.IsPlaying)
	if t.IsBuffering == false && t.IsPlaying == true {
		t.Service.StorageChanges.Publish(qstorage.StorageChange{
			InfoHash: t.InfoHash(),
			Pos: pos,
			FileLength: f.Length(),
			FileOffset: f.Offset(),
		})
	}
}

func average(xs[]int64) float64 {
	var total int64
	for _, v := range xs {
		total += v
	}
	return float64(total) / float64(len(xs))
}

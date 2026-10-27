package bittorrent

import (
	"cmp"
	"errors"
	"fmt"
	"io"
	"maps"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	lt "github.com/ElementumOrg/libtorrent-go"
	"github.com/RoaringBitmap/roaring/v2"
	"github.com/anacrolix/missinggo/perf"
	"github.com/anacrolix/sync"
	"github.com/dustin/go-humanize"
	"github.com/fatih/color"
	"github.com/valyala/bytebufferpool"
	"github.com/zeebo/bencode"

	"github.com/elgatito/elementum/config"
	"github.com/elgatito/elementum/database"
	"github.com/elgatito/elementum/tmdb"
	"github.com/elgatito/elementum/tvdb"
	"github.com/elgatito/elementum/util"
	"github.com/elgatito/elementum/util/event"
	"github.com/elgatito/elementum/xbmc"
)

// Torrent ...
type Torrent struct {
	files             []*File
	th                lt.TorrentHandle
	ti                lt.TorrentInfo
	status            atomic.Pointer[TorrentStatus]
	ms                lt.MemoryStorage
	fastResumeFile    string
	torrentFile       string
	partsFile         string
	memoryStorageFile string
	fileStorageFile   string
	addedTime         time.Time
	DownloadStorage   int

	title              string
	name               string
	infoHash           string
	readers            map[int64]*TorrentFSEntry
	reservedPieces     []int
	lastPrioritization string
	trackers           sync.Map

	awaitingPieces   *roaring.Bitmap
	demandPieces     *roaring.Bitmap
	muAwaitingPieces *sync.RWMutex
	muDemandPieces   *sync.RWMutex

	ChosenFiles []*File

	Service *Service

	BufferLength           int64
	BufferProgress         float64
	BufferProgressPrevious float64
	BufferPiecesLength     int64
	BufferPiecesProgress   map[int]float64
	MemorySize             int64

	IsMoveInProgress         bool
	IsMarkedToMove           bool
	IsPlaying                bool
	IsPaused                 bool
	IsBuffering              bool
	IsBufferingFinished      bool
	IsSeeding                bool
	IsRarArchive             bool
	IsNextFile               bool
	IsNeedFinishNotification bool
	HasNextFile              bool
	PlayerAttached           int

	DBItem *database.BTItem

	mu        *sync.Mutex
	muBuffer  *sync.RWMutex
	muReaders *sync.Mutex
	muStatus  *sync.Mutex
	muHandle  *sync.RWMutex

	pieceLength int64
	pieceCount  int

	gotMetainfo    event.Event
	Closer         event.Event
	bufferFinished chan struct{}

	bufferTicker     *time.Ticker
	prioritizeTicker *time.Ticker

	nextTimer *time.Timer
}

// NewTorrent ...
func NewTorrent(service *Service, handle lt.TorrentHandle, info lt.TorrentInfo, path string, downloadStorage int) *Torrent {
	log.Infof("Adding torrent with storage: %s", config.Storages[downloadStorage])

	ts := handle.Status()
	defer lt.DeleteTorrentStatus(ts)

	status := newTorrentStatus(ts)

	t := &Torrent{
		infoHash: status.InfoHash,

		Service:         service,
		files:           []*File{},
		th:              handle,
		ti:              info,
		torrentFile:     path,
		DownloadStorage: downloadStorage,

		readers:        map[int64]*TorrentFSEntry{},
		reservedPieces: []int{},

		awaitingPieces: roaring.NewBitmap(),
		demandPieces:   roaring.NewBitmap(),

		BufferPiecesProgress: map[int]float64{},
		BufferProgress:       -1,

		mu:               &sync.Mutex{},
		muBuffer:         &sync.RWMutex{},
		muReaders:        &sync.Mutex{},
		muAwaitingPieces: &sync.RWMutex{},
		muDemandPieces:   &sync.RWMutex{},
		muStatus:         &sync.Mutex{},
		muHandle:         &sync.RWMutex{},
	}
	t.status.Store(status)

	return t
}

func (t *Torrent) init() {
	// Run setters in MemoryStorage
	if t.IsMemoryStorage() {
		t.withHandle(func(th lt.TorrentHandle) {
			t.ms = th.GetMemoryStorage()
			t.ms.SetTorrentHandle(th)
		})

		if t.MemorySize < t.pieceLength*10 {
			t.AdjustMemorySize(t.pieceLength * 10)
		}
	}
}

// GotInfo ...
func (t *Torrent) GotInfo() <-chan struct{} {
	return t.gotMetainfo.C()
}

// Storage ...
func (t *Torrent) Storage() (storage lt.StorageInterface) {
	t.withHandle(func(th lt.TorrentHandle) {
		storage = th.GetStorageImpl()
	})
	return
}

// Watch ...
func (t *Torrent) Watch() {
	log.Debug("Starting watch events")

	t.startBufferTicker()
	t.bufferFinished = make(chan struct{}, 5)

	t.prioritizeTicker = time.NewTicker(1 * time.Second)
	t.nextTimer = time.NewTimer(0)
	updateMetadataTicker := time.NewTicker(5 * time.Minute)

	sc := t.Service.Closer.C()
	tc := t.Closer.C()

	defer func() {
		log.Debugf("Closing torrent tickers")

		t.bufferTicker.Stop()
		t.prioritizeTicker.Stop()
		t.nextTimer.Stop()
		updateMetadataTicker.Stop()
		close(t.bufferFinished)
	}()

	for {
		select {
		case <-tc:
			log.Debug("Stopping watch events")
			return

		case <-sc:
			t.Closer.Set()
			return

		case <-t.bufferTicker.C:
			go t.bufferTickerEvent()

		case <-t.bufferFinished:
			go t.bufferFinishedEvent()

		case <-t.prioritizeTicker.C:
			go t.PrioritizePieces()

		case <-t.nextTimer.C:
			if t.IsNextFile {
				t.IsNextFile = false
				go t.Service.RemoveTorrent(nil, t, RemoveOptions{})
			}

		case <-updateMetadataTicker.C:
			if t.IsPlaying && !t.IsSeeding {
				go t.UpdateTorrentMetadata()
			}
		}
	}
}

func (t *Torrent) startNextTimer() {
	t.IsNextFile = true

	t.nextTimer.Reset(15 * time.Minute)
	t.muDemandPieces.Lock()
	t.demandPieces.Clear()
	t.muDemandPieces.Unlock()
}

func (t *Torrent) stopNextTimer() {
	t.IsNextFile = false

	if t.nextTimer != nil {
		t.nextTimer.Stop()
	}
}

func (t *Torrent) startBufferTicker() {
	t.bufferTicker = time.NewTicker(1 * time.Second)
}

func (t *Torrent) bufferTickerEvent() {
	if t.Closer.IsSet() {
		return
	}

	defer perf.ScopeTimer()()

	if t.IsBuffering && len(t.BufferPiecesProgress) > 0 {
		// Making sure current progress is not less then previous
		thisProgress := t.GetBufferProgress()

		t.muBuffer.Lock()
		defer t.muBuffer.Unlock()

		piecesStatus := bytebufferpool.Get()
		defer bytebufferpool.Put(piecesStatus)

		piecesStatus.WriteString("[")
		piecesKeys := slices.Sorted(maps.Keys(t.BufferPiecesProgress))

		for _, k := range piecesKeys {
			if piecesStatus.Len() > 1 {
				piecesStatus.WriteString(", ")
			}

			piecesStatus.WriteString(fmt.Sprintf("%d:%d", k, int(t.BufferPiecesProgress[k]*100)))
		}

		if piecesStatus.Len() > 1 {
			piecesStatus.WriteString("]")
		}

		seeds, seedsTotal, peers, peersTotal := t.GetConnections()
		downSpeed, upSpeed := t.GetHumanizedSpeeds()
		log.Infof("Buffer. Pr: %d%%, Sp: %s / %s, Con: %d/%d + %d/%d, Pi: %s", int(thisProgress), downSpeed, upSpeed, seeds, seedsTotal, peers, peersTotal, piecesStatus.String())

		if thisProgress > t.BufferProgress {
			t.BufferProgress = thisProgress
		}

		if t.BufferProgress >= 100 {
			t.bufferFinished <- struct{}{}
		} else {
			t.IsBufferingFinished = false
			if t.BufferProgressPrevious > t.BufferProgress {
				t.BufferProgress = t.BufferProgressPrevious
			} else {
				t.BufferProgressPrevious = t.BufferProgress
			}
		}
	}
}

// GetConnections returns connected and overall number of peers
func (t *Torrent) GetConnections() (int, int, int, int) {
	if t.Closer.IsSet() {
		return 0, 0, 0, 0
	}

	ts := t.GetStatus()

	seeds := ts.NumSeeds
	seedsTotal := max(ts.ListSeeds, ts.NumComplete)

	peers := ts.NumPeers - ts.NumSeeds
	peersTotal := max(ts.ListPeers-ts.ListSeeds, ts.NumComplete+ts.NumIncomplete)

	return seeds, seedsTotal, peers, peersTotal
}

// GetSpeeds returns download and upload speeds
func (t *Torrent) GetSpeeds() (down, up int) {
	ts := t.GetStatus()
	return ts.DownloadPayloadRate, ts.UploadPayloadRate
}

// GetHumanizedSpeeds returns humanize download and upload speeds
func (t *Torrent) GetHumanizedSpeeds() (down, up string) {
	downInt, upInt := t.GetSpeeds()
	return humanize.Bytes(uint64(downInt)), humanize.Bytes(uint64(upInt))
}

func (t *Torrent) bufferFinishedEvent() {
	t.muBuffer.Lock()
	log.Infof("Buffer finished: %#v, %#v", t.IsBuffering, t.BufferPiecesProgress)

	t.BufferPiecesProgress = map[int]float64{}
	t.IsBuffering = false
	t.IsBufferingFinished = true

	t.muBuffer.Unlock()

	t.bufferTicker.Stop()
	t.Service.RestoreLimits()
}

// Buffer defines buffer pieces for downloading prior to sending file to Kodi.
// Kodi sends two requests, one for onecoming file read handler,
// another for a piece of file from the end (probably to get codec descriptors and so on)
// We set it as post-buffer and include in required buffer pieces array.
func (t *Torrent) Buffer(file *File, isStartup bool) {
	if file == nil || t.Closer.IsSet() {
		t.bufferFinishedEvent()
		return
	}

	defer perf.ScopeTimer()()

	t.startBufferTicker()

	startBufferSize := t.Service.GetBufferSize()
	preBufferStart, preBufferEnd, preBufferOffset, preBufferSize := t.getBufferSize(file.Offset, 0, startBufferSize)
	postBufferStart, postBufferEnd, postBufferOffset, postBufferSize := t.getBufferSize(file.Offset, file.Size-int64(config.Get().EndBufferSize), int64(config.Get().EndBufferSize))

	// TODO: Remove this piece of buffer adjustment?
	// if config.Get().AutoAdjustBufferSize && preBufferEnd-preBufferStart < 10 {
	// 	_, free := t.Service.GetMemoryStats()
	// 	autodetectStart := 10
	// 	// If this file is only a part of big torrent with big pieces -
	// 	// we don't need that much to buffer. As it will take a lot of time.
	// 	if t.pieceLength > 0 && file.Size/t.pieceLength <= 300 {
	// 		autodetectStart = 6
	// 	}

	// 	// Let's try to
	// 	var newBufferSize int64
	// 	for i := autodetectStart; i >= 4; i -= 2 {
	// 		mem := int64(i) * t.pieceLength

	// 		if mem < startBufferSize {
	// 			break
	// 		}
	// 		if free == 0 || !t.Service.IsMemoryStorage() || (mem*2)-startBufferSize < free {
	// 			newBufferSize = mem
	// 			break
	// 		}
	// 	}

	// 	if newBufferSize > 0 {
	// 		startBufferSize = newBufferSize
	// 		preBufferStart, preBufferEnd, preBufferOffset, preBufferSize = t.getBufferSize(file.Offset, 0, startBufferSize)
	// 		log.Infof("Adjusting buffer size to %s, to have at least %d pieces ready!", humanize.Bytes(uint64(startBufferSize)), preBufferEnd-preBufferStart+1)
	// 	}
	// }

	if t.IsMemoryStorage() {
		// Try to increase memory size to at most 25 pieces to have more comfortable playback.
		// Also check for free memory to avoid spending too much!
		if config.Get().AutoAdjustMemorySize {
			_, free := t.Service.GetMemoryStats()

			var newMemorySize int64
			for i := 25; i >= 10; i -= 5 {
				mem := int64(i) * t.pieceLength

				if mem < t.MemorySize || free == 0 {
					break
				}
				if (mem-t.MemorySize)*2 < free {
					newMemorySize = mem
					break
				}
			}

			if newMemorySize > 0 {
				t.AdjustMemorySize(newMemorySize)
			}
		}

		// Increase memory size if buffer does not fit there
		if preBufferSize+postBufferSize > t.MemorySize {
			t.MemorySize = preBufferSize + postBufferSize + (1 * t.pieceLength)
			log.Infof("Adjusting memory size to %s, to fit all buffer!", humanize.Bytes(uint64(t.MemorySize)))
			t.withHandle(func(lt.TorrentHandle) {
				t.ms.SetMemorySize(t.MemorySize)
			})
		}
	}

	t.muBuffer.Lock()
	t.IsBuffering = true
	t.IsBufferingFinished = false
	t.BufferProgress = 0
	t.BufferProgressPrevious = 0
	t.BufferLength = preBufferSize + postBufferSize

	for i := preBufferStart; i <= preBufferEnd; i++ {
		t.BufferPiecesProgress[i] = 0
	}
	for i := postBufferStart; i <= postBufferEnd; i++ {
		t.BufferPiecesProgress[i] = 0
	}

	t.BufferPiecesLength = 0
	for range t.BufferPiecesProgress {
		t.BufferPiecesLength += t.pieceLength
	}

	t.muBuffer.Unlock()

	log.Infof("Setting buffer for file: %s (%s / %s). Desired: %s. Pieces: %#v-%#v + %#v-%#v, PieceLength: %s, Pre: %s, Post: %s, WithOffset: %#v / %#v (%#v)",
		file.Path, humanize.Bytes(uint64(file.Size)), humanize.Bytes(uint64(t.ti.TotalSize())),
		humanize.Bytes(uint64(t.Service.GetBufferSize())),
		preBufferStart, preBufferEnd, postBufferStart, postBufferEnd,
		humanize.Bytes(uint64(t.pieceLength)), humanize.Bytes(uint64(preBufferSize)), humanize.Bytes(uint64(postBufferSize)),
		preBufferOffset, postBufferOffset, file.Offset)

	t.Service.SetBufferingLimits()

	t.muBuffer.Lock()
	defer t.muBuffer.Unlock()

	if t.Closer.IsSet() {
		return
	}

	// Properly set the pieces priority vector
	curPiece := 0

	t.muDemandPieces.Lock()
	t.withHandle(func(th lt.TorrentHandle) {
		if isStartup {
			piecesPriorities := th.PiecePriorities()
			defer lt.DeleteStdVectorInt(piecesPriorities)

			for curPiece = preBufferStart; curPiece <= preBufferEnd; curPiece++ { // get this part
				piecesPriorities.Set(curPiece, 7)
			}
			for curPiece = postBufferStart; curPiece <= postBufferEnd; curPiece++ { // get this part
				piecesPriorities.Set(curPiece, 7)
			}
			th.PrioritizePieces(piecesPriorities)
		} else {
			for curPiece = preBufferStart; curPiece <= preBufferEnd; curPiece++ { // get this part
				t.demandPieces.AddInt(curPiece)
				th.PiecePriority(curPiece, 3)
			}
			for curPiece = postBufferStart; curPiece <= postBufferEnd; curPiece++ { // get this part
				t.demandPieces.AddInt(curPiece)
				th.PiecePriority(curPiece, 3)
			}
		}
	})
	t.muDemandPieces.Unlock()

	// Using libtorrent hack to pause and resume the torrent
	if config.Get().UseLibtorrentPauseResume && isStartup {
		t.Pause()
		t.Resume()
	}

	t.withHandle(func(th lt.TorrentHandle) {
		// Force reannounce for trackers
		th.ForceReannounce()
		if !config.Get().DisableDHT {
			th.ForceDhtAnnounce()
		}

		// As long as file storage has many enabled pieces, we make sure buffer pieces are sent immediately
		if !t.IsMemoryStorage() {
			for curPiece = preBufferStart; curPiece <= preBufferEnd; curPiece++ { // get this part
				th.SetPieceDeadline(curPiece, 0, 0)
			}
			for curPiece = postBufferStart; curPiece <= postBufferEnd; curPiece++ { // get this part
				th.SetPieceDeadline(curPiece, 0, 0)
			}
		}
	})
}

// AdjustMemorySize ...
func (t *Torrent) AdjustMemorySize(ms int64) {
	if t.ms == nil || t.ms.Swigcptr() == 0 {
		return
	}

	defer perf.ScopeTimer()()

	t.MemorySize = ms
	log.Infof("Adjusting memory size to %s!", humanize.Bytes(uint64(t.MemorySize)))
	t.withHandle(func(lt.TorrentHandle) {
		t.ms.SetMemorySize(t.MemorySize)
	})
}

func (t *Torrent) getBufferSize(fileOffset int64, off, length int64) (startPiece, endPiece int, offset, size int64) {
	if off < 0 {
		off = 0
	}

	offsetStart := fileOffset + off
	startPiece = int(offsetStart / t.pieceLength)
	pieceOffset := offsetStart % t.pieceLength
	offset = offsetStart - pieceOffset

	offsetEnd := offsetStart + length
	pieceOffsetEnd := offsetEnd % t.pieceLength
	endPiece = int(math.Ceil(float64(offsetEnd) / float64(t.pieceLength)))

	if pieceOffsetEnd == 0 {
		endPiece--
	}
	if endPiece >= t.pieceCount {
		endPiece = t.pieceCount - 1
	}

	size = int64(endPiece-startPiece+1) * t.pieceLength

	// Calculated offset is more than we have in torrent, so correcting the size
	if !t.Closer.IsSet() && t.ti.TotalSize() != 0 && offset+size >= t.ti.TotalSize() {
		size = t.ti.TotalSize() - offset
	}

	offset -= fileOffset
	if offset < 0 {
		offset = 0
	}
	return
}

// PrioritizePiece ...
func (t *Torrent) PrioritizePiece(piece int) {
	t.muAwaitingPieces.Lock()
	defer t.muAwaitingPieces.Unlock()

	if t.IsBuffering || t.th == nil || t.Closer.IsSet() || t.awaitingPieces.ContainsInt(piece) {
		return
	}

	defer perf.ScopeTimer()()

	for i := piece; i < piece+3; i++ {
		if t.awaitingPieces.ContainsInt(i) || t.hasPiece(i) {
			continue
		}

		t.awaitingPieces.AddInt(i)

		t.withHandle(func(th lt.TorrentHandle) {
			th.SetPieceDeadline(i, i-piece*100, 0)
		})
	}
}

// ClearDeadlines ...
func (t *Torrent) ClearDeadlines() {
	t.muAwaitingPieces.Lock()
	t.awaitingPieces.Clear()
	t.muAwaitingPieces.Unlock()

	t.withHandle(func(th lt.TorrentHandle) {
		th.ClearPieceDeadlines()
	})
}

// PrioritizePieces ...
func (t *Torrent) PrioritizePieces() {
	t.muDemandPieces.RLock()
	defer t.muDemandPieces.RUnlock()

	if (t.IsBuffering && t.demandPieces.IsEmpty()) || t.IsSeeding || (!t.IsPlaying && !t.IsNextFile) || t.th == nil || t.Closer.IsSet() {
		return
	}

	defer perf.ScopeTimer()()

	downSpeed, upSpeed := t.GetHumanizedSpeeds()
	seeds, seedsTotal, peers, peersTotal := t.GetConnections()
	log.Debugf("Prioritizing pieces: %v%% / %s / %s, Con: %d/%d + %d/%d", int(t.GetProgress()), downSpeed, upSpeed, seeds, seedsTotal, peers, peersTotal)

	t.muReaders.Lock()

	numPieces := t.ti.NumPieces()

	priorities := [][]int{
		{},
		{},
		{},
		{},
		{},
		{},
		{},
		{},
	}
	readerProgress := map[int]float64{}
	readerPieces := make([]int, numPieces)

	i := t.demandPieces.Iterator()
	for i.HasNext() {
		idx := int(i.Next())
		readerPieces[idx] = 3
		readerProgress[idx] = 0
	}

	t.muAwaitingPieces.RLock()
	for _, r := range t.readers {
		pr := r.ReaderPiecesRange()
		log.Debugf("Reader range: %+v, last: %s", pr, r.lastUsed.Format(time.RFC3339))

		for curPiece := pr.Begin; curPiece <= pr.End; curPiece++ {
			if t.awaitingPieces.ContainsInt(curPiece) {
				readerPieces[curPiece] = 7
			} else {
				pos := curPiece - pr.Begin
				switch {
				case pos <= 0:
					readerPieces[curPiece] = 6
				case pos <= 2:
					readerPieces[curPiece] = 5
				case pos <= 5:
					readerPieces[curPiece] = 4
				case pos <= 9:
					readerPieces[curPiece] = 3
				default:
					readerPieces[curPiece] = 2
				}
			}
			priorities[readerPieces[curPiece]] = append(priorities[readerPieces[curPiece]], curPiece)

			readerProgress[curPiece] = 0
		}
	}
	t.muAwaitingPieces.RUnlock()
	t.muReaders.Unlock()

	// Update progress for piece completion
	t.piecesProgress(readerProgress)

	piecesPriorities := lt.NewStdVectorInt()
	defer lt.DeleteStdVectorInt(piecesPriorities)

	readerVector := lt.NewStdVectorInt()
	defer lt.DeleteStdVectorInt(readerVector)

	reservedVector := lt.NewStdVectorInt()
	defer lt.DeleteStdVectorInt(reservedVector)

	piecesStatus := bytebufferpool.Get()
	defer bytebufferpool.Put(piecesStatus)

	piecesStatus.WriteString("[")
	piecesKeys := []int{}
	for k, p := range readerPieces {
		if p > 0 {
			piecesKeys = append(piecesKeys, k)
		}
	}
	slices.Sort(piecesKeys)

	for i, k := range piecesKeys {
		readerVector.Add(k)

		// Should print only prioritized pieces
		if readerPieces[k] > 1 {
			progress := int(readerProgress[k] * 100)
			comma := ""
			if i > 1 && piecesKeys[i] > piecesKeys[i-1]+1 {
				piecesStatus.WriteString("]\n[")
			} else if piecesStatus.Len() > 1 {
				comma = ", "
			}

			if progress >= 100 {
				piecesStatus.WriteString(color.GreenString("%s%d:%d:%d", comma, k, readerPieces[k], progress))
			} else if progress > 0 {
				piecesStatus.WriteString(color.YellowString("%s%d:%d:%d", comma, k, readerPieces[k], progress))
			} else {
				piecesStatus.WriteString(color.RedString("%s%d:%d:%d", comma, k, readerPieces[k], progress))
			}
		}
	}

	if piecesStatus.Len() > 1 {
		piecesStatus.WriteString("]")
		log.Debugf("Priorities: %s", piecesStatus.String())
	}

	for _, piece := range t.reservedPieces {
		reservedVector.Add(piece)
	}

	if t.Closer.IsSet() {
		return
	}

	if t.IsMemoryStorage() && t.ms != nil {
		t.withHandle(func(lt.TorrentHandle) {
			t.ms.UpdateReaderPieces(readerVector)
			t.ms.UpdateReservedPieces(reservedVector)
		})
	}

	defaultPriority := 0
	if !t.IsMemoryStorage() {
		for _, f := range t.ChosenFiles {
			for i := f.PieceStart; i <= f.PieceEnd; i++ {
				if len(readerPieces) > i && readerPieces[i] == 0 {
					readerPieces[i] = 1
				}
			}
		}
	}

	minPiece, minPriority, maxPiece, maxPriority, countPiece := -1, 0, -1, 0, 0

	// Splitting to first set priorities, then deadlines,
	// so that latest set deadline is the nearest piece number
	for curPiece := range numPieces {
		if priority := readerPieces[curPiece]; priority > 0 {
			readerVector.Add(curPiece)
			piecesPriorities.Add(priority)

			if curPiece < minPiece || minPiece == -1 {
				minPiece = curPiece
				minPriority = priority
			}
			if curPiece > maxPiece {
				maxPiece = curPiece
				maxPriority = priority
			}
			countPiece++
		} else {
			piecesPriorities.Add(defaultPriority)
		}
	}

	status := fmt.Sprintf("%d:%d;%d:%d;%d", minPiece, minPriority, maxPiece, maxPriority, countPiece)
	if status == t.lastPrioritization {
		log.Debugf("Skipping prioritization due to stale priorities")
		return
	} else if t.Closer.IsSet() {
		return
	}

	t.lastPrioritization = status
	t.withHandle(func(th lt.TorrentHandle) {
		th.PrioritizePieces(piecesPriorities)
	})
}

// GetAddedTime ...
func (t *Torrent) GetAddedTime() time.Time {
	return t.addedTime
}

// GetState ...
func (t *Torrent) GetState() int {
	return t.GetStatus().State
}

// GetSmartState ...
func (t *Torrent) GetSmartState() int {
	defer perf.ScopeTimer()()

	if t.th == nil || t.th.Swigcptr() == 0 {
		return StatusQueued
	}

	torrentStatus := t.GetStatus()

	progress := torrentStatus.Progress * 100
	state := torrentStatus.State

	if t.Service.Session.IsPaused() {
		return StatusPaused
	} else if torrentStatus.Paused && state != StatusFinished && state != StatusFinding {
		if progress == 100 {
			return StatusFinished
		}

		return StatusPaused
	} else if !torrentStatus.Paused && (state == StatusFinished || progress == 100) {
		if progress < 100 {
			return StatusDownloading
		}
	} else if state != StatusQueued && t.IsBuffering {
		return StatusBuffering
	}
	if t.IsBuffering {
		return StatusBuffering
	}

	return state
}

// GetBufferProgress ...
func (t *Torrent) GetBufferProgress() float64 {
	defer perf.ScopeTimer()()

	t.muBuffer.Lock()
	defer t.muBuffer.Unlock()

	if len(t.BufferPiecesProgress) > 0 {
		totalProgress := float64(0)
		t.piecesProgress(t.BufferPiecesProgress)
		for _, v := range t.BufferPiecesProgress {
			totalProgress += v
		}
		t.BufferProgress = 100 * totalProgress / float64(len(t.BufferPiecesProgress))
	}

	if t.BufferProgress > 100 {
		return 100
	}

	return t.BufferProgress
}

func (t *Torrent) piecesProgress(pieces map[int]float64) {
	if t.Closer.IsSet() || t.th == nil || t.th.Swigcptr() == 0 {
		return
	}

	defer perf.ScopeTimer()()

	completed := 0
	t.muAwaitingPieces.Lock()
	for piece := range pieces {
		if t.hasPiece(piece) {
			completed++
			pieces[piece] = 1.0

			if t.awaitingPieces.ContainsInt(piece) {
				t.awaitingPieces.Remove(uint32(piece))
			}
		}
	}
	t.muAwaitingPieces.Unlock()

	if completed == len(pieces) {
		return
	}

	queue := lt.NewStdVectorPartialPieceInfo()
	defer lt.DeleteStdVectorPartialPieceInfo(queue)

	if !t.withHandle(func(th lt.TorrentHandle) { th.GetDownloadQueue(queue) }) {
		return
	}

	blockSize := t.GetStatus().BlockSize
	queueSize := queue.Size()
	for i := 0; i < int(queueSize); i++ {
		ppi := queue.Get(i)
		pieceIndex := ppi.GetPieceIndex()
		if v, exists := pieces[pieceIndex]; exists && v != 1.0 {
			// We don't count parts of Blocks, since they are 16Kb usually,
			// 	just to avoid having memory leaks
			totalBlockDownloaded := ppi.GetFinished() * blockSize
			totalBlockSize := ppi.GetBlocksInPiece() * blockSize
			pieces[pieceIndex] = float64(totalBlockDownloaded) / float64(totalBlockSize)

			// ppi.GetBlockSize()
			// blocks := ppi.Blocks()
			// totalBlocks := ppi.GetBlocksInPiece()
			// totalBlockDownloaded := uint(0)
			// totalBlockSize := uint(0)
			// for j := 0; j < totalBlocks; j++ {
			// 	block := blocks.Getitem(j)
			// 	totalBlockDownloaded += block.GetBytesProgress()
			// 	totalBlockSize += block.GetBlockSize()
			// }
			// lt.DeleteBlockInfoList(blocks)
			// pieces[pieceIndex] = float64(totalBlockDownloaded) / float64(totalBlockSize)
		}
	}
}

// GetProgress ...
func (t *Torrent) GetProgress() float64 {
	if t == nil || t.Closer.IsSet() {
		return 0
	}

	defer perf.ScopeTimer()()

	return t.GetRealProgress()
}

// GetRealProgress returns progress of downloading in libtorrent.
// Should be taken in mind that for memory storage it's a progress of downloading currently active pieces,
// not the whole torrent.
func (t *Torrent) GetRealProgress() float64 {
	return t.GetStatus().Progress * 100
}

// DownloadFiles sets priority 1 to list of files
func (t *Torrent) DownloadFiles(files []*File) {
	t.withHandle(func(th lt.TorrentHandle) {
		filePriorities := th.FilePriorities()
		defer lt.DeleteStdVectorInt(filePriorities)

		for _, f := range files {
			log.Debugf("Choosing file for download: %s", f.Path)

			f.Selected = true
			filePriorities.Set(f.Index, 1)
		}

		th.PrioritizeFiles(filePriorities)
	})
}

// UndownloadFiles sets priority 0 to list of files
func (t *Torrent) UndownloadFiles(files []*File) {
	t.withHandle(func(th lt.TorrentHandle) {
		filePriorities := th.FilePriorities()
		defer lt.DeleteStdVectorInt(filePriorities)

		for _, f := range files {
			log.Debugf("UnChoosing file for download: %s", f.Path)

			f.Selected = false
			filePriorities.Set(f.Index, 0)
		}

		th.PrioritizeFiles(filePriorities)
	})
}

// DownloadAllFiles ...
func (t *Torrent) DownloadAllFiles() {
	t.DownloadFiles(t.files)
}

// UnDownloadAllFiles ...
func (t *Torrent) UnDownloadAllFiles() {
	t.UndownloadFiles(t.files)
}

// SyncSelectedFiles iterates through torrent files and choosing selected files
func (t *Torrent) SyncSelectedFiles() []string {
	t.ChosenFiles = []*File{}
	selected := []string{}
	for _, f := range t.files {
		if f.Selected {
			selected = append(selected, f.Path)
			t.ChosenFiles = append(t.ChosenFiles, f)
		}
	}

	return selected
}

// SaveDBFiles ...
func (t *Torrent) SaveDBFiles() {
	selected := t.SyncSelectedFiles()

	database.GetStorm().UpdateBTItemFiles(t.infoHash, selected)
	t.FetchDBItem()
}

// DownloadFileWithPriority ...
func (t *Torrent) DownloadFileWithPriority(addFile *File, priority int) {
	addFile.Selected = true

	idx := -1
	for i, f := range t.ChosenFiles {
		if f.Index == addFile.Index {
			idx = i
			break
		}
	}

	if idx == -1 {
		t.ChosenFiles = append(t.ChosenFiles, addFile)
	}

	if t.IsMemoryStorage() {
		return
	}

	for _, f := range t.files {
		if f != addFile {
			continue
		}

		log.Debugf("Choosing file for download with priority %d: %s", priority, f.Path)
		t.withHandle(func(th lt.TorrentHandle) {
			th.FilePriority(f.Index, priority)
		})

		// Need to sleep because file_priority is executed async
		time.Sleep(50 * time.Millisecond)
	}
}

// DownloadFile ...
func (t *Torrent) DownloadFile(addFile *File) {
	t.DownloadFileWithPriority(addFile, 1)
}

// UnDownloadFile ...
func (t *Torrent) UnDownloadFile(addFile *File) bool {
	addFile.Selected = false

	idx := -1
	for i, f := range t.ChosenFiles {
		if f.Index == addFile.Index {
			idx = i
			break
		}
	}

	if idx == -1 {
		return false
	}

	log.Debugf("UnChoosing file for download: %s", addFile.Path)
	t.ChosenFiles = append(t.ChosenFiles[:idx], t.ChosenFiles[idx+1:]...)

	if t.IsMemoryStorage() {
		return true
	}

	t.withHandle(func(th lt.TorrentHandle) {
		th.FilePriority(addFile.Index, 0)
	})

	// Need to sleep because file_priority is executed async
	time.Sleep(50 * time.Millisecond)

	return true
}

// InfoHash ...
func (t *Torrent) InfoHash() string {
	return t.infoHash
}

// Name ...
func (t *Torrent) Name() string {
	if t.name != "" || t.Closer.IsSet() {
		return t.name
	}

	if t.ti.Swigcptr() == 0 {
		return ""
	}

	t.name = t.ti.Name()
	return t.name
}

// Title returns name of a torrent, or, if present, how it looked in plugin that found it.
func (t *Torrent) Title() string {
	if t.title != "" {
		return t.title
	}

	if oldTorrent, err := t.GetOldTorrent(); err == nil && oldTorrent != nil && oldTorrent.Title != "" {
		t.title = oldTorrent.Title
		return t.title
	}

	b := t.GetMetadata()
	if len(b) == 0 {
		return t.Name()
	}

	var torrentFile *TorrentFileRaw
	if errDec := bencode.DecodeBytes(b, &torrentFile); errDec != nil || torrentFile == nil || torrentFile.Title == "" {
		return t.Name()
	}

	t.title = torrentFile.Title
	return t.title
}

// GetSelectedSize returns size of all chosen files
func (t *Torrent) GetSelectedSize() int64 {
	if len(t.ChosenFiles) == 0 {
		return 0
	}

	var res int64
	for _, f := range t.ChosenFiles {
		res += int64(f.Size)
	}
	return res
}

// Length ...
func (t *Torrent) Length() int64 {
	if t.Closer.IsSet() || t.th == nil || t.ti == nil || t.ti.Swigcptr() == 0 || !t.gotMetainfo.IsSet() {
		return 0
	}

	return t.ti.TotalSize()
}

// Drop ...
func (t *Torrent) Drop(removeFiles, removeData bool) {
	defer perf.ScopeTimer()()

	t.Closer.Set()
	log.Infof("Dropping torrent: %s", t.Name())

	for _, r := range t.readers {
		if r != nil {
			r.Close()
		}
	}

	// Removing in background to avoid blocking UI
	go func() {
		// Initiating memory release after torrent is removed
		if t.IsMemoryStorage() {
			defer util.FreeMemoryGC()
		}

		toRemove := 0
		if removeData && !t.IsMemoryStorage() {
			toRemove = 1
			log.Info("Removing the torrent and deleting files after playing ...")
		} else {
			log.Info("Removing the torrent without deleting files after playing ...")
		}

		// Wait for running libtorrent calls, Closer is already set, so no new calls would start
		t.muHandle.Lock()
		err := t.Service.Session.RemoveTorrent(t.th, toRemove)
		t.muHandle.Unlock()
		if err != nil {
			log.Errorf("Could not remove torrent: %s", err)
		}

		if removeFiles {
			// Delete torrent file if it is located in temporary path
			if len(t.torrentFile) > 0 && strings.HasPrefix(t.torrentFile, config.Get().TemporaryPath) {
				if i, err := os.Stat(t.torrentFile); err == nil && !i.IsDir() {
					log.Infof("Deleting torrent file at %s", t.torrentFile)
					defer os.Remove(t.torrentFile)
				}
			}

			// Delete InfoHash torrent file
			infoHash := t.InfoHash()
			savedFilePath := filepath.Join(config.Get().TorrentsPath, fmt.Sprintf("%s.torrent", infoHash))
			if _, err := os.Stat(savedFilePath); err == nil {
				log.Infof("Deleting saved torrent file at %s", savedFilePath)
				defer os.Remove(savedFilePath)
			}
		}

		if removeFiles || t.IsMemoryStorage() {
			// Removing .fastresume file
			if _, err := os.Stat(t.fastResumeFile); err == nil {
				log.Infof("Deleting fast resume data at %s", t.fastResumeFile)
				defer os.Remove(t.fastResumeFile)
			}

			// Removing .parts file
			if _, err := os.Stat(t.partsFile); err == nil {
				log.Infof("Deleting parts file at %s", t.partsFile)
				defer os.Remove(t.partsFile)
			}

			// Removing .memory/.file file
			if _, err := os.Stat(t.memoryStorageFile); err == nil {
				log.Infof("Deleting storage type file at %s", t.memoryStorageFile)
				defer os.Remove(t.memoryStorageFile)
			}
			if _, err := os.Stat(t.fileStorageFile); err == nil {
				log.Infof("Deleting storage type file at %s", t.fileStorageFile)
				defer os.Remove(t.fileStorageFile)
			}
		}

		log.Infof("Removed %s from database", t.Name())
	}()
}

// Pause ...
func (t *Torrent) Pause() {
	if t.Closer.IsSet() {
		return
	}

	log.Infof("Pausing torrent: %s", t.InfoHash())

	t.withHandle(func(th lt.TorrentHandle) {
		th.AutoManaged(false)
		th.Pause()
	})

	t.IsPaused = true
}

// Resume ...
func (t *Torrent) Resume() {
	if t.Closer.IsSet() {
		return
	}

	log.Infof("Resuming torrent: %s", t.InfoHash())

	t.withHandle(func(th lt.TorrentHandle) {
		th.AutoManaged(true)
		th.Resume()
	})

	t.IsPaused = false
}

// ForceRecheck re-checks torrent's data
func (t *Torrent) ForceRecheck() {
	log.Infof("Re-checking torrent: %s", t.InfoHash())

	t.withHandle(func(th lt.TorrentHandle) {
		th.ForceRecheck()
	})
}

// GetDBItem ...
func (t *Torrent) GetDBItem() *database.BTItem {
	return t.DBItem
}

// FetchDBItem ...
func (t *Torrent) FetchDBItem() *database.BTItem {
	t.DBItem = database.GetStorm().GetBTItem(t.infoHash)
	return t.DBItem
}

// SaveMetainfo ...
func (t *Torrent) SaveMetainfo(path string) (string, error) {
	defer perf.ScopeTimer()()

	// Not saving torrent for memory storage
	if t.IsMemoryStorage() {
		return path, fmt.Errorf("Torrent uses memory storage")
	}
	if t.th == nil {
		return path, fmt.Errorf("Torrent is not available")
	}
	if _, err := os.Stat(path); err != nil {
		return path, fmt.Errorf("Directory %s does not exist", path)
	}

	path = filepath.Join(path, t.InfoHash()+".torrent")
	// If .torrent file is already created - do not modify it, to avoid breaking the sorting.
	if _, err := os.Stat(path); err == nil {
		return path, nil
	}

	out, _ := t.UpdateDatabaseMetadata(t.GetMetadata())
	os.WriteFile(path, out, 0644)

	return path, nil
}

// GetReadaheadSize ...
func (t *Torrent) GetReadaheadSize() (ret int64) {
	defer perf.ScopeTimer()()

	defaultRA := int64(50 * 1024 * 1024)
	if !t.IsMemoryStorage() {
		return defaultRA
	}

	size := defaultRA
	if t.Storage() != nil && len(t.readers) > 0 {
		size = lt.GetMemorySize()
	}
	if size < 0 {
		return 0
	}

	return int64(t.MemorySize - (int64(len(t.reservedPieces)+2))*t.pieceLength)
}

// CloseReaders ...
func (t *Torrent) CloseReaders() {
	t.muReaders.Lock()
	defer t.muReaders.Unlock()

	for k, r := range t.readers {
		log.Debugf("Closing active reader: %d", r.id)
		r.Close()
		delete(t.readers, k)
	}
}

// ResetReaders ...
func (t *Torrent) ResetReaders() {
	t.muReaders.Lock()
	defer t.muReaders.Unlock()

	if len(t.readers) == 0 {
		return
	}

	perReaderSize := t.GetReadaheadSize()
	countActive := float64(0)
	countIdle := float64(0)
	countHead := float64(0)
	for _, r := range t.readers {
		if r.IsHead() {
			countHead++
		} else if r.IsActive() {
			countActive++
		} else {
			countIdle++
		}
	}

	sizeActive := int64(0)
	sizeIdle := int64(0)
	sizeHead := int64(t.pieceLength)

	if countIdle > 1 {
		countIdle = 2
	}
	if countActive > 1 {
		countActive = 2
	}
	if countHead > 1 {
		countHead = 2
	}

	if countIdle > 0 {
		sizeIdle = int64(float64(perReaderSize) * 0.33)
		if countActive > 0 {
			sizeActive = perReaderSize - sizeIdle
		}
	} else if countActive > 0 {
		sizeActive = int64(float64(perReaderSize) / countActive)
	}

	if countActive == 0 && countIdle == 0 && countHead == 0 {
		return
	}

	for _, r := range t.readers {
		size := sizeActive
		if r.IsHead() {
			size = sizeHead
		} else if !r.IsActive() {
			size = sizeIdle
		}

		if r.readahead == size {
			continue
		}

		log.Infof("Setting readahead for reader %d as %s", r.id, humanize.Bytes(uint64(size)))
		r.readahead = size
	}
}

// ReadersReadaheadSum ...
func (t *Torrent) ReadersReadaheadSum() int64 {
	t.muReaders.Lock()
	defer t.muReaders.Unlock()

	if len(t.readers) == 0 {
		return 0
	}

	res := int64(0)
	for _, r := range t.readers {
		res += r.Readahead()
	}

	return res
}

// GetMetadata ...
func (t *Torrent) GetMetadata() []byte {
	defer perf.ScopeTimer()()

	if t.ti == nil || t.ti.Swigcptr() == 0 {
		return nil
	}

	torrentFile := lt.NewCreateTorrent(t.ti)
	defer lt.DeleteCreateTorrent(torrentFile)

	torrentContent := torrentFile.Generate()
	defer lt.DeleteEntry(torrentContent)

	return []byte(lt.Bencode(torrentContent))
}

// MakeFiles ...
func (t *Torrent) MakeFiles() {
	numFiles := t.ti.NumFiles()
	files := t.ti.Files()
	t.files = []*File{}

	for i := range numFiles {
		pr := t.GetFilePieces(files, i)

		t.files = append(t.files, &File{
			Index:      i,
			Name:       files.FileName(i),
			Size:       files.FileSize(i),
			Offset:     files.FileOffset(i),
			Path:       files.FilePath(i),
			PieceStart: pr.Begin,
			PieceEnd:   pr.End,
		})
	}
}

// GetFileByPath ...
func (t *Torrent) GetFileByPath(q string) *File {
	for _, f := range t.files {
		if f.Path == q {
			return f
		}
	}

	return nil
}

// GetFileByIndex ...
func (t *Torrent) GetFileByIndex(q int) *File {
	for _, f := range t.files {
		if f.Index == q {
			return f
		}
	}

	return nil
}

func (t *Torrent) hasPiece(idx int) bool {
	t.UpdateStatus(piecesRefreshDuration)

	st := t.GetStatus()
	return st.State <= int(lt.TorrentStatusSeeding) && st.Pieces.GetBit(idx)
}

func (t *Torrent) onMetadataReceived() {
	if t.Service.Closer.IsSet() || t.Closer.IsSet() {
		return
	}

	defer t.gotMetainfo.Set()

	t.withHandle(func(th lt.TorrentHandle) {
		t.ti = th.TorrentFile()
	})

	t.pieceLength = int64(t.ti.PieceLength())
	t.pieceCount = int(t.ti.NumPieces())

	t.MakeFiles()

	// Reset fastResumeFile
	infoHash := t.InfoHash()
	t.fastResumeFile = filepath.Join(t.Service.config.TorrentsPath, fmt.Sprintf("%s.fastresume", infoHash))
	t.partsFile = filepath.Join(t.Service.config.DownloadPath, fmt.Sprintf(".%s.parts", infoHash))
	t.memoryStorageFile = filepath.Join(t.Service.config.TorrentsPath, fmt.Sprintf(".%s.memory", infoHash))
	t.fileStorageFile = filepath.Join(t.Service.config.TorrentsPath, fmt.Sprintf(".%s.file", infoHash))

	go func() {
		if t.IsMemoryStorage() {
			os.Create(t.memoryStorageFile)
		} else {
			os.Create(t.fileStorageFile)
		}

		// After metadata is fetched for a torrent, we should
		// save it to torrent file for re-adding after program restart.
		t.UpdateTorrentMetadata()

		if p, err := t.SaveMetainfo(t.Service.config.TorrentsPath); err == nil {
			// Removing .torrent file
			if i, err := os.Stat(t.torrentFile); err == nil && !i.IsDir() && t.torrentFile != p && strings.Contains(t.torrentFile, t.Service.config.TorrentsPath) {
				log.Infof("Deleting old torrent file at %s", t.torrentFile)
				os.Remove(t.torrentFile)
			}

			t.torrentFile = p
		}

		t.SaveMetainfo(config.Get().Info.TempPath)
	}()
}

// HasMetadata ...
func (t *Torrent) HasMetadata() bool {
	return t.gotMetainfo.IsSet() || t.GetStatus().HasMetadata
}

// WaitForMetadata waits for getting torrent information or cancels if torrent is closed
func (t *Torrent) WaitForMetadata(xbmcHost *xbmc.XBMCHost, infoHash string) (err error) {
	sc := t.Service.Closer.C()
	tc := t.Closer.C()
	mc := t.GotInfo()
	to := time.NewTicker(time.Duration(config.Get().MagnetResolveTimeout) * time.Second)
	defer to.Stop()

	log.Infof("Waiting for information fetched for torrent: %s", infoHash)
	var dialog *xbmc.DialogProgressBG
	if xbmcHost != nil {
		dialog = xbmcHost.NewDialogProgressBG("Elementum", "LOCALIZE[30583]", "LOCALIZE[30583]")
	}

	defer func() {
		if dialog != nil {
			dialog.Close()
		}
	}()

	for {
		select {
		case <-to.C:
			err = fmt.Errorf("Expired timeout for resolving magnet link for %d seconds", config.Get().MagnetResolveTimeout)
			log.Error(err)
			return err

		case <-sc:
			log.Warningf("Cancelling waiting for torrent metadata due to service closing: %s", infoHash)
			return

		case <-tc:
			log.Warningf("Cancelling waiting for torrent metadata due to torrent closing: %s", infoHash)
			return

		case <-mc:
			log.Infof("Information fetched for torrent: %s", infoHash)
			return

		}
	}
}

// GetPaused ...
func (t *Torrent) GetPaused() bool {
	return t.GetStatus().Paused
}

// GetNextEpisodeFile ...
func (t *Torrent) GetNextEpisodeFile(season, episode int) *File {
	re := regexp.MustCompile(fmt.Sprintf(episodeMatchRegex, season, episode))
	for _, choice := range t.files {
		if re.MatchString(choice.Name) {
			return choice
		}
	}

	return nil
}

// GetNextSingleEpisodeFile ...
func (t *Torrent) GetNextSingleEpisodeFile(episode int) *File {
	lastMatched, foundMatches := 0, 0

	re := regexp.MustCompile(fmt.Sprintf(singleEpisodeMatchRegex, episode))
	for index, choice := range t.files {
		if re.MatchString(choice.Name) {
			lastMatched = index
			foundMatches++
		}
	}

	if foundMatches == 1 {
		return t.files[lastMatched]
	}

	return nil
}

// HasAvailableFiles ...
func (t *Torrent) HasAvailableFiles() bool {
	// Keeping it simple? If not all files are chosen - then true?
	return len(t.ChosenFiles) < len(t.files)
}

// GetFilePieces ...
func (t *Torrent) GetFilePieces(files lt.FileStorage, idx int) (ret PieceRange) {
	ret.Begin, ret.End = t.byteRegionPieces(files.FileOffset(idx), files.FileSize(idx))
	return
}

func (t *Torrent) byteRegionPieces(off, size int64) (begin, end int) {
	if t.pieceLength <= 0 {
		return
	}

	begin = max(0, int(off/t.pieceLength))
	end = min(t.pieceCount-1, int((off+size-1)/t.pieceLength))

	return
}

// GetFiles returns all files of a torrent
func (t *Torrent) GetFiles() []*File {
	return t.files
}

// GetCandidateFileForIndex returns CandidateFile for specific int index
func (t *Torrent) GetCandidateFileForIndex(idx int) *CandidateFile {
	if idx < 0 {
		return nil
	}

	if candidates, _, err := t.GetCandidateFiles(nil); err == nil {
		if len(candidates) <= idx {
			return nil
		}

		return candidates[idx]
	}

	return nil
}

// GetCandidateFiles returns all the files for selecting by user
func (t *Torrent) GetCandidateFiles(btp *Player) ([]*CandidateFile, int, error) {
	biggestFile := 0
	maxSize := int64(0)
	files := t.files
	isBluRay := false

	// Define the minimum file size limit.
	// Hierarchy: 1. Manual request override, 2. Smart show calculation, 3. Global config.
	minSize := config.Get().MinCandidateSize

	if btp != nil && btp.p.MinSize >= 0 {
		// Apply manual override from the request parameter (URL)
		minSize = btp.p.MinSize
		log.Infof("Overriding min_size to %d bytes via request parameter", minSize)
	} else if btp != nil && btp.p.ShowID != 0 {
		// Fallback to smart calculation for TV shows based on runtime
		if s := tmdb.GetShow(btp.p.ShowID, config.Get().Language); s != nil {
			runtime := 30
			if len(s.EpisodeRunTime) > 0 {
				for _, r := range s.EpisodeRunTime {
					if r < runtime {
						runtime = r
					}
				}
			}

			minSize = config.Get().MinCandidateShowSize * int64(runtime)
		}
	}

	var candidateFiles []int

	reRar := regexp.MustCompile(rarMatchRegex)
	reSkip := regexp.MustCompile(skipFileRegex)
	for i, f := range files {
		size := f.Size
		if size > maxSize {
			maxSize = size
			biggestFile = i
		}

		fileName := filepath.Base(f.Path)
		if reSkip.MatchString(fileName) {
			continue
		}
		if size > minSize {
			candidateFiles = append(candidateFiles, i)
		}
		if strings.Contains(f.Path, "BDMV/STREAM/") {
			isBluRay = true
			continue
		}

		if reRar.MatchString(fileName) && size > 10*1024*1024 {
			t.IsRarArchive = true
			if btp == nil || btp.xbmcHost == nil {
				return nil, -1, errors.New("RAR archive detected and download was cancelled")
			}
			if !btp.xbmcHost.DialogConfirm("Elementum", "LOCALIZE[30303]") {
				btp.notEnoughSpace = true
				return nil, -1, errors.New("RAR archive detected and download was cancelled")
			}
		}
	}

	if isBluRay {
		candidateFiles = []int{}
		dirs := map[string]int{}

		for i, f := range files {
			if idx := strings.Index(f.Path, "BDMV/STREAM/"); idx != -1 {
				dir := f.Path[0 : idx-1]
				if _, ok := dirs[dir]; !ok {
					dirs[dir] = i
				} else if files[dirs[dir]].Size < files[i].Size {
					dirs[dir] = i
				}
			}
		}

		choices := make([]*CandidateFile, 0, len(candidateFiles))
		for dir, index := range dirs {
			choices = append(choices, &CandidateFile{
				Index:       index,
				Filename:    dir,
				DisplayName: dir,
				Path:        dir,
			})
		}

		TrimChoices(choices)

		return choices, biggestFile, nil
	}

	if len(candidateFiles) > 1 {
		log.Infof("There are %d candidate files", len(candidateFiles))
		choices := make([]*CandidateFile, 0, len(candidateFiles))
		for _, index := range candidateFiles {
			fileName := filepath.Base(files[index].Path)
			choices = append(choices, &CandidateFile{
				Index:       index,
				Filename:    fileName,
				DisplayName: files[index].Path,
				Path:        files[index].Path,
				Size:        files[index].Size,
			})
		}

		TrimChoices(choices)

		return choices, biggestFile, nil
	}

	return nil, biggestFile, nil
}

// SelectDownloadFiles selects files for download, according to setting
func (t *Torrent) SelectDownloadFiles(btp *Player) {
	strategy := config.Get().DownloadFileStrategy

	if strategy == DownloadFilePlaying || t.IsMemoryStorage() {
		return
	}

	files := t.files
	choices, _, err := t.GetCandidateFiles(btp)
	if err != nil || choices == nil {
		return
	}

	if strategy == DownloadFileSeason {
		if btp == nil || btp.p.Season <= 0 {
			return
		}

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

		for _, season := range show.Seasons {
			if season == nil || season.EpisodeCount == 0 || season.Season != btp.p.Season {
				continue
			}
			tmdbSeason := tmdb.GetSeason(btp.p.ShowID, season.Season, config.Get().Language, len(show.Seasons), false)
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
					t.DownloadFile(files[choices[index].Index])
				}
			}
		}

		t.SaveDBFiles()
	} else if strategy == DownloadFileAll {
		for _, f := range files {
			t.DownloadFile(f)
		}

		t.SaveDBFiles()
	}
}

// ChooseFile opens file selector if not provided with Player, otherwise tries to detect what to open.
func (t *Torrent) ChooseFile(btp *Player, xbmcHost *xbmc.XBMCHost) (*File, int, error) {
	// Checking if we need to open specific file from torrent file.
	if btp != nil && btp.p.OriginalIndex >= 0 {
		for _, f := range t.files {
			if f.Index == btp.p.OriginalIndex {
				return f, 0, nil
			}
		}
	}

	files := t.files
	choices, biggestFile, err := t.GetCandidateFiles(btp)
	if err != nil {
		return nil, -1, err
	}

	// Automatic file selection via regex match
	if btp != nil && btp.p.FileMatch != "" && btp.p.FileIndex < 0 {
		re, err := regexp.Compile("(?i)" + btp.p.FileMatch)
		if err == nil {
			for i, choice := range choices {
				if re.MatchString(choice.Filename) || re.MatchString(choice.Path) {
					log.Infof("File found via regex match ('%s'): %s", btp.p.FileMatch, choice.Path)
					return files[choice.Index], i, nil
				}
			}
			log.Infof("No file matches the provided regex: %s", btp.p.FileMatch)
		} else {
			log.Errorf("Invalid regex provided in file_match: %s", err)
		}
	}

	if len(choices) > 1 {
		// Adding sizes to file names with optional position control
		if btp != nil {
			for _, c := range choices {
				sizeLabel := "[COLOR lightyellow][" + humanize.Bytes(uint64(files[c.Index].Size)) + "][/COLOR]"

				if btp.p.SizeAtStart {
					// Prepend size label to the display name for better visibility in long names
					c.DisplayName = sizeLabel + " " + c.DisplayName
				} else if btp.p.Episode == 0 {
					// Default legacy behavior: append size only for movies
					c.DisplayName += " " + sizeLabel
				}
			}
		}

		if btp != nil && btp.p.Season > 0 && btp.p.FileIndex < 0 {
			// In episode search we are using smart-match to store found episodes
			//   in the torrent history table
			go btp.smartMatch(choices)

			lastMatched, foundMatches := MatchEpisodeFilename(btp.p.Season, btp.p.Episode, false, btp.p.Season, nil, nil, nil, choices)

			if foundMatches == 1 {
				return files[choices[lastMatched].Index], lastMatched, nil
			}

			if s := tmdb.GetShow(btp.p.ShowID, config.Get().Language); s != nil && s.IsAnime() {
				season := tmdb.GetSeason(btp.p.ShowID, btp.p.Season, config.Get().Language, len(s.Seasons), false)
				if season != nil && season.HasEpisode(btp.p.Episode) {
					an, _ := s.ShowInfo(season.GetEpisode(btp.p.Episode))
					if an != 0 {
						btp.p.AbsoluteNumber = an

						re := regexp.MustCompile(fmt.Sprintf(singleEpisodeMatchRegex, btp.p.AbsoluteNumber))
						for index, choice := range choices {
							if re.MatchString(choice.Filename) {
								lastMatched = index
								foundMatches++
							}
						}

						if foundMatches == 1 {
							if btp == nil {
								t.DownloadFile(files[choices[lastMatched].Index])
								t.SaveDBFiles()
							}
							return files[choices[lastMatched].Index], lastMatched, nil
						}
					}
				}
			}
		}

		// Returning exact file if it is requested
		if btp != nil && btp.p.FileIndex >= 0 && btp.p.FileIndex < len(choices) {
			return files[choices[btp.p.FileIndex].Index], btp.p.FileIndex, nil
		}

		// External callers may explicitly disable the manual file picker.
		// Automatic selection above still has priority and existing behavior
		// remains unchanged unless this option is requested.
		if btp != nil && btp.p.SkipFileDialog {
			if xbmcHost != nil {
				xbmcHost.Notify("Elementum", "No matching file was found in this torrent.", config.AddonIcon())
			}
			return nil, -1, fmt.Errorf("automatic file selection failed and file dialog is disabled")
		}

		searchTitle := ""
		if btp != nil {
			if btp.p.AbsoluteNumber > 0 {
				searchTitle += fmt.Sprintf("E%d", btp.p.AbsoluteNumber)
			}
			if btp.p.Episode > 0 {
				if searchTitle != "" {
					searchTitle += " | "
				}
				searchTitle += fmt.Sprintf("S%dE%d", btp.p.Season, btp.p.Episode)
			} else if m := tmdb.GetMovieByID(strconv.Itoa(btp.p.TMDBId), config.Get().Language); m != nil {
				searchTitle += m.GetTitle()
			}
		}

		items := make([]string, 0, len(choices))
		for _, choice := range choices {
			items = append(items, choice.DisplayName)
		}

		if btp != nil && btp.xbmcHost != nil {
			xbmcHost = btp.xbmcHost
		} else if xbmcHost == nil {
			xbmcHost, _ = xbmc.GetLocalXBMCHost()
		}
		if xbmcHost == nil {
			return nil, -1, errNoXBMCHost
		}

		choice := xbmcHost.ListDialog("LOCALIZE[30560];;"+searchTitle, items...)
		log.Debugf("Choice selected: %d", choice)
		if choice >= 0 {
			if btp == nil {
				t.DownloadFile(files[choices[choice].Index])
				t.SaveDBFiles()
			}

			return files[choices[choice].Index], choice, nil
		}
		return nil, -1, fmt.Errorf("User cancelled")
	} else if len(choices) == 1 {
		if btp == nil {
			t.DownloadFile(files[choices[0].Index])
			t.SaveDBFiles()
		}

		return files[choices[0].Index], 0, nil
	}

	// If there no proper candidates - just open the biggest file
	if btp == nil {
		t.DownloadFile(files[biggestFile])
		t.SaveDBFiles()
	}

	if len(files) == 0 {
		return nil, -1, fmt.Errorf("files list is empty")
	}

	return files[biggestFile], -1, nil
}

// GetPlayURL returns url ready for Kodi
func (t *Torrent) GetPlayURL(fileIndex string) string {
	var (
		tmdbID      string
		show        string
		season      string
		episode     string
		query       string
		contentType string
	)

	toBeAdded := ""
	if t.DBItem != nil && t.DBItem.Type != "" {
		contentType = t.DBItem.Type
		if contentType == movieType {
			tmdbID = strconv.Itoa(t.DBItem.ID)
			if movie := tmdb.GetMovie(t.DBItem.ID, config.Get().Language); movie != nil {
				toBeAdded = fmt.Sprintf("%s (%d)", movie.OriginalTitle, movie.Year())
			}
		} else if contentType == showType || contentType == episodeType {
			show = strconv.Itoa(t.DBItem.ShowID)
			season = strconv.Itoa(t.DBItem.Season)
			episode = strconv.Itoa(t.DBItem.Episode)
			if show := tmdb.GetShow(t.DBItem.ShowID, config.Get().Language); show != nil {
				toBeAdded = fmt.Sprintf("%s S%02dE%02d", show.OriginalName, t.DBItem.Season, t.DBItem.Episode)
			}
		} else if contentType == searchType {
			tmdbID = strconv.Itoa(t.DBItem.ID)
			query = t.DBItem.Query
			toBeAdded = t.DBItem.Query
		}
	}

	return URLQuery(fmt.Sprintf(URLForXBMC("/play")+"/%s", url.PathEscape(toBeAdded)),
		"resume", t.InfoHash(),
		"type", contentType,
		"index", fileIndex,
		"tmdb", tmdbID,
		"show", show,
		"season", season,
		"episode", episode,
		"query", query)
}

// TorrentInfo writes torrent status to io.Writer
func (t *Torrent) TorrentInfo(xbmcHost *xbmc.XBMCHost, w io.Writer, showTrackers, showPieces bool) {
	if t.Closer.IsSet() {
		return
	}

	defer perf.ScopeTimer()()

	t.UpdateStatus(0)
	st := t.GetStatus()

	// Torrent info is empty until metadata is received
	var numPieces, pieceLength int
	var totalSize int64
	if t.ti != nil && t.ti.Swigcptr() != 0 {
		numPieces, pieceLength, totalSize = t.ti.NumPieces(), t.ti.PieceLength(), t.ti.TotalSize()
	}

	fmt.Fprintf(w, "    Name:               %s \n", st.Name)
	fmt.Fprintf(w, "    Title:              %s \n", t.Title())
	fmt.Fprintf(w, "    Infohash:           %s \n", st.InfoHash)
	fmt.Fprintf(w, "    Status:             %s \n", xbmcHost.Translate(StatusStrings[st.State]))
	fmt.Fprintf(w, "    Piece size:         %d \n", numPieces)
	fmt.Fprintf(w, "    Piece length:       %s \n", humanize.Bytes(uint64(pieceLength)))
	fmt.Fprint(w, "\n")
	fmt.Fprint(w, "    Speed:\n")
	fmt.Fprintf(w, "        Download:   %s/s \n", humanize.Bytes(uint64(st.DownloadRate)))
	fmt.Fprintf(w, "        Upload:     %s/s \n", humanize.Bytes(uint64(st.UploadRate)))
	fmt.Fprint(w, "\n")
	fmt.Fprint(w, "    Size:\n")
	fmt.Fprintf(w, "        Total:                  %v \n", humanize.Bytes(uint64(totalSize)))
	fmt.Fprintf(w, "        Done:                   %v (%.2f%%) \n", humanize.Bytes(uint64(st.TotalDone)), 100*(float64(st.TotalDone)/float64(totalSize)))
	fmt.Fprintf(w, "        Selected:               %v (%.2f%%) \n", humanize.Bytes(uint64(t.GetSelectedSize())), 100*(float64(st.TotalDone)/float64(t.GetSelectedSize())))
	fmt.Fprintf(w, "        Wanted:                 %v \n", humanize.Bytes(uint64(st.TotalWanted)))
	fmt.Fprintf(w, "        Wanted done:            %v (%.2f%%) \n", humanize.Bytes(uint64(st.TotalWantedDone)), 100*(float64(st.TotalWantedDone)/float64(st.TotalWanted)))
	fmt.Fprint(w, "\n")
	fmt.Fprint(w, "    Connections:\n")
	fmt.Fprintf(w, "        Connections:            %d \n", st.NumConnections)
	fmt.Fprintf(w, "        Connected seeds:        %d \n", st.NumSeeds)
	fmt.Fprintf(w, "        Connected peers:        %d \n", st.NumPeers)
	fmt.Fprintf(w, "        NumComplete:            %d \n", st.NumComplete)
	fmt.Fprintf(w, "        NumIncomplete:          %d \n", st.NumIncomplete)
	fmt.Fprintf(w, "        ListSeeds:              %d \n", st.ListSeeds)
	fmt.Fprintf(w, "        ListPeers:              %d \n", st.ListPeers)

	fmt.Fprint(w, "    Flags:\n")
	fmt.Fprintf(w, "        paused: %v \n", st.Paused)
	fmt.Fprintf(w, "        auto_managed: %v \n", st.AutoManaged)
	fmt.Fprintf(w, "        sequential_download: %v \n", st.SequentialDownload)
	fmt.Fprintf(w, "        need_save_resume: %v \n", st.NeedSaveResume)
	fmt.Fprintf(w, "        is_seeding: %v \n", st.IsSeeding)
	fmt.Fprintf(w, "        is_finished: %v \n", st.IsFinished)
	fmt.Fprintf(w, "        has_metadata: %v \n", st.HasMetadata)
	fmt.Fprintf(w, "        has_incoming: %v \n", st.HasIncoming)
	fmt.Fprintf(w, "        moving_storage: %v \n", st.MovingStorage)
	fmt.Fprintf(w, "        announcing_to_trackers: %v \n", st.AnnouncingToTrackers)
	fmt.Fprintf(w, "        announcing_to_lsd: %v \n", st.AnnouncingToLsd)
	fmt.Fprintf(w, "        announcing_to_dht: %v \n", st.AnnouncingToDht)
	fmt.Fprint(w, "\n")

	fmt.Fprint(w, "    Files (Priority):\n")
	t.withHandle(func(th lt.TorrentHandle) {
		filePriorities := th.FilePriorities()
		defer lt.DeleteStdVectorInt(filePriorities)

		slices.SortFunc(t.files, func(a, b *File) int { return cmp.Compare(a.Path, b.Path) })
		for _, f := range t.files {
			if pr := filePriorities.Get(f.Index); pr > 0 {
				fmt.Fprintf(w, "        %s (%s): %d \n", f.Path, humanize.Bytes(uint64(f.Size)), pr)
			} else {
				fmt.Fprintf(w, "        %s (%s): - \n", f.Path, humanize.Bytes(uint64(f.Size)))
			}
		}
	})

	fmt.Fprint(w, "\n")

	if showTrackers {
		fmt.Fprint(w, "    Libtorrent Trackers:\n")

		// Lines start with tracker url, so sorting lines sorts trackers by name
		lines := []string{}
		t.withHandle(func(th lt.TorrentHandle) {
			trackers := th.Trackers()
			trackersSize := trackers.Size()
			for i := 0; i < int(trackersSize); i++ {
				tracker := trackers.Get(i)
				lines = append(lines, fmt.Sprintf("        %-60s: %-3s seeds, %-3s peers, updating: %-5v, is_working: %-5v, message: %s\n",
					tracker.GetUrl(),
					peerNumToString(tracker.GetScrapeComplete()),
					peerNumToString(tracker.GetScrapeIncomplete()),
					tracker.GetUpdating(),
					tracker.IsWorking(),
					tracker.GetMessage()))
			}
		})
		slices.Sort(lines)
		fmt.Fprint(w, strings.Join(lines, ""))

		fmt.Fprint(w, "\n")
		fmt.Fprint(w, "    Invernal Trackers:\n")

		lines = lines[:0]
		t.trackers.Range(func(t, p any) bool {
			lines = append(lines, fmt.Sprintf("        %-60s: %-3d peers\n", t, p))
			return true
		})
		slices.Sort(lines)
		fmt.Fprint(w, strings.Join(lines, ""))
		fmt.Fprint(w, "\n")
	}

	if t.Closer.IsSet() || t.ti == nil || t.ti.Swigcptr() == 0 {
		return
	}

	if showPieces {
		var priorities []int
		t.withHandle(func(th lt.TorrentHandle) {
			piecesPriorities := th.PiecePriorities()
			defer lt.DeleteStdVectorInt(piecesPriorities)

			for i := 0; i < int(piecesPriorities.Size()); i++ {
				priorities = append(priorities, piecesPriorities.Get(i))
			}
		})

		piecesStatus := bytebufferpool.Get()
		piecesStatus.Reset()
		piecesStatus.WriteString("        ")
		for i := 0; i < t.ti.NumPieces(); i++ {
			if st.Pieces.GetBit(i) {
				piecesStatus.WriteString("+")
			} else if i >= len(priorities) || priorities[i] == 0 {
				piecesStatus.WriteString("-")
			} else {
				piecesStatus.WriteString(strconv.Itoa(priorities[i]))
			}

			if i > 0 && (i+1)%100 == 0 {
				piecesStatus.WriteString("\n        ")
			}
		}
		fmt.Fprint(w, "    Pieces:\n")
		fmt.Fprint(w, piecesStatus.String())
		bytebufferpool.Put(piecesStatus)
		fmt.Fprint(w, "\n")
	}
}

// IsMemoryStorage is a shortcut for checking whether we run memory storage
func (t *Torrent) IsMemoryStorage() bool {
	return t.DownloadStorage == config.StorageMemory
}

// AlertFinished sends notification to user that this torrent is successfully downloaded
func (t *Torrent) AlertFinished() {
	if !t.IsNeedFinishNotification || t.IsMemoryStorage() || t.GetProgress() < 100 {
		return
	}

	t.IsNeedFinishNotification = false

	if xbmcHost, err := xbmc.GetLocalXBMCHost(); xbmcHost != nil && err == nil {
		xbmcHost.Notify("Elementum", "LOCALIZE[30618];;"+t.Name(), config.AddonIcon())
	}
}

// UpdateTorrentMetadata updates metadata for specific TMDB id
func (t *Torrent) UpdateTorrentMetadata() error {
	defer perf.ScopeTimer()()

	b := t.GetMetadata()
	out, err := t.UpdateDatabaseMetadata(b)
	if err != nil {
		return err
	}

	database.GetStorm().UpdateTorrentMetadata(t.InfoHash(), out)
	return nil
}

// UpdateMetadataTitle ...
func (t *Torrent) UpdateMetadataTitle(title string, in []byte) []byte {
	defer perf.ScopeTimer()()

	var torrentFile *TorrentFileRaw
	if err := bencode.DecodeBytes(in, &torrentFile); err != nil || torrentFile == nil {
		return in
	}

	if title != "" {
		torrentFile.Title = title
	}

	out, err := bencode.EncodeBytes(torrentFile)
	if err != nil {
		return in
	}

	return out
}

// UpdateDatabaseMetadata in decoding torrent bencoded,
// adding proper Title and then returning encoded bencoded.
func (t *Torrent) UpdateDatabaseMetadata(in []byte) ([]byte, error) {
	defer perf.ScopeTimer()()

	log.Debugf("Updating torrent metadata in the database for InfoHash=%s", t.InfoHash())

	oldTorrent, err := t.GetOldTorrent()
	if err != nil {
		return in, err
	}

	out := t.UpdateMetadataTitle(oldTorrent.Title, in)
	return out, nil
}

// GetOldTorrent gets old torrent from the database
func (t *Torrent) GetOldTorrent() (*TorrentFile, error) {
	defer perf.ScopeTimer()()

	var tm database.TorrentAssignMetadata
	if err := database.GetStormDB().One("InfoHash", t.InfoHash(), &tm); err != nil {
		return nil, err
	}

	oldTorrent := &TorrentFile{}
	if tm.Metadata[0] == '{' {
		oldTorrent.UnmarshalJSON(tm.Metadata)
	} else {
		oldTorrent.LoadFromBytes(tm.Metadata)
	}

	return oldTorrent, nil
}

// ResetBuffering resets all fields related to buffering
func (t *Torrent) ResetBuffering() {
	t.IsBuffering = false
	t.IsBufferingFinished = false
	t.BufferLength = 0
	t.BufferProgress = 0
	t.BufferProgressPrevious = 0
	t.BufferPiecesLength = 0

	t.BufferPiecesProgress = map[int]float64{}
	t.BufferProgress = -1
}

func peerNumToString(num int) string {
	if num <= 0 {
		return "-"
	}

	return strconv.Itoa(num)
}

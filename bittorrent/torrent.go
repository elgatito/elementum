package bittorrent

import (
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unsafe"

	lt "github.com/ElementumOrg/libtorrent-go"
	"github.com/RoaringBitmap/roaring"
	"github.com/anacrolix/missinggo/perf"
	"github.com/anacrolix/missinggo/slices"
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
	lastStatus        lt.TorrentStatus
	lastStatusTime    time.Time
	lastProgress      float64
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

	pieceLength int64
	pieceCount  int

	gotMetainfo    event.Event
	Closer         event.Event
	bufferFinished chan struct{}

	piecesMx          sync.RWMutex
	pieces            Bitfield
	piecesLastUpdated time.Time

	bufferTicker     *time.Ticker
	prioritizeTicker *time.Ticker

	nextTimer *time.Timer
}

// NewTorrent ...
func NewTorrent(service *Service, handle lt.TorrentHandle, info lt.TorrentInfo, path string, downloadStorage int) *Torrent {
	log.Infof("Adding torrent with storage: %s", config.Storages[downloadStorage])

	ts := handle.Status()
	defer lt.DeleteTorrentStatus(ts)

	shaHash := ts.GetInfoHash().ToString()
	infoHash := hex.EncodeToString([]byte(shaHash))

	t := &Torrent{
		infoHash: infoHash,

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
	}

	return t
}

func (t *Torrent) init() {
	// Run setters in MemoryStorage
	if t.IsMemoryStorage() {
		t.ms = t.th.GetMemoryStorage()
		t.ms.SetTorrentHandle(t.th)

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
func (t *Torrent) Storage() lt.StorageInterface {
	return t.th.GetStorageImpl()
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
		piecesKeys := []int{}
		for k := range t.BufferPiecesProgress {
			piecesKeys = append(piecesKeys, k)
		}
		sort.Ints(piecesKeys)

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
	if t.Closer.IsSet() || t.th == nil || t.th.Swigcptr() == 0 {
		return 0, 0, 0, 0
	}

	ts := t.GetLastStatus(false)
	if ts == nil {
		return 0, 0, 0, 0
	}

	seeds := ts.GetNumSeeds()
	seedsTotal := max(ts.GetListSeeds(), ts.GetNumComplete())

	peers := ts.GetNumPeers() - ts.GetNumSeeds()
	peersTotal := max(ts.GetListPeers()-ts.GetListSeeds(), ts.GetNumComplete()+ts.GetNumIncomplete())

	return seeds, seedsTotal, peers, peersTotal
}

// GetSpeeds returns download and upload speeds
func (t *Torrent) GetSpeeds() (down, up int) {
	if t.th == nil || t.th.Swigcptr() == 0 {
		return 0, 0
	}

	ts := t.GetLastStatus(false)
	if ts == nil {
		return 0, 0
	}

	return ts.GetDownloadPayloadRate(), ts.GetUploadPayloadRate()
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
			t.ms.SetMemorySize(t.MemorySize)
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
	if isStartup {
		piecesPriorities := t.th.PiecePriorities()
		defer lt.DeleteStdVectorInt(piecesPriorities)

		for curPiece = preBufferStart; curPiece <= preBufferEnd; curPiece++ { // get this part
			piecesPriorities.Set(curPiece, 7)
		}
		for curPiece = postBufferStart; curPiece <= postBufferEnd; curPiece++ { // get this part
			piecesPriorities.Set(curPiece, 7)
		}
		t.th.PrioritizePieces(piecesPriorities)
	} else {
		for curPiece = preBufferStart; curPiece <= preBufferEnd; curPiece++ { // get this part
			t.demandPieces.AddInt(curPiece)
			t.th.PiecePriority(curPiece, 3)
		}
		for curPiece = postBufferStart; curPiece <= postBufferEnd; curPiece++ { // get this part
			t.demandPieces.AddInt(curPiece)
			t.th.PiecePriority(curPiece, 3)
		}
	}
	t.muDemandPieces.Unlock()

	// Using libtorrent hack to pause and resume the torrent
	if config.Get().UseLibtorrentPauseResume && isStartup {
		t.Pause()
		t.Resume()
	}

	// Force reannounce for trackers
	t.th.ForceReannounce()
	if !config.Get().DisableDHT {
		t.th.ForceDhtAnnounce()
	}

	// As long as file storage has many enabled pieces, we make sure buffer pieces are sent immediately
	if !t.IsMemoryStorage() {
		for curPiece = preBufferStart; curPiece <= preBufferEnd; curPiece++ { // get this part
			t.th.SetPieceDeadline(curPiece, 0, 0)
		}
		for curPiece = postBufferStart; curPiece <= postBufferEnd; curPiece++ { // get this part
			t.th.SetPieceDeadline(curPiece, 0, 0)
		}
	}
}

// AdjustMemorySize ...
func (t *Torrent) AdjustMemorySize(ms int64) {
	if t.ms == nil || t.ms.Swigcptr() == 0 {
		return
	}

	defer perf.ScopeTimer()()

	t.MemorySize = ms
	log.Infof("Adjusting memory size to %s!", humanize.Bytes(uint64(t.MemorySize)))
	t.ms.SetMemorySize(t.MemorySize)
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

		t.th.SetPieceDeadline(i, i-piece*100, 0)
	}
}

// ClearDeadlines ...
func (t *Torrent) ClearDeadlines() {
	t.muAwaitingPieces.Lock()
	t.awaitingPieces.Clear()
	t.muAwaitingPieces.Unlock()

	t.th.ClearPieceDeadlines()
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
	sort.Ints(piecesKeys)

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

	if t.IsMemoryStorage() && t.th != nil && t.ms != nil {
		t.ms.UpdateReaderPieces(readerVector)
		t.ms.UpdateReservedPieces(reservedVector)
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
	for curPiece := 0; curPiece < numPieces; curPiece++ {
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
	t.th.PrioritizePieces(piecesPriorities)
}

// GetAddedTime ...
func (t *Torrent) GetAddedTime() time.Time {
	return t.addedTime
}

// GetStatus ...
func (t *Torrent) GetStatus() lt.TorrentStatus {
	return t.th.Status()
}

// GetState ...
func (t *Torrent) GetState() int {
	st := t.GetStatus()
	defer lt.DeleteTorrentStatus(st)

	return int(st.GetState())
}

// GetSmartState ...
func (t *Torrent) GetSmartState() int {
	defer perf.ScopeTimer()()

	if t.th == nil || t.th.Swigcptr() == 0 {
		return StatusQueued
	}

	torrentStatus := t.GetLastStatus(false)
	if torrentStatus == nil {
		return StatusQueued
	}

	progress := float64(torrentStatus.GetProgress()) * 100
	state := int(torrentStatus.GetState())

	if t.Service.Session.IsPaused() {
		return StatusPaused
	} else if torrentStatus.GetPaused() && state != StatusFinished && state != StatusFinding {
		if progress == 100 {
			return StatusFinished
		}

		return StatusPaused
	} else if !torrentStatus.GetPaused() && (state == StatusFinished || progress == 100) {
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

	t.th.GetDownloadQueue(queue)

	st := t.GetLastStatus(false)
	if st == nil {
		return
	}

	blockSize := st.GetBlockSize()
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
	ts := t.GetLastStatus(false)
	if ts != nil {
		t.lastProgress = float64(ts.GetProgress()) * 100
	}

	return t.lastProgress
}

// DownloadFiles sets priority 1 to list of files
func (t *Torrent) DownloadFiles(files []*File) {
	filePriorities := t.th.FilePriorities()
	for _, f := range files {
		log.Debugf("Choosing file for download: %s", f.Path)

		f.Selected = true
		filePriorities.Set(f.Index, 1)
	}
	defer lt.DeleteStdVectorInt(filePriorities)

	t.th.PrioritizeFiles(filePriorities)
}

// UndownloadFiles sets priority 0 to list of files
func (t *Torrent) UndownloadFiles(files []*File) {
	filePriorities := t.th.FilePriorities()
	for _, f := range files {
		log.Debugf("UnChoosing file for download: %s", f.Path)

		f.Selected = false
		filePriorities.Set(f.Index, 0)
	}
	defer lt.DeleteStdVectorInt(filePriorities)

	t.th.PrioritizeFiles(filePriorities)
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
		t.th.FilePriority(f.Index, priority)

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

	t.th.FilePriority(addFile.Index, 0)

	// Need to sleep because file_priority is executed async
	time.Sleep(50 * time.Millisecond)

	return true
}

// InfoHash ...
func (t *Torrent) InfoHash() string {
	if t.th == nil {
		return ""
	}

	ts := t.GetLastStatus(false)
	if ts == nil {
		return ""
	}
	shaHash := ts.GetInfoHash().ToString()

	return hex.EncodeToString([]byte(shaHash))
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

		// TODO: Somehow this drops a double free error, so like it was removed already
		// if t.lastStatus != nil && t.lastStatus.Swigcptr() != 0 {
		// 	lt.DeleteTorrentStatus(t.lastStatus)
		//  t.lastStatus = nil
		// }

		toRemove := 0
		if removeData && !t.IsMemoryStorage() {
			toRemove = 1
			log.Info("Removing the torrent and deleting files after playing ...")
		} else {
			log.Info("Removing the torrent without deleting files after playing ...")
		}

		if err := t.Service.Session.RemoveTorrent(t.th, toRemove); err != nil {
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

	t.th.AutoManaged(false)
	t.th.Pause()

	t.IsPaused = true
}

// Resume ...
func (t *Torrent) Resume() {
	if t.Closer.IsSet() {
		return
	}

	log.Infof("Resuming torrent: %s", t.InfoHash())

	t.th.AutoManaged(true)
	t.th.Resume()

	t.IsPaused = false
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

	for i := 0; i < numFiles; i++ {
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

func (t *Torrent) updatePieces() error {
	defer perf.ScopeTimer()()

	t.piecesMx.Lock()
	defer t.piecesMx.Unlock()

	if time.Now().Before(t.piecesLastUpdated.Add(piecesRefreshDuration)) || t.Closer.IsSet() {
		return nil
	}

	// need to keep a reference to the status or else the pieces bitfield
	// is at risk of being collected
	t.GetLastStatus(true)

	if t.lastStatus == nil || t.lastStatus.GetState() > lt.TorrentStatusSeeding {
		return errors.New("Torrent file has invalid state")
	}

	piecesBits := t.lastStatus.GetPieces()
	piecesBitsSize := piecesBits.Size() + 1
	piecesSliceSize := piecesBitsSize / 8

	if piecesBitsSize%8 > 0 {
		// Add +1 to round up the bitfield
		piecesSliceSize++
	}

	data := (*[100000000]byte)(unsafe.Pointer(piecesBits.Bytes()))[:piecesSliceSize]
	t.pieces = Bitfield(data)
	t.piecesLastUpdated = time.Now()

	return nil
}

func (t *Torrent) hasPiece(idx int) bool {
	if err := t.updatePieces(); err != nil {
		return false
	}
	t.piecesMx.RLock()
	defer t.piecesMx.RUnlock()
	return t.pieces.GetBit(idx)
}

func (t *Torrent) onMetadataReceived() {
	if t.Service.Closer.IsSet() || t.Closer.IsSet() {
		return
	}

	defer t.gotMetainfo.Set()

	t.ti = t.th.TorrentFile()

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
	if t.th == nil || t.th.Swigcptr() == 0 {
		return false
	}

	if t.gotMetainfo.IsSet() {
		return true
	}

	ts := t.GetLastStatus(false)
	if ts == nil {
		return false
	}

	return ts.GetHasMetadata()
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

// GetHandle ...
func (t *Torrent) GetHandle() lt.TorrentHandle {
	return t.th
}

// GetPaused ...
func (t *Torrent) GetPaused() bool {
	if t.th == nil || t.th.Swigcptr() == 0 {
		return false
	}

	ts := t.GetLastStatus(false)
	if ts == nil {
		return false
	}

	return ts.GetPaused()
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

	begin = util.Max(0, int(off/t.pieceLength))
	end = util.Min(t.pieceCount-1, int((off+size-1)/t.pieceLength))

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

	// Calculate minimal size for this type of media.
	// For shows we multiply size_per_minute to properly allow 5-10 minute episodes.
	minSize := config.Get().MinCandidateSize
	if btp != nil && btp.p.ShowID != 0 {
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
func (t *Torrent) ChooseFile(btp *Player) (*File, int, error) {
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

	if len(choices) > 1 {
		// Adding sizes to file names
		if btp != nil && btp.p.Episode == 0 {
			for _, c := range choices {
				c.DisplayName += " [COLOR lightyellow][" + humanize.Bytes(uint64(files[c.Index].Size)) + "][/COLOR]"
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

		var xbmcHost *xbmc.XBMCHost
		if btp != nil && btp.xbmcHost != nil {
			xbmcHost = btp.xbmcHost
		} else {
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

	st := t.GetLastStatus(true)

	if st == nil || st.Swigcptr() == 0 {
		return
	}

	fmt.Fprintf(w, "    Name:               %s \n", st.GetName())
	fmt.Fprintf(w, "    Title:              %s \n", t.Title())
	fmt.Fprintf(w, "    Infohash:           %s \n", hex.EncodeToString([]byte(st.GetInfoHash().ToString())))
	fmt.Fprintf(w, "    Status:             %s \n", xbmcHost.Translate(StatusStrings[st.GetState()]))
	fmt.Fprintf(w, "    Piece size:         %d \n", t.ti.NumPieces())
	fmt.Fprintf(w, "    Piece length:       %s \n", humanize.Bytes(uint64(t.ti.PieceLength())))
	fmt.Fprint(w, "\n")
	fmt.Fprint(w, "    Speed:\n")
	fmt.Fprintf(w, "        Download:   %s/s \n", humanize.Bytes(uint64(st.GetDownloadRate())))
	fmt.Fprintf(w, "        Upload:     %s/s \n", humanize.Bytes(uint64(st.GetUploadRate())))
	fmt.Fprint(w, "\n")
	fmt.Fprint(w, "    Size:\n")
	fmt.Fprintf(w, "        Total:                  %v \n", humanize.Bytes(uint64(t.ti.TotalSize())))
	fmt.Fprintf(w, "        Done:                   %v (%.2f%%) \n", humanize.Bytes(uint64(st.GetTotalDone())), 100*(float64(st.GetTotalDone())/float64(t.ti.TotalSize())))
	fmt.Fprintf(w, "        Selected:               %v (%.2f%%) \n", humanize.Bytes(uint64(t.GetSelectedSize())), 100*(float64(st.GetTotalDone())/float64(t.GetSelectedSize())))
	fmt.Fprintf(w, "        Wanted:                 %v \n", humanize.Bytes(uint64(st.GetTotalWanted())))
	fmt.Fprintf(w, "        Wanted done:            %v (%.2f%%) \n", humanize.Bytes(uint64(st.GetTotalWantedDone())), 100*(float64(st.GetTotalWantedDone())/float64(st.GetTotalWanted())))
	fmt.Fprint(w, "\n")
	fmt.Fprint(w, "    Connections:\n")
	fmt.Fprintf(w, "        Connections:            %d \n", st.GetNumConnections())
	fmt.Fprintf(w, "        Connected seeds:        %d \n", st.GetNumSeeds())
	fmt.Fprintf(w, "        Connected peers:        %d \n", st.GetNumPeers())
	fmt.Fprintf(w, "        NumComplete:            %d \n", st.GetNumComplete())
	fmt.Fprintf(w, "        NumIncomplete:          %d \n", st.GetNumIncomplete())
	fmt.Fprintf(w, "        ListSeeds:              %d \n", st.GetListSeeds())
	fmt.Fprintf(w, "        ListPeers:              %d \n", st.GetListPeers())

	fmt.Fprint(w, "    Flags:\n")
	fmt.Fprintf(w, "        paused: %v \n", st.GetPaused())
	fmt.Fprintf(w, "        auto_managed: %v \n", st.GetAutoManaged())
	fmt.Fprintf(w, "        sequential_download: %v \n", st.GetSequentialDownload())
	fmt.Fprintf(w, "        need_save_resume: %v \n", st.GetNeedSaveResume())
	fmt.Fprintf(w, "        is_seeding: %v \n", st.GetIsSeeding())
	fmt.Fprintf(w, "        is_finished: %v \n", st.GetIsFinished())
	fmt.Fprintf(w, "        has_metadata: %v \n", st.GetHasMetadata())
	fmt.Fprintf(w, "        has_incoming: %v \n", st.GetHasIncoming())
	fmt.Fprintf(w, "        moving_storage: %v \n", st.GetMovingStorage())
	fmt.Fprintf(w, "        announcing_to_trackers: %v \n", st.GetAnnouncingToTrackers())
	fmt.Fprintf(w, "        announcing_to_lsd: %v \n", st.GetAnnouncingToLsd())
	fmt.Fprintf(w, "        announcing_to_dht: %v \n", st.GetAnnouncingToDht())
	fmt.Fprint(w, "\n")

	fmt.Fprint(w, "    Files (Priority):\n")
	filePriorities := t.th.FilePriorities()
	for _, f := range slices.Sort(t.files, byPath).([]*File) {
		if pr := filePriorities.Get(f.Index); pr > 0 {
			fmt.Fprintf(w, "        %s (%s): %d \n", f.Path, humanize.Bytes(uint64(f.Size)), pr)
		} else {
			fmt.Fprintf(w, "        %s (%s): - \n", f.Path, humanize.Bytes(uint64(f.Size)))
		}
	}
	lt.DeleteStdVectorInt(filePriorities)

	fmt.Fprint(w, "\n")

	if showTrackers {
		fmt.Fprint(w, "    Libtorrent Trackers:\n")

		trackers := t.th.Trackers()
		trackersSize := trackers.Size()
		for i := 0; i < int(trackersSize); i++ {
			tracker := trackers.Get(i)
			fmt.Fprintf(w, "        %-60s: %-3s seeds, %-3s peers, updating: %-5v, is_working: %-5v, message: %s\n",
				tracker.GetUrl(),
				peerNumToString(tracker.GetScrapeComplete()),
				peerNumToString(tracker.GetScrapeIncomplete()),
				tracker.GetUpdating(),
				tracker.IsWorking(),
				tracker.GetMessage())
		}

		fmt.Fprint(w, "\n")
		fmt.Fprint(w, "    Invernal Trackers:\n")

		t.trackers.Range(func(t, p interface{}) bool {
			fmt.Fprintf(w, "        %-60s: %-3d peers\n", t, p)
			return true
		})
		fmt.Fprint(w, "\n")
	}

	if t.Closer.IsSet() || t.ti == nil || t.ti.Swigcptr() == 0 || t.th == nil || t.th.Swigcptr() == 0 {
		return
	}

	if showPieces {
		// TODO: Do we need pieces into?
		// Emulate piece status get to update pieces states
		t.hasPiece(0)

		piecesStatus := bytebufferpool.Get()
		piecesStatus.Reset()
		piecesStatus.WriteString("        ")
		for i := 0; i < t.ti.NumPieces(); i++ {
			if t.hasPiece(i) {
				piecesStatus.WriteString("+")
			} else if pr := t.th.PiecePriority(i).(int); pr == 0 {
				piecesStatus.WriteString("-")
			} else {
				piecesStatus.WriteString(strconv.Itoa(pr))
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

// GetLastStatus gets, or initially sets torrenthandle status
func (t *Torrent) GetLastStatus(isForced bool) lt.TorrentStatus {
	if t.Closer.IsSet() {
		return t.lastStatus
	}

	defer perf.ScopeTimer()()

	t.muStatus.Lock()
	defer t.muStatus.Unlock()

	if !isForced && t.lastStatus != nil && t.lastStatus.Swigcptr() != 0 && time.Since(t.lastStatusTime) < 1*time.Second {
		return t.lastStatus
	}

	if t.lastStatus != nil && t.lastStatus.Swigcptr() != 0 {
		lt.DeleteTorrentStatus(t.lastStatus)
		t.lastStatus = nil
	}

	t.lastStatus = t.GetStatus()
	t.lastStatusTime = time.Now()

	return t.lastStatus
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

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

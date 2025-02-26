package bittorrent

import (
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	lt "github.com/ElementumOrg/libtorrent-go"
	"github.com/anacrolix/missinggo/perf"

	"github.com/elgatito/elementum/config"
	"github.com/elgatito/elementum/util"
	"github.com/elgatito/elementum/util/event"
)

const (
	piecesRefreshDuration = 350 * time.Millisecond
)

// TorrentFS ...
type TorrentFS struct {
	http.Dir
	s      *Service
	isHead bool
}

// TorrentFSEntry ...
type TorrentFSEntry struct {
	http.File
	tfs *TorrentFS
	t   *Torrent
	f   *File

	totalLength int64
	pieceLength int
	numPieces   int

	seeked  event.Event
	removed event.Event

	id          int64
	readahead   int64
	storageType int

	lastUsed time.Time
	isActive bool
	isHead   bool
}

// PieceRange ...
type PieceRange struct {
	Begin, End int
}

// NewTorrentFS ...
func NewTorrentFS(service *Service, method string) *TorrentFS {
	return &TorrentFS{
		s:      service,
		Dir:    http.Dir(service.config.DownloadPath),
		isHead: method == "HEAD",
	}
}

// Open ...
func (tfs *TorrentFS) Open(uname string) (http.File, error) {
	// URL always comes with "/" as the separator, but while we compare it to file storage path,
	// we should use OS specific separator in the path. URL comes already decoded.
	name := util.ReconstructFileURL(uname)

	var file http.File
	var err error

	log.Infof("Opening %s", name)

	for _, t := range tfs.s.q.All() {
		for _, f := range t.files {
			if name[1:] == f.Path {
				log.Noticef("%s belongs to torrent %s", name, t.Name())

				if !t.IsMemoryStorage() {
					file, err = os.Open(filepath.Join(string(tfs.Dir), name))
					if err != nil {
						return nil, err
					}

					// make sure we don't open a file that's locked, as it can happen
					// on BSD systems (darwin included)
					if err := unlockFile(file.(*os.File)); err != nil {
						log.Errorf("Unable to unlock file because: %s", err)
					}
				}

				return NewTorrentFSEntry(file, tfs, t, f, name)
			}
		}
	}

	return file, fmt.Errorf("Could not open file: %s", name)
}

// NewTorrentFSEntry ...
func NewTorrentFSEntry(file http.File, tfs *TorrentFS, t *Torrent, f *File, name string) (*TorrentFSEntry, error) {
	if file == nil {
		file = NewMemoryFile(tfs, t.th.GetMemoryStorage(), f, name)
	}

	tf := &TorrentFSEntry{
		File: file,
		tfs:  tfs,
		t:    t,
		f:    f,

		totalLength: t.ti.TotalSize(),
		pieceLength: t.ti.PieceLength(),
		numPieces:   t.ti.NumPieces(),
		storageType: t.DownloadStorage,
		id:          time.Now().UTC().UnixNano(),

		lastUsed: time.Now(),
		isActive: true,
		isHead:   tfs.isHead,
	}
	go tf.consumeAlerts()

	t.muReaders.Lock()
	t.readers[tf.id] = tf
	t.muReaders.Unlock()

	t.ResetReaders()

	return tf, nil
}

func (tf *TorrentFSEntry) consumeAlerts() {
	alerts, done := tf.tfs.s.Alerts()
	defer close(done)
	pc := tf.t.Closer.C()

	for {
		select {
		case <-pc:
			log.Debugf("Stopping torrentfs alerts")
			return

		case alert, ok := <-alerts:
			if !ok { // was the alerts channel closed?
				return
			}

			switch alert.Type {
			case lt.TorrentRemovedAlertAlertType:
				removedAlert := lt.SwigcptrTorrentAlert(alert.Pointer)
				if tf.t.Closer.IsSet() || hex.EncodeToString([]byte(removedAlert.GetHandle().InfoHash().ToString())) == tf.t.infoHash {
					tf.removed.Set()
					return
				}
			}
		}
	}
}

// Close ...
func (tf *TorrentFSEntry) Close() error {
	defer perf.ScopeTimer()()

	log.Info("Closing file...")
	tf.removed.Set()

	if tf.tfs.s == nil || tf.t.Closer.IsSet() || tf.tfs.s.Closer.IsSet() {
		return nil
	}

	tf.t.muReaders.Lock()
	delete(tf.t.readers, tf.id)
	tf.t.muReaders.Unlock()

	defer tf.t.ResetReaders()
	return tf.File.Close()
}

// Read ...
func (tf *TorrentFSEntry) Read(data []byte) (n int, err error) {
	defer perf.ScopeTimer()()
	tf.SetActive(true)

	currentOffset, err := tf.File.Seek(0, io.SeekCurrent)
	if err != nil {
		log.Infof("Seek failed with error: %s", err)
		return 0, err
	}

	left := len(data)
	pos := 0
	piece, pieceOffset := tf.pieceFromOffset(currentOffset)

	for left > 0 && err == nil {
		size := left

		if err = tf.waitForPiece(piece); err != nil {
			log.Infof("Wait failed for piece %d: %s", piece, err)
			continue
		}

		if pieceOffset+size > tf.pieceLength {
			size = tf.pieceLength - pieceOffset
		}

		b := data[pos : pos+size]
		n1 := 0

		if tf.storageType == config.StorageMemory {
			n1, err = tf.File.(*MemoryFile).ReadPiece(b, piece, pieceOffset)
		} else {
			n1, err = tf.File.Read(b)
		}

		if err != nil {
			if err == io.ErrShortBuffer {
				log.Debugf("Retrying to fetch piece: %d", piece)
				err = nil
				time.Sleep(500 * time.Millisecond)
				continue
			}
			log.Infof("Read failed for piece %d: %s", piece, err)
			return
		} else if n1 > 0 {
			n += n1
			left -= n1
			pos += n1

			currentOffset += int64(n1)
			piece, pieceOffset = tf.pieceFromOffset(currentOffset)
			// if n1 < len(data) {
			// 	log.Debugf("Move slice: n1=%d, req=%d, current=%d, piece=%d, offset=%d", n1, len(b), currentOffset, piece, pieceOffset)
			// }
		} else {
			return
		}
	}

	return
}

// Seek ...
func (tf *TorrentFSEntry) Seek(offset int64, whence int) (int64, error) {
	defer perf.ScopeTimer()()
	tf.SetActive(true)

	seekingOffset := offset

	switch whence {
	case io.SeekStart:
		toUpdate := false
		tf.t.muReaders.Lock()
		for _, r := range tf.t.readers {
			if r.id == tf.id {
				continue
			}

			toUpdate = r.SetActive(false) || toUpdate
		}
		tf.t.muReaders.Unlock()

		if toUpdate {
			tf.t.ResetReaders()
		}

		tf.t.PrioritizePieces()
	case io.SeekCurrent:
		currentOffset, err := tf.File.Seek(0, io.SeekCurrent)
		if err != nil {
			return currentOffset, err
		}
		seekingOffset += currentOffset
	case io.SeekEnd:
		seekingOffset = tf.f.Size - offset
	}

	log.Infof("Seeking at %d... with %d", seekingOffset, whence)
	return tf.File.Seek(offset, whence)
}

func (tf *TorrentFSEntry) waitForPiece(piece int) error {
	if tf.t.hasPiece(piece) {
		return nil
	}

	defer perf.ScopeTimer()()
	log.Warningf("Waiting for piece %d", piece)
	now := time.Now()
	defer func() {
		log.Warningf("Waiting for piece %d finished in %s", piece, time.Since(now))
		tf.t.muAwaitingPieces.Lock()
		tf.t.awaitingPieces.Remove(uint32(piece))
		tf.t.muAwaitingPieces.Unlock()
	}()

	tf.t.PrioritizePiece(piece)

	pieceRefreshTicker := time.NewTicker(piecesRefreshDuration)
	defer pieceRefreshTicker.Stop()

	removed := tf.removed.C()
	seeked := tf.seeked.C()

	for !tf.t.hasPiece(piece) {
		select {
		case <-seeked:
			tf.seeked.Clear()

			log.Warningf("Unable to wait for piece %d as file was seeked", piece)
			return errors.New("File was seeked")
		case <-removed:
			log.Warningf("Unable to wait for piece %d as file was closed", piece)
			return errors.New("File was closed")
		case <-pieceRefreshTicker.C:
			continue
		}
	}

	return nil
}

func (tf *TorrentFSEntry) pieceFromOffset(offset int64) (int, int) {
	if tf.pieceLength == 0 {
		return 0, 0
	}

	piece := (tf.f.Offset + offset) / int64(tf.pieceLength)
	pieceOffset := (tf.f.Offset + offset) % int64(tf.pieceLength)

	if int(piece) > tf.t.pieceCount {
		piece = int64(tf.t.pieceCount)
	}

	return int(piece), int(pieceOffset)
}

// ReaderPiecesRange ...
func (tf *TorrentFSEntry) ReaderPiecesRange() (ret PieceRange) {
	pos, _ := tf.Pos()
	ra := tf.Readahead()

	return tf.byteRegionPieces(tf.torrentOffset(pos), ra)
}

// Readahead returns current reader readahead
func (tf *TorrentFSEntry) Readahead() int64 {
	ra := tf.readahead
	if ra < 1 {
		// Needs to be at least 1, because [x, x) means we don't want
		// anything.
		ra = 1
	}
	pos, _ := tf.Pos()
	if tf.f.Size > 0 && ra > tf.f.Size-pos {
		ra = tf.f.Size - pos
	}
	return ra
}

// Pos returns current file position
func (tf *TorrentFSEntry) Pos() (int64, error) {
	return tf.File.Seek(0, io.SeekCurrent)
}

func (tf *TorrentFSEntry) torrentOffset(readerPos int64) int64 {
	return tf.f.Offset + readerPos
}

// Returns the range of pieces [begin, end) that contains the extent of bytes.
func (tf *TorrentFSEntry) byteRegionPieces(off, size int64) (pr PieceRange) {
	if off >= tf.totalLength {
		return
	}
	if off < 0 {
		size += off
		off = 0
	}
	if size <= 0 {
		return
	}

	pl := int64(tf.pieceLength)
	pr.Begin = util.Max(0, int(off/pl))
	pr.End = util.Min(tf.numPieces-1, int((off+size-1)/pl))

	return
}

// IsIdle ...
func (tf *TorrentFSEntry) IsIdle() bool {
	return tf.lastUsed.Before(time.Now().Add(time.Minute * -1))
}

// IsHead ...
func (tf *TorrentFSEntry) IsHead() bool {
	return tf.isHead
}

// IsActive ...
func (tf *TorrentFSEntry) IsActive() bool {
	return tf.isActive
}

// SetActive ...
func (tf *TorrentFSEntry) SetActive(is bool) (res bool) {
	res = is != tf.isActive

	if is {
		tf.lastUsed = time.Now()
		tf.isActive = true
	} else {
		tf.isActive = false
	}

	return
}

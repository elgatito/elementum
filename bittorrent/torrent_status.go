package bittorrent

import (
	"bytes"
	"encoding/hex"
	"time"
	"unsafe"

	lt "github.com/ElementumOrg/libtorrent-go"
)

const statusRefreshDuration = 1 * time.Second

// TorrentStatus is a Go copy of libtorrent's torrent_status.
// It holds no libtorrent memory, so it is safe to read at any time, even after the torrent is removed.
type TorrentStatus struct {
	InfoHash string
	Name     string
	SavePath string
	State    int
	Progress float64

	Paused               bool
	AutoManaged          bool
	SequentialDownload   bool
	NeedSaveResume       bool
	IsSeeding            bool
	IsFinished           bool
	HasMetadata          bool
	HasIncoming          bool
	MovingStorage        bool
	AnnouncingToTrackers bool
	AnnouncingToLsd      bool
	AnnouncingToDht      bool

	DownloadRate        int
	UploadRate          int
	DownloadPayloadRate int
	UploadPayloadRate   int

	NumSeeds       int
	NumPeers       int
	NumComplete    int
	NumIncomplete  int
	ListSeeds      int
	ListPeers      int
	NumConnections int

	BlockSize       int
	TotalDone       int64
	TotalWanted     int64
	TotalWantedDone int64
	AllTimeDownload int64
	AllTimeUpload   int64

	ActiveTime   int
	FinishedTime int
	SeedingTime  int

	Pieces Bitfield

	UpdatedAt time.Time
}

func newTorrentStatus(ts lt.TorrentStatus) *TorrentStatus {
	st := &TorrentStatus{
		InfoHash: hex.EncodeToString([]byte(ts.GetInfoHash().ToString())),
		Name:     ts.GetName(),
		SavePath: ts.GetSavePath(),
		State:    int(ts.GetState()),
		Progress: float64(ts.GetProgress()),

		Paused:               ts.GetPaused(),
		AutoManaged:          ts.GetAutoManaged(),
		SequentialDownload:   ts.GetSequentialDownload(),
		NeedSaveResume:       ts.GetNeedSaveResume(),
		IsSeeding:            ts.GetIsSeeding(),
		IsFinished:           ts.GetIsFinished(),
		HasMetadata:          ts.GetHasMetadata(),
		HasIncoming:          ts.GetHasIncoming(),
		MovingStorage:        ts.GetMovingStorage(),
		AnnouncingToTrackers: ts.GetAnnouncingToTrackers(),
		AnnouncingToLsd:      ts.GetAnnouncingToLsd(),
		AnnouncingToDht:      ts.GetAnnouncingToDht(),

		DownloadRate:        ts.GetDownloadRate(),
		UploadRate:          ts.GetUploadRate(),
		DownloadPayloadRate: ts.GetDownloadPayloadRate(),
		UploadPayloadRate:   ts.GetUploadPayloadRate(),

		NumSeeds:       ts.GetNumSeeds(),
		NumPeers:       ts.GetNumPeers(),
		NumComplete:    ts.GetNumComplete(),
		NumIncomplete:  ts.GetNumIncomplete(),
		ListSeeds:      ts.GetListSeeds(),
		ListPeers:      ts.GetListPeers(),
		NumConnections: ts.GetNumConnections(),

		BlockSize:       ts.GetBlockSize(),
		TotalDone:       ts.GetTotalDone(),
		TotalWanted:     ts.GetTotalWanted(),
		TotalWantedDone: ts.GetTotalWantedDone(),
		AllTimeDownload: ts.GetAllTimeDownload(),
		AllTimeUpload:   ts.GetAllTimeUpload(),

		ActiveTime:   ts.GetActiveTime(),
		FinishedTime: ts.GetFinishedTime(),
		SeedingTime:  ts.GetSeedingTime(),

		UpdatedAt: time.Now(),
	}

	// Copy pieces out of libtorrent memory, it is freed together with the status object.
	piecesBits := ts.GetPieces()
	if size := piecesBits.Size(); size > 0 && piecesBits.Bytes() != 0 {
		// piecesBits.Bytes() is a raw pointer into libtorrent's C++ bitset, valid until the status object is freed.
		st.Pieces = Bitfield(bytes.Clone(unsafe.Slice((*byte)(unsafe.Pointer(piecesBits.Bytes())), (size+7)/8))) //nolint:govet // pointer comes from libtorrent, cloned immediately
	}

	return st
}

// withHandle runs f with the torrent handle only while the torrent is alive in the session.
// Drop() takes the write lock before removing the torrent, so it waits for running calls.
// f must not call withHandle again: a nested RLock deadlocks once Drop() is waiting.
func (t *Torrent) withHandle(f func(th lt.TorrentHandle)) (ok bool) {
	t.muHandle.RLock()
	defer t.muHandle.RUnlock()

	if t.Closer.IsSet() || t.th == nil || t.th.Swigcptr() == 0 || !t.th.IsValid() {
		return false
	}

	// libtorrent exceptions are turned into Go panics by SWIG
	defer func() {
		if r := recover(); r != nil {
			log.Errorf("Recovered libtorrent call for torrent %s: %v", t.infoHash, r)
			ok = false
		}
	}()

	f(t.th)
	return true
}

// UpdateStatus refreshes cached status from libtorrent, unless it is younger than maxAge.
func (t *Torrent) UpdateStatus(maxAge time.Duration) {
	t.muStatus.Lock()
	defer t.muStatus.Unlock()

	if time.Since(t.GetStatus().UpdatedAt) < maxAge {
		return
	}

	t.withHandle(func(th lt.TorrentHandle) {
		ts := th.Status()
		defer lt.DeleteTorrentStatus(ts)

		t.status.Store(newTorrentStatus(ts))
	})
}

// GetStatus returns latest cached status, it is never nil.
func (t *Torrent) GetStatus() *TorrentStatus {
	return t.status.Load()
}

func (s *Service) updateStatusLoop() {
	defer s.wg.Done()

	closing := s.Closer.C()
	ticker := time.NewTicker(statusRefreshDuration)
	defer ticker.Stop()

	for {
		select {
		case <-closing:
			return
		case <-ticker.C:
			for _, t := range s.q.All() {
				// Skip torrents refreshed recently on demand
				t.UpdateStatus(statusRefreshDuration / 2)
			}
		}
	}
}

package bittorrent

import (
	"testing"
	"time"

	lt "github.com/ElementumOrg/libtorrent-go"
	"github.com/anacrolix/sync"
)

func TestWithHandleRefusesWithoutLiveHandle(t *testing.T) {
	tor := &Torrent{muHandle: &sync.RWMutex{}}

	if tor.withHandle(func(lt.TorrentHandle) { t.Fatal("called without handle") }) {
		t.Fatal("expected false for missing handle")
	}

	if tor.GetMetadata() != nil {
		t.Fatal("expected no metadata without handle")
	}

	tor.Closer.Set()
	if tor.withHandle(func(lt.TorrentHandle) { t.Fatal("called on closed torrent") }) {
		t.Fatal("expected false for closed torrent")
	}
}

func TestHasFileChecksOnlyFilePieces(t *testing.T) {
	tor := &Torrent{muHandle: &sync.RWMutex{}, muStatus: &sync.Mutex{}}
	pieces := make(Bitfield, 2)
	for i := 0; i <= 7; i++ {
		pieces.SetBit(i, true)
	}
	tor.status.Store(&TorrentStatus{State: StatusFinished, Pieces: pieces, UpdatedAt: time.Now()})

	if !tor.hasFile(&File{PieceStart: 2, PieceEnd: 7}) {
		t.Fatal("expected downloaded file")
	}
	if tor.hasFile(&File{PieceStart: 7, PieceEnd: 8}) {
		t.Fatal("expected file with missing piece to be not downloaded")
	}
	if tor.hasFile(nil) {
		t.Fatal("expected false for nil file")
	}

	tor.status.Store(&TorrentStatus{State: StatusAllocating, Pieces: pieces, UpdatedAt: time.Now()})
	if tor.hasFile(&File{PieceStart: 2, PieceEnd: 7}) {
		t.Fatal("expected false while allocating")
	}
}

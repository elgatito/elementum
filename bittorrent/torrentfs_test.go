package bittorrent

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/RoaringBitmap/roaring/v2"
	"github.com/anacrolix/sync"
)

// A client that closes the connection stops the wait for a piece that does not come.
func TestWaitForPieceStopsWhenClientLeaves(t *testing.T) {
	tor := &Torrent{muHandle: &sync.RWMutex{}, muStatus: &sync.Mutex{}, muAwaitingPieces: &sync.RWMutex{}, awaitingPieces: roaring.NewBitmap()}
	tor.status.Store(&TorrentStatus{State: StatusDownloading, Pieces: make(Bitfield, 1), UpdatedAt: time.Now().Add(time.Hour)})

	ctx, cancel := context.WithCancel(context.Background())
	tf := &TorrentFSEntry{t: tor, tfs: &TorrentFS{ctx: ctx}}
	time.AfterFunc(50*time.Millisecond, cancel)

	done := make(chan error, 1)
	go func() { done <- tf.waitForPiece(0) }()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("err=%v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("still waiting after the client left")
	}
}

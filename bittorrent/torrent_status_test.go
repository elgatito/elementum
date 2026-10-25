package bittorrent

import (
	"testing"

	lt "github.com/ElementumOrg/libtorrent-go"
	"github.com/anacrolix/sync"
)

func TestWithHandleRefusesWithoutLiveHandle(t *testing.T) {
	tor := &Torrent{muHandle: &sync.RWMutex{}}

	if tor.withHandle(func(lt.TorrentHandle) { t.Fatal("called without handle") }) {
		t.Fatal("expected false for missing handle")
	}

	tor.Closer.Set()
	if tor.withHandle(func(lt.TorrentHandle) { t.Fatal("called on closed torrent") }) {
		t.Fatal("expected false for closed torrent")
	}
}

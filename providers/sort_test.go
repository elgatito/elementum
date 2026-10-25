package providers

import (
	"testing"

	"github.com/elgatito/elementum/bittorrent"
)

func TestMultiSorter(t *testing.T) {
	torrents := []*bittorrent.TorrentFile{
		{Name: "a", Resolution: bittorrent.Resolution720p, Seeds: 5},
		{Name: "b", Resolution: bittorrent.Resolution1080p, Seeds: 1},
		{Name: "c", Resolution: bittorrent.Resolution720p, Seeds: 50},
		{Name: "d", Resolution: bittorrent.Resolution1080p, Seeds: 10},
	}
	resolutionDown := func(c1, c2 *bittorrent.TorrentFile) bool { return c1.Resolution > c2.Resolution }
	seeds := func(c1, c2 *bittorrent.TorrentFile) bool { return c1.Seeds > c2.Seeds }

	SortBy(resolutionDown, seeds).Sort(torrents)

	got := ""
	for _, t := range torrents {
		got += t.Name
	}
	if got != "dbca" {
		t.Fatalf("got order %q, want %q", got, "dbca")
	}
}

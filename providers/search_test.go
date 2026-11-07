package providers

import (
	"testing"
	"time"

	"github.com/elgatito/elementum/bittorrent"
	"github.com/elgatito/elementum/tmdb"
)

type fakeEpisodeSearcher struct{ results []*bittorrent.TorrentFile }

func (f fakeEpisodeSearcher) SearchEpisodeLinks(*tmdb.Show, *tmdb.Season, *tmdb.Episode) []*bittorrent.TorrentFile {
	panic("the silent search must not call SearchEpisodeLinks")
}

func (f fakeEpisodeSearcher) SearchEpisodeLinksSilent(_ *tmdb.Show, _ *tmdb.Season, _ *tmdb.Episode, withAuth bool) []*bittorrent.TorrentFile {
	return f.results
}

// The silent search has no Kodi host and no progress dialog; it must neither touch Kodi nor block on progress updates.
func TestSearchEpisodeSilent(t *testing.T) {
	magnet := func(hash string, seeds int64, provider string) *bittorrent.TorrentFile {
		return &bittorrent.TorrentFile{URI: "magnet:?xt=urn:btih:" + hash, InfoHash: hash, Seeds: seeds, Provider: provider}
	}
	a := "0123456789abcdef0123456789abcdef01234567"
	b := "1123456789abcdef0123456789abcdef01234567"
	searchers := []EpisodeSearcher{
		fakeEpisodeSearcher{[]*bittorrent.TorrentFile{magnet(a, 5, "P1"), magnet(b, 50, "P1")}},
		fakeEpisodeSearcher{[]*bittorrent.TorrentFile{magnet(a, 7, "P2")}},
	}

	done := make(chan []*bittorrent.TorrentFile)
	go func() {
		done <- SearchEpisodeSilent(nil, searchers, &tmdb.Show{}, &tmdb.Season{}, &tmdb.Episode{}, true)
	}()

	var got []*bittorrent.TorrentFile
	select {
	case got = <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("SearchEpisodeSilent did not return")
	}

	// Default sorting is by seeds; the duplicate is merged and flagged Multi.
	if len(got) != 2 || got[0].InfoHash != b || got[1].InfoHash != a {
		t.Fatalf("unexpected results: %+v", got)
	}
	if got[0].Multi || !got[1].Multi || got[1].Seeds != 7 {
		t.Errorf("merge flags wrong: %+v %+v", got[0], got[1])
	}
}

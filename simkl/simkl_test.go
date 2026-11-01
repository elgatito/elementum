package simkl

import (
	"testing"
	"time"

	"github.com/goccy/go-json"

	"github.com/elgatito/elementum/tracker"
)

// Shapes from the Simkl /sync/all-items reference: ids as strings or numbers, anime wrapped in show, nulls.
const allItemsJSON = `{
  "shows": [{
    "last_watched_at": "2026-05-15T00:35:15Z", "status": "watching",
    "show": {"title": "The Walking Dead", "year": 2010, "ids": {"simkl": 2090, "imdb": "tt1520211", "tvdb": "153021", "tmdb": "1402"}},
    "seasons": [{"number": 1, "episodes": [{"number": 2, "watched_at": "2026-05-15T00:32:20Z"}, {"number": 3, "watched_at": null}]}]
  }],
  "anime": [{"status": "completed", "anime_type": "tv", "show": {"title": "Cowboy Bebop", "ids": {"simkl": 37089, "mal": "1"}}}],
  "movies": [
    {"added_to_watchlist_at": "2026-04-10T20:13:02Z", "last_watched_at": null, "status": "plantowatch",
     "movie": {"title": "The Godfather", "year": 1972, "ids": {"simkl": 53434, "imdb": "tt0068646", "tmdb": "238"}}},
    {"status": "completed", "movie": {"title": "No id", "ids": {"slug": "x"}}}
  ]
}`

func TestDecodeAllItems(t *testing.T) {
	items, err := decodeAllItems([]byte(allItemsJSON))
	if err != nil {
		t.Fatal(err)
	}
	entries := items.entries()
	if len(entries) != 3 {
		t.Fatalf("got %d entries, want 3 (title without simkl id skipped)", len(entries))
	}

	godfather := entries["53434"]
	if godfather == nil || !godfather.IsMovie || godfather.Status != statusPlanToWatch || godfather.Item.IDs.TMDB != 238 || godfather.Item.IDs.Native != "53434" {
		t.Errorf("bad movie entry: %+v", godfather)
	}

	twd := entries["2090"]
	if twd == nil || twd.IsMovie || twd.Item.IDs.TVDB != 153021 || len(twd.Episodes) != 2 {
		t.Fatalf("bad show entry: %+v", twd)
	}
	// an episode without its own date takes the show's last watch
	if !twd.Episodes[1].LastWatchedAt.Equal(twd.LastWatchedAt) || twd.Episodes[1].Plays != 1 {
		t.Errorf("episode without date = %+v", twd.Episodes[1])
	}
	if !entries["37089"].matches(tracker.ShowType) {
		t.Error("anime must be a show")
	}

	for _, empty := range []string{"", "[]", "null"} {
		if items, err := decodeAllItems([]byte(empty)); err != nil || len(items.entries()) != 0 {
			t.Errorf("decodeAllItems(%q) = %v, %v; want empty", empty, items, err)
		}
	}
}

func TestActivitiesBuckets(t *testing.T) {
	var a activities
	if err := json.Unmarshal([]byte(`{"all": "2026-05-14T07:12:20Z",
		"tv_shows": {"watching": "2026-05-04T19:40:58Z", "plantowatch": null, "removed_from_list": "2026-05-10T00:00:00Z"},
		"movies": {"completed": "2026-05-14T06:43:15Z", "playback": "2026-05-14T06:50:38Z"},
		"custom_lists": {"lists": {"all": "2026-05-14T07:12:20Z"}}}`), &a); err != nil {
		t.Fatal(err)
	}

	b := a.buckets().Buckets
	removed := time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC)
	if !b[tracker.EpisodesWatched].Equal(removed) || !b[tracker.ShowsWatchlisted].Equal(removed) {
		t.Errorf("removals must move show buckets: %v", b)
	}
	if b[tracker.MoviesPaused].IsZero() || b[tracker.ListsUpdated].IsZero() || b[tracker.MoviesWatched].IsZero() {
		t.Errorf("missing buckets: %v", b)
	}
	if a.removedMark() == "||" {
		t.Error("removedMark must reflect removed_from_list")
	}
}

func TestHistoryBody(t *testing.T) {
	day := time.Date(2026, 5, 1, 20, 0, 0, 0, time.UTC)
	ep := func(s, e int) *tracker.WatchedItem {
		return &tracker.WatchedItem{Item: tracker.Item{Type: tracker.EpisodeType, IDs: tracker.IDs{TMDB: 1402}, Season: s, Episode: e}}
	}
	body := historyBody([]*tracker.WatchedItem{
		ep(1, 1), ep(1, 2), ep(2, 1),
		{Item: tracker.Item{Type: tracker.MovieType, IDs: tracker.IDs{TMDB: 238, Native: "53434"}}, LastWatchedAt: day},
	})

	b, _ := json.Marshal(body)
	want := `{"movies":[{"ids":{"simkl":53434,"tmdb":"238"},"watched_at":"2026-05-01T20:00:00Z"}],` +
		`"shows":[{"ids":{"tmdb":"1402"},"seasons":[{"number":1,"episodes":[{"number":1},{"number":2}]},{"number":2,"episodes":[{"number":1}]}]}]}`
	if string(b) != want {
		t.Errorf("historyBody =\n%s\nwant\n%s", b, want)
	}
}

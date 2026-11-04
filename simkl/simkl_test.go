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
    "last_watched_at": "2026-05-15T00:35:15Z", "status": "watching", "next_to_watch": "S01E04",
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
	if twd.NextSeason != 1 || twd.NextEpisode != 4 {
		t.Errorf("next_to_watch = S%02dE%02d, want S01E04", twd.NextSeason, twd.NextEpisode)
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

func TestParseNextToWatch(t *testing.T) {
	for in, want := range map[string][2]int{"S08E03": {8, 3}, "S01E12": {1, 12}, "E05": {1, 5}, "": {0, 0}, "null": {0, 0}, "S1": {0, 0}} {
		if s, e := parseNextToWatch(in); s != want[0] || e != want[1] {
			t.Errorf("parseNextToWatch(%q) = %d, %d; want %v", in, s, e, want)
		}
	}
}

func TestListErrorPremiumOnly(t *testing.T) {
	// non-PRO accounts get HTTP 200 with an error body instead of lists
	var resp struct {
		Error string `json:"error"`
		Lists []any  `json:"lists"`
	}
	if err := json.Unmarshal([]byte(`{"error": "premium_only", "message": "Custom list items are available to Simkl PRO and VIP members only."}`), &resp); err != nil {
		t.Fatal(err)
	}
	if err := listError(nil, resp.Error); err != tracker.ErrPaidAccount {
		t.Errorf("listError = %v, want ErrPaidAccount", err)
	}
	if err := listError(nil, ""); err != nil {
		t.Errorf("listError on a list body = %v, want nil", err)
	}
}

func TestProgress(t *testing.T) {
	day := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	show := func(id int, status string, next string, watched time.Time) *entry {
		e := &entry{Status: status, LastWatchedAt: watched, Item: tracker.Item{Type: tracker.ShowType, IDs: tracker.IDs{TMDB: id}}}
		e.NextSeason, e.NextEpisode = parseNextToWatch(next)
		return e
	}
	lib := &library{Entries: map[string]*entry{
		"1": show(1, "watching", "S01E02", day),
		"2": show(2, "watching", "S03E01", day.Add(time.Hour)),
		"3": show(3, "watching", "", day),        // caught up
		"4": show(4, "hold", "S01E01", day),      // not in Watching
		"5": {IsMovie: true, Status: "watching"}, // movies have no progress
	}}

	got := progress(lib)
	if len(got) != 2 || got[0].IDs.TMDB != 2 || got[1].IDs.TMDB != 1 {
		t.Fatalf("progress = %+v, want shows 2 then 1", got)
	}
	if got[0].Type != tracker.EpisodeType || got[0].Season != 3 || got[0].Episode != 1 {
		t.Errorf("next episode = %+v, want S03E01", got[0])
	}
}

func TestStatusItems(t *testing.T) {
	day := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	item := func(id int, isMovie bool, status, title string, watched time.Time) *entry {
		return &entry{IsMovie: isMovie, Status: status, LastWatchedAt: watched, Item: tracker.Item{IDs: tracker.IDs{TMDB: id}, Title: title}}
	}
	lib := &library{Entries: map[string]*entry{
		"1": item(1, false, "hold", "B", day),
		"2": item(2, false, "hold", "A", day),
		"3": item(3, false, "hold", "C", day.Add(time.Hour)),
		"4": item(4, false, "dropped", "D", day),
		"5": item(5, true, "hold", "E", day),
	}}

	got := statusItems(lib, "hold", tracker.ShowType)
	if len(got) != 3 || got[0].IDs.TMDB != 3 || got[1].IDs.TMDB != 2 || got[2].IDs.TMDB != 1 {
		t.Fatalf("statusItems = %+v, want shows 3, 2, 1", got)
	}
	if got := statusItems(lib, "hold", tracker.MovieType); len(got) != 1 || got[0].IDs.TMDB != 5 {
		t.Errorf("movies = %+v, want movie 5", got)
	}
	if got := statusItems(lib, "", tracker.ShowType); len(got) != 4 {
		t.Errorf("all shows = %+v, want 4 shows in any status", got)
	}
}

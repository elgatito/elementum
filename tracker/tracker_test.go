package tracker

import (
	"testing"
	"time"

	"github.com/elgatito/elementum/cache"
)

func movie(tmdb int) *Item { return &Item{Type: MovieType, IDs: IDs{TMDB: tmdb}} }

func TestItemKey(t *testing.T) {
	for _, c := range []struct {
		item *Item
		want string
	}{
		{&Item{Type: MovieType, IDs: IDs{TMDB: 1, IMDB: "tt1"}}, "0:tmdb:1"},
		{&Item{Type: ShowType, IDs: IDs{IMDB: "tt2", TVDB: 3}}, "1:imdb:tt2"},
		{&Item{Type: ShowType, IDs: IDs{TVDB: 3}}, "1:tvdb:3"},
		{&Item{Type: EpisodeType, IDs: IDs{TMDB: 5}, Season: 2, Episode: 7}, "3:tmdb:5:2:7"},
	} {
		if got := c.item.Key(); got != c.want {
			t.Errorf("Key() = %q, want %q", got, c.want)
		}
	}
}

func TestDiffItems(t *testing.T) {
	got := DiffItems([]*Item{movie(1), movie(2)}, []*Item{movie(2), movie(3), nil})
	if len(got) != 1 || got[0].IDs.TMDB != 3 {
		t.Fatalf("DiffItems = %+v, want only movie 3", got)
	}
}

func TestDiffWatchedMovies(t *testing.T) {
	day := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	base := []*WatchedItem{{Item: *movie(1), LastWatchedAt: day}}
	rewatched := []*WatchedItem{{Item: *movie(1), LastWatchedAt: day.Add(time.Hour)}, {Item: *movie(2)}}

	if got := DiffWatchedMovies(base, rewatched, true); len(got) != 2 {
		t.Errorf("with date check got %d movies, want 2 (rewatch and new)", len(got))
	}
	if got := DiffWatchedMovies(base, rewatched, false); len(got) != 1 || got[0].IDs.TMDB != 2 {
		t.Errorf("without date check got %+v, want only movie 2", got)
	}
}

func TestDiffWatchedShows(t *testing.T) {
	show := func(tmdb int, eps ...[2]int) *WatchedItem {
		w := &WatchedItem{Item: Item{Type: ShowType, IDs: IDs{TMDB: tmdb}}}
		for _, e := range eps {
			w.Episodes = append(w.Episodes, WatchedEpisode{Season: e[0], Number: e[1], Plays: 1})
		}
		return w
	}

	if got := DiffWatchedShows(nil, []*WatchedItem{show(1, [2]int{1, 1})}); got != nil {
		t.Errorf("without base got %+v, want nothing", got)
	}

	base := []*WatchedItem{show(1, [2]int{1, 1})}
	other := []*WatchedItem{show(1, [2]int{1, 1}, [2]int{1, 2}), show(2, [2]int{1, 1})}
	got := DiffWatchedShows(base, other)
	if len(got) != 2 {
		t.Fatalf("got %d shows, want 2", len(got))
	}
	if len(got[0].Episodes) != 1 || got[0].Episodes[0].Number != 2 {
		t.Errorf("partial show episodes = %+v, want only S01E02", got[0].Episodes)
	}
	if got[1].IDs.TMDB != 2 || len(got[1].Episodes) != 1 {
		t.Errorf("new show = %+v, want show 2 with its episode", got[1])
	}
}

func TestActivityStateChanged(t *testing.T) {
	day := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	prev, cur := NewActivities(), NewActivities()
	prev.Set(day, MoviesWatched, ShowsWatchlisted)
	cur.Set(day, MoviesWatched)
	cur.Set(day.Add(time.Minute), ShowsWatchlisted)

	s := &ActivityState{Previous: prev, Current: cur}
	if s.Changed(MoviesWatched) {
		t.Error("unchanged bucket reported as changed")
	}
	if !s.Changed(ShowsWatchlisted) || !s.Any() {
		t.Error("moved bucket not reported")
	}
	if !(&ActivityState{Previous: NewActivities(), Current: cur}).Changed(MoviesWatched) {
		t.Error("without previous state every bucket must count as changed")
	}
	if !(&ActivityState{Previous: prev}).Changed(MoviesWatched) {
		t.Error("failed activities fetch must count as changed")
	}
}

func TestSnapshotCacheEncoding(t *testing.T) {
	day := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	in := []*WatchedItem{{Item: Item{Type: ShowType, IDs: IDs{TMDB: 1, Native: "x"}}, Episodes: []WatchedEpisode{{Season: 1, Number: 2, Plays: 1, LastWatchedAt: day}}}}

	b, err := cache.MarshalMsg(nil, in)
	if err != nil {
		t.Fatal(err)
	}
	var out []*WatchedItem
	if _, err := cache.UnmarshalMsg(b, &out); err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 || out[0].Key() != in[0].Key() || !out[0].Episodes[0].LastWatchedAt.Equal(day) {
		t.Fatalf("round trip = %+v", out)
	}

	acts := NewActivities()
	acts.Set(day, MoviesWatched)
	b, _ = cache.MarshalMsg(nil, acts)
	got := NewActivities()
	if _, err := cache.UnmarshalMsg(b, got); err != nil || !got.Buckets[MoviesWatched].Equal(day) {
		t.Fatalf("activities round trip = %+v, %v", got, err)
	}
}

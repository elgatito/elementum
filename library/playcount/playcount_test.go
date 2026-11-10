package playcount

import "testing"

func TestKnownWatchedState(t *testing.T) {
	old := Watched
	Watched = map[uint64]WatchedState{}
	t.Cleanup(func() { Watched = old })
	for _, c := range []struct {
		kind int
		ids  []int
		get  func() WatchedState
	}{
		{MovieType, []int{42}, func() WatchedState { return GetWatchedMovieByTMDB(42) }},
		{ShowType, []int{42}, func() WatchedState { return GetWatchedShowByTMDB(42) }},
		{SeasonType, []int{42, 0}, func() WatchedState { return GetWatchedSeasonByTMDB(42, 0) }},
		{EpisodeType, []int{42, 0, 1}, func() WatchedState { return GetWatchedEpisodeByTMDB(42, 0, 1) }},
	} {
		if Known(c.kind, c.ids...) || bool(c.get()) {
			t.Fatalf("type %d is known before mutation", c.kind)
		}
		for _, watched := range []bool{true, false} {
			Set(c.kind, watched, c.ids...)
			if !Known(c.kind, c.ids...) || bool(c.get()) != watched {
				t.Errorf("type %d: watched=%v, known=%v, want %v", c.kind, c.get(), Known(c.kind, c.ids...), watched)
			}
		}
	}
}

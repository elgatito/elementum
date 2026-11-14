package library

import (
	"testing"
	"time"

	"github.com/elgatito/elementum/config"
	"github.com/elgatito/elementum/tracker"
)

type fakeTracker struct {
	adds, removes []call
}

type call struct {
	loc  tracker.Location
	list string
	item tracker.Item
}

func (f *fakeTracker) ID() string                                   { return "faketest" }
func (f *fakeTracker) Name() string                                 { return "Fake" }
func (f *fakeTracker) MinSyncInterval() time.Duration               { return time.Minute }
func (f *fakeTracker) Authorized() bool                             { return true }
func (f *fakeTracker) Authorize() error                             { return nil }
func (f *fakeTracker) Deauthorize() error                           { return nil }
func (f *fakeTracker) RefreshToken() error                          { return nil }
func (f *fakeTracker) LastActivities() (*tracker.Activities, error) { return nil, nil }
func (f *fakeTracker) Watchlist(tracker.MediaType, bool) ([]*tracker.Item, error) {
	return nil, nil
}
func (f *fakeTracker) Watched(tracker.MediaType, bool) ([]*tracker.WatchedItem, error) {
	return nil, nil
}
func (f *fakeTracker) Add(l tracker.Location, list string, it []*tracker.Item) error {
	f.adds = append(f.adds, call{l, list, *it[0]})
	return nil
}
func (f *fakeTracker) Remove(l tracker.Location, list string, it []*tracker.Item) error {
	f.removes = append(f.removes, call{l, list, *it[0]})
	return nil
}
func (f *fakeTracker) SetWatched([]*tracker.WatchedItem, bool) error { return nil }
func (f *fakeTracker) Scrobble(tracker.ScrobbleAction, *tracker.Item, float64) error {
	return nil
}

func TestSyncTracker(t *testing.T) {
	f := &fakeTracker{}
	tracker.Register(f)
	c := config.Get()
	if c == nil {
		c = &config.Configuration{}
	}
	saved := *c
	defer func() { *c = saved }()

	tests := []struct {
		name      string
		mediaType int
		added     bool
		wantAdd   bool
		wantType  tracker.MediaType
		wantLoc   tracker.Location
		wantList  string
	}{
		{"movie added", MovieType, true, true, tracker.MovieType, tracker.LocationList, "11"},
		{"movie removed", MovieType, false, false, tracker.MovieType, tracker.LocationWatchlist, "12"},
		{"show added", ShowType, true, true, tracker.ShowType, tracker.LocationCollection, "13"},
		{"show removed", ShowType, false, false, tracker.ShowType, tracker.LocationList, "14"},
	}
	for _, tc := range tests {
		for _, enabled := range []bool{true, false} {
			*c = saved
			c.TrackerProvider = "faketest"
			// every triple differs, so a wrong pick shows; only the wanted flag is switched on
			c.TraktSyncAddedMoviesLocation, c.TraktSyncAddedMoviesList = int(tracker.LocationList), 11
			c.TraktSyncRemovedMoviesLocation, c.TraktSyncRemovedMoviesList = int(tracker.LocationWatchlist), 12
			c.TraktSyncAddedShowsLocation, c.TraktSyncAddedShowsList = int(tracker.LocationCollection), 13
			c.TraktSyncRemovedShowsLocation, c.TraktSyncRemovedShowsList = int(tracker.LocationList), 14
			switch {
			case tc.mediaType == MovieType && tc.added:
				c.TraktSyncAddedMovies = enabled
			case tc.mediaType == MovieType:
				c.TraktSyncRemovedMovies = enabled
			case tc.added:
				c.TraktSyncAddedShows = enabled
			default:
				c.TraktSyncRemovedShows = enabled
			}
			*f = fakeTracker{}
			SyncTracker(tc.mediaType, 42, tc.added)
			got := append(f.adds, f.removes...)
			if !enabled {
				if len(got) != 0 {
					t.Errorf("%s disabled: got calls %v", tc.name, got)
				}
				continue
			}
			if len(got) != 1 || (len(f.adds) == 1) != tc.wantAdd {
				t.Fatalf("%s: calls adds=%v removes=%v", tc.name, f.adds, f.removes)
			}
			g := got[0]
			if g.item.Type != tc.wantType || g.item.IDs.TMDB != 42 || g.loc != tc.wantLoc || g.list != tc.wantList {
				t.Errorf("%s: got %+v", tc.name, g)
			}
		}
	}
}

package uid

import (
	"testing"

	"github.com/elgatito/elementum/config"
	"github.com/elgatito/elementum/tracker"
)

func TestResumeOrTracker(t *testing.T) {
	config.Get().TraktSyncPlaybackProgress = true
	defer func() { config.Get().TraktSyncPlaybackProgress = false }()

	SetTrackerPaused(tracker.MovieType, []*tracker.PausedItem{
		{Item: tracker.Item{IDs: tracker.IDs{TMDB: 1}}, Progress: 50, Runtime: 6000},
		{Item: tracker.Item{IDs: tracker.IDs{TMDB: 2}}, Progress: 25}, // no runtime, like Simkl
	})
	SetTrackerPaused(tracker.EpisodeType, []*tracker.PausedItem{
		{Item: tracker.Item{IDs: tracker.IDs{TMDB: 1}, Season: 2, Episode: 3}, Progress: 10, Runtime: 3000},
	})

	if r := MovieResumeOrTracker(nil, 1, 0); r == nil || r.Position != 3000 || r.Total != 6000 {
		t.Errorf("movie with tracker runtime: %+v", r)
	}
	if r := MovieResumeOrTracker(nil, 2, 4000); r == nil || r.Position != 1000 {
		t.Errorf("movie with given total: %+v", r)
	}
	if r := MovieResumeOrTracker(nil, 2, 0); r != nil {
		t.Errorf("movie without any runtime: %+v", r)
	}
	if r := EpisodeResumeOrTracker(nil, 1, 2, 3, 0); r == nil || r.Position != 300 {
		t.Errorf("episode: %+v", r)
	}
	if r := EpisodeResumeOrTracker(nil, 1, 2, 4, 0); r != nil {
		t.Errorf("other episode of the show: %+v", r)
	}

	// Kodi library position wins, empty library resume does not
	lib := &Resume{Position: 100, Total: 200}
	if r := MovieResumeOrTracker(lib, 1, 0); r != lib {
		t.Errorf("library resume replaced: %+v", r)
	}
	if r := MovieResumeOrTracker(&Resume{}, 1, 0); r == nil || r.Position != 3000 {
		t.Errorf("empty library resume kept: %+v", r)
	}

	// New sync replaces old progress
	SetTrackerPaused(tracker.MovieType, nil)
	if r := MovieResumeOrTracker(nil, 1, 0); r != nil {
		t.Errorf("stale progress: %+v", r)
	}
	SetTrackerPaused(tracker.EpisodeType, nil)
}

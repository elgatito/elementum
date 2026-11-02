package uid

import (
	"fmt"
	"math"
	"strconv"
	"sync"
	"time"

	"github.com/elgatito/elementum/config"
	"github.com/elgatito/elementum/tracker"
	"github.com/elgatito/elementum/xbmc"
)

// ToString ...
func (r *Resume) ToString() string {
	if r.Position == 0 {
		return ""
	}

	t1 := time.Now()
	t2 := t1.Add(time.Duration(int(r.Position)) * time.Second)

	diff := t2.Sub(t1)
	return fmt.Sprintf("%d:%02d:%02d", int(diff.Hours()), int(math.Mod(diff.Minutes(), 60)), int(math.Mod(diff.Seconds(), 60)))
}

// Reset ...
func (r *Resume) Reset() {
	log.Debugf("Resetting stored resume position")
	r.Position = 0
	r.Total = 0
}

// GetMovieResume returns Resume info for kodi id
func GetMovieResume(kodiID int) *Resume {
	mu := l.GetMutex(MoviesMutex)
	mu.RLock()
	defer mu.RUnlock()

	for _, m := range l.Movies {
		if m.UIDs.Kodi == kodiID {
			return m.Resume
		}
	}

	return nil
}

// GetEpisodeResume returns Resume info for kodi id
func GetEpisodeResume(kodiID int) *Resume {
	mu := l.GetMutex(ShowsMutex)
	mu.RLock()
	defer mu.RUnlock()

	for _, existingShow := range l.Shows {
		for _, existingEpisode := range existingShow.Episodes {
			if existingEpisode.UIDs.Kodi == kodiID {
				return existingEpisode.Resume
			}
		}
	}

	return nil
}

var (
	trackerPausedMu sync.RWMutex
	// trackerPaused keeps playback progress from the active tracker, so items outside of Kodi library can be resumed
	trackerPaused = map[tracker.MediaType]map[string]*tracker.PausedItem{}
)

func pausedKey(tmdbID, season, episode int) string {
	return fmt.Sprintf("%d_%d_%d", tmdbID, season, episode)
}

// SetTrackerPaused replaces tracker playback progress of given media type.
// Episodes are identified by show TMDB ID, season and episode numbers.
func SetTrackerPaused(t tracker.MediaType, items []*tracker.PausedItem) {
	m := make(map[string]*tracker.PausedItem, len(items))
	for _, i := range items {
		if i != nil && i.IDs.TMDB != 0 && i.Progress > 0 {
			m[pausedKey(i.IDs.TMDB, i.Season, i.Episode)] = i
		}
	}

	trackerPausedMu.Lock()
	defer trackerPausedMu.Unlock()
	trackerPaused[t] = m
}

// MovieResumeOrTracker returns resume, if it has a position, otherwise tracker playback progress of the movie, if any.
// total (seconds) is used when tracker has no runtime.
func MovieResumeOrTracker(resume *Resume, tmdbID, total int) *Resume {
	return resumeOrTracker(resume, tracker.MovieType, pausedKey(tmdbID, 0, 0), total)
}

// EpisodeResumeOrTracker is MovieResumeOrTracker for an episode of a show
func EpisodeResumeOrTracker(resume *Resume, showID, season, episode, total int) *Resume {
	return resumeOrTracker(resume, tracker.EpisodeType, pausedKey(showID, season, episode), total)
}

func resumeOrTracker(resume *Resume, t tracker.MediaType, key string, total int) *Resume {
	if (resume != nil && resume.Position > 0) || !config.Get().TraktSyncPlaybackProgress {
		return resume
	}

	trackerPausedMu.RLock()
	i := trackerPaused[t][key]
	trackerPausedMu.RUnlock()

	if i == nil {
		return resume
	}
	if i.Runtime > 0 {
		total = i.Runtime
	}
	if total <= 0 {
		return resume
	}
	// A new struct, as player updates resume it gets
	return &Resume{Position: float64(total) * i.Progress / 100, Total: float64(total)}
}

// SetProperties sets ListItem resume properties
func (r *Resume) SetProperties(p *xbmc.ListItemProperties) {
	if r == nil || p == nil {
		return
	}
	p.ResumeTime = strconv.FormatFloat(r.Position, 'f', 6, 64)
	p.TotalTime = strconv.FormatFloat(r.Total, 'f', 6, 64)
}

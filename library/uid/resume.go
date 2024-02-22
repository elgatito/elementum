package uid

import (
	"fmt"
	"math"
	"time"
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

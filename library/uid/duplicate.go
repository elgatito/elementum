package uid

import (
	"strconv"

	"github.com/anacrolix/missinggo/perf"
)

//
// Duplicate handling
//

// IsDuplicateMovie checks if movie exists in the library
func IsDuplicateMovie(tmdbID string) bool {
	l.Mu.UIDs.Lock()
	defer l.Mu.UIDs.Unlock()
	defer perf.ScopeTimer()()

	query, _ := strconv.Atoi(tmdbID)
	for _, u := range l.UIDs {
		if u.TMDB != 0 && u.MediaType == MovieType && u.TMDB == query {
			return true
		}
	}

	return false
}

// IsDuplicateShow checks if show exists in the library
func IsDuplicateShow(tmdbID string) bool {
	defer perf.ScopeTimer()()

	l.Mu.UIDs.Lock()
	defer l.Mu.UIDs.Unlock()

	query, _ := strconv.Atoi(tmdbID)
	for _, u := range l.UIDs {
		if u.TMDB != 0 && u.MediaType == ShowType && u.TMDB == query {
			return true
		}
	}

	return false
}

// IsDuplicateShowByInt checks if show exists in the library
func IsDuplicateShowByInt(tmdbID int) bool {
	defer perf.ScopeTimer()()

	l.Mu.UIDs.Lock()
	defer l.Mu.UIDs.Unlock()

	for _, u := range l.UIDs {
		if u.TMDB != 0 && u.MediaType == ShowType && u.TMDB == tmdbID {
			return true
		}
	}

	return false
}

// IsDuplicateEpisode checks if episode exists in the library
func IsDuplicateEpisode(tmdbShowID int, seasonNumber int, episodeNumber int) bool {
	l.Mu.Shows.RLock()
	defer l.Mu.Shows.RUnlock()
	defer perf.ScopeTimer()()

	for _, s := range l.Shows {
		if tmdbShowID != s.UIDs.TMDB {
			continue
		}

		for _, e := range s.Episodes {
			if e.Season == seasonNumber && e.Episode == episodeNumber {
				return true
			}
		}
	}

	return false
}

// IsAddedToLibrary checks if specific TMDB exists in the library
func IsAddedToLibrary(id string, mediaType int) (isAdded bool) {
	defer perf.ScopeTimer()()

	if mediaType == MovieType {
		return IsDuplicateMovie(id)
	} else if mediaType == ShowType {
		return IsDuplicateShow(id)
	}

	return false
}

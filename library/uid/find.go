package uid

import (
	"errors"
)

//
// Library searchers
//

// findFirst returns the first non-nil item matching match, or an error if none matches.
func findFirst[T any](items []*T, match func(*T) bool) (*T, error) {
	for _, item := range items {
		if item != nil && match(item) {
			return item, nil
		}
	}

	return nil, errors.New("Not found")
}

// GetLibraryMovie finds Movie from library
func GetLibraryMovie(kodiID int) *Movie {
	mu := l.GetMutex(MoviesMutex)
	mu.RLock()
	defer mu.RUnlock()

	movie, _ := findFirst(l.Movies, func(m *Movie) bool { return m.UIDs.Kodi == kodiID })
	return movie
}

// GetLibraryShow finds Show from library
func GetLibraryShow(kodiID int) *Show {
	mu := l.GetMutex(ShowsMutex)
	mu.RLock()
	defer mu.RUnlock()

	show, _ := findFirst(l.Shows, func(s *Show) bool { return s.UIDs.Kodi == kodiID })
	return show
}

// GetLibrarySeason finds Show/Season from library
func GetLibrarySeason(kodiID int) (*Show, *Season) {
	mu := l.GetMutex(ShowsMutex)
	mu.RLock()
	defer mu.RUnlock()

	for _, s := range l.Shows {
		for _, se := range s.Seasons {
			if se.UIDs.Kodi == kodiID {
				return s, se
			}
		}
	}

	return nil, nil
}

// GetLibraryEpisode finds Show/Episode from library
func GetLibraryEpisode(kodiID int) (*Show, *Episode) {
	mu := l.GetMutex(ShowsMutex)
	mu.RLock()
	defer mu.RUnlock()

	for _, s := range l.Shows {
		for _, e := range s.Episodes {
			if e.UIDs.Kodi == kodiID {
				return s, e
			}
		}
	}

	return nil, nil
}

// GetMovieByTMDB ...
func GetMovieByTMDB(id int) (*Movie, error) {
	mu := l.GetMutex(MoviesMutex)
	mu.RLock()
	defer mu.RUnlock()

	return findFirst(l.Movies, func(m *Movie) bool { return m.UIDs.TMDB == id })
}

// GetMovieByIMDB ...
func GetMovieByIMDB(id string) (*Movie, error) {
	mu := l.GetMutex(MoviesMutex)
	mu.RLock()
	defer mu.RUnlock()

	return findFirst(l.Movies, func(m *Movie) bool { return m.UIDs.IMDB == id })
}

// GetShowByTMDB ...
func GetShowByTMDB(id int) (*Show, error) {
	mu := l.GetMutex(ShowsMutex)
	mu.RLock()
	defer mu.RUnlock()

	return findFirst(l.Shows, func(s *Show) bool { return s.UIDs.TMDB == id })
}

// FindShowByKodi ...
func FindShowByKodi(id int) (*Show, error) {
	return findFirst(l.Shows, func(s *Show) bool { return s.UIDs.Kodi == id })
}

// FindShowByTMDB ...
func FindShowByTMDB(id int) (*Show, error) {
	return findFirst(l.Shows, func(s *Show) bool { return s.UIDs.TMDB == id })
}

// FindShowByIMDB ...
func FindShowByIMDB(id string) (*Show, error) {
	return findFirst(l.Shows, func(s *Show) bool { return s.UIDs.IMDB == id })
}

// GetShowByIMDB ...
func GetShowByIMDB(id string) (*Show, error) {
	mu := l.GetMutex(ShowsMutex)
	mu.RLock()
	defer mu.RUnlock()

	return findFirst(l.Shows, func(s *Show) bool { return s.UIDs.IMDB == id })
}

// GetEpisode ...
func (s *Show) GetEpisode(season, episode int) *Episode {
	for _, e := range s.Episodes {
		if e.Season == season && e.Episode == episode {
			return e
		}
	}

	return nil
}

// GetSeason returns season by Kodi library ID
func (s *Show) GetSeason(season int) *Season {
	for _, se := range s.Seasons {
		if se.Season == season {
			return se
		}
	}

	return nil
}

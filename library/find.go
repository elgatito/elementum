package library

import (
	"errors"
)

//
// Library searchers
//

// GetLibraryMovie finds Movie from library
func GetLibraryMovie(kodiID int) *Movie {
	l.mu.Movies.Lock()
	defer l.mu.Movies.Unlock()

	for _, m := range l.Movies {
		if m.UIDs.Kodi == kodiID {
			return m
		}
	}

	return nil
}

// GetLibraryShow finds Show from library
func GetLibraryShow(kodiID int) *Show {
	l.mu.Shows.RLock()
	defer l.mu.Shows.RUnlock()

	// query := strconv.Itoa(kodiID)
	for _, s := range l.Shows {
		if s.UIDs.Kodi == kodiID {
			return s
		}
	}

	return nil
}

// GetLibrarySeason finds Show/Season from library
func GetLibrarySeason(kodiID int) (*Show, *Season) {
	l.mu.Shows.RLock()
	defer l.mu.Shows.RUnlock()

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
	l.mu.Shows.RLock()
	defer l.mu.Shows.RUnlock()

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
	l.mu.Movies.RLock()
	defer l.mu.Movies.RUnlock()

	for _, m := range l.Movies {
		if m != nil && m.UIDs.TMDB == id {
			return m, nil
		}
	}

	return nil, errors.New("Not found")
}

// GetMovieByIMDB ...
func GetMovieByIMDB(id string) (*Movie, error) {
	l.mu.Movies.RLock()
	defer l.mu.Movies.RUnlock()

	for _, m := range l.Movies {
		if m != nil && m.UIDs.IMDB == id {
			return m, nil
		}
	}

	return nil, errors.New("Not found")
}

// GetShowByTMDB ...
func GetShowByTMDB(id int) (*Show, error) {
	l.mu.Shows.RLock()
	defer l.mu.Shows.RUnlock()

	for _, s := range l.Shows {
		if s != nil && s.UIDs.TMDB == id {
			return s, nil
		}
	}

	return nil, errors.New("Not found")
}

// findShowByKodi ...
func findShowByKodi(id int) (*Show, error) {
	for _, s := range l.Shows {
		if s != nil && s.UIDs.Kodi == id {
			return s, nil
		}
	}

	return nil, errors.New("Not found")
}

// findShowByTMDB ...
func findShowByTMDB(id int) (*Show, error) {
	for _, s := range l.Shows {
		if s != nil && s.UIDs.TMDB == id {
			return s, nil
		}
	}

	return nil, errors.New("Not found")
}

// findShowByIMDB ...
func findShowByIMDB(id string) (*Show, error) {
	for _, s := range l.Shows {
		if s != nil && s.UIDs.IMDB == id {
			return s, nil
		}
	}

	return nil, errors.New("Not found")
}

// GetShowByIMDB ...
func GetShowByIMDB(id string) (*Show, error) {
	l.mu.Shows.RLock()
	defer l.mu.Shows.RUnlock()

	for _, s := range l.Shows {
		if s != nil && s.UIDs.IMDB == id {
			return s, nil
		}
	}

	return nil, errors.New("Not found")
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

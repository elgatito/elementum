package library

import (
	"testing"

	"github.com/elgatito/elementum/library/uid"
	"github.com/elgatito/elementum/tracker"
)

func TestKodiWatchedSetter(t *testing.T) {
	l := uid.Get()
	oldShows, oldMovies := l.Shows, l.Movies
	t.Cleanup(func() { l.Shows, l.Movies = oldShows, oldMovies })
	l.Shows = []*uid.Show{{ID: 1, UIDs: &uid.UniqueIDs{TMDB: 987654},
		Seasons: []*uid.Season{{ID: 2, Season: 1}}, Episodes: []*uid.Episode{{ID: 3, Season: 1, Episode: 1}}}}
	l.Movies = []*uid.Movie{{ID: 4, UIDs: &uid.UniqueIDs{TMDB: 987654}}}
	ids := tracker.IDs{TMDB: 987654}
	for _, c := range []struct {
		name string
		item tracker.Item
		want bool
	}{
		{"movie", tracker.Item{Type: tracker.MovieType, IDs: ids}, true},
		{"show", tracker.Item{Type: tracker.ShowType, IDs: ids}, true},
		{"season in library", tracker.Item{Type: tracker.SeasonType, IDs: ids, Season: 1}, true},
		{"season missing", tracker.Item{Type: tracker.SeasonType, IDs: ids, Season: 5}, false},
		{"episode in library", tracker.Item{Type: tracker.EpisodeType, IDs: ids, Season: 1, Episode: 1}, true},
		{"episode missing", tracker.Item{Type: tracker.EpisodeType, IDs: ids, Season: 1, Episode: 2}, false},
		{"show missing", tracker.Item{Type: tracker.ShowType, IDs: tracker.IDs{TMDB: 1}}, false},
		{"movie missing", tracker.Item{Type: tracker.MovieType, IDs: tracker.IDs{TMDB: 1}}, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := KodiWatchedSetter(c.item) != nil; got != c.want {
				t.Errorf("setter present = %v, want %v", got, c.want)
			}
		})
	}
}

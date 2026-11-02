package library

import (
	"testing"

	"github.com/elgatito/elementum/library/uid"
	"github.com/elgatito/elementum/xbmc"
)

func TestFindDuplicatesPrefersRealFiles(t *testing.T) {
	l := uid.Get()
	defer func(m []*uid.Movie, s []*uid.Show) { l.Movies, l.Shows = m, s }(l.Movies, l.Shows)

	ours := &xbmc.UniqueIDs{Elementum: "1"}
	strmMovie := &uid.Movie{File: "/lib/Movie/Movie.strm", UIDs: &uid.UniqueIDs{TMDB: 10}, XbmcUIDs: ours}
	l.Movies = []*uid.Movie{
		strmMovie,
		{File: "/dl/Movie/movie.mkv", UIDs: &uid.UniqueIDs{TMDB: 10}, XbmcUIDs: &xbmc.UniqueIDs{}},
		{File: "/lib/Other/Other.strm", UIDs: &uid.UniqueIDs{TMDB: 20}, XbmcUIDs: ours},
	}
	if d, _ := findMovieDuplicates(); len(d) != 1 || d[0] != strmMovie {
		t.Fatalf("expected only downloaded movie .strm as duplicate, got %v", d)
	}

	strmEp := &uid.Episode{Season: 1, Episode: 1, File: "/lib/Show/Show S01E01.strm", UIDs: &uid.UniqueIDs{}}
	l.Shows = []*uid.Show{
		{UIDs: &uid.UniqueIDs{TMDB: 30}, XbmcUIDs: ours, Episodes: []*uid.Episode{
			strmEp,
			{Season: 1, Episode: 2, File: "/lib/Show/Show S01E02.strm", UIDs: &uid.UniqueIDs{}},
		}},
		{UIDs: &uid.UniqueIDs{TMDB: 30}, XbmcUIDs: &xbmc.UniqueIDs{}, Episodes: []*uid.Episode{
			{Season: 1, Episode: 1, File: "/dl/Show/Season 1/ep1.mkv", UIDs: &uid.UniqueIDs{}},
		}},
	}
	if d, _ := findEpisodeDuplicates(); len(d) != 1 || d[0] != strmEp {
		t.Fatalf("expected only downloaded episode .strm as duplicate, got %v", d)
	}
	if d, _ := findShowDuplicates(); len(d) != 0 {
		t.Fatalf("show with downloaded episodes must not be removed as a whole, got %v", d)
	}
}

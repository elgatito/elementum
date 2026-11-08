package apiv1

import (
	"testing"

	"github.com/elgatito/elementum/library/playcount"
)

func TestShowWatchedCounts(t *testing.T) {
	const show = 987001
	mark := func(season, from, to int) {
		for e := from; e <= to; e++ {
			playcount.Set(playcount.EpisodeType, true, show, season, e)
			t.Cleanup(func() { playcount.Set(playcount.EpisodeType, false, show, season, e) })
		}
	}
	lookup := func(s, e int) bool { return bool(playcount.GetWatchedEpisodeByTMDB(show, s, e)) }
	seasons := func() ShowDetails {
		return ShowDetails{Seasons: []SeasonSummary{{SeasonNumber: 0, EpisodeCount: 3}, {SeasonNumber: 1, EpisodeCount: 2}, {SeasonNumber: 2, EpisodeCount: 2}}}
	}

	mark(1, 1, 2)
	d := seasons()
	d.setWatched(show, lookup)
	if *d.Seasons[1].WatchedEpisodes != 2 || !*d.Seasons[1].Watched || *d.Seasons[2].Watched || *d.Seasons[0].WatchedEpisodes != 0 {
		t.Errorf("seasons %+v", d.Seasons)
	}
	if *d.Watched {
		t.Error("show with an unwatched season is watched")
	}

	mark(2, 1, 2) // specials stay unwatched, the regular seasons are done
	d = seasons()
	d.setWatched(show, lookup)
	if !*d.Watched {
		t.Error("all regular seasons watched, show is not")
	}

	d = ShowDetails{Seasons: []SeasonSummary{{SeasonNumber: 0, EpisodeCount: 1}}}
	d.setWatched(show, lookup)
	if *d.Watched {
		t.Error("a show with only specials is watched")
	}

	d = seasons() // a show-level mark counts even when its episodes are not marked
	d.Watched = ptr(true)
	d.setWatched(show+1, lookup)
	if !*d.Watched {
		t.Error("show-level watched flag was dropped")
	}
}

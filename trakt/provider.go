package trakt

import (
	"strconv"
	"time"

	"github.com/elgatito/elementum/config"
	"github.com/elgatito/elementum/tracker"
	"github.com/elgatito/elementum/util/reqapi"
)

// Provider adapts the trakt package to tracker.Provider.
type Provider struct{}

// NewProvider returns the Trakt tracker.
func NewProvider() *Provider { return &Provider{} }

func (p *Provider) ID() string                     { return "trakt" }
func (p *Provider) Name() string                   { return "Trakt" }
func (p *Provider) MinSyncInterval() time.Duration { return time.Minute }
func (p *Provider) Authorized() bool               { return config.Get().TraktToken != "" }
func (p *Provider) Authorize() error               { return Authorize(true) }
func (p *Provider) Deauthorize() error             { return Deauthorize(true) }

// RefreshToken renews the token 90 minutes before its 24h expiry, see https://github.com/trakt/api-help/discussions/495
func (p *Provider) RefreshToken() error {
	if config.Get().TraktToken == "" || time.Now().Unix() <= config.Get().TraktTokenExpiry-90*60 {
		return nil
	}
	return RefreshToken()
}

func (p *Provider) LastActivities() (*tracker.Activities, error) {
	a, err := GetLastActivities()
	if err == ErrLocked {
		go NotifyLocked()
		return nil, tracker.ErrLocked
	} else if err != nil {
		return nil, err
	}

	ret := tracker.NewActivities()
	ret.Set(a.Movies.WatchedAt, tracker.MoviesWatched)
	ret.Set(a.Movies.CollectedAt, tracker.MoviesCollected)
	ret.Set(a.Movies.WatchlistedAt, tracker.MoviesWatchlisted)
	ret.Set(a.Movies.PausedAt, tracker.MoviesPaused)
	ret.Set(a.Movies.HiddenAt, tracker.MoviesHidden)
	ret.Set(a.Episodes.WatchedAt, tracker.EpisodesWatched)
	ret.Set(a.Episodes.CollectedAt, tracker.EpisodesCollected)
	ret.Set(a.Episodes.WatchlistedAt, tracker.EpisodesWatchlisted)
	ret.Set(a.Episodes.PausedAt, tracker.EpisodesPaused)
	ret.Set(a.Shows.WatchlistedAt, tracker.ShowsWatchlisted)
	ret.Set(a.Shows.HiddenAt, tracker.ShowsHidden)
	ret.Set(a.Seasons.WatchlistedAt, tracker.SeasonsWatchlisted)
	ret.Set(a.Seasons.HiddenAt, tracker.SeasonsHidden)
	ret.Set(a.Lists.UpdatedAt, tracker.ListsUpdated)
	return ret, nil
}

func (p *Provider) Watchlist(t tracker.MediaType, refresh bool) ([]*tracker.Item, error) {
	if t == tracker.MovieType {
		movies, err := WatchlistMovies(refresh)
		return moviesToItems(movies), err
	}
	shows, err := WatchlistShows(refresh)
	return showsToItems(shows), err
}

func (p *Provider) Collection(t tracker.MediaType, refresh bool) ([]*tracker.Item, error) {
	if t == tracker.MovieType {
		movies, err := CollectionMovies(refresh)
		return moviesToItems(movies), err
	}
	shows, err := CollectionShows(refresh)
	return showsToItems(shows), err
}

func (p *Provider) Watched(t tracker.MediaType, refresh bool) ([]*tracker.WatchedItem, error) {
	if t == tracker.MovieType {
		movies, err := WatchedMovies(refresh)
		ret := make([]*tracker.WatchedItem, 0, len(movies))
		for _, m := range movies {
			if m == nil || m.Movie == nil || m.Movie.IDs == nil {
				continue
			}
			ret = append(ret, &tracker.WatchedItem{Item: *movieItem(m.Movie), Plays: m.Plays, LastWatchedAt: m.LastWatchedAt})
		}
		return ret, err
	}

	shows, err := WatchedShows(refresh)
	ret := make([]*tracker.WatchedItem, 0, len(shows))
	for _, s := range shows {
		if s == nil || s.Show == nil || s.Show.IDs == nil {
			continue
		}
		w := &tracker.WatchedItem{Item: *showItem(s.Show), Plays: s.Plays, LastWatchedAt: s.LastWatchedAt}
		for _, season := range s.Seasons {
			if season == nil {
				continue
			}
			for _, e := range season.Episodes {
				if e != nil {
					w.Episodes = append(w.Episodes, tracker.WatchedEpisode{Season: season.Number, Number: e.Number, Plays: e.Plays, LastWatchedAt: e.LastWatchedAt})
				}
			}
		}
		ret = append(ret, w)
	}
	return ret, err
}

func (p *Provider) Paused(t tracker.MediaType, refresh bool) ([]*tracker.PausedItem, error) {
	var ret []*tracker.PausedItem
	if t == tracker.MovieType {
		movies, err := PausedMovies(refresh)
		for _, m := range movies {
			if m == nil || m.Movie == nil || m.Movie.IDs == nil {
				continue
			}
			ret = append(ret, &tracker.PausedItem{Item: *movieItem(m.Movie), Progress: m.Progress, Runtime: m.Movie.Runtime * 60, PausedAt: m.PausedAt})
		}
		return ret, err
	}

	shows, err := PausedShows(refresh)
	for _, s := range shows {
		if s == nil || s.Show == nil || s.Show.IDs == nil || s.Episode == nil {
			continue
		}
		i := showItem(s.Show)
		i.Type = tracker.EpisodeType
		i.Season = s.Episode.Season
		i.Episode = s.Episode.Number
		if s.Episode.IDs != nil {
			i.EpisodeTMDB = s.Episode.IDs.TMDB
		}
		ret = append(ret, &tracker.PausedItem{Item: *i, Progress: s.Progress, Runtime: s.Episode.Runtime * 60, PausedAt: s.PausedAt})
	}
	return ret, err
}

func (p *Provider) Lists() ([]*tracker.List, error) {
	lists := Userlists()
	ret := make([]*tracker.List, 0, len(lists))
	for _, l := range lists {
		if l == nil || l.IDs == nil {
			continue
		}
		ret = append(ret, &tracker.List{ID: strconv.Itoa(l.IDs.Trakt), Name: l.Name, ItemCount: l.ItemCount, UpdatedAt: l.UpdatedAt})
	}
	return ret, nil
}

func (p *Provider) ListItems(listID string, t tracker.MediaType, refresh bool) ([]*tracker.Item, error) {
	if t == tracker.MovieType {
		movies, err := ListItemsMovies(config.Get().TraktUsername, listID)
		return moviesToItems(movies), err
	}
	shows, err := ListItemsShows(config.Get().TraktUsername, listID)
	return showsToItems(shows), err
}

// Progress returns the next episode of shows in progress. The Trakt progress menu keeps its own view;
// this makes the progress entry appear in the tracker menu.
func (p *Provider) Progress(refresh bool) ([]*tracker.Item, error) {
	shows, err := WatchedShowsProgress()
	ret := make([]*tracker.Item, 0, len(shows))
	for _, s := range shows {
		if s == nil || s.Show == nil || s.Show.IDs == nil || s.Episode == nil {
			continue
		}
		i := showItem(s.Show)
		i.Type, i.Season, i.Episode = tracker.EpisodeType, s.Episode.Season, s.Episode.Number
		if s.Episode.IDs != nil {
			i.EpisodeTMDB = s.Episode.IDs.TMDB
		}
		ret = append(ret, i)
	}
	return ret, err
}

func (p *Provider) Hidden(section string, refresh bool) ([]*tracker.Item, error) {
	shows, err := ListHiddenShows(section, refresh)
	return showsToItems(shows), err
}

func (p *Provider) Add(loc tracker.Location, listID string, items []*tracker.Item) error {
	return p.write(loc, listID, items, AddToCollection, AddToWatchlist, AddToUserlist)
}

func (p *Provider) Remove(loc tracker.Location, listID string, items []*tracker.Item) error {
	return p.write(loc, listID, items, RemoveFromCollection, RemoveFromWatchlist, RemoveFromUserlist)
}

func (p *Provider) write(loc tracker.Location, listID string, items []*tracker.Item,
	collection, watchlist func(string, string) (*reqapi.Request, error), list func(int, string, string) (*reqapi.Request, error)) error {
	id, _ := strconv.Atoi(listID)
	for _, i := range items {
		itemType := "movies"
		if i.Type != tracker.MovieType {
			itemType = "shows"
		}
		tmdbID := strconv.Itoa(i.IDs.TMDB)

		var err error
		switch loc {
		case tracker.LocationCollection:
			_, err = collection(itemType, tmdbID)
		case tracker.LocationWatchlist:
			_, err = watchlist(itemType, tmdbID)
		case tracker.LocationList:
			_, err = list(id, itemType, tmdbID)
		default:
			err = tracker.ErrUnsupported
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func (p *Provider) SetWatched(items []*tracker.WatchedItem, watched bool) error {
	var movies, shows []*WatchedItem
	for _, i := range items {
		w := &WatchedItem{KodiKey: i.KodiKey, Watched: watched, WatchedAt: i.LastWatchedAt}
		switch i.Type {
		case tracker.MovieType:
			w.MediaType, w.Movie = "movie", i.IDs.TMDB
			movies = append(movies, w)
			continue
		case tracker.ShowType:
			w.MediaType = "show"
		case tracker.SeasonType:
			w.MediaType, w.Season = "season", i.Season
		case tracker.EpisodeType:
			w.MediaType, w.Season, w.Episode = "episode", i.Season, i.Episode
		}
		w.Show = i.IDs.TMDB
		shows = append(shows, w)
	}

	for _, group := range [][]*WatchedItem{movies, shows} {
		if len(group) == 0 {
			continue
		}
		if _, err := SetMultipleWatched(group); err != nil {
			return err
		}
	}
	return nil
}

func (p *Provider) Scrobble(action tracker.ScrobbleAction, item *tracker.Item, progress float64) error {
	switch item.Type {
	case tracker.MovieType:
		Scrobble(string(action), "movie", item.IDs.TMDB, progress, 100)
	case tracker.EpisodeType:
		Scrobble(string(action), "episode", item.EpisodeTMDB, progress, 100)
	default:
		return tracker.ErrUnsupported
	}
	return nil
}

func traktIDs(ids *IDs) tracker.IDs {
	return tracker.IDs{TMDB: ids.TMDB, IMDB: ids.IMDB, TVDB: ids.TVDB, Trakt: ids.Trakt, Native: strconv.Itoa(ids.Trakt)}
}

func movieItem(m *Movie) *tracker.Item {
	return &tracker.Item{Type: tracker.MovieType, IDs: traktIDs(m.IDs), Title: m.Title, Year: m.Year, UpdatedAt: m.UpdatedAt}
}

func showItem(s *Show) *tracker.Item {
	return &tracker.Item{Type: tracker.ShowType, IDs: traktIDs(s.IDs), Title: s.Title, Year: s.Year, UpdatedAt: s.UpdatedAt}
}

func moviesToItems(movies []*Movies) []*tracker.Item {
	ret := make([]*tracker.Item, 0, len(movies))
	for _, m := range movies {
		if m != nil && m.Movie != nil && m.Movie.IDs != nil {
			ret = append(ret, movieItem(m.Movie))
		}
	}
	return ret
}

func showsToItems(shows []*Shows) []*tracker.Item {
	ret := make([]*tracker.Item, 0, len(shows))
	for _, s := range shows {
		if s != nil && s.Show != nil && s.Show.IDs != nil {
			ret = append(ret, showItem(s.Show))
		}
	}
	return ret
}

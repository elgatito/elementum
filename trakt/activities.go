package trakt

import (
	"fmt"

	"github.com/elgatito/elementum/cache"
)

type Activities struct {
	source string

	Previous *UserActivities
	Current  *UserActivities
}

func GetActivities(source string) (*Activities, error) {

	current, err := GetLastActivities()

	var previous UserActivities
	_ = cache.NewDBStore().Get(fmt.Sprintf(cache.TraktActivitiesKey, source), &previous)

	// Do not auto-save sync calls, as they might need to perform it manually
	if source != "" && err == nil {
		saveActivity(source, current)
	}

	return &Activities{
		source: source,

		Previous: &previous,
		Current:  current,
	}, err
}

func (a *Activities) SaveCurrent() error {
	return saveActivity(a.source, a.Current)
}

func saveActivity(source string, activity *UserActivities) error {
	return cache.NewDBStore().Set(fmt.Sprintf(cache.TraktActivitiesKey, source), activity, cache.TraktActivitiesExpire)
}

func (a *Activities) HasPrevious() bool {
	return a.Previous != nil && !a.Previous.All.IsZero()
}

// All    time.Time `json:"all"`

func (a *Activities) All() bool {
	return a.Current.All.After(a.Previous.All)
}

// Movies struct {
// 	WatchedAt     time.Time `json:"watched_at"`
// 	CollectedAt   time.Time `json:"collected_at"`
// 	RatedAt       time.Time `json:"rated_at"`
// 	WatchlistedAt time.Time `json:"watchlisted_at"`
// 	FavoritedAt   time.Time `json:"favorited_at"`
// 	CommentedAt   time.Time `json:"commented_at"`
// 	PausedAt      time.Time `json:"paused_at"`
// 	HiddenAt      time.Time `json:"hidden_at"`
// } `json:"movies"`

func (a *Activities) MoviesWatched() bool {
	return !a.HasPrevious() || a.Current.Movies.WatchedAt.After(a.Previous.Movies.WatchedAt)
}

func (a *Activities) MoviesCollected() bool {
	return !a.HasPrevious() || a.Current.Movies.CollectedAt.After(a.Previous.Movies.CollectedAt)
}

func (a *Activities) MoviesRated() bool {
	return !a.HasPrevious() || a.Current.Movies.RatedAt.After(a.Previous.Movies.RatedAt)
}

func (a *Activities) MoviesWatchlisted() bool {
	return !a.HasPrevious() || a.Current.Movies.WatchlistedAt.After(a.Previous.Movies.WatchlistedAt)
}

func (a *Activities) MoviesFavorited() bool {
	return !a.HasPrevious() || a.Current.Movies.FavoritedAt.After(a.Previous.Movies.FavoritedAt)
}

func (a *Activities) MoviesCommented() bool {
	return !a.HasPrevious() || a.Current.Movies.CommentedAt.After(a.Previous.Movies.CommentedAt)
}

func (a *Activities) MoviesPaused() bool {
	return !a.HasPrevious() || a.Current.Movies.PausedAt.After(a.Previous.Movies.PausedAt)
}

func (a *Activities) MoviesHidden() bool {
	return !a.HasPrevious() || a.Current.Movies.HiddenAt.After(a.Previous.Movies.HiddenAt)
}

// Episodes struct {
// 	WatchedAt     time.Time `json:"watched_at"`
// 	CollectedAt   time.Time `json:"collected_at"`
// 	RatedAt       time.Time `json:"rated_at"`
// 	WatchlistedAt time.Time `json:"watchlisted_at"`
// 	CommentedAt   time.Time `json:"commented_at"`
// 	PausedAt      time.Time `json:"paused_at"`
// } `json:"episodes"`

func (a *Activities) EpisodesWatched() bool {
	return !a.HasPrevious() || a.Current.Episodes.WatchedAt.After(a.Previous.Episodes.WatchedAt)
}

func (a *Activities) EpisodesCollected() bool {
	return !a.HasPrevious() || a.Current.Episodes.CollectedAt.After(a.Previous.Episodes.CollectedAt)
}

func (a *Activities) EpisodesRated() bool {
	return !a.HasPrevious() || a.Current.Episodes.RatedAt.After(a.Previous.Episodes.RatedAt)
}

func (a *Activities) EpisodesWatchlisted() bool {
	return !a.HasPrevious() || a.Current.Episodes.WatchlistedAt.After(a.Previous.Episodes.WatchlistedAt)
}

func (a *Activities) EpisodesCommented() bool {
	return !a.HasPrevious() || a.Current.Episodes.CommentedAt.After(a.Previous.Episodes.CommentedAt)
}

func (a *Activities) EpisodesPaused() bool {
	return !a.HasPrevious() || a.Current.Episodes.PausedAt.After(a.Previous.Episodes.PausedAt)
}

// Shows struct {
// 	RatedAt       time.Time `json:"rated_at"`
// 	WatchlistedAt time.Time `json:"watchlisted_at"`
// 	FavoritedAt   time.Time `json:"favorited_at"`
// 	CommentedAt   time.Time `json:"commented_at"`
// 	HiddenAt      time.Time `json:"hidden_at"`
// } `json:"shows"`

func (a *Activities) ShowsRated() bool {
	return !a.HasPrevious() || a.Current.Shows.RatedAt.After(a.Previous.Shows.RatedAt)
}

func (a *Activities) ShowsWatchlisted() bool {
	return !a.HasPrevious() || a.Current.Shows.WatchlistedAt.After(a.Previous.Shows.WatchlistedAt)
}

func (a *Activities) ShowsFavorited() bool {
	return !a.HasPrevious() || a.Current.Shows.FavoritedAt.After(a.Previous.Shows.FavoritedAt)
}

func (a *Activities) ShowsCommented() bool {
	return !a.HasPrevious() || a.Current.Shows.CommentedAt.After(a.Previous.Shows.CommentedAt)
}

func (a *Activities) ShowsHidden() bool {
	return !a.HasPrevious() || a.Current.Shows.HiddenAt.After(a.Previous.Shows.HiddenAt)
}

// Seasons struct {
// 	RatedAt       time.Time `json:"rated_at"`
// 	WatchlistedAt time.Time `json:"watchlisted_at"`
// 	CommentedAt   time.Time `json:"commented_at"`
// 	HiddenAt      time.Time `json:"hidden_at"`
// } `json:"seasons"`

func (a *Activities) SeasonsRated() bool {
	return !a.HasPrevious() || a.Current.Seasons.RatedAt.After(a.Previous.Seasons.RatedAt)
}

func (a *Activities) SeasonsWatchlisted() bool {
	return !a.HasPrevious() || a.Current.Seasons.WatchlistedAt.After(a.Previous.Seasons.WatchlistedAt)
}

func (a *Activities) SeasonsCommented() bool {
	return !a.HasPrevious() || a.Current.Seasons.CommentedAt.After(a.Previous.Seasons.CommentedAt)
}

func (a *Activities) SeasonsHidden() bool {
	return !a.HasPrevious() || a.Current.Seasons.HiddenAt.After(a.Previous.Seasons.HiddenAt)
}

// Lists struct {
// 	LikedAt     time.Time `json:"liked_at"`
// 	UpdatedAt   time.Time `json:"updated_at"`
// 	CommentedAt time.Time `json:"commented_at"`
// } `json:"lists"`

func (a *Activities) ListsLiked() bool {
	return !a.HasPrevious() || a.Current.Lists.LikedAt.After(a.Previous.Lists.LikedAt)
}

func (a *Activities) ListsUpdated() bool {
	return !a.HasPrevious() || a.Current.Lists.UpdatedAt.After(a.Previous.Lists.UpdatedAt)
}

func (a *Activities) ListsCommented() bool {
	return !a.HasPrevious() || a.Current.Lists.CommentedAt.After(a.Previous.Lists.CommentedAt)
}

// Watchlist struct {
// 	UpdatedAt time.Time `json:"updated_at"`
// } `json:"watchlist"`

func (a *Activities) WatchlistUpdated() bool {
	return !a.HasPrevious() || a.Current.Watchlist.UpdatedAt.After(a.Previous.Watchlist.UpdatedAt)
}

// Favorites struct {
// 	UpdatedAt time.Time `json:"updated_at"`
// } `json:"favorites"`

func (a *Activities) FavoritesUpdated() bool {
	return !a.HasPrevious() || a.Current.Favorites.UpdatedAt.After(a.Previous.Favorites.UpdatedAt)
}

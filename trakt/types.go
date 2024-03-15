package trakt

import "time"

// Object ...
type Object struct {
	Title     string    `json:"title"`
	Year      int       `json:"year"`
	IDs       *IDs      `json:"ids"`
	UpdatedAt time.Time `json:"updated_at"`
}

// MovieSearchResults ...
type MovieSearchResults []struct {
	Type  string      `json:"type"`
	Score interface{} `json:"score"`
	Movie *Movie      `json:"movie"`
}

// ShowSearchResults ...
type ShowSearchResults []struct {
	Type  string      `json:"type"`
	Score interface{} `json:"score"`
	Show  *Show       `json:"show"`
}

// EpisodeSearchResults ...
type EpisodeSearchResults []struct {
	Type    string      `json:"type"`
	Score   interface{} `json:"score"`
	Episode *Episode
	Show    *Show
}

// Movie ...
type Movie struct {
	Object

	Certification string   `json:"certification"`
	Genres        []string `json:"genres"`
	Language      string   `json:"language"`
	Overview      string   `json:"overview"`
	Rating        float32  `json:"rating"`
	Released      string   `json:"released"`
	Runtime       int      `json:"runtime"`
	TagLine       string   `json:"tagline"`
	Trailer       string   `json:"trailer"`
	Translations  []string `json:"available_translations"`
	URL           string   `json:"homepage"`
	Votes         int      `json:"votes"`
}

// Show ...
type Show struct {
	Object

	AiredEpisodes int      `json:"aired_episodes"`
	Airs          *Airs    `json:"airs"`
	Certification string   `json:"certification"`
	Country       string   `json:"country"`
	FirstAired    string   `json:"first_aired"`
	Genres        []string `json:"genres"`
	Language      string   `json:"language"`
	Network       string   `json:"network"`
	Overview      string   `json:"overview"`
	Rating        float32  `json:"rating"`
	Runtime       int      `json:"runtime"`
	Status        string   `json:"status"`
	Trailer       string   `json:"trailer"`
	Translations  []string `json:"available_translations"`
	URL           string   `json:"homepage"`
	Votes         int      `json:"votes"`
}

// Season ...
type Season struct {
	// Show          *Show   `json:"-"`
	AiredEpisodes int     `json:"aired_episodes"`
	EpisodeCount  int     `json:"episode_count"`
	FirstAired    string  `json:"first_aired"`
	Network       string  `json:"network"`
	Number        int     `json:"number"`
	Overview      string  `json:"overview"`
	Rating        float32 `json:"rating"`
	Votes         int     `json:"votes"`

	Episodes []*Episode `json:"episodes"`
	IDs      *IDs       `json:"ids"`
}

// Episode ...
type Episode struct {
	// Show          *Show       `json:"-"`
	// Season        *ShowSeason `json:"-"`
	Absolute     int      `json:"number_abs"`
	FirstAired   string   `json:"first_aired"`
	Number       int      `json:"number"`
	Overview     string   `json:"overview"`
	Season       int      `json:"season"`
	Title        string   `json:"title"`
	Translations []string `json:"available_translations"`

	Runtime int     `json:"runtime"`
	Rating  float32 `json:"rating"`
	Votes   int     `json:"votes"`

	IDs *IDs `json:"ids"`
}

// Airs ...
type Airs struct {
	Day      string `json:"day"`
	Time     string `json:"time"`
	Timezone string `json:"timezone"`
}

// Movies ...
type Movies struct {
	Watchers int    `json:"watchers"`
	Movie    *Movie `json:"movie"`
}

// Shows ...
type Shows struct {
	Watchers int   `json:"watchers"`
	Show     *Show `json:"show"`
}

// Watchlist ...
type Watchlist struct {
	Movies   []*Movie   `json:"movies"`
	Shows    []*Show    `json:"shows"`
	Episodes []*Episode `json:"episodes"`
}

// WatchlistMovie ...
type WatchlistMovie struct {
	ListedAt time.Time `json:"listed_at"`
	Type     string    `json:"type"`
	Movie    *Movie    `json:"movie"`
}

// WatchlistShow ...
type WatchlistShow struct {
	ListedAt time.Time `json:"listed_at"`
	Type     string    `json:"type"`
	Show     *Show     `json:"show"`
}

// WatchlistSeason ...
type WatchlistSeason struct {
	ListedAt time.Time `json:"listed_at"`
	Type     string    `json:"type"`
	Season   *Object   `json:"season"`
	Show     *Object   `json:"show"`
}

// WatchlistEpisode ...
type WatchlistEpisode struct {
	ListedAt time.Time `json:"listed_at"`
	Type     string    `json:"type"`
	Episode  *Episode  `json:"episode"`
	Show     *Object   `json:"show"`
}

// CollectionMovie ...
type CollectionMovie struct {
	CollectedAt time.Time `json:"collected_at"`
	Movie       *Movie    `json:"movie"`
}

// CollectionShow ...
type CollectionShow struct {
	CollectedAt time.Time          `json:"last_collected_at"`
	Show        *Show              `json:"show"`
	Seasons     []*CollectedSeason `json:"seasons"`
}

// CollectedSeason ...
type CollectedSeason struct {
	Number   int                 `json:"number"`
	Episodes []*CollectedEpisode `json:"episodes"`
}

// CollectedEpisode ...
type CollectedEpisode struct {
	CollectedAt time.Time `json:"collected_at"`
	Number      int       `json:"number"`
}

// HiddenShow ...
type HiddenShow struct {
	HiddenAt time.Time `json:"hidden_at"`
	Type     string    `json:"type"`
	Show     *Show     `json:"show"`
}

// Sizes ...
type Sizes struct {
	Full      string `json:"full"`
	Medium    string `json:"medium"`
	Thumbnail string `json:"thumb"`
}

// IDs ...
type IDs struct {
	Trakt  int    `json:"trakt"`
	IMDB   string `json:"imdb"`
	TMDB   int    `json:"tmdb"`
	TVDB   int    `json:"tvdb"`
	TVRage int    `json:"tvrage"`
	Slug   string `json:"slug"`
}

// Code ...
type Code struct {
	DeviceCode      string `json:"device_code"`
	UserCode        string `json:"user_code"`
	VerificationURL string `json:"verification_url"`
	ExpiresIn       int    `json:"expires_in"`
	Interval        int    `json:"interval"`
}

// Token ...
type Token struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	RefreshToken string `json:"refresh_token"`
	Scope        string `json:"scope"`
}

// TokenRefresh ...
type TokenRefresh struct {
	RefreshToken string `json:"refresh_token"`
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
	RedirectURI  string `json:"redirect_uri"`
	GrantType    string `json:"grant_type"`
}

// ListContainer ...
type ListContainer struct {
	LikeCount    int       `json:"like_count"`
	CommentCount int       `json:"comment_count"`
	LikedAt      time.Time `json:"liked_at"`
	Type         string    `json:"type"`
	List         *List     `json:"list"`
}

// List ...
type List struct {
	Name           string    `json:"name"`
	Description    string    `json:"description"`
	Privacy        string    `json:"privacy"`
	Type           string    `json:"type"`
	DisplayNumbers bool      `json:"display_numbers"`
	AllowComments  bool      `json:"allow_comments"`
	SortBy         string    `json:"sort_by"`
	SortHow        string    `json:"sort_how"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
	ItemCount      int       `json:"item_count"`
	CommentCount   int       `json:"comment_count"`
	Likes          int       `json:"likes"`
	IDs            *IDs      `json:"IDs"`
	User           *User     `json:"User"`
}

// ListItem ...
type ListItem struct {
	Rank     int       `json:"rank"`
	ListedAt time.Time `json:"listed_at"`
	Type     string    `json:"type"`
	Movie    *Movie    `json:"movie"`
	Show     *Show     `json:"show"`
	// Season    *Season  `json:"season"`
	// Episode   *Episode `json:"episode"`
}

// CalendarShow ...
type CalendarShow struct {
	FirstAired string   `json:"first_aired"`
	Episode    *Episode `json:"episode"`
	Show       *Show    `json:"show"`
}

// CalendarMovie ...
type CalendarMovie struct {
	Released string `json:"released"`
	Movie    *Movie `json:"movie"`
}

// User ...
type User struct {
	Username string `json:"username"`
	Private  bool   `json:"private"`
	Name     string `json:"name"`
	Vip      bool   `json:"vip"`
	VipEp    bool   `json:"vip_ep"`
	Ids      struct {
		Slug string `json:"slug"`
	} `json:"ids"`
}

// UserSettings ...
type UserSettings struct {
	User    User     `json:"user"`
	Account struct{} `json:"account"`
}

// PausedMovie represents paused movie
type PausedMovie struct {
	Progress float64   `json:"progress"`
	PausedAt time.Time `json:"paused_at"`
	ID       int       `json:"id"`
	Type     string    `json:"type"`
	Movie    *Movie    `json:"movie"`
}

// PausedEpisode represents paused episode with show information
type PausedEpisode struct {
	Progress float64   `json:"progress"`
	PausedAt time.Time `json:"paused_at"`
	ID       int       `json:"id"`
	Type     string    `json:"type"`
	Episode  *Episode  `json:"episode"`
	Show     *Show     `json:"show"`
}

// WatchedItem represents possible watched add/delete item
type WatchedItem struct {
	MediaType string
	KodiID    int
	KodiKey   uint64
	Movie     int
	Show      int
	Season    int
	Episode   int
	Watched   bool
	WatchedAt time.Time
}

// WatchedMovie ...
type WatchedMovie struct {
	Plays         int       `json:"plays"`
	LastWatchedAt time.Time `json:"last_watched_at"`
	Movie         *Movie    `json:"movie"`
}

// WatchedShow ...
type WatchedShow struct {
	Plays         int `json:"plays"`
	Watched       bool
	LastWatchedAt time.Time        `json:"last_watched_at"`
	Show          *Show            `json:"show"`
	Seasons       []*WatchedSeason `json:"seasons"`
}

// WatchedSeason ...
type WatchedSeason struct {
	Plays    int               `json:"plays"`
	Number   int               `json:"number"`
	Episodes []*WatchedEpisode `json:"episodes"`
}

// WatchedEpisode ...
type WatchedEpisode struct {
	Number        int       `json:"number"`
	Plays         int       `json:"plays"`
	LastWatchedAt time.Time `json:"last_watched_at"`
}

// WatchedProgressShow ...
type WatchedProgressShow struct {
	Aired         int       `json:"aired"`
	Completed     int       `json:"completed"`
	LastWatchedAt time.Time `json:"last_watched_at"`
	Seasons       []*Season `json:"seasons"`
	HiddenSeasons []*Season `json:"hidden_seasons"`
	NextEpisode   *Episode  `json:"next_episode"`
	LastEpisode   *Episode  `json:"last_episode"`
}

// ProgressShow ...
type ProgressShow struct {
	Episode *Episode `json:"episode"`
	Show    *Show    `json:"show"`
}

// Pagination ...
type Pagination struct {
	ItemCount int `json:"x_pagination_item_count"`
	Limit     int `json:"x_pagination_limit"`
	Page      int `json:"x_pagination_page"`
	PageCount int `json:"x_pagination_page_count"`
}

// UserActivities is a structure, returned by sync/last_activities
type UserActivities struct {
	All    time.Time `json:"all"`
	Movies struct {
		WatchedAt     time.Time `json:"watched_at"`
		CollectedAt   time.Time `json:"collected_at"`
		RatedAt       time.Time `json:"rated_at"`
		WatchlistedAt time.Time `json:"watchlisted_at"`
		FavoritedAt   time.Time `json:"favorited_at"`
		CommentedAt   time.Time `json:"commented_at"`
		PausedAt      time.Time `json:"paused_at"`
		HiddenAt      time.Time `json:"hidden_at"`
	} `json:"movies"`
	Episodes struct {
		WatchedAt     time.Time `json:"watched_at"`
		CollectedAt   time.Time `json:"collected_at"`
		RatedAt       time.Time `json:"rated_at"`
		WatchlistedAt time.Time `json:"watchlisted_at"`
		CommentedAt   time.Time `json:"commented_at"`
		PausedAt      time.Time `json:"paused_at"`
	} `json:"episodes"`
	Shows struct {
		RatedAt       time.Time `json:"rated_at"`
		WatchlistedAt time.Time `json:"watchlisted_at"`
		FavoritedAt   time.Time `json:"favorited_at"`
		CommentedAt   time.Time `json:"commented_at"`
		HiddenAt      time.Time `json:"hidden_at"`
	} `json:"shows"`
	Seasons struct {
		RatedAt       time.Time `json:"rated_at"`
		WatchlistedAt time.Time `json:"watchlisted_at"`
		CommentedAt   time.Time `json:"commented_at"`
		HiddenAt      time.Time `json:"hidden_at"`
	} `json:"seasons"`
	Comments struct {
		LikedAt   time.Time `json:"liked_at"`
		BlockedAt time.Time `json:"blocked_at"`
	} `json:"comments"`
	Lists struct {
		LikedAt     time.Time `json:"liked_at"`
		UpdatedAt   time.Time `json:"updated_at"`
		CommentedAt time.Time `json:"commented_at"`
	} `json:"lists"`
	Watchlist struct {
		UpdatedAt time.Time `json:"updated_at"`
	} `json:"watchlist"`
	Favorites struct {
		UpdatedAt time.Time `json:"updated_at"`
	} `json:"favorites"`
	Account struct {
		SettingsAt  time.Time `json:"settings_at"`
		FollowedAt  time.Time `json:"followed_at"`
		FollowingAt time.Time `json:"following_at"`
		PendingAt   time.Time `json:"pending_at"`
		RequestedAt time.Time `json:"requested_at"`
	} `json:"account"`
	SavedFilters struct {
		UpdatedAt time.Time `json:"updated_at"`
	} `json:"saved_filters"`
	Notes struct {
		UpdatedAt time.Time `json:"updated_at"`
	} `json:"notes"`
}

// ListItemsPayload describes items to add/remove from userlists
type ListItemsPayload struct {
	Movies []*Movie `json:"movies,omitempty"`
	Shows  []*Show  `json:"shows,omitempty"`
}

// HistoryResponseStats refrects stats for each action type
type HistoryResponseStats struct {
	Movies   int `json:"movies"`
	Episodes int `json:"episodes"`
}

// HistoryResponse reflects response from History remove
type HistoryResponse struct {
	Added    HistoryResponseStats `json:"added"`
	Deleted  HistoryResponseStats `json:"deleted"`
	NotFound struct {
		Movies []struct {
			IDs *IDs `json:"IDs"`
		} `json:"movies"`
		Shows []struct {
			IDs *IDs `json:"IDs"`
		} `json:"shows"`
		Seasons []struct {
			IDs *IDs `json:"IDs"`
		} `json:"seasons"`
		Episodes []struct {
			IDs *IDs `json:"IDs"`
		} `json:"episodes"`
		Ids []int `json:"ids"`
	} `json:"not_found"`
}

type WatchedMoviesType []*WatchedMovie
type WatchedShowsType []*WatchedShow

package trakt

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/elgatito/elementum/broadcast"
	"github.com/elgatito/elementum/cache"
	"github.com/elgatito/elementum/config"
	"github.com/elgatito/elementum/util"
	"github.com/elgatito/elementum/xbmc"
	"github.com/jmcvetta/napping"
	"github.com/op/go-logging"
)

//go:generate msgp -o msgp.go -io=false -tests=false

const (
	// APIURL ...
	APIURL = "https://api.trakt.tv"
	// APIVersion ...
	APIVersion = "2"
)

var log = logging.MustGetLogger("trakt")

var (
	// Cookies ...
	Cookies = ""
	// UserAgent ...
	UserAgent = "Mozilla/5.0 (Windows NT 10.0; WOW64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/53.0.2785.21 Safari/537.36"
)

var (
	// PagesAtOnce ...
	PagesAtOnce             = 5
	retriesLeft             = 3
	burstRate               = 50
	burstTime               = 10 * time.Second
	simultaneousConnections = 25
)

const (
	// ProgressSortWatched ...
	ProgressSortWatched = iota
	// ProgressSortShow ...
	ProgressSortShow
	// ProgressSortAiredNewer ...
	ProgressSortAiredNewer
	// ProgressSortAiredOlder ...
	ProgressSortAiredOlder
)

var (
	// ErrLocked reflects Trakt account locked status
	ErrLocked = errors.New("Account is locked")
)

var rl = util.NewRateLimiter(burstRate, burstTime, simultaneousConnections)

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

	Released      string   `json:"released"`
	URL           string   `json:"homepage"`
	Trailer       string   `json:"trailer"`
	Runtime       int      `json:"runtime"`
	TagLine       string   `json:"tagline"`
	Overview      string   `json:"overview"`
	Certification string   `json:"certification"`
	Rating        float32  `json:"rating"`
	Votes         int      `json:"votes"`
	Genres        []string `json:"genres"`
	Language      string   `json:"language"`
	Translations  []string `json:"available_translations"`

	Images *Images `json:"images"`
}

// Show ...
type Show struct {
	Object

	FirstAired    string   `json:"first_aired"`
	URL           string   `json:"homepage"`
	Trailer       string   `json:"trailer"`
	Runtime       int      `json:"runtime"`
	Overview      string   `json:"overview"`
	Certification string   `json:"certification"`
	Status        string   `json:"status"`
	Network       string   `json:"network"`
	AiredEpisodes int      `json:"aired_episodes"`
	Airs          *Airs    `json:"airs"`
	Rating        float32  `json:"rating"`
	Votes         int      `json:"votes"`
	Genres        []string `json:"genres"`
	Country       string   `json:"country"`
	Language      string   `json:"language"`
	Translations  []string `json:"available_translations"`

	Images *Images `json:"images"`
}

// Season ...
type Season struct {
	// Show          *Show   `json:"-"`
	Number        int     `json:"number"`
	Overview      string  `json:"overview"`
	EpisodeCount  int     `json:"episode_count"`
	AiredEpisodes int     `json:"aired_episodes"`
	Rating        float32 `json:"rating"`
	Votes         int     `json:"votes"`

	Images *Images `json:"images"`
	IDs    *IDs    `json:"ids"`
}

// Episode ...
type Episode struct {
	// Show          *Show       `json:"-"`
	// Season        *ShowSeason `json:"-"`
	Number       int      `json:"number"`
	Season       int      `json:"season"`
	Title        string   `json:"title"`
	Overview     string   `json:"overview"`
	Absolute     int      `json:"number_abs"`
	FirstAired   string   `json:"first_aired"`
	Translations []string `json:"available_translations"`

	Runtime int     `json:"runtime"`
	Rating  float32 `json:"rating"`
	Votes   int     `json:"votes"`

	Images *Images `json:"images"`
	IDs    *IDs    `json:"ids"`
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
	ListedAt string `json:"listed_at"`
	Type     string `json:"type"`
	Movie    *Movie `json:"movie"`
}

// WatchlistShow ...
type WatchlistShow struct {
	ListedAt string `json:"listed_at"`
	Type     string `json:"type"`
	Show     *Show  `json:"show"`
}

// WatchlistSeason ...
type WatchlistSeason struct {
	ListedAt string  `json:"listed_at"`
	Type     string  `json:"type"`
	Season   *Object `json:"season"`
	Show     *Object `json:"show"`
}

// WatchlistEpisode ...
type WatchlistEpisode struct {
	ListedAt string   `json:"listed_at"`
	Type     string   `json:"type"`
	Episode  *Episode `json:"episode"`
	Show     *Object  `json:"show"`
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
	CollectedAt string `json:"collected_at"`
	Number      int    `json:"number"`
}

// HiddenShow ...
type HiddenShow struct {
	HiddenAt string `json:"hidden_at"`
	Type     string `json:"type"`
	Show     *Show  `json:"show"`
}

// Images ...
type Images struct {
	Poster     *Sizes `json:"poster"`
	FanArt     *Sizes `json:"fanart"`
	ScreenShot *Sizes `json:"screenshot"`
	HeadShot   *Sizes `json:"headshot"`
	Logo       *Sizes `json:"logo"`
	ClearArt   *Sizes `json:"clearart"`
	Banner     *Sizes `json:"banner"`
	Thumbnail  *Sizes `json:"thumb"`
	Avatar     *Sizes `json:"avatar"`
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
	Name           string `json:"name"`
	Description    string `json:"description"`
	Privacy        string `json:"privacy"`
	DisplayNumbers bool   `json:"display_numbers"`
	AllowComments  bool   `json:"allow_comments"`
	SortBy         string `json:"sort_by"`
	SortHow        string `json:"sort_how"`
	CreatedAt      string `json:"created_at"`
	UpdatedAt      string `json:"updated_at"`
	ItemCount      int    `json:"item_count"`
	CommentCount   int    `json:"comment_count"`
	Likes          int    `json:"likes"`
	IDs            *IDs   `json:"IDs"`
	User           *User  `json:"User"`
}

// ListItem ...
type ListItem struct {
	Rank     int    `json:"rank"`
	ListedAt string `json:"listed_at"`
	Type     string `json:"type"`
	Movie    *Movie `json:"movie"`
	Show     *Show  `json:"show"`
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
		LikedAt time.Time `json:"liked_at"`
	} `json:"comments"`
	Lists struct {
		LikedAt     time.Time `json:"liked_at"`
		UpdatedAt   time.Time `json:"updated_at"`
		CommentedAt time.Time `json:"commented_at"`
	} `json:"lists"`
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

func totalFromHeaders(headers http.Header) (total int, err error) {
	if len(headers) > 0 {
		if itemCount, exists := headers["X-Pagination-Item-Count"]; exists {
			if itemCount != nil {
				total, err = strconv.Atoi(itemCount[0])
				return
			}
			return -1, errors.New("X-Pagination-Item-Count was empty")
		}
		return -1, errors.New("No X-Pagination-Item-Count header found")
	}
	return -1, errors.New("No valid headers in request")
}

func getPagination(headers http.Header) *Pagination {
	return &Pagination{
		ItemCount: getIntFromHeader(headers, "X-Pagination-Item-Count"),
		Limit:     getIntFromHeader(headers, "X-Pagination-Limit"),
		Page:      getIntFromHeader(headers, "X-Pagination-Page"),
		PageCount: getIntFromHeader(headers, "X-Pagination-Page-Count"),
	}
}

func getIntFromHeader(headers http.Header, key string) (res int) {
	if len(headers) > 0 {
		if itemCount, exists := headers[key]; exists {
			if itemCount != nil {
				res, _ = strconv.Atoi(itemCount[0])
				return res
			}
			return -1
		}
		return -1
	}

	return -1
}

// Get ...
func Get(endPoint string, params url.Values) (resp *napping.Response, err error) {
	header := http.Header{
		"Content-type":      []string{"application/json"},
		"trakt-api-key":     []string{config.TraktReadClientID},
		"trakt-api-version": []string{APIVersion},
		"User-Agent":        []string{UserAgent},
		"Cookie":            []string{Cookies},
	}

	req := napping.Request{
		Url:    fmt.Sprintf("%s/%s", APIURL, endPoint),
		Method: "GET",
		Params: &params,
		Header: &header,
	}

	rl.Call(func() error {
		resp, err = napping.Send(&req)
		if err != nil {
			return err
		} else if resp.Status() == 429 {
			log.Warningf("Rate limit exceeded getting %s, cooling down...", endPoint)
			rl.CoolDown(resp.HttpResponse().Header)
			return util.ErrExceeded
		} else if resp.Status() == 403 && retriesLeft > 0 {
			retriesLeft--
			resp, err = Get(endPoint, params)
		}

		return nil
	})
	return
}

// GetWithAuth ...
func GetWithAuth(endPoint string, params url.Values) (resp *napping.Response, err error) {
	header := http.Header{
		"Content-type":      []string{"application/json"},
		"Authorization":     []string{fmt.Sprintf("Bearer %s", config.Get().TraktToken)},
		"trakt-api-key":     []string{config.TraktWriteClientID},
		"trakt-api-version": []string{APIVersion},
		"User-Agent":        []string{UserAgent},
		"Cookie":            []string{Cookies},
	}

	req := napping.Request{
		Url:    fmt.Sprintf("%s/%s", APIURL, endPoint),
		Method: "GET",
		Params: &params,
		Header: &header,
	}

	rl.Call(func() error {
		resp, err = napping.Send(&req)

		if err != nil {
			return err
		} else if resp.Status() == 401 {
			err = fmt.Errorf("Trakt access token is not valid, please, re-authorize Trakt")
			log.Warningf("Request: %s, Error: %s", endPoint, err)
			xbmc.Notify("Elementum", "LOCALIZE[30576]", config.AddonIcon())
			return err
		} else if resp.Status() == 429 {
			log.Warningf("Rate limit exceeded getting %s, cooling down...", endPoint)
			rl.CoolDown(resp.HttpResponse().Header)
			return util.ErrExceeded
		} else if resp.Status() == 403 && retriesLeft > 0 {
			retriesLeft--
			resp, err = GetWithAuth(endPoint, params)
		}

		return nil
	})
	return
}

// PostJSON ...
func PostJSON(endPoint string, obj interface{}) (resp *napping.Response, err error) {
	b, err := json.Marshal(obj)
	if err != nil {
		fmt.Println(err)
		return
	}

	return Post(endPoint, bytes.NewBuffer(b))
}

// Post ...
func Post(endPoint string, payload *bytes.Buffer) (resp *napping.Response, err error) {
	header := http.Header{
		"Content-type":      []string{"application/json"},
		"Authorization":     []string{fmt.Sprintf("Bearer %s", config.Get().TraktToken)},
		"trakt-api-key":     []string{config.TraktWriteClientID},
		"trakt-api-version": []string{APIVersion},
		"User-Agent":        []string{UserAgent},
		"Cookie":            []string{Cookies},
	}

	req := napping.Request{
		Url:        fmt.Sprintf("%s/%s", APIURL, endPoint),
		Method:     "POST",
		RawPayload: true,
		Payload:    payload,
		Header:     &header,
	}

	rl.Call(func() error {
		resp, err = napping.Send(&req)
		if err != nil {
			return err
		} else if resp.Status() == 429 {
			log.Warningf("Rate limit exceeded getting %s, cooling down...", endPoint)
			rl.CoolDown(resp.HttpResponse().Header)
			return util.ErrExceeded
		} else if resp.Status() == 403 && retriesLeft > 0 {
			retriesLeft--
			resp, err = Post(endPoint, payload)
		}

		return nil
	})
	return
}

// GetCode ...
func GetCode() (code *Code, err error) {
	endPoint := "oauth/device/code"
	header := http.Header{
		"Content-type": []string{"application/json"},
		"User-Agent":   []string{UserAgent},
		"Cookie":       []string{Cookies},
	}
	params := napping.Params{
		"client_id": config.TraktWriteClientID,
	}.AsUrlValues()

	req := napping.Request{
		Url:    fmt.Sprintf("%s/%s", APIURL, endPoint),
		Method: "POST",
		Params: &params,
		Header: &header,
	}

	var resp *napping.Response
	rl.Call(func() error {
		resp, err = napping.Send(&req)
		if err != nil {
			err = resp.Unmarshal(&code)
			return err
		} else if resp.Status() == 429 {
			log.Warningf("Rate limit exceeded getting Trakt code %s, cooling down...", code)
			rl.CoolDown(resp.HttpResponse().Header)
			return util.ErrExceeded
		} else if resp.Status() == 403 && retriesLeft > 0 {
			retriesLeft--
			code, err = GetCode()
		} else {
			resp.Unmarshal(&code)
		}

		return nil
	})
	if err == nil && resp.Status() != 200 {
		err = fmt.Errorf("Unable to get Trakt code: %d", resp.Status())
	}
	return
}

// GetToken ...
func GetToken(code string) (resp *napping.Response, err error) {
	endPoint := "oauth/device/token"
	header := http.Header{
		"Content-type": []string{"application/json"},
		"User-Agent":   []string{UserAgent},
		"Cookie":       []string{Cookies},
	}
	params := napping.Params{
		"code":          code,
		"client_id":     config.TraktWriteClientID,
		"client_secret": config.TraktWriteClientSecret,
	}.AsUrlValues()

	req := napping.Request{
		Url:    fmt.Sprintf("%s/%s", APIURL, endPoint),
		Method: "POST",
		Params: &params,
		Header: &header,
	}

	rl.Call(func() error {
		resp, err = napping.Send(&req)
		if err != nil {
			return err
		} else if resp.Status() == 429 {
			log.Warningf("Rate limit exceeded getting Trakt token with code %s, cooling down...", code)
			rl.CoolDown(resp.HttpResponse().Header)
			return util.ErrExceeded
		} else if resp.Status() == 403 && retriesLeft > 0 {
			retriesLeft--
			resp, err = GetToken(code)
		}

		return nil
	})
	return
}

// PollToken ...
func PollToken(code *Code) (token *Token, err error) {
	startInterval := code.Interval
	interval := time.NewTicker(time.Duration(startInterval) * time.Second)
	defer interval.Stop()
	expired := time.NewTicker(time.Duration(code.ExpiresIn) * time.Second)
	defer expired.Stop()

	for {
		select {
		case <-interval.C:
			resp, errGet := GetToken(code.DeviceCode)
			if errGet != nil {
				return nil, errGet
			}
			if resp.Status() == 200 {
				resp.Unmarshal(&token)
				return token, err
			} else if resp.Status() == 400 {
				break
			} else if resp.Status() == 404 {
				err = errors.New("Invalid device code")
				return nil, err
			} else if resp.Status() == 409 {
				err = errors.New("Code already used")
				return nil, err
			} else if resp.Status() == 410 {
				err = errors.New("Code expired")
				return nil, err
			} else if resp.Status() == 418 {
				err = errors.New("Code denied")
				return nil, err
			} else if resp.Status() == 429 {
				// err = errors.New("Polling too quickly.")
				interval.Stop()
				interval = time.NewTicker(time.Duration(startInterval+5) * time.Second)
				break
			}

		case <-expired.C:
			err = errors.New("Code expired, please try again")
			return nil, err
		}
	}
}

// RefreshToken ...
func RefreshToken() (resp *napping.Response, err error) {
	endPoint := "oauth/token"
	header := http.Header{
		"Content-type": []string{"application/json"},
		"User-Agent":   []string{UserAgent},
		"Cookie":       []string{Cookies},
	}
	params := napping.Params{
		"refresh_token": config.Get().TraktRefreshToken,
		"client_id":     config.TraktWriteClientID,
		"client_secret": config.TraktWriteClientSecret,
		"redirect_uri":  "urn:ietf:wg:oauth:2.0:oob",
		"grant_type":    "refresh_token",
	}.AsUrlValues()

	req := napping.Request{
		Url:    fmt.Sprintf("%s/%s", APIURL, endPoint),
		Method: "POST",
		Params: &params,
		Header: &header,
	}

	resp, err = napping.Send(&req)
	if err != nil {
		return
	} else if resp.Status() == 403 && retriesLeft > 0 {
		retriesLeft--
		resp, err = RefreshToken()
	}
	return
}

// TokenRefreshHandler ...
func TokenRefreshHandler() {
	if config.Get().TraktToken == "" {
		return
	}

	var token *Token
	ticker := time.NewTicker(12 * time.Hour)
	for {
		select {
		case <-ticker.C:
			if time.Now().Unix() > int64(config.Get().TraktTokenExpiry)-int64(259200) {
				resp, err := RefreshToken()
				if err != nil {
					xbmc.Notify("Elementum", err.Error(), config.AddonIcon())
					log.Error(err)
					return
				}

				if resp.Status() == 200 {
					if errUnm := resp.Unmarshal(&token); errUnm != nil {
						xbmc.Notify("Elementum", errUnm.Error(), config.AddonIcon())
						log.Error(errUnm)
					} else {
						expiry := time.Now().Unix() + int64(token.ExpiresIn)
						xbmc.SetSetting("trakt_token_expiry", strconv.Itoa(int(expiry)))
						xbmc.SetSetting("trakt_token", token.AccessToken)
						xbmc.SetSetting("trakt_refresh_token", token.RefreshToken)
						log.Noticef("Token refreshed for Trakt authorization, next refresh in %s", time.Duration(token.ExpiresIn-259200)*time.Second)
					}
				} else {
					err = fmt.Errorf("Bad status while refreshing Trakt token: %d", resp.Status())
					xbmc.Notify("Elementum", err.Error(), config.AddonIcon())
					log.Error(err)
				}
			}
		}
	}
}

// Authorize ...
func Authorize(fromSettings bool) error {
	code, err := GetCode()
	if err != nil {
		log.Error("Could not get authorization code from Trakt.tv: %s", err)
		xbmc.Notify("Elementum", err.Error(), config.AddonIcon())
		return err
	}
	log.Noticef("Got code for %s: %s", code.VerificationURL, code.UserCode)

	go func(code *Code) {
		cl := broadcast.Closer.C()
		tick := time.NewTicker(time.Duration(5) * time.Second)
		defer tick.Stop()

		attempts := 0

		for {
			select {
			case <-cl:
				log.Error("Cancelling authorization due to closing application state")
				return

			case <-tick.C:
				attempts++

				if attempts > 30 {
					xbmc.Notify("Elementum", "LOCALIZE[30651]", config.AddonIcon())
					return
				}

				token, err := PollToken(code)
				log.Debugf("Received token: %#v, error: %s", token, err)

				if err != nil {
					continue
				}

				// Cleanup last activities to force requesting again
				cacheStore := cache.NewDBStore()
				_ = cacheStore.Set(cache.TraktActivitiesKey, "", 1)

				expiry := time.Now().Unix() + int64(token.ExpiresIn)
				xbmc.SetSetting("trakt_token_expiry", strconv.Itoa(int(expiry)))
				xbmc.SetSetting("trakt_token", token.AccessToken)
				xbmc.SetSetting("trakt_refresh_token", token.RefreshToken)

				config.Get().TraktToken = token.AccessToken

				// Getting username for currently authorized user
				params := napping.Params{}.AsUrlValues()
				resp, err := GetWithAuth("users/settings", params)
				if resp.Status() == 200 {
					user := &UserSettings{}
					errJSON := resp.Unmarshal(user)
					if errJSON != nil {
						return
					}

					if user != nil && user.User.Ids.Slug != "" {
						log.Debugf("Setting Trakt Username as %s", user.User.Ids.Slug)
						xbmc.SetSetting("trakt_username", user.User.Ids.Slug)
					}
				}

				config.Reload()

				xbmc.Notify("Elementum", "LOCALIZE[30650]", config.AddonIcon())
				return
			}
		}
	}(code)

	if xbmc.Dialog(xbmc.GetLocalizedString(30646), fmt.Sprintf(xbmc.GetLocalizedString(30649), code.VerificationURL, code.UserCode)) == false {
		return errors.New("Authentication canceled")
	}

	return nil
}

// Deauthorize ...
func Deauthorize(fromSettings bool) error {
	// Cleanup last activities to force requesting again
	cacheStore := cache.NewDBStore()
	_ = cacheStore.Set(cache.TraktActivitiesKey, "", 1)

	xbmc.SetSetting("trakt_token_expiry", "")
	xbmc.SetSetting("trakt_token", "")
	xbmc.SetSetting("trakt_refresh_token", "")
	xbmc.SetSetting("trakt_username", "")

	xbmc.Notify("Elementum", "LOCALIZE[30652]", config.AddonIcon())

	return nil
}

// Authorized ...
func Authorized() error {
	if config.Get().TraktToken == "" {
		err := Authorize(false)
		if err != nil {
			return err
		}
	}
	return nil
}

// Request is a general proxy for making requests
func Request(endPoint string, params napping.Params, isWithAuth bool, isUpdateNeeded bool, cacheKey string, cacheExpiration time.Duration, ret interface{}) error {
	if isWithAuth {
		if err := Authorized(); err != nil {
			return err
		}
	}

	cacheStore := cache.NewDBStore()
	if !isUpdateNeeded {
		if err := cacheStore.Get(cacheKey, &ret); err == nil {
			return nil
		}
	}

	var err error
	var resp *napping.Response

	if isWithAuth {
		resp, err = GetWithAuth(endPoint, params.AsUrlValues())
	} else {
		resp, err = Get(endPoint, params.AsUrlValues())
	}

	if err != nil {
		return err
	} else if resp.Status() != 200 {
		return fmt.Errorf("Bad status getting %s: %d", endPoint, resp.Status())
	}

	if err := resp.Unmarshal(&ret); err != nil {
		log.Warningf("Cannot unmarshal response: %s", err)
		return err
	}

	cacheStore.Set(cacheKey, &ret, cacheExpiration)
	return nil
}

// SyncAddedItem adds item (movie/show) to watchlist or collection
func SyncAddedItem(itemType string, tmdbID string, location int) (resp *napping.Response, err error) {
	list := config.Get().TraktSyncAddedMoviesList
	if itemType == "shows" {
		list = config.Get().TraktSyncAddedShowsList
	}

	if location == 0 {
		return AddToCollection(itemType, tmdbID)
	} else if location == 1 {
		return AddToWatchlist(itemType, tmdbID)
	} else if location == 2 && list != 0 {
		return AddToUserlist(list, itemType, tmdbID)
	}

	return
}

// SyncRemovedItem removes item (movie/show) from watchlist or collection
func SyncRemovedItem(itemType string, tmdbID string, location int) (resp *napping.Response, err error) {
	list := config.Get().TraktSyncRemovedMoviesList
	if itemType == "shows" {
		list = config.Get().TraktSyncRemovedShowsList
	}

	if location == 0 {
		return RemoveFromCollection(itemType, tmdbID)
	} else if location == 1 {
		return RemoveFromWatchlist(itemType, tmdbID)
	} else if location == 2 && list != 0 {
		return RemoveFromUserlist(list, itemType, tmdbID)
	}

	return
}

// AddToWatchlist ...
func AddToWatchlist(itemType string, tmdbID string) (resp *napping.Response, err error) {
	if err := Authorized(); err != nil {
		return nil, err
	}

	endPoint := "sync/watchlist"
	return Post(endPoint, bytes.NewBufferString(fmt.Sprintf(`{"%s": [{"ids": {"tmdb": %s}}]}`, itemType, tmdbID)))
}

// AddToUserlist ...
func AddToUserlist(listID int, itemType string, tmdbID string) (resp *napping.Response, err error) {
	if err := Authorized(); err != nil {
		return nil, err
	}

	id, _ := strconv.Atoi(tmdbID)
	endPoint := fmt.Sprintf("/users/%s/lists/%s/items", config.Get().TraktUsername, strconv.Itoa(listID))
	payload := ListItemsPayload{}
	if itemType == "movies" {
		i := &Movie{}
		i.IDs = &IDs{TMDB: id}
		payload.Movies = append(payload.Movies, i)
	} else if itemType == "shows" {
		i := &Show{}
		i.IDs = &IDs{TMDB: id}
		payload.Shows = append(payload.Shows, i)
	}

	return PostJSON(endPoint, payload)
}

// RemoveFromUserlist ...
func RemoveFromUserlist(listID int, itemType string, tmdbID string) (resp *napping.Response, err error) {
	if err := Authorized(); err != nil {
		return nil, err
	}

	id, _ := strconv.Atoi(tmdbID)
	endPoint := fmt.Sprintf("/users/%s/lists/%s/items/remove", config.Get().TraktUsername, strconv.Itoa(listID))
	payload := ListItemsPayload{}
	if itemType == "movies" {
		i := &Movie{}
		i.IDs = &IDs{TMDB: id}
		payload.Movies = append(payload.Movies, i)
	} else if itemType == "shows" {
		i := &Show{}
		i.IDs = &IDs{TMDB: id}
		payload.Shows = append(payload.Shows, i)
	}

	return PostJSON(endPoint, payload)
}

// RemoveFromWatchlist ...
func RemoveFromWatchlist(itemType string, tmdbID string) (resp *napping.Response, err error) {
	if err := Authorized(); err != nil {
		return nil, err
	}

	endPoint := "sync/watchlist/remove"
	return Post(endPoint, bytes.NewBufferString(fmt.Sprintf(`{"%s": [{"ids": {"tmdb": %s}}]}`, itemType, tmdbID)))
}

// AddToCollection ...
func AddToCollection(itemType string, tmdbID string) (resp *napping.Response, err error) {
	if err := Authorized(); err != nil {
		return nil, err
	}

	endPoint := "sync/collection"
	return Post(endPoint, bytes.NewBufferString(fmt.Sprintf(`{"%s": [{"ids": {"tmdb": %s}}]}`, itemType, tmdbID)))
}

// RemoveFromCollection ...
func RemoveFromCollection(itemType string, tmdbID string) (resp *napping.Response, err error) {
	if err := Authorized(); err != nil {
		return nil, err
	}

	endPoint := "sync/collection/remove"
	return Post(endPoint, bytes.NewBufferString(fmt.Sprintf(`{"%s": [{"ids": {"tmdb": %s}}]}`, itemType, tmdbID)))
}

// SetWatched addes and removes from watched history
func SetWatched(item *WatchedItem) (resp *napping.Response, err error) {
	if err := Authorized(); err != nil {
		return nil, err
	}

	pre := `{"movies": [`
	post := `]}`
	if item.Movie == 0 {
		pre = `{"shows": [`
	}

	query := item.String()
	endPoint := "sync/history"
	if !item.Watched {
		endPoint = "sync/history/remove"
	}

	return Post(endPoint, bytes.NewBufferString(pre+query+post))
}

// SetMultipleWatched adds and removes from watched history
func SetMultipleWatched(items []*WatchedItem) (*HistoryResponse, error) {
	if err := Authorized(); err != nil || len(items) == 0 {
		return nil, err
	}

	pre := `{"movies": [`
	post := `]}`
	if items[0].Movie == 0 {
		pre = `{"shows": [`
	}

	queries := []string{}
	for _, item := range items {
		if item == nil {
			continue
		}
		queries = append(queries, item.String())
	}
	query := strings.Join(queries, ", ")

	endPoint := "sync/history"
	if !items[0].Watched {
		endPoint = "sync/history/remove"
	}

	cache.NewDBStore().Delete(fmt.Sprintf(cache.TraktKey+"%ss.watched", items[0].MediaType))

	log.Debugf("Setting watch state at %s for %d %s items", endPoint, len(items), items[0].MediaType)

	resp, err := Post(endPoint, bytes.NewBufferString(pre+query+post))

	if err != nil {
		log.Warningf("Error getting watched items: %s", err)
		return nil, err
	} else if resp.Status() != 200 && resp.Status() != 201 {
		log.Warningf("Error getting watched items. Status: %d", resp.Status())
		return nil, fmt.Errorf("Bad status setting Trakt watched items: %d", resp.Status())
	}

	stats := HistoryResponse{}
	if err = resp.Unmarshal(&stats); err != nil {
		log.Warning(err)
	} else {
		log.Infof("Statistics for watch state at %s for %d %s items: Added: %#v, Deleted: %#v", endPoint, len(items), items[0].MediaType, stats.Added, stats.Deleted)
	}

	return &stats, err
}

func (item *WatchedItem) String() (query string) {
	watchedAt := fmt.Sprintf(`"watched_at": "%s",`, time.Now().UTC().Format("20060102-15:04:05.000"))
	if !item.WatchedAt.IsZero() {
		watchedAt = fmt.Sprintf(`"watched_at": "%s",`, item.WatchedAt.Format("20060102-15:04:05.000"))
	}

	if item.Movie != 0 {
		query = fmt.Sprintf(`{ %s "ids": {"tmdb": %d }}`, watchedAt, item.Movie)
	} else if item.Episode != 0 && item.Season != 0 && item.Show != 0 {
		query = fmt.Sprintf(`{ "ids": {"tmdb": %d}, "seasons": [{ "number": %d, "episodes": [{%s "number": %d }]}]}`, item.Show, item.Season, watchedAt, item.Episode)
	} else if item.Season != 0 && item.Show != 0 {
		query = fmt.Sprintf(`{ "ids": {"tmdb": %d}, "seasons": [{ %s "number": %d }]}`, item.Show, watchedAt, item.Season)
	} else {
		query = fmt.Sprintf(`{ "ids": {"tmdb": %d}}`, item.Show)
	}

	return
}

// This is commented for future use (if needed)
// // SetMultipleWatched addes and removes list from watched history
// func SetMultipleWatched(watched bool, itemType string, tmdbID []string) (resp *napping.Response, err error) {
// 	if err := Authorized(); err != nil {
// 		return nil, err
// 	}
//
// 	endPoint := "sync/history"
// 	if !watched {
// 		endPoint = "sync/history/remove"
// 	}
//
// 	buf := bytes.NewBuffer([]byte(""))
// 	buf.WriteString(fmt.Sprintf(`{"%ss": [`, itemType))
// 	for _, i := range tmdbID {
// 		buf.WriteString(fmt.Sprintf(`{"ids": {"tmdb": %s}}`, i))
// 	}
// 	buf.WriteString(`]}`)
// 	return Post(endPoint, buf)
// }

// Scrobble ...
func Scrobble(action string, contentType string, tmdbID int, watched float64, runtime float64) {
	if err := Authorized(); err != nil {
		return
	}

	if runtime < 1 || contentType == "search" {
		return
	}

	progress := watched / runtime * 100

	log.Noticef("%s %s: %f%%, watched: %fs, duration: %fs", action, contentType, progress, watched, runtime)

	endPoint := fmt.Sprintf("scrobble/%s", action)
	payload := fmt.Sprintf(`{"%s": {"ids": {"tmdb": %d}}, "progress": %f, "app_version": "%s"}`,
		contentType, tmdbID, progress, util.GetVersion())
	resp, err := Post(endPoint, bytes.NewBufferString(payload))
	if err != nil {
		log.Error(err.Error())
		xbmc.Notify("Elementum", "Scrobble failed, check your logs.", config.AddonIcon())
	} else if resp.Status() != 201 {
		log.Errorf("Failed to scrobble %s #%d to %s at %f: %d", contentType, tmdbID, action, progress, resp.Status())
	}
}

// GetLastActivities ...
func GetLastActivities() (a *UserActivities, err error) {
	if err := Authorized(); err != nil {
		return nil, fmt.Errorf("Not authorized")
	}

	endPoint := "sync/last_activities"

	params := napping.Params{}.AsUrlValues()
	resp, err := GetWithAuth(endPoint, params)

	if err != nil {
		return nil, err
	} else if resp.Status() == 423 {
		return nil, ErrLocked
	} else if resp.Status() != 200 {
		return nil, fmt.Errorf("Bad status getting Trakt activities: %d", resp.Status())
	}

	if err := resp.Unmarshal(&a); err != nil {
		log.Warning(err)
	} else {
		config.Get().TraktAuthorized = true
	}

	return
}

// DiffWatchedShows ...
func DiffWatchedShows(current, previous []*WatchedShow) (diff []*WatchedShow) {
	if current == nil || previous == nil || len(previous) == 0 || len(current) == 0 {
		return
	}

	foundShow := false
	foundSeason := false
	foundEpisode := false

	var show *WatchedShow
	var season *WatchedSeason

	for _, previousShow := range previous {
		foundShow = false
		foundSeason = false
		foundEpisode = false

		show = nil

		for _, currentShow := range current {
			season = nil

			if previousShow.Show.IDs.Trakt == currentShow.Show.IDs.Trakt {
				foundShow = true

				for _, previousSeason := range previousShow.Seasons {
					foundSeason = false
					foundEpisode = false

					for _, currentSeason := range currentShow.Seasons {
						if previousSeason.Number == currentSeason.Number {
							foundSeason = true

							for _, previousEpisode := range previousSeason.Episodes {
								foundEpisode = false

								for _, currentEpisode := range currentSeason.Episodes {
									if previousEpisode.Number == currentEpisode.Number {
										foundEpisode = true
									}
								}

								if !foundEpisode {
									if season == nil {
										season = &WatchedSeason{Number: previousSeason.Number}
									}

									season.Episodes = append(season.Episodes, previousEpisode)
								}
							}
						}
					}

					if !foundSeason {
						season = previousSeason
					}
					if season != nil {
						if show == nil {
							show = &WatchedShow{Show: previousShow.Show}
						}

						show.Seasons = append(show.Seasons, season)
					}
				}
			}
		}

		if !foundShow {
			diff = append(diff, previousShow)
		}
		if show != nil {
			diff = append(diff, show)
		}
	}

	return
}

// DiffWatchedMovies ...
func DiffWatchedMovies(previous, current []*WatchedMovie) []*WatchedMovie {
	ret := make([]*WatchedMovie, 0, len(current))
	found := false
	for _, ce := range current {
		found = false
		for _, pr := range previous {
			if pr.Movie.IDs.Trakt == ce.Movie.IDs.Trakt {
				found = true
				break
			}
		}

		if !found {
			ret = append(ret, ce)
		}
	}

	return ret
}

// DiffMovies ...
func DiffMovies(previous, current []*Movies) []*Movies {
	ret := make([]*Movies, 0, len(current))
	found := false
	for _, ce := range current {
		found = false
		for _, pr := range previous {
			if pr.Movie.IDs.Trakt == ce.Movie.IDs.Trakt {
				found = true
				break
			}
		}

		if found {
			ret = append(ret, ce)
		}
	}

	return ret
}

// NotifyLocked ...
func NotifyLocked() {
	cacheStore := cache.NewDBStore()
	checked := false
	if err := cacheStore.Get(cache.TraktLockedAccountKey, &checked); err == nil {
		return
	}

	cacheStore.Set(cache.TraktLockedAccountKey, checked, cache.TraktLockedAccountExpire)

	xbmc.Dialog("LOCALIZE[30616]", "LOCALIZE[30617]")
}

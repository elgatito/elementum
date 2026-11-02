// Package mdblist implements tracker.Provider for MDBList (https://api.mdblist.com/docs/).
package mdblist

import (
	"bytes"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/goccy/go-json"

	"github.com/elgatito/elementum/config"
	"github.com/elgatito/elementum/tracker"
	"github.com/elgatito/elementum/util/ident"
	"github.com/elgatito/elementum/util/reqapi"
)

const (
	pageLimit = 1000
	// maxPages bounds cursor loops if the API keeps returning a cursor.
	maxPages = 100
	// cacheExpire keeps fetched lists until activities say they changed.
	cacheExpire = 7 * 24 * time.Hour
)

// Provider is the MDBList tracker.
type Provider struct {
	device *tracker.OAuth2Device
}

// NewProvider returns the MDBList tracker.
func NewProvider() *Provider {
	p := &Provider{}
	p.device = &tracker.OAuth2Device{
		API:          reqapi.MDBListAPI,
		ClientID:     config.MDBListAPIClientID,
		Scope:        "write",
		CodeURL:      "/oauth/device-authorization/",
		TokenURL:     "/oauth/token/",
		RevokeURL:    "/oauth/revoke_token/",
		UsernameFunc: p.username,
	}
	return p
}

func (p *Provider) ID() string                     { return "mdblist" }
func (p *Provider) Name() string                   { return "MDBList" }
func (p *Provider) MinSyncInterval() time.Duration { return 15 * time.Minute }

// Authorized is true with a personal API key or an OAuth token.
func (p *Provider) Authorized() bool {
	return config.Get().MDBListAPIKey != "" || tracker.Credentials(p.ID()).Token != ""
}

func (p *Provider) Authorize() error   { return tracker.RunDeviceAuth(p, p.device) }
func (p *Provider) Deauthorize() error { return tracker.DeauthorizeDevice(p, p.device) }

// RefreshToken renews the 30-day OAuth token a day before expiry. API keys do not expire.
func (p *Provider) RefreshToken() error {
	if config.Get().MDBListAPIKey != "" {
		return nil
	}
	return tracker.RefreshDeviceToken(p, p.device, 24*time.Hour)
}

// request calls the API with the API key or the OAuth token.
func (p *Provider) request(method, path string, params url.Values, body any, result any, cached, refresh bool) (*reqapi.Request, error) {
	if params == nil {
		params = url.Values{}
	}
	header := http.Header{}
	if key := config.Get().MDBListAPIKey; key != "" {
		params.Set("apikey", key)
	} else if token := tracker.Credentials(p.ID()).Token; token != "" {
		header.Set("Authorization", "Bearer "+token)
	} else {
		return nil, tracker.ErrNotAuthorized
	}

	req := &reqapi.Request{
		API:              reqapi.MDBListAPI,
		Method:           method,
		URL:              path,
		Params:           params,
		Header:           header,
		Result:           result,
		Description:      "mdblist " + path,
		Cache:            cached,
		CacheExpire:      cacheExpire,
		CacheForceExpire: refresh,
	}
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		header.Set("Content-Type", "application/json")
		req.Payload = bytes.NewBuffer(b)
	}
	return req, req.Do()
}

func (p *Provider) username(token string) (string, error) {
	var u struct {
		Username string `json:"username"`
	}
	req := &reqapi.Request{
		API:         reqapi.MDBListAPI,
		URL:         "/user",
		Params:      url.Values{},
		Header:      http.Header{"Authorization": []string{"Bearer " + token}},
		Result:      &u,
		Description: "mdblist user",
	}
	return u.Username, req.Do()
}

// ids is the id object MDBList sends and accepts.
type ids struct {
	TMDB    int    `json:"tmdb,omitempty"`
	IMDB    string `json:"imdb,omitempty"`
	TVDB    int    `json:"tvdb,omitempty"`
	Trakt   int    `json:"trakt,omitempty"`
	MDBList string `json:"mdblist,omitempty"`
}

func (i ids) tracker() tracker.IDs {
	return tracker.IDs{TMDB: i.TMDB, IMDB: i.IMDB, TVDB: i.TVDB, Trakt: i.Trakt, Native: i.MDBList}
}

func toIDs(i tracker.IDs) ids {
	return ids{TMDB: i.TMDB, IMDB: i.IMDB, TVDB: i.TVDB}
}

// media is a movie or show object inside sync rows.
type media struct {
	Title string `json:"title"`
	Year  int    `json:"year"`
	IDs   ids    `json:"ids"`
}

func (m *media) item(t tracker.MediaType) *tracker.Item {
	return &tracker.Item{Type: t, IDs: m.IDs.tracker(), Title: m.Title, Year: m.Year}
}

func mediaType(t tracker.MediaType) string {
	if t == tracker.MovieType {
		return "movie"
	}
	return "show"
}

// paginate follows cursor pagination, calling fetch with the cursor and expecting it to return the next one.
func paginate(fetch func(cursor string) (next string, err error)) error {
	cursor := ""
	for range maxPages {
		next, err := fetch(cursor)
		if err != nil || next == "" || next == cursor {
			return err
		}
		cursor = next
	}
	return nil
}

func nextCursor(req *reqapi.Request, body string) string {
	if body != "" {
		return body
	}
	if req != nil && req.ResponseHeader != nil {
		return req.ResponseHeader.Get("X-Next-Cursor")
	}
	return ""
}

type pagination struct {
	NextCursor string `json:"next_cursor"`
}

func (p *Provider) LastActivities() (*tracker.Activities, error) {
	var a struct {
		WatchlistedAt    time.Time `json:"watchlisted_at"`
		WatchedAt        time.Time `json:"watched_at"`
		EpisodeWatchedAt time.Time `json:"episode_watched_at"`
		SeasonWatchedAt  time.Time `json:"season_watched_at"`
		CollectedAt      time.Time `json:"collected_at"`
		PausedAt         time.Time `json:"paused_at"`
		EpisodePausedAt  time.Time `json:"episode_paused_at"`
		ListUpdatedAt    time.Time `json:"list_updated_at"`
	}
	if _, err := p.request("GET", "/sync/last_activities", nil, nil, &a, false, false); err != nil {
		return nil, err
	}

	ret := tracker.NewActivities()
	ret.Set(a.WatchedAt, tracker.MoviesWatched)
	ret.Set(a.EpisodeWatchedAt, tracker.EpisodesWatched)
	ret.SetLatest(a.SeasonWatchedAt, tracker.EpisodesWatched)
	ret.Set(a.CollectedAt, tracker.MoviesCollected, tracker.EpisodesCollected)
	ret.Set(a.WatchlistedAt, tracker.MoviesWatchlisted, tracker.ShowsWatchlisted, tracker.SeasonsWatchlisted, tracker.EpisodesWatchlisted)
	ret.Set(a.PausedAt, tracker.MoviesPaused)
	ret.Set(a.EpisodePausedAt, tracker.EpisodesPaused)
	ret.Set(a.ListUpdatedAt, tracker.ListsUpdated)
	return ret, nil
}

// listEntry is an item of a watchlist or a user list. Shows do not always carry ids, so the flat fields are kept too.
type listEntry struct {
	ID          int    `json:"id"`
	Title       string `json:"title"`
	ReleaseYear int    `json:"release_year"`
	IMDBID      string `json:"imdb_id"`
	TVDBID      int    `json:"tvdb_id"`
	IDs         ids    `json:"ids"`
}

func (e *listEntry) item(t tracker.MediaType) *tracker.Item {
	i := e.IDs
	if i.TMDB == 0 {
		i.TMDB = e.ID
	}
	if i.IMDB == "" {
		i.IMDB = e.IMDBID
	}
	if i.TVDB == 0 {
		i.TVDB = e.TVDBID
	}
	return &tracker.Item{Type: t, IDs: i.tracker(), Title: e.Title, Year: e.ReleaseYear}
}

func (p *Provider) listItems(path string, t tracker.MediaType, refresh bool) ([]*tracker.Item, error) {
	var ret []*tracker.Item
	err := paginate(func(cursor string) (string, error) {
		var page struct {
			Movies     []*listEntry `json:"movies"`
			Shows      []*listEntry `json:"shows"`
			NextCursor string       `json:"next_cursor"`
			Pagination pagination   `json:"pagination"`
		}
		params := url.Values{"limit": {strconv.Itoa(pageLimit)}, "mediatype": {mediaType(t)}}
		if cursor != "" {
			params.Set("cursor", cursor)
		}
		req, err := p.request("GET", path, params, nil, &page, true, refresh)
		if err != nil {
			return "", err
		}

		entries := page.Shows
		if t == tracker.MovieType {
			entries = page.Movies
		}
		for _, e := range entries {
			if e != nil {
				ret = append(ret, e.item(t))
			}
		}
		if page.NextCursor == "" {
			page.NextCursor = page.Pagination.NextCursor
		}
		return nextCursor(req, page.NextCursor), nil
	})
	return ret, err
}

func (p *Provider) Watchlist(t tracker.MediaType, refresh bool) ([]*tracker.Item, error) {
	return p.listItems("/watchlist/items/"+mediaType(t), t, refresh)
}

// syncRows reads a /sync endpoint with cursor pagination.
func (p *Provider) syncRows(path, mediatype string, refresh bool, handle func(page *syncPage)) error {
	return paginate(func(cursor string) (string, error) {
		var page syncPage
		params := url.Values{"limit": {strconv.Itoa(pageLimit)}, "mediatype": {mediatype}}
		if cursor != "" {
			params.Set("cursor", cursor)
		}
		req, err := p.request("GET", path, params, nil, &page, true, refresh)
		if err != nil {
			return "", err
		}
		handle(&page)
		return nextCursor(req, page.Pagination.NextCursor), nil
	})
}

type syncRow struct {
	WatchedAt     time.Time `json:"watched_at"`
	LastWatchedAt time.Time `json:"last_watched_at"`
	CollectedAt   time.Time `json:"collected_at"`
	Movie         *media    `json:"movie"`
	Show          *media    `json:"show"`
	Episode       *struct {
		Season int    `json:"season"`
		Number int    `json:"number"`
		IDs    ids    `json:"ids"`
		Show   *media `json:"show"`
	} `json:"episode"`
}

func (r *syncRow) watchedAt() time.Time {
	if r.LastWatchedAt.After(r.WatchedAt) {
		return r.LastWatchedAt
	}
	return r.WatchedAt
}

type syncPage struct {
	Movies     []*syncRow `json:"movies"`
	Shows      []*syncRow `json:"shows"`
	Episodes   []*syncRow `json:"episodes"`
	Pagination pagination `json:"pagination"`
}

func (p *Provider) Collection(t tracker.MediaType, refresh bool) ([]*tracker.Item, error) {
	var ret []*tracker.Item
	err := p.syncRows("/sync/collection", mediaType(t), refresh, func(page *syncPage) {
		rows := page.Shows
		if t == tracker.MovieType {
			rows = page.Movies
		}
		for _, r := range rows {
			m := r.Movie
			if t != tracker.MovieType {
				m = r.Show
			}
			if m != nil {
				i := m.item(t)
				i.ListedAt = r.CollectedAt
				ret = append(ret, i)
			}
		}
	})
	return ret, err
}

func (p *Provider) Watched(t tracker.MediaType, refresh bool) ([]*tracker.WatchedItem, error) {
	if t == tracker.MovieType {
		var ret []*tracker.WatchedItem
		err := p.syncRows("/sync/watched", "movie", refresh, func(page *syncPage) {
			for _, r := range page.Movies {
				if r.Movie != nil {
					ret = append(ret, &tracker.WatchedItem{Item: *r.Movie.item(tracker.MovieType), Plays: 1, LastWatchedAt: r.watchedAt()})
				}
			}
		})
		return ret, err
	}

	// Episode rows carry their show, so watched shows are built by grouping episodes
	shows := map[string]*tracker.WatchedItem{}
	var order []string
	err := p.syncRows("/sync/watched", "episode", refresh, func(page *syncPage) {
		for _, r := range page.Episodes {
			if r.Episode == nil || r.Episode.Show == nil {
				continue
			}
			show := r.Episode.Show.item(tracker.ShowType)
			key := show.Key()
			w, ok := shows[key]
			if !ok {
				w = &tracker.WatchedItem{Item: *show}
				shows[key] = w
				order = append(order, key)
			}
			at := r.watchedAt()
			w.Episodes = append(w.Episodes, tracker.WatchedEpisode{Season: r.Episode.Season, Number: r.Episode.Number, Plays: 1, LastWatchedAt: at})
			w.Plays++
			if at.After(w.LastWatchedAt) {
				w.LastWatchedAt = at
			}
		}
	})

	ret := make([]*tracker.WatchedItem, 0, len(order))
	for _, k := range order {
		ret = append(ret, shows[k])
	}
	return ret, err
}

func (p *Provider) Paused(t tracker.MediaType, refresh bool) ([]*tracker.PausedItem, error) {
	var sessions []struct {
		Progress float64   `json:"progress"`
		Runtime  int       `json:"runtime"` // minutes
		PausedAt time.Time `json:"paused_at"`
		Type     string    `json:"type"`
		Movie    *media    `json:"movie"`
		Show     *media    `json:"show"`
		Episode  *struct {
			Season int `json:"season"`
			Number int `json:"number"`
			IDs    ids `json:"ids"`
		} `json:"episode"`
	}
	if _, err := p.request("GET", "/sync/playback", nil, nil, &sessions, true, refresh); err != nil {
		return nil, err
	}

	var ret []*tracker.PausedItem
	for _, s := range sessions {
		var i *tracker.Item
		switch {
		case t == tracker.MovieType && s.Type == "movie" && s.Movie != nil:
			i = s.Movie.item(tracker.MovieType)
		case t != tracker.MovieType && s.Type == "episode" && s.Show != nil && s.Episode != nil:
			i = s.Show.item(tracker.EpisodeType)
			i.Season, i.Episode, i.EpisodeTMDB = s.Episode.Season, s.Episode.Number, s.Episode.IDs.TMDB
		default:
			continue
		}
		ret = append(ret, &tracker.PausedItem{Item: *i, Progress: s.Progress, Runtime: s.Runtime * 60, PausedAt: s.PausedAt})
	}
	return ret, nil
}

// Progress returns the next unwatched episode of every show in progress, from /upnext.
func (p *Provider) Progress(refresh bool) ([]*tracker.Item, error) {
	const limit = 100
	var ret []*tracker.Item
	for offset := 0; offset < limit*maxPages; offset += limit {
		var page struct {
			Items []struct {
				Show        *media `json:"show"`
				NextEpisode *struct {
					Season  int `json:"season"`
					Episode int `json:"episode"`
					IDs     ids `json:"ids"`
				} `json:"next_episode"`
			} `json:"items"`
		}
		params := url.Values{"limit": {strconv.Itoa(limit)}, "offset": {strconv.Itoa(offset)}}
		if _, err := p.request("GET", "/upnext", params, nil, &page, true, refresh); err != nil {
			return ret, err
		}

		for _, i := range page.Items {
			if i.Show == nil || i.NextEpisode == nil || i.NextEpisode.Episode == 0 {
				continue
			}
			item := i.Show.item(tracker.EpisodeType)
			item.Season, item.Episode, item.EpisodeTMDB = i.NextEpisode.Season, i.NextEpisode.Episode, i.NextEpisode.IDs.TMDB
			ret = append(ret, item)
		}
		if len(page.Items) < limit {
			break
		}
	}
	return ret, nil
}

func (p *Provider) Lists() ([]*tracker.List, error) {
	var lists []struct {
		ID    int    `json:"id"`
		Name  string `json:"name"`
		Items int    `json:"items"`
	}
	if _, err := p.request("GET", "/lists/user", nil, nil, &lists, false, false); err != nil {
		return nil, err
	}

	ret := make([]*tracker.List, 0, len(lists))
	for _, l := range lists {
		ret = append(ret, &tracker.List{ID: strconv.Itoa(l.ID), Name: l.Name, ItemCount: l.Items})
	}
	return ret, nil
}

func (p *Provider) ListItems(listID string, t tracker.MediaType, refresh bool) ([]*tracker.Item, error) {
	return p.listItems(fmt.Sprintf("/lists/%s/items", url.PathEscape(listID)), t, refresh)
}

// byType splits items into the movies and shows arrays MDBList write endpoints take.
func byType[T any](items []*tracker.Item, conv func(*tracker.Item) T) map[string][]T {
	ret := map[string][]T{}
	for _, i := range items {
		key := "shows"
		if i.Type == tracker.MovieType {
			key = "movies"
		}
		ret[key] = append(ret[key], conv(i))
	}
	return ret
}

func (p *Provider) Add(loc tracker.Location, listID string, items []*tracker.Item) error {
	return p.write(loc, listID, items, "add")
}

func (p *Provider) Remove(loc tracker.Location, listID string, items []*tracker.Item) error {
	return p.write(loc, listID, items, "remove")
}

type idsBody struct {
	IDs ids `json:"ids"`
}

func (p *Provider) write(loc tracker.Location, listID string, items []*tracker.Item, action string) error {
	withIDs := func(i *tracker.Item) idsBody { return idsBody{IDs: toIDs(i.IDs)} }

	var path string
	var body any
	switch loc {
	case tracker.LocationWatchlist:
		path, body = "/watchlist/items/"+action, byType(items, withIDs)
	case tracker.LocationCollection:
		path, body = "/sync/collection", byType(items, withIDs)
		if action == "remove" {
			path = "/sync/collection/remove"
		}
	case tracker.LocationList:
		// list endpoints take flat id objects
		path = fmt.Sprintf("/lists/%s/items/%s", url.PathEscape(listID), action)
		body = byType(items, func(i *tracker.Item) ids { return toIDs(i.IDs) })
	default:
		return tracker.ErrUnsupported
	}

	_, err := p.request("POST", path, nil, body, nil, false, false)
	return err
}

type watchedEpisode struct {
	Number    int    `json:"number"`
	WatchedAt string `json:"watched_at,omitempty"`
}

type watchedSeason struct {
	Number   int              `json:"number"`
	Episodes []watchedEpisode `json:"episodes,omitempty"`
}

type watchedEntry struct {
	IDs       ids             `json:"ids"`
	WatchedAt string          `json:"watched_at,omitempty"`
	Seasons   []watchedSeason `json:"seasons,omitempty"`
}

// watchedBody builds a Trakt-style history payload. Episodes are grouped under their show and season.
// Items keep their date only when known, so re-sending a library never creates duplicate plays.
func watchedBody(items []*tracker.WatchedItem) map[string][]*watchedEntry {
	date := func(t time.Time) string {
		if t.IsZero() {
			return ""
		}
		return t.UTC().Format(time.RFC3339)
	}

	body := map[string][]*watchedEntry{}
	shows := map[string]*watchedEntry{}
	for _, i := range items {
		if i.Type == tracker.MovieType {
			body["movies"] = append(body["movies"], &watchedEntry{IDs: toIDs(i.IDs), WatchedAt: date(i.LastWatchedAt)})
			continue
		}

		key := (&tracker.Item{Type: tracker.ShowType, IDs: i.IDs}).Key()
		show, ok := shows[key]
		if !ok {
			show = &watchedEntry{IDs: toIDs(i.IDs)}
			shows[key] = show
			body["shows"] = append(body["shows"], show)
		}
		switch i.Type {
		case tracker.ShowType:
			show.WatchedAt = date(i.LastWatchedAt)
		case tracker.SeasonType:
			show.Seasons = append(show.Seasons, watchedSeason{Number: i.Season})
		case tracker.EpisodeType:
			ep := watchedEpisode{Number: i.Episode, WatchedAt: date(i.LastWatchedAt)}
			found := false
			for n := range show.Seasons {
				if show.Seasons[n].Number == i.Season && len(show.Seasons[n].Episodes) > 0 {
					show.Seasons[n].Episodes = append(show.Seasons[n].Episodes, ep)
					found = true
					break
				}
			}
			if !found {
				show.Seasons = append(show.Seasons, watchedSeason{Number: i.Season, Episodes: []watchedEpisode{ep}})
			}
		}
	}
	return body
}

func (p *Provider) SetWatched(items []*tracker.WatchedItem, watched bool) error {
	if len(items) == 0 {
		return nil
	}
	path := "/sync/watched"
	if !watched {
		path = "/sync/watched/remove"
	}
	_, err := p.request("POST", path, nil, watchedBody(items), nil, false, false)
	return err
}

func (p *Provider) Scrobble(action tracker.ScrobbleAction, item *tracker.Item, progress float64) error {
	body := map[string]any{
		"progress":    progress,
		"app_name":    "Elementum",
		"app_version": ident.GetVersion(),
	}
	switch item.Type {
	case tracker.MovieType:
		body["movie"] = idsBody{IDs: toIDs(item.IDs)}
	case tracker.EpisodeType:
		body["show"] = map[string]any{"ids": toIDs(item.IDs), "season": item.Season, "episode": item.Episode}
	default:
		return tracker.ErrUnsupported
	}

	_, err := p.request("POST", "/scrobble/"+string(action), nil, body, nil, false, false)
	return err
}

// Package scrob implements tracker.Provider for a self-hosted Scrob server (https://github.com/ellite/scrob).
//
// Scrob reports no activity timestamps, so every sync reads everything again. It has no watchlist, so a user
// list named "Watchlist" stands in for it. Its collection is read-only here: Scrob accepts collection writes
// only from its own web session, not from device tokens or API keys.
package scrob

import (
	"bytes"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/goccy/go-json"

	"github.com/elgatito/elementum/config"
	"github.com/elgatito/elementum/tmdb"
	"github.com/elgatito/elementum/tracker"
	"github.com/elgatito/elementum/util/reqapi"
	"github.com/elgatito/elementum/xbmc"
)

const (
	watchlistName = "Watchlist"
	pageSize      = 100
	// maxPages bounds page loops if the server keeps reporting more pages.
	maxPages = 1000
	source   = "elementum"
)

// Provider is the Scrob tracker.
type Provider struct{}

// NewProvider returns the Scrob tracker.
func NewProvider() *Provider { return &Provider{} }

func (p *Provider) ID() string                     { return "scrob" }
func (p *Provider) Name() string                   { return "Scrob" }
func (p *Provider) MinSyncInterval() time.Duration { return 15 * time.Minute }

// base is the API root of the configured server; the web app serves the backend under /api/proxy.
func base() string {
	return strings.TrimRight(config.Get().ScrobURL, "/") + "/api/proxy"
}

// Authorized is true with a server address and either an API key or a device token.
func (p *Provider) Authorized() bool {
	c := config.Get()
	return c.ScrobURL != "" && (c.ScrobAPIKey != "" || tracker.Credentials(p.ID()).Token != "")
}

func (p *Provider) Authorize() error {
	// the address may have just been typed in settings, before the daemon reloaded them
	if xbmcHost, _ := xbmc.GetLocalXBMCHost(); xbmcHost != nil {
		if u := strings.TrimSpace(xbmcHost.GetSettingString("scrob_url")); u != "" {
			config.Get().ScrobURL = u
		}
	}
	return tracker.RunDeviceAuth(p, p.flow())
}

func (p *Provider) Deauthorize() error { return tracker.DeauthorizeDevice(p, p.flow()) }

// RefreshToken renews the 24-hour device token two hours before expiry. API keys do not expire.
func (p *Provider) RefreshToken() error {
	if config.Get().ScrobAPIKey != "" {
		return nil
	}
	return tracker.RefreshDeviceToken(p, p.flow(), 2*time.Hour)
}

// device is the RFC 8628 flow of Scrob. Built per call, as the server address can change in settings.
type device struct {
	*tracker.OAuth2Device
}

func (p *Provider) flow() *device {
	// Scrob needs no client id and has no revoke endpoint for device tokens
	return &device{&tracker.OAuth2Device{API: reqapi.ScrobAPI, TokenURL: base() + "/auth/device/token", UsernameFunc: p.username}}
}

// RequestCode posts JSON: Scrob reads the code request as a JSON body, unlike the form-encoded token request.
func (d *device) RequestCode() (*tracker.DeviceCode, error) {
	var c struct {
		DeviceCode      string `json:"device_code"`
		UserCode        string `json:"user_code"`
		VerificationURI string `json:"verification_uri"`
		ExpiresIn       int    `json:"expires_in"`
		Interval        int    `json:"interval"`
	}
	req := &reqapi.Request{
		API:         reqapi.ScrobAPI,
		Method:      "POST",
		URL:         base() + "/auth/device/code",
		Params:      url.Values{},
		Header:      http.Header{"Content-Type": []string{"application/json"}},
		Payload:     bytes.NewBufferString(`{"client_name":"Elementum","scope":"write"}`),
		Result:      &c,
		Description: "scrob device code",
	}
	if err := req.Do(); err != nil {
		return nil, err
	}
	if c.DeviceCode == "" {
		return nil, errors.New("empty device code")
	}
	return &tracker.DeviceCode{DeviceCode: c.DeviceCode, UserCode: c.UserCode, VerificationURL: c.VerificationURI, ExpiresIn: c.ExpiresIn, Interval: c.Interval}, nil
}

func (p *Provider) username(token string) (string, error) {
	var u struct {
		DisplayName string `json:"display_name"`
	}
	req := &reqapi.Request{
		API:         reqapi.ScrobAPI,
		URL:         base() + "/profile/me",
		Params:      url.Values{},
		Header:      http.Header{"Authorization": []string{"Bearer " + token}},
		Result:      &u,
		Description: "scrob profile",
	}
	return u.DisplayName, req.Do()
}

// request calls the API with the API key or the device token. Status codes in ignore count as success.
func (p *Provider) request(method, path string, params url.Values, body, result any, ignore ...int) error {
	if params == nil {
		params = url.Values{}
	}
	header := http.Header{}
	if key := config.Get().ScrobAPIKey; key != "" {
		header.Set("X-Api-Key", key)
	} else if token := tracker.Credentials(p.ID()).Token; token != "" {
		header.Set("Authorization", "Bearer "+token)
	} else {
		return tracker.ErrNotAuthorized
	}

	req := &reqapi.Request{
		API:            reqapi.ScrobAPI,
		Method:         method,
		URL:            base() + path,
		Params:         params,
		Header:         header,
		Result:         result,
		Description:    "scrob " + path,
		ResponseIgnore: ignore,
	}
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		header.Set("Content-Type", "application/json")
		req.Payload = bytes.NewBuffer(b)
	}
	return req.Do()
}

// stamp reads Scrob timestamps: ISO 8601 in UTC, usually without a zone.
type stamp struct{ time.Time }

func (s *stamp) UnmarshalJSON(b []byte) error {
	var v string
	if err := json.Unmarshal(b, &v); err != nil || v == "" {
		return nil // null or not a string: unknown date
	}
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02T15:04:05.999999999"} {
		if t, err := time.Parse(layout, v); err == nil {
			s.Time = t.UTC()
			return nil
		}
	}
	return fmt.Errorf("unknown time format %q", v)
}

// media is the media object of every Scrob response. Episodes carry their show in show_* fields.
type media struct {
	TMDB        int    `json:"tmdb_id"`
	TVDB        int    `json:"tvdb_id"`
	IMDB        string `json:"imdb_id"`
	Type        string `json:"type"` // movie, series or episode
	Title       string `json:"title"`
	ReleaseDate string `json:"release_date"`
	Season      int    `json:"season_number"`
	Episode     int    `json:"episode_number"`
	Runtime     int    `json:"runtime"` // minutes
	ShowTitle   string `json:"show_title"`
	ShowTMDB    int    `json:"show_tmdb_id"`
	ShowTVDB    int    `json:"show_tvdb_id"`
}

// item converts a row: episodes keep their show's IDs, a series row with a season number is a season.
func (m *media) item() *tracker.Item {
	year := 0
	if len(m.ReleaseDate) >= 4 {
		year, _ = strconv.Atoi(m.ReleaseDate[:4])
	}
	switch m.Type {
	case "movie":
		return &tracker.Item{Type: tracker.MovieType, IDs: tracker.IDs{TMDB: m.TMDB, IMDB: m.IMDB, TVDB: m.TVDB}, Title: m.Title, Year: year}
	case "episode":
		return &tracker.Item{Type: tracker.EpisodeType, IDs: tracker.IDs{TMDB: m.ShowTMDB, TVDB: m.ShowTVDB}, Title: m.ShowTitle,
			Season: m.Season, Episode: m.Episode, EpisodeTMDB: m.TMDB}
	}
	i := &tracker.Item{Type: tracker.ShowType, IDs: tracker.IDs{TMDB: m.TMDB, IMDB: m.IMDB, TVDB: m.TVDB}, Title: m.Title, Year: year}
	if m.Season > 0 {
		i.Type, i.Season = tracker.SeasonType, m.Season
	}
	return i
}

// show returns the show of an episode or series row.
func (m *media) show() *tracker.Item {
	i := m.item()
	if i.Type == tracker.EpisodeType || i.Type == tracker.SeasonType {
		i.Type, i.Season, i.Episode, i.EpisodeTMDB = tracker.ShowType, 0, 0, 0
	}
	return i
}

// event is a watch event or a playback progress row.
type event struct {
	Media           media   `json:"media"`
	WatchedAt       stamp   `json:"watched_at"`
	ProgressPercent float64 `json:"progress_percent"` // 0-1
	PlayCount       int     `json:"play_count"`
}

// pages reads every page of an endpoint answering {"results": [...], "total_pages": n}.
func pages[T any](p *Provider, path string, params url.Values, handle func([]*T)) error {
	params.Set("page_size", strconv.Itoa(pageSize))
	for n := 1; n <= maxPages; n++ {
		var page struct {
			Results    []*T `json:"results"`
			TotalPages int  `json:"total_pages"`
		}
		params.Set("page", strconv.Itoa(n))
		if err := p.request("GET", path, params, nil, &page); err != nil {
			return err
		}
		handle(page.Results)
		if n >= page.TotalPages {
			break
		}
	}
	return nil
}

// LastActivities has nothing to report: absent buckets make every sync read everything.
func (p *Provider) LastActivities() (*tracker.Activities, error) {
	return tracker.NewActivities(), nil
}

// Watched reads the whole watch history, newest first.
func (p *Provider) Watched(t tracker.MediaType, refresh bool) ([]*tracker.WatchedItem, error) {
	kind := "episode"
	if t == tracker.MovieType {
		kind = "movie"
	}
	byKey := map[string]*tracker.WatchedItem{}
	var order []string
	err := pages(p, "/history", url.Values{"type": {kind}}, func(events []*event) {
		for _, e := range events {
			if e == nil {
				continue
			}
			i := e.Media.item()
			if t != tracker.MovieType {
				i = e.Media.show()
			}
			if i.IDs.TMDB == 0 && i.IDs.TVDB == 0 && i.IDs.IMDB == "" {
				continue
			}
			key := i.Key()
			w, ok := byKey[key]
			if !ok {
				w = &tracker.WatchedItem{Item: *i}
				byKey[key] = w
				order = append(order, key)
			}
			plays := max(e.PlayCount, 1)
			w.Plays += plays
			if e.WatchedAt.After(w.LastWatchedAt) {
				w.LastWatchedAt = e.WatchedAt.Time
			}
			if t == tracker.MovieType {
				continue
			}
			found := false
			for n := range w.Episodes {
				if ep := &w.Episodes[n]; ep.Season == e.Media.Season && ep.Number == e.Media.Episode {
					ep.Plays += plays
					if e.WatchedAt.After(ep.LastWatchedAt) {
						ep.LastWatchedAt = e.WatchedAt.Time
					}
					found = true
					break
				}
			}
			if !found {
				w.Episodes = append(w.Episodes, tracker.WatchedEpisode{Season: e.Media.Season, Number: e.Media.Episode, Plays: plays, LastWatchedAt: e.WatchedAt.Time})
			}
		}
	})

	ret := make([]*tracker.WatchedItem, 0, len(order))
	for _, k := range order {
		ret = append(ret, byKey[k])
	}
	return ret, err
}

// Collection reads collected movies, or the shows of collected series and episodes.
func (p *Provider) Collection(t tracker.MediaType, refresh bool) ([]*tracker.Item, error) {
	kinds := []string{"series", "episode"}
	if t == tracker.MovieType {
		kinds = []string{"movie"}
	}
	seen := map[string]bool{}
	var ret []*tracker.Item
	for _, kind := range kinds {
		err := pages(p, "/media", url.Values{"type": {kind}}, func(rows []*media) {
			for _, m := range rows {
				if m == nil {
					continue
				}
				i := m.show()
				if t == tracker.MovieType {
					i = m.item()
				}
				if key := i.Key(); !seen[key] {
					seen[key] = true
					ret = append(ret, i)
				}
			}
		})
		if err != nil {
			return ret, err
		}
	}
	return ret, nil
}

// Paused reads Continue Watching.
func (p *Provider) Paused(t tracker.MediaType, refresh bool) ([]*tracker.PausedItem, error) {
	var res struct {
		Items []*event `json:"continue_watching"`
	}
	if err := p.request("GET", "/history/continue-watching", url.Values{"limit": {"1000"}}, nil, &res); err != nil {
		return nil, err
	}

	var ret []*tracker.PausedItem
	for _, e := range res.Items {
		if e == nil || (e.Media.Type == "movie") != (t == tracker.MovieType) {
			continue
		}
		ret = append(ret, &tracker.PausedItem{Item: *e.Media.item(), Progress: e.ProgressPercent * 100, Runtime: e.Media.Runtime * 60, PausedAt: e.WatchedAt.Time})
	}
	return ret, nil
}

// Progress reads Next Up: the next episode of every show in progress.
func (p *Provider) Progress(refresh bool) ([]*tracker.Item, error) {
	var res struct {
		Items []*media `json:"next_up"`
	}
	if err := p.request("GET", "/history/next-up", nil, nil, &res); err != nil {
		return nil, err
	}

	ret := make([]*tracker.Item, 0, len(res.Items))
	for _, m := range res.Items {
		if m != nil && m.Type == "episode" {
			ret = append(ret, m.item())
		}
	}
	return ret, nil
}

type list struct {
	ID        int    `json:"id"`
	Name      string `json:"name"`
	ItemCount int    `json:"item_count"`
	UpdatedAt stamp  `json:"updated_at"`
}

func (p *Provider) allLists() ([]*list, error) {
	var res struct {
		Lists []*list `json:"lists"`
	}
	err := p.request("GET", "/lists", nil, nil, &res)
	return res.Lists, err
}

// Lists returns user lists except the one standing in for the watchlist.
func (p *Provider) Lists() ([]*tracker.List, error) {
	lists, err := p.allLists()
	if err != nil {
		return nil, err
	}
	ret := make([]*tracker.List, 0, len(lists))
	for _, l := range lists {
		if l != nil && !strings.EqualFold(l.Name, watchlistName) {
			ret = append(ret, &tracker.List{ID: strconv.Itoa(l.ID), Name: l.Name, ItemCount: l.ItemCount, UpdatedAt: l.UpdatedAt.Time})
		}
	}
	return ret, nil
}

// watchlistID returns the id of the "Watchlist" list, creating it when asked. Empty when it does not exist.
func (p *Provider) watchlistID(create bool) (string, error) {
	lists, err := p.allLists()
	if err != nil {
		return "", err
	}
	for _, l := range lists {
		if l != nil && strings.EqualFold(l.Name, watchlistName) {
			return strconv.Itoa(l.ID), nil
		}
	}
	if !create {
		return "", nil
	}

	var l list
	if err := p.request("POST", "/lists", nil, map[string]string{"name": watchlistName}, &l); err != nil {
		return "", err
	}
	return strconv.Itoa(l.ID), nil
}

type listItem struct {
	ID      int    `json:"id"`
	AddedAt stamp  `json:"added_at"`
	Media   *media `json:"media"`
}

func (p *Provider) listEntries(listID string) ([]*listItem, error) {
	var res struct {
		Items []*listItem `json:"items"`
	}
	err := p.request("GET", "/lists/"+url.PathEscape(listID), nil, nil, &res)
	return res.Items, err
}

// ListItems returns the movies, or the shows, of a list. Season and episode entries are skipped.
func (p *Provider) ListItems(listID string, t tracker.MediaType, refresh bool) ([]*tracker.Item, error) {
	entries, err := p.listEntries(listID)
	if err != nil {
		return nil, err
	}
	var ret []*tracker.Item
	for _, e := range entries {
		if e == nil || e.Media == nil {
			continue
		}
		if i := e.Media.item(); i.Type == t {
			i.ListedAt = e.AddedAt.Time
			ret = append(ret, i)
		}
	}
	return ret, nil
}

func (p *Provider) Watchlist(t tracker.MediaType, refresh bool) ([]*tracker.Item, error) {
	id, err := p.watchlistID(false)
	if err != nil || id == "" {
		return nil, err
	}
	return p.ListItems(id, t, refresh)
}

// listTarget is what an item becomes in a list: a movie, a season, or the show of anything else.
func listTarget(i *tracker.Item) *tracker.Item {
	switch i.Type {
	case tracker.MovieType:
		return &tracker.Item{Type: tracker.MovieType, IDs: i.IDs}
	case tracker.SeasonType:
		return &tracker.Item{Type: tracker.SeasonType, IDs: i.IDs, Season: i.Season}
	}
	return &tracker.Item{Type: tracker.ShowType, IDs: i.IDs}
}

// listID resolves a Location to a list. The collection cannot be written with device tokens or API keys.
func (p *Provider) listID(loc tracker.Location, listID string, create bool) (string, error) {
	switch loc {
	case tracker.LocationWatchlist:
		return p.watchlistID(create)
	case tracker.LocationList:
		return listID, nil
	}
	return "", tracker.ErrUnsupported
}

func (p *Provider) Add(loc tracker.Location, listID string, items []*tracker.Item) error {
	id, err := p.listID(loc, listID, true)
	if err != nil {
		return err
	}

	path := "/lists/" + url.PathEscape(id) + "/items"
	for _, i := range items {
		target := listTarget(i)
		body := map[string]any{"media_type": "series"}
		if target.Type == tracker.MovieType {
			body["media_type"] = "movie"
		} else if target.Type == tracker.SeasonType {
			body["season_number"] = target.Season
		}
		if i.IDs.TMDB != 0 {
			body["tmdb_id"] = i.IDs.TMDB
		} else if i.IDs.TVDB != 0 {
			body["tvdb_id"] = i.IDs.TVDB
		}
		// 409: already in the list
		err = errors.Join(err, p.request("POST", path, nil, body, nil, http.StatusConflict))
	}
	return err
}

func (p *Provider) Remove(loc tracker.Location, listID string, items []*tracker.Item) error {
	id, err := p.listID(loc, listID, false)
	if err != nil || id == "" {
		return err
	}
	entries, err := p.listEntries(id)
	if err != nil {
		return err
	}

	byKey := map[string]int{}
	for _, e := range entries {
		if e != nil && e.Media != nil {
			byKey[e.Media.item().Key()] = e.ID
		}
	}
	for _, i := range items {
		if itemID, ok := byKey[listTarget(i).Key()]; ok {
			err = errors.Join(err, p.request("DELETE", fmt.Sprintf("/lists/%s/items/%d", url.PathEscape(id), itemID), nil, nil, nil, http.StatusNotFound))
		}
	}
	return err
}

// episodeTMDB returns the TMDB id of an episode, which Scrob needs to remove its watches.
func episodeTMDB(i *tracker.Item) int {
	if i.EpisodeTMDB != 0 || i.IDs.TMDB == 0 {
		return i.EpisodeTMDB
	}
	if e := tmdb.GetEpisode(i.IDs.TMDB, i.Season, i.Episode, config.Get().Language); e != nil {
		return e.ID
	}
	return 0
}

// SetWatched adds or removes plays one item at a time, as Scrob has no bulk history endpoint.
func (p *Provider) SetWatched(items []*tracker.WatchedItem, watched bool) (err error) {
	for _, i := range items {
		if watched {
			err = errors.Join(err, p.markWatched(i))
		} else {
			err = errors.Join(err, p.markUnwatched(i))
		}
	}
	return
}

func (p *Provider) markWatched(i *tracker.WatchedItem) error {
	body := map[string]any{"source": source}
	switch {
	case !i.LastWatchedAt.IsZero():
		body["watched_at"] = i.LastWatchedAt.UTC().Format(time.RFC3339)
	case i.KodiKey != 0:
		body["watched_at"] = nil // a library item watched at an unknown date; omitted means now
	}
	if i.Type != tracker.MovieType {
		if i.IDs.TMDB != 0 {
			body["series_tmdb_id"] = i.IDs.TMDB
		}
		if i.IDs.TVDB != 0 {
			body["series_tvdb_id"] = i.IDs.TVDB
		}
	}

	path := "/history"
	switch i.Type {
	case tracker.MovieType:
		body["media_type"], body["tmdb_id"] = "movie", i.IDs.TMDB
	case tracker.EpisodeType:
		body["media_type"], body["season_number"], body["episode_number"] = "episode", i.Season, i.Episode
	case tracker.SeasonType:
		path, body["season_number"] = "/history/season", i.Season
	case tracker.ShowType:
		path = "/history/show-all"
	}
	// 409: a play of this item already exists within the server's duplicate window
	return p.request("POST", path, nil, body, nil, http.StatusConflict)
}

func (p *Provider) markUnwatched(i *tracker.WatchedItem) error {
	params := url.Values{}
	if i.IDs.TMDB != 0 {
		params.Set("series_tmdb_id", strconv.Itoa(i.IDs.TMDB))
	}
	if i.IDs.TVDB != 0 {
		params.Set("series_tvdb_id", strconv.Itoa(i.IDs.TVDB))
	}

	path := "/history/show-all"
	switch i.Type {
	case tracker.MovieType:
		path, params = "/history/item", url.Values{"media_type": {"movie"}, "tmdb_id": {strconv.Itoa(i.IDs.TMDB)}}
	case tracker.EpisodeType:
		id := episodeTMDB(&i.Item)
		if id == 0 {
			return fmt.Errorf("no TMDB id for episode %s", i.Key())
		}
		path, params = "/history/item", url.Values{"media_type": {"episode"}, "tmdb_id": {strconv.Itoa(id)}}
	case tracker.SeasonType:
		path = "/history/season"
		params.Set("season_number", strconv.Itoa(i.Season))
	}
	return p.request("DELETE", path, params, nil, nil)
}

// Scrobble drives a Scrob playback session. Starting a session for the same item replaces the previous one,
// so every event starts it again and then sets progress. Stop is sent only for watched items, so it completes.
func (p *Provider) Scrobble(action tracker.ScrobbleAction, item *tracker.Item, progress float64) error {
	start := map[string]any{"media_type": "movie", "tmdb_id": item.IDs.TMDB}
	switch item.Type {
	case tracker.MovieType:
	case tracker.EpisodeType:
		start = map[string]any{"media_type": "episode", "show_tmdb_id": item.IDs.TMDB, "season_number": item.Season, "episode_number": item.Episode}
		if item.EpisodeTMDB != 0 {
			start["tmdb_id"] = item.EpisodeTMDB
		}
	default:
		return tracker.ErrUnsupported
	}

	var s struct {
		SessionKey string `json:"session_key"`
		Runtime    int    `json:"runtime"` // minutes, 0 if unknown
	}
	if err := p.request("POST", "/history/session/start", nil, start, &s); err != nil {
		return err
	}
	path := "/history/session/" + url.PathEscape(s.SessionKey)
	if action == tracker.ScrobbleStop {
		return p.request("POST", path+"/complete", nil, nil, nil)
	}

	state := "playing"
	if action == tracker.ScrobblePause {
		state = "paused"
	}
	seconds := int(progress / 100 * float64(s.Runtime*60))
	return p.request("PATCH", path, nil, map[string]any{"progress_seconds": seconds, "state": state}, nil)
}

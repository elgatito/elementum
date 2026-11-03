// Package simkl implements tracker.Provider for Simkl (https://api.simkl.org/).
//
// Simkl suspends client ids that poll their library endpoints, so the library is pulled in full once
// and then only as deltas (date_from) when /sync/activities moved. See library.go.
package simkl

import (
	"bytes"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/goccy/go-json"
	"github.com/op/go-logging"

	"github.com/elgatito/elementum/config"
	"github.com/elgatito/elementum/tracker"
	"github.com/elgatito/elementum/util/ident"
	"github.com/elgatito/elementum/util/reqapi"
)

var log = logging.MustGetLogger("simkl")

const (
	appName     = "elementum"
	cacheExpire = 7 * 24 * time.Hour
)

// Provider is the Simkl tracker.
type Provider struct {
	device *tracker.OAuth2Device

	mu   sync.Mutex
	acts *activities // read at the start of a sync, the date_from base of the next delta
	lib  *library
}

// NewProvider returns the Simkl tracker.
func NewProvider() *Provider {
	p := &Provider{}
	p.device = &tracker.OAuth2Device{
		API:           reqapi.SimklAPI,
		ClientID:      config.SimklAPIClientID,
		Scope:         "media:read media:write",
		CodeURL:       "/oauth2/device",
		TokenURL:      "/oauth2/token",
		RevokeURL:     "/oauth2/revoke",
		RequiredScope: "media:write",
		Params:        appParams(),
		UsernameFunc:  p.username,
	}
	return p
}

// appParams are required on every Simkl request.
func appParams() url.Values {
	return url.Values{"client_id": {config.SimklAPIClientID}, "app-name": {appName}, "app-version": {ident.GetVersion()}}
}

func (p *Provider) ID() string                     { return "simkl" }
func (p *Provider) Name() string                   { return "Simkl" }
func (p *Provider) MinSyncInterval() time.Duration { return 15 * time.Minute }
func (p *Provider) Authorized() bool               { return tracker.Credentials(p.ID()).Token != "" }
func (p *Provider) Authorize() error               { return tracker.RunDeviceAuth(p, p.device) }
func (p *Provider) Deauthorize() error             { return tracker.DeauthorizeDevice(p, p.device) }

// RefreshToken renews the 7-day token a day before expiry.
func (p *Provider) RefreshToken() error {
	return tracker.RefreshDeviceToken(p, p.device, 24*time.Hour)
}

// request calls the API with the user token. A nil result leaves the raw body in the returned request.
func (p *Provider) request(method, path string, params url.Values, body, result any, cached, refresh bool) (*reqapi.Request, error) {
	return p.requestWithToken(tracker.Credentials(p.ID()).Token, method, path, params, body, result, cached, refresh)
}

func (p *Provider) requestWithToken(token, method, path string, params url.Values, body, result any, cached, refresh bool) (*reqapi.Request, error) {
	if token == "" {
		return nil, tracker.ErrNotAuthorized
	}

	query := appParams()
	for k, v := range params {
		query[k] = v
	}
	header := http.Header{
		"User-Agent":    []string{"Elementum/" + ident.GetVersion()},
		"Authorization": []string{"Bearer " + token},
	}

	req := &reqapi.Request{
		API:              reqapi.SimklAPI,
		Method:           method,
		URL:              path,
		Params:           query,
		Header:           header,
		Result:           result,
		Description:      "simkl " + path,
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

func (p *Provider) settings(token string) (name string, id int, err error) {
	var s struct {
		User struct {
			Name string `json:"name"`
		} `json:"user"`
		Account struct {
			ID int `json:"id"`
		} `json:"account"`
	}
	_, err = p.requestWithToken(token, "GET", "/users/settings", nil, nil, &s, false, false)
	return s.User.Name, s.Account.ID, err
}

func (p *Provider) username(token string) (string, error) {
	name, _, err := p.settings(token)
	return name, err
}

// flexInt decodes ids Simkl sends either as numbers or as strings.
type flexInt int

func (f *flexInt) UnmarshalJSON(b []byte) error {
	s := strings.Trim(string(b), `"`)
	if s == "" || s == "null" {
		*f = 0
		return nil
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		// non-numeric ids (slugs) are not useful here
		*f = 0
		return nil
	}
	*f = flexInt(n)
	return nil
}

type rawIDs struct {
	Simkl   flexInt `json:"simkl"`
	SimklID flexInt `json:"simkl_id"`
	IMDB    string  `json:"imdb"`
	TMDB    flexInt `json:"tmdb"`
	TVDB    flexInt `json:"tvdb"`
}

func (i rawIDs) simkl() int {
	if i.Simkl != 0 {
		return int(i.Simkl)
	}
	return int(i.SimklID)
}

func (i rawIDs) tracker() tracker.IDs {
	ret := tracker.IDs{TMDB: int(i.TMDB), IMDB: i.IMDB, TVDB: int(i.TVDB)}
	if s := i.simkl(); s != 0 {
		ret.Native = strconv.Itoa(s)
	}
	return ret
}

// writeIDs is the id object sent in write bodies; Simkl documents tmdb and tvdb as strings.
type writeIDs struct {
	Simkl int    `json:"simkl,omitempty"`
	IMDB  string `json:"imdb,omitempty"`
	TMDB  string `json:"tmdb,omitempty"`
	TVDB  string `json:"tvdb,omitempty"`
}

func toWriteIDs(i tracker.IDs) writeIDs {
	w := writeIDs{IMDB: i.IMDB}
	w.Simkl, _ = strconv.Atoi(i.Native)
	if i.TMDB != 0 {
		w.TMDB = strconv.Itoa(i.TMDB)
	}
	if i.TVDB != 0 {
		w.TVDB = strconv.Itoa(i.TVDB)
	}
	return w
}

type rawMedia struct {
	Title string `json:"title"`
	Year  int    `json:"year"`
	IDs   rawIDs `json:"ids"`
}

func timeOf(s string) time.Time {
	t, _ := time.Parse(time.RFC3339, s)
	return t
}

func latest(values ...string) time.Time {
	var ret time.Time
	for _, v := range values {
		if t := timeOf(v); t.After(ret) {
			ret = t
		}
	}
	return ret
}

type statusTimes struct {
	All             string `json:"all"`
	Plantowatch     string `json:"plantowatch"`
	Watching        string `json:"watching"`
	Hold            string `json:"hold"`
	Completed       string `json:"completed"`
	Dropped         string `json:"dropped"`
	Playback        string `json:"playback"`
	RemovedFromList string `json:"removed_from_list"`
}

// activities keeps timestamps as returned, because date_from must be sent exactly as Simkl formatted it.
type activities struct {
	All         string      `json:"all"`
	TVShows     statusTimes `json:"tv_shows"`
	Anime       statusTimes `json:"anime"`
	Movies      statusTimes `json:"movies"`
	CustomLists struct {
		Lists struct {
			All string `json:"all"`
		} `json:"lists"`
	} `json:"custom_lists"`
}

// removedMark changes whenever anything was removed from a list, which deltas never report.
func (a *activities) removedMark() string {
	return a.Movies.RemovedFromList + "|" + a.TVShows.RemovedFromList + "|" + a.Anime.RemovedFromList
}

func (a *activities) buckets() *tracker.Activities {
	tv, an, m := a.TVShows, a.Anime, a.Movies
	ret := tracker.NewActivities()
	ret.Set(latest(m.Completed, m.RemovedFromList), tracker.MoviesWatched)
	ret.Set(latest(m.Plantowatch, m.RemovedFromList), tracker.MoviesWatchlisted)
	ret.Set(latest(m.Playback), tracker.MoviesPaused)
	ret.Set(latest(tv.Watching, tv.Completed, tv.Hold, tv.Dropped, tv.RemovedFromList,
		an.Watching, an.Completed, an.Hold, an.Dropped, an.RemovedFromList), tracker.EpisodesWatched)
	ret.Set(latest(tv.Plantowatch, tv.RemovedFromList, an.Plantowatch, an.RemovedFromList),
		tracker.ShowsWatchlisted, tracker.SeasonsWatchlisted, tracker.EpisodesWatchlisted)
	ret.Set(latest(tv.Playback, an.Playback), tracker.EpisodesPaused)
	ret.Set(latest(a.CustomLists.Lists.All), tracker.ListsUpdated)
	return ret
}

func (p *Provider) fetchActivities() (*activities, error) {
	var a activities
	if _, err := p.request("GET", "/sync/activities", nil, nil, &a, false, false); err != nil {
		return nil, err
	}
	return &a, nil
}

// LastActivities reads activities and keeps them as the base for library deltas of this sync.
func (p *Provider) LastActivities() (*tracker.Activities, error) {
	a, err := p.fetchActivities()
	if err != nil {
		return nil, err
	}

	p.mu.Lock()
	p.acts = a
	p.mu.Unlock()
	return a.buckets(), nil
}

func (p *Provider) Watchlist(t tracker.MediaType, refresh bool) ([]*tracker.Item, error) {
	lib, err := p.library(refresh)
	if err != nil {
		return nil, err
	}

	var ret []*tracker.Item
	for _, e := range lib.Entries {
		if e.Status == statusPlanToWatch && e.matches(t) {
			i := e.Item
			ret = append(ret, &i)
		}
	}
	return ret, nil
}

func (p *Provider) Watched(t tracker.MediaType, refresh bool) ([]*tracker.WatchedItem, error) {
	lib, err := p.library(refresh)
	if err != nil {
		return nil, err
	}

	var ret []*tracker.WatchedItem
	for _, e := range lib.Entries {
		if !e.matches(t) {
			continue
		}
		if t == tracker.MovieType {
			if e.Status == statusCompleted {
				ret = append(ret, &tracker.WatchedItem{Item: e.Item, Plays: 1, LastWatchedAt: e.LastWatchedAt})
			}
		} else if len(e.Episodes) > 0 {
			ret = append(ret, &tracker.WatchedItem{Item: e.Item, Plays: len(e.Episodes), LastWatchedAt: e.LastWatchedAt, Episodes: e.Episodes, Watched: e.Status == statusCompleted})
		}
	}
	return ret, nil
}

func (p *Provider) Paused(t tracker.MediaType, refresh bool) ([]*tracker.PausedItem, error) {
	var sessions []struct {
		Progress float64   `json:"progress"`
		PausedAt time.Time `json:"paused_at"`
		Type     string    `json:"type"`
		Movie    *rawMedia `json:"movie"`
		Show     *rawMedia `json:"show"`
		Anime    *rawMedia `json:"anime"`
		Episode  *struct {
			Season int `json:"season"`
			Number int `json:"number"`
		} `json:"episode"`
	}
	if _, err := p.request("GET", "/sync/playback", nil, nil, &sessions, true, refresh); err != nil {
		return nil, err
	}

	var ret []*tracker.PausedItem
	for _, s := range sessions {
		var i *tracker.Item
		show := s.Show
		if show == nil {
			show = s.Anime
		}
		switch {
		case t == tracker.MovieType && s.Type == "movie" && s.Movie != nil:
			i = &tracker.Item{Type: tracker.MovieType, IDs: s.Movie.IDs.tracker(), Title: s.Movie.Title, Year: s.Movie.Year}
		case t != tracker.MovieType && s.Type == "episode" && show != nil && s.Episode != nil:
			i = &tracker.Item{Type: tracker.EpisodeType, IDs: show.IDs.tracker(), Title: show.Title, Year: show.Year, Season: s.Episode.Season, Episode: s.Episode.Number}
		default:
			continue
		}
		// Simkl sends no runtime, the library falls back to TMDB
		ret = append(ret, &tracker.PausedItem{Item: *i, Progress: s.Progress, PausedAt: s.PausedAt})
	}
	return ret, nil
}

// listError interprets custom list responses. Accounts without PRO or VIP get HTTP 200 with
// {"error": "premium_only"} instead of list data, which must not be read as an empty list.
func listError(err error, bodyError string) error {
	if bodyError == "premium_only" {
		return tracker.ErrPaidAccount
	} else if bodyError != "" {
		return fmt.Errorf("simkl custom lists: %s", bodyError)
	}
	return err
}

// Progress returns the next episode of every show in Watching, most recently watched first.
// Shows without a next episode are caught up and left out, as Simkl recommends.
func (p *Provider) Progress(refresh bool) ([]*tracker.Item, error) {
	lib, err := p.library(refresh)
	if err != nil {
		return nil, err
	}
	return progress(lib), nil
}

func progress(lib *library) []*tracker.Item {
	var watching []*entry
	for _, e := range lib.Entries {
		if !e.IsMovie && e.Status == "watching" && e.NextEpisode > 0 {
			watching = append(watching, e)
		}
	}
	slices.SortFunc(watching, func(a, b *entry) int { return b.LastWatchedAt.Compare(a.LastWatchedAt) })

	ret := make([]*tracker.Item, 0, len(watching))
	for _, e := range watching {
		i := e.Item
		i.Type, i.Season, i.Episode = tracker.EpisodeType, e.NextSeason, e.NextEpisode
		ret = append(ret, &i)
	}
	return ret
}

// statusLists are the library statuses, shown as lists like simkl.com "My Movies" and "My TV Shows" do.
var statusLists = []struct{ status, name string }{
	{"watching", "Watching"},
	{statusPlanToWatch, "Plan to Watch"},
	{"hold", "On Hold"},
	{statusCompleted, "Completed"},
	{"dropped", "Dropped"},
}

const statusListPrefix = "status:"

// statusItems returns the library titles of type t with status, most recently watched first.
func statusItems(lib *library, status string, t tracker.MediaType) []*tracker.Item {
	var entries []*entry
	for _, e := range lib.Entries {
		if e.Status == status && e.matches(t) {
			entries = append(entries, e)
		}
	}
	// map order is random, a stable order keeps pages from shuffling
	slices.SortFunc(entries, func(a, b *entry) int {
		if c := b.LastWatchedAt.Compare(a.LastWatchedAt); c != 0 {
			return c
		}
		return strings.Compare(a.Item.Title, b.Item.Title)
	})

	ret := make([]*tracker.Item, 0, len(entries))
	for _, e := range entries {
		i := e.Item
		ret = append(ret, &i)
	}
	return ret
}

// Lists returns the library status lists, then custom lists. Custom lists are a Simkl PRO and VIP feature,
// other accounts get only the status lists.
func (p *Provider) Lists() ([]*tracker.List, error) {
	ret := make([]*tracker.List, 0, len(statusLists))
	for _, s := range statusLists {
		ret = append(ret, &tracker.List{ID: statusListPrefix + s.status, Name: s.name})
	}

	custom, err := p.customLists()
	if errors.Is(err, tracker.ErrPaidAccount) {
		log.Debugf("Simkl custom lists need a PRO or VIP account, showing only status lists")
		return ret, nil
	}
	return append(ret, custom...), err
}

func (p *Provider) customLists() ([]*tracker.List, error) {
	_, userID, err := p.settings(tracker.Credentials(p.ID()).Token)
	if err != nil {
		return nil, err
	}

	var ret []*tracker.List
	for page := 1; ; page++ {
		var resp struct {
			Error      string `json:"error"`
			Pagination struct {
				TotalPages int `json:"total_pages"`
			} `json:"pagination"`
			Lists []struct {
				ID        int    `json:"id"`
				Name      string `json:"name"`
				UpdatedAt string `json:"updated_at"`
				Counts    struct {
					Items int `json:"items"`
				} `json:"counts"`
			} `json:"lists"`
		}
		_, err := p.request("GET", fmt.Sprintf("/lists/user/%d", userID), url.Values{"page": {strconv.Itoa(page)}}, nil, &resp, false, false)
		if err = listError(err, resp.Error); err != nil {
			return nil, err
		}

		for _, l := range resp.Lists {
			ret = append(ret, &tracker.List{ID: strconv.Itoa(l.ID), Name: l.Name, ItemCount: l.Counts.Items, UpdatedAt: timeOf(l.UpdatedAt)})
		}
		if page >= resp.Pagination.TotalPages {
			return ret, nil
		}
	}
}

func (p *Provider) ListItems(listID string, t tracker.MediaType, refresh bool) ([]*tracker.Item, error) {
	if status, ok := strings.CutPrefix(listID, statusListPrefix); ok {
		// always refresh: status changes do not move the lists bucket the caller decides by,
		// and the library only refetches when Simkl activities moved
		lib, err := p.library(true)
		if err != nil {
			return nil, err
		}
		return statusItems(lib, status, t), nil
	}

	var ret []*tracker.Item
	for page := 1; ; page++ {
		var resp struct {
			Error      string `json:"error"`
			Pagination struct {
				TotalPages int `json:"total_pages"`
			} `json:"pagination"`
			Items []struct {
				Title string `json:"title"`
				Year  int    `json:"year"`
				Type  string `json:"type"`
				IDs   rawIDs `json:"ids"`
			} `json:"items"`
		}
		params := url.Values{"page": {strconv.Itoa(page)}, "limit": {"50"}}
		// not cached: a premium_only answer must not stick around after the account is upgraded
		_, err := p.request("GET", "/lists/"+url.PathEscape(listID), params, nil, &resp, false, false)
		if err = listError(err, resp.Error); err != nil {
			return nil, err
		}

		for _, i := range resp.Items {
			isMovie := i.Type == "movie"
			if isMovie != (t == tracker.MovieType) {
				continue
			}
			ret = append(ret, &tracker.Item{Type: t, IDs: i.IDs.tracker(), Title: i.Title, Year: i.Year})
		}
		if page >= resp.Pagination.TotalPages {
			return ret, nil
		}
	}
}

// byType splits items into the movies and shows arrays of Simkl write bodies.
func byType(items []*tracker.Item, conv func(*tracker.Item) any) map[string][]any {
	ret := map[string][]any{}
	for _, i := range items {
		key := "shows"
		if i.Type == tracker.MovieType {
			key = "movies"
		}
		ret[key] = append(ret[key], conv(i))
	}
	return ret
}

// Add supports only the watchlist: Simkl has no collection and custom lists are edited on simkl.com.
func (p *Provider) Add(loc tracker.Location, listID string, items []*tracker.Item) error {
	if loc != tracker.LocationWatchlist {
		return tracker.ErrUnsupported
	}

	body := byType(items, func(i *tracker.Item) any {
		return map[string]any{"to": statusPlanToWatch, "ids": toWriteIDs(i.IDs)}
	})
	_, err := p.request("POST", "/sync/add-to-list", nil, body, nil, false, false)
	return err
}

// Remove supports only the watchlist. Simkl removes items through /sync/history/remove, which also wipes
// watch history, so only items that are in plantowatch (and therefore have no history) are removed.
func (p *Provider) Remove(loc tracker.Location, listID string, items []*tracker.Item) error {
	if loc != tracker.LocationWatchlist {
		return tracker.ErrUnsupported
	}

	lib, err := p.library(false)
	if err != nil {
		return err
	}
	planned := map[string]*tracker.Item{}
	for _, e := range lib.Entries {
		if e.Status == statusPlanToWatch {
			i := e.Item
			planned[i.Key()] = &i
		}
	}

	var remove []*tracker.Item
	for _, i := range items {
		if e, ok := planned[i.Key()]; ok {
			remove = append(remove, e)
		} else {
			log.Debugf("Not removing %s from Simkl: it is not in Plan to Watch", i.Key())
		}
	}
	if len(remove) == 0 {
		return nil
	}

	body := byType(remove, func(i *tracker.Item) any { return map[string]any{"ids": toWriteIDs(i.IDs)} })
	_, err = p.request("POST", "/sync/history/remove", nil, body, nil, false, false)
	return err
}

type historyEpisode struct {
	Number    int    `json:"number"`
	WatchedAt string `json:"watched_at,omitempty"`
}

type historySeason struct {
	Number   int              `json:"number"`
	Episodes []historyEpisode `json:"episodes,omitempty"`
}

type historyEntry struct {
	IDs       writeIDs        `json:"ids"`
	WatchedAt string          `json:"watched_at,omitempty"`
	Seasons   []historySeason `json:"seasons,omitempty"`
}

// historyBody groups episodes under their show and season.
func historyBody(items []*tracker.WatchedItem) map[string][]*historyEntry {
	date := func(t time.Time) string {
		if t.IsZero() {
			return ""
		}
		return t.UTC().Format(time.RFC3339)
	}

	body := map[string][]*historyEntry{}
	shows := map[string]*historyEntry{}
	for _, i := range items {
		if i.Type == tracker.MovieType {
			body["movies"] = append(body["movies"], &historyEntry{IDs: toWriteIDs(i.IDs), WatchedAt: date(i.LastWatchedAt)})
			continue
		}

		key := (&tracker.Item{Type: tracker.ShowType, IDs: i.IDs}).Key()
		show, ok := shows[key]
		if !ok {
			show = &historyEntry{IDs: toWriteIDs(i.IDs)}
			shows[key] = show
			body["shows"] = append(body["shows"], show)
		}
		switch i.Type {
		case tracker.ShowType:
			show.WatchedAt = date(i.LastWatchedAt)
		case tracker.SeasonType:
			show.Seasons = append(show.Seasons, historySeason{Number: i.Season})
		case tracker.EpisodeType:
			ep := historyEpisode{Number: i.Episode, WatchedAt: date(i.LastWatchedAt)}
			found := false
			for n := range show.Seasons {
				if show.Seasons[n].Number == i.Season && len(show.Seasons[n].Episodes) > 0 {
					show.Seasons[n].Episodes = append(show.Seasons[n].Episodes, ep)
					found = true
					break
				}
			}
			if !found {
				show.Seasons = append(show.Seasons, historySeason{Number: i.Season, Episodes: []historyEpisode{ep}})
			}
		}
	}
	return body
}

// SetWatched adds or removes watch history. Unwatching a whole movie or show removes it from the Simkl library,
// as Simkl has no other way to clear a watch.
func (p *Provider) SetWatched(items []*tracker.WatchedItem, watched bool) error {
	if len(items) == 0 {
		return nil
	}
	path := "/sync/history"
	if !watched {
		path = "/sync/history/remove"
	}
	_, err := p.request("POST", path, nil, historyBody(items), nil, false, false)
	return err
}

func (p *Provider) Scrobble(action tracker.ScrobbleAction, item *tracker.Item, progress float64) error {
	body := map[string]any{"progress": progress}
	switch item.Type {
	case tracker.MovieType:
		body["movie"] = map[string]any{"ids": toWriteIDs(item.IDs)}
	case tracker.EpisodeType:
		body["show"] = map[string]any{"ids": toWriteIDs(item.IDs)}
		body["episode"] = map[string]int{"season": item.Season, "number": item.Episode}
	default:
		return tracker.ErrUnsupported
	}

	req, err := p.request("POST", "/scrobble/"+string(action), nil, body, nil, false, false)
	// 409 means the same event was already recorded
	if req != nil && req.ResponseStatusCode == 409 {
		return nil
	}
	return err
}

var errMaxItems = errors.New("too many items for one request")

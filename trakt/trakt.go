package trakt

import (
	"bytes"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/elgatito/elementum/cache"
	"github.com/elgatito/elementum/config"
	"github.com/elgatito/elementum/library/uid"
	"github.com/elgatito/elementum/tracker"
	"github.com/elgatito/elementum/util/ident"
	"github.com/elgatito/elementum/util/reqapi"
	"github.com/elgatito/elementum/xbmc"

	"github.com/goccy/go-json"
	"github.com/jmcvetta/napping"
	"github.com/op/go-logging"
)

const (
	// APIURL ...
	APIURL = "https://api.trakt.tv"
	// AuthURL is used for OAuth requests
	AuthURL = "https://auth.trakt.tv"
	// APIVersion ...
	APIVersion = "2"

	ListsPerPage = 150
)

var log = logging.MustGetLogger("trakt")

var (
	// Cookies ...
	Cookies = ""
	// UserAgent ...
	UserAgent = "Elementum/" + ident.GetVersion()
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

func GetHeader() http.Header {
	return http.Header{
		"Content-type":      []string{"application/json"},
		"trakt-api-key":     []string{config.TraktAPIClientID},
		"trakt-api-version": []string{APIVersion},
		"User-Agent":        []string{UserAgent},
		"Cookie":            []string{Cookies},
	}
}

func GetAuthenticatedHeader() http.Header {
	return http.Header{
		"Content-type":      []string{"application/json"},
		"Authorization":     []string{fmt.Sprintf("Bearer %s", config.Get().TraktToken)},
		"trakt-api-key":     []string{config.TraktAPIClientID},
		"trakt-api-version": []string{APIVersion},
		"User-Agent":        []string{UserAgent},
		"Cookie":            []string{Cookies},
	}
}

func GetAvailableHeader() http.Header {
	if config.Get().TraktAuthorized {
		return GetAuthenticatedHeader()
	}
	return GetHeader()
}

// deviceFlow is the Trakt OAuth device-code protocol for tracker.RunDeviceAuth.
type deviceFlow struct{}

// authURL is a var for tests.
var authURL = AuthURL

func (deviceFlow) oauth(url, payload, description string, result any) (*reqapi.Request, error) {
	req := &reqapi.Request{
		API:         reqapi.TraktAPI,
		Method:      "POST",
		URL:         authURL + url,
		Header:      GetHeader(),
		Params:      napping.Params{}.AsUrlValues(),
		Payload:     bytes.NewBufferString(payload),
		Result:      result,
		Description: description,
	}
	return req, req.Do()
}

func toTracker(t *Token) *tracker.Token {
	return &tracker.Token{AccessToken: t.AccessToken, RefreshToken: t.RefreshToken, ExpiresIn: t.ExpiresIn}
}

func (f deviceFlow) RequestCode() (*tracker.DeviceCode, error) {
	var code *Code
	if _, err := f.oauth("/oauth/device/code", fmt.Sprintf(`{"client_id": %q}`, config.TraktAPIClientID), "oauth device code", &code); err != nil {
		return nil, err
	} else if code == nil {
		return nil, errors.New("no authorization code")
	}
	return (*tracker.DeviceCode)(code), nil
}

func (f deviceFlow) PollToken(code *tracker.DeviceCode) (*tracker.Token, error) {
	var token *Token
	req, _ := f.oauth("/oauth/device/token", fmt.Sprintf(`{"code": %q, "client_id": %q, "client_secret": %q}`, code.DeviceCode, config.TraktAPIClientID, config.TraktAPIClientSecret), "oauth device token", &token)
	switch req.ResponseStatusCode {
	case 200:
		if token == nil {
			return nil, errors.New("empty token")
		}
		return toTracker(token), nil
	case 404:
		return nil, errors.New("Invalid device code")
	case 409:
		return nil, errors.New("Code already used")
	case 410:
		return nil, errors.New("Code expired")
	case 418:
		return nil, errors.New("Code denied")
	case 429:
		return nil, tracker.ErrSlowDown
	}
	return nil, tracker.ErrAuthPending // 400 and transient errors: keep polling
}

func (f deviceFlow) RefreshToken(refreshToken string) (*tracker.Token, error) {
	var token *Token
	req, err := f.oauth("/oauth/token", fmt.Sprintf(`{"refresh_token": %q, "client_id": %q, "client_secret": %q, "redirect_uri": "urn:ietf:wg:oauth:2.0:oob", "grant_type": "refresh_token"}`, refreshToken, config.TraktAPIClientID, config.TraktAPIClientSecret), "oauth token", &token)
	if req.ResponseStatusCode == 400 || req.ResponseStatusCode == 401 {
		return nil, errors.New("Trakt refresh_token is invalid, please, re-authorize Trakt")
	} else if err != nil {
		return nil, err
	} else if token == nil {
		return nil, errors.New("Token not refreshed for Trakt authorization, please, re-authorize Trakt")
	}
	return toTracker(token), nil
}

func (f deviceFlow) Revoke(accessToken string) error {
	_, err := f.oauth("/oauth/revoke", fmt.Sprintf(`{"token": %q, "client_id": %q, "client_secret": %q}`, accessToken, config.TraktAPIClientID, config.TraktAPIClientSecret), "oauth revoke", nil)
	return err
}

func (deviceFlow) Username(accessToken string) (string, error) {
	user := &UserSettings{}
	header := GetHeader()
	header.Set("Authorization", "Bearer "+accessToken)
	req := reqapi.Request{
		API:         reqapi.TraktAPI,
		URL:         "users/settings",
		Header:      header,
		Params:      napping.Params{}.AsUrlValues(),
		Result:      &user,
		Description: "user settings",
	}
	if err := req.Do(); err != nil {
		return "", err
	}
	if user == nil || strings.TrimSpace(user.User.Ids.Slug) == "" {
		return "", errors.New("empty user settings")
	}
	return user.User.Ids.Slug, nil
}

// Authorized ...
func Authorized() error {
	if config.Get().TraktToken == "" {
		return NewProvider().Authorize()
	}
	return nil
}

// PaginatedRequest is a general proxy for collecting all pages requests.
// ret must be a pointer to a slice (e.g., &[]*WatchedMovie). Results from
// each page are appended into the slice that ret points to.
func PaginatedRequest[T any](endPoint string, params napping.Params, isWithAuth bool, isUpdateNeeded bool, isCachePages bool, cacheExpiration time.Duration) ([]T, error) {
	pageCurrent := 1
	pageTotal := 1
	pageLimit := 250
	if params["limit"] != "" {
		pageLimit, _ = strconv.Atoi(params["limit"])
	}

	var ret []T

	cacheStore := cache.NewDBStore()

	// Form a cache key for this type of request
	paramsEncoded, _ := url.QueryUnescape(params.AsUrlValues().Encode())
	cacheKey := fmt.Sprintf(cache.TraktPaginatedRequestKey, endPoint, paramsEncoded)

	// If we don't need to cache each page separately - try to fetch full list of items instead.
	if !isCachePages {
		if !isUpdateNeeded {
			if err := cacheStore.Get(cacheKey, &ret); err == nil {
				return ret, nil
			}
		}
	}

	for pageCurrent <= pageTotal {
		params["page"] = strconv.Itoa(pageCurrent)
		params["limit"] = strconv.Itoa(pageLimit)

		// Create a new pointer to an empty slice of the same element type
		var pageResult []T
		req, err := prepareRequest(endPoint, params, isWithAuth, isUpdateNeeded, isCachePages, cacheExpiration, &pageResult)
		if err != nil {
			return nil, err
		}

		if err := req.Do(); err != nil {
			return nil, err
		}

		if pageCurrent == 1 {
			pagination := getPagination(req.ResponseHeader)
			if pagination.PageCount > 0 {
				pageTotal = pagination.PageCount
			}
			if pagination.Limit > 0 {
				pageLimit = pagination.Limit
			}
		}

		ret = append(ret, pageResult...)

		pageCurrent++
	}

	// Cache collected list of items if we don't need to cache each page separately. If we do - each page is cached in the req.Do() call.
	if !isCachePages {
		defer cacheStore.Set(cacheKey, &ret, cache.TraktPaginatedRequestExpire)
	}

	return ret, nil
}

// Request is a general proxy for making requests
func Request(endPoint string, params napping.Params, isWithAuth bool, isUpdateNeeded bool, cacheExpiration time.Duration, ret any) error {
	req, err := prepareRequest(endPoint, params, isWithAuth, isUpdateNeeded, true, cacheExpiration, ret)
	if err != nil {
		return err
	}

	if err := req.Do(); err != nil {
		return err
	}

	return nil
}

// prepareRequest is a helper function to prepare a reqapi.Request with the given parameters.
func prepareRequest(endPoint string, params napping.Params, isWithAuth bool, isUpdateNeeded bool, isCacheEnabled bool, cacheExpiration time.Duration, ret any) (reqapi.Request, error) {
	if isWithAuth {
		if err := Authorized(); err != nil {
			return reqapi.Request{}, err
		}
	}

	header := GetHeader()
	if isWithAuth {
		header = GetAuthenticatedHeader()
	}

	req := reqapi.Request{
		API:    reqapi.TraktAPI,
		URL:    endPoint,
		Header: header,
		Params: params.AsUrlValues(),
		Result: ret,

		Cache:            isCacheEnabled,
		CacheExpire:      cacheExpiration,
		CacheForceExpire: isUpdateNeeded,
	}

	return req, nil
}

// SyncAddedItem adds item (movie/show) to watchlist or collection
func SyncAddedItem(itemType string, tmdbID string, location int) (req *reqapi.Request, err error) {
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
func SyncRemovedItem(itemType string, tmdbID string, location int) (req *reqapi.Request, err error) {
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
func AddToWatchlist(itemType string, tmdbID string) (req *reqapi.Request, err error) {
	if err := Authorized(); err != nil {
		return nil, err
	}

	req = &reqapi.Request{
		API:         reqapi.TraktAPI,
		Method:      "POST",
		URL:         "sync/watchlist",
		Header:      GetAuthenticatedHeader(),
		Params:      napping.Params{}.AsUrlValues(),
		Payload:     bytes.NewBufferString(fmt.Sprintf(`{"%s": [{"ids": {"tmdb": %s}}]}`, itemType, tmdbID)),
		Description: "add to watchlist",
	}

	return req, req.Do()
}

// AddToUserlist ...
func AddToUserlist(listID int, itemType string, tmdbID string) (req *reqapi.Request, err error) {
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

	payloadJSON, _ := json.Marshal(payload)
	req = &reqapi.Request{
		API:         reqapi.TraktAPI,
		Method:      "POST",
		URL:         endPoint,
		Header:      GetAuthenticatedHeader(),
		Params:      napping.Params{}.AsUrlValues(),
		Payload:     bytes.NewBuffer(payloadJSON),
		Description: "add to userlist",
	}

	return req, req.Do()
}

// RemoveFromUserlist ...
func RemoveFromUserlist(listID int, itemType string, tmdbID string) (req *reqapi.Request, err error) {
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

	payloadJSON, _ := json.Marshal(payload)
	req = &reqapi.Request{
		API:         reqapi.TraktAPI,
		Method:      "POST",
		URL:         endPoint,
		Header:      GetAuthenticatedHeader(),
		Params:      napping.Params{}.AsUrlValues(),
		Payload:     bytes.NewBuffer(payloadJSON),
		Description: "remove from userlist",
	}

	return req, req.Do()
}

// RemoveFromWatchlist ...
func RemoveFromWatchlist(itemType string, tmdbID string) (req *reqapi.Request, err error) {
	if err := Authorized(); err != nil {
		return nil, err
	}

	req = &reqapi.Request{
		API:         reqapi.TraktAPI,
		Method:      "POST",
		URL:         "sync/watchlist/remove",
		Header:      GetAuthenticatedHeader(),
		Params:      napping.Params{}.AsUrlValues(),
		Payload:     bytes.NewBufferString(fmt.Sprintf(`{"%s": [{"ids": {"tmdb": %s}}]}`, itemType, tmdbID)),
		Description: "remove from watchlist",
	}

	return req, req.Do()
}

// AddToCollection ...
func AddToCollection(itemType string, tmdbID string) (req *reqapi.Request, err error) {
	if err := Authorized(); err != nil {
		return nil, err
	}

	req = &reqapi.Request{
		API:         reqapi.TraktAPI,
		Method:      "POST",
		URL:         "sync/collection",
		Header:      GetAuthenticatedHeader(),
		Params:      napping.Params{}.AsUrlValues(),
		Payload:     bytes.NewBufferString(fmt.Sprintf(`{"%s": [{"ids": {"tmdb": %s}}]}`, itemType, tmdbID)),
		Description: "add to collection",
	}

	return req, req.Do()
}

// RemoveFromCollection ...
func RemoveFromCollection(itemType string, tmdbID string) (req *reqapi.Request, err error) {
	if err := Authorized(); err != nil {
		return nil, err
	}

	req = &reqapi.Request{
		API:         reqapi.TraktAPI,
		Method:      "POST",
		URL:         "sync/collection/remove",
		Header:      GetAuthenticatedHeader(),
		Params:      napping.Params{}.AsUrlValues(),
		Payload:     bytes.NewBufferString(fmt.Sprintf(`{"%s": [{"ids": {"tmdb": %s}}]}`, itemType, tmdbID)),
		Description: "remove from collection",
	}

	return req, req.Do()
}

// SetWatched adds and removes from watched history
func SetWatched(item *WatchedItem) (req *reqapi.Request, err error) {
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

	req = &reqapi.Request{
		API:         reqapi.TraktAPI,
		Method:      "POST",
		URL:         endPoint,
		Header:      GetAuthenticatedHeader(),
		Params:      napping.Params{}.AsUrlValues(),
		Payload:     bytes.NewBufferString(pre + query + post),
		Description: "set watched",
	}

	return req, req.Do()
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

	stats := HistoryResponse{}
	req := &reqapi.Request{
		API:         reqapi.TraktAPI,
		Method:      "POST",
		URL:         endPoint,
		Header:      GetAuthenticatedHeader(),
		Params:      napping.Params{}.AsUrlValues(),
		Payload:     bytes.NewBufferString(pre + query + post),
		Result:      &stats,
		Description: "set multiple watched",
	}

	err := req.Do()
	if err != nil {
		log.Warningf("Error getting watched items: %s", err)
		return nil, err
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
// // SetMultipleWatched adds and removes list from watched history
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
		contentType, tmdbID, progress, ident.GetVersion())

	req := &reqapi.Request{
		API:         reqapi.TraktAPI,
		Method:      "POST",
		URL:         endPoint,
		Header:      GetAuthenticatedHeader(),
		Params:      napping.Params{}.AsUrlValues(),
		Payload:     bytes.NewBufferString(payload),
		Description: endPoint,

		// Ignoring 201 and 409 as they inform that it was already scrobbled
		ResponseIgnore: []int{201, 409},
	}

	if err := req.Do(); err != nil {
		log.Errorf("Scrobble failed: %s", err)
		// TODO: Check what is the real error source (if there is an error)
		// if xbmcHost, _ := xbmc.GetLocalXBMCHost(); xbmcHost != nil {
		// 	xbmcHost.Notify("Elementum", "Scrobble failed, check your logs.", config.AddonIcon())
		// }
	} else if !slices.Contains([]int{200, 201, 409}, req.ResponseStatusCode) {
		log.Errorf("Failed to scrobble %s #%d to %s at %f: %d", contentType, tmdbID, action, progress, req.ResponseStatusCode)
	}
}

// GetLastActivities ...
func GetLastActivities() (ret *UserActivities, err error) {
	if err := Authorized(); err != nil {
		return nil, fmt.Errorf("Not authorized")
	}

	req := &reqapi.Request{
		API:         reqapi.TraktAPI,
		URL:         "sync/last_activities",
		Header:      GetAuthenticatedHeader(),
		Params:      napping.Params{}.AsUrlValues(),
		Result:      &ret,
		Description: "Last Activities",
	}

	if err = req.Do(); err != nil && req.ResponseStatusCode != 423 {
		return nil, err
	} else if req.ResponseStatusCode == 423 {
		return nil, ErrLocked
	}

	config.Get().TraktAuthorized = true

	return
}

// DiffWatchedShows ...
func DiffWatchedShows(current, previous []*WatchedShow) (diff []*WatchedShow) {
	if current == nil || len(previous) == 0 || len(current) == 0 {
		return
	}

	foundShow := false
	foundSeason := false
	foundEpisode := false

	var show *WatchedShow
	var season *WatchedSeason

	for _, previousShow := range previous {
		if previousShow == nil || previousShow.Show == nil || previousShow.Show.IDs == nil {
			continue
		}

		foundShow = false
		foundSeason = false
		foundEpisode = false

		show = nil

		for _, currentShow := range current {
			if currentShow == nil || currentShow.Show == nil || currentShow.Show.IDs == nil {
				continue
			}

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
func DiffWatchedMovies(previous, current []*WatchedMovie, checkDate bool) []*WatchedMovie {
	ret := make([]*WatchedMovie, 0, len(current))
	found := false
	for _, ce := range current {
		if ce == nil || ce.Movie == nil || ce.Movie.IDs == nil {
			continue
		}

		found = false
		for _, pr := range previous {
			if pr == nil || pr.Movie == nil || pr.Movie.IDs == nil {
				continue
			}

			if pr.Movie.IDs.Trakt == ce.Movie.IDs.Trakt && (!checkDate || !ce.LastWatchedAt.After(pr.LastWatchedAt)) {
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
		if ce == nil || ce.Movie == nil || ce.Movie.IDs == nil {
			continue
		}

		found = false
		for _, pr := range previous {
			if pr == nil || pr.Movie == nil || pr.Movie.IDs == nil {
				continue
			}

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

// previousCached reads a cached slice of T stored under key.
func previousCached[T any](key string) ([]T, error) {
	var items []T
	err := cache.NewDBStore().Get(key, &items)
	return items, err
}

// NotifyLocked ...
func NotifyLocked() {
	cacheStore := cache.NewDBStore()
	checked := false
	if err := cacheStore.Get(cache.TraktLockedAccountKey, &checked); err == nil {
		return
	}

	cacheStore.Set(cache.TraktLockedAccountKey, checked, cache.TraktLockedAccountExpire)

	if xbmcHost, _ := xbmc.GetLocalXBMCHost(); xbmcHost != nil {
		xbmcHost.Dialog("LOCALIZE[30616]", "LOCALIZE[30617]")
	}
}

func (id *IDs) ToUniqueIDs() *uid.UniqueIDs {
	return &uid.UniqueIDs{
		Trakt: id.Trakt,
		IMDB:  id.IMDB,
		TMDB:  id.TMDB,
		TVDB:  id.TVDB,
	}
}

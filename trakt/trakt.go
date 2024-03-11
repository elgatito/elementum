package trakt

import (
	"bytes"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/elgatito/elementum/broadcast"
	"github.com/elgatito/elementum/cache"
	"github.com/elgatito/elementum/config"
	"github.com/elgatito/elementum/library/uid"
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
	// APIVersion ...
	APIVersion = "2"

	ListsPerPage = 150
)

var log = logging.MustGetLogger("trakt")

var (
	// Cookies ...
	Cookies = ""
	// UserAgent ...
	UserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/122.0.0.0 Safari/537.36"
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
		"trakt-api-key":     []string{config.TraktReadClientID},
		"trakt-api-version": []string{APIVersion},
		"User-Agent":        []string{UserAgent},
		"Cookie":            []string{Cookies},
	}
}

func GetAuthenticatedHeader() http.Header {
	return http.Header{
		"Content-type":      []string{"application/json"},
		"Authorization":     []string{fmt.Sprintf("Bearer %s", config.Get().TraktToken)},
		"trakt-api-key":     []string{config.TraktWriteClientID},
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

// GetCode ...
func GetCode() (code *Code, err error) {
	req := reqapi.Request{
		API:    reqapi.TraktAPI,
		Method: "POST",
		URL:    "oauth/device/code",
		Header: http.Header{
			"Content-type": []string{"application/json"},
			"User-Agent":   []string{UserAgent},
			"Cookie":       []string{Cookies},
		},
		Params: napping.Params{
			"client_id": config.TraktWriteClientID,
		}.AsUrlValues(),
		Result:      &code,
		Description: "oauth device code",
	}

	err = req.Do()
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
			req := reqapi.Request{
				API:    reqapi.TraktAPI,
				Method: "POST",
				URL:    "oauth/device/token",
				Header: http.Header{
					"Content-type": []string{"application/json"},
					"User-Agent":   []string{UserAgent},
					"Cookie":       []string{Cookies},
				},
				Params: napping.Params{
					"code":          code.DeviceCode,
					"client_id":     config.TraktWriteClientID,
					"client_secret": config.TraktWriteClientSecret,
				}.AsUrlValues(),
				Result:      &token,
				Description: "oauth device token",
			}

			req.Do()
			// if errGet := req.Do(); errGet != nil {
			// 	return nil, errGet
			// }

			if req.ResponseStatusCode == 200 {
				return token, err
			} else if req.ResponseStatusCode == 400 {
				break
			} else if req.ResponseStatusCode == 404 {
				err = errors.New("Invalid device code")
				return nil, err
			} else if req.ResponseStatusCode == 409 {
				err = errors.New("Code already used")
				return nil, err
			} else if req.ResponseStatusCode == 410 {
				err = errors.New("Code expired")
				return nil, err
			} else if req.ResponseStatusCode == 418 {
				err = errors.New("Code denied")
				return nil, err
			} else if req.ResponseStatusCode == 429 {
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

// TokenRefreshHandler ...
func TokenRefreshHandler() {
	if config.Get().TraktToken == "" {
		return
	}

	var token *Token
	ticker := time.NewTicker(12 * time.Hour)
	closer := broadcast.Closer.C()
	defer ticker.Stop()

	for {
		select {
		case <-closer:
			return
		case <-ticker.C:
			if time.Now().Unix() > int64(config.Get().TraktTokenExpiry)-int64(259200) {
				req := reqapi.Request{
					API:    reqapi.TraktAPI,
					Method: "POST",
					URL:    "oauth/token",
					Header: http.Header{
						"Content-type": []string{"application/json"},
						"User-Agent":   []string{UserAgent},
						"Cookie":       []string{Cookies},
					},
					Params: napping.Params{
						"refresh_token": config.Get().TraktRefreshToken,
						"client_id":     config.TraktWriteClientID,
						"client_secret": config.TraktWriteClientSecret,
						"redirect_uri":  "urn:ietf:wg:oauth:2.0:oob",
						"grant_type":    "refresh_token",
					}.AsUrlValues(),
					Result:      &token,
					Description: "oauth token",
				}

				err := req.Do()
				if err != nil || token == nil {
					if xbmcHost, _ := xbmc.GetLocalXBMCHost(); xbmcHost != nil {
						xbmcHost.Notify("Elementum", err.Error(), config.AddonIcon())
					}
					log.Error(err)
					return
				}

				if req.ResponseStatusCode == 200 {
					expiry := time.Now().Unix() + int64(token.ExpiresIn)
					if xbmcHost, _ := xbmc.GetLocalXBMCHost(); xbmcHost != nil {
						xbmcHost.SetSetting("trakt_token_expiry", strconv.Itoa(int(expiry)))
						xbmcHost.SetSetting("trakt_token", token.AccessToken)
						xbmcHost.SetSetting("trakt_refresh_token", token.RefreshToken)
					}
					log.Noticef("Token refreshed for Trakt authorization, next refresh in %s", time.Duration(token.ExpiresIn-259200)*time.Second)
				}
			}
		}
	}
}

// Authorize ...
func Authorize(fromSettings bool) error {
	code, err := GetCode()
	if err != nil || code == nil {
		log.Error("Could not get authorization code from Trakt.tv: %s", err)
		if xbmcHost, _ := xbmc.GetLocalXBMCHost(); xbmcHost != nil {
			xbmcHost.Notify("Elementum", err.Error(), config.AddonIcon())
		}
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
					if xbmcHost, _ := xbmc.GetLocalXBMCHost(); xbmcHost != nil {
						xbmcHost.Notify("Elementum", "LOCALIZE[30651]", config.AddonIcon())
					}
					return
				}

				token, err := PollToken(code)
				log.Debugf("Received token: %#v, error: %s", token, err)

				if err != nil {
					continue
				}

				// Cleanup last activities to force requesting again
				cacheStore := cache.NewDBStore()
				_ = cacheStore.Set(fmt.Sprintf(cache.TraktActivitiesKey, ""), "", 1)

				expiry := time.Now().Unix() + int64(token.ExpiresIn)
				if xbmcHost, _ := xbmc.GetLocalXBMCHost(); xbmcHost != nil {
					xbmcHost.SetSetting("trakt_token_expiry", strconv.Itoa(int(expiry)))
					xbmcHost.SetSetting("trakt_token", token.AccessToken)
					xbmcHost.SetSetting("trakt_refresh_token", token.RefreshToken)
				}

				config.Get().TraktToken = token.AccessToken

				// Getting username for currently authorized user
				user := &UserSettings{}
				req := reqapi.Request{
					API:         reqapi.TraktAPI,
					URL:         "users/settings",
					Header:      GetAuthenticatedHeader(),
					Params:      napping.Params{}.AsUrlValues(),
					Result:      &user,
					Description: "user settings",
				}

				if err = req.Do(); err != nil {
					return
				}
				if req.ResponseStatusCode == 200 && user != nil && user.User.Ids.Slug != "" {
					log.Debugf("Setting Trakt Username as %s", user.User.Ids.Slug)
					if xbmcHost, _ := xbmc.GetLocalXBMCHost(); xbmcHost != nil {
						xbmcHost.SetSetting("trakt_username", user.User.Ids.Slug)
					}
				}

				config.Reload()

				if xbmcHost, _ := xbmc.GetLocalXBMCHost(); xbmcHost != nil {
					xbmcHost.Notify("Elementum", "LOCALIZE[30650]", config.AddonIcon())
				}
				return
			}
		}
	}(code)

	if xbmcHost, _ := xbmc.GetLocalXBMCHost(); xbmcHost != nil {
		if code != nil {
			if !xbmcHost.Dialog(xbmcHost.GetLocalizedString(30646), fmt.Sprintf(xbmcHost.GetLocalizedString(30649), code.VerificationURL, code.UserCode)) {
				return errors.New("Authentication canceled")
			}
		} else {
			return errors.New("Authentication canceled")
		}
	}

	return nil
}

// Deauthorize ...
func Deauthorize(fromSettings bool) error {
	// Cleanup last activities to force requesting again
	cacheStore := cache.NewDBStore()
	_ = cacheStore.Set(fmt.Sprintf(cache.TraktActivitiesKey, ""), "", 1)

	if xbmcHost, _ := xbmc.GetLocalXBMCHost(); xbmcHost != nil {
		xbmcHost.SetSetting("trakt_token_expiry", "")
		xbmcHost.SetSetting("trakt_token", "")
		xbmcHost.SetSetting("trakt_refresh_token", "")
		xbmcHost.SetSetting("trakt_username", "")

		xbmcHost.Notify("Elementum", "LOCALIZE[30652]", config.AddonIcon())
	}

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

	header := GetHeader()
	if isWithAuth {
		header = GetAuthenticatedHeader()
	}

	req := reqapi.Request{
		API:    reqapi.TraktAPI,
		URL:    endPoint,
		Header: header,
		Params: params.AsUrlValues(),
		Result: &ret,

		Cache:            true,
		CacheExpire:      cacheExpiration,
		CacheForceExpire: isUpdateNeeded,
	}

	if err := req.Do(); err != nil {
		return err
	}

	return nil
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

// SetWatched addes and removes from watched history
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
		contentType, tmdbID, progress, ident.GetVersion())

	req := &reqapi.Request{
		API:         reqapi.TraktAPI,
		Method:      "POST",
		URL:         endPoint,
		Header:      GetAuthenticatedHeader(),
		Params:      napping.Params{}.AsUrlValues(),
		Payload:     bytes.NewBufferString(payload),
		Description: "scrobble",
	}

	if err := req.Do(); err != nil {
		log.Error(err.Error())
		if xbmcHost, _ := xbmc.GetLocalXBMCHost(); xbmcHost != nil {
			xbmcHost.Notify("Elementum", "Scrobble failed, check your logs.", config.AddonIcon())
		}
	} else if req.ResponseStatusCode != 201 {
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
	if current == nil || previous == nil || len(previous) == 0 || len(current) == 0 {
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

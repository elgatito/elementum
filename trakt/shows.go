package trakt

import (
	"fmt"
	"math/rand"
	"sort"
	"strconv"
	"time"

	"github.com/elgatito/elementum/cache"
	"github.com/elgatito/elementum/config"
	"github.com/elgatito/elementum/library/playcount"
	"github.com/elgatito/elementum/library/uid"
	"github.com/elgatito/elementum/tmdb"
	"github.com/elgatito/elementum/util"
	"github.com/elgatito/elementum/util/reqapi"
	"github.com/elgatito/elementum/xbmc"

	"github.com/anacrolix/missinggo/perf"
	"github.com/anacrolix/sync"
	"github.com/jmcvetta/napping"
)

// GetShow ...
func GetShow(ID string) (show *Show) {
	defer perf.ScopeTimer()()

	req := &reqapi.Request{
		API:    reqapi.TraktAPI,
		URL:    fmt.Sprintf("shows/%s", ID),
		Header: GetAvailableHeader(),
		Params: napping.Params{
			"extended": "full",
		}.AsUrlValues(),
		Result:      &show,
		Description: "trakt show",

		Cache:       true,
		CacheExpire: cache.CacheExpireLong,
	}

	if err := req.Do(); err != nil {
		log.Error(err)
		if xbmcHost, _ := xbmc.GetLocalXBMCHost(); xbmcHost != nil {
			xbmcHost.Notify("Elementum", fmt.Sprintf("Failed getting Trakt show (%s), check your logs.", ID), config.AddonIcon())
		}
		return
	}

	return
}

// GetShowByTMDB ...
func GetShowByTMDB(tmdbID string) (show *Show) {
	defer perf.ScopeTimer()()

	var results ShowSearchResults
	req := &reqapi.Request{
		API:         reqapi.TraktAPI,
		URL:         fmt.Sprintf("search/tmdb/%s?type=show", tmdbID),
		Header:      GetAvailableHeader(),
		Params:      napping.Params{}.AsUrlValues(),
		Result:      &results,
		Description: "trakt show by tmdb",

		Cache:       true,
		CacheExpire: cache.CacheExpireLong,
	}

	if err := req.Do(); err != nil {
		log.Error(err)
		if xbmcHost, _ := xbmc.GetLocalXBMCHost(); xbmcHost != nil {
			xbmcHost.Notify("Elementum", "Failed getting Trakt show using TMDB ID, check your logs.", config.AddonIcon())
		}
		return
	}

	if len(results) > 0 && results[0].Show != nil {
		show = results[0].Show
	}
	return
}

// GetShowByTVDB ...
func GetShowByTVDB(tvdbID string) (show *Show) {
	defer perf.ScopeTimer()()

	req := &reqapi.Request{
		API:         reqapi.TraktAPI,
		URL:         fmt.Sprintf("search/tvdb/%s?type=show", tvdbID),
		Header:      GetAvailableHeader(),
		Params:      napping.Params{}.AsUrlValues(),
		Result:      &show,
		Description: "trakt show by tvdb",

		Cache:       true,
		CacheExpire: cache.CacheExpireLong,
	}

	if err := req.Do(); err != nil {
		log.Error(err)
		if xbmcHost, _ := xbmc.GetLocalXBMCHost(); xbmcHost != nil {
			xbmcHost.Notify("Elementum", "Failed getting Trakt show using TVDB ID, check your logs.", config.AddonIcon())
		}
	}
	return
}

// GetSeasonEpisodes ...
func GetSeasonEpisodes(showID, seasonNumber int) (episodes []*Episode) {
	defer perf.ScopeTimer()()

	req := &reqapi.Request{
		API:         reqapi.TraktAPI,
		URL:         fmt.Sprintf("shows/%d/seasons/%d", showID, seasonNumber),
		Header:      GetAvailableHeader(),
		Params:      napping.Params{"extended": "full"}.AsUrlValues(),
		Result:      &episodes,
		Description: "show season episodes",

		Cache:       true,
		CacheExpire: cache.CacheExpireLong,
	}

	if err := req.Do(); err != nil {
		log.Error(err)
	}
	return
}

// GetEpisode ...
func GetEpisode(showID, seasonNumber, episodeNumber int) (episode *Episode) {
	defer perf.ScopeTimer()()

	req := &reqapi.Request{
		API:         reqapi.TraktAPI,
		URL:         fmt.Sprintf("shows/%d/seasons/%d/episodes/%d", showID, seasonNumber, episodeNumber),
		Header:      GetAvailableHeader(),
		Params:      napping.Params{"extended": "full"}.AsUrlValues(),
		Result:      &episode,
		Description: "trakt episode",

		Cache:       true,
		CacheExpire: cache.CacheExpireLong,
	}

	if err := req.Do(); err != nil {
		log.Error(err)
	}
	return
}

// GetEpisodeByID ...
func GetEpisodeByID(id string) (episode *Episode) {
	defer perf.ScopeTimer()()

	req := &reqapi.Request{
		API:         reqapi.TraktAPI,
		URL:         fmt.Sprintf("search/trakt/%s?type=episode", id),
		Header:      GetAvailableHeader(),
		Params:      napping.Params{}.AsUrlValues(),
		Result:      &episode,
		Description: "trakt episode by id",

		Cache:       true,
		CacheExpire: cache.CacheExpireLong,
	}

	if err := req.Do(); err != nil {
		log.Error(err)
		if xbmcHost, _ := xbmc.GetLocalXBMCHost(); xbmcHost != nil {
			xbmcHost.Notify("Elementum", "Failed getting Trakt episode, check your logs.", config.AddonIcon())
		}
	}
	return
}

// GetEpisodeByTMDB ...
func GetEpisodeByTMDB(tmdbID string) (episode *Episode) {
	defer perf.ScopeTimer()()

	var results EpisodeSearchResults
	req := &reqapi.Request{
		API:         reqapi.TraktAPI,
		URL:         fmt.Sprintf("search/tmdb/%s?type=episode", tmdbID),
		Header:      GetAvailableHeader(),
		Params:      napping.Params{}.AsUrlValues(),
		Result:      &results,
		Description: "trakt episode by tmdb",

		Cache:       true,
		CacheExpire: cache.CacheExpireLong,
	}

	if err := req.Do(); err != nil {
		log.Error(err)
		if xbmcHost, _ := xbmc.GetLocalXBMCHost(); xbmcHost != nil {
			xbmcHost.Notify("Elementum", "Failed getting Trakt episode using TMDB ID, check your logs.", config.AddonIcon())
		}
		return
	}

	if len(results) > 0 && results[0].Episode != nil {
		episode = results[0].Episode
	}
	return
}

// GetEpisodeByTVDB ...
func GetEpisodeByTVDB(tvdbID string) (episode *Episode) {
	defer perf.ScopeTimer()()

	req := &reqapi.Request{
		API:         reqapi.TraktAPI,
		URL:         fmt.Sprintf("search/tvdb/%s?type=episode", tvdbID),
		Header:      GetAvailableHeader(),
		Params:      napping.Params{}.AsUrlValues(),
		Result:      &episode,
		Description: "trakt episode by tvdb",

		Cache:       true,
		CacheExpire: cache.CacheExpireLong,
	}

	if err := req.Do(); err != nil {
		log.Error(err)
		if xbmcHost, _ := xbmc.GetLocalXBMCHost(); xbmcHost != nil {
			xbmcHost.Notify("Elementum", "Failed getting Trakt episode using TVDB ID, check your logs.", config.AddonIcon())
		}
	}
	return
}

// SearchShows ...
// TODO: Actually use this somewhere
func SearchShows(query string, page string) (shows []*Shows, err error) {
	defer perf.ScopeTimer()()

	req := &reqapi.Request{
		API:    reqapi.TraktAPI,
		URL:    "search",
		Header: GetAvailableHeader(),
		Params: napping.Params{
			"page":     page,
			"limit":    strconv.Itoa(config.Get().ResultsPerPage),
			"query":    query,
			"extended": "full",
		}.AsUrlValues(),
		Result:      &shows,
		Description: "search show",
	}

	if err = req.Do(); err != nil {
		return
	}

	return
}

// TopShows ...
func TopShows(topCategory string, page string) (shows []*Shows, total int, err error) {
	defer perf.ScopeTimer()()

	endPoint := "shows/" + topCategory
	if topCategory == "recommendations" {
		endPoint = topCategory + "/shows"
	}

	resultsPerPage := config.Get().ResultsPerPage
	limit := resultsPerPage
	pageInt, err := strconv.Atoi(page)
	if err != nil {
		return shows, 0, err
	}
	if pageInt < -1 {
		resultsPerPage = pageInt * -1
		limit = pageInt * -1
		page = "1"
		pageInt = 1
	}
	page = strconv.Itoa((pageInt-1)*resultsPerPage/limit + 1)

	var showList []*Show
	req := &reqapi.Request{
		API:    reqapi.TraktAPI,
		URL:    endPoint,
		Header: GetAvailableHeader(),
		Params: napping.Params{
			"page":     page,
			"limit":    strconv.Itoa(limit),
			"extended": "full",
		}.AsUrlValues(),
		Result:      &shows,
		Description: "list shows",

		Cache:       true,
		CacheExpire: cache.CacheExpireMedium,
	}

	if topCategory == "popular" || topCategory == "recommendations" {
		req.Result = &showList
	}

	if err = req.Do(); err != nil {
		return shows, 0, err
	}

	if topCategory == "popular" || topCategory == "recommendations" {
		showListing := make([]*Shows, 0)
		for _, show := range showList {
			showItem := Shows{
				Show: show,
			}
			showListing = append(showListing, &showItem)
		}
		shows = showListing
	}

	pagination := getPagination(req.ResponseHeader)
	total = pagination.ItemCount

	return
}

// WatchlistShows ...
func WatchlistShows(isUpdateNeeded bool) (shows []*Shows, err error) {
	if err := Authorized(); err != nil {
		return shows, err
	}

	defer perf.ScopeTimer()()

	cacheStore := cache.NewDBStore()

	if !isUpdateNeeded {
		if err := cacheStore.Get(cache.TraktShowsWatchlistKey, &shows); err == nil {
			return shows, nil
		}
	}

	var watchlist []*WatchlistShow
	req := &reqapi.Request{
		API:    reqapi.TraktAPI,
		URL:    "sync/watchlist/shows",
		Header: GetAvailableHeader(),
		Params: napping.Params{
			"extended": "full",
		}.AsUrlValues(),
		Result:      &watchlist,
		Description: "watchlist shows",
	}

	if err = req.Do(); err != nil {
		return shows, err
	}

	showListing := make([]*Shows, 0)
	for _, show := range watchlist {
		showItem := Shows{
			Show: show.Show,
		}
		showListing = append(showListing, &showItem)
	}
	shows = showListing

	defer cacheStore.Set(cache.TraktShowsWatchlistKey, &shows, cache.TraktShowsWatchlistExpire)
	return
}

// PreviousWatchlistShows ...
func PreviousWatchlistShows() (shows []*Shows, err error) {
	err = cache.
		NewDBStore().
		Get(cache.TraktShowsWatchlistKey, &shows)

	return shows, err
}

// CollectionShows ...
func CollectionShows(isUpdateNeeded bool) (shows []*Shows, err error) {
	if err := Authorized(); err != nil {
		return shows, err
	}

	defer perf.ScopeTimer()()

	cacheStore := cache.NewDBStore()

	if !isUpdateNeeded {
		if err := cacheStore.Get(cache.TraktShowsCollectionKey, &shows); err == nil {
			return shows, nil
		}
	}

	var collection []*CollectionShow
	req := &reqapi.Request{
		API:    reqapi.TraktAPI,
		URL:    "sync/collection/shows",
		Header: GetAvailableHeader(),
		Params: napping.Params{
			"extended": "full",
		}.AsUrlValues(),
		Result:      &collection,
		Description: "collection shows",
	}

	if err = req.Do(); err != nil {
		return shows, err
	}

	sort.Slice(collection, func(i, j int) bool {
		return collection[i].CollectedAt.After(collection[j].CollectedAt)
	})

	showListing := make([]*Shows, 0, len(collection))
	for _, show := range collection {
		showItem := Shows{
			Show: show.Show,
		}
		showListing = append(showListing, &showItem)
	}

	defer cacheStore.Set(cache.TraktShowsCollectionKey, &showListing, cache.TraktShowsCollectionExpire)
	return showListing, err
}

// PreviousCollectionShows ...
func PreviousCollectionShows() (shows []*Shows, err error) {
	err = cache.
		NewDBStore().
		Get(cache.TraktShowsCollectionKey, &shows)

	return shows, err
}

// ListItemsShows ...
func ListItemsShows(user, listID string) (shows []*Shows, err error) {
	defer perf.ScopeTimer()()

	// Check if this list needs a refresh from cache
	listActivities, err := GetListActivities(user, listID)
	isUpdateNeeded := err != nil || listActivities.IsUpdated()

	url := fmt.Sprintf("users/%s/lists/%s/items/shows", user, listID)
	if user == "Trakt" { // if this is "Official" public list - we use special endpoint
		url = fmt.Sprintf("/lists/%s/items/shows", listID)
	}

	cacheStore := cache.NewDBStore()
	key := fmt.Sprintf(cache.TraktShowsListKey, listID)

	if !isUpdateNeeded {
		if err := cacheStore.Get(key, &shows); err == nil {
			return shows, nil
		}
	}

	var list []*ListItem
	req := &reqapi.Request{
		API:    reqapi.TraktAPI,
		URL:    url,
		Header: GetAvailableHeader(),
		Params: napping.Params{
			"extended": "full",
		}.AsUrlValues(),
		Result:      &list,
		Description: "list item shows",
	}

	if err = req.Do(); err != nil {
		return shows, err
	}

	showListing := make([]*Shows, 0)
	for _, show := range list {
		if show.Show == nil {
			continue
		}
		showItem := Shows{
			Show: show.Show,
		}
		showListing = append(showListing, &showItem)
	}
	shows = showListing

	defer cacheStore.Set(key, &shows, cache.TraktShowsListExpire)
	return shows, err
}

// PreviousListItemsShows ...
func PreviousListItemsShows(listID string) (shows []*Shows, err error) {
	cacheStore := cache.NewDBStore()
	key := fmt.Sprintf(cache.TraktShowsListKey, listID)
	err = cacheStore.Get(key, &shows)

	return
}

// CalendarShows ...
func CalendarShows(endPoint string, page string, cacheExpire time.Duration, isUpdateNeeded bool) (shows []*CalendarShow, total int, err error) {
	defer perf.ScopeTimer()()

	resultsPerPage := config.Get().ResultsPerPage
	limit := resultsPerPage
	pageInt, err := strconv.Atoi(page)
	if err != nil {
		return shows, 0, err
	}
	page = strconv.Itoa((pageInt-1)*resultsPerPage/limit + 1)

	req := &reqapi.Request{
		API:    reqapi.TraktAPI,
		URL:    "calendars/" + endPoint,
		Header: GetAvailableHeader(),
		Params: napping.Params{
			"page":     page,
			"limit":    strconv.Itoa(limit),
			"extended": "full",
		}.AsUrlValues(),
		Result:      &shows,
		Description: "calendar shows",

		Cache:            true,
		CacheExpire:      cacheExpire,
		CacheForceExpire: isUpdateNeeded,
	}

	if err := req.Do(); err != nil {
		return shows, 0, err
	}

	pagination := getPagination(req.ResponseHeader)
	total = pagination.ItemCount
	if err != nil {
		total = -1
	}

	return
}

// WatchedShows ...
func WatchedShows(isUpdateNeeded bool) (WatchedShowsType, error) {
	defer perf.ScopeTimer()()

	var shows []*WatchedShow
	err := Request(
		"sync/watched/shows",
		napping.Params{"extended": "full"},
		true,
		isUpdateNeeded,
		cache.TraktShowsWatchedKey,
		cache.TraktShowsWatchedExpire,
		&shows,
	)

	if len(shows) != 0 {
		defer cache.
			NewDBStore().
			Set(cache.TraktShowsWatchedKey, &shows, cache.TraktShowsWatchedExpire)
	}

	return shows, err
}

// PreviousWatchedShows ...
func PreviousWatchedShows() (shows []*WatchedShow, err error) {
	err = cache.
		NewDBStore().
		Get(cache.TraktShowsWatchedKey, &shows)

	return
}

// PausedShows ...
func PausedShows(isUpdateNeeded bool) ([]*PausedEpisode, error) {
	defer perf.ScopeTimer()()

	var shows []*PausedEpisode
	err := Request(
		"sync/playback/episodes",
		napping.Params{
			"extended": "full",
		},
		true,
		isUpdateNeeded,
		cache.TraktShowsPausedKey,
		cache.TraktShowsPausedExpire,
		&shows,
	)

	return shows, err
}

// WatchedShowsProgress ...
func WatchedShowsProgress() (shows []*ProgressShow, err error) {
	if errAuth := Authorized(); errAuth != nil {
		return nil, errAuth
	}

	defer perf.ScopeTimer()()

	activities, err := GetActivities("WatchedShowsProgress")

	cacheStore := cache.NewDBStore()

	if err != nil {
		log.Warningf("Cannot get activities: %s", err)
		return nil, err
	}

	// If last watched time was changed - we should get fresh Watched shows list
	watchedShows, errWatched := WatchedShows(activities.EpisodesWatched())
	if errWatched != nil {
		log.Errorf("Error getting the watched shows: %v", errWatched)
		return nil, errWatched
	}

	params := napping.Params{
		"hidden":         "false",
		"specials":       "false",
		"count_specials": "false",
		"extended":       "full",
	}.AsUrlValues()

	showsList := make([]*ProgressShow, len(watchedShows))
	watchedProgressShows := make([]*WatchedProgressShow, len(watchedShows))

	var wg sync.WaitGroup
	wg.Add(len(watchedShows))
	for i, show := range watchedShows {
		go func(idx int, show *WatchedShow) {
			var watchedProgressShow *WatchedProgressShow
			var cachedWatchedAt time.Time

			defer func() {
				cacheStore.Set(fmt.Sprintf(cache.TraktWatchedShowsProgressWatchedKey, show.Show.IDs.Trakt), show.LastWatchedAt, cache.TraktWatchedShowsProgressWatchedExpire)

				watchedProgressShows[idx] = watchedProgressShow

				if watchedProgressShow != nil && watchedProgressShow.NextEpisode != nil && watchedProgressShow.NextEpisode.Number != 0 && watchedProgressShow.NextEpisode.Season != 0 {
					showsList[idx] = &ProgressShow{
						Show:    show.Show,
						Episode: watchedProgressShow.NextEpisode,
					}
				}
				wg.Done()
			}()

			if err := cacheStore.Get(fmt.Sprintf(cache.TraktWatchedShowsProgressWatchedKey, show.Show.IDs.Trakt), &cachedWatchedAt); err == nil && show.LastWatchedAt.Equal(cachedWatchedAt) {
				if err := cacheStore.Get(fmt.Sprintf(cache.TraktWatchedShowsProgressKey, show.Show.IDs.Trakt), &watchedProgressShow); err == nil {
					return
				}
			}

			endPoint := fmt.Sprintf("shows/%d/progress/watched", show.Show.IDs.Trakt)
			req := &reqapi.Request{
				API:         reqapi.TraktAPI,
				URL:         endPoint,
				Header:      GetAvailableHeader(),
				Params:      params,
				Result:      &watchedProgressShow,
				Description: "watched progress shows",
			}

			if err = req.Do(); err != nil {
				log.Errorf("Error getting endpoint %s for show '%d': %#v", endPoint, show.Show.IDs.Trakt, err)
				return
			}

			defer cacheStore.Set(fmt.Sprintf(cache.TraktWatchedShowsProgressKey, show.Show.IDs.Trakt), &watchedProgressShow, cache.TraktWatchedShowsProgressExpire)
		}(i, show)
	}
	wg.Wait()

	hiddenShowsMap := GetHiddenShowsMap("progress_watched")
	for _, s := range showsList {
		if s != nil {
			if !hiddenShowsMap[s.Show.IDs.Trakt] {
				shows = append(shows, s)
			} else {
				log.Debugf("Will suppress hidden show: %s", s.Show.Title)
			}
		}
	}

	return
}

// GetHiddenShowsMap returns a map with hidden shows that can be used for filtering
func GetHiddenShowsMap(section string) map[int]bool {
	hiddenShowsMap := make(map[int]bool)
	if config.Get().TraktToken == "" || !config.Get().TraktSyncHidden {
		return hiddenShowsMap
	}

	hiddenShowsProgress, _ := ListHiddenShows(section, false)
	for _, show := range hiddenShowsProgress {
		if show == nil || show.Show == nil || show.Show.IDs == nil {
			continue
		}

		hiddenShowsMap[show.Show.IDs.Trakt] = true
	}

	return hiddenShowsMap
}

// FilterHiddenProgressShows returns a slice of ProgressShow without hidden shows
func FilterHiddenProgressShows(inShows []*ProgressShow) (outShows []*ProgressShow) {
	if config.Get().TraktToken == "" || !config.Get().TraktSyncHidden {
		return inShows
	}

	hiddenShowsMap := GetHiddenShowsMap("progress_watched")

	for _, s := range inShows {
		if s == nil || s.Show == nil || s.Show.IDs == nil {
			continue
		}
		if !hiddenShowsMap[s.Show.IDs.Trakt] {
			// append to new instead of delete in old b/c delete is O(n)
			outShows = append(outShows, s)
		} else {
			log.Debugf("Will suppress hidden show: %s", s.Show.Title)
		}
	}

	return outShows
}

// ListHiddenShows updates list of hidden shows for a given section
func ListHiddenShows(section string, isUpdateNeeded bool) (shows []*Shows, err error) {
	if err := Authorized(); err != nil {
		return shows, err
	}

	defer perf.ScopeTimer()()

	params := napping.Params{
		"type":  "show",
		"limit": "100",
	}.AsUrlValues()

	cacheStore := cache.NewDBStore()
	var cacheKey string
	var cacheExpiration time.Duration
	switch section {
	case "progress_watched":
		cacheKey = cache.TraktShowsHiddenProgressKey
		cacheExpiration = cache.TraktShowsHiddenProgressExpire
	default:
		return shows, fmt.Errorf("Unsupported section for hidden shows: %s", section)
	}

	if !isUpdateNeeded {
		if err := cacheStore.Get(cacheKey, &shows); err == nil {
			return shows, nil
		}
	}

	totalPages := 1
	for page := 1; page < totalPages+1; page++ {
		params.Add("page", strconv.Itoa(page))

		var hiddenShows []*HiddenShow
		req := &reqapi.Request{
			API:         reqapi.TraktAPI,
			URL:         "users/hidden/" + section,
			Header:      GetAvailableHeader(),
			Params:      params,
			Result:      &hiddenShows,
			Description: "hidden shows",
		}

		if err = req.Do(); err != nil {
			return shows, err
		}

		for _, show := range hiddenShows {
			showItem := Shows{
				Show: show.Show,
			}
			shows = append(shows, &showItem)
		}

		pagination := getPagination(req.ResponseHeader)
		totalPages = pagination.PageCount
	}

	defer cacheStore.Set(cacheKey, &shows, cacheExpiration)
	return
}

// ToListItem ...
func (show *Show) ToListItem() (item *xbmc.ListItem) {
	defer perf.ScopeTimer()()

	var tmdbShow *tmdb.Show
	if show.IDs.TMDB != 0 {
		if tmdbShow = tmdb.GetShow(show.IDs.TMDB, config.Get().Language); tmdbShow != nil {
			if !config.Get().ForceUseTrakt {
				item = tmdbShow.ToListItem()
			}
		}
	}
	if item == nil {
		firstAired, _ := time.Parse(time.RFC3339, show.FirstAired)
		item = &xbmc.ListItem{
			Label: show.Title,
			Info: &xbmc.ListItemInfo{
				Count:         rand.Int(),
				Title:         show.Title,
				OriginalTitle: show.Title,
				Year:          show.Year,
				Aired:         firstAired.Format(time.DateOnly),
				Genre:         show.Genres,
				Plot:          show.Overview,
				PlotOutline:   show.Overview,
				Rating:        show.Rating,
				Votes:         strconv.Itoa(show.Votes),
				Duration:      show.Runtime * 60 * show.AiredEpisodes,
				MPAA:          show.Certification,
				Code:          show.IDs.IMDB,
				IMDBNumber:    show.IDs.IMDB,
				Trailer:       util.TrailerURL(show.Trailer),
				PlayCount:     playcount.GetWatchedShowByTMDB(show.IDs.TMDB).Int(),
				DBTYPE:        "tvshow",
				Mediatype:     "tvshow",
				Studio:        []string{show.Network},
			},
			Properties: &xbmc.ListItemProperties{
				TotalEpisodes: strconv.Itoa(show.AiredEpisodes),
			},
			UniqueIDs: &xbmc.UniqueIDs{
				TMDB: strconv.Itoa(show.IDs.TMDB),
			},
		}
		if tmdbShow != nil {
			tmdbShow.SetArt(item)
		}
	}

	if ls, err := uid.GetShowByTMDB(show.IDs.TMDB); ls != nil && err == nil {
		item.Info.DBID = ls.UIDs.Kodi
	}

	if config.Get().ShowUnwatchedEpisodesNumber && item.Properties != nil && tmdbShow != nil {
		totalEpisodes := tmdbShow.CountEpisodesNumber()
		item.Properties.TotalSeasons = strconv.Itoa(tmdbShow.CountRealSeasons())
		item.Properties.TotalEpisodes = strconv.Itoa(totalEpisodes)

		watchedEpisodes := tmdbShow.CountWatchedEpisodesNumber()
		item.Properties.WatchedEpisodes = strconv.Itoa(watchedEpisodes)
		item.Properties.UnWatchedEpisodes = strconv.Itoa(totalEpisodes - watchedEpisodes)
	}

	if len(item.Info.Trailer) == 0 {
		item.Info.Trailer = util.TrailerURL(show.Trailer)
	}

	return
}

// ToListItem ...
func (episode *Episode) ToListItem(show *Show, tmdbShow *tmdb.Show) (item *xbmc.ListItem) {
	defer perf.ScopeTimer()()

	if show == nil || show.IDs == nil || episode == nil || episode.IDs == nil {
		return nil
	}

	var tmdbSeason *tmdb.Season
	var tmdbEpisode *tmdb.Episode
	if show.IDs.TMDB != 0 {
		if tmdbShow == nil {
			tmdbShow = tmdb.GetShow(show.IDs.TMDB, config.Get().Language)
		}

		if tmdbShow != nil {
			if tmdbSeason = tmdb.GetSeason(show.IDs.TMDB, episode.Season, config.Get().Language, len(tmdbShow.Seasons), false); tmdbSeason != nil {
				if tmdbEpisode = tmdb.GetEpisode(show.IDs.TMDB, episode.Season, episode.Number, config.Get().Language); tmdbEpisode != nil {
					if !config.Get().ForceUseTrakt {
						item = tmdbEpisode.ToListItem(tmdbShow, tmdbSeason)
					}
				}
			}
		}
	}

	if item == nil {
		episodeLabel := episode.Title
		if config.Get().AddEpisodeNumbers {
			episodeLabel = fmt.Sprintf("%dx%02d %s", episode.Season, episode.Number, episode.Title)
		}

		runtime := episode.Runtime * 60
		if runtime == 0 {
			runtime = show.Runtime * 60
		}

		firstAired, _ := time.Parse(time.RFC3339, episode.FirstAired)
		item = &xbmc.ListItem{
			Label:  episodeLabel,
			Label2: fmt.Sprintf("%f", episode.Rating),
			Info: &xbmc.ListItemInfo{
				Count:         rand.Int(),
				Title:         episodeLabel,
				OriginalTitle: episode.Title,
				Season:        episode.Season,
				Episode:       episode.Number,
				TVShowTitle:   show.Title,
				Plot:          episode.Overview,
				PlotOutline:   episode.Overview,
				Rating:        episode.Rating,
				Year:          firstAired.Year(),
				Aired:         firstAired.Format(time.DateOnly),
				Duration:      runtime,
				Genre:         show.Genres,
				Code:          show.IDs.IMDB,
				IMDBNumber:    show.IDs.IMDB,
				PlayCount:     playcount.GetWatchedEpisodeByTMDB(show.IDs.TMDB, episode.Season, episode.Number).Int(),
				DBTYPE:        "episode",
				Mediatype:     "episode",
				Studio:        []string{show.Network},
			},
			UniqueIDs: &xbmc.UniqueIDs{
				TMDB: strconv.Itoa(episode.IDs.TMDB),
			},
			Properties: &xbmc.ListItemProperties{
				ShowTMDBId: strconv.Itoa(show.IDs.TMDB),
			},
		}
		if tmdbEpisode != nil {
			tmdbEpisode.SetArt(tmdbShow, tmdbSeason, item)
		}
	}

	if ls, err := uid.GetShowByTMDB(show.IDs.TMDB); ls != nil && err == nil {
		if le := ls.GetEpisode(episode.Season, episode.Number); le != nil {
			item.Info.DBID = le.UIDs.Kodi
		}
	}

	return item
}

func (s WatchedShowsType) ToShows() []*Shows {
	ret := []*Shows{}
	for _, show := range s {
		ret = append(ret, &Shows{Show: show.Show})
	}
	return ret
}

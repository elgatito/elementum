package tmdb

import (
	"fmt"
	"math/rand"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/elgatito/elementum/cache"
	"github.com/elgatito/elementum/config"
	"github.com/elgatito/elementum/fanart"
	"github.com/elgatito/elementum/library/playcount"
	"github.com/elgatito/elementum/library/uid"
	"github.com/elgatito/elementum/tvdb"
	"github.com/elgatito/elementum/util"
	"github.com/elgatito/elementum/util/reqapi"
	"github.com/elgatito/elementum/xbmc"

	"github.com/anacrolix/missinggo/perf"
	"github.com/anacrolix/sync"
	"github.com/jmcvetta/napping"
)

// LogError ...
func LogError(err error) {
	if err != nil {
		pc, fn, line, _ := runtime.Caller(1)
		log.Errorf("in %s[%s:%d] %#v: %v)", runtime.FuncForPC(pc).Name(), fn, line, err, err)
	}
}

// GetShowImages ...
func GetShowImages(showID int) *Images {
	defer perf.ScopeTimer()()

	var images *Images
	req := reqapi.Request{
		API: reqapi.TMDBAPI,
		URL: fmt.Sprintf("/tv/%d/images", showID),
		Params: napping.Params{
			"api_key":                apiKey,
			"include_image_language": fmt.Sprintf("%s,%s,null", config.Get().Language, config.Get().SecondLanguage),
			"include_video_language": fmt.Sprintf("%s,%s,null", config.Get().Language, config.Get().SecondLanguage),
		}.AsUrlValues(),
		Result:      &images,
		Description: "show images",

		Cache:       true,
		CacheExpire: cache.CacheExpireLong,
	}

	req.Do()
	return images
}

// GetSeasonImages ...
func GetSeasonImages(showID int, season int) *Images {
	defer perf.ScopeTimer()()

	var images *Images
	req := reqapi.Request{
		API: reqapi.TMDBAPI,
		URL: fmt.Sprintf("/tv/%d/season/%d/images", showID, season),
		Params: napping.Params{
			"api_key":                apiKey,
			"include_image_language": fmt.Sprintf("%s,%s,null", config.Get().Language, config.Get().SecondLanguage),
			"include_video_language": fmt.Sprintf("%s,%s,null", config.Get().Language, config.Get().SecondLanguage),
		}.AsUrlValues(),
		Result:      &images,
		Description: "season images",

		Cache:       true,
		CacheExpire: cache.CacheExpireLong,
	}

	req.Do()
	return images
}

// GetEpisodeImages ...
func GetEpisodeImages(showID, season, episode int) *Images {
	defer perf.ScopeTimer()()

	var images *Images
	req := reqapi.Request{
		API: reqapi.TMDBAPI,
		URL: fmt.Sprintf("/tv/%d/season/%d/episode/%d/images", showID, season, episode),
		Params: napping.Params{
			"api_key":                apiKey,
			"include_image_language": fmt.Sprintf("%s,%s,null", config.Get().Language, config.Get().SecondLanguage),
			"include_video_language": fmt.Sprintf("%s,%s,null", config.Get().Language, config.Get().SecondLanguage),
		}.AsUrlValues(),
		Result:      &images,
		Description: "episode images",

		Cache:       true,
		CacheExpire: cache.CacheExpireLong,
	}

	req.Do()
	return images
}

// GetShowByID ...
func GetShowByID(tmdbID string, language string) *Show {
	id, _ := strconv.Atoi(tmdbID)
	return GetShow(id, language)
}

// GetShow ...
func GetShow(showID int, language string) (show *Show) {
	if showID == 0 {
		return
	}

	defer perf.ScopeTimer()()

	req := reqapi.Request{
		API: reqapi.TMDBAPI,
		URL: fmt.Sprintf("/tv/%d", showID),
		Params: napping.Params{
			"api_key":                apiKey,
			"append_to_response":     "credits,images,alternative_titles,translations,external_ids,content_ratings",
			"include_image_language": fmt.Sprintf("%s,%s,null", config.Get().Language, config.Get().SecondLanguage),
			"include_video_language": fmt.Sprintf("%s,%s,null", config.Get().Language, config.Get().SecondLanguage),
			"language":               language,
		}.AsUrlValues(),
		Result:      &show,
		Description: "show",

		Cache:       true,
		CacheExpire: cache.CacheExpireLong,
	}

	req.Do()
	if show == nil {
		return nil
	}

	switch t := show.RawPopularity.(type) {
	case string:
		if popularity, err := strconv.ParseFloat(t, 64); err == nil {
			show.Popularity = popularity
		}
	case float64:
		show.Popularity = t
	}

	return show
}

// GetShows ...
func GetShows(showIds []int, language string) Shows {
	defer perf.ScopeTimer()()

	var wg sync.WaitGroup
	shows := make(Shows, len(showIds))
	wg.Add(len(showIds))
	for i, showID := range showIds {
		go func(i int, showId int) {
			defer wg.Done()
			shows[i] = GetShow(showId, language)
		}(i, showID)
	}
	wg.Wait()
	return shows
}

// SearchShows ...
func SearchShows(query string, language string, page int) (Shows, int) {
	var results EntityList
	req := reqapi.Request{
		API: reqapi.TMDBAPI,
		URL: "/search/tv",
		Params: napping.Params{
			"api_key":       apiKey,
			"query":         query,
			"page":          strconv.Itoa(page),
			"language":      language,
			"include_adult": "false",
		}.AsUrlValues(),
		Result:      &results,
		Description: "search show",
	}

	if err := req.Do(); err != nil || (results.Results != nil && len(results.Results) == 0) {
		return nil, 0
	}

	tmdbIds := make([]int, 0, len(results.Results))
	for _, entity := range results.Results {
		tmdbIds = append(tmdbIds, entity.ID)
	}
	return GetShows(tmdbIds, language), results.TotalResults
}

func listShows(endpoint string, params napping.Params, page int) (Shows, int) {
	defer perf.ScopeTimer()()

	params["api_key"] = apiKey
	totalResults := -1

	requestPerPage := config.Get().ResultsPerPage
	requestLimitStart := (page - 1) * requestPerPage
	requestLimitEnd := page*requestPerPage - 1

	pageStart := requestLimitStart / TMDBResultsPerPage
	pageEnd := requestLimitEnd / TMDBResultsPerPage

	shows := make(Shows, requestPerPage)

	wg := sync.WaitGroup{}
	for p := pageStart; p <= pageEnd; p++ {
		wg.Add(1)
		go func(currentPage int) {
			defer wg.Done()
			var results *EntityList
			pageParams := napping.Params{
				"page": strconv.Itoa(currentPage + 1),
			}
			for k, v := range params {
				pageParams[k] = v
			}

			req := reqapi.Request{
				API:         reqapi.TMDBAPI,
				URL:         fmt.Sprintf("/%s", endpoint),
				Params:      pageParams.AsUrlValues(),
				Result:      &results,
				Description: "list shows",

				Cache:       true,
				CacheExpire: cache.CacheExpireMedium,
			}

			if err := req.Do(); err != nil || results == nil {
				return
			}

			if totalResults == -1 {
				totalResults = results.TotalResults
			}

			var wgItems sync.WaitGroup
			wgItems.Add(len(results.Results))
			for s, show := range results.Results {
				rindex := currentPage*TMDBResultsPerPage - requestLimitStart + s
				if show == nil || rindex >= len(shows) || rindex < 0 {
					wgItems.Done()
					continue
				}

				go func(rindex int, tmdbId int) {
					defer wgItems.Done()
					shows[rindex] = GetShow(tmdbId, params["language"])
				}(rindex, show.ID)
			}
			wgItems.Wait()
		}(p)
	}
	wg.Wait()

	return shows, totalResults
}

// PopularShows ...
func PopularShows(params DiscoverFilters, language string, page int) (Shows, int) {
	var p napping.Params
	if params.Genre != "" {
		p = napping.Params{
			"language":           language,
			"sort_by":            "popularity.desc",
			"first_air_date.lte": time.Now().UTC().Format(time.DateOnly),
			"with_genres":        params.Genre,
		}
	} else if params.Country != "" {
		p = napping.Params{
			"language":            language,
			"sort_by":             "popularity.desc",
			"first_air_date.lte":  time.Now().UTC().Format(time.DateOnly),
			"with_origin_country": params.Country,
		}
	} else if params.Language != "" {
		p = napping.Params{
			"language":               language,
			"sort_by":                "popularity.desc",
			"first_air_date.lte":     time.Now().UTC().Format(time.DateOnly),
			"with_original_language": params.Language,
		}
	} else {
		p = napping.Params{
			"language":           language,
			"sort_by":            "popularity.desc",
			"first_air_date.lte": time.Now().UTC().Format(time.DateOnly),
		}
	}

	return listShows("discover/tv", p, page)
}

// RecentShows ...
func RecentShows(params DiscoverFilters, language string, page int) (Shows, int) {
	var p napping.Params
	if params.Genre != "" {
		p = napping.Params{
			"language":           language,
			"sort_by":            "first_air_date.desc",
			"first_air_date.lte": time.Now().UTC().Format(time.DateOnly),
			"with_genres":        params.Genre,
		}
	} else if params.Country != "" {
		p = napping.Params{
			"language":           language,
			"sort_by":            "first_air_date.desc",
			"first_air_date.lte": time.Now().UTC().Format(time.DateOnly),
			"region":             params.Country,
		}
	} else if params.Language != "" {
		p = napping.Params{
			"language":               language,
			"sort_by":                "first_air_date.desc",
			"first_air_date.lte":     time.Now().UTC().Format(time.DateOnly),
			"with_original_language": params.Language,
		}
	} else {
		p = napping.Params{
			"language":           language,
			"sort_by":            "first_air_date.desc",
			"first_air_date.lte": time.Now().UTC().Format(time.DateOnly),
		}
	}

	return listShows("discover/tv", p, page)
}

// RecentEpisodes ...
func RecentEpisodes(params DiscoverFilters, language string, page int) (Shows, int) {
	var p napping.Params

	if params.Genre != "" {
		p = napping.Params{
			"language":           language,
			"air_date.gte":       time.Now().UTC().AddDate(0, 0, -3).Format(time.DateOnly),
			"first_air_date.lte": time.Now().UTC().Format(time.DateOnly),
			"with_genres":        params.Genre,
		}
	} else if params.Country != "" {
		p = napping.Params{
			"language":           language,
			"air_date.gte":       time.Now().UTC().AddDate(0, 0, -3).Format(time.DateOnly),
			"first_air_date.lte": time.Now().UTC().Format(time.DateOnly),
			"region":             params.Country,
		}
	} else if params.Language != "" {
		p = napping.Params{
			"language":               language,
			"air_date.gte":           time.Now().UTC().AddDate(0, 0, -3).Format(time.DateOnly),
			"first_air_date.lte":     time.Now().UTC().Format(time.DateOnly),
			"with_original_language": params.Language,
		}
	} else {
		p = napping.Params{
			"language":           language,
			"air_date.gte":       time.Now().UTC().AddDate(0, 0, -3).Format(time.DateOnly),
			"first_air_date.lte": time.Now().UTC().Format(time.DateOnly),
		}
	}

	return listShows("discover/tv", p, page)
}

// TopRatedShows ...
func TopRatedShows(genre string, language string, page int) (Shows, int) {
	return listShows("tv/top_rated", napping.Params{"language": language}, page)
}

// MostVotedShows ...
func MostVotedShows(genre string, language string, page int) (Shows, int) {
	return listShows("discover/tv", napping.Params{
		"language":           language,
		"sort_by":            "vote_count.desc",
		"first_air_date.lte": time.Now().UTC().Format(time.DateOnly),
		"with_genres":        genre,
	}, page)
}

// GetTVGenres ...
func GetTVGenres(language string) []*Genre {
	genres := GenreList{}
	var err error

	req := reqapi.Request{
		API: reqapi.TMDBAPI,
		URL: "/genre/tv/list",
		Params: napping.Params{
			"api_key":  apiKey,
			"language": language,
		}.AsUrlValues(),
		Result:      &genres,
		Description: "show genres",

		Cache:       true,
		CacheExpire: cache.CacheExpireLong,
	}

	// That is a special case, when language in on TMDB, but it results empty names.
	//   example of this: Catalan language.
	if err = req.Do(); err == nil && genres.Genres != nil && len(genres.Genres) > 0 && genres.Genres[0].Name == "" {
		req = reqapi.Request{
			API: reqapi.TMDBAPI,
			URL: "/genre/tv/list",
			Params: napping.Params{
				"api_key":  apiKey,
				"language": "en-US",
			}.AsUrlValues(),
			Result:      &genres,
			Description: "show genres",

			Cache:       true,
			CacheExpire: cache.CacheExpireLong,
		}

		err = req.Do()
	}

	if err == nil && genres.Genres != nil && len(genres.Genres) > 0 {
		for _, i := range genres.Genres {
			i.Name = strings.Title(i.Name)
		}

		sort.Slice(genres.Genres, func(i, j int) bool {
			return genres.Genres[i].Name < genres.Genres[j].Name
		})

	}
	return genres.Genres
}

// GetSeasonEpisodes ...
func (show *Show) GetSeasonEpisodes(season int) int {
	if len(show.Seasons) == 0 {
		return 0
	}

	for _, s := range show.Seasons {
		if s != nil && s.Season == season {
			return s.EpisodeCount
		}
	}

	return 0
}

// IsAnime ...
func (show *Show) IsAnime() bool {
	if show == nil || show.OriginCountry == nil || show.Genres == nil {
		return false
	}

	countryIsJP := false
	for _, country := range show.OriginCountry {
		if country == "JP" {
			countryIsJP = true
			break
		}
	}
	genreIsAnim := false
	for _, genre := range show.Genres {
		if genre.ID == 16 {
			genreIsAnim = true
			break
		}
	}

	return countryIsJP && genreIsAnim
}

// ShowInfo returns absolute episode number and show title
func (show *Show) ShowInfo(episode *Episode) (an int, st string) {
	tvdbID := util.StrInterfaceToInt(show.ExternalIDs.TVDBID)
	if tvdbShow, err := tvdb.GetShow(tvdbID, config.Get().Language); err == nil {
		return show.ShowInfoWithTVDBShow(episode, tvdbShow)
	}

	return
}

// ShowInfoWithTVDBShow ...
func (show *Show) ShowInfoWithTVDBShow(episode *Episode, tvdbShow *tvdb.Show) (an int, st string) {
	if tvdbShow != nil && episode != nil && episode.SeasonNumber > 0 && len(tvdbShow.Seasons) >= episode.SeasonNumber {
		if tvdbSeason := tvdbShow.GetSeason(episode.SeasonNumber); tvdbSeason != nil && len(tvdbSeason.Episodes) >= episode.EpisodeNumber {
			if tvdbEpisode := tvdbSeason.GetEpisode(episode.EpisodeNumber); tvdbEpisode != nil && tvdbEpisode.AbsoluteNumber > 0 {
				an = tvdbEpisode.AbsoluteNumber
			} else if episode.SeasonNumber == 1 {
				an = episode.EpisodeNumber
			}

			st = tvdbShow.SeriesName
		}
	}

	return
}

// SetArt sets artworks for show
func (show *Show) SetArt(item *xbmc.ListItem) {
	if item.Art == nil {
		item.Art = &xbmc.ListItemArt{}
	}

	item.Art.FanArt = ImageURL(show.BackdropPath, "w1280")
	item.Art.Banner = ImageURL(show.BackdropPath, "w1280")
	item.Art.Poster = ImageURL(show.PosterPath, "w1280")
	item.Art.Thumbnail = ImageURL(show.PosterPath, "w1280")
	item.Art.TvShowPoster = ImageURL(show.PosterPath, "w1280")

	if item.Art.AvailableArtworks == nil {
		item.Art.AvailableArtworks = &xbmc.Artworks{}
	}

	if show.Images != nil && show.Images.Backdrops != nil {
		fanarts := make([]string, 0)
		foundLanguageSpecificImage := false
		for _, backdrop := range show.Images.Backdrops {
			// for AvailableArtworks
			fanarts = append(fanarts, ImageURL(backdrop.FilePath, "w1280"))

			// try to use language specific art
			if !foundLanguageSpecificImage && backdrop.Iso639_1 == config.Get().Language {
				item.Art.FanArt = ImageURL(backdrop.FilePath, "w1280")
				item.Art.Banner = ImageURL(backdrop.FilePath, "w1280")
				foundLanguageSpecificImage = true // we take first image, it has top rating
			}
		}
		if len(fanarts) > 0 {
			item.Art.FanArts = fanarts
			item.Art.AvailableArtworks.FanArt = fanarts
			item.Art.AvailableArtworks.Banner = fanarts
		}
	}

	if show.Images != nil && show.Images.Posters != nil {
		posters := make([]string, 0)
		foundLanguageSpecificImage := false
		for _, poster := range show.Images.Posters {
			// for AvailableArtworks
			posters = append(posters, ImageURL(poster.FilePath, "w1280"))

			// try to use language specific art
			if !foundLanguageSpecificImage && poster.Iso639_1 == config.Get().Language {
				item.Art.Poster = ImageURL(poster.FilePath, "w1280")
				item.Art.Thumbnail = ImageURL(poster.FilePath, "w1280")
				foundLanguageSpecificImage = true // we take first image, it has top rating
			}
		}
		if len(posters) > 0 {
			item.Art.AvailableArtworks.Poster = posters
		}
	}

	if config.Get().UseFanartTv {
		if show.FanArt == nil && show.ExternalIDs != nil {
			show.FanArt = fanart.GetShow(util.StrInterfaceToInt(show.ExternalIDs.TVDBID))
		}
		if show.FanArt != nil {
			item.Art = show.FanArt.ToListItemArt(item.Art)
		}
	}

	item.Thumbnail = item.Art.Poster
}

// ToListItem ...
func (show *Show) ToListItem() *xbmc.ListItem {
	defer perf.ScopeTimer()()

	year, _ := strconv.Atoi(strings.Split(show.FirstAirDate, "-")[0])

	name := show.GetName()

	if config.Get().ShowUnwatchedEpisodesNumber {
		// Get all seasons information for this show, it is required to get Air dates
		show.NumberOfEpisodes = show.CountEpisodesNumber()
	}

	item := &xbmc.ListItem{
		Label: name,
		Info: &xbmc.ListItemInfo{
			Year:          year,
			Aired:         show.FirstAirDate,
			Count:         rand.Int(),
			Title:         name,
			OriginalTitle: show.OriginalName,
			Plot:          show.overview(),
			PlotOutline:   show.overview(),
			TagLine:       show.TagLine,
			Date:          show.FirstAirDate,
			Votes:         strconv.Itoa(show.VoteCount),
			Rating:        show.VoteAverage,
			TVShowTitle:   show.OriginalName,
			Premiered:     show.FirstAirDate,
			PlayCount:     playcount.GetWatchedShowByTMDB(show.ID).Int(),
			MPAA:          show.mpaa(),
			DBTYPE:        "tvshow",
			Mediatype:     "tvshow",
			Genre:         show.GetGenres(),
			Studio:        show.GetStudios(),
			Country:       show.GetCountries(),
		},
		Properties: &xbmc.ListItemProperties{
			TotalSeasons:  strconv.Itoa(show.CountRealSeasons()),
			TotalEpisodes: strconv.Itoa(show.NumberOfEpisodes),
		},
		UniqueIDs: &xbmc.UniqueIDs{
			TMDB: strconv.Itoa(show.ID),
		},
	}
	if show.ExternalIDs != nil {
		item.Info.Code = show.ExternalIDs.IMDBId
		item.Info.IMDBNumber = show.ExternalIDs.IMDBId
	}
	if len(show.EpisodeRunTime) > 0 {
		item.Info.Duration = show.EpisodeRunTime[len(show.EpisodeRunTime)-1] * 60 * show.NumberOfEpisodes
	}

	if ls, err := uid.GetShowByTMDB(show.ID); ls != nil && err == nil {
		item.Info.DBID = ls.UIDs.Kodi
	}

	show.SetArt(item)

	if config.Get().ShowUnwatchedEpisodesNumber {
		watchedEpisodes := show.CountWatchedEpisodesNumber()
		item.Properties.WatchedEpisodes = strconv.Itoa(watchedEpisodes)
		item.Properties.UnWatchedEpisodes = strconv.Itoa(show.NumberOfEpisodes - watchedEpisodes)
	}

	if show.InProduction {
		item.Info.Status = "Continuing"
	} else {
		item.Info.Status = "Discontinued"
	}

	for _, language := range show.SpokenLanguages {
		item.StreamInfo = &xbmc.StreamInfo{
			Audio: &xbmc.StreamInfoEntry{
				Language: language.Iso639_1,
			},
		}
		break
	}

	if show.Credits != nil {
		item.CastMembers = show.Credits.GetCastMembers()
		item.Info.Director = show.Credits.GetDirectors()
		item.Info.Writer = show.Credits.GetWriters()
	}

	return item
}

func (show *Show) mpaa() string {
	if show.ContentRatings == nil || show.ContentRatings.Ratings == nil || len(show.ContentRatings.Ratings) == 0 {
		return ""
	}

	region := config.Get().Region
	for _, r := range show.ContentRatings.Ratings {
		if strings.ToUpper(r.Iso3166_1) != region {
			continue
		}

		if r.Rating != "" {
			return r.Rating
		}
	}

	return ""
}

func (show *Show) GetName() string {
	if config.Get().UseOriginalTitle && show.OriginalName != "" {
		return show.OriginalName
	}

	// By default, if TMDB is returning a translated title - we use it
	if show.Name != "" && show.Name == show.OriginalName && show.OriginalLanguage == config.Get().Language {
		return show.Name
	}

	// If we have a title, but we don't have translations - use it
	if (show.Name != "" && show.Name != show.OriginalName) || show.Translations == nil || show.Translations.Translations == nil || len(show.Translations.Translations) == 0 {
		return show.Name
	}

	// Find translations in this order: Kodi language -> Second language -> Original language

	current := show.findTranslation(config.Get().Language)
	if current != nil && current.Data != nil && current.Data.Name != "" {
		return current.Data.Name
	}

	current = show.findTranslation(config.Get().SecondLanguage)
	if current != nil && current.Data != nil && current.Data.Name != "" {
		return current.Data.Name
	}

	current = show.findTranslation(show.OriginalLanguage)
	if current != nil && current.Data != nil && current.Data.Name != "" {
		return current.Data.Name
	}

	return show.Name
}

func (show *Show) overview() string {
	if show.Overview != "" || show.Translations == nil || show.Translations.Translations == nil || len(show.Translations.Translations) == 0 {
		return show.Overview
	}

	current := show.findTranslation(config.Get().Language)
	if current != nil && current.Data != nil && current.Data.Overview != "" {
		return current.Data.Overview
	}

	current = show.findTranslation(config.Get().SecondLanguage)
	if current != nil && current.Data != nil && current.Data.Overview != "" {
		return current.Data.Overview
	}

	current = show.findTranslation(show.OriginalLanguage)
	if current != nil && current.Data != nil && current.Data.Overview != "" {
		return current.Data.Overview
	}

	return show.Overview
}

func (show *Show) findTranslation(language string) *Translation {
	if language == "" || show.Translations == nil || show.Translations.Translations == nil || len(show.Translations.Translations) == 0 {
		return nil
	}

	language = strings.ToLower(language)
	for _, tr := range show.Translations.Translations {
		if strings.ToLower(tr.Iso639_1) == language {
			return tr
		}
	}

	return nil
}

// CountWatchedEpisodesNumber returns number of watched episodes
func (show *Show) CountWatchedEpisodesNumber() (watchedEpisodes int) {
	if playcount.GetWatchedShowByTMDB(show.ID) {
		watchedEpisodes = show.NumberOfEpisodes
	} else {
		for _, season := range show.Seasons {
			if season == nil {
				continue
			}
			watchedEpisodes += season.CountWatchedEpisodesNumber(show)
		}
	}
	return
}

// CountEpisodesNumber returns number of episodes taking into account unaired and special
func (show *Show) CountEpisodesNumber() (episodes int) {
	for _, season := range show.Seasons {
		if season == nil {
			continue
		}
		episodes += season.CountEpisodesNumber(show)
	}

	return
}

// EpisodesTillSeason counts how many episodes exist before this season.
func (show *Show) EpisodesTillSeason(season int) int {
	if len(show.Seasons) < season {
		return 0
	}

	ret := 0
	for _, s := range show.Seasons {
		if s != nil && s.Season > 0 && s.Season < season {
			ret += s.EpisodeCount
		}
	}
	return ret
}

// GetSeasonByRealNumber returns season object corresponding to real season number.
func (show *Show) GetSeasonByRealNumber(season int) *Season {
	if len(show.Seasons) <= 0 {
		return nil
	}

	for _, s := range show.Seasons {
		if s != nil && s.Season == season {
			return s
		}
	}
	return nil
}

// CountRealSeasons counts real seasons, meaning without specials.
func (show *Show) CountRealSeasons() int {
	if len(show.Seasons) <= 0 {
		return 0
	}

	c := config.Get()

	ret := 0
	for _, s := range show.Seasons {
		if s == nil {
			continue
		}

		if !c.ShowUnairedSeasons {
			if _, isAired := util.AirDateWithAiredCheck(s.AirDate, time.DateOnly, c.ShowEpisodesOnReleaseDay); !isAired {
				continue
			}
		}
		if !c.ShowSeasonsSpecials && s.Season <= 0 {
			continue
		}

		ret++
	}
	return ret
}

// GetCountries returns list of countries
func (show *Show) GetCountries() []string {
	countries := make([]string, 0, len(show.ProductionCountries))
	for _, country := range show.ProductionCountries {
		countries = append(countries, country.Name)
	}

	return countries
}

// GetStudios returns list of studios
func (show *Show) GetStudios() []string {
	if config.Get().TMDBShowUseProdCompanyAsStudio {
		studios := show.GetProductionCompanies()
		if len(studios) != 0 {
			return studios
		} else {
			return show.GetNetworks()
		}
	} else {
		studios := show.GetNetworks()
		if len(studios) != 0 {
			return studios
		} else {
			return show.GetProductionCompanies()
		}
	}
}

// GetProductionCompanies returns list of production companies
func (show *Show) GetProductionCompanies() []string {
	companies := make([]string, 0, len(show.ProductionCompanies))
	for _, company := range show.ProductionCompanies {
		companies = append(companies, company.Name)
	}

	return companies
}

// GetNetworks returns list of networks
func (show *Show) GetNetworks() []string {
	networks := make([]string, 0, len(show.Networks))
	for _, network := range show.Networks {
		networks = append(networks, network.Name)
	}

	return networks
}

// GetGenres returns list of genres
func (show *Show) GetGenres() []string {
	genres := make([]string, 0, len(show.Genres))
	for _, genre := range show.Genres {
		genres = append(genres, genre.Name)
	}

	return genres
}

func (show *Show) EnsureSeason(season int) *Season {
	if show.Seasons == nil {
		return nil
	}

	for idx, s := range show.Seasons {
		if s == nil || s.Season != season {
			continue
		}

		if s.Episodes != nil && len(s.Episodes) > 0 {
			return s
		}

		show.Seasons[idx] = GetSeason(show.ID, season, config.Get().Language, len(show.Seasons), false)
		return show.Seasons[idx]
	}

	return nil
}

func (show *Show) GetSeasonAirDate(season int) time.Time {
	if season <= 0 {
		return time.Time{}
	}

	for _, s := range show.Seasons {
		if s == nil || s.Season != season {
			continue
		}

		if aired, err := time.Parse(time.DateOnly, s.AirDate); err == nil {
			return aired
		}

		return time.Time{}
	}

	return time.Time{}
}

func (show *Show) IsSeasonAired(season int) bool {
	if season <= 0 {
		return false
	}

	// If Last aired episode is from newer season - then everything older is considered aired
	if show.LastEpisodeToAir != nil && show.LastEpisodeToAir.SeasonNumber > season {
		return true
	}

	// If next season is aired - then everything older is considered aired
	nextSeasonAired := show.GetSeasonAirDate(season + 1)
	return !nextSeasonAired.IsZero() && nextSeasonAired.Before(time.Now())
}

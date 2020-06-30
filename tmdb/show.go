package tmdb

import (
	"fmt"
	"math/rand"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/elgatito/elementum/cache"
	"github.com/elgatito/elementum/config"
	"github.com/elgatito/elementum/fanart"
	"github.com/elgatito/elementum/playcount"
	"github.com/elgatito/elementum/tvdb"
	"github.com/elgatito/elementum/util"
	"github.com/elgatito/elementum/xbmc"
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
	var images *Images
	cacheStore := cache.NewDBStore()
	key := fmt.Sprintf("com.tmdb.show.%d.images", showID)
	if err := cacheStore.Get(key, &images); err != nil {
		err = MakeRequest(APIRequest{
			URL: fmt.Sprintf("%s/tv/%d/images", tmdbEndpoint, showID),
			Params: napping.Params{
				"api_key":                apiKey,
				"include_image_language": fmt.Sprintf("%s,en,null", config.Get().Language),
			}.AsUrlValues(),
			Result:      &images,
			Description: "show images",
		})

		if images != nil {
			cacheStore.Set(key, images, imagesCacheExpiration)
		}
	}
	return images
}

// GetSeasonImages ...
func GetSeasonImages(showID int, season int) *Images {
	var images *Images
	cacheStore := cache.NewDBStore()
	key := fmt.Sprintf("com.tmdb.show.%d.%d.images", showID, season)
	if err := cacheStore.Get(key, &images); err != nil {
		err = MakeRequest(APIRequest{
			URL: fmt.Sprintf("%s/tv/%d/season/%d/images", tmdbEndpoint, showID, season),
			Params: napping.Params{
				"api_key":                apiKey,
				"include_image_language": fmt.Sprintf("%s,en,null", config.Get().Language),
			}.AsUrlValues(),
			Result:      &images,
			Description: "season images",
		})

		if images != nil {
			cacheStore.Set(key, images, imagesCacheExpiration)
		}
	}
	return images
}

// GetEpisodeImages ...
func GetEpisodeImages(showID, season, episode int) *Images {
	var images *Images
	cacheStore := cache.NewDBStore()
	key := fmt.Sprintf("com.tmdb.show.%d.%d.%d.images", showID, season, episode)
	if err := cacheStore.Get(key, &images); err != nil {
		err = MakeRequest(APIRequest{
			URL: fmt.Sprintf("%s/tv/%d/season/%d/episode/%d/images", tmdbEndpoint, showID, season, episode),
			Params: napping.Params{
				"api_key":                apiKey,
				"include_image_language": fmt.Sprintf("%s,en,null", config.Get().Language),
			}.AsUrlValues(),
			Result:      &images,
			Description: "season images",
		})

		if images != nil {
			cacheStore.Set(key, images, imagesCacheExpiration)
		}
	}
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
	cacheStore := cache.NewDBStore()
	key := fmt.Sprintf("com.tmdb.show.%d.%s", showID, language)
	if err := cacheStore.Get(key, &show); err != nil {
		err = MakeRequest(APIRequest{
			URL: fmt.Sprintf("%s/tv/%d", tmdbEndpoint, showID),
			Params: napping.Params{
				"api_key":            apiKey,
				"append_to_response": "credits,images,alternative_titles,translations,external_ids",
				"language":           language,
			}.AsUrlValues(),
			Result:      &show,
			Description: "show",
		})

		if show == nil && err != nil && err == util.ErrNotFound {
			cacheStore.Set(key, &show, cacheHalfExpiration)
		}
		if show == nil {
			return nil
		}

		cacheStore.Set(key, &show, cacheExpiration)
	}
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
	MakeRequest(APIRequest{
		URL: fmt.Sprintf("%s/search/tv", tmdbEndpoint),
		Params: napping.Params{
			"api_key": apiKey,
			"query":   query,
			"page":    strconv.Itoa(page),
		}.AsUrlValues(),
		Result:      &results,
		Description: "search show",
	})

	if results.Results != nil && len(results.Results) == 0 {
		return nil, 0
	}

	tmdbIds := make([]int, 0, len(results.Results))
	for _, entity := range results.Results {
		tmdbIds = append(tmdbIds, entity.ID)
	}
	return GetShows(tmdbIds, language), results.TotalResults
}

func listShows(endpoint string, cacheKey string, params napping.Params, page int) (Shows, int) {
	params["api_key"] = apiKey
	totalResults := -1

	genre := params["with_genres"]
	country := params["region"]
	language := params["with_original_language"]
	if params["with_genres"] == "" {
		genre = "all"
	}
	if params["region"] == "" {
		country = "all"
	}
	if params["with_original_language"] == "" {
		language = "all"
	}

	requestPerPage := config.Get().ResultsPerPage
	requestLimitStart := (page - 1) * requestPerPage
	requestLimitEnd := page*requestPerPage - 1

	pageStart := requestLimitStart / TMDBResultsPerPage
	pageEnd := requestLimitEnd / TMDBResultsPerPage

	shows := make(Shows, requestPerPage)

	cacheStore := cache.NewDBStore()
	key := fmt.Sprintf("com.tmdb.topshows.%s.%s.%s.%s.%d.%d", cacheKey, genre, country, language, requestPerPage, page)
	totalKey := fmt.Sprintf("com.tmdb.topshows.%s.%s.%s.%s.total", cacheKey, genre, country, language)
	if err := cacheStore.Get(key, &shows); err != nil {
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

				err = MakeRequest(APIRequest{
					URL:         fmt.Sprintf("%s/%s", tmdbEndpoint, endpoint),
					Params:      pageParams.AsUrlValues(),
					Result:      &results,
					Description: "list shows",
				})

				if results == nil {
					return
				}

				if totalResults == -1 {
					totalResults = results.TotalResults
					cacheStore.Set(totalKey, totalResults, recentExpiration)
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
		cacheStore.Set(key, shows, recentExpiration)
	} else {
		if err := cacheStore.Get(totalKey, &totalResults); err != nil {
			totalResults = -1
		}
	}
	return shows, totalResults
}

// PopularShows ...
func PopularShows(params DiscoverFilters, language string, page int) (Shows, int) {
	var p napping.Params
	if params.Genre != "" {
		p = napping.Params{
			"language":           language,
			"sort_by":            "popularity.desc",
			"first_air_date.lte": time.Now().UTC().Format("2006-01-02"),
			"with_genres":        params.Genre,
		}
	} else if params.Country != "" {
		p = napping.Params{
			"language":           language,
			"sort_by":            "popularity.desc",
			"first_air_date.lte": time.Now().UTC().Format("2006-01-02"),
			"region":             params.Country,
		}
	} else if params.Language != "" {
		p = napping.Params{
			"language":               language,
			"sort_by":                "popularity.desc",
			"first_air_date.lte":     time.Now().UTC().Format("2006-01-02"),
			"with_original_language": params.Language,
		}
	} else {
		p = napping.Params{
			"language":           language,
			"sort_by":            "popularity.desc",
			"first_air_date.lte": time.Now().UTC().Format("2006-01-02"),
		}
	}

	return listShows("discover/tv", "popular", p, page)
}

// RecentShows ...
func RecentShows(params DiscoverFilters, language string, page int) (Shows, int) {
	var p napping.Params
	if params.Genre != "" {
		p = napping.Params{
			"language":           language,
			"sort_by":            "first_air_date.desc",
			"first_air_date.lte": time.Now().UTC().Format("2006-01-02"),
			"with_genres":        params.Genre,
		}
	} else if params.Country != "" {
		p = napping.Params{
			"language":           language,
			"sort_by":            "first_air_date.desc",
			"first_air_date.lte": time.Now().UTC().Format("2006-01-02"),
			"region":             params.Country,
		}
	} else if params.Language != "" {
		p = napping.Params{
			"language":               language,
			"sort_by":                "first_air_date.desc",
			"first_air_date.lte":     time.Now().UTC().Format("2006-01-02"),
			"with_original_language": params.Language,
		}
	} else {
		p = napping.Params{
			"language":           language,
			"sort_by":            "first_air_date.desc",
			"first_air_date.lte": time.Now().UTC().Format("2006-01-02"),
		}
	}

	return listShows("discover/tv", "recent.shows", p, page)
}

// RecentEpisodes ...
func RecentEpisodes(params DiscoverFilters, language string, page int) (Shows, int) {
	var p napping.Params

	if params.Genre != "" {
		p = napping.Params{
			"language":           language,
			"air_date.gte":       time.Now().UTC().AddDate(0, 0, -3).Format("2006-01-02"),
			"first_air_date.lte": time.Now().UTC().Format("2006-01-02"),
			"with_genres":        params.Genre,
		}
	} else if params.Country != "" {
		p = napping.Params{
			"language":           language,
			"air_date.gte":       time.Now().UTC().AddDate(0, 0, -3).Format("2006-01-02"),
			"first_air_date.lte": time.Now().UTC().Format("2006-01-02"),
			"region":             params.Country,
		}
	} else if params.Language != "" {
		p = napping.Params{
			"language":               language,
			"air_date.gte":           time.Now().UTC().AddDate(0, 0, -3).Format("2006-01-02"),
			"first_air_date.lte":     time.Now().UTC().Format("2006-01-02"),
			"with_original_language": params.Language,
		}
	} else {
		p = napping.Params{
			"language":           language,
			"air_date.gte":       time.Now().UTC().AddDate(0, 0, -3).Format("2006-01-02"),
			"first_air_date.lte": time.Now().UTC().Format("2006-01-02"),
		}
	}

	return listShows("discover/tv", "recent.episodes", p, page)
}

// TopRatedShows ...
func TopRatedShows(genre string, language string, page int) (Shows, int) {
	return listShows("tv/top_rated", "toprated", napping.Params{"language": language}, page)
}

// MostVotedShows ...
func MostVotedShows(genre string, language string, page int) (Shows, int) {
	return listShows("discover/tv", "mostvoted", napping.Params{
		"language":           language,
		"sort_by":            "vote_count.desc",
		"first_air_date.lte": time.Now().UTC().Format("2006-01-02"),
		"with_genres":        genre,
	}, page)
}

// GetTVGenres ...
func GetTVGenres(language string) []*Genre {
	genres := GenreList{}

	cacheStore := cache.NewDBStore()
	key := fmt.Sprintf("com.tmdb.genres.shows.%s", language)
	if err := cacheStore.Get(key, &genres); err != nil {
		err = MakeRequest(APIRequest{
			URL: fmt.Sprintf("%s/genre/tv/list", tmdbEndpoint),
			Params: napping.Params{
				"api_key":  apiKey,
				"language": language,
			}.AsUrlValues(),
			Result:      &genres,
			Description: "show genres",
		})

		// That is a special case, when language in on TMDB, but it results empty names.
		//   example of this: Catalan language.
		if genres.Genres != nil && len(genres.Genres) > 0 && genres.Genres[0].Name == "" {
			err = MakeRequest(APIRequest{
				URL: fmt.Sprintf("%s/genre/tv/list", tmdbEndpoint),
				Params: napping.Params{
					"api_key":  apiKey,
					"language": "en-US",
				}.AsUrlValues(),
				Result:      &genres,
				Description: "show genres",
			})
		}

		if genres.Genres != nil && len(genres.Genres) > 0 {
			for _, i := range genres.Genres {
				i.Name = strings.Title(i.Name)
			}

			sort.Slice(genres.Genres, func(i, j int) bool {
				return genres.Genres[i].Name < genres.Genres[j].Name
			})

			cacheStore.Set(key, genres, cacheExpiration)
		}
	}
	return genres.Genres
}

// GetSeasonEpisodes ...
func (show *Show) GetSeasonEpisodes(season int) int {
	if len(show.Seasons) == 0 {
		return 0
	}

	for _, s := range show.Seasons {
		if s.Season == season {
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

// AnimeInfo returns absolute episode number and show title
func (show *Show) AnimeInfo(episode *Episode) (an int, st string) {
	tvdbID := util.StrInterfaceToInt(show.ExternalIDs.TVDBID)
	if tvdbShow, err := tvdb.GetShow(tvdbID, config.Get().Language); err == nil {
		return show.AnimeInfoWithShow(episode, tvdbShow)
	}

	return
}

// AnimeInfoWithShow ...
func (show *Show) AnimeInfoWithShow(episode *Episode, tvdbShow *tvdb.Show) (an int, st string) {
	if tvdbShow != nil && episode.SeasonNumber > 0 && len(tvdbShow.Seasons) >= episode.SeasonNumber {
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

// ToListItem ...
func (show *Show) ToListItem() *xbmc.ListItem {
	year, _ := strconv.Atoi(strings.Split(show.FirstAirDate, "-")[0])

	name := show.Name
	if config.Get().UseOriginalTitle && show.OriginalName != "" {
		name = show.OriginalName
	}

	item := &xbmc.ListItem{
		Label: name,
		Info: &xbmc.ListItemInfo{
			Year:          year,
			Count:         rand.Int(),
			Title:         name,
			OriginalTitle: show.OriginalName,
			Plot:          show.Overview,
			PlotOutline:   show.Overview,
			Code:          show.ExternalIDs.IMDBId,
			IMDBNumber:    show.ExternalIDs.IMDBId,
			Date:          show.FirstAirDate,
			Votes:         strconv.Itoa(show.VoteCount),
			Rating:        show.VoteAverage,
			TVShowTitle:   show.OriginalName,
			Premiered:     show.FirstAirDate,
			PlayCount:     playcount.GetWatchedShowByTMDB(show.ID).Int(),
			DBTYPE:        "tvshow",
			Mediatype:     "tvshow",
		},
		Art: &xbmc.ListItemArt{
			FanArt: ImageURL(show.BackdropPath, "w1280"),
			Poster: ImageURL(show.PosterPath, "w500"),
		},
	}

	item.Thumbnail = item.Art.Poster
	item.Art.Thumbnail = item.Art.Poster

	if config.Get().UseFanartTv {
		if fa := fanart.GetShow(util.StrInterfaceToInt(show.ExternalIDs.TVDBID)); fa != nil {
			item.Art = fa.ToListItemArt(item.Art)
			item.Thumbnail = item.Art.Thumbnail
		}
	}

	if show.InProduction {
		item.Info.Status = "Continuing"
	} else {
		item.Info.Status = "Discontinued"
	}

	genres := make([]string, 0, len(show.Genres))
	for _, genre := range show.Genres {
		genres = append(genres, genre.Name)
	}
	item.Info.Genre = strings.Join(genres, " / ")

	for _, company := range show.ProductionCompanies {
		item.Info.Studio = company.Name
		break
	}
	if show.Credits != nil {
		item.Info.CastAndRole = make([][]string, 0)
		for _, cast := range show.Credits.Cast {
			item.Info.CastAndRole = append(item.Info.CastAndRole, []string{cast.Name, cast.Character})
		}
		directors := make([]string, 0)
		writers := make([]string, 0)
		for _, crew := range show.Credits.Crew {
			switch crew.Job {
			case "Director":
				directors = append(directors, crew.Name)
			case "Writer":
				writers = append(writers, crew.Name)
			}
		}
		item.Info.Director = strings.Join(directors, " / ")
		item.Info.Writer = strings.Join(writers, " / ")
	}
	return item
}

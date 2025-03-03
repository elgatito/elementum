package tmdb

import (
	"fmt"
	"math/rand"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/elgatito/elementum/cache"
	"github.com/elgatito/elementum/config"
	"github.com/elgatito/elementum/fanart"
	"github.com/elgatito/elementum/library/playcount"
	"github.com/elgatito/elementum/library/uid"
	"github.com/elgatito/elementum/util/reqapi"
	"github.com/elgatito/elementum/xbmc"

	"github.com/anacrolix/missinggo/perf"
	"github.com/anacrolix/sync"
	"github.com/jmcvetta/napping"
)

// ByPopularity ...
type ByPopularity Movies

func (a ByPopularity) Len() int           { return len(a) }
func (a ByPopularity) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }
func (a ByPopularity) Less(i, j int) bool { return a[i].Popularity < a[j].Popularity }

// GetImages ...
func GetImages(movieID int) *Images {
	defer perf.ScopeTimer()()

	var images *Images
	languagesList := GetUserLanguages()

	req := reqapi.Request{
		API: reqapi.TMDBAPI,
		URL: fmt.Sprintf("/movie/%d/images", movieID),
		Params: napping.Params{
			"api_key":                apiKey,
			"include_image_language": languagesList,
			"include_video_language": languagesList,
		}.AsUrlValues(),
		Result:      &images,
		Description: "movie images",

		Cache:       true,
		CacheExpire: cache.CacheExpireLong,
	}

	req.Do()

	return images
}

// GetMovie ...
func GetMovie(tmdbID int, language string) *Movie {
	return GetMovieByID(strconv.Itoa(tmdbID), language)
}

// GetMovieByID ...
func GetMovieByID(movieID string, language string) *Movie {
	defer perf.ScopeTimer()()

	var movie *Movie
	languagesList := GetUserLanguages()

	req := reqapi.Request{
		API: reqapi.TMDBAPI,
		URL: fmt.Sprintf("/movie/%s", movieID),
		Params: napping.Params{
			"api_key":                apiKey,
			"append_to_response":     "credits,images,videos,translations,external_ids,alternative_titles,release_dates",
			"include_image_language": languagesList,
			"include_video_language": languagesList,
			"language":               language,
		}.AsUrlValues(),
		Result:      &movie,
		Description: "movie",

		Cache:       true,
		CacheExpire: cache.CacheExpireLong,
	}

	if req.Do(); movie == nil {
		return nil
	}

	switch t := movie.RawPopularity.(type) {
	case string:
		popularity, _ := strconv.ParseFloat(t, 64)
		movie.Popularity = popularity
	case float64:
		movie.Popularity = t
	}
	return movie
}

// GetMovies ...
func GetMovies(tmdbIds []int, language string) Movies {
	defer perf.ScopeTimer()()

	var wg sync.WaitGroup
	movies := make(Movies, len(tmdbIds))
	wg.Add(len(tmdbIds))
	for i, tmdbID := range tmdbIds {
		go func(i int, tmdbId int) {
			defer wg.Done()
			movies[i] = GetMovie(tmdbId, language)
		}(i, tmdbID)
	}
	wg.Wait()
	return movies
}

// GetMovieGenres ...
func GetMovieGenres(language string) []*Genre {
	defer perf.ScopeTimer()()

	var err error
	genres := GenreList{}

	req := reqapi.Request{
		API: reqapi.TMDBAPI,
		URL: "/genre/movie/list",
		Params: napping.Params{
			"api_key":  apiKey,
			"language": language,
		}.AsUrlValues(),
		Result:      &genres,
		Description: "movie genres",

		Cache:       true,
		CacheExpire: cache.CacheExpireLong,
	}

	// That is a special case, when language in on TMDB, but it results empty names.
	//   example of this: Catalan language.
	if err = req.Do(); err == nil && genres.Genres != nil && len(genres.Genres) > 0 && genres.Genres[0].Name == "" {
		req = reqapi.Request{
			API: reqapi.TMDBAPI,
			URL: "/genre/movie/list",
			Params: napping.Params{
				"api_key":  apiKey,
				"language": "en-US",
			}.AsUrlValues(),
			Result:      &genres,
			Description: "movie genres",

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

// SearchMovies ...
func SearchMovies(query string, language string, page int) (Movies, int) {
	defer perf.ScopeTimer()()

	var results EntityList

	req := reqapi.Request{
		API: reqapi.TMDBAPI,
		URL: "/search/movie",
		Params: napping.Params{
			"api_key":       apiKey,
			"query":         query,
			"page":          strconv.Itoa(page),
			"language":      language,
			"include_adult": "false",
		}.AsUrlValues(),
		Result:      &results,
		Description: "search movie",
	}

	if err := req.Do(); err != nil || (results.Results != nil && len(results.Results) == 0) {
		return nil, 0
	}

	tmdbIds := make([]int, 0, len(results.Results))
	for _, movie := range results.Results {
		tmdbIds = append(tmdbIds, movie.ID)
	}
	return GetMovies(tmdbIds, language), results.TotalResults
}

// GetIMDBList ...
func GetIMDBList(listID string, language string, page int) (movies Movies, totalResults int) {
	defer perf.ScopeTimer()()

	var results *List
	totalResults = -1

	requestPerPage := config.Get().ResultsPerPage
	requestLimitStart := (page - 1) * requestPerPage
	requestLimitEnd := page*requestPerPage - 1

	req := reqapi.Request{
		API: reqapi.TMDBAPI,
		URL: fmt.Sprintf("/list/%s", listID),
		Params: napping.Params{
			"api_key": apiKey,
		}.AsUrlValues(),
		Result:      &results,
		Description: "IMDB list",

		Cache:       true,
		CacheExpire: cache.CacheExpireLong,
	}

	if err := req.Do(); err != nil || results == nil {
		return
	}

	tmdbIds := make([]int, 0)
	for i := requestLimitStart; i <= requestLimitEnd; i++ {
		if i >= len(results.Items) || results.Items[i] == nil {
			continue
		}

		tmdbIds = append(tmdbIds, results.Items[i].ID)
	}
	movies = GetMovies(tmdbIds, language)
	totalResults = results.ItemCount
	return
}

func listMovies(endpoint string, params napping.Params, page int) (Movies, int) {
	defer perf.ScopeTimer()()

	params["api_key"] = apiKey
	totalResults := -1

	requestPerPage := config.Get().ResultsPerPage
	requestLimitStart := (page - 1) * requestPerPage
	requestLimitEnd := page*requestPerPage - 1

	pageStart := requestLimitStart / TMDBResultsPerPage
	pageEnd := requestLimitEnd / TMDBResultsPerPage

	movies := make(Movies, requestPerPage)

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
				Description: "list movies",

				Cache:       true,
				CacheExpire: cache.CacheExpireShort,
			}

			if err := req.Do(); err != nil || results == nil {
				return
			}

			if totalResults == -1 {
				totalResults = results.TotalResults
			}

			var wgItems sync.WaitGroup
			wgItems.Add(len(results.Results))
			for m, movie := range results.Results {
				rindex := currentPage*TMDBResultsPerPage - requestLimitStart + m
				if movie == nil || rindex >= len(movies) || rindex < 0 {
					wgItems.Done()
					continue
				}

				go func(rindex int, tmdbId int) {
					defer wgItems.Done()
					movies[rindex] = GetMovie(tmdbId, params["language"])
				}(rindex, movie.ID)
			}
			wgItems.Wait()
		}(p)
	}
	wg.Wait()

	return movies, totalResults
}

// PopularMovies ...
func PopularMovies(params DiscoverFilters, language string, page int) (Movies, int) {
	var p napping.Params
	if params.Genre != "" {
		p = napping.Params{
			"language":                 language,
			"sort_by":                  "popularity.desc",
			"primary_release_date.lte": time.Now().UTC().Format(time.DateOnly),
			"with_genres":              params.Genre,
		}
	} else if params.Country != "" {
		p = napping.Params{
			"language":                 language,
			"sort_by":                  "popularity.desc",
			"primary_release_date.lte": time.Now().UTC().Format(time.DateOnly),
			"with_origin_country":      params.Country,
		}
	} else if params.Language != "" {
		p = napping.Params{
			"language":                 language,
			"sort_by":                  "popularity.desc",
			"primary_release_date.lte": time.Now().UTC().Format(time.DateOnly),
			"with_original_language":   params.Language,
		}
	} else {
		p = napping.Params{
			"language":                 language,
			"sort_by":                  "popularity.desc",
			"primary_release_date.lte": time.Now().UTC().Format(time.DateOnly),
		}
	}

	return listMovies("discover/movie", p, page)
}

// RecentMovies ...
func RecentMovies(params DiscoverFilters, language string, page int) (Movies, int) {
	var p napping.Params
	if params.Genre != "" {
		p = napping.Params{
			"language":                 language,
			"sort_by":                  "primary_release_date.desc",
			"vote_count.gte":           "10",
			"primary_release_date.lte": time.Now().UTC().Format(time.DateOnly),
			"with_genres":              params.Genre,
		}
	} else if params.Country != "" {
		p = napping.Params{
			"language":                 language,
			"sort_by":                  "primary_release_date.desc",
			"vote_count.gte":           "10",
			"primary_release_date.lte": time.Now().UTC().Format(time.DateOnly),
			"region":                   params.Country,
		}
	} else if params.Language != "" {
		p = napping.Params{
			"language":                 language,
			"sort_by":                  "primary_release_date.desc",
			"vote_count.gte":           "10",
			"primary_release_date.lte": time.Now().UTC().Format(time.DateOnly),
			"with_original_language":   params.Language,
		}
	} else {
		p = napping.Params{
			"language":                 language,
			"sort_by":                  "primary_release_date.desc",
			"vote_count.gte":           "10",
			"primary_release_date.lte": time.Now().UTC().Format(time.DateOnly),
		}
	}

	return listMovies("discover/movie", p, page)
}

// TopRatedMovies ...
func TopRatedMovies(genre string, language string, page int) (Movies, int) {
	return listMovies("movie/top_rated", napping.Params{"language": language}, page)
}

// MostVotedMovies ...
func MostVotedMovies(genre string, language string, page int) (Movies, int) {
	var p napping.Params
	if genre == "" {
		p = napping.Params{
			"language":                 language,
			"sort_by":                  "vote_count.desc",
			"primary_release_date.lte": time.Now().UTC().Format(time.DateOnly),
		}
	} else {
		p = napping.Params{
			"language":                 language,
			"sort_by":                  "vote_count.desc",
			"primary_release_date.lte": time.Now().UTC().Format(time.DateOnly),
			"with_genres":              genre,
		}
	}
	return listMovies("discover/movie", p, page)
}

// Year returns year of the movie
func (movie *Movie) Year() int {
	year, _ := strconv.Atoi(strings.Split(movie.ReleaseDate, "-")[0])
	return year
}

// SetArt sets artworks for movie
func (movie *Movie) SetArt(item *xbmc.ListItem) {
	if item.Art == nil {
		item.Art = &xbmc.ListItemArt{}
	}

	imageQualities := GetImageQualities()

	item.Art.FanArt = ImageURL(movie.BackdropPath, imageQualities.FanArt)
	item.Art.Thumbnail = ImageURL(movie.BackdropPath, imageQualities.Thumbnail)
	item.Art.Poster = ImageURL(movie.PosterPath, imageQualities.Poster)

	if item.Art.AvailableArtworks == nil {
		item.Art.AvailableArtworks = &xbmc.Artworks{}
	}

	SetLocalizedArt(&movie.Entity, item)

	if config.Get().UseFanartTv {
		if movie.FanArt == nil {
			movie.FanArt = fanart.GetMovie(movie.ID)
		}
		if movie.FanArt != nil {
			item.Art = movie.FanArt.ToListItemArt(item.Art)
		}
	}

	item.Thumbnail = item.Art.Thumbnail
}

// ToListItem ...
func (movie *Movie) ToListItem() *xbmc.ListItem {
	defer perf.ScopeTimer()()

	title := movie.GetTitle()

	item := &xbmc.ListItem{
		Label:  title,
		Label2: fmt.Sprintf("%f", movie.VoteAverage),
		Info: &xbmc.ListItemInfo{
			Year:          movie.Year(),
			Date:          movie.ReleaseDate,
			Count:         rand.Int(),
			Title:         title,
			OriginalTitle: movie.OriginalTitle,
			Plot:          movie.overview(),
			PlotOutline:   movie.overview(),
			TagLine:       movie.tagline(),
			Duration:      movie.Runtime * 60,
			Code:          movie.IMDBId,
			IMDBNumber:    movie.IMDBId,
			Votes:         strconv.Itoa(movie.VoteCount),
			Rating:        movie.VoteAverage,
			PlayCount:     playcount.GetWatchedMovieByTMDB(movie.ID).Int(),
			MPAA:          movie.mpaa(),
			DBTYPE:        "movie",
			Mediatype:     "movie",
			Genre:         movie.GetGenres(),
			Studio:        movie.GetStudios(),
			Country:       movie.GetCountries(),
		},
		Properties: &xbmc.ListItemProperties{},
		UniqueIDs: &xbmc.UniqueIDs{
			TMDB: strconv.Itoa(movie.ID),
		},
	}

	if lm, err := uid.GetMovieByTMDB(movie.ID); lm != nil && err == nil {
		item.Info.DBID = lm.UIDs.Kodi
		if lm.Resume != nil {
			item.Properties.ResumeTime = strconv.FormatFloat(lm.Resume.Position, 'f', 6, 64)
			item.Properties.TotalTime = strconv.FormatFloat(lm.Resume.Total, 'f', 6, 64)
		}
	}

	movie.SetArt(item)

	SetTrailer(&movie.Entity, item)

	for _, language := range movie.SpokenLanguages {
		item.StreamInfo = &xbmc.StreamInfo{
			Audio: &xbmc.StreamInfoEntry{
				Language: language.Iso639_1,
			},
		}
		break
	}

	if movie.Credits != nil {
		item.CastMembers = movie.Credits.GetCastMembers()
		item.Info.Director = movie.Credits.GetDirectors()
		item.Info.Writer = movie.Credits.GetWriters()
	}

	return item
}

func (movie *Movie) mpaa() string {
	if movie.ReleaseDates == nil || movie.ReleaseDates.Results == nil || len(movie.ReleaseDates.Results) == 0 {
		return ""
	}

	region := config.Get().Region
	for _, r := range movie.ReleaseDates.Results {
		if r.ReleaseDates == nil || len(r.ReleaseDates) == 0 || strings.ToUpper(r.Iso3166_1) != region {
			continue
		}

		for _, rd := range r.ReleaseDates {
			if rd.Certification != "" {
				return rd.Certification
			}
		}
	}

	return ""
}

func (movie *Movie) GetSearchTitle() string {
	if year := movie.Year(); year > 0 {
		return fmt.Sprintf("%s (%d)", movie.GetTitle(), year)
	}
	return movie.GetTitle()
}

func (movie *Movie) GetTitle() string {
	// If user's language is equal to video's language - we just use Title
	if movie.Title != "" && movie.Title == movie.OriginalTitle && movie.OriginalLanguage == config.Get().Language {
		return movie.Title
	}

	// If we have a title, but we don't have translations - use it
	if (movie.Title != "" && movie.Title != movie.OriginalTitle) || movie.Translations == nil || movie.Translations.Translations == nil || len(movie.Translations.Translations) == 0 {
		return movie.Title
	}

	// Find translations in this order: Kodi language -> Second language -> English -> Original language

	current := movie.findTranslation(config.Get().Language)
	if current != nil && current.Data != nil && current.Data.Title != "" {
		return current.Data.Title
	}

	current = movie.findTranslation(config.Get().SecondLanguage)
	if current != nil && current.Data != nil && current.Data.Title != "" {
		return current.Data.Title
	}

	if config.Get().Language != "en" && config.Get().SecondLanguage != "en" {
		current = movie.findTranslation("en")
		if current != nil && current.Data != nil && current.Data.Title != "" {
			return current.Data.Title
		}
	}

	current = movie.findTranslation(movie.OriginalLanguage)
	if current != nil && current.Data != nil && current.Data.Title != "" {
		return current.Data.Title
	}

	return movie.Title
}

func (movie *Movie) overview() string {
	if movie.Overview != "" || movie.Translations == nil || movie.Translations.Translations == nil || len(movie.Translations.Translations) == 0 {
		return movie.Overview
	}

	current := movie.findTranslation(config.Get().Language)
	if current != nil && current.Data != nil && current.Data.Overview != "" {
		return current.Data.Overview
	}

	current = movie.findTranslation(config.Get().SecondLanguage)
	if current != nil && current.Data != nil && current.Data.Overview != "" {
		return current.Data.Overview
	}

	if config.Get().Language != "en" && config.Get().SecondLanguage != "en" {
		current = movie.findTranslation("en")
		if current != nil && current.Data != nil && current.Data.Overview != "" {
			return current.Data.Overview
		}
	}

	current = movie.findTranslation(movie.OriginalLanguage)
	if current != nil && current.Data != nil && current.Data.Overview != "" {
		return current.Data.Overview
	}

	return movie.Overview
}

func (movie *Movie) tagline() string {
	if movie.TagLine != "" || movie.Translations == nil || movie.Translations.Translations == nil || len(movie.Translations.Translations) == 0 {
		return movie.TagLine
	}

	current := movie.findTranslation(config.Get().Language)
	if current != nil && current.Data != nil && current.Data.TagLine != "" {
		return current.Data.TagLine
	}

	current = movie.findTranslation(config.Get().SecondLanguage)
	if current != nil && current.Data != nil && current.Data.TagLine != "" {
		return current.Data.TagLine
	}

	if config.Get().Language != "en" && config.Get().SecondLanguage != "en" {
		current = movie.findTranslation("en")
		if current != nil && current.Data != nil && current.Data.TagLine != "" {
			return current.Data.TagLine
		}
	}

	current = movie.findTranslation(movie.OriginalLanguage)
	if current != nil && current.Data != nil && current.Data.TagLine != "" {
		return current.Data.TagLine
	}

	return movie.TagLine
}

func (movie *Movie) findTranslation(language string) *Translation {
	if language == "" || movie.Translations == nil || movie.Translations.Translations == nil || len(movie.Translations.Translations) == 0 {
		return nil
	}

	language = strings.ToLower(language)
	for _, tr := range movie.Translations.Translations {
		if strings.ToLower(tr.Iso639_1) == language {
			return tr
		}
	}

	return nil
}

// GetCountries returns list of countries
func (movie *Movie) GetCountries() []string {
	countries := make([]string, 0, len(movie.ProductionCountries))
	for _, country := range movie.ProductionCountries {
		countries = append(countries, country.Name)
	}

	return countries
}

// GetStudios returns list of studios
func (movie *Movie) GetStudios() []string {
	studios := make([]string, 0, len(movie.ProductionCompanies))
	for _, company := range movie.ProductionCompanies {
		studios = append(studios, company.Name)
	}

	return studios
}

// GetGenres returns list of genres
func (movie *Movie) GetGenres() []string {
	genres := make([]string, 0, len(movie.Genres))
	for _, genre := range movie.Genres {
		genres = append(genres, genre.Name)
	}

	return genres
}

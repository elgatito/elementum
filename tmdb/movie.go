package tmdb

import (
	"fmt"
	"math/rand"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/elgatito/elementum/cache"
	"github.com/elgatito/elementum/config"
	"github.com/elgatito/elementum/fanart"
	"github.com/elgatito/elementum/library/playcount"
	"github.com/elgatito/elementum/library/uid"
	"github.com/elgatito/elementum/util"
	"github.com/elgatito/elementum/xbmc"

	"github.com/jmcvetta/napping"
)

// ByPopularity ...
type ByPopularity Movies

func (a ByPopularity) Len() int           { return len(a) }
func (a ByPopularity) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }
func (a ByPopularity) Less(i, j int) bool { return a[i].Popularity < a[j].Popularity }

// GetImages ...
func GetImages(movieID int) *Images {
	var images *Images
	cacheStore := cache.NewDBStore()
	key := fmt.Sprintf(cache.TMDBMovieImagesKey, movieID)
	if err := cacheStore.Get(key, &images); err != nil {
		err = MakeRequest(APIRequest{
			URL: fmt.Sprintf("%s/movie/%d/images", tmdbEndpoint, movieID),
			Params: napping.Params{
				"api_key":                apiKey,
				"include_image_language": fmt.Sprintf("%s,en,null", config.Get().Language),
			}.AsUrlValues(),
			Result:      &images,
			Description: "movie images",
		})

		if images != nil {
			cacheStore.Set(key, images, cache.TMDBMovieImagesExpire)
		}
	}
	return images
}

// GetMovie ...
func GetMovie(tmdbID int, language string) *Movie {
	return GetMovieByID(strconv.Itoa(tmdbID), language)
}

// GetMovieByID ...
func GetMovieByID(movieID string, language string) *Movie {
	var movie *Movie
	cacheStore := cache.NewDBStore()
	key := fmt.Sprintf(cache.TMDBMovieByIDKey, movieID, language)
	if err := cacheStore.Get(key, &movie); err != nil {
		err = MakeRequest(APIRequest{
			URL: fmt.Sprintf("%s/movie/%s", tmdbEndpoint, movieID),
			Params: napping.Params{
				"api_key":                apiKey,
				"append_to_response":     "credits,images,alternative_titles,translations,external_ids,trailers,release_dates",
				"include_image_language": fmt.Sprintf("%s,en,null", config.Get().Language),
				"language":               language,
			}.AsUrlValues(),
			Result:      &movie,
			Description: "movie",
		})

		if movie != nil {
			if config.Get().UseFanartTv {
				movie.FanArt = fanart.GetMovie(movie.ID)
			}

			cacheStore.Set(key, movie, cache.TMDBMovieByIDExpire)
		}
	}
	if movie == nil {
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
	genres := GenreList{}

	cacheStore := cache.NewDBStore()
	key := fmt.Sprintf(cache.TMDBMovieGenresKey, language)
	if err := cacheStore.Get(key, &genres); err != nil || true {
		err = MakeRequest(APIRequest{
			URL: fmt.Sprintf("%s/genre/movie/list", tmdbEndpoint),
			Params: napping.Params{
				"api_key":  apiKey,
				"language": language,
			}.AsUrlValues(),
			Result:      &genres,
			Description: "movie genres",
		})

		// That is a special case, when language in on TMDB, but it results empty names.
		//   example of this: Catalan language.
		if genres.Genres != nil && len(genres.Genres) > 0 && genres.Genres[0].Name == "" {
			err = MakeRequest(APIRequest{
				URL: fmt.Sprintf("%s/genre/movie/list", tmdbEndpoint),
				Params: napping.Params{
					"api_key":  apiKey,
					"language": "en-US",
				}.AsUrlValues(),
				Result:      &genres,
				Description: "movie genres",
			})
		}

		if genres.Genres != nil && len(genres.Genres) > 0 {
			for _, i := range genres.Genres {
				i.Name = strings.Title(i.Name)
			}

			sort.Slice(genres.Genres, func(i, j int) bool {
				return genres.Genres[i].Name < genres.Genres[j].Name
			})

			cacheStore.Set(key, genres, cache.TMDBMovieGenresExpire)
		}
	}
	return genres.Genres
}

// SearchMovies ...
func SearchMovies(query string, language string, page int) (Movies, int) {
	var results EntityList

	MakeRequest(APIRequest{
		URL: fmt.Sprintf("%s/search/movie", tmdbEndpoint),
		Params: napping.Params{
			"api_key": apiKey,
			"query":   query,
			"page":    strconv.Itoa(page),
		}.AsUrlValues(),
		Result:      &results,
		Description: "search movie",
	})

	if results.Results != nil && len(results.Results) == 0 {
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
	var results *List
	totalResults = -1

	requestPerPage := config.Get().ResultsPerPage
	requestLimitStart := (page - 1) * requestPerPage
	requestLimitEnd := page*requestPerPage - 1

	cacheStore := cache.NewDBStore()
	key := fmt.Sprintf(cache.TMDBMoviesIMDBKey, listID, requestPerPage, page)
	totalKey := fmt.Sprintf(cache.TMDBMoviesIMDBTotalKey, listID)
	if err := cacheStore.Get(key, &movies); err != nil {
		err = MakeRequest(APIRequest{
			URL: fmt.Sprintf("%s/list/%s", tmdbEndpoint, listID),
			Params: napping.Params{
				"api_key": apiKey,
			}.AsUrlValues(),
			Result:      &results,
			Description: "IMDB list",
		})

		if err != nil || results == nil {
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
		if movies != nil && len(movies) > 0 {
			cacheStore.Set(key, movies, cache.TMDBMoviesIMDBExpire)
		}
		totalResults = results.ItemCount
		cacheStore.Set(totalKey, totalResults, cache.TMDBMoviesIMDBTotalExpire)
	} else {
		if err := cacheStore.Get(totalKey, &totalResults); err != nil {
			totalResults = -1
		}
	}
	return
}

func listMovies(endpoint string, cacheKey string, params napping.Params, page int) (Movies, int) {
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

	movies := make(Movies, requestPerPage)

	cacheStore := cache.NewDBStore()
	key := fmt.Sprintf(cache.TMDBMoviesTopMoviesKey, cacheKey, genre, country, language, requestPerPage, page)
	totalKey := fmt.Sprintf(cache.TMDBMoviesTopMoviesTotalKey, cacheKey, genre, country, language)
	if err := cacheStore.Get(key, &movies); err != nil {
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
					Description: "list movies",
				})

				if results == nil {
					return
				}

				if totalResults == -1 {
					totalResults = results.TotalResults
					cacheStore.Set(totalKey, totalResults, cache.TMDBMoviesTopMoviesTotalExpire)
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
		cacheStore.Set(key, movies, cache.TMDBMoviesTopMoviesExpire)
	} else {
		if err := cacheStore.Get(totalKey, &totalResults); err != nil {
			totalResults = -1
		}
	}

	return movies, totalResults
}

// PopularMovies ...
func PopularMovies(params DiscoverFilters, language string, page int) (Movies, int) {
	var p napping.Params
	if params.Genre != "" {
		p = napping.Params{
			"language":                 language,
			"sort_by":                  "popularity.desc",
			"primary_release_date.lte": time.Now().UTC().Format("2006-01-02"),
			"with_genres":              params.Genre,
		}
	} else if params.Country != "" {
		p = napping.Params{
			"language":                 language,
			"sort_by":                  "popularity.desc",
			"primary_release_date.lte": time.Now().UTC().Format("2006-01-02"),
			"region":                   params.Country,
		}
	} else if params.Language != "" {
		p = napping.Params{
			"language":                 language,
			"sort_by":                  "popularity.desc",
			"primary_release_date.lte": time.Now().UTC().Format("2006-01-02"),
			"with_original_language":   params.Language,
		}
	} else {
		p = napping.Params{
			"language":                 language,
			"sort_by":                  "popularity.desc",
			"primary_release_date.lte": time.Now().UTC().Format("2006-01-02"),
		}
	}

	return listMovies("discover/movie", "popular", p, page)
}

// RecentMovies ...
func RecentMovies(params DiscoverFilters, language string, page int) (Movies, int) {
	var p napping.Params
	if params.Genre != "" {
		p = napping.Params{
			"language":                 language,
			"sort_by":                  "primary_release_date.desc",
			"vote_count.gte":           "10",
			"primary_release_date.lte": time.Now().UTC().Format("2006-01-02"),
			"with_genres":              params.Genre,
		}
	} else if params.Country != "" {
		p = napping.Params{
			"language":                 language,
			"sort_by":                  "primary_release_date.desc",
			"vote_count.gte":           "10",
			"primary_release_date.lte": time.Now().UTC().Format("2006-01-02"),
			"region":                   params.Country,
		}
	} else if params.Language != "" {
		p = napping.Params{
			"language":                 language,
			"sort_by":                  "primary_release_date.desc",
			"vote_count.gte":           "10",
			"primary_release_date.lte": time.Now().UTC().Format("2006-01-02"),
			"with_original_language":   params.Language,
		}
	} else {
		p = napping.Params{
			"language":                 language,
			"sort_by":                  "primary_release_date.desc",
			"vote_count.gte":           "10",
			"primary_release_date.lte": time.Now().UTC().Format("2006-01-02"),
		}
	}

	return listMovies("discover/movie", "recent", p, page)
}

// TopRatedMovies ...
func TopRatedMovies(genre string, language string, page int) (Movies, int) {
	return listMovies("movie/top_rated", "toprated", napping.Params{"language": language}, page)
}

// MostVotedMovies ...
func MostVotedMovies(genre string, language string, page int) (Movies, int) {
	var p napping.Params
	if genre == "" {
		p = napping.Params{
			"language":                 language,
			"sort_by":                  "vote_count.desc",
			"primary_release_date.lte": time.Now().UTC().Format("2006-01-02"),
		}
	} else {
		p = napping.Params{
			"language":                 language,
			"sort_by":                  "vote_count.desc",
			"primary_release_date.lte": time.Now().UTC().Format("2006-01-02"),
			"with_genres":              genre,
		}
	}
	return listMovies("discover/movie", "mostvoted", p, page)
}

// Year returns year of the movie
func (movie *Movie) Year() int {
	year, _ := strconv.Atoi(strings.Split(movie.ReleaseDate, "-")[0])
	return year
}

// ToListItem ...
func (movie *Movie) ToListItem() *xbmc.ListItem {
	title := movie.title()
	if config.Get().UseOriginalTitle && movie.OriginalTitle != "" {
		title = movie.OriginalTitle
	}

	item := &xbmc.ListItem{
		Label:  title,
		Label2: fmt.Sprintf("%f", movie.VoteAverage),
		Info: &xbmc.ListItemInfo{
			Year:          movie.Year(),
			Count:         rand.Int(),
			Title:         title,
			OriginalTitle: movie.OriginalTitle,
			Plot:          movie.overview(),
			PlotOutline:   movie.overview(),
			TagLine:       movie.TagLine,
			Duration:      movie.Runtime * 60,
			Code:          movie.IMDBId,
			IMDBNumber:    movie.IMDBId,
			Date:          movie.ReleaseDate,
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
		Art: &xbmc.ListItemArt{
			FanArt:    ImageURL(movie.BackdropPath, "w1280"),
			Poster:    ImageURL(movie.PosterPath, "w1280"),
			Thumbnail: ImageURL(movie.PosterPath, "w300"),
		},
		UniqueIDs: &xbmc.UniqueIDs{
			TMDB: strconv.Itoa(movie.ID),
		},
	}

	if lm, err := uid.GetMovieByTMDB(movie.ID); lm != nil && err == nil {
		item.Info.DBID = lm.UIDs.Kodi
	} else {
		fakeDBID := util.GetMovieFakeDBID(movie.ID)
		if fakeDBID > 0 {
			item.Info.DBID = fakeDBID
		}
	}

	if movie.Images != nil && movie.Images.Backdrops != nil {
		fanarts := make([]string, 0)
		for _, backdrop := range movie.Images.Backdrops {
			fanarts = append(fanarts, ImageURL(backdrop.FilePath, "w1280"))
		}
		if len(fanarts) > 0 {
			item.Art.FanArt = fanarts[rand.Intn(len(fanarts))]
			item.Art.FanArts = fanarts
		}
	}

	if movie.Images != nil && movie.Images.Posters != nil {
		posters := make([]string, 0)
		for _, poster := range movie.Images.Posters {
			posters = append(posters, ImageURL(poster.FilePath, "w1280"))
		}
		if len(posters) > 0 {
			if item.Art.AvailableArtworks == nil {
				item.Art.AvailableArtworks = &xbmc.Artworks{Poster: posters}
			} else {
				item.Art.AvailableArtworks.Poster = posters
			}
		}
	}

	if config.Get().UseFanartTv && movie.FanArt != nil {
		item.Art = movie.FanArt.ToListItemArt(item.Art)
	}

	item.Thumbnail = item.Art.Poster

	if movie.Trailers != nil {
		for _, trailer := range movie.Trailers.Youtube {
			item.Info.Trailer = util.TrailerURL(trailer.Source)
			break
		}
	}

	if item.Info.Trailer == "" && config.Get().Language != "en" {
		enMovie := GetMovie(movie.ID, "en")
		if enMovie != nil && enMovie.Trailers != nil {
			for _, trailer := range enMovie.Trailers.Youtube {
				item.Info.Trailer = util.TrailerURL(trailer.Source)
				break
			}
		}
	}

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

func (movie *Movie) title() string {
	if movie.Title != "" || movie.Translations == nil || movie.Translations.Translations == nil || len(movie.Translations.Translations) == 0 {
		return movie.Title
	}

	current := movie.findTranslation(config.Get().Language)
	if current != nil && current.Data != nil && current.Data.Title != "" {
		return current.Data.Title
	}

	current = movie.findTranslation("en")
	if current != nil && current.Data != nil && current.Data.Title != "" {
		return current.Data.Title
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

	current = movie.findTranslation("en")
	if current != nil && current.Data != nil && current.Data.Overview != "" {
		return current.Data.Overview
	}

	current = movie.findTranslation(movie.OriginalLanguage)
	if current != nil && current.Data != nil && current.Data.Overview != "" {
		return current.Data.Overview
	}

	return movie.Overview
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

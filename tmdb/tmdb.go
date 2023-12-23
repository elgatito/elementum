package tmdb

import (
	"fmt"
	"math/rand"
	"net/http"
	"net/url"
	"sort"
	"time"

	"github.com/elgatito/elementum/cache"
	"github.com/elgatito/elementum/config"
	"github.com/elgatito/elementum/fanart"
	"github.com/elgatito/elementum/util"
	"github.com/elgatito/elementum/util/event"
	"github.com/elgatito/elementum/xbmc"

	"github.com/anacrolix/missinggo/perf"
	"github.com/jmcvetta/napping"
	"github.com/op/go-logging"
)

//go:generate msgp -o msgp.go -io=false -tests=false

const (
	// TMDBResultsPerPage reflects TMDB number of results on the page. It's statically set to 20, so we should work with that
	TMDBResultsPerPage = 20
)

var (
	log = logging.MustGetLogger("tmdb")
)

// Movies ...
type Movies []*Movie

// Shows ...
type Shows []*Show

// SeasonList ...
type SeasonList []*Season

// EpisodeList ...
type EpisodeList []*Episode

// Movie ...
type Movie struct {
	Entity

	FanArt              *fanart.Movie `json:"fanart"`
	IMDBId              string        `json:"imdb_id"`
	Overview            string        `json:"overview"`
	ProductionCompanies []*IDNameLogo `json:"production_companies"`
	ProductionCountries []*Country    `json:"production_countries"`
	Runtime             int           `json:"runtime"`
	TagLine             string        `json:"tagline"`
	RawPopularity       interface{}   `json:"popularity"`
	Popularity          float64       `json:"-"`
	SpokenLanguages     []*Language   `json:"spoken_languages"`
	ExternalIDs         *ExternalIDs  `json:"external_ids"`

	AlternativeTitles *struct {
		Titles []*AlternativeTitle `json:"titles"`
	} `json:"alternative_titles"`

	Translations *struct {
		Translations []*Translation `json:"translations"`
	} `json:"translations"`

	Trailers *struct {
		Youtube []*Trailer `json:"youtube"`
	} `json:"trailers"`

	Credits *Credits `json:"credits,omitempty"`
	Images  *Images  `json:"images,omitempty"`

	ReleaseDates *ReleaseDatesResults `json:"release_dates"`
}

// Show ...
type Show struct {
	Entity

	FanArt              *fanart.Show  `json:"fanart"`
	EpisodeRunTime      []int         `json:"episode_run_time"`
	Homepage            string        `json:"homepage"`
	InProduction        bool          `json:"in_production"`
	LastAirDate         string        `json:"last_air_date"`
	Networks            []*IDNameLogo `json:"networks"`
	NumberOfEpisodes    int           `json:"number_of_episodes"`
	NumberOfSeasons     int           `json:"number_of_seasons"`
	OriginCountry       []string      `json:"origin_country"`
	Overview            string        `json:"overview"`
	RawPopularity       interface{}   `json:"popularity"`
	Popularity          float64       `json:"-"`
	ProductionCompanies []*IDNameLogo `json:"production_companies"`
	ProductionCountries []*Country    `json:"production_countries"`
	Status              string        `json:"status"`
	ExternalIDs         *ExternalIDs  `json:"external_ids"`

	Translations *struct {
		Translations []*Translation `json:"translations"`
	} `json:"translations"`
	AlternativeTitles *struct {
		Titles []*AlternativeTitle `json:"results"`
	} `json:"alternative_titles"`
	ContentRatings *struct {
		Ratings []*ContentRating `json:"results"`
	} `json:"content_ratings"`

	Credits *Credits `json:"credits,omitempty"`
	Images  *Images  `json:"images,omitempty"`

	Seasons SeasonList `json:"seasons"`
}

// Season ...
type Season struct {
	ID           int          `json:"id"`
	Name         string       `json:"name,omitempty"`
	Overview     string       `json:"overview"`
	Season       int          `json:"season_number"`
	EpisodeCount int          `json:"episode_count,omitempty"`
	AirDate      string       `json:"air_date"`
	Poster       string       `json:"poster_path"`
	Backdrop     string       `json:"backdrop_path"`
	ExternalIDs  *ExternalIDs `json:"external_ids"`

	AlternativeTitles *struct {
		Titles []*AlternativeTitle `json:"titles"`
	} `json:"alternative_titles"`

	Translations *struct {
		Translations []*Translation `json:"translations"`
	} `json:"translations"`

	Trailers *struct {
		Youtube []*Trailer `json:"youtube"`
	} `json:"trailers"`

	Credits *Credits `json:"credits,omitempty"`
	Images  *Images  `json:"images,omitempty"`

	Episodes EpisodeList `json:"episodes"`
}

// Episode ...
type Episode struct {
	ID            int          `json:"id"`
	Name          string       `json:"name"`
	Overview      string       `json:"overview"`
	AirDate       string       `json:"air_date"`
	SeasonNumber  int          `json:"season_number"`
	EpisodeNumber int          `json:"episode_number"`
	VoteAverage   float32      `json:"vote_average"`
	VoteCount     int          `json:"vote_count"`
	StillPath     string       `json:"still_path"`
	ExternalIDs   *ExternalIDs `json:"external_ids"`

	AlternativeTitles *struct {
		Titles []*AlternativeTitle `json:"titles"`
	} `json:"alternative_titles"`

	Translations *struct {
		Translations []*Translation `json:"translations"`
	} `json:"translations"`

	Trailers *struct {
		Youtube []*Trailer `json:"youtube"`
	} `json:"trailers"`

	Credits *Credits `json:"credits,omitempty"`
	Images  *Images  `json:"images,omitempty"`
}

// Entity ...
type Entity struct {
	IsAdult          bool      `json:"adult"`
	BackdropPath     string    `json:"backdrop_path"`
	ID               int       `json:"id"`
	Genres           []*IDName `json:"genres"`
	OriginalTitle    string    `json:"original_title,omitempty"`
	OriginalLanguage string    `json:"original_language,omitempty"`
	ReleaseDate      string    `json:"release_date"`
	FirstAirDate     string    `json:"first_air_date"`
	PosterPath       string    `json:"poster_path"`
	Title            string    `json:"title,omitempty"`
	VoteAverage      float32   `json:"vote_average"`
	VoteCount        int       `json:"vote_count"`
	OriginalName     string    `json:"original_name,omitempty"`
	Name             string    `json:"name,omitempty"`
}

// EntityList ...
type EntityList struct {
	Page         int       `json:"page"`
	Results      []*Entity `json:"results"`
	TotalPages   int       `json:"total_pages"`
	TotalResults int       `json:"total_results"`
}

// IDName ...
type IDName struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

// IDNameLogo ...
type IDNameLogo struct {
	ID            int    `json:"id"`
	Name          string `json:"name"`
	Logo          string `json:"logo_path"`
	OriginCountry string `json:"origin_country"`
}

// Genre ...
type Genre IDName

// GenreList ...
type GenreList struct {
	Genres []*Genre `json:"genres"`
}

// Country ...
type Country struct {
	Iso31661    string `json:"iso_3166_1"`
	Name        string `json:"name"`
	EnglishName string `json:"english_name"`
}

// CountryList ...
type CountryList []*Country

// LanguageList ...
type LanguageList struct {
	Languages []*Language `json:"languages"`
}

// Image ...
type Image struct {
	FilePath string `json:"file_path"`
	Height   int    `json:"height"`
	Iso639_1 string `json:"iso_639_1"`
	Width    int    `json:"width"`
}

// Images ...
type Images struct {
	Backdrops []*Image `json:"backdrops"`
	Posters   []*Image `json:"posters"`
	Stills    []*Image `json:"stills"`
}

// Cast ...
type Cast struct {
	IDName
	CastID      int    `json:"cast_id"`
	Character   string `json:"character"`
	CreditID    string `json:"credit_id"`
	Order       int    `json:"order"`
	ProfilePath string `json:"profile_path"`
}

// Crew ...
type Crew struct {
	IDName
	CreditID    string `json:"credit_id"`
	Department  string `json:"department"`
	Job         string `json:"job"`
	ProfilePath string `json:"profile_path"`
}

// Credits ...
type Credits struct {
	Cast []*Cast `json:"cast"`
	Crew []*Crew `json:"crew"`
}

// ExternalIDs ...
type ExternalIDs struct {
	IMDBId      string      `json:"imdb_id"`
	FreeBaseID  string      `json:"freebase_id"`
	FreeBaseMID string      `json:"freebase_mid"`
	TVDBID      interface{} `json:"tvdb_id"`
}

// ContentRating ...
type ContentRating struct {
	Iso3166_1 string `json:"iso_3166_1"`
	Rating    string `json:"rating"`
}

// AlternativeTitle ...
type AlternativeTitle struct {
	Iso3166_1 string `json:"iso_3166_1"`
	Title     string `json:"title"`
}

// Language ...
type Language struct {
	Iso639_1    string `json:"iso_639_1"`
	Name        string `json:"name"`
	EnglishName string `json:"english_name,omitempty"`
}

// Translation ...
type Translation struct {
	Iso3166_1   string           `json:"iso_3166_1"`
	Iso639_1    string           `json:"iso_639_1"`
	Name        string           `json:"name"`
	EnglishName string           `json:"english_name"`
	Data        *TranslationData `json:"data"`
}

// TranslationData ...
type TranslationData struct {
	Name     string `json:"name"`
	Title    string `json:"title"`
	Overview string `json:"overview"`
	Homepage string `json:"homepage"`
}

// FindResult ...
type FindResult struct {
	MovieResults     []*Entity `json:"movie_results"`
	PersonResults    []*Entity `json:"person_results"`
	TVResults        []*Entity `json:"tv_results"`
	TVEpisodeResults []*Entity `json:"tv_episode_results"`
	TVSeasonResults  []*Entity `json:"tv_season_results"`
}

// List ...
type List struct {
	CreatedBy     string    `json:"created_by"`
	Description   string    `json:"description"`
	FavoriteCount int       `json:"favorite_count"`
	ID            string    `json:"id"`
	ItemCount     int       `json:"item_count"`
	Iso639_1      string    `json:"iso_639_1"`
	Name          string    `json:"name"`
	PosterPath    string    `json:"poster_path"`
	Items         []*Entity `json:"items"`
}

// Trailer ...
type Trailer struct {
	Name   string `json:"name"`
	Size   string `json:"size"`
	Source string `json:"source"`
	Type   string `json:"type"`
}

// ReleaseDatesResults ...
type ReleaseDatesResults struct {
	Results []*ReleaseDates `json:"results"`
}

// ReleaseDates ...
type ReleaseDates struct {
	Iso3166_1    string         `json:"iso_3166_1"`
	ReleaseDates []*ReleaseDate `json:"release_dates"`
}

// ReleaseDate ...
type ReleaseDate struct {
	Certification string `json:"certification"`
	Iso639_1      string `json:"iso_639_1"`
	Note          string `json:"note"`
	ReleaseDate   string `json:"release_date"`
	Type          int    `json:"type"`
}

// DiscoverFilters ...
type DiscoverFilters struct {
	Genre    string
	Country  string
	Language string
}

// APIRequest ...
type APIRequest struct {
	URL         string
	Params      url.Values `msg:"-"`
	Result      interface{}
	ErrMsg      interface{}
	Description string
}

const (
	tmdbEndpoint  = "https://api.themoviedb.org/3"
	imageEndpoint = "https://image.tmdb.org/t/p/"
	burstRate     = 150
	burstTime     = 5 * time.Second
	// Currently TMDB is disabled rates limiting
	// burstRate               = 40
	// burstTime               = 10 * time.Second
	simultaneousConnections = 30
)

var (
	apiKeys = []string{
		"8cf43ad9c085135b9479ad5cf6bbcbda",
		"ae4bd1b6fce2a5648671bfc171d15ba4",
		"29a551a65eef108dd01b46e27eb0554a",
	}
	apiKey = apiKeys[rand.Intn(len(apiKeys))]
	// WarmingUp ...
	WarmingUp = event.Event{}
)

var rl = util.NewRateLimiter(burstRate, burstTime, simultaneousConnections)

// CheckAPIKey ...
func CheckAPIKey() {
	log.Info("Checking TMDB API key...")

	customAPIKey := config.Get().TMDBApiKey
	if customAPIKey != "" {
		apiKeys = append(apiKeys, customAPIKey)
		apiKey = customAPIKey
	}

	result := false
	for index := len(apiKeys) - 1; index >= 0; index-- {
		result = tmdbCheck(apiKey)
		if result {
			log.Noticef("TMDB API key check passed, using %s...", apiKey[:7])
			break
		} else {
			log.Warningf("TMDB API key failed: %s", apiKey)
			if apiKey == apiKeys[index] {
				apiKeys = append(apiKeys[:index], apiKeys[index+1:]...)
			}
			if len(apiKeys) > 0 {
				apiKey = apiKeys[rand.Intn(len(apiKeys))]
			} else {
				result = false
				break
			}
		}
	}
	if !result {
		log.Error("No valid TMDB API key found")
	}
}

func tmdbCheck(key string) bool {
	var result *Entity

	urlValues := napping.Params{
		"api_key": key,
	}.AsUrlValues()

	resp, err := napping.Get(
		tmdbEndpoint+"/movie/550",
		&urlValues,
		&result,
		nil,
	)

	if err != nil {
		log.Error(err.Error())
		if xbmcHost, err := xbmc.GetLocalXBMCHost(); err == nil && xbmcHost != nil {
			xbmcHost.Notify("Elementum", "TMDB check failed, check your logs.", config.AddonIcon())
		}
		return false
	} else if resp.Status() != 200 {
		return false
	}

	return true
}

// ImageURL ...
func ImageURL(uri string, size string) string {
	if uri == "" {
		return ""
	}

	return imageEndpoint + size + uri
}

// ListEntities ...
// TODO Unused...
// func ListEntities(endpoint string, params napping.Params) []*Entity {
// 	var wg sync.WaitGroup
// 	resultsPerPage := config.Get().ResultsPerPage
// 	entities := make([]*Entity, PagesAtOnce*resultsPerPage)
// 	params["api_key"] = apiKey
// 	params["language"] = config.Get().Language

// 	wg.Add(PagesAtOnce)
// 	for i := 0; i < PagesAtOnce; i++ {
// 		go func(page int) {
// 			defer wg.Done()
// 			var tmp *EntityList
// 			tmpParams := napping.Params{
// 				"page": strconv.Itoa(page),
// 			}
// 			for k, v := range params {
// 				tmpParams[k] = v
// 			}
// 			urlValues := tmpParams.AsUrlValues()
// 			rl.Call(func() error {
// 				resp, err := napping.Get(
// 					tmdbEndpoint+endpoint,
// 					&urlValues,
// 					&tmp,
// 					nil,
// 				)
// 				if err != nil {
// 					log.Error(err.Error())
// 					xbmc.Notify("Elementum", "Failed listing entities, check your logs.", config.AddonIcon())
// 				} else if resp.Status() != 200 {
// 					message := fmt.Sprintf("Bad status listing entities: %d", resp.Status())
// 					log.Error(message)
// 					xbmc.Notify("Elementum", message, config.AddonIcon())
// 				}

// 				return nil
// 			})
// 			for i, entity := range tmp.Results {
// 				entities[page*resultsPerPage+i] = entity
// 			}
// 		}(i)
// 	}
// 	wg.Wait()

// 	return entities
// }

// Find ...
func Find(externalID string, externalSource string) *FindResult {
	var result *FindResult

	cacheStore := cache.NewDBStore()
	key := fmt.Sprintf(cache.TMDBFindKey, externalSource, externalID)
	if err := cacheStore.Get(key, &result); err != nil {
		err = MakeRequest(APIRequest{
			URL: fmt.Sprintf("%s/find/%s", tmdbEndpoint, externalID),
			Params: napping.Params{
				"api_key":         apiKey,
				"external_source": externalSource,
			}.AsUrlValues(),
			Result:      &result,
			Description: "find",
		})

		if err == nil && result != nil {
			cacheStore.Set(key, result, cache.TMDBFindExpire)
		}
	}

	return result
}

// GetCountries ...
func GetCountries(language string) []*Country {
	countries := CountryList{}

	cacheStore := cache.NewDBStore()
	key := fmt.Sprintf(cache.TMDBCountriesKey, language)
	if err := cacheStore.Get(key, &countries); err != nil {
		err = MakeRequest(APIRequest{
			URL: fmt.Sprintf("%s/configuration/countries", tmdbEndpoint),
			Params: napping.Params{
				"api_key": apiKey,
			}.AsUrlValues(),
			Result:      &countries,
			Description: "countries",
		})

		if err == nil {
			sort.Slice(countries, func(i, j int) bool {
				return countries[i].EnglishName < countries[j].EnglishName
			})
			cacheStore.Set(key, countries, cache.TMDBCountriesExpire)
		}
	}
	return countries
}

// GetLanguages ...
func GetLanguages(language string) []*Language {
	languages := []*Language{}
	cacheStore := cache.NewDBStore()

	key := fmt.Sprintf(cache.TMDBLanguagesKey, language)
	if err := cacheStore.Get(key, &languages); err != nil {
		err = MakeRequest(APIRequest{
			URL: fmt.Sprintf("%s/configuration/languages", tmdbEndpoint),
			Params: napping.Params{
				"api_key": apiKey,
			}.AsUrlValues(),
			Result:      &languages,
			Description: "languages",
		})

		if err == nil {
			for _, l := range languages {
				if l.Name == "" {
					l.Name = l.EnglishName
				}
			}

			sort.Slice(languages, func(i, j int) bool {
				return languages[i].Name < languages[j].Name
			})
			cacheStore.Set(key, languages, cache.TMDBLanguagesExpire)
		}
	}
	return languages
}

// MakeRequest used to proxy requests with proper RateLimiter usage and HTTP error processing
func MakeRequest(r APIRequest) (ret error) {
	defer perf.ScopeTimer()()

	rl.Call(func() error {
		httpTransport := &http.Transport{}
		if config.Get().ProxyURL != "" {
			proxyURL, _ := url.Parse(config.Get().ProxyURL)
			httpTransport.Proxy = http.ProxyURL(proxyURL)
		}
		httpClient := &http.Client{
			Transport: httpTransport,
		}
		session := napping.Session{
			Client: httpClient,
		}
		resp, err := session.Get(
			r.URL,
			&r.Params,
			r.Result,
			r.ErrMsg,
		)
		if err != nil {
			log.Errorf("Failed to make request to %s for %s with %+v: %s", r.URL, r.Description, r.Params, err)
			ret = err
		} else if resp.Status() == 429 {
			log.Warningf("Rate limit exceeded getting %s with %+v on %s, cooling down...", r.Description, r.Params, r.URL)
			rl.CoolDown(resp.HttpResponse().Header)
			ret = util.ErrExceeded
			return util.ErrExceeded
		} else if resp.Status() == 404 {
			log.Warningf("Rate limit exceeded getting %s with %+v on %s, cooling down...", r.Description, r.Params, r.URL)
			rl.CoolDown(resp.HttpResponse().Header)
			ret = util.ErrNotFound
			return util.ErrNotFound
		} else if resp.Status() != 200 {
			log.Errorf("Bad status getting %s with %+v on %s: %d", r.Description, r.Params, r.URL, resp.Status())
			ret = util.ErrHTTP
			return util.ErrHTTP
		}

		ret = nil
		return nil
	})

	return
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
		if !c.ShowUnairedSeasons {
			if _, isExpired := util.AirDateWithExpireCheck(s.AirDate, time.DateOnly, c.ShowEpisodesOnReleaseDay); isExpired {
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

// HasEpisode checks if episode with specific number is available in the episodes list
func (season *Season) HasEpisode(episode int) bool {
	if len(season.Episodes) <= 0 {
		return false
	}

	for _, e := range season.Episodes {
		if e != nil && e.EpisodeNumber == episode {
			return true
		}
	}
	return false
}

// GetEpisode gets episode with specific number from Episodes list
func (season *Season) GetEpisode(episode int) *Episode {
	if len(season.Episodes) <= 0 {
		return nil
	}

	for _, e := range season.Episodes {
		if e != nil && e.EpisodeNumber == episode {
			return e
		}
	}
	return nil
}

// GetCastMembers returns formatted cast members
func (credits *Credits) GetCastMembers() []xbmc.ListItemCastMember {
	res := make([]xbmc.ListItemCastMember, 0)
	for _, cast := range credits.Cast {
		res = append(res, xbmc.ListItemCastMember{
			Name:      cast.Name,
			Role:      cast.Character,
			Thumbnail: ImageURL(cast.ProfilePath, "w500"),
			Order:     cast.Order,
		})
	}
	return res
}

// GetDirectors returns list of directors
func (credits *Credits) GetDirectors() []string {
	directors := make([]string, 0)
	for _, crew := range credits.Crew {
		if crew.Job == "Director" {
			directors = append(directors, crew.Name)
		}
	}
	return directors
}

// GetWriters returns list of writers
func (credits *Credits) GetWriters() []string {
	writers := make([]string, 0)
	for _, crew := range credits.Crew {
		if crew.Department == "Writing" {
			writers = append(writers, crew.Name)
		}
	}
	return writers
}

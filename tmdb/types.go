package tmdb

import (
	"net/url"

	"github.com/elgatito/elementum/fanart"
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

	ExternalIDs         *ExternalIDs  `json:"external_ids"`
	FanArt              *fanart.Movie `json:"fanart"`
	IMDBId              string        `json:"imdb_id"`
	Popularity          float64       `json:"-"`
	ProductionCompanies []*IDNameLogo `json:"production_companies"`
	ProductionCountries []*Country    `json:"production_countries"`
	RawPopularity       interface{}   `json:"popularity"`
	Runtime             int           `json:"runtime"`
	SpokenLanguages     []*Language   `json:"spoken_languages"`
	TagLine             string        `json:"tagline"`

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

	ReleaseDates *ReleaseDatesResults `json:"release_dates"`
}

// Show ...
type Show struct {
	Entity

	EpisodeRunTime      []int         `json:"episode_run_time"`
	ExternalIDs         *ExternalIDs  `json:"external_ids"`
	FanArt              *fanart.Show  `json:"fanart"`
	Homepage            string        `json:"homepage"`
	InProduction        bool          `json:"in_production"`
	LastAirDate         string        `json:"last_air_date"`
	Networks            []*IDNameLogo `json:"networks"`
	NumberOfEpisodes    int           `json:"number_of_episodes"`
	NumberOfSeasons     int           `json:"number_of_seasons"`
	OriginCountry       []string      `json:"origin_country"`
	Popularity          float64       `json:"-"`
	ProductionCompanies []*IDNameLogo `json:"production_companies"`
	ProductionCountries []*Country    `json:"production_countries"`
	RawPopularity       interface{}   `json:"popularity"`
	SpokenLanguages     []*Language   `json:"spoken_languages"`
	Status              string        `json:"status"`
	TagLine             string        `json:"tagline"`

	LastEpisodeToAir *Episode `json:"last_episode_to_air"`
	NextEpisodeToAir *Episode `json:"next_episode_to_air"`

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

	Seasons SeasonList `json:"seasons"`
}

// Season ...
type Season struct {
	Entity

	AirDate      string       `json:"air_date"`
	EpisodeCount int          `json:"episode_count,omitempty"`
	ExternalIDs  *ExternalIDs `json:"external_ids"`
	Season       int          `json:"season_number"`

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

	Episodes EpisodeList `json:"episodes"`
}

// Episode ...
type Episode struct {
	Entity

	AirDate       string       `json:"air_date"`
	EpisodeNumber int          `json:"episode_number"`
	ExternalIDs   *ExternalIDs `json:"external_ids"`
	Runtime       int          `json:"runtime"`
	SeasonNumber  int          `json:"season_number"`
	StillPath     string       `json:"still_path"`

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
}

// Entity ...
type Entity struct {
	BackdropPath     string    `json:"backdrop_path"`
	FirstAirDate     string    `json:"first_air_date"`
	Genres           []*IDName `json:"genres"`
	ID               int       `json:"id"`
	IsAdult          bool      `json:"adult"`
	Name             string    `json:"name,omitempty"`
	OriginalLanguage string    `json:"original_language,omitempty"`
	OriginalName     string    `json:"original_name,omitempty"`
	OriginalTitle    string    `json:"original_title,omitempty"`
	Overview         string    `json:"overview"`
	PosterPath       string    `json:"poster_path"`
	ReleaseDate      string    `json:"release_date"`
	Title            string    `json:"title,omitempty"`
	VoteAverage      float32   `json:"vote_average"`
	VoteCount        int       `json:"vote_count"`

	Images *Images `json:"images,omitempty"`
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
	NativeName  string `json:"native_name"`
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
	Logos     []*Image `json:"logos"`
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
	TagLine  string `json:"tagline"`
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
	ID            int       `json:"id"`
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

// ImageQualityIdentifier contains the image quality as a string, e.g. "w1280"
type ImageQualityIdentifier string

// ImageQualityBundle contains image qualities for different type of images
type ImageQualityBundle struct {
	Poster    ImageQualityIdentifier
	FanArt    ImageQualityIdentifier
	Logo      ImageQualityIdentifier
	Thumbnail ImageQualityIdentifier
	Landscape ImageQualityIdentifier
}

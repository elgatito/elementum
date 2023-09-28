package providers

import (
	"encoding/base64"
	"encoding/json"
)

// SearchPayload ...
type SearchPayload struct {
	Method       string      `json:"method"`
	CallbackURL  string      `json:"callback_url"`
	SearchObject interface{} `json:"search_object"`
}

// GeneralSearchObject ...
type GeneralSearchObject struct {
	ProxyURL         string `json:"proxy_url"`
	InternalProxyURL string `json:"internal_proxy_url"`
	ElementumURL     string `json:"elementum_url"`

	Silent   bool `json:"silent"`
	SkipAuth bool `json:"skip_auth"`
}

// QuerySearchObject ...
type QuerySearchObject struct {
	GeneralSearchObject
	Query string `json:"query"`
}

// MovieSearchObject ...
type MovieSearchObject struct {
	GeneralSearchObject
	IMDBId string            `json:"imdb_id"`
	TMDBId int               `json:"tmdb_id"`
	Title  string            `json:"title"`
	Year   int               `json:"year"`
	Years  map[string]int    `json:"years"`
	Titles map[string]string `json:"titles"`
}

// SeasonSearchObject ...
type SeasonSearchObject struct {
	GeneralSearchObject
	IMDBId     string            `json:"imdb_id"`
	TVDBId     int               `json:"tvdb_id"`
	TMDBId     int               `json:"tmdb_id"`
	ShowTMDBId int               `json:"show_tmdb_id"`
	Title      string            `json:"title"`
	Season     int               `json:"season"`
	Year       int               `json:"year"`
	Titles     map[string]string `json:"titles"`
	Anime      bool              `json:"anime"`
}

// EpisodeSearchObject ...
type EpisodeSearchObject struct {
	GeneralSearchObject
	IMDBId         string            `json:"imdb_id"`
	TVDBId         int               `json:"tvdb_id"`
	TMDBId         int               `json:"tmdb_id"`
	ShowTMDBId     int               `json:"show_tmdb_id"`
	Title          string            `json:"title"`
	Season         int               `json:"season"`
	SeasonName     string            `json:"season_name"`
	Episode        int               `json:"episode"`
	Year           int               `json:"year"`
	SeasonYear     int               `json:"season_year"`
	ShowYear       int               `json:"show_year"`
	Titles         map[string]string `json:"titles"`
	AbsoluteNumber int               `json:"absolute_number"`
	Anime          bool              `json:"anime"`
}

func (sp *SearchPayload) String() string {
	b, err := json.Marshal(sp)
	if err != nil {
		return ""
	}
	return base64.StdEncoding.EncodeToString(b)
}

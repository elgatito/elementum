package tmdb

import (
	"fmt"
	"math/rand"
	"sort"

	"github.com/elgatito/elementum/cache"
	"github.com/elgatito/elementum/config"
	"github.com/elgatito/elementum/util/event"
	"github.com/elgatito/elementum/util/reqapi"
	"github.com/elgatito/elementum/xbmc"

	"github.com/jmcvetta/napping"
	"github.com/op/go-logging"
)

const (
	// TMDBResultsPerPage reflects TMDB number of results on the page. It's statically set to 20, so we should work with that
	TMDBResultsPerPage = 20

	imageEndpoint = "https://image.tmdb.org/t/p/"
)

var (
	log = logging.MustGetLogger("tmdb")

	apiKeys = []string{
		"8cf43ad9c085135b9479ad5cf6bbcbda",
		"ae4bd1b6fce2a5648671bfc171d15ba4",
		"29a551a65eef108dd01b46e27eb0554a",
	}
	apiKey = apiKeys[rand.Intn(len(apiKeys))]

	WarmingUp = event.Event{}
)

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

	req := reqapi.Request{
		API: reqapi.TMDBAPI,
		URL: "/movie/550",
		Params: napping.Params{
			"api_key": key,
		}.AsUrlValues(),
		Result:      &result,
		Description: "tmdb api key check",
	}

	if err := req.Do(); err != nil {
		log.Error(err.Error())
		if xbmcHost, _ := xbmc.GetLocalXBMCHost(); xbmcHost != nil {
			xbmcHost.Notify("Elementum", "TMDB check failed, check your logs.", config.AddonIcon())
		}
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

	req := reqapi.Request{
		API: reqapi.TMDBAPI,
		URL: fmt.Sprintf("/find/%s", externalID),
		Params: napping.Params{
			"api_key":         apiKey,
			"external_source": externalSource,
		}.AsUrlValues(),
		Result:      &result,
		Description: "find",

		Cache:       true,
		CacheExpire: cache.CacheExpireLong,
	}

	req.Do()

	return result
}

// GetCountries ...
func GetCountries(language string) []*Country {
	countries := CountryList{}

	req := reqapi.Request{
		API: reqapi.TMDBAPI,
		URL: "/configuration/countries",
		Params: napping.Params{
			"api_key":  apiKey,
			"language": language,
		}.AsUrlValues(),
		Result:      &countries,
		Description: "countries",

		Cache:       true,
		CacheExpire: cache.CacheExpireLong,
	}

	if err := req.Do(); err == nil {
		for _, c := range countries {
			if c.NativeName != "" {
				c.EnglishName = c.NativeName
			}
		}

		sort.Slice(countries, func(i, j int) bool {
			return countries[i].EnglishName < countries[j].EnglishName
		})
	}
	return countries
}

// GetLanguages ...
func GetLanguages(language string) []*Language {
	languages := []*Language{}

	req := reqapi.Request{
		API: reqapi.TMDBAPI,
		URL: "/configuration/languages",
		Params: napping.Params{
			"api_key":  apiKey,
			"language": language,
		}.AsUrlValues(),
		Result:      &languages,
		Description: "languages",

		Cache:       true,
		CacheExpire: cache.CacheExpireLong,
	}

	if err := req.Do(); err == nil {
		for _, l := range languages {
			if l.Name == "" {
				l.Name = l.EnglishName
			}
		}

		sort.Slice(languages, func(i, j int) bool {
			return languages[i].Name < languages[j].Name
		})
	}
	return languages
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
		if crew == nil {
			continue
		}

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

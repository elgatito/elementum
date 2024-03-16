package tmdb

import (
	"fmt"
	"math/rand"
	"net/url"
	"sort"
	"strings"

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

	//                                                  Original    High    Medium  Low
	ImageQualitiesPoster    = []ImageQualityIdentifier{"original", "w780", "w500", "w342"}
	ImageQualitiesFanArt    = []ImageQualityIdentifier{"original", "w1280", "w1280", "w780"}
	ImageQualitiesLogo      = []ImageQualityIdentifier{"original", "w500", "w500", "w300"}
	ImageQualitiesThumbnail = []ImageQualityIdentifier{"original", "w1280", "w780", "w500"}
	ImageQualitiesLandscape = []ImageQualityIdentifier{"original", "w1280", "w780", "w500"}
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
func ImageURL(uri string, size ImageQualityIdentifier) (imageURL string) {
	if uri == "" {
		return ""
	}

	imageURL, _ = url.JoinPath(imageEndpoint, string(size), uri)
	return
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

func GetImageQualities() (imageQualities ImageQualityBundle) {
	return ImageQualityBundle{
		Poster:    ImageQualitiesPoster[config.Get().TMDBImagesQuality],
		FanArt:    ImageQualitiesFanArt[config.Get().TMDBImagesQuality],
		Logo:      ImageQualitiesLogo[config.Get().TMDBImagesQuality],
		Thumbnail: ImageQualitiesThumbnail[config.Get().TMDBImagesQuality],
		Landscape: ImageQualitiesLandscape[config.Get().TMDBImagesQuality],
	}
}

// GetLocalizedImages returns localized image, all images, images with text and images without text, so those can be used to set Kodi Arts
func GetLocalizedImages(images []*Image, imageQuality ImageQualityIdentifier) (localizedImage string, allImages []string, imagesWithText []string, imagesWithoutText []string) {
	foundLanguageSpecificImage := false
	for _, image := range images {
		if strings.HasSuffix(image.FilePath, ".svg") { //Kodi does not support svg images
			continue
		}

		imageURL := ImageURL(image.FilePath, imageQuality)
		allImages = append(allImages, imageURL)

		if image.Iso639_1 == "" {
			imagesWithoutText = append(imagesWithoutText, imageURL)
		} else {
			imagesWithText = append(imagesWithText, imageURL)
		}

		// Try find localized image
		if !foundLanguageSpecificImage && image.Iso639_1 == config.Get().Language {
			localizedImage = imageURL
			foundLanguageSpecificImage = true // we take first image, it has top rating
		}
	}
	// If there is no localized image - then set it to the first image with text.
	// It would be image in SecondLanguage from config, since we always get SecondLanguage images as backup.
	if !foundLanguageSpecificImage && len(imagesWithText) > 0 {
		localizedImage = imagesWithText[0]
	}

	return
}

func SetLocalizedArt(video *Entity, item *xbmc.ListItem) {
	if video.Images != nil {
		imageQualities := GetImageQualities()

		localizedBackdrop, _, backdropsWithText, _ := GetLocalizedImages(video.Images.Backdrops, imageQualities.Landscape)
		// Landscape should be with text
		if localizedBackdrop != "" { // We set Landscape only if there is a localized backdrop with text
			item.Art.Landscape = localizedBackdrop // otherwise we let skin construct Landscape
		}
		// Do not assign empty list since fallback Art could have been be set in parent (e.g. season and show)
		if len(backdropsWithText) > 0 {
			item.Art.AvailableArtworks.Landscape = backdropsWithText
		}

		_, _, _, backdropsWithoutText := GetLocalizedImages(video.Images.Backdrops, imageQualities.FanArt)
		// FanArt should be without text and it is already set by default, so set only AvailableArtworks
		if len(backdropsWithoutText) > 0 {
			item.Art.FanArts = backdropsWithoutText
			item.Art.AvailableArtworks.FanArt = backdropsWithoutText
		}

		localizedPoster, allPosters, _, _ := GetLocalizedImages(video.Images.Posters, imageQualities.Poster)
		// Poster in user's Language or SecondLanguage or leave Default Poster
		if localizedPoster != "" {
			item.Art.Poster = localizedPoster
		}
		if len(allPosters) > 0 {
			item.Art.AvailableArtworks.Poster = allPosters
		}

		localizedLogo, allLogos, _, _ := GetLocalizedImages(video.Images.Logos, imageQualities.Logo)
		if localizedLogo != "" {
			item.Art.ClearLogo = localizedLogo
		}
		if len(allLogos) > 0 {
			item.Art.AvailableArtworks.ClearLogo = allLogos
		}

		// Thumbnail does not have localization, so set only AvailableArtworks
		_, allStills, _, _ := GetLocalizedImages(video.Images.Stills, imageQualities.Thumbnail)
		if len(allStills) > 0 {
			item.Art.AvailableArtworks.Thumbnail = allStills
		}
	}
}

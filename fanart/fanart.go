package fanart

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/elgatito/elementum/cache"
	"github.com/elgatito/elementum/config"
	"github.com/elgatito/elementum/util/reqapi"
	"github.com/elgatito/elementum/xbmc"

	"github.com/anacrolix/missinggo/perf"
	"github.com/jmcvetta/napping"
)

const (
	// ClientID ...
	ClientID = "decb307ca800170b833c3061863974f3"
	// APIVersion ...
	APIVersion = "v3"
)

// Movie ...
type Movie struct {
	Name            string   `json:"name"`
	TmdbID          string   `json:"tmdb_id"`
	ImdbID          string   `json:"imdb_id"`
	HDMovieClearArt []*Image `json:"hdmovieclearart"`
	HDMovieLogo     []*Image `json:"hdmovielogo"`
	MoviePoster     []*Image `json:"movieposter"`
	MovieBackground []*Image `json:"moviebackground"`
	MovieDisc       []*Disk  `json:"moviedisc"`
	MovieThumb      []*Image `json:"moviethumb"`
	MovieArt        []*Image `json:"movieart"`
	MovieClearArt   []*Image `json:"movieclearart"`
	MovieLogo       []*Image `json:"movielogo"`
	MovieBanner     []*Image `json:"moviebanner"`
}

// Show ...
type Show struct {
	Name           string       `json:"name"`
	TvdbID         string       `json:"thetvdb_id"`
	HDClearArt     []*ShowImage `json:"hdclearart"`
	HdtvLogo       []*ShowImage `json:"hdtvlogo"`
	ClearLogo      []*ShowImage `json:"clearlogo"`
	ClearArt       []*ShowImage `json:"clearart"`
	TVPoster       []*ShowImage `json:"tvposter"`
	TVBanner       []*ShowImage `json:"tvbanner"`
	TVThumb        []*ShowImage `json:"tvthumb"`
	ShowBackground []*ShowImage `json:"showbackground"`
	SeasonPoster   []*ShowImage `json:"seasonposter"`
	SeasonThumb    []*ShowImage `json:"seasonthumb"`
	SeasonBanner   []*ShowImage `json:"seasonbanner"`
	CharacterArt   []*ShowImage `json:"characterart"`
}

// ShowImage ...
type ShowImage struct {
	Image
	Season string `json:"season"`
}

// Image ...
type Image struct {
	ID    string `json:"id"`
	URL   string `json:"url"`
	Lang  string `json:"lang"`
	Likes string `json:"likes"`
}

// Disk ...
type Disk struct {
	Image
	Disc     string `json:"disc"`
	DiscType string `json:"disc_type"`
}

func GetHeader() http.Header {
	return http.Header{
		"Content-type": []string{"application/json"},
		"api-key":      []string{ClientID},
		"api-version":  []string{APIVersion},
	}
}

// GetMovie ...
func GetMovie(tmdbID int) (movie *Movie) {
	if tmdbID == 0 {
		return nil
	}

	defer perf.ScopeTimer()()

	req := reqapi.Request{
		API:         reqapi.FanartAPI,
		URL:         fmt.Sprintf("/movies/%d", tmdbID),
		Header:      GetHeader(),
		Params:      napping.Params{}.AsUrlValues(),
		Result:      &movie,
		Description: "movie fanart",

		Cache:       true,
		CacheExpire: cache.CacheExpireLong,
	}

	req.Do()
	return
}

// GetShow ...
func GetShow(tvdbID int) (show *Show) {
	if tvdbID == 0 {
		return nil
	}

	defer perf.ScopeTimer()()

	req := reqapi.Request{
		API:         reqapi.FanartAPI,
		URL:         fmt.Sprintf("/tv/%d", tvdbID),
		Header:      GetHeader(),
		Params:      napping.Params{}.AsUrlValues(),
		Result:      &show,
		Description: "show fanart",

		Cache:       true,
		CacheExpire: cache.CacheExpireLong,
	}

	req.Do()
	return
}

// GetMultipleImage returns multiple images in a list
func GetMultipleImage(old string, lists ...[]*Image) []string {
	if len(lists) == 0 {
		return []string{old}
	}

	res := []string{}
	language := config.Get().Language
	for _, l := range lists {
		for _, i := range l {
			if i == nil {
				continue
			}

			if i.Lang == language && !contains(res, i.URL) {
				res = append(res, i.URL)
			}
			if i.Lang == "en" || i.Lang == "" {
				if !contains(res, i.URL) {
					res = append(res, i.URL)
				}
			}
		}
	}

	if len(res) > 0 {
		return res
	}
	return []string{old}
}

// GetBestImage returns best image from multiple lists,
// according to the lang setting. Taking order of lists into account.
func GetBestImage(old string, lists ...[]*Image) string {
	if len(lists) == 0 {
		return ""
	}

	language := config.Get().Language
	for _, l := range lists {
		bestLikes := 0
		bestItem := ""

		for _, i := range l {
			if i == nil {
				continue
			}

			if i.Lang == language {
				return i.URL
			}
			if i.Lang == "en" || i.Lang == "" {
				if likes := likeConvert(i.Likes); likes > bestLikes {
					bestItem = i.URL
					bestLikes = likes
				}
			}
		}

		if bestLikes > 0 {
			return bestItem
		}
	}

	return old
}

// GetMultipleShowImage returns multiple images in a list
func GetMultipleShowImage(season, old string, lists ...[]*ShowImage) []string {
	if len(lists) == 0 {
		return []string{old}
	}

	res := []string{}
	language := config.Get().Language
	for _, l := range lists {
		for _, i := range l {
			if i == nil {
				continue
			}

			if season == "" || i.Season == season {
				if i.Lang == language && !contains(res, i.URL) {
					res = append(res, i.URL)
				}
				if i.Lang == "en" || i.Lang == "" {
					if !contains(res, i.URL) {
						res = append(res, i.URL)
					}
				}
			}
		}

		if len(res) > 0 {
			return res
		}

		for _, i := range l {
			if i == nil {
				continue
			}

			if season == "" || i.Season == "0" || i.Season == "" {
				if i.Lang == language && !contains(res, i.URL) {
					res = append(res, i.URL)
				}
				if i.Lang == "en" || i.Lang == "" {
					if !contains(res, i.URL) {
						res = append(res, i.URL)
					}
				}
			}
		}
	}

	if len(res) > 0 {
		return res
	}
	return []string{old}
}

// GetBestShowImage returns best image from multiple lists,
// according to the lang setting. Taking order of lists into account.
func GetBestShowImage(season string, isStrict bool, old string, lists ...[]*ShowImage) string {
	if len(lists) == 0 {
		return ""
	}

	idx := 0
	language := config.Get().Language
	for _, l := range lists {
		idx++

		bestLikes := 0
		bestItem := ""

		for _, i := range l {
			if i == nil {
				continue
			}

			if season == "" || i.Season == season {
				if i.Lang == language {
					return i.URL
				}
				if i.Lang == "en" || i.Lang == "" {
					if likes := likeConvert(i.Likes); likes > bestLikes {
						bestItem = i.URL
						bestLikes = likes
					}
				}
			}
		}

		if bestLikes > 0 {
			return bestItem
		}

		for _, i := range l {
			if i == nil {
				continue
			}

			// Take item with season=0 only if this is not a strict mode,
			//    which means first array is season dedicated, and 0 means special.
			if season == "" || (i.Season == "0" && (!isStrict || idx > 1)) || i.Season == "" {
				if i.Lang == language {
					return i.URL
				}
				if i.Lang == "en" || i.Lang == "" {
					if likes := likeConvert(i.Likes); likes > bestLikes {
						bestItem = i.URL
						bestLikes = likes
					}
				}
			}
		}

		if bestLikes > 0 {
			return bestItem
		}
	}

	return old
}

// ToListItemArt ...
func (fa *Movie) ToListItemArt(old *xbmc.ListItemArt) *xbmc.ListItemArt {
	if old == nil {
		old = &xbmc.ListItemArt{}
	}

	availableArtworks := &xbmc.Artworks{
		Poster:    GetMultipleImage(old.Poster, fa.MoviePoster),
		Banner:    GetMultipleImage(old.Banner, fa.MovieBanner),
		FanArt:    GetMultipleImage(old.FanArt, fa.MovieBackground),
		ClearArt:  GetMultipleImage(old.ClearArt, fa.HDMovieClearArt, fa.MovieClearArt),
		ClearLogo: GetMultipleImage(old.ClearLogo, fa.HDMovieLogo, fa.MovieLogo),
		Landscape: GetMultipleImage(old.Landscape, fa.MovieThumb),
		KeyArt:    GetMultipleImage(old.KeyArt, fa.MovieBackground),
		DiscArt:   GetMultipleImage(old.DiscArt, disksToImages(fa.MovieDisc)),
	}
	return &xbmc.ListItemArt{
		Poster:            GetBestImage(old.Poster, fa.MoviePoster),
		Thumbnail:         old.Thumbnail,
		Banner:            GetBestImage(old.Banner, fa.MovieBanner),
		FanArt:            GetBestImage(old.FanArt, fa.MovieBackground),
		FanArts:           GetMultipleImage(old.FanArt, fa.MovieBackground),
		ClearArt:          GetBestImage(old.ClearArt, fa.HDMovieClearArt, fa.MovieClearArt),
		ClearLogo:         GetBestImage(old.ClearLogo, fa.HDMovieLogo, fa.MovieLogo),
		Landscape:         GetBestImage(old.Landscape, fa.MovieThumb),
		KeyArt:            GetBestImage(old.KeyArt, fa.MovieBackground),
		DiscArt:           GetBestImage(old.DiscArt, disksToImages(fa.MovieDisc)),
		AvailableArtworks: availableArtworks,
	}
}

// ToListItemArt ...
func (fa *Show) ToListItemArt(old *xbmc.ListItemArt) *xbmc.ListItemArt {
	if old == nil {
		old = &xbmc.ListItemArt{}
	}

	availableArtworks := &xbmc.Artworks{
		Poster:    GetMultipleShowImage("", old.Poster, fa.TVPoster),
		Banner:    GetMultipleShowImage("", old.Banner, fa.TVBanner),
		FanArt:    GetMultipleShowImage("", old.FanArt, fa.ShowBackground),
		ClearArt:  GetMultipleShowImage("", old.ClearArt, fa.HDClearArt, fa.ClearArt),
		ClearLogo: GetMultipleShowImage("", old.ClearLogo, fa.HdtvLogo, fa.ClearLogo),
		Landscape: GetMultipleShowImage("", old.Landscape, fa.TVThumb),
		KeyArt:    GetMultipleShowImage("", old.KeyArt, fa.ShowBackground),
	}
	return &xbmc.ListItemArt{
		TvShowPoster:      GetBestShowImage("", false, old.Poster, fa.TVPoster),
		Poster:            GetBestShowImage("", false, old.Poster, fa.TVPoster),
		Thumbnail:         old.Thumbnail,
		Banner:            GetBestShowImage("", false, old.Banner, fa.TVBanner),
		FanArt:            GetBestShowImage("", false, old.FanArt, fa.ShowBackground),
		FanArts:           GetMultipleShowImage("", old.FanArt, fa.ShowBackground),
		ClearArt:          GetBestShowImage("", false, old.ClearArt, fa.HDClearArt, fa.ClearArt),
		ClearLogo:         GetBestShowImage("", false, old.ClearLogo, fa.HdtvLogo, fa.ClearLogo),
		Landscape:         GetBestShowImage("", false, old.Landscape, fa.TVThumb),
		KeyArt:            GetBestShowImage("", false, old.KeyArt, fa.ShowBackground),
		AvailableArtworks: availableArtworks,
	}
}

// ToSeasonListItemArt ...
func (fa *Show) ToSeasonListItemArt(season int, old *xbmc.ListItemArt) *xbmc.ListItemArt {
	s := strconv.Itoa(season)
	if old == nil {
		old = &xbmc.ListItemArt{}
	}

	availableArtworks := &xbmc.Artworks{
		Poster:    GetMultipleShowImage(s, old.Poster, fa.SeasonPoster, fa.TVPoster),
		Banner:    GetMultipleShowImage(s, old.Banner, fa.SeasonBanner, fa.TVBanner),
		FanArt:    GetMultipleShowImage(s, old.FanArt, fa.ShowBackground),
		ClearArt:  GetMultipleShowImage(s, old.ClearArt, fa.HDClearArt, fa.ClearArt),
		ClearLogo: GetMultipleShowImage(s, old.ClearLogo, fa.HdtvLogo, fa.ClearLogo),
		Landscape: GetMultipleShowImage(s, old.Landscape, fa.SeasonThumb, fa.TVThumb),
		KeyArt:    GetMultipleShowImage(s, old.KeyArt, fa.ShowBackground),
	}
	return &xbmc.ListItemArt{
		TvShowPoster:      GetBestShowImage("", true, old.Poster, fa.SeasonPoster, fa.TVPoster),
		Poster:            GetBestShowImage(s, true, old.Poster, fa.SeasonPoster, fa.TVPoster),
		Thumbnail:         old.Thumbnail,
		Banner:            GetBestShowImage(s, true, old.Banner, fa.SeasonBanner, fa.TVBanner),
		FanArt:            GetBestShowImage(s, false, old.FanArt, fa.ShowBackground),
		FanArts:           GetMultipleShowImage(s, old.FanArt, fa.ShowBackground),
		ClearArt:          GetBestShowImage(s, false, old.ClearArt, fa.HDClearArt, fa.ClearArt),
		ClearLogo:         GetBestShowImage(s, false, old.ClearLogo, fa.HdtvLogo, fa.ClearLogo),
		Landscape:         GetBestShowImage(s, true, old.Landscape, fa.SeasonThumb, fa.TVThumb),
		KeyArt:            GetBestShowImage(s, false, old.KeyArt, fa.ShowBackground),
		AvailableArtworks: availableArtworks,
	}
}

// ToEpisodeListItemArt ...
func (fa *Show) ToEpisodeListItemArt(season int, old *xbmc.ListItemArt) *xbmc.ListItemArt {
	s := strconv.Itoa(season)
	if old == nil {
		old = &xbmc.ListItemArt{}
	}

	availableArtworks := &xbmc.Artworks{
		Poster:    GetMultipleShowImage(s, old.Poster, fa.SeasonPoster, fa.TVPoster),
		Banner:    GetMultipleShowImage(s, old.Banner, fa.SeasonBanner, fa.TVBanner),
		FanArt:    GetMultipleShowImage(s, old.FanArt, fa.ShowBackground),
		ClearArt:  GetMultipleShowImage(s, old.ClearArt, fa.HDClearArt, fa.ClearArt),
		ClearLogo: GetMultipleShowImage(s, old.ClearLogo, fa.HdtvLogo, fa.ClearLogo),
		Landscape: GetMultipleShowImage(s, old.Landscape, fa.SeasonThumb, fa.TVThumb),
		KeyArt:    GetMultipleShowImage(s, old.KeyArt, fa.ShowBackground),
	}
	return &xbmc.ListItemArt{
		TvShowPoster:      GetBestShowImage("", true, old.Poster, fa.SeasonPoster, fa.TVPoster),
		Poster:            GetBestShowImage(s, true, old.Poster, fa.SeasonPoster, fa.TVPoster),
		Thumbnail:         old.Thumbnail,
		Banner:            GetBestShowImage(s, true, old.Banner, fa.SeasonBanner, fa.TVBanner),
		FanArt:            GetBestShowImage(s, false, old.FanArt, fa.ShowBackground),
		FanArts:           GetMultipleShowImage(s, old.FanArt, fa.ShowBackground),
		ClearArt:          GetBestShowImage(s, false, old.ClearArt, fa.HDClearArt, fa.ClearArt),
		ClearLogo:         GetBestShowImage(s, false, old.ClearLogo, fa.HdtvLogo, fa.ClearLogo),
		Landscape:         GetBestShowImage(s, true, old.Landscape, fa.SeasonThumb, fa.TVThumb),
		KeyArt:            GetBestShowImage(s, false, old.KeyArt, fa.ShowBackground),
		AvailableArtworks: availableArtworks,
	}
}

func likeConvert(likes string) int {
	i, _ := strconv.Atoi(likes)
	return i
}

func contains(slice []string, val string) bool {
	for _, item := range slice {
		if item == val {
			return true
		}
	}
	return false
}

func disksToImages(disks []*Disk) []*Image {
	images := make([]*Image, len(disks))
	for i, disk := range disks {
		images[i] = &disk.Image
	}
	return images
}

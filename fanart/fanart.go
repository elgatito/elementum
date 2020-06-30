package fanart

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/elgatito/elementum/cache"
	"github.com/elgatito/elementum/util"
	"github.com/elgatito/elementum/xbmc"
	"github.com/jmcvetta/napping"
	logging "github.com/op/go-logging"
)

//go:generate msgp -o msgp.go -io=false -tests=false

const (
	// APIURL ...
	APIURL = "http://webservice.fanart.tv"
	// ClientID ...
	ClientID = "decb307ca800170b833c3061863974f3"
	// APIVersion ...
	APIVersion = "v3"
)

var log = logging.MustGetLogger("fanart")

var (
	retriesLeft             = 3
	burstRate               = 50
	burstTime               = 10 * time.Second
	simultaneousConnections = 25
	cacheExpiration         = 14 * 24 * time.Hour
)

var rl = util.NewRateLimiter(burstRate, burstTime, simultaneousConnections)

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
	ID       string `json:"id"`
	URL      string `json:"url"`
	Lang     string `json:"lang"`
	Likes    string `json:"likes"`
	Disc     string `json:"disc"`
	DiscType string `json:"disc_type"`
}

// Get ...
func Get(endPoint string, params url.Values) (resp *napping.Response, err error) {
	header := http.Header{
		"Content-type": []string{"application/json"},
		"api-key":      []string{ClientID},
		"api-version":  []string{APIVersion},
	}

	req := napping.Request{
		Url:    fmt.Sprintf("%s/%s/%s", APIURL, APIVersion, endPoint),
		Method: "GET",
		Params: &params,
		Header: &header,
	}

	rl.Call(func() error {
		resp, err = napping.Send(&req)
		if err != nil {
			return err
		} else if resp.Status() == 429 {
			log.Warningf("Rate limit exceeded getting %s, cooling down...", endPoint)
			rl.CoolDown(resp.HttpResponse().Header)
			return util.ErrExceeded
		} else if resp.Status() == 403 && retriesLeft > 0 {
			resp, err = Get(endPoint, params)
		}

		return nil
	})
	return
}

// GetMovie ...
func GetMovie(tmdbID int) (movie *Movie) {
	if tmdbID == 0 {
		return nil
	}

	endPoint := fmt.Sprintf("movies/%d", tmdbID)
	params := napping.Params{}.AsUrlValues()

	cacheStore := cache.NewDBStore()
	key := fmt.Sprintf("com.fanart.movie.%d", tmdbID)
	if err := cacheStore.Get(key, &movie); err != nil {
		resp, err := Get(endPoint, params)
		if err != nil {
			log.Debugf("Error getting fanart for movie (%d): %#v", tmdbID, err)
			return
		}

		if err := resp.Unmarshal(&movie); err != nil {
			log.Warningf("Unmarshal error for movie (%d): %#v", tmdbID, err)
			return
		}

		cacheStore.Set(key, movie, cacheExpiration)
	}

	return
}

// GetShow ...
func GetShow(tvdbID int) (show *Show) {
	if tvdbID == 0 {
		return nil
	}

	endPoint := fmt.Sprintf("tv/%d", tvdbID)
	params := napping.Params{}.AsUrlValues()

	cacheStore := cache.NewDBStore()
	key := fmt.Sprintf("com.fanart.show.%d", tvdbID)
	if err := cacheStore.Get(key, &show); err != nil {
		resp, err := Get(endPoint, params)
		if err != nil {
			log.Debugf("Error getting fanart for show (%d): %#v", tvdbID, err)
			return
		}

		if err := resp.Unmarshal(&show); err != nil {
			log.Warningf("Unmarshal error for show (%d): %#v", tvdbID, err)
			return
		}

		cacheStore.Set(key, show, cacheExpiration)
	}

	return
}

// GetBestImage returns best image from multiple lists,
// according to the lang setting. Taking order of lists into account.
func GetBestImage(old string, lists ...[]*Image) string {
	if lists == nil || len(lists) == 0 {
		return ""
	}

	language := xbmc.GetLanguageISO639_1()
	for _, l := range lists {
		for _, i := range l {
			if i == nil {
				continue
			}

			if i.Lang == language {
				return i.URL
			}
		}

		for _, i := range l {
			if i == nil {
				continue
			}

			return i.URL
		}
	}

	return old
}

// GetBestShowImage returns best image from multiple lists,
// according to the lang setting. Taking order of lists into account.
func GetBestShowImage(season, old string, lists ...[]*ShowImage) string {
	if lists == nil || len(lists) == 0 {
		return ""
	}

	language := xbmc.GetLanguageISO639_1()
	for _, l := range lists {
		for _, i := range l {
			if i == nil {
				continue
			}

			if i.Lang == language && i.Season == season {
				return i.URL
			}
		}

		for _, i := range l {
			if i == nil {
				continue
			}

			if i.Season == season {
				return i.URL
			}
		}

		for _, i := range l {
			if i == nil {
				continue
			}

			if i.Lang == language {
				return i.URL
			}
		}

		for _, i := range l {
			if i == nil {
				continue
			}

			return i.URL
		}
	}

	return old
}

// ToListItemArt ...
func (fa *Movie) ToListItemArt(old *xbmc.ListItemArt) *xbmc.ListItemArt {
	return &xbmc.ListItemArt{
		Poster:    GetBestImage(old.Poster, fa.MoviePoster),
		Thumbnail: GetBestImage(old.Thumbnail, fa.MovieThumb),
		Banner:    GetBestImage(old.Banner, fa.MovieBanner),
		FanArt:    GetBestImage(old.FanArt, fa.MovieArt, fa.MovieBackground),
		ClearArt:  GetBestImage(old.ClearArt, fa.HDMovieClearArt),
		ClearLogo: GetBestImage(old.ClearLogo, fa.HDMovieLogo, fa.MovieLogo),
		Landscape: GetBestImage(old.Landscape, fa.MovieBackground),
	}
}

// ToListItemArt ...
func (fa *Show) ToListItemArt(old *xbmc.ListItemArt) *xbmc.ListItemArt {
	return &xbmc.ListItemArt{
		Poster: GetBestShowImage("", old.Poster, fa.TVPoster),
		// Thumbnail: GetBestShowImage("", old.Thumbnail, fa.TVThumb),
		Thumbnail: GetBestShowImage("", old.Thumbnail, fa.TVPoster),
		Banner:    GetBestShowImage("", old.Banner, fa.TVBanner),
		FanArt:    GetBestShowImage("", old.FanArt, fa.ShowBackground),
		ClearArt:  GetBestShowImage("", old.ClearArt, fa.HDClearArt, fa.ClearArt),
		ClearLogo: GetBestShowImage("", old.ClearLogo, fa.ClearLogo, fa.HdtvLogo),
		Landscape: GetBestShowImage("", old.Landscape, fa.ShowBackground),
	}
}

// ToSeasonListItemArt ...
func (fa *Show) ToSeasonListItemArt(season int, old *xbmc.ListItemArt) *xbmc.ListItemArt {
	s := strconv.Itoa(season)

	return &xbmc.ListItemArt{
		TvShowPoster: GetBestShowImage(s, old.Poster, fa.TVPoster, fa.SeasonPoster),
		Poster:       GetBestShowImage(s, old.Poster, fa.SeasonPoster, fa.TVPoster),
		// Thumbnail:    GetBestShowImage(s, old.Thumbnail, fa.SeasonThumb, fa.TVThumb),
		Thumbnail: GetBestShowImage(s, old.Thumbnail, fa.SeasonPoster, fa.TVPoster),
		Banner:    GetBestShowImage(s, old.Banner, fa.SeasonBanner, fa.TVBanner),
		FanArt:    GetBestShowImage(s, old.FanArt, fa.ShowBackground),
		ClearArt:  GetBestShowImage(s, old.ClearArt, fa.HDClearArt, fa.ClearArt),
		ClearLogo: GetBestShowImage(s, old.ClearLogo, fa.ClearLogo, fa.HdtvLogo),
		Landscape: GetBestShowImage(s, old.Landscape, fa.ShowBackground),
	}
}

// ToEpisodeListItemArt ...
func (fa *Show) ToEpisodeListItemArt(season int, old *xbmc.ListItemArt) *xbmc.ListItemArt {
	s := strconv.Itoa(season)

	return &xbmc.ListItemArt{
		TvShowPoster: GetBestShowImage(s, old.Poster, fa.TVPoster, fa.SeasonPoster),
		Poster:       GetBestShowImage(s, old.Poster, fa.SeasonPoster, fa.TVPoster),
		Thumbnail:    old.Thumbnail,
		Banner:       GetBestShowImage(s, old.Banner, fa.SeasonBanner, fa.TVBanner),
		FanArt:       old.FanArt,
		ClearArt:     GetBestShowImage(s, old.ClearArt, fa.HDClearArt, fa.ClearArt),
		ClearLogo:    GetBestShowImage(s, old.ClearLogo, fa.ClearLogo, fa.HdtvLogo),
		Landscape:    GetBestShowImage(s, old.Landscape, fa.ShowBackground),
	}
}

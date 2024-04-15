package opensubtitles

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/elgatito/elementum/config"
	"github.com/elgatito/elementum/proxy"
	"github.com/elgatito/elementum/tmdb"
	"github.com/elgatito/elementum/util"
	"github.com/elgatito/elementum/util/ident"
	"github.com/elgatito/elementum/util/reqapi"
	"github.com/elgatito/elementum/xbmc"

	"github.com/goccy/go-json"
	"github.com/jmcvetta/napping"
	"github.com/op/go-logging"
)

const (
	apiKey = "HFMI55SCsDzByox4ZY8CVBLuxMSYvzCZ"
)

var (
	log = logging.MustGetLogger("opensubtitles")

	userAgent = "Elementum " + ident.GetVersion()

	ErrNoUser = errors.New("no OpenSubtitles username provided")
	ErrNoPass = errors.New("no OpenSubtitles password provided")
)

func GetHeader() http.Header {
	return http.Header{
		"Accept":       []string{"*/*"},
		"Content-type": []string{"application/json"},
		"Api-Key":      []string{apiKey},
		"User-Agent":   []string{userAgent},
	}
}

func GetAuthenticatedHeader() http.Header {
	headers := GetHeader()
	headers.Add("Authorization", fmt.Sprintf("Bearer %s", config.Get().OSDBToken))

	return headers
}

func Authorize() error {
	user := config.Get().OSDBUser
	pass := config.Get().OSDBPass

	if user == "" {
		return ErrNoUser
	} else if pass == "" {
		return ErrNoPass
	}

	payload := LoginPayload{
		Username: user,
		Password: pass,
	}
	b, _ := json.Marshal(payload)

	resp := LoginResponse{}
	req := &reqapi.Request{
		API:         reqapi.OpenSubtitlesAPI,
		Method:      "POST",
		URL:         "login",
		Header:      GetHeader(),
		Params:      napping.Params{}.AsUrlValues(),
		Payload:     bytes.NewBuffer(b),
		Description: "Login",

		Result: &resp,
	}

	if err := req.Do(); err != nil {
		log.Warningf("Error logging into OpenSubtitles: %s", err)
		return err
	}

	updateToken(resp.Token)
	updateExpiry()

	return nil
}

func updateToken(token string) {
	config.Get().OSDBToken = token
	if xbmcHost, _ := xbmc.GetLocalXBMCHost(); xbmcHost != nil {
		xbmcHost.SetSetting("opensubtitles_token", token)
	}
}

func updateExpiry() {
	expiry := time.Now().UTC().Add(time.Hour * 24).Unix()
	config.Get().OSDBTokenExpiry = expiry
	if xbmcHost, _ := xbmc.GetLocalXBMCHost(); xbmcHost != nil {
		xbmcHost.SetSetting("opensubtitles_token_expiry", strconv.Itoa(int(expiry)))
	}
}

func ensureLogin() error {
	// Validate existing login expiry
	if config.Get().OSDBToken != "" && config.Get().OSDBTokenExpiry > 0 {
		if config.Get().OSDBTokenExpiry > util.NowInt64() {
			return nil
		}
	}

	err := Authorize()
	if err != nil {
		log.Error("Could not authorize on Opensubtitles: %s", err)
		if xbmcHost, _ := xbmc.GetLocalXBMCHost(); xbmcHost != nil {
			xbmcHost.Notify("Elementum", err.Error(), config.AddonIcon())
		}
	}

	return err
}

func payloadToParams(payload SearchPayload, page int) napping.Params {
	params := napping.Params{
		"query":   payload.Query,
		"imdb_id": payload.IMDBId,
		"tmdb_id": payload.TMDBId,
		"year":    strconv.Itoa(payload.Year),

		"season_number":  strconv.Itoa(payload.Season),
		"episode_number": strconv.Itoa(payload.Episode),

		"languages": payload.Languages,
		"page":      strconv.Itoa(page),
	}

	// Remove map items that has default values. API does not like having empty params.
	for k, v := range params {
		if v == "" || v == "0" {
			delete(params, k)
		}
	}

	return params
}

func SearchSubtitles(payloads []SearchPayload) (results []SearchResponseData, err error) {
	if err := ensureLogin(); err != nil {
		return nil, err
	}

	for _, payload := range payloads {
		log.Debugf("Searching subtitles with payload: %+v", payload)

		counter := 1
		maxCounter := 1
		for counter <= maxCounter {
			resp := &SearchResponse{}
			req := &reqapi.Request{
				API:         reqapi.OpenSubtitlesAPI,
				Method:      "GET",
				URL:         "subtitles",
				Header:      GetAuthenticatedHeader(),
				Params:      payloadToParams(payload, counter).AsUrlValues(),
				Description: "Search Subtitles",

				Result: resp,
			}

			err := req.Do()
			if err != nil {
				log.Warningf("Error searching for subtitles: %s", err)
				return results, err
			}

			results = append(results, resp.Data...)
			maxCounter = resp.TotalPages
			counter++
		}
	}

	return results, err
}

func DownloadSubtitles(id string) (resp DownloadResponse, err error) {
	if err = ensureLogin(); err != nil {
		return
	}

	req := &reqapi.Request{
		API:         reqapi.OpenSubtitlesAPI,
		Method:      "POST",
		URL:         "download",
		Header:      GetAuthenticatedHeader(),
		Payload:     bytes.NewBufferString(fmt.Sprintf(`{ "file_id": %s }`, id)),
		Description: "Download Subtitles",

		Result: &resp,
	}

	if err = req.Do(); err != nil {
		log.Warningf("Error downloading subtitles: %s", err)
	}

	return
}

func DoSearch(payloads []SearchPayload, preferredLanguage string) ([]SearchResponseData, error) {
	results, err := SearchSubtitles(payloads)
	if err != nil {
		return nil, err
	}

	if preferredLanguage != "" {
		sort.Slice(results, func(i, j int) bool {
			id := strings.ToLower(results[i].Attributes.Language) == preferredLanguage
			return id
		})
	}

	return results, nil
}

func DoDownload(id string) (*os.File, string, string, error) {
	downloadResp, err := DownloadSubtitles(id)
	if err != nil {
		return nil, "", "", err
	}

	resp, err := proxy.GetClient().Get(downloadResp.Link)
	if err != nil || resp == nil {
		return nil, "", "", err
	}
	defer resp.Body.Close()

	subtitlesPath := filepath.Join(config.Get().DownloadPath, "Subtitles")
	if config.Get().DownloadPath == "." {
		subtitlesPath = filepath.Join(config.Get().TemporaryPath, "Subtitles")
	}
	if _, errStat := os.Stat(subtitlesPath); os.IsNotExist(errStat) {
		if errMk := os.Mkdir(subtitlesPath, 0755); errMk != nil {
			return nil, "", "", fmt.Errorf("Unable to create Subtitles folder")
		}
	}

	outPath := filepath.Join(subtitlesPath, downloadResp.FileName)
	outFile, err := os.Create(outPath)
	if err != nil {
		return nil, "", "", err
	}
	defer outFile.Close()

	io.Copy(outFile, resp.Body)

	return outFile, downloadResp.FileName, outPath, nil
}

// GetPayloads ...
func GetPayloads(xbmcHost *xbmc.XBMCHost, searchString string, languages []string, preferredLanguage string, showID int, playingFile string) ([]SearchPayload, string) {
	log.Debugf("GetPayloads: %s; %#v; %s; %s", searchString, languages, preferredLanguage, playingFile)

	// First of all, we get Subtitles language settings from Kodi
	// (there is a separate setting for that) in Player settings.
	if !config.Get().OSDBAutoLanguage && config.Get().OSDBLanguage != "" {
		languages = []string{config.Get().OSDBLanguage}
	}

	// If there is preferred language - we should use it
	if preferredLanguage != "" && preferredLanguage != "Unknown" && !contains(languages, preferredLanguage) {
		languages = append([]string{preferredLanguage}, languages...)
		preferredLanguage = strings.ToLower(preferredLanguage)
	} else {
		preferredLanguage = ""
	}

	labels := xbmcHost.InfoLabels(
		"VideoPlayer.Title",
		"VideoPlayer.OriginalTitle",
		"VideoPlayer.Year",
		"VideoPlayer.TVShowTitle",
		"VideoPlayer.Season",
		"VideoPlayer.Episode",
		"VideoPlayer.IMDBNumber",
	)
	log.Debugf("Fetched VideoPlayer labels: %#v", labels)

	for i, lang := range languages {
		if lang == "Portuguese (Brazil)" {
			languages[i] = "pt-br"
		} else {
			languages[i] = xbmcHost.ConvertLanguage(lang, xbmc.Iso639_1)
		}
	}

	payloads := []SearchPayload{}
	if searchString != "" {
		payloads = append(payloads, SearchPayload{
			Query:     searchString,
			Languages: strings.Join(languages, ","),
		})
	} else if labels != nil {
		// If player ListItem has IMDBNumber specified - we try to get TMDB item from it.
		// If not - we can use localized show/movie name - which is not always found on OSDB.
		if strings.HasPrefix(labels["VideoPlayer.IMDBNumber"], "tt") {
			if labels["VideoPlayer.TVShowTitle"] != "" {
				r := tmdb.Find(labels["VideoPlayer.IMDBNumber"], "imdb_id")
				if r != nil && len(r.TVResults) > 0 {
					labels["VideoPlayer.TVShowTitle"] = r.TVResults[0].OriginalName
				}
			} else {
				r := tmdb.Find(labels["VideoPlayer.IMDBNumber"], "imdb_id")
				if r != nil && len(r.MovieResults) > 0 {
					labels["VideoPlayer.OriginalTitle"] = r.MovieResults[0].OriginalTitle
				}
			}
		}

		var err error
		if showID != 0 {
			err = appendEpisodePayloads(showID, labels, &payloads)
		} else {
			err = appendMoviePayloads(labels, &payloads)
		}

		if err != nil {
			if !strings.HasPrefix(playingFile, "http://") && !strings.HasPrefix(playingFile, "https://") {
				appendLocalFilePayloads(playingFile, &payloads)
			} else {
				appendRemoteFilePayloads(playingFile, &payloads)
			}
		}
	}

	for i, payload := range payloads {
		payload.Languages = strings.Join(languages, ",")
		payloads[i] = payload
	}

	return payloads, xbmcHost.ConvertLanguage(preferredLanguage, xbmc.Iso639_1)
}

func appendLocalFilePayloads(playingFile string, payloads *[]SearchPayload) error {
	file, err := os.Open(playingFile)
	if err != nil {
		log.Debug(err)
		return err
	}
	defer file.Close()

	hashPayload := SearchPayload{}
	if h, err := HashFile(file); err == nil {
		hashPayload.Hash = h
	}
	hashPayload.Query = strings.Replace(filepath.Base(playingFile), filepath.Ext(playingFile), "", -1)
	if hashPayload.Query != "" {
		*payloads = append(*payloads, hashPayload)
		return nil
	}

	return fmt.Errorf("Cannot collect local information")
}

func appendRemoteFilePayloads(playingFile string, payloads *[]SearchPayload) error {
	u, _ := url.Parse(playingFile)
	f := path.Base(u.Path)
	q := strings.Replace(filepath.Base(f), filepath.Ext(f), "", -1)

	if q != "" {
		*payloads = append(*payloads, SearchPayload{Query: q})
		return nil
	}

	return fmt.Errorf("Cannot collect local information")
}

func appendMoviePayloads(labels map[string]string, payloads *[]SearchPayload) error {
	title := labels["VideoPlayer.OriginalTitle"]
	if title == "" {
		title = labels["VideoPlayer.Title"]
	}
	imdb := labels["VideoPlayer.IMDBNumber"]
	if imdb != "" {
		imdb = imdb[2:]
	}

	if title != "" {
		year, _ := strconv.Atoi(labels["VideoPlayer.Year"])

		*payloads = append(*payloads, SearchPayload{
			Type: "movie",

			Query:  fmt.Sprintf("%s %d", title, year),
			Year:   year,
			IMDBId: imdb,
		})
		return nil
	}

	return fmt.Errorf("Cannot collect movie information")
}

func appendEpisodePayloads(showID int, labels map[string]string, payloads *[]SearchPayload) error {
	season := -1
	if labels["VideoPlayer.Season"] != "" {
		if s, err := strconv.Atoi(labels["VideoPlayer.Season"]); err == nil {
			season = s
		}
	}
	episode := -1
	if labels["VideoPlayer.Episode"] != "" {
		if e, err := strconv.Atoi(labels["VideoPlayer.Episode"]); err == nil {
			episode = e
		}
	}

	if season >= 0 && episode > 0 {
		title := labels["VideoPlayer.TVShowTitle"]
		if showID != 0 {
			// Trying to get Original name of the show, otherwise we will likely fail to find anything.
			show := tmdb.GetShow(showID, config.Get().Language)
			if show != nil {
				title = show.OriginalName
			}
		}

		searchString := fmt.Sprintf("%s S%02dE%02d", title, season, episode)
		*payloads = append(*payloads, SearchPayload{
			Type: "episode",

			Query:   searchString,
			Season:  season,
			Episode: episode,
		})
		return nil
	}

	return fmt.Errorf("Cannot collect episode information")
}

func contains(s []string, e string) bool {
	for _, a := range s {
		if a == e {
			return true
		}
	}
	return false
}

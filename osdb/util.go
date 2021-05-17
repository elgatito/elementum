package osdb

import (
	"compress/gzip"
	"fmt"
	"io"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/elgatito/elementum/config"
	"github.com/elgatito/elementum/proxy"
	"github.com/elgatito/elementum/tmdb"
	"github.com/elgatito/elementum/xbmc"
)

// DoSearch ...
func DoSearch(payloads []SearchPayload, preferredLanguage string) (Subtitles, error) {
	client, err := NewClient()
	if err != nil {
		return nil, err
	}
	if err := client.LogIn(config.Get().OSDBUser, config.Get().OSDBPass, config.Get().OSDBLanguage); err != nil {
		return nil, err
	}

	results, err := client.SearchSubtitles(payloads)
	if err != nil {
		return nil, err
	}

	// If needed - try to manually sort items
	if preferredLanguage != "" {
		sort.Slice(results, func(i, j int) bool {
			id := strings.ToLower(results[i].LanguageName) == preferredLanguage
			return id
		})
	}

	return results, nil
}

// DoDownload ...
func DoDownload(file, dl string) (*os.File, string, error) {
	resp, err := proxy.GetClient().Get(dl)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()

	reader, err := gzip.NewReader(resp.Body)
	if err != nil {
		return nil, "", err
	}
	defer reader.Close()

	subtitlesPath := filepath.Join(config.Get().DownloadPath, "Subtitles")
	if config.Get().DownloadPath == "." {
		subtitlesPath = filepath.Join(config.Get().TemporaryPath, "Subtitles")
	}
	if _, errStat := os.Stat(subtitlesPath); os.IsNotExist(errStat) {
		if errMk := os.Mkdir(subtitlesPath, 0755); errMk != nil {
			return nil, "", fmt.Errorf("Unable to create Subtitles folder")
		}
	}

	outFile, err := os.Create(filepath.Join(subtitlesPath, file))
	if err != nil {
		return nil, "", err
	}
	defer outFile.Close()

	io.Copy(outFile, reader)

	return outFile, filepath.Join(subtitlesPath, file), nil
}

// GetPayloads ...
func GetPayloads(searchString string, languages []string, preferredLanguage string, showID int, playingFile string) ([]SearchPayload, string) {
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

	labels := xbmc.InfoLabels(
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
			languages[i] = "pob"
		} else {
			isoLang := xbmc.ConvertLanguage(lang, xbmc.Iso639_2)
			if isoLang == "gre" {
				isoLang = "ell"
			}
			languages[i] = isoLang
		}
	}

	payloads := []SearchPayload{}
	if searchString != "" {
		payloads = append(payloads, SearchPayload{
			Query:     searchString,
			Languages: strings.Join(languages, ","),
		})
	} else {
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
			if strings.HasPrefix(playingFile, "http://") == false && strings.HasPrefix(playingFile, "https://") == false {
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

	return payloads, preferredLanguage
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
	if s, err := file.Stat(); err == nil {
		hashPayload.Size = s.Size()
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

	if title != "" {
		*payloads = append(*payloads, SearchPayload{
			Query: fmt.Sprintf("%s %s", title, labels["VideoPlayer.Year"]),
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
			Query: searchString,
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

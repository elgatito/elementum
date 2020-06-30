package xbmc

import (
	"strings"
	"time"

	"github.com/anacrolix/missinggo/perf"
)

// UpdateAddonRepos ...
func UpdateAddonRepos() (retVal string) {
	executeJSONRPCEx("UpdateAddonRepos", &retVal, nil)
	return
}

// ResetRPC ...
func ResetRPC() (retVal string) {
	executeJSONRPCEx("Reset", &retVal, nil)
	return
}

// Refresh ...
func Refresh() (retVal string) {
	executeJSONRPCEx("Refresh", &retVal, nil)
	return
}

// VideoLibraryScan ...
func VideoLibraryScan() (retVal string) {
	executeJSONRPC("VideoLibrary.Scan", &retVal, nil)
	return
}

// VideoLibraryScanDirectory ...
func VideoLibraryScanDirectory(directory string, showDialogs bool) (retVal string) {
	executeJSONRPC("VideoLibrary.Scan", &retVal, Args{directory, showDialogs})
	return
}

// VideoLibraryClean ...
func VideoLibraryClean() (retVal string) {
	executeJSONRPC("VideoLibrary.Clean", &retVal, nil)
	return
}

// VideoLibraryGetMovies ...
func VideoLibraryGetMovies() (movies *VideoLibraryMovies, err error) {
	defer perf.ScopeTimer()()

	list := []interface{}{
		"imdbnumber",
		"playcount",
		"file",
		"dateadded",
		"resume",
	}
	if KodiVersion > 16 {
		list = append(list, "uniqueid", "year")
	}
	params := map[string]interface{}{"properties": list}

	for tries := 1; tries <= 3; tries++ {
		var err error

		err = executeJSONRPCO("VideoLibrary.GetMovies", &movies, params)
		if movies == nil || (err != nil && !strings.Contains(err.Error(), "invalid error")) {
			time.Sleep(time.Duration(tries*2) * time.Second)
			continue
		}

		break
	}

	return
}

// VideoLibraryGetElementumMovies ...
func VideoLibraryGetElementumMovies() (movies *VideoLibraryMovies, err error) {
	defer perf.ScopeTimer()()

	list := []interface{}{
		"imdbnumber",
		"playcount",
		"file",
		"dateadded",
		"resume",
	}
	sorts := map[string]interface{}{
		"method": "title",
	}

	if KodiVersion > 16 {
		list = append(list, "uniqueid", "year")
	}
	params := map[string]interface{}{
		"properties": list,
		"sort":       sorts,
	}
	err = executeJSONRPCO("VideoLibrary.GetMovies", &movies, params)
	if err != nil {
		log.Errorf("Error getting tvshows: %#v", err)
		return
	}

	if movies != nil && movies.Limits != nil && movies.Limits.Total == 0 {
		return
	}

	total := 0
	filteredMovies := &VideoLibraryMovies{
		Movies: []*VideoLibraryMovieItem{},
		Limits: &VideoLibraryLimits{},
	}
	for _, s := range movies.Movies {
		if s != nil && s.UniqueIDs.Elementum != "" {
			filteredMovies.Movies = append(filteredMovies.Movies, s)
			total++
		}
	}

	filteredMovies.Limits.Total = total
	return filteredMovies, nil
}

// PlayerGetActive ...
func PlayerGetActive() int {
	params := map[string]interface{}{}
	items := ActivePlayers{}
	executeJSONRPCO("Player.GetActivePlayers", &items, params)
	for _, v := range items {
		if v.Type == "video" {
			return v.ID
		}
	}

	return -1
}

// PlayerGetItem ...
func PlayerGetItem(playerid int) (item *PlayerItemInfo) {
	params := map[string]interface{}{
		"playerid": playerid,
	}
	executeJSONRPCO("Player.GetItem", &item, params)
	return
}

// VideoLibraryGetShows ...
func VideoLibraryGetShows() (shows *VideoLibraryShows, err error) {
	defer perf.ScopeTimer()()

	list := []interface{}{
		"imdbnumber",
		"episode",
		"dateadded",
		"playcount",
	}
	if KodiVersion > 16 {
		list = append(list, "uniqueid", "year")
	}
	params := map[string]interface{}{"properties": list}

	for tries := 1; tries <= 3; tries++ {
		err = executeJSONRPCO("VideoLibrary.GetTVShows", &shows, params)
		if err != nil {
			time.Sleep(time.Duration(tries*500) * time.Millisecond)
			continue
		}
		break
	}

	return
}

// VideoLibraryGetElementumShows returns shows added by Elementum
func VideoLibraryGetElementumShows() (shows *VideoLibraryShows, err error) {
	defer perf.ScopeTimer()()

	list := []interface{}{
		"imdbnumber",
		"episode",
		"dateadded",
		"playcount",
	}
	sorts := map[string]interface{}{
		"method": "tvshowtitle",
	}

	if KodiVersion > 16 {
		list = append(list, "uniqueid", "year")
	}
	params := map[string]interface{}{
		"properties": list,
		"sort":       sorts,
	}
	err = executeJSONRPCO("VideoLibrary.GetTVShows", &shows, params)
	if err != nil {
		log.Errorf("Error getting tvshows: %#v", err)
		return
	}

	if shows != nil && shows.Limits != nil && shows.Limits.Total == 0 {
		return
	}

	total := 0
	filteredShows := &VideoLibraryShows{
		Shows:  []*VideoLibraryShowItem{},
		Limits: &VideoLibraryLimits{},
	}
	for _, s := range shows.Shows {
		if s != nil && s.UniqueIDs.Elementum != "" {
			filteredShows.Shows = append(filteredShows.Shows, s)
			total++
		}
	}

	filteredShows.Limits.Total = total
	return filteredShows, nil
}

// VideoLibraryGetSeasons ...
func VideoLibraryGetSeasons(tvshowID int) (seasons *VideoLibrarySeasons, err error) {
	defer perf.ScopeTimer()()

	params := map[string]interface{}{"tvshowid": tvshowID, "properties": []interface{}{
		"tvshowid",
		"season",
		"episode",
		"playcount",
	}}
	err = executeJSONRPCO("VideoLibrary.GetSeasons", &seasons, params)
	if err != nil {
		log.Errorf("Error getting seasons: %#v", err)
	}
	return
}

// VideoLibraryGetAllSeasons ...
func VideoLibraryGetAllSeasons(shows []int) (seasons *VideoLibrarySeasons, err error) {
	defer perf.ScopeTimer()()

	if KodiVersion > 16 {
		params := map[string]interface{}{"properties": []interface{}{
			"tvshowid",
			"season",
			"episode",
			"playcount",
		}}

		for tries := 1; tries <= 3; tries++ {
			err = executeJSONRPCO("VideoLibrary.GetSeasons", &seasons, params)
			if seasons == nil || err != nil {
				time.Sleep(time.Duration(tries*500) * time.Millisecond)
				continue
			}
			break
		}

		return
	}

	seasons = &VideoLibrarySeasons{}
	for _, s := range shows {
		res, err := VideoLibraryGetSeasons(s)
		if res != nil && res.Seasons != nil && err == nil {
			seasons.Seasons = append(seasons.Seasons, res.Seasons...)
		}
	}

	return
}

// VideoLibraryGetEpisodes ...
func VideoLibraryGetEpisodes(tvshowID int) (episodes *VideoLibraryEpisodes, err error) {
	defer perf.ScopeTimer()()

	params := map[string]interface{}{"tvshowid": tvshowID, "properties": []interface{}{
		"tvshowid",
		"uniqueid",
		"season",
		"episode",
		"playcount",
		"file",
		"dateadded",
		"resume",
	}}
	err = executeJSONRPCO("VideoLibrary.GetEpisodes", &episodes, params)
	if err != nil {
		log.Errorf("Error getting episodes: %#v", err)
	}
	return
}

// VideoLibraryGetAllEpisodes ...
func VideoLibraryGetAllEpisodes(shows []int) (episodes *VideoLibraryEpisodes, err error) {
	defer perf.ScopeTimer()()

	if len(shows) == 0 {
		return episodes, nil
	}

	episodes = &VideoLibraryEpisodes{}
	for _, showID := range shows {
		if es, err := VideoLibraryGetEpisodes(showID); err == nil && es != nil && len(es.Episodes) != 0 {
			episodes.Episodes = append(episodes.Episodes, es.Episodes...)
		}
	}

	return episodes, nil
}

// SetMovieWatched ...
func SetMovieWatched(movieID int, playcount int, position int, total int) (ret string) {
	params := map[string]interface{}{
		"movieid":   movieID,
		"playcount": playcount,
		"resume": map[string]interface{}{
			"position": position,
			"total":    total,
		},
		"lastplayed": time.Now().Format("2006-01-02 15:04:05"),
	}
	executeJSONRPCO("VideoLibrary.SetMovieDetails", &ret, params)
	return
}

// SetMovieWatchedWithDate ...
func SetMovieWatchedWithDate(movieID int, playcount int, position int, total int, dt time.Time) (ret string) {
	params := map[string]interface{}{
		"movieid":   movieID,
		"playcount": playcount,
		"resume": map[string]interface{}{
			"position": position,
			"total":    total,
		},
		"lastplayed": dt.Format("2006-01-02 15:04:05"),
	}
	executeJSONRPCO("VideoLibrary.SetMovieDetails", &ret, params)
	return
}

// SetMovieProgress ...
func SetMovieProgress(movieID int, position int, total int) (ret string) {
	params := map[string]interface{}{
		"movieid": movieID,
		"resume": map[string]interface{}{
			"position": position,
			"total":    total,
		},
		"lastplayed": time.Now().Format("2006-01-02 15:04:05"),
	}
	executeJSONRPCO("VideoLibrary.SetMovieDetails", &ret, params)
	return
}

// SetMovieProgressWithDate ...
func SetMovieProgressWithDate(movieID int, position int, total int, dt time.Time) (ret string) {
	params := map[string]interface{}{
		"movieid": movieID,
		"resume": map[string]interface{}{
			"position": position,
			"total":    total,
		},
		"lastplayed": dt.Format("2006-01-02 15:04:05"),
	}
	executeJSONRPCO("VideoLibrary.SetMovieDetails", &ret, params)
	return
}

// SetMoviePlaycount ...
func SetMoviePlaycount(movieID int, playcount int) (ret string) {
	params := map[string]interface{}{
		"movieid":    movieID,
		"playcount":  playcount,
		"lastplayed": time.Now().Format("2006-01-02 15:04:05"),
	}
	executeJSONRPCO("VideoLibrary.SetMovieDetails", &ret, params)
	return
}

// SetShowWatched ...
func SetShowWatched(showID int, playcount int) (ret string) {
	params := map[string]interface{}{
		"tvshowid":  showID,
		"playcount": playcount,
	}
	executeJSONRPCO("VideoLibrary.SetTVShowDetails", &ret, params)
	return
}

// SetShowWatchedWithDate ...
func SetShowWatchedWithDate(showID int, playcount int, dt time.Time) (ret string) {
	params := map[string]interface{}{
		"tvshowid":   showID,
		"playcount":  playcount,
		"lastplayed": dt.Format("2006-01-02 15:04:05"),
	}
	executeJSONRPCO("VideoLibrary.SetTVShowDetails", &ret, params)
	return
}

// SetEpisodeWatched ...
func SetEpisodeWatched(episodeID int, playcount int, position int, total int) (ret string) {
	params := map[string]interface{}{
		"episodeid": episodeID,
		"playcount": playcount,
		"resume": map[string]interface{}{
			"position": position,
			"total":    total,
		},
		"lastplayed": time.Now().Format("2006-01-02 15:04:05"),
	}
	executeJSONRPCO("VideoLibrary.SetEpisodeDetails", &ret, params)
	return
}

// SetEpisodeWatchedWithDate ...
func SetEpisodeWatchedWithDate(episodeID int, playcount int, position int, total int, dt time.Time) (ret string) {
	params := map[string]interface{}{
		"episodeid": episodeID,
		"playcount": playcount,
		"resume": map[string]interface{}{
			"position": position,
			"total":    total,
		},
		"lastplayed": dt.Format("2006-01-02 15:04:05"),
	}
	executeJSONRPCO("VideoLibrary.SetEpisodeDetails", &ret, params)
	return
}

// SetEpisodeProgress ...
func SetEpisodeProgress(episodeID int, position int, total int) (ret string) {
	params := map[string]interface{}{
		"episodeid": episodeID,
		"resume": map[string]interface{}{
			"position": position,
			"total":    total,
		},
		"lastplayed": time.Now().Format("2006-01-02 15:04:05"),
	}
	executeJSONRPCO("VideoLibrary.SetEpisodeDetails", &ret, params)
	return
}

// SetEpisodeProgressWithDate ...
func SetEpisodeProgressWithDate(episodeID int, position int, total int, dt time.Time) (ret string) {
	params := map[string]interface{}{
		"episodeid": episodeID,
		"resume": map[string]interface{}{
			"position": position,
			"total":    total,
		},
		"lastplayed": dt.Format("2006-01-02 15:04:05"),
	}
	executeJSONRPCO("VideoLibrary.SetEpisodeDetails", &ret, params)
	return
}

// SetEpisodePlaycount ...
func SetEpisodePlaycount(episodeID int, playcount int) (ret string) {
	params := map[string]interface{}{
		"episodeid":  episodeID,
		"playcount":  playcount,
		"lastplayed": time.Now().Format("2006-01-02 15:04:05"),
	}
	executeJSONRPCO("VideoLibrary.SetEpisodeDetails", &ret, params)
	return
}

// SetFileWatched ...
func SetFileWatched(file string, position int, total int) (ret string) {
	params := map[string]interface{}{
		"file":      file,
		"media":     "video",
		"playcount": 0,
		"resume": map[string]interface{}{
			"position": position,
			"total":    total,
		},
		"lastplayed": time.Now().Format("2006-01-02 15:04:05"),
	}
	executeJSONRPCO("VideoLibrary.SetFileDetails", &ret, params)
	return
}

// TranslatePath ...
func TranslatePath(path string) (retVal string) {
	executeJSONRPCEx("TranslatePath", &retVal, Args{path})
	return
}

// UpdatePath ...
func UpdatePath(path string) (retVal string) {
	executeJSONRPCEx("Update", &retVal, Args{path})
	return
}

// PlaylistLeft ...
func PlaylistLeft() (retVal int) {
	executeJSONRPCEx("Playlist_Left", &retVal, Args{})
	return
}

// PlaylistSize ...
func PlaylistSize() (retVal int) {
	executeJSONRPCEx("Playlist_Size", &retVal, Args{})
	return
}

// PlaylistClear ...
func PlaylistClear() (retVal int) {
	executeJSONRPCEx("Playlist_Clear", &retVal, Args{})
	return
}

// PlayURL ...
func PlayURL(url string) {
	retVal := ""
	executeJSONRPCEx("Player_Open", &retVal, Args{url})
}

// PlayURLWithLabels ...
func PlayURLWithLabels(url string, listItem *ListItem) {
	retVal := ""
	go executeJSONRPCEx("Player_Open_With_Labels", &retVal, Args{url, listItem.Info})
}

// PlayURLWithTimeout ...
func PlayURLWithTimeout(url string) {
	retVal := ""
	go executeJSONRPCEx("Player_Open_With_Timeout", &retVal, Args{url})
}

const (
	// Iso639_1 ...
	Iso639_1 = iota
	// Iso639_2 ...
	Iso639_2
	// EnglishName ...
	EnglishName
)

// ConvertLanguage ...
func ConvertLanguage(language string, format int) string {
	retVal := ""
	executeJSONRPCEx("ConvertLanguage", &retVal, Args{language, format})
	return retVal
}

// FilesGetSources ...
func FilesGetSources() *FileSources {
	params := map[string]interface{}{
		"media": "video",
	}
	items := &FileSources{}
	executeJSONRPCO("Files.GetSources", items, params)

	return items
}

// GetLanguage ...
func GetLanguage(format int) string {
	retVal := ""
	executeJSONRPCEx("GetLanguage", &retVal, Args{format})
	return retVal
}

// GetLanguageISO639_1 ...
func GetLanguageISO639_1() string {
	language := GetLanguage(Iso639_1)
	if language == "" {
		switch GetLanguage(EnglishName) {
		case "Chinese (Simple)":
			return "zh"
		case "Chinese (Traditional)":
			return "zh"
		case "English (Australia)":
			return "en"
		case "English (New Zealand)":
			return "en"
		case "English (US)":
			return "en"
		case "French (Canada)":
			return "fr"
		case "Hindi (Devanagiri)":
			return "hi"
		case "Mongolian (Mongolia)":
			return "mn"
		case "Persian (Iran)":
			return "fa"
		case "Portuguese (Brazil)":
			return "pt"
		case "Serbian (Cyrillic)":
			return "sr"
		case "Spanish (Argentina)":
			return "es"
		case "Spanish (Mexico)":
			return "es"
		case "Tamil (India)":
			return "ta"
		default:
			return "en"
		}
	}
	return language
}

// SettingsGetSettingValue ...
func SettingsGetSettingValue(setting string) string {
	params := map[string]interface{}{
		"setting": setting,
	}
	resp := SettingValue{}

	executeJSONRPCO("Settings.GetSettingValue", &resp, params)
	return resp.Value
}

package xbmc

import (
	"strings"
	"time"

	"github.com/anacrolix/missinggo/perf"
)

func (h *XBMCHost) IsLocal() bool {
	return h.Host == "127.0.0.1" || strings.Contains(h.Host, "::1")
}

// UpdateAddonRepos ...
func (h *XBMCHost) UpdateAddonRepos() (retVal string) {
	h.executeJSONRPCEx("UpdateAddonRepos", &retVal, nil)
	return
}

// ResetRPC ...
func (h *XBMCHost) ResetRPC() (retVal string) {
	h.executeJSONRPCEx("Reset", &retVal, nil)
	return
}

// Refresh ...
func (h *XBMCHost) Refresh() (retVal string) {
	h.executeJSONRPCEx("Refresh", &retVal, nil)
	return
}

// VideoLibraryScan ...
func (h *XBMCHost) VideoLibraryScan() (retVal string) {
	h.executeJSONRPC("VideoLibrary.Scan", &retVal, nil)
	return
}

// VideoLibraryScanDirectory ...
func (h *XBMCHost) VideoLibraryScanDirectory(directory string, showDialogs bool) (retVal string) {
	h.executeJSONRPC("VideoLibrary.Scan", &retVal, Args{directory, showDialogs})
	return
}

// VideoLibraryClean ...
func (h *XBMCHost) VideoLibraryClean() (retVal string) {
	h.executeJSONRPC("VideoLibrary.Clean", &retVal, nil)
	return
}

// VideoLibraryCleanDirectory initiates Kodi library cleanup for specific removed directory
func (h *XBMCHost) VideoLibraryCleanDirectory(directory string, content string, showDialogs bool) (retVal string) {
	params := map[string]interface{}{
		"showdialogs": showDialogs,
		"directory":   directory,
		"content":     content,
	}
	h.executeJSONRPCO("VideoLibrary.Clean", &retVal, params)
	return
}

// VideoLibraryGetMovies ...
func (h *XBMCHost) VideoLibraryGetMovies() (movies *VideoLibraryMovies, err error) {
	defer perf.ScopeTimer()()

	list := []interface{}{
		"imdbnumber",
		"playcount",
		"file",
		"dateadded",
		"resume",
	}
	if KodiVersion > 16 {
		list = append(list, "uniqueid", "year", "lastplayed")
	}
	params := map[string]interface{}{"properties": list}

	for tries := 1; tries <= 3; tries++ {
		err = h.executeJSONRPCO("VideoLibrary.GetMovies", &movies, params)
		if movies == nil || (err != nil && !strings.Contains(err.Error(), "invalid error")) {
			time.Sleep(time.Duration(tries*2) * time.Second)
			continue
		}

		break
	}

	return
}

// VideoLibraryGetElementumMovies ...
func (h *XBMCHost) VideoLibraryGetElementumMovies() (movies *VideoLibraryMovies, err error) {
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
		list = append(list, "uniqueid", "year", "lastplayed")
	}
	params := map[string]interface{}{
		"properties": list,
		"sort":       sorts,
	}
	err = h.executeJSONRPCO("VideoLibrary.GetMovies", &movies, params)
	if err != nil {
		log.Errorf("Error getting tvshows: %#v", err)
		return
	}

	if movies == nil || movies.Limits == nil {
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

// VideoLibraryRemoveMovie ...
func (h *XBMCHost) VideoLibraryRemoveMovie(id int) (retVal string) {
	h.executeJSONRPC("VideoLibrary.RemoveMovie", &retVal, Args{id})
	return
}

// VideoLibraryRemoveTVShow ...
func (h *XBMCHost) VideoLibraryRemoveTVShow(id int) (retVal string) {
	h.executeJSONRPC("VideoLibrary.RemoveTVShow", &retVal, Args{id})
	return
}

// VideoLibraryRemoveEpisode ...
func (h *XBMCHost) VideoLibraryRemoveEpisode(id int) (retVal string) {
	h.executeJSONRPC("VideoLibrary.RemoveEpisode", &retVal, Args{id})
	return
}

// PlayerGetActive ...
func (h *XBMCHost) PlayerGetActive() int {
	params := map[string]interface{}{}
	items := ActivePlayers{}
	h.executeJSONRPCO("Player.GetActivePlayers", &items, params)
	for _, v := range items {
		if v.Type == "video" {
			return v.ID
		}
	}

	return -1
}

// PlayerGetItem ...
func (h *XBMCHost) PlayerGetItem(playerid int) (item *PlayerItemInfo) {
	params := map[string]interface{}{
		"playerid": playerid,
	}
	h.executeJSONRPCO("Player.GetItem", &item, params)
	return
}

// VideoLibraryGetShows ...
func (h *XBMCHost) VideoLibraryGetShows() (shows *VideoLibraryShows, err error) {
	defer perf.ScopeTimer()()

	list := []interface{}{
		"imdbnumber",
		"episode",
		"dateadded",
		"playcount",
	}
	if KodiVersion > 16 {
		list = append(list, "uniqueid", "year", "lastplayed")
	}
	params := map[string]interface{}{"properties": list}

	for tries := 1; tries <= 3; tries++ {
		err = h.executeJSONRPCO("VideoLibrary.GetTVShows", &shows, params)
		if err != nil {
			time.Sleep(time.Duration(tries*500) * time.Millisecond)
			continue
		}
		break
	}

	return
}

// VideoLibraryGetElementumShows returns shows added by Elementum
func (h *XBMCHost) VideoLibraryGetElementumShows() (shows *VideoLibraryShows, err error) {
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
		list = append(list, "uniqueid", "year", "lastplayed")
	}
	params := map[string]interface{}{
		"properties": list,
		"sort":       sorts,
	}
	err = h.executeJSONRPCO("VideoLibrary.GetTVShows", &shows, params)
	if err != nil {
		log.Errorf("Error getting tvshows: %#v", err)
		return
	}

	if shows == nil || (shows.Limits != nil && shows.Limits.Total == 0) {
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
func (h *XBMCHost) VideoLibraryGetSeasons(tvshowID int) (seasons *VideoLibrarySeasons, err error) {
	defer perf.ScopeTimer()()

	params := map[string]interface{}{"tvshowid": tvshowID, "properties": []interface{}{
		"tvshowid",
		"season",
		"episode",
		"playcount",
	}}
	err = h.executeJSONRPCO("VideoLibrary.GetSeasons", &seasons, params)
	if err != nil {
		log.Errorf("Error getting seasons: %#v", err)
	}
	return
}

// VideoLibraryGetAllSeasons ...
func (h *XBMCHost) VideoLibraryGetAllSeasons(shows []int) (seasons *VideoLibrarySeasons, err error) {
	defer perf.ScopeTimer()()

	if KodiVersion > 16 {
		params := map[string]interface{}{"properties": []interface{}{
			"tvshowid",
			"season",
			"episode",
			"playcount",
		}}

		for tries := 1; tries <= 3; tries++ {
			err = h.executeJSONRPCO("VideoLibrary.GetSeasons", &seasons, params)
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
		res, err := h.VideoLibraryGetSeasons(s)
		if res != nil && res.Seasons != nil && err == nil {
			seasons.Seasons = append(seasons.Seasons, res.Seasons...)
		}
	}

	return
}

// VideoLibraryGetEpisodes ...
func (h *XBMCHost) VideoLibraryGetEpisodes(tvshowID int) (episodes *VideoLibraryEpisodes, err error) {
	defer perf.ScopeTimer()()

	list := []interface{}{
		"tvshowid",
		"uniqueid",
		"season",
		"episode",
		"playcount",
		"file",
		"dateadded",
		"resume",
	}
	if KodiVersion > 16 {
		list = append(list, "lastplayed")
	}

	params := map[string]interface{}{"tvshowid": tvshowID, "properties": list}
	err = h.executeJSONRPCO("VideoLibrary.GetEpisodes", &episodes, params)
	if err != nil {
		log.Errorf("Error getting episodes: %#v", err)
	}
	return
}

// VideoLibraryGetAllEpisodes ...
func (h *XBMCHost) VideoLibraryGetAllEpisodes(shows []int) (episodes *VideoLibraryEpisodes, err error) {
	defer perf.ScopeTimer()()

	if len(shows) == 0 {
		return episodes, nil
	}

	episodes = &VideoLibraryEpisodes{}
	for _, showID := range shows {
		if es, err := h.VideoLibraryGetEpisodes(showID); err == nil && es != nil && len(es.Episodes) != 0 {
			episodes.Episodes = append(episodes.Episodes, es.Episodes...)
		}
	}

	return episodes, nil
}

// SetMovieWatched ...
func (h *XBMCHost) SetMovieWatched(movieID int, playcount int, position int, total int) (ret string) {
	params := map[string]interface{}{
		"movieid":   movieID,
		"playcount": playcount,
		"resume": map[string]interface{}{
			"position": position,
			"total":    total,
		},
		"lastplayed": time.Now().Format("2006-01-02 15:04:05"),
	}
	h.executeJSONRPCO("VideoLibrary.SetMovieDetails", &ret, params)
	return
}

// SetMovieWatchedWithDate ...
func (h *XBMCHost) SetMovieWatchedWithDate(movieID int, playcount int, position int, total int, dt time.Time) (ret string) {
	params := map[string]interface{}{
		"movieid":   movieID,
		"playcount": playcount,
		"resume": map[string]interface{}{
			"position": position,
			"total":    total,
		},
		"lastplayed": dt.Format("2006-01-02 15:04:05"),
	}
	h.executeJSONRPCO("VideoLibrary.SetMovieDetails", &ret, params)
	return
}

// SetMovieProgress ...
func (h *XBMCHost) SetMovieProgress(movieID int, position int, total int) (ret string) {
	params := map[string]interface{}{
		"movieid": movieID,
		"resume": map[string]interface{}{
			"position": position,
			"total":    total,
		},
		"lastplayed": time.Now().Format("2006-01-02 15:04:05"),
	}
	h.executeJSONRPCO("VideoLibrary.SetMovieDetails", &ret, params)
	return
}

// SetMovieProgressWithDate ...
func (h *XBMCHost) SetMovieProgressWithDate(movieID int, position int, total int, dt time.Time) (ret string) {
	params := map[string]interface{}{
		"movieid": movieID,
		"resume": map[string]interface{}{
			"position": position,
			"total":    total,
		},
		"lastplayed": dt.Format("2006-01-02 15:04:05"),
	}
	h.executeJSONRPCO("VideoLibrary.SetMovieDetails", &ret, params)
	return
}

// SetMoviePlaycount ...
func (h *XBMCHost) SetMoviePlaycount(movieID int, playcount int) (ret string) {
	params := map[string]interface{}{
		"movieid":    movieID,
		"playcount":  playcount,
		"lastplayed": time.Now().Format("2006-01-02 15:04:05"),
	}
	h.executeJSONRPCO("VideoLibrary.SetMovieDetails", &ret, params)
	return
}

// SetShowWatched ...
func (h *XBMCHost) SetShowWatched(showID int, playcount int) (ret string) {
	params := map[string]interface{}{
		"tvshowid":  showID,
		"playcount": playcount,
	}
	h.executeJSONRPCO("VideoLibrary.SetTVShowDetails", &ret, params)
	return
}

// SetShowWatchedWithDate ...
func (h *XBMCHost) SetShowWatchedWithDate(showID int, playcount int, dt time.Time) (ret string) {
	params := map[string]interface{}{
		"tvshowid":   showID,
		"playcount":  playcount,
		"lastplayed": dt.Format("2006-01-02 15:04:05"),
	}
	h.executeJSONRPCO("VideoLibrary.SetTVShowDetails", &ret, params)
	return
}

// SetEpisodeWatched ...
func (h *XBMCHost) SetEpisodeWatched(episodeID int, playcount int, position int, total int) (ret string) {
	params := map[string]interface{}{
		"episodeid": episodeID,
		"playcount": playcount,
		"resume": map[string]interface{}{
			"position": position,
			"total":    total,
		},
		"lastplayed": time.Now().Format("2006-01-02 15:04:05"),
	}
	h.executeJSONRPCO("VideoLibrary.SetEpisodeDetails", &ret, params)
	return
}

// SetEpisodeWatchedWithDate ...
func (h *XBMCHost) SetEpisodeWatchedWithDate(episodeID int, playcount int, position int, total int, dt time.Time) (ret string) {
	params := map[string]interface{}{
		"episodeid": episodeID,
		"playcount": playcount,
		"resume": map[string]interface{}{
			"position": position,
			"total":    total,
		},
		"lastplayed": dt.Format("2006-01-02 15:04:05"),
	}
	h.executeJSONRPCO("VideoLibrary.SetEpisodeDetails", &ret, params)
	return
}

// SetEpisodeProgress ...
func (h *XBMCHost) SetEpisodeProgress(episodeID int, position int, total int) (ret string) {
	params := map[string]interface{}{
		"episodeid": episodeID,
		"resume": map[string]interface{}{
			"position": position,
			"total":    total,
		},
		"lastplayed": time.Now().Format("2006-01-02 15:04:05"),
	}
	h.executeJSONRPCO("VideoLibrary.SetEpisodeDetails", &ret, params)
	return
}

// SetEpisodeProgressWithDate ...
func (h *XBMCHost) SetEpisodeProgressWithDate(episodeID int, position int, total int, dt time.Time) (ret string) {
	params := map[string]interface{}{
		"episodeid": episodeID,
		"resume": map[string]interface{}{
			"position": position,
			"total":    total,
		},
		"lastplayed": dt.Format("2006-01-02 15:04:05"),
	}
	h.executeJSONRPCO("VideoLibrary.SetEpisodeDetails", &ret, params)
	return
}

// SetEpisodePlaycount ...
func (h *XBMCHost) SetEpisodePlaycount(episodeID int, playcount int) (ret string) {
	params := map[string]interface{}{
		"episodeid":  episodeID,
		"playcount":  playcount,
		"lastplayed": time.Now().Format("2006-01-02 15:04:05"),
	}
	h.executeJSONRPCO("VideoLibrary.SetEpisodeDetails", &ret, params)
	return
}

// SetSeasonWatched marks season as watched in Kodi library
func (h *XBMCHost) SetSeasonWatched(seasonID int, playcount int) (ret string) {
	params := map[string]interface{}{
		"seasonid":  seasonID,
		"playcount": playcount,
	}
	h.executeJSONRPCO("VideoLibrary.SetSeasonDetails", &ret, params)
	return
}

// SetFileWatched ...
func (h *XBMCHost) SetFileWatched(file string, playcount int, position int, total int) (ret string) {
	params := map[string]interface{}{
		"file":      file,
		"media":     "video",
		"playcount": playcount,
		"resume": map[string]interface{}{
			"position": position,
			"total":    total,
		},
		"lastplayed": time.Now().Format("2006-01-02 15:04:05"),
	}
	h.executeJSONRPCO("Files.SetFileDetails", &ret, params)
	return
}

// SetFileProgress sets file progress in Kodi library
// Note: Setting a valid lastplayed without a playcount will force playcount to 1.
// since we just set progress, we do not set lastplayed or playcount.
func (h *XBMCHost) SetFileProgress(file string, position int, total int) (ret string) {
	params := map[string]interface{}{
		"file":  file,
		"media": "video",
		"resume": map[string]interface{}{
			"position": position,
			"total":    total,
		},
	}
	h.executeJSONRPCO("Files.SetFileDetails", &ret, params)
	return
}

// Translate ...
func (h *XBMCHost) Translate(str string) (retVal string) {
	h.executeJSONRPCEx("Translate", &retVal, Args{str})
	return
}

// TranslateText ...
func (h *XBMCHost) TranslateText(str string) (retVal string) {
	h.executeJSONRPCEx("TranslateText", &retVal, Args{str})
	return
}

// TranslatePath ...
func (h *XBMCHost) TranslatePath(path string) (retVal string) {
	h.executeJSONRPCEx("TranslatePath", &retVal, Args{path})
	return
}

// UpdatePath ...
func (h *XBMCHost) UpdatePath(path string) (retVal string) {
	h.executeJSONRPCEx("Update", &retVal, Args{path})
	return
}

// PlaylistLeft ...
func (h *XBMCHost) PlaylistLeft() (retVal int) {
	h.executeJSONRPCEx("Playlist_Left", &retVal, Args{})
	return
}

// PlaylistSize ...
func (h *XBMCHost) PlaylistSize() (retVal int) {
	h.executeJSONRPCEx("Playlist_Size", &retVal, Args{})
	return
}

// PlaylistClear ...
func (h *XBMCHost) PlaylistClear() (retVal int) {
	h.executeJSONRPCEx("Playlist_Clear", &retVal, Args{})
	return
}

// PlayURL ...
func (h *XBMCHost) PlayURL(url string) {
	retVal := ""
	h.executeJSONRPCEx("Player_Open", &retVal, Args{url})
}

// PlayURLWithLabels ...
func (h *XBMCHost) PlayURLWithLabels(host, url string, listItem *ListItem) {
	retVal := ""
	go h.executeJSONRPCEx("Player_Open_With_Labels", &retVal, Args{url, listItem.Info})
}

// PlayURLWithTimeout ...
func (h *XBMCHost) PlayURLWithTimeout(url string) {
	retVal := ""
	go h.executeJSONRPCEx("Player_Open_With_Timeout", &retVal, Args{url})
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
func (h *XBMCHost) ConvertLanguage(language string, format int) string {
	retVal := ""
	h.executeJSONRPCEx("ConvertLanguage", &retVal, Args{language, format})
	return retVal
}

// FilesGetSources ...
func (h *XBMCHost) FilesGetSources() *FileSources {
	params := map[string]interface{}{
		"media": "video",
	}
	items := &FileSources{}
	h.executeJSONRPCO("Files.GetSources", items, params)

	return items
}

// GetLanguage ...
func (h *XBMCHost) GetLanguage(format int, withRegion bool) string {
	retVal := ""
	h.executeJSONRPCEx("GetLanguage", &retVal, Args{format, withRegion})
	return retVal
}

// GetRegion ...
func (h *XBMCHost) GetRegion() string {
	region := h.GetLanguage(Iso639_1, true)
	if strings.Contains(region, "-") {
		region = region[strings.Index(region, "-")+1:]
	}

	if region == "" {
		region = "us"
	}
	return strings.ToUpper(region)
}

// GetLanguageISO639_1 ...
func (h *XBMCHost) GetLanguageISO639_1() string {
	language := h.GetLanguage(Iso639_1, false)
	english := strings.ToLower(h.GetLanguage(EnglishName, false))

	for k, v := range languageMappings {
		if strings.HasPrefix(english, strings.ToLower(k)) {
			return v
		}
	}

	if language == "" {
		language = "en"
	}
	return language
}

// SettingsGetSettingValue ...
func (h *XBMCHost) SettingsGetSettingValue(setting string) string {
	params := map[string]interface{}{
		"setting": setting,
	}
	resp := SettingValue{}

	h.executeJSONRPCO("Settings.GetSettingValue", &resp, params)
	return resp.Value
}

// ToggleWatched toggles watched/unwatched status for Videos
func (h *XBMCHost) ToggleWatched() {
	retVal := ""
	h.executeJSONRPCEx("ToggleWatched", &retVal, nil)
}

func (h *XBMCHost) WaitForSettingsClosed() {
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()

	for {
		<-ticker.C
		if !h.AddonSettingsOpened() {
			return
		}
	}
}

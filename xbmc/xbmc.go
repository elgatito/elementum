package xbmc

import "time"

func UpdateAddonRepos() (retVal string) {
	executeJSONRPCEx("UpdateAddonRepos", &retVal, nil)
	return
}

func ResetRPC() (retVal string) {
	executeJSONRPCEx("Reset", &retVal, nil)
	return
}

func Refresh() (retVal string) {
	executeJSONRPCEx("Refresh", &retVal, nil)
	return
}

func VideoLibraryScan() (retVal string) {
	executeJSONRPC("VideoLibrary.Scan", &retVal, nil)
	return
}

func VideoLibraryClean() (retVal string) {
	executeJSONRPC("VideoLibrary.Clean", &retVal, nil)
	return
}

func VideoLibraryGetMovies() (movies *VideoLibraryMovies) {
	params := map[string]interface{}{"properties": []interface{}{
		"imdbnumber",
		"playcount",
		"file",
		"resume",
	}}
	ret := executeJSONRPCO("VideoLibrary.GetMovies", &movies, params)
	if ret != nil {
		log.Error(ret)
	}
	return movies
}

func PlayerGetActive() (int) {
	params := map[string]interface{}{}
	items := ActivePlayers{}
	executeJSONRPCO("Player.GetActivePlayers", &items, params)
	for _, v := range items {
		if v.Type == "video" {
			return v.Id
		}
	}

	return -1
}

func PlayerGetItem(playerid int) (item *PlayerItemInfo) {
	params := map[string]interface{}{
		"playerid": playerid,
	}
	executeJSONRPCO("Player.GetItem", &item, params)
	return
}

func VideoLibraryGetShows() (shows *VideoLibraryShows) {
	params := map[string]interface{}{"properties": []interface{}{"imdbnumber", "episode"}}
	err := executeJSONRPCO("VideoLibrary.GetTVShows", &shows, params)
	if err != nil {
		log.Error(err)
	}
	return
}

func VideoLibraryGetEpisodes(tvshowId int) (episodes *VideoLibraryEpisodes) {
	params := map[string]interface{}{"tvshowid": tvshowId, "properties": []interface{}{
		"tvshowid",
		"uniqueid",
		"season",
		"episode",
		"playcount",
		"file",
		"resume",
	}}
	err := executeJSONRPCO("VideoLibrary.GetEpisodes", &episodes, params)
	if err != nil {
		log.Error(err)
	}
	return
}

func SetMovieWatched(movieId int, playcount int, position int, total int) (ret string) {
	params := map[string]interface{}{
		"movieid": movieId,
		"playcount": playcount,
		"resume": map[string]interface{}{
			"position": position,
			"total": total,
		},
		"lastplayed": time.Now().Format("2006-01-02 15:04:05"),
	}
	executeJSONRPCO("VideoLibrary.SetMovieDetails", &ret, params)
	return
}

func SetEpisodeWatched(episodeId int, playcount int, position int, total int) (ret string) {
	params := map[string]interface{}{
		"episodeid": episodeId,
		"playcount": playcount,
		"resume": map[string]interface{}{
			"position": position,
			"total": total,
		},
		"lastplayed": time.Now().Format("2006-01-02 15:04:05"),
	}
	executeJSONRPCO("VideoLibrary.SetEpisodeDetails", &ret, params)
	return
}

func SetFileWatched(file string, position int, total int) (ret string) {
	params := map[string]interface{}{
		"file": file,
		"media": "video",
		"playcount": 0,
		"resume": map[string]interface{}{
			"position": position,
			"total": total,
		},
		"lastplayed": time.Now().Format("2006-01-02 15:04:05"),
	}
	executeJSONRPCO("VideoLibrary.SetFileDetails", &ret, params)
	return
}

func TranslatePath(path string) (retVal string) {
	executeJSONRPCEx("TranslatePath", &retVal, Args{path})
	return
}

func UpdatePath(path string) (retVal string) {
	executeJSONRPCEx("Update", &retVal, Args{path})
	return
}

func PlayURL(url string) {
	retVal := ""
	executeJSONRPCEx("Player_Open", &retVal, Args{url})
}

const (
	ISO_639_1 = iota
	ISO_639_2
	EnglishName
)

func ConvertLanguage(language string, format int) string {
	retVal := ""
	executeJSONRPCEx("ConvertLanguage", &retVal, Args{language, format})
	return retVal
}

func GetLanguage(format int) string {
	retVal := ""
	executeJSONRPCEx("GetLanguage", &retVal, Args{format})
	return retVal
}

func GetLanguageISO_639_1() string{
	language := GetLanguage(ISO_639_1)
	if language == "" {
		switch GetLanguage(EnglishName) {
		case "Chinese (Simple)":      return "zh"
		case "Chinese (Traditional)": return "zh"
		case "English (Australia)":   return "en"
		case "English (New Zealand)": return "en"
		case "English (US)":          return "en"
		case "French (Canada)":       return "fr"
		case "Hindi (Devanagiri)":    return "hi"
		case "Mongolian (Mongolia)":  return "mn"
		case "Persian (Iran)":        return "fa"
		case "Portuguese (Brazil)":   return "pt"
		case "Serbian (Cyrillic)":    return "sr"
		case "Spanish (Argentina)":   return "es"
		case "Spanish (Mexico)":      return "es"
		case "Tamil (India)":         return "ta"
		default:                      return "en"
		}
	}
	return language
}

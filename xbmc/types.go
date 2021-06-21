package xbmc

import "time"

//go:generate msgp -o msgp.go -io=false -tests=false

// View ...
type View struct {
	ContentType string    `json:"content_type"`
	Items       ListItems `json:"items"`
}

// GUIIconOverlay ...
type GUIIconOverlay int

const (
	// IconOverlayNone ...
	IconOverlayNone GUIIconOverlay = iota
	// IconOverlayRAR ...
	IconOverlayRAR
	// IconOverlayZip ...
	IconOverlayZip
	// IconOverlayLocked ...
	IconOverlayLocked
	// IconOverlayHasTrainer ...
	IconOverlayHasTrainer
	// IconOverlayTrained ...
	IconOverlayTrained
	// IconOverlayWatched ...
	IconOverlayWatched
	// IconOverlayHD ...
	IconOverlayHD
)

var (
	// KodiVersion saves Kodi platform
	KodiVersion = 0

	// DialogAutoclose determines number of seconds to wait
	// until DialogConfirm should be automatically closed
	DialogAutoclose = 0

	languageMappings = map[string]string{
		"Chinese":    "zh",
		"English":    "en",
		"French":     "fr",
		"Hindi":      "hi",
		"Mongolian":  "mn",
		"Persian":    "fa",
		"Portuguese": "pt",
		"Serbian":    "sr",
		"Spanish":    "es",
		"Tamil":      "ta",
	}
)

// ListItems ...
type ListItems []*ListItem

// ListItem ...
type ListItem struct {
	Label       string               `json:"label"`
	Label2      string               `json:"label2"`
	Icon        string               `json:"icon"`
	Thumbnail   string               `json:"thumbnail"`
	IsPlayable  bool                 `json:"is_playable"`
	Path        string               `json:"path"`
	Info        *ListItemInfo        `json:"info,omitempty"`
	Properties  *ListItemProperties  `json:"properties,omitempty"`
	Art         *ListItemArt         `json:"art,omitempty"`
	StreamInfo  *StreamInfo          `json:"stream_info,omitempty"`
	ContextMenu [][]string           `json:"context_menu,omitempty"`
	CastMembers []ListItemCastMember `json:"castmembers,omitempty"`

	TraktAuth bool `json:"-"`

	UniqueIDs *UniqueIDs `json:"uniqueids,omitempty"`
}

// ListItemProperties ...
type ListItemProperties struct {
	TotalSeasons             string `json:"totalseasons,omitempty"`
	TotalEpisodes            string `json:"totalepisodes,omitempty"`
	WatchedEpisodes          string `json:"watchedepisodes,omitempty"`
	UnWatchedEpisodes        string `json:"unwatchedepisodes,omitempty"`
	SubtitlesSync            string `json:"sync,omitempty"`
	SubtitlesHearingImpaired string `json:"hearing_imp,omitempty"`
	ShowTMDBId               string `json:"showtmdbid,omitempty"`
}

// ListItemInfo ...
type ListItemInfo struct {
	// General Values that apply to all types
	Count int    `json:"count,omitempty"`
	Size  int    `json:"size,omitempty"`
	Date  string `json:"date,omitempty"`

	// Video Values
	Genre         []string       `json:"genre,omitempty"`
	Country       []string       `json:"country,omitempty"`
	Year          int            `json:"year,omitempty"`
	Episode       int            `json:"episode,omitempty"`
	Season        int            `json:"season,omitempty"`
	Top250        int            `json:"top250,omitempty"`
	TrackNumber   int            `json:"tracknumber,omitempty"`
	Rating        float32        `json:"rating,omitempty"`
	PlayCount     int            `json:"playcount,omitempty"`
	Overlay       GUIIconOverlay `json:"overlay,omitempty"`
	Director      []string       `json:"director,omitempty"`
	MPAA          string         `json:"mpaa,omitempty"`
	Plot          string         `json:"plot,omitempty"`
	PlotOutline   string         `json:"plotoutline,omitempty"`
	Title         string         `json:"title,omitempty"`
	OriginalTitle string         `json:"originaltitle,omitempty"`
	SortTitle     string         `json:"sorttitle,omitempty"`
	Duration      int            `json:"duration,omitempty"`
	Studio        []string       `json:"studio,omitempty"`
	TagLine       string         `json:"tagline,omitempty"`
	Writer        []string       `json:"writer,omitempty"`
	TVShowTitle   string         `json:"tvshowtitle,omitempty"`
	Premiered     string         `json:"premiered,omitempty"`
	Status        string         `json:"status,omitempty"`
	Code          string         `json:"code,omitempty"`
	Aired         string         `json:"aired,omitempty"`
	Credits       []string       `json:"credits,omitempty"`
	LastPlayed    string         `json:"lastplayed,omitempty"`
	Album         string         `json:"album,omitempty"`
	Artist        []string       `json:"artist,omitempty"`
	Votes         string         `json:"votes,omitempty"`
	Trailer       string         `json:"trailer,omitempty"`
	DateAdded     string         `json:"dateadded,omitempty"`
	DBID          int            `json:"dbid,omitempty"`
	DBTYPE        string         `json:"dbtype,omitempty"`
	Mediatype     string         `json:"mediatype,omitempty"`
	IMDBNumber    string         `json:"imdbnumber,omitempty"`

	// Music Values
	Lyrics string `json:"lyrics,omitempty"`

	// Picture Values
	PicturePath string `json:"picturepath,omitempty"`
	Exif        string `json:"exif,omitempty"`
}

// ListItemArt ...
type ListItemArt struct {
	Thumbnail         string    `json:"thumb,omitempty"`
	Poster            string    `json:"poster,omitempty"`
	TvShowPoster      string    `json:"tvshowposter,omitempty"`
	Banner            string    `json:"banner,omitempty"`
	FanArt            string    `json:"fanart,omitempty"`
	FanArts           []string  `json:"fanarts,omitempty"`
	ClearArt          string    `json:"clearart,omitempty"`
	ClearLogo         string    `json:"clearlogo,omitempty"`
	Landscape         string    `json:"landscape,omitempty"`
	Icon              string    `json:"icon,omitempty"`
	DiscArt           string    `json:"discart,omitempty"`
	KeyArt            string    `json:"keyart,omitempty"`
	AvailableArtworks *Artworks `json:"available_artworks,omitempty"`
}

type Artworks struct {
	Poster    []string `json:"poster,omitempty"`
	Banner    []string `json:"banner,omitempty"`
	FanArt    []string `json:"fanart,omitempty"`
	ClearArt  []string `json:"clearart,omitempty"`
	ClearLogo []string `json:"clearlogo,omitempty"`
	Landscape []string `json:"landscape,omitempty"`
	Icon      []string `json:"icon,omitempty"`
	DiscArt   []string `json:"discart,omitempty"`
	KeyArt    []string `json:"keyart,omitempty"`
}

// ListItemCastMember represents Cast member information from TMDB
type ListItemCastMember struct {
	Name      string `json:"name,omitempty"`
	Role      string `json:"role,omitempty"`
	Thumbnail string `json:"thumbnail,omitempty"`
	Order     int    `json:"order"`
}

// ContextMenuItem ...
type ContextMenuItem struct {
	Label  string `json:"label"`
	Action string `json:"action"`
}

// StreamInfo ...
type StreamInfo struct {
	Video    *StreamInfoEntry `json:"video,omitempty"`
	Audio    *StreamInfoEntry `json:"audio,omitempty"`
	Subtitle *StreamInfoEntry `json:"subtitle,omitempty"`
}

// StreamInfoEntry ...
type StreamInfoEntry struct {
	Codec    string  `json:"codec,omitempty"`
	Aspect   float32 `json:"aspect,omitempty"`
	Width    int     `json:"width,omitempty"`
	Height   int     `json:"height,omitempty"`
	Duration int     `json:"duration,omitempty"`
	Language string  `json:"language,omitempty"`
	Channels int     `json:"channels,omitempty"`
}

// VideoLibraryLimits ...
type VideoLibraryLimits struct {
	End   int `json:"end"`
	Start int `json:"start"`
	Total int `json:"total"`
}

// VideoLibraryMovies ...
type VideoLibraryMovies struct {
	Limits *VideoLibraryLimits      `json:"limits"`
	Movies []*VideoLibraryMovieItem `json:"movies"`
}

// VideoLibraryMovieItem ...
type VideoLibraryMovieItem struct {
	ID         int       `json:"movieid"`
	Title      string    `json:"label"`
	IMDBNumber string    `json:"imdbnumber"`
	PlayCount  int       `json:"playcount"`
	File       string    `json:"file"`
	Year       int       `json:"year"`
	DateAdded  KodiTime  `json:"dateadded"`
	UniqueIDs  UniqueIDs `json:"uniqueid"`
	Resume     *Resume
}

// VideoLibraryShows ...
type VideoLibraryShows struct {
	Limits *VideoLibraryLimits     `json:"limits"`
	Shows  []*VideoLibraryShowItem `json:"tvshows"`
}

// VideoLibraryShowItem ...
type VideoLibraryShowItem struct {
	ID         int       `json:"tvshowid"`
	Title      string    `json:"label"`
	IMDBNumber string    `json:"imdbnumber"`
	PlayCount  int       `json:"playcount"`
	Year       int       `json:"year"`
	Episodes   int       `json:"episode"`
	DateAdded  KodiTime  `json:"dateadded"`
	UniqueIDs  UniqueIDs `json:"uniqueid"`
}

// VideoLibrarySeasons ...
type VideoLibrarySeasons struct {
	Seasons []*VideoLibrarySeasonItem `json:"seasons"`
}

// VideoLibrarySeason ...
type VideoLibrarySeason struct {
	Episode *VideoLibrarySeasonItem `json:"seasondetails"`
}

// VideoLibrarySeasonItem ...
type VideoLibrarySeasonItem struct {
	ID        int       `json:"seasonid"`
	Title     string    `json:"label"`
	Season    int       `json:"season"`
	Episodes  int       `json:"episode"`
	TVShowID  int       `json:"tvshowid"`
	PlayCount int       `json:"playcount"`
	UniqueIDs UniqueIDs `json:"uniqueid"`
}

// VideoLibraryEpisodes ...
type VideoLibraryEpisodes struct {
	Episodes []*VideoLibraryEpisodeItem `json:"episodes"`
}

// VideoLibraryEpisode ...
type VideoLibraryEpisode struct {
	Episode *VideoLibraryEpisodeItem `json:"episodedetails"`
}

// VideoLibraryEpisodeItem ...
type VideoLibraryEpisodeItem struct {
	ID        int       `json:"episodeid"`
	Title     string    `json:"label"`
	Season    int       `json:"season"`
	Episode   int       `json:"episode"`
	TVShowID  int       `json:"tvshowid"`
	PlayCount int       `json:"playcount"`
	File      string    `json:"file"`
	DateAdded KodiTime  `json:"dateadded"`
	UniqueIDs UniqueIDs `json:"uniqueid"`
	Resume    *Resume
}

// UniqueIDs ...
type UniqueIDs struct {
	Unknown    string `json:"unknown"`
	TMDB       string `json:"tmdb"`
	TVDB       string `json:"tvdb"`
	IMDB       string `json:"imdb"`
	TheMovieDB string `json:"themoviedb"`
	Trakt      string `json:"trakt"`
	Elementum  string `json:"elementum"`
	Kodi       int
}

// Resume ...
type Resume struct {
	Position float64 `json:"position"`
	Total    float64 `json:"total"`
}

// PlayerItemInfo ...
type PlayerItemInfo struct {
	Info struct {
		ID   int    `json:"id"`
		Type string `json:"type"`
	} `json:"item"`
}

// ActivePlayers ...
type ActivePlayers []struct {
	ID   int    `json:"playerid"`
	Type string `json:"type"`
}

// FileSources ...
type FileSources struct {
	Sources []struct {
		FilePath string `json:"file"`
		Label    string `json:"label"`
	} `json:"sources"`
}

// AdvancedSettings describes advancedsettings.xml
type AdvancedSettings struct {
	LogLevel int `xml:"loglevel"`
	Cache    struct {
		MemorySizeLegacy int `xml:"cachemembuffersize"`
		MemorySize       int `xml:"memorysize"`
		BufferMode       int `xml:"buffermode"`
		ReadFactor       int `xml:"readfactor"`
	} `xml:"cache"`
}

// SettingValue ...
type SettingValue struct {
	Value string `json:"value"`
}

// KodiTime ...
type KodiTime struct {
	time.Time
}

// UnmarshalJSON ...
func (s *KodiTime) UnmarshalJSON(b []byte) (err error) {
	str := string(b[1 : len(b)-1])
	if len(str) == 0 {
		return
	}

	t, err := time.Parse("2006-01-02 15:04:05", str)
	if err != nil {
		log.Debugf("Error parsing date '%s': %s", str, err)
	}
	s.Time = t

	return
}

// NewView ...
func NewView(contentType string, items ListItems) *View {
	return &View{
		ContentType: contentType,
		Items:       items,
	}
}

func (li ListItems) Len() int           { return len(li) }
func (li ListItems) Swap(i, j int)      { li[i], li[j] = li[j], li[i] }
func (li ListItems) Less(i, j int) bool { return false }

package config

import (
	"encoding/xml"
	"errors"
	"fmt"
	"io/ioutil"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/elgatito/elementum/xbmc"

	"github.com/dustin/go-humanize"
	"github.com/op/go-logging"
	"github.com/pbnjay/memory"
	"github.com/sanity-io/litter"
)

var log = logging.MustGetLogger("config")
var privacyRegex = regexp.MustCompile(`(?i)(pass|password): "(.+?)"`)

const (
	maxMemorySize                = 300 * 1024 * 1024
	defaultAutoMemorySize        = 40 * 1024 * 1024
	defaultTraktSyncFrequencyMin = 5
	defaultEndBufferSize         = 1 * 1024 * 1024
	defaultDiskCacheSize         = 12 * 1024 * 1024

	// TraktReadClientID ...
	TraktReadClientID = "eb8839a79fb2af4ebfb93f993a8a539abd4d9674a7638497bbc662d2a4b22346"
	// TraktReadClientSecret ...
	TraktReadClientSecret = "338cfda318c5879c9d7d0888bf1875e303576d4ad7e72a2230addf5db326c791"
	// TraktWriteClientID ...
	TraktWriteClientID = "66f7807c55e9fec2d6627846baf8bc667a5e82620b6e037a044034c64e3cb5e2"
	// TraktWriteClientSecret ...
	TraktWriteClientSecret = "5d37802b559c17a8dc10daaf96c55b196b1c86a723e6667310556288b3cac7fb"
)

// Configuration ...
type Configuration struct {
	DownloadPath               string
	TorrentsPath               string
	LibraryPath                string
	Info                       *xbmc.AddonInfo
	Platform                   *xbmc.Platform
	Language                   string
	Region                     string
	TemporaryPath              string
	ProfilePath                string
	HomePath                   string
	XbmcPath                   string
	SpoofUserAgent             int
	DownloadFileStrategy       int
	KeepDownloading            int
	KeepFilesPlaying           int
	KeepFilesFinished          int
	UseTorrentHistory          bool
	TorrentHistorySize         int
	UseFanartTv                bool
	DisableBgProgress          bool
	DisableBgProgressPlayback  bool
	ForceUseTrakt              bool
	UseCacheSelection          bool
	UseCacheSearch             bool
	UseCacheTorrents           bool
	CacheSearchDuration        int
	ShowFilesWatched           bool
	ResultsPerPage             int
	GreetingEnabled            bool
	EnableOverlayStatus        bool
	SilentStreamStart          bool
	AutoYesEnabled             bool
	AutoYesTimeout             int
	ChooseStreamAutoMovie      bool
	ChooseStreamAutoShow       bool
	ChooseStreamAutoSearch     bool
	ForceLinkType              bool
	UseOriginalTitle           bool
	UseAnimeEnTitle            bool
	UseLowestReleaseDate       bool
	AddSpecials                bool
	AddEpisodeNumbers          bool
	ShowUnairedSeasons         bool
	ShowUnairedEpisodes        bool
	ShowSeasonsAll             bool
	ShowSeasonsOrder           int
	ShowSeasonsSpecials        bool
	SmartEpisodeStart          bool
	SmartEpisodeMatch          bool
	SmartEpisodeChoose         bool
	LibraryEnabled             bool
	LibrarySyncEnabled         bool
	LibrarySyncPlaybackEnabled bool
	LibraryUpdate              int
	StrmLanguage               string
	LibraryNFOMovies           bool
	LibraryNFOShows            bool
	PlaybackPercent            int
	DownloadStorage            int
	SkipBurstSearch            bool
	AutoMemorySize             bool
	AutoKodiBufferSize         bool
	AutoAdjustMemorySize       bool
	AutoMemorySizeStrategy     int
	MemorySize                 int
	AutoAdjustBufferSize       bool
	MinCandidateSize           int64
	MinCandidateShowSize       int64
	BufferTimeout              int
	BufferSize                 int
	EndBufferSize              int
	KodiBufferSize             int
	UploadRateLimit            int
	DownloadRateLimit          int
	AutoloadTorrents           bool
	AutoloadTorrentsPaused     bool
	LimitAfterBuffering        bool
	ConnectionsLimit           int
	ConnTrackerLimit           int
	ConnTrackerLimitAuto       bool
	SessionSave                int

	SeedForever        bool
	ShareRatioLimit    int
	SeedTimeRatioLimit int
	SeedTimeLimit      int

	DisableUpload            bool
	DisableLSD               bool
	DisableDHT               bool
	DisableTCP               bool
	DisableUTP               bool
	DisableUPNP              bool
	EncryptionPolicy         int
	ListenPortMin            int
	ListenPortMax            int
	ListenInterfaces         string
	ListenAutoDetectIP       bool
	ListenAutoDetectPort     bool
	OutgoingInterfaces       string
	TunedStorage             bool
	DiskCacheSize            int
	UseLibtorrentConfig      bool
	UseLibtorrentLogging     bool
	UseLibtorrentDeadlines   bool
	UseLibtorrentPauseResume bool
	LibtorrentProfile        int
	MagnetResolveTimeout     int
	AddExtraTrackers         int
	RemoveOriginalTrackers   int
	Scrobble                 bool

	AutoScrapeEnabled        bool
	AutoScrapeLibraryEnabled bool
	AutoScrapeStrategy       int
	AutoScrapeStrategyExpect int
	AutoScrapePerHours       int
	AutoScrapeLimitMovies    int
	AutoScrapeInterval       int

	TraktAuthorized                bool
	TraktUsername                  string
	TraktToken                     string
	TraktRefreshToken              string
	TraktTokenExpiry               int
	TraktSyncEnabled               bool
	TraktSyncPlaybackEnabled       bool
	TraktSyncFrequencyMin          int
	TraktSyncCollections           bool
	TraktSyncWatchlist             bool
	TraktSyncUserlists             bool
	TraktSyncPlaybackProgress      bool
	TraktSyncHidden                bool
	TraktSyncWatched               bool
	TraktSyncWatchedBack           bool
	TraktSyncAddedMovies           bool
	TraktSyncAddedMoviesLocation   int
	TraktSyncAddedMoviesList       int
	TraktSyncAddedShows            bool
	TraktSyncAddedShowsLocation    int
	TraktSyncAddedShowsList        int
	TraktSyncRemovedMovies         bool
	TraktSyncRemovedMoviesLocation int
	TraktSyncRemovedMoviesList     int
	TraktSyncRemovedShows          bool
	TraktSyncRemovedShowsLocation  int
	TraktSyncRemovedShowsList      int
	TraktProgressUnaired           bool
	TraktProgressSort              int
	TraktProgressDateFormat        string
	TraktProgressColorDate         string
	TraktProgressColorShow         string
	TraktProgressColorEpisode      string
	TraktProgressColorUnaired      string
	TraktCalendarsDateFormat       string
	TraktCalendarsColorDate        string
	TraktCalendarsColorShow        string
	TraktCalendarsColorEpisode     string
	TraktCalendarsColorUnaired     string

	UpdateFrequency  int
	UpdateDelay      int
	UpdateAutoScan   bool
	PlayResumeAction int
	PlayResumeBack   int
	TMDBApiKey       string

	OSDBUser               string
	OSDBPass               string
	OSDBLanguage           string
	OSDBAutoLanguage       bool
	OSDBAutoLoad           bool
	OSDBAutoLoadCount      int
	OSDBAutoLoadDelete     bool
	OSDBAutoLoadSkipExists bool
	OSDBIncludedEnabled    bool
	OSDBIncludedSkipExists bool

	SortingModeMovies           int
	SortingModeShows            int
	ResolutionPreferenceMovies  int
	ResolutionPreferenceShows   int
	PercentageAdditionalSeeders int

	CustomProviderTimeoutEnabled bool
	CustomProviderTimeout        int

	InternalDNSEnabled  bool
	InternalDNSSkipIPv6 bool

	InternalProxyEnabled     bool
	InternalProxyLogging     bool
	InternalProxyLoggingBody bool

	ProxyURL         string
	ProxyType        int
	ProxyEnabled     bool
	ProxyHost        string
	ProxyPort        int
	ProxyLogin       string
	ProxyPassword    string
	ProxyUseHTTP     bool
	ProxyUseTracker  bool
	ProxyUseDownload bool

	CompletedMove       bool
	CompletedMoviesPath string
	CompletedShowsPath  string

	LocalOnlyClient bool
	LogLevel        int
}

// Addon ...
type Addon struct {
	ID      string
	Name    string
	Version string
	Enabled bool
}

var (
	config          = &Configuration{}
	lock            = sync.RWMutex{}
	settingsAreSet  = false
	settingsWarning = ""

	proxyTypes = []string{
		"Socks4",
		"Socks5",
		"HTTP",
		"HTTPS",
	}
)

var (
	// Args for cli arguments parsing
	Args = struct {
		DisableBackup bool `help:"Disable database backup"`

		RemoteHost string `help:"remote host, default is '127.0.0.1'"`
		RemotePort int    `help:"remote port, default is '65221'"`

		LocalHost string `help:"local host, default is '0.0.0.0'"`
		LocalPort int    `help:"local port, default is '65220'"`
	}{
		DisableBackup: false,

		RemoteHost: "127.0.0.1",
		RemotePort: 65221,

		LocalHost: "127.0.0.1",
		LocalPort: 65220,
	}
)

// Get ...
func Get() *Configuration {
	lock.RLock()
	defer lock.RUnlock()
	return config
}

// Reload ...
func Reload() *Configuration {
	log.Info("Reloading configuration...")

	// Reloading RPC Hosts
	log.Infof("Setting remote address to %s:%d", Args.RemoteHost, Args.RemotePort)
	xbmc.XBMCJSONRPCHosts = []string{net.JoinHostPort(Args.RemoteHost, "9090")}
	xbmc.XBMCExJSONRPCHosts = []string{net.JoinHostPort(Args.RemoteHost, strconv.Itoa(Args.RemotePort))}

	defer func() {
		if r := recover(); r != nil {
			log.Warningf("Addon settings not properly set, opening settings window: %#v", r)

			message := "LOCALIZE[30314]"
			if settingsWarning != "" {
				message = settingsWarning
			}

			xbmc.AddonSettings("plugin.video.elementum")
			xbmc.Dialog("Elementum", message)

			waitForSettingsClosed()

			// Custom code to say python not to report this error
			os.Exit(5)
		}
	}()

	info := xbmc.GetAddonInfo()
	if info == nil || info.ID == "" {
		log.Warningf("Can't continue because addon info is empty")
		settingsWarning = "LOCALIZE[30113]"
		panic(settingsWarning)
	}

	info.Path = xbmc.TranslatePath(info.Path)
	info.Profile = xbmc.TranslatePath(info.Profile)
	info.Home = xbmc.TranslatePath(info.Home)
	info.Xbmc = xbmc.TranslatePath(info.Xbmc)
	info.TempPath = filepath.Join(xbmc.TranslatePath("special://temp"), "elementum")

	platform := xbmc.GetPlatform()

	// If it's Windows and it's installed from Store - we should try to find real path
	// and change addon settings accordingly
	if platform != nil && strings.ToLower(platform.OS) == "windows" && strings.Contains(info.Xbmc, "XBMCFoundation") {
		path := findExistingPath([]string{
			filepath.Join(os.Getenv("LOCALAPPDATA"), "/Packages/XBMCFoundation.Kodi_4n2hpmxwrvr6p/LocalCache/Roaming/Kodi/"),
			filepath.Join(os.Getenv("APPDATA"), "/kodi/"),
		}, "/userdata/addon_data/"+info.ID)

		if path != "" {
			info.Path = strings.Replace(info.Path, info.Home, "", 1)
			info.Profile = strings.Replace(info.Profile, info.Home, "", 1)
			info.TempPath = strings.Replace(info.TempPath, info.Home, "", 1)
			info.Icon = strings.Replace(info.Icon, info.Home, "", 1)

			info.Path = filepath.Join(path, info.Path)
			info.Profile = filepath.Join(path, info.Profile)
			info.TempPath = filepath.Join(path, info.TempPath)
			info.Icon = filepath.Join(path, info.Icon)

			info.Home = path
		}
	}

	os.RemoveAll(info.TempPath)
	if err := os.MkdirAll(info.TempPath, 0777); err != nil {
		log.Infof("Could not create temporary directory: %#v", err)
	}

	if platform.OS == "android" {
		legacyPath := strings.Replace(info.Path, "/storage/emulated/0", "/storage/emulated/legacy", 1)
		if _, err := os.Stat(legacyPath); err == nil {
			info.Path = legacyPath
			info.Profile = strings.Replace(info.Profile, "/storage/emulated/0", "/storage/emulated/legacy", 1)
			log.Info("Using /storage/emulated/legacy path.")
		}
	}
	if !PathExists(info.Profile) {
		log.Infof("Profile path does not exist, creating it at: %s", info.Profile)
		if err := os.MkdirAll(info.Profile, 0777); err != nil {
			log.Errorf("Could not create profile directory: %#v", err)
		}
	}
	if !PathExists(filepath.Join(info.Profile, "libtorrent.config")) {
		filePath := filepath.Join(info.Profile, "libtorrent.config")
		log.Infof("Creating libtorrent.config to further usage at: %s", filePath)
		if _, err := os.Create(filePath); err == nil {
			os.Chmod(filePath, 0666)
		}
	}

	downloadPath := TranslatePath(xbmc.GetSettingString("download_path"))
	libraryPath := TranslatePath(xbmc.GetSettingString("library_path"))
	torrentsPath := TranslatePath(xbmc.GetSettingString("torrents_path"))
	downloadStorage := xbmc.GetSettingInt("download_storage")
	if downloadStorage > 1 {
		downloadStorage = 1
	}

	log.Noticef("Paths translated by Kodi: Download = %s , Library = %s , Torrents = %s , Storage = %d", downloadPath, libraryPath, torrentsPath, downloadStorage)

	if downloadStorage != 1 {
		if downloadPath == "." {
			log.Warningf("Can't continue because download path is empty")
			settingsWarning = "LOCALIZE[30113]"
			panic(settingsWarning)
		} else if err := IsWritablePath(downloadPath); err != nil {
			log.Errorf("Cannot write to download location '%s': %#v", downloadPath, err)
			settingsWarning = err.Error()
			panic(settingsWarning)
		}
	}
	log.Infof("Using download path: %s", downloadPath)

	if libraryPath == "." {
		log.Errorf("Cannot use library location '%s'", libraryPath)
		settingsWarning = "LOCALIZE[30220]"
		panic(settingsWarning)
	} else if strings.Contains(libraryPath, "elementum_library") {
		if err := os.MkdirAll(libraryPath, 0777); err != nil {
			log.Errorf("Could not create temporary library directory: %#v", err)
			settingsWarning = err.Error()
			panic(settingsWarning)
		}
	}
	if err := IsWritablePath(libraryPath); err != nil {
		log.Errorf("Cannot write to library location '%s': %#v", libraryPath, err)
		settingsWarning = err.Error()
		panic(settingsWarning)
	}
	log.Infof("Using library path: %s", libraryPath)

	if torrentsPath == "." {
		torrentsPath = filepath.Join(downloadPath, "Torrents")
	} else if strings.Contains(torrentsPath, "elementum_torrents") {
		if err := os.MkdirAll(torrentsPath, 0777); err != nil {
			log.Errorf("Could not create temporary torrents directory: %#v", err)
			settingsWarning = err.Error()
			panic(settingsWarning)
		}
	}
	if err := IsWritablePath(torrentsPath); err != nil {
		log.Errorf("Cannot write to location '%s': %#v", torrentsPath, err)
		settingsWarning = err.Error()
		panic(settingsWarning)
	}
	log.Infof("Using torrents path: %s", torrentsPath)

	xbmcSettings := xbmc.GetAllSettings()
	settings := make(map[string]interface{})
	for _, setting := range xbmcSettings {
		switch setting.Type {
		case "enum":
			fallthrough
		case "number":
			value, _ := strconv.Atoi(setting.Value)
			settings[setting.Key] = value
		case "slider":
			var valueInt int
			var valueFloat float32
			switch setting.Option {
			case "percent":
				fallthrough
			case "int":
				floated, _ := strconv.ParseFloat(setting.Value, 32)
				valueInt = int(floated)
			case "float":
				floated, _ := strconv.ParseFloat(setting.Value, 32)
				valueFloat = float32(floated)
			}
			if valueFloat > 0 {
				settings[setting.Key] = valueFloat
			} else {
				settings[setting.Key] = valueInt
			}
		case "bool":
			settings[setting.Key] = (setting.Value == "true")
		default:
			settings[setting.Key] = setting.Value
		}
	}

	newConfig := Configuration{
		DownloadPath:               downloadPath,
		LibraryPath:                libraryPath,
		TorrentsPath:               torrentsPath,
		Info:                       info,
		Platform:                   platform,
		Language:                   xbmc.GetLanguageISO639_1(),
		Region:                     xbmc.GetRegion(),
		TemporaryPath:              info.TempPath,
		ProfilePath:                info.Profile,
		HomePath:                   info.Home,
		XbmcPath:                   info.Xbmc,
		DownloadStorage:            settings["download_storage"].(int),
		SkipBurstSearch:            settings["skip_burst_search"].(bool),
		AutoMemorySize:             settings["auto_memory_size"].(bool),
		AutoAdjustMemorySize:       settings["auto_adjust_memory_size"].(bool),
		AutoMemorySizeStrategy:     settings["auto_memory_size_strategy"].(int),
		MemorySize:                 settings["memory_size"].(int) * 1024 * 1024,
		AutoKodiBufferSize:         settings["auto_kodi_buffer_size"].(bool),
		AutoAdjustBufferSize:       settings["auto_adjust_buffer_size"].(bool),
		MinCandidateSize:           int64(settings["min_candidate_size"].(int) * 1024 * 1024),
		MinCandidateShowSize:       int64(settings["min_candidate_show_size"].(int) * 1024 * 1024),
		BufferTimeout:              settings["buffer_timeout"].(int),
		BufferSize:                 settings["buffer_size"].(int) * 1024 * 1024,
		EndBufferSize:              settings["end_buffer_size"].(int) * 1024 * 1024,
		UploadRateLimit:            settings["max_upload_rate"].(int) * 1024,
		DownloadRateLimit:          settings["max_download_rate"].(int) * 1024,
		AutoloadTorrents:           settings["autoload_torrents"].(bool),
		AutoloadTorrentsPaused:     settings["autoload_torrents_paused"].(bool),
		SpoofUserAgent:             settings["spoof_user_agent"].(int),
		LimitAfterBuffering:        settings["limit_after_buffering"].(bool),
		DownloadFileStrategy:       settings["download_file_strategy"].(int),
		KeepDownloading:            settings["keep_downloading"].(int),
		KeepFilesPlaying:           settings["keep_files_playing"].(int),
		KeepFilesFinished:          settings["keep_files_finished"].(int),
		UseTorrentHistory:          settings["use_torrent_history"].(bool),
		TorrentHistorySize:         settings["torrent_history_size"].(int),
		UseFanartTv:                settings["use_fanart_tv"].(bool),
		DisableBgProgress:          settings["disable_bg_progress"].(bool),
		DisableBgProgressPlayback:  settings["disable_bg_progress_playback"].(bool),
		ForceUseTrakt:              settings["force_use_trakt"].(bool),
		UseCacheSelection:          settings["use_cache_selection"].(bool),
		UseCacheSearch:             settings["use_cache_search"].(bool),
		UseCacheTorrents:           settings["use_cache_torrents"].(bool),
		CacheSearchDuration:        settings["cache_search_duration"].(int),
		ResultsPerPage:             settings["results_per_page"].(int),
		ShowFilesWatched:           settings["show_files_watched"].(bool),
		GreetingEnabled:            settings["greeting_enabled"].(bool),
		EnableOverlayStatus:        settings["enable_overlay_status"].(bool),
		SilentStreamStart:          settings["silent_stream_start"].(bool),
		AutoYesEnabled:             settings["autoyes_enabled"].(bool),
		AutoYesTimeout:             settings["autoyes_timeout"].(int),
		ChooseStreamAutoMovie:      settings["choose_stream_auto_movie"].(bool),
		ChooseStreamAutoShow:       settings["choose_stream_auto_show"].(bool),
		ChooseStreamAutoSearch:     settings["choose_stream_auto_search"].(bool),
		ForceLinkType:              settings["force_link_type"].(bool),
		UseOriginalTitle:           settings["use_original_title"].(bool),
		UseAnimeEnTitle:            settings["use_anime_en_title"].(bool),
		UseLowestReleaseDate:       settings["use_lowest_release_date"].(bool),
		AddSpecials:                settings["add_specials"].(bool),
		AddEpisodeNumbers:          settings["add_episode_numbers"].(bool),
		ShowUnairedSeasons:         settings["unaired_seasons"].(bool),
		ShowUnairedEpisodes:        settings["unaired_episodes"].(bool),
		ShowSeasonsAll:             settings["seasons_all"].(bool),
		ShowSeasonsOrder:           settings["seasons_order"].(int),
		ShowSeasonsSpecials:        settings["seasons_specials"].(bool),
		PlaybackPercent:            settings["playback_percent"].(int),
		SmartEpisodeStart:          settings["smart_episode_start"].(bool),
		SmartEpisodeMatch:          settings["smart_episode_match"].(bool),
		SmartEpisodeChoose:         settings["smart_episode_choose"].(bool),
		LibraryEnabled:             settings["library_enabled"].(bool),
		LibrarySyncEnabled:         settings["library_sync_enabled"].(bool),
		LibrarySyncPlaybackEnabled: settings["library_sync_playback_enabled"].(bool),
		LibraryUpdate:              settings["library_update"].(int),
		StrmLanguage:               settings["strm_language"].(string),
		LibraryNFOMovies:           settings["library_nfo_movies"].(bool),
		LibraryNFOShows:            settings["library_nfo_shows"].(bool),
		SeedForever:                settings["seed_forever"].(bool),
		ShareRatioLimit:            settings["share_ratio_limit"].(int),
		SeedTimeRatioLimit:         settings["seed_time_ratio_limit"].(int),
		SeedTimeLimit:              settings["seed_time_limit"].(int) * 3600,
		DisableUpload:              settings["disable_upload"].(bool),
		DisableLSD:                 settings["disable_lsd"].(bool),
		DisableDHT:                 settings["disable_dht"].(bool),
		DisableTCP:                 settings["disable_tcp"].(bool),
		DisableUTP:                 settings["disable_utp"].(bool),
		DisableUPNP:                settings["disable_upnp"].(bool),
		EncryptionPolicy:           settings["encryption_policy"].(int),
		ListenPortMin:              settings["listen_port_min"].(int),
		ListenPortMax:              settings["listen_port_max"].(int),
		ListenInterfaces:           settings["listen_interfaces"].(string),
		ListenAutoDetectIP:         settings["listen_autodetect_ip"].(bool),
		ListenAutoDetectPort:       settings["listen_autodetect_port"].(bool),
		OutgoingInterfaces:         settings["outgoing_interfaces"].(string),
		TunedStorage:               settings["tuned_storage"].(bool),
		DiskCacheSize:              settings["disk_cache_size"].(int) * 1024 * 1024,
		UseLibtorrentConfig:        settings["use_libtorrent_config"].(bool),
		UseLibtorrentLogging:       settings["use_libtorrent_logging"].(bool),
		UseLibtorrentDeadlines:     settings["use_libtorrent_deadline"].(bool),
		UseLibtorrentPauseResume:   settings["use_libtorrent_pauseresume"].(bool),
		LibtorrentProfile:          settings["libtorrent_profile"].(int),
		MagnetResolveTimeout:       settings["magnet_resolve_timeout"].(int),
		AddExtraTrackers:           settings["add_extra_trackers"].(int),
		RemoveOriginalTrackers:     settings["remove_original_trackers"].(int),
		ConnectionsLimit:           settings["connections_limit"].(int),
		ConnTrackerLimit:           settings["conntracker_limit"].(int),
		ConnTrackerLimitAuto:       settings["conntracker_limit_auto"].(bool),
		SessionSave:                settings["session_save"].(int),
		Scrobble:                   settings["trakt_scrobble"].(bool),

		AutoScrapeEnabled:        settings["autoscrape_is_enabled"].(bool),
		AutoScrapeLibraryEnabled: settings["autoscrape_library_enabled"].(bool),
		AutoScrapeStrategy:       settings["autoscrape_strategy"].(int),
		AutoScrapeStrategyExpect: settings["autoscrape_strategy_expect"].(int),
		AutoScrapePerHours:       settings["autoscrape_per_hours"].(int),
		AutoScrapeLimitMovies:    settings["autoscrape_limit_movies"].(int),
		AutoScrapeInterval:       settings["autoscrape_interval"].(int),

		TraktUsername:                  settings["trakt_username"].(string),
		TraktToken:                     settings["trakt_token"].(string),
		TraktRefreshToken:              settings["trakt_refresh_token"].(string),
		TraktTokenExpiry:               settings["trakt_token_expiry"].(int),
		TraktSyncEnabled:               settings["trakt_sync_enabled"].(bool),
		TraktSyncPlaybackEnabled:       settings["trakt_sync_playback_enabled"].(bool),
		TraktSyncFrequencyMin:          settings["trakt_sync_frequency_min"].(int),
		TraktSyncCollections:           settings["trakt_sync_collections"].(bool),
		TraktSyncWatchlist:             settings["trakt_sync_watchlist"].(bool),
		TraktSyncUserlists:             settings["trakt_sync_userlists"].(bool),
		TraktSyncPlaybackProgress:      settings["trakt_sync_playback_progress"].(bool),
		TraktSyncHidden:                settings["trakt_sync_hidden"].(bool),
		TraktSyncWatched:               settings["trakt_sync_watched"].(bool),
		TraktSyncWatchedBack:           settings["trakt_sync_watchedback"].(bool),
		TraktSyncAddedMovies:           settings["trakt_sync_added_movies"].(bool),
		TraktSyncAddedMoviesLocation:   settings["trakt_sync_added_movies_location"].(int),
		TraktSyncAddedMoviesList:       settings["trakt_sync_added_movies_list"].(int),
		TraktSyncAddedShows:            settings["trakt_sync_added_shows"].(bool),
		TraktSyncAddedShowsLocation:    settings["trakt_sync_added_shows_location"].(int),
		TraktSyncAddedShowsList:        settings["trakt_sync_added_shows_list"].(int),
		TraktSyncRemovedMovies:         settings["trakt_sync_removed_movies"].(bool),
		TraktSyncRemovedMoviesLocation: settings["trakt_sync_removed_movies_location"].(int),
		TraktSyncRemovedMoviesList:     settings["trakt_sync_removed_movies_list"].(int),
		TraktSyncRemovedShows:          settings["trakt_sync_removed_shows"].(bool),
		TraktSyncRemovedShowsLocation:  settings["trakt_sync_removed_shows_location"].(int),
		TraktSyncRemovedShowsList:      settings["trakt_sync_removed_shows_list"].(int),
		TraktProgressUnaired:           settings["trakt_progress_unaired"].(bool),
		TraktProgressSort:              settings["trakt_progress_sort"].(int),
		TraktProgressDateFormat:        settings["trakt_progress_date_format"].(string),
		TraktProgressColorDate:         settings["trakt_progress_color_date"].(string),
		TraktProgressColorShow:         settings["trakt_progress_color_show"].(string),
		TraktProgressColorEpisode:      settings["trakt_progress_color_episode"].(string),
		TraktProgressColorUnaired:      settings["trakt_progress_color_unaired"].(string),
		TraktCalendarsDateFormat:       settings["trakt_calendars_date_format"].(string),
		TraktCalendarsColorDate:        settings["trakt_calendars_color_date"].(string),
		TraktCalendarsColorShow:        settings["trakt_calendars_color_show"].(string),
		TraktCalendarsColorEpisode:     settings["trakt_calendars_color_episode"].(string),
		TraktCalendarsColorUnaired:     settings["trakt_calendars_color_unaired"].(string),

		UpdateFrequency:  settings["library_update_frequency"].(int),
		UpdateDelay:      settings["library_update_delay"].(int),
		UpdateAutoScan:   settings["library_auto_scan"].(bool),
		PlayResumeAction: settings["play_resume_action"].(int),
		PlayResumeBack:   settings["play_resume_back"].(int),
		TMDBApiKey:       settings["tmdb_api_key"].(string),

		OSDBUser:               settings["osdb_user"].(string),
		OSDBPass:               settings["osdb_pass"].(string),
		OSDBLanguage:           settings["osdb_language"].(string),
		OSDBAutoLanguage:       settings["osdb_auto_language"].(bool),
		OSDBAutoLoad:           settings["osdb_auto_load"].(bool),
		OSDBAutoLoadCount:      settings["osdb_auto_load_count"].(int),
		OSDBAutoLoadDelete:     settings["osdb_auto_load_delete"].(bool),
		OSDBAutoLoadSkipExists: settings["osdb_auto_load_skipexists"].(bool),
		OSDBIncludedEnabled:    settings["osdb_included_enabled"].(bool),
		OSDBIncludedSkipExists: settings["osdb_included_skipexists"].(bool),

		SortingModeMovies:           settings["sorting_mode_movies"].(int),
		SortingModeShows:            settings["sorting_mode_shows"].(int),
		ResolutionPreferenceMovies:  settings["resolution_preference_movies"].(int),
		ResolutionPreferenceShows:   settings["resolution_preference_shows"].(int),
		PercentageAdditionalSeeders: settings["percentage_additional_seeders"].(int),

		CustomProviderTimeoutEnabled: settings["custom_provider_timeout_enabled"].(bool),
		CustomProviderTimeout:        settings["custom_provider_timeout"].(int),

		InternalDNSEnabled:  settings["internal_dns_enabled"].(bool),
		InternalDNSSkipIPv6: settings["internal_dns_skip_ipv6"].(bool),

		InternalProxyEnabled:     settings["internal_proxy_enabled"].(bool),
		InternalProxyLogging:     settings["internal_proxy_logging"].(bool),
		InternalProxyLoggingBody: settings["internal_proxy_logging_body"].(bool),

		ProxyType:        settings["proxy_type"].(int),
		ProxyEnabled:     settings["proxy_enabled"].(bool),
		ProxyHost:        settings["proxy_host"].(string),
		ProxyPort:        settings["proxy_port"].(int),
		ProxyLogin:       settings["proxy_login"].(string),
		ProxyPassword:    settings["proxy_password"].(string),
		ProxyUseHTTP:     settings["use_proxy_http"].(bool),
		ProxyUseTracker:  settings["use_proxy_tracker"].(bool),
		ProxyUseDownload: settings["use_proxy_download"].(bool),

		CompletedMove:       settings["completed_move"].(bool),
		CompletedMoviesPath: settings["completed_movies_path"].(string),
		CompletedShowsPath:  settings["completed_shows_path"].(string),

		LocalOnlyClient: settings["local_only_client"].(bool),
		LogLevel:        settings["log_level"].(int),
	}

	updateLoggingLevel(newConfig.LogLevel)

	// Fallback for old configuration with additional storage variants
	if newConfig.DownloadStorage > 1 {
		newConfig.DownloadStorage = 1
	}

	// For memory storage we are changing configuration
	// 	to stop downloading after playback has stopped and so on
	if newConfig.DownloadStorage == 1 {
		// TODO: Do we need this?
		// newConfig.SeedTimeLimit = 24 * 60 * 60
		// newConfig.SeedTimeRatioLimit = 10000
		// newConfig.ShareRatioLimit = 10000

		// Calculate possible memory size, depending of selected strategy
		if newConfig.AutoMemorySize {
			if newConfig.AutoMemorySizeStrategy == 0 {
				newConfig.MemorySize = defaultAutoMemorySize
			} else {
				pct := uint64(8)
				if newConfig.AutoMemorySizeStrategy == 2 {
					pct = 15
				}

				mem := memory.TotalMemory() / 100 * pct
				if mem > 0 {
					newConfig.MemorySize = int(mem)
				}
				log.Debugf("Total system memory: %s\n", humanize.Bytes(memory.TotalMemory()))
				log.Debugf("Automatically selected memory size: %s\n", humanize.Bytes(uint64(newConfig.MemorySize)))
				if newConfig.MemorySize > maxMemorySize {
					log.Debugf("Selected memory size (%s) is bigger than maximum for auto-select (%s), so we decrease memory size to maximum allowed: %s", humanize.Bytes(uint64(mem)), humanize.Bytes(uint64(maxMemorySize)), humanize.Bytes(uint64(maxMemorySize)))
					newConfig.MemorySize = maxMemorySize
				}
			}
		}
	}

	// Set default Trakt Frequency
	if newConfig.TraktToken != "" && newConfig.TraktSyncFrequencyMin == 0 {
		newConfig.TraktSyncFrequencyMin = defaultTraktSyncFrequencyMin
	}

	// Setup OSDB language
	if newConfig.OSDBAutoLanguage || newConfig.OSDBLanguage == "" {
		newConfig.OSDBLanguage = newConfig.Language
	}

	// Collect proxy settings
	if newConfig.ProxyEnabled && newConfig.ProxyHost != "" {
		newConfig.ProxyURL = proxyTypes[newConfig.ProxyType] + "://"
		if newConfig.ProxyLogin != "" || newConfig.ProxyPassword != "" {
			newConfig.ProxyURL += newConfig.ProxyLogin + ":" + newConfig.ProxyPassword + "@"
		}

		newConfig.ProxyURL += newConfig.ProxyHost + ":" + strconv.Itoa(newConfig.ProxyPort)
	}

	// Reading Kodi's advancedsettings file for MemorySize variable to avoid waiting for playback
	// after Elementum's buffer is finished.
	newConfig.KodiBufferSize = getKodiBufferSize()
	if newConfig.AutoKodiBufferSize && newConfig.KodiBufferSize > newConfig.BufferSize {
		newConfig.BufferSize = newConfig.KodiBufferSize
		log.Debugf("Adjusting buffer size according to Kodi advancedsettings.xml configuration to %s", humanize.Bytes(uint64(newConfig.BufferSize)))
	}
	if newConfig.EndBufferSize < defaultEndBufferSize {
		newConfig.EndBufferSize = defaultEndBufferSize
	}

	// Read Strm Language settings and cut-off ISO value
	if strings.Contains(newConfig.StrmLanguage, " | ") {
		tokens := strings.Split(newConfig.StrmLanguage, " | ")
		if len(tokens) == 2 {
			newConfig.StrmLanguage = tokens[1]
		} else {
			newConfig.StrmLanguage = newConfig.Language
		}
	} else {
		newConfig.StrmLanguage = newConfig.Language
	}

	if newConfig.SessionSave == 0 {
		newConfig.SessionSave = 10
	}

	if newConfig.DiskCacheSize == 0 {
		newConfig.DiskCacheSize = defaultDiskCacheSize
	}

	if newConfig.AutoYesEnabled {
		xbmc.DialogAutoclose = newConfig.AutoYesTimeout
	} else {
		xbmc.DialogAutoclose = 1200
	}

	lock.Lock()
	config = &newConfig
	lock.Unlock()
	go CheckBurst()

	// Replacing passwords with asterisks
	configOutput := litter.Sdump(config)
	configOutput = privacyRegex.ReplaceAllString(configOutput, `$1: "********"`)

	log.Infof("Using configuration: %s", configOutput)

	return config
}

// AddonIcon ...
func AddonIcon() string {
	return filepath.Join(Get().Info.Path, "icon.png")
}

// AddonResource ...
func AddonResource(args ...string) string {
	return filepath.Join(Get().Info.Path, "resources", filepath.Join(args...))
}

// TranslatePath ...
func TranslatePath(path string) string {
	// Special case for temporary path in Kodi
	if strings.HasPrefix(path, "special://temp/") {
		dir := strings.Replace(path, "special://temp/", "", 1)
		kodiDir := xbmc.TranslatePath("special://temp")
		pathDir := filepath.Join(kodiDir, dir)

		if PathExists(pathDir) {
			return pathDir
		}
		if err := os.MkdirAll(pathDir, 0777); err != nil {
			log.Errorf("Could not create temporary directory: %#v", err)
			return path
		}

		return pathDir
	}

	// Do not translate nfs/smb path
	// if strings.HasPrefix(path, "nfs:") || strings.HasPrefix(path, "smb:") {
	// 	if !strings.HasSuffix(path, "/") {
	// 		path += "/"
	// 	}
	// 	return path
	// }
	return filepath.Dir(xbmc.TranslatePath(path))
}

// PathExists returns whether path exists in OS
func PathExists(path string) bool {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return false
	}

	return true
}

// IsWritablePath ...
func IsWritablePath(path string) error {
	if path == "." {
		return errors.New("Path not set")
	}
	// TODO: Review this after test evidences come
	if strings.HasPrefix(path, "nfs") || strings.HasPrefix(path, "smb") {
		return fmt.Errorf("Network paths are not supported, change %s to a locally mounted path by the OS", path)
	}
	if p, err := os.Stat(path); err != nil || !p.IsDir() {
		if err != nil {
			return err
		}
		return fmt.Errorf("%s is not a valid directory", path)
	}
	writableFile := filepath.Join(path, ".writable")
	writable, err := os.Create(writableFile)
	if err != nil {
		return err
	}
	writable.Close()
	os.Remove(writableFile)
	return nil
}

func waitForSettingsClosed() {
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if !xbmc.AddonSettingsOpened() {
				return
			}
		}
	}
}

// CheckBurst ...
func CheckBurst() {
	// Check for enabled providers and Elementum Burst
	for _, addon := range xbmc.GetAddons("xbmc.python.script", "executable", "all", []string{"name", "version", "enabled"}).Addons {
		if strings.HasPrefix(addon.ID, "script.elementum.") {
			if addon.Enabled {
				return
			}
		}
	}

	for timeout := 0; timeout < 10; timeout++ {
		if xbmc.IsAddonInstalled("repository.elementum") {
			break
		}
		log.Info("Sleeping 1 second while waiting for Elementum repository add-on to be installed")
		time.Sleep(1 * time.Second)
	}

	log.Info("Updating Kodi add-on repositories for Burst...")
	xbmc.UpdateLocalAddons()
	xbmc.UpdateAddonRepos()

	if !Get().SkipBurstSearch && xbmc.DialogConfirmFocused("Elementum", "LOCALIZE[30271]") {
		log.Infof("Triggering Kodi to check for script.elementum.burst plugin")
		xbmc.InstallAddon("script.elementum.burst")

		for timeout := 0; timeout < 30; timeout++ {
			if xbmc.IsAddonInstalled("script.elementum.burst") {
				break
			}
			log.Info("Sleeping 1 second while waiting for script.elementum.burst add-on to be installed")
			time.Sleep(1 * time.Second)
		}

		log.Infof("Checking for existence of script.elementum.burst plugin now")
		if xbmc.IsAddonInstalled("script.elementum.burst") {
			xbmc.SetAddonEnabled("script.elementum.burst", true)
			xbmc.Notify("Elementum", "LOCALIZE[30272]", AddonIcon())
		} else {
			xbmc.Dialog("Elementum", "LOCALIZE[30273]")
		}
	}
}

func findExistingPath(paths []string, addon string) string {
	// We add plugin folder to avoid getting dummy path, we should take care only for real folder
	for _, v := range paths {
		p := filepath.Join(v, addon)
		if _, err := os.Stat(p); err != nil {
			continue
		}

		return v
	}

	return ""
}

func getKodiBufferSize() int {
	xmlFile, err := os.Open(filepath.Join(xbmc.TranslatePath("special://userdata"), "advancedsettings.xml"))
	if err != nil {
		return 0
	}

	defer xmlFile.Close()

	b, _ := ioutil.ReadAll(xmlFile)

	var as *xbmc.AdvancedSettings
	if err = xml.Unmarshal(b, &as); err != nil {
		return 0
	}

	if as.Cache.MemorySizeLegacy > 0 {
		return as.Cache.MemorySizeLegacy
	} else if as.Cache.MemorySize > 0 {
		return as.Cache.MemorySize
	}

	return 0
}

func updateLoggingLevel(level int) {
	if level == 0 {
		logging.SetLevel(logging.CRITICAL, "")
	} else if level == 1 {
		logging.SetLevel(logging.ERROR, "")
	} else if level == 2 {
		logging.SetLevel(logging.INFO, "")
	} else if level == 3 {
		logging.SetLevel(logging.DEBUG, "")
	}

}

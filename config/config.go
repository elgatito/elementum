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
	"github.com/spf13/cast"
)

var log = logging.MustGetLogger("config")
var privacyRegex = regexp.MustCompile(`(?i)(pass|password|token): "(.+?)"`)

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
	ShowEpisodesOnReleaseDay   bool
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
	RemoveOriginalTrackers   bool
	ModifyTrackersStrategy   int
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

type XbmcSettings map[string]interface{}

var (
	config          = &Configuration{}
	lock            = sync.RWMutex{}
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

		RemoteHost string `help:"remote host"`
		RemotePort int    `help:"remote port"`

		LocalHost string `help:"local host"`
		LocalPort int    `help:"local port"`
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
	xbmc.XBMCExJSONRPCPort = strconv.Itoa(Args.RemotePort)

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
	settings := XbmcSettings{}
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
		DownloadStorage:            settings.ToInt("download_storage"),
		SkipBurstSearch:            settings.ToBool("skip_burst_search"),
		AutoMemorySize:             settings.ToBool("auto_memory_size"),
		AutoAdjustMemorySize:       settings.ToBool("auto_adjust_memory_size"),
		AutoMemorySizeStrategy:     settings.ToInt("auto_memory_size_strategy"),
		MemorySize:                 settings.ToInt("memory_size") * 1024 * 1024,
		AutoKodiBufferSize:         settings.ToBool("auto_kodi_buffer_size"),
		AutoAdjustBufferSize:       settings.ToBool("auto_adjust_buffer_size"),
		MinCandidateSize:           int64(settings.ToInt("min_candidate_size") * 1024 * 1024),
		MinCandidateShowSize:       int64(settings.ToInt("min_candidate_show_size") * 1024 * 1024),
		BufferTimeout:              settings.ToInt("buffer_timeout"),
		BufferSize:                 settings.ToInt("buffer_size") * 1024 * 1024,
		EndBufferSize:              settings.ToInt("end_buffer_size") * 1024 * 1024,
		UploadRateLimit:            settings.ToInt("max_upload_rate") * 1024,
		DownloadRateLimit:          settings.ToInt("max_download_rate") * 1024,
		AutoloadTorrents:           settings.ToBool("autoload_torrents"),
		AutoloadTorrentsPaused:     settings.ToBool("autoload_torrents_paused"),
		SpoofUserAgent:             settings.ToInt("spoof_user_agent"),
		LimitAfterBuffering:        settings.ToBool("limit_after_buffering"),
		DownloadFileStrategy:       settings.ToInt("download_file_strategy"),
		KeepDownloading:            settings.ToInt("keep_downloading"),
		KeepFilesPlaying:           settings.ToInt("keep_files_playing"),
		KeepFilesFinished:          settings.ToInt("keep_files_finished"),
		UseTorrentHistory:          settings.ToBool("use_torrent_history"),
		TorrentHistorySize:         settings.ToInt("torrent_history_size"),
		UseFanartTv:                settings.ToBool("use_fanart_tv"),
		DisableBgProgress:          settings.ToBool("disable_bg_progress"),
		DisableBgProgressPlayback:  settings.ToBool("disable_bg_progress_playback"),
		ForceUseTrakt:              settings.ToBool("force_use_trakt"),
		UseCacheSelection:          settings.ToBool("use_cache_selection"),
		UseCacheSearch:             settings.ToBool("use_cache_search"),
		UseCacheTorrents:           settings.ToBool("use_cache_torrents"),
		CacheSearchDuration:        settings.ToInt("cache_search_duration"),
		ResultsPerPage:             settings.ToInt("results_per_page"),
		ShowFilesWatched:           settings.ToBool("show_files_watched"),
		GreetingEnabled:            settings.ToBool("greeting_enabled"),
		EnableOverlayStatus:        settings.ToBool("enable_overlay_status"),
		SilentStreamStart:          settings.ToBool("silent_stream_start"),
		AutoYesEnabled:             settings.ToBool("autoyes_enabled"),
		AutoYesTimeout:             settings.ToInt("autoyes_timeout"),
		ChooseStreamAutoMovie:      settings.ToBool("choose_stream_auto_movie"),
		ChooseStreamAutoShow:       settings.ToBool("choose_stream_auto_show"),
		ChooseStreamAutoSearch:     settings.ToBool("choose_stream_auto_search"),
		ForceLinkType:              settings.ToBool("force_link_type"),
		UseOriginalTitle:           settings.ToBool("use_original_title"),
		UseAnimeEnTitle:            settings.ToBool("use_anime_en_title"),
		UseLowestReleaseDate:       settings.ToBool("use_lowest_release_date"),
		AddSpecials:                settings.ToBool("add_specials"),
		AddEpisodeNumbers:          settings.ToBool("add_episode_numbers"),
		ShowUnairedSeasons:         settings.ToBool("unaired_seasons"),
		ShowUnairedEpisodes:        settings.ToBool("unaired_episodes"),
		ShowEpisodesOnReleaseDay:   settings.ToBool("show_episodes_on_release_day"),
		ShowSeasonsAll:             settings.ToBool("seasons_all"),
		ShowSeasonsOrder:           settings.ToInt("seasons_order"),
		ShowSeasonsSpecials:        settings.ToBool("seasons_specials"),
		PlaybackPercent:            settings.ToInt("playback_percent"),
		SmartEpisodeStart:          settings.ToBool("smart_episode_start"),
		SmartEpisodeMatch:          settings.ToBool("smart_episode_match"),
		SmartEpisodeChoose:         settings.ToBool("smart_episode_choose"),
		LibraryEnabled:             settings.ToBool("library_enabled"),
		LibrarySyncEnabled:         settings.ToBool("library_sync_enabled"),
		LibrarySyncPlaybackEnabled: settings.ToBool("library_sync_playback_enabled"),
		LibraryUpdate:              settings.ToInt("library_update"),
		StrmLanguage:               settings.ToString("strm_language"),
		LibraryNFOMovies:           settings.ToBool("library_nfo_movies"),
		LibraryNFOShows:            settings.ToBool("library_nfo_shows"),
		SeedForever:                settings.ToBool("seed_forever"),
		ShareRatioLimit:            settings.ToInt("share_ratio_limit"),
		SeedTimeRatioLimit:         settings.ToInt("seed_time_ratio_limit"),
		SeedTimeLimit:              settings.ToInt("seed_time_limit") * 3600,
		DisableUpload:              settings.ToBool("disable_upload"),
		DisableLSD:                 settings.ToBool("disable_lsd"),
		DisableDHT:                 settings.ToBool("disable_dht"),
		DisableTCP:                 settings.ToBool("disable_tcp"),
		DisableUTP:                 settings.ToBool("disable_utp"),
		DisableUPNP:                settings.ToBool("disable_upnp"),
		EncryptionPolicy:           settings.ToInt("encryption_policy"),
		ListenPortMin:              settings.ToInt("listen_port_min"),
		ListenPortMax:              settings.ToInt("listen_port_max"),
		ListenInterfaces:           settings.ToString("listen_interfaces"),
		ListenAutoDetectIP:         settings.ToBool("listen_autodetect_ip"),
		ListenAutoDetectPort:       settings.ToBool("listen_autodetect_port"),
		OutgoingInterfaces:         settings.ToString("outgoing_interfaces"),
		TunedStorage:               settings.ToBool("tuned_storage"),
		DiskCacheSize:              settings.ToInt("disk_cache_size") * 1024 * 1024,
		UseLibtorrentConfig:        settings.ToBool("use_libtorrent_config"),
		UseLibtorrentLogging:       settings.ToBool("use_libtorrent_logging"),
		UseLibtorrentDeadlines:     settings.ToBool("use_libtorrent_deadline"),
		UseLibtorrentPauseResume:   settings.ToBool("use_libtorrent_pauseresume"),
		LibtorrentProfile:          settings.ToInt("libtorrent_profile"),
		MagnetResolveTimeout:       settings.ToInt("magnet_resolve_timeout"),
		AddExtraTrackers:           settings.ToInt("add_extra_trackers"),
		RemoveOriginalTrackers:     settings.ToBool("remove_original_trackers"),
		ModifyTrackersStrategy:     settings.ToInt("modify_trackers_strategy"),
		ConnectionsLimit:           settings.ToInt("connections_limit"),
		ConnTrackerLimit:           settings.ToInt("conntracker_limit"),
		ConnTrackerLimitAuto:       settings.ToBool("conntracker_limit_auto"),
		SessionSave:                settings.ToInt("session_save"),
		Scrobble:                   settings.ToBool("trakt_scrobble"),

		AutoScrapeEnabled:        settings.ToBool("autoscrape_is_enabled"),
		AutoScrapeLibraryEnabled: settings.ToBool("autoscrape_library_enabled"),
		AutoScrapeStrategy:       settings.ToInt("autoscrape_strategy"),
		AutoScrapeStrategyExpect: settings.ToInt("autoscrape_strategy_expect"),
		AutoScrapePerHours:       settings.ToInt("autoscrape_per_hours"),
		AutoScrapeLimitMovies:    settings.ToInt("autoscrape_limit_movies"),
		AutoScrapeInterval:       settings.ToInt("autoscrape_interval"),

		TraktUsername:                  settings.ToString("trakt_username"),
		TraktToken:                     settings.ToString("trakt_token"),
		TraktRefreshToken:              settings.ToString("trakt_refresh_token"),
		TraktTokenExpiry:               settings.ToInt("trakt_token_expiry"),
		TraktSyncEnabled:               settings.ToBool("trakt_sync_enabled"),
		TraktSyncPlaybackEnabled:       settings.ToBool("trakt_sync_playback_enabled"),
		TraktSyncFrequencyMin:          settings.ToInt("trakt_sync_frequency_min"),
		TraktSyncCollections:           settings.ToBool("trakt_sync_collections"),
		TraktSyncWatchlist:             settings.ToBool("trakt_sync_watchlist"),
		TraktSyncUserlists:             settings.ToBool("trakt_sync_userlists"),
		TraktSyncPlaybackProgress:      settings.ToBool("trakt_sync_playback_progress"),
		TraktSyncHidden:                settings.ToBool("trakt_sync_hidden"),
		TraktSyncWatched:               settings.ToBool("trakt_sync_watched"),
		TraktSyncWatchedBack:           settings.ToBool("trakt_sync_watchedback"),
		TraktSyncAddedMovies:           settings.ToBool("trakt_sync_added_movies"),
		TraktSyncAddedMoviesLocation:   settings.ToInt("trakt_sync_added_movies_location"),
		TraktSyncAddedMoviesList:       settings.ToInt("trakt_sync_added_movies_list"),
		TraktSyncAddedShows:            settings.ToBool("trakt_sync_added_shows"),
		TraktSyncAddedShowsLocation:    settings.ToInt("trakt_sync_added_shows_location"),
		TraktSyncAddedShowsList:        settings.ToInt("trakt_sync_added_shows_list"),
		TraktSyncRemovedMovies:         settings.ToBool("trakt_sync_removed_movies"),
		TraktSyncRemovedMoviesLocation: settings.ToInt("trakt_sync_removed_movies_location"),
		TraktSyncRemovedMoviesList:     settings.ToInt("trakt_sync_removed_movies_list"),
		TraktSyncRemovedShows:          settings.ToBool("trakt_sync_removed_shows"),
		TraktSyncRemovedShowsLocation:  settings.ToInt("trakt_sync_removed_shows_location"),
		TraktSyncRemovedShowsList:      settings.ToInt("trakt_sync_removed_shows_list"),
		TraktProgressUnaired:           settings.ToBool("trakt_progress_unaired"),
		TraktProgressSort:              settings.ToInt("trakt_progress_sort"),
		TraktProgressDateFormat:        settings.ToString("trakt_progress_date_format"),
		TraktProgressColorDate:         settings.ToString("trakt_progress_color_date"),
		TraktProgressColorShow:         settings.ToString("trakt_progress_color_show"),
		TraktProgressColorEpisode:      settings.ToString("trakt_progress_color_episode"),
		TraktProgressColorUnaired:      settings.ToString("trakt_progress_color_unaired"),
		TraktCalendarsDateFormat:       settings.ToString("trakt_calendars_date_format"),
		TraktCalendarsColorDate:        settings.ToString("trakt_calendars_color_date"),
		TraktCalendarsColorShow:        settings.ToString("trakt_calendars_color_show"),
		TraktCalendarsColorEpisode:     settings.ToString("trakt_calendars_color_episode"),
		TraktCalendarsColorUnaired:     settings.ToString("trakt_calendars_color_unaired"),

		UpdateFrequency:  settings.ToInt("library_update_frequency"),
		UpdateDelay:      settings.ToInt("library_update_delay"),
		UpdateAutoScan:   settings.ToBool("library_auto_scan"),
		PlayResumeAction: settings.ToInt("play_resume_action"),
		PlayResumeBack:   settings.ToInt("play_resume_back"),
		TMDBApiKey:       settings.ToString("tmdb_api_key"),

		OSDBUser:               settings.ToString("osdb_user"),
		OSDBPass:               settings.ToString("osdb_pass"),
		OSDBLanguage:           settings.ToString("osdb_language"),
		OSDBAutoLanguage:       settings.ToBool("osdb_auto_language"),
		OSDBAutoLoad:           settings.ToBool("osdb_auto_load"),
		OSDBAutoLoadCount:      settings.ToInt("osdb_auto_load_count"),
		OSDBAutoLoadDelete:     settings.ToBool("osdb_auto_load_delete"),
		OSDBAutoLoadSkipExists: settings.ToBool("osdb_auto_load_skipexists"),
		OSDBIncludedEnabled:    settings.ToBool("osdb_included_enabled"),
		OSDBIncludedSkipExists: settings.ToBool("osdb_included_skipexists"),

		SortingModeMovies:           settings.ToInt("sorting_mode_movies"),
		SortingModeShows:            settings.ToInt("sorting_mode_shows"),
		ResolutionPreferenceMovies:  settings.ToInt("resolution_preference_movies"),
		ResolutionPreferenceShows:   settings.ToInt("resolution_preference_shows"),
		PercentageAdditionalSeeders: settings.ToInt("percentage_additional_seeders"),

		CustomProviderTimeoutEnabled: settings.ToBool("custom_provider_timeout_enabled"),
		CustomProviderTimeout:        settings.ToInt("custom_provider_timeout"),

		InternalDNSEnabled:  settings.ToBool("internal_dns_enabled"),
		InternalDNSSkipIPv6: settings.ToBool("internal_dns_skip_ipv6"),

		InternalProxyEnabled:     settings.ToBool("internal_proxy_enabled"),
		InternalProxyLogging:     settings.ToBool("internal_proxy_logging"),
		InternalProxyLoggingBody: settings.ToBool("internal_proxy_logging_body"),

		ProxyType:        settings.ToInt("proxy_type"),
		ProxyEnabled:     settings.ToBool("proxy_enabled"),
		ProxyHost:        settings.ToString("proxy_host"),
		ProxyPort:        settings.ToInt("proxy_port"),
		ProxyLogin:       settings.ToString("proxy_login"),
		ProxyPassword:    settings.ToString("proxy_password"),
		ProxyUseHTTP:     settings.ToBool("use_proxy_http"),
		ProxyUseTracker:  settings.ToBool("use_proxy_tracker"),
		ProxyUseDownload: settings.ToBool("use_proxy_download"),

		CompletedMove:       settings.ToBool("completed_move"),
		CompletedMoviesPath: settings.ToString("completed_movies_path"),
		CompletedShowsPath:  settings.ToString("completed_shows_path"),

		LocalOnlyClient: settings.ToBool("local_only_client"),
		LogLevel:        settings.ToInt("log_level"),
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

func (s *XbmcSettings) ToString(key string) (ret string) {
	if _, ok := (*s)[key]; !ok {
		log.Errorf("Setting '%s' not found!", key)
		return ""
	}

	var err error
	if ret, err = cast.ToStringE((*s)[key]); err != nil {
		log.Errorf("Error casting property '%s' with value '%s' to 'string': %s", key, (*s)[key], err)
	}
	return
}

func (s *XbmcSettings) ToInt(key string) (ret int) {
	if _, ok := (*s)[key]; !ok {
		log.Errorf("Setting '%s' not found!", key)
		return 0
	}

	var err error
	if ret, err = cast.ToIntE((*s)[key]); err != nil {
		log.Errorf("Error casting property '%s' with value '%s' to 'int': %s", key, (*s)[key], err)
	}
	return
}

func (s *XbmcSettings) ToInt64(key string) (ret int64) {
	if _, ok := (*s)[key]; !ok {
		log.Errorf("Setting '%s' not found!", key)
		return 0
	}

	var err error
	if ret, err = cast.ToInt64E((*s)[key]); err != nil {
		log.Errorf("Error casting property '%s' with value '%s' to 'int64': %s", key, (*s)[key], err)
	}
	return
}

func (s *XbmcSettings) ToBool(key string) (ret bool) {
	if _, ok := (*s)[key]; !ok {
		log.Errorf("Setting '%s' not found!", key)
		return false
	}

	var err error
	if ret, err = cast.ToBoolE((*s)[key]); err != nil {
		log.Errorf("Error casting property '%s' with value '%s' to 'bool': %s", key, (*s)[key], err)
	}
	return
}

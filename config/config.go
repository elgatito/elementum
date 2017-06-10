package config

import (
	"os"
	"fmt"
	"sync"
	"time"
	"errors"
	"strings"
	"strconv"
	"path/filepath"

	"github.com/op/go-logging"
	"github.com/scakemyer/quasar/xbmc"
)

var log = logging.MustGetLogger("config")

type Configuration struct {
	DownloadPath        string
	TorrentsPath        string
	LibraryPath         string
	Info                *xbmc.AddonInfo
	Platform            *xbmc.Platform
	Language            string
	ProfilePath         string
	SpoofUserAgent      int
	KeepDownloading     int
	KeepFilesPlaying    int
	KeepFilesFinished   int
	DisableBgProgress   bool
	ResultsPerPage      int
	EnableOverlayStatus bool
	ChooseStreamAuto    bool
	UseOriginalTitle    bool
	AddSpecials         bool
	ShowUnairedSeasons  bool
	ShowUnairedEpisodes bool
	DownloadStorage     int
	MemorySize          int
	BufferSize          int
	UploadRateLimit     int
	DownloadRateLimit   int
	LimitAfterBuffering bool
	ConnectionsLimit    int
	// SessionSave         int
	// ShareRatioLimit     int
	// SeedTimeRatioLimit  int
	SeedTimeLimit       int
	DisableDHT          bool
	// DisableUPNP         bool
	EncryptionPolicy    int
	BTListenPortMin     int
	BTListenPortMax     int
	ListenInterfaces    string
	OutgoingInterfaces  string
	// TunedStorage        bool
	Scrobble            bool
	TraktUsername       string
	TraktToken          string
	TraktRefreshToken   string
	TraktTokenExpiry    int
	TraktSyncFrequency  int
	UpdateFrequency     int
	UpdateDelay         int
	UpdateAutoScan      bool
	TvScraper           int
	LibraryResume       int
	UseCloudHole        bool
	CloudHoleKey        string
	TMDBApiKey          string
	OSDBUser            string
	OSDBPass            string

	SortingModeMovies            int
	SortingModeShows             int
	ResolutionPreferenceMovies   int
	ResolutionPreferenceShows    int
	PercentageAdditionalSeeders  int

	CustomProviderTimeoutEnabled bool
	CustomProviderTimeout        int

	// ProxyType     int
	// SocksEnabled  bool
	// SocksHost     string
	// SocksPort     int
	// SocksLogin    string
	// SocksPassword string

	CompletedMove       bool
	CompletedMoviesPath string
	CompletedShowsPath  string
}

type Addon struct {
	ID      string
	Name    string
	Version string
	Enabled bool
}

var config = &Configuration{}
var lock = sync.RWMutex{}
var settingsSet = false

const (
	ListenPort = 65251
)

func Get() *Configuration {
	lock.RLock()
	defer lock.RUnlock()
	return config
}

func Reload() *Configuration {
	log.Info("Reloading configuration...")

	info := xbmc.GetAddonInfo()
	info.Path = xbmc.TranslatePath(info.Path)
	info.Profile = xbmc.TranslatePath(info.Profile)
	info.TempPath = filepath.Join(xbmc.TranslatePath("special://temp"), "quasar")
	platform := xbmc.GetPlatform()

	os.RemoveAll(info.TempPath)
	os.MkdirAll(info.TempPath, 0777)

	if platform.OS == "android" {
		legacyPath := strings.Replace(info.Path, "/storage/emulated/0", "/storage/emulated/legacy", 1)
		if _, err := os.Stat(legacyPath); err == nil {
			info.Path = legacyPath
			info.Profile = strings.Replace(info.Profile, "/storage/emulated/0", "/storage/emulated/legacy", 1)
			log.Info("Using /storage/emulated/legacy path.")
		}
	}

	downloadPath := filepath.Dir(xbmc.TranslatePath(xbmc.GetSettingString("download_path")))
	if downloadPath == "." {
		xbmc.Dialog("Quasar", "LOCALIZE[30113]")
		xbmc.AddonSettings("plugin.video.quasar")
		go waitSettingsSet()
	} else if err := IsWritablePath(downloadPath); err != nil {
		log.Errorf("Cannot write to location '%s': %#v", downloadPath, err)
		xbmc.Dialog("Quasar", err.Error())
		xbmc.AddonSettings("plugin.video.quasar")
	} else {
		settingsSet = true
	}
	log.Infof("Using download path: %s", downloadPath)

	libraryPath := filepath.Dir(xbmc.TranslatePath(xbmc.GetSettingString("library_path")))
	if libraryPath == "." {
		libraryPath = downloadPath
	} else if err := IsWritablePath(libraryPath); err != nil {
		log.Error(err)
		xbmc.Dialog("Quasar", err.Error())
		xbmc.AddonSettings("plugin.video.quasar")
	}
	log.Infof("Using library path: %s", libraryPath)

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

	defer func() {
		if r := recover(); r != nil {
			log.Warningf("Addon settings not properly set, opening settings window: %#v", r)
			xbmc.Dialog("Quasar", "LOCALIZE[30314]")
			xbmc.AddonSettings("plugin.video.quasar")
			os.Exit(0)
	 	}
	}()

	newConfig := Configuration{
		DownloadPath:        downloadPath,
		LibraryPath:         libraryPath,
		TorrentsPath:        filepath.Join(downloadPath, "Torrents"),
		Info:                info,
		Platform:            platform,
		Language:            xbmc.GetLanguageISO_639_1(),
		ProfilePath:         info.Profile,
		DownloadStorage:     settings["download_storage"].(int),
		MemorySize:          settings["memory_size"].(int) * 1024 * 1024,
		BufferSize:          settings["buffer_size"].(int) * 1024 * 1024,
		UploadRateLimit:     settings["max_upload_rate"].(int) * 1024,
		DownloadRateLimit:   settings["max_download_rate"].(int) * 1024,
		SpoofUserAgent:      settings["spoof_user_agent"].(int),
		LimitAfterBuffering: settings["limit_after_buffering"].(bool),
		KeepDownloading:     settings["keep_downloading"].(int),
		KeepFilesPlaying:    settings["keep_files_playing"].(int),
		KeepFilesFinished:   settings["keep_files_finished"].(int),
		DisableBgProgress:   settings["disable_bg_progress"].(bool),
		ResultsPerPage:      settings["results_per_page"].(int),
		EnableOverlayStatus: settings["enable_overlay_status"].(bool),
		ChooseStreamAuto:    settings["choose_stream_auto"].(bool),
		UseOriginalTitle:    settings["use_original_title"].(bool),
		AddSpecials:         settings["add_specials"].(bool),
		ShowUnairedSeasons:  settings["unaired_seasons"].(bool),
		ShowUnairedEpisodes: settings["unaired_episodes"].(bool),
		// ShareRatioLimit:     settings["share_ratio_limit"].(int),
		// SeedTimeRatioLimit:  settings["seed_time_ratio_limit"].(int),
		SeedTimeLimit:       settings["seed_time_limit"].(int),
		DisableDHT:          settings["disable_dht"].(bool),
		// DisableUPNP:         settings["disable_upnp"].(bool),
		EncryptionPolicy:    settings["encryption_policy"].(int),
		BTListenPortMin:     settings["listen_port_min"].(int),
		BTListenPortMax:     settings["listen_port_max"].(int),
		ListenInterfaces:    settings["listen_interfaces"].(string),
		OutgoingInterfaces:  settings["outgoing_interfaces"].(string),
		// TunedStorage:        settings["tuned_storage"].(bool),
		ConnectionsLimit:    settings["connections_limit"].(int),
		// SessionSave:         settings["session_save"].(int),
		Scrobble:            settings["trakt_scrobble"].(bool),
		TraktUsername:       settings["trakt_username"].(string),
		TraktToken:          settings["trakt_token"].(string),
		TraktRefreshToken:   settings["trakt_refresh_token"].(string),
		TraktTokenExpiry:    settings["trakt_token_expiry"].(int),
		TraktSyncFrequency:  settings["trakt_sync"].(int),
		UpdateFrequency:     settings["library_update_frequency"].(int),
		UpdateDelay:         settings["library_update_delay"].(int),
		UpdateAutoScan:      settings["library_auto_scan"].(bool),
		TvScraper:           settings["library_tv_scraper"].(int),
		LibraryResume:       settings["library_resume"].(int),
		UseCloudHole:        settings["use_cloudhole"].(bool),
		CloudHoleKey:        settings["cloudhole_key"].(string),
		TMDBApiKey:          settings["tmdb_api_key"].(string),
		OSDBUser:            settings["osdb_user"].(string),
		OSDBPass:            settings["osdb_pass"].(string),

		SortingModeMovies:            settings["sorting_mode_movies"].(int),
		SortingModeShows:             settings["sorting_mode_shows"].(int),
		ResolutionPreferenceMovies:   settings["resolution_preference_movies"].(int),
		ResolutionPreferenceShows:    settings["resolution_preference_shows"].(int),
		PercentageAdditionalSeeders:  settings["percentage_additional_seeders"].(int),

		CustomProviderTimeoutEnabled: settings["custom_provider_timeout_enabled"].(bool),
		CustomProviderTimeout:        settings["custom_provider_timeout"].(int),

		// ProxyType:     settings["proxy_type"].(int),
		// SocksEnabled:  settings["socks_enabled"].(bool),
		// SocksHost:     settings["socks_host"].(string),
		// SocksPort:     settings["socks_port"].(int),
		// SocksLogin:    settings["socks_login"].(string),
		// SocksPassword: settings["socks_password"].(string),

		CompletedMove:       settings["completed_move"].(bool),
		CompletedMoviesPath: settings["completed_movies_path"].(string),
		CompletedShowsPath:  settings["completed_shows_path"].(string),
	}

	// For memory we are changing configuration
	if newConfig.DownloadStorage == 1 {
		newConfig.CompletedMove = false
		newConfig.KeepDownloading = 2
		newConfig.KeepFilesFinished = 2
		newConfig.KeepFilesPlaying = 2
		newConfig.LimitAfterBuffering = false
		newConfig.SeedTimeLimit = 0
	}

	lock.Lock()
	config = &newConfig
	lock.Unlock()

	return config
}

func AddonIcon() string {
	return filepath.Join(Get().Info.Path, "icon.png")
}

func AddonResource(args ...string) string {
	return filepath.Join(Get().Info.Path, "resources", filepath.Join(args...))
}

func IsWritablePath(path string) error {
	if path == "." {
		return errors.New("Path not set")
	}
	if strings.HasPrefix(path, "nfs") || strings.HasPrefix(path, "smb") {
		return errors.New(fmt.Sprintf("Network paths are not supported, change %s to a locally mounted path by the OS", path))
	}
	if p, err := os.Stat(path); err != nil || !p.IsDir() {
		if err != nil {
			return err
		}
	  return errors.New(fmt.Sprintf("%s is not a valid directory", path))
	}
	writableFile := filepath.Join(path, ".writable")
	if writable, err := os.Create(writableFile); err != nil {
		return err
	} else {
		writable.Close()
		os.Remove(writableFile)
	}
	return nil
}

func waitSettingsSet() {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if settingsSet {
				go CheckBurst()
				return
			}
		}
	}
}

func CheckBurst() {
	// Check for enabled providers and Quasar Burst
	hasBurst := false
	enabledProviders := make([]Addon, 0)
	for _, addon := range xbmc.GetAddons("xbmc.python.script", "executable", "all", []string{"name", "version", "enabled"}).Addons {
		if strings.HasPrefix(addon.ID, "script.quasar.") {
			if addon.ID == "script.quasar.burst" && addon.Enabled == true {
				hasBurst = true
			}
			enabledProviders = append(enabledProviders, Addon{
				ID: addon.ID,
				Name: addon.Name,
				Version: addon.Version,
				Enabled: addon.Enabled,
			})
		}
	}
	if !hasBurst {
		log.Info("Updating Kodi add-on repositories for Burst...")
		xbmc.UpdateLocalAddons()
		xbmc.UpdateAddonRepos()
		time.Sleep(10 * time.Second)

		if xbmc.DialogConfirm("Quasar", "LOCALIZE[30271]") {
			xbmc.PlayURL("plugin://script.quasar.burst/")
			for _, addon := range xbmc.GetAddons("xbmc.python.script", "executable", "all", []string{"name", "version", "enabled"}).Addons {
				if addon.ID == "script.quasar.burst" && addon.Enabled == true {
					hasBurst = true
				}
			}
			if hasBurst {
				for _, addon := range enabledProviders {
					xbmc.SetAddonEnabled(addon.ID, false)
				}
				xbmc.Notify("Quasar", "LOCALIZE[30272]", AddonIcon())
			} else {
				xbmc.Dialog("Quasar", "LOCALIZE[30273]")
			}
		}
	}
}

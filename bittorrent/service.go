package bittorrent

import (
	"bufio"
	"bytes"
	"cmp"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"math/rand"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	natpmp "github.com/ElementumOrg/go-nat-pmp"
	"github.com/anacrolix/missinggo/perf"
	"github.com/anacrolix/sync"
	"github.com/cespare/xxhash"
	"github.com/dustin/go-humanize"
	"github.com/gin-gonic/gin"
	"github.com/radovskyb/watcher"
	"github.com/shirou/gopsutil/v4/mem"
	"github.com/zeebo/bencode"

	lt "github.com/ElementumOrg/libtorrent-go"

	"github.com/elgatito/elementum/broadcast"
	"github.com/elgatito/elementum/config"
	"github.com/elgatito/elementum/database"
	"github.com/elgatito/elementum/diskusage"
	"github.com/elgatito/elementum/library"
	"github.com/elgatito/elementum/proxy"
	"github.com/elgatito/elementum/tmdb"
	"github.com/elgatito/elementum/trakt"
	"github.com/elgatito/elementum/util"
	"github.com/elgatito/elementum/util/event"
	"github.com/elgatito/elementum/util/ident"
	"github.com/elgatito/elementum/util/ip"
	"github.com/elgatito/elementum/xbmc"
)

type PortMapping struct {
	Client *natpmp.Client
	Port   int
}

// Service ...
type Service struct {
	config *config.Configuration
	q      *Queue
	mu     sync.Mutex
	wg     sync.WaitGroup

	Session       lt.SessionHandle
	SessionGlobal lt.Session
	PackSettings  lt.SettingsPack

	listenInterfaces   []net.IP
	outgoingInterfaces []net.IP
	portsMu            sync.Mutex
	mappedPorts        map[string]PortMapping

	InternalProxy *proxy.CustomProxy

	Players      map[string]*Player
	SpaceChecked map[string]bool

	UserAgent   string
	PeerID      string
	ListenIP    string
	ListenIPv6  string
	ListenPort  int
	DisableIPv6 bool

	dialogProgressBG *xbmc.DialogProgressBG

	alertsBroadcaster *broadcast.Broadcaster
	Closer            event.Event
	CloserNotifier    event.Event
	isShutdown        bool
}

type activeTorrent struct {
	torrentName  string
	downloadRate float64
	uploadRate   float64
	progress     int
}

// NewService ...
func NewService() *Service {
	now := time.Now()
	defer func() {
		log.Infof("Service started in %s", time.Since(now))
	}()

	s := &Service{
		config: config.Get(),

		SpaceChecked: map[string]bool{},
		Players:      map[string]*Player{},
		mappedPorts:  map[string]PortMapping{},

		alertsBroadcaster: broadcast.NewBroadcaster(),
	}

	s.q = NewQueue(s)

	s.configure()
	if s.Session == nil || s.Session.Swigcptr() == 0 {
		log.Error("Could not start Session")
		return s
	}

	s.wg.Add(5)
	go s.onAlertsConsumer()
	go s.logAlerts()

	go s.startServices()

	go s.watchConfig()
	go s.onSaveResumeDataConsumer()
	go s.onSaveResumeDataWriter()
	go s.networkRefresh()

	go tmdb.CheckAPIKey()

	go func() {
		UpdateDefaultTrackers()
		s.loadTorrentFiles()
	}()
	go s.onDownloadProgress()

	return s
}

// Close ...
func (s *Service) Close(isShutdown bool) {
	now := time.Now()
	defer func() {
		log.Infof("Closed service in %s", time.Since(now))
	}()

	s.isShutdown = isShutdown
	s.Closer.Set()

	log.Info("Stopping Libtorrent Services...")
	s.stopServices()

	log.Info("Stopping Libtorrent session...")
	s.CloseSession()

	s.CloserNotifier.Set()
}

// CloseSession tries to close libtorrent session with a timeout,
// because it takes too much to close and Kodi hangs.
func (s *Service) CloseSession() {
	log.Infof("Waiting for completion of all goroutines")
	s.wg.Wait()

	now := time.Now()
	defer func() {
		log.Infof("Closed session in %s", time.Since(now))
	}()

	log.Info("Closing Session")
	if s.SessionGlobal != nil {
		s.SessionGlobal.Abort()

		if err := lt.DeleteSession(s.SessionGlobal); err != nil {
			log.Errorf("Could not delete libtorrent session: %s", err)
		}
	}
}

// Reconfigure fired every time addon configuration has changed
// and Kodi sent a notification about that.
// Should reassemble Service configuration and restart everything.
// For non-memory storage it should also load old torrent files.
func (s *Service) Reconfigure() {
	s.stopServices()

	config.Reload()
	proxy.Reload()
	UpdateDefaultTrackers()

	s.config = config.Get()
	s.configure()

	s.startServices()

	// After re-configure check Trakt authorization
	if config.Get().TraktToken != "" && !config.Get().TraktAuthorized {
		trakt.GetLastActivities()
	}
}

func (s *Service) configure() {
	log.Info("Configuring client...")

	proxy.Reload()
	if s.config.InternalProxyEnabled {
		s.InternalProxy = proxy.StartProxy()
	}

	if _, err := os.Stat(s.config.TorrentsPath); os.IsNotExist(err) {
		if err := os.Mkdir(s.config.TorrentsPath, 0755); err != nil {
			log.Error("Unable to create Torrents folder")
		}
	}

	settings := lt.NewSettingsPack()

	log.Info("Applying session settings...")

	s.PeerID, s.UserAgent = ident.GetUserAndPeer()
	log.Infof("UserAgent: %s, PeerID: %s", s.UserAgent, s.PeerID)
	settings.SetStr("user_agent", s.UserAgent)
	settings.SetStr("peer_fingerprint", s.PeerID)

	// Bools
	settings.SetBool("announce_to_all_tiers", true)
	settings.SetBool("announce_to_all_trackers", true)
	settings.SetBool("apply_ip_filter_to_trackers", false)
	settings.SetBool("lazy_bitfields", true)
	settings.SetBool("no_atime_storage", true)
	settings.SetBool("no_connect_privileged_ports", false)
	settings.SetBool("prioritize_partial_pieces", false)
	settings.SetBool("rate_limit_ip_overhead", false)
	settings.SetBool("smooth_connects", false)
	settings.SetBool("strict_end_game_mode", true)
	settings.SetBool("upnp_ignore_nonrouters", true)
	settings.SetBool("use_dht_as_fallback", false)
	settings.SetBool("use_parole_mode", true)

	// Enable TCP and uTP in case if they were disabled and then enabled again without restarting Kodi
	settings.SetBool("enable_outgoing_tcp", true)
	settings.SetBool("enable_incoming_tcp", true)
	settings.SetBool("enable_outgoing_utp", true)
	settings.SetBool("enable_incoming_utp", true)

	// Disabling services, as they are enabled by default in libtorrent
	settings.SetBool("enable_upnp", false)
	settings.SetBool("enable_natpmp", false)
	settings.SetBool("enable_lsd", false)
	settings.SetBool("enable_dht", false)

	// settings.SetInt("peer_tos", ipToSLowCost)
	// settings.SetInt("torrent_connect_boost", 20)
	// settings.SetInt("torrent_connect_boost", 100)
	// settings.SetInt("torrent_connect_boost", 0)
	settings.SetInt("aio_threads", runtime.NumCPU()*4)
	settings.SetInt("cache_size", -1)
	settings.SetInt("mixed_mode_algorithm", int(lt.SettingsPackPreferTcp))

	// Intervals and Timeouts
	settings.SetInt("auto_scrape_interval", 1200)
	settings.SetInt("auto_scrape_min_interval", 900)
	settings.SetInt("min_announce_interval", 30)
	settings.SetInt("dht_announce_interval", 60)
	// settings.SetInt("peer_connect_timeout", 5)
	// settings.SetInt("request_timeout", 2)
	settings.SetInt("stop_tracker_timeout", 1)

	// Ratios
	settings.SetInt("seed_time_limit", 0)
	settings.SetInt("seed_time_ratio_limit", 0)
	settings.SetInt("share_ratio_limit", 0)

	// Algorithms
	settings.SetInt("choking_algorithm", int(lt.SettingsPackFixedSlotsChoker))
	settings.SetInt("seed_choking_algorithm", int(lt.SettingsPackFastestUpload))

	// Sizes
	settings.SetInt("request_queue_time", 2)
	settings.SetInt("max_out_request_queue", 5000)
	settings.SetInt("max_allowed_in_request_queue", 5000)
	// settings.SetInt("max_out_request_queue", 60000)
	// settings.SetInt("max_allowed_in_request_queue", 25000)
	// settings.SetInt("listen_queue_size", 2000)
	// settings.SetInt("unchoke_slots_limit", 20)
	settings.SetInt("max_peerlist_size", 50000)
	settings.SetInt("dht_upload_rate_limit", 50000)
	settings.SetInt("max_pex_peers", 200)
	settings.SetInt("max_suggest_pieces", 50)
	settings.SetInt("whole_pieces_threshold", 10)
	// settings.SetInt("aio_threads", 8)

	settings.SetInt("send_buffer_low_watermark", 10*1024)
	settings.SetInt("send_buffer_watermark", 500*1024)
	settings.SetInt("send_buffer_watermark_factor", 50)

	settings.SetInt("download_rate_limit", 0)
	settings.SetInt("upload_rate_limit", 0)

	// For Android external storage / OS-mounted NAS setups
	if s.config.TunedStorage && !s.IsMemoryStorage() {
		settings.SetBool("use_read_cache", true)
		settings.SetBool("coalesce_reads", true)
		settings.SetBool("coalesce_writes", true)
		settings.SetInt("max_queued_disk_bytes", s.config.DiskCacheSize)
	}

	if s.config.ConnectionsLimit > 0 {
		settings.SetInt("connections_limit", s.config.ConnectionsLimit)
	} else {
		settings.SetInt("connections_limit", getPlatformSpecificConnectionLimit())
	}

	if s.config.ConnTrackerLimitAuto || s.config.ConnTrackerLimit == 0 {
		settings.SetInt("connection_speed", 250)
	} else {
		settings.SetInt("connection_speed", s.config.ConnTrackerLimit)
	}

	if !s.config.LimitAfterBuffering {
		if s.config.DownloadRateLimit > 0 {
			log.Infof("Rate limiting download to %s", humanize.Bytes(uint64(s.config.DownloadRateLimit)))
			settings.SetInt("download_rate_limit", s.config.DownloadRateLimit)
		}
		if s.config.UploadRateLimit > 0 {
			log.Infof("Rate limiting upload to %s", humanize.Bytes(uint64(s.config.UploadRateLimit)))
			// If we have an upload rate, use the nicer bittyrant choker
			settings.SetInt("upload_rate_limit", s.config.UploadRateLimit)
			settings.SetInt("choking_algorithm", int(lt.SettingsPackBittyrantChoker))
		}
	}

	// TODO: Enable when it's working!
	// if s.config.DisableUpload {
	// 	s.Session.AddUploadExtension()
	// }

	if !s.config.SeedForever {
		if s.config.ShareRatioLimit > 0 {
			settings.SetInt("share_ratio_limit", s.config.ShareRatioLimit)
		}
		if s.config.SeedTimeRatioLimit > 0 {
			settings.SetInt("seed_time_ratio_limit", s.config.SeedTimeRatioLimit)
		}
		if s.config.SeedTimeLimit > 0 {
			settings.SetInt("seed_time_limit", s.config.SeedTimeLimit)
		}
	}

	log.Info("Applying encryption settings...")
	settings.SetInt("allowed_enc_level", int(lt.SettingsPackPeRc4))
	settings.SetBool("prefer_rc4", true)

	if s.config.EncryptionPolicy > 0 {
		policy := int(lt.SettingsPackPeDisabled)
		level := int(lt.SettingsPackPeBoth)
		preferRc4 := false

		if s.config.EncryptionPolicy == 2 {
			policy = int(lt.SettingsPackPeForced)
			level = int(lt.SettingsPackPeRc4)
			preferRc4 = true
		}

		settings.SetInt("out_enc_policy", policy)
		settings.SetInt("in_enc_policy", policy)
		settings.SetInt("allowed_enc_level", level)
		settings.SetBool("prefer_rc4", preferRc4)
	}

	settings.SetInt("proxy_type", ProxyTypeNone)
	if s.config.ProxyEnabled && s.config.ProxyHost != "" {
		log.Info("Applying proxy settings...")
		if s.config.ProxyType == 0 {
			settings.SetInt("proxy_type", ProxyTypeSocks4)
		} else if s.config.ProxyType == 1 {
			settings.SetInt("proxy_type", ProxyTypeSocks5)
			if s.config.ProxyLogin != "" || s.config.ProxyPassword != "" {
				settings.SetInt("proxy_type", ProxyTypeSocks5Password)
			}
		} else if s.config.ProxyType == 2 {
			settings.SetInt("proxy_type", ProxyTypeSocksHTTP)
			if s.config.ProxyLogin != "" || s.config.ProxyPassword != "" {
				settings.SetInt("proxy_type", ProxyTypeSocksHTTPPassword)
			}
		} else if s.config.ProxyType == 3 {
			settings.SetInt("proxy_type", ProxyTypeI2PSAM)
			settings.SetInt("i2p_port", s.config.ProxyPort)
			settings.SetStr("i2p_hostname", s.config.ProxyHost)
			settings.SetBool("allows_i2p_mixed", true)
		}

		settings.SetInt("proxy_port", s.config.ProxyPort)
		settings.SetStr("proxy_hostname", s.config.ProxyHost)
		settings.SetStr("proxy_username", s.config.ProxyLogin)
		settings.SetStr("proxy_password", s.config.ProxyPassword)

		// Proxy files downloads
		settings.SetBool("proxy_peer_connections", config.Get().ProxyUseDownload)
		settings.SetBool("proxy_hostnames", config.Get().ProxyUseDownload)

		// Proxy Tracker connections
		settings.SetBool("proxy_tracker_connections", config.Get().ProxyUseTracker)

		// ensure no leakage, this may break download
		settings.SetBool("force_proxy", config.Get().ProxyForce)
	}

	// Set alert_mask here so it also applies on reconfigure...
	settings.SetInt("alert_mask", int(
		lt.AlertStatusNotification|
			lt.AlertStorageNotification|
			lt.AlertErrorNotification|
			lt.AlertPerformanceWarning|
			lt.AlertTrackerNotification))

	if s.config.UseLibtorrentLogging {
		settings.SetInt("alert_mask", int(lt.AlertAllCategories))
		settings.SetInt("alert_queue_size", 2500)
	}

	log.Infof("DownloadStorage: %s", config.Storages[s.config.DownloadStorage])
	if s.IsMemoryStorage() {
		needSize := s.config.BufferSize + int(s.config.EndBufferSize) + 8*1024*1024

		if config.Get().MemorySize < needSize {
			log.Noticef("Raising memory size (%d) to fit all the buffer (%d)", config.Get().MemorySize, needSize)
			config.Get().MemorySize = needSize
		}

		// Set Memory storage specific settings
		settings.SetBool("close_redundant_connections", false)

		settings.SetInt("share_ratio_limit", 0)
		settings.SetInt("seed_time_ratio_limit", 0)
		settings.SetInt("seed_time_limit", 0)

		settings.SetInt("active_downloads", -1)
		settings.SetInt("active_seeds", -1)
		settings.SetInt("active_limit", -1)
		settings.SetInt("active_tracker_limit", -1)
		settings.SetInt("active_dht_limit", -1)
		settings.SetInt("active_lsd_limit", -1)
		// settings.SetInt("read_cache_line_size", 0)
		// settings.SetInt("unchoke_slots_limit", 0)

		// settings.SetInt("request_timeout", 10)
		// settings.SetInt("peer_connect_timeout", 10)

		settings.SetInt("max_out_request_queue", 100000)
		settings.SetInt("max_allowed_in_request_queue", 100000)

		// settings.SetInt("initial_picker_threshold", 20)
		// settings.SetInt("share_mode_target", 1)
		settings.SetBool("use_read_cache", false)
		settings.SetBool("auto_sequential", false)

		// settings.SetInt("tick_interval", 300)
		// settings.SetBool("strict_end_game_mode", false)

		// settings.SetInt("disk_io_write_mode", 2)
		// settings.SetInt("disk_io_read_mode", 2)
		settings.SetInt("cache_size", 0)

		// Adjust timeouts to avoid disconnect due to idling connections
		settings.SetInt("inactivity_timeout", 60*30)
		settings.SetInt("peer_timeout", 60*10)
		settings.SetInt("min_reconnect_time", 20)
	}

	listenInterfaces, outgoingInterfaces, errInterface := s.getInterfaceSettings()
	if errInterface != nil {
		log.Errorf("Could not create configure libtorrent session due to wrong interfaces configuration: %s", errInterface)
		return
	}

	settings.SetStr("listen_interfaces", strings.Join(listenInterfaces, ","))
	log.Infof("Libtorrent listen_interfaces set to: %s", strings.Join(listenInterfaces, ","))

	if len(outgoingInterfaces) > 0 {
		settings.SetStr("outgoing_interfaces", strings.Join(outgoingInterfaces, ","))
		log.Infof("Libtorrent outgoing_interfaces set to: %s", strings.Join(outgoingInterfaces, ","))
	}

	if config.Get().LibtorrentProfile == profileMinMemory {
		log.Info("Setting Libtorrent profile settings to MinimalMemory")
		lt.MinMemoryUsage(settings)
	} else if config.Get().LibtorrentProfile == profileHighSpeed {
		log.Info("Setting Libtorrent profile settings to HighSpeed")
		lt.HighPerformanceSeed(settings)
	}

	var err error
	s.PackSettings = settings
	s.SessionGlobal, err = lt.NewSession(s.PackSettings, int(lt.WrappedSessionHandleAddDefaultPlugins))
	if err != nil {
		log.Errorf("Could not create libtorrent session: %s", err)
		return
	}

	s.Session, err = s.SessionGlobal.GetHandle()
	if err != nil {
		log.Errorf("Could not create libtorrent session handle: %s", err)
		return
	}

	// s.Session.GetHandle().ApplySettings(s.PackSettings)

	if !s.config.LimitAfterBuffering {
		s.RestoreLimits()
	}

	s.applyCustomSettings()
}

func (s *Service) startServices() {
	if s.PackSettings == nil {
		return
	}

	if s.config.DisableTCP {
		log.Info("Disabling TCP...")
		s.PackSettings.SetBool("enable_outgoing_tcp", false)
		s.PackSettings.SetBool("enable_incoming_tcp", false)
	}
	if s.config.DisableUTP {
		log.Info("Disabling UTP...")
		s.PackSettings.SetBool("enable_outgoing_utp", false)
		s.PackSettings.SetBool("enable_incoming_utp", false)
	}

	if !s.config.DisableLSD {
		log.Info("Starting LSD...")
		s.PackSettings.SetBool("enable_lsd", true)
	}

	if !s.config.DisableDHT {
		log.Info("Starting DHT...")
		s.PackSettings.SetStr("dht_bootstrap_nodes", strings.Join(dhtBootstrapNodes, ","))
		s.PackSettings.SetBool("enable_dht", true)
	}

	if !s.config.DisableUPNP {
		log.Info("Starting UPNP...")
		s.PackSettings.SetBool("enable_upnp", true)
	}

	s.Session.ApplySettings(s.PackSettings)
}

func (s *Service) stopServices() {
	if s.InternalProxy != nil && !s.InternalProxy.IsErrored && s.InternalProxy.Server != nil {
		log.Infof("Stopping internal proxy")
		s.InternalProxy.Server.Shutdown(context.Background())
		s.InternalProxy = nil
	}

	// TODO: cleanup these messages after windows hang is fixed
	// Don't need to execute RPC calls when Kodi is closing
	if s.dialogProgressBG != nil {
		log.Infof("Closing existing Dialog")
		s.dialogProgressBG.Close()
	}
	s.dialogProgressBG = nil

	// Try to clean dialogs in background to avoid getting deadlock because of already closed Kodi
	if !s.isShutdown {
		go func() {
			xbmcHost, err := xbmc.GetLocalXBMCHost()
			if err != nil || xbmcHost == nil {
				return
			}

			log.Infof("Cleaning up all DialogBG")
			xbmcHost.DialogProgressBGCleanup()

			log.Infof("Resetting RPC")
			xbmcHost.ResetRPC()
		}()
	}

	if s.PackSettings != nil && s.Session != nil {
		if !s.config.DisableLSD {
			log.Info("Stopping LSD...")
			s.PackSettings.SetBool("enable_lsd", false)
		}

		if !s.config.DisableDHT {
			log.Info("Stopping DHT...")
			s.PackSettings.SetBool("enable_dht", false)
		}

		if !s.config.DisableUPNP {
			log.Info("Stopping UPNP...")
			s.PackSettings.SetBool("enable_upnp", false)
		}

		s.portsMu.Lock()
		s.deletePortMappings()
		s.portsMu.Unlock()

		s.Session.ApplySettings(s.PackSettings)
	}
}

// CheckAvailableSpace ...
func (s *Service) checkAvailableSpace(xbmcHost *xbmc.XBMCHost, t *Torrent) bool {
	// For memory storage we don't need to check available space
	if t.IsMemoryStorage() {
		return true
	}

	diskStatus, err := diskusage.DiskUsage(config.Get().DownloadPath)
	if err != nil {
		log.Warningf("Unable to retrieve the free space for %s, continuing anyway...", config.Get().DownloadPath)
		return false
	}

	torrentInfo := t.th.TorrentFile()

	if torrentInfo == nil || torrentInfo.Swigcptr() == 0 {
		log.Warning("Missing torrent info to check available space.")
		return false
	}

	status := t.GetLastStatus(false)
	if status == nil {
		return false
	}

	totalSize := t.ti.TotalSize()
	totalDone := status.GetTotalDone()
	sizeLeft := totalSize - totalDone
	availableSpace := diskStatus.Free
	path := status.GetSavePath()

	log.Infof("Checking for sufficient space on %s...", path)
	log.Infof("Total size of download: %s", humanize.Bytes(uint64(totalSize)))
	log.Infof("All time download: %s", humanize.Bytes(uint64(status.GetAllTimeDownload())))
	log.Infof("Size total done: %s", humanize.Bytes(uint64(totalDone)))
	log.Infof("Size left to download: %s", humanize.Bytes(uint64(sizeLeft)))
	log.Infof("Available space: %s", humanize.Bytes(uint64(availableSpace)))

	if availableSpace < sizeLeft {
		log.Errorf("Unsufficient free space on %s. Has %d, needs %d.", path, diskStatus.Free, sizeLeft)
		if xbmcHost != nil {
			xbmcHost.Notify("Elementum", "LOCALIZE[30207]", config.AddonIcon())
		}

		log.Infof("Pausing torrent %s", status.GetName())
		t.Pause()
		return false
	}

	return true
}

func (s *Service) updateInterfaces() {
	settings := s.PackSettings

	listenInterfaces, outgoingInterfaces, errInterface := s.getInterfaceSettings()
	if errInterface != nil {
		log.Errorf("Could not create configure libtorrent session due to wrong interfaces configuration: %s", errInterface)
		return
	}

	settings.SetStr("listen_interfaces", strings.Join(listenInterfaces, ","))
	log.Infof("Libtorrent listen_interfaces set to: %s", strings.Join(listenInterfaces, ","))

	if len(outgoingInterfaces) > 0 {
		settings.SetStr("outgoing_interfaces", strings.Join(outgoingInterfaces, ","))
		log.Infof("Libtorrent outgoing_interfaces set to: %s", strings.Join(outgoingInterfaces, ","))
	}

	s.Session.ApplySettings(settings)
}

// AddTorrent ...
func (s *Service) AddTorrent(xbmcHost *xbmc.XBMCHost, options AddOptions) (*Torrent, error) {
	defer perf.ScopeTimer()()

	// To make sure no spaces coming from Web UI
	options.URI = strings.TrimSpace(options.URI)

	log.Infof("Adding torrent with options: %#v", options)

	if options.DownloadStorage != config.StorageMemory && s.config.DownloadPath == "." {
		log.Warningf("Cannot add torrent since download path is not set")
		if xbmcHost != nil {
			xbmcHost.Notify("Elementum", "LOCALIZE[30113]", config.AddonIcon())
		}
		return nil, fmt.Errorf("Download path empty")
	}

	torrentParams := lt.NewAddTorrentParams()
	defer lt.DeleteAddTorrentParams(torrentParams)

	if options.DownloadStorage == config.StorageMemory {
		torrentParams.SetMemoryStorage(s.GetMemorySize())
	}

	torrentParams.SetMaxConnections(getPlatformSpecificConnectionLimit())

	var err error
	var th lt.TorrentHandle
	var infoHash string
	var originalTrackers []string
	var originalTrackersSize int
	var private bool

	// Dummy check if torrent file is a file containing a magnet link
	if _, err := os.Stat(options.URI); err == nil {
		dat, err := os.ReadFile(options.URI)
		if err == nil && bytes.HasPrefix(dat, []byte("magnet:")) {
			options.URI = string(dat)
		}
	}

	errorCode := lt.NewErrorCode()
	defer lt.DeleteErrorCode(errorCode)

	if strings.HasPrefix(options.URI, "magnet:") {
		// Remove all spaces in magnet
		options.URI = strings.Replace(options.URI, " ", "", -1)

		lt.ParseMagnetUri(options.URI, torrentParams, errorCode)
		if errorCode.Failed() {
			return nil, errors.New(errorCode.Message().(string))
		}

		originalTrackersSize = int(torrentParams.GetTrackers().Size())
		for i := 0; i < originalTrackersSize; i++ {
			url := torrentParams.GetTrackers().Get(i)
			originalTrackers = append(originalTrackers, url)
		}
		log.Debugf("Magnet has %d trackers", originalTrackersSize)

		shaHash := torrentParams.GetInfoHash().ToString()
		infoHash = hex.EncodeToString([]byte(shaHash))
	} else {
		if strings.HasPrefix(options.URI, "http") {
			torrent := NewTorrentFile(options.URI)

			if err = torrent.Resolve(); err != nil {
				log.Warningf("Could not resolve torrent %s: %s", options.URI, err)
				return nil, err
			}
			options.URI = torrent.URI
		}

		if _, err := os.Stat(options.URI); err != nil {
			log.Warningf("Cannot open torrent file at %s: %s", options.URI, err)
			return nil, err
		}

		log.Debugf("Adding torrent: %#v", options.URI)

		info := lt.NewTorrentInfo(options.URI, errorCode)
		if errorCode.Failed() {
			return nil, errors.New(errorCode.Message().(string))
		}

		private = info.Priv()
		defer lt.DeleteTorrentInfo(info)
		torrentParams.SetTorrentInfo(info)

		originalTrackersSize = int(torrentParams.GetTorrentInfo().Trackers().Size())
		if !private {
			for i := 0; i < originalTrackersSize; i++ {
				announceEntry := torrentParams.GetTorrentInfo().Trackers().Get(i)
				url := announceEntry.GetUrl()
				originalTrackers = append(originalTrackers, url)
			}
		}
		log.Debugf("Torrent file has %d trackers", originalTrackersSize)

		shaHash := info.InfoHash().ToString()
		infoHash = hex.EncodeToString([]byte(shaHash))
	}

	log.Infof("Setting save path to %s", s.config.DownloadPath)
	torrentParams.SetSavePath(s.config.DownloadPath)

	skipPriorities := false
	if options.DownloadStorage != config.StorageMemory {
		log.Infof("Checking for fast resume data in %s.fastresume", infoHash)
		fastResumeFile := filepath.Join(s.config.TorrentsPath, fmt.Sprintf("%s.fastresume", infoHash))
		if _, err := os.Stat(fastResumeFile); err == nil {
			log.Info("Found fast resume data")
			fastResumeData, err := os.ReadFile(fastResumeFile)
			if err != nil {
				return nil, err
			}

			fastResumeVector := lt.NewStdVectorChar()
			defer lt.DeleteStdVectorChar(fastResumeVector)
			for _, c := range fastResumeData {
				fastResumeVector.Add(c)
			}
			// currently we don't need to merge trackers from resume data
			// but we can with smth like torrentParams.SetFlags(uint64(lt.AddTorrentParamsFlagMergeResumeTrackers))
			torrentParams.SetResumeData(fastResumeVector)

			skipPriorities = true
		}
	}

	if !skipPriorities {
		// Setting default priorities to 0 to avoid downloading non-wanted files
		filesPriorities := lt.NewStdVectorInt()
		defer lt.DeleteStdVectorInt(filesPriorities)
		for i := 0; i <= 500; i++ {
			filesPriorities.Add(0)
		}
		torrentParams.SetFilePriorities(filesPriorities)
	}

	// Call torrent creation
	th, err = s.Session.AddTorrent(torrentParams, errorCode)
	if err != nil {
		return nil, err
	} else if errorCode.Failed() || !th.IsValid() {
		if th.Swigcptr() != 0 {
			defer lt.DeleteWrappedTorrentHandle(th)
		}
		return nil, errors.New(errorCode.Message().(string))
	}

	if !options.Paused {
		th.Resume()
	}

	// modify trackers
	log.Debugf("Loaded torrent has %d trackers", th.Trackers().Size()) // from *.fastresume
	if ((config.Get().ModifyTrackersStrategy == modifyTrackersFirstTime && options.FirstTime) || config.Get().ModifyTrackersStrategy == modifyTrackersEveryTime) && !private {
		if config.Get().RemoveOriginalTrackers {
			log.Debug("Remove original trackers from torrent")
			trackers := lt.NewStdVectorAnnounceEntry()
			defer lt.DeleteStdVectorAnnounceEntry(trackers)
			th.ReplaceTrackers(trackers)
			originalTrackersSize = 0
		} else {
			// replace previous state with original trackers
			trackers := lt.NewStdVectorAnnounceEntry()
			defer lt.DeleteStdVectorAnnounceEntry(trackers)

			for _, tracker := range originalTrackers {
				announceEntry := lt.NewAnnounceEntry(tracker)
				defer lt.DeleteAnnounceEntry(announceEntry)
				trackers.Add(announceEntry)
			}

			th.ReplaceTrackers(trackers)
		}

		if len(extraTrackers) > 0 && config.Get().AddExtraTrackers != addExtraTrackersNone {
			for _, tracker := range extraTrackers {
				if tracker == "" {
					continue
				}

				announceEntry := lt.NewAnnounceEntry(tracker)
				defer lt.DeleteAnnounceEntry(announceEntry)
				th.AddTracker(announceEntry)
			}

			newTrackersSize := int(th.Trackers().Size())
			log.Debugf("Added %d extra trackers", newTrackersSize-originalTrackersSize)
		}
		log.Debugf("After modifications loaded torrent has %d trackers", th.Trackers().Size())
	}

	log.Infof("Setting sequential download to: %v", options.DownloadStorage != config.StorageMemory)
	th.SetSequentialDownload(options.DownloadStorage != config.StorageMemory)

	log.Infof("Adding new torrent item with url: %s", options.URI)
	t := NewTorrent(s, th, th.TorrentFile(), options.URI, options.DownloadStorage)

	if options.DownloadStorage == config.StorageMemory {
		t.MemorySize = s.GetMemorySize()
	}

	t.addedTime = options.AddedTime
	s.q.Add(t)

	if !t.HasMetadata() {
		if err := t.WaitForMetadata(xbmcHost, infoHash); err != nil {
			log.Infof("Auto removing torrent %s after not getting metadata", infoHash)
			s.RemoveTorrent(xbmcHost, t, RemoveOptions{})
			return nil, err
		}
	}

	// Saving torrent file
	t.onMetadataReceived()
	t.init()

	go t.Watch()

	return t, nil
}

// RemoveTorrent ...
func (s *Service) RemoveTorrent(xbmcHost *xbmc.XBMCHost, t *Torrent, flags RemoveOptions) bool {
	log.Infof("Removing torrent: %s", t.Name())
	if t == nil {
		return false
	}

	t = s.q.FindByHash(t.InfoHash())
	if t == nil {
		return false
	}

	configKeepDownloading := config.Get().KeepDownloading
	configKeepFilesFinished := config.Get().KeepFilesFinished
	configKeepFilesPlaying := config.Get().KeepFilesPlaying

	if t.IsMemoryStorage() {
		configKeepDownloading = 2
		configKeepFilesPlaying = 2
		configKeepFilesFinished = 2
	}

	keepDownloading := false
	if flags.ForceConfirmation { // action came from menu
		// if user said no - we do not delete torrent file
		if xbmcHost != nil && !xbmcHost.DialogConfirmNonTimed("Elementum", fmt.Sprintf("LOCALIZE[30711];;%s", t.Name())) {
			keepDownloading = true
		}
	} else { // action came from playback
		if flags.ForceDrop || configKeepDownloading == 2 || len(t.ChosenFiles) == 0 {
			keepDownloading = false
		} else if configKeepDownloading == 0 || (xbmcHost == nil && configKeepDownloading == 1) || (xbmcHost != nil && xbmcHost.DialogConfirmFocused("Elementum", fmt.Sprintf("LOCALIZE[30146];;%s", t.Name()))) {
			keepDownloading = true
		}
	}

	keepSetting := configKeepFilesPlaying
	if flags.IsWatched {
		keepSetting = configKeepFilesFinished
	}

	deleteTorrentFiles := false
	deleteTorrentData := false

	if !keepDownloading {
		if flags.ForceConfirmation { // action came from menu
			// if user said yes - we delete torrent data
			if xbmcHost != nil && xbmcHost.DialogConfirmNonTimed("Elementum", fmt.Sprintf("LOCALIZE[30269];;%s", t.Name())) {
				deleteTorrentData = true
			}
		} else { // action came from playback
			if flags.ForceDelete || len(t.ChosenFiles) == 0 {
				deleteTorrentData = true
			} else if flags.ForceKeepTorrentData || keepSetting == 0 {
				deleteTorrentData = false
			} else if keepSetting == 2 || (xbmcHost != nil && xbmcHost.DialogConfirm("Elementum", fmt.Sprintf("LOCALIZE[30269];;%s", t.Name()))) {
				deleteTorrentData = true
			}
		}
	}

	if !keepDownloading || t.IsMemoryStorage() {
		deleteTorrentFiles = true
	}

	if !keepDownloading {
		defer func() {
			database.GetStorm().DeleteBTItem(t.InfoHash())
		}()

		s.q.Delete(t)

		t.Drop(deleteTorrentFiles, deleteTorrentData)
	}

	return true
}

func (s *Service) onStateChanged(stateAlert lt.StateChangedAlert) {
	switch stateAlert.GetState() {
	case lt.TorrentStatusDownloading:
		torrentHandle := stateAlert.GetHandle()
		torrentStatus := torrentHandle.Status(uint(lt.WrappedTorrentHandleQueryName))
		defer lt.DeleteTorrentStatus(torrentStatus)

		shaHash := torrentStatus.GetInfoHash().ToString()
		infoHash := hex.EncodeToString([]byte(shaHash))
		if spaceChecked, exists := s.SpaceChecked[infoHash]; exists {
			if !spaceChecked {
				if t := s.GetTorrentByHash(infoHash); t != nil {
					xbmcHost, _ := xbmc.GetLocalXBMCHost()
					s.checkAvailableSpace(xbmcHost, t)
					delete(s.SpaceChecked, infoHash)
				}
			}
		}
	}
}

// GetTorrentByHash ...
func (s *Service) GetTorrentByHash(hash string) *Torrent {
	return s.q.FindByHash(hash)
}

// GetTorrentByURI ...
func (s *Service) GetTorrentByURI(uri string) *Torrent {
	return s.q.FindByURI(uri)
}

func (s *Service) onSaveResumeDataWriter() {
	defer s.wg.Done()

	saveResumeWait := time.NewTicker(time.Duration(s.config.SessionSave) * time.Second)
	closing := s.Closer.C()
	defer saveResumeWait.Stop()

	for {
		select {
		case <-closing:
			log.Info("Closing resume data loop...")
			return
		case <-saveResumeWait.C:
			for _, t := range s.q.All() {
				if t == nil || t.th == nil || t.th.Swigcptr() == 0 || !t.th.IsValid() {
					continue
				}

				status := t.GetLastStatus(false)
				if status == nil || !status.GetHasMetadata() || !status.GetNeedSaveResume() {
					continue
				}

				t.th.SaveResumeData(1)
			}
		}
	}
}

func (s *Service) onSaveResumeDataConsumer() {
	defer s.wg.Done()

	alerts, alertsDone := s.Alerts()
	closing := s.Closer.C()
	defer close(alertsDone)

	for {
		select {
		case <-closing:
			log.Info("Closing resume data consumer ...")
			return
		case alert, ok := <-alerts:
			if !ok { // was the alerts channel closed?
				return
			}
			switch alert.Type {
			case lt.StateChangedAlertAlertType:
				stateAlert := lt.SwigcptrStateChangedAlert(alert.Pointer)
				s.onStateChanged(stateAlert)

			case lt.SaveResumeDataAlertAlertType:
				bEncoded := []byte(lt.Bencode(alert.Entry))
				b := bytes.NewReader(bEncoded)
				dec := bencode.NewDecoder(b)
				var torrentFile *TorrentFileRaw
				if err := dec.Decode(&torrentFile); err != nil {
					log.Warningf("Resume data corrupted for %s, %d bytes received and failed to decode with: %s, skipping...", alert.Name, len(bEncoded), err.Error())
				} else {
					path := filepath.Join(s.config.TorrentsPath, fmt.Sprintf("%s.fastresume", alert.InfoHash))
					os.WriteFile(path, bEncoded, 0644)
				}
				lt.DeleteEntry(alert.Entry)
			}
		}
	}
}

func (s *Service) onAlertsConsumer() {
	defer s.wg.Done()

	closing := s.Closer.C()
	defer s.alertsBroadcaster.Close()

	// ltOneSecond := lt.Seconds(ltAlertWaitTime)
	ltHalfSecond := lt.Milliseconds(500)
	log.Info("Consuming alerts...")
	for {
		select {
		case <-closing:
			log.Info("Closing alert consumer ...")

			return
		default:
			if s.Session == nil || s.Session.Swigcptr() == 0 || s.Session.WaitForAlert(ltHalfSecond).Swigcptr() == 0 {
				continue
			} else if s.Closer.IsSet() {
				return
			}

			var alerts lt.StdVectorAlerts
			alerts = s.Session.PopAlerts()
			defer lt.DeleteStdVectorAlerts(alerts)
			queueSize := alerts.Size()
			var name string
			var infoHash string
			var entry lt.Entry
			for i := 0; i < int(queueSize); i++ {
				ltAlert := alerts.Get(i)
				alertType := ltAlert.Type()
				alertPtr := ltAlert.Swigcptr()
				alertMessage := ltAlert.Message()

				if alertPtr == 0 {
					continue
				}

				switch alertType {
				case lt.SaveResumeDataAlertAlertType:
					saveResumeData := lt.SwigcptrSaveResumeDataAlert(alertPtr)
					torrentHandle := saveResumeData.GetHandle()
					torrentStatus := torrentHandle.Status(uint(lt.WrappedTorrentHandleQuerySavePath) | uint(lt.WrappedTorrentHandleQueryName))
					defer lt.DeleteTorrentStatus(torrentStatus)

					name = torrentStatus.GetName()
					shaHash := torrentStatus.GetInfoHash().ToString()
					infoHash = hex.EncodeToString([]byte(shaHash))
					entry = saveResumeData.ResumeData()
				case lt.ExternalIpAlertAlertType:
					splitMessage := strings.Split(alertMessage, ":")
					splitIP := strings.Split(splitMessage[len(splitMessage)-1], ".")
					alertMessage = strings.Join(splitMessage[:len(splitMessage)-1], ":") + splitIP[0] + ".XX.XX.XX"
				case lt.MetadataReceivedAlertAlertType:
					metadataAlert := lt.SwigcptrMetadataReceivedAlert(alertPtr)
					for _, t := range s.q.All() {
						if t.th != nil && metadataAlert.GetHandle().Equal(t.th) {
							t.gotMetainfo.Set()
						}
					}
				case lt.TrackerReplyAlertAlertType:
					ta := lt.SwigcptrTrackerReplyAlert(alertPtr)
					for _, t := range s.q.All() {
						if t.th != nil && ta.GetHandle().Equal(t.th) {
							t.trackers.Store(ta.TrackerUrl(), ta.GetNumPeers())
						}
					}
				case lt.DhtReplyAlertAlertType:
					ta := lt.SwigcptrDhtReplyAlert(alertPtr)
					for _, t := range s.q.All() {
						if t.th != nil && ta.GetHandle().Equal(t.th) {
							t.trackers.Store("DHT", ta.GetNumPeers())
						}
					}
				case lt.TorrentFinishedAlertAlertType:
					ta := lt.SwigcptrTorrentFinishedAlert(alertPtr)
					for _, t := range s.q.All() {
						if t.th != nil && ta.GetHandle().Equal(t.th) {
							go t.AlertFinished()
						}
					}
				}

				alert := &Alert{
					Type:     alertType,
					Category: ltAlert.Category(),
					What:     ltAlert.What(),
					Message:  alertMessage,
					Pointer:  alertPtr,
					Name:     name,
					Entry:    entry,
					InfoHash: infoHash,
				}
				s.alertsBroadcaster.Broadcast(alert)
			}
		}
	}
}

func (s *Service) networkRefresh() {
	defer s.wg.Done()

	netTicker := time.NewTicker(time.Duration(5) * time.Minute)
	portTicker := time.NewTicker(time.Duration(1) * time.Minute)
	closing := s.Closer.C()
	defer func() {
		netTicker.Stop()
		portTicker.Stop()
	}()

	for {
		select {
		case <-closing:
			log.Info("Closing port refresh loop...")
			return
		case <-netTicker.C:
			needUpdate := false
			if listenInterfaces, outgoingInterfaces, err := s.calcInterfaces(); err == nil {
				if slices.CompareFunc(s.listenInterfaces, listenInterfaces, func(a, b net.IP) int {
					return cmp.Compare(a.String(), b.String())
				}) != 0 {
					needUpdate = true
				}
				if slices.CompareFunc(s.outgoingInterfaces, outgoingInterfaces, func(a, b net.IP) int {
					return cmp.Compare(a.String(), b.String())
				}) != 0 {
					needUpdate = true
				}
			}

			if needUpdate {
				log.Infof("Updating listen interfaces due to network changes")
				go s.updateInterfaces()
			}
		case <-portTicker.C:
			needUpdate := false
			s.portsMu.Lock()
			for _, mapping := range s.mappedPorts {
				if mapping.Client == nil {
					continue
				}

				log.Infof("Updating port mapping: %d", mapping.Port)
				port := tryNatPort(mapping.Client, mapping.Port)
				if port == 0 || port != mapping.Port {
					needUpdate = true
				}
			}
			s.portsMu.Unlock()

			if needUpdate {
				log.Infof("Updating listen interfaces due to port changes in 5s")
				time.Sleep(5 * time.Second)
				go s.updateInterfaces()
			}
		}
	}
}

// Alerts ...
func (s *Service) Alerts() (<-chan *Alert, chan<- interface{}) {
	c, done := s.alertsBroadcaster.Listen()
	ac := make(chan *Alert)
	go func() {
		for v := range c {
			ac <- v.(*Alert)
		}
	}()
	return ac, done
}

func (s *Service) logAlerts() {
	pc := s.Closer.C()
	alerts, _ := s.Alerts()

	for {
		select {
		case <-pc:
			log.Debugf("Stopping service alerts")
			return

		case alert, ok := <-alerts:
			if !ok { // was the alerts channel closed?
				return
			}

			// Skipping Tracker communication, Save_Resume, UDP errors
			// No need to spam logs.
			if alert.Category&int(lt.AlertBlockProgressNotification) != 0 ||
				alert.Category&int(lt.AlertDhtLogNotification) != 0 ||
				alert.Category&int(lt.AlertTrackerNotification) != 0 ||
				alert.Category&int(lt.AlertPeerLogNotification) != 0 ||
				alert.Category&int(lt.AlertPickerLogNotification) != 0 ||
				alert.Category&int(lt.AlertTorrentLogNotification) != 0 ||
				alert.Type == int(lt.PieceFinishedAlertAlertType) ||
				alert.Type == int(lt.SaveResumeDataAlertAlertType) ||
				alert.Type == int(lt.UdpErrorAlertAlertType) ||
				alert.Type == int(lt.StateChangedAlertAlertType) ||
				alert.Type == int(lt.TorrentFinishedAlertAlertType) {
				continue
			} else if alert.Category&int(lt.AlertErrorNotification) != 0 {
				log.Errorf("%s: %s", alert.What, alert.Message)
			} else if alert.Category&int(lt.AlertDebugNotification) != 0 {
				log.Debugf("%s: %s", alert.What, alert.Message)
			} else if alert.Category&int(lt.AlertPerformanceWarning) != 0 {
				log.Warningf("%s: %s", alert.What, alert.Message)
			} else {
				log.Noticef("%s: %s", alert.What, alert.Message)
			}
		}
	}
}

func (s *Service) loadTorrentFiles() {
	// Cleaning the queue
	s.q.Clean()

	if !s.config.AutoloadTorrents {
		return
	}

	defer perf.ScopeTimer()()

	xbmcHost, _ := xbmc.GetLocalXBMCHost()

	log.Infof("Loading torrents from: %s", s.config.TorrentsPath)
	dir, err := os.Open(s.config.TorrentsPath)
	if err != nil {
		log.Infof("Cannot read torrents dir: %s", err)
		return
	}

	files, err := dir.Readdir(-1)
	if err != nil {
		log.Infof("Cannot read torrents dir: %s", err)
		return
	}

	sort.Slice(files, func(i, j int) bool {
		return files[i].ModTime().Unix() < files[j].ModTime().Unix()
	})

	for _, torrentFile := range files {
		if s.Closer.IsSet() || s.Session == nil || s.Session.Swigcptr() == 0 {
			return
		}
		if !strings.HasSuffix(torrentFile.Name(), ".torrent") {
			continue
		}
		if s.IsMemoryStorage() &&
			!util.FileExists(filepath.Join(s.config.TorrentsPath, fmt.Sprintf(".%s.file", util.FileWithoutExtension(torrentFile.Name())))) {
			continue
		}

		filePath := filepath.Join(s.config.TorrentsPath, torrentFile.Name())
		log.Infof("Loading torrent file %s", torrentFile.Name())

		t, err := s.AddTorrent(xbmcHost, AddOptions{URI: filePath, Paused: s.config.AutoloadTorrentsPaused, DownloadStorage: config.StorageFile, FirstTime: false, AddedTime: torrentFile.ModTime()})
		if err != nil {
			log.Warningf("Cannot add torrent from existing file %s: %s", filePath, err)
			continue
		} else if t == nil {
			continue
		}

		i := database.GetStorm().GetBTItem(t.InfoHash())
		if i == nil {
			continue
		}

		t.DBItem = i

		files := []*File{}
		for _, p := range i.Files {
			if f := t.GetFileByPath(p); f != nil {
				files = append(files, f)
			}
		}
		if len(files) > 0 {
			t.DownloadFiles(files)
		}
		t.SyncSelectedFiles()
	}

	s.cleanStaleFiles(s.config.DownloadPath, ".parts")
	s.cleanStaleFiles(s.config.TorrentsPath, ".fastresume")
}

func (s *Service) cleanStaleFiles(dir string, ext string) {
	log.Infof("Cleaning up stale %s files at %s ...", ext, dir)

	staleFiles, _ := filepath.Glob(filepath.Join(dir, "*"+ext))
	for _, staleFile := range staleFiles {
		infoHash := strings.Replace(strings.Replace(staleFile, dir, "", 1), ext, "", 1)[1:]
		if infoHash[0] == '.' {
			infoHash = strings.Replace(strings.Replace(staleFile, dir, "", 1), ext, "", 1)[2:]
		}

		if t := s.GetTorrentByHash(infoHash); t != nil {
			continue
		}

		if err := os.Remove(staleFile); err != nil {
			log.Errorf("Error removing file %s: %s", staleFile, err)
		} else {
			log.Infof("Removed stale file %s", staleFile)
		}
	}
}

func (s *Service) onDownloadProgress() {
	defer s.wg.Done()

	closing := s.Closer.C()
	rotateTicker := time.NewTicker(5 * time.Second)
	defer rotateTicker.Stop()

	pathChecked := make(map[string]bool)
	warnedMissing := make(map[string]bool)

	xbmcHost, _ := xbmc.GetLocalXBMCHost()

	showNext := 0
	for {
		select {
		case <-closing:
			log.Info("Closing download progress ...")
			return

		case <-rotateTicker.C:
			// TODO: there should be a check whether service is in Pause state
			// if !s.config.DisableBgProgress && s.dialogProgressBG != nil {
			// 	s.dialogProgressBG.Close()
			// 	s.dialogProgressBG = nil
			// 	continue
			// }

			if s.Closer.IsSet() || s.Session == nil || s.Session.Swigcptr() == 0 {
				return
			}

			var totalDownloadRate float64
			var totalUploadRate float64
			var totalProgress int

			activeTorrents := make([]*activeTorrent, 0)
			torrentsVector := s.Session.GetTorrents()
			torrentsVectorSize := int(torrentsVector.Size())
			defer lt.DeleteStdVectorTorrentHandle(torrentsVector)

			for i := 0; i < torrentsVectorSize; i++ {
				torrentHandle := torrentsVector.Get(i)
				if !torrentHandle.IsValid() {
					continue
				}

				ts := torrentHandle.Status()
				defer lt.DeleteTorrentStatus(ts)

				if !ts.GetHasMetadata() || s.Session.IsPaused() {
					continue
				}

				shaHash := ts.GetInfoHash().ToString()
				infoHash := hex.EncodeToString([]byte(shaHash))

				status := ""
				isPaused := ts.GetPaused()

				t := s.GetTorrentByHash(infoHash)
				if t != nil {
					statusCode := t.GetSmartState()
					status = StatusStrings[statusCode]
				} else {
					continue
				}

				downloadRate := float64(ts.GetDownloadPayloadRate())
				uploadRate := float64(ts.GetUploadPayloadRate())
				totalDownloadRate += downloadRate
				totalUploadRate += uploadRate

				torrentName := ts.GetName()
				progress := int(float64(ts.GetProgress()) * 100)

				if progress < 100 && !isPaused {
					activeTorrents = append(activeTorrents, &activeTorrent{
						torrentName:  torrentName,
						downloadRate: downloadRate,
						uploadRate:   uploadRate,
						progress:     progress,
					})
					totalProgress += progress
					continue
				}

				seedingTime := ts.GetSeedingTime()
				finishedTime := ts.GetFinishedTime()
				if progress == 100 && seedingTime == 0 {
					seedingTime = finishedTime
				}

				if !t.IsMemoryStorage() && s.config.SeedTimeLimit > 0 && !s.config.SeedForever {
					if seedingTime >= s.config.SeedTimeLimit {
						if !isPaused {
							log.Warningf("Seeding time limit reached, pausing %s", torrentName)
							torrentHandle.AutoManaged(false)
							torrentHandle.Pause(1)
							isPaused = true
						}
						status = StatusStrings[StatusSeeding]
					}
				}
				if !t.IsMemoryStorage() && s.config.SeedTimeRatioLimit > 0 && !s.config.SeedForever {
					timeRatio := 0
					downloadTime := ts.GetActiveTime() - seedingTime
					if downloadTime > 1 {
						timeRatio = seedingTime * 100 / downloadTime
					}
					if timeRatio >= s.config.SeedTimeRatioLimit {
						if !isPaused {
							log.Warningf("Seeding time ratio reached, pausing %s", torrentName)
							torrentHandle.AutoManaged(false)
							torrentHandle.Pause(1)
							isPaused = true
						}
						status = StatusStrings[StatusSeeding]
					}
				}
				if !t.IsMemoryStorage() && s.config.ShareRatioLimit > 0 && !s.config.SeedForever {
					ratio := int64(0)
					allTimeDownload := ts.GetAllTimeDownload()
					if allTimeDownload > 0 {
						ratio = ts.GetAllTimeUpload() * 100 / allTimeDownload
					}
					if ratio >= int64(s.config.ShareRatioLimit) {
						if !isPaused {
							log.Warningf("Share ratio reached, pausing %s", torrentName)
							torrentHandle.AutoManaged(false)
							torrentHandle.Pause(1)
						}
						status = StatusStrings[StatusSeeding]
					}
				}

				if t.IsMarkedToMove {
					t.IsMarkedToMove = false
					status = StatusStrings[StatusSeeding]
				}

				//
				// Handle moving completed downloads
				//
				if t.IsMemoryStorage() || !s.config.CompletedMove || status != StatusStrings[StatusSeeding] || s.anyPlayerIsPlaying() || t.IsMoveInProgress {
					continue
				}
				if xbmcHost != nil && xbmcHost.PlayerIsPlaying() {
					continue
				}

				if _, exists := warnedMissing[infoHash]; exists {
					continue
				}

				go func(t *Torrent) error {
					if t.IsMoveInProgress {
						return nil
					}

					defer func() {
						t.IsMoveInProgress = false
					}()
					t.IsMoveInProgress = true

					item := database.GetStorm().GetBTItem(infoHash)
					if item == nil {
						warnedMissing[infoHash] = true
						return fmt.Errorf("Torrent not found with infohash: %s", infoHash)
					}

					errMsg := fmt.Sprintf("Missing item type to move files to completed folder for %s", torrentName)
					if item.Type == "" {
						log.Error(errMsg)
						return errors.New(errMsg)
					}
					log.Warning(torrentName, "finished seeding, moving files...")

					// Check paths are valid and writable, and only once
					if _, exists := pathChecked[item.Type]; !exists {
						if item.Type == "movie" {
							if err := util.IsWritablePath(s.config.CompletedMoviesPath); err != nil {
								warnedMissing[infoHash] = true
								pathChecked[item.Type] = true
								log.Error(err)
								return err
							}
							pathChecked[item.Type] = true
						} else {
							if err := util.IsWritablePath(s.config.CompletedShowsPath); err != nil {
								warnedMissing[infoHash] = true
								pathChecked[item.Type] = true
								log.Error(err)
								return err
							}
							pathChecked[item.Type] = true
						}
					}

					// Preparing list of files that need to be moved
					torrentInfo := torrentHandle.TorrentFile()
					filesToMove := []string{}
					filesToCleanup := map[string]bool{}
					for _, fp := range t.files {
						f := t.GetFileByPath(fp.Path)
						filesToMove = append(filesToMove, torrentInfo.Files().FilePath(f.Index))
					}

					log.Infof("Moving torrent '%s' to completed folder", t.Name())
					log.Info("Removing the torrent without deleting files after Completed move ...")

					// Removing torrent from libtorrent session before moving files physically
					s.RemoveTorrent(xbmcHost, t, RemoveOptions{ForceDrop: true, ForceKeepTorrentData: true})

					if len(t.files) <= 0 {
						return errors.New("No files listed in the torrent")
					}

					// Move files one by one from torrent
					for _, filePath := range filesToMove {
						fileName := filepath.Base(filePath)

						extracted := ""
						re := regexp.MustCompile(`(?i).*\.rar$`)
						if re.MatchString(fileName) {
							extractedPath := filepath.Join(s.config.DownloadPath, filepath.Dir(filePath), "extracted")
							files, err := os.ReadDir(extractedPath)
							if err != nil {
								return err
							}
							if len(files) == 1 {
								extracted = files[0].Name()
							} else {
								for _, file := range files {
									fileNameCurrent := file.Name()
									re := regexp.MustCompile(`(?i).*\.(mkv|mp4|mov|avi)`)
									if re.MatchString(fileNameCurrent) {
										extracted = fileNameCurrent
										break
									}
								}
							}
							if extracted != "" {
								filePath = filepath.Join(filepath.Dir(filePath), "extracted", extracted)
							} else {
								return errors.New("No extracted file to move")
							}
						}

						var dstPath string
						if item.Type == "movie" {
							dstPath = util.EffectiveDir(s.config.CompletedMoviesPath)
							if item.ID > 0 {
								movie := tmdb.GetMovie(item.ID, config.Get().Language)
								if movie != nil {
									dstPath = filepath.Join(dstPath, library.GetMoviePathTitle(movie))
									os.MkdirAll(dstPath, 0755)
								}
							}
						} else {
							dstPath = util.EffectiveDir(s.config.CompletedShowsPath)
							if item.ShowID > 0 {
								if show := tmdb.GetShow(item.ShowID, config.Get().Language); show != nil {
									showPath := library.GetShowPathTitle(show)
									seasonPath := filepath.Join(showPath, fmt.Sprintf("Season %d", item.Season))
									if item.Season == 0 {
										seasonPath = filepath.Join(showPath, "Specials")
									}
									dstPath = filepath.Join(dstPath, seasonPath)
									os.MkdirAll(dstPath, 0755)
								}
							}
						}

						srcPath := filepath.Join(s.config.DownloadPath, filePath)
						log.Infof("Moving file %s to %s", srcPath, dstPath)
						if dst, err := util.Move(srcPath, dstPath); err != nil {
							log.Error(err)
						} else {
							log.Warning(fileName, "moved to", dst)

							if dirPath := filepath.Dir(filePath); dirPath != "." {
								filesToCleanup[filepath.Dir(srcPath)] = true
								if extracted != "" {
									parentPath := filepath.Clean(filepath.Join(filepath.Dir(srcPath), ".."))
									if parentPath != "." && parentPath != s.config.DownloadPath {
										filesToCleanup[parentPath] = true
									}
								}
							}
						}
					}

					// Remove leftover folders
					for filePath := range filesToCleanup {
						log.Infof("Removing all from %s", filePath)
						os.RemoveAll(filePath)
					}

					log.Infof("Marking %s for removal from library and database...", torrentName)
					database.GetStorm().UpdateBTItemStatus(infoHash, Remove)

					return nil
				}(t)
			}

			totalActive := len(activeTorrents)
			if totalActive > 0 {
				showProgress := totalProgress / totalActive
				showTorrent := fmt.Sprintf("Total - D/L: %s - U/L: %s", humanize.Bytes(uint64(totalDownloadRate))+"/s", humanize.Bytes(uint64(totalUploadRate))+"/s")
				if showNext >= totalActive {
					showNext = 0
				} else {
					showProgress = activeTorrents[showNext].progress
					torrentName := activeTorrents[showNext].torrentName
					if len(torrentName) > 30 {
						torrentName = torrentName[:30] + "..."
					}
					showTorrent = fmt.Sprintf("%s - %s - %s", torrentName, humanize.Bytes(uint64(activeTorrents[showNext].downloadRate))+"/s", humanize.Bytes(uint64(activeTorrents[showNext].uploadRate))+"/s")
					showNext++
				}
				if !s.config.DisableBgProgress && (!s.config.DisableBgProgressPlayback || !s.anyPlayerIsPlaying()) {
					if s.dialogProgressBG == nil {
						s.dialogProgressBG = xbmcHost.NewDialogProgressBG("Elementum", "")
					}
					if s.dialogProgressBG != nil {
						s.dialogProgressBG.Update(showProgress, "Elementum", showTorrent)
					}
				}
			} else if (!s.config.DisableBgProgress || (s.config.DisableBgProgressPlayback && s.anyPlayerIsPlaying())) && s.dialogProgressBG != nil {
				s.dialogProgressBG.Close()
				s.dialogProgressBG = nil
			}
		}
	}
}

// SetDownloadLimit ...
func (s *Service) SetDownloadLimit(i int) {
	settings := s.PackSettings
	settings.SetInt("download_rate_limit", i)

	s.Session.ApplySettings(settings)
}

// SetUploadLimit ...
func (s *Service) SetUploadLimit(i int) {
	settings := s.PackSettings

	settings.SetInt("upload_rate_limit", i)
	s.Session.ApplySettings(settings)
}

// RestoreLimits ...
func (s *Service) RestoreLimits() {
	if s.config.DownloadRateLimit > 0 {
		s.SetDownloadLimit(s.config.DownloadRateLimit)
		log.Infof("Rate limiting download to %s", humanize.Bytes(uint64(s.config.DownloadRateLimit)))
	} else {
		s.SetDownloadLimit(0)
	}

	// if s.config.DisableUpload {
	// 	s.SetUploadLimit(1)
	// 	log.Infof("Rate limiting upload to %d byte, due to disabled upload", 1)
	// } else if s.config.UploadRateLimit > 0 {
	if s.config.UploadRateLimit > 0 {
		s.SetUploadLimit(s.config.UploadRateLimit)
		log.Infof("Rate limiting upload to %s", humanize.Bytes(uint64(s.config.UploadRateLimit)))
	} else {
		s.SetUploadLimit(0)
	}
}

// SetBufferingLimits ...
func (s *Service) SetBufferingLimits() {
	if s.config.LimitAfterBuffering {
		s.SetDownloadLimit(0)
		log.Info("Resetting rate limited download for buffering")
	}
}

// GetSeedTime ...
func (s *Service) GetSeedTime() int64 {
	if s.config.DisableUpload {
		return 0
	}

	return int64(s.config.SeedTimeLimit)
}

// GetBufferSize ...
func (s *Service) GetBufferSize() int64 {
	if s.config.BufferSize < s.config.EndBufferSize {
		return int64(s.config.EndBufferSize)
	}
	return int64(s.config.BufferSize)
}

// GetMemorySize ...
func (s *Service) GetMemorySize() int64 {
	return int64(config.Get().MemorySize)
}

// GetStorageType ...
func (s *Service) GetStorageType() int {
	return s.config.DownloadStorage
}

// PlayerStop ...
func (s *Service) PlayerStop() {
	log.Debugf("PlayerStop")
}

// PlayerSeek ...
func (s *Service) PlayerSeek() {
	log.Debugf("PlayerSeek")
}

// ClientInfo ...
func (s *Service) ClientInfo(ctx *gin.Context) {
	torrentID := ctx.Query("torrentid")

	showTrackers := ctx.DefaultQuery("trackers", "true") == "true"
	showPieces := ctx.DefaultQuery("pieces", "true") == "true"

	w := bufio.NewWriter(ctx.Writer)
	defer w.Flush()

	xbmcHost, _ := xbmc.GetXBMCHostWithContext(ctx)
	if xbmcHost == nil {
		return
	}

	for _, t := range s.q.All() {
		if t == nil || t.th == nil || (torrentID != "" && t.infoHash != torrentID) {
			continue
		}

		t.TorrentInfo(xbmcHost, w, showTrackers, showPieces)

		fmt.Fprint(w, "\n\n")
	}
}

// AttachPlayer adds Player instance to service
func (s *Service) AttachPlayer(p *Player) {
	if p == nil || p.t == nil {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.Players[p.t.InfoHash()]; ok {
		return
	}

	s.Players[p.t.InfoHash()] = p
}

// DetachPlayer removes Player instance
func (s *Service) DetachPlayer(p *Player) {
	if p == nil || p.t == nil {
		return
	}

	p.t.PlayerAttached--

	s.mu.Lock()
	defer s.mu.Unlock()

	if p == nil || p.t == nil {
		return
	}

	delete(s.Players, p.t.InfoHash())
}

// GetPlayer searches for player with desired TMDB id
func (s *Service) GetPlayer(kodiID int, tmdbID int) *Player {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, p := range s.Players {
		if p == nil || p.t == nil {
			continue
		}

		if (tmdbID != 0 && p.p.TMDBId == tmdbID) || (kodiID != 0 && p.p.KodiID == kodiID) {
			return p
		}
	}

	return nil
}

func (s *Service) anyPlayerIsPlaying() bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, p := range s.Players {
		if p == nil || p.t == nil {
			continue
		}

		if p.p.Playing {
			return true
		}
	}

	return false
}

// GetActivePlayer searches for player that is Playing anything
func (s *Service) GetActivePlayer() *Player {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, p := range s.Players {
		if p == nil || p.t == nil {
			continue
		}

		if p.p.Playing {
			return p
		}
	}

	return nil
}

// HasTorrentByID checks whether there is active torrent for queried tmdb id
func (s *Service) HasTorrentByID(tmdbID int) *Torrent {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, t := range s.q.All() {
		if t == nil || t.DBItem == nil {
			continue
		}

		if t.DBItem.ID == tmdbID {
			return t
		}
	}

	return nil
}

// HasTorrentByQuery checks whether there is active torrent with searches query
func (s *Service) HasTorrentByQuery(query string) *Torrent {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, t := range s.q.All() {
		if t == nil || t.DBItem == nil {
			continue
		}

		if t.DBItem.Query == query {
			return t
		}
	}

	return nil
}

// HasTorrentBySeason checks whether there is active torrent for queried season
func (s *Service) HasTorrentBySeason(tmdbID int, season int) *Torrent {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, t := range s.q.All() {
		if t == nil || t.DBItem == nil {
			continue
		}

		if t.DBItem.ShowID == tmdbID && t.DBItem.Season == season {
			return t
		}
	}

	return nil
}

// HasTorrentByEpisode checks whether there is active torrent for queried episode
func (s *Service) HasTorrentByEpisode(tmdbID int, season, episode int) *Torrent {
	s.mu.Lock()
	defer s.mu.Unlock()

	re := regexp.MustCompile(fmt.Sprintf(episodeMatchRegex, season, episode))

	for _, t := range s.q.All() {
		if t == nil || t.DBItem == nil {
			continue
		}

		if t.DBItem.ShowID == tmdbID && t.DBItem.Season == season && t.DBItem.Episode == episode {
			// This is a strict match
			return t
		} else if t.DBItem.ShowID == tmdbID {
			// Try to find an episode
			for _, choice := range t.files {
				if re.MatchString(choice.Name) {
					return t
				}
			}
		}
	}

	return nil
}

// HasTorrentByName checks whether there is active torrent for queried name
func (s *Service) HasTorrentByName(query string) *Torrent {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, t := range s.q.All() {
		if t == nil {
			continue
		}

		if strings.Contains(t.Name(), query) {
			return t
		}
	}

	return nil
}

// HasTorrentByFakeID checks whether there is active torrent with fake id
func (s *Service) HasTorrentByFakeID(query string) *Torrent {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, t := range s.q.All() {
		if t == nil || t.DBItem == nil {
			continue
		}

		id := strconv.Itoa(int(xxhash.Sum64String(t.DBItem.Query)))
		if id == query {
			return t
		}
	}

	return nil
}

// GetTorrents return all active torrents
func (s *Service) GetTorrents() []*Torrent {
	return s.q.All()
}

// GetListenIP returns calculated IP for TCP/TCP6
func (s *Service) GetListenIP(network string) string {
	if strings.Contains(network, "6") {
		return s.ListenIPv6
	}
	return s.ListenIP
}

// GetMemoryStats returns total and free memory sizes for this OS
func (s *Service) GetMemoryStats() (int64, int64) {
	if v, err := mem.VirtualMemory(); v != nil && err == nil {
		return int64(v.Total), int64(v.Free)
	}

	return 0, 0
}

// IsMemoryStorage is a shortcut for checking whether we run memory storage
func (s *Service) IsMemoryStorage() bool {
	return s.config.DownloadStorage == config.StorageMemory
}

// watchConfig watches for libtorrent.config changes to reapply libtorrent settings
func (s *Service) watchConfig() {
	w := watcher.New()

	go func() {
		closing := s.Closer.C()

		for {
			select {
			case event := <-w.Event:
				log.Infof("Watcher notify: %v", event)
				s.configure()
				s.applyCustomSettings()
			case err := <-w.Error:
				log.Errorf("Watcher error: %s", err)
			case <-w.Closed:
				return
			case <-closing:
				w.Close()
				return
			}
		}
	}()

	filePath := filepath.Join(config.Get().ProfilePath, "libtorrent.config")
	if err := w.Add(filePath); err != nil {
		log.Errorf("Watcher error. Could not add file to watch: %s", err)
	}

	if err := w.Start(time.Millisecond * 500); err != nil {
		log.Errorf("Error watching files: %s", err)
	}
}

func (s *Service) applyCustomSettings() {
	if !s.config.UseLibtorrentConfig {
		return
	}

	settings := s.PackSettings

	for k, v := range s.readCustomSettings() {
		if v == "true" {
			settings.SetBool(k, true)
			log.Infof("Applying bool setting: %s=true", k)
			continue
		} else if v == "false" {
			settings.SetBool(k, false)
			log.Infof("Applying bool setting: %s=false", k)
			continue
		} else if in, err := strconv.Atoi(v); err == nil {
			settings.SetInt(k, in)
			log.Infof("Applying int setting: %s=%d", k, in)
			continue
		}

		log.Errorf("Cannot parse config settings for: %s=%s", k, v)
	}

	s.Session.ApplySettings(settings)
}

func (s *Service) readCustomSettings() map[string]string {
	ret := map[string]string{}

	filePath := filepath.Join(config.Get().ProfilePath, "libtorrent.config")
	f, err := os.Open(filePath)
	if err != nil {
		return ret
	}
	defer f.Close()

	reReplace := regexp.MustCompile(`[^_\d\w=]`)
	reFind := regexp.MustCompile(`([_\d\w=]+)=(\w+)`)
	scan := bufio.NewScanner(f)
	for scan.Scan() {
		l := scan.Text()

		l = strings.Replace(l, " ", "", -1)
		if strings.HasPrefix(l, "#") {
			continue
		}

		l = reReplace.ReplaceAllString(l, "")
		res := reFind.FindStringSubmatch(l)
		if len(res) < 3 {
			continue
		}

		ret[res[1]] = res[2]
	}

	return ret
}

// StopNextFiles stops all torrents that wait for "next" playback
func (s *Service) StopNextFiles() {
	xbmcHost, _ := xbmc.GetLocalXBMCHost()

	for _, t := range s.q.All() {
		if t.IsNextFile && t.PlayerAttached <= 0 {
			log.Infof("Stopping torrent '%s' as a not-needed next episode", t.Name())

			t.stopNextTimer()
			s.RemoveTorrent(xbmcHost, t, RemoveOptions{})
		}
	}

}

// IsWatchedFile ...
func IsWatchedFile(path string, size int64) bool {
	key := fmt.Sprintf("stored.watched_file.%s.%d", path, size)

	res, _ := database.GetCache().GetCachedBool(database.CommonBucket, key)
	return res
}

// SetWatchedFile ...
func SetWatchedFile(path string, size int64, watched bool) {
	key := fmt.Sprintf("stored.watched_file.%s.%d", path, size)

	database.GetCache().SetCachedBool(database.CommonBucket, storedWatchedFileExpiration, key, watched)
}

// getInterfaceSettings is parsing configuration settings and forming list of IPs with ports for libtorrent
func (s *Service) getInterfaceSettings() ([]string, []string, error) {
	s.portsMu.Lock()
	defer s.portsMu.Unlock()

	// Clear existing mappings
	s.deletePortMappings()

	// Collect interface strings
	listenInterfaces, outgoingInterfaces, err := s.calcInterfaces()
	if err != nil {
		return nil, nil, err
	}

	s.listenInterfaces = listenInterfaces
	s.outgoingInterfaces = outgoingInterfaces

	// Search for available port to use
	listenInterfacesStrings := s.calcInterfacePorts(listenInterfaces)
	outgoingInterfacesStrings := []string{}
	for _, i := range outgoingInterfaces {
		outgoingInterfacesStrings = append(outgoingInterfacesStrings, i.String())
	}

	return listenInterfacesStrings, outgoingInterfacesStrings, nil
}

// calcInterfaces is parsing configuration values and returns list of IPs
func (s *Service) calcInterfaces() ([]net.IP, []net.IP, error) {
	listenInterfacesInput := "0.0.0.0"
	if !s.config.ListenAutoDetectIP && strings.TrimSpace(s.config.ListenInterfaces) != "" {
		listenInterfacesInput = s.config.ListenInterfaces
	}

	listenInterfaces, err := parseInterfaces(listenInterfacesInput)
	log.Debugf("Parsed listen interfaces %s: %s", listenInterfacesInput, listenInterfaces)
	if err != nil {
		return nil, nil, err
	}

	outgoingInterfaces, err := parseInterfaces(s.config.OutgoingInterfaces)
	log.Debugf("Parsed outgoing interfaces %s: %s", s.config.OutgoingInterfaces, outgoingInterfaces)
	if err != nil {
		return nil, nil, err
	}

	return listenInterfaces, outgoingInterfaces, nil
}

func (s *Service) getNatPort(local net.IP, port int) (int, *natpmp.Client) {
	gateways := ip.GetPossibleGateways(local)
	for _, gw := range gateways {
		nat := natpmp.NewClientWithTimeout(gw, 1500*time.Millisecond)
		log.Debugf("Testing NAT ports for gateway %s", gw)

		tryPort := tryNatPort(nat, port)
		if tryPort > 0 {
			return tryPort, nat
		}

		tryPort = tryNatPort(nat, 0)
		if tryPort > 0 {
			return tryPort, nat
		}
	}

	return 0, nil
}

func (s *Service) deletePortMappings() {
	for _, mapping := range s.mappedPorts {
		if mapping.Client == nil {
			continue
		}

		log.Infof("Deleting port mapping: %d", mapping.Port)
		deleteNatPort(mapping.Client, mapping.Port)
	}
	s.mappedPorts = map[string]PortMapping{}
}

func deleteNatPort(nat *natpmp.Client, port int) {
	_, err := nat.AddPortMapping("tcp", port, port, 0)
	if err != nil {
		log.Errorf("failed to request TCP mapping: %v", err)
	}

	_, err = nat.AddPortMapping("udp", port, port, 0)
	if err != nil {
		log.Errorf("failed to request UDP mapping: %v", err)
	}
}

func tryNatPort(nat *natpmp.Client, port int) int {
	tcp, err := nat.AddPortMapping("tcp", port, port, int64(time.Duration(60*time.Second)))
	if err != nil {
		log.Errorf("failed to request TCP mapping: %v", err)
		return 0
	}
	log.Debugf("Got TCP port (for port %d) %v -> %v", port, tcp.MappedExternalPort, tcp.InternalPort)

	udp, err := nat.AddPortMapping("udp", port, port, int64(time.Duration(60*time.Second)))
	if err != nil {
		log.Errorf("failed to request UDP mapping: %v", err)
		return 0
	}
	log.Debugf("Got UDP port (for port %d) %v -> %v", port, udp.MappedExternalPort, udp.InternalPort)

	if tcp.InternalPort != tcp.MappedExternalPort {
		log.Debugf("TCP internal (%v) and external (%v) ports do not match", tcp.InternalPort, tcp.MappedExternalPort)
		return 0
	}
	if udp.InternalPort != udp.MappedExternalPort {
		log.Debugf("UDP internal (%v) and external (%v) port do not match", udp.InternalPort, udp.MappedExternalPort)
		return 0
	}

	if tcp.InternalPort != udp.InternalPort {
		log.Debugf("WARN: TCP (%v) and UDP (%v) ports do not match, using TCP", tcp.InternalPort, udp.InternalPort)
	}

	return int(tcp.MappedExternalPort)
}

// calcInterfacePorts is searching for a proper port to use for each interface and returns an IP:port list
func (s *Service) calcInterfacePorts(addrs []net.IP) []string {
	// Set port range for automatic port detect
	if s.config.ListenAutoDetectPort {
		s.config.ListenPortMin = 6891
		s.config.ListenPortMax = 6899
	}

	var listenPorts []int
	for p := s.config.ListenPortMin; p <= s.config.ListenPortMax; p++ {
		listenPorts = append(listenPorts, p)
	}

	rand.Seed(time.Now().UTC().UnixNano())

	ret := []string{}
	for _, addr := range addrs {
		// Get random port from a range
		port := listenPorts[rand.Intn(len(listenPorts))]
		mapping := PortMapping{}

		// Use NAT-PMP to get available port to use from gateway
		if s.config.ListenAutoDetectPort && !s.config.DisableNATPMP {
			queryAddrs := []net.IP{addr}
			// Try all local IPs if we don't have a specific interface to be used
			if addr.String() == "0.0.0.0" {
				if ips, err := ip.VPNIPs(); err == nil && len(ips) > 0 {
					queryAddrs = ips
				}
			}

			log.Debugf("Testing NAT-PMP ports for %s", queryAddrs)

			// Try to get NAT port for each local IP
			for _, queryAddr := range queryAddrs {
				if natPort, natClient := s.getNatPort(queryAddr, port); natPort > 0 {
					port = natPort
					mapping.Client = natClient
					break
				}
			}
		}

		// Store port mapping
		mapping.Port = port
		s.mappedPorts[addr.String()] = mapping

		// Construct libtorrent-compatible settings
		if addr.To4() != nil {
			ret = append(ret, fmt.Sprintf("%s:%d", addr.To4().String(), port))
		} else {
			ret = append(ret, fmt.Sprintf("[%s]:%d", addr.To16().String(), port))
		}
	}

	return ret
}

func parseInterfaces(input string) ([]net.IP, error) {
	interfaces := strings.Split(strings.Replace(strings.TrimSpace(input), " ", "", -1), ",")
	ret := []net.IP{}

	// Prepare listen_interfaces
	for _, ifName := range interfaces {
		if ifName == "" {
			continue
		}

		// Get IPs for interface
		v4, v6, err := ip.GetInterfaceAddrs(ifName)
		if err != nil {
			return nil, err
		}

		if v4 != nil {
			ret = append(ret, v4)
		}
		if v6 != nil {
			ret = append(ret, v6)
		}
	}

	return ret, nil
}

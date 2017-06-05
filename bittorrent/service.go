package bittorrent

import (
	"os"
	// "io"
	"fmt"
	"time"
	// "bytes"
	"errors"
	"regexp"
	"strings"
	"strconv"
	"io/ioutil"
	"math/rand"
	"path/filepath"

	"github.com/op/go-logging"
	"github.com/dustin/go-humanize"
	"golang.org/x/time/rate"
	gotorrent "github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/storage"
	"github.com/anacrolix/torrent/iplist"

	fat32storage "github.com/iamacarpet/go-torrent-storage-fat32"

	// "github.com/scakemyer/quasar/broadcast"
	"github.com/scakemyer/quasar/database"
	"github.com/scakemyer/quasar/diskusage"
	"github.com/scakemyer/quasar/config"
	"github.com/scakemyer/quasar/tmdb"
	"github.com/scakemyer/quasar/util"
	"github.com/scakemyer/quasar/xbmc"
)

const (
	Delete = iota
	Update
	RemoveFromLibrary
)

const (
	Remove = iota
	Active
)

// const (
// 	ipToSDefault     = iota
// 	ipToSLowDelay    = 1 << iota
// 	ipToSReliability = 1 << iota
// 	ipToSThroughput  = 1 << iota
// 	ipToSLowCost     = 1 << iota
// )

var dhtBootstrapNodes = []string{
	"router.bittorrent.com",
	"router.utorrent.com",
	"dht.transmissionbt.com",
	"dht.aelitis.com", // Vuze
}

var DefaultTrackers = []string{
	"udp://tracker.opentrackr.org:1337/announce",
	"udp://tracker.coppersurfer.tk:6969/announce",
	"udp://tracker.leechers-paradise.org:6969/announce",
	"udp://tracker.openbittorrent.com:80/announce",
	"udp://public.popcorn-tracker.org:6969/announce",
	"udp://explodie.org:6969",
}

var (
	db *database.Database
	Bucket = database.BitTorrentBucket
)

const (
	_ = iota
	StorageFile
	StorageMemory
)

// const (
// 	ProxyTypeNone = iota
// 	ProxyTypeSocks4
// 	ProxyTypeSocks5
// 	ProxyTypeSocks5Password
// 	ProxyTypeSocksHTTP
// 	ProxyTypeSocksHTTPPassword
// 	ProxyTypeI2PSAM
// )

// type ProxySettings struct {
// 	Type     int
// 	Port     int
// 	Hostname string
// 	Username string
// 	Password string
// }

type BTConfiguration struct {
	// SpoofUserAgent      int
	DownloadStorage     int
	MemorySize          int64
	BufferSize          int64
	MaxUploadRate       int
	MaxDownloadRate     int
	LimitAfterBuffering bool
	ConnectionsLimit    int
	// SessionSave         int
	// ShareRatioLimit     int
	// SeedTimeRatioLimit  int
	SeedTimeLimit       int
	DisableDHT          bool
	// DisableUPNP         bool
	EncryptionPolicy    int
	LowerListenPort     int
	UpperListenPort     int
	ListenInterfaces    string
	OutgoingInterfaces  string
	// TunedStorage        bool
	DownloadPath        string
	TorrentsPath        string
	DisableBgProgress   bool
	CompletedMove       bool
	CompletedMoviesPath string
	CompletedShowsPath  string
	// Proxy               *ProxySettings
}

type BTService struct {
	Client            *gotorrent.Client
	ClientConfig      *gotorrent.Config
	PieceCompletion   storage.PieceCompletion
	DownloadLimiter   *rate.Limiter
	UploadLimiter     *rate.Limiter
	Torrents 					[]*Torrent

	config            *BTConfiguration
	log               *logging.Logger
	gotorrentLog      *logging.Logger
	// alertsBroadcaster *broadcast.Broadcaster
	dialogProgressBG  *xbmc.DialogProgressBG
	// packSettings      libtorrent.SettingsPack
	SpaceChecked      map[string]bool
	MarkedToMove      int
	UserAgent         string
	closing           chan interface{}
}

type DBItem struct {
	ID      int    `json:"id"`
	State   int    `json:"state"`
	Type    string `json:"type"`
	File    int    `json:"file"`
	ShowID  int    `json:"showid"`
	Season  int    `json:"season"`
	Episode int    `json:"episode"`
}

type PlayingItem struct {
	DBID        int
	DBTYPE      string

	TMDBID      int
	Season 		  int
	Episode     int

	WatchedTime float64
	Duration    float64
}

type ResumeFile struct {
	InfoHash string     `bencode:"info-hash"`
	Trackers [][]string `bencode:"trackers"`
}

type activeTorrent struct {
	torrentName  string
	downloadRate float64
	uploadRate   float64
	progress     int
}

func InitDB() {
	db, _ = database.NewDB()
}

func NewBTService(conf BTConfiguration) *BTService {
	s := &BTService{
		log:               logging.MustGetLogger("btservice"),
		gotorrentLog:      logging.MustGetLogger("gotorrent"),
		SpaceChecked:      make(map[string]bool, 0),
		MarkedToMove:      -1,
		config:            &conf,
		closing:           make(chan interface{}),
		Torrents:  				 []*Torrent{},

		DownloadLimiter:   rate.NewLimiter(rate.Inf, 1<<20),
		UploadLimiter:     rate.NewLimiter(rate.Inf, 256<<10),
	}

	if _, err := os.Stat(s.config.TorrentsPath); os.IsNotExist(err) {
		if err := os.Mkdir(s.config.TorrentsPath, 0755); err != nil {
			s.log.Error("Unable to create Torrents folder")
		}
	}

	if completion, err := storage.NewBoltPieceCompletion(config.Get().ProfilePath); err == nil {
		s.PieceCompletion = completion
	} else {
		s.PieceCompletion = storage.NewMapPieceCompletion()
	}

	s.configure()
	s.startServices()

	tmdb.CheckApiKey()

	// go s.loadTorrentFiles()
	go s.downloadProgress()

	return s
}

func (s *BTService) Close() {
	s.log.Info("Stopping BT Services...")
	s.stopServices()
	close(s.closing)
	s.Client.Close()
}

func (s *BTService) Reconfigure(config BTConfiguration) {
	s.stopServices()
	s.config = &config
	s.configure()
	s.startServices()
	// s.loadTorrentFiles()
}

func (s *BTService) configure() {
	// settings := libtorrent.NewSettingsPack()
	// s.Session = libtorrent.NewSession(settings, int(libtorrent.SessionHandleAddDefaultPlugins))

	// s.log.Info("Applying session settings...")
	s.log.Info("Configuring client...")

	var listenPorts []string
	for p := s.config.LowerListenPort; p <= s.config.UpperListenPort; p++ {
		listenPorts = append(listenPorts, strconv.Itoa(p))
	}
	rand.Seed(time.Now().UTC().UnixNano())

	listenInterfaces := []string{"0.0.0.0"}
	if strings.TrimSpace(s.config.ListenInterfaces) != "" {
		listenInterfaces = strings.Split(strings.Replace(strings.TrimSpace(s.config.ListenInterfaces), " ", "", -1), ",")
	}

	listenInterfacesStrings := make([]string, 0)
	for _, listenInterface := range listenInterfaces {
		listenInterfacesStrings = append(listenInterfacesStrings, listenInterface + ":" + listenPorts[rand.Intn(len(listenPorts))])
		if len(listenPorts) > 1 {
			listenInterfacesStrings = append(listenInterfacesStrings, listenInterface + ":" + listenPorts[rand.Intn(len(listenPorts))])
		}
	}

	// userAgent := util.UserAgent()
	// if s.config.SpoofUserAgent > 0 {
	// 	switch s.config.SpoofUserAgent {
	// 	case 1:
	// 		userAgent = ""
	// 		break
	// 	case 2:
	// 		userAgent = "libtorrent (Rasterbar) 1.1.0"
	// 		break
	// 	case 3:
	// 		userAgent = "BitTorrent 7.5.0"
	// 		break
	// 	case 4:
	// 		userAgent = "BitTorrent 7.4.3"
	// 		break
	// 	case 5:
	// 		userAgent = "µTorrent 3.4.9"
	// 		break
	// 	case 6:
	// 		userAgent = "µTorrent 3.2.0"
	// 		break
	// 	case 7:
	// 		userAgent = "µTorrent 2.2.1"
	// 		break
	// 	case 8:
	// 		userAgent = "Transmission 2.92"
	// 		break
	// 	case 9:
	// 		userAgent = "Deluge 1.3.6.0"
	// 		break
	// 	case 10:
	// 		userAgent = "Deluge 1.3.12.0"
	// 		break
	// 	case 11:
	// 		userAgent = "Vuze 5.7.3.0"
	// 		break
	// 	}
	// 	if userAgent != "" {
	// 		s.log.Infof("UserAgent: %s", userAgent)
	// 	}
	// } else {
	// 	s.log.Infof("UserAgent: %s", util.UserAgent())
	// }

	blocklist, err := iplist.MMapPacked("packed-blocklist")
	if err != nil {
		s.log.Debug(err)
	}

	s.ClientConfig = &gotorrent.Config{
		DataDir:               config.Get().DownloadPath,

		ListenAddr:            listenInterfacesStrings[0],

		NoDHT:                 s.config.DisableDHT,

		Seed: 					       s.config.SeedTimeLimit > 0,
		NoUpload:              s.config.SeedTimeLimit == 0,

		//PeerID:          userAgent,

		DisableEncryption:     s.config.EncryptionPolicy == 0,
		ForceEncryption:       s.config.EncryptionPolicy  == 2,

		IPBlocklist:           blocklist,

		DownloadRateLimiter:   s.DownloadLimiter,
		UploadRateLimiter:     s.UploadLimiter,
	}

	if config.Get().DownloadStorage == 2 {
		// FAT32 File Storage Driver
		s.ClientConfig.DefaultStorage = fat32storage.NewFat32Storage(config.Get().DownloadPath)
	/*} else if config.Get().DownloadStorage == 1 {
		// In-Memory Storage Driver
	*/} else {
		// Default to the file based driver.
		s.ClientConfig.DefaultStorage = storage.NewFileWithCompletion(config.Get().DownloadPath, s.PieceCompletion)
	}

	if !s.config.LimitAfterBuffering {
		s.RestoreLimits()
	}

	s.Client, err = gotorrent.NewClient(s.ClientConfig)



	// userAgent := util.UserAgent()
	// if s.config.SpoofUserAgent > 0 {
	// 	switch s.config.SpoofUserAgent {
	// 	case 1:
	// 		userAgent = ""
	// 		break
	// 	case 2:
	// 		userAgent = "libtorrent (Rasterbar) 1.1.0"
	// 		break
	// 	case 3:
	// 		userAgent = "BitTorrent 7.5.0"
	// 		break
	// 	case 4:
	// 		userAgent = "BitTorrent 7.4.3"
	// 		break
	// 	case 5:
	// 		userAgent = "µTorrent 3.4.9"
	// 		break
	// 	case 6:
	// 		userAgent = "µTorrent 3.2.0"
	// 		break
	// 	case 7:
	// 		userAgent = "µTorrent 2.2.1"
	// 		break
	// 	case 8:
	// 		userAgent = "Transmission 2.92"
	// 		break
	// 	case 9:
	// 		userAgent = "Deluge 1.3.6.0"
	// 		break
	// 	case 10:
	// 		userAgent = "Deluge 1.3.12.0"
	// 		break
	// 	case 11:
	// 		userAgent = "Vuze 5.7.3.0"
	// 		break
	// 	}
	// 	if userAgent != "" {
	// 		s.log.Infof("UserAgent: %s", userAgent)
	// 	} else {
	// 		s.log.Infof("UserAgent: libtorrent/%s", libtorrent.Version())
	// 	}
	// } else {
	// 	s.log.Infof("UserAgent: %s", util.UserAgent())
	// }
	//
	// if userAgent != "" {
	// 	s.UserAgent = userAgent
	// 	settings.SetStr(libtorrent.SettingByName("user_agent"), userAgent)
	// }
	// settings.SetInt(libtorrent.SettingByName("request_timeout"), 2)
	// settings.SetInt(libtorrent.SettingByName("peer_connect_timeout"), 2)
	// settings.SetBool(libtorrent.SettingByName("strict_end_game_mode"), true)
	// settings.SetBool(libtorrent.SettingByName("announce_to_all_trackers"), true)
	// settings.SetBool(libtorrent.SettingByName("announce_to_all_tiers"), true)
	// settings.SetInt(libtorrent.SettingByName("connection_speed"), 500)
	// settings.SetInt(libtorrent.SettingByName("download_rate_limit"), 0)
	// settings.SetInt(libtorrent.SettingByName("upload_rate_limit"), 0)
	// settings.SetInt(libtorrent.SettingByName("choking_algorithm"), 0)
	// settings.SetInt(libtorrent.SettingByName("share_ratio_limit"), 0)
	// settings.SetInt(libtorrent.SettingByName("seed_time_ratio_limit"), 0)
	// settings.SetInt(libtorrent.SettingByName("seed_time_limit"), 0)
	// settings.SetInt(libtorrent.SettingByName("peer_tos"), ipToSLowCost)
	// settings.SetInt(libtorrent.SettingByName("torrent_connect_boost"), 0)
	// settings.SetBool(libtorrent.SettingByName("rate_limit_ip_overhead"), true)
	// settings.SetBool(libtorrent.SettingByName("no_atime_storage"), true)
	// settings.SetBool(libtorrent.SettingByName("announce_double_nat"), true)
	// settings.SetBool(libtorrent.SettingByName("prioritize_partial_pieces"), false)
	// settings.SetBool(libtorrent.SettingByName("free_torrent_hashes"), true)
	// settings.SetBool(libtorrent.SettingByName("use_parole_mode"), true)
	// settings.SetInt(libtorrent.SettingByName("seed_choking_algorithm"), int(libtorrent.SettingsPackFastestUpload))
	// settings.SetBool(libtorrent.SettingByName("upnp_ignore_nonrouters"), true)
	// settings.SetBool(libtorrent.SettingByName("lazy_bitfields"), true)
	// settings.SetInt(libtorrent.SettingByName("stop_tracker_timeout"), 1)
	// settings.SetInt(libtorrent.SettingByName("auto_scrape_interval"), 1200)
	// settings.SetInt(libtorrent.SettingByName("auto_scrape_min_interval"), 900)
	// settings.SetBool(libtorrent.SettingByName("ignore_limits_on_local_network"), true)
	// settings.SetBool(libtorrent.SettingByName("rate_limit_utp"), true)
	// settings.SetInt(libtorrent.SettingByName("mixed_mode_algorithm"), int(libtorrent.SettingsPackPreferTcp))

	// // For Android external storage / OS-mounted NAS setups
	// if s.config.TunedStorage {
	// 	settings.SetBool(libtorrent.SettingByName("use_read_cache"), true)
	// 	settings.SetBool(libtorrent.SettingByName("coalesce_reads"), true)
	// 	settings.SetBool(libtorrent.SettingByName("coalesce_writes"), true)
	// 	settings.SetInt(libtorrent.SettingByName("max_queued_disk_bytes"), 10 * 1024 * 1024)
	// 	settings.SetInt(libtorrent.SettingByName("cache_size"), -1)
	// }
	//
	// if s.config.ConnectionsLimit > 0 {
	// 	settings.SetInt(libtorrent.SettingByName("connections_limit"), s.config.ConnectionsLimit)
	// } else {
	// 	setPlatformSpecificSettings(settings)
	// }
	//
	// if s.config.LimitAfterBuffering == false {
	// 	if s.config.MaxDownloadRate > 0 {
	// 		s.log.Infof("Rate limiting download to %dkB/s", s.config.MaxDownloadRate / 1024)
	// 		settings.SetInt(libtorrent.SettingByName("download_rate_limit"), s.config.MaxDownloadRate)
	// 	}
	// 	if s.config.MaxUploadRate > 0 {
	// 		s.log.Infof("Rate limiting upload to %dkB/s", s.config.MaxUploadRate / 1024)
	// 		// If we have an upload rate, use the nicer bittyrant choker
	// 		settings.SetInt(libtorrent.SettingByName("upload_rate_limit"), s.config.MaxUploadRate)
	// 		settings.SetInt(libtorrent.SettingByName("choking_algorithm"), int(libtorrent.SettingsPackBittyrantChoker))
	// 	}
	// }

	// if s.config.ShareRatioLimit > 0 {
	// 	settings.SetInt(libtorrent.SettingByName("share_ratio_limit"), s.config.ShareRatioLimit)
	// }
	// if s.config.SeedTimeRatioLimit > 0 {
	// 	settings.SetInt(libtorrent.SettingByName("seed_time_ratio_limit"), s.config.SeedTimeRatioLimit)
	// }
	// if s.config.SeedTimeLimit > 0 {
	// 	settings.SetInt(libtorrent.SettingByName("seed_time_limit"), s.config.SeedTimeLimit)
	// }
	//
	// s.log.Info("Applying encryption settings...")
	// if s.config.EncryptionPolicy > 0 {
	// 	policy := int(libtorrent.SettingsPackPeDisabled)
	// 	level := int(libtorrent.SettingsPackPeBoth)
	// 	preferRc4 := false
	//
	// 	if s.config.EncryptionPolicy == 2 {
	// 		policy = int(libtorrent.SettingsPackPeForced)
	// 		level = int(libtorrent.SettingsPackPeRc4)
	// 		preferRc4 = true
	// 	}
	//
	// 	settings.SetInt(libtorrent.SettingByName("out_enc_policy"), policy)
	// 	settings.SetInt(libtorrent.SettingByName("in_enc_policy"), policy)
	// 	settings.SetInt(libtorrent.SettingByName("allowed_enc_level"), level)
	// 	settings.SetBool(libtorrent.SettingByName("prefer_rc4"), preferRc4)
	// }

	// if s.config.Proxy != nil {
	// 	s.log.Info("Applying proxy settings...")
	// 	proxy_type := s.config.Proxy.Type + 1
	// 	settings.SetInt(libtorrent.SettingByName("proxy_type"), proxy_type)
	// 	settings.SetInt(libtorrent.SettingByName("proxy_port"), s.config.Proxy.Port)
	// 	settings.SetStr(libtorrent.SettingByName("proxy_hostname"), s.config.Proxy.Hostname)
	// 	settings.SetStr(libtorrent.SettingByName("proxy_username"), s.config.Proxy.Username)
	// 	settings.SetStr(libtorrent.SettingByName("proxy_password"), s.config.Proxy.Password)
	// 	settings.SetBool(libtorrent.SettingByName("proxy_tracker_connections"), true)
	// 	settings.SetBool(libtorrent.SettingByName("proxy_peer_connections"), true)
	// 	settings.SetBool(libtorrent.SettingByName("proxy_hostnames"), true)
	// 	settings.SetBool(libtorrent.SettingByName("force_proxy"), true)
	// 	if proxy_type == ProxyTypeI2PSAM {
	// 		settings.SetInt(libtorrent.SettingByName("i2p_port"), s.config.Proxy.Port)
	// 		settings.SetStr(libtorrent.SettingByName("i2p_hostname"), s.config.Proxy.Hostname)
	// 		settings.SetBool(libtorrent.SettingByName("allows_i2p_mixed"), false)
	// 		settings.SetBool(libtorrent.SettingByName("allows_i2p_mixed"), true)
	// 	}
	// }

	// Set alert_mask here so it also applies on reconfigure...
	// settings.SetInt(libtorrent.SettingByName("alert_mask"), int(
	// 	libtorrent.AlertStatusNotification |
	// 	libtorrent.AlertStorageNotification |
	// 	libtorrent.AlertErrorNotification))
	//
	// s.packSettings = settings
	// s.Session.GetHandle().ApplySettings(s.packSettings)
}

// func (s *BTService) WriteState(f io.Writer) error {
// 	entry := libtorrent.NewEntry()
// 	defer libtorrent.DeleteEntry(entry)
// 	s.Session.GetHandle().SaveState(entry, 0xFFFF)
// 	_, err := f.Write([]byte(libtorrent.Bencode(entry)))
// 	return err
// }

// func (s *BTService) LoadState(f io.Reader) error {
// 	data, err := ioutil.ReadAll(f)
// 	if err != nil {
// 		return err
// 	}
// 	entry := libtorrent.NewEntry()
// 	defer libtorrent.DeleteEntry(entry)
// 	libtorrent.Bdecode(string(data), entry)
// 	s.Session.GetHandle().LoadState(entry)
// 	return nil
// }

func (s *BTService) startServices() {
	// var listenPorts []string
	// for p := s.config.LowerListenPort; p <= s.config.UpperListenPort; p++ {
	// 	listenPorts = append(listenPorts, strconv.Itoa(p))
	// }
	// rand.Seed(time.Now().UTC().UnixNano())
	//
	// listenInterfaces := []string{"0.0.0.0"}
	// if strings.TrimSpace(s.config.ListenInterfaces) != "" {
	// 	listenInterfaces = strings.Split(strings.Replace(strings.TrimSpace(s.config.ListenInterfaces), " ", "", -1), ",")
	// }
	//
	// listenInterfacesStrings := make([]string, 0)
	// for _, listenInterface := range listenInterfaces {
	// 	listenInterfacesStrings = append(listenInterfacesStrings, listenInterface + ":" + listenPorts[rand.Intn(len(listenPorts))])
	// 	if len(listenPorts) > 1 {
	// 		listenInterfacesStrings = append(listenInterfacesStrings, listenInterface + ":" + listenPorts[rand.Intn(len(listenPorts))])
	// 	}
	// }
	// s.packSettings.SetStr(libtorrent.SettingByName("listen_interfaces"), strings.Join(listenInterfacesStrings, ","))
	//
	// if strings.TrimSpace(s.config.OutgoingInterfaces) != "" {
	// 	s.packSettings.SetStr(libtorrent.SettingByName("outgoing_interfaces"), strings.Replace(strings.TrimSpace(s.config.OutgoingInterfaces), " ", "", -1))
	// }

	// s.log.Info("Starting LSD...")
	// s.packSettings.SetBool(libtorrent.SettingByName("enable_lsd"), true)

	// if s.config.DisableDHT == false {
	// 	s.log.Info("Starting DHT...")
	// 	bootstrap_nodes := strings.Join(dhtBootstrapNodes, ":6881,") + ":6881"
	// 	s.packSettings.SetStr(libtorrent.SettingByName("dht_bootstrap_nodes"), bootstrap_nodes)
	// 	s.packSettings.SetBool(libtorrent.SettingByName("enable_dht"), true)
	// }

	// if s.config.DisableUPNP == false {
	// 	s.log.Info("Starting UPNP...")
	// 	s.packSettings.SetBool(libtorrent.SettingByName("enable_upnp"), true)
	//
	// 	s.log.Info("Starting NATPMP...")
	// 	s.packSettings.SetBool(libtorrent.SettingByName("enable_natpmp"), true)
	// }
	//
	// s.Session.GetHandle().ApplySettings(s.packSettings)
}

func (s *BTService) stopServices() {
	if s.dialogProgressBG != nil {
		s.dialogProgressBG.Close()
	}
	s.dialogProgressBG = nil
	xbmc.ResetRPC()

	s.Client.Close()

	// s.log.Info("Stopping LSD...")
	// s.packSettings.SetBool(libtorrent.SettingByName("enable_lsd"), false)
	//
	// if s.config.DisableDHT == false {
	// 	s.log.Info("Stopping DHT...")
	// 	s.packSettings.SetBool(libtorrent.SettingByName("enable_dht"), false)
	// }
	//
	// if s.config.DisableUPNP == false {
	// 	s.log.Info("Stopping UPNP...")
	// 	s.packSettings.SetBool(libtorrent.SettingByName("enable_upnp"), false)
	//
	// 	s.log.Info("Stopping NATPMP...")
	// 	s.packSettings.SetBool(libtorrent.SettingByName("enable_natpmp"), false)
	// }
	//
	// s.Session.GetHandle().ApplySettings(s.packSettings)
}

func (s *BTService) CheckAvailableSpace(torrent *Torrent) bool {
	diskStatus := &diskusage.DiskStatus{}
	if status, err := diskusage.DiskUsage(config.Get().DownloadPath); err != nil {
		s.log.Warningf("Unable to retrieve the free space for %s, continuing anyway...", config.Get().DownloadPath)
		return false
	} else {
		diskStatus = status
	}

	if torrent == nil || torrent.Info() == nil {
		s.log.Warning("Missing torrent info to check available space.")
		return false
	}

	totalSize := torrent.BytesCompleted() + torrent.BytesMissing()
	totalDone := torrent.BytesCompleted()
	sizeLeft := torrent.BytesMissing()
	availableSpace := diskStatus.Free
	path := s.ClientConfig.DataDir

	if torrent.IsRarArchive {
		sizeLeft = sizeLeft * 2
	}

	s.log.Infof("Checking for sufficient space on %s...", path)
	s.log.Infof("Total size of download: %s", humanize.Bytes(uint64(totalSize)))
	s.log.Infof("All time download: %s", humanize.Bytes(uint64(torrent.BytesCompleted())))
	s.log.Infof("Size total done: %s", humanize.Bytes(uint64(totalDone)))
	if torrent.IsRarArchive {
		s.log.Infof("Size left to download (x2 to extract): %s", humanize.Bytes(uint64(sizeLeft)))
	} else {
		s.log.Infof("Size left to download: %s", humanize.Bytes(uint64(sizeLeft)))
	}
	s.log.Infof("Available space: %s", humanize.Bytes(uint64(availableSpace)))

	if availableSpace < sizeLeft {
		s.log.Errorf("Unsufficient free space on %s. Has %d, needs %d.", path, diskStatus.Free, sizeLeft)
		xbmc.Notify("Quasar", "LOCALIZE[30207]", config.AddonIcon())

		torrent.Pause()
		return false
	}

	return true
}

func (s *BTService) AddTorrent(uri string) (*Torrent, error) {
	s.log.Infof("Adding torrent from %s", uri)

	if s.config.DownloadPath == "." {
		xbmc.Notify("Quasar", "LOCALIZE[30113]", config.AddonIcon())
		return nil, fmt.Errorf("Download path empty")
	}

	var err error
	var torrentHandle *gotorrent.Torrent
	if strings.HasPrefix(uri, "magnet:") {
		if torrentHandle, err = s.Client.AddMagnet(uri); err != nil {
			return nil, err
		}
		uri = ""
	} else {
		if strings.HasPrefix(uri, "http") {
			torrent := NewTorrentFile(uri)

			if err = torrent.Resolve(); err != nil {
				s.log.Warningf("Could not resolve torrent %s: %#v", uri, err)
				return nil, err
			}
			uri = torrent.URI
		}

		if torrentHandle, err = s.Client.AddTorrentFromFile(uri); err != nil {
			s.log.Warningf("Could not add torrent %s: %#v", uri, err)
			return nil, err
		}
	}

	// for _, f := range torrentHandle.Files() {
	// 	f.Cancel()
	//
	// 	// path := filepath.Join(s.ClientConfig.DataDir, f.Path())
	// 	// if _, err := os.Stat(path); err == nil {
	// 	// 	defer os.Remove(path)
	// 	// }
	// }

	torrent := NewTorrent(s, torrentHandle, uri)
	if s.config.ConnectionsLimit > 0 {
		torrentHandle.SetMaxEstablishedConns(s.config.ConnectionsLimit)
	}

	s.Torrents = append(s.Torrents, torrent)
	return torrent, nil
}

func (s *BTService) RemoveTorrent(torrent *Torrent, removeFiles bool) bool {
	s.log.Debugf("Removing torrent: %s", torrent.Name())
	if torrent == nil {
		return false
	}

	query := torrent.InfoHash()
	matched := -1
	for i, t := range s.Torrents {
		if t.InfoHash() == query {
			matched = i
			break
		}
	}

	if matched > -1 {
		t := s.Torrents[matched]

		go func() {
			t.Drop(removeFiles)
			t = nil
		}()

		s.Torrents = append(s.Torrents[:matched], s.Torrents[matched+1:]...)
		return true
	}

	return false
}

func (s *BTService) loadTorrentFiles() {
	pattern  := filepath.Join(s.config.TorrentsPath, "*.torrent")
	files, _ := filepath.Glob(pattern)

	for _, torrentFile := range files {
		s.log.Infof("Loading torrent file %s", torrentFile)

		torrentHandle := &gotorrent.Torrent{}
		var err error
		if torrentHandle, err = s.Client.AddTorrentFromFile(torrentFile); err != nil || torrentHandle == nil {
			s.log.Errorf("Error adding torrent file for %s", torrentFile)
			if _, err := os.Stat(torrentFile); err == nil {
				if err := os.Remove(torrentFile); err != nil {
					s.log.Error(err)
				}
			}

			continue
		}

		torrent  := NewTorrent(s, torrentHandle, torrentFile)

		s.Torrents = append(s.Torrents, torrent)
	}
}

func (s *BTService) downloadProgress() {
	rotateTicker := time.NewTicker(5 * time.Second)
	defer rotateTicker.Stop()

	pathChecked := make(map[string]bool)
	warnedMissing := make(map[string]bool)

	showNext := 0
	for {
		select {
		case <-rotateTicker.C:
			if !s.config.DisableBgProgress && s.dialogProgressBG != nil {
				s.dialogProgressBG.Close()
				s.dialogProgressBG = nil
				continue
			}

			var totalDownloadRate int64
			var totalUploadRate   int64
			var totalProgress     int

			activeTorrents := make([]*activeTorrent, 0)

			for _, torrentHandle := range s.Torrents {
				if torrentHandle == nil {
					continue
				}

				torrentName := torrentHandle.Info().Name
				progress    := int(torrentHandle.GetProgress())
				status      := torrentHandle.GetStateString()

				totalDownloadRate += torrentHandle.DownloadRate
				totalUploadRate   += torrentHandle.UploadRate

				if progress < 100 && status != "Paused" {
					activeTorrents = append(activeTorrents, &activeTorrent{
						torrentName:  torrentName,
						downloadRate: float64(torrentHandle.DownloadRate),
						uploadRate:   float64(torrentHandle.UploadRate),
						progress:     progress,
					})
					totalProgress += progress
					continue
				}

				//
				// Handle moving completed downloads
				//
				if !s.config.CompletedMove || status != "Seeded" || Playing {
					continue
				}
				if xbmc.PlayerIsPlaying() {
					continue
				}

				infoHash := torrentHandle.InfoHash()
				if _, exists := warnedMissing[infoHash]; exists {
					continue
				}

				item := &DBItem{}
				func() error {
					if err := db.GetObject(Bucket, infoHash, item); err != nil {
						warnedMissing[infoHash] = true
						return err
					}

					errMsg := fmt.Sprintf("Missing item type to move files to completed folder for %s", torrentName)
					if item.Type == "" {
						s.log.Error(errMsg)
						return errors.New(errMsg)
					} else {
						s.log.Warning(torrentName, "finished seeding, moving files...")

						// Check paths are valid and writable, and only once
						if _, exists := pathChecked[item.Type]; !exists {
							if item.Type == "movie" {
								if err := config.IsWritablePath(s.config.CompletedMoviesPath); err != nil {
									warnedMissing[infoHash] = true
									pathChecked[item.Type] = true
									s.log.Error(err)
									return err
								}
								pathChecked[item.Type] = true
							} else {
								if err := config.IsWritablePath(s.config.CompletedShowsPath); err != nil {
									warnedMissing[infoHash] = true
									pathChecked[item.Type] = true
									s.log.Error(err)
									return err
								}
								pathChecked[item.Type] = true
							}
						}

						s.log.Info("Removing the torrent without deleting files...")
						s.RemoveTorrent(torrentHandle, false)

						// Delete torrent file
						torrentFile := filepath.Join(s.config.TorrentsPath, fmt.Sprintf("%s.torrent", infoHash))
						if _, err := os.Stat(torrentFile); err == nil {
							s.log.Info("Deleting torrent file at ", torrentFile)
							if err := os.Remove(torrentFile); err != nil {
								s.log.Error(err)
								return err
							}
						}

						filePath := torrentHandle.Files()[item.File].Path()
						fileName := filepath.Base(filePath)

						extracted := ""
						re := regexp.MustCompile("(?i).*\\.rar")
						if re.MatchString(fileName) {
							extractedPath := filepath.Join(s.config.DownloadPath, filepath.Dir(filePath), "extracted")
							files, err := ioutil.ReadDir(extractedPath)
							if err != nil {
								return err
							}
							if len(files) == 1 {
								extracted = files[0].Name()
							} else {
								for _, file := range files {
									fileName := file.Name()
									re := regexp.MustCompile("(?i).*\\.(mkv|mp4|mov|avi)")
									if re.MatchString(fileName) {
										extracted = fileName
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
							dstPath = filepath.Dir(s.config.CompletedMoviesPath)
						} else {
							dstPath = filepath.Dir(s.config.CompletedShowsPath)
							if item.ShowID > 0 {
								show := tmdb.GetShow(item.ShowID, "en")
								if show != nil {
									showPath := util.ToFileName(fmt.Sprintf("%s (%s)", show.Name, strings.Split(show.FirstAirDate, "-")[0]))
									seasonPath := filepath.Join(showPath, fmt.Sprintf("Season %d", item.Season))
									if item.Season == 0 {
										seasonPath = filepath.Join(showPath, "Specials")
									}
									dstPath = filepath.Join(dstPath, seasonPath)
									os.MkdirAll(dstPath, 0755)
								}
							}
						}

						go func() {
							s.log.Infof("Moving %s to %s", fileName, dstPath)
							srcPath := filepath.Join(s.config.DownloadPath, filePath)
							if dst, err := util.Move(srcPath, dstPath); err != nil {
								s.log.Error(err)
							} else {
								// Remove leftover folders
								if dirPath := filepath.Dir(filePath); dirPath != "." {
									os.RemoveAll(filepath.Dir(srcPath))
									if extracted != "" {
										parentPath := filepath.Clean(filepath.Join(filepath.Dir(srcPath), ".."))
										if parentPath != "." && parentPath != s.config.DownloadPath {
											os.RemoveAll(parentPath)
										}
									}
								}
								s.log.Warning(fileName, "moved to", dst)

								s.log.Infof("Marking %s for removal from library and database...", torrentName)
								s.UpdateDB(RemoveFromLibrary, infoHash, 0, "")
							}
						}()
					}
					return nil
				}()
			}

			totalActive := len(activeTorrents)
			if totalActive > 0 {
				showProgress := totalProgress / totalActive
				showTorrent := fmt.Sprintf("Total - D/L: %s - U/L: %s", humanize.Bytes(uint64(totalDownloadRate)) + "/s", humanize.Bytes(uint64(totalUploadRate)) + "/s")
				if showNext >= totalActive {
					showNext = 0
				} else {
					showProgress = activeTorrents[showNext].progress
					torrentName := activeTorrents[showNext].torrentName
					if len(torrentName) > 30 {
						torrentName = torrentName[:30] + "..."
					}
					showTorrent = fmt.Sprintf("%s - %s - %s", torrentName, humanize.Bytes(uint64(activeTorrents[showNext].downloadRate)) + "/s", humanize.Bytes(uint64(activeTorrents[showNext].uploadRate)) + "/s")
					showNext += 1
				}
				if !s.config.DisableBgProgress {
					if s.dialogProgressBG == nil {
						s.dialogProgressBG = xbmc.NewDialogProgressBG("Quasar", "")
					}
					// s.log.Debugf("Dialog progress: %#v, %#v", showProgress, showTorrent)
					s.dialogProgressBG.Update(showProgress, "Quasar", showTorrent)
				}
			} else if !s.config.DisableBgProgress && s.dialogProgressBG != nil {
				s.dialogProgressBG.Close()
				s.dialogProgressBG = nil
			}
		}
	}
}

//
// Database updates
//
func (s *BTService) UpdateDB(Operation int, InfoHash string, ID int, Type string, infos ...int) error {
	switch Operation {
	case Delete:
		return db.Delete(Bucket, InfoHash)
	case Update:
		item := DBItem{
			State:   Active,
			ID:      ID,
			Type:    Type,
			File:    infos[0],
			ShowID:  infos[1],
			Season:  infos[2],
			Episode: infos[3],
		}
		return db.SetObject(Bucket, InfoHash, item)
	case RemoveFromLibrary:
		item := &DBItem{}
		if err := db.GetObject(Bucket, InfoHash, item); err != nil {
			s.log.Error(err)
			return err
		}

		item.State = Remove
		return db.SetObject(Bucket, InfoHash, item)
	}

	return nil
}

func (s *BTService) GetDBItem(infoHash string) (dbItem *DBItem) {
	if err := db.GetObject(Bucket, infoHash, dbItem); err != nil {
		return nil
	}
	return dbItem
}

func (s *BTService) SetDownloadLimit(i int) {
	if i == 0 {
		s.DownloadLimiter.SetLimit(rate.Inf)
	} else {
		s.DownloadLimiter.SetLimit(rate.Limit(i))
	}
}

func (s *BTService) SetUploadLimit(i int) {
	if i == 0 {
		s.UploadLimiter.SetLimit(rate.Inf)
	} else {
		s.UploadLimiter.SetLimit(rate.Limit(i))
	}
}

func (s *BTService) RestoreLimits() {
	if s.config.MaxDownloadRate > 0 {
		s.SetDownloadLimit(s.config.MaxDownloadRate)
		s.log.Infof("Rate limiting download to %dkB/s", s.config.MaxDownloadRate / 1024)
	}
	if s.config.MaxUploadRate > 0 {
		s.SetUploadLimit(s.config.MaxUploadRate)
		s.log.Infof("Rate limiting upload to %dkB/s", s.config.MaxUploadRate / 1024)
	}
}

func (s *BTService) SetBufferLimits() {
	if s.config.LimitAfterBuffering {
		s.SetDownloadLimit(0)
		s.log.Info("Resetting rate limited download for buffering")
	}
}

func (s *BTService) GetSeedTime() int64 {
	return int64(s.config.SeedTimeLimit)
}

func (s *BTService) GetBufferSize() int64 {
	if s.config.BufferSize < endBufferSize {
		return endBufferSize
	} else {
		return s.config.BufferSize
	}
}

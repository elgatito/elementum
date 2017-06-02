package main

import (
	"os"
	"time"
	"strings"
	"runtime"
	"strconv"
	"net/http"
	"path/filepath"
  //_ "net/http/pprof"

	"github.com/op/go-logging"
	"github.com/scakemyer/quasar/api"
	"github.com/scakemyer/quasar/lockfile"
	"github.com/scakemyer/quasar/bittorrent"
	"github.com/scakemyer/quasar/config"
	"github.com/scakemyer/quasar/database"
	"github.com/scakemyer/quasar/trakt"
	"github.com/scakemyer/quasar/util"
	"github.com/scakemyer/quasar/xbmc"
)

var log = logging.MustGetLogger("main")

const (
	QuasarLogo = `________
\_____  \  __ _______    ___________ _______
 /  / \  \|  |  \__  \  /  ___/\__  \\_  __ \
/   \_/.  \  |  // __ \_\___ \  / __ \|  | \/
\_____\ \_/____/(____  /____  >(____  /__|
       \__>          \/     \/      \/
`
)

func ensureSingleInstance(conf *config.Configuration) (lock *lockfile.LockFile, err error) {
	file := filepath.Join(conf.Info.Path, ".lockfile")
	lock, err = lockfile.New(file)
	if err != nil {
		log.Critical("Unable to initialize lockfile:", err)
		return
	}
	var pid int
	var p *os.Process
	pid, err = lock.Lock()
	if err != nil {
		log.Warningf("Unable to acquire lock %q: %v, killing...", lock.File, err)
		p, err = os.FindProcess(pid)
		if err != nil {
			log.Warning("Unable to find other process:", err)
			return
		}
		if err = p.Kill(); err != nil {
			log.Critical("Unable to kill other process:", err)
			return
		}
		if err = os.Remove(lock.File); err != nil {
			log.Critical("Unable to remove lockfile")
			return
		}
		_, err = lock.Lock()
	}
	return
}

func makeBTConfiguration(conf *config.Configuration) *bittorrent.BTConfiguration {
	btConfig := &bittorrent.BTConfiguration{
		// SpoofUserAgent:      conf.SpoofUserAgent,
		DownloadStorage:     conf.DownloadStorage,
		MemorySize:          int64(conf.MemorySize),
		BufferSize:          int64(conf.BufferSize),
		MaxUploadRate:       conf.UploadRateLimit,
		MaxDownloadRate:     conf.DownloadRateLimit,
		LimitAfterBuffering: conf.LimitAfterBuffering,
		ConnectionsLimit:    conf.ConnectionsLimit,
		// SessionSave:         conf.SessionSave,
		// ShareRatioLimit:     conf.ShareRatioLimit,
		// SeedTimeRatioLimit:  conf.SeedTimeRatioLimit,
		SeedTimeLimit:       conf.SeedTimeLimit,
		DisableDHT:          conf.DisableDHT,
		// DisableUPNP:         conf.DisableUPNP,
		EncryptionPolicy:    conf.EncryptionPolicy,
		LowerListenPort:     conf.BTListenPortMin,
		UpperListenPort:     conf.BTListenPortMax,
		ListenInterfaces:    conf.ListenInterfaces,
		OutgoingInterfaces:  conf.OutgoingInterfaces,
		// TunedStorage:        conf.TunedStorage,
		DownloadPath:        conf.DownloadPath,
		TorrentsPath:        conf.TorrentsPath,
		DisableBgProgress:   conf.DisableBgProgress,
		CompletedMove:       conf.CompletedMove,
		CompletedMoviesPath: conf.CompletedMoviesPath,
		CompletedShowsPath:  conf.CompletedShowsPath,
	}

	// if conf.SocksEnabled == true {
	// 	btConfig.Proxy = &bittorrent.ProxySettings{
	// 		Type:     conf.ProxyType,
	// 		Hostname: conf.SocksHost,
	// 		Port:     conf.SocksPort,
	// 		Username: conf.SocksLogin,
	// 		Password: conf.SocksPassword,
	// 	}
	// }

	return btConfig
}

func main() {
	// Make sure we are properly multithreaded.
	runtime.GOMAXPROCS(runtime.NumCPU())

	logging.SetFormatter(logging.MustStringFormatter(
		`%{color}%{level:.4s}  %{module:-12s} ▶ %{shortfunc:-15s}  %{color:reset}%{message}`,
	))
	logging.SetBackend(logging.NewLogBackend(os.Stdout, "", 0))

	for _, line := range strings.Split(QuasarLogo, "\n") {
		log.Debug(line)
	}
	log.Infof("Version: %s Go: %s", util.Version[1:len(util.Version) - 1], runtime.Version())

	conf := config.Reload()

	log.Infof("Addon: %s v%s", conf.Info.Id, conf.Info.Version)

	lock, err := ensureSingleInstance(conf)
	defer lock.Unlock()
	if err != nil {
		log.Warningf("Unable to acquire lock %q: %v, exiting...", lock.File, err)
		os.Exit(1)
	}

	wasFirstRun := Migrate()

	db, err := database.InitDB(conf)
	if err != nil {
		log.Error(err)
		return
	}

	api.InitDB()
	bittorrent.InitDB()

	btService := bittorrent.NewBTService(*makeBTConfiguration(conf))

	var shutdown = func() {
		log.Info("Shutting down...")
		api.CloseLibrary()
		btService.Close()
		db.Close()
		log.Info("Goodbye")
		os.Exit(0)
	}

	var watchParentProcess = func() {
		for {
			if os.Getppid() == 1 {
				log.Warning("Parent shut down, shutting down too...")
				go shutdown()
				break
			}
			time.Sleep(1 * time.Second)
		}
	}
	go watchParentProcess()

	http.Handle("/", api.Routes(btService))
	http.Handle("/files/", bittorrent.TorrentFSHandler(btService, config.Get().DownloadPath))
	http.Handle("/reload", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		btService.Reconfigure(*makeBTConfiguration(config.Reload()))
	}))
	http.Handle("/shutdown", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		shutdown()
	}))

	xbmc.Notify("Quasar", "LOCALIZE[30208]", config.AddonIcon())

	go func() {
		if !wasFirstRun {
			log.Info("Updating Kodi add-on repositories...")
			xbmc.UpdateAddonRepos()
		}

		xbmc.ResetRPC()
	}()

	go api.LibraryUpdate()
	go api.LibraryListener()
	go trakt.TokenRefreshHandler()

	http.ListenAndServe(":" + strconv.Itoa(config.ListenPort), nil)
}

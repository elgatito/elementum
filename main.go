package main

import (
	_ "github.com/anacrolix/envpprof"

	"io/ioutil"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"syscall"
	"time"

	"github.com/anacrolix/sync"
	"github.com/anacrolix/tagflag"
	"github.com/op/go-logging"

	"github.com/elgatito/elementum/api"
	"github.com/elgatito/elementum/bittorrent"
	"github.com/elgatito/elementum/config"
	"github.com/elgatito/elementum/database"
	"github.com/elgatito/elementum/library"
	"github.com/elgatito/elementum/lockfile"
	"github.com/elgatito/elementum/scrape"
	"github.com/elgatito/elementum/trakt"
	"github.com/elgatito/elementum/util"
	"github.com/elgatito/elementum/xbmc"
)

var log = logging.MustGetLogger("main")

func init() {
	// TODO: does this really work in this manner, without setting it before program starts?
	os.Setenv("GODEBUG", os.Getenv("GODEBUG")+",madvdontneed=1")
	sync.Enable()
}

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

func main() {
	now := time.Now()

	tagflag.Parse(&config.Args)

	// Make sure we are properly multithreaded.
	runtime.GOMAXPROCS(runtime.NumCPU())

	logging.SetFormatter(logging.MustStringFormatter(
		`%{color}%{level:.4s}  %{module:-12s} ▶ %{shortfunc:-15s}  %{color:reset}%{message}`,
	))
	logging.SetBackend(logging.NewLogBackend(ioutil.Discard, "", 0), logging.NewLogBackend(os.Stdout, "", 0))

	log.Infof("Starting Elementum daemon")
	log.Infof("Version: %s LibTorrent: %s Go: %s, Threads: %d", util.GetVersion(), util.GetTorrentVersion(), runtime.Version(), runtime.GOMAXPROCS(0))

	conf := config.Reload()
	xbmc.KodiVersion = conf.Platform.Kodi

	log.Infof("Addon: %s v%s", conf.Info.ID, conf.Info.Version)

	lock, err := ensureSingleInstance(conf)
	defer lock.Unlock()
	if err != nil {
		log.Warningf("Unable to acquire lock %q: %v, exiting...", lock.File, err)
		os.Exit(1)
	}

	db, err := database.InitStormDB(conf)
	if err != nil {
		log.Error(err)
		return
	}

	cacheDb, errCache := database.InitCacheDB(conf)
	if errCache != nil {
		log.Error(errCache)
		return
	}

	s := bittorrent.NewService()

	var shutdown = func(fromSignal bool) {
		if s == nil || s.Closer.IsSet() {
			return
		}

		s.Closer.Set()

		log.Info("Shutting down...")
		library.CloseLibrary()
		s.Close(true)

		db.Close()
		cacheDb.Close()

		log.Info("Goodbye")

		// If we don't give an exit code - python treat as well done and not
		// restarting the daemon. So when we come here from Signal -
		// we should properly exit with non-0 exitcode.
		if !fromSignal {
			os.Exit(0)
		} else {
			os.Exit(1)
		}
	}

	var watchParentProcess = func() {
		for {
			if os.Getppid() == 1 {
				log.Warning("Parent shut down, shutting down too...")
				go shutdown(false)
				break
			}
			time.Sleep(1 * time.Second)
		}
	}
	go watchParentProcess()

	http.Handle("/", api.Routes(s))

	http.Handle("/info", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.ClientInfo(w)
	}))
	http.Handle("/debug/all", bittorrent.DebugAll(s))
	http.Handle("/debug/bundle", bittorrent.DebugBundle(s))

	http.Handle("/files/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Connection", "close")
		handler := http.StripPrefix("/files/", http.FileServer(bittorrent.NewTorrentFS(s)))
		handler.ServeHTTP(w, r)
	}))
	http.Handle("/reload", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.Reconfigure()
	}))
	http.Handle("/notification", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		Notification(w, r, s)
	}))
	http.Handle("/shutdown", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		shutdown(false)
	}))

	if config.Get().GreetingEnabled {
		xbmc.Notify("Elementum", "LOCALIZE[30208]", config.AddonIcon())
	}

	sigc := make(chan os.Signal, 2)
	signal.Notify(sigc,
		syscall.SIGHUP,
		syscall.SIGINT,
		syscall.SIGTERM,
		syscall.SIGQUIT)
	signal.Ignore(syscall.SIGPIPE, syscall.SIGILL)
	defer close(sigc)

	go func() {
		closer := s.Closer.C()

		for {
			select {
			case <-closer:
				return
			case <-sigc:
				shutdown(true)
			}
		}
	}()

	go func() {
		if checkRepository() {
			log.Info("Updating Kodi add-on repositories... ")
			xbmc.UpdateAddonRepos()
		}

		xbmc.DialogProgressBGCleanup()
		xbmc.ResetRPC()
	}()

	go library.Init()
	go trakt.TokenRefreshHandler()
	go db.MaintenanceRefreshHandler()
	go cacheDb.MaintenanceRefreshHandler()
	go scrape.Start()

	log.Infof("Prepared in %s", time.Since(now))
	log.Infof("Starting HTTP server")
	if err = http.ListenAndServe(":"+strconv.Itoa(config.Args.LocalPort), nil); err != nil {
		panic(err)
	}
}

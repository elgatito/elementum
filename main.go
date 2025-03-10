package main

import (
	"expvar"
	"fmt"
	"io"
	"net/http"
	"net/http/pprof"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"
	"syscall"
	"time"

	"github.com/anacrolix/missinggo/perf"
	"github.com/anacrolix/sync"
	"github.com/anacrolix/tagflag"
	"github.com/op/go-logging"
	"gopkg.in/natefinch/lumberjack.v2"

	"github.com/elgatito/elementum/api"
	"github.com/elgatito/elementum/bittorrent"
	"github.com/elgatito/elementum/broadcast"
	"github.com/elgatito/elementum/config"
	"github.com/elgatito/elementum/database"
	"github.com/elgatito/elementum/exit"
	"github.com/elgatito/elementum/library"
	"github.com/elgatito/elementum/lockfile"
	"github.com/elgatito/elementum/repository"
	"github.com/elgatito/elementum/trakt"
	"github.com/elgatito/elementum/util"
	"github.com/elgatito/elementum/util/ident"
	"github.com/elgatito/elementum/util/ip"
	"github.com/elgatito/elementum/xbmc"
)

var (
	log        = logging.MustGetLogger("main")
	logPath    = ""
	fileLogger *lumberjack.Logger
)

func init() {
	sync.Enable()
}

func ensureSingleInstance(conf *config.Configuration) (lock *lockfile.LockFile, err error) {
	// Avoid killing any process when running as a shared library
	if exit.IsShared {
		return
	}

	file := filepath.Join(conf.Info.Profile, ".lockfile")
	lock, err = lockfile.New(file)
	if err != nil {
		log.Critical("Unable to initialize lockfile:", err)
		return
	}
	var pid int
	var p *os.Process
	pid, err = lock.Lock()
	if pid <= 0 {
		if err = os.Remove(lock.File); err != nil {
			log.Critical("Unable to remove lockfile")
			return
		}
		_, err = lock.Lock()
	} else if err != nil {
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

func stopLogging() {
	if fileLogger != nil {
		log.Infof("Stopping file logger")
		fileLogger.Close()
		fileLogger = nil
	}
}

func setupLogging() {
	var backend *logging.LogBackend

	if config.Args.LogPath != "" {
		logPath = config.Args.LogPath
	}
	if logPath != "" && util.IsWritablePath(filepath.Dir(logPath)) == nil {
		fileLogger = &lumberjack.Logger{
			Filename:   logPath,
			MaxSize:    10, // Size in Megabytes
			MaxBackups: 5,
		}
		backend = logging.NewLogBackend(fileLogger, "", 0)
	} else {
		backend = logging.NewLogBackend(os.Stdout, "", 0)
	}

	// Make sure we reset to initial state before configuring logger instance
	logging.Reset()
	logging.SetFormatter(logging.MustStringFormatter(
		`%{color}%{level:.4s}  %{module:-12s} ▶ %{shortfunc:-15s}  %{color:reset}%{message}`,
	))
	logging.SetBackend(logging.NewLogBackend(io.Discard, "", 0), backend)
}

func main() {
	now := time.Now()

	// If running in shared library mode, parse Args from variable, provided by library caller.
	if !exit.IsShared || exit.Args == "" {
		tagflag.Parse(&config.Args)
	} else {
		if err := tagflag.ParseErr(&config.Args, strings.Fields(exit.Args)); err != nil {
			fmt.Printf("Error parsing CLI arguments: %s", err)
			exit.Exit(exit.ExitCodeError)
			return
		}
	}

	// Make sure we are properly multithreaded.
	runtime.GOMAXPROCS(runtime.NumCPU())

	setupLogging()

	defer func() {
		stopLogging()

		if r := recover(); r != nil {
			log.Errorf("Got a panic: %s", r)
			log.Errorf("Stacktrace: \n" + string(debug.Stack()))
			exit.Exit(exit.ExitCodeError)
		}
	}()

	if exit.IsShared {
		log.Infof("Starting Elementum daemon in shared library mode")
	} else {
		log.Infof("Starting Elementum daemon")
	}
	log.Infof("Version: %s LibTorrent: %s Go: %s, Threads: %d", ident.GetVersion(), ident.GetTorrentVersion(), runtime.Version(), runtime.GOMAXPROCS(0))

	// Init default XBMC connections
	xbmc.Init()

	conf, err := config.Reload()
	if err != nil || conf == nil {
		log.Errorf("Could not get addon configuration: %s", err)
		exit.Exit(exit.ExitCodeError)
		return
	}

	xbmc.KodiVersion = conf.Platform.Kodi

	log.Infof("Addon: %s v%s", conf.Info.ID, conf.Info.Version)

	lock, err := ensureSingleInstance(conf)
	if err != nil {
		if lock != nil {
			log.Warningf("Unable to acquire lock %q: %s, exiting...", lock.File, err)
		} else {
			log.Warningf("Unable to acquire lock: %s, exiting...", err)
		}
		exit.Exit(exit.ExitCodeError)
	}
	if lock != nil {
		defer lock.Unlock()
	}

	db, err := database.InitStormDB(conf)
	if err != nil {
		log.Errorf("Could not open application database: %s", err)
		exit.Exit(exit.ExitCodeError)
		return
	}

	cacheDB, errCache := database.InitCacheDB(conf)
	if errCache != nil {
		log.Errorf("Could not open cache database: %s", errCache)
		exit.Exit(exit.ExitCodeError)
		return
	}

	s := bittorrent.NewService()

	var shutdown = func(code int) {
		if s == nil || s.Closer.IsSet() {
			return
		}

		// Set global Closer
		broadcast.Closer.Set()

		s.Closer.Set()

		log.Infof("Shutting down with code %d ...", code)
		library.CloseLibrary()
		s.Close(true)

		db.Close()
		cacheDB.Close()

		// Wait until service is finally stopped
		<-s.CloserNotifier.C()

		log.Info("Goodbye")

		if lock != nil {
			lock.Unlock()
		}

		// If we don't give an exit code - python treat as well done and not
		// restarting the daemon. So when we come here from Signal -
		// we should properly exit with non-0 exitcode.
		exit.Exit(code)
	}

	var watchParentProcess = func() {
		for {
			if os.Getppid() == 1 {
				log.Warning("Parent shut down, shutting down too...")
				go shutdown(exit.ExitCodeSuccess)
				break
			}
			time.Sleep(1 * time.Second)
		}
	}
	// If we run with custom config, then we run as daemon, thus no need to watch for parent process
	if config.Args.ConfigPath == "" && !config.Args.DisableParentProcessWatcher {
		go watchParentProcess()
	}

	// Make sure HTTP mux is empty
	http.DefaultServeMux = new(http.ServeMux)

	// Debug handlers
	http.HandleFunc("/debug/pprof/", pprof.Index)
	http.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	http.HandleFunc("/debug/pprof/profile", pprof.Profile)
	http.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	http.HandleFunc("/debug/pprof/trace", pprof.Trace)
	http.HandleFunc("/debug/perf", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=UTF-8")
		perf.WriteEventsTable(w)
	})
	http.HandleFunc("/debug/lockTimes", func(w http.ResponseWriter, r *http.Request) {
		sync.PrintLockTimes(w)
	})
	http.Handle("/debug/vars", expvar.Handler())

	http.Handle("/", api.Routes(s, shutdown, fileLogger))

	http.Handle("/files/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Connection", "close")
		handler := http.StripPrefix("/files/", http.FileServer(bittorrent.NewTorrentFS(s, r.Method)))
		handler.ServeHTTP(w, r)
	}))

	if config.Get().GreetingEnabled {
		if xbmcHost, _ := xbmc.GetLocalXBMCHost(); xbmcHost != nil {
			xbmcHost.Notify("Elementum", "LOCALIZE[30208]", config.AddonIcon())
		}
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
			case <-exit.Closer.C():
				shutdown(exit.ExitCodeSuccess)
			case <-sigc:
				log.Infof("Initiating shutdown after receiving signal")
				shutdown(exit.ExitCodeError)
			}
		}
	}()

	go func() {
		xbmcHost, _ := xbmc.GetLocalXBMCHost()
		if xbmcHost == nil || !xbmcHost.Ping() {
			return
		}

		if repository.CheckRepository(xbmcHost, conf.SkipRepositorySearch, config.Get().Info.Path) {
			// Wait until repository is available before using it
			for i := 0; i <= 30; i++ {
				if ip.TestRepositoryURL() == nil {
					break
				}

				time.Sleep(1 * time.Second)
			}

			log.Info("Updating Kodi add-on repositories... ")
			xbmcHost.UpdateAddonRepos()
			go repository.CheckBurst(xbmcHost, conf.SkipBurstSearch, config.AddonIcon())
		}

		xbmcHost.DialogProgressBGCleanup()
		xbmcHost.ResetRPC()
	}()

	go library.Init()
	go trakt.TokenRefreshHandler()
	go db.MaintenanceRefreshHandler()
	go cacheDB.MaintenanceRefreshHandler()
	go util.FreeMemoryGC()

	localAddress := fmt.Sprintf("%s:%d", config.Args.LocalHost, config.Args.LocalPort)
	log.Infof("Prepared in %s", time.Since(now))
	log.Infof("Starting HTTP server at %s", localAddress)

	exit.Server = &http.Server{
		Addr:    localAddress,
		Handler: nil,
	}

	if err = exit.Server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		exit.Panic(err)
		return
	}

	if !exit.IsShared {
		os.Exit(exit.Code)
	}
}

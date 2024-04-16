package library

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/anacrolix/missinggo/perf"
	"github.com/anacrolix/sync"
	"github.com/asdine/storm"
	"github.com/asdine/storm/q"
	"github.com/op/go-logging"

	"github.com/elgatito/elementum/cache"
	"github.com/elgatito/elementum/config"
	"github.com/elgatito/elementum/database"
	"github.com/elgatito/elementum/library/uid"
	"github.com/elgatito/elementum/tmdb"
	"github.com/elgatito/elementum/trakt"
	"github.com/elgatito/elementum/util"
	"github.com/elgatito/elementum/util/event"
	"github.com/elgatito/elementum/util/ip"
	"github.com/elgatito/elementum/xbmc"
)

const (
	movieType   = "movie"
	showType    = "show"
	episodeType = "episode"

	trueType  = "true"
	falseType = "false"
)

const (
	// MovieType ...
	MovieType = iota
	// ShowType ...
	ShowType
	// SeasonType ...
	SeasonType
	// EpisodeType ...
	EpisodeType
)

const (
	// StateDeleted ...
	StateDeleted = iota
	// StateActive ...
	StateActive
)

const (
	// ActionUpdate ...
	ActionUpdate = iota
	// ActionDelete ...
	ActionDelete
	// ActionSafeDelete ...
	ActionSafeDelete
)

const (
	// TVDBScraper ...
	TVDBScraper = iota
	// TMDBScraper ...
	TMDBScraper
	// TraktScraper ...
	TraktScraper
	// IMDBScraper ...
	IMDBScraper
)

const (
	// Active ...
	Active = iota
	// Deleted ...
	Deleted
)
const (
	// Delete ...
	Delete = iota
	// Update ...
	Update
	// Batch ...
	Batch
	// BatchDelete ...
	BatchDelete
	// DeleteTorrent ...
	DeleteTorrent
)

var (
	ItemTypes = []string{
		"Movie",
		"Show",
		"Season",
		"Episode",
	}
)

var (
	removedEpisodes chan *removedEpisode
	closer          = event.Event{}

	log = logging.MustGetLogger("library")

	cacheStore *cache.DBStore

	initialized = false

	resolveRegexp = regexp.MustCompile(`^plugin://plugin.video.elementum.*?(\d+)(\W|$)`)

	pendingShows = map[int]bool{}

	lock = sync.Mutex{}

	ErrVideoRemoved = errors.New("Video is marked as removed")
	ErrNoContainer  = errors.New("Container is not found")
)

// InitDB ...
func InitDB() {
	cacheStore = cache.NewDBStore()
}

// Init makes preparations on program start
func Init() {
	removedEpisodes = make(chan *removedEpisode)

	InitDB()

	xbmcHost, _ := xbmc.GetLocalXBMCHost()
	if xbmcHost == nil {
		return
	}

	if err := checkMoviesPath(); err != nil {
		if xbmcHost != nil {
			xbmcHost.Notify("Elementum", err.Error(), config.AddonIcon())
		}
		return
	}
	if err := checkShowsPath(); err != nil {
		if xbmcHost != nil {
			xbmcHost.Notify("Elementum", err.Error(), config.AddonIcon())
		}
		return
	}

	// Restoring UID library state
	uid.Get().Restore()

	go func() {
		// Give time to Kodi to start its JSON-RPC service
		time.Sleep(5 * time.Second)

		// After re-configure check Trakt authorization
		if config.Get().TraktToken != "" && !config.Get().TraktAuthorized {
			trakt.GetLastActivities()
		}

		RefreshLocal()
		Refresh()
		initialized = true
	}()

	// Removed episodes debouncer
	go func() {
		var episodes []*removedEpisode

		closing := closer.C()
		timer := time.NewTicker(3 * time.Second)
		defer timer.Stop()
		defer close(removedEpisodes)

		for {
			select {
			case <-closing:
				return

			case <-timer.C:
				if len(episodes) == 0 {
					break
				}

				shows := make(map[string][]*removedEpisode, 0)
				for _, episode := range episodes {
					shows[episode.ShowName] = append(shows[episode.ShowName], episode)
				}

				var label string
				var labels []string
				if len(episodes) > 1 {
					for showName, showEpisodes := range shows {
						var libraryTotal int
						if !uid.HasShows() {
							break
						}
						show, err := uid.GetShowByTMDB(showEpisodes[0].ShowID)
						if show != nil && err == nil {
							libraryTotal = len(show.Episodes)
						}
						if libraryTotal == 0 {
							break
						}
						if len(showEpisodes) == libraryTotal {
							ID := strconv.Itoa(showEpisodes[0].ShowID)
							if _, _, err := RemoveShow(ID, false); err != nil {
								log.Error("Unable to remove show after removing all episodes...")
							}
						} else {
							labels = append(labels, fmt.Sprintf("%d episodes of %s", len(showEpisodes), showName))
						}

						// Add single episodes to removed prefix
						var tmdbIDs []int
						for _, showEpisode := range showEpisodes {
							tmdbIDs = append(tmdbIDs, showEpisode.ID)
						}
						if err := updateBatchDBItem(tmdbIDs, StateDeleted, EpisodeType, showEpisodes[0].ShowID); err != nil {
							log.Error(err)
						}
					}
					if len(labels) > 0 {
						label = strings.Join(labels, ", ")
						if xbmcHost != nil && xbmcHost.DialogConfirmFocused("Elementum", fmt.Sprintf("LOCALIZE[30278];;%s", label)) {
							xbmcHost.VideoLibraryClean()
						}
					}
				} else {
					for showName, episode := range shows {
						label = fmt.Sprintf("%s S%02dE%02d", showName, episode[0].Season, episode[0].Episode)
						if err := updateDBItem(episode[0].ID, StateDeleted, EpisodeType, episode[0].ShowID); err != nil {
							log.Error(err)
						}
					}
					if xbmcHost != nil && xbmcHost.DialogConfirmFocused("Elementum", fmt.Sprintf("LOCALIZE[30278];;%s", label)) {
						xbmcHost.VideoLibraryClean()
					}
				}

				episodes = make([]*removedEpisode, 0)

			case episode, ok := <-removedEpisodes:
				if !ok {
					break
				}
				episodes = append(episodes, episode)
			}
		}
	}()

	updateDelay := config.Get().UpdateDelay
	if updateDelay > 0 {
		if updateDelay < 5 {
			// Give time to Elementum to update its cache of libraryMovies, libraryShows and libraryEpisodes
			updateDelay = 5
		}
		go func() {
			time.Sleep(time.Duration(updateDelay) * time.Second)
			closing := closer.C()

			select {
			case <-closing:
				return
			default:
				PlanTraktUpdate()
				PlanKodiShowsUpdate()
			}
		}()
	}

	updateFrequency := util.Max(1, config.Get().UpdateFrequency)
	traktFrequency := util.Max(1, config.Get().TraktSyncFrequencyMin)

	updateTicker := time.NewTicker(time.Duration(updateFrequency) * time.Hour)
	traktSyncTicker := time.NewTicker(time.Duration(traktFrequency) * time.Minute)
	markedForRemovalTicker := time.NewTicker(30 * time.Second)
	watcherTicker := time.NewTicker(1 * time.Second)

	defer updateTicker.Stop()
	defer traktSyncTicker.Stop()
	defer markedForRemovalTicker.Stop()
	defer watcherTicker.Stop()

	closing := closer.C()

	l := uid.Get()
	for {
		select {
		case <-watcherTicker.C:
			if !initialized || l.Running.IsOverall || l.Running.IsMovies || l.Running.IsShows || l.Running.IsEpisodes || l.Running.IsKodi || l.Running.IsTrakt || l.Running.IsKodiMovies || l.Running.IsKodiShows {
				continue
			} else if l.Pending.IsKodi {
				go RefreshKodi()
			} else if l.Pending.IsTrakt {
				go RefreshTrakt()
			} else if l.Pending.IsMovies {
				go RefreshMovies()
			} else if l.Pending.IsShows {
				go RefreshShows()
			} else if l.Pending.IsEpisodes {
				go RefreshEpisodes()
			} else if l.Pending.IsKodiShows {
				go updateLibraryShows(xbmcHost)
			} else if l.Pending.IsOverall {
				go Refresh()
			}
		case <-updateTicker.C:
			if config.Get().UpdateFrequency > 0 && config.Get().LibraryEnabled && config.Get().LibrarySyncEnabled && config.Get().LibrarySyncPlaybackEnabled {
				PlanKodiShowsUpdate()
			}
		case <-traktSyncTicker.C:
			PlanTraktUpdate()
		case <-markedForRemovalTicker.C:
			var items []database.BTItem
			database.GetStormDB().Select(q.Eq("State", database.StateDeleted)).Find(&items)

			for _, item := range items {
				// Remove from Elementum's library to prevent duplicates
				if item.Type == movieType {
					if uid.IsDuplicateMovie(strconv.Itoa(item.ID)) {
						if _, _, err := RemoveMovie(item.ID, false); err != nil {
							log.Warning("Nothing left to remove from Elementum")
						}
					}
				} else {
					if uid.IsDuplicateEpisode(item.ShowID, item.Season, item.Episode) {
						if err := RemoveEpisode(item.ID, item.ShowID, item.Season, item.Episode); err != nil {
							log.Warning(err)
						}
					}
				}

				database.GetStormDB().DeleteStruct(&item)
				log.Infof("Removed %s from database", item.InfoHash)
			}

		case <-closing:
			return
		}
	}
}

// MoviesLibraryPath contains calculated path for saving Movies strm files
func MoviesLibraryPath() string {
	return filepath.Join(config.Get().LibraryPath, "Movies")
}

// ShowsLibraryPath contains calculated path for saving Shows strm files
func ShowsLibraryPath() string {
	return filepath.Join(config.Get().LibraryPath, "Shows")
}

// Library updates
func updateLibraryShows(xbmcHost *xbmc.XBMCHost) error {
	if !config.Get().LibraryEnabled || !config.Get().LibrarySyncEnabled || (!config.Get().LibrarySyncPlaybackEnabled && xbmcHost != nil && xbmcHost.PlayerIsPlaying()) {
		return nil
	}

	l := uid.Get()
	if l.Running.IsKodiShows || l.Running.IsKodi {
		return nil
	}

	l.Pending.IsKodiShows = false
	l.Running.IsKodiShows = true

	defer func() {
		l.Running.IsKodiShows = false
	}()

	if err := checkShowsPath(); err != nil {
		return err
	}

	begin := time.Now()

	var lis []database.LibraryItem
	if err := database.GetStormDB().Select(q.Eq("MediaType", ShowType), q.Eq("State", StateActive)).Find(&lis); err != nil && err != storm.ErrNotFound {
		log.Infof("Could not get list of library items: %s", err)
	}

	for _, i := range lis {
		if closer.IsSet() {
			return nil
		}
		if i.ID == 0 || i.ShowID == 0 {
			continue
		}

		if _, err := writeShowStrm(i.ShowID, false, false); err != nil {
			log.Errorf("Error updating show: %s", err)
		}
	}

	log.Infof("Library updated in %s", time.Since(begin))
	PlanKodiUpdate()
	return nil
}

// Path checks
func checkLibraryPath() error {
	libraryPath := config.Get().LibraryPath
	if libraryPath == "" || libraryPath == "." {
		log.Warningf("Library path is not initialized")
		return errors.New("LOCALIZE[30220]")
	}
	if fileInfo, err := os.Stat(libraryPath); err != nil {
		if fileInfo == nil {
			log.Warningf("Library path is invalid")
			return errors.New("Invalid library path")
		}
		if !fileInfo.IsDir() {
			log.Warningf("Library path is not a directory")
			return errors.New("Library path is not a directory")
		}

		log.Warningf("Error getting Library path: %v", err)
		return err
	}
	return nil
}

func checkMoviesPath() error {
	if err := checkLibraryPath(); err != nil {
		return err
	}

	moviesLibraryPath := MoviesLibraryPath()
	if _, err := os.Stat(moviesLibraryPath); os.IsNotExist(err) {
		if err := os.Mkdir(moviesLibraryPath, 0755); err != nil {
			log.Error(err)
			return err
		}
	}
	return nil
}

func checkShowsPath() error {
	if err := checkLibraryPath(); err != nil {
		return err
	}

	showsLibraryPath := ShowsLibraryPath()
	if _, err := os.Stat(showsLibraryPath); os.IsNotExist(err) {
		if err := os.Mkdir(showsLibraryPath, 0755); err != nil {
			log.Error(err)
			return err
		}
	}
	return nil
}

//
// Writers
//

func writeMovieStrm(tmdbID string, force bool) (*tmdb.Movie, error) {
	defer perf.ScopeTimer()()

	// We should not write strm files for movies that are marked as deleted
	ID, _ := strconv.Atoi(tmdbID)
	if wasRemoved(ID, MovieType) && !force {
		return nil, ErrVideoRemoved
	}

	movie := tmdb.GetMovieByID(tmdbID, config.GetStrmLanguage())
	if movie == nil {
		return nil, errors.New("Can't find the movie")
	}

	moviePath, movieStrm := getMoviePath(movie)

	if _, err := os.Stat(moviePath); os.IsNotExist(err) {
		if err := os.Mkdir(moviePath, 0755); err != nil {
			log.Error(err)
			return movie, err
		}
	} else if force {
		os.Chtimes(moviePath, time.Now().Local(), time.Now().Local())
	}

	movieStrmPath := filepath.Join(moviePath, fmt.Sprintf("%s.strm", movieStrm))
	if config.Get().LibraryNFOMovies {
		writeMovieNFO(movie, filepath.Join(moviePath, fmt.Sprintf("%s.nfo", movieStrm)))
	}

	playLink := URLForXBMC("/library/movie/play/%s", tmdbID)
	if _, err := os.Stat(movieStrmPath); !force && err == nil {
		// log.Debugf("Movie strm file already exists at %s", movieStrmPath)
		// return movie, fmt.Errorf("LOCALIZE[30287];;%s", movie.Title)
		return movie, nil
	}
	if err := os.WriteFile(movieStrmPath, []byte(playLink), 0644); err != nil {
		log.Errorf("Could not write strm file: %s", err)
		return movie, err
	}

	return movie, nil
}

func writeMovieNFO(m *tmdb.Movie, p string) error {
	out := `<?xml version="1.0" encoding="UTF-8" standalone="yes" ?>
<movie>
	<uniqueid type="unknown" default="false">%v</uniqueid>
	<uniqueid type="elementum" default="false">%v</uniqueid>
	<uniqueid type="tmdb" default="true">%v</uniqueid>
	<uniqueid type="imdb" default="false">%v</uniqueid>
	<uniqueid type="tvdb" default="false">%v</uniqueid>
</movie>
https://www.themoviedb.org/movie/%v
`

	if m.ExternalIDs == nil {
		m.ExternalIDs = &tmdb.ExternalIDs{}
	}

	out = fmt.Sprintf(out,
		m.ID,
		m.ID,
		m.ID,
		m.ExternalIDs.IMDBId,
		m.ExternalIDs.TVDBID,
		m.ID,
	)

	if m.ExternalIDs.IMDBId != "" {
		out += fmt.Sprintf("https://www.imdb.com/title/%s/\n", m.ExternalIDs.IMDBId)
	}

	if err := os.WriteFile(p, []byte(out), 0644); err != nil {
		log.Errorf("Could not write NFO file: %s", err)
		return err
	}

	return nil
}

func writeShowStrm(showID int, adding, force bool) (*tmdb.Show, error) {
	defer perf.ScopeTimer()()

	// We should not write strm files for shows that are marked as deleted
	if wasRemoved(showID, ShowType) && !force {
		return nil, ErrVideoRemoved
	}

	show := tmdb.GetShow(showID, config.GetStrmLanguage())
	if show == nil {
		return nil, fmt.Errorf("Unable to get show (%d)", showID)
	}

	showPath, showStrm := getShowPath(show)

	if _, err := os.Stat(showPath); os.IsNotExist(err) {
		if err := os.Mkdir(showPath, 0755); err != nil {
			log.Error(err)
			return show, err
		}
	} else if force {
		os.Chtimes(showPath, time.Now().Local(), time.Now().Local())
	}

	if config.Get().LibraryNFOShows {
		nfoPath := filepath.Join(showPath, "tvshow.nfo")
		if _, err := os.Stat(nfoPath); os.IsNotExist(err) || force {
			writeShowNFO(show, nfoPath)
		}
	}

	addSpecials := config.Get().AddSpecials

	for _, season := range show.Seasons {
		if season.EpisodeCount == 0 {
			continue
		}
		if !config.Get().ShowUnairedSeasons {
			if _, isAired := util.AirDateWithAiredCheck(season.AirDate, time.DateOnly, config.Get().ShowEpisodesOnReleaseDay); !isAired {
				continue
			}
		}
		if !addSpecials && season.Season == 0 {
			continue
		}

		seasonTMDB := tmdb.GetSeason(showID, season.Season, config.Get().Language, len(show.Seasons), false)
		if seasonTMDB == nil {
			continue
		}
		episodes := seasonTMDB.Episodes

		var reAddIDs []int
		for _, episode := range episodes {
			if episode == nil {
				continue
			}

			if !config.Get().ShowUnairedEpisodes {
				if episode.AirDate == "" {
					continue
				}
				if _, isAired := util.AirDateWithAiredCheck(episode.AirDate, time.DateOnly, config.Get().ShowEpisodesOnReleaseDay); !isAired {
					continue
				}
			}

			if adding {
				reAddIDs = append(reAddIDs, episode.ID)
			}

			if !force && uid.IsDuplicateEpisode(showID, season.Season, episode.EpisodeNumber) {
				continue
			}

			episodeStrmPath := filepath.Join(showPath, fmt.Sprintf("%s S%02dE%02d.strm", showStrm, season.Season, episode.EpisodeNumber))
			playLink := URLForXBMC("/library/show/play/%d/%d/%d", showID, season.Season, episode.EpisodeNumber)
			if _, err := os.Stat(episodeStrmPath); !force && err == nil {
				continue
			}

			if err := os.WriteFile(episodeStrmPath, []byte(playLink), 0644); err != nil {
				log.Error(err)
				return show, err
			}
		}
		if len(reAddIDs) > 0 {
			if err := updateBatchDBItem(reAddIDs, StateActive, EpisodeType, showID); err != nil {
				log.Error(err)
			}
		}
	}

	return show, nil
}

func writeShowNFO(s *tmdb.Show, p string) error {
	out := `<?xml version="1.0" encoding="UTF-8" standalone="yes" ?>
<tvshow>
	<uniqueid type="unknown" default="false">%v</uniqueid>
	<uniqueid type="elementum" default="false">%v</uniqueid>
	<uniqueid type="tmdb" default="true">%v</uniqueid>
	<uniqueid type="imdb" default="false">%v</uniqueid>
	<uniqueid type="tvdb" default="false">%v</uniqueid>
</tvshow>
https://www.themoviedb.org/tv/%v
`

	if s.ExternalIDs == nil {
		s.ExternalIDs = &tmdb.ExternalIDs{}
	}

	out = fmt.Sprintf(out,
		s.ID,
		s.ID,
		s.ID,
		s.ExternalIDs.IMDBId,
		s.ExternalIDs.TVDBID,
		s.ID,
	)

	if s.ExternalIDs.IMDBId != "" {
		out += fmt.Sprintf("https://www.imdb.com/title/%v/\n", s.ExternalIDs.IMDBId)
	}
	if s.ExternalIDs.TVDBID != "" {
		out += fmt.Sprintf("https://www.thetvdb.com/?tab=series&id=%v&lid=7\n", s.ExternalIDs.TVDBID)
	}

	if err := os.WriteFile(p, []byte(out), 0644); err != nil {
		log.Errorf("Could not write NFO file: %s", err)
		return err
	}

	return nil
}

//
// Removers
//

// RemoveMovie removes movie from the library
func RemoveMovie(tmdbID int, purge bool) (*tmdb.Movie, []string, error) {
	if err := checkMoviesPath(); err != nil {
		return nil, nil, err
	}
	defer func() {
		deleteDBItem(tmdbID, MovieType, true, purge)
	}()

	ID := strconv.Itoa(tmdbID)
	movie := tmdb.GetMovieByID(ID, config.GetStrmLanguage())
	if movie == nil {
		return nil, nil, errors.New("Can't resolve movie")
	}

	paths := getMoviePaths(movie)

	if len(paths) == 0 {
		log.Warningf("Cannot find directories with strm files")
		return movie, nil, errors.New("LOCALIZE[30282]")
	}
	ret := []string{}
	for path := range paths {
		if err := os.RemoveAll(path); err != nil {
			log.Error(err)
			return movie, nil, err
		}

		ret = append(ret, path)
		log.Warningf("Directory %s removed from disk", path)
	}

	log.Warningf("%s removed from library", movie.GetTitle())
	return movie, ret, nil
}

// RemoveShow removes show from the library
func RemoveShow(tmdbID string, purge bool) (*tmdb.Show, []string, error) {
	if err := checkShowsPath(); err != nil {
		return nil, nil, err
	}
	ID, _ := strconv.Atoi(tmdbID)
	defer func() {
		deleteDBItem(ID, ShowType, true, purge)
	}()

	show := tmdb.GetShow(ID, config.GetStrmLanguage())

	if show == nil {
		return nil, nil, errors.New("Unable to find show to remove")
	}

	paths := getShowPaths(show)

	if len(paths) == 0 {
		log.Warningf("Cannot find directories with strm files")
		return show, nil, errors.New("LOCALIZE[30282]")
	}
	ret := []string{}
	for path := range paths {
		if err := os.RemoveAll(path); err != nil {
			log.Error(err)
			return show, nil, err
		}

		ret = append(ret, path)
		log.Warningf("Directory %s removed from disk", path)
	}

	log.Warningf("%s removed from library", show.GetName())

	return show, ret, nil
}

// RemoveEpisode removes episode from the library
func RemoveEpisode(tmdbID int, showID int, seasonNumber int, episodeNumber int) error {
	if err := checkShowsPath(); err != nil {
		return err
	}
	show := tmdb.GetShow(showID, config.GetStrmLanguage())

	if show == nil {
		return errors.New("Unable to find show to remove episode")
	}
	if removedEpisodes == nil {
		return errors.New("Channel is closed")
	}

	showPath, _ := getShowPath(show)
	episodeStrm := fmt.Sprintf("%s S%02dE%02d.strm", showPath, seasonNumber, episodeNumber)
	episodePath := filepath.Join(ShowsLibraryPath(), showPath, episodeStrm)

	alreadyRemoved := false
	if _, err := os.Stat(episodePath); err != nil {
		alreadyRemoved = true
	}
	if !alreadyRemoved {
		if err := os.Remove(episodePath); err != nil {
			return err
		}
	}

	removedEpisodes <- &removedEpisode{
		ID:       tmdbID,
		ShowID:   showID,
		ShowName: show.GetName(),
		Season:   seasonNumber,
		Episode:  episodeNumber,
	}

	if !alreadyRemoved {
		log.Noticef("%s removed from library", episodeStrm)
	} else {
		return errors.New("Nothing left to remove from Elementum")
	}

	return nil
}

//
// Database updates
//

func updateDBItem(tmdbID int, state int, mediaType int, showID int) error {
	if tmdbID <= 0 {
		return fmt.Errorf("Cannot update DBItem due to missing TMDB ID")
	}

	defer perf.ScopeTimer()()

	li := database.LibraryItem{
		ID:        tmdbID,
		MediaType: mediaType,
		ShowID:    showID,
		State:     state,
	}
	if err := database.GetStormDB().Save(&li); err != nil {
		log.Debugf("updateDBItem failed: %s", err)
		return err
	}
	return nil
}

func updateBatchDBItem(tmdbIds []int, state int, mediaType int, showID int) error {
	defer perf.ScopeTimer()()

	tx, err := database.GetStormDB().Begin(true)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	for _, id := range tmdbIds {
		li := database.LibraryItem{
			ID:        id,
			MediaType: mediaType,
			ShowID:    showID,
			State:     state,
		}
		err = tx.Save(&li)
		if err != nil {
			return err
		}
	}

	return tx.Commit()
}

func deleteDBItem(tmdbID int, mediaType int, removal bool, purge bool) error {
	defer perf.ScopeTimer()()

	var li database.LibraryItem
	if err := database.GetStormDB().One("ID", tmdbID, &li); err != nil {
		log.Debugf("Cannot find deleted item: %s", err)
		return err
	}

	if removal {
		li.State = StateDeleted
	} else {
		li.State = StateActive
	}

	if !purge {
		if err := database.GetStormDB().Save(&li); err != nil {
			log.Debugf("Cannot update deleted item: %s", err)
			return err
		}
	} else {
		if err := database.GetStormDB().DeleteStruct(&li); err != nil {
			log.Debugf("Cannot purge deleted item: %s", err)
			return err
		}
	}

	return nil
}

// func deleteBatchDBItem(tmdbIds []int, mediaType int) error {
// 	// tx, err := database.Get().Begin()
// 	// if err != nil {
// 	// 	log.Debugf("Cannot start transaction: %s", err)
// 	// 	return err
// 	// }
// 	// for _, id := range tmdbIds {
// 	// 	_, err := tx.Exec(`UPDATE library_items SET state = ? WHERE tmdbId = ? AND mediaType = ?`, StateDeleted, id, mediaType)
// 	// 	if err != nil {
// 	// 		log.Debugf("deleteDBItem failed: %s", err)
// 	// 		tx.Rollback()
// 	// 		return err
// 	// 	}
// 	// }
// 	// tx.Commit()

// 	return nil
// }

func wasRemoved(id int, mediaType int) (wasRemoved bool) {
	defer perf.ScopeTimer()()

	var li database.LibraryItem
	if err := database.GetStormDB().Select(q.Eq("ID", id), q.Eq("MediaType", mediaType), q.Eq("State", StateDeleted)).First(&li); err == nil && li.ID != 0 {
		log.Debugf("mediaType=%s id=%d marked as removed in database", ItemTypes[mediaType], id)
		return true
	}

	return false
}

// IsInLibrary checks for LibraryItem in Elementum database
func IsInLibrary(id int, mediaType int) (res bool) {
	defer perf.ScopeTimer()()

	var li database.LibraryItem
	if err := database.GetStormDB().Select(q.Eq("ID", id), q.Eq("MediaType", mediaType), q.Eq("State", StateActive)).First(&li); err == nil && li.ID != 0 {
		return true
	}

	return false
}

//
// Maintenance
//

// CloseLibrary ...
func CloseLibrary() {
	log.Info("Closing library...")
	closer.Set()
}

// ClearPageCache deletes cached page listings
func ClearPageCache(xbmcHost *xbmc.XBMCHost) {
	cacheDB := database.GetCache()
	if cacheDB != nil {
		cacheDB.DeleteWithPrefix(database.CommonBucket, []byte("page."))
	}
	xbmcHost.Refresh()
}

// ClearResolveCache deletes cached IDs resolve
func ClearResolveCache() {
	cacheDB := database.GetCache()
	if cacheDB != nil {
		cacheDB.DeleteWithPrefix(database.CommonBucket, []byte("Resolve_"))
	}
}

// ClearCacheKey deletes specific key
func ClearCacheKey(key string) {
	cacheDB := database.GetCache()
	if cacheDB != nil {
		log.Debugf("Removing cache key: %s", key)
		if err := cacheDB.Delete(database.CommonBucket, key); err != nil {
			log.Debugf("Error removing key from cache: %#v", err)
		}
	}
}

// ClearTraktCache deletes cached trakt data
func ClearTraktCache(xbmcHost *xbmc.XBMCHost) {
	cacheDB := database.GetCache()
	if cacheDB != nil {
		cacheDB.DeleteWithPrefix(database.CommonBucket, []byte(cache.TraktKey))
	}
	xbmcHost.Refresh()
}

// ClearTmdbCache deletes cached tmdb data
func ClearTmdbCache(xbmcHost *xbmc.XBMCHost) {
	cacheDB := database.GetCache()
	if cacheDB != nil {
		cacheDB.DeleteWithPrefix(database.CommonBucket, []byte(cache.TMDBKey))
	}
	xbmcHost.Refresh()
}

//
// Utilities
// 		mainly copied from api/routes to skip cycle imports

// URLForHTTP ...
func URLForHTTP(pattern string, args ...interface{}) string {
	u, _ := url.Parse(fmt.Sprintf(pattern, args...))
	return ip.GetHTTPHost() + u.String()
}

// URLForXBMC ...
func URLForXBMC(pattern string, args ...interface{}) string {
	u, _ := url.Parse(fmt.Sprintf(pattern, args...))
	return "plugin://" + config.Get().Info.ID + u.String()
}

// URLQuery ...
func URLQuery(route string, query ...string) string {
	v := url.Values{}
	for i := 0; i < len(query); i += 2 {
		v.Add(query[i], query[i+1])
	}
	return route + "?" + v.Encode()
}

//
// Movie internals
//

// SyncMoviesList updates trakt movie collections in cache
func SyncMoviesList(listID string, updating bool, isUpdateNeeded bool) (err error) {
	started := time.Now()
	defer func() {
		log.Debugf("Trakt sync movies %s finished in %s", listID, time.Since(started))
	}()

	var addEnabled bool
	var removeEnabled bool

	var label string
	var addedMovies []*trakt.Movies
	var removedMovies []*trakt.Movies
	var previous []*trakt.Movies
	var current []*trakt.Movies

	switch listID {
	case "watchlist":
		previous, _ = trakt.PreviousWatchlistMovies()
		current, _ = trakt.WatchlistMovies(isUpdateNeeded)

		label = "LOCALIZE[30254]"

		addEnabled = config.Get().TraktSyncWatchlist
		removeEnabled = config.Get().TraktSyncRemovedMoviesBack

		MoviesToUIDLocked(uid.WatchlistedMoviesContainer, current)
	case "collection":
		previous, _ = trakt.PreviousCollectionMovies()
		current, _ = trakt.CollectionMovies(isUpdateNeeded)

		label = "LOCALIZE[30257]"

		addEnabled = config.Get().TraktSyncCollections
		removeEnabled = config.Get().TraktSyncRemovedMoviesBack

		MoviesToUIDLocked(uid.CollectedMoviesContainer, current)
	default:
		previous, _ = trakt.PreviousListItemsMovies(listID)
		current, _ = trakt.ListItemsMovies(config.Get().TraktUsername, listID)

		label = "LOCALIZE[30263]"

		addEnabled = config.Get().TraktSyncUserlists
		removeEnabled = config.Get().TraktSyncRemovedMoviesBack

		MoviesToUIDLocked(uid.UserlistedMoviesContainer, current)
	}

	if err = checkMoviesPath(); err != nil {
		return
	}

	// For first run we will try to write all movies, not only the delta
	if !IsTraktInitialized {
		addedMovies = current
	} else {
		addedMovies = DiffTraktMovies(previous, current, IsTraktInitialized)
		removedMovies = DiffTraktMovies(current, previous, IsTraktInitialized)
	}

	// Syncing movies added to Trakt source
	if addEnabled && len(addedMovies) > 0 {
		if err = SyncMoviesListAdded(addedMovies, updating, isUpdateNeeded, label, listID); err != nil {
			log.Warningf("Could not sync added movies: %s", err)
		}
	}

	// Sync back removed Movies, meaning removing them from Kodi library.
	if removeEnabled && len(removedMovies) > 0 {
		if err := syncMoviesRemovedBack(removedMovies); err != nil {
			log.Warningf("Could not sync back removed movies: %s", err)
		}
		log.Infof("Movies list (%s) removed %d items", listID, len(removedMovies))
	}

	return nil
}

// SyncMoviesListAdded updates added movies
func SyncMoviesListAdded(movies []*trakt.Movies, updating, isUpdateNeeded bool, label, listID string) (err error) {
	var movieIDs []int
	for _, movie := range movies {
		if movie == nil || movie.Movie == nil || movie.Movie.IDs == nil {
			continue
		}

		title := movie.Movie.Title
		// Try to resolve TMDB id through IMDB id, if provided
		if movie.Movie.IDs.TMDB == 0 && len(movie.Movie.IDs.IMDB) > 0 {
			r := tmdb.Find(movie.Movie.IDs.IMDB, "imdb_id")
			if r != nil && len(r.MovieResults) > 0 {
				movie.Movie.IDs.TMDB = r.MovieResults[0].ID
			}
		}

		if movie.Movie.IDs.TMDB == 0 {
			log.Warningf("Missing TMDB ID for %s", title)
			continue
		}

		tmdbID := strconv.Itoa(movie.Movie.IDs.TMDB)

		if uid.IsDuplicateMovie(tmdbID) {
			continue
		}

		if _, err := writeMovieStrm(tmdbID, false); err != nil {
			continue
		}

		movieIDs = append(movieIDs, movie.Movie.IDs.TMDB)
	}

	if err := updateBatchDBItem(movieIDs, StateActive, MovieType, 0); err != nil {
		return err
	}

	if !updating && len(movieIDs) > 0 {
		log.Infof("Movies list (%s) added %d items", listID, len(movies))
		xbmcHost, _ := xbmc.GetLocalXBMCHost()
		if xbmcHost != nil {
			if config.Get().LibraryUpdate == 0 || (config.Get().LibraryUpdate == 1 && xbmcHost.DialogConfirmFocused("Elementum", fmt.Sprintf("LOCALIZE[30277];;%s", label))) {
				xbmcHost.VideoLibraryScan()
			}
		}
	}

	return nil
}

//
// Shows internals
//

// SyncShowsList updates trakt collections in cache
func SyncShowsList(listID string, updating bool, isUpdateNeeded bool) (err error) {
	started := time.Now()
	defer func() {
		log.Debugf("Trakt sync shows %s finished in %s", listID, time.Since(started))
	}()

	var addEnabled bool
	var removeEnabled bool

	var label string
	var addedShows []*trakt.Shows
	var removedShows []*trakt.Shows
	var previous []*trakt.Shows
	var current []*trakt.Shows

	switch listID {
	case "watchlist":
		previous, _ = trakt.PreviousWatchlistShows()
		current, _ = trakt.WatchlistShows(isUpdateNeeded)

		label = "LOCALIZE[30254]"

		addEnabled = config.Get().TraktSyncWatchlist
		removeEnabled = config.Get().TraktSyncRemovedShowsBack

		ShowsToUIDLocked(uid.WatchlistedShowsContainer, current)
	case "collection":
		previous, _ = trakt.PreviousCollectionShows()
		current, _ = trakt.CollectionShows(isUpdateNeeded)

		label = "LOCALIZE[30257]"

		addEnabled = config.Get().TraktSyncCollections
		removeEnabled = config.Get().TraktSyncRemovedShowsBack

		ShowsToUIDLocked(uid.CollectedShowsContainer, current)
	default:
		previous, _ = trakt.PreviousListItemsShows(listID)
		current, _ = trakt.ListItemsShows(config.Get().TraktUsername, listID)

		label = "LOCALIZE[30263]"

		addEnabled = config.Get().TraktSyncUserlists
		removeEnabled = config.Get().TraktSyncRemovedShowsBack

		ShowsToUIDLocked(uid.UserlistedShowsContainer, current)
	}

	if err = checkShowsPath(); err != nil {
		return err
	}

	// For first run we will try to write all shows, not only the delta
	if !IsTraktInitialized {
		addedShows = current
	} else {
		addedShows = DiffTraktShows(previous, current, IsTraktInitialized)
		removedShows = DiffTraktShows(current, previous, IsTraktInitialized)
	}

	// Syncing shows added to Trakt source
	if addEnabled && len(addedShows) > 0 {
		if err = SyncShowsListAdded(addedShows, updating, isUpdateNeeded, label, listID); err != nil {
			log.Warningf("Could not sync added shows: %s", err)
		}
	}

	// Sync back removed Shows, meaning removing them from Kodi library.
	// Ignoring watchlist items, because Trakt is automatically removing whole show from Watchlist if any episode is watched.
	if removeEnabled && len(removedShows) > 0 && listID != "watchlist" {
		if err := syncShowsRemovedBack(removedShows); err != nil {
			log.Warningf("Could not sync back removed shows: %s", err)
		}
		log.Infof("Shows list (%s) removed %d items", listID, len(removedShows))
	}

	return nil
}

// SyncShowsListAdded updates added shows
func SyncShowsListAdded(shows []*trakt.Shows, updating, isUpdateNeeded bool, label, listID string) (err error) {
	cacheStore := cache.NewDBStore()
	showsLastUpdates := map[int]time.Time{}

	// Keep tracking of processed shows to avoid re-writing and checking all of them again.
	cacheStore.Get(cache.LibraryShowsLastUpdatesKey, &showsLastUpdates)
	defer func() {
		cacheStore.Set(cache.LibraryShowsLastUpdatesKey, &showsLastUpdates, cache.LibraryShowsLastUpdatesExpire)
	}()

	var showIDs []int
	for _, show := range shows {
		if show == nil || show.Show == nil || show.Show.IDs == nil {
			continue
		}

		title := show.Show.Title
		// Try to resolve TMDB id through IMDB id, if provided
		if show.Show.IDs.TMDB == 0 {
			if len(show.Show.IDs.IMDB) > 0 {
				r := tmdb.Find(show.Show.IDs.IMDB, "imdb_id")
				if r != nil && len(r.TVResults) > 0 {
					show.Show.IDs.TMDB = r.TVResults[0].ID
				}
			}
			if show.Show.IDs.TMDB == 0 && show.Show.IDs.TVDB != 0 {
				r := tmdb.Find(strconv.Itoa(show.Show.IDs.TVDB), "tvdb_id")
				if r != nil && len(r.TVResults) > 0 {
					show.Show.IDs.TMDB = r.TVResults[0].ID
				}
			}
		}

		if show.Show.IDs.TMDB == 0 {
			log.Warningf("Missing TMDB ID for %s", title)
			continue
		}

		tmdbID := strconv.Itoa(show.Show.IDs.TMDB)
		if t, ok := showsLastUpdates[show.Show.IDs.Trakt]; ok && uid.IsDuplicateShow(tmdbID) && t.After(show.Show.UpdatedAt) {
			continue
		}
		showsLastUpdates[show.Show.IDs.Trakt] = show.Show.UpdatedAt

		if !updating && !isUpdateNeeded && uid.IsDuplicateShow(tmdbID) {
			continue
		}

		if _, err := writeShowStrm(show.Show.IDs.TMDB, false, false); err != nil {
			continue
		}

		showIDs = append(showIDs, show.Show.IDs.TMDB)
	}

	// Cleanup unused map items
	found := false
	for k := range showsLastUpdates {
		found = false
		for _, s := range shows {
			if s == nil || s.Show == nil || s.Show.IDs == nil {
				continue
			}

			if s.Show.IDs.Trakt == k {
				found = true
				break
			}
		}

		if !found {
			delete(showsLastUpdates, k)
		}
	}

	if err := updateBatchDBItem(showIDs, StateActive, ShowType, 0); err != nil {
		return err
	}

	if !updating && len(showIDs) > 0 {
		log.Infof("Shows list (%s) added %d items", listID, len(showIDs))
		xbmcHost, _ := xbmc.GetLocalXBMCHost()
		if xbmcHost != nil {
			if config.Get().LibraryUpdate == 0 || (config.Get().LibraryUpdate == 1 && xbmcHost.DialogConfirmFocused("Elementum", fmt.Sprintf("LOCALIZE[30277];;%s", label))) {
				xbmcHost.VideoLibraryScan()
			}
		}
	}

	return nil
}

// DiffTraktMovies ...
func DiffTraktMovies(previous, current []*trakt.Movies, isInitialized bool) []*trakt.Movies {
	ret := make([]*trakt.Movies, 0, len(current))
	found := false
	for _, ce := range current {
		if ce == nil || ce.Movie == nil || ce.Movie.IDs == nil {
			continue
		}

		found = false
		for _, pr := range previous {
			if pr == nil || pr.Movie == nil || pr.Movie.IDs == nil {
				continue
			}

			if pr.Movie.IDs.Trakt == ce.Movie.IDs.Trakt {
				found = true
				break
			}
		}

		// If Trakt is not yet initialized - we can leave shows that are not yet in the library
		if !found || (!isInitialized && !uid.IsDuplicateMovieByInt(ce.Movie.IDs.TMDB)) {
			ret = append(ret, ce)
		}
	}

	return ret
}

// DiffTraktShows ...
func DiffTraktShows(previous, current []*trakt.Shows, isInitialized bool) []*trakt.Shows {
	ret := make([]*trakt.Shows, 0, len(current))
	found := false
	for _, ce := range current {
		if ce == nil || ce.Show == nil || ce.Show.IDs == nil {
			continue
		}

		found = false
		for _, pr := range previous {
			if pr == nil || pr.Show == nil || pr.Show.IDs == nil {
				continue
			}

			if pr.Show.IDs.Trakt == ce.Show.IDs.Trakt {
				found = true
				break
			}
		}

		// If Trakt is not yet initialized - we can leave shows that are not yet in the library
		if !found || (!isInitialized && !uid.IsDuplicateShowByInt(ce.Show.IDs.TMDB)) {
			ret = append(ret, ce)
		}
	}

	return ret
}

//
// External handlers
//

// AddMovie is adding movie to the library
func AddMovie(tmdbID string, force bool) (*tmdb.Movie, error) {
	if err := checkMoviesPath(); err != nil {
		return nil, err
	}

	movie := tmdb.GetMovieByID(tmdbID, config.Get().Language)
	if movie == nil {
		return nil, fmt.Errorf("Movie with TMDB %s not found", tmdbID)
	}

	if !force && uid.IsDuplicateMovie(tmdbID) {
		if xbmcHost, err := xbmc.GetLocalXBMCHost(); xbmcHost != nil && err == nil {
			xbmcHost.Notify("Elementum", fmt.Sprintf("LOCALIZE[30287];;%s", movie.GetTitle()), config.AddonIcon())
		}
		return nil, fmt.Errorf("Movie already added")
	}

	if _, err := writeMovieStrm(tmdbID, force); err != nil {
		return movie, err
	}

	ID, _ := strconv.Atoi(tmdbID)
	if err := updateDBItem(ID, StateActive, MovieType, 0); err != nil {
		return movie, err
	}

	log.Noticef("%s added to library", movie.GetTitle())
	return movie, nil
}

// AddShow is adding show to the library
func AddShow(tmdbID string, force bool) (*tmdb.Show, error) {
	if err := checkShowsPath(); err != nil {
		return nil, err
	}

	ID, _ := strconv.Atoi(tmdbID)
	show := tmdb.GetShowByID(tmdbID, config.Get().Language)

	if !force && uid.IsDuplicateShow(tmdbID) {
		if xbmcHost, err := xbmc.GetLocalXBMCHost(); xbmcHost != nil && err == nil && show != nil {
			xbmcHost.Notify("Elementum", fmt.Sprintf("LOCALIZE[30287];;%s", show.GetName()), config.AddonIcon())
		}
		return show, fmt.Errorf("Show already added")
	}

	if err := updateDBItem(ID, StateActive, ShowType, ID); err != nil {
		return show, err
	}

	if _, err := writeShowStrm(ID, true, force); err != nil {
		log.Errorf("Error writing strm for a show: %s", err)
		return show, err
	}

	return show, nil
}

func getMoviePath(movie *tmdb.Movie) (moviePath, movieStrm string) {
	// If this movie already uses any directory - we should write there, to avoid having duplicates
	paths := getMoviePathsByTMDB(movie.ID)
	if len(paths) != 0 {
		for path := range paths {
			moviePath = path
			break
		}
	}

	movieName := movie.OriginalTitle
	if config.Get().StrmLanguage != "" && movie.Title != "" {
		movieName = movie.Title
	}

	movieStrm = util.ToFileName(fmt.Sprintf("%s (%s)", movieName, strings.Split(movie.ReleaseDate, "-")[0]))
	if moviePath == "" {
		moviePath = filepath.Join(MoviesLibraryPath(), movieStrm)
	} else {
		movieStrm = filepath.Base(strings.Replace(moviePath, "\\", "/", -1))
	}

	return
}

func getShowPath(show *tmdb.Show) (showPath, showStrm string) {
	// If this show already uses any directory - we should write there, to avoid having duplicates
	paths := getShowPathsByTMDB(show.ID)
	if len(paths) != 0 {
		for path := range paths {
			showPath = path
			break
		}
	}

	showName := show.OriginalName
	if config.Get().StrmLanguage != "" && show.Name != "" {
		showName = show.Name
	}

	showStrm = util.ToFileName(fmt.Sprintf("%s (%s)", showName, strings.Split(show.FirstAirDate, "-")[0]))
	if showPath == "" {
		showPath = filepath.Join(ShowsLibraryPath(), showStrm)
	} else {
		showStrm = filepath.Base(strings.Replace(showPath, "\\", "/", -1))
	}

	return
}

func getMoviePathsByTMDB(id int) (ret map[string]bool) {
	ret = map[string]bool{}

	xbmcHost, err := xbmc.GetLocalXBMCHost()
	if xbmcHost == nil {
		return
	}

	canResolveSpecialPath := true
	if xbmcHost == nil || err != nil {
		canResolveSpecialPath = false
	}

	if m, err := uid.GetMovieByTMDB(id); err == nil {
		if m != nil && m.File != "" && !util.IsNetworkPath(m.File) && strings.HasSuffix(m.File, ".strm") {
			filePath := m.File
			if strings.HasPrefix(filePath, "special:") && canResolveSpecialPath {
				filePath = xbmcHost.TranslatePath(filePath)
			}
			if util.IsValidPath(filePath) {
				ret[filepath.Dir(filePath)] = true
			}
		}
	}

	return
}

func getShowPathsByTMDB(id int) (ret map[string]bool) {
	ret = map[string]bool{}

	xbmcHost, err := xbmc.GetLocalXBMCHost()
	if xbmcHost == nil {
		return
	}

	canResolveSpecialPath := true
	if xbmcHost == nil || err != nil {
		canResolveSpecialPath = false
	}

	if s, err := uid.FindShowByTMDB(id); err == nil {
		for _, e := range s.Episodes {
			if e != nil && e.File != "" && !util.IsNetworkPath(e.File) && strings.HasSuffix(e.File, ".strm") {
				filePath := e.File
				if strings.HasPrefix(filePath, "special:") && canResolveSpecialPath {
					filePath = xbmcHost.TranslatePath(filePath)
				}
				if util.IsValidPath(filePath) {
					ret[filepath.Dir(filePath)] = true
				}
			}
		}
	}

	return
}

func getMoviePaths(movie *tmdb.Movie) map[string]bool {
	paths := getMoviePathsByTMDB(movie.ID)
	if len(paths) != 0 {
		return paths
	}

	titles := []string{movie.Title, movie.OriginalTitle}
	for _, t := range titles {
		movieStrm := util.ToFileName(fmt.Sprintf("%s (%s)", t, strings.Split(movie.ReleaseDate, "-")[0]))
		moviePath := filepath.Join(MoviesLibraryPath(), movieStrm)

		if _, err := os.Stat(moviePath); err == nil {
			paths[moviePath] = true
		}
	}

	return paths
}

func getShowPaths(show *tmdb.Show) map[string]bool {
	paths := getShowPathsByTMDB(show.ID)
	if len(paths) != 0 {
		return paths
	}

	titles := []string{show.Name, show.OriginalName}
	for _, t := range titles {
		showStrm := util.ToFileName(fmt.Sprintf("%s (%s)", t, strings.Split(show.FirstAirDate, "-")[0]))
		showPath := filepath.Join(ShowsLibraryPath(), showStrm)

		if _, err := os.Stat(showPath); err == nil {
			paths[showPath] = true
		}
	}

	return paths
}

func GetDuplicateStats() (movies, shows, episodes int, err error) {
	// Fetch Movie duplicates
	if moviesDuplicates, err := findMovieDuplicates(); err != nil {
		return movies, shows, episodes, err
	} else {
		movies = len(moviesDuplicates)
	}

	// Fetch Season duplicates
	if showsDuplicates, err := findShowDuplicates(); err != nil {
		return movies, shows, episodes, err
	} else {
		shows = len(showsDuplicates)
	}

	// Fetch Episode duplicates
	if episodeDuplicates, err := findEpisodeDuplicates(); err != nil {
		return movies, shows, episodes, err
	} else {
		episodes = len(episodeDuplicates)
	}

	return
}

func findMovieDuplicates() ([]*uid.Movie, error) {
	l := uid.Get()

	mu := l.GetMutex(uid.MoviesMutex)
	mu.RLock()
	defer mu.RUnlock()

	seen := map[int]struct{}{}
	duplicates := []*uid.Movie{}

	for _, m := range l.Movies {
		if m == nil || m.UIDs == nil || m.XbmcUIDs == nil || m.XbmcUIDs.Elementum == "" || !strings.HasSuffix(m.File, ".strm") {
			continue
		}

		if _, ok := seen[m.UIDs.TMDB]; ok {
			duplicates = append(duplicates, m)
		} else {
			seen[m.UIDs.TMDB] = struct{}{}
		}
	}

	return duplicates, nil
}

func findShowDuplicates() ([]*uid.Show, error) {
	l := uid.Get()

	mu := l.GetMutex(uid.ShowsMutex)
	mu.RLock()
	defer mu.RUnlock()

	seen := map[int]struct{}{}
	duplicates := []*uid.Show{}

	for _, s := range l.Shows {
		if s == nil || s.UIDs == nil || s.XbmcUIDs == nil || s.XbmcUIDs.Elementum == "" {
			continue
		}

		if _, ok := seen[s.UIDs.TMDB]; ok {
			duplicates = append(duplicates, s)
		} else {
			seen[s.UIDs.TMDB] = struct{}{}
		}
	}

	return duplicates, nil
}

func findEpisodeDuplicates() ([]*uid.Episode, error) {
	l := uid.Get()

	mu := l.GetMutex(uid.ShowsMutex)
	mu.RLock()
	defer mu.RUnlock()

	seen := map[string]struct{}{}
	duplicates := []*uid.Episode{}
	id := ""

	for _, s := range l.Shows {
		if s == nil || s.UIDs == nil || s.Episodes == nil || s.XbmcUIDs == nil || s.XbmcUIDs.Elementum == "" {
			continue
		}

		for _, e := range s.Episodes {
			if e == nil || e.UIDs == nil || !strings.HasSuffix(e.File, ".strm") {
				continue
			}

			id = fmt.Sprintf("%d.%d.%d", s.UIDs.TMDB, e.Season, e.Episode)
			if _, ok := seen[id]; ok {
				duplicates = append(duplicates, e)
			} else {
				seen[id] = struct{}{}
			}
		}
	}

	return duplicates, nil
}

func RemoveDuplicates() error {
	xbmcHost, err := xbmc.GetLocalXBMCHost()
	if xbmcHost == nil || err != nil {
		return err
	}

	var movies, shows, episodes int

	if movies, err = removeMovieDuplicates(xbmcHost); err != nil {
		log.Warningf("Could not remove movie duplicates: %s", err)
		return err
	}

	if shows, err = removeShowDuplicates(xbmcHost); err != nil {
		log.Warningf("Could not remove show duplicates: %s", err)
		return err
	}

	if episodes, err = removeEpisodeDuplicates(xbmcHost); err != nil {
		log.Warningf("Could not remove episode duplicates: %s", err)
		return err
	}

	if movies > 0 || shows > 0 || episodes > 0 {
		log.Debugf("Removed duplicate stats. Movies: %d, Shows: %d, Episodes: %d", movies, shows, episodes)
		PlanOverallUpdate()

		xbmcHost.Notify("Elementum", fmt.Sprintf("LOCALIZE[30684];;%d;;%d;;%d", movies, shows, episodes), config.AddonIcon())
	}

	return nil
}

func removeMovieDuplicates(xbmcHost *xbmc.XBMCHost) (int, error) {
	count := 0
	movies, err := findMovieDuplicates()
	if err != nil {
		return count, err
	}

	for _, m := range movies {
		if m.XbmcUIDs == nil || !strings.HasSuffix(m.File, ".strm") {
			continue
		}

		dir := filepath.Dir(m.File)
		log.Debugf("Removing duplicate movie '%s' from '%s'", m.Title, dir)

		// Remove strm file with parent folder from disk
		if err := os.RemoveAll(dir); err != nil {
			log.Error(err)
			continue
		}

		xbmcHost.VideoLibraryCleanDirectory(m.File, "movies", false)
		xbmcHost.VideoLibraryRemoveMovie(m.XbmcUIDs.Kodi)

		count++
	}

	return count, nil
}

func removeShowDuplicates(xbmcHost *xbmc.XBMCHost) (int, error) {
	count := 0
	shows, err := findShowDuplicates()
	if err != nil {
		return count, err
	}

	for _, s := range shows {
		if s.XbmcUIDs == nil || len(s.Episodes) == 0 || !strings.HasSuffix(s.Episodes[0].File, ".strm") {
			continue
		}

		dir := filepath.Dir(s.Episodes[0].File)
		log.Debugf("Removing duplicate show '%s' from '%s'", s.Title, dir)

		// Remove strm file with parent folder from disk
		if err := os.RemoveAll(dir); err != nil {
			log.Error(err)
			continue
		}

		for _, e := range s.Episodes {
			xbmcHost.VideoLibraryCleanDirectory(e.File, "tvshows", false)
		}

		xbmcHost.VideoLibraryRemoveTVShow(s.XbmcUIDs.Kodi)

		count++
	}

	return count, nil
}

func removeEpisodeDuplicates(xbmcHost *xbmc.XBMCHost) (int, error) {
	count := 0
	episodes, err := findEpisodeDuplicates()
	if err != nil {
		return count, err
	}

	for _, e := range episodes {
		if e.XbmcUIDs == nil || !strings.HasSuffix(e.File, ".strm") {
			continue
		}

		log.Debugf("Removing duplicate episode '%s' from '%s'", e.Title, e.File)

		// Remove strm file with parent folder from disk
		if err := os.RemoveAll(e.File); err != nil {
			log.Error(err)
			continue
		}

		xbmcHost.VideoLibraryCleanDirectory(e.File, "tvshows", false)
		xbmcHost.VideoLibraryRemoveEpisode(e.XbmcUIDs.Kodi)

		count++
	}

	return count, nil
}

package providers

import (
	"os"
	"sort"
	"strings"
	"time"

	"github.com/anacrolix/sync"
	"github.com/op/go-logging"
	"github.com/zeebo/bencode"

	"github.com/elgatito/elementum/bittorrent"
	"github.com/elgatito/elementum/config"
	"github.com/elgatito/elementum/tmdb"
	"github.com/elgatito/elementum/util"
	"github.com/elgatito/elementum/util/event"
	"github.com/elgatito/elementum/xbmc"
)

const (
	// SortMovies ...
	SortMovies = iota
	// SortShows ...
	SortShows
)

const (
	// SortBySeeders ...
	SortBySeeders = iota
	// SortByResolution ...
	SortByResolution
	// SortBalanced ...
	SortBalanced
	// SortBySize ...
	SortBySize
)

const (
	// Sort1080p720p480p ...
	Sort1080p720p480p = iota
	// Sort720p1080p480p ...
	Sort720p1080p480p
	// Sort720p480p1080p ...
	Sort720p480p1080p
	// Sort480p720p1080p ...
	Sort480p720p1080p
)

var (
	trackerTimeout = 6000 * time.Millisecond
	log            = logging.MustGetLogger("linkssearch")
)

// Search ...
func Search(xbmcHost *xbmc.XBMCHost, searchers []Searcher, query string) []*bittorrent.TorrentFile {
	torrentsChan := make(chan *bittorrent.TorrentFile)
	go func() {
		wg := sync.WaitGroup{}
		for _, searcher := range searchers {
			wg.Add(1)
			go func(searcher Searcher) {
				defer wg.Done()
				for _, torrent := range searcher.SearchLinks(query) {
					torrentsChan <- torrent
				}
			}(searcher)
		}
		wg.Wait()
		close(torrentsChan)
	}()

	return processLinks(xbmcHost, torrentsChan, SortMovies, false)
}

// SearchMovie ...
func SearchMovie(xbmcHost *xbmc.XBMCHost, searchers []MovieSearcher, movie *tmdb.Movie) []*bittorrent.TorrentFile {
	torrentsChan := make(chan *bittorrent.TorrentFile)
	go func() {
		wg := sync.WaitGroup{}
		for _, searcher := range searchers {
			wg.Add(1)
			go func(searcher MovieSearcher) {
				defer wg.Done()
				for _, torrent := range searcher.SearchMovieLinks(movie) {
					torrentsChan <- torrent
				}
			}(searcher)
		}
		wg.Wait()
		close(torrentsChan)
	}()

	return processLinks(xbmcHost, torrentsChan, SortMovies, false)
}

// SearchMovieSilent ...
func SearchMovieSilent(xbmcHost *xbmc.XBMCHost, searchers []MovieSearcher, movie *tmdb.Movie, withAuth bool) []*bittorrent.TorrentFile {
	torrentsChan := make(chan *bittorrent.TorrentFile)
	go func() {
		wg := sync.WaitGroup{}
		for _, searcher := range searchers {
			wg.Add(1)
			go func(searcher MovieSearcher) {
				defer wg.Done()
				for _, torrent := range searcher.SearchMovieLinksSilent(movie, withAuth) {
					torrentsChan <- torrent
				}
			}(searcher)
		}
		wg.Wait()
		close(torrentsChan)
	}()

	return processLinks(xbmcHost, torrentsChan, SortMovies, true)
}

// SearchSeason ...
func SearchSeason(xbmcHost *xbmc.XBMCHost, searchers []SeasonSearcher, show *tmdb.Show, season *tmdb.Season) []*bittorrent.TorrentFile {
	torrentsChan := make(chan *bittorrent.TorrentFile)
	go func() {
		wg := sync.WaitGroup{}
		for _, searcher := range searchers {
			wg.Add(1)
			go func(searcher SeasonSearcher) {
				defer wg.Done()
				for _, torrent := range searcher.SearchSeasonLinks(show, season) {
					torrentsChan <- torrent
				}
			}(searcher)
		}
		wg.Wait()
		close(torrentsChan)
	}()

	return processLinks(xbmcHost, torrentsChan, SortShows, false)
}

// SearchEpisode ...
func SearchEpisode(xbmcHost *xbmc.XBMCHost, searchers []EpisodeSearcher, show *tmdb.Show, season *tmdb.Season, episode *tmdb.Episode) []*bittorrent.TorrentFile {
	torrentsChan := make(chan *bittorrent.TorrentFile)
	go func() {
		wg := sync.WaitGroup{}
		for _, searcher := range searchers {
			wg.Add(1)
			go func(searcher EpisodeSearcher) {
				defer wg.Done()
				for _, torrent := range searcher.SearchEpisodeLinks(show, season, episode) {
					torrentsChan <- torrent
				}
			}(searcher)
		}
		wg.Wait()
		close(torrentsChan)
	}()

	return processLinks(xbmcHost, torrentsChan, SortShows, false)
}

func processLinks(xbmcHost *xbmc.XBMCHost, torrentsChan chan *bittorrent.TorrentFile, sortType int, isSilent bool) []*bittorrent.TorrentFile {
	torrentsMap := map[string]*bittorrent.TorrentFile{}

	torrents := make([]*bittorrent.TorrentFile, 0)

	log.Info("Resolving torrent files...")
	progress := 0
	progressTotal := 1
	progressUpdate := make(chan string)
	closed := event.Event{}

	defer func() {
		log.Debug("Closing progressupdate")
		closed.Set()
		close(progressUpdate)
	}()

	wg := sync.WaitGroup{}
	for torrent := range torrentsChan {
		wg.Add(1)
		if !strings.HasPrefix(torrent.URI, "magnet") {
			progressTotal++
		}
		torrents = append(torrents, torrent)
		go func(torrent *bittorrent.TorrentFile) {
			defer wg.Done()

			resolved := make(chan bool)
			failed := make(chan bool)

			go func(torrent *bittorrent.TorrentFile) {
				if err := torrent.Resolve(); err != nil {
					log.Warningf("Resolve failed for %s : %s", torrent.URI, err.Error())
					close(failed)
				}
				close(resolved)
			}(torrent)

			for {
				select {
				case <-time.After(trackerTimeout * 2): // Resolve timeout...
					return
				case <-failed:
					return
				case <-resolved:
					if closed.IsSet() {
						return
					}

					if !strings.HasPrefix(torrent.URI, "magnet") {
						progress++
						progressUpdate <- "LOCALIZE[30117]"
					} else {
						progressUpdate <- "skip"
					}

					return
				}
			}
		}(torrent)
	}

	var dialogProgressBG *xbmc.DialogProgressBG
	if !isSilent {
		dialogProgressBG = xbmcHost.NewDialogProgressBG("Elementum", "LOCALIZE[30117]", "LOCALIZE[30117]", "LOCALIZE[30118]")
		go func() {
			for {
				select {
				case <-time.After(trackerTimeout * 2):
					return
				case msg, ok := <-progressUpdate:
					if !ok {
						return
					}
					if dialogProgressBG != nil {
						if msg != "skip" {
							dialogProgressBG.Update(progress*100/progressTotal, "Elementum", msg)
						}
					} else {
						return
					}
				}
			}
		}()
	}

	wg.Wait()

	if !isSilent && dialogProgressBG != nil {
		dialogProgressBG.Update(100, "Elementum", "LOCALIZE[30117]")
	}

	for _, torrent := range torrents {
		if torrent.InfoHash == "" {
			continue
		}

		torrentKey := torrent.InfoHash
		if torrent.IsPrivate {
			torrentKey = torrent.InfoHash + "-" + torrent.Provider
		}

		if existingTorrent, exists := torrentsMap[torrentKey]; exists {
			// Collect all trackers
			for _, trackerURL := range torrent.Trackers {
				if !util.StringSliceContains(existingTorrent.Trackers, trackerURL) {
					existingTorrent.Trackers = append(existingTorrent.Trackers, trackerURL)
				}
			}

			existingTorrent.Provider += ", " + torrent.Provider
			if torrent.Resolution > existingTorrent.Resolution {
				existingTorrent.Name = torrent.Name
				existingTorrent.Resolution = torrent.Resolution
			}
			if torrent.VideoCodec > existingTorrent.VideoCodec {
				existingTorrent.VideoCodec = torrent.VideoCodec
			}
			if torrent.AudioCodec > existingTorrent.AudioCodec {
				existingTorrent.AudioCodec = torrent.AudioCodec
			}
			if torrent.RipType > existingTorrent.RipType {
				existingTorrent.RipType = torrent.RipType
			}
			if torrent.SceneRating > existingTorrent.SceneRating {
				existingTorrent.SceneRating = torrent.SceneRating
			}
			if existingTorrent.Title == "" && torrent.Title != "" {
				existingTorrent.Title = torrent.Title
			}
			if existingTorrent.IsMagnet() && !torrent.IsMagnet() {
				existingTorrent.URI = torrent.URI
			}
			if existingTorrent.Seeds < torrent.Seeds {
				existingTorrent.Seeds = torrent.Seeds
				existingTorrent.Peers = torrent.Peers
			}

			existingTorrent.Multi = true
		} else {
			torrentsMap[torrentKey] = torrent
		}
	}

	torrents = make([]*bittorrent.TorrentFile, 0, len(torrentsMap))
	for _, torrent := range torrentsMap {
		torrent.UpdateTorrentTrackers()
		torrents = append(torrents, torrent)
	}

	log.Infof("Received %d unique links.", len(torrents))

	if len(torrents) == 0 {
		if !isSilent && dialogProgressBG != nil {
			dialogProgressBG.Close()
		}
		return torrents
	}

	if !isSilent && dialogProgressBG != nil {
		dialogProgressBG.Close()
		dialogProgressBG = nil
	}

	for _, t := range torrents {
		if _, err := os.Stat(t.URI); err != nil {
			continue
		}

		in, err := os.ReadFile(t.URI)
		if err != nil {
			log.Debugf("Cannot read torrent file: %s", err)
			continue
		}

		var torrentFile *bittorrent.TorrentFileRaw
		err = bencode.DecodeBytes(in, &torrentFile)
		if err != nil || torrentFile == nil {
			log.Debugf("Cannot decode torrent file: %s", err)
			continue
		}

		torrentFile.Title = t.Name
		torrentFile.Announce = ""
		torrentFile.AnnounceList = [][]string{}
		uniqueTrackers := map[string]struct{}{}
		for _, tr := range t.Trackers {
			if len(tr) == 0 {
				continue
			}

			uniqueTrackers[tr] = struct{}{}
		}
		for tr := range uniqueTrackers {
			torrentFile.AnnounceList = append(torrentFile.AnnounceList, []string{tr})
		}

		out, err := bencode.EncodeBytes(torrentFile)
		if err != nil {
			log.Debugf("Cannot encode torrent file: %s", err)
			continue
		}

		err = os.WriteFile(t.URI, out, 0666)
		if err != nil {
			log.Debugf("Cannot write torrent file: %s", err)
			continue
		}

	}

	// Sorting resulting list of torrents
	conf := config.Get()
	sortMode := conf.SortingModeMovies
	resolutionPreference := conf.ResolutionPreferenceMovies

	if sortType == SortShows {
		sortMode = conf.SortingModeShows
		resolutionPreference = conf.ResolutionPreferenceShows
	}

	seeds := func(c1, c2 *bittorrent.TorrentFile) bool { return c1.Seeds > c2.Seeds }
	resolutionUp := func(c1, c2 *bittorrent.TorrentFile) bool { return c1.Resolution < c2.Resolution }
	resolutionDown := func(c1, c2 *bittorrent.TorrentFile) bool { return c1.Resolution > c2.Resolution }
	resolution720p1080p := func(c1, c2 *bittorrent.TorrentFile) bool { return Resolution720p1080p(c1) < Resolution720p1080p(c2) }
	resolution720p480p := func(c1, c2 *bittorrent.TorrentFile) bool { return Resolution720p480p(c1) < Resolution720p480p(c2) }
	balanced := func(c1, c2 *bittorrent.TorrentFile) bool { return float64(c1.Seeds) > Balanced(c2) }

	if sortMode == SortBySize {
		sort.Slice(torrents, func(i, j int) bool {
			return torrents[i].SizeParsed > torrents[j].SizeParsed
		})
	} else if sortMode == SortBySeeders {
		sort.Sort(sort.Reverse(BySeeds(torrents)))
	} else {
		switch resolutionPreference {
		case Sort1080p720p480p:
			if sortMode == SortBalanced {
				SortBy(balanced, resolutionDown).Sort(torrents)
			} else {
				SortBy(resolutionDown, seeds).Sort(torrents)
			}
		case Sort480p720p1080p:
			if sortMode == SortBalanced {
				SortBy(balanced, resolutionUp).Sort(torrents)
			} else {
				SortBy(resolutionUp, seeds).Sort(torrents)
			}
		case Sort720p1080p480p:
			if sortMode == SortBalanced {
				SortBy(balanced, resolution720p1080p).Sort(torrents)
			} else {
				SortBy(resolution720p1080p, seeds).Sort(torrents)
			}
		case Sort720p480p1080p:
			if sortMode == SortBalanced {
				SortBy(balanced, resolution720p480p).Sort(torrents)
			} else {
				SortBy(resolution720p480p, seeds).Sort(torrents)
			}
		}
	}

	// log.Info("Sorted torrent candidates.")
	// for _, torrent := range torrents {
	// 	log.Infof("S:%d P:%d %s - %s - %s", torrent.Seeds, torrent.Peers, torrent.Name, torrent.Provider, torrent.URI)
	// }

	return torrents
}

package providers

import (
	"cmp"
	"os"
	"slices"
	"strings"
	"sync/atomic"
	"time"

	"github.com/anacrolix/sync"
	"github.com/op/go-logging"
	"github.com/zeebo/bencode"

	"github.com/elgatito/elementum/bittorrent"
	"github.com/elgatito/elementum/config"
	"github.com/elgatito/elementum/tmdb"
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
	return searchAll(xbmcHost, searchers, SortMovies, false, func(s Searcher) []*bittorrent.TorrentFile {
		return s.SearchLinks(query)
	})
}

// SearchMovie ...
func SearchMovie(xbmcHost *xbmc.XBMCHost, searchers []MovieSearcher, movie *tmdb.Movie) []*bittorrent.TorrentFile {
	return searchAll(xbmcHost, searchers, SortMovies, false, func(s MovieSearcher) []*bittorrent.TorrentFile {
		return s.SearchMovieLinks(movie)
	})
}

// SearchMovieSilent ...
func SearchMovieSilent(xbmcHost *xbmc.XBMCHost, searchers []MovieSearcher, movie *tmdb.Movie, withAuth bool) []*bittorrent.TorrentFile {
	return searchAll(xbmcHost, searchers, SortMovies, true, func(s MovieSearcher) []*bittorrent.TorrentFile {
		return s.SearchMovieLinksSilent(movie, withAuth)
	})
}

// SearchSeason ...
func SearchSeason(xbmcHost *xbmc.XBMCHost, searchers []SeasonSearcher, show *tmdb.Show, season *tmdb.Season) []*bittorrent.TorrentFile {
	return searchAll(xbmcHost, searchers, SortShows, false, func(s SeasonSearcher) []*bittorrent.TorrentFile {
		return s.SearchSeasonLinks(show, season)
	})
}

// SearchEpisode ...
func SearchEpisode(xbmcHost *xbmc.XBMCHost, searchers []EpisodeSearcher, show *tmdb.Show, season *tmdb.Season, episode *tmdb.Episode) []*bittorrent.TorrentFile {
	return searchAll(xbmcHost, searchers, SortShows, false, func(s EpisodeSearcher) []*bittorrent.TorrentFile {
		return s.SearchEpisodeLinks(show, season, episode)
	})
}

// SearchEpisodeSilent is SearchEpisode without Kodi dialogs, in Elementum and in the providers.
func SearchEpisodeSilent(xbmcHost *xbmc.XBMCHost, searchers []EpisodeSearcher, show *tmdb.Show, season *tmdb.Season, episode *tmdb.Episode, withAuth bool) []*bittorrent.TorrentFile {
	return searchAll(xbmcHost, searchers, SortShows, true, func(s EpisodeSearcher) []*bittorrent.TorrentFile {
		return s.SearchEpisodeLinksSilent(show, season, episode, withAuth)
	})
}

// searchAll runs search on every searcher concurrently and resolves the merged links.
func searchAll[S any](xbmcHost *xbmc.XBMCHost, searchers []S, sortType int, isSilent bool, search func(S) []*bittorrent.TorrentFile) []*bittorrent.TorrentFile {
	torrentsChan := make(chan *bittorrent.TorrentFile)
	go func() {
		wg := sync.WaitGroup{}
		for _, searcher := range searchers {
			wg.Add(1)
			go func(searcher S) {
				defer wg.Done()
				for _, torrent := range search(searcher) {
					torrentsChan <- torrent
				}
			}(searcher)
		}
		wg.Wait()
		close(torrentsChan)
	}()

	return processLinks(xbmcHost, torrentsChan, sortType, isSilent)
}

func processLinks(xbmcHost *xbmc.XBMCHost, torrentsChan chan *bittorrent.TorrentFile, sortType int, isSilent bool) []*bittorrent.TorrentFile {
	torrentsMap := map[string]*bittorrent.TorrentFile{}

	torrents := make([]*bittorrent.TorrentFile, 0)

	log.Info("Resolving torrent files...")
	var progress atomic.Int32
	progressTotal := 1
	progressUpdate := make(chan string)

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
					if !strings.HasPrefix(torrent.URI, "magnet") {
						progress.Add(1)
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
	if !isSilent && xbmcHost != nil {
		dialogProgressBG = xbmcHost.NewDialogProgressBG("Elementum", "LOCALIZE[30117]", "LOCALIZE[30117]", "LOCALIZE[30118]")
	}
	// Drain until closed so resolvers never block on send; the dialog is optional.
	readerDone := make(chan struct{})
	go func(dialog *xbmc.DialogProgressBG) {
		defer close(readerDone)
		for msg := range progressUpdate {
			if dialog != nil && msg != "skip" {
				dialog.Update(int(progress.Load())*100/progressTotal, "Elementum", msg)
			}
		}
	}(dialogProgressBG)

	wg.Wait()
	// All senders are done, so closing is safe; wait for the reader before touching the dialog.
	close(progressUpdate)
	<-readerDone

	if dialogProgressBG != nil {
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
				if !slices.Contains(existingTorrent.Trackers, trackerURL) {
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
		slices.SortFunc(torrents, func(a, b *bittorrent.TorrentFile) int {
			return cmp.Compare(b.SizeParsed, a.SizeParsed)
		})
	} else if sortMode == SortBySeeders {
		slices.SortFunc(torrents, func(a, b *bittorrent.TorrentFile) int {
			return cmp.Compare(b.Seeds, a.Seeds)
		})
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

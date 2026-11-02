package bittorrent

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	lt "github.com/ElementumOrg/libtorrent-go"

	"github.com/elgatito/elementum/config"
	"github.com/elgatito/elementum/database"
	"github.com/elgatito/elementum/library"
	"github.com/elgatito/elementum/tmdb"
	"github.com/elgatito/elementum/util"
	"github.com/elgatito/elementum/xbmc"
)

var (
	completedVideoRe   = regexp.MustCompile(`(?i)\.(mkv|mp4|avi|mov|m4v|wmv|ts|m2ts)$`)
	completedSampleRe  = regexp.MustCompile(`(?i)\bsample\b`)
	completedEpisodeRe = regexp.MustCompile(`(?i)S(\d{1,2})[ ._-]?E(\d{1,3})|\b(\d{1,2})x(\d{2,3})\b`)
	completedRarRe     = regexp.MustCompile(`(?i)\.rar$`)
)

// completedFileNames maps torrent file paths to Kodi-style names, like library .strm files use:
// "Title (Year).ext" for a movie, "Show (Year) S01E02.ext" for episodes.
// Files named after a renamed video (subtitles, nfo) follow it, ambiguous files keep their names.
func completedFileNames(files []string, name string, isMovie bool, season, episode int) map[string]string {
	videos := []string{}
	for _, f := range files {
		if completedVideoRe.MatchString(f) && !completedSampleRe.MatchString(filepath.Base(f)) {
			videos = append(videos, f)
		}
	}

	stems := map[string]string{}
	used := map[string]int{}
	for _, v := range videos {
		stem := ""
		if isMovie {
			if len(videos) == 1 {
				stem = name
			}
		} else if m := completedEpisodeRe.FindStringSubmatch(filepath.Base(v)); m != nil {
			s, _ := strconv.Atoi(m[1] + m[3])
			e, _ := strconv.Atoi(m[2] + m[4])
			stem = fmt.Sprintf("%s S%02dE%02d", name, s, e)
		} else if len(videos) == 1 && episode > 0 {
			stem = fmt.Sprintf("%s S%02dE%02d", name, season, episode)
		}
		if stem != "" {
			stems[v] = stem
			used[stem]++
		}
	}

	names := map[string]string{}
	oldStems := map[string]string{}
	for v, stem := range stems {
		// Same episode in several files (e.g. different qualities) would overwrite each other
		if used[stem] > 1 {
			continue
		}
		names[v] = stem + filepath.Ext(v)
		oldStems[strings.TrimSuffix(filepath.Base(v), filepath.Ext(v))] = stem
	}

	for _, f := range files {
		if _, ok := names[f]; ok {
			continue
		}
		base := filepath.Base(f)
		for old, stem := range oldStems {
			if strings.HasPrefix(base, old+".") {
				names[f] = stem + base[len(old):]
				break
			}
		}
	}

	return names
}

// moveCompleted removes a finished torrent from the session and moves its files to the completed folder
func (s *Service) moveCompleted(xbmcHost *xbmc.XBMCHost, t *Torrent, torrentName string, pathChecked, warnedMissing *sync.Map) error {
	if !t.IsMoveInProgress.CompareAndSwap(false, true) {
		return nil
	}
	defer t.IsMoveInProgress.Store(false)

	infoHash := t.InfoHash()
	item := database.GetStorm().GetBTItem(infoHash)
	if item == nil {
		warnedMissing.Store(infoHash, true)
		return fmt.Errorf("Torrent not found with infohash: %s", infoHash)
	}

	if item.Type == "" {
		err := fmt.Errorf("Missing item type to move files to completed folder for %s", torrentName)
		log.Error(err)
		return err
	}
	log.Warning(torrentName, "finished seeding, moving files...")

	// Check paths are valid and writable, and only once
	completedPath := s.config.CompletedShowsPath
	if item.Type == "movie" {
		completedPath = s.config.CompletedMoviesPath
	}
	if _, checked := pathChecked.LoadOrStore(item.Type, true); !checked {
		if err := util.IsWritablePath(completedPath); err != nil {
			warnedMissing.Store(infoHash, true)
			log.Error(err)
			return err
		}
	}

	filesToMove := []string{}
	if !t.withHandle(func(lt.TorrentHandle) {
		for _, fp := range t.files {
			f := t.GetFileByPath(fp.Path)
			filesToMove = append(filesToMove, t.ti.Files().FilePath(f.Index))
		}
	}) {
		return errors.New("Torrent is closed")
	}

	log.Infof("Moving torrent '%s' to completed folder", t.Name())
	log.Info("Removing the torrent without deleting files after Completed move ...")

	// Removing torrent from libtorrent session before moving files physically
	s.RemoveTorrent(xbmcHost, t, RemoveOptions{ForceDrop: true, ForceKeepTorrentData: true})

	if len(t.files) <= 0 {
		return errors.New("No files listed in the torrent")
	}

	dstPath, mediaName := completedDestination(item, util.EffectiveDir(completedPath))

	newNames := map[string]string{}
	if s.config.CompletedRename && mediaName != "" {
		newNames = completedFileNames(filesToMove, mediaName, item.Type == "movie", item.Season, item.Episode)
	}

	filesToCleanup := map[string]bool{}
	notMoved := []string{}
	for _, filePath := range filesToMove {
		fileName := filepath.Base(filePath)

		extracted := ""
		if completedRarRe.MatchString(fileName) {
			var err error
			if extracted, err = s.extractedFile(filePath); err != nil {
				return err
			}
			filePath = filepath.Join(filepath.Dir(filePath), "extracted", extracted)
		}

		// Renamed file must not overwrite an existing one, e.g. same movie downloaded before
		target := dstPath
		if name, ok := newNames[filePath]; ok && !util.FileExists(filepath.Join(dstPath, name)) {
			target = filepath.Join(dstPath, name)
		}

		srcPath := filepath.Join(s.config.DownloadPath, filePath)
		dst, err := moveWithRetry(srcPath, target)
		if err != nil {
			log.Error(err)
			// File is still in the download folder, its directory must not be cleaned up
			if util.FileExists(srcPath) {
				notMoved = append(notMoved, filepath.Dir(srcPath))
			}
			continue
		}
		log.Warning(fileName, "moved to", dst)

		if filepath.Dir(filePath) != "." {
			filesToCleanup[filepath.Dir(srcPath)] = true
			if extracted != "" {
				parentPath := filepath.Clean(filepath.Join(filepath.Dir(srcPath), ".."))
				if parentPath != "." && parentPath != s.config.DownloadPath {
					filesToCleanup[parentPath] = true
				}
			}
		}
	}

	removeMovedFolders(filesToCleanup, notMoved)

	// Data is no longer in the download folder, resume data is useless now
	if len(notMoved) == 0 {
		os.Remove(t.fastResumeFile)
		os.Remove(t.partsFile)
	}

	log.Infof("Marking %s for removal from library and database...", torrentName)
	database.GetStorm().UpdateBTItemStatus(infoHash, Remove)

	return nil
}

// completedDestination returns the folder for moved files, named like library folders,
// and the media name used for it (empty if TMDB item is unknown)
func completedDestination(item *database.BTItem, basePath string) (dstPath, mediaName string) {
	dstPath = basePath
	if item.Type == "movie" {
		if item.ID > 0 {
			if movie := tmdb.GetMovie(item.ID, config.Get().Language); movie != nil {
				mediaName = library.GetMoviePathTitle(movie)
				dstPath = filepath.Join(dstPath, mediaName)
				os.MkdirAll(dstPath, 0755)
			}
		}
	} else if item.ShowID > 0 {
		if show := tmdb.GetShow(item.ShowID, config.Get().Language); show != nil {
			mediaName = library.GetShowPathTitle(show)
			seasonPath := filepath.Join(mediaName, fmt.Sprintf("Season %d", item.Season))
			if item.Season == 0 {
				seasonPath = filepath.Join(mediaName, "Specials")
			}
			dstPath = filepath.Join(dstPath, seasonPath)
			os.MkdirAll(dstPath, 0755)
		}
	}
	return
}

// extractedFile returns the name of the video extracted from a rar archive into "extracted" folder next to it
func (s *Service) extractedFile(rarPath string) (string, error) {
	files, err := os.ReadDir(filepath.Join(s.config.DownloadPath, filepath.Dir(rarPath), "extracted"))
	if err != nil {
		return "", err
	}
	if len(files) == 1 {
		return files[0].Name(), nil
	}
	for _, file := range files {
		if completedVideoRe.MatchString(file.Name()) {
			return file.Name(), nil
		}
	}
	return "", errors.New("No extracted file to move")
}

// moveWithRetry retries transient failures (e.g. NAS hiccup during copy), a failed Move leaves the source in place
func moveWithRetry(srcPath, target string) (dst string, err error) {
	log.Infof("Moving file %s to %s", srcPath, target)
	dst, err = util.Move(srcPath, target)
	for attempt := 2; err != nil && attempt <= moveAttempts && util.FileExists(srcPath); attempt++ {
		log.Warningf("Moving %s failed: %s. Retrying in %s (attempt %d of %d)", srcPath, err, moveRetryDelay, attempt, moveAttempts)
		time.Sleep(moveRetryDelay)
		dst, err = util.Move(srcPath, target)
	}
	return
}

// removeMovedFolders removes leftover folders, except ones still holding files that failed to move
func removeMovedFolders(folders map[string]bool, notMoved []string) {
	for folder := range folders {
		if slices.ContainsFunc(notMoved, func(dir string) bool {
			return dir == folder || strings.HasPrefix(dir, folder+string(filepath.Separator))
		}) {
			log.Warningf("Not removing %s, it still contains files that failed to move", folder)
			continue
		}
		log.Infof("Removing all from %s", folder)
		os.RemoveAll(folder)
	}
}

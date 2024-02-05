package repository

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/elgatito/elementum/api/repository"
	"github.com/elgatito/elementum/proxy"
	"github.com/elgatito/elementum/xbmc"

	"github.com/op/go-logging"
)

const (
	repositoryOrg  = "ElementumOrg"
	repositoryName = "repository.elementumorg"
)

var (
	log = logging.MustGetLogger("repository")
)

// CheckBurst ...
func CheckBurst(h *xbmc.XBMCHost, skipBurstSearch bool, addonIcon string) {
	if h == nil || !h.Ping() {
		return
	}

	// Check for enabled providers and Elementum Burst
	for _, addon := range h.GetAddons("xbmc.python.script", "executable", "all", []string{"name", "version", "enabled"}).Addons {
		if strings.HasPrefix(addon.ID, "script.elementum.") {
			if addon.Enabled {
				return
			}
		}
	}

	for timeout := 0; timeout < 10; timeout++ {
		if h.IsAddonInstalled(repositoryName) {
			break
		}
		log.Info("Sleeping 1 second while waiting for Elementum repository add-on to be installed")
		time.Sleep(1 * time.Second)
	}

	log.Info("Updating Kodi add-on repositories for Burst...")
	h.UpdateLocalAddons()
	h.UpdateAddonRepos()

	if !skipBurstSearch {
		log.Infof("Triggering Kodi to check for script.elementum.burst plugin")
		h.InstallAddon("script.elementum.burst")

		for timeout := 0; timeout < 30; timeout++ {
			if h.IsAddonInstalled("script.elementum.burst") {
				break
			}
			log.Info("Sleeping 1 second while waiting for script.elementum.burst add-on to be installed")
			time.Sleep(1 * time.Second)
		}

		log.Infof("Checking for existence of script.elementum.burst plugin after installation attempt")
		if h.IsAddonInstalled("script.elementum.burst") {
			h.SetAddonEnabled("script.elementum.burst", true)
			h.Notify("Elementum", "LOCALIZE[30272]", addonIcon)
		} else {
			h.Dialog("Elementum", "LOCALIZE[30273]")
		}
	}
}

func CheckRepository(h *xbmc.XBMCHost, skipRepositorySearch bool, profilePath string) bool {
	if h == nil || !h.Ping() || skipRepositorySearch {
		return false
	}

	if h.IsAddonInstalled(repositoryName) {
		if !h.IsAddonEnabled(repositoryName) {
			h.SetAddonEnabled(repositoryName, true)
		}
		return true
	}

	log.Info("Creating Elementum repository add-on...")
	if err := MakeElementumRepositoryAddon(profilePath); err != nil {
		log.Errorf("Unable to create repository add-on: %s", err)
		return false
	}

	h.UpdateLocalAddons()
	for _, addon := range h.GetAddons("xbmc.addon.repository", "unknown", "all", []string{"name", "version", "enabled"}).Addons {
		if addon.ID == repositoryName && addon.Enabled {
			log.Info("Found enabled Elementum repository add-on")
			return false
		}
	}
	log.Info("Elementum repository not installed, installing...")
	h.InstallAddon(repositoryName)
	for timeout := 0; timeout < 10; timeout++ {
		if h.IsAddonInstalled(repositoryName) {
			break
		}
		log.Info("Sleeping 1 second while waiting for Elementum repository add-on to be installed")
		time.Sleep(1 * time.Second)
	}
	time.Sleep(1 * time.Second)
	h.SetAddonEnabled(repositoryName, true)
	h.UpdateLocalAddons()
	h.UpdateAddonRepos()

	return true
}

// MakeElementumRepositoryAddon ...
func MakeElementumRepositoryAddon(profilePath string) error {
	addonPath := filepath.Clean(filepath.Join(profilePath, ".."))
	if err := os.MkdirAll(addonPath, 0777); err != nil {
		return err
	}

	release := repository.GetLatestRelease(repositoryOrg, repositoryName)
	if release == nil || len(release.Assets) == 0 {
		log.Errorf("Could not find release information for %s/%s", repositoryOrg, repositoryName)
		return errors.New("Cannot find repository release information")
	}

	asset := release.Assets[len(release.Assets)-1]
	if asset.BrowserDownloadURL == "" {
		return errors.New("Cannot find repository asset")
	}

	resp, err := proxy.GetClient().Get(asset.BrowserDownloadURL)
	if err != nil || resp == nil {
		log.Errorf("Could not download zip file: %s", err)
		return err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Errorf("Could not read zip file response: %s", err)
		return err
	}

	return unzip(body, addonPath)
}

func unzip(src []byte, dest string) error {
	r, err := zip.NewReader(bytes.NewReader(src), int64(len(src)))
	if err != nil {
		return err
	}

	log.Debugf("Extracting repository addon information to %s", dest)

	// Closure to address file descriptors issue with all the deferred .Close() methods
	extractAndWriteFile := func(f *zip.File) error {
		rc, err := f.Open()
		if err != nil {
			return err
		}
		defer func() {
			if err := rc.Close(); err != nil {
				panic(err)
			}
		}()

		path := filepath.Join(dest, f.Name)

		// Check for ZipSlip (Directory traversal)
		if !strings.HasPrefix(path, filepath.Clean(dest)+string(os.PathSeparator)) {
			return fmt.Errorf("illegal file path: %s", path)
		}

		if f.FileInfo().IsDir() {
			os.MkdirAll(path, f.Mode())
		} else {
			os.MkdirAll(filepath.Dir(path), f.Mode())
			f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode())
			if err != nil {
				return err
			}
			defer func() {
				if err := f.Close(); err != nil {
					panic(err)
				}
			}()

			_, err = io.Copy(f, rc)
			if err != nil {
				return err
			}
		}
		return nil
	}

	for _, f := range r.File {
		err := extractAndWriteFile(f)
		if err != nil {
			return err
		}
	}

	return nil
}

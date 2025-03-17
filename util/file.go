package util

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"

	"github.com/elgatito/elementum/xbmc"
)

const (
	WindowsPathSeparator = `\`
	LinuxPathSeparator   = `/`
)

var (
	windowsPathRegex = regexp.MustCompile(`^[a-zA-Z]:\\`)
	networkPathRegex = regexp.MustCompile(`^\\\\`)
)

var audioExtensions = []string{
	".nsv",
	".m4a",
	".flac",
	".aac",
	".strm",
	".pls",
	".rm",
	".rma",
	".mpa",
	".wav",
	".wma",
	".ogg",
	".mp3",
	".mp2",
	".m3u",
	".gdm",
	".imf",
	".m15",
	".sfx",
	".uni",
	".ac3",
	".dts",
	".cue",
	".aif",
	".aiff",
	".wpl",
	".ape",
	".mac",
	".mpc",
	".mp+",
	".mpp",
	".shn",
	".wv",
	".dsp",
	".xsp",
	".xwav",
	".waa",
	".wvs",
	".wam",
	".gcm",
	".idsp",
	".mpdsp",
	".mss",
	".spt",
	".rsd",
	".sap",
	".cmc",
	".cmr",
	".dmc",
	".mpt",
	".mpd",
	".rmt",
	".tmc",
	".tm8",
	".tm2",
	".oga",
	".tta",
	".wtv",
	".mka",
	".tak",
	".opus",
	".dff",
	".dsf",
	".m4b",
}

var srtExtensions = []string{
	".srt",         // SubRip text file
	".ssa", ".ass", // Advanced Substation
	".usf", // Universal Subtitle Format
	".cdg",
	".idx", // VobSub
	".sub", // MicroDVD or SubViewer
	".utf",
	".aqt", // AQTitle
	".jss", // JacoSub
	".psb", // PowerDivX
	".rt",  // RealText
	".smi", // SAMI
	// ".txt", // MPEG 4 Timed Text
	".smil",
	".stl", // Spruce Subtitle Format
	".dks",
	".pjs", // Phoenix Subtitle
	".mpl2",
	".mks",
}

// ToFileName ...
func ToFileName(filename string) string {
	reserved := []string{"<", ">", ":", "\"", "/", "\\", "", "", "?", "*", "%", "+"}
	for _, reservedchar := range reserved {
		filename = strings.Replace(filename, reservedchar, "", -1)
	}
	return filename
}

// IsSubtitlesExt checks if extension belong to Subtitles type
func IsSubtitlesExt(ext string) bool {
	for _, e := range srtExtensions {
		if ext == e {
			return true
		}
	}

	return false
}

// HasSubtitlesExt searches different subtitles extensions in file name
func HasSubtitlesExt(filename string) bool {
	for _, e := range srtExtensions {
		if strings.HasSuffix(filename, e) {
			return true
		}
	}

	return false
}

// IsAudioExt checks if extension belong to Audio type
func IsAudioExt(ext string) bool {
	for _, e := range audioExtensions {
		if ext == e {
			return true
		}
	}

	return false
}

// HasAudioExt searches different audio extensions in file name
func HasAudioExt(filename string) bool {
	for _, e := range audioExtensions {
		if strings.HasSuffix(filename, e) {
			return true
		}
	}

	return false
}

// FileExists check for file existence in a simple way
func FileExists(name string) bool {
	if _, err := os.Stat(name); err != nil {
		if os.IsNotExist(err) {
			return false
		}
	}
	return true
}

// FileWithoutExtension returns file without extension
func FileWithoutExtension(name string) string {
	if pos := strings.LastIndexByte(name, '.'); pos != -1 {
		return name[:pos]
	}
	return name
}

// PathExists returns whether path exists in OS
func PathExists(path string) bool {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return false
	}

	return true
}

// IsWritablePath ...
func IsWritablePath(path string) error {
	if path == "." {
		return errors.New("Path not set")
	}
	// TODO: Review this after test evidences come
	if IsNetworkPath(path) {
		return fmt.Errorf("Network paths are not supported, change %s to a locally mounted path by the OS", path)
	}
	if p, err := os.Stat(path); err != nil || !p.IsDir() {
		if err != nil {
			return err
		}
		return fmt.Errorf("%s is not a valid directory", path)
	}
	writableFile := filepath.Join(path, ".writable")
	writable, err := os.Create(writableFile)
	if err != nil {
		return err
	}
	writable.Close()
	os.Remove(writableFile)
	return nil
}

func IsSpecialPath(path string) bool {
	return strings.HasPrefix(path, "special:")
}

func IsNetworkPath(path string) bool {
	return strings.HasPrefix(path, "nfs") || strings.HasPrefix(path, "smb")
}

func GetKodiPath(path string, substitutions *map[string]string, platform *xbmc.Platform) (ret string) {
	// Select default separator depending on the Kodi platform
	separator := LinuxPathSeparator
	if platform != nil && strings.ToLower(platform.OS) == "windows" {
		separator = WindowsPathSeparator
	}

	return SubstitutePathToFrom(path, substitutions, separator)
}

func GetRealPath(path string, substitutions *map[string]string) (ret string) {
	xbmcHost, err := xbmc.GetLocalXBMCHost()
	canResolveSpecialPath := xbmcHost != nil && err == nil

	if IsSpecialPath(path) && canResolveSpecialPath {
		return SubstitutePathFromTo(xbmcHost.TranslatePath(path), substitutions)
	}

	return SubstitutePathFromTo(path, substitutions)
}

// SubstitutePathFromTo replaces path with configured substitutions
func SubstitutePathFromTo(pathOrigin string, substitutions *map[string]string) string {
	if len(*substitutions) == 0 {
		return pathOrigin
	}

	for from, to := range *substitutions {
		if !strings.HasPrefix(pathOrigin, from) {
			continue
		}

		var dirs []string
		if windowsPathRegex.MatchString(pathOrigin) || networkPathRegex.MatchString(pathOrigin) {
			dirs = strings.Split(strings.Replace(pathOrigin, from, "", 1), WindowsPathSeparator)
		} else {
			dirs = strings.Split(strings.Replace(pathOrigin, from, "", 1), LinuxPathSeparator)
		}

		return filepath.Join(to, strings.Join(dirs, string(os.PathSeparator)))
	}

	return pathOrigin
}

// SubstitutePathToFrom replaces path with configured substitutions backwards
func SubstitutePathToFrom(pathOrigin string, substitutions *map[string]string, separator string) string {
	if len(*substitutions) == 0 {
		return pathOrigin
	}

	for from, to := range *substitutions {
		if !strings.HasPrefix(pathOrigin, to) {
			continue
		}

		// Make backward replacement, depending on the separator required for
		var dirs []string
		if windowsPathRegex.MatchString(pathOrigin) || networkPathRegex.MatchString(pathOrigin) {
			dirs = strings.Split(strings.Replace(pathOrigin, to, "", 1), WindowsPathSeparator)
		} else {
			dirs = strings.Split(strings.Replace(pathOrigin, to, "", 1), LinuxPathSeparator)
		}

		// Force backslash separator for clear Windows/Network path
		if windowsPathRegex.MatchString(from) || networkPathRegex.MatchString(from) {
			separator = WindowsPathSeparator
		}

		// Remove duplicate separators to make more "clean" path
		return strings.ReplaceAll(strings.Join(append([]string{from}, dirs...), separator), separator+separator, separator)
	}

	return pathOrigin
}

func IsValidPath(path string) bool {
	if IsNetworkPath(path) {
		return false
	}

	if runtime.GOOS != "windows" && strings.Contains(path, ":\\") {
		return false
	}

	return true
}

// EffectiveDir is checking argument path and returning closest folder.
func EffectiveDir(p string) string {
	if info, err := os.Stat(p); err == nil && info != nil && info.IsDir() {
		return p
	}

	return filepath.Dir(p)
}

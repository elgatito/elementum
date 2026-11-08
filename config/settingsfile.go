package config

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/elgatito/elementum/xbmc"
)

// SettingsFileVersion is the settings file format written by this binary; a newer file is rejected.
const SettingsFileVersion = 1

// SettingsPollInterval is how often the settings file is checked for changes.
const SettingsPollInterval = 2 * time.Second

type fileStamp struct {
	mod  time.Time
	size int64
}

var (
	fileMu      sync.Mutex // serializes file writes and the change stamp
	lastStamp   fileStamp
	appliedNorm []byte // the loaded file without write-back keys

	kodiMu      sync.Mutex
	kodiChecked time.Time
	kodiAlive   bool
)

func maskSecrets(s string) string {
	return privacyRegex.ReplaceAllString(s, `$1: "********"`)
}

func maskSecret(s string) string {
	if s == "" {
		return `""`
	}
	return "********"
}

// ApplyEnvConfigPath makes ELEMENTUM_CONFIG_PATH act as -configPath when the flag is empty.
// In the c-shared build Go copies the environment at load, so lookupEnv also asks libc, which sees the
// host's later putenv (Python's os.environ).
func ApplyEnvConfigPath() {
	if Args.ConfigPath == "" {
		Args.ConfigPath = lookupEnv("ELEMENTUM_CONFIG_PATH")
	}
}

// SettingsSource says where settings come from: "file" when -configPath is set, otherwise "kodi".
func SettingsSource() string {
	if Args.ConfigPath != "" {
		return "file"
	}
	return "kodi"
}

// KodiConnected reports whether the local Kodi add-on port accepts connections, cached for a few seconds.
// A plain dial (not Ping) keeps it quiet: Ping logs Critical on every failure. State changes are logged once.
func KodiConnected() bool {
	kodiMu.Lock()
	if time.Since(kodiChecked) < 5*time.Second {
		defer kodiMu.Unlock()
		return kodiAlive
	}
	kodiMu.Unlock()

	alive := false
	if h, _ := xbmc.GetLocalXBMCHost(); h != nil {
		if c, err := net.DialTimeout("tcp", net.JoinHostPort(h.Host, xbmc.XBMCExJSONRPCPort), time.Second); err == nil {
			c.Close()
			alive = true
		}
	}

	kodiMu.Lock()
	defer kodiMu.Unlock()
	if alive != kodiAlive || kodiChecked.IsZero() {
		log.Infof("Kodi connected: %v", alive)
	}
	kodiAlive, kodiChecked = alive, time.Now()
	return alive
}

func validateBundle(b *ConfigBundle) error {
	switch {
	case b.Version > SettingsFileVersion:
		return fmt.Errorf("unsupported settings version %d (this binary supports up to %d)", b.Version, SettingsFileVersion)
	case b.Info == nil || b.Info.ID == "":
		return errors.New("Info.ID is missing")
	case b.Info.Profile == "":
		return errors.New("Info.Profile is missing")
	case b.Platform == nil:
		return errors.New("Platform is missing")
	case b.Settings == nil:
		return errors.New("Settings is missing")
	}
	return nil
}

// writeFileAtomic writes next to path and renames, so readers never see a partial file. Mode is 0600.
func writeFileAtomic(path string, content []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	_, err = tmp.Write(content)
	if err == nil {
		err = tmp.Sync()
	}
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Chmod(name, 0600)
	}
	if err == nil {
		err = os.Rename(name, path)
	}
	if err != nil {
		os.Remove(name)
	}
	return err
}

func statStamp(path string) (fileStamp, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return fileStamp{}, err
	}
	return fileStamp{fi.ModTime(), fi.Size()}, nil
}

// loadSettingsFile reads the file (creating a default one when missing) and remembers its stamp.
func loadSettingsFile(path string) (*ConfigBundle, error) {
	fileMu.Lock()
	defer fileMu.Unlock()

	if _, err := os.Stat(path); os.IsNotExist(err) {
		if err := createDefaultSettingsFile(path); err != nil {
			return nil, fmt.Errorf("could not create default settings file: %w", err)
		}
	}
	st, _ := statStamp(path) // before the read, so a concurrent change is seen by the next poll
	b, err := importConfig(path)
	if err != nil {
		return nil, err
	}
	lastStamp = st
	appliedNorm = normalizeNoWriteBack(b)
	return b, nil
}

// isWriteBackKey tells the settings that config.SetSetting changes at runtime (tokens, expiries, usernames).
func isWriteBackKey(k string) bool {
	for _, id := range []string{"trakt", "opensubtitles", "simkl", "mdblist", "scrob"} {
		switch strings.TrimPrefix(k, id) {
		case "_token", "_refresh_token", "_token_expiry", "_username":
			if strings.HasPrefix(k, id) {
				return true
			}
		}
	}
	return false
}

// normalizeNoWriteBack serializes the bundle without the write-back keys, to compare two versions of the file.
func normalizeNoWriteBack(b *ConfigBundle) []byte {
	c := *b
	c.Settings = XbmcSettings{}
	for k, v := range b.Settings {
		if !isWriteBackKey(k) {
			c.Settings[k] = v
		}
	}
	out, _ := json.Marshal(c) // map keys are sorted, so the output is stable
	return out
}

// WriteBackOnlyChange applies the file to the live config when only write-back keys differ from the loaded one,
// so a token refresh does not restart services. It returns false when a full reload is needed.
func WriteBackOnlyChange() bool {
	if Args.ConfigPath == "" {
		return false
	}
	fileMu.Lock()
	defer fileMu.Unlock()
	b, err := importConfig(Args.ConfigPath)
	if err != nil || appliedNorm == nil || !bytes.Equal(normalizeNoWriteBack(b), appliedNorm) {
		return false
	}
	applyWriteBack(b.Settings, Get())
	log.Info("Settings file changed only in tokens, applied without a restart")
	return true
}

func applyWriteBack(s XbmcSettings, c *Configuration) {
	c.TraktUsername = s.ToString("trakt_username")
	c.TraktToken = s.ToString("trakt_token")
	c.TraktRefreshToken = s.ToString("trakt_refresh_token")
	c.TraktTokenExpiry = s.ToInt64("trakt_token_expiry")
	c.OSDBToken = s.ToString("opensubtitles_token")
	c.OSDBTokenExpiry = s.ToInt64("opensubtitles_token_expiry")
	if c.TrackerCredentials == nil {
		c.TrackerCredentials = map[string]*TrackerCredentials{}
	}
	for _, id := range []string{"simkl", "mdblist", "scrob"} {
		c.TrackerCredentials[id] = &TrackerCredentials{
			Token:        s.ToString(id + "_token"),
			RefreshToken: s.ToString(id + "_refresh_token"),
			TokenExpiry:  s.ToInt64(id + "_token_expiry"),
			Username:     s.ToString(id + "_username"),
		}
	}
}

// WatchSettingsFile polls the settings file and calls reload when it changed to a valid one.
// shortcut: polling (no fsnotify), see docs/settings-json.md; fine for one small file.
func WatchSettingsFile(path string, interval time.Duration, closer <-chan struct{}, reload func()) {
	tick := time.NewTicker(interval)
	defer tick.Stop()
	for {
		select {
		case <-closer:
			return
		case <-tick.C:
			if settingsFileChanged(path) {
				pollOnce(path, reload)
			}
		}
	}
}

func settingsFileChanged(path string) bool {
	fileMu.Lock()
	defer fileMu.Unlock()
	st, err := statStamp(path)
	if err != nil || st == lastStamp {
		return false
	}
	lastStamp = st
	return true
}

func pollOnce(path string, reload func()) {
	fileMu.Lock()
	_, err := importConfig(path)
	fileMu.Unlock()
	if err != nil {
		log.Errorf("Settings file changed but is not valid, keeping the previous settings: %s", err)
		return
	}
	if WriteBackOnlyChange() {
		return
	}
	log.Info("Settings file changed, reloading")
	reload()
}

// SetSetting stores one setting: in file mode into the settings file (atomically), and into Kodi when it is connected.
func SetSetting(key string, value any) {
	if Args.ConfigPath == "" {
		if h, _ := xbmc.GetLocalXBMCHost(); h != nil {
			h.SetSetting(key, value)
		}
		return
	}
	if err := updateSettingsFile(Args.ConfigPath, key, value); err != nil {
		log.Errorf("Could not save setting %s to %s: %s", key, Args.ConfigPath, err)
	}
	if KodiConnected() {
		if h, _ := xbmc.GetLocalXBMCHost(); h != nil {
			h.SetSetting(key, value)
		}
	}
}

func updateSettingsFile(path, key string, value any) error {
	fileMu.Lock()
	defer fileMu.Unlock()

	if detectConfigFormat(path) != JSONConfigFormat {
		return errors.New("write-back needs a .json settings file")
	}
	before, _ := statStamp(path)
	content, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	// Edit generically so every other key, including ones this binary does not know, survives.
	var doc map[string]any
	if err := json.Unmarshal(content, &doc); err != nil {
		return err
	}
	settings, _ := doc["Settings"].(map[string]any)
	if settings == nil {
		return errors.New("Settings is missing")
	}
	settings[key] = coerceLike(settings[key], value)
	out, err := json.MarshalIndent(doc, "", "    ")
	if err != nil {
		return err
	}
	if err := writeFileAtomic(path, out); err != nil {
		return err
	}
	// Our own write must not trigger a reload, but a foreign change since the stamp must still be seen.
	if before == lastStamp {
		lastStamp, _ = statStamp(path)
	}
	return nil
}

// coerceLike gives a string value the type the setting already has (Kodi passes everything as text).
func coerceLike(old, value any) any {
	str, ok := value.(string)
	if !ok {
		return value
	}
	switch old.(type) {
	case float64:
		if f, err := strconv.ParseFloat(str, 64); err == nil {
			return f
		}
	case bool:
		if b, err := strconv.ParseBool(str); err == nil {
			return b
		}
	}
	return value
}

// createDefaultSettingsFile writes defaults taken from the addon's settings.xml when found, else built-ins.
func createDefaultSettingsFile(path string) error {
	if detectConfigFormat(path) == "" {
		return fmt.Errorf("Configuration file %s is not of Yaml or Json format", path)
	}
	dir, err := filepath.Abs(filepath.Dir(path))
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	profile := firstNonEmpty(Args.ProfilePath, dir)
	temp := firstNonEmpty(Args.TempPath, filepath.Join(os.TempDir(), "elementum"))
	b := &ConfigBundle{
		Info: &xbmc.AddonInfo{
			ID: "plugin.video.elementum", Name: "Elementum", Version: "0.0.0",
			Path: Args.AddonPath, Profile: profile, Home: profile, TempPath: temp,
		},
		Platform: &xbmc.Platform{OS: runtime.GOOS, Arch: runtime.GOARCH},
		Settings: defaultSettings(Args.AddonPath, profile, temp),
		Language: "en", Region: "US",
	}
	log.Warningf("Settings file %s does not exist, creating a default one", path)
	return exportConfig(path, b)
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func defaultSettings(addonPath, profile, temp string) XbmcSettings {
	s := XbmcSettings{}
	{
		var r io.Reader = bytes.NewReader(embeddedSettingsXML)
		if f, err := os.Open(filepath.Join(addonPath, "resources", "settings.xml")); addonPath != "" && err == nil {
			defer f.Close()
			r = f
		}
		{
			d := xml.NewDecoder(r)
			for {
				tok, err := d.Token()
				if err != nil {
					break
				}
				if se, ok := tok.(xml.StartElement); ok && se.Name.Local == "setting" {
					var id, typ, def, opt string
					var hasDef bool
					for _, a := range se.Attr {
						switch a.Name.Local {
						case "id":
							id = a.Value
						case "type":
							typ = a.Value
						case "default":
							def, hasDef = a.Value, true
						case "option":
							opt = a.Value
						}
					}
					if id != "" && (hasDef || typ != "action") {
						s[id] = castDefault(typ, opt, def)
					}
				}
			}
		}
	}
	// special://temp does not exist without Kodi, so map it under the temp dir.
	for k, v := range s {
		if str, ok := v.(string); ok && strings.HasPrefix(str, "special://temp/") {
			s[k] = filepath.Join(filepath.Dir(temp), strings.TrimPrefix(str, "special://temp/"))
		}
	}
	for _, k := range []string{"library_path", "torrents_path"} {
		if s.ToString(k) == "" {
			s[k] = filepath.Join(filepath.Dir(temp), "elementum_"+strings.TrimSuffix(k, "_path"))
		}
	}
	if s.ToString("download_path") == "" {
		s["download_path"] = filepath.Join(profile, "downloads")
		os.MkdirAll(s.ToString("download_path"), 0777)
	}
	return s
}

// castDefault applies the same casts as fetchConfiguration to a settings.xml default.
func castDefault(typ, opt, def string) any {
	switch typ {
	case "enum", "number":
		v, _ := strconv.Atoi(def)
		return v
	case "slider":
		f, _ := strconv.ParseFloat(def, 32)
		if opt == "float" && f > 0 {
			return float32(f)
		}
		return int(f)
	case "bool":
		return def == "true"
	}
	return def
}

// embeddedSettingsXML is a snapshot of the addon's resources/settings.xml, the fallback source of default settings.
//
//go:embed default_settings.xml
var embeddedSettingsXML []byte

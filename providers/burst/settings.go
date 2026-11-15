package burst

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Settings is what the user sets for one provider, kept in <dir>/settings.json.
type Settings struct {
	Enabled  *bool  `json:"enabled,omitempty"` // nil: the definition's default
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`
	Passkey  string `json:"passkey,omitempty"`
	Alias    string `json:"alias,omitempty"` // a mirror: another domain, or URL, for the same site
	// Contains limits the provider to "movies" or "shows" searches; "" is both.
	Contains string `json:"contains,omitempty"`
	// Cookies ("name=value; other=value") from a browser signed in to the site, or past its bot check,
	// and the browser's User-Agent they belong to. With the site's login cookie among them no login is made.
	Cookies   string `json:"cookies,omitempty"`
	UserAgent string `json:"user_agent,omitempty"`
}

const settingsFile = "settings.json"

// file is settings.json: the general options and each provider's settings.
type file struct {
	General   General             `json:"general,omitempty"`
	Providers map[string]Settings `json:"providers,omitempty"`
}

func readFile(dir string) file {
	f := file{General: General{}, Providers: map[string]Settings{}}
	if dir == "" {
		return f
	}
	b, err := os.ReadFile(filepath.Join(dir, settingsFile))
	if err == nil {
		err = json.Unmarshal(b, &f)
	}
	if err != nil && !os.IsNotExist(err) {
		log.Errorf("Ignoring %s: %s", filepath.Join(dir, settingsFile), err)
	}
	if f.General == nil {
		f.General = General{}
	}
	if f.Providers == nil {
		f.Providers = map[string]Settings{}
	}
	return f
}

// LoadSettings reads the per-provider settings of dir; a missing or broken file gives none.
func LoadSettings(dir string) map[string]Settings { return readFile(dir).Providers }

// LoadGeneral reads the general options the user changed.
func LoadGeneral(dir string) General { return readFile(dir).General }

var settingsMu = make(chan struct{}, 1) // serializes read-change-write of the file

// change rewrites settings.json after edit. The file holds passwords, so it is private (0600).
func change(dir string, edit func(*file)) error {
	settingsMu <- struct{}{}
	defer func() { <-settingsMu }()
	f := readFile(dir)
	edit(&f)
	b, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp := filepath.Join(dir, settingsFile+".tmp")
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(dir, settingsFile))
}

// SaveSettings stores the settings of one provider.
func SaveSettings(dir, id string, s Settings) error {
	return change(dir, func(f *file) { f.Providers[id] = s })
}

// SaveOption stores one general option. The value must have the type of the option's default
// (bool, float64 or string); an unknown id or another type is refused.
func SaveOption(dir, id string, value any) error {
	for _, o := range Options {
		if o.ID != id {
			continue
		}
		if fmt.Sprintf("%T", value) != fmt.Sprintf("%T", o.Default) {
			return fmt.Errorf("%s must be a %T", id, o.Default)
		}
		return change(dir, func(f *file) { f.General[id] = value })
	}
	return fmt.Errorf("unknown option %s", id)
}

// Status tells whether a provider is searched, and why not.
type Status struct {
	ID, Name   string
	Private    bool
	Languages  []string
	Enabled    bool   // the user's choice, or the default
	Unusable   string // why it can't be searched even when enabled, "" when it can
	Username   string
	HasSecret  bool // a password or passkey is stored
	HasCookies bool
	Contains   string
	NeedsLogin bool // private, and no credentials are stored
}

func status(id string, d Definition, s Settings) Status {
	st := Status{ID: id, Name: d.str("name"), Private: d.flag("private"), Unusable: d.Skipped(), Username: s.Username,
		HasSecret: s.Password != "" || s.Passkey != "", HasCookies: s.Cookies != "", Contains: s.Contains}
	for _, l := range strings.Split(d.str("languages"), ",") {
		if l = strings.TrimSpace(l); l != "" {
			st.Languages = append(st.Languages, l)
		}
	}
	st.Enabled = (d.flag("predefined") || d.flag("custom")) && d.flag("enabled")
	if s.Enabled != nil {
		st.Enabled = *s.Enabled
	}
	// A private tracker is searched with a passkey, with the login its definition describes, or with the
	// cookies of a browser that is signed in (pasted, or from the cookie sync).
	st.NeedsLogin = st.Private && s.Passkey == "" && s.Cookies == "" && (s.Username == "" || s.Password == "") && !synced(d)
	return st
}

// List returns the status of every provider of dir, by name.
func List(dir string) []Status {
	defs, settings := Load(dir), LoadSettings(dir)
	out := make([]Status, 0, len(defs))
	for id, d := range defs {
		out = append(out, status(id, d, settings[id]))
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name) })
	return out
}

// Enabled returns the ids of the providers to search with a method ("" for any), sorted: those switched
// on (by the user, or by default: Burst's "predefined" set and the user's own providers) that can be
// searched now and are not limited to the other kind of content.
func Enabled(defs map[string]Definition, settings map[string]Settings, method string) []string {
	kind := map[string]string{"movie": "movies", "season": "shows", "episode": "shows", "anime": "shows"}[method]
	var ids []string
	for id, d := range defs {
		st := status(id, d, settings[id])
		if c := st.Contains; c != "" && method != "" && c != kind && !d.flag("custom") {
			continue
		}
		if st.Enabled && st.Unusable == "" && !st.NeedsLogin {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	return ids
}

// synced reports whether the cookie sync brought cookies for the provider's site.
func synced(d Definition) bool {
	h := ""
	if u, err := url.Parse(d.str("root_url")); err == nil {
		h = u.Hostname() // cookies know no ports
	}
	syncState.mu.Lock()
	defer syncState.mu.Unlock()
	for domain := range syncState.raw {
		if h != "" && domainMatches(h, domain) {
			return true
		}
	}
	return false
}

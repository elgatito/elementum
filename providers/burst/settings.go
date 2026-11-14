package burst

import (
	"encoding/json"
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
}

const settingsFile = "settings.json"

// LoadSettings reads the per-provider settings of dir; a missing or broken file gives none.
func LoadSettings(dir string) map[string]Settings {
	out := map[string]Settings{}
	if dir == "" {
		return out
	}
	b, err := os.ReadFile(filepath.Join(dir, settingsFile))
	if err == nil {
		err = json.Unmarshal(b, &out)
	}
	if err != nil && !os.IsNotExist(err) {
		log.Errorf("Ignoring %s: %s", filepath.Join(dir, settingsFile), err)
	}
	return out
}

var settingsMu = make(chan struct{}, 1) // serializes read-change-write of the file

// SaveSettings stores the settings of one provider. The file holds passwords, so it is private (0600).
func SaveSettings(dir, id string, s Settings) error {
	settingsMu <- struct{}{}
	defer func() { <-settingsMu }()
	all := LoadSettings(dir)
	all[id] = s
	b, err := json.MarshalIndent(all, "", "  ")
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

// Status tells whether a provider is searched, and why not.
type Status struct {
	ID, Name   string
	Private    bool
	Languages  []string
	Enabled    bool   // the user's choice, or the default
	Unusable   string // why it can't be searched even when enabled, "" when it can
	Username   string
	HasSecret  bool // a password or passkey is stored
	NeedsLogin bool // private, and no credentials are stored
}

func status(id string, d Definition, s Settings) Status {
	st := Status{ID: id, Name: d.str("name"), Private: d.flag("private"), Unusable: d.Skipped(), Username: s.Username,
		HasSecret: s.Password != "" || s.Passkey != ""}
	for _, l := range strings.Split(d.str("languages"), ",") {
		if l = strings.TrimSpace(l); l != "" {
			st.Languages = append(st.Languages, l)
		}
	}
	st.Enabled = (d.flag("predefined") || d.flag("custom")) && d.flag("enabled")
	if s.Enabled != nil {
		st.Enabled = *s.Enabled
	}
	// A private tracker is searched with a passkey, or with the login its definition describes.
	st.NeedsLogin = st.Private && s.Passkey == "" && (s.Username == "" || s.Password == "")
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

// Enabled returns the ids of the providers to search, sorted: those switched on (by the user, or by
// default: Burst's "predefined" set and the user's own providers) that can be searched now.
func Enabled(defs map[string]Definition, settings map[string]Settings) []string {
	var ids []string
	for id, d := range defs {
		if st := status(id, d, settings[id]); st.Enabled && st.Unusable == "" && !st.NeedsLogin {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	return ids
}

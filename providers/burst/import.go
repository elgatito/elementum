package burst

import (
	"encoding/xml"
	"os"
	"path/filepath"
	"strconv"
)

// CanImportBurst reports whether this is the first run of the built-in search (dir has no settings.json
// yet) and the Burst add-on has settings to take over.
func CanImportBurst(dir, burstSettings string) bool {
	if dir == "" {
		return false
	}
	if _, err := os.Stat(filepath.Join(dir, settingsFile)); err == nil {
		return false
	}
	fi, err := os.Stat(burstSettings)
	return err == nil && fi.Size() > 0
}

// SkipImport writes an empty settings.json, so the import is not offered again.
func SkipImport(dir string) error { return change(dir, func(*file) {}) }

// ImportBurst copies the settings of the Burst add-on (its settings.xml in Kodi's addon_data) into
// dir's settings.json, when that file does not exist yet: the first run of the built-in search.
// It reports how many providers came out enabled; nothing is imported, and no file written, when Burst
// has no settings.
// shortcut: Burst's own provider files (<its profile>/providers/*.json) hold Python expressions and
// are not imported: they have no builtin part.
func ImportBurst(dir, burstSettings string) (enabled int, imported bool) {
	if dir == "" {
		return 0, false
	}
	if _, err := os.Stat(filepath.Join(dir, settingsFile)); err == nil {
		return 0, false
	}
	f, err := os.Open(burstSettings)
	if err != nil {
		return 0, false
	}
	defer f.Close()
	var doc struct {
		Settings []struct {
			ID    string `xml:"id,attr"`
			Value string `xml:"value,attr"` // old Kodi versions
			Text  string `xml:",chardata"`
		} `xml:"setting"`
	}
	if err := xml.NewDecoder(f).Decode(&doc); err != nil {
		log.Warningf("Could not read Burst settings %s: %s", burstSettings, err)
		return 0, false
	}
	values := map[string]string{}
	for _, s := range doc.Settings {
		if values[s.ID] = s.Text; s.Text == "" {
			values[s.ID] = s.Value
		}
	}
	if len(values) == 0 {
		return 0, false
	}

	out := file{General: General{}, Providers: map[string]Settings{}}
	for _, o := range Options {
		v, ok := values[o.ID]
		if !ok {
			continue
		}
		switch o.Default.(type) {
		case bool:
			out.General[o.ID] = v == "true"
		case float64:
			if n, err := strconv.ParseFloat(v, 64); err == nil {
				out.General[o.ID] = n
			}
		default:
			out.General[o.ID] = v
		}
	}
	for id := range Load(dir) {
		use, ok := values["use_"+id]
		if !ok {
			continue
		}
		on := use == "true"
		set := Settings{Enabled: &on, Username: values[id+"_username"], Password: values[id+"_password"], Passkey: values[id+"_passkey"], Alias: values[id+"_alias"]}
		switch values[id+"_contains"] {
		case "1", "Movies":
			set.Contains = "movies"
		case "2", "Shows":
			set.Contains = "shows"
		}
		if on {
			enabled++
		}
		out.Providers[id] = set
	}
	if err := change(dir, func(f *file) { *f = out }); err != nil {
		log.Warningf("Could not save the imported Burst settings: %s", err)
		return 0, false
	}
	log.Infof("Imported the Burst add-on settings from %s: %d providers enabled", burstSettings, enabled)
	return enabled, true
}

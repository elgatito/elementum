package burst

import (
	"encoding/xml"
	"os"
	"strconv"
)

// HasBurstSettings reports whether the Burst add-on has settings to take over.
func HasBurstSettings(burstSettings string) bool {
	fi, err := os.Stat(burstSettings)
	return err == nil && fi.Size() > 0
}

// CanImportBurst reports whether to ask about importing the Burst add-on's settings: it has some, and
// the question was not answered yet. The answer is kept in settings.json, so settings made before the
// question existed do not stop it from being asked once.
func CanImportBurst(dir, burstSettings string) bool {
	return dir != "" && readFile(dir).BurstImport == "" && HasBurstSettings(burstSettings)
}

// SkipImport records that the import was declined, so it is not offered again. It can still be made
// from the settings.
func SkipImport(dir string) error { return change(dir, func(f *file) { f.BurstImport = "declined" }) }

// ImportBurst copies the settings of the Burst add-on (its settings.xml in Kodi's addon_data) into dir's
// settings.json. What Burst has replaces the same settings here; the rest (texts Burst leaves empty,
// cookies, providers it does not know) stays. It reports how many providers came out enabled; nothing is
// written when Burst has no settings.
// shortcut: Burst's own provider files (<its profile>/providers/*.json) hold Python expressions and
// are not imported: they have no builtin part.
func ImportBurst(dir, burstSettings string) (enabled int, imported bool) {
	if dir == "" {
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

	general, providers := General{}, map[string]Settings{}
	for _, o := range Options {
		v, ok := values[o.ID]
		if !ok {
			continue
		}
		switch o.Default.(type) {
		case bool:
			general[o.ID] = v == "true"
		case float64:
			if n, err := strconv.ParseFloat(v, 64); err == nil {
				general[o.ID] = n
			}
		default:
			if v != "" { // an empty text in Burst does not clear one set here, as a new GitHub token
				general[o.ID] = v
			}
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
		providers[id] = set
	}
	err = change(dir, func(f *file) {
		for k, v := range general {
			f.General[k] = v
		}
		for id, set := range providers {
			old := f.Providers[id]
			old.Enabled, old.Contains = set.Enabled, set.Contains
			for _, field := range []struct {
				to   *string
				from string
			}{
				{&old.Username, set.Username}, {&old.Password, set.Password}, {&old.Passkey, set.Passkey}, {&old.Alias, set.Alias},
			} {
				if field.from != "" {
					*field.to = field.from
				}
			}
			f.Providers[id] = old
		}
		f.BurstImport = "imported"
	})
	if err != nil {
		log.Warningf("Could not save the imported Burst settings: %s", err)
		return 0, false
	}
	log.Infof("Imported the Burst add-on settings from %s: %d providers enabled", burstSettings, enabled)
	return enabled, true
}

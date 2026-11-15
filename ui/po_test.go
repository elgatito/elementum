package ui

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/elgatito/elementum/config"
	"github.com/elgatito/elementum/xbmc"
)

func useFixture(t *testing.T) {
	t.Helper()
	old := stringsDir
	stringsDir = func() string { return "testdata" }
	t.Cleanup(func() { stringsDir = old })
}

func TestTranslate(t *testing.T) {
	useFixture(t)
	for _, c := range []struct{ lang, in, want string }{
		{"de", "LOCALIZE[30287];;Matrix", "Matrix zur Bibliothek hinzugefügt"},
		{"de-DE", "LOCALIZE[30204]", "Keine \"Ergebnisse\""},
		{"en", "LOCALIZE[30287];;Matrix", "Matrix added to the library"},
		{"de", "LOCALIZE[30684];;3;;4", "Library updated: 3 movies, 4 shows"}, // missing in de: English, multi-line msgstr
		{"de", "LOCALIZE[30999]", "Only in English"},
		{"xx", "LOCALIZE[30204]", "No results"},      // unknown language: English
		{"de", "LOCALIZE[39999]", "LOCALIZE[39999]"}, // unknown id: as given
		{"de", "plain text", "plain text"},
		{"de", "Failed: LOCALIZE[30204] now", "Failed: Keine \"Ergebnisse\" now"}, // inline, no args
		{"de", "LOCALIZE[30287];;LOCALIZE[30204]", "Keine \"Ergebnisse\" zur Bibliothek hinzugefügt"},
	} {
		if got := Translate(c.lang, c.in); got != c.want {
			t.Errorf("Translate(%q, %q) = %q, want %q", c.lang, c.in, got, c.want)
		}
	}
}

// Without the add-on's folder the strings built into the binary are used; an id nobody has stays as it is.
func TestTranslateNoStringsDir(t *testing.T) {
	old := stringsDir
	stringsDir = func() string { return "" }
	defer func() { stringsDir = old }()
	if got := Translate("de", "LOCALIZE[30204]"); got != "Kann keine Provider finden" {
		t.Errorf("got %q", got)
	}
	if got := Translate("de", "LOCALIZE[99999]"); got != "LOCALIZE[99999]" {
		t.Errorf("unknown id: %q", got)
	}
}

func TestText(t *testing.T) {
	dir := t.TempDir()
	po := filepath.Join(dir, "resource.language.de_de")
	if err := os.MkdirAll(po, 0o755); err != nil {
		t.Fatal(err)
	}
	content := "msgctxt \"#" + strconv.Itoa(textIDs["General"]) + "\"\nmsgid \"General\"\nmsgstr \"Allgemein\"\n"
	if err := os.WriteFile(filepath.Join(po, "strings.po"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	old := stringsDir
	stringsDir = func() string { return dir }
	t.Cleanup(func() { stringsDir = old })

	// the add-on's folder has German only: French comes from the strings built into the binary
	for lang, want := range map[string]string{"de": "Allgemein", "de-DE": "Allgemein", "fr": "Général"} {
		if got := Text(lang, "General"); got != want {
			t.Errorf("%s: %q, want %q", lang, got, want)
		}
	}
	// a text the strings do not have, and one that is not in the table at all, stay English
	if got := Text("de", "Filtering"); got != "Filtering" {
		t.Errorf("untranslated: %q", got)
	}
	if got := Text("de", "Not a known text"); got != "Not a known text" {
		t.Errorf("unknown: %q", got)
	}
	// without an add-on folder, as in a standalone run, every text comes from the binary
	stringsDir = func() string { return "" }
	for lang, want := range map[string]string{"de": "Filterung", "ru-RU": "Фильтрация", "pt": "Filtragem", "xx": "Filtering"} {
		if got := Text(lang, "Filtering"); got != want {
			t.Errorf("built-in %s: %q, want %q", lang, got, want)
		}
	}
	if got := Translate("de", "LOCALIZE[30214]"); got == "LOCALIZE[30214]" || got == "" {
		t.Errorf("an old id is not translated without the add-on: %q", got)
	}
	// every language of the add-on is in the binary, with the texts of the built-in search
	entries, _ := embeddedStrings.ReadDir("strings")
	if len(entries) < 22 {
		t.Fatalf("%d embedded languages", len(entries))
	}
	for _, e := range entries {
		lang := strings.TrimSuffix(e.Name(), ".po.gz")
		if m := embedded(lang); len(m) < 700 || m[textIDs["GitHub is connected"]] == "" {
			t.Errorf("%s: %d strings", lang, len(m))
		}
	}
}

func TestKodiTexts(t *testing.T) {
	c := config.Get()
	if c == nil {
		t.Skip("no config")
	}
	old := *c
	t.Cleanup(func() { *c = old })
	oldDir := stringsDir
	stringsDir = func() string { return "" }
	t.Cleanup(func() { stringsDir = oldDir })

	// metadata in German, Kodi in Russian: Kodi's texts are Russian
	c.Language, c.InterfaceLanguage = "de", "ru-ru"
	if got := xbmc.Localize("LOCALIZE[" + strconv.Itoa(textIDs["Filtering"]) + "]"); got != "Фильтрация" {
		t.Errorf("interface language: %q", got)
	}
	// an older settings file without the interface language: the metadata language
	c.InterfaceLanguage = ""
	if InterfaceLanguage() != "de" {
		t.Errorf("fallback: %q", InterfaceLanguage())
	}
	// a text for several hosts keeps its raw form, with the arguments
	raw := Localized("Burst settings imported, %d providers enabled", 3)
	if raw != "LOCALIZE["+strconv.Itoa(textIDs["Burst settings imported, %d providers enabled"])+"];;3" {
		t.Errorf("raw: %q", raw)
	}
	if got := Translate("de", raw); got != "Burst-Einstellungen importiert, 3 Anbieter aktiviert" {
		t.Errorf("raw translated: %q", got)
	}
	if got := Localized("Not known %d", 2); got != "Not known 2" {
		t.Errorf("unknown: %q", got)
	}
}

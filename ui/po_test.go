package ui

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
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

func TestTranslateNoStringsDir(t *testing.T) {
	old := stringsDir
	stringsDir = func() string { return "" }
	defer func() { stringsDir = old }()
	if got := Translate("de", "LOCALIZE[30204]"); got != "LOCALIZE[30204]" {
		t.Errorf("got %q", got)
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

	for lang, want := range map[string]string{"de": "Allgemein", "de-DE": "Allgemein", "fr": "General"} {
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
	// without an add-on folder, as in a standalone run
	stringsDir = func() string { return "" }
	if got := Text("de", "General"); got != "General" {
		t.Errorf("no strings: %q", got)
	}
}

package ui

import "testing"

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

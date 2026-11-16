package api

import (
	"strings"
	"testing"

	"github.com/elgatito/elementum/providers/burst"
)

func TestBuiltinMenuState(t *testing.T) {
	t.Cleanup(burst.Reload)
	d := builtinDialog{dir: t.TempDir(), lang: "en"}

	// Burst's defaults: the six public providers, no private one
	if got := d.enabledNames(false); !strings.Contains(got, "YTS") || !strings.Contains(got, "Knaben") {
		t.Errorf("public: %q", got)
	}
	if got := d.enabledNames(true); got != "" {
		t.Errorf("private: %q", got)
	}
	on := true
	if err := burst.SaveSettings(d.dir, "rutracker", burst.Settings{Enabled: &on}); err != nil {
		t.Fatal(err)
	}
	if got := d.enabledNames(true); !strings.Contains(got, "RuTracker") || !strings.Contains(got, "no login yet") {
		t.Errorf("private without a login: %q", got)
	}

	// switches that are on, values with their names
	if got := d.summary("Resolution"); !strings.Contains(got, "1080p") || strings.Contains(got, "240p") {
		t.Errorf("resolution: %q", got)
	}
	if got := d.summary("General"); !strings.Contains(got, "Maximum results per provider: 10") || !strings.Contains(got, "Balanced") {
		t.Errorf("general: %q", got)
	}
	_ = burst.SaveOption(d.dir, "cookie_sync_token", "SECRET")
	_ = burst.SaveOption(d.dir, "cookie_sync_user", "octocat")
	_ = burst.SelectGist(d.dir, "5610124e92f038bccfa1f399480969e4", "Browser cookies")
	if got := d.cookieSyncSummary(); !strings.Contains(got, "GitHub user: octocat") || !strings.Contains(got, "Active Gist: Browser cookies (5610124e92f038bccfa1f399480969e4)") || strings.Contains(got, "SECRET") {
		t.Errorf("cookie sync: %q", got)
	}
	if got := shorten([]string{strings.Repeat("x", 200)}); len([]rune(got)) != 110 {
		t.Errorf("shorten: %d", len([]rune(got)))
	}
}

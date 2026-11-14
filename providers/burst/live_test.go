package burst

import (
	"context"
	"os"
	"testing"
	"time"
)

// TestLive searches the real trackers: BURST_LIVE=1 go test -run TestLive -v ./providers/burst
// It is how a definition is checked against the site; it is skipped in normal runs.
func TestLive(t *testing.T) {
	if os.Getenv("BURST_LIVE") == "" {
		t.Skip("set BURST_LIVE=1 to search the real trackers")
	}
	t.Cleanup(Reload)
	defs := Load("")
	q := Query{Method: "movie", Title: "the matrix", Year: 1999, IMDBId: "tt0133093", TMDBId: 603,
		Titles: map[string]string{"original": "The Matrix", "en": "The Matrix"}, Languages: []string{"en"}}
	for _, id := range Enabled(defs, nil) {
		ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
		start := time.Now()
		results := searchProvider(ctx, id, defs[id], Settings{}, q)
		cancel()
		t.Logf("%-14s %2d results in %4.1fs", id, len(results), time.Since(start).Seconds())
		for i, r := range results {
			if i < 2 {
				t.Logf("    %q seeds=%d size=%q uri=%.60s", r.Name, r.Seeds, r.Size, r.URI)
			}
		}
	}
}

package providers

import (
	"testing"

	"github.com/goccy/go-json"

	"github.com/elgatito/elementum/bittorrent"
)

func TestDiagnosisCheck(t *testing.T) {
	body := `[
		{"uri": "magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567", "name": "Good"},
		{"uri": "magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567"},
		{"uri": "magnet:?dn=broken", "name": "Broken"},
		{"name": "No uri"}
	]`
	torrents := []*bittorrent.TorrentFile{}
	if err := json.Unmarshal([]byte(body), &torrents); err != nil {
		t.Fatal(err)
	}

	d := &Diagnosis{}
	d.check(torrents)

	if len(d.Results) != 4 || d.Usable != 2 {
		t.Fatalf("expected 4 results and 2 usable, got %d and %d", len(d.Results), d.Usable)
	}
	expected := []string{"Result has no 'name'", "Magnet link has no valid info hash", "Result has no 'uri'"}
	if len(d.Problems) != len(expected) {
		t.Fatalf("unexpected problems: %+v", d.Problems)
	}
	if d.Results[0].Problem != "" || d.Results[1].Problem != expected[0] || d.Results[3].Name != "No uri" {
		t.Errorf("unexpected results: %+v %+v %+v", d.Results[0], d.Results[1], d.Results[3])
	}
	for i, text := range expected {
		if d.Problems[i].Text != text || d.Problems[i].Count != 1 {
			t.Errorf("problem %d: %+v, expected %q", i, d.Problems[i], text)
		}
	}
}

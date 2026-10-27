package library

import (
	"encoding/xml"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/elgatito/elementum/tmdb"
	"github.com/elgatito/elementum/xbmc"
)

func TestWriteShowNFO(t *testing.T) {
	defer func(v int) { xbmc.KodiVersion = v }(xbmc.KodiVersion)

	cases := []struct {
		kodi  int
		tvdb  any
		guide string
	}{
		{18, 436457.0, ""},
		{21, 436457.0, `<episodeguide>{"imdb":"tt22202452","tmdb":"225171","tvdb":"436457"}</episodeguide>`},
		{21, nil, `<episodeguide>{"imdb":"tt22202452","tmdb":"225171"}</episodeguide>`},
	}
	for _, c := range cases {
		xbmc.KodiVersion = c.kodi
		p := filepath.Join(t.TempDir(), "tvshow.nfo")
		s := &tmdb.Show{ExternalIDs: &tmdb.ExternalIDs{IMDBId: "tt22202452", TVDBID: c.tvdb}}
		s.ID = 225171
		if err := writeShowNFO(s, p); err != nil {
			t.Fatal(err)
		}
		b, _ := os.ReadFile(p)
		out := string(b)

		if c.guide == "" && strings.Contains(out, "episodeguide") || !strings.Contains(out, c.guide) {
			t.Errorf("kodi %d: unexpected episodeguide in:\n%s", c.kodi, out)
		}
		if c.tvdb == nil && (strings.Contains(out, "<nil>") || strings.Contains(out, "thetvdb")) {
			t.Errorf("empty tvdb leaked into:\n%s", out)
		}
		xmlPart := out[:strings.Index(out, "</tvshow>")+len("</tvshow>")]
		if err := xml.Unmarshal([]byte(xmlPart), new(struct{})); err != nil {
			t.Errorf("invalid xml: %s\n%s", err, xmlPart)
		}
	}
}

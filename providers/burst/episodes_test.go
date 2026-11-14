package burst

import "testing"

// The cases of Burst's tests/test_episodes.py.
func TestNameMatches(t *testing.T) {
	check := func(name string, season, episode int, want bool) {
		t.Helper()
		if got := nameMatches(name, season, episode); got != want {
			t.Errorf("nameMatches(%q, %d, %d) = %v, want %v", name, season, episode, got, want)
		}
	}
	for _, name := range []string{
		"Show.S02E09.1080p.WEB.h264", "Show S02 E09 720p", "Show S02 EP09 720p", "Show.S02.Episode.9.720p", "Show_S02E09_720p", "Show 2x09 HDTV",
		"Show S02E09v2 720p", "Show S02E08-E10 1080p", "Show S02E08-S02E10 1080p", "Show S01E01-S04E10 Complete", "Show S02E08E09E10 720p",
		"Show S02E08-09-10 720p", "Show S02E08 & E09 720p", "Show 2x08-2x10 HDTV", "Show 2x08-10 HDTV", "Show S02E08–E10 1080p", "Show S02 1080p WEB",
		"Show Season 2 Complete", "Show Season 2 Episode 9", "Show Season 2 Episodes 1-10", "Show S02 1080p E09", "Show S01-S06 1080p", "Show Season 1-6",
		"Show Seasons 1 to 4", "Show S01–S04 1080p", "Show Season 1—4", "Show S01 through S04", "Show S01 thru S04", "Show S01 & S02 1080p",
		"Show Seasons 1, 2 and 4", "Show S02-09 Complete", "Show Complete Series 1080p", "Show 1920x1080 x264", "Шоу Сезон 2 1080p",
		"Шоу / Сезон: 2 / Серии: 1-10 из 10", "Шоу 2 сезон 1080p", "Шоу 2-й сезон", "Шоу 1-4 сезоны", "Show Saison 2", "Show Temporada 2", "Show Staffel 2",
		"Show 2х09 HDTV", "Show E09 720p", "Show E05 720p", "Show Ep 125 1080p", "Show Episode 125",
	} {
		check(name, 2, 9, true)
	}
	for _, name := range []string{
		"Show.S02E10.1080p.WEB.h264", "Show S03E09 720p", "Show 2x10 HDTV", "Show 3x09 HDTV", "Show S02E05-E08 1080p", "Show S02E10-S02E12 1080p",
		"Show S01E01-S02E08 1080p", "Show S02E05E06E07 720p", "Show Season 2 Episode 5", "Show Season 2 Ep 5", "Show Season 2 - E05", "Show S02 1080p E05",
		"Show Season 2 Episodes 1-8", "Show Season 1", "Show S04 Complete 1080p", "Show S03-S04 1080p", "Show S01 & S04 1080p", "Show S01 and S03",
		"Шоу Сезон 1 1080p", "Шоу / Сезон: 2 / Серии: 1-8 из 10", "Шоу 1 сезон", "Show Saison 1", "Show Temporada 1", "Show Staffel 1",
	} {
		check(name, 2, 9, false)
	}
	for _, tag := range []string{"720p", "480p", "576p", "360p", "240p", "1080p", "2160p", "10bit"} {
		name := "Show S02E09-" + tag + " WEB"
		check(name, 2, 9, true)
		check(name, 2, 10, false)
		check(name, 2, 100, false)
	}
	for _, name := range []string{"Show S02E08E09E10 720p", "Show S02E08-09-10 720p", "Show S02E08-E09-E10 720p"} {
		check(name, 2, 8, true)
		check(name, 2, 10, true)
		check(name, 2, 7, false)
		check(name, 2, 11, false)
	}
	check("Show S01 & S04 1080p", 2, -1, false)
	check("Show S01 & S04 1080p", 3, -1, false)
	check("Show S01 & S04 1080p", 4, -1, true)
	check("Show S02-09 Complete", 2, 9, true)
	check("Show S02-09 Complete", 5, 1, true)
	check("Show S02-09 Complete", 9, -1, true)
	check("Show S02-09 Complete", 1, 9, false)
	check("Show S02-09 Complete", 10, 1, false)
	check("Show 2х09 HDTV", 3, 9, false)
	for name, kept := range map[string]bool{
		"Show S02 1080p": true, "Show S02E05 720p": true, "Show 2x05 HDTV": true, "Show S01-S04 1080p": true, "Show S01E01-S04E10 Complete": true,
		"Show Season 1 through 4": true, "Шоу Сезон 2": true, "Show Complete Series": true, "Show E05 720p": true,
		"Show S03 1080p": false, "Show S03E01 720p": false, "Show S01 & S04 1080p": false, "Show S03-S04 1080p": false, "Шоу Сезон 3": false,
		"Show Saison 1": false, "Шоу Сезон 1": false, "Show Temporada 1": false, "Show Staffel 1": false,
	} {
		check(name, 2, -1, kept)
	}
	check("Show S00E01 Special", 0, 1, true)
	check("Show S00E02 Special", 0, 1, false)
	check("Show S01E01 720p", 0, 1, false)
	check("Show Specials 720p", 0, 1, true)
	check("Show S00 720p", 0, -1, true)
	check("Show S01 720p", 0, -1, false)
	check("Show S01E00 Pilot", 1, 0, true)
	check("Show S01E01 720p", 1, 0, false)
}

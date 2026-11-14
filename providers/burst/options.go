package burst

import "strings"

// Option is one general setting of the search, with Burst's id and default. The list drives the
// settings file, the API and the Kodi menu.
type Option struct {
	ID, Group, Label string
	Default          any // bool, float64 or string
}

// Options are Burst's own settings that the built-in search keeps.
var Options = []Option{
	{"max_results", "General", "Results per provider", 10.0},
	{"sort_by", "General", "Keep the best results by (0 resolution, 1 seeds, 2 size, 3 balanced)", 3.0},
	{"min_size", "General", "Minimum size, GB", 0.0},
	{"max_size", "General", "Maximum size, GB", 100.0},
	{"separate_sizes", "General", "Separate sizes for movies, seasons and episodes", false},
	{"min_size_movies", "General", "Movies: minimum size, GB", 0.0},
	{"max_size_movies", "General", "Movies: maximum size, GB", 100.0},
	{"min_size_seasons", "General", "Seasons: minimum size, GB", 0.0},
	{"max_size_seasons", "General", "Seasons: maximum size, GB", 100.0},
	{"min_size_episodes", "General", "Episodes: minimum size, GB", 0.0},
	{"max_size_episodes", "General", "Episodes: maximum size, GB", 100.0},

	{"require_resolution", "Filtering", "Drop results of a resolution that is switched off", true},
	{"require_release_type", "Filtering", "Drop results by release type", true},
	{"require_size", "Filtering", "Drop results outside the size range", true},
	{"require_keywords", "Filtering", "Apply the required keywords", true},
	{"require_episode", "Filtering", "Drop results for another season or episode", false},
	{"allow_noseeds", "Filtering", "Keep results without seeds", false},
	{"filter_quotes", "Filtering", "Remove apostrophes from queries", false},

	{"filter_4k", "Resolutions", "4K/2160p", true},
	{"filter_2k", "Resolutions", "2K/1440p", true},
	{"filter_1080p", "Resolutions", "1080p", true},
	{"filter_720p", "Resolutions", "720p", true},
	{"filter_480p", "Resolutions", "480p", true},
	{"filter_240p", "Resolutions", "240p", false},
	{"filter_music", "Resolutions", "Music", false},

	{"filter_brrip", "Release types", "BRRip / BDRip / BluRay", true},
	{"filter_webdl", "Release types", "WEB-DL / WEBRip", true},
	{"filter_hdrip", "Release types", "HDRip", true},
	{"filter_hdtv", "Release types", "HDTV", true},
	{"filter_dvd", "Release types", "DVD / DVDRip", true},
	{"filter_dvdscr", "Release types", "DVDScr", true},
	{"filter_screener", "Release types", "Screener", true},
	{"filter_3d", "Release types", "3D", false},
	{"filter_telesync", "Release types", "TeleSync / TS / TeleCine", false},
	{"filter_cam", "Release types", "CAM", false},
	{"filter_tvrip", "Release types", "TVRip / SATRip", true},
	{"filter_iptvrip", "Release types", "IPTVRip", true},
	{"filter_vhsrip", "Release types", "VHSRip", false},
	{"filter_trailer", "Release types", "Trailer", false},
	{"filter_workprint", "Release types", "Workprint", false},
	{"filter_line", "Release types", "Line", false},

	{"language_priority", "Languages", "Preferred languages, most wanted first (codes, comma separated; kodi is Elementum's language)", "kodi"},
	{"kodi_language", "Languages", "Search with the title in Elementum's language", true},
	{"language_exceptions", "Languages", "Providers that keep their own title language (ids, comma separated)", ""},
	{"overwrite_anime_original_title", "Languages", "Anime original title (0 as is, 1 Romaji, 2 English)", 1.0},

	{"additional_filters", "Keywords", "Use the keyword lists below", false},
	{"accept", "Keywords", "Also accept (comma separated)", ""},
	{"block", "Keywords", "Block (comma separated)", ""},
	{"require", "Keywords", "Require (comma separated)", ""},
}

// General holds the values the user changed; the rest are the defaults of Options.
type General map[string]any

func (g General) value(id string) any {
	if v, ok := g[id]; ok {
		return v
	}
	for _, o := range Options {
		if o.ID == id {
			return o.Default
		}
	}
	return nil
}

func (g General) on(id string) bool     { b, _ := g.value(id).(bool); return b }
func (g General) num(id string) float64 { f, _ := g.value(id).(float64); return f }
func (g General) text(id string) string {
	s, _ := g.value(id).(string)
	return strings.ToLower(strings.TrimSpace(s))
}

// Get returns the value of an option: the user's, or its default.
func (g General) Get(id string) any { return g.value(id) }

package burst

import "strings"

// Option is one general setting of the search, with Burst's id and default. The list drives the
// settings file, the API and the Kodi menu. Group, Prefix and Label are English texts with translations
// in the add-on's strings (see ui.Text); the names of resolutions and release types are not translated.
type Option struct {
	ID, Group, Label string
	Default          any      // bool, float64 or string
	Prefix           string   // what the label is about, when several options share it ("Movies")
	Choices          []string // the meaning of a number option's values, from 0
}

// Options are Burst's own settings that the built-in search keeps.
var Options = []Option{
	{ID: "max_results", Group: "General", Label: "Maximum results per provider", Default: 10.0},
	{ID: "sort_by", Group: "General", Label: "Sort returned results by", Default: 3.0, Choices: []string{"Resolution", "Seeds", "Size", "Balanced"}},
	{ID: "min_size", Group: "General", Label: "Minimum size (GB)", Default: 0.0},
	{ID: "max_size", Group: "General", Label: "Maximum size (GB)", Default: 100.0},
	{ID: "separate_sizes", Group: "General", Label: "Use separate size range for movies, seasons and episodes", Default: false},
	{ID: "min_size_movies", Group: "General", Prefix: "Movies", Label: "Minimum size (GB)", Default: 0.0},
	{ID: "max_size_movies", Group: "General", Prefix: "Movies", Label: "Maximum size (GB)", Default: 100.0},
	{ID: "min_size_seasons", Group: "General", Prefix: "Seasons", Label: "Minimum size (GB)", Default: 0.0},
	{ID: "max_size_seasons", Group: "General", Prefix: "Seasons", Label: "Maximum size (GB)", Default: 100.0},
	{ID: "min_size_episodes", Group: "General", Prefix: "Episodes", Label: "Minimum size (GB)", Default: 0.0},
	{ID: "max_size_episodes", Group: "General", Prefix: "Episodes", Label: "Maximum size (GB)", Default: 100.0},

	// Burst's "Require matching of these settings"
	{ID: "require_resolution", Group: "Filtering", Label: "Resolution", Default: true},
	{ID: "require_release_type", Group: "Filtering", Label: "Release type", Default: true},
	{ID: "require_size", Group: "Filtering", Label: "Size", Default: true},
	{ID: "require_keywords", Group: "Filtering", Label: "Required keywords", Default: true},
	{ID: "require_episode", Group: "Filtering", Label: "Season and episode", Default: false},
	{ID: "allow_noseeds", Group: "Filtering", Label: "Allow results without seeds", Default: false},
	{ID: "filter_quotes", Group: "Filtering", Label: "Remove quotes from search query", Default: false},

	{ID: "filter_4k", Group: "Resolution", Label: "4K/2160p", Default: true},
	{ID: "filter_2k", Group: "Resolution", Label: "2K/1440p", Default: true},
	{ID: "filter_1080p", Group: "Resolution", Label: "1080p", Default: true},
	{ID: "filter_720p", Group: "Resolution", Label: "720p", Default: true},
	{ID: "filter_480p", Group: "Resolution", Label: "480p", Default: true},
	{ID: "filter_240p", Group: "Resolution", Label: "240p", Default: false},
	{ID: "filter_music", Group: "Resolution", Label: "MP3 / FLAC", Default: false},

	{ID: "filter_brrip", Group: "Release type", Label: "BRRip / BDRip / BluRay", Default: true},
	{ID: "filter_webdl", Group: "Release type", Label: "WEB-DL / WEBRip", Default: true},
	{ID: "filter_hdrip", Group: "Release type", Label: "HDRip", Default: true},
	{ID: "filter_hdtv", Group: "Release type", Label: "HDTV", Default: true},
	{ID: "filter_dvd", Group: "Release type", Label: "DVD / DVDRip", Default: true},
	{ID: "filter_dvdscr", Group: "Release type", Label: "DVDScr", Default: true},
	{ID: "filter_screener", Group: "Release type", Label: "Screener", Default: true},
	{ID: "filter_3d", Group: "Release type", Label: "3D", Default: false},
	{ID: "filter_telesync", Group: "Release type", Label: "TeleSync / TS / TeleCine", Default: false},
	{ID: "filter_cam", Group: "Release type", Label: "CAM", Default: false},
	{ID: "filter_tvrip", Group: "Release type", Label: "TVRip / SATRip", Default: true},
	{ID: "filter_iptvrip", Group: "Release type", Label: "IPTVRip", Default: true},
	{ID: "filter_vhsrip", Group: "Release type", Label: "VHSRip", Default: false},
	{ID: "filter_trailer", Group: "Release type", Label: "Trailer", Default: false},
	{ID: "filter_workprint", Group: "Release type", Label: "Workprint", Default: false},
	{ID: "filter_line", Group: "Release type", Label: "Line", Default: false},

	{ID: "language_priority", Group: "Languages", Label: "Language priority", Default: "kodi"},
	{ID: "kodi_language", Group: "Languages", Label: "Use language preference from Kodi when a translation is available", Default: true},
	{ID: "language_exceptions", Group: "Languages", Label: "Exceptions", Default: ""},
	{ID: "overwrite_anime_original_title", Group: "Languages", Label: "Set 'original' title of Anime to:", Default: 1.0, Choices: []string{"Original", "Romanization of Japanese", "English"}},

	{ID: "additional_filters", Group: "Keywords", Label: "Additional keyword filters (comma-separated)", Default: false},
	{ID: "accept", Group: "Keywords", Label: "Accept", Default: ""},
	{ID: "block", Group: "Keywords", Label: "Block", Default: ""},
	{ID: "require", Group: "Keywords", Label: "Require", Default: ""},

	{ID: "cookie_sync_enabled", Group: "Cookie sync", Label: "Enable Cookie sync", Default: false},
	{ID: "cookie_sync_token", Group: "Cookie sync", Label: "GitHub token", Default: ""},
	{ID: "cookie_sync_user", Group: "Cookie sync", Label: "GitHub user", Default: ""},
	{ID: "cookie_sync_password", Group: "Cookie sync", Label: "Encryption password (optional)", Default: ""},
	{ID: "cookie_sync_info", Group: "Cookie sync", Label: "Active Gist", Default: ""}, // its description, as Burst keeps it
	{ID: "cookie_sync_gist_id", Group: "Cookie sync", Label: "Gist ID", Default: ""},
	{ID: "cookie_sync_filename", Group: "Cookie sync", Label: "File name (optional)", Default: "kevast-gist-default.json"},
}

// Secret tells the options whose value must not be shown or returned.
func (o Option) Secret() bool { return o.ID == "cookie_sync_token" || o.ID == "cookie_sync_password" }

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

// raw is text without the trimming and lower-casing of text, for tokens and passwords.
func (g General) raw(id string) string { s, _ := g.value(id).(string); return s }

// Get returns the value of an option: the user's, or its default.
func (g General) Get(id string) any { return g.value(id) }

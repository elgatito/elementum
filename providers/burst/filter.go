package burst

import (
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/dlclark/regexp2"
)

// Burst's result filters (filtering.py): what a name must and must not contain, by the general options.

// The patterns of each resolution, lowest first: the last one found in a name is its resolution.
var resolutions = []struct {
	id       string
	patterns []string
}{
	{"filter_240p", []string{`240[pр]`, `vhs\-?rip`}},
	{"filter_480p", []string{`480[pр]`, `xvid|dvd|dvdrip|hdtv|web\-(dl)?rip|iptv|sat\-?rip|tv\-?rip`}},
	{"filter_720p", []string{`720[pр]|1280x720`, `hd720p?|hd\-?rip|b[rd]rip`}},
	{"filter_1080p", []string{`1080[piр]|1920x1080`, `hd1080p?|fullhd|fhd|blu\W*ray|bd\W*remux`}},
	{"filter_2k", []string{`1440[pр]`, `2k`}},
	{"filter_4k", []string{`4k|2160[pр]|uhd`, `4k|hd4k`}},
	{"filter_music", []string{`mp3|flac|alac|ost|sound\-?track`}},
}

var releaseTypes = map[string]string{
	"filter_brrip": `brrip|bd\-?rip|blu\-?ray|bd\-?remux`, "filter_webdl": `web_?\-?dl|web\-?rip|dl\-?rip|yts`, "filter_hdrip": `hd\-?rip`,
	"filter_hdtv": `hd\-?tv`, "filter_dvd": `dvd|dvd\-?rip|vcd\-?rip`, "filter_dvdscr": `dvd\-?scr`, "filter_screener": `screener|scr`,
	"filter_3d": `3d`, "filter_telesync": `telesync|ts|telecine|tc|hdts`, "filter_cam": `cam|cam\-?rip|hd\-?cam`, "filter_tvrip": `tv\-?rip|sat\-?rip|dvb`,
	"filter_iptvrip": `iptv\-?rip`, "filter_vhsrip": `vhs\-?rip`, "filter_trailer": `trailer|трейлер|тизер`, "filter_workprint": `workprint`, "filter_line": `line`,
}

// filter checks the results of one search.
type filter struct {
	opts         General
	q            Query
	allowedRes   map[string]bool
	allRes       bool     // every resolution is allowed: nothing to check
	allow, deny  []string // release patterns
	require      []string
	minGB, maxGB float64
}

var listRe = regexp.MustCompile(`,\s?`)

func newFilter(opts General, q Query) *filter {
	f := &filter{opts: opts, q: q, allowedRes: map[string]bool{}, allRes: true}
	for _, r := range resolutions {
		if opts.on(r.id) {
			f.allowedRes[r.id] = true
			f.allow = append(f.allow, r.patterns...)
		} else {
			f.allRes = false
		}
	}
	ids := make([]string, 0, len(releaseTypes))
	for id := range releaseTypes {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		if opts.on(id) {
			f.allow = append(f.allow, releaseTypes[id])
		} else {
			f.deny = append(f.deny, releaseTypes[id])
		}
	}
	if opts.on("additional_filters") {
		list := func(id string) []string {
			if s := opts.text(id); s != "" {
				return listRe.Split(s, -1)
			}
			return nil
		}
		f.allow, f.deny, f.require = append(f.allow, list("accept")...), append(f.deny, list("block")...), list("require")
	}
	f.minGB, f.maxGB = opts.num("min_size"), opts.num("max_size")
	if kind := map[string]string{"movie": "movies", "season": "seasons", "episode": "episodes", "anime": "episodes"}[q.Method]; kind != "" && opts.on("separate_sizes") {
		f.minGB, f.maxGB = opts.num("min_size_"+kind), opts.num("max_size_"+kind)
	}
	if f.minGB > f.maxGB {
		f.minGB = f.maxGB - 1
	}
	return f
}

var keysRx sync.Map // joined patterns -> *regexp2.Regexp

// included reports whether a name has one of the patterns as a word of its own.
func included(name string, patterns []string) bool {
	if len(patterns) == 0 {
		return false
	}
	key := strings.Join(patterns, "\x00")
	cached, ok := keysRx.Load(key)
	if !ok {
		// regexp2, as in episodes.go: \W must not take Cyrillic letters for separators, as Go's regexp does
		re, err := regexp2.Compile(`\W+((?:`+strings.Join(patterns, ")|(?:")+`))\W+`, regexp2.None)
		if err != nil {
			log.Errorf("Bad filter keywords %q: %s", patterns, err)
			re = nil
		}
		cached, _ = keysRx.LoadOrStore(key, re)
	}
	re, _ := cached.(*regexp2.Regexp)
	if re == nil {
		return false
	}
	found, _ := re.MatchString(" " + strings.ToLower(name) + " ")
	return found
}

// resolution returns the resolution of a name (480p when it names none) and its rank, lowest 0.
func resolution(name string) (string, int) {
	id, rank := "filter_480p", 0
	for i, r := range resolutions {
		if included(name, r.patterns) {
			id, rank = r.id, i
		}
	}
	return id, rank
}

// verify reports whether a result passes the filters, and why not. title is the searched text for the
// definitions that ask for a name check ("" otherwise).
func (f *filter) verify(r Result, title string) (bool, string) {
	if r.Name == "" {
		return false, "empty name"
	}
	name := normalize(r.Name)
	if !f.allRes && f.opts.on("require_resolution") {
		if id, _ := resolution(name); !f.allowedRes[id] {
			return false, "resolution not allowed (" + id + ")"
		}
	}
	for _, word := range strings.Fields(title) {
		if !strings.Contains(name, word) {
			return false, "name mismatch"
		}
	}
	if f.opts.on("require_keywords") {
		for _, required := range f.require {
			if !included(name, []string{required}) {
				return false, "missing required keyword"
			}
		}
	}
	if f.opts.on("require_release_type") {
		if !included(name, f.allow) {
			return false, "missing release type keyword"
		}
		if included(name, f.deny) {
			return false, "blocked by release type keyword"
		}
	}
	if r.Size != "" && f.opts.on("require_size") {
		if b := sizeBytes(r.Size); b < f.minGB*1e9 || b > f.maxGB*1e9 {
			return false, "size out of range (" + r.Size + ")"
		}
	}
	if f.opts.on("require_episode") && (f.q.Method == "season" || f.q.Method == "episode") {
		episode := -1
		if f.q.Method == "episode" {
			episode = f.q.Episode
		}
		if !nameMatches(name, f.q.Season, episode) {
			return false, "other season or episode"
		}
	}
	return true, ""
}

var floatRe = regexp.MustCompile(`[\d.]+`)

// sizeBytes reads "1.4 GB", "700 MB" or "1,4 ГБ"-like sizes with a K, M, G or T; 0 when it can't.
func sizeBytes(size string) float64 {
	s := strings.ToUpper(size)
	// "1,234.5" has a thousands comma, "1,4" and "1.234,5" a decimal one
	comma, point := strings.Index(s, ","), strings.Index(s, ".")
	switch {
	case comma > 0 && point > 0 && comma < point:
		s = strings.ReplaceAll(s, ",", "")
	case comma > 0 && point > 0:
		s = strings.ReplaceAll(strings.ReplaceAll(s, ".", ""), ",", ".")
	case comma > 0:
		s = strings.ReplaceAll(s, ",", ".")
	}
	n, _ := strconv.ParseFloat(floatRe.FindString(s), 64)
	for unit, factor := range map[string]float64{"K": 1e3, "M": 1e6, "G": 1e9, "T": 1e12} {
		if strings.Contains(s, unit) {
			n *= factor
		}
	}
	return n
}

// best keeps a provider's results up to the max_results option, ordered by sort_by.
func (f *filter) best(results []Result) []Result {
	rank := func(r Result) float64 {
		_, res := resolution(normalize(r.Name))
		switch int(f.opts.num("sort_by")) {
		case 0:
			return float64(res)
		case 1:
			return float64(r.Seeds)
		case 2:
			return sizeBytes(r.Size)
		}
		return float64((r.Seeds + 1) * 3 * (res + 1)) // balanced
	}
	sort.SliceStable(results, func(i, j int) bool { return rank(results[i]) > rank(results[j]) })
	if limit := int(f.opts.num("max_results")); limit > 0 && len(results) > limit {
		results = results[:limit]
	}
	return results
}

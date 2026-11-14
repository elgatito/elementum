package burst

import (
	"fmt"
	"html"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"golang.org/x/text/encoding/htmlindex"
	"golang.org/x/text/unicode/norm"
)

// Query is what to search for: the fields of Elementum's search objects that definitions can use.
type Query struct {
	Method string // general, movie, season, episode or anime

	Title  string
	Titles map[string]string // by language, plus "original" and "source"
	Year   int
	Years  map[string]int // by country

	Season, Episode, AbsoluteNumber int

	IMDBId                     string
	TMDBId, ShowTMDBId, TVDBId int

	// SkipAuth searches private trackers without signing in first (a background search).
	SkipAuth bool

	// Languages are the preferred languages, most wanted first, for {languages} in a search URL.
	Languages []string
}

// search is one request to try: the query, the definition's extra for it, and its priority.
// Priorities above 1 are fallbacks, tried only while there are no results.
type search struct {
	query, extra string
	priority     int
}

var keywordRe = regexp.MustCompile(`{(.*?)}`)

// itemType is the prefix of a method's keys in a definition, and the key of its URL suffix.
func itemType(method string) (prefix, queryKey string) {
	switch method {
	case "movie":
		return "movie", "movie_query"
	case "season":
		return "season", "season_query"
	case "episode":
		return "tv", "show_query"
	case "anime":
		return "anime", "anime_query"
	}
	return "general", "general_query"
}

// searches lists the queries of a definition for q, the main ones before the fallbacks.
func searches(d Definition, q Query) []search {
	prefix, _ := itemType(q.Method)
	years := q.differentYears()
	var out []search
	priority := 1
	for _, kind := range []string{"", "_fallback"} {
		for _, n := range []string{"", "2", "3", "4"} {
			template := d.str(prefix + "_keywords" + kind + n)
			if template == "" {
				continue
			}
			for _, query := range splitPerYear(splitPerLanguage(template, prefix), years) {
				if kind != "" {
					priority++
				}
				out = append(out, search{query, d.str(prefix + "_extra" + kind + n), priority})
			}
		}
	}
	return out
}

// splitPerLanguage turns {title:lang1:lang2} into one query per language, and a last one with {title}.
func splitPerLanguage(template, prefix string) []string {
	var out []string
	for _, m := range keywordRe.FindAllStringSubmatch(template, -1) {
		keyword := strings.ToLower(m[1])
		if !strings.Contains(keyword, "title") || !strings.Contains(keyword, ":") {
			continue
		}
		if prefix == "general" { // a typed query has no translations
			return []string{strings.ReplaceAll(template, "{"+keyword+"}", "{title}")}
		}
		for _, lang := range strings.Split(keyword, ":")[1:] {
			out = append(out, strings.ReplaceAll(template, "{"+keyword+"}", "{title:"+lang+"}"))
		}
		out = append(out, strings.ReplaceAll(template, "{"+keyword+"}", "{title}"))
	}
	if out == nil {
		return []string{template}
	}
	return out
}

// differentYears returns the countries whose release year differs, "default" being the query's own year.
func (q Query) differentYears() []string {
	if q.Year == 0 || q.Years == nil {
		return nil
	}
	countries := make([]string, 0, len(q.Years))
	for c := range q.Years {
		countries = append(countries, c)
	}
	sort.Strings(countries)
	seen, out := map[int]bool{q.Year: true}, []string{"default"}
	for _, c := range countries {
		if !seen[q.Years[c]] {
			seen[q.Years[c]] = true
			out = append(out, c)
		}
	}
	return out
}

func splitPerYear(queries, countries []string) []string {
	if len(countries) < 2 {
		return queries
	}
	var out []string
	for _, query := range queries {
		if !strings.Contains(query, "{year}") {
			out = append(out, query)
			continue
		}
		for _, c := range countries {
			out = append(out, strings.ReplaceAll(query, "{year}", "{year:"+c+"}"))
		}
	}
	return out
}

// shortcut: only the languages below have a long name for {languages:long} (Torrentio); others are
// sent as their code, which Torrentio ignores. Extend the table when a language is missed.
var languageNames = map[string]string{
	"en": "english", "ru": "russian", "uk": "ukrainian", "de": "german", "fr": "french", "es": "spanish", "it": "italian",
	"pt": "portuguese", "pl": "polish", "nl": "dutch", "tr": "turkish", "cs": "czech", "hu": "hungarian", "ro": "romanian",
	"bg": "bulgarian", "el": "greek", "ja": "japanese", "ko": "korean", "zh": "chinese", "hi": "hindi", "ar": "arabic",
	"he": "hebrew", "sv": "swedish", "no": "norwegian", "da": "danish", "fi": "finnish", "hr": "croatian", "sr": "serbian",
	"sk": "slovak", "sl": "slovenian", "lt": "lithuanian", "lv": "latvian", "et": "estonian", "vi": "vietnamese", "th": "thai",
}

// fill replaces the {keywords} of a template. It returns "" when the query can't be made, for example
// when the title has no translation in the language the template asks for.
func fill(d Definition, template string, q Query) string {
	out := template
	for _, m := range keywordRe.FindAllStringSubmatch(template, -1) {
		keyword := strings.ToLower(m[1])
		parts := strings.Split(keyword, ":")
		replace := func(value string) { out = strings.ReplaceAll(out, "{"+keyword+"}", value) }
		// number formats {season}, {season:2} (zero padded) and {season+1}
		num := func(n int) string {
			if name, add, ok := strings.Cut(keyword, "+"); ok && name != "" {
				return strconv.Itoa(n + getInt(add))
			}
			if len(parts) > 1 {
				return fmt.Sprintf("%0*d", getInt(parts[1]), n)
			}
			return strconv.Itoa(n)
		}
		switch {
		case strings.HasPrefix(keyword, "languages"):
			langs := append([]string(nil), q.Languages...)
			if keyword == "languages:long" {
				for i, l := range langs {
					if name := languageNames[l]; name != "" {
						langs[i] = name
					}
				}
			}
			replace(strings.Join(langs, ","))
		case strings.Contains(keyword, "title"):
			title, ok := q.title(d, parts)
			if !ok {
				return ""
			}
			replace(title)
		case strings.Contains(keyword, "year"):
			if len(parts) == 1 {
				replace(strconv.Itoa(q.Year))
			} else if y := q.Years[parts[1]]; y != 0 {
				replace(strconv.Itoa(y))
			} else if parts[1] == "default" {
				replace(strconv.Itoa(q.Year))
			}
		case strings.Contains(keyword, "show_tmdb_id"):
			replace(strconv.Itoa(q.ShowTMDBId))
		case strings.Contains(keyword, "tmdb_id"):
			replace(strconv.Itoa(q.TMDBId))
		case strings.Contains(keyword, "tvdb_id"):
			replace(strconv.Itoa(q.TVDBId))
		case strings.Contains(keyword, "imdb_id"):
			replace(q.IMDBId)
		case strings.Contains(keyword, "season"):
			replace(num(q.Season))
		case strings.Contains(keyword, "absolute_episode"):
			if q.AbsoluteNumber == 0 {
				return ""
			}
			replace(num(q.AbsoluteNumber))
		case strings.Contains(keyword, "episode"):
			replace(num(q.Episode))
		}
	}
	return out
}

// title picks the title for a {title} or {title:lang} keyword: the asked language, else the provider's,
// else the original. ok is false when the asked translation and the original are both missing.
func (q Query) title(d Definition, parts []string) (title string, ok bool) {
	lang := ""
	if len(parts) > 1 {
		lang = parts[1]
	}
	providerLang := d.str("language")
	if (lang == "" && providerLang == "") || len(q.Titles) == 0 {
		return q.Title, true
	}
	if lang == "" && q.Titles[providerLang] != "" {
		lang = providerLang
	}
	if q.Titles[lang] == "" {
		lang = "original"
	}
	if q.Titles[lang] == "" {
		return "", false
	}
	title = normalize(q.Titles[lang])
	// Translated titles lose their accents, unless the language is one of the tracker's own.
	if lang != "original" && (lang == "en" || !contains(strings.Split(strings.ReplaceAll(d.str("languages"), " ", ""), ","), lang)) {
		title = removeAccents(title)
	}
	if chars := d.str("remove_special_characters"); chars != "" {
		for _, c := range chars {
			title = strings.ReplaceAll(title, string(c), "")
		}
		title = strings.Join(strings.Fields(title), " ")
	}
	return title, true
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// normalize is Burst's normalize_string: no control characters, unquoted, unescaped, lower case.
func normalize(s string) string {
	s = strings.Map(func(r rune) rune {
		switch {
		case r == 160:
			return ' '
		case r < 32 || (r >= 127 && r < 160):
			return -1
		}
		return r
	}, s)
	if u, err := url.PathUnescape(s); err == nil {
		s = u
	}
	s = strings.ReplaceAll(strings.ReplaceAll(s, "<![CDATA[", ""), "]]", "")
	return strings.ToLower(html.UnescapeString(s))
}

func removeAccents(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.Is(unicode.Mn, r) {
			return -1
		}
		return r
	}, norm.NFKD.String(s))
}

// quote is Python's urllib quote: everything but letters, digits and "_.-~/" is percent-encoded, in the
// tracker's charset when it is not UTF-8.
func quote(s, charset string) string {
	if charset != "" && !strings.Contains(strings.ToLower(charset), "utf") {
		if enc, err := htmlindex.Get(charset); err == nil {
			if encoded, err := enc.NewEncoder().String(s); err == nil {
				s = encoded
			}
		}
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.IndexByte("_.-~/", c) >= 0 {
			b.WriteByte(c)
		} else {
			fmt.Fprintf(&b, "%%%02X", c)
		}
	}
	return b.String()
}

// searchURL builds the request URL of one query.
func searchURL(d Definition, q Query, query, extra string) string {
	_, queryKey := itemType(q.Method)
	base := d.str("base_url") + d.str(queryKey)
	if strings.Contains(base, "{") { // keywords can be in the URL too, as {languages:long} in Torrentio's
		base = fill(d, base, q)
	}
	sep := d.str("separator")
	u := strings.ReplaceAll(strings.ReplaceAll(base, "QUERY", query), "EXTRA", extra)
	u = strings.ReplaceAll(u, " ", sep)
	if sep != "%20" {
		u = strings.ReplaceAll(u, "%20", sep)
	}
	first := ""
	if query != "" {
		first = query[:1]
	}
	return strings.ReplaceAll(u, "FIRSTLETTER", first)
}

var numberRe = regexp.MustCompile(`\d{1,3}(?:[,. ]\d{3})+|\d+`)

// getInt reads a counter such as "1,234", "1 234" or "12 seeds"; 0 when there is none.
func getInt(s string) int {
	if n, err := strconv.Atoi(strings.TrimSpace(s)); err == nil {
		return n
	}
	m := numberRe.FindString(s)
	n, _ := strconv.Atoi(strings.Map(func(r rune) rune {
		if r < '0' || r > '9' {
			return -1
		}
		return r
	}, m))
	return n
}

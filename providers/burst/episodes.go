package burst

import (
	"strconv"
	"strings"

	"github.com/dlclark/regexp2"
)

// Season and episode numbers in torrent names: Burst's episodes.py. Its patterns need look-arounds, which
// Go's regexp does not have, hence regexp2 here.

const (
	rxStart     = `(?<![^\W_])` // not after a letter or digit; unlike \b, "_" counts as a separator
	rxEnd       = `(?![^\W_])`
	rxSep       = `[ ._]*`
	rxSeparator = `(-|~|&|,|\+|(?:to|thru|through|and)` + rxEnd + `)`
	rxSeason    = `(?:seasons?|saisons?|temporadas?|staffeln?|stagion[ei]|sezon[aiy]?|сезон[аиы]?)`
	rxEpisode   = `(?:episodes?|episodios?|episódios?|épisodes?|folgen?|odcinek|odcinki|epizod[aey]?|capitulos?|capítulos?|серия|серии|серія|серії|эпизод[аы]?|епізод[иі]?)`
	rxSxxEyy    = `s(\d{1,2})[ ._-]*(?:ep?|episode)[ ._-]*(\d{1,3})(?!\d)`
	rxNxYY      = `(\d{1,2})x(\d{2,3})` + rxEnd
)

func rx(pattern string) *regexp2.Regexp { return regexp2.MustCompile(pattern, regexp2.None) }

// The "next" patterns start with \G: they must match right where the number before them ended.
var (
	// S02E09, S02 EP09, S02.Episode.9, 2x09
	episodeRx = rx(rxStart + `(?:` + rxSxxEyy + `|` + rxNxYY + `)`)
	// what continues an episode: -S02E10, -2x10, -E10, -10 or & E10 after a separator, or E10 right after it
	nextEpisodeRx = rx(`\G(?:` + rxSep + rxSeparator + rxSep + `(?:` + rxSxxEyy + `|` + rxNxYY + `|(?:ep?|episode)?` + rxSep + `(\d{1,3})` + rxEnd + `)|[ ._-]*e(\d{1,3})(?!\d))`)
	// S02, Season 2, Saison 2, Сезон 2
	seasonRx     = rx(rxStart + `(?:s|` + rxSeason + `[ ._:-]*)(\d{1,2})` + rxEnd)
	nextSeasonRx = rx(`\G` + rxSep + rxSeparator + rxSep + `(?:s|` + rxSeason + `[ ._:-]*)?(\d{1,2})` + rxEnd)
	// 2 сезон, 2-й сезон, 1-4 сезоны; not "2 сезона", which counts seasons
	seasonBeforeRx = rx(rxStart + `(\d{1,2})(?:` + rxSep + `-` + rxSep + `(\d{1,2})` + rxSep + `сезон[ыи]|(?:-?(?:й|ый|ий|ой))?` + rxSep + `сезон(?![а-яё]))`)
	// E05, Ep 5, Episode 5, Серии 1-10
	bareEpisodeRx     = rx(rxStart + `(?:e|ep[ ._-]*|` + rxEpisode + `[ ._:-]*)(\d{1,3})` + rxEnd)
	nextBareEpisodeRx = rx(`\G` + rxSep + rxSeparator + rxSep + `(?:e|ep[ ._-]*|` + rxEpisode + `[ ._:-]*)?(\d{1,3})` + rxEnd)
	dashesRx          = rx(`[\x{2010}-\x{2015}\x{2212}]`)
	cyrillicXRx       = rx(`(?<=\d)х(?=\d)`)
)

// epnum is a season, an episode, or a (season, episode) pair; pairs compare season first.
type epnum [2]int

func (a epnum) less(b epnum) bool { return a[0] < b[0] || (a[0] == b[0] && a[1] < b[1]) }

// span is an inclusive range of numbers.
type span [2]epnum

func (s span) has(n epnum) bool { return !n.less(s[0]) && !s[1].less(n) }

func group(m *regexp2.Match, i int) string {
	if g := m.GroupByNumber(i); g != nil && len(g.Captures) > 0 {
		return g.String()
	}
	return ""
}

func atoi(s string) int { n, _ := strconv.Atoi(s); return n }

// episodeOf reads the season and episode of an episodeRx match, or of a nextEpisodeRx match (whose
// groups come after the separator's): a bare episode number keeps the season before it.
func episodeOf(m *regexp2.Match, last *epnum) epnum {
	first := 1
	if last != nil {
		first = 2
	}
	g := func(i int) string { return group(m, first+i) }
	if g(0) != "" || g(2) != "" {
		return epnum{atoi(g(0) + g(2)), atoi(g(1) + g(3))}
	}
	return epnum{last[0], atoi(g(4) + g(5))}
}

// lastNumber reads the number of a season or bare episode match: its last group that matched.
func lastNumber(m *regexp2.Match, _ *epnum) epnum {
	for i := m.GroupCount() - 1; i > 0; i-- {
		if s := group(m, i); s != "" {
			return epnum{atoi(s)}
		}
	}
	return epnum{}
}

// ranges collects the ranges first and the next matches right after it name, and blanks them in text.
func ranges(text []rune, first, next *regexp2.Regexp, value func(*regexp2.Match, *epnum) epnum) []span {
	var out []span
	m, _ := first.FindRunesMatch(text)
	for m != nil {
		last := value(m, nil)
		out = append(out, span{last, last})
		end := m.Index + m.Length
		for {
			follow, _ := next.FindRunesMatchStartingAt(text, end)
			if follow == nil {
				break
			}
			current := value(follow, &last)
			switch group(follow, 1) {
			case "-", "~", "to", "thru", "through":
				if current.less(last) {
					follow = nil
				} else {
					out[len(out)-1][1] = current
				}
			default:
				out = append(out, span{current, current})
			}
			if follow == nil {
				break
			}
			last, end = current, follow.Index+follow.Length
		}
		for i := m.Index; i < end; i++ {
			text[i] = ' '
		}
		m, _ = first.FindRunesMatchStartingAt(text, end)
	}
	return out
}

// parseName finds what a torrent name is for: (season, episode) ranges from SxxEyy and NxYY, seasons named
// without an episode (S02, Season 2), and episodes named without a season (E05, Episode 5).
func parseName(name string) (episodes, seasons, bare []span) {
	s, _ := dashesRx.Replace(strings.ToLower(name), "-", -1, -1)
	s, _ = cyrillicXRx.Replace(s, "x", -1, -1)
	text := []rune(s)
	episodes = ranges(text, episodeRx, nextEpisodeRx, episodeOf)
	for m, _ := seasonBeforeRx.FindRunesMatch(text); m != nil; m, _ = seasonBeforeRx.FindNextMatch(m) {
		first := atoi(group(m, 1))
		seasons = append(seasons, span{epnum{first}, epnum{max(first, atoi(group(m, 2)))}})
	}
	for m, _ := seasonBeforeRx.FindRunesMatch(text); m != nil; m, _ = seasonBeforeRx.FindRunesMatchStartingAt(text, m.Index+m.Length) {
		for i := m.Index; i < m.Index+m.Length; i++ {
			text[i] = ' '
		}
	}
	seasons = append(seasons, ranges(text, seasonRx, nextSeasonRx, lastNumber)...)
	bare = ranges(text, bareEpisodeRx, nextBareEpisodeRx, lastNumber)
	return
}

// nameMatches reports whether a torrent name is for the season, and for the episode when episode is not
// negative. Ranges and lists count (S02E08-E10, S01-S04, S01 & S04); a season named without episodes is a
// pack of all of them. A name with no numbers this knows is kept.
func nameMatches(name string, season, episode int) bool {
	episodes, seasons, bare := parseName(name)
	any := func(list []span, test func(span) bool) bool {
		for _, s := range list {
			if test(s) {
				return true
			}
		}
		return false
	}
	inSeasons := any(seasons, func(s span) bool { return s.has(epnum{season}) })
	if episode < 0 {
		if len(episodes) == 0 && len(seasons) == 0 {
			return true
		}
		return inSeasons || any(episodes, func(s span) bool { return s[0][0] <= season && season <= s[1][0] })
	}
	if any(episodes, func(s span) bool { return s.has(epnum{season, episode}) }) {
		return true
	}
	if inSeasons {
		return len(bare) == 0 || any(bare, func(s span) bool { return s.has(epnum{episode}) })
	}
	// an episode without a season (E125) may be an absolute number
	return len(episodes) == 0 && len(seasons) == 0
}

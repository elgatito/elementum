package ui

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"github.com/elgatito/elementum/config"
)

// stringsDir is the addon's language directory (resources/language). Tests replace it.
var stringsDir = func() string {
	if c := config.Get(); c != nil && c.Info != nil && c.Info.Path != "" {
		return filepath.Join(c.Info.Path, "resources", "language")
	}
	return ""
}

var (
	poMu    sync.Mutex
	poCache = map[string]map[int]string{} // by strings.po path; parsed once, nil for unreadable files

	localizeRe = regexp.MustCompile(`LOCALIZE\[(\d+)\]`)
	verbRe     = regexp.MustCompile(`%[-+ #0-9.]*[a-zA-Z%]`)
	markupRe   = regexp.MustCompile(`\[(?:/?(?:B|I|UPPERCASE|LOWERCASE|CAPITALIZE|LIGHT)|/?COLOR(?: [^\]]*)?)\]`)

	langMu    sync.Mutex
	langCache = map[string]map[int]string{} // dir|lang -> strings; bounded
)

const maxLangCache = 64

// plain turns Kodi label markup into plain text for HTTP clients.
func plain(s string) string {
	if !strings.Contains(s, "[") {
		return s
	}
	return markupRe.ReplaceAllString(strings.ReplaceAll(s, "[CR]", "\n"), "")
}

// loadPO parses a Kodi strings.po into id -> text (msgstr, or msgid when untranslated).
func loadPO(path string) map[int]string {
	poMu.Lock()
	defer poMu.Unlock()
	if m, ok := poCache[path]; ok {
		return m
	}
	var m map[int]string
	if b, err := os.ReadFile(path); err == nil {
		m = parsePO(string(b))
	}
	if m != nil { // failed loads are not cached, so unknown paths can't grow the map
		poCache[path] = m
	}
	return m
}

func parsePO(src string) map[int]string {
	out := map[int]string{}
	var id int
	var msgid, msgstr strings.Builder
	var cur *strings.Builder
	flush := func() {
		if id > 0 {
			if s := msgstr.String(); s != "" {
				out[id] = s
			} else if s := msgid.String(); s != "" {
				out[id] = s
			}
		}
		id = 0
		msgid.Reset()
		msgstr.Reset()
		cur = nil
	}
	for _, line := range strings.Split(src, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "msgctxt"):
			flush()
			if n, err := strconv.Atoi(strings.Trim(strings.TrimSpace(strings.TrimPrefix(line, "msgctxt")), `"#`)); err == nil {
				id = n
			}
		case strings.HasPrefix(line, "msgid"):
			cur = &msgid
			cur.WriteString(unquote(strings.TrimPrefix(line, "msgid")))
		case strings.HasPrefix(line, "msgstr"):
			cur = &msgstr
			cur.WriteString(unquote(strings.TrimPrefix(line, "msgstr")))
		case strings.HasPrefix(line, `"`) && cur != nil:
			cur.WriteString(unquote(line))
		}
	}
	flush()
	return out
}

func unquote(s string) string {
	if u, err := strconv.Unquote(strings.TrimSpace(s)); err == nil {
		return u
	}
	return ""
}

// stringsFor finds the strings of a language ("de", "de-DE"), or nil. Kodi names the dirs resource.language.de_de.
func stringsFor(lang string) map[int]string {
	dir := stringsDir()
	if dir == "" {
		return nil
	}
	key := dir + "|" + lang
	langMu.Lock()
	m, ok := langCache[key]
	langMu.Unlock()
	if ok {
		return m
	}
	m = findStrings(dir, lang)
	langMu.Lock()
	if len(langCache) < maxLangCache {
		langCache[key] = m
	}
	langMu.Unlock()
	return m
}

func findStrings(dir, lang string) map[int]string {
	l := strings.ToLower(strings.ReplaceAll(lang, "-", "_"))
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	have := map[string]bool{}
	for _, e := range entries {
		if n := strings.TrimPrefix(e.Name(), "resource.language."); n != e.Name() {
			have[n] = true
		}
	}
	cands := []string{l}
	if i := strings.IndexByte(l, '_'); i > 0 {
		l = l[:i]
	}
	for _, e := range entries { // directory order is sorted, so this is deterministic
		if n := strings.TrimPrefix(e.Name(), "resource.language."); n != e.Name() && strings.HasPrefix(n, l+"_") {
			cands = append(cands, n)
		}
	}
	for _, c := range cands {
		if !have[c] { // only existing directories are ever read
			continue
		}
		if m := loadPO(filepath.Join(dir, "resource.language."+c, "strings.po")); m != nil {
			return m
		}
	}
	return nil
}

// Translate resolves "LOCALIZE[30xxx]" and "LOCALIZE[30xxx];;arg;;arg" the way the Kodi addon does:
// the string is formatted with the args (an arg may be LOCALIZE[..] itself). Unknown ids fall back to
// English, then to the text as given.
func Translate(lang, text string) string {
	if !strings.Contains(text, "LOCALIZE") {
		return text
	}
	tr, en := stringsFor(lang), stringsFor("en")
	str := func(id int) (string, bool) {
		if s, ok := tr[id]; ok {
			return s, true
		}
		s, ok := en[id]
		return s, ok
	}
	if !strings.Contains(text, ";;") {
		return localizeRe.ReplaceAllStringFunc(text, func(m string) string {
			id, _ := strconv.Atoi(localizeRe.FindStringSubmatch(m)[1])
			if s, ok := str(id); ok {
				return s
			}
			return m
		})
	}
	parts := strings.Split(text, ";;")
	m := localizeRe.FindStringSubmatch(parts[0])
	if m == nil {
		return text
	}
	id, _ := strconv.Atoi(m[1])
	format, ok := str(id)
	if !ok {
		return text
	}
	args := parts[1:]
	for i, a := range args {
		if am := localizeRe.FindStringSubmatch(a); am != nil && strings.HasPrefix(a, "LOCALIZE") {
			n, _ := strconv.Atoi(am[1])
			if s, ok := str(n); ok {
				args[i] = s
			}
		}
	}
	i := 0
	return verbRe.ReplaceAllStringFunc(format, func(v string) string {
		if v == "%%" {
			return "%"
		}
		if i < len(args) {
			i++
			return args[i-1]
		}
		return v
	})
}

func addonIcon() string {
	if c := config.Get(); c != nil && c.Info != nil {
		return c.Info.Icon
	}
	return ""
}

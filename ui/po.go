package ui

import (
	"cmp"
	"compress/gzip"
	"embed"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"github.com/elgatito/elementum/config"
	"github.com/elgatito/elementum/xbmc"
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

// Plain turns Kodi label markup into plain text for HTTP clients.
func Plain(s string) string {
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

// The add-on's translations, copied into the binary so Elementum has its texts without the add-on:
// in a standalone run, a container, or as a server for client apps. Refresh them with
// `go generate ./ui` after the add-on's strings change.
//
//go:generate go run gen_strings.go
//go:embed strings/*.po.gz
var embeddedStrings embed.FS

// embedded returns the built-in strings of a language ("de", "de-DE"), or nil: the same choice of
// language as findStrings makes among the add-on's folders.
func embedded(lang string) map[int]string {
	l := strings.ToLower(strings.ReplaceAll(lang, "-", "_"))
	entries, _ := embeddedStrings.ReadDir("strings")
	name := ""
	for _, e := range entries { // sorted, so the first language of a family is always the same
		n := strings.TrimSuffix(e.Name(), ".po.gz")
		if n == l {
			name = n
			break
		}
		if name == "" && strings.HasPrefix(n, strings.SplitN(l, "_", 2)[0]+"_") {
			name = n
		}
	}
	if name == "" {
		return nil
	}
	f, err := embeddedStrings.Open("strings/" + name + ".po.gz")
	if err != nil {
		return nil
	}
	defer f.Close()
	zr, err := gzip.NewReader(f)
	if err != nil {
		return nil
	}
	b, err := io.ReadAll(zr)
	if err != nil {
		return nil
	}
	return parsePO(string(b))
}

// stringsFor finds the strings of a language ("de", "de-DE"), or nil. The add-on's own files win when
// its folder is there (they may be newer than the binary); else the strings built into the binary are used.
// Kodi names the dirs resource.language.de_de.
func stringsFor(lang string) map[int]string {
	dir := stringsDir()
	key := dir + "|" + lang
	langMu.Lock()
	m, ok := langCache[key]
	langMu.Unlock()
	if ok {
		return m
	}
	if dir != "" {
		m = findStrings(dir, lang)
	}
	if m == nil {
		m = embedded(lang)
	}
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

func init() {
	xbmc.Localize = func(lang, s string) string { return Translate(cmp.Or(lang, InterfaceLanguage()), s) }
}

// LanguageOf is the language of the texts for a Kodi host: the one its request asked for, else the
// configured interface language.
func LanguageOf(h *xbmc.XBMCHost) string {
	if h != nil && h.Lang != "" {
		return h.Lang
	}
	return InterfaceLanguage()
}

// InterfaceLanguage is the language of Kodi's interface, for the texts Elementum shows in Kodi. It is not
// the metadata language (config Language), which the user can set apart.
func InterfaceLanguage() string {
	c := config.Get()
	if c == nil {
		return ""
	}
	return cmp.Or(c.InterfaceLanguage, c.Language)
}

// Localized returns the raw form of an English text of the built-in search, "LOCALIZE[id];;arg;;arg",
// for a message that goes to several hosts: Kodi and each HTTP client translate it into their own
// language. An unknown text is returned with its arguments formatted in.
func Localized(english string, args ...any) string {
	id := textIDs[english]
	if id == 0 {
		return fmt.Sprintf(english, args...)
	}
	raw := "LOCALIZE[" + strconv.Itoa(id) + "]"
	for _, a := range args {
		raw += ";;" + fmt.Sprint(a)
	}
	return raw
}

// Text returns the add-on's translation of an English text of the built-in search (see textIDs) in
// lang. It returns the English text itself when there is no translation: an unknown text, or no add-on
// folder to read the strings from, as in a standalone run.
func Text(lang, english string) string {
	if id := textIDs[english]; id != 0 {
		raw := "LOCALIZE[" + strconv.Itoa(id) + "]"
		if t := Translate(lang, raw); t != raw {
			return t
		}
	}
	return english
}

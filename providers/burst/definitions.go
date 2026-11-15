// Package burst is Elementum's built-in torrent search: the providers of the Burst add-on
// (script.elementum.burst), searched from Go with no Kodi and no Python.
//
// providers.json is generated from Burst's own file by convert.py; see docs/builtin-providers.md.
package burst

//go:generate python3 convert.py

import (
	"embed"
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/op/go-logging"
)

var log = logging.MustGetLogger("burst")

//go:embed providers.json
var bundled []byte

// Icons of the providers, from Burst, by provider id.
//
//go:embed icons/*.png
var icons embed.FS

// writeIcons puts the icons in dir/icons, where Kodi can load them from; existing files are kept.
func writeIcons(dir string) {
	entries, _ := icons.ReadDir("icons")
	target := filepath.Join(dir, "icons")
	if len(entries) == 0 || os.MkdirAll(target, 0o755) != nil {
		return
	}
	for _, e := range entries {
		path := filepath.Join(target, e.Name())
		if fi, _ := e.Info(); fi != nil {
			if old, err := os.Stat(path); err == nil && old.Size() == fi.Size() {
				continue
			}
		}
		if b, err := icons.ReadFile("icons/" + e.Name()); err == nil {
			_ = os.WriteFile(path, b, 0o644)
		}
	}
}

// Icon returns the icon file of a provider in dir, "" when it has none.
func Icon(dir, id string) string {
	if dir == "" {
		return ""
	}
	path := filepath.Join(dir, "icons", id+".png")
	if _, err := os.Stat(path); err != nil {
		return ""
	}
	return path
}

// Definition is one provider: Burst's definition, as decoded JSON. Query templates are looked up by name
// (movie_keywords, tv_keywords_fallback2, ...), so the fields stay a map.
type Definition map[string]any

func (d Definition) str(key string) string {
	s, _ := d[key].(string)
	return s
}

func (d Definition) flag(key string) bool {
	b, _ := d[key].(bool)
	return b
}

// strings returns a JSON object of strings (post_data, headers, api_format), nil when absent.
func (d Definition) strings(key string) map[string]string {
	m, _ := d[key].(map[string]any)
	if m == nil {
		return nil
	}
	out := map[string]string{}
	for k, v := range m {
		if s, ok := v.(string); ok {
			out[k] = s
		}
	}
	return out
}

// Skipped says why Elementum does not search this provider yet, "" when it does.
func (d Definition) Skipped() string {
	switch {
	case d.str("unsupported") != "":
		return "definition not converted: " + d.str("unsupported")
	case d.str("token") != "":
		return "needs a request token, which is not supported"
	}
	return ""
}

var (
	mu          sync.Mutex
	definitions map[string]Definition
	loadedFrom  string
)

// Load reads the bundled definitions and merges every *.json file of dir (the user's overrides and own
// providers, in the same format) over them. It also writes the bundled file there for reference.
// An empty dir loads the bundled definitions only.
func Load(dir string) map[string]Definition {
	mu.Lock()
	defer mu.Unlock()
	if definitions != nil && loadedFrom == dir {
		return definitions
	}
	defs := map[string]Definition{}
	merge(defs, bundled, "bundled providers.json", false)
	if dir != "" {
		const reference = "providers.default.json"
		if err := os.MkdirAll(dir, 0o755); err == nil {
			// Rewritten on every load and never read back: edits belong in other files.
			_ = os.WriteFile(filepath.Join(dir, reference), bundled, 0o644)
			writeIcons(dir)
		}
		files, _ := filepath.Glob(filepath.Join(dir, "*.json"))
		sort.Strings(files)
		for _, f := range files {
			// settings.json is reserved for the per-provider settings.
			if name := filepath.Base(f); name == reference || name == settingsFile || name == cookiesFile {
				continue
			}
			if b, err := os.ReadFile(f); err != nil {
				log.Warningf("Could not read %s: %s", f, err)
			} else {
				merge(defs, b, f, true)
			}
		}
	}
	definitions, loadedFrom = defs, dir
	return defs
}

// Reload drops the loaded definitions, so the next search reads the files again.
func Reload() {
	mu.Lock()
	definitions = nil
	mu.Unlock()
}

// merge adds the definitions of one file. custom marks the user's files: a provider they define is on.
func merge(defs map[string]Definition, content []byte, source string, custom bool) {
	var in map[string]map[string]any
	if err := json.Unmarshal(content, &in); err != nil {
		log.Errorf("Ignoring %s: %s", source, err)
		return
	}
	for id, d := range in {
		// As Burst: the site root follows the search URL, so an override of base_url moves both.
		if base, _ := d["base_url"].(string); base != "" {
			if u, err := url.Parse(base); err == nil && u.Host != "" {
				d["root_url"] = u.Scheme + "://" + u.Host
			}
		}
		d["id"] = id
		if defs[id] == nil {
			defs[id] = Definition{}
			if custom {
				d["custom"], d["enabled"] = true, true
			}
		}
		update(defs[id], d)
	}
}

// update merges u into d, nested objects key by key, as Burst does for overrides.
func update(d, u map[string]any) {
	for k, v := range u {
		if child, ok := v.(map[string]any); ok {
			if old, ok := d[k].(map[string]any); ok {
				update(old, child)
				continue
			}
		}
		d[k] = v
	}
}

// withAlias returns the definition moved to a mirror: alias is another domain ("example.org") or URL
// ("http://example.org") of the same site. The original is not changed.
func (d Definition) withAlias(alias string) Definition {
	oldHost := host(d.str("root_url"))
	if oldHost == "" {
		oldHost = host(d.str("base_url"))
	}
	scheme, newHost, found := strings.Cut(alias, "://")
	if !found {
		scheme, newHost = "", alias
	}
	newHost = strings.Trim(newHost, "/ ")
	if alias == "" || oldHost == "" || newHost == "" {
		return d
	}
	// Through JSON, so the domain is replaced in every text of the definition, parser specs included.
	b, err := json.Marshal(d)
	if err != nil {
		return d
	}
	text := strings.ReplaceAll(string(b), oldHost, newHost)
	if scheme != "" {
		text = strings.NewReplacer("https://"+newHost, scheme+"://"+newHost, "http://"+newHost, scheme+"://"+newHost).Replace(text)
	}
	var out Definition
	if json.Unmarshal([]byte(text), &out) != nil {
		return d
	}
	return out
}

func host(raw string) string {
	if u, err := url.Parse(raw); err == nil {
		return u.Host
	}
	return ""
}

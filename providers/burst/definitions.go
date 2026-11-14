// Package burst is Elementum's built-in torrent search: the providers of the Burst add-on
// (script.elementum.burst), searched from Go with no Kodi and no Python.
//
// providers.json is generated from Burst's own file by convert.py; see docs/builtin-providers.md.
package burst

//go:generate python3 convert.py

import (
	_ "embed"
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"sync"

	"github.com/op/go-logging"
)

var log = logging.MustGetLogger("burst")

//go:embed providers.json
var bundled []byte

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
	case d.flag("private"), d.str("login_path") != "", d.str("token") != "":
		return "needs a login" // shortcut: private trackers are not ported yet, see docs/builtin-providers.md phase 4
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
		}
		files, _ := filepath.Glob(filepath.Join(dir, "*.json"))
		sort.Strings(files)
		for _, f := range files {
			// settings.json is reserved for the per-provider settings.
			if name := filepath.Base(f); name == reference || name == "settings.json" {
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

// Enabled returns the ids of the providers to search, sorted. A provider is on when its definition says
// "predefined": true (Burst's default set), which an override file can set for others, or when it is the
// user's own provider.
// shortcut: per-provider settings (settings.json and the API) are phase 3.
func Enabled(defs map[string]Definition) []string {
	var ids []string
	for id, d := range defs {
		if (d.flag("predefined") || d.flag("custom")) && d.flag("enabled") && d.Skipped() == "" {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	return ids
}

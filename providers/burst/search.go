package burst

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/html"
	"golang.org/x/net/html/charset"
	"golang.org/x/text/encoding/htmlindex"
)

// Result is one torrent, in the JSON form Elementum reads provider results in (bittorrent.TorrentFile).
type Result struct {
	Name     string `json:"name"`
	URI      string `json:"uri"`
	InfoHash string `json:"info_hash"`
	Size     string `json:"size"`
	Seeds    int    `json:"seeds"`
	Peers    int    `json:"peers"`
	Language string `json:"language"`
	Provider string `json:"provider"`
}

// MaxResults is how many results one provider may return, its best seeded ones (Burst's default).
const MaxResults = 10

// userAgent is sent to trackers; many refuse clients that do not look like a browser.
const userAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"

// Client returns the HTTP client for tracker requests. Elementum sets it to its internal proxy client.
var Client = func() *http.Client { return http.DefaultClient }

// Search asks every enabled provider of dir (see Load) and returns their results, best seeded first.
// Providers still running at the deadline of ctx are left out.
func Search(ctx context.Context, dir string, q Query) []Result {
	defs := Load(dir)
	ids := Enabled(defs)
	if len(ids) == 0 {
		log.Warning("No providers enabled")
		return nil
	}
	log.Infof("Searching %s with %s", q.Method, strings.Join(ids, ", "))

	start := time.Now()
	var mu sync.Mutex
	var all []Result
	done := make(chan struct{})
	var wg sync.WaitGroup
	for _, id := range ids {
		wg.Go(func() {
			results := searchProvider(ctx, id, defs[id], q)
			log.Infof("[%s] returned %d results in %.1f seconds", id, len(results), time.Since(start).Seconds())
			mu.Lock()
			all = append(all, results...)
			mu.Unlock()
		})
	}
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-ctx.Done():
		log.Warningf("Search timed out after %.1f seconds, some providers were too slow", time.Since(start).Seconds())
	}
	mu.Lock()
	defer mu.Unlock()
	return cleanup(all)
}

// searchProvider runs a definition's queries: the fallbacks only while there are no results.
func searchProvider(ctx context.Context, id string, d Definition, q Query) (results []Result) {
	defer func() {
		if r := recover(); r != nil { // a broken definition must not take the search down
			log.Errorf("[%s] failed: %v", id, r)
		}
	}()
	used := map[string]bool{}
	lastPriority := 1
	for _, s := range searches(d, q) {
		query, extra := fill(d, s.query, q), fill(d, s.extra, q)
		switch {
		case query == "", used[query+extra], ctx.Err() != nil:
			continue
		case s.priority > lastPriority && len(results) > 0:
			continue // fallbacks are for when there is nothing
		}
		used[query+extra], lastPriority = true, s.priority

		var post map[string]string
		for k, v := range d.strings("post_data") {
			if post == nil {
				post = map[string]string{}
			}
			post[k] = strings.ReplaceAll(strings.ReplaceAll(v, "QUERY", query), "EXTRA", extra)
		}
		content, _, err := fetch(ctx, d, searchURL(d, q, quote(query, d.str("charset")), quote(extra, d.str("charset"))), post, nil)
		if err != nil {
			log.Warningf("[%s] request failed: %s", id, err)
			continue
		}
		var found []Result
		if d.flag("is_api") {
			found = fromAPI(id, d, content)
		} else {
			found = fromPage(id, d, content)
			if d.flag("subpage") {
				found = subpages(ctx, id, d, found)
			}
		}
		title := ""
		if d.flag("filter_title") {
			title = normalize(query)
		}
		for _, r := range found {
			if verify(r, title) {
				r.Language = d.str("language")
				r.Provider = fmt.Sprintf("[COLOR %s]%s[/COLOR]", d.str("color"), r.Provider)
				results = append(results, r)
			}
		}
		results = cleanup(results)
	}
	if len(results) > MaxResults {
		results = results[:MaxResults]
	}
	return results
}

// fetch requests a URL (with POST when post is set) and returns the page decoded to UTF-8, and its content type.
func fetch(ctx context.Context, d Definition, target string, post, headers map[string]string) (string, string, error) {
	method, body := http.MethodGet, io.Reader(nil)
	if post != nil {
		form := url.Values{}
		for k, v := range post {
			form.Set(k, v)
		}
		method, body = http.MethodPost, strings.NewReader(form.Encode())
	}
	req, err := http.NewRequestWithContext(ctx, method, target, body)
	if err != nil {
		return "", "", err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	if method == http.MethodPost {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	for k, v := range d.strings("headers") {
		req.Header.Set(k, v)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := Client().Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return "", "", fmt.Errorf("status %d", resp.StatusCode)
	}
	// The definition's charset wins; else the one the response declares.
	reader := io.Reader(io.LimitReader(resp.Body, 16<<20))
	name := d.str("response_charset")
	if name == "" {
		name = d.str("charset")
	}
	if enc, err := htmlindex.Get(name); name != "" && err == nil {
		reader = enc.NewDecoder().Reader(reader)
	} else if r, err := charset.NewReader(reader, resp.Header.Get("Content-Type")); err == nil {
		reader = r
	}
	content, err := io.ReadAll(reader)
	return string(content), resp.Header.Get("Content-Type"), err
}

// subpageWorkers is how many result pages of one provider are fetched at once (as in Burst).
const subpageWorkers = 5

// subpages replaces each result's link to its page with the magnet or torrent link found on that page.
// Results whose page has none, or is not fetched before ctx ends, are dropped.
func subpages(ctx context.Context, id string, d Definition, results []Result) []Result {
	var headers map[string]string
	if d.str("subpage_mode") == "xhr" {
		headers = map[string]string{"X-Requested-With": "XMLHttpRequest"}
	}
	slots := make(chan struct{}, subpageWorkers)
	var wg sync.WaitGroup
	for i := range results {
		r := &results[i]
		if r.Name == "" || r.URI == "" || strings.HasPrefix(r.URI, "magnet") {
			continue
		}
		wg.Go(func() {
			slots <- struct{}{}
			defer func() { <-slots }()
			page := r.URI
			r.URI = ""
			if ctx.Err() != nil {
				return
			}
			content, contentType, err := fetch(ctx, d, page, nil, headers)
			switch {
			case err != nil:
				log.Debugf("[%s] subpage %s failed: %s", id, page, err)
			case strings.Contains(contentType, "bittorrent"):
				r.URI = page // the link was the torrent file itself
			default:
				r.URI = linkOnPage(d, content)
			}
		})
	}
	wg.Wait()
	return results
}

// The links Burst looks for on a result's page, in its order.
var (
	pageMagnetRe  = regexp.MustCompile(`magnet:\?[^'"\s<>\[\]]+`)
	pageTorrentRe = regexp.MustCompile(`http(.*?).torrent["']`)
	pagePathRes   = []*regexp.Regexp{
		regexp.MustCompile(`/download\?token=[A-Za-z0-9%]+`),
		regexp.MustCompile(`"(/download/[A-Za-z0-9]+)"`),
		regexp.MustCompile(`/torrents/download/\?id=[a-z0-9-_.]+`),
	}
	pageHashRes = []*regexp.Regexp{
		regexp.MustCompile(`: ([A-Fa-f0-9]{40})`),
		regexp.MustCompile(`/download.php\?id=([A-Fa-f0-9]{40})\W`),
	}
	pagePathRes2 = regexp.MustCompile(`(/download.php\?id=[A-Za-z0-9]+[^\s'"]*)`)
	pageHashRes2 = []*regexp.Regexp{
		regexp.MustCompile(`/get_torrents?/([A-Fa-f0-9]{40})`),
		regexp.MustCompile(`/hash/([A-Fa-f0-9]{40})`),
	}
)

// linkOnPage finds the magnet or torrent link of a result's page, "" when there is none.
// shortcut: Burst's animaunt case (a Referer header appended to the link) is left out with the other
// header-carrying links, until private trackers are ported.
func linkOnPage(d Definition, content string) string {
	last := func(m []string) string { return m[len(m)-1] }
	if m := pageMagnetRe.FindString(content); m != "" {
		return strings.ReplaceAll(m, "xt.1=", "xt=") // libtorrent does not support xt groups
	}
	if m := pageTorrentRe.FindStringSubmatch(content); m != nil {
		return strings.ReplaceAll("http"+m[1]+".torrent", "torcache.net", "itorrents.org")
	}
	for _, re := range pagePathRes {
		if m := re.FindStringSubmatch(content); m != nil {
			return d.str("root_url") + last(m)
		}
	}
	for _, re := range pageHashRes {
		if m := re.FindStringSubmatch(content); m != nil {
			return "magnet:?xt=urn:btih:" + m[1]
		}
	}
	if m := pagePathRes2.FindStringSubmatch(content); m != nil {
		return d.str("root_url") + m[1]
	}
	for _, re := range pageHashRes2 {
		if m := re.FindStringSubmatch(content); m != nil {
			return "magnet:?xt=urn:btih:" + m[1]
		}
	}
	return ""
}

// fromPage reads the results of an HTML page with the definition's parser specs.
func fromPage(id string, d Definition, content string) []Result {
	dom, err := html.Parse(strings.NewReader(content))
	if err != nil {
		log.Warningf("[%s] could not parse the page: %s", id, err)
		return nil
	}
	parser, _ := d["parser"].(map[string]any)
	key, _ := eval(parser["key"], dom, "")
	field := func(name string, row *html.Node) string {
		v, err := eval(parser[name], row, key)
		if err != nil {
			panic(err) // drops the row, see below
		}
		return v
	}
	var results []Result
	for _, row := range follow(dom, parser["row"]) {
		if row.FirstChild == nil {
			continue
		}
		func() {
			defer func() {
				if r := recover(); r != nil {
					log.Debugf("[%s] skipping a row: %v", id, r)
				}
			}()
			r := Result{Name: field("name", row), URI: field("torrent", row), InfoHash: field("infohash", row), Size: cleanSize(field("size", row)),
				Seeds: getInt(field("seeds", row)), Peers: getInt(field("peers", row)), Provider: d.str("name")}
			if description := field("description", row); description != "" {
				r.Name = fmt.Sprintf("%s (%s)", r.Name, description)
			}
			if i := strings.Index(r.URI, "magnet:?"); i > 0 {
				r.URI = r.URI[i:]
			}
			if r.URI != "" && !strings.HasPrefix(r.URI, "magnet") && !strings.HasPrefix(r.URI, "http") {
				r.URI = d.str("root_url") + r.URI
			}
			results = append(results, r)
		}()
	}
	return results
}

var (
	torrentioSeedsRe  = regexp.MustCompile(`\x{1F464}\s*(\d+)`)
	torrentioSizeRe   = regexp.MustCompile(`\b(\d+(?:\.\d+)? [MG]B)\b`)
	torrentioSourceRe = regexp.MustCompile(`\x{2699}\x{FE0F}?[ \t]*([^\n]+)`)
)

// fromAPI reads the results of a JSON API by the definition's api_format, which names the fields.
func fromAPI(id string, d Definition, content string) []Result {
	var data any
	if err := json.Unmarshal([]byte(content), &data); err != nil {
		log.Warningf("[%s] response is not JSON: %s", id, err)
		return nil
	}
	format := d.strings("api_format")
	items, _ := data.([]any)
	if format["results"] != "" {
		items, _ = nested(data, format["results"]).([]any)
	}
	if sub := format["subresults"]; sub != "" { // YTS: one result per entry of the movie's "torrents"
		var flat []any
		for _, item := range items {
			parent, _ := item.(map[string]any)
			children, _ := parent[sub].([]any)
			for _, c := range children {
				merged := map[string]any{}
				for k, v := range parent {
					merged[k] = v
				}
				child, _ := c.(map[string]any)
				for k, v := range child {
					merged[k] = v
				}
				flat = append(flat, merged)
			}
		}
		items = flat
	}

	var results []Result
	for _, item := range items {
		if _, ok := item.(map[string]any); !ok {
			continue
		}
		get := func(field string) any {
			if format[field] == "" {
				return nil
			}
			return nested(item, format[field])
		}
		str := func(field string) string { s, _ := get(field).(string); return s }
		r := Result{Provider: d.str("name"), InfoHash: str("info_hash"), Size: size(get("size")), Seeds: count(get("seeds")), Peers: count(get("peers"))}
		r.Name = strings.NewReplacer("\n", " ", "\r", "").Replace(str("name"))
		trackers := ""
		if id == "torrentio" { // seeds, size and source are lines of the title, after the name
			title := str("name")
			r.Name = strings.TrimSpace(strings.SplitN(title, "\n", 2)[0])
			if m := torrentioSeedsRe.FindStringSubmatch(title); m != nil {
				r.Seeds = getInt(m[1])
			}
			if m := torrentioSizeRe.FindStringSubmatch(title); m != nil {
				r.Size = m[1]
			}
			if m := torrentioSourceRe.FindStringSubmatch(title); m != nil {
				r.Provider = fmt.Sprintf("%s (%s)", r.Provider, strings.TrimSpace(m[1]))
			}
			sources, _ := nested(item, "sources").([]any)
			for _, s := range sources {
				if tracker, ok := strings.CutPrefix(fmt.Sprint(s), "tracker:"); ok {
					trackers += "&tr=" + url.QueryEscape(tracker)
				}
			}
		}
		if description := str("description"); description != "" {
			r.Name = strings.TrimSpace(r.Name + " " + description)
		}
		if quality := str("quality"); format["quality"] != "" {
			r.Name = fmt.Sprintf("%s - %s", r.Name, quality)
		}
		if r.URI = str("torrent"); r.URI != "" && d.str("download_path") != "" {
			r.URI = d.str("download_path") + r.URI + trackers
		}
		results = append(results, r)
	}
	return results
}

// nested reads a dotted key ("data.movies") from decoded JSON, nil when it is not there.
func nested(v any, key string) any {
	for _, k := range strings.Split(key, ".") {
		m, ok := v.(map[string]any)
		if !ok {
			return nil
		}
		v = m[k]
	}
	return v
}

// count reads a JSON number or numeric string.
func count(v any) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case string:
		return getInt(n)
	}
	return 0
}

// size formats a JSON size: bytes become "1.4 GB", a text is kept.
func size(v any) string {
	bytes, ok := v.(float64)
	if s, isText := v.(string); isText {
		if n := getInt(s); fmt.Sprint(n) != s {
			return cleanSize(s)
		} else {
			bytes, ok = float64(n), true
		}
	}
	if !ok {
		return ""
	}
	for _, unit := range []string{"", "K", "M", "G", "T"} {
		if bytes < 1024 {
			return fmt.Sprintf("%.1f %sB", bytes, unit)
		}
		bytes /= 1024
	}
	return fmt.Sprintf("%.1f PB", bytes)
}

// cleanSize cuts what follows the unit: "1.4 GB in 3 files" is "1.4 GB".
func cleanSize(s string) string {
	s = strings.ReplaceAll(s, "\u00a0", " ")
	if i := strings.LastIndex(s, "B"); i > 0 {
		return s[:i+1]
	}
	return s
}

// verify drops a result without a name, and one whose name lacks a word of the searched title
// (title is "" unless the definition asks for that check).
// shortcut: Burst's optional filters by resolution, release type, size range and season/episode numbers
// are off or wide open by default and are not ported; Elementum sorts and filters results itself.
func verify(r Result, title string) bool {
	if r.Name == "" {
		return false
	}
	name := normalize(r.Name)
	for _, word := range strings.Fields(title) {
		if !strings.Contains(name, word) {
			return false
		}
	}
	return true
}

// cleanup drops results without seeds or link and duplicates of a provider, and sorts by seeds.
func cleanup(results []Result) []Result {
	seen := map[string]bool{}
	out := results[:0:0]
	for _, r := range results {
		if r.Seeds == 0 || r.URI == "" {
			continue
		}
		hash := r.InfoHash
		if u, err := url.Parse(r.URI); hash == "" && err == nil && u.Scheme == "magnet" {
			hash = strings.TrimPrefix(u.Query().Get("xt"), "urn:btih:")
		}
		if hash == "" { // without an info hash a result is unique by its link, without appended headers
			sum := md5.Sum([]byte(strings.Split(r.URI, "|")[0]))
			hash = hex.EncodeToString(sum[:])
		}
		if key := r.Provider + strings.ToUpper(hash); !seen[key] {
			seen[key] = true
			out = append(out, r)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Seeds > out[j].Seeds })
	return out
}

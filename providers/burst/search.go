package burst

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
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

// userAgent is sent to trackers; many refuse clients that do not look like a browser.
const userAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"

// Client returns the HTTP client for tracker requests. Elementum sets it to its internal proxy client.
var Client = func() *http.Client { return http.DefaultClient }

// Search asks every enabled provider of dir (see Load and LoadSettings) and returns their results, best
// seeded first. Providers still running at the deadline of ctx are left out.
func Search(ctx context.Context, dir string, q Query) []Result {
	defs, settings, opts := Load(dir), LoadSettings(dir), LoadGeneral(dir)
	ids := Enabled(defs, settings, q.Method)
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
			results := searchProvider(ctx, id, defs[id], settings[id], opts, q)
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
	return cleanup(all, opts.on("allow_noseeds"))
}

// session is one provider's search: its definition (on the user's mirror), credentials and cookies.
type session struct {
	id        string
	d         Definition
	set       Settings
	client    *http.Client
	userAgent string
	loggedIn  bool
}

// jars keeps each provider's cookies for the life of the process, so a login is reused by later searches.
var jars sync.Map // "id|username" -> http.CookieJar

func newSession(id string, d Definition, set Settings) *session {
	client := *Client() // a copy: the jar is this provider's own
	jar, _ := cookiejar.New(nil)
	if cached, loaded := jars.LoadOrStore(id+"|"+set.Username, http.CookieJar(jar)); loaded {
		jar, _ = cached.(*cookiejar.Jar)
	}
	client.Jar = jar
	s := &session{id: id, d: d.withAlias(set.Alias), set: set, client: &client, userAgent: userAgent}
	if set.UserAgent != "" {
		s.userAgent = set.UserAgent
	}
	// The user's browser cookies: a session on a private tracker, or a passed bot check.
	if root, err := url.Parse(s.d.str("root_url")); err == nil && set.Cookies != "" {
		var cookies []*http.Cookie
		for _, pair := range strings.Split(set.Cookies, ";") {
			if name, value, ok := strings.Cut(strings.TrimSpace(pair), "="); ok && name != "" {
				cookies = append(cookies, &http.Cookie{Name: name, Value: value, Path: "/"})
				if name == s.d.str("login_cookie") {
					s.loggedIn = true
				}
			}
		}
		jar.SetCookies(root, cookies)
	}
	return s
}

var (
	lostfilmSeriesRe   = regexp.MustCompile(`PlayEpisode\('(\d+)'\)">`)
	lostfilmRedirectRe = regexp.MustCompile(`url=(.*?)">`)
)

// lostfilmPage follows LostFilm's search result to the page with the torrents of the series, "" when
// the search found no series.
func (s *session) lostfilmPage(ctx context.Context, target string, post map[string]string) string {
	root := s.d.str("root_url")
	page, _, _ := s.fetch(ctx, target, post, nil)
	m := lostfilmSeriesRe.FindStringSubmatch(page)
	if m == nil {
		return ""
	}
	target = root + "/v_search.php?a=" + m[1]
	page, _, _ = s.fetch(ctx, target, nil, nil)
	if m = lostfilmRedirectRe.FindStringSubmatch(page); m != nil {
		if target = m[1]; !strings.HasPrefix(target, "http") {
			target = root + target
		}
	}
	return target
}

// searchProvider runs a definition's queries: the fallbacks only while there are no results.
func searchProvider(ctx context.Context, id string, d Definition, set Settings, opts General, q Query) (results []Result) {
	defer func() {
		if r := recover(); r != nil { // a broken definition must not take the search down
			log.Errorf("[%s] failed: %v", id, r)
		}
	}()
	s := newSession(id, d, set)
	d = s.d
	q = q.withOptions(opts)
	f := newFilter(opts, q)
	used := map[string]bool{}
	lastPriority := 1
	for _, sr := range searches(d, q) {
		query, extra := fill(d, unquoteTitle(sr.query, q), q), fill(d, sr.extra, q)
		if opts.on("filter_quotes") {
			query, extra = strings.ReplaceAll(query, "'", ""), strings.ReplaceAll(extra, "'", "")
		}
		switch {
		case query == "", used[query+extra], ctx.Err() != nil:
			continue
		case sr.priority > lastPriority && len(results) > 0:
			continue // fallbacks are for when there is nothing
		}
		used[query+extra], lastPriority = true, sr.priority

		var post map[string]string
		for k, v := range d.strings("post_data") {
			if post == nil {
				post = map[string]string{}
			}
			post[k] = strings.ReplaceAll(strings.ReplaceAll(v, "QUERY", query), "EXTRA", extra)
		}
		target := searchURL(d, q, quote(query, d.str("charset")), quote(extra, d.str("charset")))
		target = strings.NewReplacer("USERNAME", set.Username, "PASSKEY", set.Passkey).Replace(target)
		if d.flag("private") && !q.SkipAuth {
			token, err := s.login(ctx)
			if err != nil {
				log.Warningf("[%s] login failed: %s", id, err)
				return results
			}
			target = strings.ReplaceAll(target, "CSRF_TOKEN", token)
		}
		if id == "lostfilm" && s.loggedIn { // its search answers with the series, whose page lists the torrents
			if target = s.lostfilmPage(ctx, target, post); target == "" {
				return results
			}
		}
		content, _, err := s.fetch(ctx, target, post, nil)
		if err != nil {
			log.Warningf("[%s] request failed: %s", id, err)
			continue
		}
		var found []Result
		if d.flag("is_api") {
			found = fromAPI(id, d, content, q.Languages)
		} else {
			found = fromPage(id, d, content)
			if d.flag("subpage") {
				found = s.subpages(ctx, found)
			}
			for i := range found {
				found[i].URI = s.withHeaders(found[i].URI, target)
			}
		}
		title := ""
		if d.flag("filter_title") {
			title = normalize(query)
		}
		for _, r := range found {
			if ok, why := f.verify(r, title); ok {
				r.Language = d.str("language")
				r.Provider = fmt.Sprintf("[COLOR %s]%s[/COLOR]", d.str("color"), r.Provider)
				results = append(results, r)
			} else {
				log.Debugf("[%s] dropped %q: %s", id, r.Name, why)
			}
		}
		results = cleanup(results, opts.on("allow_noseeds"))
	}
	return f.best(results)
}

var csrfRe = regexp.MustCompile(`name="_?csrf_?[tT]oken" value="(.*?)"`)

// login signs in to a private tracker once per session, with the form its definition describes. With a
// passkey there is nothing to do. It returns the CSRF token for the search URL of the trackers that want one.
// shortcut: Burst's cookie sync from a GitHub gist and its lostfilm lookup are not ported.
func (s *session) login(ctx context.Context) (csrf string, err error) {
	d := s.d
	form := d.strings("login_object")
	if s.loggedIn || s.set.Passkey != "" || len(form) == 0 {
		return "", nil
	}
	root := d.str("root_url")
	abs := func(u string) string {
		if u == "" || strings.HasPrefix(u, "http") {
			return u
		}
		return root + u
	}
	headers := map[string]string{}
	for k, v := range d.strings("login_headers") {
		headers[k] = keywordRe.ReplaceAllStringFunc(v, func(m string) string { return d.str(strings.Trim(m, "{}")) })
	}
	if d.flag("csrf_token") { // the login form carries a token to send back
		if page, _, err := s.fetch(ctx, abs(d.str("login_path")), nil, nil); err == nil {
			if m := csrfRe.FindStringSubmatch(page); m != nil {
				csrf = m[1]
			} else {
				s.loggedIn = true // no form: the cookies of an earlier login still hold
			}
		}
	}
	if !s.loggedIn {
		if pre := abs(d.str("login_prerequest")); pre != "" {
			_, _, _ = s.fetch(ctx, pre, nil, headers)
		}
		post := map[string]string{}
		for k, v := range form {
			post[k] = strings.NewReplacer("USERNAME", s.set.Username, "PASSWORD", s.set.Password, "CSRF_TOKEN", csrf).Replace(v)
		}
		page, _, err := s.fetch(ctx, abs(d.str("login_path")), post, headers)
		if err != nil {
			return "", err
		}
		if failed := d.str("login_failed"); failed != "" {
			if re, err := regexp.Compile(failed); err == nil && re.MatchString(page) {
				return "", errors.New("wrong username or password")
			}
		}
		s.loggedIn = true
	}
	if strings.Contains(d.str("base_url"), "CSRF_TOKEN") { // the search itself wants a token, from the site's own page
		csrf = ""
		if page, _, err := s.fetch(ctx, root+"/torrents.php", nil, nil); err == nil {
			if m := csrfRe.FindStringSubmatch(page); m != nil {
				csrf = m[1]
			}
		}
	}
	return csrf, nil
}

// withHeaders prepares a torrent file link for Elementum's download: the passkey is filled in, and
// what the tracker wants with the request (browser, referer, session cookies) is appended as
// "|Header=value", the form bittorrent.TorrentFile reads. Magnets are left alone.
func (s *session) withHeaders(link, page string) string {
	if link == "" || strings.HasPrefix(link, "magnet") {
		return link
	}
	if s.set.Passkey != "" {
		return strings.ReplaceAll(link, "PASSKEY", s.set.Passkey)
	}
	link += "|User-Agent=" + s.userAgent + "|Referer=" + page + "|Origin=" + page
	if u, err := url.Parse(strings.Split(link, "|")[0]); err == nil {
		var cookies []string
		for _, c := range s.client.Jar.Cookies(u) {
			cookies = append(cookies, c.Name+"="+c.Value)
		}
		if len(cookies) > 0 {
			link += "|Cookie=" + strings.Join(cookies, ";")
		}
	}
	return link
}

// fetch requests a URL (with POST when post is set) and returns the page decoded to UTF-8, and its content type.
func (s *session) fetch(ctx context.Context, target string, post, headers map[string]string) (string, string, error) {
	d := s.d
	method, body := http.MethodGet, io.Reader(nil)
	if post != nil {
		form := url.Values{}
		for k, v := range post {
			// the form goes in the tracker's charset, as the query does
			if enc, err := htmlindex.Get(d.str("charset")); err == nil && !strings.Contains(strings.ToLower(d.str("charset")), "utf") {
				if e, err := enc.NewEncoder().String(v); err == nil {
					v = e
				}
			}
			form.Set(k, v)
		}
		method, body = http.MethodPost, strings.NewReader(form.Encode())
	}
	req, err := http.NewRequestWithContext(ctx, method, target, body)
	if err != nil {
		return "", "", err
	}
	req.Header.Set("User-Agent", s.userAgent)
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
	resp, err := s.client.Do(req)
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
func (s *session) subpages(ctx context.Context, results []Result) []Result {
	var headers map[string]string
	if s.d.str("subpage_mode") == "xhr" {
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
			content, contentType, err := s.fetch(ctx, page, nil, headers)
			switch {
			case err != nil:
				log.Debugf("[%s] subpage %s failed: %s", s.id, page, err)
			case strings.Contains(contentType, "bittorrent"):
				r.URI = page // the link was the torrent file itself
			default:
				r.URI = linkOnPage(s.d, content)
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

var flagsRe = regexp.MustCompile(`[\x{1F1E6}-\x{1F1FF}]{2}`)

// fromAPI reads the results of a JSON API by the definition's api_format, which names the fields.
// languages are the preferred ones: Torrentio orders by them but does not filter, so that is done here.
func fromAPI(id string, d Definition, content string, languages []string) []Result {
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
			if len(languages) > 0 {
				// languages are flags in the last line; a release in English only shows none
				lines := strings.Split(title, "\n")
				found := flagsRe.FindAllString(lines[len(lines)-1], -1)
				if len(found) == 0 {
					found = flags("en")
				}
				wanted := false
				for _, l := range languages {
					for _, f := range flags(l) {
						wanted = wanted || contains(found, f)
					}
				}
				if !wanted {
					continue
				}
			}
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

// cleanup drops results without link, without seeds (unless noSeeds), and duplicates of a provider, and
// sorts by seeds.
func cleanup(results []Result, noSeeds bool) []Result {
	seen := map[string]bool{}
	out := results[:0:0]
	for _, r := range results {
		if (r.Seeds == 0 && !noSeeds) || r.URI == "" {
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

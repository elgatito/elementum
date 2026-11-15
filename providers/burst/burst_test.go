package burst

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"golang.org/x/net/html"
)

func parse(t *testing.T, s string) *html.Node {
	t.Helper()
	n, err := html.Parse(strings.NewReader(s))
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func spec(t *testing.T, s string) any {
	t.Helper()
	var v any
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		t.Fatalf("%s: %v", s, err)
	}
	return v
}

// Every bundled definition must be made only of specs the evaluator knows, with regexes Go compiles.
func TestBundledDefinitions(t *testing.T) {
	defs := Load("")
	if len(defs) < 80 {
		t.Fatalf("%d definitions", len(defs))
	}
	empty := parse(t, "<html></html>")
	var check func(id, field string, v any)
	check = func(id, field string, v any) {
		switch s := v.(type) {
		case map[string]any:
			if p, _ := s["pattern"].(string); p != "" {
				if _, err := regexp.Compile(p); err != nil {
					t.Errorf("%s %s: %v", id, field, err)
				}
			}
			for _, child := range s {
				check(id, field, child)
			}
		case []any:
			for _, child := range s {
				check(id, field, child)
			}
		}
	}
	searchable := 0
	for id, d := range defs {
		if d.str("unsupported") != "" {
			continue
		}
		if d.Skipped() == "" {
			searchable++
		}
		parser, _ := d["parser"].(map[string]any)
		for field, s := range parser {
			check(id, field, s)
			if field == "row" {
				follow(empty, s)
			} else if _, err := eval(s, empty, ""); err != nil && err != errNoMatch {
				t.Errorf("%s %s: %v", id, field, err)
			}
		}
	}
	if searchable < 40 {
		t.Errorf("only %d providers are searchable", searchable)
	}
	if on := Enabled(defs, nil, ""); !reflect.DeepEqual(on, []string{"1337x", "bt4g", "knaben", "thepiratebay", "torrentio", "yts"}) {
		t.Errorf("default providers: %v", on)
	}
}

const page = `<table><tbody>
<tr><th>head</th></tr>
<tr class="row odd"><td><a href="/cat">Movies</a><a href="/t/1">First <b>Movie</b> 2020 1080p </a></td><td>1.4 GB in 2 files</td><td> 1,234 </td><td>5 | 7</td>
    <td><div class="outer"><div class="inner">in</div>out</div></td><td align="right">a</td><td align="right">last</td></tr>
<tr class="row"><td><a href="/cat">Movies</a><a href="/t/2">Second</a></td><td>700 MB</td><td>0</td><td>1 | 2</td></tr>
<tr></tr>
</tbody></table>`

func TestSelector(t *testing.T) {
	dom := parse(t, page)
	rows := follow(dom, spec(t, `[{"tag":"tbody"},{"tag":"tr","all":true,"start":2}]`))
	if len(rows) != 3 {
		t.Fatalf("%d rows", len(rows))
	}
	if odd := follow(dom, spec(t, `[{"tag":"tr","all":true,"select":[["class",["odd","even"]]]}]`)); len(odd) != 1 {
		t.Errorf("class match: %d rows", len(odd))
	}
	if rows := follow(dom, spec(t, `[{"css":"tbody > tr.row"}]`)); len(rows) != 2 {
		t.Errorf("css rows: %d", len(rows))
	}
	if rows := follow(dom, spec(t, `[{"css":"tr","nth":2},{"css":"td"}]`)); len(rows) != 7 {
		t.Errorf("css steps: %d cells", len(rows))
	}
	for s, want := range map[string]string{
		`{"tag":"a","nth":2}`:                                                     "First Movie 2020 1080p",
		`{"tag":"a","nth":2,"attr":"href"}`:                                       "/t/1",
		`{"tag":"td","nth":3}`:                                                    "1,234",
		`{"tag":"td","nth":4,"divider":["|",1]}`:                                  "7",
		`{"tag":"td","nth":4,"divider":["|",5]}`:                                  "",
		`{"tag":"td","nth":99}`:                                                   "",
		`{"tag":"td","nth":0}`:                                                    "last", // list[-1]
		`{"tag":"div","nth":1}`:                                                   "in",   // children are counted before their parent
		`{"tag":"div","nth":2}`:                                                   "inout",
		`{"tag":"div","select":[["class",["outer"]]]}`:                            "inout",
		`{"in":[{"tag":"td","nth":1}],"tag":"a","attr":"href"}`:                   "/cat",
		`{"in":[{"tag":"td","select":[["align",["right"]]],"nth":0}],"raw":true}`: "last",
		`{"op":"format","format":"https://x.to%s","arg":{"tag":"a","nth":2,"attr":"href"}}`:    "https://x.to/t/1",
		`{"op":"concat","args":[{"tag":"a"}," [",{"tag":"td","nth":2},"]"]}`:                   "Movies [1.4 GB in 2 files]",
		`{"op":"split","arg":{"tag":"a","nth":2,"attr":"href"},"sep":"/","max":-1,"index":-1}`: "1",
		`{"op":"slice","arg":{"tag":"a","nth":2,"attr":"href"},"from":1}`:                      "t/1",
		`{"op":"regex","pattern":"(\\d+\\.\\d+ ..)","group":1,"arg":{"tag":"td","nth":2}}`:     "1.4 GB",
		`{"op":"upper","arg":{"op":"replace","arg":{"tag":"a"},"old":"ies","new":"y"}}`:        "MOVY",
		`{"op":"unquote","arg":{"op":"resub","pattern":"^x","repl":"","arg":"xa%20b"}}`:        "a b",
		`{"op":"if","test":{"tag":"span"},"then":"yes","else":"no"}`:                           "no",
		`{"op":"key"}`:                           "KEY",
		`{"css":"td:nth-child(2)"}`:              "1.4 GB in 2 files",
		`{"css":"td > a","nth":2,"attr":"href"}`: "/t/1",
		`{"css":"div.outer"}`:                    "inout",
		`{"css":"td[align=right]","nth":0}`:      "last",
		`{"css":"video"}`:                        "",
		`{"css":"td:nth-child(","attr":"href"}`:  "", // a selector that does not compile matches nothing
	} {
		if got, err := eval(spec(t, s), rows[0], "KEY"); err != nil || got != want {
			t.Errorf("%s = %q, %v; want %q", s, got, err, want)
		}
	}
	for _, s := range []string{
		`{"op":"regex","pattern":"nope(.)","group":1,"arg":{"tag":"a"}}`,
		`{"op":"split","arg":{"tag":"a"},"sep":"/","index":3}`,
		`{"in":[{"tag":"video"}],"attr":"src","raw":true}`,
		`{"op":"nope","arg":"x"}`,
	} {
		if got, err := eval(spec(t, s), rows[0], ""); err == nil {
			t.Errorf("%s = %q, want an error", s, got)
		}
	}
}

func TestQueries(t *testing.T) {
	d := Definition{
		"language": "ru", "languages": "ru", "separator": "+", "base_url": "https://x.to/s/QUERY/EXTRA", "movie_query": "?c=1",
		"movie_keywords": "{title:original:en} {year}", "movie_extra": "hd",
		"tv_keywords": "{title} s{season:2}e{episode:2}", "tv_keywords_fallback": "{title} {season}x{episode+1}", "tv_keywords_fallback2": "{title} e{absolute_episode:3}",
		"general_keywords": "{title:en}",
	}
	q := Query{Method: "movie", Title: "amelie", Year: 2001, Years: map[string]int{"original": 2001, "us": 2002, "fr": 2001},
		Titles: map[string]string{"original": "Le Fabuleux Destin d'Amélie", "en": "Amélie", "ru": "Амели"}}
	var got []string
	for _, s := range searches(d, q) {
		got = append(got, fmt.Sprintf("%d %s|%s", s.priority, fill(d, s.query, q), s.extra))
	}
	want := []string{
		"1 le fabuleux destin d'amélie 2001|hd", "1 le fabuleux destin d'amélie 2002|hd",
		"1 amelie 2001|hd", "1 amelie 2002|hd", // English titles lose their accents
		"1 амели 2001|hd", "1 амели 2002|hd", // a bare {title} is the provider's language
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("movie queries:\n%q\nwant\n%q", got, want)
	}
	if u := searchURL(d, q, quote("a b'c", ""), "hd"); u != "https://x.to/s/a+b%27c/hd?c=1" {
		t.Errorf("url %s", u)
	}
	if e := quote("Амели", "windows-1251"); e != "%C0%EC%E5%EB%E8" {
		t.Errorf("charset quote %s", e)
	}

	q = Query{Method: "episode", Title: "dark", Season: 2, Episode: 5, Titles: map[string]string{"original": "Dark"}}
	got = nil
	for _, s := range searches(d, q) {
		got = append(got, fmt.Sprintf("%d %s", s.priority, fill(d, s.query, q)))
	}
	// the last fallback needs an absolute number, so it makes no query
	if want := []string{"1 dark s02e05", "2 dark 2x6", "3 "}; !reflect.DeepEqual(got, want) {
		t.Errorf("episode queries: %q, want %q", got, want)
	}
	q.Method, q.Titles = "general", map[string]string{"source": "Some Query", "original": "Some Query"}
	if s := searches(d, q); len(s) != 1 || fill(d, s[0].query, q) != "some query" {
		t.Errorf("general: %+v", s)
	}
	quoted := "'{title:original}' s{season:2}"
	if got := unquoteTitle(quoted, Query{Titles: map[string]string{"source": "Mr. Robot"}}); got != "{title:original} s{season:2}" {
		t.Errorf("unquote: %s", got)
	}
	if got := unquoteTitle(quoted, Query{Titles: map[string]string{"source": "Dark"}}); got != quoted {
		t.Errorf("kept quotes: %s", got)
	}
	for in, want := range map[string]int{"12": 12, " 1,234 ": 1234, "1 234 seeds": 1234, "5.0": 5, "": 0, "n/a": 0} {
		if got := getInt(in); got != want {
			t.Errorf("getInt(%q) = %d, want %d", in, got, want)
		}
	}
}

// A search through a local server: an HTML provider with result pages, a JSON API, and the user's files.
func TestSearch(t *testing.T) {
	var requests []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.URL.RequestURI())
		switch {
		case strings.HasPrefix(r.URL.Path, "/search/"):
			fmt.Fprint(w, page)
		case r.URL.Path == "/t/1":
			fmt.Fprint(w, `<a href="magnet:?xt=urn:btih:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA&dn=x">get</a>`)
		case r.URL.Path == "/api":
			fmt.Fprint(w, `{"data":{"movies":[{"title_long":"Api Movie (2020)","torrents":[
				{"hash":"BBBB","quality":"1080p","seeds":7,"peers":1,"size":"1.2 GB","url":"http://x/1.torrent"},
				{"hash":"CCCC","quality":"720p","seeds":0,"peers":0,"size":"700 MB","url":"http://x/2.torrent"}]}]}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	dir := t.TempDir()
	custom := fmt.Sprintf(`{
	 "yts": {"predefined": false},
	 "mine": {"name": "Mine", "color": "FF00FF00", "language": "en", "languages": "en", "separator": "+", "subpage": true,
	   "base_url": "%[1]s/search/QUERY", "movie_keywords": "{title} {year}", "movie_keywords_fallback": "{title}",
	   "parser": {"row": [{"tag":"tr","all":true,"start":2}], "name": {"tag":"a","nth":2}, "torrent": {"tag":"a","nth":2,"attr":"href"},
	              "size": {"tag":"td","nth":2}, "seeds": {"tag":"td","nth":3}, "peers": {"tag":"td","nth":4,"divider":["|",1]}}},
	 "myapi": {"name": "MyApi", "color": "FFFFFFFF", "language": "en", "separator": "+", "is_api": true,
	   "base_url": "%[1]s/api?q=QUERY", "movie_keywords": "{title}",
	   "api_format": {"results":"data.movies","subresults":"torrents","name":"title_long","quality":"quality","torrent":"url","info_hash":"hash","seeds":"seeds","peers":"peers","size":"size"}}
	}`, srv.URL)
	if err := os.WriteFile(filepath.Join(dir, "my.json"), []byte(custom), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(Reload)
	defs := Load(dir)
	off := false
	for _, id := range []string{"1337x", "bt4g", "knaben", "thepiratebay", "torrentio"} {
		if err := SaveSettings(dir, id, Settings{Enabled: &off}); err != nil { // only the local ones are asked
			t.Fatal(err)
		}
	}
	if on := Enabled(defs, LoadSettings(dir), ""); !reflect.DeepEqual(on, []string{"mine", "myapi"}) {
		t.Fatalf("enabled: %v", on)
	}
	if defs["mine"].str("root_url") != srv.URL {
		t.Errorf("root_url %q", defs["mine"].str("root_url"))
	}
	if _, err := os.Stat(filepath.Join(dir, "providers.default.json")); err != nil {
		t.Errorf("reference file: %v", err)
	}

	var steps []string
	got := Search(context.Background(), dir, Query{Method: "movie", Title: "first movie", Year: 2020}, func(done, total int, waiting []string) {
		steps = append(steps, fmt.Sprintf("%d/%d", done, total))
	})
	if !reflect.DeepEqual(steps, []string{"0/2", "1/2", "2/2"}) {
		t.Errorf("progress: %v", steps)
	}
	if Icon(dir, "yts") == "" || Icon(dir, "mine") != "" {
		t.Errorf("icons: yts %q, mine %q", Icon(dir, "yts"), Icon(dir, "mine"))
	}
	want := []Result{
		{Name: "First Movie 2020 1080p", URI: "magnet:?xt=urn:btih:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA&dn=x", Size: "1.4 GB", Seeds: 1234, Peers: 7, Language: "en", Provider: "[COLOR FF00FF00]Mine[/COLOR]", id: "mine"},
		{Name: "Api Movie (2020) - 1080p", URI: "http://x/1.torrent", InfoHash: "BBBB", Size: "1.2 GB", Seeds: 7, Peers: 1, Language: "en", Provider: "[COLOR FFFFFFFF]MyApi[/COLOR]", id: "myapi"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("results:\n%+v\nwant\n%+v", got, want)
	}
	// The row without seeds is dropped, and with a result the fallback query is not sent.
	for _, r := range requests {
		if r == "/search/first+movie" {
			t.Errorf("fallback was requested: %v", requests)
		}
	}
}

func TestLinkOnPage(t *testing.T) {
	d := Definition{"root_url": "https://x.to"}
	for content, want := range map[string]string{
		`<a href="magnet:?xt.1=urn:btih:AA&tr=x">`:                         "magnet:?xt=urn:btih:AA&tr=x",
		`<a href="http://torcache.net/t/AA.torrent">`:                      "http://itorrents.org/t/AA.torrent",
		`<a href="/download/abc123">`:                                      "https://x.to/download/abc123",
		`Hash: 0123456789abcdef0123456789abcdef01234567 `:                  "magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567",
		`<a href="/hash/0123456789abcdef0123456789abcdef01234567?name=x">`: "magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567",
		`nothing here`: "",
	} {
		if got := linkOnPage(d, content); got != want {
			t.Errorf("%s: %q, want %q", content, got, want)
		}
	}
}

// resultsPage is a results table of n rows, the size of a real tracker page.
func resultsPage(n int) string {
	var b strings.Builder
	b.WriteString(`<html><head><title>x</title></head><body><div class="nav"><a href="/">home</a></div><table class="results"><tbody>`)
	for i := 0; i < n; i++ {
		fmt.Fprintf(&b, `<tr class="row"><td><a href="/cat">Movies</a><a href="/t/%[1]d" title="t">Some Movie %[1]d 1080p BluRay x264</a></td><td>1.4 GB</td><td>%[1]d</td><td>5</td><td><span class="date">2024-01-01</span></td></tr>`+"\n", i)
	}
	b.WriteString(`</tbody></table></body></html>`)
	return b.String()
}

func benchmarkPage(b *testing.B, parser string) {
	content := resultsPage(300)
	var p map[string]any
	if err := json.Unmarshal([]byte(parser), &p); err != nil {
		b.Fatal(err)
	}
	d := Definition{"name": "Bench", "parser": p}
	b.SetBytes(int64(len(content)))
	for b.Loop() {
		if got := fromPage("bench", d, content); len(got) != 300 {
			b.Fatalf("%d results", len(got))
		}
	}
}

// One results page of 300 rows, parsed and read with each selector form.
func BenchmarkPageTags(b *testing.B) {
	benchmarkPage(b, `{"row":[{"tag":"tbody"},{"tag":"tr","all":true}],"name":{"tag":"a","nth":2},"torrent":{"tag":"a","nth":2,"attr":"href"},"size":{"tag":"td","nth":2},"seeds":{"tag":"td","nth":3},"peers":{"tag":"td","nth":4}}`)
}

func BenchmarkPageCSS(b *testing.B) {
	benchmarkPage(b, `{"row":[{"css":"table.results > tbody > tr"}],"name":{"css":"td:nth-child(1) > a:nth-child(2)"},"torrent":{"css":"td:nth-child(1) > a:nth-child(2)","attr":"href"},"size":{"css":"td:nth-child(2)"},"seeds":{"css":"td:nth-child(3)"},"peers":{"css":"td:nth-child(4)"}}`)
}

// A private tracker on a local server: the login form, the session cookie on the search and on the
// torrent link, a passkey tracker, and a mirror.
func TestPrivate(t *testing.T) {
	logins := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/takelogin.php":
			logins++
			if r.PostFormValue("username") == "me" && r.PostFormValue("password") == "secret" && r.PostFormValue("returnto") == "/" {
				http.SetCookie(w, &http.Cookie{Name: "session", Value: "ok", Path: "/"})
				fmt.Fprint(w, "welcome")
			} else {
				fmt.Fprint(w, "Login failed")
			}
		case "/browse":
			if c, err := r.Cookie("session"); err != nil || c.Value != "ok" {
				http.Error(w, "no session", http.StatusForbidden)
				return
			}
			fmt.Fprint(w, `<table><tr><td><a href="/download/1.torrent">Private Movie 720p</a></td><td>2 GB</td><td>9</td></tr></table>`)
		case "/api":
			if r.URL.Query().Get("key") != "PK1" || r.URL.Query().Get("user") != "me" {
				http.Error(w, "bad key", http.StatusForbidden)
				return
			}
			fmt.Fprint(w, `[{"name":"Key Movie BluRay","seeders":3,"link":"/dl/7?passkey=PK1"}]`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	form := Definition{"name": "Form", "color": "FFFFFFFF", "private": true, "enabled": true, "predefined": true, "separator": "+",
		"base_url": "https://old.example/browse?q=QUERY", "root_url": "https://old.example", "movie_keywords": "{title}",
		"login_path": "/takelogin.php", "login_failed": "Login failed",
		"login_object": map[string]any{"username": "USERNAME", "password": "PASSWORD", "returnto": "/"},
		"parser": map[string]any{"row": spec(t, `[{"tag":"tr","all":true}]`), "name": spec(t, `{"tag":"a"}`),
			"torrent": spec(t, `{"tag":"a","attr":"href"}`), "size": spec(t, `{"tag":"td","nth":2}`), "seeds": spec(t, `{"tag":"td","nth":3}`)}}
	q := Query{Method: "movie", Title: "private movie"}
	ctx := context.Background()

	if st := status("form", form, Settings{}); !st.NeedsLogin || !st.Enabled {
		t.Errorf("status without credentials: %+v", st)
	}
	if got := searchProvider(ctx, "form", form, Settings{Username: "me", Password: "wrong", Alias: srv.URL}, nil, q); len(got) != 0 {
		t.Errorf("wrong password: %+v", got)
	}
	set := Settings{Username: "me", Password: "secret", Alias: srv.URL}
	got := searchProvider(ctx, "form", form, set, nil, q)
	if len(got) != 1 || got[0].Name != "Private Movie 720p" || got[0].Seeds != 9 {
		t.Fatalf("results: %+v", got)
	}
	parts := strings.Split(got[0].URI, "|")
	if parts[0] != srv.URL+"/download/1.torrent" || !contains(parts, "Cookie=session=ok") || !contains(parts, "Referer="+srv.URL+"/browse?q=private+movie") {
		t.Errorf("link: %q", got[0].URI)
	}
	if form.str("base_url") != "https://old.example/browse?q=QUERY" {
		t.Errorf("the mirror changed the definition: %s", form.str("base_url"))
	}
	// A background search does not sign in; here the cookies of the search before still hold.
	before := logins
	q.SkipAuth = true
	if got := searchProvider(ctx, "form", form, set, nil, q); len(got) != 1 || logins != before {
		t.Errorf("skip auth: %d results, %d logins", len(got), logins-before)
	}

	key := Definition{"name": "Key", "color": "FFFFFFFF", "private": true, "is_api": true, "separator": "+", "movie_keywords": "{title}",
		"base_url": srv.URL + "/api?user=USERNAME&key=PASSKEY&q=QUERY", "root_url": srv.URL, "download_path": srv.URL,
		"api_format": map[string]any{"name": "name", "seeds": "seeders", "torrent": "link"}}
	got = searchProvider(ctx, "key", key, Settings{Username: "me", Passkey: "PK1"}, nil, q)
	if len(got) != 1 || got[0].URI != srv.URL+"/dl/7?passkey=PK1" || got[0].Seeds != 3 {
		t.Errorf("passkey: %+v", got)
	}
}

func TestSettings(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "providers")
	on := true
	if err := SaveSettings(dir, "rutor", Settings{Enabled: &on}); err != nil {
		t.Fatal(err)
	}
	if err := SaveSettings(dir, "x", Settings{Username: "u", Password: "p"}); err != nil {
		t.Fatal(err)
	}
	got := LoadSettings(dir)
	if len(got) != 2 || got["rutor"].Enabled == nil || !*got["rutor"].Enabled || got["x"].Password != "p" {
		t.Errorf("%+v", got)
	}
	if fi, err := os.Stat(filepath.Join(dir, "settings.json")); err != nil || fi.Mode().Perm() != 0o600 {
		t.Errorf("settings file: %v %v", fi, err)
	}
	t.Cleanup(Reload)
	// settings.json is not read as a definitions file, and switches a provider that is off by default on
	if ids := Enabled(Load(dir), got, ""); !contains(ids, "rutor") || contains(ids, "x") {
		t.Errorf("enabled: %v", ids)
	}
	for _, st := range List(dir) {
		if st.ID == "rutracker" && (!st.Private || !st.NeedsLogin || st.Enabled) {
			t.Errorf("rutracker: %+v", st)
		}
	}
	if d := (Definition{"base_url": "https://a.org/s?q=QUERY", "root_url": "https://a.org", "parser": map[string]any{"torrent": map[string]any{"format": "https://a.org%s"}}}).withAlias("http://b.net"); d.str("base_url") != "http://b.net/s?q=QUERY" ||
		d["parser"].(map[string]any)["torrent"].(map[string]any)["format"] != "http://b.net%s" {
		t.Errorf("alias: %v", d)
	}
}

func TestFilters(t *testing.T) {
	check := func(opts General, q Query, r Result, want string) {
		t.Helper()
		if ok, why := newFilter(opts, q).verify(r, ""); (want == "") != ok || !strings.HasPrefix(why, want) {
			t.Errorf("%q %s with %v: ok=%v %q, want %q", r.Name, r.Size, opts, ok, why, want)
		}
	}
	movie := Query{Method: "movie"}
	// Burst's defaults
	check(nil, movie, Result{Name: "Movie 2020 1080p BluRay x264", Size: "8 GB"}, "")
	check(nil, movie, Result{Name: "Movie.2020.WEB-DL", Size: "700 MB"}, "")
	check(nil, movie, Result{Name: "Movie 2020"}, "missing release type")
	check(nil, movie, Result{Name: "Movie 2020 HDCAM 720p"}, "blocked by release type")
	check(nil, movie, Result{Name: "Movie 2020 TS 720p"}, "blocked by release type")
	check(nil, movie, Result{Name: "Movie 2020 240p WEB-DL"}, "resolution not allowed")
	check(nil, movie, Result{Name: "Фильм 2020 1080р WEBRip"}, "") // Cyrillic р
	check(nil, movie, Result{Name: "Фильмcam 2020 1080p"}, "")     // "cam" inside a word is not a keyword
	check(nil, movie, Result{Name: "Movie 2160p UHD", Size: "120 GB"}, "size out of range")
	check(nil, movie, Result{Name: "Show S03E01 720p"}, "") // the episode check is off by default

	check(General{"filter_cam": true}, movie, Result{Name: "Movie 2020 HDCAM 720p"}, "")
	check(General{"require_release_type": false}, movie, Result{Name: "Movie 2020"}, "")
	check(General{"max_size": 4.0}, movie, Result{Name: "Movie 1080p", Size: "4,5 GB"}, "size out of range")
	check(General{"separate_sizes": true, "max_size_movies": 4.0}, Query{Method: "episode"}, Result{Name: "Show 1080p", Size: "8 GB"}, "")
	check(General{"separate_sizes": true, "max_size_movies": 4.0}, movie, Result{Name: "Movie 1080p", Size: "8 GB"}, "size out of range")
	keywords := General{"additional_filters": true, "require": "x265, hdr", "block": "hindi", "accept": "remux"}
	check(keywords, movie, Result{Name: "Movie 1080p x265 HDR"}, "")
	check(keywords, movie, Result{Name: "Movie 1080p x265"}, "missing required keyword")
	check(keywords, movie, Result{Name: "Movie 1080p x265 HDR Hindi"}, "blocked by release type")
	episode := General{"require_episode": true}
	check(episode, Query{Method: "episode", Season: 2, Episode: 9}, Result{Name: "Show S02E09 720p"}, "")
	check(episode, Query{Method: "episode", Season: 2, Episode: 9}, Result{Name: "Show S02E10 720p"}, "other season or episode")
	check(episode, Query{Method: "season", Season: 2}, Result{Name: "Show S03 720p"}, "other season or episode")
	check(episode, Query{Method: "anime", Season: 2, Episode: 9}, Result{Name: "Show S03E01 720p"}, "")

	for in, want := range map[string]float64{"1.4 GB": 1.4e9, "700 MB": 7e8, "1,4 GB": 1.4e9, "1,234.5 MB": 1.2345e9, "1.86 GiB": 1.86e9, "": 0} {
		if got := sizeBytes(in); got < want*0.999 || got > want*1.001 {
			t.Errorf("sizeBytes(%q) = %g, want %g", in, got, want)
		}
	}
	// balanced: a well seeded 720p loses to a 1080p with a third of its seeds, and the cap follows
	f := newFilter(General{"max_results": 2.0}, movie)
	got := f.best([]Result{{Name: "a 480p", Seeds: 100}, {Name: "b 720p", Seeds: 90}, {Name: "c 1080p", Seeds: 80}})
	if len(got) != 2 || got[0].Name != "c 1080p" || got[1].Name != "b 720p" {
		t.Errorf("best: %+v", got)
	}

	dir := t.TempDir()
	if err := SaveOption(dir, "filter_cam", true); err != nil {
		t.Fatal(err)
	}
	if err := SaveOption(dir, "max_size", 50.0); err != nil {
		t.Fatal(err)
	}
	if SaveOption(dir, "max_size", "big") == nil || SaveOption(dir, "nope", true) == nil {
		t.Error("a bad option was saved")
	}
	if g := LoadGeneral(dir); !g.on("filter_cam") || g.num("max_size") != 50 || !g.on("filter_1080p") || g.num("max_results") != 10 {
		t.Errorf("options: %v", g)
	}
}

func TestLanguageOptions(t *testing.T) {
	d := Definition{"id": "x", "language": "ru", "languages": "ru"}
	q := Query{Method: "movie", Title: "amelie", Language: "de", Titles: map[string]string{"original": "Amélie", "de": "Die fabelhafte Welt", "ru": "Амели", "jp": "Romaji", "en": "English"}}

	// Elementum's language wins over the tracker's for a bare {title}, unless switched off or excepted
	if got := fill(d, "{title}", q.withOptions(nil)); got != "die fabelhafte welt" {
		t.Errorf("kodi language: %q", got)
	}
	if got := fill(d, "{title}", q.withOptions(General{"kodi_language": false})); got != "амели" {
		t.Errorf("provider language: %q", got)
	}
	if got := fill(d, "{title}", q.withOptions(General{"language_exceptions": "y, x"})); got != "амели" {
		t.Errorf("exception: %q", got)
	}
	if got := fill(d, "{title:original}", q.withOptions(nil)); got != "amélie" {
		t.Errorf("explicit language: %q", got)
	}

	if got := q.withOptions(General{"language_priority": "fr, kodi,fr"}).Languages; !reflect.DeepEqual(got, []string{"fr", "de"}) {
		t.Errorf("priority: %v", got)
	}
	if got := fill(Definition{}, "{languages:long}|{languages}", q.withOptions(General{"language_priority": "kodi,en"})); got != "german,english|de,en" {
		t.Errorf("languages keyword: %q", got)
	}

	q.Method = "anime"
	for mode, want := range map[float64]string{0: "Amélie", 1: "Romaji", 2: "English"} {
		if got := q.withOptions(General{"overwrite_anime_original_title": mode}).Titles["original"]; got != want {
			t.Errorf("anime title mode %v: %q", mode, got)
		}
	}
	if q.Titles["original"] != "Amélie" {
		t.Error("the shared query was changed")
	}

	// Torrentio: only results in a preferred language; no flag means English
	torrentio := Definition{"name": "Torrentio", "api_format": map[string]any{"results": "streams", "name": "title", "torrent": "infoHash"}, "download_path": "magnet:?xt=urn:btih:"}
	person, de, gb, ru := "\U0001F464", "\U0001F1E9\U0001F1EA", "\U0001F1EC\U0001F1E7", "\U0001F1F7\U0001F1FA"
	content := `{"streams":[{"title":"English 1080p\n` + person + ` 5","infoHash":"a"},{"title":"German 1080p\n` + person + ` 5\n` + de + `","infoHash":"b"},{"title":"Dual 1080p\n` + person + ` 5\n` + gb + ` / ` + ru + `","infoHash":"c"}]}`
	names := func(langs ...string) (out []string) {
		for _, r := range fromAPI("torrentio", torrentio, content, langs) {
			out = append(out, r.Name)
		}
		return
	}
	if got := names(); len(got) != 3 {
		t.Errorf("no priority: %v", got)
	}
	if got := names("de"); !reflect.DeepEqual(got, []string{"German 1080p"}) {
		t.Errorf("de: %v", got)
	}
	if got := names("en"); !reflect.DeepEqual(got, []string{"English 1080p", "Dual 1080p"}) {
		t.Errorf("en: %v", got)
	}
}

func TestContainsAndCookies(t *testing.T) {
	defs := map[string]Definition{
		"a": {"enabled": true, "predefined": true},
		"b": {"enabled": true, "predefined": true},
		"c": {"enabled": true, "predefined": true, "private": true, "login_cookie": "sid"},
	}
	settings := map[string]Settings{"a": {Contains: "movies"}, "b": {Contains: "shows"}, "c": {Cookies: "sid=1"}}
	for method, want := range map[string][]string{"movie": {"a", "c"}, "episode": {"b", "c"}, "anime": {"b", "c"}, "general": {"c"}, "": {"a", "b", "c"}} {
		if got := Enabled(defs, settings, method); !reflect.DeepEqual(got, want) {
			t.Errorf("%s: %v, want %v", method, got, want)
		}
	}

	var gotCookie, gotAgent string
	logins := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/login" {
			logins++
			return
		}
		gotAgent = r.UserAgent()
		if c, err := r.Cookie("cf_clearance"); err == nil {
			gotCookie = c.Value
		}
		fmt.Fprint(w, `<table><tr><td><a href="/t/1.torrent">Movie 1080p</a></td><td>3</td></tr></table>`)
	}))
	defer srv.Close()
	d := Definition{"name": "Cookie", "color": "FFFFFFFF", "private": true, "separator": "+", "base_url": srv.URL + "/s?q=QUERY", "root_url": srv.URL,
		"movie_keywords": "{title}", "login_path": "/login", "login_cookie": "sid", "login_object": map[string]any{"u": "USERNAME"},
		"parser": map[string]any{"row": spec(t, `[{"css":"tr"}]`), "name": spec(t, `{"css":"a"}`), "torrent": spec(t, `{"css":"a","attr":"href"}`), "seeds": spec(t, `{"css":"td","nth":2}`)}}
	set := Settings{Cookies: "sid=abc; cf_clearance=xyz", UserAgent: "MyBrowser/1"}
	got := searchProvider(context.Background(), "cookie", d, set, nil, Query{Method: "movie", Title: "movie"})
	if len(got) != 1 || gotCookie != "xyz" || gotAgent != "MyBrowser/1" || logins != 0 {
		t.Fatalf("results %+v, cookie %q, agent %q, logins %d", got, gotCookie, gotAgent, logins)
	}
	if !strings.Contains(got[0].URI, "|User-Agent=MyBrowser/1") || !strings.Contains(got[0].URI, "sid=abc") {
		t.Errorf("link: %s", got[0].URI)
	}
}

// One providers.json serves Burst and the built-in search: Elementum reads the "builtin" part, and skips a
// provider that only has Burst's Python expressions.
func TestBuiltinPart(t *testing.T) {
	t.Cleanup(Reload)
	dir := t.TempDir()
	custom := `{
	 "both": {"name": "Both", "base_url": "https://both.example/s?q=QUERY", "login_object": "{'u': USERNAME}",
	   "parser": {"row": "find_all('tr')", "name": "item('a')"},
	   "builtin": {"login_object": {"u": "USERNAME"}, "parser": {"row": [{"tag": "tr", "all": true}], "name": {"tag": "a"}}}},
	 "python": {"name": "Python", "base_url": "https://python.example/s?q=QUERY", "parser": {"row": "find_all('tr')", "name": "item('a')"}}
	}`
	if err := os.WriteFile(filepath.Join(dir, "my.json"), []byte(custom), 0o644); err != nil {
		t.Fatal(err)
	}
	defs := Load(dir)
	both := defs["both"]
	if _, isSteps := both["parser"].(map[string]any)["row"].([]any); !isSteps || both.strings("login_object")["u"] != "USERNAME" || both["builtin"] != nil || both.Skipped() != "" {
		t.Errorf("both: %v", both)
	}
	if why := defs["python"].Skipped(); !strings.Contains(why, "Python") {
		t.Errorf("python-only definition: %q", why)
	}
	// the bundled file is Burst's own: it still has the Python expressions next to the builtin part
	if !strings.Contains(string(bundled), `"builtin"`) || !strings.Contains(string(bundled), "find_once(") {
		t.Error("the bundled providers.json is not the shared file")
	}
}

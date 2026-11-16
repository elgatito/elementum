package burst

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestJar(t *testing.T) {
	dir := t.TempDir()
	j := &Jar{cookies: map[string]Cookie{}}
	j.open(dir)
	site, _ := url.Parse("https://www.example.org/forum/index.php")
	j.SetCookies(site, []*http.Cookie{
		{Name: "host", Value: "1"},
		{Name: "wide", Value: "2", Domain: ".example.org", Path: "/"},
		{Name: "deep", Value: "3", Path: "/admin"},
		{Name: "other", Value: "4", Domain: "evil.net"},
		{Name: "old", Value: "5", MaxAge: -1},
		{Name: "safe", Value: "6", Secure: true},
	})
	names := func(raw string) string {
		u, _ := url.Parse(raw)
		var out []string
		for _, c := range j.Cookies(u) {
			out = append(out, c.Name)
		}
		sortStrings(out)
		return strings.Join(out, ",")
	}
	for raw, want := range map[string]string{
		"https://www.example.org/forum/": "host,safe,wide",
		"http://www.example.org/":        "host,wide",
		"https://www.example.org/admin/": "deep,host,safe,wide",
		"https://example.org/":           "wide",
		"https://evil.net/":              "",
	} {
		if got := names(raw); got != want {
			t.Errorf("%s: %q, want %q", raw, got, want)
		}
	}
	j.save()
	// a second run has what the site set, and a changed value replaces the old one
	j2 := &Jar{cookies: map[string]Cookie{}}
	j2.open(dir)
	if len(j2.cookies) != 4 {
		t.Fatalf("reloaded %d cookies", len(j2.cookies))
	}
	j2.SetCookies(site, []*http.Cookie{{Name: "wide", Value: "new", Domain: "example.org", Path: "/"}})
	j2.save()
	j3 := &Jar{cookies: map[string]Cookie{}}
	j3.open(dir)
	if c := j3.cookies["example.org|/|wide"]; c.Value != "new" {
		t.Errorf("changed cookie: %+v", c)
	}
	if fi, err := os.Stat(filepath.Join(dir, cookiesFile)); err != nil || fi.Mode().Perm() != 0o600 {
		t.Errorf("cookies file: %v %v", fi, err)
	}
}

func sortStrings(s []string) {
	for i := range s {
		for k := i; k > 0 && s[k] < s[k-1]; k-- {
			s[k], s[k-1] = s[k-1], s[k]
		}
	}
}

// The cookie sync against a fake GitHub: encrypted cookies come into the jar before the search, the
// site changes one and adds one, and both go back to the gist.
func TestCookieSync(t *testing.T) {
	const password, filename = "pw", "kevast-gist-default.json"
	if got, err := aesDecode(aesEncode("hello cookies", password), password); err != nil || got != "hello cookies" {
		t.Fatalf("aes round trip: %q %v", got, err)
	}

	var siteURL string
	site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if c, err := r.Cookie("session"); err != nil || c.Value != "from-browser" {
			http.Error(w, "no session", http.StatusForbidden)
			return
		}
		if r.UserAgent() != "SyncedBrowser/1" {
			http.Error(w, "other browser", http.StatusForbidden)
			return
		}
		http.SetCookie(w, &http.Cookie{Name: "session", Value: "rotated", Path: "/"})
		http.SetCookie(w, &http.Cookie{Name: "seen", Value: "1", Path: "/"})
		fmt.Fprint(w, `<table><tr><td><a href="magnet:?xt=urn:btih:AA">Synced Movie 1080p</a></td><td>4</td></tr></table>`)
	}))
	defer site.Close()
	siteURL = site.URL
	domain := strings.Split(strings.TrimPrefix(siteURL, "http://"), ":")[0] // 127.0.0.1

	browser, _ := json.Marshal([]map[string]any{{"name": "session", "value": "from-browser", "path": "/", "hostOnly": true, "storeId": "0"}})
	content, _ := json.Marshal(map[string]string{domain: aesEncode(string(browser), password), "__userAgent": aesEncode("SyncedBrowser/1", password)})
	gistContent := string(content)
	patches := 0
	hub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer TOKEN" {
			http.Error(w, "bad token", http.StatusUnauthorized)
			return
		}
		file := map[string]any{"filename": filename, "content": gistContent}
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/": // the list
			if r.URL.Query().Get("page") == "1" {
				json.NewEncoder(w).Encode([]any{map[string]any{"id": "G1", "updated_at": "2026-01-01", "files": map[string]any{filename: file}}})
			} else {
				fmt.Fprint(w, "[]")
			}
		case r.Method == http.MethodGet && r.URL.Path == "/G1":
			json.NewEncoder(w).Encode(map[string]any{"id": "G1", "files": map[string]any{filename: file}})
		case r.Method == http.MethodPatch && r.URL.Path == "/G1":
			patches++
			var body struct {
				Files map[string]struct{ Content string } `json:"files"`
			}
			b, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(b, &body)
			gistContent = body.Files[filename].Content
			fmt.Fprint(w, "{}")
		default:
			http.NotFound(w, r)
		}
	}))
	defer hub.Close()
	oldAPI, oldJar := GistAPI, jar
	GistAPI, jar = hub.URL, &Jar{cookies: map[string]Cookie{}}
	t.Cleanup(func() { GistAPI, jar = oldAPI, oldJar; syncState = cookieSync{}; Reload() })

	dir := t.TempDir()
	custom := fmt.Sprintf(`{"synced": {"name": "Synced", "color": "FFFFFFFF", "private": true, "separator": "+", "login_cookie": "session",
	  "base_url": "%s/s?q=QUERY", "movie_keywords": "{title}",
	  "parser": {"row": [{"css":"tr"}], "name": {"css":"a"}, "torrent": {"css":"a","attr":"href"}, "seeds": {"css":"td","nth":2}}}}`, siteURL)
	if err := os.WriteFile(filepath.Join(dir, "my.json"), []byte(custom), 0o644); err != nil {
		t.Fatal(err)
	}
	off := false
	for _, id := range []string{"1337x", "bt4g", "knaben", "thepiratebay", "torrentio", "yts"} {
		_ = SaveSettings(dir, id, Settings{Enabled: &off})
	}
	for id, v := range map[string]any{"cookie_sync_enabled": true, "cookie_sync_token": "TOKEN", "cookie_sync_password": password} {
		if err := SaveOption(dir, id, v); err != nil {
			t.Fatal(err)
		}
	}

	// The private provider has no login stored: the synced cookies are its login.
	q := Query{Method: "movie", Title: "synced movie"}
	got := Search(context.Background(), dir, q, nil)
	if len(got) != 1 || got[0].Name != "Synced Movie 1080p" {
		t.Fatalf("results: %+v", got)
	}
	if LoadGeneral(dir).raw("cookie_sync_gist_id") != "G1" {
		t.Error("the found gist was not remembered")
	}
	if patches == 0 {
		t.Fatal("changed cookies were not sent back")
	}
	var pushed map[string]string
	_ = json.Unmarshal([]byte(gistContent), &pushed)
	text, err := aesDecode(pushed[domain], password)
	var cookies []map[string]any
	if err != nil || json.Unmarshal([]byte(text), &cookies) != nil || len(cookies) != 2 {
		t.Fatalf("pushed cookies: %q %v", text, err)
	}
	if cookies[0]["value"] != "rotated" || cookies[0]["storeId"] != "0" || cookies[1]["name"] != "seen" {
		t.Errorf("pushed cookies: %v", cookies)
	}
	if ua, _ := aesDecode(pushed["__userAgent"], password); ua != "SyncedBrowser/1" {
		t.Errorf("user agent was not kept: %q", ua)
	}
	// and the rotated value is on disk for the next run
	j := &Jar{cookies: map[string]Cookie{}}
	j.open(dir)
	if c := j.cookies[domain+"|/|session"]; c.Value != "rotated" {
		t.Errorf("stored cookie: %+v", c)
	}
}

func TestImportBurst(t *testing.T) {
	t.Cleanup(Reload)
	dir := filepath.Join(t.TempDir(), "providers")
	burstSettings := filepath.Join(t.TempDir(), "settings.xml")
	xml := `<settings version="2">
    <setting id="use_yts">false</setting>
    <setting id="use_rutor" default="true">true</setting>
    <setting id="use_rutracker">true</setting>
    <setting id="rutracker_username">me</setting>
    <setting id="rutracker_password">secret</setting>
    <setting id="rutracker_alias">http://mirror.example</setting>
    <setting id="rutracker_contains">2</setting>
    <setting id="max_results">15</setting>
    <setting id="filter_cam">true</setting>
    <setting id="accept">remux</setting>
    <setting id="cookie_sync_token">TokenWithCase</setting>
    <setting id="unknown_setting">x</setting>
</settings>`
	if err := os.WriteFile(burstSettings, []byte(xml), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, imported := ImportBurst(dir, filepath.Join(t.TempDir(), "missing.xml")); imported {
		t.Error("imported from a missing file")
	}
	if CanImportBurst(dir, filepath.Join(t.TempDir(), "missing.xml")) || !CanImportBurst(dir, burstSettings) {
		t.Error("CanImportBurst is wrong before the import")
	}
	// settings made before the question existed (a settings dialog of an older build) do not stop it,
	// and what Burst does not set stays
	on := true
	if err := SaveSettings(dir, "toloka", Settings{Username: "mine", Password: "pw", Cookies: "a=b", Enabled: &on}); err != nil {
		t.Fatal(err)
	}
	if err := SaveSettings(dir, "rutracker", Settings{Cookies: "sid=1", Username: "old"}); err != nil {
		t.Fatal(err)
	}
	if err := SaveOption(dir, "cookie_sync_user", "octocat"); err != nil {
		t.Fatal(err)
	}
	if !CanImportBurst(dir, burstSettings) {
		t.Error("not offered for settings made before the question")
	}
	enabled, imported := ImportBurst(dir, burstSettings)
	if !imported || enabled != 2 {
		t.Fatalf("imported %v, %d enabled", imported, enabled)
	}
	set, general := LoadSettings(dir), LoadGeneral(dir)
	if r := set["rutracker"]; r.Username != "me" || r.Password != "secret" || r.Alias != "http://mirror.example" || r.Contains != "shows" || !*r.Enabled || r.Cookies != "sid=1" {
		t.Errorf("rutracker: %+v", r)
	}
	if *set["yts"].Enabled || general.num("max_results") != 15 || !general.on("filter_cam") || general.text("accept") != "remux" || general.raw("cookie_sync_token") != "TokenWithCase" {
		t.Errorf("yts %+v, general %v", set["yts"], general)
	}
	if set["toloka"].Username != "mine" || set["toloka"].Cookies != "a=b" || general.raw("cookie_sync_user") != "octocat" {
		t.Errorf("settings Burst does not have were lost: %+v %v", set["toloka"], general)
	}
	if ids := Enabled(Load(dir), set, "episode"); !contains(ids, "rutor") || !contains(ids, "rutracker") || contains(ids, "yts") {
		t.Errorf("enabled: %v", ids)
	}
	// declining leaves an empty settings file, and nothing is offered again
	declined := filepath.Join(t.TempDir(), "providers")
	if err := SkipImport(declined); err != nil || CanImportBurst(declined, burstSettings) || len(LoadSettings(declined)) != 0 {
		t.Errorf("declined import: %v", err)
	}
	if CanImportBurst(dir, burstSettings) {
		t.Error("offered again after the import")
	}
	// asked once, but it can always be made again from the settings
	if _, again := ImportBurst(dir, burstSettings); !again {
		t.Error("an import asked for again did not run")
	}
}

func TestDeviceLogin(t *testing.T) {
	polls := 0
	hub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		switch r.URL.Path {
		case "/login/device/code":
			if r.PostForm.Get("client_id") == "" || r.PostForm.Get("scope") != "gist" {
				fmt.Fprint(w, `{"error":"bad_request"}`)
				return
			}
			fmt.Fprint(w, `{"device_code":"DEV","user_code":"AB12-CD34","verification_uri":"https://github.com/login/device","expires_in":60,"interval":1}`)
		case "/login/oauth/access_token":
			if polls++; polls == 1 {
				fmt.Fprint(w, `{"error":"authorization_pending"}`)
			} else if r.PostForm.Get("device_code") == "DEV" {
				fmt.Fprint(w, `{"access_token":"NEWTOKEN"}`)
			}
		case "/user":
			if r.Header.Get("Authorization") == "Bearer NEWTOKEN" {
				fmt.Fprint(w, `{"login":"octocat"}`)
			}
		default:
			http.NotFound(w, r)
		}
	}))
	defer hub.Close()
	oldLogin, oldAPI := GitHubLogin, GistAPI
	GitHubLogin, GistAPI = hub.URL, hub.URL+"/gists"
	t.Cleanup(func() { GitHubLogin, GistAPI = oldLogin, oldAPI; syncState = cookieSync{} })

	dir := t.TempDir()
	_ = SaveOption(dir, "cookie_sync_gist_id", "GIST-OF-ANOTHER-ACCOUNT")
	code, err := StartDeviceLogin(context.Background())
	if err != nil || code.UserCode != "AB12-CD34" || code.VerificationURL == "" || code.Interval != 1 {
		t.Fatalf("code %+v, %v", code, err)
	}
	if err := FinishDeviceLogin(context.Background(), dir, code); err != nil {
		t.Fatal(err)
	}
	g := LoadGeneral(dir)
	if g.raw("cookie_sync_token") != "NEWTOKEN" || g.raw("cookie_sync_user") != "octocat" || !g.on("cookie_sync_enabled") || g.raw("cookie_sync_gist_id") != "" || polls != 2 {
		t.Errorf("after login: %v, %d polls", g, polls)
	}
	// cancelled while waiting
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := FinishDeviceLogin(ctx, dir, code); err != context.Canceled {
		t.Errorf("cancelled login: %v", err)
	}
}

func TestListGists(t *testing.T) {
	const password, filename = "pw", "kevast-gist-default.json"
	cookies := func(n int) string {
		list := make([]map[string]string, n)
		for i := range list {
			list[i] = map[string]string{"name": fmt.Sprint("c", i), "value": "v"}
		}
		b, _ := json.Marshal(list)
		return aesEncode(string(b), password)
	}
	content, _ := json.Marshal(map[string]string{"a.org": cookies(2), "b.org": cookies(3), "__userAgent": aesEncode("UA", password)})
	file := map[string]any{"filename": filename, "content": string(content)}
	hub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/" && r.URL.Query().Get("page") == "1":
			json.NewEncoder(w).Encode([]any{
				map[string]any{"id": "OLD", "description": "old", "updated_at": "2025-01-01T00:00:00Z", "files": map[string]any{filename: file}},
				map[string]any{"id": "NEW", "description": "Browser", "updated_at": "2026-11-15T17:20:00Z", "files": map[string]any{filename: file}},
				map[string]any{"id": "OTHER", "files": map[string]any{"notes.txt": map[string]any{"filename": "notes.txt"}}},
			})
		case r.URL.Path == "/":
			fmt.Fprint(w, "[]")
		default:
			json.NewEncoder(w).Encode(map[string]any{"id": strings.TrimPrefix(r.URL.Path, "/"), "files": map[string]any{filename: file}})
		}
	}))
	defer hub.Close()
	old := GistAPI
	GistAPI = hub.URL
	t.Cleanup(func() { GistAPI = old; syncState = cookieSync{} })

	dir := t.TempDir()
	if _, err := ListGists(context.Background(), dir); err == nil {
		t.Error("listed without a token")
	}
	_ = SaveOption(dir, "cookie_sync_token", "T")
	_ = SaveOption(dir, "cookie_sync_password", password)
	gists, err := ListGists(context.Background(), dir)
	if err != nil || len(gists) != 2 || gists[0].ID != "NEW" || gists[0].Description != "Browser" || gists[0].Cookies != 5 || gists[0].Sites != 2 {
		t.Fatalf("%+v %v", gists, err)
	}
	if err := SelectGist(dir, "OLD"); err != nil || LoadGeneral(dir).raw("cookie_sync_gist_id") != "OLD" {
		t.Errorf("select: %v", err)
	}
	// a wrong password: the gist is listed, its cookies are not counted
	_ = SaveOption(dir, "cookie_sync_password", "wrong")
	if gists, _ = ListGists(context.Background(), dir); len(gists) != 2 || gists[0].Cookies != 0 {
		t.Errorf("wrong password: %+v", gists)
	}
}

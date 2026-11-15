package jsonapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/elgatito/elementum/config"
	"github.com/elgatito/elementum/providers"
	"github.com/elgatito/elementum/providers/burst"
	"github.com/elgatito/elementum/xbmc"
)

func TestProviders(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	Register(r, nil)
	old := config.Get().Info
	config.Get().Info = &xbmc.AddonInfo{Profile: t.TempDir()}
	t.Cleanup(func() { config.Get().Info = old; burst.Reload() })
	burst.Reload()

	call := func(method, path, body string) (*httptest.ResponseRecorder, Provider) {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		var p Provider
		_ = json.Unmarshal(w.Body.Bytes(), &p)
		return w, p
	}

	w, _ := call(http.MethodGet, "/api/providers", "")
	var list []Provider
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil || w.Code != http.StatusOK || len(list) < 80 {
		t.Fatalf("status %d, %d providers: %v", w.Code, len(list), err)
	}
	byID := map[string]Provider{}
	for _, p := range list {
		byID[p.Id] = p
	}
	if p := byID["yts"]; !p.Enabled || !p.Searched || p.Private || p.Reason != nil {
		t.Errorf("yts: %+v", p)
	}
	if p := byID["rutracker"]; p.Enabled || p.Searched || !p.Private {
		t.Errorf("rutracker: %+v", p)
	}

	// Enabled without credentials: not searched, with the reason. Then the credentials; the password is not echoed.
	if w, p := call(http.MethodPut, "/api/providers/rutracker", `{"enabled":true}`); w.Code != http.StatusOK || !p.Enabled || p.Searched || p.Reason == nil {
		t.Errorf("enable: %d %s", w.Code, w.Body)
	}
	w, p := call(http.MethodPut, "/api/providers/rutracker", `{"username":"me","password":"secret","alias":"http://mirror.example"}`)
	if w.Code != http.StatusOK || !p.Enabled || !p.Searched || !p.HasSecret || *p.Username != "me" || *p.Alias != "http://mirror.example" || strings.Contains(w.Body.String(), `"secret"`) {
		t.Errorf("credentials: %d %s", w.Code, w.Body)
	}
	if w, p := call(http.MethodPut, "/api/providers/rutracker", `{"password":""}`); w.Code != http.StatusOK || p.HasSecret || p.Searched || *p.Username != "me" {
		t.Errorf("clear password: %d %s", w.Code, w.Body)
	}
	w, p = call(http.MethodPut, "/api/providers/rutracker", `{"contains":"shows","cookies":"bb_session=1","user_agent":"UA/1"}`)
	if w.Code != http.StatusOK || *p.Contains != ProviderContainsShows || !*p.HasCookies || !p.Searched || *p.UserAgent != "UA/1" || strings.Contains(w.Body.String(), "bb_session") {
		t.Errorf("cookies: %d %s", w.Code, w.Body)
	}
	if w, _ := call(http.MethodPut, "/api/providers/rutracker", `{"contains":"music"}`); w.Code != http.StatusBadRequest {
		t.Errorf("bad contains: %d", w.Code)
	}
	if w, _ := call(http.MethodPut, "/api/providers/nope", `{"enabled":true}`); w.Code != http.StatusNotFound {
		t.Errorf("unknown: %d", w.Code)
	}
	if w, _ := call(http.MethodPut, "/api/providers/yts", `{"enabled":"yes"}`); w.Code != http.StatusBadRequest {
		t.Errorf("bad body: %d", w.Code)
	}

	w, _ = call(http.MethodGet, "/api/providers/options", "")
	var options []ProviderOption
	if err := json.Unmarshal(w.Body.Bytes(), &options); err != nil || w.Code != http.StatusOK || len(options) != len(burst.Options) {
		t.Fatalf("options: %d %v", w.Code, err)
	}
	for _, o := range options {
		if o.Id == "sort_by" && (o.Choices == nil || len(*o.Choices) != 4 || (*o.Choices)[3] != "Balanced") {
			t.Errorf("sort_by choices: %+v", o.Choices)
		}
		if o.Id == "max_size_movies" && o.Label != "Movies: Maximum size (GB)" {
			t.Errorf("label with prefix: %q", o.Label)
		}
	}
	if w, _ := call(http.MethodGet, "/api/providers/options?lang=english", ""); w.Code != http.StatusBadRequest {
		t.Errorf("bad lang: %d", w.Code)
	}

	// GitHub device login: the code comes back at once, the token is stored when the user has entered it
	hub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/login/device/code":
			_, _ = w.Write([]byte(`{"device_code":"DEV","user_code":"AB12-CD34","verification_uri":"https://github.com/login/device","expires_in":30,"interval":1}`))
		case "/login/oauth/access_token":
			_, _ = w.Write([]byte(`{"access_token":"TOKEN"}`))
		case "/user":
			_, _ = w.Write([]byte(`{"login":"octocat"}`))
		}
	}))
	defer hub.Close()
	oldLogin, oldAPI := burst.GitHubLogin, burst.GistAPI
	burst.GitHubLogin, burst.GistAPI = hub.URL, hub.URL+"/gists"
	t.Cleanup(func() { burst.GitHubLogin, burst.GistAPI = oldLogin, oldAPI })
	w, _ = call(http.MethodPost, "/api/providers/cookie-sync/login", "")
	var code DeviceLoginCode
	if err := json.Unmarshal(w.Body.Bytes(), &code); err != nil || w.Code != http.StatusOK || code.UserCode != "AB12-CD34" || code.VerificationUrl == "" {
		t.Fatalf("device login: %d %s", w.Code, w.Body)
	}
	deadline := time.Now().Add(5 * time.Second)
	for burst.LoadGeneral(providers.ProvidersDir()).Get("cookie_sync_user") != "octocat" {
		if time.Now().After(deadline) {
			t.Fatal("the token was not stored")
		}
		time.Sleep(50 * time.Millisecond)
	}
	if w, _ := call(http.MethodGet, "/api/providers/options", ""); strings.Contains(w.Body.String(), `"TOKEN"`) {
		t.Error("the token is returned by the API")
	}

	if w, _ := call(http.MethodPut, "/api/providers/options/filter_cam", `{"value":true}`); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"value":true`) {
		t.Errorf("set option: %d %s", w.Code, w.Body)
	}
	if w, _ := call(http.MethodPut, "/api/providers/options/max_size", `{"value":40}`); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"value":40`) {
		t.Errorf("set number: %d %s", w.Code, w.Body)
	}
	for path, body := range map[string]string{"/api/providers/options/filter_cam": `{"value":"yes"}`, "/api/providers/options/nope": `{"value":true}`} {
		if w, _ := call(http.MethodPut, path, body); w.Code != http.StatusBadRequest {
			t.Errorf("%s %s: %d", path, body, w.Code)
		}
	}
	// the provider settings set above survive the option changes: one file holds both
	if set := burst.LoadSettings(providers.ProvidersDir())["rutracker"]; set.Username != "me" {
		t.Errorf("provider settings lost: %+v", set)
	}
}

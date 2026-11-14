package jsonapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/elgatito/elementum/config"
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
	if w, _ := call(http.MethodPut, "/api/providers/nope", `{"enabled":true}`); w.Code != http.StatusNotFound {
		t.Errorf("unknown: %d", w.Code)
	}
	if w, _ := call(http.MethodPut, "/api/providers/yts", `{"enabled":"yes"}`); w.Code != http.StatusBadRequest {
		t.Errorf("bad body: %d", w.Code)
	}
}

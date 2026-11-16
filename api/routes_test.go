package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestSameSiteAndCORS(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(CORS(), SameSite())
	ok := func(c *gin.Context) { c.String(http.StatusOK, "ok") }
	r.GET("/shutdown", ok)
	r.GET("/web/", ok)
	r.GET("/api/system/info", ok)

	call := func(method, path string, headers map[string]string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, nil)
		req.Host = "192.168.1.10:65220"
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}
	for name, c := range map[string]struct {
		path    string
		headers map[string]string
		want    int
	}{
		"Kodi and scripts send no browser headers": {"/shutdown", nil, http.StatusOK},
		"the Web UI calls its own server":          {"/shutdown", map[string]string{"Sec-Fetch-Site": "same-origin"}, http.StatusOK},
		"typed in the address bar":                 {"/web/", map[string]string{"Sec-Fetch-Site": "none"}, http.StatusOK},
		"another service on the same machine":      {"/shutdown", map[string]string{"Sec-Fetch-Site": "same-site"}, http.StatusOK},
		"a page on the internet":                   {"/shutdown", map[string]string{"Sec-Fetch-Site": "cross-site"}, http.StatusForbidden},
		"an old browser with a foreign Origin":     {"/shutdown", map[string]string{"Origin": "https://evil.example"}, http.StatusForbidden},
		"an old browser on the same host":          {"/shutdown", map[string]string{"Origin": "http://192.168.1.10:65220"}, http.StatusOK},
		"a sandboxed page":                         {"/shutdown", map[string]string{"Origin": "null"}, http.StatusForbidden},
		"the JSON API is for other apps":           {"/api/system/info", map[string]string{"Sec-Fetch-Site": "cross-site", "Origin": "file://"}, http.StatusOK},
	} {
		if w := call(http.MethodGet, c.path, c.headers); w.Code != c.want {
			t.Errorf("%s: %d, want %d", name, w.Code, c.want)
		}
	}

	// CORS only on the JSON API
	if w := call(http.MethodGet, "/api/system/info", nil); w.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Error("no CORS on the API")
	}
	if w := call(http.MethodOptions, "/api/system/info", nil); w.Code != http.StatusNoContent {
		t.Errorf("API preflight: %d", w.Code)
	}
	if w := call(http.MethodGet, "/shutdown", nil); w.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Error("CORS on a Kodi route")
	}
}

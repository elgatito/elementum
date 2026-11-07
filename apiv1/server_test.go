package apiv1_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/elgatito/elementum/api"
	"github.com/elgatito/elementum/apiv1"
	"github.com/elgatito/elementum/config"
)

// newEngine wires the same global middleware as api.Routes, which can't run in a unit test (it needs the cache DB).
func newEngine() *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(api.CORS(), api.Auth())
	r.GET("/status", func(c *gin.Context) { c.String(http.StatusOK, "ok") }) // stands in for a Kodi route
	apiv1.Register(r, nil)
	return r
}

func setCreds(t *testing.T, login, password string) {
	old := config.Args
	t.Cleanup(func() { config.Args = old })
	config.Args.LocalLogin, config.Args.LocalPassword = login, password
}

type request struct {
	method, path, body string
	user, pass         string
	headers            map[string]string
}

func do(r *gin.Engine, req request) *httptest.ResponseRecorder {
	if req.method == "" {
		req.method = http.MethodGet
	}
	hr := httptest.NewRequest(req.method, req.path, strings.NewReader(req.body))
	if req.user != "" || req.pass != "" {
		hr.SetBasicAuth(req.user, req.pass)
	}
	if req.body != "" {
		hr.Header.Set("Content-Type", "application/json")
	}
	for k, v := range req.headers {
		hr.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, hr)
	return w
}

func errorCode(t *testing.T, w *httptest.ResponseRecorder) apiv1.ErrorCode {
	t.Helper()
	var e apiv1.Error
	if err := json.Unmarshal(w.Body.Bytes(), &e); err != nil {
		t.Fatalf("body is not an Error: %v: %q", err, w.Body.String())
	}
	if e.Error.Message == "" {
		t.Errorf("empty error message: %q", w.Body.String())
	}
	return e.Error.Code
}

func TestAuth(t *testing.T) {
	setCreds(t, "user", "secret")
	r := newEngine()

	for name, req := range map[string]request{
		"no credentials":    {path: "/api/v1/system/info"},
		"wrong password":    {path: "/api/v1/system/info", user: "user", pass: "nope"},
		"wrong user":        {path: "/api/v1/system/info", user: "nope", pass: "secret"},
		"before param errs": {path: "/api/v1/movies/lists/popular?page=x"},
	} {
		t.Run(name, func(t *testing.T) {
			w := do(r, req)
			if w.Code != http.StatusUnauthorized {
				t.Fatalf("status %d, want 401", w.Code)
			}
			if h := w.Header().Get("WWW-Authenticate"); h != "" {
				t.Errorf("WWW-Authenticate = %q, want none", h)
			}
			if code := errorCode(t, w); code != apiv1.ErrorCodeUnauthorized {
				t.Errorf("code %q", code)
			}
		})
	}

	t.Run("valid credentials", func(t *testing.T) {
		w := do(r, request{path: "/api/v1/system/info", user: "user", pass: "secret"})
		if w.Code != http.StatusOK {
			t.Fatalf("status %d: %s", w.Code, w.Body)
		}
		var info apiv1.SystemInfo
		if err := json.Unmarshal(w.Body.Bytes(), &info); err != nil {
			t.Fatal(err)
		}
		if !info.AuthRequired || info.Version == "" {
			t.Errorf("info = %+v", info)
		}
	})

	t.Run("stream skips Basic", func(t *testing.T) {
		w := do(r, request{path: "/api/v1/stream/abc/0?exp=1&sig=" + strings.Repeat("0", 64)})
		if w.Code == http.StatusUnauthorized {
			t.Fatalf("stream asked for Basic auth")
		}
	})

	t.Run("docs and spec are public", func(t *testing.T) {
		for _, path := range []string{"/api/docs", "/api/v1/openapi.yaml"} {
			if w := do(r, request{path: path}); w.Code != http.StatusOK {
				t.Errorf("%s: status %d", path, w.Code)
			}
		}
	})

	t.Run("Kodi routes keep the old auth", func(t *testing.T) {
		w := do(r, request{path: "/status"})
		if w.Code != http.StatusUnauthorized || w.Header().Get("WWW-Authenticate") == "" {
			t.Errorf("status %d, WWW-Authenticate %q", w.Code, w.Header().Get("WWW-Authenticate"))
		}
		if w := do(r, request{path: "/status", user: "user", pass: "secret"}); w.Code != http.StatusOK {
			t.Errorf("with credentials: status %d", w.Code)
		}
	})
}

func TestAuthDisabled(t *testing.T) {
	setCreds(t, "", "")
	w := do(newEngine(), request{path: "/api/v1/system/info"})
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body)
	}
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["auth_required"] != false {
		t.Errorf("auth_required = %v", body["auth_required"])
	}
	if v, ok := body["tracker"]; !ok || v != nil {
		t.Errorf("tracker = %v (present %v), want null", v, ok)
	}
}

func TestAuthPasswordOnly(t *testing.T) {
	setCreds(t, "", "secret")
	r := newEngine()
	for _, tc := range []struct {
		user, pass string
		status     int
	}{
		{"", "", http.StatusUnauthorized},
		{"", "nope", http.StatusUnauthorized},
		{"user", "secret", http.StatusUnauthorized},
		{"", "secret", http.StatusOK},
	} {
		if w := do(r, request{path: "/api/v1/system/info", user: tc.user, pass: tc.pass}); w.Code != tc.status {
			t.Errorf("%q:%q: status %d, want %d", tc.user, tc.pass, w.Code, tc.status)
		}
	}
}

func TestCORSPreflight(t *testing.T) {
	setCreds(t, "user", "secret")
	w := do(newEngine(), request{method: http.MethodOptions, path: "/api/v1/playback/abc", headers: map[string]string{
		"Origin":                         "http://tv.local",
		"Access-Control-Request-Method":  "DELETE",
		"Access-Control-Request-Headers": "Authorization",
	}})
	if w.Code != http.StatusNoContent {
		t.Fatalf("status %d", w.Code)
	}
	if o := w.Header().Get("Access-Control-Allow-Origin"); o != "*" {
		t.Errorf("Allow-Origin = %q", o)
	}
	if m := w.Header().Get("Access-Control-Allow-Methods"); !strings.Contains(m, "DELETE") {
		t.Errorf("Allow-Methods = %q", m)
	}
	if h := w.Header().Get("Access-Control-Allow-Headers"); !strings.Contains(h, "Authorization") {
		t.Errorf("Allow-Headers = %q", h)
	}
}

func TestErrors(t *testing.T) {
	setCreds(t, "user", "secret")
	r := newEngine()

	for _, tc := range []struct {
		req    request
		status int
		code   apiv1.ErrorCode
	}{
		// no tracker selected; static and param siblings in the same tree must both resolve
		{request{path: "/api/v1/tracker/shows/watchlist"}, http.StatusConflict, apiv1.ErrorCodeTrackerNotAuthorized},
		{request{path: "/api/v1/tracker/shows/progress"}, http.StatusConflict, apiv1.ErrorCodeTrackerNotAuthorized},
		{request{path: "/api/v1/tracker/lists"}, http.StatusConflict, apiv1.ErrorCodeTrackerNotAuthorized},
		// library, Trakt and tracker: bad enums and params are rejected before any fetch
		{request{path: "/api/v1/library/movies?page=0"}, http.StatusBadRequest, apiv1.ErrorCodeBadRequest},
		{request{path: "/api/v1/library/shows?lang=deu"}, http.StatusBadRequest, apiv1.ErrorCodeBadRequest},
		{request{path: "/api/v1/trakt/films/lists/trending"}, http.StatusBadRequest, apiv1.ErrorCodeBadRequest},
		{request{path: "/api/v1/trakt/movies/lists/boxoffice"}, http.StatusBadRequest, apiv1.ErrorCodeBadRequest},
		{request{path: "/api/v1/trakt/shows/lists/trending?limit=0"}, http.StatusBadRequest, apiv1.ErrorCodeBadRequest},
		{request{path: "/api/v1/tracker/films/watchlist"}, http.StatusBadRequest, apiv1.ErrorCodeBadRequest},
		{request{path: "/api/v1/tracker/films/collection"}, http.StatusBadRequest, apiv1.ErrorCodeBadRequest},
		{request{path: "/api/v1/tracker/shows/progress?page=0"}, http.StatusBadRequest, apiv1.ErrorCodeBadRequest},
		{request{path: "/api/v1/tracker/lists/42/items?media=films"}, http.StatusBadRequest, apiv1.ErrorCodeBadRequest},
		{request{path: "/api/v1/tracker/lists/42/items"}, http.StatusBadRequest, apiv1.ErrorCodeBadRequest},
		{request{path: "/api/v1/tracker/lists/a..b/items?media=movies"}, http.StatusBadRequest, apiv1.ErrorCodeBadRequest},
		{request{path: "/api/v1/movies/genres?lang=deu"}, http.StatusBadRequest, apiv1.ErrorCodeBadRequest},
		{request{path: "/api/v1/movies/603?lang=deu"}, http.StatusBadRequest, apiv1.ErrorCodeBadRequest},
		// catalog: bad enums and params are rejected before any TMDB call
		{request{path: "/api/v1/movies/lists/nope"}, http.StatusBadRequest, apiv1.ErrorCodeBadRequest},
		{request{path: "/api/v1/movies/lists/by_genre"}, http.StatusBadRequest, apiv1.ErrorCodeBadRequest},
		{request{path: "/api/v1/movies/lists/popular?page=0"}, http.StatusBadRequest, apiv1.ErrorCodeBadRequest},
		{request{path: "/api/v1/movies/lists/popular?limit=101"}, http.StatusBadRequest, apiv1.ErrorCodeBadRequest},
		{request{path: "/api/v1/shows/lists/now_playing"}, http.StatusBadRequest, apiv1.ErrorCodeBadRequest},
		{request{path: "/api/v1/shows/lists/by_genre?year=2000"}, http.StatusBadRequest, apiv1.ErrorCodeBadRequest},
		{request{path: "/api/v1/shows/genres?lang=de-"}, http.StatusBadRequest, apiv1.ErrorCodeBadRequest},
		{request{path: "/api/v1/shows/1396?lang=1"}, http.StatusBadRequest, apiv1.ErrorCodeBadRequest},
		{request{path: "/api/v1/shows/1396/seasons/1?lang=1"}, http.StatusBadRequest, apiv1.ErrorCodeBadRequest},
		{request{path: "/api/v1/search?query=matrix&type=person"}, http.StatusBadRequest, apiv1.ErrorCodeBadRequest},
		{request{path: "/api/v1/search?query=%20&type=movie"}, http.StatusBadRequest, apiv1.ErrorCodeBadRequest},
		{request{path: "/api/v1/search?query=matrix&type=multi"}, http.StatusBadRequest, apiv1.ErrorCodeBadRequest},
		{request{path: "/api/v1/search?query=matrix"}, http.StatusBadRequest, apiv1.ErrorCodeBadRequest},
		{request{path: "/api/v1/movies/lists/by_genre?genre=0"}, http.StatusBadRequest, apiv1.ErrorCodeBadRequest},
		{request{path: "/api/v1/shows/lists/by_genre?genre=18&year=1869"}, http.StatusBadRequest, apiv1.ErrorCodeBadRequest},
		{request{path: "/api/v1/movies/lists/by_genre?genre=28&year=2101"}, http.StatusBadRequest, apiv1.ErrorCodeBadRequest},
		{request{path: "/api/v1/search"}, http.StatusBadRequest, apiv1.ErrorCodeBadRequest},
		{request{method: http.MethodDelete, path: "/api/v1/playback/abc"}, http.StatusNotFound, apiv1.ErrorCodeNotFound},
		{request{path: "/api/v1/nope"}, http.StatusNotFound, apiv1.ErrorCodeNotFound},
		{request{path: "/api/nope"}, http.StatusNotFound, apiv1.ErrorCodeNotFound},
		{request{path: "/api/v1/movies/lists/popular?page=x"}, http.StatusBadRequest, apiv1.ErrorCodeBadRequest},
		{request{path: "/api/v1/movies/abc"}, http.StatusBadRequest, apiv1.ErrorCodeBadRequest},
		{request{method: http.MethodPost, path: "/api/v1/playback", body: "{"}, http.StatusBadRequest, apiv1.ErrorCodeBadRequest},
	} {
		t.Run(tc.req.method+" "+tc.req.path, func(t *testing.T) {
			tc.req.user, tc.req.pass = "user", "secret"
			w := do(r, tc.req)
			if w.Code != tc.status {
				t.Fatalf("status %d, want %d: %s", w.Code, tc.status, w.Body)
			}
			if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
				t.Errorf("Content-Type %q", ct)
			}
			if code := errorCode(t, w); code != tc.code {
				t.Errorf("code %q, want %q", code, tc.code)
			}
		})
	}

	t.Run("unknown Kodi path keeps gin's 404", func(t *testing.T) {
		w := do(r, request{path: "/nope", user: "user", pass: "secret"})
		if w.Code != http.StatusNotFound || strings.HasPrefix(w.Header().Get("Content-Type"), "application/json") {
			t.Errorf("status %d, Content-Type %q", w.Code, w.Header().Get("Content-Type"))
		}
	})
}

// Pages past TMDB's last one are answered empty without a TMDB call (none is reachable in tests).
func TestPastLastPage(t *testing.T) {
	setCreds(t, "", "")
	r := newEngine()
	for _, path := range []string{
		"/api/v1/movies/lists/popular?page=501&limit=20",
		"/api/v1/shows/lists/by_genre?genre=18&page=335&limit=30",
		"/api/v1/search?query=matrix&type=show&page=9223372036854775807&limit=100",
	} {
		w := do(r, request{path: path})
		var page apiv1.MediaItemPage
		if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil || w.Code != http.StatusOK {
			t.Fatalf("%s: status %d, %v: %s", path, w.Code, err, w.Body)
		}
		if page.Items == nil || len(page.Items) != 0 || page.TotalResults != nil || page.Page < 335 {
			t.Errorf("%s: %+v", path, page)
		}
	}
}

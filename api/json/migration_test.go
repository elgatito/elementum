package jsonapi_test

import (
	"net/http"
	"strings"
	"testing"
)

func TestUnversionedRoutes(t *testing.T) {
	setCreds(t, "user", "secret")
	r := newEngine()
	for _, path := range []string{"/api/system/info", "/api/openapi.yaml", "/api/docs"} {
		if w := do(r, request{path: path, user: "user", pass: "secret"}); w.Code != http.StatusOK {
			t.Errorf("%s: status %d, body %s", path, w.Code, w.Body)
		}
	}
	for _, path := range []string{"/api/v1/system/info", "/api/v1/openapi.yaml", "/api/v1/stream/test/0"} {
		w := do(r, request{path: path, user: "user", pass: "secret"})
		if w.Code != http.StatusNotFound || w.Header().Get("Location") != "" {
			t.Errorf("obsolete route %s: status %d, location %q", path, w.Code, w.Header().Get("Location"))
		}
	}
	for _, path := range []string{"/api", "/apiary"} {
		w := do(r, request{path: path})
		if w.Code != http.StatusUnauthorized || w.Header().Get("WWW-Authenticate") == "" {
			t.Errorf("%s bypassed legacy auth: status %d", path, w.Code)
		}
	}
	w := do(r, request{path: "/api/docs"})
	if !strings.Contains(w.Body.String(), `url: "/api/openapi.yaml"`) {
		t.Error("Swagger UI does not load the unversioned contract")
	}
}
